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

// shellWord is a word of a shell command and how the command touches the
// file it may name: read (only mentioned), edit (sed -i, with the change) or
// write.
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
	// codexMaxPlans checks could outlast the hook timeout. A file checked
	// after another that ran out of it gets no answer, not the E0 deadline
	// verdict: the time was not its own (Stop and the skills check it).
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

// shellToken is a lexed piece of a command: a word, or an operator
// ("sep" ends a simple command, ">" writes to the next word, "<" reads it).
type shellToken struct {
	op   string // "", "sep", ">", "<"
	text string
}

// heredoc is a here-document of a command line: its delimiter word and
// whether leading tabs are stripped (<<-).
type heredoc struct {
	delim string
	strip bool
}

// shellLex splits a command into words and operators: blanks separate words;
// ; & | ( ) newline and backtick end a simple command; quotes and
// backslashes are removed. Expansions are left as text: this only has to
// find literal file names. A # that starts a word starts a comment, up to
// the newline. A here-document (<< or <<- WORD) is not a file:
// its delimiter is dropped and its body, the lines after the next newline up
// to the delimiter line, is skipped. Inside arithmetic (( )), $(( )) and
// $[ ] the operators < << <<= > >> and parentheses are arithmetic, not
// redirections, heredocs or command ends; a group still open at ; or a
// newline (save the ; of for (( ; ; ))) ends there.
func shellLex(cmd string) []shellToken {
	var toks []shellToken
	var cur strings.Builder
	in := false // cur holds a word, possibly empty ("")
	digits := true
	wantDelim, strip := false, false // the next word is a heredoc delimiter
	var docs []heredoc               // heredocs whose bodies start after the next newline
	arith, parens := false, 0        // inside (( )) or $(( )); open inner parentheses
	forArith := false                // the (( )) of for (( ; ; )), where ; is arithmetic
	brackets := 0                    // open $[ ] groups
	// endArith leaves an unbalanced arithmetic group at the end of a
	// command, so a later redirection is still seen.
	endArith := func() {
		arith, parens, forArith, brackets = false, 0, false, 0
	}
	flush := func() {
		if in {
			if wantDelim {
				docs = append(docs, heredoc{delim: cur.String(), strip: strip})
				wantDelim = false
			} else {
				toks = append(toks, shellToken{text: cur.String()})
			}
			cur.Reset()
			in, digits = false, true
		}
	}
	// skipBodies skips the bodies of docs; nl is the index of the newline
	// that ends their command line. It returns the index of the newline that
	// ends the last delimiter line, or len(cmd).
	skipBodies := func(nl int) int {
		i := nl
		for _, d := range docs {
			for i < len(cmd) {
				start := i + 1
				end := strings.IndexByte(cmd[start:], '\n')
				if end < 0 {
					end = len(cmd)
				} else {
					end += start
				}
				line := strings.TrimSuffix(cmd[start:end], "\r")
				if d.strip {
					line = strings.TrimLeft(line, "\t")
				}
				i = end
				if line == d.delim {
					break
				}
			}
		}
		docs = docs[:0]
		return i
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
		if (arith || brackets > 0) && (c == '<' || c == '>') {
			flush() // an arithmetic operator
			continue
		}
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
			if in && digits && !wantDelim {
				cur.Reset() // a file descriptor, as in 2>file
				in = false
			}
			flush()
			for i+1 < len(cmd) && (cmd[i+1] == '>' || cmd[i+1] == '|') {
				i++ // >> and >|
			}
			if i+2 < len(cmd) && cmd[i+1] == '&' && (cmd[i+2] == '-' || (cmd[i+2] >= '0' && cmd[i+2] <= '9')) {
				for i++; i+1 < len(cmd) && (cmd[i+1] == '-' || (cmd[i+1] >= '0' && cmd[i+1] <= '9')); i++ {
				}
				continue // >&2: a descriptor, not a file
			}
			if i+1 < len(cmd) && cmd[i+1] == '&' {
				i++ // >&file: stdout and stderr to the file
			}
			toks = append(toks, shellToken{op: ">"})
		case '<':
			flush()
			switch {
			case strings.HasPrefix(cmd[i:], "<<<"):
				i += 2 // a here-string: the next word is text, read like a file
			case strings.HasPrefix(cmd[i:], "<<"):
				i++
				strip = i+1 < len(cmd) && cmd[i+1] == '-'
				if strip {
					i++
				}
				wantDelim = true
				continue
			}
			toks = append(toks, shellToken{op: "<"})
		case '&':
			flush()
			if i+1 < len(cmd) && cmd[i+1] == '>' {
				continue // &> file
			}
			toks = append(toks, shellToken{op: "sep"})
		case '\n':
			flush()
			endArith()
			wantDelim = false
			toks = append(toks, shellToken{op: "sep"})
			if len(docs) > 0 {
				i = skipBodies(i)
			}
		case '(':
			flush()
			switch {
			case arith:
				parens++
			case i+1 < len(cmd) && cmd[i+1] == '(':
				arith, parens = true, 0 // (( or $((
				n := len(toks)
				forArith = n > 0 && toks[n-1].op == "" && toks[n-1].text == "for"
				i++
			default:
				toks = append(toks, shellToken{op: "sep"})
			}
		case ')':
			flush()
			switch {
			case arith && parens > 0:
				parens--
			case arith:
				arith = false // the first ) of ))
				if i+1 < len(cmd) && cmd[i+1] == ')' {
					i++
				}
			default:
				toks = append(toks, shellToken{op: "sep"})
			}
		case '[':
			if i > 0 && cmd[i-1] == '$' {
				brackets++ // $[ ]
			}
			add(c)
		case ']':
			if brackets > 0 {
				brackets--
			}
			add(c)
		case ';':
			flush()
			if !(arith && forArith) {
				endArith()
			}
			toks = append(toks, shellToken{op: "sep"})
		case '|', '`':
			flush()
			toks = append(toks, shellToken{op: "sep"})
		case '#':
			if in {
				add(c) // inside a word, as in a#b or $#
				break
			}
			for i+1 < len(cmd) && cmd[i+1] != '\n' {
				i++ // a comment, up to the newline
			}
		case ' ', '\t', '\r':
			flush()
		default:
			add(c)
		}
	}
	flush()
	return toks
}

