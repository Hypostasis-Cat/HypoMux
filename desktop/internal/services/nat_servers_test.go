package services

import (
	"net"
	"path/filepath"
	"testing"
)

func TestNATServerStorePersistsSelectionAndDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nat_servers.json")
	store := newNATServerStore(path)
	initial := store.snapshot()
	if initial.SelectedID != natServerAutoID || len(initial.Servers) < 3 {
		t.Fatalf("unexpected defaults: %+v", initial)
	}
	added, err := store.add("Office STUN", "stun.example.com")
	if err != nil {
		t.Fatal(err)
	}
	custom := added.Servers[len(added.Servers)-1]
	if custom.Address != "stun.example.com:3478" || custom.BuiltIn {
		t.Fatalf("unexpected custom server: %+v", custom)
	}
	if _, err := store.selectServer(custom.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.remove("builtin-voipgate"); err != nil {
		t.Fatal(err)
	}

	reloaded := newNATServerStore(path).snapshot()
	if reloaded.SelectedID != custom.ID {
		t.Fatalf("selection was not persisted: %+v", reloaded)
	}
	for _, server := range reloaded.Servers {
		if server.ID == "builtin-voipgate" {
			t.Fatal("deleted built-in server returned after reload")
		}
	}
}

func TestNormalizeNATServerAddress(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"stun:stun.example.com:3478", "stun.example.com:3478"},
		{"STUN.EXAMPLE.COM", "stun.example.com:3478"},
		{"203.0.113.8:5349", "203.0.113.8:5349"},
	}
	for _, test := range tests {
		got, err := normalizeNATServerAddress(test.input)
		if err != nil || got != test.want {
			t.Fatalf("normalizeNATServerAddress(%q) = %q, %v; want %q", test.input, got, err, test.want)
		}
	}
	if _, err := normalizeNATServerAddress("https://example.com"); err == nil {
		t.Fatal("expected invalid scheme to be rejected")
	}
}

func TestFirstUsableSTUNIPRejectsFakeIP(t *testing.T) {
	if got := firstUsableSTUNIP([]net.IP{net.ParseIP("198.18.0.7"), net.ParseIP("185.125.180.70")}); got == nil || got.String() != "185.125.180.70" {
		t.Fatalf("unexpected usable address: %v", got)
	}
	if got := firstUsableSTUNIP([]net.IP{net.ParseIP("198.18.0.8")}); got != nil {
		t.Fatalf("fake-IP was accepted: %v", got)
	}
}
