# 040. Каналы: `updates.getChannelDifference` и `updateChannelTooLong`

- Статус: todo
- Зависит от: 039
- ADR: 0019, 0021, «Ящик канала» (из 035). Исследование: `docs/tasks/035-research/` (`server.md` §2.1–2.2, §2.5, задача T5; `clients.md` §1 «Catch up after being offline», §2; `plan.md` §2 C1, C4, C5, §3, задача «039» плана).

## Цель
Клиент, который был офлайн или закрыт, догоняет пропущенное в каналах во всех четырёх клиентах, без тугих циклов запросов. До этой задачи каналы не выкатываются в прод (C5).

## Что известно
- **Заглушки:** bff `app/bff/updates/internal/core/updates.getChannelDifference_handler.go:27` отвечает `ErrEnterpriseIsBlocked`; biz `app/service/biz/updates/internal/core/updates.getChannelDifferenceV2_handler.go` пишет «blocked» и отдаёт не финальную пустую разницу с `in.Pts` (`server.md` §1.1). `api-coverage.md:1016` — `blocked`.
- **`getDifference` отбрасывает канальные обновления** (`merge_updates_helper.go:148-153`, «Channels have their own pts», проверено) — это правильно; `updateChannelTooLong` он сейчас не шлёт. Каналы в `chats` разницы не разрешаются (ADR 0021 §7).
- **Протокол** (core.telegram.org/api/updates, /api/channel): лимит клиента 10–100; не финальный ответ — «запроси снова с новым `pts`»; финальный несёт `timeout`; `channelDifferenceTooLong` — когда `pts < latestPts - 100000`, с `dialog`, `messages`, `chats`, `users` (api.tl:770-772); при старте клиент вызывает только `getDifference`, каналы для догона помечаются `updateChannelTooLong` (api.tl:344).
- **Таймаут (C1):** документация — 1 с по умолчанию; iOS без флага ждёт 30 с (`AccountStateManagementUtils.swift:3143-3149`), а явный 0 даёт непрерывный опрос (`:3159-3163`, `:3220-3224`; выведено из кода, не наблюдалось); tdesktop и Android — 1 с. Решение: всегда явный `timeout > 0`, значение — из 035 (что отдаёт Telegram — UNVERIFIED до 035).
- **Клиенты:**
  - iOS `pollChannel`: `flags 0`, `channelMessagesFilterEmpty`, `pts: max(pollPts,1)`, `limit 100` (2 в DEBUG), `CHANNEL_PRIVATE`/`CHANNEL_INVALID` → nil, любая другая ошибка → `retryRequest` бесконечно с паузой 0,2–5 с (`AccountStateManagementUtils.swift:3413-3440` в `756315d7`, проверено; `:3433-3458` в `6ad963e5`; C3 — ни одно дерево не сверено с закреплением). Обработки `final` в `pollChannel` не найдено: iOS берёт одну страницу за опрос (выведено из кода, `clients.md` §1).
  - iOS на tooLong берёт `dialog.pts`, `topMessage`, id прочтения и счётчики, без `dialog.pts` — `assertionFailure` (`:3606-3640` в `756315d7`; `:3626-3700` в `6ad963e5`).
  - Android повторяет сразу при не финальном ответе (`MessagesController.java:15895-16056`), на tooLong — `overwriteChannel` (`:16027-16045`), `force` — кроме коротких опросов (`:15895`) (UNVERIFIED, U10).
  - tdesktop повторяет сразу (`api/api_updates.cpp:389-437`), tooLong — `ptsInit(dialog.pts)` (`:403-424`), при сбое — пауза 1, 2, 4 … 64 с (`:464-472`, `:576-590`).
  - Web K повторяет рекурсивно с лимитом 1000 (`apiUpdatesManager.ts:435-487`), на tooLong сбрасывает состояние и перечитывает диалог (`:451-457`, `appMessagesManager.ts:10849-10863`) (UNVERIFIED, U10).
  - **Не финальный ответ без роста `pts`** — тугой цикл у Android, tdesktop и Web K (`MessagesController.java:16054`, `api_updates.cpp:433-437`, `apiUpdatesManager.ts:485-487`).
- **`updateChannelTooLong` в `getDifference`:** Android вызывает `getChannelDifference`, если `pts` нет или он больше локального, без локального `pts` — `getPeerDialogs` (`MessagesController.java:18462-18494`, UNVERIFIED, U10); tdesktop — так же, незагруженный канал игнорирует (`api_updates.cpp:2480-2488`); Web K — только при известном состоянии канала (`apiUpdatesManager.ts:632-636`, `:658-664`, UNVERIFIED, U10); iOS — C4, см. 038 (UNVERIFIED до 035). Признак, по которому Telegram его шлёт, — UNVERIFIED (U2); варианты исследования: отметка в ящике пользователя о недоставленном событии или «каждый канал, чей `pts` вырос с даты базы разницы» (`server.md` §2.5).
- Ошибки: `CHANNEL_PRIVATE` — не участник закрытого канала, `CHANNEL_INVALID` — неверный `access_hash`; зритель видит только `id >= available_min_id` (`server.md` §2.5).

## Область
- bff и biz `updates.getChannelDifference`: свёртка `channel_pts_updates` в `(pts, channels.pts]` по образцу ADR 0019 (новые неудалённые сообщения в последней правке по возрастанию id, правки и удаления — в `other_updates`); срез при превышении `limit` без `final`; `channelDifferenceEmpty` с явным `timeout > 0`; `channelDifferenceTooLong` с полным `dialog` (`pts`, id прочтения, счётчики), сообщениями, `chats`, `users`, когда `pts` вне окна журнала или по `force`; ошибки `CHANNEL_PRIVATE`, `CHANNEL_INVALID`; фильтр `available_min_id`.
- Обрезка журнала по окну из ADR «Ящик канала» (100000 pts и, возможно, срок — решение владельца в 035).
- `updateChannelTooLong` в `getDifference` по правилу ADR из 035; каналы разрешаются в `chats` разницы (источник резолвера — 037).
- Не трогаем: `channelMessagesFilter` по диапазонам сверх того, что нужно клиентам (уточнить в 035).

## Критерии приёмки
- Модульные тесты свёртки (по образцу `seq_fold_test.go`): правка и удаление после вставки, срез на границе, пустой ответ, tooLong.
- Интеграционные тесты на MySQL через `pkg/testinfra`: границы среза при `limit` 2 (iOS DEBUG) и 100; tooLong после обрезки журнала; `timeout` всегда > 0.
- e2e через `pkg/mtclient`: клиент офлайн → 150 постов → переподключение → `getDifference` даёт `updateChannelTooLong` → цикл `getChannelDifference` доходит до `final`; тест падает на текущей заглушке.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: после закрытия и повторного открытия приложения пропущенные посты появляются в iOS, Android, Desktop (CachyOS) и Web K; в сетевых логах нет тугих циклов `getChannelDifference` (C1, C5).

## Результат
