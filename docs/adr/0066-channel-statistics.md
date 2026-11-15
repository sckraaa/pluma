# 0066. Статистика каналов, групп, постов, историй и опросов: считаем на запросе из своих таблиц, события пишем с момента развёртывания

- Статус: предложено (2026-11-15)
- Задача: 103 (ветка `task/103-channel-stats`). Обслуживаются `stats.getBroadcastStats`, `stats.getMegagroupStats`, `stats.loadAsyncGraph`, `stats.getMessageStats`, `stats.getMessagePublicForwards#5f150144`, `stats.getStoryStats`, `stats.getStoryPublicForwards`, `stats.getPollStats`; флаги `channelFull.can_view_stats`, `channelFull.stats_dc`, `pollResults.can_view_stats`.
- Связано: ADR 0041 (всё Premium открыто всем: статистика без порога подписчиков), ADR 0046 (свои строки на 37 языках), ADR 0048 (призраки не видны в счётчиках), ADR 0054 (истории), ADR 0057 и 0061 (содержимое пользователей не покидает домашний сервер), задача 046 (просмотры и пересылки постов), задача 098 (исправление заглушения каналов — от него зависят события заглушения).

## Контекст

### Документация (прочитано 2026-11-15)
- https://core.telegram.org/api/stats: статистика — для админов каналов и супергрупп «выше порога», порог не назван, доступность — флаг `channelFull.can_view_stats`; все запросы — в DC из `channelFull.stats_dc`. Опрос: статистику видит отправитель при `pollResults.can_view_stats`. Длина периода «зависит от активности канала». `views_per_post`/`shares_per_post` — сумма за посты периода, делённая на число постов; `previous` — такой же отрезок перед `min_date`. Формат графика: `columns` (первый элемент — метка), `types` (`line`/`area`/`step`/`bar`/`x`, x — миллисекунды Unix), `colors` (`"ключ#RRGGBB"` или hex), `names` (локализованные), `subchart {show, defaultZoom}`, `y_scaled` (две оси, «только step»), `percentage` (с `area`), `stacked` (всегда с `bar`); до 50 столбцов, у всех столбцов один тип; ключи цветов `red`, `lightblue`, `lightgreen`, `golden`, `green`, `orange`, `blue`, `indigo`; `dark` выбирает палитру hex. `statsGraphAsync` — отложенный график, `zoom_token` + `x` — приближение; `percentage`-график при приближении становится круговой диаграммой.
- Страницы методов: `stats.loadAsyncGraph` — `GRAPH_EXPIRED_RELOAD`, `GRAPH_INVALID_RELOAD`, `GRAPH_OUTDATED_RELOAD`; `stats.getBroadcastStats` — `BROADCAST_REQUIRED`, `CHANNEL_INVALID`, `CHANNEL_PRIVATE`, `CHAT_ADMIN_REQUIRED` (403); `stats.getMegagroupStats` — `CHANNEL_INVALID`, `CHAT_ADMIN_REQUIRED`, `MEGAGROUP_REQUIRED`; `stats.getMessagePublicForwards` — `CHANNEL_INVALID`, `CHAT_ADMIN_REQUIRED`, `MESSAGE_ID_INVALID`, `PEER_ID_INVALID`, пустой `offset` в первом вызове, дальше `next_offset`, его отсутствие — конец; `stats.getStoryStats` — `CHANNEL_INVALID`, `PEER_ID_INVALID`, `STORIES_NEVER_CREATED`; `stats.getPollStats` — `PEER_ID_INVALID`.
- Схема слоя 233: `api.tl:1398-1447`, `:1805-1813`, `:2210`, методы `:3172-3179` (tdesktop `Telegram/SourceFiles/mtproto/scheme/api.tl`). В `chatAdminRights` (`:1304`) отдельного права «статистика» нет.

### Кто видит статистику
- tdlib (`tdlib-td/td/generate/scheme/td_api.tl:839`): право `can_post_messages` — «создавать посты канала **или смотреть статистику канала**»; `:843`: `can_restrict_members` — «ограничивать участников **или смотреть статистику супергруппы**». Это и есть «право статистики»: в канале — создатель или админ с `post_messages`, в супергруппе — создатель или админ с `ban_users`.

