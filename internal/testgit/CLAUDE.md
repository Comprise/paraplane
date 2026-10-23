# internal/testgit
Хелперы git для тестов: изолированное окружение, временный репозиторий, поддельный старый git, переносимая программа-маркер. Импортируется только из `_test.go`.

- `testgit.go` — `Env`, `Isolate`, `Run`, `Init`, `Commit`, `FakeOldGit`, `MarkerProgram`
- `testgit_test.go` — изоляция от `GIT_DIR`, обёртка старого git, запуск маркера

Details: context/internal/testgit.md
