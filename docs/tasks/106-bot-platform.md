# 106. Платформа ботов: вход на сайты, мини-приложения, меню вложений, игры, виджеты

- Статус: готово, ждёт выкладки (2026-11-16)
- Решение: ADR 0068 (платформа ботов; продолжает ADR 0043).
- Зависит от: 070 (боты как аккаунты, `pkg/botstore`, ожидание ответа бота, журнал seq бота), 098 (пакет шума: пустой `messages.getAttachMenuBots`, `messages.getEmojiGameInfo` в `RPCGames`), 099 (инлайн-боты: правит `messages.inline_handlers.go` — сливается раньше 106), 026 / ADR 0036 (сервис `links`, `pluma.chat`, `/_l/avatar/<имя>`), 091 (образец секретов в `.env`, текстов в `pkg/langpack` на 37 языках).
- Ветка: `task/106-bot-platform` (части — подветки `task/106-<буква>`, сливаются в неё по одной).

## Цель
Боты Pluma умеют то же, что боты Telegram вне платежей: кнопка «Войти» и вход на сайт через Pluma (с «Подключёнными сайтами» в настройках), мини-приложения из кнопок, меню, профиля бота, прямых ссылок и меню вложений с подписанными `initData`, отправка результата приложения в чат, подготовленные сообщения и запрос чата из приложения, игры со счётом, виджеты в сообщениях. Работает во всех четырёх официальных клиентах без патчей. `account.getWebAuthorizations` больше не отвечает выдуманным пустым списком.

## Область
В задаче:
- **Вход (`RPCSeamless`):** `messages.requestUrlAuth`, `acceptUrlAuth`, `declineUrlAuth`, `checkUrlAuthMatchCode`; `account.getWebAuthorizations` (убрать из `app/bff/bff/client/fake_rpc_result.go:40-44`), `resetWebAuthorization`, `resetWebAuthorizations`; кнопка `inputKeyboardButtonUrlAuth` (сейчас `BUTTON_TYPE_INVALID`, `pkg/botmarkup/botmarkup.go:106-111`); `url_auth_domains` в конфиге приложения (`teamgramd/bin/app_config.json:125`).
- **Провайдер входа на сайты** `oauth.pluma.chat` в сервисе `links`: OIDC (`/auth`, `/token`, `/.well-known/openid-configuration`, `/.well-known/jwks.json`), старый формат виджета (редирект с полями и `hash`), страница подтверждения с QR и кнопкой, опрос состояния.
- **Мини-приложения (`RPCMiniBotApps`, `RPCMainMiniBotApps`):** `messages.requestWebView`, `requestSimpleWebView` (`#413a3e73`), `requestMainWebView`, `requestAppWebView` (`#53618bce`), `prolongWebView`, `sendWebViewResultMessage`, `sendWebViewData`, `getBotApp`; `bots.canSendMessage`, `bots.allowSendMessage`, `bots.requestWebViewButton`, `bots.getRequestedWebViewButton`.
- **Меню вложений (`RPCBotMenu`):** `messages.getAttachMenuBots` (заменяет пустой ответ 098), `getAttachMenuBot`, `toggleBotInAttachMenu`.
- **Подготовленные сообщения (`RPCPreparedInlineMessages`):** `messages.savePreparedInlineMessage`, `getPreparedInlineMessage`; **запрошенные чаты:** `messages.sendBotRequestedPeer` (кнопка клавиатуры и `webapp_req_id`).
- **Игры (`RPCGames`):** `messages.setGameScore`, `setInlineGameScore`, `getGameHighScores`, `getInlineGameHighScores`; `inputMediaGame` в `messages.sendMedia`; `inputBotInlineResultGame`/`inputBotInlineMessageGame` в инлайне; `getBotCallbackAnswer` с `game`; `messages.editInlineBotMessage`.
- **Виджеты (новая служба `RPCBotWidgets`):** `messages.getBotWidgetQueryAnswer`, `setBotWidgetQueryAnswer`, `getWidgetPublicData`, `editWidgetData`, `editMessageWidgetData`, `editInlineBotMessageWidgetData`; `inputMediaWidget`, `inputMediaWidgetId`, `inputBotInlineMessageWidget(Id)`.
- **Настройка:** `pluma-admin bot` — домен, главное приложение, прямые приложения, меню вложений, игры, клиенты OIDC; ключи бота (выводятся из токена, хранятся зашифрованными); свой ключ Ed25519 и ключ подписи ID-токенов.

Не в задаче (ADR 0068, «Что не делается»):
- платежи и звёзды в приложениях, TON и кошелёк в виджетах (`widgetQueryTypeSignData`, `SentTransaction`, `with_wallet`), подарки;
- `messages.requestChatJoinWebView`, `bots.setJoinChatResults`, `bots.getAccessSettings`/`editAccessSettings` — боты-стражи вступления, отдельная задача по слову владельца;
- `bots.invokeWebViewCustomMethod`, `bots.checkDownloadFileParams`, `bots.updateUserEmojiStatus`, `bots.toggleUserEmojiStatusPermission`, `bots.*PreviewMedia*`, `bots.getPreviewInfo`, `bots.getPopularAppBots`, `messages.setBotGuestChatResult`, `bots.createBot`/`exportBotToken` — остаются `ErrEnterpriseIsBlocked`;
- ввод телефона на странице входа, пуш `OAUTH_REQUEST`, refresh-токены, `autologin_domains`;
- запрет боту писать без разрешения (открытый пункт 070); HTTP Bot API.

## Контракт клиентов
Источники — закреплённые выпуски в `~/pluma-work/reference-src-new`, telegram-web-app.js и core.telegram.org (прочитано 2026-11-15). Подробно — ADR 0068, «Контекст». Главное для реализации:

