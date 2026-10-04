//go:build windows

package wfp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Opt-in: creates only dynamic, process/source/interface-scoped DNS filters.
// It neither changes routes nor disables filters belonging to another program.
func TestRealIPv6DNSExemptionLifecycle(t *testing.T) {
	if os.Getenv("HYPOMUX_RUN_WFP_IPV6_NETWORK_TEST") != "1" {
		t.Skip("enable the elevated physical IPv6 WFP acceptance explicitly")
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Fatal("real WFP registration requires an elevated test process")
	}
	var cfg struct {
		Adapter struct {
			Name        string `json:"name"`
			SourceIPv6  string `json:"source_ipv6"`
			IPv6IfIndex uint32 `json:"ipv6_if_index"`
		} `json:"adapter"`
		IPv6UDP string `json:"ipv6_udp"`
	}
	if err := json.Unmarshal([]byte(os.Getenv("HYPOMUX_IPV6_ACCEPTANCE_CONFIG")), &cfg); err != nil {
		t.Fatal(err)
	}
	source := net.ParseIP(cfg.Adapter.SourceIPv6)
	iface, err := net.InterfaceByName(cfg.Adapter.Name)
	if err != nil || source == nil || source.To4() != nil || cfg.Adapter.IPv6IfIndex == 0 {
		t.Fatalf("invalid physical IPv6 adapter: %v", err)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		t.Fatal(err)
	}
	owned := false
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		owned = owned || (err == nil && ip.Equal(source))
	}
	if !owned || source.IsLoopback() || source.IsLinkLocalUnicast() {
		t.Fatal("the selected physical adapter does not own this IPv6 source")
	}
	host, port, err := net.SplitHostPort(cfg.IPv6UDP)
	if err != nil || net.ParseIP(host) == nil || net.ParseIP(host).To4() != nil || port != "53" {
		t.Fatal("provide a literal IPv6 DNS endpoint on port 53")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	session, err := OpenDNSExemption(executable, []Adapter{{Name: cfg.Adapter.Name, SourceIPv6: source.String(), IPv6IfIndex: cfg.Adapter.IPv6IfIndex}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ids := session.FilterIDs()
	if len(ids) != 2 {
		t.Fatalf("native IPv6 TCP/UDP filters=%v, want two", ids)
	}
	api := newAPI()
	var observer windows.Handle
	if err := api.call(api.engineOpen, "FwpmEngineOpen0", 0, uintptr(rpcCAuthnDefault), 0, 0, uintptr(unsafe.Pointer(&observer))); err != nil {
		t.Fatal(err)
	}
	defer api.engineClose.Call(uintptr(observer))
	get := windows.NewLazySystemDLL("fwpuclnt.dll").NewProc("FwpmFilterGetById0")
	for _, id := range ids {
		var entry *filter
		status, _, _ := get.Call(uintptr(observer), uintptr(id), uintptr(unsafe.Pointer(&entry)))
		if status != 0 || entry == nil {
			t.Fatalf("native filter %d missing: 0x%08x", id, status)
		}
		valid := entry.LayerKey == layerALEAuthConnectV6 && entry.Action.Type == fwpActionPermit && entry.NumFilterConditions == 5
		if valid {
			foundSource, foundInterface := false, false
			for _, condition := range unsafe.Slice(entry.FilterCondition, entry.NumFilterConditions) {
				if condition.FieldKey == conditionLocalAddress {
					foundSource = true
					if condition.ConditionValue.Type != fwpByteArray16Type {
						valid = false
						continue
					}
					// This native union member is an FWP_BYTE_ARRAY16 pointer.
					address := *(**[16]byte)(unsafe.Pointer(&condition.ConditionValue.Value))
					valid = valid && address != nil && *address == [16]byte(source.To16())
				}
				if condition.FieldKey == conditionInterface {
					foundInterface = true
					valid = valid && condition.ConditionValue.Type == fwpUint32 && uint32(condition.ConditionValue.Value) == cfg.Adapter.IPv6IfIndex
				}
			}
			valid = valid && foundSource && foundInterface
		}
		api.free(uintptr(unsafe.Pointer(&entry)))
		if !valid {
			t.Fatalf("native IPv6 filter %d has incorrect layer/source/interface constraints", id)
		}
	}
	t.Logf("registered real IPv6 TCP/UDP WFP filters=%v adapter=%s", ids, cfg.Adapter.Name)
	for _, network := range []string{"udp6", "tcp6"} {
		t.Run(network, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			dialer := net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
				var optionErr error
				if err := raw.Control(func(fd uintptr) {
					optionErr = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, 31, int(cfg.Adapter.IPv6IfIndex))
				}); err != nil {
					return err
				}
				return optionErr
			}}
			if network == "udp6" {
				dialer.LocalAddr = &net.UDPAddr{IP: source}
			} else {
				dialer.LocalAddr = &net.TCPAddr{IP: source}
			}
			connection, err := dialer.DialContext(ctx, network, cfg.IPv6UDP)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			connection.SetDeadline(time.Now().Add(5 * time.Second))
			query := []byte{0x64, 0x06, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 3, 'd', 'n', 's', 6, 'a', 'l', 'i', 'd', 'n', 's', 3, 'c', 'o', 'm', 0, 0, 28, 0, 1}
			wire := query
			if network == "tcp6" {
				wire = append([]byte{0, byte(len(query))}, query...)
			}
			if _, err := connection.Write(wire); err != nil {
				t.Fatal(err)
			}
			buffer := make([]byte, 4096)
			var n int
			if network == "tcp6" {
				var size [2]byte
				if _, err := io.ReadFull(connection, size[:]); err != nil {
					t.Fatal(err)
				}
				n = int(binary.BigEndian.Uint16(size[:]))
				if n > len(buffer) {
					t.Fatal("DNS response exceeds acceptance buffer")
				}
				_, err = io.ReadFull(connection, buffer[:n])
			} else {
				n, err = connection.Read(buffer)
			}
			if err != nil || n < 12 || binary.BigEndian.Uint16(buffer) != 0x6406 || buffer[2]&0x80 == 0 || buffer[3]&0xf != 0 || binary.BigEndian.Uint16(buffer[6:]) == 0 {
				t.Fatalf("real %s DNS reply invalid: bytes=%d error=%v", network, n, err)
			}
			t.Logf("verified source=%s peer=%s response_bytes=%d", connection.LocalAddr(), connection.RemoteAddr(), n)
		})
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		var entry *filter
		status, _, _ := get.Call(uintptr(observer), uintptr(id), uintptr(unsafe.Pointer(&entry)))
		if entry != nil {
			api.free(uintptr(unsafe.Pointer(&entry)))
		}
		if status != uintptr(windows.FWP_E_FILTER_NOT_FOUND) {
			t.Fatalf("dynamic filter %d not removed after Close: 0x%08x", id, status)
		}
	}
	t.Log("verified dynamic IPv6 filters removed after Close")
}

