package services

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppearancePersistsBackgroundOutsideJSONAndReloadsIt(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	service := NewAppearanceService()
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	loaded, err := service.Save(`{"backgroundSource":"local","localBackgroundUrl":"` + dataURL + `","mode":"dark"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(loaded, dataURL) {
		t.Fatalf("background was not rehydrated: %s", loaded)
	}
	if _, err := service.Save(loaded); err != nil {
		t.Fatalf("updating an existing appearance document failed: %v", err)
	}
	document, err := os.ReadFile(filepath.Join(settingsDirectory(), "appearance.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(document), "base64") || strings.Contains(string(document), dataURL) {
		t.Fatal("background bytes leaked into appearance.json")
	}
	if _, err := os.Stat(filepath.Join(settingsDirectory(), "appearance", "background.png")); err != nil {
		t.Fatal(err)
	}
}

func TestAppearanceRejectsMismatchedBackgroundType(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	service := NewAppearanceService()
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	if _, err := service.Save(`{"backgroundSource":"local","localBackgroundUrl":"data:image/jpeg;base64,` + png + `"}`); err == nil {
		t.Fatal("expected mismatched content type to fail")
	}
}
