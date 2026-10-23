# paraplane — progress hook and per-task commits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. The paraplane execution rules (`skills/rules/references/execution.md`) take precedence: tasks of a wave run in parallel, each only in its `owns`, subagents never commit, golden files are edited by hand. Trailers: `Paraplane-Plan: docs/superpowers/plans/2026-10-23-progress-hook.md`, `Paraplane-Wave: <id>`.

**Goal:** progress shows itself to the user (a `PostToolUse` hook with `systemMessage`) when a wave or task is done, done-ness comes from wave commits, stage commits, task commits (`Paraplane-Task`), returned subagents and `--done`, and the audit checks committed work of a wave.

**Architecture:** `gitx` reads plan trailers (waves and tasks) and the files of wave commits; a new `state` package keeps tasks of returned subagents in `<git-dir>/paraplane/closed.json`; a new `track` package combines commits, state and `--done` into the rendered progress and done counts, used by both the CLI and the hook; `rules.Audit` checks commit files per task. Existing `progress.RenderClosed` stays the renderer.

**Tech Stack:** Go (stdlib + `gopkg.in/yaml.v3`), Markdown skills, hook JSON.

**Spec:** `docs/superpowers/specs/2026-10-23-progress-hook-design.md`; T7 merges it into `docs/superpowers/specs/2026-10-20-paraplane-design.md` (§5.1, §7, §7.1, §8, §12a).

## Global Constraints

- No new dependencies; `go.mod`/`go.sum` unchanged.
- Every git call goes through `gitx`'s hardened `run` (same protections as `Changed`: filter drivers, partial clone, 16 MiB output cap).
- Trailer keys, verbatim: `Paraplane-Plan`, `Paraplane-Wave`, `Paraplane-Task`; key case ignored; several `Paraplane-Wave`/`Paraplane-Task` per commit all count; ids match the plan id rule `[A-Za-z][A-Za-z0-9_.-]{0,31}`.
- Subagent prompt marker, verbatim: a line `Paraplane-Task: <plan path from the root> <task id>`; at most 16 such lines read per prompt.
- State file: `<git-dir>/paraplane/closed.json`, JSON `{"version":1,"lastHead":…,"entries":[{"plan","task","head"}],"shown":{<plan>:{"waves","tasks"}}}`; lock `closed.json.lock` created with `O_EXCL`, wait ≤ 2 s, a lock older than 30 s is stale; ≤ 4096 entries (oldest dropped); read through `fsx` (regular file, ≤ 1 MiB); unreadable or invalid = empty state, silently.
- Texts, verbatim: `warning: task <id> commit <hash>: trailers not in the last paragraph`; `warning: task <id> was committed before <running id>`; `error A1 <W>: <file> changed by <T> (commit <hash>) but <T> does not own it`; `error A1 <W>: <file> changed in commit <hash> but no task of <W1, W2> owns it`; `audit ok: <W>, <n> changed file(s), <m> commit(s)` (without commits the old line); additional context `paraplane: progress of <plan> was shown to the user; do not repeat it.`
- Bash trigger words (whole words in the command): `commit`, `merge`, `cherry-pick`, `rebase`, `am`, `pull`, `revert`; nothing else starts git in the Bash hook.
- Hooks fail open: an internal error gives an empty answer; plan with E errors, no git, old git → silent.
- Every README.md change has its README.ru.md twin; each code task updates its own `context/<unit>.md`; a new unit gets `<unit>/CLAUDE.md` and `context/internal/<unit>.md` in Russian.

## Review Focus

- Two subagents of one wave return at the same moment → both tasks end up in `closed.json` (no lost update); test in T2.
- `git reset --hard` to before a recorded task → that task no longer counts; test in T8.
- A stage commit (W1+W2) shows progress once, with both waves done, in `wave` mode; test in T6.
- `progress: task` and a subagent prompt without the marker line → no state write, no message; test in T6.
- A task commit naming a task of a waiting wave → warning, not counted, wave not closed; test in T3.

## Waves

