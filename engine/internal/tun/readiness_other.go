//go:build !windows

package tun

func tunPlatformReady() bool {
	_, ready := tunInterfaceWithExpectedAddress()
	return ready
}
