package state

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// hash returns a valid 40-hex head for n.
func hash(n int) string { return fmt.Sprintf("%040x", n) }

// sha returns a valid 64-hex plan hash for n.
func sha(n int) string { return fmt.Sprintf("%064x", n) }

func stateFile(dir string) string { return filepath.Join(dir, "paraplane", "closed.json") }
func lockFile(dir string) string  { return filepath.Join(dir, "paraplane", "closed.json.lock") }

func mkState(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "paraplane"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setForTest replaces *p with v for the duration of the test.
func setForTest[T any](t *testing.T, p *T, v T) {
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

func TestLoadMissingAndBroken(t *testing.T) {
	dir := t.TempDir()
	if s, err := Load(filepath.Join(dir, "nope")); err != nil || !reflect.DeepEqual(s, State{}) {
		t.Errorf("missing: %+v %v", s, err)
	}
	mkState(t, dir)
	file := stateFile(dir)
	for _, body := range []string{"{", "[]", "{}", `{"entries":[]}`} {
		writeFile(t, file, body)
		if s, err := Load(dir); err != nil || !reflect.DeepEqual(s, State{}) {
			t.Errorf("%q: %+v %v", body, s, err)
		}
	}
	if runtime.GOOS != "windows" {
		os.Remove(file)
		target := filepath.Join(dir, "paraplane", "real.json")
		writeFile(t, target, `{"version":1,"lastHead":"`+hash(1)+`"}`)
		if err := os.Symlink("real.json", file); err != nil {
			t.Fatal(err)
		}
		if s, err := Load(dir); err != nil || !reflect.DeepEqual(s, State{}) {
			t.Errorf("symlink: %+v %v", s, err)
		}
	}
}

func TestLoadTooLarge(t *testing.T) {
	dir := t.TempDir()
	mkState(t, dir)
	body := `{"version":1,"lastHead":"` + hash(1) + `","pending":["` + strings.Repeat("a", maxRead) + `"]}`
	writeFile(t, stateFile(dir), body)
	if s, err := Load(dir); err != nil || !reflect.DeepEqual(s, State{}) {
		t.Errorf("too large: %v %v", s.LastHead, err)
	}
}

func TestUpdateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	err := Update(context.Background(), dir, func(s *State) {
		s.LastHead = hash(1)
		s.Add(Entry{Plan: "p.md", Task: "T1", Head: hash(1), PlanHash: sha(1)})
		s.Shown = map[string]Shown{"p.md": {Hash: sha(1), Waves: 1, Tasks: 2}, "q.md": {Waves: 1}}
		s.Pending = []string{"p.md"}
		s.Late = map[string]int{"p.md": 2}
		s.Scan = &ScanCache{Head: hash(1), Commits: nil}
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := State{
		LastHead: hash(1),
		Entries:  []Entry{{Plan: "p.md", Task: "T1", Head: hash(1), PlanHash: sha(1)}},
		Shown:    map[string]Shown{"p.md": {Hash: sha(1), Waves: 1, Tasks: 2}, "q.md": {Waves: 1}},
		Pending:  []string{"p.md"},
		Late:     map[string]int{"p.md": 2},
		Scan:     &ScanCache{Head: hash(1)},
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("got %+v\nwant %+v", s, want)
	}
	if _, err := os.Lstat(lockFile(dir)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("lock left behind: %v", err)
	}
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

// leftovers lists files of the state directory other than closed.json.
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(filepath.Join(dir, "paraplane"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, de := range des {
		if de.Name() != "closed.json" {
			out = append(out, de.Name())
		}
	}
	return out
}

func TestUpdateConcurrent(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if err := Update(context.Background(), dir, func(s *State) {
				s.Add(Entry{Plan: "p.md", Task: fmt.Sprintf("T%d", i), Head: hash(1)})
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := len(mustLoad(t, dir).Entries); n != 20 {
		t.Errorf("%d entries, want 20", n)
	}
}

func TestLockPathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	// A non-empty, old directory: it can neither be opened nor removed.
	if err := os.MkdirAll(filepath.Join(lockFile(dir), "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockFile(dir), past, past); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err := Update(context.Background(), dir, func(s *State) { s.LastHead = hash(1) })
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if d := time.Since(start); d > 150*time.Millisecond {
		t.Errorf("took %v, want at once", d)
	}
	if _, err := os.Lstat(stateFile(dir)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("state written without the lock: %v", err)
	}
}

func TestLockPathIsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dir := t.TempDir()
	mkState(t, dir)
	if err := os.Symlink("elsewhere", lockFile(dir)); err != nil {
		t.Fatal(err)
	}
	if err := Update(context.Background(), dir, func(*State) {}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
}

func TestKilledHolder(t *testing.T) {
	dir := t.TempDir()
	mkState(t, dir)
	writeFile(t, lockFile(dir), "")
	old := time.Now().Add(-12 * time.Second)
	if err := os.Chtimes(lockFile(dir), old, old); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := Update(context.Background(), dir, func(s *State) { s.LastHead = hash(1) }); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v", d)
	}
	if mustLoad(t, dir).LastHead != hash(1) {
		t.Error("not written")
	}
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("left: %v", left)
	}
}

func TestFutureLockIsStale(t *testing.T) {
	dir := t.TempDir()
	mkState(t, dir)
	writeFile(t, lockFile(dir), "")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(lockFile(dir), future, future); err != nil {
		t.Fatal(err)
	}
	if err := Update(context.Background(), dir, func(s *State) { s.LastHead = hash(1) }); err != nil {
		t.Fatal(err)
	}
}

func TestBusy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		mkState(t, dir)
		writeFile(t, lockFile(dir), "")
		now := time.Now()
		if err := os.Chtimes(lockFile(dir), now, now); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := Update(ctx, dir, func(*State) {}); !errors.Is(err, ErrBusy) {
			t.Fatalf("err = %v, want ErrBusy", err)
		}
		if d := time.Since(now); d < 2*time.Second || d > 2*time.Second+50*time.Millisecond {
			t.Errorf("waited %v, want the context's 2s", d)
		}
	})
}

// TestWaitThenStale: a live-looking lock that is never released becomes stale
// after 11 s and is taken over while waiting without a deadline.
func TestWaitThenStale(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		mkState(t, dir)
		writeFile(t, lockFile(dir), "")
		now := time.Now()
		if err := os.Chtimes(lockFile(dir), now, now); err != nil {
			t.Fatal(err)
		}
		if err := Update(context.Background(), dir, func(s *State) { s.LastHead = hash(3) }); err != nil {
			t.Fatal(err)
		}
		if d := time.Since(now); d < staleLock || d > staleLock+time.Second {
			t.Errorf("took over after %v, want just over %v", d, staleLock)
		}
	})
}

// TestTakeOverRace: between seeing the stale lock and claiming it, another
// process takes it over and creates a fresh lock. The late claimer must give
// that lock back and keep waiting, not enter as a second owner.
func TestTakeOverRace(t *testing.T) {
	dir := t.TempDir()
	mkState(t, dir)
	writeFile(t, lockFile(dir), "")
	old := time.Now().Add(-12 * time.Second)
	if err := os.Chtimes(lockFile(dir), old, old); err != nil {
		t.Fatal(err)
	}
	once := false
	setForTest(t, &beforeClaim, func() {
		if once {
			return
		}
		once = true
		// The winner: removes the stale lock and creates its own.
		if err := os.Remove(lockFile(dir)); err != nil {
			t.Error(err)
		}
		writeFile(t, lockFile(dir), "winner")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := Update(ctx, dir, func(*State) { t.Error("second owner entered") }); !errors.Is(err, ErrBusy) {
		t.Errorf("err = %v, want ErrBusy", err)
	}
	if b, err := os.ReadFile(lockFile(dir)); err != nil || string(b) != "winner" {
		t.Errorf("winner's lock not restored: %q %v", b, err)
	}
	if left := leftovers(t, dir); !reflect.DeepEqual(left, []string{"closed.json.lock"}) {
		t.Errorf("left: %v", left)
	}
}

func TestStaleLockValue(t *testing.T) {
	if staleLock != 11*time.Second {
		t.Errorf("staleLock = %v, want 11s", staleLock)
	}
}

func TestCapEntries(t *testing.T) {
	dir := t.TempDir()
	if err := Update(context.Background(), dir, func(s *State) {
		for i := range 4100 {
			s.Entries = append(s.Entries, Entry{Plan: "p.md", Task: fmt.Sprintf("T%d", i), Head: hash(1)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	e := mustLoad(t, dir).Entries
	if len(e) != 4096 || e[0].Task != "T4" {
		t.Errorf("%d entries, first %q", len(e), e[0].Task)
	}
}

func TestLoadDropsInvalid(t *testing.T) {
	dir := t.TempDir()
	mkState(t, dir)
	good := Entry{Plan: "docs/p.md", Task: "T1", Head: hash(1), PlanHash: sha(1)}
	good64 := Entry{Plan: "docs/p.md", Task: "T2", Head: sha(2)}
	bad := []Entry{
		{Plan: "docs/p.md", Task: "1bad", Head: hash(1)},
		{Plan: "docs/p.md", Task: "T1;rm", Head: hash(1)},
		{Plan: "docs/p.md", Task: "T1", Head: ""},
		{Plan: "docs/p.md", Task: "T1", Head: "HEAD"},
		{Plan: "docs/p.md", Task: "T1", Head: "--all"},
		{Plan: "docs/p.md", Task: "T1", Head: strings.ToUpper(hash(10))},
		{Plan: "docs/p.md", Task: "T1", Head: hash(1)[:39]},
		{Plan: "", Task: "T1", Head: hash(1)},
		{Plan: "../x.md", Task: "T1", Head: hash(1)},
		{Plan: "docs/../../x.md", Task: "T1", Head: hash(1)},
		{Plan: `docs\..\x.md`, Task: "T1", Head: hash(1)},
		{Plan: "docs/p.md", Task: "T1", Head: hash(1), PlanHash: "xyz"},
	}
	var body strings.Builder
	body.WriteString(`{"version":1,"lastHead":"--help","entries":[`)
	for i, e := range append([]Entry{good}, append(bad, good64)...) {
		if i > 0 {
			body.WriteString(",")
		}
		fmt.Fprintf(&body, `{"plan":%q,"task":%q,"head":%q,"planHash":%q}`, e.Plan, e.Task, e.Head, e.PlanHash)
	}
	body.WriteString(`],"pending":["docs/p.md","","../up.md"],` +
		`"shown":{"docs/p.md":{"waves":1,"tasks":1},"docs/h.md":{"hash":"` + sha(3) + `","waves":2},"docs/bad.md":{"hash":"xyz","waves":1},"../x.md":{"waves":1,"tasks":1}},` +
		`"late":{"docs/p.md":2,"docs/q.md":1,"../up.md":1},` +
		`"scan":{"head":"nothex","commits":[1]}}`)
	writeFile(t, stateFile(dir), body.String())
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := State{
		Entries: []Entry{good, good64},
		Pending: []string{"docs/p.md"},
		Shown:   map[string]Shown{"docs/p.md": {Waves: 1, Tasks: 1}, "docs/h.md": {Hash: sha(3), Waves: 2}},
		Late:    map[string]int{"docs/p.md": 2}, // only plans still pending
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("got %+v\nwant %+v", s, want)
	}
}

func TestLoadCapsEntries(t *testing.T) {
	dir := t.TempDir()
	mkState(t, dir)
	var body strings.Builder
	body.WriteString(`{"version":1,"entries":[`)
	for i := range 5000 {
		if i > 0 {
			body.WriteString(",")
		}
		fmt.Fprintf(&body, `{"plan":"p.md","task":"T%d","head":%q}`, i, hash(1))
	}
	body.WriteString(`]}`)
	writeFile(t, stateFile(dir), body.String())
	e := mustLoad(t, dir).Entries
	if len(e) != maxEntries || e[0].Task != "T904" {
		t.Errorf("%d entries, first %q", len(e), e[0].Task)
	}
}

func TestUpdatePrunesInvalid(t *testing.T) {
	dir := t.TempDir()
	if err := Update(context.Background(), dir, func(s *State) {
		s.Entries = append(s.Entries, Entry{Plan: "p.md", Task: "T1", Head: ""}, Entry{Plan: "p.md", Task: "T2", Head: hash(2)})
		s.LastHead = "nope"
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(stateFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"T1"`) || strings.Contains(string(data), "nope") {
		t.Errorf("invalid content written: %s", data)
	}
}

func TestAddDedup(t *testing.T) {
	a := Entry{Plan: "p.md", Task: "T1", Head: hash(1), PlanHash: sha(1)}
	b := Entry{Plan: "p.md", Task: "T2", Head: hash(1), PlanHash: sha(1)}
	var s State
	s.Add(a)
	s.Add(b)
	s.Add(a)
	if want := []Entry{b, a}; !reflect.DeepEqual(s.Entries, want) {
		t.Errorf("got %v, want %v (the repeat moves to the end)", s.Entries, want)
	}
	// Any field that differs makes another entry.
	for _, e := range []Entry{
		{Plan: "q.md", Task: "T1", Head: hash(1), PlanHash: sha(1)},
		{Plan: "p.md", Task: "T1", Head: hash(2), PlanHash: sha(1)},
		{Plan: "p.md", Task: "T1", Head: hash(1), PlanHash: sha(2)},
	} {
		s.Add(e)
	}
	if len(s.Entries) != 5 {
		t.Errorf("%d entries, want 5", len(s.Entries))
	}
}

func TestPrune(t *testing.T) {
	s := State{Entries: []Entry{
		{Plan: "p.md", Task: "T1", Head: hash(1)},
		{Plan: "p.md", Task: "T2", Head: hash(2)},
		{Plan: "q.md", Task: "T1", Head: hash(1)},
	}}
	s.Prune(func(e Entry) bool { return e.Head != hash(1) })
	if want := []Entry{{Plan: "p.md", Task: "T2", Head: hash(2)}}; !reflect.DeepEqual(s.Entries, want) {
		t.Errorf("got %v", s.Entries)
	}
}

func TestForget(t *testing.T) {
	s := State{Entries: []Entry{
		{Plan: "p.md", Task: "T1", Head: hash(1)},
		{Plan: "p.md", Task: "T2", Head: hash(1)},
		{Plan: "p.md", Task: "T1", Head: hash(2)},
		{Plan: "q.md", Task: "T1", Head: hash(1)},
		{Plan: "p.md", Task: "T3", Head: hash(1)},
	}}
	s.Forget("p.md", []string{"T1", "T3"})
	want := []Entry{
		{Plan: "p.md", Task: "T2", Head: hash(1)},
		{Plan: "q.md", Task: "T1", Head: hash(1)},
	}
	if !reflect.DeepEqual(s.Entries, want) {
		t.Errorf("got %v", s.Entries)
	}
}

func TestTakePendingKeepsOthers(t *testing.T) {
	s := State{Pending: []string{"a.md", "b.md", "c.md"}}
	s.TakePending([]string{"b.md", "x.md"})
	if want := []string{"a.md", "c.md"}; !reflect.DeepEqual(s.Pending, want) {
		t.Errorf("got %v", s.Pending)
	}
	s.TakePending([]string{"a.md", "c.md"})
	if len(s.Pending) != 0 {
		t.Errorf("got %v", s.Pending)
	}
}

func TestOtherVersionUntouched(t *testing.T) {
	dir := t.TempDir()
	mkState(t, dir)
	body := `{"version":2,"entries":[{"plan":"p","task":"T1","head":"h"}],"future":true}`
	writeFile(t, stateFile(dir), body)
	if _, err := Load(dir); !errors.Is(err, ErrVersion) {
		t.Errorf("Load err = %v, want ErrVersion", err)
	}
	called := false
	if err := Update(context.Background(), dir, func(*State) { called = true }); !errors.Is(err, ErrVersion) {
		t.Errorf("Update err = %v, want ErrVersion", err)
	}
	if called {
		t.Error("f called for another version")
	}
	if b, _ := os.ReadFile(stateFile(dir)); string(b) != body {
		t.Errorf("file changed: %s", b)
	}
	if _, err := os.Lstat(lockFile(dir)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("lock left behind: %v", err)
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

// TestUpdateRefusesSymlinks plants symlinks at the exact names the
// implementation uses: the temporary file (its name comes from the
// injectable tempName) and closed.json itself.
func TestUpdateRefusesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	const tmpName = "closed.json.tmp-planted"
	setForTest(t, &tempName, func() string { return tmpName })
	dir := t.TempDir()
	mkState(t, dir)
	// The victim is inside the state directory, so os.Root alone would
	// follow the link: only O_EXCL protects it.
	victim := filepath.Join(dir, "paraplane", "victim")
	writeFile(t, victim, "keep")
	if err := os.Symlink("victim", filepath.Join(dir, "paraplane", tmpName)); err != nil {
		t.Fatal(err)
	}
	_ = Update(context.Background(), dir, func(s *State) { s.LastHead = hash(1) })
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("victim overwritten through the temporary file: %q", b)
	}
	if fi, err := os.Lstat(filepath.Join(dir, "paraplane", tmpName)); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("planted link replaced or removed: %v", err)
	}

	// A symlink at closed.json is replaced, not written through.
	setForTest(t, &tempName, func() string { return "closed.json.tmp-ok" })
	dir2 := t.TempDir()
	mkState(t, dir2)
	victim2 := filepath.Join(dir2, "paraplane", "victim")
	writeFile(t, victim2, "keep")
	if err := os.Symlink("victim", stateFile(dir2)); err != nil {
		t.Fatal(err)
	}
	if err := Update(context.Background(), dir2, func(s *State) { s.LastHead = hash(1) }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim2); string(b) != "keep" {
		t.Errorf("victim overwritten through closed.json: %q", b)
	}
	if fi, err := os.Lstat(stateFile(dir2)); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("closed.json is not a regular file: %v", err)
	}
}

func TestRootContainment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	other := t.TempDir()
	dir := t.TempDir()
	if err := os.Symlink(other, filepath.Join(dir, "paraplane")); err != nil {
		t.Fatal(err)
	}
	if err := Update(context.Background(), dir, func(s *State) { s.LastHead = hash(1) }); err == nil {
		t.Error("symlinked state directory accepted")
	}
	if entries, _ := os.ReadDir(other); len(entries) != 0 {
		t.Errorf("wrote through the symlink: %v", entries)
	}
	// Also a link that stays inside the git dir is refused.
	dir2 := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir2, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inner", filepath.Join(dir2, "paraplane")); err != nil {
		t.Fatal(err)
	}
	if err := Update(context.Background(), dir2, func(s *State) { s.LastHead = hash(1) }); err == nil {
		t.Error("symlinked state directory (inside) accepted")
	}
	if entries, _ := os.ReadDir(filepath.Join(dir2, "inner")); len(entries) != 0 {
		t.Errorf("wrote through the symlink: %v", entries)
	}
	// Reading through the link gives nothing either.
	writeFile(t, filepath.Join(other, "closed.json"), `{"version":1,"lastHead":"`+hash(1)+`"}`)
	if s, _ := Load(dir); s.LastHead != "" {
		t.Error("read through a symlinked state directory")
	}
}

func TestRenameRetry(t *testing.T) {
	fails := 2
	setForTest(t, &rename, func(r *os.Root, a, b string) error {
		if fails > 0 {
			fails--
			return os.ErrPermission
		}
		return r.Rename(a, b)
	})
	dir := t.TempDir()
	if err := Update(context.Background(), dir, func(s *State) { s.LastHead = hash(1) }); err != nil {
		t.Fatalf("rename not retried: %v", err)
	}
	if mustLoad(t, dir).LastHead != hash(1) {
		t.Error("not written")
	}
}

func FuzzLoad(f *testing.F) {
	f.Add([]byte(`{"version":1,"lastHead":"` + hash(1) + `","entries":[{"plan":"p.md","task":"T1","head":"` + hash(1) + `"}]}`))
	f.Add([]byte(`{"version":1,"entries":[{"plan":"../p","task":"1","head":""}],"pending":["",".."],"scan":{"head":"x"}}`))
	f.Add([]byte(`{"version":2}`))
	f.Add([]byte(`{`))
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		mkState(t, dir)
		if err := os.WriteFile(stateFile(dir), data, 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := Load(dir)
		if err != nil {
			if !errors.Is(err, ErrVersion) {
				t.Fatalf("unexpected error %v", err)
			}
			return
		}
		if s.LastHead != "" && !validHead(s.LastHead) {
			t.Errorf("lastHead %q", s.LastHead)
		}
		if len(s.Entries) > maxEntries {
			t.Errorf("%d entries", len(s.Entries))
		}
		for _, e := range s.Entries {
			if !validEntry(e) {
				t.Errorf("invalid entry %+v", e)
			}
		}
		for _, p := range s.Pending {
			if !validPlan(p) {
				t.Errorf("invalid pending %q", p)
			}
		}
		if s.Scan != nil && !validHead(s.Scan.Head) {
			t.Errorf("scan head %q", s.Scan.Head)
		}
	})
}

// TestLoadLargeLinear: a 4 MiB file of distinct pending plans or distinct
// entries loads in linear time (a hostile .git must not stall the hook).
func TestLoadLargeLinear(t *testing.T) {
	for _, kind := range []string{"pending", "entries"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			mkState(t, dir)
			var b strings.Builder
			b.WriteString(`{"version":1,"` + kind + `":[`)
			for i := 0; b.Len() < maxRead-200; i++ {
				if i > 0 {
					b.WriteString(",")
				}
				if kind == "pending" {
					fmt.Fprintf(&b, `"%x"`, i)
				} else {
					fmt.Fprintf(&b, `{"plan":"%x","task":"T","head":%q}`, i, hash(i))
				}
			}
			b.WriteString(`]}`)
			if b.Len() > maxRead {
				t.Fatalf("file %d bytes", b.Len())
			}
			writeFile(t, stateFile(dir), b.String())
			start := time.Now()
			s, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			d := time.Since(start)
			t.Logf("load of %d bytes: %v", b.Len(), d)
			if d > 6*time.Second {
				t.Errorf("load took %v", d)
			}
			if n := len(s.Pending) + len(s.Entries); n != maxEntries {
				t.Errorf("%d items, want %d", n, maxEntries)
			}
		})
	}
}

// mustLoad is Load that fails the test on an error.
func mustLoad(t testing.TB, dir string) State {
	t.Helper()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	if Exists(dir) {
		t.Error("Exists on an empty git dir")
	}
	mkState(t, dir)
	if Exists(dir) {
		t.Error("Exists without the file")
	}
	writeFile(t, stateFile(dir), "{}")
	if !Exists(dir) {
		t.Error("Exists with the file")
	}
}
