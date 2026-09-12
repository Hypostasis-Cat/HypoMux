package startup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const automaticProfileXML = `<WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1"><connectionType>ESS</connectionType><connectionMode>auto</connectionMode></WLANProfile>`

type fakeWiFiSession struct {
	items  []wifiInterface
	saved  []wifiProfile
	calls  []string
	err    error
	closed int
}

func (s *fakeWiFiSession) interfaces() ([]wifiInterface, error)   { return s.items, nil }
func (s *fakeWiFiSession) profiles(string) ([]wifiProfile, error) { return s.saved, nil }
func (s *fakeWiFiSession) connect(id, name string) error {
	s.calls = append(s.calls, id+":"+name)
	return s.err
}
func (s *fakeWiFiSession) close() { s.closed++ }

func TestWiFiConnectUsesSelectedAdapterAndAutomaticProfilesInPreferenceOrder(t *testing.T) {
	now := time.Now()
	session := &fakeWiFiSession{
		items: []wifiInterface{{"chosen", "Renamed wireless", 4}, {"other", "WLAN 2", 4}},
		saved: []wifiProfile{{"manual", strings.ReplaceAll(automaticProfileXML, "auto", "manual")}, {"home", automaticProfileXML}, {"office", automaticProfileXML}},
	}
	connector := &WiFiConnector{open: func() (wifiSession, error) { return session, nil }, now: func() time.Time { return now }, attempts: map[string]wifiAttempt{}}
	request := func() {
		t.Helper()
		if err := connector.TryConnect(context.Background(), []string{"Renamed wireless"}); err != nil {
			t.Fatal(err)
		}
	}
	request()
	request()
	if len(session.calls) != 1 || session.calls[0] != "chosen:home" {
		t.Fatalf("unexpected connections: %#v", session.calls)
	}
	now = now.Add(21 * time.Second)
	request()
	if len(session.calls) != 2 || session.calls[1] != "chosen:office" {
		t.Fatalf("did not try alternate saved profile: %#v", session.calls)
	}
	for _, state := range []uint32{1, 2, 3, 5, 6, 7} {
		session.items[0].state = state
		now = now.Add(time.Minute)
		request()
	}
	if len(session.calls) != 2 {
		t.Fatal("interrupted an existing connection or connection attempt")
	}
	if session.closed != 9 {
		t.Fatalf("session leak: closed %d", session.closed)
	}
}

func TestWiFiConnectCancellationAndNoSelectionDoNotOpenWLAN(t *testing.T) {
	connector := &WiFiConnector{open: func() (wifiSession, error) { t.Fatal("unexpected WLAN access"); return nil, nil }}
	if err := connector.TryConnect(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := connector.TryConnect(ctx, []string{"WLAN"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestWiFiConnectReportsMissingProfilesAndPolicyFailureWithoutChangingProfiles(t *testing.T) {
	session := &fakeWiFiSession{items: []wifiInterface{{"id", "WLAN", 4}}}
	connector := &WiFiConnector{open: func() (wifiSession, error) { return session, nil }, now: time.Now, attempts: map[string]wifiAttempt{}}
	if err := connector.TryConnect(context.Background(), []string{"WLAN"}); err == nil || !strings.Contains(err.Error(), "自动连接") {
		t.Fatal(err)
	}
	session.saved = []wifiProfile{{"saved", automaticProfileXML}}
	session.err = errors.New("policy prevents connection")
	if err := connector.TryConnect(context.Background(), []string{"WLAN"}); !errors.Is(err, session.err) {
		t.Fatal(err)
	}
}

func TestAutomaticWiFiProfileRejectsManualAdHocAndMalformedProfiles(t *testing.T) {
	for _, payload := range []string{"", "<broken", strings.ReplaceAll(automaticProfileXML, "auto", "manual"), strings.ReplaceAll(automaticProfileXML, "ESS", "IBSS")} {
		if automaticWiFiProfile(payload) {
			t.Fatalf("accepted profile: %s", payload)
		}
	}
}
