# 098. Ошибки и шум клиентов

- Статус: todo
- Решение: без нового ADR. Опирается на ADR 0009 (наши правки `third_party/teamgram-proto`), ADR 0020 (журнал `seq`), ADR 0041 (Premium всем), ADR 0057/0061 (содержимое не уходит с домашнего сервера).
- Зависит от: 009 (пуши), 025 (очередь жалоб `pkg/moderation`), 044/053 (реакции в супергруппах, `reactions.LoadChannel`), 070 (боты: `app/bff/bots`), 073 (`pkg/logincode`, код входа приходит сообщением 777000 в другие сессии).
- Связь с партией: 106 (платформа ботов) потом наполнит `RPCBotMenu` и `RPCGames` (службы и маршруты создаёт часть C этой задачи, 106 их не дублирует); `inputMessageCallbackQuery` не берёт ни одна задача партии. `messages.summarizeText` — владелец эта задача (часть D), 101 его не трогает. `messages.getRecentLocations` — владелец эта задача (часть G); 100 его не делает. 103 (статистика) пишет события заглушения поверх исправления части A.
- Ветка: `task/098-noise-and-fixes`.

## Цель
Убрать ошибки, которые клиенты получают сегодня на запуске и при обычной работе, и починить найденную ошибку: выключение уведомлений канала и супергруппы. Каждый метод из области отвечает так, как ждут официальные клиенты, — настоящим ответом из настоящих данных, а не заглушкой. После задачи в логах bff при запуске iOS, Android, Desktop и Web нет строк `blocked, License key` от методов этой задачи.

Главное, что найдено при подготовке (прочитано в коде, не запускалось):
- **iOS бесконечно повторяет `account.getWebBrowserSettings`** раз в ≤5 с на каждом устройстве: запрос идёт через `retryRequestIfNotFrozen` (iOS `TelegramCore/Sources/Settings/AccountWebBrowserSettings.swift:110-111`), а тот повторяет при любой ошибке, кроме `FROZEN_METHOD_INVALID`, без предела попыток (`TelegramCore/Sources/Network/Network.swift:1513-1525`, `maxRetries: nil`, `maxDelay: 5.0`). Причина — `RPCWebBrowser` зарегистрирован в bff (`app/bff/bff/internal/server/server.go:674-678`), но не прописан в `session.yaml`, и прокси отвечает `ERR_ENTERPRISE_IS_BLOCKED` (`app/bff/bff/client/fake_rpc_result.go:48`). Это первая по важности часть.
- **Выключение уведомлений канала и супергруппы не работает ни в одном клиенте**: ветка `PEER_CHANNEL` (`app/bff/notification/internal/core/account.updateNotifySettings_handler.go:114-129`) требует `svcCtx.Plugin`, а bff создаёт службу с `nil` (`server.go:539-547`) — ответ `ERR_ENTERPRISE_IS_BLOCKED`. Тема форума идёт другим путём (`:62-79`), e2e мьютят только личные чаты и темы.
- **Android шлёт `messages.reportReaction` старым конструктором** `0x61422a48 peer:InputPeer id:int user_id:InputUser` (Android `tgnet/TLRPC.java:64027-64043`), которого нет в нашем кодеке (`third_party/teamgram-proto/mtproto/codec_schema.tl.pb.go:319585-319607` знает только `0x3f64c076`).

## Область
В задаче:
1. `account.updateNotifySettings` для канала и супергруппы (ошибка).
2. Настройки встроенного браузера: `account.getWebBrowserSettings`, `account.updateWebBrowserSettings`, `account.toggleWebBrowserSettingsException` (`#60ed4229` и старый `#2d0a0571`), `account.deleteWebBrowserSettingsExceptions` — хранение на пользователя, хэш, обновления другим устройствам, маршрут в `session.yaml`.
3. `messages.getAttachMenuBots` — настоящий пустой список с хэшем (служба `RPCBotMenu`); `messages.getEmojiGameInfo` — `messages.emojiGameUnavailable` (служба `RPCGames`). Остальные методы этих служб — `ErrEnterpriseIsBlocked` до 106.
4. `messages.reportReadMetrics`, `messages.reportMusicListen` — принять, проверить и **хранить** (решение владельца 2026-11-15: «давай собирать по-настоящему»); данные доступны статистике (103).
5. `messages.reportReaction` — проверка прав, жалоба в очередь модерации как у остальных жалоб; старый конструктор Android через правку `third_party(proto)`.
6. `messages.summarizeText` — вместо пустого успеха (выдуманные данные) честный отказ `ErrEnterpriseIsBlocked`, пока нет модели.
7. `messages.receivedMessages` — без `TODO`, по семантике документации.
8. `messages.getMessages` / `channels.getMessages`: `inputMessagePinned`.
9. `account.invalidateSignInCodes` и серверное аннулирование кода входа, который пользователь отправил или переслал в чат (core.telegram.org/api/auth, «Invalidating login codes»).
10. `messages.getRecentLocations`.
11. `messages.getEmojiURL` — решение: оставить отказ (обоснование ниже), без изменения кода, с записью в реестре функций.

Не в задаче:
- `inputMessageCallbackQuery` в `messages.getMessages` — нужен журнал «запрос кнопки → сообщение в ящике бота» (сейчас `BeginCallback` хранит только `query_id → bot_id`, `app/bff/bots/internal/dao/queries.go:118-121`); в задачи партии не входит (106 его не берёт), записать в STATUS как известное. Сейчас конструктор молча пропускается; 098 этого не меняет, только переписывает комментарий без `TODO`.
- `messages.getAttachMenuBot`, `messages.toggleBotInAttachMenu`, игры (`setGameScore` и т. д.) — 106.
- Графики по метрикам прочтения и прослушиваниям — 103 (хранение делает эта задача, часть D).
- `account.getWebAuthorizations` и сброс веб-сессий — вместе с URL-авторизацией ботов (106).
- `account.getNotifySettings` для канала с `TODO(@benqi)` «проверить, что канал есть» (`account.getNotifySettings_handler.go:62-63`) и `channels.getMessages` без `inputMessageReplyTo` — замечены, в задачу не входят, записать в STATUS как известные.
- Правка и остановка живой геопозиции (`editMessage` с `inputMediaGeoLive{stopped}`) — не проверялась; `TestRecentLocations` части G проверяет и правку точки (от неё зависит `edit_date` в хэше). Если правка не работает, это не 100 (там её нет): записать в STATUS как известное и в «Результат», отдельная задача.

## Контракт клиентов
Пути клиентов — от корней `~/pluma-work/reference-src-new/{android/TMessagesProj/src/main/java/org/telegram, ios/submodules, tdesktop/Telegram/SourceFiles, tweb/src}`. Схема — `tdesktop/.../mtproto/scheme/api.tl` (слой 233). Страницы core.telegram.org прочитаны 2026-11-15.

