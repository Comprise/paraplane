package gitx

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	"paraplane/internal/ids"
)

// LostTrailer is a commit whose message has Paraplane-Plan and
// Paraplane-Wave or Paraplane-Task lines that git does not read as trailers:
// they are not in the last paragraph (a blank line before Co-Authored-By,
// say).
type LostTrailer struct {
	Commit string // abbreviated hash
	Kind   string // "wave" or "task"
	ID     string // the wave or task id
}

// Commits is what the commits of a plan record: waves and tasks named by
// trailers, and the lines git did not read as trailers.
type Commits struct {
	Waves, Tasks []string
	Lost         []LostTrailer
}

// TrailerCommit is one commit with paraplane lines: the plan they name (the
// last Paraplane-Plan of the trailers, or of the message when the trailers
// have none), the waves and tasks of its trailers, and the Paraplane-Wave and
// Paraplane-Task lines for that plan that git does not read as trailers.
type TrailerCommit struct {
	Hash                 string // full
	Plan                 string
	Waves, Tasks         []string
	LostWaves, LostTasks []string
}

// Scanned is the result of Scan: the HEAD it read up to and its plan
// commits, oldest first.
type Scanned struct {
	Head    string
	Commits []TrailerCommit
}

// planTrailers is the git log format of a plan commit's trailers.
const planTrailers = "%(trailers:key=Paraplane-Plan,key=Paraplane-Wave,key=Paraplane-Task,unfold)"

// planGreps limits git log to commits with a Paraplane-Wave or
// Paraplane-Task line, so the output does not grow with the history.
var planGreps = []string{"-i", "--grep=^paraplane-wave:", "--grep=^paraplane-task:"}

var (
	recSep   = []byte{0x1e}
	fieldSep = []byte{0x1f}
	newline  = []byte("\n")
)

// Scan reads in one git log the plan commits of all plans in from..HEAD,
// oldest first; with from empty, not a full hash or not an ancestor of HEAD
// (rewritten history) it reads the whole history. A repository without
// commits gives an empty Scanned. It needs git 2.24 or later (ErrOldGit).
func Scan(ctx context.Context, root, from string) (Scanned, error) {
	if err := requireGit(ctx, root, minGit[0], minGit[1], minGit[2]); err != nil {
		return Scanned{}, fmt.Errorf("%w for progress", err)
	}
	head, err := Head(ctx, root)
	if err != nil || head == "" {
		return Scanned{}, err
	}
	if err := guardLazyFetch(ctx, root); err != nil {
		return Scanned{}, err
	}
	rng := head
	if validHash(from) {
		if from == head {
			return Scanned{Head: head}, nil
		}
		ok, err := isAncestor(ctx, root, from, head)
		if e := ctx.Err(); e != nil {
			if err == nil {
				err = e
			}
			return Scanned{}, err
		}
		if err == nil && ok {
			rng = from + ".." + head
		}
	}
	args := append([]string{"log", "--no-show-signature", "--reverse"}, planGreps...)
	out, err := run(ctx, root, append(args, "--format=%H%x1f"+planTrailers+"%x1f%B%x1e", "--end-of-options", rng)...)
	if err != nil {
		return Scanned{}, err
	}
	s := Scanned{Head: head}
	for rec := range bytes.SplitSeq(out, recSep) {
		hash, rest, ok := bytes.Cut(bytes.TrimLeft(rec, "\n"), fieldSep)
		if !ok {
			continue
		}
		trailers, body, _ := bytes.Cut(rest, fieldSep)
		if c, ok := trailerCommit(string(hash), parseLines(trailers), parseLines(body)); ok {
			s.Commits = append(s.Commits, c)
		}
	}
	return s, nil
}

// trailerCommit builds the commit from its trailer lines t and message
// lines b; ok is false when it names no plan or no ids.
func trailerCommit(hash string, t, b lines) (TrailerCommit, bool) {
	c := TrailerCommit{Hash: hash, Plan: t.plan}
	if !t.hasPlan {
		c.Plan = b.plan
	}
	if c.Plan == "" {
		return c, false
	}
	if t.hasPlan {
		c.Waves, c.Tasks = appendNew(nil, t.waves...), appendNew(nil, t.tasks...)
	}
	if b.hasPlan && b.plan == c.Plan {
		for _, w := range b.waves {
			if !slices.Contains(c.Waves, w) {
				c.LostWaves = appendNew(c.LostWaves, w)
			}
		}
		for _, id := range b.tasks {
			if !slices.Contains(c.Tasks, id) {
				c.LostTasks = appendNew(c.LostTasks, id)
			}
		}
	}
	ok := len(c.Waves)+len(c.Tasks)+len(c.LostWaves)+len(c.LostTasks) > 0
	return c, ok
}

// CommitsOf folds the commits of plan: waves and tasks each in order of first
// appearance, without duplicates; Lost lists, once per id and only for ids
// not counted, the commits whose lines git does not read as trailers.
func CommitsOf(commits []TrailerCommit, plan string) Commits {
	c := Commits{Waves: []string{}, Tasks: []string{}}
	var lost []LostTrailer
	for _, tc := range commits {
		if tc.Plan != plan {
			continue
		}
		c.Waves = appendNew(c.Waves, tc.Waves...)
		c.Tasks = appendNew(c.Tasks, tc.Tasks...)
		for _, w := range tc.LostWaves {
			lost = append(lost, LostTrailer{Commit: short(tc.Hash), Kind: "wave", ID: w})
		}
		for _, id := range tc.LostTasks {
			lost = append(lost, LostTrailer{Commit: short(tc.Hash), Kind: "task", ID: id})
		}
	}
	for _, l := range lost {
		counted := c.Waves
		if l.Kind == "task" {
			counted = c.Tasks
		}
		if !slices.Contains(counted, l.ID) && !slices.ContainsFunc(c.Lost, func(x LostTrailer) bool { return x.Kind == l.Kind && x.ID == l.ID }) {
			c.Lost = append(c.Lost, l)
		}
	}
	return c
}

