package services

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
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
	// Keep the image above WebView2's ordinary response buffering range. The
	// persisted bytes may be large, but Save and Load must remain small JSON
	// responses that point at the same-origin asset route.
	png = append(png, bytes.Repeat([]byte{0}, 2<<20)...)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	loaded, err := service.Save(`{"backgroundSource":"local","localBackgroundUrl":"` + dataURL + `","mode":"dark"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(loaded, "base64") || !strings.Contains(loaded, AppearanceBackgroundPath+`?v=`) {
		t.Fatalf("background was not represented by its asset URL: %s", loaded)
	}
	if len(loaded) > 4096 {
		t.Fatalf("appearance save response still contains large image data: %d bytes", len(loaded))
	}
	if _, err := service.Save(loaded); err != nil {
		t.Fatalf("updating an existing appearance document failed: %v", err)
	}
	loadedWithoutImage, err := service.Save(`{"backgroundSource":"local","mode":"light"}`)
	if err != nil {
		t.Fatalf("reusing an existing background without resending its bytes failed: %v", err)
	}
	if strings.Contains(loadedWithoutImage, "base64") || !strings.Contains(loadedWithoutImage, AppearanceBackgroundPath+`?v=`) {
		t.Fatalf("reused background was not represented by its asset URL: %s", loadedWithoutImage)
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
	request := httptest.NewRequest(http.MethodGet, AppearanceBackgroundPath, nil)
	response := httptest.NewRecorder()
	NewAppearanceBackgroundHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("background asset returned %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "image/png" || !bytes.Equal(response.Body.Bytes(), png) {
		t.Fatal("background asset did not return the persisted PNG")
	}
	restartedService := NewAppearanceService()
	reloaded, err := restartedService.Load()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reloaded, "base64") || !strings.Contains(reloaded, AppearanceBackgroundPath+`?v=`) {
		t.Fatalf("background URL did not survive a service restart: %s", reloaded)
	}
	if len(reloaded) > 4096 {
		t.Fatalf("appearance load response still contains large image data: %d bytes", len(reloaded))
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
