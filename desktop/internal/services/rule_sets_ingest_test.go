package services

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseSingBoxSourceSubscription(t *testing.T) {
	body := []byte(`{
		"version": 3,
		"rules": [
			{"domain_suffix": [".steampowered.com", ".steamcommunity.com"]},
			{"domain": ["example.com"]},
			{"domain_keyword": ["steam"]},
			{"ip_cidr": ["203.0.113.0/24"]},
			{"type": "logical", "mode": "and", "rules": []},
			{"port": [443]}
		]
	}`)
	result, err := parseRuleSetSubscription(body)
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != RuleSetFormatSingBoxSource {
		t.Fatalf("format = %q", result.Format)
	}
	// The logical and port rules are not representable headless entries and must
	// be counted as ignored instead of silently changing the list's meaning.
	if result.Entries != 5 || result.Ignored != 2 {
		t.Fatalf("entries = %d ignored = %d, want 5/2", result.Entries, result.Ignored)
	}
	payload, total, err := canonicalExternalRuleSetPayload(result.Rules)
	if err != nil {
		t.Fatal(err)
	}
	if total != result.Entries {
		t.Fatalf("canonical payload holds %d entries, want %d", total, result.Entries)
	}
	if _, err := decodeExternalRuleSetPayload(payload); err != nil {
		t.Fatalf("canonical payload rejected: %v", err)
	}
}

func TestParseClashProviderSubscription(t *testing.T) {
	body := []byte("# a comment\npayload:\n  - '+.steamcommunity.com'\n  - 'example.com'\n" +
		"  - DOMAIN-SUFFIX,steampowered.com\n  - DOMAIN-KEYWORD,steam\n" +
		"  - IP-CIDR,203.0.113.0/24,no-resolve\n  - GEOSITE,games\n  - ''\n")
	result, err := parseRuleSetSubscription(body)
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != RuleSetFormatClashProvider {
		t.Fatalf("format = %q", result.Format)
	}
	if result.Entries != 5 || result.Ignored != 1 {
		t.Fatalf("entries = %d ignored = %d, want 5/1", result.Entries, result.Ignored)
	}
	payload, _, err := canonicalExternalRuleSetPayload(result.Rules)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	merged := map[string][]string{}
	for _, rule := range document.Rules {
		for kind, raw := range rule {
			for _, value := range raw.([]any) {
				merged[kind] = append(merged[kind], value.(string))
			}
		}
	}
	want := map[string][]string{
		"domain":         {"example.com", "steamcommunity.com"},
		"domain_suffix":  {".steamcommunity.com", ".steampowered.com"},
		"domain_keyword": {"steam"},
		"ip_cidr":        {"203.0.113.0/24"},
	}
	if !reflect.DeepEqual(merged, want) {
		t.Fatalf("canonical entries = %#v, want %#v", merged, want)
	}
}

