package rules

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"paraplane/internal/pathset"
	"paraplane/internal/plan"
)

// Audit checks the files changed by wave waveID (spec section 5.1): each must
// match the owns of some task of the wave, unless it is the plan file
// planPath or matches an audit_ignore pattern. changed and planPath are
// relative to the project root with "/" separators. p must be the Plan of a
// Check result, whose paths Check has cleaned. The A1 findings follow the
// order of changed; an unknown wave is an error.
func Audit(p *plan.Plan, waveID string, changed []string, planPath string, ignore []string) ([]Finding, error) {
	return AuditWaves(p, []string{waveID}, changed, planPath, ignore)
}

// AuditWaves is Audit for several waves at once, as for a stage of waves
// committed together: a file may be owned by a task of any of them. The
// findings name the waves joined by "," (Where) and ", " (message).
func AuditWaves(p *plan.Plan, waveIDs []string, changed []string, planPath string, ignore []string) ([]Finding, error) {
	return AuditCommits(p, waveIDs, changed, nil, planPath, ignore)
}

// CommitFiles is a commit of the plan: the waves and tasks its trailers name
// and the files it changed.
type CommitFiles struct {
	Hash                string
	Waves, Tasks, Files []string
}

// AuditCommits is AuditWaves that also checks the commits of the waves
// (spec 2026-10-23 §9): a commit naming a task of a listed wave, or a listed
// wave. A file of a task commit must be owned by one of the tasks it names;
// a file of a wave or stage commit by a task of one of the waves it names.
// Commit findings come first, in commit order, each file once per commit.
func AuditCommits(p *plan.Plan, waveIDs []string, changed []string, commits []CommitFiles, planPath string, ignore []string) ([]Finding, error) {
	if p == nil {
		return nil, errors.New("no plan to audit")
	}
	listed := map[string]bool{}
	var owns []string
	for _, id := range waveIDs {
		w := findWave(p, id)
		if w == nil {
			return nil, fmt.Errorf("unknown wave %s", id)
		}
		listed[id] = true
		for _, t := range w.Tasks {
			owns = append(owns, t.Owns...)
		}
	}
	waveOf, taskOwns := map[string]string{}, map[string][]string{}
	for _, w := range p.Waves {
		for _, t := range w.Tasks {
			waveOf[t.ID], taskOwns[t.ID] = w.ID, t.Owns
		}
	}
	where, names := strings.Join(waveIDs, ","), strings.Join(waveIDs, ", ")
	skip := func(f string) bool { return f == planPath || matchAny(ignore, f) }
	var out []Finding
	add := func(msg string) {
		out = append(out, Finding{Code: "A1", Severity: Error, Where: where, Msg: msg})
	}
	for _, c := range commits {
		relevant := slices.ContainsFunc(c.Waves, func(w string) bool { return listed[w] }) ||
			slices.ContainsFunc(c.Tasks, func(t string) bool { return listed[waveOf[t]] })
		if !relevant {
			continue
		}
		var allowed []string
		for _, t := range c.Tasks {
			allowed = append(allowed, taskOwns[t]...)
		}
		for _, w := range c.Waves {
			if wv := findWave(p, w); wv != nil {
				for _, t := range wv.Tasks {
					allowed = append(allowed, t.Owns...)
				}
			}
		}
		seen := map[string]bool{}
		for _, f := range c.Files {
			if seen[f] || skip(f) || matchAny(allowed, f) {
				continue
			}
			seen[f] = true
			switch {
			case len(c.Waves) > 0:
				add(fmt.Sprintf("%s changed in commit %s but no task of %s owns it", f, c.Hash, strings.Join(c.Waves, ", ")))
			case len(c.Tasks) == 1:
				add(fmt.Sprintf("%s changed by %s (commit %s) but %s does not own it", f, c.Tasks[0], c.Hash, c.Tasks[0]))
			default:
				add(fmt.Sprintf("%s changed by %s (commit %s) but none of them owns it", f, strings.Join(c.Tasks, ", "), c.Hash))
			}
		}
	}
	for _, f := range changed {
		if skip(f) || matchAny(owns, f) {
			continue
		}
		add(fmt.Sprintf("%s changed but no task of %s owns it", f, names))
	}
	return out, nil
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

func matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if pathset.Match(p, name) {
			return true
		}
	}
	return false
}
