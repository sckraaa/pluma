# 0011. Долговечные счётчики: состояние ящика пользователя в MySQL и одна транзакция на ящик

- Статус: accepted
- Дата: 2026-10-22
- Задача 002. Закрывает находки аудита `docs/audit/01-updates-and-storage.md` №1–5, 9, 11, 16, 17 и №19 в той части, которая касается пути записи. Развивает ADR 0005: MySQL — источник истины, Redis — только кэш. Опирается на ADR 0009 (своя копия marmota).

## Контекст

Клиент Telegram держит локальный `pts` общего ящика (common message box) и применяет обновление, только если `local_pts + pts_count == pts`. Если сумма больше `pts`, обновление считается уже применённым и отбрасывается. Если меньше, это дыра: клиент ждёт до 0,5 с и вызывает `updates.getDifference` (https://core.telegram.org/api/updates). Отсюда инварианты, которые должен держать сервер для каждого пользователя:

- **И1.** Каждый `pts`, который видит клиент (ответ RPC, push, `getState`, `getDifference`), закоммичен вместе со строкой журнала `user_pts_updates` и с изменением данных, которое эта строка описывает.
- **И2.** Журнал без дыр и дублей: отрезки `(pts − pts_count, pts]` всех строк покрывают `(база, state.pts]` ровно по одному разу.
- **И3.** Порядок commit совпадает с порядком `pts`. Читатель, увидевший `state.pts = P`, видит все строки журнала с `pts ≤ P`.
- **И4.** `pts` и id сообщений в ящике (`user_message_box_id`) никогда не уменьшаются, в том числе после потери Redis.

Сейчас ни один из инвариантов не выполняется:

- `pts`, id в ящике, `seq` и `qts` — счётчики `INCRBY` в Redis (`app/service/idgen/internal/core/idgen.getNextSeqId_handler.go`, ключи в `app/service/idgen/client/vars.go`). Восстановления из MySQL нет. Ошибка idgen превращается в `pts=0` или `id=0` (`idgen_client2.go`).
- `pts` выделяется до записи и вне транзакции. Строка журнала пишется отдельным запросом, ошибка только логируется (`outbox.go` `sendMessageToOutboxV2`, `inbox/internal/core/pts_helper.go`, `msg/internal/dao/pts.go` `AddToPtsQueue`). Некоторые пути выделяют `pts` и вовсе без журнала (`msg.readMessageContents` через `NextNPtsId`). msg-RPC и inbox-консьюмер выделяют `pts` одному пользователю параллельно, и порядок commit не совпадает с порядком `pts`.
- В режиме V2 отправитель получает `updateNewMessage(pts)` и строку журнала раньше, чем строку `messages`: её пишет inbox-консьюмер по событию `Out` из Kafka. `dialogs` пишется отдельно, через RPC в biz.
- Проверки и вычисления делаются по снимку, прочитанному до записи и вне транзакции (`readHistoryV2`: `ReadInboxMaxId`, `UnreadCount`; `editMessageV2`: `dstMessage` от BFF; `deleteMessages`: `SelectDialogLastMessageId` после удаления).
- `updates.getState` отдаёт текущее значение Redis-счётчика, а не закоммиченное. При 0 он сжигает один `pts` (`updates.getStateV2_handler.go`).
- Ключ Kafka равен получателю только у части inbox-событий: `deleteMessagesToInbox`, `updatePinnedMessage`, `unpinAllMessages`, `updateHistoryReaded` публикуются с ключом `PeerId` (в группе это id чата), `deleteChatHistoryToInbox` — с id чата (`inbox_mq_client.go`). Порядок держится случайно: `Inbox-T` создаётся с одной партицией (`docker-compose-env.yaml`, `Inbox-T:1:0`), а при автосоздании их 3 (`KAFKA_NUM_PARTITIONS: 3`).
- Параллельно живут пути V2 и V3 (флаг `-usev3`) и остатки V1 (`sendMessageToOutbox`, `editOutboxMessage`, мёртвые inbox-события).

Факты, на которые опирается решение:

- msg-RPC и inbox-консьюмер работают в одном процессе `msg` (`app/messenger/msg/internal/server/server.go`) и в одной базе MySQL 8.0. go-zero кэширует `*sql.DB` по строке DSN, поэтому RPC и консьюмер делят один пул (`Active: 64`). `msg.yaml` задаёт `Timeout: 0`, то есть у RPC нет дедлайна.
- go-zero начинает транзакцию через `db.Begin()` без контекста и без уровня изоляции, а `TransactCtx` проходит через автомат отключения пула (`core/stores/sqlx/tx.go`, `sqlconn.go`). У marmota `*sqlx.Tx` есть `QueryRow`/`QueryRows`, но конструктор не экспортирован. У DAO, сгенерированных dalgen, `*Tx`-варианты есть только у записей; генератора (`dalgen3`) в репозитории нет.
- `DoIdempotent` (marmota `pkg/idempotent`) после успешного `fn` вызывает `SetCacheValue` и возвращает её ошибку; блокировка — 5 с (`DoIdempotent(..., 5, 90, ...)`).
- В biz dialog есть многооператорные транзакции над `dialogs` и `saved_dialogs` (`reorderPinnedDialogs` с диапазонным `UPDATE … NOT IN (…)`, `reorderPinnedSavedDialogs`, `setHistoryTTL` и `setChatTheme` — по строке двух пользователей), ошибки внутри них частично проглатываются (`_, _ =`).
- `dialog_message_id`, id фото и документов, `grouped_id` — snowflake из idgen. Они от Redis не зависят.
- До 100 пользователей: надёжность важнее микрооптимизаций, но задержку отправки нужно измерить. Пользователей у системы пока нет (этап 0).
- Надёжная доставка через Kafka (offset, ретраи, DLQ, идемпотентность консьюмера, outbox) — задача 003. `seq`, `qts` и полнота `getDifference` — задача 010.

## Решение

### 1. Таблица `user_state` — единственный источник счётчиков ящика

```sql
CREATE TABLE IF NOT EXISTS `user_state` (
  `user_id` bigint(20) NOT NULL,
  `pts` int(11) NOT NULL DEFAULT '1',
  `qts` int(11) NOT NULL DEFAULT '0',
  `seq` int(11) NOT NULL DEFAULT '0',
  `last_message_box_id` int(11) NOT NULL DEFAULT '0',
  `date2` bigint(20) NOT NULL DEFAULT '0',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

- `pts` — последний закоммиченный `pts` общего ящика. База — 1: первое обновление получает `pts = 2`. Так `getState` не отдаёт 0, как и сейчас, но без сожжённого `pts`.
- `last_message_box_id` — последний выданный `user_message_box_id`.
- `qts` и `seq` — колонки для задачи 010. В 002 они не растут.
- `date2` — время последнего изменения счётчиков.
- Отсутствие строки означает базовое состояние: `pts = 1`, id в ящике 0. Строка создаётся при первой записи в ящик (см. п. 2), поэтому код создания пользователей не меняется.
- Писать в `user_state` можно только через примитив из п. 2. Исключение — команды обслуживания `pluma-admin` из пп. 10–11, которые выполняются при остановленных сервисах.

### 2. Транзакция ящика: пакет `pkg/userbox`

Каждое изменение ящика одного пользователя — одна транзакция MySQL. В ней пишутся данные (`messages`, `dialogs`, `hash_tags`, `saved_dialogs`, `message_read_outbox`), строки журнала и новые значения счётчиков. Пакет лежит в `pkg/`, потому что им пользуются msg и biz dialog (п. 3), а в задаче 010 — и другие biz-сервисы (журнал `seq`).

```go
func Check(ctx context.Context, db *sqlx.DB) error                // при старте сервиса, см. «Настройки сервера»
func Read(ctx context.Context, db *sqlx.DB, userId int64) (State, error) // закоммиченное состояние; нет строки — база
func Run(ctx context.Context, db *sqlx.DB, userId int64, fn func(b *Box) error) (*Result, error)

func (b *Box) Context() context.Context                          // контекст попытки с меткой «внутри Run»
func (b *Box) Tx() *sqlx.Tx                                       // единственный путь к SQL внутри fn
func (b *Box) State() State                                       // заблокированное состояние на начало
func (b *Box) NextMessageId() int32                               // last_message_box_id + 1
func (b *Box) AddPtsUpdate(u *mtproto.Update, ptsCount int32)     // pts += ptsCount; u.Pts_INT32, u.PtsCount; строка журнала
func (b *Box) InvalidateCache(keys ...string)                     // ключи кэша, сбрасываемые после commit
func (b *Box) Emit(ev Event)                                      // побочный эффект после commit: push, событие inbox
// Result: закоммиченное State, журналированные Updates (по возрастанию pts), Events и ключи кэша.
```

Запросы `Run` для пользователя U:

```sql
SET TRANSACTION ISOLATION LEVEL READ COMMITTED; START TRANSACTION;  -- BeginTx с sql.LevelReadCommitted
INSERT INTO user_state (user_id, date2) VALUES (:u, :now)
  ON DUPLICATE KEY UPDATE user_id = user_id;      -- X-блокировка строки; первая запись создаёт её с pts = 1
SELECT pts, qts, seq, last_message_box_id, date2
  FROM user_state WHERE user_id = :u FOR UPDATE;
-- fn(b): чтения строк U через b.Tx(), проверки по прочитанному, затем записи. Например, для отправки:
INSERT INTO messages (...) VALUES (...);          -- обычный INSERT: дубликат ключа = ошибка
INSERT INTO dialogs (...) VALUES (...) AS new
  ON DUPLICATE KEY UPDATE top_message = GREATEST(dialogs.top_message, new.top_message), date2 = new.date2,
  unread_count = dialogs.unread_count + new.unread_count, ...;
INSERT INTO hash_tags (...) VALUES (...);
-- конец fn
INSERT INTO user_pts_updates (user_id, pts, pts_count, update_type, update_data, date2)
  VALUES (...), (...);                            -- все строки журнала одним запросом
UPDATE user_state SET pts = :pts, last_message_box_id = :box_id, date2 = :now
  WHERE user_id = :u AND pts = :old_pts AND last_message_box_id = :old_box_id;  -- RowsAffected = 1, иначе откат
COMMIT;                                           -- под своим пределом времени, см. ниже
-- после commit: сброс ключей кэша, публикация Result.Events
```

**Выделение и проверка журнала.**
- Выделение происходит в памяти, под блокировкой строки `user_state`, и становится видимым только при commit вместе с журналом. Сожжённых `pts` не бывает. Перед commit `Run` проверяет: сумма `pts_count` строк журнала равна `pts_new − pts_old`, и строки идут подряд.
- Последний `UPDATE` условный: он сверяет на стороне БД, что счётчики не изменились с начала транзакции, и требует `RowsAffected = 1`. Это настоящая проверка И2/И3 в базе, а не сверка памяти с памятью. Если ничего не выделено, `UPDATE` не выполняется.
- **И3** следует из блокировки. Следующее выделение для U ждёт commit предыдущего. InnoDB в MySQL 8.0 убирает транзакцию из списка активных для MVCC раньше, чем снимает её блокировки, поэтому изменения видны до того, как следующая транзакция получит строку.
- Сначала `INSERT … ON DUPLICATE KEY UPDATE`, потом `SELECT … FOR UPDATE`: первый запрос атомарно создаёт строку или берёт X-блокировку существующей. Обратный порядок (`SELECT … FOR UPDATE`, а при отсутствии строки `INSERT`) даёт гонку двух первых записей: при REPEATABLE READ — взаимоблокировку на gap-блокировках, при READ COMMITTED — ошибку дубликата у проигравшего. `INSERT IGNORE` берёт S-блокировку на существующей строке, и её повышение до X ведёт к взаимоблокировке.

**Что делает `fn`.**
- Внутри `fn` любой SQL, включая чтения, идёт только через `b.Tx()`. Для чтений строк ящика рядом с генерированными DAO пишутся `Select…Tx`-методы в том же стиле (генератора dalgen в репозитории нет) или используется `tx.QueryRow`/`tx.QueryRows`. Чтение через пул внутри `fn` не видит собственных незакоммиченных записей и занимает второе соединение из пула, которого InnoDB не видит: 64 одновременных `Run`, каждый ждущий второе соединение, повисают.
- Все проверки, которые зависят от состояния ящика, выполняются внутри `fn` по прочитанному под блокировкой: «уже прочитано», «сообщение существует, не удалено и принадлежит отправителю», список id для удаления, новый `top_message`, `unread_count`, закреп. Снимки, полученные до `Run` (`dstMessage` от BFF, `UnreadCount`, `ReadInboxMaxId` из `dialogs`), для записи не используются. До `Run` остаются только проверки, не зависящие от ящика: параметры запроса, пользователи, участие в чате, блокировка, RPC в другие сервисы.
- Монотонные поля только растут: `read_inbox_max_id`, `read_outbox_max_id`, `top_message` при вставке (`GREATEST` в SQL плюс проверка в `fn`). `top_message` уменьшается только при удалении и тогда вычисляется чтением через `b.Tx()`.
- Столбцы ящика в `dialogs` (`top_message`, `read_inbox_max_id`, `read_outbox_max_id`, `unread_count`, `unread_mentions_count`, `pinned_msg_id`) пишутся только внутри `Run`.
- Бизнес-пустышка (нечего удалять, уже прочитано, копии для правки нет) — `fn` возвращает `nil` до первой записи, ничего не выделив. Бизнес-ошибка (`MSG_ID_INVALID` и т. п.) — `fn` возвращает её, `Run` откатывает и не повторяет.
- Внутри `fn` нет сетевых вызовов: RPC, Kafka и Redis выполняются до `Run` или после commit (`Emit`, `InvalidateCache`). Блокировка держится миллисекунды.
- Вложенный `Run` запрещён. `Run`, получивший контекст с меткой (`b.Context()`), сразу возвращает ошибку. Если вложенный вызов получил внешний контекст без метки, внутренняя транзакция упрётся в блокировку внешней и через `innodb_lock_wait_timeout` получит 1205, а не повиснет. Статическая проверка (п. 14) ловит такие вызовы до запуска.
- `fn` получает только `Box`, но замыкание может захватить `Dao`. Поэтому изоляция от пула обеспечивается статической проверкой и тестом на пуле из одного соединения (п. 14), а не типами.

**Ошибки запросов.** `Box.Tx()` — это `*sqlx.Tx` marmota поверх обёртки сессии go-zero, которая запоминает первую ошибку любого запроса (кроме `sql.ErrNoRows` у чтения одной строки: это результат, а не сбой). После первой ошибки обёртка больше не отправляет запросы на сервер и сразу возвращает ту же ошибку. Если в попытке была ошибка запроса, `Run` не делает commit, что бы ни вернула `fn`, и выполняет `ROLLBACK`. Это закрывает частичный commit: после 1213 InnoDB уже откатил транзакцию и снял блокировки, соединение работает в autocommit, и каждый следующий запрос того же `*sql.Tx` коммитился бы сразу, без блокировки `user_state`. После 1205 при `innodb_rollback_on_timeout=OFF` (значение по умолчанию) откатывается только сам оператор, транзакция остаётся открытой; явный `ROLLBACK` её завершает.

**Своя транзакция.** `Run` не использует `TransactCtx` go-zero. Он открывает транзакцию сам: `RawDB().BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})`, оборачивает `*sql.Tx` в `sqlx.NewSessionFromTx` и записывающую обёртку, а из неё делает `*sqlx.Tx` marmota. Для этого в копию marmota (`third_party/teamgram-marmota`, ADR 0009: первый коммит — нетронутый upstream v0.2.0, затем коммит `third_party(marmota):`) добавляется экспортируемый конструктор `sqlx.NewTx(ctx, sqlx.Session) *Tx` с пометкой `// pluma:`. Та же копия понадобится задаче 003 (`pkg/mq/consumer_group.go`). Следствия:
- уровень изоляции — свойство примитива, а не DSN: любой сервис, вызвавший `Run`, работает в READ COMMITTED. Каждый запрос видит последнее закоммиченное состояние, сериализацию по пользователю даёт блокировка `user_state`, gap-блокировок меньше;
- контекст привязан к `BEGIN`;
- автомат отключения пула go-zero не видит транзакций ящика: бизнес-ошибки и таймауты блокировок одного пользователя не отключают пул для остальных.