```paraplane
version: 1
gate: "go mod tidy -diff && go vet ./... && GOOS=windows go vet ./... && GOOS=darwin go vet ./... && test -z \"$(gofmt -l .)\" && go test ./... && go test -race ./internal/..."
waves:
  - id: W1
    tasks:
      - id: T1
        title: "gitx: PlanCommits, wave commit files, HEAD helpers"
        owns: ["internal/gitx/**", context/internal/gitx.md]
        verify: "go test ./internal/gitx/"
      - id: T2
        title: "state: closed.json with lock"
        owns: ["internal/state/**", context/internal/state.md]
        verify: "go test -race ./internal/state/"
      - id: T3
        title: "progress: RenderFrom with committed tasks"
        owns: ["internal/progress/**", context/internal/progress.md]
        verify: "go test ./internal/progress/"
      - id: T4
        title: "rules: audit of wave commits"
        owns: [internal/rules/audit.go, internal/rules/audit_test.go, context/internal/rules.md]
        verify: "go test ./internal/rules/"
      - id: T7
        title: "rules text, docs, main spec, root CLAUDE.md"
        owns: ["skills/**", README.md, README.ru.md, hooks/README.md, hooks/README.ru.md, docs/reference.md, docs/reference.ru.md, docs/superpowers/specs/2026-10-20-paraplane-design.md, CLAUDE.md]
        reads: [docs/superpowers/specs/2026-10-23-progress-hook-design.md]
        verify: "go test ./internal/plugintest/"
  - id: W2
    tasks:
      - id: T8
        title: "track: done sets and rendering for a plan"
        owns: ["internal/track/**", context/internal/track.md]
        reads: [internal/gitx/gitx.go, internal/state/state.go, internal/progress/progress.go]
        depends: [T1, T2, T3]
        verify: "go test ./internal/track/"
  - id: W3
    tasks:
      - id: T5
        title: "CLI: progress via track, audit of wave commits"
        owns: ["cmd/paraplane/**", hooks/hooks.json, context/cmd/paraplane.md]
        reads: [internal/track/track.go, internal/rules/audit.go]
        depends: [T8, T4]
        verify: "go test ./cmd/paraplane/"
      - id: T6
        title: "hooks: progress hook for Agent and Bash, Codex, digest"
        owns: ["internal/hooks/**", internal/gitx/plans.go, internal/gitx/plans_test.go, context/internal/hooks.md]
        reads: [internal/track/track.go]
        depends: [T8]
        verify: "go test ./internal/hooks/"
```

---

### Task T1: gitx

**Files:** Modify `internal/gitx/gitx.go`, `internal/gitx/gitx_test.go` (and callers in `fallback_test.go`, `hardening_test.go`), `context/internal/gitx.md`.

**Interfaces — Produces:**
- `type Commits struct { Waves, Tasks []string; Lost []LostTrailer }`; `LostTrailer` gains `Kind string` (`"wave"`/`"task"`) and `ID string`; the field `Wave` stays (set for kind `wave`) so the current caller compiles.
- `func PlanCommits(root, planPath string) (Commits, error)`; `WaveCommits(root, planPath) (waves []string, lost []LostTrailer, err error)` stays as a thin wrapper (its caller `cmd/paraplane/progress.go` moves to `track` only in T5, and the W1 gate must build); `git log -i --grep=^paraplane-wave: --grep=^paraplane-task:`; Waves/Tasks in order of first appearance, deduplicated; Lost once per id, only for ids not counted.
- `type CommitFiles struct { Hash string; Waves, Tasks, Files []string }`
- `func PlanCommitFiles(root, planPath string) ([]CommitFiles, error)` — every commit with `Paraplane-Plan` = planPath and at least one Wave or Task trailer, oldest first, `--name-only --no-renames`, merges contribute no files.
- `func Head(root string) (string, error)` (full hash; `""`, nil for a repository without commits); `func IsAncestor(root, a, b string) (bool, error)`; `func GitDir(root string) (string, error)` (absolute).

