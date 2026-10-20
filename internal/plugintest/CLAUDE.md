# internal/plugintest
Тесты упаковки плагина: frontmatter скиллов и агентов, POSIX-шим, манифесты, ship.txt, примеры планов.

- `doc.go` — только объявление пакета; весь код в `_test.go`
- `frontmatter_test.go` — строгий разбор frontmatter `skills/*/SKILL.md` и `agents/*.md`, константа `root`
- `shim_test.go` — поведение `bin/paraplane` с фейковым бинарником и без него
- `packaging_test.go` — `.gitattributes`, `ship.txt`, пары README, workflows и Makefile
- `codex_test.go` — `.codex-plugin/plugin.json` и marketplace Codex
- `example_test.go` — примеры планов из скилла `rules` и спецификации проходят `rules.Check` без ошибок

Details: context/internal/plugintest.md
