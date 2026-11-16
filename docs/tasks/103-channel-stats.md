# 103. Статистика каналов, групп, постов, историй и опросов

- Статус: готово, ждёт выкладки (2026-11-16)
- Решение: ADR 0066.
- Зависит от: 046 (просмотры и пересылки постов), 044 (реакции каналов), 084-stories / ADR 0054 (истории), опросы (`pkg/polls`), 076 / ADR 0046 (37 языков строк), **098** (исправление `account.updateNotifySettings` для каналов: без него заглушений нет и графику заглушений нечего считать), 078 (имена каналов: «публичный» канал или группа — с активным именем, `app/service/biz/channel/internal/dao/look.go:198-360`).
- Ветка: `task/103-channel-stats`.

## Цель
Админ канала или супергруппы с правом статистики видит во всех четырёх клиентах экран статистики, как в Telegram: обзор, графики роста, подписок, заглушений, лучших часов, просмотров и их источников, вступлений и их источников, языков, взаимодействий и реакций постов и историй, а также статистику отдельного поста, истории и опроса и список публичных пересылок. Без порога подписчиков. Ничего не уходит с домашнего сервера; патчей клиентов нет.

## Область
В задаче:
- Все 8 методов `stats.*` слоя 233: `getBroadcastStats`, `getMegagroupStats`, `loadAsyncGraph`, `getMessageStats`, `getMessagePublicForwards#5f150144`, `getStoryStats`, `getStoryPublicForwards`, `getPollStats`.
- Маршрут `"/mtproto.RPCStatistics": "bff.bff"` в трёх `session.yaml`, регистрация в `app/bff/bff/internal/server/server.go`.
- `channelFull.can_view_stats` и `stats_dc`, `pollResults.can_view_stats`.
- Запись событий, которых нет: вступления и выходы с источником, заглушения, источник просмотра (и учёт просмотров пересланных копий в каналах и супергруппах), индекс публичных пересылок постов и историй, читатели супергрупп по дням, действия админов по дням.
- Пакет графиков `pkg/statsgraph` (JSON, палитры, форматтеры, корзины по времени, HMAC-токены приближения), строки `pkg/langpack/stats.json` на 37 языках.
- Миграция с индексами, конфигурация, метрики, `docs/ops`, `TESTING-BACKLOG`.

Не в задаче (ADR 0066, «Что не делается»): доходы, звёзды, бусты, реклама; `iv_interactions_graph` (всегда `statsGraphError`); `statsGraphAsync`; старый `getMessagePublicForwards#5630281b` (остаётся `ErrEnterpriseIsBlocked`); восстановление событий до развёртывания; хранение `messages.reportReadMetrics`.

## Контракт клиентов
Источники — `~/pluma-work/reference-src-new` и core.telegram.org (прочитано 2026-11-15); подробно, с номерами строк — ADR 0066, «Контекст». Главное для реализации:
- **Кто видит.** tdlib: `can_post_messages` даёт статистику канала, `can_restrict_members` — супергруппы (`tdlib-td/td/generate/scheme/td_api.tl:839`, `:843`). Отдельного права в `chatAdminRights` нет (`api.tl:1304`).
- **Флаги, которые смотрят клиенты.** Android: `can_view_stats` — экран, профиль, истории (`ui/StatisticActivity.java:133`, `ui/ProfileActivity.java:12348`, `ui/Stories/PeerStoriesView.java:1372`); пост — из окна пересылки при `forwards > 0` (`ui/Components/ShareAlert.java:1423-1431`). tdesktop: `can_view_stats`, а для поста ещё «может писать посты и 10+ просмотров» (`history/view/history_view_context_menu.cpp:1353-1378`). Web K: `stats_dc` **и** `can_view_stats` (`lib/appManagers/appProfileManager.ts:1444-1455`). Опрос: `pollResults.can_view_stats` (Android `ui/ChatActivity.java:46661-46669`, tdesktop `data/data_poll.cpp:206-207`, Web K `components/chat/bubbleParts/pollMessageContent/pollStatistics.ts:4-10`).
- **`stats_dc` = 1** (`teamgramd/bin/config.json:16`). iOS без него (0) уходит в загрузочное соединение к DC 0 (`TelegramCore/Sources/Statistics/PeerStatistics.swift:318-319`); Android шлёт туда и приближения (`ui/StatisticActivity.java:433`, `:1711`).
- **Тип графика выбирает клиент по полю** (Android `StatisticActivity.java:293-304`, `:374-388`, `MessageStatisticActivity.java:751-752`; iOS `StatisticsUI/Sources/ChannelStatsController.swift:1344-1401`, `GroupStatsController.swift:493-528`, `MessageStatsController.swift:254-279`; tdesktop `info/statistics/info_statistics_inner_widget.cpp:198-296`). Сервер обязан дать данные той формы, которую ждёт этот тип: двухосевой — два ряда; «по источникам» и реакции — сложенные столбцы; языки и дни недели — проценты.
- **Жёсткие требования к JSON** (нарушение — график пропал или клиент упал):
  - есть `columns`, `types`, `names`, `colors`, у каждого y-столбца — тип, имя и цвет (iOS `GraphCore/Sources/Charts Reader/ChartsCollection.swift:40-86`, Android `ui/Charts/data/ChartData.java:90-108`);
  - `x` — первый столбец (Android `ChartData.java:72` читает `x.length` в цикле) и не последний, JSON компактный (iOS ищет `["x",`, `MessageStatistics.swift:106-110`);
  - длины столбцов равны (iOS `ChartsCollection.swift:91-95`, tdesktop `statistics/statistics_data_deserialize.cpp:100-106`);
  - хотя бы один y-столбец (Android `StackLinearChartData.java:49`) и 2+ точки (Android `StatisticActivity.java:1015-1016`);
  - **в двухосевом графике нет столбца из одних нулей рядом с ненулевым** (Android `DoubleLinearChartData.java:34`: деление `long` на 0);
  - цвет `ключ#hex` (регэксп `(.*)(#.*)` Android `ChartData.java:90`, tdesktop `statistics_data_deserialize.cpp:166`; Web K берёт с `#`, `statistics.tsx:130-132`; iOS переводит имена `UIColor+Utils.swift:30-75`);
  - форматтеры — строками таблицы Web K (`lib/tchart/utils.ts:184-213`), иначе Web K пишет на оси сырые миллисекунды; недели tdesktop узнаёт по `'week'` в `xTooltipFormatter` (`statistics_data_deserialize.cpp:150-157`);
  - `defaultZoom` — точные значения `x` (`statistics_data_deserialize.cpp:112-140`).
