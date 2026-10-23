// Package state keeps what paraplane learns outside git: the tasks whose
// subagents returned (spec 2026-10-23 §5). It lives in
// <git-dir>/paraplane/closed.json, out of the working tree, so it is never
// committed and never audited.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"paraplane/internal/fsx"
)

// Limits of the state file.
const (
	maxEntries = 4096
	staleLock  = 30 * time.Second
)

// lockWait bounds the wait for the lock; tests shorten it.
var lockWait = 2 * time.Second

// rename replaces the state file; tests make it fail. On Windows it fails
// while another process has the file open, so Update retries it.
var rename = os.Rename

// ErrBusy is returned by Update when another process holds the lock.
var ErrBusy = errors.New("paraplane state is busy")

// Entry is a task whose subagent returned, with HEAD at that moment.
type Entry struct {
	Plan string `json:"plan"`
	Task string `json:"task"`
	Head string `json:"head"`
}

// Shown is what a plan's progress counted when it was last shown.
type Shown struct {
	Waves int `json:"waves"`
	Tasks int `json:"tasks"`
}

// State is the content of closed.json.
type State struct {
	LastHead string           `json:"lastHead,omitempty"`
	Entries  []Entry          `json:"entries,omitempty"`
	Shown    map[string]Shown `json:"shown,omitempty"`
}

// file is the on-disk form, with its version.
type file struct {
	Version int `json:"version"`
	State
}

func path(gitDir string) string { return filepath.Join(gitDir, "paraplane", "closed.json") }

// Load reads the state of gitDir. A missing, unreadable, irregular (symlink,
// FIFO) or invalid file, or another version, is an empty State.
func Load(gitDir string) State {
	data, err := fsx.ReadRegular(path(gitDir), fsx.MaxFile)
	if err != nil {
		return State{}
	}
	var f file
	if json.Unmarshal(data, &f) != nil || f.Version != 1 {
		return State{}
	}
	return f.State
}

// Update changes the state of gitDir under a lock: it loads the state, calls
// f, keeps the last maxEntries entries and writes the file atomically. It
// returns ErrBusy when the lock is not taken within lockWait; a lock older
// than staleLock is taken over.
func Update(gitDir string, f func(*State)) error {
	p := path(gitDir)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The repository may be untrusted: never write through a symlink.
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return errors.New("paraplane state directory is not a directory")
	}
	lock := p + ".lock"
	deadline := time.Now().Add(lockWait)
	for {
		l, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			l.Close()
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if fi, e := os.Lstat(lock); e == nil && time.Since(fi.ModTime()) > staleLock {
			os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return ErrBusy
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer os.Remove(lock)
	s := Load(gitDir)
	f(&s)
	if n := len(s.Entries); n > maxEntries {
		s.Entries = slices.Clone(s.Entries[n-maxEntries:])
	}
	data, err := json.Marshal(file{Version: 1, State: s})
	if err != nil {
		return err
	}
	// A fresh temporary file (O_EXCL), so a planted symlink is not followed.
	tmp, err := os.CreateTemp(dir, "closed.json.tmp*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp.Name())
		return werr
	}
	for i := 0; ; i++ {
		err = rename(tmp.Name(), p)
		if err == nil || i == 20 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// Closed returns the tasks of plan whose entry head passes valid, without
// duplicates, in entry order.
func (s State) Closed(plan string, valid func(head string) bool) []string {
	var out []string
	for _, e := range s.Entries {
		if e.Plan == plan && !slices.Contains(out, e.Task) && valid(e.Head) {
			out = append(out, e.Task)
		}
	}
	return out
}
