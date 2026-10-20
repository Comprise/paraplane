package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Turns of testdata/codex_transcript.jsonl.
const (
	turnBad   = "01a15914-2abb-7442-952c-4e7261ae2507" // proposed plan with E7 and W1
	turnFixed = "01a15914-8987-7ce0-9f52-61fec1ca0bec" // a bad plan, then a revised valid one
	turnNone  = "01a15915-2708-7462-9a82-1d3f93a59640" // no plan from the assistant
)

// safeRoot is a plugin root that passes pluginRootSafe.
const safeRoot = "/opt/codex/plugins/paraplane"

// codexEnv is testEnv for the Codex agent.
func codexEnv(t *testing.T) Env {
	t.Helper()
	env := testEnv(t)
	env.Agent = Codex
	return env
}

// codexPayload reads testdata/<name>.json (a captured Codex payload), applies
// the overrides and decodes it into Input.
func codexPayload(t *testing.T, name string, set map[string]any) Input {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for k, v := range set {
		m[k] = v
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var in Input
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	return in
}

// fixtureAbs is the absolute path of a testdata file.
func fixtureAbs(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// stopIn is the captured Stop payload for turn with the fixture transcript.
func stopIn(t *testing.T, turn string) Input {
	t.Helper()
	return codexPayload(t, "codex_stop", map[string]any{
		"turn_id": turn, "transcript_path": fixtureAbs(t, "codex_transcript.jsonl"),
	})
}

func TestAgentRefs(t *testing.T) {
	if got := Claude.Skill("rules"); got != "paraplane:rules" {
		t.Errorf("Claude.Skill = %q", got)
	}
	if got := Agent("").Skill("rules"); got != "paraplane:rules" {
		t.Errorf("zero Agent.Skill = %q", got)
	}
	if got := Codex.Skill("rules"); got != "$paraplane:rules" {
		t.Errorf("Codex.Skill = %q", got)
	}
	if a, ok := ParseAgent("codex"); a != Codex || !ok {
		t.Errorf("ParseAgent(codex) = %q, %v", a, ok)
	}
	if a, ok := ParseAgent("claude"); a != Claude || !ok {
		t.Errorf("ParseAgent(claude) = %q, %v", a, ok)
	}
	for _, s := range []string{"", "Codex", "CODEX", "gpt", " codex"} {
		if a, ok := ParseAgent(s); a != "" || ok {
			t.Errorf("ParseAgent(%q) = %q, %v", s, a, ok)
		}
	}
}

func TestCodexCLIPath(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", safeRoot)
	if got := Codex.CLI(); got != safeRoot+"/bin/paraplane" {
		t.Errorf("Codex.CLI = %q", got)
	}
	if got := Claude.CLI(); got != "paraplane" {
		t.Errorf("Claude.CLI = %q", got)
	}
	env := codexEnv(t)
	out, err := SessionStart(env, Input{HookEventName: "SessionStart"})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "codex_session_start", out)

	// Claude's text is unchanged whatever the plugin root.
	out, err = SessionStart(testEnv(t), Input{HookEventName: "SessionStart"})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "base_session_start", out)

	for _, bad := range []string{"", "/", "//", "/.", "/home/u/my plugin", "relative/path", "/x;rm", "/x$(id)", "/x`id`"} {
		t.Setenv("CLAUDE_PLUGIN_ROOT", bad)
		if got := Codex.CLI(); got != "paraplane" {
			t.Errorf("root %q: Codex.CLI = %q, want the bare name", bad, got)
		}
	}
}

func TestCodexStopPlanErrors(t *testing.T) {
	in := stopIn(t, turnBad)
	if in.LastAssistantMessage != "" || in.TurnID != turnBad || in.StopHookActive {
		t.Fatalf("payload decoded as %+v", in)
	}
	out, err := Stop(codexEnv(t), in)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "codex_stop_block", out)
}

func TestCodexStopWarnMode(t *testing.T) {
	env := codexEnv(t)
	env.Config.Mode = "warn"
	out, err := Stop(env, stopIn(t, turnBad))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "codex_stop_warnmode", out)
}

func TestCodexStopPlanOK(t *testing.T) {
	// The turn's last plan wins: the revised plan is valid.
	out, err := Stop(codexEnv(t), stopIn(t, turnFixed))
	mustNil(t, out, err)
}

func TestCodexStopOtherTurn(t *testing.T) {
	// Plans of other turns and a user message quoting a plan are ignored.
	out, err := Stop(codexEnv(t), stopIn(t, turnNone))
	mustNil(t, out, err)
	in := stopIn(t, "")
	out, err = Stop(codexEnv(t), in)
	mustNil(t, out, err)
}

