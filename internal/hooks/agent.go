package hooks

import (
	"os"
	"path"
	"regexp"
	"strings"
)

// Agent is the coding agent that runs the hooks. Its zero value means Claude.
type Agent string

const (
	Claude Agent = "claude"
	Codex  Agent = "codex"
)

// ParseAgent maps a --agent value to an Agent (exact case); ok is false for
// anything else.
func ParseAgent(s string) (Agent, bool) {
	switch Agent(s) {
	case Claude:
		return Claude, true
	case Codex:
		return Codex, true
	}
	return "", false
}

// Skill is how texts for this agent name a plugin skill: paraplane:x in
// Claude Code, $paraplane:x in Codex.
func (a Agent) Skill(name string) string {
	if a == Codex {
		return "$paraplane:" + name
	}
	return "paraplane:" + name
}

// pluginRootSafe limits the plugin root put into suggested commands: the text
// is pasted into a shell, so nothing but plain path characters may appear.
var pluginRootSafe = regexp.MustCompile(`^/[A-Za-z0-9_./-]*$`)

// CLI is how texts for this agent name the plugin binary. Codex's shell tool
// has no plugin bin/ on PATH, so it gets the absolute path under
// CLAUDE_PLUGIN_ROOT; an unset or unsafe root falls back to the bare name.
// The root is a POSIX path (pluginRootSafe), so it is joined with "/" on
// every OS: the text goes to a shell, not to the file system API.
func (a Agent) CLI() string {
	if a == Codex {
		if root := os.Getenv("CLAUDE_PLUGIN_ROOT"); pluginRootSafe.MatchString(root) {
			return path.Join(root, "bin", "paraplane")
		}
	}
	return "paraplane"
}

// text adapts a hook text written for Claude Code to the agent: the quoted
// commands `paraplane check|audit|progress …` use CLI() and
// "skill paraplane:x" uses Skill(x). For Claude the text is returned unchanged.
func (a Agent) text(s string) string {
	if a != Codex {
		return s
	}
	cli := "`" + a.CLI() + " "
	return strings.NewReplacer(
		"`paraplane check ", cli+"check ",
		"`paraplane audit ", cli+"audit ",
		"`paraplane progress ", cli+"progress ",
		"skill paraplane:", "skill "+a.Skill(""),
	).Replace(s)
}
