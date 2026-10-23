---
unit: internal/gitx
source_hash: sha256:2dc09730bb44209c320c42cc04f3dc9f415df0c022e203f1d5997367d67cc8f1
commit: 1ad392a8cc668fcc44ffd6e2d14e9965d92cfa05
updated: "2026-10-23"
---
# internal/gitx
Чтение изменённых файлов и трейлеров paraplane из git, запускаемого так, чтобы конфиг чужого репозитория не исполнил программ; общий бюджет времени через `context.Context`.

## Назначение
- `paraplane audit` (правило A1) получает изменённые файлы и коммиты плана с файлами, `paraplane progress` и хуки — закоммиченные волны и задачи всех планов одним проходом истории.
- Репозиторий может быть недоверенным: его конфиг способен заставить git запустить произвольную программу (filter-драйверы, хуки, fsmonitor, pager, gpg, транспорты partial clone). Пакет либо отключает такой механизм, либо отказывается запускать опасную команду.

## Публичный API / точки входа
Все функции принимают `ctx` первым аргументом; каждый вызов git длится не дольше `min(10 с, остаток ctx)`.
- `ChangedContext(ctx, root)` — изменённые файлы относительно `root`: слэши, отсортированы, без дублей; пути вне `root` отбрасываются. Переименование даёт оба пути (`--no-renames`). Сабмодули игнорируются.
- `Scan(ctx, root, from) (Scanned, error)` — один `git log` по `from..HEAD` для всех планов; при пустом `from`, не полном хеше или не предке HEAD (переписанная история) — вся история. `Scanned{Head, Commits []TrailerCommit}`; `TrailerCommit{Hash (полный), Plan, Waves, Tasks, LostWaves, LostTasks}`. `Plan` — последний `Paraplane-Plan` трейлеров, а без них — сообщения. Без коммитов → пустой `Scanned`.
- `CommitsOf(commits, plan) Commits` — свёртка скана для одного плана: `Commits{Waves, Tasks, Lost}` в порядке первого появления, без дублей; `Lost` — `LostTrailer{Commit (12 знаков), Kind, ID, Wave}` по одному на id и только для незасчитанных.
- `PlanCommitsContext(ctx, root, plan)` — `CommitsOf` по полному `Scan`.
- `PlanCommitFilesContext(ctx, root, plan) ([]CommitFiles, error)` — коммиты плана с волнами/задачами и изменёнными файлами (`--name-only --no-renames -z`; файлы вне `root` отбрасываются, остальные — относительно `root`; слияния без файлов), старые первыми.
- `PlansSinceContext(ctx, root, from, to)` — планы, названные коммитами `from..to` (без `from` — только `to`); оба — полные хеши.
- `Unreachable(ctx, root, hashes, head) (map[string]bool, error)` — для каждого хеша: недостижим ли он из `head`. Не полный хеш, несуществующий объект или не коммит → недостижим, без ошибки и без git. Два процесса на любое число хешей: `cat-file --batch-check` (существующие коммиты) и `rev-list --stdin` с `^head`; вывод не копится, а сверяется построчно.
- `HeadContext` (полный хеш, `""` без коммитов), `IsAncestorContext(ctx, root, a, b)` (только полные хеши, разовые проверки), `GitDirContext` (`rev-parse --absolute-git-dir`).
- `ErrOldGit` (`git 2.24 or later is required`: все функции, читающие коммиты, — `Scan`, `PlanCommitsContext`, `PlanCommitFilesContext`, `PlansSinceContext`, `IsAncestorContext`, `Unreachable` — требуют git ≥ 2.24 ради `--end-of-options`; `ChangedContext` такого порога не имеет; вызывающие оборачивают: `… for progress`, `… for audit of commits`), `ErrFilterDrivers`, `ErrPartialClone`.
- Обёртки совместимости `Changed`, `PlanCommits`, `PlanCommitFiles`, `PlansSince`, `Head`, `GitDir`, `IsAncestor` (`compat.go`) передают `context.Background()`; их удалит задача R13 вместе с переводом вызывающих. `WaveCommits` удалён.

