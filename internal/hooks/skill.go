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
	execRules    = "paraplane execution rules apply to any plan with a ```paraplane block and take precedence over this skill's instructions: dispatch every task of a wave in parallel (one message, one subagent per task, each limited to its owns, its prompt carrying a line Paraplane-Task: <plan path> <task id>); subagents never commit; after the wave run `paraplane audit <plan> <wave>`, then the wave's gate, and fix until both pass; then the coordinator commits the wave with trailers Paraplane-Plan: <plan path> and Paraplane-Wave: <wave id> in the last paragraph of the message, with Co-Authored-By and other trailers and no blank line between them. Only when the project's own rules require one commit per stage, commit once per stage instead: audit with every wave of the stage so far (`paraplane audit <plan> W1,W2`) and list them all in that commit, one Paraplane-Wave trailer each."
	progressWave = "Progress: in Claude Code a hook shows it to the user by itself, so do not repeat it; in Codex show the output of `paraplane progress <plan>` to the user after each closed wave (audit and gate passed), passing every closed task not committed yet (of all uncommitted waves): `paraplane progress --done <ids> <plan>`."
	progressTask = "In Codex also: After each closed task (its subagent returned and its review passed) run `paraplane progress --done <every closed, uncommitted task> <plan>` and show the output."
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
