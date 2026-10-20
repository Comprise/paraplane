# paraplane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan **wave by wave** (rules below). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Плагин Claude Code `paraplane` на Go: проверяет, что любой план содержит блок ` ```paraplane `, пригодный для параллельного исполнения субагентами (владение файлами, волны, зависимости, gate), подсказывает более короткую раскладку, проверяет после волны, что субагенты не вышли за свои файлы, и показывает прогресс исполнения.

**Architecture:** Один бинарник `paraplane` (`check`, `audit`, `progress`, `hook <event>`, `version`) поверх чистых пакетов: `pathset` (пути, пересечение шаблонов, горячие файлы) → `plan` (извлечение и разбор блока) → `layout` (конфликты, раскладка) → `rules` (E0–E7, W1–W5, A1) → `hooks` → `cmd/paraplane`; `gitx` даёт изменённые файлы для аудита и коммиты волн для прогресса; `progress` рисует состояние. Упаковка как в auto-context: шим `bin/`, бинарники `libexec/`, skills, агент, `hooks/hooks.json`.

**Tech Stack:** Go 1.27, `gopkg.in/yaml.v3` v3.0.1, `github.com/bmatcuk/doublestar/v4` v4.10.3.

**Spec:** `docs/superpowers/specs/2026-10-20-paraplane-design.md` — читать вместе с планом; «п. N» ссылается на её разделы.

## Execution Rules (coordinator and every subagent)

1. **Pre-flight (координатор, до W1):** дописать в `.gitignore` строки `.claude/settings.local.json`, `.mcp.json`, `.serena/`, `/dist/`, `/libexec/`, `/paraplane`, `/paraplane.exe`; написать `go.mod` (`module paraplane`, `go 1.27`, `require` обеих зависимостей без `// indirect`); скопировать `go.sum` из `/home/elia/Projects/auto-context/go.sum`; `go mod verify`; `git add -A && git commit -m "chore: spec, plan and project config"`; `git status --porcelain` пуст. После этого `go.mod`/`go.sum` не меняет никто.
2. Все задачи волны запускаются **одним сообщением**. Промпт субагента: текст его задачи, Global Constraints, его `owns`, `reads`, `verify`.
3. Субагент меняет **только** файлы из своего `owns`. Нужен другой файл, или потребляемый интерфейс не совпадает с блоком Interfaces — **остановиться и сообщить**, не править чужой пакет и не добавлять в него помощников.
4. Субагенты **не коммитят**, не отмечают чекбоксы в плане, не запускают `go get`, `go mod tidy`, `go fmt ./...`, `gofmt -w .`; `gofmt -w` — только по своим файлам; `go build` — только с `-o` в каталог вне репозитория.
5. Golden-файлы в `testdata/` пишутся **вручную** из дословных текстов задачи, не генерируются из реализации; флагов `-update` нет.
6. После волны координатор: сверяет `git status` с объединением `owns` волны (W1–W4 вручную; в W5 — `go run ./cmd/paraplane audit docs/superpowers/plans/2026-10-20-paraplane.md W5`), затем запускает gate волны. Красный gate чинит субагент задачи, владеющей сломанным файлом в текущей волне; поломка в файле более ранней волны — правка плана, не молчаливый фикс.
7. Зелёный gate → координатор отмечает чекбоксы и делает **один коммит на волну** из сообщений коммитов её задач, с трейлерами `Paraplane-Plan: docs/superpowers/plans/2026-10-20-paraplane.md` и `Paraplane-Wave: <id>` (п. 7.1), затем показывает пользователю прогресс в формате п. 7.1 (до W5 — составленный вручную, после — `go run ./cmd/paraplane progress …`).
8. Проектный хук code-review-graph (`.claude/settings.json`) срабатывает на каждую правку каждого субагента — ожидаемы медленные хуки, на аудит не влияет (пишет в игнорируемый `.code-review-graph/`).
9. До конца W5 ничего не пушится (CI требует `README.md`, он появляется в W5).

## Global Constraints

- Модуль `paraplane` (`go 1.27`). Зависимости только stdlib, `gopkg.in/yaml.v3`, `github.com/bmatcuk/doublestar/v4`.
- Блок плана — огороженный код с info-строкой `paraplane`. Других публичных имён (кроме названных в спецификации) не вводить.
- Хуки всегда выходят с 0, ничего не пишут на диск; ошибки — в stderr, ответ пустой (fail-open).
- Пути в плане и конфиге — относительные, разделитель `/`; пути из ОС приводятся `filepath.ToSlash` до сравнения.
- Все тексты для Claude и пользователя — английские; текст из плана, конфига или ошибок YAML перед выводом проходит `textx.Line`/`textx.Block`.
- Автор — `Elia Samoylov`; репозиторий `https://github.com/Comprise/paraplane`; лицензия MIT.
- Платформы: linux, darwin, windows × amd64, arm64. Тесты — `go test ./...`, ФС — `t.TempDir()`.
- Версия: `var version = "dev"` в `cmd/paraplane`, задаётся `-ldflags "-X main.version=<tag>"`. В `plugin.json` на `main` поля `version` нет.

## Review Focus

