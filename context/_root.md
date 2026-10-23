---
unit: _root
source_hash: sha256:6901a212a41064448a0e6dfc6cd039ed3e984000b2d44ec8df3e505f76423f94
commit: cddecf504da238f42651c5e1d929af8f6c500234
updated: "2026-10-23"
---
# _root
Плагин paraplane: правила и проверки плана для параллельного исполнения субагентами в Claude Code и Codex.

## Назначение
- Плагин только задаёт и проверяет правила плана; планы он не исполняет и субагентов не запускает.
- Источник правды по поведению — `docs/superpowers/specs/2026-10-20-paraplane-design.md`; README и skills пересказывают его.

## Структура и поток данных
- Один бинарник `paraplane` (`cmd/paraplane`) обслуживает CLI (`check`, `audit`, `progress`, `version`) и хуки (`hook <event> [--agent codex]`).
- `bin/paraplane` — POSIX-shim: выбирает `libexec/paraplane-<os>-<arch>[.exe]`; бинарники собираются только в релизе (`make dist`), в `main` их нет.
- `hooks/hooks.json` (Claude) и `hooks/codex.json` (Codex) вызывают shim; `PostToolUse` Claude ловит и `Bash`/`Agent`/`Task` для хука прогресса. `skills/` (`rules`, `create`, `check`, `progress`) и `agents/conflict-auditor.md` — текстовая часть плагина.
- Что сделано в плане, знают коммиты (трейлеры `Paraplane-Wave`/`Paraplane-Task`) и состояние вне рабочего дерева `<git-dir>/paraplane/closed.json` (`internal/state`); сводит их `internal/track`.
- Манифесты: `.claude-plugin/` (плагин и маркетплейс Claude), `.codex-plugin/plugin.json` и `.agents/plugins/marketplace.json` (Codex).
- `ship.txt` — единственный список того, что поставляется; его читают `make tree`, CI и релиз.

## Инварианты и подводные камни
- Зависимости только stdlib, `gopkg.in/yaml.v3`, `github.com/bmatcuk/doublestar/v4`; `go.mod`/`go.sum` не меняются без причины.
- Хуки всегда выходят с кодом 0 и ничего не пишут на диск (fail-open); ошибки — в stderr.
- Публичное имя блока — только ` ```paraplane `; новых публичных имён не вводить.
- В `main` в `plugin.json` нет поля `version`: релиз проставляет его из тега (`vX.Y.Z` → `X.Y.Z`) в ветке `release`.
- `.gitattributes` фиксирует LF: shim и golden-файлы ломаются от CRLF на Windows.
- Каждый `README.md` имеет двойника `README.ru.md` (так же `docs/reference.md` и `docs/reference.ru.md`); английский основной, обе версии синхронны.
- `README` — что это, установка, как пользоваться; всё подробное — в `docs/reference*.md`. `docs/` не поставляется, поэтому README ссылается на справочник абсолютным URL на `main`.
- Тексты для Claude и пользователя — английские; текст из плана, конфига и ошибок YAML выводится через `textx`.

## Сборка, CI, релиз
- `make test` — `go vet` под windows и darwin плюс `go test ./...`; `make dist VERSION=…`; `make tree DIR=…`.
- CI: тесты на linux, macOS, windows, отдельные job с `-race` и кросс-`vet`.
- Релиз по тегу `v*`: job `validate` без секретов собирает 6 платформ, загружает архив и checksums до запуска стороннего кода; `publish` проверяет checksums и публикует релиз и ветку `release`.
