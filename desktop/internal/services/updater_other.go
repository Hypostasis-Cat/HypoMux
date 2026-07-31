//go:build !windows

package services

import "errors"

func launchInstallerAfterExit(string, int) error {
	return errors.New("当前平台不支持 Windows 安装包更新")
}
