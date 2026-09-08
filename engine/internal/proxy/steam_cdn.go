package proxy

import (
	"context"
	"crypto/tls"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

const cdnLifetime = 10 * time.Minute

func (s *Server) ConfigureSteamCDN(enabled *bool, reset bool) SteamCDNStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cdn == nil {
		return SteamCDNStatus{Entries: []SteamCDNEntry{}}
	}
	if enabled != nil {
		s.cdn.configure(*enabled, reset)
	} else if reset {
		s.cdn.configure(s.cdn.active(), true)
	}
	return s.cdn.snapshot()
}

type SteamCDNEntry struct {
	Adapter       string    `json:"adapter"`
	Domain        string    `json:"domain"`
	Port          string    `json:"port"`
	IP            string    `json:"ip"`
	DownloadBPS   float64   `json:"download_bps"`
	Samples       uint64    `json:"samples"`
	Selections    uint64    `json:"selections"`
	CooldownUntil time.Time `json:"cooldown_until"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type SteamCDNStatus struct {
	Enabled      bool            `json:"enabled"`
	Probing      int             `json:"probing"`
	Replacements uint64          `json:"replacements"`
	Fallbacks    uint64          `json:"fallbacks"`
	Entries      []SteamCDNEntry `json:"entries"`
}

type cdnKey struct{ adapter, domain, port, ip string }
type cdnCandidate struct {
	ip      string
	expires time.Time
}

// All mutable learning state belongs to one engine run. A generation prevents
// cancelled probes and old connections from repopulating a reset/disabled pool.
type steamCDN struct {
	mu                      sync.Mutex
	root                    context.Context
	ctx                     context.Context
	cancel                  context.CancelFunc
	enabled                 bool
	generation              uint64
	entries                 map[cdnKey]*SteamCDNEntry
	discovery               map[string]time.Time
	probing                 int
	replacements, fallbacks uint64
	now                     func() time.Time
}

func newSteamCDN(root context.Context, enabled bool) *steamCDN {
	c := &steamCDN{root: root, now: time.Now}
	c.configure(enabled, true)
	return c
}

func (c *steamCDN) configure(enabled, reset bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil && c.enabled == enabled && !reset {
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	c.ctx, c.cancel = context.WithCancel(c.root)
	c.enabled = enabled
	c.generation++
	c.entries = make(map[cdnKey]*SteamCDNEntry)
	c.discovery = make(map[string]time.Time)
	c.probing = 0
	c.replacements, c.fallbacks = 0, 0
	if !enabled {
		c.cancel()
	}
}

func (c *steamCDN) active() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled && c.ctx.Err() == nil
}

func steamDownloadHost(host string) bool {
	host = normalizeDomain(host)
	if len(host) > 253 || host == "" {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return strings.HasSuffix(host, ".steamcontent.com") ||
		host == "cdn.mileweb.cs.steampowered.com.8686c.com" ||
		host == "cdn-ws.content.steamchina.com" ||
		host == "cdn-qc.content.steamchina.com" ||
		host == "cdn-ali.content.steamchina.com"
}

func publicCDNIP(value string) bool {
	ip := net.ParseIP(value)
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		return v4[0] != 0 && !(v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127) &&
			!(v4[0] == 198 && (v4[1] == 18 || v4[1] == 19))
	}
	return true
}

func (c *steamCDN) pruneLocked() {
	now := c.now()
	for key, entry := range c.entries {
		if !entry.ExpiresAt.After(now) {
			delete(c.entries, key)
		}
	}
	for key, expiry := range c.discovery {
		if !expiry.After(now) {
			delete(c.discovery, key)
		}
	}
}

func (c *steamCDN) snapshot() SteamCDNStatus {
	result := SteamCDNStatus{Entries: []SteamCDNEntry{}}
	if c == nil {
		return result
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
	result.Enabled, result.Probing = c.enabled, c.probing
	result.Replacements, result.Fallbacks = c.replacements, c.fallbacks
	for _, entry := range c.entries {
		result.Entries = append(result.Entries, *entry)
	}
	sort.Slice(result.Entries, func(i, j int) bool {
		a, b := result.Entries[i], result.Entries[j]
		if a.Adapter != b.Adapter {
			return a.Adapter < b.Adapter
		}
		if a.Domain != b.Domain {
			return a.Domain < b.Domain
		}
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		return a.IP < b.IP
	})
	return result
}

func (c *steamCDN) choose(adapter, domain, port string) (string, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled {
		return "", c.generation
	}
	c.pruneLocked()
	var best, explore *SteamCDNEntry
	var selections uint64
	for _, entry := range c.entries {
		if entry.Adapter != adapter || entry.Domain != domain || entry.Port != port || entry.CooldownUntil.After(c.now()) {
			continue
		}
		selections += entry.Selections
		if best == nil || entry.DownloadBPS > best.DownloadBPS || entry.DownloadBPS == best.DownloadBPS && entry.IP < best.IP {
			best = entry
		}
		if explore == nil || entry.Selections < explore.Selections || entry.Selections == explore.Selections && entry.IP < explore.IP {
			explore = entry
		}
	}
	if best == nil {
		return "", c.generation
	}
	// Give each candidate a chance, then retain 1/8 exploration for changing CDN load.
	if explore.Selections == 0 || selections%8 == 0 {
		best = explore
	}
	best.Selections++
	return best.IP, c.generation
}

func (c *steamCDN) observe(key cdnKey, generation uint64, bytes uint64, elapsed time.Duration) {
	if bytes < 64*1024 || elapsed < 100*time.Millisecond {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled || generation != c.generation {
		return
	}
	entry := c.entries[key]
	if entry == nil || !entry.ExpiresAt.After(c.now()) {
		return
	}
	bps := float64(bytes) / elapsed.Seconds()
	if entry.Samples == 0 {
		entry.DownloadBPS = bps
	} else {
		entry.DownloadBPS = entry.DownloadBPS*0.75 + bps*0.25
	}
	entry.Samples++
}

func (c *steamCDN) outcome(key cdnKey, generation uint64, success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled || generation != c.generation {
		return
	}
	if success {
		c.replacements++
		return
	}
	c.fallbacks++
	if entry := c.entries[key]; entry != nil {
		entry.CooldownUntil = c.now().Add(time.Minute)
		entry.DownloadBPS = 0
		entry.Samples = 0
	}
}

func (s *Server) discoverSteamCDN(host, port string) {
	c := s.cdn
	if !c.active() {
		return
	}
	c.mu.Lock()
	c.pruneLocked()
	key := net.JoinHostPort(host, port)
	if !c.enabled || c.probing >= 2 || len(c.discovery) >= 64 || c.discovery[key].After(c.now()) {
		c.mu.Unlock()
		return
	}
	c.discovery[key] = c.now().Add(30 * time.Second)
	c.probing++
	generation, root := c.generation, c.ctx
	c.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(root, 20*time.Second)
		defer cancel()
		defer func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if generation == c.generation {
				c.probing--
			}
		}()
		// Optional discovery must not emit the ordinary resolver's strict-DNS
		// fallback events. Its entire DNS lifetime ends with this probe job.
		probeResolver, err := dns.New(ctx, s.config.DNS, s.dialDNS)
		if err != nil {
			return
		}
		candidates := s.steamCandidates(ctx, host, probeResolver)
		for _, adapter := range s.config.Adapters {
			for _, candidate := range candidates {
				if ctx.Err() != nil {
					return
				}
				if !adapterSupportsNetwork(adapter, networkForIP("tcp", net.ParseIP(candidate.ip))) {
					continue
				}
				if !s.verifySteamCandidate(ctx, adapter, host, candidate.ip) {
					continue
				}
				c.mu.Lock()
				if c.enabled && generation == c.generation && len(c.entries) < 512 && candidate.expires.After(c.now()) {
					entryKey := cdnKey{adapter.Name, host, port, candidate.ip}
					if old := c.entries[entryKey]; old != nil {
						old.ExpiresAt = candidate.expires
					} else {
						c.entries[entryKey] = &SteamCDNEntry{Adapter: adapter.Name, Domain: host, Port: port, IP: candidate.ip, ExpiresAt: candidate.expires}
					}
					if expiry := c.discovery[key]; candidate.expires.After(expiry) {
						c.discovery[key] = candidate.expires
					}
				}
				c.mu.Unlock()
			}
		}
	}()
}

func (s *Server) steamCandidates(ctx context.Context, host string, resolver *dns.Resolver) []cdnCandidate {
	var groups [][]cdnCandidate
	for _, adapter := range s.config.Adapters {
		var group []cdnCandidate
		for _, record := range []dns.RecordType{dns.RecordA, dns.RecordAAAA} {
			if ctx.Err() != nil {
				break
			}
			if record == dns.RecordAAAA && adapter.SourceIPv6 == "" {
				continue
			}
			queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			answer, err := resolver.Resolve(queryCtx, dns.Query{Domain: host, RecordType: record, Binding: adapterDNSBinding(adapter)})
			cancel()
			if err != nil || answer.ExpiresAt == nil {
				continue
			}
			expiry := time.Now().Add(cdnLifetime)
			if answer.ExpiresAt.Before(expiry) {
				expiry = *answer.ExpiresAt
			}
			seen := map[string]bool{}
			for _, ip := range append([]string{answer.Address}, answer.Addresses...) {
				if !publicCDNIP(ip) || seen[ip] {
					continue
				}
				seen[ip] = true
				group = append(group, cdnCandidate{ip, expiry})
			}
		}
		groups = append(groups, group)
	}
	// Interleave NIC-local answers so a large first answer cannot exclude the
	// other networks' CDN choices from the bounded candidate pool.
	var result []cdnCandidate
	seen := map[string]bool{}
	for index := 0; index < 64 && len(result) < 8; index++ {
		for _, group := range groups {
			if index >= len(group) {
				continue
			}
			candidate := group[index]
			if seen[candidate.ip] {
				continue
			}
			seen[candidate.ip] = true
			result = append(result, candidate)
			if len(result) == 8 {
				break
			}
		}
	}
	return result
}

func (s *Server) verifySteamCandidate(ctx context.Context, adapter Adapter, host, ip string) bool {
	ctx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	dialer, err := boundNetworkDialer(adapter, 750*time.Millisecond, networkForIP("tcp", net.ParseIP(ip)))
	if err != nil {
		return false
	}
	conn, err := s.dialTCP(ctx, dialer, net.JoinHostPort(ip, "443"))
	if err != nil {
		return false
	}
	defer conn.Close()
	secure := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	return secure.HandshakeContext(ctx) == nil
}

// prepareSteamCDN retains the working original connection until a verified
// candidate connects. It never replays application data or changes the NIC.
func (s *Server) prepareSteamCDN(session *connection, original net.Conn, adapter Adapter, host, port string) net.Conn {
	if session.channel == ChannelDirect || !s.cdn.active() || (port != "80" && port != "443") || !steamDownloadHost(host) {
		return original
	}
	host = normalizeDomain(host)
	originalIP, _, _ := net.SplitHostPort(original.RemoteAddr().String())
	// LAN content caches/local transfers must remain local even when another
	// network resolves the same Steam hostname to a public CDN.
	if !publicCDNIP(originalIP) {
		return original
	}
	s.discoverSteamCDN(host, port)
	ip, generation := s.cdn.choose(adapter.Name, host, port)
	actualIP := originalIP
	if ip != "" && ip != originalIP {
		key := cdnKey{adapter.Name, host, port, ip}
		ctx, cancel := context.WithTimeout(s.ctx, 750*time.Millisecond)
		dialer, err := boundNetworkDialer(adapter, 750*time.Millisecond, networkForIP("tcp", net.ParseIP(ip)))
		var replacement net.Conn
		if err == nil {
			if s.shouldTuneTCP(session.channel) {
				enableTCPDialerTuning(dialer)
			}
			replacement, err = s.dialTCP(ctx, dialer, net.JoinHostPort(ip, port))
		}
		cancel()
		if err == nil {
			s.cdn.mu.Lock()
			session.mu.Lock()
			usable := s.cdn.enabled && generation == s.cdn.generation && s.ctx.Err() == nil
			if usable {
				if s.shouldTuneTCP(session.channel) {
					tuneTCPConnection(replacement)
				}
				session.upstream, session.remote = replacement, replacement.RemoteAddr().String()
			}
			session.mu.Unlock()
			s.cdn.mu.Unlock()
			if usable {
				_ = original.Close()
				original, actualIP = replacement, ip
				s.cdn.outcome(key, generation, true)
			} else {
				_ = replacement.Close()
			}
		} else {
			s.cdn.outcome(key, generation, false)
		}
	}
	session.cdnKey = cdnKey{adapter.Name, host, port, actualIP}
	session.cdnGeneration = generation
	return original
}
