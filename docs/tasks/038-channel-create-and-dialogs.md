# 038. Каналы: создание, `getChannels`, `getFullChannel`, диалоги

- Статус: in progress (сервер сделан 2026-11-04, ветка `task/038-channel-create`; в прод — вместе с 040)
- Зависит от: 037
- ADR: 0011, 0021, 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` §2.2–2.5, задача T3; `clients.md` §1 «Create», «Open a channel», §2, §4 п. 1; `plan.md` §3, задача «037» плана).

## Цель
Канал и супергруппу можно создать и открыть во всех четырёх клиентах; канал появляется в списке чатов с ненулевым `dialog.pts`; Android показывает ссылку-приглашение без падения.

## Что известно
- **Ответ `channels.createChannel`** (`Updates`):
  - `channel` в `chats`: Web K берёт `updates.chats[0].id` и падает без чатов (`appManagers/appChatsManager.ts:588-593`); Android без чатов шлёт `chatDidFailCreate`, канал берёт из `chats[0]` (`MessagesController.java:15036-15069` в `aac1efcb`, 035);
  - сообщение нового канала: iOS ищет в `updates.messages` первое сообщение с пиром нового канала, и этот пир должен оказаться в локальном хранилище за 5 с, иначе `.generic` (`TelegramEngine/Peers/ChannelCreation.swift:50-90`, `State/UpdatesApiUtils.swift:665-690`); на практике это `updateNewChannelMessage(messageActionChannelCreate)`, id 1, `pts` 2 при базе 1 (ADR 0034, п. 1; `pts` Telegram не проверен, 035);
  - ошибки, которые разбирает iOS: 406 (показывает текст сервера), `CHANNELS_TOO_MUCH`, `CHANNELS_ADMIN_LOCATED_TOO_MUCH`, `USER_RESTRICTED`.
  - tdesktop (035, U7): канал должен стоять первым в `chats` ответа, иначе окно закрывается; затем `getFullChannel` и `exportChatInvite`, если ссылки нет; ошибки `NO_CHAT_TITLE`, `USER_RESTRICTED`, `CHANNELS_TOO_MUCH` (`boxes/add_contact_box.cpp:822-917`).
- **Экран создания Android** (`ui/ChannelCreateActivity.java`, `clients.md` §1, §2):
  - `channels.checkUsername(inputChannelEmpty, "1")` — проверка лимита публичных ссылок (`:170-173`); блокирует только `CHANNELS_ADMIN_PUBLIC_TOO_MUCH`;
  - затем `getAdminedPublicChannels`, `updateUsername` и `messages.getExportedChatInvites(limit=1)` (`:935-944`);
  - **падение:** пустой список из `getExportedChatInvites(limit=1)` → `invites.invites.get(0)` (`:940-943`, проверено в `plan.md` §1 п. 11). Создатель всегда должен получать постоянную ссылку.
- **Открытие канала:**
  - Android: `channels.getFullChannel` и загрузка админов (`MessagesController.java:7619-7677`, 035);
  - iOS: `getFullChannel` вместе с `channels.getParticipant(inputPeerSelf)` (`UpdateCachedPeerData.swift:657-680`, 035); ошибки `getFullChannel` становятся nil; поля — `:697-935` (`participants_count`, `admins_count`, `can_view_participants`, `migrated_from_*`, медленный режим);
  - `pts` канала клиенты берут из `dialog.pts` или `channelFull.pts`: tdesktop не инициализирует `pts` и не запрашивает разницу при нуле (`api/api_updates.cpp:726`), Web K `addChannelState` бросает исключение на `pts` 0 (`apiUpdatesManager.ts:531-534`, 035), Android на неизвестный канал без `pts` зовёт `getPeerDialogs` (`MessagesController.java:18137-18147`, 035).
- **Диалоги:**
  - `messages.getPeerDialogs` пропускает каналы с логом «blocked» (`app/bff/dialogs/internal/core/messages.getPeerDialogs_handler.go:93-95`; путь исправлен в `plan.md` §1 п. 4);
  - biz dialog уже перечисляет диалоги `PEER_CHANNEL` (`app/service/biz/dialog/internal/core/dialog.getMyDialogsData_handler.go:102-130`, ключ кэша `GetChannelsCacheKey`);
  - `dialog#fc89f7f3` (api.tl:216) несёт `pts`; в таблице `dialogs` столбца `pts` нет — `dialog.pts` берётся из `channels.pts`, не из копии (`server.md`, §2.3);
  - C4 (закрыто в 035): после `getDifference` iOS сбрасывает каналы с `updateChannelTooLong.pts` выше локального через `getPeerDialogs` (`AccountStateManagementUtils.swift:952-957`, `:2055-2070`, `resetChannels` `:3226-3431`). Поэтому `getPeerDialogs` здесь отдаёт полный `dialog`: `pts`, `top_message`, `read_inbox_max_id`, `read_outbox_max_id`, `unread_count`, `unread_mentions_count`, уведомления, папку — и верхние сообщения; поля вычисляются по ADR 0034, п. 1.
  - Android на неизвестный канал без `pts` вызывает `getPeerDialogs` (`loadUnknownChannel`, `MessagesController.java:16380-16441`, 035).
