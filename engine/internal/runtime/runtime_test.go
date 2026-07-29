package runtime

import (
	"testing"
	"time"
)

func TestRuntimeStartsStopped(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	engineRuntime := New(func() time.Time { return now })

	got := engineRuntime.Snapshot()
	if got.State != StateStopped {
		t.Fatalf("initial state = %q, want %q", got.State, StateStopped)
	}
	if got.Sequence != 0 {
		t.Fatalf("initial sequence = %d, want 0", got.Sequence)
	}
	if !got.StateChangedAt.Equal(now.UTC()) {
		t.Fatalf("initial timestamp = %v, want %v", got.StateChangedAt, now.UTC())
	}
}

func TestRuntimeAcceptsExpectedLifecycle(t *testing.T) {
	engineRuntime := New(time.Now)
	path := []State{
		StateStarting,
		StateRunning,
		StateDegraded,
		StateRunning,
		StateStopping,
		StateStopped,
	}

	for index, state := range path {
		change, err := engineRuntime.Transition(state, "test")
		if err != nil {
			t.Fatalf("transition to %q failed: %v", state, err)
		}
		if change.Current.State != state {
			t.Fatalf("transition state = %q, want %q", change.Current.State, state)
		}
		if change.Current.Sequence != uint64(index+1) {
			t.Fatalf("sequence = %d, want %d", change.Current.Sequence, index+1)
		}
	}
}

func TestRuntimeRejectsInvalidTransition(t *testing.T) {
	engineRuntime := New(time.Now)

	if _, err := engineRuntime.Transition(StateRunning, "skip startup"); err == nil {
		t.Fatal("stopped -> running unexpectedly succeeded")
	}
	if got := engineRuntime.Snapshot().State; got != StateStopped {
		t.Fatalf("state changed after rejected transition: %q", got)
	}
}
