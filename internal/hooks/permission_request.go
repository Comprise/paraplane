package hooks

type permissionRequestOutput struct {
	Hook struct {
		EventName string `json:"hookEventName"`
		Decision  struct {
			Behavior string `json:"behavior"`
		} `json:"decision"`
	} `json:"hookSpecificOutput"`
}

// PermissionRequest is Codex's counterpart of the Bash PreToolUse: a Codex
// PreToolUse allow rewrites the command, so the plugin's own read-only CLI
// calls are approved here, when Codex is about to ask. Anything else yields
// no output and keeps the normal approval prompt. Claude Code does not use
// this hook.
func PermissionRequest(env Env, in Input) ([]byte, error) {
	if !env.Config.Enabled || env.Agent != Codex || !bashApprovable(env, in) {
		return nil, nil
	}
	var out permissionRequestOutput
	out.Hook.EventName = "PermissionRequest"
	out.Hook.Decision.Behavior = "allow"
	return encodeJSON(out)
}
