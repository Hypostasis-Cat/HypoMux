package services

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

var tunConnectivityURLs = []string{
	"http://www.msftconnecttest.com/connecttest.txt",
	"https://www.baidu.com/",
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
// paths: the DNS bootstrap endpoint is reached directly through its literal
// IP, while public HTTP is sent through the aggregation SOCKS channel.
func probeTUNConnectivityThroughChannels(
	parent context.Context,
	aggregationEndpoint string,
	dnsResult dnsResolveResult,
) (tunConnectivityReport, error) {
	report := tunConnectivityReport{Checks: make([]tunConnectivityCheck, 0, len(tunConnectivityURLs)+1)}
	report.Checks = append(report.Checks, probeDNSBootstrap(parent, dnsResult))
	for _, endpoint := range tunConnectivityURLs {
		report.Checks = append(report.Checks, probeHTTPURL(parent, endpoint, &aggregationEndpoint, "aggregation"))
	}
	if len(tunConnectivityURLs) == 0 {
		report.Checks = append(report.Checks, tunConnectivityCheck{
			Stage: "aggregation_data", Endpoint: "none", Outbound: "aggregation",
			Error: "no HTTP connectivity endpoints configured",
		})
	}
	return report, report.failure()
}

func probeDNSBootstrap(parent context.Context, result dnsResolveResult) tunConnectivityCheck {
	check := tunConnectivityCheck{
		Stage:    "dns_bootstrap",
		Endpoint: result.Server,
		Outbound: "system-direct",
	}
	transport := strings.ToLower(strings.TrimSpace(result.Transport))
	if transport == "" || strings.TrimSpace(result.Server) == "" {
		check.Error = "DNS bootstrap result is incomplete"
		return check
	}
	server := strings.TrimSpace(result.Server)
	serverName := ""
	if parts := strings.SplitN(server, "@", 2); len(parts) == 2 {
		serverName = strings.TrimSpace(parts[0])
		server = strings.TrimSpace(parts[1])
	}
	defaultPort := 53
	if transport == "doh" {
		defaultPort = 443
	} else if transport == "dot" {
		defaultPort = 853
	}
	host, port, err := splitEndpoint(server, defaultPort)
	if err != nil {
		check.Error = err.Error()
		return check
	}
	ip := net.ParseIP(host)
	if ip == nil {
		check.Error = "DNS bootstrap endpoint is not a literal IP"
		return check
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	network := "tcp4"
	if ip.To4() == nil {
		network = "tcp6"
	}
	address := net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port))
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	switch transport {
	case "doh", "dot":
		connection, dialErr := dialer.DialContext(ctx, network, address)
		if dialErr != nil {
			check.Error = dialErr.Error()
			return check
		}
		defer connection.Close()
		if serverName == "" {
			serverName = host
		}
		tlsConnection := tls.Client(connection, &tls.Config{
			MinVersion: tls.VersionTLS12, ServerName: serverName,
		})
		if deadline, ok := ctx.Deadline(); ok {
			_ = tlsConnection.SetDeadline(deadline)
		}
		if handshakeErr := tlsConnection.HandshakeContext(ctx); handshakeErr != nil {
			check.Error = handshakeErr.Error()
			return check
		}
		query := buildConnectivityDNSQuery()
		if transport == "doh" {
			request, requestErr := http.NewRequestWithContext(
				ctx, http.MethodPost, "https://dns-bootstrap.invalid/dns-query", bytes.NewReader(query),
			)
			if requestErr != nil {
				check.Error = requestErr.Error()
				return check
			}
			request.Host = serverName
			request.Header.Set("Accept", "application/dns-message")
			request.Header.Set("Content-Type", "application/dns-message")
			request.Header.Set("User-Agent", "HypoMux-Desktop/1")
			request.Close = true
			if requestErr = request.Write(tlsConnection); requestErr != nil {
				check.Error = requestErr.Error()
				return check
			}
			response, responseErr := http.ReadResponse(bufio.NewReader(tlsConnection), request)
			if responseErr != nil {
				check.Error = responseErr.Error()
				return check
			}
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
			_ = response.Body.Close()
			if readErr != nil {
				check.Error = readErr.Error()
				return check
			}
			if response.StatusCode != http.StatusOK {
				check.Error = fmt.Sprintf("DoH HTTP %d", response.StatusCode)
				return check
			}
			if len(body) < 12 {
				check.Error = "DoH returned a truncated DNS response"
				return check
			}
		} else {
			if err := binary.Write(tlsConnection, binary.BigEndian, uint16(len(query))); err != nil {
				check.Error = err.Error()
				return check
			}
			if _, err := tlsConnection.Write(query); err != nil {
				check.Error = err.Error()
				return check
			}
			var length uint16
			if err := binary.Read(tlsConnection, binary.BigEndian, &length); err != nil {
				check.Error = err.Error()
				return check
			}
			if length < 12 {
				check.Error = "DoT returned a truncated DNS response"
				return check
			}
			response := make([]byte, int(length))
			if _, err := io.ReadFull(tlsConnection, response); err != nil {
				check.Error = err.Error()
				return check
			}
		}
		check.OK = true
		check.Detail = fmt.Sprintf("%s DNS response via %s", strings.ToUpper(transport), address)
		return check
	case "udp":
		connection, dialErr := dialer.DialContext(ctx, "udp"+ipVersionSuffix(ip), address)
		if dialErr != nil {
			check.Error = dialErr.Error()
			return check
		}
		defer connection.Close()
		if deadline, ok := ctx.Deadline(); ok {
			_ = connection.SetDeadline(deadline)
		}
		query := buildConnectivityDNSQuery()
		if _, writeErr := connection.Write(query); writeErr != nil {
			check.Error = writeErr.Error()
			return check
		}
		response := make([]byte, 4096)
		count, readErr := connection.Read(response)
		if readErr != nil {
			check.Error = readErr.Error()
			return check
		}
		if count < 12 {
			check.Error = "DNS bootstrap returned a truncated response"
			return check
		}
		check.OK = true
		check.Detail = fmt.Sprintf("UDP DNS response via %s", address)
		return check
	case "tcp":
		connection, dialErr := dialer.DialContext(ctx, network, address)
		if dialErr != nil {
			check.Error = dialErr.Error()
			return check
		}
		defer connection.Close()
		if deadline, ok := ctx.Deadline(); ok {
			_ = connection.SetDeadline(deadline)
		}
		query := buildConnectivityDNSQuery()
		if err := binary.Write(connection, binary.BigEndian, uint16(len(query))); err != nil {
			check.Error = err.Error()
			return check
		}
		if _, err := connection.Write(query); err != nil {
			check.Error = err.Error()
			return check
		}
		var length uint16
		if err := binary.Read(connection, binary.BigEndian, &length); err != nil {
			check.Error = err.Error()
			return check
		}
		if length < 12 {
			check.Error = "DNS bootstrap returned a truncated TCP response"
			return check
		}
		if _, err := io.CopyN(io.Discard, connection, int64(length)); err != nil {
			check.Error = err.Error()
			return check
		}
		check.OK = true
		check.Detail = fmt.Sprintf("TCP DNS response via %s", address)
		return check
	default:
		check.Error = "unsupported DNS bootstrap transport: " + result.Transport
		return check
	}
}

