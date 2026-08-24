//go:build windows

package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const natFirewallRuleName = "HypoMux NAT Type Detection"

const natFirewallShellMaskNoCloseProcess = 0x00000040

type natFirewallShellExecuteInfo struct {
	Size        uint32
	Mask        uint32
	Window      windows.Handle
	Verb        *uint16
	File        *uint16
	Parameters  *uint16
	Directory   *uint16
	Show        int32
	Instance    windows.Handle
	IDList      uintptr
	Class       *uint16
	ClassKey    windows.Handle
	HotKey      uint32
	IconMonitor windows.Handle
	Process     windows.Handle
}

var natFirewallRegistryPaths = []string{
	`SYSTEM\CurrentControlSet\Services\SharedAccess\Parameters\FirewallPolicy\FirewallRules`,
}

func currentNATFirewallState() NATFirewallState {
	executable, err := os.Executable()
	if err != nil {
		return NATFirewallState{Supported: true, Enabled: true, Detail: err.Error()}
	}
	executable, _ = filepath.Abs(executable)
	state := NATFirewallState{Supported: true, Enabled: windowsFirewallEnabled()}
	if !state.Enabled {
		state.Allowed = true
		state.Detail = "Windows Firewall is disabled"
		return state
	}
	state.Allowed = hasInboundUDPFirewallRule(executable)
	if state.Allowed {
		state.Detail = "Inbound UDP is allowed for the current HypoMux executable"
	} else {
		state.Detail = "No inbound UDP allow rule exists for the current HypoMux executable"
	}
	return state
}

func windowsFirewallEnabled() bool {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\SharedAccess\Parameters\FirewallPolicy`, registry.READ)
	if err != nil {
		return true
	}
	defer key.Close()
	for _, profile := range []string{"DomainProfile", "PublicProfile", "StandardProfile"} {
		profileKey, openErr := registry.OpenKey(key, profile, registry.READ)
		if openErr != nil {
			continue
		}
		enabled, _, valueErr := profileKey.GetIntegerValue("EnableFirewall")
		profileKey.Close()
		if valueErr == nil && enabled != 0 {
			return true
		}
	}
	return false
}

func hasInboundUDPFirewallRule(executable string) bool {
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		for _, path := range natFirewallRegistryPaths {
			key, err := registry.OpenKey(root, path, registry.READ)
			if err != nil {
				continue
			}
			names, err := key.ReadValueNames(-1)
			if err == nil {
				for _, name := range names {
					value, _, valueErr := key.GetStringValue(name)
					if valueErr == nil && firewallRuleAllowsProgramUDP(value, executable) {
						key.Close()
						return true
					}
				}
			}
			key.Close()
		}
	}
	return false
}

func firewallRuleAllowsProgramUDP(rule string, executable string) bool {
	value := strings.ToLower(rule)
	program := strings.ToLower(filepath.Clean(executable))
	return strings.Contains(value, "active=true|") &&
		strings.Contains(value, "action=allow|") &&
		strings.Contains(value, "dir=in|") &&
		strings.Contains(value, "protocol=17|") &&
		strings.Contains(value, "app="+program+"|")
}

func allowNATFirewallTraffic() (NATFirewallState, error) {
	if state := currentNATFirewallState(); state.Allowed {
		return state, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return currentNATFirewallState(), err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return currentNATFirewallState(), err
	}
	parameters := `advfirewall firewall add rule name="` + natFirewallRuleName +
		`" dir=in action=allow enable=yes profile=any protocol=UDP program="` + executable + `"`
	if err := runElevatedFirewallCommand(parameters); err != nil {
		return currentNATFirewallState(), fmt.Errorf("allow HypoMux UDP replies: %w", err)
	}
	state := currentNATFirewallState()
	if !state.Allowed {
		return state, fmt.Errorf("Windows Firewall did not retain the HypoMux UDP allow rule")
	}
	return state, nil
}

func runElevatedFirewallCommand(parameters string) error {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString("netsh.exe")
	parameterPointer, err := windows.UTF16PtrFromString(parameters)
	if err != nil {
		return err
	}
	info := natFirewallShellExecuteInfo{
		Mask:       natFirewallShellMaskNoCloseProcess,
		Verb:       verb,
		File:       file,
		Parameters: parameterPointer,
		Show:       0,
	}
	info.Size = uint32(unsafe.Sizeof(info))
	procedure := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")
	result, _, callErr := procedure.Call(uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) || errors.Is(callErr, syscall.Errno(windows.ERROR_CANCELLED)) {
			return errors.New("user cancelled the administrator permission request")
		}
		return callErr
	}
	if info.Process == 0 || info.Process == windows.InvalidHandle {
		return errors.New("elevated firewall command did not return a process handle")
	}
	defer windows.CloseHandle(info.Process)
	waitResult, waitErr := windows.WaitForSingleObject(info.Process, windows.INFINITE)
	if waitErr != nil {
		return waitErr
	}
	if waitResult != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("unexpected elevated firewall wait status 0x%X", waitResult)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(info.Process, &exitCode); err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("elevated firewall command exited with code %d", exitCode)
	}
	return nil
}
