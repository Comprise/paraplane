# Example plan

A complete plan with a valid block. The outer four-backtick fence keeps the inner block from being
read as a plan block of this file; copy only the contents of the outer fence.

````markdown
# Notes service Implementation Plan

**Goal:** Store Markdown notes, render them to HTML and serve them over HTTP and from a CLI.

**Architecture:** `internal/store` keeps notes as JSON files in a directory (stdlib only);
`internal/render` turns Markdown into HTML with goldmark (the only new dependency); `internal/api`
serves both; `cmd/notes` is the CLI; `cmd/server/main.go` wires the handlers.

## Execution rules

- Dispatch all tasks of a wave in one message; each subagent changes only its `owns` and stops and
  reports when it needs anything else.
- Subagents do not commit. After each wave: `paraplane audit docs/plans/2026-10-20-notes.md <wave>`,
  then the gate; the coordinator commits once per wave with the `Paraplane-Plan` and `Paraplane-Wave`
  trailers (once per stage, listing every wave, only when the project's rules require it) and shows
  `paraplane progress docs/plans/2026-10-20-notes.md`.

## Waves

Why this layout: the only new dependency, goldmark, is added to `go.mod`/`go.sum` by one integrator
task (T2), so no other task touches the hot files. The store uses only the standard library, so it
compiles without T2 and runs next to it in W1. The renderer imports goldmark and goes after T2; the
CLI needs only the store, so it runs next to the renderer instead of after the API (the original
step 4 moved up).
Route registration in `cmd/server/main.go` belongs to the API task, the only one that changes it.

```paraplane
version: 1
gate: "go vet ./... && go test ./..."
waves:
  - id: W1
    tasks:
      - id: T1
        title: "Note store"
        owns: ["internal/store/**"]
        verify: "go test ./internal/store/"
      - id: T2
        title: "Add the goldmark dependency"
        owns: [go.mod, go.sum]
        verify: "go mod verify"
  - id: W2
    tasks:
      - id: T3
        title: "Markdown renderer"
        owns: ["internal/render/**"]
        reads: [go.mod]
        depends: [T2]
        verify: "go test ./internal/render/"
      - id: T4
        title: "CLI command"
        owns: ["cmd/notes/**"]
        reads: [internal/store/store.go]
        depends: [T1]
        verify: "go test ./cmd/notes/"
  - id: W3
    gate: "go vet ./... && go test ./... && go test -tags e2e ./e2e/"
    tasks:
      - id: T5
        title: "HTTP handlers and routes"
        owns: ["internal/api/**", cmd/server/main.go]
        reads: [internal/store/store.go, internal/render/render.go]
        depends: [T1, T3]
        verify: "go test ./internal/api/ ./cmd/server/"
      - id: T6
        title: "CLI documentation"
        owns: ["docs/cli/*.md"]
        reads: [cmd/notes/main.go]
        depends: [T4]
        verify: "test -s docs/cli/notes.md"
```

### Task 1: Note store

- [ ] **Step 1:** `internal/store/store.go`: `type Note struct { ID int64; Title, Body string }`,
  `Open(dir string) (*Store, error)`, `Put`, `Get`, `List`, one JSON file per note via
  `encoding/json` and `os` (no third-party packages); tests in `store_test.go` with `t.TempDir()`.
- [ ] **Step 2:** `go test ./internal/store/` passes.

### Task 2: Dependencies

- [ ] **Step 1:** `go get github.com/yuin/goldmark`; `go mod verify`.

### Task 3: Markdown renderer

- [ ] **Step 1:** `internal/render/render.go`: `HTML(md string) (string, error)` on goldmark; table
  tests in `render_test.go`.

### Task 4: CLI command

- [ ] **Step 1:** `cmd/notes/main.go`: `notes add <title>` (body from stdin), `notes list`,
  `notes show <id>` on `store.Store`; tests in `main_test.go`.

### Task 5: HTTP handlers and routes

- [ ] **Step 1:** `internal/api/api.go`: `GET /notes`, `GET /notes/{id}` (rendered HTML),
  `POST /notes`; tests with `httptest`.
- [ ] **Step 2:** `cmd/server/main.go`: open the store, register the handlers, listen on `:8080`.

### Task 6: CLI documentation

- [ ] **Step 1:** `docs/cli/notes.md`: every command of `cmd/notes/main.go` with an example.
````

What makes it valid:

- one top-level ```` ```paraplane ```` block, globs in quotes, literal paths unquoted;
- a gate for every wave (`gate` at the top, W3 overrides it);
- `depends` only on tasks of earlier waves;
- no two tasks of one wave own overlapping paths, and no task reads what another task of its wave
  owns (T3 reads `go.mod`, owned by T2 in an earlier wave);
- the hot files `go.mod` and `go.sum` are named literally and owned by one task;
- no task changes files owned by an earlier wave;
- every `depends` follows a real contract: T1 needs no new dependency, so it does not wait for T2;
- the layout is already the shortest: T3 needs T2, T5 needs T3, so three waves are required.
