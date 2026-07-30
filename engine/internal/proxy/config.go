package proxy

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

const (
	DefaultListenHost  = "127.0.0.1"
	DefaultSOCKSPort   = 10800
	DefaultHTTPPort    = 10801
	ChannelEthernet    = "nic_ethernet"
	ChannelWiFi        = "nic_wifi"
	ChannelAggregation = "aggregation"
)

type Adapter struct {
	Name        string   `json:"name"`
	SourceIP    string   `json:"source_ip"`
	IfIndex     int      `json:"if_index,omitempty"`
	SourceIPv6  string   `json:"source_ipv6,omitempty"`
	IPv6IfIndex int      `json:"ipv6_if_index,omitempty"`
	Weight      int      `json:"weight,omitempty"`
	DNSServers  []string `json:"dns_servers,omitempty"`
}

type Channel struct {
	Name         string   `json:"name"`
	Port         int      `json:"port"`
	AdapterNames []string `json:"adapter_names"`
}

type Config struct {
	ListenHost     string        `json:"listen_host"`
	SOCKSPort      int           `json:"socks_port"`
	HTTPPort       int           `json:"http_port"`
	Weighted       bool          `json:"weighted"`
	Adapters       []Adapter     `json:"adapters"`
	Channels       []Channel     `json:"channels,omitempty"`
	ConnectTimeout time.Duration `json:"-"`
	DNS            dns.Config    `json:"-"`
}

