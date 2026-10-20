package pathset

import (
	"math/rand"
	"strings"
	"testing"
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

	for i := 0; i < 3000; i++ {
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
		return a == b
	case litA:
		return Match(b, a)
	case litB:
		return Match(a, b)
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
			if pa[i] != pb[i] {
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
			if !Match(t, s) {
				return false
			}
		case !HasMeta(t):
			if !Match(s, t) {
				return false
			}
		default:
			ls, okS := starLit(s)
			lt, okT := starLit(t)
			if okS && okT && !strings.HasSuffix(ls, lt) && !strings.HasSuffix(lt, ls) {
				return false
			}
		}
	}
	return true
}

// segmentPool builds random patterns covering every branch of spec 4.1:
// literals, `**`, `*` next to brace punctuation, `*<lit>`, classes, `?`.
var segmentPool = []string{
	"a", "b", "x.go", "y_test.go", "*", "**", "*.go", "*_test.go", "*.ts", "x*",
	"{p,q}", "{p,q}.go", "{*,a}", "{a,b}*", "*{.go,.ts}", "[ab]", "[ab].go", "x?",
	"?.go", "{x,y}[ab].go", "p", "q.go", "a.go",
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
