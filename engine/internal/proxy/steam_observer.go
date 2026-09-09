package proxy

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Classification never returns the request URI in a diagnostic. Signed URIs
// live only in bounded observer state and the task that consumes them.
func steamRequestURI(req *http.Request) (string, string) {
	if req.Method != "GET" {
		return "", "http_method"
	}
	if req.ContentLength > 0 || len(req.TransferEncoding) > 0 {
		return "", "http_request_body"
	}
	if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" || req.URL.User != nil {
		return "", "http_credentials"
	}
	if req.URL.RawPath != "" || !steamChunkPattern.MatchString(req.URL.Path) {
		return "", "http_path"
	}
	if req.Header.Get("Range") != "" {
		return "", "http_client_range"
	}
	if req.URL.RawQuery != "" || req.URL.ForceQuery {
		q, e := url.ParseQuery(req.URL.RawQuery)
		if e != nil {
			return "", "http_query"
		}
		host := normalizeDomain(req.Host)
		if h, p, e := net.SplitHostPort(host); e == nil && p == "80" {
			host = h
		}
		allowed := map[string]bool{}
		switch host {
		case "xz.pphimalayanrt.com", "xz.sycontroller.com":
			if q.Get("auth_key") == "" {
				return "", "http_query"
			}
			allowed = map[string]bool{"auth_key": true, "reqhost": true, "reqaidabccty": true}
		case "st.dl.eccdnx.com", "gstore.val.manlaxy.com", "gstore-y.bal.manlaxy.com":
			allowed = map[string]bool{"expiration_time": true, "token": true}
		case "dl.steam.clngaa.com", "dl1.steam.clngaa.com":
			if q.Get("t") == "" || q.Get("k") == "" {
				return "", "http_query"
			}
			allowed = map[string]bool{"t": true, "k": true, "rdkey": true}
		default:
			return "", "http_query"
		}
		if (q.Get("auth_key") == "" && q.Get("t") == "" && len(q) != len(allowed)) || len(q) == 0 {
			return "", "http_query"
		}
		for k, v := range q {
			if !allowed[k] || len(v) != 1 || len(v[0]) == 0 || len(v[0]) > 512 {
				return "", "http_query"
			}
		}
		// Allow only these observed content-token profiles; a server rejection
		// is reported rather than assuming token reuse succeeds. Never normalize signed bytes.
		expiry := q.Get("expiration_time")
		if q.Get("t") != "" {
			expiry = q.Get("t")
		}
		if v := expiry; v != "" {
			n, e := strconv.ParseInt(v, 10, 64)
			if e != nil || n <= time.Now().Unix() {
				return "", "http_signature_expired"
			}
		}
		if v := q.Get("auth_key"); v != "" {
			parts := strings.Split(v, "-")
			if len(parts) != 4 {
				return "", "http_query"
			}
			if _, e := strconv.ParseInt(parts[0], 10, 64); e != nil {
				return "", "http_query"
			}
		}
		return req.URL.RequestURI(), "http_signed_eligible"
	}
	return req.URL.RequestURI(), "http_eligible"
}

type steamObservedRequest struct {
	req *http.Request
	uri string
	at  time.Time
}
type steamHTTPObserver struct {
	stopContext func() bool
	mu          sync.Mutex
	s           *Server
	session     *connection
	adapter     Adapter
	up, down    steamHTTPFramer
	pending     []steamObservedRequest
	current     *steamObservedRequest
	prefix      []byte
	total       int64
	timer       *time.Timer
	stopped     bool
}

// The parser observes bytes at the forwarding boundary. It never waits for bytes,
// owns at most two bounded headers, and skips payloads by HTTP framing.
type steamHTTPFramer struct {
	header    []byte
	remaining int64
	state     string
	chunked   bool
	trailers  int
}

