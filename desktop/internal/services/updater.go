package services

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
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
	CurrentVersion           = "2.5.7"
	githubRepositoryURL      = "https://github.com/Hypostasis-Cat/HypoMux"
	releaseDownloadURL       = githubRepositoryURL + "/releases/download/"
	githubLatestManifestURL  = "https://raw.githubusercontent.com/Hypostasis-Cat/HypoMux/update-channel/latest.json"
	cnbRepositoryURL         = "https://cnb.cool/Hypostasis-Cat/HypoMux"
	cnbDownloadURL           = cnbRepositoryURL + "/-/releases/download/"
	cnbLatestManifestURL     = cnbRepositoryURL + "/-/git/raw/update-channel/latest.json"
	updateManifestName       = "latest.json"
	maxUpdateManifestSize    = 2 << 20
	maxManifestSignatureSize = 1024
	maxInstallerMirrorCount  = 4
	updateMetadataTimeout    = 10 * time.Second
	installerDownloadTimeout = 15 * time.Minute
)

var (
	installerNamePattern = regexp.MustCompile(`(?i)^HypoMux_Setup_[A-Za-z0-9][A-Za-z0-9._+\-]*\.exe$`)
	versionPattern       = regexp.MustCompile(`(?i)^v?(\d+(?:\.\d+){1,3})$`)
	sha256Pattern        = regexp.MustCompile(`(?i)^[a-f0-9]{64}$`)

	//go:embed update_manifest_ed25519_public_key.txt
	updateManifestPublicKey string
)

type ReleaseInfo struct {
	TagName         string   `json:"tag_name"`
	Name            string   `json:"name"`
	Notes           string   `json:"notes"`
	PageURL         string   `json:"page_url"`
	InstallerURLs   []string `json:"installer_urls"`
	InstallerName   string   `json:"installer_name"`
	InstallerSize   int64    `json:"installer_size"`
	InstallerDigest string   `json:"installer_digest"`
}

type updateManifest struct {
	SchemaVersion int    `json:"schema_version"`
	Version       string `json:"version"`
	Name          string `json:"name"`
	Notes         string `json:"notes"`
	Installer     struct {
		Name   string   `json:"name"`
		Size   int64    `json:"size"`
		SHA256 string   `json:"sha256"`
		URLs   []string `json:"urls"`
	} `json:"installer"`
}

type UpdateCheckResult struct {
	CurrentVersion string      `json:"current_version"`
	Available      bool        `json:"available"`
	Release        ReleaseInfo `json:"release"`
}

type UpdaterService struct {
	client            *http.Client
	manifestPublicKey ed25519.PublicKey
	launchInstaller   func(string, int) error
	verifyInstaller   func(string) error
	quit              func()
	mu                sync.RWMutex
	progress          UpdateProgress
}

func NewUpdaterService(quit ...func()) *UpdaterService {
	service := &UpdaterService{
		client:            &http.Client{},
		manifestPublicKey: mustUpdateManifestPublicKey(),
		launchInstaller:   launchInstallerAfterExit,
		verifyInstaller:   verifyDownloadedInstallerAuthenticity,
		progress:          UpdateProgress{State: "idle"},
	}
	if len(quit) > 0 {
		service.quit = quit[0]
	}
	return service
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
	ctx, cancel := context.WithTimeout(context.Background(), updateMetadataTimeout)
	defer cancel()

	type metadataSource struct {
		name  string
		check func(context.Context) (ReleaseInfo, error)
	}
	sources := []metadataSource{
		{name: "CNB manifest", check: func(ctx context.Context) (ReleaseInfo, error) {
			return s.checkUpdateManifest(ctx, cnbLatestManifestURL)
		}},
		{name: "GitHub manifest", check: func(ctx context.Context) (ReleaseInfo, error) {
			return s.checkUpdateManifest(ctx, githubLatestManifestURL)
		}},
	}
	type sourceResult struct {
		index   int
		release ReleaseInfo
		err     error
	}
	results := make(chan sourceResult, len(sources))
	for index, source := range sources {
		go func(index int, source metadataSource) {
			release, err := source.check(ctx)
			results <- sourceResult{index: index, release: release, err: err}
		}(index, source)
	}

	ordered := make([]sourceResult, len(sources))
	for range sources {
		result := <-results
		ordered[result.index] = result
	}
	candidates := make([]ReleaseInfo, 0, len(sources))
	errorsBySource := make([]string, 0, len(sources))
	for index, result := range ordered {
		if result.err != nil {
			errorsBySource = append(errorsBySource, sources[index].name+": "+result.err.Error())
			continue
		}
		candidates = append(candidates, result.release)
	}
	if len(candidates) == 0 {
		return UpdateCheckResult{}, errors.New(strings.Join(errorsBySource, "；"))
	}

	release, err := selectLatestRelease(candidates)
	if err != nil {
		return UpdateCheckResult{}, err
	}
	return UpdateCheckResult{
		CurrentVersion: CurrentVersion,
		Available:      isNewerVersion(release.TagName, CurrentVersion),
		Release:        release,
	}, nil
}

