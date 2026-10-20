# internal/plan
Извлечение блока ```paraplane из Markdown и строгий разбор его YAML в `Plan`.

- `extract.go` — поиск единственного fenced-блока `paraplane` по правилам CommonMark (`Extract`, `Block`)
- `plan.go` — типы `Plan`/`Wave`/`Task`, строгий разбор (`Parse`, `Load`), лимиты, типы ошибок

Details: context/internal/plan.md
