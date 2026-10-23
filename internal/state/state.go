// Package state keeps what paraplane learns outside git: the tasks whose
// subagents returned (spec 2026-10-23 §5). It lives in
// <git-dir>/paraplane/closed.json, out of the working tree, so it is never
// committed and never audited.
//
// The repository may be untrusted: every file operation goes through an
// os.Root of the state directory, the content is validated on load, and the
// lock never makes a hook spin or wait past its context.
package state

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"paraplane/internal/fsx"
	"paraplane/internal/ids"
)

// Names inside <git-dir>.
const (
	dirName  = "paraplane"
	fileName = "closed.json"
	lockName = fileName + ".lock"
)

// Exists reports whether the state file of gitDir exists (even if it is not
// a regular file or is invalid): a hook that has no plan to show writes into
// <gitDir>/paraplane only when it does.
func Exists(gitDir string) bool {
	_, err := os.Lstat(filepath.Join(gitDir, dirName, fileName))
	return err == nil
}

// Limits and timings.
const (
	version    = 1
	maxEntries = 4096    // entries kept, the newest win; also the cap of Pending
	maxRead    = 4 << 20 // bytes of closed.json read; a longer file is empty
	// staleLock is the age after which a lock is abandoned: above the 10 s
	// host timeout of a hook, so a live holder is never taken over.
	staleLock = 11 * time.Second
	pollEvery = 10 * time.Millisecond
)

// rename replaces the state file; tests make it fail. On Windows it fails
// while another process has the file open, so the write retries it.
var rename = (*os.Root).Rename

// tempName names the temporary file of a write; tests fix it to plant a
// symlink at the exact name.
// beforeClaim is a test hook run between seeing a stale lock and claiming
// it; nil in production.
var beforeClaim func()

var tempName = func() string { return fileName + ".tmp-" + rand.Text() }

var (
	// ErrBusy means the lock was not taken: another process holds it until
	// the context ends, or the lock path is not a regular file.
	ErrBusy = errors.New("paraplane state is busy")
	// ErrVersion means closed.json has another version: callers neither
	// read nor write it.
	ErrVersion = errors.New("paraplane state has another version")

	errNotDir = errors.New("paraplane state directory is not a directory")
)

var (
	headRE = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	shaRE  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Entry is a task whose subagent returned, with HEAD at that moment and the
// SHA-256 of the plan block it was recorded against.
type Entry struct {
	Plan     string `json:"plan"`
	Task     string `json:"task"`
	Head     string `json:"head"`
	PlanHash string `json:"planHash,omitempty"`
}

// Shown is what a plan's progress counted when it was last shown, with the
// SHA-256 of the plan block it was counted against: counts of another block
// (a new plan at the same path, an edited block) do not hold. Hash is empty
// in states written before it existed, which then matches no block.
type Shown struct {
	Hash  string `json:"hash,omitempty"`
	Waves int    `json:"waves"`
	Tasks int    `json:"tasks"`
}

// ScanCache is the commit scan up to Head; state keeps Commits opaque.
type ScanCache struct {
	Head    string            `json:"head"`
	Commits []json.RawMessage `json:"commits,omitempty"`
}

// State is the content of closed.json.
type State struct {
	LastHead string           `json:"lastHead,omitempty"`
	Entries  []Entry          `json:"entries,omitempty"`
	Shown    map[string]Shown `json:"shown,omitempty"`
	Pending  []string         `json:"pending,omitempty"` // plans whose progress waits for a hook that can show it
	Late     map[string]int   `json:"late,omitempty"`    // pending plans: checks in a row that ran out of time
	Scan     *ScanCache       `json:"scan,omitempty"`
}

// file is the on-disk form, with its version.
type file struct {
	Version int `json:"version"`
	State
}

// Load reads the state of gitDir. A missing, irregular (symlink, FIFO),
// too large or invalid file, or a state directory that is not a directory,
// is an empty State; another version is ErrVersion. Invalid values are
// dropped (see clean).
func Load(gitDir string) (State, error) {
	r, err := openDir(gitDir, false)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errNotDir) {
			return State{}, nil
		}
		return State{}, err
	}
	defer r.Close()
	return load(r)
}

