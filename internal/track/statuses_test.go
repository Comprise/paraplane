package track

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"paraplane/internal/gitx"
	"paraplane/internal/plan"
	"paraplane/internal/state"
	"paraplane/internal/testgit"
)

const (
	hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// newRepo is a repository with one plain commit, isolated from the user's
// git config; it returns the directory and its HEAD.
func newRepo(t testing.TB) (string, string) {
	t.Helper()
	testgit.Isolate(t)
	dir := testgit.Init(t)
	testgit.Commit(t, dir, "init.txt", "init")
	return dir, testgit.Run(t, dir, "rev-parse", "HEAD")
}

// threeWaves is W1 (T1, T2), W2 (T3), W3 (T4).
func threeWaves() *plan.Plan {
	return &plan.Plan{Waves: []plan.Wave{
		{ID: "W1", Tasks: []plan.Task{{ID: "T1"}, {ID: "T2"}}},
		{ID: "W2", Tasks: []plan.Task{{ID: "T3"}}},
		{ID: "W3", Tasks: []plan.Task{{ID: "T4"}}},
	}}
}

// counters counts the calls of the git seams for the duration of a test.
type counters struct{ scans, unreach int }

func countCalls(t testing.TB) *counters {
	t.Helper()
	c := &counters{}
	oldScan, oldUnreach := scan, unreachable
	scan = func(ctx context.Context, root, from string) (gitx.Scanned, error) {
		c.scans++
		return oldScan(ctx, root, from)
	}
	unreachable = func(ctx context.Context, root string, hs []string, head string) (map[string]bool, error) {
		c.unreach++
		return oldUnreach(ctx, root, hs, head)
	}
	t.Cleanup(func() { scan, unreachable = oldScan, oldUnreach })
	return c
}

// statusesApply runs Statuses on *st and applies the result to it, as the
// hook does inside state.UpdateCtx.
func statusesApply(t testing.TB, dir string, plans []PlanRef, st *state.State) (map[string]Status, error) {
	t.Helper()
	r, err := Statuses(ctx(t), dir, plans, *st)
	if err != nil {
		return nil, err
	}
	r.Apply(st)
	return r.ByPlan, nil
}

func ctx(t testing.TB) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestPlanHash(t *testing.T) {
	a := PlanHash([]byte("# x\n\n```paraplane\nwaves: []\n```\n"))
	b := PlanHash([]byte("# y, other prose\n\n```paraplane\nwaves: []\n```\ntail\n"))
	c := PlanHash([]byte("```paraplane\nwaves: [W1]\n```\n"))
	if len(a) != 64 || a != b || a == c {
		t.Errorf("a=%q b=%q c=%q", a, b, c)
	}
	if h := PlanHash([]byte("no block")); h != "" {
		t.Errorf("no block: %q", h)
	}
}

// TestNewPlanSamePath: entries recorded under the block hash A do not count
// for a new block B saved at the same path.
func TestNewPlanSamePath(t *testing.T) {
	dir, head := newRepo(t)
	p := threeWaves()
	st := &state.State{Entries: []state.Entry{
		{Plan: "plan.md", Task: "T1", Head: head, PlanHash: hashA},
		{Plan: "plan.md", Task: "T2", Head: head, PlanHash: hashA},
	}}
	res, err := statusesApply(t, dir, []PlanRef{{Rel: "plan.md", Hash: hashB, Plan: p}}, st)
	if err != nil {
		t.Fatal(err)
	}
	if s := res["plan.md"]; s.Waves != 0 || s.Tasks != 0 {
		t.Errorf("block B counted entries of A: %+v", s)
	}
	if len(st.Entries) != 2 {
		t.Errorf("entries of A pruned (reverting the block must bring them back): %+v", st.Entries)
	}
	res, err = statusesApply(t, dir, []PlanRef{{Rel: "plan.md", Hash: hashA, Plan: p}}, st)
	if err != nil || res["plan.md"].Waves != 1 || res["plan.md"].Tasks != 2 {
		t.Errorf("same block: %+v %v", res["plan.md"], err)
	}
}

// TestPlanStatusContextHash: the read-only form applies the same rule.
func TestPlanStatusContextHash(t *testing.T) {
	dir, head := newRepo(t)
	g := testgit.Run(t, dir, "rev-parse", "--absolute-git-dir")
	if err := state.UpdateCtx(ctx(t), g, func(s *state.State) {
		s.Add(state.Entry{Plan: "plan.md", Task: "T1", Head: head, PlanHash: hashA})
	}); err != nil {
		t.Fatal(err)
	}
	p := threeWaves()
	if s, err := PlanStatusContext(ctx(t), dir, "plan.md", p, hashB, nil); err != nil || s.Tasks != 0 {
		t.Errorf("hash B: %+v %v", s, err)
	}
	if s, err := PlanStatusContext(ctx(t), dir, "plan.md", p, hashA, nil); err != nil || s.Tasks != 1 {
		t.Errorf("hash A: %+v %v", s, err)
	}
	if s, _ := state.LoadState(g); len(s.Entries) != 1 {
		t.Errorf("read-only form changed the state: %+v", s)
	}
}

// TestStatusesOneScan: three plans and a hundred entries cost one Scan and
// one Unreachable.
func TestStatusesOneScan(t *testing.T) {
	dir, head := newRepo(t)
	testgit.Commit(t, dir, "a.txt", "a\n\nParaplane-Plan: p1.md\nParaplane-Task: T1\n")
	c := countCalls(t)
	var plans []PlanRef
	st := &state.State{}
	for i := range 3 {
		rel := fmt.Sprintf("p%d.md", i+1)
		plans = append(plans, PlanRef{Rel: rel, Hash: hashA, Plan: threeWaves()})
		for j := range 34 {
			h := head
			if j%2 == 1 {
				h = strings.Repeat(fmt.Sprintf("%x", j%16), 40) // unknown commit
			}
			st.Entries = append(st.Entries, state.Entry{Plan: rel, Task: "T2", Head: h, PlanHash: hashA})
		}
	}
	res, err := statusesApply(t, dir, plans, st)
	if err != nil {
		t.Fatal(err)
	}
	if c.scans != 1 || c.unreach != 1 {
		t.Errorf("scans %d, unreachable %d; want 1 and 1", c.scans, c.unreach)
	}
	if res["p1.md"].Waves != 1 || res["p2.md"].Tasks != 1 || res["p3.md"].Tasks != 1 {
		t.Errorf("%+v", res)
	}
}

// TestIncrementalScanCache: the second call reads only the new commits and
// keeps the cached ones; rewritten history is read again in full.
func TestIncrementalScanCache(t *testing.T) {
	dir, _ := newRepo(t)
	testgit.Commit(t, dir, "a.txt", "a\n\nParaplane-Plan: plan.md\nParaplane-Task: T1\n")
	plans := []PlanRef{{Rel: "plan.md", Hash: hashA, Plan: threeWaves()}}
	st := &state.State{}
	if _, err := statusesApply(t, dir, plans, st); err != nil {
		t.Fatal(err)
	}
	head1 := testgit.Run(t, dir, "rev-parse", "HEAD")
	if st.Scan == nil || st.Scan.Head != head1 || len(st.Scan.Commits) != 1 {
		t.Fatalf("cache after first call: %+v", st.Scan)
	}
	testgit.Commit(t, dir, "b.txt", "b\n\nParaplane-Plan: plan.md\nParaplane-Task: T2\n")
	var froms []string
	old := scan
	scan = func(ctx context.Context, root, from string) (gitx.Scanned, error) {
		froms = append(froms, from)
		return old(ctx, root, from)
	}
	defer func() { scan = old }()
	res, err := statusesApply(t, dir, plans, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(froms) != 1 || froms[0] != head1 {
		t.Errorf("scan from %q, want %q", froms, head1)
	}
	if res["plan.md"].Waves != 1 || len(st.Scan.Commits) != 2 {
		t.Errorf("%+v cache %d", res["plan.md"], len(st.Scan.Commits))
	}
	// Same HEAD: nothing new, the cache stays.
	if res, _ := statusesApply(t, dir, plans, st); res["plan.md"].Waves != 1 || len(st.Scan.Commits) != 2 {
		t.Errorf("same head: %+v cache %d", res["plan.md"], len(st.Scan.Commits))
	}
	// Rewritten history: back to before both task commits, one new commit.
	testgit.Run(t, dir, "reset", "-q", "--hard", "HEAD~2")
	testgit.Commit(t, dir, "c.txt", "c\n\nParaplane-Plan: plan.md\nParaplane-Task: T3\n")
	res, err = statusesApply(t, dir, plans, st)
	if err != nil {
		t.Fatal(err)
	}
	if s := res["plan.md"]; s.Waves != 0 || s.Tasks != 0 || len(st.Scan.Commits) != 1 {
		t.Errorf("after rewrite: %+v cache %d", s, len(st.Scan.Commits))
	}
}

// TestForgedScanCache: a cache with invalid commits is dropped and the
// history read in full.
func TestForgedScanCache(t *testing.T) {
	dir, head := newRepo(t)
	plans := []PlanRef{{Rel: "plan.md", Hash: hashA, Plan: threeWaves()}}
	for _, raw := range []string{
		`{"Hash":"zz","Plan":"plan.md","Waves":["W1"]}`,
		`{"Hash":"` + head + `","Plan":"plan.md","Waves":["bad id!"]}`,
		`{"Hash":"` + head + `","Plan":"","Waves":["W1"]}`,
		`[1,2]`,
	} {
		st := &state.State{Scan: &state.ScanCache{Head: head, Commits: []json.RawMessage{json.RawMessage(raw)}}}
		res, err := statusesApply(t, dir, plans, st)
		if err != nil || res["plan.md"].Waves != 0 || len(st.Scan.Commits) != 0 {
			t.Errorf("%s: %+v %v cache %+v", raw, res["plan.md"], err, st.Scan)
		}
	}
}

// TestPruneCommitted: Apply prunes entries of committed tasks (by task or
// wave commit, any block hash) and entries of the current block whose task
// is not in the plan. Entries of another block, with an unreachable head or
// of other plans only do not count; they stay.
func TestPruneCommitted(t *testing.T) {
	dir, head := newRepo(t)
	testgit.Commit(t, dir, "a.txt", "a\n\nParaplane-Plan: plan.md\nParaplane-Task: T1\n")
	testgit.Commit(t, dir, "b.txt", "b\n\nParaplane-Plan: plan.md\nParaplane-Wave: W2\n")
	orphan := strings.Repeat("c", 40)
	keep := []state.Entry{
		{Plan: "plan.md", Task: "T2", Head: head, PlanHash: hashA},
		{Plan: "plan.md", Task: "T4", Head: head, PlanHash: hashB},   // other block
		{Plan: "plan.md", Task: "T9", Head: head, PlanHash: hashB},   // other block, unknown task
		{Plan: "plan.md", Task: "T4", Head: orphan, PlanHash: hashA}, // unreachable head
		{Plan: "other.md", Task: "X2", Head: orphan, PlanHash: hashB},
	}
	st := &state.State{Entries: append([]state.Entry{
		{Plan: "plan.md", Task: "T1", Head: head, PlanHash: hashA}, // committed task
		{Plan: "plan.md", Task: "T3", Head: head, PlanHash: hashB}, // committed wave, other block
		{Plan: "plan.md", Task: "T9", Head: head, PlanHash: hashA}, // not in the current plan
	}, keep...)}
	res, err := statusesApply(t, dir, []PlanRef{{Rel: "plan.md", Hash: hashA, Plan: threeWaves()}}, st)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(st.Entries, keep) {
		t.Errorf("entries:\n got %+v\nwant %+v", st.Entries, keep)
	}
	if s := res["plan.md"]; s.Waves != 2 || s.Tasks != 3 {
		t.Errorf("%+v", s)
	}
}

// TestBranchSwitchBack: an entry made on another branch does not count while
// it is unreachable and counts again after switching back.
func TestBranchSwitchBack(t *testing.T) {
	dir, _ := newRepo(t)
	testgit.Run(t, dir, "checkout", "-q", "-b", "side")
	testgit.Commit(t, dir, "s.txt", "side work")
	side := testgit.Run(t, dir, "rev-parse", "HEAD")
	plans := []PlanRef{{Rel: "plan.md", Hash: hashA, Plan: threeWaves()}}
	st := &state.State{Entries: []state.Entry{{Plan: "plan.md", Task: "T1", Head: side, PlanHash: hashA}}}
	testgit.Run(t, dir, "checkout", "-q", "main")
	res, err := statusesApply(t, dir, plans, st)
	if err != nil || res["plan.md"].Tasks != 0 || len(st.Entries) != 1 {
		t.Fatalf("on main: %+v %v %+v", res["plan.md"], err, st.Entries)
	}
	testgit.Run(t, dir, "checkout", "-q", "side")
	if res, err = statusesApply(t, dir, plans, st); err != nil || res["plan.md"].Tasks != 1 {
		t.Errorf("back on side: %+v %v", res["plan.md"], err)
	}
}

// TestApplyLaterState: Statuses leaves its input alone; Apply on a state
// loaded later keeps the entries added meanwhile (unless the rule drops
// them) and sets the new scan cache.
func TestApplyLaterState(t *testing.T) {
	dir, head := newRepo(t)
	testgit.Commit(t, dir, "a.txt", "a\n\nParaplane-Plan: plan.md\nParaplane-Task: T1\n")
	st := state.State{Entries: []state.Entry{{Plan: "plan.md", Task: "T1", Head: head, PlanHash: hashA}}}
	r, err := Statuses(ctx(t), dir, []PlanRef{{Rel: "plan.md", Hash: hashA, Plan: threeWaves()}}, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Entries) != 1 || st.Scan != nil {
		t.Errorf("Statuses changed its input: %+v", st)
	}
	added := state.Entry{Plan: "plan.md", Task: "T2", Head: head, PlanHash: hashA}
	later := state.State{Entries: append(slices.Clone(st.Entries), added,
		state.Entry{Plan: "plan.md", Task: "T1", Head: head, PlanHash: hashB})}
	r.Apply(&later)
	if !slices.Equal(later.Entries, []state.Entry{added}) {
		t.Errorf("entries: %+v", later.Entries)
	}
	if later.Scan == nil || later.Scan.Head != testgit.Run(t, dir, "rev-parse", "HEAD") || len(later.Scan.Commits) != 1 {
		t.Errorf("scan: %+v", later.Scan)
	}
}

// TestRecordedSeparateFromDone: state entries go to Recorded with their own
// warning; --done ids keep the --done warnings.
func TestRecordedSeparateFromDone(t *testing.T) {
	dir, head := newRepo(t)
	g := testgit.Run(t, dir, "rev-parse", "--absolute-git-dir")
	if err := state.UpdateCtx(ctx(t), g, func(s *state.State) {
		s.Add(state.Entry{Plan: "plan.md", Task: "T3", Head: head, PlanHash: hashA})
	}); err != nil {
		t.Fatal(err)
	}
	s, err := PlanStatusContext(ctx(t), dir, "plan.md", threeWaves(), hashA, []string{"T4", "Z9"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"warning: task T3 finished before W1\n",
		"warning: --done T4 is in waiting wave W3\n",
		"warning: --done Z9 is not a task of the plan\n",
	} {
		if !strings.Contains(s.Text, want) {
			t.Errorf("missing %q in\n%s", want, s.Text)
		}
	}
	if strings.Contains(s.Text, "--done T3") {
		t.Errorf("recorded task warned as --done:\n%s", s.Text)
	}
}

// TestSources: commits, state entries and --done, alone and together.
func TestSources(t *testing.T) {
	type tc struct {
		name           string
		commits        []string // messages
		entries        []string // tasks recorded at HEAD under hashA
		extra          []string
		waves, tasks   int
		warn, notWarns []string
	}
	for _, c := range []tc{
		{name: "nothing"},
		{name: "task commit", commits: []string{"Paraplane-Task: T1"}, tasks: 1},
		{name: "wave commit", commits: []string{"Paraplane-Wave: W1"}, waves: 1, tasks: 2},
		{name: "entry", entries: []string{"T1"}, tasks: 1},
		{name: "done", extra: []string{"T1"}, tasks: 1},
		{name: "commit and entry", commits: []string{"Paraplane-Task: T1"}, entries: []string{"T2"}, waves: 1, tasks: 2},
		{name: "entry and done", entries: []string{"T1"}, extra: []string{"T2", "T3"}, waves: 2, tasks: 3},
		{name: "all three", commits: []string{"Paraplane-Task: T1"}, entries: []string{"T2"}, extra: []string{"T3", "T4"}, waves: 3, tasks: 4},
		{name: "same task twice", commits: []string{"Paraplane-Task: T1"}, entries: []string{"T1"}, extra: []string{"T1"}, tasks: 1},
		{name: "waiting entry", entries: []string{"T4"}, warn: []string{"warning: task T4 finished before W1"}, notWarns: []string{"--done"}},
		{name: "waiting commit", commits: []string{"Paraplane-Task: T3"}, warn: []string{"warning: task T3 was committed before W1"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir, _ := newRepo(t)
			for i, m := range c.commits {
				testgit.Commit(t, dir, fmt.Sprintf("f%d.txt", i), "work\n\nParaplane-Plan: plan.md\n"+m+"\n")
			}
			head := testgit.Run(t, dir, "rev-parse", "HEAD")
			g := testgit.Run(t, dir, "rev-parse", "--absolute-git-dir")
			if err := state.UpdateCtx(ctx(t), g, func(s *state.State) {
				for _, id := range c.entries {
					s.Add(state.Entry{Plan: "plan.md", Task: id, Head: head, PlanHash: hashA})
				}
			}); err != nil {
				t.Fatal(err)
			}
			s, err := PlanStatusContext(ctx(t), dir, "plan.md", threeWaves(), hashA, c.extra)
			if err != nil {
				t.Fatal(err)
			}
			if s.Waves != c.waves || s.Tasks != c.tasks {
				t.Errorf("waves %d tasks %d, want %d %d\n%s", s.Waves, s.Tasks, c.waves, c.tasks, s.Text)
			}
			for _, w := range c.warn {
				if !strings.Contains(s.Text, w) {
					t.Errorf("missing %q in\n%s", w, s.Text)
				}
			}
			for _, w := range c.notWarns {
				if strings.Contains(s.Text, w) {
					t.Errorf("unexpected %q in\n%s", w, s.Text)
				}
			}
		})
	}
}

// TestNoCommits: a repository without commits has no progress and no error;
// entries cannot be reachable there, but they are kept.
func TestNoCommits(t *testing.T) {
	testgit.Isolate(t)
	dir := testgit.Init(t)
	st := &state.State{Entries: []state.Entry{{Plan: "plan.md", Task: "T1", Head: strings.Repeat("a", 40), PlanHash: hashA}}}
	res, err := statusesApply(t, dir, []PlanRef{{Rel: "plan.md", Hash: hashA, Plan: threeWaves()}}, st)
	if err != nil || res["plan.md"].Tasks != 0 || len(st.Entries) != 1 || st.Scan != nil {
		t.Errorf("%+v %v %+v", res, err, st.Entries)
	}
}

// TestGitErrorsReturned (L13): a failing Unreachable is an error, not a
// silent "not done"; the state is left alone.
func TestGitErrorsReturned(t *testing.T) {
	dir, head := newRepo(t)
	old := unreachable
	unreachable = func(context.Context, string, []string, string) (map[string]bool, error) {
		return nil, errors.New("boom")
	}
	defer func() { unreachable = old }()
	e := state.Entry{Plan: "plan.md", Task: "T1", Head: head, PlanHash: hashA}
	st := &state.State{Entries: []state.Entry{e}}
	if _, err := Statuses(ctx(t), dir, []PlanRef{{Rel: "plan.md", Hash: hashA, Plan: threeWaves()}}, *st); err == nil {
		t.Error("no error")
	}
	if len(st.Entries) != 1 || st.Scan != nil {
		t.Errorf("state changed on error: %+v", st)
	}
	if _, err := PlanStatusContext(ctx(t), t.TempDir(), "plan.md", threeWaves(), hashA, nil); err == nil {
		t.Error("not a repository: no error")
	}
}

// TestStateVersionIgnored: a state of another version is not read and is
// no error.
func TestStateVersionIgnored(t *testing.T) {
	dir, head := newRepo(t)
	g := testgit.Run(t, dir, "rev-parse", "--absolute-git-dir")
	if err := os.MkdirAll(filepath.Join(g, "paraplane"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"version":99,"entries":[{"plan":"plan.md","task":"T1","head":"` + head + `"}]}`
	if err := os.WriteFile(filepath.Join(g, "paraplane", "closed.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, err := PlanStatusContext(ctx(t), dir, "plan.md", threeWaves(), hashA, nil); err != nil || s.Tasks != 0 {
		t.Errorf("%+v %v", s, err)
	}
}

func TestBudgetExceeded(t *testing.T) {
	dir, head := newRepo(t)
	c, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)
	st := &state.State{Entries: []state.Entry{{Plan: "plan.md", Task: "T1", Head: head, PlanHash: hashA}}}
	if _, err := Statuses(c, dir, []PlanRef{{Rel: "plan.md", Hash: hashA, Plan: threeWaves()}}, *st); err == nil {
		t.Error("Statuses: no error")
	}
	if _, err := PlanStatusContext(c, dir, "plan.md", threeWaves(), hashA, nil); err == nil {
		t.Error("PlanStatusContext: no error")
	}
}

func BenchmarkStatuses(b *testing.B) {
	dir, head := newRepo(b)
	testgit.Commit(b, dir, "a.txt", "a\n\nParaplane-Plan: plan.md\nParaplane-Task: T1\n")
	p := threeWaves()
	var entries []state.Entry
	for i := range 4096 {
		h := head
		if i%4 == 0 {
			h = fmt.Sprintf("%040x", i)
		}
		entries = append(entries, state.Entry{Plan: fmt.Sprintf("p%d.md", i%8), Task: "T2", Head: h, PlanHash: hashA})
	}
	plans := []PlanRef{{Rel: "p0.md", Hash: hashA, Plan: p}, {Rel: "p1.md", Hash: hashA, Plan: p}}
	b.ResetTimer()
	for b.Loop() {
		st := state.State{Entries: append([]state.Entry(nil), entries...)}
		r, err := Statuses(context.Background(), dir, plans, st)
		if err != nil {
			b.Fatal(err)
		}
		r.Apply(&st)
	}
}

// TestCommittedPruneNeedsReachableHead: an entry of a task committed on the
// current branch is pruned only when its head is reachable; one recorded on
// another branch stays for when that branch is checked out again.
func TestCommittedPruneNeedsReachableHead(t *testing.T) {
	dir, _ := newRepo(t)
	testgit.Run(t, dir, "checkout", "-q", "-b", "side")
	testgit.Commit(t, dir, "s.txt", "side work")
	side := testgit.Run(t, dir, "rev-parse", "HEAD")
	testgit.Run(t, dir, "checkout", "-q", "main")
	testgit.Commit(t, dir, "a.txt", "a\n\nParaplane-Plan: plan.md\nParaplane-Task: T2\n")
	head := testgit.Run(t, dir, "rev-parse", "HEAD")
	onSide := state.Entry{Plan: "plan.md", Task: "T2", Head: side, PlanHash: hashA}
	st := &state.State{Entries: []state.Entry{onSide, {Plan: "plan.md", Task: "T2", Head: head, PlanHash: hashA}}}
	if _, err := statusesApply(t, dir, []PlanRef{{Rel: "plan.md", Hash: hashA, Plan: threeWaves()}}, st); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(st.Entries, []state.Entry{onSide}) {
		t.Errorf("entries: %+v", st.Entries)
	}
}

// TestCacheOverCap: a scan above maxCache is not cached, and the statuses
// stay right on this and the next call.
func TestCacheOverCap(t *testing.T) {
	dir, _ := newRepo(t)
	testgit.Commit(t, dir, "a.txt", "a\n\nParaplane-Plan: plan.md\nParaplane-Task: T1\n")
	testgit.Commit(t, dir, "b.txt", "b\n\nParaplane-Plan: plan.md\nParaplane-Task: T2\n")
	old := maxCache
	maxCache = 10
	defer func() { maxCache = old }()
	if c := storeCache(strings.Repeat("a", 40), []gitx.TrailerCommit{{Hash: strings.Repeat("b", 40), Plan: "plan.md", Tasks: []string{"T1"}}}); c != nil {
		t.Errorf("storeCache over the cap: %+v", c)
	}
	plans := []PlanRef{{Rel: "plan.md", Hash: hashA, Plan: threeWaves()}}
	st := &state.State{Scan: &state.ScanCache{Head: strings.Repeat("d", 40)}}
	for i := range 2 {
		res, err := statusesApply(t, dir, plans, st)
		if err != nil || res["plan.md"].Waves != 1 || res["plan.md"].Tasks != 2 || st.Scan != nil {
			t.Errorf("call %d: %+v %v scan %+v", i, res["plan.md"], err, st.Scan)
		}
	}
}
