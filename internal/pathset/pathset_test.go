package pathset

import (
	"math/rand"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
)

func TestCleanRejects(t *testing.T) {
	bad := []string{
		"",
		"src\\a.go",
		"/etc/x",
		"C:/x",
		"a/../b",
		".",
		"./",
		strings.Repeat("a", 257),
		strings.Repeat("{a,b}", 9),
		"[a",
		"{a,b/c}/x",
		"a[/]b",
		"a[!x]b",
		"[^a]",
		"a[--0]b",
		"{..,a}/x",
	}
	for _, p := range bad {
		if got, err := Clean(p); err == nil {
			t.Errorf("Clean(%q) = %q, nil; want error", p, got)
		} else if err.Error() == "" {
			t.Errorf("Clean(%q): empty error message", p)
		}
	}
}

func TestCleanAccepts(t *testing.T) {
	cases := map[string]string{
		"./a//b.go":                "a/b.go",
		"internal/plan/**":         "internal/plan/**",
		"{a,b}/*.go":               "{a,b}/*.go",
		"[a-z].go":                 "[a-z].go",
		"a[a-]b":                   "a[a-]b",
		strings.Repeat("a", 256):   strings.Repeat("a", 256),
		strings.Repeat("{a,b}", 8): strings.Repeat("{a,b}", 8),
	}
	for in, want := range cases {
		got, err := Clean(in)
		if err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v; want %q, nil", in, got, err, want)
		}
	}
}

func TestHasMeta(t *testing.T) {
	for p, want := range map[string]bool{
		"a.go": false, "a/b,c]": false, "*.go": true, "?.go": true, "[ab]": true, "{a,b}": true,
	} {
		if got := HasMeta(p); got != want {
			t.Errorf("HasMeta(%q) = %v; want %v", p, got, want)
		}
	}
}

func TestMatch(t *testing.T) {
	if !Match("internal/**", "internal/a/b.go") {
		t.Error("Match(internal/**, internal/a/b.go) = false")
	}
	if Match("*.go", "a/b.go") {
		t.Error("Match(*.go, a/b.go) = true")
	}
	if Match("[a", "a") {
		t.Error("Match on bad pattern must be false")
	}
}

func TestMatchFold(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		want          bool
	}{
		{"[A-Z].go", "a.GO", true},
		{"[a-c]", "B", true},
		{"[a-c]", "d", false},
		{"[Z-a]", "z", true}, // Z is in the range
		{"[Z-a]", "\\", true},
		{"[Z-a]", "b", false},
		{"[a-]", "-", true},
		{"[a-]", "A", true},
		{"[k]", "\u212a", true},
		{"[\u212a]", "K", true},
		{"s*", "ſx", true},
		{"[0-\uffff]", "Q", true}, // a huge range degrades to '?'
		{"{A,b}.go", "a.GO", true},
		{"a.go", "b.go", false},
		{"*.go", "x/y.go", false},
		{"**/*.Go", "x/Y.gO", true},
		{"я*", "Я", true},
		// doublestar reads an inverted range lo-hi as the single rune lo.
		{"[z-a].go", "z.go", true},
		{"[z-a].go", "Z.GO", true},
		{"[z-a]", "a", false},
		{"[z-a]", "m", false},
		{"[c-a]", "C", true},
		{"[c-a]", "b", false},
		{"[ac-a]", "A", true},
		{"[ac-a]", "c", true},
		{"[ac-a]", "b", false},
		{"[a-c-e]", "-", true},
		{"[a-c-e]", "E", true},
		{"[a-c-e]", "d", false},
		{"[+-.]", "-", true},
		{"[Z-a]", "]", true},
		{"[Z-a]", "^", true},
		{"[j-l]", "\u212a", true}, // Kelvin sign
		{"[\u212a-\u212b]", "k", true},
		{"[r-t]", "ſ", true},
		{"[ſ]", "S", true},
		{"[ß]", "ẞ", true},
		{"ß.go", "ẞ.GO", true},
		{"[ß]", "s", false},
		{"[ı]", "I", false}, // dotless i has no simple fold
		{"[ı]", "ı", true},
		{"İ", "i", false},
		{"[ς]", "Σ", true},
		{"[σ]", "ς", true},
		{"[π-τ]", "Σ", true},
		{"[µ]", "μ", true},
	} {
		if got := matchFold(c.pattern, c.name); got != c.want {
			t.Errorf("matchFold(%q, %q) = %v; want %v", c.pattern, c.name, got, c.want)
		}
		if strings.Contains(c.pattern, "\uffff") {
			continue // widened to '?' on purpose
		}
		if got, ok := variantMatch(c.pattern, c.name); ok && got != c.want {
			t.Errorf("variantMatch(%q, %q) = %v; the case table is wrong", c.pattern, c.name, got)
		}
		if got := refMatchFold(c.pattern, c.name); got != c.want {
			t.Errorf("refMatchFold(%q, %q) = %v; want %v", c.pattern, c.name, got, c.want)
		}
	}
}