## Структура и поток данных
- `gitx.go` — запуск git, версия, защиты, `ChangedContext`; `commits.go` — `Scan`, `CommitsOf`, `PlanCommitFilesContext`, разбор трейлеров; `refs.go` — `HeadContext`, `GitDirContext`, `IsAncestorContext`, `Unreachable`; `plans.go` — `PlansSinceContext`; `compat.go` — обёртки.
- Все вызовы идут через `runIO` (`run`/`runCode` — его частные случаи):
  - `-c` переопределяет `core.fsmonitor`, `core.pager`, `log.showSignature`, все `gpg.*program`, `core.hooksPath=/dev/null`; плюс `--no-pager`;
  - из окружения удаляются переменные, перенаправляющие git на другой репозиторий (`GIT_DIR`, `GIT_WORK_TREE`, `GIT_CONFIG` и т. п.; `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_COUNT` намеренно сохраняются);
  - добавляются `GIT_NO_LAZY_FETCH=1`, `GIT_ALLOW_PROTOCOL=none` (никакой транспорт не стартует даже на git, игнорирующем `GIT_NO_LAZY_FETCH`), `GIT_OPTIONAL_LOCKS=0`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`, `LC_ALL=C`;
  - `exec.CommandContext` с `WaitDelay = 1 с`: потомок git, держащий stdout, не задерживает возврат; истёкший бюджет → `git <cmd>: timed out: context deadline exceeded` (`errors.Is` работает);
  - stdout ограничен 16 MiB (`maxOutput`), при переполнении git останавливается и возвращается ошибка; у потокового приёмника (`Unreachable`) лимита нет, его ограничивает бюджет.
- Версия git кешируется на процесс по пути, в который `exec.LookPath("git")` разрешает `git`; `requireGit(ctx, root, major, minor, patch)` — единая проверка версии, вызывается с `minGit` (2.24).
- `guardLazyFetch`: на git без `GIT_NO_LAZY_FETCH` проверки partial clone и `uploadpack` → `ErrPartialClone`. Вызывается в `ChangedContext`, `Scan` (и `PlanCommitsContext`), `PlanCommitFilesContext`, `PlansSinceContext`, `IsAncestorContext`, `Unreachable`.
- `ChangedContext`: `rev-parse --show-prefix` → `repoConfigSets(filterKeys)` → `guardLazyFetch` → `git status --porcelain=v1 -z`.
- `Scan`: версия → `HeadContext` → `guardLazyFetch` → при валидном `from` `merge-base --is-ancestor` → `git log --reverse -i --grep=^paraplane-wave: --grep=^paraplane-task: --format=%H\x1f<трейлеры>\x1f%B\x1e --end-of-options <диапазон>`. Значение трейлера может быть списком через запятую; значение с хоть одной частью не по `ids.Valid` отбрасывается целиком; строки `Paraplane-*` вне трейлеров (этап, разбитый пустой строкой) попадают в `LostWaves`/`LostTasks`.
- Разбор вывода — через `bytes` (`SplitSeq`, `Cut`) без копирования всего вывода в строку; в строки копируются только нужные значения.

## Инварианты и подводные камни
- Хеши и ссылки из файлов (файл состояния) проверяются по `^[0-9a-f]{40}([0-9a-f]{24})?$` и передаются после `--end-of-options` или через stdin; `IsAncestorContext` и `PlansSinceContext` с невалидным хешем возвращают ошибку, не запуская git. `--end-of-options` требует git ≥ 2.24, поэтому порог `minGit` = 2.24 (раньше 2.22 — ради `%(trailers:key=…)`).
- `run` сам не нейтрализует filter-драйверы и не проверяет partial clone: любая новая команда, способная читать объекты, обязана вызвать `guardLazyFetch` (и, если запускает фильтры, повторить проверку `ChangedContext`).
- Доверенные области конфига — только `global` и `command`; любая другая область (local, worktree, включённые через `include`/`includeIf` файлы, неизвестные) с filter-драйвером → `ErrFilterDrivers`. Системный конфиг не читается вовсе.
- Маркеры partial clone (`extensions.partialClone`, `remote.*.promisor`) учитываются из любой области, включая global.
- Список исправленных версий git один (`lazyFetchFirst`, `lazyFetchFixed`): `GIT_NO_LAZY_FETCH` поддержан с 2.46.0 и в 2.39.4, 2.40.2, 2.41.1, 2.42.2, 2.43.4, 2.44.1, 2.45.1; 2.45.0 намеренно не доверяется; текст `ErrPartialClone` собирается из того же списка. Нераспознанная версия считается небезопасной.
- git < 2.26 не знает `--show-scope` (код 129): тогда отдельно опрашиваются `--local` и `--worktree` (последний — только если `extensions.worktreeConfig` может быть включён, см. `worktreeConfigMaybeOn`).
- Любой неожиданный сбой проверки конфига — ошибка, то есть отказ; «нет совпадений» распознаётся строго как код 1 без вывода (`noMatches`).
- FIFO в `include.path` вешает любой git на открытии файла: спасает только бюджет вызывающего (`TestBudgetKillsSlowGit`).
- Тесты `fallback_test.go` и `lazy_test.go` подставляют на `PATH` shell-обёртки `git`; на Windows они пропускаются. `lazy_test.go` моделирует старый git через `testgit.FakeOldGit` (2.34.1, без `GIT_NO_LAZY_FETCH`).

## Связи (зависит от / используется в)
- Зависит от stdlib, `internal/ids` и внешнего бинарника `git`; тесты — от `internal/testgit`.
- Используется в: `cmd/paraplane/audit.go` (`Changed`, `PlanCommitFiles`), `internal/track` (`PlanCommits`, `GitDir`, `Head`, `IsAncestor`), `internal/hooks/progress_hook.go` (`GitDir`, `Head`, `IsAncestor`, `PlansSince`) — пока через обёртки.
