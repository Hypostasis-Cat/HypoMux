package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
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
	if err != nil || req.Method != "GET" || req.ContentLength > 0 || len(req.TransferEncoding) > 0 || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
		return ""
	}
	if req.URL.RawQuery != "" || req.URL.RawPath != "" || !steamChunkPattern.MatchString(req.URL.Path) {
		return ""
	}
	return req.URL.Path
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
	if !steamDownloadHost(host) || !publicCDNIP(ip) || !steamChunkPattern.MatchString(path) {
		return nil, errors.New("invalid probe scope")
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
	req.Header.Set("Range", "bytes=0-4095")
	req.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusPartialContent || response.Header.Get("Content-Encoding") != "" {
		return nil, errors.New("range unsupported or invalid response")
	}
	if response.Header.Get("Content-Range") == "" {
		return nil, errors.New("missing content range")
	}
	// Require an exact 4 KiB prefix; short chunks simply retain original routing.
	total, parseErr := strconv.ParseUint(strings.TrimPrefix(response.Header.Get("Content-Range"), "bytes 0-4095/"), 10, 64)
	if !strings.HasPrefix(response.Header.Get("Content-Range"), "bytes 0-4095/") || parseErr != nil || total < 4096 {
		return nil, errors.New("unexpected range")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(data) != 4096 {
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
