package gitx

import (
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
