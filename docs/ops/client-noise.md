# Ошибки и шум клиентов (задача 098): выкат

Задача 098 закрывает ошибки, которые клиенты получали на запуске и в обычной работе: уведомления канала, настройки встроенного браузера, меню вложений и кубик, метрики прочтения и прослушивания, жалоба на реакцию, аннулирование кодов входа, последние геопозиции. Новых переменных окружения нет.

## Миграции

Все три применяет сервис `migrate` при запуске, до `biz` и `bff`; повторный запуск ничего не меняет.

- `migrate-20261119-web-browser-settings.sql` — таблицы `user_web_browser_settings` (флаги браузера аккаунта) и `user_web_browser_exceptions` (исключения по доменам, до 100 в каждом из двух списков). Только `CREATE TABLE IF NOT EXISTS`. Удаление аккаунта удаляет его строки обеих таблиц. Нет строки — значения по умолчанию: встроенный браузер с кнопкой закрытия (`pkg/webbrowser.Defaults`).
- `migrate-20261119-read-metrics.sql` — таблицы `message_read_metrics` (`messages.reportReadMetrics`: по строке на показ сообщения) и `music_listens` (`messages.reportMusicListen`: по строке на слушателя, трек и минуту). Только `CREATE TABLE IF NOT EXISTS`. Пока таблицы только пишутся: статистика задачи 103 метрики чтения не показывает (ADR 0066, «Что не делается»), сводка для неё готова (`pkg/readmetrics.Aggregate`), но не вызывается. Удаление аккаунта удаляет строки пользователя, выход из канала — нет.
- `migrate-20261119-geo-live-filter.sql` — переводит уже сохранённые сообщения с живой геопозицией (`messageMediaGeoLive`) на `message_filter_type = 14` в `messages` и `channel_messages` и добавляет индексы `messages.user_dialog_filter` и `channel_messages.filter` для отбора по типу медиа (`messages.getRecentLocations`, списки медиа чата). `UPDATE` один раз читает `message_data` всех строк обеих таблиц в `READ COMMITTED`: блокируются только изменяемые строки (старые живые геопозиции), без блокировок промежутков, так что запись сообщений идёт и во время него; индексы строятся онлайн (`ALGORITHM = INPLACE, LOCK = NONE`), без блокировки записи. Старый `msg` до перезапуска сохраняет новые живые геопозиции с `-1`: файл прогоняется ещё раз после перезапуска (раздел «Порядок»). Время на домашнем сервере не измерялось: перед выкатом — свежая резервная копия, как обычно.

## Конфигурация

- `session.yaml` (`teamgramd/etc`, `teamgramd/etc2`, `app/interface/session/etc`): маршруты `"/mtproto.RPCWebBrowser"`, `"/mtproto.RPCBotMenu"`, `"/mtproto.RPCGames"` → `bff.bff`. Без них прокси отвечал `ERR_ENTERPRISE_IS_BLOCKED`, и iOS повторял `account.getWebBrowserSettings` каждые несколько секунд с каждого устройства.
- `bff.yaml` не меняется: служба браузера берёт `Mysql` и `SyncClient` из общих секций bff, аннулирование кодов — `LOGIN_CODE_HMAC_KEY` и Redis, которые у bff уже есть.

## Порядок

`migrate` → `bff` → `session`. `session` с новыми маршрутами при ещё старом `bff` ответит на методы `RPCBotMenu` и `RPCGames` gRPC `Unimplemented` (500), который клиенты повторяют (у прежнего `bff` этих служб нет), а на `RPCWebBrowser` — прежней заглушкой с отказом. При обычном обновлении (`up -d` всех сервисов, `deployment.md`, раздел «Обновление») порядок соблюдается сам: `migrate` отрабатывает до сервисов, `bff` и `session` поднимаются вместе.

После перезапуска `msg` — ещё раз `migrate-20261119-geo-live-filter.sql`: живые геопозиции, отправленные между миграцией и перезапуском, старый `msg` сохранил с `message_filter_type = -1`, и `messages.getRecentLocations` их не нашёл бы. Файл идемпотентен, запись в `schema_migrations` он не трогает:
```bash
docker exec -i mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot teamgram' < teamgramd/deploy/sql/migrate-20261119-geo-live-filter.sql
```

## Проверка после выката

- В логах `bff` нет строк `account.getWebBrowserSettings blocked, License key` и `messages.getRecentLocations blocked, License key` (их писали прежние заглушки; есть — значит, работает старый образ `bff`).
- Остальные исправленные методы прежде получали отказ молча — от прокси `session` (`messages.getAttachMenuBots`, `messages.getEmojiGameInfo`: служб не было) или от заглушки без записи в лог (`messages.reportReadMetrics`, `messages.reportMusicListen`). Их видно только по числу отказов: `docker compose logs --since 10m session | grep -c ERR_ENTERPRISE_IS_BLOCKED` после выката заметно меньше, чем до него при том же числе клиентов (оставшиеся — методы, которые Pluma не обслуживает намеренно).
- `messages.getEmojiURL` и `messages.summarizeText` по-прежнему отвечают отказом (400): это решение, а не ошибка (реестр функций; у Pluma нет платформы переводов и модели пересказа).
- Коды входа, отправленные в чат или снятые на экран, аннулируются: см. `security-setup.md`, раздел 7.

## Диск и память

Настройки браузера — не больше 201 строки на пользователя (флаги и два списка по 100). Метрики прочтения растут с чтением: строка на каждый показ сообщения, который клиент отчитал (объём на домашнем сервере ещё не измерялся; удаляются только вместе с аккаунтом). Памяти сервисам не прибавляется: кэш доступа к чату для метрик живёт минуту.

## Известное

- iOS повторяет любой отказ `account.toggleWebBrowserSettingsException` без предела, раз в ≤5 с (`TelegramCore/Sources/Settings/AccountWebBrowserSettings.swift:173-174`, `retryRequestIfNotFrozen` в `Network/Network.swift:1513-1525`). Сам iOS число исключений не проверяет, поэтому 101-е исключение одного списка (`LIMIT_INVALID`) даёт бесконечные повторы с этого устройства, пока приложение не перезапустят. Нужна отдельная задача.
- `inputMessageCallbackQuery` в `messages.getMessages` не обслуживается (ничего не добавляет): сервер не хранит, из какого сообщения пришёл запрос кнопки бота. Клиенты его не шлют, только боты.
- `account.getNotifySettings` не проверяет, что пользователь, группа или канал существуют (`TODO(@benqi)` в `account.getNotifySettings_handler.go:59-63`): на чужой или несуществующий id отвечает тем, что хранится для этого id у самого пользователя (без записи — значениями по умолчанию), без проверки доступа. Утечки нет: настройки свои, но ошибки `PEER_ID_INVALID`, как у Telegram, клиент не получает.
