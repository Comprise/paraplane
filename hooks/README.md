# hooks/

**English** | [Русский](README.ru.md)

`hooks.json` registers the hooks. Each calls `"${CLAUDE_PLUGIN_ROOT}"/bin/paraplane hook <event>`; the logic lives in `internal/hooks`.

| Event | Matcher | Subcommand | Timeout | What it does |
|---|---|---|---|---|
| `SessionStart` | — | `session-start` | 10 s | `additionalContext`: one line about the rules and `paraplane:rules`; on config problems — `systemMessage` |
| `PreToolUse` | `ExitPlanMode` | `pre-tool-use` | 10 s | validates the plan (`tool_input.plan`, otherwise the file `planFilePath`, ≤ 1 MB); E errors (including E0) — `deny` with a list of violations (≤ 20); only W — `additionalContext` |
| `PreToolUse` | `Skill` | `pre-tool-use` | 10 s | an execution skill (the name contains `execut` or `subagent-driven`, not `paraplane:`) — a digest of the execution rules; a skill with `plan` in its name — a digest of the plan-writing rules |
| `PreToolUse` | `Bash`, `if: Bash(paraplane *)` | `pre-tool-use` | 10 s | auto-approves `paraplane check <path>`, `paraplane audit <path> <wave>`, `paraplane progress <path>`, `paraplane version` |
| `PostToolUse` | `Write\|Edit` | `post-tool-use` | 10 s | if the file is in `plan_paths` — plan validation: E0 — context, E1–E7 — `decision: block`, only W — context (and only for `Write`) |

## Codex: `hooks/codex.json`

Codex reads only `hooks/codex.json` (it ignores `hooks/hooks.json`). All hooks call
`"${CLAUDE_PLUGIN_ROOT}"/bin/paraplane hook <event> --agent codex`; there is no `if` field — the matcher and the binary filter.

| Event | Matcher | Subcommand | Timeout | What it does |
|---|---|---|---|---|
| `SessionStart` | — | `session-start` | 10 s | the same `additionalContext` with `$paraplane:rules` |
| `UserPromptSubmit` | — | `prompt-submit` | 10 s | the prompt mentions `$…` with `execut`/`subagent-driven` — a digest of the execution rules; with `plan` — of the plan-writing rules |
| `PostToolUse` | `^(Bash\|apply_patch)$` | `post-tool-use` | 10 s | path words are taken from `tool_input.command` (relative ones from `cwd`), from a patch — the headers `*** Add/Update File:`, `*** Move to:`; existing regular files matching `plan_paths` are validated: E0 — context, E1–E7 — `block` |
| `Stop` | — | `stop` | 10 s | in Plan Mode `last_assistant_message` is empty: the transcript (`transcript_path`, a regular file, tail ≤ 4 MB) — the assistant's last `<proposed_plan>` of the current turn; E errors — `{"decision":"block","reason":…}`; `stop_hook_active: true`, no transcript or plan — silent |
| `PermissionRequest` | `^Bash$` | `permission-request` | 10 s | `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}` only for the strict form of the command `paraplane check/audit/progress/version` with the absolute CLI path |

- The hook texts name the CLI by the absolute path `$CLAUDE_PLUGIN_ROOT/bin/paraplane` (`bin/` is not on the Codex PATH); skills are named only the Codex way, `$paraplane:x` (the skill texts themselves name both `/paraplane:x` and `$paraplane:x`).
- Codex does not load plugin agents, so there are no events for `conflict-auditor`: the skills perform the audit.
- The Codex hooks must be trusted in `/hooks`.

## General

- A hook never blocks the session: the shim always exits 0, errors go to stderr (visible in `claude --debug`), the response on error is empty (fail-open). The plugin writes nothing to the repository.
- `ExitPlanMode` and `Skill` are combined into one matcher `ExitPlanMode|Skill`: there is one handler (`pre-tool-use`), dispatch by tool name happens in the binary.
- `mode: warn` in `.paraplane/config.yaml` replaces `deny` and `block` with `additionalContext` of the same text; `enabled: false` — the hooks stay silent.
- During validation the hook is limited by a 5 s deadline: if it does not finish — error E0 `plan is too complex to check in time; simplify brace patterns or split the plan`.
- `Edit` is checked only if the edit touches the `paraplane` block or its keys; ticking checkboxes in a plan creates no noise.
- The list of events is checked against the handlers in `cmd/paraplane` by the test `TestHooksJSONContract`: add a new event in both places.
- `if` on `PreToolUse` is a Claude Code filter so that the binary does not start on every Bash call; the binary makes the decision itself.
