# 105. Telegram Business для всех

- Статус: готово, ждёт выкладки (2026-11-16)
- Решение: ADR 0067 (предложено).
- Зависит от: 061 (отложенные сообщения: образец отдельного ящика и воркера), 070 / ADR 0043 (боты MTProto, `pkg/botstore`), 069 (qts и очередь секретных чатов, `pkg/secretchat`), 071 / ADR 0041 (Premium для всех: строки Business скрыты патчами), 026 / ADR 0036 (ссылки pluma.chat, сервис `links`), ADR 0012 (outbox), ADR 0028 (каталог: `help.getTimezonesList`). Из этой партии: 100 (карты — превью местоположения в клиентах), 099 и 106 (общие файлы, см. «Порядок работ»).
- Ветка: `task/105-business`.

## Цель
Любой пользователь Pluma включает в «Настройки → Telegram Business» часы работы, местоположение, приветствие, автоответ «нет на месте», своё вступление, ссылки на чат, быстрые ответы и подключённого бота — во всех четырёх клиентах так же, как пользователь Telegram с Premium. Автоответы отправляет сервер по правилам Telegram; подключённый бот получает переписку бизнес-чатов и отвечает от имени владельца по выданным правам.

## Область
В задаче:
- Профиль: `account.updateBusinessWorkHours`, `account.updateBusinessLocation`, `account.updateBusinessIntro`; поля `userFull.business_work_hours` (с `open_now`), `business_location`, `business_intro`.
- Автоответы: `account.updateBusinessGreetingMessage`, `account.updateBusinessAwayMessage`; `userFull.business_greeting_message`, `business_away_message` (только себе); отправка автоответов сервером.
- Ссылки на чат: `account.createBusinessChatLink`, `editBusinessChatLink`, `deleteBusinessChatLink`, `getBusinessChatLinks`, `resolveBusinessChatLink`; страница `pluma.chat/m/<slug>` в сервисе `links`.
- Быстрые ответы: `messages.getQuickReplies`, `getQuickReplyMessages`, `checkQuickReplyShortcut`, `editQuickReplyShortcut`, `deleteQuickReplyShortcut`, `reorderQuickReplies`, `sendQuickReplyMessages`, `deleteQuickReplyMessages`; `quick_reply_shortcut` в `messages.sendMessage`, `sendMedia`, `sendMultiMedia`, `forwardMessages`, `sendInlineBotResult`; `quick_reply_shortcut_id` в `messages.editMessage`; обновления `updateQuickReplies`, `updateNewQuickReply`, `updateDeleteQuickReply`, `updateQuickReplyMessage`, `updateDeleteQuickReplyMessages`.
- Подключённый бот: `account.updateConnectedBot`, `getConnectedBots`, `toggleConnectedBotPaused`, `disablePeerConnectedBot`, `getBotBusinessConnection`, `confirmBotConnection`; `peerSettings.business_bot_*`; `invokeWithBusinessConnection` и `messages.uploadMedia.business_connection_id`; обновления боту `updateBotBusinessConnect`, `updateBotNewBusinessMessage`, `updateBotEditBusinessMessage`, `updateBotDeleteBusinessMessage`, `updateBusinessBotCallbackQuery` с qts; `pluma-admin bot set --business`.
- Клиенты: снова показать строку «Telegram Business» (откат части правок 071) и скрыть переключатель рекламы на экранах Business.
- Пределы в `app_config.json`, миграции, тесты, `docs/ops/business.md`, строки `TESTING-BACKLOG`.

Не в задаче (ADR 0067, «Что не делается»): платные сообщения и всё со звёздами и подарками (в том числе действие прав `view_gifts`…`transfer_stars`, `inputInvoiceBusinessBotTransferStars`); истории бота от имени владельца (`manage_stories`); смена фото и имени пользователя ботом; «люди рядом» по местоположению (`contacts.getLocated` остаётся закрыт); поток `updateNewBotConnection`; реклама (`account.toggleSponsoredMessages`); HTTP Bot API (задача 106); свой `business_promo_order`.

## Контракт клиентов
Пути: **A** = `~/pluma-work/reference-src-new/android/TMessagesProj/src/main/java/org/telegram/`, **I** = `~/pluma-work/reference-src-new/ios/submodules/`, **D** = `~/pluma-work/reference-src-new/tdesktop/Telegram/SourceFiles/`, **W** = `~/pluma-work/reference-src-new/tweb/src/`. Схема — D `mtproto/scheme/api.tl` (LAYER 233, совпадает со схемой сервера). Страницы core.telegram.org прочитаны 2026-11-15.

### Схема (D `mtproto/scheme/api.tl`)
- Типы: `businessWeeklyOpen`, `businessWorkHours` (`open_now:flags.0`, `timezone_id`, `weekly_open`), `businessLocation` (`geo_point:flags.0`, `address`), `inputBusinessRecipients`/`businessRecipients`, `businessAwayMessageScheduleAlways|OutsideWorkHours|Custom(start_date,end_date)`, `inputBusinessGreetingMessage`/`businessGreetingMessage` (`shortcut_id`, `recipients`, `no_activity_days`), `inputBusinessAwayMessage`/`businessAwayMessage` (`offline_only`, `shortcut_id`, `schedule`, `recipients`), `quickReply` (`shortcut_id`, `shortcut`, `top_message`, `count`), `inputQuickReplyShortcut`/`inputQuickReplyShortcutId`, `messages.quickReplies`/`quickRepliesNotModified`, `connectedBot`, `account.connectedBots`, `botBusinessConnection`, `inputBusinessIntro`/`businessIntro`, `inputBusinessBotRecipients`/`businessBotRecipients` (`exclude_users`), `inputBusinessChatLink`/`businessChatLink` (`link`, `message`, `entities`, `title`, `views`), `account.businessChatLinks`, `account.resolvedBusinessChatLinks`, `businessBotRights`: `api.tl:1853-1927, 2070`.
- Обновления (имена проверены по схеме, в отчёте исследования они были «не проверено»): `updateQuickReplies#f9470ab2`, `updateNewQuickReply#f53da717`, `updateDeleteQuickReply#53e6f1ec shortcut_id`, `updateQuickReplyMessage#3e050d0f message`, `updateDeleteQuickReplyMessages#566fe7cd shortcut_id messages`, `updateBotBusinessConnect … qts`, `updateBotNewBusinessMessage … reply_to_message:flags.0 qts`, `updateBotEditBusinessMessage`, `updateBotDeleteBusinessMessage connection_id peer messages qts`: `api.tl:450-458`; `updateBusinessBotCallbackQuery` (без qts): `:461`; `updateNewBotConnection`: `:484`.
- Поля: `message.via_business_bot_id:flags2.0`, `message.offline:flags2.1`, `message.quick_reply_shortcut_id:flags.30` (`:126`); `peerSettings.business_bot_paused:flags.11`, `business_bot_can_reply:flags.12`, `business_bot_id`, `business_bot_manage_url:flags.13` (`:260`); `userFull.business_work_hours…business_intro: flags2.0-4` (`:276`); `user.bot_business:flags2.11` (`:91`).
- Методы: `account.updateBusiness*`, `*ConnectedBot*`, `*BusinessChatLink*` (`:2540-2555`), `account.confirmBotConnection` (`:2571`), `invokeWithBusinessConnection` (`:2412`), `quick_reply_shortcut:flags.17` в `sendMessage`/`sendMedia`/`forwardMessages`/`sendMultiMedia`/`sendInlineBotResult` и `quick_reply_shortcut_id:flags.17` в `editMessage` (`:2625-2705`), `messages.uploadMedia business_connection_id:flags.0` (`:2698`), `messages.*QuickReply*` (`:2814-2821`).
- Сервисы gRPC (сгенерированный код `third_party/teamgram-proto/mtproto/schema.tl.sync_service_grpc.pb.go`): `RPCBusinessOpeningHours`, `RPCBusinessLocation`, `RPCBusinessGreeting` (оба автоответа), `RPCBusinessIntro`, `RPCBusinessChatLinks`, `RPCBusinessQuickReply`, `RPCBusinessConnectedBots`; `confirmBotConnection` — в `RPCAccount` (bff `account`).

