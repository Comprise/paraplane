package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"paraplane/internal/testgit"
)

// commitMsg writes f with a fresh content and commits it with msg.
func commitMsg(t testing.TB, dir, msg string) string {
	t.Helper()
	p := filepath.Join(dir, "f")
	old, _ := os.ReadFile(p)
	if err := os.WriteFile(p, append(old, 'x'), 0o644); err != nil {
		t.Fatal(err)
	}
	testgit.Run(t, dir, "add", "-A")
	testgit.Run(t, dir, "commit", "-q", "-m", msg)
	return testgit.Run(t, dir, "rev-parse", "HEAD")
}

// TestBudgetKillsSlowGit: a repository whose config includes a FIFO makes
// every git block on open; the caller's context stops it in time.
func TestBudgetKillsSlowGit(t *testing.T) {
	skipWindows(t)
	dir := testgit.Init(t)
	fifo := filepath.Join(t.TempDir(), "ff")
	if out, err := exec.Command("mkfifo", fifo).CombinedOutput(); err != nil {
		t.Skipf("mkfifo: %v %s", err, out)
	}
	testgit.Run(t, dir, "config", "include.path", fifo)
	for name, call := range map[string]func(ctx context.Context) error{
		"Head":    func(ctx context.Context) error { _, err := Head(ctx, dir); return err },
		"GitDir":  func(ctx context.Context) error { _, err := GitDir(ctx, dir); return err },
		"Changed": func(ctx context.Context) error { _, err := Changed(ctx, dir); return err },
		"Scan":    func(ctx context.Context) error { _, err := Scan(ctx, dir, ""); return err },
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
			defer cancel()
			start := time.Now()
			err := call(ctx)
			if d := time.Since(start); d > 4500*time.Millisecond {
				t.Fatalf("returned after %v", d)
			}
			if err == nil {
				t.Fatal("no error")
			}
		})
	}
}

// TestRunWaitDelay: a git whose child keeps stdout open after git is killed
// does not hold the caller past the budget.
func TestRunWaitDelay(t *testing.T) {
	skipWindows(t)
	bin := t.TempDir()
	write(t, filepath.Join(bin, "git"), "#!/bin/sh\nsleep 5\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := run(ctx, t.TempDir(), "log")
	if err == nil || !strings.Contains(err.Error(), "timed out") || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want timed out", err)
	}
	if d := time.Since(start); d > 2500*time.Millisecond {
		t.Fatalf("returned after %v: WaitDelay is not set", d)
	}
}

