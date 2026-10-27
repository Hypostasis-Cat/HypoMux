//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/fileintegrity"
)

// verifyDesktopHostProcess pins the process serving the host pipe to the
// installed HypoMux desktop application. The serve-pipe arguments (--pipe
// included) are chosen by whoever launches the elevated engine, so a caller
// cannot be trusted to identify the host; the engine derives the expectation
// from sources it controls itself.
func verifyDesktopHostProcess(serverPID uint32) error {
	serverPath, err := processExecutablePath(serverPID)
	if err != nil {
		return err
	}
	serverCanonical, err := canonicalRegularFile(serverPath, "desktop host executable")
	if err != nil {
		return err
	}
	return verifyDesktopHostImage(serverCanonical, serverPID)
}

// verifyDesktopHostImage accepts a host executable that is either the
// SHA-256-pinned desktop executable from the HKLM Core Service policy or the
// desktop executable of the layout this engine itself belongs to. An
// attacker's pipe server matches neither.
func verifyDesktopHostImage(serverCanonical string, serverPID uint32) error {
	if policy, policyErr := loadCoreServicePolicy(); policyErr == nil {
		expected, err := canonicalRegularFile(policy.DesktopPath, "trusted HypoMux desktop executable")
		if err == nil && strings.EqualFold(filepath.Clean(serverCanonical), filepath.Clean(expected)) {
			if err := fileintegrity.VerifySHA256(expected, policy.DesktopSHA256); err != nil {
				return fmt.Errorf("reject modified desktop host executable for PID %d: %w", serverPID, err)
			}
			return nil
		}
	}
	for _, candidate := range siblingDesktopHostPaths() {
		expected, err := canonicalRegularFile(candidate, "HypoMux desktop executable")
		if err != nil {
			continue
		}
		if strings.EqualFold(filepath.Clean(serverCanonical), filepath.Clean(expected)) {
			return nil
		}
	}
	return fmt.Errorf("reject unexpected desktop host executable for PID %d", serverPID)
}

// siblingDesktopHostPaths derives where hypomux.exe must sit relative to this
// engine: one level up in the installed layout (<install>\bin\
// hypomux-engine.exe) or beside the engine in the development layout
// (desktop\bin).
func siblingDesktopHostPaths() []string {
	enginePath, err := os.Executable()
	if err != nil {
		return nil
	}
	engine, err := canonicalRegularFile(enginePath, "Core engine executable")
	if err != nil {
		return nil
	}
	bin := filepath.Dir(engine)
	return []string{
		filepath.Join(filepath.Dir(bin), "hypomux.exe"),
		filepath.Join(bin, "hypomux.exe"),
	}
}
