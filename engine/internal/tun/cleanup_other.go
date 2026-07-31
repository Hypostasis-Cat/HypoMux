//go:build !windows

package tun

import "context"

func cleanupPlatform(context.Context) error {
	return nil
}
