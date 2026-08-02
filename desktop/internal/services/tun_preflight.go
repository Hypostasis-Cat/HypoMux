package services

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
)

type TunPreflightIssue struct {
	Code   string `json:"code"`
	Level  string `json:"level"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type TunPreflightSnapshot struct {
	Ready                    bool                `json:"ready"`
	CheckedAt                time.Time           `json:"checked_at"`
	SelectedAdapterIDs       []string            `json:"selected_adapter_ids"`
	HostElevated             bool                `json:"host_elevated"`
	PrivilegeBrokerAvailable bool                `json:"privilege_broker_available"`
	EngineAvailable          bool                `json:"engine_available"`
	SingBoxAvailable         bool                `json:"sing_box_available"`
	WFPReady                 bool                `json:"wfp_ready"`
	WFPDetail                string              `json:"wfp_detail,omitempty"`
	StrictRouteRequested     bool                `json:"strict_route_requested"`
	EffectiveStrictRoute     bool                `json:"effective_strict_route"`
	ForeignTUN               string              `json:"foreign_tun,omitempty"`
	SharedGatewayRisks       []string            `json:"shared_gateway_risks"`
	Issues                   []TunPreflightIssue `json:"issues"`
}

type tunPlatformSnapshot struct {
	HostElevated             bool
	PrivilegeBrokerAvailable bool
	WFPReady                 bool
	WFPDetail                string
	DefaultRouteAliases      []string
	RouteScanError           string
}

type TunService struct {
	mu              sync.Mutex
	settings        *SettingsService
	adapters        *AdapterService
	listAdapters    func() ([]AdapterView, error)
	inspectPlatform func(bool) tunPlatformSnapshot
	resolveEngine   func() (string, error)
	resolveSingBox  func() (string, error)
	now             func() time.Time
	latest          TunPreflightSnapshot
}

func NewTunService(settings *SettingsService, adapters *AdapterService) *TunService {
	return &TunService{
		settings:        settings,
		adapters:        adapters,
		listAdapters:    adapters.List,
		inspectPlatform: inspectTunPlatform,
		resolveEngine:   engineclient.ResolveExecutable,
		resolveSingBox:  func() (string, error) { return resolveRuntimeAsset("sing-box.exe") },
		now:             time.Now,
		latest: TunPreflightSnapshot{
			CheckedAt: time.Time{}, SharedGatewayRisks: []string{}, Issues: []TunPreflightIssue{},
		},
	}
}

func (s *TunService) Latest() TunPreflightSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneTunPreflight(s.latest)
}

// Preflight is deliberately read-only. It does not start the engine, create a
// Wintun adapter, add WFP filters, change routes, or request elevation.
func (s *TunService) Preflight(adapterIDs []string) (TunPreflightSnapshot, error) {
	available, err := s.listAdapters()
	if err != nil {
		return TunPreflightSnapshot{}, fmt.Errorf("扫描 TUN 网卡失败：%w", err)
	}
	wanted := make(map[string]struct{}, len(adapterIDs))
	for _, id := range adapterIDs {
		if value := strings.TrimSpace(id); value != "" {
			wanted[value] = struct{}{}
		}
	}
	selected := make([]AdapterView, 0, len(available))
	for _, adapter := range available {
		_, explicitlySelected := wanted[adapter.ID]
		if (len(wanted) == 0 && adapter.Selected) || explicitlySelected {
			selected = append(selected, adapter)
		}
	}
	return s.checkSelected(selected), nil
}

func (s *TunService) checkSelected(selected []AdapterView) TunPreflightSnapshot {
	settings := s.settings.Get()
	rememberedWFPFailure, rememberedWFPDetail := s.settings.rememberedWFPCompatibilityFailure()
	platform := s.inspectPlatform(settings.StrictRoute && !rememberedWFPFailure)
	if settings.StrictRoute && rememberedWFPFailure {
		platform.WFPReady = false
		platform.WFPDetail = "此设备已有匹配当前 Windows 与 HypoMux 版本的 WFP 失败记录"
		if strings.TrimSpace(rememberedWFPDetail) != "" {
			platform.WFPDetail += "：" + rememberedWFPDetail
		}
	}
	enginePath, engineErr := s.resolveEngine()
	singBoxPath, singBoxErr := s.resolveSingBox()
	snapshot := TunPreflightSnapshot{
		CheckedAt:                s.now().UTC(),
		HostElevated:             platform.HostElevated,
		PrivilegeBrokerAvailable: platform.PrivilegeBrokerAvailable,
		EngineAvailable:          engineErr == nil && enginePath != "",
		SingBoxAvailable:         singBoxErr == nil && singBoxPath != "",
		WFPReady:                 platform.WFPReady,
		WFPDetail:                platform.WFPDetail,
		StrictRouteRequested:     settings.StrictRoute,
		EffectiveStrictRoute:     settings.StrictRoute && platform.WFPReady,
		SharedGatewayRisks:       sharedIPv4GatewayRisks(selected),
		Issues:                   []TunPreflightIssue{},
	}
	for _, adapter := range selected {
		snapshot.SelectedAdapterIDs = append(snapshot.SelectedAdapterIDs, adapter.ID)
	}
	sort.Strings(snapshot.SelectedAdapterIDs)
	if len(selected) == 0 {
		snapshot.Issues = append(snapshot.Issues, tunBlocker(
			"no_adapter", "未选择活动网卡", "请至少选择一张具有有效 IPv4 地址的活动网卡。",
		))
	}
	if !snapshot.EngineAvailable {
		snapshot.Issues = append(snapshot.Issues, tunBlocker(
			"engine_missing", "聚合核心不可用", errorDetail(engineErr, "未找到 hypomux-engine.exe"),
		))
	}
	if !snapshot.SingBoxAvailable {
		snapshot.Issues = append(snapshot.Issues, tunBlocker(
			"sing_box_missing", "TUN 侧车不可用", errorDetail(singBoxErr, "未找到 sing-box.exe"),
		))
	}
	if platform.HostElevated {
		snapshot.Issues = append(snapshot.Issues, tunWarning(
			"elevated_ui_host",
			"桌面界面正在管理员权限下运行",
			"HypoMux 已进入管理员兼容模式，系统代理与 TUN 仍可使用。建议下次使用普通权限启动；高权限网络操作仍由独立聚合核心承接。",
		))
	}
	if !platform.PrivilegeBrokerAvailable {
		snapshot.Issues = append(snapshot.Issues, tunBlocker(
			"privilege_broker_unavailable",
			"未连接高权限聚合核心",
			"虚拟网卡需要独立权限服务创建 TUN、WFP 与路由资源；本次不会启动出站池，也不会修改系统网络。",
		))
	}
	for _, alias := range platform.DefaultRouteAliases {
		clean := strings.TrimSpace(alias)
		if clean == "" {
			continue
		}
		if strings.EqualFold(clean, "HypoMux-Tun") {
			snapshot.Issues = append(snapshot.Issues, tunBlocker(
				"stale_hypomux_tun", "检测到 HypoMux TUN 残留",
				"系统仍存在由 HypoMux-Tun 接管的默认路由。应先由独立聚合核心执行精确恢复，再重新预检。",
			))
		} else if snapshot.ForeignTUN == "" {
			snapshot.ForeignTUN = clean
			snapshot.Issues = append(snapshot.Issues, tunBlocker(
				"foreign_tun", "第三方虚拟隧道正在接管默认路由",
				fmt.Sprintf("检测到 %s。请先关闭对应代理或 VPN，再启动虚拟网卡模式。", clean),
			))
		}
	}
	if platform.RouteScanError != "" {
		snapshot.Issues = append(snapshot.Issues, tunWarning(
			"route_scan_failed", "未能完成默认路由检查", platform.RouteScanError,
		))
	}
	if settings.StrictRoute && !platform.WFPReady {
		detail := "WFP 只读预检未通过；用户偏好保持开启，但本次应仅使用兼容 TUN，不能把降级结果写回为用户偏好。"
		if rememberedWFPFailure {
			detail = platform.WFPDetail + "；本次直接使用兼容 TUN。点击“重新检测并修复”或升级 Windows/HypoMux 后会重新尝试。"
		}
		snapshot.Issues = append(snapshot.Issues, tunWarning(
			"wfp_compatibility",
			"严格路由当前不可用",
			detail,
		))
	}
	for _, detail := range snapshot.SharedGatewayRisks {
		snapshot.Issues = append(snapshot.Issues, tunWarning(
			"shared_lan_gateway", "所选网卡共用子网和默认网关", detail+"；允许继续，但 Windows 无法保证独立出口或带宽聚合。",
		))
	}
	snapshot.Ready = true
	for _, issue := range snapshot.Issues {
		if issue.Level == "blocker" {
			snapshot.Ready = false
			break
		}
	}
	s.mu.Lock()
	s.latest = cloneTunPreflight(snapshot)
	s.mu.Unlock()
	return snapshot
}

func tunBlocker(code, title, detail string) TunPreflightIssue {
	return TunPreflightIssue{Code: code, Level: "blocker", Title: title, Detail: detail}
}

func tunWarning(code, title, detail string) TunPreflightIssue {
	return TunPreflightIssue{Code: code, Level: "warning", Title: title, Detail: detail}
}

func errorDetail(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}

func sharedIPv4GatewayRisks(adapters []AdapterView) []string {
	var risks []string
	for leftIndex, left := range adapters {
		leftNetwork := adapterIPv4Network(left)
		if leftNetwork == nil || left.Gateway == "" {
			continue
		}
		for _, right := range adapters[leftIndex+1:] {
			rightNetwork := adapterIPv4Network(right)
			if rightNetwork == nil || right.Gateway == "" {
				continue
			}
			if leftNetwork.String() == rightNetwork.String() &&
				strings.EqualFold(primaryGateway(left.Gateway), primaryGateway(right.Gateway)) {
				risks = append(risks, fmt.Sprintf(
					"%s 与 %s 同属 %s，且共用网关 %s",
					left.Name, right.Name, leftNetwork.String(), primaryGateway(left.Gateway),
				))
			}
		}
	}
	return risks
}

func adapterIPv4Network(adapter AdapterView) *net.IPNet {
	if adapter.PrefixLength < 1 || adapter.PrefixLength > 32 {
		return nil
	}
	ip := net.ParseIP(adapter.Address).To4()
	if ip == nil {
		return nil
	}
	mask := net.CIDRMask(adapter.PrefixLength, 32)
	return &net.IPNet{IP: ip.Mask(mask), Mask: mask}
}

func primaryGateway(value string) string {
	return strings.TrimSpace(strings.SplitN(value, ",", 2)[0])
}

func cloneTunPreflight(value TunPreflightSnapshot) TunPreflightSnapshot {
	value.SelectedAdapterIDs = append([]string(nil), value.SelectedAdapterIDs...)
	value.SharedGatewayRisks = append([]string(nil), value.SharedGatewayRisks...)
	value.Issues = append([]TunPreflightIssue(nil), value.Issues...)
	return value
}

func firstTunBlocker(snapshot TunPreflightSnapshot) error {
	for _, issue := range snapshot.Issues {
		if issue.Level == "blocker" {
			return errors.New(issue.Title + "：" + issue.Detail)
		}
	}
	return nil
}
