//go:build windows

package setup

import (
	"errors"

	"golang.org/x/sys/windows"
)

// A scanner or indexer can briefly open a just-probed executable without
// FILE_SHARE_WRITE. A running image or a holder that never closes still fails
// once the retry window expires.
func isTransientLock(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
