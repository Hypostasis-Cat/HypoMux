//go:build darwin

package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const networkSetupPath = "/usr/sbin/networksetup"

var darwinOwnedBypass = []string{
	"localhost", "127.0.0.1", "*.local", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
}

type darwinProxyEndpoint struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server"`
	Port    int    `json:"port"`
}

type darwinServiceProxy struct {
	Name      string              `json:"name"`
	Web       darwinProxyEndpoint `json:"web"`
	SecureWeb darwinProxyEndpoint `json:"secure_web"`
	SOCKS     darwinProxyEndpoint `json:"socks"`
	Bypass    []string            `json:"bypass"`
}

type darwinProxySnapshot struct {
	Version   int                  `json:"version"`
	State     string               `json:"state"`
	HTTPPort  int                  `json:"http_port"`
	SOCKSPort int                  `json:"socks_port"`
	Services  []darwinServiceProxy `json:"services"`
}

func proxyMarkerPath() string {
	return filepath.Join(settingsDirectory(), "proxy-owned-macos.json")
}

func enableSystemProxy(httpPort int, socksPort int) error {
	if _, err := restoreSystemProxyDetailed(); err != nil {
		return fmt.Errorf("启用代理前恢复上次状态失败：%w", err)
	}
	services, err := readDarwinProxyServices()
	if err != nil {
		return err
	}
	if len(services) == 0 {
		return errors.New("没有找到可配置的 macOS 网络服务")
	}
	snapshot := darwinProxySnapshot{
		Version: 1, State: "prepared", HTTPPort: httpPort, SOCKSPort: socksPort, Services: services,
	}
	if err := writeDarwinProxySnapshot(snapshot); err != nil {
		return err
	}
	commands := make([][]string, 0, len(services)*7)
	for _, service := range services {
		commands = append(commands,
			[]string{"-setwebproxy", service.Name, "127.0.0.1", strconv.Itoa(httpPort), "off"},
			[]string{"-setwebproxystate", service.Name, "on"},
			[]string{"-setsecurewebproxy", service.Name, "127.0.0.1", strconv.Itoa(httpPort), "off"},
			[]string{"-setsecurewebproxystate", service.Name, "on"},
			[]string{"-setsocksfirewallproxy", service.Name, "127.0.0.1", strconv.Itoa(socksPort), "off"},
			[]string{"-setsocksfirewallproxystate", service.Name, "on"},
			append([]string{"-setproxybypassdomains", service.Name}, darwinOwnedBypass...),
		)
	}
	if err := runNetworkSetupChanges(commands); err != nil {
		if _, rollbackErr := recoverPreparedDarwinSnapshot(snapshot); rollbackErr != nil {
			return fmt.Errorf("设置 macOS 系统代理失败：%v；自动回滚也失败：%w", err, rollbackErr)
		}
		return fmt.Errorf("设置 macOS 系统代理失败，已恢复原设置：%w", err)
	}
	snapshot.State = "active"
	if err := writeDarwinProxySnapshot(snapshot); err != nil {
		return fmt.Errorf("提交系统代理恢复点失败：%w", err)
	}
	return nil
}

func restoreSystemProxy() error {
	_, err := restoreSystemProxyDetailed()
	return err
}