- **«Лучшие часы»:** `x` — номер часа 0–23 (Web K `lib/tchart/format.ts:13-19` умножает на 3600e3 и сдвигает на пояс; Android пишет `%02d:00`, `ui/Charts/view_data/LegendSignatureView.java:163-170`; iOS `x < 24` — секунды, `ChartsCollection.swift:68-77`).
- **Шаг графика поста** iOS выводит из первых двух `x`: 3600 с — почасовой, 300 с — пятиминутный, иначе дневной (`MessageStatistics.swift:104-116`, `MessageStatsController.swift:265-277`). График опроса iOS рисует пятиминутной шкалой (`MessageStatsController.swift:254`), Android — сложенными столбцами (`ui/Components/poll/sheets/PollStatisticsBottomSheet.java:56`).
- **Ошибки.** `statsGraphError` скрывает график во всех клиентах; для опроса Android показывает «статистика появится позже» (`ui/ChatActivity.java:34959-34963`), Web K — текст ошибки (`statistics.tsx:888-910`). Ошибка RPC у iOS — бесконечный повтор (`Network/Network.swift:1508-1511`), у tdesktop сбой приближения стопорит следующие (`api/api_statistics.cpp:268-270`), у Android `statsGraphError` на приближении — `Toast` из сетевого обратного вызова (`StatisticActivity.java:1693`). Поэтому: приближение всегда отвечает графиком, RPC-ошибки — только на доступ и неверные аргументы.
- **`dark`** от iOS не приходит (ставит бит 1, `PeerStatistics.swift:313`); палитра hex важна в основном для Web K и запасного цвета Android.
- Документация методов: ошибки — ADR 0066, «Контекст»; `offset`/`next_offset` у публичных пересылок — непрозрачная строка, пустая в первом вызове.

## Дизайн

### Доступ (`app/bff/statistics/internal/core/access.go`)
- `canViewStats(ch, me)`: участник присутствует; создатель — да; админ — `ADMIN_POST_MESSAGES` в канале, `ADMIN_BAN_USERS` в супергруппе; монофорум — нет. Одна функция в `pkg/channelperm` (например `CanViewStats(ch Channel, p Participant) bool`), её же зовёт `channels.getFullChannel`.
- Ошибки: нет канала — `CHANNEL_INVALID`; не читаемый — `CHANNEL_PRIVATE`; нет права — `CHAT_ADMIN_REQUIRED` (403); `getBroadcastStats` для супергруппы — `BROADCAST_REQUIRED`, `getMegagroupStats` для канала — `MEGAGROUP_REQUIRED`; пост не найден/удалён/служебный — `MESSAGE_ID_INVALID`; история не найдена — `STORY_ID_INVALID`, у пира не было историй — `STORIES_NEVER_CREATED`; история пользователя — `PEER_ID_INVALID`; опрос не найден или нет прав — `PEER_ID_INVALID` (единственная ошибка на странице метода); `limit` вне 1..100 — приводится к 1..100.
- Флаг выключения: без `STATS_GRAPH_TOKEN_KEY` методы отвечают `ErrEnterpriseIsBlocked` (400, не повторяется клиентами), а `can_view_stats` не ставится — интерфейс скрыт, как сегодня.

### Флаги
- `channels.getFullChannel_handler.go`: `CanViewStats: channelperm.CanViewStats(...)` и `StatsDc: MakeFlagsInt32(thisDc)` при нём (`thisDc` — из конфигурации bff, тот же 1, что в `config.json`; одно место, не литерал).
- `pkg/polls` `Record.View`: `can_view_stats` при `viewer == creator_user_id` (итоги для других зрителей не меняются; `min`-итоги флаг не несут).
- Права меняются — клиенты перечитывают `getFullChannel` по `updateChannel` (как сейчас при смене прав); отдельной рассылки нет.

### Таблицы (`teamgramd/deploy/sql/migrate-20261119-channel-stats.sql`, идемпотентно через `information_schema`, как `migrate-20261108-channel-views.sql`)
```sql
CREATE TABLE IF NOT EXISTS `stats_collection` (
  `kind` varchar(32) NOT NULL,          -- member_events, mute_events, view_sources, public_forwards, readers, admin_actions
  `since` bigint(20) NOT NULL,
  PRIMARY KEY (`kind`)
);
INSERT IGNORE INTO stats_collection (kind, since) VALUES ('member_events', UNIX_TIMESTAMP()), …;

CREATE TABLE IF NOT EXISTS `channel_stats_member_events` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `channel_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `date` bigint(20) NOT NULL,
  `joined` tinyint(1) NOT NULL,         -- 1 вступил, 0 вышел/исключён
  `source` tinyint(4) NOT NULL DEFAULT 0, -- для вступлений: 1 ссылка, 2 папка, 3 заявка, 4 публичный вход, 5 добавлен, 0 прочее
  `inviter_id` bigint(20) NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `channel_date` (`channel_id`, `date`)
);

