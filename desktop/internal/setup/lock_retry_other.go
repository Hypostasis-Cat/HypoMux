//go:build !windows

package setup

func isTransientLock(error) bool { return false }
