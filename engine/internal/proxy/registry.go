package proxy

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

type AdapterTelemetry struct {
	Name        string `json:"name"`
	SourceIP    string `json:"source_ip"`
	IfIndex     int    `json:"if_index"`
	Connections uint64 `json:"connections"`
	BytesUp     uint64 `json:"bytes_up"`
	BytesDown   uint64 `json:"bytes_down"`
}

type ConnectionSnapshot struct {
	ID        uint64    `json:"id"`
	Protocol  string    `json:"protocol"`
	Client    string    `json:"client"`
	Target    string    `json:"target,omitempty"`
	Adapter   string    `json:"adapter,omitempty"`
	StartedAt time.Time `json:"started_at"`
	BytesUp   uint64    `json:"bytes_up"`
	BytesDown uint64    `json:"bytes_down"`
}

type TelemetrySnapshot struct {
	StartedAt   time.Time            `json:"started_at"`
	SampledAt   time.Time            `json:"sampled_at"`
	Adapters    []AdapterTelemetry   `json:"adapters"`
	Connections []ConnectionSnapshot `json:"active_connections,omitempty"`
	DNS         *dns.Status          `json:"dns,omitempty"`
	Total       struct {
		Connections uint64 `json:"connections"`
		BytesUp     uint64 `json:"bytes_up"`
		BytesDown   uint64 `json:"bytes_down"`
	} `json:"total"`
}

type adapterCounters struct {
	adapter   Adapter
	active    atomic.Uint64
	bytesUp   atomic.Uint64
	bytesDown atomic.Uint64
}

type connection struct {
	id         uint64
	protocol   string
	client     string
	startedAt  time.Time
	clientConn net.Conn

	mu        sync.RWMutex
	upstream  net.Conn
	target    string
	adapter   string
	bytesUp   atomic.Uint64
	bytesDown atomic.Uint64
}

type registry struct {
	mu          sync.RWMutex
	nextID      atomic.Uint64
	startedAt   time.Time
	connections map[uint64]*connection
	adapters    map[string]*adapterCounters
	order       []string
}

func newRegistry(adapters []Adapter) *registry {
	result := &registry{
		startedAt:   time.Now().UTC(),
		connections: make(map[uint64]*connection),
		adapters:    make(map[string]*adapterCounters, len(adapters)),
		order:       make([]string, 0, len(adapters)),
	}
	for _, adapter := range adapters {
		result.adapters[adapter.Name] = &adapterCounters{adapter: adapter}
		result.order = append(result.order, adapter.Name)
	}
	return result
}

func (r *registry) Begin(protocol string, client net.Conn) *connection {
	session := &connection{
		id:         r.nextID.Add(1),
		protocol:   protocol,
		client:     client.RemoteAddr().String(),
		startedAt:  time.Now().UTC(),
		clientConn: client,
	}
	r.mu.Lock()
	r.connections[session.id] = session
	r.mu.Unlock()
	return session
}

func (r *registry) Attach(session *connection, upstream net.Conn, target string, adapter Adapter) {
	session.mu.Lock()
	session.upstream = upstream
	session.target = target
	session.adapter = adapter.Name
	session.mu.Unlock()
	if counters := r.adapters[adapter.Name]; counters != nil {
		counters.active.Add(1)
	}
}

func (r *registry) AddUp(session *connection, amount uint64) {
	if amount == 0 {
		return
	}
	session.bytesUp.Add(amount)
	session.mu.RLock()
	counters := r.adapters[session.adapter]
	session.mu.RUnlock()
	if counters != nil {
		counters.bytesUp.Add(amount)
	}
}

func (r *registry) AddDown(session *connection, amount uint64) {
	if amount == 0 {
		return
	}
	session.bytesDown.Add(amount)
	session.mu.RLock()
	counters := r.adapters[session.adapter]
	session.mu.RUnlock()
	if counters != nil {
		counters.bytesDown.Add(amount)
	}
}

func (r *registry) Finish(session *connection) {
	r.mu.Lock()
	delete(r.connections, session.id)
	r.mu.Unlock()

	session.mu.RLock()
	adapter := session.adapter
	session.mu.RUnlock()
	if counters := r.adapters[adapter]; counters != nil {
		counters.active.Add(^uint64(0))
	}
}

func (r *registry) CloseAll() {
	r.mu.RLock()
	sessions := make([]*connection, 0, len(r.connections))
	for _, session := range r.connections {
		sessions = append(sessions, session)
	}
	r.mu.RUnlock()
	for _, session := range sessions {
		_ = session.clientConn.Close()
		session.mu.RLock()
		upstream := session.upstream
		session.mu.RUnlock()
		if upstream != nil {
			_ = upstream.Close()
		}
	}
}

func (r *registry) Snapshot(includeConnections bool) TelemetrySnapshot {
	result := TelemetrySnapshot{
		StartedAt: r.startedAt,
		SampledAt: time.Now().UTC(),
		Adapters:  make([]AdapterTelemetry, 0, len(r.order)),
	}
	for _, name := range r.order {
		counters := r.adapters[name]
		item := AdapterTelemetry{
			Name:        counters.adapter.Name,
			SourceIP:    counters.adapter.SourceIP,
			IfIndex:     counters.adapter.IfIndex,
			Connections: counters.active.Load(),
			BytesUp:     counters.bytesUp.Load(),
			BytesDown:   counters.bytesDown.Load(),
		}
		result.Adapters = append(result.Adapters, item)
		result.Total.Connections += item.Connections
		result.Total.BytesUp += item.BytesUp
		result.Total.BytesDown += item.BytesDown
	}
	if !includeConnections {
		return result
	}

	r.mu.RLock()
	sessions := make([]*connection, 0, len(r.connections))
	for _, session := range r.connections {
		sessions = append(sessions, session)
	}
	r.mu.RUnlock()
	result.Connections = make([]ConnectionSnapshot, 0, len(sessions))
	for _, session := range sessions {
		session.mu.RLock()
		item := ConnectionSnapshot{
			ID:        session.id,
			Protocol:  session.protocol,
			Client:    session.client,
			Target:    session.target,
			Adapter:   session.adapter,
			StartedAt: session.startedAt,
			BytesUp:   session.bytesUp.Load(),
			BytesDown: session.bytesDown.Load(),
		}
		session.mu.RUnlock()
		result.Connections = append(result.Connections, item)
	}
	return result
}
