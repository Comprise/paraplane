---
unit: internal/fsx
source_hash: sha256:c6feb50fb684648fab5fabb19403d0e6933692809b75f6cbb735d2bd86592a27
commit: ee26fb1c0e795a3aaa095fbd0556059d4c9450de
updated: "2026-10-23"
---
# internal/fsx
Чтение только обычных файлов: симлинк, FIFO, устройство, каталог или подмена файла дают ErrNotRegular.

## Назначение
- Файлы плана и конфига приходят из чужого клонированного репозитория: чтение не должно идти по симлинку наружу и не должно зависать на FIFO/устройстве.

## Публичный API / точки входа
- `OpenRegular(name)` — открытый файл и его `FileInfo`; вызывающий закрывает файл.
- `ReadRegular(name, max)` — весь файл; больше `max` байт — `ErrTooLarge`. `math.MaxInt64` снимает лимит.
- `MaxFile` (1 MiB) — лимит по умолчанию для хуков и команд. Те же 1 MiB берут `config.MaxSize` и `maxPlanFile` в `cmd/paraplane` (константы от `fsx.MaxFile`, не копии).
- Отсутствующий файл возвращает исходную ошибку `os` (проверять через `fs.ErrNotExist`), а не `ErrNotRegular`.

## Структура и поток данных
- `Lstat` → проверка «обычный файл» → `OpenFile` с `nonblockFlag` → `f.Stat()` → `os.SameFile(lstat, stat)`.
- На Unix `nonblockFlag` = `O_NONBLOCK|O_NOFOLLOW|O_NOCTTY`: подменённый между `Lstat` и `open` FIFO не блокирует, симлинк не открывается. На прочих ОС флаг 0, защита держится на сверке `SameFile`.

## Инварианты и подводные камни
- Проверяется только последний компонент пути: симлинки в каталогах по пути допустимы (macOS `/var` → `/private/var`). Выход за корень проекта проверяет вызывающий (`cmd/paraplane/check.go`).
- Windows: `FileInfo` из `Lstat` не содержит ID файла, `os.SameFile` загружает его лениво по пути. Поэтому `SameFile(lfi, lfi)` вызывается до `open` — иначе ID прочитался бы уже у подменённого файла. Не удалять этот вызов.
- `afterLstat` — тестовый хук между `Lstat` и `open` (тесты подмены на FIFO и на другой файл); в продакшене nil.
- `ReadRegular` читает `max+1` байт через `LimitReader`, чтобы отличить «ровно max» от «больше».

## Связи (зависит от / используется в)
- Зависит только от stdlib.
- Используется в: `internal/config` (чтение `config.yaml`), `internal/hooks` (`plan_text.go`, `codex_stop.go`), `cmd/paraplane/check.go`.
