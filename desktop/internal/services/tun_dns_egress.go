package services

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	DNSEgressAuto    = "auto"
	DNSEgressSystem  = "system"
	DNSEgressAdapter = "adapter"
)

type tunDNSEgressDecision struct {
	Adapter   AdapterView
	Mode      string
	Source    string
	Ambiguous bool
	Detail    string
}

func resolveTUNDNSEgress(
	settings AppSettings,
	selected []AdapterView,
	rules []RoutingRule,
	systemDefaultID string,
	systemDefaultErr error,
) (tunDNSEgressDecision, error) {
	if len(selected) == 0 {
		return tunDNSEgressDecision{}, errors.New("无法选择 DNS 出口：没有已启用的网卡")
	}

	switch settings.DNSEgressMode {
	case DNSEgressAdapter:
		adapter, ok := selectedAdapterByID(selected, settings.DNSAdapterID)
		if !ok {
			return tunDNSEgressDecision{}, fmt.Errorf(
				"指定的 DNS 出口网卡 %q 未启用或当前不可用，请在设置中重新选择",
				settings.DNSAdapterID,
			)
		}
		return tunDNSEgressDecision{
			Adapter: adapter, Mode: DNSEgressAdapter, Source: "explicit_adapter",
		}, nil
	case DNSEgressSystem:
		if systemDefaultErr != nil {
			return tunDNSEgressDecision{}, fmt.Errorf("读取 Windows 默认路由失败：%w", systemDefaultErr)
		}
		adapter, ok := selectedAdapterByID(selected, systemDefaultID)
		if !ok {
			return tunDNSEgressDecision{}, fmt.Errorf(
				"Windows 默认路由网卡 %q 未在 HypoMux 中启用，请启用该网卡或改用自动 DNS 出口",
				systemDefaultID,
			)
		}
		return tunDNSEgressDecision{
			Adapter: adapter, Mode: DNSEgressSystem, Source: "system_default_route",
		}, nil
	case "", DNSEgressAuto:
		return resolveAutomaticTUNDNSEgress(selected, rules, systemDefaultID, systemDefaultErr), nil
	default:
		return tunDNSEgressDecision{}, fmt.Errorf("不支持的 DNS 出口模式：%s", settings.DNSEgressMode)
	}
}

func resolveAutomaticTUNDNSEgress(
	selected []AdapterView,
	rules []RoutingRule,
	systemDefaultID string,
	systemDefaultErr error,
) tunDNSEgressDecision {
	var ipv4Default, ipv6Default *AdapterView
	for _, rule := range rules {
		if rule.MatchType != MatchIP {
			continue
		}
		var target **AdapterView
		switch rule.Value {
		case "0.0.0.0/0":
			target = &ipv4Default
		case "::/0":
			target = &ipv6Default
		default:
			continue
		}
		if adapter, ok := selectedAdapterForOutbound(selected, rule.Outbound); ok {
			copy := adapter
			*target = &copy
		}
	}

	if ipv4Default != nil {
		decision := tunDNSEgressDecision{
			Adapter: *ipv4Default, Mode: DNSEgressAuto, Source: "ipv4_default_rule",
		}
		if ipv6Default != nil && !strings.EqualFold(ipv4Default.ID, ipv6Default.ID) {
			decision.Ambiguous = true
			decision.Detail = fmt.Sprintf(
				"IPv4 与 IPv6 默认分流出口不同；DNS 查询优先跟随 IPv4 出口 %s",
				ipv4Default.Name,
			)
		}
		return decision
	}
	if ipv6Default != nil {
		return tunDNSEgressDecision{
			Adapter: *ipv6Default, Mode: DNSEgressAuto, Source: "ipv6_default_rule",
		}
	}
	if adapter, ok := selectedAdapterByID(selected, systemDefaultID); ok {
		return tunDNSEgressDecision{
			Adapter: adapter, Mode: DNSEgressAuto, Source: "system_default_route",
		}
	}

	adapter := lowestMetricAdapter(selected)
	detail := "Windows 默认路由不在已启用网卡中；已按最低接口跃点值选择 DNS 出口"
	if systemDefaultErr != nil {
		detail = fmt.Sprintf("无法读取 Windows 默认路由（%v）；已按最低接口跃点值选择 DNS 出口", systemDefaultErr)
	}
	return tunDNSEgressDecision{
		Adapter: adapter, Mode: DNSEgressAuto, Source: "metric_fallback",
		Ambiguous: len(selected) > 1, Detail: detail,
	}
}

func selectedAdapterForOutbound(selected []AdapterView, outbound string) (AdapterView, bool) {
	if !strings.HasPrefix(outbound, "nic_") {
		return AdapterView{}, false
	}
	return selectedAdapterByID(selected, strings.TrimPrefix(outbound, "nic_"))
}

func selectedAdapterByID(selected []AdapterView, id string) (AdapterView, bool) {
	id = strings.TrimSpace(id)
	for _, adapter := range selected {
		if adapter.Operational && adapter.ID == id {
			return adapter, true
		}
	}
	for _, adapter := range selected {
		if adapter.Operational && strings.EqualFold(adapter.ID, id) {
			return adapter, true
		}
	}
	return AdapterView{}, false
}

func lowestMetricAdapter(selected []AdapterView) AdapterView {
	candidates := append([]AdapterView(nil), selected...)
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i].Metric, candidates[j].Metric
		if left < 0 {
			left = int(^uint(0) >> 1)
		}
		if right < 0 {
			right = int(^uint(0) >> 1)
		}
		if left != right {
			return left < right
		}
		return strings.ToLower(candidates[i].Name) < strings.ToLower(candidates[j].Name)
	})
	return candidates[0]
}
