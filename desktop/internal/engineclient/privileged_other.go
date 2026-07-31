//go:build !windows

package engineclient

import (
	"context"
	"errors"
)

var ErrElevationCancelled = errors.New("用户取消了管理员权限请求")

type unsupportedPrivilegedLauncher struct{}

func newPrivilegedLauncher() coreLauncher {
	return unsupportedPrivilegedLauncher{}
}

func PrivilegedLaunchSupported() bool {
	return false
}

func (unsupportedPrivilegedLauncher) Launch(context.Context, string) (*coreSession, error) {
	return nil, errors.New("当前平台不支持独立管理员核心")
}
