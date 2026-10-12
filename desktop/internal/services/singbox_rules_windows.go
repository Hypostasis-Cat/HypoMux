//go:build windows

package services

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func renameRuleSetFile(temporary, path string) error {
	// A rule-set watcher or antivirus scan can briefly hold a reader without
	// delete sharing. Keep the old file intact while retrying the replacement.
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := os.Rename(temporary, path)
		if err == nil || (!errors.Is(err, windows.ERROR_ACCESS_DENIED) &&
			!errors.Is(err, windows.ERROR_SHARING_VIOLATION)) {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return err
		}
		time.Sleep(min(20*time.Millisecond, remaining))
	}
}
