---
name: rules
description: This skill should be used whenever Claude writes, revises or reviews an implementation plan — in plan mode before ExitPlanMode, with superpowers:writing-plans, or in any docs/plans file — or when a paraplane hook reports plan errors (E0–E7), warnings (W1–W5) or audit errors (A1). Also loaded as $paraplane:rules in Codex. Defines the paraplane block and the rules that let subagents execute waves in parallel without conflicts.
---
# paraplane rules

Every implementation plan carries exactly one fenced ```` ```paraplane ```` block: a YAML list of
waves, each a set of tasks that run in parallel, one subagent per task. The prose of the plan is for
people; the block is what `paraplane check` validates and what the executor follows.

- **Task** — the work of one subagent. `owns` — paths and globs the task creates, changes or deletes
  (only this task); `reads` — paths whose contents it relies on without changing them; `depends` —
  earlier tasks whose result it needs; `verify` — its own check command.
- **Wave** — tasks executed at the same time. Waves run one after another; each ends with a **gate**,
  the full check run, and the next wave starts only when the gate passes.
- **Hot file** — a shared file almost any task may touch in passing (`go.mod`, lock files,
  migrations), configured by `hot_files` in `.paraplane/config.yaml`.

Minimal shape:

```paraplane
version: 1
gate: "go vet ./... && go test ./..."
waves:
  - id: W1
    tasks:
      - id: T1
        title: "Store package"
        owns: ["internal/store/**"]
        verify: "go test ./internal/store/"
  - id: W2
    tasks:
      - id: T2
        title: "HTTP handlers"
        owns: ["internal/api/**"]
        reads: [internal/store/store.go]
        depends: [T1]
        verify: "go test ./internal/api/"
