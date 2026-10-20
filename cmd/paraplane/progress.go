package main

import (
	"fmt"
	"io"
	"path"

	"paraplane/internal/config"
	"paraplane/internal/gitx"
	"paraplane/internal/hooks"
	"paraplane/internal/progress"
)

func runProgress(args []string, stdout, stderr io.Writer, cwd string) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	root := config.FindRoot(cwd)
	// Config problems are not printed: this output is shown to the user as is.
	cfg, cfgErr := config.Load(root)
	env := hooks.Env{Root: root, Config: cfg, ConfigErr: cfgErr}
	p, rel, err := loadValidPlan(args[0], cwd, env)
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	done, err := gitx.WaveCommits(root, rel)
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	out := progress.Render(p, path.Base(rel), done)
	fmt.Fprint(stdout, out)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		fmt.Fprintln(stdout)
	}
	return 0
}