func TestCodexStopHookActive(t *testing.T) {
	in := stopIn(t, turnBad)
	in.StopHookActive = true
	out, err := Stop(codexEnv(t), in)
	mustNil(t, out, err)
}

func TestCodexStopNotCodexOrDisabled(t *testing.T) {
	out, err := Stop(testEnv(t), stopIn(t, turnBad))
	mustNil(t, out, err)
	env := codexEnv(t)
	env.Config.Enabled = false
	out, err = Stop(env, stopIn(t, turnBad))
	mustNil(t, out, err)
}

func TestCodexStopLastMessage(t *testing.T) {
	// A future Codex may send the plan in last_assistant_message.
	in := codexPayload(t, "codex_stop", map[string]any{
		"transcript_path": nil, "last_assistant_message": "<proposed_plan>\n" + planBad + "</proposed_plan>",
	})
	out, err := Stop(codexEnv(t), in)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "codex_stop_block", out)
}

func TestCodexStopNoTranscript(t *testing.T) {
	env := codexEnv(t)
	for name, p := range map[string]any{
		"null":     nil,
		"missing":  filepath.Join(env.Root, "nope.jsonl"),
		"relative": "testdata/codex_transcript.jsonl",
		"dir":      env.Root,
	} {
		in := codexPayload(t, "codex_stop", map[string]any{"transcript_path": p})
		out, err := Stop(env, in)
		if err != nil || out != nil {
			t.Errorf("%s: got %q, %v; want nil, nil", name, out, err)
		}
	}
}

func TestCodexStopUnknownTranscriptFormat(t *testing.T) {
	in := codexPayload(t, "codex_stop", map[string]any{"transcript_path": fixtureAbs(t, "codex_unknown.jsonl")})
	out, err := Stop(codexEnv(t), in)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "codex_stop_unknown", out)

	// Without this turn in the tail (another turn, empty, blank, not yet
	// flushed) nothing is said.
	other := in
	other.TurnID = "01a15916-0000-7000-8000-000000000000"
	out, err = Stop(codexEnv(t), other)
	mustNil(t, out, err)
	noTurn := in
	noTurn.TurnID = ""
	out, err = Stop(codexEnv(t), noTurn)
	mustNil(t, out, err)
	for name, content := range map[string]string{"empty": "", "blank": "\n\n  \n"} {
		p := filepath.Join(t.TempDir(), name+".jsonl")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		in.TranscriptPath = p
		out, err = Stop(codexEnv(t), in)
		if err != nil || out != nil {
			t.Errorf("%s: got %q, %v; want nil, nil", name, out, err)
		}
	}
}

func TestCodexStopTranscriptFIFO(t *testing.T) {
	env := codexEnv(t)
	dir := t.TempDir()
	cases := map[string]string{}
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(fixtureAbs(t, "codex_transcript.jsonl"), link); err == nil {
		cases["symlink"] = link
	}
	if runtime.GOOS != "windows" {
		fifo := filepath.Join(dir, "fifo.jsonl")
		if out, err := exec.Command("mkfifo", fifo).CombinedOutput(); err != nil {
			t.Fatalf("mkfifo: %v %s", err, out)
		}
		cases["fifo"] = fifo
	}
	if len(cases) == 0 {
		t.Skip("neither symlinks nor FIFOs on this system")
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			in := stopIn(t, turnBad)
			in.TranscriptPath = p
			nilOut(t)(within(t, func() ([]byte, error) { return Stop(env, in) }))
		})
	}
}

