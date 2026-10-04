// Independent OS-routed client for IPv6 acceptance and IPv4 regression.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	target := flag.String("target", "", "literal TLS target")
	name := flag.String("server-name", "", "verified TLS hostname")
	udpTarget := flag.String("udp-target", "", "literal NTP target for an independent OS-routed UDP check")
	family := flag.String("family", "IPv6", "explicit IPv6 or IPv4 system route")
	path := flag.String("path", "/", "HTTPS request path")
	payloadBytes := flag.Int("payload-bytes", 0, "exact byte range to request, or zero for the whole response")
	flag.Parse()
	if *family != "IPv6" && *family != "IPv4" {
		panic("provide IPv6 or IPv4 family")
	}
	suffix := "6"
	if *family == "IPv4" {
		suffix = "4"
	}
	if *udpTarget != "" {
		verifyUDP(*udpTarget, suffix)
		return
	}
	parsed, err := url.ParseRequestURI(*path)
	if err != nil || parsed.IsAbs() || !strings.HasPrefix(*path, "/") {
		panic("provide a valid origin-form HTTPS request path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp"+suffix, *target)
	if err != nil {
		panic(err)
	}
	defer raw.Close()
	raw.SetDeadline(time.Now().Add(25 * time.Second))
	conn := tls.Client(raw, &tls.Config{ServerName: *name, MinVersion: tls.VersionTLS12})
	if err := conn.HandshakeContext(ctx); err != nil {
		panic(err)
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "connected", "source": raw.LocalAddr().String(), "peer": raw.RemoteAddr().String(), "certificate_verified": true})
	var command string
	if _, err := fmt.Fscanln(os.Stdin, &command); err != nil || command != "fetch" {
		panic("controller did not request fetch")
	}
	rangeHeader := ""
	if *payloadBytes > 0 {
		rangeHeader = fmt.Sprintf("Range: bytes=0-%d\r\n", *payloadBytes-1)
	}
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: Mozilla/5.0 HypoMuxIPv6Acceptance/1.0\r\nAccept-Encoding: identity\r\n%sConnection: close\r\n\r\n", *path, *name, rangeHeader)
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	if *payloadBytes > 0 && (response.StatusCode != http.StatusPartialContent || !strings.HasPrefix(response.Header.Get("Content-Range"), fmt.Sprintf("bytes 0-%d/", *payloadBytes-1))) {
		panic("HTTPS server did not honor the bounded payload range")
	}
	digest := sha256.New()
	reader := io.Reader(response.Body)
	if *payloadBytes > 0 {
		reader = io.LimitReader(response.Body, int64(*payloadBytes)+1)
	}
	count, err := io.Copy(digest, reader)
	if err != nil {
		panic(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 400 || count == 0 {
		panic(fmt.Sprintf("HTTP status=%d bytes=%d", response.StatusCode, count))
	}
	if *payloadBytes > 0 && count != int64(*payloadBytes) {
		panic("HTTPS payload length mismatch")
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "downloaded", "http_status": response.StatusCode, "bytes": count, "server_name": *name, "path": *path, "sha256": fmt.Sprintf("%x", digest.Sum(nil))})
}

func verifyUDP(target, suffix string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "udp"+suffix, target)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(8 * time.Second))
	query := make([]byte, 48)
	query[0] = 0x23
	now := time.Now()
	binary.BigEndian.PutUint32(query[40:], uint32(now.Unix()+2208988800))
	binary.BigEndian.PutUint32(query[44:], uint32((uint64(now.Nanosecond())<<32)/1e9))
	if _, err := conn.Write(query); err != nil {
		panic(err)
	}
	buffer := make([]byte, 4096)
	n, err := conn.Read(buffer)
	if err != nil {
		panic(err)
	}
	p := buffer[:n]
	if n < 48 || p[0]&7 != 4 || p[0]>>6 == 3 || p[1] == 0 || p[1] > 15 || !bytes.Equal(p[24:32], query[40:48]) || bytes.Equal(p[40:48], make([]byte, 8)) {
		panic("invalid NTP reply or request correlation")
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "udp_reply", "source": conn.LocalAddr().String(), "peer": conn.RemoteAddr().String(), "bytes": n, "request_verified": true})
}
