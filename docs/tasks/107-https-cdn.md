# 107. HTTPS-CDN: большие файлы через Gcore, ключи и хэши дома

- Статус: todo
- Решение: ADR 0071 (предложено; решения владельца 2026-11-16 внесены). Заменяет узел MTProto и `dc_options` задачи 102 (ADR 0065), оставляет её домашний `pkg/cdn`. Опирается на ADR 0057, 0061 (DNAT 443 домой, Caddy дома, исходящий трафик через VPS), 0041, 0009.
- Зависит от: 102 (домашний `pkg/cdn` в `batch/20261115`), 096 (дом и VPS), 057 (правила диапазона `upload.getFile`).
- Ветки: часть 0 — в `batch/20261115` (до мержа партии); остальное — `task/107-https-cdn` от `master` после партии; клиенты — `task/107-https-cdn` в pluma-android, pluma-desktop, pluma-ios (от `task/102-ios-cdn`, `40a25ee`), pluma-web.

## Цель
Большие файлы (≥ 1 МиБ, документы и фото) во всех чатах, кроме секретных, патченные Android, Desktop, iOS и Web качают по HTTPS с Gcore CDN. Gcore тянет шифр из дома (точка источника за Caddy, закрытая секретным заголовком); дом шифрует на лету и ничего не заливает. Ключ, IV и хэши клиент получает только по MTProto из дома, расшифровывает и сверяет сам. При любом сбое файл докачивается из дома, как сегодня. Старые сборки, официальные клиенты и сессии без признака не замечают ничего. Россия — позже (ADR 0071, «Что не делается»); код оставляет шов под вторую выдачу.

## Область
В задаче:
- Часть 0: снять с партии `batch/20261115` узел MTProto, `dc_options` и всё, что нужно только им.
- Сервер: выдача Gcore (подпись Secure Token и запасная `path-hmac`), точка источника с шифрованием на лету, подготовка хэшей (переделанный заливщик 102), маршрут из одной записи, признак `pluma_cdn`, сигнал отката, здоровье, бюджет, новая миграция, метрики и алерты.
- Публикация источника: блок Caddy, сеть compose.
- e2e на стенде с фальшивым CDN.
- Патчи четырёх клиентов.
- Документы: `docs/ops/cdn.md` (переписать), `security-setup.md`, `monitoring.md`, `home-server.md`, `.env.example`, реестры `clients/patches/*.md`, `TESTING-BACKLOG`, `pluma-admin cdn canary-url`.

Не в задаче (ADR 0071, «Что не делается»): Россия и СНГ (`getIpCountry`, маршрут по стране, Yandex); `upload.getFileHashes`; purge при удалении сообщения; HTTPS-CDN через SOCKS5/HTTP на Android и iOS и через MTProxy; Cloudflare (только если Gcore не пройдёт «Проверку владельца» 1 или аккаунт не пройдёт KYC/AUP; отказ проверки 2 — это `path-hmac`, проверки 0 — свой домен на Gcore).

## Контракт клиентов
Закреплённые исходники — `~/pluma-work/reference-src-new` (Android 13.2.1, iOS release-13.2, tdesktop 7.4.1, Web K). Номера строк — апстрим. **Контракт не изменился** по сравнению с прежней редакцией брифа: клиенту всё равно, какой провайдер за URL.

**Что есть сегодня (на что опираемся):**
- Флаг `cdn_supported`: Android — всегда, кроме веб-файлов (`FileLoadOperation.java:2465-2470`; ставится в `:2469`), tdesktop — каждый облачный файл (`storage/download_manager_mtproto.cpp:580-582`), iOS — `FetchV2.swift:974` (патч I-131 ставит его без движка Rust), Web K — никогда (`src/lib/appManagers/apiFileManager.ts:483-487`). Поэтому признак HTTPS-CDN — не флаг, а ключ `params`.
- Непатченный Android на редирект к DC вне списка ждёт этот DC бесконечно (`jni/tgnet/ConnectionsManager.cpp:2585-2590, :2849-2856, :3166-3168`). Значит редирект — только сессиям с признаком.
- `params` в `initConnection`: Android `jni/tgnet/ConnectionsManager.cpp:3225-3278` (рядом `tz_offset`, `:3266`), tdesktop `mtproto/session_private.cpp:549-570` (`prepareInitParams`), iOS `submodules/BuildConfig/Sources/BuildConfig.m:157-158`, tweb не шлёт (`src/lib/mtproto/networker.ts:429-440`). Сервер хранит их на ключ авторизации: `app/interface/session/internal/sess/main_auth_wrapper.go:416, 472-473`, `app/service/authsession/internal/dao/auths.go:140`; чтение в bff — как `tz_offset` (`app/bff/configuration/internal/dao/suggestions.go:49-57`).
- Редирект: Android `FileLoadOperation.java:2592-2623` (ключ 32 / IV 16, `:2602-2611`), tdesktop `switchToCDN`/`changeCDNParams` `download_manager_mtproto.cpp:982-1047` (ключ/IV `:1012-1020`), iOS `FetchV2.swift:999-1011`. `dc_id` ≠ 0: tdesktop считает `_cdnDcId != 0` режимом CDN (`:517-519`); < 1000 (`mtproto/mtp_instance.h:45`).
- Запрос части CDN: Android `:2449-2455`, `:2525-2526` (части 32/128/512 КБ, `:166-167`, `:303`), tdesktop `sendRequest` `:513-528` (128 КБ, `download_manager_mtproto.h:26`), iOS `FetchV2.swift:903-956` (128–256 КБ, выравнивание 128 КБ, `:1074-1090`), tweb `requestFilePart` `apiFileManager.ts:459-503` (64 КБ…1 МБ, `:514-531`).
- Расшифровка и хэш: Android `:1900-1906`, хэш после записи `:2002-2054`, несовпадение — `onFail` без отката `:2048`; tdesktop `cdnPartLoaded` `:674-737`, хэш до записи, несовпадение — `cancelOnFail` `:710-714`; iOS `:925-939`, проверка до коммита `:595-640`, откат после 4 несовпадений `:635-650`; tweb — WebCrypto, AES-CTR уже есть (`src/lib/crypto/aesCtrUtils.ts:17-21`).
- Откат на мастер: Android `FILE_TOKEN_INVALID` `:2583-2589`; tdesktop `cdnPartFailed` `:958-980`; iOS `fallBackToMaster` `:1312-1330`, обновление токена ≤ 3 (`maxCdnTokenRefreshes`, `:118`).
- Стриминг идёт тем же путём: Android `FileLoadOperationStream` через ту же операцию (`:902-923`), tdesktop `media/streaming/media_streaming_loader_mtproto.h:15` наследует задачу, iOS — интервалы MediaBox, tweb — Service Worker `src/lib/serviceWorker/stream.ts:95-121` → `requestFilePart`.
- HTTP-стек: Android — `HttpURLConnection` (OkHttp нет; `ImageLoader.java:414, 576`; minSdk 23, `TMessagesProj/build.gradle:143`); tdesktop — QNAM в своём потоке (`storage/file_download_web.cpp:136-169`); iOS — `URLSession`; tweb — `fetch` в mtproto-воркере.
- Прокси: Android `SharedConfig.isProxyEnabled()` (`SharedConfig.java:1535-1537`), типы `utils/proxy/ProxySettings.java:27-39`; tdesktop `core/sandbox.cpp:714-731` (Socks5/Http → application proxy, MTProto → `NoProxy`), типы `mtproto/mtproto_proxy_data.h:20-26`; iOS `Network.swift:1181-1190` (`updateProxySettings`), `:927, 1184` (WEB-прокси).