// orbit returns the simple case-folding orbit of r, r first.
func orbit(r rune) []rune {
	o := []rune{r}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		o = append(o, f)
	}
	return o
}

// variantMatch reports whether doublestar matches pattern against some case
// variant of name, enumerating the variants; ok is false when there are
// more than 1<<12.
func variantMatch(pattern, name string) (match, ok bool) {
	vs := []string{""}
	for _, r := range name {
		var next []string
		for _, v := range vs {
			for _, f := range orbit(r) {
				next = append(next, v+string(f))
			}
		}
		if vs = next; len(vs) > 1<<12 {
			return false, false
		}
	}
	for _, v := range vs {
		if ok, err := doublestar.Match(pattern, v); err == nil && ok {
			return true, true
		}
	}
	return false, true
}

// refMatchFold is the reference for matchFold, independent of foldPattern:
// it reports whether doublestar matches pattern (passed Clean) against some
// case variant of name. The pattern is closed under case variants: every
// literal rune becomes the class of its orbit, and every class gains the
// orbits of its members, the members found by asking doublestar rune by
// rune (refClassScan), so the class syntax is never parsed here.
func refMatchFold(pattern, name string) bool {
	ok, err := doublestar.Match(refClosure(pattern), name)
	return err == nil && ok
}

// refClassScan bounds the runes refClosure tries against a class; every
// simple case-folding orbit lies below it.
const refClassScan = 0x20000

var refClassCache = map[string]string{}

