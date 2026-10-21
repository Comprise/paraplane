---
unit: internal/hooks
source_hash: sha256:38f9babdb54e2c3c0d4962f3b3901968f2c5aa127b906ff9520c383323e8b13c
commit: c561fda5a44cad4ab07769acc3b811773093822d
updated: "2026-10-21"
---
# internal/hooks
Обработчики хуков Claude Code и Codex: проверка плана, дайджесты правил, автоодобрение CLI paraplane.

## Назначение
- Превращает payload хука (JSON на stdin) в ответ хука (JSON на stdout) для Claude Code и Codex.
- Проверка плана — через `rules.Check`; хуки только выбирают, когда проверять и как ответить (deny / block / context).

## Публичный API / точки входа
- `SessionStart`, `PostToolUse` — общие для обоих агентов; `PreToolUse` — только Claude Code.
- `Stop`, `PromptSubmit`, `PermissionRequest` — только Codex; при `Agent != Codex` ничего не отвечают.
- `Options(env)` — опции `rules.Check` с дедлайном 5 с; CLI берёт их же и обнуляет `Deadline`.
- `Agent`, `ParseAgent`, `Agent.Skill`, `Agent.CLI`; константы `SessionContext`, `Digest`, `ExecDigest`.
- Каждый обработчик: `func(Env, Input) ([]byte, error)`; `nil` — пустой ответ (обычный поток агента).

## Структура и поток данных
- `Env.Config.Enabled == false` — все обработчики молчат.
- PreToolUse (подключён только в `hooks/hooks.json`):
  - `ExitPlanMode`: план из `tool_input.plan`, иначе файл `planFilePath`; ошибки (включая E0) → `permissionDecision: deny`, только предупреждения → context.
  - `Skill`: чужой скилл с `execut`/`subagent-driven` в имени → `ExecDigest`, с `plan` → `Digest`; скиллы `paraplane:*` пропускаются.
  - `Bash`: точная команда своего CLI → `allow`.
- PostToolUse (Write/Edit): файл должен попадать в `plan_paths` внутри корня.
  - Edit проверяется, только если `editTouchesPlan`: строки правки содержат ключ из `planKeys` или `new_string` пересекается с блоком.
  - `postVerdict`: E0 → context; E1–E7 → `decision: block`; предупреждения → context только для Write.
- Codex PostToolUse (`Bash`, `apply_patch`) — `codexPost`:
  - кандидаты в пути — слова команды (`shellLex` → `shellWords`) или заголовки патча (`patchFiles`);
  - вид касания: запись (редирект, `tee`, цель `cp`/`mv`/`install`, Add File/Move to), правка (`sed -i` с простыми `s`-командами, Update File), чтение (всё остальное);
  - чтение даёт только context по E1–E7 и никогда не блокирует;
  - сначала проверяются записанные/правленые файлы, потом прочитанные; первый block побеждает, иначе первый ответ.
- Codex Stop: план — последний `<proposed_plan>` из `last_assistant_message`, иначе из хвоста транскрипта (до 4 МиБ) для текущего `turn_id`; ошибки → `decision: block` (в `warn` — `systemMessage`).
- Codex PromptSubmit: упоминания `$skill`/`$plugin:skill` в промпте → дайджест, `ExecDigest` важнее `Digest`.
- Codex PermissionRequest: заменяет Bash-allow из PreToolUse.

## Инварианты и подводные камни
- Режим `mode: warn`: всё, что блокировало бы, уходит в context / `systemMessage`.
- Тексты для агента пишутся в форме Claude Code; `Agent.text` переписывает `` `paraplane check|audit|progress `` и `skill paraplane:` для Codex. Текст плана и пути не переписываются; в находках адаптируется только W3.
- В Codex CLI — абсолютный `$CLAUDE_PLUGIN_ROOT/bin/paraplane`: в shell Codex нет `bin/` плагина в PATH. Корень проходит `pluginRootSafe` (текст вставляется в shell), иначе — голое имя.
- Автоодобрение Bash:
  - regexp `bashOwnCLI` сверяет сырую команду целиком, без trim; `..` запрещено;
  - Claude: голое имя одобряется, только если `paraplane` в PATH — бинарник плагина (`bashShadowed`, сравнение после EvalSymlinks) или его в PATH хука нет вовсе (Bash-инструмент дописывает `bin/` плагинов в конец PATH); без абсолютного `CLAUDE_PLUGIN_ROOT` — отказ;
  - Codex: одобряется только абсолютная форма `Codex.CLI()`, голое имя — никогда.
- Codex PreToolUse с allow переписывает команду, поэтому для Codex одобрение идёт через PermissionRequest.
- Один дедлайн `Options` на вызов хука: в `codexPost` до `codexMaxPlans` (8) файлов делят 5 с; E0 с сообщением `layout.ErrDeadline` `postVerdict` считает ошибкой (block), а не «блока ещё нет»; у записанного или изменённого файла, проверяемого не первым (`shared`), такой E0 даёт block «was not checked» с советом запустить `paraplane check`.
- `readPlan` через `fsx`: симлинки, FIFO, устройства, каталоги и файлы больше `fsx.MaxFile` молча пропускаются — хук не должен зависнуть на открытии.
- `rootRel`: обратный слеш — разделитель на любой ОС; абсолютный путь вне корня повторно сравнивается после EvalSymlinks каталогов (macOS `/var`), последний элемент не разрешается.
- Лексер shell ищет только литеральные имена файлов: раскрытия не вычисляются, тела heredoc пропускаются, `<`/`>` внутри `(( ))`, `$(( ))`, `$[ ]` — арифметика. Косвенная запись (скрипт, `$(...)`) не видна — её ловят Stop и скиллы.
- `sed -i` со скриптом не только из `s`-команд, с `-f`, с пустым скриптом или с `&`/`\N` в замене — это запись (полный вердикт Write), а не правка. `-i ''`/`-i .bak` — суффикс BSD; `-l N`, `--line-length N` и `--` учитываются.
- Лексер: `#` в начале слова после пробела или конца команды (не после `)` и обратной кавычки) — комментарий до конца строки или обратной кавычки; `>&файл` — запись в файл, `>&2`/`>&-` — дескриптор.
- Формат транскрипта Codex не документирован. Если хвост содержит `turn_id`, но нет ни одной записи `response_item` message, — `systemMessage` `codexUnknownTranscript`; пустой или ещё не записанный транскрипт — молчание.
- Stop срабатывает один раз за ход: при `stop_hook_active` ничего не делает.
- Длинные значения из плана и ФС режутся `textx.Line`/`textx.Block` (`maxLine`, `maxPath`, `maxE0`, `maxFindings`); макет W3 длиннее 64 КиБ заменяется ссылкой на `paraplane check --suggest` (обрезанный YAML был бы невалиден).
- `encodeJSON` без HTML-экранирования и без перевода строки в конце; golden-файлы `testdata/*.golden.json` пишутся вручную.
- `race_on_test.go`/`race_off_test.go`: под `-race` тесты на время по часам отключаются.

## Связи (зависит от / используется в)
- Зависит от: `internal/config`, `internal/rules`, `internal/plan`, `internal/layout`, `internal/pathset`, `internal/fsx`, `internal/textx`.
- Используется в: `cmd/paraplane` (`hookHandlers`, `Options`, `Env` для CLI).
- Подключение: `hooks/hooks.json` (Claude Code), `hooks/codex.json` (Codex, `--agent codex`).