**Время.** Дедлайн — общий на все попытки, а не на попытку.

| Параметр | Значение | Где задаётся |
|---|---|---|
| `innodb_lock_wait_timeout` (сессия) | 2 с: ожидание блокировки обрывает сервер ошибкой 1205 | DSN всех пулов, которые вызывают `Run` (msg, biz); проверяет `Check` |
| попытки `Run` | до 3, пауза 10–50 мс со случайным разбросом | `pkg/userbox` |
| бюджет `Run` | 6 с на все попытки вместе с паузами; запасной ограничитель, обычно попытку прерывает сервер через 2 с | `context.WithTimeout` в `Run` |
| `COMMIT` | отдельный предел 5 с, не отменяется дедлайном запроса (`context.WithoutCancel`) | `pkg/userbox` |
| проверка исхода `COMMIT` | 5 с, новое соединение | `pkg/userbox` |
| публикация событий после commit | 10 с на событие, 20 с на все события одной транзакции; после таймаута события того же топика в этой транзакции не отправляются | `userbox.Publish` (`EventTimeout`, `PublishTimeout`) |
| блокировка идемпотентности | 60 с > 5 + 16 + 20 + 5 + 5 с (сброс кэша, `Run` с проверкой `COMMIT`, публикация, сброс кэша, кэш ответа) | п. 6, `sendOnceSection` в msg |
| дедлайн RPC с транзакцией ящика | 0 (нет) или не меньше операции: biz и `BizServiceClient` в bff — 30 с (`dialog.MaxBoxOpTime` = 26 с), msg и `MsgClient` — 0 | `userbox.CheckRPCTimeout` при старте |
| повтор обработчика inbox до 003 | до 30 с, backoff 0,1 → 5 с | п. 9 |

Отменённый запрос, который ждёт блокировку, сервер замечает только после окончания ожидания. Сессионный `innodb_lock_wait_timeout` ограничивает это время двумя секундами: после отмены контекста серверная транзакция живёт не дольше 2 с и потом откатывается.

**Повторы.** 1213 (deadlock) и 1205 (lock wait timeout) после явного `ROLLBACK` означают, что ничего не записано. Транзакция повторяется целиком, `fn` вызывается заново на чистом `Box`: обновления, события, выделенные id и ключи кэша неудачной попытки отбрасываются и не публикуются. Повторы ограничены числом попыток и бюджетом.

**Неопределённый `COMMIT`.** Если `COMMIT` вернул ошибку (обрыв соединения, истёк предел), неизвестно, применился ли он. `Run` выясняет это на новом соединении:
1. открывает транзакцию и выполняет `SELECT pts FROM user_state WHERE user_id = :u FOR UPDATE`. Если старая транзакция ещё завершается на сервере (fsync), она держит X-блокировку строки, и запрос ждёт её исхода. Ожидание ограничено `innodb_lock_wait_timeout`;
2. если `pts < pts_new` — не применился (И4: `pts` не уменьшается);
3. иначе сравнивает свои строки журнала с записанными: `SELECT COUNT(*) FROM user_pts_updates WHERE user_id = :u AND pts = :p AND pts_count = :c AND update_type = :t AND update_data = CAST(:d AS JSON)` для каждой строки. Сравнение JSON семантическое, на сервере, потому что MySQL нормализует `JSON` при записи. Все совпали — применился, иначе на этих `pts` записано другое — не применился.