func refClosure(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); {
		switch p[i] {
		case '*', '?', '{', ',', '}', '/':
			b.WriteByte(p[i])
			i++
		case '[':
			end := i + strings.IndexByte(p[i:], ']') // Clean: no escapes, no ']' member
			b.WriteString(refClassClosure(p[i : end+1]))
			i = end + 1
		default:
			r, n := utf8.DecodeRuneInString(p[i:])
			i += n
			if o := orbit(r); len(o) > 1 {
				b.WriteString(refRuneClass(o))
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func refClassClosure(class string) string {
	if c, ok := refClassCache[class]; ok {
		return c
	}
	in := map[rune]bool{}
	for r := range rune(refClassScan) {
		if r == '/' || !utf8.ValidRune(r) {
			continue
		}
		if ok, err := doublestar.Match(class, string(r)); err == nil && ok {
			in[r] = true
		}
	}
	var extra []rune
	for r := range in {
		for _, f := range orbit(r) {
			if !in[f] && !slices.Contains(extra, f) {
				extra = append(extra, f)
			}
		}
	}
	c := class
	if len(extra) > 0 {
		slices.Sort(extra)
		c = "{" + class + "," + refRuneClass(extra) + "}"
	}
	refClassCache[class] = c
	return c
}

// refRuneClass is the class of the runes rs, every rune escaped.
func refRuneClass(rs []rune) string {
	var b strings.Builder
	b.WriteByte('[')
	for _, r := range rs {
		b.WriteByte('\\')
		b.WriteRune(r)
	}
	b.WriteByte(']')
	return b.String()
}

func TestOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"a.go", "a.go", true},
		{"a.go", "b.go", false},
		{"internal/plan/**", "internal/plan/x.go", true},
		{"internal/**", "internal/plan/*.go", true},
		{"cmd/*.go", "cmd/*_test.go", true},
		{"src/*.go", "src/*.ts", false},
		{"a/*", "a/b/*", false},
		{"a/**", "b/**", false},
		{"**/package.json", "web/package.json", true},
		{"{a,b}/x.go", "b/x.go", true},
		{"{a,b}/*.go", "a/*.go", true},
		{"*{.go,.ts}", "*.ts", true},
		{"?.go", "ab.go", false},
		{"[ab].go", "a.go", true},
		{"**/*.go", "docs/x.md", false},
		{"internal/hooks/testdata/base_*", "internal/hooks/testdata/plan_*", true},
		// Brace expansion can splice a globstar: these match p/q/x, x/p/q/y, ab/c.
		{"{*,a}*/x", "*/*/x", true},
		{"x/{*,a}*/y", "x/*/*/y", true},
		{"a{*,b}*", "a*/*", true},
		// Literals compare and match case-insensitively (spec 4.1).
		{"README.md", "readme.md", true},
		{"Docs/**", "docs/x.md", true},
		{"DOCS/*.md", "docs/*.MD", true},
		{"src/*.GO", "src/*_test.go", true},
		{"src/*.go", "SRC/*.TS", false},
		{"[A-Z].go", "a.go", true},
		{"[a-c]/x", "B/X", true},
		{"ſ.go", "S.GO", true},      // U+017F folds to s
		{"\u212a.go", "k.go", true}, // Kelvin sign folds to k
		{"[\u212a]/*", "K/*", true},
		{"a/{X,y}.go", "A/x.go", true},
		{"a/b.go", "A/C.go", false},
		// Inverted ranges, a trailing '-', and orbits beyond ASCII.
		{"[z-a].go", "z.go", true},
		{"[z-a].go", "Z.go", true},
		{"[z-a].go", "a.go", false},
		{"[z-a]/*.go", "Z/*.go", true},
		{"[z-a]/*.go", "[a-y]/*.go", true}, // false positive: rule 3 never compares two classes
		{"[ac-a]/x", "C/x", true},
		{"[ac-a]/x", "b/x", false},
		{"[c-a]", "b", false},
		{"[a-]", "-", true},
		{"[j-l].go", "\u212a.go", true},
		{"[ß]x", "ẞX", true},
		{"[ς]", "Σ", true},
		{"ı", "I", false},
		{"İ", "i", false},
	}
	for _, c := range cases {
		if got := Overlap(c.a, c.b); got != c.want {
			t.Errorf("Overlap(%q, %q) = %v; want %v", c.a, c.b, got, c.want)
		}
		if got := Overlap(c.b, c.a); got != c.want {
			t.Errorf("Overlap(%q, %q) = %v; want %v", c.b, c.a, got, c.want)
		}
	}
}

// TestOverlapNoMisses checks the "no misses" property: if some path matches
// both patterns, Overlap must report true.
func TestOverlapNoMisses(t *testing.T) {
	patSegs := []string{"a", "b", "*", "**", "*.go", "*_test.go", "*.ts", "x.go", "{a,b}", "?.go", "[ab]",
		"{*,a}*", "a{*,b}*", "x{,*}*"}
	pathSegs := []string{"a", "b", "x.go", "a_test.go", "b.ts", "ab.go"}

	var paths []string
	var gen func(prefix string, depth int)
	gen = func(prefix string, depth int) {
		for _, s := range pathSegs {
			p := s
			if prefix != "" {
				p = prefix + "/" + s
			}
			paths = append(paths, p)
			if depth < 4 {
				gen(p, depth+1)
			}
		}
	}
	gen("", 1)

	cache := map[string][]bool{}
	matches := func(pat string) []bool {
		if m, ok := cache[pat]; ok {
			return m
		}
		m := make([]bool, len(paths))
		for i, p := range paths {
			m[i] = Match(pat, p)
		}
		cache[pat] = m
		return m
	}

	rng := rand.New(rand.NewSource(20261020))
	randPattern := func() string {
		n := 1 + rng.Intn(3)
		segs := make([]string, n)
		for i := range segs {
			segs[i] = patSegs[rng.Intn(len(patSegs))]
		}
		return strings.Join(segs, "/")
	}

	for range 3000 {
		a, b := randPattern(), randPattern()
		ma, mb := matches(a), matches(b)
		for j := range paths {
			if ma[j] && mb[j] {
				if !Overlap(a, b) {
					t.Fatalf("Overlap(%q, %q) = false, but both match %q", a, b, paths[j])
				}
				break
			}
		}
	}
}