func TestCodexStopLargeTranscript(t *testing.T) {
	// Only the last 4 MB are read; the cut first line is skipped.
	data, err := os.ReadFile(filepath.Join("testdata", "codex_transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	pad := `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` +
		strings.Repeat("x", 5<<20) + `"}]}}` + "\n"
	p := filepath.Join(t.TempDir(), "big.jsonl")
	if err := os.WriteFile(p, append([]byte(pad), data...), 0o644); err != nil {
		t.Fatal(err)
	}
	in := stopIn(t, turnBad)
	in.TranscriptPath = p
	out, err := Stop(codexEnv(t), in)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "codex_stop_block", out)
}

// bashIn is the captured Bash PostToolUse payload with command run in cwd.
func bashIn(t *testing.T, cwd, command string) Input {
	t.Helper()
	return codexPayload(t, "codex_post_bash", map[string]any{
		"cwd": cwd, "tool_input": map[string]any{"command": command},
	})
}

func TestCodexBashPlanFile(t *testing.T) {
	env := codexEnv(t)
	writePlan(t, env, "docs/plans/x.md", planBad)
	in := codexPayload(t, "codex_post_bash", map[string]any{"cwd": env.Root})
	if in.ToolName != "Bash" || in.TurnID != turnNone {
		t.Fatalf("payload decoded as %+v", in)
	}
	for name, in := range map[string]Input{
		"captured": in,
		"printf":   bashIn(t, env.Root, "mkdir -p docs/plans && printf 'x' > docs/plans/x.md"),
		"from cwd": bashIn(t, filepath.Join(env.Root, "docs"), "printf x >plans/x.md"),
		"absolute": bashIn(t, "/elsewhere", "tee "+filepath.Join(env.Root, "docs", "plans", "x.md")+"</dev/null"),
	} {
		out, err := PostToolUse(env, in)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(name, func(t *testing.T) { golden(t, "codex_post_block", out) })
	}

	// Claude has no Bash post-check: its behaviour is unchanged.
	out, err := PostToolUse(testEnv(t), in)
	mustNil(t, out, err)
}

func TestCodexBashQuoted(t *testing.T) {
	env := codexEnv(t)
	writePlan(t, env, "docs/plans/a b.md", planBad)
	for _, cmd := range []string{
		`cat > "docs/plans/a b.md" <<EOF`,
		`cat > 'docs/plans/a b.md' <<'EOF'`,
		`cat > docs/plans/a\ b.md`,
		`(cd . && cat >"docs/plans/a b.md");`,
	} {
		out, err := PostToolUse(env, bashIn(t, env.Root, cmd))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), `"decision":"block"`) || !strings.Contains(string(out), "docs/plans/a b.md is not ready") {
			t.Errorf("%s: got %s", cmd, out)
		}
	}
}

func TestCodexBashOther(t *testing.T) {
	env := codexEnv(t)
	writePlan(t, env, "docs/plans/ok.md", planOK)
	writePlan(t, env, "notes.md", planBad)
	writePlan(t, env, "docs/plans/warn.md", planW1)
	for _, cmd := range []string{
		"ls -la && echo hi > notes.txt",
		"cat notes.md",              // not a plan path
		"cat docs/plans/ok.md",      // a valid plan
		"cat docs/plans/warn.md",    // warnings only: not a Write
		"cat docs/plans/missing.md", // no such file
		"cat ../outside/docs/plans/x.md",
		"",
	} {
		out, err := PostToolUse(env, bashIn(t, env.Root, cmd))
		if err != nil || out != nil {
			t.Errorf("%q: got %q, %v; want nil, nil", cmd, out, err)
		}
	}
	// A command that is not a string is ignored.
	in := codexPayload(t, "codex_post_bash", map[string]any{"cwd": env.Root, "tool_input": map[string]any{"command": []string{"cat", "docs/plans/x.md"}}})
	nilOut(t)(PostToolUse(env, in))
}

func TestCodexBashSpecialFile(t *testing.T) {
	env := codexEnv(t)
	for name, p := range specialFiles(t, env, "docs/plans") {
		t.Run(name, func(t *testing.T) {
			nilOut(t)(within(t, func() ([]byte, error) {
				return PostToolUse(env, bashIn(t, env.Root, "cat > "+p))
			}))
		})
	}
}

func TestCodexApplyPatchPlanFile(t *testing.T) {
	env := codexEnv(t)
	writePlan(t, env, "docs/plans/x.md", planBad)
	writePlan(t, env, "docs/plans/ok.md", planOK)
	patch := "*** Begin Patch\n*** Add File: docs/plans/x.md\n+# Plan\n*** End Patch\n"
	for name, fields := range map[string]map[string]any{
		"command": {"command": patch},
		"input":   {"input": patch},
		"patch":   {"patch": patch},
		"move":    {"input": "*** Begin Patch\n*** Update File: docs/plans/ok.md\n*** Move to: docs/plans/x.md\n@@\n-a\n+b\n*** End Patch\n"},
	} {
		in := codexPayload(t, "codex_post_bash", map[string]any{"cwd": env.Root, "tool_name": "apply_patch", "tool_input": fields})
		out, err := PostToolUse(env, in)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(name, func(t *testing.T) { golden(t, "codex_post_block", out) })
	}
	in := codexPayload(t, "codex_post_bash", map[string]any{"cwd": env.Root, "tool_name": "apply_patch",
		"tool_input": map[string]any{"input": "*** Begin Patch\n*** Update File: docs/plans/ok.md\n*** End Patch\n"}})
	nilOut(t)(PostToolUse(env, in))
}

