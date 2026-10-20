package hooks

import (
	"encoding/json"
	"path/filepath"

	"paraplane/internal/rules"
)

// PreToolUse dispatches by tool: ExitPlanMode checks the plan, Skill adds a
// rules digest, Bash approves the plugin's own CLI; other tools get nothing.
func PreToolUse(env Env, in Input) ([]byte, error) {
	if !env.Config.Enabled {
		return nil, nil
	}
	switch in.ToolName {
	case "ExitPlanMode":
		return exitPlan(env, in)
	case "Skill":
		return skill(env, in)
	case "Bash":
		return bash(env, in)
	}
	return nil, nil
}

// exitPlan denies leaving plan mode while the plan has errors (E0
// included); warnings only add context. In mode warn errors add context too.
func exitPlan(env Env, in Input) ([]byte, error) {
	md, err := exitPlanText(env, in)
	if md == nil || err != nil {
		return nil, err
	}
	r := rules.Check(md, Options(env))
	switch {
	case r.Errors() > 0:
		text := "paraplane: the plan is not ready for parallel execution. Fix these and call ExitPlanMode again:\n" +
			postFindings(r.Findings) + "\nFormat and rules: skill paraplane:rules."
		if env.Config.Mode == "warn" {
			return postContext("PreToolUse", text)
		}
		return encodeJSON(hookOutput{Hook: &hookSpecific{
			EventName:                "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: text,
		}})
	case r.Warnings() > 0:
		return postContext("PreToolUse", postWarnings(r))
	}
	return nil, nil
}

// exitPlanText is tool_input.plan, or else the file tool_input.planFilePath
// (relative to Root when not absolute); nil when there is neither.
func exitPlanText(env Env, in Input) ([]byte, error) {
	var v struct {
		Plan         any `json:"plan"`
		PlanFilePath any `json:"planFilePath"`
	}
	if json.Unmarshal(in.ToolInput, &v) != nil {
		return nil, nil
	}
	if s, _ := v.Plan.(string); s != "" {
		return []byte(s), nil
	}
	name, _ := v.PlanFilePath.(string)
	if name == "" {
		return nil, nil
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(env.Root, name)
	}
	data, ok, err := postReadFile(name)
	if !ok || len(data) == 0 {
		return nil, err
	}
	return data, nil
}
