//go:build windows

package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestReplaceRuleSetWaitsForOpenReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, []byte("old rules"), 0o600); err != nil {
		t.Fatal(err)
	}
	// os.Open on Windows does not share delete access, like a rule-set reader.
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	result := make(chan error, 1)
	go func() { result <- replaceFileAtomically(path, []byte("new rules"), 0o600) }()

	select {
	case err := <-result:
		t.Fatalf("replacement finished while its destination was open: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "old rules" {
		t.Fatalf("locked destination changed: data=%q err=%v", data, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("replacement did not recover after the reader closed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("replacement did not finish")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new rules" {
		t.Fatalf("new rules were not published: data=%q err=%v", data, err)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary file remains: %v", err)
	}
}

func TestReplaceRuleSetPersistentLockPreservesDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, []byte("old rules"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	started := time.Now()
	err = replaceFileAtomically(path, []byte("new rules"), 0o600)
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("expected a Windows file-lock error, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("replacement exceeded its retry budget: %v", elapsed)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "old rules" {
		t.Fatalf("failed replacement damaged old rules: data=%q err=%v", data, err)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed replacement left a temporary file: %v", err)
	}
}
