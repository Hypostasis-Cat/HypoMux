package services

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Use the lifecycle gate for both runtime updates and persistence, preventing
// a queued edit from racing start/stop or another configuration transaction.
func (s *EngineService) saveRuntimeSelection(mode string, weighted bool, adapters []AdapterView) ([]AdapterView, error) {
	strategy, _ := normalizeSchedulingStrategy("", weighted)
	return s.SaveScheduling(mode, strategy, adapters)
}

func (s *EngineService) SaveScheduling(mode, strategy string, adapters []AdapterView) ([]AdapterView, error) {
	strategy, err := normalizeSchedulingStrategy(strategy, false)
	if err != nil {
		return nil, err
	}
	weighted := strategy == "weighted"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.acquireLifecycle(ctx); err != nil {
		return nil, err
	}
	defer s.releaseLifecycle()
	if mode != "proxy" && mode != "tun" {
		return nil, fmt.Errorf("不支持的运行模式：%s", mode)
	}
	for _, adapter := range adapters {
		if adapter.Weight < AdapterWeightMin || adapter.Weight > AdapterWeightMax {
			return nil, fmt.Errorf("网卡权重必须在 %d–%d 之间", AdapterWeightMin, AdapterWeightMax)
		}
	}
	if s.client.Hello().ProtocolVersion == 0 {
		return s.adapters.saveSelectionStrategy(mode, weighted, adapters, strategy)
	}
	var status engineStatusResult
	if err := s.client.Request(ctx, "engine.status", nil, &status); err != nil {
		return nil, err
	}
	if status.Engine.State == "stopped" || status.Engine.State == "failed" {
		return s.adapters.saveSelectionStrategy(mode, weighted, adapters, strategy)
	}
	if status.Engine.State != "running" && status.Engine.State != "degraded" {
		return nil, fmt.Errorf("引擎正在切换状态，请稍后重试")
	}
	if mode != s.settings.Get().Mode {
		return nil, fmt.Errorf("切换系统代理或虚拟网卡模式前，请先停止聚合")
	}
	if !slices.Contains(s.client.Hello().Capabilities, "engine.scheduling") {
		return nil, fmt.Errorf("当前核心不支持运行中修改聚合配置，请更新核心")
	}
	if strategy == "adaptive-throughput" && !slices.Contains(s.client.Hello().SchedulingStrategies, strategy) {
		return nil, fmt.Errorf("当前 Core 不支持自适应调度，请更新核心或选择轮询")
	}
	// Bind using freshly enumerated OS data, never addresses supplied by the UI.
	available, err := s.adapters.List()
	if err != nil {
		return nil, err
	}
	selected, err := schedulingAdapters(adapters, available)
	if err != nil {
		return nil, err
	}
	err = commitScheduling(ctx, s.client.Request, map[string]any{
		"strategy": strategy, "weighted": weighted, "adapters": engineAdapters(selected),
	}, func() error { return s.adapters.persistSelectionStrategy(mode, weighted, adapters, strategy) })
	if err != nil {
		return nil, err
	}
	return s.adapters.List()
}

func commitScheduling(ctx context.Context, request func(context.Context, string, any, any) error, next any, persist func() error) error {
	var previous json.RawMessage
	if err := request(ctx, "engine.scheduling", next, &previous); err != nil {
		return err
	}
	// Persistence failure leaves settings untouched; restore the exact runtime
	// configuration, including bindings no longer enumerated by Windows.
	if err := persist(); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var ignored json.RawMessage
		if rollbackErr := request(rollbackCtx, "engine.scheduling", previous, &ignored); rollbackErr != nil {
			return fmt.Errorf("保存失败：%v；恢复运行配置失败：%w", err, rollbackErr)
		}
		return err
	}
	return nil
}

func schedulingAdapters(requested, available []AdapterView) ([]AdapterView, error) {
	byID := make(map[string]AdapterView, len(available))
	for _, adapter := range available {
		byID[adapter.ID] = adapter
	}
	selected := make([]AdapterView, 0, len(requested))
	seen := make(map[string]bool)
	for _, requested := range requested {
		if !requested.Selected {
			continue
		}
		adapter, ok := byID[requested.ID]
		if !ok || !adapter.Operational {
			return nil, fmt.Errorf("网卡 %s 当前不可用，请刷新网卡列表", requested.Name)
		}
		if seen[adapter.ID] {
			return nil, fmt.Errorf("不能重复选择同一网卡")
		}
		seen[adapter.ID] = true
		adapter.Weight = requested.Weight
		adapter.Selected = true
		selected = append(selected, adapter)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("聚合运行时至少需要保留一张参与网卡")
	}
	if err := validateAdapterSources(selected); err != nil {
		return nil, err
	}
	return selected, nil
}

func normalizeSchedulingStrategy(strategy string, weighted bool) (string, error) {
	if strategy == "" {
		if weighted {
			return "weighted", nil
		}
		return "round-robin", nil
	}
	switch strategy {
	case "round-robin", "weighted", "adaptive-throughput":
		return strategy, nil
	}
	return "", fmt.Errorf("未知调度策略：%s", strategy)
}
func effectiveSchedulingStrategy(settings AppSettings) string {
	strategy, _ := normalizeSchedulingStrategy(settings.Strategy, settings.Weighted)
	return strategy
}
