# 112. Выбор места на карте: MapLibre и свои тайлы вместо Google Maps и Mapbox

- Статус: открыта (бриф 2026-11-17), код не начат.
- Поставлена владельцем 2026-11-17: в Pluma Android выбор места показывает пустую карту (подтверждено на устройстве); «выбор места делаем через MapLibre».
- Решение: нужно новое ADR (следующий свободный номер; 0070 в индексе не занят), которое заменяет в ADR 0063 пункты «Что не делается» о выборе точки в Desktop и об интерактивной карте Android. Опирается на ADR 0063 (свои данные Protomaps и tileserver-gl дома), 0057 и 0061 (дом — ядро, 443 идёт домой DNAT через VPS), 0041.
- Зависит от: 100 (сервис `maps`, данные `/opt/pluma-maps/pluma.pmtiles`, образ `pluma-maps`), 107 (приём «отдельный сайт Caddy подключается только при заданном имени», `cdn-origin.caddy`).
- Ветки (план): сервер `task/112-maps-public`; Android `task/112-android-maplibre` (`~/pluma-android`); Desktop `task/112-desktop-maplibre` (`pluma-desktop`). iOS и Web K не меняются.

## Цель
Выбор места на карте (вложение «Геопозиция», геопозиция группы, адрес бизнеса, стикер места в историях, экран трансляций) на Android и Desktop работает на векторной карте MapLibre, тайлы, стиль, шрифты и значки которой отдаёт дом Pluma по HTTPS. Ни Google Maps SDK с ключом Telegram, ни Mapbox, ни один чужой картографический сервис клиенты Pluma для карты не используют. Видимое поведение — как у Telegram: перетаскиваемая карта, булавка в центре, «моё местоположение», отправка выбранной точки, тёмная карта в тёмной теме.

## Почему карта пустая (прочитано)
Исходники — `~/pluma-work/reference-src-new`, совпадают с закреплёнными: Android 13.2.1 `7be672ea` (`upstream.env` в `origin/main` `pluma-android`), tdesktop v7.4.1 `157f5cfc`, tweb `aa74832e`. Номера строк — апстрим.
- Android собирается `:TMessagesProj_App:assembleAfatRelease` (`clients/patches/android.md:19`); flavor `afat` берёт манифест `TMessagesProj/config/release/AndroidManifest_SDK23.xml` (`TMessagesProj_App/build.gradle:132-145`), где `com.google.android.maps.v2.API_KEY` = ключ Telegram (`AndroidManifest_SDK23.xml:41`; так же `config/debug/AndroidManifest_SDK23.xml:40`, `release/AndroidManifest.xml:33`, `release/AndroidManifest_standalone.xml:127`). Патч Pluma ключ не трогает (`ci/patch.py` в `origin/main`, поиск по `API_KEY`/`maps`). Ключ Google Maps Android ограничивается пакетом и подписью; у нас пакет `chat.pluma.app` — SDK не получает тайлов (наблюдение владельца на устройстве; само ограничение ключа — **не проверено**, со стороны Google мы его не видим).
- Карта выбора места идёт через абстракцию `IMapsProvider` (`messenger/IMapsProvider.java`, 199 строк, `@Keep`). Провайдер создаёт `ApplicationLoader.getMapsProvider()` → `onCreateMapsProvider()` → `new GoogleMapsProvider()` (`messenger/ApplicationLoader.java:98-107`); Huawei-сборка подставляет `HuaweiMapsProvider` (`TMessagesProj_AppHuawei/.../HuaweiApplicationLoader.java:35-39`), этот модуль Pluma уже исключает (`clients/patches/android.md:1006`). Классы `com.google.android.gms.maps` используются только в `GoogleMapsProvider.java` (поиск по `TMessagesProj/src`, `TMessagesProj_App/src`).
- Desktop: пункт «Геопозиция» показывается, только если `LocationPicker::Available()` (`ui/controls/location_picker.cpp:343-351`): веб-вью с `customSchemeRequests`, `customReferer` и непустой `tdesktop_config_map.maps` из app config (`inline_bots/bot_attach_web_view.cpp:286-295`, `:3127-3134`). У Pluma ключа нет (ADR 0063, «Контекст»), пункта нет.
- iOS — MapKit на устройстве, работает (ADR 0063 §1).
- Web K: интерактивной карты нет. Пункт «location» меню вложений — блок карты rich-сообщения (`components/chat/input.ts:1540-1549`, проверка `isRichMessageMapAvailable`), картинка — `upload.getWebFile` (`components/popups/richMessageLocation.tsx:10`, `:75-90`), её уже рисует дом (задача 100). Обычная геопозиция — только по запросу кнопки бота, через `navigator.geolocation` (`lib/appImManager.ts:4371-4407`). Менять нечего.

