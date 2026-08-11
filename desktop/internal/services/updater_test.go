package services

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

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

func TestUpdaterFallsBackToReleaseFeedWhenAPIIsRateLimited(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <title>HypoMux 2.5.5</title>
    <link rel="alternate" href="https://github.com/Hypostasis-Cat/HypoMux/releases/tag/v2.5.5" />
  </entry>
</feed>`
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := &http.Response{
			Request: request,
			Header:  make(http.Header),
			Body:    io.NopCloser(strings.NewReader("")),
		}
		switch {
		case request.URL.String() == latestReleaseAPI:
			response.StatusCode = http.StatusForbidden
		case request.URL.String() == releasesFeedURL:
			response.StatusCode = http.StatusOK
			response.Body = io.NopCloser(strings.NewReader(feed))
		case request.Method == http.MethodHead && request.URL.String() ==
			releaseDownloadURL+"v2.5.5/HypoMux_Setup_2.5.5.exe":
			response.StatusCode = http.StatusOK
			response.ContentLength = 30_000_000
		default:
			response.StatusCode = http.StatusNotFound
		}
		return response, nil
	})}
	service := NewUpdaterService()
	service.client = client

	result, err := service.Check()
	if err != nil {
		t.Fatal(err)
	}
	if !result.Available || result.Release.TagName != "v2.5.5" {
		t.Fatalf("fallback update result = %#v", result)
	}
	if result.Release.InstallerName != "HypoMux_Setup_2.5.5.exe" ||
		result.Release.InstallerSize != 30_000_000 {
		t.Fatalf("fallback release = %#v", result.Release)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