func TestCodexPromptDigest(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", safeRoot)
	env := codexEnv(t)
	cases := []struct {
		prompt string
		golden string // "" means nil output
	}{
		{"$superpowers:executing-plans docs/plans/x.md", "codex_prompt_exec"},
		{"use $superpowers:subagent-driven-development now", "codex_prompt_exec"},
		{"$superpowers:writing-plans for the cache", "codex_prompt_digest"},
		{"$superpowers:writing-plans.", "codex_prompt_digest"},
		{"$writing-plans", "codex_prompt_digest"},
		{"$Writing-Plans", ""},
		{"echo $PLAN_FILE > x", ""},
		{"run $plan_file and $EXECUTOR", ""},
		{"$superpowers:Executing-Plans", ""},
		{"$superpowers:writing-plans then $superpowers:executing-plans", "codex_prompt_exec"},
		{"$superpowers:writing-plans$superpowers:executing-plans", "codex_prompt_exec"},
		{"$x$superpowers:writing-plans", "codex_prompt_digest"},
		{"$plan_file$superpowers:writing-plans", "codex_prompt_digest"},
		{"$paraplane:rules about the plan", ""},
		{"$paraplane:create a plan", ""},
		{"write a plan and execute it", ""},
		{"$pp-probe:probe", ""},
		{"", ""},
	}
	for _, c := range cases {
		in := codexPayload(t, "codex_prompt", map[string]any{"prompt": c.prompt})
		out, err := PromptSubmit(env, in)
		if err != nil {
			t.Fatal(err)
		}
		if c.golden == "" {
			if out != nil {
				t.Errorf("%q: got %s, want nil", c.prompt, out)
			}
			continue
		}
		golden(t, c.golden, out)
	}
	in := codexPayload(t, "codex_prompt", map[string]any{"prompt": "$superpowers:writing-plans"})
	off := codexEnv(t)
	off.Config.Enabled = false
	nilOut(t)(PromptSubmit(off, in))
	nilOut(t)(PromptSubmit(testEnv(t), in))
}

// permIn is the PermissionRequest payload for a Bash command.
func permIn(t *testing.T, command string) Input {
	t.Helper()
	return codexPayload(t, "codex_permission_request", map[string]any{
		"tool_input": map[string]any{"command": command, "description": nil},
	})
}

func TestPermissionRequestAllows(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", safeRoot)
	env := codexEnv(t)
	cli := safeRoot + "/bin/paraplane "
	for _, cmd := range []string{cli + "version", cli + "check docs/plans/x.md", cli + "audit docs/p.md W1", cli + "progress docs/p.md", cli + "check --suggest p.md"} {
		out, err := PermissionRequest(env, permIn(t, cmd))
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "codex_permission_allow", out)
	}
}

func TestPermissionRequestSilent(t *testing.T) {
	for _, root := range []string{"", safeRoot, "/home/u/my plugin"} {
		t.Run("root "+root, func(t *testing.T) {
			t.Setenv("CLAUDE_PLUGIN_ROOT", root)
			env := codexEnv(t)
			// The captured payload runs the bare name: never approved for Codex.
			nilOut(t)(PermissionRequest(env, codexPayload(t, "codex_permission_request", nil)))
			for _, cmd := range []string{
				"paraplane version", "paraplane check docs/plans/x.md", "paraplane audit docs/p.md W1",
				"paraplane check ../x.md", "paraplane check a.md; rm -rf ~", "paraplane check  a.md", " paraplane version",
				"paraplane version\n", "rm -rf /", "paraplane hook stop", "paraplane check $(id).md",
				"/home/u/my plugin/bin/paraplane version",
			} {
				out, err := PermissionRequest(env, permIn(t, cmd))
				if err != nil || out != nil {
					t.Errorf("%q: got %q, %v; want nil, nil", cmd, out, err)
				}
			}
			other := codexPayload(t, "codex_permission_request", map[string]any{"tool_name": "apply_patch"})
			nilOut(t)(PermissionRequest(env, other))
		})
	}
	t.Setenv("CLAUDE_PLUGIN_ROOT", safeRoot)
	off := codexEnv(t)
	off.Config.Enabled = false
	nilOut(t)(PermissionRequest(off, permIn(t, safeRoot+"/bin/paraplane version")))
	nilOut(t)(PermissionRequest(testEnv(t), permIn(t, safeRoot+"/bin/paraplane version")))
}

