//go:build windows

package startup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const legacyAutostartTaskName = `\HypoMuxAutoStart`

var (
	user32                          = windows.NewLazySystemDLL("user32.dll")
	getShellWindow                  = user32.NewProc("GetShellWindow")
	getWindowThreadProcessID        = user32.NewProc("GetWindowThreadProcessId")
	advapi32                        = windows.NewLazySystemDLL("advapi32.dll")
	createProcessWithTokenW         = advapi32.NewProc("CreateProcessWithTokenW")
	launchWithInteractiveShellToken = launchStandardUIWithInteractiveShellToken
	removeLegacyAutostartTask       = repairLegacyAutostartTask
)

// PrepareDesktopLaunch normalises an accidentally elevated UI before any
// WebView2 or application state is created. The elevated process is only a
// short-lived bootstrap; the replacement process is verified as non-elevated
// before it is resumed.
func PrepareDesktopLaunch(arguments []string) DesktopLaunchSecurity {
	currentToken := windows.GetCurrentProcessToken()
	if !currentToken.IsElevated() {
		return DesktopLaunchSecurity{}
	}

	result := DesktopLaunchSecurity{Elevated: true}
	if err := removeLegacyAutostartTask(); err != nil {
		result.LegacyTaskRepairNote = "legacy autostart cleanup failed: " + err.Error()
	}

	shellToken, sameUser, shellErr := interactiveShellToken(currentToken)
	if shellToken != 0 {
		defer shellToken.Close()
	}
	result.ProxyCompatible = shellErr == nil && sameUser

	if hasStandardUIRelaunchArgument(arguments) {
		result.Detail = combineLaunchDetails(
			"the standard-permission relaunch remained elevated; automatic relaunch was stopped to avoid a loop",
			result.LegacyTaskRepairNote,
		)
		return result
	}
	if shellErr != nil {
		result.Detail = combineLaunchDetails(
			"could not obtain the interactive desktop token: "+shellErr.Error(),
			result.LegacyTaskRepairNote,
		)
		return result
	}
	if shellToken.IsElevated() {
		result.Detail = combineLaunchDetails(
			"the interactive Windows shell is also elevated, so no standard desktop token is available",
			result.LegacyTaskRepairNote,
		)
		return result
	}

	executable, err := os.Executable()
	if err == nil {
		executable, err = filepath.Abs(executable)
	}
	if err != nil {
		result.Detail = combineLaunchDetails("could not resolve the HypoMux executable: "+err.Error(), result.LegacyTaskRepairNote)
		return result
	}
	if err := launchWithInteractiveShellToken(shellToken, executable, standardUIRelaunchArguments(arguments)); err != nil {
		result.Detail = combineLaunchDetails("could not start the standard-permission UI: "+err.Error(), result.LegacyTaskRepairNote)
		return result
	}
	result.Relaunched = true
	return result
}

func interactiveShellToken(currentToken windows.Token) (windows.Token, bool, error) {
	hwnd, _, callErr := getShellWindow.Call()
	if hwnd == 0 {
		if callErr != nil && !errors.Is(callErr, windows.ERROR_SUCCESS) {
			return 0, false, fmt.Errorf("GetShellWindow: %w", callErr)
		}
		return 0, false, errors.New("Windows shell window is unavailable")
	}
	var shellPID uint32
	_, _, callErr = getWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&shellPID)))
	if shellPID == 0 {
		if callErr != nil && !errors.Is(callErr, windows.ERROR_SUCCESS) {
			return 0, false, fmt.Errorf("GetWindowThreadProcessId: %w", callErr)
		}
		return 0, false, errors.New("Windows shell process identity is unavailable")
	}
	var currentSession uint32
	if err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &currentSession); err != nil {
		return 0, false, fmt.Errorf("read HypoMux session: %w", err)
	}
	var shellSession uint32
	if err := windows.ProcessIdToSessionId(shellPID, &shellSession); err != nil {
		return 0, false, fmt.Errorf("read Windows shell session: %w", err)
	}
	if currentSession != shellSession {
		return 0, false, fmt.Errorf("Windows shell is in session %d, HypoMux is in session %d", shellSession, currentSession)
	}

	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, shellPID)
	if err != nil {
		return 0, false, fmt.Errorf("open Windows shell process: %w", err)
	}
	defer windows.CloseHandle(process)
	var shellToken windows.Token
	access := uint32(windows.TOKEN_QUERY | windows.TOKEN_DUPLICATE | windows.TOKEN_ASSIGN_PRIMARY)
	if err := windows.OpenProcessToken(process, access, &shellToken); err != nil {
		return 0, false, fmt.Errorf("open Windows shell token: %w", err)
	}
	currentSID, currentErr := tokenUserSID(currentToken)
	shellSID, shellErr := tokenUserSID(shellToken)
	if currentErr != nil || shellErr != nil {
		shellToken.Close()
		return 0, false, fmt.Errorf("compare desktop user identity: current=%v shell=%v", currentErr, shellErr)
	}
	return shellToken, strings.EqualFold(currentSID, shellSID), nil
}

