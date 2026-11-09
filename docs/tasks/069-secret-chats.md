# 069. Секретные чаты

- Статус: done (сервер; полный прогон партии и проверка в клиентах — позже)
- Зависит от: 002 (ящик), ADR 0019/0020 (getDifference, seq), 009 (пуш), 025 (жалобы)
- ADR: новых решений уровня ADR нет

## Цель
ROADMAP, этап 4: секретные чаты слоя 228. Методы `messages.getDhConfig`, `requestEncryption`, `acceptEncryption`, `discardEncryption`, `setEncryptedTyping`, `readEncryptedHistory`, `sendEncrypted`, `sendEncryptedFile`, `sendEncryptedService`, `receivedQueue`, `reportEncryptedSpam`, `uploadEncryptedFile`; updates `updateEncryption`, `updateNewEncryptedMessage`, `updateEncryptedChatTyping`, `updateEncryptedMessagesRead`; qts в `updates.getState` и `getDifference`, очередь на устройство, файлы, push, логаут и блокировки. Сервер не видит открытый текст и не пишет полезную нагрузку в логи.

## Что выяснено у клиентов и в документации
Прочитано core.telegram.org/api/end-to-end, /api/updates, /api/push-updates, страницы методов и `encryptedChat`; клиенты — `~/pluma-work/reference-src`.
- **Привязка к устройству.** «Secret chats are associated with specific devices (or rather with authorization keys), not users» (end-to-end). Запрос приходит на все устройства участника; при принятии остальным устройствам — `encryptedChatDiscarded`. Android принимает автоматически при получении `encryptedChatRequested` (`SecretChatHelper.processUpdateEncryption`, :190-219).
- **qts.** На событие +1 (end-to-end, /api/updates). Подтверждение — `messages.receivedQueue` и неявно qts в `updates.getDifference`; неподтверждённое и старше 7 дней можно удалять; при выходе «the event queue of the corresponding device will be forcibly cleared» (end-to-end). Android применяет обновление, если локальный qts равен 0 или совпадает (`MessagesController.java:18113`), и после этого шлёт `receivedQueue` (:18297); iOS — `AccountStateManager.swift:471`. Обе платформы берут `state.qts` из difference (`MessagesController.java:17022`, iOS `AccountStateManagementUtils.swift:727`).
- **Обновления без qts** (`updateEncryption`, `updateEncryptedMessagesRead`) идут отдельно; повтор безопасен (Android `SecretChatHelper.processUpdateEncryption`:190-249: `encryptedChat` без ожидания ничего не делает; `Requested` для существующего чата попадает в общую ветку и перезаписывает его тем же состоянием, а сервер шлёт `Requested`, только пока чат не принят; iOS `UpdateSecretChat.swift` такие повторы только пишет в журнал).
- **qts = -1.** Клиенты без секретных чатов не ведут qts: tweb всегда шлёт `qts: -1` (`apiUpdatesManager.ts:320`), tdesktop держит -1, пока в состоянии нет ненулевого (`api_updates.h:175`, `api_updates.cpp:364`). Для них пустая разница остаётся `differenceEmpty`.
- **Очередь при завершении чата.** end-to-end разрешает удалять только подтверждённое, старше 7 дней и при деавторизации. Android расшифровывает `new_encrypted_messages` до применения `other_updates` (`MessagesController.java:16931`, `:17014`), поэтому последние сообщения завершённого чата доходят до устройства, которое было офлайн.
- **`g_a_or_b`.** «B = g ^ b mod p, if the currently authorized user is the chat's creator, or A = g ^ a mod p otherwise» (constructor/encryptedChat).
- **Ошибки** — страницы методов: `DH_G_A_INVALID`, `RANDOM_ID_DUPLICATE`, `INPUT_USER_DEACTIVATED`, `USER_ID_INVALID`, `USER_IS_BLOCKED` (403), `ENCRYPTION_ALREADY_ACCEPTED/DECLINED`, `ENCRYPTION_ID_INVALID`, `ENCRYPTION_DECLINED`, `DATA_TOO_LONG`, `DATA_INVALID`, `FILE_EMTPY` (в документации так), `MAX_QTS_INVALID`, `MAX_DATE_INVALID`, `RANDOM_LENGTH_INVALID`.
- **Файлы.** Загрузка частями и `inputEncryptedFileUploaded`, повтор — `inputEncryptedFile` (end-to-end). Android берёт из ответа `TL_messages_sentEncryptedFile.file` (`SecretChatHelper.java:752`).
- **Push.** `ENCRYPTED_MESSAGE`, `ENCRYPTION_REQUEST`, `ENCRYPTION_ACCEPT` с `custom.encryption_id` и `random_id` (push-updates); Android — `PushListenerController.java:343-351,1374,1398`; iOS — `NotificationService.swift:1022`.
- **Другие клиенты.** tdesktop не поддерживает секретные чаты: `updateNewEncryptedMessage`, `updateEncryptedChatTyping`, `updateEncryption`, `updateEncryptedMessagesRead` — пустые ветки (`api_updates.cpp:2165-2176`). В tweb обработчиков нет (grep по исходникам `src/lib`). Запрос до таких клиентов доходит и ими игнорируется.
- **Простой DH.** Группа — `goodPrime` Android (`jni/tgnet/Handshake.cpp:274`), та же, что в handshake сервера, g = 3 (p mod 3 = 2).

