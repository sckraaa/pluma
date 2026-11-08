# 060. Форумы и темы в супергруппах

- Статус: done (сервер; проверка в живых клиентах — позже, владельцем)
- Зависит от: 035–048 (каналы, ADR 0034; обсуждения, `reply_top_id`, `channel_thread_reads`), T14 из `docs/tasks/035-research/server.md`
- ADR: новых решений уровня ADR нет; дополняет ADR 0034

## Цель
Супергруппа включается как форум, темы создаются, редактируются, закрепляются, закрываются и удаляются, сообщения ходят по темам, у каждой темы своя история, поиск, непрочитанные и закреплённые. Официальные клиенты открывают такой форум без ошибок.

## Методы слоя 228 (`third_party/teamgram-proto`, `pkg/catalog/tl/api.tl`)
- `messages.*` — актуальные: `getForumTopics#3ba47bff`, `getForumTopicsByID#af0a4a08`, `createForumTopic#2f98c3d5`, `editForumTopic#cecc1134`, `updatePinnedForumTopic#175df251`, `reorderPinnedForumTopics#e7841f0`, `deleteTopicHistory#d2816f10`. Их шлют все клиенты: Android `TopicsController.java:121,528,725,747,865,902`, tdesktop `data_forum.cpp:173,449`, `data_histories.cpp:1079`, iOS `ForumChannels.swift:229,336,523,1141`, Web K `appMessagesManager.ts:4938,9276,9300,9426`.
- `channels.*` — те же методы прежних слоёв (`createForumTopic#f40c0224`, `getForumTopics#de560d1`, `getForumTopicsByID#b0831eb9`, `editForumTopic#f4dfa185`, `updatePinnedForumTopic#6c2d9026`, `deleteTopicHistory#34435f2d`, `reorderPinnedForumTopics#2950a18f`): схема их ещё содержит, обслуживаются той же логикой.
- Только `channels.*`: `toggleForum#3ff75734` (и старый `#a4298b29` без `tabs`), `toggleViewForumAsMessages#9738bb15`.
- Сервис — `RPCForums` (регистрация в `server.go`, маршрут в трёх `session.yaml`).

