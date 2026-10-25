# 009. Push: APNs (включая VoIP) и FCM

- Статус: done
- Зависит от: 003, 006
- ADR: 0003, 0012, 0017

## Цель
Сообщения приходят в фоне на iOS и Android; содержимое push зашифровано ключом устройства.

## Область
- `account.registerDevice` / `unregisterDevice` (сейчас заглушки), хранение токенов и секретов устройств.
- Сервис push: APNs (HTTP/2, token-based auth), FCM HTTP v1; зашифрованные push по схеме Telegram; VoIP push для звонков.
- Учёт настроек уведомлений (mute, исключения), отсутствие push при активной сессии.

## Данные владельца
Получены 2026-10-25. Секреты лежат на Mac владельца в `~/pluma-secrets` (каталог 700, файлы 600), в git и в чат не попадают.
- **APNs.**
  - Team ID — в `.env` (`APNS_TEAM_ID`).
  - Ключ «Pluma APNs», Key ID — в `.env` (`APNS_KEY_ID`), P-256, файл `~/pluma-secrets/apns/AuthKey_<Key ID>.p8`. Один ключ на sandbox и production, для обычных и VoIP push.
  - Bundle ID `chat.pluma.app` с Push Notifications.
- **FCM.**
  - Проект Firebase Pluma (ID и номер — в `google-services.json`).
  - Android-приложение `chat.pluma.app`, `~/pluma-secrets/firebase/google-services.json`. Файл не секретный, он вшивается в APK вместо файла Telegram.
  - Ключ сервера: отдельный сервисный аккаунт `pluma-push@<проект>.iam.gserviceaccount.com` с ролью Firebase Cloud Messaging API Admin (не Admin SDK), файл `~/pluma-secrets/firebase/<проект>-<id ключа>.json`.

## Решения (план реализации)
Архитектура — ADR 0017. Что ждут клиенты, сверено по коду DrKLO/Telegram 12.9.1 (`PushListenerController`), Telegram-iOS (`NotificationService`) и Web K. Шаги по порядку, каждый — отдельные коммиты.

**1. Устройства** (bff `notification`, хранение — `pkg/pushdevices`).
- **Таблица `push_devices`** (миграция): `user_id`, perm `auth_key_id`, `token_type`, `token`, `app_sandbox`, `no_muted`, `secret` (запечатан), `other_uids`, `lang_code` (из `initConnection`), `locked_period`, `invalidated`, даты.
  - Ключи уникальности: (`auth_key_id`, `token_type`) и (`user_id`, `token_type`, `token`).
  - Повторная регистрация — upsert. Android перерегистрируется каждые 3 часа, iOS — при смене токена.
- **`account.registerDevice`.**
  - Типы: 1 APNs, 2 FCM, 9 VoIP, 10 Web push; остальные — `TOKEN_TYPE_INVALID`.
  - Пустой или длиннее 4096 токен — `TOKEN_EMPTY` / `TOKEN_INVALID`.
  - Для 1, 2, 9 `secret` ровно 256 байт; 10 — 0 или 256.
  - Токен, который APNs объявил недействительным, на повторную регистрацию отвечает `TOKEN_WAS_INVALIDATED`: iOS берёт новый.
- **`unregisterDevice`** удаляет строку этого пользователя и токена.
- **`updateDeviceLocked`** хранит период для авторизации.
- **`messages.reportMessagesDelivery`** — `boolTrue`: Android шлёт его на каждый push.
- **Удаление устройств авторизации:** `auth.logOut`, `auth.resetAuthorizations`, `account.resetAuthorization`, `destroy_auth_key`. Удалённо завершённой — сначала `SESSION_REVOKE` (шаг 3).

**2. Сервис `push`** (`app/messenger/push`, своя группа на `Sync-T`, сервис compose).
- **События.**
  - `updateNewMessage` входящего — видимый push.
  - `updateReadHistoryInbox` — `READ_HISTORY` с `max_id` другим устройствам читателя.
  - `updateDeleteMessages` — `MESSAGE_DELETED`; диалог ищется по id сообщений.
  - Правки, реакции, звонки, каналы, отложенные — не в этой задаче.
