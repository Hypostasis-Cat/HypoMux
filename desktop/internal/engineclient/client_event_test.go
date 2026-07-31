package engineclient

import (
	"bytes"
	"testing"
	"time"
)

func TestReadLoopDeliversCoreEventsWithoutTreatingThemAsResponses(t *testing.T) {
	client := newClient(countingLauncher{}, countingLauncher{})
	session := &coreSession{
		reader: bytes.NewBufferString(
			"{\"protocol\":1,\"sequence\":7,\"event\":\"dns.fallback_required\",\"data\":{\"adapter\":\"WLAN\"}}\n",
		),
		close: func() error { return nil },
	}
	client.session = session
	go client.readLoop(session)

	select {
	case event := <-client.Events():
		if event.Name != "dns.fallback_required" || event.Sequence != 7 ||
			!bytes.Contains(event.Data, []byte(`"adapter":"WLAN"`)) {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("core event was not delivered")
	}
}