func TestTouches(t *testing.T) {
	cases := []struct {
		owns []string
		hot  string
		want bool
	}{
		{[]string{"go.mod"}, "go.mod", true},
		{[]string{"internal/**"}, "go.mod", false},
		{[]string{"internal/**"}, "**/package.json", false},
		{[]string{"web/package.json"}, "**/package.json", true},
		{[]string{"*"}, "go.mod", true},
		{[]string{"**/migrations/**"}, "**/migrations/**", true},
		// Case-insensitive, like Overlap (spec 4.1, 4.2).
		{[]string{"GO.MOD"}, "go.mod", true},
		{[]string{"Web/Package.json"}, "**/package.json", true},
		{[]string{"*.MOD"}, "go.mod", true},
		{[]string{"[A-Z]o.mod"}, "go.mod", true},
		{[]string{"**/Migrations/**"}, "**/migrations/**", true},
		{[]string{"\u212a.mod"}, "k.mod", true}, // Kelvin sign folds to k
		{[]string{"GO.SUM"}, "go.mod", false},
		{[]string{"Internal/**"}, "go.mod", false},
	}
	for _, c := range cases {
		if got := Touches(c.owns, c.hot); got != c.want {
			t.Errorf("Touches(%q, %q) = %v; want %v", c.owns, c.hot, got, c.want)
		}
	}
}

func TestCleanBraceBound(t *testing.T) {
	// 13^8 alternatives: one doublestar.Match on it takes tens of seconds.
	alts := "{" + strings.Repeat("a,", 12) + "a}"
	hostile := strings.Repeat(alts, 8)
	if _, err := Clean(hostile); err == nil || err.Error() != "pattern expands to more than 256 alternatives" {
		t.Errorf("Clean(%q) error = %v; want the expansion bound", hostile, err)
	}
	// A nested group adds to its alternative: (1 + 2) × 2^7 = 384.
	nested := "{a,{b,c}}" + strings.Repeat("{a,b}", 7)
	if _, err := Clean(nested); err == nil {
		t.Errorf("Clean(%q) accepted; want the expansion bound", nested)
	}
	// 257 = 2^8 + 1: one past the bound.
	over := "{" + strings.Repeat("{a,b}", 8) + ",c}"
	if _, err := Clean(over); err == nil {
		t.Errorf("Clean(%q) accepted; want the expansion bound", over)
	}
	for p, want := range map[string]int{
		"a":                                      1,
		"{a,b}":                                  2,
		"{,a}":                                   2,
		"{a,{b,c}}":                              3,
		"{a,b}{c,d}":                             4,
		"{a,{b,c}{d,e}}x{f,g}":                   10,
		"{a,{b,c}}{a,{b,c}}{a,{b,c}}{a,b,c,d,e}": 135,
		"{[,]a,b}":                               2,
		"a,b":                                    1,
		strings.Repeat("{a,b}", 8):               256,
	} {
		if got := expansion(p); got != want {
			t.Errorf("expansion(%q) = %d; want %d", p, got, want)
		}
		if _, err := Clean(p); err != nil {
			t.Errorf("Clean(%q) = %v; want nil", p, err)
		}
	}
	// Commas outside groups and inside classes are literal.
	for _, ok := range []string{
		strings.Repeat("{a,b}", 8), // exactly 256
		"a,b,c,d/" + strings.Repeat("{a,b}", 8),
		"{[,.]x,y}" + strings.Repeat("{a,b}", 7),
	} {
		if _, err := Clean(ok); err != nil {
			t.Errorf("Clean(%q) = %v; want nil", ok, err)
		}
	}
}

func TestCleanRuneLength(t *testing.T) {
	if _, err := Clean(strings.Repeat("я", 150)); err != nil {
		t.Errorf("Clean(150 × я) = %v; want nil", err)
	}
	if _, err := Clean(strings.Repeat("я", 256)); err != nil {
		t.Errorf("Clean(256 × я) = %v; want nil", err)
	}
	_, err := Clean(strings.Repeat("я", 257))
	if err == nil || err.Error() != "path longer than 256 characters" {
		t.Errorf("Clean(257 × я) error = %v; want the length limit", err)
	}
}

