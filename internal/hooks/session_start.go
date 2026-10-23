package hooks

import "paraplane/internal/textx"

// SessionContext is the one-line reminder added to every session.
const SessionContext = "paraplane: every implementation plan must contain one ```paraplane block (waves of tasks with owns, reads, depends, verify, and a gate per wave) and pass `paraplane check <file>`. Load skill paraplane:rules before writing or changing a plan."

// sessionTask is added to SessionContext with progress: task, so the mode
// reaches a coordinator that loads no execution skill.
func sessionTask(a Agent) string {
	if a == Codex {
		return " This project sets progress: task. " + progressCodexTask
	}
	return " This project sets progress: task: " + progressClaudeTask + " " + progressClaude
}

// SessionStart adds the plan-rules reminder; a config problem is also shown to
// the user. Nothing is said when the plugin is disabled.
func SessionStart(env Env, in Input) ([]byte, error) {
	if !env.Config.Enabled {
		return nil, nil
	}
	_, in, done := begin(env, in, "SessionStart")
	defer done()
	text := SessionContext
	if env.Config.Progress == "task" {
		text += sessionTask(env.Agent)
	}
	out := hookOutput{Hook: &hookSpecific{EventName: eventOf(in), AdditionalContext: env.Agent.text(text)}}
	if env.ConfigErr != nil {
		out.SystemMessage = "paraplane: " + textx.Line(env.ConfigErr.Error(), maxPath) + " (defaults are used for those settings)."
	}
	return encodeJSON(out)
}
