package proxy

import (
	"net"
	"testing"
)

func TestUDPTuningPolicyAndTelemetry(t *testing.T) {
	for _, test := range []struct {
		mode, channel, profile string
		tune                   bool
	}{
		{"", ChannelAggregation, "aggregation-1m-256k", true},
		{"", ChannelEthernet, "aggregation-1m-256k", false},
		{"", ChannelDirect, "aggregation-1m-256k", false},
		{"off", ChannelAggregation, "baseline", false},
		{"force", ChannelDirect, "forced-1m-256k", true},
	} {
		t.Run(test.mode+"/"+test.channel, func(t *testing.T) {
			t.Setenv("HYPOMUX_UDP_TUNING", test.mode)
			s, err := New(twoAdapterChannelConfig())
			if err != nil {
				t.Fatal(err)
			}
			if s.shouldTuneUDP(test.channel) != test.tune || s.Snapshot(false).UDPProfile != test.profile {
				t.Fatal("wrong UDP tuning scope/profile")
			}
		})
	}
	t.Setenv("HYPOMUX_UDP_TUNING", "auto")
	s, err := New(Config{Adapters: []Adapter{{Name: "single", SourceIP: "127.0.0.1"}}})
	if err != nil || s.shouldTuneUDP("") || s.udpProfileName() != "baseline" {
		t.Fatal("tuned single NIC", err)
	}
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	tuneUDPConnection(client) // Non-UDP transports and unsupported options are safe.
}
