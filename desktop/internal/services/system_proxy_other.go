//go:build !windows && !darwin

package services

import "fmt"

func enableSystemProxy(_ int, _ int) error {
	return fmt.Errorf("系统代理模式仅在 Windows 上可用")
}

func restoreSystemProxy() error { return nil }

func restoreSystemProxyDetailed() (string, error) { return "", nil }
