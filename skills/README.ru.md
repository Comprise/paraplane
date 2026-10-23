# skills/

[English](README.md) | **Русский**

Skills плагина. Claude Code находит каждый по `skills/<name>/SKILL.md`; этот README не загружается.

| Skill / агент | Вызов в Claude Code | Вызов в Codex | Назначение |
|---|---|---|---|
| `rules` | Claude сам, `/paraplane:rules` | `$paraplane:rules` | все правила: формат блока ```` ```paraplane ````, коды E0–E7, W1–W5, A1 и их исправление, как разбивать и переставлять этапы, правила исполнения и их приоритет над skills исполнения; подробности — `references/format.md`, `references/execution.md`, пример — `examples/plan.md` |
| `create` | `/paraplane:create <задача или путь к спецификации>` | `$paraplane:create …` | уточнить задачу, изучить код, составить план, `conflict-auditor`, исправить, `paraplane check`, сохранить в `plans_dir/YYYY-MM-DD-<slug>.md` |
| `check` | `/paraplane:check [путь]` | `$paraplane:check [путь]` | `paraplane check --suggest` + `conflict-auditor`, правка самого плана, отчёт о перестановках |
| `progress` | `/paraplane:progress [путь]` | `$paraplane:progress [путь]` | `paraplane progress [--done <ids>]` по запросу: исполняемый план или самый новый, закрытые незакоммиченные задачи сессии — в `--done`; вывод в блоке кода, ничего не меняется |
| `agents/conflict-auditor` | из skills; правило `rules`: при ≥ 2 задачах в волне | агенты плагина Codex не загружает: skill выполняет аудит сам по тексту агента | сверяет план с кодом: скрытые общие файлы, регистрационные точки, ломка API внутри волны, миграции; только `Read, Grep, Glob`; план правит основной Claude |

- `disable-model-invocation: true` у `create`, `check` и `progress`: они работают по запросу пользователя и запускаются только им. `rules` Claude загружает сам — при составлении, правке и проверке любого плана и при замечаниях хуков paraplane.
- Правила исполнения в `rules` главнее указаний любого skill исполнения (`superpowers:subagent-driven-development`, `superpowers:executing-plans` и т.п.): для плана с блоком ```` ```paraplane ```` задачи волны идут параллельно, а коммитит только координатор: раз на волну, либо раз на этап или на задачу, если этого требуют правила проекта. Прогресс показывают по-разному: в Claude Code — хуки, в Codex — координатор.
- Пример `rules/examples/plan.md` лежит внутри 4-бэктикового ограждения, поэтому сам файл не считается планом; тест `TestRulesExamplePlan` (`internal/plugintest`) прогоняет внутренний блок через `rules.Check` — при правке примера блок должен оставаться без ошибок.
- Тексты, которые выводят CLI и хуки (`plan ok: …`, `plan invalid: …`, `Shorter layout:`, `error A1 …`, `audit ok: …`, `quote glob patterns`, предупреждения `paraplane progress`), skills приводят дословно: при их изменении в Go правьте и skills. Выжимки правил в хуках (`internal/hooks`) — короткая версия `rules`, при изменении правил правьте обе.
- В Claude Code skills вызывают `paraplane` по имени: `bin/` плагина в PATH. В Codex `bin/` в PATH нет: skills просят брать команду по абсолютному пути из текста хука (`<каталог плагина>/bin/paraplane`). Подробности Codex — `rules/references/codex.md`.
