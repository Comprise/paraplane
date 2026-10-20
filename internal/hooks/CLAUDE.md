# internal/hooks
Обработчики хуков Claude Code и Codex: проверка плана при выходе из plan mode и после правок, дайджесты правил, автоодобрение своего CLI.

- `hooks.go` — `Input` (payload хука), `Env`, общий формат ответа
- `agent.go` — `Agent` (claude/codex): имя скилла и CLI в текстах для агента
- `session_start.go` — SessionStart: напоминание о правилах, ошибка конфига
- `pre_tool_use.go` — PreToolUse: ExitPlanMode, Skill, Bash
- `post_tool_use.go` — PostToolUse для Write/Edit, `postVerdict`
- `plan_text.go` — `Options`, тексты находок, `readPlan`, `rootRel`, `isPlanPath`
- `skill.go` — дайджесты `Digest`/`ExecDigest` для чужих скиллов
- `bash.go`, `permission_request.go` — одобрение `paraplane …` в Bash
- `codex_post.go` — PostToolUse Codex: лексер shell и apply_patch
- `codex_stop.go`, `codex_prompt.go` — Stop (план из транскрипта) и UserPromptSubmit Codex

Details: context/internal/hooks.md
