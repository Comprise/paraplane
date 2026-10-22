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
	"strconv"
	"strings"
	"time"
)

const timeout = 10 * time.Second

// maxOutput caps the stdout run captures; a variable for the tests.
var maxOutput = 16 << 20

// filterKeys matches the filter driver commands that git status runs.
const filterKeys = `^filter\..*\.(clean|smudge|process)$`

// partialCloneKeys matches the config that makes a repository a partial
// clone, one that fetches missing objects from a promisor remote on demand.
// On git without GIT_NO_LAZY_FETCH it counts from any scope, global and
// command included: such a fetch can run any repo-set transport program
// (remote.<name>.uploadpack, core.sshCommand, a remote URL helper), so the
// trust of the scope that made the clone partial does not matter. git config
// matches the regexp against the lowercased key; a subsection keeps its case
// and is matched by ".*".
const partialCloneKeys = `^(extensions\.partialclone|remote\..*\.promisor)$`

// uploadPackKeys matches the upload-pack program a fetch runs; on git
// without GIT_NO_LAZY_FETCH a repo-set one refuses as well.
const uploadPackKeys = `^remote\..*\.uploadpack$`

// trustedScopes are the --show-scope scopes whose filter drivers the user
// configured: the global (user) config and the command line or environment.
// The system config is never read; every other scope, known or not, refuses.
var trustedScopes = []string{"global", "command"}

