---
unit: internal/rules
source_hash: sha256:c677d1ae6a9e09e1a71eb537cdb15577e07e71551fee0c6ae70f83816819f232
commit: 45ec0164ada8ce7a4636b7626c1c8a0a5ce9582d
updated: "2026-10-23"
---
# internal/rules
Валидирует блок paraplane по правилам E0–E7, W1–W5 и проверяет изменения волны против owns (A1).

## Назначение
- Единое место правил спецификации (разд. 5, 5.1); CLI и хуки только форматируют результат.

## Публичный API / точки входа
- `Check(md, Options)` → `Result{Plan, Findings, Suggested}`.
- `Options`: `HotFiles`, `DefaultGate`, `MaxParallel` (0 → 6), `IsDir` (nil — W5 не проверяется), `Deadline`.
- `Result.Errors()`, `Warnings()`, `Summary()` — последняя строка `paraplane check`.
- `Audit(p, waveID, changed, planPath, ignore)` → находки A1; неизвестная волна — ошибка, не находка.
- `AuditWaves(p, waveIDs, …)` — то же для этапа из нескольких волн: файл может принадлежать задаче любой из них; `Where` — id через `,`, в тексте — через `, `. `Audit` = `AuditWaves` с одной волной.
- `AuditCommits(p, waveIDs, changed, commits []CommitFiles, planPath, ignore)` — то же плюс коммиты волн (`CommitFiles{Hash, Waves, Tasks, Files}`): коммит задачи — файл должен входить в `owns` названной задачи (`changed by T3 (commit h) but T3 does not own it`; несколько задач — `but none of them owns it`), коммит волны или этапа — в `owns` задачи любой названной волны (`changed in commit h but no task of W1, W2 owns it`). Находки коммитов идут первыми, по порядку коммитов, файл — один раз на коммит. `AuditWaves` = `AuditCommits` без коммитов.
- `Finding.String()` — `error E2 W1: …`.

## Структура и поток данных
- `plan.Load`: `SchemaError` → один E1, любая другая ошибка → один E0, дальше ничего не проверяется.
- План клонируется (`clone`), пути в клоне заменяются очищенными; неверные пути дают E1 и выбрасываются.
- Этапы: `structure` (E1) → `depends` (E1 для неизвестных, E3) → `gates` (E7, W2) → `tasks` (W1, W4, W5) → при отсутствии E1/E3 — `layout.ConflictsWithin` и `conflicts` (E2, E4, E5, E6) → W3, только если ошибок нет вовсе.
- E6 — только owns-конфликт между волнами, если более поздняя задача не зависит от ранней транзитивно; транзитивные depends считаются за один проход, так как без E1/E3 они ведут только в более ранние волны.
- Находки сортируются по коду, затем по позиции (план, волна, задачи волны); равные сохраняют порядок обнаружения.

## Инварианты и подводные камни
- `Finding.Msg` содержит текст плана как есть — вызывающий обязан его санировать (`textx`).
- `Result.Plan` — очищенный клон без неверных путей; `nil` при E0 и ошибке схемы. `Summary` считает `Plan == nil` невалидным.
- Дедлайн поиска конфликтов превращается в единственный E0 с текстом `layout.ErrDeadline`; хуки отличают его по тексту. CLI вызывает `Check` с нулевым дедлайном, хуки — с 5 с.
- Волны и задачи делят одно пространство id; id: буква, затем до 31 символа `[A-Za-z0-9_.-]`.
- Волна или задача без пригодного id адресуется позицией («2 of wave W1»), `Where` тогда пустой или id волны.
- Gate волны есть, если непуст после `TrimSpace` хотя бы один из `wave.gate`, `plan.gate`, `default_gate`.
- W5 только для литералов owns (без метасимволов), для которых `IsDir` истинно.
- `Suggested` не nil только вместе с W3.
- `Audit` берёт owns как есть: план должен прийти из `Check` (`Result.Plan`, пути уже очищены); CLI передаёт ему `Result.Plan` валидного плана.
- `Audit` сопоставляет через `pathset.Match` — с учётом регистра, в отличие от `Overlap`.
- `Audit` не различает задачи внутри волны: запись одной задачи в файл другой той же волны не видна.

## Связи (зависит от / используется в)
- Зависит от: `internal/plan`, `internal/pathset`, `internal/layout`.
- Используется в: `cmd/paraplane` (check, audit), `internal/hooks` (проверка плана при записи/чтении, ExitPlanMode, Codex Stop).
