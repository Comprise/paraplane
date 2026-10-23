package hooks

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"paraplane/internal/fsx"
	"paraplane/internal/gitx"
	"paraplane/internal/ids"
	"paraplane/internal/plan"
	"paraplane/internal/rules"
	"paraplane/internal/shellx"
	"paraplane/internal/state"
	"paraplane/internal/textx"
	"paraplane/internal/track"
)

// Limits of the progress hook (spec 2026-10-23 §7).
const (
	maxTaskLines     = 16 // Paraplane-Task lines read from one prompt
	maxProgressPlans = 8  // plans shown per hook call
)

// maxStateSize caps the marshalled state a hook writes: above it the scan
// cache is dropped, so the file stays well under the 4 MiB the state
// package reads (a larger file would read back as empty). A variable so
// tests can lower it.
var maxStateSize = 2 << 20

// taskLine is a subagent prompt line naming its plan and task.
var taskLine = regexp.MustCompile(`(?m)^[ \t]*Paraplane-Task:[ \t]+(\S+)[ \t]+(` + ids.Pattern + `)[ \t]*$`)

// commitWords are the git subcommands after which new commits may exist.
var commitWords = []string{"commit", "merge", "cherry-pick", "rebase", "am", "pull", "revert"}

// gitWrappers are command names that may run git as a later word: wrappers
// and shell keywords (shellx.CommandNames gives only the first word).
var gitWrappers = []string{"sudo", "env", "command", "exec", "time", "nohup", "{", "!", "if", "then", "do"}

// The track and test hooks, replaced in tests: statuses to make the scan
// fail, beforeShowWrite to run another hook between the scan and the write
// of showProgress.
var (
	statuses        = track.Statuses
	beforeShowWrite func()
)

// commitCommand reports whether a Bash command may have made commits: it
// runs git (a command name whose base is git, or a wrapper or keyword
// followed somewhere by a git word) and one of its words is a commit word.
// It is a cheap check that runs before any git process.
func commitCommand(cmd string) bool {
	words := shellx.Words(cmd)
	trigger := false
	for _, w := range words {
		if slices.Contains(commitWords, w.Text) {
			trigger = true
			break
		}
	}
	if !trigger {
		return false
	}
	isGit := func(name string) bool {
		b := path.Base(anySlash(name))
		return b == "git" || b == "git.exe"
	}
	wrapped := false
	for _, n := range shellx.CommandNames(cmd) {
		if isGit(n) {
			return true
		}
		if slices.Contains(gitWrappers, path.Base(anySlash(n))) {
			wrapped = true
		}
	}
	return wrapped && slices.ContainsFunc(words, func(w shellx.Word) bool { return isGit(w.Text) })
}

// checked is a plan file read once per hook call: parsed for recording,
// fully checked for showing.
type checked struct {
	md     []byte
	parsed *plan.Plan // nil: missing, irregular or not parsable
	hash   string     // track.PlanHash of md
	plan   *plan.Plan // the checked plan; nil until check, or with E errors
	done   bool       // check ran
	late   bool       // the parse or check ran out of time: the plan may be fine
}

// planCache keeps the plan files of one hook call by path from the root.
type planCache map[string]*checked

// read reads and parses the plan file rel once.
func (c planCache) read(env Env, rel string) *checked {
	if ch, ok := c[rel]; ok {
		return ch
	}
	ch := &checked{}
	c[rel] = ch
	md, ok, err := readPlan(filepath.Join(env.Root, filepath.FromSlash(rel)))
	if err != nil || !ok {
		return ch
	}
	p, err := plan.LoadWithin(md, Options(env).Deadline)
	switch {
	case err == nil:
		ch.md, ch.parsed, ch.hash = md, p, track.PlanHash(md)
	case errors.Is(err, plan.ErrDeadline):
		ch.late = true
	}
	return ch
}

// check checks the plan rel by the rules once: plan is nil with E errors
// or when the file cannot be read; late tells a timeout apart.
func (c planCache) check(env Env, rel string) *checked {
	ch := c.read(env, rel)
	if ch.done || ch.parsed == nil || ch.late {
		return ch
	}
	ch.done = true
	r := rules.Check(ch.md, Options(env))
	ch.late = r.TimedOut
	if r.Errors() == 0 && r.Plan != nil {
		ch.plan = r.Plan
	}
	return ch
}

