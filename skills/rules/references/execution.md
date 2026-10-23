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
   - a line `Paraplane-Task: <plan path from the project root> <task id>` on its own (the progress hook
     reads it when the subagent returns, to count the task as done; see "When a `Paraplane-Task` line
     counts" below);
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
   The audit also checks the commits of the wave on the current branch: a task commit
   (`Paraplane-Task`) may only change files of the tasks it names, a wave or stage commit only files of
   the waves it names:
   - `error A1 W2: internal/x.go changed by T3 (commit abc1234) but T3 does not own it`;
   - `error A1 W2: internal/x.go changed in commit abc1234 but no task of W1, W2 owns it`;
   - the summary then counts the commits: `audit ok: W2, 5 changed file(s), 2 commit(s)`.
   Run the audit before the gate, so gate artifacts (coverage files, binaries) do not enter it. In the
   working tree the audit cannot tell tasks of one wave apart (T1 writing a file of T2 is not visible);
   task commits can, and the gate's tests catch the rest.
   The audit runs `git status` with `--ignore-submodules=all`. Because `git status` may run filter
   drivers and the audit is approved without a prompt, it refuses to run when the local or worktree
   repository config (including files pulled in with `include`/`includeIf`) defines
   `filter.<name>.clean`, `.smudge` or `.process`. Only the user's global git config is trusted
   (git-lfs and the like); the system config is not read:
   `error: the repository config defines filter drivers; paraplane audit does not run git status here`
   (exit 1). On git that ignores `GIT_NO_LAZY_FETCH` (anything but 2.46+ and 2.45.1, 2.44.1, 2.43.4,
   2.42.2, 2.41.1, 2.40.2, 2.39.4; an unparsable version and a distro backport under an older version
   number count as old) it refuses with `error: … partial clone …` when `extensions.partialClone` or
   `remote.*.promisor` is set in any config, or the repository config sets `remote.*.uploadpack`.
   It also refuses when git output exceeds 16 MiB (`git output too large`). Report the refusal to the user and check the wave's files against `owns`
   by hand.
2. **Gate:** run the wave's gate (`wave.gate`, else `plan.gate`, else `default_gate`) — the full
   check run, not only the new tests. The gate, `verify` and `default_gate` are commands from the
   repository, code like any other: in a repository you did not write yourself, show the command to the
   user before you run it the first time, and never run one that does more than build and test.
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

   Git reads trailers only from the last paragraph of the message. Put these two lines there, together
   with `Co-Authored-By` and any other trailers, with no blank line between them; otherwise the wave is
   not counted and `paraplane progress` prints `warning: wave <id> commit <hash>: trailers not in the last
   paragraph`. Amend such a commit (`git commit --amend`) so the trailers form the last paragraph.

   Only when the project's own rules require one commit per task, the coordinator commits each task
   after its review, one at a time, with `Paraplane-Plan` and `Paraplane-Task: <task id>` (several tasks:
   one trailer each); the wave needs no commit of its own. Subagents still never commit.

   Only when the project's own rules (its `CLAUDE.md`, say) require one commit per stage of several
   waves, commit once per stage instead of once per wave. The audit then sees the uncommitted changes of
   every wave of the stage so far, so after each wave audit them all at once:
   `paraplane audit <plan> W1,W2` (a file may be owned by a task of any listed wave). The gate still runs
   after each wave. The stage commit lists every wave it closes, one `Paraplane-Wave` trailer each
   (`Paraplane-Wave: W1`, `Paraplane-Wave: W2`; `Paraplane-Wave: W1, W2` works too), and progress counts
   all of them.

5. **Show progress** as the next section says.

## When to show progress

The moments come from the key `progress` in `.paraplane/config.yaml` (default `wave`): `wave` — after
each wave that got done, `task` — also after each task that got done. The execution digest the hooks add
names the mode; when no digest named it, read the key yourself. Who shows the progress depends on the
agent.

### Claude Code: the hooks show it

The hooks show the progress to the user by themselves (`systemMessage`) at those moments: when a
subagent with a `Paraplane-Task` line returns (a background one: at the end of your turn, from the `Stop`
hook) or when a commit names the plan. When the hook showed it, its context says
`paraplane: progress of <plan> was shown to the user; do not repeat it.` Do not repeat it. Run
`paraplane progress <plan>` yourself only when the user asks, or when a wave got done and no hook
message appeared for it; then paste the output into your message in a code block. Pass in `--done` only
tasks closed without a subagent (see below).

### Codex: you show it

Codex has no subagent tool, so no hook sees returned subagents. After a commit that names the plan the
`PostToolUse` hook passes the progress to you as context; show it to the user in a code block. Otherwise
show it yourself:

- **`wave`:** after each closed wave (audit and gate passed) run `paraplane progress <plan>`. Pass in
  `--done` every closed task that is not committed yet, of all uncommitted waves (in a stage, the earlier
  waves too): `paraplane progress --done <ids> <plan>`.
- **`task`:** the same, and after each closed task (its work finished and its review, if the execution
  skill makes one, passed) run `paraplane progress --done <every closed, uncommitted task> <plan>`, the
  ids joined by `,` without spaces.

Paste the output into the message to the user in a code block.

### Where "done" comes from

Closed tasks come from three places, and `paraplane progress` reads the first two itself: task and wave
commits on the current branch (their trailers, so they survive a session restart), the returned
subagents the hook recorded in `<git-dir>/paraplane/closed.json`, and `--done`. Pass in `--done` only
tasks closed without a subagent. The user can ask for progress at any moment with `/paraplane:progress`.

### When a `Paraplane-Task` line counts

The hook records a returned subagent only when its prompt line:

- stands on a line of its own (only spaces or tabs around it) and is among the first 16 such lines;
- names the plan by a path without spaces, from the project root (an absolute path inside the root also
  works), that matches `plan_paths` and is a regular file;
- names a task id of that plan (`[A-Za-z][A-Za-z0-9_.-]{0,31}`).

The entry is tied to the plan block as it was then (its SHA-256): after the block changes, the entry no
longer counts. It counts toward the progress shown only while the plan has no E errors, and only while
the commit it was recorded on is reachable from `HEAD` (after a branch switch it waits, it is not lost).
Any line that misses these conditions is ignored silently, and the task is not counted.

The hook records the task when its subagent returns, **before** your review and the gate. When a
recorded task fails its review, undo the record: `paraplane progress --forget <ids> <plan>` (ids joined by
`,`; it prints `forgot <ids> in <plan>`, or `nothing recorded for …`). Then the hook shows the plan's
progress again at its next moment.

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

A task is done when a commit names it (`Paraplane-Task`), its wave has a commit (`Paraplane-Wave`), its
subagent returned with the `Paraplane-Task` line (kept in `<git-dir>/paraplane/closed.json`, as the
previous section says), or it is in `--done`. A wave is closed when it has a commit or all its tasks are
done. A closed wave whose previous waves are all `done` is `done`; the first wave that is not `done` is
`running`; the rest are `waiting`. Done tasks of the running wave, from any of these sources, are drawn
🟩, the others 🟨.
Warnings to act on:

- `warning: wave <id> was committed before <running id>` — a later wave was committed while an
  earlier one is not done; finish the earlier wave (audit, gate, commit) before continuing.
- `warning: commit for unknown wave <id>` — a commit's `Paraplane-Wave` trailer names a wave the plan
  does not have; check the trailer and the plan.
- `warning: wave <id> commit <hash>: trailers not in the last paragraph` — the commit has `Paraplane-*` lines
  that git does not read as trailers (a blank line before `Co-Authored-By`, say), so the wave is not counted;
  amend the commit so they are in its last paragraph.
- `warning: task <id> commit <hash>: trailers not in the last paragraph` — as for waves: amend the commit.
- `warning: task <id> was committed before <running id>` — a task of a later wave was committed while an
  earlier wave is not done; it is not counted until its wave is running.
- `warning: --done <id> is not a task of the plan` — a typo in `--done`; fix the id.
- `warning: --done <id> is in waiting wave <W>` — a task of a later wave was passed; it is not counted
  until its wave is running.
- `warning: task <id> finished before <running id>` — the subagent of a task of a later wave returned
  while an earlier wave is not done; it is not counted until its wave is running. Finish the earlier
  wave first.

## Exit codes

`paraplane check`, `audit` and `progress` exit 0 when there are no errors, 1 on errors or an
unreadable file, 2 on wrong arguments. A line starting with `error:` (no git, not a repository,
unknown wave, a plan with E errors, filter drivers in the repository config for `audit`, a task id
the plan does not have for `progress --forget`) means the command could not run: report it and stop.
`audit` prints at most 100 A1 lines, then `… and <n> more`; the summary counts all of them.
