// Package track puts together what is known done for a plan: commit
// trailers (waves, stages, tasks), returned subagents kept in the state file,
// and tasks passed with --done (spec 2026-10-23 §4). The CLI and the progress
// hook both render progress through it.
package track

import (
	"fmt"
	"slices"

	"paraplane/internal/gitx"
	"paraplane/internal/plan"
	"paraplane/internal/progress"
	"paraplane/internal/state"
	"paraplane/internal/textx"
)

// maxID caps the runes of an id echoed in a warning.
const maxID = 32

// Status is the rendered progress of a plan and how much of it is done.
type Status struct {
	Text  string // progress output, lost-trailer warnings last
	Waves int    // done waves
	Tasks int    // done tasks (done waves and done tasks of the running wave)
}

// PlanStatus renders the progress of plan p, whose path from root is
// planRel: committed waves and tasks from git, tasks of returned subagents
// from the state (only entries whose HEAD is an ancestor of the current
// one), and extra (--done).
func PlanStatus(root, planRel string, p *plan.Plan, extra []string) (Status, error) {
	c, err := gitx.PlanCommits(root, planRel)
	if err != nil {
		return Status{}, err
	}
	closed := slices.Clone(extra)
	if g, err := gitx.GitDir(root); err == nil {
		head, err := gitx.Head(root)
		if err != nil {
			return Status{}, err
		}
		seen := map[string]bool{}
		valid := func(h string) bool {
			if h == "" || h == head {
				return true
			}
			if v, ok := seen[h]; ok {
				return v
			}
			ok, err := gitx.IsAncestor(root, h, head)
			seen[h] = err == nil && ok
			return seen[h]
		}
		closed = append(state.Load(g).Closed(planRel, valid), closed...)
	}
	in := progress.Input{CommittedWaves: c.Waves, CommittedTasks: c.Tasks, Closed: closed}
	text := progress.RenderFrom(p, planBase(planRel), in)
	for _, l := range c.Lost {
		text += fmt.Sprintf("warning: %s %s commit %s: trailers not in the last paragraph\n",
			l.Kind, textx.Line(l.ID, maxID), textx.Line(l.Commit, 16))
	}
	waves, tasks := count(p, in)
	return Status{Text: text, Waves: waves, Tasks: tasks}, nil
}

// count applies the done rule of progress.RenderFrom: waves in plan order are
// done while closed (a commit, or all tasks done); the first other wave is
// running and adds its done tasks.
func count(p *plan.Plan, in progress.Input) (waves, tasks int) {
	done := map[string]bool{}
	for _, id := range append(slices.Clone(in.CommittedTasks), in.Closed...) {
		done[id] = true
	}
	for _, w := range p.Waves {
		n := 0
		for _, t := range w.Tasks {
			if done[t.ID] {
				n++
			}
		}
		if slices.Contains(in.CommittedWaves, w.ID) || (len(w.Tasks) > 0 && n == len(w.Tasks)) {
			waves++
			tasks += len(w.Tasks)
			continue
		}
		return waves, tasks + n
	}
	return waves, tasks
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
