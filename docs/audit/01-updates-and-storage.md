# Аудит 01: модель обновлений (pts/qts/seq) и хранение сообщений

Область: `app/service/idgen`, `app/messenger/msg` (msg + inbox), `app/messenger/sync`, `app/interface/session`, `app/bff/updates`, `app/service/biz/updates`, `app/service/biz/message`, схема `teamgramd/deploy/sql/*.sql`.
Эталон: https://core.telegram.org/api/updates (pts — общий ящик пользователя, qts — секретные чаты/боты, seq — прочие updates в `updates`/`updatesCombined`, у каналов свой pts и `getChannelDifference`).
Ревизия: master @ 708fb953. Код только читался, ничего не запускалось. Места, где вывод сделан по коду без проверки в рантайме, помечены «(не проверено)».

---

## Вердикт

**Fix, не rewrite, но фикс глубокий.** Модель хранения для личных чатов и basic groups — у каждого пользователя свой ящик (`messages` с ключом `user_id, user_message_box_id`), свой `pts` и журнал `user_pts_updates` — по сути повторяет модель Telegram для «common message box». Её можно оставить, и каналы/супергруппы добавляются к ней сбоку без переписывания. А вот механика выдачи и сохранения pts в нынешнем виде для production не годится:
- счётчики pts, seq и id сообщений живут только в Redis, без транзакционной связи с MySQL;
- pts выделяется до записи в БД и вне той транзакции, где пишется сообщение;
- журнал пишется «best effort», ошибки только логируются;
- Kafka-консьюмер подтверждает offset, даже если обработчик упал;
- `seq` фактически не реализован (всегда 0), поэтому все не-pts обновления — fire-and-forget;
- каналов, супергрупп и `getChannelDifference` в open-source версии нет вообще (заглушка «enterprise»).

Ядро (схема ящиков, msg/inbox/sync, сессии) стоит оставить. Подсистему выдачи pts/seq и путь записи сообщения (allocate → persist → push) нужно переписать на транзакционную модель: счётчик в MySQL, одна транзакция на «сообщение + запись в журнал + новый pts», push только после commit. Каналы — отдельный новый модуль.

---

## Как устроено сейчас

### Выдача счётчиков
- Все последовательные счётчики делает `service.idgen` через `INCRBY` в Redis: `app/service/idgen/internal/core/idgen.getNextSeqId_handler.go:20`, `idgen.getNextIdValList_handler.go:42-58`. Хранилище — `kv.NewStore(c.SeqIDGen)` (`app/service/idgen/internal/dao/dao.go:35`), в конфиге это Redis (`teamgramd/etc/idgen.yaml`, `SeqIDGen: - Host: 127.0.0.1:6379`). В `docker-compose-env.yaml:68` стоит `--appendonly yes` с fsync по умолчанию (everysec).
- Ключи: `pts_updates_ngen_<userId>`, `message_box_ngen_<userId>`, `seq_updates_ngen_<authKeyId>`, `qts_updates_ngen_<authKeyId>`, `channel_pts_updates_ngen_<id>` и другие (`app/service/idgen/client/vars.go:18-28`).
- Клиент `IDGenClient2` при ошибке RPC или Redis **возвращает 0** и только пишет в лог (`app/service/idgen/client/idgen_client2.go:86-97`, далее `NextPtsId:163` и т.д.).
- Восстановления счётчика из MySQL нет: `SelectLastPts` объявлен в DAO (`app/messenger/msg/internal/dal/dao/mysql_dao/user_pts_updates_dao.go:92`), но нигде не используется для инициализации.

