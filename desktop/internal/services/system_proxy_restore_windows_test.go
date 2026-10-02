//go:build windows

package services

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

type fakeProxyRegistry struct {
	values  proxyRegistryValues
	failAt  string
	readErr error
	writes  int
}

func (k *fakeProxyRegistry) GetIntegerValue(string) (uint64, uint32, error) {
	if k.readErr != nil {
		return 0, 0, k.readErr
	}
	if !k.values.HasEnable {
		return 0, 0, registry.ErrNotExist
	}
	return k.values.Enable, registry.DWORD, nil
}
func (k *fakeProxyRegistry) GetStringValue(name string) (string, uint32, error) {
	if name == "ProxyServer" {
		if !k.values.HasServer {
			return "", 0, registry.ErrNotExist
		}
		return k.values.Server, registry.SZ, nil
	}
	if !k.values.HasOverride {
		return "", 0, registry.ErrNotExist
	}
	return k.values.Override, registry.SZ, nil
}
func (k *fakeProxyRegistry) write(name string) error {
	k.writes++
	if k.failAt == name {
		k.failAt = ""
		return errors.New("injected registry write failure")
	}
	return nil
}
func (k *fakeProxyRegistry) SetDWordValue(name string, value uint32) error {
	if err := k.write(name); err != nil {
		return err
	}
	k.values.HasEnable, k.values.Enable = true, uint64(value)
	return nil
}
func (k *fakeProxyRegistry) SetStringValue(name, value string) error {
	if err := k.write(name); err != nil {
		return err
	}
	if name == "ProxyServer" {
		k.values.HasServer, k.values.Server = true, value
	} else {
		k.values.HasOverride, k.values.Override = true, value
	}
	return nil
}
func (k *fakeProxyRegistry) DeleteValue(name string) error {
	if err := k.write(name); err != nil {
		return err
	}
	switch name {
	case "ProxyEnable":
		k.values.HasEnable, k.values.Enable = false, 0
	case "ProxyServer":
		k.values.HasServer, k.values.Server = false, ""
	case "ProxyOverride":
		k.values.HasOverride, k.values.Override = false, ""
	}
	return nil
}

func proxyRecoveryTestFixture(t *testing.T) (*fakeProxyRegistry, proxySnapshot, proxyRecoveryActions) {
	t.Helper()
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	snapshot := proxySnapshot{Version: 2, State: "active", OwnedServer: "http=127.0.0.1:10801", OwnedOverride: systemProxyBypass,
		HasProxyEnable: true, ProxyEnable: 0, HasProxyServer: true, ProxyServer: "original.example:8080", HasProxyOverride: true, ProxyOverride: "original-bypass.example"}
	key := &fakeProxyRegistry{values: proxyRegistryValues{true, 1, true, snapshot.OwnedServer, true, systemProxyBypass}}
	if err := saveProxySnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	return key, snapshot, proxyRecoveryActions{save: saveProxySnapshot, notify: func() error { return nil }, remove: func() error { return os.Remove(proxyMarkerPath()) }}
}

