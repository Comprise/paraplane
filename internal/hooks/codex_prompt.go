package hooks

import "regexp"

// codexSkillMention is a skill named in a Codex prompt: $name or
// $plugin:name in lowercase. The byte after it must not continue a name
// (codexMentionEnd), so shell variables such as $PLAN_FILE or $plan_file are
// not skills. That byte is checked outside the regexp and not consumed, so
// "$a$b" yields both mentions.
var codexSkillMention = regexp.MustCompile(`\$([a-z0-9-]+(?::[a-z0-9-]+)?)`)

// codexMentions are the skill names mentioned in prompt, in order.
func codexMentions(prompt string) []string {
	var names []string
	for _, m := range codexSkillMention.FindAllStringSubmatchIndex(prompt, -1) {
		if end := m[1]; end < len(prompt) && codexMentionEnd(prompt[end]) {
			continue
		}
		names = append(names, prompt[m[2]:m[3]])
	}
	return names
}

// codexMentionEnd reports whether c would continue a skill or variable name.
func codexMentionEnd(c byte) bool {
	return c == ':' || c == '-' || isWordByte(c)
}

// PromptSubmit is Codex's counterpart of the Skill PreToolUse: Codex has no
// Skill tool, the user names skills in the prompt as $plugin:skill. A
// mentioned execution skill adds ExecDigest, else a planning skill adds
// Digest (spec section 12a). Claude Code does not use this hook.
func PromptSubmit(env Env, in Input) ([]byte, error) {
	if !env.Config.Enabled || env.Agent != Codex {
		return nil, nil
	}
	digest := ""
	for _, name := range codexMentions(in.Prompt) {
		switch d := skillDigest(name); d {
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
