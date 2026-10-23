// Package gitx runs git so that a repository's own config cannot make it run
// programs, within the time budget of the caller's context. It reads the
// changed files of the working tree, the paraplane trailers of commits, the
// head, the git directory and ancestry.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// timeout is the limit of one git call; a caller's context with less time
// left shortens it.
const timeout = 10 * time.Second

// waitDelay bounds the wait for git's stdout and stderr after git exits or is
// killed, when a child of git still holds them open.
const waitDelay = time.Second

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

// repoVars point git at another repository than the one around dir.
var repoVars = []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_OBJECT_DIRECTORY=",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES=", "GIT_COMMON_DIR=", "GIT_NAMESPACE=", "GIT_PREFIX=",
	"GIT_CONFIG="} // exact: GIT_CONFIG_GLOBAL, GIT_CONFIG_COUNT etc. are not matched

// statusArgs is the git status command of Changed.
var statusArgs = []string{"status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=all", "--no-renames"}

// fullHash is a full SHA-1 or SHA-256 object name, as git prints it.
var fullHash = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// validHash reports whether s is a full object name. Hashes read from files
// (the state file) must pass it before they reach git's argv or stdin.
func validHash(s string) bool { return fullHash.MatchString(s) }

// checkHashes returns an error for the first argument that is not a full
// object name.
func checkHashes(hs ...string) error {
	for _, h := range hs {
		if !validHash(h) {
			return fmt.Errorf("invalid commit hash %q", h)
		}
	}
	return nil
}

// ErrFilterDrivers is returned by Changed when the repository's local config
// defines filter drivers, which git status would execute.
var ErrFilterDrivers = errors.New("the repository config defines filter drivers; paraplane audit does not run git status here")

// ErrOldGit is wrapped by every function that reads commits: they need git
// 2.24 or later, for --end-of-options before revisions (and 2.22 for
// %(trailers:key=…); older git prints no trailers at all). Changed keeps no
// such floor.
var ErrOldGit = errors.New("git 2.24 or later is required")

// minGit is the git version ErrOldGit names.
var minGit = gitVersion{2, 24, 0}

// gitVersion is a parsed "git version" triple.
type gitVersion [3]int

func (v gitVersion) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// lazyFetchFirst is the first release line that honors GIT_NO_LAZY_FETCH;
// lazyFetchFixed are the first fixed patch releases of the older maintenance
// lines (May 2024). 2.45.0 itself is not trusted, to stay conservative. This
// list is the only source: lazyFetchSafe and ErrPartialClone both use it.
var (
	lazyFetchFirst = gitVersion{2, 46, 0}
	lazyFetchFixed = []gitVersion{{2, 39, 4}, {2, 40, 2}, {2, 41, 1}, {2, 42, 2}, {2, 43, 4}, {2, 44, 1}, {2, 45, 1}}
)

// ErrPartialClone is returned when git does not honor GIT_NO_LAZY_FETCH and
// any config (of any scope) makes the repository a partial clone, or the
// repository's own config sets a remote's uploadpack program: a git command
// that needs a missing object could then fetch it through repo-configured
// transports that run programs. Every function that may read objects
// refuses so.
var ErrPartialClone = errors.New("the repository is a partial clone (extensions.partialClone or remote.<name>.promisor in any config) or its config sets remote.<name>.uploadpack, and this git does not support GIT_NO_LAZY_FETCH; upgrade git to " +
	strings.TrimSuffix(lazyFetchFirst.String(), ".0") + " or later (or " + fixedList() + "); paraplane does not run git here")

func fixedList() string {
	s := make([]string, len(lazyFetchFixed))
	for i, v := range lazyFetchFixed {
		s[i] = v.String()
	}
	return strings.Join(s, ", ")
}

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

// versions caches the parsed git version per git executable for the life of
// the process: the version depends on the binary, not on the repository.
// The key is the path exec resolves "git" to, so a test that puts another
// git first on PATH gets its own entry.
var versions struct {
	sync.Mutex
	m map[string]versionResult
}

type versionResult struct {
	v  gitVersion
	ok bool
}

