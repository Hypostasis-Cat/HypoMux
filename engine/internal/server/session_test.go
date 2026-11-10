package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/tun"
)

// sessionTestClient drives one attached session over net.Pipe. A dedicated
// reader goroutine continuously drains the transport so broadcast event writes
// never block the engine, dispatching responses by id and recording events.
type sessionTestClient struct {
	conn      net.Conn
	responses chan protocol.Response
	ids       int
	eventsMu  sync.Mutex
	events    []protocol.Event
}

func dialSession(t *testing.T, host *Server, role ClientRole) *sessionTestClient {
	t.Helper()
	clientConn, engineConn := net.Pipe()
	session := host.Attach(engineConn, engineConn, role)
	go func() { _ = session.Run(context.Background()) }()
	client := &sessionTestClient{
		conn:      clientConn,
		responses: make(chan protocol.Response, 32),
	}
	go func() {
		scanner := bufio.NewScanner(clientConn)
		scanner.Buffer(make([]byte, 64*1024), protocol.MaxMessageBytes)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			var probe struct {
				Event string `json:"event"`
			}
			if json.Unmarshal(line, &probe) == nil && probe.Event != "" {
				var event protocol.Event
				if json.Unmarshal(line, &event) == nil {
					client.eventsMu.Lock()
					client.events = append(client.events, event)
					client.eventsMu.Unlock()
				}
				continue
			}
			var response protocol.Response
			if json.Unmarshal(line, &response) == nil {
				client.responses <- response
			}
		}
		close(client.responses)
	}()
	t.Cleanup(func() { _ = clientConn.Close() })
	return client
}

