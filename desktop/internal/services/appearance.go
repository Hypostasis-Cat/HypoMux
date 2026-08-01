package services

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxBackgroundBytes       = 20 << 20
	maxAppearancePayload     = 28 << 20
	AppearanceBackgroundPath = "/hypomux/appearance/background"
)

type appearanceDocument struct {
	Version          int            `json:"version"`
	Settings         map[string]any `json:"settings"`
	BackgroundFile   string         `json:"background_file,omitempty"`
	BackgroundSHA256 string         `json:"background_sha256,omitempty"`
}

type AppearanceService struct {
	mu   sync.Mutex
	path string
}

func NewAppearanceService() *AppearanceService {
	return &AppearanceService{path: filepath.Join(settingsDirectory(), "appearance.json")}
}

func (s *AppearanceService) Load() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *AppearanceService) Save(payload string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(payload) == 0 || len(payload) > maxAppearancePayload {
		return "", fmt.Errorf("外观设置为空或超过 %d MiB", maxAppearancePayload>>20)
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(payload), &settings); err != nil {
		return "", fmt.Errorf("外观设置格式无效：%w", err)
	}
	if len(settings) == 0 {
		return "", fmt.Errorf("外观设置必须是非空 JSON 对象")
	}
	current, _ := s.readDocumentLocked()
	document := appearanceDocument{Version: 1, Settings: settings}
	backgroundSource, _ := settings["backgroundSource"].(string)
	backgroundURL, _ := settings["localBackgroundUrl"].(string)
	delete(settings, "localBackgroundUrl")

	if backgroundSource == "local" {
		if strings.HasPrefix(backgroundURL, "data:") {
			content, extension, mimeType, err := decodeBackgroundDataURL(backgroundURL)
			if err != nil {
				return "", err
			}
			digest := sha256.Sum256(content)
			document.BackgroundSHA256 = hex.EncodeToString(digest[:])
			document.BackgroundFile = "background" + extension
			backgroundPath := filepath.Join(settingsDirectory(), "appearance", document.BackgroundFile)
			if current.BackgroundSHA256 != document.BackgroundSHA256 || current.BackgroundFile != document.BackgroundFile {
				if err := atomicWriteFile(backgroundPath, content, 0o600); err != nil {
					return "", fmt.Errorf("保存背景图片失败，请检查配置目录权限：%w", err)
				}
			}
			settings["localBackgroundMime"] = mimeType
		} else if current.BackgroundFile != "" {
			document.BackgroundFile = current.BackgroundFile
			document.BackgroundSHA256 = current.BackgroundSHA256
		} else {
			return "", fmt.Errorf("本地背景设置缺少图片数据")
		}
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化外观设置失败：%w", err)
	}
	if err := atomicWriteFile(s.path, data, 0o600); err != nil {
		return "", fmt.Errorf("保存外观设置失败：%w", err)
	}
	if backgroundSource != "local" && current.BackgroundFile != "" {
		_ = os.Remove(filepath.Join(settingsDirectory(), "appearance", filepath.Base(current.BackgroundFile)))
	}
	return s.loadLocked()
}

func (s *AppearanceService) loadLocked() (string, error) {
	document, err := s.readDocumentLocked()
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	settings := cloneAnyMap(document.Settings)
	if document.BackgroundFile != "" {
		path := filepath.Join(settingsDirectory(), "appearance", filepath.Base(document.BackgroundFile))
		content, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			settings["backgroundSource"] = "system"
			delete(settings, "localBackgroundUrl")
			payload, marshalErr := json.Marshal(settings)
			if marshalErr != nil {
				return "", fmt.Errorf("读取外观设置失败：%w", marshalErr)
			}
			return string(payload), nil
		}
		if readErr != nil {
			return "", fmt.Errorf("读取背景图片失败：%w", readErr)
		}
		mimeType, _, valid := detectBackgroundType(content)
		if !valid {
			return "", fmt.Errorf("保存的背景图片格式无效")
		}
		settings["localBackgroundMime"] = mimeType
		settings["localBackgroundUrl"] = AppearanceBackgroundPath + "?v=" + document.BackgroundSHA256
	}
	payload, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("读取外观设置失败：%w", err)
	}
	return string(payload), nil
}

