//go:build windows

package main

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestServiceSessionAllowsOnlyActivelyConnectedUsers(t *testing.T) {
	states := []serviceSessionState{
		serviceSessionActive,
		serviceSessionConnected,
		serviceSessionConnectQuery,
		serviceSessionShadow,
		serviceSessionDisconnected,
		serviceSessionIdle,
		serviceSessionListen,
		serviceSessionReset,
		serviceSessionDown,
		serviceSessionInit,
	}
	for _, state := range states {
		want := state == serviceSessionActive
		if got := serviceSessionIsActive(state); got != want {
			t.Fatalf("state %s allowed=%v, want %v", state, got, want)
		}
	}
}

func TestServiceSessionStateNamesRemainDiagnostic(t *testing.T) {
	if got := serviceSessionDisconnected.String(); got != "disconnected" {
		t.Fatalf("unexpected disconnected state name %q", got)
	}
	if got := serviceSessionState(99).String(); got != "unknown-99" {
		t.Fatalf("unexpected unknown state name %q", got)
	}
}

func TestQueryCurrentProcessSessionState(t *testing.T) {
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &sessionID); err != nil {
		t.Fatal(err)
	}
	state, err := queryServiceSessionState(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if state > serviceSessionInit {
		t.Fatalf("current process session %d returned invalid state %s", sessionID, state)
	}
}
