# bin/

[English](README.md) | **Русский**

`paraplane` — POSIX sh-шим. Claude Code добавляет `bin/` плагина в PATH для Bash, поэтому skills вызывают `paraplane` по имени.

- Выбирает бинарь `../libexec/paraplane-<os>-<arch>[.exe]` по `uname` (linux, darwin, windows через Git Bash/MSYS; amd64, arm64). Бинари собирает `make dist` или релиз.
- Для `hook …` всегда выходит с 0: падение бинаря (Go fatal error выходит с 2, Claude Code считает это блокировкой) не блокирует сессию. Вывод КАЖДОГО хука буферизуется и печатается только при коде 0; завершающие переводы строк нормализуются, вывод не побайтовый.
- Проверка `enabled: false` смотрит на корень проекта: от `$CLAUDE_PROJECT_DIR` (иначе `$PWD`) поднимается к ближайшему каталогу с `.git` или `.paraplane/config.yaml`, не заходя снизу в `$HOME`; если такого нет, берётся исходный каталог.
- Файл должен оставаться с переводами строк LF (это обеспечивает `.gitattributes`), иначе шим не запустится на Windows. Релиз проверяет его: `sh bin/paraplane version` должен напечатать версию тега без префикса `v`.
- Нет бинаря для платформы: для `hook session-start` (если проект не отключил плагин `enabled: false` в `.paraplane/config.yaml`) — JSON с `systemMessage`, иначе для хуков тишина; для CLI — ошибка и код 1.
