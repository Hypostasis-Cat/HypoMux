package server

import (
	"errors"
	"net"
	"strings"

	api "github.com/Hypostasis-Cat/HypoMux/engine/internal/api/v1"
)

// ClientRole is the trust level the Core Service assigns to a pipe client from
// the executable pin it matched. It is never taken from the request body. The
// desktop pin yields the full protocol surface; the CLI pin yields a restricted
// surface so that any interactive process able to launch the pinned CLI cannot
// drive the SYSTEM service's privileged capabilities.
type ClientRole string

const (
	RoleDesktop ClientRole = "desktop"
	RoleCLI     ClientRole = "cli"
)

// cliMethodAllowlist is the complete method surface exposed to the CLI role.
// Privileged or desktop-orchestration methods (tun.activate, mtu.set,
// host.shutdown, wfp.inspect, hotspot.inspect, steam_cdn.configure, dns.dohRelay)
// are intentionally absent and rejected with security_policy_rejected.
var cliMethodAllowlist = map[string]struct{}{
	api.MethodEngineHello:            {},
	api.MethodEngineStatus:           {},
	api.MethodEngineTelemetry:        {},
	api.MethodEngineStart:            {},
	api.MethodEngineStop:             {},
	api.MethodEngineScheduling:       {},
	api.MethodHealthCheck:            {},
	api.MethodDNSResolve:             {},
	api.MethodDNSStatus:              {},
	api.MethodDiagnosticRun:          {},
	api.MethodDomainQuarantineUpdate: {},
	api.MethodTunStatus:              {},
	api.MethodTunDeactivate:          {},
}

func roleAllowsMethod(role ClientRole, method string) bool {
	if role != RoleCLI {
		return true
	}
	_, ok := cliMethodAllowlist[method]
	return ok
}

// authorizeStartForRole restricts how the CLI role may start the engine: proxy
// mode only, bound to a loopback address. An empty listen host is allowed because
// the proxy config normalizes it to 127.0.0.1. The desktop role is unrestricted.
func authorizeStartForRole(role ClientRole, mode string, listenHost string) error {
	if role != RoleCLI {
		return nil
	}
	if mode != "proxy" {
		return errors.New("CLI clients may only start proxy mode; TUN orchestration is a desktop workflow")
	}
	host := strings.TrimSpace(listenHost)
	if host == "" {
		return nil
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return errors.New("CLI clients must bind the proxy to a loopback address")
	}
	return nil
}

// trimHelloForRole narrows engine.hello to the role's actual surface so protocol
// negotiation (and the CLI `raw` escape hatch) agrees with server-side
// enforcement: a restricted client is never advertised a method it cannot call.
func trimHelloForRole(result api.HelloResult, role ClientRole) api.HelloResult {
	if role != RoleCLI {
		return result
	}
	capabilities := make([]string, 0, len(result.Capabilities))
	for _, capability := range result.Capabilities {
		if roleAllowsMethod(RoleCLI, capability) {
			capabilities = append(capabilities, capability)
		}
	}
	result.Capabilities = capabilities
	result.Modes = []string{"proxy"}
	result.ModeFeatures = map[string][]string{"proxy": result.ModeFeatures["proxy"]}
	return result
}
