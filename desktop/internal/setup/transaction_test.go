package setup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakePlatform struct {
	root                    string
	service                 string
	stopError, restoreError error
	stops                   int
}

func (p *fakePlatform) Snapshot(string) (json.RawMessage, error) { return json.Marshal(p.service) }
func (p *fakePlatform) Quiesce() error                           { p.stops++; p.service = "disabled"; return p.stopError }
func (p *fakePlatform) Restore(_ string, b json.RawMessage) error {
	if p.restoreError != nil {
		return p.restoreError
	}
	return json.Unmarshal(b, &p.service)
}
func (*fakePlatform) Metadata(string) (json.RawMessage, error)      { return json.RawMessage(`null`), nil }
func (*fakePlatform) RestoreMetadata(string, json.RawMessage) error { return nil }
func (*fakePlatform) PrepareTarget(string) error                    { return nil }
func (p *fakePlatform) ValidateTarget(path string) error {
	r, e := filepath.Rel(p.root, path)
	if e != nil || strings.HasPrefix(r, "..") || filepath.IsAbs(r) {
		return errors.New("outside target root")
	}
	return nil
}
func fixture(t *testing.T) (*Transaction, []Item, *fakePlatform) {
	t.Helper()
	root := t.TempDir()
	install := filepath.Join(root, "安装 % & files")
	if e := os.MkdirAll(install, 0700); e != nil {
		t.Fatal(e)
	}
	journal := filepath.Join(root, "journal")
	if e := os.Mkdir(journal, 0700); e != nil {
		t.Fatal(e)
	}
	var items []Item
	for i, name := range []string{"hypomux.exe", "core.exe", "new.dll"} {
		target := filepath.Join(install, name)
		if i < 2 {
			mustWrite(t, target, "old "+name)
		}
		source := filepath.Join(root, "payload-"+name)
		mustWrite(t, source, "new "+name)
		items = append(items, Item{target, source})
	}
	p := &fakePlatform{root: install, service: "running-old"}
	return &Transaction{Root: journal, Platform: p}, items, p
}
func mustWrite(t *testing.T, path, s string) {
	t.Helper()
	if e := os.WriteFile(path, []byte(s), 0600); e != nil {
		t.Fatal(e)
	}
}
func mustText(t *testing.T, path, want string) {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil || string(b) != want {
		t.Fatalf("%s: %q %v want %q", path, b, e, want)
	}
}
func assertRestored(t *testing.T, items []Item, p *fakePlatform) {
	t.Helper()
	for _, item := range items[:2] {
		mustText(t, item.Target, "old "+filepath.Base(item.Target))
	}
	if _, e := os.Stat(items[2].Target); !os.IsNotExist(e) {
		t.Fatal("fresh file survived rollback")
	}
	if p.service != "running-old" {
		t.Fatal("service not restored", p.service)
	}
}
func TestTransactionRecoversEveryReplacementBoundary(t *testing.T) {
	for failAt := 0; failAt < 3; failAt++ {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			tx, items, p := fixture(t)
			if e := tx.Begin(items); e != nil {
				t.Fatal(e)
			}
			tx.AfterReplace = func(i int) error {
				if i == failAt {
					return errors.New("power loss")
				}
				return nil
			}
			if e := tx.Apply(); e == nil {
				t.Fatal("fault not injected")
			}
			// A new instance represents a new installer after process loss.
			recovered := Transaction{Root: tx.Root, Platform: p}
			if e := recovered.Recover(); e != nil {
				t.Fatal(e)
			}
			assertRestored(t, items, p)
			if e := recovered.Recover(); e != nil {
				t.Fatal(e)
			}
			assertRestored(t, items, p)
		})
	}
}
func TestTransactionPreparedAndServiceStopFailureRecover(t *testing.T) {
	for _, stopFailure := range []bool{false, true} {
		tx, items, p := fixture(t)
		if e := tx.Begin(items); e != nil {
			t.Fatal(e)
		}
		if stopFailure {
			p.stopError = errors.New("service stop failure")
			if e := tx.Apply(); e == nil {
				t.Fatal("ignored stop failure")
			}
			p.stopError = nil
		}
		if e := tx.Rollback(); e != nil {
			t.Fatal(e)
		}
		assertRestored(t, items, p)
	}
}
func TestTransactionCommitNeverRollsBack(t *testing.T) {
	tx, items, p := fixture(t)
	if e := tx.Begin(items); e != nil {
		t.Fatal(e)
	}
	if e := tx.Apply(); e != nil {
		t.Fatal(e)
	}
	p.service = "running-new"
	if e := tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e := tx.Recover(); e != nil {
		t.Fatal(e)
	}
	for _, i := range items {
		mustText(t, i.Target, "new "+filepath.Base(i.Target))
	}
	if p.service != "running-new" {
		t.Fatal("committed service reverted")
	}
}
func TestTransactionRejectsCorruptedStageBeforeStopping(t *testing.T) {
	tx, items, p := fixture(t)
	if e := tx.Begin(items); e != nil {
		t.Fatal(e)
	}
	mustWrite(t, filepath.Join(tx.Root, "new-2"), "corrupt")
	if e := tx.Apply(); e == nil {
		t.Fatal("accepted corruption")
	}
	if p.stops != 0 {
		t.Fatal("stopped before validating full payload")
	}
	assertRestored(t, items, p)
}
func TestTransactionRetainsBackupsOnFailedRecovery(t *testing.T) {
	tx, items, p := fixture(t)
	if e := tx.Begin(items); e != nil {
		t.Fatal(e)
	}
	if e := tx.Apply(); e != nil {
		t.Fatal(e)
	}
	p.restoreError = errors.New("registry unavailable")
	if e := tx.Rollback(); e == nil {
		t.Fatal("ignored restore failure")
	}
	mustText(t, filepath.Join(tx.Root, "old-0"), "old hypomux.exe")
	p.restoreError = nil
	if e := tx.Recover(); e != nil {
		t.Fatal(e)
	}
	assertRestored(t, items, p)
}
func TestTransactionCorruptBackupDoesNotOverwriteAnyTarget(t *testing.T) {
	tx, items, _ := fixture(t)
	if e := tx.Begin(items); e != nil {
		t.Fatal(e)
	}
	if e := tx.Apply(); e != nil {
		t.Fatal(e)
	}
	mustWrite(t, filepath.Join(tx.Root, "old-1"), "corrupt")
	if e := tx.Rollback(); e == nil {
		t.Fatal("accepted bad backup")
	}
	for _, i := range items {
		mustText(t, i.Target, "new "+filepath.Base(i.Target))
	}
}
func TestTransactionRejectsEscapingJournal(t *testing.T) {
	tx, items, _ := fixture(t)
	if e := tx.Begin(items); e != nil {
		t.Fatal(e)
	}
	j, e := tx.load()
	if e != nil {
		t.Fatal(e)
	}
	j.Items[0].Backup = "../victim"
	if e = tx.save(j); e != nil {
		t.Fatal(e)
	}
	if e = tx.Recover(); e == nil {
		t.Fatal("accepted escaping backup")
	}
}
func TestTransactionCommitRechecksInstalledBytes(t *testing.T) {
	tx, items, _ := fixture(t)
	if e := tx.Begin(items); e != nil {
		t.Fatal(e)
	}
	if e := tx.Apply(); e != nil {
		t.Fatal(e)
	}
	mustWrite(t, items[0].Target, "quarantined")
	if e := tx.Commit(); e == nil {
		t.Fatal("committed damaged binary")
	}
	if e := tx.Rollback(); e != nil {
		t.Fatal(e)
	}
}

