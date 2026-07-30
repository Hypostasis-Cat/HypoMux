package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

type Server struct {
	config     Config
	scheduler  *scheduler
	schedulers map[string]*scheduler
	registry   *registry
	resolver   *dns.Resolver
	dialTCP    func(context.Context, *net.Dialer, string) (net.Conn, error)
	listenTCP  func(string, string) (net.Listener, error)

	mu                 sync.RWMutex
	ctx                context.Context
	cancel             context.CancelFunc
	listeners          []net.Listener
	endpoints          Endpoints
	running            bool
	wg                 sync.WaitGroup
	dnsFallbackHandler func(dns.FallbackEvent)
}

func New(config Config) (*Server, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	server := &Server{
		config:     normalized,
		scheduler:  newScheduler(normalized.Adapters, normalized.Weighted),
		schedulers: make(map[string]*scheduler, len(normalized.Channels)),
		registry:   newRegistry(normalized.Adapters),
	}
	for _, channel := range normalized.Channels {
		server.schedulers[channel.Name] = newScheduler(
			adaptersForChannel(normalized.Adapters, channel),
			normalized.Weighted,
		)
	}
	server.dialTCP = func(
		ctx context.Context,
		dialer *net.Dialer,
		target string,
	) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", target)
	}
	server.listenTCP = net.Listen
	return server, nil
}

func (s *Server) Start() (Endpoints, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return s.endpoints, nil
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())
	if len(s.config.Channels) == 0 {
		resolver, err := dns.New(s.ctx, s.config.DNS, s.dialDNS)
		if err != nil {
			s.cancel()
			return Endpoints{}, fmt.Errorf("create DNS resolver: %w", err)
		}
		resolver.SetFallbackHandler(s.dnsFallbackHandler)
		s.resolver = resolver
	}

	if len(s.config.Channels) > 0 {
		return s.startChannelListeners()
	}
	socks, err := s.listenTCP("tcp4", listenAddress(s.config.ListenHost, s.config.SOCKSPort))
	if err != nil {
		s.cancel()
		s.resolver = nil
		return Endpoints{}, fmt.Errorf("listen SOCKS: %w", err)
	}
	httpListener, err := s.listenTCP("tcp4", listenAddress(s.config.ListenHost, s.config.HTTPPort))
	if err != nil {
		_ = socks.Close()
		s.cancel()
		s.resolver = nil
		return Endpoints{}, fmt.Errorf("listen HTTP: %w", err)
	}
	s.listeners = []net.Listener{socks, httpListener}
	s.endpoints = Endpoints{
		SOCKS: socks.Addr().String(),
		HTTP:  httpListener.Addr().String(),
	}
	s.running = true
	s.wg.Add(2)
	go s.acceptLoop(socks, "socks5", "")
	go s.acceptLoop(httpListener, "http", "")
	return s.endpoints, nil
}

func (s *Server) startChannelListeners() (Endpoints, error) {
	listeners := make([]net.Listener, 0, len(s.config.Channels))
	endpoints := make(map[string]string, len(s.config.Channels))
	for _, channel := range s.config.Channels {
		listener, err := s.listenTCP(
			"tcp4",
			listenAddress(s.config.ListenHost, channel.Port),
		)
		if err != nil && channel.Port != 0 {
			listener, err = s.listenTCP("tcp4", listenAddress(s.config.ListenHost, 0))
		}
		if err != nil {
			for _, opened := range listeners {
				_ = opened.Close()
			}
			s.cancel()
			return Endpoints{}, fmt.Errorf("listen channel %q: %w", channel.Name, err)
		}
		listeners = append(listeners, listener)
		endpoints[channel.Name] = listener.Addr().String()
	}
	s.listeners = listeners
	s.endpoints = Endpoints{Channels: endpoints}
	s.running = true
	for index, listener := range listeners {
		channel := s.config.Channels[index].Name
		s.wg.Add(1)
		go s.acceptLoop(listener, "socks5", channel)
	}
	return s.endpoints, nil
}

func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	if s.cancel != nil {
		s.cancel()
	}
	listeners := append([]net.Listener(nil), s.listeners...)
	s.listeners = nil
	s.mu.Unlock()

	var closeErrors []error
	for _, listener := range listeners {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeErrors = append(closeErrors, err)
		}
	}
	s.registry.CloseAll()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("stop proxy: %w", ctx.Err())
	}
	return errors.Join(closeErrors...)
}

func (s *Server) Running() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *Server) Endpoints() Endpoints {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.endpoints
}

func (s *Server) Snapshot(includeConnections bool) TelemetrySnapshot {
	result := s.registry.Snapshot(includeConnections)
	if s.resolver != nil {
		status := s.resolver.Status()
		result.DNS = &status
	}
	return result
}

