//go:build windows

package wfp

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/bits"
	"net"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
	"golang.org/x/sys/windows"
)

// An actual Windows denial scoped to this test executable, source and interface.
// Other processes, including the user's existing VPN, keep their original policy.
func TestRealDualStackTCPFaultFallback(t *testing.T) {
	if os.Getenv("HYPOMUX_RUN_WFP_IPV6_NETWORK_TEST") != "1" {
		t.Skip("enable the elevated, process-scoped dual-stack fault acceptance explicitly")
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Fatal("native fault injection requires elevation")
	}
	var cfg struct {
		Adapter proxy.Adapter `json:"adapter"`
	}
	if err := json.Unmarshal([]byte(os.Getenv("HYPOMUX_IPV6_ACCEPTANCE_CONFIG")), &cfg); err != nil {
		t.Fatal(err)
	}
	a := cfg.Adapter
	if net.ParseIP(a.SourceIP).To4() == nil || net.ParseIP(a.SourceIPv6).To16() == nil || net.ParseIP(a.SourceIPv6).To4() != nil || a.IfIndex <= 0 || a.IPv6IfIndex <= 0 {
		t.Fatal("provide both physical source addresses and their interface indices")
	}
	targets := map[string]string{"tcp4": "223.5.5.5:443", "tcp6": "[2400:3200::1]:443"}
	dial := func(network string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		source, index, level := a.SourceIP, bits.ReverseBytes32(uint32(a.IfIndex)), windows.IPPROTO_IP
		if network == "tcp6" {
			source, index, level = a.SourceIPv6, uint32(a.IPv6IfIndex), windows.IPPROTO_IPV6
		}
		d := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(source)}, Control: func(_, _ string, raw syscall.RawConn) error {
			var optionErr error
			err := raw.Control(func(fd uintptr) { optionErr = windows.SetsockoptInt(windows.Handle(fd), level, 31, int(index)) })
			if err != nil {
				return err
			}
			return optionErr
		}}
		return d.DialContext(ctx, network, targets[network])
	}
	// Prove both public paths work before creating any fault.
	for _, network := range []string{"tcp4", "tcp6"} {
		connection, err := dial(network)
		if err != nil {
			t.Fatalf("%s baseline: %v", network, err)
		}
		connection.SetDeadline(time.Now().Add(8 * time.Second))
		client := tls.Client(connection, &tls.Config{ServerName: "dns.alidns.com", MinVersion: tls.VersionTLS12})
		err = client.Handshake()
		connection.Close()
		if err != nil {
			t.Fatalf("%s baseline certificate: %v", network, err)
		}
		t.Logf("verified physical %s TLS baseline", network)
	}
	for _, blocked := range []string{"tcp4", "tcp6"} {
		t.Run("blocked_"+blocked, func(t *testing.T) {
			exemption, err := OpenDNSExemption("", []Adapter{{Name: a.Name, SourceIP: a.SourceIP, IfIndex: uint32(a.IfIndex), SourceIPv6: a.SourceIPv6, IPv6IfIndex: uint32(a.IPv6IfIndex)}})
			if err != nil {
				t.Fatal(err)
			}
			defer exemption.Close()
			owned := exemption.(*dnsSession)
			addAcceptanceTCPBlock(t, owned, a, blocked)
			connection, err := dial(blocked)
			if connection != nil {
				connection.Close()
			}
			if err == nil {
				t.Fatal("native family fault did not deny the bound test socket")
			}
			t.Logf("verified process-scoped %s denial: %v", blocked, err)
			server, err := proxy.New(proxy.Config{Adapters: []proxy.Adapter{a}, DNS: dns.Config{Policy: dns.PolicyAliDNS}})
			if err != nil {
				t.Fatal(err)
			}
			endpoints, err := server.Start()
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := server.Stop(ctx); err != nil {
					t.Error(err)
				}
			}()
			connection, err = net.DialTimeout("tcp", endpoints.SOCKS, 3*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			connection.SetDeadline(time.Now().Add(20 * time.Second))
			connection.Write([]byte{5, 1, 0})
			var greeting [2]byte
			if _, err := io.ReadFull(connection, greeting[:]); err != nil || greeting != [2]byte{5, 0} {
				t.Fatalf("SOCKS greeting %v: %v", greeting, err)
			}
			host := "dns.alidns.com"
			request := append([]byte{5, 1, 0, 3, byte(len(host))}, host...)
			request = append(request, 1, 187)
			connection.Write(request)
			var reply [4]byte
			if _, err := io.ReadFull(connection, reply[:]); err != nil || reply[0] != 5 || reply[1] != 0 {
				t.Fatalf("SOCKS fallback reply %v: %v", reply, err)
			}
			size := 4
			if reply[3] == 4 {
				size = 16
			} else if reply[3] != 1 {
				t.Fatalf("unexpected SOCKS reply family %d", reply[3])
			}
			if _, err := io.ReadFull(connection, make([]byte, size+2)); err != nil {
				t.Fatal(err)
			}
			client := tls.Client(connection, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
			if err := client.Handshake(); err != nil {
				t.Fatal(err)
			}
			flows := server.Snapshot(true).Connections
			if len(flows) != 1 || flows[0].Adapter != a.Name {
				t.Fatalf("fallback escaped selected adapter: %+v", flows)
			}
			peer, _, err := net.SplitHostPort(flows[0].Remote)
			if err != nil || (net.ParseIP(peer).To4() != nil) != (blocked == "tcp6") {
				t.Fatalf("fallback used blocked family: %+v", flows)
			}
			t.Logf("verified TLS fallback blocked=%s remote=%s adapter=%s", blocked, flows[0].Remote, flows[0].Adapter)
			if err := exemption.Close(); err != nil {
				t.Fatal(err)
			}
			// Verify the formerly denied physical path immediately recovers.
			recovered, err := dial(blocked)
			if err != nil {
				t.Fatalf("physical family did not recover after owned filter removal: %v", err)
			}
			recovered.Close()
		})
	}
}

