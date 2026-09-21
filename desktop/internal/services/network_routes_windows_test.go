//go:build windows

package services

import (
	"errors"
	"net/netip"
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestRouteCollectionPreservesOtherFamilyOnFailure(t *testing.T) {
	for _, failedFamily := range []uint16{windows.AF_INET, windows.AF_INET6} {
		routes, err := collectNetworkRoutes(func(family uint16) ([]networkRoute, error) {
			prefix := "0.0.0.0/0"
			if family == windows.AF_INET6 {
				prefix = "::/0"
			}
			observed := []networkRoute{routeFixture(prefix, "VPN", 1)}
			if family == failedFamily {
				return observed, windows.ERROR_ACCESS_DENIED
			}
			return observed, nil
		})
		if len(routes) != 2 || !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Fatalf("partial evidence or failure lost: %v %v", routes, err)
		}
		aliases, _ := assessNetworkRoutes(routes)
		if len(aliases) != 1 || aliases[0] != "VPN" {
			t.Fatal("partial failure hid a conflict")
		}
	}
}

func TestReadOnlyNetworkRouteSnapshot(t *testing.T) {
	if os.Getenv("HYPOMUX_RUN_TUN_PREFLIGHT_TEST") != "1" {
		t.Skip("set HYPOMUX_RUN_TUN_PREFLIGHT_TEST=1 for native read-only route inspection")
	}
	routes, err := readNetworkRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) == 0 {
		t.Fatal("expected active routes on the Windows test host")
	}
	for _, route := range routes {
		if !route.Prefix.IsValid() || !route.Connected || route.InterfaceIndex == 0 || route.Alias == "" || !route.MetadataKnown {
			t.Fatalf("incomplete native route: %+v", route)
		}
	}
	aliases, risks := assessNetworkRoutes(routes)
	t.Logf("read %d active routes; %d competing/stale interfaces; %d route observations", len(routes), len(aliases), len(risks))
}

func TestNativeRoutePrefixDecoding(t *testing.T) {
	for _, address := range []string{"0.0.0.0", "128.0.0.0", "172.19.0.0", "::", "8000::", "fdfe:dcba:9876::"} {
		want := netip.MustParseAddr(address)
		var raw windows.RawSockaddrInet
		if want.Is4() {
			v4 := (*windows.RawSockaddrInet4)(unsafe.Pointer(&raw))
			v4.Family, v4.Addr = windows.AF_INET, want.As4()
		} else {
			v6 := (*windows.RawSockaddrInet6)(unsafe.Pointer(&raw))
			v6.Family, v6.Addr = windows.AF_INET6, want.As16()
		}
		got, ok := routePrefixAddress(raw)
		if !ok || got != want {
			t.Fatalf("decode %s: %s %v", want, got, ok)
		}
	}
	if _, ok := routePrefixAddress(windows.RawSockaddrInet{}); ok {
		t.Fatal("unknown address family accepted")
	}
}
