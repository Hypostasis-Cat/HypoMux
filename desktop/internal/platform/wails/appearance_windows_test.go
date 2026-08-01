//go:build windows

package wails

import "testing"

func TestNativeMaterialValuesDistinguishMicaAndSolid(t *testing.T) {
	mica, micaOK := nativeMaterialValue("mica")
	solid, solidOK := nativeMaterialValue("solid")
	if !micaOK || !solidOK {
		t.Fatal("supported window materials were not resolved")
	}
	if mica != 2 || solid != 1 || mica == solid {
		t.Fatalf("unexpected DWM backdrop values: mica=%d solid=%d", mica, solid)
	}
}
