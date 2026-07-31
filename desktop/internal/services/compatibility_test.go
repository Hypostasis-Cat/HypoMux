package services

import (
	"strings"
	"testing"
)

func TestCompatibilityCatalogIsDeterministicAndContainsLegacySafetyProfiles(t *testing.T) {
	plan := normalizedCompatibilityPlan([]string{`C:\Tools\Mihomo.exe`, `c:\tools\mihomo.exe`}, nil)
	if len(plan.ProcessPaths) != 1 {
		t.Fatalf("paths were not deduplicated case-insensitively: %#v", plan.ProcessPaths)
	}
	joined := strings.ToLower(strings.Join(plan.ProcessNames, "|"))
	for _, required := range []string{"mihomo.exe", "v2ray.exe", "qiyou.exe", "uu.exe", "leigod.exe"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("compatibility catalog is missing %s", required)
		}
	}
}
