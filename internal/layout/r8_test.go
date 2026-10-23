package layout

import (
	"fmt"
	"testing"
	"time"

	"paraplane/internal/plan"
)

func compileAll(ss ...string) []pattern {
	out := make([]pattern, len(ss))
	for i, s := range ss {
		out[i] = compile(s)
	}
	return out
}

// TestFirstOverlapSame: a literally equal pair wins over an earlier
// overlapping one, the first in the order of as; without one the first
// overlapping pair is returned; a passed deadline expires.
func TestFirstOverlapSame(t *testing.T) {
	cases := []struct {
		as, bs []string
		x, y   string
		ok     bool
	}{
		{[]string{"a/**", "b/x.go", "c/y.go"}, []string{"c/y.go", "b/*.go", "b/x.go"}, "b/x.go", "b/x.go", true},
		{[]string{"a/**", "b/x.go"}, []string{"b/*.go", "a/q"}, "a/**", "a/q", true},
		{[]string{"a/*.go", "B/X.go"}, []string{"b/x.go"}, "B/X.go", "b/x.go", true},
		{[]string{"a/x"}, []string{"b/x"}, "", "", false},
		{nil, []string{"a"}, "", "", false},
	}
	for _, tc := range cases {
		bs := compileAll(tc.bs...)
		x, y, ok, exp := firstOverlap(compileAll(tc.as...), bs, patternSet(bs), &clock{})
		if x != tc.x || y != tc.y || ok != tc.ok || exp {
			t.Errorf("firstOverlap(%q, %q) = %q, %q, %v, %v; want %q, %q, %v", tc.as, tc.bs, x, y, ok, exp, tc.x, tc.y, tc.ok)
		}
	}
	bs := compileAll("a")
	if _, _, ok, exp := firstOverlap(compileAll("a"), bs, patternSet(bs), &clock{deadline: time.Now().Add(-time.Second)}); ok || !exp {
		t.Errorf("past deadline: ok %v, expired %v", ok, exp)
	}
}

func BenchmarkConflicts(b *testing.B) {
	var waves [][]plan.Task
	for w := range 4 {
		var wave []plan.Task
		for i := range 125 {
			var owns []string
			for k := range 4 {
				owns = append(owns, fmt.Sprintf("internal/p%d_%d/f%d/**", w, i, k))
			}
			wave = append(wave, task(fmt.Sprintf("T%d_%d", w, i), owns, s(fmt.Sprintf("docs/r%d_%d.md", w, i)), nil))
		}
		waves = append(waves, wave)
	}
	p := mk(waves...)
	for b.Loop() {
		if _, err := ConflictsWithin(p, defaultHot, time.Time{}); err != nil {
			b.Fatal(err)
		}
	}
}
