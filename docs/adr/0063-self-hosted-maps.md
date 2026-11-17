# 0063. Карты: статические превью рисует свой tileserver-gl дома по векторным данным Protomaps, отдаёт `upload.getWebFile`

- Статус: accepted (2026-11-16). Владелец подтвердил регион (Европа, Россия, СНГ, обе Америки); пункты «Что осталось подтвердить владельцу» ждут его слова.
- Задача: 100 (ветка `task/100-static-maps`). Обслуживается `upload.getWebFile` с `inputWebFileGeoPointLocation`; меняется `webfile_dc_id` в `help.getConfig`.
- Связано: ADR 0057 и 0061 (открытое содержимое не покидает дом), ADR 0056 (свой сервис рядом с сервером за узким HTTP-интерфейсом — тот же приём), задача 027 (пункт «карта в геопозиции» закрывается здесь), задача 099 (общая часть `upload.getWebFile`), задача 105 (адрес бизнеса — та же картинка).

## Контекст

### Кто и как просит карту (исходники клиентов в `~/pluma-work/reference-src-new`)
- **Android** при `static_maps_provider = "telegram"` (у Pluma так и есть, `teamgramd/bin/config.json:54-56`; без поля Android сам ставит `"telegram"`, `MessagesController.java:5707-5712`) грузит превью через `upload.getWebFile(inputWebFileGeoPointLocation)`: `WebFile.createWithGeoPoint` (`messenger/WebFile.java:31-46`) — `w`, `h` в dp, `zoom` 15, `scale = min(2, ceil(density))`, ожидаемый тип `image/png`, ключ кэша клиента `maps_%.6f_%.6f_w_h_zoom_scale.png`. Вызовы: пузырь геопозиции, места и трансляции (`ui/Cells/ChatMessageCell.java:8783, 8828, 8860`), уведомление 100×100 (`PopupNotificationActivity.java:951`), инлайн-результат 72×72 (`ContextLinkCell.java:295`), опросы до 36×36 и z13 (`poll/attached/PollAttachedMediaLocation.java:44`, `poll/buttons/PollButtonDrawable.java:257`), адрес бизнеса с `access_hash = 0` (`ui/Business/LocationActivity.java:322`, `ProfileLocationCell.java:112`).
- **tdesktop** — тот же запрос (`storage/download_manager_mtproto.cpp:542-561`) на DC из `webfile_dc_id` (`storage/file_download_mtproto.cpp:64`, `mtproto/mtproto_config.cpp:257`). Параметры (`data/data_location.cpp:95-110`): `scale = 1 + (cScale·DPR)/200`, `zoom = 13 + (scale − 1)`, `w, h = locationSize / scale`. Масштаб интерфейса `cScale` — 50…300 (`kScaleMax = 300`, `lib_ui/ui/style/style_core_scale.h` в desktop-app/lib_ui, прочитано 2026-11-15), `DevicePixelRatio()` — целое, поэтому `scale` доходит до 4 (300 % × DPR 2) и в редких сочетаниях выше (300 % × DPR 3 = 5). Ответ принимается только если `size` во всех частях один и тот же (`file_download_mtproto.cpp:170-181`, иначе загрузка отменяется).
- **Web K** — тот же запрос (`helpers/getWebFileLocation.ts:3-23`) на `config.webfile_dc_id` (`lib/appManagers/apiFileManager.ts:200, 381-450`): пузырь `zoom` 16, ширина 277 или 420, высота `ширина·195/277` (`components/wrappers/geo.ts:63-84, 152`), превью 320×160 (`components/mapPreview.tsx`), профиль 48×48 z16 (`components/peerProfile.tsx:1371`). `scale = window.devicePixelRatio`; целые пишутся в `Int32Array` (`lib/mtproto/tl_utils.ts:34, 45, 114-120`), поэтому дробное усекается: 1,25 → 1, 295,7 → 295.
- **iOS** карту у сервера не просит: снимок рисует MapKit на устройстве (`LocationResources/Sources/MapResources.swift:74-85`, `MKMapSnapshotter`, `scale = 2`, JPEG 0,9); вне сгенерированного кода `inputWebFileGeoPointLocation` не встречается.
- **Метку и аватар трансляции рисует клиент** (tdesktop `history_view_location.cpp:551-552`, Web K `geo.ts:120-147`); в картинке сервера метки нет.
- **Смысл `zoom` — «гугловский», мир из 256-пиксельных тайлов.** Web K считает метры на пиксель как `156543.03392·cos(lat)/2^zoom` (`geo.ts:170-172`) и по ним рисует круг точности; Android для трансляции сдвигает центр по формуле Меркатора с миром `2^28` на z21 — то есть 256·2^z пикселей на z (`ChatMessageCell.java:8776-8780`). Картинка с другим смыслом масштаба разъедется с этими кругами и сдвигами.
- **Трансляция геопозиции.** Каждая правка сообщения даёт новую точку; Android строит новый `WebFile` и плавно меняет картинку (`ChatMessageCell.java:8781-8784, 8886-8897`), Web K перерисовывает пузырь. От сервера нужна только новая картинка по новым координатам.
- **Выбор точки в Desktop** — веб-вью с Mapbox GL JS 3.4.0 с api.mapbox.com (`ui/controls/location_picker.cpp:111-112, 406`) и геокодер Mapbox (`core/current_geo_location.cpp:70`). Нужны токены `tdesktop_config_map.maps`/`.geo` из app config (`inline_bots/bot_attach_web_view.cpp:286-295`); без токена `LocationPicker::Available()` ложно (`location_picker.cpp:343-351`), и пункт «Геопозиция» в меню вложений не появляется (`bot_attach_web_view.cpp:3127-3134`), опрос и адрес бизнеса точку не выбирают (`boxes/create_poll_box.cpp:2626-2627`, `settings/business/settings_location.cpp:287`). В `teamgramd/bin/app_config.json` у Pluma этого ключа нет.