type Endpoints struct {
	SOCKS    string            `json:"socks,omitempty"`
	HTTP     string            `json:"http,omitempty"`
	Channels map[string]string `json:"channels,omitempty"`
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
	if len(config.Channels) == 0 {
		if config.SOCKSPort == 0 && config.HTTPPort != 0 || config.HTTPPort == 0 && config.SOCKSPort != 0 {
			return Config{}, fmt.Errorf("SOCKS and HTTP ports must both be zero for automatic allocation")
		}
		if config.SOCKSPort != 0 && config.SOCKSPort == config.HTTPPort {
			return Config{}, fmt.Errorf("SOCKS and HTTP ports must differ")
		}
	} else if config.SOCKSPort != 0 || config.HTTPPort != 0 {
		return Config{}, fmt.Errorf("channel pool cannot also configure ordinary SOCKS or HTTP ports")
	}
	if len(config.Adapters) == 0 {
		return Config{}, fmt.Errorf("at least one adapter is required")
	}
	if len(config.Adapters) > 64 {
		return Config{}, fmt.Errorf("too many adapters")
	}

	seenNames := make(map[string]struct{}, len(config.Adapters))
	seenIPs := make(map[string]struct{}, len(config.Adapters))
	seenIPv6 := make(map[string]struct{}, len(config.Adapters))
	adapters := make([]Adapter, 0, len(config.Adapters))
	for _, adapter := range config.Adapters {
		adapter.Name = strings.TrimSpace(adapter.Name)
		adapter.SourceIP = strings.TrimSpace(adapter.SourceIP)
		adapter.SourceIPv6 = strings.TrimSpace(adapter.SourceIPv6)
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
		if adapter.IPv6IfIndex < 0 {
			return Config{}, fmt.Errorf("adapter %q has invalid IPv6 interface index", adapter.Name)
		}
		if adapter.SourceIPv6 != "" {
			ipv6 := net.ParseIP(adapter.SourceIPv6)
			if ipv6 == nil || ipv6.To4() != nil {
				return Config{}, fmt.Errorf(
					"adapter %q has invalid IPv6 source address",
					adapter.Name,
				)
			}
			if ipv6.IsUnspecified() || ipv6.IsMulticast() || ipv6.IsLinkLocalUnicast() {
				return Config{}, fmt.Errorf(
					"adapter %q has unusable IPv6 source address",
					adapter.Name,
				)
			}
			adapter.SourceIPv6 = ipv6.String()
			if adapter.IPv6IfIndex == 0 {
				adapter.IPv6IfIndex = adapter.IfIndex
			}
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
		if adapter.SourceIPv6 != "" {
			if _, exists := seenIPv6[adapter.SourceIPv6]; exists {
				return Config{}, fmt.Errorf(
					"duplicate adapter source IPv6 %q",
					adapter.SourceIPv6,
				)
			}
			seenIPv6[adapter.SourceIPv6] = struct{}{}
		}
		seenNames[adapter.Name] = struct{}{}
		seenIPs[adapter.SourceIP] = struct{}{}
		adapters = append(adapters, adapter)
	}
	config.Adapters = adapters
	if err := normalizeChannels(&config); err != nil {
		return Config{}, err
	}
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

func normalizeChannels(config *Config) error {
	if len(config.Channels) == 0 {
		return nil
	}
	if len(config.Channels) != 3 {
		return fmt.Errorf("TUN TCP pool requires exactly three channels")
	}
	requiredChannels := map[string]bool{
		ChannelEthernet:    false,
		ChannelWiFi:        false,
		ChannelAggregation: false,
	}
	knownAdapters := make(map[string]struct{}, len(config.Adapters))
	for _, adapter := range config.Adapters {
		knownAdapters[adapter.Name] = struct{}{}
	}
	seenNames := make(map[string]struct{}, len(config.Channels))
	seenPorts := make(map[int]struct{}, len(config.Channels))
	channels := make([]Channel, 0, len(config.Channels))
	for _, channel := range config.Channels {
		channel.Name = strings.TrimSpace(channel.Name)
		if channel.Name == "" {
			return fmt.Errorf("channel name is required")
		}
		if _, required := requiredChannels[channel.Name]; !required {
			return fmt.Errorf("unsupported channel name %q", channel.Name)
		}
		if _, exists := seenNames[channel.Name]; exists {
			return fmt.Errorf("duplicate channel name %q", channel.Name)
		}
		if channel.Port < 0 || channel.Port > 65535 {
			return fmt.Errorf("channel %q has invalid port %d", channel.Name, channel.Port)
		}
		if channel.Port != 0 {
			if _, exists := seenPorts[channel.Port]; exists {
				return fmt.Errorf("duplicate channel port %d", channel.Port)
			}
			seenPorts[channel.Port] = struct{}{}
		}
		if len(channel.AdapterNames) == 0 {
			return fmt.Errorf("channel %q requires at least one adapter", channel.Name)
		}
		seenAdapters := make(map[string]struct{}, len(channel.AdapterNames))
		names := make([]string, 0, len(channel.AdapterNames))
		for _, rawName := range channel.AdapterNames {
			name := strings.TrimSpace(rawName)
			if _, exists := knownAdapters[name]; !exists {
				return fmt.Errorf("channel %q references unknown adapter %q", channel.Name, name)
			}
			if _, duplicate := seenAdapters[name]; duplicate {
				return fmt.Errorf("channel %q repeats adapter %q", channel.Name, name)
			}
			seenAdapters[name] = struct{}{}
			names = append(names, name)
		}
		channel.AdapterNames = names
		channels = append(channels, channel)
		seenNames[channel.Name] = struct{}{}
		requiredChannels[channel.Name] = true
	}
	for name, present := range requiredChannels {
		if !present {
			return fmt.Errorf("missing required channel %q", name)
		}
	}
	config.Channels = channels
	return nil
}

func adaptersForChannel(adapters []Adapter, channel Channel) []Adapter {
	allowed := make(map[string]struct{}, len(channel.AdapterNames))
	for _, name := range channel.AdapterNames {
		allowed[name] = struct{}{}
	}
	result := make([]Adapter, 0, len(allowed))
	for _, adapter := range adapters {
		if _, ok := allowed[adapter.Name]; ok {
			result = append(result, adapter)
		}
	}
	return result
}
