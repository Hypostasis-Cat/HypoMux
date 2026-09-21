package services

import (
	"errors"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetworkObservationChangesAndIncompleteScans(t *testing.T) {
	store := newSupportLogStore(filepath.Join(t.TempDir(), "support.log"))
	store.Start("tun", nil, nil)
	service := &EngineService{logs: store}
	physical := routeFixture("0.0.0.0/0", "Ethernet", 25)
	physical.Hardware = true
	own := routeFixture("0.0.0.0/1", "HypoMux-Tun", 1)
	routes := []networkRoute{physical, own}
	service.observeTUNNetworkEnvironment(routes, nil, true)
	baseline := service.tunNetworkFingerprint
	service.observeTUNNetworkEnvironment([]networkRoute{own, physical, own}, nil, false)
	service.observeTUNNetworkEnvironment(nil, errors.New("partial inspection"), false)
	if service.tunNetworkFingerprint != baseline {
		t.Fatal("enumeration order, duplicate rows or failed scan replaced the baseline")
	}
	// A VPN starting later must be recorded even if ordinary Internet works.
	routes = append(routes, routeFixture("128.0.0.0/1", "New VPN", 1))
	service.observeTUNNetworkEnvironment(routes, nil, false)
	service.observeTUNNetworkEnvironment(routes, nil, false)
	withVPN := service.tunNetworkFingerprint
	if baseline == withVPN {
		t.Fatal("new VPN was not observed")
	}
	// Same interface and route metric, new DHCP gateway.
	routes[0].NextHop = netip.MustParseAddr("192.0.2.1")
	service.observeTUNNetworkEnvironment(routes, nil, false)
	if withVPN == service.tunNetworkFingerprint {
		t.Fatal("gateway change was not observed")
	}
	store.Finish("test")
	data, err := store.Raw()
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, `"event":"baseline"`) != 1 || strings.Count(text, `"event":"changed"`) != 2 ||
		strings.Count(text, `"event":"inspection_incomplete"`) != 1 {
		t.Fatalf("unexpected observation log: %s", text)
	}
	if strings.Contains(text, `"foreign_route_interfaces":["HypoMux-Tun"`) || !strings.Contains(text, "New VPN") {
		t.Fatalf("incorrect competing interface attribution: %s", text)
	}
	if strings.Contains(text, "192.0.2.1") {
		t.Fatal("raw gateway leaked into periodic observation log")
	}
}
