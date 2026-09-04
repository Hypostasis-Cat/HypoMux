package services

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ConnectionView struct {
	ID             uint64    `json:"id"`
	Process        string    `json:"process,omitempty"`
	Protocol       string    `json:"protocol"`
	Client         string    `json:"client,omitempty"`
	Target         string    `json:"target,omitempty"`
	Domain         string    `json:"domain,omitempty"`
	RemoteIP       string    `json:"remote_ip,omitempty"`
	RemotePort     string    `json:"remote_port,omitempty"`
	Adapter        string    `json:"adapter,omitempty"`
	Outbound       string    `json:"outbound"`
	OutboundDetail string    `json:"outbound_detail,omitempty"`
	StartedAt      time.Time `json:"started_at"`
	BytesUp        int64     `json:"bytes_up"`
	BytesDown      int64     `json:"bytes_down"`
}

type ConnectionListSnapshot struct {
	Phase       string           `json:"phase"`
	Mode        string           `json:"mode"`
	SampledAt   time.Time        `json:"sampled_at"`
	Connections []ConnectionView `json:"connections"`
}

func (s *EngineService) Connections() (ConnectionListSnapshot, error) {
	if phase := s.currentTransition(); phase != "" {
		return ConnectionListSnapshot{
			Phase: phase, Mode: s.settings.Get().Mode, SampledAt: time.Now(), Connections: []ConnectionView{},
		}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.client.Ensure(ctx); err != nil {
		return ConnectionListSnapshot{}, err
	}
	var status engineStatusResult
	if err := s.client.Request(ctx, "engine.status", nil, &status); err != nil {
		return ConnectionListSnapshot{}, fmt.Errorf("读取聚合核心状态失败：%w", err)
	}
	result := ConnectionListSnapshot{
		Phase:       status.Engine.State,
		Mode:        s.settings.Get().Mode,
		SampledAt:   time.Now(),
		Connections: []ConnectionView{},
	}
	if status.Engine.State != "running" {
		return result, nil
	}

	var telemetry telemetryResult
	if err := s.client.Request(ctx, "engine.telemetry", map[string]any{"include_connections": true}, &telemetry); err != nil {
		return ConnectionListSnapshot{}, fmt.Errorf("读取活动连接失败：%w", err)
	}
	result.SampledAt = telemetry.SampledAt
	processes := resolveConnectionProcesses(telemetry.Connections)
	s.mu.Lock()
	clashAPI := s.clashAPI
	s.mu.Unlock()
	clashDetails := fetchClashConnectionDetails(ctx, clashAPI, telemetry.Connections)
	for id, details := range clashDetails {
		if details.Process != "" {
			processes[id] = details.Process
		}
	}
	result.Connections = make([]ConnectionView, 0, len(telemetry.Connections))
	for _, item := range telemetry.Connections {
		// SOCKS UDP uses a long-lived TCP control association in addition to
		// one registry entry per actual UDP destination. The association has
		// no target and is transport plumbing, not a user-visible connection.
		if strings.TrimSpace(item.Target) == "" {
			continue
		}
		targetHost, targetPort := splitConnectionEndpoint(item.Target)
		remoteHost, remotePort := splitConnectionEndpoint(item.Remote)
		domain := ""
		if targetHost != "" && net.ParseIP(strings.Trim(targetHost, "[]")) == nil {
			domain = targetHost
		}
		if domain == "" {
			domain = clashDetails[item.ID].Domain
		}
		if remoteHost == "" && net.ParseIP(strings.Trim(targetHost, "[]")) != nil {
			remoteHost = targetHost
		}
		if remotePort == "" {
			remotePort = targetPort
		}
		outbound, detail := connectionOutbound(item.Channel, item.Adapter)
		process := processes[item.ID]
		if process != "" {
			process = filepath.Base(process)
		}
		if strings.EqualFold(process, "sing-box.exe") || strings.EqualFold(process, "hypomux-engine.exe") {
			process = ""
		}
		result.Connections = append(result.Connections, ConnectionView{
			ID: item.ID, Process: process, Protocol: item.Protocol, Client: item.Client,
			Target: item.Target, Domain: domain, RemoteIP: remoteHost, RemotePort: remotePort,
			Adapter: item.Adapter, Outbound: outbound, OutboundDetail: detail,
			StartedAt: item.StartedAt, BytesUp: item.BytesUp, BytesDown: item.BytesDown,
		})
	}
	sortConnectionViews(result.Connections)
	return result, nil
}

func sortConnectionViews(connections []ConnectionView) {
	sort.SliceStable(connections, func(left, right int) bool {
		if connections[left].StartedAt.Equal(connections[right].StartedAt) {
			return connections[left].ID < connections[right].ID
		}
		return connections[left].StartedAt.Before(connections[right].StartedAt)
	})
}

func splitConnectionEndpoint(value string) (string, string) {
	if value == "" {
		return "", ""
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return value, ""
	}
	return strings.Trim(host, "[]"), port
}

func connectionOutbound(channel string, adapter string) (string, string) {
	switch {
	case channel == "" || channel == "aggregation":
		return "aggregation", adapter
	case channel == "direct":
		return "direct", ""
	case strings.HasPrefix(channel, "nic_"):
		if adapter != "" {
			return "adapter", adapter
		}
		return "adapter", strings.TrimPrefix(channel, "nic_")
	default:
		return channel, adapter
	}
}
