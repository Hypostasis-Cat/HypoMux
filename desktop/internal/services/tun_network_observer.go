package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

func networkEnvironmentFingerprint(routes []networkRoute) string {
	// Order and duplicate rows are not a network change. Hash the snapshot
	// locally; do not put the full routing table into periodic support logs.
	unique := map[string]bool{}
	for _, route := range routes {
		unique[fmt.Sprintf("%s|%s|%d|%s|%s|%d|%d|%t|%t|%t|%d", route.Prefix.Masked(), route.NextHop, route.InterfaceIndex,
			route.Alias, route.Description, route.InterfaceType, route.TunnelType, route.MetadataKnown, route.Hardware, route.Connected, route.Metric)] = true
	}
	rows := make([]string, 0, len(unique))
	for row := range unique {
		rows = append(rows, row)
	}
	sort.Strings(rows)
	digest := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(digest[:])
}

// Reuse the existing 30-second health cadence. Changes are observations, not
// permission to rewrite somebody else's routes or restart a healthy session.
// The existing real-data-path watchdog remains responsible for rollback.
func (s *EngineService) recordTUNNetworkEnvironment(baseline bool) {
	if s.logs == nil {
		return
	}
	routes, err := readNetworkRoutes()
	s.observeTUNNetworkEnvironment(routes, err, baseline)
}

func (s *EngineService) observeTUNNetworkEnvironment(routes []networkRoute, err error, baseline bool) {
	if s.logs == nil {
		return
	}
	if err != nil {
		// Do not replace the last complete baseline with partial observations.
		s.logs.RecordEvent("tun_network", "inspection_incomplete", map[string]any{"error": err.Error()})
		return
	}
	fingerprint := networkEnvironmentFingerprint(routes)
	s.mu.Lock()
	previous := s.tunNetworkFingerprint
	s.tunNetworkFingerprint = fingerprint
	s.mu.Unlock()
	if !baseline && previous == fingerprint {
		return
	}
	event := "changed"
	if baseline || previous == "" {
		event = "baseline"
	}
	aliases, risks := assessNetworkRoutes(routes)
	foreign := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		if !strings.EqualFold(alias, "HypoMux-Tun") {
			foreign = append(foreign, alias)
		}
	}
	s.logs.RecordEvent("tun_network", event, map[string]any{
		"fingerprint": fingerprint, "route_count": len(routes),
		"foreign_route_interfaces": foreign, "route_observations": risks,
	})
}
