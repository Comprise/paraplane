// Package shellx splits a shell command line into words, the way the Codex
// hooks need it: to find the literal file names a command reads, edits or
// writes. It is not a shell parser: expansions stay text and only the
// syntax that changes what a word is (quotes, redirections, here-documents,
// arithmetic, comments, command ends) is understood.
package shellx

import "strings"

// op is the kind of a lexed token.
type op int

const (
	opWord op = iota // a word
	opSep            // the end of a simple command
	opOut            // >, >>, >|, &>, >&: the next word is written
	opIn             // <, <<<: the next word is read
)

// token is a lexed piece of a command: a word or an operator.
type token struct {
	op   op
	text string
	fd   bool // opIn: the word just before is a descriptor glued to <, as in 2<f
}

// heredoc is a here-document of a command line: its delimiter word and
// whether leading tabs are stripped (<<-).
type heredoc struct {
	delim string
	strip bool
}

// lexer holds the state of lex between bytes of the command.
type lexer struct {
	cmd  string
	i    int // index of the byte being lexed
	toks []token

	cur    strings.Builder
	in     bool // cur holds a word, possibly empty ("")
	digits bool // cur holds only digits so far (a file descriptor before >)

	wantDelim bool      // the next word is a heredoc delimiter
	strip     bool      // that heredoc is <<-
	docs      []heredoc // heredocs whose bodies start after the next newline

	arith    bool // inside (( )) or $(( ))
	parens   int  // open inner parentheses of that group
	forArith bool // the (( )) of for (( ; ; )), where ; is arithmetic
	brackets int  // open $[ ] groups
}

// lex splits a command into words and operators: blanks separate words;
// ; & | ( ) newline and backtick end a simple command; quotes and
// backslashes are removed. Expansions are left as text: this only has to
// find literal file names. A # that starts a word after a blank or a
// command end starts a comment, up to the newline or a backtick. A
// here-document (<< or <<- WORD) is not a file: its delimiter is dropped
// and its body, the lines after the next newline up to the delimiter line,
// is skipped. Inside arithmetic (( )), $(( )) and $[ ] the operators < <<
// <<= > >> and parentheses are arithmetic, not redirections, heredocs or
// command ends; a group still open at ; or a newline (save the ; of
// for (( ; ; ))) ends there.
func lex(cmd string) []token {
	l := &lexer{cmd: cmd, digits: true}
	for ; l.i < len(cmd); l.i++ {
		l.step(cmd[l.i])
	}
	l.flush()
	return l.toks
}

// step lexes the byte c at l.i; a method may move l.i past the bytes it
// consumed, up to the last one.
func (l *lexer) step(c byte) {
	if (l.arith || l.brackets > 0) && (c == '<' || c == '>') {
		l.flush() // an arithmetic operator
		return
	}
	switch c {
	case '\'', '"', '\\':
		l.quote(c)
	case '>', '<':
		l.redirect(c)
	case '&', '\n', ';', '|', '`':
		l.separator(c)
	case '(', ')', '[', ']':
		l.arithmetic(c)
	case '#':
		l.comment()
	case ' ', '\t', '\r':
		l.flush()
	default:
		l.add(c)
	}
}

// emit appends an operator token.
func (l *lexer) emit(o op) {
	l.toks = append(l.toks, token{op: o})
}

// add appends c to the current word.
func (l *lexer) add(c byte) {
	l.in = true
	if c < '0' || c > '9' {
		l.digits = false
	}
	l.cur.WriteByte(c)
}

// flush ends the current word: it becomes a word token, or the delimiter
// of a pending heredoc.
func (l *lexer) flush() {
	if !l.in {
		return
	}
	if l.wantDelim {
		l.docs = append(l.docs, heredoc{delim: l.cur.String(), strip: l.strip})
		l.wantDelim = false
	} else {
		l.toks = append(l.toks, token{text: l.cur.String()})
	}
	l.cur.Reset()
	l.in, l.digits = false, true
}

// endArith leaves an unbalanced arithmetic group at the end of a command,
// so a later redirection is still seen.
func (l *lexer) endArith() {
	l.arith, l.parens, l.forArith, l.brackets = false, 0, false, 0
}

// quote lexes a single-quoted or double-quoted string or a backslash
// escape into the current word, without the quotes. In double quotes a
// backslash escapes only " \ $ ` and newline; an escaped newline outside
// quotes joins lines.
func (l *lexer) quote(c byte) {
	l.in, l.digits = true, false
	cmd := l.cmd
	switch c {
	case '\'':
		j := strings.IndexByte(cmd[l.i+1:], '\'')
		if j < 0 {
			l.cur.WriteString(cmd[l.i+1:])
			l.i = len(cmd)
			return
		}
		l.cur.WriteString(cmd[l.i+1 : l.i+1+j])
		l.i += j + 1
	case '"':
		for l.i++; l.i < len(cmd) && cmd[l.i] != '"'; l.i++ {
			if cmd[l.i] == '\\' && l.i+1 < len(cmd) && strings.IndexByte("\"\\$`\n", cmd[l.i+1]) >= 0 {
				l.i++
			}
			l.cur.WriteByte(cmd[l.i])
		}
	default:
		if l.i+1 < len(cmd) {
			l.i++
			if cmd[l.i] != '\n' {
				l.cur.WriteByte(cmd[l.i])
			}
		}
	}
}