// specOverlap is spec 4.1 written down rule by rule, the reference for the
// compiled implementation.
func specOverlap(a, b string) bool {
	litA, litB := !HasMeta(a), !HasMeta(b)
	switch {
	case litA && litB:
		return strings.EqualFold(a, b)
	case litA:
		return refMatchFold(b, a)
	case litB:
		return refMatchFold(a, b)
	}
	sa, sb := strings.Split(a, "/"), strings.Split(b, "/")
	if SpansSegments(a) || SpansSegments(b) {
		prefix := func(segs []string) []string {
			for i, s := range segs {
				if HasMeta(s) {
					return segs[:i]
				}
			}
			return segs
		}
		pa, pb := prefix(sa), prefix(sb)
		if len(pa) > len(pb) {
			pa, pb = pb, pa
		}
		for i := range pa {
			if !strings.EqualFold(pa[i], pb[i]) {
				return false
			}
		}
		return true
	}
	if len(sa) != len(sb) {
		return false
	}
	starLit := func(s string) (string, bool) {
		if len(s) > 0 && s[0] == '*' && !HasMeta(s[1:]) {
			return s[1:], true
		}
		return "", false
	}
	for i := range sa {
		s, t := sa[i], sb[i]
		switch {
		case !HasMeta(s):
			if !refMatchFold(t, s) {
				return false
			}
		case !HasMeta(t):
			if !refMatchFold(s, t) {
				return false
			}
		default:
			ls, okS := starLit(s)
			lt, okT := starLit(t)
			if okS && okT && !hasSuffixFold(ls, lt) && !hasSuffixFold(lt, ls) {
				return false
			}
		}
	}
	return true
}

// hasSuffixFold reports whether s ends in a case variant of suffix: simple
// case folding maps rune to rune, so the runes are compared from the end.
func hasSuffixFold(s, suffix string) bool {
	rs, rf := []rune(s), []rune(suffix)
	if len(rf) > len(rs) {
		return false
	}
	return strings.EqualFold(string(rs[len(rs)-len(rf):]), suffix)
}

// segmentPool builds random patterns covering every branch of spec 4.1:
// literals, `**`, `*` next to brace punctuation, `*<lit>`, classes, `?`.
// TestOverlapNoMissesFolded checks the "no misses" property under case
// folding: if a path matching one pattern and a path matching the other are
// equal under strings.EqualFold (one file on macOS and Windows), Overlap
// must report true.
func TestOverlapNoMissesFolded(t *testing.T) {
	noMissesFolded(t, []string{"a", "A", "b", "*", "**", "*.go", "*.GO", "*_TEST.go", "*.ts", "X.go", "{A,b}",
		"?.Go", "[ab]", "[A-Z]", "[Z-a]", "[k]", "\u212a", "ſ*", "S", "{*,A}*", "x{,*}*", "[0-\uffff]"},
		[]string{"a", "A", "b", "x.go", "X.GO", "a_test.go", "B.TS", "K", "k", "\u212a", "s", "ſ", "\\"},
		3, 20261021, 500)
}

// TestOverlapNoMissesFoldedClasses is the same property for inverted ranges,
// a trailing '-' and the non-ASCII orbits (ß/ẞ, σ/ς/Σ, µ, the dotless and
// dotted i), on shallower paths.
func TestOverlapNoMissesFoldedClasses(t *testing.T) {
	noMissesFolded(t, []string{"[z-a]", "[c-a]", "[ac-a]", "[a-]", "[a-c-e]", "[j-l]", "[r-t]*", "ß", "[ß]",
		"ı", "İ", "[ς]", "Σ*", "[µ]", "[Z-a]", "*", "**", "Z", "c", "-", "[A-Z]", "{z,C}"},
		[]string{"a", "z", "Z", "c", "C", "b", "-", "e", "k", "\u212a", "ſ", "ẞ", "ß", "I", "i", "ı", "İ",
			"σ", "ς", "Μ", "]", "^"},
		2, 20261022, 300)
}