// Update changes the state of gitDir under the lock: it loads the state,
// calls f, drops invalid values, keeps the last maxEntries entries and
// writes the file atomically (temporary file, Sync, rename). It returns
// ErrBusy when the lock is not taken before ctx ends or the lock path is not
// a regular file, and ErrVersion (f not called, file untouched) for another
// version. A lock older than staleLock is taken over.
func Update(ctx context.Context, gitDir string, f func(*State)) error {
	r, err := openDir(gitDir, true)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := acquire(ctx, r); err != nil {
		return err
	}
	defer r.Remove(lockName)
	s, err := load(r)
	if err != nil {
		return err
	}
	f(&s)
	s.clean()
	data, err := json.Marshal(file{Version: version, State: s})
	if err != nil {
		return err
	}
	return write(r, data)
}

// openDir opens <gitDir>/paraplane as a Root, creating it when create is set.
// It must be a real directory, not a symlink, even one that stays inside
// gitDir.
func openDir(gitDir string, create bool) (*os.Root, error) {
	root, err := os.OpenRoot(gitDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if create {
		if err := root.Mkdir(dirName, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	fi, err := root.Lstat(dirName)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, errNotDir
	}
	return root.OpenRoot(dirName)
}

// load reads and decodes closed.json in r.
func load(r *os.Root) (State, error) {
	data, err := readRegular(r, fileName, maxRead)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fsx.ErrNotRegular), errors.Is(err, fsx.ErrTooLarge):
		return State{}, nil
	case err != nil:
		return State{}, err
	}
	return decode(data)
}

// readRegular reads name in r only if it is a regular file: a symlink is
// refused by Lstat, a FIFO is opened without blocking and refused by Stat.
func readRegular(r *os.Root, name string, max int64) ([]byte, error) {
	lfi, err := r.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !lfi.Mode().IsRegular() {
		return nil, fsx.ErrNotRegular
	}
	f, err := r.OpenFile(name, os.O_RDONLY|nonblockFlag, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fsx.ErrNotRegular
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fsx.ErrTooLarge
	}
	return data, nil
}

// decode parses closed.json: no version or broken JSON is an empty State,
// another version ErrVersion.
func decode(data []byte) (State, error) {
	var v struct {
		Version *int `json:"version"`
	}
	if json.Unmarshal(data, &v) != nil || v.Version == nil {
		return State{}, nil
	}
	if *v.Version != version {
		return State{}, ErrVersion
	}
	var f file
	if json.Unmarshal(data, &f) != nil {
		return State{}, nil
	}
	f.State.clean()
	return f.State, nil
}

// acquire takes the lock file of r. A non-regular lock path is ErrBusy at
// once; a stale lock is taken over; every wait ends with ctx.
func acquire(ctx context.Context, r *os.Root) error {
	var denied time.Time // first create refused as a pending delete
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %w", ErrBusy, err)
		}
		l, err := r.OpenFile(lockName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			l.Close()
			return nil
		}
		if !errors.Is(err, fs.ErrExist) && !deletePending(err) {
			// Windows reports a directory at the lock path as an open
			// error, not as ErrExist.
			if fi, lerr := r.Lstat(lockName); lerr == nil && !fi.Mode().IsRegular() {
				return ErrBusy
			}
			return err
		}
		fi, lerr := r.Lstat(lockName)
		if !errors.Is(err, fs.ErrExist) || deletePending(lerr) {
			// A pending delete refuses both the create and the Lstat; it
			// clears in moments, a refusal that lasts is a real one.
			if denied.IsZero() {
				denied = time.Now()
			} else if time.Since(denied) > staleLock {
				return err
			}
			lerr = fs.ErrNotExist
		}
		switch err := lerr; {
		case errors.Is(err, fs.ErrNotExist):
			// Released meanwhile: retry after the poll interval.
		case err != nil:
			return err
		case !fi.Mode().IsRegular():
			return ErrBusy // a directory or link no release will remove
		case stale(fi) && takeOver(r):
			continue
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", ErrBusy, ctx.Err())
		case <-time.After(pollEvery):
		}
	}
}

// stale reports whether a lock's mtime is more than staleLock away from now,
// in either direction: a lock dated in the future would never age.
func stale(fi fs.FileInfo) bool {
	age := time.Since(fi.ModTime())
	return age > staleLock || age < -staleLock
}

// takeOver claims a stale lock by renaming it to a unique name: of several
// processes only one rename succeeds. If the claimed file turns out fresh, a
// winner's new lock was taken between Lstat and rename; it is linked back
// (never replacing a newer lock) and takeOver reports false.
func takeOver(r *os.Root) bool {
	if beforeClaim != nil {
		beforeClaim()
	}
	claim := lockName + ".stale-" + rand.Text()
	if r.Rename(lockName, claim) != nil {
		return false
	}
	fi, err := r.Lstat(claim)
	if err == nil && fi.Mode().IsRegular() && stale(fi) {
		r.Remove(claim)
		return true
	}
	if err == nil {
		_ = r.Link(claim, lockName)
	}
	r.Remove(claim)
	return false
}

