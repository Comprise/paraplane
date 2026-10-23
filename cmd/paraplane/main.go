// Command paraplane checks wave plans, audits and shows their execution, and
// serves the Claude Code and Codex hooks.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	"paraplane/internal/config"
	"paraplane/internal/hooks"
	"paraplane/internal/textx"
)

// version is set by -ldflags -X; empty means a build without it (go install).
var version string

// buildVersion is the version this binary reports.
func buildVersion() string {
	bi, _ := debug.ReadBuildInfo()
	return versionFrom(version, bi)
}

// versionFrom prefers the ldflags version, then the module version of the
// build info, else "dev".
func versionFrom(ldflags string, bi *debug.BuildInfo) string {
	switch {
	case ldflags != "":
		return ldflags
	case bi != nil && bi.Main.Version != "" && bi.Main.Version != "(devel)":
		return bi.Main.Version
	}
	return "dev"
}

// revisionFrom is the VCS revision the binary was built from, marked when the
// tree was modified; "unknown" without VCS stamping.
func revisionFrom(bi *debug.BuildInfo) string {
	if bi == nil {
		return "unknown"
	}
	rev, modified := "", false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	switch {
	case rev == "":
		return "unknown"
	case modified:
		return rev + " (modified)"
	}
	return rev
}

// hookBudget is the time one hook process has for everything; the host
// kills it at 10 s (hooks/hooks.json).
const hookBudget = 7 * time.Second

const usage = `usage: paraplane <command>

commands:
  check [--suggest] <file>         check a plan; - reads stdin
  audit <file> <wave>[,<wave>…]    audit the files changed by a wave (or a stage of waves)
  progress [--done <ids>] <file>   show the execution progress of a plan;
                                   --done: closed, uncommitted task ids, comma-separated
  progress --forget <ids> <file>   forget the recorded returns of those tasks
  hook <session-start|pre-tool-use|post-tool-use|subagent-stop|stop> [--agent codex]
  hook <stop|prompt-submit|permission-request> --agent codex
  version [-v]
`

func main() {
	os.Exit(start(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getwd))
}

func start(args []string, stdin io.Reader, stdout, stderr io.Writer, getwd func() (string, error)) int {
	cwd, err := getwd()
	if err != nil {
		if len(args) > 0 && args[0] == "hook" {
			// Hooks always exit 0; the project dir, if given, still gives a root.
			return run(args, stdin, stdout, stderr, os.Getenv("CLAUDE_PROJECT_DIR"))
		}
		printErr(stdout, err)
		return 1
	}
	return run(args, stdin, stdout, stderr, cwd)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, cwd string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	rest := args[1:]
	switch args[0] {
	case "hook":
		// The flag comes after the event: the plugin shim reads the event from $2.
		agent := hooks.Claude
		switch {
		case len(rest) == 1:
		case len(rest) == 3 && rest[1] == "--agent":
			a, ok := hooks.ParseAgent(rest[2])
			if !ok {
				fmt.Fprintln(stderr, "paraplane: hook: bad arguments")
				return 0
			}
			agent = a
		default:
			fmt.Fprintln(stderr, "paraplane: hook: bad arguments")
			return 0
		}
		return runHook(rest[0], agent, stdin, stdout, stderr, cwd)
	case "check":
		return runCheck(rest, stdin, stdout, stderr, cwd)
	case "audit":
		return runAudit(rest, stdout, stderr, cwd)
	case "progress":
		return runProgress(rest, stdout, stderr, cwd)
	case "version":
		switch {
		case len(rest) == 0:
			fmt.Fprintln(stdout, buildVersion())
			return 0
		case len(rest) == 1 && rest[0] == "-v":
			bi, _ := debug.ReadBuildInfo()
			fmt.Fprintf(stdout, "paraplane %s\ngo %s\nrevision %s\n", versionFrom(version, bi), runtime.Version(), revisionFrom(bi))
			return 0
		}
	}
	fmt.Fprint(stderr, usage)
	return 2
}