### Хранение
- `messages` — **копия сообщения на каждого участника** (fan-out on write). `user_message_box_id` — локальный id в ящике пользователя (int32), `dialog_message_id` — общий snowflake-id, связывающий копии. Индексы: `UNIQUE(user_id, user_message_box_id)`, `KEY(user_id, dialog_id1, dialog_id2)` (`teamgramd/deploy/sql/1_teamgram.sql:409-436, 932-935`). Удаление мягкое (`deleted=1`, `app/service/biz/message/internal/dal/dao/mysql_dao/messages_dao.go:770-811`). Индекса по `(sender_user_id, random_id)` нет, хотя `SelectByRandomId` ищет именно по этим полям (`messages_dao.go:109`).
- `user_pts_updates(user_id, pts, pts_count, update_type, update_data JSON, date2)`. Индекс только `KEY(user_id, pts)`, **не UNIQUE** (`1_teamgram.sql:743-751, 1058-1060`). В JSON хранится полный снимок `Update`: для `updateNewMessage` — всё сообщение.
- `auth_seq_updates(auth_id, user_id, seq, …)` (`1_teamgram.sql:89-98`). Таблица есть, **но в неё никто не пишет**: единственный писатель, `AddSeqToUpdatesQueue` (`app/messenger/sync/internal/dao/update.go:20`), нигде не вызывается.
- `dialogs` — по строке на пользователя и пира: `top_message`, `read_inbox_max_id`, `read_outbox_max_id`, `unread_count`. Upsert вида `unread_count = unread_count + values(...)` (`app/service/biz/dialog/internal/dal/dao/mysql_dao/dialogs_dao.go:93`). Плюс `message_read_outbox` (`migrate-20241016.sql`).

### Путь отправки сообщения (режим V2, включён по умолчанию; V3 — флаг `-usev3`, `app/messenger/msg/msg/internal/core/vars.go`)
1. `msg.sendMessageV2` → `DoIdempotent` (Redis-лок на 5 с, кэш ответа на 90 с; `app/messenger/msg/internal/dao/dao.go`, marmota `idempotent.go:108`).
2. `sendMessageToOutboxV2` (`app/messenger/msg/internal/dao/outbox.go:686`). Выделяет `dialogMessageId`, `outBoxMsgId` и `pts` отправителя из Redis. **Сразу** пишет `user_pts_updates` отправителя без транзакции (`outbox.go:759-768`, при ошибке только лог). Строка в `messages` в этот момент ещё **не создаётся**.
3. Сообщения публикуются в Kafka: `InboxSendUserMessageToInboxV2` для отправителя (Out=true) и для получателя (`msg.sendMessageV2_handler.go:185-227`). Ключ партиции — `user_id` адресата (`app/messenger/msg/inbox/client/inbox_mq_client.go:164-170`). Producer: `app/messenger/msg/msg/internal/svc/service_context.go:57`.
4. Клиент-отправитель получает ответ `updates` с `updateMessageID` и `updateNewMessage(pts)` ещё до того, как сообщение сохранено.
5. Inbox-консьюмер (`app/messenger/msg/inbox/internal/server/mq/mq.go`):
   - Out: `SendMessageToOutboxV1NoPts` пишет строку в `messages`, затем dialog через RPC (`outbox.go:595-680`), затем `SyncUpdatesNotMe` на остальные устройства отправителя (`inbox.sendUserMessageToInboxV2_handler.go:68-114`).
   - In: `sendMessageToInbox` выделяет `inBoxMsgId` (Redis) → транзакция `messages` → dialog вне транзакции → **после** commit выделяет pts (`app/messenger/msg/internal/dao/inbox.go:58-263`) → отдельный `persistPtsUpdate` (`inbox/internal/core/pts_helper.go:9-21`, ошибки только в лог) → `SyncPushUpdates`.
6. `sync` (`app/messenger/sync/internal/core/core.go:179-251`) берёт онлайн-сессии пользователя из Redis (`status`) и шлёт RPC на нужный session-сервер. Ничего не персистит: в `processUpdates` стоят комментарии «removed in pure push mode», `core.go:59-151`.
7. `session` кладёт update в in-memory очередь и отдаёт в gateway (`app/interface/session/internal/sess/session_sync.go:29-52`). Буфер живёт только в памяти.

