//go:build !windows

package services

func adapterPlatformMetadata() map[int]adapterMetadata {
	return map[int]adapterMetadata{}
}
