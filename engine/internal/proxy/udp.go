package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultUDPFlowLimit         = 256
	defaultUDPFlowIdleTimeout   = 120 * time.Second
	defaultUDPFlowSweepInterval = 5 * time.Second
	maxSOCKSUDPDatagramBytes    = 65535
)

// socksUDPPacket carries the destination as a comparable netip value so the
// per-datagram path never builds a host:port string. The string form is only
// produced when a flow is actually created.
type socksUDPPacket struct {
	addr    netip.AddrPort
	payload []byte
}

type udpAssociation struct {
	server        *Server
	control       net.Conn
	channel       string
	scheduler     *scheduler
	relay         *net.UDPConn
	allowedIP     net.IP
	lockedPort    int
	flowLimit     int
	idleTimeout   time.Duration
	sweepInterval time.Duration

	mu     sync.Mutex
	flows  map[netip.AddrPort]*udpFlow
	closed bool
}

type udpFlow struct {
	association *udpAssociation
	addr        netip.AddrPort
	target      string // Cached at flow creation for latency failover checks.
	adapter     Adapter
	connection  net.Conn
	session     *connection
	lastActive  atomic.Int64
	lastReply   atomic.Int64
	closeOnce   sync.Once
	sendMu      sync.Mutex
}

func (s *Server) handleUDPAssociation(
	reader *bufio.Reader,
	client net.Conn,
	session *connection,
	requestedHost string,
	requestedPort int,
) (bool, error) {
	peer, ok := client.RemoteAddr().(*net.TCPAddr)
	if !ok || peer.IP == nil || peer.IP.To4() == nil {
		return false, errors.New("UDP ASSOCIATE requires an IPv4 TCP peer")
	}
	requestedIP := net.ParseIP(requestedHost)
	if requestedIP == nil {
		return false, errors.New("UDP ASSOCIATE has an invalid client address")
	}
	if !requestedIP.IsUnspecified() && !requestedIP.Equal(peer.IP) {
		return false, errors.New("UDP ASSOCIATE client address does not match TCP peer")
	}
	local, ok := client.LocalAddr().(*net.TCPAddr)
	if !ok || local.IP == nil || local.IP.To4() == nil {
		return false, errors.New("UDP ASSOCIATE requires an IPv4 loopback listener")
	}
	relay, err := s.listenUDP("udp4", &net.UDPAddr{IP: local.IP.To4()})
	if err != nil {
		return false, fmt.Errorf("listen UDP relay: %w", err)
	}
	association := &udpAssociation{
		server:        s,
		control:       client,
		channel:       session.channel,
		scheduler:     s.schedulers[session.channel],
		relay:         relay,
		allowedIP:     peer.IP.To4(),
		lockedPort:    requestedPort,
		flowLimit:     s.udpFlowLimit,
		idleTimeout:   s.udpIdleTimeout,
		sweepInterval: s.udpSweepInterval,
		flows:         make(map[netip.AddrPort]*udpFlow),
	}
	if association.scheduler == nil && association.channel != ChannelDirect {
		_ = relay.Close()
		return false, fmt.Errorf("unknown UDP channel %q", session.channel)
	}
	if !writeSOCKSBindReply(client, 0, relay.LocalAddr().(*net.UDPAddr)) {
		_ = relay.Close()
		return false, errors.New("write UDP ASSOCIATE reply")
	}
	defer association.close()

	controlDone := make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		_, _ = io.Copy(io.Discard, reader)
		close(controlDone)
	}()
	associationDone := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-controlDone:
			_ = relay.Close()
		case <-s.ctx.Done():
			_ = relay.Close()
		case <-associationDone:
		}
	}()
	err = association.serve(controlDone)
	close(associationDone)
	<-watcherDone
	return true, err
}

func (a *udpAssociation) serve(controlDone <-chan struct{}) error {
	buffer := make([]byte, maxSOCKSUDPDatagramBytes)
	// Refresh the periodic read deadline only after it expires, rather than
	// resetting its timer for every datagram.
	deadline := time.Time{}
	for {
		select {
		case <-controlDone:
			return nil
		case <-a.server.ctx.Done():
			return nil
		default:
		}

		if now := time.Now(); !now.Before(deadline) {
			deadline = now.Add(a.sweepInterval)
			_ = a.relay.SetReadDeadline(deadline)
		}
		count, clientAddress, err := a.relay.ReadFromUDP(buffer)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			var networkError net.Error
			if errors.As(err, &networkError) && networkError.Timeout() {
				continue
			}
			return fmt.Errorf("read UDP relay: %w", err)
		}
		if clientAddress == nil || !clientAddress.IP.Equal(a.allowedIP) {
			continue
		}
		packet, ok := parseSOCKSUDPPacket(buffer[:count])
		if !ok {
			continue
		}
		if a.lockedPort != 0 && clientAddress.Port != a.lockedPort {
			continue
		}
		if a.lockedPort == 0 {
			a.lockedPort = clientAddress.Port
		}
		a.forward(clientAddress, packet)
	}
}

