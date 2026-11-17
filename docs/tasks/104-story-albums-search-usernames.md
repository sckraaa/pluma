# 104. Альбомы историй, поиск историй, несколько имён

- Статус: готово, ждёт выкладки (2026-11-16)
- Решение: ADR 0069 (альбомы и поиск историй; частично заменяет ADR 0054, раздел «Не сделано»). Имена пользователя — без ADR: повторяют решение задачи 078 для каналов.
- Зависит от: 084 (истории, `pkg/stories`, ADR 0054), 078 (имена каналов: `channel.editUsernames`, `app/service/biz/channel/internal/dao/look.go:198-360`), 066 (поиск, `pkg/msgsearch` — только как образец разбора тегов).
- Ветка: `task/104-story-albums-search-usernames`.

## Цель
Вкладки альбомов в профиле работают во всех четырёх клиентах как в Telegram: создать альбом, положить в него истории (и сразу при публикации), переименовать, переставить, удалить; чужие альбомы видны со своими историями. Клик по хэштегу, кэштегу, заведению или геоточке в истории находит публичные истории. Пользователь может показывать, скрывать и переставлять свои имена так же, как канал (задача 078), с обновлением на других устройствах и у тех, кто его видит. Профиль больше не получает 400 на `stories.getAlbums` при каждом открытии.

## Область
В задаче:
- `stories.getAlbums`, `getAlbumStories`, `createAlbum`, `updateAlbum`, `deleteAlbum`, `reorderAlbums` (сейчас `ErrEnterpriseIsBlocked`, `app/bff/stories/internal/server/grpc/service/not_served.go:38-60`).
- `stories.sendStory` с `albums` (сейчас отбрасывается, `app/bff/stories/internal/core/stories.sendStory_handler.go:30-31`); `storyItem.albums` во всех ответах и обновлениях с историями, включая `messageMediaStory` и `channelFull.stories`.
- `stories.searchPosts` (хэштег, кэштег, `mediaAreaVenue`, `mediaAreaGeoPoint` с адресом, `peer`), только публичные истории.
- Индекс тегов и мест историй, дозаполнение для историй, опубликованных до миграции.
- `account.toggleUsername`, `account.reorderUsernames` (сейчас `ErrEnterpriseIsBlocked`, `app/bff/usernames/internal/server/grpc/service/fragment_service_impl.go:29-37`) на общей с каналами логике.
- Ключи `stories_albums_limit`, `stories_album_stories_limit` в конфиге приложения.

Не в задаче:
- `stories.startLive` — ждёт слова владельца о групповых звонках (остаётся `ErrEnterpriseIsBlocked`).
- `stories.toggleAllStoriesHidden`, `getUserStories`, `getAllReadUserStories` — остаются как есть.
- `bots.toggleUsername`, `bots.reorderUsernames` — платформа ботов (задача 106) может взять общий пакет из части E.
- Покупные имена (Fragment), `inputMediaAreaVenue` (не делает ни одна задача партии: 099 оставляет `QUERY_ID_INVALID`), поиск по тексту подписи.

