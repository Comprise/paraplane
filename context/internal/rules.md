---
unit: internal/rules
source_hash: sha256:652405ed42d8bb1f397c6f6798f583a34f7ba5d0ce7e41f02864556b15198c1f
commit: ee26fb1c0e795a3aaa095fbd0556059d4c9450de
updated: "2026-10-23"
---
# internal/rules
Валидирует блок paraplane по правилам E0–E7, W1–W5 и проверяет изменения волны против owns (A1).

## Назначение
- Единое место правил спецификации (разд. 5, 5.1); CLI и хуки только форматируют результат.

## Публичный API / точки входа
- `Check(md, Options)` → `Result{Plan, Findings, Suggested, TimedOut}`.
- `Options`: `HotFiles`, `DefaultGate`, `MaxParallel` (0 → 6), `IsDir` (nil — W5 не проверяется), `Deadline`.
- `OptionsFor(cfg config.Config, root, deadline)` — `Options` проекта из конфига: `HotFiles`, `DefaultGate`, `MaxParallel`, `IsDir` через `os.Stat` под `root`, дедлайн задаёт вызывающий (нулевой — без ограничения). Перенесено из `hooks.Options` (A-M1); `hooks.Options` пока остаётся у хуков до R10.
- `Result.TimedOut` — дедлайн истёк до конца проверки (`plan.ErrDeadline` при разборе или `layout.ErrDeadline` в поиске конфликтов); тогда единственная находка — E0, и она ничего не говорит о самом плане. Вызывающим не нужно сравнивать текст сообщения.
- `Result.Errors()`, `Warnings()`, `Summary()` — последняя строка `paraplane check`.
- `AuditCommits(p, waveIDs, changed, commits []CommitFiles, planPath, ignore)` — основной API аудита A1: файлы рабочего дерева `changed` должны входить в `owns` задачи одной из волн `waveIDs` (этап из нескольких волн: файл может принадлежать задаче любой из них; `Where` — id через `,`, в тексте — через `, `); неизвестная волна — ошибка, не находка. Плюс коммиты волн (`CommitFiles{Hash, Waves, Tasks, Files}`): коммит задачи — файл должен входить в `owns` названной задачи (`changed by T3 (commit h) but T3 does not own it`; несколько задач — `but none of them owns it`), коммит волны или этапа — в `owns` задачи любой названной волны (`changed in commit h but no task of W1, W2 owns it`). Находки коммитов идут первыми, по порядку коммитов, файл — один раз на коммит.
- `CommitsOfWaves(p, waves, commits)` — сколько коммитов относится к волнам по тому же правилу, по которому их проверяет `AuditCommits` (коммит называет волну из списка или задачу такой волны; волны, которых нет в плане, не считаются). Заменяет `waveCommits` из CLI (Q-M9).
- `Finding.String()` — `error E2 W1: …`.

## Структура и поток данных
- `plan.LoadWithin(md, Deadline)`: `SchemaError` → один E1, любая другая ошибка → один E0 (при `plan.ErrDeadline` — текст `layout.ErrDeadline` и `TimedOut`), дальше ничего не проверяется.
- План клонируется (`clone`), пути в клоне заменяются очищенными; неверные пути дают E1 и выбрасываются.
- Этапы: `structure` (E1) → `depends` (E1 для неизвестных, E3) → `gates` (E7, W2) → `tasks` (W1, W4, W5) → при отсутствии E1/E3 — `layout.ConflictsWithin` и `conflicts` (E2, E4, E5, E6) → W3, только если ошибок нет вовсе.
- E6 — только owns-конфликт между волнами, если более поздняя задача не зависит от ранней транзитивно; транзитивные depends считаются за один проход, так как без E1/E3 они ведут только в более ранние волны.
- Находки сортируются по коду, затем по позиции (план, волна, задачи волны); равные сохраняют порядок обнаружения (`slices.SortStableFunc`).
- Аудит устроен как `auditor`: `relevant` — принадлежность коммита волнам (общая с `CommitsOfWaves`), `commit` — проверка файлов одного коммита, затем рабочее дерево.

## Инварианты и подводные камни
- `Finding.Msg` содержит текст плана как есть — вызывающий обязан его санировать (`textx`).
- `Result.Plan` — очищенный клон без неверных путей; `nil` при E0 и ошибке схемы. `Summary` считает `Plan == nil` невалидным.
- Дедлайн — при разборе (`plan.ErrDeadline`) или в поиске конфликтов (`layout.ErrDeadline`) — превращается в единственный E0 всегда с текстом `layout.ErrDeadline` и `TimedOut = true`: хуки до R10 ещё распознают дедлайн сравнением текста, и общий текст не даёт им пропустить непроверенный план. CLI вызывает `Check` с нулевым дедлайном, хуки — с 5 с.
- W5 вызывает `IsDir` (`os.Stat`) только до дедлайна: после него W5 пропускается (это предупреждение), а поиск конфликтов сразу даёт E0 (P-L5).
- Волны и задачи делят одно пространство id; грамматика id — `ids.Valid` (буква, затем до 31 символа `[A-Za-z0-9_.-]`).
- Волна или задача без пригодного id адресуется позицией («2 of wave W1»), `Where` тогда пустой или id волны.
- Gate волны есть, если непуст после `TrimSpace` хотя бы один из `wave.gate`, `plan.gate`, `default_gate`.
- W5 только для литералов owns (без метасимволов), для которых `IsDir` истинно.
- `Suggested` не nil только вместе с W3.
- `AuditCommits` берёт owns как есть: план должен прийти из `Check` (`Result.Plan`, пути уже очищены); CLI передаёт ему `Result.Plan` валидного плана.
- Аудит сопоставляет через `pathset.Match`/`MatchAny` — с учётом регистра, в отличие от `Overlap`.
- Рабочее дерево аудит не делит по задачам внутри волны: запись одной задачи в файл другой той же волны видна только в коммитах задач.

## Связи (зависит от / используется в)
- Зависит от: `internal/plan`, `internal/pathset`, `internal/layout`, `internal/ids`, `internal/config` (тип `Config` для `OptionsFor`).
- Используется в: `cmd/paraplane` (check, audit), `internal/hooks` (проверка плана при записи/чтении, ExitPlanMode, Codex Stop).
