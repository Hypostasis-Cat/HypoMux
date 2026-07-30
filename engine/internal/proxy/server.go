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
)

type Server struct {
	config    Config
	scheduler *scheduler
	registry  *registry

	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	listeners []net.Listener
	endpoints Endpoints
	running   bool
	wg        sync.WaitGroup
}

func New(config Config) (*Server, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	return &Server{
		config:    normalized,
		scheduler: newScheduler(normalized.Adapters, normalized.Weighted),
		registry:  newRegistry(normalized.Adapters),
	}, nil
}

func (s *Server) Start() (Endpoints, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return s.endpoints, nil
	}

	socks, err := net.Listen("tcp4", listenAddress(s.config.ListenHost, s.config.SOCKSPort))
	if err != nil {
		return Endpoints{}, fmt.Errorf("listen SOCKS: %w", err)
	}
	httpListener, err := net.Listen("tcp4", listenAddress(s.config.ListenHost, s.config.HTTPPort))
	if err != nil {
		_ = socks.Close()
		return Endpoints{}, fmt.Errorf("listen HTTP: %w", err)
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.listeners = []net.Listener{socks, httpListener}
	s.endpoints = Endpoints{
		SOCKS: socks.Addr().String(),
		HTTP:  httpListener.Addr().String(),
	}
	s.running = true
	s.wg.Add(2)
	go s.acceptLoop(socks, "socks5")
	go s.acceptLoop(httpListener, "http")
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
	return s.registry.Snapshot(includeConnections)
}

func (s *Server) acceptLoop(listener net.Listener, protocol string) {
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
		session := s.registry.Begin(protocol, client)
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
	upstream, adapter, err := s.dialUpstream(ctx, target)
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