- `channels.checkUsername` сейчас `blocked` (`api-coverage.md:280`).
- Лимиты appConfig: `channels_limit_default` (умолчание клиентов 500, `UserLimitsConfiguration.swift:41`), `channels_public_limit_default` (10, `:42`); по core.telegram.org/api/config — 500 и 10, их же уже отдаёт `teamgramd/bin/app_config.json` (035, U9); лимит закрытой установки — вопрос владельцу 3 в 035.
- **C5:** пока нет 040, iOS, открыв канал, бесконечно повторяет `getChannelDifference` (заглушка отвечает 400, `AccountStateManagementUtils.swift:3448-3458`). На стенде это ожидаемо; в прод создание каналов не выкатывается до 040 (решение владельца из 035).

## Область
- `channels.createChannel` (broadcast и megagroup, без форумов): строка `channels`, создатель в `channel_participants`, сообщение 1 `messageActionChannelCreate` с `pts` 1 в ящике канала (`pkg/channelbox`), диалог создателя через `userbox.Run`, `updateChannel`.
- `channels.getChannels`, `channels.getFullChannel` (`channelFull` с `pts`, количествами, флагами прав), `channels.getParticipant` для себя.
- Ветка каналов в `messages.getDialogs`, `messages.getPeerDialogs` (`:93`) и `dialog.getMyDialogsData` с ненулевым `dialog.pts`.
- `messages.getExportedChatInvites` для канала отдаёт создателю постоянную ссылку (создаётся вместе с каналом).
- `channels.checkUsername` — настоящая минимальная проверка (формат, занятость, лимит публичных ссылок); полная работа с именами — 044.
- Лимит каналов на пользователя по решению владельца, `CHANNELS_TOO_MUCH` при превышении.
- Не трогаем: отправку, историю, участников кроме создателя.

## Критерии приёмки
- Модульные тесты: сборка ответа `createChannel` (чаты, сообщение, `pts`), `checkUsername`.
- Интеграционные тесты на MySQL через `pkg/testinfra`: создание пишет канал, создателя, сообщение 1, журнал с `pts` 1 и диалог создателя одной логической операцией; ошибка на шаге не оставляет полусозданного канала.
- e2e через `pkg/mtclient`: `createChannel` → `getFullChannel` (`pts` ≥ 1) → `getDialogs` и `getPeerDialogs` показывают канал с `dialog.pts` > 0; `getExportedChatInvites(limit=1)` не пуст; тест падает на текущем коде.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами в этом брифе: канал и супергруппа создаются и открываются в iOS, Android, Desktop (CachyOS) и Web K; Android показывает ссылку-приглашение без падения; повторы `getChannelDifference` в iOS до 040 отмечены как ожидаемые.

## Результат
Сервер готов, ветка `task/038-channel-create` (коммит `40b1801b`), не выкачено: каналы показываются людям только после 040 (решение владельца).

- **Создание.** `channels.createChannel` → `channel.createChannel` в biz channel: одна транзакция ящика создателя пишет:
  - строку `channels` (id из `AUTO_INCREMENT` с 1000000000, `migrate-20261104-channel-ids.sql`);
  - создателя с правами;
  - сообщение 1 `messageActionChannelCreate` с `pts` 2;
  - диалог создателя (всё прочитано);
  - постоянную ссылку в `chat_invites`: хэш с буквы — канал, как у апстрима.

  `updateChannel` — обновление с `seq` (свёртка по каналу), другим устройствам создателя. Ответ: канал первым в `chats`, `updateNewChannelMessage`.
- **Чтения.**
  - `channels.getChannels` (неверный `access_hash` — `CHANNEL_INVALID`);
  - `channels.getFullChannel` (`pts`, отметки прочтения, количества, права, постоянная ссылка админам);
  - `channels.getParticipant` для себя;
  - ветки каналов в `messages.getDialogs` и `getPeerDialogs` (`pts`, `top_message`, непрочитанное — из канала, RPC `channel.getChannelDialogs`, `channel.getTopMessages`);
  - `messages.getExportedChatInvites` для админов канала.
- **Проверка.**
  - e2e `TestChannelCreate`: канал и супергруппа, `pts` ≥ 2 в полном канале и диалогах, ссылка не пуста, `getChannels`, `getParticipant`, `updateChannel` на втором устройстве;
  - интеграционный `TestMySQLAutoId`;
  - `make lint`, `go vet`, тесты затронутых пакетов.
- **Не сделано.**
  - `channels.checkUsername` (в 044 вместе с именами);
  - проверка в клиентах — вместе с 039–040, когда каналы можно открыть и писать в них.
- **Решения (технические):**
  - права создателя — наборы по типу чата (у Telegram не проверено);
  - лимит 500 каналов на пользователя;
  - название обрезается до 128 символов;
  - форумы, импорт и гео-группы пока отвечают 400.