## Что выяснено у клиентов и в документации
- **Топик = id сервисного сообщения.** `messageActionTopicCreate`; General — id 1, неудаляем, в General у сообщений нет `top_msg_id` (core.telegram.org/api/forum). Android `MessageObject.getTopicId` (`MessageObject.java:499-531`): `reply_to == null || !forum_topic → 1`; не сервисное — `reply_to_top_id`, иначе `reply_to_msg_id`; сервисное (кроме pin) — наоборот. tdesktop `HistoryItem::topicRootId` (`history_item.cpp:4180-4193`) то же: нет `topicPost` → General.
- **История темы — `messages.getReplies(msg_id = тема)`** у всех клиентов, General — `msg_id = 1` (Android `MessagesController.java:11596`, `TopicsController.java:1334`; tdesktop `data_replies_list.cpp:538,595,639`; iOS `Holes.swift:512`; Web K `appMessagesManager.ts:12443`). Корневое сообщение входит в историю: tdesktop сам вставляет только корень, не принадлежащий теме (`data_replies_list.cpp:306-311`; `topicRootId` темы — её id, `history_item.cpp:4187-4190`), значит корень темы приходит от сервера.
- **Чтение темы** — `messages.readDiscussion(тема, max)` (Android `MessagesController.java:14501-14508`, tdesktop `data_replies_list.cpp:1008`, iOS `ApplyMaxReadIndexInteractively.swift:183`, Web K `appMessagesManager.ts:8398`); `updateReadChannelDiscussionInbox`/`Outbox` применяются клиентами по теме (Android `MessagesController.java:19610-19622`, tdesktop `api_updates.cpp:2508-2538`).
- **Счётчик непрочитанных в теме** важнее dialog-уровня: tdesktop считает unread форума по темам (`history.cpp:2732-2762`), Android — `countsTmp` по темам (`TopicsController.java:657-661`).
- **Создание.** tdesktop применяет ответ по `random_id` (`data_histories.cpp:1079-1095`), iOS берёт id темы из `updateNewChannelMessage` ответа (`ForumChannels.swift:229-262`). Сообщение темы у получателя: tdesktop создаёт тему по сервисному сообщению, iOS запрашивает `getForumTopicsByID` (`AccountStateManagementUtils.swift:2229`), Web K строит локально (`appMessagesManager.ts:9518-9529`).
- **Список тем.** Android использует `topics[]`, `messages[]` (`top_message` и корень, `TopicsController.java:278-297`) и `count`; tdesktop считает конец списка по `count` (`data_forum.cpp:181-190`), ошибка `CHANNEL_FORUM_MISSING` снимает форум (`data_forum.cpp:197-200`). `forumTopicDeleted` — через `getForumTopicsByID` (tdesktop `data_forum.cpp:405`, Android `TopicsController.java:270-276`). Смещение — последняя тема страницы (core.telegram.org/method/messages.getForumTopics, tdesktop `_offset`, Web K `appMessagesManager.ts:4923-4928`).
- **Права.** Создание — `canUserDoAction(ACTION_MANAGE_TOPICS)` (Android `ChatObject.java:2287`), редактирование и закрытие своей темы — `my || canManageTopics` (tdesktop `data_forum_topic.cpp:317-336`, Android `ChatObject.java:2295`), закрепление — только `canManageTopics` (tdesktop `:338-340`). Удаление: право удалять чужое, либо своя тема, где только свои сообщения и их не больше 10 (`data_forum_topic.cpp:321-332`, `data_replies_list.cpp:30,1028-1047`). Включает форум только владелец (core.telegram.org/api/forum).
- **Закрытая тема.** Писать может только автор темы или управляющий темами (tdesktop `data_peer_values.cpp:207`, ошибка `TOPIC_CLOSED` — `apiwrap.cpp:626`). Скрытый General клиент считает закрытым (Android `TopicsController.java:832-842`).
- **Настройки уведомлений темы.** `forumTopic.notify_settings` обязателен и применяется клиентом (tdesktop `data_forum_topic.cpp:413`); `account.getNotifySettings(inputNotifyForumTopic)` клиенты могут запросить (tdesktop `apiwrap.cpp:1975`), без ответа мешает открытию не больше, чем ошибка запроса (не проверено).
- **`topics` в `messages.channelMessages`** клиенты читают, если есть: tdesktop `apiwrap.cpp:3464`, `api_messages_search.cpp:176`, iOS `Holes.swift:268`.
- **Лимит закреплённых** — `topics_pinned_limit`, 5 (core.telegram.org/api/config); у Android запас по умолчанию 3, берётся из конфига (`MessagesController.java:1676,3750-3755`).
- **Ошибки Android** `PINNED_TOO_MUCH`, `PINNED_TOPIC_NOT_MODIFIED` (`TopicsController.java:882-897`).
- **Открытие форума** клиенты определяют только по флагу `channel.forum` (Android `DialogsActivity.java:7964`, `ChatObject.isForum`; tdesktop `data_forum.cpp`, флаг канала; iOS `TelegramChannelFlags.isForum`; Web K `appChatsManager.ts:447`). Преобразование `toggleForum` приходит как `updateChannel` с перечитыванием канала.

## Решения
- Сообщение знает тему по колонке `channel_messages.topic_id` (0 — General и всё вне форума). При включении форума ни одна строка не переписывается: всё старое — General. Отдельно от `reply_top_id`, чтобы старые ветки ответов в супергруппе не смешивались с темами и не терялись при выключении форума.
- Чтение темы — строка в `channel_thread_reads` (`top_id` = тема, для General 1), как потоки обсуждений. Dialog-уровень `read_inbox_max_id` в форуме работает как нижняя граница для всех тем (`GREATEST`): вступление и `channels.readHistory` читают всё сразу. Поэтому отправитель в форуме двигает не `read_inbox_max_id` диалога, а чтение своей темы (`channelbox.ForumMoved`), иначе своё сообщение прочитывало бы все темы.
- `top_message` темы в строке — ключ сортировки списка, актуальный `top_message` ответа считается по сообщениям; `channels.deleteMessages` обновляет сортировку.
- `channels.deleteMessages` не удаляет корень темы: он уходит только с темой (`deleteTopicHistory`), как ожидают клиенты (core.telegram.org/api/forum: удаление темы видно по корню среди удалённых).
- `deleteTopicHistory` — порциями по 100 с конца, корень последним; при `offset > 0` клиент повторяет (tdesktop `window_peer_menu.cpp:2193-2200`, Android `TopicsController.java:733-741`).
- Форум нельзя сделать группой обсуждения и наоборот: `CHAT_DISCUSSION_UNALLOWED` (core.telegram.org/method/channels.toggleForum) и `MEGAGROUP_ID_INVALID` в `setDiscussionGroup`.

