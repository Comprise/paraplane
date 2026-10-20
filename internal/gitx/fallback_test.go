package gitx

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeGit puts first on PATH a git wrapper that behaves like git older than
// 2.26: it rejects --show-scope with exit 129 and execs the real git
// otherwise. extra is shell code run before that, with the arguments in "$@".
// It returns the marker the wrapper creates when it rejects --show-scope.
func fakeGit(t *testing.T, extra string) (marker string) {
	t.Helper()
	skipWindows(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	marker = filepath.Join(t.TempDir(), "rejected")
	write(t, filepath.Join(bin, "git"), "#!/bin/sh\n"+
		"for a in \"$@\"; do\n"+
		"  if [ \"$a\" = --show-scope ]; then\n"+
		"    touch '"+marker+"'\n"+
		"    echo \"error: unknown option \\`show-scope'\" >&2\n"+
		"    exit 129\n"+
		"  fi\n"+
		"done\n"+
		extra+"\n"+
		"exec '"+real+"' \"$@\"\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

// hasArg is shell code true when the wrapper's arguments contain $1.
const hasArgFn = "has() { for a in $ARGS; do [ \"$a\" = \"$1\" ] && return 0; done; return 1; }\nARGS=\"$*\"\n"

func assertFellBack(t *testing.T, marker string) {
	t.Helper()
	if !exists(marker) {
		t.Fatal("the fake git never saw --show-scope: the fallback was not exercised")
	}
}

// refuseCases set up a filter driver that git status would run, from the
// local or worktree scope; Changed must refuse each, with and without
// --show-scope.
var refuseCases = []struct {
	name  string
	setup func(t *testing.T, dir string)
}{
	{"local", func(t *testing.T, dir string) {
		git(t, dir, "config", "filter.x.clean", "touch PWNED")
	}},
	{"smudge", func(t *testing.T, dir string) {
		git(t, dir, "config", "filter.x.smudge", "touch PWNED")
	}},
	{"process", func(t *testing.T, dir string) {
		git(t, dir, "config", "filter.x.process", "touch PWNED")
	}},
	{"include", func(t *testing.T, dir string) {
		evil := filepath.Join(t.TempDir(), "evil.cfg")
		write(t, evil, "[filter \"x\"]\n\tclean = touch PWNED\n")
		git(t, dir, "config", "include.path", evil)
	}},
	{"nested include", func(t *testing.T, dir string) {
		d := t.TempDir()
		write(t, filepath.Join(d, "evil.cfg"), "[filter \"x\"]\n\tclean = touch PWNED\n")
		write(t, filepath.Join(d, "outer.cfg"), "[include]\n\tpath = evil.cfg\n")
		git(t, dir, "config", "include.path", filepath.Join(d, "outer.cfg"))
	}},
	{"includeIf", func(t *testing.T, dir string) {
		evil := filepath.Join(t.TempDir(), "evil.cfg")
		write(t, evil, "[filter \"x\"]\n\tclean = touch PWNED\n")
		git(t, dir, "config", "includeIf.gitdir:"+realDir(t, dir)+"/.path", evil)
	}},
	// With extensions.worktreeConfig the worktree query no longer reads the
	// local config, so the local query alone must follow includes.
	{"include with worktreeConfig", func(t *testing.T, dir string) {
		evil := filepath.Join(t.TempDir(), "evil.cfg")
		write(t, evil, "[filter \"x\"]\n\tclean = touch PWNED\n")
		git(t, dir, "config", "extensions.worktreeConfig", "true")
		git(t, dir, "config", "include.path", evil)
	}},
	{"includeIf with worktreeConfig", func(t *testing.T, dir string) {
		evil := filepath.Join(t.TempDir(), "evil.cfg")
		write(t, evil, "[filter \"x\"]\n\tclean = touch PWNED\n")
		git(t, dir, "config", "extensions.worktreeConfig", "true")
		git(t, dir, "config", "includeIf.gitdir:"+realDir(t, dir)+"/.path", evil)
	}},
	{"worktree", func(t *testing.T, dir string) {
		needGit(t, 2, 20, "git config --worktree")
		git(t, dir, "config", "extensions.worktreeConfig", "true")
		git(t, dir, "config", "--worktree", "filter.x.clean", "touch PWNED")
	}},
	{"worktree include", func(t *testing.T, dir string) {
		evil := filepath.Join(t.TempDir(), "evil.cfg")
		write(t, evil, "[filter \"x\"]\n\tclean = touch PWNED\n")
		needGit(t, 2, 20, "git config --worktree")
		git(t, dir, "config", "extensions.worktreeConfig", "true")
		git(t, dir, "config", "--worktree", "include.path", evil)
	}},
}

func TestChangedRefusesAllScopes(t *testing.T) {
	for _, tc := range refuseCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filterRepo(t)
			tc.setup(t, dir)
			assertRefused(t, dir)
		})
	}
}

