package hooks

import (
	"encoding/json"
	"path"
	"path/filepath"
	"strings"
)

// Limits for the paths a Codex PostToolUse looks at.
const (
	codexMaxWord  = 4096 // bytes in one candidate path
	codexMaxPlans = 8    // plan files checked per tool call
)

// codexPatchHeaders are the apply_patch lines that name a written file.
var codexPatchHeaders = []string{"*** Add File:", "*** Update File:", "*** Move to:"}

// shellWord is a word of a shell command; write is set when the command
// writes to it.
type shellWord struct {
	text  string
	write bool
}

// codexPost checks the plan files a Codex Bash command or apply_patch names
// (spec section 12a.2). The paths come from the command: words that,
// relative to cwd, match plan_paths and are existing regular files. A write
// target (redirection, tee, the destination of cp/mv/install, sed -i, a patch
// header) gets the verdict of a Write; a file that is only mentioned, as by a
// read, blocks on E1–E7 alone. A file written indirectly is missed; Stop and
// the skills catch that. The first plan file with an answer wins.
func codexPost(env Env, in Input) ([]byte, error) {
	var cands []shellWord
	if in.ToolName == "Bash" {
		cands = shellWords(bashCommand(in))
	} else {
		for _, p := range patchPaths(patchText(in)) {
			cands = append(cands, shellWord{p, true})
		}
	}
	cwd := in.Cwd
	if !filepath.IsAbs(cwd) {
		cwd = env.Root
	}
	var order []string
	write := map[string]bool{}
	for _, w := range cands {
		if w.text == "" || len(w.text) > codexMaxWord {
			continue
		}
		p := w.text
		if !postAbs(strings.ReplaceAll(filepath.ToSlash(p), `\`, "/")) {
			p = filepath.Join(cwd, p)
		}
		rel, ok := postRel(env.Root, p)
		if !ok || !postIsPlan(env, rel) {
			continue
		}
		if _, seen := write[rel]; !seen {
			if len(order) == codexMaxPlans {
				continue
			}
			order = append(order, rel)
		}
		write[rel] = write[rel] || w.write
	}
	for _, rel := range order {
		md, ok, err := postReadFile(filepath.Join(env.Root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		k := postRead
		if write[rel] {
			k = postWrite
		}
		out, err := postVerdict(env, rel, md, k)
		if out != nil || err != nil {
			return out, err
		}
	}
	return nil, nil
}

// shellToken is a lexed piece of a command: a word, or an operator
// ("sep" ends a simple command, ">" writes to the next word, "<" reads it).
type shellToken struct {
	op   string // "", "sep", ">", "<"
	text string
}

// shellLex splits a command into words and operators: blanks separate words;
// ; & | ( ) newline and backtick end a simple command; quotes and
// backslashes are removed. Expansions are left as text: this only has to
// find literal file names.
func shellLex(cmd string) []shellToken {
	var toks []shellToken
	var cur strings.Builder
	in := false // cur holds a word, possibly empty ("")
	digits := true
	flush := func() {
		if in {
			toks = append(toks, shellToken{text: cur.String()})
			cur.Reset()
			in, digits = false, true
		}
	}
	add := func(c byte) {
		in = true
		if c < '0' || c > '9' {
			digits = false
		}
		cur.WriteByte(c)
	}
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch c {
		case '\'':
			in, digits = true, false
			j := strings.IndexByte(cmd[i+1:], '\'')
			if j < 0 {
				cur.WriteString(cmd[i+1:])
				i = len(cmd)
				break
			}
			cur.WriteString(cmd[i+1 : i+1+j])
			i += j + 1
		case '"':
			in, digits = true, false
			for i++; i < len(cmd) && cmd[i] != '"'; i++ {
				if cmd[i] == '\\' && i+1 < len(cmd) && strings.IndexByte("\"\\$`\n", cmd[i+1]) >= 0 {
					i++
				}
				cur.WriteByte(cmd[i])
			}
		case '\\':
			in, digits = true, false
			if i+1 < len(cmd) {
				i++
				if cmd[i] != '\n' {
					cur.WriteByte(cmd[i])
				}
			}
		case '>':
			if in && digits {
				cur.Reset() // a file descriptor, as in 2>file
				in = false
			}
			flush()
			for i+1 < len(cmd) && (cmd[i+1] == '>' || cmd[i+1] == '|') {
				i++ // >> and >|
			}
			if i+1 < len(cmd) && cmd[i+1] == '&' {
				for i++; i+1 < len(cmd) && (cmd[i+1] == '-' || (cmd[i+1] >= '0' && cmd[i+1] <= '9')); i++ {
				}
				continue // >&2: a descriptor, not a file
			}
			toks = append(toks, shellToken{op: ">"})
		case '<':
			flush()
			toks = append(toks, shellToken{op: "<"})
		case '&':
			flush()
			if i+1 < len(cmd) && cmd[i+1] == '>' {
				continue // &> file
			}
			toks = append(toks, shellToken{op: "sep"})
		case ';', '|', '(', ')', '`', '\n':
			flush()
			toks = append(toks, shellToken{op: "sep"})
		case ' ', '\t', '\r':
			flush()
		default:
			add(c)
		}
	}
	flush()
	return toks
}

