//go:build !windows

package services

func currentNATFirewallState() NATFirewallState {
	return NATFirewallState{Detail: "Host firewall integration is only available on Windows"}
}

func allowNATFirewallTraffic() (NATFirewallState, error) {
	return currentNATFirewallState(), nil
}