CREATE TABLE IF NOT EXISTS `channel_stats_mute_events` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `channel_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `date` bigint(20) NOT NULL,
  `mute_until` int(11) NOT NULL,        -- 0 — звук включён; >date — заглушён до; 2147483647 — навсегда
  PRIMARY KEY (`id`),
  KEY `channel_date` (`channel_id`, `date`),
  KEY `channel_user` (`channel_id`, `user_id`, `date`)
);

ALTER TABLE channel_message_views ADD COLUMN `source` tinyint(4) NOT NULL DEFAULT 0, ALGORITHM = INSTANT; -- 1 подписчик, 2 канал, 3 группа, 4 личный чат, 0 прочее
ALTER TABLE channel_message_views ADD KEY `channel_date` (`channel_id`, `date`);

CREATE TABLE IF NOT EXISTS `channel_stats_public_forwards` (
  `src_kind` tinyint(4) NOT NULL,       -- 1 пост канала, 2 история
  `src_peer_type` int(11) NOT NULL,
  `src_peer_id` bigint(20) NOT NULL,
  `src_id` int(11) NOT NULL,            -- id поста или истории
  `dst_channel_id` bigint(20) NOT NULL,
  `dst_message_id` int(11) NOT NULL,
  `date` bigint(20) NOT NULL,
  PRIMARY KEY (`src_kind`, `src_peer_type`, `src_peer_id`, `src_id`, `dst_channel_id`, `dst_message_id`),
  KEY `src_date` (`src_kind`, `src_peer_type`, `src_peer_id`, `src_id`, `date`)
);

CREATE TABLE IF NOT EXISTS `channel_stats_readers` (
  `channel_id` bigint(20) NOT NULL,
  `day` int(11) NOT NULL,               -- floor(unix / 86400), UTC
  `user_id` bigint(20) NOT NULL,
  PRIMARY KEY (`channel_id`, `day`, `user_id`)
);

CREATE TABLE IF NOT EXISTS `channel_stats_admin_actions` (
  `channel_id` bigint(20) NOT NULL,
  `day` int(11) NOT NULL,
  `admin_id` bigint(20) NOT NULL,
  `deleted` int(11) NOT NULL DEFAULT 0,
  `kicked` int(11) NOT NULL DEFAULT 0,
  `banned` int(11) NOT NULL DEFAULT 0,
  PRIMARY KEY (`channel_id`, `day`, `admin_id`)
);

-- индексы для запросов по датам (онлайн-DDL, ALGORITHM=INPLACE, LOCK=NONE)
channel_messages            ADD KEY `channel_date` (`channel_id`, `date2`)
channel_message_reactions   ADD KEY `channel_date` (`channel_id`, `date`)
channel_message_forwards    ADD KEY `channel_date` (`channel_id`, `date`)
story_views                 ADD KEY `peer_date` (`peer_type`, `peer_id`, `date`)
stories                     ADD KEY `fwd_source` (`fwd_peer_type`, `fwd_peer_id`, `fwd_story_id`)
```
- Заполнение `channel_stats_public_forwards` уже сделанными пересылками постов — в той же миграции, `INSERT IGNORE … SELECT` из `channel_messages` по `JSON_EXTRACT(message_data, '$.fwd_from…')`. Пути JSON сверить с хранимой формой (`jsonx` от `mtproto.Message`; `channel_post` — обёртка `{"value": N}`) интеграционным тестом; автоматические копии в группу обсуждения не берутся (у них `saved_from`/признак обсуждения — проверить по коду 048). Истории-репосты уже есть в `stories.fwd_*`.

### Запись событий (в той же транзакции, что изменение)
Помощник `pkg/channelstats` (`MemberJoined(tx, channelId, userId, date, source, inviterId)`, `MemberLeft`, `MuteChanged`, `Reader`, `AdminAction`, `PublicForward`) — одна точка записи и проверка «призрак не пишется» (`pkg/ghost`).
- **Участие** (`app/service/biz/channel/internal/dao`): `membership.go` `Join` (источник: ссылка — `joinedByLink`, `ViaChatlist` → папка; без ссылки — публичный вход), `Leave`; `invite.go` `writeMembers` (добавлен, `inviter_id` — добавивший); `join_requests.go` `approve` (заявка); `rights.go` исключение (`BANNED_VIEW_MESSAGES` → вышел) и возврат; `delete_channel.go` не пишет (канала больше нет). Проверить и решить по каждой: `app/messenger/msg/internal/dao/channel_send.go:517` (вставка участника при отправке), `migrate.go:154` (перевод базовой группы — участники переносятся, не вступают: не писать), `import.go:470` (импорт группы — не писать), `createChannel` (создатель — не писать), `pkg/ghost` (никогда). Тест-страж: каждое место, где меняется `participants_count`, покрыто записью или явным комментарием «не событие».
- **Действия админов:** `delete.go` (админ удалил чужие сообщения — `deleted += n`), `rights.go` (исключил — `kicked`, ограничил — `banned`). Счётчик — `INSERT … ON DUPLICATE KEY UPDATE`.
- **Читатели:** `read.go` при `channels.readHistory` супергруппы — `INSERT IGNORE` (день, пользователь); при отправке сообщения в супергруппу — тоже (`channel_send.go`).
- **Заглушения** (`app/service/biz/user/internal/dao/user_notify_settings.go`, после 098): при записи настроек пира-канала, если **действующее** состояние сменилось (учесть `mute_until = -1` → настройка пользователя по умолчанию для каналов/групп и истёкший `mute_until`), — строка с новым `mute_until`. Смена настройки по умолчанию для всех каналов событий по каналам не пишет (это не действие с каналом; решение к подтверждению).
- **Источник просмотра:** `channel.incrementMessageViews` получает поле `source` (внутренний TL `app/service/biz/channel/channel/channel.tl.proto` и codec — по образцу того, как задача 046 добавила метод). В `messages.getMessagesViews`: пир — сам канал поста → подписчик, если участник, иначе прочее; пир — личный чат → личный чат; базовая группа → группа; **другой канал или супергруппа с пересланной копией** → канал/группа: сейчас `channelMessagesViews` такие копии не переводит в оригинал (`app/bff/messages/internal/core/channel_views.go`), добавить перевод как в ветке личных чатов (`messages.getMessagesViews_handler.go:137-149`). Первая запись просмотра фиксирует источник; повторы не меняют.
- **Публичные пересылки:** в `channel_send.go` при вставке сообщения канала/супергруппы: `fwd_from.from_id` — канал и есть `channel_post` → `src_kind=1`; медиа `messageMediaStory` пира-канала → `src_kind=2`. Пишется для любого канала-адресата; «публичность» проверяется при чтении (имя может появиться или пропасть).

