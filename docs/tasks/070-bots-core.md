# 070. Боты на уровне MTProto (ядро этапа 4)

- Статус: done (сервер; полный прогон партии и проверка в клиентах — позже)
- Зависит от: 002, 020 (ящики и журналы), 041–048 (каналы)
- ADR: 0041

## Цель
Бот — учётная запись MTProto: вход токеном, команды, описание, кнопка меню, инлайн-клавиатуры и ответы на кнопки, инлайн-режим, `/start` с параметром и глубокая ссылка, обновления через `getDifference`, боты в группах и каналах с режимом конфиденциальности. HTTP Bot API и мини-приложения — отдельные задачи.

## Что выяснено у клиентов и в документации
- **Вход:** `auth.importBotAuthorization` — `ACCESS_TOKEN_INVALID`, `ACCESS_TOKEN_EXPIRED`, `API_ID_INVALID` (core.telegram.org/method/auth.importBotAuthorization). Клиенты Telegram его не вызывают; вызывают библиотеки. `bots.createBot` — поток управляемых ботов: `manager_id` (tdesktop `boxes/peers/create_managed_bot_box.cpp`, Android `CreateBotAlert.java:275-279`), в ADR 0041 не берётся.
- **Команды** приходят в `botInfo` внутри `userFull`, `chatFull`, `channelFull`; изменение видно по `bot_info_version` (core.telegram.org/api/bots/commands). Порядок выбора списка, ограничения команд и описаний: core.telegram.org/bots/api, разделы Determining list of commands, BotCommand (команда 1–32 символа, строчные, цифры, `_`; описание 1–256), setMyCommands (не более 100), setMyName (64), setMyDescription (512), setMyShortDescription (120).
- **Кнопки:** `messages.getBotCallbackAnswer` → `updateBotCallbackQuery` боту → `messages.setBotCallbackAnswer`; клиенты обрабатывают `BOT_RESPONSE_TIMEOUT`; `data` до 64 байт (core.telegram.org/api/bots/buttons, method/messages.getBotCallbackAnswer, bots/api#inlinekeyboardbutton; `text` ответа до 200 символов — bots/api#answercallbackquery). `requires_password` от бота не поддержан: сервер пароль на нажатие не спрашивает.
- **Инлайн:** core.telegram.org/api/bots/inline; `cache_time`, `private`, `next_offset` до 64 байт, не больше 50 результатов — method/messages.setInlineBotResults и bots/api#answerinlinequery.
- **/start:** `messages.startBot` с `peer = inputPeerEmpty` (tdesktop `apiwrap.cpp:4936-4941`) или с ботом (Android `MessagesController.java:15485`). Для группы клиент не шлёт `messages.addChatUser`, а только `startBot` (Android `addUserToChat`, строки 15502–15627; tdesktop показывает ошибки как у добавления) — сервер добавляет бота сам. Параметр: A-Z a-z 0-9 _ -, до 64 символов; бот получает `/start abc`, в группе `/start@bot abc` (core.telegram.org/bots/features#deep-linking).
- **Режим конфиденциальности:** core.telegram.org/bots/features#privacy-mode: в группах бот видит команды ему, общие команды (если он был последним ботом, писавшим там), сообщения через него, ответы ему, служебные сообщения; бот-админ видит всё; сообщения каналов и личек видят все боты. Сообщения других ботов не видны ни в каком режиме (core.telegram.org/bots/faq).
- **Инлайн без чата:** `messages.getInlineBotResults` приходит с `inputPeerEmpty` из секретного чата (Android `MentionsAdapter.java:925`) и без получателя (tdesktop `location_picker.cpp:1230`); тогда `peer_type` в обновлении нет.
- **Флаги пользователя:** `bot_chat_history`, `bot_nochats`, `bot_inline_placeholder`, `bot_info_version`, `bot_can_edit` — в схеме layer 228.

## Решения
- Хранилище и путь управления — ADR 0041, `pkg/botstore`, `pluma-admin bot`.
- Клавиатуры: `pkg/botmarkup` проверяет разметку бота; `inputKeyboardButton*` превращаются в полученные клиентом формы; `reply_markup` от обычного аккаунта отбрасывается молча.
- Обновления боту о кнопке и инлайне — журнал seq (`EmitSeqUpdate`), новые типы разрешены в `userbox.SeqUpdate`; offline-бот получит их через `other_updates`, ответ на устаревший запрос — `QUERY_ID_INVALID`.
- Запросы с ожиданием бота не задерживают остальные запросы ключа (сессия, `waitsForBot`).

## Результат
Боты работают как MTProto-аккаунты: `auth.importBotAuthorization`, новый модуль bff `bots` (`bots.setBotCommands/getBotCommands/resetBotCommands`, `setBotInfo/getBotInfo`, `setBotMenuButton/getBotMenuButton`, `getAdminedBots`, `messages.getBotCallbackAnswer/setBotCallbackAnswer`, `getInlineBotResults/setInlineBotResults/sendInlineBotResult`, `messages.startBot` — глубокие ссылки). `userFull.bot_info`, `chatFull.bot_info`, `channelFull.bot_info` с командами по областям и языкам и кнопкой меню; `bot_can_edit` по владельцу. Пуши и журнал для бота — как у пользователя (`BotSync` не нужен). Режим конфиденциальности — в базовых группах и супергруппах. Управление — `pluma-admin bot create|list|token|set`. Клавиатуры проверяются и доходят до клиентов.

- **Миграция:** `migrate-20261109-bots-core.sql` (колонки `bots`, таблицы `bot_command_lists`, `bot_info_texts`, `bot_menu_buttons`); действий оператора нет, см. `docs/ops/bots.md`.
- **Тесты.** Модульные: `pkg/botstore` (токены, порядок областей, проверки), `pkg/botprivacy`, `pkg/botmarkup`, `forBot` (sync). Интеграционный: `TestStoreOnMySQL` (`pkg/botstore`). E2E: `TestBotAccount`, `TestBotPrivacyInGroup`. Проверка, что тест падает без изменения: `TestVisible` падает при разрешении всех сообщений (проверено).
- **Проверка на ветке:** `go build ./...`, `go vet -tags integration,e2e -unreachable=false` затронутых пакетов, `go test` затронутых пакетов и `./tools/...` — зелёные. Интеграционные и e2e написаны и компилируются; выполнены будут в общем прогоне партии, как `-race`, `make lint`. Проверки в клиентах нет.

**Исправлено при проверке ветки:**
- нажатие кнопки в личке и базовой группе всегда падало: вызывался незаполненный `message.getPeerUserMessageId`, теперь `message.getPeerUserMessage`;
- `updates.getChannelDifference` отдавал боту всю супергруппу, а фильтр пуша сам создаёт разрыв pts, из-за которого клиент этот запрос и шлёт; теперь разница фильтруется тем же правилом, pts остаётся;
- сообщения других ботов больше не приходят боту в группах ни в каком режиме;
- упоминание в тексте в супергруппе больше не считается ответом боту, как и в базовой группе;
- `messages.createChat` с перевёрнутым условием `bot_nochats` пропускал обычных ботов и добавлял запрещённых;
- `inputPeerEmpty` в `messages.getInlineBotResults` давал `PEER_ID_INVALID`;
- в `updateBotCallbackQuery` из канала не было самого канала в `chats`;
- ошибка чтения Redis в момент окончания ожидания давала 500 вместо `BOT_RESPONSE_TIMEOUT`;
- `pluma-admin bot token` теперь завершает уже вошедшие сессии бота.

## Решения на подтверждение
- Общая команда без `@имя` приходит всем ботам группы, а не только последнему писавшему (документация: только ему). Нужно состояние на чат; надмножество сознательно.
- `messages.startBot`: пользователь видит в чате ту же строку, что получает бот (`/start abc`). В настоящем Telegram, скорее всего, пользователь видит только `/start` (не проверено, из памяти).
- Ожидание ответа бота — 10 с (Telegram срока не называет).
- `inputUserEmpty` в `bots.setBotMenuButton`/`getBotMenuButton` считается кнопкой по умолчанию для всех пользователей: страницы Telegram этого не говорят (не проверено).
- Результат инлайна с контентом по URL и без своего фото/документа (`inputBotInlineMessageMediaAuto`) отклоняется `SEND_MESSAGE_MEDIA_INVALID`: сервер не качает URL. Статьи с текстом работают; медиа — через загруженные на сервер файлы (`inputBotInlineResultPhoto`/`Document`).
- `hide_via` не поддерживается: подпись «через @бот» остаётся всегда.

## Не сделано (последующие задачи)
- HTTP Bot API, мини-приложения, меню вложений, игры, инвойсы, `messages.editInlineBotMessage`, `updateBotInlineSend` (отзывы), `messages.sendBotRequestedPeer`, входная кнопка авторизации (`keyboardButtonUrlAuth` отклоняется `BUTTON_TYPE_INVALID`).
- `updateBotCommands` и `updateBotMenuButton` не рассылаются: клиенты обновляют сведения по `bot_info_version`.
- `c.MD.IsBot` по-прежнему не выставляется в сессии, поэтому запрет методов «только для пользователей» боту не действует.
- Бот может написать пользователю, который его не запускал.
- Пока `c.MD.IsBot` не выставлен, `messages.getHistory` и `channels.getMessages` отдают боту всю супергруппу: режим конфиденциальности соблюдается только в пушах и в `updates.getChannelDifference`.
- Имя бота на другом языке хранится, но видно только через `bots.getBotInfo`.