// short abbreviates a full hash to 12 digits.
func short(h string) string { return h[:min(12, len(h))] }

// PlanCommits returns the waves and tasks committed with the
// Paraplane-Plan trailer equal to planPath: CommitsOf over a full Scan.
func PlanCommits(ctx context.Context, root, planPath string) (Commits, error) {
	s, err := Scan(ctx, root, "")
	if err != nil {
		return Commits{Waves: []string{}, Tasks: []string{}}, err
	}
	return CommitsOf(s.Commits, planPath), nil
}

// CommitFiles is a commit of a plan with the waves and tasks its trailers
// name and the files it changed.
type CommitFiles struct {
	Hash                string // abbreviated
	Waves, Tasks, Files []string
}

// PlanCommitFiles returns the commits of the current branch whose
// trailers name planPath and at least one wave or task, oldest first, with
// their changed files under root, relative to it (sorted; --no-renames, so a
// rename gives both paths; a merge gives none). It needs git 2.24 or later
// (ErrOldGit) and refuses like guardLazyFetch.
func PlanCommitFiles(ctx context.Context, root, planPath string) ([]CommitFiles, error) {
	if err := requireGit(ctx, root, minGit[0], minGit[1], minGit[2]); err != nil {
		return nil, fmt.Errorf("%w for audit of commits", err)
	}
	head, err := Head(ctx, root)
	if err != nil || head == "" {
		return nil, err
	}
	if err := guardLazyFetch(ctx, root); err != nil {
		return nil, err
	}
	pre, err := run(ctx, root, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	prefix := string(bytes.TrimSpace(pre))
	args := append([]string{"log", "--no-show-signature", "--reverse", "--no-renames", "--name-only", "-z"}, planGreps...)
	out, err := run(ctx, root, append(args, "--format=%x1e%h%x1f"+planTrailers+"%x1f", "--end-of-options", head)...)
	if err != nil {
		return nil, err
	}
	var res []CommitFiles
	first := true
	for rec := range bytes.SplitSeq(out, recSep) {
		if first { // the text before the first separator
			first = false
			continue
		}
		parts := bytes.SplitN(rec, fieldSep, 3)
		if len(parts) != 3 {
			continue
		}
		l := parseLines(parts[1])
		if !l.hasPlan || l.plan != planPath || len(l.waves)+len(l.tasks) == 0 {
			continue
		}
		// -z: names are NUL-separated and unquoted, relative to the top of
		// the repository; those under root are kept, made relative to it.
		var files []string
		for f := range bytes.SplitSeq(parts[2], []byte{0}) {
			f = bytes.TrimLeft(f, "\n")
			if len(f) == 0 {
				continue
			}
			if rel, ok := strings.CutPrefix(string(f), prefix); ok {
				files = appendNew(files, rel)
			}
		}
		slices.Sort(files)
		res = append(res, CommitFiles{Hash: string(parts[0]), Waves: l.waves, Tasks: l.tasks, Files: files})
	}
	return res, nil
}

// appendNew appends the ids not yet in list.
func appendNew(list []string, ids ...string) []string {
	for _, id := range ids {
		if !slices.Contains(list, id) {
			list = append(list, id)
		}
	}
	return list
}

// lines are the paraplane lines of a text: the last Paraplane-Plan value and
// the valid ids of every Paraplane-Wave and Paraplane-Task line, in order.
type lines struct {
	plan         string
	hasPlan      bool
	waves, tasks []string
}

// parseLines reads Paraplane-Plan, Paraplane-Wave and Paraplane-Task lines
// (key case ignored; the last plan wins). A value may list several ids
// separated by commas (Paraplane-Wave: W1, W2); a value with any invalid part
// is dropped. Only the matching lines are copied out of text.
func parseLines(text []byte) lines {
	var l lines
	for len(text) > 0 {
		var line []byte
		line, text, _ = bytes.Cut(text, newline)
		k, v, ok := bytes.Cut(line, []byte(":"))
		if !ok {
			continue
		}
		k = bytes.TrimSpace(k)
		switch {
		case bytes.EqualFold(k, []byte("paraplane-plan")):
			l.plan, l.hasPlan = string(bytes.TrimSpace(v)), true
		case bytes.EqualFold(k, []byte("paraplane-wave")):
			l.waves = append(l.waves, idList(v)...)
		case bytes.EqualFold(k, []byte("paraplane-task")):
			l.tasks = append(l.tasks, idList(v)...)
		}
	}
	return l
}

// idList splits a trailer value on commas; any invalid part drops it all.
func idList(v []byte) []string {
	var out []string
	for part := range bytes.SplitSeq(v, []byte(",")) {
		id := string(bytes.TrimSpace(part))
		if !ids.Valid(id) {
			return nil
		}
		out = append(out, id)
	}
	return out
}