### Модуль `app/bff/statistics`
- Раскладка как у `app/bff/transcription`: `helper.go`, `internal/{config,svc,core,server/grpc/service}`; обработчик на метод: `stats.getBroadcastStats_handler.go` и т. д. Конфиг: `Mysql` (только чтение), `ChannelClient`/`BizServiceClient` (доступ, сообщения как их видит зритель), `StoryClient` по образцу соседей, `ThisDc`, `TokenKey`.
- Хранилище `internal/dao`: по файлу на раздел (`broadcast.go`, `megagroup.go`, `message.go`, `story.go`, `poll.go`, `forwards.go`, `members.go`), каждый запрос — с диапазоном по индексу `(канал, дата)` и `GROUP BY` корзины. Сборка графиков — `pkg/statsgraph`.
- Время: `now` один на запрос; окно дневных графиков — `[max(создание канала, now−365 д, since вида событий), сегодня]`; период обзора — последние 30 дней, `previous` — 30 дней перед ними.
- Схлопывание одинаковых запросов (`singleflight`, ключ: метод, пир, объект, язык, dark), срок 10 с, метрика длительности.
- Приближение: `loadAsyncGraph` проверяет HMAC, срок, зрителя (`MD.UserId` = из токена) и права заново; строит почасовой график 7 суток с выбранным днём в середине (обрезка по `since` и `now`); вид графика из токена.

### Пакет `pkg/statsgraph`
- `Graph{Series []Series; X []int64; Type; YScaled, Stacked, Percentage bool; Scale (Day|Hour|FiveMin|Week|HourOfDay); Subchart *Subchart}`; `Series{ID, NameKey or Name, Color, Values []int64}`; `Build(lang string, dark bool) (*mtproto.StatsGraph)` → `statsGraph` или `statsGraphError`.
- Правила ADR 0066 §4 в одном месте: порядок ключей (`columns` первым), `x` первым, проверки длин, ≥1 ряда и ≥2 точек, ≤50 столбцов, выброс нулевого ряда у `YScaled`, палитры (8 ключей + hex, светлая и тёмная), форматтеры по шкале, `subchart {show: true, defaultZoom: [x последних 30 дней]}` и `xRangeFormatter: "null"` у дневных, `yTickFormatter: "null"`, `yTooltipFormatter: "statsFormatTooltipValue"`.
- Корзины: день — `floor(t/86400)`, `x = day*86400000 + 43200000` (полдень UTC); час — `x` = начало часа в мс; 5 минут — начало пятиминутки; неделя — понедельник 12:00 UTC; час суток — 0..23.
- Токены: `Sign(Token) string` / `Verify(s, now) (Token, error)`; base64url, HMAC-SHA256, версия `v1`, сравнение за постоянное время.
- Имена: `langpack.StatsText(key, lang)` (на общем загрузчике 099 A, как `MailText`, `pkg/langpack/stats.json`), имена языков — ключи `PlumaStatsLang_<код>`; `-` в именах не ставить (tdesktop меняет на тире).

### Графики: источник и форма запроса
Обозначения: `W` — окно дней, `P` — период обзора, `C` — канал. Все запросы фильтруют `channel_id = ?` и диапазон по индексу; удалённые сообщения (`deleted = 0`) и призраки не считаются.

**`stats.broadcastStats`** (тип у клиента: Android/iOS/tdesktop)
| поле | ряды (`names`) | источник и SQL | тип JSON | приближение |
|---|---|---|---|---|
| `period` | — | `P` | — | — |
| `followers` | — | сейчас: `channels.participants_count` (без призраков); `previous`: сейчас − Σ(вступил − вышел) с `max(P.min, since)` по `member_events` | — | — |
| `views_per_post`, `shares_per_post`, `reactions_per_post` | — | посты `channel_messages` с `date2 ∈ P`: `SUM(views)/COUNT(*)`, `SUM(forwards)/COUNT(*)`; реакции — `COUNT(*) FROM channel_message_reactions WHERE (channel_id, message_id) IN постов`; `previous` — то же за предыдущие 30 дней | — | — |
| `views_per_story`, `shares_per_story`, `reactions_per_story` | — | истории `stories` пира с `date ∈ P`: `COUNT(story_views)`, `forwards_count`+`COUNT(story_shares)`, `COUNT(story_views WHERE reaction<>'')` на историю | — | — |
| `enabled_notifications` | — | `part` — участники без действующего заглушения (настройка пира, иначе настройка по умолчанию для каналов; `mute_until` в прошлом — не заглушён), `total` — участники | — | — |
| `growth_graph` | Подписчики | число на конец каждого дня с `since`: текущее − накопленная с конца сумма (вступил − вышел) по `member_events` | `line` | да (почасово) |
| `followers_graph` | Вступили, Вышли | `member_events`: `GROUP BY day, joined` | `line` | да |
| `mute_graph` | Заглушили, Включили звук | `mute_events` по дням: заглушили — переход в заглушённое; включили — переход в незаглушённое явно или истечением `mute_until` (вычислить в Go по событиям пользователей окна) | `line` | да |
| `top_hours_graph` | Этот период, Прошлый период | `channel_message_views` с `date ∈ P` и `∈ prev P`: `GROUP BY HOUR(FROM_UNIXTIME(date))` в UTC, 24 точки | `step` | нет |
| `interactions_graph` | Просмотры, Пересылки | `channel_message_views` и `channel_message_forwards` по дням (`channel_date`) | `step`, `y_scaled` | да |
| `iv_interactions_graph` | — | всегда `statsGraphError` (нет Instant View) | — | — |
| `views_by_source_graph` | Подписчики, Каналы, Группы, Личные чаты, Прочее | `channel_message_views`: `GROUP BY day, source`; строки до `since(view_sources)` — «Прочее» не выдумывается: окно этого графика начинается с `since` | `bar`, `stacked` | да |
| `new_followers_by_source_graph` | Ссылка, Папка, Заявка, Публичный вход, Добавлены, Прочее | `member_events WHERE joined=1 GROUP BY day, source` | `bar`, `stacked` | да |
| `languages_graph` | языки (топ 9 + Прочие) | участники на конец каждого дня по `member_events` (до `since` — нынешние участники с `channel_participants.date ≤ день`), язык — `lang_code` последней активной авторизации (`auths` через `auth_users`, одним запросом на всех участников), базовый код (`pt-br`→`pt`) | `area`, `percentage`, `stacked` | нет (клиент сам строит круг) |
| `reactions_by_emotion_graph` | Положительные, Отрицательные, Прочие | `channel_message_reactions` по дням, эмодзи → настроение по таблице в коде, свои эмодзи → Прочие | `bar`, `stacked` | да |
| `story_interactions_graph` | Просмотры, Пересылки | `story_views` (`peer_date`) и `story_shares` + репосты `stories.fwd_*` по дням | `step`, `y_scaled` | да |
| `story_reactions_by_emotion_graph` | как реакции постов | `story_views.reaction`, `reaction_date` по дням | `bar`, `stacked` | да |
| `recent_posts_interactions` | — | последние 50 постов и 50 историй по дате вместе (посты — `postInteractionCountersMessage` с `views`, `forwards`, числом реакций; истории — `postInteractionCountersStory`) | — | — |

