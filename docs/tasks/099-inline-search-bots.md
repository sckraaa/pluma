# 099. Встроенные боты @pic, @gif, @places

- Статус: готово, ждёт выкладки (2026-11-16)
- Решение: ADR 0062.
- Зависит от: 070 / ADR 0043 (инлайн-режим, `pkg/botstore`, кэш ответов в Redis), 0027 (`pkg/webpage`: безопасная загрузка, фото в dfs), 096 / ADR 0061 (дом, исходящий трафик через VPS), 076 / ADR 0046 (37 языков строк). Внутри партии: часть B этой задачи — единственный владелец общей основы `upload.getWebFile` (`pkg/webfile`, обработчик, `webfile_dc_id`, `DontLogContentForMethod`, имена геоданных в `logaudit`); 100 (карты) только регистрирует свой `Source` для `inputWebFileGeoPointLocation` и сливается после B. 106 (платформа ботов) переносит в `runsAside` ещё один метод и отказывает ботам реестра в своих командах `pluma-admin`.
- Ветка: `task/099-inline-search-bots`.

> **Решение владельца 2026-11-16, имеет приоритет над текстом ниже: места (`@places`) не делаем.** Нет бота `places` в реестре и в `z_pluma-inline-bots.sql`, нет поставщика `photon`, части F, контейнера Photon, `PHOTON_URL`, `docker-compose.places.yaml`, тестов `TestInlinePlaces` и строк проверки владельца про места. `venue_search_username` в `help.getConfig` и `stories_venue_search_username` в app config не меняются против сегодняшних значений (на них нет нашего бота, как и сейчас; клиенты 13.2 в пикерах геопозиции этот бот не используют — Android `BaseLocationAdapter.java:91`, iOS `LocationPickerControllerNode.swift:529, 792`). Реестр рассчитан на два бота, `@gif` и `@pic`; тип `KindVenue` не вводится.

## Цель
Вкладка GIF в панели эмодзи всех четырёх клиентов показывает GIF, набор `@gif`, `@pic`, `@places` в поле ввода даёт GIF, картинки и места, поиск картинок в галерее Android работает — всё как в Telegram и без патчей клиентов. Медиа приходят только с сервера Pluma: устройство не обращается к сторонним хостам ни за ответами этих ботов, ни за миниатюрами сторонних инлайн-ботов (прокси `upload.getWebFile`). Картинки ищет свой SearXNG дома, места — свой Photon дома; GIF по тексту — KLIPY, только после письменного согласия KLIPY на серверную интеграцию (ADR 0062, п. 4.1), до этого `@gif` отдаёт популярное внутри Pluma.

## Область
В задаче:
- `pkg/inlinebots` — реестр трёх ботов; учётные записи из `z_pluma-inline-bots.sql`; проверка реестра при старте.
- `contacts.resolveUsername` без таблицы апстрима `GetBotIdByName`.
- `pluma-admin bot token|set` отказывают ботам реестра.
- `help.getConfig`: три имени ботов всегда, `webfile_dc_id = 1`. `help.getAppConfig`: `stories_venue_search_username`, `gif_search_emojies`.
- `messages.getInlineBotResults` к ботам реестра — ответ поставщика вместо процесса бота; кэш, страницы (`next_offset`), пустой запрос, пределы, метрики.
- `pkg/inlinemedia`: импорт фото и GIF в dfs при запросе, таблица `inline_media`, дневной бюджет.
- `messages.sendInlineBotResult`: `hide_via` для ботов реестра; счётчик отправок `@gif`.
- Поставщики: `local` (популярные GIF), `searxng` (картинки), `photon` (места); контейнеры `searxng`, `photon`; импорт и обновление данных Photon.
- `pkg/webfetch` (вынести безопасную загрузку из `pkg/webpage`), `pkg/webfile` и `upload.getWebFile` для `inputWebFileLocation` (подписанный прокси); подписанные `webDocument` у сторонних инлайн-ботов; `upload.getWebFile` в обход очереди ключа в сессии.
- Описания ботов на 37 языках (`pkg/langpack/inlinebots.json`) через `botstore.BotInfo`.
- Миграция, конфигурация, `.env.example`, compose, метрики, алерты, `logaudit`, `docs/ops/inline-bots.md`, тестовые поставщики в e2e-стенде.

Не в задаче (ADR 0062, «Что не делается»):
- Патчи клиентов: места в пикере iOS/Android, поиск картинок в iOS, tdesktop, Web K.
- `inputMediaAreaVenue` в историях (остаётся `QUERY_ID_INVALID`).
- URL-контент сторонних ботов с `MediaAuto` (по-прежнему `SEND_MESSAGE_MEDIA_INVALID`).
- Сборка мусора импортированных файлов, перевод имён и плейсхолдеров на язык зрителя.
- `inputWebFileGeoPointLocation` (задача 100), `inputWebFileAudioAlbumThumbLocation`.
- GIPHY, Tenor, платный поиск картинок.
- **Адаптер KLIPY (часть G)** — только после согласия KLIPY и их спецификации кэша. Без них в ветку не входит.

## Контракт клиентов
Источники — `~/pluma-work/reference-src-new` (Android 13.2.1, iOS release-13.2, tdesktop 7.4.1, Web K), схема — layer 233 (tdesktop `mtproto/scheme/api.tl`), страницы core.telegram.org прочитаны 2026-11-15. Подробно — ADR 0062, «Контекст».

### Как клиенты находят ботов
- Имена — из `help.getConfig`: Android `MessagesController.java:5688-5693`; если `img_search_username` нет, бот картинок Android становится `null` (`:5691`), поэтому все три поля шлются всегда. iOS — `ManagedConfigurationUpdates.swift:68`; tdesktop — только `gif_search_username` (`mtproto_config.cpp:283`); Web K — `appGifsManager.ts:70`. Истории iOS — `stories_venue_search_username` (`AccountContext.swift:2039-2042`).
- Бот находится через `contacts.resolveUsername`: Android `PhotoPickerActivity.java:1599`, `EmojiView.java:8948`; tdesktop `gifs_list_widget.cpp:901`; iOS `GifContext.swift:56`. Сегодня `gif` резолвится в несуществующего пользователя 101 (`contacts.resolveUsername_handler.go:41`, `vars.go:31-96`), поэтому таблица апстрима убирается.

### Вкладка GIF
- iOS не покажет ничего без `bot_inline_placeholder` (`GifContext.swift:65`). Он же обязателен на сервере (`messages.inline_handlers.go:97`).
- Android: пустой запрос — популярное (`EmojiView.java:8939`), `peer = inputPeerEmpty` (`:9200`). Категории — эмодзи из `gif_search_emojies` идут запросом (`:9015`); пустой список убирает категории (`MessagesController.java:3090-3110`). iOS и tdesktop читают тот же ключ (`ChatEntityKeyboardInputNode.swift:233`, `stickers_list_footer.cpp:135-137`).
- iOS шлёт `peer = inputPeerSelf` (`GifContext.swift:66`). tdesktop пустой запрос не шлёт (`gifs_list_widget.cpp:927`), пустой `next_offset` — конец (`:944`).
- Web K берёт только `botInlineMediaResult.document` (`appGifsManager.ts:81`). iOS — `internalReference` с файлом (`GifContext.swift:131-134`). Android — `result.document` (`EmojiView.java:9244`). Значит, ответ `@gif` — документы из dfs.
- Отправка из панели: Android и iOS — `messages.sendInlineBotResult` с `hide_via` (`ChatActivityEnterView.java:12491-12509`, `SendMessagesHelper.java:6544`; `ChatController.swift:2820`); tdesktop — `hideViaBot` (`gifs_list_widget.cpp:545`); Web K — документ как обычное медиа (`searchGifs` отдаёт документы). Набранный `@gif запрос` Android шлёт без `hide_via` (параметр `bot` есть) — подпись «через @gif» остаётся, как в Telegram.
- `hide_via` — «only for bot usernames encountered in the config» (core.telegram.org/method/messages.sendInlineBotResult).

