# paraplane — polish (every remaining finding)

**Status:** implemented in v0.0.1 (first released as v1.0.0).

> **For agentic workers:** Execution Rules of `docs/superpowers/plans/2026-10-20-paraplane.md` (parallel wave, only your `owns`, no commits, golden files by hand, stop and report on interface mismatch). Trailers: `Paraplane-Plan: docs/superpowers/plans/2026-10-20-paraplane-polish.md`, `Paraplane-Wave: <id>`.

**Goal:** fix every finding left open after the final review, including the three behaviour items the user decided on: (1) a Codex *read* of a broken plan gives context instead of blocking; (2) overlap compares literal segments case-insensitively; (3) git < 2.26 gets a filter-driver fallback. Unicode NFC/NFD stays a documented limitation (would need `golang.org/x/text`, outside the dependency list).

**Spec:** `docs/superpowers/specs/2026-10-20-paraplane-design.md` (the coordinator amends §4.1, §5.1, §12a.2, §13 before the wave).

## Waves

```paraplane
version: 1
gate: "go mod tidy -diff && go vet ./... && GOOS=windows go vet ./... && GOOS=darwin go vet ./... && test -z \"$(gofmt -l .)\" && go test ./... && go test -race ./internal/... && sh -n bin/paraplane"
waves:
  - id: W1
    tasks:
      - id: P1
        title: "textx + progress polish"
        owns: ["internal/textx/**", "internal/progress/**"]
        verify: "go test ./internal/textx/ ./internal/progress/"
      - id: P2
        title: "pathset: case-insensitive overlap"
        owns: ["internal/pathset/**"]
        verify: "go test ./internal/pathset/ ./internal/layout/"
      - id: P3
        title: "plan: CommonMark de-indent, fence tests"
        owns: ["internal/plan/**"]
        verify: "go test ./internal/plan/"
      - id: P4
        title: "gitx: old-git fallback, output cap, trailer tests"
        owns: ["internal/gitx/**"]
        verify: "go test ./internal/gitx/"
      - id: P5
        title: "config: stricter default_gate and plans_dir"
        owns: ["internal/config/**"]
        verify: "go test ./internal/config/"
      - id: P7
        title: "rules: messages, Where consistency, W3 wording"
        owns: ["internal/rules/**", internal/hooks/testdata/plan_exit_warnings.golden.json, internal/hooks/codex_test.go]
        verify: "go test ./internal/rules/"
  - id: W2
    tasks:
      - id: P10
        title: "release: upload the built archive before any third-party code runs"
        owns: [".github/**"]
        verify: "for f in .github/workflows/*.yml; do python3 -c 'import sys,yaml;yaml.safe_load(open(sys.argv[1]))' $f || exit 1; done"
      - id: P6
        title: "plugintest + agent text"
        owns: ["internal/plugintest/**", "agents/**"]
        verify: "go test ./internal/plugintest/"
      - id: P8
        title: "hooks: Codex read → context, Root symlinks, edit window, heredocs, windows .exe, timing test"
        owns: ["internal/hooks/**"]
        depends: [P1, P2, P3, P4, P5, P7]
        verify: "go test -race ./internal/hooks/"
      - id: P9
        title: "cmd + docs"
        owns: ["cmd/**", README.md, README.ru.md, hooks/README.md, hooks/README.ru.md, bin/README.md, bin/README.ru.md, "skills/**"]
        depends: [P1, P4, P5, P7]
        verify: "go test ./cmd/paraplane/ ./internal/plugintest/"
```

P7 owns one hooks golden file, so P8 (all of internal/hooks) depends on it (E6). P6, P8 and P9 share no files.

---

### Task P1: textx + progress
- textx: negative `max` → treated as 0 (no panic); doc comments say the `…` suffix is not counted in `max`; tests: `max==0`, negative, C1 controls, newline at the cut point in `Block`; bidi characters in tests written as `‮` escapes.
- progress: rename `maxIDRunes`/`maxTaskID` to names that say they are pre-ellipsis caps; cap warning lines at 72 columns; tests: dedup of repeated unknown ids, control characters in names/ids sanitised, header and counts asserted in `TestRenderCommittedOutOfOrder`.

