package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func auditRuleSetService(t *testing.T) (*RuleSetService, RuleSet) {
	t.Helper()
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	service := NewRuleSetService(settings, NewAdapterService(settings))
	set := RuleSet{ID: "one", Name: "One", URL: "https://example.com/rules", Outbound: "direct"}
	if _, err := service.Save([]RuleSet{set}); err != nil {
		t.Fatal(err)
	}
	service.fetch = func(context.Context, RuleSet) (ruleSetFetchResult, error) {
		return ruleSetFetchResult{Body: []byte("payload:\n  - old.example\n"), ETag: "v1"}, nil
	}
	return service, set
}

func TestRuleSetUpdateRechecksEditsAfterDownload(t *testing.T) {
	for _, change := range []string{"disable", "delete", "url", "failed-delete"} {
		t.Run(change, func(t *testing.T) {
			service, set := auditRuleSetService(t)
			service.fetch = func(context.Context, RuleSet) (ruleSetFetchResult, error) {
				next := []RuleSet{set}
				switch change {
				case "disable":
					next[0].Disabled = true
					next[0].Priority = 90
				case "url":
					next[0].URL = "https://example.com/new"
				default:
					next = nil
				}
				if _, err := service.Save(next); err != nil {
					t.Fatal(err)
				}
				if change == "failed-delete" {
					return ruleSetFetchResult{}, errors.New("download failed")
				}
				return ruleSetFetchResult{Body: []byte("payload:\n  - new.example\n")}, nil
			}
			_, err := service.Update(set.ID)
			got := service.List()
			if change == "disable" {
				if err != nil || len(got) != 1 || !got[0].Disabled || got[0].Priority != 90 || got[0].EntryCount != 1 {
					t.Fatalf("lost edit: %v %+v", err, got)
				}
			} else {
				if err == nil {
					t.Fatal("stale download accepted")
				}
				if change == "url" {
					if len(got) != 1 || got[0].URL != "https://example.com/new" || got[0].EntryCount != 0 {
						t.Fatalf("lost URL edit: %+v", got)
					}
				} else if len(got) != 0 {
					t.Fatalf("resurrected deleted set: %+v", got)
				}
			}
		})
	}
}

func TestRuleSetSavePreservesLatestIngestionStatus(t *testing.T) {
	service, set := auditRuleSetService(t)
	if _, err := service.Update(set.ID); err != nil {
		t.Fatal(err)
	}
	set.Disabled = true // Stale frontend row still has no ingestion metadata.
	saved, err := service.Save([]RuleSet{set})
	if err != nil {
		t.Fatal(err)
	}
	if saved[0].ETag != "v1" || saved[0].EntryCount != 1 || !saved[0].Disabled {
		t.Fatalf("lost status: %+v", saved)
	}
}

func TestRuleSetUpdateRestoresSourceAndWatchedFilesOnCommitFailure(t *testing.T) {
	for _, initial := range []bool{false, true} {
		t.Run(map[bool]string{false: "first-download", true: "existing-cache"}[initial], func(t *testing.T) {
			service, set := auditRuleSetService(t)
			if initial {
				if _, err := service.Update(set.ID); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := writeSingBoxRuleSetPlan(nil, service.List(), []string{"direct", "aggregation"}, true)
			if err != nil {
				t.Fatal(err)
			}
			paths := ruleSetPaths(plan)
			before := readRuleSetFiles(t, paths)
			source := externalRuleSetSourcePathIn(ruleSetDirectory(), set)
			sourceBefore, _ := os.ReadFile(source)
			statusBefore := service.List()
			// An existing directory cannot be atomically replaced with settings JSON.
			service.settings.path = filepath.Join(t.TempDir(), "blocked")
			if err := os.Mkdir(service.settings.path, 0700); err != nil {
				t.Fatal(err)
			}
			service.fetch = func(context.Context, RuleSet) (ruleSetFetchResult, error) {
				return ruleSetFetchResult{Body: []byte("payload:\n  - new.example\n")}, nil
			}
			if _, err := service.Update(set.ID); err == nil {
				t.Fatal("commit failure not reported")
			}
			sourceAfter, readErr := os.ReadFile(source)
			if !initial && !errors.Is(readErr, os.ErrNotExist) {
				t.Fatalf("failed first download left source: %v", readErr)
			}
			if string(sourceAfter) != string(sourceBefore) {
				t.Fatal("source was not rolled back")
			}
			if !reflect.DeepEqual(before, readRuleSetFiles(t, paths)) {
				t.Fatal("watched rules changed after failure")
			}
			if !reflect.DeepEqual(statusBefore, service.List()) {
				t.Fatal("settings changed after failure")
			}
		})
	}
}

func TestRuleSetMissingCacheDisablesConditionalFetch(t *testing.T) {
	service, set := auditRuleSetService(t)
	if _, err := service.Update(set.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(externalRuleSetSourcePathIn(ruleSetDirectory(), set)); err != nil {
		t.Fatal(err)
	}
	service.fetch = func(_ context.Context, requested RuleSet) (ruleSetFetchResult, error) {
		if requested.ETag != "" || requested.LastModified != "" {
			t.Fatal("sent validators for missing payload")
		}
		return ruleSetFetchResult{Body: []byte("payload:\n  - restored.example\n")}, nil
	}
	if _, err := service.Update(set.ID); err != nil {
		t.Fatal(err)
	}
	entries, err := service.Entries(set.ID, "restored", 0, 100)
	if err != nil || entries.Total != 1 {
		t.Fatalf("cache not restored: %+v %v", entries, err)
	}
}

func TestSubscriptionsRejectInvalidRegexAndAcceptScalar(t *testing.T) {
	for _, body := range []string{`{"version":3,"rules":[{"domain_regex":["["]}]}`, "payload:\n  - 'DOMAIN-REGEX,['\n"} {
		if _, err := parseRuleSetSubscription([]byte(body)); err == nil {
			t.Fatal("invalid regex accepted")
		}
	}
	parsed, err := parseRuleSetSubscription([]byte(`{"version":3,"rules":[{"domain":"example.com"}]}`))
	if err != nil || parsed.Entries != 1 {
		t.Fatalf("scalar source rejected: %+v %v", parsed, err)
	}
}
