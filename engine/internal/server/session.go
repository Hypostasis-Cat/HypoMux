package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	api "github.com/Hypostasis-Cat/HypoMux/engine/internal/api/v1"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/tun"
)

// Session is one authenticated protocol-v1 client of the shared engine. Many
// sessions (the desktop long connection plus short-lived CLI invocations) attach
// to the same Server concurrently; ending a session only detaches that client.
// The engine keeps running until an explicit engine.stop, the desktop's
// privileged-state disconnect cleanup, or the service itself stops.
type Session struct {
	server    *Server
	input     io.Reader
	encoder   *json.Encoder
	role      ClientRole
	writeMu   sync.Mutex
	closeOnce sync.Once
}

// Attach registers a new client session and its event subscription.
func (s *Server) Attach(input io.Reader, output io.Writer, role ClientRole) *Session {
	session := &Session{
		server:  s,
		input:   input,
		encoder: json.NewEncoder(output),
		role:    role,
	}
	s.sessionsMu.Lock()
	s.sessions[session] = struct{}{}
	s.sessionsMu.Unlock()
	return session
}

func (s *Server) removeSession(session *Session) {
	s.sessionsMu.Lock()
	delete(s.sessions, session)
	s.sessionsMu.Unlock()
}

func (session *Session) close() {
	session.closeOnce.Do(func() {
		if closer, ok := session.input.(io.Closer); ok {
			_ = closer.Close()
		}
	})
}

func (session *Session) writeMessage(message any) error {
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	return session.encoder.Encode(message)
}

// Run serves protocol-v1 requests as newline-delimited JSON for this session.
// Returning detaches the session and closes its transport; it never stops the
// shared engine. State changes raised while handling a request are broadcast to
// every attached session so the desktop UI observes CLI-driven changes too.
func (session *Session) Run(ctx context.Context) error {
	s := session.server
	defer s.removeSession(session)
	// Give the reader its own lifetime so every Run exit also releases queued
	// request delivery, even when the service context outlives this session.
	ctx, cancel := context.WithCancel(ctx)
	scanner := bufio.NewScanner(session.input)
	scanner.Buffer(make([]byte, 64*1024), protocol.MaxMessageBytes)

	// scanner.Scan blocks on the peer's next write/close; run it in its own
	// goroutine so ctx cancellation ends Run immediately instead of waiting for
	// the input stream to close.
	lines := make(chan []byte)
	scanErr := make(chan error, 1)
	go func() {
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
		scanErr <- scanner.Err()
		close(lines)
	}()

	// Cancel before closing: the reader may be waiting on lines or in Scan.
	defer func() {
		cancel()
		session.close()
	}()

	for {
		var raw []byte
		select {
		case <-ctx.Done():
			return ctx.Err()
		case received, ok := <-lines:
			if !ok {
				if err := <-scanErr; err != nil {
					return fmt.Errorf("read request: %w", err)
				}
				return nil
			}
			raw = received
		}

		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}

		stateBefore := s.runtime.Snapshot()
		tunBefore := s.tun.Status()
		response, shutdown := s.handle(ctx, session.role, line)
		if err := session.writeMessage(response); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
		stateAfter := s.runtime.Snapshot()
		if stateAfter.Sequence != stateBefore.Sequence {
			s.emitEvent(api.EventEngineStateChanged, stateAfter)
		}
		tunAfter := s.tun.Status()
		if tunAfter.State != tunBefore.State || tunAfter.PID != tunBefore.PID {
			s.emitEvent(api.EventTunStateChanged, tunAfter)
		}
		if shutdown {
			s.emitEvent(api.EventHostExiting, api.HostExitingData{Reason: "requested"})
			return nil
		}
	}
}

// emitEvent broadcasts an unsolicited notification to every attached session
// with a single host-wide monotonic sequence. Broadcasts are serialized so each
// session observes events in sequence order. A session whose transport fails is
// closed (unblocking its reader) rather than failing the whole broadcast.
func (s *Server) emitEvent(name string, data any) {
	s.broadcastMu.Lock()
	defer s.broadcastMu.Unlock()
	s.sessionsMu.Lock()
	s.eventSeq++
	sequence := s.eventSeq
	targets := make([]*Session, 0, len(s.sessions))
	for session := range s.sessions {
		targets = append(targets, session)
	}
	s.sessionsMu.Unlock()
	message := protocol.Notification(sequence, name, data)
	for _, session := range targets {
		if err := session.writeMessage(message); err != nil {
			session.close()
		}
	}
}

// Close shuts the shared engine down (service stop). It disconnects every
// session and stops the proxy/TUN transaction exactly once.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.sessionsMu.Lock()
		targets := make([]*Session, 0, len(s.sessions))
		for session := range s.sessions {
			targets = append(targets, session)
		}
		s.sessionsMu.Unlock()
		for _, session := range targets {
			session.close()
		}
		s.stopProxyForHostExit()
	})
}

// DesktopSessionEnded preserves the "no residual routes/TUN after the desktop
// exits" guarantee. The desktop is the only role that can activate the managed
// TUN sidecar and its WFP DNS exemption, so when a desktop session disconnects
// while that privileged state is live (e.g. the UI crashed), the whole TUN
// workflow is torn down. A plain proxy pool has no route/TUN footprint and is
// left running for the next client or an explicit engine.stop.
func (s *Server) DesktopSessionEnded() {
	s.lifecycleMu.Lock()
	privileged := s.tun.Status().State != tun.StateStopped || s.dnsExemption != nil
	s.lifecycleMu.Unlock()
	if !privileged {
		return
	}
	s.stopProxyForHostExit()
	s.emitEvent(api.EventEngineStateChanged, s.runtime.Snapshot())
	s.emitEvent(api.EventTunStateChanged, s.tun.Status())
}
