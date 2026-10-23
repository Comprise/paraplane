package shellx

import (
	"path"
	"path/filepath"
	"strings"
)

// Kind is how a command touches the file a word may name. A larger kind
// wins when one file is touched several ways.
type Kind int

const (
	Read  Kind = iota // only mentioned
	Edit              // edited in place (sed -i): Old and News hold the change
	Write             // written
)

// Word is a word of a command and how the command touches the file it may
// name. For an Edit, Old is the sed script (scripts joined by newlines)
// and News the replacement texts of its s commands.
type Word struct {
	Text string
	Kind Kind
	Old  string
	News []string
}

// Words are the words of a command with the way it touches them: words
// after >, >> or >| and the files of tee are writes; so is the last
// argument of cp, mv and install, or with -t DIR each DIR/<source name>;
// the files of sed -i are edits (writes when the script is not plain s
// commands); every other word is a read. Heredoc delimiters and bodies and
// comments are not words.
func Words(cmd string) []Word {
	var words []Word
	var args []int // indexes in words of the current simple command's arguments
	endCommand := func() {
		words = append(words, markWrites(words, args)...)
		args = args[:0]
	}
	redirect := false
	for _, t := range lex(cmd) {
		switch t.op {
		case opSep:
			endCommand()
			redirect = false
		case opOut:
			redirect = true
		case opIn:
			redirect = false
		default:
			k := Read
			if redirect {
				k = Write
			}
			words = append(words, Word{Text: t.text, Kind: k})
			if !redirect {
				args = append(args, len(words)-1)
			}
			redirect = false
		}
	}
	endCommand()
	return words
}

// CommandNames are the first words of the simple commands of cmd, after
// VAR=value prefixes and without redirection targets (of <, <<<, > and the
// like) or a descriptor glued to < (2<f), in order: "cd x && git commit"
// gives cd and git, "echo git" only echo, "<in git commit" git.
func CommandNames(cmd string) []string {
	var names []string
	first, redirect := true, false
	named := false // the previous token was a word taken as a name
	for _, t := range lex(cmd) {
		switch t.op {
		case opSep:
			first, redirect = true, false
		case opOut:
			redirect = true
		case opIn:
			if t.fd && named {
				names, first = names[:len(names)-1], true // 2<f: 2 is a descriptor
			}
			redirect = true
		default:
			if first && !redirect && !strings.Contains(t.text, "=") {
				names = append(names, t.text)
				first = false
				named = true
				redirect = false
				continue
			}
			redirect = false
		}
		named = false
	}
	return names
}

