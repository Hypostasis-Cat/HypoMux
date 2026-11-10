//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/fileintegrity"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/server"
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

	policy, err := buildCoreServicePolicy(enginePath, desktopPath, "")
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
	if policy.CoreCliPath != "" || policy.CoreCliSHA256 != "" {
		t.Fatalf("empty --cli must not pin a CLI executable: %#v", policy)
	}
	if err := validateCoreServicePolicy(policy); err != nil {
		t.Fatalf("policy without a CLI pin must validate: %v", err)
	}
}

func TestBuildCoreServicePolicyPinsOptionalCLIExecutable(t *testing.T) {
	temporaryRoot := t.TempDir()
	installRoot := filepath.Join(temporaryRoot, "D-Apps", "HypoMux")
	binDirectory := filepath.Join(temporaryRoot, "ProgramData", "HypoMux", "Core", "bin")
	if err := os.MkdirAll(binDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(installRoot, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	enginePath := filepath.Join(binDirectory, "hypomux-engine.exe")
	desktopPath := filepath.Join(installRoot, "hypomux.exe")
	tunPath := filepath.Join(binDirectory, "sing-box.exe")
	cliPath := filepath.Join(installRoot, "bin", "hypomux-cli.exe")
	for path, contents := range map[string]string{
		enginePath:  "engine",
		desktopPath: "desktop",
		tunPath:     "sidecar",
		cliPath:     "cli",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	policy, err := buildCoreServicePolicy(enginePath, desktopPath, cliPath)
	if err != nil {
		t.Fatal(err)
	}
	expectedCLI, err := canonicalRegularFile(cliPath, "expected CLI executable")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(policy.CoreCliPath, expectedCLI) {
		t.Fatalf("CLI pin path = %q, want %q", policy.CoreCliPath, expectedCLI)
	}
	if err := fileintegrity.VerifySHA256(policy.CoreCliPath, policy.CoreCliSHA256); err != nil {
		t.Fatalf("CLI pin: %v", err)
	}
	if err := validateCoreServicePolicy(policy); err != nil {
		t.Fatalf("policy with a CLI pin must validate: %v", err)
	}
	if len(policy.serviceClientPins()) != 2 {
		t.Fatalf("policy pins = %#v, want desktop and CLI entries", policy.serviceClientPins())
	}

	if _, err := buildCoreServicePolicy(enginePath, desktopPath, filepath.Join(temporaryRoot, "missing-cli.exe")); err == nil {
		t.Fatal("a missing CLI executable was accepted")
	}
	// A half-written pin must fail validation so an interrupted install can
	// never leave a path-only or digest-only grant behind. Digest *mismatch*
	// is format-valid here and is enforced later by fileintegrity at pipe
	// connect time, matching the desktop pin behavior.
	broken := policy
	broken.CoreCliSHA256 = ""
	if err := validateCoreServicePolicy(broken); err == nil {
		t.Fatal("path-only CLI pin was accepted")
	}
	broken = policy
	broken.CoreCliPath = ""
	if err := validateCoreServicePolicy(broken); err == nil {
		t.Fatal("digest-only CLI pin was accepted")
	}
	broken = policy
	broken.CoreCliPath = "relative\\hypomux-cli.exe"
	if err := validateCoreServicePolicy(broken); err == nil {
		t.Fatal("relative CLI pin path was accepted")
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
	pin := clientExecutablePin{Path: executable, SHA256: digest}
	if err := validateServiceClientExecutable(uint32(os.Getpid()), pin); err != nil {
		t.Fatalf("current trusted process was rejected: %v", err)
	}
	pin.SHA256 = strings.Repeat("0", 64)
	if err := validateServiceClientExecutable(uint32(os.Getpid()), pin); err == nil {
		t.Fatal("client with a mismatched pinned digest was accepted")
	}
	pin.Path = filepath.Join(t.TempDir(), "different.exe")
	if err := os.WriteFile(pin.Path, []byte("different executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateServiceClientExecutable(uint32(os.Getpid()), pin); err == nil {
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

// The matched pin determines the session role, so each pin must carry the role
// the engine will enforce. The desktop pin grants the full surface; the optional
// CLI pin grants the restricted surface.
func TestServiceClientPinsCarryRoles(t *testing.T) {
	policy := coreServicePolicy{
		DesktopPath:   `C:\HypoMux\hypomux.exe`,
		DesktopSHA256: strings.Repeat("a", 64),
		CoreCliPath:   `C:\HypoMux\bin\hypomux-cli.exe`,
		CoreCliSHA256: strings.Repeat("b", 64),
	}
	pins := policy.serviceClientPins()
	if len(pins) != 2 || pins[0].Role != server.RoleDesktop || pins[1].Role != server.RoleCLI {
		t.Fatalf("pins with CLI = %#v, want desktop then cli roles", pins)
	}

	policy.CoreCliPath = ""
	policy.CoreCliSHA256 = ""
	pins = policy.serviceClientPins()
	if len(pins) != 1 || pins[0].Role != server.RoleDesktop {
		t.Fatalf("pins without CLI = %#v, want desktop only", pins)
	}
}