- **Пример блока внутри более длинного ограждения** (````` ````markdown `````) — не блок плана. Тест в T6 (`TestExtractIgnoresNestedExample`); пример skill проходит `rules.Check` — тест в T17 (`TestRulesExamplePlan`).
- **Glob без кавычек в YAML** (`owns: [**/x]`) — E0 с подсказкой `quote glob patterns`, `Render` всегда пишет шаблоны в кавычках. Тесты в T6 (`TestParseAliasHint`) и T11 (`TestRenderRoundTrip`).
- **Обратный слэш, абсолютные пути, `/` внутри `{}`/`[]`** — E1; `file_path` с `\` в PostToolUse сопоставляется с `plan_paths`. Тесты в T5 (`TestCleanRejects`) и T13b (`TestPostToolUseWindowsPath`).
- **Корень проекта — подкаталог git-репозитория** — аудит сравнивает пути от корня проекта, файлы вне корня отбрасываются. Тест в T16 (`TestChangedSubdirRoot`).
- **Цикл и ссылки на неизвестные задачи** — `rules.Check` не вызывает `layout.Suggest`, не паникует. Тест в T12 (`TestCheckCycleNoSuggest`).

## Waves

```paraplane
version: 1
gate: "go mod tidy -diff && go vet ./... && test -z \"$(gofmt -l .)\" && go test ./... && sh -n bin/paraplane && jq . .claude-plugin/plugin.json .claude-plugin/marketplace.json >/dev/null"
waves:
  - id: W1
    tasks:
      - id: T2
        title: "Plugin manifests, shim, Makefile"
        owns: [".claude-plugin/**", "bin/**", Makefile]
        verify: "sh -n bin/paraplane && jq . .claude-plugin/plugin.json .claude-plugin/marketplace.json >/dev/null"
      - id: T3
        title: "CI and release workflows"
        owns: [".github/**"]
        verify: "for f in .github/workflows/*.yml; do python3 -c 'import sys,yaml;yaml.safe_load(open(sys.argv[1]))' $f || exit 1; done"
      - id: T4
        title: "internal/textx"
        owns: ["internal/textx/**"]
        verify: "go test ./internal/textx/"
      - id: T5
        title: "internal/pathset"
        owns: ["internal/pathset/**"]
        verify: "go test ./internal/pathset/"
      - id: T6
        title: "internal/plan"
        owns: ["internal/plan/**"]
        verify: "go test ./internal/plan/"
      - id: T16
        title: "internal/gitx"
        owns: ["internal/gitx/**"]
        verify: "go test ./internal/gitx/"
  - id: W2
    tasks:
      - id: T7
        title: "Skill rules"
        owns: ["skills/rules/**", skills/README.md]
        reads: [docs/superpowers/specs/2026-10-20-paraplane-design.md]
        verify: "grep -q '^name: rules$' skills/rules/SKILL.md"
      - id: T8
        title: "Skills create and check"
        owns: ["skills/create/**", "skills/check/**"]
        reads: [docs/superpowers/specs/2026-10-20-paraplane-design.md]
        verify: "grep -q '^disable-model-invocation: true$' skills/create/SKILL.md && grep -q '^disable-model-invocation: true$' skills/check/SKILL.md"
      - id: T9
        title: "Agent conflict-auditor"
        owns: ["agents/**"]
        verify: "grep -q '^name: conflict-auditor$' agents/conflict-auditor.md"
      - id: T10
        title: "internal/config"
        owns: ["internal/config/**"]
        reads: [internal/pathset/pathset.go]
        depends: [T5]
        verify: "go test ./internal/config/"
      - id: T11
        title: "internal/layout"
        owns: ["internal/layout/**"]
        reads: [internal/plan/plan.go, internal/pathset/pathset.go]
        depends: [T5, T6]
        verify: "go test ./internal/layout/"
      - id: T18
        title: "internal/progress"
        owns: ["internal/progress/**"]
        reads: [internal/plan/plan.go, internal/textx/textx.go]
        depends: [T6, T4]
        verify: "go test ./internal/progress/"
  - id: W3
    tasks:
      - id: T12
        title: "internal/rules"
        owns: ["internal/rules/**"]
        reads: [internal/layout/layout.go, internal/plan/plan.go, internal/pathset/pathset.go]
        depends: [T11]
        verify: "go test ./internal/rules/"
      - id: T19
        title: "internal/gitx: refuse repo-local filter drivers"
        owns: ["internal/gitx/**"]
        depends: [T16]
        verify: "go test ./internal/gitx/"
      - id: T13a
        title: "internal/hooks: base, session-start, skill, bash"
        owns: [internal/hooks/hooks.go, internal/hooks/session_start.go, internal/hooks/skill.go, internal/hooks/bash.go, internal/hooks/base_test.go, "internal/hooks/testdata/base_*"]
        reads: [internal/config/config.go, internal/textx/textx.go]
        depends: [T10, T4]
        verify: "go test ./internal/hooks/"
  - id: W4
    tasks:
      - id: T13b
        title: "internal/hooks: pre-tool-use, post-tool-use, hooks.json"
        owns: [internal/hooks/pre_tool_use.go, internal/hooks/post_tool_use.go, internal/hooks/options.go, internal/hooks/plan_test.go, "internal/hooks/testdata/plan_*", hooks/hooks.json]
        reads: [internal/rules/rules.go, internal/hooks/hooks.go, internal/hooks/session_start.go, internal/hooks/skill.go, internal/hooks/bash.go, internal/hooks/base_test.go]
        depends: [T12, T13a]
        verify: "go test ./internal/hooks/"
      - id: T17
        title: "internal/plugintest"
        owns: ["internal/plugintest/**"]
        reads: [internal/rules/rules.go, "skills/**", "agents/**", bin/paraplane]
        depends: [T2, T7, T8, T9, T12]
        verify: "go test ./internal/plugintest/"
  - id: W5
    gate: "go mod tidy -diff && go vet ./... && test -z \"$(gofmt -l .)\" && go test ./... && sh -n bin/paraplane && t=$(mktemp -d) && cp -r .claude-plugin hooks skills agents bin README.md LICENSE \"$t/\" && jq '.version=\"0.0.0-ci\"' .claude-plugin/plugin.json > \"$t/.claude-plugin/plugin.json\" && claude plugin validate --strict \"$t\" && go run ./cmd/paraplane check docs/superpowers/plans/2026-10-20-paraplane.md"
    tasks:
      - id: T14
        title: "cmd/paraplane"
        owns: ["cmd/**"]
        reads: [hooks/hooks.json, internal/hooks/pre_tool_use.go, internal/rules/rules.go]
        depends: [T13b, T16, T18, T19]
        verify: "go test ./cmd/paraplane/"
      - id: T15
        title: "README, CLAUDE.md, hooks/README.md"
        owns: [README.md, CLAUDE.md, hooks/README.md]
        reads: [hooks/hooks.json, docs/superpowers/specs/2026-10-20-paraplane-design.md]
        depends: [T13b, T2, T7, T8, T9]
        verify: "test -s README.md && test -s hooks/README.md && grep -q '<!-- code-review-graph MCP tools -->' CLAUDE.md"
```

Почему так: `go.mod`/`go.sum` готовы до W1 (pre-flight), поэтому листовые Go-пакеты стартуют сразу. Цепочка `pathset → layout → rules → hooks(b) → cmd` строго последовательна (каждый импортирует предыдущий) и задаёт 5 волн; всё остальное разложено вокруг неё. `internal/hooks` поделён по файлам: T13a не зависит от `rules` и идёт параллельно с T12; T13b не трогает файлы T13a. Номера задач сохранены из первой редакции (T1 упразднён — его работа в pre-flight).

---

### Task 2: Plugin manifests, shim, Makefile

**Files:** Create `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json`, `bin/paraplane` (исполняемый), `bin/README.md`, `Makefile`.

**Interfaces:** Produces: шим `bin/paraplane`, запускающий `libexec/paraplane-<os>-<arch>[.exe]`; `make test`, `make dist`.

- [x] **Step 1:** `plugin.json` и `marketplace.json` по образцу `/home/elia/Projects/auto-context/.claude-plugin/`: name `paraplane`, description `Makes every implementation plan safe for parallel subagents: file ownership, waves, dependencies and a gate per wave`, author `{"name": "Elia Samoylov"}`, license MIT, homepage/repository `https://github.com/Comprise/paraplane`, keywords `["planning", "parallel", "subagents", "plan-mode", "hooks"]`; marketplace source `{"source":"github","repo":"Comprise/paraplane","ref":"release"}`, owner `Elia Samoylov`. Без `version`.
- [x] **Step 2:** `bin/paraplane` на основе `/home/elia/Projects/auto-context/bin/auto-context`: тот же выбор платформы; **убрать** `active()`, вызов `hook-exit` и уведомления про `.auto-context/log`. Нет бинаря: для `hook session-start` — если в `${CLAUDE_PROJECT_DIR:-.}/.paraplane/config.yaml` нет строки `^enabled:[[:space:]]*false`, печатает `{"systemMessage":"paraplane: the binary for <os>/<arch> <problem> (<bin>), plan checks are off. Reinstall the plugin (it installs from the release branch).","hookSpecificOutput":{"hookEventName":"SessionStart"}}` (с JSON-экранированием как в образце); прочие хуки — тишина и 0; CLI — ошибка в stderr и 1. Бинарь есть: для `hook session-start` вывод буферизуется и печатается только при коде 0; для `hook …` любой ненулевой код → 0; остальное — `exec`.
- [x] **Step 3:** `Makefile` — копия `/home/elia/Projects/auto-context/Makefile` с заменой `auto-context` → `paraplane`. `bin/README.md` — 4–5 строк по образцу auto-context (русский).
- [x] **Step 4:** `chmod +x bin/paraplane`; `verify` задачи.
- [x] **Step 5:** Сообщение коммита: `feat: plugin manifests, shim and Makefile`.