### getState / getDifference
- `updates.getState` → `UpdatesGetStateV2` (`app/service/biz/updates/internal/core/updates.getStateV2_handler.go:21-39`). `pts` = **текущее значение Redis-счётчика**, а не максимальный закоммиченный pts в журнале. Если pts==0, вызывается `NextPtsId`, то есть сжигается один pts. `seq` = Redis-счётчик по `AuthKeyId` (никогда не растёт), при 0 отдаётся `-1`. `qts` — тоже по `AuthKeyId`. `date` = now(). `unread_count` = 0.
- `updates.getDifference` (`app/bff/updates/internal/core/updates.getDifference_handler.go`) → `UpdatesGetDifferenceV2` (`app/service/biz/updates/internal/core/updates.getDifferenceV2_handler.go:24-125`): `SELECT … WHERE user_id=? AND pts>? ORDER BY pts LIMIT N` (N ≤ 5000) плюс выборка `auth_seq_updates` по дате (всегда пусто). Потом merge (`merge_updates_helper.go`): `updateNewMessage` уходит в `new_messages`, остальное схлопывается и получает `pts=lastPts, pts_count=0`.
  - При `len >= limit` отдаётся `differenceSlice`, при пустом результате — `differenceEmpty`. `differenceTooLong` не выдаётся никогда (код закомментирован, `:74-78`).
  - `qts` из запроса игнорируется. `users`/`chats` добираются в BFF, ошибки игнорируются.
- `updates.getChannelDifference` — заглушка: `ErrEnterpriseIsBlocked` (`app/bff/updates/internal/core/updates.getChannelDifference_handler.go`, `app/service/biz/updates/internal/core/updates.getChannelDifferenceV2_handler.go:18-27`).

### Каналы
`PEER_CHANNEL` в `msg.sendMessageV2` — сразу `ErrEnterpriseIsBlocked` (`msg.sendMessageV2_handler.go:90-92`), в `msg.deleteMessages` — `ErrPeerIdInvalid` (`msg.deleteMessages_handler.go:47-50`). Модуля `bff/channels` нет. Таблиц для каналов в схеме нет. `messages.migrateChat` заблокирован. Готовы только ключи idgen под каналы и case-ветки «ignore» для channel-updates в merge.

---

## Находки

