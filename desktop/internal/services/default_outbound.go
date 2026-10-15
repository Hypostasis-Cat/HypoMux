package services

import (
	"fmt"
	"strings"
)

// Missing values retain the fallback used by versions before this preference.
func normalizeDefaultOutbound(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "aggregation", nil
	}
	if !isValidOutbound(value) || len(value) > 260 || strings.ContainsAny(value, "\x00\r\n\t") {
		return "", fmt.Errorf("全局默认规则的出口无效：%q", value)
	}
	return value, nil
}

func validateDefaultOutbound(value string, adapters []AdapterView) error {
	outbound, err := normalizeDefaultOutbound(value)
	if err != nil {
		return err
	}
	if _, ok := availableRoutingOutbounds(adapters)[outbound]; !ok {
		return fmt.Errorf("全局默认规则引用了未启用或不可用的网卡出口：%s；请在设置 → 高级网络中重新选择", outbound)
	}
	return nil
}
