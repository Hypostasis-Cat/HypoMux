package services

import (
	"context"
	"errors"
	"slices"
	"time"
)

type SteamCDNEntry struct {
	Validated             bool    `json:"validated"`
	Preferred             bool    `json:"preferred"`
	TotalBPS              float64 `json:"total_bps"`
	ActiveConnections     int     `json:"active_connections"`
	SuccessfulConnections uint64  `json:"successful_connections"`
	EffectiveBytes        uint64  `json:"effective_bytes"`

	Adapter       string    `json:"adapter"`
	Domain        string    `json:"domain"`
	Port          string    `json:"port"`
	IP            string    `json:"ip"`
	DownloadBPS   float64   `json:"download_bps"`
	Samples       uint64    `json:"samples"`
	Selections    uint64    `json:"selections"`
	CooldownUntil time.Time `json:"cooldown_until"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type SteamCDNDiagnostic struct {
	Domain  string    `json:"domain"`
	Adapter string    `json:"adapter"`
	IP      string    `json:"ip"`
	Stage   string    `json:"stage"`
	At      time.Time `json:"at"`
}
type SteamCDNStatus struct {
	StageCounts           map[string]uint64 `json:"stage_counts"`
	EffectiveReplacements uint64            `json:"effective_replacements"`

	Recognized   uint64               `json:"recognized"`
	Diagnostics  []SteamCDNDiagnostic `json:"diagnostics"`
	Available    bool                 `json:"available"`
	Enabled      bool                 `json:"enabled"`
	Probing      int                  `json:"probing"`
	Replacements uint64               `json:"replacements"`
	Fallbacks    uint64               `json:"fallbacks"`
	Entries      []SteamCDNEntry      `json:"entries"`
}

func (s *EngineService) SteamCDNStatus(reset bool) (SteamCDNStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if reset && s.logs != nil {
		if status, err := s.configureSteamCDNLocked(nil, false); err == nil {
			s.logs.RecordEvent("steam_cdn", "before_reset", map[string]any{"status": status})
		}
	}
	return s.configureSteamCDNLocked(nil, reset)
}

// Keep runtime changes and persistence serialized with lifecycle operations.
// Only this field is persisted, preserving unrelated settings and routing.
func (s *EngineService) SetSteamCDNEnabled(enabled bool) (AppSettings, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.acquireLifecycle(ctx); err != nil {
		return AppSettings{}, err
	}
	defer s.releaseLifecycle()
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.settings.Get().SteamCDNEnabled
	if s.logs != nil {
		if status, e := s.configureSteamCDNLocked(nil, false); e == nil {
			s.logs.RecordEvent("steam_cdn", "before_toggle", map[string]any{"status": status, "next_enabled": enabled})
		}
	}
	if _, err := s.configureSteamCDNLocked(&enabled, false); err != nil {
		return AppSettings{}, err
	}
	s.settings.mu.Lock()
	next := cloneSettings(s.settings.settings)
	next.SteamCDNEnabled = enabled
	err := s.settings.commitLocked(next)
	s.settings.mu.Unlock()
	if err != nil {
		_, _ = s.configureSteamCDNLocked(&previous, false)
		return AppSettings{}, err
	}
	return next, nil
}

func (s *EngineService) configureSteamCDNLocked(enabled *bool, reset bool) (SteamCDNStatus, error) {
	result := SteamCDNStatus{Entries: []SteamCDNEntry{}}
	hello := s.client.Hello()
	if hello.ProtocolVersion == 0 {
		return result, nil
	}
	if !slices.Contains(hello.Capabilities, "steam_cdn.configure") {
		if enabled != nil && *enabled {
			return result, errors.New("当前 Core 不支持 Steam 下载优选，请更新核心")
		}
		return result, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	params := map[string]any{"reset": reset}
	if enabled != nil {
		params["enabled"] = *enabled
	}
	if err := s.client.Request(ctx, "steam_cdn.configure", params, &result); err != nil {
		return result, err
	}
	result.Available = true
	return result, nil
}
