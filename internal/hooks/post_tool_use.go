package hooks

import (
	"encoding/json"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"paraplane/internal/layout"
	"paraplane/internal/pathset"
	"paraplane/internal/plan"
	"paraplane/internal/rules"
	"paraplane/internal/textx"
)

// postKeys are the words whose presence in an Edit makes it a plan edit.
var postKeys = []string{"paraplane", "waves:", "tasks:", "id:", "title:", "owns:", "reads:", "depends:", "verify:", "gate:"}

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
	rel, ok := postRel(env.Root, fp)
	if !ok || !postIsPlan(env, rel) {
		return nil, nil
	}
	md, ok, err := postReadFile(filepath.Join(env.Root, filepath.FromSlash(rel)))
	if !ok {
		return nil, err
	}
	edit := in.ToolName == "Edit"
	if edit {
		oldS, _ := v.OldString.(string)
		newS, _ := v.NewString.(string)
		if !postEditTouchesPlan(md, oldS, newS) {
			return nil, nil
		}
	}

	k := postWrite
	if edit {
		k = postEdit
	}
	return postVerdict(env, rel, md, k)
}

// postKind is how a tool call touched a plan file.
type postKind int

const (
	postWrite postKind = iota // the file was written: every finding counts
	postEdit                  // the file was edited: warnings are not repeated
	postRead                  // the file was only mentioned: only E1–E7 count
)

// postVerdict is the PostToolUse answer for the plan file rel with content
// md: E0 adds context, E1–E7 block (context in mode warn), warnings add
// context only for a write. A read answers only E1–E7.
func postVerdict(env Env, rel string, md []byte, k postKind) ([]byte, error) {
	r := rules.Check(md, Options(env))
	if k == postRead && (r.Errors() == 0 || r.Findings[0].Code == "E0") {
		return nil, nil
	}
	name := textx.Line(rel, postMaxPath)
	rulesSkill := env.Agent.Skill("rules")
	switch {
	case len(r.Findings) > 0 && r.Findings[0].Code == "E0" && r.Findings[0].Msg != layout.ErrDeadline.Error():
		return postContext("PostToolUse", "paraplane: "+name+" has no valid ```paraplane block yet ("+
			textx.Line(r.Findings[0].Msg, postMaxE0)+"); add the block before the plan is final. Format: skill "+rulesSkill+".")
	case r.Errors() > 0:
		reason := "paraplane: " + name + " is not ready for parallel execution (fix it before the plan is final):\n" +
			postFindings(r.Findings) + "\nFormat and rules: skill " + rulesSkill + "."
		if env.Config.Mode == "warn" {
			return postContext("PostToolUse", reason)
		}
		return encodeJSON(hookOutput{Decision: "block", Reason: reason})
	case r.Warnings() > 0 && k == postWrite:
		return postContext("PostToolUse", postWarnings(r))
	}
	return nil, nil
}

// postRel turns file_path into a path relative to root with "/" separators;
// a backslash counts as a separator on every platform. A relative file_path
// is taken relative to root. ok is false outside root.
func postRel(root, fp string) (string, bool) {
	if fp == "" || root == "" {
		return "", false
	}
	slash := func(s string) string { return strings.ReplaceAll(filepath.ToSlash(s), `\`, "/") }
	p := slash(fp)
	if postAbs(p) {
		r := strings.TrimRight(slash(root), "/")
		if len(p) <= len(r)+1 || p[len(r)] != '/' || !postSamePrefix(p[:len(r)], r) {
			return "", false
		}
		p = p[len(r)+1:]
	}
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || postAbs(p) {
		return "", false
	}
	return p, true
}

// postAbs reports whether a "/"-separated path is absolute ("/x" or "C:/x").
func postAbs(p string) bool {
	return strings.HasPrefix(p, "/") || (len(p) >= 3 && p[1] == ':' && p[2] == '/')
}

// postSamePrefix compares path prefixes; case-insensitively on Windows.
func postSamePrefix(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// postIsPlan reports whether rel matches one of plan_paths.
func postIsPlan(env Env, rel string) bool {
	for _, pat := range env.Config.PlanPaths {
		if pathset.Match(pat, rel) {
			return true
		}
	}
	return false
}

// postEditTouchesPlan reports whether an Edit may have changed the plan
// block: either string names the block or one of its keys, or a non-empty
// new_string occurs inside the block of the edited file.
func postEditTouchesPlan(md []byte, oldS, newS string) bool {
	for _, k := range postKeys {
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
