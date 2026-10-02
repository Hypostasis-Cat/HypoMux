package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxHTTPHeaderBytes = 64 * 1024
const httpHeaderTimeout = 10 * time.Second

func (s *Server) handleHTTP(reader *bufio.Reader, client net.Conn, session *connection) *Adapter {
	_ = client.SetReadDeadline(time.Now().Add(httpHeaderTimeout))
	header, err := readHTTPHeader(reader)
	_ = client.SetReadDeadline(time.Time{})
	if err != nil {
		writeHTTPError(client, "400 Bad Request")
		return nil
	}
	request, err := parseProxyRequest(header)
	if err != nil {
		writeHTTPError(client, "400 Bad Request")
		return nil
	}
	var message *http.Request
	var messageReader *bufio.Reader
	if !request.connect {
		messageReader = bufio.NewReader(io.MultiReader(bytes.NewReader(header), reader))
		message, err = http.ReadRequest(messageReader)
		if err != nil {
			writeHTTPError(client, "400 Bad Request")
			return nil
		}
		defer func() {
			// Body.Close can drain an unfinished server-side request body. Avoid
			// waiting for that body after a failed dial or an early response.
			_ = client.SetReadDeadline(time.Now())
			_ = message.Body.Close()
		}()
		prepareHTTPForwardRequest(message)
	}
	upstream, adapter, err := s.connect(session, net.JoinHostPort(request.host, strconv.Itoa(request.port)))
	if err != nil {
		writeHTTPError(client, "502 Bad Gateway")
		return nil
	}
	upstream = s.prepareSteamCDN(session, upstream, adapter, request.host, strconv.Itoa(request.port), steamChunkPath(request.forwardHeader))
	session.cdnObserver = s.newSteamObserver(session)
	defer session.cdnObserver.close()
	if request.connect {
		if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\nProxy-Agent: HypoMux\r\n\r\n")); err != nil {
			_ = upstream.Close()
			return nil
		}
		s.relay(reader, client, upstream, session)
	} else {
		s.relayHTTP(message, messageReader, client, upstream, session)
	}
	return &adapter
}

type proxyRequest struct {
	host          string
	port          int
	connect       bool
	forwardHeader []byte
}

func readHTTPHeader(reader *bufio.Reader) ([]byte, error) {
	var result bytes.Buffer
	lineStart := 0
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(fragment) > maxHTTPHeaderBytes-result.Len() {
			return nil, fmt.Errorf("HTTP header exceeds %d bytes", maxHTTPHeaderBytes)
		}
		result.Write(fragment)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return nil, err
		}
		line := result.Bytes()[lineStart:]
		if bytes.Equal(line, []byte("\r\n")) || bytes.Equal(line, []byte("\n")) {
			return result.Bytes(), nil
		}
		lineStart = result.Len()
	}
}

func httpUpgrade(header http.Header) bool {
	if header.Get("Upgrade") == "" {
		return false
	}
	for _, value := range header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				return true
			}
		}
	}
	return false
}

func stripHTTPHopHeaders(header http.Header, upgrade bool) {
	protocol := header.Get("Upgrade")
	for _, value := range header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			header.Del(strings.TrimSpace(token))
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Proxy-Authorization", "Proxy-Authenticate", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(name)
	}
	if upgrade {
		header.Set("Connection", "Upgrade")
		header.Set("Upgrade", protocol)
	}
}

func prepareHTTPForwardRequest(request *http.Request) {
	upgrade := httpUpgrade(request.Header)
	stripHTTPHopHeaders(request.Header, upgrade)
	request.RequestURI = ""
	request.URL.Scheme, request.URL.Host = "", ""
	// One framed request per proxy connection prevents a second authority from
	// inheriting the first request's upstream or NIC. CONNECT/101 remain tunnels.
	request.Close = !upgrade
}