func (a *udpAssociation) forward(clientAddress *net.UDPAddr, packet socksUDPPacket) {
	if a.scheduler != nil {
		a.scheduler.watchLatencyAddr(packet.addr.Addr())
	}
	exclude := ""
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	flow := a.flows[packet.addr]
	if flow != nil {
		a.mu.Unlock()
		// Outbound writes are not proof of connectivity. Require missing replies
		// AND comparative probes before retiring a silent flow. Never replay a
		// datagram already written to the old path.
		if a.scheduler != nil && time.Since(time.Unix(0, flow.lastReply.Load())) >= 3*time.Second && a.scheduler.latencyFailover(flow.adapter, flow.target) {
			exclude = flow.adapter.Name
			flow.close()
		} else {
			if err := flow.send(packet.payload); err != nil {
				flow.recordFailure(err)
				flow.close()
			}
			return
		}
		a.mu.Lock()
		if a.closed {
			a.mu.Unlock()
			return
		}
	}
	if len(a.flows) >= a.flowLimit {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	flow, err := a.createFlow(clientAddress, packet.addr, packet.payload, exclude)
	if err != nil {
		return
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		flow.close()
		return
	}
	if existing := a.flows[packet.addr]; existing != nil {
		a.mu.Unlock()
		flow.close()
		_ = existing.send(packet.payload)
		return
	}
	a.flows[packet.addr] = flow
	a.mu.Unlock()
	a.server.wg.Add(1)
	go func() {
		defer a.server.wg.Done()
		flow.receiveLoop(clientAddress)
	}()
}

func (a *udpAssociation) createFlow(
	clientAddress *net.UDPAddr,
	addr netip.AddrPort,
	firstPayload []byte,
	skip ...string,
) (*udpFlow, error) {
	// Cold path: the destination is only stringified when a flow is created.
	target := addr.String()
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("UDP target: %w", err)
	}
	targetIP := net.ParseIP(host)
	if targetIP == nil {
		return nil, errors.New("UDP target must be a literal IP address")
	}
	network := networkForIP("udp", targetIP)
	if a.channel == ChannelDirect {
		return a.createDirectFlow(clientAddress, addr, network, firstPayload)
	}
	adapters := a.scheduler.snapshot().Adapters
	excluded := make(map[string]struct{}, len(adapters))
	for _, name := range skip {
		for _, adapter := range adapters {
			if adapter.Name == name {
				excluded[name] = struct{}{}
			}
		}
	}
	for _, adapter := range adapters {
		if !adapterSupportsNetwork(adapter, network) && !(targetIP.To4() != nil && adapter.SourceIPv6 != "") {
			excluded[adapter.Name] = struct{}{}
		}
	}
	excludeCoolingFamilies(adapters, excluded, a.scheduler.health, network)
	attempts := len(adapters) - len(excluded)
	if attempts > 2 {
		attempts = 2
	}
	var failures []error
	for range attempts {
		adapter, ok := a.scheduler.selectForTarget(excluded, target)
		if !ok {
			break
		}
		excluded[adapter.Name] = struct{}{}
		flowNetwork, dialTarget := network, target
		if targetIP.To4() != nil && adapter.SourceIP == "" {
			translateCtx, stop := context.WithTimeout(a.server.ctx, a.server.config.DNS.QueryTimeout)
			translated, translateErr := a.server.resolver.TranslateIPv4(translateCtx, adapterDNSBinding(adapter), targetIP)
			stop()
			if translateErr != nil {
				failures = append(failures, translateErr)
				continue
			}
			flowNetwork, dialTarget = "udp6", net.JoinHostPort(translated.String(), port)
		}
		dialer, err := boundNetworkDialer(
			adapter,
			a.server.config.ConnectTimeout,
			flowNetwork,
		)
		if err != nil {
			a.scheduler.health.recordFamily(adapter, flowNetwork, false)
			if adapter.SourceIP == "" || adapter.SourceIPv6 == "" {
				a.scheduler.MarkFailure(adapter.Name)
			}
			failures = append(failures, fmt.Errorf("%s bind: %w", adapter.Name, err))
			continue
		}
		ctx, cancel := context.WithTimeout(
			a.server.ctx,
			a.server.config.ConnectTimeout,
		)
		upstream, err := a.server.dialUDP(ctx, dialer, dialTarget)
		cancel()
		if err == nil {
			var written int
			written, err = upstream.Write(firstPayload)
			if err == nil && written != len(firstPayload) {
				err = io.ErrShortWrite
			}
		}
		if err != nil {
			if upstream != nil {
				_ = upstream.Close()
			}
			if isLocalConnectFailure(err) {
				a.scheduler.health.recordFamily(adapter, flowNetwork, false)
				if adapter.SourceIP == "" || adapter.SourceIPv6 == "" {
					a.scheduler.MarkFailure(adapter.Name)
				}
			}
			failures = append(failures, fmt.Errorf("%s UDP setup: %w", adapter.Name, err))
			continue
		}
		a.scheduler.MarkSuccess(adapter.Name)
		a.scheduler.health.recordFamily(adapter, flowNetwork, true)
		clientLabel := clientAddress.String()
		telemetry := a.server.registry.BeginAddress(
			"socks5_udp",
			a.channel,
			clientLabel,
			a.relay,
		)
		a.server.registry.Attach(telemetry, upstream, target, adapter)
		a.server.registry.AddUp(telemetry, uint64(len(firstPayload)))
		flow := &udpFlow{
			association: a,
			addr:        addr,
			target:      target,
			adapter:     adapter,
			connection:  upstream,
			session:     telemetry,
		}
		flow.lastReply.Store(time.Now().UnixNano())
		flow.touch()
		return flow, nil
	}
	if len(failures) == 0 {
		return nil, errors.New("no UDP adapter available")
	}
	return nil, errors.Join(failures...)
}

