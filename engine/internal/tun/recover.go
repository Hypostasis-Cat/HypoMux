package tun

import "context"

// Recover removes HypoMux-owned TUN routes and devices left behind by an
// interrupted session. It is intentionally narrow: cleanupPlatform only
// targets the HypoMux-Tun adapter and its default routes.
func Recover(ctx context.Context) error {
	return cleanupPlatform(ctx)
}
