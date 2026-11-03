# 045. Супергруппы: медленный режим

- Статус: todo
- Зависит от: 042
- ADR: «Ящик канала» (из 035). Исследование: `docs/tasks/035-research/` (`server.md` §2.6, задача T10; `clients.md` §4 п. 6; `plan.md` §2 U3, U4, §3, задача «044» плана).

## Цель
Админ включает медленный режим, и участник видит отсчёт в поле ввода и не может отправить раньше времени во всех четырёх клиентах.

## Что известно
- `slowmode_seconds` хранится на канале, время последней отправки участника — в `channel_participants`; `channelFull` отдаёт `slowmode_seconds` и `slowmode_next_send_date` (api.tl:110) (`server.md` §2.6).
- Клиенты: iOS `SlowMode.swift` (`channels.toggleSlowMode`); Android разбирает `SLOWMODE_WAIT_X` как ошибку 420 (`jni/tgnet/ConnectionsManager.cpp:1399-1420`); Web K её сам не повторяет (`apiManager.ts:744`) (`clients.md` §4 п. 6).
- Имя ошибки `SLOWMODE_WAIT_X` и код 420 — UNVERIFIED до сверки с https://core.telegram.org/method/messages.sendMessage (U3), закрывается в 035.
- Освобождены ли админы от медленного режима — UNVERIFIED (U4), закрывается в 035.
- Зависит от 042: проверка встраивается в `channelperm`/путь отправки, исключения для админов — по правам.

## Область
- `channels.toggleSlowMode` (служебное `updateChannel`, проверка прав).
- Проверка в пути отправки (`sendMessage`, `sendMedia`, `sendMultiMedia`, пересылка, когда она появится), запись времени отправки в той же транзакции.
- `slowmode_seconds` и `slowmode_next_send_date` в `channelFull` для зрителя.
- Не трогаем: прочие права (042).

## Критерии приёмки
- Модульные тесты: арифметика времени (граница, смена интервала, выключение), исключения по правам.
- Интеграционный тест на MySQL через `pkg/testinfra`: две параллельные отправки одного участника в окне — проходит одна.
- e2e через `pkg/mtclient`: вторая отправка раньше N секунд отклоняется ошибкой с именем из 035; админ — по результату U4; тест падает на текущем коде.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: отсчёт в поле ввода и отказ при ранней отправке в iOS, Android, Desktop (CachyOS) и Web K.

## Результат