func TestScanIncremental(t *testing.T) {
	needGit(t, 2, 24, "--end-of-options")
	ctx := t.Context()
	dir := testgit.Init(t)
	if s, err := Scan(ctx, dir, ""); err != nil || s.Head != "" || len(s.Commits) != 0 {
		t.Fatalf("empty repo: %+v %v", s, err)
	}
	c1 := commitMsg(t, dir, "a\n\nParaplane-Plan: docs/a.md\nParaplane-Wave: W1\n")
	commitMsg(t, dir, "plain")
	c3 := commitMsg(t, dir, "b\n\nParaplane-Plan: docs/b.md\nParaplane-Task: T1, T2\n")
	c4 := commitMsg(t, dir, "lost\n\nParaplane-Plan: docs/a.md\nParaplane-Wave: W2\n\nCo-Authored-By: A <a@b>\n")
	s1, err := Scan(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []TrailerCommit{
		{Hash: c1, Plan: "docs/a.md", Waves: []string{"W1"}},
		{Hash: c3, Plan: "docs/b.md", Tasks: []string{"T1", "T2"}},
		{Hash: c4, Plan: "docs/a.md", LostWaves: []string{"W2"}},
	}
	if s1.Head != c4 || !reflect.DeepEqual(s1.Commits, want) {
		t.Fatalf("full scan: head %s, %+v", s1.Head, s1.Commits)
	}
	c5 := commitMsg(t, dir, "t3\n\nParaplane-Plan: docs/a.md\nParaplane-Task: T3\n")
	s2, err := Scan(ctx, dir, s1.Head)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Head != c5 || !reflect.DeepEqual(s2.Commits, []TrailerCommit{{Hash: c5, Plan: "docs/a.md", Tasks: []string{"T3"}}}) {
		t.Fatalf("incremental: %+v", s2)
	}
	if s3, err := Scan(ctx, dir, s2.Head); err != nil || s3.Head != c5 || len(s3.Commits) != 0 {
		t.Fatalf("no new commits: %+v %v", s3, err)
	}
	// Rewritten history: the old head is not an ancestor, so a full scan.
	testgit.Run(t, dir, "commit", "-q", "--amend", "-m", "t3 again\n\nParaplane-Plan: docs/a.md\nParaplane-Task: T4\n")
	c5b := testgit.Run(t, dir, "rev-parse", "HEAD")
	for _, from := range []string{c5, "zz", "-x", "HEAD"} {
		s, err := Scan(ctx, dir, from)
		if err != nil || s.Head != c5b || len(s.Commits) != 4 || s.Commits[0].Hash != c1 ||
			!reflect.DeepEqual(s.Commits[3].Tasks, []string{"T4"}) {
			t.Fatalf("from %q: %+v %v", from, s, err)
		}
	}
	// CommitsOf folds a scan for one plan as PlanCommits does.
	full, _ := Scan(ctx, dir, "")
	c := CommitsOf(full.Commits, "docs/a.md")
	if !reflect.DeepEqual(c.Waves, []string{"W1"}) || !reflect.DeepEqual(c.Tasks, []string{"T4"}) ||
		len(c.Lost) != 1 || c.Lost[0].ID != "W2" || c.Lost[0].Kind != "wave" || len(c.Lost[0].Commit) < 7 {
		t.Fatalf("CommitsOf: %+v", c)
	}
}

func TestUnreachable(t *testing.T) {
	ctx := t.Context()
	dir := testgit.Init(t)
	c1 := commitMsg(t, dir, "one")
	c2 := commitMsg(t, dir, "two")
	testgit.Run(t, dir, "commit", "-q", "--amend", "-m", "two again")
	head := testgit.Run(t, dir, "rev-parse", "HEAD")
	tree := testgit.Run(t, dir, "rev-parse", "HEAD^{tree}")
	missing := strings.Repeat("ab", 20)
	in := []string{c1, c2, head, "-x", "", "zz", "HEAD", missing, tree, "--all"}
	got, err := Unreachable(ctx, dir, in, head)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{c1: false, c2: true, head: false, "-x": true, "": true, "zz": true, "HEAD": true,
		missing: true, tree: true, "--all": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	if got, err := Unreachable(ctx, dir, nil, head); err != nil || len(got) != 0 {
		t.Fatalf("no hashes: %v %v", got, err)
	}
	if _, err := Unreachable(ctx, dir, []string{c1}, "HEAD"); err == nil {
		t.Fatal("invalid head: no error")
	}
}

func TestIsAncestorValidates(t *testing.T) {
	dir := testgit.Init(t)
	c1 := commitMsg(t, dir, "one")
	for _, bad := range []string{"--help", "HEAD", "", c1[:7]} {
		if _, err := IsAncestor(t.Context(), dir, bad, c1); err == nil {
			t.Errorf("%q: no error", bad)
		}
		if _, err := PlansSince(t.Context(), dir, bad, c1); bad != "" && err == nil {
			t.Errorf("PlansSince from %q: no error", bad)
		}
	}
}

func TestErrOldGit(t *testing.T) {
	for _, ver := range []string{"git version 2.21.0", "git version 2.23.4", "git version weird"} {
		t.Run(ver, func(t *testing.T) { testErrOldGit(t, ver) })
	}
}

func testErrOldGit(t *testing.T, ver string) {
	dir := testgit.Init(t)
	c1 := commitMsg(t, dir, "one")
	versionGit(t, ver)
	ctx := t.Context()
	for name, call := range map[string]func() error{
		"IsAncestor":      func() error { _, err := IsAncestor(ctx, dir, c1, c1); return err },
		"Unreachable":     func() error { _, err := Unreachable(ctx, dir, []string{c1}, c1); return err },
		"PlanCommits":     func() error { _, err := PlanCommits(ctx, dir, "p.md"); return err },
		"PlanCommitFiles": func() error { _, err := PlanCommitFiles(ctx, dir, "p.md"); return err },
		"PlansSince":      func() error { _, err := PlansSince(ctx, dir, "", c1); return err },
		"Scan":            func() error { _, err := Scan(ctx, dir, ""); return err },
	} {
		if err := call(); !errors.Is(err, ErrOldGit) {
			t.Errorf("%s: err = %v, want ErrOldGit", name, err)
		}
	}
}

// TestVersionCached: git version runs once per git binary.
func TestVersionCached(t *testing.T) {
	needGit(t, 2, 24, "--end-of-options")
	skipWindows(t)
	dir := testgit.Init(t)
	commitMsg(t, dir, "one")
	log := filepath.Join(t.TempDir(), "log")
	bin := t.TempDir()
	next, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(bin, "git"), "#!/bin/sh\n"+hasArgFn+
		"if has version; then echo v >> '"+log+"'; fi\nexec '"+next+"' \"$@\"\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for range 3 {
		if _, err := PlanCommits(t.Context(), dir, "p.md"); err != nil {
			t.Fatal(err)
		}
		if _, err := Changed(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := os.ReadFile(log); bytes.Count(b, []byte("v\n")) != 1 {
		t.Fatalf("git version ran %d times, want 1", bytes.Count(b, []byte("v\n")))
	}
}

// TestFixedVersionsOneList: the ErrPartialClone text names exactly the
// versions lazyFetchSafe trusts.
func TestFixedVersionsOneList(t *testing.T) {
	for _, v := range lazyFetchFixed {
		if !lazyFetchSafe(v, true) {
			t.Errorf("%v not safe", v)
		}
		prev := v
		prev[2]--
		if lazyFetchSafe(prev, true) {
			t.Errorf("%v safe", prev)
		}
		if s := fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]); !strings.Contains(ErrPartialClone.Error(), s) {
			t.Errorf("ErrPartialClone does not name %s", s)
		}
	}
}

// BenchmarkScan: one full scan of 10k commits, every 100th with trailers.
func BenchmarkScan(b *testing.B) {
	if runtime.GOOS == "windows" {
		b.Skip()
	}
	dir := testgit.Init(b)
	var in bytes.Buffer
	for i := range 10_000 {
		msg := "c"
		if i%100 == 0 {
			msg = fmt.Sprintf("w\n\nParaplane-Plan: docs/p%d.md\nParaplane-Task: T%d\n", i%3, i)
		}
		fmt.Fprintf(&in, "commit refs/heads/main\ncommitter t <t@t> %d +0000\ndata %d\n%s\n", 1_700_000_000+i, len(msg), msg)
	}
	cmd := exec.Command("git", "-C", dir, "fast-import", "--quiet")
	cmd.Env = testgit.Env()
	cmd.Stdin = &in
	if out, err := cmd.CombinedOutput(); err != nil {
		b.Fatalf("%v\n%s", err, out)
	}
	ctx := context.Background()
	for b.Loop() {
		s, err := Scan(ctx, dir, "")
		if err != nil || len(s.Commits) != 100 {
			b.Fatalf("%d commits, %v", len(s.Commits), err)
		}
	}
}
