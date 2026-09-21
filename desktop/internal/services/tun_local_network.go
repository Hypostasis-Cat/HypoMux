package services

// Device discovery and link-local traffic belong to their original links,
// not to the aggregation SOCKS path. Keep the exclusion at the TUN boundary:
// a direct outbound after interception cannot preserve interface scope.
// Do not bypass RFC1918/ULA wholesale: that would swallow user routing rules
// and VPN subnets. These fixed exclusions also work for devices started later.
func tunRouteExclusions(dns dnsResolveResult, policy string, ipv6 bool) []string {
	exclusions := []string{
		"169.254.0.0/16",
		"224.0.0.0/4",
		"255.255.255.255/32",
	}
	if ipv6 {
		exclusions = append(exclusions, "fe80::/10", "ff00::/8")
	}
	if policy != "system" {
		exclusions = append(exclusions, dnsBootstrapRouteExclusions(dns)...)
	}
	return exclusions
}
