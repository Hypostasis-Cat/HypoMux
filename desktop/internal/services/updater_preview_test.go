package services

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestPreviewVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		candidate, current string
		newer              bool
	}{
		{"2.7.0-beta.10", "2.7.0-beta.2", true},
		{"2.7.0-rc.1", "2.7.0-beta.99", true},
		{"2.7.0", "2.7.0-rc.99", true},
		{"2.7.0-rc.99", "2.7.0", false},
		{"2.7.0-rc.1", "2.7.0-rc.1", false},
		{"2.7.0-bata1", "2.6.0", false},
		{"2.7.0-rc.01", "2.6.0", false},
	} {
		if isNewerVersion(tc.candidate, tc.current) != tc.newer {
			t.Errorf("comparison: %+v", tc)
		}
	}
	if sameVersion("2.7.0-beta.1", "2.7.0-rc.1") || sameVersion("2.7.0-rc.1", "2.7.0") {
		t.Fatal("prerelease identity lost")
	}
}

func TestPreviewUpdateChannels(t *testing.T) {
	for _, tc := range []struct {
		name, channel, current, stable, preview, want string
		available, wantError                          bool
	}{
		{"stable never checks preview", "stable", "2.6.0", "2.6.0", "2.7.0-beta.1", "v2.6.0", false, false},
		{"stable opts into preview", "preview", "2.6.0", "2.6.0", "2.7.0-beta.1", "v2.7.0-beta.1", true, false},
		{"preview follows RC", "preview", "2.7.0-beta.1", "2.6.0", "2.7.0-rc.1", "v2.7.0-rc.1", true, false},
		{"preview graduates", "preview", "2.7.0-rc.1", "2.7.0", "2.7.0-rc.1", "v2.7.0", true, false},
		{"stable channel rejects preview", "stable", "2.6.0", "2.7.0-rc.1", "", "", false, true},
		{"preview outage falls back to stable", "preview", "2.7.0-rc.1", "2.7.0", "", "v2.7.0", true, false},
		{"never downgrade preview", "preview", "2.7.0-rc.2", "2.6.0", "2.7.0-rc.1", "v2.7.0-rc.1", false, false},
		{"switching stable does not downgrade", "stable", "2.7.0-rc.1", "2.6.0", "2.7.0-rc.2", "v2.6.0", false, false},
		{"switching stable accepts final", "stable", "2.7.0-rc.1", "2.7.0", "2.8.0-beta.1", "v2.7.0", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := NewUpdaterService()
			service.manifestPublicKey = testManifestPublicKey()
			stableBody, stableSig := signedManifestJSON(t, testManifest(tc.stable, []byte("stable")))
			previewBody, previewSig := "", []byte(nil)
			if tc.preview != "" {
				previewBody, previewSig = signedManifestJSON(t, testManifest(tc.preview, []byte("preview")))
			}
			var mu sync.Mutex
			previewRequests := 0
			service.client = clientFor(func(r *http.Request) *http.Response {
				body, sig := stableBody, stableSig
				if strings.Contains(r.URL.Path, "update-channel-preview/") {
					mu.Lock()
					previewRequests++
					mu.Unlock()
					if tc.preview == "" {
						return stringResponse(r, http.StatusNotFound, "")
					}
					body, sig = previewBody, previewSig
				}
				if strings.HasSuffix(r.URL.Path, ".sig") {
					return bytesResponse(r, http.StatusOK, sig)
				}
				return stringResponse(r, http.StatusOK, body)
			})
			result, err := service.checkVersion(tc.current, tc.channel)
			if (err != nil) != tc.wantError {
				t.Fatalf("error: %v", err)
			}
			if !tc.wantError && (result.Release.TagName != tc.want || result.Available != tc.available) {
				t.Fatalf("result: %+v", result)
			}
			if tc.channel == "stable" && previewRequests != 0 {
				t.Fatal("stable client queried preview channel")
			}
		})
	}
}

func TestUpdaterReadsSavedChannelOnEveryCheck(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	s := NewUpdaterServiceWithSettings(settings, func() {})
	s.manifestPublicKey = testManifestPublicKey()
	body, sig := signedManifestJSON(t, testManifest("65535.0.0-rc.1", []byte("preview")))
	s.client = clientFor(func(r *http.Request) *http.Response {
		if !strings.Contains(r.URL.Path, "update-channel-preview/") {
			return stringResponse(r, http.StatusNotFound, "")
		}
		if strings.HasSuffix(r.URL.Path, ".sig") {
			return bytesResponse(r, http.StatusOK, sig)
		}
		return stringResponse(r, http.StatusOK, body)
	})
	for _, channel := range []string{"stable", "preview", "stable"} {
		if _, err := settings.UpdateFields(AppSettings{UpdateChannel: channel}, []string{"update_channel"}); err != nil {
			t.Fatal(err)
		}
		result, err := s.Check()
		if channel == "preview" {
			if err != nil || !result.Available {
				t.Fatalf("saved preview channel ignored: %+v %v", result, err)
			}
		} else if err == nil {
			t.Fatal("stable mode used preview manifest")
		}
	}
}

func TestPreviewManifestRetainsURLAndSignatureValidation(t *testing.T) {
	manifest := testManifest("2.7.0-rc.1", []byte("preview"))
	if _, err := releaseFromManifest(manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Installer.URLs[0] = strings.Replace(manifest.Installer.URLs[0], "v2.7.0-rc.1/", "v2.7.0/", 1)
	if _, err := releaseFromManifest(manifest); err == nil {
		t.Fatal("mismatched preview URL accepted")
	}
	for _, endpoint := range []string{githubPreviewManifestURL, cnbPreviewManifestURL} {
		for _, suffix := range []string{"", ".sig"} {
			if err := validateUpdateMetadataURL(endpoint + suffix); err != nil {
				t.Fatal(err)
			}
		}
		if validateUpdateMetadataURL(endpoint+"?other=true") == nil {
			t.Fatal("noncanonical preview URL accepted")
		}
	}
}
