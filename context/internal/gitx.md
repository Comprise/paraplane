---
unit: internal/gitx
source_hash: sha256:d1cc8ddcf0d1fa87c0aea71e7b04813bd7f849ce37571e6a8657b031a8ddbaf0
updated: "2026-10-20"
---
# internal/gitx
Changed и WaveCommits поверх git, запускаемого так, чтобы конфиг чужого репозитория не исполнил программ.

## Назначение
- `paraplane audit` (правило A1) получает список изменённых файлов, `paraplane progress` — список закоммиченных волн плана.
- Репозиторий может быть недоверенным: его конфиг способен заставить git запустить произвольную программу (filter-драйверы, хуки, fsmonitor, pager, gpg, транспорты partial clone). Пакет либо отключает такой механизм, либо отказывается запускать опасную команду.

## Публичный API / точки входа
- `Changed(root)` — изменённые файлы относительно `root`: слэши, отсортированы, без дублей; пути вне `root` отбрасываются. Переименование даёт оба пути (`--no-renames`). Сабмодули игнорируются.
- `WaveCommits(root, planPath)` — id волн из коммитов с трейлером `Paraplane-Plan` == `planPath`, в порядке первого появления, без дублей. Требует git ≥ 2.22. Репозиторий без коммитов → пустой список.
- `ErrFilterDrivers`, `ErrPartialClone` — отказы `Changed`.

## Структура и поток данных
- Все вызовы идут через `runCode`:
  - `-c` переопределяет `core.fsmonitor`, `core.pager`, `log.showSignature`, все `gpg.*program`, `core.hooksPath=/dev/null`; плюс `--no-pager`;
  - из окружения удаляются переменные, перенаправляющие git на другой репозиторий (`GIT_DIR`, `GIT_WORK_TREE`, `GIT_CONFIG` и т. п.; `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_COUNT` намеренно сохраняются);
  - добавляются `GIT_NO_LAZY_FETCH=1`, `GIT_OPTIONAL_LOCKS=0` (status не пишет индекс и не дёргает index-хуки), `GIT_CONFIG_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`, `LC_ALL=C`;
  - таймаут 10 с; stdout ограничен 16 MiB (`maxOutput`), при переполнении git останавливается и возвращается ошибка.
- `Changed`: `rev-parse --show-prefix` → версия git → `repoConfigSets(filterKeys)` → при git без `GIT_NO_LAZY_FETCH` проверки partial clone и `uploadpack` → `git status --porcelain=v1 -z`.
- `WaveCommits`: `git log --reverse` с `%(trailers:key=…,unfold)` и разделителем записей `\x1e`; при повторе трейлера в одном коммите побеждает последний; id волны должен совпасть с `waveID`.

## Инварианты и подводные камни
- `run` сам не нейтрализует filter-драйверы и lazy fetch: любая новая команда, способная их запустить, обязана повторить проверки `Changed`.
- Доверенные области конфига — только `global` и `command`; любая другая область (local, worktree, включённые через `include`/`includeIf` файлы, неизвестные) с filter-драйвером → `ErrFilterDrivers`. Системный конфиг не читается вовсе.
- Маркеры partial clone (`extensions.partialClone`, `remote.*.promisor`) учитываются из любой области, включая global: lazy fetch может запустить транспорт, заданный репозиторием, независимо от того, кто сделал клон partial.
- `GIT_NO_LAZY_FETCH` считается поддержанным с 2.46.0 и в патч-релизах 2.39.4, 2.40.2, 2.41.1, 2.42.2, 2.43.4, 2.44.1, 2.45.1; 2.45.0 намеренно не доверяется. Нераспознанная версия считается небезопасной.
- git < 2.26 не знает `--show-scope` (код 129): тогда отдельно опрашиваются `--local` и `--worktree`. `--worktree` запускается, только если `extensions.worktreeConfig` может быть включён (`worktreeConfigMaybeOn` читает все значения с include и без, любое не-false считается включением): иначе он перечитывает local или падает с кодом 128 в репозитории со связанными worktree.
- Любой неожиданный сбой проверки конфига — ошибка, то есть отказ; «нет совпадений» распознаётся строго как код 1 без вывода (`noMatches`).
- Огромное неотслеживаемое дерево упирается в лимит 16 MiB, и `Changed` падает с `git output too large`.
- Тесты `fallback_test.go` подставляют на `PATH` shell-обёртку `git`, имитирующую старые версии; на Windows они пропускаются.

## Связи (зависит от / используется в)
- Зависит только от stdlib и внешнего бинарника `git`.
- Используется в: `cmd/paraplane/audit.go` (`Changed`), `cmd/paraplane/progress.go` (`WaveCommits`).