### Уведомления канала
- Клиенты мьютят канал через `inputNotifyPeer{inputPeerChannel}` (реестр функций: Android `messenger/NotificationsController.java:6120`, iOS `TelegramCore/Sources/State/ManagedPendingPeerNotificationSettings.swift:166`, tdesktop `apiwrap.cpp:2126`). Ответ `Bool`, состояние приходит обновлением `updateNotifySettings` с `seq` (ADR 0020, п. 2).

### Встроенный браузер
Схема: `account.webBrowserSettings#79eb8cb3 flags:# open_external_browser:flags.0?true display_close_button:flags.1?true external_exceptions:Vector<WebDomainException> inapp_exceptions:Vector<WebDomainException> hash:long`, `account.webBrowserSettingsNotModified#c31c8f4e`, `webDomainException#933ca597 flags:# domain:string url:string title:string favicon:flags.0?long`, `updateWebBrowserSettings#c39a2ade`, `updateWebBrowserException#140502d1 flags:# delete:flags.1?true open_external_browser:flags.0?Bool exception:WebDomainException` (`api.tl:485-486, 2235-2238, 2572-2575`).
- **Кто зовёт.** iOS на запуске и раз в час (`TelegramCore/Sources/State/AccountTaskManager.swift:146` → `State/ManagedWebBrowserSettingsUpdates.swift:7-15`) с хэшем из кэша (`AccountWebBrowserSettings.swift:133-140`). Android — кэш с часовым обновлением (`messenger/MessagesController.java:25392-25419`, `:25508`). Desktop и Web не зовут (реестр функций).
- **Значения по умолчанию.** iOS без ответа сервера считает `openExternalBrowser = false`, то есть встроенный браузер (`AccountWebBrowserSettings.swift:33-35`). Android без настроек открывает ссылки во внешнем браузере / Custom Tabs (`MessagesController.java:25361-25371`: `isWebBrowserInAppEnabled()` ложно при `webBrowserSettings == null`), а переключатели настроек ничего не делают — они меняют только существующий объект (`:25633-25643`). Значит сегодня Android-пользователь не может включить встроенный браузер вообще.
- **Хэш.** `getWebBrowserSettings(hash)`: на совпадение — `NotModified` (Android `:25512-25521`, iOS `:116-118`). iOS при `NotModified` ничего не меняет.
- **Исключение.** `toggleWebBrowserSettingsException` отвечает `Updates`; iOS меняет своё состояние **только** по `updateWebBrowserException`/`updateWebBrowserSettings` внутри ответа (`AccountWebBrowserSettings.swift:180-218`), иначе исключение не появится. Android применяет ответ и перечитывает настройки (`MessagesController.java:25444-25454`, `:25474-25488`).
  - Добавление в один список убирает домен из другого: Android `:25672-25700` (`addWebBrowserExceptionFromList` + `deleteWebBrowserExceptionFromList`), iOS `:86-94`.
  - Удаление: iOS шлёт `url = exception.url`, а если он пуст — `domain` (`SettingsUI/Sources/Data and Storage/WebBrowserSettingsController.swift:536-538`), и сравнивает по `url` или по `domain` (`AccountWebBrowserSettings.swift:66-72`). Android сравнивает по домену без учёта регистра (`tgnet/tl/TL_account.java:4401-4409`) и передаёт в `url` иногда голый домен (`ui/ArticleViewer.java:4870`).
  - Совпадение домена при открытии ссылки — равенство или суффикс `"." + domain` (Android `MessagesController.java:25622-25631`); домен Android берёт как authority в нижнем регистре (`messenger/AndroidUtilities.java:6240-6257`).
  - Предел — 100 исключений в списке, 5 на тестовом бэкенде (Android `MessagesController.java:25378-25383`); Android проверяет его сам перед добавлением (`ui/ArticleViewer.java:4880`).
- **Очистить всё.** `deleteWebBrowserSettingsExceptions` отвечает полными настройками; Android до ответа локально ставит оба флага в `true` и очищает списки, а ответом перезаписывает (`MessagesController.java:25491-25506`).
- **Отображение.** Строка исключения — `title` крупно и `domain` подписью, `favicon` — значок (iOS `WebBrowserSettingsController.swift:292, 308`; Android `ui/web/WebBrowserSettings.java:309-311, 337-339`).

### Меню вложений и игра с кубиком
- `messages.getAttachMenuBots#16fcc2cb hash:long = AttachMenuBots`, `attachMenuBots#3c4301c0 hash:long bots users`, `attachMenuBotsNotModified#f1d88a5c` (`api.tl:1564-1565, 2774`). Документация: «Returns installed attachment menu bot mini apps», хэш по core.telegram.org/api/offsets#hash-generation.
  - iOS на запуске с `hash: 0` (`force: true`, `AccountTaskManager.swift:110` → `TelegramEngine/Messages/AttachMenuBots.swift:286-301`); ошибку глотает. Web — при старте (`lib/appManagers/appAttachMenuBotsManager.ts:70`) и при открытии меню (`components/chat/input.ts:1684`). Android — `messenger/MediaDataController.java:568-582` (берёт `hash` из ответа). Desktop — `inline_bots/bot_attach_web_view.cpp:2698-2715`.
- `messages.getEmojiGameInfo#fb7e8ca7`, `messages.emojiGameUnavailable#59e65335` (`api.tl:2189-2192, 2853`) — игра со ставкой в TON, вне области (решение владельца), но ответ `emojiGameUnavailable` настоящий.
  - iOS на запуске и раз в час (`AccountTaskManager.swift:137` → `State/ManagedEmojiGameUpdates.swift:79-105`), ошибку глотает.
  - Android: без ответа `stakeDiceInfo` остаётся `null`, и каждый вызов `loadStakeDiceInfo` идёт на сервер снова (`MessagesController.java:25338-25357`); `emojiGameUnavailable` кэшируется и даёт «недоступно».
  - Desktop: `emojiGameUnavailable` → пустые `DiceGameOptions` (`chat_helpers/stickers_dice_pack.cpp:156-163`).

### Метрики прочтения и прослушивания
- `messages.reportReadMetrics#4067c5e6 peer:InputPeer metrics:Vector<InputMessageReadMetric> = Bool`, `inputMessageReadMetric#402b4495 msg_id view_id time_in_view_ms active_time_in_view_ms height_to_viewport_ratio_permille seen_range_ratio_permille` (`api.tl:2202, 2861`). Документация: «Report viewport read metrics for visible messages…», «Only users can use this method», ошибок не перечисляет.
  - Android — каждая группа и канал (`ui/ChatActivity.java:8778`, `ui/Components/chat/ChatActivityMessageMetricsView.java:101-114`), ответ игнорирует. Desktop — любая история и ответы, пачками раз в 5 с (`api/api_read_metrics.cpp:16, 34-72`, `history/view/history_view_list_widget.cpp:131-139`), ошибку не показывает. iOS (`TelegramEngine/Messages/TelegramEngineMessages.swift:1995-2020`) и Web (`lib/appManagers/appMessagesManager.ts:15757-15800`) так же.
  - `height_to_viewport_ratio_permille` может быть больше 1000: iOS считает `heightToViewportRatio * 1000` без ограничения (`TelegramEngineMessages.swift:2010`).
