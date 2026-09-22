//go:build windows

package services

import (
	"context"
	"os"
	"testing"
	"time"
)

// Opt-in and read-only: validates the real Windows query and ICMP ABI without
// changing interface settings or requiring elevation.
func TestMTUWindowsReadOnlySmoke(t *testing.T) {
	id := os.Getenv("HYPOMUX_MTU_SMOKE_ADAPTER")
	if id == "" {
		t.Skip("set HYPOMUX_MTU_SMOKE_ADAPTER for read-only Windows smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	info, err := readMTU(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if info.Current < 576 || info.GUID == "" || info.Address == "" {
		t.Fatalf("invalid info: %+v", info)
	}
	// Local echo exercises pointer layout, source address, and DF flag without
	// depending on an external target permitting echo requests.
	if ok, err := probeMTU(ctx, info.Address, info.Address, 576); err != nil || !ok {
		t.Fatalf("native ICMP smoke: %v %v", ok, err)
	}
	t.Logf("Read-only MTU query and ICMP passed: %s, MTU %d", id, info.Current)
}