## Контракт клиентов
Источники — `~/pluma-work/reference-src-new` (закреплённые выпуски) и core.telegram.org (прочитано 2026-11-15). Подробно с номерами строк — ADR 0069, «Контекст». Главное:
- Схема слоя 233 (`tdesktop/Telegram/SourceFiles/mtproto/scheme/api.tl`): `storyItem.albums:flags.19?Vector<int>` (`:1741`), `storyAlbum` с `icon_photo:flags.0`/`icon_video:flags.1` (`:2106`), `stories.albums hash`/`albumsNotModified` (`:2108-2109`), `sendStory albums:flags.8` (`:3194`), методы (`:3218-3224`), `foundStory`/`stories.foundStories` (`:1966-1968`), `account.reorderUsernames`/`toggleUsername` (`:2528-2529`), `updateUserName … usernames` (`:334`). Обновлений про альбомы в схеме нет. Всё это уже есть в `third_party/teamgram-proto` (`schema.tl.sync.proto:12889-12901`, `:12955`, `:19817-19835`, `:3544-3554`).
- `getAlbums`: iOS зовёт с `hash: 0` при каждой загрузке закреплённых историй профиля (`ios/submodules/TelegramCore/Sources/TelegramEngine/Messages/StoryListContext.swift:1578-1590`); tdesktop хранит и шлёт `hash` из ответа (`tdesktop/Telegram/SourceFiles/data/data_stories.cpp:1888-1913`); Android шлёт 0 (`android/TMessagesProj/src/main/java/org/telegram/ui/Stories/StoriesController.java:5002`). `albumsNotModified` разбирают tdesktop и Android.
- `getAlbumStories`: `offset` — число уже загруженных историй (Android `StoriesController.java:3850-3856`, tdesktop `data_stories.cpp:1854-1858`), порядок ответа — порядок альбома, клиенты не сортируют (tdesktop `:1806-1810`); конец — пустая страница (tdesktop `:1825`) или `count` (Android `:3903`). `pinned_to_top` для альбома не нужен.
- `createAlbum` с историями и без (tdesktop `data_stories.cpp:1915-1945` — одна история или ни одной; iOS `StoryListContext.swift:682-700` — список; Android `StoriesController.java:5062` — без историй). Ответ `storyAlbum` клиенты кладут в конец списка (tdesktop `:1932-1935`, Android `:5087`).
- `updateAlbum`: только `title` (tdesktop `:1946-1976`, Android `:5140`, iOS `:749-765`), только `add_stories`/`delete_stories` (Android `:5156-5220`, iOS `:1263`), только `order` (tdesktop `:2007-2050` — порядок загруженных историй, iOS `:1201-1216`). Android не ждёт ответа и сам правит `storyItem.albums` (`:5165-5172`).
- `deleteAlbum`, `reorderAlbums`: Android не ждёт ответа (`:5127-5138`, `:5245-5255`); iOS после `reorderAlbums` ставит неназванные папки в конец (`StoryListContext.swift:818-828`).
- Принадлежность — по `storyItem.albums`, независимо от `pinned` (Android `StoriesController.java:4029-4032`, tdesktop `data_stories.cpp:549-606`). Права на альбомы — свой аккаунт или канал с `edit_stories`/создатель (Android `:4765-4790`).
- Обложка: tdesktop читает оба поля (`data_stories.cpp:102-116`), Android — только `icon_photo` (`ui/Components/ItemOptions.java:2374-2386`), iOS не читает.
- Пределы: `stories_albums_limit`, `stories_album_stories_limit` (Android `messenger/AppGlobalConfig.java:47-48`, умолчания 100/100; iOS `TelegramUI/Components/PeerInfo/PeerInfoVisualMediaPaneNode/Sources/PeerInfoStoryPaneNode.swift:1831-1843`, умолчания 100/1000). Название — до 12 символов UTF-16 (tdesktop `info/stories/info_stories_albums.cpp:25`, Android `ui/Components/AlertsCreator.java:915-916`). `ALBUMS_TOO_MANY` — tdesktop показывает окно предела (`info_stories_albums.cpp:86-94`), прочие ошибки — тостом с именем.
- `searchPosts`: Android шлёт тег со знаком (`ui/Components/HashtagActivity.java:63-73`, `HashtagsSearchAdapter.java:96`), iOS и Web K срезают только `#` (`StoryListContext.swift:2516`, `tweb/src/lib/appManagers/appStoriesManager.ts:1864`). `peer` из `#tag@username` (Android `StoriesController.java:3150-3173`, iOS `StoryListContext.swift:2517-2521`). iOS — `limit` 100 (`:2532`). Конец — нет `next_offset` или набран `count` (Android `:3198`). tdesktop метод не вызывает. Документация: «Either hashtag or area must always be set», точку ищут только с `address`, ошибка `HASHTAG_INVALID` (core.telegram.org/method/stories.searchPosts).
- Геоадрес Android шлёт уровнями: улица / город / страна, на английском (`ui/Adapters/BaseLocationAdapter.java:225-240, 305-318, 348-358`).
- Имена: `account.toggleUsername` — для покупных имён; ошибки `USERNAMES_ACTIVE_TOO_MUCH`, `USERNAME_INVALID`, `USERNAME_NOT_MODIFIED`; `account.reorderUsernames` — «All active usernames must be specified», ошибки `ORDER_INVALID`, `USERNAME_NOT_MODIFIED`; только пользователи (core.telegram.org/method/account.toggleUsername, …/account.reorderUsernames). Android на редактируемом имени не зовёт toggle, а ставит курсор в поле (`ui/ChangeUsernameActivity.java:281-284`), reorder шлёт только после перетаскивания и только активные (`:613-640`); tdesktop разбирает только `USERNAMES_ACTIVE_TOO_MUCH` и `FLOOD_WAIT_` (`api/api_user_names.cpp:161-170`), ошибку reorder игнорирует (`:227-229`).

## Дизайн

