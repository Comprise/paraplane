// Package pathset validates plan paths, decides conservatively whether two
// glob patterns may describe the same file, and detects hot-file touches.
package pathset

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	// MaxPatternLen is the maximum length of a path or pattern, in runes.
	MaxPatternLen = 256
	// MaxBraceGroups is the maximum number of `{` groups in a pattern.
	MaxBraceGroups = 8
	// maxExpansion bounds the brace expansion of a pattern: one
	// doublestar.Match on more alternatives can take tens of seconds and
	// cannot be interrupted by a deadline.
	maxExpansion = 256
	// maxClassRunes bounds the runes of a class that foldPattern enumerates;
	// a larger class is widened to `?`.
	maxClassRunes = 256
)

// Clean validates a plan path or glob pattern and returns it normalized by
// path.Clean. The restrictions on `{}` and `[]` guarantee that every segment
// of a pattern without `**`, and without a `*` next to `{`, `,` or `}`, matches
// exactly one path segment, which Overlap relies on. Brace expansion can
// splice a globstar (`{*,a}*` matches `p/q`), so such patterns may span
// several segments; Overlap treats them like `**`. The brace expansion of
// a pattern (the number of brace-free patterns it stands for) is at most 256.
func Clean(p string) (string, error) {
	switch {
	case p == "":
		return "", errors.New("empty path")
	case utf8.RuneCountInString(p) > MaxPatternLen:
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
	if slices.Contains(strings.Split(p, "/"), "..") {
		return "", errors.New("path contains '..'")
	}
	if err := checkGroups(p); err != nil {
		return "", err
	}
	if expansion(p) > maxExpansion {
		return "", fmt.Errorf("pattern expands to more than %d alternatives", maxExpansion)
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

// expansion returns the number of brace-free patterns p expands to, capped
// at maxExpansion+1: a group counts the sum of its alternatives, a sequence
// the product of its parts. Commas outside groups and the contents of
// classes are literal. Every partial count only grows into the total, so
// capping early is exact for the comparison with maxExpansion.
func expansion(p string) int {
	capped := func(n int) int { return min(n, maxExpansion+1) }
	type group struct{ sum, cur int }
	stack := []group{{cur: 1}} // stack[0] is the pattern outside any group
	for i := 0; i < len(p); i++ {
		top := &stack[len(stack)-1]
		switch p[i] {
		case '[':
			if end := strings.IndexByte(p[i+1:], ']'); end >= 0 {
				i += 1 + end
			}
		case '{':
			stack = append(stack, group{cur: 1})
		case ',':
			if len(stack) > 1 {
				top.sum, top.cur = capped(top.sum+top.cur), 1
			}
		case '}':
			if len(stack) > 1 {
				n := capped(top.sum + top.cur)
				stack = stack[:len(stack)-1]
				parent := &stack[len(stack)-1]
				parent.cur = capped(parent.cur * n)
			}
		}
	}
	return stack[0].cur
}

// span is one member of a class as written: a rune (lo == hi), a range
// lo-hi, or an inverted range (lo > hi), which holds only lo.
type span struct{ lo, hi rune }

// classSpans parses the class starting at p[start] == '[' the way
// doublestar.Match does and returns its spans and the index of its closing
// ']'; ok is false when there is none. Every rune is a member; a rune
// followed by `-` and a rune other than `]` makes a range up to that rune
// (the end is not a member by itself), and `-` before `]` is a member. p has
// no `\` (Clean), so there are no escapes. Negation is the caller's matter.
func classSpans(p string, start int) (spans []span, end int, ok bool) {
	i := start + 1
	ranged := true // the last span cannot start a range
	for i < len(p) && p[i] != ']' {
		r, n := utf8.DecodeRuneInString(p[i:])
		i += n
		if !ranged && r == '-' && i < len(p) && p[i] != ']' {
			hi, n := utf8.DecodeRuneInString(p[i:])
			i += n
			spans[len(spans)-1].hi = hi
			ranged = true
			continue
		}
		spans = append(spans, span{r, r})
		ranged = false
	}
	if i >= len(p) {
		return nil, 0, false
	}
	return spans, i, true
}

// checkClass checks the class starting at p[start] == '[' and returns the
// index of its closing ']'. p passed doublestar.ValidatePattern, so the
// class is closed.
func checkClass(p string, start int) (int, error) {
	if p[start+1] == '!' || p[start+1] == '^' {
		return 0, errors.New("negated class '[!…]' or '[^…]' can match '/'")
	}
	spans, end, ok := classSpans(p, start)
	if !ok {
		return 0, errors.New("unclosed '['")
	}
	for _, s := range spans {
		switch {
		case s.lo == '/':
			return 0, errors.New("'/' inside '[…]'")
		case s.hi == '/' || s.lo <= '/' && '/' <= s.hi:
			return 0, fmt.Errorf("class range '%c-%c' can match '/'", s.lo, s.hi)
		}
	}
	return end, nil
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

// MatchAny reports whether name matches one of patterns (Match).
func MatchAny(patterns []string, name string) bool {
	return slices.ContainsFunc(patterns, func(p string) bool { return Match(p, name) })
}

// foldRune returns the smallest rune of the simple case-folding orbit of r,
// so foldRune(r) == foldRune(s) exactly when r and s are equal under
// strings.EqualFold. Metacharacters, `/` and `\` fold to themselves.
func foldRune(r rune) rune {
	if r < utf8.RuneSelf {
		// An ASCII orbit holds at most one upper and one lower ASCII letter
		// (k and s add non-ASCII runes above them): its minimum is the upper.
		if 'a' <= r && r <= 'z' {
			return r - ('a' - 'A')
		}
		return r
	}
	m := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		m = min(m, f)
	}
	return m
}

// foldLiteral maps every rune of s to its foldRune: two strings are equal
// under strings.EqualFold exactly when their foldLiterals are equal.
func foldLiteral(s string) string {
	return strings.Map(foldRune, s)
}

// foldCache memoizes foldPattern for patterns with a class: folding a class
// enumerates its runes, and Touches and Compile fold the same hot and owned
// patterns over and over. It is cleared when full.
var foldCache struct {
	sync.Mutex
	m map[string]string
}

const foldCacheSize = 4096

// foldPattern rewrites a pattern (passed Clean) so that it matches
// foldLiteral(x) exactly for the x equal under strings.EqualFold to a string
// the original matches: literal runes are folded, and a class becomes the
// class of the folded runes it contains, or `?` when it holds more than
// maxClassRunes runes (a class never matches `/`, so `?` only widens it).
func foldPattern(p string) string {
	if !strings.Contains(p, "[") {
		return foldLiteral(p)
	}
	foldCache.Lock()
	f, ok := foldCache.m[p]
	foldCache.Unlock()
	if ok {
		return f
	}
	f = foldPatternUncached(p)
	foldCache.Lock()
	if foldCache.m == nil || len(foldCache.m) >= foldCacheSize {
		foldCache.m = make(map[string]string)
	}
	foldCache.m[p] = f
	foldCache.Unlock()
	return f
}

func foldPatternUncached(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); {
		if p[i] != '[' {
			r, n := utf8.DecodeRuneInString(p[i:])
			b.WriteRune(foldRune(r))
			i += n
			continue
		}
		end, class := foldClass(p, i)
		b.WriteString(class)
		i = end + 1
	}
	return b.String()
}

// foldClass parses the class starting at p[start] == '[' and returns the
// index of its closing ']' and its folded form: `[…]` of the folded runes,
// escaped and merged into ranges, or `?` for a class of more than
// maxClassRunes runes. p passed Clean: no negation, no escapes, no empty
// class. The class is parsed by classSpans, like checkClass parses it; an
// inverted range adds nothing (`[z-a]` is {z}, `[ac-a]` is {a, c}). A
// malformed class (no closing ']') is copied as is.
func foldClass(p string, start int) (int, string) {
	spans, end, ok := classSpans(p, start)
	if !ok {
		return len(p) - 1, p[start:]
	}
	size := 0
	for i, s := range spans {
		if s.hi < s.lo { // an inverted range holds its start only
			spans[i].hi = s.lo
		}
		size += int(spans[i].hi-s.lo) + 1
		if size > maxClassRunes {
			return end, "?"
		}
	}
	runes := make([]rune, 0, size)
	for _, s := range spans {
		for r := s.lo; r <= s.hi; r++ {
			runes = append(runes, foldRune(r))
		}
	}
	slices.Sort(runes)
	runes = slices.Compact(runes)
	var b strings.Builder
	b.WriteByte('[')
	for j := 0; j < len(runes); {
		k := j // runes[j..k] are consecutive
		for k+1 < len(runes) && runes[k+1] == runes[k]+1 {
			k++
		}
		writeClassRune(&b, runes[j])
		if k-j >= 2 {
			b.WriteByte('-')
			writeClassRune(&b, runes[k])
			j = k + 1
		} else {
			j++
		}
	}
	b.WriteByte(']')
	return end, b.String()
}

// writeClassRune writes r as a class member or range end, escaped when
// doublestar would read it as class syntax.
func writeClassRune(b *strings.Builder, r rune) {
	if strings.ContainsRune(`\]-^!`, r) {
		b.WriteByte('\\')
	}
	b.WriteRune(r)
}

// matchFold reports whether a case variant of name (a string equal to it
// under strings.EqualFold) may match pattern (passed Clean); it never misses
// one, and may report true for a class wider than maxClassRunes.
func matchFold(pattern, name string) bool {
	return Match(foldPattern(pattern), foldLiteral(name))
}

// Overlap reports whether patterns a and b (both passed Clean) may describe
// the same file (spec 4.1), comparing and matching literals case-insensitively.
// False positives are allowed, misses are not: if a path matching a and a path
// matching b are equal under strings.EqualFold, Overlap returns true.
//
// Overlap is the reference API: it is Compile(a).Overlaps(Compile(b)), which
// callers checking many pairs use instead, and tests pin both to each other.
func Overlap(a, b string) bool {
	return Compile(a).Overlaps(Compile(b))
}

// Compiled is a pattern prepared once for repeated Overlaps checks.
type Compiled struct {
	s      string
	f      string // foldPattern(s)
	lit    bool   // no metacharacters: a literal path
	span   bool   // a segment may match several path segments (rule 2)
	segs   []string
	fsegs  []string // foldPattern of each segment
	meta   []bool   // segment has a metacharacter
	star   []string // foldLiteral of the lit of a `*lit` segment
	isStar []bool   // segment has the form `*lit`, lit without metacharacters
	prefix int      // number of leading segments without metacharacters
}

// Compile prepares p (passed Clean) for Overlaps.
func Compile(p string) Compiled {
	segs := strings.Split(p, "/")
	c := Compiled{
		s:      p,
		lit:    !HasMeta(p),
		span:   SpansSegments(p),
		segs:   segs,
		fsegs:  make([]string, len(segs)),
		meta:   make([]bool, len(segs)),
		star:   make([]string, len(segs)),
		isStar: make([]bool, len(segs)),
		prefix: len(segs),
	}
	for i, g := range segs {
		c.fsegs[i] = foldPattern(g)
		c.meta[i] = HasMeta(g)
		if c.meta[i] && c.prefix == len(segs) {
			c.prefix = i
		}
		if len(g) > 0 && g[0] == '*' && !HasMeta(g[1:]) {
			c.star[i], c.isStar[i] = foldLiteral(g[1:]), true
		}
	}
	c.f = strings.Join(c.fsegs, "/") // classes hold no '/', so folding is per segment
	return c
}

// Overlaps reports whether a and b may describe the same file (spec 4.1);
// see Overlap. Literal segments are compared folded (foldLiteral), and
// patterns are matched against literals through foldPattern.
func (a Compiled) Overlaps(b Compiled) bool {
	// Segments before the first metacharacter of either pattern are
	// literal in both and aligned; a difference there rules out every rule.
	// A literal path is compared only on its own segments, so `a` vs `a/**`
	// still reaches Match.
	for i := range min(a.prefix, b.prefix) {
		if a.fsegs[i] != b.fsegs[i] {
			return false
		}
	}
	switch {
	case a.lit && b.lit:
		return a.f == b.f
	case a.lit:
		return matchesLiteral(&b, &a)
	case b.lit:
		return matchesLiteral(&a, &b)
	case a.span || b.span:
		return true // static prefixes agree on the shorter one (checked above)
	case len(a.segs) != len(b.segs):
		return false
	}
	// Rule 3; the cheap segment pairs first, the result is a conjunction.
	for i := range a.segs {
		switch {
		case !a.meta[i] && !b.meta[i]:
			if a.fsegs[i] != b.fsegs[i] {
				return false
			}
		case a.isStar[i] && b.isStar[i]:
			if !strings.HasSuffix(a.star[i], b.star[i]) && !strings.HasSuffix(b.star[i], a.star[i]) {
				return false
			}
		}
	}
	for i := range a.segs {
		switch {
		case a.meta[i] && !b.meta[i]:
			if !Match(a.fsegs[i], b.fsegs[i]) {
				return false
			}
		case !a.meta[i] && b.meta[i]:
			if !Match(b.fsegs[i], a.fsegs[i]) {
				return false
			}
		}
	}
	return true
}

// matchesLiteral is matchFold(p, l) for a literal l. Without spanning, every
// segment of p matches exactly one path segment (Clean), so the segment
// counts must agree.
func matchesLiteral(p, l *Compiled) bool {
	if !p.span && len(p.segs) != len(l.segs) {
		return false
	}
	return Match(p.f, l.f)
}

// SpansSegments reports whether a segment of p may match several path
// segments: p contains `**`, or a `*` next to `{`, `,` or `}`, which brace
// expansion can splice into a globstar.
func SpansSegments(p string) bool {
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

// Touches reports whether a task owning owns touches the hot pattern hot
// (spec 4.2): some p in owns equals hot, or p is a literal matched by hot, or
// hot is a literal matched by p. Like Overlap, it ignores case: equality is
// strings.EqualFold and matching goes through matchFold.
func Touches(owns []string, hot string) bool {
	hotLit := !HasMeta(hot)
	for _, p := range owns {
		if strings.EqualFold(p, hot) ||
			!HasMeta(p) && matchFold(hot, p) ||
			hotLit && matchFold(p, hot) {
			return true
		}
	}
	return false
}