### Task 3: CI and release workflows

**Files:** Create `.github/workflows/ci.yml`, `.github/workflows/release.yml`.

- [x] **Step 1:** Копии `/home/elia/Projects/auto-context/.github/workflows/{ci,release}.yml` с заменой `auto-context` → `paraplane`; в `cp -r`, `tar` и `git add -f` добавить `agents`; из `paths-ignore` убрать `context/**`; шаг «Shim syntax» — `sh -n bin/paraplane`; в job `test` добавить шаг `test -z "$(gofmt -l .)"` (только ubuntu).
- [x] **Step 2:** `verify` задачи.
- [x] **Step 3:** Сообщение коммита: `ci: test, validate and release workflows`.

### Task 4: internal/textx

**Files:** Create `internal/textx/textx.go`, `internal/textx/textx_test.go`. Можно взять за основу `/home/elia/Projects/auto-context/internal/textx/`.

**Interfaces:** Produces: `func Line(s string, max int) string` — `\r`, `\n`, `\t` → пробел, удаляет прочие управляющие (`unicode.IsControl`) и символы `Cf` (bidi), обрезает до `max` рун с суффиксом `…`. `func Block(s string, max int) string` — то же, но сохраняет `\n`, обрезает до `max` байт по границе руны с суффиксом `…`.

- [x] **Step 1:** Тесты `TestLine` (`"a\nb\x1b[31m"` → `"a b[31m"`, `"\u202eabc"` → `"abc"`, `Line("абвгд", 3)` → `"абв…"`), `TestBlock` (сохраняет `\n`, не режет руну).
- [x] **Step 2:** Реализация; `verify` → PASS.
- [x] **Step 3:** Сообщение коммита: `feat(textx): sanitize text for output`.

### Task 5: internal/pathset

**Files:** Create `internal/pathset/pathset.go`, `internal/pathset/pathset_test.go`.