### Поиск картинок (только Android)
- `PhotoPickerActivity.java:1615-1740`: берёт только `type == "photo"`; при `result.photo` отправляет готовое фото как обычное медиа (`SendMessagesHelper.java:11188-11189`).
- При URL-результатах Android сам качает адрес по HTTP — и миниатюру (`PhotoPickerPhotoCell.java:167-168`), и картинку при отправке (`needDownloadHttp`, `SendMessagesHelper.java:11185-11200`). Поэтому ответ `@pic` — `botInlineMediaResult` с фото из dfs.
- Страницы — `next_offset`; конец — `next_offset == null` или нет новых (`PhotoPickerActivity.java:1740`).

### Места
- Работает только набор `@places …`. Android пикер — Geocoder ОС (`BaseLocationAdapter.java:91`). iOS пикер — места выключены (`LocationPickerController.swift:53`, `LocationPickerControllerNode.swift:529, 792`).
- Android для бота с `bot_inline_geo` спрашивает разрешение и шлёт `geo_point` (`MentionsAdapter.java:583-596, 924-926`).
- Карточка результата: tdesktop делает отправку места из `botInlineMessageMediaVenue` (`inline_bot_result.cpp:237-245`), тип `venue` (`:73`). Android рисует карту миниатюры через `inputWebFileGeoPointLocation` (`ContextLinkCell.java:290-296`) — после задачи 100; до неё — без картинки.
- `venue_type` всегда пустой: для непустого iOS качает значок с `ss3.4sqi.net` (`VenueIconResources.swift:39, 137-139`).

### `upload.getWebFile` и `webfile_dc_id`
- Запрос идёт на `webfile_dc_id`: Android `FileLoadOperation.java:411`, iOS `RemoteStorageConfiguration.swift:6-16`, tdesktop `file_download_mtproto.cpp:64`, Web K `apiFileManager.ts:200, 381, 450`.
- `dcId: 4` в `inlineHelper.ts:228` Web K на маршрут не влияет: `requestWebFilePart` шлёт на `this.webFileDcId` (`apiFileManager.ts:363-381, 440-452, 931`). Патч Web K и псевдоним DC 4 в gnetway/session не нужны.
- Клиенты Pluma знают только DC 1; запрос к неизвестному DC tgnet держит без ответа (`ConnectionsManager.cpp:2584-2590`). Отсюда `webfile_dc_id = 1`. До первого `help.getConfig` у свежей установки по умолчанию 4 (Android `MessagesController.java:1836`, iOS `RemoteStorageConfiguration.swift:14`, tdesktop `mtproto_config.cpp:27`): такие ранние запросы веб-файлов зависают до перезапуска клиента. Это касается только миниатюр, запрошенных в первые секунды после установки.
- Схема: `inputWebFileLocation#c239d686 url access_hash`, `webDocument#1c570ed1 url access_hash size mime_type attributes`, `upload.webFile#21e753bc size mime_type file_type mtime bytes` (`api.tl:1019-1028`). Ошибки метода — `DOCUMENT_INVALID`, `LOCATION_INVALID`. Правил `offset`/`limit` для веб-файлов страница не даёт; для `upload.getFile` — кратность 4 КБ и не больше 1 МБ (core.telegram.org/api/files).

### Отправка результата (открытый вопрос проверки)
- `sendInlineBotResult` берёт результат из Redis (`InlineAnswer`), `outOfResult` для `botInlineMessageMediaAuto` с фото даёт `messageMediaPhoto`, с документом — `messageMediaDocument` (`messages.inline_handlers.go:346-366`).
- Проверено по коду: для `botInlineMediaResult` встроенных ботов ничего менять не нужно. Новое — только `hide_via` и счётчик отправок `@gif`.
- Ответ встроенного бота кладётся в Redis тем же `inlineAnswerKey` и на тот же срок (`inlineKeepOf`), что и ответ стороннего.

## Дизайн

### Учётные записи и реестр (часть A)
`pkg/inlinebots`:
```go
type Kind int // Gif, Photo, Venue
type Bot struct{ Id int64; Username string; Kind Kind; Geo bool }
var Gif    = Bot{Id: 1000101, Username: "gif", Kind: KindGif}
var Pic    = Bot{Id: 1000102, Username: "pic", Kind: KindPhoto}
var Places = Bot{Id: 1000103, Username: "places", Kind: KindVenue, Geo: true}
func ByID(id int64) (Bot, bool)
func Placeholder() string // "🔍"
```
- Id — диапазон служебных аккаунтов Pluma (антиспам 1000001, `pkg/antispam/antispam.go:40`); автоинкремент `users` с 136907713, пересечений нет.
- `teamgramd/deploy/sql/z_pluma-inline-bots.sql` сортируется после `z_init.sql`, его можно запускать снова:
  ```sql
  INSERT IGNORE INTO users (id, user_type, access_hash, first_name, username, phone, country_code, is_bot, account_days_ttl, created_at, updated_at)
  VALUES (1000101, <bot type>, <random63>, 'GIF', 'gif', '42101', '', 1, 180, …),
         (1000102, …, 'Pic', 'pic', '42102', …),
         (1000103, …, 'Places', 'places', '42103', …);
  INSERT IGNORE INTO username (username, peer_type, peer_id) VALUES ('gif', 2, 1000101), ('pic', 2, 1000102), ('places', 2, 1000103);
  INSERT IGNORE INTO bots (bot_id, creator_user_id, token, bot_nochats, bot_inline_geo, bot_inline_placeholder)
  VALUES (1000101, 0, '', 1, 0, '🔍'), (1000102, 0, '', 1, 0, '🔍'), (1000103, 0, '', 1, 1, '🔍');
  ```
  - Колонки и `user_type` бота — как в `botstore.Create` (`pkg/botstore/store.go:95-137`). Номер — служебный формат, как `42001` у антиспама.
  - Пустой `token` не совпадёт ни с одним токеном: `hashOf` всегда 64 hex-символа (`token.go:44-60`, `store.go:196-206`).
  - Если имя `places` уже занято человеком, `INSERT IGNORE` тихо не вставит строку `username`. Проверка при старте это поймает (ниже), а `docs/ops/inline-bots.md` даёт запрос для проверки до выката.
