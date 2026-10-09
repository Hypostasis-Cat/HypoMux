package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

// Read the running Core's normalized configuration instead of duplicating its
// provider catalog. Explicit force startup can use this local RPC without a
// public test-domain query; normal startup must verify its DNS egress first.
type tunDNSConfiguration struct {
	Policy        string           `json:"policy"`
	LegacyServers []string         `json:"legacy_servers"`
	DoHEndpoints  []tunDoHEndpoint `json:"doh_endpoints"`
}

type tunDoHEndpoint struct {
	IP   string `json:"ip"`
	Host string `json:"host"`
	Path string `json:"path"`
	Port int    `json:"port,omitempty"`
}

func configuredTUNDNS(config tunDNSConfiguration, adapter AdapterView) (dnsResolveResult, error) {
	result := dnsResolveResult{Adapter: adapter.Name}
	useNetworkDNS := config.Policy == "auto" && adapter.Address == "" && len(adapter.DNSServers) > 0
	if config.Policy != "off" && config.Policy != "system" && !useNetworkDNS {
		for _, endpoint := range config.DoHEndpoints {
			ip := net.ParseIP(endpoint.IP)
			if ip != nil && ((ip.To4() != nil && adapter.Address != "") || (ip.To4() == nil && adapter.SourceIPv6 != "")) && endpoint.Host != "" {
				port := endpoint.Port
				if port == 0 {
					port = 443
				}
				result.Transport, result.Server = "doh", endpoint.Host+"@"+net.JoinHostPort(endpoint.IP, fmt.Sprint(port))
				result.DoHPath = endpoint.Path
				return result, nil
			}
		}
		return result, fmt.Errorf("核心未提供当前 DNS 策略 %q 的有效上游配置", config.Policy)
	}
	for _, server := range append(append([]string(nil), adapter.DNSServers...), config.LegacyServers...) {
		if ip, err := netip.ParseAddr(strings.TrimSpace(server)); err == nil && ((ip.Unmap().Is4() && adapter.Address != "") || (ip.Is6() && !ip.Is4In6() && adapter.SourceIPv6 != "")) {
			result.Transport, result.Server = "udp", net.JoinHostPort(ip.String(), "53")
			return result, nil
		}
	}
	return result, fmt.Errorf("核心未提供有效的传统 DNS 上游配置")
}

// Normal startup requires a working source-bound DNS query. Only an explicit
// force start may build configuration without that verification.
func prepareTUNDNS(ctx context.Context, adapter AdapterView, force bool,
	resolve connectivityDNSResolver, configuration func(context.Context) (tunDNSConfiguration, error),
) (result dnsResolveResult, diagnosticErr error, err error) {
	if !force {
		result, diagnosticErr = resolveConnectivityBootstrap(ctx, adapter.Name, resolve)
		if diagnosticErr == nil {
			return result, nil, nil
		}
		return result, diagnosticErr, diagnosticErr
	}
	config, err := configuration(ctx)
	if err != nil {
		return result, diagnosticErr, err
	}
	result, err = configuredTUNDNS(config, adapter)
	needsBootstrap := config.Policy == "dnspod" && adapter.Address == "" && adapter.SourceIPv6 != ""
	for _, endpoint := range config.DoHEndpoints {
		needsBootstrap = needsBootstrap || endpoint.IP == ""
	}
	if err != nil && needsBootstrap {
		// Providers with hostname access need source-bound bootstrap on force
		// too, including DNSPod's IPv6 path.
		result, err = resolveConnectivityBootstrap(ctx, adapter.Name, resolve)
		if err != nil {
			err = fmt.Errorf("DoH 上游引导失败: %w", err)
		}
	}
	return result, diagnosticErr, err
}

type preparedTUNDNS struct {
	Result dnsResolveResult
	Pool   []dnsResolveResult
}

