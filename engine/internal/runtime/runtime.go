package runtime

import (
	"fmt"
	"sync"
	"time"
)

type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateDegraded State = "degraded"
	StateStopping State = "stopping"
	StateFailed   State = "failed"
)

type Snapshot struct {
	State          State     `json:"state"`
	Sequence       uint64    `json:"sequence"`
	StateChangedAt time.Time `json:"state_changed_at"`
	Reason         string    `json:"reason,omitempty"`
}

type StateChange struct {
	Previous State
	Current  Snapshot
}

// Runtime owns the canonical engine lifecycle independently of any UI toolkit.
type Runtime struct {
	mu       sync.RWMutex
	snapshot Snapshot
	now      func() time.Time
}

func New(now func() time.Time) *Runtime {
	if now == nil {
		now = time.Now
	}
	return &Runtime{
		snapshot: Snapshot{
			State:          StateStopped,
			StateChangedAt: now().UTC(),
		},
		now: now,
	}
}

func (r *Runtime) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot
}

func (r *Runtime) Transition(next State, reason string) (StateChange, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	previous := r.snapshot.State
	if previous == next {
		return StateChange{Previous: previous, Current: r.snapshot}, nil
	}
	if !canTransition(previous, next) {
		return StateChange{}, fmt.Errorf("invalid engine state transition %q -> %q", previous, next)
	}

	r.snapshot = Snapshot{
		State:          next,
		Sequence:       r.snapshot.Sequence + 1,
		StateChangedAt: r.now().UTC(),
		Reason:         reason,
	}
	return StateChange{Previous: previous, Current: r.snapshot}, nil
}

func canTransition(current, next State) bool {
	switch current {
	case StateStopped:
		return next == StateStarting
	case StateStarting:
		return next == StateRunning || next == StateStopping || next == StateFailed
	case StateRunning:
		return next == StateDegraded || next == StateStopping || next == StateFailed
	case StateDegraded:
		return next == StateRunning || next == StateStopping || next == StateFailed
	case StateStopping:
		return next == StateStopped || next == StateFailed
	case StateFailed:
		return next == StateStarting || next == StateStopped
	default:
		return false
	}
}