### Клиенты (`~/pluma-work/reference-src-new`, закреплённые выпуски)
- **Где смотрят флаг.** Android открывает экран и строку профиля только при `can_view_stats` (`ui/StatisticActivity.java:133`, `ui/ProfileActivity.java:12348`), статистику истории — тоже (`ui/Stories/PeerStoriesView.java:1372`), статистику поста — из окна пересылки при `forwards > 0` и правах админа (`ui/Components/ShareAlert.java:1423-1431`). tdesktop: меню канала, статистика истории — по `can_view_stats` (`window/window_peer_menu.cpp:1314-1333`, `media/view/media_view_overlay_widget.cpp:2515-2525`); статистика поста — по `can_view_stats` **или** праву писать посты при 10+ просмотрах (`history/view/history_view_context_menu.cpp:1353-1378`). Web K требует и `stats_dc`, и `can_view_stats` (`lib/appManagers/appProfileManager.ts:1444-1455`). Опрос: Android — `pollResults.can_view_stats` и не пересланное (`ui/ChatActivity.java:46661-46669`), tdesktop — `canViewStats()` (`history/view/history_view_context_menu.cpp:2405`, `data/data_poll.cpp:206-207`: `min`-итоги флаг не снимают), Web K — `components/chat/bubbleParts/pollMessageContent/pollStatistics.ts:4-10`.
- **`stats_dc`.** iOS шлёт запрос через загрузочное соединение, если `stats_dc` отличается от текущего DC, и по умолчанию держит 0 (`TelegramCore/Sources/Statistics/PeerStatistics.swift:318-319`, `SyncCore/SyncCore_CachedChannelData.swift:314`); Android шлёт и приближение прямо в `chat.stats_dc` (`ui/StatisticActivity.java:433`, `:1711`); tdesktop игнорирует значение, равное главному DC (`data/data_session.cpp:5966-5977`); Web K без `stats_dc` статистики не покажет. Наш DC — 1 (`teamgramd/bin/config.json:16` `this_dc`, `app/bff/configuration/internal/core/help.getNearestDc_handler.go:36`).
- **Разбор JSON.**
  - iOS отвергает график без `columns`, `types`, `names`, `colors`, без цвета и имени у столбца, с длиной столбца, не равной длине `x` (`GraphCore/Sources/Charts Reader/ChartsCollection.swift:40-95`); `x` меньше 24 считает секундами, иначе миллисекундами (`:68-77`); именованные цвета переводит в свои (`GraphCore/Sources/Helpers/UIColor+Utils.swift:30-75`). Длительность шага графика поста iOS ищет подстрокой `["x",` в сыром JSON (`TelegramCore/Sources/Statistics/MessageStatistics.swift:101-116`): JSON — компактный, `x` — первый столбец и не последний. Флаг `dark` iOS ставит не в тот бит (`PeerStatistics.swift:313`, `1 << 1` вместо `1 << 0`), так что от iOS `dark` не приходит. iOS повторяет запрос статистики при любой ошибке бесконечно (`retryRequest`, `Network/Network.swift:1508-1511`).
  - Android читает `x` внутри цикла по столбцам и обращается к `x.length` на каждом шаге — `x` должен идти первым (`ui/Charts/data/ChartData.java:47-77`); `colors.getString`/`names.getString` без ключа — `JSONException`, график пропадает (`:90-108`); цвет — по `(.*)(#.*)`, ключ — `statisticChartLine_<ключ>` (`:90-100`, ключи `ui/ActionBar/ThemeColors.java:1708-1717`); `x` короче 2 точек — «нет данных» (`ui/StatisticActivity.java:1015-1016`). **Двухосевой график** (`DoubleLinearChartData.java:34`) делит `long` максимум на максимум столбца: столбец из одних нулей рядом с ненулевым — `ArithmeticException` в обратном вызове сети. Языки — `StackLinearChartData` берёт `lines.get(0)` (`:49`): график без столбцов y ломает экран. Час в «лучших часах» Android пишет как `%02d:00` от значения `x` (`ui/Charts/view_data/LegendSignatureView.java:163-170`).
  - tdesktop: без `columns` — пусто; несовпадение длин — пусто; `defaultZoom` ищет точное совпадение с `x`; `xTooltipFormatter` с `'week'` включает недельные подписи; `-` в имени заменяет на тире (`statistics/statistics_data_deserialize.cpp:22-176`). Отложенный график он грузит как приближение с `x = 0` без флага, а сбой приближения стопорит очередь приближений (`api/api_statistics.cpp:246-276`, `info/statistics/info_statistics_inner_widget.cpp:147-196`).
  - Web K берёт цвет с `#` (`components/sidebarRight/tabs/statistics.tsx:130-132`), у графика языков меняет `bar` на `area` (`:370-383`), строки форматтеров сопоставляет таблице: `statsFormat('day'|'week'|'hour'|'5min')`, `statsTooltipFormat(…)`, `statsFormatDayHour`, `statsFormatDayHourFull`, `statsFormatTooltipValue`, `null`; неизвестная строка — сырое число (`lib/tchart/utils.ts:184-213`). Значение `x` «лучших часов» — номер часа, который он сдвигает на свой часовой пояс (`lib/tchart/format.ts:13-19`). `statsGraphError` опроса он показывает текстом (`statistics.tsx:888-910`).