### Документация (core.telegram.org, 2026-11-15)
- `/api/business`: минута 0 — понедельник 00:00 («61 — понедельник 01:01»); неделя 7×24×60, `end_minute` до 8×24×60 (интервал через ночь с воскресенья на понедельник); интервалы сортируются, неверные (короче минуты, вне диапазона) отбрасываются, пересекающиеся и смежные сливаются по модулю недели, длиннее недели обрезаются; до 28 интервалов; `open_now` от клиента игнорируется, ставит сервер; удаление — вызов без флага; изменения шлют `updateUser`. Адрес до 96 символов UTF-8, пустой вызов удаляет местоположение. Быстрые ответы: создание и добавление — отправка в `inputPeerSelf` с `quick_reply_shortcut` (по имени создаёт, по id дополняет), новый ярлык — `updateNewQuickReply` всем сессиям, сообщения с `quick_reply_shortcut_id` в `updateQuickReplyMessage`, id «из отдельной последовательности»; переименование и порядок — `updateQuickReplies` другим сессиям; удаление ярлыка — только `updateDeleteQuickReply`; хеш `getQuickReplies` — id, хеш MD5 имени, `top_message`, дата правки; хеш `getQuickReplyMessages` — id и дата правки; отправлять быстрые ответы можно «только пользователям, в личные чаты с другими пользователями». Ссылки на чат: просмотр считается при resolve; предел `business_chat_links_limit`, `CHATLINKS_TOO_MUCH`; длина текста — `message_length_max`.
- `/method/account.updateBusinessWorkHours`: `BUSINESS_WORK_HOURS_EMPTY`, `BUSINESS_WORK_HOURS_PERIOD_INVALID`, `TIMEZONE_INVALID`. `/method/account.createBusinessChatLink`: `CHATLINKS_TOO_MUCH`, `DOCUMENT_INVALID`. `/method/account.resolveBusinessChatLink`: `CHATLINK_SLUG_EMPTY`, `CHATLINK_SLUG_EXPIRED`. `/method/account.updateConnectedBot`: `BOT_BUSINESS_MISSING`, `BUSINESS_RECIPIENTS_EMPTY`. `/method/messages.sendQuickReplyMessages`: `PEER_ID_INVALID`; `id` пуст — отправить все; `random_id` по одному на сообщение. `/method/messages.editQuickReplyShortcut`: `SHORTCUT_INVALID`. У `updateBusinessGreetingMessage`, `updateBusinessIntro`, `checkQuickReplyShortcut` ошибок на страницах нет; что значит `boolFalse` у `checkQuickReplyShortcut`, не сказано.
- `/constructor/inputBusinessGreetingMessage`: `no_activity_days` — 7, 14, 21, 28. `/constructor/inputBusinessAwayMessage`: `offline_only` — не отправлять, если аккаунт был в сети последние 10 минут. `/constructor/inputBusinessRecipients`: `existing_chats` «все существующие личные чаты», `new_chats` «все новые», `contacts`, `non_contacts`, `users`, `exclude_selected` «инвертирует выбор».
- `/constructor/message`: `offline` — «отправлено из-за запланированного действия отправителя, например, как автоответ»; `via_business_bot_id` — отправлено бизнес-ботом от имени пользователя.
- `/api/bots/connected-business-bots`: один бот на аккаунт; `user.bot_business` обязателен; Premium не нужен; бот получает `updateBotBusinessConnect` при подключении, отключении и смене настроек (`/constructor/updateBotBusinessConnect`: «с новыми настройками и `connection_id`»); `updateBotNewBusinessMessage` — по настройкам получателей; ответ на `updateBusinessBotCallbackQuery` — без обёртки; `account.getBotBusinessConnection` — чтобы бот получил неизвестное ему подключение; запросы через обёртку идут в `dc_id` подключения; разрешённые методы (из того, что есть в Pluma): `account.updateProfile`, `messages.deleteMessages`, `editMessage`, `readHistory`, `sendMedia`, `sendMessage`, `sendMultiMedia`, `setTyping`, `updatePinnedMessage`; ошибки `BUSINESS_CONNECTION_INVALID`, `BUSINESS_CONNECTION_NOT_ALLOWED`, `BOT_ACCESS_FORBIDDEN`; `toggleConnectedBotPaused` — временно, `disablePeerConnectedBot` — добавляет в `exclude_users` (или убирает из `users` при `exclude_selected`).
- `/constructor/businessBotRights`: `reply` — «отправлять и править сообщения в личных чатах, где были входящие за последние 24 часа»; `read_messages`; `delete_sent_messages` (отправленные ботом); `delete_received_messages`; `edit_name`; `edit_bio`; остальное — фото, имя пользователя, подарки, звёзды, истории.
- `/constructor/peerSettings`: `business_bot_paused`/`can_reply` стоят вместе с `business_bot_id` и `business_bot_manage_url`; `manage_url` — «глубокая ссылка в меню управления бота».
- `/bots/api`, `Message.sender_business_bot`: есть «только у исходящих сообщений, отправленных от имени подключённого бизнес-аккаунта» — отсюда вывод: боту приходят и исходящие сообщения бизнес-чата.
- `/api/links`: `t.me/m/<slug>`, `tg://message?slug=<slug>`; клиент вызывает `account.resolveBusinessChatLink` и подставляет текст в поле ввода.

### Клиенты: профиль
- Часы: удаление — без флага (D `data/business/data_business_info.cpp:83-87`). Местоположение: без точки D шлёт `inputGeoPointEmpty` и не ставит флаг `geo_point`; без адреса не ставит флаг `address` (`:144-160`). Вступление со стикером или без (`:108-128`).
- `business_away_message` и `business_greeting_message` клиент читает только из своего `userFull` (D `data/data_user.cpp:1057-1071`); часы, местоположение, вступление — у любого пользователя.
- Часовые пояса — `help.getTimezonesList` (Android грузит при входе в Business, A `ui/PremiumPreviewFragment.java:1487-1489`); у Pluma список из каталога (`app/bff/catalog/internal/core/help.getTimezonesList_handler.go`), без импорта каталога пуст.
- Пределы вступления: `intro_title_length_limit` и `intro_description_length_limit`, по умолчанию 32 и 70 у D (`settings/business/settings_chat_intro.cpp:569, 574`), 32 и 72 у A (`messenger/MessagesController.java:1773-1774`, ключи `:4383, :4394`).

### Клиенты: быстрые ответы
- Автоответы — обычные ярлыки `hello` и `away` (A `ui/Business/QuickRepliesController.java:31-32`; D `settings/business/settings_shortcut_messages.cpp:319-323`, `settings_away_message.cpp:307`). Удаляя такой ярлык, Android сам выключает автоответ пустым `updateBusinessGreetingMessage`/`updateBusinessAwayMessage` (A `QuickRepliesController.java:467-482`).
- Имя ярлыка: 1–32 символа, буквы, цифры, `_`, U+200C, U+00B7, U+0D80–U+0DFF (D `settings/business/settings_quick_replies.cpp:32, 166-180`; A `ui/Business/QuickRepliesActivity.java:303, 359-361`). Поиск по имени без учёта регистра (D `data/business/data_shortcut_messages.cpp`, `lookupShortcutId`, `compare(…, Qt::CaseInsensitive)`). Занятое имя при переименовании — `SHORTCUT_OCCUPIED` (D `data_shortcut_messages.cpp`, `editShortcut`, проверка до запроса).
- D всегда шлёт ярлык по имени (`ShortcutIdToMTP`, `data_shortcut_messages.cpp:783-790`); iOS — по id, если ярлык уже есть, иначе по имени (I `TelegramCore/Sources/State/PendingMessageManager.swift:1339-1345, 1487-1493, 1881-1887`).
- Ответ на отправку в ярлык iOS сопоставляет по `updateMessageID` и сообщению с `quick_reply_shortcut_id` (I `TelegramCore/Sources/State/ApplyUpdateMessage.swift:304-355`, `UpdateInterpreter+Messages.swift:263-268`); D переносит ярлык с локального id на серверный по `updateNewQuickReply` с тем же именем (`data_shortcut_messages.cpp:283-305`).
- Сообщения ярлыка — в «Избранном» владельца, отдельным пространством id (D `IsShortcutMsgId`, `data_shortcut_messages.cpp:113`; I `Namespaces.Message.QuickReplyCloud`, `QuickReplyMessages.swift:191`): `peer_id` сообщения — сам владелец.
- Хеш `getQuickReplies` одинаков у A и I: по ярлыкам `id`, первые 8 байт MD5 имени, id `top_message`, `edit_date` (0, если не правилось) (A `QuickRepliesController.java:203-215`; I `TelegramCore/Sources/TelegramEngine/Messages/QuickReplyMessages.swift:119-142`). Хеш сообщений у D — id и дата правки в обратном порядке (`data_shortcut_messages.cpp:763-781`).
- `sendQuickReplyMessages`: Android шлёт явные `id` и по `random_id` на каждое (A `QuickRepliesController.java:773-830`), iOS — пустые `id` и `random_id` (I `QuickReplyMessages.swift:393`).
- Пределы: `quick_replies_limit` (D по умолчанию 100, `settings/business/settings_recipients_helper.cpp:397`; A 10, `MessagesController.java:1719`), `quick_reply_messages_limit` (20 у D `:409` и A `messenger/AppGlobalConfig.java:105`). Сервер должен прислать оба явно.

### Клиенты: автоответы и получатели
- Получатели собираются так (D `data/business/data_business_common.cpp:55-112`): в режиме «только выбранные» флаги категорий и `users` — включённые; в режиме «все, кроме» (`exclude_selected`) те же флаги и `users` — исключённые. Для бота в режиме «только выбранные» есть ещё `exclude_users`. Пустой выбор клиент превращает в «все, кроме» (`MakeValid`, `:72-77`).
- Строки клиентов: приветствие — «при первом сообщении или после периода без активности», период — «сколько дней должно пройти после вашего последнего общения с получателем» (A `res/values/strings.xml` `BusinessGreetInfo`, `BusinessGreetPeriodInfo`; D `Resources/langs/lang.strings` `lng_greeting_about`, `lng_greeting_period_about`). «Только не в сети» — «не отправлять, если вы недавно были в сети» (`lng_away_offline_only_about`).

### Клиенты: ссылки на чат
- Ссылку строит сервер (`businessChatLink.link`); Android Pluma узнаёт свою ссылку `https://pluma.chat/m/<slug>` (правки ссылок задачи 026, `clients/patches/android.md:838`), Web K копирует путь на pluma.chat (`clients/patches/web.md:317`).
- Разбор `m/<slug>` есть во всех клиентах: A `ui/LaunchActivity.java:2204` → `resolveBusinessChatLink` (`:5562-5566`); I `UrlHandling/Sources/UrlHandling.swift:680`; D `core/local_url_handlers.cpp:2213` (`tg://message?slug=`); W `lib/internalLinkProcessor.ts:718, 735`. Хост pluma.chat клиенты Pluma считают своим (ADR 0036, п. 6).
- Пределы: `business_chat_links_limit` (A по умолчанию 100, `MessagesController.java:1775`; D читает `settings/business/settings_chat_links.cpp:734`).

