//go:build windows

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func payloadDigestList(t *testing.T, dir string, contents map[string]string) string {
	t.Helper()
	parts := make([]string, 0, len(payloadDigestNames))
	for _, name := range payloadDigestNames {
		content, ok := contents[name]
		if !ok {
			continue
		}
		path := filepath.Join(dir, name)
		mustWrite(t, path, content)
		h, e := hashFile(path)
		if e != nil {
			t.Fatal(e)
		}
		parts = append(parts, name+"="+h)
	}
	return strings.Join(parts, " ")
}

func TestVerifyPayloadIntegrityAcceptsMatchingDigests(t *testing.T) {
	dir := t.TempDir()
	expected := payloadDigestList(t, dir, map[string]string{
		"hypomux-engine.exe": "engine-bytes",
		"sing-box.exe":       "sidecar-bytes",
		"wintun.dll":         "driver-bytes",
	})
	if err := verifyPayloadIntegrity(dir, expected, false); err != nil {
		t.Fatalf("matching payload rejected: %v", err)
	}
}

func TestVerifyPayloadIntegrityRejectsTamperedPayload(t *testing.T) {
	dir := t.TempDir()
	expected := payloadDigestList(t, dir, map[string]string{
		"hypomux-engine.exe": "engine-bytes",
		"sing-box.exe":       "sidecar-bytes",
		"wintun.dll":         "driver-bytes",
	})
	mustWrite(t, filepath.Join(dir, "sing-box.exe"), "replaced sidecar")
	err := verifyPayloadIntegrity(dir, expected, false)
	if err == nil || !strings.Contains(err.Error(), "sing-box.exe") {
		t.Fatalf("tampered payload accepted: %v", err)
	}
}

func TestVerifyPayloadIntegrityRejectsMissingDigest(t *testing.T) {
	dir := t.TempDir()
	expected := payloadDigestList(t, dir, map[string]string{
		"hypomux-engine.exe": "engine-bytes",
		"sing-box.exe":       "sidecar-bytes",
	})
	if err := verifyPayloadIntegrity(dir, expected, false); err == nil {
		t.Fatal("incomplete digest list accepted")
	}
}

func TestVerifyPayloadIntegrityRejectsUnknownAndMalformedEntries(t *testing.T) {
	dir := t.TempDir()
	expected := payloadDigestList(t, dir, map[string]string{
		"hypomux-engine.exe": "engine-bytes",
		"sing-box.exe":       "sidecar-bytes",
		"wintun.dll":         "driver-bytes",
	})
	for _, entry := range []string{"cmd.exe=run", "sing-box.exe"} {
		if err := verifyPayloadIntegrity(dir, expected+" "+entry, false); err == nil {
			t.Fatalf("entry %q accepted", entry)
		}
	}
}

func TestVerifyPayloadIntegrityRequiresDigestList(t *testing.T) {
	dir := t.TempDir()
	if err := verifyPayloadIntegrity(dir, "", false); err == nil {
		t.Fatal("empty digest list accepted")
	}
}

func TestVerifyPayloadIntegrityRejectsUntrustedMachineStaging(t *testing.T) {
	// A plain temp directory is owned by the current user; machine scope must
	// refuse it whether or not the digests would match, because its bytes
	// could still be replaced between verification and the copy.
	dir := t.TempDir()
	expected := payloadDigestList(t, dir, map[string]string{
		"hypomux-engine.exe": "engine-bytes",
		"sing-box.exe":       "sidecar-bytes",
		"wintun.dll":         "driver-bytes",
	})
	err := verifyPayloadIntegrity(dir, expected, true)
	if err == nil || !strings.Contains(err.Error(), "untrusted payload staging") {
		t.Fatalf("user-owned staging accepted at machine scope: %v", err)
	}
}

func TestVerifyPayloadIntegrityRejectsLinkedPayload(t *testing.T) {
	dir := t.TempDir()
	expected := payloadDigestList(t, dir, map[string]string{
		"hypomux-engine.exe": "engine-bytes",
		"sing-box.exe":       "sidecar-bytes",
		"wintun.dll":         "driver-bytes",
	})
	outer := t.TempDir()
	link := filepath.Join(outer, "wintun.dll")
	if err := os.Symlink(filepath.Join(dir, "wintun.dll"), link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	// Replace the staged name with the reparse point.
	if err := os.Remove(filepath.Join(dir, "wintun.dll")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(link, filepath.Join(dir, "wintun.dll")); err != nil {
		t.Fatal(err)
	}
	err := verifyPayloadIntegrity(dir, expected, false)
	if err == nil || !strings.Contains(err.Error(), "wintun.dll") {
		t.Fatalf("linked payload accepted: %v", err)
	}
}
