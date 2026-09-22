package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMTUSearchBoundaries(t *testing.T) {
	for _, limit := range []int{576, 1280, 1492, 1500, 9000} {
		calls := 0
		got, err := findMTU(context.Background(), 9000, func(_ context.Context, size int) (bool, error) { calls++; return size <= limit, nil })
		if err != nil || got != limit || calls > 20 {
			t.Fatalf("limit=%d got=%d calls=%d err=%v", limit, got, calls, err)
		}
	}
}
func TestMTUSearchDoesNotInterpretLossAsFragmentation(t *testing.T) {
	lost := errors.New("timeout")
	got, err := findMTU(context.Background(), 1500, func(_ context.Context, size int) (bool, error) {
		if size > 1000 {
			return false, lost
		}
		return true, nil
	})
	if got != 0 || !errors.Is(err, lost) {
		t.Fatalf("unsafe recommendation: %d, %v", got, err)
	}
}
func TestMTUSearchCancellationAndVerification(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := findMTU(ctx, 1500, func(context.Context, int) (bool, error) { t.Fatal("probe after cancel"); return true, nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	passes := 0
	got, err := findMTU(context.Background(), 576, func(context.Context, int) (bool, error) { passes++; return passes < 3, nil })
	if got != 0 || err == nil {
		t.Fatalf("unstable result accepted: %d %v", got, err)
	}
}
func TestMTUOriginalSurvivesRestartAndRepeatedChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "originals.json")
	service := &MTUService{path: path}
	if err := service.saveOriginal("adapter-a", 1500); err != nil {
		t.Fatal(err)
	}
	if err := service.saveOriginal("adapter-a", 1492); err != nil {
		t.Fatal(err)
	}
	restarted := &MTUService{path: path}
	values, err := restarted.originals()
	if err != nil || values["adapter-a"] != 1500 {
		t.Fatalf("lost baseline: %v %v", values, err)
	}
	if err := restarted.clearOriginal("adapter-a"); err != nil {
		t.Fatal(err)
	}
	if err := restarted.saveOriginal("adapter-a", 1480); err != nil {
		t.Fatal(err)
	}
	values, err = restarted.originals()
	if err != nil || values["adapter-a"] != 1480 {
		t.Fatalf("new baseline not saved: %v %v", values, err)
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restarted.saveOriginal("adapter-b", 1500); err == nil {
		t.Fatal("overwrote damaged recovery record")
	}
}
