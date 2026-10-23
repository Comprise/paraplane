package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"paraplane/internal/config"
	"paraplane/internal/gitx"
	"paraplane/internal/hooks"
	"paraplane/internal/ids"
	"paraplane/internal/plan"
	"paraplane/internal/state"
	"paraplane/internal/textx"
	"paraplane/internal/track"
)

func runProgress(args []string, stdout, stderr io.Writer, cwd string) int {
	var closed, forget []string
	if len(args) == 3 && (args[0] == "--done" || args[0] == "--forget") {
		list, ok := ids.ParseList(args[1])
		if ok && args[0] == "--done" {
			closed = list
		} else if ok {
			forget = list
		}
		if ok {
			args = args[2:]
		}
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "--") {
		fmt.Fprint(stderr, usage)
		return 2
	}
	root := config.FindRoot(cwd)
	// Config problems are not printed: this output is shown to the user as is.
	cfg, cfgErr := config.Load(root)
	env := hooks.Env{Root: root, Config: cfg, ConfigErr: cfgErr}
	p, rel, md, err := loadValidPlan(args[0], cwd, env)
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	ctx := context.Background()
	if forget != nil {
		for _, id := range forget {
			if !slices.ContainsFunc(p.Waves, func(w plan.Wave) bool {
				return slices.ContainsFunc(w.Tasks, func(t plan.Task) bool { return t.ID == id })
			}) {
				printErr(stdout, fmt.Errorf("task %s is not in %s", id, rel))
				return 2
			}
		}
		return forgetTasks(ctx, stdout, root, rel, forget)
	}
	st, err := track.PlanStatusContext(ctx, root, rel, p, track.PlanHash(md), closed)
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	fmt.Fprint(stdout, st.Text)
	return 0
}

// forgetTasks removes the recorded returns of tasks of plan rel and the
// plan's shown counts (they only grow, so the progress hook would stay
// silent otherwise). A state with nothing to remove is not written.
func forgetTasks(ctx context.Context, stdout io.Writer, root, rel string, tasks []string) int {
	gitDir, err := gitx.GitDirContext(ctx, root)
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	st, err := state.LoadState(gitDir)
	if err != nil && !errors.Is(err, state.ErrVersion) {
		printErr(stdout, err)
		return 1
	}
	_, shown := st.Shown[rel]
	if err == nil && !shown && !slices.ContainsFunc(st.Entries, func(e state.Entry) bool {
		return e.Plan == rel && slices.Contains(tasks, e.Task)
	}) {
		fmt.Fprintf(stdout, "nothing recorded for %s in %s\n", strings.Join(tasks, ","), textx.Line(rel, maxName))
		return 0
	}
	err = state.UpdateCtx(ctx, gitDir, func(s *state.State) {
		s.Forget(rel, tasks)
		delete(s.Shown, rel)
	})
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	fmt.Fprintf(stdout, "forgot %s in %s\n", strings.Join(tasks, ","), textx.Line(rel, maxName))
	return 0
}
