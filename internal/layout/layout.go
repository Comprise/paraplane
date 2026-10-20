// Package layout finds conflicts between plan tasks, computes the shortest
// valid wave layout (spec section 6) and renders a layout as a ```paraplane
// block.
package layout

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"paraplane/internal/pathset"
	"paraplane/internal/plan"
)

// Kind is the kind of a conflict between two tasks.
type Kind string

// Conflict kinds.
const (
	OwnsOverlap Kind = "owns"  // owns of A and owns of B may overlap (E2, E6)
	ReadsOwns   Kind = "reads" // reads of A may overlap owns of B (E4)
	HotFile     Kind = "hot"   // A and B touch the same hot pattern (E5)
)

// Conflict is a reason two tasks may not share a wave.
type Conflict struct {
	// A and B are task ids. For owns and hot, A precedes B in the author's
	// order; for reads, A is the reading task and B the owning one, in
	// either order.
	A, B string
	Kind Kind
	// Path is the owns pattern of A (owns), the reads pattern of A (reads)
	// or the hot_files pattern (hot).
	Path string
	// With is the owns pattern of B (owns, reads); "" for hot.
	With string
}

// pattern is a path pattern with its compiled form.
type pattern struct {
	s string
	c pathset.Compiled
}

func compile(s string) pattern { return pattern{s: s, c: pathset.Compile(s)} }

// ErrDeadline reports that ConflictsWithin ran out of time.
var ErrDeadline = errors.New("plan is too complex to check in time; simplify brace patterns or split the plan")

// clock checks a deadline before every pattern evaluation (time.Now costs
// tens of nanoseconds, one doublestar.Match up to milliseconds); a zero
// deadline never expires.
type clock struct {
	deadline time.Time
	expired  bool
}

// tick reports whether the deadline has passed; call it before each
// evaluation.
func (c *clock) tick() bool {
	if c.deadline.IsZero() {
		return false
	}
	if !c.expired {
		c.expired = !time.Now().Before(c.deadline)
	}
	return c.expired
}

// firstOverlap returns a pair of patterns (as × bs) that may overlap,
// preferring a literally equal pair, else the first in order of as, then bs.
// expired is true when the clock ran out; the pair is then meaningless.
func firstOverlap(as, bs []pattern, c *clock) (x, y string, ok, expired bool) {
	for i := range as {
		for j := range bs {
			if c.tick() {
				return "", "", false, true
			}
			if as[i].c.Overlaps(bs[j].c) {
				if as[i].s != bs[j].s {
					for _, x := range as[i:] {
						for _, y := range bs {
							if x.s == y.s {
								return x.s, y.s, true, false
							}
						}
					}
					// Equal pairs before as[i] would have overlapped.
				}
				return as[i].s, bs[j].s, true, false
			}
		}
	}
	return "", "", false, false
}

// prepared is a task with its patterns split and its hot touches known.
type prepared struct {
	t     *plan.Task
	owns  []pattern
	reads []pattern
	hot   []int // indices into the hot list the task touches
}

func prepare(p *plan.Plan, hot []string, c *clock) ([]prepared, bool) {
	ts := p.Tasks()
	out := make([]prepared, len(ts))
	for i, t := range ts {
		pt := prepared{t: t}
		for _, s := range t.Owns {
			pt.owns = append(pt.owns, compile(s))
		}
		for _, s := range t.Reads {
			pt.reads = append(pt.reads, compile(s))
		}
		for h, hs := range hot {
			// Touches(owns, H) holds when it holds for some single pattern;
			// one call per pattern keeps every Match under the clock.
			for _, o := range t.Owns {
				if c.tick() {
					return nil, true
				}
				if pathset.Touches([]string{o}, hs) {
					pt.hot = append(pt.hot, h)
					break
				}
			}
		}
		out[i] = pt
	}
	return out, false
}

// ConflictsWithin returns the conflicts of every pair of tasks of p, in the
// author's order of pairs. For each pair it reports at most one owns
// conflict, one reads conflict per direction and one hot conflict per hot
// pattern both tasks touch; Path/With are a literally equal pattern pair if
// there is one, else the first overlapping pair. Paths must have passed
// pathset.Clean.
//
// It checks the clock before every pattern evaluation and returns nil,
// ErrDeadline once the deadline has passed. A zero deadline means no
// limit.
func ConflictsWithin(p *plan.Plan, hot []string, deadline time.Time) ([]Conflict, error) {
	c := &clock{deadline: deadline}
	ts, expired := prepare(p, hot, c)
	if expired {
		return nil, ErrDeadline
	}
	var out []Conflict
	for i := range ts {
		a := &ts[i]
		for j := i + 1; j < len(ts); j++ {
			b := &ts[j]
			x, y, ok, exp := firstOverlap(a.owns, b.owns, c)
			if exp {
				return nil, ErrDeadline
			}
			if ok {
				out = append(out, Conflict{A: a.t.ID, B: b.t.ID, Kind: OwnsOverlap, Path: x, With: y})
			}
			x, y, ok, exp = firstOverlap(a.reads, b.owns, c)
			if exp {
				return nil, ErrDeadline
			}
			if ok {
				out = append(out, Conflict{A: a.t.ID, B: b.t.ID, Kind: ReadsOwns, Path: x, With: y})
			}
			x, y, ok, exp = firstOverlap(b.reads, a.owns, c)
			if exp {
				return nil, ErrDeadline
			}
			if ok {
				out = append(out, Conflict{A: b.t.ID, B: a.t.ID, Kind: ReadsOwns, Path: x, With: y})
			}
			for _, h := range sharedHot(a.hot, b.hot) {
				out = append(out, Conflict{A: a.t.ID, B: b.t.ID, Kind: HotFile, Path: hot[h]})
			}
		}
	}
	return out, nil
}