// hasTask reports whether id is a task of p.
func hasTask(p *plan.Plan, id string) bool {
	return slices.ContainsFunc(p.Tasks(), func(t *plan.Task) bool { return t.ID == id })
}

// taskPlanRel is the plan path of a Paraplane-Task line as a path from the
// root; absolute paths inside the root count. ok is false outside the root
// or plan_paths.
func taskPlanRel(env Env, p string) (string, bool) {
	if !slashAbs(anySlash(p)) {
		p = filepath.Join(env.Root, filepath.FromSlash(p))
	}
	rel, ok := rootRel(env.Root, p)
	return rel, ok && isPlanPath(env, rel)
}

// storedPlanRel checks a plan path that comes from a commit trailer or the
// state file before anything reads it: it must already be a clean path from
// the root (no "..", ".", absolute form or backslash) of plan_paths.
func storedPlanRel(env Env, p string) bool {
	rel, ok := rootRel(env.Root, p)
	return ok && rel == p && isPlanPath(env, rel)
}

// progressAfterAgent records the tasks a returned subagent names in its
// prompt (Paraplane-Task: <plan> <id>) and shows the progress of their plans
// when it moved.
func progressAfterAgent(env Env, in Input) ([]byte, error) {
	var v struct {
		Prompt     any `json:"prompt"`
		Background any `json:"run_in_background"`
	}
	if json.Unmarshal(in.ToolInput, &v) != nil {
		return nil, nil
	}
	// A background subagent's PostToolUse fires when it is launched, not
	// when it finishes: it is not done yet.
	if v.Background == true || strings.Contains(string(in.ToolResponse), "Async agent launched") {
		return nil, nil
	}
	prompt, _ := v.Prompt.(string)
	cache := planCache{}
	gitDir, plans, ok := recordTasks(env, cache, prompt, false)
	if !ok {
		return nil, nil
	}
	return showProgress(env, in, cache, gitDir, plans, showOpts{})
}

// progressAfterBash shows the progress of the plans that new commits name,
// only for a git command with a commit word (no git runs otherwise). The
// state is written only when a commit names a plan or the state file
// exists already, and lastHead advances only once the commits were read.
func progressAfterBash(env Env, in Input) ([]byte, error) {
	if !commitCommand(bashCommand(in)) {
		return nil, nil
	}
	ctx := env.ctx()
	gitDir, err := gitx.GitDirContext(ctx, env.Root)
	if err != nil {
		return nil, nil
	}
	head, err := gitx.HeadContext(ctx, env.Root)
	if err != nil || head == "" {
		return nil, nil
	}
	st, err := state.LoadState(gitDir)
	if err != nil || st.LastHead == head {
		return nil, nil
	}
	from := ""
	if st.LastHead != "" {
		if ok, err := gitx.IsAncestorContext(ctx, env.Root, st.LastHead, head); err == nil && ok {
			from = st.LastHead
		}
	}
	named, err := gitx.PlansSinceContext(ctx, env.Root, from, head)
	if err != nil {
		return nil, nil
	}
	var plans []string
	for _, p := range named {
		if storedPlanRel(env, p) {
			plans = appendUnique(plans, p)
		}
	}
	// Plans still waiting (a Stop not yet run; Codex, whose Stop does not
	// show them) are shown with the named ones.
	for _, p := range st.Pending {
		plans = appendUnique(plans, p)
	}
	if len(plans) == 0 {
		if stateExists(gitDir) {
			state.UpdateCtx(ctx, gitDir, func(s *state.State) { moveLastHead(s, st.LastHead, head) })
		}
		return nil, nil
	}
	return showProgress(env, in, planCache{}, gitDir, plans, showOpts{lastHead: head, prevHead: st.LastHead})
}

// moveLastHead sets s.LastHead to head unless another hook moved it since
// it was read as prev: two overlapping hooks must not move it back.
func moveLastHead(s *state.State, prev, head string) {
	if s.LastHead == prev || s.LastHead == "" {
		s.LastHead = head
	}
}

// stateExists reports whether <gitDir>/paraplane/closed.json exists (the
// names of the state package).
func stateExists(gitDir string) bool {
	_, err := os.Lstat(filepath.Join(gitDir, "paraplane", "closed.json"))
	return err == nil
}

// showOpts is what a showProgress write does besides the shown counts.
type showOpts struct {
	lastHead string // move lastHead to it (Bash), when not ""
	prevHead string // the lastHead read before the scan (moveLastHead)
}

