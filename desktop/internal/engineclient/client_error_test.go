package engineclient

import (
	"strings"
	"testing"
)

func TestRemoteErrorIncludesCoreFailureDetail(t *testing.T) {
	err := (&RemoteError{
		Code:    "tun_failed",
		Message: "could not activate managed TUN lifecycle",
		Details: map[string]any{
			"message": "TUN interface HypoMux-Tun did not become ready within 20s",
		},
	}).Error()
	for _, fragment := range []string{"tun_failed", "could not activate", "did not become ready"} {
		if !strings.Contains(err, fragment) {
			t.Fatalf("remote error lost %q: %s", fragment, err)
		}
	}
}
