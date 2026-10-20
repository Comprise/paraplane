package hooks

import "regexp"

// codexSkillMention is a skill named in a Codex prompt: $name or
// $plugin:name in lowercase, so shell variables such as $PLAN_FILE or
// $plan_file are not skills.
var codexSkillMention = regexp.MustCompile(`\$([a-z0-9-]+(?::[a-z0-9-]+)?)(?:[^A-Za-z0-9_:-]|$)`)

// PromptSubmit is Codex's counterpart of the Skill PreToolUse: Codex has no
// Skill tool, the user names skills in the prompt as $plugin:skill. A
// mentioned execution skill adds ExecDigest, else a planning skill adds
// Digest (spec section 12a). Claude Code does not use this hook.
func PromptSubmit(env Env, in Input) ([]byte, error) {
	if !env.Config.Enabled || env.Agent != Codex {
		return nil, nil
	}
	digest := ""
	for _, m := range codexSkillMention.FindAllStringSubmatch(in.Prompt, -1) {
		switch d := skillDigest(m[1]); d {
		case ExecDigest:
			digest = d
		case Digest:
			if digest == "" {
				digest = d
			}
		}
		if digest == ExecDigest {
			break
		}
	}
	if digest == "" {
		return nil, nil
	}
	return encodeJSON(hookOutput{Hook: &hookSpecific{EventName: "UserPromptSubmit", AdditionalContext: env.Agent.text(digest)}})
}