// redirect lexes an output (>) or input (<) redirection.
func (l *lexer) redirect(c byte) {
	if c == '>' {
		l.output()
	} else {
		l.input()
	}
}

// output lexes >, >>, >|, >&file and N>file; >&N and >&- duplicate a
// descriptor and name no file.
func (l *lexer) output() {
	cmd := l.cmd
	if l.in && l.digits && !l.wantDelim {
		l.cur.Reset() // a file descriptor, as in 2>file
		l.in = false
	}
	l.flush()
	for l.i+1 < len(cmd) && (cmd[l.i+1] == '>' || cmd[l.i+1] == '|') {
		l.i++ // >> and >|
	}
	if l.i+2 < len(cmd) && cmd[l.i+1] == '&' && isDescriptor(cmd[l.i+2]) {
		for l.i++; l.i+1 < len(cmd) && isDescriptor(cmd[l.i+1]); l.i++ {
		}
		return // >&2: a descriptor, not a file
	}
	if l.i+1 < len(cmd) && cmd[l.i+1] == '&' {
		l.i++ // >&file: stdout and stderr to the file
	}
	l.emit(opOut)
}

// isDescriptor reports whether c may follow >& in a descriptor (>&2, >&-).
func isDescriptor(c byte) bool {
	return c == '-' || (c >= '0' && c <= '9')
}

// input lexes <, a here-string <<< (its word is read like a file) and the
// start of a heredoc. A digit word glued to < (2<f) stays a word, as Words
// has always seen it, but the token is marked fd for CommandNames.
func (l *lexer) input() {
	fd := l.in && l.digits && !l.wantDelim
	l.flush()
	switch {
	case strings.HasPrefix(l.cmd[l.i:], "<<<"):
		l.i += 2
	case strings.HasPrefix(l.cmd[l.i:], "<<"):
		l.heredoc()
		return
	}
	l.toks = append(l.toks, token{op: opIn, fd: fd})
}

// heredoc lexes << or <<-: the next word is the delimiter.
func (l *lexer) heredoc() {
	l.i++
	l.strip = l.i+1 < len(l.cmd) && l.cmd[l.i+1] == '-'
	if l.strip {
		l.i++
	}
	l.wantDelim = true
}

// bodies skips the bodies of the pending heredocs; l.i is the newline that
// ends their command line. It leaves l.i at the newline that ends the last
// delimiter line, or at len(cmd).
func (l *lexer) bodies() {
	cmd := l.cmd
	for _, d := range l.docs {
		for l.i < len(cmd) {
			start := l.i + 1
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
			l.i = end
			if line == d.delim {
				break
			}
		}
	}
	l.docs = l.docs[:0]
}

// separator lexes a byte that ends a simple command: & (but not &>), a
// newline (which also ends arithmetic and starts heredoc bodies), ; (which
// ends arithmetic outside for (( ; ; ))), | and a backtick.
func (l *lexer) separator(c byte) {
	l.flush()
	switch c {
	case '&':
		if l.i+1 < len(l.cmd) && l.cmd[l.i+1] == '>' {
			return // &> file
		}
	case '\n':
		l.endArith()
		l.wantDelim = false
		l.emit(opSep)
		if len(l.docs) > 0 {
			l.bodies()
		}
		return
	case ';':
		if !(l.arith && l.forArith) {
			l.endArith()
		}
	}
	l.emit(opSep)
}

// arithmetic lexes parentheses and brackets: (( and $(( open an arithmetic
// group whose parentheses are not command ends, $[ opens a bracket group;
// a plain ( or ) ends a simple command; [ and ] stay in the word.
func (l *lexer) arithmetic(c byte) {
	cmd := l.cmd
	switch c {
	case '[':
		if l.i > 0 && cmd[l.i-1] == '$' {
			l.brackets++ // $[ ]
		}
		l.add(c)
	case ']':
		if l.brackets > 0 {
			l.brackets--
		}
		l.add(c)
	case '(':
		l.flush()
		switch {
		case l.arith:
			l.parens++
		case l.i+1 < len(cmd) && cmd[l.i+1] == '(':
			l.arith, l.parens = true, 0 // (( or $((
			n := len(l.toks)
			l.forArith = n > 0 && l.toks[n-1].op == opWord && l.toks[n-1].text == "for"
			l.i++
		default:
			l.emit(opSep)
		}
	case ')':
		l.flush()
		switch {
		case l.arith && l.parens > 0:
			l.parens--
		case l.arith:
			l.arith = false // the first ) of ))
			if l.i+1 < len(cmd) && cmd[l.i+1] == ')' {
				l.i++
			}
		default:
			l.emit(opSep)
		}
	}
}

// comment lexes #: a comment only after a blank or a command end, up to
// the newline or the closing backtick. After ) or a backtick the word goes
// on ($(cmd)#x), so # is a letter, as inside a word (a#b, $#).
func (l *lexer) comment() {
	cmd := l.cmd
	if l.in || (l.i > 0 && strings.IndexByte(" \t\r\n;&|(", cmd[l.i-1]) < 0) {
		l.add('#')
		return
	}
	for l.i+1 < len(cmd) && cmd[l.i+1] != '\n' && cmd[l.i+1] != '`' {
		l.i++
	}
}