**Что требуется от патча (общий контракт, ADR 0071 §8):**
1. `params`: ключ `pluma_cdn`, jsonNumber `1`.
2. `cdn_supported` — только если: нет перерыва (п. 8), CDN для этой загрузки не выключен, прокси не запрещает (п. 9).
3. Редирект с `file_token`, начинающимся с `PLH1`: разбор `"PLH1"` ‖ id[32] ‖ exp u32 BE ‖ url; url обязан начинаться с `https://` (dev-сборки: ещё `http://` к адресам стенда, п. 11). Ошибка разбора — эта загрузка из дома без `cdn_supported`. Без `PLH1` — поведение апстрима (сервер такого не шлёт). К `dc_id` не подключаться, `help.getCdnConfig` не звать.
4. Запрос `GET url`, `Range: bytes=a-b`, без кэша, без редиректов, `Accept-Encoding: identity`. Ответ — только 206 с `Content-Range: bytes a-b/total` (на конце файла `b` может быть меньше), `total` равен известному клиенту размеру файла, тело длиной `b−a+1`. Таймауты: соединение 5 с, тишина чтения 10 с, весь запрос — 30 с. Один повтор того же диапазона через 300–800 мс при сетевой ошибке, таймауте, 5xx.
5. Блоки 128 КБ, выровненные по 128 КБ; часть меньше 128 КБ (Android 32 КБ) берётся срезом из скачанного блока (LRU 4–8 блоков на загрузку). Расшифровка по IV-правилу (байты 12–15 IV = `offset/16` BE), SHA-256 каждого полного блока (последний — короче) против хэша дома **до** выдачи байтов в файл или плеер. Нет хэша — `upload.getCdnFileHashes(file_token, offset)` на DC 1.
6. 403 или 410 — обновить URL: `getFile` с `cdn_supported` (как `FILE_TOKEN_INVALID`), не больше 3 раз за загрузку. (Gcore отвечает 403 и на истёкший токен по одной странице и 410 — по другой; оба — сюда.)
7. Любая другая ошибка (404, прочие 4xx, TLS, неверный `Content-Range`, `total` или длина, общий таймаут запроса, несовпадение хэша, 4-й 403) — откат: эта загрузка до конца из дома, `getFile` без `cdn_supported`, уже проверенные части сохраняются. `upload.reuploadCdnFile` не вызывать.
8. Перерыв: 3 транспортных отказа подряд (сеть, таймаут, TLS, 5xx после повтора) по разным загрузкам — 10 минут без `cdn_supported` во всём приложении.
9. Прокси: MTProxy или WEB-прокси включён — HTTPS-CDN выключен. SOCKS5/HTTP: Desktop — HTTPS идёт через тот же прокси (application proxy); Android и iOS — выключен. Веб — без ограничений.
10. Никакого HTTP-кэша (Android `setUseCaches(false)`, Qt `AlwaysNetwork` без `QNetworkDiskCache`, iOS `urlCache = nil` + `.reloadIgnoringLocalCacheData`, tweb `cache: 'no-store'`, `credentials: 'omit'`, `redirect: 'error'`). URL, ключ, IV — только в памяти загрузки. В логах — хост, статус, смещение; никогда URL и токен. `User-Agent` нативных клиентов — постоянная строка `Pluma` без модели и версии ОС.
11. Dev-сборки (`ci/configure.sh dev`) дополнительно принимают `http://` только к адресу dev-сервера из конфигурации dev (и `10.0.2.2` эмулятора Android); `verify()` для цели `server` проверяет, что этого разрешения в сборке нет.
12. Трафик HTTPS-CDN попадает в статистику использования сети клиента там, где она есть (Android `:2667-2681`).

## Дизайн

### Формат редиректа
`upload.fileCdnRedirect{dc_id = выдача (201), file_token = PLH1…, encryption_key (32), encryption_iv (16, последние 4 байта нулевые), file_hashes = Answer(hashes, offset)}` — до 64 хэшей от `offset` вниз до 128 КБ (как в 102).
```
file_token = "PLH1" | id[32] | exp:uint32be | url (UTF-8, до конца)
url        = https://<префикс>.gcorecdn.com/o/<hex(id)>?md5=<токен>&expires=<exp>     (gcore-md5)
           | https://<префикс>.gcorecdn.com/t/<exp>/<sig>/o/<hex(id)>                 (path-hmac)
```
`pkg/cdn/token.go`: `EncodeRedirectToken(id, exp, url)`, `ParseRedirectToken(b) (id, exp, url, err)`; разбор строгий (метка, длина ≥ 40, URL непустой и валидный). Дом принимает в `getCdnFileHashes` только `PLH1`.

### Признак клиента
- `pkg/cdn/clientcaps.go`: `PlumaCdnVersion(params string) int` — `params` в том виде, в каком их хранит authsession (JSON строки `ClientSession.Params`, разбор как в `dao.ClientZone`, `app/bff/configuration/internal/dao/suggestions.go:64-84`); значение `pluma_cdn`, если это jsonNumber ≥ 1 (строка и прочее — 0).
- В bff (`app/bff/files/internal/svc`): LRU по `PermAuthKeyId` (10 000 записей, 1 мин) → версия признака; источник — `AuthsessionGetAuthStateData(...).Client.Params` (как `clientZone`, `app/bff/configuration/internal/core/help.getPromoData_handler.go:63-72`). Ошибка authsession — без редиректа.
- **Сессия:** в `app/interface/session/internal/sess/main_auth_wrapper.go` (ветка `if m.client.Params != params`, сейчас без записи — `// clientNeedUpdate = true`) ставить `clientNeedUpdate = true`, когда `cdn.PlumaCdnVersion` старых и новых `params` различается. Смена `tz_offset` или `perf_cat` записи не вызывает (как сейчас).

### Выдачи, маршрут, шов (`pkg/cdn/delivery.go`, `router.go`)
```go
// Delivery is one way to hand a client an HTTPS URL for a prepared file.
type Delivery struct {
    ID     int32   // dc_id in fileCdnRedirect, 201..999
    Name   string
    Signer Signer  // gcore-md5 | path-hmac (later: worker-hmac, yandex-md5, s3-presign)
    Source string  // "home-origin" today; "push-store" later (target_id >= 2)
    MonthlyBytes int64
}
```
- `Routes`: грамматика `<страны или *>:<id,id,…>;…`; сегодня допустима только запись `*` — запись со странами даёт отказ запуска с текстом «country routes need a country source» (определения страны в этой задаче нет). Неизвестная или выключенная выдача в маршруте — отказ запуска.
- `Router.Pick(authKeyID, fileRowID, now) (Delivery, ok)`: первая выдача маршрута, которая здорова, в бюджете, не избегается ключом; выбор держится на ключ 10 мин.
- Сигнал отката: запрос **без** `cdn_supported` от ключа с признаком, по файлу, который этому ключу перенаправлялся ≤ 5 мин назад на P → P в избегание ключа на 1 ч, `pluma_cdn_fallback_total{dc}`. Состояние — в памяти bff (одна реплика). Запрос под `invokeWithTakeout` (экспорт данных tdesktop шлёт `upload.getFile` с `flags 0`, `export/export_api_wrap.cpp:760-766`) откатом не считается. Остаются ложные откаты от путей, которые флаг не ставят и с патченной загрузкой того же ключа и файла могут совпасть по времени: iOS `multipartFetchV1` (`MultipartFetch.swift:352`; выбирается вместо `FetchV2` при выключенной «экспериментальной загрузке» или `ios_killswitch_disable_downloadv2`, `MultipartFetch.swift:1135`, `Network.swift:747-750`, то есть не в одном процессе с `FetchV2`), tdesktop — загрузка с неизвестным размером (патч D-104 ставит флаг только при известном). Цена ложного отката — час этого ключа без CDN и завышенный `CdnFallbackHigh`; точный признак отката от клиентов (флаг в запросе) — отдельное изменение контракта четырёх клиентов, не сделано.