// shellWords are the words of a command, with the write targets marked:
// words after >, >> or >|, the files of tee, the last argument of cp, mv
// and install, and the files of sed -i.
func shellWords(cmd string) []shellWord {
	var words []shellWord
	var args []int // indexes in words of the current simple command's arguments
	endCommand := func() {
		shellMarkWrites(words, args)
		args = args[:0]
	}
	redirect := false
	for _, t := range shellLex(cmd) {
		switch t.op {
		case "sep":
			endCommand()
			redirect = false
		case ">":
			redirect = true
		case "<":
			redirect = false
		default:
			words = append(words, shellWord{text: t.text, write: redirect})
			if !redirect {
				args = append(args, len(words)-1)
			}
			redirect = false
		}
	}
	endCommand()
	return words
}

// shellMarkWrites marks the files a simple command writes; args are the
// indexes of its words (redirections excluded), the first is the command.
func shellMarkWrites(words []shellWord, args []int) {
	for len(args) > 0 && strings.Contains(words[args[0]].text, "=") {
		args = args[1:] // VAR=value prefixes
	}
	if len(args) == 0 {
		return
	}
	name := path.Base(words[args[0]].text)
	var files []int // non-option arguments
	opts := map[string]bool{}
	scriptOpt := false // sed -e or -f: the script is not a file argument
	skipNext := false
	for _, i := range args[1:] {
		w := words[i].text
		switch {
		case skipNext:
			skipNext = false
		case name == "sed" && (w == "-e" || w == "-f" || w == "--expression" || w == "--file"):
			scriptOpt, skipNext = true, true
		case strings.HasPrefix(w, "-") && len(w) > 1:
			opts[w] = true
		default:
			files = append(files, i)
		}
	}
	switch name {
	case "tee":
		for _, i := range files {
			words[i].write = true
		}
	case "cp", "mv", "install":
		if len(files) > 1 {
			words[files[len(files)-1]].write = true
		}
	case "sed":
		inPlace := false
		for o := range opts {
			if o == "--in-place" || strings.HasPrefix(o, "--in-place=") ||
				(!strings.HasPrefix(o, "--") && strings.Contains(o, "i")) {
				inPlace = true
			}
		}
		if !inPlace {
			return
		}
		if !scriptOpt && len(files) > 0 {
			files = files[1:] // the script
		}
		for _, i := range files {
			words[i].write = true
		}
	}
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

// patchPaths are the file names of the patch headers that write a file.
func patchPaths(patch string) []string {
	var paths []string
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimRight(line, "\r")
		for _, h := range codexPatchHeaders {
			if rest, ok := strings.CutPrefix(line, h); ok {
				paths = append(paths, strings.TrimSpace(rest))
			}
		}
	}
	return paths
}
