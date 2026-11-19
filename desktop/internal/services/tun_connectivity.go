package services

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	xproxy "golang.org/x/net/proxy"
)

var tunConnectivityURLs = []string{
	"http://www.msftconnecttest.com/connecttest.txt",
	"https://www.baidu.com/",
}

const connectivityDNSDomain = "www.msftconnecttest.com"

const tunDataPathURL = "http://www.msftconnecttest.com/connecttest.txt"

var tunDataPathProbe = func(parent context.Context) tunConnectivityCheck {
	return probeTUNDataPathPlatform(parent, append([]string(nil), tunConnectivityURLs...))
}

type tunConnectivityCheck struct {
	Stage    string `json:"stage"`
	Endpoint string `json:"endpoint"`
	Outbound string `json:"outbound"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail,omitempty"`
	Error    string `json:"error,omitempty"`
}

type tunConnectivityReport struct {
	Checks []tunConnectivityCheck `json:"checks"`
}

func (r tunConnectivityReport) summary() string {
	parts := make([]string, 0, len(r.Checks))
	for _, check := range r.Checks {
		value := fmt.Sprintf(
			"stage=%s endpoint=%s outbound=%s ok=%t",
			check.Stage, check.Endpoint, check.Outbound, check.OK,
		)
		if check.Error != "" {
			value += " error=" + check.Error
		} else if check.Detail != "" {
			value += " detail=" + check.Detail
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, "; ")
}

func (r tunConnectivityReport) failure() error {
	requiredFailures := make([]error, 0, len(r.Checks))
	aggregationFailures := make([]error, 0, len(r.Checks))
	aggregationSeen := false
	aggregationOK := false
	for _, check := range r.Checks {
		if check.Stage == "aggregation_data" {
			aggregationSeen = true
			if check.OK {
				aggregationOK = true
				continue
			}
			aggregationFailures = append(aggregationFailures, connectivityCheckError(check))
			continue
		}
		if !check.OK {
			requiredFailures = append(requiredFailures, connectivityCheckError(check))
		}
	}
	if aggregationSeen && !aggregationOK {
		requiredFailures = append(requiredFailures, aggregationFailures...)
	}
	if len(requiredFailures) == 0 {
		return nil
	}
	return errors.Join(requiredFailures...)
}

func connectivityCheckError(check tunConnectivityCheck) error {
	detail := check.Error
	if detail == "" {
		detail = check.Detail
	}
	if detail == "" {
		detail = "check failed"
	}
	return fmt.Errorf(
		"stage=%s endpoint=%s outbound=%s: %s",
		check.Stage, check.Endpoint, check.Outbound, detail,
	)
}

// probeTUNConnectivity is kept as a small direct-HTTP helper for compatibility
// tests and explicit physical-path diagnostics. Startup and the periodic
// watchdog use the channel-aware variant below so HypoMux.exe's system-direct
// route exemption cannot make a false positive look like an aggregation
// success.
func probeTUNConnectivity(parent context.Context) (string, error) {
	report := tunConnectivityReport{Checks: make([]tunConnectivityCheck, 0, len(tunConnectivityURLs))}
	for _, endpoint := range tunConnectivityURLs {
		check := probeHTTPURL(parent, endpoint, nil, "system-direct")
		report.Checks = append(report.Checks, check)
	}
	if len(report.Checks) == 0 {
		report.Checks = append(report.Checks, tunConnectivityCheck{
			Stage: "aggregation_data", Endpoint: "none", Outbound: "system-direct",
			Error: "no HTTP connectivity endpoints configured",
		})
	}
	if err := report.failure(); err != nil {
		return "", err
	}
	return report.summary(), nil
}

// probeTUNConnectivityThroughChannels verifies the two independent startup
// paths: Core queries the selected DNS endpoint on its bound adapter, while
// public HTTP is sent through the aggregation SOCKS channel.
func probeTUNConnectivityThroughChannels(
	parent context.Context,
	aggregationEndpoint string,
	dnsResult dnsResolveResult,
	probe func(context.Context, dnsResolveResult) error,
	resolvers ...connectivityDNSResolver,
) (tunConnectivityReport, error) {
	probes := make([]func() tunConnectivityCheck, 0, len(tunConnectivityURLs)+2)
	probes = append(probes, func() tunConnectivityCheck { return probeDNSBootstrap(parent, dnsResult, probe) })
	aggregationTargets := 0
	for _, endpoint := range tunConnectivityURLs {
		if len(resolvers) > 0 && resolvers[0] != nil {
			endpoint := endpoint
			aggregationTargets++
			probes = append(probes, func() tunConnectivityCheck {
				return probeAggregationAlternatives(parent, endpoint, aggregationEndpoint, dnsResult, resolvers[0])
			})
			continue
		}
		requestEndpoint, originalHost, supported, err := resolveAggregationTarget(endpoint, dnsResult)
		if !supported {
			continue
		}
		aggregationTargets++
		if err != nil {
			failure := tunConnectivityCheck{
				Stage: "aggregation_data", Endpoint: endpoint, Outbound: "aggregation", Error: err.Error(),
			}
			probes = append(probes, func() tunConnectivityCheck { return failure })
			continue
		}
		endpoint, requestEndpoint, originalHost := endpoint, requestEndpoint, originalHost
		probes = append(probes, func() tunConnectivityCheck {
			return probeHTTPURLTarget(
				parent, endpoint, requestEndpoint, originalHost, &aggregationEndpoint, "aggregation",
			)
		})
	}
	if aggregationTargets == 0 {
		failure := tunConnectivityCheck{
			Stage: "aggregation_data", Endpoint: "none", Outbound: "aggregation",
			Error: "no connectivity endpoint matches the pre-TUN DNS result",
		}
		probes = append(probes, func() tunConnectivityCheck { return failure })
	}
	probes = append(probes, func() tunConnectivityCheck { return tunDataPathProbe(parent) })
	report := tunConnectivityReport{Checks: make([]tunConnectivityCheck, len(probes))}
	var wait sync.WaitGroup
	wait.Add(len(probes))
	for index, probe := range probes {
		index, probe := index, probe
		go func() {
			defer wait.Done()
			report.Checks[index] = probe()
		}()
	}
	wait.Wait()
	return report, report.failure()
}

// The data-path probe is an ordinary unmatched application flow. When the
// global default rule rejects unmatched traffic, its failure is the configured
// policy working, not a broken tunnel, so it must not raise a connectivity
// notice on every watchdog cycle. The aggregation and DNS checks still apply.
func discountRejectedTUNDataPath(report tunConnectivityReport, err error, defaultOutbound string) (tunConnectivityReport, error) {
	if err == nil || strings.TrimSpace(defaultOutbound) != OutboundReject {
		return report, err
	}
	checks := append([]tunConnectivityCheck(nil), report.Checks...)
	for index := range checks {
		if checks[index].Stage == "tun_data_path" && !checks[index].OK {
			checks[index].OK = true
			checks[index].Detail = "skipped: global default rule rejects unmatched traffic"
			checks[index].Error = ""
		}
	}
	report = tunConnectivityReport{Checks: checks}
	return report, report.failure()
}

type connectivityDNSResolver func(context.Context, string, string) (dnsResolveResult, error)

func resolveConnectivityBootstrap(ctx context.Context, adapter string, resolve connectivityDNSResolver) (dnsResolveResult, error) {
	var failures []error
	for _, endpoint := range tunConnectivityURLs {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		lookupCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		result, err := resolve(lookupCtx, parsed.Hostname(), adapter)
		cancel()
		if err == nil {
			return result, nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", parsed.Hostname(), err))
	}
	if len(failures) == 0 {
		return dnsResolveResult{}, errors.New("no DNS connectivity targets configured")
	}
	return dnsResolveResult{}, errors.Join(failures...)
}

// Resolve through Core's adapter-bound DNS, never the system resolver (which
// may return FakeIP after TUN takeover). Refresh each round so expired CDN
// addresses are not pinned for the entire session.
func probeAggregationAlternatives(parent context.Context, endpoint, socks string, bootstrap dnsResolveResult, resolve connectivityDNSResolver) tunConnectivityCheck {
	failure := tunConnectivityCheck{Stage: "aggregation_data", Endpoint: endpoint, Outbound: "aggregation"}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" {
		failure.Error = "invalid connectivity endpoint"
		return failure
	}
	result := dnsResolveResult{Domain: parsed.Hostname(), Address: parsed.Hostname()}
	if net.ParseIP(parsed.Hostname()) == nil {
		ctx, cancel := context.WithTimeout(parent, 4*time.Second)
		result, err = resolve(ctx, parsed.Hostname(), bootstrap.Adapter)
		cancel()
		if err != nil {
			failure.Error = "resolve connectivity target: " + err.Error()
			return failure
		}
	}
	addresses := append([]string{result.Address}, result.Addresses...)
	seen := map[string]bool{}
	targets := make([]string, 0, 3)
	for _, address := range addresses {
		ip := net.ParseIP(address)
		if ip == nil || seen[ip.String()] || isConnectivityFakeIP(ip) {
			continue
		}
		seen[ip.String()] = true
		candidate := result
		candidate.Domain, candidate.Address = parsed.Hostname(), ip.String()
		target, _, supported, targetErr := resolveAggregationTarget(endpoint, candidate)
		if targetErr != nil || !supported {
			continue
		}
		targets = append(targets, target)
		if len(targets) == 3 {
			break
		}
	}
	if len(targets) == 0 {
		failure.Error = "DNS returned no usable literal connectivity address"
		return failure
	}
	checks := make([]tunConnectivityCheck, len(targets))
	var wg sync.WaitGroup
	for index, target := range targets {
		wg.Add(1)
		go func(index int, target string) {
			defer wg.Done()
			checks[index] = probeHTTPURLTarget(parent, endpoint, target, parsed.Host, &socks, "aggregation")
			checks[index].Detail += " target=" + target
		}(index, target)
	}
	wg.Wait()
	details := make([]string, 0, len(checks))
	for _, check := range checks {
		failure.OK = failure.OK || check.OK
		details = append(details, fmt.Sprintf("%s ok=%t error=%s", check.Detail, check.OK, check.Error))
	}
	failure.Detail = strings.Join(details, "; ")
	if !failure.OK {
		failure.Error = failure.Detail
	}
	return failure
}

func isConnectivityFakeIP(ip net.IP) bool {
	v4 := ip.To4()
	if v4 != nil {
		return v4[0] == 198 && (v4[1] == 18 || v4[1] == 19)
	}
	v6 := ip.To16()
	return v6 != nil && v6[0] == 0xfc && v6[1] == 0 && v6[2]&0xc0 == 0 // fc00::/18
}

func (s *EngineService) resolveConnectivityDNS(ctx context.Context, domain, adapter string) (dnsResolveResult, error) {
	var result dnsResolveResult
	err := s.client.Request(ctx, "dns.resolve", map[string]any{
		"domain": domain, "adapter": adapter, "timeout_ms": 1500,
	}, &result)
	return result, err
}

func (s *EngineService) probeConnectivityDNS(ctx context.Context, target dnsResolveResult) error {
	var result dnsResolveResult
	err := s.client.Request(ctx, "dns.resolve", map[string]any{
		"domain": connectivityDNSDomain, "adapter": target.Adapter, "record_type": "A",
		"timeout_ms": 3500, "probe_server": target.Server, "probe_transport": target.Transport,
		"probe_doh_path": target.DoHPath,
	}, &result)
	if err != nil {
		return err
	}
	if !result.Probe {
		return errors.New("HY Core does not support fresh DNS probes")
	}
	return nil
}

func probeDNSBootstrap(parent context.Context, target dnsResolveResult, probe func(context.Context, dnsResolveResult) error) tunConnectivityCheck {
	check := tunConnectivityCheck{Stage: "dns_bootstrap", Endpoint: target.Server, Outbound: "core-dns"}
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	if err := probe(ctx, target); err != nil {
		check.Error = err.Error()
		return check
	}
	check.OK = true
	check.Detail = fmt.Sprintf("Core resolved %s through %s on %s", connectivityDNSDomain, target.Server, target.Adapter)
	return check
}

func probeHTTPURL(
	parent context.Context,
	endpoint string,
	aggregationEndpoint *string,
	outbound string,
) tunConnectivityCheck {
	return probeHTTPURLTarget(parent, endpoint, endpoint, "", aggregationEndpoint, outbound)
}

func probeHTTPURLTarget(
	parent context.Context,
	endpoint string,
	requestEndpoint string,
	originalHost string,
	aggregationEndpoint *string,
	outbound string,
) tunConnectivityCheck {
	check := tunConnectivityCheck{
		Stage:    "aggregation_data",
		Endpoint: endpoint,
		Outbound: outbound,
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: -1,
		}).DialContext,
		DisableKeepAlives: true,
	}
	if aggregationEndpoint != nil {
		if parsed, parseErr := url.Parse(endpoint); parseErr == nil && strings.EqualFold(parsed.Scheme, "https") {
			transport.TLSClientConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
				ServerName: parsed.Hostname(),
			}
		}
	}
	if aggregationEndpoint != nil {
		endpointValue := strings.TrimSpace(*aggregationEndpoint)
		if _, _, err := net.SplitHostPort(endpointValue); err != nil {
			check.Error = "aggregation SOCKS endpoint is invalid: " + err.Error()
			return check
		}
		dialer, err := xproxy.SOCKS5("tcp", endpointValue, nil, &net.Dialer{Timeout: 3 * time.Second})
		if err != nil {
			check.Error = err.Error()
			return check
		}
		transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
			if contextual, ok := dialer.(xproxy.ContextDialer); ok {
				return contextual.DialContext(ctx, network, address)
			}
			return dialer.Dial(network, address)
		}
		check.Detail = "via " + endpointValue
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   8 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("connectivity probe followed too many redirects")
			}
			return nil
		},
	}
	request, err := http.NewRequestWithContext(parent, http.MethodGet, requestEndpoint, nil)
	if err != nil {
		check.Error = err.Error()
		return check
	}
	if originalHost != "" {
		// The aggregation channel receives the literal IP target required by the
		// TUN TCP pool. Keep the original host for HTTP virtual hosting and TLS
		// SNI so the probe still validates the intended endpoint.
		request.Host = originalHost
	}
	response, err := client.Do(request)
	if err != nil {
		check.Error = err.Error()
		return check
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 500 {
		check.Error = fmt.Sprintf("HTTP %d", response.StatusCode)
		return check
	}
	check.OK = true
	if check.Detail == "" {
		check.Detail = fmt.Sprintf("HTTP %d", response.StatusCode)
	} else {
		check.Detail += fmt.Sprintf(" -> HTTP %d", response.StatusCode)
	}
	return check
}