Применился — `Run` продолжает как после успеха: ответ, сброс кэша, `Emit`. Не применился — это повторяемая ошибка, как 1213. Если исход выяснить не удалось (1205, нет соединения), `Run` возвращает `ErrCommitUnknown`, считает его в метрике и пишет в лог `user_id` и диапазон `pts`; такая ошибка не повторяется. `Run` без строк журнала (biz, п. 3) исход не проверяет и возвращает `ErrCommitUnknown`: такие операции идемпотентны, повтор даёт то же состояние. Операции msg без журнала ничего не пишут (бизнес-пустышки).

**Настройки сервера.** `Check` при старте сервиса проверяет: сессионный `innodb_lock_wait_timeout` не больше 2 с; `innodb_flush_log_at_trx_commit = 1`; при включённом binlog — `binlog_format = ROW` (READ COMMITTED требует построчный binlog) и `sync_binlog = 1`. Клиент видит `pts` сразу после commit, поэтому ослабленная долговечность при сбое MySQL повторила бы №1. При несовпадении сервис не запускается, ошибка называет переменную. Требования записываются в `docs/ops`.

**Наблюдаемость.** `pkg/logpolicy` отключает логи и метрики SQL go-zero (`sqlx.DisableLog()`), поэтому `Run` пишет свои:
- гистограммы `userbox_tx_seconds` и `userbox_lock_wait_seconds` (время до возврата блокирующего `INSERT … ON DUPLICATE KEY UPDATE` и `SELECT … FOR UPDATE`);
- счётчики `userbox_tx_retries_total{code="1213|1205|commit"}`, `userbox_tx_errors_total{code}` и `userbox_commit_unknown_total{outcome="applied|not_applied|unknown"}`;
- в msg — `msg_post_commit_errors_total{kind="publish|cache|idempotency"}`, `msg_inbox_handler_errors_total{method}` и `msg_idempotency_bypass_total`.

При каждом откате в лог идут номер ошибки MySQL и `user_id`. В логах только идентификаторы и `pts`, без содержимого (`tools/logaudit`).

### 3. Порядок блокировок

1. В любой транзакции, которая меняет ящики, первыми блокируются строки `user_state` всех затрагиваемых пользователей. Порядок — по возрастанию `user_id`, для каждого пара запросов из п. 2.
2. Дальше транзакция трогает только строки этих пользователей. `UPDATE` и `DELETE` идут по равенству первичного или уникального ключа. Набор строк сначала находится чтением через `b.Tx()`, затем меняется по списку ключей. Блокирующих чтений по диапазону (`SELECT … FOR UPDATE`, `UPDATE … WHERE` по неуникальному индексу или с `NOT IN`) нет.
3. В 002 каждая транзакция блокирует ровно одного пользователя. Рассылка другим пользователям, в том числе участникам basic group, — это отдельные транзакции в inbox-консьюмере, по одной на получателя. Операция над двумя пользователями без журнала (biz `setHistoryTTL`, `setChatTheme`) выполняется двумя `Run` по возрастанию `user_id`. Ошибка второй возвращается клиенту, повтор идемпотентен. Правило п. 1 действует для будущих операций, которым понадобится атомарно менять несколько ящиков.
4. Если `Run` меняет несколько строк одной таблицы, он делает это в порядке возрастания ключа (`peer_dialog_id`, `user_message_box_id`), а не в порядке обхода map.
5. Любая транзакция, которая меняет больше одной строки `dialogs`, `saved_dialogs` или `messages` одного пользователя, идёт через `Run`. В 002 на него переводятся biz `dialog.reorderPinnedDialogs`, `reorderPinnedSavedDialogs`, `setHistoryTTL` и `setChatTheme`: блокировка `user_state` первой, диапазонный `UPDATE … NOT IN (…)` заменяется чтением и построчными `UPDATE` по ключу, проглоченные ошибки исчезают. В 002 журнала у них нет, в задаче 010 к тем же `Run` добавятся seq-обновления (`updatePinnedDialogs` и т. п.). Остальные писатели этих таблиц (черновики, настройки уведомлений, папки) — одиночные autocommit-запросы по первичному ключу. Они держат одну блокировку и не могут замкнуть цикл. Остаточные взаимоблокировки на проверке дубликатов уникальных индексов InnoDB обнаруживает сам, и `Run` их повторяет.

### 4. Путь записи по операциям

Общая схема: проверки и RPC, не зависящие от ящика (пользователи, чат и его участники, блокировка, `RemakeMessage`, snowflake для `dialog_message_id`) → `Run(инициатор)` → commit → `Emit` → ответ RPC с закоммиченным `pts`.

**Правила событий.**
- Каждое inbox-событие адресовано ровно одному пользователю и обрабатывается одной `Run`. Получатель — поле `user_id` события, инициатор — `from_id`. Где этих полей нет (события удаления и истории, `readOutboxHistory`, `readMediaUnreadToInboxV2`), они добавляются; у `updatePinnedMessage` и `unpinAllMessages`, где `user_id` сейчас означает инициатора, инициатор переносится в `from_id`. Ключ Kafka — `user_id` события, MQ-клиент берёт ключ только из него. Порядок событий одного получателя — это порядок одной партиции при любом числе партиций `Inbox-T`.
- Рассылку по участникам выполняет производитель (msg-RPC), как уже сделано для отправки. Многопользовательских обработчиков (`DeleteInboxMessages` по всем участникам) нет.
- Производитель не адресует событие инициатору. Обработчик отвергает событие с `user_id = from_id` (лог и `msg_inbox_handler_errors_total`), не выполняя его.
- Одно событие — одна `Run` со всем содержимым: альбом или пересылка k сообщений (`BoxList`) пишется у получателя одной транзакцией.
- `Emit` публикуются синхронно, в порядке добавления, после commit и до возврата ответа RPC или обработчика. Фоновой публикации (`threading2.WrapperGoFunc` в msg `readHistoryV2` и BFF `readMessageContents`) нет. Session выполняет RPC одного ключа последовательно (`rpcRunLoop`), поэтому отправка и следующая правка с одного устройства публикуются в том же порядке.
  Публикация идёт сразу после commit, до сброса кэша и шагов идемпотентности в Redis. Две транзакции одного пользователя, закоммиченные одна за другой (отправка с двух устройств), публикуются в порядке commit, если публикация первой не медленнее всей второй транзакции; гарантии нет: порядок партиции — это порядок публикации, а не commit. Порядок commit по инициатору гарантирует relay задачи 003 (п. 13).
- Если публикация события получателю R не удалась, остальные события этого `Result` для R не публикуются (иначе R получил бы их без предыдущего), а события другим получателям публикуются. Каждая неудача — `msg_post_commit_errors_total{kind="publish"}` и строка лога с методом, получателем и `dialog_message_id`, без содержимого.
- Побочные эффекты после commit выполняются в контексте, отвязанном от запроса (`context.WithoutCancel`), с пределом 10 с на событие. Продюсер Kafka контекста не принимает: msg ждёт ответа брокера не дольше предела события, отправка продолжается в фоне, и событие, засчитанное как неопубликованное, может всё же дойти. Если событие упёрлось в предел, остальные события того же топика в этой транзакции не отправляются (брокер не отвечает), а все события одной транзакции вместе ограничены 20 с. `Destroy` msg ждёт выполняющиеся запросы `MaxRequestTime`, а при закрытии продюсеров — ещё до 10 с незавершённые отправки.

