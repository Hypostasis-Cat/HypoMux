package services

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

var currentWFPFingerprint = wfpEnvironmentFingerprint

func wfpEnvironmentFingerprint() string {
	executable, _ := os.Executable()
	absolute, _ := filepath.Abs(executable)
	identity := absolute
	if info, err := os.Stat(absolute); err == nil {
		identity = fmt.Sprintf("%s:%d:%d", absolute, info.Size(), info.ModTime().UnixNano())
	}
	return fmt.Sprintf("%s|%s|%s", wfpPlatformFingerprint(), runtime.GOARCH, identity)
}
