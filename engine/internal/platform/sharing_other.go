//go:build !windows

package platform

import (
	"context"
	"errors"
)

func InspectSharing(context.Context) (SharingSnapshot, error) {
	return SharingSnapshot{}, errors.New("Windows sharing is unavailable on this platform")
}
