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
	loadedWithoutImage, err := service.Save(`{"backgroundSource":"local","mode":"light"}`)
	if err != nil {
		t.Fatalf("reusing an existing background without resending its bytes failed: %v", err)
	}
	if !strings.Contains(loadedWithoutImage, dataURL) {
		t.Fatalf("reused background was not rehydrated: %s", loadedWithoutImage)
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
	restartedService := NewAppearanceService()
	reloaded, err := restartedService.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reloaded, dataURL) {
		t.Fatalf("background did not survive a service restart: %s", reloaded)
	}
}

func TestAppearanceRequiresBytesForFirstLocalBackgroundSave(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	service := NewAppearanceService()
	_, err := service.Save(`{"backgroundSource":"local","mode":"dark"}`)
	if err == nil || !strings.Contains(err.Error(), "本地背景设置缺少图片数据") {
		t.Fatalf("expected missing first-upload data error, got %v", err)
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