func (a *udpAssociation) createDirectFlow(
	clientAddress *net.UDPAddr,
	addr netip.AddrPort,
	network string,
	firstPayload []byte,
) (*udpFlow, error) {
	target := addr.String()
	dialer := &net.Dialer{Timeout: a.server.config.ConnectTimeout}
	if network == "udp6" {
		dialer.LocalAddr = &net.UDPAddr{IP: net.IPv6unspecified}
	}
	ctx, cancel := context.WithTimeout(
		a.server.ctx,
		a.server.config.ConnectTimeout,
	)
	upstream, err := a.server.dialUDP(ctx, dialer, target)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("direct UDP setup: %w", err)
	}
	written, err := upstream.Write(firstPayload)
	if err == nil && written != len(firstPayload) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = upstream.Close()
		return nil, fmt.Errorf("direct UDP setup: %w", err)
	}
	telemetry := a.server.registry.BeginAddress(
		"socks5_udp",
		a.channel,
		clientAddress.String(),
		a.relay,
	)
	a.server.registry.AttachDirect(telemetry, upstream, target)
	a.server.registry.AddUp(telemetry, uint64(len(firstPayload)))
	flow := &udpFlow{
		association: a,
		addr:        addr,
		target:      target,
		connection:  upstream,
		session:     telemetry,
	}
	flow.touch()
	return flow, nil
}

func (f *udpFlow) send(payload []byte) error {
	f.sendMu.Lock()
	defer f.sendMu.Unlock()
	written, err := f.connection.Write(payload)
	if written > 0 {
		f.association.server.registry.AddUp(f.session, uint64(written))
		f.touch()
	}
	if err != nil {
		return err
	}
	if written != len(payload) {
		return io.ErrShortWrite
	}
	return nil
}

func (f *udpFlow) receiveLoop(clientAddress *net.UDPAddr) {
	defer f.close()
	packet, headerSize, ok := newSOCKSUDPReplyBuffer(f.addr)
	if !ok {
		return
	}
	// The target is immutable for this flow. Read directly after its cached
	// SOCKS header; WriteToUDP finishes using the buffer before the next read.
	buffer := packet[headerSize:]
	// Same amortisation as serve: re-arm the deadline only after it elapses.
	deadline := time.Time{}
	for {
		if now := time.Now(); !now.Before(deadline) {
			deadline = now.Add(f.association.sweepInterval)
			_ = f.connection.SetReadDeadline(deadline)
		}
		count, err := f.connection.Read(buffer)
		if err != nil {
			var networkError net.Error
			if errors.As(err, &networkError) && networkError.Timeout() {
				if time.Since(f.lastActivity()) < f.association.idleTimeout {
					continue
				}
			}
			f.recordFailure(err)
			return
		}
		f.lastReply.Store(time.Now().UnixNano())
		if count == 0 {
			continue
		}
		written, err := f.association.relay.WriteToUDP(packet[:headerSize+count], clientAddress)
		if err != nil {
			return
		}
		if written != headerSize+count {
			return
		}
		f.association.server.registry.AddDown(f.session, uint64(count))
		if written > 0 {
			f.touch()
		}
	}
}

func (f *udpFlow) touch() {
	f.lastActive.Store(time.Now().UnixNano())
}

func (f *udpFlow) lastActivity() time.Time {
	return time.Unix(0, f.lastActive.Load())
}

