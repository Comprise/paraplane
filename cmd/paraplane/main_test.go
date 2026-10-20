package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"paraplane/internal/hooks"
)

// planMD wraps a ```paraplane block body into a Markdown plan.
func planMD(block string) string { return "# Plan\n\n```paraplane\n" + block + "```\n" }

// validPlan has two waves; T2 depends on T1, so no shorter layout exists.
var validPlan = planMD(`gate: go test ./...
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a.go], verify: go test}
  - id: W2
    tasks:
      - {id: T2, title: b, owns: [b.go, sub/**], depends: [T1], verify: go test}
`)

// shorterPlan has two independent waves: W3 suggests a single wave.
var shorterPlan = planMD(`gate: go test ./...
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a.go], verify: go test}
  - id: W2
    tasks:
      - {id: T2, title: b, owns: [b.go], verify: go test}
`)

// invalidPlan has E2: both tasks of W1 own a.go.
var invalidPlan = planMD(`gate: go test ./...
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a.go], verify: go test}
      - {id: T2, title: b, owns: [a.go], verify: go test}
`)

func write(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// cli runs the binary in-process in cwd.
func cli(t *testing.T, cwd, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var o, e bytes.Buffer
	code = run(args, strings.NewReader(stdin), &o, &e, cwd)
	return code, o.String(), e.String()
}

// project makes a project root (marked by .paraplane/config.yaml) with no git.
func project(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, ".paraplane/config.yaml", "mode: block\n")
	return dir
}

