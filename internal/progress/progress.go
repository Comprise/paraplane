// Package progress renders plan execution progress as text with emoji bars.
package progress

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"paraplane/internal/plan"
	"paraplane/internal/textx"
)

const (
	lineWidth  = 72
	barWidth   = 40
	maxSquares = 12
	// Caps on ids before the "…" suffix is added (textx.Line does not count
	// the suffix in its max).
	idRunesBeforeEllipsis     = 32
	taskIDRunesBeforeEllipsis = 40
)

// WaveState is where a wave stands: done waves come first, then at most
// one running wave, then the waiting ones.
type WaveState int

// Wave states.
const (
	Done WaveState = iota
	Running
	Waiting
)

// squares and labels draw the wave states.
var (
	squares = [...]string{Done: "🟩", Running: "🟨", Waiting: "⬜"}
	labels  = [...]string{Done: "✓ done", Running: "▶ running", Waiting: "· waiting"}
)

// labelWidth is the width of the widest label.
var labelWidth = func() int {
	n := 0
	for _, l := range labels {
		n = max(n, width(l))
	}
	return n
}()

// wide reports whether r takes two terminal columns: the East Asian Wide
// and Fullwidth blocks (CJK, Hangul, kana, fullwidth forms) and the emoji
// planes.
func wide(r rune) bool {
	switch {
	case r < 0x1100:
		return false
	case r <= 0x115F, // Hangul Jamo initials
		r == 0x2329, r == 0x232A,
		r == 0x2B1B, r == 0x2B1C, r == 0x2B50, r == 0x2B55, // ⬛ ⬜ ⭐ ⭕
		0x2E80 <= r && r <= 0x303E, // CJK radicals … CJK symbols
		0x3041 <= r && r <= 0x33FF, // kana … CJK compatibility
		0x3400 <= r && r <= 0x4DBF, // CJK extension A
		0x4E00 <= r && r <= 0x9FFF, // CJK unified ideographs
		0xA000 <= r && r <= 0xA4CF, // Yi
		0xA960 <= r && r <= 0xA97F, // Hangul Jamo extended A
		0xAC00 <= r && r <= 0xD7A3, // Hangul syllables
		0xF900 <= r && r <= 0xFAFF, // CJK compatibility ideographs
		0xFE10 <= r && r <= 0xFE19, // vertical forms
		0xFE30 <= r && r <= 0xFE6F, // CJK compatibility forms, small forms
		0xFF00 <= r && r <= 0xFF60, // fullwidth forms
		0xFFE0 <= r && r <= 0xFFE6,
		0x1F300 <= r && r <= 0x1F64F, // pictographs, emoticons
		0x1F900 <= r && r <= 0x1FAFF, // supplemental symbols and pictographs
		0x1F7E0 <= r && r <= 0x1F7EB, // 🟠 … 🟫 (🟩 🟨)
		0x20000 <= r && r <= 0x3FFFD: // CJK extensions B…
		return true
	}
	return false
}

// runeWidth is 2 for a wide rune, else 1.
func runeWidth(r rune) int {
	if wide(r) {
		return 2
	}
	return 1
}

