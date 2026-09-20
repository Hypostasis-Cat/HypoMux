package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
)

func TestSchedulingCommitRollsBackOnlyFailedPersistence(t *testing.T) {
	for _, stage := range []string{"success", "apply", "persist", "rollback"} {
		t.Run(stage, func(t *testing.T) {
			previous := json.RawMessage(`{"strategy":"adaptive-throughput","weighted":false,"adapters":[{"name":"original"}]}`)
			calls, saves := 0, 0
			request := func(ctx context.Context, method string, params any, result any) error {
				calls++
				if method != "engine.scheduling" {
					t.Fatal(method)
				}
				if calls == 1 {
					if stage == "apply" {
						return errors.New("apply failed")
					}
					*result.(*json.RawMessage) = previous
				} else {
					if string(params.(json.RawMessage)) != string(previous) {
						t.Fatal("rollback lost original bindings")
					}
					if stage == "rollback" {
						return errors.New("rollback failed")
					}
				}
				return nil
			}
			err := commitScheduling(context.Background(), request, map[string]any{"weighted": true}, func() error {
				saves++
				if stage == "persist" || stage == "rollback" {
					return errors.New("disk full")
				}
				return nil
			})
			if (err == nil) != (stage == "success") {
				t.Fatal(stage, err)
			}
			if stage == "apply" && saves != 0 {
				t.Fatal("persisted rejected update")
			}
			if (stage == "persist" || stage == "rollback") && calls != 2 {
				t.Fatal("missing rollback")
			}
			if stage == "success" && calls != 1 {
				t.Fatal("rolled back successful update")
			}
		})
	}
}

func TestSchedulingAdaptersUsesCurrentOSBinding(t *testing.T) {
	available := []AdapterView{{ID: "a", Name: "a", Address: "192.0.2.1", IfIndex: 3, Operational: true, Weight: 1}}
	requested := []AdapterView{{ID: "a", Name: "spoofed", Address: "203.0.113.1", IfIndex: 99, Weight: 7, Selected: true}}
	selected, err := schedulingAdapters(requested, available)
	if err != nil {
		t.Fatal(err)
	}
	if selected[0].Name != "a" || selected[0].IfIndex != 3 || selected[0].Address != "192.0.2.1" || selected[0].Weight != 7 {
		t.Fatal(selected)
	}
	for _, bad := range [][]AdapterView{nil, {{ID: "missing", Selected: true}}, {requested[0], requested[0]}} {
		if _, err := schedulingAdapters(bad, available); err == nil {
			t.Fatal("accepted invalid pool", bad)
		}
	}
}

func TestSchedulingStoppedPersistenceAndFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HYPOMUX_DATA_DIR", dir)
	settings := NewSettingsService()
	adapters := NewAdapterService(settings)
	s := &EngineService{settings: settings, adapters: adapters, client: engineclient.New(), lifecycleGate: make(chan struct{}, 1)}
	adapters.saveRuntimeSelection = s.saveRuntimeSelection
	if _, err := adapters.SaveSelection("proxy", true, nil); err != nil {
		t.Fatal(err)
	}
	if !NewSettingsService().Get().Weighted {
		t.Fatal("save did not persist")
	}
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	settings.path = filepath.Join(blocker, "settings.json")
	if _, err := adapters.SaveSelection("proxy", false, nil); err == nil {
		t.Fatal("expected persistence error")
	}
	if !settings.Get().Weighted {
		t.Fatal("failed save changed preferences")
	}
	if s.client.Hello().ProtocolVersion != 0 {
		t.Fatal("settings started Core")
	}
}
