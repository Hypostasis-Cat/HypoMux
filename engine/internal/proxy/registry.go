package proxy

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

type AdapterTelemetry struct {
	Name                string     `json:"name"`
	SourceIP            string     `json:"source_ip"`
	IfIndex             int        `json:"if_index"`
	SourceIPv6          string     `json:"source_ipv6,omitempty"`
	IPv6IfIndex         int        `json:"ipv6_if_index,omitempty"`
	Connections         uint64     `json:"connections"`
	BytesUp             uint64     `json:"bytes_up"`
	BytesDown           uint64     `json:"bytes_down"`
	HealthState         string     `json:"health_state"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	HealthSuccesses     uint64     `json:"health_successes"`
	HealthFailures      uint64     `json:"health_failures"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt       *time.Time `json:"last_failure_at,omitempty"`
	CooldownUntil       *time.Time `json:"cooldown_until,omitempty"`
	DomainQuarantines   int        `json:"domain_quarantines"`
}

type DomainQuarantineTelemetry struct {
	Adapter   string    `json:"adapter"`
	Domain    string    `json:"domain"`
	Evidence  int       `json:"evidence"`
	ExpiresAt time.Time `json:"expires_at"`
}

type ConnectionSnapshot struct {
	ID        uint64    `json:"id"`
	Protocol  string    `json:"protocol"`
	Channel   string    `json:"channel,omitempty"`
	Client    string    `json:"client"`
	Listener  string    `json:"listener,omitempty"`
	Target    string    `json:"target,omitempty"`
	Remote    string    `json:"remote,omitempty"`
	Adapter   string    `json:"adapter,omitempty"`
	StartedAt time.Time `json:"started_at"`
	BytesUp   uint64    `json:"bytes_up"`
	BytesDown uint64    `json:"bytes_down"`
}

type TelemetrySnapshot struct {
	StartedAt         time.Time                   `json:"started_at"`
	SampledAt         time.Time                   `json:"sampled_at"`
	TCPProfile        string                      `json:"tcp_profile,omitempty"`
	Adapters          []AdapterTelemetry          `json:"adapters"`
	Connections       []ConnectionSnapshot        `json:"active_connections,omitempty"`
	DomainQuarantines []DomainQuarantineTelemetry `json:"domain_quarantines,omitempty"`
	DNS               *dns.Status                 `json:"dns,omitempty"`
	Total             struct {
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
	channel    string
	client     string
	listener   string
	startedAt  time.Time
	clientConn net.Conn

	mu        sync.RWMutex
	upstream  net.Conn
	target    string
	remote    string
	adapter   string
	counters  atomic.Pointer[adapterCounters]
	direct    atomic.Bool
	bytesUp   atomic.Uint64
	bytesDown atomic.Uint64
}

type registry struct {
	mu              sync.RWMutex
	nextID          atomic.Uint64
	startedAt       time.Time
	connections     map[uint64]*connection
	adapters        map[string]*adapterCounters
	order           []string
	directActive    atomic.Uint64
	directBytesUp   atomic.Uint64
	directBytesDown atomic.Uint64
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

func (r *registry) Begin(protocol string, channel string, client net.Conn) *connection {
	clientAddress := ""
	listenerAddress := ""
	if address := client.RemoteAddr(); address != nil {
		clientAddress = address.String()
	}
	if address := client.LocalAddr(); address != nil {
		listenerAddress = address.String()
	}
	return r.beginAddress(protocol, channel, clientAddress, listenerAddress, client)
}

func (r *registry) BeginAddress(
	protocol string,
	channel string,
	clientAddress string,
	client net.Conn,
) *connection {
	return r.beginAddress(protocol, channel, clientAddress, "", client)
}

func (r *registry) beginAddress(
	protocol string,
	channel string,
	clientAddress string,
	listenerAddress string,
	client net.Conn,
) *connection {
	session := &connection{
		id:         r.nextID.Add(1),
		protocol:   protocol,
		channel:    channel,
		client:     clientAddress,
		listener:   listenerAddress,
		startedAt:  time.Now().UTC(),
		clientConn: client,
	}
	r.mu.Lock()
	r.connections[session.id] = session
	r.mu.Unlock()
	return session
}

func (r *registry) Attach(session *connection, upstream net.Conn, target string, adapter Adapter) {
	counters := r.adapters[adapter.Name]
	session.mu.Lock()
	session.upstream = upstream
	session.target = target
	if address := upstream.RemoteAddr(); address != nil {
		session.remote = address.String()
	}
	session.adapter = adapter.Name
	session.mu.Unlock()
	if counters != nil {
		session.counters.Store(counters)
		counters.active.Add(1)
	}
}

func (r *registry) AttachDirect(session *connection, upstream net.Conn, target string) {
	session.mu.Lock()
	session.upstream = upstream
	session.target = target
	if address := upstream.RemoteAddr(); address != nil {
		session.remote = address.String()
	}
	session.adapter = ""
	session.mu.Unlock()
	session.direct.Store(true)
	r.directActive.Add(1)
}

func (r *registry) AddUp(session *connection, amount uint64) {
	if amount == 0 {
		return
	}
	session.bytesUp.Add(amount)
	counters := session.counters.Load()
	if counters != nil {
		counters.bytesUp.Add(amount)
	} else if session.direct.Load() {
		r.directBytesUp.Add(amount)
	}
}

func (r *registry) AddDown(session *connection, amount uint64) {
	if amount == 0 {
		return
	}
	session.bytesDown.Add(amount)
	counters := session.counters.Load()
	if counters != nil {
		counters.bytesDown.Add(amount)
	} else if session.direct.Load() {
		r.directBytesDown.Add(amount)
	}
}

func (r *registry) Finish(session *connection) {
	r.mu.Lock()
	delete(r.connections, session.id)
	r.mu.Unlock()

	if counters := session.counters.Load(); counters != nil {
		counters.active.Add(^uint64(0))
	} else if session.direct.Load() {
		r.directActive.Add(^uint64(0))
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
			SourceIPv6:  counters.adapter.SourceIPv6,
			IPv6IfIndex: counters.adapter.IPv6IfIndex,
			Connections: counters.active.Load(),
			BytesUp:     counters.bytesUp.Load(),
			BytesDown:   counters.bytesDown.Load(),
		}
		result.Adapters = append(result.Adapters, item)
		result.Total.Connections += item.Connections
		result.Total.BytesUp += item.BytesUp
		result.Total.BytesDown += item.BytesDown
	}
	result.Total.Connections += r.directActive.Load()
	result.Total.BytesUp += r.directBytesUp.Load()
	result.Total.BytesDown += r.directBytesDown.Load()
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
			Channel:   session.channel,
			Client:    session.client,
			Listener:  session.listener,
			Target:    session.target,
			Remote:    session.remote,
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
