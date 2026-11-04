package services

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"time"
)

const (
	runtimeBindingPollInterval  = 5 * time.Second
	runtimeBindingRestartLimit  = 3
	runtimeBindingRestartWindow = 10 * time.Minute
)

// Survives a failed automatic restart, including its original hotspot intent.
// Explicit Start/Stop clears it before waiting for the lifecycle gate.
type runtimeBindingRecovery struct {
	hotspot *HotspotConfig
}

// The service owns this monitor, so hiding the WebView or starting silently
// cannot suspend binding recovery. Explicitly stopped sessions do not scan.
func (s *EngineService) watchRuntimeBindings() {
	defer close(s.bindingMonitorDone)
	ticker := time.NewTicker(runtimeBindingPollInterval)
	defer ticker.Stop()
	s.runRuntimeBindingMonitor(s.client.Done(), ticker.C)
}

func (s *EngineService) runRuntimeBindingMonitor(done <-chan struct{}, ticks <-chan time.Time) {
	for {
		select {
		case <-done:
			return
		case <-ticks:
			s.scheduleRuntimeBindingRefresh()
		}
	}
}

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

// Logged with each automatic restart. Addresses are reduced to set/empty and
// DNS lists to counts so a support log never needs values to name the trigger.
type runtimeBindingChange struct {
	Adapter string `json:"adapter"`
	Field   string `json:"field"`
	Before  string `json:"before"`
	After   string `json:"after"`
}

func runtimeBindingChanges(previous, available []AdapterView) ([]runtimeBindingChange, error) {
	current := make(map[string]AdapterView, len(available))
	for _, adapter := range available {
		current[adapter.ID] = adapter
	}
	var changes []runtimeBindingChange
	var missing error
	installed := make(map[string]bool, len(previous))
	for _, old := range previous {
		installed[old.ID] = true
		next, ok := current[old.ID]
		if !ok || !usableRuntimeBinding(next) {
			// Disconnection alone must not interrupt the remaining working NICs.
			missing = fmt.Errorf("等待网卡 %s 恢复可用地址", old.Name)
			continue
		}
		changes = append(changes, adapterBindingChanges(old, next)...)
	}
	// A restart may have omitted a disconnected NIC. Its persisted selection
	// still matters even if it returns with exactly the same address/index.
	for _, next := range available {
		if next.Selected && usableRuntimeBinding(next) && !installed[next.ID] {
			changes = append(changes, runtimeBindingChange{Adapter: next.Name, Field: "selection", Before: "absent", After: "usable"})
		}
	}
	if len(changes) == 0 && missing != nil {
		return nil, missing
	}
	return changes, nil
}

func adapterBindingChanges(old, next AdapterView) []runtimeBindingChange {
	var changes []runtimeBindingChange
	add := func(field, before, after string) {
		changes = append(changes, runtimeBindingChange{Adapter: next.Name, Field: field, Before: before, After: after})
	}
	if old.Name != next.Name {
		add("name", old.Name, next.Name)
	}
	if old.Address != next.Address {
		add("ipv4", bindingAddressState(old.Address), bindingAddressState(next.Address))
	}
	if old.SourceIPv6 != next.SourceIPv6 {
		add("ipv6", bindingAddressState(old.SourceIPv6), bindingAddressState(next.SourceIPv6))
	}
	if old.IfIndex != next.IfIndex {
		add("if_index", strconv.Itoa(old.IfIndex), strconv.Itoa(next.IfIndex))
	}
	if old.IPv6IfIndex != next.IPv6IfIndex {
		add("ipv6_if_index", strconv.Itoa(old.IPv6IfIndex), strconv.Itoa(next.IPv6IfIndex))
	}
	oldDNS4, oldDNS6 := dnsServersByFamily(old.DNSServers)
	nextDNS4, nextDNS6 := dnsServersByFamily(next.DNSServers)
	if dnsFamilyChanged(oldDNS4, nextDNS4) {
		add("dns_ipv4", strconv.Itoa(len(oldDNS4)), strconv.Itoa(len(nextDNS4)))
	}
	if dnsFamilyChanged(oldDNS6, nextDNS6) {
		add("dns_ipv6", strconv.Itoa(len(oldDNS6)), strconv.Itoa(len(nextDNS6)))
	}
	return changes
}

func bindingAddressState(address string) string {
	if address == "" {
		return "empty"
	}
	return "set"
}

