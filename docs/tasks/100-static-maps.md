# 100. Карты: статические превью геопозиций со своего сервера

- Статус: готово, ждёт выкладки (2026-11-16)
- Решение: ADR 0063.
- Зависит от: 027 (пункт «карта в геопозиции» закрывается здесь), 096 / ADR 0061 (домашний сервер, `pluma.slice`), 090 / ADR 0056 (образец сервиса рядом с bff). **099, часть B** — единственный владелец общей части: `pkg/webfile` (разбор по виду, `offset`/`limit`, ответ), обработчик `upload.getWebFile`, `webfile_dc_id: 1`, `DontLogContentForMethod`, имена геоданных в `logaudit`. 100 только регистрирует свой источник и сливается после 099 B (см. «Порядок работ», часть 1). Получение текущих геопозиций чата (`messages.getRecentLocations`) — 098, часть G. Пользуется результатом: 105 (адрес бизнеса показывает ту же картинку).
- Ветка: `task/100-static-maps`.

## Цель
Геопозиция, место, трансляция геопозиции, геоточка в опросе, уведомлении и адресе бизнеса показывают карту на Android, Desktop и Web K так же, как в Telegram. Картинку рисует свой tileserver-gl дома по векторным данным Protomaps (Европа, Россия, СНГ, обе Америки — выбор владельца 2026-11-15); координаты из сообщений не покидают дом. iOS рисует карту сам через MapKit и не меняется.

## Область
В задаче:
- `upload.getWebFile` с `inputWebFileGeoPointLocation` (сейчас `ErrEnterpriseIsBlocked`, `app/bff/files/internal/core/upload.getWebFile_handler.go:33-44`).
- `webfile_dc_id: 4` → `1` в `teamgramd/bin/config.json:59` — делает 099 B; здесь только e2e-проверка `TestConfigWebfileDc`.
- Пакет `pkg/staticmap`: приведение параметров, клиент tileserver-gl, кэш Redis, схлопывание запросов, ограничение одновременных рендеров, предел на пользователя, подпись «© OpenStreetMap», метрики.
- Сервис `maps` (tileserver-gl v5.7.0 со своими стилями) дома: образ, конфиг, внутренняя сеть, compose-файл, проверка после выкладки.
- Данные: многоугольник региона, скрипт вырезки и обновления PMTiles, оценка и замер диска и памяти.
- `tools/logaudit`, метрики и алерт, `docs/ops/maps.md`, `docs/ops/memory.md`, строки `TESTING-BACKLOG`.

Не в задаче (ADR 0063, «Что не делается»):
- выбор точки в Desktop (Mapbox) — остаётся недоступным, см. «Решения к подтверждению», п. 5;
- интерактивные карты и геокодеры клиентов (Google Maps SDK и Geocoder на Android, MapKit и `CLGeocoder` на iOS);
- поиск мест и `venue_search_username` — 099;
- `messages.getRecentLocations` — делает 098, часть G (`MEDIA_GEO_LIVE` в `message_filter_type` и миграция старых сообщений);
- `inputWebFileLocation` (веб-документы ботов) и `inputWebFileAudioAlbumThumbLocation` — 099 и как есть.

