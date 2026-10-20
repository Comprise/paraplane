# Executing a paraplane plan

These rules apply to every plan with a ```` ```paraplane ```` block, whoever executes it: Claude
directly, `superpowers:subagent-driven-development`, `superpowers:executing-plans` or any other
execution skill. They take precedence over that skill's instructions; "never dispatch implementers in
parallel" and "the implementer commits" do not apply to such a plan.

The coordinator is the main Claude session. Subagents implement tasks.

## Before the first wave

1. Run `paraplane check <plan>` and continue only on `plan ok` (warnings allowed). On errors, fix the
   plan first.
2. Make sure the working tree is clean (`git status --porcelain` is empty): the audit treats every
   change in the working tree as a change of the current wave.

## Running a wave

1. **Dispatch every task of the wave in one message**, one subagent per task, so they run in parallel.
2. **The prompt of each subagent contains:**
   - the text of its task from the plan and the global constraints of the plan;
   - its `owns`, `reads` and `verify`, verbatim;
   - the rule: "Change only files in your `owns`. Do not touch files outside `owns`; if you need one,
     or a consumed interface does not match the plan, stop and report instead of editing it."
   - the rule: "Do not commit, do not tick checkboxes in the plan, run formatters (`gofmt -w` and the
     like) only on your own files, never on the whole tree."
3. **Subagents never commit.** Parallel commits race for `index.lock` and sweep up other tasks' files;
   the audit also relies on the working tree holding exactly the changes of the current wave.
4. When a subagent stops and reports a needed file outside its `owns`, do not let it edit the file.
   Change the plan (give the file to a task, add an integrator task or a dependency), run
   `paraplane check` again, then continue.

## After a wave

1. **Audit:** `paraplane audit <plan> <wave>`. Every changed file of the working tree (renames count
   with both paths) must be owned by some task of this wave; the plan file itself and `audit_ignore`
   patterns are exempt. Output:
   - a violation: `error A1 W2: internal/x.go changed but no task of W2 owns it`;
   - the summary: `audit ok: W2, 5 changed file(s)` or `audit failed: 2 file(s) outside W2`.
   Run the audit before the gate, so gate artifacts (coverage files, binaries) do not enter it. The
   audit cannot tell tasks of one wave apart (T1 writing a file of T2 is not visible); the gate's
   tests catch that.
   The audit runs `git status` with `--ignore-submodules=all`. Because `git status` may run filter
   drivers and the audit is approved without a prompt, it refuses to run when the local repository
   config defines `filter.<name>.clean`, `.smudge` or `.process`:
   `error: the repository config defines filter drivers; paraplane audit does not run git status here`
   (exit 1). Report it to the user and check the wave's files against `owns` by hand.
2. **Gate:** run the wave's gate (`wave.gate`, else `plan.gate`, else `default_gate`) — the full
   check run, not only the new tests.
3. **Fix before the next wave.** An audit violation or a red gate is fixed before the next wave
   starts: revert stray changes, or let the subagent of the task that owns the broken file in this
   wave fix it. A breakage in a file of an earlier wave is a change to the plan, not a silent fix.
   Repeat audit and gate until both pass.
4. **Commit once per wave.** After the green gate the coordinator ticks the wave's checkboxes and makes
   one commit for the whole wave, with the trailers:

   ```
   Paraplane-Plan: <plan path from the project root>
   Paraplane-Wave: <wave id>
   ```

5. **Show progress.** After the commit run `paraplane progress <plan>` and paste its output into the
   message to the user in a code block. Progress is read from the block and the commit trailers of the
   current branch, so it survives a session restart.

## Progress output

```
paraplane · example.md                                    wave 3/5 · 64%
█████████████████████████░░░░░░░░░░░░░░░  11/17 tasks

W1  🟩🟩🟩🟩🟩🟩  6/6  ✓ done     T1 T2 T3 T4 T5 T6
W2  🟩🟩🟩🟩🟩    5/5  ✓ done     T7 T8 T9 T10 T11
W3  🟨🟨          0/2  ▶ running  T12 T13
W4  ⬜⬜          0/2  · waiting  T14 T15
W5  ⬜⬜          0/2  · waiting  T16 T17
```

A wave with a commit whose previous waves are all `done` is `done`; the first wave that is not
`done` is `running`; the rest are `waiting`. Warnings to act on:

- `warning: wave <id> was committed before <running id>` — a later wave was committed while an
  earlier one is not done; finish the earlier wave (audit, gate, commit) before continuing.
- `warning: commit for unknown wave <id>` — a commit's `Paraplane-Wave` trailer names a wave the plan
  does not have; check the trailer and the plan.

## Exit codes

`paraplane check`, `audit` and `progress` exit 0 when there are no errors, 1 on errors or an
unreadable file, 2 on wrong arguments. A line starting with `error:` (no git, not a repository,
unknown wave, a plan with E errors, filter drivers in the repository config for `audit`) means the
command could not run: report it and stop.
