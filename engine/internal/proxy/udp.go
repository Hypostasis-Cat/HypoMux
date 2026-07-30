package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
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

type socksUDPPacket struct {
	target  string
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
	flows  map[string]*udpFlow
	closed bool
}

type udpFlow struct {
	association *udpAssociation
	target      string
	adapter     Adapter
	connection  net.Conn
	session     *connection
	lastActive  atomic.Int64
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
		flows:         make(map[string]*udpFlow),
	}
	if association.scheduler == nil {
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
	for {
		select {
		case <-controlDone:
			return nil
		case <-a.server.ctx.Done():
			return nil
		default:
		}

		_ = a.relay.SetReadDeadline(time.Now().Add(a.sweepInterval))
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
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	flow := a.flows[packet.target]
	if flow != nil {
		a.mu.Unlock()
		if err := flow.send(packet.payload); err != nil {
			flow.close()
		}
		return
	}
	if len(a.flows) >= a.flowLimit {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	flow, err := a.createFlow(clientAddress, packet.target, packet.payload)
	if err != nil {
		return
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		flow.close()
		return
	}
	if existing := a.flows[packet.target]; existing != nil {
		a.mu.Unlock()
		flow.close()
		_ = existing.send(packet.payload)
		return
	}
	a.flows[packet.target] = flow
	a.mu.Unlock()
	a.server.wg.Add(1)
	go func() {
		defer a.server.wg.Done()
		flow.receiveLoop(clientAddress)
	}()
}

func (a *udpAssociation) createFlow(
	clientAddress *net.UDPAddr,
	target string,
	firstPayload []byte,
) (*udpFlow, error) {
	attempts := len(a.scheduler.adapters)
	if attempts > 2 {
		attempts = 2
	}
	excluded := make(map[string]struct{}, attempts)
	var failures []error
	for range attempts {
		adapter, ok := a.scheduler.Select(excluded)
		if !ok {
			break
		}
		excluded[adapter.Name] = struct{}{}
		dialer, err := boundNetworkDialer(
			adapter,
			a.server.config.ConnectTimeout,
			"udp4",
		)
		if err != nil {
			a.scheduler.MarkFailure(adapter.Name)
			failures = append(failures, fmt.Errorf("%s bind: %w", adapter.Name, err))
			continue
		}
		ctx, cancel := context.WithTimeout(
			a.server.ctx,
			a.server.config.ConnectTimeout,
		)
		upstream, err := a.server.dialUDP(ctx, dialer, target)
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
			a.scheduler.MarkFailure(adapter.Name)
			failures = append(failures, fmt.Errorf("%s UDP setup: %w", adapter.Name, err))
			continue
		}
		a.scheduler.MarkSuccess(adapter.Name)
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
			target:      target,
			adapter:     adapter,
			connection:  upstream,
			session:     telemetry,
		}
		flow.touch()
		return flow, nil
	}
	if len(failures) == 0 {
		return nil, errors.New("no UDP adapter available")
	}
	return nil, errors.Join(failures...)
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
	buffer := make([]byte, maxSOCKSUDPDatagramBytes)
	defer f.close()
	for {
		_ = f.connection.SetReadDeadline(
			time.Now().Add(f.association.sweepInterval),
		)
		count, err := f.connection.Read(buffer)
		if err != nil {
			var networkError net.Error
			if errors.As(err, &networkError) && networkError.Timeout() {
				if time.Since(f.lastActivity()) < f.association.idleTimeout {
					continue
				}
			}
			return
		}
		if count == 0 {
			continue
		}
		packet, ok := packSOCKSUDPReply(f.target, buffer[:count])
		if !ok {
			return
		}
		written, err := f.association.relay.WriteToUDP(packet, clientAddress)
		if err != nil {
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
		if f.association.flows[f.target] == f {
			delete(f.association.flows, f.target)
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
	if len(payload) < 10 || payload[0] != 0 || payload[1] != 0 || payload[2] != 0 {
		return socksUDPPacket{}, false
	}
	if payload[3] != 1 {
		return socksUDPPacket{}, false
	}
	ip := net.IP(payload[4:8]).To4()
	if ip == nil {
		return socksUDPPacket{}, false
	}
	port := int(binary.BigEndian.Uint16(payload[8:10]))
	if port == 0 || len(payload) == 10 {
		return socksUDPPacket{}, false
	}
	return socksUDPPacket{
		target:  net.JoinHostPort(ip.String(), strconv.Itoa(port)),
		payload: payload[10:],
	}, true
}

func packSOCKSUDPReply(target string, payload []byte) ([]byte, bool) {
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return nil, false
	}
	ip := net.ParseIP(host).To4()
	port, err := strconv.Atoi(portText)
	if ip == nil || err != nil || port <= 0 || port > 65535 {
		return nil, false
	}
	packet := make([]byte, 10+len(payload))
	packet[3] = 1
	copy(packet[4:8], ip)
	binary.BigEndian.PutUint16(packet[8:10], uint16(port))
	copy(packet[10:], payload)
	return packet, true
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
