//go:build !windows

package services

import "fmt"

func listRunningProcesses() ([]string, error) {
	return nil, fmt.Errorf("运行中进程选择仅在 Windows 上可用")
}

func listRunningProcessChoices() ([]RunningProcess, error) {
	return nil, fmt.Errorf("运行中进程选择仅在 Windows 上可用")
}
