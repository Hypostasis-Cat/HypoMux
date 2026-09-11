package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var steamChunkPattern = regexp.MustCompile(`^/depot/[0-9]+/chunk/[a-fA-F0-9]{40}$`)

// Only public content-addressed chunk paths, with no tokens/query/cookies,
// may be replayed as bounded probes. Never forward application headers.
func steamChunkPath(header []byte) string {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(header)))
	if err != nil {
		return ""
	}
	uri, _ := steamRequestURI(req)
	return uri
}
func peekSteamChunkPath(reader *bufio.Reader) string {
	n := min(reader.Buffered(), steamSniffLimit)
	data, _ := reader.Peek(n)
	if end := bytes.Index(data, []byte("\r\n\r\n")); end >= 0 {
		return steamChunkPath(data[:end+4])
	}
	return ""
}
func (s *Server) probeSteamHTTP(ctx context.Context, adapter Adapter, host, ip, path string) ([]byte, error) {
	return s.probeSteamHTTPRange(ctx, adapter, host, ip, path, 4096, 0)
}
func steamProbeReason(err error) string {
	if err == nil {
		return "verified"
	}
	switch err.Error() {
	case "http_budget", "http_signature_rejected", "http_range_unsupported", "http_redirect_observed":
		return err.Error()
	}
	return "http_probe_failed"
}
func (s *Server) probeSteamHTTPRange(ctx context.Context, adapter Adapter, host, ip, path string, size int, totalExpected int64) ([]byte, error) {
	return s.probeSteamRange(ctx, adapter, host, ip, path, size, totalExpected, false)
}

// Extended requests are only used after content-prefix verification. They use
// a separate, persistent budget and never contribute to real-transfer scores.
func (s *Server) probeSteamRange(ctx context.Context, adapter Adapter, host, ip, path string, size int, totalExpected int64, extended bool) ([]byte, error) {
	limit := 4096
	if extended {
		limit = steamSpeedProbeSize
	}
	if !steamDownloadHost(host) || !publicCDNIP(ip) || !validSteamProbeURI(host, path) || size < 1 || size > limit {
		return nil, errors.New("invalid probe scope")
	}
	if ctx.Err() != nil {
		return nil, errors.New("http_probe_cancelled")
	}
	if extended {
		if s.cdn == nil || !s.cdn.reserveSpeedProbe(size+1) {
			return nil, errors.New("http_speed_budget")
		}
	} else if s.cdn != nil {
		c := s.cdn
		c.mu.Lock()
		now := c.now()
		if now.Sub(c.budgetAt) >= time.Minute {
			c.budgetAt = now
			c.budgetBytes = 0
			c.budgetAttempts = 0
		}
		allowed := c.enabled && c.budgetBytes+size+1 <= 256*1024 && c.budgetAttempts < 64
		if allowed {
			c.budgetBytes += size + 1
			c.budgetAttempts++
		}
		c.mu.Unlock()
		if !allowed {
			return nil, errors.New("http_budget")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, MaxResponseHeaderBytes: 16 * 1024,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer, err := boundNetworkDialer(adapter, 750*time.Millisecond, networkForIP("tcp", net.ParseIP(ip)))
			if err != nil {
				return nil, err
			}
			return s.dialTCP(ctx, dialer, net.JoinHostPort(ip, "80"))
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+host+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", size-1))
	req.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("http_probe_failed")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return nil, errors.New("http_signature_rejected")
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return nil, errors.New("http_redirect_observed")
	}
	if response.StatusCode != http.StatusPartialContent || response.Header.Get("Content-Encoding") != "" {
		return nil, errors.New("http_range_unsupported")
	}
	if response.Header.Get("Content-Range") == "" {
		return nil, errors.New("missing content range")
	}
	// Require exactly the requested range and the observed total object length.
	rangePrefix := fmt.Sprintf("bytes 0-%d/", size-1)
	total, parseErr := strconv.ParseUint(strings.TrimPrefix(response.Header.Get("Content-Range"), rangePrefix), 10, 64)
	if !strings.HasPrefix(response.Header.Get("Content-Range"), rangePrefix) || parseErr != nil || total < uint64(size) || totalExpected > 0 && total != uint64(totalExpected) {
		return nil, errors.New("unexpected range")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(size+1)))
	if err != nil || len(data) != size {
		return nil, errors.New("invalid body length")
	}
	return data, nil
}
func (c *steamCDN) note(generation uint64, host, adapter, ip, stage string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled || generation != c.generation {
		return
	}
	now := c.now()
	c.stageCounts[stage]++
	for _, previous := range c.diagnostics {
		if previous.Domain == host && previous.Adapter == adapter && previous.IP == ip && previous.Stage == stage && now.Sub(previous.At) < 30*time.Second {
			return
		}
	}
	if len(c.diagnostics) >= 64 {
		c.diagnostics = c.diagnostics[1:]
	}
	c.diagnostics = append(c.diagnostics, SteamCDNDiagnostic{Domain: host, Adapter: adapter, IP: ip, Stage: stage, At: now})
}

func validSteamProbeURI(host, uri string) bool {
	u, e := url.ParseRequestURI(uri)
	if e != nil || u.IsAbs() || u.Host != "" {
		return false
	}
	value, _ := steamRequestURI(&http.Request{Method: "GET", Host: host, URL: u, Header: make(http.Header)})
	return value != ""
}