## Контракт клиентов
Источники — `~/pluma-work/reference-src-new` (закреплённые выпуски), подробно — ADR 0063, «Контекст». Главное для реализации:
- **Кто просит.** Android при `static_maps_provider = "telegram"` (`MessagesController.java:5707-5712`; у Pluma так и есть, `config.json:54-56`), tdesktop всегда (`storage/download_manager_mtproto.cpp:542-561`), Web K всегда (`helpers/getWebFileLocation.ts:3-23`). iOS — нет: `MKMapSnapshotter` на устройстве (`LocationResources/Sources/MapResources.swift:74-85`).
- **Куда.** На DC из `webfile_dc_id`: Android `MessagesController.java:5696`, `FileLoadOperation.java:411`; tdesktop `file_download_mtproto.cpp:64`, `mtproto_config.cpp:257`; Web K `apiFileManager.ts:200, 381-450`. По умолчанию у всех 4 (Android `:1834-1836`, tdesktop `mtproto_config.cpp:27`), а патченые клиенты знают только DC 1 (`clients/patches/android.md:74-93`, `desktop.md:53, 94-102`, `web.md:106-114`). Поэтому `webfile_dc_id: 1` — обязательная часть.
- **Параметры, которые придут.**
  - Android: `w`, `h` в dp, `zoom` 15 (опросы — 13), `scale = min(2, ceil(density))` (`messenger/WebFile.java:31-46`; вызовы `ChatMessageCell.java:8783, 8828, 8860`, `PopupNotificationActivity.java:951`, `ContextLinkCell.java:295`, `poll/attached/PollAttachedMediaLocation.java:44, 64`, `poll/buttons/PollButtonDrawable.java:257`, `ui/Business/LocationActivity.java:322` с `access_hash = 0` и уже умноженными `w`, `h`, `ProfileLocationCell.java:112`).
  - tdesktop: `scale = 1 + (cScale·DPR)/200`, `zoom = 13 + (scale−1)`, `w, h = locationSize/scale` (`data/data_location.cpp:95-110`); `cScale` 50…300 (`kScaleMax = 300`, desktop-app/lib_ui `ui/style/style_core_scale.h`, прочитано 2026-11-15), `DevicePixelRatio()` целое → **`scale` до 4**, изредка 5.
  - Web K: `zoom` 16 (пузырь), 15 (превью 320×160), 16 (профиль 48×48); ширина 277/420, высота `ширина·195/277` (`components/wrappers/geo.ts:63-84, 152`, `mapPreview.tsx`, `peerProfile.tsx:1371`). `scale = devicePixelRatio`, дробные **усекаются** при записи в `Int32Array` (`lib/mtproto/tl_utils.ts:34, 45, 114-120`): 1,25 → 1, 2,625 → 2, высота 295,7 → 295.
  - Документация: `w`, `h` 16–1024, `zoom` 13–20, `scale` 1–3 (https://core.telegram.org/constructor/inputWebFileGeoPointLocation, прочитано 2026-11-15). Поведение Telegram вне диапазонов не проверено — решение ADR 0063, п. 2 (`scale` 4 принимаем ради tdesktop).
- **Смысл zoom** — мир 256·2^z пикселей: Web K `geo.ts:170-172` (метры на пиксель для круга точности), Android `ChatMessageCell.java:8776-8780` (сдвиг центра трансляции). tileserver-gl v5.7.0 для статических картинок сам передаёт MapLibre `zoom − 1` (`src/render_mode.js:10-13`), то есть **zoom клиента передаётся в URL как есть**; пересчёт в bff даст карту вдвое мельче.
- **Ответ.** `upload.webFile` (`api.tl:1028`): Android ждёт `image/png` (`WebFile.java:42`) и с `size = 0` у веб-файла заканчивает на неполной части (`FileLoadOperation.java:410, 1965`, части по 32 КБ); tdesktop берёт `size` из первой части и **отменяет загрузку, если в другой части он иной** (`file_download_mtproto.cpp:170-181`) — байты всех частей должны быть из одной и той же картинки.
- **Метку не рисовать**: клиенты рисуют свою (tdesktop `history_view_location.cpp:551-552`, Web K `geo.ts:120-147`). Правый нижний угол занят временем сообщения в пузыре — подпись OSM в левом нижнем.
- **Трансляция.** Правка сообщения с новой точкой → клиент просит новую картинку и плавно меняет старую на новую (Android `ChatMessageCell.java:8781-8784, 8886-8897`). Серверу достаточно рисовать по новым координатам; правка медиа уже идёт через `makeMediaByInputMedia` (`app/bff/messages/internal/core/messages.editMessage_handler.go:146-157`, `core_messages_helper.go:245`).
- **Ошибки** (https://core.telegram.org/method/upload.getWebFile): `LOCATION_INVALID`, `DOCUMENT_INVALID`. Остальные (`OFFSET_INVALID`, `LIMIT_INVALID`, `FLOOD_WAIT_X`, 500) — наши, по образцу `upload.getFile` (`upload.getFile_handler.go:153-180`).
- **Desktop без токена Mapbox** не показывает пункт «Геопозиция» во вложениях (`inline_bots/bot_attach_web_view.cpp:3127-3134`, `ui/controls/location_picker.cpp:343-351`), опрос и адрес бизнеса не выбирают точку (`boxes/create_poll_box.cpp:2626-2627`, `settings/business/settings_location.cpp:287`). Полученные геопозиции Desktop показывает — это и есть задача.

## Дизайн

### Таблицы
Нет. Миграций нет. Кэш — Redis, данные карты — файл на диске хоста.

### Поток запроса
```
клиент ──upload.getWebFile(geo)──► session ──► bff (files)
   bff: привести параметры → ключ → Redis webmap:<ключ>?
     есть → вырезать offset/limit → upload.webFile
     нет  → предел пользователя → singleflight(ключ) → семафор
            → GET http://maps:8080/styles/<стиль>/static/<lon>,<lat>,<zoom>/<w>x<h>[@<s>x].png
            → проверить PNG (сигнатура, размеры w·s × h·s) → подпись OSM (если min(w,h) ≥ 100)
            → SET webmap:<ключ> EX MAPS_CACHE_TTL → вырезать → upload.webFile
```

### `pkg/staticmap` (новый)
- `Params{Lat, Long float64; W, H, Zoom, Scale int32}` и `Normalize(in *mtproto.InputWebFileLocation) (Params, error)` — правила ADR 0063, п. 2:
  - не число / `|long| > 180` → `ErrLocationInvalid`; `lat` ∈ [−85.0511, 85.0511]; округление до 1e-6;
  - `w`, `h` ≤ 0 → `ErrLocationInvalid`; иначе clamp 16…1024; `zoom` clamp 13…20; `scale` clamp 1…4, затем уменьшать, пока `w·scale > 2048 || h·scale > 2048`.
- `Key(style string, p Params) string` — `sha256` от строки фиксированного вида (`style|%.6f|%.6f|w|h|zoom|scale`), hex.
- `StyleFor(langCode string, styles []string) string` — ADR 0063, п. 5 (`langpack.CanonicalLangCode` для нормализации кода).
- `Renderer` (интерфейс, одна функция `Render(ctx, style, Params) ([]byte, error)`) и `HTTPRenderer` (tileserver-gl): `http.Client` без редиректов, таймаут `MAPS_TIMEOUT`, ответ не больше 16 МиБ (`io.LimitReader`), статус ≠ 200 — ошибка с кодом статуса (тело в лог не идёт: в нём может быть URL), `Content-Type` `image/png`.
- `Service`:
  - `Get(ctx, userId int64, langCode string, loc) (*Blob, error)`, где `Blob{Bytes []byte; Mtime int32}`;
  - кэш `kv.Store` (`webmap:` + ключ, `SETEX`); при ошибке Redis рисует без кэша и не кэширует (метрика), но не падает;
  - `singleflight.Group` (`golang.org/x/sync` уже в `go.mod:46`);
  - семафор `MAPS_CONCURRENCY` с ожиданием в пределах контекста запроса;
  - предел: счётчик Redis `webmap_rl:<user>:<минута>` (INCR + EXPIRE в Lua, как `pkg/logincode/limiter.go`), 60 в минуту → `mtproto.NewErrFloodWaitX(до конца минуты)`; попадание в кэш не считается;
  - проверка PNG: сигнатура и `IHDR` (`image.DecodeConfig`), размеры ровно `w·s × h·s`, иначе ошибка;
  - подпись: `image/png` decode → плашка `rgba(255,255,255,0.8)` и текст `© OpenStreetMap` `rgba(0,0,0,0.8)`, кегль 10·s, отступ 3·s от левого и нижнего края (`golang.org/x/image/font/gofont/goregular`, `x/image` уже в `go.mod:44`) → `png.Encoder{CompressionLevel: png.BestCompression}` (замерить время; при > 50 мс на 1260×888 — `DefaultCompression`).
- `Config` (go-zero, `json:",optional"`): `URL`, `Styles` (строка через запятую, по умолчанию `ru,en`), `Timeout` (10s), `Concurrency` (4), `CacheTTL` (6h), `PerUserPerMinute` (60). Пустой `URL` — карты выключены. `Validate()`: `URL` http(s) без пути, стили — `[a-z-]{2,10}`.
- Метрики (`prometheus`, как `pkg/transcribe/metrics.go`): `webmap_render_total{result=ok|timeout|error|invalid}`, `webmap_render_seconds` (гистограмма), `webmap_cache_total{result=hit|miss|error}`, `webmap_flood_total`.

### bff files
- Обработчик `upload.getWebFile`, разбор по виду, проверка `offset`/`limit`, вырезка части и сборка `upload.webFile` — `pkg/webfile` задачи 099 (часть B). Здесь — только источник `app/bff/files/internal/core/geo_source.go`: `webfile.Source` для `inputWebFileGeoPointLocation` поверх `staticmap.Service` (`Resolve` → `Normalize`, `Open` → `Get` с `Request.UserId` и `Request.LangCode`, `Cached() = true`: копию держит Redis `staticmap`), регистрация `Server.Register` в `svc`, если `Maps != nil`.
- Ошибки: карты выключены — источник не регистрируется, общая часть отвечает `LOCATION_INVALID`; `Normalize` → `ErrLocationInvalid`; `offset`/`limit` — общие правила 099 (`OFFSET_INVALID`, `LIMIT_INVALID`); предел → `FLOOD_WAIT_X`; рендер/таймаут → `ErrInternalServerError`. Лог — только класс ошибки и `user_id`, без координат и размеров.
- `internal/config/config.go`: `KV kv.KvConf`, `Maps staticmap.Config`. `internal/svc/service_context.go`: `Maps *staticmap.Service` (nil при пустом `URL`).
- `DontLogContentForMethod` для `upload_getWebFile` и имена геоданных в `logaudit` — уже сделаны 099 B.
- `app/bff/bff/internal/server/server.go:323-329`: передать `KV: c.KV`, `Maps: c.Maps`; `app/bff/bff/internal/config/config.go`: `Maps staticmap.Config`.

### Сервис `maps` (дом)
- `teamgramd/deploy/maps/Dockerfile`:
  - стадия `node:<LTS>@sha256:…`: `npm ci` по `package.json`/`package-lock.json` с `@protomaps/basemaps@5.7.2`; `build-styles.mjs` пишет `styles/<lang>.json` для каждого языка: `sources.protomaps = {type: "vector", url: "pmtiles://pluma.pmtiles"}` (локальный файл в `paths.pmtiles`, синтаксис config.html), атрибуция источника пустая (подпись рисует bff), `glyphs: "{fontstack}/{range}.pbf"`, `sprite` — локальный путь `sprites/light`, `layers(…, namedFlavor("light"), {lang})`;
  - шрифты и спрайты — из `protomaps/basemaps-assets` на закреплённом коммите (zip по URL коммита, проверка sha256 в Dockerfile): `fonts/Noto Sans {Regular,Medium,Italic}`, `sprites/v4/light{,@2x}`;
  - финальная стадия `FROM maptiler/tileserver-gl:v5.7.0@sha256:24d0364e1d40da0ae3fb3269f617c26e36a883ad4b7454e5c4064a8795bfc807` (индекс; amd64 `sha256:61f07d91…`), `COPY` стилей, шрифтов, спрайтов и `config.json`.
- `teamgramd/deploy/maps/config.json` (tileserver-gl): `options.paths` (`root`, `fonts`, `sprites`, `styles`, `pmtiles: /data`), `maxScaleFactor: 4`, `maxSize: 2048`, `minRendererPoolSizes: [1,1,1,1]`, `maxRendererPoolSizes: [2,2,2,1]`, `serveAllStyles: false`, `serveAllFonts: false`, `allowRemoteMarkerIcons: false`; `styles.<lang>.{style, serve_rendered: true, serve_data: false}`; стили ссылаются на файл напрямую (`pmtiles://pluma.pmtiles`), раздела `data` нет (он раздавал бы сами тайлы).
- `docker-compose.maps.yaml` (образец — `docker-compose.stt.yaml`):
  - `maps`: `image: ${PLUMA_MAPS_IMAGE:-pluma-maps:local}`, `build`, `mem_limit`/`memswap_limit: ${MAPS_MEMORY:-1536m}`, `cpus: ${MAPS_CPUS:-2}`, `cgroup_parent: ${PLUMA_CGROUP_PARENT:-}`, `read_only: true`, `tmpfs: /tmp` (Xvfb пишет `/tmp/.X99-lock`, `docker-entrypoint.sh`), том `${MAPS_DATA_DIR:-/opt/pluma-maps}:/data:ro`, команда `--config /app/config.json --silent --log_format ":method :status :res[content-length] :response-time ms"` (без `:url` — в нём координаты), `healthcheck` на `GET /health`, без опубликованных портов, сеть только `maps_net`;
  - `bff`: добавить сеть `maps_net` к его сетям; `MAPS_URL` уже в основном compose (пустой по умолчанию);
  - `networks.maps_net: {internal: true}` — у контейнера карт нет выхода наружу совсем.
- `MAPS_URL=http://maps:8080` в `.env` дома включает карты.

### Данные
- `teamgramd/deploy/maps/region.geojson` — MultiPolygon: страны Европы (с Турцией и Кипром, Исландией, Канарами, Азорами, Мадейрой), Россия, Беларусь, Украина, Молдова, Грузия, Армения, Азербайджан, Казахстан, Кыргызстан, Таджикистан, Туркменистан, Узбекистан; Северная, Центральная, Южная Америка, Карибы, Гренландия. Источник — Natural Earth Admin 0, 1:10m (общественное достояние), буфер ~20 км, упрощение до ~1 км, разрез по ±180° (Чукотка, Алеуты). Генератор `teamgramd/deploy/maps/make-region.sh` (ogr2ogr/mapshaper в одноразовом контейнере с закреплённым образом) лежит рядом, результат коммитится (размер файла ≤ 500 КБ).
- `teamgramd/deploy/maps/extract.sh` (на хосте, от root):
  1. скачать `go-pmtiles` v1.31.2 (linux x86_64) с GitHub Releases, сверить с `checksums.txt` выпуска;
  2. `pmtiles extract https://build.protomaps.com/<YYYYMMDD>.pmtiles <dir>/pluma-<YYYYMMDD>.pmtiles --region=region.geojson --maxzoom=15 --download-threads=4 --dry-run` → вывести размер и проверить место (`df`), нужно размер + 10 %;
  3. без `--dry-run` — скачать; `pmtiles verify`; `pmtiles show` (версия тайлов `4.x`);
  4. атомарно перевести ссылку `pluma.pmtiles` → новый файл, `docker compose … restart maps`, `check.sh`, удалить старый файл.
  Сборка Protomaps хранится неделю («daily builds», https://docs.protomaps.com/basemaps/downloads) — скрипт берёт дату из `build-metadata.protomaps.dev/builds.json`. Трафик — ~100 ГБ входящего; в запросах только номера тайлов региона, без данных пользователей.
- Оценка: 90–105 ГБ (ADR 0063, п. 4); замер `--dry-run` записать в `docs/ops/maps.md` и в «Результат».

### Обновления другим устройствам и пользователям
Нет: картинка — ответ на запрос, рассылки не нужно. Новая точка трансляции приходит обычной правкой сообщения.

### Пределы
- Параметры: ADR 0063, п. 2. `limit` ≤ 1 МиБ.
- Одновременно рисуется ≤ 4 картинок на bff; 60 новых картинок в минуту на пользователя.
- Ответ tileserver-gl ≤ 16 МиБ; картинка в Redis — как есть (оценка 0,1–1 МБ, замерить), TTL 6 ч.
- Память bff: декодирование 2048×2048 RGBA — 16 МБ на рендер, при 4 одновременно ≤ 64 МБ сверх обычного; `mem_limit` bff (384m, `docker-compose.yaml:270`) проверить замером, поднять при нужде с пересчётом `docs/ops/memory.md`.

### После перезапуска
- bff: кэш в Redis переживает перезапуск; несостоявшиеся рендеры клиент повторит.
- `maps`: рендереры поднимаются заново (секунды); до готовности bff получает ошибку соединения → 500, клиенты повторяют при следующем показе. `restart: unless-stopped`, `depends_on` bff на `maps` не ставить (bff не должен ждать карты).
- Нет файла данных → tileserver-gl не стартует (без `--ignore-missing-files`); bff отвечает 500, алерт.

### Файлы
Добавить:
- `pkg/staticmap/{staticmap.go,params.go,render.go,attribution.go,cache.go,limit.go,config.go,metrics.go}` и тесты.
- `app/bff/files/internal/core/geo_source.go`, `geo_source_test.go` (части через `webfile.Server` с поддельным `Renderer`).
- `teamgramd/deploy/maps/{Dockerfile,config.json,package.json,package-lock.json,build-styles.mjs,region.geojson,make-region.sh,extract.sh,check.sh}`.
- `docker-compose.maps.yaml`.
- `test/e2e/fakemaps/{main.go,Dockerfile}`, `test/e2e/maps_test.go`.
- `docs/ops/maps.md`.

Изменить:
- `app/bff/files/internal/config/config.go`, `internal/svc/service_context.go` (регистрация источника).
- `app/bff/bff/internal/config/config.go`, `app/bff/bff/internal/server/server.go`, `app/bff/bff/etc/bff.yaml`, `teamgramd/etc/bff.yaml`, `teamgramd/etc2/bff.yaml` (блок `Maps` с `${MAPS_URL}` и т. д.; `KV` у files — тот же, что у bff).
- `docker-compose.yaml` (`MAPS_URL` и прочие у bff), `.env.example` (`MAPS_URL`, `MAPS_STYLES`, `MAPS_TIMEOUT`, `MAPS_CONCURRENCY`, `MAPS_CACHE_TTL`, `MAPS_PER_USER_PER_MINUTE`, `MAPS_DATA_DIR`, `MAPS_MEMORY`, `MAPS_CPUS`, `PLUMA_MAPS_IMAGE`), `pkg/conf` — если там список известных переменных.
- `test/e2e/docker-compose.e2e.yaml`, `test/e2e/run.sh` (сервис `fakemaps`, `MAPS_URL` на него).
- `tools/logaudit/logaudit.go` — не трогать: имена геоданных добавляет 099 B.
- `teamgramd/deploy/prometheus/server/rules/` (группа `maps`), `docs/ops/monitoring.md`, `docs/ops/memory.md`, `docs/ops/home-server.md` (каталог данных, команда compose с `-f docker-compose.maps.yaml`), `docs/ops/deployment.md` при необходимости.
- `docs/project/TESTING-BACKLOG.md`, `docs/project/api-coverage.md` (`go run ./tools/apicoverage`), `docs/project/STATUS.md` — главная сессия.

## Порядок работ
Части рассчитаны на одного исполнителя за один заход. Пересечения по файлам указаны.

1. **Общая часть `upload.getWebFile`** — делает 099, часть B (единственный владелец). Части 3 и 5 этой задачи начинаются на ветке, где 099 B уже влита; части 2 и 4 от неё не зависят.
2. **`pkg/staticmap`** (M, ~1 день). Всё из раздела «`pkg/staticmap`» с модульными и интеграционными (Redis) тестами; `Renderer` подменяется в тестах. Не трогает файлов других частей — **параллельно с 3 и 4**.
3. **Подключение в bff** (S, ~3–4 ч). После 099 B. Конфиги files и bff, `server.go`, yaml в трёх местах, `docker-compose.yaml`, `.env.example`, источник `geo_source.go` и его регистрация, тест с поддельным `Renderer`. Зависит от интерфейса части 2 (сначала согласовать сигнатуры `staticmap.Service`/`Config`, можно параллельно). `server.go` и `bff.yaml` правят многие задачи пакета — сливать по одной, читая обе стороны.
4. **Сервис `maps` и данные** (M, ~1 день, на CachyOS и дома). Dockerfile, стили, `config.json`, `docker-compose.maps.yaml`, `region.geojson` и генератор, `extract.sh`, `check.sh`; сборка образа; `--dry-run` по региону (число в «Результат»); на CachyOS — вырезка малого города (`--bbox`, z0–15) и замер памяти контейнера под нагрузкой (`docker stats` при 4 параллельных рендерах каждого масштаба 1–4) → `MAPS_MEMORY`, пулы. **Параллельно с 2 и 3.**
5. **Тесты стенда и e2e** (M, ~4–6 ч). `test/e2e/fakemaps`, сервис в стенде, `maps_test.go`; интеграционный тест с настоящим tileserver-gl (ниже). После 1–3.
6. **Эксплуатация и документы** (S, ~2–3 ч). `docs/ops/maps.md`, `memory.md`, `monitoring.md`, `home-server.md`, алерты, `TESTING-BACKLOG`. После 4.
7. **Выкладка дома** (по `docs/ops/maps.md`): место и память (`df`, `free -m`), вырезка (часы), образ, `up -d maps`, `MAPS_URL`, перезапуск bff, `check.sh`, просмотр на Android/Desktop/Web K. Делает главная сессия после сборной ветки.

## Тесты
Модульные:
- `pkg/staticmap`: `Normalize` — каждая граница (lat ±85,06 и ±90, long ±180,000001, NaN, `w`/`h` 0, 15, 16, 1024, 1025; zoom 12/13/20/21; scale 0/1/3/4/5; 1024×1024@4 → @2; поля Web K после усечения); `Key` стабилен и различает каждое поле и стиль; `StyleFor` (`ru`, `ru-RU`, `be`, `kk`, `uk` при `ru,en` и при `ru,en,uk`, пустой код, неизвестный).
- `HTTPRenderer` на `httptest`: точный путь `/styles/ru/static/37.617635,55.755814,15/277x195@2x.png` (lon первым, zoom без пересчёта, без суффикса при scale 1), отказ от редиректа, таймаут, 400/503, большой ответ, не-PNG, PNG чужого размера.
- `Service`: попадание в кэш не зовёт рендер и не считает предел; 10 одновременных одинаковых запросов → один рендер; семафор не пропускает пятый; ошибка Redis → картинка без кэша; ошибка рендера не кэшируется; 61-й промах в минуту → `FLOOD_WAIT_X`.
- Подпись: есть при 100×100, нет при 99×200; пиксели слева снизу изменены, справа снизу — нет; масштаб 2 — плашка вдвое больше.
- Обработчик: части `offset`/`limit` склеиваются в исходный PNG; `size` одинаковый во всех частях; `offset ≥ size` → пустые байты; ошибки из таблицы; выключенные карты → `LOCATION_INVALID`.
- (`tools/logaudit` ловит `Errorf("%v", loc.GetGeoPoint().GetLat())` — тест 099 B.)

Интеграционные (`pkg/testinfra`):
- Redis: `SETEX`/чтение байтов, TTL, Lua предела под гонкой 100 горутин (ровно 60 проходят).
- **Настоящий tileserver-gl** (образ по дайджесту, через testcontainers, как прочие образы `testinfra`; тег `integration`): стиль только с фоном и встроенным GeoJSON-источником — две вертикальные линии на долготах `lon ± Δ`. Рендер `z = 15`, `w = 512`, `scale 1` и `2`: расстояние между линиями в пикселях равно `2Δ·256·2^15/360·scale` с точностью ±1 px — проверка смысла zoom и `@Nx` без данных Protomaps. Там же: `@4x` работает при `maxScaleFactor: 4`, `@5x` — 404.

Сквозные (`pkg/mtclient`, стенд с `fakemaps`, сессия после `getState`):
- `fakemaps` отвечает на `/styles/{id}/static/...` PNG ровно `w·s × h·s`, в первом пикселе кодирует номер запроса, хранит последние запросы и отдаёт их тесту на loopback-порту (как `fakemail`); умеет плановые 503 и задержку.
- `TestWebMapGeo`: A шлёт геопозицию B; B берёт `upload.getWebFile` частями по 32 КБ (как Android) и 128 КБ — PNG нужного размера, `mime_type "image/png"`, `file_type storage.filePng`, одинаковый `size`; `fakemaps` видел один запрос с zoom 15 и стилем `ru` для сессии `lang_code ru`, `en` — для `en`.
- `TestWebMapLive`: трансляция, правка с новой точкой → новый запрос к `fakemaps` с новыми координатами; повтор старой точки — из кэша.
- `TestWebMapRefusals`: `w = 0`, `long = 200` → `LOCATION_INVALID`; `offset = −1` → `OFFSET_INVALID`; `limit = 2 МиБ` → `LIMIT_INVALID`; `fakemaps` 503 → 500 и следующий успешный запрос рисует заново; стенд без `MAPS_URL` → `LOCATION_INVALID`.
- `TestWebMapScale`: tdesktop-подобный запрос `scale 4, zoom 16` → `@4x`; `scale 5` → `@4x`; `1024×1024@4` → `@2x`.
- `TestConfigWebfileDc`: `help.getConfig` → `webfile_dc_id = 1`.
- Проверить, что `TestWebMapGeo` падает без изменения (сейчас `ENTERPRISE_IS_BLOCKED`).

## Проверки владельца
Строки в `docs/project/TESTING-BACKLOG.md` (раздел «Карты»):
- [ ] Android: отправить геопозицию и место — в пузыре карта нужного места, метка по центру, подпись «© OpenStreetMap» слева снизу не закрыта временем.
- [ ] Desktop (100 % и 200–300 % масштаба): та же геопозиция — карта резкая, место совпадает с Android.
- [ ] Web K: та же геопозиция; на мобильной ширине и на широком окне; круг точности трансляции соответствует карте.
- [ ] Трансляция геопозиции с Android на iPhone и Desktop: карта в пузыре сдвигается за точкой.
- [ ] Подписи по-русски при русском языке клиента и по-английски при английском; Париж — «Париж» и «Paris».
- [ ] Геопозиция вне региона (например, Египет) — фон без ошибки и без вечной загрузки.
- [ ] Геоточка в опросе и адрес бизнеса (Android) — маленькие карты без подписи OSM.
- [ ] iPhone: карты как раньше (MapKit), ничего не сломалось.
- [ ] Android: интерактивная карта при отправке геопозиции (Google Maps SDK с ключом Telegram) — отображается или серая; результат записать (вне этой задачи, но решает, нужен ли патч).

## Эксплуатация
- `docs/ops/maps.md` (новый): что это и зачем (ADR 0063); каталог данных `/opt/pluma-maps` (владелец root, 0755, файл 0644); первая вырезка и обновление (`extract.sh`, `--dry-run`, место, время); сборка образа; запуск `docker compose -f docker-compose.yaml … -f docker-compose.maps.yaml up -d maps`; `MAPS_URL=http://maps:8080` в `.env` и перезапуск bff; `check.sh` (рендер Красной площади z15 277×195@2x и точки в Атлантике: размеры PNG, не одноцветная / одноцветная картинка); что делать при «нет файла», «нет памяти»; подпись OSM; лицензии (ODbL данных, OFL шрифтов, MIT спрайтов — basemaps-assets; лицензию tileserver-gl и Protomaps-стилей переписать из их репозиториев на закреплённых версиях).
- Переменные: `MAPS_URL`, `MAPS_STYLES`, `MAPS_TIMEOUT`, `MAPS_CONCURRENCY`, `MAPS_CACHE_TTL`, `MAPS_PER_USER_PER_MINUTE` (bff); `MAPS_DATA_DIR`, `MAPS_MEMORY`, `MAPS_CPUS`, `PLUMA_MAPS_IMAGE` (compose). Секретов нет.
- Миграций нет. Сайдкар: `maps`, внутри `pluma.slice` (ADR 0063, п. 3); `docs/ops/memory.md` — пересчитать сумму потолков (сейчас Pluma ~3,6 ГБ из 10 ГБ, `home-server.md:58`; карты — до `MAPS_MEMORY`, оценка 1–1,5 ГБ, замер в части 4).
- Диск дома: ~100 ГБ данных (+ столько же на время обновления), образ ~300 МБ. Свободно ~340 ГБ, диск общий с медиасервером владельца — проверять `df` перед вырезкой.
- Метрики и алерты: `MapsRenderFailing` (доля `webmap_render_total{result!="ok"}` > 20 % за 10 мин при ≥ 10 запросах), `MapsRendererDown` (нет успешных рендеров 15 мин при запросах) — группа `maps`.
- Лог `maps` — без URL (формат в compose); лог bff — без координат (`DontLogContentForMethod`, `logaudit`).

## Решения к подтверждению
1. Регион: Турция входит (как Европа); добавить ли популярные направления (Египет, ОАЭ, Таиланд, Израиль) или всю планету (139,5 ГБ вместо ~100 ГБ).
2. Стили `ru` и `en`; `be`, `kk` и др. → `ru`; `uk` — английские подписи, пока не добавлен стиль `uk` (каждый стиль — свои рендереры и память).
3. Подпись «© OpenStreetMap» не переводится (имя собственное, юридическая отметка), только на картах от 100×100.
4. Параметры вне документации: `scale` 4 принимается (tdesktop), 5+ → 4; остальное прижимается к диапазонам документации, а не отклоняется.
5. Desktop без выбора точки (нет токена Mapbox): оставить как есть. Альтернатива — отдельная задача: патч `location_picker.cpp` на MapLibre GL JS со своим стилем, публичная раздача тайлов через край и свой геокодер.
6. Предел 60 новых карт в минуту на пользователя (защита рендера; владелец 2026-11-12 не хотел пределов для расшифровки — здесь предел только от злоупотребления, обычный показ его не достигает).
7. Обновление данных — вручную, раз в полгода.
8. Кэш на сервере 6 ч в Redis (клиенты кэшируют сами).

## Не проверено
- Поведение настоящего сервера Telegram: формат картинки, подпись данных, ответ на значения вне диапазонов, проверка `access_hash`.
- Размер вырезки региона (оценка 90–105 ГБ по выгрузкам Geofabrik) — замер `--dry-run` в части 4.
- Память и время рендера tileserver-gl с пулами `[1,1,1,1]…[2,2,2,1]` на два стиля; размер PNG (0,1–1 МБ — оценка).
- Совместимость `@protomaps/basemaps` 5.7.2 со сборкой тайлов 4.15.2 (документация не связывает версии) — проверить рендером на стенде (дороги, подписи, здания).
- Значки спрайта при `@3x`/`@4x`: в basemaps-assets есть только 1x и 2x; как tileserver-gl/MapLibre Native поступает с остальными масштабами — проверить на стенде; при пустых значках собрать спрайты `@3x`/`@4x` (spreet) из тех же SVG.
- Работает ли интерактивная карта Android (ключ Google Maps Telegram в чужом пакете) — проверка владельца, вне задачи.
- Что сессия Web K, уже державшая DC 4, корректно переходит на DC 1 после нового конфига (ожидается при следующем `help.getConfig`) — проверка в клиенте.

## Результат
Превью карт у геопозиций рисует свой сервис `maps` дома (tileserver-gl по данным Protomaps). Картинки отдаёт `upload.getWebFile`, координаты не уходят с сервера. Части 2–6 сделаны (часть 1 — общая основа `upload.getWebFile` из задачи 099, B). Часть 7 — выкладка дома — остаётся шагом выкладки. Код в партии `batch/20261115`, на прод не выкачен.

- **2.** `pkg/staticmap`:
  - размер, зум и масштаб прижимаются к допустимым диапазонам;
  - подпись «© OpenStreetMap» — на картах от 100×100;
  - PNG хранится в Redis, чтобы все части одного файла резались из одних байт;
  - одинаковые запросы рендерятся один раз, число и время рендеров ограничены, есть предел на человека.
- **3.** Источник `inputWebFileGeoPointLocation` в bff files: блок `Maps` в `bff.yaml` и переменные `MAPS_*`. Без `MAPS_URL` карты выключены, ответ `LOCATION_INVALID`, как раньше.
- **4.** Сервис `maps`:
  - образ со стилями `ru` и `en`, шрифтами и спрайтом внутри;
  - `docker-compose.maps.yaml`: память и CPU ограничены, корень только для чтения, порт не опубликован, отдельная сеть только с bff, в журнале нет адресов с координатами;
  - `make-region.sh`, `extract.sh` и `check.sh`; `region.geojson`.
- **5.** Стенд `fakemaps` и сквозные тесты: геоточки, разовые и живые геопозиции, один рендер на одновременные запросы, отказы, масштабы клиентов, `webfile_dc_id`.
- **6.** `docs/ops/maps.md`, тревоги, `memory.md` (предел 2560m по замеру пика около 2 ГиБ), `home-server.md`.

**Отклонения от брифа.** По коммитам и документам частей не найдены. Размер вырезки региона по замеру `--dry-run` — около 103 ГБ (оценка брифа была 90–105 ГБ).

**Решения.** Регион подтверждён владельцем: Европа, Россия, СНГ, обе Америки. Ждут подтверждения:
- п. 1 — добавить ли направления поездок или всю планету (139,5 ГБ);
- п. 2–8 — стили, подпись без перевода, параметры вне документации, Desktop без выбора точки, предел 60 карт в минуту, обновление раз в полгода, кэш 6 ч.

**Для выкладки** (`docs/ops/maps.md`):
- вырезка данных дома: около 103 ГБ диска и около 10 ГБ памяти вне `pluma.slice` на время вырезки;
- сборка образа, `MAPS_URL`;
- `docker-compose.maps.yaml` в каждой команде compose, пересоздающей bff.

**Известное.** Прирост памяти bff с картами — оценка, не замер. Desktop по-прежнему без выбора точки на карте.

**Не проверено.** В клиентах — пункты задачи 100 в `TESTING-BACKLOG.md`. На домашнем сервере не запускалось. Поведение Telegram — раздел «Не проверено» выше.

**Проверка.** Партия `batch/20261115`, полный прогон на CachyOS на `2c1b155cf`: `go build ./...` и `go vet -tags integration,e2e -unreachable=false` обоих модулей; `go test ./...` — 237 пакетов ok, proto — 3; `go test ./tools/...` (с журнальным аудитом) — 6; `-race` — 191 пакет ok; `make lint` — 0 замечаний; интеграционные тесты — 70 пакетов по одному, все ok; e2e на свежем стенде — 300 PASS, 0 FAIL, 2 SKIP (замер задержки и ручной тест). На прод не выкачено, в клиентах владельцем не проверено.
