// Package v1 defines the transport DTOs exposed by protocol version 1.
//
// These types are deliberately independent of any UI toolkit. Their JSON
// representation is the public boundary consumed by desktop clients.
package v1

import (
	"runtime"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/diagnostic"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/platform"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
	engineRuntime "github.com/Hypostasis-Cat/HypoMux/engine/internal/runtime"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/tun"
)

const (
	MethodEngineHello     = "engine.hello"
	MethodEngineStatus    = "engine.status"
	MethodEngineStart     = "engine.start"
	MethodEngineStop      = "engine.stop"
	MethodEngineTelemetry = "engine.telemetry"
	MethodTunActivate     = "tun.activate"
	MethodTunStatus       = "tun.status"
	MethodTunDeactivate   = "tun.deactivate"
	MethodDNSResolve      = "dns.resolve"
	MethodDNSStatus       = "dns.status"
	MethodHealthCheck     = "health.check"
	MethodDiagnosticRun   = "diagnostic.run"
	MethodWFPInspect      = "wfp.inspect"
	MethodHostShutdown    = "host.shutdown"

	EventEngineStateChanged  = "engine.state_changed"
	EventDNSFallbackRequired = "dns.fallback_required"
	EventTunStateChanged     = "tun.state_changed"
	EventLogRecord           = "log.record"
	EventHostExiting         = "host.exiting"
)

var capabilities = []string{
	MethodEngineHello,
	MethodEngineStatus,
	MethodEngineStart,
	MethodEngineStop,
	MethodEngineTelemetry,
	MethodTunActivate,
	MethodTunStatus,
	MethodTunDeactivate,
	MethodDNSResolve,
	MethodDNSStatus,
	MethodHealthCheck,
	MethodDiagnosticRun,
	MethodWFPInspect,
	MethodHostShutdown,
}

// Capabilities returns a copy so callers cannot mutate the advertised
// protocol surface.
func Capabilities() []string {
	return append([]string(nil), capabilities...)
}

type HelloResult struct {
	Engine          string              `json:"engine"`
	EngineVersion   string              `json:"engine_version"`
	Commit          string              `json:"commit"`
	ProtocolVersion int                 `json:"protocol_version"`
	Transport       string              `json:"transport"`
	Capabilities    []string            `json:"capabilities"`
	Modes           []string            `json:"modes"`
	ModeFeatures    map[string][]string `json:"mode_features"`
	OS              string              `json:"os"`
	Arch            string              `json:"arch"`
	PID             int                 `json:"pid"`
	Elevated        bool                `json:"elevated"`
	StartedAt       time.Time           `json:"started_at"`
}

func NewHelloResult(
	engine string,
	engineVersion string,
	commit string,
	pid int,
	elevated bool,
	startedAt time.Time,
) HelloResult {
	return HelloResult{
		Engine:          engine,
		EngineVersion:   engineVersion,
		Commit:          commit,
		ProtocolVersion: protocol.Version,
		Transport:       protocol.Transport,
		Capabilities:    Capabilities(),
		Modes:           []string{"proxy", "tun_tcp_pool"},
		ModeFeatures: map[string][]string{
			"proxy": {
				"socks5_connect",
				"http_connect",
				"source_bound_dns",
				"ipv6_egress",
				"adaptive_health",
				"domain_quarantine",
			},
			"tun_tcp_pool": {
				"tcp_connect",
				"udp_associate",
				"ipv6_egress",
				"adaptive_health",
				"managed_tun_lifecycle",
				"dynamic_nic_channels",
			},
		},
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		PID:       pid,
		Elevated:  elevated,
		StartedAt: startedAt,
	}
}

type HealthResult struct {
	OK           bool                `json:"ok"`
	State        engineRuntime.State `json:"state"`
	HostUptimeMS int64               `json:"host_uptime_ms"`
}

type StatusResult struct {
	Engine       engineRuntime.Snapshot `json:"engine"`
	HostUptimeMS int64                  `json:"host_uptime_ms"`
	Proxy        *ProxyStatus           `json:"proxy,omitempty"`
	Tun          *tun.Status            `json:"tun,omitempty"`
}

type ProxyStatus struct {
	Mode      string                  `json:"mode"`
	Running   bool                    `json:"running"`
	Endpoints proxy.Endpoints         `json:"endpoints"`
	Telemetry proxy.TelemetrySnapshot `json:"telemetry"`
}

type DiagnosticRunParams struct {
	SourceIP string `json:"src_ip"`
	TargetIP string `json:"target_ip"`
	Count    int    `json:"count"`
	Timeout  int    `json:"timeout_ms"`
}

type WFPInspectParams struct {
	Repair bool `json:"repair"`
}

type WFPInspectResult = platform.WFPStatus

func (p DiagnosticRunParams) Config() diagnostic.Config {
	return diagnostic.Config{
		SourceIP: p.SourceIP,
		TargetIP: p.TargetIP,
		Count:    p.Count,
		Timeout:  time.Duration(p.Timeout) * time.Millisecond,
	}
}

