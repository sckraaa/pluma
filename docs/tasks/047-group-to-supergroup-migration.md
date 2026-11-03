# 047. Переход базовой группы в супергруппу

- Статус: todo
- Зависит от: 041, 042
- ADR: 0011, 0012, 0021, «Ящик канала» (из 035). Исследование: `docs/tasks/035-research/` (`server.md` §1.1, §2.1–2.2, §2.7, задача T12; `plan.md` §1 п. 7, 9, §2 U3, U4, U6, U8, §3, задача «046» плана).

## Цель
«Преобразовать в супергруппу» работает во всех четырёх клиентах, как и действия, для которых клиент сам переводит группу в супергруппу (ROADMAP, этап 1: сейчас они кончаются ошибкой); участники сохраняют общую историю; повтор после сбоя не создаёт второй канал.

## Что известно
- `messages.migrateChat` (`app/bff/chats/internal/core/messages.migrateChat_handler.go:27`) отвечает `ErrEnterpriseIsBlocked`; `api-coverage.md:651` — `blocked`.
- Полуготовый помощник `chat.migratedToChannel` (`app/service/biz/chat/internal/core/chat.migratedToChannel_handler.go`): в одной транзакции ставит `chats.migrated_to_id/access_hash` и участников в `ChatMemberStateMigrated`; никто его не вызывает; результат `UpdateStateByChatIdTx` не проверяется (`:42`, проглоченная ошибка, проверено, `plan.md` §1 п. 7).
- **Telegram** (core.telegram.org/api/channel): ответ содержит старый `chat` с `migrated_to` и новый мегагрупповой `channel`; `channelFull` несёт `migrated_from_chat_id`, `migrated_from_max_id`; старым участникам — `messageActionChatMigrateTo` (api.tl:156); канал начинается с `messageActionChannelMigrateFrom` (api.tl:157); новые сообщения идут в супергруппу, клиенты склеивают обе истории.
- **iOS** `_internal_convertGroupToSupergroup`: `messages.migrateChat` с таймаутом 5 с, разбирает `CHANNELS_TOO_MUCH`, новый пир берёт из первого сообщения в `updates.messages`, чей пир не старый чат; без такого — `.generic` (`TelegramEngine/Peers/ConvertGroupToSupergroup.swift:13-29`, проверено, `plan.md` §1 п. 9). Ответ должен содержать служебное сообщение канала и прийти за 5 с.
- Как разбирают ответ Android, tdesktop и Web K — UNVERIFIED (U8).
- Какие настройки базовой группы заставляют клиент запускать переход сам — UNVERIFIED (U6).
- Как переносятся права админов группы — UNVERIFIED (U4); имя ошибки отправки в перенесённую группу (`CHAT_MIGRATED` или другое) — UNVERIFIED (U3). Всё закрывается в 035.
- **Порядок шагов** (`server.md` §2.7): под транзакцией чата — проверка, что вызывающий создатель, чат не деактивирован и не перенесён; строка `channels` (`megagroup`, `migrated_from_chat_id`, `migrated_from_max_id` = последнее сообщение чата, название, фото, описание, `default_banned_rights`); участники (создатель → создатель, админы — по U4); сообщение 1 `messageActionChannelMigrateFrom`; в ящике каждого участника — `messageActionChatMigrateTo`, `updateChat` (`seq`) и пометка диалога. Канал и ответ вызывающему — первыми, участники — через события outbox, как в ADR 0021; ключ идемпотентности — `chat_id`, повтор возвращает существующий канал.
- Ссылки-приглашения переносятся (`chat_invites.migrated_to_id`).

## Область
- `messages.migrateChat` и переходы, которые запускают клиенты (по итогам U6).
- Исправить `chat.migratedToChannel:42`; шаги в порядке выше; идемпотентность по `chat_id`.
- Отправка в старый чат отвечает ошибкой перехода (имя — из 035).
- Взаимные ссылки `getFullChat` старого чата и `getFullChannel` нового; поля склейки истории.
- Перенос ссылок-приглашений.
- Не трогаем: `convertToGigagroup` (отложен, вопрос владельцу в 035).

## Критерии приёмки
- Модульные тесты: сборка ответа (`chat` с `migrated_to`, `channel`, сообщение `channelMigrateFrom`), перенос прав.
- Интеграционные тесты на MySQL и Kafka через `pkg/testinfra`: сбой между шагами и повтор сходятся без второго канала; каждый участник получает `chatMigrateTo` ровно один раз.
- e2e через `pkg/mtclient`: три участника, переход, ответ содержит `chat(migrated_to)`, `channel` и `channelMigrateFrom` меньше чем за 5 с (`ConvertGroupToSupergroup.swift:20-29`); остальные получают `chatMigrateTo` и пишут в супергруппу; отправка в старый чат даёт ошибку перехода; тест падает на текущем коде.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: «Преобразовать в супергруппу» и переход по настройкам из U6 работают в iOS, Android, Desktop (CachyOS) и Web K; участники видят склеенную историю.

## Результат
