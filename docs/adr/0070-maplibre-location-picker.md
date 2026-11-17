# 0070. Выбор места на карте: MapLibre в клиентах, свои векторные тайлы дома по HTTPS с ключом в пути

- Статус: proposed (2026-11-17). Карта — MapLibre: решение владельца 2026-11-17. Ждут его слова пункты «Что осталось подтвердить владельцу».
- Дата: 2026-11-17
- Задача: 112 (бриф `docs/tasks/112-maplibre-location-picker.md`; ветки `task/112-maps-public`, `task/112-android-maplibre`, `task/112-desktop-maplibre`).
- Заменяет в ADR 0063 два пункта «Что не делается»: выбор точки в Desktop и интерактивную карту Android. Остальное ADR 0063 (статические превью, данные, стили, подпись) не меняется.
- Опирается на: ADR 0063 (данные Protomaps и tileserver-gl дома), ADR 0057 и 0061 (дом — ядро, 443 идёт домой DNAT через VPS), ADR 0041, ADR 0071 (приём «отдельный сайт Caddy только при заданном имени»).

## Контекст

### Что видят клиенты сегодня
Исходники — `~/pluma-work/reference-src-new`: Android 13.2.1 `7be672ea`, tdesktop v7.4.1 `157f5cfc`.
- **Android.** Выбор места (вложение «Геопозиция», геопозиция группы, адрес бизнеса, трансляции) рисуется через `IMapsProvider`; провайдер создаёт `ApplicationLoader.onCreateMapsProvider()` → `new GoogleMapsProvider()` (`messenger/ApplicationLoader.java:98-107`). Google Maps SDK берёт ключ Telegram из манифеста (`config/release/AndroidManifest_SDK23.xml:41`), а наш пакет — `chat.pluma.app`: владелец видит на устройстве пустую карту (2026-11-17). Что дело именно в ограничении ключа по пакету и подписи — **не проверено**, со стороны Google мы его не видим. Перед выбором места клиент требует установленные Google Maps (`GoogleMapsProvider.java:76-78`, `AndroidUtilities.isMapsInstalled`) — подробности в брифе 112, «Контракт клиентов».
- **Desktop.** Пункт «Геопозиция» есть, только если в `tdesktop_config_map` app config задан непустой `maps` (`ui/controls/location_picker.cpp:343-351`, ключ читают `inline_bots/bot_attach_web_view.cpp:286-296`, опрос `boxes/create_poll_box.cpp:2593-2601`, Instant View `iv/editor/iv_editor_session.cpp:234-244`); адрес бизнеса — свой ключ `bmaps` (`settings/business/settings_location.cpp:71-81`). Значения — только строки: объект с одним нестроковым значением tdesktop отбрасывает целиком (`main/main_app_config.cpp:471-493`). Пикер — веб-вью с Mapbox GL JS 3.4.0 с `api.mapbox.com` (`location_picker.cpp:111-112`, `:406`). У Pluma ключа нет — пункта нет.
- **iOS** — MapKit на устройстве, работает. **Web K** интерактивной карты не имеет (бриф 112).

### Что уже есть дома
Сервис `maps` (ADR 0063): tileserver-gl v5.7.0 из образа `pluma-maps`, данные Protomaps `/opt/pluma-maps/pluma.pmtiles` (регион, z0–15), стили `@protomaps/basemaps` 5.7.2 по языкам, шрифты Noto Sans и спрайты из `basemaps-assets` в образе. Он рисует статические картинки для bff, наружу не виден и сидит в `tiles_net` без выхода.

### tileserver-gl v5.7.0 (прочитано по исходникам тега, 2026-11-17)
- Стиль с `serve_data` (флаг стиля, по умолчанию истина) отдаётся как JSON `/styles/<id>/style.json`; tileserver-gl сам переписывает в нём источник `pmtiles://{protomaps}` на `local://data/protomaps.json`, спрайт-имя — на `local://styles/<id>/sprite`, шрифты — на `local://fonts/{fontstack}/{range}.pbf` (`src/serve_style.js`, функция `add`), а при ответе меняет `local://` на публичный адрес (`fixUrl`, `src/utils.js`). Спрайт — `/styles/<id>/sprite[@2x].{json,png}`, шрифты — `/fonts/<стек>/<диапазон>.pbf`, TileJSON — `/data/protomaps.json`, тайлы — `/data/protomaps/{z}/{x}/{y}.pbf` (`src/serve_data.js`; тайлы отдаются с `Content-Encoding: gzip`).
- Публичный адрес: либо `--public_url`, либо из запроса — `Host`, протокол по `X-Forwarded-Proto` (в `src/server.js` включён `trust proxy`) и префикс пути из заголовка `X-Forwarded-Path` (`getUrlObject`, `getTileUrls` в `src/utils.js`). Из запроса — только для имён из `TILESERVER_GL_ALLOWED_HOSTS` (с `--config` переменная читается, пустая значит «все»; `src/main.js`); для чужого имени ссылки получаются относительными.
- В TileJSON `attribution` берётся из метаданных архива, а блок `tilejson` записи данных в конфиге его перекрывает (`src/serve_data.js`).
- CORS включён по умолчанию, `--no-cors` выключает; логирование — morgan, `--silent` пишет только неуспешные ответы, формат задаётся `--log_format`.
- Страницы просмотра (`/styles/<id>/`, `wmts.xml`) и списки (`/styles.json`, `/data.json`, `/index.json`) остаются и при `frontPage: false`.

