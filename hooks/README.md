# hooks/

`hooks.json` — регистрация хуков. Каждый вызывает `"${CLAUDE_PLUGIN_ROOT}"/bin/paraplane hook <event>`; логика — в `internal/hooks`.

| Событие | Matcher | Подкоманда | Тайм-аут | Что делает |
|---|---|---|---|---|
| `SessionStart` | — | `session-start` | 10 с | `additionalContext`: одна строка о правилах и `paraplane:rules`; при проблемах конфига — `systemMessage` |
| `PreToolUse` | `ExitPlanMode` | `pre-tool-use` | 10 с | проверяет план (`tool_input.plan`, иначе файл `planFilePath`, ≤ 1 МБ); есть ошибки E (включая E0) — `deny` со списком нарушений (≤ 20); только W — `additionalContext` |
| `PreToolUse` | `Skill` | `pre-tool-use` | 10 с | skill исполнения (имя содержит `execut` или `subagent-driven`, не `paraplane:`) — выжимка правил исполнения; skill с `plan` в имени — выжимка правил составления плана |
| `PreToolUse` | `Bash`, `if: Bash(paraplane *)` | `pre-tool-use` | 10 с | авто-одобрение `paraplane check <путь>`, `paraplane audit <путь> <волна>`, `paraplane progress <путь>`, `paraplane version` |
| `PostToolUse` | `Write\|Edit` | `post-tool-use` | 10 с | если файл входит в `plan_paths` — проверка плана: E0 — контекст, E1–E7 — `decision: block`, только W — контекст (и только для `Write`) |

- Хук никогда не блокирует сессию: шим всегда выходит с 0, ошибки идут в stderr (видны в `claude --debug`), ответ при ошибке пустой (fail-open). Плагин ничего не пишет в репозиторий.
- `ExitPlanMode` и `Skill` объединены в один matcher `ExitPlanMode|Skill`: обработчик один (`pre-tool-use`), разбор по имени инструмента — в бинаре.
- `mode: warn` в `.paraplane/config.yaml` заменяет `deny` и `block` на `additionalContext` с тем же текстом; `enabled: false` — хуки молчат.
- При проверке хук ограничен дедлайном 5 с: не уложился — ошибка E0 `plan is too complex to check in time; simplify brace patterns or split the plan`.
- `Edit` проверяется, только если правка касается блока `paraplane` или его ключей; отметка чекбоксов в плане шума не создаёт.
- Список событий сверяется с обработчиками в `cmd/paraplane` тестом `TestHooksJSONContract`: новое событие добавляйте в оба места.
- `if` у `PreToolUse` — фильтр Claude Code, чтобы бинарь не запускался на каждый Bash-вызов; само решение принимает бинарь.
