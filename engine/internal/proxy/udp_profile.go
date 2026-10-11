package proxy

import (
	"net"
	"os"
	"strings"
)

const (
	udpSocketBufferSize      = 256 * 1024
	udpRelaySocketBufferSize = 1024 * 1024
)

type udpTuningMode uint8

const (
	udpTuningAuto udpTuningMode = iota
	udpTuningOff
	udpTuningForce
)

func udpTuningModeFromEnvironment() udpTuningMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HYPOMUX_UDP_TUNING"))) {
	case "off":
		return udpTuningOff
	case "force":
		return udpTuningForce
	default:
		return udpTuningAuto
	}
}

func (s *Server) shouldTuneUDP(channel string) bool {
	switch s.udpTuningMode {
	case udpTuningOff:
		return false
	case udpTuningForce:
		return true
	}
	if channel == "" {
		return len(s.scheduler.snapshot().Adapters) >= 2
	}
	if channel != ChannelAggregation {
		return false
	}
	scheduler := s.schedulers[channel]
	return scheduler != nil && len(scheduler.snapshot().Adapters) >= 2
}

func (s *Server) udpProfileName() string {
	switch s.udpTuningMode {
	case udpTuningOff:
		return "baseline"
	case udpTuningForce:
		return "forced-1m-256k"
	}
	if s.shouldTuneUDP("") || s.shouldTuneUDP(ChannelAggregation) {
		return "aggregation-1m-256k"
	}
	return "baseline"
}

func tuneUDPConnection(connection net.Conn) {
	udp, ok := connection.(*net.UDPConn)
	if !ok {
		return
	}
	// Bounded burst headroom on aggregation sockets. Errors are best effort,
	// and off mode permits comparison with the operating system defaults.
	_ = udp.SetReadBuffer(udpSocketBufferSize)
	_ = udp.SetWriteBuffer(udpSocketBufferSize)
}

func tuneUDPRelay(relay *net.UDPConn) {
	// The association multiplexes flows, whereas each upstream socket has a
	// smaller per-flow budget. Do not reserve 1 MiB for all 256 possible flows.
	_ = relay.SetReadBuffer(udpRelaySocketBufferSize)
	_ = relay.SetWriteBuffer(udpRelaySocketBufferSize)
}
