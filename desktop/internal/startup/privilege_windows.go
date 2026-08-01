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
	launchWithExplorerParent        = launchStandardUIWithExplorerParent
	removeLegacyAutostartTask       = repairLegacyAutostartTask
)

// PrepareDesktopLaunch normalises an accidentally elevated UI before any
// WebView2 or application state is created. The elevated process is only a
// short-lived bootstrap; the replacement process is verified as non-elevated
// before it is resumed.
func PrepareDesktopLaunch(arguments []string) DesktopLaunchSecurity {
	currentToken := windows.GetCurrentProcessToken()
	currentElevated, currentElevationErr := tokenElevation(currentToken)
	if currentElevationErr == nil && !currentElevated {
		return DesktopLaunchSecurity{}
	}

	result := DesktopLaunchSecurity{Elevated: true}
	if currentElevationErr != nil {
		result.Detail = "could not verify the HypoMux token elevation state: " + currentElevationErr.Error()
	}
	if err := removeLegacyAutostartTask(); err != nil {
		result.LegacyTaskRepairNote = "legacy autostart cleanup failed: " + err.Error()
	}

	shellToken, sameUser, shellPID, shellErr := interactiveShellToken(currentToken)
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
	executable, err := os.Executable()
	if err == nil {
		executable, err = filepath.Abs(executable)
	}
	if err != nil {
		result.Detail = combineLaunchDetails("could not resolve the HypoMux executable: "+err.Error(), result.LegacyTaskRepairNote)
		return result
	}
	relaunchArguments := standardUIRelaunchArguments(arguments)
	launchDetails := make([]string, 0, 4)

	// Firefox-style first choice: an elevated UAC token normally has a linked
	// medium-integrity token for the same account. Normalize it as a primary
	// token before process creation instead of assuming its original token type.
	if linkedToken, linkedErr := currentToken.GetLinkedToken(); linkedErr == nil {
		defer linkedToken.Close()
		currentSID, currentSIDErr := tokenUserSID(currentToken)
		linkedSID, linkedSIDErr := tokenUserSID(linkedToken)
		sameLinkedUser := currentSIDErr == nil && linkedSIDErr == nil && strings.EqualFold(currentSID, linkedSID)
		linkedElevated, linkedElevationErr := tokenElevation(linkedToken)
		if linkedElevationErr == nil && !linkedElevated && sameLinkedUser {
			if launchErr := launchWithInteractiveShellToken(linkedToken, executable, relaunchArguments); launchErr == nil {
				result.Relaunched = true
				return result
			} else {
				launchDetails = append(launchDetails, "linked-token launch failed: "+launchErr.Error())
			}
		} else {
			launchDetails = append(launchDetails, fmt.Sprintf(
				"the linked UAC token was not a verified standard token for the same user: elevation_error=%v",
				linkedElevationErr,
			))
		}
	} else {
		launchDetails = append(launchDetails, "linked UAC token unavailable: "+linkedErr.Error())
	}

	if shellErr != nil {
		result.Detail = combineLaunchDetails(
			strings.Join(launchDetails, "; "),
			"could not obtain the interactive desktop token: "+shellErr.Error(),
			result.LegacyTaskRepairNote,
		)
		return result
	}
	shellElevated, shellElevationErr := tokenElevation(shellToken)
	if shellElevationErr != nil || shellElevated {
		result.Detail = combineLaunchDetails(
			strings.Join(launchDetails, "; "),
			fmt.Sprintf("the interactive Windows shell is not a verified standard token: elevated=%t error=%v", shellElevated, shellElevationErr),
			result.LegacyTaskRepairNote,
		)
		return result
	}
	if launchErr := launchWithInteractiveShellToken(shellToken, executable, relaunchArguments); launchErr == nil {
		result.Relaunched = true
		return result
	} else {
		launchDetails = append(launchDetails, "shell-token launch failed: "+launchErr.Error())
	}

	// PowerToys-style fallback: make Explorer the logical parent. Windows then
	// creates the replacement with Explorer's standard token and outside a
	// legacy Task Scheduler job, without requiring token-assignment privileges.
	if launchErr := launchWithExplorerParent(shellPID, shellToken, executable, relaunchArguments); launchErr == nil {
		result.Relaunched = true
		return result
	} else {
		launchDetails = append(launchDetails, "Explorer-parent launch failed: "+launchErr.Error())
	}
	result.Detail = combineLaunchDetails(
		"could not start the standard-permission UI: "+strings.Join(launchDetails, "; "),
		result.LegacyTaskRepairNote,
	)
	return result
}

