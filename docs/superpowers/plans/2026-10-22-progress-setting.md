# paraplane — progress setting, `--done` and /paraplane:progress Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. The paraplane execution rules (`skills/rules/references/execution.md`) take precedence: tasks of a wave run in parallel, each only in its `owns`, subagents never commit, golden files are edited by hand. Trailers: `Paraplane-Plan: docs/superpowers/plans/2026-10-22-progress-setting.md`, `Paraplane-Wave: <id>`.

**Goal:** the user picks when progress is shown (`progress: wave | task` in `.paraplane/config.yaml`), closed but uncommitted tasks count as done via `paraplane progress --done <ids> <file>`, and `/paraplane:progress` shows progress on demand.

**Architecture:** the plugin stays stateless: committed waves come from git trailers, closed uncommitted tasks from the coordinator through `--done`. The mode reaches the coordinator in the execution digest the hooks already inject. `progress.Render` keeps its signature; a new `RenderClosed` carries the closed tasks, so the CLI can switch over in a later wave without breaking the build.

**Tech Stack:** Go (stdlib + existing `gopkg.in/yaml.v3`), Markdown skills.

**Spec:** `docs/superpowers/specs/2026-10-22-progress-setting-design.md`; T5 merges it into `docs/superpowers/specs/2026-10-20-paraplane-design.md` (§7, §7.1, §8, §9, §10).

## Global Constraints

- No new dependencies; `go.mod`/`go.sum` unchanged.
- Config key `progress`: `wave` (default) or `task`; anything else → default plus a `Problems` entry, like the other keys.
- `--done` value: task ids joined by `,` with no spaces, each matching `[A-Za-z][A-Za-z0-9_.-]*` (≤ 32 runes, the plan id rule); flag before the file; an empty list, a bad id or the flag twice → usage, exit 2.
- Warning texts, verbatim: `warning: --done <id> is not a task of the plan`, `warning: --done <id> is in waiting wave <W>`; they do not change the exit code.
- Squares: closed tasks of the running wave are 🟩 (not a new colour), the rest 🟨, in task order.
- Every README.md change has its README.ru.md twin (English primary).
- Each code task updates its own `context/<unit>.md`.

## Review Focus

- `--done` lists every task of the running wave → that wave is `done`, the next one `running`; test in T2.
- `--done` naming a task of an already committed wave → counted once, no warning; test in T2.
- `--done` with duplicate ids (`T1,T1`) → counted once; test in T2.
- `paraplane progress --done T1,T2 x.md` is auto-approved, `--done 'T1 T2'`, `--done T1;rm` and `--done ../x` are not; test in T4.
- An invalid `progress` value in the config still lets every other key apply; test in T1.

## Waves

```paraplane
version: 1
gate: "go mod tidy -diff && go vet ./... && GOOS=windows go vet ./... && GOOS=darwin go vet ./... && test -z \"$(gofmt -l .)\" && go test ./... && go test -race ./internal/..."
waves:
  - id: W1
    tasks:
      - id: T1
        title: "config: progress key"
        owns: ["internal/config/**", context/internal/config.md]
        verify: "go test ./internal/config/"
      - id: T2
        title: "progress: RenderClosed"
        owns: ["internal/progress/**", context/internal/progress.md]
        verify: "go test ./internal/progress/"
      - id: T5
        title: "skills, docs, main spec"
        owns: ["skills/**", README.md, README.ru.md, hooks/README.md, hooks/README.ru.md, docs/reference.md, docs/reference.ru.md, docs/superpowers/specs/2026-10-20-paraplane-design.md]
        reads: [docs/superpowers/specs/2026-10-22-progress-setting-design.md]
        verify: "go test ./internal/plugintest/"
  - id: W2
    tasks:
      - id: T3
        title: "CLI: progress --done"
        owns: ["cmd/paraplane/**", context/cmd/paraplane.md]
        reads: [internal/progress/progress.go]
        depends: [T2]
        verify: "go test ./cmd/paraplane/"
      - id: T4
        title: "hooks: digest by mode, approval of --done"
        owns: ["internal/hooks/**", context/internal/hooks.md]
        reads: [internal/config/config.go]
        depends: [T1]
        verify: "go test ./internal/hooks/"
```

---

### Task T1: config key `progress`

**Files:** Modify `internal/config/config.go`, `internal/config/config_test.go`, `context/internal/config.md`.

**Interfaces:**
- Produces: `Config.Progress string` — `"wave"` or `"task"`; `Default().Progress == "wave"`; `"progress"` in `knownKeys`.