// width counts terminal columns: wide runes take two, every other rune one.
func width(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

// cutWidth is textx.Line by columns: it keeps at most n columns of s and
// appends "…" (not counted in n) when it cuts.
func cutWidth(s string, n int) string {
	if width(s) <= n {
		return s
	}
	w := 0
	for i, r := range s {
		if w += runeWidth(r); w > n {
			return s[:i] + "…"
		}
	}
	return s
}

// midCut shortens s to n runes keeping its head and tail around a "…", so ids
// that differ only at the end stay distinct.
func midCut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	head := (n - 1) / 2
	tail := n - 1 - head
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// capLine keeps a one-line s within lineWidth columns, ending in "…" when it
// cuts.
func capLine(s string) string {
	if width(s) <= lineWidth {
		return s
	}
	return cutWidth(strings.TrimSuffix(s, "…"), lineWidth-1)
}

func pad(s string, n int) string {
	if w := width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// Render draws the progress of p with the waves done committed.
//
// Deprecated: a compatibility wrapper until R13; use RenderFrom.
func Render(p *plan.Plan, name string, done []string) string {
	return RenderFrom(p, name, Input{CommittedWaves: done})
}

// RenderClosed draws the progress of p with the waves done committed and
// the tasks closed passed with --done.
//
// Deprecated: a compatibility wrapper until R13; use RenderFrom.
func RenderClosed(p *plan.Plan, name string, done, closed []string) string {
	return RenderFrom(p, name, Input{CommittedWaves: done, Closed: closed})
}

// Input is what is known done: waves and tasks named by commit trailers,
// tasks passed with --done (Closed) and tasks of returned subagents kept in
// the state file (Recorded). The three task lists count alike; only their
// warnings differ.
type Input struct {
	CommittedWaves, CommittedTasks []string
	Closed                         []string // --done
	Recorded                       []string // returned subagents
}

// tasksDone is the set of tasks known done one by one.
func (in Input) tasksDone() map[string]bool {
	done := map[string]bool{}
	for _, l := range [][]string{in.CommittedTasks, in.Closed, in.Recorded} {
		for _, id := range l {
			done[id] = true
		}
	}
	return done
}

// Counts is how far a plan is done.
type Counts struct {
	Waves int         // done waves
	Tasks int         // tasks of done waves plus done tasks of the running wave
	State []WaveState // per wave of the plan
}

// running returns the index of the running wave, or -1.
func (c Counts) running() int {
	if c.Waves < len(c.State) {
		return c.Waves
	}
	return -1
}

// Count applies the one done rule (spec 7.1): in plan order, a wave is done
// while it has a commit or all its tasks (at least one) are done; the first
// other wave is running and adds its done tasks; the rest are waiting, and
// their tasks do not count.
func Count(p *plan.Plan, in Input) Counts {
	done := in.tasksDone()
	c := Counts{State: make([]WaveState, len(p.Waves))}
	cur := -1
	for i, w := range p.Waves {
		n := 0
		for _, t := range w.Tasks {
			if done[t.ID] {
				n++
			}
		}
		switch {
		case cur >= 0:
			c.State[i] = Waiting
		case slices.Contains(in.CommittedWaves, w.ID) || len(w.Tasks) > 0 && n == len(w.Tasks):
			c.State[i] = Done
			c.Waves++
			c.Tasks += len(w.Tasks)
		default:
			c.State[i] = Running
			cur = i
			c.Tasks += n
		}
	}
	return c
}

// waveID and taskID are the ids as RenderFrom shows them.
func waveID(id string) string { return textx.Line(id, idRunesBeforeEllipsis) }
func taskID(id string) string { return textx.Line(id, taskIDRunesBeforeEllipsis) }

// RenderFrom draws the progress of p (spec 7.1) from Count: a header with
// the plan file name, a bar of done tasks, a row per wave and warnings.
// Tasks done one by one are drawn 🟩 in the running wave. name is the plan
// file name.
func RenderFrom(p *plan.Plan, name string, in Input) string {
	c := Count(p, in)
	var b strings.Builder
	writeHeader(&b, p, name, c)
	writeRows(&b, p, c, in.tasksDone())
	for _, w := range warnings(p, in, c) {
		b.WriteString(capLine(w) + "\n")
	}
	return b.String()
}

// writeHeader writes the header line, the bar and an empty line.
func writeHeader(b *strings.Builder, p *plan.Plan, name string, c Counts) {
	total := len(p.Tasks())
	pct, full := 0, 0
	if total > 0 {
		pct, full = 100*c.Tasks/total, barWidth*c.Tasks/total
	}
	right := fmt.Sprintf("wave %d/%d · %d%%", min(c.Waves+1, len(p.Waves)), len(p.Waves), pct)
	const prefix = "paraplane · "
	room := lineWidth - width(prefix) - width(right) - 1
	left := prefix + cutWidth(textx.Line(name, math.MaxInt), room-1)
	b.WriteString(left + strings.Repeat(" ", max(1, lineWidth-width(left)-width(right))) + right + "\n")
	fmt.Fprintf(b, "%s%s  %d/%d tasks\n\n", strings.Repeat("█", full), strings.Repeat("░", barWidth-full), c.Tasks, total)
}

// row is one wave line: id, squares, done/total, label and task ids.
type row struct {
	id, cells, count string
	st               WaveState
	tasks            string
}

// newRow builds the row of wave w in state st.
func newRow(w plan.Wave, st WaveState, done map[string]bool) row {
	r := row{id: waveID(w.ID), st: st}
	ids := make([]string, len(w.Tasks))
	d := 0
	var cells strings.Builder
	for j, t := range w.Tasks {
		ids[j] = taskID(t.ID)
		sq := squares[st]
		if st == Done || st == Running && done[t.ID] {
			sq = squares[Done]
			d++
		}
		if j < maxSquares {
			cells.WriteString(sq)
		}
	}
	if k := len(w.Tasks); k > maxSquares {
		fmt.Fprintf(&cells, " +%d", k-maxSquares)
	}
	r.cells, r.count, r.tasks = cells.String(), fmt.Sprintf("%d/%d", d, len(w.Tasks)), strings.Join(ids, " ")
	return r
}

// writeRows writes a line per wave; the columns are padded to the widest
// cell, and ids are cut only when the fixed columns leave less room.
func writeRows(b *strings.Builder, p *plan.Plan, c Counts, done map[string]bool) {
	rows := make([]row, len(p.Waves))
	idw, sqw, cntw := 0, 0, 0
	for i, w := range p.Waves {
		rows[i] = newRow(w, c.State[i], done)
		idw, sqw, cntw = max(idw, width(rows[i].id)), max(sqw, width(rows[i].cells)), max(cntw, width(rows[i].count))
	}
	// ids are never cut below 32 runes unless the fixed columns would push a
	// line past the width limit.
	if fixed := 2 + sqw + 2 + cntw + 2 + labelWidth + 2; idw+fixed > lineWidth {
		idw = max(1, lineWidth-fixed)
		for i := range rows {
			if width(rows[i].id) > idw {
				rows[i].id = midCut(rows[i].id, idw)
			}
		}
	}
	for _, r := range rows {
		line := pad(r.id, idw) + "  " + pad(r.cells, sqw) + "  " + pad(r.count, cntw) + "  " + pad(labels[r.st], labelWidth) + "  "
		ids := []rune(r.tasks)
		if room := lineWidth - width(line); len(ids) > room {
			if room < 1 {
				ids = nil
			} else {
				ids = append(ids[:room-1], '…')
			}
		}
		b.WriteString(strings.TrimRight(line+string(ids), " ") + "\n")
	}
}

// warnings lists, in this order: committed waves that wait, commits of
// unknown waves, committed tasks of waiting waves, --done ids that are no
// task or wait, and recorded tasks of waiting waves (unless already
// warned about). Each id is warned about once per kind; ids outside the
// plan in Recorded are not shown (the hook records only tasks of the plan).
func warnings(p *plan.Plan, in Input, c Counts) []string {
	cur := c.running()
	waveOf := map[string]int{} // task id → wave index
	known := map[string]bool{}
	var out []string
	for i, w := range p.Waves {
		known[w.ID] = true
		for _, t := range w.Tasks {
			waveOf[t.ID] = i
		}
		if c.State[i] == Waiting && slices.Contains(in.CommittedWaves, w.ID) {
			out = append(out, fmt.Sprintf("warning: wave %s was committed before %s", waveID(w.ID), waveID(p.Waves[cur].ID)))
		}
	}
	waiting := func(id string) (int, bool) {
		i, ok := waveOf[id]
		return i, ok && c.State[i] == Waiting
	}
	for _, id := range unique(in.CommittedWaves) {
		if !known[id] {
			out = append(out, "warning: commit for unknown wave "+waveID(id))
		}
	}
	warned := map[string]bool{}
	for _, id := range unique(in.CommittedTasks) {
		if _, ok := waiting(id); ok {
			warned[id] = true
			out = append(out, "warning: task "+taskID(id)+" was committed before "+waveID(p.Waves[cur].ID))
		}
	}
	for _, id := range unique(in.Closed) {
		i, ok := waveOf[id]
		switch {
		case !ok:
			out = append(out, "warning: --done "+taskID(id)+" is not a task of the plan")
		case c.State[i] == Waiting:
			warned[id] = true
			out = append(out, "warning: --done "+taskID(id)+" is in waiting wave "+waveID(p.Waves[i].ID))
		}
	}
	for _, id := range unique(in.Recorded) {
		if _, ok := waiting(id); ok && !warned[id] {
			out = append(out, "warning: task "+taskID(id)+" finished before "+waveID(p.Waves[cur].ID))
		}
	}
	return out
}

// unique returns ids without repeats, in first-seen order.
func unique(ids []string) []string {
	seen := map[string]bool{}
	return slices.DeleteFunc(slices.Clone(ids), func(id string) bool {
		if seen[id] {
			return true
		}
		seen[id] = true
		return false
	})
}
