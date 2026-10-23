package gitx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"paraplane/internal/testgit"
)

// treelessClone returns a treeless (--filter=tree:0) clone of a repository
// with plan commits; its remote.origin.uploadpack is the marker program, so
// any lazy fetch of a missing tree creates the marker. base is the first
// commit, head the last.
func treelessClone(t *testing.T) (dir, base, head, marker string) {
	t.Helper()
	src := testgit.Init(t)
	testgit.Commit(t, src, "f", "base")
	for i, msg := range []string{
		"w1\n\nParaplane-Plan: plan.md\nParaplane-Wave: W1\n",
		"t1\n\nParaplane-Plan: plan.md\nParaplane-Task: T1\n",
	} {
		write(t, filepath.Join(src, "sub", "a.go"), "package a // "+string(rune('a'+i))+"\n")
		testgit.Run(t, src, "add", "-A")
		testgit.Run(t, src, "commit", "-q", "-m", msg)
	}
	testgit.Run(t, src, "config", "uploadpack.allowFilter", "true")
	dir = filepath.Join(t.TempDir(), "clone")
	testgit.Run(t, src, "clone", "-q", "--filter=tree:0", "file://"+filepath.ToSlash(src), dir)
	prog, marker := testgit.MarkerProgram(t)
	testgit.Run(t, dir, "config", "remote.origin.uploadpack", prog)
	base = testgit.Run(t, dir, "rev-list", "--max-parents=0", "HEAD")
	head = testgit.Run(t, dir, "rev-parse", "HEAD")
	return dir, base, head, marker
}

// oldGit puts first on PATH testgit.FakeOldGit: a git 2.34.1 that ignores
// GIT_NO_LAZY_FETCH.
func oldGit(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", testgit.FakeOldGit(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestLazyFetchAllFunctions: with a git that ignores GIT_NO_LAZY_FETCH, no
// gitx function reading a treeless clone runs the repo-configured
// uploadpack. Refusing is fine; fetching is not.
func TestLazyFetchAllFunctions(t *testing.T) {
	needGit(t, 2, 24, "--end-of-options")
	// Control: an unprotected git log --name-only does fetch here.
	cdir, _, _, cmarker := treelessClone(t)
	gitRun(cdir, nil, "log", "--name-only")
	if !exists(cmarker) {
		t.Skip("control: git log --name-only did not lazy-fetch in a treeless clone here")
	}
	ctx := context.Background()
	calls := []struct {
		name string
		call func(dir, base, head string) error
	}{
		{"Changed", func(d, _, _ string) error { _, err := Changed(ctx, d); return err }},
		{"PlanCommits", func(d, _, _ string) error { _, err := PlanCommits(ctx, d, "plan.md"); return err }},
		{"PlanCommitFiles", func(d, _, _ string) error { _, err := PlanCommitFiles(ctx, d, "plan.md"); return err }},
		{"PlansSince", func(d, b, h string) error { _, err := PlansSince(ctx, d, b, h); return err }},
		{"Scan", func(d, _, _ string) error { _, err := Scan(ctx, d, ""); return err }},
		{"Unreachable", func(d, b, h string) error { _, err := Unreachable(ctx, d, []string{b, h}, h); return err }},
		{"IsAncestor", func(d, b, h string) error { _, err := IsAncestor(ctx, d, b, h); return err }},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			dir, base, head, marker := treelessClone(t)
			oldGit(t)
			err := c.call(dir, base, head)
			assertNoMarker(t, marker, c.name)
			if !errors.Is(err, ErrPartialClone) {
				t.Errorf("err = %v, want ErrPartialClone", err)
			}
		})
	}
	// The second layer: GIT_ALLOW_PROTOCOL=none stops the fetch of an
	// unguarded git command even when GIT_NO_LAZY_FETCH is ignored.
	t.Run("run without the guard", func(t *testing.T) {
		dir, _, _, marker := treelessClone(t)
		oldGit(t)
		_, _ = run(ctx, dir, "log", "--name-only")
		assertNoMarker(t, marker, "git log --name-only through run")
	})
}
