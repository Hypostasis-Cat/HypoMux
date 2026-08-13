//go:build windows

package tun

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/fileintegrity"
	"golang.org/x/sys/windows"
)

const trustedConfigDirectorySDDL = "O:BAD:P(A;OICI;GA;;;SY)(A;OICI;GA;;;BA)"

func stageTrustedConfig(config Config) (string, func(), error) {
	if strings.TrimSpace(config.ConfigSHA256) == "" {
		return config.ConfigPath, func() {}, nil
	}
	if !currentProcessElevated() {
		if config.RequireProtectedConfig {
			return "", nil, errors.New("protected TUN config staging requires an elevated Core")
		}
		// A non-elevated stdio Core cannot create ProgramData's protected
		// directory and does not cross a privilege boundary.
		return config.ConfigPath, func() {}, nil
	}
	directory, err := trustedConfigDirectory()
	if err != nil {
		return "", nil, err
	}
	if err := ensureTrustedConfigDirectory(directory); err != nil {
		return "", nil, err
	}
	return copyPinnedConfig(config.ConfigPath, directory, config.ConfigSHA256)
}

func copyPinnedConfig(sourcePath string, directory string, expectedDigest string) (string, func(), error) {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", nil, fmt.Errorf("read requested sing-box config: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "tun-config-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create protected sing-box config: %w", err)
	}
	path := temporary.Name()
	var removeOnce sync.Once
	remove := func() { removeOnce.Do(func() { _ = os.Remove(path) }) }
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		remove()
		return "", nil, fmt.Errorf("write protected sing-box config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		remove()
		return "", nil, fmt.Errorf("flush protected sing-box config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		remove()
		return "", nil, fmt.Errorf("close protected sing-box config: %w", err)
	}
	// The staged bytes, rather than a second open of the user-writable source,
	// are the security decision. This closes a source-file swap between ReadFile
	// and digest verification.
	if !strings.EqualFold(fileintegrity.SHA256Bytes(data), strings.TrimSpace(expectedDigest)) {
		remove()
		return "", nil, errors.New("requested sing-box config SHA-256 digest mismatch")
	}
	if err := fileintegrity.VerifySHA256(path, expectedDigest); err != nil {
		remove()
		return "", nil, fmt.Errorf("verify protected sing-box config: %w", err)
	}
	return path, remove, nil
}

// PrepareTrustedConfigStorage creates the machine-owned staging boundary used
// by elevated TUN activations. Service installation calls it before exposing
// the named pipe so a standard user cannot pre-position this path.
func PrepareTrustedConfigStorage() error {
	if !currentProcessElevated() {
		return errors.New("preparing trusted TUN config storage requires elevation")
	}
	directory, err := trustedConfigDirectory()
	if err != nil {
		return err
	}
	return ensureTrustedConfigDirectory(directory)
}

func trustedConfigDirectory() (string, error) {
	programData, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", fmt.Errorf("resolve ProgramData for trusted TUN config: %w", err)
	}
	return filepath.Join(programData, "HypoMuxCoreRuntime"), nil
}

func currentProcessElevated() bool {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}

func ensureTrustedConfigDirectory(path string) error {
	descriptor, err := windows.SecurityDescriptorFromString(trustedConfigDirectorySDDL)
	if err != nil {
		return fmt.Errorf("build protected TUN config ACL: %w", err)
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	if err := createProtectedDirectoryTree(path, &attributes); err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		name,
		windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return fmt.Errorf("open protected TUN config directory: %w", err)
	}
	defer windows.CloseHandle(handle)
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil {
		return fmt.Errorf("inspect protected TUN config directory: %w", err)
	}
	if information.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("protected TUN config directory must not be a reparse point")
	}

	currentDescriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("read protected TUN config owner: %w", err)
	}
	currentOwner, _, err := currentDescriptor.Owner()
	if err != nil {
		return fmt.Errorf("decode protected TUN config owner: %w", err)
	}
	administrators, err := windows.StringToSid("S-1-5-32-544")
	if err != nil {
		return fmt.Errorf("resolve Administrators SID: %w", err)
	}
	localSystem, err := windows.StringToSid("S-1-5-18")
	if err != nil {
		return fmt.Errorf("resolve LocalSystem SID: %w", err)
	}
	if currentOwner == nil || (!currentOwner.Equals(administrators) && !currentOwner.Equals(localSystem)) {
		return errors.New("protected TUN config directory has an untrusted owner")
	}

	owner, _, err := descriptor.Owner()
	if err != nil {
		return fmt.Errorf("read protected TUN config target owner: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read protected TUN config ACL: %w", err)
	}
	if err := windows.SetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|
			windows.PROTECTED_DACL_SECURITY_INFORMATION,
		owner,
		nil,
		dacl,
		nil,
	); err != nil {
		return fmt.Errorf("enforce protected TUN config ACL: %w", err)
	}
	return nil
}

func createProtectedDirectoryTree(path string, attributes *windows.SecurityAttributes) error {
	parent := filepath.Dir(path)
	if _, err := os.Stat(parent); errors.Is(err, os.ErrNotExist) {
		if err := createProtectedDirectoryTree(parent, attributes); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("inspect protected TUN config parent: %w", err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := windows.CreateDirectory(name, attributes); err != nil &&
		!errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return fmt.Errorf("create protected TUN config directory: %w", err)
	}
	return nil
}
