# Выпуск paraplane

[English](releasing.md) | **Русский**

## Каналы

| Канал | Ветка | Что получает |
|---|---|---|
| стабильный | `release` | теги `vMAJOR.MINOR.PATCH` |
| пре-релиз | `release-next` | теги `vMAJOR.MINOR.PATCH-rc1` и подобные |

Обе ветки только перематываются вперёд: каждый выпуск — новый коммит поверх предыдущего, без force-push. Ветка переходит только на более высокую версию (`1.2.0-rc1 < 1.2.0`); более старый тег, отправленный позже, ветку не трогает. Тестировщики ставят пре-релиз с `--ref release-next` (Codex: `marketplace add ... --ref release-next`).

## Выпуск

1. `main` зелёный (`ci` должен пройти на том самом коммите, который тегируете: это проверяет job `guard`).
2. Обновите changelog, если он есть, затем тег и push:

   ```sh
   git tag -s v1.2.3 -m "v1.2.3" && git push origin v1.2.3
   ```

3. Workflow `release` выполняет `guard`, `ci`, `build` (Go 1.27.3, версия из `go.mod`), `assemble` (ставит версию, собирает архив и `checksums.txt`, отдаёт суммы как output job'а), `validate` (единственный job с npx, ничего не выгружает), `smoke` (каждый бинарник запускается на своей ОС и архитектуре), `publish` (сверяет скачанные файлы с output, подписывает attestation бинарники, архив и checksums, двигает ветку канала, создаёт GitHub Release) и `verify-release` (дерево ветки канала должно совпасть с архивом).
4. После пачки тегов проверьте `gh run list --workflow release`: запуски разных тегов не отменяют друг друга, но упавший нужно перезапустить.

Повторный запуск уже опубликованной версии ничего не пушит; `verify-release` падает, если в ветке не те байты, что в проверенном архиве.

## Отзыв плохого выпуска

Выпуск нельзя забрать назад: ветка только растёт, теги неизменяемы. Выпускается исправление.

1. Исправление (или revert) на `main`, дождитесь зелёного CI.
2. Тег следующей патч-версии (`v1.2.4`); конвейер двигает `release` вперёд.
3. Пометьте плохой Release, но не удаляйте его (суммы и attestation остаются проверяемыми):

   ```sh
   gh release edit v1.2.3 --prerelease \
     --notes "WITHDRAWN: security issue GHSA-xxxx-xxxx-xxxx, upgrade to v1.2.4. Original notes below."
   ```

   `--notes` заменяет текст: исходные заметки перенесите под предупреждение.
4. Опубликуйте GitHub Security Advisory (Security -> Advisories) с затронутым диапазоном и исправленной версией, при необходимости запросите CVE. Пользователей старой версии просят обновиться (`/plugin update` в Claude Code; Codex: заново добавить marketplace с `--ref release`).

## Настройки репозитория (вручную, не в коде)

Workflow их выставить не может; задайте один раз в настройках репозитория.

- **Rulesets** (Settings -> Rules -> Rulesets):
  - `main`: обязателен pull request; обязательные проверки `docs`, `test`, `lint`, `old-git`, `vet-cross`, `plugin`; без force-push и удаления.
  - `release` и `release-next`: запрет удаления и non-fast-forward; обновление только через bypass для GitHub Actions (или GitHub App деплоя); прямой push запрещён всем.
  - теги `v*`: создание только мейнтейнерам (bypass list); запрет update и удаления.
- **Releases**: включите *Immutable releases*, чтобы ассеты опубликованного Release нельзя было подменить.
- **Security**: включите Private vulnerability reporting (на него ссылается `SECURITY.ru.md`), Dependabot alerts и security updates, secret scanning с push protection.
- **Actions**: токен workflow по умолчанию только для чтения; права записи `GITHUB_TOKEN` запрашиваются в конкретных job'ах.

## Проверки, которые охраняют изменение

`ci` запускается на каждый push и pull request (без фильтров путей, обязательные проверки всегда отчитываются): docs, тесты на Linux, macOS и Windows с Go `1.27.x` и `stable` (перемешивание, `-race`, порог покрытия 90 %), `lint` (`go mod tidy -diff`, staticcheck, shellcheck, actionlint, zizmor), `fuzz` (30 с на каждую fuzz-цель, цели находит `go test -list '^Fuzz'`), `old-git` (контейнеры Ubuntu 22.04 и Debian 11: git 2.34 и 2.30), `vet-cross`, `plugin`. `fix` (`go fix -diff`) только сообщает, пока код не модернизирован; потом станет блокирующим. `security` запускает govulncheck (еженедельно тоже, в том числе по выпущенному бинарнику) и dependency review; `codeql` сканирует Go-код.
