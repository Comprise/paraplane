package hooks

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"paraplane/internal/shellx"
)

// Limits for the paths a Codex PostToolUse looks at.
const (
	codexMaxWord  = 4096 // bytes in one candidate path
	codexMaxPlans = 8    // plan files checked per tool call
)

// shellWord is a candidate plan file of a shell command (shellx.Words) or
// a patch (patchFiles) and how the call touches it: read (only mentioned),
// edit (sed -i or a patch Update File, with the change) or write.
type shellWord struct {
	text   string
	kind   postKind
	change planChange // the change of an edit
}

// planChange is what an in-place edit of a file changed, for the Edit test
// of spec section 8: old is the removed text (or the sed script), news the
// inserted texts.
type planChange struct {
	old  string
	news []string
}

// touches reports whether the change may have touched the plan block of
// md, as editTouchesPlan does for an Edit.
func (c planChange) touches(md []byte) bool {
	if editTouchesPlan(md, c.old, "") {
		return true
	}
	for _, n := range c.news {
		if editTouchesPlan(md, "", n) {
			return true
		}
	}
	return false
}

// codexPost checks the plan files a Codex Bash command or apply_patch names
// (spec section 12a.2). The paths come from the command: words that,
// relative to cwd, match plan_paths and are existing regular files. A write
// target (redirection, tee, the destination of cp/mv/install, a patch Add
// File or Move to) gets the verdict of a Write; an in-place edit (sed -i, a
// patch Update File) gets the verdict of an Edit, and only when the change
// touches the block or its keys; a file that is only mentioned, as by a
// read, gets context on E1–E7 alone and is never blocked (also when the same
// command edits it without touching the block). A file written indirectly is
// missed; Stop and the skills catch that. The first block wins; without one,
// the first answer of a written or edited file, then of a read one.
func codexPost(env Env, in Input) ([]byte, error) {
	var cands []shellWord
	if in.ToolName == "Bash" {
		cands = shellWords(bashCommand(in))
	} else {
		cands = patchFiles(patchText(in))
	}
	cwd := in.Cwd
	if !filepath.IsAbs(cwd) {
		cwd = env.Root
	}
	var order []string
	kind := map[string]postKind{}
	read := map[string]bool{}
	changes := map[string][]planChange{}
	for _, w := range cands {
		if w.text == "" || len(w.text) > codexMaxWord {
			continue
		}
		p := w.text
		if !slashAbs(anySlash(p)) {
			p = filepath.Join(cwd, p)
		}
		rel, ok := rootRel(env.Root, p)
		if !ok || !isPlanPath(env, rel) {
			continue
		}
		if _, seen := kind[rel]; !seen {
			if len(order) == codexMaxPlans {
				continue
			}
			order = append(order, rel)
		}
		kind[rel] = max(kind[rel], w.kind)
		switch w.kind {
		case postEdit:
			changes[rel] = append(changes[rel], w.change)
		case postRead:
			read[rel] = true
		}
	}
	// One deadline for the whole hook call: with a fresh one per file, up to
	// codexMaxPlans checks could outlast the hook timeout. A written or
	// edited file checked after the others used it up is blocked as not
	// checked, not as too complex: the time was not its own.
	// Written and edited files go first, then those only read: a block
	// wins over any context, and a read's context never hides a later
	// write's block. Without a block the first answer is returned.
	o := Options(env)
	var first []byte
	checked := 0
	for _, reads := range []bool{false, true} {
		for _, rel := range order {
			if (kind[rel] == postRead) != reads {
				continue
			}
			md, ok, err := readPlan(filepath.Join(env.Root, filepath.FromSlash(rel)))
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			k := kind[rel]
			if k == postEdit && !anyTouches(changes[rel], md) {
				if !read[rel] {
					continue
				}
				k = postRead
			}
			out, err := postVerdict(env, o, rel, md, k, checked > 0)
			checked++
			if err != nil {
				return nil, err
			}
			if isBlock(out) {
				return out, nil
			}
			if first == nil {
				first = out
			}
		}
	}
	return first, nil
}

// isBlock reports whether a PostToolUse answer is decision: block.
func isBlock(out []byte) bool {
	var v hookOutput
	return out != nil && json.Unmarshal(out, &v) == nil && v.Decision == "block"
}

// anyTouches reports whether one of the changes touches the plan block of md.
func anyTouches(cs []planChange, md []byte) bool {
	for _, c := range cs {
		if c.touches(md) {
			return true
		}
	}
	return false
}

// shellWords are the words of a Bash command (see shellx.Words) as
// candidate plan files.
func shellWords(cmd string) []shellWord {
	ws := shellx.Words(cmd)
	out := make([]shellWord, len(ws))
	for i, w := range ws {
		out[i] = shellWord{text: w.Text, kind: postKindOf(w.Kind), change: planChange{old: w.Old, news: w.News}}
	}
	return out
}

// postKindOf is the postKind of a shellx.Kind.
func postKindOf(k shellx.Kind) postKind {
	switch k {
	case shellx.Edit:
		return postEdit
	case shellx.Write:
		return postWrite
	}
	return postRead
}

// isWordByte reports whether c is an ASCII letter, digit or underscore
// (for codex_prompt.go; shellx has its own copy for sed).
func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// patchText is the apply_patch text: tool_input.command, input or patch,
// whichever is a non-empty string first (the field is not documented).
func patchText(in Input) string {
	var v map[string]any
	if json.Unmarshal(in.ToolInput, &v) != nil {
		return ""
	}
	for _, k := range []string{"command", "input", "patch"} {
		if s, _ := v[k].(string); s != "" {
			return s
		}
	}
	return ""
}

// patchFiles are the files a patch touches: Add File and Move to are writes;
// Update File is an edit whose change is its hunks' - and + lines.
func patchFiles(patch string) []shellWord {
	var files []shellWord
	cur := -1 // index in files of the Update File the hunk lines belong to
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimRight(line, "\r")
		if rest, ok := strings.CutPrefix(line, "*** "); ok {
			switch {
			case strings.HasPrefix(rest, "Add File:"):
				files = append(files, shellWord{text: patchName(rest), kind: postWrite})
				cur = -1
			case strings.HasPrefix(rest, "Update File:"):
				files = append(files, shellWord{text: patchName(rest), kind: postEdit})
				cur = len(files) - 1
			case strings.HasPrefix(rest, "Move to:"):
				files = append(files, shellWord{text: patchName(rest), kind: postWrite})
			case rest == "End of File":
			default:
				cur = -1 // Delete File, Begin Patch, End Patch
			}
			continue
		}
		if cur < 0 || line == "" {
			continue
		}
		c := &files[cur].change
		switch line[0] {
		case '-':
			c.old += line[1:] + "\n"
		case '+':
			c.news = append(c.news, line[1:])
		}
	}
	return files
}

// patchName is the file name after a patch header "Xxx File: name".
func patchName(header string) string {
	_, name, _ := strings.Cut(header, ":")
	return strings.TrimSpace(name)
}
