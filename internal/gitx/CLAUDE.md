# internal/gitx
Чтение изменённых файлов рабочего дерева и закоммиченных волн из git без запуска программ, заданных репозиторием.

- `gitx.go` — `Changed`, `WaveCommits`, обёртка `run`/`runCode` с захардённым запуском git, проверки конфига и версии git
- `gitx_test.go`, `hardening_test.go`, `fallback_test.go` — поведение, защита от исполнения, старые версии git через подставной `git`

Details: context/internal/gitx.md
