package proxy

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

func TestTCPTuningPolicy(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		config      Config
		channel     string
		wantTune    bool
		wantProfile string
	}{
		{
			name: "single adapter remains baseline",
			config: Config{Adapters: []Adapter{
				{Name: "one", SourceIP: "127.0.0.1"},
			}},
			wantProfile: "baseline",
		},
		{
			name: "ordinary multi-adapter proxy uses aggregation profile",
			config: Config{Adapters: []Adapter{
				{Name: "one", SourceIP: "127.0.0.1"},
				{Name: "two", SourceIP: "127.0.0.2"},
			}},
			wantTune:    true,
			wantProfile: "aggregation-1m",
		},
		{
			name:        "dedicated TUN channel remains baseline",
			config:      twoAdapterChannelConfig(),
			channel:     ChannelEthernet,
			wantProfile: "aggregation-1m",
		},
		{
			name:        "TUN aggregation channel is tuned",
			config:      twoAdapterChannelConfig(),
			channel:     ChannelAggregation,
			wantTune:    true,
			wantProfile: "aggregation-1m",
		},
		{
			name:        "off restores baseline",
			environment: "off",
			config:      twoAdapterChannelConfig(),
			channel:     ChannelAggregation,
			wantProfile: "baseline",
		},
		{
			name:        "force tunes direct diagnostics",
			environment: "force",
			config:      twoAdapterChannelConfig(),
			channel:     ChannelDirect,
			wantTune:    true,
			wantProfile: "forced-1m",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HYPOMUX_TCP_TUNING", test.environment)
			server, err := New(test.config)
			if err != nil {
				t.Fatal(err)
			}
			if got := server.shouldTuneTCP(test.channel); got != test.wantTune {
				t.Fatalf("shouldTuneTCP(%q) = %v, want %v", test.channel, got, test.wantTune)
			}
			if got := server.tcpProfileName(); got != test.wantProfile {
				t.Fatalf("tcpProfileName() = %q, want %q", got, test.wantProfile)
			}
			if got := server.Snapshot(false).TCPProfile; got != test.wantProfile {
				t.Fatalf("telemetry profile = %q, want %q", got, test.wantProfile)
			}
		})
	}
}

func TestReaderOnlyKeepsCopyBufferInControl(t *testing.T) {
	source := &writerToReader{Reader: bytes.NewBufferString("payload")}
	buffer := make([]byte, 3)
	var destination bytes.Buffer
	if _, err := io.CopyBuffer(&destination, readerOnly{Reader: source}, buffer); err != nil {
		t.Fatal(err)
	}
	if source.writeToCalled {
		t.Fatal("underlying WriterTo bypassed the relay buffer")
	}
	if destination.String() != "payload" {
		t.Fatalf("copied payload = %q", destination.String())
	}
}

func TestTuneTCPConnectionIsBestEffortForNonTCPConnections(t *testing.T) {
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	tuneTCPConnection(client)
}

func TestRelayPreservesLargePayloadsAndHalfClose(t *testing.T) {
	clientApplication, proxyClient := tcpConnectionPair(t)
	proxyUpstream, upstreamApplication := tcpConnectionPair(t)
	defer clientApplication.Close()
	defer proxyClient.Close()
	defer proxyUpstream.Close()
	defer upstreamApplication.Close()
	for _, connection := range []*net.TCPConn{clientApplication, proxyClient, proxyUpstream, upstreamApplication} {
		_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	}

	server, err := New(Config{Adapters: []Adapter{{Name: "loopback", SourceIP: "127.0.0.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	session := server.registry.Begin("test", "", proxyClient)
	server.registry.Attach(session, proxyUpstream, "loopback", server.config.Adapters[0])
	done := make(chan struct{})
	go func() {
		server.relay(bufio.NewReaderSize(proxyClient, 64*1024), proxyClient, proxyUpstream, session)
		close(done)
	}()

	upload := bytes.Repeat([]byte("upstream-payload-"), 256*1024)
	uploadError := make(chan error, 1)
	go func() {
		_, err := clientApplication.Write(upload)
		if err == nil {
			err = clientApplication.CloseWrite()
		}
		uploadError <- err
	}()
	receivedUpload, err := io.ReadAll(upstreamApplication)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-uploadError; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(receivedUpload, upload) {
		t.Fatalf("upload size = %d, want %d", len(receivedUpload), len(upload))
	}

	download := bytes.Repeat([]byte("downstream-payload-"), 192*1024)
	downloadError := make(chan error, 1)
	go func() {
		_, err := upstreamApplication.Write(download)
		if err == nil {
			err = upstreamApplication.CloseWrite()
		}
		downloadError <- err
	}()
	receivedDownload, err := io.ReadAll(clientApplication)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-downloadError; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(receivedDownload, download) {
		t.Fatalf("download size = %d, want %d", len(receivedDownload), len(download))
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay did not finish after both half-closes")
	}
	if session.bytesUp.Load() != uint64(len(upload)) || session.bytesDown.Load() != uint64(len(download)) {
		t.Fatalf("relay accounting up=%d down=%d", session.bytesUp.Load(), session.bytesDown.Load())
	}
}

type writerToReader struct {
	io.Reader
	writeToCalled bool
}

func (r *writerToReader) WriteTo(writer io.Writer) (int64, error) {
	r.writeToCalled = true
	return io.Copy(writer, r.Reader)
}

func twoAdapterChannelConfig() Config {
	return Config{
		Adapters: []Adapter{
			{Name: "wired", SourceIP: "127.0.0.1"},
			{Name: "wireless", SourceIP: "127.0.0.2"},
		},
		Channels: []Channel{
			{Name: ChannelEthernet, AdapterNames: []string{"wired"}},
			{Name: ChannelWiFi, AdapterNames: []string{"wireless"}},
			{Name: ChannelAggregation, AdapterNames: []string{"wired", "wireless"}},
			{Name: ChannelDirect},
		},
	}
}

func tcpConnectionPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan *net.TCPConn, 1)
	acceptError := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptTCP()
		if err != nil {
			acceptError <- err
			return
		}
		accepted <- connection
	}()
	client, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case server := <-accepted:
		return client, server
	case err := <-acceptError:
		client.Close()
		t.Fatal(err)
	}
	return nil, nil
}
