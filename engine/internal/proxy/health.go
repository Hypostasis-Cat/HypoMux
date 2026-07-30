package proxy

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	domainFailureThreshold = 2
	domainEvidenceTTL      = 10 * time.Minute
	domainQuarantineTTL    = 30 * time.Minute
)

var adapterFailureBackoff = [...]time.Duration{
	2 * time.Second,
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
}

type adapterHealth struct {
	consecutiveFailures int
	successes           uint64
	failures            uint64
	lastSuccessAt       time.Time
	lastFailureAt       time.Time
	cooldownUntil       time.Time
	domains             map[string]*domainHealth
}

type domainHealth struct {
	evidence  int
	expiresAt time.Time
}

type healthTable struct {
	mu       sync.Mutex
	adapters map[string]*adapterHealth
	now      func() time.Time
}

type adapterHealthSnapshot struct {
	State               string
	ConsecutiveFailures int
	Successes           uint64
	Failures            uint64
	LastSuccessAt       time.Time
	LastFailureAt       time.Time
	CooldownUntil       time.Time
	DomainQuarantines   int
}

type domainQuarantineSnapshot struct {
	Adapter   string
	Domain    string
	Evidence  int
	ExpiresAt time.Time
}

func newHealthTable(adapters []Adapter) *healthTable {
	table := &healthTable{
		adapters: make(map[string]*adapterHealth, len(adapters)),
		now:      time.Now,
	}
	for _, adapter := range adapters {
		table.adapters[adapter.Name] = &adapterHealth{
			domains: make(map[string]*domainHealth),
		}
	}
	return table
}

func (h *healthTable) candidates(
	adapters []Adapter,
	excluded map[string]struct{},
	domain string,
) []Adapter {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := h.now()
	domain = normalizeDomain(domain)
	healthy := make([]Adapter, 0, len(adapters))
	domainFallback := make([]Adapter, 0, len(adapters))
	recovery := make([]Adapter, 0, len(adapters))
	all := make([]Adapter, 0, len(adapters))
	for _, adapter := range adapters {
		if _, skip := excluded[adapter.Name]; skip {
			continue
		}
		state := h.adapters[adapter.Name]
		if state == nil {
			continue
		}
		h.pruneExpiredDomains(state, now)
		all = append(all, adapter)
		cooling := state.cooldownUntil.After(now)
		quarantined := h.domainQuarantined(state, domain, now)
		if !cooling {
			domainFallback = append(domainFallback, adapter)
			if !quarantined {
				healthy = append(healthy, adapter)
			}
		}
		if !quarantined {
			recovery = append(recovery, adapter)
		}
	}
	if len(healthy) > 0 {
		return healthy
	}
	// Domain isolation must not turn a destination into a total outage.
	if len(domainFallback) > 0 {
		return domainFallback
	}
	// If every link is cooling down, keep one recovery path available.
	if len(recovery) > 0 {
		return earliestCooldown(recovery, h.adapters)
	}
	return earliestCooldown(all, h.adapters)
}

func (h *healthTable) recordLocalFailure(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.adapters[name]
	if state == nil {
		return
	}
	now := h.now().UTC()
	state.failures++
	state.consecutiveFailures++
	state.lastFailureAt = now
	index := state.consecutiveFailures - 1
	if index >= len(adapterFailureBackoff) {
		index = len(adapterFailureBackoff) - 1
	}
	state.cooldownUntil = now.Add(adapterFailureBackoff[index])
}

func (h *healthTable) recordSuccess(name string, domain string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.adapters[name]
	if state == nil {
		return
	}
	now := h.now().UTC()
	state.successes++
	state.consecutiveFailures = 0
	state.cooldownUntil = time.Time{}
	state.lastSuccessAt = now
	delete(state.domains, normalizeDomain(domain))
}

