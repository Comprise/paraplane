---
name: create
description: This skill should be used when the user runs /paraplane:create (or $paraplane:create in Codex) to build a new implementation plan that is ready for parallel subagents.
argument-hint: "<task description or path to a spec>"
disable-model-invocation: true
allowed-tools: Bash(paraplane check *)
---
# /paraplane:create (Codex: $paraplane:create)

In Codex, `paraplane` is not on PATH: run the commands below by the absolute path the hook text gives (`<plugin dir>/bin/paraplane`).

Build a new implementation plan for: $ARGUMENTS

If the argument is empty, ask the user what the plan is for and stop until they answer. If it is a path to a
spec, read that file first.

1. Invoke the `paraplane:rules` skill (Codex: `$paraplane:rules`) and follow it for the whole run: block format, validation rules E0-E7 and
   W1-W5, how to split work into tasks and waves, hot files.
2. Ask the user about anything unclear: scope, non-goals, constraints, the gate command (tests/lint) of the
   project. Ask once, in one message, and do not start planning before the answers.
3. Study the code the plan will touch (Read, Grep, Glob). Find the files each task will create or change,
   the interfaces tasks consume, registration points (routers, indexes, registries), and hot files
   (lockfiles, migrations). Do not guess file names that the code could tell you.
4. Write the plan as Markdown: the prose for people, plus exactly one fenced block with the info string
   `paraplane` (see `paraplane:rules` for the format). Give every task `owns`, `reads`, `depends` and `verify`;
   set a gate for the plan or each wave. Order tasks by dependencies, not by the natural order of stages:
   an independent stage goes into the earliest wave it can run in. Quote glob patterns in YAML.
5. Save the draft directly at its final path before the audit: `<plans_dir>/YYYY-MM-DD-<slug>.md`, where
   `plans_dir` comes from `.paraplane/config.yaml` (default `docs/plans`; create the directory if it does not exist), the date is
   today's, and the slug is a short lowercase kebab-case name of the task. The file must be inside the project
   root: `paraplane check` rejects files elsewhere (for example in /tmp) with `error:`.
6. When a wave has two or more tasks, dispatch the `paraplane:conflict-auditor` agent with the path of the
   saved file and the project root. Wait for its report (a list of findings and a verdict
   `clean` / `conflicts`). In Codex plugin agents are not loaded: read `agents/conflict-auditor.md` in the
   plugin directory and perform the audit yourself, read-only, with exactly that output format.
7. Fix the plan yourself for every finding (narrow `owns`, add `depends`, move a task to another wave, merge
   tasks). Keep the block and the prose in sync: a task renamed or moved in the block is renamed or moved in
   the text.
8. Validate it: `paraplane check --suggest <file>`. Repeat the fixes and the check until the last line is
   `plan ok` (possibly `plan ok with N warning(s)`).
   - The first line that is not a `warning:` line starts with `error:`, or the exit code is 2: report the
     message to the user and stop.
   - `plan invalid: …` with `error E…` lines: fix each error, then check again.
   - `Shorter layout:` followed by a block: take the suggested layout into the plan (replace the block, update
     the prose to match) unless it breaks something the auditor found.
   - `warning:` lines are not blocking; fix W1 (no `verify`) and W4 (empty `owns`) when the plan allows it.
9. Report briefly: the file path; the waves and their tasks (id and title per wave); what you moved
   or changed against the first draft and why (reordered stages, split or merged tasks, auditor findings fixed);
   remaining warnings.