// resolveAggregationTarget converts a target into the literal address resolved
// before TUN/FakeIP takeover. Channel schedulers deliberately require literal
// addresses, so resolving again through the system resolver after TUN startup
// would incorrectly feed a 198.18.0.0/15 FakeIP to a physical adapter.
func resolveAggregationTarget(
	endpoint string,
	dnsResult dnsResolveResult,
) (string, string, bool, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", "", true, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", "", true, errors.New("aggregation probe endpoint is missing a scheme or host")
	}
	host := parsed.Hostname()
	if host == "" {
		return "", "", true, errors.New("aggregation probe endpoint has an empty host")
	}
	originalHost := parsed.Host
	if net.ParseIP(host) != nil {
		return endpoint, originalHost, true, nil
	}
	if !strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(strings.TrimSpace(dnsResult.Domain), ".")) {
		return "", "", false, nil
	}
	selected := net.ParseIP(strings.TrimSpace(dnsResult.Address))
	if selected == nil {
		return "", "", true, fmt.Errorf("pre-TUN DNS result for %q has no literal address", host)
	}
	if port := parsed.Port(); port != "" {
		parsed.Host = net.JoinHostPort(selected.String(), port)
	} else if selected.To4() != nil {
		parsed.Host = selected.String()
	} else {
		parsed.Host = "[" + selected.String() + "]"
	}
	return parsed.String(), originalHost, true, nil
}
