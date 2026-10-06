// Package setup owns the durable file transaction used by the Windows installer.
// The UI and legacy migration code must not delete its journal or backups.
package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Item struct {
	Target string
	Source string
}
type Entry struct {
	Target   string
	Existed  bool
	Backup   string
	OldHash  string
	Staged   string
	Hash     string
	Metadata json.RawMessage
}
type Journal struct {
	Version int
	Phase   string
	Items   []Entry
	System  json.RawMessage
	Created time.Time
}

// Platform is deliberately injectable: fault tests never touch real services.
type Platform interface {
	Snapshot(string) (json.RawMessage, error)
	Quiesce() error
	Restore(string, json.RawMessage) error
	Metadata(string) (json.RawMessage, error)
	RestoreMetadata(string, json.RawMessage) error
	PrepareTarget(string) error
	ValidateTarget(string) error
}
type Transaction struct {
	Root         string
	Platform     Platform
	AfterReplace func(int) error
}

func hashFile(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func copyFile(src, dst string) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
		return e
	}
	flags := os.O_TRUNC | os.O_WRONLY
	if _, err := os.Lstat(dst); errors.Is(err, os.ErrNotExist) {
		flags |= os.O_CREATE | os.O_EXCL
	} else if err != nil {
		return err
	}
	out, e := os.OpenFile(dst, flags, 0600)
	if e != nil {
		return e
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	return errors.Join(copyErr, syncErr, closeErr)
}
func (t *Transaction) log(message string) {
	f, e := os.OpenFile(filepath.Join(t.Root, "setup.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e == nil {
		fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), message)
		_ = f.Sync()
		_ = f.Close()
	}
}
func (t *Transaction) save(j *Journal) error {
	b, e := json.MarshalIndent(j, "", "  ")
	if e != nil {
		return e
	}
	path := filepath.Join(t.Root, "journal.json")
	f, e := os.OpenFile(path+".new", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, we := f.Write(b)
	se := f.Sync()
	ce := f.Close()
	if e = errors.Join(we, se, ce); e != nil {
		return e
	}
	return os.Rename(path+".new", path)
}
func (t *Transaction) load() (*Journal, error) {
	f, e := os.Open(filepath.Join(t.Root, "journal.json"))
	if e != nil {
		return nil, e
	}
	defer f.Close()
	var j Journal
	info, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if info.Size() > 4<<20 {
		return nil, errors.New("oversized setup journal")
	}
	d := json.NewDecoder(io.LimitReader(f, 4<<20))
	d.DisallowUnknownFields()
	if e = d.Decode(&j); e != nil {
		return nil, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return nil, errors.New("trailing or oversized setup journal data")
	}
	if j.Version != 1 || len(j.Items) == 0 || len(j.Items) > 64 {
		return nil, errors.New("unsupported setup journal")
	}
	switch j.Phase {
	case "prepared", "applying", "rolling-back", "committed", "rolled-back":
	default:
		return nil, errors.New("invalid setup phase")
	}
	seen := map[string]bool{}
	for i, v := range j.Items {
		key := strings.ToLower(filepath.Clean(v.Target))
		if seen[key] {
			return nil, errors.New("duplicate recovery target")
		}
		seen[key] = true
		// Completed journals only clean their own backups. An old destination
		// can have been removed, moved, or disconnected since installation.
		if j.Phase != "committed" && j.Phase != "rolled-back" {
			if e = t.Platform.ValidateTarget(v.Target); e != nil {
				return nil, e
			}
		}
		if v.Backup != fmt.Sprintf("old-%d", i) || (v.Staged != "" && v.Staged != fmt.Sprintf("new-%d", i)) {
			return nil, errors.New("invalid backup path")
		}
	}
	return &j, nil
}

// Begin validates and stages EVERY file before persisting a prepared journal.
// No target files or services are changed while preparing the transaction.
func (t *Transaction) Begin(items []Item) error {
	if err := t.Recover(); err != nil {
		return err
	}
	if len(items) == 0 || len(items) > 64 {
		return errors.New("invalid payload plan")
	}
	j := &Journal{Version: 1, Phase: "prepared", Created: time.Now()}
	seen := map[string]bool{}
	for i, item := range items {
		if err := t.Platform.ValidateTarget(item.Target); err != nil {
			return err
		}
		key := strings.ToLower(filepath.Clean(item.Target))
		if seen[key] {
			return fmt.Errorf("duplicate target %s", item.Target)
		}
		seen[key] = true
		entry := Entry{Target: item.Target, Backup: fmt.Sprintf("old-%d", i)}
		info, err := os.Lstat(item.Target)
		if err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("target is not a regular file: %s", item.Target)
			}
			entry.Existed = true
			if entry.Metadata, err = t.Platform.Metadata(item.Target); err != nil {
				return err
			}
			if entry.OldHash, err = hashFile(item.Target); err != nil {
				return err
			}
			if err = copyFile(item.Target, filepath.Join(t.Root, entry.Backup)); err != nil {
				return err
			}
			h, err := hashFile(filepath.Join(t.Root, entry.Backup))
			if err != nil || h != entry.OldHash {
				return fmt.Errorf("backup verification failed for %s: %w", item.Target, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if item.Source != "" {
			info, err := os.Lstat(item.Source)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("invalid payload %s", item.Source)
			}
			entry.Staged = fmt.Sprintf("new-%d", i)
			if entry.Hash, err = hashFile(item.Source); err != nil {
				return err
			}
			if err = copyFile(item.Source, filepath.Join(t.Root, entry.Staged)); err != nil {
				return err
			}
			h, err := hashFile(filepath.Join(t.Root, entry.Staged))
			if err != nil || h != entry.Hash {
				return fmt.Errorf("payload verification failed: %s", item.Source)
			}
		}
		j.Items = append(j.Items, entry)
	}
	state, err := t.Platform.Snapshot(t.Root)
	if err != nil {
		return err
	}
	j.System = state
	if err = t.save(j); err != nil {
		return err
	}
	t.log("prepared")
	return nil
}
func (t *Transaction) Apply() error {
	j, err := t.load()
	if err != nil {
		return err
	}
	if j.Phase != "prepared" {
		return fmt.Errorf("cannot apply phase %s", j.Phase)
	}
	// Verify the complete staged payload before making any changes.
	for _, e := range j.Items {
		if e.Staged != "" {
			h, err := hashFile(filepath.Join(t.Root, e.Staged))
			if err != nil || h != e.Hash {
				return fmt.Errorf("staged payload damaged: %s", e.Target)
			}
		}
	}
	j.Phase = "applying"
	if err = t.save(j); err != nil {
		return err
	}
	t.log("applying")
	if err = t.Platform.Quiesce(); err != nil {
		return err
	}
	for i, e := range j.Items {
		if e.Staged == "" {
			continue
		}
		if err = t.Platform.ValidateTarget(e.Target); err != nil {
			return err
		}
		if err = t.Platform.PrepareTarget(e.Target); err != nil {
			return err
		}
		if err = copyFile(filepath.Join(t.Root, e.Staged), e.Target); err != nil {
			return fmt.Errorf("replace %s: %w", e.Target, err)
		}
		// PrepareTarget also applies the protected service-file ACL after creation.
		if err = t.Platform.PrepareTarget(e.Target); err != nil {
			return err
		}
		t.log("replaced " + e.Target)
		if t.AfterReplace != nil {
			if err = t.AfterReplace(i); err != nil {
				return err
			}
		}
	}
	return nil
}
func (t *Transaction) Commit() error {
	j, err := t.load()
	if err != nil {
		return err
	}
	if j.Phase != "applying" {
		return fmt.Errorf("cannot commit phase %s", j.Phase)
	}
	for _, e := range j.Items {
		if e.Staged != "" {
			h, err := hashFile(e.Target)
			if err != nil || h != e.Hash {
				return fmt.Errorf("installed payload verification failed: %s", e.Target)
			}
		}
	}
	j.Phase = "committed"
	if err = t.save(j); err != nil {
		return err
	}
	t.log("committed")
	// Cleanup is deliberately not part of commit success. Recovery sees the
	// committed marker and never restores an old version after successful commit.
	t.cleanup(j)
	return nil
}
func (t *Transaction) Recover() error {
	j, err := t.load()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read recovery journal: %w", err)
	}
	if j.Phase == "committed" || j.Phase == "rolled-back" {
		t.cleanup(j)
		return nil
	}
	return t.Rollback()
}
func (t *Transaction) Rollback() error {
	j, err := t.load()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if j.Phase == "committed" || j.Phase == "rolled-back" {
		return nil
	}
	// Validate all backups before overwriting even one installed file.
	for _, e := range j.Items {
		if e.Existed {
			h, err := hashFile(filepath.Join(t.Root, e.Backup))
			if err != nil || h != e.OldHash {
				return fmt.Errorf("backup damaged; recovery retained at %s", t.Root)
			}
		}
	}
	j.Phase = "rolling-back"
	if err = t.save(j); err != nil {
		return err
	}
	t.log("rolling back")
	if err = t.Platform.Quiesce(); err != nil {
		// A stop failure can leave an otherwise untouched old service disabled.
		// Restore its configuration only when every file is still the old one.
		unchanged := true
		for _, e := range j.Items {
			if e.Existed {
				h, he := hashFile(e.Target)
				unchanged = unchanged && he == nil && h == e.OldHash
			} else {
				_, se := os.Lstat(e.Target)
				unchanged = unchanged && errors.Is(se, os.ErrNotExist)
			}
		}
		if unchanged {
			err = errors.Join(err, t.Platform.Restore(t.Root, j.System))
		}
		return err
	}
	for _, e := range j.Items {
		if err = t.Platform.ValidateTarget(e.Target); err != nil {
			return err
		}
		if e.Existed {
			h, hashErr := hashFile(e.Target)
			if hashErr != nil || h != e.OldHash {
				if err = t.Platform.PrepareTarget(e.Target); err != nil {
					return err
				}
				if err = copyFile(filepath.Join(t.Root, e.Backup), e.Target); err != nil {
					return err
				}
			}
			if err = t.Platform.RestoreMetadata(e.Target, e.Metadata); err != nil {
				return err
			}
		} else {
			if _, statErr := os.Lstat(e.Target); statErr == nil {
				if err = t.Platform.PrepareTarget(e.Target); err != nil {
					return err
				}
			}
			if err = os.Remove(e.Target); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	if err = t.Platform.Restore(t.Root, j.System); err != nil {
		return err
	}
	j.Phase = "rolled-back"
	if err = t.save(j); err != nil {
		return err
	}
	t.log("rolled back")
	return nil
}
func (t *Transaction) cleanup(j *Journal) {
	// Exact journal-owned files only; never recursively delete a target folder.
	for _, e := range j.Items {
		_ = os.Remove(filepath.Join(t.Root, e.Backup))
		if e.Staged != "" {
			_ = os.Remove(filepath.Join(t.Root, e.Staged))
		}
	}
}