// write replaces closed.json in r with data: a fresh temporary file (O_EXCL,
// so a planted symlink is never followed), Sync, then rename with retries
// for about half a second.
func write(r *os.Root, data []byte) error {
	name := tempName()
	t, err := r.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, werr := t.Write(data)
	if werr == nil {
		werr = t.Sync()
	}
	if cerr := t.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		r.Remove(name)
		return werr
	}
	for i := 0; ; i++ {
		err = rename(r, name, fileName)
		if err == nil || i == 20 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		r.Remove(name)
	}
	return err
}

// validHead reports whether h is a full SHA-1 or SHA-256 hex hash.
func validHead(h string) bool { return headRE.MatchString(h) }

// validPlan reports whether p is a non-empty plan path without a ".."
// element.
func validPlan(p string) bool {
	if p == "" {
		return false
	}
	parts := strings.FieldsFunc(p, func(c rune) bool { return c == '/' || c == '\\' })
	return !slices.Contains(parts, "..")
}

// validEntry reports whether every field of e is well formed; PlanHash may
// be empty (entries written before it existed).
func validEntry(e Entry) bool {
	return ids.Valid(e.Task) && validHead(e.Head) && validPlan(e.Plan) &&
		(e.PlanHash == "" || shaRE.MatchString(e.PlanHash))
}

// clean caps the raw Entries and Pending at their last maxEntries items,
// then drops invalid values and duplicates (the last entry copy and the
// first pending copy win). Empty slices become nil.
func (s *State) clean() {
	if !validHead(s.LastHead) {
		s.LastHead = ""
	}
	if s.Scan != nil && !validHead(s.Scan.Head) {
		s.Scan = nil
	}
	// Cap the raw input first, then de-duplicate with a map: a hostile file
	// of hundreds of thousands of items stays linear.
	raw := lastN(s.Entries)
	seen := make(map[Entry]bool, len(raw))
	var entries []Entry
	for _, e := range slices.Backward(raw) {
		if validEntry(e) && !seen[e] {
			seen[e] = true
			entries = append(entries, e)
		}
	}
	slices.Reverse(entries)
	s.Entries = entries
	rawPending := lastN(s.Pending)
	seenPlan := make(map[string]bool, len(rawPending))
	var pending []string
	for _, p := range rawPending {
		if validPlan(p) && !seenPlan[p] {
			seenPlan[p] = true
			pending = append(pending, p)
		}
	}
	s.Pending = pending
	for k, v := range s.Shown {
		if !validPlan(k) || (v.Hash != "" && !shaRE.MatchString(v.Hash)) {
			delete(s.Shown, k)
		}
	}
	if len(s.Shown) == 0 {
		s.Shown = nil
	}
	// Late counts only plans still pending, so it is bounded by Pending.
	for k, n := range s.Late {
		if n < 1 || !seenPlan[k] {
			delete(s.Late, k)
		}
	}
	if len(s.Late) == 0 {
		s.Late = nil
	}
}

// lastN returns the last maxEntries elements of x (a view, not a copy).
func lastN[T any](x []T) []T {
	if n := len(x); n > maxEntries {
		return x[n-maxEntries:]
	}
	return x
}

// Add appends e; an equal entry (Plan, Task, Head and PlanHash) is removed
// first, so a repeat moves to the end instead of piling up.
func (s *State) Add(e Entry) {
	s.Entries = append(slices.DeleteFunc(s.Entries, func(x Entry) bool { return x == e }), e)
}

// Prune keeps only the entries keep accepts.
func (s *State) Prune(keep func(Entry) bool) {
	s.Entries = slices.DeleteFunc(s.Entries, func(e Entry) bool { return !keep(e) })
}

// Forget removes the entries of plan whose task is one of tasks.
func (s *State) Forget(plan string, tasks []string) {
	s.Prune(func(e Entry) bool { return e.Plan != plan || !slices.Contains(tasks, e.Task) })
}

// TakePending removes plans from Pending and keeps the others.
func (s *State) TakePending(plans []string) {
	s.Pending = slices.DeleteFunc(s.Pending, func(p string) bool { return slices.Contains(plans, p) })
	if len(s.Pending) == 0 {
		s.Pending = nil
	}
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
