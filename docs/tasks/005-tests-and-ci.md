# 005. Зелёная база тестов и CI

- Статус: todo
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
—