- `messages.reportMusicListen#ddbcd819 id:InputDocument listened_duration:int = Bool` (`api.tl:2862`). iOS шлёт `inputDocument` с `listenedDuration` (`TelegramEngineMessages.swift:2023-2034`) и ошибку глотает; Web — `appMessagesManager.ts:15748-15753`.

### Жалоба на реакцию
- `messages.reportReaction#3f64c076 peer:InputPeer id:int reaction_peer:InputPeer = Bool` (`api.tl:2787`). Документация: «Report a message reaction», ошибки `MSG_ID_INVALID`, `PEER_ID_INVALID`, `USER_ID_INVALID`.
- Desktop: пункт есть в профиле участника, открытом из реакции, только в **супергруппе**; пожаловаться может любой участник **публичной** супергруппы и не на себя, забанить — тот, кто может ограничивать (`api/api_report.cpp:185-194`, `info/profile/info_profile_actions.cpp:1322-1357, 2457`); после ответа — «Спасибо за жалобу».
- Android: только из листа удаления сообщений администратора в режиме «только реакции», и только если у участника ровно одно сообщение (`ui/Components/DeleteMessagesBottomSheet.java:1171-1176`), старым конструктором с `InputUser` (`tgnet/TLRPC.java:64027-64043`), ответ не читает.
- iOS: `reactionPeer` — автор реакции (`TelegramCore/Sources/TelegramEngine/Peers/ReportPeer.swift:196-209`), ошибку глотает.

### Краткий пересказ
- Кнопку «Кратко» клиенты показывают только у сообщения с `summary_from_language` (`message#7600b9d3 … summary_from_language:flags2.11?string`, `api.tl:126`): Desktop `history/history_item_helpers.cpp:986-988` (`CanBeSummarized`), Android `messenger/TranslateController.java:143`, Web `components/chat/bubbles.ts:10653`. Сервер это поле не заполняет нигде (поиск `SummaryFromLanguage` по `app/` пуст), поэтому до `summarizeText` клиенты сами не доходят.
- На ошибку: Android закрывает окно и показывает «Не удалось перевести» (`ui/Components/TranslateAlert2.java:384-398`), но на `TRANSLATIONS_DISABLED_ALT` уходит в запасной перевод на устройстве (`:384-385`) — этот ответ слать нельзя (текст ушёл бы стороннему переводчику, ADR 0057). iOS любую ошибку, кроме перечисленных, считает `.generic` (`TelegramEngine/Messages/Summarize.swift:30-44`).

### Подтверждение доставки
- `messages.receivedMessages#5a954c0 max_id:int = Vector<ReceivedNotifyMessage>`. Документация: «Confirms receipt of messages by a client, cancels PUSH-notification sending»; `receivedNotifyMessage` — «Message ID, for which PUSH-notifications were cancelled» (`flags` — «Reserved for future use»).
- Зовёт только Web при каждом новом максимальном id (`lib/appManagers/appMessagesManager.ts:13421-13435`), ответ не читает.

### `inputMessagePinned` и `inputMessageCallbackQuery`
- Ни один из четырёх клиентов их не шлёт: поиск по исходникам находит их только в файлах схемы (`ios/submodules/TelegramApi/swift_scheme.tl`, `api.tl:1166-1167`, `tweb/src/layer.d.ts`). Документация: `inputMessagePinned` — «Pinned message»; `inputMessageCallbackQuery` — «Used by bots for fetching information about the message that originated a callback query». То есть это сторона ботов.

### Аннулирование кодов входа
- `account.invalidateSignInCodes#ca8ae8ba codes:Vector<string> = Bool` (`api.tl:2535`). core.telegram.org/api/auth, раздел «Invalidating login codes»: серверы Telegram сами аннулируют коды входа, которые пользователь отправил или переслал в другой чат; клиент должен вызвать метод, когда пользователь делает снимок экрана или пересылает текстовое сообщение 777000 с кодом; код — 5–7 десятичных цифр, возможно с `-`, которые клиент убирает перед отправкой.
- iOS: снимок экрана чата 777000 → коды по регулярному выражению `\b\d{5,7}\b` из видимых сообщений (`TelegramUI/Sources/ChatController.swift:7945-7958`) → `_internal_invalidateLoginCodes` (`TelegramCore/Sources/Authorization.swift:1428-1434`), ошибку глотает. Остальные клиенты метод не зовут.
- Наш код входа — 6 цифр (`pkg/logincode/code.go`, `Length`), под регулярное выражение iOS подходит.

### Последние геопозиции
- `messages.getRecentLocations#702a40e0 peer:InputPeer limit:int hash:long = messages.Messages` (`api.tl:2704`). Документация: «Get all recent live locations sent to a specific chat: returns up to 1 location message (`messageMediaGeoLive`) per chat participant»; `peer` — «Target group or private chat»; хэш по offsets.
- Android: экран карты чата, `limit = 100`, не зовёт для вещательного канала, оставляет из ответа только `messageMediaGeoLive` (`ui/LocationActivity.java:2590-2625`). iOS заполняет им дыру тега `.liveLocation` с `hash: 0` (`TelegramCore/Sources/State/Holes.swift:819-827`).

### `messages.getEmojiURL`
- Документация: URL для входа на платформу переводов и предложения ключевых слов эмодзи, действителен 30 с. Зовёт только Android: значок вопроса в пустом результате поиска эмодзи → лист «Предложить» → запрос; при ошибке лист просто снимает индикатор и ничего не открывает (`ui/Components/EmojiView.java:8246-8305`).
- Платформы переводов у Pluma нет; ссылка на `translations.telegram.org` увела бы человека к Telegram с нашим аккаунтом, который там не работает. **Решение: метод остаётся `ErrEnterpriseIsBlocked`** (как сейчас, `app/bff/catalog/internal/server/grpc/service/catalog_service_impl.go:493-496`); в `docs/project/feature-registry.json` — запись «не нужно» с этой причиной.

## Дизайн

### Часть A. Уведомления канала
- В `account.updateNotifySettings_handler.go` ветка `PEER_CHANNEL`: канал через `c.svcCtx.Dao.Channel.ChannelGetMutableChannel` (как `forumOf`, `account.getNotifySettings_handler.go:88-108`): несовпадение `access_hash` — `PEER_ID_INVALID`, `channelForbidden` для пользователя — `CHANNEL_PRIVATE`; `peerChannel = channel.ToChat(...)` уходит в `Chats` пуша, как сейчас.
- Общую проверку вынести в один помощник (`channelOf(peer)`), которым пользуются и `forumOf`, и эта ветка.
- Поле `Plugin` и параметр `notification_helper.New(..., plugin)` больше ничем не используются — убрать их и пакет `app/bff/notification/plugin`, если поиск по репозиторию других пользователей не находит (`server.go:547` передаёт `nil`).
- Обновление другим устройствам уже идёт через biz `UserSetNotifySettings` с `seq` (ADR 0020) — не трогать.