// showProgress renders the plans (paths pass storedPlanRel first) with one
// track.Statuses outside the lock and answers for those whose done waves
// (progress: wave) or done tasks (progress: task) grew since they were last
// shown. At most maxProgressPlans are rendered; the others and those whose
// check ran out of time go to Pending for the next hook, so a moved
// lastHead loses no plan. The write applies the statuses, raises the shown
// counts (never lowers them), moves lastHead and takes from Pending the
// plans shown or unusable; a plan that got an entry after the scan stays
// pending. A failed scan writes nothing. Claude gets a systemMessage for the
// user (Stop gets nothing else); Codex, whose display of it is unverified,
// gets it as context to show.
func showProgress(env Env, in Input, cache planCache, gitDir string, plans []string, o showOpts) ([]byte, error) {
	var refs []track.PlanRef
	var dead []string  // plans that can never be shown
	var later []string // plans to show in a later hook
	for _, p := range plans {
		if !storedPlanRel(env, p) {
			dead = append(dead, p)
			continue
		}
		if len(refs) == maxProgressPlans {
			later = append(later, p)
			continue
		}
		ch := cache.check(env, p)
		switch {
		case ch.late:
			later = append(later, p)
		case ch.plan == nil:
			dead = append(dead, p)
		default:
			refs = append(refs, track.PlanRef{Rel: p, Hash: ch.hash, Plan: ch.plan})
		}
	}
	if len(refs) == 0 && o.lastHead == "" && len(dead) == 0 && len(later) == 0 {
		return nil, nil
	}
	ctx := env.ctx()
	st, err := state.LoadState(gitDir)
	if err != nil {
		return nil, nil
	}
	res := track.Result{Apply: func(*state.State) {}}
	if len(refs) > 0 {
		if res, err = statuses(ctx, env.Root, refs, st); err != nil {
			return nil, nil
		}
	}
	seen := map[state.Entry]bool{}
	for _, e := range st.Entries {
		seen[e] = true
	}
	if beforeShowWrite != nil {
		beforeShowWrite()
	}
	var texts, names []string
	err = state.UpdateCtx(ctx, gitDir, func(s *state.State) {
		take := dead
		for _, r := range refs {
			if !slices.ContainsFunc(s.Entries, func(e state.Entry) bool { return e.Plan == r.Rel && !seen[e] }) {
				take = append(take, r.Rel)
			}
		}
		res.Apply(s)
		if o.lastHead != "" {
			moveLastHead(s, o.prevHead, o.lastHead)
		}
		if s.Shown == nil {
			s.Shown = map[string]state.Shown{}
		}
		for _, r := range refs {
			now, prev := res.ByPlan[r.Rel], s.Shown[r.Rel]
			if now.Waves > prev.Waves || (env.Config.Progress == "task" && now.Tasks > prev.Tasks) {
				texts = append(texts, strings.TrimRight(now.Text, "\n"))
				names = append(names, textx.Line(r.Rel, maxPath))
			}
			s.Shown[r.Rel] = state.Shown{Waves: max(prev.Waves, now.Waves), Tasks: max(prev.Tasks, now.Tasks)}
		}
		s.TakePending(take)
		for _, p := range later {
			s.Pending = appendUnique(s.Pending, p)
		}
		capState(s)
	})
	if err != nil || len(texts) == 0 {
		return nil, nil
	}
	msg := strings.Join(texts, "\n\n")
	about := "paraplane: progress of " + strings.Join(names, ", ")
	switch {
	case env.Agent == Codex:
		return contextOutput(eventOf(in), about+":\n"+msg+"\nShow this progress to the user in a code block.")
	case eventOf(in) == "Stop":
		return encodeJSON(hookOutput{SystemMessage: msg})
	}
	return encodeJSON(hookOutput{SystemMessage: msg, Hook: &hookSpecific{EventName: eventOf(in),
		AdditionalContext: about + " was shown to the user; do not repeat it."}})
}

// capState drops the scan cache when the state would be larger than
// maxStateSize.
func capState(s *state.State) {
	if s.Scan == nil {
		return
	}
	if data, err := json.Marshal(s); err != nil || len(data) > maxStateSize {
		s.Scan = nil
	}
}

