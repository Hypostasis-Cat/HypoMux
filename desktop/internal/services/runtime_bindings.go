package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

func (s *EngineService) availableRuntimeAdapters() ([]AdapterView, error) {
	if s.runtimeAdapters != nil {
		return s.runtimeAdapters()
	}
	return s.adapters.List()
}

func cloneRuntimeBindings(adapters []AdapterView) []AdapterView {
	result := append([]AdapterView(nil), adapters...)
	for i := range result {
		result[i].DNSServers = append([]string(nil), result[i].DNSServers...)
	}
	return result
}

// Track explicit NIC channels as well as the aggregation pool and TUN DNS
// uplink. An unrelated interface or route change cannot trigger a restart.
func (s *EngineService) rememberRuntimeBindingsLocked(mode string, selected []AdapterView, dns AdapterView) {
	s.runtimeBindings = cloneRuntimeBindings(selected)
	if mode == "tun" && dns.ID != "" {
		s.addRuntimeBindingsLocked([]AdapterView{dns})
	}
	s.runtimeBindingMode = mode
	s.runtimeBindingEnabled = true
	s.runtimeBindingNotice = ""
	s.lastBindingCheck = time.Now()
}

func (s *EngineService) addRuntimeBindingsLocked(adapters []AdapterView) {
	for _, adapter := range adapters {
		found := false
		for _, previous := range s.runtimeBindings {
			found = found || previous.ID == adapter.ID
		}
		if !found {
			s.runtimeBindings = append(s.runtimeBindings, cloneRuntimeBindings([]AdapterView{adapter})[0])
		}
	}
}

func (s *EngineService) rememberAdditionalRuntimeBindings(adapters []AdapterView) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeBindingEnabled {
		// Do not replace an old explicit-channel binding with a new pool
		// binding: the subsequent scan must still notice that it is stale.
		s.addRuntimeBindingsLocked(adapters)
	}
}

func runtimeBindingsChanged(previous, available []AdapterView) (bool, error) {
	current := make(map[string]AdapterView, len(available))
	for _, adapter := range available {
		current[adapter.ID] = adapter
	}
	changed := false
	var missing error
	for _, old := range previous {
		next, ok := current[old.ID]
		if !ok || !next.Operational || (next.Address == "" && next.SourceIPv6 == "") {
			// During suspend/disconnect, preserve other working NICs. Rebuild
			// only after the selected interfaces have usable OS bindings again.
			missing = fmt.Errorf("等待网卡 %s 恢复可用地址", old.Name)
			continue
		}
		changed = changed || old.Name != next.Name || old.Address != next.Address ||
			old.SourceIPv6 != next.SourceIPv6 || old.IfIndex != next.IfIndex ||
			old.IPv6IfIndex != next.IPv6IfIndex || !slices.Equal(old.DNSServers, next.DNSServers)
	}
	if !changed && missing != nil {
		return false, missing
	}
	return changed, nil
}

// Explicit Start/Stop wins over a pending automatic recovery. In particular,
// a late scan may never restart aggregation after the user pressed Stop.
func (s *EngineService) cancelRuntimeBindingRefresh() {
	s.mu.Lock()
	s.runtimeBindingEnabled = false
	cancel := s.runtimeBindingCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *EngineService) scheduleRuntimeBindingRefresh() {
	s.mu.Lock()
	if !s.runtimeBindingEnabled || s.closing || s.runtimeBindingRefresh ||
		len(s.runtimeBindings) == 0 || time.Since(s.lastBindingCheck) < 5*time.Second {
		s.mu.Unlock()
		return
	}
	select {
	case s.lifecycleGate <- struct{}{}:
	default:
		s.mu.Unlock()
		return
	}
	// Include cleanup and restoration of an already-owned hotspot. Stop can
	// cancel this background transaction rather than wait for its deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	s.runtimeBindingRefresh, s.runtimeBindingCancel = true, cancel
	s.lastBindingCheck = time.Now()
	s.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			s.mu.Lock()
			s.runtimeBindingRefresh, s.runtimeBindingCancel = false, nil
			s.mu.Unlock()
			s.releaseLifecycle()
		}()
		if err := s.refreshRuntimeBindings(ctx, s.runtimeBindingActions()); err != nil && !errors.Is(err, context.Canceled) {
			s.setRuntimeBindingNotice("提示：自动更新网卡绑定未完成："+err.Error(), "refresh_failed")
		}
	}()
}

type runtimeBindingActions struct {
	status  func(context.Context) (string, error)
	stop    func(context.Context) error
	start   func(context.Context, string) error
	hotspot func(context.Context, HotspotConfig) error
}

func (s *EngineService) runtimeBindingActions() runtimeBindingActions {
	return runtimeBindingActions{
		status: func(ctx context.Context) (string, error) {
			var state engineStatusResult
			err := s.client.Request(ctx, "engine.status", nil, &state)
			return state.Engine.State, err
		},
		stop:  func(ctx context.Context) error { _, err := s.stopLocked(ctx); return err },
		start: func(ctx context.Context, mode string) error { _, err := s.startLocked(ctx, mode); return err },
		hotspot: func(ctx context.Context, config HotspotConfig) error {
			_, err := s.startHotspotLocked(ctx, config)
			return err
		},
	}
}

// lifecycleGate is held from the fresh OS scan through resource cleanup and
// activation. A full owned-session restart retires explicit channels, DNS/DoH
// pools, NAT64 cache, WFP rules and the sidecar's DNS source binding together.
func (s *EngineService) refreshRuntimeBindings(ctx context.Context, actions runtimeBindingActions) error {
	s.mu.Lock()
	previous, mode := cloneRuntimeBindings(s.runtimeBindings), s.runtimeBindingMode
	closing := s.closing
	hotspot := s.hotspot
	s.mu.Unlock()
	if closing || len(previous) == 0 {
		return nil
	}
	available, err := s.availableRuntimeAdapters()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	changed, err := runtimeBindingsChanged(previous, available)
	if err != nil {
		s.setRuntimeBindingNotice("提示："+err.Error(), "waiting_for_adapter")
		return nil
	}
	if !changed {
		s.setRuntimeBindingNotice("", "")
		return nil
	}
	state, err := actions.status(ctx)
	if err != nil {
		return err
	}
	if state != "running" && state != "degraded" {
		return nil
	}
	resumeHotspot := hotspot != nil && hotspot.snapshot().State == "running"
	if s.logs != nil {
		s.logs.RecordEvent("adapter_binding", "refresh_requested", map[string]any{"mode": mode})
	}
	if err := actions.stop(ctx); err != nil {
		return fmt.Errorf("清理旧绑定失败：%w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := actions.start(ctx, mode); err != nil {
		return fmt.Errorf("建立新绑定失败：%w", err)
	}
	if resumeHotspot {
		if err := actions.hotspot(ctx, hotspot.config); err != nil {
			return fmt.Errorf("聚合已恢复，恢复原热点失败：%w", err)
		}
	}
	s.setRuntimeBindingNotice("", "")
	if s.logs != nil {
		s.logs.RecordEvent("adapter_binding", "refresh_completed", map[string]any{"mode": mode})
	}
	return nil
}

func (s *EngineService) setRuntimeBindingNotice(notice, event string) {
	s.mu.Lock()
	changed := s.runtimeBindingNotice != notice
	s.runtimeBindingNotice = notice
	s.mu.Unlock()
	if changed && event != "" && s.logs != nil {
		s.logs.RecordEvent("adapter_binding", event, map[string]any{"message": notice})
	}
}
