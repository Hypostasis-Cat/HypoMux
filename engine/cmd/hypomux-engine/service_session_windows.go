//go:build windows

package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type serviceSessionState uint32

const (
	serviceSessionActive serviceSessionState = iota
	serviceSessionConnected
	serviceSessionConnectQuery
	serviceSessionShadow
	serviceSessionDisconnected
	serviceSessionIdle
	serviceSessionListen
	serviceSessionReset
	serviceSessionDown
	serviceSessionInit
)

const wtsConnectStateInfoClass = 8

var (
	wtsapi32                        = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQuerySessionInformationW = wtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory               = wtsapi32.NewProc("WTSFreeMemory")
)

func queryServiceSessionState(sessionID uint32) (serviceSessionState, error) {
	var buffer *uint16
	var bytesReturned uint32
	result, _, callErr := procWTSQuerySessionInformationW.Call(
		0,
		uintptr(sessionID),
		wtsConnectStateInfoClass,
		uintptr(unsafe.Pointer(&buffer)),
		uintptr(unsafe.Pointer(&bytesReturned)),
	)
	if result == 0 {
		return 0, fmt.Errorf("query WTS session %d connection state: %w", sessionID, callErr)
	}
	if buffer == nil {
		return 0, fmt.Errorf("query WTS session %d returned an empty state", sessionID)
	}
	defer procWTSFreeMemory.Call(uintptr(unsafe.Pointer(buffer)))
	if bytesReturned < uint32(unsafe.Sizeof(uint32(0))) {
		return 0, fmt.Errorf("query WTS session %d returned %d state bytes", sessionID, bytesReturned)
	}
	return serviceSessionState(*(*uint32)(unsafe.Pointer(buffer))), nil
}

func serviceSessionIsActive(state serviceSessionState) bool {
	return state == serviceSessionActive
}

func (state serviceSessionState) String() string {
	names := [...]string{
		"active",
		"connected",
		"connect-query",
		"shadow",
		"disconnected",
		"idle",
		"listen",
		"reset",
		"down",
		"init",
	}
	if int(state) >= 0 && int(state) < len(names) {
		return names[state]
	}
	return fmt.Sprintf("unknown-%d", uint32(state))
}
