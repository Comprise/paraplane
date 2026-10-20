# cmd/paraplane
Бинарник `paraplane`: CLI (check, audit, progress, version) и точка входа хуков (`hook <event>`).

- `main.go` — разбор аргументов, `hookHandlers`, `runHook`, `hookRoot`
- `check.go` — `check [--suggest]`, `readPlanFile`, `loadEnv`, `cliOptions`
- `audit.go` — `audit <file> <wave>`, `loadValidPlan`
- `progress.go` — `progress <file>`
- `main_test.go` — тесты CLI и контракт `hooks/hooks.json`/`hooks/codex.json`

Details: context/cmd/paraplane.md
