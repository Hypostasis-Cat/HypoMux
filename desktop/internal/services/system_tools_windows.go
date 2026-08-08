//go:build windows

package services

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func resolveWindowsPowerShellExecutable() (string, error) {
	candidates := make([]string, 0, 5)
	for _, root := range windowsRoots() {
		candidates = append(candidates,
			filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
			filepath.Join(root, "Sysnative", "WindowsPowerShell", "v1.0", "powershell.exe"),
		)
	}
	if path, err := exec.LookPath("powershell.exe"); err == nil {
		candidates = append(candidates, path)
	}
	if path := firstRegularExecutable(candidates); path != "" {
		return path, nil
	}
	return "", errors.New("Windows PowerShell is unavailable from System32, Sysnative, and PATH")
}

func resolveWindowsSystemExecutable(name string) (string, error) {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." {
		return "", errors.New("Windows system executable name is empty")
	}
	candidates := make([]string, 0, 5)
	for _, root := range windowsRoots() {
		candidates = append(candidates,
			filepath.Join(root, "System32", name),
			filepath.Join(root, "Sysnative", name),
		)
	}
	if path, err := exec.LookPath(name); err == nil {
		candidates = append(candidates, path)
	}
	if path := firstRegularExecutable(candidates); path != "" {
		return path, nil
	}
	return "", errors.New(name + " is unavailable from System32, Sysnative, and PATH")
}

func windowsRoots() []string {
	result := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, value := range []string{os.Getenv("SystemRoot"), os.Getenv("WINDIR")} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		value = filepath.Clean(value)
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func firstRegularExecutable(candidates []string) string {
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(strings.TrimSpace(candidate))
		if candidate == "" || candidate == "." {
			continue
		}
		key := strings.ToLower(candidate)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err == nil {
			return absolute
		}
	}
	return ""
}
