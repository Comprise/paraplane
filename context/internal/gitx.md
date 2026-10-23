---
unit: internal/gitx
source_hash: sha256:7fe88c437809bb4b0bb22ee3563471325884177b495134d76a56e6d10f1508b2
commit: cf351827e7241787d9a30c285c647699aa9f20f9
updated: "2026-10-23"
---
# internal/gitx
Changed и WaveCommits поверх git, запускаемого так, чтобы конфиг чужого репозитория не исполнил программ.

## Назначение
- `paraplane audit` (правило A1) получает список изменённых файлов, `paraplane progress` — список закоммиченных волн плана.
- Репозиторий может быть недоверенным: его конфиг способен заставить git запустить произвольную программу (filter-драйверы, хуки, fsmonitor, pager, gpg, транспорты partial clone). Пакет либо отключает такой механизм, либо отказывается запускать опасную команду.

## Публичный API / точки входа
- `Changed(root)` — изменённые файлы относительно `root`: слэши, отсортированы, без дублей; пути вне `root` отбрасываются. Переименование даёт оба пути (`--no-renames`). Сабмодули игнорируются.
- `PlanCommits(root, planPath) (Commits, error)` — `Commits{Waves, Tasks, Lost}`: волны и задачи из трейлеров `Paraplane-Wave`/`Paraplane-Task` коммитов с `Paraplane-Plan` == `planPath`, в порядке первого появления, без дублей; значение может быть списком через запятую. `Lost` — `LostTrailer{Commit, Kind, ID, Wave}`: строки `Paraplane-*` в теле, которых нет в трейлерах (не последний абзац), по одной на id и только для незасчитанных. Требует git ≥ 2.22. Репозиторий без коммитов → пусто.
- `WaveCommits(root, planPath) (waves, lost, err)` — обёртка над `PlanCommits` только с волнами (для старых вызовов).
- `PlanCommitFiles(root, planPath) ([]CommitFiles, error)` — коммиты плана с волнами/задачами и изменёнными файлами (`--name-only --no-renames`, слияния без файлов), старые первыми.
- `Head(root)` (полный хеш, `""` без коммитов), `IsAncestor(root, a, b)` (`merge-base --is-ancestor`), `GitDir(root)` (`rev-parse --absolute-git-dir`).
- `ErrFilterDrivers`, `ErrPartialClone` — отказы `Changed`.

## Структура и поток данных
- Все вызовы идут через `runCode`:
  - `-c` переопределяет `core.fsmonitor`, `core.pager`, `log.showSignature`, все `gpg.*program`, `core.hooksPath=/dev/null`; плюс `--no-pager`;
  - из окружения удаляются переменные, перенаправляющие git на другой репозиторий (`GIT_DIR`, `GIT_WORK_TREE`, `GIT_CONFIG` и т. п.; `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_COUNT` намеренно сохраняются);
  - добавляются `GIT_NO_LAZY_FETCH=1`, `GIT_OPTIONAL_LOCKS=0` (status не пишет индекс и не дёргает index-хуки), `GIT_CONFIG_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`, `LC_ALL=C`;
  - таймаут 10 с; stdout ограничен 16 MiB (`maxOutput`), при переполнении git останавливается и возвращается ошибка.
- `Changed`: `rev-parse --show-prefix` → версия git → `repoConfigSets(filterKeys)` → при git без `GIT_NO_LAZY_FETCH` проверки partial clone и `uploadpack` → `git status --porcelain=v1 -z`.
- `PlanCommits`/`PlanCommitFiles`: `git log --reverse -i --grep=^paraplane-wave: --grep=^paraplane-task:` (только коммиты с такими строками, чтобы вывод с `%B` не рос с историей), формат `%h`, `%(trailers:key=…,unfold)`, `%B` через `\x1f`, разделитель записей `\x1e`; коммит может закрывать несколько волн — засчитываются все `Paraplane-Wave`, значение может быть списком через запятую (`W1, W2`; значение с хоть одной частью не по `waveID` отбрасывается целиком); строки `Paraplane-*` в теле, которых нет в трейлерах (этап, разбитый пустой строкой), попадают в `lost`; при повторе `Paraplane-Plan` побеждает последний.

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
