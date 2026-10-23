# 005. Зелёная база тестов и CI

- Статус: done (CI зелёный на GitHub в PR sckraaa/pluma#1, 2026-10-22)
- Зависит от: —
- ADR: 0002

## Цель
`go build`, `go vet`, `go test` проходят; CI проверяет каждый push; есть интеграционный стенд для тестов на реальных MySQL/Redis/Kafka.

## Область
По `docs/audit/03-architecture-and-ops.md`:
- Починить 5 несобирающихся тестовых пакетов и 3 падающих; исправить ~15 реальных ошибок printf из `go vet` (сгенерированный unreachable code — исключить из vet).
- `make test`, `make lint`, `make vet`; golangci-lint с умеренным набором.
- GitHub Actions: build, vet, unit-тесты; интеграционные тесты на testcontainers (MySQL, Redis, Kafka).
- govulncheck в CI; обновить Go и уязвимые зависимости (в т.ч. `x/image`).
- Шаг в CI: `go run ./tools/apicoverage -summary`.

## Критерии приёмки
- CI зелёный на master.
- Хотя бы один интеграционный тест полного пути: handshake → вход → отправка сообщения → получение апдейта вторым клиентом.

## Результат
Всё готово: локально и на GitHub (PR sckraaa/pluma#1, все 6 проверок зелёные). В `master` — после слияния PR. Как запускать и почему так устроено — `docs/ops/testing.md`.

- **База.** Тулчейн go1.27.3, обновлены уязвимые зависимости. Починены несобиравшиеся и падавшие тесты и реальные находки `go vet`; выключен только `unreachable`, из-за сгенерированных кодеков. Оба модуля (корень и `third_party/teamgram-proto`) проверяются всеми целями.
- **`make ci` = CI.** build, vet, test, test-race, golangci-lint (по коду после апстрима), govulncheck, apicoverage-check, actionlint и shellcheck. Все инструменты закреплены в `Makefile`; shellcheck — релизный бинарник с проверкой SHA-256, actionlint проверяет `run:`-блоки тем же shellcheck. Actions закреплены по SHA. У задач integration и e2e разные ключи кэша Go.
- **Интеграционные тесты** (`make test-integration`, `pkg/testinfra`, testcontainers): MySQL с продовой схемой, Redis, Kafka. Образы берутся из `docker-compose-env.yaml`, образ Ryuk закреплён по digest. Тесты не зависят от порядка и перезапуска: `-count=3 -shuffle=on` зелёный, тесты миграций начинаются с `Reset` базы.
- **e2e полного пути** (`make e2e`, клиент `pkg/mtclient`): handshake → вход по allowlist с кодом от администратора → `messages.sendMessage` → пуш второму клиенту и `updates.getDifference` → привязка temp-ключа. Стек считается готовым, когда консьюмеры Kafka получили все партиции. Тест падает, если сервер прислал какое-то сообщение повторно.
- **Обновления.** Dependabot раз в неделю: actions, Go-модули обоих модулей, `Dockerfile`, compose-файлы; major-версии образов — вручную. Плюс еженедельный прогон CI по расписанию: govulncheck узнаёт о новых уязвимостях без push.
- **Найдено тестами и исправлено.** session отправлял `new_session_created` дважды с одним msg_id: сначала сразу, потом из очереди. mtclient теперь отбрасывает повторы по окну из 400 msg_id и считает их, `WaitUpdate` будит всех ожидающих.
- Публичный репозиторий помечен как форк teamgram: `README.md`, `README-zh.md`, `NOTICE`.