func dnsServersByFamily(servers []string) (ipv4, ipv6 []string) {
	for _, server := range servers {
		if ip, err := netip.ParseAddr(server); err == nil && ip.Unmap().Is4() {
			ipv4 = append(ipv4, server)
		} else {
			ipv6 = append(ipv6, server)
		}
	}
	return ipv4, ipv6
}

// Windows can report an empty per-family DNS list while the TUN is up and
// restore it once the TUN stops, even though the NIC's addresses never changed
// (phone hotspots advertising IPv6 DNS, #109). Restarting removes the TUN, so
// the next snapshot holds the list again and the restart repeats indefinitely.
// An empty list names no replacement server, so only a new list is a change.
func dnsFamilyChanged(before, after []string) bool {
	return len(after) > 0 && !slices.Equal(before, after)
}

// A running session keeps working on its old binding, so holding back a restart
// is safe. The window ages out, which lets a genuine later change still recover.
func (s *EngineService) allowRuntimeBindingRestart(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeBindingRestarts = slices.DeleteFunc(s.runtimeBindingRestarts, func(at time.Time) bool {
		return now.Sub(at) >= runtimeBindingRestartWindow
	})
	if len(s.runtimeBindingRestarts) >= runtimeBindingRestartLimit {
		return false
	}
	s.runtimeBindingRestarts = append(s.runtimeBindingRestarts, now)
	return true
}

func usableRuntimeBinding(adapter AdapterView) bool {
	return adapter.Operational && (adapter.Address != "" || adapter.SourceIPv6 != "")
}

// Explicit Start/Stop wins over a pending automatic recovery. In particular,
// a late scan may never restart aggregation after the user pressed Stop.
func (s *EngineService) cancelRuntimeBindingRefresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeBindingEnabled = false
	s.runtimeBindingRecovery = nil
	s.runtimeBindingRestarts = nil
	if s.runtimeBindingCancel != nil {
		// Serialize cancellation with the startup commit so it cannot re-enable
		// monitoring after an explicit Stop has already revoked recovery.
		s.runtimeBindingCancel()
	}
}

func (s *EngineService) scheduleRuntimeBindingRefresh() {
	s.mu.Lock()
	if !s.runtimeBindingEnabled || s.closing || s.runtimeBindingRefresh ||
		len(s.runtimeBindings) == 0 {
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
	recovery := s.runtimeBindingRecovery
	closing := s.closing
	enabled := s.runtimeBindingEnabled
	hotspot := s.hotspot
	s.mu.Unlock()
	if closing || !enabled || len(previous) == 0 {
		return nil
	}
	available, err := s.availableRuntimeAdapters()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	changes, err := runtimeBindingChanges(previous, available)
	hasSelected := slices.ContainsFunc(available, func(a AdapterView) bool { return a.Selected && usableRuntimeBinding(a) })
	if err != nil && (recovery == nil || !hasSelected) {
		s.setRuntimeBindingNotice("提示："+err.Error(), "waiting_for_adapter")
		return nil
	}
	if len(changes) == 0 && recovery == nil {
		s.setRuntimeBindingNotice("", "")
		return nil
	}
	if recovery == nil {
		state, err := actions.status(ctx)
		if err != nil {
			return err
		}
		if state != "running" && state != "degraded" {
			return nil
		}
		if !s.allowRuntimeBindingRestart(time.Now()) {
			s.setRuntimeBindingNotice("提示：网卡配置短时间内反复变化，已暂停自动重新绑定，稍后会自动重试；也可以手动停止后重新启动", "refresh_suspended")
			return nil
		}
		recovery = &runtimeBindingRecovery{}
		if hotspot != nil && hotspot.snapshot().State == "running" {
			config := hotspot.config
			recovery.hotspot = &config
		}
	}
	s.mu.Lock()
	if !s.runtimeBindingEnabled || s.closing || ctx.Err() != nil {
		s.mu.Unlock()
		return ctx.Err()
	}
	s.runtimeBindingRecovery = recovery
	s.mu.Unlock()
	if s.logs != nil {
		s.logs.RecordEvent("adapter_binding", "refresh_requested", map[string]any{"mode": mode, "changes": changes})
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
	s.mu.Lock()
	if s.runtimeBindingRecovery == recovery {
		s.runtimeBindingRecovery = nil
	}
	s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if recovery.hotspot != nil {
		if err := actions.hotspot(ctx, *recovery.hotspot); err != nil {
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
