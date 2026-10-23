// Package track puts together what is known done for a plan: commit
// trailers (waves, stages, tasks), returned subagents kept in the state file,
// and tasks passed with --done (spec 2026-10-23 §4). The CLI and the progress
// hook both render progress through it.
//
// The history is read once per call for all plans (gitx.Scan), incrementally
// from the scan cached in the state; the heads of all state entries are
// checked with one gitx.Unreachable. A state entry counts only for the same
// plan block (PlanHash), with a reachable head and a task of the plan.
package track

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"paraplane/internal/gitx"
	"paraplane/internal/ids"
	"paraplane/internal/plan"
	"paraplane/internal/progress"
	"paraplane/internal/state"
	"paraplane/internal/textx"
)

// maxID caps the runes of an id echoed in a warning.
const maxID = 32

// maxCache caps the bytes of the scan kept in the state: closed.json is read
// up to 4 MiB, and a larger scan would make the whole state unreadable. A
// scan above the cap is not cached (the next call reads the history again).
// A variable so tests can lower it.
var maxCache = 1 << 20

// The git calls, replaced in tests to count them or make them fail.
var (
	scan        = gitx.Scan
	unreachable = gitx.Unreachable
)

var fullHash = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// Status is the rendered progress of a plan and how much of it is done.
type Status struct {
	Text  string // progress output, lost-trailer warnings last
	Waves int    // done waves
	Tasks int    // done tasks (done waves and done tasks of the running wave)
}

// PlanRef is a plan to report on: its path from the root, the hash of its
// block (PlanHash) and the parsed plan.
type PlanRef struct {
	Rel, Hash string
	Plan      *plan.Plan
}

// PlanHash is the hex SHA-256 of the ```paraplane block body of md, or ""
// when md has no single block.
func PlanHash(md []byte) string {
	b, err := plan.Extract(md)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b.Body)
	return hex.EncodeToString(sum[:])
}

// Result is what Statuses found: the status of each plan, keyed by Rel, and
// Apply, which brings a state up to date with it.
type Result struct {
	ByPlan map[string]Status
	// Apply sets s.Scan to the new scan cache (nil when too large or without
	// commits) and prunes the entries of tasks now committed (by a task or a
	// wave commit of their plan, whatever their block hash) and the entries
	// recorded for the current block of a plan (same PlanHash) whose task is
	// not in it. It runs no git and is safe on a state loaded later than the
	// one given to Statuses (inside state.UpdateCtx): entries added meanwhile
	// stay unless the same rule drops them. Entries with an unreachable head
	// or another block hash only do not count; they are kept, so a branch
	// switch back or a reverted plan text brings them back.
	Apply func(s *state.State)
}

// Statuses renders the progress of each of plans from st with one Scan
// (from st.Scan when it is still an ancestor of HEAD) and one Unreachable
// for every entry head. st is not changed; the caller applies Result.Apply
// to the state it writes. Git errors are returned as they are. Of several
// PlanRefs with the same Rel only the first is used; the others are ignored.
func Statuses(ctx context.Context, root string, plans []PlanRef, st state.State) (Result, error) {
	r, err := compute(ctx, root, plans, &st)
	if err != nil {
		return Result{}, err
	}
	return Result{ByPlan: r.statuses, Apply: func(s *state.State) {
		s.Scan = r.cache
		s.Prune(r.keep)
	}}, nil
}

// PlanStatusContext renders the progress of plan p at planRel from root
// whose block hash is planHash: commits, entries of the state file (read,
// never written; a state of another version is ignored) and extra (--done).
// Git and state read errors are returned.
func PlanStatusContext(ctx context.Context, root, planRel string, p *plan.Plan, planHash string, extra []string) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	g, err := gitx.GitDirContext(ctx, root)
	if err != nil {
		return Status{}, err
	}
	st, err := state.LoadState(g)
	if err != nil && !errors.Is(err, state.ErrVersion) {
		return Status{}, err
	}
	r, err := compute(ctx, root, []PlanRef{{Rel: planRel, Hash: planHash, Plan: p}}, &st, extra...)
	if err != nil {
		return Status{}, err
	}
	return r.statuses[planRel], nil
}

// PlanStatus is PlanStatusContext without a deadline and with an empty plan
// hash, which matches the entries recorded without one.
//
// Deprecated: use PlanStatusContext or Statuses; R13 removes this wrapper.
func PlanStatus(root, planRel string, p *plan.Plan, extra []string) (Status, error) {
	return PlanStatusContext(context.Background(), root, planRel, p, "", extra)
}

// result is what compute found; nothing is applied to the state.
type result struct {
	statuses map[string]Status
	cache    *state.ScanCache
	keep     func(state.Entry) bool
}