- [x] **Step 1: Failing tests** in `config_test.go`: `TestDefault` asserts `Progress == "wave"`; new `TestLoadProgress` table — `progress: task` → `"task"`, no error; `progress: wave` → `"wave"`; `progress: daily` and `progress: [x]` → `"wave"` and `Load` returns `Problems` containing `progress`, while `max_parallel: 3` in the same file still applies.
- [x] **Step 2: Run** `go test ./internal/config/` — FAIL (`Progress` undefined).
- [x] **Step 3: Implement** a `case "progress":` in the key switch next to `mode` (decode a string, accept only `wave`/`task`, else `errors.New("must be wave or task")`).
- [x] **Step 4: Run** `go test ./internal/config/` — PASS. Update `context/internal/config.md` (the key and its default).

### Task T2: `progress.RenderClosed`

**Files:** Modify `internal/progress/progress.go`, `internal/progress/progress_test.go`, `context/internal/progress.md`.

**Interfaces:**
- Produces: `func RenderClosed(p *plan.Plan, name string, done, closed []string) string` — `done`: wave ids with a commit (as today); `closed`: task ids from `--done`. `Render(p, name, done)` stays and returns `RenderClosed(p, name, done, nil)`.

Rules (spec §5): a wave is closed when it has a commit or all its tasks are in `closed`; `done` = closed and every earlier wave `done`; first non-`done` is `running`. Done tasks = tasks of `done` waves + tasks of the running wave in `closed`. Header `P` and bar use that count; the running row's `d/n` counts them and draws 🟩 for them in task order. Ids in `closed` not in the plan → `warning: --done <id> is not a task of the plan`; in a `waiting` wave → `warning: --done <id> is in waiting wave <W>` (ids pass through `textx.Line` like wave ids; each id warned once, in `closed` order, after the existing warnings, each line `capLine`d).

- [x] **Step 1: Failing tests** with `mk(3, 3, 2)` (W1 T1–T3, W2 T4–T6, W3 T7–T8):
  - `TestRenderClosedRunning`: `RenderClosed(p, "p.md", []string{"W1"}, []string{"T4", "T6"})` — header ends `wave 2/3 · 62%`; line 2 is 25 `█` + 15 `░` + `  5/8 tasks`; W2 row contains `🟩🟨🟩` and `2/3` and `▶ running`.
  - `TestRenderClosedWholeWave`: `closed = T4,T5,T6`, done `W1` → W2 `✓ done`, W3 `▶ running`, `6/8 tasks`.
  - `TestRenderClosedWarnings`: `closed = X9,T7` → output ends with `warning: --done X9 is not a task of the plan\nwarning: --done T7 is in waiting wave W3\n`.
  - `TestRenderClosedDedup`: `closed = T1,T4,T4` with done `W1` → `4/8 tasks`, no warning.
  - `TestRenderClosedNil`: for every existing test case `RenderClosed(..., nil) == Render(...)`.
- [x] **Step 2: Run** `go test ./internal/progress/` — FAIL (`RenderClosed` undefined).
- [x] **Step 3: Implement** `RenderClosed`; move the body of `Render` into it.
- [x] **Step 4: Run** `go test ./internal/progress/` — PASS (old tests unchanged). Update `context/internal/progress.md`.

### Task T3: CLI `progress [--done <ids>] <file>`

**Files:** Modify `cmd/paraplane/progress.go`, `cmd/paraplane/main.go` (usage line), `cmd/paraplane/main_test.go`, `context/cmd/paraplane.md`.

**Interfaces:**
- Consumes: `progress.RenderClosed(p, name, done, closed []string) string` (T2).
- Produces: usage line `  progress [--done <ids>] <file>  show the execution progress of a plan` (column aligned with the others).

- [x] **Step 1: Failing tests** in `TestProgressCLI` (repo with `validPlan`: W1 T1, W2 T2; no commits): `progress --done T1 docs/plan.md` → code 0, W1 line `✓ done`, W2 `▶ running`; new `TestProgressDoneArgs`: `progress --done "" docs/plan.md`, `--done "T1 T2"`, `--done 1T`, `--done T1 --done T2 docs/plan.md`, `progress docs/plan.md --done T1`, `progress --done` → code 2 and usage on stderr.
- [x] **Step 2: Run** `go test ./cmd/paraplane/ -run Progress` — FAIL.
- [x] **Step 3: Implement** argument parsing in `runProgress` (optional leading `--done <ids>`, split on `,`, validate each id with the plan id rule; exactly one file left) and call `RenderClosed`.
- [x] **Step 4: Run** `go test ./cmd/paraplane/` — PASS. Update `context/cmd/paraplane.md`.

### Task T4: hooks — digest by mode, approval of `--done`

**Files:** Modify `internal/hooks/skill.go`, `internal/hooks/codex_prompt.go`, `internal/hooks/bash.go`, `internal/hooks/base_test.go`, `internal/hooks/codex_test.go`, `internal/hooks/testdata/base_skill_exec.golden.json`, `internal/hooks/testdata/codex_prompt_exec.golden.json`, `context/internal/hooks.md`.