func noMissesFolded(t *testing.T, patSegs, pathSegs []string, depth int, seed int64, minHits int) {
	t.Helper()

	var paths []string
	var gen func(prefix string, d int)
	gen = func(prefix string, d int) {
		for _, s := range pathSegs {
			p := s
			if prefix != "" {
				p = prefix + "/" + s
			}
			paths = append(paths, p)
			if d < depth {
				gen(p, d+1)
			}
		}
	}
	gen("", 1)

	// class[i] numbers the EqualFold class of paths[i].
	class := make([]int, len(paths))
	var reps []string
	for i, p := range paths {
		class[i] = -1
		for c, r := range reps {
			if strings.EqualFold(p, r) {
				class[i] = c
				break
			}
		}
		if class[i] < 0 {
			class[i] = len(reps)
			reps = append(reps, p)
		}
	}

	cache := map[string][]bool{}
	classes := func(pat string) []bool { // classes with a path matching pat
		if m, ok := cache[pat]; ok {
			return m
		}
		m := make([]bool, len(reps))
		for i, p := range paths {
			if Match(pat, p) {
				m[class[i]] = true
			}
		}
		cache[pat] = m
		return m
	}

	rng := rand.New(rand.NewSource(seed))
	randPattern := func() string {
		n := 1 + rng.Intn(3)
		segs := make([]string, n)
		for i := range segs {
			segs[i] = patSegs[rng.Intn(len(patSegs))]
		}
		return strings.Join(segs, "/")
	}

	hits := 0
	for range 5000 {
		a, b := randPattern(), randPattern()
		ma, mb := classes(a), classes(b)
		for c := range reps {
			if ma[c] && mb[c] {
				hits++
				if !Overlap(a, b) {
					t.Fatalf("Overlap(%q, %q) = false, but both match a case variant of %q", a, b, reps[c])
				}
				break
			}
		}
	}
	if hits < minHits {
		t.Fatalf("only %d overlapping pairs; the generator is too weak", hits)
	}
}

var segmentPool = []string{
	"a", "b", "x.go", "y_test.go", "*", "**", "*.go", "*_test.go", "*.ts", "x*",
	"{p,q}", "{p,q}.go", "{*,a}", "{a,b}*", "*{.go,.ts}", "[ab]", "[ab].go", "x?",
	"?.go", "{x,y}[ab].go", "p", "q.go", "a.go",
	"A", "X.GO", "*.GO", "*_TEST.go", "[A-Z]", "ſ", "S", "\u212a.go",
	"[z-a]", "[ac-a].go", "[a-]", "Z", "C.go", "-", "ẞ", "ß", "ı", "İ", "I", "[ς]", "Σ", "[j-l]", "k",
}

// TestCompiledMatchesSpec pins Compile(a).Overlaps(Compile(b)) to the rule
// by rule reading of spec 4.1 on random patterns (fixed seed).
func TestCompiledMatchesSpec(t *testing.T) {
	r := rand.New(rand.NewSource(41))
	var ps []string
	for len(ps) < 600 {
		n := 1 + r.Intn(4)
		segs := make([]string, n)
		for i := range segs {
			segs[i] = segmentPool[r.Intn(len(segmentPool))]
		}
		if c, err := Clean(strings.Join(segs, "/")); err == nil {
			ps = append(ps, c)
		}
	}
	ps = append(ps, "a/*.e1_2_3", "a/*.e1_2_4", "a/b/c/d/e/f/g/h/*/x1/{p,q}[ab].go",
		"a/b/c/d/e/f/g/h/*/x2/{p,q}[ab].go", "a/b/c/d/e/f/g/h/*/x1/pa.go", "a", "a/**")
	cs := make([]Compiled, len(ps))
	for i, p := range ps {
		cs[i] = Compile(p)
	}
	for i := range cs {
		for j := range cs {
			if got, want := cs[i].Overlaps(cs[j]), specOverlap(ps[i], ps[j]); got != want {
				t.Fatalf("Compile(%q).Overlaps(%q) = %v, spec 4.1 = %v", ps[i], ps[j], got, want)
			}
			if got, want := Overlap(ps[i], ps[j]), cs[i].Overlaps(cs[j]); got != want {
				t.Fatalf("Overlap(%q, %q) = %v, Compiled = %v", ps[i], ps[j], got, want)
			}
		}
	}
}

func TestSpansSegments(t *testing.T) {
	for p, want := range map[string]bool{
		"a/**": true, "{*,a}*": true, "a{*,b}": true, "x,*": true, "a/*.go": false, "{a,b}/*.go": false,
	} {
		if got := SpansSegments(p); got != want {
			t.Errorf("SpansSegments(%q) = %v; want %v", p, got, want)
		}
	}
}