### Документация (прочитано 2026-11-15)
- https://core.telegram.org/constructor/inputWebFileGeoPointLocation: `w`, `h` — «Map width/height in pixels before applying scale; 16-1024», `zoom` — «13-20», `scale` — «1-3», `access_hash` — «Access hash of the geoPoint».
- https://core.telegram.org/method/upload.getWebFile: ошибки `LOCATION_INVALID`, `DOCUMENT_INVALID`; запрос — «to the DC specified in the `webfile_dc_id`»; только пользователи.
- Что Telegram делает со значениями вне диапазонов (tdesktop шлёт `scale` 4), какой у его картинки формат, есть ли подпись данных — **не проверено**: ответов настоящего сервера мы не снимали.

### Сервер сегодня
- `app/bff/files/internal/core/upload.getWebFile_handler.go:33-44` — всё, кроме обложки альбома, отвечает `ErrEnterpriseIsBlocked`; карта не работает на Android, Desktop и Web K.
- `webfile_dc_id: 4` (`teamgramd/bin/config.json:59`), а у Pluma один DC 1 (`this_dc: 1`, `dc_options: []`); патченые Android и Desktop знают только DC 1 (`clients/patches/android.md:74-93`, `desktop.md:53, 94-102`). Значение по умолчанию у клиентов тоже 4 (Android `MessagesController.java:1834-1836`, tdesktop `mtproto_config.cpp:27`), поэтому оно должно прийти из конфига.
- `access_hash` у точек Pluma всегда 0: `MakeGeoPointByInput` его не заполняет (`third_party/teamgram-proto/mtproto/geo_point.go:31-36`).