**`stats.megagroupStats`**
| поле | ряды | источник и SQL | тип | приближение |
|---|---|---|---|---|
| `members` | — | как `followers` | — | — |
| `messages` | — | `COUNT(*) channel_messages` (не служебные) с `date2 ∈ P` и `∈ prev P` | — | — |
| `viewers` | — | `COUNT(DISTINCT user_id) channel_stats_readers` за `P` и `prev P` (до `since` — `previous` = 0) | — | — |
| `posters` | — | `COUNT(DISTINCT sender_user_id)` за `P`, `prev P` | — | — |
| `growth_graph` | Участники | как у канала | `line` | да |
| `members_graph` | Вступили, Вышли | как `followers_graph` | `line` | да |
| `new_members_by_source_graph` | как у канала | `member_events` | `bar`, `stacked` | да |
| `languages_graph` | языки | как у канала | `area`, `percentage`, `stacked` | нет |
| `messages_graph` | Текст, Фото, Видео, Голосовые, Кружки, Стикеры, GIF, Файлы, Музыка, Прочее | `channel_messages GROUP BY day, message_filter_type` (константы `mtproto.MEDIA_*`, `third_party/teamgram-proto/mtproto/message_filter_util.go:33-48`; `MEDIA_EMPTY` → Текст) | `bar`, `stacked` | да |
| `actions_graph` | Сообщения, Зрители | сообщения по дням; зрители — `COUNT(DISTINCT user_id) channel_stats_readers GROUP BY day` (с `since`) | `line`, `y_scaled` | да |
| `top_hours_graph` | Сообщения | `channel_messages` за `P`: `GROUP BY HOUR(FROM_UNIXTIME(date2))` UTC | `line` | нет |
| `weekdays_graph` | Пн … Вс | сообщения по (неделя, день недели) за окно, `x` — недели | `area`, `percentage`, `stacked`, форматтеры `'week'` | нет |
| `top_posters` | — | за `P`: `sender_user_id, COUNT(*), AVG(CHAR_LENGTH(message))` топ 10 (без анонимных админов от имени группы и каналов) | — | — |
| `top_admins` | — | `channel_stats_admin_actions` за `P`: суммы по админу, топ 10 | — | — |
| `top_inviters` | — | `member_events WHERE source=5` за `P`: `GROUP BY inviter_id`, топ 10 | — | — |
| `users` | — | все упомянутые id, как их видит зритель | — | — |

**`stats.messageStats`**: `views_graph` — Просмотры, Пересылки поста (`channel_message_views`/`channel_message_forwards` по `(channel_id, message_id)`), от даты поста до сейчас: почасово, если посту не больше 48 ч (iOS включит почасовую шкалу по шагу 3600), иначе по дням с приближением до часов; `step`, `y_scaled`. `reactions_by_emotion_graph` — реакции поста по дням/часам той же шкалы.

**`stats.storyStats`**: `views_graph` — Просмотры, Пересылки истории (`story_views.date`, `story_shares.date` + репосты), почасово до 48 ч жизни, дальше по дням; `reactions_by_emotion_graph` — `story_views.reaction_date`.

**`stats.pollStats`**: `votes_graph` — по ряду на вариант (имя — текст варианта), `poll_votes` (`by_date`): `GROUP BY корзина, opt`; корзина — 5 минут, если от создания до `min(now, close_date)` не больше 48 ч, иначе час, больше 60 дней — день; `bar`, `stacked`. Голосов нет — `statsGraphError` «Голосов пока нет» (Android покажет «появится позже»). Отозванные голоса не видны (строк нет) — график показывает живые голоса по времени подачи.

**Публичные пересылки.** `getMessagePublicForwards`: строки `channel_stats_public_forwards (1, канал, пост)`, где адресат сейчас публичен (активное имя), сообщение не удалено и видно зрителю; порядок — `date DESC, dst_channel_id, dst_message_id`; `offset` — base64url(`date:channel:msg`), `next_offset` — если отдано `limit`; `count` — общее число (тем же фильтром); `forwards` — `publicForwardMessage` с сообщением, как его видит зритель; `chats`, `users`. `getStoryPublicForwards`: то же для `(2, пир, история)` плюс репосты `stories WHERE fwd_peer_type, fwd_peer_id, fwd_story_id` публичных каналов — `publicForwardStory{peer, story}`; слияние двух потоков по дате.

### Обновления другим устройствам
Нет: статистика — чтение. Новые флаги приходят с `getFullChannel` и итогами опроса.

