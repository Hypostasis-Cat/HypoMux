package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const maxHTTPHeaderBytes = 64 * 1024

func (s *Server) handleHTTP(reader *bufio.Reader, client net.Conn, session *connection) *Adapter {
	header, err := readHTTPHeader(reader)
	if err != nil {
		writeHTTPError(client, "400 Bad Request")
		return nil
	}
	request, err := parseProxyRequest(header)
	if err != nil {
		writeHTTPError(client, "400 Bad Request")
		return nil
	}
	upstream, adapter, err := s.connect(session, net.JoinHostPort(request.host, strconv.Itoa(request.port)))
	if err != nil {
		writeHTTPError(client, "502 Bad Gateway")
		return nil
	}
	if request.connect {
		if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\nProxy-Agent: HypoMux\r\n\r\n")); err != nil {
			_ = upstream.Close()
			return nil
		}
	} else if _, err := upstream.Write(request.forwardHeader); err != nil {
		_ = upstream.Close()
		writeHTTPError(client, "502 Bad Gateway")
		return nil
	}
	s.relay(reader, client, upstream, session)
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
	for result.Len() <= maxHTTPHeaderBytes {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		result.Write(line)
		if bytes.Equal(line, []byte("\r\n")) || bytes.Equal(line, []byte("\n")) {
			return result.Bytes(), nil
		}
	}
	return nil, fmt.Errorf("HTTP header exceeds %d bytes", maxHTTPHeaderBytes)
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