### Откуда брать картинку
- **Чужой сервис (OSM, Google, Яндекс, Mapbox) за прокси сервера** — координаты из сообщений уходят из дома в открытом виде, против ADR 0057/0061. Публичные тайлы OSM к тому же нельзя использовать для продукта: «best effort», блокировка «without notice», запрет предзагрузки (https://operations.osmfoundation.org/policies/tiles/, прочитано 2026-11-15).
- **Свой рендер дома.** tileserver-gl (maptiler/tileserver-gl, выпуск v5.7.0 от 2026-10-23; образ `maptiler/tileserver-gl:v5.7.0`, amd64/arm64, около 282 МБ) рисует статические картинки `/styles/{id}/static/{lon},{lat},{zoom}/{w}x{h}[@{s}x].{png}` (https://tileserver.readthedocs.io/en/latest/endpoints.html; статических картинок нет в `-light`). Проверено по исходникам v5.7.0:
  - `src/render_mode.js:10-13` — для статических картинок MapLibre получает `zoom − 1`, то есть **zoom запроса уже в 256-пиксельном смысле**, как у клиентов; пересчитывать его в bff нельзя (в v5.6.0 при ширине 512 смысл был другим, `serve_rendered.js:635-641` — поэтому не ниже v5.7.0);
  - `src/utils.js:38-49` — суффикс `@2x…@9x`, ограничен `maxScaleFactor` (по умолчанию 3, максимум 9 — https://tileserver.readthedocs.io/en/latest/config.html);
  - `serve_rendered.js:571-586` — центр с `|lat| > 85.06` и сторона больше `maxSize` (по умолчанию 2048, с учётом масштаба) — 400;
  - `staticAttributionText` рисует подпись в правом нижнем углу (`src/render.js:492-514`) — там, где клиенты рисуют время сообщения.
  - Шрифты, спрайты и данные читаются из локальных путей (`"glyphs": "{fontstack}/{range}.pbf"`, `pmtiles://file.pmtiles` — config.html).
- **Данные.** Protomaps basemap — векторные тайлы z0–15 из OSM, ODbL, «OpenStreetMap attribution required» (https://docs.protomaps.com/basemaps/downloads). Сборка планеты 20261115 — 139,5 ГБ, версия тайлов 4.15.2 (метаданные `build-metadata.protomaps.dev/builds.json`, 2026-11-15). Вырезка: `pmtiles extract <url> out.pmtiles --region=file.geojson` (Polygon/MultiPolygon/Feature/FeatureCollection), `--maxzoom`, `--dry-run`, источник может быть удалённым (https://docs.protomaps.com/pmtiles/cli; go-pmtiles v1.31.2 `main.go:62-73`).
- **Стиль и подписи.** `@protomaps/basemaps` 5.7.2 (`layers(source, namedFlavor(...), {lang})`, https://docs.protomaps.com/basemaps/maplibre). Языки подписей — 41 код, среди них `ru`, `uk`, `en`, нет `be` и `kk`; местное название показывается рядом, если оно другим письмом (https://docs.protomaps.com/basemaps/localization). Шрифты Noto Sans (OFL) с кириллицей (диапазон `1024-1279.pbf` есть) и спрайты v4 только 1x и 2x — github.com/protomaps/basemaps-assets.
- **Подпись OSM** (https://osmfoundation.org/wiki/Licence/Attribution_Guidelines, прочитано 2026-11-15): статичные картинки подписываются как интерактивные карты, в любом углу; годится «© OpenStreetMap»; маленькие миниатюры и значки можно без подписи.

## Решение

### 1. Что обслуживается
- `upload.getWebFile` с `inputWebFileGeoPointLocation` — PNG карты без метки, центр ровно в точке, размер `w·scale × h·scale`. Ответ `upload.webFile{size, mime_type "image/png", file_type storage.filePng, mtime, bytes}`; `offset`/`limit` режут одну и ту же закэшированную картинку, `size` — её полная длина во всех частях (tdesktop отменит загрузку при другом `size`).
- `webfile_dc_id: 1` в `help.getConfig` и общая основа `upload.getWebFile` (`pkg/webfile`: `offset`/`limit`, вид без источника — `LOCATION_INVALID`) — задача 099, часть B (ADR 0062 §5); карта — зарегистрированный там источник.
- `static_maps_provider` остаётся `"telegram"`: Android не ходит к Google и Яндексу за превью.
- iOS не меняется: MapKit рисует снимок на устройстве. Это запрос телефона к Apple с координатами — поведение официального Telegram, патча нет (правило «минимальных правок клиентов»).

### 2. Приведение параметров (документация молчит о поведении вне диапазонов)
- Координаты: не число или `|long| > 180` → `LOCATION_INVALID`; `lat` прижимается к ±85,0511 (предел Меркатора и tileserver-gl). Обе округляются до 6 знаков (как ключ кэша Android, ~0,1 м) — и для рисования, и для ключа.
- `w`, `h` ≤ 0 → `LOCATION_INVALID`; иначе каждая прижимается к 16…1024 (документация).
- `zoom` прижимается к 13…20 (документация; Android-опросы шлют 13, Web K — 15–16, tdesktop — до 16 и выше).
- `scale` прижимается к 1…4: шире документации на одно значение, потому что tdesktop шлёт 4. Если `w·scale` или `h·scale` больше 2048, масштаб уменьшается до наибольшего, при котором обе стороны ≤ 2048 (клиент растянет).
- `access_hash` не проверяется: Android для бизнеса шлёт 0, у Pluma он и так 0 у всех точек. `accuracy_radius` на картинку не влияет.
- `offset < 0` → `OFFSET_INVALID`; `limit ≤ 0` или больше 1 МиБ → `LIMIT_INVALID`; `offset ≥ size` → пустые `bytes` (Android заканчивает загрузку на неполной части, `FileLoadOperation.java:1965`).

### 3. Рендер: tileserver-gl дома, рядом с bff, без выхода наружу
- Свой образ `pluma-maps` на основе `maptiler/tileserver-gl:v5.7.0@sha256:24d0364e…` (полный дайджест — в бриф 100): стили, шрифты и спрайты внутри образа; данные (`.pmtiles`) — с диска хоста только на чтение. Во время рисования не делается ни одного внешнего запроса: стиль ссылается только на локальные файлы, контейнер подключён только к внутренней сети `maps_net` (`internal: true`) вместе с bff.
- Опубликованного порта нет; `serve_data: false`, веб-интерфейс недоступен снаружи сети. Лог запросов без URL (`--silent --log_format` без `:url`): URL статической картинки содержит координаты.
- `maxScaleFactor: 4`, `maxSize: 2048`, пулы рендереров `minRendererPoolSizes [1,1,1,1]`, `maxRendererPoolSizes [2,2,2,1]` на стиль (точные числа — после замера памяти, бриф 100, часть 4).
- Контейнер — внутри `pluma.slice` (`cgroup_parent: ${PLUMA_CGROUP_PARENT}`), с собственным `mem_limit` и `cpus`: это сервис Pluma, и общий потолок 10 ГБ должен его учитывать; медиасервер владельца в своём `media.slice` не затрагивается.

### 4. Данные
- Регион — выбор владельца 2026-11-15: Европа, Россия, СНГ, обе Америки. Многоугольник `teamgramd/deploy/maps/region.geojson` строится из границ стран Natural Earth (общественное достояние) с запасом ~20 км и разрезом по 180-му меридиану (Чукотка, Алеутские острова). Турция входит как часть Европы (подтвердить).
- Вырезка `pmtiles extract` из сборки Protomaps до z15 (полный максимум данных; превью z15–16 и выше рисуются дорисовкой с z15). Оценка — 90–105 ГБ (по долям выгрузок Geofabrik: Европа 33,0 ГБ, Северная Америка 18,2, Южная 3,9, Центральная 0,76, Россия 3,9, Центральная Азия и Закавказье ~0,6 против 89 ГБ планеты OSM; не замер — точное число даёт `--dry-run` до скачивания). Вся планета — 139,5 ГБ.
- Точка вне региона рисуется фоном стиля без дорог и подписей; ошибки нет.
- Обновление — вручную, по решению владельца (предложение — раз в полгода): новая вырезка рядом со старой, переключение, удаление старой. На время обновления нужно ещё столько же места.

### 5. Стили и язык подписей
- Стиль собирается из `@protomaps/basemaps` 5.7.2, вариант `light`, по одному на язык из `MAPS_STYLES` (по умолчанию `ru,en`). Каждый стиль — свои пулы рендереров, поэтому языков немного.
- Язык выбирается по `lang_code` сессии запроса: есть такой стиль — он; `be`, `kk`, `ky`, `tg`, `uz`, `hy`, `az` → `ru`, если он есть; иначе `en`. Картинка — карта, а не текст интерфейса: подписи — названия из OSM, а не строки Pluma, поэтому правило «тексты только ключами» к ним не относится.

### 6. Подпись данных
- «© OpenStreetMap» дорисовывает bff в **левом нижнем** углу (в правом клиенты рисуют время сообщения), полупрозрачной плашкой, шрифтом Go (`golang.org/x/image/font/gofont`, BSD), с учётом `scale`. Не дорисовывается при `min(w, h) < 100` — миниатюры (документ OSM разрешает).
- Это имя собственное и юридическая отметка, не текст интерфейса; не переводится и в langpack не идёт (подтвердить владельцу). `staticAttributionText` tileserver-gl не используется: он в правом углу и задаётся на стиль целиком, без условия по размеру.

### 7. Кэш, повторы и пределы (bff)
- Готовая картинка — в Redis по ключу `webmap:<sha256(стиль, lat, long, w, h, zoom, scale)>` на `MAPS_CACHE_TTL` (по умолчанию 6 ч): все части одного файла и все зрители одного сообщения получают одни и те же байты. Клиенты кэшируют сами (Android — по имени файла), поэтому долгий кэш на сервере не нужен.
- Одинаковые одновременные запросы схлопываются (`singleflight`); одновременно рисуется не больше `MAPS_CONCURRENCY` (по умолчанию 4) картинок; запрос к tileserver-gl ограничен `MAPS_TIMEOUT` (10 с).
- Предел от злоупотребления: не больше 60 новых (мимо кэша) картинок в минуту на пользователя → `FLOOD_WAIT_X`. Попадания в кэш не считаются.
- Сбой или таймаут рендера → `INTERNAL_SERVER_ERROR` (500); ничего не кэшируется. Карты выключены (`MAPS_URL` пуст) → `LOCATION_INVALID`.

### 8. Что пишется в лог и метрики
- Координаты, `w`/`h`, картинка — никогда: `zrpc.DontLogContentForMethod("/mtproto.RPCFiles/upload_getWebFile")`, имена `lat`, `long`, `geopoint`, `getlat`, `getlong`, `getgeopoint` — в `tools/logaudit`.
- Метрики `webmap_render_total{result}`, `webmap_render_seconds`, `webmap_cache_total{result}`; алерт на долю сбоев рендера.

## Последствия
- Карта в геопозиции, месте, трансляции, опросе, уведомлении и адресе бизнеса видна на Android, Desktop и Web K; точки не уходят из дома. iOS — как у Telegram (MapKit на устройстве).
- Новый постоянный сервис дома: ~100 ГБ диска, память в пределах `pluma.slice` (замер в задаче), CPU при рисовании.
- Подписи — на языке сессии из небольшого набора стилей; беларуская и казахская сессии видят русские подписи.
- Данные стареют до ручного обновления.
- Выбор точки в Desktop по-прежнему недоступен (нет токена Mapbox) — отличие от официального Desktop, которое закрывается только патчем клиента (ниже).

## Что не делается
- Выбор точки в Desktop (Mapbox GL JS и геокодер Mapbox): токен Mapbox — внешний аккаунт и утечка координат к Mapbox; замена на MapLibre GL JS со своим стилем требует патча `location_picker.cpp`, публичной раздачи тайлов и своего геокодера. Отдельная задача по решению владельца.
- Интерактивная карта и поиск адреса на Android (Google Maps SDK, Geocoder) и на iOS (MapKit, `CLGeocoder`) — поведение официальных клиентов, без патчей.
- Поиск мест (`venue_search_username`) и геокодер — задача 099 (встроенные боты).
- `messages.getRecentLocations`: не рендер карт — делает задача 098 (часть G).
- Ссылка «открыть в картах» (Google Maps в браузере) — клиентская, не трогается.
- Прокси к публичным тайловым и картографическим сервисам — запрещено этим решением.

## Рассмотренные альтернативы
- **Прокси к OSM/Google/Яндексу** — координаты уходят наружу; правила OSM для продукта не выполнимы.
- **Растровые тайлы и склейка в Go** — нужен свой растровый источник (тот же рендер) плюс код склейки; tileserver-gl делает это готово.
- **Своя сборка Planetiler из OSM** — около 32 ГБ памяти на планету (отчёт исследования; не проверено); готовые сборки Protomaps проще.
- **OpenFreeMap MBTiles** — схема и стиль не проверены, сборка реже.
- **Вся планета** вместо региона — +35–50 ГБ, покрывает поездки (Египет, ОАЭ, Таиланд); оставлено владельцу.
- **JPEG вместо PNG** — меньше, но Android явно ждёт `image/png` (`WebFile.java:42`); оставлен PNG.

## Что осталось подтвердить владельцу
1. Турция — в регионе; добавить ли популярные направления поездок (Египет, ОАЭ, Таиланд, Израиль) или всю планету.
2. Стили `ru` и `en` (украинская сессия видит английские подписи, пока `uk` не добавлен в `MAPS_STYLES`).
3. Подпись «© OpenStreetMap» не переводится.
4. Обновление данных раз в полгода, вручную.
5. Desktop без выбора точки (как сейчас) — или отдельная задача с патчем клиента.
