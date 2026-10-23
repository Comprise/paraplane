package layout

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"paraplane/internal/pathset"
	"paraplane/internal/plan"
)

// defaultHot is the default hot_files list of spec section 9.
var defaultHot = []string{
	"go.mod", "go.sum", "**/package.json", "**/package-lock.json", "**/pnpm-lock.yaml",
	"**/yarn.lock", "**/Cargo.toml", "**/Cargo.lock", "**/pyproject.toml", "**/uv.lock",
	"**/poetry.lock", "**/migrations/**",
}

// mk builds a plan from waves of tasks.
func mk(waves ...[]plan.Task) *plan.Plan {
	p := &plan.Plan{Version: 1}
	for i, ts := range waves {
		p.Waves = append(p.Waves, plan.Wave{ID: fmt.Sprintf("W%d", i+1), Tasks: ts})
	}
	return p
}

func task(id string, owns, reads, depends []string) plan.Task {
	return plan.Task{ID: id, Title: "task " + id, Owns: owns, Reads: reads, Depends: depends}
}

func s(xs ...string) []string { return xs }

// conflicts is ConflictsWithin without a deadline.
func conflicts(p *plan.Plan, hot []string) []Conflict {
	cs, err := ConflictsWithin(p, hot, time.Time{})
	if err != nil {
		panic(err)
	}
	return cs
}

func TestConflicts(t *testing.T) {
	cases := []struct {
		name string
		p    *plan.Plan
		want []Conflict
	}{
		{
			name: "owns",
			p: mk([]plan.Task{
				task("T1", s("a/x.go", "internal/**"), nil, nil),
				task("T2", s("b/y.go", "internal/plan/*.go"), nil, nil),
			}),
			want: []Conflict{{A: "T1", B: "T2", Kind: OwnsOverlap, Path: "internal/**", With: "internal/plan/*.go"}},
		},
		{
			name: "owns across waves keeps author order",
			p: mk(
				[]plan.Task{task("T1", s("a/x.go"), nil, nil)},
				[]plan.Task{task("T2", s("a/*.go"), nil, s("T1"))},
			),
			want: []Conflict{{A: "T1", B: "T2", Kind: OwnsOverlap, Path: "a/x.go", With: "a/*.go"}},
		},
		{
			name: "reads of the later task",
			p: mk([]plan.Task{
				task("T1", s("internal/plan/**"), nil, nil),
				task("T2", s("internal/rules/**"), s("internal/plan/plan.go"), nil),
			}),
			want: []Conflict{{A: "T2", B: "T1", Kind: ReadsOwns, Path: "internal/plan/plan.go", With: "internal/plan/**"}},
		},
		{
			name: "reads of the earlier task",
			p: mk([]plan.Task{
				task("T1", s("internal/rules/**"), s("internal/plan/plan.go"), nil),
				task("T2", s("internal/plan/**"), nil, nil),
			}),
			want: []Conflict{{A: "T1", B: "T2", Kind: ReadsOwns, Path: "internal/plan/plan.go", With: "internal/plan/**"}},
		},
		{
			name: "reads in both directions",
			p: mk([]plan.Task{
				task("T1", s("a/**"), s("b/x.go"), nil),
				task("T2", s("b/**"), s("a/y.go"), nil),
			}),
			want: []Conflict{
				{A: "T1", B: "T2", Kind: ReadsOwns, Path: "b/x.go", With: "b/**"},
				{A: "T2", B: "T1", Kind: ReadsOwns, Path: "a/y.go", With: "a/**"},
			},
		},
		{
			name: "hot go.mod",
			p: mk([]plan.Task{
				task("T1", s("a/**", "go.mod"), nil, nil),
				task("T2", s("b/**", "go.mod"), nil, nil),
			}),
			want: []Conflict{
				{A: "T1", B: "T2", Kind: OwnsOverlap, Path: "go.mod", With: "go.mod"},
				{A: "T1", B: "T2", Kind: HotFile, Path: "go.mod"},
			},
		},
		{
			name: "hot migrations with different files",
			p: mk([]plan.Task{
				task("T1", s("migrations/0003_a.sql"), nil, nil),
				task("T2", s("migrations/0003_b.sql"), nil, nil),
			}),
			want: []Conflict{{A: "T1", B: "T2", Kind: HotFile, Path: "**/migrations/**"}},
		},
		{
			name: "disjoint directories with default hot files",
			p: mk([]plan.Task{
				task("T1", s("internal/a/**"), nil, nil),
				task("T2", s("internal/b/**"), nil, nil),
			}),
			want: nil,
		},
		{
			name: "same static prefix but different segment count",
			p: mk([]plan.Task{
				task("T1", s("a/*/x.go"), nil, nil),
				task("T2", s("a/*.go"), nil, nil),
			}),
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := conflicts(c.p, defaultHot)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Conflicts =\n%+v\nwant\n%+v", got, c.want)
			}
		})
	}
}

