package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// progressPlan: W1 (T1, T2), W2 (T3).
const progressPlan = "# x\n\n```paraplane\ngate: make\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a.go], verify: v}\n      - {id: T2, title: b, owns: [b.go], verify: v}\n  - id: W2\n    tasks:\n      - {id: T3, title: c, owns: [c.go], depends: [T1], verify: v}\n```\n"

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// progressEnv is env with a git repository holding the plan, committed.
func progressEnv(t *testing.T, env Env, mode string) Env {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	env.Config.Progress = mode
	gitRun(t, env.Root, "init", "-q")
	writePlan(t, env, "docs/plans/x.md", progressPlan)
	gitRun(t, env.Root, "add", "-A")
	gitRun(t, env.Root, "commit", "-q", "-m", "plan")
	return env
}

func agentIn(t *testing.T, prompt string) Input {
	t.Helper()
	return postIn(t, "Agent", map[string]any{"prompt": prompt, "description": "task"})
}

func decode(t *testing.T, out []byte) hookOutput {
	t.Helper()
	var v hookOutput
	if out != nil {
		if err := json.Unmarshal(out, &v); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	return v
}

func TestProgressAfterAgentTaskMode(t *testing.T) {
	env := progressEnv(t, testEnv(t), "task")
	in := agentIn(t, "Do T1.\nParaplane-Task: docs/plans/x.md T1\n")
	out, err := PostToolUse(env, in)
	v := decode(t, out)
	if err != nil || !strings.Contains(v.SystemMessage, "1/3 tasks") || v.Hook == nil ||
		v.Hook.AdditionalContext != "paraplane: progress of docs/plans/x.md was shown to the user; do not repeat it." {
		t.Fatalf("got %s, %v", out, err)
	}
	if out, err := PostToolUse(env, in); out != nil || err != nil {
		t.Errorf("repeat: got %s, %v", out, err)
	}
}

func TestProgressAfterAgentWaveMode(t *testing.T) {
	env := progressEnv(t, testEnv(t), "wave")
	if out, err := PostToolUse(env, agentIn(t, "Paraplane-Task: docs/plans/x.md T1")); out != nil || err != nil {
		t.Fatalf("T1: got %s, %v", out, err)
	}
	out, err := PostToolUse(env, agentIn(t, "Paraplane-Task: "+filepath.Join(env.Root, "docs/plans/x.md")+" T2"))
	v := decode(t, out)
	if err != nil || !strings.Contains(v.SystemMessage, "✓ done") || !strings.Contains(v.SystemMessage, "2/3 tasks") {
		t.Fatalf("T2: got %s, %v", out, err)
	}
}

func TestProgressAfterAgentNoMarker(t *testing.T) {
	env := progressEnv(t, testEnv(t), "task")
	for _, prompt := range []string{"Do T1.", "Paraplane-Task: docs/other.md T1", "Paraplane-Task: docs/plans/x.md 1bad"} {
		if out, err := PostToolUse(env, agentIn(t, prompt)); out != nil || err != nil {
			t.Errorf("%q: got %s, %v", prompt, out, err)
		}
	}
	if _, err := os.Stat(filepath.Join(env.Root, ".git", "paraplane", "closed.json")); !os.IsNotExist(err) {
		t.Errorf("state written: %v", err)
	}
}

func TestProgressAfterBashNoTrigger(t *testing.T) {
	env := testEnv(t)
	t.Setenv("PATH", t.TempDir()) // no git: the trigger check runs first
	for _, cmd := range []string{"ls", "echo committed", "make test"} {
		if out, err := PostToolUse(env, postIn(t, "Bash", map[string]any{"command": cmd})); out != nil || err != nil {
			t.Errorf("%q: got %s, %v", cmd, out, err)
		}
	}
}

func TestProgressAfterBashStageCommit(t *testing.T) {
	env := progressEnv(t, testEnv(t), "wave")
	bash := postIn(t, "Bash", map[string]any{"command": "git commit -m stage"})
	nilOut(t)(PostToolUse(env, bash)) // first look: remembers HEAD, the plan commit names no wave
	if err := os.WriteFile(filepath.Join(env.Root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, env.Root, "add", "-A")
	gitRun(t, env.Root, "commit", "-q", "-m", "stage\n\nParaplane-Plan: docs/plans/x.md\nParaplane-Wave: W1\nParaplane-Wave: W2")
	out, err := PostToolUse(env, bash)
	v := decode(t, out)
	if err != nil || strings.Count(v.SystemMessage, "✓ done") != 2 || !strings.Contains(v.SystemMessage, "3/3 tasks") {
		t.Fatalf("got %s, %v", out, err)
	}
	nilOut(t)(PostToolUse(env, bash))
}

func TestProgressCodexContext(t *testing.T) {
	env := progressEnv(t, codexEnv(t), "wave")
	gitRun(t, env.Root, "commit", "-q", "--allow-empty", "-m", "w1\n\nParaplane-Plan: docs/plans/x.md\nParaplane-Wave: W1")
	out, err := PostToolUse(env, bashIn(t, env.Root, "git commit --allow-empty -m w1"))
	v := decode(t, out)
	if err != nil || v.SystemMessage != "" || v.Hook == nil || !strings.Contains(v.Hook.AdditionalContext, "✓ done") ||
		!strings.Contains(v.Hook.AdditionalContext, "Show this progress to the user in a code block.") {
		t.Fatalf("got %s, %v", out, err)
	}
}