- **Тип графика выбирает клиент по полю**, а не по `types`: Android — `StatisticActivity.java:293-304`, `:374-388`; iOS — `StatisticsUI/Sources/ChannelStatsController.swift:1344-1401`, `GroupStatsController.swift:493-528`, `MessageStatsController.swift:254-279`; tdesktop — `info/statistics/info_statistics_inner_widget.cpp:198-296`.
- **`statsGraphError` скрывает график** во всех клиентах: Android (`StatisticActivity.java:1002`), iOS (`PeerStatistics.swift:42-51`), Web K (`statistics.tsx:907-910`), tdesktop (пустой `chart`, `info_statistics_inner_widget.cpp:150-160`). Для опроса Android показывает «статистика появится позже» (`ui/ChatActivity.java:34959-34963`).

### Сервер сегодня
- `RPCStatistics` не маршрутизирован (`#"/mtproto.RPCStatistics"` в `teamgramd/etc/session.yaml:131`, `teamgramd/etc2/session.yaml:131`, `app/interface/session/etc/session.yaml:111`), `can_view_stats`/`stats_dc` не ставятся (`app/bff/channels/internal/core/channels.getFullChannel_handler.go:69-87`), поэтому интерфейс статистики скрыт и шума нет.
- Есть сырьё с датами: `channel_message_views`, `channel_message_forwards` (`migrate-20261108-channel-views.sql:36-51`), `channel_message_reactions.date` (`migrate-20261107-channel-reactions.sql`), `story_views`, `story_shares`, `stories.fwd_*` (`migrate-20261114-stories.sql`), `poll_votes.date` (`migrate-20261108-polls-checklists.sql:45-53`), `channel_messages.date2`, `message_filter_type`, `message`, `channel_participants.date`/`inviter_user_id`, `auths.lang_code`.
- Нет: истории вступлений и выходов (строки участников перезаписываются), заглушений, источника просмотра, адресата пересылки; журнал действий админов хранится 48 часов (`pkg/adminlog/adminlog.go:30`), так что «лучших админов» по нему не посчитать; отметки о прочтении супергрупп чистятся (`migrate-20261111-channel-read-marks.sql:1-3`).

## Решение

### 1. Доступ
- `can_view_stats` — создатель (присутствующий) или админ с `post_messages` в канале, с `ban_users` в супергруппе; без порога подписчиков (владелец, ADR 0041). Монофорум — нет (tdesktop его пропускает, `window_peer_menu.cpp:1316`).
- `stats_dc` = `this_dc` из конфигурации (1), ставится вместе с `can_view_stats`.
- Те же правила проверяют все методы. Не админ — `CHAT_ADMIN_REQUIRED` (403); канал вместо супергруппы — `MEGAGROUP_REQUIRED`, супергруппа вместо канала — `BROADCAST_REQUIRED`; не читаемый — `CHANNEL_PRIVATE`.
- Истории: только истории каналов и супергрупп при тех же правах (Android, tdesktop открывают статистику историй только каналов); история пользователя — `PEER_ID_INVALID`.
- Опрос: `pollResults.can_view_stats` — для автора опроса (`polls.creator_user_id`), не для пересланных копий; `stats.getPollStats` — автору и тем, кто видит статистику канала, где опрос опубликован.

