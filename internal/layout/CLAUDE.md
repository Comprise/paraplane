# internal/layout
Поиск конфликтов между задачами плана, кратчайшая раскладка по волнам и её вывод готовым блоком ```paraplane.

- `layout.go` — `ConflictsWithin` (с дедлайном), `SuggestWith`, `Render`, тип `Conflict`
- `race_test.go`, `norace_test.go` — флаг `raceEnabled` для отключения проверок времени под `-race`

Details: context/internal/layout.md
