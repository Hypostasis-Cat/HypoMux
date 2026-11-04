//go:build windows

package services

import "testing"

func TestResolveSystemRuleSetProxy(t *testing.T) {
	const owned = "http=127.0.0.1:10801;https=127.0.0.1:10801;socks=127.0.0.1:10800"
	enabled := func(server string) proxyRegistryValues {
		return proxyRegistryValues{HasEnable: true, Enable: 1, HasServer: true, Server: server}
	}
	verge := enabled("127.0.0.1:7897")
	for _, test := range []struct {
		name     string
		current  proxyRegistryValues
		original proxyRegistryValues
		marker   bool
		want     string
	}{
		{name: "clash verge system proxy", current: verge, want: "127.0.0.1:7897"},
		{name: "v2rayN system proxy", current: enabled("127.0.0.1:10808"), want: "127.0.0.1:10808"},
		{name: "system proxy switched off", current: proxyRegistryValues{HasEnable: true, HasServer: true, Server: "127.0.0.1:7897"}},
		{name: "no proxy values at all"},
		{name: "takeover looks through to the previous proxy", current: enabled(owned), original: verge, marker: true, want: "127.0.0.1:7897"},
		{name: "takeover with no previous proxy", current: enabled(owned), original: proxyRegistryValues{HasEnable: true, HasServer: true, Server: "127.0.0.1:7897"}, marker: true},
		{name: "takeover without a recovery marker", current: enabled(owned)},
		{name: "stale takeover recorded as the previous proxy", current: enabled(owned), original: enabled(owned), marker: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			proxy := resolveSystemRuleSetProxy(test.current, func() (proxyRegistryValues, bool) { return test.original, test.marker })
			got := ""
			if proxy != nil {
				got = proxy.Host
			}
			if got != test.want {
				t.Fatalf("proxy = %q, want %q", got, test.want)
			}
		})
	}
}
