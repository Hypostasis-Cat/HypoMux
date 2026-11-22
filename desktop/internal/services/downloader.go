package services

// 多线程分段下载模块（MultiDownloader）
// 将一个大文件按字节切成 N 段，N 条 HTTP Range 连接并发下载，
// 与 HypoMux 聚合引擎配合时，各连接会被分配到不同网卡实现多链路叠加。
// 支持断点续传（分段进度落盘），服务器不支持 Range 时自动退回单连接。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	downloadMaxSegments    = 64
	downloadDefaultSegment = 16
	downloadChunk          = 256 * 1024
	downloadMaxRetries     = 8
	downloadPartSuffix     = ".hypopart"
	downloadMetaSuffix     = ".hypometa.json"
)

// DownloadProbe 下载前的探测结果。
type DownloadProbe struct {
	URL          string `json:"url"`
	FinalURL     string `json:"final_url"`
	FileName     string `json:"file_name"`
	Size         int64  `json:"size"`
	AcceptRanges bool   `json:"accept_ranges"`
}

// DownloadSegment 单个分段的进度。
type DownloadSegment struct {
	Index      int   `json:"index"`
	Start      int64 `json:"start"`
	End        int64 `json:"end"` // 闭区间
	Downloaded int64 `json:"downloaded"`
}

// DownloadTask 一个下载任务的对外快照。
type DownloadTask struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Output    string `json:"output"`
	Total     int64  `json:"total"`
	Done      int64  `json:"done"`
	Segments  int    `json:"segments"`
	Status    string `json:"status"` // running | paused | completed | error | cancelled
	Error     string `json:"error,omitempty"`
	Speed     float64 `json:"speed"` // bytes/s
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type downloadSegmentState struct {
	Start      int64 `json:"start"`
	End        int64 `json:"end"`
	Downloaded int64 `json:"downloaded"`
}

type downloaderTask struct {
	id      string
	url     string
	output  string
	total   int64
	segments []downloadSegmentState
	mu      sync.Mutex
	status  string
	errText string
	done    int64
	speed   float64
	started time.Time
	updated time.Time

	pauseFlag bool
	cancelFn  context.CancelFunc
	lastDone  int64
	lastTick  time.Time
}

// DownloaderService 多线程分段下载服务。
type DownloaderService struct {
	mu    sync.Mutex
	tasks map[string]*downloaderTask
	next  int
}

func NewDownloaderService() *DownloaderService {
	return &DownloaderService{tasks: map[string]*downloaderTask{}}
}

// ---------- HTTP 客户端（跟随 HypoMux 接管的 Windows 系统代理） ----------

func downloaderHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 0, // 下载不设整体超时，单次读由外层控制
		Transport: &http.Transport{
			Proxy:               downloadWindowsProxy,
			MaxIdleConns:        128,
			MaxConnsPerHost:     0,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 15 * time.Second,
		},
	}
}

func downloadWindowsProxy(req *http.Request) (*url.URL, error) {
	// 与 Go 默认行为一致（环境变量优先），否则读取 Windows 注册表系统代理，
	// 使下载流量经过 HypoMux 的本地聚合代理。
	// 本地/环回地址永远直连，避免被本机代理服务截胡后挂死。
	host := req.URL.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil, nil
	}
	if proxy, err := http.ProxyFromEnvironment(req); proxy != nil || err != nil {
		return proxy, err
	}
	return downloadRegistryProxy()
}

// ---------- 探测 ----------

func (s *DownloaderService) Probe(rawURL string) (DownloadProbe, error) {
	probe, err := probeDownload(rawURL)
	if err != nil {
		return DownloadProbe{}, err
	}
	return probe, nil
}

