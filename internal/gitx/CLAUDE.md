# internal/gitx
Чтение изменённых файлов и трейлеров paraplane из git без запуска программ, заданных репозиторием, в пределах бюджета `context.Context`.

- `gitx.go` — захардённый запуск git (`runIO`), кеш версии, `requireGit`, `guardLazyFetch`, `Changed`, проверки конфига
- `commits.go` — `Scan`, `CommitsOf`, `PlanCommits`, `PlanCommitFiles`, разбор трейлеров
- `refs.go` — `Head`, `GitDir`, `IsAncestor`, `Unreachable`; `plans.go` — `PlansSince`
- `*_test.go` — поведение, защита от исполнения, lazy fetch на старом git, бюджет, старые версии git через подставной `git`

Details: context/internal/gitx.md
