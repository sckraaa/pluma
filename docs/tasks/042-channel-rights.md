# 042. Каналы: права админов, баны, права по умолчанию

- Статус: todo
- Зависит от: 041
- ADR: 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` §2.5–2.6, задача T7; `clients.md` §2, §4 п. 4–5; `plan.md` §2 U3, §3, задача «041» плана).

## Цель
Экраны админов и забаненных работают во всех четырёх клиентах; каждое действие записи в канале проверяется одной функцией прав; бан с `view_messages` превращает канал у пользователя в `channelForbidden`.

## Что известно
- Права по участнику: `admin_rights` (`chatAdminRights`, api.tl:1293) и `banned_rights` (`chatBannedRights`, api.tl:1295, с `until_date`); `default_banned_rights` — на канале. Исследование предлагает одну чистую функцию `channelperm.Can(channel, participant, action)` для всех обработчиков записи (отправка по типу медиа, закрепление, приглашение, смена данных) (`server.md` §2.6).
- `messages.editChatDefaultBannedRights` для каналов заблокирован (`app/bff/chats/.../messages.editChatDefaultBannedRights_handler.go:60-63`, `server.md` §1.1).
- `channelForbidden` (api.tl:105) — для исключённого или забаненного с `view_messages`; `until_date` — временный бан (`server.md` §2.5).
- Клиенты: iOS — `PeerAdmins.swift` (`editAdmin` с `updateChannelParticipant`, `admin_rights` в `Chat`), `ChannelBlacklist.swift` (`editBanned`, `editChatDefaultBannedRights`); `channelFull.admins_count`, `participants_count`, `can_view_participants` (`UpdateCachedPeerData.swift:697-935`, 035). Android на `updateChannel` после исключения удаляет диалог (`MessagesController.java:20153-20154`, 035). `updateChannelParticipant` разбирает только Web K, для сброса кэша (`appChatsManager.ts:1419-1422`), поэтому он не шлётся (ADR 0034, п. 3). Web K переписывает чат в `channelForbidden` и шлёт `updateChannel` (`appMessagesManager.ts:12545-12565`, `appProfileManager.ts:782-830`).
- Ошибки (035, U3; core.telegram.org/method/messages.sendMessage, sendMedia): `USER_BANNED_IN_CHANNEL` — 400, `CHAT_SEND_{PLAIN,MEDIA,PHOTOS,VIDEOS,ROUNDVIDEOS,DOCS,VOICES,AUDIOS,STICKERS,GIFS,POLL}_FORBIDDEN` — 403, `CHAT_ADMIN_REQUIRED` — 403 (ADR 0034, п. 8).
- Проверки по роли, временно поставленные в 039, 041, 043, 044 (что из них сделано раньше), переводятся на `channelperm.Can`.

## Область
- `channels.editAdmin`, `editBanned`, `editCreator`, `getFutureCreatorAfterLeave`; ветка каналов в `messages.editChatDefaultBannedRights` (снять блок `:60`).
- Пакет `channelperm` с `Can` и вызов из всех уже существующих обработчиков записи в каналы.
- `channelForbidden` с `until_date`; истечение временного бана; `updateChannel` и `updateChannelParticipant` затронутым.
- Счётчики `admins_count`, `kicked_count`, `banned_count` в строке `channels`.
- Не трогаем: медленный режим (045), журнал действий админов (отложен, вопрос владельцу в 035).

## Критерии приёмки
- Модульные тесты: таблица `Can` по каждому биту `chatAdminRights` и `chatBannedRights`, права по умолчанию, истечение `until_date`, создатель.
- Интеграционные тесты на MySQL через `pkg/testinfra`: смена прав и бан меняют участника и счётчики одной транзакцией.
- e2e через `pkg/mtclient`: забаненный без медиа не может отправить медиа (`CHAT_SEND_MEDIA_FORBIDDEN` или ошибка типа медиа, ADR 0034, п. 8); админ без `pin_messages` не может закрепить (закрепление — в 043; если 043 ещё не сделана, этот тест добавляет она); бан с `view_messages` даёт `channelForbidden`; тест падает на текущем коде.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: экраны админов, забаненных и прав по умолчанию работают в iOS, Android, Desktop (CachyOS) и Web K; исключённый видит, что доступа нет.

- **Из ревью ветки каналов (2026-11-04):** с появлением банов закрыть для не-админов фильтры `channelParticipantsKicked`/`channelParticipantsBanned` в `channels.getParticipants` и строку `channelParticipantBanned` другого пользователя в `channels.getParticipant` (сейчас они видны любому читателю супергруппы, но пусты: `state = 2` пока никто не пишет). Что именно отдаёт Telegram не-админу — сверить по источнику.

## Результат
