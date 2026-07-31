//go:build !windows

package platform

import "errors"

func SetAutostart(bool) error {
	return errors.New("当前平台暂不支持开机自启")
}

func AutostartEnabled() (bool, error) {
	return false, nil
}
