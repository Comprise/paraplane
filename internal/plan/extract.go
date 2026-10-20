package plan

import (
	"bytes"
	"errors"
)

var (
	ErrNoBlock    = errors.New("no ```paraplane block")
	ErrManyBlocks = errors.New("more than one ```paraplane block")
)

// Block is the ```paraplane fenced block found in a Markdown document.
type Block struct {
	Body       []byte // content between the fences
	Start, End int    // byte offsets of the whole block (fences included) in md
}

// fence is an opening code fence line.
type fence struct {
	char byte
	n    int    // fence length
	info []byte // info string, trimmed
}

// openFence reports whether line (without its terminator) opens a
// CommonMark fenced code block.
func openFence(line []byte) (fence, bool) {
	i := indent(line)
	if i > 3 || i >= len(line) {
		return fence{}, false
	}
	c := line[i]
	if c != '`' && c != '~' {
		return fence{}, false
	}
	n := run(line[i:], c)
	if n < 3 {
		return fence{}, false
	}
	info := bytes.Trim(line[i+n:], " \t")
	if c == '`' && bytes.IndexByte(info, '`') >= 0 {
		return fence{}, false
	}
	return fence{char: c, n: n, info: info}, true
}

// closes reports whether line closes the fenced block opened by f.
func (f fence) closes(line []byte) bool {
	i := indent(line)
	if i > 3 || i >= len(line) || line[i] != f.char {
		return false
	}
	n := run(line[i:], f.char)
	return n >= f.n && len(bytes.Trim(line[i+n:], " \t")) == 0
}

// isPlan reports whether the first word of the info string is "paraplane".
func (f fence) isPlan() bool {
	w := f.info
	if j := bytes.IndexAny(w, " \t"); j >= 0 {
		w = w[:j]
	}
	return string(w) == "paraplane"
}

func indent(line []byte) int {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	return i
}

func run(b []byte, c byte) int {
	n := 0
	for n < len(b) && b[n] == c {
		n++
	}
	return n
}

// Extract finds the single top-level fenced block whose info string starts
// with the word "paraplane". The contents of other fenced blocks are not
// scanned, so an example block nested in a longer fence is ignored. An
// unclosed fence runs to the end of md.
func Extract(md []byte) (Block, error) {
	var (
		found  bool // a plan block was opened
		inPlan bool // the current open fence is the plan block
		blk    Block
		open   fence
		inside bool
		bodyAt int // offset just after the current opening fence line
	)
	for pos := bomLen(md); pos < len(md); { // a leading UTF-8 BOM is skipped; offsets stay absolute
		start := pos
		end := len(md) // end of line content, terminator excluded
		next := len(md)
		if j := bytes.IndexByte(md[pos:], '\n'); j >= 0 {
			end, next = pos+j, pos+j+1
		}
		pos = next
		line := md[start:end]
		if l := len(line); l > 0 && line[l-1] == '\r' {
			line = line[:l-1]
		}
		lineEnd := start + len(line)

		if !inside {
			f, ok := openFence(line)
			if !ok {
				continue
			}
			inside, open, bodyAt = true, f, next
			if f.isPlan() {
				if found {
					return Block{}, ErrManyBlocks
				}
				found, inPlan = true, true
				blk.Start = start
			}
			continue
		}
		if open.closes(line) {
			inside = false
			if inPlan {
				inPlan = false
				blk.Body, blk.End = md[bodyAt:start], lineEnd
			}
		}
	}
	if !found {
		return Block{}, ErrNoBlock
	}
	if inPlan { // unclosed: runs to the end of md
		blk.Body, blk.End = md[bodyAt:], len(md)
	}
	return blk, nil
}

// bomLen is the length of a leading UTF-8 byte order mark in md, or 0.
func bomLen(md []byte) int {
	if bytes.HasPrefix(md, []byte("\xef\xbb\xbf")) {
		return 3
	}
	return 0
}
