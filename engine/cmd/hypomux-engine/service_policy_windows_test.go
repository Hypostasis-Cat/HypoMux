//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/fileintegrity"
)

func TestBuildCoreServicePolicyPinsInstalledDesktopAndSidecar(t *testing.T) {
	temporaryRoot := t.TempDir()
	installRoot := filepath.Join(temporaryRoot, "D-Apps", "HypoMux")
	binDirectory := filepath.Join(temporaryRoot, "ProgramData", "HypoMux", "Core", "bin")
	if err := os.MkdirAll(binDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(installRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	enginePath := filepath.Join(binDirectory, "hypomux-engine.exe")
	desktopPath := filepath.Join(installRoot, "hypomux.exe")
	tunPath := filepath.Join(binDirectory, "sing-box.exe")
	for path, contents := range map[string]string{
		enginePath:  "engine",
		desktopPath: "desktop",
		tunPath:     "sidecar",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	policy, err := buildCoreServicePolicy(enginePath, desktopPath)
	if err != nil {
		t.Fatal(err)
	}
	// 临时目录可能包含 8.3 短名（如 ADMINI~1），而策略路径经
	// GetFinalPathNameByHandle 规范化为长名，期望值必须同样规范化。
	expectedDesktop, err := canonicalRegularFile(desktopPath, "expected desktop executable")
	if err != nil {
		t.Fatal(err)
	}
	expectedTun, err := canonicalRegularFile(tunPath, "expected sing-box executable")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(policy.DesktopPath, expectedDesktop) ||
		!strings.EqualFold(policy.TunExecutable, expectedTun) {
		t.Fatalf("unexpected policy paths: %#v", policy)
	}
	if err := fileintegrity.VerifySHA256(policy.DesktopPath, policy.DesktopSHA256); err != nil {
		t.Fatalf("desktop pin: %v", err)
	}
	if err := fileintegrity.VerifySHA256(policy.TunExecutable, policy.TunExecutableSHA256); err != nil {
		t.Fatalf("sidecar pin: %v", err)
	}
}

func TestValidateServiceClientExecutableRequiresPathAndDigest(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = canonicalRegularFile(executable, "test executable")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := fileintegrity.SHA256(executable)
	if err != nil {
		t.Fatal(err)
	}
	policy := coreServicePolicy{DesktopPath: executable, DesktopSHA256: digest}
	if err := validateServiceClientExecutable(uint32(os.Getpid()), policy); err != nil {
		t.Fatalf("current trusted process was rejected: %v", err)
	}
	policy.DesktopSHA256 = strings.Repeat("0", 64)
	if err := validateServiceClientExecutable(uint32(os.Getpid()), policy); err == nil {
		t.Fatal("client with a mismatched pinned digest was accepted")
	}
	policy.DesktopPath = filepath.Join(t.TempDir(), "different.exe")
	if err := os.WriteFile(policy.DesktopPath, []byte("different executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateServiceClientExecutable(uint32(os.Getpid()), policy); err == nil {
		t.Fatal("client at an untrusted path was accepted")
	}
}

func TestPathWithinRejectsSiblingPrefix(t *testing.T) {
	root := filepath.Join(t.TempDir(), "HypoMux")
	if !pathWithin(root, filepath.Join(root, "bin", "hypomux-engine.exe")) {
		t.Fatal("installed child path was rejected")
	}
	if pathWithin(root, filepath.Join(filepath.Dir(root), "HypoMux-Evil", "hypomux-engine.exe")) {
		t.Fatal("sibling prefix escaped the install root")
	}
}

func TestFixedLocalVolumeRejectsUNCDesktopPath(t *testing.T) {
	if err := requireFixedLocalVolume(`\\server\share\HypoMux\hypomux.exe`, "desktop"); err == nil {
		t.Fatal("UNC desktop path was accepted for a machine Core policy")
	}
}