func TestChangedFallbackRefuses(t *testing.T) {
	for _, tc := range refuseCases {
		t.Run(tc.name, func(t *testing.T) {
			marker := fakeGit(t, "")
			dir := filterRepo(t)
			tc.setup(t, dir)
			assertRefused(t, dir)
			assertFellBack(t, marker)
		})
	}
}

// TestChangedFallbackLinkedWorktree: in a linked worktree the local config is
// the shared one and the worktree config is the linked worktree's own.
func TestChangedFallbackLinkedWorktree(t *testing.T) {
	for _, scope := range []string{"--local", "--worktree"} {
		t.Run(scope, func(t *testing.T) {
			needGit(t, 2, 20, "git config --worktree and git worktree add -q")
			marker := fakeGit(t, "")
			main := filterRepo(t)
			git(t, main, "config", "extensions.worktreeConfig", "true")
			wt := filepath.Join(t.TempDir(), "wt")
			git(t, main, "worktree", "add", "-q", wt)
			git(t, wt, "config", scope, "filter.x.clean", "touch PWNED")
			assertRefused(t, wt)
			assertFellBack(t, marker)
		})
	}
}

// TestChangedFallbackLinkedWorktreeNoExtension: without
// extensions.worktreeConfig, git config --worktree fails (exit 128) in a
// repository with linked worktrees; the fallback must not run it then.
func TestChangedFallbackLinkedWorktreeNoExtension(t *testing.T) {
	for _, tc := range []struct{ dirOf, ext string }{
		{"main", ""}, {"linked", ""}, {"main", "false"}, {"linked", "false"},
	} {
		t.Run(tc.dirOf+"/extension="+tc.ext, func(t *testing.T) {
			marker := fakeGit(t, "")
			main := initRepo(t)
			if tc.ext != "" {
				git(t, main, "config", "extensions.worktreeConfig", tc.ext)
			}
			write(t, filepath.Join(main, "a.txt"), "a\n")
			commitAll(t, main, "init")
			wt := filepath.Join(t.TempDir(), "wt")
			git(t, main, "worktree", "add", wt) // git < 2.20 has no -q
			dir := map[string]string{"main": main, "linked": wt}[tc.dirOf]
			write(t, filepath.Join(dir, "a.txt"), "changed\n")
			got, err := Changed(dir)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, []string{"a.txt"}) {
				t.Fatalf("got %v", got)
			}
			assertFellBack(t, marker)
		})
	}
	t.Run("local filter", func(t *testing.T) {
		marker := fakeGit(t, "")
		main := filterRepo(t)
		wt := filepath.Join(t.TempDir(), "wt")
		git(t, main, "worktree", "add", wt)
		git(t, main, "config", "filter.x.clean", "touch PWNED")
		assertRefused(t, wt)
		assertFellBack(t, marker)
	})
}

// TestReviewExtFlippedByInclude: git's repository format check reads
// extensions.worktreeConfig from .git/config without includes, so an include
// that sets it false does not turn the worktree config off; the fallback must
// still read it and refuse its filter.
func TestReviewExtFlippedByInclude(t *testing.T) {
	needGit(t, 2, 20, "the worktree config")
	marker := fakeGit(t, "")
	dir := filterRepo(t)
	off := filepath.Join(t.TempDir(), "off.cfg")
	write(t, off, "[extensions]\n\tworktreeConfig = false\n")
	git(t, dir, "config", "extensions.worktreeConfig", "true")
	git(t, dir, "config", "include.path", off)
	write(t, filepath.Join(dir, ".git", "config.worktree"), "[filter \"x\"]\n\tclean = touch PWNED\n")
	assertRefused(t, dir)
	assertFellBack(t, marker)
}

