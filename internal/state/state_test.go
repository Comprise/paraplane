package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestLoadMissingAndBroken(t *testing.T) {
	dir := t.TempDir()
	if s := Load(filepath.Join(dir, "nope")); !reflect.DeepEqual(s, State{}) {
		t.Errorf("missing: %+v", s)
	}
	if err := os.MkdirAll(filepath.Join(dir, "paraplane"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "paraplane", "closed.json")
	for _, body := range []string{"{", `{"version":2,"entries":[{"plan":"p","task":"T1","head":"h"}]}`, "[]"} {
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if s := Load(dir); !reflect.DeepEqual(s, State{}) {
			t.Errorf("%q: %+v", body, s)
		}
	}
	if runtime.GOOS != "windows" {
		os.Remove(file)
		if err := os.Symlink("/etc/hostname", file); err != nil {
			t.Fatal(err)
		}
		if s := Load(dir); !reflect.DeepEqual(s, State{}) {
			t.Errorf("symlink: %+v", s)
		}
	}
}

func TestUpdateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	err := Update(dir, func(s *State) {
		s.LastHead = "abc"
		s.Entries = append(s.Entries, Entry{Plan: "p.md", Task: "T1", Head: "abc"})
		s.Shown = map[string]Shown{"p.md": {Waves: 1, Tasks: 2}}
	})
	if err != nil {
		t.Fatal(err)
	}
	s := Load(dir)
	if s.LastHead != "abc" || len(s.Entries) != 1 || s.Shown["p.md"] != (Shown{Waves: 1, Tasks: 2}) {
		t.Errorf("%+v", s)
	}
	if _, err := os.Stat(filepath.Join(dir, "paraplane", "closed.json.lock")); !os.IsNotExist(err) {
		t.Errorf("lock left behind: %v", err)
	}
}

func TestUpdateConcurrent(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Update(dir, func(s *State) {
				s.Entries = append(s.Entries, Entry{Plan: "p.md", Task: fmt.Sprintf("T%d", i), Head: "h"})
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := len(Load(dir).Entries); n != 20 {
		t.Errorf("%d entries, want 20", n)
	}
}

func TestStaleLock(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "paraplane", "closed.json.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-31 * time.Second)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if err := Update(dir, func(s *State) { s.LastHead = "x" }); err != nil {
		t.Fatal(err)
	}
}

func TestBusy(t *testing.T) {
	old := lockWait
	lockWait = 100 * time.Millisecond
	t.Cleanup(func() { lockWait = old })
	dir := t.TempDir()
	lock := filepath.Join(dir, "paraplane", "closed.json.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Update(dir, func(s *State) {}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
}

func TestCapEntries(t *testing.T) {
	dir := t.TempDir()
	if err := Update(dir, func(s *State) {
		for i := range 4100 {
			s.Entries = append(s.Entries, Entry{Plan: "p.md", Task: fmt.Sprintf("T%d", i), Head: "h"})
		}
	}); err != nil {
		t.Fatal(err)
	}
	e := Load(dir).Entries
	if len(e) != 4096 || e[0].Task != "T4" {
		t.Errorf("%d entries, first %q", len(e), e[0].Task)
	}
}

func TestClosedValid(t *testing.T) {
	s := State{Entries: []Entry{
		{Plan: "p.md", Task: "T1", Head: "good"},
		{Plan: "p.md", Task: "T2", Head: "bad"},
		{Plan: "q.md", Task: "T3", Head: "good"},
		{Plan: "p.md", Task: "T1", Head: "good"},
		{Plan: "p.md", Task: "T4", Head: "good"},
	}}
	got := s.Closed("p.md", func(h string) bool { return h == "good" })
	if !reflect.DeepEqual(got, []string{"T1", "T4"}) {
		t.Errorf("got %v", got)
	}
}

func TestUpdateRefusesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "paraplane"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, "paraplane", "closed.json.tmp")); err != nil {
		t.Fatal(err)
	}
	_ = Update(dir, func(s *State) { s.LastHead = "x" })
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("victim overwritten: %q", b)
	}
	// A symlinked state directory is refused.
	other := t.TempDir()
	dir2 := t.TempDir()
	if err := os.Symlink(other, filepath.Join(dir2, "paraplane")); err != nil {
		t.Fatal(err)
	}
	if err := Update(dir2, func(s *State) { s.LastHead = "x" }); err == nil {
		t.Error("symlinked state directory accepted")
	}
	if entries, _ := os.ReadDir(other); len(entries) != 0 {
		t.Errorf("wrote through the symlink: %v", entries)
	}
}

func TestRenameRetry(t *testing.T) {
	old := rename
	t.Cleanup(func() { rename = old })
	fails := 2
	rename = func(a, b string) error {
		if fails > 0 {
			fails--
			return os.ErrPermission
		}
		return os.Rename(a, b)
	}
	dir := t.TempDir()
	if err := Update(dir, func(s *State) { s.LastHead = "x" }); err != nil {
		t.Fatalf("rename not retried: %v", err)
	}
	if Load(dir).LastHead != "x" {
		t.Error("not written")
	}
}
