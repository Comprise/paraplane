package progress

import (
	"fmt"
	"strings"
	"testing"

	"paraplane/internal/plan"
)

func mk(sizes ...int) *plan.Plan {
	p := &plan.Plan{}
	n := 0
	for i, s := range sizes {
		w := plan.Wave{ID: fmt.Sprintf("W%d", i+1)}
		for j := 0; j < s; j++ {
			n++
			w.Tasks = append(w.Tasks, plan.Task{ID: fmt.Sprintf("T%d", n)})
		}
		p.Waves = append(p.Waves, w)
	}
	return p
}

func render(p *plan.Plan, name string, done []string) string {
	return RenderFrom(p, name, Input{CommittedWaves: done})
}

func renderClosed(p *plan.Plan, name string, done, closed []string) string {
	return RenderFrom(p, name, Input{CommittedWaves: done, Closed: closed})
}

// cols counts terminal columns independently of width: the squares, CJK,
// Hangul, fullwidth forms and emoji take two.
func cols(s string) int {
	n := 0
	for _, r := range s {
		switch {
		case r == '🟩', r == '🟨', r == '⬜',
			0x1100 <= r && r <= 0x115F, 0x2E80 <= r && r <= 0xA4CF, 0xAC00 <= r && r <= 0xD7A3,
			0xF900 <= r && r <= 0xFAFF, 0xFF00 <= r && r <= 0xFF60, 0xFFE0 <= r && r <= 0xFFE6,
			0x1F300 <= r && r <= 0x1FAFF, 0x20000 <= r && r <= 0x3FFFD:
			n += 2
		default:
			n++
		}
	}
	return n
}

const specExample = "paraplane · example.md                                    wave 3/5 · 64%\n" +
	"█████████████████████████░░░░░░░░░░░░░░░  11/17 tasks\n" +
	"\n" +
	"W1  🟩🟩🟩🟩🟩🟩  6/6  ✓ done     T1 T2 T3 T4 T5 T6\n" +
	"W2  🟩🟩🟩🟩🟩    5/5  ✓ done     T7 T8 T9 T10 T11\n" +
	"W3  🟨🟨          0/2  ▶ running  T12 T13\n" +
	"W4  ⬜⬜          0/2  · waiting  T14 T15\n" +
	"W5  ⬜⬜          0/2  · waiting  T16 T17\n"

func TestRenderSpecExample(t *testing.T) {
	got := render(mk(6, 5, 2, 2, 2), "example.md", []string{"W1", "W2"})
	if got != specExample {
		t.Fatalf("got:\n%s\nwant:\n%s", got, specExample)
	}
}

func TestRenderNothingDone(t *testing.T) {
	got := render(mk(2, 2), "p.md", nil)
	if !strings.Contains(got, "wave 1/2 · 0%") || !strings.Contains(got, strings.Repeat("░", 40)+"  0/4 tasks") {
		t.Fatalf("bad header:\n%s", got)
	}
	if !strings.Contains(got, "W1  🟨🟨  0/2  ▶ running  T1 T2\n") {
		t.Fatalf("bad W1:\n%s", got)
	}
}

func TestRenderAllDone(t *testing.T) {
	got := render(mk(2, 2), "p.md", []string{"W1", "W2"})
	if !strings.Contains(got, "wave 2/2 · 100%") || strings.Count(got, "✓ done") != 2 ||
		strings.Contains(got, "░") || !strings.Contains(got, strings.Repeat("█", 40)+"  4/4 tasks") {
		t.Fatalf("bad:\n%s", got)
	}
}

func TestRenderManyTasks(t *testing.T) {
	got := render(mk(15), "p.md", nil)
	if !strings.Contains(got, strings.Repeat("🟨", 12)+" +3") || strings.Contains(got, strings.Repeat("🟨", 13)) {
		t.Fatalf("bad:\n%s", got)
	}
}

func TestRenderUnknownWave(t *testing.T) {
	got := render(mk(2), "p.md", []string{"W9"})
	if !strings.Contains(got, "warning: commit for unknown wave W9\n") {
		t.Fatalf("no warning:\n%s", got)
	}
}

