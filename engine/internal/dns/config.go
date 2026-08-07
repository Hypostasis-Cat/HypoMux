package dns

import (
	"fmt"
	"net"
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

	DefaultCacheTTL         = 180 * time.Second
	DefaultQueryTimeout     = 4 * time.Second
	DefaultMaxCacheEntries  = 1024
	DefaultFailureThreshold = 3
)

var defaultLegacyServers = []string{"223.5.5.5", "119.29.29.29"}

type Endpoint struct {
	IP   string `json:"ip"`
	Host string `json:"host"`
	Path string `json:"path"`
}

var providerEndpoints = map[string][]Endpoint{
	PolicyAliDNS: {
		{IP: "223.5.5.5", Host: "dns.alidns.com", Path: "/dns-query"},
	},
	PolicyDNSPod: {
		{IP: "1.12.12.12", Host: "doh.pub", Path: "/dns-query"},
		{IP: "120.53.53.53", Host: "doh.pub", Path: "/dns-query"},
	},
	PolicyGoogle: {
		{IP: "8.8.8.8", Host: "dns.google", Path: "/dns-query"},
		{IP: "8.8.4.4", Host: "dns.google", Path: "/dns-query"},
	},
}

type Config struct {
	Policy           string
	LegacyServers    []string
	CacheTTL         time.Duration
	QueryTimeout     time.Duration
	MaxCacheEntries  int
	FailureThreshold int
}

type Binding struct {
	Name       string
	SourceIP   string
	IfIndex    int
	DNSServers []string
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
	case PolicyAuto, PolicyOff, PolicySystem, PolicyAliDNS, PolicyDNSPod, PolicyGoogle:
	default:
		return Config{}, fmt.Errorf("unsupported DNS policy %q", config.Policy)
	}

	servers, err := normalizeIPv4List(config.LegacyServers)
	if err != nil {
		return Config{}, fmt.Errorf("legacy DNS servers: %w", err)
	}
	for _, fallback := range defaultLegacyServers {
		if !contains(servers, fallback) {
			servers = append(servers, fallback)
		}
	}
	config.LegacyServers = servers

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
	if binding.Name == "" {
		return Binding{}, fmt.Errorf("adapter name is required")
	}
	source := net.ParseIP(binding.SourceIP)
	if source == nil || source.To4() == nil {
		return Binding{}, fmt.Errorf("adapter %q has invalid IPv4 source address", binding.Name)
	}
	binding.SourceIP = source.To4().String()
	if binding.IfIndex < 0 {
		return Binding{}, fmt.Errorf("adapter %q has invalid interface index", binding.Name)
	}
	servers, err := normalizeIPv4List(binding.DNSServers)
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

func LegacyServers(config Config, binding Binding) []string {
	result := append([]string(nil), binding.DNSServers...)
	for _, server := range config.LegacyServers {
		if !contains(result, server) {
			result = append(result, server)
		}
	}
	return result
}

func normalizeIPv4List(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		ip := net.ParseIP(text)
		if ip == nil || ip.To4() == nil {
			return nil, fmt.Errorf("%q is not an IPv4 address", text)
		}
		text = ip.To4().String()
		if !contains(result, text) {
			result = append(result, text)
		}
	}
	return result, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
