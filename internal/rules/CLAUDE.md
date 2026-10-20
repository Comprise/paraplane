# internal/rules
Проверка плана по правилам E0–E7, W1–W5 и аудит изменённых файлов волны (A1).

- `rules.go` — `Check`, `Options`, `Result`, `Finding`, порядок и сортировка находок, `Summary`
- `audit.go` — `Audit`: изменённые файлы против `owns` задач волны
- `race_test.go`, `norace_test.go` — флаг `raceEnabled` для тестов со временем

Details: context/internal/rules.md