### Task P2: pathset
- Overlap compares literal segments case-insensitively (`strings.EqualFold`) and matches patterns against literals case-insensitively (fold both sides before `doublestar.Match`), so `README.md`/`readme.md` → overlap (conservative "may overlap"; spec §4.1). Keep the no-miss property test and add a case-insensitive brute-force variant.
- Keep `pathset.Overlap` (reference API) and document it as such.

### Task P3: plan
- Fence body of a fence indented 1–3 spaces is de-indented like CommonMark (remove up to the fence's indent from each body line).
- Tests: closing fence indented ≥ 4 does not close; shorter closing fence does not close; `ErrManyBlocks` with one unclosed block.

### Task P4: gitx
- git < 2.26 (no `--show-scope`, exit 129): fall back to `git config --local --includes --get-regexp …` and `git config --worktree --includes --get-regexp …` (worktree query error ignored when unsupported); any match → `ErrFilterDrivers`; exit 1 + empty output → none; any other failure → error. Test via a fake `git` wrapper on PATH that rejects `--show-scope`.
- `run`: cap captured stdout at 16 MiB (error "git output too large").
- Tests: trailer `Paraplane-Plan` without `Paraplane-Wave` ignored; duplicate trailers last-wins documented and tested; case-insensitive trailer keys; deletions and staged-only entries in `Changed`.

### Task P5: config
- `default_gate`: reject any control character (incl. NUL) and U+2028/U+2029, not only `\r\n`.
- `plans_dir`: reject glob metacharacters (`* ? [ ] { }`).
- Tests for both; FIFO tests stay build-tagged.

### Task P6: plugintest + agents
- Frontmatter test decodes into a struct with `KnownFields(true)` (real strictness; list the allowed keys from Claude Code docs: name, description, argument-hint, disable-model-invocation, allowed-tools, user-invocable, model, tools, color) and asserts the agent has a `tools:` key.
- Example-plan tests log warnings (`t.Logf`) and fail on none.
- Packaging test: workflows read `ship.txt` (grep) and release version is `${GITHUB_REF_NAME#v}`.
- Shim test: existing but non-executable binary → `is not executable` notice; non-session-start hook passes the binary's stdout through.
- agents/conflict-auditor.md: say what to output when the plan file or block cannot be read (`verdict: conflicts` is wrong — output `verdict: error` with one `- kind: input` line), and mark the example finding as illustrative.

### Task P7: rules
- E3 messages and every finding of a task with an invalid/duplicate id use the same `Where` and `waveName` rules as waves.
- W3 wording without "plan file": ``the layout can be shorter: N wave(s) instead of M (`paraplane check --suggest` prints it)``; update `internal/hooks/testdata/plan_exit_warnings.golden.json` by hand.

### Task P8: hooks
- Codex PostToolUse: a plan file that is only *read* gets `additionalContext` (same text as the block reason) instead of `decision: block`; writes keep the full verdict (spec §12a.2 amended).
- Root comparison: resolve `env.Root` and the candidate path with `filepath.EvalSymlinks` (macOS `/var` → `/private/var`) before the prefix check.
- Edit filter: an occurrence of `new_string` that starts before `Block.Start` but overlaps the block counts.
- Shell lexer: heredoc bodies are not scanned for plan paths.
- `bashShadowed`: on Windows compare against `bin/paraplane` and `bin/paraplane.exe`; test the comparison as a pure function so it runs everywhere.
- `TestCodexSlowPlansOneDeadline` and other wall-clock tests: skip under `-race` and use generous thresholds (≥ 3× budget).
- Golden helper tolerates CRLF; `TestDisabled` table-driven over every handler.

### Task P9: cmd + docs
- cmd: `check ..` on a directory says "is a directory"; error stream consistent.
- Docs (all README pairs, skills/rules references): Codex read → context; case-insensitive overlap; NFC limitation; old-git fallback; bin/README notes that hook output is buffered and trailing newlines normalised; remove the stale "Windows never auto-approves" wording if present; keep `rules/SKILL.md` body ≤ 1500 words.

### Task P10: release ordering
- release.yml `validate` job: upload the `dist` artifact (archive + checksums.txt) **before** any step that runs downloaded third-party code (`npx … plugin validate`) and before "Validate the tree users install"; `publish` keeps verifying checksums of exactly those bytes. Keep pinned SHAs and permissions; YAML must parse. (Final-fix re-review: the npx step could otherwise alter `dist/` before upload and the self-referential checksum would not notice.)