| RPC msg | Транзакция ящика инициатора | События после commit, по порядку |
|---|---|---|
| `sendMessageV2`, `pushUserMessage` (тип 0); личный чат и basic group; 1..k сообщений | k × (id в ящике, `INSERT messages`, `hash_tags`, `updateNewMessage`); upsert `dialogs`; `saved_dialogs` при отправке себе | `updatesNotMe`; одно `sendUserMessageToInboxV2` со всеми k каждому получателю, кроме инициатора и заблокировавших |
| `editMessageV2` | своя копия читается через `b.Tx()`: есть, не удалена, исходящая; `UPDATE messages`, `hash_tags`, `updateEditMessage` | `updatesNotMe`; `editMessageToInboxV2` каждому получателю |
| `deleteMessages` | id читаются через `b.Tx()` (не удалённые, по возрастанию); мягкое удаление; новый `top_message` затронутых диалогов чтением через `b.Tx()`; `updateDeleteMessages` с `pts_count` = числу удалённых | `updatesNotMe`; при revoke — `deleteMessagesToInbox` каждому получателю (`dialog_message_id`) |
| `deleteHistory` | порция до 1000 id, прочитанных через `b.Tx()`; удаление; правка служебного сообщения и `updateReadHistoryInbox` по текущей логике; `dialogs`; `updateDeleteMessages` с `pts_count` = числу удалённых; без `just_clear` последняя порция удаляет строку `dialogs` в той же транзакции. Ответ `messages.affectedHistory` с `offset > 0`, пока остались сообщения | `updatesNotMe`; при revoke — `deleteUserHistoryToInbox` / `deleteChatHistoryToInbox` каждому получателю, на каждой порции |
| `readHistoryV2` | `dialogs` через `b.Tx()`; уже прочитано — пустышка; `read_inbox_max_id` только растёт; `unread_count` = число непрочитанных входящих после `max_id` (чтение через `b.Tx()`); `updateReadHistoryInbox` с `still_unread_count = unread_count` | `updatesNotMe`; `readOutboxHistory`: в личном чате — собеседнику; в basic group — каждому участнику, у которого есть исходящие в `(old_max, max]` |
| `readMessageContents` | id читаются через `b.Tx()`; флаги `media_unread`/`mentioned`/реакций снимаются только там, где стоят; одна строка `updateReadMessagesContents` с `messages` = изменённым id и `pts_count` = их числу. Пустой результат — пустышка, ответ с текущим `pts` | `updatesNotMe`; `readMediaUnreadToInboxV2` каждому отправителю изменённых сообщений |
| `updatePinnedMessage` | `messages.pinned`, `dialogs.pinned_msg_id`, `updatePinnedMessages` (P); если закреп с уведомлением — в той же `Run` служебное сообщение `messageActionPinMessage` (id в ящике, `INSERT messages`, `updateNewMessage`, P+1). Ответ — `Updates` с `updatePinnedMessages`, `updateMessageID` и `updateNewMessage` | `updatesNotMe` с обоими обновлениями; каждому получателю `updatePinnedMessage`, затем `sendUserMessageToInboxV2` (один ключ, в этом порядке) |
| `unpinAllMessages` | порция до 1000 закреплённых id через `b.Tx()`; `messages.pinned`, `dialogs.pinned_msg_id`, `updatePinnedMessages`. Ответ `messages.affectedHistory` с `offset > 0`, пока остались | `updatesNotMe`; `unpinAllMessages` каждому получателю |
| `deletePhoneCallHistory` | порция до 1000 id через `b.Tx()`; удаление; `dialogs`; `updateDeleteMessages` с `pts_count` = числу удалённых. Ответ `messages.affectedFoundMessages` с `offset > 0`, пока остались | `updatesNotMe`; при revoke — `deleteMessagesToInbox` с `peer_type` = пользователь каждому собеседнику, по его сообщениям |

| Событие inbox (Kafka) | Транзакция ящика получателя | После commit |
|---|---|---|
| `sendUserMessageToInboxV2` | k × (id в ящике, `INSERT messages`, `hash_tags`, `updateNewMessage`); upsert `dialogs` (`unread_count + k`, упоминания); `updateReadHistoryInbox` для `messageActionChatMigrateTo` | push (`sync.pushUpdates` или бот) |
| `editMessageToInboxV2` | копия по `dialog_message_id` через `b.Tx()`; нет копии — пустышка; иначе `UPDATE messages`, `hash_tags`, `updateEditMessage` | push |
| `deleteMessagesToInbox`, `deleteUserHistoryToInbox`, `deleteChatHistoryToInbox` | id копий через `b.Tx()`; порциями до 1000, по `Run` на порцию, пока не исчерпано; удаление, `dialogs`, `updateDeleteMessages`; у `deleteUserHistoryToInbox` последняя порция удаляет строку `dialogs` (у `deleteChatHistoryToInbox` строка остаётся: получатель остаётся в группе) | push после каждой порции |
| `readOutboxHistory` | `message_read_outbox`, `dialogs.read_outbox_max_id` (только растёт), `updateReadHistoryOutbox` | push |
| `readMediaUnreadToInboxV2`, `updatePinnedMessage`, `unpinAllMessages` | изменение строк, найденных через `b.Tx()`; журнал | push |

- `deleteHistory`, `unpinAllMessages` и `deletePhoneCallHistory` удаляют порциями, как Telegram: клиенты (tdesktop, Android, tdlib) повторяют вызов, пока `offset > 0`. Одна транзакция с неограниченным списком id и JSON на десятки тысяч id не уложилась бы в бюджет, а блокировка задерживала бы входящие пользователя. В строке журнала `updateDeleteMessages` `messages` — ровно удалённые id, `pts_count` — их число (сейчас ветка без `just_clear` пишет N−1 и сжигает `pts`).
- `dialogs`, затрагиваемые операцией ящика, пишутся в той же транзакции через `*Tx`-методы `DialogsDAO`, а не через RPC `dialog.insertOrUpdateDialog`. Заодно закрывается №13: прочтение и счётчики непрочитанного меняются атомарно.
- **Кэш `dialogs`.** Ключи считает общий помощник в пакете `app/service/biz/dialog/dialog`, им пользуются biz и msg. По `RowsAffected` upsert-а (1 — строка создана, 2 — изменена) он возвращает ключ строки (`GetDialogCacheKeyByPeer`), а при создании — ещё списочные ключи (`GetCacheKeyByPeerType`, `GetNotPinnedDialogIdListCacheKey`, `GetFolderNotPinnedDialogIdListCacheKey`). Ключи строк известных диалогов сбрасываются до `Run` и после commit (двойное удаление сужает окно, в котором читатель кладёт в кэш старое значение), списочные — после commit. Ошибка сброса — `msg_post_commit_errors_total{kind="cache"}`, операция успешна. DSN не должен включать `clientFoundRows`, иначе `RowsAffected` upsert-а не различает создание и изменение.
- **Удаление строки `dialogs`** — только внутри `Run`, вместе с последней порцией истории (иначе входящее сообщение, закоммиченное между порцией и удалением, осталось бы без строки диалога, а следующее создало бы строку с неверными счётчиками). BFF строк `dialogs` не удаляет: `messages.deleteHistory` и `messages.deleteChat` повторяют `msg.deleteHistory`, пока `offset > 0`. biz `dialog.deleteDialog` (исключение участника из basic group) — тоже транзакция ящика. Сброс кэша — ключ строки и все списки диалогов пользователя (`dialog.DeleteCacheKeys`).
- **Счётчики `dialogs` без чтения всего диалога.** Индекс `messages (user_id, dialog_id1, dialog_id2, deleted, user_message_box_id)` (`migrate-20261022-dialog-index.sql`) отдаёт верхнее сообщение одним чтением индекса и непрочитанные — диапазоном после `read_inbox_max_id`. `unread_count` при прочтении — число входящих после `max_id`, при удалении уменьшается на удалённые непрочитанные входящие; `unread_mentions_count` и `unread_reactions_count` меняются на флаги изменённых строк. Пересчёт по всем сообщениям диалога держал бы блокировку ящика ~100 мс на диалоге в 100 000 сообщений на каждом прочтении.
- Исправления по ходу: №11 — в `sendChatOutgoingMessageV2`, `sendChatOutgoingMessageList` и `editChatOutgoingMessageV2` проглатывались ошибки (`return err` вместо `err2`, проверка `err` вместо `err2`). Ошибки до commit теперь возвращаются, ответ после commit всегда строится из закоммиченного ящика. №16 — пустой `deleteMessages` отдаёт `pts` пользователя `in.UserId`, а не `in.PeerId`.

### 5. Какой путь остаётся, что удаляется

Остаётся форма V2: синхронный ответ RPC с `pts` и рассылка получателям через Kafka. Но ящик инициатора пишется целиком в RPC, внутри одной транзакции.

Удаляется:
- весь V3: флаг `-usev3` (`msg/internal/core/vars.go`), `sendUserOutgoingMessageV3`, `sendMessageToOutboxV3`, `SendUserMessageV3`, `msgReadHistoryV3`, ветки `isUseV3` в inbox, асинхронный ответ через `sync.pushRpcResult` из msg и inbox. Сам `sync.pushRpcResult` не на пути записи и не трогается;
- события `Out` (ветки `in.Out` в `sendUserMessageToInboxV2` и `editMessageToInboxV2`) и событие `inbox.readInboxHistory`: их работа выполняется в транзакции RPC;
- `msg.readHistory` (вызывающих нет, BFF вызывает `readHistoryV2`) и событие `inbox.updateHistoryReaded` вместе с обработчиком (производителя нет);
- из схемы inbox (`inbox.tl.proto`): поле `out` и поля V3 (layer, server_id, session_id, client_req_msg_id). Схема всё равно перегенерируется ради поля получателя (п. 4): `protoc` по `build2.sh`, записи кодека правятся вручную, как для idgen (п. 6). Формат событий меняется, поэтому `Inbox-T` должен быть пуст при обновлении (п. 10);
- раздельная запись журнала: `AddToPtsQueue`, `AddToPtsQueueE`, `persistPtsUpdate`, предварительная строка журнала в `sendMessageToOutboxV2` и `editOutboxMessageV2`, мёртвые `AddToPtsQueue` и `AddSeqToUpdatesQueue` в sync (sync больше не ходит в idgen и не пишет журнал);
- push `updateReadMessagesContents` из BFF `messages.readMessageContents` (его делает msg, иначе другие устройства получили бы два обновления с одним `pts` и разными `messages`) и чтение `CurrentPtsId` в его раннем выходе: пустой ответ берётся из msg;
- остатки V1 без вызовов: `sendMessageToOutbox`, `SendUser/ChatMessage`, `SendUser/ChatMultiMessage`, `sendMessageToOutboxV1(NoPts)`, `SendUser/ChatMultiMessageToInbox`, `EditUser/ChatInboxMessage`, inbox-события без производителей (`editUserMessageToInbox`, `editChatMessageToInbox`, `readUserMediaUnreadToInbox`, `readChatMediaUnreadToInbox`, `updatePinnedMessageV2`) вместе с ветками диспетчера `inbox/internal/server/mq/mq.go`;
- отдельный gRPC-сервер inbox (`app/messenger/msg/inbox/cmd`, `inbox/internal/server/server.go`, `inbox/internal/server/grpc`). Он не собирается и не запускается: inbox работает только как консьюмер внутри `msg`;
- вызовы `dialog.insertOrUpdateDialog` из msg и inbox. Если у RPC не остаётся вызывающих, он удаляется вместе с обработчиком так же, как методы idgen;
- `InsertOrReturnIdTx` (`ON DUPLICATE KEY UPDATE id = last_insert_id(id)`) на пути записи. Вместо него — обычный `InsertTx`.

