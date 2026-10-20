// Package rules validates a ```paraplane plan (spec section 5: E0–E7,
// W1–W5) and audits the files changed by a wave (spec section 5.1: A1).
package rules

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"paraplane/internal/layout"
	"paraplane/internal/pathset"
	"paraplane/internal/plan"
)

// Severity tells errors, which make a plan invalid, from warnings.
type Severity string

// Severities.
const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

// Finding is one violation of the rules. Msg may contain text from the plan
// as is; callers sanitise it before output.
type Finding struct {
	Code     string // "E0"…"E7", "W1"…"W5", "A1"
	Severity Severity
	Where    string // wave or task id; "" for the plan
	Msg      string
}

// String formats f as "error E2 W1: …", or "error E0: …" without Where.
func (f Finding) String() string {
	if f.Where == "" {
		return fmt.Sprintf("%s %s: %s", f.Severity, f.Code, f.Msg)
	}
	return fmt.Sprintf("%s %s %s: %s", f.Severity, f.Code, f.Where, f.Msg)
}

// Options configure Check.
type Options struct {
	HotFiles    []string
	DefaultGate string
	MaxParallel int                   // 0 → 6
	IsDir       func(rel string) bool // nil → W5 is not checked
	// Deadline bounds the conflict search; zero means no limit. Once it has
	// passed, Check reports a single E0 with the text of layout.ErrDeadline.
	Deadline time.Time
}

// Result is the outcome of Check.
type Result struct {
	Plan      *plan.Plan // the plan with cleaned paths (bad ones dropped); nil at E0 or a schema error
	Findings  []Finding  // by code, then in the author's order of waves and tasks
	Suggested [][]string // the shorter layout; non-nil only with W3
}

const defaultMaxParallel = 6

var idRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,31}$`)

// Check validates the ```paraplane block of md in the order of spec
// section 5.
func Check(md []byte, o Options) Result {
	p, err := plan.Load(md)
	if err != nil {
		var se *plan.SchemaError
		if errors.As(err, &se) {
			return Result{Findings: []Finding{{Code: "E1", Severity: Error, Msg: se.Error()}}}
		}
		return Result{Findings: []Finding{{Code: "E0", Severity: Error, Msg: err.Error()}}}
	}
	c := &checker{p: clone(p), o: o, wave: map[string]int{}, pos: map[string]int{}, ids: map[string]bool{}}
	return c.run()
}

type checker struct {
	p    *plan.Plan
	o    Options
	fs   []Finding
	keys []key           // sort key of fs[i]
	wave map[string]int  // task id → index of its wave (first occurrence)
	pos  map[string]int  // task id → index of the task in its wave
	ids  map[string]bool // every wave and task id seen
	// waveWhere and waveName are the Where and the message name of each
	// wave: its id, or "" and its number when the id is missing or invalid.
	waveWhere, waveName []string
	// taskWhere and taskName are the same for every task, by wave and position.
	taskWhere, taskName [][]string
}

// key orders findings of one code: plan-level first (-1, -1), then by wave,
// wave-level (task -1) before the tasks of the wave.
type key struct{ wave, task int }

var planKey = key{-1, -1}

func (c *checker) add(code, where string, k key, format string, args ...any) {
	sev := Error
	if code[0] == 'W' {
		sev = Warning
	}
	c.fs = append(c.fs, Finding{Code: code, Severity: sev, Where: where, Msg: fmt.Sprintf(format, args...)})
	c.keys = append(c.keys, k)
}

// sorted returns the findings by code, then by key; equal ones keep the
// order they were found in.
func (c *checker) sorted() []Finding {
	idx := make([]int, len(c.fs))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		x, y := idx[a], idx[b]
		if c.fs[x].Code != c.fs[y].Code {
			return c.fs[x].Code < c.fs[y].Code
		}
		kx, ky := c.keys[x], c.keys[y]
		if kx.wave != ky.wave {
			return kx.wave < ky.wave
		}
		return kx.task < ky.task
	})
	out := make([]Finding, len(idx))
	for i, j := range idx {
		out[i] = c.fs[j]
	}
	return out
}

func (c *checker) run() Result {
	c.structure()
	c.depends()
	structural := c.count(Error) > 0 // only E1 and E3 so far
	c.gates()
	c.tasks()
	var suggested [][]string
	if !structural {
		cs, err := layout.ConflictsWithin(c.p, c.o.HotFiles, c.o.Deadline)
		if err != nil {
			return Result{Findings: []Finding{{Code: "E0", Severity: Error, Msg: err.Error()}}}
		}
		c.conflicts(cs)
		if c.count(Error) == 0 {
			if l := layout.SuggestWith(c.p, cs); len(l) < len(c.p.Waves) {
				c.add("W3", "", planKey, "the layout can be shorter: %s instead of %d (`paraplane check --suggest` prints it)", waveCount(len(l)), len(c.p.Waves))
				suggested = l
			}
		}
	}
	return Result{Plan: c.p, Findings: c.sorted(), Suggested: suggested}
}