func readProxyRecoveryTestMarker(t *testing.T) proxySnapshot {
	t.Helper()
	data, err := os.ReadFile(proxyMarkerPath())
	if err != nil {
		t.Fatal(err)
	}
	var snapshot proxySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestProxyRestoreRetriesEveryInterruptedStage(t *testing.T) {
	for _, phase := range []string{"ProxyEnable", "ProxyServer", "ProxyOverride", "notify", "remove"} {
		t.Run(phase, func(t *testing.T) {
			key, snapshot, actions := proxyRecoveryTestFixture(t)
			switch phase {
			case "notify":
				actions.notify = func() error { return errors.New("injected notify failure") }
			case "remove":
				actions.remove = func() error { return errors.New("injected cleanup failure") }
			default:
				key.failAt = phase
			}
			if _, err := restoreProxySnapshot(key, snapshot, actions); err == nil {
				t.Fatal("expected injected failure")
			}
			journal := readProxyRecoveryTestMarker(t)
			if journal.State != "restoring" || journal.RestoreFrom == nil {
				t.Fatalf("missing journal: %+v", journal)
			}
			actions.notify = func() error { return nil }
			actions.remove = func() error { return os.Remove(proxyMarkerPath()) }
			if message, err := restoreProxySnapshot(key, journal, actions); err != nil || message != "" {
				t.Fatalf("retry: %q %v", message, err)
			}
			if key.values != proxyOriginalValues(snapshot) {
				t.Fatalf("incomplete restore: %+v", key.values)
			}
			if _, err := os.Stat(proxyMarkerPath()); !os.IsNotExist(err) {
				t.Fatal("completed journal retained")
			}
		})
	}
}

func TestProxyRestoreJournalFailureDoesNotWriteRegistry(t *testing.T) {
	key, snapshot, actions := proxyRecoveryTestFixture(t)
	before := key.values
	actions.save = func(proxySnapshot) error { return errors.New("disk full") }
	if _, err := restoreProxySnapshot(key, snapshot, actions); err == nil {
		t.Fatal("journal failure ignored")
	}
	if key.writes != 0 || key.values != before || readProxyRecoveryTestMarker(t).State != "active" {
		t.Fatal("registry changed before journal commit")
	}
}

func TestProxyRestorePreservesExternalEditsDuringRetry(t *testing.T) {
	for _, field := range []string{"ProxyServer", "ProxyEnable", "ProxyOverride"} {
		t.Run(field, func(t *testing.T) {
			key, snapshot, actions := proxyRecoveryTestFixture(t)
			key.failAt = "ProxyOverride"
			if _, err := restoreProxySnapshot(key, snapshot, actions); err == nil {
				t.Fatal("expected interruption")
			}
			journal := readProxyRecoveryTestMarker(t)
			switch field {
			case "ProxyServer":
				key.values.Server = "user.example:9999"
			case "ProxyEnable":
				key.values.Enable = 2
			case "ProxyOverride":
				key.values.Override = "user-bypass"
			}
			before, writes := key.values, key.writes
			message, err := restoreProxySnapshot(key, journal, actions)
			if err != nil || message == "" {
				t.Fatalf("external edit not detected: %q %v", message, err)
			}
			if key.values != before || key.writes != writes {
				t.Fatal("external settings overwritten")
			}
		})
	}
}

func TestProxyRestoreActiveMarkerHonorsUserChangesBackToOriginal(t *testing.T) {
	for _, field := range []string{"ProxyEnable", "ProxyServer", "ProxyOverride"} {
		t.Run(field, func(t *testing.T) {
			key, snapshot, actions := proxyRecoveryTestFixture(t)
			switch field {
			case "ProxyEnable":
				key.values.Enable = snapshot.ProxyEnable
			case "ProxyServer":
				key.values.Server = snapshot.ProxyServer
			case "ProxyOverride":
				key.values.Override = snapshot.ProxyOverride
			}
			before := key.values
			message, err := restoreProxySnapshot(key, snapshot, actions)
			if err != nil || message == "" || key.writes != 0 || key.values != before {
				t.Fatalf("user changes overwritten: %q %v %+v", message, err, key.values)
			}
		})
	}
}

func TestProxyRestoreLegacyAndPreparedMarkers(t *testing.T) {
	for _, state := range []string{"active", "prepared", "legacy"} {
		t.Run(state, func(t *testing.T) {
			key, snapshot, actions := proxyRecoveryTestFixture(t)
			snapshot.Version, snapshot.State, snapshot.OwnedOverride = 1, state, ""
			key.values.Enable, key.values.Server, key.values.Override = snapshot.ProxyEnable, snapshot.ProxyServer, legacySystemProxyBypass
			if state == "legacy" {
				snapshot.Version, snapshot.State, snapshot.OwnedServer = 0, "", ""
				key.values.Server = "legacy-owned-proxy"
			}
			if message, err := restoreProxySnapshot(key, snapshot, actions); err != nil || message != "" {
				t.Fatalf("legacy restore: %q %v", message, err)
			}
			if key.values != proxyOriginalValues(snapshot) {
				t.Fatal("legacy partial restore lost")
			}
		})
	}
}

func TestProxyRestoreMissingOriginalValuesAndReadFailure(t *testing.T) {
	key, snapshot, actions := proxyRecoveryTestFixture(t)
	key.readErr = errors.New("access denied")
	if _, err := restoreProxySnapshot(key, snapshot, actions); err == nil {
		t.Fatal("read failure ignored")
	}
	if key.writes != 0 {
		t.Fatal("read failure caused writes")
	}
	if _, err := os.Stat(proxyMarkerPath()); err != nil {
		t.Fatal("read failure discarded marker")
	}
	key.readErr = nil
	snapshot.HasProxyEnable, snapshot.HasProxyServer, snapshot.HasProxyOverride = false, false, false
	snapshot.ProxyEnable, snapshot.ProxyServer, snapshot.ProxyOverride = 0, "", ""
	if _, err := restoreProxySnapshot(key, snapshot, actions); err != nil {
		t.Fatal(err)
	}
	if key.values != (proxyRegistryValues{}) {
		t.Fatalf("missing original values not deleted: %+v", key.values)
	}
}

func TestSystemProxyBypassMatchesOnlyIntended172PrivateRange(t *testing.T) {
	for _, test := range []struct {
		address string
		want    bool
	}{
		{"172.16.0.1", true}, {"172.20.0.1", true}, {"172.29.0.1", true}, {"172.31.255.254", true},
		{"172.2.0.1", false}, {"172.200.0.1", false}, {"172.15.0.1", false}, {"172.32.0.1", false},
	} {
		matched := false
		for _, pattern := range strings.Split(systemProxyBypass, ";") {
			ok, _ := path.Match(pattern, test.address)
			matched = matched || ok
		}
		if matched != test.want {
			t.Errorf("bypass %s = %t", test.address, matched)
		}
	}
}
