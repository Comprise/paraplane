# bin/

[English](README.md) | **Русский**

`paraplane` — POSIX sh-шим. Claude Code добавляет `bin/` плагина в PATH для Bash, поэтому skills вызывают `paraplane` по имени.

- Выбирает бинарь `../libexec/paraplane-<os>-<arch>[.exe]` по `uname` (linux, darwin, windows через Git Bash/MSYS; amd64, arm64). Бинари собирает `make dist` или релиз.
- Для `hook …` всегда выходит с 0: падение бинаря (Go fatal error выходит с 2, Claude Code считает это блокировкой) не блокирует сессию; вывод session-start буферизуется и печатается только при коде 0.
- Нет бинаря для платформы: для `hook session-start` (если проект не отключил плагин `enabled: false` в `.paraplane/config.yaml`) — JSON с `systemMessage`, иначе для хуков тишина; для CLI — ошибка и код 1.
