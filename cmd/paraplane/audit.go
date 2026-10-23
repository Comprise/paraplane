package main

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"paraplane/internal/config"
	"paraplane/internal/gitx"
	"paraplane/internal/hooks"
	"paraplane/internal/plan"
	"paraplane/internal/rules"
	"paraplane/internal/textx"
)

// maxWave caps the runes of a wave id echoed from the arguments.
const maxWave = 64

// waveList matches the wave argument of audit: one wave id, or several joined
// by "," for a stage committed at once.
var waveList = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,31}(,[A-Za-z][A-Za-z0-9_.-]{0,31})*$`)

func runAudit(args []string, stdout, stderr io.Writer, cwd string) int {
	if len(args) != 2 || (strings.Contains(args[1], ",") && !waveList.MatchString(args[1])) {
		fmt.Fprint(stderr, usage)
		return 2
	}
	root := config.FindRoot(cwd)
	env := loadEnv(stdout, root)
	p, rel, err := loadValidPlan(args[0], cwd, env)
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	changed, err := gitx.Changed(root)
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	commits, err := gitx.PlanCommitFiles(root, rel)
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	var cf []rules.CommitFiles
	for _, c := range commits {
		cf = append(cf, rules.CommitFiles{Hash: c.Hash, Waves: c.Waves, Tasks: c.Tasks, Files: c.Files})
	}
	waves := strings.Split(args[1], ",")
	findings, err := rules.AuditCommits(p, waves, changed, cf, rel, env.Config.AuditIgnore)
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	wave := textx.Line(args[1], maxWave)
	for _, f := range findings {
		fmt.Fprintln(stdout, textx.Line(f.String(), maxLine))
	}
	if len(findings) > 0 {
		fmt.Fprintf(stdout, "audit failed: %d file(s) outside %s\n", len(findings), wave)
		return 1
	}
	if n := waveCommits(cf, p, waves); n > 0 {
		fmt.Fprintf(stdout, "audit ok: %s, %d changed file(s), %d commit(s)\n", wave, len(changed), n)
		return 0
	}
	fmt.Fprintf(stdout, "audit ok: %s, %d changed file(s)\n", wave, len(changed))
	return 0
}

// loadValidPlan reads the plan file name and checks it; a plan with errors
// (E0–E7) is an error. rel is the plan path from the root with "/".
func loadValidPlan(name, cwd string, env hooks.Env) (*plan.Plan, string, error) {
	md, rel, err := readPlanFile(name, cwd, env.Root)
	if err != nil {
		return nil, "", err
	}
	r := rules.Check(md, cliOptions(env))
	if n := r.Errors(); n > 0 || r.Plan == nil {
		first := ""
		for _, f := range r.Findings {
			if f.Severity == rules.Error {
				first = f.String()
				break
			}
		}
		return nil, "", fmt.Errorf("%s is not a valid plan (%d error(s), first: %s); run paraplane check %s",
			textx.Line(name, maxName), n, first, textx.Line(name, maxName))
	}
	return r.Plan, rel, nil
}

// waveCommits counts the commits that name one of waves or a task of them.
func waveCommits(cs []rules.CommitFiles, p *plan.Plan, waves []string) int {
	in := map[string]bool{}
	for _, w := range p.Waves {
		if slices.Contains(waves, w.ID) {
			in[w.ID] = true
			for _, t := range w.Tasks {
				in[t.ID] = true
			}
		}
	}
	n := 0
	for _, c := range cs {
		if slices.ContainsFunc(c.Waves, func(id string) bool { return in[id] }) || slices.ContainsFunc(c.Tasks, func(id string) bool { return in[id] }) {
			n++
		}
	}
	return n
}
