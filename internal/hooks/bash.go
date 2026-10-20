package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// bashOwnCLI matches a single plain invocation of the plugin's own CLI: no
// shell metacharacters, quotes, variables or extra whitespace can appear.
var bashOwnCLI = regexp.MustCompile(`^paraplane (version|check( --suggest)? [A-Za-z0-9_.][A-Za-z0-9_./-]*|progress [A-Za-z0-9_.][A-Za-z0-9_./-]*|audit [A-Za-z0-9_.][A-Za-z0-9_./-]* [A-Za-z][A-Za-z0-9_.-]*)$`)

// bash auto-approves the plugin's own read-only CLI calls; anything else keeps
// the normal permission flow.
func bash(env Env, in Input) ([]byte, error) {
	if !env.Config.Enabled || !bashApprovable(env, in) {
		return nil, nil
	}
	return encodeJSON(hookOutput{Hook: &hookSpecific{
		EventName:                "PreToolUse",
		PermissionDecision:       "allow",
		PermissionDecisionReason: "paraplane CLI",
	}})
}

// bashApprovable reports whether the tool call is a Bash run of the plugin's
// own CLI that the hook may approve. Codex runs it by absolute path (its shell
// has no plugin bin/ on PATH); that path points at the plugin's binary itself,
// so PATH shadowing does not matter for it.
func bashApprovable(env Env, in Input) bool {
	if in.ToolName != "Bash" {
		return false
	}
	cmd := bashCommand(in)
	if env.Agent == Codex {
		if abs := Codex.CLI(); abs != "paraplane" {
			if rest, ok := strings.CutPrefix(cmd, abs+" "); ok {
				return bashAllowed("paraplane " + rest)
			}
		}
	}
	return bashAllowed(cmd) && !bashShadowed()
}

// bashCommand is tool_input.command, or "" when it is not a string.
func bashCommand(in Input) string {
	var v struct {
		Command any `json:"command"`
	}
	if json.Unmarshal(in.ToolInput, &v) != nil {
		return ""
	}
	cmd, _ := v.Command.(string)
	return cmd
}

// bashAllowed matches the raw command exactly, without trimming.
func bashAllowed(cmd string) bool {
	if !bashOwnCLI.MatchString(cmd) {
		return false
	}
	return !strings.Contains(cmd, "..")
}

// bashShadowed reports whether "paraplane" on PATH is not the plugin's own
// binary, so the approval would run someone else's program. Best effort: the
// hook sees Claude Code's PATH, which is close to the Bash tool's.
func bashShadowed() bool {
	pluginRoot := os.Getenv("CLAUDE_PLUGIN_ROOT")
	if pluginRoot == "" {
		return false
	}
	found, err := exec.LookPath("paraplane")
	if errors.Is(err, exec.ErrDot) {
		return true // found through a relative PATH entry such as "."
	}
	if err != nil {
		return false // not on our PATH; nothing to compare
	}
	got, err1 := filepath.EvalSymlinks(found)
	want, err2 := filepath.EvalSymlinks(filepath.Join(pluginRoot, "bin", "paraplane"))
	return err1 != nil || err2 != nil || got != want
}
