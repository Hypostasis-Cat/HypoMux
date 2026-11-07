package services

import (
	"context"
	"fmt"
	"slices"
	"time"
)

const domainQuarantineUpdateMethod = "domain_quarantine.update"

type engineRequest func(ctx context.Context, method string, params any, target any) error

// syncDomainQuarantines brings a running engine and the stored records together
// after a telemetry sample. The settings page saves the expiry switch directly,
// so comparing it with the engine's reported policy on every sample also covers
// an update that failed earlier or an engine started before the change.
func syncDomainQuarantines(
	ctx context.Context,
	request engineRequest,
	records *BlockedDomainService,
	capabilities []string,
	useExpiry bool,
	token uint64,
	telemetry telemetryResult,
) {
	if reported := telemetry.DomainIsolationExpiry; reported != nil && *reported != useExpiry {
		if slices.Contains(capabilities, domainQuarantineUpdateMethod) {
			var ignored struct{}
			_ = request(ctx, domainQuarantineUpdateMethod, map[string]any{"expiry": useExpiry}, &ignored)
		}
		// This sample was produced under the old policy; the next one is not.
		return
	}
	entries := make([]BlockedDomainEntry, 0, len(telemetry.DomainQuarantines))
	for _, entry := range telemetry.DomainQuarantines {
		entries = append(entries, BlockedDomainEntry{
			Adapter: entry.Adapter, Domain: entry.Domain, ExpiresAt: entry.ExpiresAt,
		})
	}
	_ = records.replaceRuntimeSample(token, entries)
}

// evictDomainQuarantines removes records from a running engine, then runs
// commit. Holding lifecycleGate throughout keeps a concurrent start from seeding
// the engine with records that are being removed.
func (s *EngineService) evictDomainQuarantines(params map[string]any, commit func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.acquireLifecycle(ctx); err != nil {
		return fmt.Errorf("网络服务正在切换状态，请稍后重试：%w", err)
	}
	defer s.releaseLifecycle()
	return evictAndCommit(ctx, s.client.Request, s.client.Hello().Capabilities, params, commit)
}

// evictAndCommit keeps the record when a connected core fails to remove it. A
// disconnected core reports no capabilities and has no runtime state to remove.
func evictAndCommit(
	ctx context.Context,
	request engineRequest,
	capabilities []string,
	params map[string]any,
	commit func() error,
) error {
	if slices.Contains(capabilities, domainQuarantineUpdateMethod) {
		var ignored struct{}
		if err := request(ctx, domainQuarantineUpdateMethod, params, &ignored); err != nil {
			return fmt.Errorf("从运行中的核心移除记录失败：%w", err)
		}
	}
	return commit()
}