// version returns the version of the git that run uses; ok is false when its
// output cannot be parsed. A failed git version is not cached.
func version(ctx context.Context, root string) (v gitVersion, ok bool, err error) {
	key, lerr := exec.LookPath("git")
	versions.Lock()
	defer versions.Unlock()
	if r, found := versions.m[key]; found && lerr == nil {
		return r.v, r.ok, nil
	}
	out, err := run(ctx, root, "version")
	if err != nil {
		return gitVersion{}, false, err
	}
	v, ok = parseVersion(string(out))
	if lerr == nil {
		if versions.m == nil {
			versions.m = map[string]versionResult{}
		}
		versions.m[key] = versionResult{v, ok}
	}
	return v, ok, nil
}

// requireGit returns ErrOldGit when git is older than major.minor.patch or
// its version cannot be parsed; callers pass minGit.
func requireGit(ctx context.Context, root string, major, minor, patch int) error {
	v, ok, err := version(ctx, root)
	if err != nil {
		return err
	}
	if !ok || !v.atLeast(major, minor, patch) {
		return ErrOldGit
	}
	return nil
}

// lazyFetchSafe reports whether git v honors GIT_NO_LAZY_FETCH=1.
func lazyFetchSafe(v gitVersion, ok bool) bool {
	if !ok {
		return false
	}
	if v.atLeast(lazyFetchFirst[0], lazyFetchFirst[1], lazyFetchFirst[2]) {
		return true
	}
	return slices.ContainsFunc(lazyFetchFixed, func(f gitVersion) bool {
		return v[0] == f[0] && v[1] == f[1] && v[2] >= f[2]
	})
}

// guardLazyFetch returns ErrPartialClone when git may ignore
// GIT_NO_LAZY_FETCH (see lazyFetchSafe) and any config makes the repository
// a partial clone, or the repository's config sets remote.<name>.uploadpack.
// Every function whose git command may need objects calls it first.
// GIT_ALLOW_PROTOCOL=none (see runIO) blocks the transports as well; this is
// the second layer.
func guardLazyFetch(ctx context.Context, root string) error {
	v, ok, err := version(ctx, root)
	if err != nil {
		return err
	}
	if lazyFetchSafe(v, ok) {
		return nil
	}
	// Partial clone markers count from every scope, so no --show-scope.
	out, code, err := runCode(ctx, root, "config", "--includes", "--get-regexp", partialCloneKeys)
	switch {
	case err == nil:
		return ErrPartialClone
	case !noMatches(out, code, err):
		return err
	}
	if set, err := repoConfigSets(ctx, root, uploadPackKeys); err != nil {
		return err
	} else if set {
		return ErrPartialClone
	}
	return nil
}

// run runs git in root with fsmonitor, the pager, hooks, every signature
// verification program and every transport disabled and returns stdout; on
// failure the error carries stderr. It does not neutralize filter drivers or
// lazy fetches on git without GIT_NO_LAZY_FETCH: callers that run commands
// which can execute them must guard against that themselves (Changed,
// guardLazyFetch).
func run(ctx context.Context, root string, args ...string) ([]byte, error) {
	out, _, err := runCode(ctx, root, args...)
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
func runCode(ctx context.Context, root string, args ...string) ([]byte, int, error) {
	return runIO(ctx, root, nil, nil, args...)
}

// runIO is runCode with stdin and, when sink is not nil, stdout streamed to
// sink instead of captured (no output cap; the returned stdout is nil).
// The call ends at min(timeout, the time left in ctx).
func runIO(ctx context.Context, root string, stdin io.Reader, sink io.Writer, args ...string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
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
	cmd.WaitDelay = waitDelay
	for _, kv := range os.Environ() {
		if !slices.ContainsFunc(repoVars, func(p string) bool { return strings.HasPrefix(kv, p) }) {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	// GIT_OPTIONAL_LOCKS=0 stops status from refreshing (writing) the index and
	// so from running index hooks such as post-index-change; GIT_NO_LAZY_FETCH=1
	// stops a partial clone from fetching missing objects through repo-configured
	// transports (uploadpack, sshCommand, credential helpers) that run programs;
	// GIT_ALLOW_PROTOCOL=none forbids every transport, so even a git that
	// ignores GIT_NO_LAZY_FETCH cannot start one; GIT_CONFIG_NOSYSTEM=1 means
	// the system config is never read, so the only config files trusted with
	// filter drivers are the user's global ones. Later entries win over the
	// inherited environment.
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=none",
		"GIT_OPTIONAL_LOCKS=0", "GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C", "LANGUAGE=C")
	out := &capWriter{limit: maxOutput, stop: cancel}
	var errb bytes.Buffer
	cmd.Stdout, cmd.Stderr, cmd.Stdin = out, &errb, stdin
	if sink != nil {
		cmd.Stdout = sink
	}
	err := cmd.Run()
	if out.overflow {
		return nil, -1, fmt.Errorf("git %s: git output too large", args[0])
	}
	if err != nil {
		if e := ctx.Err(); e != nil {
			if errors.Is(e, context.DeadlineExceeded) {
				return nil, -1, fmt.Errorf("git %s: timed out: %w", args[0], e)
			}
			return nil, -1, fmt.Errorf("git %s: %w", args[0], e)
		}
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			// ExitCode is -1 when git was killed by a signal.
			var o []byte
			if sink == nil {
				o = out.buf.Bytes()
			}
			return o, ee.ExitCode(), fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(errb.String()))
		}
		return nil, -1, fmt.Errorf("git %s: %w", args[0], err)
	}
	if sink != nil {
		return nil, 0, nil
	}
	return out.buf.Bytes(), 0, nil
}

