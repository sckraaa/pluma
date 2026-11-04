# 041. Каналы: участники и приглашения

- Статус: in progress (готово: вступление, выход, участники, вход по ссылке, приглашение; нет: заявки на вступление, `getLeftChannels`, ветки каналов в прочих методах ссылок)
- Зависит от: 040
- ADR: 0020, 0021, 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` §1.1, §1.5, §2.5–2.6, задача T6; `clients.md` §1 «Send and receive», §2, §4 п. 3–4; `plan.md` §2 C2, U1, §3, задача «040» плана).

## Цель
В канал и супергруппу можно вступить по ссылке или по имени, пригласить, выйти; список участников работает со всеми фильтрами; заявки на вступление работают; счётчики участников верны при одновременных действиях.

## Что известно
- **Приглашения:** методы ссылок-приглашений (`exportChatInvite`, `checkChatInvite`, `importChatInvite` ×2, `getExportedChatInvite(s)`, `editExportedChatInvite`, `deleteExportedChatInvite`, `deleteRevokedExportedChatInvites`, `getAdminsWithInvites`, `getChatInviteImporters`, `hideChatJoinRequest`) работают только для базовых групп (`server.md` §1.5, строки 493–654 `api-coverage.md`). Хеш, начинающийся с буквы и длиной 20 символов, уже классифицируется как ссылка канала (`app/service/biz/chat/chat/chat_invite_helper.go:18-30`, проверено, `plan.md` §1 п. 5); `chat_invites` имеет `migrated_to_id`.
- **`toggleJoinRequest`, `toggleJoinToSend`** — `blocked` (`api-coverage.md:339-340`); утверждение `clients.md` «уже обслуживаются» неверно (C2).
- **Ответы, которые ждут клиенты:**
  - `messages.checkChatInvite`, `importChatInvite`/`channels.joinChannel` → `messages.ChatInviteJoinResult` (Web K `appChatsManager.ts:681-688`, `appChatInvitesManager.ts:93-99`);
  - после `joinChannel` или `importChatInvite` iOS вызывает `channels.getParticipant(self)`; при ошибке показывает `.generic`, хотя вступление прошло (`JoinChannel.swift:70-81`, проверено в 035; путь по ссылке `JoinLink.swift:123-171` ждёт пир из ответа 5 с);
  - ошибки `INVITE_REQUEST_SENT`, `USERS_TOO_MUCH`, `CHANNELS_TOO_MUCH`, `CHANNEL_PRIVATE` (iOS `JoinChannel.swift:36-41`, tdesktop `apiwrap.cpp:1901-1930`); Android на `CHANNEL_PRIVATE`/`CHANNEL_PUBLIC_GROUP_NA`/`USER_BANNED_IN_CHANNEL` показывает «закрыт / забанен» и не повторяет (`MessagesController.java:16798-16806`, 035);
  - `updateChannel` (api.tl:345): Android при вступлении без диалога зовёт `getChannelDifference(pts=1, limit=1)` (`MessagesController.java:20151-20152`, `:16540-16541`), при выходе или исключении удаляет диалог (`:20153-20154`), всегда `loadFullChat` (`:20173`) (035); Web K перечитывает чат и диалог через `getPeerDialogs` или убирает его (`appMessagesManager.ts:10810-10847`, 035); tdesktop для участника перечитывает диалог и себя (`api_updates.cpp:2455-2478`).
- **Своим устройствам** (U1, ADR 0034, п. 3): `updateChannel` тому, кого касается, идёт с `seq` его аккаунта — своё действие через `EmitSeqUpdate`, чужое (приглашение, исключение) через inbox-событие ADR 0021; `updateChannel` добавляется в `userbox.SeqUpdate`. Как у Telegram — не проверено. `updateChannelParticipant` не шлётся (наши клиенты, кроме Web K, его не разбирают).
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
### Часть 1 (2026-11-04, ветка `task/038-channel-create`)
- **Вступление.** `channels.joinChannel` (оба варианта схемы: `#24b524c5` → `Updates`, `#7f6a1e22` → `chatInviteJoinResultOk`) — в публичный канал; `messages.importChatInvite` (оба варианта) по ссылке канала (хэш с буквы) — в любой. biz `channel.joinChannel`: одна транзакция ящика вступающего под блокировкой канала пишет строку участника, счётчик, диалог (прочитан до последнего сообщения), в супергруппе — служебное сообщение `messageActionChatAddUser` или `messageActionChatJoinedByLink` с пушем участникам. Своим другим устройствам — `updateChannel` с `seq`. Ошибки по core.telegram.org/method/channels.joinChannel: `CHANNEL_PRIVATE` (закрытый без ссылки, исключённый), `USER_ALREADY_PARTICIPANT`, `CHANNELS_TOO_MUCH` (500), `USERS_TOO_MUCH` (200000), `INVITE_HASH_EXPIRED` (отозвана, истекла, исчерпана), `INVITE_HASH_INVALID`. Скрытая история: `available_min_id` — служебное сообщение вступления.
- **Выход.** `channels.leaveChannel`: строка участника `left` (роль и права сброшены), счётчики, диалог удалён, в супергруппе — `messageActionChatDeleteUser`. Ответ — канал как его видит вышедший (`left` или `channelForbidden`). Создатель — `USER_CREATOR`, не участник — `USER_NOT_PARTICIPANT` (core.telegram.org/method/channels.leaveChannel).
- **Ссылка.** `messages.checkChatInvite` для канала: участнику — `chatInviteAlready` (в любом состоянии ссылки), остальным — `chatInvite` (название, описание, фото, число участников, вид); `participants` не отдаётся (поле необязательное).
- **Участники.** `channels.getParticipants` со всеми фильтрами (`recent`, `admins`, `kicked`, `banned`, `search`, `mentions`, `bots`, `contacts`): создатель и админы первыми, затем по дате; поиск — начало имени, фамилии, полного имени или username. В канале (broadcast) список только админам (`CHAT_ADMIN_REQUIRED`). `channels.getParticipant` — теперь и о других (в канале — только админы).
- **Проверка:** интеграционные `TestJoinLeave`, `TestJoinByLink`, `TestJoinConcurrent` (12 параллельных вступлений и выходов — счётчик и строки сходятся), `TestParticipants`; e2e `TestChannelJoin` (супергруппа и канал: ссылка, вступление, `getParticipant(self)`, список, пуш поста новому участнику, подписчик канала не публикует — критерий 039, выход, `channelForbidden`, `USER_CREATOR`).
- **Решения на подтверждение:** при вступлении диалог прочитан до последнего сообщения; у вышедшего диалог удаляется; ссылка с `request_needed` для канала пока невозможна (её создаёт `exportChatInvite`, у каналов его нет) и отвечается `INVITE_HASH_INVALID` — до заявок на вступление.
- **Осталось:** `getLeftChannels`, заявки на вступление, `toggleJoinRequest`/`toggleJoinToSend`, ветки каналов в `exportChatInvite`, `editExportedChatInvite`, `getChatInviteImporters`, `hideChatJoinRequest` и прочих методах ссылок; вступление по имени — после 044.