func probeDownload(rawURL string) (DownloadProbe, error) {
	client := downloaderHTTPClient()
	result := DownloadProbe{URL: rawURL}
	// 先 HEAD
	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return result, fmt.Errorf("无效的下载地址：%w", err)
	}
	req.Header.Set("User-Agent", "HypoMux-MultiDownloader/1.0")
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		result.FinalURL = resp.Request.URL.String()
		result.Size, _ = strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
		result.AcceptRanges = strings.Contains(strings.ToLower(resp.Header.Get("Accept-Ranges")), "bytes")
	} else {
		// HEAD 不被支持时退回 GET，读完头部即断开
		req2, _ := http.NewRequest(http.MethodGet, rawURL, nil)
		req2.Header.Set("User-Agent", "HypoMux-MultiDownloader/1.0")
		resp2, err2 := client.Do(req2)
		if err2 != nil {
			return result, fmt.Errorf("连接下载服务器失败：%w", err2)
		}
		defer resp2.Body.Close()
		result.FinalURL = resp2.Request.URL.String()
		result.Size, _ = strconv.ParseInt(resp2.Header.Get("Content-Length"), 10, 64)
		result.AcceptRanges = strings.Contains(strings.ToLower(resp2.Header.Get("Accept-Ranges")), "bytes")
		resp2.Body.Close()
	}
	result.FileName = downloadFileName(result.FinalURL)
	return result, nil
}

func downloadFileName(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "download.bin"
	}
	name := filepath.Base(parsed.Path)
	name = strings.TrimRight(name, "/")
	if name == "" || name == "." || name == "/" {
		return "download.bin"
	}
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	return name
}

// ---------- 任务管理 ----------

func (s *DownloaderService) Start(rawURL string, output string, segments int) (DownloadTask, error) {
	if strings.TrimSpace(rawURL) == "" {
		return DownloadTask{}, errors.New("下载地址不能为空")
	}
	if segments < 1 {
		segments = downloadDefaultSegment
	}
	if segments > downloadMaxSegments {
		segments = downloadMaxSegments
	}
	probe, err := probeDownload(rawURL)
	if err != nil {
		return DownloadTask{}, err
	}
	if strings.TrimSpace(output) == "" {
		output = probe.FileName
	}
	if !filepath.IsAbs(output) {
		home, e := os.UserHomeDir()
		if e != nil {
			return DownloadTask{}, e
		}
		output = filepath.Join(home, "Downloads", output)
	}
	if probe.Size <= 0 {
		return DownloadTask{}, errors.New("服务器未返回文件大小，无法分段下载")
	}
	if !probe.AcceptRanges {
		// 不支持 Range 时退回单连接（仍可断点续传整个文件）
		segments = 1
	}
	// 文件比分段数还小的时候按文件大小收敛分段数，避免出现空区间
	if int64(segments) > probe.Size {
		segments = int(probe.Size)
	}

	s.mu.Lock()
	// 复用同一输出文件的未完成任务 → 续传
	for _, t := range s.tasks {
		if t.output == output && (t.status == "running" || t.status == "paused" || t.status == "error") && t.url == probe.FinalURL {
			task := t
			s.mu.Unlock()
			if t.status == "error" || t.status == "paused" {
				s.resumeLocked(task)
			}
			return task.snapshot(), nil
		}
	}
	id := s.newTaskID()
	step := probe.Size / int64(segments)
	segs := make([]downloadSegmentState, segments)
	for i := range segs {
		segs[i].Start = int64(i) * step
		segs[i].End = probe.Size - 1
		if i < segments-1 {
			segs[i].End = segs[i].Start + step - 1
		}
	}
	task := &downloaderTask{
		id: id, url: probe.FinalURL, output: output,
		total: probe.Size, segments: segs,
		status: "running", started: time.Now(), updated: time.Now(),
		lastTick: time.Now(),
	}
	s.tasks[id] = task
	s.mu.Unlock()
	task.launch()
	return task.snapshot(), nil
}

func (s *DownloaderService) newTaskID() string {
	s.next++
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("dl-%d-%s", s.next, hex.EncodeToString(buf))
}

func (s *DownloaderService) List() []DownloadTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]DownloadTask, 0, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, t.snapshot())
	}
	// 新任务在前
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].StartedAt.After(list[j-1].StartedAt); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	return list
}

func (s *DownloaderService) Status(id string) (DownloadTask, error) {
	s.mu.Lock()
	t := s.tasks[id]
	s.mu.Unlock()
	if t == nil {
		return DownloadTask{}, fmt.Errorf("下载任务不存在：%s", id)
	}
	return t.snapshot(), nil
}

