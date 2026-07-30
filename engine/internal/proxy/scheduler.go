package proxy

import (
	"sync"
	"time"
)

const adapterFailureCooldown = 15 * time.Second

type scheduler struct {
	mu               sync.Mutex
	adapters         []Adapter
	weighted         bool
	next             int
	currentWeight    map[string]int
	unavailableUntil map[string]time.Time
}

func newScheduler(adapters []Adapter, weighted bool) *scheduler {
	return &scheduler{
		adapters:         append([]Adapter(nil), adapters...),
		weighted:         weighted,
		currentWeight:    make(map[string]int, len(adapters)),
		unavailableUntil: make(map[string]time.Time),
	}
}

func (s *scheduler) Select(excluded map[string]struct{}) (Adapter, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	available := make([]Adapter, 0, len(s.adapters))
	fallback := make([]Adapter, 0, len(s.adapters))
	for _, adapter := range s.adapters {
		if _, skip := excluded[adapter.Name]; skip {
			continue
		}
		fallback = append(fallback, adapter)
		if !s.unavailableUntil[adapter.Name].After(now) {
			available = append(available, adapter)
		}
	}
	candidates := available
	if len(candidates) == 0 {
		candidates = fallback
	}
	if len(candidates) == 0 {
		return Adapter{}, false
	}
	if s.weighted {
		return s.selectWeighted(candidates), true
	}
	selected := candidates[s.next%len(candidates)]
	s.next = (s.next + 1) % len(candidates)
	return selected, true
}

func (s *scheduler) MarkFailure(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unavailableUntil[name] = time.Now().Add(adapterFailureCooldown)
}

func (s *scheduler) MarkSuccess(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.unavailableUntil, name)
}

func (s *scheduler) selectWeighted(candidates []Adapter) Adapter {
	total := 0
	selected := candidates[0]
	for _, adapter := range candidates {
		total += adapter.Weight
		s.currentWeight[adapter.Name] += adapter.Weight
		if s.currentWeight[adapter.Name] > s.currentWeight[selected.Name] {
			selected = adapter
		}
	}
	s.currentWeight[selected.Name] -= total
	return selected
}
