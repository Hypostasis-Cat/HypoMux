//go:build windows

package tun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/fileintegrity"
)

func TestCopyPinnedConfigStagesExactBytesAndRemovesFile(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.json")
	data := []byte(`{"route":{"final":"aggregation"}}`)
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := fileintegrity.SHA256Bytes(data)
	staged, remove, err := copyPinnedConfig(source, directory, digest)
	if err != nil {
		t.Fatal(err)
	}
	if staged == source {
		t.Fatal("configuration was not copied to a separate protected path")
	}
	stagedData, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	if string(stagedData) != string(data) {
		t.Fatalf("staged bytes = %q", stagedData)
	}
	remove()
	remove()
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged config was not removed: %v", err)
	}
}

func TestCopyPinnedConfigRejectsDigestMismatch(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.json")
	if err := os.WriteFile(source, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := copyPinnedConfig(source, directory, strings.Repeat("0", 64)); err == nil {
		t.Fatal("configuration with a mismatched digest was staged")
	}
}

func TestTrustedConfigACLHasOnlySystemAndAdministrators(t *testing.T) {
	for _, forbidden := range []string{";;;AU)", ";;;BU)", ";;;WD)"} {
		if strings.Contains(trustedConfigDirectorySDDL, forbidden) {
			t.Fatalf("trusted config ACL contains broad principal %q: %s", forbidden, trustedConfigDirectorySDDL)
		}
	}
	for _, required := range []string{"O:BA", ";;;SY)", ";;;BA)"} {
		if !strings.Contains(trustedConfigDirectorySDDL, required) {
			t.Fatalf("trusted config ACL is missing %q: %s", required, trustedConfigDirectorySDDL)
		}
	}
}