```

Schema, limits, globs, overlap rules: `references/format.md`. A complete plan: `examples/plan.md`.

## Writing a plan

1. **List the file changes step by step.** For every step of the work, write down which files it
   creates, changes or deletes, including the incidental ones: route tables, `main.go`, `index.ts`,
   DI containers, generated code, `go.mod` for a new dependency, migrations.
2. **Assign `owns`.** Give every file to exactly one task. Move shared files and hot files into
   separate integrator tasks (one task adds all new dependencies to `go.mod`/`go.sum`; one task
   registers all new routes) instead of letting several tasks touch them.
3. **Set `depends` and `reads` from real contracts.** A task depends on another only when it needs
   that task's result (a type, a function, a schema). List in `reads` the files the task relies on
   (interfaces, schemas) without changing them.
4. **Lay out waves as soon as possible.** Put each task in the first wave after the last task it
   depends on or conflicts with. Reorder the original steps freely: an independent "step 3" goes to
   W1. The original numbering is not an order.
5. **Give every wave a gate** — the full run of the project's checks (build, vet/lint, all tests),
   not only the new tests, so the next wave cannot break the previous one. Set `gate` once at the top
   of the block, and per wave only where it differs.
6. **Run `paraplane check --suggest <file>`** and fix every error. On W3 it prints `Shorter layout:`
   and a ready block; adopt it and update the prose to match.
7. **When any wave has 2 or more tasks, run the agent `paraplane:conflict-auditor`** (Codex: see `references/codex.md`) on the plan. It
   finds conflicts the block cannot show (shared files outside `owns`, registration points, API breaks
   inside a wave, numbered migrations). Fix the plan, then run `paraplane check` again until
   `plan ok`.

### Quote globs

Quote every pattern that contains any of `* ? [ ] { } ,` or starts with `!` or `&`: `owns: ["internal/config/**", "cmd/*.go"]`. Unquoted, YAML reads `*` as an alias, `?` as a key,
`&` as an anchor, and `[]{},` break the list (the error hints `quote glob patterns`). Plain literal paths (`internal/plan/plan.go`) need no quotes.

### Name hot files literally

A task touches hot pattern `H` when an entry `p` of its `owns` equals `H`, or `p` is a literal that
`H` matches (`web/package.json` touches `**/package.json`), or `H` is a literal that `p` matches
(`**` touches `go.mod`). Any other pattern does not touch it: `internal/**` does not match `go.mod`,
and a pattern without `**` (`web/*.json`) never touches the hot pattern `**/package.json`, so
`paraplane check` cannot see such a clash. To change `go.mod`, a lock file or a migration, name it
literally in `owns` (`go.mod`, `web/package.json`, `migrations/0003_add_notes.sql`). Two tasks of one
wave touching the same hot pattern conflict even through different files: `migrations/0003_a.sql`
and `migrations/0003_b.sql` clash on the number.

A literal path is a file; to own a directory write `dir/**`.

## Findings and fixes

`paraplane check` prints `warning: config: …` lines for problems in `.paraplane/config.yaml` (if
any), then one line per finding (`error E2 W1: owns of T1 (internal/a.go) and T2 (internal/*.go) may overlap; …`), then
`plan ok: 3 waves, 7 tasks`, `plan ok with 2 warning(s): 3 waves, 7 tasks` or
`plan invalid: 2 error(s), 1 warning(s)`. Errors (E) make the plan unfit; warnings (W) do not.

Check order: E0 stops everything else. With E1 or E3 only E7, W1, W2, W4 and W5 are also checked
(overlaps and layout need a whole structure), so more findings may appear after these are fixed.
W3 is checked only when there are no errors at all.

| Code | Meaning | Fix |
|---|---|---|
| E0 | No ```` ```paraplane ```` block, more than one, YAML does not parse, or a limit is exceeded | Add exactly one top-level block (not indented by 4+ spaces, not inside a longer fence); quote globs; split oversized plans |
| E1 | Structure: no waves, empty wave, missing `id`/`title`, duplicate `id`, `depends` on an unknown task (or on a wave id), bad path or pattern, unknown key or wrong type, `version` not 1 | Fix the named field; paths are relative, `/`-separated, without `..` |
| E2 | `owns` of two tasks of one wave overlap | Give the file to one task, narrow the patterns, or move one task to a later wave with `depends` |
| E3 | `depends` points to a task of the same or a later wave (including a cycle) | Move the dependent task to a later wave, or drop the false dependency |
| E4 | `reads` of a task overlaps `owns` of another task of its wave | Move the reader to a later wave with `depends` on the owner, or drop the read |
| E5 | Two tasks of one wave touch one hot pattern | Move all changes to that hot file into one integrator task |
| E6 | A task changes what a task of an earlier wave owns without depending on it (directly or transitively) | Add `depends` on the earlier owner (a deliberate continuation), or move the change into that task |
| E7 | A wave has no gate | Add `gate` to the wave or to the plan (or set `default_gate` in the config) |
| W1 | A task has no `verify` | Add a focused check command for the task |
| W2 | A wave has more tasks than `max_parallel` | Split the wave, or accept the warning |
| W3 | A shorter layout exists | Adopt the block printed after `Shorter layout:` by `paraplane check --suggest` |
| W4 | A task has an empty `owns` | List its files, or fold the task into another |
| W5 | A literal in `owns` is an existing directory (CLI and hooks only) | Write `dir/**` |
| A1 | After a wave a changed file is owned by no task of that wave (`error A1 W2: internal/x.go changed but no task of W2 owns it`) | Revert the stray change, or, when it was needed, add the file to the right task's `owns` and re-check the plan; fix before the gate |

"May overlap" messages can be false alarms: narrow the patterns. Overlap and hot files ignore case
(`README.md` and `readme.md` may overlap); `audit` matches changed paths exactly. NFC and NFD spellings
(macOS) are not unified; give such files to one task.

## Executing a plan

Follow `references/execution.md`. In short: dispatch all tasks of a wave in one message, each subagent
limited to its `owns`; subagents never commit; after the wave run `paraplane audit <plan> <wave>`,
then the gate; fix until both pass; the coordinator makes one commit per wave with the trailers
`Paraplane-Plan` and `Paraplane-Wave`, then shows the output of `paraplane progress <plan>` to the user
in a code block.

## Precedence

For a plan with a ```` ```paraplane ```` block the paraplane execution rules take precedence over the
instructions of any execution skill (`superpowers:subagent-driven-development`,
`superpowers:executing-plans` and the like). Instructions such as "never dispatch implementers in
parallel" or "the implementer commits" do not apply to such a plan: dispatch the tasks of a wave in
parallel, and let only the coordinator commit, once per wave after a green gate. Their other asks
(reviews, verification, reporting) still apply.

## Additional resources

- **`references/format.md`** — block schema, limits, ids, paths and globs, quoting, overlap of
  patterns, hot files, fence pitfalls.
- **`references/execution.md`** — coordinator and subagent rules, audit, gate, commits, progress.
- **`references/codex.md`** — Codex CLI: where the plan is checked, the CLI path, the audit without agents.
- **`examples/plan.md`** — a complete plan (prose and a three-wave block) that passes
  `paraplane check`.