### Конфигурация (`Cdn` в `teamgramd/etc{,2}/bff.yaml`, значения из `.env`)
```yaml
Cdn:
  Deliveries:                         # delivery without BaseURL is off
    - Id: 201
      Name: gcore
      Signer: ${CDN_GCORE_SIGNER:gcore-md5}      # path-hmac if Secure Token is unavailable
      BaseURL: ${CDN_GCORE_BASE_URL}              # https://<prefix>.gcorecdn.com
      Key: ${CDN_GCORE_TOKEN_KEY}                 # gcore-md5: 6..32 chars; path-hmac: 32 bytes base64
      Source: home-origin
      MonthlyGB: ${CDN_GCORE_MONTHLY_GB:800}
  Routes: ${CDN_ROUTES:*:201}
  Origin:
    Listen: ${CDN_ORIGIN_LISTEN:0.0.0.0:20480}  # Docker network only, published by Caddy
    AuthValue: ${CDN_ORIGIN_AUTH}                # 64 hex; header X-Pluma-Origin-Auth
    MaxConcurrent: 16
  UrlTTL: ${CDN_URL_TTL:6h}
  PrepareRateMbit: ${CDN_PREPARE_RATE_MBIT:800}
  MinFileSize: ${CDN_MIN_FILE_SIZE:1048576}
  Kinds: ${CDN_KINDS:document,photo}             # encrypted (secret chats) is rejected at start
  DevAllowHTTP: ${CDN_DEV_ALLOW_HTTP:0}           # e2e only: http:// BaseURL to the stand
  HealthEvery: ${CDN_HEALTH_EVERY:1m}            # e2e: 5s
  AvoidTTL: ${CDN_AVOID_TTL:1h}                  # e2e: shorter
```
- Отказ запуска: маршрут со странами; ссылка на неизвестную/выключенную выдачу; `BaseURL` не `https://`, если не задан явный `CDN_DEV_ALLOW_HTTP=1` (его ставит только `test/e2e/run.sh`; при нём bff пишет предупреждение при старте и метрика `pluma_cdn_dev_http{dc}` = 1, алерт на проде; релизные клиенты `http://` всё равно отвергают, контракт п. 3); ключ `gcore-md5` вне 6–32 символов или с пробелом; ключ `path-hmac` не 32 байта; `CDN_ORIGIN_AUTH` не 64 hex при включённой выдаче с `home-origin`; `UrlTTL` > 24 ч; `MinFileSize` не кратен 1 МиБ; `encrypted` в `Kinds`. Нет ни одной включённой выдачи — CDN выключен, обработчики работают как без CDN, точка источника не слушает.
- Учётные данные — только переменные `.env`, в лог не пишутся.

### Подписи (`pkg/cdn/signer*.go`)
- `gcore-md5`: `path = "/o/" + hex(id)`; `md5 = base64url(MD5(exp + path + " " + key))` без `=` (`+/` → `-_`); URL `BaseURL + path + "?md5=" + md5 + "&expires=" + exp`. По https://docs.gcore.com/cdn/cdn-resource-options/security/use-a-secure-token/configure-and-use-secure-token.md (прочитано 2026-11-16). Эталон теста — вывод команды со страницы `echo -n '<exp><path> <key>' | openssl md5 -binary | openssl base64 | tr +/ -_ | tr -d =` на значениях теста, записанный в `pkg/cdn/testdata/gcore-md5.json`; плюс свойства (смена пути, срока или ключа меняет подпись).
- `path-hmac` (запасной): `bucket = ceil((now + UrlTTL) / UrlTTL) · UrlTTL` (одинаков для всех клиентов в окне); `sig = base64url(HMAC-SHA256(key, "o/" + hex + "|" + bucket))[:22]` (132 бита); URL `BaseURL + "/t/" + bucket + "/" + sig + "/o/" + hex`. Проверяет источник (за постоянное время); `bucket < now` — 410; `bucket − now > 2·UrlTTL` или подпись неверна — 403; ответ с `Cache-Control: public, max-age = bucket − now`.
- `exp` в `PLH1` — фактический срок URL (для `path-hmac` — `bucket`).

### Источник (`pkg/cdn/origin.go`, слушатель в bff)
- Только `GET` и `HEAD` (прочее — 405; `HEAD` — те же заголовки без тела). Заголовок `X-Pluma-Origin-Auth` == `CDN_ORIGIN_AUTH` (`subtle.ConstantTimeCompare`), иначе 403 — до разбора пути, так что без заголовка существующий `id` не отличить от несуществующего. Строка запроса не учитывается (Gcore может передать `md5`/`expires` источнику — НЕ ПРОВЕРЕНО), сверка — по `URL.Path`, `RawPath` с `%`-кодированием — 404. Пути: `^/o/[0-9a-f]{64}$`; при `path-hmac` — `^/t/[0-9]{1,10}/[A-Za-z0-9_-]{22}/o/[0-9a-f]{64}$`; `^/c/v1$` (канарейка); прочее — 404.
- `/o/<hex>`: `ByToken(id)` → строка `cdn_files`; нет, не подготовлена (нет хэшей), вид вне `Kinds` — 404. Иначе — `Range` (один диапазон `bytes=a-b`, `bytes=a-`, `bytes=-n`) → 206 + `Content-Range`; без `Range` → 200; несколько диапазонов → 206 `multipart/byteranges`, как `http.ServeContent` (slice Gcore шлёт один диапазон, ветка нужна только для корректности); за концом → 416 с `Content-Range: bytes */size`.
- Байты: открытый текст диапазона из источника байтов `app/bff/files/internal/svc/cdn_files.go` (тот же, что у заливщика 102; если он читает только целыми частями dfs — читать охватывающие части и резать), шифрование `ctr.go` с IV-правилом 0065 §3 от смещения `a` (смещение не кратно 16 — сдвиг внутри блока потока ключей). Отдача потоком, без буфера на весь диапазон.
- Условные запросы по RFC 9110: `If-None-Match`/`If-Modified-Since` с совпадением — 304; `If-Range` с другим `ETag` — 200 целиком. Удобно через `http.ServeContent` поверх `io.ReadSeeker`, шифрующего на лету (он же даёт `Range`, 416, `HEAD` и `multipart/byteranges`). `Content-Type` ставится до вызова, чтобы `ServeContent` не нюхал байты.
- Заголовки: `Content-Type: application/octet-stream`, `Content-Length`, `Accept-Ranges: bytes`, `ETag: "<hex>"`, `Last-Modified` = `cdn_files.created_at`, `Cache-Control: public, max-age=2592000, immutable` (для `path-hmac` — как выше). Никакого сжатия.
- `/c/v1`: 262 144 байта `SHA-256(seed ‖ uint32be(i))` подряд, `seed = "pluma-cdn-canary-v1"`; те же правила `Range` и кэша.
- Пределы: семафор `MaxConcurrent` (сверх — 503), `WriteTimeout` 120 с, `ReadHeaderTimeout` 10 с. Ошибка чтения dfs посреди ответа — обрыв соединения (Gcore не закэширует обрезанный кусок), метрика.
- Счётчик отданных байт по выдаче → `cdn_delivery_usage.origin_bytes` (пакетная запись раз в минуту).
- Лог: метод, код, длина, время; без `id`, пути и заголовков.

### Подготовка (`pkg/cdn/preparer.go`, бывший `pusher.go`)
- Очередь `cdn_replicas` с арендой, откатами, `short_read`, одна работа за раз — как в 102. `Target` вместо `NodeAPI`:
```go
// Target is where a prepared file must be. home-origin only needs the hashes;
// a push store (later) also needs the ciphertext uploaded.
type Target interface {
    ID() int32                 // cdn_replicas.target_id; 1 = home-origin
    NeedsCiphertext() bool
    Put(ctx context.Context, name string, r io.Reader, size int64) error
}
```
- Цель `home-origin` (`target_id = 1`): чтение файла с пределом `PrepareRateMbit`, хэши 128 КБ открытого текста в `cdn_files.hashes`, без шифрования и записи; `ready` = хэши записаны. Ключ и IV создаются до чтения (как в 102).
- Сроки подготовки: чтение одной части 1 МБ из dfs — не дольше 1 мин (иначе `timeout`, повтор по откатам); вся подготовка — не дольше времени чтения файла на 1 МиБ/с (или на `PrepareRateMbit`, если он ниже) плюс 2 мин. Предел скорости только разносит чтения и срока не задаёт: dfs медленнее 800 Мбит/с не делает большие файлы вечным `timeout` (4 ГБ — около 70 мин вместо 163 с).
- Удаляются `control.go`, `prober.go`, `reqtoken.go` и их тесты.