// appendUnique appends s when it is not in list yet.
func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// mergeOutputs combines two hook answers: a block wins; otherwise the system
// messages and the additional contexts are joined.
func mergeOutputs(a, b []byte) ([]byte, error) {
	if a == nil || isBlock(a) {
		if a == nil {
			return b, nil
		}
		return a, nil
	}
	if b == nil {
		return a, nil
	}
	var x, y hookOutput
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return a, nil
	}
	join := func(s, t string) string {
		if s == "" || t == "" {
			return s + t
		}
		return s + "\n\n" + t
	}
	x.SystemMessage = join(x.SystemMessage, y.SystemMessage)
	if y.Hook != nil {
		if x.Hook == nil {
			x.Hook = y.Hook
		} else {
			x.Hook.AdditionalContext = join(x.Hook.AdditionalContext, y.Hook.AdditionalContext)
		}
	}
	return encodeJSON(x)
}

// recordTasks records the tasks the subagent prompt names
// (Paraplane-Task: <plan> <id>, at most maxTaskLines lines) with the current
// HEAD and the hash of the plan block; with pending the plans also wait for
// a hook that can show their progress. A line counts when its plan is a
// plan file of plan_paths that parses and its id is a task of that plan;
// the full check waits for the show. ok is false when nothing was recorded
// (and nothing was written).
func recordTasks(env Env, cache planCache, prompt string, pending bool) (gitDir string, plans []string, ok bool) {
	var entries []state.Entry
	for _, m := range taskLine.FindAllStringSubmatch(prompt, maxTaskLines) {
		rel, ok := taskPlanRel(env, m[1])
		if !ok {
			continue
		}
		ch := cache.read(env, rel)
		if ch.parsed == nil || !hasTask(ch.parsed, m[2]) {
			continue
		}
		entries = append(entries, state.Entry{Plan: rel, Task: m[2], PlanHash: ch.hash})
		plans = appendUnique(plans, rel)
	}
	if len(entries) == 0 {
		return "", nil, false
	}
	ctx := env.ctx()
	gitDir, err := gitx.GitDirContext(ctx, env.Root)
	if err != nil {
		return "", nil, false
	}
	head, err := gitx.HeadContext(ctx, env.Root)
	if err != nil || head == "" {
		return "", nil, false
	}
	err = state.UpdateCtx(ctx, gitDir, func(s *state.State) {
		for _, e := range entries {
			e.Head = head
			s.Add(e)
		}
		if pending {
			for _, p := range plans {
				s.Pending = appendUnique(s.Pending, p)
			}
		}
	})
	return gitDir, plans, err == nil
}

// maxTranscriptHead caps the bytes read from a subagent transcript: only its
// first record, the prompt, is needed.
const maxTranscriptHead = 1 << 20

// SubagentStop records the tasks a finished subagent's prompt names, read from
// the first record of its transcript. It only records (a parse, no check,
// no scan): Claude Code does not show a SubagentStop systemMessage, so the
// progress waits for Stop (pending), which checks and scans. Subagents
// without a Paraplane-Task line, among them Claude Code's own helpers, are
// ignored.
func SubagentStop(env Env, in Input) ([]byte, error) {
	if !env.Config.Enabled || in.AgentTranscriptPath == "" {
		return nil, nil
	}
	env, _, done := begin(env, in, "SubagentStop")
	defer done()
	f, _, err := fsx.OpenRegular(in.AgentTranscriptPath)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	line, _ := bufio.NewReaderSize(io.LimitReader(f, maxTranscriptHead), 64<<10).ReadString('\n')
	var rec struct {
		Type    string `json:"type"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(line), &rec) != nil || rec.Type != "user" {
		return nil, nil
	}
	var prompt string
	if json.Unmarshal(rec.Message.Content, &prompt) != nil {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(rec.Message.Content, &blocks) != nil {
			return nil, nil
		}
		for _, b := range blocks {
			if b.Type == "text" {
				prompt += b.Text + "\n"
			}
		}
	}
	recordTasks(env, planCache{}, prompt, true)
	return nil, nil
}

// flushPending shows the progress of the plans waiting since a SubagentStop
// (Claude Code's Stop shows a systemMessage); showProgress takes them from
// Pending only once handled.
func flushPending(env Env, in Input) ([]byte, error) {
	gitDir, err := gitx.GitDirContext(env.ctx(), env.Root)
	if err != nil {
		return nil, nil
	}
	st, err := state.LoadState(gitDir)
	if err != nil || len(st.Pending) == 0 {
		return nil, nil
	}
	return showProgress(env, in, planCache{}, gitDir, st.Pending, showOpts{})
}