- **Кому.** Устройства получателя, кроме авторизации-инициатора и авторизаций с живой записью в `status`. Перед отправкой проверяется, что ключ ещё принадлежит пользователю; иначе устройство удаляется.
- **Настройки получателя.**
  - Mute (по чату, затем по типу): видимого push нет. Устройству с `no_muted=false` — служебный `MESSAGE_MUTED`, не чаще раза в 15 с на диалог.
  - `show_previews=false` — без текста.
  - `silent` у настройки или у сообщения — `custom.silent=1` (Android), без звука (iOS).
- **Содержимое.**
  - Android и Web: `{loc_key, loc_args, custom, user_id}`, id числами.
    - Ключи: `MESSAGE_*`, `CHAT_MESSAGE_*` по типу медиа и альбомы.
    - Служебные `CHAT_CREATED`, `CHAT_ADD_YOU`, `CHAT_ADD_MEMBER`, `CHAT_DELETE_MEMBER`, `CHAT_DELETE_YOU`, `CHAT_LEFT`, `CHAT_RETURNED`, `CHAT_JOINED`, `CHAT_TITLE_EDITED`, `CHAT_PHOTO_EDITED`.
    - `PINNED_*` (в группе с `chat_from_id`).
    - В `custom` — `from_id` или `chat_id`, `msg_id` (обязателен, по нему Android гасит прочитанное), `mention`, `silent`.
  - iOS:
    - для сообщения — `aps.alert{title, body}` на языке авторизации (шаблоны ru, en), `sound`, `thread-id`, `category`, id строками на верхнем уровне. В группе только `chat_id`.
    - для служебного события — `loc-key` на верхнем уровне, тип APNs `background`.
    - Если истёк период `updateDeviceLocked`, текст скрыт.
- **Шифрование.** `p` = base64url(`key_id` ‖ `msg_key` ‖ AES-IGE(LE32 длина ‖ JSON ‖ паддинг 12–1024)), MTProto 2.0 с x = 8. Внешний текст APNs — общий («Новое сообщение»).

**3. Отправка** (в сервисе `push`).
- **APNs.**
  - HTTP/2, JWT ES256 из `.p8`, обновление раз в 50 минут.
  - `apns-topic` — bundle ID. Шлюз — production или sandbox по `app_sandbox`.
  - 410 и `BadDeviceToken` — устройство помечается недействительным.
- **FCM HTTP v1.**
  - Токен OAuth из ключа сервисного аккаунта.
  - Data-сообщение `{p}`, `android.priority=HIGH`.
  - `UNREGISTERED` и неверный токен — устройство удаляется.
- **Отказы.** Общий дедлайн события 10 с; 429 и 5xx — до трёх повторов внутри него.
- **Ключи.** Пути к файлам и ID — из `.env`, файлы монтируются только в `push`. Без ключа провайдер выключен.
- **Метрики и алерты.**
  - Метрики `push_sent_total{provider,result}`, `push_dropped_total{reason}`.
  - Алерт на отказ авторизации у провайдера (APNs 403, FCM 401/403) — критичный, на рост ошибок — предупреждение.
- **`pluma-admin push check`** проверяет ключи без доставки:
  - APNs с фиктивным токеном ждёт `BadDeviceToken`;
  - FCM с `validate_only` ждёт ошибку токена.
- Документ `docs/ops/push.md`.

**4. Клиент Android.**
- `google-services.json` проекта Firebase подставляется при сборке `pluma-android` из секрета GitHub Actions (в публичный репозиторий не кладём).
- Секрет заводится с разрешения владельца.

**Проверки.**
- **Unit.**
  - Шифрование против Go-копии расшифровки Android/iOS, включая `key_id` и порчу байта.
  - Содержимое по каждому `loc_key`, правила получателей и настроек.
  - JWT и запросы APNs/FCM, разбор ошибок (`httptest`, HTTP/2).
- **Интеграционные:** хранение устройств (upsert, удаление, запечатывание).
- **e2e:** поддельный провайдер в compose стенда записывает запросы. Сценарии:
  - устройство зарегистрировано, другой пользователь пишет — push расшифровывается `secret` в ожидаемый JSON;
  - онлайн — push нет;
  - выход — push нет;
  - mute;
  - `READ_HISTORY` другому устройству.