### Таблицы (`teamgramd/deploy/sql/migrate-20261119-story-albums-search.sql`, новые таблицы и guarded ALTER через `information_schema`, как `migrate-20261116-password-email.sql`)
```sql
CREATE TABLE IF NOT EXISTS `story_albums` (
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `album_id` int(11) NOT NULL,
  `title` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `position` bigint(20) NOT NULL,
  `created_at` int(11) NOT NULL,
  PRIMARY KEY (`peer_type`, `peer_id`, `album_id`),
  KEY `ordered` (`peer_type`, `peer_id`, `position`)
);
CREATE TABLE IF NOT EXISTS `story_album_items` (
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `album_id` int(11) NOT NULL,
  `story_id` int(11) NOT NULL,
  `position` bigint(20) NOT NULL,
  PRIMARY KEY (`peer_type`, `peer_id`, `album_id`, `story_id`),
  KEY `ordered` (`peer_type`, `peer_id`, `album_id`, `position`),
  KEY `story` (`peer_type`, `peer_id`, `story_id`)
);
CREATE TABLE IF NOT EXISTS `story_tags` (
  `kind` tinyint(4) NOT NULL,            -- 1 hashtag, 2 cashtag
  `tag` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL, -- lower case, no sign
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `story_id` int(11) NOT NULL,
  `everyone` tinyint(1) NOT NULL,        -- audience "everyone" (exceptions allowed); 1 for channels
  `date` int(11) NOT NULL,
  PRIMARY KEY (`kind`, `tag`, `peer_type`, `peer_id`, `story_id`),
  KEY `found` (`kind`, `tag`, `everyone`, `date`),
  KEY `story` (`peer_type`, `peer_id`, `story_id`)
);
CREATE TABLE IF NOT EXISTS `story_places` (
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `story_id` int(11) NOT NULL,
  `seq` int(11) NOT NULL,                -- index of the area in media_areas
  `kind` tinyint(4) NOT NULL,            -- 1 venue, 2 geo point with address
  `venue_key` varchar(255) COLLATE utf8mb4_bin NOT NULL DEFAULT '', -- provider ':' venue_id
  `country` varchar(8) NOT NULL DEFAULT '', `state` varchar(128) NOT NULL DEFAULT '',
  `city` varchar(128) NOT NULL DEFAULT '', `street` varchar(255) NOT NULL DEFAULT '', -- lower case
  `everyone` tinyint(1) NOT NULL,
  `date` int(11) NOT NULL,
  PRIMARY KEY (`peer_type`, `peer_id`, `story_id`, `seq`),
  KEY `venue` (`kind`, `venue_key`, `date`),
  KEY `geo` (`kind`, `country`, `city`, `date`)
);
-- guarded: story_counters.last_album_id int NOT NULL DEFAULT 0
-- guarded: stories.search_indexed tinyint(1) NOT NULL DEFAULT 0, KEY unindexed (search_indexed)
```
Все таблицы `ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, комментарий в начале файла — в стиле `migrate-20261114-stories.sql`. Безопасна на пустой и наполненной базе и при повторном запуске.

### `pkg/stories` (хранилище; все изменения — под `lockPeerTx`)
- Новый файл `albums.go`: тип `Album{Peer, Id, Title, Position}`; константы `MaxAlbums = 100`, `MaxAlbumStories = 1000`, `MaxAlbumTitle = 12`; ошибки `ErrAlbumsTooMany`, `ErrAlbumInvalid`, `ErrAlbumFull`, `ErrStoryInvalid`.
  - `CreateAlbum(ctx, peer, title, storyIds, now) (*Album, error)`: `last_album_id+1`, позиция — в конец; истории должны существовать у `peer` (иначе `ErrStoryInvalid`), дубликаты в запросе схлопываются.
  - `EditAlbum(ctx, peer, albumId, title *string, del, add, order []int32) (*Album, changedStories []int32, error)`: порядок — удалить, добавить (в конец, уже лежащие пропускаются), переставить (названные — первыми, остальные следом в прежнем порядке; не входящие в альбом id в `order` пропускаются). Предел `MaxAlbumStories` — после добавления.
  - `DeleteAlbum`, `ReorderAlbums(order)` (та же семантика «названные первыми»), `Albums(peer)` (по `position`), `AlbumStories(peer, albumId) ([]*Story, error)` — все истории альбома по порядку (не больше `MaxAlbumStories`; фильтр видимости и страницы — в bff), `AlbumsOf(peer, storyIds) map[int32][]int32`.
  - Позиции — `int64`, перестановка переписывает позиции альбома целиком (не больше 1000 строк, как `reorderUsernames` у каналов).
- `store.go`:
  - `Create` принимает `st.Albums []int32`: в той же транзакции добавляет историю в конец существующих и незаполненных альбомов, остальные пропускает и возвращает в `stored.Albums` то, куда положил (ADR 0069, п. 6).
  - `Delete` удаляет `story_album_items`, `story_tags`, `story_places` удаляемых историй.
  - `Create` и `Update` пишут индекс (`indexTx`, часть C) и ставят `search_indexed = 1`.
- `Story` получает `Albums []int32` (не колонка: заполняется из `story_album_items`), `Seen` — `Albums []int32`; `Item` ставит `item.Albums`. `SeenOf` читает `AlbumsOf` одним запросом, так что `storyItem.albums` появляется везде, где строится `Item` (ответы, `updateStory`, `channelFull.stories`, `messageMediaStory` через плагин `UserStories`).
- Новый файл `search.go`: `Tags(caption, entities) []Tag` и `Places(areas) []Place` — чистые функции; `indexTx(tx, st)` (удалить старые строки истории, вставить новые; `everyone` = `StoryAudienceOf(privacy) == Public` для пользователя, 1 для канала); `Unindexed(ctx, limit)` и `Reindex(ctx, peer, id)` для воркера; `SearchTags(ctx, kind, tag, peer *Peer, cursor, scan)` и `SearchPlaces(ctx, place, peer, cursor, scan)` — кандидаты `JOIN stories` с `everyone = 1 AND (expire_date > now OR pinned = 1)`, новые сначала, не больше `MaxSearchScan = 2000`.
- Теги: сущности `messageEntityHashtag`/`messageEntityCashtag` клиента (подстрока подписи по UTF-16) плюс `mention.GetTags('#', …)` и `GetTags('$', …)`; нижний регистр; до 64 символов (`[]rune`), длиннее — не индексируется; кэштег — только с буквы в начале; уникальные по `(kind, tag)`.
- Места: `mediaAreaVenue` → `venue_key = provider + ":" + venue_id` (пустой `venue_id` — не индексировать); `mediaAreaGeoPoint` с `address` → поля адреса в нижнем регистре; без адреса — не индексировать.

### bff stories (`app/bff/stories/internal/core/`)
Файлы на методы, по одному на TL-метод, как соседние:
- `stories.getAlbums_handler.go`: `view(peer)`; владелец (`manages()`) видит все альбомы; остальные — только альбомы, где им видна хоть одна история (`shown`). Обложка — первая видимая история: `messageMediaPhoto.photo` → `icon_photo`, `messageMediaDocument.document` → `icon_video`. `hash` = `hashx.CombineInt64Hash` по `(album_id, позиция в выдаче, id истории обложки)` и `hashx`-сумма заголовка (CRC32 строки); совпал с запросом — `stories.albumsNotModified`.
- `stories.getAlbumStories_handler.go`: `view`; альбома нет — `ALBUM_ID_INVALID`; истории альбома по порядку → `shown` → срез `[offset, offset+limit)` (`limit` 1…100, `offset < 0` — `LIMIT_INVALID`); `count` — число видимых; ответ через `storiesAnswer` без `pinned_to_top`.
- `stories.createAlbum_handler.go`, `stories.updateAlbum_handler.go`, `stories.deleteAlbum_handler.go`, `stories.reorderAlbums_handler.go`: новый помощник `albumEditor(peer)` в `access.go` — аккаунт сам; канал — создатель или админ с `ADMIN_EDIT_STORIES`; бот — `BOT_METHOD_INVALID`; чужой пользователь — `PEER_ID_INVALID`, канал без права — `CHAT_ADMIN_REQUIRED`. Заголовок: `strings.TrimSpace`, 1…12 UTF-16 — иначе `ALBUM_TITLE_INVALID`. `updateAlbum` без флагов — `ALBUM_ID_INVALID` не нужен: отвечает текущим альбомом. После изменения состава — `pushChanged(v, changedStories)` (`stories.togglePinned_handler.go:75-96`). Ответ `createAlbum`/`updateAlbum` — `storyAlbum` с обложкой для вызывающего.
- `stories.sendStory_handler.go`: передать `in.Albums` в `Store.Create` (снять фразу «Albums are not served»); `updateStory` в ответе и в рассылке уже содержит `albums` через `Item`.
- `stories.searchPosts_handler.go`: бот — `BOT_METHOD_INVALID`; ни `hashtag`, ни `area` — `SEARCH_QUERY_EMPTY` (как `channels.searchPosts`, `app/bff/messages/internal/core/search_global.go:253-255`); оба — тоже `SEARCH_QUERY_EMPTY`. Хэштег: `$` в начале — кэштег, иначе срезать один `#`; пусто, больше 64 символов, пробелы или `#`/`$` внутри — `HASHTAG_INVALID`. Область: `mediaAreaVenue` или `mediaAreaGeoPoint` с `address`, иначе `MEDIA_INVALID`. `peer`: через `view` (канал, который нельзя читать, — `CHANNEL_PRIVATE`). Кандидаты из `pkg/stories` → фильтр в Go: пользователь — не удалён, не `shutOutBy` (`access.go:102-112`), `EvaluateStoryPrivacy` правил для искавшего (как `visible`, `access.go:176-196`); канал — `ChannelGetMutableChannel`, есть активное имя, искавший не забанен. `count` — число прошедших фильтр среди кандидатов; страница по курсору; `next_offset` — курсор последней отданной, если дальше есть видимые. Ответ: `foundStory{peer, Item(...)}` (`SeenOf` по каждому владельцу), `users`/`chats` через `refs`.
- `not_served.go`: убрать семь методов, оставить `startLive`, `toggleAllStoriesHidden`, `getUserStories`, `getAllReadUserStories`; комментарий файла поправить.
- `server/grpc/service/stories_service_impl.go`: семь методов, как соседние.
- `errors.go`: `errAlbumsTooMany` (`ALBUMS_TOO_MANY`), `errAlbumIdInvalid`, `errAlbumTitleInvalid`, `errAlbumStoriesTooMany`, `errHashtagInvalid` с комментариями, откуда имя (ADR 0069, п. 6: три имени не прочитаны) и перевод из ошибок `pkg/stories`.
- `sweeper.go`: шаг «индекс»: `Unindexed(ctx, sweepBatch)` → `Reindex` каждой; ошибка логируется, следующий проход повторит.

