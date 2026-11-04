# 043. Каналы: правка, удаление, закрепление, прочтение

- Статус: in progress (прочтение, правка, удаление, пересылка из канала готовы; закрепление, `readMessageContents`, упоминания — нет)
- Зависит от: 040
- ADR: 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` §1.1, §2.3, §2.5, задача T8; `clients.md` §1 «Read history», «Send and receive»; `plan.md` §2 U1, U5, §3, задача «042» плана).

## Цель
Правки и удаления в канале видят все участники; закрепление работает; галочки прочтения и счётчики непрочитанного согласованы между устройствами.

## Что известно
- **Заблокировано сейчас** (`server.md` §1.1): `msg.editMessageV2` для `PEER_CHANNEL` — `ErrEnterpriseIsBlocked` (`app/messenger/msg/msg/internal/core/msg.editMessageV2_handler.go:41-43`); `msg.deleteMessages` — `ErrPeerIdInvalid` (`msg.deleteMessages_handler.go:32`). Остальные `channels.*` здесь — `missing` в `api-coverage.md`.
- **Обновления с `pts` канала:** `updateEditChannelMessage`, `updateDeleteChannelMessages`; `updatePinnedChannelMessages`; `updateReadChannelInbox` (api.tl:347, свой `pts`), `updateReadChannelOutbox` (`server.md` §2.1, §2.5).
- **`messages.affectedMessages`** из `channels.deleteMessages` и похожих Android пропускает через `processNewChannelDifferenceParams` (`MessagesController.java:8171-8213`, 035): `pts` там должен принадлежать последовательности канала.
- **Прочтение:** `channels.readHistory` и `channels.readMessageContents` (Android — оба; iOS `AccountViewTracker` — `readMessageContents`). Для 100 пользователей состояние прочтения остаётся в `dialogs` (`server.md` §2.3); исследование рекомендует писать только строку читателя.
- **Своим устройствам** (U1, ADR 0034, п. 3): `updateReadChannelInbox` идёт с `seq` своего аккаунта, свёртка — последнее по каналу; его `pts` — текущий `channels.pts`: Web K проводит его через проверку `pts` канала с `pts_count = 0` (`apiUpdatesManager.ts:639-641`, `:696-724`), tdesktop и iOS верят `still_unread_count` только при равном `pts` (`history/history.cpp:2183`, `AccountStateManagementUtils.swift:4725-4764`). Так же своим устройствам — `updateChannelReadMessagesContents`, `updateChannelAvailableMessages`. Как у Telegram — не проверено.
- **`read_outbox_max_id` в супергруппе** (U5, ADR 0034, пп. 1, 3): для зрителя — наибольший `read_inbox_max_id` других участников, вычисляется при чтении; `updateReadChannelOutbox` — пушем без `seq` авторам сообщений в прочитанном диапазоне, только в супергруппах. Определение Telegram не проверено.
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
### Прочтение (2026-11-04, ветка `task/038-channel-create`)
Сделано раньше остального 043: без него счётчик непрочитанного у участников канала не сбрасывается, а это блокирует выкат каналов.
- `channels.readHistory` → biz `channel.readHistory`: транзакция ящика читателя, канал не блокируется и его `pts` не растёт. `read_inbox_max_id` двигается только вперёд (`max_id` 0 или выше последнего — до последнего), упоминания до него прочитаны. Своим устройствам — `updateReadChannelInbox` с `seq` (свёртка — последнее по каналу), с `pts` канала и `still_unread_count`. В супергруппе авторам прочитанного — `updateReadChannelOutbox` пушем без `seq` (не больше 1000 авторов). Метку «непрочитано» прочтение не трогает (у каналов её снимает `messages.markDialogUnread`). Не участник публичного канала получает `true` без записи; чужой закрытый — `CHANNEL_PRIVATE`.
- Строка диалога сбрасывается в кэше biz dialog после коммита, как у всех писателей диалогов каналов.
- **Проверка:** интеграционный `TestRead` (только вперёд, упоминания, `seq`-строка с `pts` и счётчиком, пуш автору, не участник; падает без проверки «только вперёд»), модульный `TestSeqFold` (свёртка `updateChannel` и `updateReadChannelInbox` по каналу), e2e `TestChannelRead` (счётчик до и после, второе устройство, автор).

### Правка, удаление, пересылка (2026-11-04)
- **Правка.** `messages.editMessage` в канале → msg `editChannelMessage`: в транзакции редактора под блокировкой канала меняется единственная копия, строка журнала `updateEditChannelMessage` несёт всё сообщение после правки (как ADR 0019), пуш участникам. Кто может: автор своё сообщение в супергруппе; в канале — создатель и админы с `edit_messages` любой пост, автор — свой, пока может публиковать (core.telegram.org/constructor/chatAdminRights). Иначе `MESSAGE_AUTHOR_REQUIRED`.
- **Удаление.** `channels.deleteMessages` → biz `channel.deleteMessages`: свои сообщения — всем, чужие — создателю и админам с `delete_messages`, иначе `MESSAGE_DELETE_FORBIDDEN` (core.telegram.org/method/channels.deleteMessages). Одна строка журнала на запрос (`pts_count` — число id), упоминания удалённых снимаются, `top_message` откатывается к последнему оставшемуся, пуш участникам. Несуществующие и уже удалённые id пропускаются; ничего не удалено — `pts` канала и `pts_count` 0. Не больше 100 id.
- **Пересылка из канала.** `messages.forwardMessages` с `from_peer` канала: сообщения берутся из единственной копии как их видит пересылающий (biz отвечает `CHANNEL_PRIVATE` постороннему закрытого канала); пост канала — `fwd_from` от канала с `channel_post`, сообщение супергруппы — от автора; в «Избранном» `saved_from_peer` — канал. Канал с запретом пересылки — `CHAT_FORWARDS_RESTRICTED`. Служебные сообщения не пересылаются.
- **Проверка:** интеграционный `TestDelete`; e2e `TestChannelEdit` (правка автором доходит до другого участника, чужое не правится и не удаляется, создатель удаляет, удаление доходит пушем, история и разница его учитывают) и `TestChannelForward`.
- **Осталось:** закрепление, `readMessageContents`, ветки каналов в `readMentions` и `getUnreadMentions`.