func (c *sessionTestClient) call(t *testing.T, method string, params string) protocol.Response {
	t.Helper()
	c.ids++
	id := fmt.Sprintf("t-%d", c.ids)
	request := `{"protocol":1,"id":"` + id + `","method":"` + method + `"`
	if params != "" {
		request += `,"params":` + params
	}
	request += "}\n"
	if _, err := c.conn.Write([]byte(request)); err != nil {
		t.Fatalf("write %s: %v", method, err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case response, ok := <-c.responses:
			if !ok {
				t.Fatalf("connection closed before %s response", method)
			}
			if response.ID == id {
				return response
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s response", method)
		}
	}
}

func (c *sessionTestClient) awaitEvent(t *testing.T, name string) protocol.Event {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.eventsMu.Lock()
		for _, event := range c.events {
			if event.Event == name {
				c.eventsMu.Unlock()
				return event
			}
		}
		c.eventsMu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session never received %s event", name)
	return protocol.Event{}
}

const loopbackProxyParams = `{"mode":"proxy","listen_host":"127.0.0.1","socks_port":0,"http_port":0,"dns":{"policy":"off"},"adapters":[{"name":"loopback","source_ip":"127.0.0.1"}]}`

// A proxy started by one session must outlive that session: a later session's
// status observes the same running engine. This is the production semantics the
// per-connection Server model could not provide.
func TestSharedEngineSurvivesSessionClose(t *testing.T) {
	host := NewHost(Metadata{Name: "test"})
	t.Cleanup(host.Close)

	first := dialSession(t, host, RoleCLI)
	start := first.call(t, "engine.start", loopbackProxyParams)
	if start.Error != nil {
		t.Fatalf("engine.start failed: %#v", start.Error)
	}
	// Close the starting session exactly as a one-shot CLI process exiting does.
	_ = first.conn.Close()

	second := dialSession(t, host, RoleCLI)
	status := second.call(t, "engine.status", "")
	if status.Error != nil {
		t.Fatalf("engine.status failed: %#v", status.Error)
	}
	result := status.Result.(map[string]any)
	engine := result["engine"].(map[string]any)
	if engine["state"] != "running" {
		t.Fatalf("engine state after starter session closed = %#v, want running", engine)
	}

	stop := second.call(t, "engine.stop", "")
	if stop.Error != nil {
		t.Fatalf("engine.stop failed: %#v", stop.Error)
	}
}

// The desktop's long-lived session and a CLI session must coexist, and a state
// change raised by one session is broadcast to every attached session.
func TestSessionsCoexistAndEventsBroadcast(t *testing.T) {
	host := NewHost(Metadata{Name: "test"})
	t.Cleanup(host.Close)

	desktop := dialSession(t, host, RoleDesktop)
	cli := dialSession(t, host, RoleCLI)

	// The desktop session is online while the CLI connects and starts the engine.
	start := cli.call(t, "engine.start", loopbackProxyParams)
	if start.Error != nil {
		t.Fatalf("CLI engine.start failed: %#v", start.Error)
	}
	desktop.awaitEvent(t, "engine.state_changed")
	cli.awaitEvent(t, "engine.state_changed")

	// Both sessions still work after the broadcast.
	if response := desktop.call(t, "engine.status", ""); response.Error != nil {
		t.Fatalf("desktop status failed: %#v", response.Error)
	}
	if response := cli.call(t, "health.check", ""); response.Error != nil {
		t.Fatalf("CLI health failed: %#v", response.Error)
	}
}

func TestDesktopDisconnectCleansPrivilegedTUNState(t *testing.T) {
	host := NewHost(testTunMetadata())
	t.Cleanup(host.Close)
	controller := &fakeTunController{status: tun.Status{State: tun.StateStopped}}
	host.tun = controller
	startTUNPoolForLifecycleTest(t, host)
	// Simulate a live managed TUN sidecar owned by the desktop workflow.
	controller.mu.Lock()
	controller.status = tun.Status{State: tun.StateRunning, Generation: 1, PID: 1234}
	controller.mu.Unlock()
	host.activeTunGeneration = 1

	host.DesktopSessionEnded()

	if controller.Status().State != tun.StateStopped || controller.stopCalls == 0 {
		t.Fatalf("desktop disconnect left TUN running: %#v stopCalls=%d", controller.Status(), controller.stopCalls)
	}
	if host.proxy != nil || host.runtime.Snapshot().State != "stopped" {
		t.Fatalf("desktop disconnect left residual engine: proxy=%v state=%s", host.proxy, host.runtime.Snapshot().State)
	}
}

func TestDesktopDisconnectKeepsPlainProxyPool(t *testing.T) {
	host := NewHost(Metadata{Name: "test"})
	t.Cleanup(host.Close)
	line := []byte(`{"protocol":1,"id":"start","method":"engine.start","params":` + loopbackProxyParams + `}`)
	response, _ := host.handle(context.Background(), RoleDesktop, line)
	if response.Error != nil {
		t.Fatalf("engine.start failed: %#v", response.Error)
	}

	host.DesktopSessionEnded()

	if host.proxy == nil || host.runtime.Snapshot().State != "running" {
		t.Fatalf("plain proxy pool was torn down on desktop disconnect: proxy=%v state=%s", host.proxy, host.runtime.Snapshot().State)
	}
}

func TestHelloAdvertisesSharedEngineIdentity(t *testing.T) {
	host := NewHost(Metadata{Name: "hypomux-engine", Version: "test"})
	t.Cleanup(host.Close)
	first := dialSession(t, host, RoleCLI)
	second := dialSession(t, host, RoleCLI)
	helloFirst := first.call(t, "engine.hello", "")
	helloSecond := second.call(t, "engine.hello", "")
	if helloFirst.Error != nil || helloSecond.Error != nil {
		t.Fatalf("hello failed: %#v / %#v", helloFirst.Error, helloSecond.Error)
	}
	startedFirst := helloFirst.Result.(map[string]any)["started_at"]
	startedSecond := helloSecond.Result.(map[string]any)["started_at"]
	if startedFirst != startedSecond {
		t.Fatalf("sessions saw different engine start times (%v vs %v); engine is not a shared singleton", startedFirst, startedSecond)
	}
}
