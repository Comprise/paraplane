# skills/

**English** | [Русский](README.ru.md)

The plugin's skills. Claude Code finds each one at `skills/<name>/SKILL.md`; this README is not loaded.

| Skill / agent | Invocation in Claude Code | Invocation in Codex | Purpose |
|---|---|---|---|
| `rules` | Claude itself, `/paraplane:rules` | `$paraplane:rules` | all the rules: the ```` ```paraplane ```` block format, codes E0–E7, W1–W5, A1 and how to fix them, how to split and reorder stages, the execution rules and their priority over execution skills; details — `references/format.md`, `references/execution.md`, an example — `examples/plan.md` |
| `create` | `/paraplane:create <task or path to a spec>` | `$paraplane:create …` | clarify the task, study the code, compose a plan, `conflict-auditor`, fix, `paraplane check`, save to `plans_dir/YYYY-MM-DD-<slug>.md` |
| `check` | `/paraplane:check [path]` | `$paraplane:check [path]` | `paraplane check --suggest` + `conflict-auditor`, edit the plan itself, report the reorderings |
| `agents/conflict-auditor` | from skills; `rules` rule: with ≥ 2 tasks in a wave | Codex does not load plugin agents: the skill performs the audit itself from the agent's text | compares the plan with the code: hidden shared files, registration points, API breaks inside a wave, migrations; only `Read, Grep, Glob`; the main Claude edits the plan |

- `disable-model-invocation: true` on `create` and `check`: they change files at the user's request and are started only by the user. Claude loads `rules` itself — when writing, revising and checking any plan and on paraplane hook messages.
- The execution rules in `rules` outrank the instructions of any execution skill (`superpowers:subagent-driven-development`, `superpowers:executing-plans`, etc.): for a plan with a ```` ```paraplane ```` block the tasks of a wave run in parallel, and only the coordinator commits, once per wave.
- The example `rules/examples/plan.md` sits inside a 4-backtick fence, so the file itself is not counted as a plan; the test `TestRulesExamplePlan` (`internal/plugintest`) runs the inner block through `rules.Check` — when editing the example the block must stay free of errors.
- The texts printed by the CLI and hooks (`plan ok: …`, `plan invalid: …`, `Shorter layout:`, `error A1 …`, `audit ok: …`, `quote glob patterns`, `paraplane progress` warnings) are quoted verbatim in the skills: when they change in Go, edit the skills too. The rule digests in the hooks (`internal/hooks`) are a short version of `rules`; when the rules change, edit both.
- In Claude Code skills call `paraplane` by name: the plugin's `bin/` is on PATH. In Codex `bin/` is not on PATH: the skills tell Claude to take the command by the absolute path from the hook text (`<plugin dir>/bin/paraplane`). Codex details — `rules/references/codex.md`.
