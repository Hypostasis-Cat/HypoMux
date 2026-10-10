package dns

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	PolicyAuto = "auto"
	PolicyOff  = "off"
	// PolicySystem is the explicit native-DNS/no-DoH mode. The TUN sidecar maps
	// it to sing-box's local resolver; the Go engine keeps its source-bound
	// traditional DNS path and never enables DoH for this policy.
	PolicySystem = "system"
	PolicyAliDNS = "alidns"
	PolicyDNSPod = "dnspod"
	PolicyGoogle = "google"
	PolicyCustom = "custom"
	PolicyDoT    = "dot"

	DefaultCacheTTL         = 180 * time.Second
	DefaultQueryTimeout     = 4 * time.Second
	DefaultMaxCacheEntries  = 1024
	DefaultFailureThreshold = 3
)

var defaultLegacyServers = []string{"223.5.5.5", "119.29.29.29", "2400:3200::1", "2001:4860:4860::8888"}

type Endpoint struct {
	IP   string `json:"ip"`
	Host string `json:"host"`
	Path string `json:"path"`
	Port int    `json:"port,omitempty"`
}

func (e Endpoint) port() string {
	if e.Port == 0 {
		return "443"
	}
	return strconv.Itoa(e.Port)
}

func (e Endpoint) authority() string {
	if e.Port != 0 && e.Port != 443 {
		return net.JoinHostPort(e.Host, e.port())
	}
	if strings.Contains(e.Host, ":") {
		return "[" + e.Host + "]"
	}
	return e.Host
}

func parseDoHURL(value string) (Endpoint, string, error) {
	value = strings.TrimSpace(value)
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || len(value) > 2048 {
		return Endpoint{}, "", fmt.Errorf("invalid DoH URL: use an HTTPS URL without credentials or a fragment")
	}
	port := 443
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
	}
	if err != nil || port < 1 || port > 65535 || strings.HasSuffix(u.Host, ":") {
		return Endpoint{}, "", fmt.Errorf("invalid DoH port")
	}
	host := strings.ToLower(u.Hostname())
	endpoint := Endpoint{Host: host, Port: port}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			return Endpoint{}, "", fmt.Errorf("invalid DoH IP address")
		}
		endpoint.IP = ip.String()
	} else if _, err := normalizeDomain(host); err != nil {
		return Endpoint{}, "", fmt.Errorf("invalid DoH hostname: %w", err)
	}
	if u.Path == "" {
		u.Path = "/dns-query"
	}
	endpoint.Path = u.RequestURI()
	u.Host = endpoint.authority()
	return endpoint, u.String(), nil
}

var providerEndpoints = map[string][]Endpoint{
	PolicyAliDNS: {
		{IP: "223.5.5.5", Host: "dns.alidns.com", Path: "/dns-query"},
		{IP: "2400:3200::1", Host: "dns.alidns.com", Path: "/dns-query"},
	},
	PolicyDNSPod: {
		{IP: "1.12.12.12", Host: "doh.pub", Path: "/dns-query"},
		{IP: "120.53.53.53", Host: "doh.pub", Path: "/dns-query"},
	},
	PolicyGoogle: {
		{IP: "8.8.8.8", Host: "dns.google", Path: "/dns-query"},
		{IP: "8.8.4.4", Host: "dns.google", Path: "/dns-query"},
		{IP: "2001:4860:4860::8888", Host: "dns.google", Path: "/dns-query"},
		{IP: "2001:4860:4860::8844", Host: "dns.google", Path: "/dns-query"},
	},
}

type Config struct {
	Policy           string
	LegacyServers    []string
	DoHServers       []string
	DoTServers       []string
	CacheTTL         time.Duration
	QueryTimeout     time.Duration
	MaxCacheEntries  int
	FailureThreshold int
}

type Binding struct {
	Name        string
	SourceIP    string
	IfIndex     int
	SourceIPv6  string
	IPv6IfIndex int
	DNSServers  []string
}

func DefaultConfig() Config {
	return Config{
		Policy:           PolicyAuto,
		LegacyServers:    append([]string(nil), defaultLegacyServers...),
		CacheTTL:         DefaultCacheTTL,
		QueryTimeout:     DefaultQueryTimeout,
		MaxCacheEntries:  DefaultMaxCacheEntries,
		FailureThreshold: DefaultFailureThreshold,
	}
}

