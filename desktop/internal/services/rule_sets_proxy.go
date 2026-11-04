package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

var errRuleSetBlockedTarget = errors.New("订阅地址指向了不允许的内网地址")

// ruleSetConnectError marks a request that never got an HTTP answer. Only these
// may be retried directly: a status from the server would not change by route.
type ruleSetConnectError struct{ err error }

func (e *ruleSetConnectError) Error() string { return e.err.Error() }
func (e *ruleSetConnectError) Unwrap() error { return e.err }

// detectRuleSetProxy prefers the process environment, then the system proxy
// that tools such as Clash Verge Rev and v2rayN publish. net/http only knows
// the former, and a GUI launched from Explorer rarely has it set.
func detectRuleSetProxy(target *url.URL) *url.URL {
	if proxy, err := http.ProxyFromEnvironment(&http.Request{URL: target}); err == nil && proxy != nil {
		return proxy
	}
	return systemRuleSetProxy()
}

// fetchRuleSetWithFallback tries the proxy first and then the direct route, so
// a proxy left configured after its tool exited cannot break updates that used
// to work without it.
func fetchRuleSetWithFallback(
	ctx context.Context,
	set RuleSet,
	proxy *url.URL,
	attempt func(context.Context, RuleSet, *url.URL) (ruleSetFetchResult, error),
) (ruleSetFetchResult, error) {
	var connectErr *ruleSetConnectError
	if proxy == nil {
		result, err := attempt(ctx, set, nil)
		if errors.As(err, &connectErr) {
			return result, fmt.Errorf("%w（未检测到可用代理，本次为直连）", err)
		}
		return result, err
	}
	result, err := attempt(ctx, set, proxy)
	if !errors.As(err, &connectErr) || errors.Is(err, errRuleSetBlockedTarget) || ctx.Err() != nil {
		return result, err
	}
	result, directErr := attempt(ctx, set, nil)
	if directErr != nil {
		return result, fmt.Errorf("通过代理 %s %w；直连也失败：%v", proxy.Host, err, directErr)
	}
	return result, nil
}

// guardRuleSetProxyTarget is the proxied counterpart of the dial guard. The
// proxy resolves the name, so only the host as written can be checked here.
// Resolving it locally as well would refuse exactly the hosts a proxy is needed
// for: DNS poisoning commonly answers them with 0.0.0.0 or a loopback address.
func guardRuleSetProxyTarget(host string) error {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	literal, _, _ := strings.Cut(host, "%")
	if ip := net.ParseIP(literal); ip != nil {
		if isNonPublicRuleSetIP(ip) {
			return fmt.Errorf("%w：%s", errRuleSetBlockedTarget, host)
		}
		return nil
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return fmt.Errorf("%w：%s", errRuleSetBlockedTarget, host)
	}
	// A numeric last label is never a public name; proxies may read it as an
	// alternate IPv4 spelling such as 0x7f.1 or 127.1.
	last := host[strings.LastIndex(host, ".")+1:]
	if strings.HasPrefix(last, "0x") || strings.Trim(last, "0123456789") == "" {
		return fmt.Errorf("%w：%s", errRuleSetBlockedTarget, host)
	}
	return nil
}

// parseSystemProxyServer reads the Windows ProxyServer value, either a bare
// `host:port` or a `scheme=host:port;...` list. The subscription is always
// HTTPS, so the https entry wins and socks is the last resort.
func parseSystemProxyServer(server string) *url.URL {
	entries := map[string]string{}
	for _, part := range strings.Split(server, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		scheme, endpoint, found := strings.Cut(part, "=")
		if !found {
			scheme, endpoint = "", part
		}
		scheme = strings.ToLower(strings.TrimSpace(scheme))
		if _, exists := entries[scheme]; !exists {
			entries[scheme] = strings.TrimSpace(endpoint)
		}
	}
	for _, candidate := range []struct{ entry, scheme string }{
		{"https", "http"}, {"", "http"}, {"http", "http"}, {"socks", "socks5"},
	} {
		if endpoint := entries[candidate.entry]; endpoint != "" {
			if proxy := parseSystemProxyEndpoint(endpoint, candidate.scheme); proxy != nil {
				return proxy
			}
		}
	}
	return nil
}

func parseSystemProxyEndpoint(endpoint, scheme string) *url.URL {
	if !strings.Contains(endpoint, "://") {
		endpoint = scheme + "://" + endpoint
	}
	proxy, err := url.Parse(endpoint)
	if err != nil || proxy.Hostname() == "" || proxy.Port() == "" {
		return nil
	}
	switch strings.ToLower(proxy.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return proxy
	case "socks":
		proxy.Scheme = "socks5"
		return proxy
	}
	return nil
}
