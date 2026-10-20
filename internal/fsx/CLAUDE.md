# internal/fsx
Безопасное чтение обычных файлов: без перехода по симлинкам и без зависания на FIFO и устройствах.

- `fsx.go` — `OpenRegular`, `ReadRegular`, лимит `MaxFile`, ошибки `ErrNotRegular`/`ErrTooLarge`
- `nonblock_unix.go`, `nonblock_other.go` — флаги открытия по платформам

Details: context/internal/fsx.md
