# 048. Каналы: группа обсуждения и комментарии

- Статус: todo
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