func (h *healthTable) recordComparativeDomainFailure(
	name string,
	domain string,
) {
	domain = normalizeDomain(domain)
	if domain == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.adapters[name]
	if state == nil {
		return
	}
	now := h.now().UTC()
	entry := state.domains[domain]
	if entry == nil || (!entry.expiresAt.IsZero() && !entry.expiresAt.After(now)) {
		entry = &domainHealth{}
		state.domains[domain] = entry
	}
	entry.evidence++
	if entry.evidence >= domainFailureThreshold {
		entry.expiresAt = now.Add(domainQuarantineTTL)
	} else {
		entry.expiresAt = now.Add(domainEvidenceTTL)
	}
}

func (h *healthTable) snapshot() (
	map[string]adapterHealthSnapshot,
	[]domainQuarantineSnapshot,
) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now().UTC()
	adapters := make(map[string]adapterHealthSnapshot, len(h.adapters))
	var quarantines []domainQuarantineSnapshot
	for name, state := range h.adapters {
		h.pruneExpiredDomains(state, now)
		domainCount := 0
		for domain, entry := range state.domains {
			if entry.evidence < domainFailureThreshold ||
				!entry.expiresAt.After(now) {
				continue
			}
			domainCount++
			quarantines = append(quarantines, domainQuarantineSnapshot{
				Adapter:   name,
				Domain:    domain,
				Evidence:  entry.evidence,
				ExpiresAt: entry.expiresAt,
			})
		}
		stateName := "healthy"
		if state.cooldownUntil.After(now) {
			stateName = "cooldown"
		} else if state.consecutiveFailures > 0 {
			stateName = "probing"
		}
		adapters[name] = adapterHealthSnapshot{
			State:               stateName,
			ConsecutiveFailures: state.consecutiveFailures,
			Successes:           state.successes,
			Failures:            state.failures,
			LastSuccessAt:       state.lastSuccessAt,
			LastFailureAt:       state.lastFailureAt,
			CooldownUntil:       state.cooldownUntil,
			DomainQuarantines:   domainCount,
		}
	}
	sort.Slice(quarantines, func(i int, j int) bool {
		if quarantines[i].Adapter == quarantines[j].Adapter {
			return quarantines[i].Domain < quarantines[j].Domain
		}
		return quarantines[i].Adapter < quarantines[j].Adapter
	})
	return adapters, quarantines
}

func (h *healthTable) domainQuarantined(
	state *adapterHealth,
	domain string,
	now time.Time,
) bool {
	if domain == "" {
		return false
	}
	entry := state.domains[domain]
	return entry != nil &&
		entry.evidence >= domainFailureThreshold &&
		entry.expiresAt.After(now)
}

func (h *healthTable) pruneExpiredDomains(
	state *adapterHealth,
	now time.Time,
) {
	for domain, entry := range state.domains {
		if !entry.expiresAt.IsZero() && !entry.expiresAt.After(now) {
			delete(state.domains, domain)
		}
	}
}

func earliestCooldown(
	adapters []Adapter,
	states map[string]*adapterHealth,
) []Adapter {
	if len(adapters) < 2 {
		return adapters
	}
	selected := adapters[0]
	for _, adapter := range adapters[1:] {
		if states[adapter.Name].cooldownUntil.Before(
			states[selected.Name].cooldownUntil,
		) {
			selected = adapter
		}
	}
	return []Adapter{selected}
}

func normalizeDomain(domain string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
}

func isLocalConnectFailure(err error) bool {
	if err == nil {
		return false
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch uintptr(errno) {
	// Windows WSAEINVAL, WSAEADDRNOTAVAIL, WSAENETDOWN, WSAENETUNREACH,
	// WSAEHOSTUNREACH, ERROR_INVALID_NETNAME, ERROR_NETWORK_UNREACHABLE,
	// ERROR_HOST_UNREACHABLE. Windows can report ERROR_INVALID_NETNAME when
	// probing with ICMP and WSAEINVAL when dialing a socket that remains
	// pinned to an interface that has just been disabled.
	case 10022, 10049, 10050, 10051, 10065, 1214, 1231, 1232:
		return true
	// Common Unix/macOS equivalents used by Go unit and development builds.
	case 49, 51, 64, 65, 99, 100, 101, 113:
		return true
	default:
		return false
	}
}
