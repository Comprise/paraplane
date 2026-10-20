package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// planBad has one error (E7) and one warning (W1).
const planBad = "# Plan\n\n- [ ] step 1\n\n```paraplane\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a.go]}\n```\n"

// planOK has no findings.
const planOK = "```paraplane\ngate: make\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a.go], verify: v}\n```\n"

// planW3 has a single warning W3 (a shorter layout exists).
const planW3 = "```paraplane\ngate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a], verify: v}\n  - id: W2\n    tasks:\n      - {id: T2, title: b, owns: [b], verify: v}\n  - id: W3\n    tasks:\n      - {id: T3, title: c, owns: [c], depends: [T1], verify: v}\n```\n"

// planW1 has a single warning W1.
const planW1 = "```paraplane\ngate: make\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a.go]}\n```\n"

func mustNil(t *testing.T, out []byte, err error) {
	t.Helper()
	if err != nil || out != nil {
		t.Fatalf("got %q, %v; want nil, nil", out, err)
	}
}

func exitPlanIn(t *testing.T, fields map[string]any) Input {
	t.Helper()
	return toolInput(t, "ExitPlanMode", fields)
}

// writePlan writes content to rel under env.Root and returns its absolute path.
func writePlan(t *testing.T, env Env, rel, content string) string {
	t.Helper()
	p := filepath.Join(env.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func postIn(t *testing.T, tool string, fields map[string]any) Input {
	t.Helper()
	in := toolInput(t, tool, fields)
	in.HookEventName = "PostToolUse"
	return in
}

func TestExitPlanModeDeny(t *testing.T) {
	out, err := PreToolUse(testEnv(t), exitPlanIn(t, map[string]any{"plan": planBad}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_exit_deny", out)
}

func TestExitPlanModeNoBlockDeny(t *testing.T) {
	out, err := PreToolUse(testEnv(t), exitPlanIn(t, map[string]any{"plan": "# Plan\n\nJust steps.\n"}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_exit_noblock", out)
}

func TestExitPlanModeOK(t *testing.T) {
	out, err := PreToolUse(testEnv(t), exitPlanIn(t, map[string]any{"plan": planOK}))
	mustNil(t, out, err)
}

func TestExitPlanModeWarnings(t *testing.T) {
	out, err := PreToolUse(testEnv(t), exitPlanIn(t, map[string]any{"plan": planW3}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_exit_warnings", out)
}

func TestExitPlanModeWarnMode(t *testing.T) {
	env := testEnv(t)
	env.Config.Mode = "warn"
	out, err := PreToolUse(env, exitPlanIn(t, map[string]any{"plan": planBad}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_exit_warnmode", out)
}

func TestExitPlanModeFromFile(t *testing.T) {
	env := testEnv(t)
	abs := writePlan(t, env, "plans/p.md", planBad)
	out, err := PreToolUse(env, exitPlanIn(t, map[string]any{"plan": "", "planFilePath": abs}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_exit_deny", out)

	t.Run("relative to root", func(t *testing.T) {
		out, err := PreToolUse(env, exitPlanIn(t, map[string]any{"planFilePath": "plans/p.md"}))
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "plan_exit_deny", out)
	})
	t.Run("plan wins over file", func(t *testing.T) {
		out, err := PreToolUse(env, exitPlanIn(t, map[string]any{"plan": planOK, "planFilePath": abs}))
		mustNil(t, out, err)
	})
	t.Run("too large", func(t *testing.T) {
		big := writePlan(t, env, "plans/big.md", planBad+strings.Repeat("x", 1<<20))
		out, err := PreToolUse(env, exitPlanIn(t, map[string]any{"planFilePath": big}))
		mustNil(t, out, err)
	})
	t.Run("directory", func(t *testing.T) {
		out, err := PreToolUse(env, exitPlanIn(t, map[string]any{"planFilePath": filepath.Join(env.Root, "plans")}))
		mustNil(t, out, err)
	})
	t.Run("missing", func(t *testing.T) {
		out, err := PreToolUse(env, exitPlanIn(t, map[string]any{"planFilePath": filepath.Join(env.Root, "nope.md")}))
		mustNil(t, out, err)
	})
}

func TestExitPlanModeNoPlan(t *testing.T) {
	env := testEnv(t)
	nilOut(t)(PreToolUse(env, exitPlanIn(t, map[string]any{})))
	nilOut(t)(PreToolUse(env, exitPlanIn(t, map[string]any{"plan": "", "planFilePath": ""})))
	nilOut(t)(PreToolUse(env, exitPlanIn(t, map[string]any{"plan": 42})))
	nilOut(t)(PreToolUse(env, Input{ToolName: "ExitPlanMode", ToolInput: []byte(`[1`)}))
}

// nilOut returns a checker for a two-value hook call that must give nil, nil.
func nilOut(t *testing.T) func([]byte, error) {
	t.Helper()
	return func(out []byte, err error) {
		t.Helper()
		mustNil(t, out, err)
	}
}

func TestExitPlanModeManyFindings(t *testing.T) {
	var b strings.Builder
	b.WriteString("```paraplane\nwaves:\n")
	for i := range 25 {
		fmt.Fprintf(&b, "  - id: W%d\n    tasks:\n      - {id: T%d, title: a, owns: [f%d], verify: v}\n", i, i, i)
	}
	b.WriteString("```\n")
	out, err := PreToolUse(testEnv(t), exitPlanIn(t, map[string]any{"plan": b.String()}))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if got := strings.Count(s, "error E7 "); got != 20 {
		t.Errorf("listed %d findings, want 20: %s", got, s)
	}
	if !strings.Contains(s, `\n… 5 more\nFormat and rules: skill paraplane:rules."`) {
		t.Errorf("no '… 5 more' line: %s", s)
	}
}

func TestPostToolUseWriteErrors(t *testing.T) {
	env := testEnv(t)
	abs := writePlan(t, env, "docs/plans/a.md", planBad)
	out, err := PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": abs, "content": planBad}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_post_block", out)

	t.Run("warn mode", func(t *testing.T) {
		e := env
		e.Config.Mode = "warn"
		out, err := PostToolUse(e, postIn(t, "Write", map[string]any{"file_path": abs}))
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "plan_post_warnmode", out)
	})
}

func TestPostToolUseWriteNoBlock(t *testing.T) {
	env := testEnv(t)
	abs := writePlan(t, env, "docs/plans/a.md", "# Plan\n\n- [ ] step 1\n")
	out, err := PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": abs}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_post_noblock", out)
}

func TestPostToolUseWriteWarnings(t *testing.T) {
	env := testEnv(t)
	abs := writePlan(t, env, "docs/plans/a.md", planW1)
	out, err := PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": abs}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_post_warnings", out)

	// Warnings alone are not reported for Edit.
	out, err = PostToolUse(env, postIn(t, "Edit", map[string]any{"file_path": abs, "old_string": "a", "new_string": "verify: v"}))
	mustNil(t, out, err)
}

func TestPostToolUseWriteOK(t *testing.T) {
	env := testEnv(t)
	abs := writePlan(t, env, "docs/plans/a.md", planOK)
	nilOut(t)(PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": abs})))
}

func TestPostToolUseEditCheckbox(t *testing.T) {
	env := testEnv(t)
	abs := writePlan(t, env, "docs/plans/a.md", planBad)
	out, err := PostToolUse(env, postIn(t, "Edit", map[string]any{"file_path": abs, "old_string": "- [x] step 1", "new_string": "- [ ] step 1"}))
	mustNil(t, out, err)
}

func TestPostToolUseEditInsideBlock(t *testing.T) {
	env := testEnv(t)
	abs := writePlan(t, env, "docs/plans/a.md", planBad)
	out, err := PostToolUse(env, postIn(t, "Edit", map[string]any{"file_path": abs, "old_string": "b.go", "new_string": "a.go"}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_post_block", out)

	t.Run("keyword", func(t *testing.T) {
		for _, kw := range []string{"paraplane", "waves:", "tasks:", "id:", "title:", "owns:", "reads:", "depends:", "verify:", "gate:"} {
			out, err := PostToolUse(env, postIn(t, "Edit", map[string]any{"file_path": abs, "old_string": "x " + kw, "new_string": "zzz"}))
			if err != nil {
				t.Fatal(err)
			}
			golden(t, "plan_post_block", out)
		}
	})
	t.Run("new text only outside block", func(t *testing.T) {
		out, err := PostToolUse(env, postIn(t, "Edit", map[string]any{"file_path": abs, "old_string": "x", "new_string": "Plan"}))
		mustNil(t, out, err)
	})
}

func TestPostToolUseEditOverlapsBlock(t *testing.T) {
	// planBad has "- [ ] step 1\n\n" right before the block's opening fence.
	env := testEnv(t)
	abs := writePlan(t, env, "docs/plans/a.md", planBad)
	edit := func(newS string) ([]byte, error) {
		return PostToolUse(env, postIn(t, "Edit", map[string]any{"file_path": abs, "old_string": "x", "new_string": newS}))
	}
	// Starts before the block and reaches into its fence.
	out, err := edit("1\n\n```")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_post_block", out)
	// Ends right where the block starts: outside.
	nilOut(t)(edit("step 1\n\n"))
}

func TestPostToolUseEditDeleteInBlock(t *testing.T) {
	env := testEnv(t)
	abs := writePlan(t, env, "docs/plans/a.md", planBad)
	out, err := PostToolUse(env, postIn(t, "Edit", map[string]any{
		"file_path":  abs,
		"old_string": "      - {id: T2, title: b, owns: [b.go]}\n",
		"new_string": "",
	}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_post_block", out)

	t.Run("empty new without keyword", func(t *testing.T) {
		out, err := PostToolUse(env, postIn(t, "Edit", map[string]any{"file_path": abs, "old_string": "step 2\n", "new_string": ""}))
		mustNil(t, out, err)
	})
}

func TestPostToolUseOtherFile(t *testing.T) {
	env := testEnv(t)
	readme := writePlan(t, env, "README.md", planBad)
	nilOut(t)(PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": readme})))

	outside := filepath.Join(t.TempDir(), "docs", "plans", "a.md")
	nilOut(t)(PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": outside})))

	nilOut(t)(PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": filepath.Join(env.Root, "..", "docs", "plans", "a.md")})))
	nilOut(t)(PostToolUse(env, postIn(t, "Write", map[string]any{})))
	nilOut(t)(PostToolUse(env, postIn(t, "Read", map[string]any{"file_path": readme})))
	nilOut(t)(PostToolUse(env, Input{ToolName: "Write", ToolInput: []byte(`[1`)}))

	t.Run("missing plan file", func(t *testing.T) {
		p := filepath.Join(env.Root, "docs", "plans", "gone.md")
		nilOut(t)(PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": p})))
	})
	t.Run("too large", func(t *testing.T) {
		p := writePlan(t, env, "docs/plans/big.md", planBad+strings.Repeat("x", 1<<20))
		nilOut(t)(PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": p})))
	})
}

func TestPostToolUseWindowsPath(t *testing.T) {
	env := testEnv(t)
	writePlan(t, env, "docs/plans/a.md", planBad)
	win := strings.ReplaceAll(filepath.Join(env.Root, "docs", "plans", "a.md"), "/", `\`)
	out, err := PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": win}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_post_block", out)

	t.Run("relative", func(t *testing.T) {
		out, err := PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": `docs\plans\a.md`}))
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "plan_post_block", out)
	})
}

// TestRootRelSymlink: a root and a path that name the same directory through
// a symlink (macOS: /var is /private/var) still compare equal; the plan file
// itself is not resolved.
func TestRootRelSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs", "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ root, fp string }{
		{link, filepath.Join(dir, "docs", "plans", "a.md")},
		{dir, filepath.Join(link, "docs", "plans", "a.md")},
		{link, filepath.Join(link, "docs", "plans", "a.md")},
	} {
		if rel, ok := rootRel(c.root, c.fp); !ok || rel != "docs/plans/a.md" {
			t.Errorf("rootRel(%s, %s) = %q, %v", c.root, c.fp, rel, ok)
		}
	}
	if rel, ok := rootRel(link, filepath.Join(t.TempDir(), "docs", "plans", "a.md")); ok {
		t.Errorf("outside root: got %q", rel)
	}
	// The hook itself finds and checks the plan.
	env := testEnv(t)
	env.Root = link
	writePlan(t, Env{Root: dir}, "docs/plans/a.md", planBad)
	out, err := PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": filepath.Join(dir, "docs", "plans", "a.md")}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_post_block", out)
	// A symlinked plan file is not followed.
	if err := os.Symlink(filepath.Join(dir, "docs", "plans", "a.md"), filepath.Join(dir, "docs", "plans", "b.md")); err != nil {
		t.Fatal(err)
	}
	nilOut(t)(PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": filepath.Join(dir, "docs", "plans", "b.md")})))
}

func TestPreToolUseDispatch(t *testing.T) {
	noShadow(t)
	env := testEnv(t)
	out, err := PreToolUse(env, toolInput(t, "Skill", map[string]any{"skill": "superpowers:writing-plans"}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "base_skill_digest", out)

	out, err = PreToolUse(env, toolInput(t, "Bash", map[string]any{"command": "paraplane version"}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "base_bash_allow", out)

	nilOut(t)(PreToolUse(env, toolInput(t, "Read", map[string]any{"file_path": "x"})))
}

// TestDisabled runs every handler for both agents: with enabled: false each
// is silent, and the same input gets an answer while enabled wherever the
// handler serves that agent (so a silent handler cannot pass by accident).
func TestDisabled(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no paraplane on PATH: Claude's Bash approval does not look further
	both := map[Agent]bool{Claude: true, Codex: true}
	codexOnly := map[Agent]bool{Codex: true}
	cases := []struct {
		name string
		live map[Agent]bool // the agents that get an answer while enabled
		call func(Env, string) ([]byte, error)
	}{
		{"SessionStart", both, func(e Env, _ string) ([]byte, error) {
			return SessionStart(e, Input{HookEventName: "SessionStart"})
		}},
		{"ExitPlanMode", both, func(e Env, _ string) ([]byte, error) {
			return PreToolUse(e, exitPlanIn(t, map[string]any{"plan": planBad}))
		}},
		{"Skill", both, func(e Env, _ string) ([]byte, error) {
			return PreToolUse(e, toolInput(t, "Skill", map[string]any{"skill": "superpowers:writing-plans"}))
		}},
		{"Bash", both, func(e Env, _ string) ([]byte, error) {
			return PreToolUse(e, toolInput(t, "Bash", map[string]any{"command": e.Agent.CLI() + " version"}))
		}},
		{"Write", both, func(e Env, abs string) ([]byte, error) {
			return PostToolUse(e, postIn(t, "Write", map[string]any{"file_path": abs}))
		}},
		{"Edit", both, func(e Env, abs string) ([]byte, error) {
			return PostToolUse(e, postIn(t, "Edit", map[string]any{"file_path": abs, "old_string": "x", "new_string": "owns:"}))
		}},
		{"Codex Bash", codexOnly, func(e Env, _ string) ([]byte, error) {
			return PostToolUse(e, bashIn(t, e.Root, "printf x > docs/plans/a.md"))
		}},
		{"Codex Bash read", codexOnly, func(e Env, _ string) ([]byte, error) {
			return PostToolUse(e, bashIn(t, e.Root, "cat docs/plans/a.md"))
		}},
		{"apply_patch", codexOnly, func(e Env, _ string) ([]byte, error) {
			return PostToolUse(e, Input{ToolName: "apply_patch", Cwd: e.Root,
				ToolInput: json.RawMessage(`{"input":"*** Add File: docs/plans/a.md\n"}`)})
		}},
		{"Stop", codexOnly, func(e Env, _ string) ([]byte, error) { return Stop(e, stopIn(t, turnBad)) }},
		{"PromptSubmit", codexOnly, func(e Env, _ string) ([]byte, error) {
			return PromptSubmit(e, Input{Prompt: "$superpowers:executing-plans"})
		}},
		{"PermissionRequest", codexOnly, func(e Env, _ string) ([]byte, error) {
			return PermissionRequest(e, permIn(t, safeRoot+"/bin/paraplane version"))
		}},
	}
	for _, agent := range []Agent{Claude, Codex} {
		// Claude's Bash approval needs a root the OS calls absolute
		// ("C:\…" on Windows); Codex's CLI path needs a POSIX root.
		root := safeRoot
		if agent == Claude {
			root = t.TempDir()
		}
		t.Setenv("CLAUDE_PLUGIN_ROOT", root)
		env := testEnv(t)
		env.Agent = agent
		env.ConfigErr = errors.New("config.yaml: ignored mode (bad)")
		abs := writePlan(t, env, "docs/plans/a.md", planBad)
		for _, c := range cases {
			t.Run(string(agent)+" "+c.name, func(t *testing.T) {
				out, err := c.call(env, abs)
				if live := c.live[agent]; err != nil || (out != nil) != live {
					t.Errorf("enabled: got %q, %v; want an answer: %v", out, err, live)
				}
				off := env
				off.Config.Enabled = false
				nilOut(t)(c.call(off, abs))
			})
		}
	}
}

func TestOptions(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	old := postNow
	postNow = func() time.Time { return now }
	t.Cleanup(func() { postNow = old })
	env := testEnv(t)
	env.Config.HotFiles = []string{"go.mod"}
	env.Config.DefaultGate = "make"
	env.Config.MaxParallel = 3
	if err := os.MkdirAll(filepath.Join(env.Root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePlan(t, env, "a/f.go", "x")
	o := Options(env)
	if len(o.HotFiles) != 1 || o.HotFiles[0] != "go.mod" || o.DefaultGate != "make" || o.MaxParallel != 3 {
		t.Errorf("Options = %+v", o)
	}
	if want := now.Add(5 * time.Second); !o.Deadline.Equal(want) {
		t.Errorf("deadline %v, want %v", o.Deadline, want)
	}
	if o.IsDir == nil || !o.IsDir("a/b") || !o.IsDir("a") || o.IsDir("a/f.go") || o.IsDir("nope") {
		t.Error("IsDir does not follow the file system under Root")
	}
}

// planLongW3 is planW3 with a title so long that the shorter layout is over
// maxLayout.
func planLongW3() string {
	return strings.Replace(planW3, "title: a,", "title: "+strings.Repeat("a", maxLayout)+",", 1)
}

func TestExitPlanModeLongLayout(t *testing.T) {
	// A shorter layout over the cap is not cut (a cut block is not valid
	// YAML): the text points at `paraplane check --suggest` instead.
	out, err := PreToolUse(testEnv(t), exitPlanIn(t, map[string]any{"plan": planLongW3()}))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "warning W3: ") || strings.Contains(s, "Shorter layout:") || strings.Contains(s, "```paraplane\\nversion") ||
		!strings.HasSuffix(s, `\nThe shorter layout is too long to show here: run `+"`paraplane check --suggest <file>`"+` for the shorter layout."}}`) {
		t.Errorf("got %.500s … %s", s, s[max(0, len(s)-300):])
	}
	if len(out) > 8<<10 {
		t.Errorf("answer is %d bytes", len(out))
	}
}

// specialFiles makes a FIFO and a symlink to a valid plan file under dir of
// env.Root; each case gets a name and the absolute path.
func specialFiles(t *testing.T, env Env, dir string) map[string]string {
	t.Helper()
	target := writePlan(t, env, "target/p.md", planBad)
	got := map[string]string{}
	link := filepath.Join(env.Root, filepath.FromSlash(dir), "link.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err == nil {
		got["symlink"] = link
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		fifo := filepath.Join(env.Root, filepath.FromSlash(dir), "fifo.md")
		if out, err := exec.Command("mkfifo", fifo).CombinedOutput(); err != nil {
			t.Fatalf("mkfifo: %v %s", err, out)
		}
		got["fifo"] = fifo
	}
	return got
}

// within fails the test when call does not return within 10 s: a hook that
// opened a FIFO or a device would block for good, so the limit only has to
// be far above the time of a normal call, also under -race.
func within(t *testing.T, call func() ([]byte, error)) ([]byte, error) {
	t.Helper()
	type res struct {
		out []byte
		err error
	}
	ch := make(chan res, 1)
	go func() {
		out, err := call()
		ch <- res{out, err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-time.After(10 * time.Second):
		t.Fatal("hook blocked")
		return nil, nil
	}
}

func TestExitPlanModeSpecialFile(t *testing.T) {
	env := testEnv(t)
	for name, p := range specialFiles(t, env, "plans") {
		t.Run(name, func(t *testing.T) {
			nilOut(t)(within(t, func() ([]byte, error) {
				return PreToolUse(env, exitPlanIn(t, map[string]any{"planFilePath": p}))
			}))
		})
	}
}

func TestPostToolUseSpecialFile(t *testing.T) {
	env := testEnv(t)
	for name, p := range specialFiles(t, env, "docs/plans") {
		t.Run(name, func(t *testing.T) {
			nilOut(t)(within(t, func() ([]byte, error) {
				return PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": p}))
			}))
		})
	}
}

func TestPostToolUseDeadline(t *testing.T) {
	old := postNow
	postNow = func() time.Time { return time.Now().Add(-time.Minute) }
	t.Cleanup(func() { postNow = old })
	env := testEnv(t)
	abs := writePlan(t, env, "docs/plans/a.md", planOK)
	out, err := PostToolUse(env, postIn(t, "Write", map[string]any{"file_path": abs}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_post_deadline", out)

	out, err = PreToolUse(env, exitPlanIn(t, map[string]any{"plan": planOK}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_exit_deadline", out)
}
