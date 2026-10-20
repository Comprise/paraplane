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