// shellWords are the words of a command with the way it touches them:
// words after >, >> or >| and the files of tee are writes; so is the last
// argument of cp, mv and install, or with -t DIR each DIR/<source name>;
// the files of sed -i are edits (writes when the script is not plain s
// commands); every other word is a read.
func shellWords(cmd string) []shellWord {
	var words []shellWord
	var args []int // indexes in words of the current simple command's arguments
	endCommand := func() {
		words = append(words, shellMarkWrites(words, args)...)
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
			k := postRead
			if redirect {
				k = postWrite
			}
			words = append(words, shellWord{text: t.text, kind: k})
			if !redirect {
				args = append(args, len(words)-1)
			}
			redirect = false
		}
	}
	endCommand()
	return words
}

// shellMarkWrites marks the files a simple command writes or edits; args are
// the indexes of its words (redirections excluded), the first is the
// command. It returns the extra written paths that are not words of the
// command (cp -t DIR src writes DIR/src).
func shellMarkWrites(words []shellWord, args []int) []shellWord {
	for len(args) > 0 && strings.Contains(words[args[0]].text, "=") {
		args = args[1:] // VAR=value prefixes
	}
	if len(args) == 0 {
		return nil
	}
	switch path.Base(words[args[0]].text) {
	case "tee":
		for _, i := range args[1:] {
			if w := words[i].text; !strings.HasPrefix(w, "-") || w == "-" {
				words[i].kind = postWrite
			}
		}
	case "cp", "mv", "install":
		return markCopy(words, args[1:])
	case "sed":
		markSed(words, args[1:])
	}
	return nil
}

