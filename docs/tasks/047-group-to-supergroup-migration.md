# 047. Переход базовой группы в супергруппу

- Статус: in progress (2026-11-07, сервер готов; ждёт проверки в клиентах)
- Зависит от: 041, 042
- ADR: 0011, 0012, 0021, 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` §1.1, §2.1–2.2, §2.7, задача T12; `plan.md` §1 п. 7, 9, §2 U3, U4, U6, U8, §3, задача «046» плана).

## Цель
«Преобразовать в супергруппу» работает во всех четырёх клиентах, как и действия, для которых клиент сам переводит группу в супергруппу (ROADMAP, этап 1: сейчас они кончаются ошибкой); участники сохраняют общую историю; повтор после сбоя не создаёт второй канал.

## Что известно
- `messages.migrateChat` (`app/bff/chats/internal/core/messages.migrateChat_handler.go:27`) отвечает `ErrEnterpriseIsBlocked`; `api-coverage.md:651` — `blocked`.
- Полуготовый помощник `chat.migratedToChannel` (`app/service/biz/chat/internal/core/chat.migratedToChannel_handler.go`): в одной транзакции ставит `chats.migrated_to_id/access_hash` и участников в `ChatMemberStateMigrated`; никто его не вызывает; результат `UpdateStateByChatIdTx` не проверяется (`:42`, проглоченная ошибка, проверено, `plan.md` §1 п. 7).
- **Telegram** (core.telegram.org/api/channel): ответ содержит старый `chat` с `migrated_to` и новый мегагрупповой `channel`; `channelFull` несёт `migrated_from_chat_id`, `migrated_from_max_id`; старым участникам — `messageActionChatMigrateTo` (api.tl:156); канал начинается с `messageActionChannelMigrateFrom` (api.tl:157); новые сообщения идут в супергруппу, клиенты склеивают обе истории.
- **iOS** `_internal_convertGroupToSupergroup`: `messages.migrateChat` с таймаутом 5 с, разбирает `CHANNELS_TOO_MUCH`, новый пир берёт из первого сообщения в `updates.messages`, чей пир не старый чат; без такого — `.generic` (`TelegramEngine/Peers/ConvertGroupToSupergroup.swift:13-29`, проверено, `plan.md` §1 п. 9). Ответ должен содержать служебное сообщение канала и прийти за 5 с.
- Разбор ответа (U8, 035): Android ищет в `chats` старую группу с `migrated_to` и сам канал (`MessagesController.java:15103-15122`); tdesktop берёт `migrated_to` из `chats` после `applyUpdates`, затем `getFullChannel` (`apiwrap.cpp:1387-1394`, `data/data_session.cpp:889-896`); Web K ищет `updateChannel` в `updates`, без него — TypeError (`appChatsManager.ts:784-786`). Состав ответа для всех четырёх — ADR 0034, п. 10.
- Переход по действию (U6, 035): клиенты сами зовут `migrateChat`, а затем метод канала — для публичного имени, видимости истории, прав админа не по умолчанию или звания, ограничения участника, медленного режима, привязки обсуждения, форума, заявок на вступление, передачи владения, импорта истории (строки всех четырёх клиентов — бриф 035, U6). Серверного автоперехода не нужно; по числу участников клиенты не переводят.
- Права (U4, ADR 0034, п. 10): админы группы — админы канала с `{other, change_info, delete_messages, ban_users, invite_users, pin_messages, manage_call, manage_ranks}` (tdesktop `data/data_chat.cpp:52-63`, Android `ChatRightsEditActivity.java:1089-1091`; у Telegram не проверено). Отправка в перенесённую группу — `CHAT_WRITE_FORBIDDEN` 403 (U3: `CHAT_MIGRATED` нет ни в документации, ни в клиентах; имя Telegram не проверено). Ошибки метода — `CHANNELS_TOO_MUCH`, `CHAT_ADMIN_REQUIRED`, `CHAT_ID_INVALID`, `PEER_ID_INVALID` (core.telegram.org/method/messages.migrateChat).
- **Порядок шагов** (`server.md` §2.7): под транзакцией чата — проверка, что вызывающий создатель, чат не деактивирован и не перенесён; строка `channels` (`megagroup`, `migrated_from_chat_id`, `migrated_from_max_id` = последнее сообщение чата, название, фото, описание, `default_banned_rights`); участники (создатель → создатель, админы — по U4); сообщение 1 `messageActionChannelMigrateFrom`; в ящике каждого участника — `messageActionChatMigrateTo`, `updateChat` (`seq`) и пометка диалога. Канал и ответ вызывающему — первыми, участники — через события outbox, как в ADR 0021; ключ идемпотентности — `chat_id`, повтор возвращает существующий канал.
- Ссылки-приглашения переносятся (`chat_invites.migrated_to_id`).

## Область
- `messages.migrateChat` (одна транзакция: `userbox.Run(создатель)` → `chats` → новый `channels`, ADR 0034, п. 10).
- Исправить `chat.migratedToChannel:42`; шаги в порядке выше; идемпотентность по `chat_id`.
- Отправка в старый чат отвечает `CHAT_WRITE_FORBIDDEN`.
- Взаимные ссылки `getFullChat` старого чата и `getFullChannel` нового; поля склейки истории.
- Перенос ссылок-приглашений.
- Не трогаем: `convertToGigagroup` (отложен, вопрос владельцу в 035).

## Критерии приёмки
- Модульные тесты: сборка ответа (`chat` с `migrated_to`, `channel`, сообщение `channelMigrateFrom`), перенос прав.
- Интеграционные тесты на MySQL и Kafka через `pkg/testinfra`: сбой между шагами и повтор сходятся без второго канала; каждый участник получает `chatMigrateTo` ровно один раз.
- e2e через `pkg/mtclient`: три участника, переход, ответ содержит `chat(migrated_to)`, `channel` и `channelMigrateFrom` меньше чем за 5 с (`ConvertGroupToSupergroup.swift:20-29`); остальные получают `chatMigrateTo` и пишут в супергруппу; отправка в старый чат даёт ошибку перехода; тест падает на текущем коде.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: «Преобразовать в супергруппу» и переход по действиям из U6 работают в iOS, Android, Desktop (CachyOS) и Web K; участники видят склеенную историю.

## Результат

### Сервер (2026-11-07, ветка `task/047-migrate`)
- `messages.migrateChat` → новый RPC `msg.migrateChat`: одна транзакция ящика создателя (`dao.MigrateChat`): группа `FOR UPDATE`; проверки — создатель (`CHAT_ADMIN_REQUIRED`), не удалена (`CHAT_ID_INVALID`), лимит каналов (`CHANNELS_TOO_MUCH`); супергруппа с названием, описанием, фото, общими запретами группы (без `view_messages`); участники: создатель → создатель, админы → админы с правами базовой группы (`MigrateAdminRights`, по tdesktop и Android), остальные — участники, звание и пригласивший сохраняются; сообщение 1 `messageActionChannelMigrateFrom`; `chatMigrateTo` в ящике создателя (пока группа ещё активна); группа — `migrated_to`, `deactivated`, участники в состоянии `Migrated`; ссылки-приглашения переходят к супергруппе (и постоянная ссылка у неё есть всегда).
- Остальным участникам — inbox-события той же транзакции: свой `chatMigrateTo`, затем диалог супергруппы (`channelInvitedToInbox`). Другим устройствам создателя — `chatMigrateTo` и `updateChannel` с `seq`.
- Ответ — `updateNewMessage(chatMigrateTo)`, `updateChannel`, `updateNewChannelMessage(channelMigrateFrom)`, в `chats` — группа с `migrated_to` и супергруппа (ADR 0034, п. 10). Повтор отвечает той же супергруппой без второй.
- `migrated_from_max_id` — наибольший `top_message` диалогов группы у всех участников: id базовой группы у каждого свои, а история каждого кончается ниже этого числа (решение на подтверждение; как считает Telegram, не сверено).
- Отправка в перенесённую группу — `CHAT_WRITE_FORBIDDEN` (уходящий из обычной группы по-прежнему отправляет служебное сообщение о выходе — первый вариант проверки это ломал, найдено e2e `TestSeqOthersChatLeave`).
- Ключи кэша групп вынесены в `chat.CacheKey`/`ParticipantCacheKey`, чтобы `msg` сбрасывал те же ключи.
- **Проверка:** e2e `TestChatMigrate` (участник не переводит, ответ меньше 5 с с тремя обновлениями и двумя чатами, `chatMigrateTo` у Боба, Боб пишет в супергруппу, старая группа не принимает, повтор — та же супергруппа), модульный `TestCacheKeys`. Интеграционного теста сбоя между шагами нет: всё — одна транзакция.
- **Осталось:** проверка в четырёх клиентах («Преобразовать в супергруппу» и переход по действиям U6).
