# bin/

**English** | [Русский](README.ru.md)

`paraplane` is a POSIX sh shim. Claude Code adds the plugin's `bin/` to PATH for Bash, so skills call `paraplane` by name.

- Picks the binary `../libexec/paraplane-<os>-<arch>[.exe]` by `uname` (linux, darwin, windows via Git Bash/MSYS; amd64, arm64). The binaries are built by `make dist` or the release.
- For `hook …` it always exits 0: a binary crash (a Go fatal error exits with 2, which Claude Code treats as a block) does not block the session; the session-start output is buffered and printed only on exit code 0.
- No binary for the platform: for `hook session-start` (unless the project disabled the plugin with `enabled: false` in `.paraplane/config.yaml`) — JSON with `systemMessage`, otherwise silence for hooks; for the CLI — an error and exit code 1.
