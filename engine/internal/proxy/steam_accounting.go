package proxy

import (
	"sort"
	"time"
)

type steamFailureWindow struct {
	at, until time.Time
	count     int
}

func steamFailureKey(k cdnKey) string { return k.adapter + "/" + k.domain + ":" + k.port }

// Counts forwarded downstream bytes, including protocol overhead. These are
// attribution counters, not verified game bytes or a speedup estimate.
func (c *steamCDN) accountTransfer(k cdnKey, gen uint64, delta int, amount uint64, switched, failed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accountTransferLocked(k, gen, delta, amount, switched, failed)
}

func (c *steamCDN) accountTransferLocked(k cdnKey, gen uint64, delta int, amount uint64, switched, failed bool) {
	if !c.enabled || gen != c.generation {
		return
	}
	t := c.traffic[k]
	if t == nil {
		return
	}
	if switched {
		t.switchedActive = max(0, t.switchedActive+delta)
		t.switchedBytes += amount
		c.switchedBytes += amount
	} else {
		t.originalBytes += amount
		c.originalBytes += amount
	}
	if !failed || !switched {
		return
	}
	t.failures++
	c.transferFailures++
	key := steamFailureKey(k)
	state := c.failures[key]
	if state == nil {
		if len(c.failures) >= 512 {
			return
		}
		state = &steamFailureWindow{at: c.now()}
		c.failures[key] = state
	}
	if c.now().Sub(state.at) > time.Minute {
		state.at = c.now()
		state.count = 0
	}
	state.count++
	if state.count >= 3 {
		state.until = c.now().Add(time.Minute)
	}
}

// Session observations are discovery hints only. The caller must validate
// them with the current request on the destination adapter before admission.
func (c *steamCDN) observedCandidatesLocked(host, port string) []cdnCandidate {
	var result []cdnCandidate
	for k, expiry := range c.observed {
		if k.domain == host && k.port == port && expiry.After(c.now()) && publicCDNIP(k.ip) {
			result = append(result, cdnCandidate{k.ip, expiry})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ip < result[j].ip })
	return mergeSteamCandidatePool([][]cdnCandidate{rotateSteamCandidates(result, int(c.now().Unix()/30))}, 32)
}

func mergeSteamSources(dnsCandidates, observed []cdnCandidate) ([]cdnCandidate, map[string]string) {
	sources := map[string]string{}
	for _, candidate := range dnsCandidates {
		sources[candidate.ip] = "dns"
	}
	for _, candidate := range observed {
		if sources[candidate.ip] == "dns" {
			sources[candidate.ip] = "dns_and_session"
		} else {
			sources[candidate.ip] = "session"
		}
	}
	return mergeSteamCandidatePool([][]cdnCandidate{dnsCandidates, observed}, 32), sources
}
