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
	"GIT_ALTERNATE_OBJECT_DIRECTORIES=", "GIT_COMMON_DIR=", "GIT_NAMESPACE=", "GIT_PREFIX="}

// run runs git in root with repo-controlled configuration disabled and
// returns stdout; on failure the error carries stderr.
func run(root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	full := append([]string{"-C", root, "-c", "core.fsmonitor=false", "-c", "core.pager=cat", "--no-pager"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	for _, kv := range os.Environ() {
		if !slices.ContainsFunc(repoVars, func(p string) bool { return strings.HasPrefix(kv, p) }) {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C", "LANGUAGE=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("git %s: timed out", args[0])
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(errb.String()))
		}
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out.Bytes(), nil
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
	out, err := run(root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
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
