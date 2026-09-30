package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
)

func TestSteamSniffHTTPReadAheadPreservesHostAndBytes(t *testing.T) {
	request := "GET " + testSteamChunk + " HTTP/1.1\r\nHost: " + testSteamHost + "\r\n\r\n"
	tests := []struct {
		name, payload string
	}{
		{"pipelined requests", strings.Repeat(request, 300)},
	}
	for _, bodySize := range []int{8192, 20000, 70000} {
		tests = append(tests, struct{ name, payload string }{
			fmt.Sprintf("body %d", bodySize),
			fmt.Sprintf("POST /metadata HTTP/1.1\r\nHost: %s\r\nContent-Length: %d\r\n\r\n", testSteamHost, bodySize) + strings.Repeat("x", bodySize),
		})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, peer := net.Pipe()
			defer client.Close()
			defer peer.Close()
			reader := bufio.NewReaderSize(strings.NewReader(test.payload), 64*1024)
			if host := sniffSteamHost(reader, client, "80"); host != testSteamHost {
				t.Errorf("host = %q, want %q", host, testSteamHost)
			}
			data, err := io.ReadAll(reader)
			if err != nil || string(data) != test.payload {
				t.Fatal("sniff changed forwarded bytes", err)
			}
		})
	}
}

func TestSteamSniffHTTPHeaderLimit(t *testing.T) {
	prefix := "GET / HTTP/1.1\r\nHost: " + testSteamHost + "\r\nX-Padding: "
	suffix := "\r\n\r\n"
	for _, size := range []int{steamSniffLimit, steamSniffLimit + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			payload := prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
			client, peer := net.Pipe()
			defer client.Close()
			defer peer.Close()
			reader := bufio.NewReaderSize(strings.NewReader(payload), 64*1024)
			want := ""
			if size == steamSniffLimit {
				want = testSteamHost
			}
			if got := sniffSteamHost(reader, client, "80"); got != want {
				t.Errorf("host = %q, want %q", got, want)
			}
			data, err := io.ReadAll(reader)
			if err != nil || string(data) != payload {
				t.Fatal("header limit changed forwarded bytes", err)
			}
		})
	}
}