func tokenUserSID(token windows.Token) (string, error) {
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	if user == nil || user.User.Sid == nil {
		return "", errors.New("token has no user SID")
	}
	return user.User.Sid.String(), nil
}

func launchStandardUIWithInteractiveShellToken(token windows.Token, executable string, arguments []string) error {
	var environment *uint16
	if err := windows.CreateEnvironmentBlock(&environment, token, false); err != nil {
		return fmt.Errorf("create desktop user environment: %w", err)
	}
	defer windows.DestroyEnvironmentBlock(environment)

	applicationName, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return err
	}
	workingDirectory, err := windows.UTF16PtrFromString(filepath.Dir(executable))
	if err != nil {
		return err
	}
	commandLineText := windows.ComposeCommandLine(append([]string{executable}, arguments...))
	// Break away from a legacy Task Scheduler job so the replacement UI is not
	// terminated when the short-lived elevated task process exits.
	creationFlags := uint32(
		windows.CREATE_SUSPENDED |
			windows.CREATE_UNICODE_ENVIRONMENT |
			windows.CREATE_BREAKAWAY_FROM_JOB,
	)

	create := func(useCreateProcessAsUser bool) (windows.ProcessInformation, error) {
		commandLine, convertErr := windows.UTF16PtrFromString(commandLineText)
		if convertErr != nil {
			return windows.ProcessInformation{}, convertErr
		}
		startupInfo := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
		var processInfo windows.ProcessInformation
		if useCreateProcessAsUser {
			createErr := windows.CreateProcessAsUser(
				token, applicationName, commandLine, nil, nil, false, creationFlags,
				environment, workingDirectory, &startupInfo, &processInfo,
			)
			return processInfo, createErr
		}
		const logonWithProfile = 0x00000001
		success, _, callErr := createProcessWithTokenW.Call(
			uintptr(token), logonWithProfile,
			uintptr(unsafe.Pointer(applicationName)), uintptr(unsafe.Pointer(commandLine)),
			uintptr(creationFlags), uintptr(unsafe.Pointer(environment)),
			uintptr(unsafe.Pointer(workingDirectory)), uintptr(unsafe.Pointer(&startupInfo)),
			uintptr(unsafe.Pointer(&processInfo)),
		)
		if success == 0 {
			if callErr == nil || errors.Is(callErr, windows.ERROR_SUCCESS) {
				callErr = windows.GetLastError()
			}
			return windows.ProcessInformation{}, callErr
		}
		return processInfo, nil
	}

	processInfo, tokenErr := create(false)
	if tokenErr != nil {
		var asUserErr error
		processInfo, asUserErr = create(true)
		if asUserErr != nil {
			return fmt.Errorf("CreateProcessWithTokenW: %v; CreateProcessAsUserW: %w", tokenErr, asUserErr)
		}
	}
	defer windows.CloseHandle(processInfo.Process)
	defer windows.CloseHandle(processInfo.Thread)

	var childToken windows.Token
	if err := windows.OpenProcessToken(processInfo.Process, windows.TOKEN_QUERY, &childToken); err != nil {
		_ = windows.TerminateProcess(processInfo.Process, 1)
		return fmt.Errorf("verify replacement UI token: %w", err)
	}
	childElevated := childToken.IsElevated()
	childToken.Close()
	if childElevated {
		_ = windows.TerminateProcess(processInfo.Process, 1)
		return errors.New("replacement UI unexpectedly received an elevated token")
	}
	if _, err := windows.ResumeThread(processInfo.Thread); err != nil {
		_ = windows.TerminateProcess(processInfo.Process, 1)
		return fmt.Errorf("resume replacement UI: %w", err)
	}
	return nil
}

func repairLegacyAutostartTask() error {
	query := exec.Command("schtasks.exe", "/Query", "/TN", legacyAutostartTaskName)
	hideWindow(query)
	if err := query.Run(); err != nil {
		return nil
	}
	remove := exec.Command("schtasks.exe", "/Delete", "/TN", legacyAutostartTaskName, "/F")
	hideWindow(remove)
	output, err := remove.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return errors.New(detail)
	}
	return nil
}
