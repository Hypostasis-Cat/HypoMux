//go:build windows

package services

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func readMTU(ctx context.Context, id string) (MTUInfo, error) {
	iface, err := net.InterfaceByName(id)
	if err != nil {
		return MTUInfo{}, err
	}
	if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || isHypoMuxManagedAdapter(id) {
		return MTUInfo{}, errors.New("请选择已连接的外部网卡")
	}
	info := MTUInfo{AdapterID: id, IfIndex: iface.Index}
	addresses, err := iface.Addrs()
	if err != nil {
		return info, err
	}
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil && !ip.IsLinkLocalUnicast() && !ip.IsLoopback() {
			info.Address = ip.String()
			break
		}
	}
	if info.Address == "" {
		return info, errors.New("所选网卡没有可用的 IPv4 地址")
	}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return info, err
	}
	// Only a resolved integer is interpolated, never user-supplied shell text.
	script := `$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest; [Console]::OutputEncoding=[Text.UTF8Encoding]::new($false); $i=` + strconv.Itoa(iface.Index) + `; $a=Get-NetAdapter -InterfaceIndex $i; $n=Get-NetIPInterface -InterfaceIndex $i -AddressFamily IPv4 -PolicyStore ActiveStore; @{guid=([guid]$a.InterfaceGuid).ToString(); current=[int]$n.NlMtu} | ConvertTo-Json -Compress`
	cmd := exec.CommandContext(ctx, filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script)
	configureBackgroundCommand(cmd)
	data, err := cmd.Output()
	if err != nil {
		return info, fmt.Errorf("读取网卡 MTU 失败：%w", err)
	}
	if err = json.Unmarshal(data, &info); err != nil {
		return info, err
	}
	if strings.TrimSpace(info.GUID) == "" || info.Current < 576 || info.Current > 65535 {
		return info, errors.New("网卡未返回有效的 IPv4 MTU 或标识")
	}
	return info, nil
}

func probeMTU(ctx context.Context, source, target string, size int) (bool, error) {
	handle, _, err := icmpCreateFileProc.Call()
	if handle == 0 || handle == ^uintptr(0) {
		return false, fmt.Errorf("创建 ICMP 探针失败：%v", err)
	}
	defer icmpCloseHandleProc.Call(handle)
	payload := make([]byte, size-28)
	reply := make([]byte, int(unsafe.Sizeof(icmpEchoReply{}))+len(payload)+16)
	options := struct {
		TTL, TOS, Flags, OptionsSize byte
		OptionsData                  uintptr
	}{TTL: 128, Flags: 2}
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		clear(reply)
		count, _, callErr := icmpSendEcho2ExProc.Call(handle, 0, 0, 0,
			uintptr(binary.LittleEndian.Uint32(net.ParseIP(source).To4())), uintptr(binary.LittleEndian.Uint32(net.ParseIP(target).To4())),
			uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)), uintptr(unsafe.Pointer(&options)), uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), 800)
		runtime.KeepAlive(payload)
		runtime.KeepAlive(options)
		status := uint32(0)
		if count > 0 {
			status = (*icmpEchoReply)(unsafe.Pointer(&reply[0])).Status
		} else if errno, ok := callErr.(syscall.Errno); ok {
			status = uint32(errno)
		} else {
			return false, fmt.Errorf("ICMP 调用失败：%v", callErr)
		}
		if count > 0 && status == 0 {
			return true, nil
		}
		if status == 11009 {
			return false, nil
		} // IP_PACKET_TOO_BIG
		if status != 11010 {
			return false, fmt.Errorf("ICMP 探测失败（Windows %d），无法判断 MTU", status)
		}
	}
	return false, errors.New("目标未回应 ICMP 探测，可能存在丢包或过滤；无法给出可靠 MTU，请更换目标或重试")
}
