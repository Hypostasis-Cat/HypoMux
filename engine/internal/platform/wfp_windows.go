//go:build windows

package platform

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func InspectWFP(repair bool) (WFPStatus, error) {
	status := WFPStatus{
		Elevated:        CurrentIdentity().Elevated,
		RepairAttempted: repair,
	}
	service, err := openBFEService(repair)
	if err != nil {
		status.Detail = err.Error()
		return status, err
	}
	defer service.Close()
	serviceStatus, err := service.Query()
	if err != nil {
		return status, fmt.Errorf("query Base Filtering Engine: %w", err)
	}
	status.BFERunning = serviceStatus.State == svc.Running
	if !status.BFERunning && repair {
		if !status.Elevated {
			return status, errors.New("repairing Base Filtering Engine requires an elevated Core")
		}
		if serviceStatus.State == svc.Stopped {
			if err := service.Start(); err != nil && !isServiceAlreadyRunning(err) {
				return status, fmt.Errorf("start Base Filtering Engine: %w", err)
			}
		}
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			serviceStatus, err = service.Query()
			if err != nil {
				return status, fmt.Errorf("verify Base Filtering Engine: %w", err)
			}
			if serviceStatus.State == svc.Running {
				status.BFERunning = true
				status.Repaired = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !status.BFERunning {
		status.Detail = "Base Filtering Engine is not running"
		return status, errors.New(status.Detail)
	}
	detail, err := openWFPEngine()
	status.EngineReady = err == nil
	status.Detail = detail
	if err != nil {
		return status, err
	}
	return status, nil
}

func openBFEService(repair bool) (*mgr.Service, error) {
	access := uint32(windows.SERVICE_QUERY_STATUS)
	if repair {
		access |= windows.SERVICE_START
	}
	return openBFEWithAccess(access)
}

func openBFEWithAccess(access uint32) (*mgr.Service, error) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return nil, fmt.Errorf("connect Service Control Manager: %w", err)
	}
	defer windows.CloseServiceHandle(manager)
	name, _ := windows.UTF16PtrFromString("BFE")
	handle, err := windows.OpenService(manager, name, access)
	if err != nil {
		return nil, fmt.Errorf("open Base Filtering Engine service: %w", err)
	}
	return &mgr.Service{Name: "BFE", Handle: windows.Handle(handle)}, nil
}

func isServiceAlreadyRunning(err error) bool {
	return errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) ||
		strings.Contains(strings.ToLower(err.Error()), "already running")
}

func openWFPEngine() (string, error) {
	library := windows.NewLazySystemDLL("fwpuclnt.dll")
	open := library.NewProc("FwpmEngineOpen0")
	closeEngine := library.NewProc("FwpmEngineClose0")
	var handle windows.Handle
	status, _, _ := open.Call(
		0,
		uintptr(^uint32(0)),
		0,
		0,
		uintptr(unsafe.Pointer(&handle)),
	)
	if status != 0 {
		err := syscall.Errno(status)
		return fmt.Sprintf("FwpmEngineOpen0 failed (0x%08X): %s", uint32(status), err.Error()), err
	}
	if handle != 0 {
		_, _, _ = closeEngine.Call(uintptr(handle))
	}
	return "Base Filtering Engine is running and FwpmEngineOpen0 succeeded", nil
}
