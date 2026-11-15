# 0067. Telegram Business для всех: профиль, быстрые ответы отдельным ящиком, автоответы на сервере, подключённые боты через очередь qts

- Статус: предложено (2026-11-15)
- Задача: 105 (`docs/tasks/105-business.md`, ветка `task/105-business`).
- Связано: ADR 0041 (Premium для всех — Business тоже), ADR 0011/0012 (ящик и outbox), ADR 0020 (журнал seq), ADR 0036 (ссылки pluma.chat), ADR 0043 (боты MTProto), ADR 0028 (каталог: часовые пояса), ADR 0030 (статус в сети), ADR 0057/0061 (содержимое не покидает домашний сервер), ADR 0009 (наши правки в `third_party` отдельными коммитами с `// pluma:`).

## Контекст

### Что есть сейчас
- Все методы Business не обслуживаются (`docs/project/api-coverage.md`: `account.updateBusiness*`, `*BusinessChatLink*`, `*ConnectedBot*`, `messages.*QuickReply*` — `missing`, `account.confirmBotConnection` — `blocked`). Маршрутов к сервисам `RPCBusinessOpeningHours`, `RPCBusinessLocation`, `RPCBusinessGreeting`, `RPCBusinessIntro`, `RPCBusinessChatLinks`, `RPCBusinessQuickReply`, `RPCBusinessConnectedBots` в `session.yaml` нет (есть только `RPCTimezones`), поэтому прокси отвечает `ERR_ENTERPRISE_IS_BLOCKED`.
- `users.getFullUser` отдаёт все поля `business_*` пустыми (`app/bff/users/internal/core/users.getFullUser_handler.go:123-127`), `peerSettings.business_bot_*` тоже пусты (`app/bff/dialogs/internal/core/messages.getPeerSettings_handler.go:51-56`).
- Сессия снимает обёртку `invokeWithBusinessConnection` и выполняет запрос как обычный (`app/interface/session/internal/sess/session_invoke.go:267-282`): бот, приславший такой запрос, сейчас действует от своего имени.
- `quick_reply_shortcut` в `messages.sendMessage` и соседях не читается (`app/bff/messages/internal/core/messages.sendMessage_handler.go:83,124`).
- qts у Pluma — только у секретных чатов, свой у каждой авторизации: счётчик `encrypted_chat_devices.qts`, очередь `encrypted_chat_queue`, подтверждение через `updates.getDifference` (`pkg/secretchat/store.go:512-518, 575-700`). `user_state.qts` зарезервирован и не меняется (`pkg/userbox/state.go:26`).
- Флаг бота `bots.bot_business` уже есть в базе и доходит до `user.bot_business` (`migrate-20261109-bots-core.sql`, `app/service/biz/user/internal/dao/user.go:60`, `immutable_user_helper.go:249`), но задать его нечем: `pluma-admin bot set` знает только `--privacy`, `--groups`, `--inline-geo`, `--inline` (`tools/pluma-admin/bot.go:178-185`).
- Клиенты Pluma прячут строку «Telegram Business» (задача 071: A-109, I-109, D-084, у Web K строка внутри блока покупки).

