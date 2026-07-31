package services

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

type AdapterView struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Address      string   `json:"address"`
	PrefixLength int      `json:"prefix_length"`
	SourceIPv6   string   `json:"source_ipv6,omitempty"`
	IfIndex      int      `json:"if_index"`
	IPv6IfIndex  int      `json:"ipv6_if_index,omitempty"`
	Gateway      string   `json:"gateway,omitempty"`
	DNSServers   []string `json:"dns_servers"`
	Metric       int      `json:"metric"`
	AutoMetric   bool     `json:"automatic_metric"`
	Selected     bool     `json:"selected"`
	Weight       int      `json:"weight"`
	Kind         string   `json:"kind"`
	Operational  bool     `json:"operational"`
}

type AdapterService struct {
	settings *SettingsService
}

func NewAdapterService(settings *SettingsService) *AdapterService {
	return &AdapterService{settings: settings}
}

func (s *AdapterService) List() ([]AdapterView, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("扫描网络适配器失败：%w", err)
	}
	settings := s.settings.Get()
	metadata := adapterPlatformMetadata()
	selected := make(map[string]struct{}, len(settings.SelectedAdapterIDs))
	for _, id := range settings.SelectedAdapterIDs {
		selected[id] = struct{}{}
	}
	result := make([]AdapterView, 0, len(interfaces))
	for _, item := range interfaces {
		if item.Flags&net.FlagUp == 0 || item.Flags&net.FlagLoopback != 0 || isHypoMuxManagedAdapter(item.Name) {
			continue
		}
		addresses, addressErr := item.Addrs()
		if addressErr != nil {
			continue
		}
		var ipv4, ipv6 string
		prefixLength := 0
		for _, address := range addresses {
			ip, network, parseErr := net.ParseCIDR(address.String())
			if parseErr != nil || ip.IsLoopback() || ip.IsUnspecified() {
				continue
			}
			if value := ip.To4(); value != nil {
				if value[0] == 169 && value[1] == 254 {
					continue
				}
				if ipv4 == "" {
					ipv4 = value.String()
					if ones, bits := network.Mask.Size(); bits == 32 {
						prefixLength = ones
					}
				}
			} else if !ip.IsLinkLocalUnicast() && !ip.IsMulticast() && ipv6 == "" {
				ipv6 = ip.String()
			}
		}
		if ipv4 == "" {
			continue
		}
		id := item.Name
		_, isSelected := selected[id]
		weight := settings.AdapterWeights[id]
		if weight < AdapterWeightMin || weight > AdapterWeightMax {
			weight = AdapterWeightDefault
		}
		kind := "ethernet"
		lowerName := strings.ToLower(item.Name)
		if strings.Contains(lowerName, "wi-fi") || strings.Contains(lowerName, "wifi") ||
			strings.Contains(lowerName, "wlan") || strings.Contains(item.Name, "无线") {
			kind = "wifi"
		}
		description := item.HardwareAddr.String()
		if description == "" {
			description = "Windows 网络接口"
		}
		details, hasDetails := metadata[item.Index]
		if !hasDetails {
			details = adapterMetadata{Metric: -1, AutoMetric: true}
		}
		result = append(result, AdapterView{
			ID:           id,
			Name:         item.Name,
			Description:  description,
			Address:      ipv4,
			PrefixLength: prefixLength,
			SourceIPv6:   ipv6,
			IfIndex:      item.Index,
			IPv6IfIndex:  item.Index,
			Gateway:      details.Gateway,
			DNSServers:   details.DNSServers,
			Metric:       details.Metric,
			AutoMetric:   details.AutoMetric,
			Selected:     isSelected,
			Weight:       weight,
			Kind:         kind,
			Operational:  true,
		})
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

func (s *AdapterService) Refresh() ([]AdapterView, error) {
	return s.List()
}

func (s *AdapterService) SaveSelection(mode string, weighted bool, adapters []AdapterView) ([]AdapterView, error) {
	selected := make([]string, 0, len(adapters))
	weights := make(map[string]int, len(adapters))
	for _, adapter := range adapters {
		if adapter.Weight < AdapterWeightMin || adapter.Weight > AdapterWeightMax {
			return nil, fmt.Errorf("%s 的调度权重必须在 %d–%d 之间", adapter.Name, AdapterWeightMin, AdapterWeightMax)
		}
		weights[adapter.ID] = adapter.Weight
		if adapter.Selected {
			selected = append(selected, adapter.ID)
		}
	}
	if _, err := s.settings.UpdateHome(mode, weighted, selected, weights); err != nil {
		return nil, err
	}
	return s.List()
}