## Решения
- **Хранилище — `pkg/secretchat`** (четыре таблицы, миграция `migrate-20261109-secret-chats.sql`, плюс уникальный ключ на `encrypted_files.encrypted_file_id`): `encrypted_chats`, `encrypted_chat_devices` (qts и курсор устройства), `encrypted_chat_queue`, `encrypted_chat_sent` (ответ на `random_id`, 7 дней).
- **qts — на устройство** (по perm auth key), не из `user_state`: очередь и нумерация у каждого устройства свои (документация так требует).
- **Идемпотентность.** `requestEncryption` — по `(устройство, random_id)`; `send*` — по `(чат, устройство, random_id)`, повтор получает прежние date и файл без второй доставки и без повторной загрузки. Повтор `acceptEncryption` тем же устройством возвращает чат.
- **Транзакция отправки.** Блокировка строки чата → строка `encrypted_chat_sent` → qts получателя (строка устройства, FOR UPDATE) → строка очереди; без пропусков qts, порядок коммитов совпадает с qts.
- **`getDifference`.** Очередь отдаётся страницами по qts (100 сообщений, 4 МиБ); незаконченная очередь делает ответ `differenceSlice`. Изменения чатов считаются из состояния строк, а не из журнала: повторная выдача безопасна, потеря пуша не теряет событие; окно — от меньшего из `date` клиента и времени последней выдачи состояния, минус 60 с. Запрос с `date` INT32_MAX (расширение уведомлений iOS) получает только очередь.
- **Доставка на одно устройство.** Без нового RPC: `sync.updatesMe` без `server_id` теперь идёт только сессиям этого устройства (`skipSession`). Раньше такой push (`account.resetAuthorization`, `account.deleteAccount`) шёл всем сессиям пользователя, и `updateAccountResetAuthorization` session применял к каждому устройству (`main_auth_wrapper.go:1117`) — завершение одной сессии разрывало сессии остальных устройств; теперь только адресату. Это единственное изменение общего поведения, о нём в отчёте.
- **Завершение чата.** Очередь удаляется только при `delete_history`; иначе — по подтверждению и ретенции, у ушедшего устройства — дворником.
- **Логаут и прочее.** Единый дворник `Store.Sweep` (раз в минуту, из bff): чаты устройств, которых нет в живых `auth_users`, завершаются (`encryptedChatDiscarded`), очереди и строки устройств удаляются; ретенция 7 дней; не отвеченные запросы старше 7 дней завершаются. Безопасно при нескольких экземплярах: завершение чата происходит одним из них.
- **Файлы.** `dfs.uploadEncryptedFileV2` как есть; запись в `encrypted_files` с собственным `access_hash` (dfs его не проверяет) и fingerprint, проверка хэша в `upload.getFile`. `inputEncryptedFile` — только по известному файлу и хэшу.
- **Блокировки.** `USER_IS_BLOCKED` (403) на запрос и на отправку; повтор уже отправленного сообщения получает ответ и при блокировке.
- **Жалоба.** `reportEncryptedSpam` — обычная жалоба на собеседника (`moderation.KindSpam`) без содержимого: читать нечего.

