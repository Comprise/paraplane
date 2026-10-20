// Command paraplane checks wave plans, audits and shows their execution, and
// serves the Claude Code and Codex hooks.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	"paraplane/internal/config"
	"paraplane/internal/hooks"
	"paraplane/internal/textx"
)

var version = "dev"

const usage = `usage: paraplane <command>

commands:
  check [--suggest] <file>   check a plan; - reads stdin
  audit <file> <wave>        audit the files changed by a wave
  progress <file>            show the execution progress of a plan
  hook <session-start|pre-tool-use|post-tool-use> [--agent codex]
  hook <stop|prompt-submit|permission-request> --agent codex
  version
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
		if len(rest) == 0 {
			fmt.Fprintln(stdout, version)
			return 0
		}
	}
	fmt.Fprint(stderr, usage)
	return 2
}

// hookHandlers maps `paraplane hook <event>` to its handler. stop,
// prompt-submit and permission-request serve only Codex (hooks/codex.json)
// and answer nothing for Claude.
var hookHandlers = map[string]func(hooks.Env, hooks.Input) ([]byte, error){
	"session-start":      hooks.SessionStart,
	"pre-tool-use":       hooks.PreToolUse,
	"post-tool-use":      hooks.PostToolUse,
	"stop":               hooks.Stop,
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

// runHook never fails: errors and panics go to stderr, the answer stays
// empty and the exit code is 0.
func runHook(event string, agent hooks.Agent, stdin io.Reader, stdout, stderr io.Writer, cwd string) int {
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
		out, err = h(hooks.Env{Root: root, Config: cfg, ConfigErr: cfgErr, Agent: agent}, in)
		return err
	}()
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
