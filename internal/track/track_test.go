package track

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"paraplane/internal/gitx"
	"paraplane/internal/plan"
	"paraplane/internal/state"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, file, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", msg)
}

// repo has a plan of W1 (T1, T2) and W2 (T3) and one plain commit.
func repo(t *testing.T) (string, *plan.Plan) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	commit(t, dir, "init.txt", "init")
	p := &plan.Plan{Waves: []plan.Wave{
		{ID: "W1", Tasks: []plan.Task{{ID: "T1"}, {ID: "T2"}}},
		{ID: "W2", Tasks: []plan.Task{{ID: "T3"}}},
	}}
	return dir, p
}

func record(t *testing.T, dir, task string) {
	t.Helper()
	g, err := gitx.GitDir(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	head, _ := gitx.Head(t.Context(), dir)
	if err := state.Update(t.Context(), g, func(s *state.State) {
		s.Entries = append(s.Entries, state.Entry{Plan: "plan.md", PlanHash: hashA, Task: task, Head: head})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPlanStatusCommitAndState(t *testing.T) {
	dir, p := repo(t)
	commit(t, dir, "a.txt", "t1\n\nParaplane-Plan: plan.md\nParaplane-Task: T1\n")
	record(t, dir, "T2")
	st, err := PlanStatus(t.Context(), dir, "plan.md", p, hashA, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Waves != 1 || st.Tasks != 2 || !strings.Contains(st.Text, "✓ done") {
		t.Errorf("%+v", st)
	}
	st, _ = PlanStatus(t.Context(), dir, "plan.md", p, hashA, []string{"T3"})
	if st.Waves != 2 || st.Tasks != 3 {
		t.Errorf("extra: %+v", st)
	}
}

func TestPlanStatusReset(t *testing.T) {
	dir, p := repo(t)
	commit(t, dir, "b.txt", "work")
	record(t, dir, "T1")
	git(t, dir, "reset", "-q", "--hard", "HEAD~1")
	st, err := PlanStatus(t.Context(), dir, "plan.md", p, hashA, nil)
	if err != nil || st.Tasks != 0 {
		t.Errorf("after reset: %+v %v", st, err)
	}
}

func TestPlanStatusLostTask(t *testing.T) {
	dir, p := repo(t)
	commit(t, dir, "c.txt", "t1\n\nParaplane-Plan: plan.md\nParaplane-Task: T1\n\nCo-Authored-By: A <a@b>\n")
	st, err := PlanStatus(t.Context(), dir, "plan.md", p, hashA, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`\nwarning: task T1 commit [0-9a-f]{7,}: trailers not in the last paragraph\n$`).MatchString(st.Text) || st.Tasks != 0 {
		t.Errorf("%q %+v", st.Text, st)
	}
}