Главный рычаг: из клиента idgen удаляются методы `pts`, id в ящике, `seq` и `qts`. Любой забытый путь выделения перестаёт компилироваться.

### 6. Redis и idgen

- **Уходят из Redis полностью:** `pts` пользователя, id в ящике, `seq`, `qts` (ключи `pts_updates_ngen_*`, `message_box_ngen_*`, `seq_updates_ngen_*`, `qts_updates_ngen_*`). Кэша счётчиков нет, поэтому прогревать и восстанавливать нечего. ADR 0005 допускает Redis как ускорение, но здесь оно не нужно: блокировка строки и один `UPDATE` в транзакции, которая всё равно пишет сообщение, стоят доли миллисекунды.
- **Остаются в idgen:** snowflake (`idgen.nextId`, `idgen.nextIds`) для `dialog_message_id`, id фото и документов, `grouped_id`. Они и сейчас не в Redis: генерируются в памяти процесса idgen (`NodeId`), уникальны без хранения и не обязаны идти подряд, так что пропуски безвредны. Остаточный риск — откат системных часов при перезапуске idgen. Он закрывается синхронизацией времени (задача 004).
- Неиспользуемые ключи (`channel_pts_*`, `channel_message_box_*`, `scheduled_*`, `bot_updates_*`, `story_*`, `message_data_*`) удаляются из клиента. Будущие счётчики каналов — строка в MySQL с тем же примитивом (аудит, раздел о каналах, п. 2).
- Методы idgen `getCurrentSeqId`, `setCurrentSeqId`, `getNextSeqId`, `getNextNSeqId`, `getNextIdValList` и `getCurrentSeqIdList` удаляются из `idgen.tl.proto`. Go-код перегенерируется `protoc` (`build2.sh`, версии из заголовков файлов), записи кодека mtprotoc удаляются вручную. `SeqIDGen` уходит из конфига и из `teamgramd/etc*/idgen.yaml`: idgen больше не подключается к Redis.
- Клиент: `NextId(ctx) (int64, error)` и `NextIds(ctx, n) ([]int64, error)`. Ошибка передаётся вызывающему (msg, dfs, bff `sendMultiMedia`). Нулевые id больше не возвращаются (№2).
- **Идемпотентность отправки до задачи 003.** `DoIdempotent` marmota заменяется обёрткой в msg dao поверх экспортируемых методов `Idempotent` marmota (`TryGetCacheValue`, `Lock`, `Unlock`, `SetCacheValue`). Порядок: кэш → блокировка (TTL 60 с, см. таблицу времени в п. 2) → повторная проверка кэша (запрос, державший блокировку, мог закэшировать ответ и снять её между проверкой и захватом) → `Run` → commit → `Emit` → `SetCacheValue` → снятие блокировки → ответ. Каждый шаг в Redis ограничен 5 с. Ошибка `SetCacheValue` после commit — лог и `msg_post_commit_errors_total{kind="idempotency"}`, ответ успешный: commit уже состоялся, и ошибка клиенту привела бы к повтору и второму сообщению. Недоступность Redis на чтении кэша или взятии блокировки не валит отправку: она выполняется без защиты от повтора, с метрикой `msg_idempotency_bypass_total`. Redis по ADR 0005 — кэш, а постоянная дедупликация `random_id` появится в 003.
- В Redis остаются: кэш `dialogs` и `users` (sqlc), окно идемпотентности отправки до задачи 003, онлайн-сессии (`status`), коды входа. Их потеря не трогает `pts` и id: кэш пересобирается из MySQL. Старые ключи счётчиков после обновления никто не читает, их можно удалить; для отката они пишутся заново (п. 11).

### 7. `getState` и `getDifference` читают только закоммиченное

- `updates.getStateV2`: `SELECT pts, qts, seq, date2 FROM user_state WHERE user_id = ?`, без строки — база. `pts` закоммичен, ничего не сжигается. `qts` = `user_state.qts`. `seq` = `user_state.seq`, но отображение в ответе (сейчас 0 → −1) и `date = now()` не меняются до задачи 010.
- `updates.getDifferenceV2(pts = L)`:
  1. `S = userbox.Read(user)`;
  2. `SELECT user_id, pts, pts_count, update_type, update_data FROM user_pts_updates WHERE user_id = ? AND pts > ? AND pts <= ? ORDER BY pts LIMIT ?` с параметрами `(user, L, S.pts, N)`;
  3. если строк N — `differenceSlice`, `intermediate_state.pts` = последняя строка. Иначе, если `L < S.pts`, — `difference` с `state.pts = S.pts`, даже при пустом списке (так клиент переходит через скачок миграции из п. 10). Иначе — `differenceEmpty`.

  Корректность: все строки с `pts ≤ S.pts` закоммичены раньше, чем стало видно `S` (И3), значит выборка полная. Верхняя граница отсекает строки, закоммиченные между шагами 1 и 2. Их клиент получит push-ем или следующим `getDifference`. И3 держится только на одном сервере: шаги 1 и 2 читают мастер (`BoxDB` и `Master()` marmota), реплики `ReadDSN` для журнала и состояния не используются — отстающая реплика отдала бы `state.pts` без строк до него, и клиент потерял бы их навсегда.
- Из biz updates уходят чтения `CurrentPtsId` и `CurrentSeqId`. BFF (`messages.deleteHistory`) и msg берут текущий `pts` через `updates.getStateV2` или `userbox.Read`, а не через idgen.
- Остальное — `differenceTooLong`, `seq`, `qts`, `unread_count`, клиент впереди сервера — задача 010.

### 8. Ограничения уникальности

- `user_pts_updates`: `UNIQUE (user_id, pts)` вместо `KEY user_id (user_id, pts)`, имя индекса то же. Добавляется здесь.
- `messages`: `UNIQUE (user_id, user_message_box_id)` уже есть. Запись идёт обычным `INSERT`, поэтому дубликат — это ошибка операции, а не молча потерянное сообщение (№1).
- `user_state`: первичный ключ `user_id`.
- **В задачу 003:** `UNIQUE (user_id, dialog_message_id)` в `messages` — повторная доставка события не создаёт вторую копию и не выделяет `pts`. И постоянная дедупликация `random_id` у отправителя: у служебных сообщений `random_id = 0`, поэтому простой `UNIQUE` невозможен, схему выбирает 003. Причина переноса: это идемпотентность доставки, она имеет смысл вместе с ретраями и коммитом offset. После удаления старого `sendMessageToOutbox` путь записи 002 по `random_id` не читает.

### 9. Отказы

- **До commit** — ошибка idgen, MySQL, дубликат ключа, нарушение проверки журнала или условного `UPDATE`, ошибка валидации. Транзакция откатывается, ничего не записано, `pts` и id не потрачены. RPC возвращает ошибку (`INTERNAL` для инфраструктурных).
- **Бизнес-пустышка** — успех без изменений и без `pts`.
- **Неопределённый `COMMIT`** — см. п. 2: применился — успех, не применился — повтор, не выяснено — `ErrCommitUnknown`.
- **После commit** — сброс кэша, кэш идемпотентности и публикация событий. Ошибка логируется и считается в `msg_post_commit_errors_total{kind}`, операция успешна. Данные закоммичены, устройства инициатора догонят через `getDifference`. Ответ RPC всегда строится из `Result` и никогда не бывает `nil`. Возвращать ошибку клиенту нельзя: повтор создал бы второе сообщение у отправителя.
- **Обработчик inbox до задачи 003.** Повторяемые ошибки (1213, 1205, неприменённый `COMMIT`, разрыв соединения до `COMMIT`) обработчик повторяет сам, до 30 с с backoff, чтобы не потерять событие при кратком сбое или горячем пользователе. Консьюмер последовательный, поэтому на это время задерживается вся партиция; при ≤100 пользователях это допустимо. `ErrCommitUnknown` не повторяется. Событие, которое так и не выполнилось, консьюмер всё равно подтверждает (№6): лог с методом, получателем и `dialog_message_id` (без содержимого) и `msg_inbox_handler_errors_total{method}`, чтобы его можно было найти и переиграть вручную. В задаче 003 этот последний шаг заменяется DLQ.
- **Остановка процесса.** `Destroy` msg останавливает приём RPC (`GracefulStop`), дожидается выполняющихся RPC и обработчиков (их `Emit` синхронны), закрывает консьюмер и сбрасывает продюсеры. Сейчас `Destroy` вызывает только `grpcSrv.Stop()`, и перезапуск между commit и публикацией терял рассылку.
- **Выкатка.** Пока не сделана задача 003, сбой публикации после commit или потеря события в консьюмере оставляет получателя без сообщения: его нет ни в push, ни в `getDifference`, ни в истории. Поэтому 002 не выкатывается пользователям отдельно от 003; 003 идёт сразу следом. Пользователей у системы пока нет, так что ограничение ничего не стоит.

### 10. Миграция `teamgramd/deploy/sql/migrate-20261022.sql` и порядок обновления