- Проверка при старте bff `bots` (`svc`): для каждого бота — `username` → его id, `users.is_bot = 1`, плейсхолдер не пуст, `bot_inline_geo` у `places`. Расхождение — `Errorf` и метрика `inline_bot_misconfigured{bot}` = 1. Такой бот отвечает `BOT_INLINE_DISABLED`, остальные работают.
- `contacts.resolveUsername`: убрать ветку `GetBotIdByName` (`contacts.resolveUsername_handler.go:41-44`), имя ищет только `UserResolveUsername`. `vars.go` не трогать: константы используются только там (проверить `grep` при работе). Белый список коротких имён в `user.resolveUsername_handler.go:36-39` (`gif`, `pic`, `bing`) не менять: `places` длиннее 5.
- `pluma-admin bot token|set <имя>`: id из реестра — ошибка «built-in bot, managed by the server» (`tools/pluma-admin/bot.go`). `bot list` их показывает.
- `botstore.BotInfo`: для ботов реестра описание — `langpack.InlineBotText(key, v.Lang)`, если своих текстов на языке нет. Ключи: `PlumaInlineGifAbout`, `PlumaInlinePicAbout`, `PlumaInlinePlacesAbout`. Последний включает «© OpenStreetMap contributors» (ODbL); в `PlumaInlinePicAbout` написано, что картинки — из открытого веба по поиску. Загрузчик общий с `mail.json`: вынести `loadTexts(name, data)` и `pick` (`pkg/langpack/mail.go`), не меняя `MailText`.

### Конфигурация клиентов (часть A)
- `teamgramd/bin/config.json`: `gif_search_username: gif`, `venue_search_username: places`, `img_search_username: pic`. Тест `help.getConfig_handler_test.go`: все три поля заданы. (`webfile_dc_id: 1` и его проверка в том же тесте — часть B.)
- `teamgramd/bin/app_config.json`: `"stories_venue_search_username": "places"`, `"gif_search_emojies": []`. Список эмодзи (`👍 👎 😍 😂 😮 🙄 😥 😡 🥳 😎`, `MessagesController.java:1948-1957`) ставит оператор, когда у `@gif` появится поиск по тексту (`docs/ops/inline-bots.md`); `docs/ops/app-config.md` — две строки.

### Обработка запроса к встроенному боту (часть C)
В `MessagesGetInlineBotResults` после проверок (`messages.inline_handlers.go:88-110`): если `inlinebots.ByID(bot.Id())` — ветка `c.builtinResults(...)` вместо `PushToBot`/`WaitInlineAnswer`:
1. Ключ кэша — `dao.InlineCacheKey(botId, 0, "builtin", geo, query, offset)`: ответы общие для всех, вид чата не важен. `geo` перед ключом и запросом округляется до 3 знаков (≈110 м), чтобы соседние точки попадали в кэш. Кэш есть — ответ из Redis, как сейчас.
2. Предел на пользователя: 60 запросов к встроенным ботам в минуту (`logincode.Limiter`, ключ `inline_builtin_rate_<user>`). Сверх него — `FLOOD_WAIT_X`.
3. `EnterWait`/`LeaveWait` как у сторонних (предел 32 одновременных).
4. Поставщик по виду бота: `Provider.Search(ctx, Query{Text, Offset, Geo, Lang}) (Page, error)`, где `Page{Items []Item; Next string; CacheTime int32}`. `Item` — источник медиа (ключ, URL, размеры, длительность) или место.
5. Для медиа: `inlinemedia.Ensure(ctx, items)` — уже импортированные берутся из `inline_media`, новые импортируются (ниже). Общий срок ответа 7 с: что не успело — пропускается, `next_offset` продолжает после последнего опрошенного элемента.
6. Результаты: `@gif` — `botInlineMediaResult{id, type: "gif", document, send_message: botInlineMessageMediaAuto{}}`; `@pic` — `botInlineMediaResult{id, type: "photo", photo, send_message: MediaAuto}`; `@places` — `botInlineResult{id, type: "venue", title, description: адрес, send_message: botInlineMessageMediaVenue{geo, title, address, provider: "osm", venue_id: "node/123", venue_type: ""}}`. `id` результата — ключ источника в hex (≤ 64 байт).
7. `queryId := dao.NewQueryId()`; новый `dao.PutInlineAnswer(ctx, queryId, a)` кладёт ответ на `inlineKeepOf(cacheTime)` без проверки владельца (`Bot` = id бота); `CacheInline` — если `cacheTime > 0`. Ответ — `inlineResults(queryId, a, botId)`.
8. Ошибка поставщика или срок вышел — пустой список с `cache_time = 0`, счётчик и `Errorf` без текста запроса. Клиенты показывают «ничего не найдено». 500 tgnet повторил бы сам.

Пределы и сроки:
| Что | Значение |
|---|---|
| Результатов на страницу | `@gif` 20, `@pic` 12, `@places` 20 |
| `cache_time` | `@gif` популярное 600, поиск 3600; `@pic` 3600; `@places` текст 3600, точка 300 |
| Импорт | 6 параллельно на запрос, 16 на процесс; 5 с на файл; фото ≤ 8 МБ на входе (как `pkg/webpage`), GIF ≤ 2 МБ |
| Бюджет импорта | `INLINE_IMPORT_DAILY_BYTES`, по умолчанию 1 ГиБ в сутки по UTC (Redis `inline_import_bytes_<yyyymmdd>`); сверх — только уже импортированное |
| Поставщики | SearXNG 4 одновременных запроса, 6 с; Photon 8, 3 с |

`hide_via` (часть C): в `MessagesSendInlineBotResult` `ViaBotId` не ставится, если `in.HideVia` и `inlinebots.ByID(answer.Bot)`. Для `@gif` после успешной отправки — `inlinemedia.CountGifSend(documentId)`. Его ошибка логируется, на ответ не влияет: сообщение уже ушло.

### Импорт медиа (часть C, `pkg/inlinemedia`)
- `pkg/webfetch` — вынести из `pkg/webpage` без изменения поведения: `safe.go` (только публичные адреса, `dialControl`), загрузку картинки с пределом (`fetch.go:126-147`), `pictureJPEG` (`build.go:156`), `RPCPhotos.UploadPhoto` (`photos.go`). `pkg/webpage` импортирует их оттуда; его тесты не меняются и проходят.
- Новое в `pkg/webfetch`:
  - `Fetch(ctx, url, accept, max)` с проверкой `Content-Type`;
  - `UploadAnimation(ctx, owner, mp4, w, h, duration)`: части в dfs (`DfsWriteFilePartData`), затем `media.uploadedDocumentMedia` с `inputMediaUploadedDocument{mime_type: "video/mp4", attributes: [documentAttributeAnimated, documentAttributeVideo{w,h,duration}, documentAttributeFilename{"animation.mp4"}]}`. Ветка mp4 медиасервиса (`media.uploadedDocumentMedia_handler.go:50-63`) делает миниатюру первым кадром.
- Владелец файлов — id бота (`creator`).
- Фото пережимается в JPEG ≤ 1280 px по большей стороне. `media.uploadPhotoFile` делает размеры, как у превью ссылок.
- Строка `inline_media` пишется после того, как файл создан. Сбой между ними оставит файл без строки (учитывается метрикой `inline_media_orphans_total` по ошибке записи), повторный запрос импортирует заново.
- Одна загрузка на ключ одновременно (`singleflight`). Две реплики bff могут импортировать одно и то же дважды: вставка `INSERT … ON DUPLICATE KEY UPDATE last_served_at`, вторая копия остаётся без строки (принято, редко).
- Ключ источника: `sha256(provider ‖ 0x00 ‖ id или URL)[:16]`. Сам URL не хранится.

