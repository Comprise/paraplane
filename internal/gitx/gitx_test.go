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

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
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
	if _, err := Changed(t.TempDir()); err == nil {
		t.Fatal("want error")
	}
}

func TestWaveCommits(t *testing.T) {
	dir := initRepo(t)
	got, err := WaveCommits(dir, "plan.md")
	if err != nil || len(got) != 0 {
		t.Fatalf("empty repo: got %v, %v", got, err)
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
	got, err = WaveCommits(dir, "plan.md")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"W1", "W2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWaveCommitsNotRepo(t *testing.T) {
	if _, err := WaveCommits(t.TempDir(), "plan.md"); err == nil {
		t.Fatal("want error")
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
	t.Setenv("GIT_CONFIG", "/dev/null")
	assertRefused(t, dir)
}

func TestChangedNoLazyFetch(t *testing.T) {
	dir := initRepo(t)
	var body strings.Builder
	for i := 0; i < 50; i++ {
		body.WriteString("line number " + strings.Repeat("x", i) + "\n")
	}
	write(t, filepath.Join(dir, "a.txt"), body.String())
	commitAll(t, dir, "init")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD:a.txt").Output()
	if err != nil {
		t.Fatal(err)
	}
	h := strings.TrimSpace(string(out))
	marker := filepath.Join(t.TempDir(), "PWNED")
	git(t, dir, "config", "extensions.partialClone", "origin")
	git(t, dir, "config", "remote.origin.url", "file:///nonexistent")
	git(t, dir, "config", "remote.origin.promisor", "true")
	git(t, dir, "config", "remote.origin.uploadpack", "touch "+marker+"; false")
	if err := os.Remove(filepath.Join(dir, ".git", "objects", h[:2], h[2:])); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "b.txt"), body.String()+"more\n")
	git(t, dir, "add", "-A")
	Changed(dir) // may fail closed; it must not run the transport
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("lazy fetch ran repo-configured uploadpack (stat err %v)", err)
	}
}
