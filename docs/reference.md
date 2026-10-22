# paraplane reference

**English** | [Русский](reference.ru.md)

The full reference: block format, rules, commands and their output, hooks, configuration, requirements and limitations. Start with the [README](../README.md).

## The `paraplane` block

A plan (Markdown) has exactly one fenced code block with the info string `paraplane`. The rest of the plan is
for people; paraplane does not read it. The content is YAML:

````
```paraplane
version: 1                  # optional; only 1 is supported
gate: "go test ./..."       # optional; default gate for the waves of the plan
waves:
  - id: W1
    gate: "go vet ./... && go test ./..."   # optional; overrides the plan gate
    tasks:
      - id: T1
        title: "Config package"
        owns: ["internal/config/**"]
        reads: []
        depends: []
        verify: "go test ./internal/config/"
      - id: T2
        title: "Plan package"
        owns: ["internal/plan/**"]
  - id: W2
    tasks:
      - id: T3
        title: "Validator"
        owns: ["internal/rules/**"]
        reads: [internal/plan/plan.go]
        depends: [T2]
```
````

- **`owns`** — paths and glob patterns of the files the task creates, changes or deletes. Only this task.
- **`reads`** — paths whose contents the task relies on (interfaces, schemas) without changing them.
- **`depends`** — ids of tasks from earlier waves; these are task dependencies, not wave ids.
- **`verify`** — the task's check command; **`gate`** — the command the executor runs after a wave.
  Wave gate: `wave.gate`, then `plan.gate`, then `default_gate` from the config.