func TestRenderCommittedOutOfOrder(t *testing.T) {
	got := render(mk(1, 1, 1), "p.md", []string{"W1", "W3"})
	lines := strings.Split(got, "\n")
	if !strings.HasPrefix(lines[0], "paraplane · p.md") || !strings.HasSuffix(lines[0], "wave 2/3 · 33%") {
		t.Fatalf("bad header %q", lines[0])
	}
	if lines[1] != strings.Repeat("█", 13)+strings.Repeat("░", 27)+"  1/3 tasks" {
		t.Fatalf("bad bar %q", lines[1])
	}
	for _, w := range []string{"W1  🟩  1/1  ✓ done", "W2  🟨  0/1  ▶ running", "W3  ⬜  0/1  · waiting", "warning: wave W3 was committed before W2\n"} {
		if !strings.Contains(got, w) {
			t.Fatalf("missing %q:\n%s", w, got)
		}
	}
}

func TestRenderWidth(t *testing.T) {
	p := mk(15, 3)
	p.Waves[0].ID = strings.Repeat("LongWaveID", 10)
	p.Waves[1].Tasks[0].ID = strings.Repeat("task", 40)
	got := render(p, strings.Repeat("name", 40)+".md", []string{p.Waves[0].ID})
	if !strings.Contains(got, "…") {
		t.Fatalf("no ellipsis:\n%s", got)
	}
	for _, l := range strings.Split(got, "\n") {
		if cols(l) > 72 {
			t.Fatalf("line %d cols: %q", cols(l), l)
		}
	}
	if cols(strings.Split(got, "\n")[0]) != 72 {
		t.Fatalf("header not 72 columns")
	}
}

func TestRenderLongWaveIDs(t *testing.T) {
	p := mk(2, 2)
	p.Waves[0].ID = "backend-api-01"
	p.Waves[1].ID = "backend-api-02"
	got := render(p, "plan.md", []string{"backend-api-01"})
	for _, id := range []string{"backend-api-01", "backend-api-02"} {
		if !strings.Contains(got, id+"  ") {
			t.Fatalf("missing full id %q:\n%s", id, got)
		}
	}
	for _, l := range strings.Split(got, "\n") {
		if cols(l) > 72 {
			t.Fatalf("line %d cols: %q", cols(l), l)
		}
	}
}

func TestRenderShrunkIDsStayDistinct(t *testing.T) {
	for _, n := range []int{40, 300} {
		p := mk(n, n)
		p.Waves[0].ID = strings.Repeat("x", 24) + "-wave-01"
		p.Waves[1].ID = strings.Repeat("x", 24) + "-wave-02"
		got := render(p, "plan.md", nil)
		lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
		a, b := strings.Fields(lines[3])[0], strings.Fields(lines[4])[0]
		if a == b {
			t.Fatalf("n=%d ids collapsed: %q\n%s", n, a, got)
		}
		for _, l := range lines {
			if cols(l) > 72 {
				t.Fatalf("line %d cols: %q", cols(l), l)
			}
		}
	}
}

func TestRenderUnknownWaveDedup(t *testing.T) {
	got := render(mk(2), "p.md", []string{"W9", "W9", "W9"})
	if n := strings.Count(got, "commit for unknown wave W9"); n != 1 {
		t.Fatalf("want 1 warning, got %d:\n%s", n, got)
	}
}

func TestRenderSanitisesControls(t *testing.T) {
	p := mk(1, 1)
	p.Waves[0].ID = "W\x1b[31m1\n"
	p.Waves[0].Tasks[0].ID = "T\x07\u202e1"
	got := render(p, "n\x1bame.md", []string{"bad\x1b[0m\nid"})
	for _, r := range got {
		if r != '\n' && (r < ' ' || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x202e) {
			t.Fatalf("control %U in output:\n%q", r, got)
		}
	}
	for _, w := range []string{"W[31m1", "T1", "name.md", "commit for unknown wave bad[0m id"} {
		if !strings.Contains(got, w) {
			t.Fatalf("missing %q:\n%q", w, got)
		}
	}
}

func TestRenderWarningsCapped(t *testing.T) {
	p := mk(1, 1, 1)
	p.Waves[1].ID = strings.Repeat("b", 50)
	p.Waves[2].ID = strings.Repeat("c", 50)
	got := render(p, "p.md", []string{p.Waves[2].ID, strings.Repeat("u", 80)})
	n := 0
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "warning:") {
			n++
			if cols(l) > 72 || (strings.Contains(l, "unknown") && !strings.HasSuffix(l, "…")) {
				t.Fatalf("warning not capped to 72 cols: %d %q", cols(l), l)
			}
		}
	}
	if n != 2 {
		t.Fatalf("want 2 warnings, got %d:\n%s", n, got)
	}
}

func rowOf(out, id string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, id+" ") {
			return l
		}
	}
	return ""
}