func probeHTTPURL(
	parent context.Context,
	endpoint string,
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
	requestEndpoint := endpoint
	originalHost := ""
	if aggregationEndpoint != nil {
		var err error
		requestEndpoint, originalHost, err = resolveAggregationTarget(parent, endpoint)
		if err != nil {
			check.Error = err.Error()
			return check
		}
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

// resolveAggregationTarget converts a domain target into a literal IP before
// it enters an aggregation channel. Channel schedulers deliberately reject
// domains; the HTTP Host header and TLS SNI retain the original name.
func resolveAggregationTarget(parent context.Context, endpoint string) (string, string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", "", errors.New("aggregation probe endpoint is missing a scheme or host")
	}
	host := parsed.Hostname()
	if host == "" {
		return "", "", errors.New("aggregation probe endpoint has an empty host")
	}
	originalHost := parsed.Host
	if net.ParseIP(host) != nil {
		return endpoint, originalHost, nil
	}
	lookupContext, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIP(lookupContext, "ip", host)
	if err != nil {
		return "", "", fmt.Errorf("aggregation target DNS lookup for %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return "", "", fmt.Errorf("aggregation target DNS lookup for %q returned no addresses", host)
	}
	selected := addresses[0]
	for _, address := range addresses {
		if address.To4() != nil {
			selected = address
			break
		}
	}
	if port := parsed.Port(); port != "" {
		parsed.Host = net.JoinHostPort(selected.String(), port)
	} else if selected.To4() != nil {
		parsed.Host = selected.String()
	} else {
		parsed.Host = "[" + selected.String() + "]"
	}
	return parsed.String(), originalHost, nil
}

func buildConnectivityDNSQuery() []byte {
	const domain = "www.msftconnecttest.com"
	query := make([]byte, 12)
	query[0] = byte(time.Now().UnixNano() >> 8)
	query[1] = byte(time.Now().UnixNano())
	query[2] = 1
	query[5] = 1
	for _, label := range strings.Split(domain, ".") {
		query = append(query, byte(len(label)))
		query = append(query, label...)
	}
	query = append(query, 0, 0, 1, 0, 1)
	return query
}

func ipVersionSuffix(ip net.IP) string {
	if ip.To4() == nil {
		return "6"
	}
	return "4"
}