| # | Severity | Где | Проблема | Последствие для клиентов |
|---|---|---|---|---|
| 1 | **critical** | `app/service/idgen/internal/dao/dao.go:35`; `idgen.getNextSeqId_handler.go:20`; нет кода восстановления из `user_pts_updates`/`messages` | pts и `user_message_box_id` существуют только в Redis (AOF everysec). Если Redis потеряет данные (crash до fsync, потеря тома, FLUSH, переезд), счётчики откатятся. | Повтор `user_message_box_id`: вставка `ON DUPLICATE KEY UPDATE id=last_insert_id(id)` (`messages_dao.go:81-105`) **молча не пишет новое сообщение**, а в inbox-пути `rowsAffected` не проверяется (`inbox.go:175`, `outbox.go:611`). Повтор или откат pts: у клиента локальный pts больше серверного, новые updates с меньшим pts отбрасываются как уже применённые, `getDifference` возвращает `differenceEmpty`. Сообщения «невидимы» до переустановки. Для мобильных клиентов это фатально. |
| 2 | **critical** | `app/service/idgen/client/idgen_client2.go:86-97` и все `NextPtsId`/`NextMessageBoxId` без проверки на 0: `inbox.go:60, 260`, `inbox.deleteMessagesToInbox_handler.go:40`, `msg.readHistoryV2_handler.go:93`, `inbox.updateHistoryReaded_handler.go:73` и др. | Ошибка idgen или Redis превращается в `pts=0` или `id=0`, и работа продолжается. | Update с `pts=0, pts_count=1` клиент считает дубликатом или некорректным. Сообщение с `id=0` ломает UI. При коротком сбое Redis — тихая потеря событий. |
| 3 | **high** | `outbox.go:686-768` (V2), `inbox.go:151-263`, `msg.deleteMessages_handler.go:77-86`, `msg.readHistoryV2_handler.go:93-102` и т.д. | pts выделяется INCR-ом, а строка журнала коммитится отдельным запросом в другом процессе (msg-RPC и inbox-консьюмер работают параллельно для одного пользователя). Порядок commit ≠ порядок pts. `getDifference` читает `pts > X ORDER BY pts` без барьера «все pts ≤ max закоммичены». | Сценарий: pts=10 (входящее, inbox) ещё не закоммичен, pts=11 (readHistory с другого устройства) уже закоммичен. Устройство делает `getDifference`, получает state.pts=11. Update 10 **навсегда теряется** для этого устройства: ни push (уже «старый»), ни difference его не вернут. |
| 4 | **high** | `updates.getStateV2_handler.go:22-25` | `getState` отдаёт текущее значение Redis-счётчика, а не максимальный **закоммиченный** pts. | Сразу после логина или переподключения клиент может перепрыгнуть через выделенные, но ещё не записанные pts. Это та же потеря, что в п.3. |
| 5 | **high** | `inbox.go:251-262`, `inbox.sendUserMessageToInboxV2_handler.go:261`, `pts_helper.go:9-21`; `outbox.go:759-768`; `msg/internal/dao/pts.go:15-21` (`AddToPtsQueue` глотает ошибку) | Запись в `messages`, `dialogs` и `user_pts_updates` идёт в 2–3 независимых шага без общей транзакции. Ошибки записи журнала только логируются, а push и RPC-ответ уходят всё равно. | Сообщение есть в истории, но нет в журнале: офлайн-устройства не узнают о нём через `getDifference`, увидят только после `getDialogs`/`getHistory`. Обратный случай (V2 у отправителя): журнал уже говорит `updateNewMessage`, а строки в `messages` нет (Kafka упала, консьюмер упал). Получается «призрачное» сообщение на других устройствах. |
| 6 | **high** | marmota `pkg/mq/consumer_group.go:92-122` (используется в `inbox/internal/server/mq/mq.go`) | Консьюмер вызывает `MarkMessage` независимо от результата обработчика, повторов нет, DLQ нет. Плюс autocommit: при падении процесса возможна повторная доставка. | При ошибке БД в inbox сообщение у получателя **теряется навсегда**. При повторной доставке выделяется новый `inBoxMsgId` и новый pts, и у получателя появляется **дубликат** сообщения. Дедупликации по `random_id` или `dialog_message_id` в inbox нет. |
| 7 | **high** | `app/messenger/sync/internal/dao/update.go:20` (не вызывается); `core.go:50-177`; proto `updates_helper.go:31-84` (`Seq: 0`) | `seq` не реализован. Все не-pts обновления (`updateUserName`, `updateChatParticipants`, `updateNotifySettings`, `updateDialogPinned`, `updateChatDefaultBannedRights`, `updatePeerSettings`…) уходят только live-push с `seq=0` и нигде не сохраняются. По документации Telegram state (pts/qts/seq/date) — общий на аккаунт; здесь seq привязан к `AuthKeyId`, а `getState` отдаёт `seq=-1`. | Устройство, которое было офлайн или в фоне (iOS/Android почти всегда), **никогда не получит** такие изменения: устаревшие участники группы, имена, закрепы, настройки уведомлений — до ручного обновления экрана или релогина. Поведение «1:1 как в Telegram» недостижимо без seq-журнала. |
| 8 | **high** | Весь каналовый путь: `msg.sendMessageV2_handler.go:90-92`, `bff/updates/.../updates.getChannelDifference_handler.go`, отсутствие `bff/channels` и таблиц | Каналов и супергрупп нет. | Официальные клиенты активно используют channel-API: супергруппы, «каналы», обсуждения, топики, `messages.migrateChat`. Если клиент создаст или получит супергруппу (в том числе через миграцию basic group), всё сломается. Создание супергрупп нужно блокировать патчем клиента или реализовывать. |
| 9 | medium | `inbox.editMessageToInboxV2_handler.go:97-107`; `outbox.go:173-283` (старый путь, dup random_id); любые ошибки после `NextPtsId` | «Сожжённые» pts: pts выделен, но update не записан (сообщение для редактирования не найдено, tx упала и т.п.). | Дыра в последовательности. Клиент видит gap, ждёт (~0.5–1 с по рекомендации Telegram) и делает `getDifference`. Данные не теряются, но появляются лишние задержки и запросы, а на мобильных — заметное «подвисание» доставки. |
| 10 | medium | `app/messenger/msg/internal/dao/dao.go` (`DoIdempotent(..., 5, 90, ...)`), marmota `idempotent.go:108-147`; нет UNIQUE по `random_id` | Идемпотентность `sendMessage` держится только в Redis: лок 5 с, кэш 90 с. | Мобильный клиент, переотправивший сообщение из офлайн-очереди позже чем через 90 с, или обработка дольше 5 с — **дубликат сообщения** у обеих сторон. Telegram в этом случае отвечает `RANDOM_ID_DUPLICATE` или возвращает исходное сообщение. |
| 11 | medium | `msg.sendMessageV2_handler.go:345-349, 377-380` | В `sendChatOutgoingMessageV2` внутри замыкания стоит `return err` (внешняя переменная, равна nil) вместо `err2`. Ошибки отправки в группу проглатываются, `nil` кэшируется на 90 с, ответ RPC — `(nil, nil)`. | При сбое отправки в basic group клиент получает пустой или битый ответ вместо ошибки. Повтор в течение 90 с тоже получит кэшированный nil. Сообщение повисает «в отправке» (не проверено в рантайме). |
| 12 | medium | `updates.getDifferenceV2_handler.go:36-79`; `bff/updates/.../getDifference_handler.go` | `differenceTooLong` не выдаётся никогда. Журнал `user_pts_updates` не чистится. `qts` игнорируется. `state.unread_count=0`. Нет защиты от случая «клиентский pts > серверного». | После долгого офлайна клиент выкачивает до 5000 updates слайсами (это допустимо). Но при рассинхроне (п.1) нет механизма «сбрось кэш» — Telegram в этом случае шлёт `differenceTooLong`, и клиент перезапрашивает диалоги. |
| 13 | medium | `msg.readHistoryV2_handler.go:93-116` + `inbox.readInboxHistory_handler.go` | pts и update прочтения пишутся сразу, а `dialogs.read_inbox_max_id/unread_count` обновляются асинхронно через Kafka. | Короткое окно, в котором `getDialogs`/`getPeerDialogs` возвращают старые счётчики непрочитанного, хотя update уже применён: «прыгающие» бейджи. |
| 14 | medium | `merge_updates_helper.go:62-200` | При слиянии в difference у всех other_updates выставляются `pts=lastPts, pts_count=0`, delete/readContents переставляются в конец списка. Это нестандартная форма. | Зависит от клиента. tdlib и официальные клиенты применяют `other_updates` из difference, не проверяя pts, так что скорее всего работает. Для iOS/Android/Desktop **не проверено**. Нужен интеграционный тест. |
| 15 | low | `merge_updates_helper.go:99, 124` | Проверка `foundId > 0` вместо `>= 0`: совпадение с элементом с индексом 0 обрабатывается неверно (элемент удалён, вставлен новый, в индексе висит удалённый). | Возможны дублирующиеся `updateReadHistory*` в difference. Последствия скорее косметические. |
| 16 | low | `msg.deleteMessages_handler.go:37` | Для пустого списка id возвращается `CurrentPtsId(in.PeerId)` вместо `in.UserId`. | Неверный pts в `messages.affectedMessages` — лишний gap и `getDifference`. |
| 17 | low | `1_teamgram.sql:1058-1060` | Нет `UNIQUE(user_id, pts)` в `user_pts_updates`, нет `UNIQUE(user_id, random_id)` и индекса под `SelectByRandomId` в `messages`. | Дубли в журнале не ловятся на уровне БД. Поиск по random_id идёт полным сканом по ящику. |
| 18 | low | `dialogs_dao.go:93` (`top_message = values(top_message)`) | `top_message` перезаписывается без `GREATEST`. | При переупорядочивании обработки возможен откат `top_message` в списке диалогов. Внутри одной Kafka-партиции порядок сохраняется, так что риск невысокий. |
| 19 | info | `inbox.sendUserMessageToInboxV2_handler.go:34-159`, `inbox.readInboxHistory_handler.go`, флаг `-usev3` | Параллельно живут два пути (V2/V3) с разной точкой выделения pts и разным порядком persist/push. В V3 RPC-результат отправляется до записи журнала. | Высокая сложность и риск регрессий. Нужно выбрать один путь и удалить второй. |