// TestChangedFallbackWorktreeQueryGate: the fallback queries --worktree when
// any value of extensions.worktreeConfig, read without or with includes, is
// true, and only then.
func TestChangedFallbackWorktreeQueryGate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		local  []string // values in .git/config, in order
		inc    string   // value in an included file, "" for no include
		wantWt bool
	}{
		{"true then include false", []string{"true"}, "false", true},
		{"true then false", []string{"true", "false"}, "", true},
		{"false then true", []string{"false", "true"}, "", true},
		{"true only in include", nil, "true", true},
		{"yes", []string{"yes"}, "", true},
		{"unset", nil, "", false},
		{"false", []string{"false", "off"}, "false", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wt := filepath.Join(t.TempDir(), "worktree-queried")
			marker := fakeGit(t, hasArgFn+"if has config && has --worktree; then touch '"+wt+"'; fi")
			dir := initRepo(t)
			for _, v := range tc.local {
				git(t, dir, "config", "--add", "extensions.worktreeConfig", v)
			}
			if tc.inc != "" {
				cfg := filepath.Join(t.TempDir(), "inc.cfg")
				write(t, cfg, "[extensions]\n\tworktreeConfig = "+tc.inc+"\n")
				git(t, dir, "config", "include.path", cfg)
			}
			write(t, filepath.Join(dir, "a.txt"), "a\n")
			Changed(dir) // the result depends on what git makes of the values
			assertFellBack(t, marker)
			if exists(wt) != tc.wantWt {
				t.Fatalf("--worktree queried: %v, want %v", exists(wt), tc.wantWt)
			}
		})
	}
}

func TestChangedFallbackNoFilters(t *testing.T) {
	marker := fakeGit(t, "")
	dir := initRepo(t)
	git(t, dir, "config", "filter.x.required", "true") // not clean/smudge/process
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	got, err := Changed(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a.txt"}) {
		t.Fatalf("got %v", got)
	}
	assertFellBack(t, marker)
}