### Клиенты: подключённый бот
- Список — `account.getConnectedBots`, берётся первый (D `data/business/data_business_chatbots.cpp:28-50`); его же читают iOS при загрузке своего `userFull` (I `TelegramCore/Sources/TelegramEngine/Peers/UpdateCachedPeerData.swift:236`) и Web K при каждом открытии настроек (W `components/sidebarLeft/tabs/settings.tsx:301, 328`).
- Отключение: D шлёт `deleted` с прежним ботом (`data_business_chatbots.cpp:86-90`), iOS — `deleted` с `inputUserEmpty` и пустыми получателями (I `QuickReplyMessages.swift:1214, 1265-1267`). Ответ `Updates` клиенты применяют (D `:92`, I `:1272-1279`).
- Бот без `bot_business` клиенты не дают выбрать (D `settings/business/settings_chatbots.cpp:342`, A `ui/Business/ChatbotsActivity.java:565`), ошибку `BOT_BUSINESS_MISSING` разбирает D (`:875`).
- Пауза и отключение в чате — панель над чатом по `peerSettings.business_bot_*`: D меняет флаги сам после ответа или перечитывает `getPeerSettings` (`data_business_chatbots.cpp:119-180`), A `ui/Business/BusinessBotButton.java:116, 133`; «Управлять» открывает `business_bot_manage_url` как ссылку (D `history/view/history_view_contact_status.cpp:1305-1312`, A `BusinessBotButton.java:145-147`).
- Права в интерфейсе — сообщения, профиль, подарки, истории (D `data_business_chatbots.cpp:185-215`; биты I `QuickReplyMessages.swift:1224-1265`).
- `confirmBotConnection` клиенты зовут из экрана «новое подключение» по `updateNewBotConnection` (A `messenger/UnconfirmedAuthController.java:355-362`, `MessagesController.java:20338`; I `TelegramEngine/AccountData/BotConnectionReviews.swift:7-30`).
- iOS показывает «Автоматизация чатов» в редактировании профиля (I `TelegramUI/Components/PeerInfo/PeerInfoScreen/Sources/PeerInfoSettingsItems.swift:530-538`) — без патчей.

### Клиенты: строка Business и реклама
- Android: строка в настройках и профиле при `!premiumFeaturesBlocked()` (A `ui/SettingsActivity.java:830-832`, `ui/ProfileActivity.java:10918-10920`); Pluma заменила условие на `premiumPurchaseBlocked()` (A-109, `clients/patches/android.md:1100`) и закрыла ссылку `settings/business` (A-112, `:1268`). Экран при Premium открывает редакторы (A `ui/PremiumPreviewFragment.java:867-880`) и показывает «Не скрывать рекламу» (`:1454-1458`, переключатель шлёт `toggleSponsoredMessages`, `:843-860`).
- iOS: строка при `!isPremiumDisabled || context.isPremium` (I `PeerInfoSettingsItems.swift:324-328`), Pluma убрала `|| context.isPremium` (I-109, `clients/patches/ios.md:1043`); экран — `PremiumIntroScreen(mode: .business)` (I `TelegramUI/Sources/SharedAccountContext.swift:3177-3179`), реклама — `PremiumUI/Sources/PremiumIntroScreen.swift:2729-2767`, поиск по настройкам — `SettingsUI/Sources/Search/SettingsSearchableItems.swift:772`.
- Desktop: кнопка Business внутри `BuildPremiumSection` (D `settings/sections/settings_main.cpp:501-575`), которую Pluma показывает только при `premiumCanBuy` (D-084, `clients/patches/desktop.md:992-998`); реклама — `BuildSponsoredSection` (D `settings/sections/settings_business.cpp:298-360`, вызов `:523`, видна при Premium `:401-403`).
- Web K: строка внутри `<Show when={!premiumBlocked()}>` вместе с Premium и звёздами (W `components/sidebarLeft/tabs/settings.tsx:474-483`), поиск прячет `TelegramBusiness` (W-070, `clients/patches/web.md:264`); при Premium экран открывает редакторы часов, местоположения, ссылок, вступления и бота (W `components/popups/premium.tsx:108-114`), реклама — `components/premium/promoSlide.tsx:111`. Быстрых ответов, приветствия и «нет на месте» в Web K нет (вкладок нет в `components/sidebarLeft/tabs/`).

## Дизайн

### Пределы (`teamgramd/bin/app_config.json`)
| ключ | значение | почему |
|---|---|---|
| `quick_replies_limit` | 100 | умолчание tdesktop; включает `hello` и `away` |
| `quick_reply_messages_limit` | 20 | умолчание A и D |
| `business_chat_links_limit` | 100 | умолчание A |
| `intro_title_length_limit` | 32 | A и D |
| `intro_description_length_limit` | 70 | меньшее из умолчаний A и D |

Сервер проверяет те же числа (константы в `pkg/business`, тест держит их равными `app_config.json`). Адрес — 96 символов, интервалов часов — 28, текст ссылки — `message_length_max` (4096), заголовок ссылки — 32 символа (не проверено, у Telegram предел не опубликован; решение к подтверждению).

