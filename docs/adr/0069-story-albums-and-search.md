# 0069. Альбомы историй и поиск историй: хранение, видимость, индекс тегов и мест

- Статус: предложено (2026-11-15)
- Задача: 104 (`docs/tasks/104-story-albums-search-usernames.md`, ветка `task/104-story-albums-search-usernames`). Обслуживаются `stories.getAlbums`, `getAlbumStories`, `createAlbum`, `updateAlbum`, `deleteAlbum`, `reorderAlbums`, `searchPosts`, поле `albums` у `stories.sendStory` и `storyItem.albums`.
- Связано: ADR 0054 (истории: хранение, приватность, срок, ленты), ADR 0041 (Premium для всех), ADR 0039 (поиск сообщений, `pkg/msgsearch`).
- Частично заменяет ADR 0054, раздел «Не сделано»: альбомы и поиск историй больше не отвечают `ERR_ENTERPRISE_IS_BLOCKED`. Живые истории (`stories.startLive`) по-прежнему не делаются — ждут слова владельца о групповых звонках.

## Контекст

### Схема (слой 233, `tdesktop/Telegram/SourceFiles/mtproto/scheme/api.tl`)
- `storyItem … albums:flags.19?Vector<int> music:flags.20?Document` (`:1741`); у Pluma поле есть в `third_party/teamgram-proto/mtproto/schema.tl.sync.proto:12955` (`repeated int32 albums = 25`), сервер его не заполняет (`pkg/stories/view.go:42-89`).
- `storyAlbum#9325705a flags:# album_id:int title:string icon_photo:flags.0?Photo icon_video:flags.1?Document` (`:2106`), `stories.albumsNotModified`, `stories.albums hash:long albums:Vector<StoryAlbum>` (`:2108-2109`).
- `stories.sendStory … albums:flags.8?Vector<int>` (`:3194`).
- `stories.searchPosts#d1810907 flags:# hashtag:flags.0?string area:flags.1?MediaArea peer:flags.2?InputPeer offset:string limit:int = stories.FoundStories` (`:3218`); `foundStory peer story`, `stories.foundStories count stories next_offset:flags.0?string chats users` (`:1966-1968`).
- `stories.createAlbum peer title stories:Vector<int> = StoryAlbum`, `updateAlbum … title:flags.0 delete_stories:flags.1 add_stories:flags.2 order:flags.3 = StoryAlbum`, `reorderAlbums peer order = Bool`, `deleteAlbum peer album_id = Bool`, `getAlbums peer hash:long`, `getAlbumStories peer album_id offset:int limit:int = stories.Stories` (`:3219-3224`).
- Обновлений про альбомы в схеме нет (поиск `Album` по `api.tl` находит только перечисленное).

### Документация (прочитано 2026-11-15)
- https://core.telegram.org/api/stories, раздел об альбомах: методы выше; «A profile can contain a maximum of `stories_albums_limit` albums», у альбома свой предел историй; новая история попадает в альбомы параметром `albums` у `sendStory`. Как выбирается обложка и как считается `hash`, не сказано.
- https://core.telegram.org/method/stories.searchPosts: глобальный поиск по хэштегу (без `#`) или по области `mediaAreaGeoPoint`/`mediaAreaVenue`; точку ищут, только если у неё есть `address`; `peer` сужает поиск до одного автора; `offset` — пустая строка, затем `next_offset`; ошибка `HASHTAG_INVALID`; вызывают только пользователи. Какие истории попадают в выдачу по приватности и как сравниваются места — не сказано.

