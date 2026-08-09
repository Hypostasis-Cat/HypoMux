//go:build windows

package engineclient

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	sessionProtocol        = 1
	sessionAuthKind        = "hypomux.core.authenticate"
	sessionReadyKind       = "hypomux.host.ready"
	seeMaskNoCloseProcess  = 0x00000040
	maxSessionMessageBytes = 4096
)

var ErrElevationCancelled = errors.New("用户取消了管理员权限请求")

type privilegedLauncher struct{}

type sessionAuthMessage struct {
	Protocol int    `json:"protocol"`
	Kind     string `json:"kind"`
	Token    string `json:"token"`
}

type sessionReadyMessage struct {
	Protocol int    `json:"protocol"`
	Kind     string `json:"kind"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

type namedPipeServer struct {
	handle windows.Handle
	name   string
	token  string
}

type shellExecuteInfo struct {
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

type windowsCoreProcess struct {
	handle windows.Handle
	pid    int
	mu     sync.Mutex
	once   sync.Once
}

func newPrivilegedLauncher() coreLauncher {
	return serviceFirstLauncher{
		service:  windowsServiceLauncher{},
		fallback: privilegedLauncher{},
	}
}

func PrivilegedLaunchSupported() bool {
	return true
}

func (privilegedLauncher) Launch(ctx context.Context, path string) (*coreSession, error) {
	pipe, err := createAuthenticatedPipe()
	if err != nil {
		return nil, fmt.Errorf("创建高权限核心通信通道失败：%w", err)
	}
	defer pipe.close()

	process, err := launchElevatedCore(path, pipe.name, pipe.token)
	if err != nil {
		return nil, err
	}
	connection, err := pipe.accept(ctx, process.pid)
	if err != nil {
		_ = process.Kill()
		_ = process.Wait()
		return nil, fmt.Errorf("验证高权限核心通信失败：%w", err)
	}
	return &coreSession{
		reader:  connection,
		writer:  connection,
		close:   connection.Close,
		process: process,
		path:    path,
	}, nil
}

func createAuthenticatedPipe() (*namedPipeServer, error) {
	random := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(random)
	name := `\\.\pipe\HypoMux-Core-` + token[:24]

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("读取当前用户 SID：%w", err)
	}
	descriptor, err := windows.SecurityDescriptorFromString(
		"D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;BA)(A;;GA;;;SY)",
	)
	if err != nil {
		return nil, fmt.Errorf("创建命名管道安全描述符：%w", err)
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateNamedPipe(
		namePointer,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE|windows.FILE_FLAG_OVERLAPPED,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1,
		64*1024,
		64*1024,
		0,
		&attributes,
	)
	if err != nil {
		return nil, err
	}
	return &namedPipeServer{handle: handle, name: name, token: token}, nil
}

func (p *namedPipeServer) accept(ctx context.Context, expectedPID int) (*os.File, error) {
	if err := connectAuthenticatedPipeServer(ctx, p.handle); err != nil {
		return nil, err
	}

	var clientPID uint32
	if err := windows.GetNamedPipeClientProcessId(p.handle, &clientPID); err != nil {
		return nil, fmt.Errorf("读取核心进程身份：%w", err)
	}
	if expectedPID <= 0 || clientPID != uint32(expectedPID) {
		return nil, fmt.Errorf("拒绝非预期核心进程（PID %d）", clientPID)
	}

	connection := os.NewFile(uintptr(p.handle), p.name)
	p.handle = windows.InvalidHandle
	if connection == nil {
		return nil, errors.New("创建命名管道文件句柄失败")
	}
	if err := authenticateCore(ctx, connection, p.token); err != nil {
		_ = connection.Close()
		return nil, err
	}
	return connection, nil
}

func connectAuthenticatedPipeServer(ctx context.Context, handle windows.Handle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return fmt.Errorf("创建核心管道连接事件失败：%w", err)
	}
	defer windows.CloseHandle(event)
	overlapped := windows.Overlapped{HEvent: event}
	err = windows.ConnectNamedPipe(handle, &overlapped)
	if err == nil || errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		return nil
	}
	if !errors.Is(err, windows.ERROR_IO_PENDING) {
		return fmt.Errorf("连接高权限核心管道失败：%w", err)
	}
	for {
		result, waitErr := windows.WaitForSingleObject(event, 50)
		if waitErr != nil {
			return fmt.Errorf("等待高权限核心连接失败：%w", waitErr)
		}
		switch result {
		case windows.WAIT_OBJECT_0:
			var transferred uint32
			if err := windows.GetOverlappedResult(handle, &overlapped, &transferred, false); err != nil {
				return fmt.Errorf("完成高权限核心连接失败：%w", err)
			}
			return nil
		case uint32(windows.WAIT_TIMEOUT):
			if ctx.Err() == nil {
				continue
			}
			_ = windows.CancelIoEx(handle, &overlapped)
			var transferred uint32
			_ = windows.GetOverlappedResult(handle, &overlapped, &transferred, true)
			return ctx.Err()
		default:
			return fmt.Errorf("等待高权限核心连接返回 0x%X", result)
		}
	}
}

func authenticateCore(ctx context.Context, connection *os.File, token string) error {
	result := make(chan error, 1)
	go func() {
		reader := bufio.NewReaderSize(connection, maxSessionMessageBytes)
		line, err := reader.ReadBytes('\n')
		if err != nil {
			result <- err
			return
		}
		if len(line) > maxSessionMessageBytes {
			result <- errors.New("核心身份消息过长")
			return
		}
		var auth sessionAuthMessage
		if err := json.Unmarshal(line, &auth); err != nil {
			result <- errors.New("核心身份消息无效")
			return
		}
		tokenMatches := subtle.ConstantTimeCompare([]byte(auth.Token), []byte(token)) == 1
		if auth.Protocol != sessionProtocol || auth.Kind != sessionAuthKind || !tokenMatches {
			_ = writeSessionMessage(connection, sessionReadyMessage{
				Protocol: sessionProtocol,
				Kind:     sessionReadyKind,
				OK:       false,
				Error:    "authentication_failed",
			})
			result <- errors.New("核心一次性会话凭据不匹配")
			return
		}
		result <- writeSessionMessage(connection, sessionReadyMessage{
			Protocol: sessionProtocol,
			Kind:     sessionReadyKind,
			OK:       true,
		})
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		_ = connection.Close()
		return ctx.Err()
	}
}

func writeSessionMessage(writer io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = writer.Write(append(data, '\n'))
	return err
}

func launchElevatedCore(path, pipeName, token string) (*windowsCoreProcess, error) {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	parameters := "serve-pipe --pipe " + pipeName +
		" --session-token " + token +
		" --host-pid " + strconv.Itoa(os.Getpid())
	parameterPointer, err := windows.UTF16PtrFromString(parameters)
	if err != nil {
		return nil, err
	}
	directory, err := windows.UTF16PtrFromString(filepathDir(path))
	if err != nil {
		return nil, err
	}
	info := shellExecuteInfo{
		Mask:       seeMaskNoCloseProcess,
		Verb:       verb,
		File:       file,
		Parameters: parameterPointer,
		Directory:  directory,
		Show:       0,
	}
	info.Size = uint32(unsafe.Sizeof(info))
	procedure := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")
	result, _, callErr := procedure.Call(uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) || errors.Is(callErr, syscall.Errno(windows.ERROR_CANCELLED)) {
			return nil, ErrElevationCancelled
		}
		return nil, fmt.Errorf("启动管理员核心失败：%w", callErr)
	}
	if info.Process == 0 || info.Process == windows.InvalidHandle {
		return nil, errors.New("管理员核心未返回进程句柄")
	}
	pid, err := windows.GetProcessId(info.Process)
	if err != nil {
		_ = windows.CloseHandle(info.Process)
		return nil, fmt.Errorf("读取管理员核心进程 ID：%w", err)
	}
	return &windowsCoreProcess{handle: info.Process, pid: int(pid)}, nil
}

func filepathDir(path string) string {
	index := len(path) - 1
	for index >= 0 && path[index] != '\\' && path[index] != '/' {
		index--
	}
	if index <= 0 {
		return "."
	}
	return path[:index]
}

func (p *namedPipeServer) close() {
	if p.handle != 0 && p.handle != windows.InvalidHandle {
		_ = windows.CloseHandle(p.handle)
		p.handle = windows.InvalidHandle
	}
}

func (p *windowsCoreProcess) Wait() error {
	p.mu.Lock()
	handle := p.handle
	p.mu.Unlock()
	event, err := windows.WaitForSingleObject(handle, windows.INFINITE)
	p.release()
	if err != nil {
		return err
	}
	if event != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("等待管理员核心退出返回状态 0x%X", event)
	}
	return nil
}

func (p *windowsCoreProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handle == 0 || p.handle == windows.InvalidHandle {
		return nil
	}
	err := windows.TerminateProcess(p.handle, 1)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return nil
	}
	return err
}

func (p *windowsCoreProcess) PID() int {
	return p.pid
}

func (p *windowsCoreProcess) release() {
	p.once.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.handle != 0 && p.handle != windows.InvalidHandle {
			_ = windows.CloseHandle(p.handle)
			p.handle = windows.InvalidHandle
		}
	})
}