### Документация (core.telegram.org, прочитано 2026-11-15)
- `/api/business`: часы — минуты недели от понедельника 00:00, `end_minute` до 8×24×60, до 28 интервалов, `open_now` ставит только сервер, `timezone_id` из `help.getTimezonesList`; адрес до 96 символов, пустой вызов удаляет местоположение; изменения профиля шлют `updateUser`. Быстрые ответы создаются отправкой в `inputPeerSelf` с `quick_reply_shortcut`, сообщения получают id «из отдельной последовательности», обновления `updateNewQuickReply`, `updateQuickReplyMessage`, `updateDeleteQuickReplyMessages`, `updateQuickReplies`, `updateDeleteQuickReply`; пределы `quick_replies_limit`, `quick_reply_messages_limit`, ошибки `QUICK_REPLIES_TOO_MUCH`, `REPLY_MESSAGES_TOO_MUCH`; отправлять быстрые ответы можно только в личные чаты с пользователями. Ссылки на чат: просмотр считается при `account.resolveBusinessChatLink`, предел `business_chat_links_limit`, `CHATLINKS_TOO_MUCH`.
- `/constructor/inputBusinessGreetingMessage`: `no_activity_days` — 7, 14, 21 или 28. `/constructor/inputBusinessAwayMessage`: `offline_only` — «не отправлять, если аккаунт был в сети последние 10 минут». `/constructor/inputBusinessRecipients`: категории и `exclude_selected` «инвертирует выбор».
- `/constructor/message`: `offline` — сообщение отправлено «из-за запланированного действия отправителя, например, как автоответ»; `via_business_bot_id` — отправлено бизнес-ботом от имени пользователя.
- `/api/bots/connected-business-bots`: подключить можно одного бота с `user.bot_business` (иначе `BOT_BUSINESS_MISSING`); бот получает `updateBotBusinessConnect`, `updateBotNewBusinessMessage`, `updateBotEditBusinessMessage`, `updateBotDeleteBusinessMessage`, `updateBusinessBotCallbackQuery`; действует через `invokeWithBusinessConnection` в `dc_id` подключения; `messages.uploadMedia` берёт `business_connection_id` вместо обёртки; ошибки `BUSINESS_CONNECTION_INVALID`, `BUSINESS_CONNECTION_NOT_ALLOWED`, `BOT_ACCESS_FORBIDDEN`; пауза — временное исключение, `disablePeerConnectedBot` добавляет собеседника в `exclude_users` (или убирает из `users` при `exclude_selected`).
- `/constructor/businessBotRights`: `reply` — «отправлять и править сообщения в личных чатах, где были входящие за последние 24 часа».
- `/constructor/updateBotBusinessConnect`, `/constructor/updateBotNewBusinessMessage`: у всех таких обновлений есть `qts`.
- `/bots/api`, поле `Message.sender_business_bot`: «только у исходящих сообщений, отправленных от имени подключённого бизнес-аккаунта» — значит, `business_message` приходит боту и для исходящих сообщений владельца (вывод).
- `/api/links`: `t.me/m/<slug>` и `tg://message?slug=<slug>`.

### Клиенты
Подробно, с номерами строк — бриф 105, раздел «Контракт клиентов». Решающее для архитектуры:
- Сообщения быстрых ответов лежат у клиентов в «Избранном» владельца с отдельным пространством id (tdesktop `data/business/data_shortcut_messages.cpp:113`, `IsShortcutMsgId`, iOS `Namespaces.Message.QuickReplyCloud` в `QuickReplyMessages.swift:191`), поэтому их `peer_id` — сам владелец, а id не должны пересекаться с обычными только в смысле «другое пространство»: клиент их не смешивает.
- Автоответы — это обычные ярлыки с именами `hello` и `away` (Android `ui/Business/QuickRepliesController.java:31-32`, tdesktop `settings/business/settings_shortcut_messages.cpp:319-323`); сервер их не создаёт, только ссылается по `shortcut_id`.
- Отключение бота с iOS приходит с `deleted` и `bot = inputUserEmpty` (`TelegramCore/Sources/TelegramEngine/Messages/QuickReplyMessages.swift:1214, 1265-1267`).

## Решение

### 1. Business у всех, без проверок Premium
Все методы обслуживаются для любого пользователя (не бота); `PREMIUM_ACCOUNT_REQUIRED` не отвечается никогда (ADR 0041). Пределы — в `app_config.json` (бриф 105, «Пределы»).

### 2. Хранилище профиля — `pkg/business`
Часы, местоположение, приветствие, автоответ «нет на месте», ссылки на чат и подключение бота — пакет `pkg/business` над MySQL (как `pkg/contactnotes`, `pkg/botstore`): его читают bff (`business`, `users`, `dialogs`, `files`, `messages`), сервис msg (решения об автоответах и о рассылке боту) и сервис links. Кэша в Redis нет: чтения по первичному ключу, ~100 пользователей. Значения часов и получателей хранятся нормализованными (отсортированные и слитые интервалы, id пользователей), а не как пришли.

### 3. Быстрые ответы — отдельный ящик в сервисе msg
Как отложенные сообщения (`scheduled_messages`, задача 061): таблицы `quick_reply_shortcuts` и `quick_reply_messages` в сервисе msg, сообщение хранится JSON-ом, id — `AUTO_INCREMENT` таблицы (общий для всех, клиенты считают id непрозрачными), `random_id` уникален на владельца. Сообщения быстрых ответов никогда не попадают в ящик, диалоги, поиск и `getDifference`; их обновления идут в журнал seq владельца (ADR 0020: «загрузи заново» или полное состояние, повтор хвоста журнала даёт то же). `messages.sendQuickReplyMessages` — обычная отправка из ящика владельца (все проверки личного чата, `random_id` клиента или сгенерированные сервером).

