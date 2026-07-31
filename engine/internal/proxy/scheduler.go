package proxy

import (
	"sync"
)

type scheduler struct {
	mu            sync.Mutex
	adapters      []Adapter
	weighted      bool
	next          int
	currentWeight map[string]int
	health        *healthTable
}

func newScheduler(
	adapters []Adapter,
	weighted bool,
	sharedHealth ...*healthTable,
) *scheduler {
	health := (*healthTable)(nil)
	if len(sharedHealth) > 0 {
		health = sharedHealth[0]
	}
	if health == nil {
		health = newHealthTable(adapters)
	}
	return &scheduler{
		adapters:      append([]Adapter(nil), adapters...),
		weighted:      weighted,
		currentWeight: make(map[string]int, len(adapters)),
		health:        health,
	}
}

func (s *scheduler) Select(excluded map[string]struct{}) (Adapter, bool) {
	return s.SelectForDomain(excluded, "")
}

func (s *scheduler) SelectForDomain(
	excluded map[string]struct{},
	domain string,
) (Adapter, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	candidates := s.health.candidates(s.adapters, excluded, domain)
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
	s.health.recordLocalFailure(name)
}

func (s *scheduler) MarkSuccess(name string, domain ...string) {
	value := ""
	if len(domain) > 0 {
		value = domain[0]
	}
	s.health.recordSuccess(name, value)
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
