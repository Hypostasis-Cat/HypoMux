package services

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestInstallAndQuitUsesLifecycleCallbackAfterLauncherStarts(t *testing.T) {
	quitCalled := false
	service := NewUpdaterService(func() { quitCalled = true })
	launchCalled := false
	service.launchInstaller = func(path string, processID int) error {
		launchCalled = true
		if path != "installer.exe" || processID <= 0 {
			t.Fatalf("unexpected launcher arguments: path=%q pid=%d", path, processID)
		}
		return nil
	}

	if err := service.InstallAndQuit("installer.exe"); err != nil {
		t.Fatal(err)
	}
	if !launchCalled || !quitCalled {
		t.Fatalf("launch=%t quit=%t", launchCalled, quitCalled)
	}
	if progress := service.Progress(); progress.State != "installing" {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestInstallAndQuitDoesNotQuitWhenLauncherFails(t *testing.T) {
	quitCalled := false
	service := NewUpdaterService(func() { quitCalled = true })
	service.launchInstaller = func(string, int) error { return errors.New("launcher failed") }

	if err := service.InstallAndQuit("installer.exe"); err == nil {
		t.Fatal("launcher failure was ignored")
	}
	if quitCalled {
		t.Fatal("application quit after launcher failure")
	}
	if progress := service.Progress(); progress.State != "failed" {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestInstallAndQuitRequiresLifecycleCallback(t *testing.T) {
	service := NewUpdaterService()
	service.launchInstaller = func(string, int) error {
		t.Fatal("launcher should not run without lifecycle callback")
		return nil
	}

	if err := service.InstallAndQuit("installer.exe"); err == nil {
		t.Fatal("missing lifecycle callback was accepted")
	}
}

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

func TestUpdaterAcceptsOnlyExactOfficialInstallerURLs(t *testing.T) {
	const tagName = "v2.5.8"
	const installerName = "HypoMux_Setup_2.5.8.exe"
	for _, value := range []string{
		"http://github.com/Hypostasis-Cat/HypoMux/releases/download/v2.5.8/" + installerName,
		"https://user@github.com/Hypostasis-Cat/HypoMux/releases/download/v2.5.8/" + installerName,
		"https://github.com.example.invalid/Hypostasis-Cat/HypoMux/releases/download/v2.5.8/" + installerName,
		"https://github.com:443/Hypostasis-Cat/HypoMux/releases/download/v2.5.8/" + installerName,
		"https://github.com/Hypostasis-Cat/HypoMux/releases/download/v2.5.8/" + installerName + "?download=1",
		"https://github.com/Hypostasis-Cat/HypoMux/releases/download/v2.5.8/" + installerName + "#fragment",
		"https://github.com/Hypostasis-Cat/HypoMux/releases/download/v2.5.8/%48ypoMux_Setup_2.5.8.exe",
		"https://github.com/Hypostasis-Cat/HypoMux/releases/download/v2.5.8/../v2.5.8/" + installerName,
		"https://github.com/Hypostasis-Cat/HypoMux/releases/download/v2.5.9/" + installerName,
		"https://github.com/another/repository/releases/download/v2.5.8/" + installerName,
		"https://cnb.cool/another/repository/-/releases/download/v2.5.8/" + installerName,
	} {
		if validateInstallerMirrorURL(value, tagName, installerName) == nil {
			t.Fatalf("unsafe installer URL was accepted: %s", value)
		}
	}
	for _, value := range officialInstallerMirrors(tagName, installerName) {
		if err := validateInstallerMirrorURL(value, tagName, installerName); err != nil {
			t.Fatalf("official installer URL rejected: %s: %v", value, err)
		}
	}
}

func TestManifestRequiresSchemaDigestAndExactInstallerMetadata(t *testing.T) {
	valid := testManifest("2.5.8", []byte("payload"))
	if _, err := releaseFromManifest(valid); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	cases := map[string]func(*updateManifest){
		"missing schema": func(value *updateManifest) { value.SchemaVersion = 0 },
		"missing digest": func(value *updateManifest) { value.Installer.SHA256 = "" },
		"short digest":   func(value *updateManifest) { value.Installer.SHA256 = strings.Repeat("a", 63) },
		"wrong name":     func(value *updateManifest) { value.Installer.Name = "HypoMux_Setup_2.5.9.exe" },
		"zero size":      func(value *updateManifest) { value.Installer.Size = 0 },
		"no mirrors":     func(value *updateManifest) { value.Installer.URLs = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			candidate.Installer.URLs = append([]string(nil), valid.Installer.URLs...)
			mutate(&candidate)
			if _, err := releaseFromManifest(candidate); err == nil {
				t.Fatal("invalid manifest was accepted")
			}
		})
	}
}

func TestUpdaterChoosesNewestMetadataSource(t *testing.T) {
	githubManifest, githubSignature := signedManifestJSON(t, testManifest("2.5.8", []byte("new")))
	cnbManifest, cnbSignature := signedManifestJSON(t, testManifest("2.5.7", []byte("old")))
	service := NewUpdaterService()
	service.manifestPublicKey = testManifestPublicKey()
	service.client = clientFor(func(request *http.Request) *http.Response {
		switch request.URL.String() {
		case githubLatestManifestURL:
			return stringResponse(request, http.StatusOK, githubManifest)
		case githubLatestManifestURL + ".sig":
			return bytesResponse(request, http.StatusOK, githubSignature)
		case cnbLatestManifestURL:
			return stringResponse(request, http.StatusOK, cnbManifest)
		case cnbLatestManifestURL + ".sig":
			return bytesResponse(request, http.StatusOK, cnbSignature)
		default:
			return stringResponse(request, http.StatusServiceUnavailable, "")
		}
	})

	result, err := service.Check()
	if err != nil {
		t.Fatal(err)
	}
	if !result.Available || result.Release.TagName != "v2.5.8" {
		t.Fatalf("newest update result = %#v", result)
	}
}

func TestUpdaterRejectsConflictingMetadataForSameVersion(t *testing.T) {
	left := releaseFromTestManifest(t, testManifest("2.5.8", []byte("left")))
	right := releaseFromTestManifest(t, testManifest("2.5.8", []byte("right")))
	if _, err := selectLatestRelease([]ReleaseInfo{left, right}); err == nil {
		t.Fatal("conflicting metadata for one version was accepted")
	}
}

func TestUpdaterUsesCNBManifestWhenGitHubMetadataIsUnavailable(t *testing.T) {
	manifest, signature := signedManifestJSON(t, testManifest("2.5.8", []byte("payload")))
	service := NewUpdaterService()
	service.manifestPublicKey = testManifestPublicKey()
	service.client = clientFor(func(request *http.Request) *http.Response {
		switch request.URL.String() {
		case cnbLatestManifestURL:
			return stringResponse(request, http.StatusOK, manifest)
		case cnbLatestManifestURL + ".sig":
			return bytesResponse(request, http.StatusOK, signature)
		}
		return stringResponse(request, http.StatusServiceUnavailable, "")
	})

	result, err := service.Check()
	if err != nil {
		t.Fatal(err)
	}
	if result.Release.TagName != "v2.5.8" || result.Release.InstallerURLs[0] !=
		cnbDownloadURL+"v2.5.8/HypoMux_Setup_2.5.8.exe" {
		t.Fatalf("CNB manifest result = %#v", result.Release)
	}
}

func TestUpdaterRejectsMissingTamperedOrWrongManifestSignature(t *testing.T) {
	manifest, signature := signedManifestJSON(t, testManifest("2.5.8", []byte("payload")))
	_, wrongSignature := signedManifestJSON(t, testManifest("2.5.9", []byte("other")))
	tests := []struct {
		name            string
		manifest        string
		signature       []byte
		signatureStatus int
	}{
		{name: "missing", manifest: manifest, signatureStatus: http.StatusNotFound},
		{name: "tampered manifest", manifest: manifest + " ", signature: signature, signatureStatus: http.StatusOK},
		{name: "wrong signature", manifest: manifest, signature: wrongSignature, signatureStatus: http.StatusOK},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewUpdaterService()
			service.manifestPublicKey = testManifestPublicKey()
			service.client = clientFor(func(request *http.Request) *http.Response {
				switch request.URL.String() {
				case cnbLatestManifestURL:
					return stringResponse(request, http.StatusOK, testCase.manifest)
				case cnbLatestManifestURL + ".sig":
					return bytesResponse(request, testCase.signatureStatus, testCase.signature)
				default:
					return stringResponse(request, http.StatusNotFound, "")
				}
			})
			if _, err := service.checkUpdateManifest(context.Background(), cnbLatestManifestURL); err == nil {
				t.Fatal("untrusted manifest was accepted")
			}
		})
	}
}

func TestGitHubTagDiscoveryUsesSignedManifestAndCNBDownloadFallback(t *testing.T) {
	payload := []byte("signed-installer-placeholder")
	manifest, signature := signedManifestJSON(t, testManifest("2.5.8", payload))
	taggedManifestURL := releaseDownloadURL + "v2.5.8/" + updateManifestName
	apiPayload := `{
  "tag_name":"v2.5.8",
  "name":"forged unsigned metadata",
  "assets":[{
    "name":"HypoMux_Setup_2.5.8.exe",
    "browser_download_url":"https://github.com/Hypostasis-Cat/HypoMux/releases/download/v2.5.8/HypoMux_Setup_2.5.8.exe",
    "size":999999,
    "digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }]
}`
	service := NewUpdaterService()
	service.manifestPublicKey = testManifestPublicKey()
	service.verifyInstaller = func(string) error { return nil }
	service.client = clientFor(func(request *http.Request) *http.Response {
		switch request.URL.String() {
		case latestReleaseAPI:
			return stringResponse(request, http.StatusOK, apiPayload)
		case taggedManifestURL:
			return stringResponse(request, http.StatusOK, manifest)
		case taggedManifestURL + ".sig":
			return bytesResponse(request, http.StatusOK, signature)
		case releaseDownloadURL + "v2.5.8/HypoMux_Setup_2.5.8.exe":
			return stringResponse(request, http.StatusServiceUnavailable, "")
		case cnbDownloadURL + "v2.5.8/HypoMux_Setup_2.5.8.exe":
			return bytesResponse(request, http.StatusOK, payload)
		default:
			return stringResponse(request, http.StatusNotFound, "")
		}
	})

	result, err := service.Check()
	if err != nil {
		t.Fatal(err)
	}
	// Mirror lists are generic. Reversing the default CNB-first preference
	// proves that download fallback is independent from the metadata source.
	result.Release.InstallerURLs[0], result.Release.InstallerURLs[1] =
		result.Release.InstallerURLs[1], result.Release.InstallerURLs[0]
	installerPath, err := service.Download(result.Release)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(installerPath))
}