### 4. Автоответы считает сервер, отправляет воркер
- Решение «отправить приветствие / автоответ» принимается в транзакции ящика владельца, применяющей входящее личное сообщение (`inbox.sendUserMessageToInboxV2`): там известны прежняя дата последнего сообщения диалога, контакт ли отправитель и сам факт приёма. Решение записывается строкой в `business_auto_replies` в той же транзакции, поэтому повторное применение события строку не удваивает (уникальный ключ по сработавшему сообщению).
- Воркер в сервисе msg (как `scheduled_worker.go`: аренда строки условным `UPDATE`, удаление строки в транзакции ящика, которая отправляет) отправляет сообщения ярлыка от имени владельца обычным путём отправки. `random_id` выводится из id строки и номера сообщения, поэтому повтор после падения не создаёт дубликат (`sender_out_random`).
- У автоответов стоит `message.offline`.
- Правила (получатели, неактивность, расписание, «только не в сети», частота) — бриф 105, «Автоответы». То, чего документация не говорит (частота «нет на месте», смысл «существующих/новых чатов», порядок двух автоответов), — наш выбор, вынесенный на подтверждение.

### 5. Ссылки на чат — `https://pluma.chat/m/<slug>`
Ссылка строится из `env2.TDotMe` (ADR 0036, равен хосту `me_url_prefix`). Slug — 16 символов `[A-Za-z0-9]` из криптографического генератора. Сервис `links` получает тип ссылки `m/<slug>` со страницей «Открыть в Pluma» (`plm://message?slug=…`) вместо нынешнего ухода на главную. Просмотр считается только по `account.resolveBusinessChatLink` из клиента, не владельцем и не страницей сайта.

### 6. Подключённый бот
- **Модель.** Одно подключение на пользователя (`business_bot_connections`, ключ — пользователь), `connection_id` — случайная непрозрачная строка, выдаётся при подключении и живёт до отключения; изменение настроек оставляет её прежней. `dc_id` — `this_dc` конфигурации (1). Права (`businessBotRights`) хранятся битами как пришли; действуют только права сообщений и профиля (имя, «о себе»); права подарков, звёзд и историй сохраняются, но ничего не открывают (эти функции вне Pluma или вне задачи).
- **Пауза и отключение в чате.** Пауза — строка `business_bot_paused_peers`; `disablePeerConnectedBot` меняет получателей подключения ровно как сказано в документации. Владелец получает `updatePeerSettings` на свои устройства.
- **Очередь qts для ботов.** Обновления бота о бизнес-чатах несут `qts`, и библиотеки ботов считают по нему дыры. Поэтому у авторизации один счётчик qts — уже существующий `encrypted_chat_devices.qts` (секретные чаты и бизнес-обновления делят его, у бота секретных чатов нет, но общий счётчик исключает совпадение номеров в принципе). Новая таблица `qts_updates` хранит обновления с qts для каждой авторизации бота; `updates.getDifference` отдаёт их в `other_updates` вместе с очередью секретных чатов, по возрастанию qts, и подтверждает тем же `qts` клиента; `updates.getState` уже отдаёт этот счётчик. Счётчик и очередь выносятся из `pkg/secretchat` в общий пакет `pkg/qtsqueue`, `pkg/secretchat` пользуется им.
- **Доставка боту.** Транзакция ящика владельца (входящее — в inbox, исходящее, правка, удаление — в отправке) выпускает outbox-событие (ADR 0012) получателю-боту `inbox.botBusinessUpdateToInbox`, если у владельца есть подключение и собеседник подходит под получателей и не на паузе. Транзакция бота применяет событие: пишет строки `qts_updates` всем живым авторизациям бота и пушит их. Ровно один раз — по правилам ADR 0012.
- **Действия бота.** Сессия кладёт `connection_id` из `invokeWithBusinessConnection` в новое поле метаданных `business_connection_id` (по образцу `takeout`, `main_auth_wrapper.go:755`; правка `third_party/teamgram-proto/mtproto/rpc/metadata` с `// pluma:`). В bff одна функция `business.Act(ctx, md, method, peer)` проверяет подключение, право, собеседника и правило 24 часов и подменяет в метаданных пользователя на владельца, запоминая бота (`via_business_bot_id`). Разрешены: `messages.sendMessage`, `sendMedia`, `sendMultiMedia`, `editMessage`, `deleteMessages`, `readHistory`, `setTyping`, `updatePinnedMessage`, `account.updateProfile`; `messages.uploadMedia` — по своему полю `business_connection_id`. Остальное под обёрткой — `BUSINESS_CONNECTION_NOT_ALLOWED`.
- **`via_business_bot_id`** ставится на копию владельца; у собеседника его нет (не проверено, что так у Telegram; вынесено на подтверждение).
- `account.confirmBotConnection`: подключение создаёт сам владелец со своего устройства, подтверждать нечего; метод отвечает `boolTrue` для подключённого бота. `updateNewBotConnection` сервер не шлёт.
- Флаг бизнес-режима бота задаёт оператор: `pluma-admin bot set <bot> --business on|off`.