**Вход.**
- Кнопка: tdesktop шлёт `requestUrlAuth(flags: peer|msg_id|button_id)`; `Accepted` с `url` — открыть его, `Default` — открыть адрес кнопки, ошибка — тоже адрес кнопки; в окне отказа — `declineUrlAuth(url кнопки)` и адрес кнопки; `acceptUrlAuth` с `write_allowed`, без `url` (`boxes/url_auth_box.cpp:270-322, 356-430`). Ответ `urlAuthResultRequest` после `acceptUrlAuth` — ошибка клиента (`:422-424`).
- Ссылки `url_auth_domains`: `requestUrlAuth(flags: url)`, `URL_EXPIRED` — тост «ссылка устарела» (`url_auth_box.cpp:324-354`).
- OAuth-ссылка приходит в трёх формах: исходный URI целиком от Android (`ui/LinkManager.java:158, 1340-1351`, только при внешнем запуске), `tg://resolve?domain=oauth&startapp=<token>` от tdesktop (`core/local_url_handlers.cpp:707-717`), `tg://oauth?token=` / `tg://resolve?domain=oauth&startapp=` у iOS (`TelegramUI/Sources/OpenUrl.swift:45-67`). Хост — `pluma.chat` или `t.me`, схема — `https`, `tg` или `plm` (ADR 0036). Сервер принимает все формы.
- `urlAuthResultRequest` для OAuth: `bot`, `domain`, `browser`, `platform`, `ip`, `region` всегда; `match_codes` — эмодзи, одно из них показано на странице; `match_codes_first` → клиент сначала шлёт `checkUrlAuthMatchCode`, затем всё равно передаёт `match_code` в `acceptUrlAuth` (core.telegram.org/api/url-authorization, /method/messages.acceptUrlAuth; Android `ui/OAuthSheet.java:468-560`). tdesktop рисует код, только если строка — ровно одно эмодзи (`url_auth_box.cpp:99-104`). Принятый OAuth без `url` — клиент показывает «вход выполнен» (`url_auth_box.cpp:378-385`).
- `account.getWebAuthorizations` зовут все (Android `ui/SessionsActivity.java:724`, iOS `TelegramEngine/Privacy/RecentWebSessions.swift:24`, tdesktop `api/api_websites.cpp:51`, Web K `appSeamlessLoginManager.ts:56`); сброс — Android `SessionsActivity.java:553, 428`, iOS `RecentWebSessions.swift:54, 69`, tdesktop `api_websites.cpp:96, 101`.
- Данные кнопки для сайта — как у старого виджета: `id`, `first_name`, `last_name`, `username`, `photo_url`, `auth_date`, `hash = hex(HMAC_SHA256(data_check_string, SHA256(token)))` (core.telegram.org/widgets/login-legacy; bots/api `LoginUrl`: «The url's domain must be the same as the domain linked with the bot»).

**Мини-приложения.**
- Какой метод открывает что (core.telegram.org/api/bots/webapps): кнопка `keyboardButtonSimpleWebView` → `requestSimpleWebView(url)`; инлайн-кнопка `keyboardButtonWebView` → `requestWebView(url)`; кнопка меню → `requestWebView(from_bot_menu, url)`; меню вложений → `requestWebView` (+`start_param` из `startattach`); боковое меню → `requestSimpleWebView(from_side_menu)`; инлайн `switch_webview` → `requestSimpleWebView(from_switch_webview, url)`; главное приложение → `requestMainWebView`; прямая ссылка → `getBotApp(inputBotAppShortName, hash)` → `requestAppWebView`.
- Вызовы tdesktop со всеми флагами: `inline_bots/bot_attach_web_view.cpp:1249-1395`; Android — `ui/bots/BotWebViewAttachedSheet.java:1162` (`requestWebView`), `:1191`, `:1265`, `:1300`. Ошибки, которые клиенты разбирают: `BOT_INVALID` → tdesktop перечитывает меню вложений (`bot_attach_web_view.cpp:1282-1286, 1371-1375`); прочее — тост с кодом.
- `prolongWebView` раз в 60 с, любая ошибка закрывает окно (Android `BotWebViewAttachedSheet.java:164, 366-393`; core.telegram.org/api/bots/webapps: на `QUERY_ID_INVALID` закрыть). `updateWebViewResultSent(query_id)` закрывает окно с этим запросом (Android `:1620-1626`, `messenger/MessagesController.java:20716-20718`).
- `sendWebViewData(bot, random_id, button_text, data)` один раз, только для приложения из клавиатурной кнопки, `data` до 4096 байт (bots/webapps; Android `BotWebViewAttachedSheet.java:476-490`).
- Фрагмент: telegram-web-app.js читает `tgWebAppData`, `tgWebAppVersion`, `tgWebAppPlatform`, `tgWebAppThemeParams`, `tgWebAppDefaultColors`, `tgWebAppFullscreen`, `tgWebAppBotInline`, `tgWebAppShowSettings` (telegram.org/js/telegram-web-app.js, строки 298-345, 3229, 3698; версия по умолчанию `6.0`, старше 9.6 скрипт не проверяет). `start_param` также в `tgWebAppStartParam` (bots/webapps). Клиенты фрагмент не строят (tweb лишь меняет `tgWebAppVersion=8.0` на `9.0`, `components/webApp.tsx:252-254`).
- initData: поля и подписи — ADR 0068, «Документация»; `photo_url` — «.jpeg or .svg» (bots/webapps).
- `bots.canSendMessage` / `allowSendMessage` — tdesktop `bot_attach_web_view.cpp:1902-1922` (ответ `allowSendMessage` применяется как `Updates`), Android `ui/web/BotWebViewContainer.java`.
- `getRequestedWebViewButton(bot, webapp_req_id)` → `KeyboardButton`, затем `sendBotRequestedPeer(webapp_req_id)` (tdesktop `bot_attach_web_view.cpp:2160-2175`; core.telegram.org/api/bots/buttons). `getPreparedInlineMessage` — Android `ui/bots/BotShareSheet.java:78`.

**Меню вложений.**
- `getAttachMenuBots(hash)` при запуске iOS (`TelegramCore/Sources/State/AccountTaskManager.swift:110`) и Web K (`appAttachMenuBotsManager.ts:51, 121`); `updateAttachMenuBots` → Web K перечитывает (`:68-70`). `getAttachMenuBot` + `inactive` → подтверждение → `toggleBotInAttachMenu(write_allowed)` (core.telegram.org/api/bots/attach; Android `ui/LaunchActivity.java:4474-4560`).
- Значки: `default_static` — SVG (tdesktop `bot_attach_web_view.cpp:178, 916`), имена iOS (`AttachMenuBots.swift:27-43`) и Android (`messenger/MediaDataController.java:118-128`). `attach_menu_enabled` у `user` ни один клиент не читает (поиск по четырём клиентам) — отдаётся как сейчас.

**Игры.**
- `keyboardButtonGame` → `getBotCallbackAnswer(game)` (tdesktop `api/api_bot.cpp:76-82`, iOS `RequestMessageActionCallback.swift:42-43`); ответ с `url` открывается как игра. Счёт — `messageActionGameScore`: «вы набрали» по отправителю, название — из сообщения, на которое отвечает служебное (tdesktop `history/history_item.cpp:8660-8705`). `getGameHighScores` зовёт только Web K (`appGamesManager.ts:78`).
- Правила счёта и таблицы — bots/api `setGameScore`, `getGameHighScores`; кнопка игры «must always be the first button in the first row» (bots/api `InlineKeyboardButton`).

**Виджеты** (документации нет, только клиенты): ADR 0068, «Клиенты». `getWidgetPublicData(peer, msg_id, hash)` → `widgetPublicData(hash, data, period)` / `widgetPublicDataNotModified(period)`; `messageMediaWidget.payload` — zlib.

## Дизайн

### Таблицы (`teamgramd/deploy/sql/migrate-20261121-bot-platform.sql`; новые таблицы `CREATE TABLE IF NOT EXISTS`, колонки — guarded ALTER через `information_schema`, как `migrate-20261109-bots-core.sql`)
```sql
-- bots: домен входа и версия настроек платформы (для hash меню и приложений)
ALTER TABLE bots ADD COLUMN login_domain varchar(253) NOT NULL DEFAULT '';
ALTER TABLE bots ADD COLUMN platform_version int(11) NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS bot_secret_keys (           -- ADR 0068 п. 3
  bot_id bigint(20) NOT NULL PRIMARY KEY,
  token_hash char(64) NOT NULL,         -- = bots.token, когда ключи выведены
  webapp_key varbinary(96) NOT NULL,    -- seal(HMAC_SHA256("WebAppData", token))
  login_key varbinary(96) NOT NULL,     -- seal(SHA256(token))
  updated_at int(11) NOT NULL
);
CREATE TABLE IF NOT EXISTS bot_login_clients (         -- OIDC
  bot_id bigint(20) NOT NULL PRIMARY KEY,
  secret_hash char(64) NOT NULL,        -- SHA-256 client_secret, hex
  redirect_uris text NOT NULL,          -- JSON-массив точных адресов
  origins text NOT NULL,                -- JSON-массив origin для in_app_origin и JS
  created_at int(11) NOT NULL
);
CREATE TABLE IF NOT EXISTS web_authorizations (
  hash bigint(20) NOT NULL PRIMARY KEY,           -- случайный, отдаётся клиенту
  user_id bigint(20) NOT NULL,
  bot_id bigint(20) NOT NULL,
  domain varchar(253) NOT NULL,
  browser varchar(128) NOT NULL, platform varchar(64) NOT NULL,
  ip varchar(45) NOT NULL, region varchar(128) NOT NULL,
  write_allowed tinyint(1) NOT NULL DEFAULT 0,
  date_created int(11) NOT NULL, date_active int(11) NOT NULL,
  UNIQUE KEY user_bot_domain (user_id, bot_id, domain), KEY user (user_id, date_active)
);
CREATE TABLE IF NOT EXISTS bot_write_allowed (
  user_id bigint(20) NOT NULL, bot_id bigint(20) NOT NULL,
  source tinyint(4) NOT NULL,           -- 1 domain, 2 app, 3 attach_menu, 4 from_request
  allowed_at int(11) NOT NULL,
  PRIMARY KEY (user_id, bot_id)
);
CREATE TABLE IF NOT EXISTS bot_apps (
  id bigint(20) NOT NULL PRIMARY KEY, access_hash bigint(20) NOT NULL,
  bot_id bigint(20) NOT NULL, short_name varchar(64) NOT NULL,
  title varchar(64) NOT NULL, description varchar(512) NOT NULL,
  url varchar(1024) NOT NULL, photo_id bigint(20) NOT NULL, document_id bigint(20) NOT NULL DEFAULT 0,
  request_write_access tinyint(1) NOT NULL DEFAULT 0,
  version int(11) NOT NULL DEFAULT 1, deleted tinyint(1) NOT NULL DEFAULT 0,
  UNIQUE KEY bot_short (bot_id, short_name)
);
CREATE TABLE IF NOT EXISTS bot_app_launches (
  user_id bigint(20) NOT NULL, app_id bigint(20) NOT NULL, launched_at int(11) NOT NULL,
  PRIMARY KEY (user_id, app_id)
);
CREATE TABLE IF NOT EXISTS bot_attach_menus (
  bot_id bigint(20) NOT NULL PRIMARY KEY,
  short_name varchar(64) NOT NULL, url varchar(1024) NOT NULL,
  show_in_attach_menu tinyint(1) NOT NULL, show_in_side_menu tinyint(1) NOT NULL,
  request_write_access tinyint(1) NOT NULL, has_settings tinyint(1) NOT NULL,
  peer_types int(11) NOT NULL,          -- биты: same_bot_pm, bot_pm, pm, chat, broadcast
  icons text NOT NULL                   -- JSON: [{name, document_id, colors:[{name,color}]}]
);
CREATE TABLE IF NOT EXISTS bot_attach_menu_installs (
  user_id bigint(20) NOT NULL, bot_id bigint(20) NOT NULL, installed_at int(11) NOT NULL,
  PRIMARY KEY (user_id, bot_id), KEY bot (bot_id)
);
CREATE TABLE IF NOT EXISTS bot_games (
  id bigint(20) NOT NULL PRIMARY KEY, access_hash bigint(20) NOT NULL,
  bot_id bigint(20) NOT NULL, short_name varchar(64) NOT NULL,
  title varchar(64) NOT NULL, description varchar(512) NOT NULL,
  photo_id bigint(20) NOT NULL, document_id bigint(20) NOT NULL DEFAULT 0,
  deleted tinyint(1) NOT NULL DEFAULT 0,
  UNIQUE KEY bot_short (bot_id, short_name)
);
CREATE TABLE IF NOT EXISTS game_scores (
  game_id bigint(20) NOT NULL,
  chat_type tinyint(4) NOT NULL,        -- PEER_USER (пара диалога), PEER_CHAT, PEER_CHANNEL
  chat_id bigint(20) NOT NULL,          -- для лички: dialog_id (min/max пары) из pkg dialog id
  chat_id2 bigint(20) NOT NULL DEFAULT 0,
  user_id bigint(20) NOT NULL, score int(11) NOT NULL, updated_at int(11) NOT NULL,
  PRIMARY KEY (game_id, chat_type, chat_id, chat_id2, user_id),
  KEY ranking (game_id, chat_type, chat_id, chat_id2, score)
);
CREATE TABLE IF NOT EXISTS bot_prepared (                -- сообщения и кнопки для приложений
  id varchar(32) NOT NULL PRIMARY KEY,  -- base64url 18 случайных байт
  kind tinyint(4) NOT NULL,             -- 1 inline message, 2 keyboard button
  bot_id bigint(20) NOT NULL, user_id bigint(20) NOT NULL,
  payload mediumblob NOT NULL,          -- JSON BotInlineResult + peer_types / KeyboardButton
  expire_at int(11) NOT NULL, KEY expire (expire_at)
);
CREATE TABLE IF NOT EXISTS bot_widgets (                 -- общие виджеты
  widget_id bigint(20) NOT NULL PRIMARY KEY,
  bot_id bigint(20) NOT NULL, payload mediumblob NOT NULL, width int(11) NOT NULL, height int(11) NOT NULL,
  allow_queries tinyint(1) NOT NULL, public_data mediumtext NOT NULL, public_hash bigint(20) NOT NULL,
  period int(11) NOT NULL, updated_at int(11) NOT NULL, KEY bot (bot_id)
);
```
Сообщения с виджетами и играми лежат как обычно (`messageMediaGame`, `messageMediaWidget` в JSON сообщения); `chat_data` и `public_data` сообщения — в самом сообщении.

Redis (`bots` dao, ключи с TTL):
- `bot_webview:<query_id>` — JSON {user, bot, peer, reply_to, silent, send_as, top_msg_id, can_send_after} на 180 с, продление на каждом `prolongWebView`; `GETDEL` в `sendWebViewResultMessage`.
- `bot_widget_query:<query_id>` / `bot_widget_answer:<query_id>` — как callback (070), 10 с ожидания.
- `oauth_req:<token>` — запрос входа сайта (бот, `redirect_uri`, `state`, PKCE, области, `nonce`, браузер, адрес, регион, верный код, состояние `pending|accepted|declined`, user_id) на 300 с; `oauth_code:<code>` — код авторизации на 60 с, одноразовый (`GETDEL`).

### Пакеты
- `pkg/botkeys`: `Derive(token) (webappKey, loginKey)`; `Sealer` (AES-256-GCM на `BOT_SECRET_ENCRYPTION_KEY`, AAD = `bot_id ‖ назначение`, формат версии 1 как `pkg/pushdevices/seal.go`); `Store.Put/Get` над `bot_secret_keys` с проверкой `token_hash = bots.token`.
- `pkg/webappdata`: сборка initData (поля, `url.Values`-кодирование по алфавиту, `signature` Ed25519 по строке `"<bot_id>:WebAppData\n…"`, затем `hash` HMAC по всем полям кроме `hash`); сборка данных старого формата входа; `Verify` для тестов. Ключ Ed25519 — из `WEBAPP_SIGNING_KEY` (32 байта seed, hex).
- `pkg/botstore` (новые файлы `apps.go`, `attach.go`, `games.go`, `login.go`, `prepared.go`, `widgets.go`, `scores.go`, `webauth.go`): хранилища таблиц выше; каждая запись в транзакции, изменения настроек бота поднимают `bots.platform_version` и `bot_info_version` (как `touchBot`).
- `pkg/urlauth`: разбор OAuth-ссылки (все формы контракта), проверка домена кнопки, построение адреса с полями, коды совпадения (4 из фиксированного списка 32 эмодзи, присутствующих во всех клиентах; верный — `crypto/rand`), разбор `User-Agent` в «браузер» и «платформу» (для OAuth).

### bff `bots` (`app/bff/bots/internal/core/`, один файл на метод)
- `messages.requestUrlAuth_handler.go`, `acceptUrlAuth`, `declineUrlAuth`, `checkUrlAuthMatchCode`, `account.getWebAuthorizations`, `resetWebAuthorization`, `resetWebAuthorizations`.
  - Кнопка: найти сообщение и кнопку `button_id` тем же `pressedMessage`, что callback (`messages.callback_handlers.go`); кнопка должна быть `keyboardButtonUrlAuth`; бот входа — из сохранённых полей кнопки (см. ниже); домен — `bots.login_domain`; нет ключа бота — `urlAuthResultDefault`. Есть веб-сессия (бот, домен) и не просят новый доступ — `urlAuthResultAccepted(url с данными)` и `date_active`. Иначе `urlAuthResultRequest(bot, domain, request_write_access)`.
  - `acceptUrlAuth` повторяет проверки, пишет веб-сессию (браузер/платформа — приложение и устройство сессии из `authsession`, адрес — `c.MD.ClientAddr`, регион — новый RPC `authsession.getIpRegion`), при `write_allowed` — `bot_write_allowed(source=domain)` и `messageActionBotAllowed(domain)` в чат с ботом (один раз).
  - Ссылка `url`: OAuth-форма → запрос сайта из Redis (`URL_EXPIRED`, если нет; `OAUTH_REQUEST_INVALID`, если испорчен); иначе домен из `url_auth_domains` → бот этого домена → как кнопка; иначе `URL_INVALID`.
  - `in_app_origin`: только для OAuth; должен совпасть с одним из `origins` клиента OIDC, иначе `URL_INVALID`.
  - OAuth `acceptUrlAuth`: `match_code` обязателен и должен совпасть (`checkUrlAuthMatchCode` — `boolTrue`/`boolFalse`, неверный код в `accept` — `URL_INVALID` и запрос сгорает после 3 неверных); состояние → `accepted` с `user_id` и согласием на области (`write_allowed` → `telegram:bot_access`, `share_phone_number` → `phone`), ответ `urlAuthResultAccepted` без `url` (сайт получает результат опросом страницы). `declineUrlAuth` → `declined`, `boolTrue`.
- `messages.requestWebView_handler.go`, `requestSimpleWebView`, `requestMainWebView`, `requestAppWebView`, `prolongWebView`, `sendWebViewResultMessage`, `sendWebViewData`, `getBotApp`, `bots.canSendMessage`, `allowSendMessage`, `requestWebViewButton`, `getRequestedWebViewButton`.
  - Общий построитель `webview.go`: проверка бота (`botUser`), `platform` (0–64, `[A-Za-z0-9_]`), `theme_params` (JSON-объект ≤ 4 КиБ строк `#rrggbb`, иначе `THEME_PARAMS_INVALID`), `start_param` (≤ 512, `[A-Za-z0-9_-]`), адрес `https://` ≤ 1024 (`URL_INVALID`); `from_bot_menu`/`url` только в чате с этим ботом, иначе `BOT_WEBVIEW_DISABLED` (method/messages.requestWebView); права писать в чат и `send_as` — как `sendInlineBotResult` (`pkg/sendas`), `CHAT_WRITE_FORBIDDEN`, `YOU_BLOCKED_USER`.
  - initData: `user` (как видит бот: имя, фамилия, имя пользователя, язык сессии, `is_premium` по флагу `user.premium` — ADR 0041, `allows_write_to_pm`, `added_to_attachment_menu`, `photo_url` при имени пользователя), для лички из меню вложений — `receiver`, для групп и каналов — `chat`; `chat_type` и `chat_instance` (тот же `chatInstance`, что в callback, `messages.callback_handlers.go`) — для прямых и главных приложений из чата; `query_id` (строкой) и `can_send_after` — когда есть чат ответа; `start_param`; `auth_date`.
  - `requestSimpleWebView` без `query_id`; `from_switch_webview` — `url` должен совпасть с `switch_webview.url` последнего ответа инлайна этого бота этому пользователю (кэш инлайна 070), иначе `URL_INVALID`.
  - `requestAppWebView`: приложение по `inputBotAppID` (сверка `access_hash`, `BOT_APP_INVALID`), `write_allowed` → разрешение `source=app` и `messageActionBotAllowed(app)`; запуск пишется в `bot_app_launches`. `getBotApp`: `botAppNotModified`, если `hash` = `version`; `inactive` — нет запуска; `request_write_access`, `has_settings` из записи.
  - `sendWebViewResultMessage` (только бот, `USER_BOT_REQUIRED`): `GETDEL` запроса (`QUERY_ID_INVALID`), бот запроса = вызывающий; результат через `inlineResult` + `outOfResult` (`messages.inline_handlers.go`) и отправка от пользователя с `via_bot_id`, `reply_to`, `silent`, `send_as` запроса — общая функция, вынесенная из `MessagesSendInlineBotResult`; `updateWebViewResultSent(query_id)` в журнал seq пользователя.
  - `sendWebViewData`: только личка с ботом; `button_text` должен совпасть с `keyboardButtonSimpleWebView` в клавиатуре одного из последних 50 сообщений бота в этом чате; `data` ≤ 4096 байт (`DATA_TOO_LONG`); одно сообщение: у пользователя `messageActionWebViewDataSent(text)`, у бота `messageActionWebViewDataSentMe(text, data)` (поле `incoming_action`, ниже); `random_id` — идемпотентность как у `sendMessage`.
  - `bots.canSendMessage`: есть `bot_write_allowed` или пользователь писал боту (диалог с исходящими пользователя); `allowSendMessage`: разрешение `source=from_request`, служебное сообщение, ответ — `Updates` отправки.
  - `bots.requestWebViewButton` (бот): кнопка только `inputKeyboardButtonRequestPeer`/`keyboardButtonRequestPeer` (Bot API `savePreparedKeyboardButton`), нормализуется `botmarkup`, кладётся в `bot_prepared(kind=2)` на 24 ч; ответ `bots.requestedButton(webapp_req_id)`. `getRequestedWebViewButton` — только тот же пользователь, иначе `BUTTON_ID_INVALID`.
- `messages.getAttachMenuBots_handler.go` (заменяет файл 098), `getAttachMenuBot`, `toggleBotInAttachMenu`: см. ADR п. 5. `getAttachMenuBot` для бота без меню — `BOT_INVALID`. `toggleBotInAttachMenu(enabled=false)` удаляет установку; `updateAttachMenuBots` всем сессиям пользователя (journal seq, тип добавить в `userbox.SeqUpdate`).
- `messages.savePreparedInlineMessage_handler.go`, `getPreparedInlineMessage`: `result` → `inlineResult`; `user_id` — получатель; `peer_types` по умолчанию все; срок 24 ч; `getPreparedInlineMessage` отдаёт `preparedInlineMessage(query_id, result, peer_types, cache_time, users)`, где `query_id` — новый ответ инлайна в Redis на срок жизни (так `sendInlineBotResult` отправит его без изменений).
- `messages.sendBotRequestedPeer_handler.go`: кнопка `keyboardButtonRequestPeer` с `button_id` в сообщении бота в личке (или `webapp_req_id` из `bot_prepared`); число пиров ≤ `max_quantity` (1–10); каждый пир подходит под `peer_type` (бот/премиум у `requestPeerTypeUser`; создатель, имя, форум, права пользователя и бота у чатов и каналов — права через `pkg/channelperm`); `requestPeerTypeCreateBot` — `PEER_TYPES_INVALID` (управляемые боты вне задачи); сообщение: у пользователя `messageActionRequestedPeer(button_id, peers)`, у бота `messageActionRequestedPeerSentMe(button_id, requestedPeer*)` с именем, именем пользователя и фото только если кнопка их запросила (`name_requested`, `username_requested`, `photo_requested`).
- Игры: `messages.setGameScore_handler.go`, `setInlineGameScore`, `getGameHighScores`, `getInlineGameHighScores`, `messages.editInlineBotMessage_handler.go`; правки `messages.callback_handlers.go` (`game` → `updateBotCallbackQuery.game_short_name`, сообщение должно иметь `messageMediaGame` и кнопку игры), `messages.inline_handlers.go` (`inputBotInlineResultGame`, `inputBotInlineMessageGame` → `botInlineMediaResult` с игрой бота, `outOfResult` → `messageMediaGame`).
  - Счёт: `score ≥ 0` (`SCORE_INVALID`), только бот-владелец игры (`USER_BOT_REQUIRED`/`MESSAGE_ID_INVALID`), без `force` — строго больше (`BOT_SCORE_NOT_MODIFIED`); запись под `SELECT … FOR UPDATE` строки счёта. `edit_message` → служебное `messageActionGameScore(game_id, score)` от игрока ответом на сообщение игры (`msg.sendMessageV2` от имени игрока). Ответ `setGameScore` — `Updates` этой отправки (пустые `updates`, если служебного нет).
  - Таблица: позиция — `1 + COUNT(score > мой)` по индексу `ranking`; игрок, по два соседа и тройка лучших; `users` — как видит вызывающий.
- `editInlineBotMessage` / правка инлайна: `inputBotInlineMessageID64(owner_id, id)` → сообщение владельца, `via_bot_id` = вызывающий бот, иначе `MESSAGE_ID_INVALID`; новый `msg.EditTypeViaBot` (3) в `app/messenger/msg/msg/msg/vars.go`: правка от имени владельца без предела времени правки, разметка — `botmarkup`, медиа — как у `editMessage`.
- Виджеты (`app/bff/bots/internal/core/widgets.go` и по файлу на метод): `inputMediaWidget` (бот) — HTML ≤ 64 КиБ, `width`/`height` 1–4096, `public_data`/`chat_data` ≤ 4 КиБ; `payload = zlib(html)`; `shared_widget` → `bot_widgets` и `widget_id`; `inputMediaWidgetId` — только свой виджет; `messageMediaWidget(can_query = allow_queries, public_data/public_data_hash из общего, chat_data, widget_id)`. `getWidgetPublicData` — по сообщению с `widget_id`, `notModified(period)` при равном `hash`. `editWidgetData(widget_id, public_data, cache_time)` — `period = clamp(cache_time, 5, 3600)`, по умолчанию 30. `editMessageWidgetData` (бот, своё сообщение) — правка медиа сообщения у всех копий и `updateMessageWidgetData` с pts (как правка, через `msg`); `editInlineBotMessageWidgetData` — то же через инлайн-правку. `getBotWidgetQueryAnswer(peer, msg_id, query_type, data)` — сообщение с `can_query`; `widgetQueryTypeData` без `with_wallet` — `updateBotWidgetQuery` / `updateInlineBotWidgetQuery` боту и ожидание (как callback, ≤ 10 с, `BOT_RESPONSE_TIMEOUT`), иначе `WIDGET_QUERY_TYPE_UNSUPPORTED`; `data` ≤ 4 КиБ. `setBotWidgetQueryAnswer(query_id, result)` — бот запроса, `result` ≤ 4 КиБ.
- Сессия: `messages.getBotWidgetQueryAnswer` добавить в `runsAside` (`app/interface/session/internal/sess/main_auth_wrapper.go`; сегодня `waitsForBot`, `:769-777`, задача 099 B переименовывает его в `runsAside`).

### Кнопка входа в разметке (`pkg/botmarkup`)
`inputKeyboardButtonUrlAuth(text, fwd_text, url, bot, request_write_access)` → `keyboardButtonUrlAuth(text, fwd_text, url, button_id)`, где `button_id` — порядковый номер кнопки в разметке; в той же структуре `KeyboardButton` остаются поля `UserId_INT64` (бот входа: `bot` или сам бот сообщения) и `RequestWriteAccess`. Сообщения хранятся JSON-ом прото (`app/messenger/msg/internal/dao/send.go:55`), поэтому поля доживают до `requestUrlAuth`, а TL-кодирование `keyboardButtonUrlAuth` их не пишет (тест на кодирование). Проверка: хост `url` = `login_domain` бота входа (`BOT_DOMAIN_INVALID`), бот входа существует и это бот (`BOT_INVALID`). `keyboardButtonUrlAuth` от бота без `input`-формы — как сейчас `BUTTON_TYPE_INVALID`. Кнопка игры допускается только первой в первой строке и только в сообщении с игрой (`BUTTON_TYPE_INVALID`).

### `msg`: входящее действие
`OutboxMessage` (`app/messenger/msg/msg/msg/msg.tl.proto:101-109`) получает `mtproto.MessageAction incoming_action = 8`; при отправке в личку копия получателя пишется с этим действием вместо исходного (только для `messageService`). Перегенерация — `app/messenger/msg/msg/msg/build2.sh`, по образцу прошлых правок `msg.tl.proto` (`git log -- app/messenger/msg/msg/msg/msg.tl.proto`).

### Провайдер входа (`app/interface/links/internal/oauth/`)
- Хост `oauth.pluma.chat` (новый блок в `teamgramd/deploy/web/Caddyfile` → `links:9096`; `links` различает хост).
- `GET /auth?client_id&redirect_uri&response_type=code&scope&state&code_challenge&code_challenge_method=S256&nonce` — проверка клиента (`bot_login_clients`, `redirect_uri` точно из списка), `oauth_req` в Redis, страница: бот (имя, фото), QR со ссылкой `https://pluma.chat/oauth?startapp=<token>`, кнопка «Открыть Pluma» с той же ссылкой, эмодзи-код, опрос `GET /auth/state?token` раз в 2 с; `accepted` → редирект `redirect_uri?code&state`, `declined` → `error=access_denied`.
- `POST /token` — Basic `client_id:client_secret` или `client_secret_post`, `code_verifier` обязателен при PKCE, ответ `{id_token, token_type:"Bearer", expires_in}`; ID-токен RS256 (`kid` = SHA-256 открытого ключа), `iss = https://oauth.pluma.chat`, `aud = <bot_id>`, `sub`, `iat`, `exp` (+10 мин), `nonce`, профиль (`id`, `name`, `given_name`, `family_name`, `preferred_username`, `picture`) при `profile`, `phone_number`, `phone_number_verified` при `phone` и согласии. UserInfo нет (как у Telegram).
- Старый формат: `GET /auth/legacy?bot_id&origin&return_to&request_access=write` — то же подтверждение, затем редирект на `return_to` (хост = `login_domain` бота) с полями виджета и `hash`.
- Веб-сессия создаётся при подтверждении (браузер и платформа из `User-Agent`, адрес из `X-Forwarded-For` Caddy, регион — `authsession.getIpRegion`).
- `/.well-known/webapp-signing-key` — открытый ключ Ed25519 в hex.
- Тексты страницы — ключи в `app/interface/links/internal/links/texts.go` на языках сайта, как у всех страниц `links` и у страницы `m/<slug>` задачи 105 (ADR 0036; один механизм для страниц сайта, отдельного `oauth.json` нет), язык — `Accept-Language`; оформление — как страницы `links` (ADR 0036 п. 3); скрипт опроса — отдельный файл (CSP сайта).
- `links` получает Redis (`KV`) и MySQL уже есть; `bot_secret_keys` читает через `pkg/botkeys` (нужен `BOT_SECRET_ENCRYPTION_KEY` и в `links`).

### Конфигурация
- `app/bff/bots/internal/config` и `teamgramd/etc|etc2/bff.yaml`, `app/bff/bff/etc/bff.yaml`: `WebApp { Versions: {android, ios, tdesktop, macos, weba, webk, unknown}, SigningKey: ${WEBAPP_SIGNING_KEY} }`, `BotSecrets { EncryptionKey: ${BOT_SECRET_ENCRYPTION_KEY} }`, `Avatars: https://pluma.chat/_l/avatar/`.
- `links.yaml`: `OAuth { Issuer: https://oauth.pluma.chat, SigningKeyFile: ${OAUTH_SIGNING_KEY_FILE} }`, `KV`, `BotSecrets`.
- `authorization` (импорт токена бота) и `pluma-admin` (`admin.yaml`) — `BotSecrets`.
- `url_auth_domains` в `help.getAppConfig`: список `login_domain` всех ботов (кэш 60 с) вместо статического `[]` (`app_config.json:125`); строится там же, где сейчас читается `app_config.json` (`app/bff/configuration`).

### Обновления другим устройствам и пользователям
- `toggleBotInAttachMenu`, изменение меню бота администратором → `updateAttachMenuBots` (seq, пользователю и установившим).
- `sendWebViewResultMessage` → сообщение в чат (обычные обновления `msg`) и `updateWebViewResultSent` всем сессиям пользователя (seq, тип в `queryUpdateTypes` с TTL, `pkg/userbox/box.go:341`).
- `acceptUrlAuth`, `toggleBotInAttachMenu`, `requestAppWebView`, `allowSendMessage` с новым разрешением → `messageActionBotAllowed` в чат с ботом (обычная отправка, видна обеим сторонам).
- `setGameScore(edit_message)` → `messageActionGameScore` в чат игры.
- `editMessageWidgetData` → `updateMessageWidgetData` с pts всем, у кого сообщение (как правка: личка — обе копии, группа — участники, канал — `Channel-T`).
- `editInlineBotMessage` → `updateEditMessage`/`updateEditChannelMessage` как обычная правка.
- `resetWebAuthorization(s)` — обновления в схеме нет; другие устройства перечитывают список при открытии.
- Боту: `updateBotCallbackQuery.game_short_name`, `updateBotWidgetQuery`, `updateInlineBotWidgetQuery` (seq, TTL запросов, добавить в `SeqUpdate` и `queryUpdateTypes`).

### Пределы и ошибки
| что | предел / ошибка |
|---|---|
| `platform` | 0–64 `[A-Za-z0-9_]` (method/messages.requestWebView), иначе `PLATFORM_INVALID` |
| `theme_params` | JSON-объект ≤ 4 КиБ, `THEME_PARAMS_INVALID` |
| адрес приложения | `https://`, ≤ 1024, `URL_INVALID` |
| `start_param` | ≤ 512, `[A-Za-z0-9_-]`, `START_PARAM_INVALID` (выбор сервера) |
| запрос вебвью | 180 с от последнего `prolongWebView`, `QUERY_ID_INVALID` |
| `sendWebViewData.data` | 4096 байт (bots/webapps), `DATA_TOO_LONG` |
| подготовленное сообщение / кнопка | 24 ч (выбор сервера), не больше 100 живых на пару бот–пользователь |
| запрос OAuth | 300 с, `URL_EXPIRED`; 3 неверных кода — запрос сгорает |
| код авторизации OIDC | 60 с, одноразовый |
| ID-токен | 10 мин |
| `match_codes` | 4 эмодзи |
| счёт игры | int32 ≥ 0, `SCORE_INVALID`; `BOT_SCORE_NOT_MODIFIED` |
| HTML виджета | 64 КиБ до сжатия; данные — 4 КиБ; `MEDIA_INVALID` / `DATA_TOO_LONG` |
| период виджета | 5–3600 с, по умолчанию 30 (Android `WidgetMedia.java:1020`) |
| ожидание ответа бота (виджет) | 10 с, общий лимит 32 ожидания на пользователя (070) |
| запросы виджета, которые не данные | `WIDGET_QUERY_TYPE_UNSUPPORTED` |
| нет ключей бота / нет `BOT_SECRET_ENCRYPTION_KEY` | `BOT_WEBVIEW_DISABLED`; кнопка входа — `urlAuthResultDefault` |
Новые коды ошибок, которых нет в `third_party/teamgram-proto/mtproto/rpc_error_codes.go` (`URL_EXPIRED`, `OAUTH_REQUEST_INVALID`, `BOT_WEBVIEW_DISABLED`, `THEME_PARAMS_INVALID`, `PLATFORM_INVALID`, `START_PARAM_INVALID`, `PEER_TYPES_INVALID`, `BUTTON_ID_INVALID`, `WIDGET_QUERY_TYPE_UNSUPPORTED`), — в коммите `third_party(proto):` части A2.

### После перезапуска
- Всё постоянное — в MySQL; ключи — зашифрованы, ключ шифрования в `.env`.
- Перезапуск bff: открытые вебвью живут (Redis); ждущие `getBotWidgetQueryAnswer` получают `BOT_RESPONSE_TIMEOUT`, как callback.
- Потеря Redis: открытые вебвью закрываются клиентом (`QUERY_ID_INVALID`), незавершённые входы на сайт — `URL_EXPIRED`, сайт начинает заново.
- Просроченные `bot_prepared` удаляет фоновая чистка bff раз в час (`DELETE … WHERE expire_at < ? LIMIT 1000` в цикле).

### Файлы
Добавить: миграция; `pkg/botkeys/`, `pkg/webappdata/`, `pkg/urlauth/` с тестами; `pkg/botstore/{apps,attach,games,login,prepared,widgets,scores,webauth}.go` и интеграционные тесты; обработчики bff выше; `app/bff/bots/internal/server/grpc/service/*.go` для новых служб; `app/interface/links/internal/oauth/` (обработчики, страница, шаблоны, `poll.js`), ключи страниц в `links/texts.go`; `tools/pluma-admin/{botapp,botattach,botgame,botlogin}.go`; e2e — `test/e2e/bot_platform_test.go`, `test/e2e/bot_games_test.go`, `test/e2e/bot_widgets_test.go`, `test/e2e/oauth_test.go`.

Изменить: `pkg/botmarkup/botmarkup.go`; `pkg/botstore/store.go` (`Create`, `NewToken` возвращают и ключи), `tools/pluma-admin/bot.go` (`create`, `token`, `set -domain -main-app`); `app/bff/authorization/internal/core/auth.importBotAuthorization_handler.go` (запись ключей); `app/bff/bots/internal/core/messages.callback_handlers.go`, `messages.inline_handlers.go`; `app/bff/messages/internal/core/core_messages_helper.go` и `app/bff/messages/internal/dao/media.go` (`inputMediaGame`, `inputMediaWidget` — через вызов в bots-хранилище); `app/bff/files/internal/core/messages.uploadMedia_handler.go:199` (игра — `MEDIA_INVALID`, как сейчас, проверить); `app/messenger/msg/msg/msg/{msg.tl.proto,vars.go}` и обработка `incoming_action`, `EditTypeViaBot`; `pkg/userbox/box.go` (`SeqUpdate`, `queryUpdateTypes`); `app/interface/session/internal/sess/main_auth_wrapper.go` (`runsAside`, после 099 B); `app/bff/bff/internal/server/server.go` (регистрация служб); `teamgramd/etc/session.yaml`, `teamgramd/etc2/session.yaml`, `app/interface/session/etc/session.yaml` (маршруты `RPCSeamless`, `RPCMiniBotApps`, `RPCMainMiniBotApps`, `RPCPreparedInlineMessages`, `RPCBotWidgets`; `RPCBotMenu` и `RPCGames` уже маршрутизирует и регистрирует в `bots` задача 098, часть C); `app/bff/bff/client/fake_rpc_result.go` (убрать `TLAccountGetWebAuthorizations`); `app/service/authsession` (RPC `getIpRegion`); `app/service/biz/user/user/username.go` (`oauth` в `reservedUsernames`); `app/interface/links` (хост `oauth`, путь `/oauth` на `pluma.chat` — карточка «Открыть в Pluma»); `teamgramd/deploy/web/Caddyfile`; `app/bff/configuration` (`url_auth_domains`); `.env.example`, `docker-compose.yaml`, `pkg/conf`; `tools/logaudit/logaudit.go`; `docs/ops/bots.md`, `docs/ops/security-setup.md`, `docs/ops/deployment.md`.

## Порядок работ
Каждая часть — одна подветка, один исполнитель за один подход. «‖» — можно параллельно.

| часть | что | размер | после | общие файлы |
|---|---|---|---|---|
| A1 | Миграция; `pkg/botstore/*` новых таблиц с интеграционными тестами | M | — | `pkg/botstore` (новые файлы) |
| A2 ‖ A1 | `third_party(proto):` — служба `RPCBotWidgets`, коды ошибок, перегенерация; `pkg/botkeys`, `pkg/webappdata` с тестами; запись ключей в `pluma-admin bot create|token` и `auth.importBotAuthorization`; конфиг и секреты (`.env.example`, compose, `pkg/conf`, `logaudit`) | M | — | `pkg/botstore/store.go`, `tools/pluma-admin/bot.go` (A1 их не трогает) |
| B | `pluma-admin bot`: `set -domain -main-app`, `app`, `attach` (значки SVG/TGS), `game`, `login` (секрет печатается один раз), `webapp-key` (печать открытого ключа Ed25519); все новые команды отказывают встроенным ботам реестра `pkg/inlinebots` (099 A) той же ошибкой, что `token|set`; `docs/ops/bots.md` | M | A1, A2, 099 A | `tools/pluma-admin` |
| C | Вход по кнопке и ссылке, веб-сессии: `botmarkup`, `RPCSeamless` (без OAuth), `authsession.getIpRegion`, `url_auth_domains`, удаление выдумки из `fake_rpc_result.go`, маршрут, `messageActionBotAllowed` | M | A1, A2 | `server.go`, `session.yaml` ×3 |
| D1 ‖ C | Вебвью: `webview.go`, `requestWebView/Simple/Main/App`, `prolongWebView`, `getBotApp`, `canSendMessage`/`allowSendMessage`; маршруты `RPCMiniBotApps`, `RPCMainMiniBotApps`; проверка версий зондом | L | A1, A2 | `server.go`, `session.yaml` ×3 (сливать по одной) |
| D2 | `msg.incoming_action`; `sendWebViewResultMessage` (вынос общей отправки из `sendInlineBotResult` — единственный владелец выноса; 105 часть 4 встраивает ветку ярлыка уже в вынесенную функцию), `sendWebViewData`, `updateWebViewResultSent` | M | D1, 099 C | `messages.inline_handlers.go`, `msg.tl.proto`, `pkg/userbox/box.go` |
| E ‖ D1 | Меню вложений: три метода, `updateAttachMenuBots`, замена файла 098 (служба `RPCBotMenu` и маршрут уже есть — 098 C) | S–M | A1, B (для ручной проверки), 098 C | `pkg/userbox/box.go` (с D2 — по одной) |
| F | Подготовленные сообщения и кнопки, `sendBotRequestedPeer`, маршрут `RPCPreparedInlineMessages`, чистка `bot_prepared` | M | D2 | `messages.inline_handlers.go`, `msg` (`incoming_action`) |
| G1 | Игры: `inputMediaGame`, инлайн-игры, callback `game`, `setGameScore`/`getGameHighScores` в службе `RPCGames` 098 C (регистрация и маршрут уже есть, `getEmojiGameInfo` 098 не трогать) | M | A1, B, 098 C | `messages.callback_handlers.go`, `messages.inline_handlers.go`, `core_messages_helper.go` |
| G2 | `editInlineBotMessage`, `EditTypeViaBot`, `setInlineGameScore`, `getInlineGameHighScores` | M | G1 | `msg` (с D2 — по одной) |
| H | Виджеты: шесть методов, медиа виджета, `updateBotWidgetQuery`, `runsAside` (099 B), маршрут `RPCBotWidgets` | L | A2, G2, 099 B | `pkg/userbox/box.go`, `main_auth_wrapper.go` |
| I1 ‖ G1 | OAuth в bff: разбор ссылок, `oauth_req`, `match_codes`, `in_app_origin`, `declineUrlAuth`, `checkUrlAuthMatchCode`; `oauth` в зарезервированных именах | M | C | `messages.requestUrlAuth_handler.go` и соседние |
| I2 | Провайдер `oauth.pluma.chat` в `links`: `/auth`, страница, опрос, `/token`, JWKS, discovery, старый формат, ключи страниц в `links/texts.go`, Caddy, карточка `/oauth` на pluma.chat | L | I1, 105 часть 2 (тоже правит `links/route.go`, `page.go`, `texts.go`) | `app/interface/links`, Caddyfile |
| J | Документы и сводная проверка: `docs/ops/*`, `TESTING-BACKLOG`, `apicoverage`, полный прогон на сборной ветке | S | все | — |

## Тесты
Модульные:
- `pkg/webappdata`: initData по тексту core.telegram.org/bots/webapps — независимая проверка `hash` (HMAC по токену) и `signature` (Ed25519 по строке с `bot_id`) обратным алгоритмом; `hash` включает `signature`, `signature` не включает `hash`; кодирование JSON `user` и порядок полей; данные старого формата и их `hash` по `SHA256(token)`; вектор, вычисленный отдельно сторонним скриптом (Python `hmac`/`nacl`) и записанный в тест.
- `pkg/botkeys`: запечатать/открыть, чужой бот или назначение не открываются, другой ключ — ошибка; `Derive` против известных значений.
- `pkg/urlauth`: все формы OAuth-ссылки (https pluma.chat/t.me, `tg://oauth`, `tg://resolve?domain=oauth&startapp`, `plm://…`), мусор — `URL_INVALID`; домен кнопки (регистр, порт, поддомен, `http`); построение адреса при уже имеющемся запросе в URL; коды совпадения (4 разных, верный среди них).
- `pkg/botmarkup`: кнопка входа → `keyboardButtonUrlAuth` с `button_id`; TL-кодирование не содержит бота и `request_write_access`; чужой домен — `BOT_DOMAIN_INVALID`; кнопка игры не первой — `BUTTON_TYPE_INVALID`.
- bff: построитель фрагмента (версия по платформе, `tgWebAppStartParam`, `tgWebAppFullscreen`), проверки `theme_params`/`platform`/`start_param`; таблица рекордов (соседи и тройка на краях); виджет — zlib и `notModified`.
- `tools/logaudit`: новые имена (`initdata`, `tgwebappdata`, `webappkey`, `loginkey`, `clientsecret`, `idtoken`, `matchcode`, `oauthtoken`).

Интеграционные (`pkg/testinfra`, настоящие MySQL, Redis; по одному пакету за раз):
- миграция дважды подряд на пустой и заполненной базе;
- `pkg/botstore`: приложения, меню и установки (hash меняется с версией), игры и счёт под гонкой двух `setGameScore` (побеждает больший, `BOT_SCORE_NOT_MODIFIED` второму), веб-сессии (уникальность пользователь–бот–домен, сброс), `bot_prepared` с истечением и чисткой;
- `pkg/botkeys.Store`: ключи от старого токена не отдаются после `NewToken`;
- `msg`: `incoming_action` — у отправителя и получателя разные действия, один `dialog_message_id`, удаление у обоих.

Сквозные (`pkg/mtclient`, стенд `test/e2e`, сессии после `getState`; бот — аккаунт с токеном из `pluma-admin` стенда):
- `TestUrlAuthButton`: бот с доменом шлёт кнопку входа; пользователь `requestUrlAuth` → `Request`; `acceptUrlAuth(write_allowed)` → `Accepted` с адресом, `hash` проверяется токеном; служебное `messageActionBotAllowed(domain)`; второй `requestUrlAuth` → сразу `Accepted`; `getWebAuthorizations` — одна запись; `resetWebAuthorization` → снова `Request`. Проверить падающим на `master` (`BUTTON_TYPE_INVALID`).
- `TestWebViewInitData`: `requestWebView` из чата с ботом → URL с фрагментом; `tgWebAppData` проверяется токеном бота и открытым ключом стенда; `prolongWebView` → `true`; бот `sendWebViewResultMessage` → сообщение в чате с `via_bot_id`, вторая сессия пользователя получает `updateWebViewResultSent`; повтор — `QUERY_ID_INVALID`.
- `TestWebViewData`: бот шлёт клавиатуру с `keyboardButtonSimpleWebView`; `sendWebViewData` → у пользователя `messageActionWebViewDataSent`, у бота `…SentMe` с данными; 4097 байт — `DATA_TOO_LONG`.
- `TestBotAppDirectLink`: `getBotApp` (inactive) → `requestAppWebView(write_allowed)` → `getBotApp` с `hash` → `botAppNotModified`, `inactive` снят.
- `TestAttachMenu`: `getAttachMenuBots` пусто → `getAttachMenuBot` (`inactive`) → `toggleBotInAttachMenu` → вторая сессия получает `updateAttachMenuBots`, список с ботом и значком; выключить.
- `TestPreparedAndRequestedPeer`: бот `savePreparedInlineMessage` → пользователь `getPreparedInlineMessage` → `sendInlineBotResult` в группу; бот `requestWebViewButton` → `getRequestedWebViewButton` → `sendBotRequestedPeer(webapp_req_id)` → бот получает `messageActionRequestedPeerSentMe`.
- `TestGames`: бот `sendMedia(inputMediaGame)` в группу; пользователь жмёт кнопку игры → бот получает `game_short_name`, отвечает `url` → пользователь получает `url`; `setGameScore(edit_message)` → служебное от игрока; меньший счёт — `BOT_SCORE_NOT_MODIFIED`; `getGameHighScores`; инлайн-игра и `setInlineGameScore`.
- `TestWidgets`: бот шлёт общий виджет; `getWidgetPublicData` → данные, с тем же `hash` → `notModified`; `editWidgetData` меняет `hash`; `editMessageWidgetData` → участник получает `updateMessageWidgetData`; `getBotWidgetQueryAnswer` ↔ `setBotWidgetQueryAnswer`; запрос кошелька — `WIDGET_QUERY_TYPE_UNSUPPORTED`.
- `TestOAuthSite` (HTTP к `links` стенда): `/auth` → токен; пользователь `requestUrlAuth(https://pluma.chat/oauth?startapp=…)` → `Request` с `match_codes`; неверный `checkUrlAuthMatchCode` → `false`; `acceptUrlAuth(match_code)`; `/auth/state` → `accepted` → `/token` с PKCE → ID-токен проверяется JWKS; повтор кода — ошибка; `declineUrlAuth` → `access_denied`.

## Проверки перед сдачей (CachyOS)
- На ветке каждой части: `go build ./...`; `go vet -tags integration,e2e -unreachable=false` и `go test` затронутых пакетов; интеграционные и e2e пишутся и компилируются.
- На сборной ветке: все интеграционные `pkg/botstore`, `pkg/botkeys`, `msg`; e2e раздела «Тесты» со своими `E2E_PROJECT`/`E2E_DIR`, затем `test/e2e/run.sh down`; `-race`; `make lint`; `go test ./tools/...`; `go test ./pkg/conf/...`; `go run ./tools/apicoverage`.
- На маке ничего не собирать.

## Проверки для `docs/project/TESTING-BACKLOG.md`
```
## Платформа ботов (задача 106)
- [ ] Android, iOS, Desktop, Web: кнопка «Войти» тестового бота → окно входа с доменом → сайт стенда показывает имя и «подпись верна»; повторное нажатие входит без окна.
- [ ] «Настройки → Устройства → Подключённые сайты»: сайт виден с браузером и городом; «Отключить» и «Отключить все» работают, после этого кнопка снова спрашивает.
- [ ] Вход на тестовый сайт через oauth.pluma.chat: на компьютере — QR-кодом с телефона, на телефоне — кнопкой «Открыть Pluma»; выбрать эмодзи с экрана; сайт получил имя; с «разрешить сообщения» бот может написать.
- [ ] Мини-приложение из кнопки под сообщением, из кнопки меню, из профиля бота («Открыть приложение»), по прямой ссылке pluma.chat/<бот>/<приложение>: открывается, видит имя и тему, кнопка «Отправить» кладёт сообщение в чат и закрывает окно.
- [ ] Приложение из клавиатурной кнопки отправляет данные: в чате служебная строка, бот получил данные.
- [ ] Меню вложений: добавить бота по ссылке startattach, значок в меню скрепки на всех клиентах, открыть в личке и в группе, удалить.
- [ ] Приложение «Поделиться» подготовленным сообщением в выбранный чат; «Выбрать чат» из приложения отправляет боту чат.
- [ ] Игра: бот присылает игру, «Играть» открывает её, после игры в чате «вы набрали N», таблица рекордов в игре.
- [ ] Виджет в сообщении бота рисуется на Android и Desktop, обновляет общие данные, кнопка в виджете получает ответ бота.
- [ ] Тёмная тема и язык клиента приходят в приложение (theme_params, language_code).
```

## Эксплуатация
- **Миграция** `migrate-20261121-bot-platform.sql` — служба миграций, безопасно повторять.
- **Новые переменные** (`.env.example`, compose для `bff`, `links`, `migrate` не нужен, `admin`):
  - `BOT_SECRET_ENCRYPTION_KEY` — `openssl rand -hex 32`; без него мини-приложения и вход выключены. Потеря ключа — все боты должны войти заново (ключи перевыведутся при входе).
  - `WEBAPP_SIGNING_KEY` — seed Ed25519, `openssl rand -hex 32`; открытый ключ печатает `pluma-admin bot webapp-key`, его — в `docs/ops/bots.md` и разработчикам ботов.
  - `OAUTH_SIGNING_KEY_FILE` — RSA 2048 в PEM (`openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048`), файл в каталоге секретов сервера, монтируется в `links` только для чтения.
- **DNS и Caddy:** запись `oauth.pluma.chat` как у `pluma.chat`; блок сайта в `teamgramd/deploy/web/Caddyfile` → `links:9096`; сертификат — как у остальных доменов. Шаг владельца.
- **Замена ключей** — `docs/ops/bots.md`: `WEBAPP_SIGNING_KEY` — после замены открытые приложения нужно открыть заново; `OAUTH_SIGNING_KEY_FILE` — JWKS публикует старый и новый ключ сутки (поле `OAuth.PreviousKeyFile`).
- **Ресурсы:** таблицы маленькие (≤100 пользователей), Redis — килобайты; памяти и диска сверх текущего не нужно.
- **Логи:** не пишутся `initData`, токены OAuth, коды авторизации, ID-токены, `client_secret`, коды совпадения, телефоны (`tools/logaudit`).
- **Ручная настройка ботов** — `docs/ops/bots.md`: домен, главное приложение, прямые приложения, меню вложений, игры, клиент OIDC (секрет показывается один раз).

## Решения к подтверждению
1. Вход на сайты через собственный провайдер `oauth.pluma.chat` входит в эту задачу (части I1–I2), домен — `oauth.pluma.chat`.
2. Всё о ботах (домен, приложения, меню вложений, игры, клиенты входа) настраивает только администратор через `pluma-admin`; из клиента — нет (в API Telegram методов нет).
3. Меню вложений включается администратором для отдельных ботов; соглашение Telegram (`side_menu_disclaimer_needed`) не показывается.
4. Виджеты делаются, хотя Telegram их не документирует: поведение выведено из кода клиентов; пределы (64 КиБ HTML, 4 КиБ данных) — свои.
5. Свой ключ Ed25519 для `signature`; сторонние проверки должны использовать ключ Pluma, проверка `hash` токеном работает без изменений.
6. Бот, не входивший после миграции, не открывает приложений и не логинит кнопкой, пока не войдёт токеном. Повторный вход при перезапуске не гарантирован (библиотеки с сохранённой сессией его, по-видимому, не делают — не проверено), поэтому при выкате оператор выдаёт работающим ботам новый токен `pluma-admin bot token` (шаг в `docs/ops/bots.md`).
7. Сроки и пределы: вебвью 180 с от продления, подготовленные сообщения и кнопки 24 ч, запрос входа 5 мин, 4 эмодзи-кода, ID-токен 10 мин, `start_param` ≤ 512.
8. Телефон сайту отдаётся только в OIDC с областью `phone` и согласием в приложении; кнопка входа телефон не передаёт (у `inputKeyboardButtonUrlAuth` нет такого флага).
9. При `edit_message` прежнее служебное «набрал N» того же игрока не удаляется.
10. Боты-стражи вступления (`requestChatJoinWebView`, `setJoinChatResults`) — отдельной задачей позже.
11. Ввод телефона на странице входа и пуш `OAUTH_REQUEST` не делаются (iOS их не поддерживает).

## Не проверено
- Какие `tgWebAppVersion` и какие ещё параметры фрагмента Telegram сервер отдаёт каждой платформе. Часть D1 снимает это зондом (`reference-telegram-probe`: `messages.requestSimpleWebView` к настоящему боту через Web K `invokeApi` с `platform` android/ios/tdesktop/weba) и записывает в конфиг; до этого конфиг держит `8.0` (tweb `webApp.tsx:252-254` подменяет именно `8.0`, значит, Telegram отдаёт Web K эту версию).
- Где Telegram даёт `query_id` (есть ли он у главного и прямого приложения), значение `can_send_after` и срок жизни запроса вебвью — документация не говорит.
- Как Telegram решает, что пользователь «уже вошёл» на сайт (для `urlAuthResultAccepted` без окна), и создаёт ли кнопка входа запись в «Подключённых сайтах» — вывод из документации, сам Telegram не проверен.
- Срок жизни подготовленного сообщения у Telegram (`expire_date`), число `match_codes`, формат браузера и платформы в `urlAuthResultRequest`.
- Удаляет ли Telegram прежнее «набрал N» при новом рекорде и правит ли сообщение игры.
- Вся семантика виджетов вне кода клиентов: `background_query`, `is_copy`, `updateBotWidgetCopyQuery`, настоящие пределы размера.
- Принимают ли iOS и Web K ссылку `plm://oauth?token=` после патчей ADR 0036 (Android и tdesktop разбирают путь `oauth` на любом своём хосте) — проверка владельца на устройствах.

## Связи с задачами пачки
- **098** — должна слиться раньше: 106 заменяет её `messages.getAttachMenuBots` и добавляет игровые методы в ту же регистрацию `RPCGames`, где 098 отвечает `messages.getEmojiGameInfo` (`emojiGameUnavailable`). Если 098 зарегистрировала `RPCGames`/`RPCBotMenu` в другом модуле, 106 переносит регистрацию в `bots`, сохраняя файл обработчика 098.
- **099** — сливается раньше: обе правят `messages.inline_handlers.go` (099 — медиа по адресу и `getWebFile`); вынос общей отправки (D2) и инлайн-игры (G1) делаются поверх 099.
- **104** — без пересечений (`bots.toggleUsername`/`reorderUsernames` 106 не берёт).
- **105** — подключённые бизнес-боты не здесь. Общие файлы: `messages.inline_handlers.go` (105 часть 4 — после D2), `messages.callback_handlers.go` (105 часть 8 — после G1), `pkg/userbox/box.go` (105 часть 3 и D2/E/H — разные волны), `msg.tl.proto` (105 части 3–5 и D2/G2 — разные волны), `app/interface/links` (I2 — после 105 части 2), `tools/pluma-admin/bot.go` (105 часть 6 и A2/B), `server.go`/`session.yaml`. `pkg/qtsqueue` — только 105.
- **100, 101, 102, 103** — не пересекаются; `server.go`/`session.yaml` — только соседние строки.

## Результат
Платформа ботов сделана: части A1–J. Работают кнопки входа и вход на сайты, мини-приложения, меню вложений, игры, подготовленные сообщения и кнопки, правка инлайн-сообщений и виджеты в сообщениях ботов. Свой провайдер входа — `oauth.pluma.chat`. Код в партии `batch/20261115`, на прод не выкачен. Для выкладки нужны DNS `oauth.pluma.chat`, ключи (`BOT_SECRET_ENCRYPTION_KEY`, `WEBAPP_SIGNING_KEY`, файл `OAUTH_SIGNING_KEY_FILE`) и новые токены работающим ботам (`docs/ops/bots.md`).

- **A1.** `migrate-20261121-bot-platform.sql` и `pkg/botstore`.
- **A2.** Ключи ботов из токенов в `bot_secret_keys`, зашифрованы AES-256-GCM. `initData` мини-приложений подписан ключом бота и Ed25519 сервера. Служба `RPCBotWidgets` и коды ошибок (`third_party(proto)`).
- **B.** `pluma-admin`: `bot set -domain -main-app`, `bot app`, `bot attach`, `bot game`, `bot login`.
- **C.** `messages.requestUrlAuth` и `acceptUrlAuth` для кнопок входа, `url_auth_domains` в app config, подключённые сайты (`account.getWebAuthorizations` и сброс), служба `RPCSeamless`.
- **D1.** `requestWebView`, `requestSimpleWebView`, `requestMainWebView`, `requestAppWebView`, `prolongWebView`, `getBotApp`, `bots.canSendMessage`, `allowSendMessage`. Без ключей ответ `BOT_WEBVIEW_DISABLED`.
- **D2.** `sendWebViewResultMessage` и `sendWebViewData`.
- **E.** Меню вложений: `getAttachMenuBot`, `toggleBotInAttachMenu`, `updateAttachMenuBots` на другие устройства. Это настоящая замена пустого ответа задачи 098.
- **F.** Подготовленные сообщения и кнопки (24 ч), `sendBotRequestedPeer`.
- **G1.** Игры: отправка, кнопка «Играть», `setGameScore`, `getGameHighScores`.
- **G2.** `editInlineBotMessage` (`EditTypeViaBot`), счёт инлайн-игр.
- **H.** Виджеты: шесть методов, состояние, запросы с таймаутом и пределом, `updateMessageWidgetData`.
- **I1.** Запросы входа страницы oauth в приложениях: коды-эмодзи, `checkUrlAuthMatchCode`, `declineUrlAuth`.
- **I2.** Провайдер OpenID Connect и формата виджета входа в `links`.
- **J.** Сквозные тесты, порядок выкладки и замена ключей в `docs/ops`.

**Исправлено после слияния.**
- Виджеты всегда отклонялись: сессия не ставит `RpcMetadata.IsBot`, поэтому путь виджета теперь сам узнаёт, бот ли отправитель (`cde8986c7`).
- Прокси bff больше не обрывает через 5 с колбэк бота (ожидание 10 с), виджет (15 с) и встроенные инлайн-боты (`cd6e216f1`).
- `Issuer` берётся из `PLUMA_OAUTH_DOMAIN` (`bbf8f5ddb`).
- Cookie привязки OAuth всегда `Secure`.
- Бизнес-бот не шлёт виджеты и игры (`9a9d34e18`).
- При удалении аккаунта удаляются строки платформы. `game_scores` сохраняется намеренно (`f7a8983e5`, `docs/ops/bots.md`).

**Отклонения от брифа.** По коммитам и документам частей не найдены.

**Решения.** Ждут подтверждения п. 1–11 раздела «Решения к подтверждению». Главное: свой провайдер на `oauth.pluma.chat`; настройка ботов только через `pluma-admin`; свой ключ Ed25519 для `signature`; новые токены ботам при выкладке.

**Известное.**
- Сессия никогда не ставит `RpcMetadata.IsBot`, и все проверки `c.MD.IsBot` в bff не действуют. Это `translateText`, `togglePeerTranslations`, `getGameHighScores`, коды входа, `getFullUser`, `editMessage`, быстрые ответы, `createStickerSet`, инлайн. Исправлен только путь виджета; нужна отдельная задача и, вероятно, ADR.
- Изменения меню вложений из `pluma-admin` не рассылаются: клиент видит их после перезапуска.
- Если отправитель удалил инлайн-сообщение у себя в личке, бот больше не может его править.

**Не проверено.** В клиентах — пункты задачи 106 в `TESTING-BACKLOG.md`, в том числе ссылка `plm://oauth?token=` на iOS и в Web K. Поведение Telegram — раздел «Не проверено» выше.

**Проверка.** Партия `batch/20261115`, полный прогон на CachyOS на `2c1b155cf`: `go build ./...` и `go vet -tags integration,e2e -unreachable=false` обоих модулей; `go test ./...` — 237 пакетов ok, proto — 3; `go test ./tools/...` (с журнальным аудитом) — 6; `-race` — 191 пакет ok; `make lint` — 0 замечаний; интеграционные тесты — 70 пакетов по одному, все ok; e2e на свежем стенде — 300 PASS, 0 FAIL, 2 SKIP (замер задержки и ручной тест). На прод не выкачено, в клиентах владельцем не проверено.
