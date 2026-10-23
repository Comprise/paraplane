---
unit: internal/config
source_hash: sha256:8bf8c9daef1b088e06a0026d18c6c56fca26c54101feafdb08897add309e6f99
commit: fe5c0c54ae46fe556c0ffe0680403e4edf62785d
updated: "2026-10-23"
---
# internal/config
Загрузка .paraplane/config.yaml с поключевой валидацией (неверное значение → дефолт + Problems) и FindRoot.

## Назначение
- Эффективная конфигурация paraplane (`enabled`, `mode`, `plan_paths`, `hot_files`, `default_gate`, `max_parallel`, `plans_dir`, `audit_ignore`, `progress`); дефолты — раздел 9 спецификации.
- Определение корня проекта для команд и хуков.

## Публичный API / точки входа
- `Default()` — дефолтная конфигурация.
- `Load(root)` — всегда возвращает пригодный `Config`:
  - файла нет → `Default()`, nil;
  - не читается, больше `MaxSize`, не обычный файл, не YAML → `Default()` и ошибка;
  - иначе применяются все валидные ключи, отброшенные перечислены в ошибке типа `Problems`.
- `Problems` — список проигнорированных настроек; вызывающие различают его и прочие ошибки (`hooks.Env.ConfigErr`).
- `FindRoot(dir)` — ближайший вверх каталог с `.git` или `.paraplane/config.yaml`; без маркера — сам `dir`.

## Структура и поток данных
- `fsx.ReadRegular` (лимит 1 MiB) → `yaml.Unmarshal` в `map[string]yaml.Node` → ключи в отсортированном порядке → `apply` по одному ключу.
- `key:` без значения (`!!null`) оставляет дефолт без сообщения.
- Списки и `plans_dir` нормализуются через `pathset.Clean`; один невалидный элемент отбрасывает весь список (остаётся дефолт).

## Инварианты и подводные камни
- Конфиг приходит из клонированного репозитория, а текст ошибок попадает в контекст Claude:
  - неизвестный ключ показывается, только если это короткое слово из `[a-z_]`, иначе `(name not shown)`;
  - значения элементов списков в ошибках не цитируются, только номер элемента.
- `default_gate`: одна строка до 256 байт; проверка `textx.HasControl`: запрещены управляющие символы (включая таб и NUL), U+2028/U+2029 и категория Cf (bidi-override, zero-width, BOM) — чтобы команда gate не скрывала и не переставляла видимый текст.
- `max_parallel` — целое 1..32; `mode` — только `block` или `warn`; `progress` — только `wave` (по умолчанию) или `task` (когда хук показывает прогресс); `plans_dir` — без glob-символов.
- `FindRoot` не поднимается в `$HOME` снизу: случайный `~/.git` не делает домашний каталог проектом. Если `dir` сам равен `$HOME`, маркеры в нём проверяются.
- `FindRoot` использует `os.Stat` (симлинки на маркеры допустимы); чтение конфига — нет (через `fsx`).

## Связи (зависит от / используется в)
- Зависит от: `internal/fsx`, `internal/pathset`, `internal/textx`, `go.yaml.in/yaml/v3`.
- Используется в: `cmd/paraplane` (`main.go`, `check.go`, `audit.go`, `progress.go`), `internal/hooks` (`Env.Config`, `Env.ConfigErr`).
