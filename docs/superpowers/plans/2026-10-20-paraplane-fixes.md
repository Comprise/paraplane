# paraplane — audit fixes

**Status:** implemented in v0.0.1 (first released as v1.0.0).

> **For agentic workers:** execute wave by wave with the Execution Rules of `docs/superpowers/plans/2026-10-20-paraplane.md` (all tasks of a wave in parallel, only your `owns`, no commits, golden files by hand, stop and report on interface mismatch). Commit trailers: `Paraplane-Plan: docs/superpowers/plans/2026-10-20-paraplane-fixes.md`, `Paraplane-Wave: <id>`.

**Goal:** fix every finding of the security scan (`CLAUDE-SECURITY-20261020-115517`) and the three code audits (correctness; CLI/packaging/docs; tests/simplification), plus the confirmed deferred minors.

**Spec:** `docs/superpowers/specs/2026-10-20-paraplane-design.md` (amended in `8746de0`: §4 brace bound and rune length, §5.1 git signature programs and NOSYSTEM, §12a, §13).

## Global Constraints

Same as the main plan (module `paraplane`, deps, hooks exit 0 and write nothing, `/` paths, English texts through `textx`, author `Elia Samoylov`, Linux/macOS/Windows × amd64/arm64). Behaviour that is not named in a task stays unchanged; existing golden files change only where a task says so. Every README.md has a README.ru.md twin kept in sync.

## Waves

```paraplane
version: 1
gate: "go mod tidy -diff && go vet ./... && test -z \"$(gofmt -l .)\" && go test ./... && go test -race ./internal/... && sh -n bin/paraplane"
waves:
  - id: W1
    tasks:
      - id: F1
        title: "gitx: neutralise signature programs, hermetic tests"
        owns: ["internal/gitx/**"]
        verify: "go test ./internal/gitx/"
      - id: F2
        title: "pathset+layout: brace expansion bound, rune length, one overlap implementation"
        owns: ["internal/pathset/**", "internal/layout/**"]
        verify: "go test ./internal/pathset/ ./internal/layout/"
      - id: F3
        title: "plan: BOM, explicit version, limits before version"
        owns: ["internal/plan/**"]
        verify: "go test ./internal/plan/"
      - id: F4
        title: "fsx: one safe regular-file reader"
        owns: ["internal/fsx/**"]
        verify: "go test ./internal/fsx/"
      - id: F5
        title: "progress: wave ids up to 32 runes"
        owns: ["internal/progress/**"]
        verify: "go test ./internal/progress/"
      - id: F6
        title: "packaging: .gitattributes, one ship list, release smoke test, CI race, shim"
        owns: [.gitattributes, ship.txt, Makefile, ".github/**", bin/paraplane, internal/plugintest/packaging_test.go]
        verify: "sh -n bin/paraplane && for f in .github/workflows/*.yml; do python3 -c 'import sys,yaml;yaml.safe_load(open(sys.argv[1]))' $f || exit 1; done && go test ./internal/plugintest/"
  - id: W2
    tasks:
      - id: F7
        title: "rules: W3 text, version/limits consumers"
        owns: ["internal/rules/**", internal/hooks/testdata/plan_exit_warnings.golden.json]
        reads: [internal/plan/plan.go, internal/layout/layout.go]
        depends: [F2, F3]
        verify: "go test ./internal/rules/"
      - id: F8
        title: "config: regular-file read, happy-path tests"
        owns: ["internal/config/**"]
        reads: [internal/fsx/fsx.go]
        depends: [F4]
        verify: "go test ./internal/config/"
  - id: W3
    tasks:
      - id: F9
        title: "hooks: fsx, suggest cap, Codex texts/edits/approval, tests"
        owns: ["internal/hooks/**"]
        reads: [internal/fsx/fsx.go, internal/rules/rules.go, internal/config/config.go]
        depends: [F4, F7, F8, F1]
        verify: "go test -race ./internal/hooks/"
      - id: F10
        title: "cmd: fsx, no suggest cap, uncovered branches"
        owns: ["cmd/**"]
        reads: [internal/fsx/fsx.go, internal/rules/rules.go]
        depends: [F4, F7, F8, F1, F5]
        verify: "go test ./cmd/paraplane/"
      - id: F11
        title: "skills and docs"
        owns: ["skills/**", "agents/**", README.md, README.ru.md, hooks/README.md, hooks/README.ru.md, bin/README.md, bin/README.ru.md]
        reads: [docs/superpowers/specs/2026-10-20-paraplane-design.md]
        depends: [F6, F7]
        verify: "go test ./internal/plugintest/"
```

W1 holds every leaf; `rules` and `config` follow the packages they import; `hooks`, `cmd` and the docs come last because they print the final texts.

---

### Task F1: gitx