```sql
CREATE TABLE IF NOT EXISTS `user_state` (...);   -- как в п. 1

-- Дубли (user_id, pts), оставшиеся от сбросов счётчика: оставить самую новую строку.
DELETE p1 FROM `user_pts_updates` p1
  JOIN `user_pts_updates` p2 ON p2.user_id = p1.user_id AND p2.pts = p1.pts AND p2.id > p1.id;
ALTER TABLE `user_pts_updates` DROP INDEX `user_id`, ADD UNIQUE KEY `user_id` (`user_id`, `pts`);

-- Строка каждого существующего пользователя выше всего, что мог видеть клиент.
-- Только недостающие строки: существующие не трогаются, поэтому повторный прогон ничего не меняет.
INSERT INTO `user_state` (`user_id`, `pts`, `last_message_box_id`, `date2`)
SELECT * FROM (
  SELECT u.id AS user_id,
         GREATEST(1, COALESCE(p.max_pts, 0)) + 1000000 AS pts,
         COALESCE(m.max_box_id, 0) + 1000000 AS last_message_box_id,
         UNIX_TIMESTAMP() AS date2
  FROM (SELECT id FROM users UNION SELECT user_id FROM user_pts_updates UNION SELECT user_id FROM messages) u
  LEFT JOIN (SELECT user_id, MAX(pts) AS max_pts FROM user_pts_updates GROUP BY user_id) p ON p.user_id = u.id
  LEFT JOIN (SELECT user_id, MAX(user_message_box_id) AS max_box_id FROM messages GROUP BY user_id) m ON m.user_id = u.id
) s
ON DUPLICATE KEY UPDATE user_id = user_id;
```

- **Пустая база** (свежая установка, `docker-entrypoint-initdb.d`): создаются таблица и индекс, строк нет. Пользователи из `z_init.sql` получают строку при первой записи.
- **Заполненная база.** Клиенты могли видеть значения Redis-счётчика выше максимума в MySQL: сожжённые `pts` в конце (`readMessageContents` выделяет `pts` без журнала, `deleteHistory`, сбои, `getState` при 0) и id, выданные в ответе RPC до записи строки. Хвост ничем не ограничен, поэтому запас — 1 000 000 и для `pts`, и для id в ящике. Это на порядки больше любого реального хвоста при 100 пользователях, а скачок любого размера стоит одинаково: следующее обновление устройство увидит как дыру и сделает один `getDifference` (п. 7). Запас `int32` (2,1·10⁹) это выдерживает. Скачок назад был бы фатален (№1): клиент впереди сервера отбрасывает новые обновления как применённые. Пропуск id в ящике безвреден.
- **Повторный прогон** вставляет только отсутствующие строки, дубли уже удалены, индекс пересоздаётся тем же. Учёта применённых миграций в проекте нет, поэтому миграция обязана быть повторяемой.
- **`MessageSharding`.** Миграция читает только таблицу `messages`. msg и biz при `MessageSharding ≠ 1` не запускаются.
- **Время `ALTER`.** Индекс строится по всей `user_pts_updates`, а в ней JSON-снимки сообщений. Перед окном оценить размер (`SELECT table_rows, data_length FROM information_schema.tables WHERE table_name = 'user_pts_updates'`) и время по скорости, измеренной интеграционным тестом миграции (п. 14). Число записывается в `docs/ops`.
- **Порядок обновления** (окно обслуживания; описывается в `docs/ops`):
  1. закрыть вход клиентов (остановить gnetway);
  2. дождаться нулевого лага консьюмера: `/opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9094 --describe --group Inbox-MainCommunity-S` в контейнере kafka, колонка `LAG` равна 0 по всем партициям `Inbox-T`. События старой версии (`Out`, старый формат) новая не обрабатывает;
  3. остановить сервисы и сделать резервную копию;
  4. применить миграцию;
  5. выкатить новую версию (DSN msg и biz с `innodb_lock_wait_timeout=2`, idgen без `SeqIDGen`) и запустить. `Check` не даст стартовать при неверных настройках сервера.

### 11. Откат и восстановление из резервной копии

**Откат на версию до 002** после того, как новая версия поработала. Старые бинарники возобновили бы Redis-счётчики со значений ниже `user_state`: старый `AddToPtsQueue` упёрся бы в `UNIQUE (user_id, pts)` и только записал бы это в лог, а старый `InsertOrReturnIdTx` молча не записал бы сообщение с повторным id. Поэтому:
1. остановить gnetway;
2. дождаться нулевого лага `Inbox-MainCommunity-S` (команда из п. 10): события нового формата должна обработать новая версия;
3. остановить сервисы, сделать резервную копию;
4. `pluma-admin state-export-legacy --margin 1000000`: для каждой строки `user_state` записать в Redis `pts_updates_ngen_<u>` = `pts` + запас и `message_box_ngen_<u>` = `last_message_box_id` + запас (`SET`, не `INCR`);
5. вернуть `SeqIDGen` в конфиг idgen, выкатить старую версию и запустить.

Таблица `user_state` и `UNIQUE`-индекс остаются: старый код их не читает, а счётчики выше всего записанного дубликатов не дают. Команда живёт, пока откат на версию до 002 поддерживается; её удаление — отдельное решение, записанное в STATUS.

**Повторное обновление после отката.** Строки `user_state` отстали от Redis. Миграция повторяется (ничего не меняет), затем до запуска — `pluma-admin state-raise --by 1000000`.

**`pluma-admin state-raise --by N [--user U]`** — для всех или одного пользователя `pts = GREATEST(user_state.pts, MAX(pts) журнала) + N`, `last_message_box_id = GREATEST(last_message_box_id, MAX(user_message_box_id)) + N`; недостающие строки вставляются, как в миграции. Выполняется при остановленных сервисах.

**Восстановление MySQL из резервной копии** — прямой аналог потери Redis: клиенты видели `pts` и id новее копии. После восстановления и до запуска сервисов — `pluma-admin state-raise --by 1000000`. Откат обновления через восстановление копии теряет всё, что записано после обновления, и допустим только при порче данных. Обе процедуры описываются в `docs/ops`.

**Проверка состояния.** `pluma-admin verify-pts <user>` проверяет И2 на живой базе: дыры и пересечения в журнале и равенство `state.pts` последней строке. Проверка непрерывности (сейчас `CheckPtsContinuity` в `msg/internal/dao/pts.go`) переносится в `pkg/userbox`, чтобы ею пользовались и msg, и `pluma-admin`.

### 12. Производительность и замер

- **Ожидание.** Отправка в личный чат сейчас: RPC idgen (два `INCR` и snowflake), autocommit-вставка журнала, две публикации в Kafka (`Out` и `In`). После изменения: RPC idgen (snowflake), транзакция из 7–9 запросов (с `SET TRANSACTION`) с одним commit, две публикации (`updatesNotMe` и `In`). Ожидаемая прибавка — 1–3 мс на commit и запросы, при общем времени RPC порядка 10–30 мс в Docker. На стороне получателя один commit вместо двух плюс RPC в biz, то есть быстрее. Блокировка пользователя держится 1–3 мс: больше 200 операций в секунду на пользователя, а для 100 пользователей нужно на порядки меньше. Рассылка в basic group публикует событие на каждого участника после commit, а у получателей это отдельные commit с fsync в последовательном консьюмере. Её перенос в relay — задача 003.
- **Лишний сетевой круг.** `COMMIT` отправляется запросом с контекстом (ограничение по времени), после чего `database/sql` завершает `*sql.Tx` ещё одним `COMMIT` драйвера — сервер отвечает на него без работы. Это один круг на каждую `Run`; убрать его можно только своей сессией поверх `*sql.Conn` вместо сессии go-zero поверх `*sql.Tx`. Цифры `BenchmarkRun` его включают.
- **Нагрузочный замер** — `test/load` (тег `load`) на стеке e2e (`test/e2e/run.sh up`) через `pkg/mtclient`. 100 пользователей из диапазона вымышленных номеров, каждый отправляет 20 сообщений случайным собеседникам с темпом около 1 сообщения в секунду; часть отправок — в basic group из 20 участников. Измеряется:
  - p50/p95/p99 времени RPC `messages.sendMessage`;
  - p95 задержки доставки (отправка → push у получателя), отдельно для личных чатов и для basic group;
  - потери: число сообщений в `getDifference` каждого получателя равно числу отправленных;
  - операции, завершившиеся ошибкой из-за 1213 или 1205; `userbox_tx_retries_total{code}`; `lock_deadlocks` и `lock_timeouts` из `information_schema.INNODB_METRICS` (по всему серверу, включая biz);
  - `userbox_tx_seconds` и `userbox_lock_wait_seconds`.

  База для сравнения — тот же инструмент против коммита, от которого ответвлена задача. База и новая версия замеряются на одном хосте Linux (раннер CI или сервер). Docker Desktop на macOS даёт непоказательные цифры fsync и годится только для отладки. Цифры записываются в бриф задачи.
- **Критерий:** p50 и p95 `sendMessage` и p95 задержки доставки (личные чаты и basic group) не хуже базы более чем на 20%; 0 операций, завершившихся ошибкой из-за 1213 или 1205; повторов не больше 0,1% транзакций; потерь 0. Число взаимоблокировок фиксируется для сведения: остаточные взаимоблокировки на проверке дубликатов уникальных индексов допустимы, пока `Run` их повторяет. Дополнительно `BenchmarkRun` в `pkg/userbox` (тег `integration`): цена одной транзакции — последовательно для одного пользователя и параллельно для 100.

### 13. Точки расширения

