package hooks

import (
	"encoding/json"
	"strings"
)

// Digest is the plan-writing rules summary.
const Digest = "paraplane rules apply to this plan: split the work into tasks, each with an exclusive owns list; group independent tasks into waves (no shared owns, no task reading what another task of its wave owns, at most one task per hot file such as go.mod or lock files); order by depends, not by the original step numbers; end every wave with a gate that runs the full checks. Put it in one ```paraplane block and run `paraplane check <file>`. Full rules: skill paraplane:rules."

// Parts of the execution digest: the rules, the progress rule of each mode
// of the config key progress, and the pointer to the full rules.
const (
	execRules    = "paraplane execution rules apply to any plan with a ```paraplane block and take precedence over this skill's instructions: dispatch every task of a wave in parallel (one message, one subagent per task, each limited to its owns); subagents never commit; after the wave run `paraplane audit <plan> <wave>`, then the wave's gate, and fix until both pass; then the coordinator makes one commit for the wave with trailers Paraplane-Plan: <plan path> and Paraplane-Wave: <wave id> in the last paragraph of the message, with Co-Authored-By and other trailers and no blank line between them; if the project commits once per stage, that commit lists every wave of the stage, one Paraplane-Wave trailer each."
	progressWave = "Show the output of `paraplane progress <plan>` to the user after each closed wave (audit and gate passed); while the wave is not committed yet, pass all its tasks: `paraplane progress --done <ids> <plan>`."
	progressTask = "After each closed task (its subagent returned and its review passed) run `paraplane progress --done <closed, uncommitted tasks of the wave> <plan>` and show the output."
	rulesRef     = "Full rules: skill paraplane:rules."
)

// ExecDigest is the plan-execution rules summary with the default progress
// mode (wave); skillDigest returns it for an execution skill and the hooks
// replace it with execDigest of the configured mode.
const ExecDigest = execRules + " " + progressWave + " " + rulesRef

// execDigest is the execution digest for the progress mode ("wave" or
// "task").
func execDigest(mode string) string {
	if mode == "task" {
		return execRules + " " + progressWave + " " + progressTask + " " + rulesRef
	}
	return ExecDigest
}

// skill adds a rules digest to another plugin's planning or execution skill.
func skill(env Env, in Input) ([]byte, error) {
	if !env.Config.Enabled {
		return nil, nil
	}
	digest := skillDigest(skillName(in))
	if digest == "" {
		return nil, nil
	}
	if digest == ExecDigest {
		digest = execDigest(env.Config.Progress)
	}
	return encodeJSON(hookOutput{Hook: &hookSpecific{EventName: "PreToolUse", AdditionalContext: env.Agent.text(digest)}})
}

// skillDigest is the digest for another plugin's skill name: ExecDigest for
// an execution skill, Digest for a planning one, "" otherwise.
func skillDigest(name string) string {
	if name == "" || strings.HasPrefix(name, "paraplane:") {
		return ""
	}
	low := strings.ToLower(name)
	switch {
	case strings.Contains(low, "execut") || strings.Contains(low, "subagent-driven"):
		return ExecDigest
	case strings.Contains(low, "plan"):
		return Digest
	}
	return ""
}

// skillName reads tool_input.skill, then name, then command (the field name is
// not documented); the first non-empty string wins.
func skillName(in Input) string {
	var v map[string]any
	if json.Unmarshal(in.ToolInput, &v) != nil {
		return ""
	}
	for _, k := range []string{"skill", "name", "command"} {
		if s, _ := v[k].(string); s != "" {
			return s
		}
	}
	return ""
}
