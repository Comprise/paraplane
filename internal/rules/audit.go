package rules

import (
	"errors"
	"fmt"
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
	if p == nil {
		return nil, errors.New("no plan to audit")
	}
	var owns []string
	for _, id := range waveIDs {
		var wave *plan.Wave
		for i := range p.Waves {
			if p.Waves[i].ID == id {
				wave = &p.Waves[i]
				break
			}
		}
		if wave == nil {
			return nil, fmt.Errorf("unknown wave %s", id)
		}
		for _, t := range wave.Tasks {
			owns = append(owns, t.Owns...)
		}
	}
	where, names := strings.Join(waveIDs, ","), strings.Join(waveIDs, ", ")
	var out []Finding
	for _, f := range changed {
		if f == planPath || matchAny(ignore, f) || matchAny(owns, f) {
			continue
		}
		out = append(out, Finding{
			Code:     "A1",
			Severity: Error,
			Where:    where,
			Msg:      fmt.Sprintf("%s changed but no task of %s owns it", f, names),
		})
	}
	return out, nil
}

func matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if pathset.Match(p, name) {
			return true
		}
	}
	return false
}
