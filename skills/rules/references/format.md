# The paraplane block

## Where the block lives

- A fenced code block whose info string is `paraplane`: backticks or tildes, fence length 3 or more.
  The first word of the info string must be `paraplane`.
- Exactly one such block per plan. None, or two, is E0.
- Only top-level fences count. A fence indented by 4 or more spaces (for example, inside a list item)
  is not a block, so the plan has no block (E0). Put the block at the left margin, outside lists.
- Fences nested in a longer fence are not scanned: a ```` ```paraplane ```` example inside a
  ````` ````markdown ````` fence is an example, not the plan block. A plan may quote examples this way.
- An unclosed fence runs to the end of the document.

## Schema

```yaml
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

- Plan keys: `version`, `gate`, `waves`. Wave keys: `id`, `gate`, `tasks`. Task keys: `id`, `title`,
  `owns`, `reads`, `depends`, `verify`. Any other key is E1, so a typo such as `depnds` never silently
  drops a dependency. A wrong type is E1 too.
- `id` matches `[A-Za-z][A-Za-z0-9_.-]{0,31}`. Tasks and waves share one namespace: an id may not be
  reused, and `depends` on a wave id is E1 "unknown task".
- `depends` names tasks of earlier waves only (E3 otherwise).
- Gate of a wave: `wave.gate`, else `plan.gate`, else `default_gate` from `.paraplane/config.yaml`,
  else none — E7.

## Limits

They protect the hook from a hostile plan; exceeding any is E0.

- block ≤ 256 KB;
- ≤ 500 tasks;
- ≤ 100 paths (`owns` + `reads`) per task;
- ≤ 2000 paths (`owns` + `reads`) per plan;
- pattern ≤ 256 characters (runes, not bytes);
- ≤ 8 `{}` groups per pattern;
- ≤ 256 variants of brace expansion per pattern (the product of the alternative counts of its `{}`
  groups), because one `doublestar.Match` call on many groups can run for tens of seconds.

## Paths and globs

- Relative to the project root, separator `/`. No `..` segment, no leading `/`, no drive (`X:`), not
  `.`. Paths are normalized with `path.Clean`. A backslash or an absolute path is E1.
- Glob syntax is `doublestar`: `*` (within a segment), `**` (any number of segments), `?`, `[...]`,
  `{a,b}`.
- Inside `{}` and `[]` the character `/` is forbidden, and so are classes that can match `/`: negation
  `[!…]`/`[^…]` and a range `lo-hi` with `lo ≤ '/' ≤ hi`. Otherwise one segment of a pattern could cover
  several segments of a path. The alternative `..` inside `{}` (`{..,a}/x`) is forbidden like a `..`
  segment.
- **A literal path is a file.** To own a directory write `dir/**`. A literal that names an existing
  directory is W5 (reported only by the CLI and hooks, which have a filesystem).

## Quoting in YAML

Write in quotes every pattern that contains any of `* ? [ ] { } ,` or starts with `!` or `&`:

```yaml
owns: ["**/x.go", "cmd/*.go", "{api,web}/routes.go", "internal/store/**", internal/plan/plan.go]
```

Unquoted, YAML reads `*` as an alias, `?` as a mapping key, `&` as an anchor, and `[ ] { } ,` break
the flow list. A parse error containing `did not find expected alphabetic or numeric character` or
`did not find expected ',' or ']'` comes with the hint `quote glob patterns`. Quoting literal paths
too is harmless.

## Overlap of patterns

`paraplane` asks conservatively whether two patterns may describe one file:

1. Both literal — equal. One literal — the pattern matches the literal.
2. At least one contains `**`, or `*` right next to `{`, `,` or `}` (brace expansion can glue a `**`
   together) — they overlap when the static prefix of one (the segments before the first segment with
   a metacharacter) is a segment prefix of the other's. So `internal/**` overlaps
   `internal/store/store.go`, but `internal/api/**` does not overlap `internal/store/**`.
3. Otherwise each pattern segment covers exactly one path segment: the segment counts must be equal,
   and every pair of segments must admit a common string. A literal segment must match the other one;
   two segments of the form `*<lit>` overlap when one `lit` ends the other (`*.go` and `*_test.go`
   overlap, `*.go` and `*.ts` do not); anything else is assumed to overlap.

False alarms are possible (the message says "may overlap"); misses are not, with one exception: paths
are compared as exact strings, so on case-insensitive or normalizing filesystems (macOS, Windows)
`README.md` and `readme.md` (or NFC and NFD spellings) are different paths to the check though one
file. Give such files to one task by hand. Narrow the patterns to clear a false alarm.

Overlap drives E2 (`owns`/`owns` in one wave), E4 (`reads`/`owns` in one wave) and E6 (`owns` of a
later task against `owns` of an earlier task without a dependency).

## Hot files

A `hot_files` pattern marks a shared resource. Defaults: `go.mod`, `go.sum`, `**/package.json`,
`**/package-lock.json`, `**/pnpm-lock.yaml`, `**/yarn.lock`, `**/Cargo.toml`, `**/Cargo.lock`,
`**/pyproject.toml`, `**/uv.lock`, `**/poetry.lock`, `**/migrations/**`.

A task **touches** hot pattern `H` when its `owns` has an entry `p` such that `p == H`; or `p` is a
literal and `H` matches `p`; or `H` is a literal and `p` matches `H`.

- `web/package.json` touches `**/package.json`; `**` touches `go.mod` (it matches the literal).
- A broad pattern that does not match the hot file (`internal/**` and `go.mod`) does not touch it: to
  change `go.mod` a task names it.
- A pattern without `**` (`web/*.json`) does not touch a hot pattern either. Always name hot files by
  their literal path (`web/package.json`).
- Two tasks of one wave touching the same `H` are E5, even through different files:
  `migrations/0003_a.sql` and `migrations/0003_b.sql` clash on the migration number.
- `reads` of a hot file is not touching; reading `go.mod` that a task of the same wave owns is still
  E4.

Put every change to one hot file into a single integrator task, and let the tasks that need the
result depend on it.

## Shorter layout (W3)

For a plan without errors `paraplane check` builds the layout as soon as possible: every `depends`
plus, for each conflicting pair of tasks in different waves, an arc from the earlier one; each task
goes to the layer after its last predecessor. When that gives fewer layers than the plan has waves,
it reports W3, and `paraplane check --suggest` prints `Shorter layout:` and the ready block (waves
`W1…`, tasks unchanged, patterns quoted). The gate of a layer is the effective gate of the latest
author wave among its tasks. The suggested layout is not split by `max_parallel`, so it may carry W2.
