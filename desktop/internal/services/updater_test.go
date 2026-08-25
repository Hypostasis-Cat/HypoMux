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
		githubLatestManifestURL,
		githubLatestManifestURL + ".sig",
		cnbLatestManifestURL,
		cnbLatestManifestURL + ".sig",
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

func TestUpdaterAcceptsOnlyExactSignedUpdateChannelURLs(t *testing.T) {
	for _, value := range []string{
		githubLatestManifestURL,
		githubLatestManifestURL + ".sig",
		cnbLatestManifestURL,
		cnbLatestManifestURL + ".sig",
	} {
		if err := validateUpdateMetadataURL(value); err != nil {
			t.Fatalf("official metadata URL rejected: %s: %v", value, err)
		}
	}
	for _, value := range []string{
		"http://raw.githubusercontent.com/Hypostasis-Cat/HypoMux/update-channel/latest.json",
		"https://user@raw.githubusercontent.com/Hypostasis-Cat/HypoMux/update-channel/latest.json",
		"https://raw.githubusercontent.com.example.invalid/Hypostasis-Cat/HypoMux/update-channel/latest.json",
		"https://raw.githubusercontent.com:443/Hypostasis-Cat/HypoMux/update-channel/latest.json",
		githubLatestManifestURL + "?cache=off",
		githubLatestManifestURL + "#fragment",
		"https://raw.githubusercontent.com/Hypostasis-Cat/HypoMux/main/latest.json",
		"https://cnb.cool/Hypostasis-Cat/HypoMux/-/git/raw/main/latest.json",
		"https://cnb.cool/another/repository/-/git/raw/update-channel/latest.json",
		releaseDownloadURL + "v2.5.8/HypoMux_Setup_2.5.8.exe",
	} {
		if validateUpdateMetadataURL(value) == nil {
			t.Fatalf("unsafe metadata URL was accepted: %s", value)
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
	githubManifest, githubSignature := signedManifestJSON(t, testManifest("2.5.9", []byte("new")))
	cnbManifest, cnbSignature := signedManifestJSON(t, testManifest("2.5.8", []byte("old")))
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
	if !result.Available || result.Release.TagName != "v2.5.9" {
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

func TestUpdaterRejectsNonInstallerMetadataConflictForSameVersion(t *testing.T) {
	left := releaseFromTestManifest(t, testManifest("2.5.8", []byte("payload")))
	right := left
	right.Notes = "different signed release notes"
	if _, err := selectLatestRelease([]ReleaseInfo{left, right}); err == nil {
		t.Fatal("conflicting non-installer metadata for one version was accepted")
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

func TestUpdaterUsesGitHubManifestWhenCNBMetadataIsUnavailable(t *testing.T) {
	manifest, signature := signedManifestJSON(t, testManifest("2.5.8", []byte("payload")))
	service := NewUpdaterService()
	service.manifestPublicKey = testManifestPublicKey()
	service.client = clientFor(func(request *http.Request) *http.Response {
		switch request.URL.String() {
		case githubLatestManifestURL:
			return stringResponse(request, http.StatusOK, manifest)
		case githubLatestManifestURL + ".sig":
			return bytesResponse(request, http.StatusOK, signature)
		default:
			return stringResponse(request, http.StatusServiceUnavailable, "")
		}
	})

	result, err := service.Check()
	if err != nil {
		t.Fatal(err)
	}
	if result.Release.TagName != "v2.5.8" {
		t.Fatalf("GitHub fallback result = %#v", result.Release)
	}
}

func TestUpdaterFailsClosedWhenSignedChannelsConflictForSameVersion(t *testing.T) {
	githubManifest, githubSignature := signedManifestJSON(t, testManifest("2.5.8", []byte("github")))
	cnbManifest, cnbSignature := signedManifestJSON(t, testManifest("2.5.8", []byte("cnb")))
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
			return stringResponse(request, http.StatusNotFound, "")
		}
	})

	if result, err := service.Check(); err == nil {
		t.Fatalf("conflicting signed channels were accepted: %#v", result)
	}
}

func TestUpdaterIgnoresTamperedChannelWhenOtherChannelIsTrusted(t *testing.T) {
	manifest, signature := signedManifestJSON(t, testManifest("2.5.8", []byte("payload")))
	service := NewUpdaterService()
	service.manifestPublicKey = testManifestPublicKey()
	service.client = clientFor(func(request *http.Request) *http.Response {
		switch request.URL.String() {
		case cnbLatestManifestURL:
			return stringResponse(request, http.StatusOK, manifest+" ")
		case cnbLatestManifestURL + ".sig":
			return bytesResponse(request, http.StatusOK, signature)
		case githubLatestManifestURL:
			return stringResponse(request, http.StatusOK, manifest)
		case githubLatestManifestURL + ".sig":
			return bytesResponse(request, http.StatusOK, signature)
		default:
			return stringResponse(request, http.StatusNotFound, "")
		}
	})

	result, err := service.Check()
	if err != nil {
		t.Fatal(err)
	}
	if result.Release.TagName != "v2.5.8" {
		t.Fatalf("trusted fallback result = %#v", result.Release)
	}
}

func TestUpdaterFailsWhenBothChannelSignaturesAreInvalid(t *testing.T) {
	manifest, _ := signedManifestJSON(t, testManifest("2.5.8", []byte("payload")))
	invalidSignature := make([]byte, ed25519.SignatureSize)
	service := NewUpdaterService()
	service.manifestPublicKey = testManifestPublicKey()
	service.client = clientFor(func(request *http.Request) *http.Response {
		switch request.URL.String() {
		case cnbLatestManifestURL, githubLatestManifestURL:
			return stringResponse(request, http.StatusOK, manifest)
		case cnbLatestManifestURL + ".sig", githubLatestManifestURL + ".sig":
			return bytesResponse(request, http.StatusOK, invalidSignature)
		default:
			return stringResponse(request, http.StatusNotFound, "")
		}
	})

	if result, err := service.Check(); err == nil {
		t.Fatalf("invalid signed channels were accepted: %#v", result)
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
