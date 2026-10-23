# Releasing paraplane

**English** | [Русский](releasing.ru.md)

## Channels

| Channel | Branch | Gets |
|---|---|---|
| stable | `release` | tags `vMAJOR.MINOR.PATCH` |
| pre-release | `release-next` | tags `vMAJOR.MINOR.PATCH-rc1` and the like |

Both branches only fast-forward: each release is a new commit on top of the previous one and is never force-pushed. A branch moves only to a higher version (`1.2.0-rc1 < 1.2.0`); an older tag pushed later leaves it as is. Testers install the pre-release channel with `--ref release-next` (Codex: `marketplace add ... --ref release-next`).

## Release

1. `main` is green (`ci` has to pass on the exact commit you tag: the `guard` job checks it).
2. Update the changelog if there is one, then tag and push:

   ```sh
   git tag -a v1.2.3 -m "v1.2.3" && git push origin v1.2.3
   ```

3. The `release` workflow runs `guard`, `ci`, `build` (Go 1.27.3, the version `go.mod` pins), `assemble` (stamps the version, builds the archive and `checksums.txt`, exports the checksums as a job output), `validate` (the only job that runs npx; it uploads nothing), `smoke` (every binary runs on its own OS and architecture), `publish` (re-verifies the downloaded files against the output, attests binaries, archive and checksums, moves the channel branch, creates the GitHub Release) and `verify-release` (the channel branch tree must equal the archive).
4. After a batch of tags check `gh run list --workflow release`: runs of different tags do not cancel each other, but a failed one needs a re-run.

A re-run of a published version pushes nothing; `verify-release` fails if the branch holds other bytes than the validated archive.

## Yanking a bad release

A release cannot be taken back: the branch only grows and tags are immutable. Ship a fix instead.

1. Fix (or revert) on `main`, wait for green CI.
2. Tag the next patch version (`v1.2.4`); the pipeline moves `release` forward.
3. Mark the bad release, but do not delete it (its checksums and attestations stay verifiable):

   ```sh
   gh release edit v1.2.3 --prerelease \
     --notes "WITHDRAWN: security issue GHSA-xxxx-xxxx-xxxx, upgrade to v1.2.4. Original notes below." 
   ```

   `--notes` replaces the notes: copy the original text below the warning.
4. Publish a GitHub Security Advisory (Security -> Advisories) with the affected range and the fixed version, and request a CVE if it applies. Users on the old version are told to update there (`/plugin update` in Claude Code; Codex: add the marketplace again with `--ref release`).

## Repository settings (manual, not in code)

These cannot be set by a workflow; set them once in the repository settings.

- **Rulesets** (Settings -> Rules -> Rulesets):
  - `main`: pull request required; required checks `docs`, `test`, `lint`, `old-git`, `vet-cross`, `plugin`; no force-push, no deletion.
  - `release` and `release-next`: no deletion, no non-fast-forward; updates only by the GitHub Actions bypass (or a deploy GitHub App); nobody pushes directly.
  - tags `v*`: creation only by maintainers (bypass list); no update, no deletion.
- **Releases**: enable *Immutable releases*, so the assets of a published release cannot be replaced.
- **Security**: enable Private vulnerability reporting (referenced by `SECURITY.md`), Dependabot alerts and security updates, secret scanning with push protection.
- **Actions**: the workflow token defaults to read-only; `GITHUB_TOKEN` write scopes are requested per job.

## Checks that gate a change

`ci` runs on every push and pull request (no path filters, so required checks always report): docs, tests on Linux, macOS and Windows with Go `1.27.x` and `stable` (shuffled, `-race`, 90 % coverage floor), `lint` (`go mod tidy -diff`, staticcheck, shellcheck, actionlint, zizmor), `fuzz` (30 s per fuzz target, discovered by `go test -list '^Fuzz'`), `old-git` (Ubuntu 22.04 and Debian 11 containers: git 2.34 and 2.30), `vet-cross`, `plugin`. `fix` (`go fix -diff`) reports without failing until the code is modernized; then it becomes blocking. `security` runs govulncheck (weekly too, also against the released binary) and dependency review; `codeql` scans the Go code.
