//go:build windows

package setup

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsFileMetadataAndHiddenReadOnlyRecovery(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "hypomux.exe")
	source := filepath.Join(dir, "payload")
	mustWrite(t, target, "old")
	mustWrite(t, source, "new")
	p := windowsPlatform{}
	wide, _ := windows.UTF16PtrFromString(target)
	if e := windows.SetFileAttributes(wide, windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_READONLY); e != nil {
		t.Fatal(e)
	}
	defer windows.SetFileAttributes(wide, windows.FILE_ATTRIBUTE_NORMAL)
	metadata, e := p.Metadata(target)
	if e != nil {
		t.Fatal(e)
	}
	if e = p.PrepareTarget(target); e != nil {
		t.Fatal(e)
	}
	if e = copyFile(source, target); e != nil {
		t.Fatal(e)
	}
	mustText(t, target, "new")
	if e = p.RestoreMetadata(target, metadata); e != nil {
		t.Fatal(e)
	}
	attrs, e := windows.GetFileAttributes(wide)
	if e != nil || attrs&(windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_READONLY) != (windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_READONLY) {
		t.Fatalf("attributes lost: %x %v", attrs, e)
	}
}
func TestWindowsRejectsHardLinkedPayload(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "hypomux.exe")
	mustWrite(t, target, "owned")
	if e := os.Link(target, filepath.Join(dir, "unrelated")); e != nil {
		t.Fatal(e)
	}
	if e := (windowsPlatform{}).ValidateTarget(target); e == nil {
		t.Fatal("hard link accepted")
	}
}
func TestWindowsProtectedUserJournal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if e := secureDirectory(dir, false, false); e != nil {
		t.Fatal(e)
	}
	if e := secureDirectory(dir, false, false); e != nil {
		t.Fatalf("own journal rejected: %v", e)
	}
	if e := os.WriteFile(filepath.Join(dir, "journal"), []byte("ok"), 0600); e != nil {
		t.Fatal(e)
	}
}

func TestWindowsCleanupKeepsUnknownFilesAndCurrentInstall(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old")
	current := filepath.Join(root, "current")
	for _, dir := range []string{old, current} {
		if e := os.MkdirAll(filepath.Join(dir, "bin"), 0700); e != nil {
			t.Fatal(e)
		}
		mustWrite(t, filepath.Join(dir, "hypomux.exe"), "desktop")
		mustWrite(t, filepath.Join(dir, "bin", "hypomux-engine.exe"), "engine")
	}
	mustWrite(t, filepath.Join(old, "user.txt"), "keep")
	if e := cleanupPrevious(old, current, windowsPlatform{coreBin: filepath.Join(root, "protected")}); e != nil {
		t.Fatal(e)
	}
	mustText(t, filepath.Join(old, "user.txt"), "keep")
	mustText(t, filepath.Join(current, "hypomux.exe"), "desktop")
	if _, e := os.Stat(filepath.Join(old, "hypomux.exe")); !os.IsNotExist(e) {
		t.Fatal("old desktop remains")
	}
	if e := cleanupPrevious(current, current, windowsPlatform{}); e != nil {
		t.Fatal(e)
	}
	mustText(t, filepath.Join(current, "hypomux.exe"), "desktop")
}
