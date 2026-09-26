package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	ruleSetFetchTimeout = 45 * time.Second
	// Subscriptions are category lists, not backups; 16 MiB comfortably covers
	// every published domain list while keeping a hostile endpoint from pinning
	// memory with an endless body.
	ruleSetMaxBodyBytes = 16 << 20
	ruleSetMaxRedirects = 3
)

// externalRuleSetKinds is the closed set of match kinds a subscription may
// contribute. Everything else a provider ships is counted as ignored rather
// than silently rerouted.
var externalRuleSetKinds = []string{
	"domain", "domain_suffix", "domain_keyword", "domain_regex", "ip_cidr",
}

type ruleSetFetchResult struct {
	Body         []byte
	NotModified  bool
	ETag         string
	LastModified string
}

type ruleSetParseResult struct {
	Rules   []map[string]any
	Entries int
	Ignored int
	Format  string
}

// fetchRuleSet downloads the subscription over HTTPS. The dial guard rejects
// every non-public address, so a stored URL cannot be repointed at localhost or
// the LAN, and redirects cannot smuggle the request there either: every hop
// dials through the same guarded transport and must stay on HTTPS.
func fetchRuleSet(ctx context.Context, set RuleSet) (ruleSetFetchResult, error) {
	if err := validateRuleSetSourceURL(set.URL); err != nil {
		return ruleSetFetchResult{}, err
	}
	dialer := &net.Dialer{
		Timeout: 15 * time.Second,
		Control: guardRuleSetDialAddress,
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			DisableKeepAlives:     true,
			Proxy:                 nil,
			ForceAttemptHTTP2:     true,
			ResponseHeaderTimeout: 20 * time.Second,
		},
		Timeout: ruleSetFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("订阅地址重定向到了非 HTTPS 地址")
			}
			if len(via) > ruleSetMaxRedirects {
				return errors.New("订阅地址重定向次数过多")
			}
			return nil
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, set.URL, nil)
	if err != nil {
		return ruleSetFetchResult{}, fmt.Errorf("订阅地址无法构造请求：%w", err)
	}
	request.Header.Set("User-Agent", "HypoMux")
	request.Header.Set("Accept", "application/json, text/yaml, text/plain")
	if set.ETag != "" {
		request.Header.Set("If-None-Match", set.ETag)
	}
	if set.LastModified != "" {
		request.Header.Set("If-Modified-Since", set.LastModified)
	}
	response, err := client.Do(request)
	if err != nil {
		return ruleSetFetchResult{}, fmt.Errorf("下载订阅内容失败：%w", err)
	}
	defer response.Body.Close()
	result := ruleSetFetchResult{
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
	}
	if response.StatusCode == http.StatusNotModified {
		result.NotModified = true
		return result, nil
	}
	if response.StatusCode != http.StatusOK {
		return ruleSetFetchResult{}, fmt.Errorf("订阅服务器返回状态码 %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, ruleSetMaxBodyBytes+1))
	if err != nil {
		return ruleSetFetchResult{}, fmt.Errorf("读取订阅内容失败：%w", err)
	}
	if len(body) > ruleSetMaxBodyBytes {
		return ruleSetFetchResult{}, errors.New("订阅内容超过 16 MiB 上限")
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return ruleSetFetchResult{}, errors.New("订阅内容为空")
	}
	result.Body = body
	return result, nil
}

// guardRuleSetDialAddress runs after DNS resolution, which is the only point
// where a rebinding or pinned-host response can still be stopped.
func guardRuleSetDialAddress(network, address string, _ syscall.RawConn) error {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return fmt.Errorf("订阅下载不允许的网络类型：%s", network)
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("订阅下载收到无效地址：%s", address)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("订阅地址解析到了无效地址：%s", host)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("订阅地址解析到了不允许的内网地址：%s", host)
	}
	return nil
}

// parseRuleSetSubscription accepts the two provider formats the ecosystem
// publishes: a sing-box source rule-set (JSON, detected by its leading brace)
// and a Clash rule provider (YAML payload list). Format detection runs on the
// payload itself so a mislabelled Content-Type cannot break ingestion.
func parseRuleSetSubscription(body []byte) (ruleSetParseResult, error) {
	if !utf8.Valid(body) {
		return ruleSetParseResult{}, errors.New("订阅内容不是有效的 UTF-8 文本")
	}
	if strings.HasPrefix(strings.TrimLeft(string(body), " \t\r\n"), "{") {
		return parseSingBoxSourceSubscription(body)
	}
	return parseClashProviderSubscription(body)
}

