# 039. Каналы: отправка, история, `channels.getMessages`

- Статус: done (сервер; проверка в клиентах — после выката с 040, доставка второму участнику — после 041)
- Зависит от: 038
- ADR: 0011, 0012, 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` §1.1, §2.3–2.5, задача T4; `clients.md` §1 «Read history», «Send and receive in real time», §2; `plan.md` §3, задача «038» плана).

## Цель
Сообщения в канал и супергруппу отправляются и приходят другим участникам сразу, с `pts` канала подряд; история открывается без циклов «загрузки»; повтор отправки не создаёт дубля.

## Что известно
- **Отправка заблокирована:** `msg.sendMessageV2` для `PEER_CHANNEL` отвечает `ErrEnterpriseIsBlocked` после лога «License key» (`app/messenger/msg/msg/internal/core/msg.sendMessageV2_handler.go:51-53`, проверено, `plan.md` §1 п. 3). Рассылка базовой группы — копия в ящик каждого участника (`msg.sendMessageV2_handler.go:275` `sendChatMessages`, consumer `app/messenger/msg/inbox/internal/server/mq/mq.go:48`); для каналов так не делаем — одна копия в `channel_messages` (ADR 0034, п. 1).
- **Ответ на отправку:** `Updates` с `updateMessageID` и `updateNewChannelMessage(pts, pts_count=1)` (`clients.md` §1).
- **Правило разрыва у всех клиентов:** применить, если `local_pts + pts_count == pts`; пропустить, если `pts <= local`; иначе ждать и запросить разницу. Android ждёт 1,5 с (`MessagesController.java:18171-18210`, `:16256-16316`, 035), Web K — `SYNC_DELAY = 6` мс (`apiUpdatesManager.ts:54`, `:696-724`, 035), iOS — `AccountStateManagementUtils.swift:974-1036`. Общий или чужой `pts` в канальном обновлении превращает каждое обновление в разрыв (`clients.md` §2 п. 5).
- **Канал и авторы должны быть в `updates.chats`/`updates.users`:** Android на неизвестный канал без `pts` вызывает `getPeerDialogs` (`:16380-16441`, 035); Web K на отсутствующего автора или пир запрашивает разницу (`apiUpdatesManager.ts:667-691`, 035).
- **История:** `messages.getHistory` для канала — закомментированный код `ChannelClient` (`app/bff/messages/internal/core/messages.getHistory_handler.go:60-64`). Ответ — `messages.channelMessages` с ненулевым `pts`; его читают все четыре клиента: Android засевает им `pts` канала и затем вызывает `getChannelDifference` (`MessagesController.java:11892-11906`, 035), tdesktop (`data/data_session.cpp:2954-2957`, `apiwrap.cpp:3470-3471`), Web K (`appMessagesManager.ts:12531-12533`).
- **`channels.getMessages`** заполняет ответы и пропущенные сообщения (iOS `AccountStateManagementUtils.swift:2870`, `:2906`; Android — 25 мест вызова); до 200 id; `messages.getHistory` не закрывает разрывы канала (core.telegram.org/api/updates).
- **Повтор `random_id`:** сейчас единственная защита — Redis `DoIdempotent` (аудит 01 #10); после истечения TTL повтор создаёт дубль. Решение — `UNIQUE(channel_id, sender_user_id, random_id)` из 036. `RANDOM_ID_DUPLICATE` — 500 (core.telegram.org/method/messages.sendMessage, 035); полный повтор — исходный ответ, частичный — эта ошибка (ADR 0034, п. 11; ADR 0012, п. 6).
- **Broadcast:** публикуют только админы с `post_messages` (`server.md` §2.6). Подписчику — `CHAT_WRITE_FORBIDDEN` 403 (ADR 0034, п. 8; какую шлёт Telegram, не проверено). Полная проверка прав — 042; здесь достаточно роли из `channel_participants` (создатель или админ с `post_messages`), а в 042 проверка переезжает в `channelperm.Can`.
- **Push** (ADR 0034, п. 5): одна строка outbox на событие в новый топик `Channel-T`, ключ — id канала; консьюмер в sync (группа `sync-channel`, best-effort) строит вид каждого получателя (`out`, `mentioned`, `channel`, пользователи) и пушит онлайн-сессиям, кроме ключа отправителя. Уведомления на телефон (ADR 0017) для каналов — тоже от `Channel-T`.

## Область
- `PEER_CHANNEL` в `msg.sendMessageV2` (`:51`), `messages.sendMedia`, `messages.sendMultiMedia`: запись в `channel_messages` и `channel_pts_updates` через `channelbox.Run`, `updateMessageID` и `updateNewChannelMessage` отправителю, `random_id` проверяется в базе.
- Рассылка `updateNewChannelMessage` онлайн-участникам через sync, ключ — id канала; канал и пользователи — в `chats`/`users`.
- Диалог отправителя (`top_message`) через `userbox.Run`; непрочитанное у остальных считается при чтении, у получателей ничего не пишется (ADR 0034, п. 1); упоминания — строки `channel_message_mentions` в транзакции канала.
- `messages.getHistory` → `messages.channelMessages` с `pts`; `channels.getMessages`; `messages.search` в канале.
- Не трогаем: правку, удаление, прочтение (043), пересылку (`messages.forwardMessages` остаётся заблокированным для каналов, `messages.forwardMessages_handler.go:76,169`).

## Критерии приёмки
- Модульные тесты: сборка ответа отправки, проверка роли для broadcast.
- Интеграционные тесты на MySQL через `pkg/testinfra`: два отправителя параллельно дают упорядоченные id и `pts` без пропусков; повтор `random_id` после истечения TTL Redis не создаёт дубля (тест падает на текущем коде).
- e2e через `pkg/mtclient`: двое участников, один отправляет, второй получает `updateNewChannelMessage` с последовательными `pts`; `getHistory` отдаёт `channelMessages` с `pts` > 0; подписчик broadcast не может опубликовать.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: в iOS, Android, Desktop (CachyOS) и Web K сообщения в супергруппе и посты в канале приходят сразу, история открывается без бесконечной «загрузки». Повторы `getChannelDifference` в iOS до 040 ожидаемы (C5).

## Результат
Сервер готов, ветка `task/038-channel-create` (коммит `b85ead27`), не выкачено (каналы показываются людям после 040).

- **Отправка.** `messages.sendMessage` в `inputPeerChannel` → msg `sendChannelMessages`: в транзакции ящика отправителя канал блокируется после него (`channelbox.Open`), каждое сообщение получает следующий id и `pts` канала, одну копию в `channel_messages` и одну строку журнала. В канале (broadcast) пишет только создатель или админ с `post_messages` (иначе `CHAT_WRITE_FORBIDDEN`), сообщение — пост без автора; в супергруппе — от отправителя, кроме запрета `send_messages`. Упоминания (сущности и автор ответа) — в `channel_message_mentions`. Повтор `random_id` отдаёт первый ответ по уникальному ключу `out_random_id`, частичный повтор — `RANDOM_ID_DUPLICATE`.
- **Рассылка.** Событие `sync.pushChannelUpdates` в `Sync-T` с ключом по id канала (одна строка outbox на транзакцию). Консьюмер sync строит вид для каждого участника (`out`, `mentioned`, `media_unread`, канал, пользователи) и шлёт онлайн-сессиям; другим устройствам отправителя — через `SyncUpdatesNotMe`. Отступление от ADR 0034, п. 5: не отдельный топик `Channel-T`, а `Sync-T` с ключом по каналу (записано в ADR, «Уточнения при реализации»).
- **Чтения.** `messages.getHistory` канала → `messages.channelMessages` с `pts` и `count`, фильтр `available_min_id`; `channels.getMessages` по id. Не участнику закрытого канала — `CHANNEL_PRIVATE`.
- **Проверка.** e2e `TestChannelSend` (супергруппа и канал: id и `pts` по порядку, повтор `random_id`, пуш второму устройству, история, `getMessages`), модульные и интеграционные тесты msg и biz channel.
- **Не сделано в этой задаче:** доставка второму участнику и запрет публикации подписчику проверяются e2e только после вступления (041). Проверка в клиентах — после выката.
- **Найдено в 040:** `Member()` у отсутствующей строки участника отвечал «участник» (нулевое состояние — `StateIn`), так что посторонний мог читать историю закрытого канала. Исправлено в 040 с тестом `TestMember`; на прод это не попадало (ветка не выкачена).

