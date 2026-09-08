package server

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
)

func TestSteamCDNRuntimeToggleProtocol(t *testing.T) {
	s := New(strings.NewReader(""), io.Discard, Metadata{})
	p, err := proxy.New(proxy.Config{Adapters: []proxy.Adapter{{Name: "test", SourceIP: "127.0.0.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = p.Stop(ctx)
	}()
	s.proxy = p
	for _, test := range []struct {
		value   string
		enabled bool
	}{{`{"enabled":true}`, true}, {`{"reset":true}`, true}, {`{"enabled":false}`, false}, {`{}`, false}} {
		response, _ := s.handle(context.Background(), []byte(`{"protocol":1,"id":"cdn","method":"steam_cdn.configure","params":`+test.value+`}`))
		if response.Error != nil {
			t.Fatal(response.Error)
		}
		status, ok := response.Result.(proxy.SteamCDNStatus)
		if !ok || status.Enabled != test.enabled {
			t.Fatal(response.Result)
		}
	}
	response, _ := s.handle(context.Background(), []byte(`{"protocol":1,"id":"cdn","method":"steam_cdn.configure","params":{"enabled":"yes"}}`))
	if response.Error == nil || response.Error.Code != "invalid_params" {
		t.Fatal(response)
	}
}