func TestPermissionRequestAbsolutePath(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", safeRoot)
	env := codexEnv(t)
	for _, cmd := range []string{safeRoot + "/bin/paraplane check docs/plans/x.md", safeRoot + "/bin/paraplane audit docs/p.md W2"} {
		out, err := PermissionRequest(env, permIn(t, cmd))
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "codex_permission_allow", out)
	}
	for _, cmd := range []string{
		"/other/bin/paraplane version",
		safeRoot + "/bin/paraplane check ../x.md",
		safeRoot + "/bin/paraplane version; id",
		safeRoot + "/bin/paraplanex version",
		safeRoot + "/bin/paraplane paraplane version",
	} {
		out, err := PermissionRequest(env, permIn(t, cmd))
		if err != nil || out != nil {
			t.Errorf("%q: got %q, %v; want nil, nil", cmd, out, err)
		}
	}
}

// planE2 has a single error E2: both tasks of W1 own a.go.
const planE2 = "```paraplane\ngate: make\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a.go], verify: v}\n      - {id: T2, title: b, owns: [a.go], verify: v}\n```\n"

func TestCodexBashReadVsWrite(t *testing.T) {
	env := codexEnv(t)
	writePlan(t, env, "docs/plans/noblock.md", "# Old plan\n\n- [x] step 1\n")
	writePlan(t, env, "docs/plans/e2.md", planE2)
	writePlan(t, env, "docs/plans/warn.md", planW1)
	const (
		none  = ""
		e0    = "has no valid ```paraplane block yet"
		block = `"decision":"block"`
		warn  = "plan warnings (not blocking)"
	)
	for _, c := range []struct{ cmd, want string }{
		// Reads: only E1–E7 count.
		{"sed -n 1,40p docs/plans/noblock.md", none},
		{"grep -n step docs/plans/noblock.md", none},
		{"cat docs/plans/warn.md", none},
		{"cp docs/plans/noblock.md /tmp/y", none},
		{"cat docs/plans/noblock.md 2>&1 > out.txt", none},
		{"cat docs/plans/noblock.md >&2", none},
		{"sed -e s/a/b/ docs/plans/noblock.md", none},
		{"cat docs/plans/e2.md", block},
		{"grep -c x docs/plans/e2.md", block},
		// Writes: the full verdict of a Write.
		{"printf x > docs/plans/noblock.md", e0},
		{"printf x >>docs/plans/noblock.md", e0},
		{"printf x 2>/dev/null >| docs/plans/noblock.md", e0},
		{"echo x | tee -a docs/plans/noblock.md", e0},
		{"printf x &> docs/plans/noblock.md", e0},
		{"printf x &>>docs/plans/noblock.md", e0},
		{"cat > 'docs/plans/noblock.md", e0}, // unterminated quote
		{`cat > "docs/plans/noblock.md`, e0},
		{"cp /tmp/x docs/plans/noblock.md", e0},
		// cp/mv/install -t DIR: DIR is the destination, the sources are reads.
		{"cp -t /tmp/out docs/plans/noblock.md", none},
		{"cp -rt /tmp/out docs/plans/noblock.md", none},
		{"mv -t/tmp/out docs/plans/noblock.md", none},
		{"mv --target-directory=/tmp/out docs/plans/noblock.md", none},
		{"install --target-directory /tmp/out docs/plans/noblock.md docs/plans/warn.md", none},
		{"cp -t /tmp/out docs/plans/e2.md", block},
		{"cp -t docs/plans /tmp/x/noblock.md", e0}, // writes docs/plans/noblock.md
		{"mv --target-directory=docs/plans/ src/noblock.md", e0},
		{"install -t docs/plans /tmp/warn.md", warn},
		{"mv -f /tmp/x docs/plans/noblock.md", e0},
		{"install -m 644 /tmp/x docs/plans/noblock.md", e0},
		{"LC_ALL=C tee docs/plans/noblock.md </dev/null", e0},
		{"cat docs/plans/noblock.md; printf x > docs/plans/noblock.md", e0},
		{"printf x >> docs/plans/warn.md", warn},
		// sed -i is an Edit: checked only when the script names a key or
		// inserts text found inside the block, and warnings are not repeated.
		{"sed -i 's/a/b/' docs/plans/noblock.md", none},
		{"sed -i.bak -e s/a/b/ docs/plans/noblock.md", none},
		{"sed --in-place -n p docs/plans/noblock.md", e0}, // not an s command: a write
		{`sed -i 's/- \[ \] step/- [x] step/' docs/plans/e2.md`, none},
		{"sed -i s/T9/T8/ docs/plans/e2.md", none},
		{"sed -i 's/x/a.go/' docs/plans/e2.md", block},
		{"sed -i -e 1p --expression='s|x|a.go|' docs/plans/e2.md", block},
		{"sed -i '/owns:/d' docs/plans/e2.md", block},
		{"sed -i -f fix.sed docs/plans/e2.md", block},
		{"sed -i --file=fix.sed docs/plans/e2.md", block},
		{"sed -i 's/waves:/waves:/' docs/plans/noblock.md", e0},
		{"sed -i 's/x/verify: v/' docs/plans/warn.md", none},
		{"sed -n 's/x/a.go/p' docs/plans/e2.md", block}, // a read: E2 still blocks
		{"sed -i '3s/x/QQ/g;/z/,$s,a,ZZ,' docs/plans/e2.md", none},
		{`sed -i 's/x/\&/' docs/plans/e2.md`, none}, // an escaped & is literal
		// The script does not show the inserted text: the full Write verdict.
		{"sed -i '/T1,/d' docs/plans/e2.md", block},
		{"sed -i 8d docs/plans/e2.md", block},
		{"sed -i '9i foo: [' docs/plans/e2.md", block},
		{"sed -i '/T1,/a x' docs/plans/e2.md", block},
		{"sed -i '9c x' docs/plans/e2.md", block},
		{"sed -i 'y/b/a/' docs/plans/e2.md", block},
		{"sed -i 's/T2/&/' docs/plans/e2.md", block},
		{`sed -E -i 's/(T)2/\11/' docs/plans/e2.md`, block},
		{"sed -i -nf fix.sed docs/plans/e2.md", block},
		{"sed -i 's/x/y/w out' docs/plans/e2.md", block},
		{"sed -i 9d docs/plans/warn.md", warn},
		// Read and an edit that misses the block: the read verdict stays.
		{"cat docs/plans/e2.md; sed -i s/T9/T8/ docs/plans/e2.md", block},
		{"cat docs/plans/noblock.md; sed -i s/a/b/ docs/plans/noblock.md", none},
		{"printf x > docs/plans/e2.md", block},
	} {
		out, err := PostToolUse(env, bashIn(t, env.Root, c.cmd))
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case c.want == none && out != nil:
			t.Errorf("%q: got %s, want nil", c.cmd, out)
		case c.want != none && !strings.Contains(string(out), c.want):
			t.Errorf("%q: got %s, want %q", c.cmd, out, c.want)
		}
	}

	// apply_patch: Add File and Move to are writes, Update File is an edit.
	for _, c := range []struct{ patch, want string }{
		{"*** Add File: docs/plans/noblock.md\n+x\n", e0},
		{"*** Add File: docs/plans/warn.md\n+x\n", warn},
		{"*** Update File: docs/plans/noblock.md\n@@\n-- [ ] step 1\n+- [x] step 1\n", none},
		{"*** Update File: docs/plans/noblock.md\r\n@@\r\n+waves:\r\n", e0},
		{"*** Update File: docs/plans/e2.md\n@@\n- [ ] step\n+- [x] step\n", none},
		{"*** Update File: docs/plans/e2.md\n@@\n-      - {id: T2, title: b, owns: [b.go], verify: v}\n+x\n", block},
		{"*** Update File: docs/plans/e2.md\n@@\n-x\n+a.go\n", block},
		{"*** Update File: docs/plans/e2.md\n*** End of File\n*** Delete File: docs/plans/noblock.md\n+owns:\n", none},
		{"*** Update File: docs/plans/warn.md\n@@\n+verify: v\n", none},
		{"*** Update File: docs/plans/x.md\n*** Move to: docs/plans/noblock.md\n@@\n-a\n+b\n", e0},
	} {
		in := codexPayload(t, "codex_post_bash", map[string]any{"cwd": env.Root, "tool_name": "apply_patch",
			"tool_input": map[string]any{"input": "*** Begin Patch\n" + c.patch + "*** End Patch\n"}})
		out, err := PostToolUse(env, in)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case c.want == none && out != nil:
			t.Errorf("patch %q: got %s, want nil", c.patch, out)
		case c.want != none && !strings.Contains(string(out), c.want):
			t.Errorf("patch %q: got %s, want %q", c.patch, out, c.want)
		}
	}
}