// patternPool feeds the random plans; it mixes literals, globs that span
// segments, single-segment globs and hot files.
var patternPool = []string{
	"a/x.go", "a/y.go", "a/*.go", "a/**", "a/b/z.go", "a/*/z.go",
	"b/x.go", "b/**", "b/*_test.go", "b/*.ts", "{a,b}/x.go", "c/[pq].go",
	"c/p.go", "c/q.go", "**/z.go", "*.go", "main.go", "go.mod", "go.sum",
	"web/package.json", "migrations/0001_a.sql", "migrations/0001_b.sql",
	"d/{*,e}x/f.go", "d/ex/f.go", "e/**/k.go", "e/m/k.go",
}

// bruteConflicts recomputes Conflicts straight from the pathset primitives.
func bruteConflicts(p *plan.Plan, hot []string) []Conflict {
	ts := p.Tasks()
	var out []Conflict
	first := func(as, bs []string) (string, string, bool) {
		for _, a := range as {
			for _, b := range bs {
				if a == b {
					return a, b, true
				}
			}
		}
		for _, a := range as {
			for _, b := range bs {
				if pathset.Overlap(a, b) {
					return a, b, true
				}
			}
		}
		return "", "", false
	}
	for i := range ts {
		for j := i + 1; j < len(ts); j++ {
			a, b := ts[i], ts[j]
			if x, y, ok := first(a.Owns, b.Owns); ok {
				out = append(out, Conflict{A: a.ID, B: b.ID, Kind: OwnsOverlap, Path: x, With: y})
			}
			if x, y, ok := first(a.Reads, b.Owns); ok {
				out = append(out, Conflict{A: a.ID, B: b.ID, Kind: ReadsOwns, Path: x, With: y})
			}
			if x, y, ok := first(b.Reads, a.Owns); ok {
				out = append(out, Conflict{A: b.ID, B: a.ID, Kind: ReadsOwns, Path: x, With: y})
			}
			for _, h := range hot {
				if pathset.Touches(a.Owns, h) && pathset.Touches(b.Owns, h) {
					out = append(out, Conflict{A: a.ID, B: b.ID, Kind: HotFile, Path: h})
				}
			}
		}
	}
	return out
}

func pick(r *rand.Rand, n int) []string {
	var out []string
	for range n {
		out = append(out, patternPool[r.Intn(len(patternPool))])
	}
	return out
}

// pickRich mixes patternPool with random brace, globstar and class patterns.
func pickRich(r *rand.Rand, n int) []string {
	var out []string
	for len(out) < n {
		if r.Intn(2) == 0 {
			out = append(out, patternPool[r.Intn(len(patternPool))])
		} else if c, err := pathset.Clean(randomPattern(r)); err == nil {
			out = append(out, c)
		}
	}
	return out
}

