# 066. Поиск (ROADMAP, этап 2)

- Статус: done (сервер; полный прогон партии и проверка в клиентах — позже)
- Зависит от: 043, 048, 060 (поиск в каналах и темах), 053 (теги Сохранённых)
- ADR: 0039 (сопоставление текста)

## Цель
Поиск в чатах, группах и каналах ведёт себя как в Telegram для всех фильтров и входов клиентов; в поиске по всем чатам видны каналы и папки; глобальный поиск постов публичных каналов и хештеги работают; текст ищется по словам и формам русских слов.

## Что известно
- **Слои и поля:** `messages.search` (`from_id`, `saved_peer_id`, `saved_reaction`, `top_msg_id`), `messages.getSearchCounters`, `messages.getSearchResultsCalendar`, `messages.getSearchResultsPositions`, `messages.searchGlobal` (`folder_id`, `broadcasts_only`, `groups_only`, `users_only`, `community`), `messages.searchSentMedia`, `channels.searchPosts`, `channels.checkSearchPostsFlood` — `third_party/teamgram-proto/mtproto/schema.tl.sync_service.proto`.
- **`messages.search`:** `inputPeerEmpty` — все личные чаты и группы; `limit` 0 — только счётчик (core.telegram.org/method/messages.search). Поиск от отправителя возможен и в личном чате; ошибки `FROM_PEER_INVALID`, `INPUT_FILTER_INVALID`, `SEARCH_QUERY_EMPTY`, `PEER_ID_INVALID`.
- **Хештеги:** клиенты ищут `q = "#tag"` в чате (Android `HashtagSearchController.java:199-214`, docs core.telegram.org/api/search) и через `channels.searchPosts(hashtag)` — по публичным каналам, без `#` (`HashtagSearchController.java:222`, tdesktop `dialogs_widget.cpp:3433`).
- **`searchGlobal`.** Android посылает `folder_id` всегда (0 — без архива, `DialogsSearchAdapter.java:550-580`), tdesktop так же (`dialogs_widget.cpp:3462-3485`, `data_search_controller.cpp:80` «No archive»); листание — `next_rate`, затем `offset_rate/offset_peer/offset_id` последнего сообщения (`DialogsSearchAdapter.java`, core.telegram.org/method/messages.searchGlobal). `community` шлют tdesktop и tweb только для чатов типа «сообщество» — такого типа у нас нет, поле ограничивает поиск одним каналом.
- **Календарь.** Android берёт `messages[i]` как сообщение периода `i` (`CalendarActivity.java` `res.messages.get(i)`), iOS — сообщение `min_msg_id` периода (`SparseMessageList.swift:876`); продолжение — от `min_msg_id` прошлого ответа. Фильтры `Empty` и `MyMentions` не поддерживаются — `FILTER_NOT_SUPPORTED` (core.telegram.org/method/messages.getSearchResultsCalendar).
- **Позиции.** Позиции разрежены, `offset` — 0-based место во всём списке (constructor/searchResultPosition), Android просит 100, iOS 1000 (`SharedMediaLayout.java:5077`, `SparseMessageList.swift:122`).
- **`channels.searchPosts`.** Ровно одно из `hashtag` и `query`. У `query` дневной лимит; ответ — `messages.messagesSlice` с `search_flood` и `next_rate` (без него Android считает, что конец: `PostsSearchContainer.java`, `(r.flags & 1) == 0`; tdesktop считает `messages.channelMessages` ошибкой: `dialogs_search_posts.cpp`). Хештег не тратит слот (docs api/search). Запрос, уже сделанный, и страницы бесплатны; при отсутствии слотов — ошибка `FLOOD_WAIT_X_OR_STARS_Y` с кодом 400: Android и tweb разбирают именно её (`PostsSearchContainer.java`, `globalPostsSearch.tsx`), tdesktop перечитывает состояние по префиксу `FLOOD_WAIT_` (`dialogs_search_posts.cpp`); ответ с кодом 420 tgnet не отдаёт вызвавшему, а держит запрос до конца ожидания (`ConnectionsManager.cpp`, `error_code == 420`). Какой код шлёт Telegram, не проверено. Пустой `query` — так клиенты открывают список (tdesktop `PostsSearch::applyQuery`, Android `PostsSearchContainer`, «news»): ответ — новые посты, слот не тратится; что Telegram кладёт в этот список, не документировано. `hashtag` приходит по-разному: Android — со знаком (`HashtagSearchController`: `req.hashtag = query`), tdesktop и tweb — без первого символа, в том числе у кэштега (`dialogs_widget.cpp` `mid(1)`, `appMessagesManager.ts` `query.slice(1)`), поэтому тег без знака ищется и как `#`, и как `$`.
- **Сопоставление текста Telegram не документировано** (core.telegram.org/api/search его не описывает). Локальный поиск клиентов — все слова запроса, каждое от начала слова (tweb `src/lib/searchIndex.ts:71-79`). Что делает сервер Telegram — **не проверено**; выбор — решение, зафиксированное в ADR 0039, а не копия.

