package hooks

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"paraplane/internal/rules"
)

// codexMaxTranscript is how much of the end of a Codex transcript Stop reads.
const codexMaxTranscript = 4 << 20

// codexUnknownTranscript is shown to the user when the transcript names this
// turn but has no record Stop understands: the undocumented format changed
// (spec 12a.2).
const codexUnknownTranscript = "paraplane: Codex transcript format not recognized; plan checks in Plan Mode are off (update paraplane)"

// Tags around a plan in a Codex Plan Mode answer.
const (
	codexPlanOpen  = "<proposed_plan>"
	codexPlanClose = "</proposed_plan>"
)

// Stop checks the plan Codex proposed in Plan Mode during this turn. Codex
// has no ExitPlanMode tool and sends last_assistant_message as null in Plan
// Mode, so the plan is read from the transcript (spec section 12a.2). A plan
// with errors continues the turn with decision:block, once per turn
// (stop_hook_active); in mode warn the text goes to the user instead.
// Claude Code does not use this hook.
func Stop(env Env, in Input) ([]byte, error) {
	if !env.Config.Enabled || env.Agent != Codex || in.StopHookActive {
		return nil, nil
	}
	md, ok := codexTagPlan(in.LastAssistantMessage)
	if !ok {
		tail, ok, err := codexReadTail(in.TranscriptPath)
		if !ok {
			return nil, err
		}
		plan, known := codexTranscriptPlan(tail, in.TurnID)
		if !known {
			// Only a tail that already holds this turn says the format changed;
			// an empty or not yet flushed transcript says nothing.
			if in.TurnID != "" && bytes.Contains(tail, []byte(in.TurnID)) {
				return encodeJSON(hookOutput{SystemMessage: codexUnknownTranscript})
			}
			return nil, nil
		}
		if md, ok = plan, plan != ""; !ok {
			return nil, nil
		}
	}
	if len(md) > postMaxFile {
		return nil, nil
	}
	r := rules.Check([]byte(md), Options(env))
	if r.Errors() == 0 {
		return nil, nil
	}
	reason := "paraplane: the proposed plan is not ready for parallel execution. Fix these and propose the revised plan again:\n" +
		postFindings(r.Findings) + "\nFormat and rules: skill " + env.Agent.Skill("rules") + "."
	if env.Config.Mode == "warn" {
		return encodeJSON(hookOutput{SystemMessage: reason})
	}
	return encodeJSON(hookOutput{Decision: "block", Reason: reason})
}

// codexTagPlan is the text inside the last complete <proposed_plan> block
// of s; ok is false when there is none.
func codexTagPlan(s string) (string, bool) {
	end := strings.LastIndex(s, codexPlanClose)
	if end < 0 {
		return "", false
	}
	start := strings.LastIndex(s[:end], codexPlanOpen)
	if start < 0 {
		return "", false
	}
	return strings.TrimPrefix(s[start+len(codexPlanOpen):end], "\n"), true
}

// codexReadTail reads at most the last codexMaxTranscript bytes of the
// transcript; a cut first line is dropped. ok is false when name is not an
// absolute path to a regular file: the type is checked with Lstat before
// opening, so a symlink or a FIFO is never opened.
func codexReadTail(name string) (data []byte, ok bool, err error) {
	if name == "" || !filepath.IsAbs(name) {
		return nil, false, nil
	}
	fi, err := os.Lstat(name)
	if err != nil || !fi.Mode().IsRegular() {
		if err != nil && !os.IsNotExist(err) {
			return nil, false, err
		}
		return nil, false, nil
	}
	f, err := os.Open(name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(fi, opened) {
		return nil, false, nil
	}
	off := opened.Size() - codexMaxTranscript
	if off > 0 {
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			return nil, false, err
		}
	}
	data, err = io.ReadAll(io.LimitReader(f, codexMaxTranscript))
	if err != nil {
		return nil, false, err
	}
	if off > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return nil, true, nil
		}
		data = data[i+1:]
	}
	return data, true, nil
}

// codexRecord is the part of a Codex transcript line Stop reads.
type codexRecord struct {
	Type    string `json:"type"`
	Payload struct {
		Type        string          `json:"type"`
		Role        string          `json:"role"`
		TurnID      string          `json:"turn_id"`
		Content     json.RawMessage `json:"content"`
		Passthrough struct {
			TurnID string `json:"turn_id"`
		} `json:"internal_chat_message_metadata_passthrough"`
		Item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
	} `json:"payload"`
	Metadata struct {
		RetainedSource struct {
			ID struct {
				TurnID string `json:"turn_id"`
			} `json:"id"`
		} `json:"retained_source"`
	} `json:"metadata"`
}

// codexTranscriptPlan is the last plan the assistant proposed in turn: the
// <proposed_plan> block of an assistant message, or an item_completed Plan
// item. A record belongs to the turn its own turn_id names, else to the turn
// of the last task_started or turn_context record. known is false when no
// line is a response_item message, i.e. the format is not the one paraplane
// was written for.
func codexTranscriptPlan(tail []byte, turn string) (plan string, known bool) {
	current := ""
	for _, line := range bytes.Split(tail, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r codexRecord
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		p := &r.Payload
		switch {
		case r.Type == "turn_context" || (r.Type == "event_msg" && p.Type == "task_started"):
			if p.TurnID != "" {
				current = p.TurnID
			}
		case r.Type == "response_item" && p.Type == "message":
			known = true
			id := firstNonEmpty(p.Passthrough.TurnID, r.Metadata.RetainedSource.ID.TurnID, current)
			if p.Role != "assistant" || turn == "" || id != turn {
				continue
			}
			if s, ok := codexTagPlan(codexMessageText(p.Content)); ok {
				plan = s
			}
		case r.Type == "event_msg" && p.Type == "item_completed" && p.Item.Type == "Plan":
			if turn != "" && firstNonEmpty(p.TurnID, current) == turn && p.Item.Text != "" {
				plan = p.Item.Text
			}
		}
	}
	return plan, known
}

// codexMessageText joins the output_text parts of a message content list.
func codexMessageText(raw json.RawMessage) string {
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, c := range parts {
		if c.Type == "output_text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
