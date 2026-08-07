package services

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestProbeTUNConnectivityAcceptsRealHTTPResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	original := tunConnectivityURLs
	tunConnectivityURLs = []string{server.URL}
	t.Cleanup(func() { tunConnectivityURLs = original })

	detail, err := probeTUNConnectivity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if detail == "" {
		t.Fatal("expected connectivity evidence")
	}
}

func TestProbeTUNConnectivityRejectsUnavailableEndpoints(t *testing.T) {
	original := tunConnectivityURLs
	tunConnectivityURLs = []string{"http://127.0.0.1:1/"}
	t.Cleanup(func() { tunConnectivityURLs = original })
	if _, err := probeTUNConnectivity(context.Background()); err == nil {
		t.Fatal("expected failed connectivity probe")
	}
}

func TestProbeTUNConnectivityReportsEveryEndpointFailure(t *testing.T) {
	original := tunConnectivityURLs
	tunConnectivityURLs = []string{
		"http://127.0.0.1:1/first",
		"http://127.0.0.1:2/second",
	}
	t.Cleanup(func() { tunConnectivityURLs = original })
	_, err := probeTUNConnectivity(context.Background())
	if err == nil || !strings.Contains(err.Error(), "/first") || !strings.Contains(err.Error(), "/second") {
		t.Fatalf("connectivity error did not retain all endpoints: %v", err)
	}
}

func TestProbeTUNConnectivityAllowsAnyHTTPAlternative(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	failedEndpoint := closedTestHTTPURL(t)
	original := tunConnectivityURLs
	tunConnectivityURLs = []string{failedEndpoint, server.URL}
	t.Cleanup(func() { tunConnectivityURLs = original })

	detail, err := probeTUNConnectivity(context.Background())
	if err != nil {
		t.Fatalf("one successful system-direct endpoint should pass: %v", err)
	}
	if !strings.Contains(detail, failedEndpoint) || !strings.Contains(detail, "ok=true") {
		t.Fatalf("connectivity summary lost alternative endpoint evidence: %s", detail)
	}
}

func TestChannelConnectivitySeparatesDNSBootstrapAndAggregation(t *testing.T) {
	original := tunConnectivityURLs
	tunConnectivityURLs = []string{"http://127.0.0.1:1/aggregation"}
	t.Cleanup(func() { tunConnectivityURLs = original })
	report, err := probeTUNConnectivityThroughChannels(
		context.Background(), "not-a-socks-endpoint",
		dnsResolveResult{Transport: "udp", Server: "not-a-literal-dns-endpoint"},
	)
	if err == nil {
		t.Fatal("expected both channel checks to fail")
	}
	if len(report.Checks) != 2 {
		t.Fatalf("checks = %#v", report.Checks)
	}
	if report.Checks[0].Stage != "dns_bootstrap" || report.Checks[0].Outbound != "system-direct" {
		t.Fatalf("DNS bootstrap check = %#v", report.Checks[0])
	}
	if report.Checks[1].Stage != "aggregation_data" || report.Checks[1].Outbound != "aggregation" {
		t.Fatalf("aggregation check = %#v", report.Checks[1])
	}
	if !strings.Contains(err.Error(), "dns_bootstrap") || !strings.Contains(err.Error(), "aggregation_data") {
		t.Fatalf("combined error lost stage context: %v", err)
	}
}

func TestChannelConnectivityAllowsAnyAggregationHTTPAlternative(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	socksEndpoint := startTestSOCKS5(t)
	dnsEndpoint := startTestDNSUDP(t)
	failedEndpoint := closedTestHTTPURL(t)
	original := tunConnectivityURLs
	tunConnectivityURLs = []string{failedEndpoint, server.URL}
	t.Cleanup(func() { tunConnectivityURLs = original })

	report, err := probeTUNConnectivityThroughChannels(
		context.Background(), socksEndpoint,
		dnsResolveResult{Transport: "udp", Server: dnsEndpoint},
	)
	if err != nil {
		t.Fatalf("one successful aggregation endpoint should pass: %v", err)
	}
	if len(report.Checks) != 3 || !report.Checks[0].OK || report.Checks[1].OK || !report.Checks[2].OK {
		t.Fatalf("unexpected alternative endpoint report: %#v", report.Checks)
	}
	if !strings.Contains(report.summary(), failedEndpoint) {
		t.Fatalf("failed alternative endpoint was not retained: %s", report.summary())
	}
}

