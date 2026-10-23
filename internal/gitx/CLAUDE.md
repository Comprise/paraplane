# internal/gitx
Чтение изменённых файлов и трейлеров paraplane из git без запуска программ, заданных репозиторием, в пределах бюджета `context.Context`.

- `gitx.go` — захардённый запуск git (`runIO`), кеш версии, `requireGit`, `guardLazyFetch`, `ChangedContext`, проверки конфига
- `commits.go` — `Scan`, `CommitsOf`, `PlanCommitsContext`, `PlanCommitFilesContext`, разбор трейлеров
- `refs.go` — `HeadContext`, `GitDirContext`, `IsAncestorContext`, `Unreachable`; `plans.go` — `PlansSinceContext`
- `compat.go` — обёртки старых сигнатур без ctx (удалит R13)
- `*_test.go` — поведение, защита от исполнения, lazy fetch на старом git, бюджет, старые версии git через подставной `git`

Details: context/internal/gitx.md
