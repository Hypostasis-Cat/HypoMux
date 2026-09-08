package server

import (
	"encoding/json"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
)

func (s *Server) configureSteamCDN(request protocol.Request) protocol.Response {
	var params struct {
		Enabled *bool `json:"enabled"`
		Reset   bool  `json:"reset"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return protocol.Failure(request.ID, "invalid_params", "invalid Steam CDN settings", nil)
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.proxy == nil {
		return protocol.Result(request.ID, proxy.SteamCDNStatus{Entries: []proxy.SteamCDNEntry{}})
	}
	return protocol.Result(request.ID, s.proxy.ConfigureSteamCDN(params.Enabled, params.Reset))
}