### Клиенты (`~/pluma-work/reference-src-new`)
- Список альбомов: iOS просит `getAlbums` с `hash: 0` при каждой загрузке закреплённых историй профиля (`ios/submodules/TelegramCore/Sources/TelegramEngine/Messages/StoryListContext.swift:1578-1590`) — сегодня каждый такой заход даёт 400. tdesktop хранит `hash` из ответа и шлёт его снова (`tdesktop/Telegram/SourceFiles/data/data_stories.cpp:1888-1913`); Android шлёт `hash` 0 (строка с хэшем закомментирована, `android/…/ui/Stories/StoriesController.java:5002`). `albumsNotModified` разбирают tdesktop и Android.
- Обложка: tdesktop читает `icon_photo` и `icon_video` (`data_stories.cpp:102-116`), Android в меню выбора альбома рисует только `icon_photo` (`android/…/ui/Components/ItemOptions.java:2374-2386`), iOS обложку не читает (папка — `id` и `title`, `StoryListContext.swift:715`).
- Истории альбома: `offset` — сколько историй клиент уже загрузил, а не id (Android `StoriesController.java:3850-3856`, tdesktop `data_stories.cpp:1854-1858`); порядок ответа — порядок альбома, клиенты его не сортируют (tdesktop `data_stories.cpp:1806-1810`); конец списка — пустая страница (tdesktop `:1825`) или `count` (Android `:3903`).
- Принадлежность истории альбому клиенты берут из `storyItem.albums` и не связывают с «закреплённостью» (Android `StoriesController.java:4029-4032`; tdesktop `data_stories.cpp:549-606`).
- Права: альбомы своего аккаунта и канала, где есть право `edit_stories` или он создатель (Android `canEditStoryAlbums`, `StoriesController.java:4765-4790`).
- Пределы: ключи конфига приложения `stories_albums_limit` и `stories_album_stories_limit` — Android по умолчанию 100 и 100 (`android/…/messenger/AppGlobalConfig.java:47-48`), iOS 100 и 1000 (`ios/submodules/TelegramUI/Components/PeerInfo/PeerInfoVisualMediaPaneNode/Sources/PeerInfoStoryPaneNode.swift:1831-1843`). Название альбома — до 12 символов UTF-16 в tdesktop и Android (`tdesktop/…/info/stories/info_stories_albums.cpp:25`, `android/…/ui/Components/AlertsCreator.java:915-916`). Превышение числа альбомов tdesktop узнаёт по `ALBUMS_TOO_MANY` (`info_stories_albums.cpp:86`).
- Поиск: Android шлёт тег со знаком (`#tag` или `$TAG`, `android/…/ui/Components/HashtagActivity.java:63-73`, `HashtagsSearchAdapter.java:96`), iOS и Web K срезают только `#` (`StoryListContext.swift:2516`, `tweb/src/lib/appManagers/appStoriesManager.ts:1862`), значит тег без знака — хэштег, `$` — кэштег. `peer` — автор из `#tag@username` (Android `StoriesController.java:3150-3173`, iOS `:2517-2521`). iOS просит по 100 (`:2532`). Следующая страница — `next_offset`, конец — его отсутствие или `count` (Android `:3198`). tdesktop `stories.searchPosts` не вызывает.
- Геоадрес Android собирает уровнями: улица (страна, регион, город, улица), город (страна, регион, город), страна (только `country_iso2`), всегда на английском (`android/…/ui/Adapters/BaseLocationAdapter.java:225-240, 305-318, 348-358`).

### Сервер сегодня
- Все семь методов — `ErrEnterpriseIsBlocked` (`app/bff/stories/internal/server/grpc/service/not_served.go:34-60`); `sendStory` молча отбрасывает `albums` (`stories.sendStory_handler.go:30-31`).
- Истории в `stories` (`teamgramd/deploy/sql/migrate-20261114-stories.sql`), все изменения историй одного владельца идут под блокировкой строки `story_counters` (`pkg/stories/store.go:263-274`); единственное место удаления строк — `Store.Delete` (`store.go:513-546`).
- Поиск постов каналов по хэштегу живёт в `pkg/msgsearch` через REGEXP по тексту сообщений (`pkg/msgsearch/text.go:53-58`), для историй он не подходит: подписи и области историй — JSON в `stories`.

## Решение