func TestCanonicalPayloadIsStableAcrossProviderReordering(t *testing.T) {
	first, _, err := canonicalExternalRuleSetPayload([]map[string]any{
		{"domain_suffix": []string{".a.example", ".b.example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := canonicalExternalRuleSetPayload([]map[string]any{
		{"domain_suffix": []string{".b.example"}},
		{"domain_suffix": []string{".a.example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("reordered provider produced a different payload:\n%s\n%s", first, second)
	}
}

func TestPublishExternalRuleSetSourceSkipsUnchangedContent(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	set := RuleSet{ID: "steam-id", Name: "Steam", Outbound: "direct"}
	payload, _, err := canonicalExternalRuleSetPayload([]map[string]any{
		{"domain_suffix": []string{".steamcommunity.com"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	directory := ruleSetDirectory()
	sha, err := publishExternalRuleSetSource(directory, set, payload)
	if err != nil {
		t.Fatal(err)
	}
	path := externalRuleSetSourcePathIn(directory, set)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	set.ContentSHA256 = sha
	if _, err := publishExternalRuleSetSource(directory, set, payload); err != nil {
		t.Fatal(err)
	}
	again, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(info.ModTime()) {
		t.Fatal("unchanged subscription rewrote the payload")
	}
}

func TestFetchRuleSetRejectsPrivateAndCleartextTargets(t *testing.T) {
	if err := validateRuleSetSourceURL("http://rules.example.test/list.json"); err == nil {
		t.Fatal("cleartext URL accepted")
	}
	// The loopback server stands in for a rebinding or pinned-host response: the
	// dial guard must stop the request no matter what the URL says.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("payload:\n  - example.com\n"))
	}))
	defer server.Close()
	set := RuleSet{ID: "one", Name: "List", URL: "https://rules.example.test/list.json"}
	if _, err := fetchRuleSet(context.Background(), set); err == nil {
		t.Fatal("unresolvable HTTPS host accepted")
	}
	set.URL = server.URL
	if _, err := fetchRuleSet(context.Background(), set); err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("loopback target not guarded: %v", err)
	}
}

func TestFetchRuleSetFollowsConditionals(t *testing.T) {
	// The real guard blocks loopback dials, so this test swaps in an unguarded
	// fetcher twin that keeps the conditional-request behaviour.
	payload := "payload:\n  - '+.steamcommunity.com'\n"
	etag := `"v1"`
	var requests int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()
	transport := server.Client().Transport.(*http.Transport)
	transport.TLSClientConfig.InsecureSkipVerify = true
	client := *server.Client()
	fetch := func(ctx context.Context, set RuleSet) (ruleSetFetchResult, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, set.URL, nil)
		if err != nil {
			return ruleSetFetchResult{}, err
		}
		if set.ETag != "" {
			request.Header.Set("If-None-Match", set.ETag)
		}
		response, err := client.Do(request)
		if err != nil {
			return ruleSetFetchResult{}, err
		}
		defer response.Body.Close()
		if response.StatusCode == http.StatusNotModified {
			return ruleSetFetchResult{NotModified: true, ETag: response.Header.Get("ETag")}, nil
		}
		body := make([]byte, 0, 256)
		buffer := make([]byte, 256)
		for {
			read, err := response.Body.Read(buffer)
			body = append(body, buffer[:read]...)
			if err != nil {
				break
			}
		}
		return ruleSetFetchResult{Body: body, ETag: response.Header.Get("ETag")}, nil
	}

	set := RuleSet{ID: "steam-id", Name: "Steam", URL: server.URL}
	first, err := fetch(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	if first.NotModified || !strings.Contains(string(first.Body), "steamcommunity.com") {
		t.Fatalf("first fetch = %+v", first)
	}
	set.ETag = first.ETag
	second, err := fetch(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	if !second.NotModified {
		t.Fatalf("conditional fetch returned body: %+v", second)
	}
	if requests != 2 {
		t.Fatalf("conditional header not honored: %d requests", requests)
	}
}

func TestRuleSetServiceUpdateNormalizesAndPublishes(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	adapters := NewAdapterService(settings)
	payload := "payload:\n  - '+.steamcommunity.com'\n"
	serverBody := payload
	var conditional string
	service := NewRuleSetService(settings, adapters)
	service.fetch = func(_ context.Context, set RuleSet) (ruleSetFetchResult, error) {
		if set.ETag == conditional && conditional != "" {
			return ruleSetFetchResult{NotModified: true, ETag: conditional}, nil
		}
		if serverBody == "" {
			return ruleSetFetchResult{}, errors.New("订阅服务器返回状态码 500")
		}
		return ruleSetFetchResult{Body: []byte(serverBody), ETag: "etag-1"}, nil
	}
	if _, err := service.Save([]RuleSet{{
		ID: "steam-id", Name: "Steam", URL: "https://rules.example.test/steam.yaml",
		Outbound: "direct", Priority: 20,
	}}); err != nil {
		t.Fatal(err)
	}

	list, err := service.Update("steam-id")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %d entries", len(list))
	}
	updated := list[0]
	if updated.EntryCount != 2 || updated.Format != RuleSetFormatClashProvider ||
		updated.LastError != "" || updated.ETag != "etag-1" || updated.UpdatedAt == 0 {
		t.Fatalf("ingestion status incomplete: %#v", updated)
	}
	sourcePath := externalRuleSetSourcePathIn(ruleSetDirectory(), updated)
	if data, err := os.ReadFile(sourcePath); err != nil || !strings.Contains(string(data), "steamcommunity.com") {
		t.Fatalf("payload not published: %v %s", err, sourcePath)
	}

	// A failed refetch must keep the previously published payload routing and
	// only move the error message.
	serverBody = ""
	if _, err := service.Update("steam-id"); err == nil {
		t.Fatal("failed refetch did not report an error")
	}
	list = service.List()
	if list[0].LastError == "" || list[0].EntryCount != 2 {
		t.Fatalf("failure handling wrong: %#v", list[0])
	}
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("published payload vanished after a failed update: %v", err)
	}

	// A conditional not-modified response refreshes the timestamp without work.
	serverBody = payload
	conditional = "etag-1"
	if _, err := service.Update("steam-id"); err != nil {
		t.Fatal(err)
	}
	list = service.List()
	if list[0].LastError != "" || list[0].ETag != "etag-1" {
		t.Fatalf("not-modified handling wrong: %#v", list[0])
	}
}

func TestRuleSetServiceSaveValidatesAndPersists(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	service := NewRuleSetService(settings, NewAdapterService(settings))
	if _, err := service.Save([]RuleSet{{
		Name: "Steam", URL: "http://rules.example.test/steam.yaml", Outbound: "direct",
	}}); err == nil {
		t.Fatal("cleartext URL accepted")
	}
	list, err := service.Save([]RuleSet{{
		ID: "", Name: " Steam ", URL: " https://rules.example.test/steam.yaml ", Outbound: "aggregation",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID == "" || list[0].Name != "Steam" {
		t.Fatalf("normalized save = %#v", list)
	}
	reloaded := NewSettingsService().Get().RuleSets
	if len(reloaded) != 1 || reloaded[0].ID != list[0].ID {
		t.Fatalf("rule sets did not persist: %#v", reloaded)
	}
}

func TestGuardRuleSetDialAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:443", "10.1.2.3:443", "192.168.1.1:443", "169.254.1.1:443", "::1:443"} {
		if err := guardRuleSetDialAddress("tcp", address, nil); err == nil {
			t.Fatalf("private address %s accepted", address)
		}
	}
	public := net.JoinHostPort("93.184.216.34", "443")
	if err := guardRuleSetDialAddress("tcp", public, nil); err != nil {
		t.Fatalf("public address rejected: %v", err)
	}
}

func TestFetchRuleSetTimesOutQuickly(t *testing.T) {
	set := RuleSet{ID: "one", Name: "List", URL: "https://hypomux.invalid/list.json"}
	start := time.Now()
	if _, err := fetchRuleSet(context.Background(), set); err == nil {
		t.Fatal("invalid host accepted")
	}
	if elapsed := time.Since(start); elapsed > ruleSetFetchTimeout {
		t.Fatalf("fetch hung for %v", elapsed)
	}
}

func TestRuleSetSourcePathStaysBesideWatchedFile(t *testing.T) {
	set := RuleSet{ID: "steam-id"}
	_, watched := externalRuleSetBindingIn(filepath.Join("dir"), set)
	source := externalRuleSetSourcePathIn(filepath.Join("dir"), set)
	if filepath.Base(watched) != "ext-0d1e5a582d0a4be0.json" && !strings.HasSuffix(watched, ".json") {
		t.Fatalf("unexpected watched path %q", watched)
	}
	if !strings.HasPrefix(filepath.Base(source), "ext-") || !strings.HasSuffix(source, ".source.json") {
		t.Fatalf("unexpected source path %q", source)
	}
}
