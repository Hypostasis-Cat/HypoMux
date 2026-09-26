//go:build windows

package engineclient

import (
	"fmt"
	"io"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

type pipeFile interface {
	io.ReadWriteCloser
	SetReadDeadline(time.Time) error
	Fd() uintptr
}

// Take ownership of an overlapped pipe handle, including on failure. Go 1.26's
// os.File tracks a shared file offset even for pipes and races on duplex I/O
// (upstream fix f4ac29c3c743b30a1e1a2b7ef4eae2588ac5c6f3). WinIO uses independent
// overlapped operations without serializing reads behind writes or vice versa.
func newPipeFile(handle windows.Handle) (pipeFile, error) {
	file, err := winio.NewOpenFile(handle)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	connection, ok := file.(pipeFile)
	if !ok {
		_ = file.Close()
		return nil, fmt.Errorf("pipe transport does not support deadlines")
	}
	return connection, nil
}