### Пределы и производительность
- ~100 людей; каналы с долгой историей (импорт групп 083 — до сотен тысяч сообщений): каждый запрос идёт по индексу `(канал, дата)` в окне ≤ 365 дней; самый тяжёлый — сообщения группы по дням и типам (один `GROUP BY` по `channel_date`). Цель — ответ `getMegagroupStats` < 500 мс на группе в 200 тыс. сообщений (интеграционный тест с такой выборкой, время — в отчёт).
- Языки: один запрос последних `lang_code` на всех участников (`auth_users` ⋈ `auths` по `date_active`), не по одному.
- Публичные пересылки: `limit ≤ 100`.
- Схлопывание одинаковых запросов, срок 10 с; метрика `stats_request_duration_seconds{method}` и `stats_request_total{method,result}`.

### Поведение после рестарта
Состояния нет: токены приближения проверяются по ключу и сроку; события в MySQL; `since` в `stats_collection` не меняется повторной миграцией.

### Что пишется в лог
Только id каналов, методы, длительности, ошибки. Не писать имена вариантов опросов, тексты, токены приближения целиком, ключ (`tools/logaudit`: добавить `statstoken`, `tokenkey`).

## Порядок работ
Части 1, 3, 4 независимы и идут параллельно. Часть 2 — после 1 (обе правят `app/messenger/msg/internal/dao/channel_send.go` и миграцию; в одной волне не идут) и после слияния 098 A (заглушения). Часть 4 — после 099 A (общий загрузчик строк `pkg/langpack`). Часть 5 — после 3; 6 и 7 — после 5a, параллельно друг другу; 8 — после 1, 2, 5; 9 — последняя.

1. **Миграция и запись событий участия и админов** (M; `teamgramd/deploy/sql/migrate-20261119-channel-stats.sql`, `pkg/channelstats`, `app/service/biz/channel/internal/dao/{membership,invite,join_requests,rights,delete,read}.go`, `app/messenger/msg/internal/dao/channel_send.go` — только читатели). Интеграционные тесты `biz/channel`.
2. **Источник просмотра, публичные пересылки, заглушения** (M; `channel.tl.proto`+codec, `app/service/biz/channel/internal/dao/views.go`, `app/bff/messages/internal/core/{channel_views.go,messages.getMessagesViews_handler.go}`, `app/messenger/msg/internal/dao/channel_send.go` — пересылки, `app/service/biz/user/internal/dao/user_notify_settings.go`). Заглушения — после слияния 098. С частью 1 общий `channel_send.go` (разные функции).
3. **`pkg/statsgraph`** (M): JSON, проверки, палитры, форматтеры, корзины, токены; модульные тесты-«золотые» JSON и прогон разборщиков (см. «Тесты»).
4. **Строки** (S–M): `pkg/langpack/stats.json`, `StatsText` поверх общего загрузчика `loadTexts`/`pick`, который выносит из `mail.go` задача 099 (часть A) — второй загрузчик не писать; ~60 ключей (имена рядов, источников, типов сообщений, дней недели, 37 названий языков, «Прочее», тексты `statsGraphError`) × 37 языков, вычитка как у 076.
5a. **Модуль `app/bff/statistics`: каркас, доступ, `getBroadcastStats`, `loadAsyncGraph`, `getMessageStats`** (L): `helper.go`, svc, config, `internal/dao/{broadcast,message,members}.go`, обработчики; регистрация в `server.go`, конфиг bff (`teamgramd/etc`, `etc2`, `app/bff/bff/etc`), маршруты в трёх `session.yaml`.
6. **`getMegagroupStats`** (M; `internal/dao/megagroup.go`, свой обработчик).
7. **`getStoryStats`, `getMessagePublicForwards`, `getStoryPublicForwards`, `getPollStats`** (M; `internal/dao/{story,poll,forwards}.go`, обработчики).
8. **Флаги и включение** (S): `channelperm.CanViewStats`, `channels.getFullChannel` (`can_view_stats`, `stats_dc`), `pkg/polls` (`can_view_stats`), `STATS_GRAPH_TOKEN_KEY` в `.env.example`, `docker-compose.yaml`, `pkg/conf`, `tools/logaudit`, метрики.
9. **e2e, документы** (M): `test/e2e/stats_test.go`, `docs/ops/stats.md`, `TESTING-BACKLOG`, `go run ./tools/apicoverage`.

## Тесты
Модульные:
- `pkg/statsgraph`: «золотые» JSON каждого вида; `x` первый, компактность (`["x",` есть), равные длины; ≥1 ряда и ≥2 точек иначе ошибка; выброс нулевого ряда только у `y_scaled`; ≤50 столбцов; цвета — ключи из восьми и hex, тёмная палитра при `dark`; форматтеры по шкале; полдень UTC и неделя с понедельника; `defaultZoom` — точные `x`. Токены: подпись/проверка, подделка, чужой зритель, срок, другая версия.
- **Совместимость с разборщиками** (тест, падающий без правил): на Go — проверка инвариантов, которые повторяют разборщики (Android: каждый y-ряд имеет ключ в `colors` и `names`, деление в двухосевом без нуля; iOS: обязательные ключи и длины; tdesktop: `defaultZoom`); плюс разовый прогон «золотых» JSON через `JSON.parse` + `getFormatter` Web K и через `DoubleLinearChartData`/`StackLinearChartData` Android (выдержки в `testdata`, как в 090) — если поднять их без лишних зависимостей; иначе — ручная проверка в клиентах (раздел ниже).
- `channelperm.CanViewStats`: создатель, админ с/без `post_messages` в канале, с/без `ban_users` в группе, вышедший создатель, монофорум.
- Настроения реакций: таблица покрывает стандартные реакции сервера.
- `pkg/langpack`: `stats.json` — каждый ключ на 37 языках; запасной язык.
- `tools/logaudit` ловит токен и ключ.

