package services

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsDirectoryUsesHiddenHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HYPOMUX_DATA_DIR", "")
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	if got, want := settingsDirectory(), filepath.Join(home, ".hypomux"); !strings.EqualFold(filepath.Clean(got), filepath.Clean(want)) {
		t.Fatalf("settingsDirectory() = %q; want %q", got, want)
	}
}
