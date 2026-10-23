# internal/track
Сводит коммиты, состояние и `--done` в готовый прогресс плана и счётчики сделанного: один скан истории на все планы, одна проверка достижимости записей.

- `track.go` — `Status`, `PlanRef`, `Result`, `PlanHash`, `Statuses`, `PlanStatusContext`, обёртка `PlanStatus`, кеш скана
- `track_test.go` — старая обёртка: коммит и состояние, `git reset` назад, потерянный трейлер
- `statuses_test.go` — хеш блока, один Scan/Unreachable, инкрементальный и поддельный кеш, чистка записей и `Apply` на позднем состоянии, возврат на ветку, источники по отдельности и вместе, ошибки git, бюджет, бенчмарк

Details: context/internal/track.md
