# 048. Каналы: группа обсуждения и комментарии

- Статус: in progress (2026-11-07, сервер готов; ждёт проверки в клиентах)
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
- **Не сделано:** правка/удаление поста не меняют копию; `updateReadChannelDiscussionInbox`/`Outbox` не рассылаются; список групп без базовых групп; проверка в клиентах.
- **Исправлено по отчёту владельца (2026-11-07):** группа обсуждения была недоступна подписчикам канала: частная группа читалась только своими участниками. По Telegram комментаторам вступать не нужно, пока группа не требует (`join_to_send` «can be false only for discussion groups», core.telegram.org/constructor/channel; `channels.toggleJoinToSend`). Теперь участник канала, не вступивший в группу, видит её как канал (состояние зрителя `StateLinked`, не хранится), читает историю и ветки и пишет в неё как гость с общими правами группы; группа не попадает в его диалоги; гость не закрепляет; исключённый из группы остаётся вне. e2e `TestChannelComments` проверяет это за подписчика, не вступившего в группу.