// hookHandlers maps `paraplane hook <event>` to its handler. prompt-submit
// and permission-request serve only Codex (hooks/codex.json) and answer
// nothing for Claude; stop checks the plan for Codex and shows pending
// progress for Claude; subagent-stop serves only Claude.
var hookHandlers = map[string]func(hooks.Env, hooks.Input) ([]byte, error){
	"session-start":      hooks.SessionStart,
	"pre-tool-use":       hooks.PreToolUse,
	"post-tool-use":      hooks.PostToolUse,
	"stop":               hooks.Stop,
	"subagent-stop":      hooks.SubagentStop,
	"prompt-submit":      hooks.PromptSubmit,
	"permission-request": hooks.PermissionRequest,
}

// maxHookInput caps the hook payload read from stdin (spec section 8).
const maxHookInput = 4 << 20

// hookRoot picks the project root for a hook: the project dir Claude Code
// passes (stable for the whole session), else the payload's cwd, else the
// process cwd; then the nearest directory up with .git or the config file.
func hookRoot(in hooks.Input, cwd string) string {
	dir := cwd
	if d := os.Getenv("CLAUDE_PROJECT_DIR"); d != "" {
		dir = d
	} else if in.Cwd != "" {
		dir = in.Cwd
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return config.FindRoot(dir)
}

// hookEventNames maps `paraplane hook <event>` to the event name the host
// expects in hookSpecificOutput.hookEventName.
var hookEventNames = map[string]string{
	"session-start":      "SessionStart",
	"pre-tool-use":       "PreToolUse",
	"post-tool-use":      "PostToolUse",
	"stop":               "Stop",
	"subagent-stop":      "SubagentStop",
	"prompt-submit":      "UserPromptSubmit",
	"permission-request": "PermissionRequest",
}

// answerEvent returns the hookEventName of an answer, empty when it has none.
func answerEvent(out []byte) string {
	var a struct {
		Hook *struct {
			Name string `json:"hookEventName"`
		} `json:"hookSpecificOutput"`
	}
	if json.Unmarshal(out, &a) != nil || a.Hook == nil {
		return ""
	}
	return a.Hook.Name
}

// runHook never fails: errors and panics go to stderr, the answer stays
// empty and the exit code is 0. The whole process has hookBudget; an answer
// that names another event than the one asked is dropped. With
// PARAPLANE_DEBUG=1 one line with the elapsed time (and the error) goes to
// stderr.
func runHook(event string, agent hooks.Agent, stdin io.Reader, stdout, stderr io.Writer, cwd string) int {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), hookBudget)
	defer cancel()
	var out []byte
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
			}
		}()
		h, ok := hookHandlers[event]
		if !ok {
			return errors.New("unknown event")
		}
		data, err := io.ReadAll(io.LimitReader(stdin, maxHookInput+1))
		if err != nil {
			return fmt.Errorf("bad input: %w", err)
		}
		if len(data) > maxHookInput {
			return fmt.Errorf("bad input: larger than %d bytes", maxHookInput)
		}
		var in hooks.Input
		if err := json.Unmarshal(data, &in); err != nil {
			return fmt.Errorf("bad input: %w", err)
		}
		root := hookRoot(in, cwd)
		cfg, cfgErr := config.Load(root)
		out, err = h(hooks.Env{Root: root, Config: cfg, ConfigErr: cfgErr, Agent: agent, Ctx: ctx}, in)
		if err != nil {
			return err
		}
		if got, want := answerEvent(out), hookEventNames[event]; got != "" && got != want {
			out = nil
			return fmt.Errorf("answer names event %s, not %s: dropped", textx.Line(got, 64), want)
		}
		return nil
	}()
	if os.Getenv("PARAPLANE_DEBUG") == "1" {
		line := fmt.Sprintf("paraplane: debug: hook %s %dms", textx.Line(event, 64), time.Since(start).Milliseconds())
		if err != nil {
			line += " error: " + textx.Line(err.Error(), 200)
		}
		fmt.Fprintln(stderr, line)
	}
	if err != nil {
		fmt.Fprintf(stderr, "paraplane: hook %s: %s\n", textx.Line(event, 64), textx.Block(err.Error(), 64<<10))
		return 0
	}
	if len(out) > 0 {
		if _, err := stdout.Write(append(out, '\n')); err != nil {
			fmt.Fprintf(stderr, "paraplane: hook %s: writing the answer: %v\n", textx.Line(event, 64), err)
		}
	}
	return 0
}
