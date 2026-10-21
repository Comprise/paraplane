# paraplane

[English](README.md) | **Русский**

**Плагин для [Claude Code](https://docs.claude.com/en/docs/claude-code) и Codex CLI, который делает любой план
реализации пригодным для параллельного исполнения субагентами: план выполняется быстрее без потери качества.**

Параллельные субагенты ломают друг другу работу, когда двое правят один файл или когда следующий этап опирается
на то, что предыдущий ещё не закончил. paraplane добавляет в каждый план правила и проверяет их (без LLM и без
догадок):

- работа разбита на **задачи**, и каждая задача **владеет** своими файлами (`owns`), поэтому две параллельные
  задачи никогда не правят один файл;
- задачи сгруппированы в **волны**: задачи одной волны идут параллельно, волны — одна за другой;
- порядок задают **зависимости**, а не исходная нумерация: независимый «шаг 3» может пойти первым;
- каждая волна заканчивается **gate** (полным прогоном тестов), поэтому следующая волна не ломает предыдущую.

Сам paraplane планы не исполняет. Он следит, чтобы план можно было безопасно выполнять параллельно, и
подсказывает Claude, как его выполнять.

## Установка

**Claude Code:**

```
/plugin marketplace add Comprise/paraplane
/plugin install paraplane@paraplane
```

Без SSH-ключа для GitHub сначала задайте `CLAUDE_CODE_PLUGIN_PREFER_HTTPS=1`. Обновление:
`claude plugin update paraplane@paraplane` (или включите автообновление в `/plugin` → Marketplaces).

**Codex CLI:**

```
codex plugin marketplace add Comprise/paraplane --ref release
```

Затем в Codex: `/plugins` → установить `paraplane`, `/hooks` → **доверить** его хуки (недоверенные хуки не
запускаются).

**Требования:** Linux, macOS или Windows (amd64 или arm64); Go не нужен. `git` нужен для аудита волн и
прогресса. На Windows нужен [Git for Windows](https://git-scm.com/downloads/win). Подробнее:
[справочник → Требования](https://github.com/Comprise/paraplane/blob/main/docs/reference.ru.md#требования).

## Как пользоваться

Учить ничего не нужно: планируйте как обычно, paraplane работает в фоне.

1. **Составьте план** любым способом: plan mode, `superpowers:writing-plans`, файл плана в `docs/plans/`
   или собственная команда плагина:

   ```
   /paraplane:create добавить экспорт в CSV на страницу отчётов
   ```

   Claude добавляет в план блок `paraplane`: какие есть задачи, какими файлами владеет каждая, в какой волне
   она идёт.

2. **paraplane проверяет план.** Когда Claude выходит из plan mode или сохраняет файл плана, плагин проверяет
   блок. Если задачи одной волны правят один файл или задача зависит от ещё не сделанного, Claude не сможет
   закончить план, пока не исправит его. Проверить готовый план и узнать, можно ли его сократить:

   ```
   /paraplane:check docs/plans/2026-10-20-csv-export.md
   ```

3. **Выполните план.** Попросите Claude его выполнить (например, через `superpowers:subagent-driven-development`).
   Задачи каждой волны запускаются параллельно. После волны Claude проверяет, что изменились только файлы
   из `owns`, запускает gate, коммитит волну и показывает прогресс:

   ```
   paraplane · 2026-10-20-csv-export.md                      wave 2/3 · 50%
   ████████████████████░░░░░░░░░░░░░░░░░░░░  3/6 tasks

   W1  🟩🟩🟩  3/3  ✓ done     T1 T2 T3
   W2  🟨🟨    0/2  ▶ running  T4 T5
   W3  ⬜      0/1  · waiting  T6
   ```

В Codex команды называются `$paraplane:create`, `$paraplane:check` и `$paraplane:rules`.

### Как выглядит блок

````
```paraplane
version: 1
gate: "go vet ./... && go test ./..."
waves:
  - id: W1
    tasks:
      - id: T1
        title: "CSV writer"
        owns: ["internal/export/**"]
        verify: "go test ./internal/export/"
      - id: T2
        title: "Report query"
        owns: ["internal/reports/query.go"]
        verify: "go test ./internal/reports/"
  - id: W2
    tasks:
      - id: T3
        title: "Export button and handler"
        owns: ["internal/api/export.go", "web/reports/**"]
        reads: [internal/export/csv.go]
        depends: [T1, T2]
        verify: "go test ./internal/api/"
```
````

- `owns` — файлы, которые задача создаёт или меняет (трогать их может только она);
- `reads` — файлы, на которые задача опирается, не меняя их;
- `depends` — задачи прошлых волн, чей результат ей нужен;
- `verify` — собственная проверка задачи; `gate` — полная проверка после каждой волны.

Вручную блок пишут редко: его пишет Claude по правилам skill `paraplane:rules`.

### Что проверяется

| Код | Проблема |
|---|---|
| E0 | в плане нет блока `paraplane`, их несколько или YAML сломан |
| E1 | ошибка структуры: нет id или title, неизвестная задача в `depends`, плохой путь |
| E2 | две задачи одной волны владеют одним файлом |
| E3 | задача зависит от задачи той же или более поздней волны |
| E4 | задача читает файл, который меняет другая задача её волны |
| E5 | две задачи одной волны трогают один общий файл (`go.mod`, lock-файлы, миграции) |
| E6 | задача меняет файл более ранней задачи, не завися от неё |
| E7 | у волны нет gate |
| W1–W5 | предупреждения: нет `verify`, слишком много задач в волне, есть раскладка короче, пустой `owns`, каталог без `/**` |
| A1 | после волны изменился файл, которым не владеет ни одна задача волны |

Ошибки (E) делают план непригодным, предупреждения (W) — нет. Утилита командной строки делает то же вручную:

```
paraplane check [--suggest] <plan>   # проверить план; --suggest печатает раскладку короче
paraplane audit <plan> <wave>        # после волны: изменились только файлы из owns?
paraplane progress <plan>            # прогресс-бары по коммитам волн
```

## Настройка

Необязательно: `.paraplane/config.yaml` в проекте. Полезные ключи:

```yaml
mode: block            # или warn: сообщать о проблемах, а не блокировать
plan_paths: ["docs/plans/**/*.md"]
default_gate: "make test"
max_parallel: 6
```

Все ключи и значения по умолчанию: [справочник → Настройка](https://github.com/Comprise/paraplane/blob/main/docs/reference.ru.md#настройка).

## Подробнее

- [Справочник](https://github.com/Comprise/paraplane/blob/main/docs/reference.ru.md): формат блока и лимиты, все правила, вывод команд, безопасность аудита,
  хуки, отличия Codex, ограничения, решение проблем, разработка.
- [Спецификация](https://github.com/Comprise/paraplane/blob/main/docs/superpowers/specs/2026-10-20-paraplane-design.md).

## Автор и лицензия

Elia Samoylov. [MIT](LICENSE)
