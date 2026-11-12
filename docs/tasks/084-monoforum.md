# 084. Личные сообщения каналу (monoforum)

- Статус: сделано, кроме предложенных постов (ветка `task/084-monoforum`, ждёт сборки партии; находки независимой проверки исправлены, сквозной тест проходит на стенде).
- Решение: ADR 0055.

## Что нужно
Владелец: «из ненужного монофорумы можно сделать». Реализовать личные сообщения каналу, как в Telegram (core.telegram.org/api/monoforum): включение методом `channels.updatePaidMessagesPrice` с `broadcast_messages_allowed`, связанный монофорум, чаты подписчиков по `saved_peer_id`, методы под-чатов с `parent_peer`, отметки прочтения, `channels.getMessageAuthor`, права, уведомления. Цена в звёздах не поддерживается. Предложенные посты — только если работают без звёзд.

## Источники
Разобраны в ADR 0055 («Контекст»): core.telegram.org/api/monoforum, /api/paid-messages, /api/suggested-posts, страницы методов; код четырёх клиентов из `~/pluma-work/reference-src-new` с номерами строк.

## Результат
Личные сообщения каналу работают на сервере: владелец или админ с правом менять информацию включает их, у канала появляется монофорум; подписчик пишет в него и видит только свой чат с каналом; админы с правом «управлять личными сообщениями» видят чаты всех подписчиков списком, читают, отвечают от имени канала, удаляют; отметки прочтения идут в обе стороны; сервер говорит админу, кто из админов ответил. Права читаются из канала при каждом запросе, строк участников у монофорума нет. Предложенные посты не сделаны (см. ниже).

Изменения:
- Схема: `migrate-20261114-monoforum.sql` — `channels.broadcast_messages_allowed`, `channels.monoforum`, `channels.linked_monoforum_id`, `channel_messages.saved_peer_id` с индексом `sublist`, таблицы `channel_monoforum_sublists`, `channel_monoforum_drafts`.
- `pkg/monoforum`: кто пользователь в монофоруме (`SideOf`, `Members`, `AdminIds`, `Recipients`), чаты подписчиков (`Moved`, `Retop`, `ReadInbox`, `ReadOutbox`, отметка «непрочитано», список), черновики админов.
- biz channel:
  - поля `ImmutableChannel`: `monoforum = 51`, `broadcast_messages_allowed = 52`, `linked_monoforum_id = 53`; вид монофорума (`monoforumChat`), имя и фото от канала, «собранные» участники (`monoforumViewers`); `getChannelListByIdList` добавляет канал после монофорума;
  - новые RPC: `channel.updateDirectMessages`, `getSublists`, `getSublistHistory`, `readSublist`, `deleteSublistHistory`, `getMessageAuthor`, `markSublistUnread`, `getSublistUnreadMarks`, `getMessageSublists`;
  - подписчик видит только свой чат: история, сообщения по id, поиск, диалог, последнее сообщение, разница канала (`viewer.sublist`, `channelFold.setSublist`); ветки ответов в монофоруме не ведутся;
  - чтение (`readMonoforum`, `readSublist`), удаление (`deleteMonoforum`, `DeleteSublistHistory`), получатели обновлений (`MemberIds`), удаление канала удаляет монофорум.
- msg: `SendChannel` раскладывает сообщения монофорума по чатам (`routeToSublist`): подписчик — в свой, админ — в названный, от имени канала; закреп — только админы, служебное сообщение в чате; реакция подписчика — только на сообщения своего чата; реакции показываются счётчиками; диалог монофорума у админов и у подписчика, убравшего чат, появляется событием `inbox.channelInvitedToInbox`.
- sync и сервис уведомлений: подписчик получает обновления только своего чата (`channel.SublistView`), канал приходит вместе с монофорумом.
- bff:
  - channels: `channels.updatePaidMessagesPrice` (сервис `RPCPaidMessage` зарегистрирован в bff и разведён в трёх `session.yaml`; остальные его методы — `ERR_ENTERPRISE_IS_BLOCKED`), `channels.getFullChannel` отдаёт обе стороны;
  - savedmessagedialogs: `messages.getSavedDialogs`, `getSavedDialogsByID`, `getSavedHistory`, `deleteSavedHistory` с `parent_peer` монофорума, `messages.readSavedHistory`, `channels.getMessageAuthor` (заглушки апстрима заменены);
  - dialogs: `messages.markDialogUnread` и `getDialogUnreadMarks` с `parent_peer`;
  - drafts и messages: черновик админа в чате подписчика (`saveDraft`, `getAllDrafts`, `clearAllDrafts`, `clear_draft` при отправке);
  - messages: `reply_to.monoforum_peer_id` и `suggested_post` в `sendMessage`, `sendMedia`, `sendMultiMedia`, `forwardMessages`.