func NormalizeConfig(config Config) (Config, error) {
	config.Policy = strings.ToLower(strings.TrimSpace(config.Policy))
	if config.Policy == "" {
		config.Policy = PolicyAuto
	}
	switch config.Policy {
	case PolicyAuto, PolicyOff, PolicySystem, PolicyAliDNS, PolicyDNSPod, PolicyGoogle, PolicyCustom, PolicyDoT:
	default:
		return Config{}, fmt.Errorf("unsupported DNS policy %q", config.Policy)
	}

	servers, err := normalizeIPList(config.LegacyServers, nil)
	if err != nil {
		return Config{}, fmt.Errorf("legacy DNS servers: %w", err)
	}
	for _, fallback := range defaultLegacyServers {
		if !contains(servers, fallback) {
			servers = append(servers, fallback)
		}
	}
	config.LegacyServers = servers
	if len(config.DoHServers) > 16 {
		return Config{}, fmt.Errorf("at most 16 DoH servers are supported")
	}
	var dohServers []string
	for _, value := range config.DoHServers {
		_, normalized, err := parseDoHURL(value)
		if err != nil {
			return Config{}, fmt.Errorf("custom DoH: %w", err)
		}
		if !contains(dohServers, normalized) {
			dohServers = append(dohServers, normalized)
		}
	}
	config.DoHServers = dohServers
	if config.Policy == PolicyCustom && len(dohServers) == 0 {
		return Config{}, fmt.Errorf("custom DoH requires at least one HTTPS URL")
	}
	if len(config.DoTServers) > 16 {
		return Config{}, fmt.Errorf("at most 16 DoT servers are supported")
	}
	var dotServers []string
	for _, value := range config.DoTServers {
		_, normalized, err := parseDoTURL(value)
		if err != nil {
			return Config{}, fmt.Errorf("custom DoT: %w", err)
		}
		if !contains(dotServers, normalized) {
			dotServers = append(dotServers, normalized)
		}
	}
	config.DoTServers = dotServers
	if config.Policy == PolicyDoT && len(dotServers) == 0 {
		return Config{}, fmt.Errorf("DoT requires at least one TLS URL")
	}

	if config.CacheTTL <= 0 {
		config.CacheTTL = DefaultCacheTTL
	} else if config.CacheTTL > 24*time.Hour {
		config.CacheTTL = 24 * time.Hour
	}
	if config.QueryTimeout <= 0 {
		config.QueryTimeout = DefaultQueryTimeout
	} else if config.QueryTimeout > 30*time.Second {
		config.QueryTimeout = 30 * time.Second
	}
	if config.MaxCacheEntries <= 0 {
		config.MaxCacheEntries = DefaultMaxCacheEntries
	} else if config.MaxCacheEntries > 65536 {
		config.MaxCacheEntries = 65536
	}
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = DefaultFailureThreshold
	} else if config.FailureThreshold > 100 {
		config.FailureThreshold = 100
	}
	return config, nil
}

func NormalizeBinding(binding Binding) (Binding, error) {
	binding.Name = strings.TrimSpace(binding.Name)
	binding.SourceIP = strings.TrimSpace(binding.SourceIP)
	binding.SourceIPv6 = strings.TrimSpace(binding.SourceIPv6)
	if binding.Name == "" {
		return Binding{}, fmt.Errorf("adapter name is required")
	}
	if binding.SourceIP != "" {
		source := net.ParseIP(binding.SourceIP)
		if source == nil || source.To4() == nil || source.IsUnspecified() || source.IsMulticast() {
			return Binding{}, fmt.Errorf("adapter %q has invalid IPv4 source address", binding.Name)
		}
		binding.SourceIP = source.To4().String()
	}
	if binding.SourceIPv6 != "" {
		source := net.ParseIP(binding.SourceIPv6)
		if source == nil || source.To4() != nil || source.IsUnspecified() || source.IsMulticast() || source.IsLinkLocalUnicast() {
			return Binding{}, fmt.Errorf("adapter %q has invalid IPv6 source address", binding.Name)
		}
		binding.SourceIPv6 = source.String()
	}
	if binding.SourceIP == "" && binding.SourceIPv6 == "" {
		return Binding{}, fmt.Errorf("adapter %q requires an IPv4 or IPv6 source address", binding.Name)
	}
	if binding.IfIndex < 0 || binding.IPv6IfIndex < 0 {
		return Binding{}, fmt.Errorf("adapter %q has invalid interface index", binding.Name)
	}
	if binding.SourceIPv6 != "" && binding.IPv6IfIndex == 0 {
		binding.IPv6IfIndex = binding.IfIndex
	}
	servers, err := normalizeIPList(binding.DNSServers, &binding)
	if err != nil {
		return Binding{}, fmt.Errorf("adapter %q DNS servers: %w", binding.Name, err)
	}
	binding.DNSServers = servers
	return binding, nil
}

