# 0055. Личные сообщения каналу (monoforum): отдельная супергруппа без участников, чаты подписчиков по `saved_peer_id`

- Статус: proposed (2026-11-12)
- Задача: 084 (ветка `task/084-monoforum`). Владелец: «из ненужного монофорумы можно сделать».
- Связано: ADR 0034 (ящик канала), 0038 (права в каналах), 0041 (Premium для всех), 0045 (несколько слоёв), 0020/0021 (события `seq` и inbox), задача 079 (под-чаты «Избранного», `app/bff/savedmessagedialogs`).

## Контекст

### Документация
core.telegram.org/api/monoforum, /api/paid-messages, /api/suggested-posts, страницы методов (прочитаны 2026-11-11):
- `channels.updatePaidMessagesPrice` с `broadcast_messages_allowed` включает личные сообщения канала. У канала появляются `channel.broadcast_messages_allowed` и `linked_monoforum_id` — id новой особой супергруппы, монофорума; у неё флаг `channel.monoforum` и `linked_monoforum_id` канала.
- В монофорум не вступают: подписчик просто пишет в него. Каждое сообщение лежит в «теме» одного пользователя, тема — это `saved_peer_id` сообщения (не темы форума).
- Админы канала с правом `manage_direct_messages` видят все темы и отвечают от имени канала: в `reply_to` передают `inputReplyToMonoForum` (или `inputReplyToMessage.monoforum_peer_id`), `send_as` сервер ставит сам.
- Админам: `messages.getSavedDialogs` / `getSavedDialogsByID` с `parent_peer` (список тем как `monoForumDialog`), `getSavedHistory`, `readSavedHistory`, `deleteSavedHistory`, `markDialogUnread` и `getDialogUnreadMarks` с `parent_peer`, `channels.getMessageAuthor`; вместо `updateReadChannelInbox/Outbox` — `updateReadMonoForumInbox/Outbox`; у `updateDraftMessage`, `updateDialogUnreadMark` — `saved_peer_id`.
- Подписчик работает с монофорумом как с обычной супергруппой: обычные методы, обычные `updateReadChannelInbox/Outbox`.
- Ошибки со страниц методов: `STARS_AMOUNT_INVALID`, `CHANNEL_MONOFORUM_UNSUPPORTED`, `CHAT_NOT_MODIFIED` (`channels.updatePaidMessagesPrice`); `PARENT_PEER_INVALID`, `CHAT_ADMIN_REQUIRED` (`messages.readSavedHistory`); `REPLY_TO_MONOFORUM_PEER_INVALID`, `SUGGESTED_POST_PEER_INVALID`, `CHANNEL_MONOFORUM_UNSUPPORTED`, `CHAT_WRITE_FORBIDDEN` (`messages.sendMessage`); `CHANNEL_INVALID` (`channels.getMessageAuthor`).

### Клиенты (`~/pluma-work/reference-src-new`)
- **Кто админ монофорума, клиент решает по каналу, а не по монофоруму.** Android `ChatObject.java:1767-1797` (`canManageMonoForum` берёт чат по `linked_monoforum_id` и проверяет `ACTION_MANAGE_DIRECT`), tdesktop `data_channel.cpp:748-750` (`canAccessMonoforum`: создатель или `AdminRight::ManageDirect`) и `:369-390` (`setMonoforumLink`), iOS `PendingMessageManager.swift:1330` и `SyncCore_StandaloneAccountTransaction.swift:94` (`mainChannel.hasPermission(.manageDirect)`), Web K `appChatsManager.ts:538-546`. Значит, канал должен приходить вместе с монофорумом.
- **Связь двух сторон** клиенты берут из `chats` одного ответа: tdesktop `data_session.cpp:1388-1421` (`applyMonoforumLinkedId`; если второй стороны нет — запрашивает полный канал). Монофорум без `linked_monoforum_id` tdesktop считает выключенным (`MonoforumDisabled`, `data_channel.cpp:373-377`, запрет писать — `data_chat_participant_status.cpp:273-275`).
- **Имя и фото** монофорума клиенты показывают от канала: Android `DialogCell.java:2262-2264`, `ChatAvatarContainer.java:564`.
- **Включение.** tdesktop `edit_peer_info_box.cpp:323-353` (кнопка — при `canEditInformation`, `:1587-1588`; `CHAT_NOT_MODIFIED` считает успехом), Android `PostSuggestionsEditActivity.java:206-222` (`ChatEditActivity.java:925`: `ACTION_CHANGE_INFO`), оба применяют `Updates` ответа.
- **Отправка админом.** Android `SendMessagesHelper.java:12496-12528` (`reply_to.monoforum_peer_id`), tdesktop `data_histories.cpp:75`, Web K `appMessagesManager.ts:4986-4995`.
- **Список тем.** tdesktop `data_saved_messages.cpp:298-320` (`getSavedDialogs` с `parent_peer`, постранично по `offset_id` последнего сообщения страницы) и `:424-441` (тема без обычного последнего сообщения обрывает список); Android `MessagesController.java:21487-21490` (`getSavedDialogsByID`), `:14608-14612` (`readSavedHistory`).
- **Чтение всего монофорума админом** tdesktop раскладывает по темам (`history.cpp:3749-3790`).
- **Автор ответа.** tdesktop `history_item.cpp:2345-2350` (`canLookupMessageAuthor`: админ монофорума, сообщение от канала), Android `ChatActivity.java:31965` (только для не своих сообщений).
- **Опросы и чек-листы** в монофоруме клиенты не предлагают: Android `ChatObject.java:2217-2220`, tdesktop `data_peer.cpp:691-700`.

