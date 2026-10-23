package hooks

import (
	"encoding/json"
	"strings"
)

// Digest is the plan-writing rules summary.
const Digest = "paraplane rules apply to this plan: split the work into tasks, each with an exclusive owns list; group independent tasks into waves (no shared owns, no task reading what another task of its wave owns, at most one task per hot file such as go.mod or lock files); order by depends, not by the original step numbers; end every wave with a gate that runs the full checks. Put it in one ```paraplane block and run `paraplane check <file>`. Full rules: skill paraplane:rules."

// Parts of the execution digest: the rules with the commit schemes, the
// progress rules of each agent and mode (config key progress) and the
// pointer to the full rules. Claude Code's texts say nothing of Codex and
// Codex's nothing of the Claude Code hook.
const (
	execRules = "paraplane execution rules apply to any plan with a ```paraplane block and take precedence over this skill's instructions: dispatch every task of a wave in parallel (one message, one subagent per task, each limited to its owns, its prompt carrying a line Paraplane-Task: <plan path> <task id>); subagents never commit; after the wave run `paraplane audit <plan> <wave>`, then the wave's gate, and fix until both pass; then the coordinator commits the wave with trailers Paraplane-Plan: <plan path> and Paraplane-Wave: <wave id> in the last paragraph of the message, with Co-Authored-By and other trailers and no blank line between them. Only when the project's own rules require another scheme: one commit per stage (audit with every wave of the stage so far, `paraplane audit <plan> W1,W2`, and list them all in that commit, one Paraplane-Wave trailer each), or one commit per task (the coordinator commits each task after its review, one at a time, with trailers Paraplane-Plan: <plan path> and Paraplane-Task: <task id>; the audit and the gate still run after the wave, which needs no commit of its own)."
	// progressClaude is Claude Code's progress rule: the hook records and
	// shows; when a line counts; how to undo a record.
	progressClaude = "Progress: a hook records a task when its subagent ends, before the task's review, and shows the progress to the user by itself, so do not repeat it. A Paraplane-Task line is recorded only as a line of its own whose plan path has no spaces and is a plan file of plan_paths, whose id is a task of that plan, and among the first 16 such lines of the prompt; it counts toward the progress shown only while the plan has no E errors. When a recorded task fails its review, undo the record with `paraplane progress --forget <ids> <plan>`."
	// progressClaudeTask is added for progress: task in Claude Code.
	progressClaudeTask = "the hook shows the progress after each task whose subagent ended, not only after each closed wave."
	progressCodexWave  = "Progress: show the output of `paraplane progress <plan>` to the user after each closed wave (audit and gate passed), passing every closed task not committed yet (of all uncommitted waves): `paraplane progress --done <ids> <plan>`."
	progressCodexTask  = "In Codex also: After each closed task (its subagent returned and its review passed) run `paraplane progress --done <every closed, uncommitted task> <plan>` and show the output."
	rulesRef           = "Full rules: skill paraplane:rules."
)

// ExecDigest is the plan-execution rules summary for Claude Code with the
// default progress mode (wave); skillDigest returns it for an execution
// skill and the hooks replace it with execDigest of the agent and mode.
const ExecDigest = execRules + " " + progressClaude + " " + rulesRef

// execDigest is the execution digest for the agent and the progress mode
// ("wave" or "task").
func execDigest(a Agent, mode string) string {
	switch {
	case a == Codex && mode == "task":
		return execRules + " " + progressCodexWave + " " + progressCodexTask + " " + rulesRef
	case a == Codex:
		return execRules + " " + progressCodexWave + " " + rulesRef
	case mode == "task":
		return execRules + " " + progressClaude + " With progress: task " + progressClaudeTask + " " + rulesRef
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
		digest = execDigest(env.Agent, env.Config.Progress)
	}
	return encodeJSON(hookOutput{Hook: &hookSpecific{EventName: eventOf(in), AdditionalContext: env.Agent.text(digest)}})
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