Интеграционные (`pkg/testinfra`, настоящие MySQL и Redis; по одному пакету):
- миграция дважды подряд; `since` не меняется; индексы есть; заполнение публичных пересылок из готовых `channel_messages` (с копией обсуждения — не берётся).
- `biz/channel`: вступление по ссылке, по папке, по заявке, публично, добавление, выход, исключение — по одной строке с верным источником и `inviter_id`; повтор транзакции не удваивает; призрак не пишется; страж «каждое изменение `participants_count` покрыто».
- Действия админов: удаление чужих (`deleted += n`), исключение, ограничение.
- Просмотры: источник по пиру; копия в супергруппе считается оригиналу с источником «группа»; повторный просмотр источник не меняет.
- Заглушения: включение/выключение, временное с истечением, `-1` → по умолчанию.
- `app/bff/statistics` `dao`: каждый график на подготовленной выборке (ожидаемые числа руками); пустой канал; канал моложе 2 дней (`statsGraphError`); событийный график до `since`; группа в 200 тыс. сообщений — время ответа в отчёт.

Сквозные (`pkg/mtclient`, сессия после `getState`; язык сессии `ru` у админа):
- `TestStatsBroadcast`: админ создаёт канал; второй аккаунт вступает по ссылке, смотрит посты (`getMessagesViews` с `increment`), пересылает пост в публичный канал третьего, ставит реакцию, глушит канал. Админ: `getFullChannel` — `can_view_stats`, `stats_dc = 1`; у второго — нет. `getBroadcastStats`: все поля на месте, каждый `statsGraph` разбирается (обязательные ключи, `x` первый, длины), имена на русском, `iv_interactions_graph` — `statsGraphError`; `loadAsyncGraph(zoom_token, x)` — почасовой график; подделанный токен — `GRAPH_INVALID_RELOAD`; второй аккаунт — `CHAT_ADMIN_REQUIRED`; `getMegagroupStats` на канале — `MEGAGROUP_REQUIRED`.
- `TestStatsMessage`: `getMessageStats` — почасовой шаг 3600; `getMessagePublicForwards` — пересылка в публичный канал есть, в приватный — нет, пагинация `next_offset`.
- `TestStatsMegagroup`: сообщения разных типов, вступления, удаление админом; `getMegagroupStats` — `top_posters`, `top_admins`, `top_inviters`, `users`, `messages_graph` по типам.
- `TestStatsStoryAndPoll`: история канала с просмотром и репостом в публичный канал — `getStoryStats`, `getStoryPublicForwards`; опрос: автор видит `can_view_stats` в итогах, `getPollStats` — ряды по вариантам; другой — без флага и `PEER_ID_INVALID`.
- Проверить падающим без изменения хотя бы один (на `master` `stats.getBroadcastStats` — `ERR_ENTERPRISE_IS_BLOCKED` через прокси сессии).

## Проверки перед сдачей (CachyOS)
- `go build ./...`; `go vet -tags integration,e2e -unreachable=false` затронутых пакетов; `go test` затронутых и импортирующих (`pkg/channelperm`, `pkg/polls`, `app/service/biz/channel`, `app/service/biz/user`, `app/bff/messages`, `app/bff/channels`, `app/messenger/msg`), затем `-race`.
- Интеграционные по одному пакету; e2e со своими `E2E_PROJECT`/`E2E_DIR`, затем `test/e2e/run.sh down`.
- `make lint`, `go test ./tools/...`, `go test ./pkg/conf/...`, `go run ./tools/apicoverage`.
- На машине разработки ничего не собирать.

## Проверки для `docs/project/TESTING-BACKLOG.md`
- [ ] Android, iOS, Desktop, Web: в своём канале (админ с правом публикации) есть пункт «Статистика»; у админа без этого права и у подписчика — нет.
- [ ] Экран статистики канала открывается во всех четырёх клиентах: обзор с датами периода, графики роста, подписок, заглушений, лучших часов, просмотров по источникам, вступлений по источникам, языков, взаимодействий, реакций; график IV отсутствует.
- [ ] Подписи осей и подсказки — даты (не числа) в Web; часы в «лучших часах» в Android и Web (в Web — местные, в Android — по UTC).
- [ ] Двухосевой график взаимодействий при постах без пересылок открывается в Android без падения.
- [ ] Приближение дня (тап по точке) в Android, Desktop, Web, iOS — почасовые данные; выход из приближения.
- [ ] Названия рядов на языке клиента: русский, английский и один справа налево (арабский).
- [ ] Тёмная тема: цвета графиков читаемы во всех клиентах.
- [ ] Событийные графики (подписки, источники вступлений, заглушения) в первые сутки после развёртывания скрыты, потом начинаются с даты развёртывания.
- [ ] Статистика супергруппы (админ с правом блокировки): сообщения по типам, действия, лучшие часы, дни недели, лучшие участники, админы, пригласившие.
- [ ] Статистика поста из меню поста (Desktop), из окна пересылки (Android), из статистики канала (все); список публичных пересылок.
- [ ] Статистика истории канала из просмотра истории (Android, Desktop); публичные репосты.
- [ ] Статистика опроса автором (Android, Desktop, Web, iOS): график голосов; у нового опроса без голосов — «появится позже».
- [ ] Даты под точками дня совпадают с местными в часовом поясе Америки (например, UTC−5) и Москвы.

## Эксплуатация
- `docs/ops/stats.md`: что считается, с какого момента (`stats_collection`), переменная, метрики, как выключить.
- Переменная `STATS_GRAPH_TOKEN_KEY` (32 случайных байта, base64; `openssl rand -base64 32`) в `.env` (`.env.example` — пустая с пояснением), проброс в `bff` (`docker-compose.yaml`), `pkg/conf`. Без неё статистика выключена. Смена ключа делает недействительными выданные токены приближения (клиент перечитает статистику).
- Миграция `migrate-20261119-channel-stats.sql` применяется сервисом `migrate`, повторно безопасна; индексы на `channel_messages` и `channel_message_views` строятся онлайн (MySQL 8 `INPLACE`, без блокировки записи); на проде (~100 людей) — секунды. Перед развёртыванием — свежая резервная копия (как обычно).
- Новых сайдкаров нет; память и диск — пренебрежимо (тысячи строк в год; индексы — единицы МБ).
- Чистка `channel_stats_readers` старше 400 дней — раз в сутки в bff (как другие подметальщики), метрика удалённых строк.