func TestConflictsMatchesPathset(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	for n := range 1000 {
		var tasks []plan.Task
		for i := range 2 + r.Intn(8) {
			tasks = append(tasks, task(fmt.Sprintf("T%d", i+1), pickRich(r, r.Intn(4)), pickRich(r, r.Intn(3)), nil))
		}
		p := mk(tasks)
		got, want := conflicts(p, defaultHot), bruteConflicts(p, defaultHot)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("plan %d: Conflicts =\n%+v\nwant\n%+v", n, got, want)
		}
	}
}

func TestConflictsFast(t *testing.T) {
	// "broad globstar" is all conflicts (empty static prefixes); the other
	// shapes share long prefixes but never overlap.
	shapes := map[string]func(w, i, k int) string{
		"disjoint dirs":   func(w, i, k int) string { return fmt.Sprintf("internal/p%d_%d/f%d/**", w, i, k) },
		"broad globstar":  func(w, i, k int) string { return fmt.Sprintf("**/f%d_%d_%d.go", w, i, k) },
		"star suffix":     func(w, i, k int) string { return fmt.Sprintf("a/*.e%d_%d_%d", w, i, k) },
		"deep same shape": func(w, i, k int) string { return fmt.Sprintf("a/b/c/d/e/f/g/h/*/x%d_%d_%d/{p,q}[ab].go", w, i, k) },
	}
	for name, shape := range shapes {
		var waves [][]plan.Task
		for w := range 4 {
			var wave []plan.Task
			for i := range 125 {
				var owns []string
				for k := range 4 {
					owns = append(owns, shape(w, i, k))
				}
				wave = append(wave, task(fmt.Sprintf("T%d_%d", w, i), owns, nil, nil))
			}
			waves = append(waves, wave)
		}
		p := mk(waves...)
		start := time.Now()
		cs := conflicts(p, defaultHot)
		d := time.Since(start)
		start = time.Now()
		SuggestWith(p, cs)
		ds := time.Since(start)
		t.Logf("%s: Conflicts %v, SuggestWith %v, %d conflicts", name, d, ds, len(cs))
		if name != "broad globstar" && len(cs) != 0 {
			t.Errorf("%s: %d conflicts, want 0", name, len(cs))
		}
		if !raceEnabled && d > 2700*time.Millisecond {
			t.Errorf("%s: Conflicts on 500 tasks took %v", name, d)
		}
	}
}

// segmentPool builds random patterns covering every branch of spec 4.1:
// literals, `**`, `*` next to brace punctuation, `*<lit>`, classes, `?`.
var segmentPool = []string{
	"a", "b", "x.go", "y_test.go", "*", "**", "*.go", "*_test.go", "*.ts", "x*",
	"{p,q}", "{p,q}.go", "{*,a}", "{a,b}*", "*{.go,.ts}", "[ab]", "[ab].go", "x?",
	"?.go", "{x,y}[ab].go", "p", "q.go", "a.go",
}

func randomPattern(r *rand.Rand) string {
	n := 1 + r.Intn(4)
	segs := make([]string, n)
	for i := range segs {
		segs[i] = segmentPool[r.Intn(len(segmentPool))]
	}
	return strings.Join(segs, "/")
}

func TestConflictsPrefersEqualPair(t *testing.T) {
	p := mk([]plan.Task{
		task("T1", s("a/**", "b/x.go"), s("c/*.go", "d/y.go"), nil),
		task("T2", s("b/*.go", "b/x.go", "d/y.go"), nil, nil),
	})
	got := conflicts(p, defaultHot)
	want := []Conflict{
		{A: "T1", B: "T2", Kind: OwnsOverlap, Path: "b/x.go", With: "b/x.go"},
	}
	if len(got) < 1 || got[0] != want[0] {
		t.Errorf("Conflicts = %+v, want first %+v", got, want[0])
	}
}

