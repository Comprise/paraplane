package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// bashOwnCLI matches a single plain invocation of the plugin's own CLI: no
// shell metacharacters, quotes, variables or extra whitespace can appear.
var bashOwnCLI = regexp.MustCompile(`^paraplane (version|check( --suggest)? [A-Za-z0-9_.][A-Za-z0-9_./-]*|progress( --done [A-Za-z][A-Za-z0-9_.-]*(,[A-Za-z][A-Za-z0-9_.-]*)*)? [A-Za-z0-9_.][A-Za-z0-9_./-]*|audit [A-Za-z0-9_.][A-Za-z0-9_./-]* [A-Za-z][A-Za-z0-9_.-]*)$`)

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
// own CLI that the hook may approve. Codex's shell has no plugin bin/ on
// PATH, so for Codex only the absolute Codex.CLI() form is approved (it names
// the plugin's binary itself) and the bare name never is, nor anything when
// the plugin root is unset or unsafe. For Claude the bare name is approved
// unless PATH shadows the plugin's binary.
func bashApprovable(env Env, in Input) bool {
	if in.ToolName != "Bash" {
		return false
	}
	cmd := bashCommand(in)
	if env.Agent == Codex {
		abs := Codex.CLI()
		rest, ok := strings.CutPrefix(cmd, abs+" ")
		return abs != "paraplane" && ok && bashAllowed("paraplane "+rest)
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
// binary, so the approval would run someone else's program. Without an
// absolute CLAUDE_PLUGIN_ROOT there is nothing to compare with, so it fails
// closed.
// Best effort: the hook sees Claude Code's PATH, which is close to the Bash
// tool's.
func bashShadowed() bool {
	pluginRoot := os.Getenv("CLAUDE_PLUGIN_ROOT")
	if !filepath.IsAbs(pluginRoot) {
		return true
	}
	found, err := exec.LookPath("paraplane")
	if errors.Is(err, exec.ErrDot) {
		return true // found through a relative PATH entry such as "."
	}
	if err != nil {
		// Not on our PATH, so the Bash tool, whose PATH is ours with the
		// plugin bin/ directories appended, finds the plugin's binary.
		// Failing closed here would refuse every approval, since the
		// hook's PATH has no plugin bin/.
		return false
	}
	return !ownBinary(found, pluginRoot, runtime.GOOS, filepath.EvalSymlinks)
}

// ownBinary reports whether found, the paraplane that PATH names, is the
// plugin's binary pluginRoot/bin/paraplane; on Windows bin/paraplane.exe
// counts too (PATHEXT finds the .exe) and paths compare case-insensitively.
// resolve resolves symlinks; a path it cannot resolve is not the binary.
// It takes goos and resolve as arguments so tests run it on every OS.
func ownBinary(found, pluginRoot, goos string, resolve func(string) (string, error)) bool {
	got, err := resolve(found)
	if err != nil {
		return false
	}
	names := []string{"paraplane"}
	if goos == "windows" {
		names = append(names, "paraplane.exe")
	}
	for _, n := range names {
		want, err := resolve(filepath.Join(pluginRoot, "bin", n))
		if err != nil {
			continue
		}
		if got == want || (goos == "windows" && strings.EqualFold(got, want)) {
			return true
		}
	}
	return false
}