func (s *DownloaderService) Pause(id string) (DownloadTask, error) {
	s.mu.Lock()
	t := s.tasks[id]
	s.mu.Unlock()
	if t == nil {
		return DownloadTask{}, fmt.Errorf("下载任务不存在：%s", id)
	}
	t.mu.Lock()
	if t.status == "running" {
		t.pauseFlag = true
		t.status = "paused"
		t.updated = time.Now()
		t.saveMeta()
	}
	snapshot := t.snapshotLocked()
	t.mu.Unlock()
	return snapshot, nil
}

func (s *DownloaderService) Resume(id string) (DownloadTask, error) {
	s.mu.Lock()
	t := s.tasks[id]
	s.mu.Unlock()
	if t == nil {
		return DownloadTask{}, fmt.Errorf("下载任务不存在：%s", id)
	}
	s.resumeLocked(t)
	return t.snapshot(), nil
}

func (s *DownloaderService) Remove(id string) error {
	s.mu.Lock()
	t := s.tasks[id]
	if t == nil {
		s.mu.Unlock()
		return fmt.Errorf("下载任务不存在：%s", id)
	}
	s.mu.Unlock()
	t.mu.Lock()
	wasRunning := t.status == "running"
	t.status = "cancelled"
	t.updated = time.Now()
	t.mu.Unlock()
	if wasRunning {
		t.cancelFn()
	}
	s.mu.Lock()
	delete(s.tasks, id)
	s.mu.Unlock()
	// 删除残留分段文件
	t.mu.Lock()
	output := t.output
	segments := len(t.segments)
	t.mu.Unlock()
	for i := 0; i < segments; i++ {
		_ = os.Remove(output + downloadPartSuffix + strconv.Itoa(i))
	}
	_ = os.Remove(output + downloadMetaSuffix)
	return nil
}

func (s *DownloaderService) resumeLocked(t *downloaderTask) {
	t.mu.Lock()
	if t.status == "running" {
		t.mu.Unlock()
		return
	}
	t.status = "running"
	t.errText = ""
	t.updated = time.Now()
	t.mu.Unlock()
	t.launch()
}

// ---------- 执行 ----------

func (t *downloaderTask) launch() {
	ctx, cancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.cancelFn = cancel
	t.pauseFlag = false
	segments := len(t.segments)
	output := t.output
	t.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(segments)
	for i := 0; i < segments; i++ {
		go func(index int) {
			defer wg.Done()
			t.worker(ctx, index, output)
		}(i)
	}
	go func() {
		wg.Wait()
		cancel() // 全部 worker 退出后释放 context
	}()
	go t.progressSampler()
}

func (t *downloaderTask) worker(ctx context.Context, index int, output string) {
	partPath := output + downloadPartSuffix + strconv.Itoa(index)
	client := downloaderHTTPClient()
	retries := 0
	for retries < downloadMaxRetries {
		select {
		case <-ctx.Done():
			return
		default:
		}
		t.mu.Lock()
		paused := t.pauseFlag
		seg := t.segments[index]
		t.mu.Unlock()
		if paused {
			return
		}
		pos := seg.Start + seg.Downloaded
		if pos > seg.End {
			return // 本段完成
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
		if err != nil {
			t.fail(index, err)
			return
		}
		req.Header.Set("User-Agent", "HypoMux-MultiDownloader/1.0")
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", pos, seg.End))
		resp, err := client.Do(req)
		if err != nil {
			retries++
			if !t.waitRetry(ctx, retries) {
				return
			}
			continue
		}
		if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.fail(index, fmt.Errorf("服务器返回 %s（分段 %d）", resp.Status, index))
			return
		}
		file, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			resp.Body.Close()
			t.fail(index, fmt.Errorf("写入分段文件失败：%w", err))
			return
		}
		if pos > seg.Start {
			_, _ = file.Seek(pos-seg.Start, io.SeekStart)
		} else {
			_, _ = file.Seek(0, io.SeekStart)
		}
		buf := make([]byte, downloadChunk)
		for {
			select {
			case <-ctx.Done():
				file.Close()
				resp.Body.Close()
				return
			default:
			}
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				t.mu.Lock()
				if t.pauseFlag {
					t.mu.Unlock()
					file.Close()
					resp.Body.Close()
					return
				}
				t.segments[index].Downloaded += int64(n)
				t.done += int64(n)
				t.mu.Unlock()
				if _, writeErr := file.Write(buf[:n]); writeErr != nil {
					file.Close()
					resp.Body.Close()
					t.fail(index, fmt.Errorf("写入分段文件失败：%w", writeErr))
					return
				}
			}
			if readErr == io.EOF {
				file.Close()
				resp.Body.Close()
				t.finishSegment(index)
				return
			}
			if readErr != nil {
				file.Close()
				resp.Body.Close()
				retries++
				if !t.waitRetry(ctx, retries) {
					return
				}
				break
			}
		}
	}
	t.fail(index, errors.New("分段重试次数用尽"))
}

