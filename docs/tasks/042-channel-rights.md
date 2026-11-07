# 042. Каналы: права админов, баны, права по умолчанию

- Статус: in progress (2026-11-06)
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

## Исследование (2026-11-06)
Три разбора: сервер, документация Telegram, исходники четырёх клиентов на пинах `upstream.env`. Полные отчёты — `~/pluma-work/research/042/` (server.md, telegram.md, clients.md), вне репозитория.

- **Передача владения на слое 228** — `messages.editChatCreator` и `messages.getFutureChatCreatorAfterLeave`. Их зовут iOS (`ChannelOwnershipTransfer.swift:36,112`) и tdesktop (`channel_ownership_transfer.cpp:51,102`). Android зовёт старые `channels.editCreator` и `channels.getFutureCreatorAfterLeave` (`ChatRightsEditActivity.java:1116`, `AlertsCreator.java:2895`). Web K не зовёт ни те, ни другие. Обе пары есть в нашей схеме.
- **Проба iOS.** iOS проверяет, можно ли передать владение, вызовом `editChatCreator(inputPeerEmpty, user, inputCheckPasswordEmpty)` и ждёт `PASSWORD_HASH_INVALID` (`ChannelOwnershipTransfer.swift:36`). Пароль проверяется до разбора peer.
- **`default_banned_rights` есть у канала всегда.** Без него Android запрещает всё (`ChatObject.java:1932-1938`), Web K тоже (`hasRights.ts:41-45`).
- **Пустые `admin_rights` (флаги 0) не-админу не отдаются.** iOS и Android считают любые `admin_rights` правами админа (`TelegramChannel.swift:46-48`, `ChatObject.java:1928-1930`).
- **`USER_BANNED_IN_CHANNEL` все четыре клиента показывают как общий антиспам-запрет** («нельзя писать в публичные группы»): Android `AlertsCreator.java:513-514`, tdesktop `apiwrap.cpp:530-541`, iOS `PendingMessageManager.swift:94`. Поэтому запрет в канале — `CHAT_WRITE_FORBIDDEN` и `CHAT_SEND_*_FORBIDDEN`.
- **iOS повторяет `editBanned` и `getParticipants` бесконечно при любой ошибке** (`ChannelBlacklist.swift:38-39`, `Network.swift:1203-1215`).
- **`updateChannelParticipant` из клиентов разбирает только Web K**, и то строит его сам после `editAdmin`/`editBanned` (`appChatsManager.ts:865-892, 996-1010`). Запись 035 про iOS не подтверждается исходниками.
- **Что админ может выдать.** Все клиенты ограничивают выдаваемые права своими. Создатель выдаёт всё. Android берёт `null` у создателя как «все права» (`ChatRightsEditActivity.java:232-236`).
- **Документация** (core.telegram.org/api/rights, constructor/chatBannedRights): `until_date` меньше 30 с или больше 366 дней от сейчас — навсегда; права по умолчанию — все флаги, кроме `view_messages`; `add_admins` даёт права «равные или меньше» своих. Как объединяются права по умолчанию и личные, документация не говорит.
- **Сервер сегодня.** `editAdmin`, `editBanned` — заглушки; ветка каналов `editChatDefaultBannedRights` закрыта. Состояние `state = 2` никто не пишет, счётчики `kicked_count`/`banned_count` тоже. Ограниченный участник (`state 2` без `view_messages`) выпал бы из рассылки и отправки (`state = 0` в `history.go:39`, `seq.go:101`, `channel_send.go:81`). `until_date` нигде не учитывается при чтении.

## Решения (на подтверждение)
1. **`state = 2` — только исключённый** (`view_messages`). Ограниченный остаётся участником (`state = 0`) с `banned_rights` и `until_date`. Так все запросы «участники» (`state = 0`) остаются верными, и ограниченный получает обновления, как в Telegram.
2. **Истечение временного запрета — при чтении** (`until_date` сравнивается со временем). Отдельной задачи по расписанию нет: строка с истёкшим запретом ведёт себя как без него. Счётчики `kicked_count` и `banned_count` считаются запросом при `getFullChannel` (их видят только админы), а не хранятся: хранимый счётчик расходился бы с истёкшими запретами.
3. **Ошибки отправки**: личный или общий запрет — `CHAT_WRITE_FORBIDDEN` (текст) и `CHAT_SEND_<ТИП>_FORBIDDEN` (медиа по типу); `USER_BANNED_IN_CHANNEL` не используется (клиенты показывают его как антиспам). Это меняет п. 8 ADR 0034 — новый ADR.
4. **Списки исключённых и ограниченных не-админу** — пустой список, не ошибка: iOS повторяет ошибку бесконечно. Что отдаёт Telegram, не проверено.
5. **`editAdmin` на не-участника** — `USER_NOT_PARTICIPANT`: iOS по нему приглашает и повторяет (`PeerAdmins.swift:205-222`), Android приглашает заранее.
6. **Владение.** `messages.editChatCreator` и `channels.editCreator` — одна логика, пароль проверяется первым. Уход создателя с отложенной передачей через 7 дней (core.telegram.org/api/channel) — отдельной задачей; `getFutureChatCreatorAfterLeave` пока отвечает ошибкой, и клиенты показывают обычный выход.
7. **Создатель** получает полные `admin_rights` в `channel`.

## Ход работы
- [ ] `channelperm.Can` и таблица тестов.
- [ ] Модель: `state 2` только исключённый, `until_date` при чтении, счётчики запросом.
- [ ] `channels.editAdmin`, `channels.editBanned`, `messages.editChatDefaultBannedRights` (каналы).
- [ ] Перевод обработчиков записи на `channelperm.Can`; ошибки отправки по типу медиа.
- [ ] Исключённому — `updateChannel` с `channelForbidden` (событие inbox, как удаление канала).
- [ ] `getParticipants`/`getParticipant` для не-админов.
- [ ] Передача владения.
- [ ] ADR, e2e, проверка в клиентах.

## Результат
