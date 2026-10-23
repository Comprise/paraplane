package main

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"paraplane/internal/config"
	"paraplane/internal/hooks"
	"paraplane/internal/track"
)

// doneIDs matches a --done value: task ids (the plan id rule) joined by ",".
var doneIDs = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,31}(,[A-Za-z][A-Za-z0-9_.-]{0,31})*$`)

func runProgress(args []string, stdout, stderr io.Writer, cwd string) int {
	var closed []string
	if len(args) == 3 && args[0] == "--done" && doneIDs.MatchString(args[1]) {
		closed = strings.Split(args[1], ",")
		args = args[2:]
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "--") {
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
	st, err := track.PlanStatus(root, rel, p, closed)
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	fmt.Fprint(stdout, st.Text)
	return 0
}