// Preserve the preferred route first, then try the other selected, operational
// adapters by metric. Explicit egress modes never silently change interfaces.
func prepareTUNDNSEgress(ctx context.Context, preferred tunDNSEgressDecision, selected []AdapterView,
	prepare func(context.Context, AdapterView) (preparedTUNDNS, error),
	record func(tunDNSEgressDecision, error),
) (tunDNSEgressDecision, preparedTUNDNS, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	candidates := []tunDNSEgressDecision{preferred}
	if preferred.Mode == DNSEgressAuto {
		remaining := make([]AdapterView, 0, len(selected))
		seen := map[string]bool{strings.ToLower(preferred.Adapter.ID): true}
		for _, adapter := range selected {
			id := strings.ToLower(adapter.ID)
			if adapter.Selected && adapter.Operational && !seen[id] {
				remaining = append(remaining, adapter)
				seen[id] = true
			}
		}
		for len(remaining) > 0 {
			adapter := lowestMetricAdapter(remaining)
			candidates = append(candidates, tunDNSEgressDecision{
				Adapter: adapter, Mode: DNSEgressAuto, Source: "verified_fallback",
				Detail: fmt.Sprintf("首选 DNS 出口 %s 验证失败，已切换至 %s", preferred.Adapter.Name, adapter.Name),
			})
			for i := range remaining {
				if remaining[i].ID == adapter.ID {
					remaining = append(remaining[:i], remaining[i+1:]...)
					break
				}
			}
		}
	}
	var failures []error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		probeCtx, stop := context.WithTimeout(ctx, 8*time.Second)
		prepared, err := prepare(probeCtx, candidate.Adapter)
		if err == nil {
			err = probeCtx.Err()
		}
		stop()
		if record != nil {
			record(candidate, err)
		}
		if err == nil {
			return candidate, prepared, nil
		}
		failures = append(failures, fmt.Errorf("%s：%w", candidate.Adapter.Name, err))
	}
	return preferred, preparedTUNDNS{}, fmt.Errorf("没有通过验证的 TUN DNS 出口，已取消启动；请检查网络或 DNS 设置：%w", errors.Join(failures...))
}

func (s *EngineService) prepareTUNDNSAdapter(ctx context.Context, adapter AdapterView, force, pool bool) (preparedTUNDNS, error) {
	if !pool {
		result, _, err := prepareTUNDNS(ctx, adapter, force, s.resolveConnectivityDNS, s.tunDNSConfiguration)
		return preparedTUNDNS{Result: result}, err
	}
	return prepareVerifiedTUNDNSPool(ctx, adapter, force, s.resolveConnectivityDNS, s.configuredTUNDNSPool)
}

func prepareVerifiedTUNDNSPool(ctx context.Context, adapter AdapterView, force bool,
	resolve connectivityDNSResolver, configure func(context.Context, AdapterView) ([]dnsResolveResult, error),
) (preparedTUNDNS, error) {
	var verified dnsResolveResult
	if !force {
		var err error
		verified, err = resolveConnectivityBootstrap(ctx, adapter.Name, resolve)
		if err != nil {
			return preparedTUNDNS{}, err
		}
	}
	upstreams, err := configure(ctx, adapter)
	if err != nil {
		return preparedTUNDNS{}, err
	}
	if len(upstreams) == 0 {
		return preparedTUNDNS{}, fmt.Errorf("当前网卡没有可用的 DNS 上游地址")
	}
	result := upstreams[0]
	if !force {
		// Connectivity checks must use the successful upstream, which may be
		// a fallback provider rather than the first configured pool entry.
		result = verified
	}
	result.Adapter = adapter.Name
	return preparedTUNDNS{Result: result, Pool: upstreams}, nil
}

func (s *EngineService) tunDNSConfiguration(ctx context.Context) (tunDNSConfiguration, error) {
	var config tunDNSConfiguration
	err := s.client.Request(ctx, "dns.status", nil, &config)
	return config, err
}

func (s *EngineService) recordTUNConnectivityOutcome(err error) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tunConnectivityNotice = ""
	if err != nil {
		s.tunConnectivityNotice = "提示：虚拟网卡已启动，但外部联网探测未通过。探测站点可能受限，请以实际访问为准；若无法联网，请手动停止并查看网络诊断。"
	}
	return s.tunConnectivityNotice
}
