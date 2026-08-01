//go:build !windows

package startup

import (
	"context"
	"errors"
)

// CleanupZombieProcesses is a no-op on non-Windows platforms.
func CleanupZombieProcesses(ctx context.Context) error {
	return errors.New("zombie process cleanup is only supported on Windows")
}