// compute scans, checks the entry heads and renders every plan; extra is
// the --done list of each plan.
func compute(ctx context.Context, root string, plans []PlanRef, st *state.State, extra ...string) (result, error) {
	if err := ctx.Err(); err != nil {
		return result{}, err
	}
	cached, from := loadCache(st.Scan)
	sc, err := scan(ctx, root, from)
	if err != nil {
		return result{}, err
	}
	var unreach map[string]bool
	commits := sc.Commits
	if sc.Head == "" { // no commits: no entry is reachable
		unreach = map[string]bool{}
		for _, e := range st.Entries {
			unreach[e.Head] = true
		}
	} else {
		heads := make([]string, 0, len(st.Entries)+1)
		for _, e := range st.Entries {
			heads = append(heads, e.Head)
		}
		if from != "" {
			heads = append(heads, from)
		}
		if unreach, err = unreachable(ctx, root, heads, sc.Head); err != nil {
			return result{}, err
		}
		if from != "" && !unreach[from] { // incremental: from..HEAD was read
			commits = merge(cached, sc.Commits)
		}
	}
	byRel := map[string]*judge{}
	res := result{statuses: make(map[string]Status, len(plans)), cache: storeCache(sc.Head, commits)}
	for _, pr := range plans {
		if _, dup := byRel[pr.Rel]; dup {
			continue
		}
		j := newJudge(pr, gitx.CommitsOf(commits, pr.Rel))
		byRel[pr.Rel] = j
		var recorded []string
		for _, e := range st.Entries {
			if e.Plan == pr.Rel && !unreach[e.Head] && j.counts(e) && !slices.Contains(recorded, e.Task) {
				recorded = append(recorded, e.Task)
			}
		}
		res.statuses[pr.Rel] = j.status(recorded, extra)
	}
	// A committed task's entry is dropped only when its head is known to be
	// reachable: one recorded on another branch, where the task is not
	// committed, must survive a visit to this branch. Heads not checked here
	// (entries added after st was read) count as unknown and are kept.
	res.keep = func(e state.Entry) bool {
		j, ok := byRel[e.Plan]
		if !ok {
			return true
		}
		u, checked := unreach[e.Head]
		if checked && !u && j.committed(e.Task) {
			return false
		}
		return e.PlanHash != j.ref.Hash || j.has(e.Task)
	}
	return res, nil
}

// judge answers for one plan which entries count and which tasks are
// committed.
type judge struct {
	ref    PlanRef
	c      gitx.Commits
	waveOf map[string]string // task id → wave id
}

func newJudge(pr PlanRef, c gitx.Commits) *judge {
	j := &judge{ref: pr, c: c, waveOf: map[string]string{}}
	for _, w := range pr.Plan.Waves {
		for _, t := range w.Tasks {
			j.waveOf[t.ID] = w.ID
		}
	}
	return j
}

// has reports whether task is a task of the plan.
func (j *judge) has(task string) bool {
	_, ok := j.waveOf[task]
	return ok
}

// counts reports whether e was recorded for this block and names one of its
// tasks (the head is checked by the caller).
func (j *judge) counts(e state.Entry) bool {
	return e.PlanHash == j.ref.Hash && j.has(e.Task)
}

// committed reports whether a commit names the task or its wave.
func (j *judge) committed(task string) bool {
	if slices.Contains(j.c.Tasks, task) {
		return true
	}
	w, ok := j.waveOf[task]
	return ok && slices.Contains(j.c.Waves, w)
}

// status renders the plan from its commits, recorded tasks and --done ids.
func (j *judge) status(recorded, extra []string) Status {
	in := progress.Input{
		CommittedWaves: j.c.Waves, CommittedTasks: j.c.Tasks,
		Closed: slices.Clone(extra), Recorded: recorded,
	}
	text := progress.RenderFrom(j.ref.Plan, planBase(j.ref.Rel), in)
	for _, l := range j.c.Lost {
		text += fmt.Sprintf("warning: %s %s commit %s: trailers not in the last paragraph\n",
			l.Kind, textx.Line(l.ID, maxID), textx.Line(l.Commit, 16))
	}
	n := progress.Count(j.ref.Plan, in)
	return Status{Text: text, Waves: n.Waves, Tasks: n.Tasks}
}

// loadCache decodes the cached scan; any invalid commit drops the whole
// cache (from is then "", a full scan).
func loadCache(c *state.ScanCache) (commits []gitx.TrailerCommit, from string) {
	if c == nil || !fullHash.MatchString(c.Head) {
		return nil, ""
	}
	for _, raw := range c.Commits {
		var tc gitx.TrailerCommit
		if json.Unmarshal(raw, &tc) != nil || !validCommit(tc) {
			return nil, ""
		}
		commits = append(commits, tc)
	}
	return commits, c.Head
}

// validCommit reports whether a cached commit could have come from Scan.
func validCommit(tc gitx.TrailerCommit) bool {
	if !fullHash.MatchString(tc.Hash) || tc.Plan == "" {
		return false
	}
	n := 0
	for _, l := range [][]string{tc.Waves, tc.Tasks, tc.LostWaves, tc.LostTasks} {
		for _, id := range l {
			if !ids.Valid(id) {
				return false
			}
			n++
		}
	}
	return n > 0
}

// storeCache encodes the scan up to head; nil without commits in the
// repository or above maxCache bytes.
//
// The cache holds gitx trailer parse results (gitx.TrailerCommit), not raw
// commits: a change to trailer parsing in gitx (keys, id grammar, lost-line
// rules) must bump the state version or otherwise invalidate the cache, or
// old parses would be reused for the commits before the cached head.
func storeCache(head string, commits []gitx.TrailerCommit) *state.ScanCache {
	if head == "" {
		return nil
	}
	c := &state.ScanCache{Head: head}
	size := 0
	for _, tc := range commits {
		raw, err := json.Marshal(tc)
		if err != nil {
			return nil
		}
		if size += len(raw); size > maxCache {
			return nil
		}
		c.Commits = append(c.Commits, raw)
	}
	return c
}

// merge appends the new commits not already cached.
func merge(cached, fresh []gitx.TrailerCommit) []gitx.TrailerCommit {
	seen := make(map[string]bool, len(cached))
	out := make([]gitx.TrailerCommit, 0, len(cached)+len(fresh))
	for _, tc := range slices.Concat(cached, fresh) {
		if !seen[tc.Hash] {
			seen[tc.Hash] = true
			out = append(out, tc)
		}
	}
	return out
}

// planBase is the file name shown in the progress header.
func planBase(rel string) string {
	for i := len(rel) - 1; i >= 0; i-- {
		if rel[i] == '/' {
			return rel[i+1:]
		}
	}
	return rel
}