### Конфиг приложения
- `teamgramd/bin/app_config.json`: `"stories_albums_limit": 100`, `"stories_album_stories_limit": 1000`; `pkg/stories/appconfig_test.go` сверяет их с `MaxAlbums`/`MaxAlbumStories`; `docs/ops/app-config.md` — две строки.

### Имена пользователя
- Новый пакет `pkg/peernames`: из `app/service/biz/channel/internal/dao/look.go:180-196, 273-360` перенести `usernameRow`, `activeNames`, `toggleUsername`, `usernamesOrder`, `reorderUsernames`, обобщив на `(peer_type, peer_id)` внутри `userbox.Box`; ошибки — свои sentinel (`ErrNotFound`, `ErrEditable`, `ErrSame`, `ErrTooMany`, `ErrBadOrder`, `ErrSameOrder`), каждый вызывающий переводит их в ошибки своего метода. `maxShownUsernames = 10` (tdesktop `kMaxUsernames`, `edit_peer_usernames_list.cpp:338`, как в 078) — константа пакета.
- biz channel: `EditUsernames` зовёт `pkg/peernames` и переводит ошибки как сегодня (`USERNAME_INVALID`, `USERNAME_NOT_MODIFIED`, `USERNAMES_ACTIVE_TOO_MUCH`, `CHAT_NOT_MODIFIED`); поведение каналов не меняется, их тесты (`look_integration_test.go`, `test/e2e/channel_look_test.go`) — без правок.
- biz user: новый RPC `user.editUsernames user_id:long action:int username:string active:Bool order:Vector<string> = Bool` (`app/service/biz/user/user/user.tl.proto`, действия — константы рядом с `username.go`; генерация, клиент, регистры, `user_service_impl.go` — как `channel.editUsernames` в коммите 19d39b4f2). Реализация — через `changeSelf(ctx, id, Predicate_updateUserName, …)` (`app/service/biz/user/internal/dao/self.go:100-180`): под блокировкой бокса читает строки `username` пользователя `FOR UPDATE`, применяет `pkg/peernames`, а `changeSelf` в той же транзакции шлёт `updateUserName` с `seq` всем устройствам аккаунта (ADR 0020) и `tellWatchers` тем, кто видит пользователя. Нет изменения — `changed=false`, без обновления.
- Ошибки для пользователя (core.telegram.org): toggle — `USERNAME_INVALID` (не его имя, или редактируемое: оно скрывается через `account.updateUsername("")`, как у каналов), `USERNAME_NOT_MODIFIED` (то же состояние), `USERNAMES_ACTIVE_TOO_MUCH`; reorder — `ORDER_INVALID` (не все активные или лишние), `USERNAME_NOT_MODIFIED` (тот же порядок).
- bff usernames: `account.toggleUsername_handler.go`, `account.reorderUsernames_handler.go` (бот — `BOT_METHOD_INVALID`); `fragment_service_impl.go` — два метода через core, комментарий «those of accounts and bots not yet» поправить на «bots not yet». Сервис `RPCFragment` уже маршрутизирован (задача 078), `session.yaml` не меняется.
- У пользователя Pluma сегодня одно имя (покупных нет, `selfUsernames` отдаёт одно редактируемое, `self.go:183-211`), так что на практике toggle ответит `USERNAME_INVALID`/`USERNAME_NOT_MODIFIED`, а reorder своего единственного имени — `USERNAME_NOT_MODIFIED`. Это поведение Telegram для аккаунта без покупных имён; код при этом полный — строки `editable = 0` появятся, когда появится источник таких имён.

