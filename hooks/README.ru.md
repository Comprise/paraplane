# hooks/

[English](README.md) | **Русский**

`hooks.json` — регистрация хуков. Каждый вызывает `"${CLAUDE_PLUGIN_ROOT}"/bin/paraplane hook <event>`; логика — в `internal/hooks`.

| Событие | Matcher | Подкоманда | Тайм-аут | Что делает |
|---|---|---|---|---|
| `SessionStart` | — | `session-start` | 10 с | `additionalContext`: одна строка о правилах и `paraplane:rules`; при проблемах конфига — `systemMessage` |
| `PreToolUse` | `ExitPlanMode` | `pre-tool-use` | 10 с | проверяет план (`tool_input.plan`, иначе файл `planFilePath`, ≤ 1 МБ); есть ошибки E (включая E0) — `deny` со списком нарушений (≤ 20); только W — `additionalContext` |
| `PreToolUse` | `Skill` | `pre-tool-use` | 10 с | skill исполнения (имя содержит `execut` или `subagent-driven`, не `paraplane:`) — выжимка правил исполнения; skill с `plan` в имени — выжимка правил составления плана |
| `PreToolUse` | `Bash`, `if: Bash(paraplane *)` | `pre-tool-use` | 10 с | авто-одобрение `paraplane check <путь>`, `paraplane audit <путь> <волна>`, `paraplane progress <путь>`, `paraplane version` |
| `PostToolUse` | `Write\|Edit` | `post-tool-use` | 10 с | если файл входит в `plan_paths` — проверка плана: E0 — контекст, E1–E7 — `decision: block`, только W — контекст (и только для `Write`) |

## Codex: `hooks/codex.json`

Codex читает только `hooks/codex.json` (`hooks/hooks.json` игнорирует). Все хуки вызывают
`"${CLAUDE_PLUGIN_ROOT}"/bin/paraplane hook <event> --agent codex`; поля `if` нет — фильтрует matcher и бинарь.

| Событие | Matcher | Подкоманда | Тайм-аут | Что делает |
|---|---|---|---|---|
| `SessionStart` | — | `session-start` | 10 с | тот же `additionalContext` с `$paraplane:rules` |
| `UserPromptSubmit` | — | `prompt-submit` | 10 с | промпт упоминает `$…` с `execut`/`subagent-driven` — выжимка правил исполнения; с `plan` — правил составления плана |
| `PostToolUse` | `^(Bash\|apply_patch)$` | `post-tool-use` | 10 с | из `tool_input.command` берутся слова-пути (относительные — от `cwd`), из патча — заголовки `*** Add/Update File:`, `*** Move to:`; совпавшие с `plan_paths` существующие обычные файлы проверяются: E0 — контекст, E1–E7 — `block` |
| `Stop` | — | `stop` | 10 с | в Plan Mode `last_assistant_message` пуст: транскрипт (`transcript_path`, обычный файл, хвост ≤ 4 МБ) — последний `<proposed_plan>` ассистента текущего хода; есть E — `{"decision":"block","reason":…}`; `stop_hook_active: true`, нет транскрипта или плана — молчит |
| `PermissionRequest` | `^Bash$` | `permission-request` | 10 с | `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}` только для строгой формы команды `paraplane check/audit/progress/version` с абсолютным путём CLI |

- В тексте хуков CLI назван абсолютным путём `$CLAUDE_PLUGIN_ROOT/bin/paraplane` (`bin/` не в PATH Codex); skills названы только в форме Codex, `$paraplane:x` (тексты самих skills называют оба варианта — `/paraplane:x` и `$paraplane:x`).
- Агенты плагина Codex не загружает, поэтому событий для `conflict-auditor` нет: аудит выполняют skills.
- Хуки Codex нужно доверить в `/hooks`.

## Общее

- Хук никогда не блокирует сессию: шим всегда выходит с 0, ошибки идут в stderr (видны в `claude --debug`), ответ при ошибке пустой (fail-open). Плагин ничего не пишет в репозиторий.
- `ExitPlanMode` и `Skill` объединены в один matcher `ExitPlanMode|Skill`: обработчик один (`pre-tool-use`), разбор по имени инструмента — в бинаре.
- `mode: warn` в `.paraplane/config.yaml` заменяет `deny` и `block` на `additionalContext` с тем же текстом; `enabled: false` — хуки молчат.
- При проверке хук ограничен дедлайном 5 с: не уложился — ошибка E0 `plan is too complex to check in time; simplify brace patterns or split the plan`.
- `Edit` проверяется, только если правка касается блока `paraplane` или его ключей; отметка чекбоксов в плане шума не создаёт.
- Список событий сверяется с обработчиками в `cmd/paraplane` тестом `TestHooksJSONContract`: новое событие добавляйте в оба места.
- `if` у `PreToolUse` — фильтр Claude Code, чтобы бинарь не запускался на каждый Bash-вызов; само решение принимает бинарь.