// markCopy marks the destination of cp, mv or install. With a -t directory
// (-t DIR, -tDIR, -rt DIR, --target-directory[=]DIR) the written files are
// DIR/<base of each source>, returned as extra words; else the last of two
// or more file arguments is written. Sources are only read.
func markCopy(words []shellWord, args []int) []shellWord {
	var files []int
	dir := ""
	hasDir := false
	for n := 0; n < len(args); n++ {
		i := args[n]
		w := words[i].text
		switch {
		case w == "--target-directory":
			if n+1 < len(args) {
				n++
				dir, hasDir = words[args[n]].text, true
			}
		case strings.HasPrefix(w, "--target-directory="):
			dir, hasDir = strings.TrimPrefix(w, "--target-directory="), true
		case strings.HasPrefix(w, "--"):
		case strings.HasPrefix(w, "-") && len(w) > 1:
			if at := strings.IndexByte(w, 't'); at > 0 {
				if rest := w[at+1:]; rest != "" {
					dir, hasDir = rest, true
				} else if n+1 < len(args) {
					n++
					dir, hasDir = words[args[n]].text, true
				}
			}
		default:
			files = append(files, i)
		}
	}
	if !hasDir {
		if len(files) > 1 {
			words[files[len(files)-1]].kind = postWrite
		}
		return nil
	}
	var extra []shellWord
	for _, i := range files {
		if base := path.Base(anySlash(words[i].text)); base != "." && base != "/" && base != ".." {
			extra = append(extra, shellWord{text: strings.TrimRight(dir, "/") + "/" + base, kind: postWrite})
		}
	}
	return extra
}

// markSed marks the files of sed -i (--in-place, -i.bak, -ni) as edits with
// the script as the change; without -i sed only reads. The script is the
// first non-option argument unless -e or -f give it. A bare -i followed by
// an empty word or one starting with "." takes it as the backup suffix, as
// BSD sed does (sed -i "" s/a/b/ f). -l and --line-length take a number;
// after -- every word is an argument. When the change cannot be seen from
// the script (-f, an empty script, or one that is not only plain s
// commands, see sedPlain) the files are writes: the full Write verdict.
func markSed(words []shellWord, args []int) {
	var files []int
	var scripts []string
	inPlace, scriptOpt, hidden, opts := false, false, false, true
	next := func(n *int) string {
		if *n+1 < len(args) {
			*n++
			return words[args[*n]].text
		}
		return ""
	}
	for n := 0; n < len(args); n++ {
		w := words[args[n]].text
		switch {
		case !opts:
			files = append(files, args[n])
		case w == "--":
			opts = false
		case w == "--line-length":
			next(&n)
		case w == "--expression":
			scriptOpt = true
			scripts = append(scripts, next(&n))
		case strings.HasPrefix(w, "--expression="):
			scriptOpt = true
			scripts = append(scripts, strings.TrimPrefix(w, "--expression="))
		case w == "--file":
			scriptOpt, hidden = true, true
			next(&n)
		case strings.HasPrefix(w, "--file="):
			scriptOpt, hidden = true, true
		case w == "--in-place" || strings.HasPrefix(w, "--in-place="):
			inPlace = true
		case strings.HasPrefix(w, "--"):
		case strings.HasPrefix(w, "-") && len(w) > 1:
			// A short cluster: i takes the rest as a suffix; e and f take
			// the rest or the next word as the script or its file; l
			// takes the rest or the next word as a number.
		cluster:
			for k := 1; k < len(w); k++ {
				switch w[k] {
				case 'i':
					inPlace = true
					if k+1 == len(w) && n+1 < len(args) {
						if s := words[args[n+1]].text; s == "" || strings.HasPrefix(s, ".") {
							n++ // a BSD backup suffix
						}
					}
					break cluster
				case 'l':
					if k+1 == len(w) {
						next(&n)
					}
					break cluster
				case 'e', 'f':
					scriptOpt = true
					arg := w[k+1:]
					if arg == "" {
						arg = next(&n)
					}
					if w[k] == 'f' {
						hidden = true
					} else {
						scripts = append(scripts, arg)
					}
					break cluster
				}
			}
		default:
			files = append(files, args[n])
		}
	}
	if !inPlace {
		return
	}
	if !scriptOpt && len(files) > 0 {
		scripts = append(scripts, words[files[0]].text)
		files = files[1:]
	}
	c := planChange{old: strings.Join(scripts, "\n")}
	plain := !hidden && len(scripts) > 0
	for _, sc := range scripts {
		repls, ok := sedPlain(sc)
		plain = plain && ok && strings.TrimSpace(sc) != ""
		c.news = append(c.news, repls...)
	}
	for _, i := range files {
		if plain {
			words[i].kind = postEdit
			words[i].change = c
		} else {
			words[i].kind = postWrite
		}
	}
}

