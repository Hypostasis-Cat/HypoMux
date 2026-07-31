package services

import "testing"

func TestUpdaterVersionComparison(t *testing.T) {
	cases := []struct {
		candidate string
		current   string
		newer     bool
	}{
		{"v2.1.1", "2.1.0", true},
		{"2.2.0", "2.1.9", true},
		{"v2.1.0", "2.1.0", false},
		{"v2.0.9", "2.1.0", false},
		{"latest", "2.1.0", false},
	}
	for _, item := range cases {
		if actual := isNewerVersion(item.candidate, item.current); actual != item.newer {
			t.Fatalf("isNewerVersion(%q, %q) = %v", item.candidate, item.current, actual)
		}
	}
}

func TestUpdaterRejectsNonGitHubURLs(t *testing.T) {
	for _, value := range []string{
		"http://github.com/Hypostasis-Cat/HypoMux",
		"https://github.com.example.invalid/HypoMux.exe",
		"https://example.com/HypoMux.exe",
	} {
		if validateGitHubURL(value) == nil {
			t.Fatalf("unsafe update URL was accepted: %s", value)
		}
	}
	if err := validateGitHubURL("https://github.com/Hypostasis-Cat/HypoMux/releases/download/v2.3.0/HypoMux_Setup_2.3.0.exe"); err != nil {
		t.Fatalf("official GitHub URL rejected: %v", err)
	}
}
