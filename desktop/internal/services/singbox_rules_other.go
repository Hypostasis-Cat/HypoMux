//go:build !windows

package services

import "os"

func renameRuleSetFile(temporary, path string) error {
	return os.Rename(temporary, path)
}
