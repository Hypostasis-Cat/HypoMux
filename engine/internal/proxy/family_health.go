package proxy

import "time"

type familyHealthKey struct {
	binding bindingKey
	family  string
}
type FamilyHealthSnapshot struct {
	State               string    `json:"state"`
	Successes           uint64    `json:"successes"`
	Failures            uint64    `json:"failures"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	CooldownUntil       time.Time `json:"cooldown_until,omitempty"`
}

func networkFamily(network string) string {
	if len(network) > 0 && network[len(network)-1] == '6' {
		return "ipv6"
	}
	return "ipv4"
}

func (h *healthTable) familyAvailable(a Adapter, network string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.families[familyHealthKey{performanceKey(a), networkFamily(network)}].CooldownUntil.After(h.now())
}

func (h *healthTable) recordFamily(a Adapter, network string, success bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.families == nil {
		h.families = make(map[familyHealthKey]FamilyHealthSnapshot)
	}
	k := familyHealthKey{performanceKey(a), networkFamily(network)}
	// Keep at most the current binding per adapter/family across renumbering.
	for old := range h.families {
		if old.binding.name == a.Name && old.family == k.family && old != k {
			delete(h.families, old)
		}
	}
	v := h.families[k]
	if success {
		v.Successes++
		v.ConsecutiveFailures = 0
		v.CooldownUntil = time.Time{}
	} else {
		v.Failures++
		v.ConsecutiveFailures++
		index := v.ConsecutiveFailures - 1
		if index >= len(adapterFailureBackoff) {
			index = len(adapterFailureBackoff) - 1
		}
		v.CooldownUntil = h.now().Add(adapterFailureBackoff[index])
	}
	h.families[k] = v
}

func (h *healthTable) familySnapshot(a Adapter, network string) FamilyHealthSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	v := h.families[familyHealthKey{performanceKey(a), networkFamily(network)}]
	v.State = "healthy"
	if !adapterSupportsNetwork(a, network) {
		v.State = "unavailable"
	} else if v.Successes == 0 && v.Failures == 0 {
		v.State = "unknown"
	} else if v.CooldownUntil.After(h.now()) {
		v.State = "cooldown"
	} else if v.ConsecutiveFailures > 0 {
		v.State = "probing"
	}
	return v
}