Не проверено по первоисточнику (в документации нет, код опирается только на прочитанное выше): какие `admin_rights` Telegram ставит самому монофоруму; остаётся ли `linked_monoforum_id` у какой-то из сторон после выключения; кому, кроме админов и подписчика, уходят обновления; как подписчик убирает монофорум из списка чатов.

## Решение

### 1. Хранение
- **Монофорум — строка `channels`** (`megagroup = 1`, `monoforum = 1`), создаётся в транзакции первого включения (`channelbox.Create`, id выше канала — порядок блокировок ADR 0034). `linked_monoforum_id` у обеих строк; `broadcast_messages_allowed` — у канала. Выключение оставляет монофорум и сообщения, повторное включение возвращает тот же.
- **`channel_messages.saved_peer_id`** — подписчик, в чьём чате сообщение (0 вне монофорума), индекс `(channel_id, saved_peer_id, id)`. Сообщение канала отличает `send_as_id` (канал), сообщение подписчика — 0; `sender_user_id` админа хранит, кто ответил.
- **`channel_monoforum_sublists`** `(channel_id, peer_id)` — чат подписчика с каналом: `top_message` (последнее не служебное — так клиенты строят список), `read_inbox_max_id` (докуда админы прочли подписчика, одна отметка на всех админов), `read_outbox_max_id` (докуда подписчик прочёл ответы), `unread_mark`.
- **`channel_monoforum_drafts`** `(user_id, channel_id, peer_id)` — черновик админа в чате подписчика.
- Миграция `migrate-20261114-monoforum.sql`, повторный запуск безопасен.

### 2. Участников у монофорума нет
В `channel_participants` строк монофорума нет. Кто пользователь в монофоруме, каждый раз читается из канала (`pkg/monoforum`):
- создатель канала и админ с `manage_direct_messages` — **админ личных сообщений**: видит все чаты, отвечает от имени канала;
- любой другой, кто видит канал (участник; не забаненный посторонний у публичного канала), — **подписчик**: видит и пишет только свой чат;
- остальным монофорум — `channelForbidden`.

Почему не зеркальные строки участников: права админа в канале меняют полтора десятка мест (`editAdmin`, выход, бан, передача владельца, удаление канала…), и пропущенное место оставило бы бывшему админу чужую переписку. Чтение из канала делает смену прав мгновенной и не требует синхронизации.

biz channel отдаёт для монофорума «собранную» строку участника (`monoforumViewers`): `RoleAdmin` с правами админа в канале либо участник; `ToChat` строит `channel` без `left`. Клиентам право на управление всё равно говорит канал; `admin_rights` монофорума нужны серверу (удаление, закреп) и повторяют права в канале.

### 3. Обе стороны ездят вместе
- `channel.getChannelListByIdList` добавляет канал после монофорума, если его не просили (так он попадает в диалоги, истории, разницу).
- Канал добавляют явно: ответ на отправку (`sentChats`), пуш sync, `updateChannel` из inbox, `channels.getFullChannel` (обе стороны — tdesktop запрашивает его за недостающей).
- `linked_monoforum_id` у обеих сторон отдаётся только пока личные сообщения включены, `broadcast_messages_allowed` — у канала.
- Имя и фото монофорума — от канала (читаются при каждой сборке объекта).