func (s *Server) relayHTTP(request *http.Request, requestReader *bufio.Reader, client, upstream net.Conn, session *connection) {
	wantsUpgrade := httpUpgrade(request.Header)
	upgradeDecision := make(chan bool, 1)
	upstreamReader := bufio.NewReader(upstream)
	s.relayTransfers(client, upstream, session,
		func(writer io.Writer, buffer []byte) error {
			outgoing := *request
			if request.Body != nil {
				// Request.Write closes its body internally. Keep cancellation in
				// our control instead of draining an unfinished client upload.
				outgoing.Body = io.NopCloser(request.Body)
			}
			if err := outgoing.Write(writer); err != nil {
				_ = client.SetReadDeadline(time.Now())
				_ = upstream.SetReadDeadline(time.Now())
				return err
			}
			if wantsUpgrade && <-upgradeDecision {
				_, err := io.CopyBuffer(writer, readerOnly{requestReader}, buffer)
				return err
			}
			return nil
		},
		func(writer io.Writer, buffer []byte) error {
			defer func() {
				select {
				case upgradeDecision <- false:
				default:
				}
				// A final response (including an early rejection of an upload)
				// must not wait for the client to send more request body bytes.
				_ = client.SetReadDeadline(time.Now())
				_ = upstream.SetWriteDeadline(time.Now())
			}()
			for {
				header, err := readHTTPHeader(upstreamReader)
				if err != nil {
					writeHTTPError(client, "502 Bad Gateway")
					return err
				}
				response, err := http.ReadResponse(bufio.NewReader(io.MultiReader(bytes.NewReader(header), upstreamReader)), request)
				if err != nil {
					writeHTTPError(client, "502 Bad Gateway")
					return err
				}
				upgraded := response.StatusCode == http.StatusSwitchingProtocols
				if upgraded && (!wantsUpgrade || !httpUpgrade(response.Header)) {
					_ = response.Body.Close()
					writeHTTPError(client, "502 Bad Gateway")
					return fmt.Errorf("unexpected HTTP protocol upgrade")
				}
				stripHTTPHopHeaders(response.Header, upgraded)
				final := response.StatusCode >= 200
				response.Close = final
				body := response.Body
				// Response.Write also closes the body before returning an error.
				// Defer the real close until blocked upstream reads are canceled.
				response.Body = io.NopCloser(body)
				err = response.Write(writer)
				if err != nil {
					// Closing a partially read body otherwise drains the upstream,
					// which may wait forever after the client has gone away.
					_ = upstream.Close()
				}
				_ = body.Close()
				if err != nil {
					return err
				}
				if upgraded {
					upgradeDecision <- true
					_, err := io.CopyBuffer(writer, readerOnly{upstreamReader}, buffer)
					return err
				}
				if final {
					return nil
				}
			}
		})
}

func parseProxyRequest(header []byte) (proxyRequest, error) {
	text := strings.ReplaceAll(string(header), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return proxyRequest{}, fmt.Errorf("missing request line")
	}
	parts := strings.SplitN(lines[0], " ", 3)
	if len(parts) != 3 {
		return proxyRequest{}, fmt.Errorf("invalid request line")
	}
	method, target, version := parts[0], parts[1], parts[2]
	if strings.EqualFold(method, "CONNECT") {
		host, port, err := splitHostPort(target, 443)
		return proxyRequest{host: host, port: port, connect: true}, err
	}

	var host string
	port := 80
	originTarget := target
	parsed, err := url.Parse(target)
	if err == nil && parsed.IsAbs() && parsed.Hostname() != "" {
		host = parsed.Hostname()
		if parsed.Port() != "" {
			port, err = strconv.Atoi(parsed.Port())
			if err != nil {
				return proxyRequest{}, err
			}
		} else if strings.EqualFold(parsed.Scheme, "https") {
			port = 443
		}
		originTarget = parsed.RequestURI()
		if originTarget == "" {
			originTarget = "/"
		}
	} else {
		hostHeader := findHTTPHeader(lines[1:], "host")
		if hostHeader == "" {
			return proxyRequest{}, fmt.Errorf("missing Host header")
		}
		host, port, err = splitHostPort(hostHeader, 80)
		if err != nil {
			return proxyRequest{}, err
		}
	}

	var forward strings.Builder
	fmt.Fprintf(&forward, "%s %s %s\r\n", method, originTarget, version)
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(strings.SplitN(line, ":", 2)[0]))
		if name == "proxy-connection" || name == "proxy-authorization" {
			continue
		}
		forward.WriteString(line)
		forward.WriteString("\r\n")
	}
	forward.WriteString("\r\n")
	return proxyRequest{
		host:          host,
		port:          port,
		forwardHeader: []byte(forward.String()),
	}, nil
}

func findHTTPHeader(lines []string, wanted string) string {
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), wanted) {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func splitHostPort(value string, defaultPort int) (string, int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, fmt.Errorf("empty host")
	}
	if host, portText, err := net.SplitHostPort(value); err == nil {
		port, err := strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return "", 0, fmt.Errorf("invalid port")
		}
		return host, port, nil
	}
	if strings.Count(value, ":") == 1 {
		return "", 0, fmt.Errorf("invalid explicit host or port")
	}
	if strings.Count(value, ":") > 1 && !strings.HasPrefix(value, "[") {
		return "", 0, fmt.Errorf("IPv6 host must use brackets")
	}
	return strings.Trim(value, "[]"), defaultPort, nil
}

func writeHTTPError(client net.Conn, status string) {
	_, _ = fmt.Fprintf(client, "HTTP/1.1 %s\r\nConnection: close\r\nContent-Length: 0\r\n\r\n", status)
}
