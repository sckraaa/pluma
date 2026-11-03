# 041. Каналы: участники и приглашения

- Статус: todo
- Зависит от: 040
- ADR: 0020, 0021, «Ящик канала» (из 035). Исследование: `docs/tasks/035-research/` (`server.md` §1.1, §1.5, §2.5–2.6, задача T6; `clients.md` §1 «Send and receive», §2, §4 п. 3–4; `plan.md` §2 C2, U1, §3, задача «040» плана).

## Цель
В канал и супергруппу можно вступить по ссылке или по имени, пригласить, выйти; список участников работает со всеми фильтрами; заявки на вступление работают; счётчики участников верны при одновременных действиях.

## Что известно
- **Приглашения:** методы ссылок-приглашений (`exportChatInvite`, `checkChatInvite`, `importChatInvite` ×2, `getExportedChatInvite(s)`, `editExportedChatInvite`, `deleteExportedChatInvite`, `deleteRevokedExportedChatInvites`, `getAdminsWithInvites`, `getChatInviteImporters`, `hideChatJoinRequest`) работают только для базовых групп (`server.md` §1.5, строки 493–654 `api-coverage.md`). Хеш, начинающийся с буквы и длиной 20 символов, уже классифицируется как ссылка канала (`app/service/biz/chat/chat/chat_invite_helper.go:18-30`, проверено, `plan.md` §1 п. 5); `chat_invites` имеет `migrated_to_id`.
- **`toggleJoinRequest`, `toggleJoinToSend`** — `blocked` (`api-coverage.md:339-340`); утверждение `clients.md` «уже обслуживаются» неверно (C2).
- **Ответы, которые ждут клиенты:**
  - `messages.checkChatInvite`, `importChatInvite`/`channels.joinChannel` → `messages.ChatInviteJoinResult` (Web K `appChatsManager.ts:681-688`, `appChatInvitesManager.ts:93-99`);
  - после `joinChannel` или `importChatInvite` iOS вызывает `channels.getParticipant(self)`; при ошибке показывает `.generic`, хотя вступление прошло (`JoinChannel.swift:70-80`, UNVERIFIED, U10);
  - ошибки `INVITE_REQUEST_SENT`, `USERS_TOO_MUCH`, `CHANNELS_TOO_MUCH`, `CHANNEL_PRIVATE` (iOS `JoinChannel.swift:36-41`, tdesktop `apiwrap.cpp:1901-1930`); Android на `CHANNEL_PRIVATE`/`CHANNEL_PUBLIC_GROUP_NA`/`USER_BANNED_IN_CHANNEL` показывает «закрыт / забанен» и не повторяет (`MessagesController.java:16067-16092`, UNVERIFIED, U10; `ui/ChatActivity.java:22186-22225`);
  - `updateChannel` (api.tl:345): Android при вступлении без диалога зовёт `getChannelDifference(pts=1, limit=1)` (`MessagesController.java:19320-19327`, `:15820-15830`), при выходе или исключении удаляет диалог (`:19327-19332`) (UNVERIFIED, U10); Web K перечитывает чат и диалог через `getPeerDialogs` или убирает его (`appMessagesManager.ts:10810-10847`, UNVERIFIED, U10).
- **Своим устройствам:** идут ли вступление и выход через общий ящик (`pts`/`seq`) — UNVERIFIED (U1), закрывается в 035; путь доставки `seq` другим пользователям — ADR 0021.
- **`getParticipants`** — все фильтры `channelParticipantsFilter` (api.tl:784-791): recent, admins, kicked, banned, search и остальные; в broadcast не-админам список не отдаётся (`server.md` §2.6).
- **`hidden_prehistory`**: при вступлении `available_min_id = last_message_id` (`server.md` §2.6).
- Служебные сообщения `messageActionChatAddUser`/`messageActionChatJoinedByLink`; их тексты рисуют клиенты.

## Область
- `channels.joinChannel`, `leaveChannel`, `inviteToChannel` (оба варианта схемы), `getParticipants` (все фильтры), `getParticipant`, `getLeftChannels`.
- Ветки каналов в `exportChatInvite`, `checkChatInvite`, `importChatInvite`, `editExportedChatInvite`, `getChatInviteImporters`, `hideChatJoinRequest` и остальных методах ссылок из списка выше.
- `toggleJoinRequest` и `toggleJoinToSend` — настоящие.
- Служебные сообщения в ящике канала, `updateChannel` затронутым пользователям, счётчики в строке `channels`, `available_min_id` при `hidden_prehistory`.
- Проверки прав на приглашение — по роли (создатель или админ с `invite_users`); в 042 переезжают в `channelperm.Can` (кто из 041 и 042 позже, тот подключает).
- Не трогаем: права и баны (042), исключение участника (`editBanned`, 042).

## Критерии приёмки
- Модульные тесты: фильтры `getParticipants`, ответ `ChatInviteJoinResult`.
- Интеграционные тесты на MySQL через `pkg/testinfra`: параллельные вступления и выходы сохраняют верные счётчики; повторное вступление идемпотентно.
- e2e через `pkg/mtclient`: вступление по ссылке, история видна от `available_min_id`; выход даёт `left` или `channelForbidden` в объекте канала; заявка на вступление одобряется и отклоняется; тест падает на текущем коде.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: вступление по ссылке в iOS, Android, Desktop (CachyOS) и Web K (по публичному имени — после 044: публичные имена и `contacts.resolveUsername` для каналов там; `plan.md` ставит «по имени» в эту задачу, но без 044 его не проверить); после вступления iOS не показывает ошибку (`getParticipant(self)` работает); выход убирает диалог в Android; список участников открывается.

## Результат