- `third_party(proto)`: `IDListHelper.PickByMessage` берёт пользователя из `saved_peer_id` сообщений каналов (по ADR 0009 — отдельным коммитом с префиксом; см. «Отклонения»).
- Документы: ADR 0055, `docs/ops/monoforum.md`, реестр функций, `TESTING-BACKLOG`.

Проверка (CachyOS, 2026-11-12):
- `go build ./...` — без ошибок.
- `go vet -tags integration,e2e -unreachable=false` по затронутым пакетам и `./test/e2e/` — без замечаний; `gofmt -l` пуст.
- Модульные тесты затронутых пакетов (`pkg/monoforum`, biz channel, msg, sync, push, bff savedmessagedialogs, channels, dialogs, drafts, messages, bff) и `go test ./tools/...` — зелёные; `third_party/teamgram-proto/mtproto` — зелёные.
- Интеграционные на MySQL: `app/service/biz/channel/internal/core` (новые `TestDirectMessagesSwitch`, `TestMonoforumViewers`, `TestSublistsApart`, `TestSublistList`, `TestSublistRead`, `TestSublistDelete` и все прежние), `…/channel/internal/dao`, `app/messenger/msg/internal/dao` (новые `TestMonoforumSend`, `TestMonoforumReactionsAndPins` и все прежние), `pkg/channelbox`, `pkg/sendas`, `pkg/testinfra` — зелёные.
- Проверка «тест падает без изменения»: с выключенным фильтром чата (`viewer.own`) `TestSublistsApart` и `TestSublistDelete` падают (подписчик видит чужие сообщения).
- Сквозной `test/e2e/monoforum_test.go` (`TestMonoforum`) написан и собран (`go vet -tags e2e`), **не запускался**: стенды в этой волне не поднимаются. `go run ./tools/apicoverage` не запускался (один прогон в конце партии). `-race` и `make lint` — на ветке партии.

После независимой проверки (2026-11-12) исправлено:
- Набор текста в монофоруме: подписчик «печатает» только админам личных сообщений, набор админа не уходит никому (раньше уходил всем подписчикам с id набирающего); `messages.getOnlines` подписчику считает только его самого.
- `channels.getParticipant` о других в монофоруме — только админам личных сообщений (`CHAT_ADMIN_REQUIRED`), иначе подписчик узнавал, кто отвечает от имени канала.
- `default_banned_rights` монофорума запрещают закреп, смену информации и приглашения: клиенты больше не предлагают подписчику «Закрепить», которое сервер отклонял.
- `channels.leaveChannel` монофорума убирает чат с каналом из списка (tdesktop: «Покинуть группу» — единственный способ), ответ без `updateChannel`; следующее сообщение чата возвращает его.
- Поиск, счётчики, календарь и позиции с `saved_peer_id` ищут внутри одного чата (`channel.search`, `searchCount`, `searchCalendar`, `searchPositions` получили `saved_peer_id`; новые CRC, `channel.tl.pb.go` перегенерирован protoc). Раньше поиск и счётчики в чате подписчика были пустыми, календарь и позиции шли по всему монофоруму.
- Удаление канала убирает монофорум из диалогов всех, у кого он есть: у удаляющего сразу с `updateChannel`, у остальных (и у подписчиков-неучастников) — `inbox.channelDeletedToInbox` по id монофорума.
- `updateMessageReactions` несёт `saved_peer_id` чата (Android считает по нему непрочитанные реакции админа).

Проверка исправлений (CachyOS, 2026-11-12): `go build ./...` — ок; `go vet -tags integration,e2e -unreachable=false` по bff channels/dialogs/messages, biz channel, msg и `./test/e2e/` — ок; `go test ./app/... ./pkg/...` и `go test ./tools/...` — зелёные; `-race` по затронутым пакетам — зелёный; интеграционные `biz/channel/internal/core` (новые `TestMonoforumLeave`, `TestMonoforumDeletedWithChannel`, поиск в чате в `TestSublistsApart`), `biz/channel/internal/dao`, `msg/internal/dao`, `pkg/channelbox`, `pkg/monoforum` — зелёные; e2e на своём стенде (`TestMonoforum` с новыми проверками набора, `getParticipant`, поиска в чате и ухода/возврата чата, а также `TestChannelRejoinAndDelete`, `TestChannelReactions`, `TestChannelForum`, `TestChannelJoin`, `TestChannelInvite`, `TestSharedPhotos`, `TestChannelCreate`, `TestChannelSend`) — PASS, стенд снят. Без исправления `TestMonoforumChat` падает (права по умолчанию пустые); `TestMonoforumDeletedWithChannel` поймал событие inbox самому удаляющему. `make lint` — на ветке партии.