### Часть B. Встроенный браузер
**Хранение** — `teamgramd/deploy/sql/migrate-20261119-web-browser-settings.sql`, идемпотентно (`CREATE TABLE IF NOT EXISTS`):
```sql
-- Настройки встроенного браузера аккаунта (task 098); нет строки — значения по умолчанию.
CREATE TABLE IF NOT EXISTS `user_web_browser_settings` (
  `user_id` bigint(20) NOT NULL,
  `open_external_browser` tinyint(1) NOT NULL,
  `display_close_button` tinyint(1) NOT NULL,
  `updated_at` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Исключения: один домен — в одном из двух списков.
CREATE TABLE IF NOT EXISTS `user_web_browser_exceptions` (
  `user_id` bigint(20) NOT NULL,
  `domain` varchar(253) COLLATE utf8mb4_unicode_ci NOT NULL,
  `url` varchar(1024) COLLATE utf8mb4_unicode_ci NOT NULL,
  `open_external` tinyint(1) NOT NULL,   -- 1: external_exceptions, 0: inapp_exceptions
  `created_at` bigint(20) NOT NULL,      -- порядок в списке
  PRIMARY KEY (`user_id`, `domain`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```
**Пакет** `pkg/webbrowser`: `Settings{OpenExternal, DisplayCloseButton bool; External, InApp []Exception}`, `Exception{Domain, Url string}`; интерфейс `Store` (`Get`, `SetFlags`, `PutException`, `DeleteException(domain, list *bool)`, `DeleteAllExceptions`) с MySQL-реализацией и `memstore` для модульных тестов — как `pkg/pushdevices` и `pkg/twofa`.
- Значения по умолчанию (нет строки): `open_external_browser = false`, `display_close_button = true` (см. «Решения»).
- `PutException` — в одной транзакции: `INSERT … ON DUPLICATE KEY UPDATE url, open_external, created_at` (домен переходит в другой список), затем проверка предела: больше 100 в этом списке — откат и `LIMIT_INVALID`.
- `Hash(s Settings) int64` — первые 8 байт SHA-256 от канонической записи (флаги, затем списки в порядке выдачи: домен, url), со сдвигом в неотрицательное; `0` заменяется на `1`, чтобы клиент с `hash = 0` («кэша нет») всегда получал полный ответ. Хэш из содержимого, без счётчика версий: одинаковые настройки — одинаковый хэш, после перезапуска тоже.
- **Нормализация адреса** (`NormalizeURL`): обрезать пробелы; без схемы — разобрать как `//` + строка (как Android `MessagesController.java:25598-25602, 25608-25612`); `domain` = host в нижнем регистре, без `userinfo`, порта и завершающей точки; IDN — в punycode (`golang.org/x/net/idna`, уже в зависимостях, проверить `go.mod`); пустой host, host длиннее 253, `url` длиннее 1024 — `URL_INVALID`. Сохраняется исходный `url` (обрезанный).
- `webDomainException`: `domain`, `url`, `title = domain` (заголовок страницы сервер не загружает: запрос к стороннему сайту с домашнего сервера ради заголовка не нужен), `favicon` не заполняется.

**Обработчики** (`app/bff/webbrowser/internal/core/`, у всех: бот — `BOT_METHOD_INVALID`):
- `account.getWebBrowserSettings(hash)`: `Get`, хэш совпал — `account.webBrowserSettingsNotModified`, иначе полный ответ.
- `account.updateWebBrowserSettings(flags)`: `SetFlags`; ответ — полные настройки; другим устройствам — `updateWebBrowserSettings` с новыми флагами.
- `account.toggleWebBrowserSettingsException#60ed4229`: без `delete` и без `open_external_browser` — `INPUT_REQUEST_INVALID` (клиенты всегда шлют одно из двух); без `delete` — `PutException` в список по флагу; с `delete` — удаление по домену из списка по флагу или из обоих. Ответ — `updates{updates:[updateWebBrowserException{delete?, open_external_browser?, exception}], users:[], chats:[], date, seq:0}`; то же обновление — другим устройствам. Удаление несуществующего — тот же ответ (идемпотентно).
- `#2d0a0571` (тот же смысл, ответ `Bool`; ни один закреплённый клиент его не шлёт) — общий код, ответ `boolTrue`, обновление — всем устройствам, включая вызвавшее.
- `account.deleteWebBrowserSettingsExceptions`: удалить оба списка (флаги остаются — так называется метод), ответ — полные настройки; другим устройствам — по `updateWebBrowserException{delete}` на каждый удалённый домен (≤ 200), одним контейнером.
- **Обновления другим устройствам** — `SyncUpdatesNotMe` с `seq = 0`, как черновики (`app/bff/drafts/internal/core/messages.saveDraft_handler.go:167-173`). В журнал `seq` (ADR 0020) не включаем: потерянный пуш восстанавливает часовой опрос с хэшем у обоих клиентов, которые зовут эти методы. Сбой пуша — в лог, ответ остаётся успешным (данные уже записаны).
- **Подключение**: `webbrowser.Config` получает `Mysql` и `SyncClient` (`server.go:674-678`, как `notification_helper`); маршрут `"/mtproto.RPCWebBrowser": "bff.bff"` в `teamgramd/etc/session.yaml`, `teamgramd/etc2/session.yaml`, `app/interface/session/etc/session.yaml`.

### Часть C. `RPCBotMenu` и `RPCGames`
- В `app/bff/bots` (у него уже `RPCBotMenuButton`, `server.go:179`) — реализации `mtproto.RPCBotMenuServer` и `mtproto.RPCGamesServer`, регистрация в `server.go`, маршруты `"/mtproto.RPCBotMenu": "bff.bff"` и `"/mtproto.RPCGames": "bff.bff"` (сейчас закомментирован, `teamgramd/etc2/session.yaml:112`) во всех трёх `session.yaml`.
- `messages.getAttachMenuBots(hash)`: список ботов меню вложений — пустой (их нет до 106); `hash` — по алгоритму offsets от id ботов списка, для пустого списка `0`; `attachMenuBotsNotModified` — только когда хэш клиента равен нашему и не `0`. Ответ `attachMenuBots{hash, bots:[], users:[]}`. Функцию списка сделать так, чтобы 106 добавил источник ботов, не меняя обработчик.
- `messages.getEmojiGameInfo` — `messages.emojiGameUnavailable` (постоянно: TON вне области).
- Остальные методы обеих служб (`getAttachMenuBot`, `toggleBotInAttachMenu`, `setGameScore`, `setInlineGameScore`, `getGameHighScores`, `getInlineGameHighScores` и прочие из интерфейса) — `mtproto.ErrEnterpriseIsBlocked` с той же строкой лога, что у соседей (CLAUDE.md: 400, а не gRPC Unimplemented). Сегодня они так и отвечают через `fake_rpc_result`, поведение не меняется.
- Бот на `getAttachMenuBots`/`getEmojiGameInfo` — `BOT_METHOD_INVALID`.

