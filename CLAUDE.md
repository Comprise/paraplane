# paraplane
Плагин Claude Code и Codex CLI на Go: проверяет, что план реализации пригоден для параллельного исполнения субагентами (блок `paraplane`, владение файлами `owns`, волны, gate).

- `cmd/paraplane/` — бинарник (check, audit, progress, hook, version)
- `internal/` — ядро: `config`, `ids`, `plan`, `pathset`, `rules` (E0–E7, W1–W5, аудит A1), `layout`, `gitx`, `state`, `track`, `fsx`, `progress`, `hooks`, `shellx`, `textx`, `testgit`, `plugintest`
- `hooks/`, `skills/`, `agents/`, `bin/`, `.claude-plugin/`, `.codex-plugin/`, `.agents/` — упаковка плагина; `ship.txt` — что попадает в релиз
- `Makefile`, `.github/workflows/` — сборка, CI, релиз; `CHANGELOG.md` (+ `.ru.md`) — журнал изменений
- `docs/superpowers/` — спецификация и планы (источник правды по поведению)

Details: context/_root.md

## MCP Tools: code-review-graph

**This project has a knowledge graph. Start with the code-review-graph
MCP tools to narrow scope, then read the source.** The graph is cheaper than scanning files and
gives you structural context (callers, dependents, test coverage) that file search cannot.

### When to use graph tools FIRST

- **Exploring code**: `semantic_search_nodes_tool` or `query_graph_tool` instead of Grep
- **Understanding impact**: `get_impact_radius_tool` instead of manually tracing imports
- **Code review**: `detect_changes_tool` + `get_review_context_tool` instead of reading entire files
- **Finding relationships**: `query_graph_tool` with callers_of/callees_of/imports_of/tests_for
- **Architecture questions**: `get_architecture_overview_tool` + `list_communities_tool`

### Verify in the source

- Narrow scope with the graph, then read the source. Do not change code from graph output alone.
- For any non-trivial change, read the implementation and the relevant tests before concluding.
- Verify the exact source when touching behavior, database logic, migrations, retries, fallbacks,
  recovery, or compatibility code.
- When the graph and the source disagree, the source wins. The graph may be stale or may not
  model that relationship.
- An empty graph result can mean "not indexed" or "not statically visible", not "does not exist".

### Key Tools

| Tool | Use when |
| ------ | ---------- |
| `detect_changes_tool` | Reviewing code changes — gives risk-scored analysis |
| `get_review_context_tool` | Need source snippets for review — token-efficient |
| `get_impact_radius_tool` | Understanding blast radius of a change |
| `get_affected_flows_tool` | Finding which execution paths are impacted |
| `query_graph_tool` | Tracing callers, callees, imports, tests, dependencies |
| `semantic_search_nodes_tool` | Finding functions/classes by name or keyword |
| `get_architecture_overview_tool` | Understanding high-level codebase structure |
| `refactor_tool` | Planning renames, finding dead code |

### Workflow

1. The graph auto-updates on file changes (via hooks).
2. Use `detect_changes_tool` for code review.
3. Use `get_affected_flows_tool` to understand impact.
4. Use `query_graph_tool` pattern="tests_for" to check coverage.
<!-- /code-review-graph MCP tools -->

<!-- auto-context:units -->
## Units (details: context/<unit>.md)
- `cmd/paraplane` — Бинарник paraplane: CLI check, audit, progress, version и диспетчер хуков `hook <event> [--agent codex]`.
- `internal/config` — Загрузка .paraplane/config.yaml с поключевой валидацией (неверное значение → дефолт + Problems) и FindRoot.
- `internal/fsx` — Чтение только обычных файлов: симлинк, FIFO, устройство, каталог или подмена файла дают ErrNotRegular.
- `internal/gitx` — Чтение изменённых файлов и трейлеров paraplane из git, запускаемого так, чтобы конфиг чужого репозитория не исполнил программ; общий бюджет времени через `context.Context`.
- `internal/hooks` — Обработчики хуков Claude Code и Codex: проверка плана, дайджесты правил, автоодобрение CLI paraplane.
- `internal/ids` — Единая грамматика идентификаторов задач и волн: одна константа вместо копий регулярки в разных пакетах.
- `internal/layout` — Находит конфликты пар задач под дедлайном, строит кратчайшую раскладку (ASAP) и рендерит её блоком paraplane.
- `internal/pathset` — Валидирует пути плана и консервативно решает, могут ли два глоба описать один файл (без учёта регистра).
- `internal/plan` — Извлекает блок ```paraplane из Markdown и строго разбирает YAML в Plan с лимитами и типизированными ошибками.
- `internal/plugintest` — Тесты упаковки плагина: frontmatter скиллов и агентов, POSIX-шим, манифесты Claude/Codex, ship.txt, примеры планов.
- `internal/progress` — Рендер прогресса плана: шапка 72 колонки, полоса задач, строка на волну с эмодзи-квадратами, предупреждения.
- `internal/rules` — Валидирует блок paraplane по правилам E0–E7, W1–W5 и проверяет изменения волны против owns (A1).
- `internal/shellx` — Лексер shell-команд для хуков Codex: какие литеральные пути команда читает, правит (`sed -i`) или пишет, и какие команды в ней запускаются.
- `internal/state` — Состояние вне git: задачи, чьи субагенты вернулись, в `<git-dir>/paraplane/closed.json` под блокировкой.
- `internal/testgit` — Хелперы git для тестов других пакетов: изоляция окружения, временный репозиторий, поддельный старый git, программа-маркер.
- `internal/textx` — Очистка недоверенного текста перед выводом: `Line` — одна строка по рунам, `Block` — многострочный текст по байтам.
- `internal/track` — Сводит коммиты, состояние и `--done` в готовый прогресс плана и счётчики сделанного.
<!-- /auto-context:units -->
