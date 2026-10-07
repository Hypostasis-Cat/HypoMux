package services

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Read the running Core's normalized configuration instead of duplicating its
// provider catalog or requiring a public test domain to resolve before TUN can
// be created. This RPC is local and does not perform a network query.
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

// A diagnostic failure is returned separately from a configuration failure.
// Only the latter prevents creating a valid TUN configuration.
func prepareTUNDNS(ctx context.Context, adapter AdapterView, force bool,
	resolve connectivityDNSResolver, configuration func(context.Context) (tunDNSConfiguration, error),
) (result dnsResolveResult, diagnosticErr error, err error) {
	if !force {
		result, diagnosticErr = resolveConnectivityBootstrap(ctx, adapter.Name, resolve)
		if diagnosticErr == nil {
			return result, nil, nil
		}
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