- **Задача 003 (outbox и надёжный консьюмер).** `Box.Emit` — единственный способ вызвать побочный эффект операции. Событие — данные (топик, ключ-получатель, метод, JSON), ровно то, что сейчас отправляют MQ-клиенты. В 002 события возвращаются в `Result` и публикуются после commit. В 003 `Emit` будет вставлять строку в таблицу outbox в той же транзакции, а публиковать будет relay; обработчики не меняются. Имя таблицы должно отличаться от «outbox» ящика отправителя (`outbox.go`), например `mq_outbox`. Каждое событие уже адресовано одному получателю с его ключом, поэтому порядок relay — порядок строк outbox по получателю. В транзакции получателя перед выделением id появится проверка `(user_id, dialog_message_id)` — идемпотентный консьюмер. Повтор обработчика и запись потерянного события в лог (п. 9) заменяются коммитом offset после успеха и DLQ. Relay обязан публиковать события одного инициатора в порядке commit его транзакций (по id строки outbox или по `pts` в пределах `user_id`): в 002 публикация идёт после commit в потоке запроса, и две транзакции одного пользователя, закоммиченные подряд, могут опубликоваться в обратном порядке — тогда у получателя копии лягут в обратном порядке.
- **Задача 010 (seq).** `user_state.seq` и `Box.AddSeqUpdate(u)`: журнал `user_seq_updates(user_id, seq, update_type, update_data, date2)` с `UNIQUE (user_id, seq)` пишется в той же транзакции, что и изменение, которое он описывает. biz-обработчики закреплённых диалогов уже работают через `Run` (п. 3), им остаётся добавить seq-обновления. Проверка неопределённого `COMMIT` распространяется на строки seq-журнала. `qts` и `date2` уже в строке. Порядок блокировок из п. 3 обязателен и для них: `user_state` всегда первым.

### 14. Проверка

**Юнит-тесты** (без Docker):
- арифметика `Box`: диапазоны `pts` при нескольких обновлениях с разным `pts_count`, id в ящике, проверка журнала перед commit; повторная попытка начинается с чистого `Box` — обновления, события, id и ключи кэша первой попытки не попадают в `Result`;
- классификация ошибок: 1213, 1205 и неприменённый `COMMIT` повторяются; бизнес-ошибки, `ErrCommitUnknown` и прочие — нет;
- записывающая обёртка сессии на фейковой сессии: после первой ошибки ни один запрос не уходит дальше, `Run` не вызывает commit, даже если `fn` вернула `nil`; `sql.ErrNoRows` ошибкой не считается;
- вложенный `Run` с контекстом из `b.Context()` сразу возвращает ошибку;
- согласованность времени: бюджет `Run` + предел `COMMIT` + предел проверки исхода меньше TTL блокировки идемпотентности;
- обёртка идемпотентности на фейковом хранилище: ошибка `SetCacheValue` после commit даёт успешный ответ и метрику; ошибка Redis на чтении кэша и блокировке не мешает отправке и считается в `msg_idempotency_bypass_total`;
- MQ-клиент inbox: для каждого метода клиента (перечисляются отражением по интерфейсу, чтобы новый метод не выпал) ключ публикации равен `user_id` события;
- публикация `Emit`: после ошибки события получателю R следующие события для R не публикуются, для других получателей публикуются;
- клиент idgen передаёт ошибку и не возвращает 0;
- ответ отправки в чат при ошибке до commit — ошибка, а не `nil` (№11);
- `tools/boxaudit` (запускается `go test`, как `tools/logaudit`) по замыканиям, переданным в `userbox.Run`, и по функциям с параметром `*userbox.Box`: нет отброшенных ошибок (`_ =`, `_, _ =`, вызов без присваивания) у вызовов, возвращающих `error`; нет вызовов методов `*DAO` без суффикса `Tx`; нет обращений к RPC-клиентам (поля `*Client`), `CachedConn`, Kafka и Redis; нет вложенного `userbox.Run`. Тест самого инструмента — на фикстурах с нарушениями каждого вида.

**Интеграционные тесты** (`testcontainers`, тег `integration`, `-race`, проходят с `-count=3 -shuffle=on`):
- `pkg/userbox` на MySQL:
  - последовательные операции дают журнал без дыр, и `state.pts` равен последнему `pts`;
  - ошибка в `fn` после выделения: состояние и журнал не изменились, следующая операция получает следующий `pts`;
  - существующая строка журнала на `pts + 1` роняет операцию, ничего не записано;
  - **конкурентный тест:** N=32 горутины × 25 операций одному пользователю. `pts` идут от 2 подряд без дыр и дублей, порядок строк журнала по `id` совпадает с порядком по `pts`, id в ящике уникальны и идут подряд. Параллельный читатель в цикле берёт `Read` и строки `≤ state.pts` и ни разу не видит дыры;
  - **проглоченная ошибка:** `fn` вставляет дубликат ключа, игнорирует ошибку и продолжает писать; `Run` возвращает ошибку, в базе нет ни одной строки попытки;
  - **внедрённый 1213:** вторая транзакция блокирует строку `dialogs(U,B)` и ждёт `dialogs(U,A)`, которую держит `Run`; `Run` затем просит `dialogs(U,B)`. Вторая транзакция тяжелее (больше изменённых строк), поэтому жертвой InnoDB выбирает `Run`. Итог: `Run` повторён и успешен, журнал и `pts` только от успешной попытки, `userbox_tx_retries_total{code="1213"} = 1`. Вариант, где `fn` проглатывает 1213, тоже не оставляет ни журнала, ни сдвига `pts`;
  - **ожидание дольше бюджета:** тестовая транзакция держит `user_state(U)` `FOR UPDATE` 10 с. `Run` завершается ошибкой не позже бюджета, а через `innodb_lock_wait_timeout` после этого в `information_schema.INNODB_TRX` нет транзакции `Run`;
  - **неопределённый `COMMIT`** через TCP-прокси внутри теста (помощник в `pkg/testinfra`, без нового контейнера): (а) прокси передаёт `COMMIT` серверу, отбрасывает ответ и рвёт соединение — `Run` определяет «применился», возвращает успех, события в `Result` один раз; (б) прокси рвёт соединение, не передав `COMMIT`, — «не применился», попытка повторена, в журнале ровно один набор строк;
  - **пул из одного соединения** (`SetMaxOpenConns(1)`): все сценарии `pkg/userbox` и `msg/internal/dao` проходят. Чтение через пул внутри `fn` повисло бы до дедлайна, и тест упал бы;
  - уровень изоляции: на пуле с DSN без параметров (REPEATABLE READ по умолчанию) `SELECT @@transaction_isolation` внутри `fn` даёт `READ-COMMITTED`;
  - `Check` отказывает при `innodb_lock_wait_timeout = 50` и проходит при 2;
  - **миграция:** база в состоянии «до» (`SchemaFilesBefore`, `Reset`), пользователи, журнал с дублями и сообщения. После `ApplySQLFile`: состояние = максимум + 1 000 000, дубли удалены (осталась строка с большим `id`), индекс уникальный. Повторный `ApplySQLFile` не меняет ни одной строки `user_state`, в том числе после имитации работы новой версии. На пустой базе строк нет. Тест печатает скорость построения индекса (строк в секунду) для оценки окна. Тест полной схемы в `pkg/testinfra` проверяет таблицу и индекс;
  - **`pluma-admin`** на MySQL и Redis: `state-raise` поднимает отставшие строки и вставляет недостающие; `state-export-legacy` пишет ключи с запасом; `verify-pts` находит внесённую дыру.
- `app/messenger/msg/internal/dao` на MySQL и Redis; snowflake и публикаторы заменены фейками, фейковый публикатор раскладывает события по ключам и отдаёт их фейковому консьюмеру в порядке каждого ключа:
  - отправка → запись у получателя → правка → удаление → прочтение дают согласованные `messages`, `dialogs` и журнал;
  - **FLUSHALL Redis между отправками**: id и `pts` продолжают расти, все сообщения на месте;
  - конкурентная смесь входящих, собственных отправок и прочтений одного пользователя с двух «устройств»: журнал без дыр; `read_inbox_max_id` не уменьшается; `unread_count` в `dialogs` и `still_unread_count` последнего `updateReadHistoryInbox` равны фактическому числу непрочитанных входящих;
  - удаление последнего сообщения диалога: `top_message` указывает на предыдущее неудалённое;
  - `deleteHistory` диалога из 2500 сообщений: три вызова, первые два с `offset > 0`, третий с 0; в каждой строке журнала `pts_count` равен числу удалённых id; у получателя все копии удалены порциями, журнал без дыр;
  - **basic group из трёх человек**, альбом из k=3: у отправителя ровно одна копия каждого сообщения, у каждого получателя по одной, `pts` каждого получателя вырос на k одной транзакцией;
  - **порядок по получателю:** Alice отправляет в группу и сразу удаляет у всех; у Bob сообщения нет. То же для закрепа, пришедшего вслед за сообщением;
  - **закреп с уведомлением:** одна `Run`; ответ содержит `updatePinnedMessages`, `updateMessageID` и `updateNewMessage` с `pts` P и P+1; получатель получает закреп, затем служебное сообщение;
  - прочтение в группе: `readOutboxHistory` получает каждый участник, у которого есть исходящие в `(old_max, max]`, и только они;
  - `readMessageContents`: одна строка журнала с изменёнными id; повторный вызов — пустышка без `pts`;
  - Redis отказывает на `SetCacheValue`: ответ успешный, сообщение одно;
  - фейковый публикатор при каждом событии читает `user_state` отдельным соединением: `pts` события уже закоммичен (push только после commit);
  - первое сообщение от нового собеседника видно в списке диалогов, который biz dialog отдаёт через кэш Redis;
  - обработчик inbox при 1205 повторяет событие и выполняет его после снятия блокировки; событие с `user_id = from_id` отвергается;
  - `Destroy` во время выполняющегося обработчика дожидается его commit и публикации.
