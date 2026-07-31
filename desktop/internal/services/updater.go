package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	CurrentVersion   = "2.1.0"
	latestReleaseAPI = "https://api.github.com/repos/Hypostasis-Cat/HypoMux/releases/latest"
)

var (
	installerNamePattern = regexp.MustCompile(`(?i)^HypoMux_Setup_[A-Za-z0-9][A-Za-z0-9._+\-]*\.exe$`)
	versionPattern       = regexp.MustCompile(`(?i)^v?(\d+(?:\.\d+){1,3})(?:[-+].*)?$`)
)

type ReleaseInfo struct {
	TagName         string `json:"tag_name"`
	Name            string `json:"name"`
	Notes           string `json:"notes"`
	PageURL         string `json:"page_url"`
	InstallerURL    string `json:"installer_url"`
	InstallerName   string `json:"installer_name"`
	InstallerSize   int64  `json:"installer_size"`
	InstallerDigest string `json:"installer_digest,omitempty"`
}

type UpdateCheckResult struct {
	CurrentVersion string      `json:"current_version"`
	Available      bool        `json:"available"`
	Release        ReleaseInfo `json:"release"`
}

type UpdaterService struct {
	client   *http.Client
	mu       sync.RWMutex
	progress UpdateProgress
}

func NewUpdaterService() *UpdaterService {
	return &UpdaterService{
		client:   &http.Client{},
		progress: UpdateProgress{State: "idle"},
	}
}

type UpdateProgress struct {
	State      string `json:"state"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Message    string `json:"message,omitempty"`
}

func (s *UpdaterService) Progress() UpdateProgress {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.progress
}

func (s *UpdaterService) setProgress(progress UpdateProgress) {
	s.mu.Lock()
	s.progress = progress
	s.mu.Unlock()
}

func (s *UpdaterService) Check() (UpdateCheckResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPI, nil)
	if err != nil {
		return UpdateCheckResult{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "HypoMux-Updater")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := s.client.Do(request)
	if err != nil {
		return UpdateCheckResult{}, errors.New("无法连接 GitHub，请检查网络后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return UpdateCheckResult{}, fmt.Errorf("GitHub HTTP %d", response.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(&payload); err != nil {
		return UpdateCheckResult{}, errors.New("GitHub 返回了无效的更新信息")
	}
	if versionKey(payload.TagName) == nil {
		return UpdateCheckResult{}, errors.New("GitHub 最新发布的版本号无效")
	}
	if err := validateGitHubURL(payload.HTMLURL); err != nil {
		return UpdateCheckResult{}, errors.New("GitHub 发布信息中的发布页地址无效")
	}
	for _, asset := range payload.Assets {
		if !installerNamePattern.MatchString(asset.Name) {
			continue
		}
		if asset.Size <= 0 {
			return UpdateCheckResult{}, errors.New("GitHub 安装包大小无效")
		}
		if err := validateGitHubURL(asset.URL); err != nil {
			return UpdateCheckResult{}, errors.New("GitHub 发布信息中的安装包地址无效")
		}
		release := ReleaseInfo{
			TagName: payload.TagName, Name: payload.Name, Notes: strings.TrimSpace(payload.Body),
			PageURL: payload.HTMLURL, InstallerURL: asset.URL, InstallerName: asset.Name,
			InstallerSize: asset.Size, InstallerDigest: strings.TrimSpace(asset.Digest),
		}
		return UpdateCheckResult{
			CurrentVersion: CurrentVersion,
			Available:      isNewerVersion(payload.TagName, CurrentVersion),
			Release:        release,
		}, nil
	}
	return UpdateCheckResult{}, errors.New("GitHub 最新发布未找到 HypoMux 安装包")
}

func (s *UpdaterService) Download(release ReleaseInfo) (string, error) {
	s.setProgress(UpdateProgress{State: "starting", Total: release.InstallerSize})
	if !installerNamePattern.MatchString(release.InstallerName) || release.InstallerSize <= 0 {
		s.setProgress(UpdateProgress{State: "failed", Message: "安装包发布信息无效"})
		return "", errors.New("安装包发布信息无效")
	}
	if err := validateGitHubURL(release.InstallerURL); err != nil {
		return "", errors.New("安装包下载地址无效")
	}
	directory, err := os.MkdirTemp("", "HypoMuxUpdate-")
	if err != nil {
		return "", fmt.Errorf("创建更新临时目录失败：%w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(directory)
		}
	}()
	target := filepath.Join(directory, release.InstallerName)
	partial := target + ".part"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, release.InstallerURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "HypoMux-Updater")
	response, err := s.client.Do(request)
	if err != nil {
		return "", errors.New("无法从 GitHub 下载安装包，请检查网络后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub 下载失败（HTTP %d）", response.StatusCode)
	}
	stream, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("创建安装包文件失败：%w", err)
	}
	hash := sha256.New()
	writer := &updateProgressWriter{
		writer: io.MultiWriter(stream, hash),
		onWrite: func(written int64) {
			s.setProgress(UpdateProgress{
				State: "downloading", Downloaded: written, Total: release.InstallerSize,
			})
		},
	}
	written, copyErr := io.Copy(writer, io.LimitReader(response.Body, release.InstallerSize+1))
	closeErr := stream.Close()
	if copyErr != nil {
		_ = os.Remove(partial)
		return "", fmt.Errorf("下载安装包失败：%w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(partial)
		return "", fmt.Errorf("保存安装包失败：%w", closeErr)
	}
	if written != release.InstallerSize {
		_ = os.Remove(partial)
		return "", fmt.Errorf("安装包大小校验失败（预期 %d，实际 %d）", release.InstallerSize, written)
	}
	if expected := strings.TrimPrefix(strings.ToLower(release.InstallerDigest), "sha256:"); expected != "" &&
		expected != strings.ToLower(release.InstallerDigest) {
		if actual := hex.EncodeToString(hash.Sum(nil)); actual != expected {
			_ = os.Remove(partial)
			return "", errors.New("安装包 SHA-256 校验失败")
		}
	}
	if err := os.Rename(partial, target); err != nil {
		_ = os.Remove(partial)
		return "", fmt.Errorf("提交安装包失败：%w", err)
	}
	committed = true
	s.setProgress(UpdateProgress{State: "ready", Downloaded: written, Total: release.InstallerSize})
	return target, nil
}

func (s *UpdaterService) LaunchInstaller(installerPath string) error {
	if err := launchInstallerAfterExit(installerPath, os.Getpid()); err != nil {
		s.setProgress(UpdateProgress{State: "failed", Message: err.Error()})
		return err
	}
	s.setProgress(UpdateProgress{State: "installing"})
	return nil
}

type updateProgressWriter struct {
	writer  io.Writer
	written int64
	onWrite func(int64)
}

func (w *updateProgressWriter) Write(data []byte) (int, error) {
	count, err := w.writer.Write(data)
	w.written += int64(count)
	if w.onWrite != nil {
		w.onWrite(w.written)
	}
	return count, err
}

func validateGitHubURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return errors.New("必须使用 github.com HTTPS 地址")
	}
	return nil
}

func versionKey(value string) []int {
	match := versionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 2 {
		return nil
	}
	parts := strings.Split(match[1], ".")
	result := []int{0, 0, 0, 0}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		result[index] = number
	}
	return result
}

func isNewerVersion(candidate string, current string) bool {
	left, right := versionKey(candidate), versionKey(current)
	if left == nil || right == nil {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return left[index] > right[index]
		}
	}
	return false
}