Что сделано правильно:
- per-user ящики и id сообщений повторяют Telegram для ЛС и basic groups;
- `dialog_message_id` связывает копии, поэтому revoke-delete и edit корректно расходятся по ящикам (`inbox.go` `DeleteInboxMessages`, `inbox.editMessageToInboxV2_handler.go:104`);
- `updateMessageID` в ответе на отправку есть;
- `SyncUpdatesNotMe` исключает устройство-инициатора;
- push получателю идёт после записи сообщения;
- Kafka-ключ равен `user_id`, поэтому обработка входящих для одного пользователя последовательная.

---

## Что нужно для каналов/супергрупп

В нынешней модели нет ничего, что мешало бы добавить каналы. Это отдельная подсистема, а per-user ящики остаются как есть, потому что в Telegram личные чаты и basic groups живут в common box, а каналы — в своих. Нужно:

1. **Схема (новые таблицы):**
   - `channels` — метаданные, `pts`, `top_message`, флаги megagroup/broadcast;
   - `channel_participants` — роль, права, `date`, `inviter`, `left/kicked`;
   - `channel_messages(channel_id, id, …)` — **одна копия на канал**, `id` последовательный в пределах канала;
   - `channel_pts_updates(channel_id, pts, pts_count, update)` с `UNIQUE(channel_id, pts)`;
   - per-user состояние канала в `dialogs`: `read_inbox_max_id`, `read_outbox_max_id`, `unread_count`, `unread_mentions`. Таблица уже поддерживает `peer_type`, но `unread_count` для каналов придётся вычислять как `top_message - read_inbox_max_id` с учётом удалённых или вести счётчиком;
   - отдельно: per-user `channel_messages_deleted`/`available_min_id` (история для новых участников), реакции и просмотры на общей копии.