func (t *downloaderTask) finishSegment(index int) {
	t.mu.Lock()
	seg := t.segments[index]
	if seg.Start+seg.Downloaded-1 < seg.End {
		// 段落尚不完整（重试恢复路径），直接返回
		t.mu.Unlock()
		return
	}
	// 检查是否全部完成
	allDone := true
	for _, s := range t.segments {
		if s.Start+s.Downloaded-1 < s.End {
			allDone = false
			break
		}
	}
	if !allDone {
		t.updated = time.Now()
		t.mu.Unlock()
		return
	}
	if t.status != "running" {
		// 已被暂停/取消/其他 goroutine 合并，不动
		t.mu.Unlock()
		return
	}
	t.status = "merging"
	t.updated = time.Now()
	output := t.output
	t.mu.Unlock()

	if err := mergeSegments(output, len(t.segments)); err != nil {
		t.mu.Lock()
		t.status = "error"
		t.errText = err.Error()
		t.updated = time.Now()
		t.mu.Unlock()
		return
	}
	t.mu.Lock()
	t.status = "completed"
	t.updated = time.Now()
	t.mu.Unlock()
}

func mergeSegments(output string, n int) error {
	target, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("创建输出文件失败：%w", err)
	}
	defer target.Close()
	for i := 0; i < n; i++ {
		part, err := os.Open(output + downloadPartSuffix + strconv.Itoa(i))
		if err != nil {
			return fmt.Errorf("打开分段文件失败：%w", err)
		}
		if _, err = io.Copy(target, part); err != nil {
			part.Close()
			return fmt.Errorf("合并分段失败：%w", err)
		}
		part.Close()
	}
	for i := 0; i < n; i++ {
		_ = os.Remove(output + downloadPartSuffix + strconv.Itoa(i))
	}
	_ = os.Remove(output + downloadMetaSuffix)
	return nil
}

func (t *downloaderTask) waitRetry(ctx context.Context, retries int) bool {
	delay := time.Duration(1<<min(retries, 4)) * time.Second
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

func (t *downloaderTask) fail(index int, err error) {
	t.mu.Lock()
	if t.status == "running" {
		t.status = "error"
		t.errText = err.Error()
		t.updated = time.Now()
	}
	t.mu.Unlock()
}

func (t *downloaderTask) progressSampler() {
	for {
		time.Sleep(time.Second)
		t.mu.Lock()
		if t.status != "running" {
			t.mu.Unlock()
			return
		}
		now := time.Now()
		elapsed := now.Sub(t.lastTick).Seconds()
		if elapsed > 0.5 {
			t.speed = float64(t.done-t.lastDone) / elapsed
			t.lastDone = t.done
			t.lastTick = now
		}
		t.updated = now
		t.saveMeta()
		t.mu.Unlock()
	}
}

// saveMeta 落盘续传状态（调用方需持有 t.mu）。
func (t *downloaderTask) saveMeta() {
	meta := map[string]any{
		"url": t.url, "output": t.output, "total": t.total,
		"segments": t.segments,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return
	}
	_ = os.WriteFile(t.output+downloadMetaSuffix, data, 0o644)
}

func (t *downloaderTask) snapshot() DownloadTask {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}

// snapshotLocked 返回任务快照（调用方需持有 t.mu）。
func (t *downloaderTask) snapshotLocked() DownloadTask {
	return DownloadTask{
		ID: t.id, URL: t.url, Output: t.output,
		Total: t.total, Done: t.done, Segments: len(t.segments),
		Status: t.status, Error: t.errText, Speed: t.speed,
		StartedAt: t.started, UpdatedAt: t.updated,
	}
}
