---
name: check
description: This skill should be used when the user runs /paraplane:check (or $paraplane:check in Codex) to validate an existing plan and restructure it for parallel execution.
argument-hint: "[plan file]"
disable-model-invocation: true
allowed-tools: Bash(paraplane check *)
---
# /paraplane:check (Codex: $paraplane:check)

In Codex, `paraplane` is not on PATH: run the commands below by the absolute path the hook text gives (`<plugin dir>/bin/paraplane`).

Validate and restructure the plan: $ARGUMENTS

1. Pick the plan file.
   - With an argument: use that path.
   - Without one: find the newest `.md` file (by modification time) matching `plan_paths` from
     `.paraplane/config.yaml` (default `docs/plans/**/*.md`, `docs/superpowers/plans/**/*.md`,
     `.paraplane/plans/**/*.md`) using Glob, not shell commands. If there is none, tell the user and stop.
2. Invoke the `paraplane:rules` skill (Codex: `$paraplane:rules`) and follow it. Run `paraplane check --suggest <file>` and read the
   whole output: `error E…` and `warning W…` lines, `warning: config: …` lines, the summary
   (`plan ok`, `plan ok with N warning(s)` or `plan invalid: …`), and a `Shorter layout:` block if present.
   - The first line that is not a `warning:` line starts with `error:`, or the exit code is 2: report the
     message and stop.
3. When a wave has two or more tasks, dispatch the `paraplane:conflict-auditor` agent with the plan file and the
   project root. Wait for its report (findings and a verdict `clean` / `conflicts`). In Codex plugin
   agents are not loaded: read `agents/conflict-auditor.md` in the plugin directory and perform the audit
   yourself, read-only, with exactly that output format.
4. Edit the plan file itself (do not just describe the changes):
   - fix every `error E…` and every auditor finding (narrow `owns`, add `depends`, move a task to another
     wave, add a missing gate or `verify`);
   - apply `Shorter layout:` when it is offered: replace the block with the suggested one, which moves
     independent stages into earlier waves;
   - keep the block and the prose in sync: stage headings, task titles and wave numbers in the text follow
     the block.
5. Run `paraplane check --suggest <file>` again. Repeat steps 4–5 until the last line is `plan ok`
   (possibly `plan ok with N warning(s)`); re-run the auditor only if task boundaries changed.
6. Report: which E and W findings you fixed (code, place, what changed); which stages you reordered and the
   reason (dependency, shared file, hot file, shorter layout); remaining warnings; the final summary line.
