package services

import (
	"testing"
	"time"
)

func TestSortConnectionViewsKeepsTelemetryOrderStable(t *testing.T) {
	started := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	connections := []ConnectionView{
		{ID: 9, StartedAt: started.Add(time.Second)},
		{ID: 4, StartedAt: started},
		{ID: 2, StartedAt: started},
	}

	sortConnectionViews(connections)

	for index, want := range []uint64{2, 4, 9} {
		if connections[index].ID != want {
			t.Fatalf("connection order = %#v, want IDs [2 4 9]", connections)
		}
	}
}

func TestConnectionOutboundClassification(t *testing.T) {
	tests := []struct {
		channel string
		adapter string
		want    string
		detail  string
	}{
		{"", "Ethernet", "aggregation", "Ethernet"},
		{"aggregation", "WLAN", "aggregation", "WLAN"},
		{"direct", "", "direct", ""},
		{"nic_wifi", "WLAN", "adapter", "WLAN"},
		{"nic_custom", "", "adapter", "custom"},
	}
	for _, test := range tests {
		got, detail := connectionOutbound(test.channel, test.adapter)
		if got != test.want || detail != test.detail {
			t.Fatalf("connectionOutbound(%q, %q) = %q, %q; want %q, %q",
				test.channel, test.adapter, got, detail, test.want, test.detail)
		}
	}
}

func TestSplitConnectionEndpoint(t *testing.T) {
	host, port := splitConnectionEndpoint("example.com:443")
	if host != "example.com" || port != "443" {
		t.Fatalf("domain endpoint = %q, %q", host, port)
	}
	host, port = splitConnectionEndpoint("[2001:db8::1]:853")
	if host != "2001:db8::1" || port != "853" {
		t.Fatalf("IPv6 endpoint = %q, %q", host, port)
	}
}
