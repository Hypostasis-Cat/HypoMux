//go:build !windows

package services

import "errors"

func verifyDownloadedInstallerAuthenticity(string) error {
	return errors.New("当前平台不支持 Windows Authenticode 验证")
}
