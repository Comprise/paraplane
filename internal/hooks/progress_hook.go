package hooks

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"paraplane/internal/gitx"
	"paraplane/internal/plan"
	"paraplane/internal/rules"
	"paraplane/internal/state"
	"paraplane/internal/textx"
	"paraplane/internal/track"
)

// Limits of the progress hook (spec 2026-10-23 §7).
const (
	maxTaskLines     = 16 // Paraplane-Task lines read from one prompt
	maxProgressPlans = 8  // plans shown per hook call
)

// taskLine is a subagent prompt line naming its plan and task.
var taskLine = regexp.MustCompile(`(?m)^[ \t]*Paraplane-Task:[ \t]+(\S+)[ \t]+([A-Za-z][A-Za-z0-9_.-]{0,31})[ \t]*$`)

// commitWord is a word of a Bash command after which new commits may exist.
var commitWord = regexp.MustCompile(`(^|[^A-Za-z0-9_-])(commit|merge|cherry-pick|rebase|am|pull|revert)([^A-Za-z0-9_-]|$)`)

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
	var entries []state.Entry
	var plans []string
	for _, m := range taskLine.FindAllStringSubmatch(prompt, maxTaskLines) {
		p := m[1]
		if !filepath.IsAbs(p) {
			p = filepath.Join(env.Root, filepath.FromSlash(p))
		}
		rel, ok := rootRel(env.Root, p)
		if !ok || !isPlanPath(env, rel) {
			continue
		}
		if _, ok := loadPlan(env, rel); !ok {
			continue
		}
		entries = append(entries, state.Entry{Plan: rel, Task: m[2]})
		plans = appendUnique(plans, rel)
	}
	if len(entries) == 0 {
		return nil, nil
	}
	gitDir, err := gitx.GitDir(env.Root)
	if err != nil {
		return nil, nil
	}
	head, err := gitx.Head(env.Root)
	if err != nil {
		return nil, nil
	}
	for i := range entries {
		entries[i].Head = head
	}
	if state.Update(gitDir, func(s *state.State) { s.Entries = append(s.Entries, entries...) }) != nil {
		return nil, nil
	}
	return showProgress(env, gitDir, plans)
}

// progressAfterBash shows the progress of the plans that new commits name,
// only for a command with a commit-making word (no git runs otherwise).
func progressAfterBash(env Env, in Input) ([]byte, error) {
	if !commitWord.MatchString(bashCommand(in)) {
		return nil, nil
	}
	gitDir, err := gitx.GitDir(env.Root)
	if err != nil {
		return nil, nil
	}
	head, err := gitx.Head(env.Root)
	if err != nil || head == "" {
		return nil, nil
	}
	last := state.Load(gitDir).LastHead
	if last == head {
		return nil, nil
	}
	from := ""
	if last != "" {
		if ok, err := gitx.IsAncestor(env.Root, last, head); err == nil && ok {
			from = last
		}
	}
	named, err := gitx.PlansSince(env.Root, from, head)
	if state.Update(gitDir, func(s *state.State) { s.LastHead = head }) != nil || err != nil {
		return nil, nil
	}
	var plans []string
	for _, rel := range named {
		if isPlanPath(env, rel) {
			plans = append(plans, rel)
		}
	}
	return showProgress(env, gitDir, plans)
}

// showProgress renders each plan (at most maxProgressPlans) and answers for
// those whose done waves (progress: wave) or done tasks (progress: task) grew
// since they were last shown. Claude gets a systemMessage for the user;
// Codex, whose display of it is unverified, gets it as context to show.
func showProgress(env Env, gitDir string, plans []string) ([]byte, error) {
	if len(plans) > maxProgressPlans {
		plans = plans[:maxProgressPlans]
	}
	type shown struct {
		rel  string
		text string
		st   track.Status
	}
	var out []shown
	for _, rel := range plans {
		p, ok := loadPlan(env, rel)
		if !ok {
			continue
		}
		st, err := track.PlanStatus(env.Root, rel, p, nil)
		if err != nil {
			continue
		}
		out = append(out, shown{rel: rel, text: st.Text, st: st})
	}
	var texts, names []string
	err := state.Update(gitDir, func(s *state.State) {
		if s.Shown == nil {
			s.Shown = map[string]state.Shown{}
		}
		for _, o := range out {
			prev := s.Shown[o.rel]
			grew := o.st.Waves > prev.Waves || (env.Config.Progress == "task" && o.st.Tasks > prev.Tasks)
			s.Shown[o.rel] = state.Shown{Waves: o.st.Waves, Tasks: o.st.Tasks}
			if grew {
				texts = append(texts, strings.TrimRight(o.text, "\n"))
				names = append(names, textx.Line(o.rel, maxPath))
			}
		}
	})
	if err != nil || len(texts) == 0 {
		return nil, nil
	}
	msg := strings.Join(texts, "\n\n")
	if env.Agent == Codex {
		return contextOutput("PostToolUse", "paraplane: progress of "+strings.Join(names, ", ")+":\n"+msg+
			"\nShow this progress to the user in a code block.")
	}
	return encodeJSON(hookOutput{SystemMessage: msg, Hook: &hookSpecific{EventName: "PostToolUse",
		AdditionalContext: "paraplane: progress of " + strings.Join(names, ", ") + " was shown to the user; do not repeat it."}})
}

// loadPlan reads and checks the plan file rel; ok is false for a missing or
// irregular file or a plan with E errors.
func loadPlan(env Env, rel string) (*plan.Plan, bool) {
	md, ok, err := readPlan(filepath.Join(env.Root, filepath.FromSlash(rel)))
	if err != nil || !ok {
		return nil, false
	}
	r := rules.Check(md, Options(env))
	if r.Errors() > 0 || r.Plan == nil {
		return nil, false
	}
	return r.Plan, true
}

// appendUnique appends s when it is not in list yet.
func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
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
