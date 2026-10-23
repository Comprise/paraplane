package gitx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// Every test here builds a repository whose own config makes git run a
// program that creates a marker. It checks that gitx never runs the program,
// then runs the same git command without the protection under test (the
// control). If the control does not create the marker either, this git
// version cannot reproduce the attack and the test skips.

func skipWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the configured programs are shell scripts")
	}
}

// markerProgram writes an executable script dir/name that creates a marker
// file and fails. It returns the script and marker paths.
func markerProgram(t *testing.T, dir, name string) (prog, marker string) {
	t.Helper()
	marker = filepath.Join(t.TempDir(), "PWNED")
	prog = filepath.Join(dir, name)
	write(t, prog, "#!/bin/sh\ntouch '"+marker+"'\nexit 1\n")
	if err := os.Chmod(prog, 0o755); err != nil {
		t.Fatal(err)
	}
	return prog, marker
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func assertNoMarker(t *testing.T, marker, what string) {
	t.Helper()
	if exists(marker) {
		t.Fatalf("%s ran a repo-configured program", what)
	}
}

func control(t *testing.T, marker, what string) {
	t.Helper()
	if !exists(marker) {
		t.Skipf("control: %s did not run the program here, the test proves nothing", what)
	}
}

// signedRepo returns a repository whose HEAD is a commit with a forged
// signature between begin and end lines and with the wave trailers.
func signedRepo(t *testing.T, begin, end string) string {
	t.Helper()
	dir := initRepo(t)
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	commitAll(t, dir, "init")
	tree := gitOut(t, dir, "", "rev-parse", "HEAD^{tree}")
	parent := gitOut(t, dir, "", "rev-parse", "HEAD")
	// git verifies a signature it cannot check just the same: it hands it to
	// the program anyway.
	obj := "tree " + tree + "\nparent " + parent + "\n" +
		"author Test <t@example.com> 1700000000 +0000\n" +
		"committer Test <t@example.com> 1700000000 +0000\n" +
		"gpgsig " + begin + "\n \n iQEzBAABCAAdFiEEforgedforgedforged\n " + end + "\n" +
		"\nwave\n\nParaplane-Plan: plan.md\nParaplane-Wave: W1\n"
	h := gitOut(t, dir, obj, "hash-object", "-t", "commit", "-w", "--stdin")
	git(t, dir, "update-ref", "HEAD", h)
	return dir
}

// TestWaveCommitsNoSignatureProgram: the repository sets log.showSignature and
// names the verification program of each signature format.
func TestWaveCommitsNoSignatureProgram(t *testing.T) {
	skipWindows(t)
	needGit(t, 2, 24, "--end-of-options")
	for _, tc := range []struct{ name, format, key, begin, end string }{
		{"openpgp", "openpgp", "gpg.program", "-----BEGIN PGP SIGNATURE-----", "-----END PGP SIGNATURE-----"},
		{"ssh", "ssh", "gpg.ssh.program", "-----BEGIN SSH SIGNATURE-----", "-----END SSH SIGNATURE-----"},
		{"x509", "x509", "gpg.x509.program", "-----BEGIN SIGNED MESSAGE-----", "-----END SIGNED MESSAGE-----"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.format == "ssh" {
				needGit(t, 2, 34, "gpg.format=ssh")
			}
			dir := signedRepo(t, tc.begin, tc.end)
			prog, marker := markerProgram(t, t.TempDir(), "verify.sh")
			signers := filepath.Join(t.TempDir(), "allowed_signers") // ssh verifies only with one
			write(t, signers, "")
			git(t, dir, "config", "log.showSignature", "true")
			git(t, dir, "config", "gpg.format", tc.format)
			git(t, dir, "config", tc.key, prog)
			git(t, dir, "config", "gpg.ssh.allowedSignersFile", signers)

			got, _, err := waveCommits(t, dir, "plan.md")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, []string{"W1"}) {
				t.Fatalf("got %v want [W1]", got)
			}
			assertNoMarker(t, marker, "WaveCommits")
			// Explicit --show-signature: only the program override protects.
			run(context.Background(), dir, "log", "--show-signature", "-1")
			assertNoMarker(t, marker, "git log --show-signature through run")

			gitRun(dir, nil, "log", "-1") // the repo setting alone
			control(t, marker, "plain git log with log.showSignature=true")
		})
	}
}