// Changed returns the files changed in the working tree relative to
// root (slash-separated, sorted, without duplicates); paths outside root are
// dropped. Status runs with --no-renames, so a rename is reported as a
// deletion and an addition and both paths are included.
//
// It refuses (ErrFilterDrivers) when the repository's config defines filter
// drivers, and (ErrPartialClone, see guardLazyFetch) when git may ignore
// GIT_NO_LAZY_FETCH in a partial clone. The git status output is capped at
// 16 MiB like every git output here, so a huge untracked tree makes it fail
// with "git output too large".
func Changed(ctx context.Context, root string) ([]string, error) {
	pre, err := run(ctx, root, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	prefix := string(bytes.TrimSpace(pre))
	if set, err := repoConfigSets(ctx, root, filterKeys); err != nil {
		return nil, err
	} else if set {
		return nil, ErrFilterDrivers
	}
	if err := guardLazyFetch(ctx, root); err != nil {
		return nil, err
	}
	out, err := run(ctx, root, statusArgs...)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	// With --no-renames every record is "XY path"; no record carries a second path.
	for f := range bytes.SplitSeq(out, []byte{0}) {
		if len(f) < 4 {
			continue
		}
		p := filepath.ToSlash(string(f[3:]))
		if rest, ok := strings.CutPrefix(p, prefix); ok && rest != "" {
			seen[rest] = true
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
func repoConfigSets(ctx context.Context, root, keys string) (bool, error) {
	// One query over all scopes, following includes (git status does too).
	out, code, err := runCode(ctx, root, "config", "--show-scope", "--includes", "--get-regexp", keys)
	switch {
	case err == nil:
		for line := range bytes.SplitSeq(out, []byte("\n")) {
			if f := bytes.Fields(line); len(f) > 0 && !slices.Contains(trustedScopes, string(f[0])) {
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
	out, code, err = runCode(ctx, root, "config", "--local", "--includes", "--get-regexp", keys)
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
	if on, err := worktreeConfigMaybeOn(ctx, root); err != nil || !on {
		return false, err
	}
	// Git older than 2.20 does not know --worktree, nor the extension, so
	// there is no worktree config to read: only that usage error is ignored.
	out, code, err = runCode(ctx, root, "config", "--worktree", "--includes", "--get-regexp", keys)
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
func worktreeConfigMaybeOn(ctx context.Context, root string) (bool, error) {
	for _, inc := range [][]string{{"--no-includes"}, {"--includes"}} {
		args := append(append([]string{"config", "--local"}, inc...), "--bool", "--get-all", "extensions.worktreeConfig")
		out, code, err := runCode(ctx, root, args...)
		switch {
		case noMatches(out, code, err):
			continue
		case err != nil:
			return false, err
		}
		for v := range bytes.FieldsSeq(out) {
			if string(v) != "false" {
				return true, nil
			}
		}
	}
	return false, nil
}
