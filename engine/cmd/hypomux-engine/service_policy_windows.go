//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/fileintegrity"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	coreServicePolicyRegistryPath = `SOFTWARE\HypoMux\CoreServicePolicy`
	coreServicePolicyVersion      = 1

	policyValueSchemaVersion = "SchemaVersion"
	policyValueDesktopPath   = "DesktopPath"
	policyValueDesktopSHA256 = "DesktopSHA256"
	policyValueTunPath       = "TunExecutablePath"
	policyValueTunSHA256     = "TunExecutableSHA256"
)

type coreServicePolicy struct {
	DesktopPath         string
	DesktopSHA256       string
	TunExecutable       string
	TunExecutableSHA256 string
}

func buildCoreServicePolicy(enginePath string, desktopPath string) (coreServicePolicy, error) {
	engine, err := canonicalRegularFile(enginePath, "Core Service executable")
	if err != nil {
		return coreServicePolicy{}, err
	}
	if !strings.EqualFold(filepath.Base(engine), "hypomux-engine.exe") ||
		!strings.EqualFold(filepath.Base(filepath.Dir(engine)), "bin") {
		return coreServicePolicy{}, errors.New(
			"Core Service executable must use the installed bin\\hypomux-engine.exe layout",
		)
	}

	desktop, err := canonicalRegularFile(desktopPath, "HypoMux desktop executable")
	if err != nil {
		return coreServicePolicy{}, err
	}
	if err := requireFixedLocalVolume(desktop, "HypoMux desktop executable"); err != nil {
		return coreServicePolicy{}, err
	}
	tunExecutable, err := canonicalRegularFile(
		filepath.Join(filepath.Dir(engine), "sing-box.exe"),
		"sing-box executable",
	)
	if err != nil {
		return coreServicePolicy{}, err
	}
	if !strings.EqualFold(filepath.Base(desktop), "hypomux.exe") ||
		!strings.EqualFold(filepath.Dir(tunExecutable), filepath.Dir(engine)) {
		return coreServicePolicy{}, errors.New("installed desktop or protected sing-box layout is invalid")
	}
	desktopDigest, err := fileintegrity.SHA256(desktop)
	if err != nil {
		return coreServicePolicy{}, fmt.Errorf("hash HypoMux desktop executable: %w", err)
	}
	tunDigest, err := fileintegrity.SHA256(tunExecutable)
	if err != nil {
		return coreServicePolicy{}, fmt.Errorf("hash sing-box executable: %w", err)
	}
	return coreServicePolicy{
		DesktopPath:         desktop,
		DesktopSHA256:       desktopDigest,
		TunExecutable:       tunExecutable,
		TunExecutableSHA256: tunDigest,
	}, nil
}

func writeCoreServicePolicy(policy coreServicePolicy) error {
	if err := validateCoreServicePolicy(policy); err != nil {
		return err
	}
	key, _, err := registry.CreateKey(
		registry.LOCAL_MACHINE,
		coreServicePolicyRegistryPath,
		registry.SET_VALUE|registry.QUERY_VALUE|registry.WOW64_64KEY,
	)
	if err != nil {
		return fmt.Errorf("create Core Service policy registry key: %w", err)
	}
	defer key.Close()

	// Mark the key invalid while individual values are replaced. The service
	// refuses a partial policy after an interrupted installation or upgrade.
	if err := key.SetDWordValue(policyValueSchemaVersion, 0); err != nil {
		return fmt.Errorf("invalidate Core Service policy: %w", err)
	}
	values := []struct {
		name  string
		value string
	}{
		{name: policyValueDesktopPath, value: policy.DesktopPath},
		{name: policyValueDesktopSHA256, value: policy.DesktopSHA256},
		{name: policyValueTunPath, value: policy.TunExecutable},
		{name: policyValueTunSHA256, value: policy.TunExecutableSHA256},
	}
	for _, value := range values {
		if err := key.SetStringValue(value.name, value.value); err != nil {
			return fmt.Errorf("write Core Service policy value %s: %w", value.name, err)
		}
	}
	if err := key.SetDWordValue(policyValueSchemaVersion, coreServicePolicyVersion); err != nil {
		return fmt.Errorf("commit Core Service policy: %w", err)
	}
	return nil
}

