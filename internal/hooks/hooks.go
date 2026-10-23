// Package hooks implements the Claude Code and Codex hook handlers.
package hooks

import (
	"bytes"
	"encoding/json"

	"paraplane/internal/config"
)

// Input is the JSON payload Claude Code or Codex passes to a hook on stdin.
type Input struct {
	SessionID      string          `json:"session_id"`
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	PermissionMode string          `json:"permission_mode"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolResponse   json.RawMessage `json:"tool_response"`

	// Codex fields (spec section 12a.2).
	TranscriptPath       string `json:"transcript_path"`
	TurnID               string `json:"turn_id"`
	LastAssistantMessage string `json:"last_assistant_message"` // null in Plan Mode
	Prompt               string `json:"prompt"`
	StopHookActive       bool   `json:"stop_hook_active"`
}

// Env is the resolved project state a handler works with.
type Env struct {
	Root      string
	Config    config.Config
	ConfigErr error // read error or config.Problems
	Agent     Agent // the agent running the hook; zero means Claude
}

// hookOutput is the shared response form of every hook.
type hookOutput struct {
	SystemMessage string        `json:"systemMessage,omitempty"`
	Decision      string        `json:"decision,omitempty"` // PostToolUse: "block"
	Reason        string        `json:"reason,omitempty"`
	Hook          *hookSpecific `json:"hookSpecificOutput,omitempty"`
}

type hookSpecific struct {
	EventName                string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

// encodeJSON marshals v without HTML escaping and without a trailing newline.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
