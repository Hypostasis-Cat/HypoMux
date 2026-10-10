package services

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

// Hostname bootstrap is performed by the Core's source-bound legacy resolver;
// it never uses the WebView process or Windows' default resolver.
func (s *EngineService) configuredTUNDNSPool(ctx context.Context, adapter AdapterView) ([]dnsResolveResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	config, err := s.tunDNSConfiguration(ctx)
	if err != nil {
		return nil, err
	}
	var result []dnsResolveResult
	endpoints, transport, defaultPort := config.encryptedEndpoints()
	for _, endpoint := range endpoints {
		addresses := []string{endpoint.IP}
		if endpoint.IP == "" || (config.Policy == "dnspod" && adapter.Address == "" && adapter.SourceIPv6 != "") {
			addresses = nil
			kinds := []string{}
			if adapter.Address != "" {
				kinds = append(kinds, "A")
			}
			if adapter.SourceIPv6 != "" {
				kinds = append(kinds, "AAAA")
			}
			for _, kind := range kinds {
				var answer dnsResolveResult
				if err := s.client.Request(ctx, "dns.resolve", map[string]any{"domain": endpoint.Host, "adapter": adapter.Name, "record_type": kind, "bootstrap": true, "timeout_ms": 1200}, &answer); err == nil {
					addresses = append(addresses, answer.Addresses...)
				}
			}
		}
		port := endpoint.Port
		if port == 0 {
			port = defaultPort
		}
		for _, address := range uniqueNonEmpty(addresses) {
			if usableTUNDNSAddress(adapter, address) {
				upstream := dnsResolveResult{Transport: transport, Server: endpoint.Host + "@" + net.JoinHostPort(address, fmt.Sprint(port)), DoHPath: endpoint.Path}
				if transport == "doh" && strings.ContainsAny(endpoint.Path, "?%") {
					var relay struct {
						Address string `json:"address"`
					}
					relayEndpoint := endpoint
					relayEndpoint.IP = address
					if err := s.client.Request(ctx, "dns.dohRelay", map[string]any{"adapter": adapter.Name, "endpoint": relayEndpoint}, &relay); err != nil {
						return nil, fmt.Errorf("创建自定义 DoH 转发失败：%w", err)
					}
					upstream.RelayAddress = relay.Address
				}
				result = append(result, upstream)
			}
		}
	}
	return orderTUNDNSPool(adapter, config, result)
}

func orderTUNDNSPool(adapter AdapterView, config tunDNSConfiguration, encrypted []dnsResolveResult) ([]dnsResolveResult, error) {
	var result []dnsResolveResult
	appendLegacy := func(servers []string) {
		for _, address := range uniqueNonEmpty(servers) {
			if usableTUNDNSAddress(adapter, address) {
				for _, transport := range []string{"udp", "tcp"} {
					result = append(result, dnsResolveResult{Transport: transport, Server: net.JoinHostPort(address, "53")})
				}
			}
		}
	}
	if config.Policy == "auto" && adapter.Address == "" && len(adapter.DNSServers) > 0 {
		appendLegacy(adapter.DNSServers)
	}
	result = append(result, encrypted...)
	if config.Policy == "auto" || config.Policy == "off" {
		if adapter.Address != "" || config.Policy == "off" {
			appendLegacy(adapter.DNSServers)
		}
		appendLegacy(config.LegacyServers)
	}
	seen := make(map[string]bool)
	unique := result[:0]
	for _, upstream := range result {
		key := upstream.Transport + "\x00" + upstream.Server + "\x00" + upstream.DoHPath
		if !seen[key] {
			seen[key] = true
			unique = append(unique, upstream)
		}
	}
	result = unique
	if len(result) == 0 {
		return nil, fmt.Errorf("当前网卡没有可用的 DNS 上游地址")
	}
	return result, nil
}

func usableTUNDNSAddress(adapter AdapterView, address string) bool {
	ip, err := netip.ParseAddr(address)
	return err == nil && !ip.IsUnspecified() && !ip.IsMulticast() && ((ip.Unmap().Is4() && adapter.Address != "") || (ip.Is6() && !ip.Is4In6() && adapter.SourceIPv6 != ""))
}

// sing-box 1.14 evaluate/respond rules give every query the same ordered
// fallback semantics as the Core. Two encrypted endpoints race per batch;
// traditional DNS is evaluated only after the encrypted batches fail.
func buildTUNDNSPool(adapter AdapterView, options tunConfigOptions) (map[string]any, error) {
	if !options.IPv6Available {
		adapter.SourceIPv6 = ""
	}
	if options.IPv4Unavailable {
		adapter.Address = ""
	}
	var servers, rules []any
	var tags []string
	var encrypted []bool
	for _, result := range options.DNSUpstreams {
		host := result.Server
		if parts := strings.SplitN(host, "@", 2); len(parts) == 2 {
			host = parts[1]
		}
		ip, _, err := splitEndpoint(host, 53)
		if err != nil || !usableTUNDNSAddress(adapter, ip) {
			continue
		}
		upstream, err := buildDNSUpstreamForPolicy(adapter, result, options.DNSPolicy)
		if err != nil {
			return nil, err
		}
		tag := fmt.Sprintf("dns-pool-%d", len(servers))
		upstream["tag"] = tag
		servers = append(servers, upstream)
		tags = append(tags, tag)
		encrypted = append(encrypted, result.Transport == "doh" || result.Transport == "dot")
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("DNS 上游列表不支持当前地址族")
	}
	respond := func(tag string, race bool) []any {
		return []any{
			map[string]any{"match_response": tag, "response_rcode": "NOERROR", "action": "respond", "race": race},
			map[string]any{"match_response": tag, "response_rcode": "NXDOMAIN", "action": "respond", "race": race},
		}
	}
	for start := 0; start < len(tags); {
		if encrypted[start] {
			end := start + 1
			if end < len(tags) && encrypted[end] {
				end++
			}
			for _, tag := range tags[start:end] {
				rules = append(rules, map[string]any{"action": "evaluate", "server": tag, "tag": tag, "timeout": "1s"})
			}
			for _, tag := range tags[start:end] {
				rules = append(rules, respond(tag, true)...)
			}
			start = end
		} else {
			tag := tags[start]
			rules = append(rules, map[string]any{"action": "evaluate", "server": tag, "tag": tag, "timeout": "500ms"})
			rules = append(rules, respond(tag, false)...)
			start++
		}
	}
	rules = append(rules, map[string]any{"action": "predefined", "rcode": "SERVFAIL"})
	return map[string]any{"servers": servers, "rules": rules, "final": servers[0].(map[string]any)["tag"]}, nil
}
