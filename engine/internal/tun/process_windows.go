//go:build windows

package tun

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type jobContainment struct {
	mu     sync.Mutex
	handle windows.Handle
}

func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	if len(command.Args) > 1 && command.Args[1] == "run" {
		// A private, hidden console lets the shutdown helper send an interrupt
		// without broadcasting to the engine or the user's terminal.
		command.SysProcAttr.CreationFlags = windows.CREATE_NEW_CONSOLE
	}
}

func interruptProcess(ctx context.Context, process *os.Process) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, executable, "signal-tun", strconv.Itoa(process.Pid))
	configureProcess(command)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("signal sing-box: %w: %s", err, output)
	}
	return nil
}

// InterruptConsoleProcess must run in a short-lived helper process, never in
// the service/desktop process: console attachment changes process-global state.
func InterruptConsoleProcess(pid uint32) error {
	if pid == 0 || pid == uint32(os.Getpid()) {
		return fmt.Errorf("invalid TUN process ID")
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	freeConsole := kernel.NewProc("FreeConsole")
	freeConsole.Call()
	if ok, _, err := kernel.NewProc("AttachConsole").Call(uintptr(pid)); ok == 0 {
		return fmt.Errorf("attach TUN console: %w", err)
	}
	defer freeConsole.Call()
	// Refuse a shared console. Broadcasting is safe only for the owned core
	// and this helper; CREATE_NEW_CONSOLE isolates it from the engine.
	var members [3]uint32
	count, _, err := kernel.NewProc("GetConsoleProcessList").Call(uintptr(unsafe.Pointer(&members[0])), uintptr(len(members)))
	if count != 2 || !((members[0] == pid && members[1] == uint32(os.Getpid())) || (members[1] == pid && members[0] == uint32(os.Getpid()))) {
		return fmt.Errorf("TUN console is not isolated (members=%d): %v", count, err)
	}
	// Ignore the helper's own copy of CTRL_BREAK; the core's Go signal handler
	// receives os.Interrupt and runs its normal close/cache-save sequence.
	handler := syscall.NewCallback(func(event uint32) uintptr {
		if event == windows.CTRL_BREAK_EVENT {
			return 1
		}
		return 0
	})
	if ok, _, err := kernel.NewProc("SetConsoleCtrlHandler").Call(handler, 1); ok == 0 {
		return fmt.Errorf("install helper signal handler: %w", err)
	}
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, 0); err != nil {
		return err
	}
	// Console control delivery is asynchronous. Keep this helper attached until
	// the target exits; tearing down its console attachment immediately can race
	// signal dispatch on Windows.
	status, err := windows.WaitForSingleObject(process, uint32(gracefulStopTimeout/time.Millisecond))
	if err != nil {
		return err
	}
	if status != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("TUN did not exit after interrupt")
	}
	return nil
}

func containProcess(process *os.Process) (processContainment, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags =
		windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure kill-on-close job: %w", err)
	}
	processHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(process.Pid),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("open sidecar process: %w", err)
	}
	defer windows.CloseHandle(processHandle)
	if err := windows.AssignProcessToJobObject(job, processHandle); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("assign sidecar to job: %w", err)
	}
	return &jobContainment{handle: job}, nil
}

func (j *jobContainment) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(j.handle)
	j.handle = 0
	return err
}