### 4. Что видит подписчик
Одно место — `viewer.sublist` в biz channel: история, сообщения по id, поиск, счётчики, последнее сообщение диалога и разница канала отдают подписчику только строки с `saved_peer_id = он`. В разнице (`channelFold.setSublist`) сообщения чужих чатов выпадают, списки id удалений и закрепов режутся до своих, обновления без сообщений не отдаются; `pts` при этом идёт до конца.
- **Следствие общей последовательности `pts`.** У монофорума один `pts`, как у любого канала. Подписчик получает пуш с `pts`, между которыми были чужие сообщения, видит разрыв и запрашивает `updates.getChannelDifference`, который отдаёт только его сообщения. Задержка — как у любого разрыва `pts` (сколько клиенты ждут перед запросом разницы, здесь не проверялось). При ≤100 пользователях это приемлемо; отдельная последовательность на чат противоречила бы схеме (один id монофорума).
- Админ ищет внутри одного чата: `messages.search`, `getSearchCounters`, `getSearchResultsCalendar` и `getSearchResultsPositions` с `saved_peer_id` (так tdesktop запрашивает медиа чата, `data_search_controller.cpp:182-190`) идут в `channel.search*` с `saved_peer_id`; в канале без монофорума такой поиск ничего не находит.
- Ветки ответов (`messages.getReplies`, `getDiscussionMessage`) в монофоруме не ведутся (`MSG_ID_INVALID`): ветка читалась бы поперёк чатов.

### 5. Отправка (`msg`, `SendChannel` → `routeToSublist`)
- Подписчик пишет в свой чат от себя; чужой `monoforum_peer_id` игнорируется.
- Админ пишет в чат, названный `reply_to.monoforum_peer_id` (bff кладёт его в `saved_peer_id` исходящего сообщения), либо в чат сообщения, на которое отвечает. Сообщение — от канала (`from_id = peerChannel`, `send_as_id`), `send_as` запроса игнорируется. Чата нет — `REPLY_TO_MONOFORUM_PEER_INVALID`: канал отвечает тем, кто написал, первым не пишет (на подтверждение).
- Ответ остаётся ответом только на сообщение того же чата. Упоминания строк `channel_message_mentions` не создают.
- Опрос и чек-лист — `CHANNEL_MONOFORUM_UNSUPPORTED`. Личные сообщения выключены — `CHAT_WRITE_FORBIDDEN`.
- Вне монофорума `saved_peer_id` у сообщений каналов очищается.
- Тем, у кого нет диалога с монофорумом (админы при первом сообщении; подписчик, убравший чат, при ответе), уходит `inbox.channelInvitedToInbox`: получатель сам пишет свою строку `dialogs` (ADR 0034) и получает `updateChannel` с обеими сторонами.

### 6. Рассылка
`sync.pushChannelUpdates` не меняется. Получателей монофорума отдаёт `channel.getMemberIdList`: админы личных сообщений и подписчики, у которых есть чат. Sync и сервис уведомлений для подписчика оставляют только обновления его чата (`channel.SublistView`): сообщение — по `saved_peer_id`, удаление, закреп и реакцию — по чатам сообщений (`channel.getMessageSublists`, удалённые строки остаются). Обновления остальных видов получают только админы.

### 7. Чтение
- **Подписчик** (`channels.readHistory`, `messages.readHistory`): отметка — его строка `dialogs`, своим устройствам `updateReadChannelInbox` с `seq`; `read_outbox_max_id` чата растёт, админам — `updateReadMonoForumOutbox`.
- **Админ** (`messages.readSavedHistory`): `read_inbox_max_id` чата, одна на всех админов; остальным админам и своим другим устройствам — `updateReadMonoForumInbox`, подписчику — `updateReadChannelOutbox`, если прочитаны его сообщения. `channels.readHistory` админа читает все чаты до `max_id`.
- Обновления чтения монофорума уходят пушем без `seq`: отметки восстанавливаются из `getSavedDialogs` и диалога.
- **Диалог админа** считается из чатов: непрочитано — сообщения подписчиков выше отметок, `read_inbox_max_id` — наименьшая отметка среди чатов с непрочитанным. **Диалог подписчика** — его чат.

### 8. Удаление
- Подписчик удаляет свои сообщения в своём чате; ответ канала — `MESSAGE_DELETE_FORBIDDEN`; чужие id пропускаются.
- Админ личных сообщений удаляет любое сообщение монофорума и чат целиком (`messages.deleteSavedHistory` с `parent_peer`, порциями по 100, `offset` — остаток).
- `top_message` чата пересчитывается; опустевший чат выходит из списка.
- Удаление канала помечает удалённым и монофорум; он уходит из диалогов всех, у кого есть: у удаляющего сразу (`updateChannel` с `seq`), у остальных — событием `inbox.channelDeletedToInbox` по id монофорума.

