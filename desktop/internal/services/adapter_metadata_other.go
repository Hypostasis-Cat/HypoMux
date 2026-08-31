//go:build !windows && !darwin

package services

func adapterPlatformMetadata() map[int]adapterMetadata {
	return map[int]adapterMetadata{}
}
