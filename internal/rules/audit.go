package rules

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"paraplane/internal/pathset"
	"paraplane/internal/plan"
)

// Audit is AuditCommits of one wave without commits.
//
// Deprecated: a compatibility wrapper until R13; use AuditCommits.
func Audit(p *plan.Plan, waveID string, changed []string, planPath string, ignore []string) ([]Finding, error) {
	return AuditCommits(p, []string{waveID}, changed, nil, planPath, ignore)
}

// AuditWaves is AuditCommits without commits.
//
// Deprecated: a compatibility wrapper until R13; use AuditCommits.
func AuditWaves(p *plan.Plan, waveIDs []string, changed []string, planPath string, ignore []string) ([]Finding, error) {
	return AuditCommits(p, waveIDs, changed, nil, planPath, ignore)
}

// CommitFiles is a commit of the plan: the waves and tasks its trailers name
// and the files it changed.
type CommitFiles struct {
	Hash                string
	Waves, Tasks, Files []string
}

// waveIndex knows which wave each task of a plan is in.
type waveIndex struct {
	p      *plan.Plan
	waveOf map[string]string   // task id → wave id
	owns   map[string][]string // task id → owns
}

func indexWaves(p *plan.Plan) waveIndex {
	x := waveIndex{p: p, waveOf: map[string]string{}, owns: map[string][]string{}}
	for _, w := range p.Waves {
		for _, t := range w.Tasks {
			x.waveOf[t.ID], x.owns[t.ID] = w.ID, t.Owns
		}
	}
	return x
}

// relevant reports whether commit c belongs to one of the listed waves: it
// names a listed wave, or a task of one.
func (x waveIndex) relevant(c CommitFiles, listed map[string]bool) bool {
	return slices.ContainsFunc(c.Waves, func(w string) bool { return listed[w] }) ||
		slices.ContainsFunc(c.Tasks, func(t string) bool { w, ok := x.waveOf[t]; return ok && listed[w] })
}

// CommitsOfWaves returns how many of commits belong to one of waves, by
// the rule AuditCommits checks them with; waves p does not have count for
// nothing.
func CommitsOfWaves(p *plan.Plan, waves []string, commits []CommitFiles) int {
	if p == nil {
		return 0
	}
	x, listed := indexWaves(p), setOf(slices.DeleteFunc(slices.Clone(waves), func(id string) bool { return findWave(p, id) == nil }))
	n := 0
	for _, c := range commits {
		if x.relevant(c, listed) {
			n++
		}
	}
	return n
}

// auditor collects the A1 findings of one AuditCommits call.
type auditor struct {
	waveIndex
	where    string
	planPath string
	ignore   []string
	out      []Finding
}

func (a *auditor) skip(f string) bool { return f == a.planPath || pathset.MatchAny(a.ignore, f) }

func (a *auditor) add(format string, args ...any) {
	a.out = append(a.out, Finding{Code: "A1", Severity: Error, Where: a.where, Msg: fmt.Sprintf(format, args...)})
}

// waveOwns returns the owns of every task of the waves ids that p has.
func (a *auditor) waveOwns(ids []string) []string {
	var owns []string
	for _, id := range ids {
		if w := findWave(a.p, id); w != nil {
			for _, t := range w.Tasks {
				owns = append(owns, t.Owns...)
			}
		}
	}
	return owns
}

// commit checks the files of c: a file of a task commit must be owned by
// one of the tasks it names, a file of a wave or stage commit by a task of
// one of the waves it names; each file is reported once.
func (a *auditor) commit(c CommitFiles) {
	allowed := a.waveOwns(c.Waves)
	for _, id := range c.Tasks {
		allowed = append(allowed, a.owns[id]...)
	}
	seen := map[string]bool{}
	for _, f := range c.Files {
		if seen[f] || a.skip(f) || pathset.MatchAny(allowed, f) {
			continue
		}
		seen[f] = true
		switch {
		case len(c.Waves) > 0:
			a.add("%s changed in commit %s but no task of %s owns it", f, c.Hash, strings.Join(c.Waves, ", "))
		case len(c.Tasks) == 1:
			a.add("%s changed by %s (commit %s) but %s does not own it", f, c.Tasks[0], c.Hash, c.Tasks[0])
		default:
			a.add("%s changed by %s (commit %s) but none of them owns it", f, strings.Join(c.Tasks, ", "), c.Hash)
		}
	}
}

// AuditCommits checks the files changed by the waves waveIDs (spec section
// 5.1, spec 2026-10-23 §9). changed (the working tree) and planPath are
// relative to the project root with "/" separators; p must be the Plan of a
// Check result, whose paths Check has cleaned. A file is skipped when it is
// planPath or matches an audit_ignore pattern.
//
// The commits of the waves (CommitsOfWaves: a commit naming a listed wave
// or a task of one) are checked first, in commit order: a file of a task
// commit must be owned by one of the tasks it names, a file of a wave or
// stage commit by a task of one of the waves it names; each file once per
// commit. Then each file of changed must be owned by a task of one of the
// waves. The findings name the waves joined by "," (Where) and ", "
// (message). An unknown wave is an error.
func AuditCommits(p *plan.Plan, waveIDs []string, changed []string, commits []CommitFiles, planPath string, ignore []string) ([]Finding, error) {
	if p == nil {
		return nil, errors.New("no plan to audit")
	}
	for _, id := range waveIDs {
		if findWave(p, id) == nil {
			return nil, fmt.Errorf("unknown wave %s", id)
		}
	}
	a := &auditor{waveIndex: indexWaves(p), where: strings.Join(waveIDs, ","), planPath: planPath, ignore: ignore}
	listed := setOf(waveIDs)
	for _, c := range commits {
		if a.relevant(c, listed) {
			a.commit(c)
		}
	}
	owns, names := a.waveOwns(waveIDs), strings.Join(waveIDs, ", ")
	for _, f := range changed {
		if !a.skip(f) && !pathset.MatchAny(owns, f) {
			a.add("%s changed but no task of %s owns it", f, names)
		}
	}
	return a.out, nil
}

// setOf returns the set of ids.
func setOf(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// findWave returns the wave id of p, or nil.
func findWave(p *plan.Plan, id string) *plan.Wave {
	for i := range p.Waves {
		if p.Waves[i].ID == id {
			return &p.Waves[i]
		}
	}
	return nil
}