2. **pts канала** — транзакционный счётчик в строке `channels`: `UPDATE channels SET pts=pts+? … ` в той же транзакции, что `INSERT channel_messages` и `channel_pts_updates`. Ключи idgen `channel_pts_updates_ngen_*` использовать не стоит, иначе повторятся п.1 и п.3.
3. **RPC:** около 40–60 методов `channels.*` (create, invite, join/leave, getParticipants, editAdmin/editBanned, getMessages, readHistory, deleteMessages, getFullChannel, toggle*, exportInvite) плюс ветки `PEER_CHANNEL` в `messages.*`: send, edit, delete, forward, getHistory, search, pin, readMentions, reactions, getDialogs. Нужен `messages.migrateChat`, если basic groups остаются.
4. **Обновления:** `updateNewChannelMessage`/`updateEditChannelMessage`/`updateDeleteChannelMessages` с channel-pts, `updateChannel`, `updateChannelTooLong`, `updateReadChannelInbox/Outbox`. Fan-out push онлайн-участникам (для 100 пользователей хватит текущего `sync` + `status`). Реализация `updates.getChannelDifference` (empty/slice/tooLong, `final`, `timeout`). Отражение каналов в `getDifference` (Telegram отдаёт там `updateChannelTooLong`, а не сообщения каналов).
5. Объём оценивается как **крупнейший отдельный блок** работ (см. ниже). Наличие enterprise-заглушек означает, что upstream этот код имеет, но в open-source его нет. Писать придётся с нуля (не проверено, есть ли совместимая открытая реализация).

