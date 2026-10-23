// Package testgit holds git helpers for tests: an isolated environment, a
// throwaway repository, a fake old git and a portable marker program. It is
// imported only from _test.go files.
package testgit

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const markerVar = "TESTGIT_MARKER"

// A test binary that imports this package becomes the marker program when
// TESTGIT_MARKER is set: it creates the file and exits before any test runs.
func init() {
	if p := os.Getenv(markerVar); p != "" {
		if err := os.WriteFile(p, []byte("ran"), 0o600); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
}

// Env is the environment for git commands run by tests: the current one
// without any GIT_* variable, without global or system config, with a fixed
// author and committer.
func Env() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
}

// Isolate removes every GIT_* variable from the test's process environment
// (restored when the test ends) and turns off global and system config.
func Isolate(t testing.TB) {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "GIT_") {
			t.Setenv(k, "")
			os.Unsetenv(k)
		}
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
}

// Run runs git in dir under Env and returns its trimmed stdout; a failure
// fails the test.
func Run(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = Env()
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("git %v: %v\n%s", args, err, stderr)
	}
	return strings.TrimSpace(string(out))
}

// Init creates an empty repository in a fresh temporary directory.
func Init(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	Run(t, dir, "init", "-q", "-b", "main")
	return dir
}

// Commit writes file (containing msg) in dir and commits it with subject msg.
func Commit(t testing.TB, dir, file, msg string) {
	t.Helper()
	p := filepath.Join(dir, file)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(msg+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	Run(t, dir, "add", "--", file)
	Run(t, dir, "commit", "-q", "-m", msg)
}

// FakeOldGit returns a directory with a git wrapper to put first on PATH: it
// reports version 2.34.1 and drops GIT_NO_LAZY_FETCH, as a git that does not
// know the variable would. The wrapper is a POSIX shell script; on Windows
// the test is skipped.
func FakeOldGit(t testing.TB) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake old git is a POSIX shell script")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"unset GIT_NO_LAZY_FETCH\n" +
		"case \"$1\" in version|--version) echo 'git version 2.34.1'; exit 0;; esac\n" +
		"exec '" + strings.ReplaceAll(real, "'", `'\''`) + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// MarkerProgram returns a shell command line that creates the marker file
// when run, by re-executing the test binary, and the marker path. The command carries
// its own environment variable, so it needs nothing from the caller's env. It works
// wherever the tests run, Windows included.
func MarkerProgram(t testing.TB) (cmd, marker string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker = filepath.Join(t.TempDir(), "marker")
	exe = filepath.ToSlash(exe)
	marker = filepath.ToSlash(marker)
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	// The variable is part of the command, so it works even where the
	// caller sanitizes the environment before git runs the program.
	return markerVar + "=" + q(marker) + " " + q(exe) + " -test.run=^$", marker
}
