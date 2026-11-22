package services

// 多线程分段下载模块的功能测试：
// 用 httptest 起一个支持 Range 的服务器，验证分段并发、合并结果与断点续传状态。

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func rangeTestServer(t *testing.T, payload []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/big.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if r.Method == http.MethodHead {
			return
		}
		if spec := r.Header.Get("Range"); spec != "" {
			var start, end int
			if _, err := fmt.Sscanf(spec, "bytes=%d-%d", &start, &end); err != nil {
				http.Error(w, "bad range", http.StatusBadRequest)
				return
			}
			if start < 0 || end >= len(payload) || start > end {
				http.Error(w, "range out of bounds", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
			w.Header().Set("Content-Length", strconv.Itoa(end-start+1)) // 分片长度，不能沿用总长
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start : end+1])
			return
		}
		_, _ = w.Write(payload)
	})
	return httptest.NewServer(mux)
}

func waitTaskDone(t *testing.T, s *DownloaderService, id string) DownloadTask {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		task, err := s.Status(id)
		if err != nil {
			t.Fatalf("查询任务失败: %v", err)
		}
		switch task.Status {
		case "completed":
			return task
		case "error", "cancelled":
			t.Fatalf("任务异常结束: status=%s err=%s", task.Status, task.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("等待任务完成超时")
	return DownloadTask{}
}

func TestMultiSegmentDownloadMergesCorrectly(t *testing.T) {
	payload := bytes.Repeat([]byte("HypoMux-MultiDownloader-"), 40000) // 约 960KB
	server := rangeTestServer(t, payload)
	defer server.Close()

	out := filepath.Join(t.TempDir(), "big.bin")
	service := NewDownloaderService()
	task, err := service.Start(server.URL+"/big.bin", out, 8)
	if err != nil {
		t.Fatalf("启动下载失败: %v", err)
	}
	if task.Segments != 8 {
		t.Fatalf("期望 8 个分段，实际 %d", task.Segments)
	}
	task = waitTaskDone(t, service, task.ID)

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("读取输出失败: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("合并结果不一致: got %d bytes, want %d bytes", len(got), len(payload))
	}
	// 分段文件与元数据应被清理
	if _, err := os.Stat(out + downloadMetaSuffix); !os.IsNotExist(err) {
		t.Fatalf("元数据文件未清理")
	}
	for i := range 8 {
		if _, err := os.Stat(out + downloadPartSuffix + strconv.Itoa(i)); !os.IsNotExist(err) {
			t.Fatalf("分段 %d 文件未清理", i)
		}
	}
	_ = task
}

func TestSmallFileClampsSegments(t *testing.T) {
	payload := []byte("tiny")
	server := rangeTestServer(t, payload)
	defer server.Close()

	out := filepath.Join(t.TempDir(), "tiny.bin")
	service := NewDownloaderService()
	task, err := service.Start(server.URL+"/big.bin", out, 64)
	if err != nil {
		t.Fatalf("启动下载失败: %v", err)
	}
	if task.Segments != len(payload) {
		t.Fatalf("期望分段数收敛为文件大小 %d，实际 %d", len(payload), task.Segments)
	}
	waitTaskDone(t, service, task.ID)
	got, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("小文件下载结果错误: err=%v got=%q", err, got)
	}
}

func TestPauseAndResume(t *testing.T) {
	payload := bytes.Repeat([]byte(strings.Repeat("x", 1024)), 512) // 512KB
	server := rangeTestServer(t, payload)
	defer server.Close()

	out := filepath.Join(t.TempDir(), "resume.bin")
	service := NewDownloaderService()
	task, err := service.Start(server.URL+"/big.bin", out, 4)
	if err != nil {
		t.Fatalf("启动下载失败: %v", err)
	}
	// 立刻暂停再继续，验证状态机不卡死且最终完成
	if _, err = service.Pause(task.ID); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}
	paused, err := service.Status(task.ID)
	if err != nil || paused.Status != "paused" {
		t.Fatalf("暂停后状态错误: err=%v status=%s", err, paused.Status)
	}
	if _, err = service.Resume(task.ID); err != nil {
		t.Fatalf("继续失败: %v", err)
	}
	waitTaskDone(t, service, task.ID)
	got, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("暂停续传后结果错误: err=%v got=%d bytes", err, len(got))
	}
}