- ✅ Every git call (`runCode`): add `-c log.showSignature=false -c gpg.program=false -c gpg.ssh.program=false -c gpg.x509.program=false -c core.hooksPath=` + `os.DevNull` (spec §5.1). `WaveCommits`: also `--no-show-signature`. Keep `GIT_CONFIG_NOSYSTEM=1`, update the comment (system config is never read; only global filters are trusted).
- Regression test `TestWaveCommitsNoSignatureProgram`: forged `gpgsig` commit (`git hash-object -t commit -w`), local `log.showSignature=true`, `gpg.program=<script creating a marker>` → `WaveCommits` works and the marker is absent; positive control: plain `git log --show-signature` in the same repo creates the marker, else `t.Skip`.
- `TestChangedNoLazyFetch`: add the positive control (plain `git status` creates the marker, else skip).
- Hermetic tests: `TestMain` sets `HOME`, `XDG_CONFIG_HOME`, `GIT_CONFIG_GLOBAL` to a temp dir and `GIT_CEILING_DIRECTORIES` for not-a-repo tests; not-a-repo tests assert the error text mentions "not a git repository"; use `os.DevNull` instead of `"/dev/null"`.
- Remove the unreachable R/C branch under `--no-renames` only if nothing depends on it; document duplicate-trailer behaviour (last wins) in the doc comment.
- Commit message: `fix(gitx): never run signature or hook programs; hermetic tests`.

### Task F2: pathset + layout

- ✅ `pathset.Clean`: reject a pattern whose brace expansion count (product of alternatives over all groups, nested groups multiply) exceeds 256 (error "pattern expands to more than 256 alternatives"); length limit counts runes (`utf8.RuneCountInString`), message unchanged ("characters").
- One implementation: move layout's compiled-pattern overlap into pathset (`type Compiled`, `func Compile(p string) Compiled`, `func (Compiled) Overlaps(Compiled) bool`); `pathset.Overlap(a,b)` = `Compile(a).Overlaps(Compile(b))`; export `SpansSegments` once; layout uses `pathset.Compile`. Keep the differential test (now compiled vs brute force) in pathset.
- Remove `layout.Suggest` and `layout.Conflicts` (tests call `SuggestWith(p, ConflictsWithin(p, hot, time.Time{}))`).
- `layout.Render`: a gate that is empty after `strings.TrimSpace` counts as unset.
- Tests: `TestCleanBraceBound` (`{a,b,…×13}` ×8 rejected; `{a,b}` ×8 = 256 accepted), `TestCleanRuneLength` (150 × "я" accepted, 257 runes rejected), `TestRenderWhitespaceGate`.
- Commit message: `fix(pathset,layout): bound brace expansion, single overlap implementation`.

### Task F3: plan

- ✅ `Extract`: skip a leading UTF-8 BOM (offsets of `Block.Start/End` stay relative to the original bytes).
- `Plan.Version` becomes `*int`: absent = ok, explicit `0` → `*SchemaError` ("version must be 1").
- `Parse`: limit checks run before the version check (E0 wins over E1).
- Tests: `TestExtractBOM`, `TestParseVersionZero`, `TestParseLimitBeforeVersion`.
- Commit message: `fix(plan): BOM, explicit version, limits first`.

### Task F4: fsx

- ✅ New package `internal/fsx`: `const MaxFile = 1 << 20`; `func OpenRegular(name string) (*os.File, os.FileInfo, error)` — Lstat must be a regular file (not symlink/FIFO/device/dir), open, `f.Stat` + `os.SameFile` with the Lstat result; `ErrNotRegular`; `func ReadRegular(name string, max int64) ([]byte, error)` — OpenRegular + read through `io.LimitReader(max+1)`, `ErrTooLarge` when over. On unix open with `O_NONBLOCK` (build-tagged helper) so a swapped-in FIFO cannot block.
- Tests: regular, symlink, FIFO (not on windows, quick), directory, too large, missing.
- Commit message: `feat(fsx): safe regular-file reader`.

### Task F5: progress

- ✅ Wave ids truncate at 32 runes (the id limit), not 12; the task-id column absorbs the width so lines stay ≤ 72 columns; spec example unchanged byte for byte.
- Test `TestRenderLongWaveIDs` (`backend-api-01`/`-02` distinct).
- Commit message: `fix(progress): keep wave ids distinct`.

### Task F6: packaging