func loadCoreServicePolicy() (coreServicePolicy, error) {
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		coreServicePolicyRegistryPath,
		registry.QUERY_VALUE|registry.WOW64_64KEY,
	)
	if err != nil {
		return coreServicePolicy{}, fmt.Errorf("open Core Service policy registry key: %w", err)
	}
	defer key.Close()
	version, _, err := key.GetIntegerValue(policyValueSchemaVersion)
	if err != nil {
		return coreServicePolicy{}, fmt.Errorf("read Core Service policy version: %w", err)
	}
	if version != coreServicePolicyVersion {
		return coreServicePolicy{}, fmt.Errorf("unsupported Core Service policy version %d", version)
	}
	readString := func(name string) (string, error) {
		value, _, err := key.GetStringValue(name)
		if err != nil {
			return "", fmt.Errorf("read Core Service policy value %s: %w", name, err)
		}
		return value, nil
	}
	desktopPath, err := readString(policyValueDesktopPath)
	if err != nil {
		return coreServicePolicy{}, err
	}
	desktopDigest, err := readString(policyValueDesktopSHA256)
	if err != nil {
		return coreServicePolicy{}, err
	}
	tunPath, err := readString(policyValueTunPath)
	if err != nil {
		return coreServicePolicy{}, err
	}
	tunDigest, err := readString(policyValueTunSHA256)
	if err != nil {
		return coreServicePolicy{}, err
	}
	policy := coreServicePolicy{
		DesktopPath:         desktopPath,
		DesktopSHA256:       desktopDigest,
		TunExecutable:       tunPath,
		TunExecutableSHA256: tunDigest,
	}
	if err := validateCoreServicePolicy(policy); err != nil {
		return coreServicePolicy{}, err
	}
	return policy, nil
}

func deleteCoreServicePolicy() error {
	err := registry.DeleteKey(registry.LOCAL_MACHINE, coreServicePolicyRegistryPath)
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("delete Core Service policy registry key: %w", err)
	}
	return nil
}

func validateCoreServicePolicy(policy coreServicePolicy) error {
	if strings.TrimSpace(policy.DesktopPath) == "" || strings.TrimSpace(policy.TunExecutable) == "" {
		return errors.New("Core Service policy paths are required")
	}
	desktop := filepath.Clean(policy.DesktopPath)
	tunExecutable := filepath.Clean(policy.TunExecutable)
	if !filepath.IsAbs(desktop) || !filepath.IsAbs(tunExecutable) ||
		!strings.EqualFold(filepath.Base(desktop), "hypomux.exe") ||
		!strings.EqualFold(filepath.Base(tunExecutable), "sing-box.exe") ||
		!strings.EqualFold(filepath.Base(filepath.Dir(tunExecutable)), "bin") {
		return errors.New("Core Service policy does not match the installed HypoMux layout")
	}
	if err := validatePinnedSHA256(policy.DesktopSHA256); err != nil {
		return fmt.Errorf("invalid desktop executable policy: %w", err)
	}
	if err := validatePinnedSHA256(policy.TunExecutableSHA256); err != nil {
		return fmt.Errorf("invalid sing-box executable policy: %w", err)
	}
	return nil
}

func validatePinnedSHA256(value string) error {
	digest, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(digest) != sha256.Size {
		return errors.New("pinned SHA-256 digest must contain 64 hexadecimal characters")
	}
	return nil
}

func validateServiceClientExecutable(clientPID uint32, policy coreServicePolicy) error {
	clientPath, err := processExecutablePath(clientPID)
	if err != nil {
		return err
	}
	clientPath, err = canonicalRegularFile(clientPath, "Core Service client executable")
	if err != nil {
		return err
	}
	expectedPath, err := canonicalRegularFile(policy.DesktopPath, "trusted HypoMux desktop executable")
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(clientPath), filepath.Clean(expectedPath)) {
		return fmt.Errorf("reject untrusted Core Service client executable for PID %d", clientPID)
	}
	if err := fileintegrity.VerifySHA256(clientPath, policy.DesktopSHA256); err != nil {
		return fmt.Errorf("reject modified Core Service client executable for PID %d: %w", clientPID, err)
	}
	return nil
}

func processExecutablePath(processID uint32) (string, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return "", fmt.Errorf("open Core Service client process %d: %w", processID, err)
	}
	defer windows.CloseHandle(process)

	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return "", fmt.Errorf("read Core Service client executable for PID %d: %w", processID, err)
	}
	if size == 0 {
		return "", fmt.Errorf("Core Service client PID %d has no executable path", processID)
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func canonicalRegularFile(path string, label string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", label, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", label)
	}
	canonical, err := finalPathForHandle(windows.Handle(file.Fd()))
	if err != nil {
		return "", fmt.Errorf("resolve canonical %s: %w", label, err)
	}
	return filepath.Clean(canonical), nil
}

