package proxy

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

const (
	DefaultListenHost = "127.0.0.1"
	DefaultSOCKSPort  = 10800
	DefaultHTTPPort   = 10801
)

type Adapter struct {
	Name       string   `json:"name"`
	SourceIP   string   `json:"source_ip"`
	IfIndex    int      `json:"if_index,omitempty"`
	Weight     int      `json:"weight,omitempty"`
	DNSServers []string `json:"dns_servers,omitempty"`
}

type Config struct {
	ListenHost     string        `json:"listen_host"`
	SOCKSPort      int           `json:"socks_port"`
	HTTPPort       int           `json:"http_port"`
	Weighted       bool          `json:"weighted"`
	Adapters       []Adapter     `json:"adapters"`
	ConnectTimeout time.Duration `json:"-"`
	DNS            dns.Config    `json:"-"`
}

type Endpoints struct {
	SOCKS string `json:"socks"`
	HTTP  string `json:"http"`
}

func normalizeConfig(config Config) (Config, error) {
	config.ListenHost = strings.TrimSpace(config.ListenHost)
	if config.ListenHost == "" {
		config.ListenHost = DefaultListenHost
	}
	if ip := net.ParseIP(config.ListenHost); ip == nil || !ip.IsLoopback() {
		return Config{}, fmt.Errorf("listen_host must be a loopback IP address")
	}
	if config.SOCKSPort < 0 || config.SOCKSPort > 65535 {
		return Config{}, fmt.Errorf("invalid SOCKS port %d", config.SOCKSPort)
	}
	if config.HTTPPort < 0 || config.HTTPPort > 65535 {
		return Config{}, fmt.Errorf("invalid HTTP port %d", config.HTTPPort)
	}
	if config.SOCKSPort == 0 && config.HTTPPort != 0 || config.HTTPPort == 0 && config.SOCKSPort != 0 {
		return Config{}, fmt.Errorf("SOCKS and HTTP ports must both be zero for automatic allocation")
	}
	if config.SOCKSPort != 0 && config.SOCKSPort == config.HTTPPort {
		return Config{}, fmt.Errorf("SOCKS and HTTP ports must differ")
	}
	if len(config.Adapters) == 0 {
		return Config{}, fmt.Errorf("at least one adapter is required")
	}
	if len(config.Adapters) > 64 {
		return Config{}, fmt.Errorf("too many adapters")
	}

	seenNames := make(map[string]struct{}, len(config.Adapters))
	seenIPs := make(map[string]struct{}, len(config.Adapters))
	adapters := make([]Adapter, 0, len(config.Adapters))
	for _, adapter := range config.Adapters {
		adapter.Name = strings.TrimSpace(adapter.Name)
		adapter.SourceIP = strings.TrimSpace(adapter.SourceIP)
		if adapter.Name == "" {
			return Config{}, fmt.Errorf("adapter name is required")
		}
		ip := net.ParseIP(adapter.SourceIP)
		if ip == nil || ip.To4() == nil {
			return Config{}, fmt.Errorf("adapter %q has invalid IPv4 source address", adapter.Name)
		}
		adapter.SourceIP = ip.To4().String()
		if adapter.IfIndex < 0 {
			return Config{}, fmt.Errorf("adapter %q has invalid interface index", adapter.Name)
		}
		binding, err := dns.NormalizeBinding(dns.Binding{
			Name:       adapter.Name,
			SourceIP:   adapter.SourceIP,
			IfIndex:    adapter.IfIndex,
			DNSServers: adapter.DNSServers,
		})
		if err != nil {
			return Config{}, err
		}
		adapter.DNSServers = binding.DNSServers
		if adapter.Weight <= 0 {
			adapter.Weight = 1
		}
		if _, exists := seenNames[adapter.Name]; exists {
			return Config{}, fmt.Errorf("duplicate adapter name %q", adapter.Name)
		}
		if _, exists := seenIPs[adapter.SourceIP]; exists {
			return Config{}, fmt.Errorf("duplicate adapter source IP %q", adapter.SourceIP)
		}
		seenNames[adapter.Name] = struct{}{}
		seenIPs[adapter.SourceIP] = struct{}{}
		adapters = append(adapters, adapter)
	}
	config.Adapters = adapters
	if config.ConnectTimeout <= 0 {
		config.ConnectTimeout = 6 * time.Second
	} else if config.ConnectTimeout > 30*time.Second {
		config.ConnectTimeout = 30 * time.Second
	}
	dnsConfig, err := dns.NormalizeConfig(config.DNS)
	if err != nil {
		return Config{}, err
	}
	config.DNS = dnsConfig
	return config, nil
}