### Обновления другим устройствам и пользователям
- Альбомы: `updateStory` по каждой истории со сменившимся `albums` — другим устройствам вызывающего (`pushOthers`), без pts (как все обновления историй, `push.go:25-27`). Удаление альбома — `updateStory` по его историям. Про сам список альбомов обновлений нет (ADR 0069, п. 3).
- Публикация с `albums`: как сегодня — `updateStory` в ответе, другим устройствам и зрителям; `albums` внутри.
- Поиск ничего не рассылает.
- Имена: `updateUserName` с `seq` устройствам аккаунта и `tellWatchers` (как `account.updateUsername`).

### Пределы и ошибки
| что | предел | ошибка |
|---|---|---|
| альбомов у владельца | 100 | `ALBUMS_TOO_MANY` |
| историй в альбоме | 1000 | `ALBUM_STORIES_TOO_MANY` |
| название | 1…12 UTF-16 после обрезки | `ALBUM_TITLE_INVALID` |
| id в одном запросе (`stories`, `add`, `delete`, `order`) | `stories.MaxIds` (100) для историй, 100 для `reorderAlbums` | `LIMIT_INVALID` |
| `getAlbumStories.limit`, `searchPosts.limit` | 1…100 (меньше — 1, больше — 100, как `msgsearch.MaxLimit`) | — |
| кандидатов поиска за вызов | 2000 | — (выдача и `count` в этих пределах) |
| тег | 1…64 символа | `HASHTAG_INVALID` |
| несуществующий альбом | — | `ALBUM_ID_INVALID` |
| чужая или несуществующая история в `createAlbum`/`add_stories` | — | `STORY_ID_INVALID` (`errStoryIdInvalid`) |

### После перезапуска
Состояния в памяти нет. Альбомы и индекс — в MySQL в транзакциях историй. Недоиндексированные истории (до миграции или после сбоя вне транзакции — таких путей нет, но флаг страхует) добирает воркер; второй экземпляр bff безопасен: `Reindex` под `lockPeerTx`, идемпотентен. `updateStory`, не дошедший из-за падения после записи, теряется, как и сегодня у `togglePinned`: устройство перечитает профиль.

### Файлы
Добавить:
- `teamgramd/deploy/sql/migrate-20261119-story-albums-search.sql`.
- `pkg/stories/albums.go`, `pkg/stories/search.go`, `pkg/stories/search_test.go`, `pkg/stories/albums_integration_test.go`, `pkg/stories/search_integration_test.go`.
- `app/bff/stories/internal/core/stories.{getAlbums,getAlbumStories,createAlbum,updateAlbum,deleteAlbum,reorderAlbums,searchPosts}_handler.go`, `albums.go` (обложка, хэш, `albumEditor`), `albums_test.go`, `search_test.go`.
- `pkg/peernames/peernames.go`, `peernames_test.go`, `peernames_integration_test.go`.
- `app/service/biz/user/internal/core/user.editUsernames_handler.go`, `app/service/biz/user/internal/dao/usernames.go`, интеграционный тест рядом.
- `app/bff/usernames/internal/core/account.toggleUsername_handler.go`, `account.reorderUsernames_handler.go`, `account.usernames_test.go`.
- `test/e2e/story_albums_test.go`, `test/e2e/story_search_test.go`, `test/e2e/user_usernames_test.go`.

Изменить:
- `pkg/stories/stories.go` (`Story.Albums`, константы), `store.go` (`Create`, `Update`, `Delete`), `view.go` (`Seen.Albums`, `Item`, `SeenOf`), `appconfig_test.go`; `teamgramd/bin/app_config.json`.
- `app/bff/stories/internal/core/stories.sendStory_handler.go`, `access.go`, `errors.go`, `sweeper.go`; `internal/server/grpc/service/not_served.go`, `stories_service_impl.go`.
- `app/service/biz/channel/internal/dao/look.go` (перенос в `pkg/peernames`).
- `app/service/biz/user/user/user.tl.proto` и сгенерированное, `user/username.go` (константы действий), `client/user_client.go`, `internal/server/grpc/service/user_service_impl.go`.
- `app/bff/usernames/internal/server/grpc/service/fragment_service_impl.go`.
- `tools/apicoverage` перегенерировать (`docs/project/api-coverage.md` — в основной сессии), `docs/ops/stories.md`, `docs/ops/app-config.md`.