## Область
В задаче:
- Сервер: публичная раздача векторных тайлов, стилей (светлый и тёмный на язык), шрифтов и значков дома по HTTPS на отдельном имени; ключ доступа в пути; новые ключи app config, собираемые bff из окружения.
- Android: провайдер `IMapsProvider` на MapLibre Native вместо Google; Google Maps SDK и ключ Telegram из сборки убираются.
- Desktop: `location_picker.cpp` и `picker.js` на MapLibre GL JS из ресурсов приложения со своим стилем.
- Документы: ADR, `docs/ops/maps.md`, `docs/ops/app-config.md`, `docs/ops/memory.md`, `clients/patches/android.md`, `desktop.md`, TESTING-BACKLOG.

Не в задаче:
- Свой геокодер (адрес по точке и поиск по названию). Остаётся геокодер устройства, как у Telegram (см. «Геокодер и поиск»). Свой — отдельная задача, если владелец захочет.
- Поиск заведений (`venue_search_username`): бота мест нет (задача 099, решение владельца 2026-11-16); Android 13.2.1 его и не вызывает (`ui/Adapters/BaseLocationAdapter.java:91`).
- Спутник и гибрид: снимков у нас нет; пункты меню убираются.
- iOS (MapKit) и Web K.
- Статические превью — задача 100, без изменений.

## Контракт клиентов
**Android (что вызывает экран и что должен уметь провайдер).**
- `ChatAttachAlertLocationLayout`: `onCreateMapView` до `initializeMaps` (`ui/Components/ChatAttachAlertLocationLayout.java:581`, `:641`) — провайдер обязан сам инициализировать MapLibre (`MapLibre.getInstance(context)`) до создания `MapView`; перехват касаний `setOnDispatchTouchEventInterceptor`/`setOnInterceptTouchEventInterceptor` (`:582-600`); тёмный стиль `loadRawResourceStyle(..., R.raw.mapstyle_night)` → `setMapStyle` (`:650-651`, при смене темы `:1667`); `getUiSettings()` без кнопок и компаса (`:964-966`); слушатели `setOnCameraMoveStartedListener` (`:967`), `setOnCameraIdleListener` (`:986`), `setOnMyLocationChangeListener` (`:1000`); `setPadding` (`:1316`); `setMyLocationEnabled` (`:765`, `:960`, `:1488`, `:1537`); меню типа карты (`:336-337`, `:352-356`).
- `LocationActivity` (трансляции, геопозиция группы, адрес бизнеса через `LOCATION_TYPE_BIZ` из `ui/Business/LocationActivity.java:504`): меню типа карты (`ui/LocationActivity.java:260-262`, `:443-454`), `onCreateMapView` (`:1032`), стиль (`:1108`, `:3140`), маркеры с картинкой и поворотом (`:1944-2010`, `:2043`), границы `newCameraUpdateLatLngBounds` (`:2478-2580`), снимок карты через `getGlSurfaceView()` (`:2795-2813`; при `null` снимка нет — метод `default` в интерфейсе), круги (`addCircle`, интерфейс `ICircle`).
- Ворота перед выбором места: `AndroidUtilities.isMapsInstalled()` (`messenger/AndroidUtilities.java:1864-1886`) требует установленного приложения `getMapsAppPackageName()` = Google Maps (`GoogleMapsProvider.java:76-78`), иначе предлагает поставить его и **не открывает выбор** (`ui/Components/ChatAttachAlert.java:2824-2826`; ещё 10 мест: `LaunchActivity.java:7529`, `ChatActivity.java:42145, 42550`, `ChatEditActivity.java:881`, `GroupCreateFinalActivity.java:617`, `ChannelAdminLogActivity.java:3182`, `SharedMediaLayout.java:8595`, `ChatAttachAlert.java:4539`, `iv/RichEditor.java:1936`, `iv/ChatAttachAlertRichLayout.java:1012`). Наша карта встроена — ворота должны пропускать всегда.
- Постоянные причины движения камеры у Google и MapLibre называются одинаково, но числа могут отличаться (**не проверено** по исходникам MapLibre): сопоставлять по именам констант, не по числам.

