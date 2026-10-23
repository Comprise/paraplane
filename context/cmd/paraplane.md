---
unit: cmd/paraplane
source_hash: sha256:55d04d26d668644d6f5f22436c6fcc06a64c6caf8c0dc5c54c0ab0eb889d4c1e
commit: 930e887a622e41e57d7c9f836e7bf379ad8ab08f
updated: "2026-10-23"
---
# cmd/paraplane
Бинарник paraplane: CLI check, audit, progress, version и диспетчер хуков `hook <event> [--agent codex]`.

## Назначение
- Единственная точка входа плагина; вызывается шимом `bin/paraplane`, который выбирает собранный бинарник из `libexec/` по ОС и архитектуре.

## Публичный API / точки входа
- `check [--suggest] <file>` — находки, при `--suggest` — более короткая раскладка, затем сводка; `-` читает stdin; код 1 при ошибках.
- `audit <file> <wave>[,<wave>…]` — файлы рабочего дерева (`gitx.Changed`) и коммитов волн (`gitx.PlanCommitFiles`) против `owns` (`rules.AuditCommits`, A1); итог `audit ok: W, n changed file(s)[, m commit(s)]`; код 1 при находках; список волн с запятой проверяется `waveList`, иначе usage и код 2.
- `progress [--done <ids>] <file>` — прогресс через `track.PlanStatus`: коммиты волн, этапов и задач, задачи вернувшихся субагентов из `<git-dir>/paraplane/closed.json` и `--done`; вывод (с предупреждениями о потерянных трейлерах) показывается пользователю как есть. `--done` — только перед файлом, id по правилу плана через `,` без пробелов (`doneIDs`); иначе usage и код 2. После вывода — `warning: wave <id> commit <hash>: trailers not in the last paragraph` для каждой волны из `lost` `gitx.WaveCommits`.
- `hook <event> [--agent claude|codex]` — события из `hookHandlers`: `session-start`, `pre-tool-use`, `post-tool-use`, `subagent-stop` (только Claude), `stop` (Codex — проверка плана, Claude — показ ожидающего прогресса), `prompt-submit`, `permission-request`.
- `version` — значение `main.version`, задаётся через `-ldflags -X` в `Makefile`.
- Коды выхода: 0 — успех, 1 — ошибка или находки, 2 — неверные аргументы (usage в stderr).

## Структура и поток данных
- `start` → `run` → `runCheck` / `runAudit` / `runProgress` / `runHook`.
- Корень проекта для CLI: `config.FindRoot(cwd)`.
- Корень для хука (`hookRoot`): `CLAUDE_PROJECT_DIR`, иначе `cwd` из payload, иначе cwd процесса, затем `config.FindRoot`.
- Проверка плана в CLI — `hooks.Options` без дедлайна (`cliOptions`).
- `audit` и `progress` требуют план без ошибок (`loadValidPlan`), иначе ошибка с первой находкой и подсказкой запустить `check`.

## Инварианты и подводные камни
- Хук никогда не падает: неизвестное событие, плохие аргументы, неверный JSON, payload больше 4 МиБ, ошибка обработчика и паника → сообщение в stderr, пустой stdout, код 0.
- Флаг `--agent` идёт после события: шим читает событие из `$2`.
- Без cwd (`getwd` упал) хук всё равно работает от `CLAUDE_PROJECT_DIR`; остальные команды завершаются с ошибкой.
- Ошибки CLI печатаются в stdout одной строкой `error: …`; проблемы конфига — `warning: config: …` в stdout (`loadEnv`). `progress` проблемы конфига не печатает.
- `readPlanFile`:
  - только обычный файл внутри корня, не больше 1 МиБ;
  - сам файл не может быть симлинком, каталоги на пути могут (сравниваются реальные пути — macOS `/var`);
  - каталог даёт отдельное сообщение.
- Всё, что приходит из аргументов, плана и ФС, перед выводом режется `textx.Line` (`maxLine`, `maxName`, `maxWave`).
- Тесты фиксируют контракт манифестов: `hooks/hooks.json` должен совпадать с эталоном байт в байт, обработчики `prompt-submit`, `permission-request` — только в `hooks/codex.json`; `stop` — в обоих манифестах, `subagent-stop` — только в `hooks/hooks.json`. Новый обработчик требует правки `hookHandlers`, манифеста и `codexOnly` в тестах.

## Связи (зависит от / используется в)
- Зависит от: `internal/hooks`, `internal/config`, `internal/rules`, `internal/layout`, `internal/plan`, `internal/gitx`, `internal/progress`, `internal/fsx`, `internal/textx`.
- Используется в: `bin/paraplane` (шим), `hooks/hooks.json`, `hooks/codex.json`, скиллы плагина (`paraplane check|audit|progress`), `Makefile` (сборка в `libexec/`).