### 1. Хранение альбомов
- `story_albums(peer_type, peer_id, album_id, title, position, created_at)`, PK `(peer_type, peer_id, album_id)`; `story_album_items(peer_type, peer_id, album_id, story_id, position)`, PK `(peer_type, peer_id, album_id, story_id)`, ключ `(peer_type, peer_id, story_id)` для `storyItem.albums` и удаления.
- `album_id` выдаётся счётчиком `story_counters.last_album_id` и не используется повторно: ссылка на удалённый альбом не откроет новый.
- Каждое изменение альбомов берёт ту же блокировку `lockPeerTx`, что и изменения историй: добавление истории не разойдётся с её удалением.
- `Store.Delete` в той же транзакции удаляет строки `story_album_items` удаляемых историй; удаление альбома удаляет его строки; сами истории не трогаются.
- Принадлежность альбому не зависит от «закреплённости» и срока: так её понимают клиенты (см. «Клиенты»). Что показать конкретному зрителю, решает п. 2.

### 2. Видимость
- Альбомы и их истории видит тот, кто видит профиль владельца (`view`: канал, который он может читать; пользователь, не заблокировавший его).
- Владелец (аккаунт, для канала — админ с правом управлять историями) видит все истории альбома. Остальные — только те, что им вообще показываются вне ленты: в ленте или в профиле и разрешённые правилами (`shown`).
- Обложка и `hash` считаются для зрителя: обложка — медиа первой видимой ему истории альбома (фото → `icon_photo`, видео-документ → `icon_video`). Обложка из истории, которую зритель не видит, выдала бы её содержимое.
- Альбом, в котором зрителю не видна ни одна история, в `getAlbums` ему не отдаётся (владельцу — всегда). Пустая вкладка без историй зрителю ничего не даёт.
- `storyItem.albums` отдаётся каждому, кто видит историю: по нему клиенты раскладывают истории по альбомам.

### 3. Изменения альбомов
- Создавать, переименовывать, менять состав и порядок, удалять: аккаунт сам; в канале — создатель или админ с `edit_stories` (как Android). Остальным — `CHAT_ADMIN_REQUIRED` (канал) или `PEER_ID_INVALID` (чужой пользователь).
- Новый альбом встаёт в конец списка; новая история — в конец альбома. `order` у `updateAlbum` и `reorderAlbums` ставит названные элементы первыми в указанном порядке, остальные идут следом в прежнем порядке: tdesktop шлёт порядок только загруженных историй (`data_stories.cpp:2015-2045`).
- Другим устройствам владельца уходит `updateStory` по каждой истории, чей `albums` изменился (`pushChanged`, как у `togglePinned`). Обновления «список альбомов изменился» в схеме нет: другие устройства перечитывают `getAlbums` при открытии профиля (iOS — всегда, tdesktop и Android — при первой загрузке).

### 4. Поиск: индекс тегов и мест
- `story_tags(peer_type, peer_id, story_id, kind, tag, everyone)`: `kind` 1 — хэштег, 2 — кэштег; `tag` в нижнем регистре, без знака, до 64 символов; `everyone` — правила истории пользователя «все» (`StoryAudiencePublic`, с исключениями тоже), у канала всегда 1.
- `story_places(peer_type, peer_id, story_id, kind, venue_key, country, state, city, street, everyone)`: `kind` 1 — `mediaAreaVenue` (`venue_key` = `provider` + `:` + `venue_id`), 2 — `mediaAreaGeoPoint` с `address` (поля адреса в нижнем регистре).
- Теги берутся из подписи: сущности `messageEntityHashtag`/`messageEntityCashtag` от клиента и разбор текста сервером (`pkg/mention.GetTags` по `#` и `$`; кэштег — только если начинается с буквы, иначе `$100` стал бы тегом).
- Индекс пишется в транзакции `Store.Create` и `Store.Update` (подпись, области и правила меняются только там) и удаляется в `Store.Delete`. Истории, записанные до миграции, проиндексирует воркер историй: у строки `stories.search_indexed` = 0, запись индекса и флаг — одна транзакция под `lockPeerTx`.