func (s *Server) DNSStatus() (dns.Status, bool) {
	s.mu.RLock()
	resolver := s.resolver
	running := s.running
	s.mu.RUnlock()
	if resolver == nil || !running {
		return dns.Status{}, false
	}
	return resolver.Status(), true
}

func (s *Server) ResolveDNS(
	ctx context.Context,
	domain string,
	adapterName string,
	recordType dns.RecordType,
) (dns.Result, error) {
	s.mu.RLock()
	resolver := s.resolver
	running := s.running
	s.mu.RUnlock()
	if resolver == nil || !running {
		return dns.Result{}, fmt.Errorf("proxy engine is not running")
	}
	var selected *Adapter
	for index := range s.config.Adapters {
		adapter := &s.config.Adapters[index]
		if adapterName == "" || adapter.Name == adapterName {
			selected = adapter
			break
		}
	}
	if selected == nil {
		return dns.Result{}, fmt.Errorf("unknown adapter %q", adapterName)
	}
	return resolver.Resolve(ctx, dns.Query{
		Domain:     domain,
		RecordType: recordType,
		Binding:    adapterDNSBinding(*selected),
	})
}

func (s *Server) SetDNSFallbackHandler(handler func(dns.FallbackEvent)) {
	s.mu.Lock()
	s.dnsFallbackHandler = handler
	resolver := s.resolver
	s.mu.Unlock()
	if resolver != nil {
		resolver.SetFallbackHandler(handler)
	}
}

func (s *Server) dialDNS(
	ctx context.Context,
	network string,
	address string,
	binding dns.Binding,
) (net.Conn, error) {
	adapter := Adapter{
		Name:     binding.Name,
		SourceIP: binding.SourceIP,
		IfIndex:  binding.IfIndex,
	}
	dialer, err := boundNetworkDialer(adapter, s.config.DNS.QueryTimeout, network)
	if err != nil {
		return nil, err
	}
	return dialer.DialContext(ctx, network, address)
}

func (s *Server) acceptLoop(listener net.Listener, protocol string, channel string) {
	defer s.wg.Done()
	for {
		client, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			select {
			case <-s.ctx.Done():
				return
			default:
				continue
			}
		}
		_ = client.SetDeadline(time.Time{})
		s.mu.RLock()
		if !s.running {
			s.mu.RUnlock()
			_ = client.Close()
			return
		}
		session := s.registry.Begin(protocol, channel, client)
		s.wg.Add(1)
		s.mu.RUnlock()
		go s.handleClient(protocol, client, session)
	}
}

func (s *Server) handleClient(protocol string, client net.Conn, session *connection) {
	defer s.wg.Done()
	defer client.Close()
	defer s.registry.Finish(session)

	reader := bufio.NewReaderSize(client, 64*1024)
	var adapter *Adapter
	if protocol == "socks5" {
		adapter = s.handleSOCKS(reader, client, session)
	} else {
		adapter = s.handleHTTP(reader, client, session)
	}
	_ = adapter
}

func (s *Server) connect(session *connection, target string) (net.Conn, Adapter, error) {
	ctx, cancel := context.WithTimeout(s.ctx, s.config.ConnectTimeout)
	defer cancel()
	channelScheduler := s.scheduler
	literalIPv4Only := false
	if session.channel != "" {
		channelScheduler = s.schedulers[session.channel]
		literalIPv4Only = true
	}
	if channelScheduler == nil {
		return nil, Adapter{}, fmt.Errorf("unknown channel %q", session.channel)
	}
	upstream, adapter, err := s.dialUpstream(
		ctx,
		target,
		channelScheduler,
		literalIPv4Only,
	)
	if err != nil {
		return nil, Adapter{}, err
	}
	s.registry.Attach(session, upstream, target, adapter)
	return upstream, adapter, nil
}

func (s *Server) relay(clientReader io.Reader, client net.Conn, upstream net.Conn, session *connection) {
	var relay sync.WaitGroup
	relay.Add(2)
	go func() {
		defer relay.Done()
		_, _ = io.CopyBuffer(accountingWriter{
			Writer: upstream,
			add:    func(amount uint64) { s.registry.AddUp(session, amount) },
		}, clientReader, make([]byte, 128*1024))
		closeWrite(upstream)
	}()
	go func() {
		defer relay.Done()
		_, _ = io.CopyBuffer(accountingWriter{
			Writer: client,
			add:    func(amount uint64) { s.registry.AddDown(session, amount) },
		}, upstream, make([]byte, 128*1024))
		closeWrite(client)
	}()
	relay.Wait()
	_ = upstream.Close()
}

type accountingWriter struct {
	io.Writer
	add func(uint64)
}

func (w accountingWriter) Write(payload []byte) (int, error) {
	written, err := w.Writer.Write(payload)
	if written > 0 {
		w.add(uint64(written))
	}
	return written, err
}

func closeWrite(connection net.Conn) {
	if tcp, ok := connection.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
}

func listenAddress(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}
