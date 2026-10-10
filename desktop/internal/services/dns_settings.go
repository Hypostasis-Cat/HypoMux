package services

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func settingsDNSServers(settings AppSettings) []string {
	if settings.DNSServers != nil {
		return append([]string(nil), settings.DNSServers...)
	}
	return []string{settings.DNSServer}
}

func normalizeDNSSettings(settings AppSettings) (AppSettings, error) {
	values := settingsDNSServers(settings)
	if len(values) == 0 || len(values) > 16 {
		return settings, fmt.Errorf("请配置 1–16 个传统 DNS 地址")
	}
	servers := make([]string, 0, len(values))
	for index, value := range values {
		ip := net.ParseIP(strings.TrimSpace(value))
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			return settings, fmt.Errorf("第 %d 个 DNS 地址格式无效，请输入合法单播 IPv4 或 IPv6 地址；链路本地 DNS 请使用网卡自动配置", index+1)
		}
		servers = append(servers, ip.String())
	}
	servers = uniqueNonEmpty(servers)
	settings.DNSServer = servers[0]
	if settings.DNSServers != nil {
		settings.DNSServers = servers
	}
	if len(settings.DoHServers) > 16 {
		return settings, fmt.Errorf("最多可配置 16 个 DoH 地址")
	}
	dohServers := make([]string, 0, len(settings.DoHServers))
	for index, value := range settings.DoHServers {
		value = strings.TrimSpace(value)
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || len(value) > 2048 {
			return settings, fmt.Errorf("第 %d 个 DoH 地址无效，请输入不含账号或片段的完整 HTTPS 地址", index+1)
		}
		port := 443
		if u.Port() != "" {
			port, err = strconv.Atoi(u.Port())
		}
		if err != nil || port < 1 || port > 65535 || strings.HasSuffix(u.Host, ":") {
			return settings, fmt.Errorf("第 %d 个 DoH 端口无效，请使用 1–65535", index+1)
		}
		host := strings.ToLower(u.Hostname())
		if ip := net.ParseIP(host); ip != nil {
			if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
				return settings, fmt.Errorf("第 %d 个 DoH IP 地址无效", index+1)
			}
			host = ip.String()
		} else if !validDoHHostname(host) {
			return settings, fmt.Errorf("第 %d 个 DoH 域名无效", index+1)
		}
		u.Host = host
		if strings.Contains(host, ":") {
			u.Host = "[" + host + "]"
		}
		if port != 443 {
			u.Host = net.JoinHostPort(host, strconv.Itoa(port))
		}
		if u.Path == "" {
			u.Path = "/dns-query"
		}
		dohServers = append(dohServers, u.String())
	}
	if settings.DoHServers != nil {
		settings.DoHServers = uniqueNonEmpty(dohServers)
	}
	if settings.DNSPolicy == "custom" && len(dohServers) == 0 {
		return settings, fmt.Errorf("自定义 DoH 策略需要至少一个 HTTPS 地址")
	}
	if len(settings.DoTServers) > 16 {
		return settings, fmt.Errorf("最多可配置 16 个 DoT 地址")
	}
	dotServers := make([]string, 0, len(settings.DoTServers))
	for index, value := range settings.DoTServers {
		normalized, err := normalizeDoTAddress(value)
		if err != nil {
			return settings, fmt.Errorf("第 %d 个 DoT 地址无效：%w", index+1, err)
		}
		dotServers = append(dotServers, normalized)
	}
	if settings.DoTServers != nil {
		settings.DoTServers = uniqueNonEmpty(dotServers)
	}
	if settings.DNSPolicy == "dot" && len(dotServers) == 0 {
		return settings, fmt.Errorf("DoT 策略需要至少一个 tls:// 地址")
	}
	return settings, nil
}

func normalizeDoTAddress(value string) (string, error) {
	value = strings.TrimSpace(value)
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "tls" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || strings.ContainsAny(value, "?#") || len(value) > 2048 {
		return "", fmt.Errorf("请输入 tls://域名[:端口]，不含账号、路径、查询参数或片段")
	}
	port := 853
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
	}
	if err != nil || port < 1 || port > 65535 || strings.HasSuffix(u.Host, ":") {
		return "", fmt.Errorf("端口必须在 1–65535 之间")
	}
	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			return "", fmt.Errorf("请输入有效单播 IP 地址")
		}
		host = ip.String()
	} else if strings.Trim(host, "0123456789.") == "" || !validDoHHostname(host) {
		return "", fmt.Errorf("请输入有效域名")
	}
	u.Host = net.JoinHostPort(host, strconv.Itoa(port))
	return u.String(), nil
}

func validDoHHostname(host string) bool {
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	return true
}
