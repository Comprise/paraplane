---
unit: _root
source_hash: sha256:b8db7d439e1bb07dd1b24ad940cd86ab36b1dff1e22132b3a72c0359acc9ff21
commit: ee26fb1c0e795a3aaa095fbd0556059d4c9450de
updated: "2026-10-23"
---
# _root
Плагин paraplane: правила и проверки плана для параллельного исполнения субагентами в Claude Code и Codex.

## Назначение
- Плагин только задаёт и проверяет правила плана; планы он не исполняет и субагентов не запускает.
- Источник правды по поведению — `docs/superpowers/specs/2026-10-20-paraplane-design.md`; README, справочник и skills пересказывают его. Дизайн-доки 2026-10-22/23 в неё влиты; при расхождении действует она.
- Прогресс показывают: в Claude Code — хуки (`PostToolUse`, `SubagentStop` + `Stop`), в Codex — координатор; тексты для модели (`skills/rules/references/execution.md`, `codex.md`, выжимки в `internal/hooks`) должны говорить одно и то же.

## Структура и поток данных
- Один бинарник `paraplane` (`cmd/paraplane`) обслуживает CLI (`check`, `audit`, `progress`, `version`) и хуки (`hook <event> [--agent codex]`).
- `bin/paraplane` — POSIX-shim: выбирает `libexec/paraplane-<os>-<arch>[.exe]`; бинарники собираются только в релизе (`make dist`), в `main` их нет.
- `hooks/hooks.json` (Claude) и `hooks/codex.json` (Codex) вызывают shim; `PostToolUse` Claude ловит и `Bash`/`Agent`/`Task` для хука прогресса. `skills/` (`rules`, `create`, `check`, `progress`) и `agents/conflict-auditor.md` — текстовая часть плагина.
- Что сделано в плане, знают коммиты (трейлеры `Paraplane-Wave`/`Paraplane-Task`), состояние вне рабочего дерева `<git-dir>/paraplane/closed.json` (`internal/state`) и `--done`; сводит их `internal/track`. id задач и волн везде проверяет `internal/ids`.
- Манифесты: `.claude-plugin/` (плагин и маркетплейс Claude), `.codex-plugin/plugin.json` и `.agents/plugins/marketplace.json` (Codex).
- `ship.txt` — единственный список того, что поставляется; его читают `make tree`, CI и релиз.

## Инварианты и подводные камни
- Зависимости только stdlib, `go.yaml.in/yaml/v3`, `github.com/bmatcuk/doublestar/v4`; `go.mod`/`go.sum` не меняются без причины (`golang.org/x/sys` не вводится).
- Хуки всегда выходят с кодом 0 (fail-open); ошибки — в stderr, с `PARAPLANE_DEBUG=1` — ещё строка времени на хук.
- Хуки никогда не пишут в рабочее дерево. Хук прогресса пишет только `<git-dir>/paraplane/` и только когда речь о плане (строка `Paraplane-Task` с планом, коммит с трейлерами плана или файл состояния уже есть); в репозитории без планов каталог не создаётся.
- Один `context.Context` на процесс хука, 7 с (тайм-аут хоста 10 с); каждый вызов git и проверка плана (`min(5 с, остаток)`) берут оставшееся. `hookEventName` ответа равен событию; `Stop` Claude отвечает только `systemMessage`.
- Каждый процесс git — с `GIT_ALLOW_PROTOCOL=none`, `exec.CommandContext` и `WaitDelay`; хеши из файлов проверяются и идут после `--end-of-options`. Модель угроз — раздел «Threat model» в `docs/reference.md`.
- Текст плана в ответах хуков цитируется строками с `> ` и заканчивается фразой о том, что это данные.
- Публичное имя блока — только ` ```paraplane `; новых публичных имён не вводить.
- В `main` в `plugin.json` нет поля `version`: релиз проставляет его из тега (`vX.Y.Z` → `X.Y.Z`) в ветке `release`.
- `.gitattributes` фиксирует LF: shim и golden-файлы ломаются от CRLF на Windows.
- Каждый `README.md` имеет двойника `README.ru.md` (так же `docs/reference.md`/`.ru.md` и `CHANGELOG.md`/`.ru.md`); английский основной, обе версии синхронны. Изменение поведения записывается в `CHANGELOG*` (раздел версии, при необходимости — «Migration»).
- `README` — что это, установка, как пользоваться; всё подробное — в `docs/reference*.md`. `docs/` не поставляется, поэтому README ссылается на справочник абсолютным URL на `main`.
- Тексты для Claude и пользователя — английские; текст из плана, конфига и ошибок YAML выводится через `textx`.

## Сборка, CI, релиз
- `make test` — `go vet` под windows и darwin плюс `go test ./...`; `make dist VERSION=…`; `make tree DIR=…`.
- CI: тесты на linux, macOS, windows, отдельные job с `-race` и кросс-`vet`.
- Релиз по тегу `v*`: job `validate` без секретов собирает 6 платформ, загружает архив и checksums до запуска стороннего кода; `publish` проверяет checksums и публикует релиз и ветку `release`.