## Область и что не сделано
Сделано всё из постановки, кроме перечисленного ниже как открытые вопросы.

## Решения на подтверждение
- Заголовок General — `"General"` как `forum_topic.title` (Telegram шлёт заголовок сервером; ключа локализации для него в TL нет). Клиент показывает его как есть.
- Нет ошибки на недопустимый `icon_color` в документации Telegram; отвечаем `INPUT_REQUEST_INVALID`. Не проверено.
- `TOPIC_HIDE_SEPARATELY` в схеме ошибок нет: совмещение `hidden` с другими полями — `TOPIC_CLOSE_SEPARATELY` (как у `closed`, core.telegram.org). Не проверено.
- Скрытие General закрывает его, показ открывает (Android так считает локально); сервер Telegram не проверен.

## Открытые вопросы (не сделано)
- Счётчики непрочитанных реакций и голосов в теме (`unread_reactions_count`, `unread_poll_votes_count`) — 0: диалог их тоже не отдаёт; `messages.getUnreadReactions` с `top_msg_id` не фильтрует по теме.
- `messages.getSearchCounters` и `messages.setTyping` с `top_msg_id` тему не учитывают.
- Настройки уведомлений темы не хранятся: `account.getNotifySettings(inputNotifyForumTopic)` отвечает настройками по умолчанию, `account.updateNotifySettings` для темы — по-прежнему `PEER_ID_INVALID`.
- Черновики по темам не разведены: `messages.saveDraft` с темой пишет черновик диалога.
- `upsertDialog` (вступление, правка названия) сдвигает `read_inbox_max_id` действующего в форуме (мелкая неточность для него одного).
- Ветка `deleteHistory` (другая задача) должна при массовом удалении обновить `top_message` тем (`refreshTopicTops`) или полагаться на сортировку по старому значению — отображаемый `top_message` считается честно.

## Результат
Форумы и темы работают на сервере: `channels.toggleForum` (с `tabs`, только владелец, запрет для групп обсуждения), `channels.toggleViewForumAsMessages` (по аккаунту, `updateChannelViewForumAsMessages` со `seq`), `createChannel` с `forum`, создание, правка, закрытие, скрытие General, закрепление и порядок тем, удаление темы порциями, `getForumTopics`/`getForumTopicsByID`, `messages.*` и `channels.*`. General — id 1, существующие сообщения попадают в него автоматически. Сообщения ходят по темам: `top_msg_id` и ответ внутри темы, правильные `forum_topic`/`reply_to_top_id`, закрытая тема — `TOPIC_CLOSED`. По темам работают история (`getReplies`, с корневым сообщением), поиск, чтение (`readDiscussion`, `updateReadChannelDiscussionInbox`/`Outbox`), непрочитанные в теме и в диалоге, упоминания, закреплённые (`unpinAllMessages` с `top_msg_id`). Новая таблица `channel_forum_topics`, колонки `channel_messages.topic_id`, `channels.forum_tabs`, `dialogs.view_forum_as_messages` — миграция `migrate-20261108-forum-topics.sql`, применяется службой миграций; лимит закреплённых — `topics_pinned_limit = 5`, как уже отдаёт `app_config.json`.

- **Biz channel:** `internal/dao/forum.go`, `internal/core/channel.forum_handler.go`, 12 новых RPC, поле `forum` у `channel.createChannel` (TL-строка и CRC изменены), `forum_tabs` у `immutableChannel`.
- **Отправка:** `routeToTopic` в `msg/internal/dao/channel_send.go`, сервисное сообщение закрепления в теме закреплённого.
- **Bff:** `RPCForums` в `app/bff/channels` (маршрут в трёх `session.yaml`), `top_msg_id` в ответах, поиске, упоминаниях, `unpinAllMessages`, пересылке.
- **Проверка:** на ветке — `go build ./...`, `go vet -tags integration,e2e` и `go test` затронутых пакетов; e2e `TestChannel*`, включая `TestChannelForum`, прошли на стенде; `-race`, `make lint`, интеграционные тесты и полный e2e — в общем прогоне объединённой партии веток.
- **Не проверено:** поведение в живых клиентах (Android/iOS/Desktop/Web) — проверяет владелец после выкатки.