## Не сделано
- **Предложенные посты**: `suggested_post` в отправке и `messages.toggleSuggestedPostApproval`. Без звёзд они возможны, но одобрение должно публиковать пост в канал (в том числе отложенно и от имени админа, когда одобряет подписчик) — отдельная задача. Сейчас отправка с `suggested_post` в монофорум отвечает `ERR_ENTERPRISE_IS_BLOCKED`, вне монофорума — `SUGGESTED_POST_PEER_INVALID`. Кнопку клиенты показывают всем не-админам монофорума без флага сервера (tdesktop `history_widget.cpp:4195-4198`), поэтому подписчик, нажавший «Предложить пост», получит ошибку; обычные сообщения не затронуты (Android ставит `suggested_post` только после листа предложения, `SendMessagesHelper.java:5314, 5446`).
- `messages.unpinAllMessages`, `getUnreadReactions`, `readReactions` с `saved_peer_id`.
- `saved_peer_id` в `updateChannelReadMessagesContents` (клиенты находят сообщение по id: tdesktop `api_updates.cpp:1763-1789`, Android `MessagesController.java:18815-18826`).
- «Очистить историю у себя» в монофоруме (tdesktop не предлагает, `window_peer_menu.cpp:863`); запись в «Недавние действия» о включении.

## Решения, требующие подтверждения владельцем
1. Канал не пишет подписчику первым: админ отвечает только тем, кто уже написал каналу (`REPLY_TO_MONOFORUM_PEER_INVALID` иначе). Как у Telegram — не проверено.
2. Написать каналу может тот, кто видит канал: участник закрытого, любой не забаненный у публичного. Забаненный в канале теряет и свой чат с каналом.
3. Любой админ личных сообщений удаляет любое сообщение монофорума и чат целиком (отдельного права на удаление не требуется).
4. Реакции в монофоруме показываются счётчиками, без списка поставивших: иначе подписчик видел бы, кто из админов отреагировал.
5. Цена больше нуля — `STARS_AMOUNT_INVALID`; в супергруппе метод всегда отвечает `CHAT_NOT_MODIFIED`.
6. После выключения обе стороны перестают называть друг друга (`linked_monoforum_id` не отдаётся); переписка сохраняется и возвращается при включении.
7. Самому монофоруму админу отдаются те же `admin_rights`, что у него в канале (что отдаёт Telegram — не проверено; клиенты решают по каналу).
8. Набор текста подписчика видят только админы личных сообщений; набор админа не уходит никому (иначе подписчик узнаёт админа за каналом, а чат подписчика в `messages.setTyping` слоя 228 не указывается). Что делает Telegram — не проверено.
9. `default_banned_rights` монофорума запрещают закреп, смену информации и приглашения, чтобы клиенты не предлагали подписчику действий, которые сервер отклоняет. Что отдаёт Telegram — не проверено.
10. `channels.leaveChannel` монофорума только убирает чат с каналом из списка пользователя; следующее сообщение чата возвращает его. Ответ — без `updateChannel`.

## Отклонения и открытые вопросы
- Подписчик активного монофорума получает свои сообщения с разрывами `pts` (между ними были чужие чаты) и догоняет через `updates.getChannelDifference`: задержка как у любого разрыва. Следствие общей последовательности `pts` (ADR 0055, п. 4).
- Какой вызов делают Android, iOS и Web, чтобы убрать чат с каналом из списка, — не проверено (tdesktop — `channels.leaveChannel`, сделано по нему).
- Другие устройства того, кто убрал чат с каналом, узнают об этом только при следующей загрузке списка: события об удалении диалога в схеме нет, а `updateChannel` вернул бы чат (п. 10 решений).
- Правка `IDListHelper.PickByMessage` в `third_party/teamgram-proto` сейчас лежит в общем коммите задачи; по ADR 0009 её надо вынести в отдельный коммит `third_party(proto):` при сборке партии.
