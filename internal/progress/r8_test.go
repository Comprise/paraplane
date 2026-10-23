package progress

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	headerRe = regexp.MustCompile(`wave (\d+)/(\d+) · (\d+)%$`)
	barRe    = regexp.MustCompile(`  (\d+)/(\d+) tasks$`)
)

// randomIDs picks up to n ids from pool, with repeats.
func randomIDs(r *rand.Rand, pool []string, n int) []string {
	var out []string
	for range r.Intn(n + 1) {
		out = append(out, pool[r.Intn(len(pool))])
	}
	return out
}

// TestCountMatchesRender: over random plans and inputs, Count gives the
// numbers and states RenderFrom prints.
func TestCountMatchesRender(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(8))
	labels := map[WaveState]string{Done: "✓ done", Running: "▶ running", Waiting: "· waiting"}
	for iter := range 2000 {
		sizes := make([]int, 1+r.Intn(5))
		for i := range sizes {
			sizes[i] = r.Intn(5) // empty waves too
		}
		p := mk(sizes...)
		var waves, tasks []string
		for i := range len(sizes) + 1 {
			waves = append(waves, fmt.Sprintf("W%d", i+1))
		}
		for i := range 20 {
			tasks = append(tasks, fmt.Sprintf("T%d", i+1))
		}
		in := Input{
			CommittedWaves: randomIDs(r, waves, 3),
			CommittedTasks: randomIDs(r, tasks, 6),
			Closed:         randomIDs(r, tasks, 6),
			Recorded:       randomIDs(r, tasks, 6),
		}
		c := Count(p, in)
		out := RenderFrom(p, "p.md", in)
		lines := strings.Split(out, "\n")
		h := headerRe.FindStringSubmatch(lines[0])
		b := barRe.FindStringSubmatch(lines[1])
		if h == nil || b == nil {
			t.Fatalf("iter %d: cannot parse\n%s", iter, out)
		}
		total := len(p.Tasks())
		cur := min(c.Waves+1, len(p.Waves))
		pct := 0
		if total > 0 {
			pct = 100 * c.Tasks / total
		}
		if h[1] != strconv.Itoa(cur) || h[2] != strconv.Itoa(len(p.Waves)) || h[3] != strconv.Itoa(pct) ||
			b[1] != strconv.Itoa(c.Tasks) || b[2] != strconv.Itoa(total) {
			t.Fatalf("iter %d: Count %+v, input %+v\n%s", iter, c, in, out)
		}
		if len(c.State) != len(p.Waves) {
			t.Fatalf("iter %d: %d states for %d waves", iter, len(c.State), len(p.Waves))
		}
		done := 0
		for i, w := range p.Waves {
			if !strings.Contains(rowOf(out, w.ID), labels[c.State[i]]) {
				t.Fatalf("iter %d: wave %s state %v, row %q", iter, w.ID, c.State[i], rowOf(out, w.ID))
			}
			if c.State[i] == Done {
				done++
			}
		}
		if done != c.Waves {
			t.Fatalf("iter %d: %d done states, Waves %d", iter, done, c.Waves)
		}
	}
}

func TestCountRunningTasks(t *testing.T) {
	t.Parallel()
	c := Count(mk(3, 3, 2), Input{CommittedWaves: []string{"W1"}, CommittedTasks: []string{"T4"}, Closed: []string{"T5"}, Recorded: []string{"T6", "T8"}})
	want := Counts{Waves: 2, Tasks: 7, State: []WaveState{Done, Done, Running}}
	if fmt.Sprint(c) != fmt.Sprint(want) {
		t.Errorf("Count = %+v, want %+v", c, want)
	}
}

// TestRecordedWaitingWarning: a returned subagent of a waiting wave is
// warned about as finished early, not as a --done; an id outside the plan
// is not shown; recorded tasks of the running wave count.
func TestRecordedWaitingWarning(t *testing.T) {
	t.Parallel()
	got := RenderFrom(mk(3, 3, 2), "p.md", Input{CommittedWaves: []string{"W1"}, Recorded: []string{"T7", "T4", "X9", "T7"}})
	if want := "warning: task T7 finished before W2\n"; !strings.HasSuffix(got, want) || strings.Count(got, "warning") != 1 {
		t.Errorf("got:\n%s", got)
	}
	if strings.Contains(got, "--done") || strings.Contains(got, "X9") {
		t.Errorf("misleading warning:\n%s", got)
	}
	if !strings.Contains(got, "  4/8 tasks") || !strings.Contains(rowOf(got, "W2"), "🟩🟨🟨") {
		t.Errorf("recorded T4 not counted:\n%s", got)
	}
	// --done keeps its own texts, and one id is warned about once.
	got = RenderFrom(mk(3, 3, 2), "p.md", Input{CommittedWaves: []string{"W1"}, Closed: []string{"T7"}, Recorded: []string{"T7", "T8"}})
	if want := "warning: --done T7 is in waiting wave W3\nwarning: task T8 finished before W2\n"; !strings.HasSuffix(got, want) {
		t.Errorf("got:\n%s", got)
	}
}

// TestWidthWide: wide runes in the plan name take two columns, so the
// header stays 72 columns.
func TestWidthWide(t *testing.T) {
	t.Parallel()
	if w := width("план-漢字.md"); w != 12 {
		t.Errorf("width = %d, want 12", w)
	}
	for _, name := range []string{"план-漢字.md", strings.Repeat("漢字", 40) + ".md", "a" + strings.Repeat("漢", 60), "한국어-ｆｕｌｌ.md"} {
		got := RenderFrom(mk(2, 2), name, Input{})
		head, _, _ := strings.Cut(got, "\n")
		if cols(head) != 72 {
			t.Errorf("%s: header %d columns: %q", name, cols(head), head)
		}
	}
}
