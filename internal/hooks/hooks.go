// Package hooks implements the Claude Code and Codex hook handlers.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"paraplane/internal/config"
)

// hookBudget is the time one hook process has for all its git calls and
// plan checks; the host kills a hook after 10 s.
const hookBudget = 7 * time.Second

// Input is the JSON payload Claude Code or Codex passes to a hook on stdin.
type Input struct {
	SessionID      string          `json:"session_id"`
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	PermissionMode string          `json:"permission_mode"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolResponse   json.RawMessage `json:"tool_response"`

	// SubagentStop: the finished subagent's own transcript.
	AgentTranscriptPath string `json:"agent_transcript_path"`

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
	// Ctx is the budget of the hook process: every git call and plan check
	// takes what is left of it. runHook sets one per process; a handler
	// called with nil makes its own of hookBudget.
	Ctx context.Context
}

// begin prepares a handler call: env gets a budget of hookBudget when it has
// none (done releases it), and a payload without hook_event_name gets the
// handler's own event, so every answer names the event it answers.
func begin(env Env, in Input, event string) (Env, Input, context.CancelFunc) {
	if in.HookEventName == "" {
		in.HookEventName = event
	}
	if env.Ctx != nil {
		return env, in, func() {}
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookBudget)
	env.Ctx = ctx
	return env, in, cancel
}

// ctx is env.Ctx, or a context without a limit for the internal functions
// tests call without a handler.
func (env Env) ctx() context.Context {
	if env.Ctx == nil {
		return context.Background()
	}
	return env.Ctx
}

// eventOf is the event an answer to in names in hookSpecificOutput: the
// event of the payload itself (begin fills it in when missing).
func eventOf(in Input) string { return in.HookEventName }

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
	// UpdatedInput replaces the tool input of an approved Bash call (the
	// command with the absolute path of the plugin binary).
	UpdatedInput map[string]any `json:"updatedInput,omitempty"`
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
