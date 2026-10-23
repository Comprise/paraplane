# Security policy

**English** | [Русский](SECURITY.ru.md)

## Supported versions

Only the latest minor release (the one on the `release` branch) receives security fixes. Fixes ship as a new patch version; a published release is never rewritten.

## Reporting a vulnerability

Report privately through GitHub: **Security -> Report a vulnerability** (private vulnerability reporting) on this repository. Do not open a public issue or pull request for a vulnerability.

Include the paraplane version (`paraplane version`), the host (Claude Code or Codex CLI), the operating system and a minimal reproduction. A confirmed vulnerability is fixed in a patch release and announced in a GitHub Security Advisory; the affected release is marked in its notes (see `docs/releasing.md`).

## Scope

paraplane runs as a hook with the rights of the user and reads plans and git repositories that may come from untrusted sources. Reports about a way to execute code, write outside the repository's `.git/paraplane` state, or influence the host's decisions through crafted plans, configs or git data are in scope.

## Verifying a release

Binaries, the archive and `checksums.txt` carry build provenance attestations:

```sh
gh attestation verify libexec/paraplane-linux-amd64 --repo Comprise/paraplane
```
