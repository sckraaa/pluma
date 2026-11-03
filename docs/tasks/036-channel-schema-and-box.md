# 036. Каналы: схема и `pkg/channelbox`

- Статус: todo
- Зависит от: 035
- ADR: 0011, 0012, «Ящик канала» (из 035). Исследование: `docs/tasks/035-research/` (`server.md` §1.2, §2.3–2.4; `plan.md` §3, задача «035» плана).

## Цель
В базе есть таблицы каналов, а в коде — примитив записи в ящик канала: одна транзакция на канал, `pts` и id сообщений без пропусков и повторов, побочные эффекты через `mq_outbox`. Клиенты пока ничего не видят.

## Что известно
- Таблиц каналов нет. Каналы упоминаются только в `users.personal_channel_id` (`teamgramd/deploy/sql/migrate-20241010.sql`) и в соседнем столбце в `migrate-20241123.sql` (`server.md`, §1.2).
- Переиспользуемые таблицы `1_teamgram.sql`: `chats` (172-189, уже есть `migrated_to_id`, `migrated_to_access_hash`, `default_banned_rights`, `deactivated`, `version`), `chat_participants` (240-257), `chat_invites` (197-215, есть `migrated_to_id`), `dialogs` (286-308, общий `peer_type`/`peer_id`, столбца `pts` нет), `messages` (409-430), `user_pts_updates` (743-752). С ADR 0011 есть `user_state`, с ADR 0012 — `mq_outbox`.
- Образец — `pkg/userbox`: `userbox.Run(ctx, db, userId, fn)`, `type Box` (`pkg/userbox/box.go:50`), инварианты И1–И4 ADR 0011, `Box.Emit` в `mq_outbox` в той же транзакции (ADR 0012 §2). Интеграционные тесты для образца: `pkg/userbox/maint_integration_test.go`, `outbox_integration_test.go`; модульные на поддельной БД — `pkg/userbox/fakedb_test.go`.
- idgen: ключи `channel_pts_*` и `channel_message_box_*` удалены по ADR 0011 (l.230); счётчики каналов должны быть строкой MySQL под тем же примитивом (ADR 0011, l.230, l.411).
- Таблицы по исследованию (`server.md`, §2.3; окончательный состав — в ADR из 035):
  - `channels` — строка-счётчик: `id`, `access_hash`, `creator_user_id`, `title`, `about`, `photo_id`, `broadcast`, `megagroup`, `forum`, `signatures`, `pts` (база 1, как `userbox.BasePts`), `last_message_id`, `top_message`, `participants_count`, `admins_count`, `kicked_count`, `banned_count`, `default_banned_rights`, `slowmode_seconds`, `hidden_prehistory`, `linked_chat_id`, `migrated_from_chat_id`, `migrated_from_max_id`, `noforwards`, `join_to_send`, `join_request`, `participants_hidden`, `available_reactions`, `deleted`, `version`, `date`;
  - `channel_messages` — одна общая копия сообщения, `PRIMARY KEY(channel_id, id)`, `UNIQUE(channel_id, sender_user_id, random_id)` (сегодня от повторов защищает только Redis `DoIdempotent`, аудит 01 #10);
  - `channel_pts_updates(channel_id, pts, pts_count, update_type, update_data json, date)`, `UNIQUE(channel_id, pts)`;
  - `channel_participants(channel_id, user_id, role, admin_rights, rank, banned_rights, until_date, inviter_id, promoted_by, kicked_by, date, left, state, available_min_id)`; `admin_rights` — `chatAdminRights` (api.tl:1293), `banned_rights` — `chatBannedRights` (api.tl:1295).
- id сообщений в канале последовательные (`last_message_id + 1`), не snowflake и не id ящика пользователя (`server.md`, §2.3).

## Область
- Миграция `teamgramd/deploy/sql/migrate-YYYYMMDD-channels.sql`: `channels`, `channel_messages`, `channel_pts_updates`, `channel_participants`; повторный запуск безопасен (`CREATE TABLE IF NOT EXISTS`), применяется сервисом migrate.
- `pkg/channelbox`: `Run(ctx, db, channelId, fn)` берёт `SELECT … FROM channels WHERE id=? FOR UPDATE`; внутри — `NextMessageId`, `AddPtsUpdate`, `Emit` через `pkg/outbox`; плюс чтения (`Check`, `Read`), как в `pkg/userbox`. Новые `pts` и `last_message_id`, строки сообщений и журнала фиксируются одной транзакцией.
- `docs/ops/*`: новая миграция, если операторам нужно что-то сделать.
- Не трогаем: обработчики RPC, маршрутизацию, клиенты.

## Критерии приёмки
- Модульные тесты на поддельной БД (как `pkg/userbox/fakedb_test.go`).
- Интеграционные тесты на MySQL через `pkg/testinfra`:
  - параллельные `Run` на один канал дают `pts` и id без пропусков и повторов;
  - откат транзакции ничего не сжигает;
  - `UNIQUE(channel_id, pts)` и `UNIQUE(channel_id, sender_user_id, random_id)` держат;
  - миграция применяется дважды без ошибок.
- Тест, который падает без `FOR UPDATE` (проверено, что падает).
- Обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах: изменений не видно, проверка в клиентах не нужна.

## Результат