func TestChangedFallbackAllowsGlobalFilter(t *testing.T) {
	marker := fakeGit(t, "")
	home := t.TempDir()
	write(t, filepath.Join(home, ".gitconfig"), "[filter \"lfs\"]\n\tclean = cat\n\tsmudge = cat\n")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	dir := initRepo(t)
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	got, err := Changed(dir)
	if err != nil {
		t.Fatalf("global filter must not refuse: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"a.txt"}) {
		t.Fatalf("got %v", got)
	}
	assertFellBack(t, marker)
}

// TestChangedFallbackNoWorktreeOption: git older than 2.20 does not know
// --worktree (nor extensions.worktreeConfig); the local query still decides.
func TestChangedFallbackNoWorktreeOption(t *testing.T) {
	reject := hasArgFn + "if has config && has --worktree; then echo \"error: unknown option \\`worktree'\" >&2; exit 129; fi"
	t.Run("clean", func(t *testing.T) {
		fakeGit(t, reject)
		dir := initRepo(t)
		write(t, filepath.Join(dir, "a.txt"), "a\n")
		got, err := Changed(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []string{"a.txt"}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("local filter", func(t *testing.T) {
		fakeGit(t, reject)
		dir := filterRepo(t)
		git(t, dir, "config", "filter.x.clean", "touch PWNED")
		assertRefused(t, dir)
	})
}

// TestChangedFallbackFailureRefuses: any failure of a fallback query other
// than "no matches" or "--worktree unknown" refuses. The worktree query runs
// only with extensions.worktreeConfig (wtc).
func TestChangedFallbackFailureRefuses(t *testing.T) {
	const local = "has config && has --local && has --get-regexp"
	const ext = "has config && has --bool"
	for _, tc := range []struct {
		name, extra string
		wtc         bool
	}{
		{"local exit 2", hasArgFn + "if " + local + "; then echo boom >&2; exit 2; fi", false},
		{"local exit 129", hasArgFn + "if " + local + "; then echo \"error: unknown option \\`local'\" >&2; exit 129; fi", false},
		{"local exit 1 with output", hasArgFn + "if " + local + "; then echo filter.x.clean; exit 1; fi", false},
		{"local killed", hasArgFn + "if " + local + "; then kill -9 $$; fi", false},
		{"extension exit 2", hasArgFn + "if " + ext + "; then echo boom >&2; exit 2; fi", false},
		{"extension exit 1 with output", hasArgFn + "if " + ext + "; then echo true; exit 1; fi", false},
		{"extension exit 129", hasArgFn + "if " + ext + "; then echo \"error: unknown option \\`bool'\" >&2; exit 129; fi", false},
		{"worktree exit 3", hasArgFn + "if has config && has --worktree; then echo bad config >&2; exit 3; fi", true},
		{"worktree exit 129 other", hasArgFn + "if has config && has --worktree; then echo \"error: unknown option \\`includes'\" >&2; exit 129; fi", true},
		{"worktree exit 1 with output", hasArgFn + "if has config && has --worktree; then echo filter.x.clean; exit 1; fi", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := fakeGit(t, tc.extra)
			dir := initRepo(t)
			if tc.wtc {
				git(t, dir, "config", "extensions.worktreeConfig", "true")
			}
			write(t, filepath.Join(dir, "a.txt"), "a\n")
			got, err := Changed(dir)
			if err == nil {
				t.Fatalf("got %v, nil error; want a refusal", got)
			}
			assertFellBack(t, marker)
		})
	}
}

// TestChangedShowScopeFailureRefuses: a --show-scope query that fails other
// than with exit 129 or "no matches" refuses and does not fall back.
func TestChangedShowScopeFailureRefuses(t *testing.T) {
	skipWindows(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, script string }{
		{"exit 2", "echo boom >&2; exit 2"},
		{"exit 1 with output", "echo 'local filter.x.clean touch'; exit 1"},
		{"unknown scope with exit 0", "echo 'repository filter.x.clean touch'; exit 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			write(t, filepath.Join(bin, "git"), "#!/bin/sh\n"+hasArgFn+
				"if has --show-scope; then "+tc.script+"; fi\n"+
				"if has --local || has --worktree; then touch '"+filepath.Join(bin, "fellback")+"'; fi\n"+
				"exec '"+real+"' \"$@\"\n")
			if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			dir := initRepo(t)
			write(t, filepath.Join(dir, "a.txt"), "a\n")
			got, err := Changed(dir)
			if err == nil {
				t.Fatalf("got %v, nil error; want a refusal", got)
			}
			if exists(filepath.Join(bin, "fellback")) {
				t.Fatal("fell back although --show-scope was supported")
			}
		})
	}
}

// TestChangedShowScopeTrustedScopes: global and command scopes are trusted.
func TestChangedShowScopeTrustedScopes(t *testing.T) {
	dir := initRepo(t)
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "filter.y.clean")
	t.Setenv("GIT_CONFIG_VALUE_0", "cat")
	got, err := Changed(dir)
	if err != nil {
		t.Fatalf("command-scope filter must not refuse: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"a.txt"}) {
		t.Fatalf("got %v", got)
	}
}

func TestRunOutputCap(t *testing.T) {
	dir := initRepo(t)
	write(t, filepath.Join(dir, "a.txt"), strings.Repeat("x", 4096))
	commitAll(t, dir, "init")
	old := maxOutput
	maxOutput = 1024
	t.Cleanup(func() { maxOutput = old })
	_, err := run(dir, "show", "HEAD:a.txt")
	if err == nil || !strings.Contains(err.Error(), "git output too large") {
		t.Fatalf("err = %v, want git output too large", err)
	}
	out, err := run(dir, "rev-parse", "HEAD")
	if err != nil || len(out) == 0 {
		t.Fatalf("small output: %q, %v", out, err)
	}
	maxOutput = 4096
	if out, err := run(dir, "show", "HEAD:a.txt"); err != nil || len(out) != 4096 {
		t.Fatalf("output at the cap: %d bytes, %v", len(out), err)
	}
	maxOutput = 4095 // one byte short
	if _, err := run(dir, "show", "HEAD:a.txt"); err == nil || !strings.Contains(err.Error(), "git output too large") {
		t.Fatalf("output one byte over the cap: err = %v, want git output too large", err)
	}
}

func TestOutputCapIs16MiB(t *testing.T) {
	if maxOutput != 16<<20 {
		t.Fatalf("maxOutput = %d, want 16 MiB", maxOutput)
	}
}

// versionGit puts first on PATH a git wrapper whose git version prints out
// and that execs the git found before it otherwise.
func versionGit(t *testing.T, out string) {
	t.Helper()
	skipWindows(t)
	next, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	write(t, filepath.Join(bin, "git"), "#!/bin/sh\n"+hasArgFn+
		"if has version; then echo '"+out+"'; exit 0; fi\n"+
		"exec '"+next+"' \"$@\"\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want gitVersion
		ok   bool
	}{
		{"git version 2.45.1\n", gitVersion{2, 45, 1}, true},
		{"git version 2.39.5 (Apple Git-154)\n", gitVersion{2, 39, 5}, true},
		{"git version 2.47.1.windows.1\n", gitVersion{2, 47, 1}, true},
		{"git version 2.46.0.rc0\n", gitVersion{2, 46, 0}, true},
		{"git version 2.45.2.413.g1a2b3c\n", gitVersion{2, 45, 2}, true},
		{"git version 3.0\n", gitVersion{3, 0, 0}, true},
		{"git version x.y\n", gitVersion{}, false},
		{"hub version 2.45.1\n", gitVersion{}, false},
		{"", gitVersion{}, false},
	} {
		got, ok := parseVersion(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLazyFetchSafe(t *testing.T) {
	for _, tc := range []struct {
		v    gitVersion
		safe bool
	}{
		{gitVersion{2, 18, 4}, false}, {gitVersion{2, 26, 3}, false}, {gitVersion{2, 38, 5}, false},
		{gitVersion{2, 39, 3}, false}, {gitVersion{2, 39, 4}, true},
		{gitVersion{2, 40, 1}, false}, {gitVersion{2, 40, 2}, true},
		{gitVersion{2, 41, 0}, false}, {gitVersion{2, 41, 1}, true},
		{gitVersion{2, 42, 1}, false}, {gitVersion{2, 42, 2}, true},
		{gitVersion{2, 43, 3}, false}, {gitVersion{2, 43, 4}, true}, {gitVersion{2, 43, 7}, true},
		{gitVersion{2, 44, 0}, false}, {gitVersion{2, 44, 1}, true},
		{gitVersion{2, 45, 0}, false}, {gitVersion{2, 45, 1}, true},
		{gitVersion{2, 46, 0}, true}, {gitVersion{2, 56, 0}, true}, {gitVersion{3, 0, 0}, true},
		{gitVersion{1, 45, 9}, false},
	} {
		if got := lazyFetchSafe(tc.v, true); got != tc.safe {
			t.Errorf("lazyFetchSafe(%v) = %v, want %v", tc.v, got, tc.safe)
		}
	}
	if lazyFetchSafe(gitVersion{2, 56, 0}, false) {
		t.Error("an unparsed version must not be safe")
	}
}

// partialCases make the repository a partial clone through its own config.
var partialCases = []struct {
	name  string
	setup func(t *testing.T, dir string)
}{
	{"extensions.partialClone", func(t *testing.T, dir string) {
		git(t, dir, "config", "extensions.partialClone", "origin")
	}},
	{"remote promisor", func(t *testing.T, dir string) {
		git(t, dir, "config", "remote.Origin.promisor", "true")
	}},
	{"promisor false", func(t *testing.T, dir string) { // any value counts
		git(t, dir, "config", "remote.origin.promisor", "false")
	}},
	{"include", func(t *testing.T, dir string) {
		cfg := filepath.Join(t.TempDir(), "p.cfg")
		write(t, cfg, "[extensions]\n\tpartialClone = origin\n")
		git(t, dir, "config", "include.path", cfg)
	}},
	{"worktree", func(t *testing.T, dir string) {
		needGit(t, 2, 20, "git config --worktree")
		git(t, dir, "config", "extensions.worktreeConfig", "true")
		git(t, dir, "config", "--worktree", "remote.origin.promisor", "true")
	}},
	{"uploadpack", func(t *testing.T, dir string) {
		git(t, dir, "config", "remote.Origin.uploadpack", "/bin/upload.sh")
	}},
}

// TestChangedPartialCloneOldGit: with a git that may ignore
// GIT_NO_LAZY_FETCH, Changed refuses in a partial clone, on both the
// --show-scope and the fallback path, and works in any other repository.
func TestChangedPartialCloneOldGit(t *testing.T) {
	for _, path := range []string{"show-scope", "fallback"} {
		for _, ver := range []string{"git version 2.43.3", "git version 2.45.0", "git version 2.25.1", "git version unknown"} {
			for _, tc := range partialCases {
				t.Run(path+"/"+ver+"/"+tc.name, func(t *testing.T) {
					var marker string
					if path == "fallback" {
						marker = fakeGit(t, "")
					}
					versionGit(t, ver)
					dir := initRepo(t)
					tc.setup(t, dir)
					write(t, filepath.Join(dir, "a.txt"), "a\n")
					if _, err := Changed(dir); !errors.Is(err, ErrPartialClone) {
						t.Fatalf("err = %v, want ErrPartialClone", err)
					}
					if marker != "" {
						assertFellBack(t, marker)
					}
				})
			}
			t.Run(path+"/"+ver+"/not a partial clone", func(t *testing.T) {
				if path == "fallback" {
					fakeGit(t, "")
				}
				versionGit(t, ver)
				dir := initRepo(t)
				git(t, dir, "config", "remote.origin.url", "file:///nonexistent")
				write(t, filepath.Join(dir, "a.txt"), "a\n")
				got, err := Changed(dir)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, []string{"a.txt"}) {
					t.Fatalf("got %v", got)
				}
			})
		}
	}
}

// TestChangedPartialCloneSafeGit: a git that honors GIT_NO_LAZY_FETCH runs
// git status in a partial clone; a global promisor setting is trusted.
func TestChangedPartialCloneSafeGit(t *testing.T) {
	for _, ver := range []string{"git version 2.43.4", "git version 2.45.1", "git version 2.46.0.windows.1"} {
		t.Run(ver, func(t *testing.T) {
			versionGit(t, ver)
			dir := initRepo(t)
			git(t, dir, "config", "extensions.partialClone", "origin")
			write(t, filepath.Join(dir, "a.txt"), "a\n")
			got, err := Changed(dir)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, []string{"a.txt"}) {
				t.Fatalf("got %v", got)
			}
		})
	}
	// A promisor remote from the trusted global config would still fetch
	// through a repo-set uploadpack: refused on old git, allowed on safe git.
	for _, tc := range []struct {
		ver  string
		safe bool
	}{{"git version 2.43.3", false}, {"git version 2.25.1", false}, {"git version 2.43.4", true}, {"git version 2.46.0", true}} {
		for _, path := range []string{"show-scope", "fallback"} {
			t.Run("global promisor and local uploadpack/"+path+"/"+tc.ver, func(t *testing.T) {
				if path == "fallback" {
					fakeGit(t, "")
				}
				versionGit(t, tc.ver)
				home := t.TempDir()
				write(t, filepath.Join(home, ".gitconfig"), "[remote \"origin\"]\n\tpromisor = true\n")
				t.Setenv("HOME", home)
				t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
				dir := initRepo(t)
				git(t, dir, "config", "remote.origin.url", "file:///nonexistent")
				git(t, dir, "config", "remote.origin.uploadpack", "/bin/upload.sh")
				write(t, filepath.Join(dir, "a.txt"), "a\n")
				got, err := Changed(dir)
				if !tc.safe {
					if !errors.Is(err, ErrPartialClone) {
						t.Fatalf("err = %v, want ErrPartialClone", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, []string{"a.txt"}) {
					t.Fatalf("got %v", got)
				}
			})
		}
	}
	// On old git a partial clone marker counts from any scope: the lazy fetch
	// could run any repo-set transport. On safe git it does not matter.
	for _, tc := range []struct {
		ver, scope, key string
		safe            bool
	}{
		{"git version 2.43.3", "global", "remote.origin.promisor", false},
		{"git version 2.43.3", "global", "extensions.partialClone", false},
		{"git version 2.43.3", "command", "remote.origin.promisor", false},
		{"git version 2.25.1", "global", "remote.origin.promisor", false},
		{"git version 2.43.4", "global", "remote.origin.promisor", true},
		{"git version 2.43.4", "command", "extensions.partialClone", true},
	} {
		for _, path := range []string{"show-scope", "fallback"} {
			t.Run(tc.scope+" "+tc.key+" alone/"+path+"/"+tc.ver, func(t *testing.T) {
				if path == "fallback" {
					fakeGit(t, "")
				}
				versionGit(t, tc.ver)
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
				dir := initRepo(t)
				if tc.scope == "global" {
					_, val, _ := strings.Cut(tc.key, ".")
					sec, name, _ := strings.Cut(val, ".")
					if name == "" { // extensions.partialClone
						write(t, filepath.Join(home, ".gitconfig"), "[extensions]\n\tpartialClone = origin\n")
					} else {
						write(t, filepath.Join(home, ".gitconfig"), "[remote \""+sec+"\"]\n\t"+name+" = true\n")
					}
				} else {
					needGit(t, 2, 31, "GIT_CONFIG_COUNT")
					t.Setenv("GIT_CONFIG_COUNT", "1")
					t.Setenv("GIT_CONFIG_KEY_0", tc.key)
					t.Setenv("GIT_CONFIG_VALUE_0", "origin")
				}
				write(t, filepath.Join(dir, "a.txt"), "a\n")
				got, err := Changed(dir)
				if !tc.safe {
					if !errors.Is(err, ErrPartialClone) {
						t.Fatalf("err = %v, want ErrPartialClone", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, []string{"a.txt"}) {
					t.Fatalf("got %v", got)
				}
			})
		}
	}
	t.Run("global uploadpack alone on old git", func(t *testing.T) {
		versionGit(t, "git version 2.30.0")
		home := t.TempDir()
		write(t, filepath.Join(home, ".gitconfig"), "[remote \"origin\"]\n\tuploadpack = /bin/upload.sh\n")
		t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
		dir := initRepo(t)
		write(t, filepath.Join(dir, "a.txt"), "a\n")
		if _, err := Changed(dir); err != nil {
			t.Fatalf("a global uploadpack without a partial clone must not refuse: %v", err)
		}
	})
}

// TestChangedPartialCloneQueryFailureRefuses: on old git a failing partial
// clone or uploadpack query refuses.
func TestChangedPartialCloneQueryFailureRefuses(t *testing.T) {
	for _, tc := range []struct{ name, key, code, out string }{
		{"promisor exit 2", `\.promisor)$`, "2", ""},
		{"promisor exit 1 with output", `\.promisor)$`, "1", "remote.origin.promisor true"},
		{"uploadpack exit 2", `\.uploadpack$`, "2", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			versionGit(t, "git version 2.30.0")
			bin := t.TempDir()
			next, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(bin, "git"), "#!/bin/sh\n"+
				"for a in \"$@\"; do case \"$a\" in *'"+tc.key+"') echo '"+tc.out+"'; echo boom >&2; exit "+tc.code+";; esac; done\n"+
				"exec '"+next+"' \"$@\"\n")
			if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			dir := initRepo(t)
			write(t, filepath.Join(dir, "a.txt"), "a\n")
			if got, err := Changed(dir); err == nil {
				t.Fatalf("got %v, nil error; want a refusal", got)
			}
		})
	}
}

func TestWaveCommitsNeedsGit222(t *testing.T) {
	for _, tc := range []struct {
		ver string
		ok  bool
	}{
		{"git version 2.21.4", false}, {"git version 2.18.0", false}, {"git version weird", false},
		{"git version 2.22.0", true}, {"git version 2.39.5 (Apple Git-154)", true},
	} {
		t.Run(tc.ver, func(t *testing.T) {
			if !tc.ok {
				versionGit(t, tc.ver)
			}
			dir := initRepo(t)
			write(t, filepath.Join(dir, "a.txt"), "a\n")
			git(t, dir, "add", "-A")
			git(t, dir, "commit", "-q", "-m", "w\n\nParaplane-Plan: plan.md\nParaplane-Wave: W1\n")
			if tc.ok {
				needGit(t, 2, 22, "%(trailers:key=)")
				versionGit(t, tc.ver)
			}
			got, err := WaveCommits(dir, "plan.md")
			if !tc.ok {
				if err == nil || !strings.Contains(err.Error(), "git 2.22 or later is required for progress") {
					t.Fatalf("got %v, %v; want the git 2.22 error", got, err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, []string{"W1"}) {
				t.Fatalf("got %v, %v; want [W1]", got, err)
			}
		})
	}
}

// realDir is dir with symlinks resolved, as git matches includeIf gitdir:
// against the real path (macOS t.TempDir is under the /var symlink).
func realDir(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(real)
}
