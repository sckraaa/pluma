# 038. Каналы: создание, `getChannels`, `getFullChannel`, диалоги

- Статус: todo
- Зависит от: 037
- ADR: 0011, 0021, «Ящик канала» (из 035). Исследование: `docs/tasks/035-research/` (`server.md` §2.2–2.5, задача T3; `clients.md` §1 «Create», «Open a channel», §2, §4 п. 1; `plan.md` §3, задача «037» плана).

## Цель
Канал и супергруппу можно создать и открыть во всех четырёх клиентах; канал появляется в списке чатов с ненулевым `dialog.pts`; Android показывает ссылку-приглашение без падения.

## Что известно
- **Ответ `channels.createChannel`** (`Updates`):
  - `channel` в `chats`: Web K берёт `updates.chats[0].id` и падает без чатов (`appManagers/appChatsManager.ts:588-593`); Android без чатов шлёт `chatDidFailCreate` (`MessagesController.java:14398-14434`, UNVERIFIED, U10);
  - сообщение нового канала: iOS ищет в `updates.messages` первое сообщение с пиром нового канала, и этот пир должен оказаться в локальном хранилище за 5 с, иначе `.generic` (`TelegramEngine/Peers/ChannelCreation.swift:50-90`, `State/UpdatesApiUtils.swift:665-690`); на практике это `updateNewChannelMessage(messageActionChannelCreate)` с `pts` 1;
  - ошибки, которые разбирает iOS: 406 (показывает текст сервера), `CHANNELS_TOO_MUCH`, `CHANNELS_ADMIN_LOCATED_TOO_MUCH`, `USER_RESTRICTED`.
  - Поток создания в tdesktop не читался (UNVERIFIED, U7).
- **Экран создания Android** (`ui/ChannelCreateActivity.java`, `clients.md` §1, §2):
  - `channels.checkUsername(inputChannelEmpty, "1")` — проверка лимита публичных ссылок (`:170-173`); блокирует только `CHANNELS_ADMIN_PUBLIC_TOO_MUCH`;
  - затем `getAdminedPublicChannels`, `updateUsername` и `messages.getExportedChatInvites(limit=1)` (`:935-944`);
  - **падение:** пустой список из `getExportedChatInvites(limit=1)` → `invites.invites.get(0)` (`:940-943`, проверено в `plan.md` §1 п. 11). Создатель всегда должен получать постоянную ссылку.
- **Открытие канала:**
  - Android: `channels.getFullChannel` и загрузка админов (`MessagesController.java:7546-7575`, UNVERIFIED, U10);
  - iOS: `getFullChannel` вместе с `channels.getParticipant(inputPeerSelf)` (`UpdateCachedPeerData.swift:656-680`, UNVERIFIED, U10); ошибки `getFullChannel` становятся nil (`:657-663`); `participants_count`, `admins_count`, `can_view_participants` (`:694-700`);
  - `pts` канала клиенты берут из `dialog.pts` или `channelFull.pts`: tdesktop не инициализирует `pts` и не запрашивает разницу при нуле (`api/api_updates.cpp:726`), Web K `addChannelState` бросает исключение на `pts` 0 (`apiUpdatesManager.ts:531-536`, UNVERIFIED, U10), Android считает 0 «неизвестно» и снова спрашивает `getPeerDialogs` (`MessagesController.java:17417-17428`, UNVERIFIED, U10).
- **Диалоги:**
  - `messages.getPeerDialogs` пропускает каналы с логом «blocked» (`app/bff/dialogs/internal/core/messages.getPeerDialogs_handler.go:93-95`; путь исправлен в `plan.md` §1 п. 4);
  - biz dialog уже перечисляет диалоги `PEER_CHANNEL` (`app/service/biz/dialog/internal/core/dialog.getMyDialogsData_handler.go:102-130`, ключ кэша `GetChannelsCacheKey`);
  - `dialog#fc89f7f3` (api.tl:216) несёт `pts`; в таблице `dialogs` столбца `pts` нет — `dialog.pts` берётся из `channels.pts`, не из копии (`server.md`, §2.3);
  - C4 (`plan.md` §2): возможно, iOS после `getDifference` сбрасывает каналы через `getPeerDialogs` (`AccountStateManagementUtils.swift:2042-2070`, `:3226-3240`, UNVERIFIED до 035). Тогда `getPeerDialogs` должен отдавать `pts`, `top_message`, состояние прочтения и верхние сообщения уже здесь.
  - Android на неизвестный канал без `pts` вызывает `getPeerDialogs` (`loadUnknownChannel`, `MessagesController.java:15678-15730`, UNVERIFIED, U10).
- `channels.checkUsername` сейчас `blocked` (`api-coverage.md:280`).
- Лимиты appConfig: `channels_limit_default` (умолчание клиентов 500, `UserLimitsConfiguration.swift:41`), `channels_public_limit_default` (10, `:42`); настоящие значения Telegram — UNVERIFIED (U9), выбор — вопрос владельцу из 035.
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
