package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"paraplane/internal/config"
	"paraplane/internal/gitx"
	"paraplane/internal/hooks"
	"paraplane/internal/ids"
	"paraplane/internal/plan"
	"paraplane/internal/rules"
	"paraplane/internal/textx"
)

// maxWave caps the runes of a wave id echoed from the arguments.
const maxWave = 64

// maxAuditLines caps the A1 lines audit prints; the rest is counted.
const maxAuditLines = 100

func runAudit(args []string, stdout, stderr io.Writer, cwd string) int {
	if len(args) != 2 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	if _, ok := ids.ParseList(args[1]); !ok {
		fmt.Fprint(stderr, usage)
		return 2
	}
	root := config.FindRoot(cwd)
	env := loadEnv(stdout, root)
	p, rel, _, err := loadValidPlan(args[0], cwd, env)
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	// The CLI has no deadline of its own; gitx bounds every git call.
	ctx := context.Background()
	changed, err := gitx.Changed(ctx, root)
	if err != nil {
		printErr(stdout, auditGitError(err))
		return 1
	}
	commits, err := gitx.PlanCommitFiles(ctx, root, rel)
	if err != nil {
		printErr(stdout, auditGitError(err))
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
	for i, f := range findings {
		if i == maxAuditLines {
			fmt.Fprintf(stdout, "… and %d more\n", len(findings)-maxAuditLines)
			break
		}
		fmt.Fprintln(stdout, textx.Line(f.String(), maxLine))
	}
	if len(findings) > 0 {
		fmt.Fprintf(stdout, "audit failed: %d file(s) outside %s\n", len(findings), wave)
		return 1
	}
	if n := rules.CommitsOfWaves(p, waves, cf); n > 0 {
		fmt.Fprintf(stdout, "audit ok: %s, %d changed file(s), %d commit(s)\n", wave, len(changed), n)
		return 0
	}
	fmt.Fprintf(stdout, "audit ok: %s, %d changed file(s)\n", wave, len(changed))
	return 0
}

// loadValidPlan reads the plan file name and checks it; a plan with errors
// (E0–E7) is an error. md is the file content; rel is the plan path from the root with "/".
func loadValidPlan(name, cwd string, env hooks.Env) (*plan.Plan, string, []byte, error) {
	md, rel, err := readPlanFile(name, cwd, env.Root)
	if err != nil {
		return nil, "", nil, err
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
		return nil, "", nil, fmt.Errorf("%s is not a valid plan (%d error(s), first: %s); run paraplane check %s",
			textx.Line(name, maxName), n, first, textx.Line(name, maxName))
	}
	return r.Plan, rel, md, nil
}

// auditGitError adds the way out to the error of a git output over the
// limit: the changed files of a huge untracked tree.
func auditGitError(err error) error {
	if strings.Contains(err.Error(), "git output too large") {
		return fmt.Errorf("%w; add generated or vendored files to .gitignore", err)
	}
	return err
}
