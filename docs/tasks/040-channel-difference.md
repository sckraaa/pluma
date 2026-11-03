# 040. Каналы: `updates.getChannelDifference` и `updateChannelTooLong`

- Статус: todo
- Зависит от: 039
- ADR: 0019, 0021, 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` §2.1–2.2, §2.5, задача T5; `clients.md` §1 «Catch up after being offline», §2; `plan.md` §2 C1, C4, C5, §3, задача «039» плана).

## Цель
Клиент, который был офлайн или закрыт, догоняет пропущенное в каналах во всех четырёх клиентах, без тугих циклов запросов. До этой задачи каналы не выкатываются в прод (C5).

## Что известно
- **Заглушки:** bff `app/bff/updates/internal/core/updates.getChannelDifference_handler.go:27` отвечает `ErrEnterpriseIsBlocked`; biz `app/service/biz/updates/internal/core/updates.getChannelDifferenceV2_handler.go` пишет «blocked» и отдаёт не финальную пустую разницу с `in.Pts` (`server.md` §1.1). `api-coverage.md:1016` — `blocked`.
- **`getDifference` отбрасывает канальные обновления** (`merge_updates_helper.go:148-153`, «Channels have their own pts», проверено) — это правильно; `updateChannelTooLong` он сейчас не шлёт. Каналы в `chats` разницы не разрешаются (ADR 0021 §7).
- **Протокол** (core.telegram.org/api/updates, /api/channel): лимит клиента 10–100; не финальный ответ — «запроси снова с новым `pts`»; финальный несёт `timeout`; `channelDifferenceTooLong` — когда `pts < latestPts - 100000`, с `dialog`, `messages`, `chats`, `users` (api.tl:770-772); при старте клиент вызывает только `getDifference`, каналы для догона помечаются `updateChannelTooLong` (api.tl:344).
- **Таймаут (C1):** документация — 1 с по умолчанию; iOS без флага ждёт 30 с, а явный 0 даёт непрерывный опрос (выведено из кода, не наблюдалось); tdesktop без флага или с 0 — 1 с; Android ставит таймер только при флаге (035). Решение (ADR 0034, п. 4): всегда явный `timeout` — 30 с участнику, 10 с не участнику; что отдаёт Telegram — не проверено. Строки iOS в закреплённом `6ad963e5`: `:3158-3163`, `:3220-3224` (035).
- **Клиенты:**
  - iOS `pollChannel`: `flags 0`, `channelMessagesFilterEmpty`, `pts: max(pollPts,1)`, `limit 100` (2 в DEBUG), `CHANNEL_PRIVATE`/`CHANNEL_INVALID` → nil, любая другая ошибка → `retryRequest` бесконечно с паузой 0,2–5 с (`AccountStateManagementUtils.swift:3433-3458` в закреплённом `6ad963e5`, запрос `:3448`; пауза `Network.swift:1240-1243`; C3 закрыт в 035). Обработки `final` в `pollChannel` не найдено: iOS берёт одну страницу за опрос (выведено из кода, `clients.md` §1).
  - iOS на tooLong берёт `dialog.pts`, `topMessage`, id прочтения и счётчики, без `dialog.pts` — `assertionFailure` (`:3626-3727` в `6ad963e5`).
  - Android (`aac1efcb`, 035): `getChannelDifference` — `MessagesController.java:16525-16794`, `limit` 100 (1 для нового канала, `pts` 1), `force` — кроме короткого опроса (`:16603-16606`); не финальный — сразу снова (`:16765-16766`); tooLong — `MessagesStorage.overwriteChannel` по `dialog` (`MessagesStorage.java:11278-11352`, `dialog = null` — падение, вывод из кода); таймер опроса только при флаге `timeout` (`:16762-16763`); ошибки — `checkChannelError` без повтора (`:16785-16806`).
  - tdesktop повторяет сразу (`api/api_updates.cpp:433-437`), tooLong — `ptsInit(dialog.pts)` (`:403-425`), при сбое — пауза 1, 2, 4 … 64 с (`:464-472`, `:576-590`); `timeout` без флага или 0 — 1 с, только для открытого чата (`:438-443`).
  - Web K повторяет рекурсивно с лимитом 1000 (`apiUpdatesManager.ts:435-487`), на tooLong сбрасывает состояние и перечитывает диалог (`:451-458`, `appMessagesManager.ts:10849-10865`), `timeout` не читает (035).
  - **Не финальный ответ без роста `pts`** — тугой цикл у Android, tdesktop и Web K (`MessagesController.java:16054`, `api_updates.cpp:433-437`, `apiUpdatesManager.ts:485-487`).
- **`updateChannelTooLong` в `getDifference`:** Android вызывает `getChannelDifference`, если `pts` нет или он больше локального, без локального `pts` — `getPeerDialogs` (`MessagesController.java:19206-19238`, 035); tdesktop — так же, незагруженный канал игнорирует (`api_updates.cpp:2480-2488`); Web K — только при известном состоянии канала и без сравнения `pts` (`apiUpdatesManager.ts:632-637`, `:658-665`, 035); iOS пропускает канал при локальном `pts` не меньше, остальные после разницы сбрасывает через `getPeerDialogs` (C4, `AccountStateManagementUtils.swift:952-957`, `:2055-2070`). Признак Telegram не проверен (U2); правило — ADR 0034, п. 6: каналы участника с `channels.date2 ≥ min(date запроса, дата курсора ключа) − MaxRunTime`, всегда с `pts`; курсор `user_seq_cursors` дополняется датой.
- Ошибки: `CHANNEL_PRIVATE` (406, core.telegram.org/method/updates.getChannelDifference) — не участник закрытого канала, `CHANNEL_INVALID` (400) — неверный `access_hash`; других ошибок метод не отдаёт (C5); зритель видит только `id >= available_min_id` (`server.md` §2.5). Ответы, `limit` до 1000, срезы и tooLong — ADR 0034, п. 4 (035).

## Область
- bff и biz `updates.getChannelDifference`: свёртка `channel_pts_updates` в `(pts, channels.pts]` по образцу ADR 0019 (новые неудалённые сообщения в последней правке по возрастанию id, правки и удаления — в `other_updates`); срез при превышении `limit` без `final`; `channelDifferenceEmpty` с явным `timeout > 0`; `channelDifferenceTooLong` с полным `dialog` (`pts`, id прочтения, счётчики), сообщениями, `chats`, `users`, когда `pts` вне окна журнала или по `force`; ошибки `CHANNEL_PRIVATE`, `CHANNEL_INVALID`; фильтр `available_min_id`.
- Обрезка журнала по окну из ADR «Ящик канала» (100000 pts, ADR 0034, п. 4; срок сверх окна — вопрос владельцу 2 в 035).
- `updateChannelTooLong` в `getDifference` по правилу ADR 0034, п. 6; каналы разрешаются в `chats` разницы (источник резолвера — 037).
- Не трогаем: `channelMessagesFilter` по диапазонам сверх того, что нужно клиентам (уточнить в 035).

## Критерии приёмки
- Модульные тесты свёртки (по образцу `seq_fold_test.go`): правка и удаление после вставки, срез на границе, пустой ответ, tooLong.
- Интеграционные тесты на MySQL через `pkg/testinfra`: границы среза при `limit` 2 (iOS DEBUG) и 100; tooLong после обрезки журнала; `timeout` всегда > 0.
- e2e через `pkg/mtclient`: клиент офлайн → 150 постов → переподключение → `getDifference` даёт `updateChannelTooLong` → цикл `getChannelDifference` доходит до `final`; тест падает на текущей заглушке.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: после закрытия и повторного открытия приложения пропущенные посты появляются в iOS, Android, Desktop (CachyOS) и Web K; в сетевых логах нет тугих циклов `getChannelDifference` (C1, C5).

## Результат
