package proxy

import "fmt"

// SchedulingConfig changes only the aggregation pool. Explicit NIC channels
// and established TCP/UDP flows retain their bindings.
type SchedulingConfig struct {
	Weighted bool      `json:"weighted"`
	Adapters []Adapter `json:"adapters"`
}

func ValidateScheduling(config SchedulingConfig) (SchedulingConfig, error) {
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
	return SchedulingConfig{Weighted: s.weighted, Adapters: append([]Adapter(nil), s.adapters...)}
}

func (s *scheduler) update(config SchedulingConfig) SchedulingConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := SchedulingConfig{Weighted: s.weighted, Adapters: s.adapters}
	s.health.mu.Lock()
	for _, adapter := range config.Adapters {
		if s.health.adapters[adapter.Name] == nil {
			s.health.adapters[adapter.Name] = &adapterHealth{domains: make(map[string]*domainHealth)}
		}
	}
	s.health.mu.Unlock()
	s.adapters = config.Adapters
	s.weighted = config.Weighted
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
	return s.scheduler.update(next), nil
}
