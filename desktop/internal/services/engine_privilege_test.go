package services

import (
	"strings"
	"testing"
)

func TestSystemProxyTakeoverPolicy(t *testing.T) {
	settings := DefaultSettings()
	if !shouldTakeOverSystemProxy("proxy", settings) {
		t.Fatal("default proxy mode must manage the Windows system proxy")
	}
	settings.SystemProxyTakeover = false
	if shouldTakeOverSystemProxy("proxy", settings) {
		t.Fatal("manual local-proxy mode must leave the Windows system proxy unchanged")
	}
	settings.SystemProxyTakeover = true
	if shouldTakeOverSystemProxy("tun", settings) {
		t.Fatal("TUN mode must never enter the Windows system-proxy takeover path")
	}
}

func TestElevatedDifferentUserBlocksSystemProxyBeforeAdapterDiscovery(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	service := NewEngineServiceWithDomainsAndHostPrivilege(
		settings,
		NewAdapterService(settings),
		nil,
		HostPrivilegeCompatibility{
			Elevated:  true,
			ProxySafe: false,
			Detail:    "desktop user SID does not match",
		},
	)
	defer service.Shutdown()

	_, err := service.Start("proxy")
	if err == nil || !strings.Contains(err.Error(), "管理员兼容模式已阻止系统代理") {
		t.Fatalf("unsafe elevated proxy start was not blocked: %v", err)
	}
}

func TestElevatedSameUserCanProceedPastPrivilegeGate(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	service := NewEngineServiceWithDomainsAndHostPrivilege(
		settings,
		NewAdapterService(settings),
		nil,
		HostPrivilegeCompatibility{Elevated: true, ProxySafe: true},
	)
	defer service.Shutdown()

	_, err := service.Start("proxy")
	if err != nil && strings.Contains(err.Error(), "管理员兼容模式已阻止系统代理") {
		t.Fatalf("same-user compatibility was blocked by the privilege gate: %v", err)
	}
}

func TestElevatedDifferentUserCanUseLocalProxyPortsWithoutTakeover(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	next := settings.Get()
	next.SystemProxyTakeover = false
	if _, err := settings.Update(next); err != nil {
		t.Fatal(err)
	}
	service := NewEngineServiceWithDomainsAndHostPrivilege(
		settings,
		NewAdapterService(settings),
		nil,
		HostPrivilegeCompatibility{
			Elevated:  true,
			ProxySafe: false,
			Detail:    "desktop user SID does not match",
		},
	)
	defer service.Shutdown()

	_, err := service.Start("proxy")
	if err != nil && strings.Contains(err.Error(), "管理员兼容模式已阻止系统代理") {
		t.Fatalf("manual local-proxy mode was blocked by an irrelevant system-proxy gate: %v", err)
	}
}