func (f *steamHTTPFramer) feed(data []byte, header func([]byte) (int64, bool, bool), body func([]byte), done func()) bool {
	for len(data) > 0 {
		switch f.state {
		case "body":
			n := int64(len(data))
			if n > f.remaining {
				n = f.remaining
			}
			body(data[:n])
			data = data[n:]
			f.remaining -= n
			if f.remaining == 0 {
				if f.chunked {
					f.state = "crlf"
				} else {
					f.state = ""
					done()
				}
			}
		case "crlf":
			f.header = append(f.header, data[0])
			data = data[1:]
			if len(f.header) == 2 {
				if string(f.header) != "\r\n" {
					return false
				}
				f.header = nil
				f.state = "chunk"
			}
		case "chunk", "trailer":
			f.header = append(f.header, data[0])
			data = data[1:]
			if len(f.header) > 4096 {
				return false
			}
			if bytes.HasSuffix(f.header, []byte("\r\n")) {
				line := string(f.header[:len(f.header)-2])
				f.header = nil
				if f.state == "trailer" {
					f.trailers += len(line) + 2
					if f.trailers > steamSniffLimit {
						return false
					}
					if line == "" {
						f.state = ""
						done()
					}
					continue
				}
				size, _, _ := strings.Cut(line, ";")
				n, e := strconv.ParseUint(size, 16, 63)
				if e != nil {
					return false
				}
				f.remaining = int64(n)
				if n == 0 {
					f.state = "trailer"
					f.trailers = 0
				} else {
					f.state = "body"
				}
			}
		default:
			f.header = append(f.header, data[0])
			data = data[1:]
			if len(f.header) > steamSniffLimit {
				return false
			}
			if bytes.HasSuffix(f.header, []byte("\r\n\r\n")) {
				n, chunked, ok := header(f.header)
				f.header = nil
				if !ok || n < 0 && !chunked {
					return false
				}
				f.remaining = n
				f.chunked = chunked
				if chunked {
					f.state = "chunk"
				} else if n > 0 {
					f.state = "body"
				} else {
					done()
				}
			}
		}
	}
	return true
}
func (s *Server) newSteamObserver(session *connection) *steamHTTPObserver {
	if session.cdnKey.port != "80" || !s.cdn.active() {
		return nil
	}
	c := s.cdn
	c.mu.Lock()
	defer c.mu.Unlock()
	if session.cdnGeneration != c.generation || c.observers >= 64 {
		return nil
	}
	c.observers++
	o := &steamHTTPObserver{s: s, session: session}
	for _, a := range s.config.Adapters {
		if a.Name == session.cdnKey.adapter {
			o.adapter = a
			break
		}
	}
	o.timer = time.AfterFunc(5*time.Second, o.expire)
	o.mu.Lock()
	o.stopContext = context.AfterFunc(c.ctx, o.close)
	o.mu.Unlock()
	return o
}
func (o *steamHTTPObserver) expire() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return
	}
	c := o.s.cdn
	c.mu.Lock()
	active := c.enabled && c.generation == o.session.cdnGeneration
	c.mu.Unlock()
	if !active {
		o.stopLocked()
		return
	}
	if len(o.up.header) > 0 || len(o.down.header) > 0 {
		o.note("http_header_timeout")
		o.stopLocked()
		return
	}
	for i := range o.pending {
		if time.Since(o.pending[i].at) >= 25*time.Second {
			o.pending[i].uri = ""
		}
	}
	if o.current != nil && time.Since(o.current.at) >= 25*time.Second {
		o.current.uri = ""
		o.prefix = nil
	}
	o.timer = time.AfterFunc(5*time.Second, o.expire)
}
func (o *steamHTTPObserver) stopLocked() {
	if o.stopped {
		return
	}
	o.stopped = true
	if o.stopContext != nil {
		o.stopContext()
	}
	if o.timer != nil {
		o.timer.Stop()
	}
	o.pending = nil
	o.current = nil
	o.prefix = nil
	o.up.header = nil
	o.down.header = nil
	o.s.cdn.mu.Lock()
	o.s.cdn.observers--
	o.s.cdn.mu.Unlock()
}
func (o *steamHTTPObserver) close() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopLocked()
}
func (o *steamHTTPObserver) note(stage string) {
	k := o.session.cdnKey
	o.s.cdn.note(o.session.cdnGeneration, k.domain, k.adapter, k.ip, stage)
}
func (o *steamHTTPObserver) feed(up bool, p []byte) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return
	}
	c := o.s.cdn
	c.mu.Lock()
	active := c.enabled && c.generation == o.session.cdnGeneration
	c.mu.Unlock()
	if !active {
		o.stopLocked()
		return
	}
	var ok bool
	if up {
		ok = o.up.feed(p, func(h []byte) (int64, bool, bool) {
			r, e := http.ReadRequest(bufio.NewReader(bytes.NewReader(h)))
			if e != nil || r.ProtoMajor != 1 || r.Header.Get("Upgrade") != "" || len(o.pending) >= 16 {
				return 0, false, false
			}
			host := normalizeDomain(r.Host)
			if v, port, e := net.SplitHostPort(host); e == nil && port == "80" {
				host = v
			}
			if host != o.session.cdnKey.domain {
				return 0, false, false
			}
			uri, reason := steamRequestURI(r)
			o.note(reason)
			// Keep only method and URI metadata, never cookies or application headers.
			o.pending = append(o.pending, steamObservedRequest{req: &http.Request{Method: r.Method}, uri: uri, at: time.Now()})
			return r.ContentLength, len(r.TransferEncoding) > 0, true
		}, func([]byte) {}, func() {})
	} else {
		ok = o.down.feed(p, func(h []byte) (int64, bool, bool) {
			if len(o.pending) == 0 {
				return 0, false, false
			}
			r, e := http.ReadResponse(bufio.NewReader(bytes.NewReader(h)), o.pending[0].req)
			if e != nil || r.ProtoMajor != 1 || r.StatusCode == 101 {
				return 0, false, false
			}
			if r.StatusCode >= 100 && r.StatusCode < 200 {
				return 0, false, true
			}
			current := o.pending[0]
			o.pending[0] = steamObservedRequest{}
			o.pending = o.pending[1:]
			o.current = &current
			o.total = r.ContentLength
			o.prefix = nil
			if r.StatusCode >= 300 && r.StatusCode < 400 {
				stage := "http_redirect_unsupported"
				if u, e := url.Parse(r.Header.Get("Location")); e == nil {
					if u.Scheme == "" && u.Host == "" {
						stage = "http_redirect_observed"
					} else if u.Scheme == "http" && (u.Port() == "" || u.Port() == "80") {
						if publicCDNIP(u.Hostname()) {
							stage = "http_redirect_ip"
						} else if steamDownloadHost(u.Hostname()) {
							stage = "http_redirect_observed"
						}
					}
				}
				o.note(stage)
			}
			reason := ""
			switch {
			case r.StatusCode != 200:
				reason = "http_response_status"
			case r.Header.Get("Content-Encoding") != "":
				reason = "http_response_encoding"
			case len(r.TransferEncoding) > 0:
				reason = "http_response_chunked"
			case r.ContentLength <= 0:
				reason = "http_response_length"
			case time.Since(current.at) > 25*time.Second:
				reason = "http_response_stale"
			}
			if reason != "" {
				if o.current.uri != "" {
					o.note(reason)
				}
				o.current.uri = ""
			}
			if r.StatusCode >= 400 && o.session.cdnTrial {
				o.session.cdnResponseFailed = true
				o.s.cdn.finishTransfer(o.session.cdnKey, o.session.cdnGeneration, 0, true)
			}
			if r.StatusCode == 401 || r.StatusCode == 403 {
				o.note("http_signature_rejected")
			}
			length := r.ContentLength
			if r.Body == http.NoBody {
				length = 0
			}
			return length, len(r.TransferEncoding) > 0, true
		}, func(p []byte) {
			if o.current == nil || o.current.uri == "" {
				return
			}
			n := min(len(p), 4096-len(o.prefix))
			o.prefix = append(o.prefix, p[:n]...)
			if int64(len(o.prefix)) == min(int64(4096), o.total) {
				o.s.discoverObservedSteam(o.session, o.adapter, o.current.uri, append([]byte(nil), o.prefix...), o.total, o.current.at.Add(30*time.Second))
				o.current.uri = ""
				o.prefix = nil
			}
		}, func() { o.current = nil; o.prefix = nil })
	}
	if !ok {
		o.note("http_observer_unsupported")
		o.stopLocked()
	}
}

// Inspect requests before upstream writes, responses after client writes.
// A fast upstream may answer before Write returns. Parsing cannot change bytes.
type steamObserverWriter struct {
	io.Writer
	observer *steamHTTPObserver
	up       bool
}

func (w steamObserverWriter) Write(p []byte) (int, error) {
	if w.up {
		w.observer.feed(true, p)
	}
	n, e := w.Writer.Write(p)
	if !w.up {
		w.observer.feed(false, p[:n])
	}
	return n, e
}