func finalPathForHandle(handle windows.Handle) (string, error) {
	size := uint32(512)
	for size <= 32768 {
		buffer := make([]uint16, size)
		length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], size, 0)
		if err != nil {
			return "", err
		}
		if length < size {
			path := windows.UTF16ToString(buffer[:length])
			switch {
			case strings.HasPrefix(path, `\\?\UNC\`):
				path = `\\` + strings.TrimPrefix(path, `\\?\UNC\`)
			case strings.HasPrefix(path, `\\?\`):
				path = strings.TrimPrefix(path, `\\?\`)
			}
			if strings.TrimSpace(path) == "" {
				return "", errors.New("canonical path is empty")
			}
			return path, nil
		}
		size = length + 1
	}
	return "", errors.New("canonical path exceeds the Windows path limit")
}

func requireMachineInstallLocation(enginePath string) error {
	// The service runs as LocalSystem. Keep its executable and sidecars in one
	// machine-owned ProgramData directory even when the desktop is installed on
	// another fixed drive.
	engine, err := canonicalRegularFile(enginePath, "Core Service executable")
	if err != nil {
		return err
	}
	programData, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return fmt.Errorf("resolve ProgramData: %w", err)
	}
	programData, err = filepath.Abs(programData)
	if err != nil {
		return fmt.Errorf("resolve absolute ProgramData path: %w", err)
	}
	expected := filepath.Join(programData, "HypoMux", "Core", "bin", "hypomux-engine.exe")
	if !strings.EqualFold(filepath.Clean(engine), filepath.Clean(expected)) {
		return fmt.Errorf("Core Service must run from the protected path %s", expected)
	}
	if err := requireFixedNTFSVolume(engine); err != nil {
		return err
	}
	paths := []string{
		filepath.Join(programData, "HypoMux"),
		filepath.Join(programData, "HypoMux", "Core"),
		filepath.Join(programData, "HypoMux", "Core", "bin"),
		engine,
		filepath.Join(filepath.Dir(engine), "sing-box.exe"),
		filepath.Join(filepath.Dir(engine), "wintun.dll"),
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(engine), "libcronet.dll")); statErr == nil {
		paths = append(paths, filepath.Join(filepath.Dir(engine), "libcronet.dll"))
	}
	for _, path := range paths {
		if err := requireProtectedCorePath(path); err != nil {
			return err
		}
	}
	return nil
}

func requireFixedNTFSVolume(path string) error {
	root, err := fixedLocalVolumeRoot(path, "Core Service")
	if err != nil {
		return err
	}
	rootPointer, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return err
	}
	fileSystem := make([]uint16, 32)
	var flags uint32
	if err := windows.GetVolumeInformation(
		rootPointer,
		nil,
		0,
		nil,
		nil,
		&flags,
		&fileSystem[0],
		uint32(len(fileSystem)),
	); err != nil {
		return fmt.Errorf("inspect Core Service volume: %w", err)
	}
	if !strings.EqualFold(windows.UTF16ToString(fileSystem), "NTFS") ||
		flags&windows.FILE_PERSISTENT_ACLS == 0 {
		return errors.New("Core Service requires NTFS with persistent ACL support")
	}
	return nil
}

func requireFixedLocalVolume(path string, label string) error {
	_, err := fixedLocalVolumeRoot(path, label)
	return err
}

func fixedLocalVolumeRoot(path string, label string) (string, error) {
	volume := filepath.VolumeName(filepath.Clean(path))
	if volume == "" || strings.HasPrefix(volume, `\\`) {
		return "", fmt.Errorf("%s requires a local fixed volume", label)
	}
	root := volume + `\`
	rootPointer, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "", err
	}
	if driveType := windows.GetDriveType(rootPointer); driveType != windows.DRIVE_FIXED {
		return "", fmt.Errorf("%s volume is not a fixed disk (type %d)", label, driveType)
	}
	return root, nil
}

func requireProtectedCorePath(path string) error {
	pointer, err := windows.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(pointer)
	if err != nil {
		return fmt.Errorf("inspect protected Core path %s: %w", path, err)
	}
	if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("protected Core path must not be a reparse point: %s", path)
	}
	return requireProtectedCoreACL(path)
}

func requireProtectedCoreACL(path string) error {
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("read protected Core ACL for %s: %w", path, err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return fmt.Errorf("read protected Core owner for %s: %w", path, err)
	}
	administrators, err := windows.StringToSid("S-1-5-32-544")
	if err != nil {
		return err
	}
	localSystem, err := windows.StringToSid("S-1-5-18")
	if err != nil {
		return err
	}
	if owner == nil || (!owner.Equals(administrators) && !owner.Equals(localSystem)) {
		return fmt.Errorf("protected Core path has an untrusted owner: %s", path)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return fmt.Errorf("protected Core path has no trusted DACL: %s", path)
	}
	const fileDeleteChild windows.ACCESS_MASK = 0x00000040
	writeMask := windows.ACCESS_MASK(
		windows.GENERIC_ALL | windows.GENERIC_WRITE |
			windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER |
			windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA |
			windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES |
			fileDeleteChild,
	)
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return fmt.Errorf("read protected Core ACL entry %d for %s: %w", index, path, err)
		}
		if ace == nil || ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("protected Core path has an unsupported allow ACL entry: %s", path)
		}
		if ace.Mask&writeMask == 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.Equals(administrators) && !sid.Equals(localSystem) {
			return fmt.Errorf("protected Core path grants write access to %s: %s", sid.String(), path)
		}
	}
	return nil
}

func pathWithin(root string, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}