func restoreSystemProxyDetailed() (string, error) {
	data, err := os.ReadFile(proxyMarkerPath())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取 macOS 代理恢复点失败：%w", err)
	}
	var snapshot darwinProxySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return "", fmt.Errorf("macOS 代理恢复点已损坏：%w", err)
	}
	if snapshot.State == "prepared" {
		return recoverPreparedDarwinSnapshot(snapshot)
	}
	if snapshot.State != "active" {
		return "", fmt.Errorf("无法识别 macOS 代理恢复点状态 %q", snapshot.State)
	}
	current, err := readDarwinProxyServices()
	if err != nil {
		return "", err
	}
	currentByName := make(map[string]darwinServiceProxy, len(current))
	for _, service := range current {
		currentByName[service.Name] = service
	}
	for _, original := range snapshot.Services {
		value, ok := currentByName[original.Name]
		if !ok {
			continue
		}
		if !isOwnedDarwinProxy(value, snapshot.HTTPPort, snapshot.SOCKSPort) {
			if err := os.Remove(proxyMarkerPath()); err != nil && !os.IsNotExist(err) {
				return "", err
			}
			return "检测到系统代理已由用户或其他软件修改，HypoMux 未覆盖该设置", nil
		}
	}
	commands := make([][]string, 0, len(snapshot.Services)*7)
	for _, service := range snapshot.Services {
		if _, exists := currentByName[service.Name]; !exists {
			continue
		}
		commands = append(commands, restoreDarwinEndpoint(service.Name, "web", service.Web)...)
		commands = append(commands, restoreDarwinEndpoint(service.Name, "secureweb", service.SecureWeb)...)
		commands = append(commands, restoreDarwinEndpoint(service.Name, "socksfirewall", service.SOCKS)...)
		bypass := []string{"-setproxybypassdomains", service.Name}
		if len(service.Bypass) == 0 {
			bypass = append(bypass, "Empty")
		} else {
			bypass = append(bypass, service.Bypass...)
		}
		commands = append(commands, bypass)
	}
	if len(commands) > 0 {
		if err := runNetworkSetupChanges(commands); err != nil {
			return "", fmt.Errorf("恢复 macOS 系统代理失败：%w", err)
		}
	}
	if err := os.Remove(proxyMarkerPath()); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("清理 macOS 代理恢复点失败：%w", err)
	}
	return "", nil
}

func recoverPreparedDarwinSnapshot(snapshot darwinProxySnapshot) (string, error) {
	current, err := readDarwinProxyServices()
	if err != nil {
		return "", err
	}
	currentByName := make(map[string]darwinServiceProxy, len(current))
	for _, service := range current {
		currentByName[service.Name] = service
	}
	commands := make([][]string, 0)
	for _, original := range snapshot.Services {
		value, exists := currentByName[original.Name]
		if !exists {
			continue
		}
		if endpointMatches(value.Web, snapshot.HTTPPort) {
			commands = append(commands, restoreDarwinEndpoint(original.Name, "web", original.Web)...)
		}
		if endpointMatches(value.SecureWeb, snapshot.HTTPPort) {
			commands = append(commands, restoreDarwinEndpoint(original.Name, "secureweb", original.SecureWeb)...)
		}
		if endpointMatches(value.SOCKS, snapshot.SOCKSPort) {
			commands = append(commands, restoreDarwinEndpoint(original.Name, "socksfirewall", original.SOCKS)...)
		}
		if equalStrings(value.Bypass, darwinOwnedBypass) {
			bypass := []string{"-setproxybypassdomains", original.Name}
			if len(original.Bypass) == 0 {
				bypass = append(bypass, "Empty")
			} else {
				bypass = append(bypass, original.Bypass...)
			}
			commands = append(commands, bypass)
		}
	}
	if len(commands) > 0 {
		if err := runNetworkSetupChanges(commands); err != nil {
			return "", fmt.Errorf("恢复未完成的 macOS 代理事务失败：%w", err)
		}
	}
	if err := os.Remove(proxyMarkerPath()); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if len(commands) > 0 {
		return "检测到未完成的 macOS 代理事务，已恢复确认属于 HypoMux 的设置", nil
	}
	return "检测到未完成的 macOS 代理事务；未发现 HypoMux 仍持有的代理设置", nil
}

func writeDarwinProxySnapshot(snapshot darwinProxySnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("生成 macOS 代理恢复点失败：%w", err)
	}
	if err := atomicWriteFile(proxyMarkerPath(), data, 0o600); err != nil {
		return fmt.Errorf("保存 macOS 代理恢复点失败：%w", err)
	}
	return nil
}

