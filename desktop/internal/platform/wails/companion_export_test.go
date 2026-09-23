package wails

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestCompanionExportValidation(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("skin data"))
	for _, name := range []string{"../escape.muxskin", `C:\escape.muxskin`, "run.exe", "bad..name.muxskin"} {
		if _, err := decodeCompanionExport(name, encoded); err == nil {
			t.Fatalf("accepted invalid filename %q", name)
		}
	}
	if _, err := decodeCompanionExport("test.muxskin", "invalid base64!"); err == nil {
		t.Fatal("accepted invalid base64")
	}
	if _, err := decodeCompanionExport("SKIN_SPEC.md", base64.StdEncoding.EncodeToString(make([]byte, 65537))); err == nil {
		t.Fatal("accepted oversized guide")
	}
	if _, err := decodeCompanionExport("test.muxskin", encoded); err != nil {
		t.Fatal(err)
	}
}

func TestCompanionExportReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.muxskin")
	for _, data := range []string{"first", "replacement"} {
		if err := writeCompanionExport(path, []byte(data)); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != data {
			t.Fatalf("read export: %q, %v", got, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("temporary export was not cleaned up")
	}
}
