---
name: progress
description: This skill should be used when the user runs /paraplane:progress (or $paraplane:progress in Codex) to see the execution progress of a plan at any moment.
argument-hint: "[plan file]"
disable-model-invocation: true
allowed-tools: Bash(paraplane progress *)
---
# /paraplane:progress (Codex: $paraplane:progress)

In Codex, `paraplane` is not on PATH: run the commands below by the absolute path the hook text gives (`<plugin dir>/bin/paraplane`).

Show the progress of the plan: $ARGUMENTS

1. Pick the plan file.
   - With an argument: use that path.
   - Without one: the plan being executed in this session, if any. Otherwise find the newest `.md` file
     (by modification time) matching `plan_paths` from `.paraplane/config.yaml` (default
     `docs/plans/**/*.md`, `docs/superpowers/plans/**/*.md`, `.paraplane/plans/**/*.md`) using Glob, not
     shell commands. If there is none, tell the user and stop.
2. Tasks whose subagents returned with a `Paraplane-Task` line and committed tasks are counted by
   `paraplane progress` itself. If this session executes that plan and has closed tasks it does not know
   of otherwise (no subagent, or Codex), list them in `--done`, the ids joined by `,` without spaces.
3. Run `paraplane progress <file>`, or `paraplane progress --done <ids> <file>`, and paste the output into
   your answer in a code block. If the first line starts with `error:` or the exit code is 2, report the
   message and stop. Change nothing in the plan or the code.
