package proxy

import (
	"net"
	"time"
)

// Discovery keeps a wider pool, but each adapter still validates at most eight
// usable alternatives. Filtering before truncation prevents another address
// family, expired hints or the original node from consuming its trial budget.
func steamCandidatesForAdapter(pool []cdnCandidate, adapter Adapter, original string, now time.Time) []cdnCandidate {
	result := make([]cdnCandidate, 0, 8)
	seen := make(map[string]bool)
	for _, candidate := range pool {
		ip := net.ParseIP(candidate.ip)
		if ip == nil || !publicCDNIP(candidate.ip) || !candidate.expires.After(now) {
			continue
		}
		candidate.ip = ip.String()
		if candidate.ip == original || seen[candidate.ip] || !adapterSupportsNetwork(adapter, networkForIP("tcp", ip)) {
			continue
		}
		seen[candidate.ip] = true
		result = append(result, candidate)
		if len(result) == 8 {
			break
		}
	}
	return result
}
