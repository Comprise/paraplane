package gitx

import (
	"bytes"
	"context"
	"errors"
	"strings"
)

// Head returns the full hash of HEAD, or "" in a repository without
// commits.
func Head(ctx context.Context, root string) (string, error) {
	out, code, err := runCode(ctx, root, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		if code == 1 && len(bytes.TrimSpace(out)) == 0 {
			if _, e := run(ctx, root, "rev-parse", "--git-dir"); e == nil {
				return "", nil
			}
		}
		return "", err
	}
	return string(bytes.TrimSpace(out)), nil
}

// GitDir returns the absolute git directory of the repository at
// root (a worktree has its own).
func GitDir(ctx context.Context, root string) (string, error) {
	out, err := run(ctx, root, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(out)), nil
}

// IsAncestor reports whether commit a is an ancestor of b or equal to
// it. Both must be full hashes; for many hashes use Unreachable. It needs
// git 2.24 or later (ErrOldGit).
func IsAncestor(ctx context.Context, root, a, b string) (bool, error) {
	if err := checkHashes(a, b); err != nil {
		return false, err
	}
	if err := requireGit(ctx, root, minGit[0], minGit[1], minGit[2]); err != nil {
		return false, err
	}
	if err := guardLazyFetch(ctx, root); err != nil {
		return false, err
	}
	return isAncestor(ctx, root, a, b)
}

// isAncestor is IsAncestor for validated hashes and a guarded
// repository.
func isAncestor(ctx context.Context, root, a, b string) (bool, error) {
	_, code, err := runCode(ctx, root, "merge-base", "--is-ancestor", "--end-of-options", a, b)
	switch {
	case err == nil:
		return true, nil
	case code == 1:
		return false, nil
	}
	return false, err
}

// Unreachable reports for each of hashes whether it is not reachable from
// head (head itself and its ancestors are reachable). A value that is not a
// full hash, an object that does not exist or is not a commit counts as
// unreachable without an error. head must be a full hash. Two git processes
// run whatever the number of hashes: cat-file --batch-check keeps the
// existing commits, then one rev-list --stdin with ^head lists those not
// reachable; its output is streamed and matched, not stored. It needs git
// 2.24 or later (ErrOldGit), like every function that reads commits.
func Unreachable(ctx context.Context, root string, hashes []string, head string) (map[string]bool, error) {
	if err := checkHashes(head); err != nil {
		return nil, err
	}
	res := make(map[string]bool, len(hashes))
	var cand []string
	for _, h := range hashes {
		if _, done := res[h]; done {
			continue
		}
		res[h] = true
		if validHash(h) {
			cand = append(cand, h)
		}
	}
	if len(cand) == 0 {
		return res, nil
	}
	if err := requireGit(ctx, root, minGit[0], minGit[1], minGit[2]); err != nil {
		return nil, err
	}
	if err := guardLazyFetch(ctx, root); err != nil {
		return nil, err
	}
	in := strings.Join(cand, "\n") + "\n"
	out, err := runIOOut(ctx, root, in, "cat-file", "--batch-check=%(objectname) %(objecttype)")
	if err != nil {
		return nil, err
	}
	commits := map[string]bool{}
	for line := range bytes.SplitSeq(out, newline) {
		if name, typ, ok := bytes.Cut(line, []byte(" ")); ok && string(typ) == "commit" {
			commits[string(name)] = true
		}
	}
	var stdin strings.Builder
	stdin.WriteString("^" + head + "\n")
	for _, h := range cand {
		if commits[h] {
			stdin.WriteString(h + "\n")
		}
	}
	m := &lineMatcher{want: commits, found: map[string]bool{}}
	if _, _, err := runIO(ctx, root, strings.NewReader(stdin.String()), m, "rev-list", "--stdin"); err != nil {
		return nil, err
	}
	for _, h := range cand {
		res[h] = !commits[h] || m.found[h]
	}
	return res, nil
}

// runIOOut runs git with stdin and returns its captured stdout.
func runIOOut(ctx context.Context, root, stdin string, args ...string) ([]byte, error) {
	out, _, err := runIO(ctx, root, strings.NewReader(stdin), nil, args...)
	return out, err
}

// lineMatcher records which of the wanted lines a stream contains, keeping
// only the current partial line.
type lineMatcher struct {
	want, found map[string]bool
	partial     []byte
}

// maxLine bounds the partial line kept; rev-list prints object names only.
const maxLine = 256

func (m *lineMatcher) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			m.partial = append(m.partial, p...)
			if len(m.partial) > maxLine {
				return 0, errors.New("git output line too long")
			}
			break
		}
		line := p[:i]
		if len(m.partial) > 0 {
			line = append(m.partial, line...)
			m.partial = m.partial[:0]
		}
		if m.want[string(line)] {
			m.found[string(line)] = true
		}
		p = p[i+1:]
	}
	return n, nil
}
