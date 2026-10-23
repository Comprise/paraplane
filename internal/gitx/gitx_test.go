package gitx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestMain makes the tests hermetic: the user's global git config is never
// read, no variable points git at another repository, and repository
// discovery stops at the temp directory, so a "not a repository" directory is
// never inside some enclosing repository.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "gitx-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, v := range repoVars {
		os.Unsetenv(strings.TrimSuffix(v, "="))
	}
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	os.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Setenv("GIT_CEILING_DIRECTORIES", os.TempDir())
	if v, ok, err := version(os.TempDir()); err != nil || !ok {
		fmt.Fprintln(os.Stderr, "git version:", err)
		os.Exit(1)
	} else {
		realVersion = v
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// testEnv is the environment for the git commands the tests run themselves:
// the current one without repository variables (a test may set GIT_CONFIG)
// and without global or system config, plus extra.
func testEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !slices.ContainsFunc(repoVars, func(p string) bool { return strings.HasPrefix(kv, p) }) {
			env = append(env, kv)
		}
	}
	return append(append(env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1"), extra...)
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitOut runs git in dir with stdin and returns its trimmed stdout.
func gitOut(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testEnv()
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// gitRun runs git in dir with extra environment and ignores the result; it is
// the unprotected control run of the hardening tests.
func gitRun(dir string, extra []string, args ...string) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testEnv(extra...)
	cmd.Run()
}

// realVersion is the version of the git on PATH when the tests start, before
// any test puts a fake git first.
var realVersion gitVersion

// needGit skips the test when the real git is older than major.minor, which
// lacks feature.
func needGit(t *testing.T, major, minor int, feature string) {
	t.Helper()
	if v := realVersion; !v.atLeast(major, minor, 0) {
		t.Skipf("git %d.%d.%d has no %s (needs %d.%d)", v[0], v[1], v[2], feature, major, minor)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", msg)
}

func TestChangedModifiedUntracked(t *testing.T) {
	dir := initRepo(t)
	write(t, filepath.Join(dir, "a.go"), "a\n")
	write(t, filepath.Join(dir, "z.go"), "z\n")
	commitAll(t, dir, "init")
	write(t, filepath.Join(dir, "z.go"), "z2\n")
	write(t, filepath.Join(dir, "new", "n.go"), "n\n")
	got, err := Changed(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"new/n.go", "z.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestChangedRename(t *testing.T) {
	dir := initRepo(t)
	write(t, filepath.Join(dir, "old.go"), "content that is long enough to detect a rename\n")
	commitAll(t, dir, "init")
	git(t, dir, "mv", "old.go", "new.go")
	got, err := Changed(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"new.go", "old.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestChangedSpacesInName(t *testing.T) {
	dir := initRepo(t)
	write(t, filepath.Join(dir, "a.go"), "a\n")
	commitAll(t, dir, "init")
	write(t, filepath.Join(dir, "my dir", "a b.go"), "x\n")
	got, err := Changed(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"my dir/a b.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestChangedSubdirRoot(t *testing.T) {
	dir := initRepo(t)
	write(t, filepath.Join(dir, "sub", "a.go"), "a\n")
	write(t, filepath.Join(dir, "other", "b.go"), "b\n")
	commitAll(t, dir, "init")
	write(t, filepath.Join(dir, "sub", "a.go"), "a2\n")
	write(t, filepath.Join(dir, "other", "b.go"), "b2\n")
	got, err := Changed(filepath.Join(dir, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestChangedNotRepo(t *testing.T) {
	_, err := Changed(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("err = %v, want a not-a-git-repository error", err)
	}
}

func TestWaveCommits(t *testing.T) {
	needGit(t, 2, 22, "%(trailers:key=)")
	dir := initRepo(t)
	got, lost, err := WaveCommits(dir, "plan.md")
	if err != nil || len(got) != 0 || len(lost) != 0 {
		t.Fatalf("empty repo: got %v, %v, %v", got, lost, err)
	}
	const p = "Paraplane-Plan: "
	msgs := []string{
		"one\n\n" + p + "plan.md\nParaplane-Wave: W1\n",
		"other plan\n\n" + p + "other.md\nParaplane-Wave: W9\n",
		"plain commit",
		"bad id\n\n" + p + "plan.md\nParaplane-Wave: 1bad id\n",
		"two\n\n" + p + "plan.md\nParaplane-Wave: W2\n",
		"dup\n\n" + p + "plan.md\nParaplane-Wave: W1\n",
	}
	for i, m := range msgs {
		write(t, filepath.Join(dir, "f.txt"), strings.Repeat("x", i+1))
		commitAll(t, dir, m)
	}
	got, lost, err = WaveCommits(dir, "plan.md")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"W1", "W2"}
	if !reflect.DeepEqual(got, want) || len(lost) != 0 {
		t.Fatalf("got %v, lost %v; want %v", got, lost, want)
	}
}

// TestWaveCommitsLostTrailers: Paraplane-* lines that git does not read as
// trailers (a blank line before Co-Authored-By) are reported, once per wave,
// unless the wave also has a commit with real trailers.
func TestWaveCommitsLostTrailers(t *testing.T) {
	needGit(t, 2, 22, "%(trailers:key=)")
	dir := initRepo(t)
	msgs := []string{
		"w1\n\nParaplane-Plan: plan.md\nParaplane-Wave: W1\n\nCo-Authored-By: A <a@b>\n",
		"w2\n\nparaplane-plan: plan.md\nparaplane-wave: W2\n\nCo-Authored-By: A <a@b>\n",
		"w2 again\n\nParaplane-Plan: plan.md\nParaplane-Wave: W2\n\nSigned-off-by: A <a@b>\n",
		"w3 ok\n\nParaplane-Plan: plan.md\nParaplane-Wave: W3\nCo-Authored-By: A <a@b>\n",
		"w3 lost too\n\nParaplane-Plan: plan.md\nParaplane-Wave: W3\n\nCo-Authored-By: A <a@b>\n",
		"other plan\n\nParaplane-Plan: other.md\nParaplane-Wave: W4\n\nCo-Authored-By: A <a@b>\n",
		"stage lost\n\nParaplane-Plan: plan.md\nParaplane-Wave: W5\nParaplane-Wave: W6\n\nCo-Authored-By: A <a@b>\n",
		"mixed\n\nParaplane-Wave: W7\n\nParaplane-Plan: plan.md\nParaplane-Wave: W8\nCo-Authored-By: A <a@b>\n",
		"prose mentions Paraplane-Wave: W5 inline\n",
	}
	for i, m := range msgs {
		write(t, filepath.Join(dir, "f.txt"), strings.Repeat("x", i+1))
		commitAll(t, dir, m)
	}
	got, lost, err := WaveCommits(dir, "plan.md")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"W3", "W8"}) {
		t.Errorf("waves %v, want [W3 W8]", got)
	}
	var ids []string
	for _, l := range lost {
		if len(l.Commit) < 7 {
			t.Errorf("short commit %q", l.Commit)
		}
		ids = append(ids, l.Wave)
	}
	if !reflect.DeepEqual(ids, []string{"W1", "W2", "W5", "W6", "W7"}) {
		t.Errorf("lost %v, want W1 W2 W5 W6 W7", lost)
	}
}

func TestWaveCommitsNotRepo(t *testing.T) {
	needGit(t, 2, 22, "%(trailers:key=)")
	_, _, err := WaveCommits(t.TempDir(), "plan.md")
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("err = %v, want a not-a-git-repository error", err)
	}
}

func TestChangedRefusesFilterDriver(t *testing.T) {
	dir := initRepo(t)
	write(t, filepath.Join(dir, ".gitattributes"), "* filter=x\n")
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	commitAll(t, dir, "init")
	git(t, dir, "config", "filter.x.clean", "touch PWNED")
	write(t, filepath.Join(dir, "a.txt"), "changed\n") // tracked and modified: git status would run the clean filter
	_, err := Changed(dir)
	if !errors.Is(err, ErrFilterDrivers) {
		t.Fatalf("err = %v, want ErrFilterDrivers", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "PWNED")); !os.IsNotExist(statErr) {
		t.Fatalf("filter driver ran: PWNED exists (stat err %v)", statErr)
	}
}

func TestChangedNoFilterDriversOK(t *testing.T) {
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
}

func TestChangedIgnoresSubmodules(t *testing.T) {
	sub := initRepo(t)
	write(t, filepath.Join(sub, "s.txt"), "s\n")
	commitAll(t, sub, "sub")
	dir := initRepo(t)
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	commitAll(t, dir, "init")
	git(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "mod")
	commitAll(t, dir, "add submodule")
	write(t, filepath.Join(dir, "mod", "s.txt"), "dirty\n")
	write(t, filepath.Join(dir, "mod", "untracked.txt"), "u\n")
	got, err := Changed(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("submodule changes leaked: %v", got)
	}
}

func filterRepo(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	write(t, filepath.Join(dir, ".gitattributes"), "* filter=x\n")
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	commitAll(t, dir, "init")
	return dir
}

func assertRefused(t *testing.T, dir string) {
	t.Helper()
	write(t, filepath.Join(dir, "a.txt"), "changed\n")
	_, err := Changed(dir)
	if !errors.Is(err, ErrFilterDrivers) {
		t.Fatalf("err = %v, want ErrFilterDrivers", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "PWNED")); !os.IsNotExist(statErr) {
		t.Fatalf("filter driver ran (stat err %v)", statErr)
	}
}

func TestChangedRefusesIncludedFilter(t *testing.T) {
	dir := filterRepo(t)
	evil := filepath.Join(t.TempDir(), "evil.cfg")
	write(t, evil, "[filter \"x\"]\n\tclean = touch PWNED\n")
	git(t, dir, "config", "include.path", evil)
	assertRefused(t, dir)
}

func TestChangedRefusesWorktreeFilter(t *testing.T) {
	needGit(t, 2, 20, "git config --worktree")
	dir := filterRepo(t)
	git(t, dir, "config", "extensions.worktreeConfig", "true")
	git(t, dir, "config", "--worktree", "filter.x.clean", "touch PWNED")
	assertRefused(t, dir)
}

func TestChangedAllowsGlobalFilter(t *testing.T) {
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
}

func TestChangedIgnoresGitConfigEnv(t *testing.T) {
	dir := filterRepo(t)
	git(t, dir, "config", "filter.x.clean", "touch PWNED")
	t.Setenv("GIT_CONFIG", os.DevNull)
	assertRefused(t, dir)
}

// TestWaveCommitsTrailers: a plan trailer without a wave trailer is ignored;
// keys match case-insensitively; a commit may close several waves (one
// Paraplane-Wave trailer each, an invalid one skipped); when a commit repeats
// the plan trailer, its last occurrence wins.
func TestWaveCommitsTrailers(t *testing.T) {
	needGit(t, 2, 22, "%(trailers:key=)")
	dir := initRepo(t)
	msgs := []string{
		"no wave\n\nParaplane-Plan: plan.md\n",
		"wave only\n\nParaplane-Wave: W7\n",
		"lower case\n\nparaplane-plan: plan.md\nPARAPLANE-WAVE: W1\n",
		"two waves\n\nParaplane-Plan: plan.md\nParaplane-Wave: W8\nParaplane-Wave: 1bad\nParaplane-Wave: W2\n",
		"a list\n\nParaplane-Plan: plan.md\nParaplane-Wave: W4, W5,W6\nParaplane-Wave: W7, 1bad\n",
		"dup plan, last other\n\nParaplane-Plan: plan.md\nParaplane-Plan: other.md\nParaplane-Wave: W9\n",
		"dup plan, last ours\n\nParaplane-Plan: other.md\nParaplane-Plan: plan.md\nParaplane-Wave: W3\n",
	}
	for i, m := range msgs {
		write(t, filepath.Join(dir, "f.txt"), strings.Repeat("x", i+1))
		commitAll(t, dir, m)
	}
	got, _, err := WaveCommits(dir, "plan.md")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"W1", "W8", "W2", "W4", "W5", "W6", "W3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// TestChangedDeletedAndStaged: deletions (staged or not) and changes that are
// only in the index are reported.
func TestChangedDeletedAndStaged(t *testing.T) {
	dir := initRepo(t)
	for _, f := range []string{"del.go", "rm.go", "staged.go", "keep.go"} {
		write(t, filepath.Join(dir, f), f+"\n")
	}
	commitAll(t, dir, "init")
	if err := os.Remove(filepath.Join(dir, "del.go")); err != nil { // unstaged deletion
		t.Fatal(err)
	}
	git(t, dir, "rm", "-q", "rm.go") // staged deletion
	write(t, filepath.Join(dir, "staged.go"), "changed\n")
	git(t, dir, "add", "staged.go") // staged only: work tree equals index
	write(t, filepath.Join(dir, "added.go"), "new\n")
	git(t, dir, "add", "added.go") // new file, staged only
	got, err := Changed(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"added.go", "del.go", "rm.go", "staged.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
