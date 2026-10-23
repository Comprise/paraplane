package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"paraplane/internal/config"
	"paraplane/internal/layout"
	"paraplane/internal/plan"
)

// TestResultTimedOut: a deadline hit while parsing (plan.ErrDeadline) or in
// the conflict search (layout.ErrDeadline) sets TimedOut; other E0 do not.
func TestResultTimedOut(t *testing.T) {
	o := opts()
	o.Deadline = time.Now().Add(-time.Second)
	r := Check(md("gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a]}\n"), o)
	if !r.TimedOut || len(r.Findings) != 1 || r.Findings[0].Code != "E0" || r.Findings[0].Msg != layout.ErrDeadline.Error() {
		t.Errorf("past deadline: TimedOut %v, findings %v", r.TimedOut, r.Findings)
	}

	braces := strings.Repeat("{a,aa}", 8) + strings.Repeat("?", 40)
	as := strings.Repeat("a", 100)
	in := bigPlan(func(n int) string {
		if n%2 == 0 {
			return fmt.Sprintf("%s/z%04d", braces, n)
		}
		return fmt.Sprintf("%s/*z%04d", as, n)
	})
	o = opts()
	o.Deadline = time.Now().Add(50 * time.Millisecond)
	r = Check(in, o)
	if !r.TimedOut || len(r.Findings) != 1 || r.Findings[0].Msg != layout.ErrDeadline.Error() {
		t.Errorf("hostile plan: TimedOut %v, findings %v", r.TimedOut, r.Findings[:min(3, len(r.Findings))])
	}

	if r := Check([]byte("no block"), opts()); r.TimedOut {
		t.Error("missing block: TimedOut")
	}
	if r := Check(md("gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, verify: v, owns: [a]}\n"), opts()); r.TimedOut || r.Errors() != 0 {
		t.Errorf("clean plan: TimedOut %v, %v", r.TimedOut, r.Findings)
	}
}

// TestW5StopsAtDeadline: once the deadline has passed no IsDir call is made.
func TestW5StopsAtDeadline(t *testing.T) {
	calls := 0
	o := opts()
	o.IsDir = func(string) bool { calls++; return true }
	c := &checker{p: &plan.Plan{Waves: []plan.Wave{{ID: "W1", Tasks: []plan.Task{{ID: "T1", Owns: []string{"a", "b"}}}}}}, o: o}
	c.o.Deadline = time.Now().Add(-time.Second)
	c.taskWhere, c.taskName = [][]string{{"T1"}}, [][]string{{"T1"}}
	c.tasks()
	if calls != 0 {
		t.Errorf("IsDir called %d times after the deadline", calls)
	}
}

func TestOptionsFor(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "f.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.HotFiles = []string{"go.mod", "x/**"}
	cfg.DefaultGate = "make test"
	cfg.MaxParallel = 3
	dl := time.Now().Add(time.Minute)
	o := OptionsFor(cfg, root, dl)
	if !reflect.DeepEqual(o.HotFiles, cfg.HotFiles) || o.DefaultGate != "make test" || o.MaxParallel != 3 || !o.Deadline.Equal(dl) {
		t.Errorf("OptionsFor = %+v", o)
	}
	if o.IsDir == nil || !o.IsDir("a/b") || !o.IsDir("a") || o.IsDir("f.go") || o.IsDir("missing") {
		t.Error("IsDir does not look under root")
	}
	if o := OptionsFor(cfg, root, time.Time{}); !o.Deadline.IsZero() {
		t.Errorf("zero deadline became %v", o.Deadline)
	}
}

func TestCommitsOfWaves(t *testing.T) {
	p := auditPlan(t)
	commits := []CommitFiles{
		{Hash: "1", Tasks: []string{"T1"}},
		{Hash: "2", Waves: []string{"W1", "W2"}},
		{Hash: "3", Waves: []string{"W9"}},
		{Hash: "4", Tasks: []string{"T3"}},
		{Hash: "5", Tasks: []string{"X1", "T2"}},
		{Hash: "6"},
	}
	for _, tc := range []struct {
		waves []string
		want  int
	}{{[]string{"W1"}, 3}, {[]string{"W2"}, 2}, {[]string{"W1", "W2"}, 4}, {[]string{"W9"}, 0}, {nil, 0}} {
		if got := CommitsOfWaves(p, tc.waves, commits); got != tc.want {
			t.Errorf("CommitsOfWaves(%v) = %d, want %d", tc.waves, got, tc.want)
		}
	}
	if got := CommitsOfWaves(nil, []string{"W1"}, commits); got != 0 {
		t.Errorf("nil plan: %d", got)
	}
}

// TestAuditWrappers: the compatibility wrappers (removed in R13) are
// AuditCommits without commits.
func TestAuditWrappers(t *testing.T) {
	p := auditPlan(t)
	changed := []string{"a/x", "b/x", "c/x", "z"}
	want, _ := AuditCommits(p, []string{"W1"}, changed, nil, "plan.md", nil)
	if got, err := Audit(p, "W1", changed, "plan.md", nil); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Audit = %v, %v; want %v", got, err, want)
	}
	want, _ = AuditCommits(p, []string{"W1", "W2"}, changed, nil, "plan.md", nil)
	if got, err := AuditWaves(p, []string{"W1", "W2"}, changed, "plan.md", nil); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("AuditWaves = %v, %v; want %v", got, err, want)
	}
}
