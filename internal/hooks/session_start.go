package hooks

import "paraplane/internal/textx"

// SessionContext is the one-line reminder added to every session.
const SessionContext = "paraplane: every implementation plan must contain one ```paraplane block (waves of tasks with owns, reads, depends, verify, and a gate per wave) and pass `paraplane check <file>`. Load skill paraplane:rules before writing or changing a plan."

// SessionStart adds the plan-rules reminder; a config problem is also shown to
// the user. Nothing is said when the plugin is disabled.
func SessionStart(env Env, in Input) ([]byte, error) {
	if !env.Config.Enabled {
		return nil, nil
	}
	out := hookOutput{Hook: &hookSpecific{EventName: "SessionStart", AdditionalContext: SessionContext}}
	if env.ConfigErr != nil {
		out.SystemMessage = "paraplane: " + textx.Line(env.ConfigErr.Error(), 300) + " (defaults are used for those settings)."
	}
	return encodeJSON(out)
}