func interactiveShellToken(currentToken windows.Token) (windows.Token, bool, uint32, error) {
	hwnd, _, callErr := getShellWindow.Call()
	if hwnd == 0 {
		if callErr != nil && !errors.Is(callErr, windows.ERROR_SUCCESS) {
			return 0, false, 0, fmt.Errorf("GetShellWindow: %w", callErr)
		}
		return 0, false, 0, errors.New("Windows shell window is unavailable")
	}
	var shellPID uint32
	_, _, callErr = getWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&shellPID)))
	if shellPID == 0 {
		if callErr != nil && !errors.Is(callErr, windows.ERROR_SUCCESS) {
			return 0, false, 0, fmt.Errorf("GetWindowThreadProcessId: %w", callErr)
		}
		return 0, false, 0, errors.New("Windows shell process identity is unavailable")
	}
	var currentSession uint32
	if err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &currentSession); err != nil {
		return 0, false, 0, fmt.Errorf("read HypoMux session: %w", err)
	}
	var shellSession uint32
	if err := windows.ProcessIdToSessionId(shellPID, &shellSession); err != nil {
		return 0, false, 0, fmt.Errorf("read Windows shell session: %w", err)
	}
	if currentSession != shellSession {
		return 0, false, 0, fmt.Errorf("Windows shell is in session %d, HypoMux is in session %d", shellSession, currentSession)
	}

	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, shellPID)
	if err != nil {
		return 0, false, 0, fmt.Errorf("open Windows shell process: %w", err)
	}
	defer windows.CloseHandle(process)
	var shellToken windows.Token
	access := uint32(windows.TOKEN_QUERY | windows.TOKEN_DUPLICATE | windows.TOKEN_ASSIGN_PRIMARY)
	if err := windows.OpenProcessToken(process, access, &shellToken); err != nil {
		return 0, false, 0, fmt.Errorf("open Windows shell token: %w", err)
	}
	currentSID, currentErr := tokenUserSID(currentToken)
	shellSID, shellErr := tokenUserSID(shellToken)
	if currentErr != nil || shellErr != nil {
		shellToken.Close()
		return 0, false, 0, fmt.Errorf("compare desktop user identity: current=%v shell=%v", currentErr, shellErr)
	}
	return shellToken, strings.EqualFold(currentSID, shellSID), shellPID, nil
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

func tokenElevation(token windows.Token) (bool, error) {
	var elevation uint32
	var returned uint32
	if err := windows.GetTokenInformation(
		token,
		windows.TokenElevation,
		(*byte)(unsafe.Pointer(&elevation)),
		uint32(unsafe.Sizeof(elevation)),
		&returned,
	); err != nil {
		return false, err
	}
	if returned != uint32(unsafe.Sizeof(elevation)) {
		return false, fmt.Errorf("unexpected TokenElevation size %d", returned)
	}
	return elevation != 0, nil
}

func duplicatePrimaryToken(token windows.Token) (windows.Token, error) {
	var primary windows.Token
	if err := windows.DuplicateTokenEx(
		token,
		windows.MAXIMUM_ALLOWED,
		nil,
		windows.SecurityImpersonation,
		windows.TokenPrimary,
		&primary,
	); err != nil {
		return 0, err
	}
	return primary, nil
}