- **Настоящее:**
  - `push check` против APNs и FCM;
  - Android на эмуляторе с образом Google APIs и сборкой с нашим `google-services.json`: уведомление в фоне ≤3 с.
- **iOS** — после сборки iOS.

## Критерии приёмки
- iOS и Android в фоне получают уведомление ≤3 с после отправки; текст расшифровывается Notification Service Extension.

## Результат
Готово в ветке `task/009-push` (поверх 008). Решения — ADR 0017, эксплуатация — `docs/ops/push.md`.

- **Шаг 1, устройства.**
  - `pkg/pushdevices`: таблица `push_devices`, `secret` запечатан AES-256-GCM на `PUSH_SECRET_ENCRYPTION_KEY`; строка, которая не открывается, пропускается, но не удаляется.
  - В bff: `account.registerDevice`, `unregisterDevice`, `updateDeviceLocked`, `messages.reportMessagesDelivery` (отдельный gRPC-сервис, маршрут в `session.yaml`).
  - `auth.logOut` удаляет устройства авторизации.
- **Шаг 2, сервис `push`** (`app/messenger/push`, группа `Push-MainCommunity-S` на `Sync-T`).
  - `pkg/pushcrypt` шифрует как сервер Telegram. Расшифровка, написанная по Telegram-iOS, совпадает с ним.
  - `notify` разбирает апдейты:
    - сообщения по видам медиа, альбомы, пересылки;
    - события групп, закрепления (текст закреплённого — из ящика получателя);
    - прочтение.
  - Кодирование: `loc_key`/`loc_args`/`custom` для Android; готовый текст ru/en, id строками, `loc-key` у служебных — для iOS.
  - Правила:
    - не слать инициатору, онлайн-авторизациям и чужим ключам (их устройства удаляются);
    - mute: `MESSAGE_MUTED` не чаще раза в 15 с, упоминание проходит;
    - превью, беззвучность, блокировка iOS;
    - `SESSION_REVOKE` на `updateAccountResetAuthorization`.
- **Шаг 3, отправка** (`pkg/pushprovider`).
  - APNs: HTTP/2, ES256-токен на 50 минут. FCM: OAuth по ключу сервисного аккаунта, `android.priority=HIGH`.
  - Повторы внутри дедлайна 10 с; пять отказов подряд — провайдер пропускается 30 с.
  - Недействительный токен: FCM — устройство удаляется; APNs — помечается, и следующая регистрация получает `TOKEN_WAS_INVALIDATED`.
  - Метрики `push_sent_total`, `push_dropped_total` и алерты `rules/push.yml`.
  - `pluma-admin push check` проверяет ключи без доставки.
- **Шаг 4, клиент Android.** Конвейер `pluma-android` уже умел подставлять `google-services.json`. Переменная `PLUMA_GOOGLE_SERVICES_JSON` задана с разрешения владельца, dev-сборка собрана.
- **Проверки.**
  - Unit: шифрование, разбор и кодирование, правила ядра, отправители на поддельных серверах (APNs — HTTP/2 по TLS), обработчики bff, `pluma-admin`.
  - Интеграционный тест хранилища.
  - e2e `TestPush` через `test/e2e/fakepush`; полный e2e зелёный.
  - Ключи владельца: `push check` — Apple (production и sandbox) и Google принимают.
  - Android 12.9.1 на эмуляторе с Google APIs и настоящим FCM: уведомление «отправитель — текст» за ~1 с при свёрнутом приложении и при выгруженном процессе. FCM поднимает приложение, Android подтверждает доставку `reportMessagesDelivery`.
- **Попутно.**
  - `migrate` не стартовал: он читает весь `bff.yaml`, а нового ключа в нём не было. Тест compose теперь сверяет одноразовые сервисы с их конфигами.
  - `TestManualSend` пишет настоящему клиенту на стенде (`docs/ops/clients.md`).
- **Не сделано и не проверено.**
  - iOS: нет сборки; App Group и Notification Service Extension — со сборкой iOS (задача 006). Критерий для iOS открыт.
  - Web push и VoIP push не отправляются (нужны TLS-раздача Web K и звонки).
  - Push на правки, реакции и `MESSAGE_DELETED` не отправляются.