**Interfaces:** Produces:
- `const MaxPatternLen = 256`, `const MaxBraceGroups = 8`
- `func Clean(p string) (string, error)` — ошибка с причиной для: пусто; `\`; ведущий `/`; `X:` в начале; сегмент `..` или альтернатива `..` в `{…}`; результат `path.Clean` == `.`; длина > 256; > 8 `{`; `/` внутри `{…}` или `[…]`; класс `[!…]`/`[^…]`; диапазон `lo-hi` в классе с `lo ≤ '/' ≤ hi`; `!doublestar.ValidatePattern`. Иначе `path.Clean(p)`.
- `func HasMeta(p string) bool` — есть один из `*?[{`.
- `func Overlap(a, b string) bool` — п. 4.1 спецификации (входы прошли `Clean`).
- `func Touches(owns []string, hot string) bool` — п. 4.2.
- `func Match(pattern, name string) bool` — `doublestar.Match`, ошибка → false.

- [x] **Step 1:** `TestCleanRejects`: `""`, `"src\\a.go"`, `"/etc/x"`, `"C:/x"`, `"a/../b"`, `"."`, `"./"`, 257 символов, `"{a,b}"`×9, `"[a"`, `"{a,b/c}/x"`, `"a[/]b"`, `"a[!x]b"`, `"[^a]"`, `"a[--0]b"`, `"{..,a}/x"` → ошибка; `"./a//b.go"` → `"a/b.go"`.
- [x] **Step 2:** `TestOverlap` (таблица): `a.go`/`a.go` true; `a.go`/`b.go` false; `internal/plan/**`/`internal/plan/x.go` true; `internal/**`/`internal/plan/*.go` true; `cmd/*.go`/`cmd/*_test.go` true; `src/*.go`/`src/*.ts` false; `a/*`/`a/b/*` false; `a/**`/`b/**` false; `**/package.json`/`web/package.json` true; `{a,b}/x.go`/`b/x.go` true; `{a,b}/*.go`/`a/*.go` true; `*{.go,.ts}`/`*.ts` true; `?.go`/`ab.go` false; `[ab].go`/`a.go` true; `**/*.go`/`docs/x.md` false; `internal/hooks/testdata/base_*`/`internal/hooks/testdata/plan_*` true (ложное срабатывание допустимо).
- [x] **Step 3:** `TestOverlapNoMisses` — свойство: шаблоны глубины ≤ 3 из сегментов `{a, b, *, **, *.go, *_test.go, *.ts, x.go, {a,b}, ?.go, [ab]}`, пути глубины ≤ 4 из сегментов `{a, b, x.go, a_test.go, b.ts, ab.go}`; для 3000 случайных пар (фиксированный seed): если есть путь, который `Match` обоими, то `Overlap` == true.
- [x] **Step 4:** `TestTouches`: `[go.mod]`/`go.mod` true; `["internal/**"]`/`go.mod` false; `["internal/**"]`/`"**/package.json"` false; `[web/package.json]`/`"**/package.json"` true; `["*"]`/`go.mod` true; `["**/migrations/**"]`/`"**/migrations/**"` true.
- [x] **Step 5:** Реализация; `verify` → PASS.
- [x] **Step 6:** Сообщение коммита: `feat(pathset): paths, pattern overlap and hot files`.

### Task 6: internal/plan

**Files:** Create `internal/plan/plan.go`, `internal/plan/extract.go`, `internal/plan/plan_test.go`.

**Interfaces:** Produces:
```go
type Plan struct {
	Version int    `yaml:"version"`
	Gate    string `yaml:"gate"`
	Waves   []Wave `yaml:"waves"`
}
type Wave struct {
	ID    string `yaml:"id"`
	Gate  string `yaml:"gate"`
	Tasks []Task `yaml:"tasks"`
}
type Task struct {
	ID      string   `yaml:"id"`
	Title   string   `yaml:"title"`
	Owns    []string `yaml:"owns"`
	Reads   []string `yaml:"reads"`
	Depends []string `yaml:"depends"`
	Verify  string   `yaml:"verify"`
}
const (
	MaxBlock      = 256 << 10
	MaxTasks      = 500
	MaxPaths      = 100
	MaxTotalPaths = 2000
)
var (
	ErrNoBlock    = errors.New("no ```paraplane block")
	ErrManyBlocks = errors.New("more than one ```paraplane block")
)
type SyntaxError struct{ Msg string } // YAML не разбирается → E0; Msg однострочный; если текст ошибки yaml.v3 содержит "did not find expected alphabetic or numeric character" или "did not find expected ',' or ']'", оканчивается "; quote glob patterns"
type LimitError struct{ Msg string }  // превышен лимит → E0
type SchemaError struct{ Msg string } // неизвестный ключ, неверный тип, version ∉ {0,1} → E1
// у всех трёх — Error() string
type Block struct {
	Body       []byte // содержимое между ограждениями
	Start, End int    // байтовые смещения всего блока (с ограждениями) в md
}
func Extract(md []byte) (Block, error)
func Parse(body []byte) (*Plan, error) // yaml.v3 Decoder.KnownFields(true)
func Load(md []byte) (*Plan, error)    // Extract + Parse
func (p *Plan) Tasks() []*Task          // все задачи в порядке автора
```
`Extract` — построчный сканер CommonMark-ограждений: открывающее ``` ``` ``` или `~~~` длины ≥ 3 с отступом ≤ 3 пробелов и info-строкой; закрывающее — тот же символ, длина ≥ открывающей, без info. Блок плана — только ограждение верхнего уровня, первое слово info которого `paraplane`; содержимое прочих ограждений не сканируется; незакрытое ограждение тянется до конца файла. Ошибки yaml.v3 многострочные — склеить в одну строку.

- [x] **Step 1:** Тесты: `TestExtract` (бэктики, тильды, ````-ограждение, `Start`/`End` охватывают ограждения); `TestExtractIgnoresNestedExample` (```paraplane внутри ````markdown → `ErrNoBlock`); `TestExtractMany` → `ErrManyBlocks`; `TestParseUnknownKey` (`depnds:` → `*SchemaError` с `depnds`); `TestParseAliasHint` (`owns: [**/x]`, `owns: [*.go]`, `owns: [a/{x}.go]` → `*SyntaxError`, оканчивается `quote glob patterns`); `TestParseLimits` (501 задача, 101 путь в задаче, 2001 путь в плане, тело > 256 КБ → `*LimitError`; `version: 2` → `*SchemaError`); `TestLoadOK` на блоке п. 4 спецификации → 2 волны, 3 задачи, `T3.Depends == ["T2"]`.
- [x] **Step 2:** Реализация; `verify` → PASS.
- [x] **Step 3:** Сообщение коммита: `feat(plan): extract and parse the paraplane block`.

### Task 16: internal/gitx

**Files:** Create `internal/gitx/gitx.go`, `internal/gitx/gitx_test.go`.

**Interfaces:** Produces: `func WaveCommits(root, planPath string) ([]string, error)` — `git -C root log --reverse --format=%(trailers:key=Paraplane-Plan,key=Paraplane-Wave,unfold)%x1e` текущей ветки; из каждого коммита, у которого трейлер `Paraplane-Plan` равен `planPath` (путь от корня через `/`), берётся значение `Paraplane-Wave`; значения не по шаблону id `[A-Za-z][A-Za-z0-9_.-]{0,31}` отбрасываются; возвращает id волн без дублей в порядке первого появления; репозиторий без коммитов (ошибка `git log` при пустом `HEAD`) → пустой список, не ошибка. И `func Changed(root string) ([]string, error)` — `git -C root status --porcelain=v1 -z --untracked-files=all`; для `R`/`C` оба пути; пути приводятся к `root`: префикс `git -C root rev-parse --show-prefix` срезается, пути вне него отбрасываются; разделитель `/`, без дублей, отсортированы. Запуск git — с защитой по образцу `/home/elia/Projects/auto-context/internal/gitx/gitx.go` (`GIT_CONFIG_NOSYSTEM=1`, `-c core.fsmonitor=false`, без пейджера, таймаут 10 с). Нет git или не репозиторий — ошибка.

- [x] **Step 1:** Тесты в `t.TempDir()` с `git init` и начальным коммитом: `TestChangedModifiedUntracked`, `TestChangedRename` (оба пути), `TestChangedSpacesInName`, `TestChangedSubdirRoot` (root = `repo/sub`: изменения в `sub/a.go` → `a.go`, в `other/b.go` → отброшено), `TestChangedNotRepo` → ошибка; `TestWaveCommits` (коммиты с трейлерами двух планов и без трейлеров → волны только нужного плана, по порядку; без коммитов → пусто).
- [x] **Step 2:** Реализация; `verify` → PASS.
- [x] **Step 3:** Сообщение коммита: `feat(gitx): changed files and wave commits`.

### Task 18: internal/progress

**Files:** Create `internal/progress/progress.go`, `internal/progress/progress_test.go`.

**Interfaces:** Consumes: `plan.Plan`. Produces:
```go
// Render — п. 7.1 спецификации. name — имя файла плана для заголовка; done — id волн с коммитом
// (из gitx.WaveCommits); id из done, которых нет в плане, дают строку "warning: commit for unknown wave <id>".
func Render(p *plan.Plan, name string, done []string) string
```
Раскладка, состояния и предупреждения — ровно по п. 7.1 (ширина 72 колонки; эмодзи — 2 колонки, прочие символы — 1); имя файла и id проходят `textx.Line`.

- [x] **Step 1:** Тесты (ожидаемый текст — вручную, байт в байт): `TestRenderSpecExample` (синтетический план п. 7.1: 5 волн по 6/5/2/2/2 задачи T1…T17, done W1 W2 → ровно пример п. 7.1); `TestRenderNothingDone` (W1 `running`, `0%`, полоса из `░`); `TestRenderAllDone` (`wave 5/5 · 100%`, все `✓ done`); `TestRenderManyTasks` (волна из 15 задач → 12 квадратов и `+3`); `TestRenderUnknownWave` (warning), `TestRenderCommittedOutOfOrder` (done W1, W3 → W2 running, W3 waiting, warning); `TestRenderWidth` (длинные id и имя файла → строки ≤ 72 колонок, `…`).
- [x] **Step 2:** Реализация; `verify` → PASS.
- [x] **Step 3:** Сообщение коммита: `feat(progress): render plan progress`.

### Task 19: internal/gitx — refuse repo-local filter drivers

**Files:** Modify `internal/gitx/gitx.go`, `internal/gitx/gitx_test.go`.

**Interfaces:** Produces: `var ErrFilterDrivers = errors.New("the repository config defines filter drivers; paraplane audit does not run git status here")`; `Changed` сначала запускает `git -C root config --local --get-regexp "^filter\\..*\\.(clean|smudge|process)$"` (код 1 без вывода — фильтров нет) и при любом совпадении возвращает `ErrFilterDrivers`, не запуская `status`; `status` получает `--ignore-submodules=all`. `WaveCommits` не меняется. Комментарий о защите в `run` не должен обещать больше, чем делается (п. 5.1 спецификации).

- [ ] **Step 1:** Тесты: `TestChangedRefusesFilterDriver` (в репозитории `git config filter.x.clean "touch PWNED"` и `.gitattributes` `* filter=x` → `errors.Is(err, ErrFilterDrivers)`, файла `PWNED` нет); `TestChangedIgnoresSubmodules` (изменение внутри подмодуля не попадает в список и не запускает git в нём — достаточно, что подмодуль с грязным деревом не даёт записей).
- [ ] **Step 2:** Реализация; `verify` → PASS.
- [ ] **Step 3:** Сообщение коммита: `fix(gitx): never run repo-configured filter drivers`.

### Task 7: Skill rules

**Files:** Create `skills/rules/SKILL.md`, `skills/rules/references/format.md`, `skills/rules/references/execution.md`, `skills/rules/examples/plan.md`, `skills/README.md`.

- [x] **Step 1:** `SKILL.md` frontmatter: `name: rules`; `description: This skill should be used whenever Claude writes, revises or reviews an implementation plan — in plan mode before ExitPlanMode, with superpowers:writing-plans, or in any docs/plans file — or when a paraplane hook reports plan errors (E0–E7), warnings (W1–W5) or audit errors (A1). Defines the paraplane block and the rules that let subagents execute waves in parallel without conflicts.` Тело (≤ 1500 слов, английский, повелительное наклонение): алгоритм (1. выписать изменения файлов по шагам; 2. назначить `owns`, вынося общие и горячие файлы в отдельные задачи-интеграторы; 3. `depends` по реальным контрактам, `reads` — на что задача опирается; 4. разложить по волнам ASAP, переставляя этапы; 5. gate на каждую волну — полный прогон, не только новые тесты; 6. `paraplane check --suggest <file>`; 7. при ≥ 2 задачах в какой-либо волне — агент `paraplane:conflict-auditor`); правила кавычек для glob (п. 4: каждый шаблон с `* ? [ ] { } ,` или начинающийся с `!`/`&` — в кавычках); горячие файлы называть литеральным путём (п. 4.2); таблица E0–E7, W1–W5, A1 с исправлением каждого; раздел «Precedence»: для плана с блоком ```paraplane правила исполнения paraplane главнее указаний любого skill исполнения (`superpowers:subagent-driven-development`, `superpowers:executing-plans` и т.п.) — указания вроде «never dispatch implementers in parallel» или «the implementer commits» для такого плана не действуют; ссылки на references.
- [x] **Step 2:** `references/format.md` — схема блока, лимиты, glob, кавычки, литерал = файл, пересечение и горячие файлы (п. 4, 4.1, 4.2), предупреждение: ограждение с отступом ≥ 4 пробелов (внутри элемента списка) не считается блоком. `references/execution.md` — правила исполнения: промпт субагента с `owns`/`reads`/`verify`; запрет правок вне `owns` и «остановись и сообщи»; задачи волны одним сообщением; субагенты не коммитят, не отмечают чекбоксы, `gofmt` только по своим файлам; после волны `paraplane audit <plan> <wave>`, затем gate; нарушение аудита или красный gate чинится до следующей волны; один коммит координатора на волну с трейлерами `Paraplane-Plan`/`Paraplane-Wave`, после него — `paraplane progress <plan>` и его вывод в сообщении пользователю в блоке кода. `examples/plan.md` — полный пример плана (проза + блок с 3 волнами, `reads`, `depends`, кавычками у glob) внутри 4-бэктикового ограждения; блок должен проходить `paraplane check` без ошибок.
- [x] **Step 3:** `skills/README.md` — таблица skills и агента по образцу auto-context (русский).
- [x] **Step 4:** Сообщение коммита: `feat(skills): rules`.

### Task 8: Skills create and check

**Files:** Create `skills/create/SKILL.md`, `skills/check/SKILL.md`.

- [x] **Step 1:** `create`: `description: This skill should be used when the user runs /paraplane:create to build a new implementation plan that is ready for parallel subagents.`, `argument-hint: <task description or path to a spec>`, `disable-model-invocation: true`, `allowed-tools: Bash(paraplane check *)`. Тело: загрузить `paraplane:rules`; уточнить неясное; изучить код; составить план с блоком; агент `paraplane:conflict-auditor`; исправить; `paraplane check --suggest <file>` до `plan ok`; сохранить в `plans_dir` из `.paraplane/config.yaml` (по умолчанию `docs/plans`) как `YYYY-MM-DD-<slug>.md`; отчёт: волны, задачи, что переставлено.
- [x] **Step 2:** `check`: `description: This skill should be used when the user runs /paraplane:check to validate an existing plan and restructure it for parallel execution.`, `argument-hint: [plan file]`, `disable-model-invocation: true`, `allowed-tools: Bash(paraplane check *)`. Тело: без аргумента — самый новый `.md` в `plan_paths`; `paraplane check --suggest`; агент-аудитор; правка самого плана (блок и проза синхронно); повторный check до `plan ok`; отчёт: исправленные E/W, перестановки этапов и причины. `error:` в выводе или код 2 — сообщить и остановиться.
- [x] **Step 3:** Сообщение коммита: `feat(skills): create and check`.

### Task 9: Agent conflict-auditor

**Files:** Create `agents/conflict-auditor.md`.

- [x] **Step 1:** Frontmatter: `name: conflict-auditor`; `description` с 2–3 `<example>` (после составления плана в `/paraplane:create`; при `/paraplane:check`; «проверь, не столкнутся ли задачи волны W2»); `tools: Read, Grep, Glob`; `model: inherit`; `color: cyan`.
- [x] **Step 2:** Системный промпт: вход — путь к плану; для каждой волны с ≥ 2 задачами найти по коду `shared-file` (обе задачи неизбежно правят файл вне своих `owns`: регистрация роутов, `main.go`, `index.ts`, DI-контейнер, `go.mod` при новой зависимости), `hidden-write` (кодогенерация, форматтеры, миграции с номерами), `api-break` (задача меняет сигнатуру, которую использует другая задача волны), `order` (задаче нужен результат задачи без `depends`); каждую находку подтверждать путём и строкой; ничего не править. Вывод строго:
  ```
  verdict: clean|conflicts
  - kind: shared-file
    tasks: [T2, T5]
    file: internal/server/routes.go
    evidence: "routes.go:40 registers every handler"
    fix: "move route registration into a new integrator task in W3 that depends on T2 and T5"
  ```
- [x] **Step 3:** Сообщение коммита: `feat(agents): conflict-auditor`.

### Task 10: internal/config

**Files:** Create `internal/config/config.go`, `internal/config/config_test.go`.

**Interfaces:** Consumes: `pathset.Clean`. Produces:
```go
const FileName = ".paraplane/config.yaml"
const MaxSize = 1 << 20
type Config struct {
	Enabled     bool
	Mode        string // "block" | "warn"
	PlanPaths   []string
	HotFiles    []string
	DefaultGate string
	MaxParallel int
	PlansDir    string
	AuditIgnore []string
}
func Default() Config            // значения п. 9 спецификации, дословно
type Problems []string           // Error(): FileName + ": ignored " + strings.Join(p, "; ")
func Load(root string) (Config, error) // нет файла → Default(), nil; не читается/не YAML/> MaxSize → Default(), err; неверные поля → остальные применены, Problems
func FindRoot(dir string) string  // ближайший вверх каталог с .git или FileName; иначе dir
```
- [x] **Step 1:** Тесты: `TestDefault` (все значения п. 9), `TestLoadMissing`, `TestLoadPartial` (`mode: loud`, `max_parallel: 99`, `colour: x`, `../x` в `audit_ignore` → 4 проблемы; `enabled: false` применён), `TestLoadBroken` (`: :`), `TestFindRoot`.
- [x] **Step 2:** Реализация: разбор в `map[string]yaml.Node`, каждое поле проверяется отдельно; шаблоны — через `pathset.Clean`. `verify` → PASS.
- [x] **Step 3:** Сообщение коммита: `feat(config): .paraplane/config.yaml`.

### Task 11: internal/layout

**Files:** Create `internal/layout/layout.go`, `internal/layout/layout_test.go`.

**Interfaces:** Consumes: `plan.Plan`, `pathset.Overlap`, `pathset.Touches`. Produces:
```go
type Kind string
const (
	OwnsOverlap Kind = "owns"
	ReadsOwns   Kind = "reads"
	HotFile     Kind = "hot"
)
type Conflict struct {
	A, B string // id задач. owns/hot: A раньше B в порядке автора. reads: A — читающая, B — владеющая (порядок автора не соблюдается)
	Kind Kind
	Path string // owns: шаблон owns A; reads: шаблон reads A; hot: шаблон hot_files
	With string // owns/reads: шаблон owns B; hot: ""
}
func Conflicts(p *plan.Plan, hot []string) []Conflict // все пары задач плана; шаблоны разбиваются на сегменты один раз
func Suggest(p *plan.Plan, hot []string) [][]string   // п. 6; вход — план без ошибок; = SuggestWith(p, Conflicts(p, hot))
func SuggestWith(p *plan.Plan, cs []Conflict) [][]string // то же по уже посчитанным конфликтам (rules считает их один раз)
var ErrDeadline = errors.New("plan is too complex to check in time; simplify brace patterns or split the plan")
func ConflictsWithin(p *plan.Plan, hot []string, deadline time.Time) ([]Conflict, error) // как Conflicts, но проверяет часы не реже чем раз в 256 пар и возвращает ErrDeadline; нулевой deadline — без ограничения
func Render(p *plan.Plan, layers [][]string) string   // блок ```paraplane с ограждениями, version: 1, все шаблоны и строки в двойных кавычках; plan.gate вверху; gate слоя — действующий gate самой поздней волны автора среди его задач, пишется только если отличается от plan.gate (п. 6)
```
- [x] **Step 1:** Тесты: `TestConflicts` (owns; reads в обе стороны с проверкой направления A=читающая; hot: две задачи с `go.mod`; `internal/a/**` и `internal/b/**` при дефолтных hot_files — нет конфликтов); `TestSuggestReorders` (автор: W1 {T1}, W2 {T2 dep T1}, W3 {T3 без связей} → `[[T1 T3] [T2]]`); `TestSuggestKeepsConflictOrder` (T2 в W2 читает файл, которым владеет T1 в W1, без depends → T1 раньше T2); `TestSuggestNeverLonger` — свойство на 500 случайных планах генератора: `depends` только на более ранние волны; в одной волне нет конфликтов; каждое пересечение `owns` разных волн подкреплено транзитивной зависимостью; результат — слоёв ≤ волн автора, все зависимости и конфликты разведены по слоям в порядке автора; `TestRenderRoundTrip` (шаблоны `"**/x"`, `"{a,b}/x"`, `"*.go"`; `plan.Load(Render(...))` даёт те же задачи; gate по п. 6: без `plan.gate`, с `plan.gate`, с `plan.gate` и переопределением у одной волны — переопределение сохраняется в слое её задач).
- [x] **Step 2:** Реализация; `verify` → PASS.
- [x] **Step 3:** Сообщение коммита: `feat(layout): conflicts and shortest layout`.

### Task 12: internal/rules

**Files:** Create `internal/rules/rules.go`, `internal/rules/audit.go`, `internal/rules/rules_test.go`.

**Interfaces:** Consumes: `plan.Load` и его ошибки, `pathset.Clean/Match`, `layout.ConflictsWithin/SuggestWith/Render/ErrDeadline` (конфликты считаются один раз и идут и в E2/E4/E5/E6, и в `SuggestWith`; пара с `go.mod` у обеих задач даёт и `owns`-, и `hot`-конфликт — E2 и E5 для одной пары допустимы, оба сообщаются). Produces:
```go
type Severity string
const (
	Error   Severity = "error"
	Warning Severity = "warning"
)
type Finding struct {
	Code     string // "E0"…"E7", "W1"…"W5", "A1"
	Severity Severity
	Where    string // id волны или задачи; "" для плана
	Msg      string
}
func (f Finding) String() string // "error E2 W1: T1 and T2 both own internal/a.go"; без Where — "error E0: no ```paraplane block"
type Options struct {
	HotFiles    []string
	DefaultGate string
	MaxParallel int                   // 0 → 6
	IsDir       func(rel string) bool // nil → W5 не проверяется
	Deadline    time.Time             // нулевой — без ограничения; истёк — одна находка E0 с текстом layout.ErrDeadline, остальное не сообщается
}
type Result struct {
	Plan      *plan.Plan // nil при E0
	Findings  []Finding  // по коду, затем по порядку волн/задач автора
	Suggested [][]string // не nil только при W3
}
func Check(md []byte, o Options) Result
func (r Result) Errors() int
func (r Result) Warnings() int
func (r Result) Summary() string // "plan ok: 3 waves, 7 tasks" | "plan ok with 2 warning(s): 3 waves, 7 tasks" | "plan invalid: 2 error(s), 1 warning(s)"
func Audit(p *plan.Plan, waveID string, changed []string, planPath string, ignore []string) ([]Finding, error)
// п. 5.1: changed и planPath — от корня через "/"; находки A1 (Error, Where = waveID) в порядке changed; неизвестная волна — ошибка
```
Порядок — п. 5 спецификации: E0 (`ErrNoBlock`, `ErrManyBlocks`, `*SyntaxError`, `*LimitError`) → остальное не проверяется; `*SchemaError` и ошибки `pathset.Clean` (путь отбрасывается) → E1; при E1/E3 — только E7, W1, W2, W4, W5; иначе E2/E4/E5 (пары одной волны из `layout.Conflicts`), E6, E7, W1–W5; W3 — только при `Errors() == 0`. Пересечение шаблонов, не равных буквально, — в тексте «may overlap».
- [ ] **Step 1:** Тесты: по одному на каждый код (`TestE0`…`TestW5`, ассерты на `Code`, `Where` и id в `Msg`); `TestE1WaveIDInDepends`; `TestE6TransitiveOK` (T3 меняет файл T1 при T3→T2→T1 — нет E6); `TestE7DefaultGate`; `TestCheckCycleNoSuggest` (T1↔T2 → E3, `Suggested == nil`, без паники); `TestCheckSpecExample` (блок п. 4 спецификации с hot_files п. 9, переписанными в тест списком → 0 ошибок); `TestSummary`; `TestAudit` (файл из `owns` задачи волны — ок; файл чужой волны — A1; файл плана и `audit_ignore` — ок; неизвестная волна — ошибка); `TestCheckFast` (500 задач по 4 шаблона, 4 волны → `Check` < 1 с); `TestCheckDeadline` (враждебный план из 8 групп `{a,aa}` и `Deadline` через 50 мс → ровно одна находка E0 с текстом `ErrDeadline`, `Check` возвращается < 1 с).
- [ ] **Step 2:** Реализация; `verify` → PASS.
- [ ] **Step 3:** Сообщение коммита: `feat(rules): plan validation and wave audit`.

### Task 13a: internal/hooks — base, session-start, skill, bash

**Files:** Create `internal/hooks/hooks.go`, `session_start.go`, `skill.go`, `bash.go`, `base_test.go`, `testdata/base_*.golden.json`.

**Interfaces:** Consumes: `config.Config/FileName`, `textx.Line`. Образец формы ответов и `shadowed()` — `/home/elia/Projects/auto-context/internal/hooks/` (`pre_tool_use.go`, `session_start.go`, `hooks.go`). Produces:
```go
type Input struct {
	SessionID      string          `json:"session_id"`
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	PermissionMode string          `json:"permission_mode"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
}
type Env struct {
	Root      string
	Config    config.Config
	ConfigErr error // ошибка чтения или config.Problems
}
const SessionContext = "…" // дословно ниже
const Digest = "…"         // дословно ниже
const ExecDigest = "…"     // дословно ниже
func SessionStart(env Env, in Input) ([]byte, error)
func skill(env Env, in Input) ([]byte, error) // для PreToolUse из T13b
func bash(env Env, in Input) ([]byte, error)  // для PreToolUse из T13b
func encodeJSON(v any) ([]byte, error)        // без HTML-экранирования и \n в конце
// Общие формы ответов — только здесь (hooks.go); T13b их использует, не объявляет свои:
type hookOutput struct {
	SystemMessage string       `json:"systemMessage,omitempty"`
	Decision      string       `json:"decision,omitempty"` // PostToolUse: "block"
	Reason        string       `json:"reason,omitempty"`
	Hook          *hookSpecific `json:"hookSpecificOutput,omitempty"`
}
type hookSpecific struct {
	EventName                string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}
// base_test.go — общие тестовые помощники для обоих тестовых файлов пакета:
// func golden(t *testing.T, name string, got []byte) // сравнивает с testdata/<name>.golden.json, без флага -update
// func testEnv(t *testing.T) Env                       // Env с t.TempDir() и config.Default()
```
Прочие неэкспортируемые имена в файлах T13a — с префиксом файла (`skill…`, `bash…`, `session…`), чтобы не столкнуться с T13b; из auto-context переносится только логика, не имена типов.
Тексты (дословно):
- `SessionContext`: ``paraplane: every implementation plan must contain one ```paraplane block (waves of tasks with owns, reads, depends, verify, and a gate per wave) and pass `paraplane check <file>`. Load skill paraplane:rules before writing or changing a plan.``
- `Digest`: ``paraplane rules apply to this plan: split the work into tasks, each with an exclusive owns list; group independent tasks into waves (no shared owns, no task reading what another task of its wave owns, at most one task per hot file such as go.mod or lock files); order by depends, not by the original step numbers; end every wave with a gate that runs the full checks. Put it in one ```paraplane block and run `paraplane check <file>`. Full rules: skill paraplane:rules.``
- `ExecDigest`: ``paraplane execution rules apply to any plan with a ```paraplane block and take precedence over this skill's instructions: dispatch every task of a wave in parallel (one message, one subagent per task, each limited to its owns); subagents never commit; after the wave run `paraplane audit <plan> <wave>`, then the wave's gate, and fix until both pass; then the coordinator makes one commit for the wave with trailers Paraplane-Plan: <plan path> and Paraplane-Wave: <wave id> and shows `paraplane progress <plan>` to the user. Full rules: skill paraplane:rules.``
- SessionStart при `ConfigErr`: `systemMessage` = `"paraplane: " + textx.Line(ConfigErr.Error(), 300) + " (defaults are used for those settings)."`, `additionalContext` = `SessionContext`.

Поведение: `Config.Enabled == false` → `nil, nil`. `skill`: имя из `tool_input.skill`, затем `name`, затем `command`; начинается с `paraplane:` → nil; иначе без учёта регистра содержит `execut` или `subagent-driven` → `additionalContext: ExecDigest`; иначе содержит `plan` → `additionalContext: Digest`. `bash`: `permissionDecision: "allow"`, `permissionDecisionReason: "paraplane CLI"` только если команда целиком совпала с `^paraplane (version|check( --suggest)? P|progress P|audit P [A-Za-z][A-Za-z0-9_.-]*)$`, где `P` = `[A-Za-z0-9_.][A-Za-z0-9_./-]*`, ни один аргумент не содержит `..`, и `paraplane` в PATH — это `$CLAUDE_PLUGIN_ROOT/bin/paraplane` (логика `shadowed()`).
- [ ] **Step 1:** Golden-тесты (golden — вручную): `TestSessionStart`, `TestSessionStartConfigProblem`, `TestSessionStartDisabled`, `TestSkillDigest` (`superpowers:writing-plans` → `Digest`; `superpowers:executing-plans` и `superpowers:subagent-driven-development` → `ExecDigest`; поле `name` вместо `skill` → digest; `paraplane:create` и `review` → nil), `TestBashAllow` (таблица: `paraplane check docs/plans/a.md`, `paraplane check --suggest a.md`, `paraplane audit docs/plans/a.md W2`, `paraplane progress docs/plans/a.md` → allow; `paraplane check ../x`, `paraplane check /etc/passwd`, `paraplane check a; rm -rf /`, `paraplane check a b` → nil; PATH-тень → nil).
- [ ] **Step 2:** Реализация; `verify` → PASS.
- [ ] **Step 3:** Сообщение коммита: `feat(hooks): session-start, skill digest, bash approval`.

### Task 13b: internal/hooks — pre-tool-use, post-tool-use, hooks.json

**Files:** Create `internal/hooks/pre_tool_use.go`, `post_tool_use.go`, `options.go`, `plan_test.go`, `testdata/plan_*.golden.json`, `hooks/hooks.json`. Файлы T13a не менять и их типы/помощники (`hookOutput`, `hookSpecific`, `encodeJSON`, `golden`, `testEnv`) не объявлять заново: нужна правка там — остановиться и сообщить. Свои неэкспортируемые имена — с префиксом `exitPlan…`, `post…`.

**Interfaces:** Consumes: T13a (`Input`, `Env`, `skill`, `bash`, `encodeJSON`), `rules.Check/Options/Result/Finding`, `layout.Render`, `plan.Extract/Block`, `pathset.Match`, `textx.Line/Block`. Produces:
```go
func PreToolUse(env Env, in Input) ([]byte, error)  // ExitPlanMode → exitPlan; Skill → skill; Bash → bash; иначе nil
func PostToolUse(env Env, in Input) ([]byte, error) // Write | Edit
func Options(env Env) rules.Options                  // из env.Config; IsDir — os.Stat под env.Root; Deadline = сейчас + 5 с
```
Тексты (дословно; `<findings>` — `Finding.String()` по строке, ≤ 20, затем `… N more`):
- Deny `ExitPlanMode`: `paraplane: the plan is not ready for parallel execution. Fix these and call ExitPlanMode again:\n<findings>\nFormat and rules: skill paraplane:rules.`
- Замечания: `paraplane: plan warnings (not blocking):\n<findings>`; при W3 добавить `\nShorter layout:\n<layout.Render>`.
- PostToolUse с E1–E7: `decision: "block"`, `reason`: `paraplane: <rel path> is not ready for parallel execution (fix it before the plan is final):\n<findings>\nFormat and rules: skill paraplane:rules.`
- PostToolUse с E0: `additionalContext`: `paraplane: <rel path> has no valid ```paraplane block yet (<E0 message>); add the block before the plan is final. Format: skill paraplane:rules.`

Поведение: `Enabled == false` → nil. **ExitPlanMode:** `plan` из `tool_input`; пусто — файл `planFilePath` (≤ 1 МБ, обычный файл); нет ни того, ни другого → nil; есть E (включая E0) → deny; только W → `additionalContext`; `mode: warn` → вместо deny `additionalContext` с тем же текстом. **PostToolUse:** `tool_input.file_path` → `filepath.ToSlash`, относительно `Root`; вне корня или не совпал с `plan_paths` → nil; файл ≤ 1 МБ. `Edit` проверяется, только если `old_string` или `new_string` содержит `paraplane` или ключ блока (`waves:`, `tasks:`, `id:`, `title:`, `owns:`, `reads:`, `depends:`, `verify:`, `gate:`), либо непустой `new_string` встречается (любое вхождение) внутри `[Block.Start, Block.End)`; `Write` — всегда. E0 → `additionalContext`; E1–E7 → `block` (при `mode: warn` — `additionalContext` с тем же текстом); только W → `additionalContext`, только для `Write`.

`hooks/hooks.json`:
```json
{
  "hooks": {
    "SessionStart": [{ "hooks": [{ "type": "command", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/bin/paraplane hook session-start", "timeout": 10 }] }],
    "PreToolUse": [
      { "matcher": "ExitPlanMode|Skill", "hooks": [{ "type": "command", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/bin/paraplane hook pre-tool-use", "timeout": 10 }] },
      { "matcher": "Bash", "hooks": [{ "type": "command", "if": "Bash(paraplane *)", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/bin/paraplane hook pre-tool-use", "timeout": 10 }] }
    ],
    "PostToolUse": [{ "matcher": "Write|Edit", "hooks": [{ "type": "command", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/bin/paraplane hook post-tool-use", "timeout": 10 }] }]
  }
}
```
- [ ] **Step 1:** Golden-тесты (golden — вручную): `TestExitPlanModeDeny`, `TestExitPlanModeNoBlockDeny`, `TestExitPlanModeOK` (nil), `TestExitPlanModeWarnings` (с `Shorter layout`), `TestExitPlanModeWarnMode`, `TestExitPlanModeFromFile`, `TestExitPlanModeNoPlan` (nil), `TestPostToolUseWriteErrors` (block), `TestPostToolUseWriteNoBlock` (E0 → additionalContext), `TestPostToolUseEditCheckbox` (правка `- [ ]` вне блока → nil), `TestPostToolUseEditInsideBlock` (block), `TestPostToolUseEditDeleteInBlock` (пустой `new_string`, `old_string` с `owns:` → проверка), `TestPostToolUseOtherFile` (nil), `TestPostToolUseWindowsPath`, `TestPreToolUseDispatch` (Skill и Bash доходят до T13a), `TestDisabled`.
- [ ] **Step 2:** Реализация; `verify` → PASS; `jq . hooks/hooks.json`.
- [ ] **Step 3:** Сообщение коммита: `feat(hooks): plan checks on ExitPlanMode and plan files; hooks.json`.

### Task 17: internal/plugintest

**Files:** Create `internal/plugintest/doc.go` (`package plugintest`, комментарий: тесты упаковки плагина), `frontmatter_test.go`, `shim_test.go`, `example_test.go`.

**Interfaces:** Consumes: `rules.Check/Options`, `plan.Extract`, `yaml.v3`. Корень репозитория — `../..` от пакета.

- [ ] **Step 1:** `TestSkillsFrontmatter`: каждый `skills/*/SKILL.md` и `agents/*.md` начинается с YAML-frontmatter, `name` == имени каталога/файла, `description` не пуст; у `create` и `check` `disable-model-invocation: true`; у агента `tools` без `Write`, `Edit`, `Bash`.
- [ ] **Step 2:** `shim_test.go` по образцу `/home/elia/Projects/auto-context/cmd/auto-context/shim_test.go`, для `bin/paraplane` (пропуск на windows): нет бинаря — `hook session-start` → JSON с `systemMessage`; с `.paraplane/config.yaml` `enabled: false` → тишина; другой хук → тишина и 0; CLI → 1. Поддельный бинарь, выходящий с 2 на хуке → шим 0; на `session-start` с кодом 1 → пустой вывод.
- [ ] **Step 3:** `TestRulesExamplePlan`: внутренний блок из `skills/rules/examples/plan.md` (содержимое 4-бэктикового ограждения) → `rules.Check` без ошибок; `TestSpecExamplePlan`: блок ```yaml п. 4 спецификации, обёрнутый в ```paraplane → без ошибок.
- [ ] **Step 4:** `verify` → PASS.
- [ ] **Step 5:** Сообщение коммита: `test: plugin packaging checks`.

### Task 14: cmd/paraplane

**Files:** Create `cmd/paraplane/main.go`, `check.go`, `audit.go`, `progress.go`, `main_test.go`.

**Interfaces:** Consumes: `hooks.SessionStart/PreToolUse/PostToolUse/Options/Input/Env`, `config.Load/FindRoot/Problems/FileName`, `rules.Check/Audit/Result`, `layout.Render`, `plan.Load`, `gitx.Changed/WaveCommits`, `progress.Render`. Produces: CLI п. 7 и 7.1 спецификации.

- [ ] **Step 1:** Тесты: `TestCheckCLI` (валидный файл → 0 и `plan ok`; невалидный → 1; без аргументов → 2; `-` читает stdin; `--suggest` при W3 печатает `Shorter layout:` и блок; проблемы конфига — `warning: config: …` в начале; симлинк и каталог вместо файла → 1 и `error:`); `TestAuditCLI` (git-репозиторий в `t.TempDir()`: изменения в `owns` → 0 и `audit ok: W2, N changed file(s)`; лишний файл → 1, `error A1` и `audit failed: 1 file(s) outside W2`; неизвестная волна → 1; не репозиторий → 1 и `error:`; запуск из подкаталога — путь плана приводится к корню); `TestProgressCLI` (git-репозиторий: коммит с трейлерами W1 → W1 `done`, W2 `running`; не репозиторий → 1 и `error:`; план с E → 1); `TestHookNeverFails` (битый JSON, payload > 4 МБ, неизвестное событие, паника обработчика → 0, ошибка в stderr); `TestHooksJSONContract` (события и подкоманды `../../hooks/hooks.json` ↔ `hookHandlers`); `TestVersion`.
- [ ] **Step 2:** `main.go` по образцу `/home/elia/Projects/auto-context/cmd/auto-context/main.go` (`start`/`run`/`runHook` с `recover`; `hookRoot`: `CLAUDE_PROJECT_DIR` → `in.Cwd` → cwd, затем `config.FindRoot`; `Env` с `config.Load`), без логов и бюджета git; stdin хука ≤ 4 МБ. `check.go`: файл через `os.Lstat` — обычный, не симлинк, внутри корня, ≤ 1 МБ; печать `warning: config: …`, находок, `Shorter layout:` + блок, `Summary()`. `audit.go`: `plan.Load` (ошибки E → `error: …`, 1), путь плана — к корню через `/`, `gitx.Changed`, `rules.Audit`, итог по п. 7. `progress.go`: `plan.Load` (ошибки E → `error: …`, 1), `gitx.WaveCommits`, `progress.Render` с именем файла плана.
- [ ] **Step 3:** `verify` → PASS.
- [ ] **Step 4:** Сообщение коммита: `feat(cmd): paraplane CLI`.

### Task 15: README, CLAUDE.md, hooks/README.md

**Files:** Create `README.md`, `hooks/README.md`; Modify `CLAUDE.md` (блок code-review-graph сохранить целиком, сверху добавить раздел о проекте).

- [ ] **Step 1:** `README.md` (русский, структура как у auto-context): что делает; пример блока ```paraplane; правила E/W/A; команды `/paraplane:create`, `/paraplane:check`, `paraplane check`, `paraplane audit`, `paraplane progress` (с примером вывода); хуки и что они блокируют; конфиг (таблица п. 9); требования (как у auto-context, включая Windows/Git Bash); установка `/plugin marketplace add Comprise/paraplane`, `/plugin install paraplane@paraplane`; ограничения (п. 13).
- [ ] **Step 2:** `hooks/README.md` — таблица событий п. 8 по `hooks/hooks.json`, образец — auto-context; `CLAUDE.md` — 5–6 строк о структуре над существующим блоком.
- [ ] **Step 3:** `verify` → PASS.
- [ ] **Step 4:** Сообщение коммита: `docs: README and hooks reference`.

---

## После W5

- Gate W5 уже прогоняет `paraplane check` на этом плане (проверка на себе) и строгую валидацию поставляемого дерева.
- `/auto-context:init` с `language: ru` — только после коммита W5 (решение пользователя: до конца реализации auto-context в проекте не включать, чтобы его хуки и запись в `CLAUDE.md` не мешали волнам).
- Ручная проверка п. 8.1 спецификации в Claude Code: `make dist`, скопировать поставляемое дерево (`.claude-plugin hooks skills agents bin libexec README.md LICENSE`) во временный каталог и `claude --plugin-dir <каталог>` (не из корня репозитория: корневой `.mcp.json` загрузился бы как MCP-сервер плагина).
