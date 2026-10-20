// Package progress renders plan execution progress as text with emoji bars.
package progress

import (
	"fmt"
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

type state int

const (
	stDone state = iota
	stRunning
	stWaiting
)

// width counts terminal columns: 🟩 🟨 ⬜ take two, every other rune one.
func width(s string) int {
	n := 0
	for _, r := range s {
		switch r {
		case '🟩', '🟨', '⬜':
			n += 2
		default:
			n++
		}
	}
	return n
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
	for width(s) > lineWidth {
		r := []rune(strings.TrimSuffix(s, "…"))
		s = string(r[:len(r)-1]) + "…"
	}
	return s
}

func pad(s string, n int) string {
	if w := width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// Render draws the progress of p (spec 7.1). name is the plan file name; done
// lists the ids of waves that have a commit.
func Render(p *plan.Plan, name string, done []string) string {
	doneSet := map[string]bool{}
	for _, id := range done {
		doneSet[id] = true
	}
	known := map[string]bool{}
	for _, w := range p.Waves {
		known[w.ID] = true
	}

	type row struct {
		id    string
		tasks []string
		st    state
		bad   bool // committed after the running wave
	}
	rows := make([]row, 0, len(p.Waves))
	total, ndone, cur := 0, 0, -1
	var warns []string
	for i, w := range p.Waves {
		r := row{id: textx.Line(w.ID, idRunesBeforeEllipsis)}
		for _, t := range w.Tasks {
			r.tasks = append(r.tasks, textx.Line(t.ID, taskIDRunesBeforeEllipsis))
		}
		total += len(w.Tasks)
		switch {
		case cur < 0 && doneSet[w.ID]:
			r.st = stDone
			ndone += len(w.Tasks)
		case cur < 0:
			r.st = stRunning
			cur = i
		default:
			r.st = stWaiting
			if doneSet[w.ID] {
				warns = append(warns, capLine(fmt.Sprintf("warning: wave %s was committed before %s",
					textx.Line(w.ID, idRunesBeforeEllipsis), textx.Line(p.Waves[cur].ID, idRunesBeforeEllipsis))))
			}
		}
		rows = append(rows, r)
	}
	seen := map[string]bool{}
	for _, id := range done {
		if !known[id] && !seen[id] {
			seen[id] = true
			warns = append(warns, capLine("warning: commit for unknown wave "+textx.Line(id, idRunesBeforeEllipsis)))
		}
	}

	pct := 0
	if total > 0 {
		pct = 100 * ndone / total
	}
	n := len(p.Waves)
	if cur >= 0 {
		n = cur + 1
	}
	right := fmt.Sprintf("wave %d/%d · %d%%", n, len(p.Waves), pct)
	const prefix = "paraplane · "
	room := lineWidth - width(prefix) - width(right) - 1
	nm := textx.Line(name, room-1)
	left := prefix + nm
	var b strings.Builder
	b.WriteString(left + strings.Repeat(" ", max(1, lineWidth-width(left)-width(right))) + right + "\n")
	full := 0
	if total > 0 {
		full = barWidth * ndone / total
	}
	fmt.Fprintf(&b, "%s%s  %d/%d tasks\n\n", strings.Repeat("█", full), strings.Repeat("░", barWidth-full), ndone, total)

	idw, sqw, cntw := 0, 0, 0
	cells := make([]string, len(rows))
	counts := make([]string, len(rows))
	for i, r := range rows {
		k := len(r.tasks)
		sq := strings.Repeat(map[state]string{stDone: "🟩", stRunning: "🟨", stWaiting: "⬜"}[r.st], min(k, maxSquares))
		if k > maxSquares {
			sq += fmt.Sprintf(" +%d", k-maxSquares)
		}
		d := 0
		if r.st == stDone {
			d = k
		}
		cells[i] = sq
		counts[i] = fmt.Sprintf("%d/%d", d, k)
		idw = max(idw, width(r.id))
		sqw = max(sqw, width(sq))
		cntw = max(cntw, width(counts[i]))
	}
	// ids are never cut below 32 runes unless the fixed columns would push a
	// line past the width limit.
	if fixed := 2 + sqw + 2 + cntw + 2 + 9 + 2; idw+fixed > lineWidth {
		idw = max(1, lineWidth-fixed)
		for i := range rows {
			if width(rows[i].id) > idw {
				rows[i].id = midCut(rows[i].id, idw)
			}
		}
	}
	labels := map[state]string{stDone: "✓ done", stRunning: "▶ running", stWaiting: "· waiting"}
	for i, r := range rows {
		line := pad(r.id, idw) + "  " + pad(cells[i], sqw) + "  " + pad(counts[i], cntw) + "  " + pad(labels[r.st], 9) + "  "
		ids := []rune(strings.Join(r.tasks, " "))
		if room := lineWidth - width(line); len(ids) > room {
			if room < 1 {
				ids = nil
			} else {
				ids = append(ids[:room-1], '…')
			}
		}
		b.WriteString(strings.TrimRight(line+string(ids), " ") + "\n")
	}
	for _, w := range warns {
		b.WriteString(w + "\n")
	}
	return b.String()
}
