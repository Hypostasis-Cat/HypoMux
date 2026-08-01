package services

import (
	"strings"
	"testing"
)

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