// NewAppearanceBackgroundHandler keeps the persisted image out of Wails
// binding responses without exposing an http.ResponseWriter method as a Wails
// service binding.
func NewAppearanceBackgroundHandler(service *AppearanceService) http.Handler {
	return http.HandlerFunc(service.serveBackground)
}

// Returning a multi-megabyte Data URL from Load or Save can exceed WebView2's
// response buffering and make the frontend treat a valid appearance document
// as a failed load. The WebView fetches the image from this same-origin route
// instead.
func (s *AppearanceService) serveBackground(response http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	document, err := s.readDocumentLocked()
	if err != nil || document.BackgroundFile == "" {
		http.NotFound(response, request)
		return
	}
	if version := request.URL.Query().Get("v"); version != "" && version != document.BackgroundSHA256 {
		http.NotFound(response, request)
		return
	}

	path := filepath.Join(settingsDirectory(), "appearance", filepath.Base(document.BackgroundFile))
	content, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	mimeType, _, valid := detectBackgroundType(content)
	if !valid {
		http.Error(response, "invalid background image", http.StatusUnsupportedMediaType)
		return
	}
	response.Header().Set("Content-Type", mimeType)
	response.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(response, request, filepath.Base(path), time.Time{}, bytes.NewReader(content))
}

func (s *AppearanceService) readDocumentLocked() (appearanceDocument, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return appearanceDocument{}, err
	}
	var document appearanceDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return appearanceDocument{}, fmt.Errorf("外观设置文件损坏：%w", err)
	}
	if document.Version != 1 || document.Settings == nil {
		return appearanceDocument{}, fmt.Errorf("不支持的外观设置版本")
	}
	return document, nil
}

func decodeBackgroundDataURL(value string) ([]byte, string, string, error) {
	header, encoded, ok := strings.Cut(value, ",")
	headerLower := strings.ToLower(strings.TrimSpace(header))
	if !ok || !strings.HasPrefix(headerLower, "data:") || !strings.HasSuffix(headerLower, ";base64") {
		return nil, "", "", fmt.Errorf("背景图片必须使用 base64 Data URL")
	}
	declaredType := strings.TrimPrefix(strings.SplitN(headerLower, ";", 2)[0], "data:")
	content, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", "", fmt.Errorf("背景图片编码无效：%w", err)
	}
	if len(content) == 0 || len(content) > maxBackgroundBytes {
		return nil, "", "", fmt.Errorf("背景图片为空或超过 %d MiB", maxBackgroundBytes>>20)
	}
	mimeType, extension, valid := detectBackgroundType(content)
	if !valid {
		return nil, "", "", fmt.Errorf("背景图片格式不支持")
	}
	if declaredType != mimeType {
		return nil, "", "", fmt.Errorf("背景图片声明格式与实际内容不一致")
	}
	if mimeType == "image/png" || mimeType == "image/jpeg" {
		config, _, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 16384 || config.Height > 16384 {
			return nil, "", "", fmt.Errorf("背景图片内容损坏或尺寸超过 16384×16384")
		}
	}
	return content, extension, mimeType, nil
}

func detectBackgroundType(content []byte) (string, string, bool) {
	switch {
	case len(content) >= 8 && bytes.Equal(content[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", ".png", true
	case len(content) >= 3 && content[0] == 0xff && content[1] == 0xd8 && content[2] == 0xff:
		return "image/jpeg", ".jpg", true
	case len(content) >= 26 && content[0] == 'B' && content[1] == 'M':
		return "image/bmp", ".bmp", true
	case len(content) >= 30 && bytes.Equal(content[:4], []byte("RIFF")) && bytes.Equal(content[8:12], []byte("WEBP")):
		return "image/webp", ".webp", true
	default:
		return "", "", false
	}
}

func cloneAnyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
