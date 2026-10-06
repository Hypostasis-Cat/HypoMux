//go:build windows

package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "HypoMuxCore"
const uninstallBase = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\`

var uninstallNames = []string{"HypoMux", "HypoMuxHypoMux", "{7637d353-b9c0-4145-bc81-7a474e534d07}_is1"}

type registryBackup struct {
	Key      string
	File     string
	Hash     string
	Exists   bool
	Security string
}
type systemState struct {
	Service     *mgr.Config
	Running     bool
	Recovery    []mgr.RecoveryAction
	ResetPeriod uint32
	Registry    []registryBackup
}
type windowsPlatform struct {
	machine bool
	coreBin string
}
type fileMetadata struct {
	Attributes uint32
	Security   string
}

func noLinks(path string) error {
	if !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) {
		return fmt.Errorf("local absolute path required: %s", path)
	}
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		wide, err := windows.UTF16PtrFromString(p)
		if err != nil {
			return err
		}
		a, err := windows.GetFileAttributes(wide)
		if err == nil && a&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("linked installation path is not supported: %s", p)
		}
		if err != nil && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return err
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
func applySecurity(path, sddl string) error {
	sd, e := windows.SecurityDescriptorFromString(sddl)
	if e != nil {
		return e
	}
	owner, _, e := sd.Owner()
	if e != nil {
		return e
	}
	dacl, _, e := sd.DACL()
	if e != nil {
		return e
	}
	control, _, e := sd.Control()
	if e != nil {
		return e
	}
	flags := windows.OWNER_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	if control&windows.SE_DACL_PROTECTED != 0 {
		flags = windows.OWNER_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.SECURITY_INFORMATION(flags), owner, nil, dacl, nil)
}
func secureDirectory(path string, machine, public bool) error {
	if e := noLinks(path); e != nil {
		return e
	}
	sid := "BA"
	allowed := map[string]bool{"S-1-5-18": true, "S-1-5-32-544": true}
	if !machine {
		u, e := windows.GetCurrentProcessToken().GetTokenUser()
		if e != nil {
			return e
		}
		sid = u.User.Sid.String()
		allowed[sid] = true
	}
	sddl := "O:" + sid + "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + sid + ")"
	if public {
		sddl += "(A;OICI;GRGX;;;BU)"
	}
	sd, e := windows.SecurityDescriptorFromString(sddl)
	if e != nil {
		return e
	}
	wide, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	if e = windows.CreateDirectory(wide, sa); e == nil {
		return nil
	} else if !errors.Is(e, windows.ERROR_ALREADY_EXISTS) {
		return e
	}
	// Existing recovery data must already be protected; tightening an attacker-
	// writable directory does not make its journal trustworthy retroactively.
	actual, e := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		return e
	}
	owner, _, e := actual.Owner()
	if e != nil || owner == nil || !allowed[owner.String()] {
		return fmt.Errorf("untrusted setup directory owner: %s", path)
	}
	acl, _, e := actual.DACL()
	if e != nil || acl == nil {
		return fmt.Errorf("untrusted setup directory ACL: %s", path)
	}
	const writeMask = windows.GENERIC_ALL | windows.GENERIC_WRITE | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES | 0x40
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if e = windows.GetAce(acl, i, &ace); e != nil {
			return e
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("unsupported setup ACL on %s", path)
		}
		who := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if uint32(ace.Mask)&writeMask != 0 && !allowed[who.String()] {
			return fmt.Errorf("setup directory is writable by %s: %s", who.String(), path)
		}
	}
	return nil
}
func (p windowsPlatform) ValidateTarget(path string) error {
	switch strings.ToLower(filepath.Base(path)) {
	case "hypomux.exe", "uninstall.exe", "hypomux-engine.exe", "sing-box.exe", "wintun.dll", "libcronet.dll", "hypomux.lnk":
	default:
		return fmt.Errorf("unowned payload name: %s", path)
	}
	if err := noLinks(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("target is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var details windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &details); err != nil {
		return err
	}
	if details.NumberOfLinks != 1 {
		return errors.New("hard-linked installation file is not supported")
	}
	return nil
}
func (p windowsPlatform) Metadata(path string) (json.RawMessage, error) {
	wide, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	a, e := windows.GetFileAttributes(wide)
	if e != nil {
		return nil, e
	}
	sd, e := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		return nil, e
	}
	return json.Marshal(fileMetadata{a, sd.String()})
}
func (p windowsPlatform) RestoreMetadata(path string, b json.RawMessage) error {
	var m fileMetadata
	if e := json.Unmarshal(b, &m); e != nil {
		return e
	}
	if e := applySecurity(path, m.Security); e != nil {
		return e
	}
	wide, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	return windows.SetFileAttributes(wide, m.Attributes)
}
func (p windowsPlatform) PrepareTarget(path string) error {
	if e := noLinks(path); e != nil {
		return e
	}
	if p.machine && strings.EqualFold(filepath.Dir(path), p.coreBin) {
		for _, d := range []string{filepath.Dir(filepath.Dir(p.coreBin)), filepath.Dir(p.coreBin), p.coreBin} {
			if e := secureDirectory(d, true, true); e != nil {
				return e
			}
		}
		if _, e := os.Stat(path); e == nil {
			if e = applySecurity(path, "O:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;GRGX;;;BU)"); e != nil {
				return e
			}
		}
	} else if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	wide, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	a, e := windows.GetFileAttributes(wide)
	if errors.Is(e, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if e != nil {
		return e
	}
	if a&windows.FILE_ATTRIBUTE_READONLY != 0 {
		return windows.SetFileAttributes(wide, a&^windows.FILE_ATTRIBUTE_READONLY)
	}
	return nil
}
func runProgram(path string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	hideCommand(cmd)
	b, e := cmd.CombinedOutput()
	if e != nil {
		return fmt.Errorf("%s: %w: %s", filepath.Base(path), e, strings.TrimSpace(string(b)))
	}
	return nil
}
func regTool() string { d, _ := windows.GetSystemDirectory(); return filepath.Join(d, "reg.exe") }
func (p windowsPlatform) Snapshot(root string) (json.RawMessage, error) {
	state := systemState{}
	if p.machine {
		m, e := mgr.Connect()
		if e != nil {
			return nil, e
		}
		defer m.Disconnect()
		s, e := m.OpenService(serviceName)
		if e == nil {
			defer s.Close()
			c, e := s.Config()
			if e != nil {
				return nil, e
			}
			if c.ServiceStartName != "" && !strings.EqualFold(c.ServiceStartName, "LocalSystem") {
				return nil, errors.New("Core service uses an unsupported account")
			}
			state.Service = &c
			st, e := s.Query()
			if e != nil {
				return nil, e
			}
			state.Running = st.State == svc.Running || st.State == svc.StartPending
			if state.Recovery, e = s.RecoveryActions(); e != nil {
				return nil, e
			}
			if state.ResetPeriod, e = s.ResetPeriod(); e != nil {
				return nil, e
			}
		} else if !errors.Is(e, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil, e
		}
	}
	hive := registry.CURRENT_USER
	prefix := "HKCU\\"
	if p.machine {
		hive = registry.LOCAL_MACHINE
		prefix = "HKLM\\"
	}
	keys := []string{}
	for _, name := range uninstallNames {
		keys = append(keys, uninstallBase+name)
	}
	if p.machine {
		keys = append(keys, `SOFTWARE\HypoMux\CoreServicePolicy`)
	}
	for i, path := range keys {
		b := registryBackup{Key: prefix + path, File: fmt.Sprintf("registry-%d.reg", i)}
		k, e := registry.OpenKey(hive, path, registry.READ|registry.WOW64_64KEY)
		if e == nil {
			sd, securityErr := windows.GetSecurityInfo(windows.Handle(k), windows.SE_REGISTRY_KEY, windows.DACL_SECURITY_INFORMATION)
			if securityErr != nil {
				k.Close()
				return nil, securityErr
			}
			b.Security = sd.String()
			k.Close()
			b.Exists = true
			if e = runProgram(regTool(), "export", b.Key, filepath.Join(root, b.File), "/y", "/reg:64"); e != nil {
				return nil, e
			}
			if b.Hash, e = hashFile(filepath.Join(root, b.File)); e != nil {
				return nil, e
			}
		} else if !errors.Is(e, windows.ERROR_FILE_NOT_FOUND) {
			return nil, e
		}
		state.Registry = append(state.Registry, b)
	}
	return json.Marshal(state)
}
func (p windowsPlatform) Quiesce() error {
	if !p.machine {
		return nil
	}
	m, e := mgr.Connect()
	if e != nil {
		return e
	}
	defer m.Disconnect()
	s, e := m.OpenService(serviceName)
	if errors.Is(e, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if e != nil {
		return e
	}
	defer s.Close()
	c, e := s.Config()
	if e != nil {
		return e
	}
	c.StartType = mgr.StartDisabled
	if e = s.UpdateConfig(c); e != nil {
		return e
	}
	st, e := s.Query()
	if e != nil {
		return e
	}
	if st.State == svc.Stopped {
		return nil
	}
	if _, e = s.Control(svc.Stop); e != nil && !errors.Is(e, windows.ERROR_SERVICE_NOT_ACTIVE) && !errors.Is(e, windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL) {
		return e
	}
	until := time.Now().Add(25 * time.Second)
	for time.Now().Before(until) {
		st, e = s.Query()
		if e != nil {
			return e
		}
		if st.State == svc.Stopped {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("Core did not stop; files and recovery backups retained")
}
func (p windowsPlatform) Restore(root string, b json.RawMessage) error {
	var state systemState
	if e := json.Unmarshal(b, &state); e != nil {
		return e
	}
	// Check export integrity before importing privileged registry data.
	for i, k := range state.Registry {
		if k.File != fmt.Sprintf("registry-%d.reg", i) {
			return errors.New("invalid registry backup")
		}
		if k.Exists {
			h, e := hashFile(filepath.Join(root, k.File))
			if e != nil || h != k.Hash {
				return errors.New("registry backup damaged")
			}
		}
	}
	for _, k := range state.Registry {
		// The journal lives in an administrator-only directory, but constrain the
		// key set as well so a malformed record cannot delete unrelated settings.
		valid := false
		prefixExpected := "HKCU\\"
		if p.machine {
			prefixExpected = "HKLM\\"
		}
		for _, n := range uninstallNames {
			if k.Key == prefixExpected+uninstallBase+n {
				valid = true
			}
		}
		if k.Key == `HKLM\SOFTWARE\HypoMux\CoreServicePolicy` && p.machine {
			valid = true
		}
		if !valid {
			return errors.New("unexpected recovery registry key")
		}
		prefix, path, ok := strings.Cut(k.Key, "\\")
		if !ok {
			return errors.New("invalid registry key")
		}
		hive := registry.CURRENT_USER
		if prefix == "HKLM" {
			hive = registry.LOCAL_MACHINE
		}
		existing, e := registry.OpenKey(hive, path, registry.READ|registry.WOW64_64KEY)
		if e == nil {
			existing.Close()
			if e = runProgram(regTool(), "delete", k.Key, "/f", "/reg:64"); e != nil {
				return e
			}
		} else if !errors.Is(e, windows.ERROR_FILE_NOT_FOUND) {
			return e
		}
		if k.Exists {
			if e = runProgram(regTool(), "import", filepath.Join(root, k.File), "/reg:64"); e != nil {
				return e
			}
			sd, err := windows.SecurityDescriptorFromString(k.Security)
			if err != nil {
				return err
			}
			dacl, _, err := sd.DACL()
			if err != nil {
				return err
			}
			control, _, err := sd.Control()
			if err != nil {
				return err
			}
			flags := windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION
			if control&windows.SE_DACL_PROTECTED != 0 {
				flags = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
			}
			key, err := registry.OpenKey(hive, path, windows.WRITE_DAC|registry.WOW64_64KEY)
			if err != nil {
				return err
			}
			err = windows.SetSecurityInfo(windows.Handle(key), windows.SE_REGISTRY_KEY, windows.SECURITY_INFORMATION(flags), nil, nil, dacl, nil)
			key.Close()
			if err != nil {
				return err
			}
		}
	}
	if !p.machine {
		return nil
	}
	m, e := mgr.Connect()
	if e != nil {
		return e
	}
	defer m.Disconnect()
	s, e := m.OpenService(serviceName)
	if state.Service == nil {
		if errors.Is(e, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		if e != nil {
			return e
		}
		defer s.Close()
		return s.Delete()
	}
	if e != nil {
		return fmt.Errorf("original service disappeared; recovery retained: %w", e)
	}
	defer s.Close()
	config := *state.Service
	if state.Running && config.StartType == mgr.StartDisabled {
		config.StartType = mgr.StartManual
	}
	if e = s.UpdateConfig(config); e != nil {
		return e
	}
	if e = s.SetRecoveryActions(state.Recovery, state.ResetPeriod); e != nil {
		return e
	}
	if state.Running {
		if e = s.Start(); e != nil && !errors.Is(e, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return e
		}
		until := time.Now().Add(20 * time.Second)
		for {
			status, err := s.Query()
			if err != nil {
				return err
			}
			if status.State == svc.Running {
				break
			}
			if status.State == svc.Stopped || time.Now().After(until) {
				return errors.New("original Core service did not become ready; recovery retained")
			}
			time.Sleep(200 * time.Millisecond)
		}
		if state.Service.StartType == mgr.StartDisabled {
			return s.UpdateConfig(*state.Service)
		}
	}
	return nil
}