func (c *checker) count(s Severity) int {
	n := 0
	for _, f := range c.fs {
		if f.Severity == s {
			n++
		}
	}
	return n
}

// clone returns a deep copy of the waves, tasks and lists of p, so that
// structure can replace the paths with cleaned ones.
func clone(p *plan.Plan) *plan.Plan {
	q := *p
	q.Waves = make([]plan.Wave, len(p.Waves))
	for i, w := range p.Waves {
		w.Tasks = append([]plan.Task(nil), w.Tasks...)
		for j := range w.Tasks {
			t := &w.Tasks[j]
			t.Owns = append([]string(nil), t.Owns...)
			t.Reads = append([]string(nil), t.Reads...)
			t.Depends = append([]string(nil), t.Depends...)
		}
		q.Waves[i] = w
	}
	return &q
}

// cleanList cleans every path of ps, dropping and passing to bad the ones
// that fail.
func cleanList(ps []string, bad func(p string, err error)) []string {
	out := ps[:0]
	for _, p := range ps {
		cp, err := pathset.Clean(p)
		if err != nil {
			bad(p, err)
			continue
		}
		out = append(out, cp)
	}
	return out
}

// checkID registers id and reports a bad or repeated one; it reports whether
// id may be used to refer to the wave or task.
func (c *checker) checkID(kind, id string, k key) bool {
	if !idRe.MatchString(id) {
		c.add("E1", id, k, "invalid %s id %q; an id starts with a letter and has at most 32 letters, digits, '_', '.' or '-'", kind, id)
		return false
	}
	if c.ids[id] {
		c.add("E1", id, k, "duplicate id %s; waves and tasks share one namespace", id)
		return false
	}
	c.ids[id] = true
	return true
}

// structure reports E1 except for unknown depends, cleans the paths and
// indexes the ids.
func (c *checker) structure() {
	c.waveWhere = make([]string, len(c.p.Waves))
	c.waveName = make([]string, len(c.p.Waves))
	c.taskWhere = make([][]string, len(c.p.Waves))
	c.taskName = make([][]string, len(c.p.Waves))
	if len(c.p.Waves) == 0 {
		c.add("E1", "", planKey, "the plan has no waves")
		return
	}
	for i := range c.p.Waves {
		w := &c.p.Waves[i]
		wk := key{i, -1}
		if w.ID == "" {
			c.add("E1", "", wk, "wave %d has no id", i+1)
		} else if c.checkID("wave", w.ID, wk) {
			c.waveWhere[i] = w.ID
		}
		where, wname := c.waveWhere[i], w.ID
		if where == "" {
			wname = fmt.Sprint(i + 1)
		}
		c.waveName[i] = wname
		c.taskWhere[i] = make([]string, len(w.Tasks))
		c.taskName[i] = make([]string, len(w.Tasks))
		if len(w.Tasks) == 0 {
			c.add("E1", where, wk, "wave %s has no tasks", wname)
		}
		for j := range w.Tasks {
			t := &w.Tasks[j]
			tk := key{i, j}
			// tw is where the findings of t go and name is how messages call
			// it: its id when the id is usable, else, like a wave, its
			// position ("2 of wave W1").
			tw, name := where, fmt.Sprintf("%d of wave %s", j+1, wname)
			if t.ID == "" {
				c.add("E1", where, tk, "task %s has no id", name)
			} else if c.checkID("task", t.ID, tk) {
				c.wave[t.ID], c.pos[t.ID] = i, j
				tw, name = t.ID, t.ID
			}
			c.taskWhere[i][j], c.taskName[i][j] = tw, name
			if t.ID != "" && strings.TrimSpace(t.Title) == "" {
				c.add("E1", tw, tk, "task %s has no title", name)
			}
			for _, l := range []struct {
				key string
				ps  *[]string
			}{{"owns", &t.Owns}, {"reads", &t.Reads}} {
				*l.ps = cleanList(*l.ps, func(p string, err error) {
					c.add("E1", tw, tk, "task %s: invalid %s path %q: %v", name, l.key, p, err)
				})
			}
		}
	}
}

// depends reports unknown depends (E1) and depends on a task of the same or
// a later wave (E3).
func (c *checker) depends() {
	for i, w := range c.p.Waves {
		for j, t := range w.Tasks {
			tk, where, name := key{i, j}, c.taskWhere[i][j], c.taskName[i][j]
			for _, d := range t.Depends {
				wi, ok := c.wave[d]
				switch {
				case !ok:
					c.add("E1", where, tk, "task %s depends on unknown task %s", name, d)
				case wi == i:
					c.add("E3", where, tk, "task %s depends on %s of the same wave %s", name, d, c.waveName[i])
				case wi > i:
					c.add("E3", where, tk, "task %s depends on %s of the later wave %s", name, d, c.waveName[wi])
				}
			}
		}
	}
}