func readDarwinProxyServices() ([]darwinServiceProxy, error) {
	output, err := exec.Command(networkSetupPath, "-listallnetworkservices").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("读取 macOS 网络服务失败：%s", commandError(output, err))
	}
	names := parseDarwinNetworkServices(string(output))
	result := make([]darwinServiceProxy, 0, len(names))
	for _, name := range names {
		web, err := readDarwinProxyEndpoint("-getwebproxy", name)
		if err != nil {
			continue
		}
		secureWeb, err := readDarwinProxyEndpoint("-getsecurewebproxy", name)
		if err != nil {
			continue
		}
		socks, err := readDarwinProxyEndpoint("-getsocksfirewallproxy", name)
		if err != nil {
			continue
		}
		bypassOutput, bypassErr := exec.Command(networkSetupPath, "-getproxybypassdomains", name).CombinedOutput()
		if bypassErr != nil {
			continue
		}
		result = append(result, darwinServiceProxy{
			Name: name, Web: web, SecureWeb: secureWeb, SOCKS: socks,
			Bypass: parseDarwinBypassDomains(string(bypassOutput)),
		})
	}
	return result, nil
}

func readDarwinProxyEndpoint(command, service string) (darwinProxyEndpoint, error) {
	output, err := exec.Command(networkSetupPath, command, service).CombinedOutput()
	if err != nil {
		return darwinProxyEndpoint{}, fmt.Errorf("读取 %s 代理失败：%s", service, commandError(output, err))
	}
	return parseDarwinProxyEndpoint(string(output)), nil
}

func parseDarwinNetworkServices(output string) []string {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	result := make([]string, 0, len(lines))
	for index, line := range lines {
		value := strings.TrimSpace(line)
		if value == "" || index == 0 || strings.HasPrefix(value, "*") {
			continue
		}
		result = append(result, value)
	}
	return result
}

func parseDarwinProxyEndpoint(output string) darwinProxyEndpoint {
	result := darwinProxyEndpoint{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Enabled":
			result.Enabled = strings.EqualFold(strings.TrimSpace(value), "yes")
		case "Server":
			result.Server = strings.TrimSpace(value)
		case "Port":
			result.Port, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	return result
}

func parseDarwinBypassDomains(output string) []string {
	result := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		value := strings.TrimSpace(line)
		if value == "" || strings.HasPrefix(value, "There aren't any") {
			continue
		}
		result = append(result, value)
	}
	return result
}

func isOwnedDarwinProxy(service darwinServiceProxy, httpPort, socksPort int) bool {
	return endpointMatches(service.Web, httpPort) && endpointMatches(service.SecureWeb, httpPort) &&
		endpointMatches(service.SOCKS, socksPort)
}

func endpointMatches(endpoint darwinProxyEndpoint, port int) bool {
	return endpoint.Enabled && endpoint.Server == "127.0.0.1" && endpoint.Port == port
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func restoreDarwinEndpoint(service, kind string, endpoint darwinProxyEndpoint) [][]string {
	commands := make([][]string, 0, 2)
	if endpoint.Server != "" && endpoint.Port > 0 {
		commands = append(commands, []string{"-set" + kind + "proxy", service, endpoint.Server, strconv.Itoa(endpoint.Port), "off"})
	}
	state := "off"
	if endpoint.Enabled {
		state = "on"
	}
	commands = append(commands, []string{"-set" + kind + "proxystate", service, state})
	return commands
}

func runNetworkSetupChanges(commands [][]string) error {
	parts := []string{"set -e"}
	for _, arguments := range commands {
		command := []string{shellQuote(networkSetupPath)}
		for _, argument := range arguments {
			command = append(command, shellQuote(argument))
		}
		parts = append(parts, strings.Join(command, " "))
	}
	script := strings.Join(parts, "; ")
	appleScript := "do shell script " + appleScriptQuote(script) + " with administrator privileges"
	output, err := exec.Command("/usr/bin/osascript", "-e", appleScript).CombinedOutput()
	if err != nil {
		return errors.New(commandError(output, err))
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func appleScriptQuote(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return "\"" + value + "\""
}

func commandError(output []byte, err error) string {
	message := strings.TrimSpace(string(output))
	if message != "" {
		return message
	}
	return err.Error()
}