## Результат
Секретные чаты слоя 228 работают на сервере: DH (`messages.getDhConfig`, отказ на версии), запрос, принятие и отказ с привязкой к устройству, отправка `sendEncrypted`/`sendEncryptedService`/`sendEncryptedFile` с идемпотентностью по `random_id`, очередь и qts на каждое устройство, `receivedQueue`, чтение и печать, `discardEncryption` с `history_deleted`, файлы через dfs (`uploadEncryptedFile`, `inputEncryptedFile`, скачивание с проверкой `access_hash`), push `ENCRYPTED_MESSAGE`/`ENCRYPTION_REQUEST`/`ENCRYPTION_ACCEPT` только на нужное устройство, жалоба. Плюс updates: `updates.getState` с qts устройства, `updates.getDifference` с `new_encrypted_messages` и обновлениями чатов без qts. Чаты завершаются при выходе устройства или удалении аккаунта и при истечении 7 дней без ответа.

- **Новый пакет:** `app/bff/secretchats` (маршрут `/mtproto.RPCSecretChats` раскомментирован в трёх session.yaml; `messages.reportEncryptedSpam` — в reports, `messages.uploadEncryptedFile` — в files), `pkg/secretchat`.
- **Изменён общий код:** `sync` (push одному устройству, см. решение), `push` (ключи секретных чатов), `updates` bff.
- **Миграция:** `migrate-20261109-secret-chats.sql`, действий оператора нет.
- **Тесты.** Модульные `pkg/secretchat` (`TestDHPrime`, `TestValidDH`, `TestChatView`, `TestShouldTell`, `TestDifferenceEmpty`), `notify.TestEncrypted`, `sync.TestSkipSession` (падает без фильтра — проверено); интеграционные `TestRequestAndAccept`, `TestSend`, `TestDifference`, `TestDifferenceAhead`, `TestAckAndRead`, `TestDiscard`, `TestDiscardKeepsQueue`, `TestDiscardRequest`, `TestSweep`, `TestFiles` (`pkg/secretchat`, на MySQL); e2e `TestSecretChat`, `TestSecretChatBlocked`.
- **Проверка на ветке:** `go build ./...`, `go vet -tags integration,e2e -unreachable=false` и `go test` затронутых пакетов — зелёные; интеграционные тесты `pkg/secretchat` на MySQL выполнены и зелёные. e2e-тесты только компилируются; они будут выполнены в общем прогоне партии, как и `-race`, `make lint`, `go test ./tools/...`.

## Решения на подтверждение
- Предел одного сообщения — 256 КиБ (`DATA_TOO_LONG`); документация предела не называет.
- Предел `random_length` — 1024 (клиенты просят 256, документация молчит).
- Не отвеченные запросы и строки завершённых чатов удаляются через 7 дней (распространение правила очереди из end-to-end на запросы; по запросам документация молчит).
- `chat_id` генерирует сервер (случайный положительный int), а не берётся из `random_id`, как в сноске документации: `random_id` у разных пользователей может совпасть.
- При смене состояния `acceptEncryption` с другими значениями `g_b` — `ENCRYPTION_ALREADY_ACCEPTED`; при отсутствии проверки `g_b` как `DH_G_A_INVALID` (документация для `g_b` ошибки не перечисляет).

## Открытые вопросы (не сделано)
- Не проверено с живыми клиентами; проверка — за владельцем и за общим прогоном партии.
- `silent` у `sendEncrypted*` не доходит до push: апдейт его не несёт (пуш всегда со звуком).
- `folder_id` (архивация при автоматическом архивировании не из контактов) в `encryptedChatRequested` не заполняется.
- Содержимое зашифрованных файлов в объектном хранилище не удаляется вместе с чатом (как у документов; строка `encrypted_files` остаётся). Отдельная задача по сборке мусора.
- `MD5_CHECKSUM_INVALID` у `inputEncryptedFileUploaded` не проверяется: Android для шифрованных файлов шлёт пустой `md5_checksum`, а проверять чужой шифртекст незачем.