type EngineStartParams struct {
	Mode                  string                       `json:"mode"`
	ListenHost            string                       `json:"listen_host"`
	SOCKSPort             int                          `json:"socks_port"`
	HTTPPort              int                          `json:"http_port"`
	Weighted              bool                         `json:"weighted"`
	Adapters              []proxy.Adapter              `json:"adapters"`
	Channels              []proxy.Channel              `json:"channels,omitempty"`
	ConnectTimeoutMS      int                          `json:"connect_timeout_ms"`
	DNS                   DNSStartConfig               `json:"dns"`
	DomainIsolation       *bool                        `json:"domain_isolation,omitempty"`
	DomainIsolationExpiry *bool                        `json:"domain_isolation_expiry,omitempty"`
	DomainQuarantines     []proxy.DomainQuarantineSeed `json:"domain_quarantines,omitempty"`
}

type DNSStartConfig struct {
	Policy         string   `json:"policy"`
	LegacyServers  []string `json:"legacy_servers"`
	CacheTTLMS     int      `json:"cache_ttl_ms"`
	QueryTimeoutMS int      `json:"query_timeout_ms"`
}

func (c DNSStartConfig) ResolverConfig() dns.Config {
	return dns.Config{
		Policy:        c.Policy,
		LegacyServers: append([]string(nil), c.LegacyServers...),
		CacheTTL:      time.Duration(c.CacheTTLMS) * time.Millisecond,
		QueryTimeout:  time.Duration(c.QueryTimeoutMS) * time.Millisecond,
	}
}

func (p EngineStartParams) ProxyConfig() proxy.Config {
	domainIsolation := true
	if p.DomainIsolation != nil {
		domainIsolation = *p.DomainIsolation
	}
	domainIsolationExpiry := true
	if p.DomainIsolationExpiry != nil {
		domainIsolationExpiry = *p.DomainIsolationExpiry
	}
	return proxy.Config{
		ListenHost:            p.ListenHost,
		SOCKSPort:             p.SOCKSPort,
		HTTPPort:              p.HTTPPort,
		Weighted:              p.Weighted,
		Adapters:              p.Adapters,
		Channels:              p.Channels,
		ConnectTimeout:        time.Duration(p.ConnectTimeoutMS) * time.Millisecond,
		DNS:                   p.DNS.ResolverConfig(),
		DomainIsolation:       &domainIsolation,
		DomainIsolationExpiry: &domainIsolationExpiry,
		DomainQuarantines:     append([]proxy.DomainQuarantineSeed(nil), p.DomainQuarantines...),
	}
}

type EngineStartResult struct {
	State     engineRuntime.Snapshot `json:"state"`
	Mode      string                 `json:"mode"`
	Endpoints proxy.Endpoints        `json:"endpoints"`
}

type EngineStopResult struct {
	Accepted bool                   `json:"accepted"`
	State    engineRuntime.Snapshot `json:"state"`
}

type EngineTelemetryParams struct {
	IncludeConnections bool `json:"include_connections"`
}

type TunActivateParams struct {
	Executable             string `json:"executable"`
	ConfigPath             string `json:"config_path"`
	IPv4FallbackConfigPath string `json:"ipv4_fallback_config_path,omitempty"`
	StartupTimeoutMS       int    `json:"startup_timeout_ms"`
	StrictRoute            bool   `json:"strict_route"`
}

func (p TunActivateParams) Config() tun.Config {
	return tun.Config{
		Executable:     p.Executable,
		ConfigPath:     p.ConfigPath,
		StartupTimeout: time.Duration(p.StartupTimeoutMS) * time.Millisecond,
	}
}

func (p TunActivateParams) IPv4FallbackConfig() tun.Config {
	return tun.Config{
		Executable:     p.Executable,
		ConfigPath:     p.IPv4FallbackConfigPath,
		StartupTimeout: time.Duration(p.StartupTimeoutMS) * time.Millisecond,
	}
}

type TunLifecycleResult struct {
	Accepted              bool       `json:"accepted"`
	RecoveredStaleAdapter bool       `json:"recovered_stale_adapter,omitempty"`
	IPv4OnlyFallback      bool       `json:"ipv4_only_fallback,omitempty"`
	Tun                   tun.Status `json:"tun"`
}

type LogRecordData struct {
	Component string `json:"component"`
	Message   string `json:"message"`
}

type DNSResolveParams struct {
	Domain     string         `json:"domain"`
	Adapter    string         `json:"adapter"`
	RecordType dns.RecordType `json:"record_type"`
}

type DNSFallbackRequiredData struct {
	Adapter string `json:"adapter"`
	Policy  string `json:"policy"`
	Reason  string `json:"reason"`
}

type ShutdownResult struct {
	Accepted bool `json:"accepted"`
}

type HostExitingData struct {
	Reason string `json:"reason"`
}
