// Package pathset validates plan paths, decides conservatively whether two
// glob patterns may describe the same file, and detects hot-file touches.
package pathset

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	// MaxPatternLen is the maximum length of a path or pattern, in bytes.
	MaxPatternLen = 256
	// MaxBraceGroups is the maximum number of `{` groups in a pattern.
	MaxBraceGroups = 8
)

// Clean validates a plan path or glob pattern and returns it normalized by
// path.Clean. The restrictions on `{}` and `[]` guarantee that every segment
// of a pattern without `**`, and without a `*` next to `{`, `,` or `}`, matches
// exactly one path segment, which Overlap relies on. Brace expansion can
// splice a globstar (`{*,a}*` matches `p/q`), so such patterns may span
// several segments; Overlap treats them like `**`.
func Clean(p string) (string, error) {
	switch {
	case p == "":
		return "", errors.New("empty path")
	case len(p) > MaxPatternLen:
		return "", fmt.Errorf("path longer than %d characters", MaxPatternLen)
	case strings.Contains(p, `\`):
		return "", errors.New(`path contains '\'; use '/' as the separator`)
	case p[0] == '/':
		return "", errors.New("path must be relative, not start with '/'")
	case len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0]):
		return "", errors.New("path must be relative, not start with a drive letter")
	case strings.Count(p, "{") > MaxBraceGroups:
		return "", fmt.Errorf("pattern has more than %d '{' groups", MaxBraceGroups)
	case !doublestar.ValidatePattern(p):
		return "", errors.New("invalid glob pattern")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", errors.New("path contains '..'")
		}
	}
	if err := checkGroups(p); err != nil {
		return "", err
	}
	c := path.Clean(p)
	if c == "." {
		return "", errors.New("path refers to the project root '.'")
	}
	return c, nil
}

func isASCIILetter(b byte) bool {
	return 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}

// checkGroups rejects `/` inside `{…}` and `[…]`, classes that can match `/`
// and a `..` alternative in `{…}`. p has passed doublestar.ValidatePattern and
// contains no `\`, so classes and groups are well formed.
func checkGroups(p string) error {
	depth := 0
	altStart := -1 // start of the current alternative, when depth > 0
	endAlt := func(i int) error {
		if depth > 0 && p[altStart:i] == ".." {
			return errors.New("'{…}' has a '..' alternative")
		}
		return nil
	}
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '[':
			end, err := checkClass(p, i)
			if err != nil {
				return err
			}
			i = end
		case '{':
			depth++
			altStart = i + 1
		case ',':
			if err := endAlt(i); err != nil {
				return err
			}
			altStart = i + 1
		case '}':
			if err := endAlt(i); err != nil {
				return err
			}
			depth--
			altStart = i + 1
		case '/':
			if depth > 0 {
				return errors.New("'/' inside '{…}'")
			}
		}
	}
	return nil
}

// checkClass checks the class starting at p[start] == '[' and returns the
// index of its closing ']'. Ranges are parsed the way doublestar parses them.
func checkClass(p string, start int) (int, error) {
	i := start + 1
	if p[i] == '!' || p[i] == '^' {
		return 0, errors.New("negated class '[!…]' or '[^…]' can match '/'")
	}
	last := utf8.MaxRune
	for p[i] != ']' {
		r, n := utf8.DecodeRuneInString(p[i:])
		i += n
		if r == '/' {
			return 0, errors.New("'/' inside '[…]'")
		}
		if last < utf8.MaxRune && r == '-' && p[i] != ']' {
			hi, n := utf8.DecodeRuneInString(p[i:])
			i += n
			if hi == '/' || last <= '/' && '/' <= hi {
				return 0, fmt.Errorf("class range '%c-%c' can match '/'", last, hi)
			}
			last = utf8.MaxRune
			continue
		}
		last = r
	}
	return i, nil
}

// HasMeta reports whether p contains a glob metacharacter (`*?[{`).
func HasMeta(p string) bool {
	return strings.ContainsAny(p, "*?[{")
}

// Match reports whether name matches the doublestar pattern; a bad pattern
// matches nothing.
func Match(pattern, name string) bool {
	ok, err := doublestar.Match(pattern, name)
	return err == nil && ok
}

// Overlap reports whether patterns a and b (both passed Clean) may describe
// the same file. False positives are allowed, misses are not: if a path
// matches both, Overlap returns true.
func Overlap(a, b string) bool {
	litA, litB := !HasMeta(a), !HasMeta(b)
	switch {
	case litA && litB:
		return a == b
	case litA:
		return Match(b, a)
	case litB:
		return Match(a, b)
	}
	sa, sb := strings.Split(a, "/"), strings.Split(b, "/")
	if spansSegments(a) || spansSegments(b) {
		pa, pb := staticPrefix(sa), staticPrefix(sb)
		if len(pa) > len(pb) {
			pa, pb = pb, pa
		}
		for i := range pa {
			if pa[i] != pb[i] {
				return false
			}
		}
		return true
	}
	if len(sa) != len(sb) {
		return false
	}
	for i := range sa {
		if !segmentsMayMeet(sa[i], sb[i]) {
			return false
		}
	}
	return true
}

// spansSegments reports whether a segment of p may match several path
// segments: p contains `**`, or a `*` next to `{`, `,` or `}`, which brace
// expansion can splice into a globstar.
func spansSegments(p string) bool {
	if strings.Contains(p, "**") {
		return true
	}
	for _, s := range []string{"*{", "*,", "*}", "{*", ",*", "}*"} {
		if strings.Contains(p, s) {
			return true
		}
	}
	return false
}

// staticPrefix returns the segments before the first one with a metacharacter.
func staticPrefix(segs []string) []string {
	for i, s := range segs {
		if HasMeta(s) {
			return segs[:i]
		}
	}
	return segs
}

// segmentsMayMeet reports whether two single-segment patterns may match a
// common string.
func segmentsMayMeet(s, t string) bool {
	switch {
	case !HasMeta(s):
		return Match(t, s)
	case !HasMeta(t):
		return Match(s, t)
	}
	ls, okS := starLiteral(s)
	lt, okT := starLiteral(t)
	if okS && okT {
		return strings.HasSuffix(ls, lt) || strings.HasSuffix(lt, ls)
	}
	return true
}

// starLiteral returns lit for a segment of the form `*lit`, lit without
// metacharacters.
func starLiteral(s string) (string, bool) {
	if len(s) > 0 && s[0] == '*' && !HasMeta(s[1:]) {
		return s[1:], true
	}
	return "", false
}

// Touches reports whether a task owning owns touches the hot pattern hot
// (spec 4.2): some p in owns equals hot, or p is a literal matched by hot, or
// hot is a literal matched by p.
func Touches(owns []string, hot string) bool {
	hotLit := !HasMeta(hot)
	for _, p := range owns {
		if p == hot ||
			!HasMeta(p) && Match(hot, p) ||
			hotLit && Match(p, hot) {
			return true
		}
	}
	return false
}