**Desktop.**
- HTML страницы — `PickerContent()` (`location_picker.cpp:100-143`): `picker.js`/`picker.css` из ресурсов приложения через свою схему (`:548-567`, имя файла проверяется `^[a-zA-Z\.\-_0-9]+$`), Mapbox GL JS 3.4.0 с `api.mapbox.com` (`:111-112`). Веб-вью создаётся с `dataRequestRedirectHost = "api.mapbox.com"` (`:406`); `picker.js` на хостах, отличных от `desktop-app-resource`, переписывает адрес API Mapbox на тот же источник с префиксом `/api.mapbox.com` (`Resources/picker_html/picker.js:52-56`). Как именно веб-вью перенаправляет такие запросы — в `lib_webview`, его в локальных выгрузках нет (каталог пуст) — **прочитать** перед частью D (подмодуль `Telegram/lib_webview` на коммите тега v7.4.1).
- `LocationPicker.init({token, center, bounds})` (`location_picker.cpp:650-656`), стиль по умолчанию Mapbox Standard с `config.basemap.lightPreset` день/ночь (`picker.js:57-59`), смена темы — `setConfigProperty` (`picker.js:41-50`): у MapLibre этого нет, ночь — `setStyle` тёмного стиля.
- Адрес выбранной точки: `Platform::ResolveLocationAddress` — macOS `CLGeocoder` (`platform/mac/current_geo_location_mac.mm:120-140`), Windows и Linux пусто (`platform/win/current_geo_location_win.cpp:62-67`, `platform/linux/current_geo_location_linux.cpp:98-103`); при пустом и непустом `geo` — Mapbox (`core/current_geo_location.cpp:199-215`, запрос `:70-75`). Без `tdesktop_config_map.geo` Windows/Linux пишут координаты `(lat, lon)` (`location_picker.cpp:626-630`).

## Дизайн

