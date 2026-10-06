//go:build windows

package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
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

func TestVerifyDownloadedInstallerAuthenticityRejectsUnsignedFile(t *testing.T) {
	installer := filepath.Join(t.TempDir(), "HypoMux_Setup_2.5.8.exe")
	if err := os.WriteFile(installer, []byte("not a signed executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyDownloadedInstallerAuthenticity(installer); err == nil {
		t.Fatal("unsigned installer was accepted")
	}
}

func TestVerifyDownloadedInstallerAuthenticityAcceptsOfficialSignedInstaller(t *testing.T) {
	installer := os.Getenv("HYPOMUX_SIGNED_INSTALLER_TEST")
	if installer == "" {
		t.Skip("set HYPOMUX_SIGNED_INSTALLER_TEST to an official signed installer")
	}
	if err := verifyDownloadedInstallerAuthenticity(installer); err != nil {
		t.Fatalf("official signed installer rejected: %v", err)
	}
}

func TestIsRevokedCertificateErrorOnlyAcceptsDefinitiveRevocation(t *testing.T) {
	revoked := []error{
		syscall.Errno(trustERevoked),
		syscall.Errno(windows.CRYPT_E_REVOKED),
		fmt.Errorf("wrapped: %w", syscall.Errno(trustERevoked)),
	}
	for _, err := range revoked {
		if !isRevokedCertificateError(err) {
			t.Errorf("revocation result %v was not recognised", err)
		}
	}
	// Inconclusive results must not count: treating an unreachable CRL server as
	// revocation would block updates for every offline or firewalled machine.
	inconclusive := []error{
		syscall.Errno(windows.CRYPT_E_REVOCATION_OFFLINE),
		syscall.Errno(windows.CRYPT_E_NO_REVOCATION_CHECK),
		syscall.Errno(windows.CRYPT_E_NO_REVOCATION_DLL),
		syscall.Errno(windows.TRUST_E_NOSIGNATURE),
		errors.New("plain failure"),
	}
	for _, err := range inconclusive {
		if isRevokedCertificateError(err) {
			t.Errorf("inconclusive result %v was treated as revocation", err)
		}
	}
}

func TestCheckInstallerRevocationFailsOpenWithoutRevocationEvidence(t *testing.T) {
	installer := filepath.Join(t.TempDir(), "HypoMux_Setup_2.5.8.exe")
	if err := os.WriteFile(installer, []byte("not a signed executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkInstallerRevocation(installer); err != nil {
		t.Fatalf("revocation pass blocked an installer without revocation evidence: %v", err)
	}
}

func TestLaunchInstallerRejectsUnsignedFileBeforeCreatingHelper(t *testing.T) {
	directory, err := os.MkdirTemp("", "HypoMuxUpdate-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	installer := filepath.Join(directory, "HypoMux_Setup_2.5.8.exe")
	if err := os.WriteFile(installer, []byte("not a signed executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := launchInstallerAfterExit(installer, os.Getpid()); err == nil {
		t.Fatal("unsigned installer reached the launcher")
	}
	if helpers, err := filepath.Glob(filepath.Join(directory, "hypomux-update-helper-*.exe")); err != nil || len(helpers) != 0 {
		t.Fatal("launcher helper was created before Authenticode validation")
	}
}

func TestUpdaterNativeResultRetainsFailedInstaller(t *testing.T) {
	for _, name := range []string{"success", "cancel", "exit67", "timeout"} {
		t.Run(name, func(t *testing.T) {
			installer := filepath.Join(t.TempDir(), "安装 % &.exe")
			if e := os.WriteFile(installer, []byte("fixture"), 0600); e != nil {
				t.Fatal(e)
			}
			launched := false
			err := finishUpdate(func() error {
				if name == "timeout" {
					return errors.New("timeout")
				}
				return nil
			}, func() (uint32, error) {
				launched = true
				if name == "cancel" {
					return 0, windows.ERROR_CANCELLED
				}
				if name == "exit67" {
					return 67, nil
				}
				return 0, nil
			}, func() error { return os.Remove(installer) }, func(string) {})
			if name == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if _, e := os.Stat(installer); !os.IsNotExist(e) {
					t.Fatal("successful installer not cleaned")
				}
			} else {
				if err == nil {
					t.Fatal("failure ignored")
				}
				if _, e := os.Stat(installer); e != nil {
					t.Fatal("failed installer lost", e)
				}
			}
			if name == "timeout" && launched {
				t.Fatal("launched before shutdown")
			}
		})
	}
}
func TestUpdaterNativeUnicodeAndLiteralShellCharacters(t *testing.T) {
	dir, e := os.MkdirTemp("", "HypoMuxUpdate-中文 空格 % & ! ")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	installer := filepath.Join(dir, "HypoMux_Setup_2.7.1.exe")
	if e = os.WriteFile(installer, []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = validateDownloadedInstallerPath(installer); e != nil {
		t.Fatal(e)
	}
	ready := filepath.Join(dir, "ready")
	if e = writeUpdateReady(ready, "ready"); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(ready)
	if e != nil || string(b) != "ready" {
		t.Fatal("bad handshake", e, string(b))
	}
}
func TestUpdaterSerializesInstallAndDownload(t *testing.T) {
	s := NewUpdaterService(func() {})
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	s.launchInstaller = func(string, int) error { close(entered); <-release; return nil }
	go func() { done <- s.InstallAndQuit("fixture") }()
	<-entered
	if e := s.InstallAndQuit("fixture"); e == nil {
		t.Error("duplicate installer accepted")
	}
	if _, e := s.Download(ReleaseInfo{}); e == nil {
		t.Error("download during install accepted")
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if e := s.InstallAndQuit("fixture"); e == nil {
		t.Fatal("repeat accepted after exit was scheduled")
	}
}