- [x] **Step 1: Failing tests** — `TestPlanCommitsTasks`: commits `Paraplane-Task: T1` / `Paraplane-Task: T2` + `Paraplane-Task: T3` in one commit / a task of another plan / `Paraplane-Task: 1bad` → Tasks `[T1 T2 T3]`; a task line after a blank line before `Co-Authored-By` → `Lost` with `Kind:"task"`. Existing `WaveCommits` tests stay green (wrapper). `TestPlanCommitFiles`: a task commit touching `a.go`, a stage commit (W1+W2) touching `b.go c.go`, an unrelated commit → two entries, hashes ≥ 7 chars, files sorted. `TestHeadAndAncestor`: empty repo → `""`; after two commits `IsAncestor(first, second)` true, reversed false. `TestGitDir` returns an absolute existing directory.
- [x] **Step 2: Run** `go test ./internal/gitx/` — FAIL (undefined).
- [x] **Step 3: Implement** the functions above; `paraplaneLines` returns waves and tasks.
- [x] **Step 4: Run** `go test ./internal/gitx/` — PASS; update `context/internal/gitx.md`.

### Task T2: state

**Files:** Create `internal/state/state.go`, `internal/state/state_test.go`, `internal/state/CLAUDE.md`, `context/internal/state.md`.

**Interfaces — Produces:**
- `type Entry struct { Plan, Task, Head string }`; `type Shown struct { Waves, Tasks int }`
- `type State struct { LastHead string; Entries []Entry; Shown map[string]Shown }`
- `func Load(gitDir string) State` — never fails; missing/invalid/irregular file → zero State.
- `func Update(gitDir string, f func(*State)) error` — lock, load, `f`, cap 4096 entries (drop oldest), atomic write (`closed.json.tmp` + rename), unlock; `ErrBusy` when the lock is not taken within 2 s.
- `func (s State) Closed(plan string, valid func(head string) bool) []string` — task ids of `plan` whose `Head` passes `valid`, deduplicated, in entry order.
- `var ErrBusy = errors.New("paraplane state is busy")`

- [x] **Step 1: Failing tests** — `TestLoadMissingAndBroken` (no dir, `{`, a symlink, a FIFO → zero State); `TestUpdateRoundTrip`; `TestUpdateConcurrent`: 20 goroutines each `Update` adding one entry → 20 entries; `TestStaleLock`: a lock file with mtime 31 s ago → `Update` succeeds; `TestBusy`: fresh lock held → `ErrBusy` after ≈2 s (use an injectable `var lockWait = 2*time.Second` set to 100 ms in the test); `TestCapEntries`: 4100 entries → 4096, the first four dropped; `TestClosedValid`: `valid` rejecting one head drops its entry.
- [x] **Step 2: Run** `go test ./internal/state/` — FAIL.
- [x] **Step 3: Implement**; read via `fsx` (see `internal/fsx`), create `<gitDir>/paraplane/` with 0o755.
- [x] **Step 4: Run** `go test -race ./internal/state/` — PASS; write `CLAUDE.md` and `context/internal/state.md` (Russian, as the other units).

### Task T3: progress

**Files:** Modify `internal/progress/progress.go`, `internal/progress/progress_test.go`, `context/internal/progress.md`.

**Interfaces — Produces:** `type Input struct { CommittedWaves, CommittedTasks, Closed []string }`; `func RenderFrom(p *plan.Plan, name string, in Input) string`. `RenderClosed(p, name, done, closed)` = `RenderFrom(p, name, Input{CommittedWaves: done, Closed: closed})`. A task is done if in `CommittedTasks` or `Closed`; warning for a committed task of a waiting wave `warning: task <id> was committed before <running id>`; for `Closed` the existing `--done` warnings.