func (f *udpFlow) close() {
	f.closeOnce.Do(func() {
		_ = f.connection.Close()
		f.association.mu.Lock()
		if f.association.flows[f.addr] == f {
			delete(f.association.flows, f.addr)
		}
		f.association.mu.Unlock()
		f.association.server.registry.Finish(f.session)
	})
}

func (a *udpAssociation) close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	flows := make([]*udpFlow, 0, len(a.flows))
	for _, flow := range a.flows {
		flows = append(flows, flow)
	}
	a.mu.Unlock()
	_ = a.relay.Close()
	for _, flow := range flows {
		flow.close()
	}
}

func parseSOCKSUDPPacket(payload []byte) (socksUDPPacket, bool) {
	if len(payload) < 4 || payload[0] != 0 || payload[1] != 0 || payload[2] != 0 {
		return socksUDPPacket{}, false
	}
	var addr netip.Addr
	var portOffset int
	switch payload[3] {
	case 1:
		if len(payload) < 10 {
			return socksUDPPacket{}, false
		}
		addr = netip.AddrFrom4([4]byte(payload[4:8]))
		portOffset = 8
	case 4:
		if len(payload) < 22 {
			return socksUDPPacket{}, false
		}
		addr = netip.AddrFrom16([16]byte(payload[4:20]))
		// 部分客户端会用 IPv6 格式携带 IPv4 映射地址（::ffff:a.b.c.d），
		// 按 IPv4 处理而不是拒绝，保持与回复编码（IPv4 用 ATYP=1）对称。
		if unmapped := addr.Unmap(); unmapped.Is4() {
			addr = unmapped
		}
		portOffset = 20
	default:
		return socksUDPPacket{}, false
	}
	if !addr.IsValid() {
		return socksUDPPacket{}, false
	}
	port := binary.BigEndian.Uint16(payload[portOffset : portOffset+2])
	payloadOffset := portOffset + 2
	if port == 0 || len(payload) == payloadOffset {
		return socksUDPPacket{}, false
	}
	return socksUDPPacket{
		addr:    netip.AddrPortFrom(addr, port),
		payload: payload[payloadOffset:],
	}, true
}

func packSOCKSUDPReply(target netip.AddrPort, payload []byte) ([]byte, bool) {
	if !target.IsValid() || target.Port() == 0 {
		return nil, false
	}
	// Encode IPv4-mapped IPv6 addresses as ATYP=1, matching net.IP.To4().
	addr := target.Addr().Unmap()
	var packet []byte
	if addr.Is4() {
		ipv4 := addr.As4()
		packet = make([]byte, 10+len(payload))
		packet[3] = 1
		copy(packet[4:8], ipv4[:])
		binary.BigEndian.PutUint16(packet[8:10], target.Port())
		copy(packet[10:], payload)
	} else {
		ipv6 := addr.As16()
		packet = make([]byte, 22+len(payload))
		packet[3] = 4
		copy(packet[4:20], ipv6[:])
		binary.BigEndian.PutUint16(packet[20:22], target.Port())
		copy(packet[22:], payload)
	}
	return packet, true
}

// One allocation for packet storage per flow, rather than per datagram.
// Preserve the full upstream read size; oversized encapsulated datagrams are
// still rejected by the UDP socket instead of silently truncating their data.
func newSOCKSUDPReplyBuffer(target netip.AddrPort) ([]byte, int, bool) {
	header, ok := packSOCKSUDPReply(target, nil)
	if !ok {
		return nil, 0, false
	}
	packet := make([]byte, len(header)+maxSOCKSUDPDatagramBytes)
	copy(packet, header)
	return packet, len(header), true
}

func writeSOCKSBindReply(client net.Conn, reply byte, address *net.UDPAddr) bool {
	if address == nil || address.IP.To4() == nil || address.Port < 0 || address.Port > 65535 {
		return false
	}
	payload := make([]byte, 10)
	payload[0] = 5
	payload[1] = reply
	payload[3] = 1
	copy(payload[4:8], address.IP.To4())
	binary.BigEndian.PutUint16(payload[8:10], uint16(address.Port))
	_, err := client.Write(payload)
	return err == nil
}

// Remote refusal and idle timeouts must not poison adapter health.
func (f *udpFlow) recordFailure(err error) {
	s := f.association.scheduler
	if s == nil || !isLocalConnectFailure(err) {
		return
	}
	if s.snapshot().Strategy == StrategyLatency {
		if remote, ok := f.connection.RemoteAddr().(*net.UDPAddr); ok {
			s.health.recordFamily(f.adapter, networkForIP("udp", remote.IP), false)
		}
		if f.adapter.SourceIP == "" || f.adapter.SourceIPv6 == "" {
			s.MarkFailure(f.adapter.Name)
		}
	}
}