// notRepo is project(t) that git cannot find a repository above: the
// ceiling stops the search at the temp dir's parent.
func notRepo(t *testing.T) string {
	t.Helper()
	dir := project(t)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// repo makes a git repository with the plan committed at docs/plan.md.
func repo(t *testing.T, planText string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	write(t, dir, "docs/plan.md", planText)
	write(t, dir, "README", "x\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func TestCheckCLI(t *testing.T) {
	dir := project(t)
	write(t, dir, "ok.md", validPlan)
	write(t, dir, "bad.md", invalidPlan)
	write(t, dir, "short.md", shorterPlan)

	code, out, _ := cli(t, dir, "", "check", "ok.md")
	if code != 0 || out != "plan ok: 2 waves, 2 tasks\n" {
		t.Errorf("valid: code %d, out %q", code, out)
	}

	code, out, _ = cli(t, dir, "", "check", "bad.md")
	if code != 1 || !strings.Contains(out, "error E2 W1: ") || !strings.HasSuffix(out, "plan invalid: 1 error(s), 0 warning(s)\n") {
		t.Errorf("invalid: code %d, out %q", code, out)
	}

	for _, args := range [][]string{{"check"}, {"check", "a", "b"}, {"check", "--bogus", "ok.md"}, {}, {"nope"}} {
		if code, _, errOut := cli(t, dir, "", args...); code != 2 || !strings.Contains(errOut, "usage:") {
			t.Errorf("%v: code %d, stderr %q", args, code, errOut)
		}
	}

	code, out, _ = cli(t, dir, validPlan, "check", "-")
	if code != 0 || out != "plan ok: 2 waves, 2 tasks\n" {
		t.Errorf("stdin: code %d, out %q", code, out)
	}
	code, out, _ = cli(t, dir, strings.Repeat("x", 1<<20+1), "check", "-")
	if code != 1 || !strings.Contains(out, "error: ") {
		t.Errorf("big stdin: code %d, out %q", code, out)
	}

	// Without --suggest the warning is listed but no layout.
	code, out, _ = cli(t, dir, "", "check", "short.md")
	if code != 0 || !strings.Contains(out, "warning W3") || strings.Contains(out, "Shorter layout:") {
		t.Errorf("W3: code %d, out %q", code, out)
	}
	code, out, _ = cli(t, dir, "", "check", "--suggest", "short.md")
	if code != 0 || !strings.Contains(out, "Shorter layout:\n```paraplane\n") ||
		!strings.HasSuffix(out, "```\nplan ok with 1 warning(s): 2 waves, 2 tasks\n") {
		t.Errorf("--suggest: code %d, out %q", code, out)
	}
	if i, j := strings.Index(out, "warning W3"), strings.Index(out, "Shorter layout:"); i < 0 || i > j {
		t.Errorf("--suggest: findings must come before the layout: %q", out)
	}

	// A file name with control characters is sanitized in the error.
	code, out, _ = cli(t, dir, "", "check", "no\x1b[31m.md")
	if code != 1 || !strings.HasPrefix(out, "error: ") || strings.Contains(out, "\x1b") {
		t.Errorf("missing: code %d, out %q", code, out)
	}

	// A symlink, a directory, a file outside the root and a big file are refused.
	outside := t.TempDir()
	write(t, outside, "o.md", validPlan)
	refused := []string{"d.md", filepath.Join(outside, "o.md"), "big.md"}
	// Windows without developer mode cannot make symlinks; the case is skipped there.
	if err := os.Symlink(filepath.Join(dir, "ok.md"), filepath.Join(dir, "link.md")); err == nil {
		refused = append(refused, "link.md")
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "d.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "big.md", validPlan+strings.Repeat("x", 1<<20))
	for _, name := range refused {
		code, out, _ := cli(t, dir, "", "check", name)
		if code != 1 || !strings.HasPrefix(out, "error: ") {
			t.Errorf("%s: code %d, out %q", name, code, out)
		}
	}

	// Config problems come first.
	write(t, dir, ".paraplane/config.yaml", "mode: loud\n")
	code, out, _ = cli(t, dir, "", "check", "ok.md")
	if code != 0 || !strings.HasPrefix(out, "warning: config: ") || !strings.HasSuffix(out, "plan ok: 2 waves, 2 tasks\n") {
		t.Errorf("config: code %d, out %q", code, out)
	}

	// From a subdirectory the path is relative to the current directory.
	sub := filepath.Join(dir, "sub")
	write(t, sub, "x", "")
	if code, out, _ := cli(t, sub, "", "check", "../ok.md"); code != 0 {
		t.Errorf("subdir: code %d, out %q", code, out)
	}
}

func TestAuditCLI(t *testing.T) {
	dir := repo(t, validPlan)

	write(t, dir, "b.go", "package b\n")
	write(t, dir, "sub/c.go", "package c\n")
	write(t, dir, "docs/plan.md", validPlan+"\n- [x] done\n") // the plan file itself is allowed
	code, out, _ := cli(t, dir, "", "audit", "docs/plan.md", "W2")
	if code != 0 || out != "audit ok: W2, 3 changed file(s)\n" {
		t.Errorf("ok: code %d, out %q", code, out)
	}

	// From a subdirectory the plan path is taken to the root.
	code, out, _ = cli(t, filepath.Join(dir, "docs"), "", "audit", "plan.md", "W2")
	if code != 0 || out != "audit ok: W2, 3 changed file(s)\n" {
		t.Errorf("subdir: code %d, out %q", code, out)
	}

	write(t, dir, "a.go", "package a\n")
	code, out, _ = cli(t, dir, "", "audit", "docs/plan.md", "W2")
	want := "error A1 W2: a.go changed but no task of W2 owns it\naudit failed: 1 file(s) outside W2\n"
	if code != 1 || out != want {
		t.Errorf("outside: code %d, out %q, want %q", code, out, want)
	}

	code, out, _ = cli(t, dir, "", "audit", "docs/plan.md", "W9")
	if code != 1 || !strings.HasPrefix(out, "error: ") {
		t.Errorf("unknown wave: code %d, out %q", code, out)
	}

	write(t, dir, "docs/bad.md", invalidPlan)
	code, out, _ = cli(t, dir, "", "audit", "docs/bad.md", "W1")
	if code != 1 || !strings.HasPrefix(out, "error: ") {
		t.Errorf("invalid plan: code %d, out %q", code, out)
	}

	for _, args := range [][]string{{"audit"}, {"audit", "docs/plan.md"}, {"audit", "a", "b", "c"}} {
		if code, _, _ := cli(t, dir, "", args...); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
	}

	nr := notRepo(t)
	write(t, nr, "plan.md", validPlan)
	code, out, _ = cli(t, nr, "", "audit", "plan.md", "W1")
	if code != 1 || !strings.HasPrefix(out, "error: ") {
		t.Errorf("not a repo: code %d, out %q", code, out)
	}
}

func TestProgressCLI(t *testing.T) {
	dir := repo(t, validPlan)

	code, out, _ := cli(t, dir, "", "progress", "docs/plan.md")
	if code != 0 || !strings.Contains(out, "▶ running") || strings.Contains(out, "✓ done") {
		t.Errorf("no wave commits: code %d, out %q", code, out)
	}

	write(t, dir, "a.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "feat: a\n\nParaplane-Plan: docs/plan.md\nParaplane-Wave: W1")

	code, out, _ = cli(t, dir, "", "progress", "docs/plan.md")
	if code != 0 || !strings.HasPrefix(out, "paraplane · plan.md") {
		t.Fatalf("code %d, out %q", code, out)
	}
	var w1, w2 string
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "W1 "):
			w1 = l
		case strings.HasPrefix(l, "W2 "):
			w2 = l
		}
	}
	if !strings.Contains(w1, "✓ done") || !strings.Contains(w2, "▶ running") {
		t.Errorf("W1 %q, W2 %q", w1, w2)
	}

	// From a subdirectory the trailer path is still the one from the root.
	if code, out, _ := cli(t, filepath.Join(dir, "docs"), "", "progress", "plan.md"); code != 0 || !strings.Contains(out, "✓ done") {
		t.Errorf("subdir: code %d, out %q", code, out)
	}

	write(t, dir, "docs/bad.md", planMD("waves: [\n"))
	if code, out, _ := cli(t, dir, "", "progress", "docs/bad.md"); code != 1 || !strings.HasPrefix(out, "error: ") {
		t.Errorf("E plan: code %d, out %q", code, out)
	}
	write(t, dir, "docs/bad2.md", invalidPlan)
	if code, out, _ := cli(t, dir, "", "progress", "docs/bad2.md"); code != 1 || !strings.HasPrefix(out, "error: ") {
		t.Errorf("E2 plan: code %d, out %q", code, out)
	}

	if code, _, _ := cli(t, dir, "", "progress"); code != 2 {
		t.Errorf("no args: code %d", code)
	}

	nr := notRepo(t)
	write(t, nr, "plan.md", validPlan)
	if code, out, _ := cli(t, nr, "", "progress", "plan.md"); code != 1 || !strings.HasPrefix(out, "error: ") {
		t.Errorf("not a repo: code %d, out %q", code, out)
	}
}

func TestHookNeverFails(t *testing.T) {
	dir := project(t)
	t.Setenv("CLAUDE_PROJECT_DIR", dir)

	code, out, errOut := cli(t, dir, `{"hook_event_name":"SessionStart"}`, "hook", "session-start")
	if code != 0 || !strings.Contains(out, "paraplane") || errOut != "" {
		t.Errorf("session-start: code %d, out %q, stderr %q", code, out, errOut)
	}

	big := `{"cwd":"` + strings.Repeat("x", 4<<20) + `"}`
	cases := map[string]struct {
		stdin string
		args  []string
	}{
		"bad json":      {"{", []string{"hook", "session-start"}},
		"too big":       {big, []string{"hook", "session-start"}},
		"unknown event": {"{}", []string{"hook", "nope"}},
		"no event":      {"{}", []string{"hook"}},
		"extra args":    {"{}", []string{"hook", "session-start", "x"}},
		"panic":         {"{}", []string{"hook", "boom"}},
	}
	hookHandlers["boom"] = func(hooks.Env, hooks.Input) ([]byte, error) { panic("boom") }
	defer delete(hookHandlers, "boom")
	for name, c := range cases {
		code, out, errOut := cli(t, dir, c.stdin, c.args...)
		if code != 0 || out != "" || !strings.Contains(errOut, "paraplane: hook") {
			t.Errorf("%s: code %d, out %q, stderr %q", name, code, out, errOut)
		}
	}

	// A handler error is reported and the answer stays empty.
	hookHandlers["fail"] = func(hooks.Env, hooks.Input) ([]byte, error) { return []byte("x"), errors.New("bad") }
	defer delete(hookHandlers, "fail")
	if code, out, errOut := cli(t, dir, "{}", "hook", "fail"); code != 0 || out != "" || !strings.Contains(errOut, "bad") {
		t.Errorf("error: code %d, out %q, stderr %q", code, out, errOut)
	}

	// An unreadable cwd still exits 0 for a hook.
	var o, e bytes.Buffer
	if code := start([]string{"hook", "session-start"}, strings.NewReader("{}"), &o, &e,
		func() (string, error) { return "", errors.New("gone") }); code != 0 {
		t.Errorf("no cwd: code %d", code)
	}
}

func TestHookRoot(t *testing.T) {
	dir := project(t)
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	if got := hookRoot(hooks.Input{Cwd: sub}, other); got != dir {
		t.Errorf("payload cwd: %q, want %q", got, dir)
	}
	if got := hookRoot(hooks.Input{}, sub); got != dir {
		t.Errorf("process cwd: %q, want %q", got, dir)
	}
	t.Setenv("CLAUDE_PROJECT_DIR", sub)
	if got := hookRoot(hooks.Input{Cwd: other}, other); got != dir {
		t.Errorf("project dir: %q, want %q", got, dir)
	}
}

func TestHooksJSONContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
				If      string `json:"if"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"SessionStart": "session-start", "PreToolUse": "pre-tool-use", "PostToolUse": "post-tool-use",
	}
	if len(cfg.Hooks) != len(want) {
		t.Errorf("events = %d, want %d", len(cfg.Hooks), len(want))
	}
	used := map[string]bool{}
	for event, groups := range cfg.Hooks {
		for _, g := range groups {
			if len(g.Hooks) == 0 {
				t.Errorf("%s: empty hooks list", event)
			}
			for _, h := range g.Hooks {
				const prefix = `"${CLAUDE_PLUGIN_ROOT}"/bin/paraplane hook `
				sub, ok := strings.CutPrefix(h.Command, prefix)
				if !ok || sub != want[event] {
					t.Errorf("%s: command %q, want %q", event, h.Command, prefix+want[event])
				}
				if _, ok := hookHandlers[sub]; !ok {
					t.Errorf("%s: binary has no handler for %q", event, sub)
				}
				used[sub] = true
			}
		}
	}
	for sub := range hookHandlers {
		if !used[sub] && !codexOnly[sub] {
			t.Errorf("handler %q is not used by hooks.json", sub)
		}
		if used[sub] && codexOnly[sub] {
			t.Errorf("Codex-only handler %q is used by hooks.json", sub)
		}
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := cli(t, t.TempDir(), "", "version")
	if code != 0 || out != version+"\n" {
		t.Errorf("code %d, out %q", code, out)
	}
	if code, _, _ := cli(t, t.TempDir(), "", "version", "x"); code != 2 {
		t.Errorf("extra arg: code %d", code)
	}
}

// claudeHooks is hooks/hooks.json as shipped before Codex support: Codex
// support must not change it.
const claudeHooks = `{
  "hooks": {
    "SessionStart": [{ "hooks": [{ "type": "command", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/bin/paraplane hook session-start", "timeout": 10 }] }],
    "PreToolUse": [
      { "matcher": "ExitPlanMode|Skill", "hooks": [{ "type": "command", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/bin/paraplane hook pre-tool-use", "timeout": 10 }] },
      { "matcher": "Bash", "hooks": [{ "type": "command", "if": "Bash(paraplane *)", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/bin/paraplane hook pre-tool-use", "timeout": 10 }] }
    ],
    "PostToolUse": [{ "matcher": "Write|Edit", "hooks": [{ "type": "command", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/bin/paraplane hook post-tool-use", "timeout": 10 }] }]
  }
}
`

// codexOnly are the handlers only hooks/codex.json uses.
var codexOnly = map[string]bool{"stop": true, "prompt-submit": true, "permission-request": true}

func TestClaudeHooksUnchanged(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.ReplaceAll(string(data), "\r\n", "\n"); got != claudeHooks {
		t.Errorf("hooks/hooks.json changed:\n%s", data)
	}
}

func TestCodexHooksManifest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "hooks", "codex.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Matcher *string `json:"matcher"`
			Hooks   []struct {
				Type    string  `json:"type"`
				Command string  `json:"command"`
				Timeout int     `json:"timeout"`
				If      *string `json:"if"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{ sub, matcher string }{
		"SessionStart":      {"session-start", ""},
		"UserPromptSubmit":  {"prompt-submit", ""},
		"Stop":              {"stop", ""},
		"PermissionRequest": {"permission-request", "^Bash$"},
		"PostToolUse":       {"post-tool-use", "^(Bash|apply_patch)$"},
	}
	if len(cfg.Hooks) != len(want) {
		t.Errorf("events = %d, want %d", len(cfg.Hooks), len(want))
	}
	used := map[string]bool{}
	for event, groups := range cfg.Hooks {
		w, known := want[event]
		if !known {
			t.Errorf("unexpected event %s", event)
			continue
		}
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Errorf("%s: want one group with one hook", event)
			continue
		}
		g, h := groups[0], groups[0].Hooks[0]
		wantCmd := `"${CLAUDE_PLUGIN_ROOT}"/bin/paraplane hook ` + w.sub + " --agent codex"
		if h.Type != "command" || h.Command != wantCmd {
			t.Errorf("%s: command %q, want %q", event, h.Command, wantCmd)
		}
		if _, ok := hookHandlers[w.sub]; !ok {
			t.Errorf("%s: binary has no handler for %q", event, w.sub)
		}
		used[w.sub] = true
		if h.Timeout != 10 {
			t.Errorf("%s: timeout %d, want 10", event, h.Timeout)
		}
		if h.If != nil {
			t.Errorf("%s: Codex has no if field", event)
		}
		switch {
		case w.matcher == "" && g.Matcher != nil:
			t.Errorf("%s: matcher %q, want none", event, *g.Matcher)
		case w.matcher != "" && (g.Matcher == nil || *g.Matcher != w.matcher):
			t.Errorf("%s: matcher %v, want %q", event, g.Matcher, w.matcher)
		}
	}
	for sub := range codexOnly {
		if !used[sub] {
			t.Errorf("Codex-only handler %q is not used by codex.json", sub)
		}
	}
	if len(hookHandlers) != len(codexOnly)+3 {
		t.Errorf("hookHandlers has %d entries; update the hooks.json and codex.json contracts", len(hookHandlers))
	}
}

// codexHook runs `paraplane hook <event> --agent codex` with a payload.
func codexHook(t *testing.T, cwd, event string, payload map[string]any) (int, string, string) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return cli(t, cwd, string(raw), "hook", event, "--agent", "codex")
}

func TestHookAgentFlag(t *testing.T) {
	dir := project(t)
	t.Setenv("CLAUDE_PROJECT_DIR", "") // Codex does not set it
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	transcript, err := filepath.Abs(filepath.Join("..", "..", "internal", "hooks", "testdata", "codex_transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	stop := map[string]any{
		"session_id": "s1", "turn_id": "01a15914-2abb-7442-952c-4e7261ae2507", "transcript_path": transcript,
		"cwd": dir, "hook_event_name": "Stop", "permission_mode": "default", "stop_hook_active": false,
		"last_assistant_message": nil,
	}
	code, out, errOut := codexHook(t, t.TempDir(), "stop", stop)
	if code != 0 || !strings.HasPrefix(out, `{"decision":"block","reason":"paraplane: the proposed plan`) ||
		!strings.Contains(out, "skill $paraplane:rules.") || errOut != "" {
		t.Errorf("stop: code %d, out %q, stderr %q", code, out, errOut)
	}
	raw, _ := json.Marshal(stop)
	for _, args := range [][]string{{"hook", "stop"}, {"hook", "stop", "--agent", "claude"}} {
		if code, out, _ := cli(t, dir, string(raw), args...); code != 0 || out != "" {
			t.Errorf("%v: code %d, out %q; Stop is Codex-only", args, code, out)
		}
	}

	code, out, _ = codexHook(t, dir, "session-start", map[string]any{"cwd": dir})
	if code != 0 || !strings.Contains(out, "skill $paraplane:rules") {
		t.Errorf("codex session-start: code %d, out %q", code, out)
	}
	code, out, _ = cli(t, dir, `{"cwd":`+strconvQuote(dir)+`}`, "hook", "session-start")
	if code != 0 || !strings.Contains(out, "skill paraplane:rules") || strings.Contains(out, "$") {
		t.Errorf("claude session-start: code %d, out %q", code, out)
	}

	write(t, dir, "docs/plans/x.md", invalidPlan)
	post := map[string]any{"cwd": dir, "hook_event_name": "PostToolUse", "tool_name": "Bash",
		"tool_input": map[string]any{"command": "printf x > docs/plans/x.md"}}
	code, out, _ = codexHook(t, dir, "post-tool-use", post)
	if code != 0 || !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "error E2 W1") {
		t.Errorf("codex post-tool-use: code %d, out %q", code, out)
	}
	raw, _ = json.Marshal(post)
	if code, out, _ := cli(t, dir, string(raw), "hook", "post-tool-use"); code != 0 || out != "" {
		t.Errorf("claude post-tool-use Bash: code %d, out %q", code, out)
	}

	code, out, _ = codexHook(t, dir, "prompt-submit", map[string]any{"cwd": dir, "prompt": "$superpowers:executing-plans"})
	if code != 0 || !strings.Contains(out, `"hookEventName":"UserPromptSubmit"`) || !strings.Contains(out, "paraplane execution rules") {
		t.Errorf("prompt-submit: code %d, out %q", code, out)
	}

	t.Setenv("CLAUDE_PLUGIN_ROOT", "/opt/p")
	code, out, _ = codexHook(t, dir, "permission-request", map[string]any{"cwd": dir, "tool_name": "Bash",
		"tool_input": map[string]any{"command": "/opt/p/bin/paraplane check docs/plans/x.md"}})
	if code != 0 || out != `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}`+"\n" {
		t.Errorf("permission-request: code %d, out %q", code, out)
	}
	// The bare name is never approved under Codex.
	code, out, _ = codexHook(t, dir, "permission-request", map[string]any{"cwd": dir, "tool_name": "Bash",
		"tool_input": map[string]any{"command": "paraplane check docs/plans/x.md"}})
	if code != 0 || out != "" {
		t.Errorf("bare permission-request: code %d, out %q", code, out)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestHookAgentFlagBad(t *testing.T) {
	dir := project(t)
	t.Setenv("CLAUDE_PROJECT_DIR", dir)
	for _, args := range [][]string{
		{"hook", "session-start", "--agent"},
		{"hook", "session-start", "--agent", "gpt"},
		{"hook", "session-start", "--agent", "Codex"},
		{"hook", "session-start", "--x", "codex"},
		{"hook", "--agent", "codex", "session-start"},
		{"hook", "session-start", "--agent", "codex", "x"},
		{"hook", "session-start", "--agent=codex"},
	} {
		code, out, errOut := cli(t, dir, `{}`, args...)
		if code != 0 || out != "" || !strings.Contains(errOut, "paraplane: hook: bad arguments") {
			t.Errorf("%v: code %d, out %q, stderr %q", args, code, out, errOut)
		}
	}
}

func TestAuditFilterDriver(t *testing.T) {
	dir := repo(t, validPlan)
	write(t, dir, ".gitattributes", "* filter=x\n")
	git(t, dir, "config", "filter.x.clean", "touch PWNED")
	write(t, dir, "README", "changed\n") // tracked and modified: git status would run the clean filter
	code, out, _ := cli(t, dir, "", "audit", "docs/plan.md", "W1")
	if code != 1 || !strings.HasPrefix(out, "error: ") || !strings.Contains(out, "filter drivers") {
		t.Errorf("code %d, out %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "PWNED")); err == nil {
		t.Error("filter driver ran")
	}
}

func TestCheckFIFO(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no FIFOs on Windows")
	}
	dir := project(t)
	if out, err := exec.Command("mkfifo", filepath.Join(dir, "fifo.md")).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v %s", err, out)
	}
	type res struct {
		code int
		out  string
	}
	ch := make(chan res, 1)
	go func() {
		var o, e bytes.Buffer
		code := run([]string{"check", "fifo.md"}, strings.NewReader(""), &o, &e, dir)
		ch <- res{code, o.String()}
	}()
	select {
	case r := <-ch:
		if r.code != 1 || !strings.HasPrefix(r.out, "error: ") {
			t.Errorf("code %d, out %q", r.code, r.out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("check blocked on a FIFO")
	}
}

// bigShorterPlan has n independent one-task waves: W3 suggests one wave whose
// layout is far larger than 64 KB.
func bigShorterPlan(n int) string {
	title := strings.Repeat("long title ", 15)
	var b strings.Builder
	b.WriteString("gate: go test ./...\nwaves:\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "  - id: W%d\n    tasks:\n      - {id: T%d, title: %s, owns: [internal/some/package/pkg%d/file.go], verify: go test}\n", i, i, title, i)
	}
	return planMD(b.String())
}

func TestCheckSuggestFullLayout(t *testing.T) {
	dir := project(t)
	md := bigShorterPlan(500)
	if len(md) < 100<<10 || len(md) > 1<<20 {
		t.Fatalf("plan is %d bytes", len(md))
	}
	write(t, dir, "big.md", md)
	code, out, _ := cli(t, dir, "", "check", "--suggest", "big.md")
	i := strings.Index(out, "Shorter layout:\n")
	if code != 0 || i < 0 {
		t.Fatalf("code %d, out %.200q", code, out)
	}
	block := out[i:]
	if len(block) < 70<<10 {
		t.Fatalf("layout is only %d bytes", len(block))
	}
	if !strings.Contains(block, "T500") || strings.Contains(block, "…") {
		t.Error("layout is cut")
	}
	if !strings.HasSuffix(block, "```\nplan ok with 1 warning(s): 500 waves, 500 tasks\n") {
		t.Errorf("block does not end with a closed fence and the summary: %q", block[len(block)-120:])
	}
}

func TestStartNoCwd(t *testing.T) {
	gone := func() (string, error) { return "", errors.New("gone") }
	var o, e bytes.Buffer
	if code := start([]string{"version"}, strings.NewReader(""), &o, &e, gone); code != 1 ||
		!strings.HasPrefix(o.String(), "error: gone") || e.Len() != 0 {
		t.Errorf("non-hook: code %d, stdout %q, stderr %q", code, o.String(), e.String())
	}
	// A hook falls back to CLAUDE_PROJECT_DIR for the root.
	dir := project(t)
	t.Setenv("CLAUDE_PROJECT_DIR", dir)
	o.Reset()
	e.Reset()
	if code := start([]string{"hook", "session-start"}, strings.NewReader(`{"hook_event_name":"SessionStart"}`), &o, &e, gone); code != 0 ||
		!strings.Contains(o.String(), "paraplane") || e.Len() != 0 {
		t.Errorf("hook: code %d, stdout %q, stderr %q", code, o.String(), e.String())
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom read") }

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("boom write") }

func TestHookIOErrors(t *testing.T) {
	dir := project(t)
	t.Setenv("CLAUDE_PROJECT_DIR", dir)
	var o, e bytes.Buffer
	if code := run([]string{"hook", "session-start"}, errReader{}, &o, &e, dir); code != 0 || o.Len() != 0 ||
		!strings.Contains(e.String(), "bad input") || !strings.Contains(e.String(), "boom read") {
		t.Errorf("stdin error: code %d, stdout %q, stderr %q", code, o.String(), e.String())
	}
	e.Reset()
	if code := run([]string{"hook", "session-start"}, strings.NewReader(`{"hook_event_name":"SessionStart"}`), errWriter{}, &e, dir); code != 0 ||
		!strings.Contains(e.String(), "writing the answer") || !strings.Contains(e.String(), "boom write") {
		t.Errorf("stdout error: code %d, stderr %q", code, e.String())
	}
}

var _ io.Reader = errReader{}
