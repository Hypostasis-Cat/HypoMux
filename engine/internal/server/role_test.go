package server

import (
	"context"
	"io"
	"strings"
	"testing"

	api "github.com/Hypostasis-Cat/HypoMux/engine/internal/api/v1"
)

// The CLI pin authenticates any interactive process that can launch the pinned
// CLI, so the server must refuse privileged methods for the CLI role regardless
// of params, while the desktop role keeps the full surface.
func TestCLIRoleRejectsPrivilegedMethods(t *testing.T) {
	privileged := []string{
		api.MethodTunActivate,
		api.MethodMTUSet,
		api.MethodHostShutdown,
		api.MethodWFPInspect,
		api.MethodHotspotInspect,
		api.MethodSteamCDNConfigure,
		api.MethodDNSDoHRelay,
	}
	for _, method := range privileged {
		t.Run(method, func(t *testing.T) {
			line := []byte(`{"protocol":1,"id":"m","method":"` + method + `"}`)
			cli := New(strings.NewReader(""), io.Discard, Metadata{})
			response, _ := cli.handle(context.Background(), RoleCLI, line)
			if response.Error == nil || response.Error.Code != "security_policy_rejected" {
				t.Fatalf("CLI %s response = %#v, want security_policy_rejected", method, response)
			}

			desktop := New(strings.NewReader(""), io.Discard, Metadata{})
			desktopResponse, _ := desktop.handle(context.Background(), RoleDesktop, line)
			if desktopResponse.Error != nil && desktopResponse.Error.Code == "security_policy_rejected" {
				t.Fatalf("desktop %s was whitelist-rejected: %#v", method, desktopResponse.Error)
			}
		})
	}
}

func TestCLIRoleEngineStartRestrictions(t *testing.T) {
	cases := []struct {
		name   string
		params string
		reject bool
	}{
		{
			name:   "tun_tcp_pool mode",
			params: `{"mode":"tun_tcp_pool","listen_host":"127.0.0.1","adapters":[{"name":"loopback","source_ip":"127.0.0.1"}],"channels":[{"name":"nic_ethernet","adapter_names":["loopback"]},{"name":"nic_wifi","adapter_names":["loopback"]},{"name":"aggregation","adapter_names":["loopback"]}]}`,
			reject: true,
		},
		{
			name:   "non-loopback listen host",
			params: `{"mode":"proxy","listen_host":"0.0.0.0","adapters":[{"name":"loopback","source_ip":"127.0.0.1"}]}`,
			reject: true,
		},
		{
			name:   "LAN listen host",
			params: `{"mode":"proxy","listen_host":"192.168.1.10","adapters":[{"name":"loopback","source_ip":"127.0.0.1"}]}`,
			reject: true,
		},
		{
			name:   "loopback proxy",
			params: `{"mode":"proxy","listen_host":"127.0.0.1","adapters":[{"name":"loopback","source_ip":"127.0.0.1"}]}`,
			reject: false,
		},
		{
			name:   "default listen host",
			params: `{"mode":"proxy","adapters":[{"name":"loopback","source_ip":"127.0.0.1"}]}`,
			reject: false,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := New(strings.NewReader(""), io.Discard, Metadata{})
			line := []byte(`{"protocol":1,"id":"start","method":"engine.start","params":` + test.params + `}`)
			response, _ := server.handle(context.Background(), RoleCLI, line)
			if test.reject {
				if response.Error == nil || response.Error.Code != "security_policy_rejected" {
					t.Fatalf("CLI start response = %#v, want security_policy_rejected", response)
				}
				return
			}
			if response.Error != nil {
				t.Fatalf("CLI loopback proxy start failed: %#v", response.Error)
			}
			server.stopProxy("cleanup")
		})
	}
}

func TestDesktopRoleMayStartTUNPool(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, Metadata{})
	startTUNPoolForLifecycleTest(t, server)
	if server.runtime.Snapshot().State != "running" || server.mode != "tun_tcp_pool" {
		t.Fatalf("desktop TUN pool state = %s mode = %s", server.runtime.Snapshot().State, server.mode)
	}
	server.stopProxy("cleanup")
}

func TestHelloCapabilitiesTrimmedForCLIRole(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, Metadata{Name: "test"})

	desktop := server.hello(RoleDesktop)
	if len(desktop.Capabilities) != len(api.Capabilities()) {
		t.Fatalf("desktop capabilities = %#v, want full surface", desktop.Capabilities)
	}
	if len(desktop.Modes) != 2 {
		t.Fatalf("desktop modes = %#v", desktop.Modes)
	}

	cli := server.hello(RoleCLI)
	for _, capability := range cli.Capabilities {
		if !roleAllowsMethod(RoleCLI, capability) {
			t.Fatalf("CLI hello advertises restricted method %q", capability)
		}
	}
	for _, restricted := range []string{api.MethodTunActivate, api.MethodMTUSet, api.MethodHostShutdown, api.MethodWFPInspect} {
		for _, capability := range cli.Capabilities {
			if capability == restricted {
				t.Fatalf("CLI hello advertises %q", restricted)
			}
		}
	}
	if len(cli.Modes) != 1 || cli.Modes[0] != "proxy" {
		t.Fatalf("CLI modes = %#v, want proxy only", cli.Modes)
	}
	if _, ok := cli.ModeFeatures["tun_tcp_pool"]; ok {
		t.Fatalf("CLI mode_features still advertises tun_tcp_pool: %#v", cli.ModeFeatures)
	}
	if len(cli.Capabilities) == 0 {
		t.Fatal("CLI capabilities must not be empty")
	}
}
