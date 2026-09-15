package server

import (
	"encoding/json"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/wfp"
)

func (s *Server) updateScheduling(request protocol.Request) protocol.Response {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.proxy == nil || !s.proxy.Running() {
		return protocol.Failure(request.ID, "invalid_state", "engine must be running", nil)
	}
	var params proxy.SchedulingConfig
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return protocol.Failure(request.ID, "invalid_params", err.Error(), nil)
	}
	next, err := proxy.ValidateScheduling(params)
	if err != nil {
		return protocol.Failure(request.ID, "invalid_params", err.Error(), nil)
	}
	// Retain old bindings for established flows and explicit NIC routes. Install
	// the union before publishing the new pool so newly added NIC DNS works too.
	bindings := append([]wfp.Adapter(nil), s.adapters...)
	for _, adapter := range next.Adapters {
		if adapter.IfIndex <= 0 {
			continue
		}
		binding := wfp.Adapter{Name: adapter.Name, SourceIP: adapter.SourceIP, IfIndex: uint32(adapter.IfIndex)}
		found := false
		for _, old := range bindings {
			if old == binding {
				found = true
				break
			}
		}
		if !found {
			bindings = append(bindings, binding)
		}
	}
	if s.dnsExemption != nil && len(bindings) != len(s.adapters) {
		exemption, openErr := wfp.OpenDNSExemption("", bindings)
		if openErr != nil {
			return protocol.Failure(request.ID, "wfp_unavailable", openErr.Error(), nil)
		}
		if closeErr := s.closeDNSExemption(); closeErr != nil {
			_ = exemption.Close()
			return protocol.Failure(request.ID, "wfp_unavailable", closeErr.Error(), nil)
		}
		s.dnsExemption = exemption
	}
	previous, err := s.proxy.UpdateScheduling(next)
	if err != nil {
		return protocol.Failure(request.ID, "update_failed", err.Error(), nil)
	}
	s.adapters = bindings
	return protocol.Result(request.ID, previous)
}
