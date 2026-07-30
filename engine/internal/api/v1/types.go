// Package v1 defines the transport DTOs exposed by protocol version 1.
//
// These types are deliberately independent of any UI toolkit. Their JSON
// representation is the public boundary consumed by Python today and by the
// future WPF client.
package v1

import (
	"runtime"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/diagnostic"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
	engineRuntime "github.com/Hypostasis-Cat/HypoMux/engine/internal/runtime"
)

const (
	MethodEngineHello     = "engine.hello"
	MethodEngineStatus    = "engine.status"
	MethodEngineStart     = "engine.start"
	MethodEngineStop      = "engine.stop"
	MethodEngineTelemetry = "engine.telemetry"
	MethodHealthCheck     = "health.check"
	MethodDiagnosticRun   = "diagnostic.run"
	MethodHostShutdown    = "host.shutdown"

	EventEngineStateChanged = "engine.state_changed"
	EventHostExiting        = "host.exiting"
)

var capabilities = []string{
	MethodEngineHello,
	MethodEngineStatus,
	MethodEngineStart,
	MethodEngineStop,
	MethodEngineTelemetry,
	MethodHealthCheck,
	MethodDiagnosticRun,
	MethodHostShutdown,
}

// Capabilities returns a copy so callers cannot mutate the advertised
// protocol surface.
func Capabilities() []string {
	return append([]string(nil), capabilities...)
}

type HelloResult struct {
	Engine          string    `json:"engine"`
	EngineVersion   string    `json:"engine_version"`
	Commit          string    `json:"commit"`
	ProtocolVersion int       `json:"protocol_version"`
	Transport       string    `json:"transport"`
	Capabilities    []string  `json:"capabilities"`
	OS              string    `json:"os"`
	Arch            string    `json:"arch"`
	PID             int       `json:"pid"`
	Elevated        bool      `json:"elevated"`
	StartedAt       time.Time `json:"started_at"`
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
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		PID:             pid,
		Elevated:        elevated,
		StartedAt:       startedAt,
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
}

type ProxyStatus struct {
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

func (p DiagnosticRunParams) Config() diagnostic.Config {
	return diagnostic.Config{
		SourceIP: p.SourceIP,
		TargetIP: p.TargetIP,
		Count:    p.Count,
		Timeout:  time.Duration(p.Timeout) * time.Millisecond,
	}
}

type EngineStartParams struct {
	Mode             string          `json:"mode"`
	ListenHost       string          `json:"listen_host"`
	SOCKSPort        int             `json:"socks_port"`
	HTTPPort         int             `json:"http_port"`
	Weighted         bool            `json:"weighted"`
	Adapters         []proxy.Adapter `json:"adapters"`
	ConnectTimeoutMS int             `json:"connect_timeout_ms"`
}

func (p EngineStartParams) ProxyConfig() proxy.Config {
	return proxy.Config{
		ListenHost:     p.ListenHost,
		SOCKSPort:      p.SOCKSPort,
		HTTPPort:       p.HTTPPort,
		Weighted:       p.Weighted,
		Adapters:       p.Adapters,
		ConnectTimeout: time.Duration(p.ConnectTimeoutMS) * time.Millisecond,
	}
}

type EngineStartResult struct {
	State     engineRuntime.Snapshot `json:"state"`
	Endpoints proxy.Endpoints        `json:"endpoints"`
}

type EngineStopResult struct {
	Accepted bool                   `json:"accepted"`
	State    engineRuntime.Snapshot `json:"state"`
}

type EngineTelemetryParams struct {
	IncludeConnections bool `json:"include_connections"`
}

type ShutdownResult struct {
	Accepted bool `json:"accepted"`
}

type HostExitingData struct {
	Reason string `json:"reason"`
}
