package server

import (
	"encoding/json"
	"strings"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
)

func (s *Server) updateDomainQuarantines(request protocol.Request) protocol.Response {
	var params proxy.DomainQuarantineUpdate
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return protocol.Failure(request.ID, "invalid_params", "invalid domain quarantine update", nil)
	}
	for index, key := range params.Remove {
		params.Remove[index].Adapter = strings.TrimSpace(key.Adapter)
		if params.Remove[index].Adapter == "" || strings.TrimSpace(key.Domain) == "" {
			return protocol.Failure(request.ID, "invalid_params", "adapter and domain are required", nil)
		}
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.proxy == nil {
		return protocol.Result(request.ID, proxy.DomainQuarantineUpdateResult{})
	}
	s.proxy.UpdateDomainQuarantines(params)
	return protocol.Result(request.ID, proxy.DomainQuarantineUpdateResult{Applied: true})
}