### Поставщики (части D, E, F)
- **`local` (`@gif` без KLIPY, часть D).**
  - Пустой запрос — до 200 документов: отправки через `@gif` за 30 дней (`inline_gif_sends`) плюс GIF, сохранённые не меньше чем тремя разными людьми (`SELECT document_id, COUNT(*) c FROM user_saved_gifs GROUP BY document_id HAVING c >= 3`), по сумме. Список кэшируется в Redis на 10 минут. Страницы — смещение в списке.
  - Документы — `media.getDocumentList` по id; удалённые и не-анимации пропускаются.
  - Запрос с текстом — пустой список, `cache_time` 600.
  - Импорта нет.
- **`searxng` (`@pic`, часть E).**
  - Запрос: `GET {SEARXNG_URL}/search?q=&categories=images&format=json&pageno=&safesearch=1&language={lang}`. Подходят только результаты с `img_src` по http(s); `img_format`/`resolution` — подсказка.
  - Картинка — `img_src`; не вышло — `thumbnail_src`. Обе — через `pkg/webfetch` (только публичные адреса).
  - `next_offset` = `p<pageno>.<index>`.
  - Пустой запрос — пустой список: у Android нет «популярного» для картинок (`PhotoPickerActivity` ищет только по тексту).
- **`photon` (`@places`, часть F).**
  - Без точки: `GET {PHOTON_URL}/api?q=&limit=20&lang=`.
  - С точкой: текст есть — `/api?q=&lat=&lon=&limit=20&lang=`; текста нет — `/reverse?lat=&lon=&limit=20&radius=1&lang=`.
  - Из GeoJSON: `name` (иначе улица и дом) — `title`, адрес из `street`, `housenumber`, `postcode`, `city`/`state`, `country` — `address`, `osm_type`+`osm_id` — `venue_id` (`node/…`, `way/…`, `relation/…`). Результаты без названия и адреса пропускаются.
  - Страницы: Photon страниц не знает. `next_offset` = число уже отданных; следующая страница — запрос с `limit = offset + 20` (не больше 60), отдаются новые. Дальше 60 — конец.
  - Язык — `c.MD.LangCode`, при неподдержанном Photon сам возьмёт местное название (docs/api-v1.md).
- **`klipy` (часть G, после согласия).** По спецификации KLIPY: `GET api/v1/{key}/gifs/search|trending`, `per_page = 20`, без `customer_id`, порядок как в ответе, без смешивания с `local`, импорт версии `mp4` `md`/`sm` ≤ 2 МБ, ревалидация и удаление из `inline_media` по их правилам (`provider_ref`). Плейсхолдер «Search KLIPY» — по договорённости с KLIPY (ADR 0062, п. 6). С KLIPY `gif_search_emojies` — список выше; эмодзи-категории переводятся в слова KLIPY таблицей в коде.

### `upload.getWebFile` (часть B, общая с задачей 100)
`pkg/webfile`:
```go
type Location interface{ Key() string } // the cache key
type Request struct{ UserId int64; LangCode string } // who asks: per-user limits, the language of a map
type Source interface { // one per kind of InputWebFileLocation
    Resolve(ctx, Request, *mtproto.InputWebFileLocation) (Location, error) // LOCATION_INVALID
    Open(ctx, Request, Location) (File, error)   // WEBFILE_NOT_AVAILABLE or the source's own error
    Cached() bool // true: the source keeps the same bytes for every part itself (the map, Redis); Server does not cache it
}
type File struct{ Bytes []byte; MimeType string; Mtime int32 }
type Server struct{ sources map[string]Source; cache *DiskCache }
func (s *Server) Register(predicate string, src Source)
func (s *Server) Get(ctx, Request, loc, offset, limit int32) (*mtproto.Upload_WebFile, error)
func Sign(key []byte, url string) int64
```
- Общие правила `Get` (одни для всех видов, задача 100 их не меняет):
  - `offset < 0` — `OFFSET_INVALID`; `limit <= 0`, `limit > 1 МиБ` — `LIMIT_INVALID` (как `upload.getFile`, `upload.getFile_handler.go:160-182`);
  - вида нет среди зарегистрированных (в том числе `inputWebFileGeoPointLocation`, пока 100 не влита или карты выключены) — `LOCATION_INVALID` (ошибка из документации метода);
  - байты всех частей одного файла — из одной копии: `Source` с `Cached() = false` кэшируется на диске `Server` (ниже), с `Cached() = true` держит копию сам (tdesktop отменяет загрузку, если `size` частей разный, `file_download_mtproto.cpp:170-181`);
  - `offset >= size` — пустые `bytes` с полным `size`;
  - ответ `upload.webFile{size, mime_type, file_type, mtime, bytes[offset:offset+limit]}`, где `file_type`: `image/jpeg` → `storage.fileJpeg`, `image/png` → `filePng`, `image/gif` → `fileGif`, `image/webp` → `fileWebp`, `video/mp4` → `fileMp4`, `audio/mpeg` → `fileMp3`, иначе `fileUnknown`.
  - Web K берёт `size` из первого ответа, если не знал (`apiFileManager.ts:983`).
- Кэш: файлы целиком на диске bff (`WEBFILE_CACHE_DIR`, по умолчанию `/tmp/webfile` в контейнере), предел `WEBFILE_CACHE_BYTES` (2 ГиБ), вытеснение по давности, TTL 24 ч, одна загрузка на ключ (`singleflight`). После перезапуска кэш может быть пуст — файлы просто качаются заново.
- `urlSource` (`inputWebFileLocation`):
  - `access_hash` должен быть равен `Sign(WEBFILE_HMAC_KEY, url)` (HMAC-SHA256 с меткой `pluma-webfile-v1\x00`, первые 8 байт как int63), иначе `LOCATION_INVALID`;
  - загрузка через `pkg/webfetch`: только http(s) и публичные адреса, до 5 переадресаций, 10 с, ≤ 20 МБ;
  - типы: `image/*`, `video/mp4`, `audio/mpeg`, `audio/ogg`; иначе `WEBFILE_NOT_AVAILABLE`.
- Обработчик `app/bff/files/internal/core/upload.getWebFile_handler.go`: `inputWebFileAudioAlbumThumbLocation` — как сейчас (`WEBFILE_NOT_AVAILABLE`); остальное — через `Server.Get` (`inputWebFileGeoPointLocation` до задачи 100 — `LOCATION_INVALID` вместо `ErrEnterpriseIsBlocked`). `Request` — из метаданных (`UserId`, `LangCode` сессии). Задача 100 регистрирует свой `Source` (`Register`) и больше ничего в общей части не меняет.
- `zrpc.DontLogContentForMethod("/mtproto.RPCFiles/upload_getWebFile")` в `app/bff/files/helper.go`: в запросе адреса и координаты.
- `teamgramd/bin/config.json`: `webfile_dc_id: 1` (было 4); тест `help.getConfig_handler_test.go`: `webfile_dc_id == this_dc`.
- Подпись в bots: `webDocument()` (`messages.inline_handlers.go:309-316`) строит `webDocument{url, access_hash: Sign(...)}` вместо `webDocumentNoProxy`. Ключ — в конфиге bots и files.
- Сессия: `waitsForBot` (`main_auth_wrapper.go:769-777`) переименовать в `runsAside` и добавить `*mtproto.TLUploadGetWebFile` с комментарием: загрузка с чужого хоста не держит остальные запросы ключа. Обновить тест.

