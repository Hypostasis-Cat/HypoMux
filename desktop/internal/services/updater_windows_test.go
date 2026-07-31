//go:build windows

package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDownloadedInstallerPathRequiresOwnedTempDirectory(t *testing.T) {
	owned, err := os.MkdirTemp("", "HypoMuxUpdate-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(owned)
	installer := filepath.Join(owned, "HypoMux_Setup_2.3.1.exe")
	if err := os.WriteFile(installer, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateDownloadedInstallerPath(installer); err != nil {
		t.Fatalf("owned installer rejected: %v", err)
	}

	foreign, err := os.MkdirTemp("", "ForeignUpdate-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(foreign)
	foreignInstaller := filepath.Join(foreign, "HypoMux_Setup_2.3.1.exe")
	if err := os.WriteFile(foreignInstaller, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateDownloadedInstallerPath(foreignInstaller); err == nil {
		t.Fatal("foreign temporary installer was accepted")
	}
}

func TestValidateDownloadedInstallerPathRejectsUnexpectedName(t *testing.T) {
	owned, err := os.MkdirTemp("", "HypoMuxUpdate-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(owned)
	installer := filepath.Join(owned, "payload.exe")
	if err := os.WriteFile(installer, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateDownloadedInstallerPath(installer); err == nil {
		t.Fatal("unexpected installer name was accepted")
	}
}
