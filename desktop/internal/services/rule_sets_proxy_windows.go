//go:build windows

package services

import (
	"encoding/json"
	"net/url"
	"os"

	"golang.org/x/sys/windows/registry"
)

func systemRuleSetProxy() *url.URL {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	current, err := readProxyRegistry(key)
	if err != nil {
		return nil
	}
	return resolveSystemRuleSetProxy(current, func() (proxyRegistryValues, bool) {
		data, err := os.ReadFile(proxyMarkerPath())
		if err != nil {
			return proxyRegistryValues{}, false
		}
		var snapshot proxySnapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return proxyRegistryValues{}, false
		}
		return proxyOriginalValues(snapshot), true
	})
}

// resolveSystemRuleSetProxy looks through HypoMux's own takeover: its local
// proxy egresses on the same blocked network, so the proxy the user had before
// the takeover, still running beside us, is the one that can reach the source.
func resolveSystemRuleSetProxy(current proxyRegistryValues, original func() (proxyRegistryValues, bool)) *url.URL {
	if !current.HasEnable || current.Enable == 0 || !current.HasServer {
		return nil
	}
	if !hypoMuxProxyServerPattern.MatchString(current.Server) {
		return parseSystemProxyServer(current.Server)
	}
	previous, ok := original()
	if !ok || !previous.HasEnable || previous.Enable == 0 || !previous.HasServer || hypoMuxProxyServerPattern.MatchString(previous.Server) {
		return nil
	}
	return parseSystemProxyServer(previous.Server)
}
