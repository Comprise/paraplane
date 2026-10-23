# internal/ids
Единая грамматика идентификаторов задач и волн: `[A-Za-z][A-Za-z0-9_.-]{0,31}`.

- `ids.go` — `Pattern`, `ListPattern` (для сборки регулярок), `Valid`, `ParseList`
- `ids_test.go` — границы длины, не-ASCII, пустые части списка, fuzz round-trip

Details: context/internal/ids.md
