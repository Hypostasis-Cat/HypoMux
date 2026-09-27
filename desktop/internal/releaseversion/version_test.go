package releaseversion

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestReleaseVersionOrderingAndWindowsMapping(t *testing.T) {
	versions := []string{"2.6.0", "2.7.0-beta.1", "2.7.0-beta.2", "2.7.0-beta.10", "2.7.0-rc.1", "2.7.0-rc.10", "2.7.0", "2.7.1-beta.1"}
	var previous []int
	for _, input := range versions {
		v, err := Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		if v.String() != input {
			t.Fatalf("round trip: %s", v)
		}
		key := Key("v" + input)
		if previous != nil && slices.Compare(previous, key) >= 0 {
			t.Fatalf("wrong order at %s", input)
		}
		previous = key
		if (v.Channel() == "update-channel-preview") != v.Prerelease() {
			t.Fatalf("channel: %s", input)
		}
	}
	for input, want := range map[string]string{"2.7.0-beta.1": "2.7.0.1", "2.7.0-beta.29999": "2.7.0.29999", "2.7.0-rc.1": "2.7.0.30001", "2.7.0-rc.29999": "2.7.0.59999", "2.7.0": "2.7.0.65535"} {
		v, _ := Parse(input)
		if v.Windows() != want {
			t.Fatalf("%s: %s != %s", input, v.Windows(), want)
		}
	}
	if !slices.Equal(Key("2.6"), Key("v2.6.0.0")) {
		t.Fatal("legacy numeric equivalence lost")
	}
}

func TestRejectMalformedReleaseVersions(t *testing.T) {
	for _, input := range []string{"", "v2.7.0", "2.7", "2.7.0.1", "02.7.0", "2.7.0-bata1", "2.7.0-beta1", "2.7.0-beta.0", "2.7.0-beta.01", "2.7.0-RC.1", "2.7.0-rc.30000", "65536.0.0", "2.7.0+build.1", "2.7.0-rc.1\n", "2.7.0-rc.99999999999999999999"} {
		if _, err := Parse(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestMetadataRoundTripInIsolatedCheckout(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"VERSION", "Taskfile.yml", "build/config.yml", "frontend/package.json", "frontend/src/product.ts", "internal/services/updater.go", "build/windows/nsis/wails_tools.nsh", "build/windows/nsis/version.nsh", "build/windows/info.json", "build/windows/wails.exe.manifest", "build/windows/msix/template.xml", "build/windows/msix/app_manifest.xml"} {
		data, err := os.ReadFile(filepath.Join("../..", path))
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{"2.8.0-beta.2", "2.8.0-rc.10", "2.8.0"} {
		v, _ := Parse(input)
		// Force a mismatch even when CI is building one of these exact tags.
		if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("0.0.0\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := SyncMetadata(root, v, true); err == nil {
			t.Fatal("missed stale metadata")
		}
		if err := SyncMetadata(root, v, false); err != nil {
			t.Fatal(err)
		}
		if err := SyncMetadata(root, v, true); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(filepath.Join(root, "build/windows/info.json"))
		var info struct {
			Fixed map[string]string
			Info  map[string]map[string]string
		}
		if err := json.Unmarshal(data, &info); err != nil {
			t.Fatal(err)
		}
		if info.Fixed["file_version"] != v.Windows() || info.Info["0409"]["ProductVersion"] != input {
			t.Fatalf("wrong Windows metadata: %s", data)
		}
		manifest, _ := os.ReadFile(filepath.Join(root, "build/windows/wails.exe.manifest"))
		if !strings.Contains(string(manifest), `version="6.0.0.0"`) {
			t.Fatal("dependency identity was changed")
		}
	}
}
