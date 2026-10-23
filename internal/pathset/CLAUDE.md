# internal/pathset
Валидация путей и глоб-шаблонов плана, консервативная проверка пересечения шаблонов и касания горячих файлов.

- `pathset.go` — `Clean`, `Overlap`/`Compile`/`Overlaps`, `Touches`, `Match`/`MatchAny`, `HasMeta`, `SpansSegments`, разбор класса `classSpans`, свёртка регистра
- `bench_test.go` — бенчмарки `Overlap`

Details: context/internal/pathset.md