### 5. Поиск: что находится
- Только публичное: история пользователя с правилами «все», которую правила пускают к искавшему (исключения из «все» соблюдаются), если автор не заблокировал искавшего и не скрыл от него истории; история канала, если у канала есть активное публичное имя и искавший не забанен в нём. Плюс история должна быть видна вне ленты: в ленте (срок не вышел) или в профиле (`pinned`). Свои публичные истории искавший тоже находит.
- Хэштег: тег без знака или с `#` — хэштег, с `$` — кэштег; сравнение без регистра, целиком. Пустой после среза, длиннее 64 символов или с недопустимыми символами — `HASHTAG_INVALID`.
- Заведение: совпадение `provider` и `venue_id`. Точка: совпадение страны и тех уровней адреса, которые заданы в запросе (запрос «город» находит истории этого города с любой улицей; «страна» — всю страну). Точка без `address` и другие области — `MEDIA_INVALID`.
- `peer` сужает выдачу до одного автора по тем же правилам. Новые сначала (`date`, затем владелец и id). `offset` — непрозрачная строка-курсор (`date`, тип и id владельца, id истории); `count` — число всех видимых искавшему совпадений, подсчитанных в пределах `MaxSearchScan` строк-кандидатов; `limit` 1…100.

### 6. Пределы
- `stories_albums_limit` = 100, `stories_album_stories_limit` = 1000 в `teamgramd/bin/app_config.json`, те же константы в `pkg/stories` (тест сверяет, как `appconfig_test.go`). Название — 1…12 символов UTF-16 после обрезки пробелов.
- Ошибки: `ALBUMS_TOO_MANY` (имя из tdesktop), `ALBUM_ID_INVALID`, `ALBUM_TITLE_INVALID`, `ALBUM_STORIES_TOO_MANY` (имена трёх последних не прочитаны ни в одном источнике; клиенты их по имени не разбирают, показывают общую ошибку), `STORY_ID_INVALID` для чужой или несуществующей истории в `add_stories`/`stories`.
- `sendStory` с `albums`: несуществующие альбомы и заполненные пропускаются, история публикуется — потерять загруженное видео из-за устаревшего выбора альбома хуже, чем не положить его в альбом.

### 7. После перезапуска
- Всё в MySQL, памяти нет. Недоиндексированные старые истории добирает воркер; повторный проход ничего не меняет (`INSERT … ON DUPLICATE`, флаг ставится в той же транзакции).

## Последствия
- Профиль перестаёт получать 400 на `getAlbums`; вкладки альбомов работают в четырёх клиентах; хэштеги и места в историях кликабельны и находят публичные истории.
- Две новые таблицы альбомов, две таблицы индекса, две колонки (`story_counters.last_album_id`, `stories.search_indexed`). `Store.Create`/`Update`/`Delete` пишут чуть больше в той же транзакции.
- Плотные данные малы (≤100 пользователей): поиск фильтрует кандидатов в Go без ухода в отдельный поисковый движок.
- Поиск ничего не отправляет за пределы домашнего сервера (ADR 0057, 0061).

## Что не делается
- `stories.startLive` (живые истории) — после решения владельца о групповых звонках.
- Отдельного обновления «альбомы изменились» нет и не придумывается: в схеме его нет.
- Поиск по тексту подписи и по областям, кроме заведения и точки с адресом.
- Поиск историй из приватных каналов и историй «контакты/близкие/выбранные» — только публичное (решение задачи).
- `inputMediaAreaVenue` по-прежнему `QUERY_ID_INVALID` (ADR 0054; задача 099 его тоже не делает, ADR 0062); когда отдельная задача начнёт превращать его в `mediaAreaVenue`, индекс подхватит такие истории без изменений.

## Рассмотренные альтернативы
- Поиск REGEXP по `stories.caption` и JSON `media_areas` без индекса: проще, но сканирует все истории и не отделяет кэштег от хэштега без повторного разбора; индекс дешёвый и пишется там же, где история.
- Хранить обложку альбома: устаревает при удалении и смене правил истории и выдаёт скрытые истории; считается при чтении.
- Альбом только из закреплённых историй: клиенты так не считают, а снятие с профиля молча выкидывало бы историю из альбома.

## Что осталось подтвердить владельцу
- Пределы 100 альбомов и 1000 историй в альбоме.
- Пропуск неверных альбомов в `sendStory` вместо ошибки.
- Сопоставление мест (заведение по `venue_id`, точка по уровням адреса) — поведение Telegram не описано.