func (s *UpdaterService) checkUpdateManifest(ctx context.Context, manifestURL string) (ReleaseInfo, error) {
	if err := validateUpdateMetadataURL(manifestURL); err != nil {
		return ReleaseInfo{}, errors.New("更新 manifest 地址无效")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "HypoMux-Updater")
	response, err := s.client.Do(request)
	if err != nil {
		return ReleaseInfo{}, errors.New("无法读取更新 manifest")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("更新 manifest HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxUpdateManifestSize+1))
	if err != nil {
		return ReleaseInfo{}, errors.New("读取更新 manifest 失败")
	}
	if len(body) > maxUpdateManifestSize {
		return ReleaseInfo{}, errors.New("更新 manifest 超过大小限制")
	}
	signatureURL := manifestURL + ".sig"
	if err := validateUpdateMetadataURL(signatureURL); err != nil {
		return ReleaseInfo{}, errors.New("更新 manifest 签名地址无效")
	}
	signatureRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, signatureURL, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	signatureRequest.Header.Set("Accept", "application/octet-stream")
	signatureRequest.Header.Set("User-Agent", "HypoMux-Updater")
	signatureResponse, err := s.client.Do(signatureRequest)
	if err != nil {
		return ReleaseInfo{}, errors.New("无法读取更新 manifest 签名")
	}
	defer signatureResponse.Body.Close()
	if signatureResponse.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("更新 manifest 签名 HTTP %d", signatureResponse.StatusCode)
	}
	signature, err := io.ReadAll(io.LimitReader(signatureResponse.Body, maxManifestSignatureSize+1))
	if err != nil {
		return ReleaseInfo{}, errors.New("读取更新 manifest 签名失败")
	}
	if len(signature) > maxManifestSignatureSize {
		return ReleaseInfo{}, errors.New("更新 manifest 签名超过大小限制")
	}
	if len(s.manifestPublicKey) != ed25519.PublicKeySize ||
		len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(s.manifestPublicKey, body, signature) {
		return ReleaseInfo{}, errors.New("更新 manifest 的 Ed25519 签名无效")
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var manifest updateManifest
	if err := decoder.Decode(&manifest); err != nil {
		return ReleaseInfo{}, errors.New("更新 manifest JSON 无效")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return ReleaseInfo{}, errors.New("更新 manifest 包含多余内容")
	}
	return releaseFromManifest(manifest)
}

func mustUpdateManifestPublicKey() ed25519.PublicKey {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(updateManifestPublicKey))
	if err != nil || len(key) != ed25519.PublicKeySize {
		panic("invalid embedded update manifest public key")
	}
	return ed25519.PublicKey(key)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func releaseFromManifest(manifest updateManifest) (ReleaseInfo, error) {
	if manifest.SchemaVersion != 1 {
		return ReleaseInfo{}, errors.New("manifest schema 版本不受支持")
	}
	tagName := normalizeTagName(manifest.Version)
	if versionKey(tagName) == nil {
		return ReleaseInfo{}, errors.New("manifest 版本号无效")
	}
	version := strings.TrimPrefix(tagName, "v")
	expectedName := "HypoMux_Setup_" + version + ".exe"
	if manifest.Installer.Name != expectedName || !installerNamePattern.MatchString(manifest.Installer.Name) {
		return ReleaseInfo{}, errors.New("manifest 安装包名称与版本不一致")
	}
	if manifest.Installer.Size <= 0 {
		return ReleaseInfo{}, errors.New("manifest 安装包大小无效")
	}
	digest, err := normalizeSHA256(manifest.Installer.SHA256)
	if err != nil {
		return ReleaseInfo{}, errors.New("manifest 缺少有效的 64 位 SHA-256")
	}
	if len(manifest.Installer.URLs) == 0 || len(manifest.Installer.URLs) > maxInstallerMirrorCount {
		return ReleaseInfo{}, errors.New("manifest 下载镜像数量无效")
	}
	urls := make([]string, 0, len(manifest.Installer.URLs))
	seen := make(map[string]struct{}, len(manifest.Installer.URLs))
	for _, mirrorURL := range manifest.Installer.URLs {
		if err := validateInstallerMirrorURL(mirrorURL, tagName, manifest.Installer.Name); err != nil {
			return ReleaseInfo{}, fmt.Errorf("manifest 包含无效镜像：%w", err)
		}
		if _, exists := seen[mirrorURL]; exists {
			continue
		}
		seen[mirrorURL] = struct{}{}
		urls = append(urls, mirrorURL)
	}
	urls = preferCNBMirror(urls)
	pageURL := githubRepositoryURL + "/releases/tag/" + tagName
	return ReleaseInfo{
		TagName: tagName, Name: strings.TrimSpace(manifest.Name), Notes: strings.TrimSpace(manifest.Notes),
		PageURL: pageURL, InstallerURLs: urls, InstallerName: manifest.Installer.Name,
		InstallerSize:   manifest.Installer.Size,
		InstallerDigest: "sha256:" + digest,
	}, nil
}

func selectLatestRelease(candidates []ReleaseInfo) (ReleaseInfo, error) {
	if len(candidates) == 0 {
		return ReleaseInfo{}, errors.New("没有可用的更新元数据")
	}
	latest := candidates[0]
	for _, candidate := range candidates[1:] {
		if isNewerVersion(candidate.TagName, latest.TagName) {
			latest = candidate
		}
	}
	merged := latest
	for _, candidate := range candidates {
		if !sameVersion(candidate.TagName, latest.TagName) {
			continue
		}
		if !sameReleaseMetadata(candidate, latest) {
			return ReleaseInfo{}, errors.New("官方更新源对同一版本返回了不一致的元数据")
		}
	}
	if err := validateReleaseInfo(merged); err != nil {
		return ReleaseInfo{}, err
	}
	return merged, nil
}

func sameReleaseMetadata(left ReleaseInfo, right ReleaseInfo) bool {
	if left.TagName != right.TagName || left.Name != right.Name || left.Notes != right.Notes ||
		left.PageURL != right.PageURL || left.InstallerName != right.InstallerName ||
		left.InstallerSize != right.InstallerSize || left.InstallerDigest != right.InstallerDigest ||
		len(left.InstallerURLs) != len(right.InstallerURLs) {
		return false
	}
	for index := range left.InstallerURLs {
		if left.InstallerURLs[index] != right.InstallerURLs[index] {
			return false
		}
	}
	return true
}

func (s *UpdaterService) Download(release ReleaseInfo) (string, error) {
	s.setProgress(UpdateProgress{State: "starting", Total: release.InstallerSize})
	if err := validateReleaseInfo(release); err != nil {
		return "", s.failDownload(err)
	}
	expectedDigest, _ := normalizeSHA256(release.InstallerDigest)
	directory, err := os.MkdirTemp("", "HypoMuxUpdate-")
	if err != nil {
		return "", s.failDownload(fmt.Errorf("创建更新临时目录失败：%w", err))
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(directory)
		}
	}()
	target := filepath.Join(directory, release.InstallerName)
	partial := target + ".part"
	attemptFailures := make([]string, 0, len(release.InstallerURLs))
	integrityFailures := make([]string, 0, len(release.InstallerURLs))
	for _, mirrorURL := range release.InstallerURLs {
		_ = os.Remove(partial)
		ctx, cancel := context.WithTimeout(context.Background(), installerDownloadTimeout)
		written, integrityFailure, attemptErr := s.downloadInstallerMirror(
			ctx, mirrorURL, partial, release.InstallerSize, expectedDigest,
		)
		cancel()
		if attemptErr != nil {
			label := updateMirrorLabel(mirrorURL)
			attemptFailures = append(attemptFailures, label+": "+attemptErr.Error())
			if integrityFailure {
				integrityFailures = append(integrityFailures, label+": "+attemptErr.Error())
			}
			continue
		}
		if len(integrityFailures) > 0 {
			_ = os.Remove(partial)
			return "", s.failDownload(fmt.Errorf(
				"检测到官方更新镜像内容或签名不一致，已拒绝安装：%s",
				strings.Join(integrityFailures, "；"),
			))
		}
		if err := os.Rename(partial, target); err != nil {
			_ = os.Remove(partial)
			return "", s.failDownload(fmt.Errorf("提交安装包失败：%w", err))
		}
		committed = true
		s.setProgress(UpdateProgress{State: "ready", Downloaded: written, Total: release.InstallerSize})
		return target, nil
	}
	message := "所有官方更新镜像均下载失败"
	if len(integrityFailures) > 0 {
		message = "安装包完整性或 Authenticode 验证失败"
	}
	if len(attemptFailures) > 0 {
		message += "：" + strings.Join(attemptFailures, "；")
	}
	return "", s.failDownload(errors.New(message))
}

func (s *UpdaterService) downloadInstallerMirror(
	ctx context.Context,
	mirrorURL string,
	partial string,
	expectedSize int64,
	expectedDigest string,
) (int64, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, mirrorURL, nil)
	if err != nil {
		return 0, false, err
	}
	request.Header.Set("User-Agent", "HypoMux-Updater")
	response, err := s.client.Do(request)
	if err != nil {
		return 0, false, errors.New("连接或下载失败")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, false, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if response.ContentLength > 0 && response.ContentLength != expectedSize {
		return 0, false, fmt.Errorf(
			"Content-Length 不一致（预期 %d，实际 %d）",
			expectedSize, response.ContentLength,
		)
	}
	stream, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, false, fmt.Errorf("创建安装包文件失败：%w", err)
	}
	hash := sha256.New()
	writer := &updateProgressWriter{
		writer: io.MultiWriter(stream, hash),
		onWrite: func(written int64) {
			s.setProgress(UpdateProgress{
				State: "downloading", Downloaded: written, Total: expectedSize,
			})
		},
	}
	written, copyErr := io.Copy(writer, io.LimitReader(response.Body, expectedSize+1))
	closeErr := stream.Close()
	if copyErr != nil {
		_ = os.Remove(partial)
		return written, false, fmt.Errorf("下载中断：%w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(partial)
		return written, false, fmt.Errorf("保存安装包失败：%w", closeErr)
	}
	if written != expectedSize {
		_ = os.Remove(partial)
		return written, false, fmt.Errorf("大小校验失败（预期 %d，实际 %d）", expectedSize, written)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != expectedDigest {
		_ = os.Remove(partial)
		return written, true, errors.New("SHA-256 校验失败")
	}
	if err := s.verifyInstaller(partial); err != nil {
		_ = os.Remove(partial)
		return written, true, fmt.Errorf("Authenticode 验证失败：%w", err)
	}
	return written, false, nil
}

func (s *UpdaterService) failDownload(err error) error {
	s.setProgress(UpdateProgress{State: "failed", Message: err.Error()})
	return err
}

// InstallAndQuit launches the verified update helper and then leaves the
// application through its normal lifecycle path. The lifecycle callback is
// responsible for stopping the engine and restoring system networking before
// the process exits. Keeping both actions behind one backend call prevents the
// web layer from bypassing cleanup with a raw application quit.
func (s *UpdaterService) InstallAndQuit(installerPath string) error {
	if s.quit == nil {
		return errors.New("应用退出清理尚未初始化")
	}
	if err := s.launchInstaller(installerPath, os.Getpid()); err != nil {
		s.setProgress(UpdateProgress{State: "failed", Message: err.Error()})
		return err
	}
	s.setProgress(UpdateProgress{State: "installing"})
	s.quit()
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

func validateUpdateMetadataURL(value string) error {
	if value == githubLatestManifestURL || value == githubLatestManifestURL+".sig" ||
		value == cnbLatestManifestURL || value == cnbLatestManifestURL+".sig" {
		return nil
	}
	return errors.New("必须使用 HypoMux 官方 signed update channel 地址")
}

func validateInstallerMirrorURL(value string, tagName string, installerName string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Opaque != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" ||
		parsed.ForceQuery || parsed.Path == "" {
		return errors.New("安装包必须使用官方 HTTPS Release 地址")
	}
	canonicalTag := normalizeTagName(tagName)
	if versionKey(canonicalTag) == nil || installerName == "" {
		return errors.New("安装包版本或名称无效")
	}
	githubURL := releaseDownloadURL + canonicalTag + "/" + installerName
	cnbURL := cnbDownloadURL + canonicalTag + "/" + installerName
	if value != githubURL && value != cnbURL {
		return errors.New("安装包地址必须与官方版本和文件名完全匹配")
	}
	return nil
}

func validateReleaseInfo(release ReleaseInfo) error {
	tagName := normalizeTagName(release.TagName)
	if versionKey(tagName) == nil || release.TagName != tagName {
		return errors.New("更新版本号无效")
	}
	version := strings.TrimPrefix(tagName, "v")
	expectedName := "HypoMux_Setup_" + version + ".exe"
	if release.InstallerName != expectedName || !installerNamePattern.MatchString(release.InstallerName) {
		return errors.New("安装包名称与更新版本不一致")
	}
	if release.InstallerSize <= 0 {
		return errors.New("安装包大小无效")
	}
	if _, err := normalizeSHA256(release.InstallerDigest); err != nil {
		return errors.New("更新信息缺少有效的安装包 SHA-256")
	}
	if len(release.InstallerURLs) == 0 || len(release.InstallerURLs) > maxInstallerMirrorCount {
		return errors.New("安装包镜像数量无效")
	}
	seen := make(map[string]struct{}, len(release.InstallerURLs))
	for _, mirrorURL := range release.InstallerURLs {
		if err := validateInstallerMirrorURL(mirrorURL, tagName, release.InstallerName); err != nil {
			return fmt.Errorf("安装包镜像无效：%w", err)
		}
		if _, exists := seen[mirrorURL]; exists {
			return errors.New("安装包镜像重复")
		}
		seen[mirrorURL] = struct{}{}
	}
	if release.PageURL != "" {
		expectedPageURL := githubRepositoryURL + "/releases/tag/" + tagName
		if release.PageURL != expectedPageURL {
			return errors.New("发布页地址与更新版本不一致")
		}
	}
	return nil
}

func normalizeSHA256(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) >= len("sha256:") && strings.EqualFold(value[:len("sha256:")], "sha256:") {
		value = value[len("sha256:"):]
	}
	if !sha256Pattern.MatchString(value) {
		return "", errors.New("invalid SHA-256")
	}
	return strings.ToLower(value), nil
}

func normalizeTagName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if value[0] == 'v' || value[0] == 'V' {
		return "v" + value[1:]
	}
	return "v" + value
}

func sameVersion(left string, right string) bool {
	leftKey, rightKey := versionKey(left), versionKey(right)
	if leftKey == nil || rightKey == nil {
		return false
	}
	for index := range leftKey {
		if leftKey[index] != rightKey[index] {
			return false
		}
	}
	return true
}

func officialInstallerMirrors(tagName string, installerName string) []string {
	tagName = normalizeTagName(tagName)
	return []string{
		cnbDownloadURL + tagName + "/" + installerName,
		releaseDownloadURL + tagName + "/" + installerName,
	}
}

func preferCNBMirror(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.HasPrefix(value, cnbDownloadURL) {
			result = append(result, value)
		}
	}
	for _, value := range values {
		if !strings.HasPrefix(value, cnbDownloadURL) {
			result = append(result, value)
		}
	}
	return result
}

func updateMirrorLabel(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return "未知镜像"
	}
	return parsed.Host
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
