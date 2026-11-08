# 048. Каналы: группа обсуждения и комментарии

- Статус: in progress (2026-11-08, сервер и остатки готовы; ждёт проверки в клиентах)
- Зависит от: 041, 046
- ADR: 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` §1.1, §1.5, §2.6, задача T13; `clients.md` §4 п. 9; `plan.md` §3, задача «047» плана).

## Цель
К каналу можно привязать группу обсуждения; под постами появляется кнопка комментариев, счётчик растёт, комментарии читаются и отмечаются прочитанными.

## Что известно
- Маршрут `RPCMessageThreads` закомментирован во всех трёх `session.yaml` (`teamgramd/etc2/session.yaml:104`, `teamgramd/etc/session.yaml:104`, `app/interface/session/etc/session.yaml:84`, проверено, `plan.md` §1 п. 1). Этот сервис несёт `messages.getReplies`, `getDiscussionMessage`, `readDiscussion`; в `api-coverage.md` они `missing` (618, 565, 654).
- `channels.setDiscussionGroup` (api.tl:2794) ставит `linked_chat_id` с обеих сторон; каждый пост трансляции автоматически пересылается в группу; `getDiscussionMessage`, `getReplies`, `readDiscussion` работают по `reply_to_top_id`; счётчики `replies` хранятся на посте (`server.md` §2.6).
- Клиенты: iOS `ManageChannelDiscussionGroup.swift` (`getGroupsForDiscussion`, `setDiscussionGroup`); `linked_chat_id`; `replies` в посте и в `getMessagesViews` (Android `MessagesController.java:10579-10630`, 035) (`clients.md` §4 п. 9).
- В `test/e2e` уже есть тесты ответов (`replies`) для обычных чатов (`server.md` §1.4) — образец.
- Регистрация нового сервиса bff — по правилу проекта (`server.go` и три `session.yaml`); необслуживаемые методы `RPCMessageThreads` отвечают `ErrEnterpriseIsBlocked`.

- **Отчёт владельца 2026-11-07:** комментарии в каналах не работают. Как в Telegram (core.telegram.org/api/discussion, прочитано 2026-11-07): комментарии — это ветка ответов на автоматически пересланный пост в **привязанной супергруппе обсуждения**. Без привязанной группы кнопки комментариев нет. Админ привязывает свою супергруппу (`channels.getGroupsForDiscussion` → `channels.setDiscussionGroup`; у группы должна быть открыта история, `channels.togglePreHistoryHidden`); клиенты предлагают и создать новую группу для этого (создание — обычный `channels.createChannel`, затем привязка; по исходникам клиентов не сверено). Новый пост пересылается в группу и закрепляется там; у поста — `messageReplies` (счётчик, `channel_id` группы, последние комментаторы); `messages.getDiscussionMessage` отдаёт начало ветки; удаление пересланного сообщения из группы выключает комментарии к посту. `linked_chat_id` — в `channelFull`.

## Область
- `channels.setDiscussionGroup`, `channels.getGroupsForDiscussion`.
- Автопересылка постов в привязанную группу (одна логическая операция с публикацией; при сбое шаг не теряется — через outbox).
- `messages.getDiscussionMessage`, `getReplies`, `readDiscussion`; счётчики `replies` в посте и в `getMessagesViews`.
- Маршрут `RPCMessageThreads` и регистрация модуля.
- Не трогаем: форумы и топики (отложены, вопрос владельцу в 035).

## Критерии приёмки
- Модульные тесты: сборка `replies`, `getDiscussionMessage`.
- Интеграционные тесты через `pkg/testinfra` (MySQL, Kafka): пост доходит в группу ровно один раз даже при повторе события; счётчик `replies` верен при параллельных комментариях.
- e2e через `pkg/mtclient`: пост появляется в группе, счётчик комментариев растёт, `readDiscussion` двигает прочтение; тест падает на текущем коде.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: кнопка комментариев под постами и растущий счётчик в iOS, Android, Desktop (CachyOS) и Web K.

## Результат
### Сервер (2026-11-07, ветка `task/048-discussion`, по отчёту владельца)
- **Привязка** (`channels.setDiscussionGroup`, `getGroupsForDiscussion`): супергруппа привязывается к каналу-трансляции с обеих сторон (`linked_chat_id`); в одной транзакции блокируются все затронутые каналы по возрастанию id; права — `change_info` в обоих; прежние привязки снимаются. Ошибки — как у Telegram: `BROADCAST_ID_INVALID`, `MEGAGROUP_ID_INVALID`, `MEGAGROUP_PREHISTORY_HIDDEN`, `LINK_NOT_MODIFIED`. Участники обоих перечитывают каналы. В списке для привязки — только супергруппы (базовую клиент сначала переводит, 047); базовые группы в списке не отдаются.
- **Копия поста:** пост канала с группой получает копию в группе в той же транзакции (оба канала заблокированы по возрастанию id): от канала, `fwd_from` с `channel_post` и `saved_from_*`, текст и медиа поста. Связь — `channel_discussion_posts` (пост ↔ копия, по одной). Участникам группы — пуш. Правка и удаление поста копию пока не трогают.
- **Ветки:** ответ в супергруппе попадает в ветку сообщения, на которое отвечает (`channel_messages.reply_top_id`, миграция `migrate-20261107-channel-discussion.sql`); комментарии поста — ветка его копии. `messages.getDiscussionMessage` (по посту или сообщению группы — копия, последний ответ, прочитано, непрочитанные), `getReplies` (страница ветки), `readDiscussion` (`channel_thread_reads`). Сервис `RPCMessageThreads` зарегистрирован на модуле `channels` и маршрутизирован в трёх `session.yaml`; `contacts.blockFromReplies` — `ERR_ENTERPRISE_IS_BLOCKED`.
- **Счётчик:** у постов — `messageReplies` (`comments`, число, группа, последний ответ, до трёх авторов, `replies_pts` группы) в истории, поиске и `messages.getMessagesViews` (его клиенты опрашивают; просмотры и пересылки там пока не считаются — 046). `channelFull.linked_chat_id` и сама связанная группа в `chats`.
- **Проверка:** e2e `TestChannelComments` (подписчик не привязывает, группа в списке, привязка, повтор — `LINK_NOT_MODIFIED`, копия поста, комментарий, счётчик и автор в `getMessagesViews`, ветка, непрочитанное до и после `readDiscussion`).
- **Не сделано** (закрыто ниже, кроме проверки в клиентах): правка/удаление поста не меняли копию; `updateReadChannelDiscussionInbox`/`Outbox` не рассылались; в списке не было базовых групп; проверка в клиентах.
- **Исправлено по отчёту владельца (2026-11-07):** группа обсуждения была недоступна подписчикам канала: частная группа читалась только своими участниками. По Telegram комментаторам вступать не нужно, пока группа не требует (`join_to_send` «can be false only for discussion groups», core.telegram.org/constructor/channel; `channels.toggleJoinToSend`). Теперь участник канала, не вступивший в группу, видит её как канал (состояние зрителя `StateLinked`, не хранится), читает историю и ветки и пишет в неё как гость с общими правами группы; группа не попадает в его диалоги; гость не закрепляет; исключённый из группы остаётся вне. e2e `TestChannelComments` проверяет это за подписчика, не вступившего в группу.

### Остатки (2026-11-08, ветка `task/048-discussion-leftovers`)
Правка поста теперь правит и его копию, удаление поста удаляет копию, удаление копии выключает комментарии поста; прочтение комментариев рассылает `updateReadChannelDiscussionInbox`/`Outbox`; в список для привязки попали базовые группы и супергруппы со скрытой историей; ветка с удалённым началом открывается. В клиентах ещё не проверено: это отдельный шаг.
- **Правка.** `EditChannel` (msg) в одной транзакции меняет пост и копию: оба канала заблокированы по возрастанию id, у копии — своя запись журнала с pts группы (`updateEditChannelMessage`). Копия получает то же, что пост после правки (текст, разметка, медиа, `edit_date`).
- **Удаление поста.** `Delete` (biz) удаляет и копию (`updateDeleteChannelMessages` с pts группы, связь `channel_discussion_posts` снимается, `top_message` группы сдвигается). Комментарии остаются в группе.
- **Удаление копии в группе** выключает комментарии поста, как в core.telegram.org/api/discussion («the comment section of a particular post can be disabled by removing the auto-forwarded channel post message»): у поста снимается `replies`, записью журнала с pts канала и без `edit_date` (`updateEditChannelMessage`; tdesktop `history_item.cpp:2484-2488` при отсутствии `replies` делает `clearReplies`). Android на такой пост показывает `ChannelPostDeleted` (`ChatActivity.java:35593`).
- **Открытие ветки.** Пост без копии — `MSG_ID_INVALID` (раньше пост канала без копии открывал ветку «сам из себя»). Сообщение или начало ветки в группе могут быть удалены: `getDiscussionMessage` отвечает без `messages` с `max_id` и счётчиками (tdesktop `window_session_controller.cpp:1265-1273` открывает ветку по id корня), `getReplies` и `readDiscussion` работают по удалённому корню.
- **Прочтение.** `readDiscussion` доводит не дальше последнего сообщения ветки; прочитавшему — `updateReadChannelDiscussionInbox` с seq на все устройства, с `broadcast_id`/`broadcast_post` для комментариев поста (схема; tdesktop `api_updates.cpp:2508-2538`, Android `MessagesController.java:20522-20533`, iOS `AccountStateManagementUtils.swift:1295-1305`); авторам прочитанных ответов — `updateReadChannelDiscussionOutbox` с их последним прочитанным сообщением (без pts и seq, как `updateReadChannelOutbox`). `getDiscussionMessage` отдаёт `read_outbox_max_id`. Обновление добавлено в `userbox.SeqUpdate` и свёртку журнала; индекс — миграция `migrate-20261108-channel-thread-reads.sql`, применяется сервисом migrate.
- **Базовые группы.** `channels.getGroupsForDiscussion` отдаёт и базовые группы, созданные пользователем (только создатель может `messages.migrateChat`), живые и не перенесённые: core.telegram.org/method/channels.getGroupsForDiscussion, все клиенты берут `chat` из списка и переводят её сами перед `setDiscussionGroup` (Android `ChatLinkActivity.java:578-585`, tdesktop `edit_discussion_link_box.cpp:199`, iOS `ChannelDiscussionGroupSetupController.swift:365`, Web `appChatsManager.ts:1241`). Супергруппа со скрытой историей тоже в списке: страница метода требует открыть историю перед привязкой, Android и Web делают это сами, привязка без этого — `MEGAGROUP_PREHISTORY_HIDDEN`.

**Решения, требующие подтверждения:**
- Что происходит с копией при правке и удалении поста, официально нигде не описано: `core.telegram.org/api/discussion` об этом молчит, в исходниках клиентов логики нет, в поиске прямых источников нет. Сделано так, как задано в постановке: правка переносится, удаление удаляет копию, комментарии остаются. Не проверено на настоящем Telegram (нужна проверка через Web K на реальном канале).
- Без подтверждения источником: у копии остаётся тот же `edit_date`, что у поста (`edit_hide` не ставится).

**Вне задачи, найдено:** push копии нового поста идёт с ключом отправителя (`channel_send.go`, `EmitChannelPush` для `disc.Copies`), поэтому устройство автора не узнаёт о копии до `getChannelDifference`; пересылка правок и удалений этого не повторяет. Копия в группе не закрепляется, хотя по `docs/tasks/048` («Что известно») должна. `messageReplies.read_max_id` в истории не заполняется (нет зрителя в `withReplies`); клиенты узнают его из `updateReadChannelDiscussionInbox` и `getDiscussionMessage`.
