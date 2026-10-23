package testgit

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestIsolate(t *testing.T) {
	t.Setenv("GIT_DIR", "/nonexistent")
	t.Setenv("GIT_INDEX_FILE", "/nonexistent/index")
	t.Setenv("GIT_WORK_TREE", "/nonexistent")
	Isolate(t)
	for _, k := range []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Fatalf("%s still set to %q after Isolate", k, v)
		}
	}
	dir := Init(t)
	Run(t, dir, "status")
	Commit(t, dir, "a.txt", "first")
	if got := Run(t, dir, "log", "-1", "--format=%s"); got != "first" {
		t.Fatalf("subject = %q", got)
	}
}

func TestEnv(t *testing.T) {
	t.Setenv("GIT_DIR", "/nonexistent")
	env := strings.Join(Env(), "\n")
	if strings.Contains(env, "GIT_DIR=") {
		t.Fatal("GIT_DIR leaked into Env")
	}
	if !strings.Contains(env, "GIT_CONFIG_NOSYSTEM=1") {
		t.Fatal("system config not disabled")
	}
}

func TestFakeOldGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake git is a POSIX shell script")
	}
	Isolate(t)
	dir := FakeOldGit(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_NO_LAZY_FETCH", "1")
	out, err := exec.Command("git", "version").Output()
	if err != nil || !strings.Contains(string(out), "2.34.1") {
		t.Fatalf("git version = %q, %v", out, err)
	}
	repo := Init(t)
	// The version is reported after global options too, as gitx calls it.
	for _, args := range [][]string{
		{"-C", repo, "-c", "x.y=z", "version"},
		{"-C", repo, "-c", "x.y=z", "--no-pager", "--git-dir=" + repo + "/.git", "version"},
		{"--version"},
		{"-c", "a.b=c", "--version"},
	} {
		out, err := exec.Command("git", args...).Output()
		if err != nil || strings.TrimSpace(string(out)) != "git version 2.34.1" {
			t.Fatalf("git %q = %q, %v", args, out, err)
		}
	}
	// A subcommand argument named version is not the version command.
	if out, err := exec.Command("git", "-C", repo, "config", "--get", "core.bare").Output(); err != nil || strings.Contains(string(out), "2.34.1") {
		t.Fatalf("config through the wrapper: %q, %v", out, err)
	}
	cmd := exec.Command("git", "-C", repo, "-c", "alias.e=!env", "e")
	cmd.Env = append(Env(), "GIT_NO_LAZY_FETCH=1")
	out, err = cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "GIT_NO_LAZY_FETCH") {
		t.Fatal("wrapper did not strip GIT_NO_LAZY_FETCH")
	}
}

func TestMarkerProgram(t *testing.T) {
	cmd, marker := MarkerProgram(t)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("marker exists before the run")
	}
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("sh", "-c", cmd)
	} else {
		c = exec.Command("/bin/sh", "-c", cmd)
	}
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker not created: %v", err)
	}
}

func TestMarkerProgramThroughGit(t *testing.T) {
	Isolate(t)
	cmd, marker := MarkerProgram(t)
	repo := Init(t)
	g := exec.Command("git", "-C", repo, "-c", "alias.x=!"+cmd, "x")
	g.Env = Env() // sanitized: the marker must not depend on the caller's env
	if out, err := g.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("git ran the command but no marker: %v", err)
	}
}
