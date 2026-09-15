package server

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestSchedulingRPCPreservesLifecycle(t *testing.T) {
	s := New(strings.NewReader(""), io.Discard, Metadata{Name: "test"})
	ctx := context.Background()
	request := []byte(`{"protocol":1,"id":"edit","method":"engine.scheduling","params":{"weighted":true,"adapters":[{"name":"b","source_ip":"127.0.0.2","weight":3}]}}`)
	response, _ := s.handle(ctx, request)
	if response.Error == nil {
		t.Fatal("update succeeded while stopped")
	}
	response, _ = s.handle(ctx, []byte(`{"protocol":1,"id":"start","method":"engine.start","params":{"adapters":[{"name":"a","source_ip":"127.0.0.1","weight":1}]}}`))
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	defer s.stopProxy("stop")
	state := s.runtime.Snapshot()
	proxy := s.proxy
	response, _ = s.handle(ctx, request)
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	if s.proxy != proxy || s.runtime.Snapshot() != state {
		t.Fatal("live update restarted the engine")
	}
	response, _ = s.handle(ctx, []byte(`{"protocol":1,"id":"invalid","method":"engine.scheduling","params":{"adapters":[]}}`))
	if response.Error == nil {
		t.Fatal("empty pool accepted")
	}
}