## Порядок работ
Части по размеру — одна посадка исполнителя каждая.

- **A. Хранилище альбомов** (M). Миграция целиком (все таблицы и обе колонки, чтобы файл был один), `pkg/stories/albums.go`, `Story.Albums`/`Seen.Albums`/`Item`/`SeenOf`, `Create` с альбомами, каскад в `Delete`, константы и `app_config.json` с тестом. Интеграционные тесты `albums_integration_test.go`.
- **B. Методы альбомов** (M). Шесть обработчиков, `albumEditor`, обложка и хэш, `sendStory` с `albums`, `pushChanged`, ошибки, `not_served.go`, `stories_service_impl.go`, модульные тесты, `test/e2e/story_albums_test.go`. Зависит от A.
- **C. Индекс поиска** (M). `pkg/stories/search.go`: разбор тегов и мест, `indexTx` в `Create`/`Update`, удаление в `Delete`, `Unindexed`/`Reindex`, запросы кандидатов; шаг воркера в `sweeper.go`; модульные и интеграционные тесты. Зависит от A (миграция и правки `store.go` в тех же функциях `Create`/`Delete`) — делать после A, не параллельно.
- **D. `stories.searchPosts`** (S–M). Обработчик, фильтр видимости, курсор, `count`, e2e `story_search_test.go`. Зависит от C. С B пересекается в `not_served.go`, `stories_service_impl.go`, `errors.go` — если B и D идут параллельно, эти три файла сливать руками.
- **E. Имена пользователя** (M). `pkg/peernames` с переносом из biz channel, `user.editUsernames` (proto, генерация, клиент, сервис, dao через `changeSelf`), два обработчика bff usernames, тесты, e2e `user_usernames_test.go`. Независима от A–D, идёт параллельно с ними.

Параллельно: {A → C → D} и {E}; B после A, параллельно с C (B не трогает `store.go`). Общие файлы: `store.go` (A, C), `not_served.go`/`stories_service_impl.go`/`errors.go` (B, D), `sweeper.go` (только C).

## Тесты
Модульные:
- `pkg/stories/search_test.go`: теги из сущностей и из текста, без регистра, `$100` — не кэштег, `#тег` кириллицей, длиннее 64 — нет, дубли схлопываются, UTF-16 смещения сущностей с эмодзи; места: заведение без `venue_id` — нет, точка без адреса — нет, уровни адреса.
- `pkg/stories`: `Item` ставит `albums`; пустой список — поле не задано.
- bff stories `albums_test.go`: обложка — первая видимая (фото → `icon_photo`, видео → `icon_video`); `hash` меняется при переименовании, перестановке, смене обложки и не меняется без изменений; `albumsNotModified`; права (`edit_stories`, создатель, чужой, бот); заголовок 12/13 символов UTF-16, пробелы.
- bff stories `search_test.go`: разбор `hashtag` (`#a`, `a`, `$A`, пусто, `a b`, `#`), выбор ошибки на `area`, курсор туда и обратно.
- `pkg/peernames`: порядок «все активные ровно раз», тот же порядок, редактируемое имя, предел 10.
- bff usernames: перевод ошибок для пользователя; бот — `BOT_METHOD_INVALID`.

Интеграционные (`pkg/testinfra`, настоящий MySQL; по одному пакету за раз):
- миграция дважды подряд на наполненной базе (истории 084 есть, колонки добавляются один раз).
- `albums_integration_test.go`: id не повторяется после удаления; предел альбомов и историй под гонкой двух транзакций; `EditAlbum` удалить+добавить+переставить в одном вызове; «названные первыми»; `Delete` истории убирает её из альбомов; `Create` с альбомами — пропуск несуществующего и заполненного; удаление истории и добавление её в альбом параллельно — либо в альбоме нет строки, либо истории нет, но не строка без истории.
- `search_integration_test.go`: индекс пишется в `Create`, переписывается в `Update` (смена подписи и правил «все» ↔ «контакты»), удаляется в `Delete`; `Unindexed`/`Reindex` дозаполняет строки с `search_indexed = 0` и повтор ничего не меняет; кандидаты: истёкшая незакреплённая не находится, закреплённая находится; порядок и курсор.
- `pkg/peernames` / biz user: `user.editUsernames` с подготовленными строками `editable = 0` — toggle, reorder, предел; `updateUserName` с `seq` записан в outbox в той же транзакции; без изменения — ничего.