func parseSingBoxSourceSubscription(body []byte) (ruleSetParseResult, error) {
	var document struct {
		Version int              `json:"version"`
		Rules   []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return ruleSetParseResult{}, fmt.Errorf("订阅内容不是有效的 sing-box 源格式：%w", err)
	}
	if document.Version < 1 || document.Version > singBoxRuleSetVersion {
		return ruleSetParseResult{}, fmt.Errorf("订阅内容的 sing-box 源版本 %d 不受支持", document.Version)
	}
	result := ruleSetParseResult{Format: RuleSetFormatSingBoxSource, Rules: []map[string]any{}}
	for _, rule := range document.Rules {
		entry, values, ok := externalEntryFromRule(rule)
		if ok {
			result.Rules = append(result.Rules, entry)
			result.Entries += values
		} else {
			result.Ignored++
		}
	}
	if result.Entries == 0 {
		return ruleSetParseResult{}, errors.New("订阅内容没有可用的域名或 IP 条目")
	}
	return result, nil
}

// externalEntryFromRule keeps a rule only when every field is a match kind the
// plan compiler can emit verbatim. Logical or exotic rules would either change
// the list's meaning or reference another rule-set, which sing-box rejects
// inside rule-set files.
func externalEntryFromRule(rule map[string]any) (map[string]any, int, bool) {
	entry := map[string]any{}
	values := 0
	for _, kind := range externalRuleSetKinds {
		raw, exists := rule[kind]
		if !exists {
			continue
		}
		list, ok := raw.([]any)
		if !ok {
			return nil, 0, false
		}
		cleaned := make([]string, 0, len(list))
		for _, item := range list {
			value, ok := item.(string)
			if !ok || strings.TrimSpace(value) == "" {
				return nil, 0, false
			}
			cleaned = append(cleaned, strings.TrimSpace(value))
		}
		if len(cleaned) == 0 {
			continue
		}
		entry[kind] = cleaned
		values += len(cleaned)
	}
	if len(entry) == 0 {
		return nil, 0, false
	}
	if _, ok := rule["ip_cidr"]; ok && !validIPCIDRValues(entry["ip_cidr"].([]string)) {
		return nil, 0, false
	}
	return entry, values, true
}

func validIPCIDRValues(values []string) bool {
	for _, value := range values {
		if net.ParseIP(value) == nil {
			if _, _, err := net.ParseCIDR(value); err != nil {
				return false
			}
		}
	}
	return true
}

// parseClashProviderSubscription reads the flat `payload:` list of a Clash rule
// provider. The format is line-oriented YAML, so a tolerant line parser covers
// everything real providers ship without pulling in a YAML dependency.
func parseClashProviderSubscription(body []byte) (ruleSetParseResult, error) {
	lines := strings.Split(string(body), "\n")
	start := -1
	for index, line := range lines {
		if trimmed := strings.TrimRight(line, "\r"); trimmed == "payload:" {
			start = index + 1
			break
		}
	}
	if start < 0 {
		return ruleSetParseResult{}, errors.New("订阅内容不是有效的 Clash 规则负载（缺少 payload 列表）")
	}
	result := ruleSetParseResult{Format: RuleSetFormatClashProvider, Rules: []map[string]any{}}
	for _, line := range lines[start:] {
		item := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if item == "" || strings.HasPrefix(item, "#") || strings.HasPrefix(item, "//") {
			continue
		}
		item = strings.TrimSpace(strings.TrimPrefix(item, "-"))
		item = strings.Trim(strings.TrimSpace(item), `"'`)
		if item == "" {
			continue
		}
		entry, ok := externalEntryFromClashItem(item)
		if !ok {
			result.Ignored++
			continue
		}
		result.Rules = append(result.Rules, entry)
		result.Entries++
	}
	if result.Entries == 0 {
		return ruleSetParseResult{}, errors.New("订阅 payload 中没有可用的域名或 IP 条目")
	}
	return result, nil
}