### Сервер: публичная раздача (`maps-tiles`)
- **Отдельный контейнер** `maps-tiles` из того же образа `pluma-maps` со своим конфигом `teamgramd/deploy/maps/config-public.json` (собирает `build-styles.mjs` вторым выходом): `serve_data: true` для `protomaps`, у стилей `serve_rendered: false`, `serveStaticMaps: false`, `frontPage: false`, `serveAllFonts: false`, `serveAllStyles: false`. Рисующий `maps` не меняется и наружу по-прежнему не виден. Причины: публичный трафик не отнимает рендеры у превью чатов; наружу нет ни одного URL статической картинки (в нём координаты, и он дорог); у контейнера нет рендереров — мало памяти.
- **Стили:** к светлым `<lang>` добавить тёмные `<lang>-dark` (`namedFlavor('dark')` `@protomaps/basemaps` 5.7.2, спрайты `dark`, `dark@2x` из `basemaps-assets` того же коммита `028c18f7`). Тёмные — только в публичном конфиге (превью их не просят). Языки — тот же `MAPS_STYLES`.
- **Адреса, которые видит клиент:** `https://<PLUMA_MAPS_DOMAIN>/<ключ>/styles/<id>/style.json`; из него tileserver-gl сам строит ссылки на TileJSON `/data/protomaps.json`, тайлы `/data/protomaps/{z}/{x}/{y}.pbf`, шрифты `/fonts/{fontstack}/{range}.pbf`, спрайты `/styles/<id>/sprite[@2x].{json,png}` — с публичным префиксом из `--public_url https://<домен>/<ключ>/` (параметр CLI tileserver-gl — **сверить** с документацией выпуска v5.7.0 при реализации, https://tileserver.readthedocs.io/en/latest/usage.html).
- **Сеть:** новая внутренняя сеть `maps_public_net` (`internal: true`) только для `maps-tiles` и `caddy`. `maps-tiles` в `tiles_net` не входит, выхода наружу у него нет. Caddy не регистрируется в etcd, порядок имён сетей (урок 2026-11-17, `TestComposeServicesNetworkFirst`) его не касается, но тест должен покрывать и новый файл compose.
- **Caddy:** файл `teamgramd/deploy/web/maps-public.caddy`, подключается как `cdn-origin.caddy` — только при непустом `PLUMA_MAPS_DOMAIN` (`docker-compose.web.yaml:31-49`, тот же приём ссылки в `/etc/caddy/maps-public.d/`). В блоке:
  - только `GET`/`HEAD`/`OPTIONS`; путь обязан начинаться с `/{$MAPS_PUBLIC_KEY}/`, иначе 404 одинаково для всего; `uri strip_prefix`; дальше белый список путей (`/styles/*/style.json`, `/styles/*/sprite*`, `/fonts/*`, `/data/protomaps.json`, `/data/protomaps/*`) → `reverse_proxy maps-tiles:8080`; остальное 404;
  - `Access-Control-Allow-Origin: *`, `Access-Control-Allow-Methods: GET, HEAD`, ответ на `OPTIONS` без тела — для веб-вью Desktop (своя схема — другой источник) и на будущее; данные открытые, доступ ограничивает ключ;
  - `Cache-Control`: тайлы и шрифты `public, max-age=86400`, `style.json` и TileJSON `no-cache` (ключ и версия данных меняются);
  - `encode zstd gzip` только для JSON (тайлы уже сжаты gzip в pmtiles);
  - без журнала доступа (в пути ключ, а номера тайлов — где смотрит пользователь), `-Server`, `-Via`, HSTS, `X-Content-Type-Options`.
- **Ключ доступа** `MAPS_PUBLIC_KEY` — 32 случайных символа base64url в `.env`, не секрет уровня паролей (его получает каждый вошедший пользователь), но не даёт чужим превратить домашний канал в бесплатный тайловый сервер. Смена — правка `.env`, перезапуск `caddy`, `maps-tiles`, `bff`; клиенты получат новый адрес со следующим app config (Android — минуты, tdesktop — периодически). На время смены старые открытые экраны карты перестают грузить новые тайлы до переоткрытия — допустимо.
- **app config** (bff): файл `teamgramd/bin/app_config.json` статичен (`docs/ops/app-config.md`, «Где лежит»), поэтому bff при загрузке дописывает в ответ ключи из окружения, если `MAPS_PUBLIC_URL` задан (`https://<домен>/<ключ>`; проверка: только `https`, без query и фрагмента, без `/` на конце):
  - `tdesktop_config_map`: `{"maps": "<MAPS_PUBLIC_URL>"}` — проходит ворота Desktop без патча ворот; `geo` не передаётся;
  - `pluma_maps_url`: та же строка — для Android;
  - `pluma_maps_styles`: список `MAPS_STYLES` (клиенту выбрать язык подписей).
  Хэш app config считается уже с ними. Без `MAPS_PUBLIC_URL` ключей нет, и всё ведёт себя как сегодня (Desktop без пункта, Android — см. ниже). Тест `appconfig` «каждый ключ в таблицах» дополняется этими ключами.
- **Что узнаёт дом:** IP устройства и номера тайлов видимой области при открытом выборе места — то есть примерно где пользователь смотрит карту. Это тот же сервер, который получит отправленную точку; наружу ничего не уходит, журналов нет (Caddy без `log`, tileserver-gl с `--silent` и форматом без `:url`, как у `maps`). Кэш тайлов MapLibre на устройстве — в данных приложения.
- **Память и CPU** (оценка, **замерить** в части C): tileserver-gl без рендереров — процесс Node, читающий pmtiles; ожидание 150–300 МиБ, лимит `MAPS_TILES_MEMORY=512m`, `MAPS_TILES_CPUS=1`. Нагрузка: одно открытие выбора — 20–40 тайлов по 20–100 КБ и несколько диапазонов шрифтов; с перетаскиванием 2–10 МБ. 100 пользователей — единицы одновременных экранов, CPU — доли ядра, диск — случайные чтения по pmtiles (общий с медиасервером). Трафик идёт через туннель VPS (ADR 0061): 1000 открытий в месяц ≈ 5–10 ГБ.
- **Метрики:** счётчик запросов и байт Caddy по сайту (если метрики Caddy уже собираются; иначе `docker stats` в `check.sh`). Алерт не нужен: превью чатов от этого сервиса не зависят.

### Android: `PlumaMapsProvider` на MapLibre Native
- Зависимость `org.maplibre.gl:android-sdk:13.7.1` (Maven Central, 2026-10-15; лицензия в POM — BSD-2-Clause, https://github.com/maplibre/maplibre-native). Транзитивно: `okhttp 4.12.0`, `kotlin-stdlib 2.2.10`, `kotlinx-coroutines-core 1.10.2`, `timber 5.0.1`, `androidx.fragment 1.8.9`, `android-sdk-geojson`/`-turf 6.0.1`, `maplibre-android-gestures 0.0.4`. Kotlin-исходников в Telegram нет (0 файлов `.kt`), плагин Kotlin 2.1.0 (`build.gradle:13`) их не компилирует — конфликта метаданных ждать не от чего, но сборка это подтвердит. minSdk приложения 23 (`TMessagesProj_App/build.gradle:169`); minSdk MapLibre 13.7.1 — **сверить** с манифестом AAR. Выпуски `2026.x-globe` — экспериментальная ветка, не берём.
- Новый файл `TMessagesProj/src/main/java/org/telegram/messenger/PlumaMapsProvider.java` (копируется патчем целиком, SHA-256 в `patch.py`), реализует весь `IMapsProvider`:
  - `initializeMaps` и `onCreateMapView` — `MapLibre.getInstance(appContext)` один раз; `MapView` в обёртке `FrameLayout` с теми же перехватчиками касаний, что у `GoogleMapView` (`GoogleMapsProvider.java:531-640`);
  - стиль — `pluma_maps_url` + `/styles/<lang>[-dark]/style.json`, язык — язык интерфейса по правилу bff (`docs/ops/maps.md`, «Языки подписей»: есть в `pluma_maps_styles` — он; `be, kk, ky, tg, uz, hy, az` → `ru`; иначе `en`); `loadRawResourceStyle(R.raw.mapstyle_night)` возвращает признак «тёмный», любой другой ресурс — светлый; `setMapStyle(null)` — светлый;
  - маркеры и круги — свои `GeoJsonSource` + `SymbolLayer`/`FillLayer`/`LineLayer` (иконка-Bitmap в стиль через `addImage`, поворот — `icon-rotate`, пунктир круга — `line-dasharray`); устаревший API аннотаций MapLibre не используется; всё пересоздаётся после смены стиля;
  - `setMyLocationEnabled` — `LocationComponent` после загрузки стиля и только при выданном разрешении (иначе тихо выключено, как у Google без разрешения), источник — системный `LocationManager`, без Play Services; `setOnMyLocationChangeListener` — из его обновлений;
  - `getProjection().toScreenLocation`, `CameraUpdateFactory` (точка, точка+масштаб, границы с отступом), `getCameraPosition`, `setPadding`, `getMaxZoomLevel`/`getMinZoomLevel` (максимум ставим 18: данные до z15, дальше дорисовка), `setOnMapLoadedCallback` — по `onDidFinishLoadingStyle` + первому `onDidFinishRenderingMap`;
  - `setMapType`: всегда обычная карта; `getUiSettings`: кнопок масштаба у MapLibre нет — no-op; компас и логотип выключены, **подпись «© OpenStreetMap» (кнопка атрибуции) остаётся** — требование ODbL (ADR 0063 §6);
  - `getGlSurfaceView()` — `null` (MapLibre рисует в `SurfaceView`/`TextureView`); снимок карты при закрытии экрана не делается — как сегодня у провайдеров без GL-вью;
  - `getMapsAppPackageName()` — пакет самого приложения (он всегда установлен), `getInstallMapsString()` — существующая строка `InstallGoogleMaps` не показывается никогда.
- Если `pluma_maps_url` ещё нет (сервер без `MAPS_PUBLIC_URL`, первый запуск до app config): карта не создаётся, экран выбора работает без неё как сейчас со списком «моя геопозиция»; ворота пропускают.
- Сетевой стек MapLibre — свой OkHttp; прокси Telegram (MTProxy/SOCKS) на него не действует — так же было с Google Maps SDK. Прокси-клиент для карт — не в задаче.
- Правки апстрима патчем:
  - `messenger/ApplicationLoader.java:105-107` — `onCreateMapsProvider()` возвращает `new PlumaMapsProvider()`;
  - `GoogleMapsProvider.java` удаляется; `TMessagesProj/build.gradle:66` `play-services-maps` → `org.maplibre.gl:android-sdk:13.7.1` (`play-services-location` `:70` остаётся — его используют другие места);
  - мета-данные `com.google.android.maps.v2.API_KEY` удаляются из четырёх манифестов (строки выше): ключ Telegram больше не едет в APK;
  - `messenger/MessagesController.java`, разбор app config — поля `plumaMapsUrl`, `plumaMapsStyles` (строка `https://…`, иначе пусто); сохраняются в `mainPreferences`, как прочие ключи;
  - меню «Спутник»/«Гибрид» убирается: `ChatAttachAlertLocationLayout.java:336-337`, `LocationActivity.java:453-454` (кнопка типа карты с одним пунктом не нужна — прятать кнопку целиком, `mapTypeButton`);
  - `AndroidUtilities.isMapsInstalled` (`:1864`) — ничего не меняется: пакет провайдера — само приложение, проверка проходит (меньше правок, чем трогать 11 вызовов).
- ProGuard/R8: `IMapsProvider` уже `@Keep`; правила MapLibre (JNI-классы) — в `consumer-proguard` AAR (**сверить** наличие `proguard.txt` в AAR 13.7.1); если нет — добавить в `TMessagesProj/proguard-rules.pro` `-keep class org.maplibre.android.** { *; }` по документации MapLibre. Проверка — релизная сборка `afat` и открытие карты на устройстве (R8 ломает JNI только в рантайме).
- Размер APK: AAR 13.7.1 — 18,7 МБ (четыре ABI, `.so` основная часть); `afat` собирает все четыре ABI (`TMessagesProj_App/build.gradle:132-135`) — ожидание +15–20 МБ к APK, минус клиент Google Maps (небольшой). **Замерить** в CI (размер до/после в отчёте).

### Desktop: MapLibre GL JS из ресурсов
- `maplibre-gl` 6.16.0 (npm, 2026-10-25, BSD-3-Clause; 6.17+ моложе двух недель на момент брифа — взять самый новый выпуск не моложе двух недель на день реализации, зафиксировать SHA-256). Файлы `maplibre-gl.js` и `maplibre-gl.css` кладутся патчем в `Telegram/Resources/picker_html/` и в `Resources/qrc/telegram/picker.qrc` (имена проходят проверку `location_picker.cpp:557`); лицензия — в `LICENSE` сборки и в лицензиях приложения. Прирост бинарника — около 1 МБ (оценка).
- `location_picker.cpp:111-112` — `<script src="/location/maplibre-gl.js">`, `<link href="/location/maplibre-gl.css">`.
- `location_picker.cpp:406` — `dataRequestRedirectHost` = хост из `mapsToken` (наш домен) вместо `api.mapbox.com`, если механизм перенаправления (после чтения `lib_webview`) нужен на Windows/macOS; иначе адреса абсолютные, и работает CORS Caddy. Выбор — после чтения, записать в `desktop.md` с цитатой.
- `location_picker.cpp:650-656` — в `init` передаются `base` (значение `mapsToken`), `lang` (тот же выбор языка, что на Android, по `Lang::GetInstance()`), `night`.
- `picker.js`: `mapboxgl` → `maplibregl`; `style: base + '/styles/' + lang + (night ? '-dark' : '') + '/style.json'`; `updateStyles` (`:41-50`) вместо `setConfigProperty` вызывает `map.setStyle(...)` при смене ночи; ветка `API_URL` (`:54-56`) удаляется или переводится на наш хост по итогам чтения `lib_webview`; `attributionControl` включён (подпись OSM).
- Своя CSP страницы (сейчас её нет): `<meta http-equiv="Content-Security-Policy">` с `connect-src`/`img-src` только к нашему домену, `worker-src blob:` (MapLibre запускает воркеры из blob), `script-src 'self' 'unsafe-inline'` (встроенный `LocationPicker.notify` в теле). Проверить на трёх ОС; если какая-то веб-вью не принимает CSP через meta для своей схемы — убрать и записать.
- MapLibre GL JS 5+ требует WebGL2 (**не проверено** для конкретных версий WebKitGTK у пользователей Linux); при ошибке `picker.js` отправляет событие ошибки, и Desktop показывает свой текст ошибки веб-вью (`showWebviewError`, `location_picker.cpp:572`).
- Адрес точки: `geo` не передаём — macOS `CLGeocoder`, Windows/Linux координаты. Отличие от Telegram (у него Mapbox), записать в `desktop.md`.

### Геокодер и поиск
- Android: адрес выбранной точки (`LocationController.java:1050-1057`) и поиск по названию (`BaseLocationAdapter.java:91-104`) — системный `android.location.Geocoder`, как у официального клиента; на устройствах с сервисами Google запрос идёт в Google, без них список пуст. Не патчим (правило минимальных правок клиентов, ADR 0063 «Что не делается»).
- Свой геокодер — отдельное решение владельца. Варианты для будущей задачи: Photon (владелец отклонил, 6–8 ГБ), Nominatim (больше Photon), Pelias (несколько сервисов, Elasticsearch); облегчённо — обратное геокодирование по слоям `places`/`roads` наших же тайлов в bff (только «улица, город», без номеров домов; состав слоёв Protomaps 4.x — **не проверено**).

### Пределы и ошибки
- Caddy: тело запроса не принимается; несуществующий стиль/тайл — 404 от tileserver-gl; `maps-tiles` недоступен — 502 без `Server`.
- Нет данных (`pluma.pmtiles` отсутствует) — `maps-tiles` не стартует, как `maps`; `check.sh` проверяет и его.
- После перезапуска: состояния нет; клиенты повторяют загрузку тайлов сами.

## Порядок работ
- **A. ADR** (новое, заменяет пункты ADR 0063) — по этому брифу, с вариантами «без ключа / ключ в пути / подписанные URL» и выбором.
- **B. Стили и конфиг:** `build-styles.mjs` — тёмные стили и `config-public.json`; `Dockerfile` — спрайты `dark`; тест сборки стилей (существующий для 100, если есть) — оба выхода.
- **C. Compose и Caddy:** `docker-compose.maps.yaml` — сервис `maps-tiles`, сеть `maps_public_net`; `docker-compose.web.yaml` — `PLUMA_MAPS_DOMAIN`, ссылка `maps-public.d`, сеть у `caddy`; `maps-public.caddy`; `check.sh` — `style.json`, один тайл, шрифт, отказ без ключа и на `/styles/*/static/*`. Замер памяти `maps-tiles`.
- **D. bff:** ключи app config из `MAPS_PUBLIC_URL`, `MAPS_STYLES`; конфигурация (`teamgramd/etc{,2}/bff.yaml`), проверка значения; тесты.
- **E. Android** — патч и записи в `clients/patches/android.md` (новые A-номера), тесты `patch_test.py` (DAMAGE на каждое место правки).
- **F. Desktop** — чтение `lib_webview`, патч, `desktop.md` (D-номера), тесты `patch_test.py`.
- **G. Документы:** `docs/ops/maps.md` (раздел «Публичная раздача»), `app-config.md`, `memory.md`, `home-server.md` (имя, A-запись), TESTING-BACKLOG, STATUS.

## Тесты
- bff: модульные — ключи app config есть только при заданном `MAPS_PUBLIC_URL`, неверный URL (не https, query, `/` на конце) не даёт bff стартовать с понятной причиной, хэш меняется; e2e `TestAppConfigMapsKeys` через `pkg/mtclient` (`help.getAppConfig`).
- compose: `TestComposeServicesNetworkFirst` и тест «у `maps-tiles` только внутренние сети, нет `ports`».
- Caddy: `caddy validate` с заданным и пустым `PLUMA_MAPS_DOMAIN` (как в 107); интеграционный прогон стенда: `style.json` с ключом — 200 и ссылки внутри начинаются с публичного префикса; без ключа и с чужим ключом — 404; `/styles/ru/static/...` — 404; `OPTIONS` — CORS-заголовки.
- Логи: `tools/logaudit` не должен найти URL с ключом и координатами (журналов нет).
- Клиенты: `patch_test.py` (повреждение каждого места даёт `PatchError`), сборки в CI. Ни одна проверка не говорит, что карта рисуется — это проверки владельца.
- Проверить, что хотя бы один тест падает без изменения (например, ключи app config).

## Проверки перед сдачей (CachyOS)
- `go build ./...`; `go vet -tags integration,e2e -unreachable=false` затронутых пакетов (`app/bff/configuration/...`, `test/e2e/...`, `tools/...`); `go test` их и зависящих, `-race` — в общем прогоне партии.
- e2e раздела «Тесты» со своими `E2E_PROJECT`/`E2E_DIR`, затем `test/e2e/run.sh down`; `caddy validate`; `make lint`; `go test ./tools/...`; `go run ./tools/apicoverage` (обработчики не меняются — матрица не должна измениться).
- Клиенты — CI их репозиториев; на машине разработки ничего не собирать.

## Проверки владельца
- [ ] Android (сборка с патчем): «Скрепка → Геопозиция» — карта видна, перетаскивается, булавка в центре, адрес под ней меняется; «Отправить выбранную геопозицию» — превью в чате совпадает с точкой.
- [ ] Android: тёмная тема — тёмная карта; переключить тему при открытом экране.
- [ ] Android: кнопка «моё местоположение» ведёт к себе; без разрешения на геопозицию экран не падает.
- [ ] Android: трансляция геопозиции — экран трансляции с кругом/маркерами собеседника; геопозиция группы; адрес бизнеса.
- [ ] Android: пунктов «Спутник» и «Гибрид» нет; в углу карты есть «© OpenStreetMap».
- [ ] Android: размер APK до/после (из CI).
- [ ] Desktop Windows, macOS, Linux: «Геопозиция» в меню вложений есть; карта грузится, ночная тема — тёмная, отправка точки; на macOS под точкой адрес, на Windows/Linux — координаты.
- [ ] `curl https://<PLUMA_MAPS_DOMAIN>/<неверный ключ>/styles/ru/style.json` → 404; с верным — 200.
- [ ] iPhone и Web — без изменений (выбор места на iPhone, превью на Web).

## Эксплуатация
- DNS: A-запись `<PLUMA_MAPS_DOMAIN>` (предложение — `maps.pluma.chat`) на адрес VPS, как у `PLUMA_CDN_ORIGIN_DOMAIN` (`docs/ops/home-server.md:78`); CAA зоны уже допускает только наш ACME-аккаунт.
- `.env`: `PLUMA_MAPS_DOMAIN`, `MAPS_PUBLIC_KEY` (сгенерировать на сервере, `openssl rand -base64 24 | tr '+/' '-_'`), `MAPS_PUBLIC_URL=https://<домен>/<ключ>`; затем `build maps` (новые стили), `up -d maps-tiles caddy bff` с оверлеем карт. Секретов нового класса нет.

## Решения
- Карта — MapLibre (решение владельца 2026-11-17). Тайлы — дом, свой tileserver-gl, отдельный контейнер без рендеринга; рисующий `maps` наружу не открывается.
- Ключ доступа в пути, приходит клиентам через app config (без правки клиентов при смене).
- Геокодер — устройства, как у Telegram; Desktop без `geo`.

## Решения к подтверждению
- Имя `maps.pluma.chat`.
- Ключ в пути (а не открытая раздача или подписанные короткоживущие URL): защищает канал дома от чужих, но не от вошедших пользователей; подписанные URL усложнили бы клиенты без выигрыша при закрытой аудитории.
- Спутник и гибрид убираются (снимков нет).
- Адрес на Windows/Linux — координаты (без своего геокодера).
- Свой геокодер — не делать сейчас.

## Не проверено
- Ограничение ключа Google Telegram как причина пустой карты (вывод по наблюдению владельца и манифесту).
- Параметр `--public_url` tileserver-gl v5.7.0 и форма ссылок в отдаваемом `style.json`.
- Как `lib_webview` перенаправляет запросы по `dataRequestRedirectHost`; нужен ли CORS для своей схемы на каждой ОС.
- minSdk и правила R8 внутри AAR MapLibre 13.7.1; числа причин движения камеры в MapLibre.
- WebGL2 в веб-вью Linux (WebKitGTK) и macOS (WKWebView) для MapLibre GL JS 6.x.
- Память `maps-tiles` и прирост APK — оценки до замера.

## Результат
—
