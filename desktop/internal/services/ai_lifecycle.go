package services

import (
	"context"
	"errors"
	"time"
)

var errAIDisabled = errors.New("AI 功能已关闭，请在设置中开启后使用")

// bindSettings serializes initialization with commits. The callback never
// reads SettingsService, avoiding a settings/AI lock inversion.
func (s *AIService) bindSettings() {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	s.disabled = !s.settings.settings.AIEnabled
	s.settings.aiEnabledChanged = s.applyEnabled
}

func (s *AIService) availableLocked() error {
	if s.disabled {
		return errAIDisabled
	}
	if s.closed {
		return errors.New("应用正在退出")
	}
	return nil
}

// All provider requests and tool invocations are registered, including model
// discovery and connection tests which are independent of the chat worker.
func (s *AIService) requestLocked(parent context.Context, timeout time.Duration) (context.Context, func(), error) {
	if err := s.availableLocked(); err != nil {
		return nil, nil, err
	}
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	if s.requests == nil {
		s.requests = make(map[uint64]context.CancelFunc)
	}
	s.requestID++
	id := s.requestID
	s.requests[id] = cancel
	return ctx, func() {
		cancel()
		s.mu.Lock()
		delete(s.requests, id)
		s.mu.Unlock()
	}, nil
}

func (s *AIService) applyEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disabled = !enabled
	if !enabled {
		s.stopLocked()
	}
}

func (s *AIService) stopLocked() {
	if s.cancel != nil {
		s.cancel()
	}
	for _, cancel := range s.requests {
		cancel()
	}
	for id, approval := range s.approvals {
		delete(s.approvals, id)
		approval.reply <- false
	}
	if m := s.mcp; m != nil {
		s.mcp = nil
		m.cancel()
		_ = m.server.Close()
	}
}
