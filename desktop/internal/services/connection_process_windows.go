//go:build windows

package services

import (
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

const tcpTableOwnerPIDAll = 5

var getExtendedTCPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

type tcpRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPID  uint32
}

type tcpEndpointPair struct {
	local  uint16
	remote uint16
}

func resolveConnectionProcesses(connections []connectionTelemetry) map[uint64]string {
	wanted := make(map[tcpEndpointPair][]uint64, len(connections))
	for _, item := range connections {
		_, clientPortValue, clientErr := net.SplitHostPort(item.Client)
		_, listenerPortValue, listenerErr := net.SplitHostPort(item.Listener)
		if clientErr != nil || listenerErr != nil {
			continue
		}
		var clientPort, listenerPort uint16
		if _, err := fmt.Sscan(clientPortValue, &clientPort); err != nil {
			continue
		}
		if _, err := fmt.Sscan(listenerPortValue, &listenerPort); err != nil {
			continue
		}
		key := tcpEndpointPair{local: clientPort, remote: listenerPort}
		wanted[key] = append(wanted[key], item.ID)
	}
	if len(wanted) == 0 {
		return map[uint64]string{}
	}

	result := make(map[uint64]string)
	processCache := make(map[uint32]string)
	for _, row := range tcpOwnerRows() {
		key := tcpEndpointPair{
			local:  networkPort(row.LocalPort),
			remote: networkPort(row.RemotePort),
		}
		ids := wanted[key]
		if len(ids) == 0 {
			continue
		}
		name, exists := processCache[row.OwningPID]
		if !exists {
			name = processImageName(row.OwningPID)
			processCache[row.OwningPID] = name
		}
		for _, id := range ids {
			result[id] = name
		}
	}
	return result
}

func tcpOwnerRows() []tcpRowOwnerPID {
	size := uint32(0)
	status, _, _ := getExtendedTCPTable.Call(
		0, uintptr(unsafe.Pointer(&size)), 0, uintptr(windows.AF_INET), uintptr(tcpTableOwnerPIDAll), 0,
	)
	if status != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || size < 4 {
		return nil
	}
	buffer := make([]byte, size)
	status, _, _ = getExtendedTCPTable.Call(
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)), 0,
		uintptr(windows.AF_INET), uintptr(tcpTableOwnerPIDAll), 0,
	)
	if status != 0 {
		return nil
	}
	count := *(*uint32)(unsafe.Pointer(&buffer[0]))
	rowSize := unsafe.Sizeof(tcpRowOwnerPID{})
	if uintptr(4)+uintptr(count)*rowSize > uintptr(len(buffer)) {
		return nil
	}
	rows := make([]tcpRowOwnerPID, 0, count)
	for index := uint32(0); index < count; index++ {
		rowOffset := 4 + int(index)*int(rowSize)
		rows = append(rows, *(*tcpRowOwnerPID)(unsafe.Pointer(&buffer[rowOffset])))
	}
	return rows
}

func networkPort(value uint32) uint16 {
	port := uint16(value)
	return port<<8 | port>>8
}

func processImageName(pid uint32) string {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buffer[:size])
}