### Здоровье и бюджет (`pkg/cdn/health.go`, `usage.go`)
- Раз в `HealthEvery` на выдачу: `GET SignURL("/c/v1")` с `Range: bytes=131072-135167`, таймаут 10 с → 206, `Content-Range: bytes 131072-135167/262144`, тело совпало с канарейкой. Второй запрос — тот же объект с испорченной подписью (последний символ `md5` или `sig` заменён) → обязан быть 403/410; 2xx — `token_off` (Secure Token выключен в портале или не проверяется для закэшированного объекта: URL перестали истекать). Успех обоих — `healthy_until = now + 3·HealthEvery` в `cdn_deliveries`; отказ — `unhealthy_reason` (`status`, `range`, `bytes`, `timeout`, `tls`, `token_off`). Исходящий запрос идёт обычным путём контейнера (через VPS, ADR 0061 §3).
- Бюджет: `origin_bytes` (точно, из источника) + `edge_bytes_est` (на редирект `+size`, если пара «ключ, файл» не встречалась за сутки; память bff, LRU) за текущий месяц UTC ≥ `MonthlyGB` → выдача вне маршрута до конца месяца, `result="budget"`. Месяц у Gcore сбрасывается 1-го числа (https://docs.gcore.com/cdn/how-the-cdn-service-and-its-additional-options-are-billed.md; часовой пояс НЕ ПРОВЕРЕН — берём UTC, запас 200 ГБ это покрывает).

### Таблицы (`teamgramd/deploy/sql/migrate-20261125-https-cdn.sql`, безопасно при повторе)
После `migrate-20261120-cdn.sql` задачи 102 (она остаётся в партии, таблицы на проде пустые).
```sql
CREATE TABLE IF NOT EXISTS cdn_deliveries (
  dc_id            INT NOT NULL,              -- delivery id in fileCdnRedirect (201...)
  healthy_until    BIGINT NOT NULL DEFAULT 0,
  unhealthy_reason VARCHAR(32) NOT NULL DEFAULT '',
  updated_at       BIGINT NOT NULL,
  PRIMARY KEY (dc_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS cdn_delivery_usage (
  dc_id          INT NOT NULL,
  month          CHAR(7) NOT NULL,            -- YYYY-MM, UTC
  origin_bytes   BIGINT NOT NULL DEFAULT 0,   -- served by the home origin, exact
  edge_bytes_est BIGINT NOT NULL DEFAULT 0,   -- home-side estimate of CDN egress
  PRIMARY KEY (dc_id, month)
) ENGINE=InnoDB;

-- cdn_replicas: dc_id -> target_id (1 = home-origin). Guarded like
-- migrate-20261031-read-dates.sql:11-19.
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'cdn_replicas' AND COLUMN_NAME = 'dc_id') = 1,
  'ALTER TABLE `cdn_replicas` CHANGE COLUMN `dc_id` `target_id` INT NOT NULL',
  'DO 0');
PREPARE ddl FROM @ddl; EXECUTE ddl; DEALLOCATE PREPARE ddl;

DROP TABLE IF EXISTS cdn_nodes;   -- node of task 102, never used in production
```
Индекс `k_queue (dc_id, …)` после переименования столбца MySQL переносит сам; тест миграции проверяет итоговую схему (`SHOW CREATE TABLE`).

### Дом: ход вызовов
`upload.getFile` (`app/bff/files/internal/core/upload.getFile_handler.go`, то же место, что в 102 — после проверок доступа, до dfs):
1. `cdn_supported`, не `precise`, CDN включён, вид файла в `Kinds` (`policy.Key`).
2. Признак `pluma_cdn ≥ 1` у ключа (LRU). Нет — без редиректа.
3. Состояние файла (кэш 60 с поверх MySQL, как в 102); меньше порога — без редиректа.
4. `Router.Pick`; файл подготовлен для источника выдачи (`target_id = 1` `ready`) — редирект: `exp`, `url = Signer.SignURL`, `PLH1`; запись для сигнала отката и бюджета; метрика.
5. Не подготовлен — ответ из dfs и `Enqueue(target 1)`.
6. Запрос без `cdn_supported` — проверка сигнала отката, затем обычный ответ.

`upload.getCdnFileHashes(file_token, offset)` → разбор `PLH1` → `ByToken(id)` → `Answer`; ошибка разбора, нет файла, нет хэшей — `FILE_TOKEN_INVALID`.
`upload.reuploadCdnFile` → всегда `FILE_TOKEN_INVALID` (предел 30/мин на пользователя из 102 — `FLOOD_WAIT_X`).
`help.getCdnConfig` → пустой `cdnConfig`. `upload.getCdnFile` на DC 1 → `FILE_TOKEN_INVALID`.

### Публикация источника
- `teamgramd/deploy/web/Caddyfile`: блок `{$PLUMA_CDN_ORIGIN_DOMAIN}` — `reverse_proxy` на порт 20480 сервиса bff (имя сервиса — как в compose дома); без `encode`; заголовок `X-Pluma-Origin-Auth` проходит как есть, Caddy его не логирует (журнал доступа Caddy для блока выключен); `-Server` в ответе. Сертификат — как у остальных имён (Let's Encrypt под аккаунтом дома, CAA `pluma.chat` уже разрешает его).
- compose: bff и Caddy в одной сети (как `gnetway`); порт 20480 на хост не публикуется.

### Пределы
| Что | Значение |
|---|---|
| Порог размера | `CDN_MIN_FILE_SIZE` = 1 МиБ |
| Хэшей в ответе | 64 (8 МБ) |
| Срок URL | `CDN_URL_TTL` = 6 ч (`path-hmac`: 6–12 ч) |
| Обновлений URL на загрузку (клиент) | 3 |
| Избегание выдачи ключом | 1 ч |
| Признак в bff | LRU 10 000, 1 мин |
| Выбор выдачи | на ключ, 10 мин |
| Подготовка | 800 Мбит/с чтения, одна за раз |
| Источник | 16 запросов одновременно, запись 120 с |
| Здоровье | раз в минуту, 4 КБ, `healthy_until` +3 мин |
| Бюджет Gcore | 800 ГБ в месяц (лимит 1 ТБ) |
| Reupload на пользователя | 30 в минуту |

### Ошибки
| Где | Условие | Ответ |
|---|---|---|
| `getCdnFileHashes` | не `PLH1`, неизвестный `id`, нет хэшей | 400 `FILE_TOKEN_INVALID` |
| `reuploadCdnFile` | всегда | 400 `FILE_TOKEN_INVALID` (420 `FLOOD_WAIT_X` сверх предела) |
| `getCdnFile` (дом) | всегда | 400 `FILE_TOKEN_INVALID` |
| решение о редиректе | ошибка MySQL, authsession, подписи | обычный ответ из dfs, ошибка в лог (без токенов) и метрику |
| источник | нет заголовка / неизвестный путь / не подготовлен или вне политики / метод / плохой диапазон / занято | 403 / 404 / 404 / 405 / 416 / 503 |
| источник, `path-hmac` | подпись или срок > 2·TTL / истёк | 403 / 410 |

### Поведение после перезапуска
- bff: аренды подготовки истекают и подхватываются; признаки, выбор, избегание, оценка отдачи за сутки — заново (потеря избегания безопасна); `healthy_until` и учёт месяца — в MySQL.
- Дом недоступен: Gcore отдаёт только закэшированное, промахи — 5xx, клиенты откатываются (и домой тоже не попадут — как сегодня без CDN). После возврата проба через ≤ 1 мин снова здорова.
- Gcore недоступен: проба отказывает, через ≤ 3 мин редиректов нет; клиенты с URL откатываются сами.

### Лог и приватность
- Не писать: `file_token`, URL с подписью, `id`, ключ подписи, `CDN_ORIGIN_AUTH`, ключ и IV файла, хэши, адреса клиентов вместе с файлами. `tools/logaudit`: новые имена (`signedurl`, `cdnurl`, `signkey`, `tokenkey`, `originauth`) рядом с правилами 102.
- Метрики — без id файлов и токенов.
- Что видит Gcore — ADR 0071 §11; то же — в `docs/ops/security-setup.md`.

### Метрики и алерты
- `pluma_cdn_redirects_total{dc,result=redirect|not_ready|unhealthy|small|no_caps|avoided|budget|error}`, `pluma_cdn_fallback_total{dc}`, `pluma_cdn_prepare_total{result}`, `pluma_cdn_prepare_bytes_total`, `pluma_cdn_queue{state}`, `pluma_cdn_origin_requests_total{code}`, `pluma_cdn_origin_bytes_total`, `pluma_cdn_origin_inflight`, `pluma_cdn_usage_bytes{dc,kind=origin|edge_est}`, `pluma_cdn_delivery_healthy{dc}`, `pluma_cdn_dev_http{dc}`.
- Алерты (`teamgramd/deploy/prometheus/server/rules/cdn.yml`, переписать): `CdnDeliveryUnhealthy` (10 мин), `CdnPrepareFailing`, `CdnFallbackHigh` (доля откатов > 20 % за 30 мин при ≥ 20 редиректах), `CdnBudget` (80 % `MonthlyGB`), `CdnOriginBusy` (503 источника > 1 % за 15 мин), `CdnDevHttp` (`pluma_cdn_dev_http` = 1 — на проде не должно быть).

## Порядок работ
Части — на одного исполнителя за заход. **0** — первой, в партии. Затем параллельно: **A1, C, F, G, H, I** (контракт клиентов и формат токена зафиксированы выше). После A1 параллельно **A2** и **B**. **D** — после A2, B, C. **E** — после A2, B, C. Клиентские части заканчиваются прогоном против стенда D.

**0. Снять узел MTProto с партии (малая–средняя; ветка `batch/20261115`, до её финального прогона).**
- Удалить: `app/service/cdn/` целиком; `teamgramd/deploy/cdn/{Dockerfile,cdn.yaml,control-certs.sh}`; сервис `cdn` в `docker-compose.edge.yaml` (`:62-101, :153`); образ `pluma-cdn` в `.github/workflows/images.yml`; цель `cdn*` и правила узла в `teamgramd/deploy/prometheus/server/{prometheus.yml,rules/cdn.yml}`; `docker-compose.cdn.yaml`; `pkg/mtclient/cdn.go`; `test/e2e/cdn_test.go` и CDN-часть `test/e2e/docker-compose.e2e.yaml`, `test/e2e/run.sh`.
- `dc_options` задачи 102: `app/bff/configuration/internal/config/dc_options.go`, вставка в `help.getConfig_handler.go` (не трогать правки 099 `webfile_dc_id`), `PLUMA_DC1_ADDRESSES` в конфигах и `.env.example`, абзац в `docs/ops/clients.md`, тест `TestConfigDcOptions`.
- `pkg/mtserver/codec/secret.go`, `withSecret`/`arbitraryLength` в padded-кодеке, `pkg/mtclient/obfuscated.go` — удалить, если `grep` не находит других потребителей кроме узла и его тестов; иначе оставить и записать, кто потребитель.
- Оставить: `pkg/mtserver` (вынос из gnetway), домашний `pkg/cdn` и `migrate-20261120-cdn.sql` — спящие (без `CDN_NODE_CONTROL` CDN выключен); правила `tools/logaudit`.
- `docs/ops/cdn.md` — заменить на короткую заметку «CDN не включается до задачи 107»; ADR 0065 в партии — та же строка статуса, что в `master`.
- Проверки: `go build ./...`, `go vet`, `go test` затронутых пакетов (`pkg/cdn`, `pkg/mtserver/...`, `app/interface/gnetway/...`, `app/bff/configuration/...`, `app/bff/files/...`, `pkg/mtclient`, `test/e2e` компиляция), `go run ./tools/apicoverage`.

**A1. Токен, признак, выдачи, подписи, конфиг, схема (средняя).** `pkg/cdn/{token,clientcaps,delivery,signer,signer_gcore,signer_pathhmac,config}.go` + тесты; `pkg/cdn/testdata/{gcore-md5,path-hmac}.json`; `migrate-20261125-https-cdn.sql`; `store.go`, `mysql.go`, `memstore.go` (`target_id`, `cdn_deliveries`, `cdn_delivery_usage`; удалить `Node`/`PutNode`/`RotateRtKey`); интеграционный тест миграции и хранилища (`pkg/testinfra`, MySQL).

**A2. Источник, подготовка, здоровье, бюджет (средняя; после A1).** `pkg/cdn/{origin,preparer,health,usage,metrics}.go` + тесты; удалить `control.go`, `prober.go`, `reqtoken.go` и тесты; `Runtime` на `Deliveries`/`Targets`; запуск слушателя источника в bff (`app/bff/files/internal/svc` или `app/bff/bff` — там, где `Runtime` поднимается в 102; останов вместе с bff).

**B. Обработчики, маршрут, признак (средняя; после A1).** `pkg/cdn/{router,redirect}.go`; `app/bff/files/internal/core/{upload.getFile,upload.getCdnFileHashes,upload.reuploadCdnFile,upload.getCdnFile,help.getCdnConfig}_handler.go` (где лежит `help.getCdnConfig` — как в 102); `app/bff/files/internal/{config,svc}` (LRU признака, `Cdn`); запись смены `pluma_cdn` в `app/interface/session/internal/sess/main_auth_wrapper.go` с тестом; `teamgramd/etc{,2}/bff.yaml`, `.env.example`. Общие файлы с другими задачами: `bff.yaml`, `.env.example`.

**C. Публикация источника (малая).** `teamgramd/deploy/web/Caddyfile` (блок `{$PLUMA_CDN_ORIGIN_DOMAIN}`), compose дома (сеть bff ↔ Caddy, переменная `PLUMA_CDN_ORIGIN_DOMAIN` у Caddy; пустая — блок не создаётся: проверить, как Caddy ведёт себя с пустым адресом блока, и при необходимости вынести блок в отдельный подключаемый файл, как `/srv/pluma-caddy-sites/*.caddy`); проверка `caddy validate` в контейнере на CachyOS; `test/e2e/fakecdn/` — фальшивый CDN для стенда (см. D), чтобы D не ждал.
- Фальшивый CDN (`test/e2e/fakecdn/main.go`, ~200 строк, свой образ стенда): проверяет `md5`/`expires` **своей** реализацией формулы Gcore (не `pkg/cdn`), 403 на неверный/истёкший; ключ кэша — путь без запроса; тянет у источника куски по 10 МБ с `X-Pluma-Origin-Auth`, кэширует их в памяти, отдаёт клиенту 206 на произвольный диапазон; управляющие ручки только для теста: `/_ctl/tamper` (портить байт в отдаваемых данных), `/_ctl/down` (отвечать 502), `/_ctl/badrange` (сдвинуть `Content-Range` на 16 байт), `/_ctl/full200` (отвечать 200 целиком на `Range`), `/_ctl/notoken` (не проверять токен), `/_ctl/flush`, `/_ctl/stats` (сколько тянул у источника). К источнику передаёт строку запроса клиента как есть (худший случай для источника), без `Accept-Encoding`. Ручки `/_ctl/*` слушают отдельный порт, недоступный клиентам стенда. Режим `path-hmac`: просто прокси с кэшем по полному пути и уважением `max-age`.

**D. e2e (средняя; после A2, B, C).** `test/e2e/docker-compose.e2e.yaml` и `run.sh`: фальшивый CDN `fakecdn` в стенде, `CDN_GCORE_BASE_URL=http://fakecdn:8080`, ключ и `CDN_ORIGIN_AUTH` прогона (генерирует `run.sh`), короткие интервалы (`CDN_URL_TTL=20s`, `CDN_HEALTH_EVERY=5s`, `CDN_AVOID_TTL=30s` — через переменные, не через код тестов); `pkg/mtclient`: `params` в `initConnection` (если ещё нет) и `cdnhttp.go` (разбор `PLH1`, `Range`-GET, расшифровка, сверка хэшей); `test/e2e/https_cdn_test.go`.

**E. Эксплуатация и документы (малая; после A2, B, C).** `docs/ops/cdn.md` (переписать: аккаунт Gcore, ресурс, переменные, проверка, откат), `docs/ops/security-setup.md` (что видит Gcore), `docs/ops/monitoring.md`, `docs/ops/home-server.md` (переменные `.env`, имя источника), `teamgramd/deploy/prometheus/server/rules/cdn.yml`, `tools/logaudit`, `tools/pluma-admin` (`cdn canary-url <dc>`: подписанный URL канарейки сроком 1 ч и строка `curl -r 131072-135167 -o /dev/null -w '%{http_code} %{size_download}'`), строки `TESTING-BACKLOG`.

**F. Android (средняя; pluma-android, `ci/patch.py`).** Новый `TMessagesProj/src/main/java/org/telegram/messenger/PlumaCdnHttps.java` (~200 строк: пул 4 потока, `HttpURLConnection`, `Range`, проверка 206, LRU блоков, SHA-256 до выдачи, разбор `PLH1`, перерыв, `isAllowed()` = нет перерыва и `!SharedConfig.isProxyEnabled()`); `FileLoadOperation.java`: поля `cdnHttpsUrl`, `cdnHttpsDisabled`, `cdnHttpsRefreshes`, `httpsCall` в `RequestInfo` (`:113-125`); флаг `:2469`; разбор в редиректе `:2612-2620`; вместо `sendRequestSync` при HTTPS — фетчер (`:2449-2455`, `:2525`), лямбда ответа `:2526-2689` выносится в `RequestDelegate`; фетчер отдаёт расшифрованные проверенные байты как `TL_upload_file`, ветки `isCdn` `:1889`, `:2002` при HTTPS пропускаются; ошибки → `FILE_TOKEN_INVALID` (403/410) или новая ветка отката рядом с `:2583`; отмена `:911`, `:1466-1490`, `:2210-2225` (свой отрицательный счётчик `requestToken`); `discardConnection` `:1497`, `:2236` пропускать; статистика `:2667-2681`; `jni/tgnet/ConnectionsManager.cpp:3225-3278` — `pluma_cdn`. Запись в `clients/patches/android.md` (следующий свободный номер A-…).

**G. Desktop (средняя; pluma-desktop).** Новый `Telegram/SourceFiles/storage/download_cdn_https.{h,cpp}` (~220 строк: `CdnHttpsLoader`, QNAM в своём `QThread` по образцу `file_download_web.cpp:154-169`, отрицательные `mtpRequestId`, `setTransferTimeout(10000)`, `AlwaysNetwork`, колбэки через `crl::on_main`; разрешение — `!proxy.isEnabled() || selected().type ∈ {Socks5, Http}` и нет перерыва); `download_manager_mtproto.{h,cpp}`: поля, флаг `:580-582`, разбор в `switchToCDN` `:982-992` (без MTProto-сессии к `dc_id`), `sendRequest` `:519-528`, вынос `cdnBytesReceived` из `:674-722`, `fallBackToMaster` вместо `cancelOnFail` при HTTPS (`:710-714`, `:768-773`), `cdnHttpsFailed`, `cancelRequest` `:898-909` и перепосылка `:1033-1035` для отрицательных id; `mtproto/session_private.cpp:549-570` — `pluma_cdn`. Запись в `clients/patches/desktop.md`.

**H. iOS (средняя; pluma-ios, поверх I-131 из `task/102-ios-cdn`).** Новый `submodules/TelegramCore/Sources/Network/CdnHttpsFetch.swift` (~160 строк: эфемерный `URLSession`, `urlCache = nil`, `timeoutIntervalForRequest = 10`, `httpMaximumConnectionsPerHost = 4`, `waitsForConnectivity = false`, `Signal` с отменой, разбор `PLH1`, перерыв); `Network.swift:1181-1190` — публичный атомарный `isProxyActive`; `FetchV2.swift`: `CdnData.httpsUrl` (`:218-240`, `:1005-1011`), флаг `:974`, запрос части `:903-956` с вынесенной расшифровкой `:925-939`, часть 512 КБ / максимум 1 МБ при HTTPS (`:786-792`, `:1079-1084`), откат при первом несовпадении хэша при HTTPS (`:633-649`), 403/410 → `FILE_TOKEN_INVALID`, прочее → `fallBackToMaster` без редиректа; `BuildConfig.m:157-158` — `pluma_cdn`. I-131 в реестре переписать под общий патч, `ci/patch.py`, `patch_test.py`.

**I. Web K (средняя; pluma-web).** Новый `src/lib/appManagers/cdnHttps.ts` (~260 строк: состояние на файл, `CryptoKey` AES-CTR, хэши, LRU 8 расшифрованных блоков, `readRange`, `getCdnFileHashes` на мастер-DC, `fetch` с `AbortController` и связкой с `checkCancel`, счётчик в байтах 12–15 IV с полным 128-битным приращением `length: 128`, `subtle.digest`); `apiFileManager.ts`: тип ответа `:77`, `requestFilePart` `:480-492` (HTTPS при живом состоянии; иначе `getFile` с `cdn_supported`; отказ — состояние `disabled` и повтор части через `getFile` без флага, без ошибки наверх), очередь `:495-502` общая; `src/lib/mtproto/networker.ts:429-440` — `params` с `pluma_cdn` (проверить, что сериализатор сам выставляет бит флага). Проверить CSP веб-сервера Pluma (`connect-src` должен разрешать `https://<префикс>.gcorecdn.com`). Запись в `clients/patches/web.md`.

## Тесты
Модульные (сервер):
- `token`: круговой тест; обрезанный, без метки, пустой URL, не-UTF-8 — ошибка.
- `clientcaps`: jsonNumber 1 → 1; строка `"1"`, 0, отсутствие, вложенный объект, пустая строка, `null`, битый JSON → 0.
- Сессия: смена одного `pluma_cdn` в `initConnection` того же ключа вызывает `AuthsessionSetInitConnection`; смена одного `tz_offset` — нет (тест на `MainAuthWrapper` с фальшивым клиентом authsession; без правки падает).
- Подписи: `gcore-md5` по эталону OpenSSL и свойствам; `path-hmac` — круговая проверка подписи источником, граница окна (два вызова в одном окне дают один URL, в соседних — разные), 403/410.
- `config`: каждое правило отказа запуска (включая маршрут со странами, `encrypted` в `Kinds` и `http://` без `CDN_DEV_ALLOW_HTTP`); выключенная выдача; CDN выключен целиком.
- `origin`: без заголовка, с неверным — 403 (сравнение за постоянное время — через `subtle`); 405; неизвестный, неподготовленный, секретный чат — 404; `Range` `a-b`, `a-`, `-n`, за концом (416), несколько диапазонов; **байты любого диапазона, в том числе со смещением не кратным 16, равны срезу полного шифра файла**; `ETag`, `Content-Length`, `Cache-Control` постоянны; семафор → 503; обрыв при ошибке чтения посреди ответа; канарейка детерминирована; `?md5=…&expires=…` и любая строка запроса не меняют ответ; `HEAD` — те же заголовки без тела; `If-None-Match` с `ETag` → 304, `If-Range` с чужим `ETag` → 200 целиком.
- `preparer`: хэши совпадают с `hashes.go` по всему файлу; короткое чтение → `failed` с откатом; предел скорости; цель `home-origin` не шифрует.
- `router`: нездоровая, избегаемая, вне бюджета выдача пропускается; сигнал отката → избегание ключа и истечение; выбор держится 10 мин.
- `redirect` (memstore): без признака — нет редиректа даже при `ready`; с признаком — `PLH1`, ключ 32, IV 16, хэши от смещения; `precise` и вид вне `Kinds` — нет; неподготовленный — обычный ответ и очередь; ошибка Store/authsession — обычный ответ.
- `health`: 200 вместо 206, другой `Content-Range`, другие байты, таймаут → нездоров с причиной; 2xx на испорченную подпись → `token_off`.
- `usage`: оценка отдачи считает пару «ключ, файл» раз в сутки; переход месяца; предел выводит выдачу.
- Обработчики: `getCdnFileHashes` с `PLH1` и без; `reuploadCdnFile` → `FILE_TOKEN_INVALID`, предел; `getCdnConfig` пустой.
- `tools/logaudit` ловит `signedURL`, `tokenKey`, `originAuth`.
- Тест, падающий без изменения: `TestRedirectRequiresPlumaCdnParam` на коде 102 (там редирект по одному `cdn_supported`) — проверить падение и записать в результат.

Интеграционные (`pkg/testinfra`, MySQL):
- миграция 102 + 107 дважды подряд; миграция 107 без данных 102; итоговая схема `SHOW CREATE TABLE`;
- `Claim` двух подготовщиков — одна подготовка; истёкшая аренда подхватывается (как в 102); учёт месяца в `cdn_delivery_usage`.

Сквозные (`pkg/mtclient`, стенд с `fakecdn`, сессии после `getState`; `test/e2e/https_cdn_test.go`):
- `TestHttpsCdnDownload`: A шлёт B документ 3 МБ + 5 байт; B с `pluma_cdn=1`: `getFile` с `cdn_supported` — сначала `upload.file`, затем (≤ 30 с) `fileCdnRedirect` с `dc_id` 201 и `PLH1`; `Range`-запросы по 128 КБ и 512 КБ по URL, расшифровка, хэши (часть — через `getCdnFileHashes`), итог побайтно равен отправленному.
- `TestHttpsCdnLargeSlices`: файл 25 МБ — запросы в трёх разных кусках по 10 МБ; `fakecdn/_ctl/stats` показывает, что источник отдал каждый кусок один раз при повторных запросах второй сессией.
- `TestHttpsCdnNeedsParam`: та же сессия без `pluma_cdn` — никогда не редирект (несколько попыток за 40 с).
- `TestHttpsCdnParamChange`: тот же ключ повторяет `initConnection` с той же `app_version`, но с `pluma_cdn=1` → после TTL LRU стенда редирект появляется; обратно без признака → редиректа нет.
- `TestHttpsCdnTampered`: `/_ctl/tamper` → `cdnhttp.go` находит несовпадение хэша первого блока; `getFile` без `cdn_supported` отдаёт исходные байты.
- `TestHttpsCdnUrlExpiry`: после `CDN_URL_TTL` стенда запрос по старому URL — 403; новый `getFile` даёт рабочий URL.
- `TestHttpsCdnFallbackSignal`: после редиректа — `getFile` без `cdn_supported`; следующий `getFile` с флагом этого ключа — `upload.file` до `CDN_AVOID_TTL`; другому пользователю — редирект.
- `TestHttpsCdnUnhealthy`: `/_ctl/down` → через ≤ 3 интервала здоровья редиректов нет; вернуть — есть. То же с `/_ctl/notoken` — причина `token_off`.
- `TestHttpsCdnBadResponses`: `/_ctl/badrange` и `/_ctl/full200` → `cdnhttp.go` отвергает ответ (не пишет ни байта), `getFile` без `cdn_supported` отдаёт исходные байты.
- `TestHttpsCdnOriginAuth`: запрос к источнику напрямую из стенда без заголовка — 403, с чужим — 403.
- `TestHttpsCdnSecretChatNotRedirected`: файл секретного чата 2 МБ не перенаправляется, источник отвечает 404 на его `id`.
- `TestHttpsCdnLegacyMethods`: `getCdnFile` на DC 1 и `reuploadCdnFile` → `FILE_TOKEN_INVALID`; `getCdnConfig` пустой; `help.getConfig` — пустой `dc_options`.
- В логах bff и Caddy стенда нет hex `id`, подписей URL, `CDN_ORIGIN_AUTH`, ключей (поиск по значениям из теста).

Клиенты (в репозитории каждого клиента):
- `python3 -I -m unittest discover -s ci -p '*_test.py'`: `test_patch` на закреплённом апстриме; `test_damage` — порча каждой якорной строки новой правки (точки флага, редиректа, запроса части, отката, отмены, `params`, прокси) роняет `patch.py` с именем файла; `verify()` цели `server` падает, если в сборке осталось разрешение `http://` dev.
- Проверка против стенда D (dev-сборка, `http://` к `fakecdn`): видео 5 МБ — в метриках стенда `redirect` по 201, файл открывается; `/_ctl/down` во время загрузки — докачивается из дома; `/_ctl/tamper` — откат после первого блока, без показа испорченных байтов; `/_ctl/badrange` и `/_ctl/full200` — откат, файл цел. Android — эмулятор, Desktop — CachyOS (изолированный Xwayland), iOS — симулятор, Web — браузер. Скриншоты в результат.

## Проверки перед сдачей (CachyOS)
- `go build ./...`; `go vet -tags integration,e2e -unreachable=false` затронутых пакетов (`pkg/cdn`, `app/bff/files/...`, `app/bff/configuration/...`, `app/interface/session/...`, `pkg/mtclient`, `test/e2e/...`, `tools/...`); `go test` их и импортёров, затем `-race`.
- Интеграционные `pkg/cdn` по одному пакету; e2e раздела «Тесты» и регресс медиа (`TestMedia*`, `TestDfsParts*`) со своими `E2E_PROJECT`/`E2E_DIR`, затем `test/e2e/run.sh down` и удаление образов прогона (в том числе `fakecdn`).
- `caddy validate` для нового Caddyfile (в контейнере Caddy той же версии).
- `make lint`, `go test ./tools/...`, `go run ./tools/apicoverage`.
- Клиенты — как в «Тесты». На машине разработки ничего не собирать.

## Проверки владельца
До выпуска клиентов (после «Эксплуатации»), с помощью `pluma-admin cdn canary-url 201`:
0. **HTTPS на домене ресурса** (сразу после создания ресурса, до остального): `curl -sS -o /dev/null -w '%{http_code} %{ssl_verify_result}\n' https://<префикс>.gcorecdn.com/` с компьютера и открыть тот же адрес в браузере телефона — сертификат принят без исключений (`ssl_verify_result` = 0; код ответа любой). Не принят — удалить ресурс и создать новый на своём нейтральном домене (не `pluma.chat`) с «Get free Let's Encrypt certificate» и CNAME (ADR 0071 §1); `CDN_GCORE_BASE_URL` — этот домен.
1. **206 на произвольный диапазон** (жёсткое требование; документация Gcore код не называет): `curl -r 131072-135167`, `-r 1000000-` и `-r 0-0` по URL канарейки → 206, `Content-Range` ровно запрошенный (`bytes 131072-135167/262144` и т. д.), длина совпадает. Повторить на настоящем файле ≥ 25 МБ (URL берётся из лога dev-сессии `pkg/mtclient` или через `pluma-admin`, если E добавит такую команду). Не 206 или другой диапазон — CDN не включать, сообщить: запасной дизайн Cloudflare (ADR 0071, «Последствия»).
2. **Secure Token на бесплатном плане:** опция «Access → Secure token» есть и включается; без `md5` — 403; с испорченным `md5` и с прошедшим `expires` — 403/410 **и для объекта, уже закэшированного** (сначала валидный URL дважды — второй ответ с HIT в Log viewer, затем тот же путь с плохой подписью). Опции нет или закэшированный объект отдаётся без токена (тогда и проба подписи даст `token_off`) — переключить на `path-hmac` (`CDN_GCORE_SIGNER=path-hmac`, Secure Token выключить, ключ — 32 байта base64) и повторить п. 1.
3. **Кэш общий для разных URL:** два разных подписанных URL к одному объекту — второй HIT (Log viewer, колонка статуса кэша); в метрике `pluma_cdn_origin_bytes_total` второй запрос не добавил байт.
4. **Источник закрыт:** `curl https://<PLUMA_CDN_ORIGIN_DOMAIN>/c/v1` и `…/o/<любые 64 hex>` без заголовка → 403 (одинаково).
4а. **Устаревший кэш не отдаётся:** в настройках ресурса Always online выключен; при `path-hmac` — запрос по URL, срок которого прошёл, → 403/410, а не 200.
5. С телефонов и компьютеров в Европе и через VPN из России: открыть канарейку 201 (записать сеть, город, результат). Без VPN из России — для сведения, не условие.
6. Веб-клиент: файл ≥ 1 МБ качается через CDN (в инструментах разработчика — запросы к `*.gcorecdn.com` со статусом 206, без ошибок CORS и без предварительного `OPTIONS`, либо `OPTIONS` успешен).

Строки для `docs/project/TESTING-BACKLOG.md`:
- [ ] Android (сборка с патчем): видео 50 МБ в группе — открыть; в Grafana `pluma_cdn_redirects_total{dc="201",result="redirect"}` растёт; второй просмотр (другой пользователь) — `pluma_cdn_origin_bytes_total` почти не растёт; перемотка работает.
- [ ] То же — Desktop (Windows или Linux) стримингом с перемоткой и сохранением; размер файла совпадает.
- [ ] То же — iOS: просмотр и сохранение.
- [ ] То же — Web: просмотр и скачивание.
- [ ] Пользователь в России через VPN: тот же файл через `dc="201"`; без VPN — открывается (из CDN или из дома после отката).
- [ ] Включить MTProxy на Android и iOS: медиа качаются (из дома), запросов к `*.gcorecdn.com` нет.
- [ ] SOCKS5 на Desktop: медиа качаются через CDN через прокси.
- [ ] Старая сборка (без патча) и официальный Telegram, если есть под рукой: медиа качаются как раньше.
- [ ] Отключить выдачу (убрать `CDN_GCORE_BASE_URL`, деплой): всё из дома; вернуть — снова 201.
- [ ] Файл в секретном чате 10 МБ — Android и iOS получают (из дома).
- [ ] Через месяц: в портале Gcore трафик за месяц ≈ `pluma_cdn_usage_bytes` (origin + edge_est) и далеко от 1 ТБ.

## Эксплуатация
### Что владелец делает сам (аккаунт — только владелец)
**Gcore:**
1. Зарегистрироваться на gcore.com (почта владельца, 2FA). Карта для CDN не нужна (https://gcore.com/cdn/free-content-delivery-network); добавлять ли — «Решения к подтверждению». Возможна проверка KYC (MSA §2.17).
2. **CDN → CDN resources → Create CDN resource:** домен — **Gcore domain**, префикс нейтральный, без слов Pluma (`<префикс>.gcorecdn.com`); **Origin → URL / IP** = значение `PLUMA_CDN_ORIGIN_DOMAIN`; Advanced → **Host header override** = то же имя. Ресурс оживает через ~15 мин.
3. В настройках ресурса (**OPTIONS**):
   - **Origin pull protocol: HTTPS** (порт 443). Проверка сертификата источника документирована только через API/SDK/Terraform, шагов в портале страница не даёт (ADR 0071, Контекст); включение (`proxy_ssl_enabled` + CA — корень цепочки сертификата источника, посмотреть `openssl s_client -connect <имя>:443 -showcerts` + обязательный клиентский сертификат, одноразовый самоподписанный) — по желанию, не условие запуска (§3). Если владелец решит включать: API-токен создаётся на время настройки и удаляется, дому не передаётся.
   - **HTTP headers → Request headers:** `X-Pluma-Origin-Auth` = значение, сгенерированное дома `openssl rand -hex 32` (то же — в `.env` как `CDN_ORIGIN_AUTH`).
   - **Access → Secure token:** включить, ключ — `openssl rand -hex 16` дома (32 символа), «Add a client's IP to the token» — **не** отмечать; ключ — в `.env` как `CDN_GCORE_TOKEN_KEY`.
   - **Cache → Query string:** Ignore query string, режим **Ignore all**.
   - **Cache → CDN caching:** Origin controlled, Default cache expiry — 1 день (источник сам шлёт `max-age`).
   - **Cache → Always online:** выключен (проверить: если включён — выключить; иначе на 403 источника Gcore отдаёт устаревший кэш).
   - **Content → Large files delivery optimization:** включить. Gzip, Brotli и Fetch compressed — выключены (с slice несовместимы).
   - **HTTP headers → Access-Control-Allow-Origin:** адрес веб-клиента Pluma, «Always add … regardless of response code» — включить. **Response headers (add):** `Access-Control-Expose-Headers: Content-Range, Content-Length`, тот же флажок.
   - **Enable Network Error Logging** — выключить.
   - Logs uploader, Raw Logs, Origin Shielding — не включать.
   - Сохранить; если slice включили после первых запросов — Purge all.
4. В `.env` дома: `CDN_GCORE_BASE_URL=https://<префикс>.gcorecdn.com`.

**DNS (Cloudflare, зона `pluma.chat`):** A-запись `PLUMA_CDN_ORIGIN_DOMAIN` (например, поддомен `pluma.chat`) → адрес Pluma 195.201.252.126, без проксирования. CAA уже разрешает выпуск домашнему аккаунту Let's Encrypt.

### Дом
- **Миграция** `migrate-20261125-https-cdn.sql` — сервис migrate, повтор безопасен.
- **`.env` (только имена):** `CDN_GCORE_BASE_URL`, `CDN_GCORE_TOKEN_KEY`, `CDN_GCORE_SIGNER` (по умолчанию `gcore-md5`), `CDN_GCORE_MONTHLY_GB` (по умолчанию 800), `CDN_ORIGIN_AUTH`, `PLUMA_CDN_ORIGIN_DOMAIN`, `CDN_ROUTES` (по умолчанию `*:201`), `CDN_URL_TTL`, `CDN_KINDS`. `CDN_NODE_CONTROL` и `PLUMA_DC1_ADDRESSES` задачи 102 — не задавать.
- **Сеть:** VPS — ничего нового (443 уже DNAT домой). Проверить из контейнера bff исходящий адрес: `curl -s https://ifconfig.me` → 195.201.252.126.
- **Порядок включения:** (1) партия без CDN (часть 0); (2) сервер 107 с пустыми `CDN_*` — редиректов нет, источник не слушает; (3) DNS, аккаунт, ресурс, проверка владельца 0, `.env`, деплой — в Grafana `pluma_cdn_delivery_healthy{dc="201"}` = 1; (4) «Проверки владельца» 1–6; (5) выпуск клиентов с патчем.
- **Откат:** убрать `CDN_GCORE_BASE_URL` и деплой — редиректов нет сразу, источник перестаёт слушать; клиенты с открытыми загрузками откатываются сами. Кэш Gcore очищается сам (36 ч).
- **Стоимость:** 0 € при трафике до 1 ТБ в месяц (отдача + тяга источника + пробы ≈ 0,2 ГБ).

## Решения
Приняты владельцем 2026-11-16: провайдер Gcore (бесплатный 1 ТБ); Россия — позже, шов оставлен; секретные чаты не идут через CDN; прокси — Desktop SOCKS5/HTTP через CDN, Android и iOS с любым прокси без CDN, MTProxy и WEB-прокси без CDN; `User-Agent: Pluma`; срок URL 6 ч, 404 — сразу домой, 10 минут без CDN после 3 отказов.

Приняты в дизайне (ADR 0071, обоснование там): источник — дом за Caddy с шифрованием на лету, не бакет; подпись — Secure Token Gcore, запасная — `path-hmac`; признак `pluma_cdn` (jsonNumber 1) в `params`; URL целиком в `file_token`; выдача 201; домен Gcore, а не свой; без purge и API-токена Gcore; предел 800 ГБ.

## Решения к подтверждению
- Карта в аккаунте Gcore: без карты (рекомендация; дом останавливает редиректы на 800 ГБ, что Gcore делает без карты сверх 1 ТБ — НЕ ПРОВЕРЕНО) или с картой (€0,01/ГБ сверх 1 ТБ; тогда поднять `CDN_GCORE_MONTHLY_GB`).
- Часть 0 в партии до мержа (рекомендация) против «выкатить партию с 102 как есть и снять в 107».

## Не проверено
- Gcore: **206 с точным `Content-Range` на произвольный `Range` клиента** при slice (страница пишет только «serves the requested range»); **действительный сертификат на `*.gcorecdn.com`** (проверка 0); передаются ли источнику отброшенные «Ignore query string» параметры; значение Always online по умолчанию; принимает ли Gcore без `proxy_ssl_enabled` любой сертификат источника (не условие, §3 ADR); **Secure Token на бесплатном/PAYG**; отказ токена для уже закэшированного объекта; код на истёкший токен (403 на одной странице, 410 на другой); доступность Origin Shielding на PAYG (страницы противоречат, не нужен); текст AUP (не прочитан: страница не отдала) и прохождение KYC из страны владельца; поведение без карты при превышении 1 ТБ («may stop» — вывод из совета на странице); часовой пояс сброса месяца; сколько Gcore хранит логи сверх 3 дней Log viewer; HTTP/3 на ресурсе (не нужен).
- Доступность `*.gcorecdn.com` из России без VPN и при «белых списках» (замер — только сайт gcore.com из дата-центров: 10/12).
- Предварительный запрос CORS для `Range` в браузерах (по стандарту Fetch `bytes=a-b` в безопасном списке) и ответ Gcore на `OPTIONS`; CSP веб-сервера Pluma.
- Поведение Caddy с пустым адресом блока (`PLUMA_CDN_ORIGIN_DOMAIN` не задан) — часть C.
- Выставляет ли сериализатор tweb бит флага `params` сам; поведение iOS `URLSession` при сворачивании во время загрузки; что `updateProxySettings` — единственная точка смены прокси на iOS; ATS симулятора для `http://` к стенду.
- Поведение tdesktop на редирект к DC вне списка (код не читался; не важно, пока редирект только патченным).

## Результат
_Заполняется по завершении._
