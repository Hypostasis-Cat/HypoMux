//go:build windows

package services

import (
	"encoding/csv"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func listRunningProcesses() ([]string, error) {
	command := exec.Command("tasklist", "/NH", "/FO", "CSV")
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("读取运行中进程失败：%w", err)
	}
	rows, err := csv.NewReader(strings.NewReader(decodeWindowsOutput(output))).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("解析进程列表失败：%w", err)
	}
	seen := map[string]string{}
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		name := strings.TrimSpace(row[0])
		if strings.HasSuffix(strings.ToLower(name), ".exe") && !strings.ContainsAny(name, "/\\:\x00") {
			seen[strings.ToLower(name)] = name
		}
	}
	result := make([]string, 0, len(seen))
	for _, name := range seen {
		result = append(result, name)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i]) < strings.ToLower(result[j]) })
	return result, nil
}

func decodeWindowsOutput(data []byte) string {
	if decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(data); err == nil {
		return string(decoded)
	}
	return string(data)
}