// TestRunDisablesShowSignature isolates the log.showSignature override: the
// program override is "false", found on PATH, here a script with a marker.
func TestRunDisablesShowSignature(t *testing.T) {
	skipWindows(t)
	needGit(t, 2, 24, "--end-of-options")
	dir := signedRepo(t, "-----BEGIN PGP SIGNATURE-----", "-----END PGP SIGNATURE-----")
	bin := t.TempDir()
	_, marker := markerProgram(t, bin, "false")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	git(t, dir, "config", "log.showSignature", "true")

	if _, err := run(context.Background(), dir, "log", "-1"); err != nil {
		t.Fatal(err)
	}
	assertNoMarker(t, marker, "git log through run")
	if _, _, err := waveCommits(t, dir, "plan.md"); err != nil {
		t.Fatal(err)
	}
	assertNoMarker(t, marker, "WaveCommits")

	gitRun(dir, nil, "-c", "gpg.program=false", "log", "-1")
	control(t, marker, "git log with log.showSignature=true and gpg.program=false")
}

// TestRunNoHooks isolates the core.hooksPath override with a command that
// runs a hook whatever the environment.
func TestRunNoHooks(t *testing.T) {
	skipWindows(t)
	dir := initRepo(t)
	hooks := t.TempDir()
	_, marker := markerProgram(t, hooks, "pre-commit")
	git(t, dir, "config", "core.hooksPath", hooks)

	if _, err := run(context.Background(), dir, "commit", "--allow-empty", "-q", "-m", "x"); err != nil {
		t.Fatal(err)
	}
	assertNoMarker(t, marker, "git commit through run")

	gitRun(dir, nil, "commit", "--allow-empty", "-q", "-m", "y")
	control(t, marker, "plain git commit")
}

