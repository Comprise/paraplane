package hooks

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"paraplane/internal/testgit"
)

// progressPlan: W1 (T1, T2), W2 (T3).
const progressPlan = "# x\n\n```paraplane\ngate: make\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a.go], verify: v}\n      - {id: T2, title: b, owns: [b.go], verify: v}\n  - id: W2\n    tasks:\n      - {id: T3, title: c, owns: [c.go], depends: [T1], verify: v}\n```\n"

// gitRun runs git in dir (testgit.Run).
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	testgit.Run(t, dir, args...)
}

// progressEnv is env with a git repository holding the plan, committed;
// the hooks' own git calls run without the caller's GIT_* variables and
// global config (testgit.Isolate).
func progressEnv(t *testing.T, env Env, mode string) Env {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	testgit.Isolate(t)
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
	if _, err := os.Stat(filepath.Join(env.Root, ".git", "paraplane", "closed.json")); !errors.Is(err, fs.ErrNotExist) {
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

func TestProgressAfterAgentBackground(t *testing.T) {
	env := progressEnv(t, testEnv(t), "task")
	in := postIn(t, "Agent", map[string]any{"prompt": "Paraplane-Task: docs/plans/x.md T1", "run_in_background": true})
	nilOut(t)(PostToolUse(env, in))
	in = postIn(t, "Agent", map[string]any{"prompt": "Paraplane-Task: docs/plans/x.md T1"})
	in.ToolResponse = json.RawMessage(`[{"type":"text","text":"Async agent launched successfully."}]`)
	nilOut(t)(PostToolUse(env, in))
	// Captured from Claude Code: a subagent it put in the background on its
	// own has no run_in_background, and the response is the result object.
	nilOut(t)(PostToolUse(env, codexPayload(t, "claude_post_agent_async", nil)))
	if _, err := os.Stat(filepath.Join(env.Root, ".git", "paraplane", "closed.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a launched background subagent was recorded: %v", err)
	}
}

// transcript writes a subagent transcript whose first record is the prompt.
func transcript(t *testing.T, content any) string {
	t.Helper()
	rec, err := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "agent-x.jsonl")
	if err := os.WriteFile(p, append(rec, []byte("\n{\"type\":\"assistant\"}\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSubagentStopThenStop(t *testing.T) {
	env := progressEnv(t, testEnv(t), "task")
	// Captured from Claude Code: the launch, then SubagentStop twice for the
	// same subagent (the second with stop_hook_active).
	set := map[string]any{"agent_transcript_path": fixtureAbs(t, "claude_agent_transcript.jsonl")}
	nilOut(t)(PostToolUse(env, codexPayload(t, "claude_post_agent_async", nil)))
	// SubagentStop's systemMessage is not shown: record only.
	nilOut(t)(SubagentStop(env, codexPayload(t, "claude_subagent_stop", set)))
	nilOut(t)(SubagentStop(env, codexPayload(t, "claude_subagent_stop_again", set)))
	out, err := Stop(env, Input{HookEventName: "Stop"})
	v := decode(t, out)
	if err != nil || !strings.Contains(v.SystemMessage, "1/3 tasks") || v.Decision != "" {
		t.Fatalf("stop: got %s, %v", out, err)
	}
	nilOut(t)(Stop(env, Input{HookEventName: "Stop"}))

	// Content as a list of text blocks (not seen live; kept as a fallback).
	in := Input{HookEventName: "SubagentStop", AgentTranscriptPath: transcript(t, []map[string]any{{"type": "text", "text": "Paraplane-Task: docs/plans/x.md T2"}})}
	nilOut(t)(SubagentStop(env, in))
	out, _ = Stop(env, Input{HookEventName: "Stop"})
	if v := decode(t, out); !strings.Contains(v.SystemMessage, "2/3 tasks") {
		t.Fatalf("blocks: got %s", out)
	}
}

func TestSubagentStopIgnored(t *testing.T) {
	env := progressEnv(t, testEnv(t), "task")
	for name, path := range map[string]string{
		"no marker": transcript(t, "Summarize the conversation."),
		"missing":   filepath.Join(t.TempDir(), "nope.jsonl"),
		"empty":     "",
	} {
		nilOut(t)(SubagentStop(env, Input{HookEventName: "SubagentStop", AgentTranscriptPath: path}))
		if out, err := Stop(env, Input{HookEventName: "Stop"}); out != nil || err != nil {
			t.Errorf("%s: stop got %s, %v", name, out, err)
		}
	}
}
