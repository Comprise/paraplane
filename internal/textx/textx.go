// Package textx cleans text that may come from a plan, a config or a YAML
// error before the plugin shows it: on the terminal or in Claude's context.
package textx

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// sanitize turns \r and \t (and \n unless keepNL) into spaces and drops all
// other control characters and format characters (ANSI escapes, bidi).
func sanitize(s string, keepNL bool) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' && keepNL:
			return r
		case r == '\r' || r == '\n' || r == '\t':
			return ' '
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
}

// Line makes s one clean line of at most max runes and appends "…" when it
// cuts.
func Line(s string, max int) string {
	s = sanitize(s, false)
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// Block cleans s but keeps its newlines, and keeps at most max bytes, cutting
// on a rune boundary and appending "…" when it cuts.
func Block(s string, max int) string {
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