// This checks the OS resource lifetime independently of public connectivity.
// The owned helper permits DNS only from the actual IPv6 loopback interface.
func TestRealIPv6DNSExemptionCrashCleanup(t *testing.T) {
	if os.Getenv("HYPOMUX_RUN_WFP_IPV6_NETWORK_TEST") != "1" {
		t.Skip("enable the elevated native IPv6 WFP acceptance explicitly")
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Fatal("real WFP registration requires an elevated test process")
	}
	if os.Getenv("HYPOMUX_WFP_CRASH_HELPER") == "1" {
		session, err := OpenDNSExemption("", []Adapter{{Name: "loopback", SourceIPv6: "::1", IPv6IfIndex: 1}})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		data, _ := json.Marshal(session.FilterIDs())
		fmt.Println(string(data))
		select {} // The parent intentionally terminates this owned process.
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestRealIPv6DNSExemptionCrashCleanup$", "-test.timeout=30s")
	command.Env = append(os.Environ(), "HYPOMUX_WFP_CRASH_HELPER=1")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = command.Wait() }()
	var ids []uint64
	if err := json.NewDecoder(output).Decode(&ids); err != nil || len(ids) != 2 {
		t.Fatalf("owned WFP helper did not report two filters: %v %v", ids, err)
	}
	api := newAPI()
	var observer windows.Handle
	if err := api.call(api.engineOpen, "FwpmEngineOpen0", 0, uintptr(rpcCAuthnDefault), 0, 0, uintptr(unsafe.Pointer(&observer))); err != nil {
		t.Fatal(err)
	}
	defer api.engineClose.Call(uintptr(observer))
	get := windows.NewLazySystemDLL("fwpuclnt.dll").NewProc("FwpmFilterGetById0")
	lookup := func(id uint64) uintptr {
		var entry *filter
		status, _, _ := get.Call(uintptr(observer), uintptr(id), uintptr(unsafe.Pointer(&entry)))
		if entry != nil {
			api.free(uintptr(unsafe.Pointer(&entry)))
		}
		return status
	}
	for _, id := range ids {
		if status := lookup(id); status != 0 {
			t.Fatalf("live helper filter %d missing: 0x%08x", id, status)
		}
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	for _, id := range ids {
		deadline := time.Now().Add(3 * time.Second)
		for {
			status := lookup(id)
			if status == uintptr(windows.FWP_E_FILTER_NOT_FOUND) {
				break
			}
			if status != 0 || time.Now().After(deadline) {
				t.Fatalf("filter %d survived owned helper crash: 0x%08x", id, status)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	t.Logf("verified OS removed IPv6 filters=%v after owned helper PID=%d was terminated", ids, command.Process.Pid)
}
