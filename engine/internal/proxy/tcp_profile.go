package proxy

import (
	"io"
	"net"
	"os"
	"strings"
	"sync"
)

const (
	tcpRelayBufferSize  = 128 * 1024
	tcpSocketBufferSize = 1024 * 1024
)

type tcpTuningMode uint8

const (
	tcpTuningAuto tcpTuningMode = iota
	tcpTuningOff
	tcpTuningForce
)

var tcpRelayBufferPool = sync.Pool{
	New: func() any {
		buffer := make([]byte, tcpRelayBufferSize)
		return &buffer
	},
}

func tcpTuningModeFromEnvironment() tcpTuningMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HYPOMUX_TCP_TUNING"))) {
	case "off":
		return tcpTuningOff
	case "force":
		return tcpTuningForce
	default:
		return tcpTuningAuto
	}
}

func (s *Server) shouldTuneTCP(channel string) bool {
	switch s.tcpTuningMode {
	case tcpTuningOff:
		return false
	case tcpTuningForce:
		return true
	}
	if channel == "" {
		return len(s.config.Adapters) >= 2
	}
	if channel != ChannelAggregation {
		return false
	}
	scheduler := s.schedulers[channel]
	return scheduler != nil && len(scheduler.adapters) >= 2
}

func (s *Server) tcpProfileName() string {
	switch s.tcpTuningMode {
	case tcpTuningOff:
		return "baseline"
	case tcpTuningForce:
		return "forced-1m"
	}
	if s.shouldTuneTCP("") || s.shouldTuneTCP(ChannelAggregation) {
		return "aggregation-1m"
	}
	return "baseline"
}

func tuneTCPConnection(connection net.Conn) {
	tcp, ok := connection.(*net.TCPConn)
	if !ok {
		return
	}
	// Tuning is deliberately best effort. A platform or security product that
	// rejects one of these options must not make an otherwise valid flow fail.
	_ = tcp.SetReadBuffer(tcpSocketBufferSize)
	_ = tcp.SetWriteBuffer(tcpSocketBufferSize)
	_ = tcp.SetNoDelay(true)
}

func acquireTCPRelayBuffer() (*[]byte, []byte) {
	pooled := tcpRelayBufferPool.Get().(*[]byte)
	return pooled, (*pooled)[:tcpRelayBufferSize]
}

func releaseTCPRelayBuffer(pooled *[]byte) {
	tcpRelayBufferPool.Put(pooled)
}

// readerOnly prevents io.CopyBuffer from selecting an upstream WriterTo
// implementation (notably bufio.Reader.WriteTo), so the configured relay
// buffer is consistently used in both directions.
type readerOnly struct {
	io.Reader
}