func externalEntryFromClashItem(item string) (map[string]any, bool) {
	upper := strings.ToUpper(item)
	switch {
	case strings.HasPrefix(upper, "DOMAIN-SUFFIX,"):
		return singleKindEntry("domain_suffix", suffixValue(item[len("DOMAIN-SUFFIX,"):]))
	case strings.HasPrefix(upper, "DOMAIN,"):
		return singleKindEntry("domain", normalizeRuleSetDomain(item[len("DOMAIN,"):]))
	case strings.HasPrefix(upper, "DOMAIN-KEYWORD,"):
		return singleKindEntry("domain_keyword", normalizeRuleSetDomain(item[len("DOMAIN-KEYWORD,"):]))
	case strings.HasPrefix(upper, "DOMAIN-REGEX,"):
		return singleKindEntry("domain_regex", strings.TrimSpace(item[len("DOMAIN-REGEX,"):]))
	case strings.HasPrefix(upper, "IP-CIDR6,"), strings.HasPrefix(upper, "IP-CIDR,"):
		value := item[strings.Index(item, ",")+1:]
		// Trailing options such as `no-resolve` are irrelevant for routing.
		if index := strings.Index(value, ","); index >= 0 {
			value = value[:index]
		}
		value = strings.TrimSpace(value)
		if _, _, err := net.ParseCIDR(value); err != nil {
			return nil, false
		}
		return singleKindEntry("ip_cidr", value)
	case strings.HasPrefix(item, "+."):
		// Clash's `+.example.com` matches the domain and every subdomain.
		domain := normalizeRuleSetDomain(item[2:])
		if domain == "" {
			return nil, false
		}
		return map[string]any{"domain": []string{domain}, "domain_suffix": []string{"." + domain}}, true
	case strings.HasPrefix(item, "*."), strings.HasPrefix(item, "."):
		dotted := item
		if strings.HasPrefix(item, "*.") {
			dotted = item[1:]
		}
		return singleKindEntry("domain_suffix", suffixValue(dotted))
	}
	if strings.ContainsAny(item, ",/") || strings.Contains(item, " ") {
		return nil, false
	}
	return singleKindEntry("domain", normalizeRuleSetDomain(item))
}

func singleKindEntry(kind, value string) (map[string]any, bool) {
	if value == "" {
		return nil, false
	}
	return map[string]any{kind: []string{value}}, true
}

func normalizeRuleSetDomain(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.Trim(value, ".")))
	if value == "" || strings.ContainsAny(value, " \t/\\") || !utf8.ValidString(value) {
		return ""
	}
	return value
}

func suffixValue(value string) string {
	domain := normalizeRuleSetDomain(value)
	if domain == "" {
		return ""
	}
	return "." + domain
}

// canonicalExternalRuleSetPayload collapses parsed entries into at most one
// rule per match kind, deduplicated and sorted, so the stored payload — and its
// digest — stays stable when a provider only reorders its list.
func canonicalExternalRuleSetPayload(entries []map[string]any) ([]byte, int, error) {
	merged := map[string][]string{}
	seen := map[string]map[string]struct{}{}
	total := 0
	for _, entry := range entries {
		for _, kind := range externalRuleSetKinds {
			values, ok := entry[kind].([]string)
			if !ok {
				continue
			}
			if seen[kind] == nil {
				seen[kind] = map[string]struct{}{}
			}
			for _, value := range values {
				if _, exists := seen[kind][value]; exists {
					continue
				}
				seen[kind][value] = struct{}{}
				merged[kind] = append(merged[kind], value)
				total++
			}
		}
	}
	rules := make([]map[string]any, 0, len(externalRuleSetKinds))
	for _, kind := range externalRuleSetKinds {
		if len(merged[kind]) == 0 {
			continue
		}
		sort.Strings(merged[kind])
		rules = append(rules, map[string]any{kind: merged[kind]})
	}
	payload, err := json.MarshalIndent(map[string]any{
		"version": singBoxRuleSetVersion,
		"rules":   rules,
	}, "", "  ")
	if err != nil {
		return nil, 0, fmt.Errorf("编码规则集载荷失败：%w", err)
	}
	return append(payload, '\n'), total, nil
}

// publishExternalRuleSetSource stores the normalized payload beside the watched
// rule-set files. The digest of the canonical payload decides whether the file
// needs rewriting, so an unchanged subscription never makes sing-box reload.
func publishExternalRuleSetSource(directory string, set RuleSet, payload []byte) (string, error) {
	digest := sha256.Sum256(payload)
	contentSHA256 := hex.EncodeToString(digest[:])
	path := externalRuleSetSourcePathIn(directory, set)
	if contentSHA256 == set.ContentSHA256 {
		if _, err := os.Stat(path); err == nil {
			return contentSHA256, nil
		}
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("创建规则集目录失败：%w", err)
	}
	if err := replaceFileAtomically(path, payload, 0o600); err != nil {
		return "", fmt.Errorf("写入规则集载荷失败：%w", err)
	}
	return contentSHA256, nil
}