func Endpoints(policy string) []Endpoint {
	if policy == PolicyAuto {
		var result []Endpoint
		for _, provider := range []string{PolicyAliDNS, PolicyDNSPod, PolicyGoogle} {
			result = append(result, providerEndpoints[provider]...)
		}
		return result
	}
	return append([]Endpoint(nil), providerEndpoints[policy]...)
}

func ConfigEndpoints(config Config) []Endpoint {
	if (config.Policy == PolicyAuto || config.Policy == PolicyCustom) && len(config.DoHServers) > 0 {
		result := make([]Endpoint, 0, len(config.DoHServers))
		for _, value := range config.DoHServers {
			endpoint, _, err := parseDoHURL(value)
			if err == nil {
				result = append(result, endpoint)
			}
		}
		return result
	}
	return Endpoints(config.Policy)
}

func boundEndpoints(policy string, binding Binding) []Endpoint {
	var endpoints []Endpoint
	for _, endpoint := range Endpoints(policy) {
		if supportsEndpoint(binding, endpoint.IP) {
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints
}

func LegacyServers(config Config, binding Binding) []string {
	result := append([]string(nil), binding.DNSServers...)
	for _, server := range config.LegacyServers {
		if !contains(result, server) {
			result = append(result, server)
		}
	}
	filtered := result[:0]
	for _, server := range result {
		if supportsEndpoint(binding, server) {
			filtered = append(filtered, server)
		}
	}
	return filtered
}

func normalizeIPv4List(values []string) ([]string, error) {
	return normalizeIPList(values, nil)
}

func normalizeIPList(values []string, binding *Binding) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		ip, err := netip.ParseAddr(text)
		if err != nil || ip.IsUnspecified() || ip.IsMulticast() {
			return nil, fmt.Errorf("%q is not a usable DNS IP address", text)
		}
		ip = ip.Unmap()
		if ip.IsLinkLocalUnicast() {
			if !ip.Is6() {
				return nil, fmt.Errorf("link-local IPv4 DNS is unsupported")
			}
			if binding != nil {
				zone := strconv.Itoa(binding.IPv6IfIndex)
				if binding.IPv6IfIndex <= 0 {
					return nil, fmt.Errorf("link-local DNS requires an IPv6 interface index")
				}
				if ip.Zone() != "" && ip.Zone() != zone && ip.Zone() != binding.Name {
					return nil, fmt.Errorf("DNS scope does not match adapter %q", binding.Name)
				}
				ip = ip.WithZone(zone)
			} else if ip.Zone() == "" {
				return nil, fmt.Errorf("link-local DNS requires an interface scope")
			}
		} else if ip.Zone() != "" {
			return nil, fmt.Errorf("global DNS address cannot have an interface scope")
		}
		text = ip.String()
		if !contains(result, text) {
			result = append(result, text)
		}
	}
	return result, nil
}

func supportsEndpoint(binding Binding, endpoint string) bool {
	ip, err := netip.ParseAddr(endpoint)
	if err == nil && ip.Is6() && ip.IsLinkLocalUnicast() {
		return binding.SourceIPv6 != "" && binding.IPv6IfIndex > 0 && (ip.Zone() == strconv.Itoa(binding.IPv6IfIndex) || ip.Zone() == binding.Name)
	}
	return err == nil && ((ip.Unmap().Is4() && binding.SourceIP != "") || (ip.Is6() && !ip.Is4In6() && binding.SourceIPv6 != ""))
}

func endpointNetwork(transport, endpoint string) string {
	ip, err := netip.ParseAddr(endpoint)
	if err == nil && ip.Unmap().Is4() {
		return transport + "4"
	}
	return transport + "6"
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
