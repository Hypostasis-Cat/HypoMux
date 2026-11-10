package main

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

// localAdapter is one usable physical/logical network interface as seen by the
// machine the CLI runs on. It carries exactly the fields protocol v1
// engine.start expects; richer metadata (gateway, DNS, metric) stays a
// desktop concern.
type localAdapter struct {
	Name         string
	IfIndex      int
	SourceIP     string
	PrefixLength int
	SourceIPv6   string
	MAC          string
	Kind         string
}

// listAdapters is the adapter enumeration entry point; tests override it to
// describe deterministic NICs.
var listAdapters = listLocalAdapters

// listLocalAdapters enumerates active adapters eligible for aggregation,
// mirroring the desktop's baseline filtering: up, non-loopback, not the
// HypoMux TUN device, with at least one global IPv4 or IPv6 source address.
func listLocalAdapters() ([]localAdapter, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("扫描网络适配器失败：%w", err)
	}
	result := make([]localAdapter, 0, len(interfaces))
	for _, item := range interfaces {
		if item.Flags&net.FlagUp == 0 || item.Flags&net.FlagLoopback != 0 ||
			isHypoMuxManagedAdapter(item.Name) {
			continue
		}
		addresses, addressErr := item.Addrs()
		if addressErr != nil {
			continue
		}
		adapter := localAdapter{
			Name:    item.Name,
			IfIndex: item.Index,
			MAC:     item.HardwareAddr.String(),
		}
		for _, address := range addresses {
			ip, network, parseErr := net.ParseCIDR(address.String())
			if parseErr != nil || ip.IsLoopback() || ip.IsUnspecified() {
				continue
			}
			if value := ip.To4(); value != nil {
				if value[0] == 169 && value[1] == 254 {
					// Skip APIPA self-assigned addresses: they prove no link.
					continue
				}
				if adapter.SourceIP == "" {
					adapter.SourceIP = value.String()
					if ones, bits := network.Mask.Size(); bits == 32 {
						adapter.PrefixLength = ones
					}
				}
			} else if !ip.IsLinkLocalUnicast() && !ip.IsMulticast() && !ip.IsPrivate() &&
				adapter.SourceIPv6 == "" {
				// Without platform metadata there is no IPv6 gateway to prove
				// that a unique local address (fc00::/7) reaches the Internet,
				// so only global unicast addresses qualify.
				adapter.SourceIPv6 = ip.String()
			}
		}
		if adapter.SourceIP == "" && adapter.SourceIPv6 == "" {
			continue
		}
		adapter.Kind = classifyAdapterKind(item.Name)
		result = append(result, adapter)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, nil
}

func isHypoMuxManagedAdapter(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "HypoMux-Tun")
}

func classifyAdapterKind(name string) string {
	lowerName := strings.ToLower(name)
	if strings.Contains(lowerName, "wi-fi") || strings.Contains(lowerName, "wifi") ||
		strings.Contains(lowerName, "wlan") || strings.Contains(name, "无线") {
		return "wifi"
	}
	return "ethernet"
}

// resolveRequestedAdapters maps user-selected adapter names onto the locally
// enumerated adapters. Weight syntax is `NAME` or `NAME:weight` (1-100); names
// are matched case-insensitively.
func resolveRequestedAdapters(selected []string, available []localAdapter) ([]startAdapter, error) {
	if len(selected) == 0 {
		return nil, nil
	}
	byName := make(map[string]localAdapter, len(available))
	for _, adapter := range available {
		byName[strings.ToLower(adapter.Name)] = adapter
	}
	resolved := make([]startAdapter, 0, len(selected))
	seen := make(map[string]struct{}, len(selected))
	for _, request := range selected {
		name := strings.TrimSpace(request)
		weight := 0
		name, weight = splitAdapterWeight(name)
		if name == "" {
			return nil, errors.New("适配器名称不能为空")
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("适配器 %q 重复选择", name)
		}
		seen[key] = struct{}{}
		adapter, exists := byName[key]
		if !exists {
			return nil, fmt.Errorf("找不到可用适配器 %q；运行 hypomux-cli adapters 查看可用列表", name)
		}
		if weight == 0 {
			weight = 1
		}
		if weight < 1 || weight > 100 {
			return nil, fmt.Errorf("适配器 %q 的权重必须在 1 到 100 之间", name)
		}
		resolved = append(resolved, startAdapter{
			Name:       adapter.Name,
			SourceIP:   adapter.SourceIP,
			IfIndex:    adapter.IfIndex,
			SourceIPv6: adapter.SourceIPv6,
			Weight:     weight,
		})
	}
	return resolved, nil
}

// splitAdapterWeight splits the optional `:weight` suffix. The suffix is
// unambiguous because interface names never end in `:digits`.
func splitAdapterWeight(request string) (string, int) {
	index := strings.LastIndex(request, ":")
	if index <= 0 || index == len(request)-1 {
		return request, 0
	}
	weight, err := atoiStrict(request[index+1:])
	if err != nil {
		return request, 0
	}
	return request[:index], weight
}

func atoiStrict(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("空数字")
	}
	number := 0
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("非法数字 %q", value)
		}
		number = number*10 + int(digit-'0')
		if number > 1<<31-1 {
			return 0, fmt.Errorf("数字过大 %q", value)
		}
	}
	return number, nil
}