func launchStandardUIWithInteractiveShellToken(token windows.Token, executable string, arguments []string) error {
	primaryToken, err := duplicatePrimaryToken(token)
	if err != nil {
		return fmt.Errorf("duplicate standard primary token: %w", err)
	}
	defer primaryToken.Close()
	token = primaryToken

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
	creationFlags := uint32(
		windows.CREATE_SUSPENDED |
			windows.CREATE_UNICODE_ENVIRONMENT,
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
		const logonWithoutProfile = 0
		success, _, callErr := createProcessWithTokenW.Call(
			uintptr(token), logonWithoutProfile,
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
	expectedSID, err := tokenUserSID(token)
	if err != nil {
		_ = windows.TerminateProcess(processInfo.Process, 1)
		return fmt.Errorf("read replacement UI identity: %w", err)
	}
	return verifyAndResumeReplacement(processInfo, expectedSID)
}

func launchStandardUIWithExplorerParent(
	shellPID uint32,
	shellToken windows.Token,
	executable string,
	arguments []string,
) error {
	shellProcess, err := windows.OpenProcess(
		windows.PROCESS_CREATE_PROCESS|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		shellPID,
	)
	if err != nil {
		return fmt.Errorf("open Explorer as replacement parent: %w", err)
	}
	defer windows.CloseHandle(shellProcess)

	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fmt.Errorf("create Explorer parent attribute list: %w", err)
	}
	defer attributes.Delete()
	if err := attributes.Update(
		windows.PROC_THREAD_ATTRIBUTE_PARENT_PROCESS,
		unsafe.Pointer(&shellProcess),
		unsafe.Sizeof(shellProcess),
	); err != nil {
		return fmt.Errorf("set Explorer as replacement parent: %w", err)
	}

	var environment *uint16
	if err := windows.CreateEnvironmentBlock(&environment, shellToken, false); err != nil {
		return fmt.Errorf("create Explorer user environment: %w", err)
	}
	defer windows.DestroyEnvironmentBlock(environment)
	applicationName, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return err
	}
	commandLine, err := windows.UTF16PtrFromString(
		windows.ComposeCommandLine(append([]string{executable}, arguments...)),
	)
	if err != nil {
		return err
	}
	workingDirectory, err := windows.UTF16PtrFromString(filepath.Dir(executable))
	if err != nil {
		return err
	}
	startupInfo := windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	startupInfo.StartupInfo.Cb = uint32(unsafe.Sizeof(startupInfo))
	var processInfo windows.ProcessInformation
	creationFlags := uint32(
		windows.CREATE_SUSPENDED |
			windows.CREATE_UNICODE_ENVIRONMENT |
			windows.EXTENDED_STARTUPINFO_PRESENT,
	)
	if err := windows.CreateProcess(
		applicationName,
		commandLine,
		nil,
		nil,
		false,
		creationFlags,
		environment,
		workingDirectory,
		&startupInfo.StartupInfo,
		&processInfo,
	); err != nil {
		return fmt.Errorf("CreateProcessW with Explorer parent: %w", err)
	}
	defer windows.CloseHandle(processInfo.Process)
	defer windows.CloseHandle(processInfo.Thread)
	expectedSID, err := tokenUserSID(shellToken)
	if err != nil {
		_ = windows.TerminateProcess(processInfo.Process, 1)
		return fmt.Errorf("read Explorer user identity: %w", err)
	}
	return verifyAndResumeReplacement(processInfo, expectedSID)
}

func verifyAndResumeReplacement(processInfo windows.ProcessInformation, expectedSID string) error {
	var childToken windows.Token
	if err := windows.OpenProcessToken(processInfo.Process, windows.TOKEN_QUERY, &childToken); err != nil {
		_ = windows.TerminateProcess(processInfo.Process, 1)
		return fmt.Errorf("verify replacement UI token: %w", err)
	}
	childElevated, childElevationErr := tokenElevation(childToken)
	childSID, childSIDErr := tokenUserSID(childToken)
	childToken.Close()
	if childElevationErr != nil {
		_ = windows.TerminateProcess(processInfo.Process, 1)
		return fmt.Errorf("verify replacement UI elevation: %w", childElevationErr)
	}
	if childElevated {
		_ = windows.TerminateProcess(processInfo.Process, 1)
		return errors.New("replacement UI unexpectedly received an elevated token")
	}
	if childSIDErr != nil || !strings.EqualFold(childSID, expectedSID) {
		_ = windows.TerminateProcess(processInfo.Process, 1)
		return fmt.Errorf("replacement UI user mismatch: expected=%s actual=%s error=%v", expectedSID, childSID, childSIDErr)
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
