package services

import (
	"context"
	"errors"
	"slices"
	"time"
)

type SteamCDNEntry struct {
	ProbeBPS         float64   `json:"probe_bps"`
	ProbedAt         time.Time `json:"probed_at"`
	AdmissionReason  string    `json:"admission_reason,omitempty"`
	Source           string    `json:"source,omitempty"`
	EvaluatedAt      time.Time `json:"evaluated_at"`
	SwitchedBytes    uint64    `json:"switched_bytes"`
	OriginalBytes    uint64    `json:"original_bytes"`
	SwitchedBPS      float64   `json:"switched_bps"`
	SwitchedActive   int       `json:"switched_active"`
	TransferFailures uint64    `json:"transfer_failures"`

	DecisionReason        string  `json:"decision_reason,omitempty"`
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
	SpeedProbeBytes int    `json:"speed_probe_bytes"`
	SpeedProbeLimit int    `json:"speed_probe_limit"`
	CoreVersion     string `json:"core_version,omitempty"`
	CoreCommit      string `json:"core_commit,omitempty"`
	ConfiguredMode  string `json:"configured_mode,omitempty"`

	AccountingVersion int       `json:"accounting_version"`
	StartedAt         time.Time `json:"started_at"`
	SampledAt         time.Time `json:"sampled_at"`
	SwitchedBytes     uint64    `json:"switched_bytes"`
	OriginalBytes     uint64    `json:"original_bytes"`
	TransferFailures  uint64    `json:"transfer_failures"`

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
	result.CoreVersion, result.CoreCommit = hello.EngineVersion, hello.Commit
	result.ConfiguredMode = s.settings.Get().Mode
	result.Available = true
	return result, nil
}
