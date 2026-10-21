package rules

import (
	"errors"
	"fmt"

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
	if p == nil {
		return nil, errors.New("no plan to audit")
	}
	var wave *plan.Wave
	for i := range p.Waves {
		if p.Waves[i].ID == waveID {
			wave = &p.Waves[i]
			break
		}
	}
	if wave == nil {
		return nil, fmt.Errorf("unknown wave %s", waveID)
	}
	var owns []string
	for _, t := range wave.Tasks {
		owns = append(owns, t.Owns...)
	}
	var out []Finding
	for _, f := range changed {
		if f == planPath || matchAny(ignore, f) || matchAny(owns, f) {
			continue
		}
		out = append(out, Finding{
			Code:     "A1",
			Severity: Error,
			Where:    waveID,
			Msg:      fmt.Sprintf("%s changed but no task of %s owns it", f, waveID),
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