### Часть 2: приглашение (2026-11-04)
- `channels.inviteToChannel` (оба варианта схемы; в layer 228 — `#c9e33d54` → `messages.invitedUsers`). bff исключает тех, чья приватность «кто может добавлять меня» не пускает приглашающего, и называет их в `missing_invitees` (проверка перенесена из bff chats в biz user, `MissingInvitee`), удалённые аккаунты пропускает.
- biz `channel.inviteToChannel`: транзакция ящика приглашающего под блокировкой канала пишет строки участников (вернувшийся исключённый — заново участник), счётчик, в супергруппе одно служебное `messageActionChatAddUser` со всеми добавленными (пуш участникам) и на каждого добавленного — inbox-событие `inbox.channelInvitedToInbox`. Его транзакция у получателя (msg inbox) пишет диалог, прочитанный до последнего сообщения, и `updateChannel` с `seq` (ADR 0021, ADR 0034, п. 3); если к тому времени он уже не в канале — ничего.
- Права: создатель и админы с `invite_users` — всегда; в супергруппе обычный участник — если права по умолчанию не запрещают `invite_users`; иначе `CHAT_ADMIN_REQUIRED`. Исключённого возвращает только админ с `ban_users` (`USER_KICKED`). Уже участники пропускаются; у кого 500 каналов — `USER_CHANNELS_TOO_MUCH`; не больше 200 за вызов.
- **Проверка:** интеграционный `TestInvite` (повторы, служебное сообщение, события, права участника, исключённый, подписчик канала); e2e `TestChannelInvite` (приглашённый получает `updateChannel`, диалог и пост; закрывший приглашения — в `missing_invitees`).

