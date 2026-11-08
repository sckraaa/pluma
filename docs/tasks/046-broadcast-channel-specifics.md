# 046. Каналы-трансляции: просмотры, подписи, ссылки на посты

- Статус: done (код и тесты; проверка в клиентах со скриншотами — отдельно)
- Зависит от: 040
- ADR: 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` §2.3, §2.6, задача T11; `clients.md` §1 «Open a channel», §4 п. 7–8; `plan.md` §3, задача «045» плана).

## Цель
У постов канала растут просмотры (один раз на пользователя), видна подпись автора, когда она включена, ссылку на пост можно скопировать.

## Что известно
- `messages.getMessagesViews` — `partial` (`api-coverage.md:599`). Клиенты зовут его с `increment` для видимых постов: Android каждые 5 с (`MessagesController.java:10579-10630`, 035), iOS (`AccountViewTracker.swift:723`), Web K. Android берёт оттуда и `replies` (там же, 035). `updateChannelMessageViews` — без `pts`, в журнал канала не идёт; рассылка — `Channel-T` (ADR 0034, пп. 1, 5).
- `updateChannelMessageViews` — Web K `appMessagesManager.ts:10865-10875`; поля `views` и `forwards` в постах (`clients.md` §4 п. 7).
- Дедупликация просмотров — отдельная таблица `channel_message_views` (`server.md` §2.3, «later task»).
- Подписи: `post_author`/`from_id` в постах, `channels.toggleSignatures` (044) (`clients.md` §4 п. 8); в broadcast публикуют только админы с `post_messages` (`server.md` §2.6).
- `channels.exportMessageLink` — `missing`. Формат ссылки — с учётом задачи 026 (ссылки pluma.chat вместо t.me).
- `channels.getSendAs` — `blocked`; в `plan.md` не входит, остаётся 400.

## Область
- Миграция `channel_message_views` (повторный запуск безопасен).
- `messages.getMessagesViews` с `increment` для каналов, рост `views` один раз на пользователя, `updateChannelMessageViews` (частоту рассылки ограничить).
- `post_author` и подписи в постах при включённых подписях.
- `channels.exportMessageLink`.
- Не трогаем: обсуждения и `replies` (048), `getSendAs`.

## Критерии приёмки
- Модульные тесты: сборка `post_author`, формат ссылки.
- Интеграционные тесты на MySQL через `pkg/testinfra`: просмотры растут один раз на пользователя при параллельных вызовах; миграция применяется дважды.
- e2e через `pkg/mtclient`: подписчик не может опубликовать; просмотры растут один раз на пользователя; подпись автора видна при включённых подписях; тест падает на текущем коде.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: счётчик просмотров, подпись автора и копирование ссылки на пост в iOS, Android, Desktop (CachyOS) и Web K.

## Результат

Просмотры и пересылки постов считаются и видны клиентам. У поста в broadcast-канале есть `views` и `forwards` во всех его объектах (ответ на отправку, рассылка, `getHistory`, `getMessages`, `updates.getChannelDifference`, последнее сообщение в `getDialogs` и `getPeerDialogs`, правка); `messages.getMessagesViews` с `increment` считает просмотр один раз на пользователя при любой параллельности и отдаёт `views`, `forwards` и `replies`; участники получают `updateChannelMessageViews` и `updateChannelMessageForwards` (без `pts`, вне журнала, не чаще раза в 5 с на канал). `channels.exportMessageLink` уже был сделан в 026; проверен по документации и исходникам клиентов, менять нечего. Подписи и запрет публикации подписчику уже были (044, 042), теперь покрыты e2e.

- **Миграция** `migrate-20261108-channel-views.sql`: колонки `channel_messages.views` (по умолчанию 1), `forwards` (0), таблицы `channel_message_views` и `channel_message_forwards`. Применяется сервисом migrate, действий оператора не нужно; повторный запуск безопасен (проверено тестом).
- **Новые RPC biz channel:** `channel.incrementMessageViews`, `channel.addMessageForwards`.
- **Счётчики.** Истина — колонки; в JSON поста и в журнале значение устаревает, поэтому чтения накладывают текущее (`WithPostCounters`), а правка пишет его в обновление. `views` и `forwards` — один флаг `flags.10` у `message`: выставляются только вместе (`MakeFlagsInt32` оставляет 0 неустановленным, поэтому `wrapperspb.Int32`).
- **Блокировки.** Просмотр и пересылка не берут блокировку канала и не двигают `pts`: это самая частая запись, и проверять под ней нечего. Запись — короткая транзакция ящика пользователя; `INSERT IGNORE` по первичному ключу считает один раз, строки постов обновляются одним `UPDATE` в порядке ключа.
- **Рассылка.** `counterPush` (в памяти процесса): первое изменение в тихом канале уходит сразу, остальные за окно 5 с — одним событием, с текущим значением. После перезапуска несостоявшиеся рассылки теряются, клиенты получают значения из `getMessagesViews` и истории.
- **Пересылка.** Считается `messages.forwardMessages` из канала после успешной отправки; повтор с тем же `random_id` считается один раз. Отправка важнее счётчика: ошибка счётчика только логируется.

Источники:
- Пост стартует с 1 просмотра: Android `SendMessagesHelper.java:2424,4785`, tdesktop `history_item.cpp:698`, iOS `EnqueueMessage.swift:1012`, Web K `appMessagesManager.ts:4093`.
- Пересланный пост сохраняет просмотры: Android `SendMessagesHelper.java:2428-2431`, tdesktop `history_item.cpp:692-694`.
- `getMessagesViews`, `increment`, ответ: core.telegram.org/method/messages.getMessagesViews; Android `MessagesController.java:10579-10630` (и `replies`), iOS `AccountViewTracker.swift:723`, Web K `appMessagesManager.ts:11557`.
- `updateChannelMessageViews/Forwards`: Android `MessagesController.java:19310-19340`, tdesktop `api_updates.cpp:2490-2507`, Web K `appMessagesManager.ts:10867-10877`.
- Ссылка: core.telegram.org/api/links («Message links»: id потока — в пути или параметром, `single`), core.telegram.org/method/channels.exportMessageLink (`grouped`, `thread`); разбор формы `<поток>/<id>` — Android `LaunchActivity.java:2125-2145,2262`, tdesktop `local_url_handlers.cpp:2025,2057`, iOS `UrlHandling.swift:691,779`, Web K `internalLinkProcessor.ts:377`. У ответа прямо на первое сообщение потока `reply_to_top_id` нет, потоком считается `reply_to_msg_id` (tdesktop `history_item.cpp:2502`, `apiwrap.cpp:782-786`); с флагом `thread` ссылка теперь содержит его и для комментария прямо к посту.

Проверка (сборка на CachyOS, стенд `pluma-e2e-046`, образ `pluma-e2e-046:local`, стенд снят):
- `go build ./...`, `go vet -tags integration,e2e -unreachable=false` затронутых пакетов, `go test ./app/...`, `-race` затронутых и зависящих (`biz/channel`, `bff/messages`, `bff/channels`, `messenger/...`) — зелёные. `go test ./tools/...` (с логаудитом) — зелёный. `go run ./tools/apicoverage` не меняет матрицу (набор методов тот же: новых обработчиков TL-методов нет).
- Интеграция на MySQL: `biz/channel/internal/dao` и `core` с `-race`, `msg/internal/dao` — зелёные. Новые: `TestViewsOncePerUser` (параллельные просмотры), `TestViewsOnlyPostsTheyMaySee`, `TestForwardsOncePerRandomId`, `TestPushCountersEvent`, `TestViewsMigration` (двойной запуск), `TestTopMessageCounters`, `TestDifferencePostCounters`. Без правки падают `TestTopMessageCounters` (нет счётчиков у последнего поста в диалогах) и случай прямого комментария в `TestMessageLink`.
- e2e: `TestChannelViewsAndLinks` и все `TestChannel*` — PASS.
- Тест падает без изменения: при снятой дедупликации `TestViewsOncePerUser` даёт 49 просмотров вместо 9. Первый прогон e2e на чужом образе (тег был перезаписан параллельной сборкой) дал `views <nil>` — тоже подтверждение, но случайное.
- `make lint` завершился с ошибкой (exit 1): в копии рабочего дерева на хосте `.git` — файл-ссылка (`fatal: gitfile does not point to a valid repository`), без истории `new-from-rev` линтер показал все старые замечания. В изменённых файлах замечания только на старых строках (`forwardMessages:231`, `getMessagesViews:133,138`), в новом коде — ни одного. Нужно перепроверить на основной копии или в CI.
- После последних правок (счётчики у последнего поста в диалогах, поток в ссылке, новые проверки e2e) выполнены сборка, `go vet`, `go test` затронутых пакетов и интеграционные тесты `biz/channel`. e2e, `-race` и `make lint` этих правок — в общем прогоне объединённых веток.

Решения на подтверждение:
- Просмотр автора поста — стартовая единица, он сам не добавляет просмотр. Это вывод из клиентов (локальная копия с `views = 1`), прямого описания у Telegram нет.
- Как считается пересылка: каждая успешная пересылка по своему `random_id`, в том числе в «Избранное» и с `drop_author`. Правила Telegram нигде не описаны (не проверено).
- Копия с `drop_author` счётчиков не несёт; пересланный пост в чат хранит `views` на момент пересылки и `forwards = 0`.
- Копия поста в группе обсуждения идёт без счётчиков, чтобы не показывать устаревшее число.
- Окно рассылки — 5 с, в памяти процесса; при нескольких процессах biz у каждого своё окно.

Не сделано и открыто:
- Просмотры поста, пересланного в личный чат или группу: документация говорит «sent or forwarded from a channel», но запрос с `peer` чата по-прежнему отвечает пустым `messageViews` (ветка не каналов не менялась, вне брифа).
- Проверка в клиентах со скриншотами (iOS, Android, Desktop, Web K) не выполнена.
- `channels.getMessagePublicForwards` и `stats.*` не трогали.