### 7. Клиенты
- Строка «Telegram Business» снова видна во всех четырёх клиентах (откат части правок 071: Android `SettingsActivity`/`ProfileActivity`, iOS `PeerInfoSettingsItems`, Desktop `BuildPremiumSection`, Web K `settings.tsx` и поиск по настройкам W-070). Экраны Business у пользователя с Premium открывают редакторы функций, без покупки (Android `PremiumPreviewFragment.java:867-880`, iOS `PremiumIntroScreen(mode: .business)`, tdesktop `settings_business.cpp`, Web K `components/popups/premium.tsx:108-114`).
- Переключатель «Не скрывать рекламу» на экранах Business скрывается патчем во всех четырёх клиентах: рекламы в Pluma нет (решение владельца), `account.toggleSponsoredMessages` не обслуживается, и переключатель показывал бы ошибку.
- Других правок клиентов нет: всё остальное клиенты умеют сами.

## Последствия
- Новые таблицы: `business_profiles`, `business_auto_reply_settings`, `business_auto_replies`, `business_auto_reply_log`, `business_chat_links`, `business_bot_connections`, `business_bot_paused_peers` (`pkg/business`), `quick_reply_shortcuts`, `quick_reply_messages` (msg), `qts_updates` (`pkg/qtsqueue`). Миграции идемпотентны, применяет сервис migrate.
- Сервис msg получает второй воркер (автоответы); после перезапуска он продолжает с неотправленных строк, аренда упавшего экземпляра истекает.
- `updates.getDifference` для бота отдаёт обновления qts; поведение для людей не меняется (у людей строк `qts_updates` нет).
- Генерируемый код: `msg.tl` (RPC быстрых ответов и автоответов), `inbox.tl` (событие боту), метаданные RPC — перегенерация один раз при слиянии.
- Сервисам bff, msg и links нужен `time/tzdata` (часовые пояса без системной базы образа).
- Текстов для людей сервер не добавляет: всё, что видно в клиентах, — их собственные строки; страница `m/` сервиса `links` — тексты сайта ru/en, как у остальных страниц (ADR 0036).

## Что не делается
- Платные сообщения и всё со звёздами (`inputInvoiceBusinessBotTransferStars`, права подарков и звёзд) — вне Pluma (решение владельца 2026-11-15); права хранятся, но не действуют.
- Истории от имени владельца ботом (`stories.sendStory` с бизнес-пиром, право `manage_stories`), смена фото и имени пользователя ботом, `account.setGlobalPrivacySettings` через подключение.
- «Люди рядом» по бизнес-местоположению (`contacts.getLocated` остаётся закрыт, `BUSINESS_ADDRESS_ACTIVE` не нужен).
- Поток проверки подключений (`updateNewBotConnection`, экран «новое подключение бота»).
- Реклама и `account.toggleSponsoredMessages`.
- HTTP Bot API: бизнес-обновления получают боты MTProto; HTTP-шлюз — задача платформы ботов (106).
- Свой `business_promo_order`: клиенты показывают свой порядок функций.

## Рассмотренные альтернативы
- **Быстрые ответы как сообщения «Избранного» с флагом.** Отвергнуто: попали бы в диалог, поиск, счётчики и `getDifference`; клиенты ждут отдельное пространство id.
- **Автоответ прямо в транзакции входящего.** Отвергнуто: транзакция чужого ящика не должна отправлять от имени владельца (ADR 0011: одна транзакция — один ящик), а отправка — это ещё и событие получателю.
- **Бизнес-обновления боту через журнал seq без qts** (как `updateBotCallbackQuery`, ADR 0043). Отвергнуто: у конструкторов `qts` обязателен, и клиент MTProto, увидевший qts без последовательности, уйдёт в бесконечный `getDifference`; к тому же сообщения бизнес-чата нельзя терять через 60 с, как нажатия кнопок.
- **Отдельный счётчик qts для бизнес-обновлений.** Отвергнуто: qts в `updates.state` один на авторизацию.

## Что осталось подтвердить владельцу
Список — бриф 105, «Решения к подтверждению».