### 9. Цена
Сообщения бесплатны: платежей нет, `stars_paid_messages_available = false` в app config. `send_paid_messages_stars > 0` — `STARS_AMOUNT_INVALID` (страница метода: «The specified amount in stars is invalid»). В супергруппе метод задаёт только цену, поэтому там всегда `CHAT_NOT_MODIFIED`. Остальные методы `RPCPaidMessage` — `ERR_ENTERPRISE_IS_BLOCKED`.

### 10. Подписчик не видит других подписчиков и админов
- **Набор текста** (`messages.setTyping`) подписчика уходит только админам личных сообщений. Набор админа не уходит никому: он назвал бы подписчику админа за каналом, а в `messages.setTyping` слоя 228 нет `saved_peer_id`, по которому можно было бы найти чат; tdesktop из чата подписчика набор и не шлёт (`history_view_chat_section.cpp:676-679`).
- `messages.getOnlines` подписчику считает только его самого (Android монофорум и не опрашивает, `MessagesController.java:16588`); `channels.getParticipant` о других — только админам личных сообщений (`CHAT_ADMIN_REQUIRED`, как в канале).
- `default_banned_rights` монофорума запрещают закреп, смену информации и приглашения: иначе клиенты предлагают подписчику «Закрепить», которое сервер отклоняет (tdesktop `data_peer.cpp:669-682, 1748-1790`, `data_channel.cpp:794-797`; Android `ChatObject.java:1918-1947, 2287-2288`). Админов клиенты пускают по их `admin_rights`.

### 11. Убрать чат с каналом из списка
В монофорум не вступают, поэтому `channels.leaveChannel` монофорума только убирает его из диалогов пользователя (`dialogs.deleted = 1`); кем пользователь был в монофоруме, тем и остаётся. Так его убирает tdesktop: пункт «Покинуть группу» — единственный для монофорума (`window_peer_menu.cpp:882-911`), клиент убирает диалог сам и зовёт `channels.leaveChannel` (`apiwrap.cpp:2287-2291, 2310-2311`). В ответе нет `updateChannel`: по нему tdesktop для канала, в котором пользователь остаётся, загрузил бы диалог обратно (`api_updates.cpp:2537-2558`). Следующее сообщение чата (ответ канала или своё) возвращает диалог. Каким вызовом это делают Android, iOS и Web — не проверено.

### 12. Что не сделано в этой задаче
- **Предложенные посты** (`suggested_post`, `messages.toggleSuggestedPostApproval`). Без звёзд они возможны (цена необязательна), но одобрение должно публиковать пост в канал (в том числе отложенно и от имени админа, когда одобряет подписчик) — это отдельная задача. Пока отправка с `suggested_post` в монофорум отвечает `ERR_ENTERPRISE_IS_BLOCKED`, вне монофорума — `SUGGESTED_POST_PEER_INVALID`.
- `messages.unpinAllMessages` и `getUnreadReactions`/`readReactions` с `saved_peer_id`.
- `updateChannelReadMessagesContents.saved_peer_id` не заполняется (tdesktop и Android находят сообщение по id: `api_updates.cpp:1763-1789`, `MessagesController.java:18815-18826`).
- «Очистить историю у себя» в монофоруме (tdesktop его не предлагает, `window_peer_menu.cpp:863`); запись в «Недавние действия» о включении.

## Последствия
- Новый сервис bff `RPCPaidMessage` (в `channels`), маршрут в трёх `session.yaml`.
- Все сервисы перезапускаются вместе: у biz channel новые RPC и поля `ImmutableChannel`.
- Монофорум расходует `pts` и id сообщений одного канала на все чаты; окно журнала (100000 `pts`) общее.
- Сообщения слоя 228: `monoforum`, `linked_monoforum_id`, `saved_peer_id`, `monoForumDialog`, `updateReadMonoForum*` в схеме 228 есть, отдельной конвертации не нужно.

## Что отвергнуто
- **Строки участников монофорума, зеркалящие админов канала** — см. п. 2.
- **Список подлистов в `sync.pushChannelUpdates`** (поле с получателями): каждый, кто шлёт обновление канала, должен был бы его заполнять; пропуск — утечка. Поиск чатов по id сообщений в одном месте (sync) безопасен по умолчанию.
- **Хранить чаты подписчиков в ящиках пользователей** (как «Избранное»): у монофорума id канала, общая история для всех админов и `pts` канала — это ящик канала.