## Область
- `pkg/msgsearch`: разбор запроса и основа русского слова (`Patterns`, Snowball), виды фильтров (`KindOf`, `Columns.Kind`), запросы по ящику и каналу (`SearchBox`, `Count`, `Calendar`, `Positions`), квота постов.
- `channel` сервис: `Search` через `msgsearch`, новые RPC `channel.searchCount/Calendar/Positions/Across`.
- bff messages: все методы области, `not_served`-заглушки снимать не пришлось (были ENTERPRISE-заглушки `messages.getSearchResultsCalendar`, `messages.searchSentMedia`, `channels.searchPosts`, `channels.checkSearchPostsFlood`, `messages.getSearchResultsPositions` без ответа).
- Миграция `migrate-20261109-posts-search-slots.sql`.

## Критерии приёмки
- Модульные тесты: разбор запроса, таблица основ, фильтры, полночь квоты.
- Интеграционные (MySQL, ICU) `pkg/msgsearch`: русский текст, теги и знаки, все фильтры, отправитель, Сохранённые, листание, календарь, позиции, архив, квота. `app/service/biz/channel/internal/core`: поиск по каналам с курсором, публичные посты, анонимный автор.
- e2e `test/e2e/search_test.go`.

## Решения
- **Текст ищется по словам** (все слова запроса, каждое от начала слова) и формам русских слов через Snowball только в запросе, без изменений данных — ADR 0039. Это решение, а не воспроизведение Telegram.
- **Фильтры без своего `message_filter_type`** (`Geo`, `Contacts`, `Poll`, голосовое/кружок раздельно, `MyMentions`, пропущенные звонки) считаются условиями по `message_data` и столбцу `mentioned` — столбец только для медиа-типов.
- **Анонимный пост канала не находится по автору**: `sender_user_id` реального автора не раскрывается; `from_id` — канал находит копии постов в группе обсуждения.
- **Глобальный поиск** соединяет ящик и каналы по дате; при равной дате ящик идёт раньше канала, каналы — по id. Страница каждого источника после курсора независима, поэтому совпадение идентификаторов разных источников не теряет сообщения; курсор сужает страницу, а не `count`.
- **`channels.searchPosts`**: суточная квота 100 различных запросов, таблица `posts_search_slots`, хеш запроса вместо текста, платежей нет (`stars_amount: 0`). Гонка параллельных запросов может превысить лимит на их число — решено единым `INSERT … SELECT`, без блокировки.
- **`searchSentMedia` с `Empty`-фильтром** отдаёт все медиа; не-медиа-фильтры — `FILTER_NOT_SUPPORTED`.

## Результат
Реализованы все методы области: `messages.search` со всеми фильтрами и `from_id` (в том числе в личных чатах, `saved_peer_id`, `limit=0`, `inputPeerEmpty`), `messages.getSearchCounters`, `getSearchResultsCalendar`, `getSearchResultsPositions`, `searchGlobal` с каналами и папками, `searchSentMedia`, `channels.searchPosts` и `checkSearchPostsFlood`, хештеги и кэштеги. Текст ищется по `pkg/msgsearch` (ICU `REGEXP_LIKE`, основы Snowball); проверено сборкой, `go vet` с тегами и модульными тестами затронутых пакетов на хосте; интеграционные и e2e-тесты написаны и пройдут при полном прогоне партии (регулярные выражения MySQL на этой ветке не запускались). Проверка в клиентах и запись прогона — за полным прогоном партии.