## Решение

### 1. Клиенты рисуют карту сами, MapLibre
- **Android:** свой `IMapsProvider` на MapLibre Native (`org.maplibre.gl:android-sdk`) вместо Google; Google Maps SDK и ключ Telegram из сборки убираются; ворота «установите Google Maps» пропускают всегда. Детали — бриф 112, части E, и `clients/patches/android.md`.
- **Desktop:** `location_picker.cpp` и `picker.js` на MapLibre GL JS из ресурсов приложения со своим стилем вместо Mapbox. Детали — бриф 112, часть F, и `clients/patches/desktop.md`.
- iOS и Web K не меняются.

### 2. Тайлы отдаёт дом: отдельный контейнер `maps-tiles`
- Тот же образ `pluma-maps`, второй конфиг `config-public.json` (собирает `build-styles.mjs`): на каждый язык `MAPS_STYLES` светлый стиль `<язык>` и тёмный `<язык>-dark` (`namedFlavor('dark')`, спрайты `dark`, `dark@2x` того же коммита `basemaps-assets`), оба `serve_rendered: false`, `serve_data: true`; `serveStaticMaps`, `frontPage`, `serveAllFonts`, `serveAllStyles` — `false`. Рендереров нет — нет и статических картинок с координатами в URL, и памяти под них. Тёмные стили есть только в публичном конфиге.
- Рисующий `maps` не меняется и наружу по-прежнему не виден: публичный трафик не отнимает рендеры у превью чатов.
- В TileJSON `attribution` — «© OpenStreetMap» со ссылкой на `openstreetmap.org/copyright` (блок `tilejson` данных): её показывает кнопка атрибуции MapLibre, ODbL выполняется (ADR 0063, §6).
- Сеть: новая внутренняя `maps_public_net` (`internal: true`) — только `maps-tiles` и Caddy. У `maps-tiles` нет ни выхода наружу, ни других сетей, ни опубликованного порта. Caddy в etcd не регистрируется, порядок его интерфейсов неважен; `TestComposeServicesRegisteredAddress` проверяет только сервисы образа `TEAMGRAM_IMAGE`.

### 3. Публикация: Caddy на отдельном имени, ключ в пути
- Сайт `teamgramd/deploy/web/maps-public.caddy` подключается, как сайт источника CDN (ADR 0071), только при непустом `PLUMA_MAPS_DOMAIN`; контейнер `web` не стартует, если при заданном имени `MAPS_PUBLIC_KEY` пуст или не подходит (16–128 символов base64url).
- Адрес для клиентов: `https://<PLUMA_MAPS_DOMAIN>/<MAPS_PUBLIC_KEY>`. Под ключом — только `GET`/`HEAD` ресурсов карты: `styles/<id>/style.json`, `styles/<id>/sprite[@2x].{json,png}`, `fonts/<стек>/<a>-<b>.pbf`, `data/protomaps.json`, `data/protomaps/<z>/<x>/<y>.pbf`; `OPTIONS` по этим путям — 204. Всё остальное, в том числе без ключа и с чужим ключом, — одинаковый пустой 404: страницы просмотра, списки и статические картинки tileserver-gl недостижимы.
- **Ключа нет в `maps-tiles`:** Caddy срезает его из пути и передаёт как `X-Forwarded-Path`, а `Host` и `X-Forwarded-Proto` выставляет сам (что прислал клиент, не проходит); tileserver-gl строит из них ссылки стиля и TileJSON, и только для имени из `TILESERVER_GL_ALLOWED_HOSTS` = `PLUMA_MAPS_DOMAIN`. Смена ключа — `.env`, перезапуск `web` и `bff`, без пересборки и без `maps-tiles`.
- CORS: `Access-Control-Allow-Origin: *`, `Access-Control-Allow-Methods: GET, HEAD` — пикер Desktop живёт на своей схеме веб-вью, это другой источник; данные открытые, доступ ограничивает ключ. Собственный CORS tileserver-gl выключен (`--no-cors`): два `Access-Control-Allow-Origin` браузер отвергает.
- Кэш: тайлы, шрифты, спрайты — `public, max-age=86400`; `style.json` и TileJSON — `no-cache` (в них ключ и версия данных). Сжимается только JSON (тайлы уже gzip).
- Журналов нет: у сайта нет `log`, у `maps-tiles` — `--silent` и формат без `:url`. Без `Server` и `Via`, с HSTS и `nosniff`.

