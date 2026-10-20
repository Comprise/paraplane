package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"paraplane/internal/config"
)

// golden compares got with testdata/<name>.golden.json (written by hand),
// ignoring CRLF line endings and a trailing newline of the file.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", name+".golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	// A checkout with CRLF line endings must not fail the comparison.
	if string(got) != strings.TrimRight(strings.ReplaceAll(string(want), "\r\n", "\n"), "\n") {
		t.Errorf("%s mismatch\n got: %s\nwant: %s", name, got, want)
	}
}

func testEnv(t *testing.T) Env {
	t.Helper()
	return Env{Root: t.TempDir(), Config: config.Default()}
}

func toolInput(t *testing.T, tool string, fields map[string]any) Input {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return Input{HookEventName: "PreToolUse", ToolName: tool, ToolInput: raw}
}

func TestSessionStart(t *testing.T) {
	out, err := SessionStart(testEnv(t), Input{HookEventName: "SessionStart"})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "base_session_start", out)
}

func TestSessionStartConfigProblem(t *testing.T) {
	env := testEnv(t)
	env.ConfigErr = errors.New("config.yaml: ignored mode (bad)")
	out, err := SessionStart(env, Input{})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "base_session_config", out)
}

func TestSessionStartDisabled(t *testing.T) {
	env := testEnv(t)
	env.Config.Enabled = false
	env.ConfigErr = errors.New("x")
	out, err := SessionStart(env, Input{})
	if err != nil || out != nil {
		t.Fatalf("got %q, %v; want nil, nil", out, err)
	}
}

func TestSkillDigest(t *testing.T) {
	env := testEnv(t)
	cases := []struct {
		name   string
		fields map[string]any
		golden string // "" means nil output
	}{
		{"writing-plans", map[string]any{"skill": "superpowers:writing-plans"}, "base_skill_digest"},
		{"executing-plans", map[string]any{"skill": "superpowers:executing-plans"}, "base_skill_exec"},
		{"subagent-driven", map[string]any{"skill": "superpowers:subagent-driven-development"}, "base_skill_exec"},
		{"name field", map[string]any{"name": "superpowers:writing-plans"}, "base_skill_digest"},
		{"command field", map[string]any{"command": "Writing-Plans"}, "base_skill_digest"},
		{"skill wins over name", map[string]any{"skill": "superpowers:brainstorming", "name": "x-plan"}, ""},
		{"own skill", map[string]any{"skill": "paraplane:create"}, ""},
		{"own skill with plan", map[string]any{"skill": "paraplane:plan-execute"}, ""},
		{"review", map[string]any{"skill": "review"}, ""},
		{"no fields", map[string]any{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := skill(env, toolInput(t, "Skill", c.fields))
			if err != nil {
				t.Fatal(err)
			}
			if c.golden == "" {
				if out != nil {
					t.Fatalf("want nil, got %s", out)
				}
				return
			}
			golden(t, c.golden, out)
		})
	}
	t.Run("disabled", func(t *testing.T) {
		e := testEnv(t)
		e.Config.Enabled = false
		out, _ := skill(e, toolInput(t, "Skill", map[string]any{"skill": "superpowers:writing-plans"}))
		if out != nil {
			t.Fatalf("want nil, got %s", out)
		}
	})
	t.Run("bad json", func(t *testing.T) {
		out, err := skill(env, Input{ToolName: "Skill", ToolInput: json.RawMessage(`[1`)})
		if out != nil || err != nil {
			t.Fatalf("got %q, %v", out, err)
		}
	})
}

// noShadow sets a plugin root and a PATH without paraplane, so the shadow
// check has nothing to compare and approves.
func noShadow(t *testing.T) {
	t.Helper()
	t.Setenv("CLAUDE_PLUGIN_ROOT", t.TempDir())
	t.Setenv("PATH", t.TempDir())
}