- The block is fenced with backticks or tildes (fence length at least 3). Unknown keys are an error.
- Paths are relative to the project root, `/`-separated, without `..` and a leading `/`. Globs use the
  [doublestar](https://github.com/bmatcuk/doublestar) syntax (`*`, `**`, `?`, `[...]`, `{a,b}`). A literal path
  is a file; a whole directory is written as `dir/**`.
- A pattern containing any of `* ? [ ] { } ,` or starting with `!` or `&` is written in quotes
  (`"**/x.go"`), otherwise YAML reads it as an alias, a key or an anchor. If YAML fails to parse for this
  reason, the check suggests `quote glob patterns`.
- Limits (protection of the hook against a hostile plan): block ≤ 256 KB, ≤ 500 tasks, ≤ 100 paths per task,
  ≤ 2000 paths (`owns` + `reads`) per plan, pattern ≤ 256 characters (runes), ≤ 8 `{}` groups in a pattern and ≤ 256 variants of brace expansion
  (the product of the alternative counts of its `{}` groups).

## Validation rules

Every violation is a line with a code, a place and a text. Errors (E) make the plan unfit, warnings (W) do not.

| Code | Rule |
|---|---|
| E0 | there is no `paraplane` block, there is more than one, YAML does not parse, or a limit is exceeded |
| E1 | structure: no waves; empty wave; no `id`/`title`; duplicate `id`; `depends` on an unknown task; bad path or pattern; unknown key or wrong type; `version` is not 1 |
| E2 | `owns` of two tasks of one wave overlap |
| E3 | `depends` points to a task of the same or a later wave (including a cycle) |
| E4 | `reads` of a task overlaps `owns` of another task of the same wave |
| E5 | two tasks of one wave touch the same hot pattern |
| E6 | a task changes what a task of an earlier wave `owns` but does not depend on it (directly or transitively) |
| E7 | a wave has no gate |
| W1 | a task has no `verify` |
| W2 | a wave has more than `max_parallel` tasks |
| W3 | the layout can be made shorter |
| W4 | a task has an empty `owns` |
| W5 | a literal in `owns` is an existing directory (`dir/**` was meant) |
| A1 | `paraplane audit` only: a changed file of the wave belongs to no task of it |

- E6 is the main "the next stage does not break the previous one" rule: changing someone else's result is
  allowed only as a deliberate continuation (a dependency), so that the executor sees the predecessor's contract.
- **Hot files** (`hot_files`) are a shared resource (`go.mod`, lock files, migrations). A task touches a hot
  pattern if it names a matching file explicitly; a wide pattern (`internal/**`) does not touch a hot file. Two
  tasks of one wave touching the same hot pattern are E5, even if the files differ
  (`migrations/0003_a.sql` and `migrations/0003_b.sql` clash on the number).
- Pattern overlap is computed conservatively: false alarms are possible (the message says "may overlap"),
  misses are not. Literal segments compare case-insensitively (`README.md` and `readme.md` in one wave may
  overlap: on macOS and Windows they are one file by default); hot files compare the same way.
- With E0 errors nothing else is checked; with E1/E3 errors only E7, W1, W2, W4, W5 are checked;
  W3 is reported only when there are no errors at all.

## Commands

In Claude Code:

| Command | What it does |
|---|---|
| `/paraplane:create <task or path to a spec>` | clarify the task, study the code, compose a plan, run `conflict-auditor`, fix, validate with `paraplane check` and save to `plans_dir/YYYY-MM-DD-<slug>.md` |
| `/paraplane:check [path]` | `paraplane check --suggest` plus `conflict-auditor`, edit the plan itself and report the reorderings |
| `/paraplane:progress [path]` | `paraplane progress` on demand: the plan being executed or the newest plan; closed, uncommitted tasks of the session in `--done`; the output in a code block |
| `/paraplane:rules` | all the rules: block format, E/W, how to split and reorder stages, execution rules (Claude loads it by itself) |

Only the user invokes `create`, `check` and `progress`. The `conflict-auditor` agent is started from
these skills when a wave has two or more tasks and looks for what the plan does not show: hidden shared files,
registration points, API breaks inside a wave, migrations. Its report is a list of findings and a verdict,
`clean` or `conflicts`; the plan is edited by the main Claude. When the plan cannot be audited (unreadable file,
zero or several `paraplane` blocks, invalid YAML, a requested wave id the plan does not have) the verdict is `error` with one `input` item: that is not
`clean` — the skill fixes the input and audits again, or reports the reason.

The command-line utility (you can run it by hand; Claude uses it by itself):

```
paraplane check [--suggest] <file>   # validate a plan; - reads stdin
paraplane audit <file> <wave>        # audit the changes of a wave
paraplane progress [--done <ids>] <file>  # execution progress; --done: closed, uncommitted tasks
paraplane hook <session-start|pre-tool-use|post-tool-use> [--agent codex]
paraplane hook <stop|prompt-submit|permission-request> --agent codex   # Codex events: see hooks/README.md
paraplane version
```

**Outside Claude Code.** The plugin's `bin/` is on PATH only for the Bash tool of Claude Code. From a regular
terminal run `<plugin dir>/bin/paraplane …` (in Claude Code this is the directory in the plugin cache, also
`${CLAUDE_PLUGIN_ROOT}`; in Codex it is the plugin directory in the Codex cache) or build the binary from
source: `go build -o /usr/local/bin/paraplane ./cmd/paraplane` (Go required) and call it as `paraplane`.

### `paraplane check`

Prints the violations, then a summary. For example, for two tasks of one wave with `owns` `internal/a.go` and `internal/*.go`:

```
error E2 W1: owns of T1 (internal/a.go) and T2 (internal/*.go) may overlap; narrow the patterns or move a task to another wave
warning W1 T1: task T1 has no verify
warning W1 T2: task T2 has no verify
plan invalid: 1 error(s), 2 warning(s)
```

Summaries: `plan ok: 3 waves, 7 tasks`, `plan ok with 2 warning(s): 3 waves, 7 tasks` or
`plan invalid: 1 error(s), 2 warning(s)`. With `--suggest`, on W3 the line `Shorter layout:` and a ready
`paraplane` block with a shorter layout are printed before the summary (an independent "third" stage moves to
W1). The W3 text is ``warning W3: the layout can be shorter: 2 waves instead of 3 (`paraplane check --suggest` prints it)`` (`1 wave` in the singular). Exit code: 0 — no errors, 1 — there are errors or the file is unreadable, 2 — bad arguments. Config
problems are `warning: config: …` lines before the violations. The plan file must be a regular file (not a
symlink, FIFO or directory: `error: <path> is a directory, not a plan file`) inside the project root and at most
1 MB. All `error:`, `warning:` and result lines go to stdout; only the usage text (exit code 2) goes to stderr.
The shim is the exception: when the binary is missing or not executable, `bin/paraplane` prints
`paraplane: binary … was not found` (or `… is not executable`) to stderr and exits with 1.

### `paraplane audit`

A check **after** a wave is executed and **before** its gate (gate artifacts — coverage, binaries — do not
count). Changed files of the working copy must fall into `owns` of some task of this wave; exceptions are the
plan file itself and the `audit_ignore` patterns. For example, with two stray files:

```
error A1 W2: internal/x.go changed but no task of W2 owns it
error A1 W2: internal/y.go changed but no task of W2 owns it
audit failed: 2 file(s) outside W2
```

Success is `audit ok: W2, 5 changed file(s)`. No git or not a repository, an unknown wave, a plan with E errors —
`error: …` and exit code 1. The audit relies on the execution rule "subagents do not commit, the coordinator
commits after a green gate": then the working copy contains exactly the changes of the current wave. It does
not distinguish tasks within a wave — the gate tests catch that.

**Audit security.** `git status` can run a command set by the repository config, and `paraplane audit` is
approved by the hook without a prompt, therefore:

- before `status`, filter drivers (`filter.*.clean|smudge|process`) are checked; any defined in the `local` or
  `worktree` scope or any other scope except `global` and `command` (including ones pulled in via
  `include`/`includeIf`) is a refusal
  `error: the repository config defines filter drivers; paraplane audit does not run git status here`
  (exit code 1). Only filters from the user's global git config (for example git-lfs) are trusted; git runs with
  `GIT_CONFIG_NOSYSTEM=1`, so the system config is not read at all. Git older than 2.26 has no `--show-scope`:
  then the local and (when `extensions.worktreeConfig` is on) worktree configs are queried separately, and any
  filter there is a refusal;
- on git that does not honor `GIT_NO_LAZY_FETCH` (anything but 2.46+ and the fixes 2.45.1, 2.44.1, 2.43.4, 2.42.2,
  2.41.1, 2.40.2, 2.39.4, or a version that cannot be parsed), `audit` refuses with `error: … partial clone …` in a partial
  clone (`extensions.partialClone` or `remote.*.promisor` set in any scope) or when the repository config sets
  `remote.*.uploadpack`; ordinary repositories work on old git too;
- git output is capped at 16 MiB: more (for example a huge untracked tree) is a refusal `git output too large`;
- every git call runs with `GIT_NO_LAZY_FETCH=1`, `GIT_OPTIONAL_LOCKS=0` (the index is not written, index hooks do
  not fire) and `GIT_CONFIG_NOSYSTEM=1`, without `GIT_DIR`, `GIT_CONFIG` and other repository variables, and with
  `-c core.fsmonitor=false -c core.pager=cat --no-pager -c log.showSignature=false -c gpg.program=false
  -c gpg.ssh.program=false -c gpg.x509.program=false -c core.hooksPath=/dev/null` (no pager, signature programs
  or hooks can start);
- `status` runs with `--no-renames` and `--ignore-submodules=all`, `git log` with `--no-show-signature`.

### `paraplane progress`

The coordinator commits each wave once, after a green gate, with the trailers
`Paraplane-Plan: <plan path from the root>` and `Paraplane-Wave: <wave id>`. `paraplane progress <file>` reads
the block and the commit trailers of the current branch. The plugin keeps no state, so progress survives a
session restart. Example (plan `example.md`, 5 waves, 17 tasks, W1 and W2 done):

```
paraplane · example.md                                    wave 3/5 · 64%
█████████████████████████░░░░░░░░░░░░░░░  11/17 tasks

W1  🟩🟩🟩🟩🟩🟩  6/6  ✓ done     T1 T2 T3 T4 T5 T6
W2  🟩🟩🟩🟩🟩    5/5  ✓ done     T7 T8 T9 T10 T11
W3  🟨🟨          0/2  ▶ running  T12 T13
W4  ⬜⬜          0/2  · waiting  T14 T15
W5  ⬜⬜          0/2  · waiting  T16 T17
```

🟩 — wave done, 🟨 — in progress, ⬜ — waiting. A commit of a wave that follows the running one gives the line
`warning: wave <id> was committed before <running id>`; a commit of a wave that is not in the plan gives
`warning: commit for unknown wave <id>`.

`--done T7,T9` (ids joined by `,`, no spaces, before the file) lists tasks closed but not committed yet. A
wave is closed when it has a commit or all its tasks are in `--done`; closed tasks of the running wave
count as done and are drawn 🟩, so the bar moves before the wave commit. An id that is no task of the plan
gives `warning: --done <id> is not a task of the plan`, a task of a waiting wave
`warning: --done <id> is in waiting wave <W>`; neither is counted. An empty list, a bad id or the flag
twice is a usage error (exit 2).

When the coordinator shows progress depends on the key `progress`: `wave` (default) — after each closed
wave (audit and gate passed), `task` — also after each closed task. The output always goes to the user
in a code block. The user can ask for it at any moment with `/paraplane:progress`.

## Execution rules

The skill `paraplane:rules` outranks the instructions of any execution skill
(`superpowers:subagent-driven-development`, `executing-plans`, etc.): for a plan with a `paraplane` block,
contradicting instructions ("do not run implementers in parallel", "the implementer commits") do not apply.

1. The prompt of a subagent contains its `owns`, `reads` and `verify`; "do not touch files outside `owns`; if
   needed, stop and report".
2. The tasks of a wave are started in one message.
3. After a wave — `paraplane audit <plan> <wave>`, then the gate. On an audit violation or a red gate — fix
   before the next wave starts.
4. Subagents do not commit (parallel commits race for `index.lock` and pick up other tasks' files): the
   coordinator commits once after a green wave gate with the trailers `Paraplane-Plan` and `Paraplane-Wave`.
5. Progress is shown after each closed wave, and with `progress: task` also after each closed task.

## Hooks

The hooks catch a plan in any form: from plan mode, from a skill with `plan` in its name, from a file in `plan_paths`.

| Event | What it does |
|---|---|
| `SessionStart` | adds to the context a line about the rules and `paraplane:rules`; on config problems — a message |
| `PreToolUse` `ExitPlanMode` | validates the plan; E errors (including E0) — **forbids leaving plan mode** and lists the violations (up to 20) with an instruction; only W warnings — adds them to the context |
| `PreToolUse` `Skill` | before an execution skill (the name contains `execut` or `subagent-driven`, does not start with `paraplane:`) reminds of the execution rules and their priority; before a skill with `plan` in its name — the plan-writing rules |
| `PreToolUse` `Bash` | approves without a prompt `paraplane check <path>`, `paraplane audit <path> <wave>`, `paraplane progress [--done <ids>] <path>`, `paraplane version` |
| `PostToolUse` `Write\|Edit` | if the file is in `plan_paths` — validates it (≤ 1 MB). E0 — only the hint `add the block before the plan is final`; E1–E7 — **block** with a reason; only W — a hint, and only for `Write` |

- `Edit` is checked only if the edit touches the `paraplane` block or its keys (`waves:`, `tasks:`, `id:`,
  `title:`, `owns:`, `reads:`, `depends:`, `verify:`, `gate:`) or the new text occurs inside the block: while
  the executor ticks checkboxes, the hook stays silent.
- Hooks always exit with code 0 and write nothing to the repository. A plugin error goes to stderr (visible in
  `claude --debug`), the response is empty: the plugin does not get in the way (fail-open).
- The check in a hook is limited to 5 seconds; if it does not finish — error E0
  `plan is too complex to check in time; simplify brace patterns or split the plan`. The CLI checks without a deadline.
- `mode: warn` in the config replaces the denial and the block with a message of the same text.
- More in [`hooks/README.md`](../hooks/README.md).

## Configuration

The config is optional and lives in `.paraplane/config.yaml`; all fields are optional. An unknown key or a bad
value is a warning, the other fields apply.

| Field | Default | Meaning |
|---|---|---|
| `enabled` | `true` | turn the plugin off in the project |
| `mode` | `block` | `block` or `warn`: whether hooks block (`ExitPlanMode`, PostToolUse) or only report |
| `plan_paths` | `docs/plans/**/*.md`, `docs/superpowers/plans/**/*.md`, `.paraplane/plans/**/*.md` | which files are plans |
| `hot_files` | `go.mod`, `go.sum`, `**/package.json`, `**/package-lock.json`, `**/pnpm-lock.yaml`, `**/yarn.lock`, `**/Cargo.toml`, `**/Cargo.lock`, `**/pyproject.toml`, `**/uv.lock`, `**/poetry.lock`, `**/migrations/**` | hot files |
| `default_gate` | `""` | default gate; one line within the length limit, without control characters (including TAB, NUL, ESC, DEL), U+2028/U+2029 or invisible format characters (category Cf), otherwise ignored with a warning |
| `max_parallel` | `6` | W2 threshold, 1…32 |
| `plans_dir` | `docs/plans` | where `/paraplane:create` saves plans; a relative path without `..` and without the glob characters `* ? [ ] { }`, otherwise ignored with a warning |
| `audit_ignore` | `[]` | patterns of files the audit ignores (generated, caches) |
| `progress` | `wave` | when the coordinator shows progress: `wave` — after each closed wave; `task` — also after each closed task |

The project root is `CLAUDE_PROJECT_DIR`, otherwise the session `cwd`; then the nearest directory upward with
`.git` or `.paraplane/config.yaml`.

## Requirements

- Claude Code (CLI, IDE extension or desktop app) or Codex CLI (see "Codex CLI").
- Linux, macOS or Windows (amd64 or arm64). Go is not needed: the plugin is installed with prebuilt binaries.
- `git` — needed for `paraplane audit` and `paraplane progress`; plan validation (`check`) and hooks work without it.
  - `audit` is tested from git 2.18. In a partial clone it refuses unless git honors `GIT_NO_LAZY_FETCH`: 2.46+,
    or 2.45.1, 2.44.1, 2.43.4, 2.42.2, 2.41.1, 2.40.2, 2.39.4. A distro git that backports the fix under an older
    version number (for example Ubuntu's 2.34.1) is treated as old, so `audit` refuses in partial clones there:
    upgrade git or use a full clone.
  - `progress` needs git 2.22+ (it reads trailers with `%(trailers:key=…)`); older git gives an explicit error.
  - git output over 16 MiB (for example a huge untracked tree) makes `audit` refuse with `git output too large`.
- **Windows:** [Git for Windows](https://git-scm.com/downloads/win) is required — without Git Bash the plugin
  hooks do not run. If Git Bash is not in the standard place, set the path in `settings.json`:

  ```json
  { "env": { "CLAUDE_CODE_GIT_BASH_PATH": "C:\\Program Files\\Git\\bin\\bash.exe" } }
  ```

## Installation

See [Installation in the README](../README.md#installation).

## Codex CLI

The same rules and the same binary; only the delivery differs.

In a terminal:

```
codex plugin marketplace add Comprise/paraplane --ref release
```

Then in Codex: `/plugins` — install `paraplane`; `/hooks` — **trust** the plugin hooks (until they are
trusted, Codex does not run them).

- Skills are invoked as `$paraplane:rules`, `$paraplane:create <task>`, `$paraplane:check [path]`.
- The plugin's `bin/` is not on PATH of the Codex shell tool: hook texts name the CLI by the absolute path
  `<plugin dir>/bin/paraplane`, and the skills take the command from there. The Codex hooks are
  `hooks/codex.json`, calling `paraplane hook <event> --agent codex`.
- Codex does not load plugin agents: the skills perform the audit themselves from the text of `agents/conflict-auditor.md`.

| What | Claude Code | Codex |
|---|---|---|
| Plan from plan mode | `PreToolUse` `ExitPlanMode`: leaving is forbidden | `Stop`: the plan from the `<proposed_plan>` block in the transcript; E errors — `block`, Codex continues the turn and fixes the plan; a repeated `Stop` of the same turn stays silent |
| Plan file | `PostToolUse` `Write\|Edit` | `PostToolUse` `^(Bash\|apply_patch)$`: files from the `Bash` command (`printf … > file`) and from patch headers; E1–E7 in a file the command writes — `block`, a file it only reads (`cat plan.md`) — context only; a write through an indirect command can slip past the hook — `Stop` and the skills catch it |
| Rules before a skill | `PreToolUse` `Skill` | `UserPromptSubmit`: the prompt mentions `$…` with `execut`/`subagent-driven` or `plan` |
| CLI approval | `PreToolUse` `Bash` | `PermissionRequest` `^Bash$`, only the strict command form and the absolute CLI path |
| Session context | `SessionStart` | `SessionStart` (with a reference to `$paraplane:rules`) |
| Agent `conflict-auditor` | started from the skills | not loaded; Claude performs the audit itself from the agent's text |

`mode: warn` and `enabled: false` work the same way. Hook details — [`hooks/README.md`](../hooks/README.md).

## Limitations

- Executing plans, starting subagents and worktrees are out of scope: the plugin only defines and checks the rules.
- A ban on writing outside `owns` during execution is not implemented (the hook does not know which subagent
  writes): for now there is only the audit after a wave.
- The audit does not distinguish tasks within a wave: a write of one task into another's file is caught by the gate tests.
- Hidden conflicts (files not named in the plan) are invisible to the deterministic check — that is what the
  `conflict-auditor` agent is for; it is started from skills, not from hooks.
- Hooks do not use an LLM.
- Overlap and hot files compare case-insensitively; `audit` (A1) matches changed paths against `owns` exactly,
  case included. There is no Unicode normalization: on macOS the NFC and NFD spellings of one name are one file but
  different paths to the check, so their overlap is not detected (documented in the skill `rules`).
- Codex: returning a plan through `Stop` in Plan Mode (`decision: block`) has not been verified live (Codex avoided
  the conflict itself); recheck after Codex updates (spec, section 13).
- A user's `/skill` call does not pass through `PreToolUse`; such plans are caught by `ExitPlanMode` and `PostToolUse`.
- Whether the session stays in plan mode after leaving is denied is not specified in the Claude Code
  documentation; the behavior is verified manually.

## Troubleshooting

- `/hooks` should list the plugin hooks: `SessionStart`, `PreToolUse`, `PostToolUse` (in Codex — `SessionStart`, `UserPromptSubmit`, `PostToolUse`, `Stop`, `PermissionRequest`, and they must be trusted).
- `paraplane check <plan>` shows config warnings (`warning: config: …`) and plan violations.
- Hook errors go to stderr: run `claude --debug`.

## Development

```sh
make test                                    # tests
make dist                                    # binaries for 6 platforms into libexec/ (needed before --plugin-dir)
make tree DIR=/tmp/paraplane-tree            # the shipped tree: exactly the paths of ship.txt
claude --plugin-dir /path/to/paraplane       # run Claude Code with the local version
```

Without `make dist` the shim `bin/paraplane` finds no binary in `libexec/` (see `bin/README.md`).

- `.gitattributes` forces LF line endings, so Windows checkouts and installs keep a working POSIX shim; it is shipped too.
- `ship.txt` is the single list of shipped paths. `make tree DIR=<dir>` assembles the tree from it (skipping `libexec`
  when it is absent); the CI strict validation, the release tarball and the `release` branch all read the same file.
- CI runs `go test` on three platforms, a separate `go test -race ./...` job on ubuntu and plugin validation;
  changes under `docs/` trigger CI too.
- A release is a tag `vX.Y.Z` on a commit from `main`: the workflow builds the binaries and updates the `release`
  branch the plugin is installed from. The binary version has no `v` prefix (`paraplane version` prints `0.9.1`
  for the tag `v0.9.1`). Before publishing, the release smoke-tests the shipped binary and the shim: both must
  print the tag version.
- Specification (in Russian):
  [`docs/superpowers/specs/2026-10-20-paraplane-design.md`](https://github.com/Comprise/paraplane/blob/main/docs/superpowers/specs/2026-10-20-paraplane-design.md).
