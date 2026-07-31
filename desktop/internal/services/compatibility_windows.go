//go:build windows

package services

import (
	"net"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func detectCompatibilityPlan() compatibilityPlan {
	known := make(map[string]struct{}, len(compatibilityProcessNames))
	for _, name := range compatibilityProcessNames {
		known[strings.ToLower(name)] = struct{}{}
	}
	paths := []string{}
	detected := []compatibilityDetection{}

	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err == nil {
		defer windows.CloseHandle(snapshot)
		entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
		for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
			name := windows.UTF16ToString(entry.ExeFile[:])
			if _, ok := known[strings.ToLower(name)]; !ok {
				continue
			}
			path := processImageName(entry.ProcessID)
			if path != "" {
				paths = append(paths, path)
			}
			detected = append(detected, compatibilityDetection{Name: name, Path: path, Source: "known-profile"})
		}
	}

	ports := currentLoopbackProxyPorts()
	if len(ports) > 0 {
		seenPID := map[uint32]struct{}{}
		for _, row := range tcpOwnerRows() {
			if row.State != 2 {
				continue
			}
			if _, wanted := ports[networkPort(row.LocalPort)]; !wanted {
				continue
			}
			if _, exists := seenPID[row.OwningPID]; exists {
				continue
			}
			seenPID[row.OwningPID] = struct{}{}
			path := processImageName(row.OwningPID)
			if path == "" {
				continue
			}
			paths = append(paths, path)
			detected = append(detected, compatibilityDetection{
				Name: filepathBase(path), Path: path, Source: "windows-system-proxy-listener",
			})
		}
	}
	return normalizedCompatibilityPlan(paths, detected)
}

func currentLoopbackProxyPorts() map[uint16]struct{} {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return nil
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return nil
	}
	ports := map[uint16]struct{}{}
	for _, part := range strings.Split(server, ";") {
		endpoint := strings.TrimSpace(part)
		if _, value, found := strings.Cut(endpoint, "="); found {
			endpoint = value
		}
		host, portText, splitErr := net.SplitHostPort(endpoint)
		if splitErr != nil || (host != "127.0.0.1" && !strings.EqualFold(host, "localhost")) {
			continue
		}
		port, parseErr := strconv.ParseUint(portText, 10, 16)
		if parseErr == nil && port > 0 {
			ports[uint16(port)] = struct{}{}
		}
	}
	return ports
}

func filepathBase(path string) string {
	if index := strings.LastIndexAny(path, `\/`); index >= 0 {
		return path[index+1:]
	}
	return path
}