### Часть D. Мелкие ответы
- **`messages.reportReadMetrics`** (`app/bff/reports`, новый `messages.reportReadMetrics_handler.go`, `service.go:92-96` — на `core`): бот — `BOT_METHOD_INVALID`; `peer` — `inputPeerUser/Chat/Channel/Self` (через `peerOf`, `report.go:213-223`), иначе `PEER_ID_INVALID`; метрик больше 1000 — `LIMIT_INVALID`; у метрики `msg_id <= 0` — `MSG_ID_INVALID`; отрицательные `time_in_view_ms`, `active_time_in_view_ms`, `height_to_viewport_ratio_permille`, `seen_range_ratio_permille` или `seen_range_ratio_permille > 1000` — `INPUT_REQUEST_INVALID`. Ответ `boolTrue`. **Метрики хранятся** (решение владельца 2026-11-15) в таблице `message_read_metrics` (миграция `migrate-20261119-read-metrics.sql`, идемпотентная): `user_id`, `peer_type`, `peer_id`, `msg_id`, `view_id`, `time_in_view_ms`, `active_time_in_view_ms`, `height_to_viewport_ratio_permille`, `seen_range_ratio_permille`, `date`; первичный ключ `(peer_type, peer_id, msg_id, user_id, view_id)`, запись `INSERT IGNORE` пачкой (повтор того же показа не дублируется), индекс `(user_id, date)` для удаления данных пользователя. Перед записью — проверка, что пользователь видит чат (участник канала или группы, собеседник личного чата; кэш ответа на минуту на пару пользователь–чат, чтобы пачки раз в 5 с не били biz), и что `msg_id` не больше верхнего id чата; чужие чаты — `PEER_ID_INVALID`. Удаление аккаунта и выход из канала данные не трогают (как просмотры `channel_message_views`); удаление аккаунта — удалить строки пользователя. В лог значения не пишутся. Чтение для статистики — функция `Aggregate(peer, msg_ids)` (суммы и медианы по сообщению), которой пользуется 103.
- **`messages.reportMusicListen`**: бот — `BOT_METHOD_INVALID`; `id` не `inputDocument` или `id == 0` — `DOCUMENT_INVALID`; `listened_duration < 0` — `INPUT_REQUEST_INVALID`; ответ `boolTrue`. **Хранится** (решение владельца) в `music_listens` (`user_id`, `document_id`, `listened_duration`, `date`; та же миграция), не чаще одной строки на пару пользователь–документ в минуту; документ должен существовать в dfs/media, иначе `DOCUMENT_INVALID`.
- **`messages.summarizeText`** (`app/bff/messages/internal/core/messages.summarizeText_handler.go`): `mtproto.ErrEnterpriseIsBlocked` и строка лога как у соседей; комментарий: сервер не заполняет `summary_from_language`, поэтому клиенты кнопку не показывают; отказ, а не пустой текст. Не `TRANSLATIONS_DISABLED_ALT` (см. контракт). Обновить строку комментария схемы на конструктор слоя 233 (`#abbbd346 … tone:flags.2?string`).
- **`messages.receivedMessages`**: бот — `BOT_METHOD_INVALID`; `max_id < 0` — `MSG_ID_INVALID`; ответ — пустой вектор, и это верный ответ: пуш уходит сразу на устройства без живой сессии (`app/messenger/push/internal/core/core.go:233-248`), отложенных пушей, которые можно отменить, у сервера нет, значит отменённых сообщений нет. Комментарий объясняет это вместо `TODO`.
- **`inputMessagePinned`**:
  - `channels.getMessages`: id самого нового закреплённого сообщения канала тем же запросом, что `channels.getFullChannel` (`ChannelSearch` с `MEDIA_PINNED`, `Limit: 1`, `channels.getFullChannel_handler.go:159-169`); нет закреплённого — ничего не добавляет.
  - `messages.getMessages`: у метода нет чата, «закреплённое сообщение» не определено — конструктор ничего не добавляет; комментарий вместо `TODO` (решение к подтверждению, поведение Telegram не проверено).
  - `inputMessageCallbackQuery` в `messages.getMessages`: комментарий «не обслуживается: нужен журнал запроса кнопки» вместо `TODO`; поведение не меняется.

### Часть E. Жалоба на реакцию
- **Правка `third_party(proto)`** (ADR 0009, отдельный коммит `third_party(proto): …`, строки с `// pluma:`): декодер `TLMessagesReportReaction` принимает и `0x61422a48 peer:InputPeer id:int user_id:InputUser` — `user_id` превращается в `ReactionPeer = inputPeerUser{user_id, access_hash}` (`inputUserSelf` — `inputPeerSelf`); регистрация конструктора в таблицах классов и имён, как у вариантов `summarizeText` (`schema.tl.crc32.pb.go:2896-2897`, `codec_schema.tl.pb.go:323771-323854`). Кодировать этот вариант серверу не нужно. Перед правкой проверить, что сейчас делает session с неизвестным конструктором в запросе Android (ответ или обрыв), и записать в брифе.
- **Обработчик** `messages.reportReaction` в `app/bff/catalog` (метод принадлежит `RPCReactions`, `catalog_service_impl.go:91-95`):
  1. бот — `BOT_METHOD_INVALID`; `peer` не канал — `PEER_ID_INVALID`;
  2. `ChannelGetMutableChannel` с вызывающим: не мегагруппа (вещательный канал, монофорум) — `PEER_ID_INVALID`; `channelForbidden` — `CHANNEL_PRIVATE`;
  3. право: администратор с `ban_users` (или создатель) — всегда; остальные — только в публичной супергруппе (есть `username`); иначе `CHAT_ADMIN_REQUIRED`. Это объединение Desktop (любой в публичной, `api_report.cpp:189-193`) и Android (лист администратора);
  4. сообщение из `ChannelGetMessages` от лица вызывающего, нет или `messageEmpty` — `MSG_ID_INVALID`;
  5. `reaction_peer` — `inputPeerUser` (или `inputPeerSelf` — тогда `USER_ID_INVALID`, на себя нельзя); его реакция на это сообщение есть в `reactions.LoadChannel` — иначе `USER_ID_INVALID`;
  6. жалоба: `moderation.Report{Kind: KindReaction, PeerType: PEER_CHANNEL, PeerId, TargetUserId: автор реакции, Reason: "reaction"}`, снимок — сообщение (`snapshotMessage`), автор реакции и сообщения, название чата и сама реакция (новое поле `Snapshot.Reaction string json:"reaction,omitempty"`, ключ `reactions.Key`); часовой предел `moderation.ReportsPerHour` — как в `stories/internal/core/report.go:96-104` (`FLOOD_WAIT_3600`).
