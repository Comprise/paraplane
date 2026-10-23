# internal/progress
Отрисовка прогресса исполнения плана текстом с эмодзи-полосами (`paraplane progress`, спецификация п. 7.1).

- `progress.go` — `Count` (одно правило «сделано»), `RenderFrom`: шапка, полоса задач, строка на волну, предупреждения; ширина с учётом широких рун
- `progress_test.go` — пример из спецификации, ширина 72, обрезка id, предупреждения

Details: context/internal/progress.md