### Таблицы (миграции идемпотентны, `CREATE TABLE IF NOT EXISTS`)
`migrate-20261119-business.sql` (часть 1, все таблицы `pkg/business`):
```sql
CREATE TABLE IF NOT EXISTS `business_profiles` (
  `user_id` bigint(20) NOT NULL,
  `hours_timezone` varchar(64) NOT NULL DEFAULT '',     -- '' — часов нет
  `hours_intervals` varchar(1024) NOT NULL DEFAULT '',  -- JSON [[start,end],...], нормализовано
  `location_address` varchar(512) NOT NULL DEFAULT '',  -- до 96 символов
  `location_has_geo` tinyint(1) NOT NULL DEFAULT '0',
  `location_lat` double NOT NULL DEFAULT '0',
  `location_long` double NOT NULL DEFAULT '0',
  `location_set` tinyint(1) NOT NULL DEFAULT '0',
  `intro_set` tinyint(1) NOT NULL DEFAULT '0',
  `intro_title` varchar(256) NOT NULL DEFAULT '',
  `intro_description` varchar(1024) NOT NULL DEFAULT '',
  `intro_sticker_id` bigint(20) NOT NULL DEFAULT '0',
  `updated_at` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`)
);
CREATE TABLE IF NOT EXISTS `business_auto_reply_settings` (
  `user_id` bigint(20) NOT NULL,
  `kind` tinyint(4) NOT NULL,                 -- 1 приветствие, 2 «нет на месте»
  `shortcut_id` int(11) NOT NULL,
  `recipients` varchar(4096) NOT NULL,        -- JSON: флаги, users
  `no_activity_days` int(11) NOT NULL DEFAULT '0',
  `schedule` tinyint(4) NOT NULL DEFAULT '0', -- 1 всегда, 2 вне часов, 3 своё
  `schedule_start` int(11) NOT NULL DEFAULT '0',
  `schedule_end` int(11) NOT NULL DEFAULT '0',
  `offline_only` tinyint(1) NOT NULL DEFAULT '0',
  `enabled_at` int(11) NOT NULL,              -- граница «существующих» и «новых» чатов
  PRIMARY KEY (`user_id`, `kind`)
);
CREATE TABLE IF NOT EXISTS `business_auto_replies` (   -- очередь отправки
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `user_id` bigint(20) NOT NULL,              -- владелец, отправитель
  `peer_id` bigint(20) NOT NULL,              -- собеседник
  `kind` tinyint(4) NOT NULL,
  `trigger_box_id` int(11) NOT NULL,          -- id входящего в ящике владельца
  `shortcut_id` int(11) NOT NULL,
  `claim_until` bigint(20) NOT NULL DEFAULT '0',
  `attempts` int(11) NOT NULL DEFAULT '0',
  `created_at` bigint(20) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `trigger` (`user_id`, `peer_id`, `kind`, `trigger_box_id`),
  KEY `due` (`claim_until`, `id`)
);
CREATE TABLE IF NOT EXISTS `business_auto_reply_log` (  -- когда последний раз ушёл автоответ
  `user_id` bigint(20) NOT NULL, `peer_id` bigint(20) NOT NULL, `kind` tinyint(4) NOT NULL,
  `sent_at` int(11) NOT NULL,
  PRIMARY KEY (`user_id`, `peer_id`, `kind`)
);
CREATE TABLE IF NOT EXISTS `business_chat_links` (
  `slug` varchar(32) COLLATE utf8mb4_bin NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `message` text NOT NULL,
  `entities` mediumtext NOT NULL,             -- JSON сущностей, '' — нет
  `title` varchar(256) NOT NULL DEFAULT '',
  `views` int(11) NOT NULL DEFAULT '0',
  `created_at` bigint(20) NOT NULL,
  PRIMARY KEY (`slug`), KEY `user` (`user_id`, `created_at`)
);
CREATE TABLE IF NOT EXISTS `business_bot_connections` (
  `user_id` bigint(20) NOT NULL,              -- один бот на пользователя
  `bot_id` bigint(20) NOT NULL,
  `connection_id` varchar(64) COLLATE utf8mb4_bin NOT NULL,
  `rights` int(11) NOT NULL,                  -- биты businessBotRights как пришли
  `recipients` varchar(8192) NOT NULL,        -- JSON: флаги, users, exclude_users
  `created_at` int(11) NOT NULL,
  `updated_at` int(11) NOT NULL,
  PRIMARY KEY (`user_id`), UNIQUE KEY `connection` (`connection_id`), KEY `bot` (`bot_id`)
);
CREATE TABLE IF NOT EXISTS `business_bot_paused_peers` (
  `user_id` bigint(20) NOT NULL, `peer_id` bigint(20) NOT NULL, `paused_at` int(11) NOT NULL,
  PRIMARY KEY (`user_id`, `peer_id`)
);
```
`migrate-20261119-quick-replies.sql` (часть 3, сервис msg), по образцу `migrate-20261108-scheduled-messages.sql`:
```sql
CREATE TABLE IF NOT EXISTS `quick_reply_shortcuts` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `user_id` bigint(20) NOT NULL,
  `shortcut` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,  -- сравнение без регистра
  `sort` int(11) NOT NULL,
  `deleted` tinyint(1) NOT NULL DEFAULT '0',
  `created_at` bigint(20) NOT NULL,
  PRIMARY KEY (`id`), UNIQUE KEY `user_shortcut` (`user_id`, `shortcut`, `deleted`) -- см. ниже
);
CREATE TABLE IF NOT EXISTS `quick_reply_messages` (
  `id` int(11) NOT NULL AUTO_INCREMENT,       -- id, который видит клиент
  `user_id` bigint(20) NOT NULL,
  `shortcut_id` int(11) NOT NULL,
  `random_id` bigint(20) NOT NULL DEFAULT '0',
  `out_random_id` bigint(20) GENERATED ALWAYS AS (IF(`random_id` <> 0, `random_id`, NULL)) VIRTUAL,
  `grouped_id` bigint(20) NOT NULL DEFAULT '0',
  `message_data` mediumtext COLLATE utf8mb4_bin NOT NULL,
  `edit_date` int(11) NOT NULL DEFAULT '0',
  `created_at` bigint(20) NOT NULL,
  PRIMARY KEY (`id`), UNIQUE KEY `user_out_random` (`user_id`, `out_random_id`),
  KEY `shortcut` (`shortcut_id`, `id`)
);
```
Удалённый ярлык удаляется строкой целиком вместе с сообщениями в одной транзакции (флаг `deleted` не нужен, если удаление синхронное; исполнитель выбирает одно из двух и держит уникальность имени).

`migrate-20261119-qts-updates.sql` (часть 7):
```sql
CREATE TABLE IF NOT EXISTS `qts_updates` (
  `auth_key_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `qts` int(11) NOT NULL,
  `update_data` mediumblob NOT NULL,          -- TL-обновление без users/chats
  `user_ids` varchar(1024) NOT NULL DEFAULT '', -- кого положить в users при выдаче
  `date` int(11) NOT NULL,
  PRIMARY KEY (`auth_key_id`, `user_id`, `qts`)
);
```

### Сервисы и файлы
- **`pkg/business`** (новый): `Store` над MySQL — профиль, автоответы (настройки, очередь, журнал), ссылки, подключение и пауза; нормализация часов (`hours.go`: проверка, сортировка, слияние по модулю недели, перенос хвоста за воскресенье в начало — как D `data_business_common.cpp:20-50`, `open_now(now, tz)`), сопоставление получателей (`recipients.go`), правила автоответов (`autoreply.go`), slug (`slug.go`), права и правило 24 часов (`rights.go`); константы пределов. Часовые пояса — `time.LoadLocation`, в бинарники bff и msg импортируется `time/tzdata`.
- **`app/bff/business`** (новый модуль bff по образцу `app/bff/scheduledmessages`): серверы gRPC всех семи сервисов Business; регистрация в `app/bff/bff/internal/server/server.go`; маршруты `"/mtproto.RPCBusinessOpeningHours"`, `RPCBusinessLocation`, `RPCBusinessGreeting`, `RPCBusinessIntro`, `RPCBusinessChatLinks`, `RPCBusinessQuickReply`, `RPCBusinessConnectedBots` → `bff.bff` в `teamgramd/etc/session.yaml`, `teamgramd/etc2/session.yaml`, `app/interface/session/etc/session.yaml`. До готовности своей части метод отвечает `mtproto.ErrEnterpriseIsBlocked` (CLAUDE.md: 400, не Unimplemented). Обработчики — по файлу на метод (`account.updateBusinessWorkHours_handler.go` и т. д.).
- **`app/bff/users`**: `users.getFullUser_handler.go` — поля `business_*` из `pkg/business` (часы с `open_now` на момент ответа; автоответы — только при `peerId == c.MD.UserId`), стикер вступления — `MediaClient` (как `PersonalPhoto` рядом). Боты полей не имеют.
- **`app/bff/dialogs`** `messages.getPeerSettings_handler.go` и **`app/service/biz/user`** `UserGetPeerSettings` (где собирается `peerSettings` для `userFull.settings`): `business_bot_id`, `business_bot_manage_url`, `business_bot_paused`, `business_bot_can_reply` для чата владельца с собеседником, которого обслуживает бот. Сборку держать в одной функции `pkg/business` (`PeerBar(ctx, userId, peerId)`), вызываемой из обоих мест, или собирать только в bff (поле `Settings` в `getFullUser` заполняется из `UserGetPeerSettings` — дополнить там же в bff, не трогая сервис biz, если так проще).
- **`app/messenger/msg`**: быстрые ответы — `internal/dao/quick_replies.go` (по образцу `scheduled.go`), ядро `msg/internal/core/quick_replies.go`, RPC в `msg.tl`: `msg.addQuickReplyMessages`, `msg.getQuickReplies`, `msg.getQuickReplyMessages`, `msg.editQuickReplyShortcut`, `msg.deleteQuickReplyShortcut`, `msg.reorderQuickReplies`, `msg.checkQuickReplyShortcut`, `msg.editQuickReplyMessage`, `msg.deleteQuickReplyMessages`, `msg.sendQuickReplyMessages`; автоответы — решение в `msg/inbox/internal/core/inbox.sendUserMessageToInboxV2_handler.go`, воркер `msg/msg/internal/core/business_autoreply_worker.go` (по образцу `scheduled_worker.go`); события боту — в отправке (`msg.sendMessageV2_handler.go`, `sendUserMessages`), правке (`msg.editMessageV2_handler.go`), удалении (`msg.deleteMessages_handler.go`) и в inbox (входящее, правка, удаление: `inbox.editMessageToInboxV2_handler.go`, `inbox.deleteMessagesToInbox_handler.go`); новый обработчик `inbox.botBusinessUpdateToInbox` (в `inbox.tl`).
- **`pkg/qtsqueue`** (новый, часть 7): счётчик qts авторизации (перенос `nextQts`, `State`, подтверждения из `pkg/secretchat/store.go:512-700`), очередь `qts_updates`; `pkg/secretchat` пользуется им; `app/bff/updates` `updates.getDifference_handler.go` и `updates.getState_handler.go` сливают обе очереди по qts.
- **Сессия** (часть 8): `session_invoke.go` `onInvokeWithBusinessConnection` кладёт `connection_id` в метаданные (поле `business_connection_id` в `third_party/teamgram-proto/mtproto/rpc/metadata`, отдельный коммит `third_party(proto):` с `// pluma:`), как `takeout` (`main_auth_wrapper.go:755`).
- **`app/bff/messages`**, **`app/bff/files`** (`messages.uploadMedia`), **`app/bff/account`** (`account.updateProfile`, `account.confirmBotConnection_handler.go`), **`app/bff/bots`** (`messages.sendInlineBotResult` с ярлыком; `messages.getBotCallbackAnswer` для сообщений, отправленных ботом через подключение): ветки ярлыка и подключения.
- **`app/interface/links`**: тип `KindBusinessChat` в `route.go` для `m/<slug>` (`App: "message?slug=" + slug`), страница в `page.go` с именем и публичным фото владельца по правилам ADR 0036 п. 4, тексты ru/en в `texts.go`; slug в лог не пишется. Данные — `pkg/business` (только чтение `slug → user_id`), без счёта просмотров.
- **`tools/pluma-admin/bot.go`**: `bot set <bot> --business on|off` (`TLUserUpdateBotData.BotBusiness`, если поля нет — добавить в `user.tl` и в обработчик); при выключении все подключения этого бота удаляются с `updateBotBusinessConnect{disabled}` боту.
- **Ошибки** — в `third_party/teamgram-proto/mtproto/rpc_error_codes.go` (коммит `third_party(proto):`, `// pluma:`): `BUSINESS_WORK_HOURS_EMPTY`, `BUSINESS_WORK_HOURS_PERIOD_INVALID`, `TIMEZONE_INVALID`, `ADDRESS_INVALID` (не проверено, имя выбрано по смыслу; длина адреса), `CHATLINKS_TOO_MUCH`, `CHATLINK_SLUG_EMPTY`, `CHATLINK_SLUG_EXPIRED`, `QUICK_REPLIES_TOO_MUCH`, `REPLY_MESSAGES_TOO_MUCH`, `SHORTCUT_INVALID`, `SHORTCUT_OCCUPIED`, `BOT_BUSINESS_MISSING`, `BUSINESS_RECIPIENTS_EMPTY`, `BUSINESS_CONNECTION_INVALID`, `BUSINESS_CONNECTION_NOT_ALLOWED`, `BOT_ACCESS_FORBIDDEN`.

### Профиль
- `updateBusinessWorkHours`: без флага — удалить; иначе `timezone_id` должен быть в списке каталога (если каталог пуст — любой, который грузит `time.LoadLocation`), иначе `TIMEZONE_INVALID`; пустой `weekly_open` — `BUSINESS_WORK_HOURS_EMPTY`; после нормализации пусто или больше 28 — `BUSINESS_WORK_HOURS_PERIOD_INVALID`. `open_now` клиента игнорируется.
- `open_now`: локальная минута недели `m` в поясе владельца (понедельник 00:00 = 0); открыто, если какой-то интервал содержит `m` или `m + 10080`.
- `updateBusinessLocation`: без адреса и без точки — удалить; `inputGeoPointEmpty` без флага — точки нет; адрес длиннее 96 символов — `ADDRESS_INVALID`. Широта и долгота проверяются на диапазон (`GEO_POINT_INVALID`, уже есть в `rpc_error_codes.go`).
- `updateBusinessIntro`: без `intro` — удалить; длины по пределам `intro_title_length_limit` и `intro_description_length_limit`, превышение — `TITLE_INVALID` и `DESCRIPTION_INVALID` (не проверено: Telegram имён не публикует; решение к подтверждению); стикер — документ должен существовать и быть стикером, иначе `DOCUMENT_INVALID`; `inputDocumentEmpty` — без стикера.
- Каждое изменение — `updateUser{user_id: self}` в журнал seq владельца для всех его устройств, кроме вызвавшего (`AddSeqUpdateExcept`), как у правки профиля. Другим пользователям ничего не шлётся: они перечитывают `userFull` при открытии профиля (так же, как «о себе»).

### Автоответы
Настройки:
- `updateBusinessGreetingMessage`/`updateBusinessAwayMessage` без `message` — удалить настройку (и неотправленные строки очереди этого вида). Иначе: `shortcut_id` должен быть ярлыком владельца с хотя бы одним сообщением, иначе `SHORTCUT_INVALID`; `no_activity_days` ∈ {7, 14, 21, 28}, иначе `400 NO_ACTIVITY_DAYS_INVALID` (не проверено, имя по смыслу; решение к подтверждению); получатели без единого флага и пользователя в режиме «только выбранные» — `BUSINESS_RECIPIENTS_EMPTY` (имя из `/method/account.updateConnectedBot`, для автоответов не документировано); пользователи получателей — существующие люди (не боты, не сам владелец), не больше 100 (не проверено); `businessAwayMessageScheduleCustom` — `start_date < end_date`. `enabled_at` ставится при первом включении вида и не меняется при правке (иначе каждая правка превращала бы все чаты в «существующие»).
- Ответ `boolTrue`; владельцу на другие устройства `updateUser{self}` (как у профиля: клиенты берут настройки из своего `userFull`).
- Удаление ярлыка, на который ссылается настройка, выключает настройку в той же транзакции (делает то же, что Android сам, A `QuickRepliesController.java:467-482`; tdesktop и iOS этого не делают — сервер не должен оставлять висячую ссылку).

Когда срабатывает (в транзакции ящика владельца B, применяющей входящее личное сообщение от P):
- Только `PEER_USER`, P — человек, не бот, не служебный аккаунт (777000, призрак поддержки — те же проверки, что у автоархива), P ≠ B; не служебное сообщение; у B есть настройка вида.
- **Получатели** (`pkg/business.Recipients.Match`): категории P — «новый» (первое сообщение диалога после `enabled_at`), иначе «существующий»; «контакт» (P в контактах B), иначе «не контакт». Без `exclude_selected`: подходит, если P в `users` или хотя бы одна его категория отмечена. С `exclude_selected`: подходит, если P нет в `users` и ни одна его категория не отмечена.
- **Приветствие**: диалог B–P до этого сообщения пуст, или последнее сообщение диалога (любого направления, в ящике B) старше `no_activity_days` суток.
- **«Нет на месте»**: расписание действует сейчас — «всегда»; «вне часов» — у B заданы часы и `open_now` ложно (часов нет — не отправлять; не проверено); «своё» — `start_date ≤ now < end_date`. При `offline_only` — B не в сети и последний раз был в сети больше 10 минут назад (тот же источник, что `userStatus`, ADR 0030). Частота: не чаще одного раза на собеседника, пока B сам не написал P после прошлого автоответа, и не чаще раза в 24 часа (`business_auto_reply_log`; не проверено, решение к подтверждению).
- Сработавший вид — строка в `business_auto_replies` (уникальный ключ по `trigger_box_id`: повтор события строку не удваивает). Если сработали оба, сначала приветствие, затем «нет на месте» (не проверено).

Отправка (воркер в сервисе msg, каждую секунду, по образцу `scheduled_worker.go`):
- Строка берётся условным `UPDATE` с арендой 2 минуты; сообщения ярлыка отправляются от B к P тем же путём, что `sendQuickReplyMessages` (все проверки личного чата: блокировка, приватность «кто может писать»), с `message.offline = true`; `random_id` — детерминированно из id строки и порядкового номера сообщения; строка удаляется и `business_auto_reply_log` пишется в транзакции ящика B, которая отправляет. Альбом уходит одним пакетом.
- Постоянная ошибка (ярлык удалён, P заблокировал B, приватность) — строка удаляется без отправки, счётчик метрики. Временная — `attempts + 1`, повтор после аренды; после 10 попыток строка удаляется с записью в лог (только id и код).
- После перезапуска воркер продолжает с неотправленных строк; два экземпляра не отправят одно и то же (аренда + `random_id`).

### Ссылки на чат
- `createBusinessChatLink`: предел 100 (`CHATLINKS_TOO_MUCH`), текст до 4096 (`MESSAGE_TOO_LONG`), сущности проверяются тем же кодом, что у отправки сообщения; slug — 16 символов `[A-Za-z0-9]`, повтор при совпадении. Ответ — `businessChatLink{link: "https://" + env2.TDotMe + "/m/" + slug, views: 0}`.
- `editBusinessChatLink`/`deleteBusinessChatLink`: только свой slug, иначе `CHATLINK_SLUG_EXPIRED`. `getBusinessChatLinks` — по дате создания, с `users`/`chats` для упоминаний в сущностях.
- `resolveBusinessChatLink`: пустой — `CHATLINK_SLUG_EMPTY`, неизвестный или удалённый, или владелец удалён — `CHATLINK_SLUG_EXPIRED`. Ответ — `peer = peerUser(владелец)`, `message`, `entities`, `users` (владелец и упомянутые). `views + 1` одним `UPDATE`, если вызывающий — не владелец. Боты и Premium не проверяются.
- Обновлений на другие устройства нет (документация их не называет; клиенты перечитывают список при входе на экран — не проверено у iOS).

### Быстрые ответы
- **Добавление** (`sendMessage`/`sendMedia`/`sendMultiMedia`/`forwardMessages`/`sendInlineBotResult` с `quick_reply_shortcut`): `peer` должен быть `inputPeerSelf` (иначе `PEER_ID_INVALID`); `schedule_date` вместе с ярлыком — `SCHEDULE_DATE_INVALID` (не проверено). Ярлык по имени: имя проверяется по правилам клиентов (иначе `SHORTCUT_INVALID`), нет — создаётся (предел 100, `QUICK_REPLIES_TOO_MUCH`), в конец порядка; по id — должен существовать (`SHORTCUT_INVALID`). Сообщений в ярлыке не больше 20 (`REPLY_MESSAGES_TOO_MUCH`). Медиа загружается и проверяется обычным кодом отправки; сообщение хранится с `peer_id = self`, `out`, `quick_reply_shortcut_id`, без `from_id`-особенностей «Избранного» (`saved_peer_id` не ставится). Повтор `random_id` отвечает уже сохранённым сообщением.
- **Ответ** на добавление: `updates{updateMessageID(random_id → id), updateQuickReplyMessage(message), [updateNewQuickReply(quickReply) — если ярлык создан]}`; в журнал seq — `updateNewQuickReply` и `updateQuickReplyMessage` для остальных устройств (новые типы в `userbox.SeqUpdate`, `pkg/userbox/box.go:281`).
- `getQuickReplies(hash)`: ярлыки по `sort`, у каждого `top_message` — id последнего сообщения, `count`; `messages` — верхние сообщения; хеш как у A/I (`pkg/hashx`, MD5 имени — первые 8 байт как беззнаковое число); совпал — `messages.quickRepliesNotModified`.
- `getQuickReplyMessages(shortcut_id, id?, hash)`: сообщения ярлыка по возрастанию id (или выбранные); хеш — id и `edit_date` в порядке D; совпал — `messages.messagesNotModified{count}`. Чужой ярлык — `SHORTCUT_INVALID`.
- `checkQuickReplyShortcut(shortcut)`: `boolTrue`, если в ярлык с этим именем можно добавить сообщение (ярлыка нет и предел ярлыков не достигнут, или он есть и в нём меньше 20 сообщений), иначе `boolFalse` (смысл результата не документирован — вывод из описания метода).
- `editQuickReplyShortcut`: имя по правилам (`SHORTCUT_INVALID`), занято другим непустым ярлыком — `SHORTCUT_OCCUPIED`; `updateQuickReplies` (весь список) остальным устройствам. `reorderQuickReplies(order)`: неизвестные id пропускаются, отсутствующие в `order` идут в конец; `updateQuickReplies`. `deleteQuickReplyShortcut`: удалить ярлык и сообщения, выключить ссылающиеся автоответы; остальным устройствам только `updateDeleteQuickReply`.
- `editMessage` с `quick_reply_shortcut_id`: правка сообщения ярлыка (текст, сущности, медиа, `invert_media`), `edit_date`; ответ и журнал — `updateQuickReplyMessage`. Чужое — `MESSAGE_ID_INVALID`.
- `deleteQuickReplyMessages(shortcut_id, id)`: ответ и журнал — `updateDeleteQuickReplyMessages`; если ярлык опустел, он остаётся (клиенты сами удаляют пустые, D `editShortcut`).
- `sendQuickReplyMessages(peer, shortcut_id, id, random_id)`: `peer` — пользователь, не бот, не сам владелец (иначе `PEER_ID_INVALID`; бот — не проверено); пустой `id` — все сообщения ярлыка; `random_id` пуст — сервер генерирует; длины не совпадают — `RANDOM_ID_INVALID`. Сообщения уходят одним вызовом обычной отправки из ящика владельца в порядке id, альбомы с новым `grouped_id`, без заголовка пересылки и без `quick_reply_shortcut_id`; ответ — как у `sendMultiMedia`/`forwardMessages`.

### Подключённый бот
Настройки (часть 6):
- `updateConnectedBot`: без `deleted` — бот должен быть ботом с `bot_business` (`BOT_BUSINESS_MISSING`), получатели не пусты (ни одного флага и ни одного пользователя в режиме «только выбранные» — `BUSINESS_RECIPIENTS_EMPTY`). Другой бот уже подключён — заменяется (одно подключение; старому боту `updateBotBusinessConnect{disabled}`). Новое подключение — новый `connection_id` (24 байта из `crypto/rand`, base64url); правка — прежний. С `deleted` — удалить подключение независимо от переданного бота (iOS шлёт `inputUserEmpty`), очистить паузы. Ответ — `updates{updates: [], users: [бот]}` (что Telegram кладёт в `updates`, не проверено). Боту — `updateBotBusinessConnect` (часть 7).
- `getConnectedBots`: `connectedBot{bot_id, recipients, rights}` без `device`/`date`/`location` (они есть у подключений, ожидающих подтверждения; таких у Pluma нет), `users: [бот]`.
- `toggleConnectedBotPaused(peer, paused)`: строка паузы; `updatePeerSettings` владельцу на все устройства, кроме вызвавшего. Чат не обслуживается ботом — `PEER_ID_INVALID` (не проверено).
- `disablePeerConnectedBot(peer)`: P в `exclude_users` (в режиме «только выбранные» убирается из `users`, а если он подходил категорией — добавляется в `exclude_users`; в режиме «все, кроме» добавляется в `users`), пауза снимается; `updatePeerSettings` владельцу; боту `updateBotBusinessConnect` с прежним `connection_id`.
- `confirmBotConnection(bot)`: подключён этот бот — `boolTrue`; нет — `BOT_INVALID`.
- `peerSettings` чата B–P, если у B есть подключение и P подходит под получателей: `business_bot_id`, `business_bot_manage_url = "https://" + env2.TDotMe + "/" + <username бота>` (Telegram, по-видимому, добавляет параметр start — не проверено; решение к подтверждению), `business_bot_paused` (есть строка паузы), `business_bot_can_reply` (есть право `reply` и нет паузы).

Обновления боту (часть 7):
- Событие `inbox.botBusinessUpdateToInbox{bot_id, connection_id, kind, update}` выпускается в транзакции ящика B, если подключение есть, P подходит и не на паузе: входящее от P (inbox), исходящее B к P (отправка, в том числе автоответы и сообщения, отправленные самим ботом), правка, удаление (с `peer = P` и id ящика B). `message` — в представлении B (как его видит владелец), `reply_to_message` — сообщение из ящика B, на которое ответили, если оно есть.
- Транзакция ящика бота применяет событие: для каждой живой авторизации бота (`auth_users`, `deleted = 0`) — следующий qts из общего счётчика и строка `qts_updates`; пуш `updates{updates: [update с qts], users, chats}` на эту авторизацию. Бот без авторизаций — событие применено, ничего не пишется (как у ботов в группах вне сети).
- `updateBotBusinessConnect` — при подключении, правке, отключении (`disabled`), выключении бизнес-режима бота; `botBusinessConnection{connection_id, user_id: B, dc_id: this_dc, date, rights}`.
- `updates.getDifference` бота: строки `qts_updates` с qts больше переданного, вместе с очередью секретных чатов, по возрастанию, в `other_updates`, со `state.qts`; переданный qts подтверждает (удаляет) строки до него. `updates.getState` — тот же счётчик (уже так, `updates.getState_handler.go:38-40`).
- Хранение: строка живёт до подтверждения; старше 7 суток удаляется при следующей записи этой авторизации (как `TrimQueryUpdates` у ботов, ADR 0043), чтобы забытая авторизация не копила очередь (решение к подтверждению).
- `account.getBotBusinessConnection(connection_id)` (вызывает бот): своё подключение — `updates{updateBotBusinessConnect(qts = текущий, без записи в очередь)}`; чужое или нет — `BUSINESS_CONNECTION_INVALID`.

Действия бота (часть 8):
- Сессия: `invokeWithBusinessConnection` → `md.BusinessConnectionId`. bff: одна функция `business.Act(ctx, md, method, peer)` перед обработчиком. Проверки: вызывающий — бот (иначе `BUSINESS_CONNECTION_NOT_ALLOWED`), подключение существует и принадлежит ему (`BUSINESS_CONNECTION_INVALID`), бизнес-режим бота включён, метод в списке (иначе `BUSINESS_CONNECTION_NOT_ALLOWED`), нужное право есть и P подходит, не на паузе и не отключён (`BOT_ACCESS_FORBIDDEN`).
- Права по методам: `sendMessage`/`sendMedia`/`sendMultiMedia`/`setTyping`/`updatePinnedMessage` — `reply` и входящее от P в ящике B за последние 24 часа; `editMessage` — `reply`, только сообщения, отправленные этим ботом (по `via_business_bot_id` копии B), правило 24 часов; `readHistory` — `read_messages`; `deleteMessages` — `delete_sent_messages` для своих, `delete_received_messages` для входящих, `revoke` как у владельца; `account.updateProfile` — `edit_name` (имя) и `edit_bio` («о себе»). `messages.uploadMedia` с `business_connection_id` — те же проверки, право `reply`.
- После проверок `md.UserId = B`, `md.PermAuthKeyId = 0` (пуш получат все устройства владельца), `md.ViaBusinessBotId = бот`. На копии B ставится `via_business_bot_id`; у копии P его нет (не проверено). `reply_markup` бота (инлайн-клавиатура) сохраняется — у сообщений через подключение он разрешён.
- Нажатие кнопки в таком сообщении (`messages.getBotCallbackAnswer` от P): если у сообщения в ящике B есть `via_business_bot_id`, боту уходит `updateBusinessBotCallbackQuery{query_id, user_id: P, connection_id, message (в представлении B), chat_instance, data}` вместо `updateBotCallbackQuery`; ответ бота `messages.setBotCallbackAnswer` — без обёртки, как обычно (механизм ожидания ADR 0043).

### Обновления на другие устройства и другим пользователям (сводка)
| действие | владельцу (другие устройства) | другим |
|---|---|---|
| часы, местоположение, вступление, автоответы | `updateUser{self}` (seq) | ничего, перечитывают `userFull` |
| ссылки на чат | ничего | ничего |
| ярлык создан / сообщение добавлено или правлено | `updateNewQuickReply`, `updateQuickReplyMessage` (seq) | — |
| переименование, порядок | `updateQuickReplies` (seq) | — |
| удаление ярлыка / сообщений | `updateDeleteQuickReply` / `updateDeleteQuickReplyMessages` (seq) | — |
| автоответ отправлен | обычная отправка (pts) | обычное входящее у P |
| подключение бота | ответ `Updates` | боту `updateBotBusinessConnect` (qts) |
| пауза, отключение в чате | `updatePeerSettings` (seq) | боту `updateBotBusinessConnect` при отключении |
| сообщение бизнес-чата | — | боту `updateBot*BusinessMessage` (qts) |

### После перезапуска
Всё состояние в MySQL; события — outbox (ADR 0012); воркер автоответов продолжает с неотправленных строк по аренде; очередь qts переживает перезапуск, бот догоняет `getDifference`. Redis для Business не нужен.

### Метрики и лог
`business_autoreply_total{kind, result}` (`sent`, `skipped`, `failed`), `business_bot_updates_total{kind}`, `business_chatlink_resolve_total{result}`. В лог — только id пользователей, ярлыков и коды ошибок; ни текста сообщений, ни адресов, ни slug, ни `connection_id` (`tools/logaudit`: добавить имена `slug`, `connectionid`, `address`).

## Порядок работ
Каждая часть — одна ветка-коммит серии, свои тесты; размеры: S ≈ полдня, M ≈ день, L ≈ два дня одного исполнителя.

1. **Каркас и профиль (M).** `pkg/business` (хранилище, часы, получатели, slug, константы), миграция `migrate-20261119-business.sql` со всеми таблицами `pkg/business`, модуль `app/bff/business` с регистрацией всех семи сервисов и маршрутами в трёх `session.yaml` (неготовые методы — `ErrEnterpriseIsBlocked`), ошибки в `rpc_error_codes.go`, ключи пределов в `app_config.json`; `updateBusinessWorkHours`, `updateBusinessLocation`, `updateBusinessIntro`; поля профиля в `users.getFullUser`, `open_now`, `updateUser`; `time/tzdata`. **Первая, остальные после неё.**
2. **Ссылки на чат (M).** Пять методов, `KindBusinessChat` и страница в `app/interface/links`. После 1; параллельно с 3, 6, 9.
3. **Быстрые ответы: хранилище и ярлыки (L).** Миграция `migrate-20261119-quick-replies.sql`, `quick_replies.go` в dao msg, RPC `msg.*` (добавление, чтение, проверка, переименование, порядок, удаление ярлыка), ветка ярлыка в `messages.sendMessage`/`sendMedia`/`sendMultiMedia` (bff messages), обработчики `getQuickReplies`, `getQuickReplyMessages`, `checkQuickReplyShortcut`, `editQuickReplyShortcut`, `reorderQuickReplies`, `deleteQuickReplyShortcut`, типы seq. После 1; параллельно с 2, 6, 9.
4. **Быстрые ответы: правка, удаление сообщений, отправка (M).** `editMessage` с `quick_reply_shortcut_id`, `deleteQuickReplyMessages`, `sendQuickReplyMessages`, ярлык в `forwardMessages` и `messages.sendInlineBotResult` (`app/bff/bots/internal/core/messages.inline_handlers.go`). После 3 (те же файлы msg и bff messages) и после 106 D2: та выносит общую отправку инлайн-результата из `MessagesSendInlineBotResult`, ветка ярлыка встраивается в вынесенную функцию (второй выноски не делать).
5. **Автоответы (L).** `updateBusinessGreetingMessage`, `updateBusinessAwayMessage`, поля в `getFullUser`, решение в `inbox.sendUserMessageToInboxV2`, воркер, выключение при удалении ярлыка. После 4 (отправка ярлыка — общая функция).
6. **Подключение бота: настройки (M).** `updateConnectedBot`, `getConnectedBots`, `toggleConnectedBotPaused`, `disablePeerConnectedBot`, `confirmBotConnection`, `peerSettings`, `pluma-admin bot set --business`. После 1; параллельно с 2, 3, 9. Обновления боту здесь не шлются — только запись (их добавляет 7).
7. **Очередь qts и обновления боту (L).** `pkg/qtsqueue` (перенос счётчика из `pkg/secretchat` без изменения поведения секретных чатов), миграция `migrate-20261119-qts-updates.sql`, слияние в `getDifference`/`getState`, событие `inbox.botBusinessUpdateToInbox` и его выпуск из отправки, правки, удаления и inbox, `updateBotBusinessConnect`, `getBotBusinessConnection`. После 6; после 5, если они в одной серии (обе меняют `inbox.sendUserMessageToInboxV2_handler.go`).
8. **Действия бота через подключение (M).** Поле метаданных, сессия, `business.Act`, ветки в `sendMessage`/`sendMedia`/`sendMultiMedia`/`editMessage`/`deleteMessages`/`readHistory`/`setTyping`/`updatePinnedMessage`/`account.updateProfile`/`messages.uploadMedia`, `via_business_bot_id`, `updateBusinessBotCallbackQuery`. После 7 (и после 3–4: те же обработчики bff messages).
9. **Клиенты (M, четыре патча).** Android: условие строк Business в `SettingsActivity`/`ProfileActivity` снова апстримовское (`premiumFeaturesBlocked`), маршрут `settings/business` в `LinkManager` открыт (часть A-112), строки `showAds*` в `PremiumPreviewFragment` скрыты. iOS: `|| context.isPremium` в `PeerInfoSettingsItems` возвращён, раздел рекламы в `PremiumIntroScreen` (mode `.business`) и запись поиска `Business_DontHideAds` скрыты. Desktop: кнопка Business вне условия `premiumCanBuy` в `BuildPremiumSection` (остальное как в D-084), `BuildSponsoredSection` не вызывается. Web K: строка Business вне `<Show when={!premiumBlocked()}>`, `TelegramBusiness` убран из `SALE_ENTRIES` (W-070), `SponsoredToggle` в `promoSlide.tsx` скрыт. Записи в `clients/patches/*.md` (следующие номера по `clients/patches/README.md`), `verify_*` и тест порчи в `ci/patch.py` каждого клиента. Независима от сервера, выкатывать вместе с ним (иначе строка откроет экраны, которые отвечают ошибками).
10. **Сквозные тесты и документы (S).** e2e, не вошедшие в части; `docs/ops/business.md`; строки `TESTING-BACKLOG`; `go run ./tools/apicoverage`.

Общие файлы:
- Внутри задачи: `app/bff/messages/internal/core/messages.send*_handler.go` (3, 4, 8), `msg.tl` и dao msg (3, 4, 5), `inbox.sendUserMessageToInboxV2_handler.go` (5, 7), `users.getFullUser_handler.go` (1, 5), `app_config.json` (1).
- С партией: `messages.inline_handlers.go` — задачи 099 (B, C) и 106 (D2, F, G1), сливать по одной, часть 4 — после 106 D2; `pkg/botstore`/`pluma-admin bot` — 099 A и 106 A2/B (разные волны). `pkg/qtsqueue` и таблица `qts_updates` — единственный владелец часть 7 этой задачи; 106 qts не использует (HTTP Bot API и прочие обновления ботов с qts в партию не входят). `app_config.json` — 099, 101, 104, 106; `users.getFullUser_handler.go` — 101 (часть 6); превью местоположения на картах — 100.

## Тесты
Модульные:
- `pkg/business`: нормализация часов (сортировка, слияние смежных и пересекающихся, перенос хвоста за воскресенье, обрезка длиннее недели, 29 интервалов — ошибка, пустой результат — ошибка) на тех же примерах, что D `SortAndMerge`/`MoveTailToFront`; `open_now` на границах (воскресенье 23:59 → понедельник 00:00, интервал через полночь, смена летнего времени в поясе); сопоставление получателей — таблица всех комбинаций флагов, `users`, `exclude_selected`, `exclude_users`; правила приветствия (пустой диалог, 6 и 8 суток при 7), «нет на месте» (три расписания, `offline_only` на 9 и 11 минутах, частота), slug (алфавит, длина), права и правило 24 часов.
- Хеши `getQuickReplies` и `getQuickReplyMessages` против значений, посчитанных по алгоритмам A (`MediaDataController.calcHash`) и D на фиксированных данных.
- Проверка имени ярлыка (символы из правил клиентов, 32/33 символа).
- Пределы в коде равны `app_config.json`.
- `tools/logaudit` ловит вывод `slug`, `connection_id`, адреса.

Интеграционные (`pkg/testinfra`, MySQL, Redis, Kafka; по одному пакету за раз):
- миграции дважды подряд; `pkg/business` под гонкой двух записей настроек; предел ссылок под параллельным созданием; `views` под параллельным resolve;
- быстрые ответы: создание ярлыка двумя параллельными отправками с одним именем — один ярлык; предел 20 сообщений под гонкой; повтор `random_id`; удаление ярлыка выключает автоответ;
- автоответы: повтор inbox-события не удваивает строку; три воркера — отправка один раз; аренда упавшего воркера; постоянная ошибка (P заблокировал B) удаляет строку; частота «нет на месте»;
- очередь qts: секретный чат и бизнес-обновление у одной авторизации получают разные qts подряд; `getDifference` отдаёт обе очереди по порядку и подтверждает; повтор события боту не удваивает строки; удаление старше 7 суток;
- `business.Act`: правило 24 часов, пауза, отключение, чужое подключение.

Сквозные (`pkg/mtclient`, `test/e2e`, сессии после `getState`):
- `TestBusinessProfile`: часы (с `open_now` в поясе, где сейчас открыто и закрыто), местоположение, вступление со стикером; второй пользователь видит в `users.getFullUser`, автоответов не видит; `updateUser` на втором устройстве; `TIMEZONE_INVALID`, `BUSINESS_WORK_HOURS_PERIOD_INVALID`.
- `TestBusinessChatLinks`: создать, исправить, список, resolve вторым пользователем (`views` 1), resolve владельцем (`views` не меняется), удалить → `CHATLINK_SLUG_EXPIRED`; 101-я — `CHATLINKS_TOO_MUCH`; страница `links` для `m/<slug>` отвечает 200 с `plm://message?slug=`.
- `TestQuickReplies`: создание по имени (последовательность iOS и D), добавление по id, `updateNewQuickReply`/`updateQuickReplyMessage` на втором устройстве, хеш и `NotModified`, переименование (`SHORTCUT_OCCUPIED`), порядок, правка, удаление сообщений, `sendQuickReplyMessages` с явными id (Android) и пустыми (iOS), альбом, удаление ярлыка — `updateDeleteQuickReply`.
- `TestBusinessGreeting`: новый собеседник получает приветствие с `offline`; второе сообщение — нет; собеседник вне получателей — нет.
- `TestBusinessAway`: «всегда» с `offline_only` — владелец в сети не отвечает, не в сети больше 10 минут (время стенда подменяется через конфиг воркера, как в тестах отложенных) — отвечает; второй раз в тот же день — нет.
- `TestBusinessBot`: бот с `--business on`, подключение, бот получает `updateBotBusinessConnect` и `updateBotNewBusinessMessage` с qts, отвечает через `invokeWithBusinessConnection` — собеседник получает сообщение от владельца, владелец видит `via_business_bot_id`; пауза — бот не получает и не может ответить (`BOT_ACCESS_FORBIDDEN`); `disablePeerConnectedBot`; отключение с `inputUserEmpty`; бот вне сети догоняет `getDifference` по qts; `BUSINESS_CONNECTION_NOT_ALLOWED` для `messages.getHistory` под обёрткой.
- Хотя бы один тест проверить падающим без изменения (например, `TestQuickReplies` на текущем `master` получает `ERR_ENTERPRISE_IS_BLOCKED`).

## Проверки владельца (строки для `docs/project/TESTING-BACKLOG.md`)
- [ ] Android, iOS, Desktop, Web: в настройках есть «Telegram Business», экран открывает функции без покупки и без переключателя рекламы.
- [ ] Часы работы с часовым поясом: в профиле у второго аккаунта «Открыто»/«Закрыто» верно, в том числе интервал через полночь.
- [ ] Местоположение с адресом и точкой: видно в профиле у второго аккаунта (превью карты — после задачи 100).
- [ ] Своё вступление со стикером: второй аккаунт без чата видит его вместо стандартного.
- [ ] Ссылка на чат: создать, открыть со второго аккаунта в каждом клиенте — открывается чат с подставленным текстом, счётчик просмотров растёт; ссылка `https://pluma.chat/m/…` в браузере ведёт на страницу «Открыть в Pluma».
- [ ] Быстрые ответы (Android, iOS, Desktop): создать с текстом, фото и альбомом, отправить через `/` в личном чате, переименовать, переупорядочить, удалить; изменения видны на втором устройстве.
- [ ] Приветствие: новый собеседник получает его один раз; после периода без активности — снова.
- [ ] «Нет на месте»: «вне часов работы» и «только не в сети» — собеседник получает автоответ, когда владелец закрыт и не в сети, и не получает, когда владелец в сети.
- [ ] Подключённый бот (тестовый бот с бизнес-режимом): подключить с правами, бот отвечает собеседнику от имени владельца; панель над чатом — пауза, возобновление, «отключить в этом чате»; отключить бота с iOS.
- [ ] Настройки Business на втором устройстве того же аккаунта совпадают после перезапуска клиента.

## Эксплуатация
- Миграции: `migrate-20261119-business.sql`, `migrate-20261119-quick-replies.sql`, `migrate-20261119-qts-updates.sql` — применяет сервис migrate, действий оператора нет.
- Новых переменных окружения и сайдкаров нет. В бинарники bff, msg и links входит `time/tzdata` (+~450 КБ).
- `help.getTimezonesList` берётся из каталога: перед выкаткой проверить, что в каталоге прода есть запись (`docs/ops/catalog.md`); пустой список ломает выбор пояса в клиентах.
- Бизнес-режим бота: `pluma-admin bot set <bot> --business on` (`docs/ops/bots.md`).
- `docs/ops/business.md`: что хранится, пределы, как выключить автоответы пользователю (удалить строки `business_auto_reply_settings`), метрики, алерт `BusinessAutoReplyFailing` (доля `failed` > 10 % за 15 минут) в `teamgramd/deploy/prometheus/server/rules/`.
- Диск: сотни строк на пользователя (ярлыки до 100 × 20 сообщений JSON) — единицы МБ на весь сервер; очередь qts ограничена подтверждением и 7 сутками. RAM — без изменений.
- Выкатывать сервер вместе с клиентскими патчами части 9.

## Решения к подтверждению
1. Пределы: 100 ярлыков, 20 сообщений в ярлыке, 100 ссылок, вступление 32/70, заголовок ссылки 32 (Telegram значений не публикует; взяты умолчания клиентов).
2. «Существующий» чат — начатый до первого включения автоответа (`enabled_at`), «новый» — после; правка настройки границу не сдвигает. Документация смысл не раскрывает.
3. «Нет на месте» уходит собеседнику не чаще раза, пока владелец сам не ответит, и не чаще раза в 24 часа. При «вне часов работы» без заданных часов не уходит. Если сработали оба автоответа, уходят оба, приветствие первым.
4. Автоответы не отправляются ботам, служебным аккаунтам и в «Избранное».
5. `via_business_bot_id` — только у копии владельца; собеседник видит обычное сообщение владельца.
6. `business_bot_manage_url` — ссылка на бота `https://pluma.chat/<бот>` без параметра start.
7. Права подарков, звёзд и историй сохраняются, но не действуют; смена фото и имени пользователя через подключение не поддерживается.
8. Очередь qts бота хранит неподтверждённое 7 суток.
9. Переключатель рекламы на экранах Business скрывается патчем во всех клиентах.
10. Имена ошибок, которых Telegram не публикует: `ADDRESS_INVALID`, `NO_ACTIVITY_DAYS_INVALID`, `TITLE_INVALID`/`DESCRIPTION_INVALID` для вступления.

## Не проверено
- Как Telegram понимает «существующие»/«новые» чаты, как часто шлёт «нет на месте», шлёт ли оба автоответа сразу и что делает при «вне часов» без часов.
- Видит ли собеседник `via_business_bot_id`; что Telegram кладёт в `business_bot_manage_url` (вероятно, `?start=…`) и в `updates` ответа `updateConnectedBot`.
- Получает ли бот исходящие сообщения владельца — вывод из описания `sender_business_bot` в Bot API, прямого утверждения нет.
- Смысл `boolFalse` у `checkQuickReplyShortcut`; можно ли ставить ярлык вместе с `schedule_date`; можно ли `sendQuickReplyMessages` боту.
- Пределы iOS для быстрых ответов (ключей `quick_replies_limit` в Swift-коде не нашлось; откуда iOS берёт пределы — не прослежено).
- Обновляют ли клиенты список ссылок на чат на другом устройстве без повторного входа на экран.
- Есть ли запись `help.getTimezonesList` в каталоге прода.
- Что показывает страница `t.me/m/<slug>` в браузере у Telegram (оформление страницы `m/` — по образцу страницы пользователя).
- Поведение Web K в Business для функций без редактора (быстрые ответы, автоответы): в списке функций без перехода — не проверено вживую.

## Результат
Telegram Business для всех сделан: части 1–10. Готовы профиль, ссылки на чат, быстрые ответы, приветствие и «нет на месте», подключённый бот с настройками, обновления боту по qts и действия бота от имени аккаунта. Патчи всех четырёх клиентов возвращают вход в Business. Код в партии `batch/20261115`, на прод не выкачен. Ветки клиентов `task/105-client-patches` не влиты.

- **1.** `pkg/business`, таблицы `business_*`, новая служба bff: часы работы с часовым поясом, местоположение, вступление. Всё это есть в `users.getFullUser`. Пределы в app config.
- **2.** Ссылки на чат: пять методов, до 100 ссылок, случайный slug. Страница `https://pluma.chat/m/<slug>` в `links` — без текста ссылки, с `noindex`.
- **3.** Быстрые ответы: ярлыки и сообщения в своих таблицах (`migrate-20261119-quick-replies.sql`), отдельная последовательность id, повтор отправки по `random_id` не дублирует сообщение. Обслуживаются методы `getQuickReplies`, `getQuickReplyMessages`, `checkQuickReplyShortcut`, `editQuickReplyShortcut`, `reorderQuickReplies`, `deleteQuickReplyShortcut`.
- **4.** Правка, удаление и отправка быстрых ответов. Это три новых RPC msg.
- **5.** Автоответы. Решение принимается в транзакции ящика аккаунта, рабочий процесс msg берёт строку в аренду и отправляет. Ответ уходит один раз при любых рестартах, временный сбой повторяется ограниченное число раз.
- **6.** Подключённый бот:
  - один на аккаунт, с правами и получателями;
  - пауза и отключение в отдельных чатах;
  - панель в `getPeerSettings`;
  - `pluma-admin bot set -business`.
- **7.** Очередь `qts_updates` на авторизацию от общего счётчика qts с секретными чатами. По ней бот получает `updateBotBusinessConnect` и `updateBotNew`/`Edit`/`DeleteBusinessMessage`, а также `account.getBotBusinessConnection`.
- **8.** `invokeWithBusinessConnection`: сессия передаёт подключение в метаданных (`third_party(proto)`). Любой другой метод получает `BUSINESS_CONNECTION_NOT_ALLOWED`. Права проверяет `pkg/business`.
- **9.** Патчи A-123, I-132, D-103, W-084: вход в Business снова виден, переключателя рекламы нет.
- **10.** Сквозные тесты, тревога `BusinessAutoReplyFailing`, `docs/ops/business.md`.

**Исправлено после слияния.**
- Бизнес-бот не может прислать виджет от имени аккаунта (`INPUT_MEDIA_WIDGET_NOT_ALLOWED`), а игра через подключение явно отклоняется `BOT_ACCESS_FORBIDDEN` (`9a9d34e18`).
- Поиск бизнес-подключения выполняется только для `getBotCallbackAnswer` (`5aa7d6996`).
- Коды входа в сообщениях бизнес-бота аннулируются (`af465c56c`).
- При удалении аккаунта удаляются строки `business_*` и `quick_reply_*` (`f7a8983e5`).

**Отклонения от брифа.** По коммитам и документам частей не найдены.

**Решения.** Ждут подтверждения п. 1–10 раздела «Решения к подтверждению». Новый вопрос владельцу: Bot API (`sendGame`) допускает `business_connection_id`, то есть Telegram разрешает бизнес-игры. Сейчас они отклоняются; если их делать, это отдельная функция.

**Не проверено.** В клиентах — пункты задачи 105 в `TESTING-BACKLOG.md`. Патчи клиентов в собранных выпусках. Поведение Telegram — раздел «Не проверено» выше.

**Проверка.** Партия `batch/20261115`, полный прогон на CachyOS на `2c1b155cf`: `go build ./...` и `go vet -tags integration,e2e -unreachable=false` обоих модулей; `go test ./...` — 237 пакетов ok, proto — 3; `go test ./tools/...` (с журнальным аудитом) — 6; `-race` — 191 пакет ok; `make lint` — 0 замечаний; интеграционные тесты — 70 пакетов по одному, все ok; e2e на свежем стенде — 300 PASS, 0 FAIL, 2 SKIP (замер задержки и ручной тест). На прод не выкачено, в клиентах владельцем не проверено.