func TestShellWordsCopyTarget(t *testing.T) {
	kinds := func(cmd string) map[string]postKind {
		got := map[string]postKind{}
		for _, w := range shellWords(cmd) {
			got[w.text] = w.kind
		}
		return got
	}
	for _, cmd := range []string{"cp -t D a x/b", "mv -tD a x/b", "install --target-directory=D/ a x/b", "cp -vt D a x/b"} {
		k := kinds(cmd)
		if k["D/a"] != postWrite || k["D/b"] != postWrite || k["a"] != postRead || k["x/b"] != postRead {
			t.Errorf("%q: kinds %v; want D/a and D/b written, the sources read", cmd, k)
		}
	}
	if k := kinds("cp a b"); k["a"] != postRead || k["b"] != postWrite {
		t.Errorf("cp a b: kinds %v", k)
	}
}

func TestSedPlain(t *testing.T) {
	for script, want := range map[string]string{
		"s/a/b/":                   "b",
		"s|a|b/c|g":                "b/c",
		`s/a\/b/c\/d/`:             "c/d",
		"/x/s/a/b/;3s,c,d,":        "b d",
		"1,$ s/a/b/2\n/x/!s/c/d/I": "b d",
		`\#x#s/a/b/`:               "b",
		`s/a/\&/`:                  `\&`,
	} {
		got, ok := sedPlain(script)
		if !ok || strings.Join(got, " ") != want {
			t.Errorf("%q: got %q, %v; want %q", script, got, ok, want)
		}
	}
	for _, script := range []string{"p", "8d", "s/unterminated", "s/a/&/", `s/(a)/\1/`, "y/a/b/", "s/a/b/w f", "s/a/b/e", "{s/a/b/}", "/x/d", "s/a/b/;d", ""} {
		if script == "" {
			continue // an empty script is checked by markSed (no scripts)
		}
		if got, ok := sedPlain(script); ok {
			t.Errorf("%q: plain with %q", script, got)
		}
	}
}

