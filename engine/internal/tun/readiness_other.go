//go:build !windows

package tun

func tunPlatformReady(expectedAddress string) bool {
	_, ready := tunInterfaceWithExpectedAddress(expectedAddress)
	return ready
}