### 2. Считаем на запросе, без отложенных графиков
- Каждый ответ строится синхронно из своих таблиц запросами по индексам `(канал, дата)`. При ~100 людях и истории до года это десятки миллисекунд на график; срок запроса — 10 с; одинаковые одновременные запросы схлопываются (`singleflight` по ключу «метод, пир, язык, dark»).
- `statsGraphAsync` не выдаём: он нужен Telegram для разгрузки, нам — нет, а tdesktop и так грузит его через путь приближения.
- `zoom_token` — у дневных графиков, где есть часовые данные. Токен подписан HMAC-SHA256 ключом `STATS_GRAPH_TOKEN_KEY`: версия, вид графика, пир, объект (пост, история, опрос), id зрителя, язык, `dark`, срок (24 ч). Чужой, подделанный или устаревший токен — `GRAPH_INVALID_RELOAD`/`GRAPH_EXPIRED_RELOAD`; доступ при приближении проверяется заново. Приближение отвечает почасовыми данными семи дней с выбранным в середине (окно не проверено, см. «Не проверено» задачи) и никогда не отвечает `statsGraphError`: Android показывает его `Toast` из обратного вызова сети, tdesktop стопорит очередь.

### 3. События с момента развёртывания
- Новые таблицы (`migrate-20261119-channel-stats.sql`), каждая пишется **в той же транзакции**, что и само изменение, — повтор транзакции (`RunBox`) пишет заново, без двойного счёта:
  - `channel_stats_member_events` — вступление и выход с источником (ссылка, папка, заявка, публичный вход по имени или поиску, добавлен участником, прочее) и пригласившим;
  - `channel_stats_mute_events` — смена действующего заглушения канала участником (с `mute_until`, чтобы истёкшее временное заглушение считалось включением звука в свой момент);
  - `channel_message_views.source` — откуда просмотр (подписчик, другой канал, группа, личный чат, прочее);
  - `channel_stats_public_forwards` — пересылки постов и историй в каналы и супергруппы (адресат и его сообщение);
  - `channel_stats_readers` — кто читал супергруппу в какой день (для «зрителей»);
  - `channel_stats_admin_actions` — удаления, исключения и ограничения по админам и дням;
  - `stats_collection` — с какого момента собирается каждый вид событий (ставит миграция один раз, `INSERT IGNORE`).
- **История событийных графиков начинается с развёртывания.** Видимое поведение: такой график начинается с даты начала сбора (видна на оси x и в начале выделенного отрезка), а пока у него меньше двух дневных точек, вместо него — `statsGraphError` с локализованным текстом «Данные собираются с <дата>», и клиенты его не показывают. Графики из уже имевшихся таблиц (просмотры, пересылки, реакции, сообщения групп, истории, опросы) показывают всю свою историю.

### 4. Контракт JSON (пакет `pkg/statsgraph`)
- Компактный JSON (`encoding/json` структуры с порядком полей), `columns` — первым ключом, столбец `x` — первым и не последним; длины всех столбцов равны; хотя бы один столбец y и хотя бы 2 точки, иначе — `statsGraphError`; не больше 50 столбцов (языки и реакции — топ и «прочее»).
- У каждого столбца есть `types`, `names`, `colors`. Цвета — `"ключ#RRGGBB"` только из ключей, общих для документации, Android и iOS (`red`, `green`, `blue`, `golden`, `lightgreen`, `lightblue`, `orange`, `indigo`); девятый и дальше — голый `#RRGGBB`. Две палитры hex — светлая и тёмная (`dark`).
- Форматтеры — строками из таблицы Web K: дни — `statsFormat('day')`/`statsTooltipFormat('day')`, часы — `'hour'`, пять минут — `'5min'`, недели — `'week'` (tdesktop узнаёт недели по `'week'`), «лучшие часы» — `statsFormatDayHour`/`statsFormatDayHourFull`, всегда `yTickFormatter: "null"`, `yTooltipFormatter: "statsFormatTooltipValue"`, `xRangeFormatter: "null"` при показанном `subchart`.
- **Двухосевые графики** (взаимодействия постов и историй, IV, действия в группе, график поста и истории) не содержат столбца из одних нулей рядом с ненулевым: такой столбец выбрасывается (Android падает на делении на ноль). Если нулевые все — график остаётся как есть.
- Дни — по UTC; точка дня ставится на **полдень UTC**, чтобы дата под точкой совпадала с местной от UTC−11 до UTC+11 (аудитория — Европа, Россия, СНГ и Америка; полночь UTC в Америке — предыдущий вечер). «Лучшие часы» — номер часа 0–23 по UTC: Web K переводит его в местное время, Android пишет как есть.

