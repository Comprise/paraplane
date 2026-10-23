# internal/shellx
Лексер shell-команд для хуков Codex: слова команды и то, как она трогает файл (чтение, правка `sed -i`, запись), плюс имена простых команд.

- `lexer.go` — `lexer`: кавычки, редиректы, heredoc, арифметика, комментарии, концы команд
- `words.go` — `Kind`, `Word`, `Words`, `CommandNames`; разметка `tee`, `cp`/`mv`/`install`, `sed -i` и разбор простых `s`-скриптов
- `shellx_test.go` — контракт поведения: heredoc, арифметика, `cp -t`, `sed`, `CommandNames`
- `fuzz_test.go` — `FuzzWords`: без паник, слова из байтов команды, `Kind` в диапазоне

Details: context/internal/shellx.md