**Interfaces:**
- Consumes: `Config.Progress` (T1).
- Produces: `func execDigest(mode string) string` — `ExecDigest` without its old progress clause (`and shows \`paraplane progress <plan>\` to the user`) plus the mode sentence; `skill` and `PromptSubmit` emit `execDigest(env.Config.Progress)` where they emitted `ExecDigest` (the constant stays as the marker `skillDigest` returns).

Mode sentences, verbatim:
- `wave`: ``Show the output of `paraplane progress <plan>` to the user after each closed wave (audit and gate passed); while the wave is not committed yet, pass all its tasks: `paraplane progress --done <ids> <plan>`.``
- `task`: the `wave` sentence, then `` After each closed task (its subagent returned and its review passed) run `paraplane progress --done <closed, uncommitted tasks of the wave> <plan>` and show the output.``

`bashOwnCLI`: the `progress` alternative becomes `progress( --done [A-Za-z][A-Za-z0-9_.-]*(,[A-Za-z][A-Za-z0-9_.-]*)*)? <path>`.

- [x] **Step 1: Failing tests:** `base_test.go` allow list gets `paraplane progress --done T1,T2.a docs/plans/a.md`, deny list gets `paraplane progress --done T1, a.md`, `paraplane progress --done 'T1 T2' a.md`, `paraplane progress --done T1;rm a.md`, `paraplane progress --done ../x a.md`; Codex permission test (codex_test.go, the `cli + …` list) gets `cli + "progress --done T1 docs/p.md"`; new `TestExecDigestMode`: with `Progress: "task"` the skill hook output contains `After each closed task`, with `"wave"` it does not and contains `after each closed wave`; for Codex the commands use `Codex.CLI()`. Update the two golden files by hand to the `wave` text.
- [x] **Step 2: Run** `go test ./internal/hooks/` — FAIL.
- [x] **Step 3: Implement** `execDigest`, use it in `skill` and `PromptSubmit`, widen `bashOwnCLI`.
- [x] **Step 4: Run** `go test ./internal/hooks/` — PASS. Update `context/internal/hooks.md` (digest by mode, approved forms).

### Task T5: skills, docs, main spec

**Files:**
- Create: `skills/progress/SKILL.md`.
- Modify: `skills/rules/SKILL.md` (Executing a plan), `skills/rules/references/execution.md` (step 5 → section «When to show progress»), `skills/rules/references/codex.md`, `skills/rules/examples/plan.md` (if its prose names the old rule), `skills/README.md`, `skills/README.ru.md`, `hooks/README.md`, `hooks/README.ru.md`, `README.md`, `README.ru.md`, `docs/reference.md`, `docs/reference.ru.md`, `docs/superpowers/specs/2026-10-20-paraplane-design.md`.

**Interfaces:**
- Consumes (texts only): the CLI form and warnings from Global Constraints, the mode sentences of T4, the spec's §8 for the skill.

- [x] **Step 1: `skills/progress/SKILL.md`** — frontmatter `name: progress`, a `description` in the style of `skills/check/SKILL.md` ("This skill should be used when the user runs /paraplane:progress (or $paraplane:progress in Codex) to see the execution progress of a plan."), `argument-hint: "[plan file]"`, `disable-model-invocation: true`, `allowed-tools: Bash(paraplane progress *)`; body = spec §8 steps 1–3, plus the Codex absolute-path line copied from `skills/check/SKILL.md`.
- [x] **Step 2: Rules text.** `execution.md`: section «When to show progress» per spec §7 (mode from `progress` in `.paraplane/config.yaml`, default `wave`; the digest names it; `wave` and `task` rules; output always in a code block); the Progress output section documents `--done`, 🟩 for closed tasks and the two warnings. `SKILL.md`, `codex.md`: same rule in one sentence each.
- [x] **Step 3: Docs.** `skills/README*`: a `progress` row in the skills table and `disable-model-invocation` note. `README*`, `docs/reference*`: `progress [--done <ids>] <file>`, the `progress` config key, `/paraplane:progress`, approved forms of the Bash hook. `hooks/README*` (lines on PreToolUse and PermissionRequest approval): add `paraplane progress --done <ids> <path>`. Main spec: fold the design doc into §7 (CLI table), §7.1 (state rule, `--done`, warnings, execution rule), §8 (approval list), §9 (config table), §10 (skills).
- [x] **Step 4: Run** `go test ./internal/plugintest/` — PASS (frontmatter of the new skill, ship list).

## After the plan

- Coordinator: `paraplane progress docs/superpowers/plans/2026-10-22-progress-setting.md` after each wave, per the new rules.
- Release: a minor version (new key, new flag, new skill) — `v1.1.0`, after the user says so.