func TestCodexMaxPlans(t *testing.T) {
	// Only the first codexMaxPlans plan files of one command are checked.
	env := codexEnv(t)
	var files []string
	for i := range codexMaxPlans {
		files = append(files, fmt.Sprintf("docs/plans/ok%d.md", i))
		writePlan(t, env, files[i], planOK)
	}
	writePlan(t, env, "docs/plans/bad.md", planE2)
	cmd := "cat " + strings.Join(files, " ") + " docs/plans/bad.md"
	nilOut(t)(PostToolUse(env, bashIn(t, env.Root, cmd)))
	// A plan file mentioned again does not take a new slot.
	cmd = "cat docs/plans/bad.md " + strings.Join(files, " ") + " docs/plans/bad.md"
	out, err := PostToolUse(env, bashIn(t, env.Root, cmd))
	if err != nil || !strings.Contains(string(out), `"decision":"block"`) {
		t.Errorf("bad first: got %s, %v", out, err)
	}
}

// TestCodexOneDeadline: the plan files of one command share one deadline.
// The first Options call sees a deadline already passed and any later call
// a fresh one: both plans (E2, read) must get the deadline verdict, which a
// read answers with nothing.
func TestCodexOneDeadline(t *testing.T) {
	old := postNow
	calls := 0
	postNow = func() time.Time {
		calls++
		if calls == 1 {
			return time.Now().Add(-time.Minute)
		}
		return time.Now()
	}
	t.Cleanup(func() { postNow = old })
	env := codexEnv(t)
	writePlan(t, env, "docs/plans/a.md", planE2)
	writePlan(t, env, "docs/plans/b.md", planE2)
	nilOut(t)(PostToolUse(env, bashIn(t, env.Root, "cat docs/plans/a.md docs/plans/b.md")))
	if calls != 1 {
		t.Errorf("Options called %d times, want 1", calls)
	}
}

// slowPlan owns the slowest patterns pathset.Clean allows next to long
// literals none of them match (layout's worstPlan): its conflict check runs
// until the deadline.
func slowPlan() string {
	const pat = "**/*/{*a,*b}*/{*a,*b}**/*/{*a,*b}**/{*a,*b}**/{*a,*b}**/{*a,*b}**/{*a,*b}**/{*a,*b}/z%d"
	lit := strings.TrimSuffix(strings.Repeat("ab/", 84), "/") + "/x%d"
	var b strings.Builder
	b.WriteString("```paraplane\ngate: make\nwaves:\n  - id: W1\n    tasks:\n")
	n := 0
	for i := range 40 {
		fmt.Fprintf(&b, "      - id: T%d\n        title: t\n        verify: v\n        owns:\n", i)
		for range 4 {
			n++
			f := lit
			if i%2 == 0 {
				f = pat
			}
			fmt.Fprintf(&b, "          - %q\n", fmt.Sprintf(f, n))
		}
	}
	b.WriteString("```\n")
	return b.String()
}