- `pkg/moderation`: `KindReaction int8 = 8` с комментарием; `catalog` получает `moderation.New(sqlx.NewMySQL(&c.Mysql))` в dao (у него уже есть `Mysql`, `app/bff/catalog/internal/config/config.go:26`). Действия по жалобе (`pkg/moderation/effects`) — общие ограничения пользователя, отдельного эффекта для реакции нет; удалить реакцию модератор может руками (`messages.deleteParticipantReactions` уже есть).

### Часть F. Коды входа
- **`pkg/logincode`**: `(*Service).Invalidate(ctx, phone string, codes []string) (int, error)` — нормализует коды (убрать `-`, оставить только `Length` цифр, дубли убрать, не больше 100), читает ожидающие запросы телефона (`store.requests`), для каждого живого слота (`hmac`, `admin_hmac`) сравнивает HMAC (`Hasher.Verify`); совпал любой код — `drop(phone, hash)` (запрос пропадает целиком: и код приложения, и код администратора). Попытки не считаются, пределы не тратятся. Возвращает число аннулированных запросов. Ни коды, ни телефон в лог не пишутся (только число; `tools/logaudit`).
- Узкий конструктор для того, кому нужно только аннулирование: `NewInvalidator(c Config, kv kv.Store)` без списка допуска (`NewService` требует `Allowlist` и MySQL).
- **`account.invalidateSignInCodes`** (`app/bff/authorization`, `account.invalidateSignInCodes_handler.go`): бот — `BOT_METHOD_INVALID`; кодов больше 100 — `LIMIT_INVALID`; телефон вызывающего — через `UserClient`; `Invalidate`; ответ `boolTrue` и тогда, когда ничего не совпало (клиент ответ не читает, а иначе метод становился бы оракулом).
- **Серверное аннулирование** (документация: сервер сам аннулирует отправленные или пересланные коды): в `app/bff/messages` после успешных `messages.sendMessage`, `sendMedia` (подпись), `sendMultiMedia` (подписи), `forwardMessages` (тексты пересылаемых сообщений, которые обработчик уже читает, `messages.forwardMessages_handler.go:207`) и `editMessage` (новый текст), только для пользователя (не бота):
  - быстрый отбор без обращений: в тексте есть последовательность из цифр и `-`, дающая ровно `Length` цифр (функция `logincode.Candidates(text) []string`, модульный тест); нет — ничего не делать;
  - есть — `Invalidate` по телефону отправителя (один `UserGetImmutableUser` и одно чтение Redis на такое сообщение), в той же горутине после ответа biz, со своим сроком 2 с; ошибка — только в лог без кода; отправка от этого не зависит и не замедляется больше чем на этот срок.
  - Все чаты, включая «Избранное» (см. «Решения»).
  - Конфигурация: `messages_helper.Config` получает `LoginCode` (только `HmacKey`) и `KV` из конфига bff (`app/bff/bff/internal/config/config.go:36-39`); секрет не уходит в другой контейнер.

### Часть G. Последние геопозиции
- **Тип медиа.** Правка `third_party(proto)` (ADR 0009): `MEDIA_GEO_LIVE = 14` в `message_filter_util.go` и ветка `messageMediaGeoLive` в `GetMediaType` (сейчас геопозиция — `MEDIA_EMPTY`). Пишется в `message_filter_type` при отправке и правке без других изменений: `send.go:74, 261`, `channel_send.go:790, 1031`, `channel_edit.go:151, 261` уже зовут `GetMediaType`. Проверить, обновляет ли правка в личных ящиках `message_filter_type`; если нет — добавить, иначе правка с другой медиа сломает отбор.
- **Миграция** `teamgramd/deploy/sql/migrate-20261119-geo-live-filter.sql`: `UPDATE messages SET message_filter_type = 14 WHERE message_filter_type <> 14 AND <медиа сообщения — messageMediaGeoLive>` и то же для `channel_messages`; путь к имени конструктора в `message_data` взять из того, как msg сериализует сообщение (`jsonx`), и проверить на настоящей строке в интеграционном тесте. Повторный запуск ничего не меняет. Таблица `messages` не шардирована (`ShardingSize` в конфигах не задан, `CalcTableName` → `messages`, `messages_dao.go:44-49`) — проверить в `teamgramd/etc*/biz.yaml` перед написанием.
- **Отбор**: личный чат и базовая группа — `message.searchByMediaType` с `MEDIA_GEO_LIVE` по ящику вызывающего (`app/service/biz/message/internal/core/message.searchByMediaType_handler.go`); супергруппа — `ChannelSearch` с тем же типом от лица вызывающего; вещательный канал — пустой `messages.channelMessages` (Android его не спрашивает, документация говорит «group or private chat»).
  - Из выборки — **по одному самому новому сообщению на отправителя**, только живые: `date + period > now` или `period == 0x7fffffff` (бессрочная); новые сверху; не больше `limit` (1…100; `limit <= 0` → 100).
  - Глубина выборки: брать страницами по 100 от новых, пока не набрано `limit` отправителей или сообщения не старше `now − 86400` (дольше суток живой бывает только бессрочная; для неё — отдельная проверка тем же отбором без нижней границы, но не больше 500 строк). Предел защищает от медленного запроса в чате с длинной историей геопозиций.
  - **Хэш**: offsets-алгоритм по парам (`id`, `edit_date`) выдачи (правка геопозиции меняет `edit_date`, а id — нет); совпал с `hash` клиента и не `0` — `messages.messagesNotModified{count}`. Помощник хэша — локальный, как в `app/messenger/msg/internal/dao/scheduled.go:148`.
  - Ответ: `messages.messages` для личного чата и группы, `messages.channelMessages` с `pts` канала для супергруппы; `users`/`chats` — как в `channels.getMessages`.
- Обработчик — `app/bff/messages/internal/core/messages.getRecentLocations_handler.go` (сейчас `ErrEnterpriseIsBlocked`); бот — `BOT_METHOD_INVALID`.

### Ошибки (сводка)
`BOT_METHOD_INVALID` (боты; «Only users can use this method» в документации каждого метода), `PEER_ID_INVALID`, `CHANNEL_PRIVATE`, `MSG_ID_INVALID`, `USER_ID_INVALID`, `CHAT_ADMIN_REQUIRED`, `DOCUMENT_INVALID`, `URL_INVALID`, `LIMIT_INVALID`, `INPUT_REQUEST_INVALID`, `FLOOD_WAIT_3600`, `ErrEnterpriseIsBlocked` (`summarizeText`, `getEmojiURL`, неслужимые методы `RPCBotMenu`/`RPCGames`). Все константы уже есть в `third_party/teamgram-proto/mtproto` (например, `ErrInputRequestInvalid`, `ErrLimitInvalid`, `ErrUrlInvalid`).

### После перезапуска
Всё состояние — в MySQL (настройки браузера, жалобы, `message_filter_type`) и Redis (запросы кодов входа); в памяти ничего. Хэши считаются из содержимого и после перезапуска те же. Потерянный пуш браузера восстанавливает часовой опрос клиентов.