Альтернатива для закрытой аудитории ≤100 человек: оставить только ЛС и basic groups (лимит 200 участников) и патчем клиентов убрать создание каналов и супергрупп и миграцию. Это сокращает объём в разы. Вопрос — насколько это совместимо с требованием «1:1 как Telegram». Будут недоступны топики, обсуждения, публичные группы, slow mode, гранулярные права админов.

---

## Рекомендация и оценка объёма

**Решение: оставить ядро (схему ящиков, сервисы msg/inbox/sync/session, BFF), переписать подсистему последовательностей и путь записи.**

Приоритетный план (оценки грубые, один опытный Go-разработчик, без учёта QA на реальных клиентах):

1. **Транзакционные pts — 1.5–2.5 недели.** Таблица `user_state(user_id PK, pts, seq, qts, date, max_box_id)`. Каждая операция над ящиком пользователя выполняется в одной MySQL-транзакции: `SELECT … FOR UPDATE` (или `UPDATE … SET pts=pts+N`) + `INSERT messages` + `INSERT user_pts_updates` (`UNIQUE(user_id,pts)`) + `dialogs`. Push только после commit. Это автоматически сериализует pts по пользователю и убирает п.1–5 и п.9. Redis остаётся для snowflake и кэшей. `getState` читает `user_state` из БД. Нагрузка для 100 пользователей ничтожная.
2. **Надёжная доставка в inbox — 1 неделя.** Обработчик возвращает ошибку; commit offset только после успеха; ретраи и DLQ; идемпотентность по `(user_id, dialog_message_id)` через `UNIQUE` в `messages`. Альтернатива — для ≤100 пользователей убрать Kafka из критического пути и писать ящики получателей синхронно. Плюс постоянная дедупликация `random_id`: `UNIQUE(user_id, random_id)` для исходящих (п.6, п.10, п.11).
3. **seq-журнал — 1–1.5 недели.** Per-user `seq` (как pts, в `user_state`) для не-pts обновлений, запись в `auth_seq_updates` или новую `user_seq_updates`, выдача в `getDifference.other_updates`, отправка в `updates`/`updatesCombined` с `seq/seq_start`. Сюда же `differenceTooLong` и TTL журнала (п.7, п.12).
4. **Удалить V3/дубли путей, починить мелочи** (п.11, 13–19) — 3–5 дней.
5. **Интеграционные тесты на реальных клиентах** (tdlib + Telegram Desktop, затем iOS/Android): gap, офлайн, мультидевайс, рестарты Redis/Kafka/сервисов — 1–2 недели. Нужен до принятия решения о каналах.
6. **Каналы/супергруппы** (если нужны): схема, channel-pts, getChannelDifference, около 50 RPC, ветки в messages.*, миграция basic→super — **6–10 недель** (оценка неточная, сильно зависит от набора фич: топики, реакции, обсуждения, админ-лог).

Итого без каналов: **~5–7 недель**, чтобы получить корректную модель sync для ЛС и basic groups. С каналами — **~11–17 недель**. Полный rewrite ядра стоил бы заметно больше (вся BFF и сервисы опираются на текущую схему ящиков) и не даёт выигрыша: проблема локализована в слое выдачи и записи последовательностей, а не в модели данных.
