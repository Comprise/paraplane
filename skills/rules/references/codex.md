# paraplane in Codex CLI

The rules are the same as in Claude Code; only the delivery differs. Skills are invoked as
`$paraplane:<name>` (`$paraplane:rules`, `$paraplane:create`, `$paraplane:check`, `$paraplane:progress`).

## The CLI path

The plugin `bin/` is not on PATH of the Codex shell tool. Run `paraplane …` by the absolute path the
hook text gives, `<plugin dir>/bin/paraplane` (the plugin directory in the Codex cache; the hooks see it as
`CLAUDE_PLUGIN_ROOT`). Without a path in the hook text, ask the user for it. Every command in these skills
(`check`, `audit`, `progress`) works the same with the absolute path.

## Where the plan is checked

- **Plan Mode.** There is no `ExitPlanMode`. The plan arrives as a `<proposed_plan>` block in the answer. When
  the turn ends, the `Stop` hook reads the last such block from the transcript and validates the
  `paraplane` block inside it. With errors E the hook answers `block`: continue the turn, fix the plan and
  finish again. The second `Stop` of the same turn (`stop_hook_active`) stays silent, so run
  `paraplane check` yourself on the final text.
- **Plan files.** Codex writes files through `Bash` (`printf … > docs/plans/x.md`) or `apply_patch`. The
  `PostToolUse` hook checks existing files that match `plan_paths`: E0 is a hint, E1–E7 block a file the command
  writes; a file the command only reads (`cat plan.md`) gets E1–E7 as context, never a block, and E0 gives
  nothing. A file written
  indirectly can be missed, so always run `paraplane check <file>` before declaring the plan final.
- **Skills.** `UserPromptSubmit` sees `$…` mentions in the prompt: an execution skill (`execut`,
  `subagent-driven`) brings the execution rules (with the progress mode), a skill with `plan` in its name
  brings the planning rules. Without such a mention, read `progress` in `.paraplane/config.yaml` yourself.
- **Progress.** In Codex you show the progress; no hook shows it to the user. Codex has no subagent
  tool, so returned subagents are not seen and nothing is recorded for them. After a commit that names
  the plan (`git commit`, `merge`, …) the `PostToolUse` hook passes the progress to you as context; show
  it to the user in a code block. Otherwise run `paraplane progress <plan>` yourself at the moments of
  `progress` (`references/execution.md`, "Codex: you show it") and pass closed, uncommitted tasks in
  `--done`.

## The conflict-auditor without agents

Codex does not load plugin agents. When these rules say "run `paraplane:conflict-auditor`", read
`agents/conflict-auditor.md` from the plugin directory and do the audit yourself, as that text describes:
read-only (read and search files, no edits), the same finding kinds and the same output (`verdict: clean`,
`verdict: conflicts` plus findings with file and line evidence, or `verdict: error` with one `- kind: input`
item when the plan cannot be read, has zero or several `paraplane` blocks, its YAML is invalid, or it lacks a
requested wave id). Fix the
plan afterwards as usual; `error` is never `clean`: fix the input and audit again, or report it to the user.

## Subagents

Codex has no subagent tool of the plugin's kind. Where Codex can run workers in parallel, dispatch the
tasks of a wave in one step; otherwise execute them one after another, which is safe too. The rules stay
the same: each task changes only its `owns`, workers never commit, `paraplane audit` and the gate run
after the wave. The coordinator commits by the same three schemes as in Claude Code: once per wave
(default, `Paraplane-Wave`), once per stage when the project's rules say so (one `Paraplane-Wave` trailer
per wave, audit with `W1,W2`), or once per task when the project's rules say so (`Paraplane-Task: <id>`,
no wave commit). The gate is repository code: in a repository you did not write, show it to the user
before running it.
