package state

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Child mode: the test binary re-executed by TestStaleLockOneWinner (as
// internal/testgit.MarkerProgram does) runs one Update and exits before any
// test runs.
const (
	childDir  = "STATE_TEST_CHILD_DIR"
	childTask = "STATE_TEST_CHILD_TASK"
	childGo   = "STATE_TEST_CHILD_GO"
)

func TestMain(m *testing.M) {
	if dir := os.Getenv(childDir); dir != "" {
		os.Exit(runChild(dir, os.Getenv(childTask), os.Getenv(childGo)))
	}
	os.Exit(m.Run())
}

// runChild waits for the go file, then adds its entry under the lock. Inside
// the critical section it holds an O_EXCL marker: a second owner at the same
// time fails to create it.
func runChild(dir, task, goFile string) int {
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(goFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "no go signal")
			return 3
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	inside := filepath.Join(dir, "inside")
	var overlap error
	err := UpdateCtx(ctx, dir, func(s *State) {
		m, err := os.OpenFile(inside, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			overlap = err
			return
		}
		m.Close()
		time.Sleep(100 * time.Millisecond)
		s.Add(Entry{Plan: "p.md", Task: task, Head: hash(1)})
		os.Remove(inside)
	})
	if overlap != nil {
		fmt.Fprintln(os.Stderr, "two owners:", overlap)
		return 4
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		return 5
	}
	return 0
}

// TestStaleLockOneWinner: two processes see the same 12 s old lock at once;
// only one takes it over, the other waits, and no update is lost.
func TestStaleLockOneWinner(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for round := range 3 {
		dir := t.TempDir()
		mkState(t, dir)
		writeFile(t, lockFile(dir), "")
		old := time.Now().Add(-12 * time.Second)
		if err := os.Chtimes(lockFile(dir), old, old); err != nil {
			t.Fatal(err)
		}
		goFile := filepath.Join(t.TempDir(), "go")
		var cmds []*exec.Cmd
		for _, task := range []string{"A", "B"} {
			cmd := exec.Command(exe, "-test.run=^$")
			cmd.Env = append(os.Environ(), childDir+"="+dir, childTask+"="+task, childGo+"="+goFile)
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			cmds = append(cmds, cmd)
		}
		time.Sleep(200 * time.Millisecond) // both children are polling
		writeFile(t, goFile, "")
		for _, cmd := range cmds {
			if err := cmd.Wait(); err != nil {
				t.Errorf("round %d: child: %v", round, err)
			}
		}
		s, err := LoadState(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Entries) != 2 {
			t.Errorf("round %d: entries %v, want both A and B", round, s.Entries)
		}
		if _, err := os.Lstat(lockFile(dir)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("round %d: lock left behind: %v", round, err)
		}
		if left := leftovers(t, dir); len(left) != 0 {
			t.Errorf("round %d: left: %v", round, left)
		}
	}
}