func TestGitHubAPICannotBypassSignedManifest(t *testing.T) {
	const apiPayload = `{
  "tag_name":"v99.0.0",
  "name":"forged unsigned release",
  "body":"must never enter ReleaseInfo",
  "html_url":"https://github.com/Hypostasis-Cat/HypoMux/releases/tag/v99.0.0",
  "assets":[{
    "name":"HypoMux_Setup_99.0.0.exe",
    "browser_download_url":"https://github.com/Hypostasis-Cat/HypoMux/releases/download/v99.0.0/HypoMux_Setup_99.0.0.exe",
    "size":1,
    "digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }]
}`
	service := NewUpdaterService()
	service.manifestPublicKey = testManifestPublicKey()
	service.client = clientFor(func(request *http.Request) *http.Response {
		if request.URL.String() == latestReleaseAPI {
			return stringResponse(request, http.StatusOK, apiPayload)
		}
		return stringResponse(request, http.StatusNotFound, "")
	})

	if release, err := service.checkGitHubAPI(context.Background()); err == nil {
		t.Fatalf("unsigned GitHub API metadata entered ReleaseInfo: %#v", release)
	}
}

func TestUpdaterFallsBackToTaggedManifestThroughReleaseFeed(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"><entry>
  <title>HypoMux 2.5.8</title>
  <link rel="alternate" href="https://github.com/Hypostasis-Cat/HypoMux/releases/tag/v2.5.8" />
</entry></feed>`
	manifest, signature := signedManifestJSON(t, testManifest("2.5.8", []byte("payload")))
	taggedManifestURL := releaseDownloadURL + "v2.5.8/" + updateManifestName
	service := NewUpdaterService()
	service.manifestPublicKey = testManifestPublicKey()
	service.client = clientFor(func(request *http.Request) *http.Response {
		switch request.URL.String() {
		case releasesFeedURL:
			return stringResponse(request, http.StatusOK, feed)
		case taggedManifestURL:
			return stringResponse(request, http.StatusOK, manifest)
		case taggedManifestURL + ".sig":
			return bytesResponse(request, http.StatusOK, signature)
		default:
			return stringResponse(request, http.StatusServiceUnavailable, "")
		}
	})

	result, err := service.Check()
	if err != nil {
		t.Fatal(err)
	}
	if result.Release.TagName != "v2.5.8" || result.Release.InstallerSize != int64(len("payload")) {
		t.Fatalf("Atom fallback result = %#v", result.Release)
	}
}

func TestUpdaterDownloadFallsBackBetweenMirrorsInBothDirections(t *testing.T) {
	for _, testCase := range []struct {
		name string
		urls func(string, string) []string
	}{
		{name: "CNB to GitHub", urls: officialInstallerMirrors},
		{name: "GitHub to CNB", urls: func(tagName, installerName string) []string {
			values := officialInstallerMirrors(tagName, installerName)
			return []string{values[1], values[0]}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			payload := []byte("signed-installer-placeholder")
			release := testRelease("2.5.8", payload)
			release.InstallerURLs = testCase.urls(release.TagName, release.InstallerName)
			var mu sync.Mutex
			attempted := make([]string, 0, 2)
			service := NewUpdaterService()
			service.verifyInstaller = func(string) error { return nil }
			service.client = clientFor(func(request *http.Request) *http.Response {
				mu.Lock()
				attempted = append(attempted, request.URL.String())
				attempt := len(attempted)
				mu.Unlock()
				if attempt == 1 {
					return stringResponse(request, http.StatusServiceUnavailable, "")
				}
				return bytesResponse(request, http.StatusOK, payload)
			})

			installerPath, err := service.Download(release)
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(filepath.Dir(installerPath))
			if len(attempted) != 2 || attempted[0] != release.InstallerURLs[0] || attempted[1] != release.InstallerURLs[1] {
				t.Fatalf("mirror attempts = %#v", attempted)
			}
		})
	}
}

func TestUpdaterDownloadFailsWhenAllMirrorsFail(t *testing.T) {
	release := testRelease("2.5.8", []byte("payload"))
	service := NewUpdaterService()
	service.verifyInstaller = func(string) error { return nil }
	service.client = clientFor(func(request *http.Request) *http.Response {
		return stringResponse(request, http.StatusBadGateway, "")
	})

	if installerPath, err := service.Download(release); err == nil || installerPath != "" {
		t.Fatalf("all-mirror failure returned path=%q err=%v", installerPath, err)
	}
}

func TestUpdaterDigestMismatchIsFailClosedAcrossMirrors(t *testing.T) {
	goodPayload := []byte("expected-payload")
	release := testRelease("2.5.8", goodPayload)
	release.InstallerURLs[0], release.InstallerURLs[1] = release.InstallerURLs[1], release.InstallerURLs[0]
	verifyCalls := 0
	attempt := 0
	service := NewUpdaterService()
	service.verifyInstaller = func(string) error { verifyCalls++; return nil }
	service.client = clientFor(func(request *http.Request) *http.Response {
		attempt++
		if attempt == 1 {
			return bytesResponse(request, http.StatusOK, []byte("tampered-payload"))
		}
		return bytesResponse(request, http.StatusOK, goodPayload)
	})

	if installerPath, err := service.Download(release); err == nil || installerPath != "" {
		t.Fatalf("digest inconsistency returned path=%q err=%v", installerPath, err)
	}
	if attempt != 2 || verifyCalls != 1 {
		t.Fatalf("attempts=%d verifyCalls=%d", attempt, verifyCalls)
	}
}

func TestUpdaterRejectsMissingDigestBeforeNetwork(t *testing.T) {
	release := testRelease("2.5.8", []byte("payload"))
	release.InstallerDigest = ""
	requests := 0
	service := NewUpdaterService()
	service.client = clientFor(func(request *http.Request) *http.Response {
		requests++
		return stringResponse(request, http.StatusOK, "")
	})

	if _, err := service.Download(release); err == nil {
		t.Fatal("missing digest was accepted")
	}
	if requests != 0 {
		t.Fatalf("made %d network requests before validating digest", requests)
	}
}

func TestUpdaterRejectsSizeMismatchBeforeSignatureCheck(t *testing.T) {
	payload := []byte("payload")
	release := testRelease("2.5.8", payload)
	verifyCalls := 0
	service := NewUpdaterService()
	service.verifyInstaller = func(string) error { verifyCalls++; return nil }
	service.client = clientFor(func(request *http.Request) *http.Response {
		return bytesResponse(request, http.StatusOK, append(payload, '!'))
	})

	if _, err := service.Download(release); err == nil {
		t.Fatal("size mismatch was accepted")
	}
	if verifyCalls != 0 {
		t.Fatalf("signature check ran %d times on wrong-sized data", verifyCalls)
	}
}

func TestUpdaterRejectsUntrustedAuthenticodePublisher(t *testing.T) {
	payload := []byte("payload")
	release := testRelease("2.5.8", payload)
	service := NewUpdaterService()
	service.verifyInstaller = func(string) error { return errors.New("unexpected publisher") }
	service.client = clientFor(func(request *http.Request) *http.Response {
		return bytesResponse(request, http.StatusOK, payload)
	})

	if installerPath, err := service.Download(release); err == nil || installerPath != "" {
		t.Fatalf("untrusted signature returned path=%q err=%v", installerPath, err)
	}
}

func testManifest(version string, payload []byte) updateManifest {
	digest := sha256.Sum256(payload)
	tagName := normalizeTagName(version)
	version = strings.TrimPrefix(tagName, "v")
	installerName := "HypoMux_Setup_" + version + ".exe"
	manifest := updateManifest{
		SchemaVersion: 1,
		Version:       version,
		Name:          "HypoMux " + version,
		Notes:         "Release notes",
	}
	manifest.Installer.Name = installerName
	manifest.Installer.Size = int64(len(payload))
	manifest.Installer.SHA256 = hex.EncodeToString(digest[:])
	manifest.Installer.URLs = officialInstallerMirrors(tagName, installerName)
	return manifest
}

func testRelease(version string, payload []byte) ReleaseInfo {
	return releaseFromTestManifest(nil, testManifest(version, payload))
}

func releaseFromTestManifest(t *testing.T, manifest updateManifest) ReleaseInfo {
	release, err := releaseFromManifest(manifest)
	if err != nil {
		if t != nil {
			t.Fatal(err)
		}
		panic(err)
	}
	return release
}

func signedManifestJSON(t *testing.T, manifest updateManifest) (string, []byte) {
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload), ed25519.Sign(testManifestPrivateKey(), payload)
}

func testManifestPrivateKey() ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("HypoMux updater manifest test key"))
	return ed25519.NewKeyFromSeed(seed[:])
}

func testManifestPublicKey() ed25519.PublicKey {
	return testManifestPrivateKey().Public().(ed25519.PublicKey)
}

func clientFor(handler func(*http.Request) *http.Response) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return handler(request), nil
	})}
}

func stringResponse(request *http.Request, status int, body string) *http.Response {
	return bytesResponse(request, status, []byte(body))
}

func bytesResponse(request *http.Request, status int, body []byte) *http.Response {
	response := &http.Response{
		Request:       request,
		Header:        make(http.Header),
		StatusCode:    status,
		Body:          io.NopCloser(strings.NewReader(string(body))),
		ContentLength: int64(len(body)),
	}
	if status != http.StatusOK {
		response.ContentLength = 0
	}
	return response
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
