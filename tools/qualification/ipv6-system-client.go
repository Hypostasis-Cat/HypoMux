// Independent OS-routed IPv6 client for the opt-in system acceptance runner.
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	target := flag.String("target", "", "literal IPv6 TLS target")
	name := flag.String("server-name", "", "verified TLS hostname")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp6", *target)
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
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nUser-Agent: Mozilla/5.0 HypoMuxIPv6Acceptance/1.0\r\nAccept-Encoding: identity\r\nConnection: close\r\n\r\n", *name)
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	count, err := io.Copy(io.Discard, response.Body)
	if err != nil {
		panic(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 400 || count == 0 {
		panic(fmt.Sprintf("HTTP status=%d bytes=%d", response.StatusCode, count))
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "downloaded", "http_status": response.StatusCode, "bytes": count, "server_name": *name})
}