// sedPlain parses a sed script that holds only s commands, each with
// optional addresses (N, $, /re/, \cREc, a second one after a comma, !) and
// flags (g, p, i, I, m, M, digits), separated by ; or newlines. It returns
// their replacement texts; ok is false for anything else, or for a
// replacement that refers to the match (& or \N), whose inserted text the
// script does not show.
func sedPlain(script string) (repls []string, ok bool) {
	i := 0
	skipBlank := func() {
		for i < len(script) && strings.IndexByte(" \t\n;", script[i]) >= 0 {
			i++
		}
	}
	address := func() bool {
		switch {
		case i < len(script) && script[i] == '$':
			i++
		case i < len(script) && script[i] >= '0' && script[i] <= '9':
			for i < len(script) && script[i] >= '0' && script[i] <= '9' {
				i++
			}
		case i < len(script) && script[i] == '/':
			_, _, j, ok := sedPart(script, i+1, '/')
			if !ok {
				return false
			}
			i = j
		case i+1 < len(script) && script[i] == '\\':
			_, _, j, ok := sedPart(script, i+2, script[i+1])
			if !ok {
				return false
			}
			i = j
		}
		return true
	}
	for skipBlank(); i < len(script); skipBlank() {
		if !address() {
			return nil, false
		}
		if i < len(script) && script[i] == ',' {
			i++
			if !address() {
				return nil, false
			}
		}
		for i < len(script) && (script[i] == ' ' || script[i] == '!') {
			i++
		}
		if i+1 >= len(script) || script[i] != 's' {
			return nil, false
		}
		d := script[i+1]
		if d == '\\' || d == '\n' || d == ' ' || d == '\t' || isWordByte(d) {
			return nil, false
		}
		_, _, j, ok := sedPart(script, i+2, d)
		if !ok {
			return nil, false
		}
		repl, ref, k, ok := sedPart(script, j, d)
		if !ok || ref {
			return nil, false
		}
		repls = append(repls, repl)
		for i = k; i < len(script) && strings.IndexByte("gpiImM0123456789", script[i]) >= 0; i++ {
		}
		if i < len(script) && strings.IndexByte(" \t\n;", script[i]) < 0 {
			return nil, false // w file, e, a block brace or junk
		}
	}
	return repls, true
}

// sedPart reads a part of an s command or an address from i up to the
// delimiter d; next is the index after the delimiter. A backslash before d
// or another backslash is dropped. ref reports an unescaped & or a \N.
func sedPart(s string, i int, d byte) (part string, ref bool, next int, ok bool) {
	var b strings.Builder
	for ; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s):
			i++
			if s[i] >= '0' && s[i] <= '9' {
				ref = true
			}
			if s[i] != d && s[i] != '\\' {
				b.WriteByte('\\')
			}
			b.WriteByte(s[i])
		case c == d:
			return b.String(), ref, i + 1, true
		case c == '&':
			ref = true
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return "", ref, i, false
}

// isWordByte reports whether c is an ASCII letter, digit or underscore.
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