func TestTransactionStopFailureRestoresUnchangedServiceConfiguration(t *testing.T) {
	tx, items, p := fixture(t)
	if e := tx.Begin(items); e != nil {
		t.Fatal(e)
	}
	p.stopError = errors.New("stop stuck")
	if e := tx.Apply(); e == nil {
		t.Fatal("stop fault ignored")
	}
	if e := tx.Rollback(); e == nil {
		t.Fatal("must report incomplete stop")
	}
	assertRestored(t, items, p)
	p.stopError = nil
	if e := tx.Recover(); e != nil {
		t.Fatal(e)
	}
	assertRestored(t, items, p)
}

func TestTransactionCompletedJournalDoesNotInspectUnavailableOldTargets(t *testing.T) {
	for _, phase := range []string{"committed", "rolled-back"} {
		t.Run(phase, func(t *testing.T) {
			tx, items, p := fixture(t)
			if e := tx.Begin(items); e != nil {
				t.Fatal(e)
			}
			if phase == "committed" {
				if e := tx.Apply(); e != nil {
					t.Fatal(e)
				}
				if e := tx.Commit(); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := tx.Rollback(); e != nil {
					t.Fatal(e)
				}
			}
			// Simulate a moved/unavailable old destination: the platform now rejects it.
			p.root = t.TempDir()
			before := p.stops
			if e := tx.Recover(); e != nil {
				t.Fatalf("completed history blocked recovery: %v", e)
			}
			if p.stops != before {
				t.Fatal("completed history changed the service")
			}
			target := filepath.Join(p.root, "new.exe")
			source := items[0].Source
			if e := tx.Begin([]Item{{target, source}}); e != nil {
				t.Fatalf("completed old journal blocked a new destination: %v", e)
			}
		})
	}
}

func TestTransactionRejectsOversizedJournalEvenWithValidPrefix(t *testing.T) {
	tx, items, _ := fixture(t)
	if e := tx.Begin(items); e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(filepath.Join(tx.Root, "journal.json"), os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.WriteString(strings.Repeat(" ", 4<<20))
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Recover(); e == nil {
		t.Fatal("oversized journal accepted")
	}
}