func TestSuggestWith(t *testing.T) {
	p := mk(
		[]plan.Task{task("T1", s("a/**"), nil, nil)},
		[]plan.Task{task("T2", s("b/**"), nil, nil)},
	)
	if got, want := SuggestWith(p, nil), [][]string{{"T1", "T2"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("SuggestWith(nil) = %v, want %v", got, want)
	}
	cs := []Conflict{{A: "T2", B: "T1", Kind: ReadsOwns, Path: "a/x", With: "a/**"}}
	if got, want := SuggestWith(p, cs), [][]string{{"T1"}, {"T2"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("SuggestWith = %v, want %v", got, want)
	}
}

func TestRenderWaveIDsAvoidTaskIDs(t *testing.T) {
	p := mk(
		[]plan.Task{task("W1", s("a/**"), nil, nil)},
		[]plan.Task{task("W3", s("b/**"), nil, s("W1"))},
	)
	out := Render(p, [][]string{{"W1"}, {"W3"}})
	got, err := plan.Load([]byte(out))
	if err != nil {
		t.Fatalf("plan.Load(Render): %v\n%s", err, out)
	}
	var ids []string
	for _, w := range got.Waves {
		ids = append(ids, w.ID)
	}
	if want := []string{"W2", "W4"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("wave ids = %v, want %v", ids, want)
	}
}

func TestSuggestReorders(t *testing.T) {
	p := mk(
		[]plan.Task{task("T1", s("a/**"), nil, nil)},
		[]plan.Task{task("T2", s("b/**"), nil, s("T1"))},
		[]plan.Task{task("T3", s("c/**"), nil, nil)},
	)
	got := SuggestWith(p, conflicts(p, defaultHot))
	want := [][]string{{"T1", "T3"}, {"T2"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Suggest = %v, want %v", got, want)
	}
}

func TestSuggestKeepsConflictOrder(t *testing.T) {
	p := mk(
		[]plan.Task{task("T1", s("internal/plan/**"), nil, nil)},
		[]plan.Task{task("T2", s("internal/rules/**"), s("internal/plan/plan.go"), nil)},
	)
	got := SuggestWith(p, conflicts(p, defaultHot))
	want := [][]string{{"T1"}, {"T2"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Suggest = %v, want %v", got, want)
	}
}

func TestSuggestHotFileOrder(t *testing.T) {
	p := mk(
		[]plan.Task{task("T1", s("a/**", "go.mod"), nil, nil)},
		[]plan.Task{task("T2", s("b/**", "go.mod"), nil, s("T1"))},
		[]plan.Task{task("T3", s("migrations/0001_a.sql"), nil, nil)},
		[]plan.Task{task("T4", s("migrations/0002_b.sql"), nil, nil)},
	)
	got := SuggestWith(p, conflicts(p, defaultHot))
	want := [][]string{{"T1", "T3"}, {"T2", "T4"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Suggest = %v, want %v", got, want)
	}
}

// conflictFree reports whether t may share a wave with every task of others.
func conflictFree(t plan.Task, others []plan.Task, hot []string) bool {
	overlapAny := func(as, bs []string) bool {
		for _, a := range as {
			for _, b := range bs {
				if pathset.Overlap(a, b) {
					return true
				}
			}
		}
		return false
	}
	for _, o := range others {
		if overlapAny(t.Owns, o.Owns) || overlapAny(t.Reads, o.Owns) || overlapAny(o.Reads, t.Owns) {
			return false
		}
		for _, h := range hot {
			if pathset.Touches(t.Owns, h) && pathset.Touches(o.Owns, h) {
				return false
			}
		}
	}
	return true
}

// randomPlan generates a plan without errors: depends only on earlier waves,
// no conflicts inside a wave, every owns overlap across waves backed by a
// (here direct, so also transitive) dependency.
func randomPlan(r *rand.Rand) *plan.Plan {
	var waves [][]plan.Task
	var earlier []plan.Task
	n := 0
	for range 1 + r.Intn(6) {
		var wave []plan.Task
		for range 1 + r.Intn(4) {
			for range 20 {
				n++
				tk := task(fmt.Sprintf("T%d", n), pick(r, 1+r.Intn(3)), pick(r, r.Intn(3)), nil)
				if !conflictFree(tk, wave, defaultHot) {
					continue
				}
				deps := map[string]bool{}
				for _, e := range earlier {
					if r.Intn(5) == 0 {
						deps[e.ID] = true
					}
					for _, a := range tk.Owns {
						for _, b := range e.Owns {
							if pathset.Overlap(a, b) {
								deps[e.ID] = true
							}
						}
					}
				}
				for _, e := range earlier {
					if deps[e.ID] {
						tk.Depends = append(tk.Depends, e.ID)
					}
				}
				wave = append(wave, tk)
				break
			}
		}
		if len(wave) == 0 {
			continue
		}
		waves = append(waves, wave)
		earlier = append(earlier, wave...)
	}
	return mk(waves...)
}

func TestSuggestNeverLonger(t *testing.T) {
	r := rand.New(rand.NewSource(2026))
	shorter, ordered := 0, 0
	for n := range 500 {
		p := randomPlan(r)
		layers := SuggestWith(p, conflicts(p, defaultHot))
		if len(layers) < len(p.Waves) {
			shorter++
		}
		if len(layers) > len(p.Waves) {
			t.Fatalf("plan %d: %d layers for %d waves", n, len(layers), len(p.Waves))
		}
		layerOf := map[string]int{}
		for i, l := range layers {
			if len(l) == 0 {
				t.Fatalf("plan %d: empty layer %d", n, i)
			}
			for _, id := range l {
				if _, dup := layerOf[id]; dup {
					t.Fatalf("plan %d: %s in two layers", n, id)
				}
				layerOf[id] = i
			}
		}
		waveOf := map[string]int{}
		for i, w := range p.Waves {
			for _, tk := range w.Tasks {
				waveOf[tk.ID] = i
				if _, ok := layerOf[tk.ID]; !ok {
					t.Fatalf("plan %d: %s missing from layers", n, tk.ID)
				}
			}
		}
		if len(layerOf) != len(waveOf) {
			t.Fatalf("plan %d: %d tasks in layers, %d in plan", n, len(layerOf), len(waveOf))
		}
		for _, tk := range p.Tasks() {
			for _, d := range tk.Depends {
				if layerOf[d] >= layerOf[tk.ID] {
					t.Fatalf("plan %d: %s depends on %s but layers %d >= %d", n, tk.ID, d, layerOf[d], layerOf[tk.ID])
				}
			}
		}
		for _, c := range bruteConflicts(p, defaultHot) {
			a, b := c.A, c.B
			if waveOf[a] > waveOf[b] {
				a, b = b, a
			}
			if waveOf[a] == waveOf[b] {
				t.Fatalf("plan %d: generator produced a same-wave conflict %+v", n, c)
			}
			if layerOf[a] >= layerOf[b] {
				t.Fatalf("plan %d: conflict %+v not ordered: layers %d >= %d", n, c, layerOf[a], layerOf[b])
			}
			ordered++
		}
	}
	// The generator must exercise both reordering and conflict ordering.
	if shorter == 0 || ordered == 0 {
		t.Errorf("weak generator: %d shorter layouts, %d cross-wave conflicts", shorter, ordered)
	}
	t.Logf("%d of 500 layouts shorter, %d cross-wave conflicts checked", shorter, ordered)
}

func ptrs(ts []plan.Task) []*plan.Task {
	var out []*plan.Task
	for i := range ts {
		out = append(out, &ts[i])
	}
	return out
}

// norm drops the difference between nil and empty slices.
func norm(ts []*plan.Task) []plan.Task {
	var out []plan.Task
	for _, tk := range ts {
		c := *tk
		for _, f := range []*[]string{&c.Owns, &c.Reads, &c.Depends} {
			if len(*f) == 0 {
				*f = nil
			}
		}
		out = append(out, c)
	}
	return out
}

func TestRenderRoundTrip(t *testing.T) {
	src := "intro\n\n```paraplane\n" + `version: 1
waves:
  - id: W1
    tasks:
      - id: T1
        title: "Globs: \"quoted\" & co"
        owns: ["**/x", "{a,b}/x", "*.go"]
        reads: ["!neg", "&anchor", "c/[pq].go"]
        verify: "go test ./... # all"
  - id: W2
    tasks:
      - id: T2
        title: 'Пакет plan'
        owns: [internal/plan/plan.go]
        depends: [T1]
        verify: ""
      - id: T3
        title: "Tab\tand newline\nhere"
        owns: ["d/y.go"]
        reads: []
        depends: []
` + "```\n"
	orig, err := plan.Load([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	layers := SuggestWith(orig, conflicts(orig, defaultHot))
	if want := [][]string{{"T1", "T3"}, {"T2"}}; !reflect.DeepEqual(layers, want) {
		t.Fatalf("Suggest = %v, want %v", layers, want)
	}
	out := Render(orig, layers)
	if !strings.HasPrefix(out, "```paraplane\nversion: 1\n") || !strings.HasSuffix(out, "\n```\n") {
		t.Fatalf("Render does not fence the block:\n%s", out)
	}
	for _, p := range []string{`"**/x"`, `"{a,b}/x"`, `"*.go"`, `"internal/plan/plan.go"`, `"T1"`} {
		if !strings.Contains(out, p) {
			t.Errorf("Render output lacks %s:\n%s", p, out)
		}
	}
	got, err := plan.Load([]byte(out))
	if err != nil {
		t.Fatalf("plan.Load(Render): %v\n%s", err, out)
	}
	if got.Version != 1 || len(got.Waves) != 2 || got.Waves[0].ID != "W1" || got.Waves[1].ID != "W2" {
		t.Fatalf("waves = %+v", got.Waves)
	}
	byID := map[string]plan.Task{}
	for _, tk := range norm(orig.Tasks()) {
		byID[tk.ID] = tk
	}
	for i, l := range layers {
		w := got.Waves[i]
		if len(w.Tasks) != len(l) {
			t.Fatalf("W%d has %d tasks, want %d", i+1, len(w.Tasks), len(l))
		}
		for j, tk := range norm(ptrs(w.Tasks)) {
			if tk.ID != l[j] || !reflect.DeepEqual(tk, byID[l[j]]) {
				t.Errorf("W%d task %d = %+v, want %+v", i+1, j, tk, byID[l[j]])
			}
		}
	}
}

func TestRenderGates(t *testing.T) {
	base := func() *plan.Plan {
		return mk(
			[]plan.Task{task("T1", s("a/**"), nil, nil)},
			[]plan.Task{task("T2", s("b/**"), nil, s("T1"))},
			[]plan.Task{task("T3", s("c/**"), nil, nil)},
		)
	}
	layers := [][]string{{"T1", "T3"}, {"T2"}}
	gates := func(p *plan.Plan) (string, []string) {
		t.Helper()
		out := Render(p, layers)
		got, err := plan.Load([]byte(out))
		if err != nil {
			t.Fatalf("plan.Load(Render): %v\n%s", err, out)
		}
		var gs []string
		for _, w := range got.Waves {
			gs = append(gs, w.Gate)
		}
		return got.Gate, gs
	}

	p := base()
	if g, ws := gates(p); g != "" || !reflect.DeepEqual(ws, []string{"", ""}) {
		t.Errorf("no gates: plan %q, waves %q", g, ws)
	}

	p = base()
	p.Gate = "go test ./..."
	if g, ws := gates(p); g != "go test ./..." || !reflect.DeepEqual(ws, []string{"", ""}) {
		t.Errorf("plan gate: plan %q, waves %q", g, ws)
	}

	// The override of W3 (task T3) moves with T3 into layer 1, the latest
	// author wave of that layer.
	p = base()
	p.Gate = "go test ./..."
	p.Waves[2].Gate = "make check"
	if g, ws := gates(p); g != "go test ./..." || !reflect.DeepEqual(ws, []string{"make check", ""}) {
		t.Errorf("override: plan %q, waves %q", g, ws)
	}

	// An override equal to plan.gate is not repeated; an override of an
	// earlier author wave loses to the latest wave of the layer.
	p = base()
	p.Gate = "go test ./..."
	p.Waves[0].Gate = "make w1"
	p.Waves[2].Gate = "go test ./..."
	if g, ws := gates(p); g != "go test ./..." || !reflect.DeepEqual(ws, []string{"", ""}) {
		t.Errorf("latest wave wins: plan %q, waves %q", g, ws)
	}

	// Without plan.gate a layer gets the gate of its latest author wave.
	p = base()
	p.Waves[1].Gate = "make w2"
	if g, ws := gates(p); g != "" || !reflect.DeepEqual(ws, []string{"", "make w2"}) {
		t.Errorf("wave gate only: plan %q, waves %q", g, ws)
	}
}

// hostilePlan is a plan within the limits whose pattern pairs each cost a
// brace-backtracking Match: 500 tasks × 4 owns, 4 waves, alternating kinds.
func hostilePlan() *plan.Plan {
	braces := strings.Repeat("{a,aa}", 8) + strings.Repeat("?", 40)
	as := strings.Repeat("a", 120)
	var waves [][]plan.Task
	n := 0
	for w := range 4 {
		var wave []plan.Task
		for i := range 125 {
			var owns []string
			for k := range 4 {
				n++
				if (i+k)%2 == 0 {
					owns = append(owns, fmt.Sprintf("%s/z%04d", braces, n))
				} else {
					owns = append(owns, fmt.Sprintf("%s/*z%04d", as, n))
				}
			}
			wave = append(wave, task(fmt.Sprintf("T%d_%d", w, i), owns, nil, nil))
		}
		waves = append(waves, wave)
	}
	return mk(waves...)
}

func TestConflictsWithinDeadline(t *testing.T) {
	p := hostilePlan()
	start := time.Now()
	cs, err := ConflictsWithin(p, defaultHot, start.Add(50*time.Millisecond))
	d := time.Since(start)
	if !errors.Is(err, ErrDeadline) || cs != nil {
		t.Fatalf("ConflictsWithin = %d conflicts, %v; want nil, ErrDeadline", len(cs), err)
	}
	t.Logf("hostile plan: ErrDeadline after %v", d)
	if !raceEnabled && d > 1500*time.Millisecond {
		t.Errorf("ErrDeadline took %v", d)
	}

	// A deadline already passed fails even a small plan.
	small := mk([]plan.Task{
		task("T1", s("a/**", "go.mod"), s("b/x.go"), nil),
		task("T2", s("b/**", "go.mod"), nil, nil),
	})
	if _, err := ConflictsWithin(small, defaultHot, time.Now().Add(-time.Second)); !errors.Is(err, ErrDeadline) {
		t.Errorf("past deadline: err = %v, want ErrDeadline", err)
	}

	// A zero deadline means no limit.
	got, err := ConflictsWithin(small, defaultHot, time.Time{})
	if err != nil || !reflect.DeepEqual(got, conflicts(small, defaultHot)) || len(got) == 0 {
		t.Errorf("zero deadline: %+v, %v; want %+v", got, err, conflicts(small, defaultHot))
	}
}

func TestRenderWhitespaceGate(t *testing.T) {
	p := mk(
		[]plan.Task{task("T1", s("a/**"), nil, nil)},
		[]plan.Task{task("T2", s("b/**"), nil, s("T1"))},
	)
	layers := [][]string{{"T1"}, {"T2"}}

	// A blank plan.gate is not written; a blank wave gate falls back to it.
	p.Gate = " \t "
	p.Waves[1].Gate = "  "
	out := Render(p, layers)
	got, err := plan.Load([]byte(out))
	if err != nil {
		t.Fatalf("plan.Load(Render): %v\n%s", err, out)
	}
	if got.Gate != "" || got.Waves[0].Gate != "" || got.Waves[1].Gate != "" {
		t.Errorf("blank gates: plan %q, waves %q %q\n%s", got.Gate, got.Waves[0].Gate, got.Waves[1].Gate, out)
	}

	// A blank wave gate falls back to a real plan.gate, which is not repeated.
	p.Gate = "go test ./..."
	out = Render(p, layers)
	if got, err = plan.Load([]byte(out)); err != nil {
		t.Fatalf("plan.Load(Render): %v\n%s", err, out)
	}
	if got.Gate != "go test ./..." || got.Waves[0].Gate != "" || got.Waves[1].Gate != "" {
		t.Errorf("blank wave gate: plan %q, waves %q %q\n%s", got.Gate, got.Waves[0].Gate, got.Waves[1].Gate, out)
	}

	// A blank plan.gate does not hide a real wave gate.
	p.Gate = "   "
	p.Waves[1].Gate = "make w2"
	out = Render(p, layers)
	if got, err = plan.Load([]byte(out)); err != nil {
		t.Fatalf("plan.Load(Render): %v\n%s", err, out)
	}
	if got.Gate != "" || got.Waves[0].Gate != "" || got.Waves[1].Gate != "make w2" {
		t.Errorf("blank plan gate: plan %q, waves %q %q\n%s", got.Gate, got.Waves[0].Gate, got.Waves[1].Gate, out)
	}
}

// worstPlan pairs tasks owning the slowest patterns pathset.Clean allows
// with tasks owning long literal paths none of them match: every owns pair
// costs a multi-millisecond doublestar.Match.
func worstPlan() *plan.Plan {
	const pat = "**/*/{*a,*b}*/{*a,*b}**/*/{*a,*b}**/{*a,*b}**/{*a,*b}**/{*a,*b}**/{*a,*b}**/{*a,*b}/z%d"
	lit := strings.TrimSuffix(strings.Repeat("ab/", 84), "/") + "/x%d"
	var ts []plan.Task
	n := 0
	for i := range 40 {
		var owns []string
		for range 4 {
			n++
			if i%2 == 0 {
				owns = append(owns, fmt.Sprintf(pat, n))
			} else {
				owns = append(owns, fmt.Sprintf(lit, n))
			}
		}
		ts = append(ts, task(fmt.Sprintf("T%d", i), owns, nil, nil))
	}
	return mk(ts)
}

func TestConflictsWithinWorstMatch(t *testing.T) {
	p := worstPlan()
	for _, tk := range p.Tasks() {
		for _, o := range tk.Owns {
			if c, err := pathset.Clean(o); err != nil || c != o {
				t.Fatalf("pathset.Clean(%q) = %q, %v", o, c, err)
			}
		}
	}
	const budget = 200 * time.Millisecond
	start := time.Now()
	cs, err := ConflictsWithin(p, defaultHot, start.Add(budget))
	d := time.Since(start)
	if !errors.Is(err, ErrDeadline) || cs != nil {
		t.Fatalf("ConflictsWithin = %d conflicts, %v; want nil, ErrDeadline", len(cs), err)
	}
	t.Logf("worst plan: ErrDeadline after %v", d)
	if !raceEnabled && d > budget+300*time.Millisecond {
		t.Errorf("ErrDeadline after %v; want within %v", d, budget+300*time.Millisecond)
	}
}