// TestCodexSlowPlansOneDeadline: two plan files that each run until the
// deadline take one deadline in all, not one each.
func TestCodexSlowPlansOneDeadline(t *testing.T) {
	const budget = 400 * time.Millisecond
	old := postNow
	postNow = func() time.Time { return time.Now().Add(budget - checkTimeout) }
	t.Cleanup(func() { postNow = old })
	env := codexEnv(t)
	writePlan(t, env, "docs/plans/a.md", slowPlan())
	writePlan(t, env, "docs/plans/b.md", slowPlan())
	// A read answers a deadline E0 with nothing, so both files are checked.
	start := time.Now()
	out, err := PostToolUse(env, bashIn(t, env.Root, "cat docs/plans/a.md docs/plans/b.md"))
	d := time.Since(start)
	mustNil(t, out, err)
	t.Logf("two slow plans: %v", d)
	// A deadline per file would take at least 2*budget.
	if d >= 2*budget {
		t.Errorf("took %v; want under %v (one deadline)", d, 2*budget)
	}
}

func TestCodexPatchTextFallbacks(t *testing.T) {
	env := codexEnv(t)
	writePlan(t, env, "docs/plans/x.md", planBad)
	patch := "*** Begin Patch\n*** Add File: docs/plans/x.md\n+# Plan\n*** End Patch\n"
	for name, raw := range map[string]string{
		"command not a string": `{"command":5,"input":` + strconvQuote(patch) + `}`,
		"empty command":        `{"command":"","patch":` + strconvQuote(patch) + `}`,
	} {
		in := Input{ToolName: "apply_patch", Cwd: env.Root, ToolInput: json.RawMessage(raw)}
		out, err := PostToolUse(env, in)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(name, func(t *testing.T) { golden(t, "codex_post_block", out) })
	}
	for name, raw := range map[string]string{"bad json": `[1`, "no field": `{"text":"x"}`, "array": `["x"]`} {
		t.Run(name, func(t *testing.T) {
			nilOut(t)(PostToolUse(env, Input{ToolName: "apply_patch", Cwd: env.Root, ToolInput: json.RawMessage(raw)}))
		})
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestCodexStopTailWithoutNewline(t *testing.T) {
	// A transcript longer than the tail whose tail holds no newline: the
	// tail is one cut line, so nothing is checked and nothing is said.
	p := filepath.Join(t.TempDir(), "one-line.jsonl")
	line := `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` +
		strings.Repeat("x", codexMaxTranscript) + turnBad + `"}]}}`
	if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	in := stopIn(t, turnBad)
	in.TranscriptPath = p
	nilOut(t)(Stop(codexEnv(t), in))
}

func TestCodexTextsUseCLIPath(t *testing.T) {
	// Every Codex text names the CLI by its absolute path (spec 12a).
	t.Setenv("CLAUDE_PLUGIN_ROOT", safeRoot)
	env := codexEnv(t)
	writePlan(t, env, "docs/plans/w3.md", planW3)
	out, err := PostToolUse(env, bashIn(t, env.Root, "printf x > docs/plans/w3.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "(`"+safeRoot+"/bin/paraplane check --suggest` prints it)") ||
		strings.Contains(string(out), "`paraplane ") {
		t.Errorf("post W3: %s", out)
	}
	out, err = PostToolUse(env, bashIn(t, env.Root, "printf x > "+writePlan(t, env, "docs/plans/long.md", planLongW3())))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "run `"+safeRoot+"/bin/paraplane check --suggest <file>` for the shorter layout") {
		t.Errorf("post long W3: %s", out)
	}
	// Plan-derived text is never rewritten: a plan path quoting the bare
	// command stays as it is.
	writePlan(t, env, "docs/plans/`paraplane check x`.md", planBad)
	out, err = PostToolUse(env, bashIn(t, env.Root, "printf x > 'docs/plans/`paraplane check x`.md'"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "docs/plans/`paraplane check x`.md is not ready") || !strings.Contains(string(out), "skill $paraplane:rules.") {
		t.Errorf("quoted path: %s", out)
	}
	stop := codexPayload(t, "codex_stop", map[string]any{
		"transcript_path": nil, "last_assistant_message": "<proposed_plan>\n" + planBad + "</proposed_plan>",
	})
	out, err = Stop(env, stop)
	if err != nil || !strings.Contains(string(out), "skill $paraplane:rules.") {
		t.Errorf("stop: %s, %v", out, err)
	}
}
