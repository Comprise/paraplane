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
- `audit <file> <wave>[,<wave>…]` — файлы рабочего дерева (`gitx.ChangedContext`) и коммитов волн (`gitx.PlanCommitFilesContext`) против `owns` (`rules.AuditCommits`, A1); число коммитов волн — `rules.CommitsOfWaves`; итог `audit ok: W, n changed file(s)[, m commit(s)]`; код 1 при находках; каждый id волны (и в списке, и одиночный) проверяется `ids.ParseList`, иначе usage и код 2. Печатается не больше 100 строк A1, остальное — `… and <n> more`; ошибка «git output too large» (16 МиБ) дополняется подсказкой добавить файлы в `.gitignore` (`auditGitError`).
- `progress [--done <ids>] <file>` — прогресс через `track.PlanStatusContext` с хэшем блока плана (`track.PlanHash`): коммиты волн, этапов и задач, задачи вернувшихся субагентов из `<git-dir>/paraplane/closed.json` (только записи с тем же хэшем) и `--done`; вывод (с предупреждениями о потерянных трейлерах) показывается пользователю как есть. `--done` — только перед файлом, id через `,` без пробелов (`ids.ParseList`); иначе usage и код 2. После вывода — `warning: wave <id> commit <hash>: trailers not in the last paragraph` для каждой волны из `lost` `gitx.WaveCommits`.
- `progress --forget <ids> <file>` — убирает записи этих задач плана (`State.Forget`) и ключ плана в `State.Shown` одним `state.UpdateCtx` (иначе хук прогресса молчал бы: `Shown` только растёт). Нет записей — ничего не пишет и говорит `nothing recorded for …`; id не из плана — `error: task … is not in …`, код 2.
- `hook <event> [--agent claude|codex]` — события из `hookHandlers`: `session-start`, `pre-tool-use`, `post-tool-use`, `subagent-stop` (только Claude), `stop` (Codex — проверка плана, Claude — показ ожидающего прогресса), `prompt-submit`, `permission-request`.
- `version [-v]` — `main.version` из `-ldflags -X` (пусто — версия модуля из `debug.ReadBuildInfo`, иначе `dev`); `-v` печатает `paraplane <версия>`, `go <версия Go>`, `revision <vcs.revision>[ (modified)]` (`unknown` без VCS-штампа).
- Коды выхода: 0 — успех, 1 — ошибка или находки, 2 — неверные аргументы (usage в stderr).

## Структура и поток данных
- `start` → `run` → `runCheck` / `runAudit` / `runProgress` / `runHook`.
- Корень проекта для CLI: `config.FindRoot(cwd)`.
- Корень для хука (`hookRoot`): `CLAUDE_PROJECT_DIR`, иначе `cwd` из payload, иначе cwd процесса, затем `config.FindRoot`.
- Проверка плана в CLI — `rules.OptionsFor` без дедлайна (`cliOptions`). CLI вызывает git с `context.Background()`: лимит на вызов задаёт `gitx`.
- `runHook` создаёт один `context` на 7 с (`hookBudget`, хост убивает хук на 10 с) и кладёт его в `hooks.Env.Ctx`; ответ с `hookSpecificOutput.hookEventName`, не равным событию (`hookEventNames`), отбрасывается с сообщением в stderr.
- `PARAPLANE_DEBUG=1` — одна строка `paraplane: debug: hook <event> <n>ms[ error: …]` в stderr на хук; stdout не меняется.
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
- Зависит от: `internal/hooks`, `internal/config`, `internal/rules`, `internal/layout`, `internal/plan`, `internal/gitx`, `internal/progress`, `internal/fsx`, `internal/textx`, `internal/ids`, `internal/state`, `internal/track`.
- Используется в: `bin/paraplane` (шим), `hooks/hooks.json`, `hooks/codex.json`, скиллы плагина (`paraplane check|audit|progress`), `Makefile` (сборка в `libexec/`).
