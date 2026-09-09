package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const steamSniffLimit = 16 * 1024

// This connection only peeks. TLS's parser handles fragmented ClientHellos;
// its replies are discarded and every input byte remains for the real server.
type helloPeekConn struct {
	net.Conn
	reader *bufio.Reader
	offset int
}

func (p *helloPeekConn) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	if p.offset >= steamSniffLimit {
		return 0, io.ErrShortBuffer
	}
	_, err := p.reader.Peek(p.offset + 1)
	if err != nil {
		return 0, err
	}
	end := min(p.reader.Buffered(), p.offset+len(dst), steamSniffLimit)
	data, err := p.reader.Peek(end)
	if err != nil {
		return 0, err
	}
	n := copy(dst, data[p.offset:end])
	p.offset += n
	return n, nil
}
func (p *helloPeekConn) Write(data []byte) (int, error) { return len(data), nil }

func sniffSteamHost(reader *bufio.Reader, client net.Conn, port string) string {
	if port != "80" && port != "443" {
		return ""
	}
	_ = client.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	defer client.SetReadDeadline(time.Time{})
	first, err := reader.Peek(1)
	if err != nil {
		return ""
	}
	host := ""
	if first[0] == 22 && port == "443" {
		parser := tls.Server(&helloPeekConn{Conn: client, reader: reader}, &tls.Config{
			GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
				host = hello.ServerName
				return nil, errors.New("SNI inspection complete")
			},
		})
		_ = parser.Handshake()
	} else if port == "80" && (first[0] >= 'A' && first[0] <= 'Z') {
		for n := 1; n <= steamSniffLimit; {
			data, peekErr := reader.Peek(n)
			if peekErr != nil {
				break
			}
			if end := bytes.Index(data, []byte("\r\n\r\n")); end >= 0 {
				request, parseErr := http.ReadRequest(bufio.NewReader(bytes.NewReader(data[:end+4])))
				if parseErr == nil && request.Method != "CONNECT" {
					host = request.Host
					if strings.Contains(host, ":") {
						var explicitPort string
						host, explicitPort, _ = net.SplitHostPort(host)
						if explicitPort != port {
							host = ""
						}
					}
				}
				break
			}
			n = min(steamSniffLimit+1, max(n+1, reader.Buffered()))
		}
	}
	if !steamDownloadHost(host) {
		return ""
	}
	return normalizeDomain(host)
}
