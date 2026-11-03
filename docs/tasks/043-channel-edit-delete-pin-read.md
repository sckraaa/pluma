# 043. Каналы: правка, удаление, закрепление, прочтение

- Статус: todo
- Зависит от: 040
- ADR: «Ящик канала» (из 035). Исследование: `docs/tasks/035-research/` (`server.md` §1.1, §2.3, §2.5, задача T8; `clients.md` §1 «Read history», «Send and receive»; `plan.md` §2 U1, U5, §3, задача «042» плана).

## Цель
Правки и удаления в канале видят все участники; закрепление работает; галочки прочтения и счётчики непрочитанного согласованы между устройствами.

## Что известно
- **Заблокировано сейчас** (`server.md` §1.1): `msg.editMessageV2` для `PEER_CHANNEL` — `ErrEnterpriseIsBlocked` (`app/messenger/msg/msg/internal/core/msg.editMessageV2_handler.go:41-43`); `msg.deleteMessages` — `ErrPeerIdInvalid` (`msg.deleteMessages_handler.go:32`). Остальные `channels.*` здесь — `missing` в `api-coverage.md`.
- **Обновления с `pts` канала:** `updateEditChannelMessage`, `updateDeleteChannelMessages`; `updatePinnedChannelMessages`; `updateReadChannelInbox` (api.tl:347, свой `pts`), `updateReadChannelOutbox` (`server.md` §2.1, §2.5).
- **`messages.affectedMessages`** из `channels.deleteMessages` и похожих Android пропускает через `processNewChannelDifferenceParams` (`MessagesController.java:8054-8095`, UNVERIFIED, U10): `pts` там должен принадлежать последовательности канала.
- **Прочтение:** `channels.readHistory` и `channels.readMessageContents` (Android — оба; iOS `AccountViewTracker` — `readMessageContents`). Для 100 пользователей состояние прочтения остаётся в `dialogs` (`server.md` §2.3); исследование рекомендует писать только строку читателя.
- **Своим устройствам:** идёт ли `updateReadChannelInbox` через общий ящик пользователя — UNVERIFIED (U1), закрывается в 035.
- **`read_outbox_max_id` в супергруппе** — по исследованию «максимум, прочитанный другими по всему каналу», как его определяет Telegram — UNVERIFIED (U5), закрывается в 035.
- Упоминания: `readMentions`, `getUnreadMentions` — `partial`; таблица упоминаний каналов — по образцу `migrate-20261030-unread-mentions.sql` (`server.md` §2.3). `dialog.getChannelMessageReadParticipants` уже отвечает из `dialogs.read_inbox_max_id`.
- Проверки прав (правка чужого, удаление чужого, закрепление) — по роли, если 042 ещё нет; иначе через `channelperm.Can` (кто из 042 и 043 позже, тот подключает).

## Область
- `PEER_CHANNEL` в `msg.editMessageV2` (`:41`), `messages.editMessage`, `messages.getMessageEditData`.
- `channels.deleteMessages` (`affectedMessages` с `pts` канала), `channels.deleteHistory` (оба варианта; «для себя» — через `available_min_id`), `channels.deleteParticipantHistory`.
- `messages.updatePinnedMessage` и `unpinAllMessages` для каналов (`updatePinnedChannelMessages`).
- `channels.readHistory` (`updateReadChannelInbox` своим устройствам, `updateReadChannelOutbox` остальным), `channels.readMessageContents`, ветки каналов в `readMentions` и `getUnreadMentions`.
- Свёртка правок и удалений в `getChannelDifference` (040) учитывает новые типы записей журнала.
- Не трогаем: реакции (044), просмотры (046).

## Критерии приёмки
- Модульные тесты: сборка ответов и обновлений, `affectedMessages` с `pts` канала.
- Интеграционные тесты на MySQL через `pkg/testinfra`: свёртка журнала «правка, затем удаление»; параллельные удаления не дают дыр в `pts`.
- e2e через `pkg/mtclient`: правку и удаление видит другой участник; прочтение на устройстве A доходит до устройства B; отправитель получает `updateReadChannelOutbox`; тест падает на текущем коде.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: правки, удаления и закрепления видят другие участники в iOS, Android, Desktop (CachyOS) и Web K; галочки и счётчики непрочитанного совпадают на двух устройствах одного пользователя.

## Результат