## Решения к подтверждению
1. Права: канал — создатель или админ с `post_messages`, группа — создатель или админ с `ban_users` (по tdlib); без порога подписчиков (владелец, 2026-11-15).
2. Период обзора 30 дней, дневные графики до 365 дней, `defaultZoom` — последние 30; приближение — 7 суток по часам.
3. Точка дня в полдень UTC (даты верны от UTC−11 до UTC+11); «лучшие часы» по UTC.
4. «Реакции по эмоциям» — три ряда (положительные, отрицательные, прочие) по таблице в коде; запасной вариант — ряд на каждую реакцию.
5. Источники просмотров (подписчики, каналы, группы, личные чаты, прочее) и вступлений (ссылка, папка, заявка, публичный вход, добавлены, прочее).
6. «Зрители» и «действия» группы — по читавшим и писавшим за день; график действий — сообщения и зрители.
7. Смена настройки уведомлений по умолчанию для всех каналов не считается заглушением конкретного канала (`enabled_notifications` её учитывает).
8. Статистику опроса видит его автор; в канале — ещё админы с правом статистики (через `getPollStats`, флаг в итогах — только автору).
9. История событий начинается с развёртывания, до двух дней данных график скрыт.

## Не проверено
- Составы рядов Telegram (имена и число рядов каждого графика, что именно Telegram относит к источникам и «эмоциям», что такое «зрители» группы, длина периода и окно приближения) не описаны в документации и в клиентах; выбранные составы — наши (ADR 0066 §6), клиенты принимают любые ряды нужной формы.
- Часовой пояс корзин Telegram (UTC ли) — не проверено; Web K переводит час «лучших часов» из UTC в местный, Android — нет.
- Строки форматтеров взяты из таблицы Web K, а не из ответа Telegram; Android и iOS их не читают, tdesktop смотрит только на `'week'`.
- Как Android переносит исключение из обратного вызова `sendRequest` (падение приложения или потеря экрана) — не проверено; правило «нет нулевого ряда рядом с ненулевым» снимает вопрос.
- Пятиминутная шкала опроса на iOS при часовых и дневных корзинах (подписи времени вместо дат) — проверить в клиенте.
- Пути JSON `fwd_from` в `channel_messages.message_data` для заполнения индекса и признак автоматической копии в группу обсуждения — сверить с кодом 048 и данными.

## Результат
Статистика каналов, супергрупп, постов, историй и опросов сделана: части 1–9. Все методы `stats.*` из брифа обслуживает отдельная область bff `statistics`. Статистика включается ключом `STATS_GRAPH_TOKEN_KEY`; без него `stats.*` отвечают `ERR_ENTERPRISE_IS_BLOCKED`, а `can_view_stats` не ставится. События копятся с момента выкладки. Код в партии `batch/20261115`, на прод не выкачен.

- **1.** `pkg/channelstats` пишет события в транзакции изменения: вступления и выходы с источником, заглушения, читателей дня, действия админов. `migrate-20261119-channel-stats.sql` добавляет таблицы событий, `stats_collection` и индексы.
- **2.** Источник просмотра (подписчик, посторонний, копия в канале или группе, пересылка в личку), публичные пересылки постов и репостов историй с заполнением старых, события заглушения.
- **3.** `pkg/statsgraph`: JSON графиков, шкалы, корзины, свёртка в «прочее», палитра клиентов, токены приближения. Совместимость проверена тестами против разборщиков Android, iOS, Desktop и Web.
- **4.** Строки рядов на всех языках `own.json` (`pkg/langpack`).
- **5a.** `stats.getBroadcastStats`, `loadAsyncGraph`, `getMessageStats`. Токены приближения подписаны и истекают.
- **6.** `stats.getMegagroupStats`. Год сообщений группы читается из индекса по виртуальным колонкам `stats_kind` и `stats_chars`, без перестройки таблицы.
- **7.** `stats.getStoryStats`, `getStoryPublicForwards`, `getMessagePublicForwards`, `getPollStats`.
- **8.** `can_view_stats` в `channelFull` и итогах опроса, `channelperm.CanViewStats`. Неверный ключ останавливает bff при старте; в сообщении названа только переменная.
- **9.** Сквозные тесты, `docs/ops/stats.md`.

**Исправлено после слияния.** Заполнение старых публичных пересылок в миграции идёт под `READ COMMITTED` (`a6377f912`, `94a36a497`). После рестарта миграцию нужно прогнать ещё раз (`docs/ops/stats.md`).

**Отклонения от брифа.** По коммитам и документам частей не найдены.

**Решения.** Подтверждены владельцем: доступ (п. 1 — создатель или админ с `post_messages` в канале и с `ban_users` в группе, без порога подписчиков) и хранение метрик прочтения (задача 098). Ждут подтверждения п. 2–9: периоды и приближение, полдень UTC, три ряда «эмоций», составы источников, «зрители» группы, настройка по умолчанию не считается заглушением, кто видит статистику опроса, история с момента выкладки.

**Известное.**
- Таблица `channel_stats_readers` не чистится и растёт бессрочно; при ~100 людях это десятки мегабайт за годы (`docs/ops/stats.md`).
- Составы рядов — наши: Telegram их не документирует.

**Не проверено.** В клиентах — пункты задачи 103 в `TESTING-BACKLOG.md`. Поведение Telegram — раздел «Не проверено» выше.

**Проверка.** Партия `batch/20261115`, полный прогон на CachyOS на `2c1b155cf`: `go build ./...` и `go vet -tags integration,e2e -unreachable=false` обоих модулей; `go test ./...` — 237 пакетов ok, proto — 3; `go test ./tools/...` (с журнальным аудитом) — 6; `-race` — 191 пакет ok; `make lint` — 0 замечаний; интеграционные тесты — 70 пакетов по одному, все ok; e2e на свежем стенде — 300 PASS, 0 FAIL, 2 SKIP (замер задержки и ручной тест). На прод не выкачено, в клиентах владельцем не проверено.
