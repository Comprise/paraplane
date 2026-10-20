# bin/

**English** | [Русский](README.ru.md)

`paraplane` is a POSIX sh shim. Claude Code adds the plugin's `bin/` to PATH for Bash, so skills call `paraplane` by name.

- Picks the binary `../libexec/paraplane-<os>-<arch>[.exe]` by `uname` (linux, darwin, windows via Git Bash/MSYS; amd64, arm64). The binaries are built by `make dist` or the release.
- For `hook …` it always exits 0: a binary crash (a Go fatal error exits with 2, which Claude Code treats as a block) does not block the session. The output of EVERY hook is buffered and printed only on exit code 0; trailing newlines are normalised, so the output is not byte-exact.
- The `enabled: false` check looks at the project root: it walks up from `$CLAUDE_PROJECT_DIR` (else `$PWD`) to the nearest directory that has `.git` or `.paraplane/config.yaml`, without climbing into `$HOME` from below it; if there is none, the start directory is used.
- The file must keep LF line endings (`.gitattributes` enforces it), otherwise the shim does not run on Windows. The release smoke-tests it: `sh bin/paraplane version` must print the tag version without the `v` prefix.
- No binary for the platform: for `hook session-start` (unless the project disabled the plugin with `enabled: false` in `.paraplane/config.yaml`) — JSON with `systemMessage`, otherwise silence for hooks; for the CLI — `paraplane: binary … was not found` on stderr and exit code 1. A binary that exists but is not executable is handled the same way, with `… is not executable`.
