// Package gitx reads the working tree state and wave commits from git.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const timeout = 10 * time.Second

var waveID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,31}$`)

// repoVars point git at another repository than the one around dir.
var repoVars = []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_OBJECT_DIRECTORY=",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES=", "GIT_COMMON_DIR=", "GIT_NAMESPACE=", "GIT_PREFIX=",
	"GIT_CONFIG="} // exact: GIT_CONFIG_GLOBAL, GIT_CONFIG_COUNT etc. are not matched

// ErrFilterDrivers is returned by Changed when the repository's local config
// defines filter drivers, which git status would execute.
var ErrFilterDrivers = errors.New("the repository config defines filter drivers; paraplane audit does not run git status here")

// run runs git in root with fsmonitor and the pager disabled and returns
// stdout; on failure the error carries stderr. It does not neutralize other
// repo-local config: callers that run commands which can execute configured
// programs (filter drivers) must guard against that themselves, see Changed.
func run(root string, args ...string) ([]byte, error) {
	out, _, err := runCode(root, args...)
	return out, err
}

// runCode is run that also returns the exit code of git (-1 when git did not
// exit normally or did not start).
func runCode(root string, args ...string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	full := append([]string{"-C", root, "-c", "core.fsmonitor=false", "-c", "core.pager=cat", "--no-pager"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	for _, kv := range os.Environ() {
		if !slices.ContainsFunc(repoVars, func(p string) bool { return strings.HasPrefix(kv, p) }) {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	// GIT_OPTIONAL_LOCKS=0 stops status from refreshing (writing) the index and
	// so from running index hooks such as post-index-change; GIT_NO_LAZY_FETCH=1
	// stops a partial clone from fetching missing objects through repo-configured
	// transports (uploadpack, sshCommand, credential helpers) that run programs.
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C", "LANGUAGE=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, -1, fmt.Errorf("git %s: timed out", args[0])
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, ee.ExitCode(), fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(errb.String()))
		}
		return nil, -1, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out.Bytes(), 0, nil
}

// WaveCommits returns the ids of waves committed with the Paraplane-Plan
// trailer equal to planPath, in order of first appearance, without duplicates.
func WaveCommits(root, planPath string) ([]string, error) {
	out, err := run(root, "log", "--reverse", "--format=%(trailers:key=Paraplane-Plan,key=Paraplane-Wave,unfold)%x1e")
	if err != nil {
		if _, e := run(root, "rev-parse", "--git-dir"); e == nil {
			if _, e := run(root, "rev-parse", "--verify", "-q", "HEAD"); e != nil {
				return []string{}, nil // no commits yet
			}
		}
		return nil, err
	}
	waves := []string{}
	for _, rec := range strings.Split(string(out), "\x1e") {
		var plan, wave string
		var hasPlan bool
		for _, line := range strings.Split(rec, "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "paraplane-plan":
				plan, hasPlan = v, true
			case "paraplane-wave":
				wave = v
			}
		}
		if hasPlan && plan == planPath && waveID.MatchString(wave) && !slices.Contains(waves, wave) {
			waves = append(waves, wave)
		}
	}
	return waves, nil
}

// Changed returns the files changed in the working tree relative to root
// (slash-separated, sorted, without duplicates); both paths of a rename or
// copy are included, paths outside root are dropped.
func Changed(root string) ([]string, error) {
	pre, err := run(root, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimSpace(string(pre))
	// One query over all scopes, following includes (git status does too).
	// Exit code 1 with no output means no matches. Filters from the user's own
	// global/system/command config are trusted; local and worktree scopes
	// (included files inherit the including file's scope) are refused.
	filters, code, err := runCode(root, "config", "--show-scope", "--includes", "--get-regexp", `^filter\..*\.(clean|smudge|process)$`)
	if err != nil && !(code == 1 && len(bytes.TrimSpace(filters)) == 0) {
		return nil, err
	}
	for _, line := range strings.Split(string(filters), "\n") {
		if f := strings.Fields(line); len(f) > 0 && (f[0] == "local" || f[0] == "worktree") {
			return nil, ErrFilterDrivers
		}
	}
	out, err := run(root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=all", "--no-renames")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	add := func(p string) {
		p = filepath.ToSlash(p)
		if rest, ok := strings.CutPrefix(p, prefix); ok && rest != "" {
			seen[rest] = true
		}
	}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		add(f[3:])
		if f[0] == 'R' || f[0] == 'C' || f[1] == 'R' || f[1] == 'C' {
			i++
			if i < len(fields) {
				add(fields[i])
			}
		}
	}
	res := make([]string, 0, len(seen))
	for p := range seen {
		res = append(res, p)
	}
	slices.Sort(res)
	return res, nil
}