- ✅ `.gitattributes`: `* text=auto eol=lf`, `bin/paraplane text eol=lf`, `*.go`, `*.md`, `*.json`, `*.jsonl`, `*.yml`, `*.yaml` → `text eol=lf`.
- `ship.txt`: one path per line — `.claude-plugin .codex-plugin .agents .gitattributes hooks skills agents bin libexec README.md README.ru.md LICENSE`; Makefile target `tree DIR=…` copies it (without libexec when absent); ci.yml strict-validate step, release.yml tar and `git add -f` read the list from `ship.txt`.
- release.yml: after extracting the archive, `libexec/paraplane-linux-amd64 version` and `sh bin/paraplane version` must print the stamped version; ldflags `-X main.version=${GITHUB_REF_NAME#v}` (no `v`); reword the comment that claims `--strict` checks skill frontmatter.
- ci.yml: `docs/**` out of `paths-ignore`; add `go test -race ./...` (ubuntu).
- bin/paraplane: buffer the binary's stdout for every hook and print only on exit 0; the `enabled: false` check walks up from `${CLAUDE_PROJECT_DIR:-$PWD}` to the nearest dir with `.git` or `.paraplane/config.yaml`.
- `internal/plugintest/packaging_test.go`: `.gitattributes` exists and sets `eol=lf` for `bin/paraplane`; every path in `ship.txt` exists (except `libexec`); README.md/README.ru.md pairs exist in every dir that has a README.md.
- Commit message: `build: LF endings, single ship list, release smoke test`.

### Task F7: rules

- W3 text: `` `N wave(s)` `` grammar ("1 wave instead of 2", "3 waves instead of 4"); mention `paraplane check --suggest` in backticks (so the Codex replacer finds it) only as "see the shorter layout below" when `Suggested` will be printed is not known to rules — keep "run `paraplane check --suggest <file>`" but backticked.
- Adapt to `plan.Version *int` and the removed `layout.Suggest/Conflicts` if referenced.
- Commit message: `fix(rules): W3 wording`.

### Task F8: config

- `Load` reads through `fsx.ReadRegular(…, MaxSize)` (FIFO/symlink → treated as a broken config error, never a hang).
- Table test of every key with a valid value (asserting the field), `max_parallel` 0/1/32/33, `default_gate` with newline, `plans_dir: ..`, `key: null` keeps the default; FIFO config → error returned quickly (not on windows); `TestFindRoot` checks `MkdirAll` errors and uses `GIT_CEILING`-independent temp layout.
- Commit message: `fix(config): never block on special files; key tests`.

### Task F9: hooks

- Replace `postReadFile`/`codexReadTail` open logic with `fsx` (tail read keeps its 4 MiB seek); rename shared helpers out of the `post` prefix (`readPlan`, `planFindings`, …) into a `plan_text.go`; constants once.
- Suggested layout larger than the cap: omit it and say ``run `paraplane check --suggest <file>` for the shorter layout`` instead of truncating the block.
- Codex: route every text through `env.Agent.text` (W3 command becomes the absolute CLI path); `apply_patch *** Update File:` and `sed -i` use Edit semantics (no warnings, block only when the change touches the block or its keys); `cp/mv -t DIR` marks DIR, not the sources; `$a$b` finds both mentions; PermissionRequest/Bash approval under Codex accepts only the absolute `Codex.CLI()` form (bare name never auto-approved for Codex); empty `CLAUDE_PLUGIN_ROOT` → shadow check fails closed (no auto-approve).
- Tests: the uncovered branches listed by the audit (`codexMaxPlans`, unterminated quote, `&>`, big transcript without newline, `patchText` fallbacks), `TestOptions` via `postNow`, `TestDisabled` table over every handler, golden helper tolerant to CRLF; `Agent` zero-value comment fixed; `cmp.Or` instead of `firstNonEmpty`.
- Commit message: `fix(hooks): safe reads via fsx, Codex texts and edits, stricter approval`.

### Task F10: cmd

- `readPlanFile` via `fsx`; `check --suggest` prints the whole layout (no 64 KB cap); `getwd` failure for non-hook commands prints `error:` to stdout like the rest.
- Tests: getwd failure (hook branch falls back to `CLAUDE_PROJECT_DIR`), stdin read error, stdout write error in `runHook`, `--suggest` on a 103 KB plan prints a complete block; not-a-repo tests hermetic (`GIT_CEILING_DIRECTORIES`).
- Commit message: `fix(cmd): fsx reads, full suggested layout`.

### Task F11: skills and docs

- skills/create: write the draft to its final path before the auditor step and pass the path; skills/check: "Repeat steps 4–5"; skills/rules: case-insensitive FS limitation (spec §13) and brace bound in references/format.md; references/execution.md: "local or worktree" scope wording.
- README pairs: git filters — only global (user) config is trusted, system config is not read; `make dist` before `--plugin-dir`; links to the spec as GitHub URLs (`https://github.com/Comprise/paraplane/blob/main/docs/...`) so they work on the release branch; Codex: Stop blocking not yet verified live (spec §13); W3 sample text updated to the new wording.
- Commit message: `docs: audit fixes`.
