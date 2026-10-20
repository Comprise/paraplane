---
unit: internal/plugintest
source_hash: sha256:73085cc477a8672447e177c5ad325e58025bae45f416283d6fa9ccd0e968ae9b
updated: "2026-10-20"
---
# internal/plugintest
Тесты упаковки плагина: frontmatter скиллов и агентов, POSIX-шим, манифесты Claude/Codex, ship.txt, примеры планов.

## Назначение
- Ловит ошибки в файлах, которые не компилируются и сами себя не проверяют: frontmatter, `bin/paraplane`, манифесты, workflows, примеры в документации.
- Пакет без production-кода: `doc.go` только объявляет пакет, всё остальное — `_test.go`. Запускается обычным `go test ./...`.

## Структура и поток данных
- `root = "../.."` — все пути считаются от корня репозитория относительно каталога пакета.
- `frontmatter_test.go` — строгий YAML-разбор (`KnownFields(true)`) frontmatter в `skillMeta`/`agentMeta`: неизвестный ключ — ошибка. Проверяет совпадение `name` с именем каталога/файла, непустой `description`, `disable-model-invocation: true` у скиллов `create` и `check`, у агентов — наличие `tools` без `Write`, `Edit`, `Bash`, `NotebookEdit`.
- `shim_test.go` — копирует настоящий `bin/paraplane` во временный каталог плагина, подкладывает фейковый `libexec/paraplane-<os>-<arch>` (sh-скрипт) и запускает шим с `CLAUDE_PROJECT_DIR`.
- `packaging_test.go` — `eol=lf` для `bin/paraplane` в `.gitattributes`; `ship.txt` — непустой список уникальных относительных путей, которые существуют (кроме `libexec`) и включают обязательные; у каждого `README.md` есть `README.ru.md` и наоборот; Makefile и workflows берут состав релиза из `ship.txt`, версию — из тега без `v`.
- `codex_test.go` — общие поля `.codex-plugin/plugin.json` совпадают с `.claude-plugin/plugin.json`, `hooks` = `./hooks/codex.json`, `version` на main не задан; marketplace Codex — один плагин `paraplane` с источником `local ./`.
- `example_test.go` — пример из `skills/rules/examples/plan.md` (внутри четырёх обратных кавычек) и YAML-блок из раздела 4 спецификации проходят `rules.Check` без ошибок (предупреждения только логируются).

## Инварианты и подводные камни
- Списки ключей в `skillMeta`/`agentMeta` — это все ключи из документации Claude Code; новый поддерживаемый ключ frontmatter нужно добавить сюда, иначе тест упадёт. Написание ключей у скиллов и агентов разное (`disallowed-tools` против `disallowedTools`).
- Ожидаемое поведение шима: хук никогда не возвращает ненулевой код и при падении бинарника не печатает частичный ответ; CLI-команда пробрасывает код выхода; отсутствующий бинарник и бинарник без бита исполнения различаются в сообщении; `enabled: false` ищется вверх от каталога проекта до ближайшего `.git` или `.paraplane/config.yaml`.
- Тесты шима пропускаются вне linux/darwin и вне amd64/arm64.
- `TestSpecExamplePlan` ищет заголовок `## 4. Блок \`paraplane\``: переименование раздела спецификации ломает тест.
- `TestWorkflowsUseShipListAndTagVersion` проверяет точные подстроки в `release.yml` и `Makefile` — правка этих команд требует правки теста.
- Обход README пропускает каталоги, начинающиеся с `.`, и `testdata`.

## Связи (зависит от / используется в)
- Зависит от: `internal/plan` (`Extract`), `internal/rules` (`Check`), `gopkg.in/yaml.v3`; читает `skills/`, `agents/`, `bin/paraplane`, `hooks/codex.json`, `.claude-plugin/`, `.codex-plugin/`, `.agents/plugins/marketplace.json`, `ship.txt`, `.gitattributes`, `Makefile`, `.github/workflows/`, спецификацию.
- Никем не импортируется.
