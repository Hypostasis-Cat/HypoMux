//go:build windows

package services

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func listRunningProcesses() ([]string, error) {
	processes, err := snapshotRunningProcesses(false)
	if err != nil {
		return nil, err
	}
	result := make([]string, len(processes))
	for index, process := range processes {
		result[index] = process.name
	}
	return result, nil
}

type runningProcessSnapshot struct {
	name string
	path string
}

func listRunningProcessChoices() ([]RunningProcess, error) {
	processes, err := snapshotRunningProcesses(true)
	if err != nil {
		return nil, err
	}
	result := make([]RunningProcess, len(processes))
	for index, process := range processes {
		result[index] = RunningProcess{
			Name: process.name,
			Icon: processIconDataURL(process.path),
		}
	}
	return result, nil
}

func snapshotRunningProcesses(resolvePaths bool) ([]runningProcessSnapshot, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("创建运行中进程快照失败：%w", err)
	}
	defer windows.CloseHandle(snapshot)

	seen := map[string]runningProcessSnapshot{}
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return []runningProcessSnapshot{}, nil
		}
		return nil, fmt.Errorf("读取运行中进程快照失败：%w", err)
	}
	for {
		name := strings.TrimSpace(windows.UTF16ToString(entry.ExeFile[:]))
		if strings.HasSuffix(strings.ToLower(name), ".exe") && !strings.ContainsAny(name, "/\\:\x00") {
			key := strings.ToLower(name)
			item := seen[key]
			if item.name == "" {
				item.name = name
			}
			if resolvePaths && item.path == "" {
				item.path = processImageName(entry.ProcessID)
			}
			seen[key] = item
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, fmt.Errorf("枚举运行中进程失败：%w", err)
		}
	}
	result := make([]runningProcessSnapshot, 0, len(seen))
	for _, process := range seen {
		result = append(result, process)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].name) < strings.ToLower(result[j].name)
	})
	return result, nil
}