### 5. Имена — через серверный пакет строк
- `names` и тексты `statsGraphError` — по ключам `pkg/langpack/stats.json` (37 языков, как `mail.json`, ADR 0046) на языке `lang_code` сессии зрителя; имена языков в графике языков — там же. Ответы того же пира на разных языках строятся отдельно. Имена вариантов опроса и эмодзи реакций — как есть (это содержимое, оно показывается только автору и админам на этом же сервере).

### 6. Источники и составы графиков
- Таблица графиков, их источники и SQL — в брифе 103, раздел «Графики». Решения, которых нет в документации:
  - источники просмотров: подписчики, каналы, группы, личные чаты, прочее (не подписчик открыл пост);
  - источники вступлений: ссылка-приглашение, папка, заявка, публичный вход (имя или поиск), добавлен участником, прочее;
  - «реакции по эмоциям»: три ряда — положительные, отрицательные, прочие — по закреплённой в коде таблице стандартных реакций; свои эмодзи — «прочие»;
  - «действия» группы: сообщения и зрители по дням; «зрители» группы — разные участники, читавшие или писавшие в группу за период;
  - период обзора — последние 30 дней, `previous` — 30 дней перед ними; дневные графики — до 365 дней, `defaultZoom` — последние 30.

### 7. Приватность и хранение
- Наружу ничего не уходит: только агрегаты зрителю с правами на этом сервере (ADR 0057, 0061). В событиях хранятся id пользователей — без них не посчитать «разных» людей; тексты не копируются. Призраки (ADR 0048) не пишутся и не считаются.
- Объём: на ~100 людей — тысячи строк в год; хранится бессрочно, кроме `channel_stats_readers` — 400 дней (чистка раз в сутки).

## Последствия
- Статистика появляется во всех четырёх клиентах без патчей клиентов.
- Пять путей записи получают по одной строке в той же транзакции: участие в каналах (`app/service/biz/channel`), заглушения (`app/service/biz/user`), просмотры, отправка сообщений каналов (`app/messenger/msg`), чтение и удаление.
- Новая переменная окружения `STATS_GRAPH_TOKEN_KEY` (без неё `stats.*` отключены, `can_view_stats` не ставится).
- Новые индексы на больших таблицах (`channel_messages`, `channel_message_views`, `channel_message_reactions`, `channel_message_forwards`, `story_views`, `stories`) — онлайн-DDL MySQL 8, разово при миграции.
- Первые недели после развёртывания событийные графики короткие или скрыты.

## Что не делается
- Доходы, звёзды, реклама, бусты (решение владельца): `stats.getBroadcastRevenue*`, `can_view_revenue`, графики монетизации.
- `iv_interactions_graph` — всегда `statsGraphError` (Instant View нет).
- `statsGraphAsync`.
- Старый `stats.getMessagePublicForwards#5630281b` — остаётся `ErrEnterpriseIsBlocked` (клиенты слоя 233 зовут `#5f150144`).
- Восстановление событий до развёртывания по косвенным данным (даты вступления нынешних участников и т. п.): это была бы выдумка.
- Хранение метрик чтения `messages.reportReadMetrics` (отдельное решение о приватности).

## Рассмотренные альтернативы
- **Ежедневные сводные таблицы, собираемые фоновым заданием.** Быстрее на огромных каналах, но сложнее (пересчёт после удалений, после рестарта, часовые пояса) и не нужно на нашей нагрузке; индексы `(канал, дата)` достаточны. Можно добавить позже без изменения контракта.
- **Токены приближения в Redis.** Не нужен новый секрет, но нужна запись на каждый ответ и чистка; HMAC без состояния проще и переживает рестарт Redis.
- **Ряды реакций по каждому эмодзи.** Честно и без таблицы настроений, но не «по эмоциям», как названо поле; оставлено запасным вариантом (решение к подтверждению).
- **Точка дня в полночь UTC.** Привычно, но в Америке подписи дат съезжают на день назад.