func TestChannelConnectivityRequiresDNSBootstrapEvenWhenHTTPWorks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	socksEndpoint := startTestSOCKS5(t)
	original := tunConnectivityURLs
	tunConnectivityURLs = []string{server.URL}
	t.Cleanup(func() { tunConnectivityURLs = original })

	report, err := probeTUNConnectivityThroughChannels(
		context.Background(), socksEndpoint,
		dnsResolveResult{Transport: "udp", Server: "not-a-literal-dns-endpoint"},
	)
	if err == nil || report.Checks[0].OK || !report.Checks[1].OK {
		t.Fatalf("DNS bootstrap must be required: report=%#v err=%v", report.Checks, err)
	}
	if !strings.Contains(err.Error(), "dns_bootstrap") {
		t.Fatalf("DNS bootstrap failure lost stage context: %v", err)
	}
}

func TestChannelConnectivityReportsAllFailedAggregationEndpoints(t *testing.T) {
	socksEndpoint := startTestSOCKS5(t)
	dnsEndpoint := startTestDNSUDP(t)
	first := closedTestHTTPURL(t)
	second := closedTestHTTPURL(t)
	original := tunConnectivityURLs
	tunConnectivityURLs = []string{first, second}
	t.Cleanup(func() { tunConnectivityURLs = original })

	report, err := probeTUNConnectivityThroughChannels(
		context.Background(), socksEndpoint,
		dnsResolveResult{Transport: "udp", Server: dnsEndpoint},
	)
	if err == nil || !report.Checks[0].OK {
		t.Fatalf("all aggregation endpoints should fail: report=%#v err=%v", report.Checks, err)
	}
	if !strings.Contains(err.Error(), first) || !strings.Contains(err.Error(), second) {
		t.Fatalf("aggregation failure lost an endpoint: %v", err)
	}
}

func TestAggregationConnectivityUsesSOCKSDataChannel(t *testing.T) {
	hosts := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		hosts <- request.Host
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	socksEndpoint := startTestSOCKS5(t)
	endpoint := strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
	check := probeHTTPURL(context.Background(), endpoint, &socksEndpoint, "aggregation")
	if !check.OK || check.Outbound != "aggregation" {
		t.Fatalf("aggregation SOCKS check = %#v", check)
	}
	if host := <-hosts; !strings.HasPrefix(host, "localhost:") {
		t.Fatalf("aggregation probe lost original HTTP Host header: %q", host)
	}
}

func startTestSOCKS5(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go serveTestSOCKS5(connection)
		}
	}()
	return listener.Addr().String()
}

func startTestDNSUDP(t *testing.T) string {
	t.Helper()
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		query := make([]byte, 4096)
		count, address, readErr := listener.ReadFromUDP(query)
		if readErr != nil || count < 2 {
			return
		}
		response := make([]byte, 12)
		response[0], response[1] = query[0], query[1]
		response[2], response[3] = 0x81, 0x80
		response[4], response[5] = 0, 1
		_, _ = listener.WriteToUDP(response, address)
	}()
	return listener.LocalAddr().String()
}

func closedTestHTTPURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return "http://" + address + "/failed"
}

func serveTestSOCKS5(connection net.Conn) {
	defer connection.Close()
	reader := bufio.NewReader(connection)
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil || header[0] != 5 {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}
	if _, err := connection.Write([]byte{5, 0}); err != nil {
		return
	}
	request := make([]byte, 4)
	if _, err := io.ReadFull(reader, request); err != nil || request[0] != 5 || request[1] != 1 {
		return
	}
	var host string
	switch request[3] {
	case 1:
		address := make([]byte, 4)
		if _, err := io.ReadFull(reader, address); err != nil {
			return
		}
		host = net.IP(address).String()
	case 3:
		length, err := reader.ReadByte()
		if err != nil {
			return
		}
		address := make([]byte, int(length))
		if _, err := io.ReadFull(reader, address); err != nil {
			return
		}
		host = string(address)
	case 4:
		address := make([]byte, 16)
		if _, err := io.ReadFull(reader, address); err != nil {
			return
		}
		host = net.IP(address).String()
	default:
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBytes); err != nil {
		return
	}
	target, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(portBytes)))))
	if err != nil {
		_, _ = connection.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer target.Close()
	local := target.LocalAddr().(*net.TCPAddr)
	reply := []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	copy(reply[4:8], local.IP.To4())
	binary.BigEndian.PutUint16(reply[8:], uint16(local.Port))
	if _, err := connection.Write(reply); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(target, reader); done <- struct{}{} }()
	go func() { _, _ = io.Copy(connection, target); done <- struct{}{} }()
	<-done
}
