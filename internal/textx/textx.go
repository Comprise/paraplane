// Package textx cleans text that may come from a plan, a config or a YAML
// error before the plugin shows it: on the terminal or in Claude's context.
package textx

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// unsafe reports a rune that sanitize drops or replaces: a control character
// (including NUL, tab, DEL), the Unicode line and paragraph separators U+2028
// and U+2029, or a format character (category Cf: bidi overrides, zero-width
// spaces, BOM).
func unsafe(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp, unicode.Cf)
}

// HasControl reports whether s contains a rune that Line and Block would drop
// or turn into a space: it would hide or reorder what is shown to the user.
func HasControl(s string) bool { return strings.IndexFunc(s, unsafe) >= 0 }

// sanitize turns \r, \t and the Unicode line and paragraph separators
// U+2028 and U+2029 (and \n unless keepNL) into spaces and drops all other
// control characters and format characters (ANSI escapes, bidi).
func sanitize(s string, keepNL bool) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' && keepNL:
			return r
		case r == '\r' || r == '\n' || r == '\t' || unicode.In(r, unicode.Zl, unicode.Zp):
			return ' '
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
}

// Line makes s one clean line of at most max runes and appends "…" when it
// cuts. The "…" is not counted in max, so the result can be max+1 runes. A
// negative max is treated as 0.
func Line(s string, max int) string {
	max = max0(max)
	s = sanitize(s, false)
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// Block cleans s but keeps its newlines, and keeps at most max bytes, cutting
// on a rune boundary and appending "…" when it cuts. The "…" is not counted in
// max. A negative max is treated as 0.
func Block(s string, max int) string {
	max = max0(max)
	s = sanitize(s, true)
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
