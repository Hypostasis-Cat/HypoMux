//go:build windows

package services

import (
	"strings"
	"testing"
)

func TestListRunningProcessesReturnsSortedExecutableNames(t *testing.T) {
	processes, err := listRunningProcesses()
	if err != nil {
		t.Fatal(err)
	}
	if len(processes) == 0 {
		t.Fatal("native process snapshot returned no executables")
	}
	seen := make(map[string]struct{}, len(processes))
	previous := ""
	for _, name := range processes {
		key := strings.ToLower(name)
		if !strings.HasSuffix(key, ".exe") {
			t.Fatalf("process name is not an executable: %q", name)
		}
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate process name: %q", name)
		}
		if previous != "" && previous > key {
			t.Fatalf("process names are not sorted: %q before %q", previous, key)
		}
		seen[key] = struct{}{}
		previous = key
	}
}
