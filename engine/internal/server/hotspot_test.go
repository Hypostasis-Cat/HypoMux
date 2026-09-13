package server

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestHotspotInspectionRequiresElevatedCore(t *testing.T) {
	s := New(strings.NewReader(""), io.Discard, Metadata{})
	s.identity.Elevated = false
	response, shutdown := s.handle(context.Background(), []byte(`{"protocol":1,"id":"inspect","method":"hotspot.inspect"}`))
	if shutdown || response.Error == nil || response.Error.Code != "elevation_required" {
		t.Fatalf("unexpected response: %+v", response)
	}
}