Сквозные (`pkg/mtclient`, сессии после `getState`):
- `TestStoryAlbums`: A публикует три истории (одну «контакты»), создаёт альбом с двумя, второй пустой → `getAlbums` (порядок, обложки, `hash`; повтор с `hash` — `albumsNotModified`); `updateAlbum` add/delete/order/title; второе устройство A получает `updateStory` с новым `albums`; `getAlbumStories` по страницам `offset`; `reorderAlbums`; `deleteAlbum`; `sendStory` с `albums` → история в альбоме, `albums` в ответе; удаление истории убирает её из альбома.
- `TestStoryAlbumsOfOthers`: B (не контакт) видит альбом без «контактной» истории, обложка — не она; альбом только из невидимых историй B не виден; канал: админ без `edit_stories` — `CHAT_ADMIN_REQUIRED`, с правом — работает, подписчик видит альбом.
- `TestStorySearch`: A — история «все» с `#кот $TSLA` и заведением, история «контакты» с `#кот`, геоточка с адресом-городом; B находит только публичную по `кот`, `#кот`, `$TSLA`, по заведению и по городу; `peer` сужает; A заблокировал B — ничего; публичный канал находится, приватный — нет; `HASHTAG_INVALID`, `SEARCH_QUERY_EMPTY`; две страницы по `next_offset` с `limit` 1.
- `TestUserUsernames`: `account.toggleUsername` своего имени — `USERNAME_INVALID`, `account.reorderUsernames([своё])` — `USERNAME_NOT_MODIFIED`, порядок с чужим именем — `ORDER_INVALID`.
- Хотя бы один тест проверить падающим без изменения (например, `TestStoryAlbums` на `master` получает `ERR_ENTERPRISE_IS_BLOCKED`).

## Проверки перед сдачей (CachyOS)
- На ветке: `go build ./...`; `go vet -tags integration,e2e -unreachable=false` затронутых пакетов; `go test` затронутых (`pkg/stories`, `pkg/peernames`, `app/bff/stories/...`, `app/bff/usernames/...`, `app/service/biz/user/...`, `app/service/biz/channel/...`); интеграционные `pkg/stories`, `pkg/peernames`, biz user и biz channel по одному пакету; e2e раздела «Тесты» со своими `E2E_PROJECT`/`E2E_DIR`, затем `test/e2e/run.sh down`.
- `go run ./tools/apicoverage` — 9 методов из 400 в обслуживаемые (файл затем `git restore` у исполнителя).
- Полный набор (`-race`, `make lint`, `go test ./tools/...`, все интеграционные и e2e) — на ветке пачки.
- На Mac ничего не собирать.

## Проверки для `docs/project/TESTING-BACKLOG.md`
Раздел «## Альбомы и поиск историй, имена (задача 104, ADR 0069)»:
- [ ] Android, iOS, Desktop, Web: открыть свой профиль → истории — нет ошибок, вкладка альбомов есть; создать альбом, добавить две истории, переименовать, переставить истории, удалить альбом.
- [ ] Опубликовать историю сразу в альбом (Android и iOS — выбор альбома в редакторе) — она в альбоме.
- [ ] Второе устройство того же аккаунта: после добавления истории в альбом с первого — история в альбоме без перезапуска (по `updateStory`); после создания альбома — альбом виден при новом открытии профиля.
- [ ] Чужой профиль: альбомы видны, «контактная» история не видна не-контакту и не стоит обложкой; альбом только из скрытых историй не виден.
- [ ] Канал: админ с правом «Редактировать истории» ведёт альбомы, подписчик их видит.
- [ ] Более 100 альбомов — Desktop показывает окно предела (`ALBUMS_TOO_MANY`).
- [ ] Ссылка «Поделиться альбомом» (Desktop) открывает альбом в другом клиенте.
- [ ] Клик по `#тегу` в подписи чужой публичной истории — выдача историй (Android, iOS, Web); `$TSLA` — кэштеги; `#тег@канал` — только истории канала.
- [ ] История с заведением и с геоточкой «город» — клик по области показывает истории этого места (Android, iOS).
- [ ] «Контактная» история и история приватного канала в поиске не появляются.
- [ ] Android, iOS, Desktop, Web: экран имени пользователя — без ошибок; имя меняется как раньше, другие устройства видят новое имя сразу.

## Эксплуатация
- Миграция `migrate-20261119-story-albums-search.sql` применяется сервисом migrate при развёртывании; повторный запуск безопасен. Колонка `stories.search_indexed` добавляется `ALGORITHM = INSTANT`; индекс `unindexed` — обычным `ALTER` (таблица маленькая: истории с 2026-11-14).
- После выката воркер историй за несколько проходов (64 истории каждые 3 с) индексирует старые истории; проверка в `docs/ops/stories.md`: `SELECT COUNT(*) FROM stories WHERE search_indexed = 0` → 0.
- `app_config.json` — две новые строки, перезапуск не нужен сверх обычного выката.
- Новых переменных, сайдкаров, внешних сервисов нет; диск — десятки байт на историю и тег; память не меняется.
- `docs/ops/stories.md`: альбомы, пределы, индекс поиска и проверка дозаполнения. `docs/ops/app-config.md`: два ключа.

## Решения к подтверждению
1. Пределы: 100 альбомов, 1000 историй в альбоме (iOS по умолчанию 1000, Android 100; значения Telegram не прочитаны). Название — до 12 символов, как в клиентах.
2. `sendStory` с несуществующим или заполненным альбомом публикует историю и пропускает альбом, а не отказывает.
3. Альбом, где зрителю не видна ни одна история, ему не показывается; обложка — первая видимая зрителю история.
4. Поиск — только публичное: «все» у пользователя (с исключениями) и публичные каналы; закреплённые истории находятся и после срока.
5. Места: заведение — по `provider`+`venue_id`; точка — по стране и заданным в запросе уровням адреса (город находит все улицы города).
6. Тег без знака — хэштег (так шлют iOS и Web K), в отличие от `channels.searchPosts`, где он ищет оба знака (`pkg/msgsearch/text.go:48-58`).
7. Имена ошибок `ALBUM_ID_INVALID`, `ALBUM_TITLE_INVALID`, `ALBUM_STORIES_TOO_MANY` выбраны сами (клиенты их по имени не разбирают).
8. Редактируемое имя пользователя `account.toggleUsername` не скрывает (`USERNAME_INVALID`), как у каналов в 078.