var waveID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,31}$`)

// repoVars point git at another repository than the one around dir.
var repoVars = []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_OBJECT_DIRECTORY=",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES=", "GIT_COMMON_DIR=", "GIT_NAMESPACE=", "GIT_PREFIX=",
	"GIT_CONFIG="} // exact: GIT_CONFIG_GLOBAL, GIT_CONFIG_COUNT etc. are not matched

// statusArgs is the git status command of Changed.
var statusArgs = []string{"status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=all", "--no-renames"}

// ErrFilterDrivers is returned by Changed when the repository's local config
// defines filter drivers, which git status would execute.
var ErrFilterDrivers = errors.New("the repository config defines filter drivers; paraplane audit does not run git status here")

// ErrPartialClone is returned by Changed when git does not honor
// GIT_NO_LAZY_FETCH and any config (of any scope) makes the repository a
// partial clone, or the repository's own config sets a remote's uploadpack
// program, so git status could fetch missing objects through
// repo-configured transports that run programs.
var ErrPartialClone = errors.New("the repository is a partial clone (extensions.partialClone or remote.<name>.promisor in any config) or its config sets remote.<name>.uploadpack, and this git does not support GIT_NO_LAZY_FETCH; upgrade git to 2.46 or later (or 2.39.4, 2.40.2, 2.41.1, 2.42.2, 2.43.4, 2.44.1, 2.45.1); paraplane audit does not run git status here")

// gitVersion is a parsed "git version" triple.
type gitVersion [3]int

var versionRe = regexp.MustCompile(`^git version (\d+)\.(\d+)(?:\.(\d+))?`)

// parseVersion parses the output of git version: "git version X.Y.Z"
// followed by anything (vendor suffixes such as " (Apple Git-154)",
// ".windows.1" or ".rc0"). A missing Z counts as 0.
func parseVersion(s string) (gitVersion, bool) {
	m := versionRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return gitVersion{}, false
	}
	var v gitVersion
	for i, g := range m[1:] {
		if g == "" {
			continue
		}
		n, err := strconv.Atoi(g)
		if err != nil {
			return gitVersion{}, false
		}
		v[i] = n
	}
	return v, true
}

// atLeast reports whether v is major.minor.patch or later.
func (v gitVersion) atLeast(major, minor, patch int) bool {
	return slices.Compare(v[:], []int{major, minor, patch}) >= 0
}

// version returns the version of the git that run uses; ok is false when its
// output cannot be parsed.
func version(root string) (v gitVersion, ok bool, err error) {
	out, err := run(root, "version")
	if err != nil {
		return gitVersion{}, false, err
	}
	v, ok = parseVersion(string(out))
	return v, ok, nil
}

// lazyFetchSafe reports whether git v honors GIT_NO_LAZY_FETCH=1. The
// variable came with 2.45.0 and was backported to the maintenance releases of
// May 2024; to stay conservative 2.45.0 itself is not trusted.
func lazyFetchSafe(v gitVersion, ok bool) bool {
	if !ok {
		return false
	}
	if v.atLeast(2, 46, 0) {
		return true
	}
	// The first fixed patch release of each maintenance line.
	fixed := map[int]int{45: 1, 44: 1, 43: 4, 42: 2, 41: 1, 40: 2, 39: 4}
	p, found := fixed[v[1]]
	return v[0] == 2 && found && v[2] >= p
}

// run runs git in root with fsmonitor, the pager, hooks and every signature
// verification program disabled and returns stdout; on failure the error
// carries stderr. It does not neutralize filter drivers or lazy fetches on
// git without GIT_NO_LAZY_FETCH: callers that run commands which can execute
// them must guard against that themselves, see Changed.
func run(root string, args ...string) ([]byte, error) {
	out, _, err := runCode(root, args...)
	return out, err
}

// capWriter keeps at most limit bytes; past that it records the overflow,
// stops git and fails the write.
type capWriter struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
	stop     func()
}

func (w *capWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.limit {
		w.overflow = true
		w.stop()
		return 0, errors.New("git output too large")
	}
	return w.buf.Write(p)
}

// runCode is run that also returns the exit code of git (-1 when git did not
// exit normally or did not start) and, when git exits with an error code, its
// stdout.
func runCode(root string, args ...string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// Repo-local config can name programs that git runs on its own: signature
	// verifiers (log.showSignature makes git log run gpg.program) and hooks.
	// Command-line config overrides every config file. core.pager=cat and
	// --no-pager have no test: git starts a pager only when stdout is a
	// terminal, and run always captures stdout through a pipe, so they are
	// defense in depth that no test can observe.
	full := append([]string{"-C", root,
		"-c", "core.fsmonitor=false", "-c", "core.pager=cat",
		"-c", "log.showSignature=false",
		"-c", "gpg.program=false", "-c", "gpg.ssh.program=false", "-c", "gpg.x509.program=false",
		"-c", "core.hooksPath=" + os.DevNull,
		"--no-pager"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	for _, kv := range os.Environ() {
		if !slices.ContainsFunc(repoVars, func(p string) bool { return strings.HasPrefix(kv, p) }) {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	// GIT_OPTIONAL_LOCKS=0 stops status from refreshing (writing) the index and
	// so from running index hooks such as post-index-change; GIT_NO_LAZY_FETCH=1
	// stops a partial clone from fetching missing objects through repo-configured
	// transports (uploadpack, sshCommand, credential helpers) that run programs;
	// GIT_CONFIG_NOSYSTEM=1 means the system config is never read, so the only
	// config files trusted with filter drivers are the user's global ones.
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C", "LANGUAGE=C")
	out := &capWriter{limit: maxOutput, stop: cancel}
	var errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = out, &errb
	err := cmd.Run()
	if out.overflow {
		return nil, -1, fmt.Errorf("git %s: git output too large", args[0])
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, -1, fmt.Errorf("git %s: timed out", args[0])
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// ExitCode is -1 when git was killed by a signal.
			return out.buf.Bytes(), ee.ExitCode(), fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(errb.String()))
		}
		return nil, -1, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out.buf.Bytes(), 0, nil
}

// LostTrailer is a commit whose message has Paraplane-Plan and
// Paraplane-Wave lines that git does not read as trailers: they are not in
// the last paragraph (a blank line before Co-Authored-By, say).
type LostTrailer struct {
	Commit string // abbreviated hash
	Wave   string
}

// WaveCommits returns the ids of waves committed with the Paraplane-Plan
// trailer equal to planPath, in order of first appearance, without duplicates.
// When a commit repeats a trailer, its last occurrence wins. lost lists, once
// per wave and only for waves missing from waves, the commits whose
// Paraplane-* lines for planPath git does not read as trailers. Only commits
// with a line starting with Paraplane-Wave: are read. It needs git 2.22 or
// later: older git has no %(trailers:key=…) and would print no trailers at
// all, so it is an error.
func WaveCommits(root, planPath string) (waves []string, lost []LostTrailer, err error) {
	v, ok, err := version(root)
	if err != nil {
		return nil, nil, err
	}
	if !ok || !v.atLeast(2, 22, 0) {
		return nil, nil, errors.New("git 2.22 or later is required for progress")
	}
	out, err := run(root, "log", "--no-show-signature", "--reverse", "-i", "--grep=^paraplane-wave:",
		"--format=%h%x1f%(trailers:key=Paraplane-Plan,key=Paraplane-Wave,unfold)%x1f%B%x1e")
	if err != nil {
		if _, e := run(root, "rev-parse", "--git-dir"); e == nil {
			if _, e := run(root, "rev-parse", "--verify", "-q", "HEAD"); e != nil {
				return []string{}, nil, nil // no commits yet
			}
		}
		return nil, nil, err
	}
	waves = []string{}
	var lostAll []LostTrailer
	for _, rec := range strings.Split(string(out), "\x1e") {
		hash, rest, _ := strings.Cut(strings.TrimLeft(rec, "\n"), "\x1f")
		trailers, body, _ := strings.Cut(rest, "\x1f")
		if wave, ok := paraplaneLines(trailers, planPath); ok {
			if !slices.Contains(waves, wave) {
				waves = append(waves, wave)
			}
		} else if wave, ok := paraplaneLines(body, planPath); ok {
			lostAll = append(lostAll, LostTrailer{Commit: hash, Wave: wave})
		}
	}
	for _, l := range lostAll {
		if !slices.Contains(waves, l.Wave) && !slices.ContainsFunc(lost, func(x LostTrailer) bool { return x.Wave == l.Wave }) {
			lost = append(lost, l)
		}
	}
	return waves, lost, nil
}

// paraplaneLines reads Paraplane-Plan and Paraplane-Wave lines (key case
// ignored, the last of each wins) and reports the wave when the plan is
// planPath and the wave is a valid id.
func paraplaneLines(text, planPath string) (string, bool) {
	var plan, wave string
	var hasPlan bool
	for _, line := range strings.Split(text, "\n") {
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
	return wave, hasPlan && plan == planPath && waveID.MatchString(wave)
}

// Changed returns the files changed in the working tree relative to root
// (slash-separated, sorted, without duplicates); paths outside root are
// dropped. Status runs with --no-renames, so a rename is reported as a
// deletion and an addition and both paths are included.
//
// Changed refuses (ErrFilterDrivers) when the repository's config defines
// filter drivers, and (ErrPartialClone) when git does not honor
// GIT_NO_LAZY_FETCH (see lazyFetchSafe) and any config makes the repository
// a partial clone or the repository's config sets remote.<name>.uploadpack. The
// git status output is capped at 16 MiB like every git output here, so a
// huge untracked tree makes Changed fail with "git output too large".
func Changed(root string) ([]string, error) {
	pre, err := run(root, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimSpace(string(pre))
	v, ok, err := version(root)
	if err != nil {
		return nil, err
	}
	if set, err := repoConfigSets(root, filterKeys); err != nil {
		return nil, err
	} else if set {
		return nil, ErrFilterDrivers
	}
	if !lazyFetchSafe(v, ok) {
		// Partial clone markers count from every scope, so no --show-scope.
		out, code, err := runCode(root, "config", "--includes", "--get-regexp", partialCloneKeys)
		switch {
		case err == nil:
			return nil, ErrPartialClone
		case !noMatches(out, code, err):
			return nil, err
		}
		if set, err := repoConfigSets(root, uploadPackKeys); err != nil {
			return nil, err
		} else if set {
			return nil, ErrPartialClone
		}
	}
	out, err := run(root, statusArgs...)
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
	// With --no-renames every record is "XY path"; no record carries a second path.
	for _, f := range strings.Split(string(out), "\x00") {
		if len(f) >= 4 {
			add(f[3:])
		}
	}
	res := make([]string, 0, len(seen))
	for p := range seen {
		res = append(res, p)
	}
	slices.Sort(res)
	return res, nil
}

// noMatches reports whether a git config query found nothing: exit code 1
// without output. Any other failure is an error.
func noMatches(out []byte, code int, err error) bool {
	return err != nil && code == 1 && len(bytes.TrimSpace(out)) == 0
}

// repoConfigSets reports whether the repository's own config sets a key
// matching the regexp keys: the local or worktree scope, including every
// file included from them (include and includeIf; included files inherit the
// including file's scope). Keys from the user's global config and from the
// command line are trusted. Any unexpected failure is an error, so the
// caller refuses.
func repoConfigSets(root, keys string) (bool, error) {
	// One query over all scopes, following includes (git status does too).
	out, code, err := runCode(root, "config", "--show-scope", "--includes", "--get-regexp", keys)
	switch {
	case err == nil:
		for _, line := range strings.Split(string(out), "\n") {
			if f := strings.Fields(line); len(f) > 0 && !slices.Contains(trustedScopes, f[0]) {
				return true, nil
			}
		}
		return false, nil
	case noMatches(out, code, err):
		return false, nil
	case code != 129:
		return false, err
	}
	// Exit code 129 is a usage error: git older than 2.26 has no
	// --show-scope. Query the untrusted scopes one by one instead, following
	// includes the same way; any match counts.
	out, code, err = runCode(root, "config", "--local", "--includes", "--get-regexp", keys)
	if err == nil {
		return true, nil
	}
	if !noMatches(out, code, err) {
		return false, err
	}
	// git reads the worktree config only with extensions.worktreeConfig;
	// without it --worktree reads the local config again or, in a repository
	// with linked worktrees, fails (exit 128). So --worktree runs only when
	// the extension may be on, see worktreeConfigMaybeOn.
	if on, err := worktreeConfigMaybeOn(root); err != nil || !on {
		return false, err
	}
	// Git older than 2.20 does not know --worktree, nor the extension, so
	// there is no worktree config to read: only that usage error is ignored.
	out, code, err = runCode(root, "config", "--worktree", "--includes", "--get-regexp", keys)
	switch {
	case err == nil:
		return true, nil
	case noMatches(out, code, err):
		return false, nil
	case code == 129 && len(bytes.TrimSpace(out)) == 0 && strings.Contains(err.Error(), "unknown option `worktree'"):
		return false, nil
	}
	return false, err
}

// worktreeConfigMaybeOn reports whether extensions.worktreeConfig may be on
// for git. git's repository format check reads the extension from the local
// config file itself, without includes, and the last value wins there; the
// config command sees includes too. To stay on the safe side every value is
// read, both without and with includes, and any true one counts: only when
// all are unset or false is the extension off. A value that is not a bool
// makes git config fail, which is an error, so the caller refuses.
func worktreeConfigMaybeOn(root string) (bool, error) {
	for _, inc := range [][]string{{"--no-includes"}, {"--includes"}} {
		args := append(append([]string{"config", "--local"}, inc...), "--bool", "--get-all", "extensions.worktreeConfig")
		out, code, err := runCode(root, args...)
		switch {
		case noMatches(out, code, err):
			continue
		case err != nil:
			return false, err
		}
		for _, v := range strings.Fields(string(out)) {
			if v != "false" {
				return true, nil
			}
		}
	}
	return false, nil
}
