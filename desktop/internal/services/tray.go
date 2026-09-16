package services

import (
	"context"
	"time"
)

type TraySnapshot struct {
	Phase string `json:"phase"`
	Mode  string `json:"mode"`
}

// TrayStatus avoids Snapshot's telemetry sampling and throughput bookkeeping.
// Opening a popup must not alter the main window's sampling interval.
func (s *EngineService) TrayStatus() (TraySnapshot, error) {
	result := TraySnapshot{Phase: s.currentTransition(), Mode: s.settings.Get().Mode}
	if result.Phase != "" {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := s.client.Ensure(ctx); err != nil {
		return result, err
	}
	var status engineStatusResult
	if err := s.client.Request(ctx, "engine.status", nil, &status); err != nil {
		return result, err
	}
	result.Phase = status.Engine.State
	return result, nil
}
