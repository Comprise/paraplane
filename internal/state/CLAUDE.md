# internal/state
Состояние вне git: задачи, чьи субагенты вернулись, в `<git-dir>/paraplane/closed.json` под блокировкой.

- `state.go` — `State`, `Entry`, `Shown`, `Load`, `Update`, `State.Closed`, `ErrBusy`
- `state_test.go`, `fifo_unix_test.go` — битый и нерегулярный файл, параллельные записи, брошенная блокировка, лимит записей

Details: context/internal/state.md
