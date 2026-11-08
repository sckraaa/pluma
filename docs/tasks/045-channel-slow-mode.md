# 045. Супергруппы: медленный режим

- Статус: in progress (сервер готов; ждёт проверки в клиентах)
- Зависит от: 042
- ADR: 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` §2.6, задача T10; `clients.md` §4 п. 6; `plan.md` §2 U3, U4, §3, задача «044» плана).

## Цель
Админ включает медленный режим, и участник видит отсчёт в поле ввода и не может отправить раньше времени во всех четырёх клиентах.

## Что известно
- `slowmode_seconds` хранится на канале, время последней отправки участника — в `channel_participants`; `channelFull` отдаёт `slowmode_seconds` и `slowmode_next_send_date` (api.tl:110) (`server.md` §2.6).
- Клиенты: iOS `SlowMode.swift` (`channels.toggleSlowMode`); Android разбирает `SLOWMODE_WAIT_X` как ошибку 420 (`jni/tgnet/ConnectionsManager.cpp:1399-1420`); Web K её сам не повторяет (`apiManager.ts:744`) (`clients.md` §4 п. 6).
- `SLOWMODE_WAIT_%d`, код 420 (core.telegram.org/method/messages.sendMessage, 035). tdesktop при X < 3 сам повторяет запрос (`mtproto/mtp_instance.cpp:1388`, `:1495-1496`), iOS сверяет по префиксу (`PendingMessageManager.swift:98`), Web K его не повторяет (`apiManager.ts:744`).
- Создатель и любой админ медленным режимом не ограничены во всех четырёх клиентах (iOS `TelegramChannel.swift:302-312`, tdesktop `data/data_peer.cpp:1831-1837`, Android `ChatObject.java:2119-2120`, Web K `appMessagesManager.ts:9222-9233`); сервер их тоже не ограничивает (ADR 0034, п. 9; сервер Telegram не проверен, 035).
- Зависит от 042: проверка встраивается в `channelperm`/путь отправки, исключения для админов — по правам.

## Область
- `channels.toggleSlowMode` (служебное `updateChannel`, проверка прав).
- Проверка в пути отправки (`sendMessage`, `sendMedia`, `sendMultiMedia`, пересылка, когда она появится), запись времени отправки в той же транзакции.
- `slowmode_seconds` и `slowmode_next_send_date` в `channelFull` для зрителя.
- Не трогаем: прочие права (042).

## Критерии приёмки
- Модульные тесты: арифметика времени (граница, смена интервала, выключение), исключения по правам.
- Интеграционный тест на MySQL через `pkg/testinfra`: две параллельные отправки одного участника в окне — проходит одна.
- e2e через `pkg/mtclient`: вторая отправка раньше N секунд отклоняется `SLOWMODE_WAIT_%d` (420); админ и создатель не ограничены; тест падает на текущем коде.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: отсчёт в поле ввода и отказ при ранней отправке в iOS, Android, Desktop (CachyOS) и Web K.

## Результат
На сервере работают медленный режим супергрупп и подписи постов со ссылкой на профиль автора (расширение 044). Проверка в четырёх клиентах со скриншотами не выполнялась и остаётся за задачей.

### Медленный режим
- `channels.toggleSlowMode` → biz `channel.toggleSlowMode`: транзакция меняющего под блокировкой канала; ответ — `updateChannel` и канал с `slowmode_enabled`, участникам пуш. tdesktop по `updateChannel` у не-админа перечитывает полный канал (`api_updates.cpp:2461-2465`), iOS — `addExternallyUpdatedPeerId` (`AccountStateManagementUtils.swift:1282-1283`), Android после смены сам зовёт `loadFullChat` (`MessagesController.java:8784-8788`).
- Значения: 0, 5, 10, 30, 60, 300, 900, 3600 (tdesktop `edit_peer_permissions_box.cpp:216-229`, Android `ChatUsersActivity.java:2113-2130`, iOS `ChatSlowmodeItem.swift:64`). Остальное — `SECONDS_INVALID`. Ошибки — по core.telegram.org/method/channels.toggleSlowMode: `CHANNEL_INVALID` (трансляция), `CHAT_ADMIN_REQUIRED`, `CHAT_NOT_MODIFIED`.
- Право — `ban_users` или создатель: только им клиенты показывают настройку (tdesktop `data_channel.cpp:798-802`, Android `ChatUsersActivity.java:452`).
- Проверка в `SendChannel` под блокировкой канала: через этот путь идут `sendMessage`, `sendMedia`, `sendMultiMedia` и пересылка. Время отправки пишется в той же транзакции (`channel_participants.last_send_date`, колонка уже была). Транзакция READ COMMITTED, поэтому второй отправитель после блокировки видит запись первого. Создатель и админы не ограничены (ADR 0034, п. 9).
- Одна отправка — одно сообщение или один альбом, в том числе пересланный (`grouped_id` общий; tdesktop `history_item_helpers.cpp:158-173`). Иначе `SLOWMODE_MULTI_MSGS_DISABLED` (core.telegram.org/method/messages.forwardMessages).
- `SLOWMODE_WAIT_X` с кодом 420 — через `mtproto.NewErrSlowModeWaitX`; X от 1 до интервала. Повтор с теми же `random_id` получает первый ответ (`ChannelSentBefore`) и ошибки не вызывает.
- `channelFull`: `slowmode_seconds` всем, `slowmode_next_send_date` — ограниченному зрителю, пока окно не кончилось (tdesktop `data_channel.cpp:1415-1417`, Android `AlertsCreator.java:794`, Web K `input.ts:3901`).
- Комментатор группы обсуждения без вступления ограничен как участник. У него нет строки участника, поэтому она создаётся с `state = 1` (читается как «нет строки»); вступление дату сохраняет.

### Подписи со ссылкой на профиль
Что делают клиенты с `channel.signature_profiles` и постом с `from_id` и `post_author`:
- core.telegram.org: `constructor/channel` — посты админов ссылаются на профиль, поле входит в список полей min-конструктора; `method/channels.toggleSignatures` — `profiles_enabled` только вместе с `signatures_enabled`; `constructor/message` — `post_author` у постов с подписями.
- tdesktop: `HistoryItem::isPostHidingAuthor` смотрит только на флаг канала (`history_item.cpp:4044-4051`); подпись снизу не рисуется, если у поста есть `from_id` и флаг включён (`history_view_bottom_info.cpp:677-685`); профили меняют раскладку всей истории (`data_channel.cpp:269-271`).
- Android: при флаге и `from_id` (`flags & 256`) подпись скрыта (`ChatMessageCell.java:18373-18378`), отправитель берётся из `from_id` (`:18673-18674`), рисуется аватар (`MessageObject.java:9542-9549`).
- iOS: флаг читается в `ApiGroupOrChannel.swift:136`, включается флагом `1 << 1` запроса (`ToggleChannelSignatures.swift:15-17`). Отрисовку поста в iOS не читал: не проверено.
- Web K: канал с флагом считается похожим на группу (`appPeersManager.ts:137-148`), `post_author` тогда не показывается (`bubbles.ts:9082`).

Сделано: колонка `channels.signature_profiles` (`migrate-20261108-channel-signature-profiles.sql`), поле в `immutableChannel`, флаг в `channel` и в min-форме; biz `channel.toggleSignatures` меняет оба флага одной записью (из `toggleSetting` подписи убраны). `profiles_enabled` без подписей игнорируется: отдельной ошибки в документации нет, и выключение подписей снимает ссылки. Посты: `post_author` при подписях, `from_id` автора — при подписях со ссылкой.

Доставка автора: история, `channels.getMessages`, `getChannelDifference`, ответ на отправку и пуш берут пользователей через `PickByMessage`, то есть из `from_id`. **Найден и исправлен дефект:** `sync.pushChannelUpdates` запрашивал только авторов, а `GetUserListByIdList` строит вид от строки зрителя и без неё отдаёт пусто. Поэтому пуш участнику приходил с пустым `users` и в супергруппах тоже. Теперь запрашиваются и участники, как в `msg.sendMessageV2`. Min-объекты не используются: подписчик получает полного пользователя, как и в супергруппе (что отдаёт Telegram, не сверено).

### Проверка
- Модульные: `pkg/channelperm` (`TestSlowmodeWait`: граница, смена интервала, выключение, часы назад; `TestSlowmodeValid`, `TestSlowmodeApplies`), `channel_view_test.go`.
- Интеграционные на MySQL: `TestChannelSlowmodeParallel` (8 параллельных отправок — проходит одна, в базе одно сообщение), `TestChannelSlowmodeRules`, `TestChannelSlowmodeGuest`, `TestChannelSignatureProfiles`, `TestToggleSignatures`, `TestToggleSlowMode`.
- e2e: `TestChannelSlowMode`, `TestChannelSignatureProfiles`.
- Прогоны на CachyOS: `go build ./...`, `go vet -tags integration,e2e -unreachable=false` затронутых пакетов, `go test ./tools/...` — чисто; `go test -race ./app/... ./pkg/channelperm/` — чисто (часть пакетов перезапущена с `GOTMPDIR` на диске: первый прогон упёрся в заполненный tmpfs `/tmp` хоста); интеграционные channel и msg с `-race` — чисто; e2e `-run TestChannel` на чистом стенде — 19 из 19. `make lint` по всему репозитории завершается с ошибкой (479 замечаний), в затронутых файлах замечаний нет; сравнения с `master` по числу замечаний не делал.
- Падение без изменения показано: без вызова `checkSlowmode` падают все три `TestChannelSlowmode*` (8 из 8 отправок проходят); `TestChannelSignatureProfiles` падал на пустом `users` пуша до исправления `sync`.

### Решения на подтверждение
1. Время отправки пишется только под действующим режимом и только для ограниченных. Включение режима не наказывает за сообщения до него, а разжалованный админ получает одну свободную отправку. Как считает сервер Telegram, не сверено.
2. Значения интервала — ровно те, что предлагают клиенты; принимает ли Telegram другие, не сверено.
3. `profiles_enabled` без `signatures_enabled` не ошибка, а выключенные ссылки (документация запрещает сочетание, но ошибки не называет).
4. В посте со ссылкой есть и `post_author`, и `from_id`: клиенты подпись тогда не рисуют, а при выключении ссылок подпись остаётся. Что именно присылает Telegram, не сверено.

### Открытые вопросы
- `app/messenger/push/internal/dao/dao.go:144` запрашивает пользователей так же, как делал `sync` до исправления: пустой `users` в событии пуша канала. Не правил: вне области задачи, проверить отдельно.
- Проверка в клиентах со скриншотами (iOS, Android, Desktop, Web K) не выполнялась.
