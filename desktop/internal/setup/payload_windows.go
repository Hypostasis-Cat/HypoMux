//go:build windows

package setup

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// payloadDigestNames lists the Core binaries the installer stages and the
// setup helper copies into place. The helper process itself (hypomux.exe)
// cannot appear here — it is the party doing the verification; its integrity
// rests on the hardened staging directory instead (see the
// HypoMuxHardenStagingDirectory macro in project.nsi).
var payloadDigestNames = []string{"hypomux-engine.exe", "sing-box.exe", "wintun.dll"}

// verifyPayloadIntegrity proves the staged payload still contains exactly the
// bytes the installer embedded at compile time. The digests travel on the
// helper's command line, which an unprivileged caller can read but never
// rewrite on an elevated process, so they are an independent reference even
// though the staging directory is user-writable before hardening.
func verifyPayloadIntegrity(payloadDir, expected string, machine bool) error {
	if machine {
		// The installer hardens the staging directory before extracting; if
		// that protection is absent the digests below would describe bytes an
		// attacker could still replace between this check and the copy.
		if e := secureDirectory(payloadDir, true, false); e != nil {
			return fmt.Errorf("untrusted payload staging: %w", e)
		}
	}
	if expected == "" {
		return errors.New("payload digest list is required")
	}
	seen := make(map[string]bool, len(payloadDigestNames))
	for _, entry := range strings.Fields(expected) {
		name, digest, ok := strings.Cut(entry, "=")
		if !ok {
			return fmt.Errorf("malformed payload digest entry %q", entry)
		}
		if !isPayloadDigestName(name) {
			return fmt.Errorf("unexpected payload name %q", name)
		}
		if seen[name] {
			return fmt.Errorf("duplicate payload digest for %q", name)
		}
		seen[name] = true
		if err := checkDigestHex(digest); err != nil {
			return fmt.Errorf("payload digest for %q: %w", name, err)
		}
		source := filepath.Join(payloadDir, name)
		if err := checkPlainPayloadFile(source); err != nil {
			return fmt.Errorf("payload %q: %w", name, err)
		}
		actual, err := hashFile(source)
		if err != nil {
			return fmt.Errorf("payload %q: %w", name, err)
		}
		if subtle.ConstantTimeCompare([]byte(strings.ToLower(actual)), []byte(digest)) != 1 {
			return fmt.Errorf("payload %q does not match the digests embedded in the installer", name)
		}
	}
	for _, name := range payloadDigestNames {
		if !seen[name] {
			return fmt.Errorf("payload digest missing for %q", name)
		}
	}
	return nil
}

func isPayloadDigestName(name string) bool {
	for _, allowed := range payloadDigestNames {
		if name == allowed {
			return true
		}
	}
	return false
}

func checkDigestHex(digest string) error {
	if len(digest) != 64 {
		return errors.New("digest must be 64 hex characters")
	}
	for _, r := range digest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return errors.New("digest must be lowercase hex")
		}
	}
	return nil
}

func checkPlainPayloadFile(path string) error {
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := windows.GetFileAttributes(wide)
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("linked payload is not supported")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("payload is not a regular file")
	}
	return nil
}
