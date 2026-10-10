package dns

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func parseDoTURL(value string) (Endpoint, string, error) {
	value = strings.TrimSpace(value)
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "tls" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || strings.ContainsAny(value, "?#") || len(value) > 2048 {
		return Endpoint{}, "", fmt.Errorf("invalid DoT URL: use tls://hostname[:port] without credentials, path, query or fragment")
	}
	port := 853
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
	}
	if err != nil || port < 1 || port > 65535 || strings.HasSuffix(u.Host, ":") {
		return Endpoint{}, "", fmt.Errorf("invalid DoT port")
	}
	host := strings.ToLower(u.Hostname())
	endpoint := Endpoint{Host: host, Port: port}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			return Endpoint{}, "", fmt.Errorf("invalid DoT IP address")
		}
		endpoint.IP, endpoint.Host = ip.String(), ip.String()
	} else {
		normalized, err := normalizeDomain(host)
		if err != nil || strings.Trim(host, "0123456789.") == "" || normalized != strings.TrimSuffix(host, ".") {
			return Endpoint{}, "", fmt.Errorf("invalid DoT hostname")
		}
	}
	u.Host = net.JoinHostPort(endpoint.Host, strconv.Itoa(port))
	return endpoint, u.String(), nil
}

func ConfigDoTEndpoints(config Config) []Endpoint {
	if config.Policy != PolicyDoT {
		return nil
	}
	var result []Endpoint
	for _, value := range config.DoTServers {
		endpoint, _, err := parseDoTURL(value)
		if err == nil {
			result = append(result, endpoint)
		}
	}
	return result
}
