# internal/hooks
Обработчики хуков Claude Code и Codex: проверка плана при выходе из plan mode и после правок, дайджесты правил, автоодобрение своего CLI.

- `hooks.go` — `Input` (payload хука), `Env` (с бюджетом `Ctx`), `begin`, `eventOf`, общий формат ответа
- `agent.go` — `Agent` (claude/codex): имя скилла и CLI в текстах для агента
- `session_start.go` — SessionStart: напоминание о правилах, ошибка конфига
- `pre_tool_use.go` — PreToolUse: ExitPlanMode, Skill, Bash
- `post_tool_use.go` — PostToolUse для Write/Edit, `postVerdict`
- `plan_text.go` — `Options`, тексты находок с цитированием (`quote`, `quoteNote`), `readPlan`, `rootRel`, `isPlanPath`
- `skill.go` — дайджесты `Digest`/`ExecDigest` для чужих скиллов
- `bash.go`, `permission_request.go` — одобрение `paraplane …` в Bash
- `codex_post.go` — PostToolUse Codex: слова shell (`shellx`) и apply_patch
- `progress_hook.go` — хук прогресса: запись задач, `SubagentStop`, показ через `track.Statuses`
- `codex_stop.go`, `codex_prompt.go` — Stop (план из транскрипта) и UserPromptSubmit Codex

Details: context/internal/hooks.md