// sharedHot intersects two ascending index lists.
func sharedHot(a, b []int) []int {
	var out []int
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	return out
}

// SuggestWith returns the shortest layout of spec section 6 for the
// conflicts cs of p: tasks are placed on the layer after their latest
// predecessor, where predecessors are the depends plus the earlier task of
// every conflicting pair from different waves. Layers list task ids in the
// author's order. p must have no errors.
func SuggestWith(p *plan.Plan, cs []Conflict) [][]string {
	ts := p.Tasks()
	index := make(map[string]int, len(ts))
	wave := make([]int, len(ts))
	k := 0
	for w := range p.Waves {
		for range p.Waves[w].Tasks {
			wave[k] = w
			k++
		}
	}
	for i, t := range ts {
		index[t.ID] = i
	}
	preds := make([][]int, len(ts))
	for i, t := range ts {
		for _, d := range t.Depends {
			if j, ok := index[d]; ok && j != i {
				preds[i] = append(preds[i], j)
			}
		}
	}
	for _, c := range cs {
		a, okA := index[c.A]
		b, okB := index[c.B]
		if !okA || !okB {
			continue
		}
		switch {
		case wave[a] < wave[b]:
			preds[b] = append(preds[b], a)
		case wave[b] < wave[a]:
			preds[a] = append(preds[a], b)
		}
	}
	// Every arc points to a strictly earlier author wave, so the author's
	// order is a topological order.
	layer := make([]int, len(ts))
	var out [][]string
	for i, t := range ts {
		for _, j := range preds[i] {
			if j < i {
				layer[i] = max(layer[i], layer[j]+1)
			}
		}
		for len(out) <= layer[i] {
			out = append(out, nil)
		}
		out[layer[i]] = append(out[layer[i]], t.ID)
	}
	return out
}

// Render writes layers as a ```paraplane block (fences included): waves
// W1… (skipping numbers taken by task ids), the tasks of p unchanged, every string double-quoted. plan.gate is
// written at the top; a layer gets the effective gate (wave.gate, then
// plan.gate) of the latest author wave among its tasks, written only when
// it differs from plan.gate. A gate that is blank after strings.TrimSpace
// counts as unset. Ids not in p are skipped.
func Render(p *plan.Plan, layers [][]string) string {
	type at struct {
		t    *plan.Task
		wave int
	}
	byID := map[string]at{}
	used := map[string]bool{}
	for w := range p.Waves {
		for i := range p.Waves[w].Tasks {
			t := &p.Waves[w].Tasks[i]
			byID[t.ID] = at{t: t, wave: w}
			used[t.ID] = true
		}
	}
	var b strings.Builder
	b.WriteString("```paraplane\nversion: 1\n")
	planGate := gateOrEmpty(p.Gate)
	if planGate != "" {
		fmt.Fprintf(&b, "gate: %s\n", q(planGate))
	}
	b.WriteString("waves:\n")
	n := 0
	for _, l := range layers {
		latest := -1
		var tasks []*plan.Task
		for _, id := range l {
			if x, ok := byID[id]; ok {
				tasks = append(tasks, x.t)
				latest = max(latest, x.wave)
			}
		}
		if len(tasks) == 0 {
			continue
		}
		// Task and wave ids share one namespace (spec 4).
		id := ""
		for id == "" || used[id] {
			n++
			id = fmt.Sprintf("W%d", n)
		}
		fmt.Fprintf(&b, "  - id: %s\n", q(id))
		gate := gateOrEmpty(p.Waves[latest].Gate)
		if gate == "" {
			gate = planGate
		}
		if gate != planGate {
			fmt.Fprintf(&b, "    gate: %s\n", q(gate))
		}
		b.WriteString("    tasks:\n")
		for _, t := range tasks {
			fmt.Fprintf(&b, "      - id: %s\n", q(t.ID))
			if t.Title != "" {
				fmt.Fprintf(&b, "        title: %s\n", q(t.Title))
			}
			list(&b, "owns", t.Owns)
			list(&b, "reads", t.Reads)
			list(&b, "depends", t.Depends)
			if t.Verify != "" {
				fmt.Fprintf(&b, "        verify: %s\n", q(t.Verify))
			}
		}
	}
	b.WriteString("```\n")
	return b.String()
}

// gateOrEmpty returns g, or "" when g is blank.
func gateOrEmpty(g string) string {
	if strings.TrimSpace(g) == "" {
		return ""
	}
	return g
}

// q double-quotes s; every escape strconv.Quote emits for valid UTF-8 is
// also a YAML double-quoted escape.
func q(s string) string { return strconv.Quote(s) }

func list(b *strings.Builder, key string, xs []string) {
	if len(xs) == 0 {
		return
	}
	qs := make([]string, len(xs))
	for i, x := range xs {
		qs[i] = q(x)
	}
	fmt.Fprintf(b, "        %s: [%s]\n", key, strings.Join(qs, ", "))
}
