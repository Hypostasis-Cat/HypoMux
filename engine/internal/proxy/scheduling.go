package proxy

import "fmt"

// SchedulingConfig changes only the aggregation pool. Explicit NIC channels
// and established TCP flows retain their bindings. UDP flows stay bound except
// for evidence-based recovery while latency-first is selected.
type SchedulingConfig struct {
	Strategy string    `json:"strategy,omitempty"`
	Weighted bool      `json:"weighted"`
	Adapters []Adapter `json:"adapters"`
}

func ValidateScheduling(config SchedulingConfig) (SchedulingConfig, error) {
	strategy, err := NormalizeStrategy(config.Strategy, config.Weighted)
	if err != nil {
		return SchedulingConfig{}, err
	}
	config.Strategy = strategy
	config.Weighted = strategy == StrategyWeighted
	for _, adapter := range config.Adapters {
		if adapter.Weight < 1 || adapter.Weight > 100 {
			return SchedulingConfig{}, fmt.Errorf("adapter %q weight must be between 1 and 100", adapter.Name)
		}
	}
	normalized, err := normalizeConfig(Config{Adapters: config.Adapters})
	if err != nil {
		return SchedulingConfig{}, err
	}
	config.Adapters = normalized.Adapters
	return config, nil
}

func (s *scheduler) snapshot() SchedulingConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SchedulingConfig{Strategy: s.strategy, Weighted: s.weighted, Adapters: append([]Adapter(nil), s.adapters...)}
}

func (s *scheduler) update(config SchedulingConfig) SchedulingConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := SchedulingConfig{Strategy: s.strategy, Weighted: s.weighted, Adapters: s.adapters}
	if s.performance != nil {
		s.performance.mu.Lock()
		if s.strategy != config.Strategy || !s.performance.allocation.matches(config.Adapters) {
			s.performance.allocation = adaptiveAllocation{}
		}
		s.performance.mu.Unlock()
	}
	s.health.mu.Lock()
	for _, adapter := range config.Adapters {
		if s.health.adapters[adapter.Name] == nil {
			s.health.adapters[adapter.Name] = &adapterHealth{domains: make(map[string]*domainHealth)}
		}
	}
	s.health.mu.Unlock()
	s.adapters = config.Adapters
	s.weighted = config.Weighted
	s.strategy = config.Strategy
	s.next = 0
	s.currentWeight = make(map[string]int, len(config.Adapters))
	return previous
}

func (s *Server) UpdateScheduling(config SchedulingConfig) (SchedulingConfig, error) {
	next, err := ValidateScheduling(config)
	if err != nil {
		return SchedulingConfig{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return SchedulingConfig{}, fmt.Errorf("proxy engine is not running")
	}
	previous := s.scheduler.update(next)
	// Keep explicit NIC channels observable even when removed from the pool.
	bindings := append([]Adapter(nil), next.Adapters...)
	for _, scheduler := range s.schedulers {
		if scheduler != s.scheduler {
			bindings = append(bindings, scheduler.snapshot().Adapters...)
		}
	}
	s.performance.retain(bindings)
	return previous, nil
}
