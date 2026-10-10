package services

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
)

// Uses a built Core and loopback traffic only; no system-proxy or NIC changes.
func TestManualProxyTelemetryIntegration(t *testing.T) {
	path := os.Getenv("HYPOMUX_TEST_ENGINE_PATH")
	if path == "" {
		t.Skip("set HYPOMUX_TEST_ENGINE_PATH to a freshly built Core")
	}
	t.Setenv("HYPOMUX_ENGINE_PATH", path)
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	values := settings.Get()
	values.Mode, values.SystemProxyTakeover = "proxy", false
	if _, err := settings.Update(values); err != nil {
		t.Fatal(err)
	}
	service := &EngineService{client: engineclient.New(), settings: settings}
	defer service.client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := service.client.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	var started engineStartResult
	if err := service.client.Request(ctx, "engine.start", map[string]any{
		"mode": "proxy", "socks_port": 0, "http_port": 0,
		"adapters": []map[string]any{{"name": "loopback", "source_ip": "127.0.0.1"}},
	}, &started); err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{"socks5", "http"} {
		t.Run(protocol, func(t *testing.T) {
			finish := make(chan struct{})
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = w.Write(bytes.Repeat([]byte("d"), 128*1024))
				w.(http.Flusher).Flush()
				<-finish
			}))
			defer origin.Close()
			defer close(finish)
			endpoint := started.Endpoints.SOCKS
			if protocol == "http" {
				endpoint = started.Endpoints.HTTP
			}
			proxyURL, err := url.Parse(protocol + "://" + endpoint)
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
			baseline, err := service.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Post(origin.URL, "application/octet-stream", bytes.NewReader(bytes.Repeat([]byte("u"), 64*1024)))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if _, err := io.ReadFull(response.Body, make([]byte, 128*1024)); err != nil {
				t.Fatal(err)
			}
			time.Sleep(minimumThroughputInterval + 100*time.Millisecond)
			snapshot, err := service.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Connections < 1 || snapshot.SessionBytes <= baseline.SessionBytes || snapshot.DownloadBPS <= 0 || snapshot.UploadBPS <= 0 ||
				len(snapshot.Adapters) != 1 || snapshot.Adapters[0].DownloadBPS <= 0 || snapshot.Adapters[0].UploadBPS <= 0 {
				t.Fatalf("manual proxy traffic missing from Home telemetry: %+v", snapshot)
			}
			connections, err := service.Connections()
			if err != nil {
				t.Fatal(err)
			}
			if len(connections.Connections) < 1 || connections.Connections[0].BytesUp == 0 || connections.Connections[0].BytesDown == 0 {
				t.Fatalf("manual proxy traffic missing from Connections: %+v", connections)
			}
			if runtime.GOOS == "windows" && connections.Connections[0].Process == "" {
				t.Fatalf("manual proxy client process was not resolved: %+v", connections)
			}
		})
	}
}
