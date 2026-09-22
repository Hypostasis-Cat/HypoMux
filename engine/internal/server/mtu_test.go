package server

import (
	"context"
	engineRuntime "github.com/Hypostasis-Cat/HypoMux/engine/internal/runtime"
	"io"
	"strings"
	"testing"
)

func TestMTURejectsUnprivilegedRunningAndInvalidRequests(t *testing.T) {
	for _, test := range []struct {
		name              string
		elevated, running bool
		want              string
	}{
		{"unprivileged", false, false, "elevation_required"},
		{"running", true, true, "engine_running"},
		{"invalid", true, false, "invalid_params"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := New(strings.NewReader(""), io.Discard, Metadata{})
			s.identity.Elevated = test.elevated
			if test.running {
				if _, err := s.runtime.Transition(engineRuntime.StateStarting, "test"); err != nil {
					t.Fatal(err)
				}
			}
			response, shutdown := s.handle(context.Background(), []byte(`{"protocol":1,"id":"mtu","method":"mtu.set","params":{"value":1}}`))
			if shutdown || response.Error == nil || response.Error.Code != test.want {
				t.Fatalf("unexpected response: %+v", response)
			}
		})
	}
}
