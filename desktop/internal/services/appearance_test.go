package services

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
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
	stored, err := service.readDocumentLocked()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(settingsDirectory(), "appearance", stored.BackgroundFile)); err != nil {
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

func TestAppearanceUsesActualBackgroundTypeWhenMIMEIsWrong(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	service := NewAppearanceService()
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	loaded, err := service.Save(`{"backgroundSource":"local","localBackgroundUrl":"data:image/jpeg;base64,` + png + `"}`)
	if err != nil {
		t.Fatalf("valid PNG with a wrong declared MIME was rejected: %v", err)
	}
	if !strings.Contains(loaded, `"localBackgroundMime":"image/png"`) {
		t.Fatalf("background did not use its detected PNG type: %s", loaded)
	}
	stored, err := service.readDocumentLocked()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(stored.BackgroundFile, ".png") {
		t.Fatal("detected extension lost")
	}
	if _, err := os.Stat(filepath.Join(settingsDirectory(), "appearance", stored.BackgroundFile)); err != nil {
		t.Fatalf("background was not persisted with its detected PNG extension: %v", err)
	}
}

func appearanceTestImage(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 1, 1))
	im.SetRGBA(0, 0, c)
	var output bytes.Buffer
	if err := png.Encode(&output, im); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func appearanceTestPayload(content []byte) string {
	return `{"backgroundSource":"local","localBackgroundUrl":"data:image/png;base64,` + base64.StdEncoding.EncodeToString(content) + `"}`
}

func TestAppearanceJSONFailurePreservesOldBackgroundAndRestart(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	service := NewAppearanceService()
	oldImage := appearanceTestImage(t, color.RGBA{B: 255, A: 255})
	newImage := appearanceTestImage(t, color.RGBA{R: 255, A: 255})
	if _, err := service.Save(appearanceTestPayload(oldImage)); err != nil {
		t.Fatal(err)
	}
	oldJSON, err := os.ReadFile(service.path)
	if err != nil {
		t.Fatal(err)
	}
	oldDocument, err := service.readDocumentLocked()
	if err != nil {
		t.Fatal(err)
	}
	service.writeFile = func(path string, content []byte, permission os.FileMode) error {
		if path == service.path {
			return errors.New("injected JSON commit failure")
		}
		return atomicWriteFile(path, content, permission)
	}
	if _, err := service.Save(appearanceTestPayload(newImage)); err == nil {
		t.Fatal("commit failure ignored")
	}
	currentJSON, _ := os.ReadFile(service.path)
	imagePath := filepath.Join(settingsDirectory(), "appearance", oldDocument.BackgroundFile)
	persistedImage, err := os.ReadFile(imagePath)
	if err != nil || !bytes.Equal(oldImage, persistedImage) || !bytes.Equal(oldJSON, currentJSON) {
		t.Fatal("failed save changed old document or image")
	}
	files, err := os.ReadDir(filepath.Dir(imagePath))
	if err != nil || len(files) != 1 {
		t.Fatalf("failed save leaked resources: %v %v", files, err)
	}
	if _, err := NewAppearanceService().Load(); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, AppearanceBackgroundPath+"?v="+oldDocument.BackgroundSHA256, nil)
	response := httptest.NewRecorder()
	NewAppearanceBackgroundHandler(NewAppearanceService()).ServeHTTP(response, request)
	if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), oldImage) {
		t.Fatal("restart did not serve old image")
	}
}

func TestAppearanceSuccessfulReplacementAndLegacyBackground(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	service := NewAppearanceService()
	oldImage := appearanceTestImage(t, color.RGBA{B: 255, A: 255})
	legacy := appearanceDocument{Version: 1, Settings: map[string]any{"backgroundSource": "local"}, BackgroundFile: "background.png", BackgroundSHA256: "legacy-hash"}
	data, _ := json.Marshal(legacy)
	if err := atomicWriteFile(service.path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(settingsDirectory(), "appearance", legacy.BackgroundFile)
	if err := atomicWriteFile(oldPath, oldImage, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(loaded); err != nil {
		t.Fatal("legacy background could not be reused", err)
	}
	if _, err := service.Save(appearanceTestPayload(oldImage)); err != nil {
		t.Fatal(err)
	}
	document, err := service.readDocumentLocked()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(document.BackgroundFile, "background-"+document.BackgroundSHA256) {
		t.Fatal("missing content-addressed filename")
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("legacy image not cleaned after commit")
	}
	firstPath := filepath.Join(settingsDirectory(), "appearance", document.BackgroundFile)
	if _, err := service.Save(appearanceTestPayload(oldImage)); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(filepath.Dir(firstPath))
	if len(files) != 1 {
		t.Fatal("same background created duplicate resources")
	}
	newImage := appearanceTestImage(t, color.RGBA{R: 255, A: 255})
	if _, err := service.Save(appearanceTestPayload(newImage)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatal("previous image not cleaned after replacement")
	}
	if _, err := service.Save(`{"backgroundSource":"builtin"}`); err != nil {
		t.Fatal(err)
	}
	files, _ = os.ReadDir(filepath.Dir(firstPath))
	if len(files) != 0 {
		t.Fatal("unused background not cleaned")
	}
}