func TestRenderClosedRunning(t *testing.T) {
	got := renderClosed(mk(3, 3, 2), "p.md", []string{"W1"}, []string{"T4", "T6"})
	lines := strings.Split(got, "\n")
	if !strings.HasSuffix(lines[0], "wave 2/3 · 62%") {
		t.Errorf("header %q", lines[0])
	}
	if want := strings.Repeat("█", 25) + strings.Repeat("░", 15) + "  5/8 tasks"; lines[1] != want {
		t.Errorf("bar %q, want %q", lines[1], want)
	}
	if w2 := rowOf(got, "W2"); !strings.Contains(w2, "🟩🟨🟩") || !strings.Contains(w2, "2/3") || !strings.Contains(w2, "▶ running") {
		t.Errorf("W2 row %q", w2)
	}
}

func TestRenderClosedWholeWave(t *testing.T) {
	got := renderClosed(mk(3, 3, 2), "p.md", []string{"W1"}, []string{"T4", "T5", "T6"})
	if !strings.Contains(rowOf(got, "W2"), "✓ done") || !strings.Contains(rowOf(got, "W3"), "▶ running") || !strings.Contains(got, "  6/8 tasks") {
		t.Errorf("got:\n%s", got)
	}
	// Without any commit: W1 closed by --done alone.
	got = renderClosed(mk(3, 3, 2), "p.md", nil, []string{"T1", "T2", "T3"})
	if !strings.Contains(rowOf(got, "W1"), "✓ done") || !strings.Contains(rowOf(got, "W2"), "▶ running") {
		t.Errorf("no commits:\n%s", got)
	}
}

func TestRenderClosedWarnings(t *testing.T) {
	got := renderClosed(mk(3, 3, 2), "p.md", []string{"W1"}, []string{"X9", "T7", "X9"})
	if want := "warning: --done X9 is not a task of the plan\nwarning: --done T7 is in waiting wave W3\n"; !strings.HasSuffix(got, want) {
		t.Errorf("got:\n%s", got)
	}
	if !strings.Contains(got, "  3/8 tasks") {
		t.Errorf("warned ids counted:\n%s", got)
	}
}

func TestRenderClosedDedup(t *testing.T) {
	got := renderClosed(mk(3, 3, 2), "p.md", []string{"W1"}, []string{"T1", "T4", "T4"})
	if !strings.Contains(got, "  4/8 tasks") || strings.Contains(got, "warning") {
		t.Errorf("got:\n%s", got)
	}
}

// TestRenderWrappers: the compatibility wrappers (removed in R13) render
// like RenderFrom.
func TestRenderWrappers(t *testing.T) {
	for _, done := range [][]string{nil, {"W1"}, {"W1", "W2"}, {"W2"}, {"W9"}} {
		p := mk(6, 5, 2, 2, 2)
		want := RenderFrom(p, "x.md", Input{CommittedWaves: done, Closed: []string{"T7"}})
		if a := RenderClosed(p, "x.md", done, []string{"T7"}); a != want {
			t.Errorf("RenderClosed, done %v:\n%s\n!=\n%s", done, a, want)
		}
		if a, b := Render(p, "x.md", done), RenderFrom(p, "x.md", Input{CommittedWaves: done}); a != b {
			t.Errorf("Render, done %v:\n%s\n!=\n%s", done, a, b)
		}
	}
}

func TestRenderFromCommittedTasks(t *testing.T) {
	got := RenderFrom(mk(3, 3, 2), "p.md", Input{CommittedTasks: []string{"T1", "T2", "T3"}})
	if !strings.Contains(rowOf(got, "W1"), "✓ done") || !strings.Contains(rowOf(got, "W2"), "▶ running") {
		t.Errorf("tasks close a wave:\n%s", got)
	}
	got = RenderFrom(mk(3, 3, 2), "p.md", Input{CommittedWaves: []string{"W1"}, CommittedTasks: []string{"T7"}})
	if !strings.HasSuffix(got, "warning: task T7 was committed before W2\n") || !strings.Contains(got, "  3/8 tasks") {
		t.Errorf("task of a waiting wave:\n%s", got)
	}
}

func TestRenderFromMixed(t *testing.T) {
	got := RenderFrom(mk(3, 3, 2), "p.md", Input{CommittedWaves: []string{"W1"}, CommittedTasks: []string{"T4"}, Closed: []string{"T5"}})
	if !strings.Contains(rowOf(got, "W2"), "🟩🟩🟨") || !strings.Contains(got, "  5/8 tasks") || strings.Contains(got, "warning") {
		t.Errorf("got:\n%s", got)
	}
}
