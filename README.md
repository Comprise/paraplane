# paraplane

**English** | [Русский](README.ru.md)

**A plugin for [Claude Code](https://docs.claude.com/en/docs/claude-code) and Codex CLI that makes every
implementation plan fit for parallel execution by subagents, so the plan runs faster with no loss of quality.**

Parallel subagents break each other's work when two of them edit the same file, or when a later stage relies on
something an earlier one has not finished. paraplane adds rules to every plan and checks them (no LLM, no
guessing):

- the work is split into **tasks**, and each task **owns** its files (`owns`), so no two parallel tasks edit
  the same file;
- tasks are grouped into **waves**: tasks of one wave run in parallel, waves run one after another;
- the order comes from **dependencies**, not from the original numbering: an independent "step 3" can run first;
- each wave ends with a **gate** (the full test run), so a later wave cannot break an earlier one.

paraplane does not run plans itself. It makes sure the plan is safe to run in parallel, and tells Claude how to
run it.

## Installation

**Claude Code:**

```
/plugin marketplace add Comprise/paraplane
/plugin install paraplane@paraplane
```

Without an SSH key for GitHub, set `CLAUDE_CODE_PLUGIN_PREFER_HTTPS=1` first. To update:
`claude plugin update paraplane@paraplane` (or turn on auto-update in `/plugin` → Marketplaces).

**Codex CLI:**

```
codex plugin marketplace add Comprise/paraplane --ref release
```

Then in Codex: `/plugins` → install `paraplane`, and `/hooks` → **trust** its hooks (untrusted hooks do not run).

**Requirements:** Linux, macOS or Windows (amd64 or arm64); Go is not needed. `git` is needed for the wave audit
and progress. On Windows, [Git for Windows](https://git-scm.com/downloads/win) is required. Details:
[reference → Requirements](https://github.com/Comprise/paraplane/blob/main/docs/reference.md#requirements).

## How to use

You do not have to learn anything new: plan as usual. paraplane works in the background.

1. **Make a plan** in any way you like: plan mode, `superpowers:writing-plans`, a plan file in `docs/plans/`,
   or the plugin's own command:

   ```
   /paraplane:create add CSV export to the reports page
   ```

   Claude adds a `paraplane` block to the plan: which tasks there are, which files each one owns, in which wave
   it runs.

2. **paraplane checks the plan.** When Claude leaves plan mode or saves a plan file, the plugin validates the
   block. If tasks of one wave would edit the same file, or a task depends on something that is not done yet,
   Claude cannot finish the plan until it fixes it. To check an existing plan and see if it can be shorter:

   ```
   /paraplane:check docs/plans/2026-10-20-csv-export.md
   ```

3. **Run the plan.** Ask Claude to execute it (for example with `superpowers:subagent-driven-development`).
   The tasks of each wave start in parallel. After a wave, Claude checks that only the owned files changed,
   runs the gate, commits the wave and shows the progress:

   ```
   paraplane · 2026-10-20-csv-export.md                      wave 2/3 · 50%
   ████████████████████░░░░░░░░░░░░░░░░░░░░  3/6 tasks

   W1  🟩🟩🟩  3/3  ✓ done     T1 T2 T3
   W2  🟨🟨    0/2  ▶ running  T4 T5
   W3  ⬜      0/1  · waiting  T6
   ```

   With `progress: task` in the config Claude also shows it after each closed task, before the wave commit.
   To see it at any moment yourself: `/paraplane:progress`.

In Codex the commands are `$paraplane:create`, `$paraplane:check`, `$paraplane:progress` and `$paraplane:rules`.

### What the block looks like

````
```paraplane
version: 1
gate: "go vet ./... && go test ./..."
waves:
  - id: W1
    tasks:
      - id: T1
        title: "CSV writer"
        owns: ["internal/export/**"]
        verify: "go test ./internal/export/"
      - id: T2
        title: "Report query"
        owns: ["internal/reports/query.go"]
        verify: "go test ./internal/reports/"
  - id: W2
    tasks:
      - id: T3
        title: "Export button and handler"
        owns: ["internal/api/export.go", "web/reports/**"]
        reads: [internal/export/csv.go]
        depends: [T1, T2]
        verify: "go test ./internal/api/"
```
````

- `owns` — files the task creates or changes (only this task may touch them);
- `reads` — files the task relies on without changing them;
- `depends` — tasks from earlier waves whose result it needs;
- `verify` — the task's own check; `gate` — the full check after each wave.

You rarely write the block by hand: Claude writes it following the `paraplane:rules` skill.

### What is checked

| Code | Problem |
|---|---|
| E0 | the plan has no `paraplane` block, has several, or the YAML is broken |
| E1 | a structure error: missing id or title, unknown task in `depends`, bad path |
| E2 | two tasks of one wave own the same file |
| E3 | a task depends on a task of the same or a later wave |
| E4 | a task reads a file that another task of its wave changes |
| E5 | two tasks of one wave touch the same shared file (`go.mod`, lock files, migrations) |
| E6 | a task changes an earlier task's file without depending on it |
| E7 | a wave has no gate |
| W1–W5 | warnings: no `verify`, too many tasks in a wave, a shorter layout exists, empty `owns`, a directory without `/**` |
| A1 | after a wave, a file changed that no task of the wave owns |

Errors (E) make the plan unfit; warnings (W) do not. The command line tool does the same by hand:

```
paraplane check [--suggest] <plan>   # validate a plan; --suggest prints a shorter layout
paraplane audit <plan> <wave>        # after a wave: only owned files changed?
paraplane progress [--done <ids>] <plan>  # progress bars; --done: closed, uncommitted tasks
```

## Configuration

Optional: `.paraplane/config.yaml` in the project. The useful keys:

```yaml
mode: block            # or warn: report problems instead of blocking
plan_paths: ["docs/plans/**/*.md"]
default_gate: "make test"
max_parallel: 6
progress: wave         # or task: also show progress after each closed task
```

All keys and their defaults: [reference → Configuration](https://github.com/Comprise/paraplane/blob/main/docs/reference.md#configuration).

## More

- [Reference](https://github.com/Comprise/paraplane/blob/main/docs/reference.md): the block format and limits, all rules, command output, audit security,
  hooks, Codex differences, limitations, troubleshooting, development.
- [Specification](https://github.com/Comprise/paraplane/blob/main/docs/superpowers/specs/2026-10-20-paraplane-design.md)
  (in Russian).

## Author and license

Elia Samoylov. [MIT](LICENSE)
