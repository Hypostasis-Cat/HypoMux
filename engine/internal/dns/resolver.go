package dns

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/expiry"
)

const maxDNSMessageBytes = 64 * 1024

type DialFunc func(
	context.Context,
	string,
	string,
	Binding,
) (net.Conn, error)

type Query struct {
	Domain     string
	RecordType RecordType
	Binding    Binding
}

type Result struct {
	Addresses  []string   `json:"addresses,omitempty"`
	Domain     string     `json:"domain"`
	Address    string     `json:"address"`
	RecordType RecordType `json:"record_type"`
	Adapter    string     `json:"adapter"`
	Transport  string     `json:"transport"`
	Server     string     `json:"server"`
	Cached     bool       `json:"cached"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type Status struct {
	Policy             string     `json:"policy"`
	LegacyServers      []string   `json:"legacy_servers"`
	DoHEndpoints       []Endpoint `json:"doh_endpoints,omitempty"`
	CacheEntries       int        `json:"cache_entries"`
	Inflight           int        `json:"inflight"`
	Queries            uint64     `json:"queries"`
	CacheHits          uint64     `json:"cache_hits"`
	DoHSuccesses       uint64     `json:"doh_successes"`
	DoHFailures        uint64     `json:"doh_failures"`
	LegacySuccesses    uint64     `json:"legacy_successes"`
	LegacyFailures     uint64     `json:"legacy_failures"`
	AutomaticFallbacks uint64     `json:"automatic_fallbacks"`
}

type FallbackEvent struct {
	Adapter string `json:"adapter"`
	Policy  string `json:"policy"`
	Reason  string `json:"reason"`
}

type cacheKey struct {
	adapter    string
	sourceIP   string
	ifIndex    int
	domain     string
	recordType RecordType
}

type cacheEntry struct {
	result    Result
	expiresAt time.Time
}

type lookup struct {
	done   chan struct{}
	result Result
	err    error
}

type Resolver struct {
	root   context.Context
	config Config
	dial   DialFunc
	now    func() time.Time

	mu              sync.Mutex
	cache           map[cacheKey]cacheEntry
	cacheExpiry     expiry.Index[cacheKey]
	inflight        map[cacheKey]*lookup
	strictFailures  map[string]int
	fallbackEmitted map[string]bool
	onFallback      func(FallbackEvent)

	dohMu         sync.Mutex
	dohTransports map[dohPoolKey]*dohPoolEntry
	dohClosed     bool
	dohSequence   uint64

	queries            atomic.Uint64
	cacheHits          atomic.Uint64
	dohSuccesses       atomic.Uint64
	dohFailures        atomic.Uint64
	legacySuccesses    atomic.Uint64
	legacyFailures     atomic.Uint64
	automaticFallbacks atomic.Uint64
}

func New(root context.Context, config Config, dial DialFunc) (*Resolver, error) {
	if root == nil {
		root = context.Background()
	}
	if dial == nil {
		return nil, fmt.Errorf("DNS dial function is required")
	}
	normalized, err := NormalizeConfig(config)
	if err != nil {
		return nil, err
	}
	resolver := &Resolver{
		root:            root,
		config:          normalized,
		dial:            dial,
		now:             time.Now,
		cache:           make(map[cacheKey]cacheEntry),
		inflight:        make(map[cacheKey]*lookup),
		strictFailures:  make(map[string]int),
		fallbackEmitted: make(map[string]bool),
	}
	context.AfterFunc(root, resolver.closeDoHTransports)
	return resolver, nil
}

func (r *Resolver) SetFallbackHandler(handler func(FallbackEvent)) {
	r.mu.Lock()
	r.onFallback = handler
	r.mu.Unlock()
}

func (r *Resolver) Resolve(ctx context.Context, query Query) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	binding, err := NormalizeBinding(query.Binding)
	if err != nil {
		return Result{}, err
	}
	domain, err := normalizeDomain(query.Domain)
	if err != nil {
		return Result{}, err
	}
	recordType, _, err := normalizeRecordType(query.RecordType)
	if err != nil {
		return Result{}, err
	}
	key := cacheKey{
		adapter:    binding.Name,
		sourceIP:   binding.SourceIP,
		ifIndex:    binding.IfIndex,
		domain:     domain,
		recordType: recordType,
	}
	r.queries.Add(1)

	now := r.now().UTC()
	r.mu.Lock()
	if entry, ok := r.cache[key]; ok {
		if !entry.expiresAt.After(now) {
			delete(r.cache, key)
			r.cacheExpiry.Delete(key)
		} else {
			result := entry.result
			result.Addresses = append([]string(nil), result.Addresses...)
			result.Cached = true
			r.mu.Unlock()
			r.cacheHits.Add(1)
			return result, nil
		}
	}
	call := r.inflight[key]
	if call == nil {
		call = &lookup{done: make(chan struct{})}
		r.inflight[key] = call
		go r.runLookup(key, binding, call)
	}
	r.mu.Unlock()

	select {
	case <-call.done:
		result := call.result
		result.Addresses = append([]string(nil), result.Addresses...)
		return result, call.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-r.root.Done():
		return Result{}, r.root.Err()
	}
}

func (r *Resolver) Status() Status {
	now := r.now().UTC()
	r.mu.Lock()
	r.removeExpiredLocked(now)
	cacheEntries := len(r.cache)
	inflight := len(r.inflight)
	r.mu.Unlock()
	return Status{
		Policy:             r.config.Policy,
		LegacyServers:      append([]string(nil), r.config.LegacyServers...),
		DoHEndpoints:       Endpoints(r.config.Policy),
		CacheEntries:       cacheEntries,
		Inflight:           inflight,
		Queries:            r.queries.Load(),
		CacheHits:          r.cacheHits.Load(),
		DoHSuccesses:       r.dohSuccesses.Load(),
		DoHFailures:        r.dohFailures.Load(),
		LegacySuccesses:    r.legacySuccesses.Load(),
		LegacyFailures:     r.legacyFailures.Load(),
		AutomaticFallbacks: r.automaticFallbacks.Load(),
	}
}

func (r *Resolver) runLookup(key cacheKey, binding Binding, call *lookup) {
	ctx, cancel := context.WithTimeout(r.root, r.config.QueryTimeout)
	result, ttl, err := r.resolveUncached(ctx, key.domain, key.recordType, binding)
	cancel()
	if err == nil {
		result.Domain = key.domain
		result.RecordType = key.recordType
		result.Adapter = binding.Name
		if ttl > r.config.CacheTTL {
			ttl = r.config.CacheTTL
		}
		if ttl > 0 {
			expiresAt := r.now().UTC().Add(ttl)
			result.ExpiresAt = &expiresAt
		}
	}

	r.mu.Lock()
	if err == nil && result.ExpiresAt != nil {
		r.makeCacheRoomLocked(r.now().UTC())
		r.cache[key] = cacheEntry{result: result, expiresAt: *result.ExpiresAt}
		r.cacheExpiry.Set(key, *result.ExpiresAt)
	}
	call.result = result
	call.err = err
	delete(r.inflight, key)
	close(call.done)
	r.mu.Unlock()
}

func (r *Resolver) resolveUncached(
	ctx context.Context,
	domain string,
	recordType RecordType,
	binding Binding,
) (Result, time.Duration, error) {
	_, wireType, _ := normalizeRecordType(recordType)
	if r.config.Policy != PolicyOff && r.config.Policy != PolicySystem {
		// Auto must leave a real deadline budget for source-bound traditional
		// DNS when HTTPS resolvers silently drop packets.
		dohCtx := ctx
		cancelDoH := func() {}
		if r.config.Policy == PolicyAuto {
			dohCtx, cancelDoH = context.WithTimeout(ctx, remainingQueryBudget(ctx, r.config.QueryTimeout)/2)
		}
		result, ttl, err := r.resolveDoH(dohCtx, domain, wireType, binding)
		cancelDoH()
		if err == nil {
			r.recordDoHSuccess(binding)
			return result, ttl, nil
		}
		if ctx.Err() != nil {
			return Result{}, 0, ctx.Err()
		}
		r.dohFailures.Add(1)
		if r.config.Policy != PolicyAuto {
			r.recordStrictFailure(binding, err)
			return Result{}, 0, fmt.Errorf("DoH resolution failed: %w", err)
		}
		r.automaticFallbacks.Add(1)
		legacyResult, legacyTTL, legacyErr := r.resolveLegacy(ctx, domain, wireType, binding)
		if legacyErr == nil {
			return legacyResult, legacyTTL, nil
		}
		return Result{}, 0, errors.Join(
			fmt.Errorf("automatic DoH failed: %w", err),
			fmt.Errorf("traditional DNS fallback failed: %w", legacyErr),
		)
	}
	return r.resolveLegacy(ctx, domain, wireType, binding)
}

const (
	// maxDoHRace limits concurrent DoH dials per batch; see resolveDoH.
	maxDoHRace = 2
)

func (r *Resolver) resolveDoH(
	ctx context.Context,
	domain string,
	recordType uint16,
	binding Binding,
) (Result, time.Duration, error) {
	endpoints := Endpoints(r.config.Policy)
	if len(endpoints) == 0 {
		return Result{}, 0, fmt.Errorf("no DoH endpoint configured")
	}
	// Race endpoints in small batches, each with its own bounded time budget.
	// A single shared deadline would let the first endpoints consume the whole
	// budget while blocked, starving later (possibly healthy) endpoints of any
	// attempt; per-batch budgets guarantee every batch gets a real chance.
	batchCount := (len(endpoints) + maxDoHRace - 1) / maxDoHRace
	batchBudget := remainingQueryBudget(ctx, r.config.QueryTimeout) / time.Duration(batchCount)
	var failures []error
	for start := 0; start < len(endpoints); start += maxDoHRace {
		end := start + maxDoHRace
		if end > len(endpoints) {
			end = len(endpoints)
		}
		batch := endpoints[start:end]
		batchCtx, cancel := context.WithTimeout(ctx, batchBudget)
		result, ttl, err := r.raceDoHBatch(
			batchCtx,
			batch,
			domain,
			recordType,
			binding,
		)
		cancel()
		if err == nil {
			return result, ttl, nil
		}
		failures = append(failures, err)
		if ctx.Err() != nil {
			break
		}
	}
	return Result{}, 0, errors.Join(failures...)
}

func (r *Resolver) raceDoHBatch(
	ctx context.Context,
	batch []Endpoint,
	domain string,
	recordType uint16,
	binding Binding,
) (Result, time.Duration, error) {
	type outcome struct {
		result Result
		ttl    time.Duration
		err    error
	}
	outcomes := make(chan outcome, len(batch))
	for _, endpoint := range batch {
		endpoint := endpoint
		go func() {
			result, ttl, err := r.queryDoH(
				ctx,
				domain,
				recordType,
				binding,
				endpoint,
			)
			outcomes <- outcome{result: result, ttl: ttl, err: err}
		}()
	}
	var failures []error
	for range batch {
		select {
		case outcome := <-outcomes:
			if outcome.err == nil {
				r.dohSuccesses.Add(1)
				return outcome.result, outcome.ttl, nil
			}
			failures = append(failures, outcome.err)
		case <-ctx.Done():
			return Result{}, 0, ctx.Err()
		}
	}
	return Result{}, 0, errors.Join(failures...)
}

func (r *Resolver) resolveLegacy(
	ctx context.Context,
	domain string,
	recordType uint16,
	binding Binding,
) (Result, time.Duration, error) {
	var failures []error
	servers := LegacyServers(r.config, binding)
	for index, server := range servers {
		// Reserve time for TCP and later servers even when the first DNS
		// server drops UDP instead of returning an error.
		attemptBudget := remainingQueryBudget(ctx, r.config.QueryTimeout) / time.Duration(2*(len(servers)-index))
		udpCtx, cancelUDP := context.WithTimeout(ctx, attemptBudget)
		result, ttl, err := r.queryUDP(udpCtx, domain, recordType, binding, server)
		cancelUDP()
		if err == nil {
			r.legacySuccesses.Add(1)
			return result, ttl, nil
		}
		failures = append(failures, fmt.Errorf("udp/%s: %w", server, err))
		tcpCtx, cancelTCP := context.WithTimeout(ctx, attemptBudget)
		result, ttl, err = r.queryTCP(tcpCtx, domain, recordType, binding, server)
		cancelTCP()
		if err == nil {
			r.legacySuccesses.Add(1)
			return result, ttl, nil
		}
		failures = append(failures, fmt.Errorf("tcp/%s: %w", server, err))
		if ctx.Err() != nil {
			break
		}
	}
	if ctx.Err() != nil {
		return Result{}, 0, ctx.Err()
	}
	r.legacyFailures.Add(1)
	return Result{}, 0, errors.Join(failures...)
}

func remainingQueryBudget(ctx context.Context, fallback time.Duration) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		return max(0, time.Until(deadline))
	}
	return fallback
}

func (r *Resolver) queryUDP(
	ctx context.Context,
	domain string,
	recordType uint16,
	binding Binding,
	server string,
) (Result, time.Duration, error) {
	packet, queryID, err := buildQuery(domain, recordType)
	if err != nil {
		return Result{}, 0, err
	}
	address := net.JoinHostPort(server, "53")
	connection, err := r.dial(ctx, "udp4", address, binding)
	if err != nil {
		return Result{}, 0, err
	}
	defer connection.Close()
	setContextDeadline(connection, ctx)
	if _, err := connection.Write(packet); err != nil {
		return Result{}, 0, err
	}
	response := make([]byte, 4096)
	count, err := connection.Read(response)
	if err != nil {
		return Result{}, 0, err
	}
	answer, err := parseResponse(response[:count], queryID, recordType)
	if err != nil {
		return Result{}, 0, err
	}
	return Result{
		Address:   answer.Address,
		Addresses: append([]string(nil), answer.Addresses...),
		Transport: "udp",
		Server:    address,
	}, answer.TTL, nil
}

func (r *Resolver) queryTCP(
	ctx context.Context,
	domain string,
	recordType uint16,
	binding Binding,
	server string,
) (Result, time.Duration, error) {
	packet, queryID, err := buildQuery(domain, recordType)
	if err != nil {
		return Result{}, 0, err
	}
	address := net.JoinHostPort(server, "53")
	connection, err := r.dial(ctx, "tcp4", address, binding)
	if err != nil {
		return Result{}, 0, err
	}
	defer connection.Close()
	setContextDeadline(connection, ctx)
	if err := binary.Write(connection, binary.BigEndian, uint16(len(packet))); err != nil {
		return Result{}, 0, err
	}
	if _, err := connection.Write(packet); err != nil {
		return Result{}, 0, err
	}
	var length uint16
	if err := binary.Read(connection, binary.BigEndian, &length); err != nil {
		return Result{}, 0, err
	}
	if length == 0 || int(length) > maxDNSMessageBytes {
		return Result{}, 0, fmt.Errorf("invalid DNS TCP response length %d", length)
	}
	response := make([]byte, int(length))
	if _, err := io.ReadFull(connection, response); err != nil {
		return Result{}, 0, err
	}
	answer, err := parseResponse(response, queryID, recordType)
	if err != nil {
		return Result{}, 0, err
	}
	return Result{
		Address:   answer.Address,
		Addresses: append([]string(nil), answer.Addresses...),
		Transport: "tcp",
		Server:    address,
	}, answer.TTL, nil
}

func (r *Resolver) queryDoH(
	ctx context.Context,
	domain string,
	recordType uint16,
	binding Binding,
	endpoint Endpoint,
) (Result, time.Duration, error) {
	packet, queryID, err := buildQuery(domain, recordType)
	if err != nil {
		return Result{}, 0, err
	}
	address := net.JoinHostPort(endpoint.IP, "443")
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://"+endpoint.Host+endpoint.Path,
		bytes.NewReader(packet),
	)
	if err != nil {
		return Result{}, 0, err
	}
	request.Host = endpoint.Host
	request.Header.Set("Accept", "application/dns-message")
	request.Header.Set("Content-Type", "application/dns-message")
	request.Header.Set("User-Agent", "HypoMux-Engine/1")
	transport, err := r.doHTransport(binding, endpoint)
	if err != nil {
		return Result{}, 0, err
	}
	// RoundTrip does not follow redirects to unconfigured resolvers.
	response, err := transport.RoundTrip(request)
	if err != nil {
		return Result{}, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Result{}, 0, fmt.Errorf("DoH HTTP status %d", response.StatusCode)
	}
	if contentType := strings.TrimSpace(response.Header.Get("Content-Type")); contentType != "" {
		mediaType, _, parseErr := mime.ParseMediaType(contentType)
		if parseErr != nil || !strings.EqualFold(mediaType, "application/dns-message") {
			return Result{}, 0, fmt.Errorf("unexpected DoH content type %q", contentType)
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDNSMessageBytes+1))
	if err != nil {
		return Result{}, 0, err
	}
	if len(body) > maxDNSMessageBytes {
		return Result{}, 0, fmt.Errorf("DoH response exceeds %d bytes", maxDNSMessageBytes)
	}
	answer, err := parseResponse(body, queryID, recordType)
	if err != nil {
		return Result{}, 0, err
	}
	return Result{
		Address:   answer.Address,
		Addresses: append([]string(nil), answer.Addresses...),
		Transport: "doh",
		Server:    endpoint.Host + "@" + address,
	}, answer.TTL, nil
}

func (r *Resolver) recordDoHSuccess(binding Binding) {
	key := bindingKey(binding)
	r.mu.Lock()
	delete(r.strictFailures, key)
	delete(r.fallbackEmitted, key)
	r.mu.Unlock()
}

func (r *Resolver) recordStrictFailure(binding Binding, failure error) {
	key := bindingKey(binding)
	var handler func(FallbackEvent)
	r.mu.Lock()
	r.strictFailures[key]++
	if r.strictFailures[key] >= r.config.FailureThreshold && !r.fallbackEmitted[key] {
		r.fallbackEmitted[key] = true
		handler = r.onFallback
	}
	r.mu.Unlock()
	if handler != nil {
		handler(FallbackEvent{
			Adapter: binding.Name,
			Policy:  r.config.Policy,
			Reason:  limitText(failure.Error(), 512),
		})
	}
}

func (r *Resolver) removeExpiredLocked(now time.Time) {
	for {
		key, ok := r.cacheExpiry.PopExpired(now)
		if !ok {
			return
		}
		delete(r.cache, key)
	}
}

func (r *Resolver) makeCacheRoomLocked(now time.Time) {
	r.removeExpiredLocked(now)
	for len(r.cache) >= r.config.MaxCacheEntries {
		oldestKey, _, ok := r.cacheExpiry.First()
		if !ok {
			return
		}
		r.cacheExpiry.Delete(oldestKey)
		delete(r.cache, oldestKey)
	}
}

func bindingKey(binding Binding) string {
	return binding.Name + "\x00" + binding.SourceIP + "\x00" + strconv.Itoa(binding.IfIndex)
}

func setContextDeadline(connection net.Conn, ctx context.Context) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
}

func limitText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}