- biz dialog на MySQL: `reorderPinnedDialogs` параллельно с многодиалоговым `deleteMessages` того же пользователя (100 итераций) — ни одной операции, упавшей по 1213; `setHistoryTTL` меняет обе строки.
- `app/service/biz/updates`: `getState` и `getDifference` на фикстурах — верхняя граница, `slice`, `difference` после скачка, `empty`.

**e2e** (`make e2e`):
- существующие сценарии без изменений и зелёные;
- новый: Bob не ждёт push-ей, Alice отправляет 20 сообщений, `getDifference` Bob-а от начального состояния возвращает все 20 с возрастающими id, а `state.pts` равен начальному + 20;
- новый: `FLUSHALL` в Redis стека между двумя пачками. `pts` в ответах Alice строго растут, `getDifference` Bob-а возвращает обе пачки. Push после `FLUSHALL` не проверяется: онлайн-сессии живут в Redis;
- новый: `messages.deleteHistory` диалога из 1200 сообщений через `pkg/mtclient` повторяется по `offset` до 0, история пуста у обоих.

**Процедуры обслуживания** — один прогон на стенде e2e: стек базового коммита с трафиком → обновление по п. 10 → трафик → откат по п. 11 → трафик → повторное обновление с `state-raise`. После каждого шага `getDifference` всех пользователей без потерь, `verify-pts` без нарушений. Результат записывается в бриф.

**Нагрузочный замер** — п. 12.

## Последствия

- Потеря Redis больше не трогает `pts` и id: их там нет. Состояние ящика, журнал и данные меняются атомарно, проверки идут по заблокированному состоянию. Клиент видит только закоммиченные `pts`, без дыр (кроме однократного скачка миграции) и без сожжённых значений. Повторение №3 и №5 исключено конструкцией и проверками в базе, а не дисциплиной.
- Путь записи один (форма V2) и проще: у отправителя нет события `Out`, нет записи `dialogs` через RPC, нет флага `-usev3`. Каждое inbox-событие адресовано одному получателю, порядок по получателю обеспечивает Kafka. Ошибки проверяются компилятором (в клиенте idgen нет методов выделения `pts`) и статической проверкой замыканий `Run`.
- **Цена:**
  - каждая операция с ящиком держит блокировку строки пользователя до commit, и ответ отправки ждёт commit MySQL и синхронной публикации;
  - msg пишет `dialogs` напрямую и сбрасывает кэш; гонка «читатель записал в кэш старое значение после сброса» сужена двойным удалением, но остаётся, как во всём проекте;
  - копия marmota в `third_party` с одним патчем; перегенерация кода idgen и inbox; четыре biz-обработчика переводятся на `Run`;
  - обновление требует окна обслуживания и вызывает один `getDifference` на устройство; у отката есть своя процедура и команда `pluma-admin`;
  - 002 не выкатывается пользователям без 003.
- **Пересмотреть, если:**
  - p99 `userbox_tx_seconds` превышает 50 мс или p99 `userbox_lock_wait_seconds` — 10 мс (признак горячих пользователей — ботов, больших групп);
  - нужны каналы с общим ящиком на канал (своя строка состояния канала тем же примитивом);
  - пользователей больше примерно 1000.

## Рассмотренные альтернативы

- **Оставить Redis-счётчики и прогревать их из MySQL** (`SelectLastPts` при старте или промахе). Не решает №3: выделение по-прежнему вне транзакции, порядок commit ≠ порядок `pts`, сожжённые `pts`. Окно потерь AOF (everysec) остаётся, источников истины два.
- **`pts = MAX(pts) + 1` из журнала под `SELECT … FOR UPDATE`, без таблицы состояния.** Блокировки диапазона и gap-блокировки, взаимоблокировки при первой записи. Негде хранить id в ящике, `seq` и `qts`, а базу и скачок миграции не выразить.
- **Счётчик в MySQL отдельной autocommit-транзакцией** (`UPDATE … SET pts = LAST_INSERT_ID(pts + n)`). Долговечно, но выделение снова отделено от записи данных. Сожжённые `pts` и неверный порядок commit остаются.
- **Писать все ящики (отправителя и получателей) одной транзакцией без Kafka на критическом пути.** Нужно несколько блокировок, задержка растёт с размером группы, меняется архитектура доставки, которая относится к задаче 003. Для личных чатов можно вернуться к этому варианту в 003, правило блокировок из п. 3 его допускает.
- **Оставить V3** (асинхронный ответ RPC, `pts` в консьюмере). Ответ уходит раньше записи журнала (№19). Сложнее, а выигрыша при 100 пользователях нет.
- **Минимальный transactional outbox уже в 002** (`Emit` пишет строку `mq_outbox` в той же транзакции, публикует relay). Отклонено. Relay публикует «хотя бы раз», а без идемпотентного консьюмера, коммита offset после успеха и DLQ повторная публикация создаёт у получателя дубликат, а ошибка обработчика по-прежнему теряет событие (№6). Outbox, relay и надёжный консьюмер — одна единица гарантии, это задача 003, и её бриф уже опирается на транзакцию 002. Вместо этого принят запрет выкатывать 002 без 003 (п. 9): пользователей нет, запрет ничего не стоит. `Emit` устроен так, что 003 меняет только его реализацию.
- **Уровень изоляции только через DSN, с проверкой `@@transaction_isolation` в `SELECT … FOR UPDATE`.** Отклонено в пользу `BeginTx` с `sql.LevelReadCommitted`: изоляция становится свойством примитива и не зависит от конфигурации каждого сервиса. Своя транзакция заодно привязывает контекст к `BEGIN` и уводит `Run` из-под автомата отключения go-zero. Цена — одна копия marmota с экспортируемым конструктором и лишний `SET TRANSACTION` на транзакцию (доли миллисекунды, попадает в замер).
- **Изолировать `fn` от пула типами** (передавать в `fn` только `Box`, без `Dao`). Не работает: замыкание захватывает `Dao` из окружения. Принята статическая проверка `tools/boxaudit` и тест на пуле из одного соединения, которые ловят и прямые обращения, и обращения из вспомогательных функций.
- **Маркер операции для неопределённого `COMMIT`** (колонка `user_state.last_op_id` или поиск сообщения по `dialog_message_id`). Отклонено. `last_op_id` перезаписывает следующая операция, и случай «наша применилась, следом прошла чужая» неотличим от «наша не применилась, её `pts` заняла чужая». Поиск по `dialog_message_id` годится только для отправки. Сравнение строк журнала на тех же `pts` под блокировкой `user_state` работает для любой операции с журналом, не требует новых колонок, а блокировка заставляет проверку дождаться исхода ещё не завершённого `COMMIT`.
- **Импорт точных значений Redis-счётчиков при миграции** вместо запаса. Отклонено. Запас 1 000 000 перекрывает любой реальный хвост, а скачок любого размера стоит одного `getDifference`. Импорт добавил бы в обновление зависимость от состояния Redis, которое к этому моменту может быть потеряно или восстановлено частично. Запас 1000 из первой редакции отклонён: хвост сожжённых `pts` (`readMessageContents` без журнала) за месяцы работы мог его превысить, и клиент оказался бы впереди сервера.
- **`ON DUPLICATE KEY UPDATE pts = GREATEST(…)` в миграции.** Отклонено: повторный прогон после работы новой версии сдвигал бы всех ещё на величину запаса. Подъём существующих строк — явная команда `pluma-admin state-raise`, а не миграция.
- **Пул biz на READ COMMITTED и повтор 1213 в biz-обработчиках** вместо перевода многострочных писателей `dialogs` на `Run`. Отклонено: цикл между диапазонным `UPDATE` biz и многострочной `Run` msg остаётся возможным, его только повторяют. Перевод на `Run` убирает цикл (обе стороны сначала берут `user_state`), даёт повторы и запрет проглатывать ошибки без нового кода, а в задаче 010 этим обработчикам всё равно понадобится `Run` для seq.
- **Сократить бюджет `Run` ниже TTL блокировки идемпотентности (5 с).** Отклонено: бюджет должен вмещать три попытки с ожиданием блокировки, `COMMIT` и проверку его исхода. Поднят TTL до 30 с. Цена — повторный запрос с тем же `random_id`, пришедший во время выполнения первого, ждёт его результат дольше.
- **Отказывать в отправке при недоступном Redis** (как `DoIdempotent`). Отклонено: Redis по ADR 0005 — кэш, и его отказ не должен останавливать переписку. Риск дубликата при повторе клиента в это окно принят до задачи 003, где дедупликация `random_id` станет постоянной.
- **Отвергать в обработчике события с `Out = true`.** Заменено удалением поля `out` из схемы inbox (схема всё равно перегенерируется) и отказом от событий с `user_id = from_id`.
- **Одно совмещённое inbox-событие «закреп + служебное сообщение».** Отклонено: два существующих события с одним ключом в фиксированном порядке дают тот же результат без нового метода. До 003 сбой второго оставляет у получателя закреп без служебного сообщения; после 003 событие доставляется гарантированно.
- **Двойное удаление и для списочных ключей кэша `dialogs`.** Отклонено: заранее неизвестно, будет ли строка создана, а сброс списков до каждой операции лишил бы их кэширования для активных пользователей. Списочные ключи сбрасываются после commit, когда строка действительно создана.

## Вне рамок 002

- После `FLUSHALL` push-и не идут до переподключения: онлайн-сессии хранятся в Redis (003/004).
- `seq`, `qts`, `differenceTooLong`, клиент впереди сервера — задача 010.
- Постоянная дедупликация `random_id` и `(user_id, dialog_message_id)`, outbox, коммит offset после успеха, DLQ — задача 003.
- `msg.deleteChatHistory` не реализован (`ErrMethodNotImpl`) и на пути записи не участвует.