### 4. Ключ доступа
- `MAPS_PUBLIC_KEY` — 32 символа base64url в `.env` (`openssl rand -base64 24 | tr '+/' '-_'`). Это не секрет уровня паролей: его получает каждый вошедший пользователь. Он не даёт чужим превратить домашний канал в бесплатный тайловый сервер.
- Смена: правка `.env` (`MAPS_PUBLIC_KEY` и `MAPS_PUBLIC_URL`), перезапуск `web` и `bff`; клиенты получат новый адрес со следующим app config. Открытые экраны карты до переоткрытия перестают грузить новые тайлы — допустимо.

### 5. Как клиенты узнают адрес: app config от bff
- `teamgramd/bin/app_config.json` статичен, поэтому bff при старте дописывает ключи из `MAPS_PUBLIC_URL` (`https://<домен>/<ключ>`; только `https`, без логина, query, фрагмента и `/` на конце, ключ — единственный сегмент пути) и `MAPS_STYLES`:
  - `tdesktop_config_map`: `maps` и `bmaps` = `MAPS_PUBLIC_URL` (другие ключи этого объекта из файла сохраняются); `geo` и `bgeo` не передаются — своего геокодера нет;
  - `pluma_maps_url` — та же строка, для Android;
  - `pluma_maps_styles` — список светлых стилей, клиент выбирает язык подписей (правило — `docs/ops/maps.md`, «Языки подписей»), тёмный — `<id>-dark`.
- Хэш app config считается уже с этими ключами. Неверный `MAPS_PUBLIC_URL` останавливает bff с понятной причиной (сам адрес с ключом в лог не пишется). Без `MAPS_PUBLIC_URL` ключей нет, и всё как сегодня.

### 6. Геокодер
Адрес выбранной точки и поиск по названию — геокодер устройства, как у Telegram: Android `android.location.Geocoder`, macOS `CLGeocoder`; Windows и Linux пишут координаты (`location_picker.cpp:626-630`), потому что `geo` не передаётся. Свой геокодер — отдельное решение владельца.

## Последствия
- Выбор места работает на Android и Desktop без Google и Mapbox; ключ Telegram больше не едет в APK.
- Дом узнаёт IP устройства и номера тайлов видимой области, пока открыт выбор места, — примерно где пользователь смотрит карту. Это тот же сервер, который получит отправленную точку; наружу ничего не уходит, журналов нет.
- Новый контейнер дома: процесс Node без рендереров, оценка 150–300 МиБ (лимит `MAPS_TILES_MEMORY=512m`, `MAPS_TILES_CPUS=1`) — **замерить** на стенде. Трафик идёт через туннель VPS (ADR 0061): одно открытие — 20–40 тайлов по 20–100 КБ, 1000 открытий в месяц — около 5–10 ГБ (оценка).
- Карта видна только в регионе данных ADR 0063; вне его — фон стиля.
- MapLibre Native тянет свой OkHttp: прокси Telegram (MTProxy/SOCKS) на карту не действует — как было с Google Maps SDK.
- Признак пересмотра: заметный рост трафика тайлов в туннеле, утечка ключа наружу (тогда — короткоживущие подписанные адреса) или решение владельца о своём геокодере.

## Рассмотренные альтернативы
- **Без ключа (открытая раздача).** Проще, но любой, кто найдёт имя, получает бесплатный тайловый сервер на домашнем канале. Отвергнуто.
- **Ключ в пути (выбрано).** Достаточно при закрытой аудитории: ключ получает каждый вошедший, но не посторонний; меняется без правки клиентов; клиентам ничего не нужно, кроме базового адреса.
- **Подписанные короткоживущие URL.** Защищают и от утечки ключа вошедшим, но MapLibre грузит тайлы по шаблону из TileJSON — подпись на каждый тайл потребовала бы перехвата запросов в обоих клиентах и обновления подписи при открытом экране. Сложность без выигрыша при закрытой аудитории.
- **Ключ в заголовке.** MapLibre GL JS умеет `transformRequest`, но это лишняя правка обоих клиентов и CORS-предзапрос на каждый тайл; путь проще.
- **`--public_url` с ключом у tileserver-gl.** Работает, но ключ оказывается в команде контейнера и требует его пересоздания при смене; `X-Forwarded-Path` от Caddy держит ключ в одном месте.
- **Раздача из рисующего `maps`.** Публичный трафик отнимал бы рендеры у превью и открыл бы наружу контейнер со статическими картинками; отдельный контейнер дешевле и уже.
- **Чужие тайлы (OSM, MapTiler, Mapbox, Google).** Координаты просмотра уходят наружу, против ADR 0057/0061; правила OSM для продукта невыполнимы (ADR 0063).

## Что осталось подтвердить владельцу
1. Имя `maps.pluma.chat` (`PLUMA_MAPS_DOMAIN`).
2. Ключ в пути, а не открытая раздача или подписанные URL.
3. «Спутник» и «Гибрид» убираются (снимков нет).
4. Адрес точки на Windows и Linux — координаты (без своего геокодера); свой геокодер сейчас не делать.
5. Адрес бизнеса в Desktop берёт тот же адрес тайлов (`bmaps`), что и вложение (`maps`).