### Таблицы (часть C)
`teamgramd/deploy/sql/migrate-20261119-inline-bots.sql`, повторный запуск безопасен (`IF NOT EXISTS`):
```sql
CREATE TABLE IF NOT EXISTS `inline_media` (
  `provider` varchar(16) NOT NULL,          -- 'searxng', 'klipy'
  `source_key` binary(16) NOT NULL,          -- sha256(provider, id or URL)[:16]
  `kind` tinyint(4) NOT NULL,                -- 1 photo, 2 gif
  `media_id` bigint(20) NOT NULL,            -- photo id or document id
  `provider_ref` varchar(128) NOT NULL DEFAULT '', -- the provider's id, for revalidation (KLIPY)
  `bytes` int(11) NOT NULL,
  `created_at` bigint(20) NOT NULL,
  `last_served_at` bigint(20) NOT NULL,
  PRIMARY KEY (`provider`, `source_key`),
  KEY `served` (`last_served_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `inline_gif_sends` (
  `document_id` bigint(20) NOT NULL,
  `day` date NOT NULL,
  `sends` int(11) NOT NULL,
  PRIMARY KEY (`document_id`, `day`),
  KEY `day` (`day`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```
`inline_gif_sends` старше 30 дней удаляет тот же код при подсчёте популярного (`DELETE … WHERE day < ? LIMIT 1000`).

### Обновления другим устройствам и людям
Новых нет. Отправленный результат — обычное сообщение через `msg` (`MsgSendMessageV2`) со всеми его обновлениями; без `via_bot_id` при `hide_via` у ботов реестра.

### Ошибки
| Ситуация | Ответ |
|---|---|
| Бот реестра расходится с базой | `BOT_INLINE_DISABLED` |
| Частые запросы к встроенным ботам | `FLOOD_WAIT_X` |
| Поставщик недоступен, срок вышел, бюджет исчерпан и ничего не импортировано | `messages.botResults` без результатов, `cache_time = 0` |
| `upload.getWebFile`: чужой `access_hash`, не тот вид | `LOCATION_INVALID` |
| `upload.getWebFile`: адрес не публичный, тип, размер, ошибка хоста | `WEBFILE_NOT_AVAILABLE` |
| `upload.getWebFile`: `offset < 0` | `OFFSET_INVALID` |
| `upload.getWebFile`: `limit` вне 1…1 МиБ | `LIMIT_INVALID` |
| `upload.getWebFile`: вид без зарегистрированного источника | `LOCATION_INVALID` |

### Поведение после перезапуска
- Ответы и кэши — в Redis, переживают перезапуск bff. Импорт, прерванный посередине, оставляет файл без строки; следующий запрос импортирует снова.
- Бюджет — счётчик Redis на сутки.
- Кэш веб-файлов — на диске контейнера и может пропасть, это нормально.
- Photon и SearXNG без состояния (кроме тома данных Photon). Перезапуск bff во время их недоступности — пустые ответы до возвращения.

### Наблюдаемость и logaudit
- Метрики:
  - `inline_builtin_requests_total{bot,result=ok|empty|error|limited}`, `inline_builtin_latency_seconds{bot}`;
  - `inline_import_total{kind,result}`, `inline_import_bytes_total{kind}`, `inline_media_orphans_total`, `inline_bot_misconfigured{bot}`;
  - `webfile_requests_total{kind,result}`, `webfile_cache_bytes`.
- Алерты (`codes.yml`, группа `inline_bots`): `InlineProviderFailing` (доля `error` > 50 % за 15 мин у бота), `InlineBotMisconfigured`, `InlineImportBudgetSpent` (бюджет исчерпан до 18:00 UTC).
- `tools/logaudit`: запрещённые имена в логах — `query`, `inlinequery`, `geo`, `webfileurl` (часть C) и общие для геоданных `lat`, `long`, `getlat`, `getlong`, `geopoint`, `getgeopoint` (часть B; задача 100 их не добавляет второй раз). Перед добавлением `query` проверить, что нынешние вызовы логов его не используют (иначе — точнее, `inlinequery`). Текст запроса, координаты и адреса веб-файлов в логи не пишутся.

## Порядок работ
Размер: S — до полудня, M — одна сессия. Параллельность и общие файлы указаны у каждой части.

- **A. Реестр, учётные записи, конфигурация, тексты** (M). `pkg/inlinebots`, `z_pluma-inline-bots.sql`, проверка при старте, `contacts.resolveUsername`, `pluma-admin`, `config.json`, `app_config.json`, `pkg/langpack/inlinebots.json` (37 языков, вычитка как у 076) и общий загрузчик, `botstore.BotInfo`. Можно параллельно с B.
- **B. `pkg/webfetch`, `pkg/webfile`, `upload.getWebFile`** (M). Вынос из `pkg/webpage`, `Server`, кэш, `urlSource`, подпись `webDocument` у сторонних ботов, `runsAside` в сессии, конфиг files и bots (`WEBFILE_HMAC_KEY`), `webfile_dc_id: 1`, `DontLogContentForMethod`, имена геоданных в `logaudit`. Можно параллельно с A, если A не трогает `config.json` в той же волне (у A там три имени ботов). Общий файл с C — `messages.inline_handlers.go` (B меняет только `webDocument()`). **Единственный владелец общей части `upload.getWebFile`; её ждёт задача 100 (части 3 и 5).**
- **C. Ветка встроенных ботов и импорт** (M). Интерфейс поставщика, `builtinResults`, `PutInlineAnswer`, предел, `pkg/inlinemedia`, `UploadAnimation`, миграция, `hide_via`, счётчик отправок, метрики. После A и B.
- **D. Поставщик `local`** (S). После C. Можно параллельно с E и F; общий файл — конфиг bots (`InlineBots`).
- **E. Поставщик `searxng` и контейнер** (S/M). `teamgramd/deploy/searxng/settings.yml`, `docker-compose.search.yaml`. После C, параллельно с D и F.
- **F. Поставщик `photon`, контейнер и данные** (M). `teamgramd/deploy/photon/Dockerfile` (Java 21, jar закреплённой версии со сверкой SHA-256), `docker-compose.places.yaml`, `teamgramd/deploy/photon/update.sh` (скачать дамп, проверить md5, импорт в новый том, переключить, удалить старый). Код поставщика — после C; контейнер и скрипт можно делать сразу. Параллельно с D и E.
- **G. Адаптер KLIPY** (M) — только после письменного согласия KLIPY и их спецификации кэша. В этой ветке не делается без них.
- **H. e2e: тестовые поставщики и тесты** (M). После D–F.
- **I. Документы** (S). `docs/ops/inline-bots.md`, `app-config.md`, `security-setup.md` (`WEBFILE_HMAC_KEY`, `SEARXNG_SECRET`), `monitoring.md`, `bots.md` (встроенные боты, `pluma-admin` их не меняет), строки `TESTING-BACKLOG`. Последней.

## Файлы
Добавить:
- `pkg/inlinebots/inlinebots.go`, `_test.go`.
- `pkg/inlinemedia/` (`media.go` — `Ensure`, бюджет, `CountGifSend`, `Popular`; `store.go`; тесты, `store_integration_test.go`).
- `pkg/webfetch/` (`safe.go`, `fetch.go`, `jpeg.go`, `upload.go` — перенос из `pkg/webpage` плюс `UploadAnimation`), тесты.
- `pkg/webfile/` (`webfile.go`, `cache.go`, `url.go`, `sign.go`), тесты.
- `app/bff/bots/internal/core/builtin.go` (ветка встроенных), `builtin_test.go`.
- `app/bff/bots/internal/inline/` (`provider.go`, `local.go`, `searxng.go`, `photon.go`, тесты на `httptest`).
- `pkg/langpack/inlinebots.json`, `inlinebots.go`, `inlinebots_test.go`.
- `teamgramd/deploy/sql/migrate-20261119-inline-bots.sql`, `z_pluma-inline-bots.sql`.
- `teamgramd/deploy/searxng/settings.yml`, `docker-compose.search.yaml`.
- `teamgramd/deploy/photon/Dockerfile`, `update.sh`, `docker-compose.places.yaml`.
- `test/e2e/fakeinline/` (`main.go`, `Dockerfile`): ответы как SearXNG (`/search`) и Photon (`/api`, `/reverse`), раздаёт картинки JPEG и mp4 из `testdata`, отдаёт запланированные ошибки и задержки.
- `test/e2e/inline_bots_test.go`, `test/e2e/webfile_test.go`.
- `docs/ops/inline-bots.md`.

Изменить:
- `app/bff/bots/internal/core/messages.inline_handlers.go` (ветка, `webDocument()`, `hide_via`, счётчик), `internal/dao/queries.go` (`PutInlineAnswer`), `internal/svc/service_context.go` (поставщики, проверка реестра), `internal/config/config.go` (`InlineBots`, `WebFileKey`).
- `app/bff/files/internal/core/upload.getWebFile_handler.go`, `internal/svc/service_context.go`, `internal/config/config.go`.
- `app/bff/usernames/internal/core/contacts.resolveUsername_handler.go` и его тест.
- `app/interface/session/internal/sess/main_auth_wrapper.go` (`runsAside`) и тест.
- `pkg/webpage/*.go` (импорт из `pkg/webfetch`, без изменения поведения).
- `pkg/botstore/store.go` (`BotInfo` для реестра).
- `pkg/langpack/mail.go` (общий загрузчик).
- `tools/pluma-admin/bot.go`.
- `teamgramd/bin/config.json`, `teamgramd/bin/app_config.json`, `app/bff/configuration/internal/core/help.getConfig_handler_test.go`.
- `app/bff/bff/internal/config/config.go`, `app/bff/bff/etc/bff.yaml`, `teamgramd/etc/bff.yaml`, `teamgramd/etc2/bff.yaml` (`InlineBots`: `SearxngURL`, `PhotonURL`, `ImportDailyBytes`; `WebFile`: `HMACKey`, `CacheDir`, `CacheBytes`).
- `.env.example`, `docker-compose.yaml` (переменные bff), `pkg/conf` (список переменных).
- `test/e2e/docker-compose.e2e.yaml`, `test/e2e/run.sh` (сервис `fakeinline`, адреса поставщиков на него; `WebFetch.AllowHosts: [fakeinline]` — только в конфиге e2e, в боевом его нет).
- `tools/logaudit/logaudit.go`, `teamgramd/deploy/prometheus/server/rules/codes.yml`, `docs/ops/monitoring.md`, `docs/ops/app-config.md`, `docs/ops/bots.md`, `docs/ops/security-setup.md`.

## Тесты
Модульные:
- `pkg/inlinebots`: `ByID`, имена совпадают с `config.json` (тест читает файл).
- `contacts.resolveUsername`: `music`, `youtube` резолвятся в людей и каналы с такими именами; `gif` — в 1000101 (подменный клиент биз-сервиса).
- `pkg/langpack`: `inlinebots.json` загружается, каждый ключ на всех 37 языках `own.json`, запасной язык.
- `botstore.BotInfo` для бота реестра: описание на языке зрителя, свои тексты имеют приоритет.
- `builtinResults` с подменным поставщиком:
  - вид результата по боту (`botInlineMediaResult` с документом и фото, `botInlineResult` с `MediaVenue`, `venue_type` пустой, `provider` `osm`);
  - `next_offset` ≤ 64 байт;
  - кэш — общий (`userId` 0, вид `builtin`), повтор не зовёт поставщика;
  - `geo` округляется;
  - ошибка и срок — пустой список и `cache_time 0`;
  - предел — `FLOOD_WAIT_X`;
  - неверно заведённый бот — `BOT_INLINE_DISABLED`.
- `sendInlineBotResult`: `hide_via` у бота реестра убирает `via_bot_id`, у стороннего — нет; результат `@gif` превращается в `messageMediaDocument`, `@pic` — в `messageMediaPhoto`, `@places` — в `messageMediaVenue`.
- `pkg/inlinemedia`: повтор ключа не импортирует; бюджет; срок 7 с режет страницу и `next_offset` продолжает; одна загрузка на ключ.
- `pkg/webfetch`: перенесённые тесты `pkg/webpage` (`safe_test.go`) зелёные на новом месте; `UploadAnimation` шлёт нужные атрибуты (подменный клиент media).
- `pkg/webfile`:
  - `Sign` стабилен и зависит от ключа;
  - чужой хэш — `LOCATION_INVALID`;
  - `offset`/`limit`: за концом, ровно конец, 1 МБ, больше 1 МБ, отрицательный;
  - `file_type` по MIME;
  - кэш: предел, вытеснение, TTL, одна загрузка на ключ;
  - адрес `127.0.0.1`/`10.x`/`[::1]` — `WEBFILE_NOT_AVAILABLE`.
- Поставщики на `httptest`: разбор ответа SearXNG (без `img_src`, `thumbnail_src` вместо него, нечитаемый JSON, 403 без `json`), Photon (forward, reverse, без названия, OSM-типы), `local` (порог трёх людей, сумма с отправками, удалённый документ пропущен).
- Сессия: `runsAside` для `upload.getWebFile` и обоих бот-методов.
- `tools/logaudit` ловит вывод текста запроса и координат.

Интеграционные (`pkg/testinfra`, настоящие MySQL и Redis; по одному пакету за раз):
- миграция и `z_pluma-inline-bots.sql` дважды подряд, затем `contacts.resolveUsername("gif")`/`("places")` через биз-сервис;
- `pkg/inlinemedia`: `inline_media` под гонкой двух импортов одного ключа (одна строка), `inline_gif_sends` (инкремент, сумма 30 дней, удаление старых), популярное из `user_saved_gifs` с порогом;
- `PutInlineAnswer` + `InlineAnswer` + `sendInlineBotResult` на настоящем Redis.

Сквозные (`pkg/mtclient`, стенд с `fakeinline`, сессия после `getState`):
- `TestInlineBotsConfig`: `help.getConfig` — три имени, `webfile_dc_id = 1`; `contacts.resolveUsername` для каждого — бот с `bot_inline_placeholder`, у `places` — `bot_inline_geo`.
- `TestInlinePic`: `getInlineBotResults(@pic, "cat")` → фото из dfs; `upload.getFile` фото отдаёт байты; `next_offset` даёт следующую страницу; `sendInlineBotResult` → сообщение с фото и `via_bot_id`; второй пользователь видит сообщение.
- `TestInlineGifTrending`: три пользователя сохраняют один GIF → пустой запрос к `@gif` у четвёртого отдаёт его документом; `sendInlineBotResult` с `hide_via` → без `via_bot_id`; запрос с текстом — пустой список.
- `TestInlinePlaces`: с `geo_point` и без; результат `venue`, отправка даёт `messageMediaVenue` с `provider = osm`, `venue_type = ""`.
- `TestInlineProviderDown`: `fakeinline` отвечает 503 и задержкой → пустой список, `cache_time = 0`, без ошибки.
- `TestWebFileProxy`: сторонний бот (как `TestBotAccount`) отвечает URL-миниатюрой на `fakeinline` → клиент получает `webDocument` с `access_hash`; `upload.getWebFile` частями даёт файл целиком; чужой `access_hash` — `LOCATION_INVALID`.
- Проверить падающим без изменения хотя бы два: `TestInlineBotsConfig` (на `master` `gif` резолвится в несуществующего 101) и `TestWebFileProxy` (на `master` — `webDocumentNoProxy` и `ErrEnterpriseIsBlocked`).

## Проверки перед сдачей (CachyOS)
- `go build ./...`; `go vet -tags integration,e2e -unreachable=false` затронутых пакетов; `go test` затронутых пакетов и импортирующих их (`pkg/webpage`, `pkg/botstore`, `pkg/langpack`, bff `bots`, `files`, `usernames`, `messages`, `webpage`, сессия).
- В конце партии: `-race`, интеграционные `pkg/inlinemedia` и e2e раздела «Тесты» со своими `E2E_PROJECT`/`E2E_DIR`, затем `test/e2e/run.sh down`.
- `make lint`, `go test ./tools/...`, `go test ./pkg/conf/...`, `go run ./tools/apicoverage` (`upload.getWebFile` → ok).
- Контейнеры: `docker compose -f … -f docker-compose.search.yaml config` и `… places.yaml config` проходят; SearXNG на CachyOS отвечает JSON на `categories=images`.
- Photon на CachyOS: импорт выбранных данных, замер диска, кучи, времени импорта и ответа (цифры — в `docs/ops/inline-bots.md`), до выката домой.
- На машине разработки ничего не собирать.

## Проверки для `docs/project/TESTING-BACKLOG.md`
- [ ] Android, iOS, Desktop, Web: вкладка GIF открывается без ошибок; при пустом поиске — популярное Pluma (или пусто, если GIF ещё никто не сохранял); выбранный GIF уходит без «через @gif».
- [ ] Набрать `@gif ` в каждом клиенте: виден плейсхолдер `🔍`, бот найден, пустой список без падения.
- [ ] Android: вложение → галерея → поиск картинок: результаты по запросу, листание вниз подгружает ещё, отправка — обычное фото.
- [ ] `@pic кот` в каждом клиенте: сетка фото, отправка с «через @pic».
- [ ] `@places кафе` в каждом клиенте; на Android — разрешить геопозицию боту, результаты рядом; отправленное место открывается картой; на iOS у места стандартный значок.
- [ ] Профиль `@gif`, `@pic`, `@places`: описание на языке клиента (русский, английский, арабский), у `@places` есть «© OpenStreetMap contributors».
- [ ] Сторонний инлайн-бот с картинками-миниатюрами: миниатюры видны во всех клиентах; в логе хоста бота адрес VPS, а не телефона.
- [ ] Во время теста `docker compose stop searxng`: `@pic` даёт пустой список, клиент не виснет; после `start` — снова работает.
- [ ] Пользователь или канал с именем `music` (или `youtube`) находится поиском по имени.

## Эксплуатация
- **Миграции:** `migrate-20261119-inline-bots.sql`, `z_pluma-inline-bots.sql` — сервис migrate, повторный запуск безопасен.
  - До выката: `SELECT username, peer_type, peer_id FROM username WHERE username IN ('gif','pic','places');` — у людей этих имён быть не должно (`docs/ops/inline-bots.md`, что делать, если занято).
  - Кэш пользователей в Redis не мешает: аккаунтов 1000101–1000103 раньше не было.
- **Переменные `.env`:**
  - `WEBFILE_HMAC_KEY` (32 случайных байта, base64; без него `upload.getWebFile` для URL выключен, сторонние боты отдают миниатюры как раньше `webDocumentNoProxy` — это отдельно сказано в ops);
  - `SEARXNG_SECRET` (секрет самого SearXNG);
  - `SEARXNG_URL` (`http://searxng:8080`, пусто — `@pic` выключен), `PHOTON_URL` (`http://photon:2322`, пусто — `@places` выключен);
  - `INLINE_IMPORT_DAILY_BYTES` (по умолчанию 1073741824);
  - позже `KLIPY_API_KEY` (часть G).
- **Контейнеры дома:**
  - `searxng` — закреплённый образ `searxng/searxng:<версия>`, `mem_limit` 512m (уточнить замером), без опубликованных портов, `read_only` кроме `/etc/searxng`, в `pluma.slice`. Ходит в интернет обычным маршрутом дома — через туннель с адреса VPS (ADR 0061). Возможны CAPTCHA и блокировки поисковиков: метрика и алерт `InlineProviderFailing` их покажут, движки меняются в `settings.yml`.
  - `photon` — своя сборка (Java 21), том `photon-data`, без доступа в интернет (сеть только сервисов), `mem_limit` по замеру (ориентир 6–8 ГБ, `-Xmx` на 1–2 ГБ меньше). Решить с владельцем, укладывается ли это в память дома (32 ГБ, `pluma.slice` 10 ГБ, медиастек рядом).
- **Диск:**
  - Photon: планета ≈ 95 ГБ (README); регионы владельца ≈ 85 ГБ (оценка по сжатым дампам: 55,4 из 63,2 ГБ планеты, не измерено). Обновление раз в месяц `update.sh` — нужен второй том того же размера на время импорта, либо простой `@places` на часы.
  - Импорт медиа: до 1 ГиБ в сутки по бюджету; реально меньше из-за повторов (оценка: 20 GIF × ~0,3 МБ `hd` mp4 на новый запрос, фото ~150 КБ после пережатия). Метрика `inline_import_bytes_total`; при росте — задача на сборку мусора.
  - Кэш веб-файлов — до 2 ГиБ в контейнере bff.
- **Сборка образов** — на CachyOS, потом образы домой, как у остальных сервисов. Кэш сборки Docker почистить после.

## Шаги владельца
1. Решить про KLIPY (ADR 0062, п. 4.1). Если да — написать на developers@klipy.com с описанием серверной интеграции:
   - сервер ищет GIF и берёт популярное по одному ключу, без `customer_id`;
   - выбранные и показанные GIF хранятся в Pluma, отправленный GIF остаётся в истории чата;
   - порядок выдачи не меняется, выдача не смешивается с другими источниками;
   - плейсхолдер — символ `🔍` (слово в плейсхолдере не локализуется);
   - около 100 пользователей.

   Попросить спецификацию кэша и ревалидации и боевой ключ. Ответ и ключ — сюда, ключ — только в `.env` на сервере.
2. Данные Photon: владелец выбрал регионы «Европа, Россия, СНГ, Америка» (2026-11-15, как для карт 100). Часть F проверяет на CachyOS импорт нескольких дампов в одну базу; получилось — регионы, нет — планета (~95 ГБ, разница ~10 ГБ). Отдельного вопроса владельцу нет.
3. Подтвердить память под Photon дома после замера на CachyOS. Сумма потолков партии превышает нынешний `MemoryMax=10G` `pluma.slice`: Pluma ~3,6 ГБ + `translate` 2,5–4 (101) + `maps` до 1,5 (100) + `photon` 6–8 + `searxng` ~0,5 ≈ 14–17 ГБ (оценки, не замер). Нужно решение владельца: поднять потолок `pluma.slice` (дом — 32 ГБ, медиастек до 12 ГБ) или отказаться от части сервисов.

## Решения, требующие подтверждения владельцем
1. KLIPY по стандартным условиям не подходит, подходит только с их письменным согласием на серверный кэш; до него `@gif` — популярное внутри Pluma без поиска по тексту.
2. Популярное из сохранённых GIF — только документы, которые сохранили не меньше трёх разных людей. Личный GIF одного человека другим не покажется.
3. Текст запроса `@pic` уходит поисковикам через SearXNG с адреса VPS (как адреса ссылок при превью). Пользователь и его IP не видны.
4. Имена ботов `GIF`, `Pic`, `Places` и плейсхолдер `🔍` — без перевода: объект пользователя у Pluma один для всех зрителей. Описания ботов переводятся.
5. Импортированные файлы не удаляются (в dfs нет удаления, сборка мусора — отдельная задача); рост ограничен бюджетом 1 ГиБ в сутки.
6. Photon: регионы владельца, если импорт нескольких дампов в одну базу проходит на CachyOS; иначе планета (разница ~10 ГБ, импорт планеты описан в README).
7. `webfile_dc_id = 1`; свежая установка, запросившая веб-файл до первого конфига, получит его только после перезапуска клиента.
8. Пределы: 60 запросов в минуту на человека к встроенным ботам, 7 с на ответ, страницы 20/12/20, `cache_time` по таблице.
9. `WEBFILE_NOT_AVAILABLE`, `OFFSET_INVALID` и `LIMIT_INVALID` у `upload.getWebFile` — выбор сервера (как у `upload.getFile`): страница метода называет только `DOCUMENT_INVALID` и `LOCATION_INVALID`. Одни для веб-документов и карт (100).

## Не проверено
- Что настоящий `@gif` Telegram отвечает документами, а `@pic` — фото на серверах Telegram. Вывод из фильтра Web K (`appGifsManager.ts:81`) и из того, что Android качает URL сам; дизайн на этом не держится — он следует из поведения клиентов.
- Имена, описания и плейсхолдеры настоящих `@gif`/`@pic` Telegram; размер их страниц.
- Есть ли у KLIPY бесплатный боевой доступ без рекламы и что они ответят про серверный кэш. Документация цены не называет; «бесплатно» — только в блоге KLIPY и пересказах.
- Объём RAM SearXNG и Photon, время импорта Photon, размер данных регионов (только оценка по сжатым дампам).
- Можно ли импортировать в одну базу Photon несколько JSON-дампов регионов (`photon.jar import -import-file` читает один поток; склейка не проверена).
- Какие языки названий есть в дампах GraphHopper (влияет на `lang`).
- Устойчивость SearXNG к блокировкам поисковиков с адреса Hetzner.
- Условия Openverse и других открытых источников GIF: страница API не загрузилась.
- Что кнопка «места рядом» iOS недоступна при `locationPickerVenuesEnabled = false` во всех путях: проверены `LocationPickerControllerNode.swift:529, 792`.

## Результат
Сделаны встроенные боты `@gif` и `@pic` (части A–E, H, I). Сервер отвечает за них сам, а медиа приходят клиентам только с сервера Pluma, в том числе миниатюры сторонних инлайн-ботов (подписанный прокси `upload.getWebFile`). `@places` не делаем (решение владельца 2026-11-16). Часть G (KLIPY) ждёт письменного согласия KLIPY. Код в партии `batch/20261115`, на прод не выкачен.

- **A.** Реестр `pkg/inlinebots` и учётные записи ботов (`z_pluma-inline-bots.sql`; запуск можно повторять, имя, занятое человеком, не трогается). bff проверяет учётные записи при старте и потом; несовпадение видно по метрике `inline_bot_misconfigured`. Клиенты находят ботов через `gif_search_username` и `img_search_username`. `resolveUsername` читает только таблицу имён: прежние жёстко заданные имена ботов больше не перекрывают настоящих людей. `pluma-admin` не меняет встроенных ботов. Описания ботов переводятся (`pkg/langpack`).
- **B.** `upload.getWebFile` с подписью адресов (`WEBFILE_HMAC_KEY`), `pkg/webfile`, загрузчик `pkg/webfetch` (вынесен из `pkg/webpage`), кэш на сутки. Запрос идёт в стороне от очереди ключа сессии. Без ключа миниатюры остаются неподписанными, как раньше.
- **C.** Ветка встроенных ботов в `messages.getInlineBotResults`. Поставщики подключаемые, медиа один раз импортируются в dfs (`pkg/inlinemedia`, таблица `inline_media`). Есть дневной бюджет импорта (`INLINE_IMPORT_DAILY_BYTES`), предел на человека и общий кэш ответов. GIF из вкладки уходят с `hide_via`.
- **D.** `@gif` отдаёт популярное внутри сервера: GIF, сохранённые не меньше чем тремя людьми, плюс отправленные через `@gif` за 30 дней; всего до 200, по 20 на страницу, пересчёт раз в 10 минут. На текстовый запрос — пустой ответ, а не зависание.
- **E.** `@pic` ищет через SearXNG (`docker-compose.search.yaml`, `SEARXNG_URL`, `SEARXNG_SECRET`). Без него ответ пустой.
- **H.** Стенд `fakeinline` и сквозные тесты: конфиг, `@pic`, популярное `@gif`, недоступный поставщик, прокси веб-файлов. Хосты стенда разрешены только его `bff.yaml` (`WebFetch.AllowHosts`).
- **I.** `docs/ops/inline-bots.md`, тревоги Prometheus, ссылки из `bots.md`, `app-config.md`, `monitoring.md`, `security-setup.md`.

**Отклонения от брифа.**
- Без мест: нет бота `places`, поставщика Photon, части F и `KindVenue`. `venue_search_username` в `help.getConfig` снова `foursquare`, `stories_venue_search_username` не задан — как до задачи.
- Часть G не делалась.
- Шаг владельца 3 (память под Photon) отпал вместе с Photon.

**Решения.** Подтверждены владельцем: поставщики (`@gif` — популярное внутри Pluma, `@pic` — SearXNG, KLIPY — только с письменным согласием) и отказ от мест. Ждут подтверждения п. 2–5, 7–9: порог в три человека; запросы `@pic` идут к поисковикам с адреса VPS; имена и плейсхолдер без перевода; импортированные файлы не удаляются; `webfile_dc_id = 1`; пределы; коды ошибок `upload.getWebFile`.

**Известное.**
- Импортированные файлы не удаляются: в dfs нет удаления. Рост ограничен бюджетом импорта.
- SearXNG ходит к поисковикам с адреса VPS, поэтому возможны блокировки и CAPTCHA.
- Предел памяти SearXNG (512 МБ) — оценка; на CachyOS было около 170 МБ.

**Не проверено.** В клиентах — пункты задачи 099 в `TESTING-BACKLOG.md`. SearXNG на проде не запускался.

**Проверка.** Партия `batch/20261115`, полный прогон на CachyOS на `2c1b155cf`: `go build ./...` и `go vet -tags integration,e2e -unreachable=false` обоих модулей; `go test ./...` — 237 пакетов ok, proto — 3; `go test ./tools/...` (с журнальным аудитом) — 6; `-race` — 191 пакет ok; `make lint` — 0 замечаний; интеграционные тесты — 70 пакетов по одному, все ok; e2e на свежем стенде — 300 PASS, 0 FAIL, 2 SKIP (замер задержки и ручной тест). На прод не выкачено, в клиентах владельцем не проверено.