// gates reports E7 and W2.
func (c *checker) gates() {
	maxPar := c.o.MaxParallel
	if maxPar <= 0 {
		maxPar = defaultMaxParallel
	}
	for i, w := range c.p.Waves {
		where, name, wk := c.waveWhere[i], c.waveName[i], key{i, -1}
		if strings.TrimSpace(w.Gate) == "" && strings.TrimSpace(c.p.Gate) == "" && strings.TrimSpace(c.o.DefaultGate) == "" {
			c.add("E7", where, wk, "wave %s has no gate; set gate on the wave or the plan, or default_gate in the config", name)
		}
		if len(w.Tasks) > maxPar {
			c.add("W2", where, wk, "wave %s has %d tasks, more than max_parallel %d", name, len(w.Tasks), maxPar)
		}
	}
}

// tasks reports W1, W4 and W5.
func (c *checker) tasks() {
	for i, w := range c.p.Waves {
		for j, t := range w.Tasks {
			name, where, tk := c.taskName[i][j], c.taskWhere[i][j], key{i, j}
			if strings.TrimSpace(t.Verify) == "" {
				c.add("W1", where, tk, "task %s has no verify", name)
			}
			if len(t.Owns) == 0 {
				c.add("W4", where, tk, "task %s has empty owns", name)
			}
			if c.o.IsDir == nil {
				continue
			}
			for _, p := range t.Owns {
				if !pathset.HasMeta(p) && c.o.IsDir(p) {
					c.add("W5", where, tk, "owns of %s lists %s, an existing directory; write %s/** for the whole directory", name, p, p)
				}
			}
		}
	}
}

// conflicts reports E2, E4 and E5 for pairs of one wave and E6 for owns
// conflicts across waves without a dependency.
func (c *checker) conflicts(cs []layout.Conflict) {
	tasks := c.p.Tasks()
	idx := make(map[string]int, len(tasks))
	for i, t := range tasks {
		idx[t.ID] = i
	}
	// anc[i] holds the transitive depends of task i; depends point to
	// earlier waves here (no E1/E3), so they are computed first.
	anc := make([]map[int]bool, len(tasks))
	for i, t := range tasks {
		anc[i] = map[int]bool{}
		for _, d := range t.Depends {
			j := idx[d]
			anc[i][j] = true
			for k := range anc[j] {
				anc[i][k] = true
			}
		}
	}
	for _, k := range cs {
		wa, wb := c.wave[k.A], c.wave[k.B]
		if wa == wb {
			wid, wk := c.p.Waves[wa].ID, key{wa, -1}
			switch k.Kind {
			case layout.OwnsOverlap:
				if k.Path == k.With {
					c.add("E2", wid, wk, "%s and %s both own %s", k.A, k.B, k.Path)
				} else {
					c.add("E2", wid, wk, "owns of %s (%s) and %s (%s) may overlap; narrow the patterns or move a task to another wave", k.A, k.Path, k.B, k.With)
				}
			case layout.ReadsOwns:
				if k.Path == k.With {
					c.add("E4", wid, wk, "%s reads %s, which %s owns", k.A, k.Path, k.B)
				} else {
					c.add("E4", wid, wk, "reads of %s (%s) may overlap owns of %s (%s); narrow the patterns or move a task to another wave", k.A, k.Path, k.B, k.With)
				}
			case layout.HotFile:
				c.add("E5", wid, wk, "%s and %s both touch the hot file %s; move one of them to another wave", k.A, k.B, k.Path)
			}
			continue
		}
		if k.Kind != layout.OwnsOverlap || anc[idx[k.B]][idx[k.A]] {
			continue
		}
		ew, bk := c.p.Waves[wa].ID, key{wb, c.pos[k.B]}
		if k.Path == k.With {
			c.add("E6", k.B, bk, "%s changes %s, which %s of the earlier wave %s owns, but does not depend on %s; add %s to its depends", k.B, k.Path, k.A, ew, k.A, k.A)
		} else {
			c.add("E6", k.B, bk, "owns of %s (%s) may overlap owns of %s (%s) of the earlier wave %s, but %s does not depend on %s; add %s to its depends or narrow the patterns", k.B, k.With, k.A, k.Path, ew, k.B, k.A, k.A)
		}
	}
}

// Errors returns the number of error findings.
func (r Result) Errors() int { return r.count(Error) }

// Warnings returns the number of warning findings.
func (r Result) Warnings() int { return r.count(Warning) }

func (r Result) count(s Severity) int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == s {
			n++
		}
	}
	return n
}

// Summary is the last line of paraplane check (spec section 7).
func (r Result) Summary() string {
	if e := r.Errors(); e > 0 || r.Plan == nil {
		return fmt.Sprintf("plan invalid: %d error(s), %d warning(s)", e, r.Warnings())
	}
	size := fmt.Sprintf("%d waves, %d tasks", len(r.Plan.Waves), len(r.Plan.Tasks()))
	if w := r.Warnings(); w > 0 {
		return fmt.Sprintf("plan ok with %d warning(s): %s", w, size)
	}
	return "plan ok: " + size
}

// waveCount renders n with the correct singular or plural noun.
func waveCount(n int) string {
	if n == 1 {
		return "1 wave"
	}
	return fmt.Sprintf("%d waves", n)
}