- [x] **Step 1: Failing tests** with `mk(3, 3, 2)`: `TestRenderFromCommittedTasks` — CommittedTasks `T1,T2,T3` → W1 `✓ done`, W2 `▶ running`; CommittedTasks `T7` (W3 waiting) + W1 done → `warning: task T7 was committed before W2`, `3/8 tasks`; `TestRenderFromMixed` — CommittedTasks `T4`, Closed `T5`, CommittedWaves `W1` → W2 row `🟩🟩🟨`, `5/8 tasks`.
- [x] **Step 2: Run** `go test ./internal/progress/` — FAIL.
- [x] **Step 3: Implement** `RenderFrom` (move `RenderClosed`'s body, add committed tasks).
- [x] **Step 4: Run** `go test ./internal/progress/` — PASS (old tests unchanged); update the context doc.

### Task T4: rules — audit of wave commits

**Files:** Modify `internal/rules/audit.go`, create `internal/rules/audit_test.go`, modify `context/internal/rules.md`.

**Interfaces — Produces:** `type CommitFiles struct { Hash string; Waves, Tasks, Files []string }` (same shape as gitx's; the CLI converts); `func AuditCommits(p *plan.Plan, waveIDs []string, changed []string, commits []CommitFiles, planPath string, ignore []string) ([]Finding, error)` (several ids = a stage, as `AuditWaves` since v1.1.2; `Where` = ids joined by `,`). Relevant commits: a Task of a listed wave, or a listed wave among Waves. Task commit: each file in the owns of that task (any named task of this wave, for several); wave/stage commit: in the owns of any task of any named wave. Same file+commit reported once. `AuditWaves` = `AuditCommits(…, nil, …)`; the working-tree part keeps its v1.1.2 texts.

- [x] **Step 1: Failing tests** (plan W1: T1 `a/**`, T2 `b/**`; W2: T3 `c/**`): task commit T1 with `b/x` → `error A1 W1: b/x changed by T1 (commit abc1234) but T1 does not own it`; stage commit W1+W2 with `a/x c/x` → no finding for W1 nor W2; stage commit with `d/x` → `error A1 W1: d/x changed in commit abc1234 but no task of W1, W2 owns it`; commit of another wave only → ignored; plan file and `audit_ignore` exempt.
- [x] **Step 2: Run** `go test ./internal/rules/` — FAIL.
- [x] **Step 3: Implement** `AuditCommits`.
- [x] **Step 4: Run** `go test ./internal/rules/` — PASS; update the context doc.

### Task T7: rules text, docs, main spec

**Files:** `skills/rules/references/execution.md`, `skills/rules/SKILL.md`, `skills/rules/references/codex.md`, `skills/progress/SKILL.md`, `skills/README*`, `README*`, `hooks/README*`, `docs/reference*`, main spec, root `CLAUDE.md` (units list: `internal/state`, `internal/track`).

- [x] **Step 1: execution.md** — subagent prompt carries `Paraplane-Task: <plan> <id>`; commit schemes (wave / stage / task; task commits by the coordinator one by one after review, with `Paraplane-Task`); Claude Code: the hook shows progress, do not repeat it; Codex: show it yourself; audit covers wave commits; new warnings and A1 texts verbatim.
- [x] **Step 2: SKILL.md, codex.md, skills/progress** (state counts; `--done` still accepted), **README*, hooks/README*** (PostToolUse matcher `Write|Edit|Bash|Agent|Task`, what it shows), **docs/reference*** (sources of done-ness, state file, audit of commits), **main spec** (fold the design doc into §5.1, §7, §7.1, §8, §12a), root `CLAUDE.md` units.
- [x] **Step 3: Run** `go test ./internal/plugintest/` — PASS.

### Task T8: track

**Files:** Create `internal/track/track.go`, `internal/track/track_test.go`, `internal/track/CLAUDE.md`, `context/internal/track.md`.

**Interfaces:**
- Consumes: `gitx.PlanCommits`, `gitx.GitDir`, `gitx.Head`, `gitx.IsAncestor` (T1); `state.Load`, `State.Closed` (T2); `progress.RenderFrom`, `progress.Input` (T3).
- Produces: `type Status struct { Text string; Waves, Tasks int }` (Text ends with the lost-trailer warnings, verbatim from the spec); `func PlanStatus(root, planRel string, p *plan.Plan, extra []string) (Status, error)` — committed waves/tasks from git, closed = valid state entries ∪ `extra`; `Waves`/`Tasks` = number of done waves / done tasks by the §4 rule.

- [x] **Step 1: Failing tests** in a temp repo: task commit T1 + state entry T2 (head = HEAD) of a 2-task W1 → W1 done, `Waves 1`; state entry recorded at a head then `git reset --hard HEAD~1` → entry not counted; `extra` adds a task; lost task trailer → text ends with `warning: task T1 commit <hash>: trailers not in the last paragraph`.
- [x] **Step 2: Run** `go test ./internal/track/` — FAIL.
- [x] **Step 3: Implement** `PlanStatus`.
- [x] **Step 4: Run** `go test ./internal/track/` — PASS; write `CLAUDE.md` and the context doc.

### Task T5: CLI

**Files:** Modify `cmd/paraplane/progress.go`, `cmd/paraplane/audit.go`, `cmd/paraplane/main_test.go`, `hooks/hooks.json`, `context/cmd/paraplane.md`.

**Interfaces — Consumes:** `track.PlanStatus` (T8); `gitx.PlanCommitFiles` (T1); `rules.AuditCommits`, `rules.CommitFiles` (T4).

- [x] **Step 1: Failing tests** — `TestProgressTaskCommits`: repo with `validPlan`, commit with `Paraplane-Task: T1` → W1 `✓ done`; `TestProgressState`: a state entry for T1 written via `state.Update` into the repo's git dir → W1 `✓ done` without `--done`; `TestAuditCommits`: a committed T1 file outside its owns → `error A1 W1: … changed by T1 (commit …)`, exit 1; a clean task commit plus one uncommitted owned file → `audit ok: W1, 1 changed file(s), 1 commit(s)`.
- [x] **Step 2: Run** `go test ./cmd/paraplane/ -run 'Progress|Audit'` — FAIL.
- [x] **Step 3: Implement** `runProgress` via `track.PlanStatus(root, rel, p, closed)`; `runAudit` via `PlanCommitFiles` + `AuditCommits` with the wave list `W1,W2` as today, new summary line; `hooks/hooks.json` PostToolUse matcher `Write|Edit` → `Write|Edit|Bash|Agent|Task` and the `claudeHooks` constant of `TestClaudeHooksUnchanged` with it.
- [x] **Step 4: Run** `go test ./cmd/paraplane/` — PASS; update the context doc.

### Task T6: hooks

**Files:** Modify `internal/hooks/post_tool_use.go`, create `internal/hooks/progress_hook.go` and `internal/hooks/progress_hook_test.go`, modify `internal/hooks/codex_post.go`, `internal/hooks/skill.go`, `internal/hooks/session_start.go`, goldens as needed, `context/internal/hooks.md` (`hooks/hooks.json` is T5's).

**Interfaces — Consumes:** `track.PlanStatus` (T8), `state.Update`/`state.Load` (T2), `gitx.Head`/`gitx.IsAncestor`/`gitx.GitDir` (T1).
**Produces:** `func progressAfterAgent(env Env, in Input) ([]byte, error)`, `func progressAfterBash(env Env, in Input) ([]byte, error)`; `PostToolUse` dispatches `Agent`/`Task` and (Claude) `Bash` to them; Codex `codexPost` calls `progressAfterBash` after its plan checks and merges the answers (a block wins).

Rules: spec §7 (markers, trigger words, `lastHead`, `shown`, `wave`/`task` thresholds, ≤ 8 plans, outputs joined by a blank line). Claude answer = `systemMessage` + `additionalContext` `paraplane: progress of <plan> was shown to the user; do not repeat it.`; Codex answer = `additionalContext` with the progress text and `Show this progress to the user in a code block.` Digest/`sessionTask`: coordinator puts `Paraplane-Task: <plan> <id>` into each subagent prompt; for Claude Code the hook shows progress.

- [x] **Step 1: Failing tests** — `TestProgressAfterAgentTaskMode` (marker for T1 of 2-task W1 → `systemMessage` with `1/2`; same call again → nil); `TestProgressAfterAgentWaveMode` (T1 → nil, then T2 → `systemMessage` with W1 `✓ done`); `TestProgressAfterAgentNoMarker` → nil, no `closed.json`; `TestProgressAfterBashNoTrigger` (`ls`) → nil and git not run (git missing from PATH must not matter); `TestProgressAfterBashStageCommit` (commit with W1+W2 trailers, wave mode) → one message with both done; `TestProgressCodexContext` → `additionalContext`, no `systemMessage`.
- [x] **Step 2: Run** `go test ./internal/hooks/` — FAIL.
- [x] **Step 3: Implement** `progress_hook.go`, dispatch, Codex merge, digest text.
- [x] **Step 4: Run** `go test ./internal/hooks/` — PASS; update the context doc.

## After the plan

- Release as `v1.2.0` after the user says so.