### Тексты, нагрузка
- Новых видимых пользователю текстов нет (`title` исключения — домен).
- Нагрузка: `getWebBrowserSettings` — одна строка и ≤200 строк исключений раз в час на устройство; метрики — одна пакетная вставка на пачку и проверка доступа с минутным кэшем; аннулирование при отправке — только для сообщений с 6-значным числом; `getRecentLocations` — индексированный отбор по `message_filter_type` (проверить `EXPLAIN` на индекс ящика с `message_filter_type`; если индекса нет, добавить его в миграцию части G).

## Порядок работ
Части по размеру — на одного исполнителя за один заход. S — до полудня, M — около дня.

| Часть | Размер | Параллельно с | Общие файлы |
|---|---|---|---|
| A. Уведомления канала | S | всеми | `server.go` (строка `notification_helper.New`) |
| B. Встроенный браузер | M | A, D, E, F, G | `server.go`, три `session.yaml` — **с C** |
| C. `RPCBotMenu`, `RPCGames` | S | A, D, E, F, G; **после B** (или тем же исполнителем) | `server.go`, три `session.yaml`, `app/bff/bots` (с 106!) |
| D. Мелкие ответы | S | всеми | `app/bff/messages` — с F и G (разные файлы) |
| E. Жалоба на реакцию | M | всеми | `third_party/teamgram-proto` — с G (разные файлы; отдельные коммиты `third_party(proto)`), `pkg/moderation` |
| F. Коды входа | M | всеми | `app/bff/messages` (новый файл + вызовы в 5 обработчиках), `server.go` (конфиг messages) |
| G. Последние геопозиции | M | всеми | `third_party/teamgram-proto`, `app/bff/messages`, biz message, миграция |
| H. Документы | S | в конце | `api-coverage.md`, `feature-registry.json`, `TESTING-BACKLOG.md`, `docs/ops` |

Порядок внутри частей: миграция и хранилище → логика с модульными тестами → обработчики → подключение и маршруты → интеграционные → e2e. Часть B — первой по важности (бесконечные повторы iOS).

**В плане партии** A, B и C — одна часть одного исполнителя (`098-abc`): все три правят `server.go`, а B и C — ещё и три `session.yaml`; D, E, F, G — отдельные части, F тоже правит `server.go` и потому идёт в другой волне, чем `098-abc`.

**С задачей 106:** C создаёт службы `RPCBotMenu` и `RPCGames` в `app/bff/bots`, 106 заменяет в них отказы на реализацию. Если 106 идёт в той же партии, при слиянии взять реализацию 106, а из 098 — `getEmojiGameInfo` и хэш пустого списка; маршруты в `session.yaml` не дублировать.

## Тесты
Модульные:
- A: `channelOf` — несовпадение `access_hash`, `channelForbidden` (заглушка клиента канала в `core`, как в `notify_exceptions_test.go`).
- B: `NormalizeURL` (схема/без схемы, `//`, порт, `userinfo`, IDN, завершающая точка, верхний регистр, пустой и длинный host, длинный url); `Hash` (стабилен, меняется от флага, от порядка и от url; `0` → `1`); `memstore`: перенос домена между списками, удаление из одного/обоих, предел 100, очистка; обработчики на `memstore`: `NotModified`, состав `Updates`, ответ `#2d0a0571`, `INPUT_REQUEST_INVALID`.
- C: хэш пустого списка, `NotModified` только при ненулевом совпадении; `emojiGameUnavailable`; неслужимые методы — `ErrEnterpriseIsBlocked`.
- D: проверки `reportReadMetrics` (каждая граница, `height > 1000` принимается), `reportMusicListen`, `receivedMessages`, `summarizeText` — ошибка, а не пустой текст.
- E: декодирование `0x61422a48` в `TLMessagesReportReaction` (байты, собранные вручную, `inputUser` и `inputUserSelf`) — в `third_party/teamgram-proto/mtproto` рядом с тестами `// pluma:`; права (админ без `ban_users` в частной — отказ, обычный участник в публичной — да, на себя — нет).
- F: `Candidates` (5 и 7 цифр не берутся, `12-34-56` берётся, цифры внутри длинного числа не берутся, несколько кодов); `Invalidate` на `memstore`-KV: совпадение по слоту приложения и администратора, чужой телефон не трогается, попытки не тратятся; `logaudit` ловит вывод кода в новом коде.
- G: отбор «по одному на отправителя», живость (`period`, бессрочная), `limit`, хэш с `edit_date`; `GetMediaType` для `messageMediaGeoLive`.

Интеграционные (`pkg/testinfra`, настоящие MySQL и Redis, по одному пакету за раз):
- `pkg/webbrowser`: миграция дважды; гонка двух `PutException` одного домена в разные списки — домен в одном списке; предел 100 под гонкой двух вставок (не больше 100).
- `pkg/logincode`: `Invalidate` на Redis — после него `Verify` того же кода даёт `PHONE_CODE_EXPIRED`; ожидающий запрос без совпадения остаётся.
- biz message / msg: сообщение с `inputMediaGeoLive` получает `message_filter_type = 14`; миграция части G переводит старую строку и второй раз ничего не меняет; `searchByMediaType(14)`.
- `pkg/moderation`: жалоба `KindReaction` со снимком читается обратно с полем `reaction`.

Сквозные (`pkg/mtclient`, `test/e2e`, каждая сессия после `getState`):
- `TestNotifySettingsChannel`: мьют супергруппы и вещательного канала (`inputNotifyPeer{inputPeerChannel}`) → `boolTrue`; `getNotifySettings` отдаёт `mute_until`; второе устройство получает `updateNotifySettings` с `seq`. **Проверить, что падает на текущем `master`** (`ERR_ENTERPRISE_IS_BLOCKED`).
- `TestWebBrowserSettings`: `get(0)` — значения по умолчанию и хэш; `get(хэш)` — `NotModified`; `update(open_external)` — второе устройство получает `updateWebBrowserSettings`; `toggle` добавляет исключение (в ответе `updateWebBrowserException`, второе устройство получает то же), повтор в другой список переносит домен, `delete` убирает; `deleteWebBrowserSettingsExceptions` чистит списки и оставляет флаги.
- `TestStartupNoise`: `getAttachMenuBots(0)` — `attachMenuBots` с пустыми списками; `getEmojiGameInfo` — `emojiGameUnavailable`; `reportReadMetrics` по каналу и личному чату и `reportMusicListen` — `boolTrue`; `receivedMessages` — пустой вектор; `summarizeText` — 400; `getEmojiURL` — 400; `getAttachMenuBot` — 400 (не 500).
- `TestReportReaction`: Боб ставит реакцию в публичной супергруппе Алисы; Карл (участник) жалуется на реакцию Боба → `boolTrue`, жалоба есть в `moderation_reports` с `kind = 8` (через `pluma-admin reports` или запрос стенда); жалоба на того, кто не реагировал, — `USER_ID_INVALID`; в частной супергруппе обычный участник — `CHAT_ADMIN_REQUIRED`, админ — `boolTrue`. Отдельный вызов сырыми байтами `0x61422a48` (как шлёт Android) — `boolTrue`.
- `TestInvalidateSignInCodes`: новый клиент делает `auth.sendCode` на телефон Алисы; код приходит в 777000 сессии Алисы (`serviceTexts`); Алиса зовёт `account.invalidateSignInCodes([код])`; `auth.signIn` нового клиента — `PHONE_CODE_EXPIRED`.
- `TestLoginCodeForwardedIsInvalidated`: то же, но Алиса пересылает сообщение 777000 Бобу (и отдельно — вставляет код текстом в чат); `auth.signIn` — `PHONE_CODE_EXPIRED`; сообщение доставлено Бобу.
- `TestRecentLocations`: Алиса и Боб шлют в группу `inputMediaGeoLive` (Алиса дважды), Карл шлёт обычную геопозицию; `getRecentLocations` — два сообщения, по одному на Алису и Боба, новые сверху; с полученным хэшем — `messagesNotModified`; то же в супергруппе.

