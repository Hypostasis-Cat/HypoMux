package services

import (
	"reflect"
	"sync"
	"testing"
)

func TestSettingsFieldUpdatePreservesConcurrentOwners(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	s := NewSettingsService()
	stale := s.Get()
	current := cloneSettings(stale)
	current.Mode = "proxy"
	current.Strategy = "latency-first"
	current.SelectedAdapterIDs = []string{"new-nic"}
	current.AdapterWeights = map[string]int{"new-nic": 7}
	current.RoutingRules = []RoutingRule{{MatchType: "process", Value: "cs2.exe", Outbound: "direct"}}
	current.RoutingMatchOrder = []string{"ip", "domain", "process"}
	current.SteamCDNEnabled = true
	current.HTTPPort = 12345
	if _, err := s.Update(current); err != nil {
		t.Fatal(err)
	}
	stale.HideVirtualAdapters = false
	got, err := s.UpdateFields(stale, []string{"hide_virtual_adapters"})
	if err != nil {
		t.Fatal(err)
	}
	current.HideVirtualAdapters = false
	if !reflect.DeepEqual(got, current) {
		t.Fatalf("unrelated owner state overwritten: got %+v, want %+v", got, current)
	}
	if reloaded := NewSettingsService().settings; !reflect.DeepEqual(reloaded, current) {
		t.Fatalf("disk configuration differs: %+v", reloaded)
	}
}

func TestSettingsFieldUpdatesMergeUnderLock(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	s := NewSettingsService()
	a, b := s.Get(), s.Get()
	a.Language = "en"
	b.HTTPPort = 12345
	var wg sync.WaitGroup
	for _, change := range []struct {
		values AppSettings
		field  string
	}{{a, "language"}, {b, "http_port"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.UpdateFields(change.values, []string{change.field}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := s.Get(); got.Language != "en" || got.HTTPPort != 12345 {
		t.Fatalf("lost concurrent field edit: %+v", got)
	}
}

func TestSettingsFieldUpdateRejectsInvalidOrForeignFieldsAtomically(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	s := NewSettingsService()
	before := s.Get()
	for _, fields := range [][]string{{"language", "routing_rules"}, {"mode"}, {"strategy"}, {"autostart"}, {"unknown"}, {"http_port"}} {
		next := cloneSettings(before)
		next.Language = "en"
		next.HTTPPort = -1
		if _, err := s.UpdateFields(next, fields); err == nil {
			t.Fatalf("accepted invalid update: %v", fields)
		}
		if !reflect.DeepEqual(s.Get(), before) {
			t.Fatal("rejected update changed configuration")
		}
	}
	// Use a directory as the target to force a commit failure.
	s.path = t.TempDir()
	next := cloneSettings(before)
	next.Language = "en"
	if _, err := s.UpdateFields(next, []string{"language"}); err == nil {
		t.Fatal("ignored failed persistence")
	}
	if !reflect.DeepEqual(s.Get(), before) {
		t.Fatal("failed persistence changed memory")
	}
}