## Не проверено
- Как Telegram выбирает обложку альбома и считает `hash` (документация молчит; дизайн от этого не зависит — `hash` непрозрачен для клиентов).
- Видит ли в Telegram посторонний альбомы и `storyItem.albums` чужого профиля в том же объёме (из клиентов: Android грузит альбомы любого `dialogId`, `StoriesController.java:2602-2615`; правила сервера Telegram не прочитаны).
- Попадает ли в выдачу Telegram закреплённая история после срока и истории из исключений «все кроме».
- Как Telegram сравнивает геоточки (по уровням адреса или по расстоянию) и заведения.
- Предел длины названия на сервере Telegram (клиенты режут на 12).
- Можно ли в Telegram скрыть редактируемое имя пользователя при наличии покупных (Android такого действия не предлагает, `ChangeUsernameActivity.java:281-284`; iOS не читался).
- Правила кэштега Telegram (длина, только латиница) — индексируется всё, что начинается с буквы.

## Связи с задачами пачки
- 098 (шум): `stories.getAlbums` не трогает; владелец — эта задача.
- 099 (инлайн-боты): задаёт `stories_venue_search_username`, но `inputMediaAreaVenue` по-прежнему `QUERY_ID_INVALID` (ADR 0062, «Что не делается»). Когда отдельная задача начнёт превращать его в `mediaAreaVenue`, такие истории попадут в индекс мест без правок 104; до тех пор индекс мест наполняют только `mediaAreaVenue`/`mediaAreaGeoPoint`, присланные готовыми.
- 106 (платформа ботов): `bots.toggleUsername`/`reorderUsernames` берут `pkg/peernames` из части E.
- 101, 102, 103, 105 — не пересекаются (103 — статистика историй, если затронет `pkg/stories/view.go`, сливать `Seen` руками).

## Результат
Альбомы историй, поиск историй по тегам и местам и порядок имён пользователя сделаны: части A–E. iOS больше не получает 400 при открытии историй профиля. Код в партии `batch/20261115`, на прод не выкачен.

- **A.** Таблицы `story_albums`, `story_album_items`, `story_tags`, `story_places` и флаг `search_indexed` в одной миграции (`migrate-20261119-story-albums-search.sql`, её можно запускать повторно).
  - Пределы: 100 альбомов, 1000 историй в альбоме, название до 12 единиц UTF-16. Они переданы в app config как `stories_albums_limit` и `stories_album_stories_limit`.
  - `storyItem.albums` видят все зрители.
- **B.** `stories.createAlbum`, `updateAlbum`, `deleteAlbum`, `reorderAlbums`, `getAlbums`, `getAlbumStories`. `sendStory` кладёт новую историю в названные альбомы.
- **C.** Теги и места пишутся в индекс в транзакции создания и правки. Хэштеги и кэштеги приводятся к нижнему регистру, длина до 64 знаков. Старые истории индексирует подметальщик: 64 истории раз в 3 с.
- **D.** `stories.searchPosts` ищет публичные истории пользователей (с учётом исключений) и каналов с публичным именем: новые сначала, по страницам.
- **E.** `account.toggleUsername` и `account.reorderUsernames`. Общие правила имён переехали в `pkg/peernames` (каналы и аккаунты). Без Fragment у аккаунта одно имя, поэтому ответы `USERNAME_INVALID` и `USERNAME_NOT_MODIFIED`, как у Telegram.

**Исправлено после слияния.** В `docs/ops/stories.md` описаны миграция альбомов и поиска, наполнение индекса и проверка `SELECT COUNT(*) FROM stories WHERE search_indexed = 0` (`3824e4d5a`).

**Отклонения от брифа.** Заведений (`mediaAreaVenue`) клиенты пока создать не могут: `inputMediaAreaVenue` по-прежнему отвечает `QUERY_ID_INVALID`. Поиск по ним проверен только сквозным тестом. Бота мест нет (задача 099, решение владельца).

**Решения.** Ждут подтверждения п. 1–8 раздела «Решения к подтверждению»: пределы, `sendStory` с заполненным альбомом, видимость альбомов и обложка, поиск только публичного, сравнение мест, тег без знака, имена ошибок, редактируемое имя не скрывается.

**Известное.** Альбомы и индекс поиска не удаляются при удалении аккаунта: удаление строк аккаунта (`f7a8983e5`) их не затрагивает.

**Не проверено.** В клиентах — пункты задачи 104 в `TESTING-BACKLOG.md`. Поведение Telegram — раздел «Не проверено» выше.

**Проверка.** Партия `batch/20261115`, полный прогон на CachyOS на `2c1b155cf`: `go build ./...` и `go vet -tags integration,e2e -unreachable=false` обоих модулей; `go test ./...` — 237 пакетов ok, proto — 3; `go test ./tools/...` (с журнальным аудитом) — 6; `-race` — 191 пакет ok; `make lint` — 0 замечаний; интеграционные тесты — 70 пакетов по одному, все ok; e2e на свежем стенде — 300 PASS, 0 FAIL, 2 SKIP (замер задержки и ручной тест). На прод не выкачено, в клиентах владельцем не проверено.
