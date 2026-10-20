package hooks

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"paraplane/internal/layout"
	"paraplane/internal/plan"
	"paraplane/internal/rules"
	"paraplane/internal/textx"
)

// planKeys are the words whose presence in an Edit makes it a plan edit.
var planKeys = []string{"paraplane", "waves:", "tasks:", "id:", "title:", "owns:", "reads:", "depends:", "verify:", "gate:"}

// postInput is the part of a Write or Edit tool_input the hook reads.
type postInput struct {
	FilePath  any `json:"file_path"`
	OldString any `json:"old_string"`
	NewString any `json:"new_string"`
}

// PostToolUse checks a plan file after Write or Edit. It stays quiet while a
// plan is being written and while checkboxes are ticked (spec section 8).
func PostToolUse(env Env, in Input) ([]byte, error) {
	if !env.Config.Enabled {
		return nil, nil
	}
	if env.Agent == Codex && (in.ToolName == "Bash" || in.ToolName == "apply_patch") {
		return codexPost(env, in)
	}
	if in.ToolName != "Write" && in.ToolName != "Edit" {
		return nil, nil
	}
	var v postInput
	if json.Unmarshal(in.ToolInput, &v) != nil {
		return nil, nil
	}
	fp, _ := v.FilePath.(string)
	rel, ok := rootRel(env.Root, fp)
	if !ok || !isPlanPath(env, rel) {
		return nil, nil
	}
	md, ok, err := readPlan(filepath.Join(env.Root, filepath.FromSlash(rel)))
	if !ok {
		return nil, err
	}
	if in.ToolName == "Edit" {
		oldS, _ := v.OldString.(string)
		newS, _ := v.NewString.(string)
		if !editTouchesPlan(md, oldS, newS) {
			return nil, nil
		}
		return postVerdict(env, Options(env), rel, md, postEdit)
	}
	return postVerdict(env, Options(env), rel, md, postWrite)
}

// postKind is how a tool call touched a plan file; a larger kind wins when
// one call touches the file several ways.
type postKind int

const (
	postRead  postKind = iota // the file was only mentioned: only E1–E7 count
	postEdit                  // the file was edited: warnings are not repeated
	postWrite                 // the file was written: every finding counts
)

// postVerdict is the PostToolUse answer for the plan file rel with content
// md, checked with o (one Options, so one deadline, per hook call): E0 adds context, E1–E7 block (context in mode warn), warnings add
// context only for a write. A read answers only E1–E7. paraplane's own
// strings go through env.Agent.text; the plan path and plan text do not.
func postVerdict(env Env, o rules.Options, rel string, md []byte, k postKind) ([]byte, error) {
	r := rules.Check(md, o)
	if k == postRead && (r.Errors() == 0 || r.Findings[0].Code == "E0") {
		return nil, nil
	}
	name := textx.Line(rel, maxPath)
	text := env.Agent.text
	switch {
	case len(r.Findings) > 0 && r.Findings[0].Code == "E0" && r.Findings[0].Msg != layout.ErrDeadline.Error():
		return contextOutput("PostToolUse", "paraplane: "+name+" has no valid ```paraplane block yet ("+
			textx.Line(r.Findings[0].Msg, maxE0)+text("); add the block before the plan is final. Format: skill paraplane:rules."))
	case r.Errors() > 0:
		reason := "paraplane: " + name + " is not ready for parallel execution (fix it before the plan is final):\n" +
			planFindings(env.Agent, r.Findings) + text("\nFormat and rules: skill paraplane:rules.")
		if env.Config.Mode == "warn" {
			return contextOutput("PostToolUse", reason)
		}
		return encodeJSON(hookOutput{Decision: "block", Reason: reason})
	case r.Warnings() > 0 && k == postWrite:
		return contextOutput("PostToolUse", planWarnings(env.Agent, r))
	}
	return nil, nil
}

// editTouchesPlan reports whether an Edit may have changed the plan block:
// either string names the block or one of its keys, or a non-empty
// new_string occurs inside the block of the edited file.
func editTouchesPlan(md []byte, oldS, newS string) bool {
	for _, k := range planKeys {
		if strings.Contains(oldS, k) || strings.Contains(newS, k) {
			return true
		}
	}
	if newS == "" {
		return false
	}
	blk, err := plan.Extract(md)
	if err != nil {
		return false
	}
	s := string(md)
	for from := 0; from < blk.End; {
		i := strings.Index(s[from:], newS)
		if i < 0 {
			return false
		}
		at := from + i
		if at >= blk.Start && at < blk.End {
			return true
		}
		from = at + 1
	}
	return false
}
