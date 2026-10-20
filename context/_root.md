---
unit: _root
source_hash: sha256:97fa2f983a0d177ad082646d5f87e486a01314ff87b46f51a9ac2e7ee9de1ce0
updated: "2026-10-20"
---
# _root
Плагин paraplane: правила и проверки плана для параллельного исполнения субагентами в Claude Code и Codex.

## Назначение
- Плагин только задаёт и проверяет правила плана; планы он не исполняет и субагентов не запускает.
- Источник правды по поведению — `docs/superpowers/specs/2026-10-20-paraplane-design.md`; README и skills пересказывают его.

## Структура и поток данных
- Один бинарник `paraplane` (`cmd/paraplane`) обслуживает CLI (`check`, `audit`, `progress`, `version`) и хуки (`hook <event> [--agent codex]`).
- `bin/paraplane` — POSIX-shim: выбирает `libexec/paraplane-<os>-<arch>[.exe]`; бинарники собираются только в релизе (`make dist`), в `main` их нет.
- `hooks/hooks.json` (Claude) и `hooks/codex.json` (Codex) вызывают shim; `skills/` (`rules`, `create`, `check`) и `agents/conflict-auditor.md` — текстовая часть плагина.
- Манифесты: `.claude-plugin/` (плагин и маркетплейс Claude), `.codex-plugin/plugin.json` и `.agents/plugins/marketplace.json` (Codex).
- `ship.txt` — единственный список того, что поставляется; его читают `make tree`, CI и релиз.

## Инварианты и подводные камни
- Зависимости только stdlib, `gopkg.in/yaml.v3`, `github.com/bmatcuk/doublestar/v4`; `go.mod`/`go.sum` не меняются без причины.
- Хуки всегда выходят с кодом 0 и ничего не пишут на диск (fail-open); ошибки — в stderr.
- Публичное имя блока — только ` ```paraplane `; новых публичных имён не вводить.
- В `main` в `plugin.json` нет поля `version`: релиз проставляет его из тега (`vX.Y.Z` → `X.Y.Z`) в ветке `release`.
- `.gitattributes` фиксирует LF: shim и golden-файлы ломаются от CRLF на Windows.
- Каждый `README.md` имеет двойника `README.ru.md`; английский основной, обе версии синхронны.
- Тексты для Claude и пользователя — английские; текст из плана, конфига и ошибок YAML выводится через `textx`.

## Сборка, CI, релиз
- `make test` — `go vet` под windows и darwin плюс `go test ./...`; `make dist VERSION=…`; `make tree DIR=…`.
- CI: тесты на linux, macOS, windows, отдельные job с `-race` и кросс-`vet`.
- Релиз по тегу `v*`: job `validate` без секретов собирает 6 платформ, загружает архив и checksums до запуска стороннего кода; `publish` проверяет checksums и публикует релиз и ветку `release`.
