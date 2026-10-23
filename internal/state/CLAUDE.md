# internal/state
Состояние вне git: задачи, чьи субагенты вернулись, в `<git-dir>/paraplane/closed.json` под блокировкой.

- `state.go` — `State`, `Entry`, `Shown`, `ScanCache`, `Load`, `Update`, методы `Add`/`Prune`/`Forget`/`TakePending`/`Closed`, `ErrBusy`, `ErrVersion`
- `nonblock_unix.go`, `nonblock_other.go` — флаг неблокирующего открытия (FIFO)
- `state_test.go`, `fifo_unix_test.go` — валидация, дедуп, версия, симлинки, `os.Root`, блокировка (каталог, брошенная, гонка перехвата, `synctest`), `FuzzLoad`
- `proc_test.go` — `TestMain` с дочерним режимом: два процесса перехватывают одну брошенную блокировку

Details: context/internal/state.md