func addAcceptanceTCPBlock(t *testing.T, owned *dnsSession, a proxy.Adapter, network string) {
	t.Helper()
	api := newAPI()
	get := windows.NewLazySystemDLL("fwpuclnt.dll").NewProc("FwpmFilterGetById0")
	var existing *filter
	status, _, _ := get.Call(uintptr(owned.engine), uintptr(owned.FilterIDs()[0]), uintptr(unsafe.Pointer(&existing)))
	if status != 0 || existing == nil {
		t.Fatalf("locate owned dynamic sublayer: 0x%08x", status)
	}
	subLayer := existing.SubLayerKey
	api.free(uintptr(unsafe.Pointer(&existing)))
	executable, _ := os.Executable()
	path, _ := windows.UTF16PtrFromString(executable)
	var app *byteBlob
	if err := api.call(api.getAppID, "FwpmGetAppIdFromFileName0", uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&app))); err != nil {
		t.Fatal(err)
	}
	defer api.free(uintptr(unsafe.Pointer(&app)))
	conditions := []filterCondition{
		makeCondition(conditionALEAppID, fwpByteBlobType, uintptr(unsafe.Pointer(app))),
		makeCondition(conditionLocalAddress, fwpUint32, uintptr(binary.BigEndian.Uint32(net.ParseIP(a.SourceIP).To4()))),
		makeCondition(conditionInterface, fwpUint32, uintptr(a.IfIndex)),
		makeCondition(conditionIPProtocol, fwpUint8, uintptr(ipProtoTCP)),
		makeCondition(conditionRemotePort, fwpUint16, 443),
	}
	layer := layerALEAuthConnectV4
	var source6 [16]byte
	if network == "tcp6" {
		layer = layerALEAuthConnectV6
		copy(source6[:], net.ParseIP(a.SourceIPv6).To16())
		conditions[1] = makeCondition(conditionLocalAddress, fwpByteArray16Type, uintptr(unsafe.Pointer(&source6[0])))
		conditions[2] = makeCondition(conditionInterface, fwpUint32, uintptr(a.IPv6IfIndex))
	}
	key, err := windows.GenerateGUID()
	if err != nil {
		t.Fatal(err)
	}
	name, _ := windows.UTF16PtrFromString("HypoMux owned acceptance TCP fault")
	entry := filter{FilterKey: key, DisplayData: displayData{Name: name}, LayerKey: layer, SubLayerKey: subLayer,
		Weight: value{Type: fwpUint8, Value: 15}, NumFilterConditions: uint32(len(conditions)), FilterCondition: &conditions[0],
		Action: action{Type: 0x1001}} // FWP_ACTION_BLOCK, in this dynamic test session only.
	var id uint64
	err = api.call(api.filterAdd, "FwpmFilterAdd0", uintptr(owned.engine), uintptr(unsafe.Pointer(&entry)), 0, uintptr(unsafe.Pointer(&id)))
	runtime.KeepAlive(conditions)
	runtime.KeepAlive(source6)
	if err != nil {
		t.Fatal(err)
	}
	owned.mu.Lock()
	owned.filterIDs = append(owned.filterIDs, id)
	owned.mu.Unlock()
	t.Logf("registered owned %s fault filter=%d", network, id)
}