## Проверки перед сдачей (CachyOS)
- `go build ./...`; `go vet -tags integration,e2e -unreachable=false` затронутых пакетов; `go test` затронутых и импортирующих их, затем `-race`.
- Интеграционные `pkg/webbrowser`, `pkg/logincode`, `pkg/moderation`, biz message — по одному пакету; e2e из раздела «Тесты» со своими `E2E_PROJECT`/`E2E_DIR`, затем `test/e2e/run.sh down`.
- `make lint`, `go test ./tools/...` (log audit, реестр функций), `go run ./tools/apicoverage`.
- На машине разработки ничего не собирать.

## Проверки для `docs/project/TESTING-BACKLOG.md`
- [ ] iOS: после обновления сервера в логах bff нет повторяющихся `account.getWebBrowserSettings` (раньше — раз в несколько секунд с каждого iPhone).
- [ ] Android, iOS, Desktop, Web: выключить и включить уведомления супергруппы и канала из меню чата; на втором устройстве значок «без звука» меняется сам.
- [ ] Android: Настройки → Оформление → Браузер: включить встроенный браузер, ссылка из чата открывается внутри приложения; выключить — снаружи.
- [ ] iOS: Настройки → Данные и память → Браузер: выбрать внешний браузер и вернуть встроенный; добавить сайт в исключения, он виден в списке на втором устройстве (Android) через минуту или после перезапуска; удалить; «Удалить все».
- [ ] Android: долгое нажатие на ссылку → «всегда открывать во внешнем браузере» (если пункт есть) — домен появился в исключениях.
- [ ] iOS и Android: открыть меню вложений (скрепка) — никаких лишних пунктов и ошибок; бросить кубик 🎲 — обычный кубик без ставок.
- [ ] Desktop: в публичной супергруппе открыть список реакций, профиль поставившего реакцию → «Пожаловаться» → «Спасибо за жалобу»; жалоба видна в `pluma-admin reports`.
- [ ] Android: админ удаляет сообщения участника в режиме «только реакции» и отмечает «пожаловаться» — жалоба в `pluma-admin reports`.
- [ ] iOS: запросить код входа с другого устройства, на iPhone открыть чат 777000 и сделать снимок экрана — код больше не принимается на втором устройстве.
- [ ] Любой клиент: переслать сообщение с кодом входа другому человеку — код больше не принимается.
- [ ] Android и iOS: в группе двое делятся живой геопозицией; третий открывает карту чата — видны оба, у каждого одна метка.
- [ ] Android: длинный канал листается без ошибок в логах bff (`reportReadMetrics`); музыка играет без ошибок (`reportMusicListen`).

## Эксплуатация
- Миграции `migrate-20261119-web-browser-settings.sql` и `migrate-20261119-geo-live-filter.sql` применяет служба migrate; повторный запуск безопасен. Вторая обновляет строки с живой геопозицией — их единицы, время — секунды.
- Новых переменных окружения нет: `LOGIN_CODE_HMAC_KEY` и Redis bff уже есть; меняются только `bff.yaml` (секции `Mysql`/`SyncClient` для webbrowser — из общих секций bff, в коде `server.go`) и три `session.yaml` (маршруты `RPCWebBrowser`, `RPCBotMenu`, `RPCGames`).
- Порядок выката: migrate → bff → session (session с маршрутом на ещё старый bff ответит `Unimplemented`, поэтому сначала bff).
- `docs/ops/`: строка про две миграции в журнале выката (`docs/ops/` — там, где перечисляются миграции партии); в `docs/ops/security-setup.md` — абзац «коды входа, отправленные в чат или снятые на экран, аннулируются».
- Диск и память: новые таблицы — десятки строк на пользователя; памяти не прибавляется.

## Решения, требующие подтверждения
1. **Встроенный браузер по умолчанию включён** (`open_external_browser = false`), как у iOS без сервера (`AccountWebBrowserSettings.swift:33-35`). На Android это меняет поведение: ссылки начнут открываться внутри приложения, пока человек не переключит. Альтернатива — по умолчанию внешний (Android останется как сейчас, iOS переключится на внешний). Что делает Telegram для нового аккаунта — не проверено.
2. `display_close_button = true` по умолчанию (Android без настроек ведёт себя так, `MessagesController.java:25361-25362`).
3. `title` исключения — домен; заголовок и значок сайта сервер не загружает.
4. Обновления браузера — без `seq` (восстанавливает часовой опрос).
5. `reportReaction` разрешён администратору с правом банить в любой супергруппе и любому участнику публичной (объединение Desktop и Android).
6. Код входа аннулируется при отправке в **любой** чат, включая «Избранное» (документация говорит «в другой чат»; человек всегда может запросить новый код).
7. `getRecentLocations`: только живые на сейчас геопозиции, по одной на отправителя; вещательный канал — пустой ответ.
8. `inputMessagePinned` в `messages.getMessages` ничего не добавляет (у метода нет чата).
9. `getEmojiURL` остаётся отказом.
10. Метрики и прослушивания хранятся (решение владельца); доступ к чату проверяется с минутным кэшем.

## Не проверено
- Значения настроек браузера у Telegram для нового аккаунта; шлёт ли Telegram `updateWebBrowser*` другим устройствам.
- Имена ошибок Telegram для предела исключений, для неверных метрик и для `reportReaction` без прав (документация перечисляет только `MSG_ID_INVALID`, `PEER_ID_INVALID`, `USER_ID_INVALID`).
- Что Telegram отвечает на `inputMessagePinned` в `messages.getMessages` и что понимает под «recent» в `getRecentLocations` (только живые или и завершённые).
- Что сейчас делает session с запросом Android `0x61422a48` (ответ или обрыв соединения) — проверить в части E.
- Работает ли у нас правка и остановка живой геопозиции (влияет на `edit_date` и хэш части G).
- Порт в домене исключения: Android сравнивает authority с портом (`AndroidUtilities.java:6247`), мы храним host без порта.

## Результат
_(заполняется по окончании)_