func TestBashAllow(t *testing.T) {
	noShadow(t)
	env := testEnv(t)
	allow := []string{
		"paraplane version",
		"paraplane check docs/plans/a.md",
		"paraplane check --suggest a.md",
		"paraplane audit docs/plans/a.md W2",
		"paraplane progress docs/plans/a.md",
		"paraplane check .paraplane/plans/a.md",
	}
	deny := []string{
		"paraplane check ../x",
		"paraplane check a/../x",
		"paraplane check /etc/passwd",
		"paraplane check a; rm -rf /",
		"paraplane check a b",
		"paraplane check",
		"paraplane check  a.md",
		"paraplane check a.md ",
		"paraplane check $(id)",
		"paraplane check --suggest",
		"paraplane audit a.md",
		"paraplane audit a.md 2",
		"paraplane audit a.md W2 extra",
		"paraplane version x",
		"paraplane progress a.md\n",
		"paraplane check a.md\nrm -rf /",
		"paraplane check -x",
		"paraplane suggest a.md",
		"/usr/bin/paraplane check a.md",
		"",
	}
	for _, cmd := range allow {
		out, err := bash(env, toolInput(t, "Bash", map[string]any{"command": cmd}))
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "base_bash_allow", out)
	}
	for _, cmd := range deny {
		out, err := bash(env, toolInput(t, "Bash", map[string]any{"command": cmd}))
		if err != nil || out != nil {
			t.Errorf("%q: got %q, %v; want nil", cmd, out, err)
		}
	}
	t.Run("not a string", func(t *testing.T) {
		out, _ := bash(env, toolInput(t, "Bash", map[string]any{"command": []string{"paraplane", "version"}}))
		if out != nil {
			t.Fatalf("got %s", out)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		e := testEnv(t)
		e.Config.Enabled = false
		out, _ := bash(e, toolInput(t, "Bash", map[string]any{"command": "paraplane version"}))
		if out != nil {
			t.Fatalf("got %s", out)
		}
	})
}

func fakeBin(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "paraplane"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestBashAllowPathShadow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH lookup of extensionless fake binary")
	}
	env := testEnv(t)
	in := toolInput(t, "Bash", map[string]any{"command": "paraplane version"})
	plugin := t.TempDir()
	fakeBin(t, filepath.Join(plugin, "bin"))
	t.Setenv("CLAUDE_PLUGIN_ROOT", plugin)

	t.Run("own binary first", func(t *testing.T) {
		t.Setenv("PATH", filepath.Join(plugin, "bin"))
		out, _ := bash(env, in)
		golden(t, "base_bash_allow", out)
	})
	t.Run("shadowed", func(t *testing.T) {
		other := filepath.Join(t.TempDir(), "bin")
		fakeBin(t, other)
		t.Setenv("PATH", other+string(os.PathListSeparator)+filepath.Join(plugin, "bin"))
		if out, _ := bash(env, in); out != nil {
			t.Fatalf("got %s", out)
		}
	})
	t.Run("not on PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		out, _ := bash(env, in)
		golden(t, "base_bash_allow", out)
	})
	for _, root := range []string{"", "relative/plugin"} {
		t.Run("plugin root "+root, func(t *testing.T) {
			// Without an absolute CLAUDE_PLUGIN_ROOT the check cannot tell:
			// fail closed.
			t.Setenv("CLAUDE_PLUGIN_ROOT", root)
			for _, p := range []string{filepath.Join(plugin, "bin"), t.TempDir()} {
				t.Setenv("PATH", p)
				if out, _ := bash(env, in); out != nil {
					t.Fatalf("PATH %s: got %s", p, out)
				}
			}
		})
	}
}

func TestBashCodexBareName(t *testing.T) {
	// Codex's shell has no plugin bin/ on PATH: the bare name is never its
	// own binary, so it is not approved even where Claude's would be.
	noShadow(t)
	env := testEnv(t)
	env.Agent = Codex
	nilOut(t)(bash(env, toolInput(t, "Bash", map[string]any{"command": "paraplane version"})))
	t.Setenv("CLAUDE_PLUGIN_ROOT", safeRoot)
	nilOut(t)(bash(env, toolInput(t, "Bash", map[string]any{"command": "paraplane version"})))
	out, err := bash(env, toolInput(t, "Bash", map[string]any{"command": safeRoot + "/bin/paraplane version"}))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "base_bash_allow", out)
}

func TestGoldenCRLF(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("testdata", "crlf.golden.json"), []byte("{\"a\":\"x\\ny\"}\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	golden(t, "crlf", []byte(`{"a":"x\ny"}`))
}