// racy gives path and the index the same old mtime, so that git status must
// look at the content of path and would refresh (write) the index.
func racy(t *testing.T, dir, path string) time.Time {
	t.Helper()
	ts := time.Now().Add(-time.Hour).Truncate(time.Second)
	for _, p := range []string{filepath.Join(dir, path), filepath.Join(dir, ".git", "index")} {
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	return ts
}

// TestChangedNoIndexWriteOrHook: a refresh of a racy index would write the
// index (stopped by GIT_OPTIONAL_LOCKS=0) and run post-index-change (stopped
// by GIT_OPTIONAL_LOCKS=0 and by core.hooksPath).
func TestChangedNoIndexWriteOrHook(t *testing.T) {
	skipWindows(t)
	dir := initRepo(t)
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	commitAll(t, dir, "init")
	hooks := t.TempDir()
	_, marker := markerProgram(t, hooks, "post-index-change")
	git(t, dir, "config", "core.hooksPath", hooks)
	ts := racy(t, dir, "a.txt")

	got, err := Changed(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want no changes", got)
	}
	assertNoMarker(t, marker, "Changed")
	fi, err := os.Stat(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Equal(ts) {
		t.Fatalf("Changed wrote the index (mtime %v, was %v)", fi.ModTime(), ts)
	}

	gitRun(dir, []string{"GIT_OPTIONAL_LOCKS=1"}, statusArgs...)
	control(t, marker, "git status of Changed with optional locks")
}

// TestChangedNoLazyFetch: in a partial clone whose .gitattributes and
// .gitmodules blobs are missing (skip-worktree, absent from the work tree),
// Changed's own git status needs them and would fetch them through the
// repo-configured uploadpack.
func TestChangedNoLazyFetch(t *testing.T) {
	skipWindows(t)
	dir := initRepo(t)
	write(t, filepath.Join(dir, ".gitattributes"), "*.txt text\n")
	write(t, filepath.Join(dir, ".gitmodules"), "[submodule \"m\"]\n\tpath = m\n\turl = ./m\n")
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	commitAll(t, dir, "init")
	for _, f := range []string{".gitattributes", ".gitmodules"} {
		h := gitOut(t, dir, "", "rev-parse", "HEAD:"+f)
		if err := os.Remove(filepath.Join(dir, ".git", "objects", h[:2], h[2:])); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "update-index", "--skip-worktree", ".gitattributes", ".gitmodules")
	for _, f := range []string{".gitattributes", ".gitmodules"} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil {
			t.Fatal(err)
		}
	}
	upload, marker := markerProgram(t, t.TempDir(), "upload-pack.sh")
	git(t, dir, "config", "extensions.partialClone", "origin")
	git(t, dir, "config", "remote.origin.url", "file:///nonexistent")
	git(t, dir, "config", "remote.origin.promisor", "true")
	git(t, dir, "config", "remote.origin.uploadpack", upload)

	racy(t, dir, "a.txt")
	gitRun(dir, []string{"GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=0"}, statusArgs...)
	if !exists(marker) {
		t.Skip("control: the git status of Changed did not lazy-fetch here, the test proves nothing")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	racy(t, dir, "a.txt")
	_, err := Changed(t.Context(), dir) // may fail closed; it must not run the transport
	assertNoMarker(t, marker, "Changed (lazy fetch)")
	// A git that may ignore GIT_NO_LAZY_FETCH must not get to git status.
	if v, ok, _ := version(context.Background(), dir); !lazyFetchSafe(v, ok) && !errors.Is(err, ErrPartialClone) {
		t.Fatalf("git %v: err = %v, want ErrPartialClone", v, err)
	}
}

// TestChangedNoFsmonitor: a repo-configured core.fsmonitor names a program
// that git status runs to ask for changed files.
func TestChangedNoFsmonitor(t *testing.T) {
	skipWindows(t)
	dir := initRepo(t)
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	commitAll(t, dir, "init")
	prog, marker := markerProgram(t, t.TempDir(), "fsmonitor.sh")
	git(t, dir, "config", "core.fsmonitor", prog)
	write(t, filepath.Join(dir, "a.txt"), "changed\n")

	got, err := Changed(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a.txt"}) {
		t.Fatalf("got %v", got)
	}
	assertNoMarker(t, marker, "Changed")

	gitRun(dir, []string{"GIT_OPTIONAL_LOCKS=0"}, statusArgs...)
	control(t, marker, "the git status of Changed without core.fsmonitor=false")
}

// TestChangedNoSystemConfig: run itself sets GIT_CONFIG_NOSYSTEM=1, so a
// filter driver in the system config is neither trusted nor read, whatever
// the environment of paraplane. Read, it would show up in the system scope
// and refuse.
func TestChangedNoSystemConfig(t *testing.T) {
	dir := filterRepo(t)
	sys := filepath.Join(t.TempDir(), "gitconfig")
	write(t, sys, "[filter \"x\"]\n\tclean = touch PWNED\n")
	t.Setenv("GIT_CONFIG_SYSTEM", sys) // git 2.32+
	t.Setenv("GIT_CONFIG_NOSYSTEM", "")
	os.Unsetenv("GIT_CONFIG_NOSYSTEM") // TestMain sets it; t.Setenv restores it

	cmd := exec.Command("git", "-C", dir, "config", "--system", "--get", "filter.x.clean")
	if out, err := cmd.Output(); err != nil || len(out) == 0 {
		t.Skipf("control: git does not read GIT_CONFIG_SYSTEM here (%v), the test proves nothing", err)
	}
	write(t, filepath.Join(dir, "a.txt"), "changed\n")
	got, err := Changed(t.Context(), dir)
	if err != nil {
		t.Fatalf("Changed read the system config: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"a.txt"}) {
		t.Fatalf("got %v", got)
	}
	assertNoMarker(t, filepath.Join(dir, "PWNED"), "Changed")
}