// markWrites marks the files a simple command writes or edits; args are
// the indexes of its words (redirections excluded), the first is the
// command. It returns the extra written paths that are not words of the
// command (cp -t DIR src writes DIR/src).
func markWrites(words []Word, args []int) []Word {
	for len(args) > 0 && strings.Contains(words[args[0]].Text, "=") {
		args = args[1:] // VAR=value prefixes
	}
	if len(args) == 0 {
		return nil
	}
	switch path.Base(words[args[0]].Text) {
	case "tee":
		for _, i := range args[1:] {
			if w := words[i].Text; !strings.HasPrefix(w, "-") || w == "-" {
				words[i].Kind = Write
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
func markCopy(words []Word, args []int) []Word {
	var files []int
	dir := ""
	hasDir := false
	for n := 0; n < len(args); n++ {
		i := args[n]
		w := words[i].Text
		switch {
		case w == "--target-directory":
			if n+1 < len(args) {
				n++
				dir, hasDir = words[args[n]].Text, true
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
					dir, hasDir = words[args[n]].Text, true
				}
			}
		default:
			files = append(files, i)
		}
	}
	if !hasDir {
		if len(files) > 1 {
			words[files[len(files)-1]].Kind = Write
		}
		return nil
	}
	var extra []Word
	for _, i := range files {
		if base := path.Base(anySlash(words[i].Text)); base != "." && base != "/" && base != ".." {
			extra = append(extra, Word{Text: strings.TrimRight(dir, "/") + "/" + base, Kind: Write})
		}
	}
	return extra
}

// anySlash turns OS and Windows separators into slashes.
func anySlash(s string) string { return strings.ReplaceAll(filepath.ToSlash(s), `\`, "/") }

// markSed marks the files of sed -i (--in-place, -i.bak, -ni) as edits with
// the script as the change; without -i sed only reads. The script is the
// first non-option argument unless -e or -f give it. A bare -i followed by
// an empty word or one starting with "." takes it as the backup suffix, as
// BSD sed does (sed -i "" s/a/b/ f). -l and --line-length take a number;
// after -- every word is an argument. When the change cannot be seen from
// the script (-f, an empty script, or one that is not only plain s
// commands, see sedPlain) the files are writes: the full Write verdict.
func markSed(words []Word, args []int) {
	s := &sedArgs{words: words, args: args}
	s.parse()
	if !s.inPlace {
		return
	}
	files, scripts := s.files, s.scripts
	if !s.scriptOpt && len(files) > 0 {
		scripts = append(scripts, words[files[0]].Text)
		files = files[1:]
	}
	old := strings.Join(scripts, "\n")
	var news []string
	plain := !s.hidden && len(scripts) > 0
	for _, sc := range scripts {
		repls, ok := sedPlain(sc)
		plain = plain && ok && strings.TrimSpace(sc) != ""
		news = append(news, repls...)
	}
	for _, i := range files {
		if plain {
			words[i].Kind = Edit
			words[i].Old, words[i].News = old, news
		} else {
			words[i].Kind = Write
		}
	}
}

// sedArgs is the parse of the arguments of one sed command.
type sedArgs struct {
	words []Word
	args  []int // indexes in words of the arguments after "sed"
	n     int   // index in args of the argument being parsed

	files     []int    // indexes in words of the file arguments
	scripts   []string // scripts given by -e and --expression
	inPlace   bool     // -i or --in-place
	scriptOpt bool     // -e, -f, --expression or --file gave the script
	hidden    bool     // -f or --file: the script is in a file
}

// parse reads the arguments into the fields of s.
func (s *sedArgs) parse() {
	opts := true
	for s.n = 0; s.n < len(s.args); s.n++ {
		w := s.words[s.args[s.n]].Text
		switch {
		case !opts || w == "-" || !strings.HasPrefix(w, "-"):
			s.files = append(s.files, s.args[s.n])
		case w == "--":
			opts = false
		case strings.HasPrefix(w, "--"):
			s.long(w)
		default:
			s.short(w)
		}
	}
}

// next consumes and returns the next argument, or "" at the end.
func (s *sedArgs) next() string {
	if s.n+1 < len(s.args) {
		s.n++
		return s.words[s.args[s.n]].Text
	}
	return ""
}

// long reads a long option; unknown ones are skipped.
func (s *sedArgs) long(w string) {
	switch {
	case w == "--line-length":
		s.next()
	case w == "--expression":
		s.scriptOpt = true
		s.scripts = append(s.scripts, s.next())
	case strings.HasPrefix(w, "--expression="):
		s.scriptOpt = true
		s.scripts = append(s.scripts, strings.TrimPrefix(w, "--expression="))
	case w == "--file":
		s.scriptOpt, s.hidden = true, true
		s.next()
	case strings.HasPrefix(w, "--file="):
		s.scriptOpt, s.hidden = true, true
	case w == "--in-place" || strings.HasPrefix(w, "--in-place="):
		s.inPlace = true
	}
}

// short reads a cluster of short options: i takes the rest as a suffix;
// e and f take the rest or the next word as the script or its file; l
// takes the rest or the next word as a number. The first of them ends the
// cluster; other letters are flags.
func (s *sedArgs) short(w string) {
	for k := 1; k < len(w); k++ {
		switch w[k] {
		case 'i':
			s.inPlace = true
			if k+1 == len(w) && s.n+1 < len(s.args) {
				if t := s.words[s.args[s.n+1]].Text; t == "" || strings.HasPrefix(t, ".") {
					s.n++ // a BSD backup suffix
				}
			}
			return
		case 'l':
			if k+1 == len(w) {
				s.next()
			}
			return
		case 'e', 'f':
			s.scriptOpt = true
			arg := w[k+1:]
			if arg == "" {
				arg = s.next()
			}
			if w[k] == 'f' {
				s.hidden = true
			} else {
				s.scripts = append(s.scripts, arg)
			}
			return
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
	p := &sedScanner{s: script}
	for p.blank(); p.i < len(script); p.blank() {
		repl, ok := p.command()
		if !ok {
			return nil, false
		}
		repls = append(repls, repl)
	}
	return repls, true
}

// sedScanner reads a sed script for sedPlain; i is the next byte.
type sedScanner struct {
	s string
	i int
}

// blank skips blanks, newlines and semicolons between commands.
func (p *sedScanner) blank() {
	for p.i < len(p.s) && strings.IndexByte(" \t\n;", p.s[p.i]) >= 0 {
		p.i++
	}
}

// command reads one command with its addresses; ok is false unless it is
// a plain s command. repl is its replacement text.
func (p *sedScanner) command() (repl string, ok bool) {
	if !p.address() {
		return "", false
	}
	if p.i < len(p.s) && p.s[p.i] == ',' {
		p.i++
		if !p.address() {
			return "", false
		}
	}
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '!') {
		p.i++
	}
	return p.subst()
}

// address reads an optional address: N, $, /re/ or \cREc.
func (p *sedScanner) address() bool {
	s := p.s
	switch {
	case p.i < len(s) && s[p.i] == '$':
		p.i++
	case p.i < len(s) && s[p.i] >= '0' && s[p.i] <= '9':
		for p.i < len(s) && s[p.i] >= '0' && s[p.i] <= '9' {
			p.i++
		}
	case p.i < len(s) && s[p.i] == '/':
		return p.skipPart(p.i+1, '/')
	case p.i+1 < len(s) && s[p.i] == '\\':
		return p.skipPart(p.i+2, s[p.i+1])
	}
	return true
}

// skipPart moves past a delimited part that starts at i.
func (p *sedScanner) skipPart(i int, d byte) bool {
	_, _, j, ok := sedPart(p.s, i, d)
	if ok {
		p.i = j
	}
	return ok
}

// subst reads s/re/repl/flags; ok is false for another command, a bad
// delimiter, a replacement that refers to the match, or a flag or text
// other than g p i I m M and digits before the command end.
func (p *sedScanner) subst() (repl string, ok bool) {
	s := p.s
	if p.i+1 >= len(s) || s[p.i] != 's' {
		return "", false
	}
	d := s[p.i+1]
	if d == '\\' || d == '\n' || d == ' ' || d == '\t' || isWordByte(d) {
		return "", false
	}
	_, _, j, ok := sedPart(s, p.i+2, d)
	if !ok {
		return "", false
	}
	repl, ref, k, ok := sedPart(s, j, d)
	if !ok || ref {
		return "", false
	}
	for p.i = k; p.i < len(s) && strings.IndexByte("gpiImM0123456789", s[p.i]) >= 0; p.i++ {
	}
	if p.i < len(s) && strings.IndexByte(" \t\n;", s[p.i]) < 0 {
		return "", false // w file, e, a block brace or junk
	}
	return repl, true
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
