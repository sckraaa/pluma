# Карты в геопозициях

Решение — ADR 0063, задача 100. Android, Desktop и Web K показывают геопозицию, место, трансляцию, геоточку опроса и адрес бизнеса картинкой карты, которую просят у сервера (`upload.getWebFile` с `inputWebFileGeoPointLocation`). Картинку рисует свой сервис `maps` (tileserver-gl v5.7.0) дома по векторным данным Protomaps с диска хоста; bff приводит параметры, дорисовывает «© OpenStreetMap» в левом нижнем углу (на картах от 100×100) и держит готовую картинку в Redis 6 ч. iPhone рисует карту сам (MapKit) и сервер не спрашивает.

Координаты из сообщений не покидают дом: у контейнера `maps` нет опубликованного порта, он подключён только к внутренней сети `tiles_net` (`internal: true`, выхода наружу нет) вместе с bff, а все стили, шрифты и значки лежат в образе. Наружу ходит только `extract.sh` при вырезке данных — за номерами тайлов региона, без данных пользователей.

```
клиент ─upload.getWebFile─► bff ─Redis webmap:<ключ>?─► есть: части того же PNG
                              └─ нет ─► http://maps:8080/styles/<язык>/static/<lon>,<lat>,<zoom>/<w>x<h>@<s>x.png
                                          maps (tiles_net, без выхода) ◄── /opt/pluma-maps/pluma.pmtiles (только чтение)
```

Без `MAPS_URL` карты выключены: bff отвечает `LOCATION_INVALID`, клиенты показывают геопозицию без карты и не зависают.

Интерактивную карту выбора места Android и Desktop рисуют сами (MapLibre) по векторным тайлам того же файла, которые отдаёт второй контейнер `maps-tiles` через Caddy — раздел «Публичная раздача» (задача 112, ADR 0070).

## Что где

| Что | Где |
|---|---|
| Образ | `teamgramd/deploy/maps/Dockerfile` → `pluma-maps:local` (`PLUMA_MAPS_IMAGE`), собирается на сервере |
| Конфиг tileserver-gl | `teamgramd/deploy/maps/config.json`; стили собирает `build-styles.mjs` по `MAPS_STYLES` при сборке образа |
| Сервис | `docker-compose.maps.yaml`: `maps` и сеть `tiles_net` у bff; `maps-tiles` и сеть `maps_public_net` (публичная раздача) |
| Публичный конфиг tileserver-gl | `config-public.json` в образе (тоже собирает `build-styles.mjs`): светлый и тёмный стиль на язык, без рендеров |
| Сайт Caddy публичной раздачи | `teamgramd/deploy/web/maps-public.caddy`, подключается из `docker-compose.web.yaml` при `PLUMA_MAPS_DOMAIN` |
| Регион | `teamgramd/deploy/maps/region.geojson` (генератор `make-region.sh`, список стран в `make_region.py`) |
| Данные | `/opt/pluma-maps` (`MAPS_DATA_DIR`): `pluma-<дата>.pmtiles` и ссылка `pluma.pmtiles` на текущий |
| Вырезка и обновление | `teamgramd/deploy/maps/extract.sh` |
| Проверка | `teamgramd/deploy/maps/check.sh` |
| Код bff | `pkg/staticmap`, источник `app/bff/files/internal/core/geo_source.go`, блок `Maps` в `bff.yaml`; ключи app config — `app/bff/configuration/internal/appconfig` (`WithMaps`) |
| Алерты | группа `maps`, `teamgramd/deploy/prometheus/server/rules/maps.yml` |

## Команда compose

С наложением карт bff стоит в двух сетях и регистрирует в etcd свой адрес в `teamgram_net` явно: постоянный `172.20.255.10` (`PLUMA_BFF_IP`) и `POD_IP` с тем же значением. Без этого go-zero берёт адрес первого интерфейса контейнера, а Docker не держит `teamgram_net` первой: 2026-11-17 bff дважды зарегистрировал адрес в сети карт, сессия не могла до него дойти, и все запросы клиентов падали («Обновление…»). Проверка после выкладки: `docker exec etcd etcdctl get --prefix bff.bff` — адрес `172.20.255.10:20010`. Тест `TestComposeServicesRegisteredAddress` не пропустит сервис во второй сети без такого адреса.

Как только карты включены, **каждая** команда compose, которая создаёт или пересоздаёт bff, включает `-f docker-compose.maps.yaml`: оно добавляет bff сеть `tiles_net`. Без него `up -d` пересоздаст bff без этой сети, имя `maps` перестанет разрешаться, новые карты не нарисуются (ответ 500), через 15 минут — `MapsRendererDown`. Это касается и скрипта выкладки ядра: ему нужна ветка, как у `stt` — если в `.env` есть `MAPS_URL=http://maps:`, добавить `-f docker-compose.maps.yaml` и в команду сервисов (`C2`), и в команду дополнительных сервисов, а `maps` — в их список.

Дома команда сервисов с картами:

```bash
cd /opt/pluma
C="docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml -f docker-compose.maps.yaml"
```

`extract.sh` и `check.sh` берут команду из `MAPS_COMPOSE` (по умолчанию `docker compose -f docker-compose.yaml -f docker-compose.maps.yaml`); дома задать её с `docker-compose.public-ip.yaml`, как выше, чтобы перезапуск `maps` не пересоздавал остальное по другому набору файлов.

## Первый запуск

1. **Место и память.** Данные региона — около 103 ГБ (замер `--dry-run` 2026-11-16, сборка Protomaps 4.x, z0–15, 113 млн тайлов; вся планета — 139,5 ГБ). На время обновления нужно столько же ещё раз. Вырезка держит в памяти каталог тайлов — около 10 ГБ, `extract.sh` не начинает без 12 ГБ доступной памяти (`MAPS_EXTRACT_MEMORY_GB`). Дома (32 ГБ, `pluma.slice` до 10 ГБ, `media.slice` до 12 ГБ — это потолки, не резерв) проверить:
   ```bash
   df -h /opt          # свободно ≥ 115 ГБ; диск общий с медиасервером
   free -g             # available ≥ 12
   ```
   Если памяти не хватает (медиасервер занят), вырезать на другой машине с Linux x86_64: `MAPS_DATA_DIR=<каталог> extract.sh --no-switch`, перенести файл в `/opt/pluma-maps` и переключиться `extract.sh --use pluma-<дата>.pmtiles`.
2. **Каталог данных** (владелец root, файлы только на чтение для остальных):
   ```bash
   sudo install -d -m 0755 -o root -g root /opt/pluma-maps
   ```
3. **Пробный прогон** — размер без скачивания (около десяти минут, читает каталоги сборки):
   ```bash
   cd /opt/pluma && sudo MAPS_COMPOSE="$C" teamgramd/deploy/maps/extract.sh --dry-run
   ```
   Печатает сборку, объём скачивания, нужное место и свободное в `/opt/pluma-maps`. Сборка Protomaps хранится около недели; скрипт берёт самую новую со схемой тайлов 4.x.
4. **Образ** (несколько минут; скачивает базовые образы, пакет стилей из npm и архив шрифтов и значков, сверяя его SHA-256):
   ```bash
   $C build maps
   ```
   Языки подписей — `MAPS_STYLES` из `.env` (по умолчанию `ru,en`), по одному стилю на язык, тот же список читает bff.
5. **Вырезка, проверка и запуск** (часы; трафик около 100 ГБ входящего через туннель):
   ```bash
   sudo MAPS_COMPOSE="$C" teamgramd/deploy/maps/extract.sh
   ```
   Скрипт скачивает `pmtiles` v1.31.2 и сверяет SHA-256, пишет `pluma-<дата>.pmtiles.part`, проверяет (`pmtiles verify`, схема тайлов 4.x), выставляет 0644, переводит ссылку `pluma.pmtiles`, поднимает `maps` (`up -d`) и запускает `check.sh`. Два запуска одновременно не пойдут (блокировка в каталоге).
6. **Включить в bff:** в `.env` `MAPS_URL=http://maps:8080`, затем пересоздать bff с наложением карт:
   ```bash
   $C up -d bff
   docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}' pluma-bff-1   # есть pluma_tiles_net
   ```
   При ошибочном `MAPS_URL` (не http(s), с путём) или `MAPS_*` bff не стартует и пишет, какая переменная неверна.
7. **Проверка в клиентах** — строки задачи 100 в `docs/project/TESTING-BACKLOG.md`.

## Проверка сервиса (`check.sh`)

```bash
cd /opt/pluma && MAPS_COMPOSE="$C" teamgramd/deploy/maps/check.sh
```

Ждёт `/health` (до `MAPS_CHECK_TIMEOUT`, 180 с) и для каждого стиля образа рисует внутри контейнера:
- Красную площадь, z15, 277×195@2x — PNG ровно 554×390, больше одного цвета (карта);
- точку в Атлантике (30° с. ш. 30° з. д., вне региона), z15, 277×195 — PNG 277×195 одного цвета (фон стиля).

Печатает по строке `ok <стиль> …` с размером и временем; при ошибке — `FAIL …` и код 1. Запускать после выкладки, перезапуска и новых данных.

## Публичная раздача

Задача 112, ADR 0070. Android (MapLibre Native) и Desktop (MapLibre GL JS) рисуют выбор места сами; стили, шрифты, значки и векторные тайлы им отдаёт дом:

```
клиент ─https://<PLUMA_MAPS_DOMAIN>/<MAPS_PUBLIC_KEY>/styles/ru/style.json─► VPS (443, DNAT) ─► Caddy (web)
   Caddy: путь под ключом и из белого списка? ─ нет ─► 404
          └─ да: ключ срезан, X-Forwarded-Path=/<ключ> ─► maps-tiles:8080 (maps_public_net, без выхода)
                                                         ◄── /opt/pluma-maps/pluma.pmtiles (только чтение)
```

- **`maps-tiles`** — тот же образ `pluma-maps` с `config-public.json`: на каждый язык `MAPS_STYLES` светлый стиль `<язык>` и тёмный `<язык>-dark` (спрайты `dark`), шрифты, спрайты, TileJSON `data/protomaps.json` с подписью «© OpenStreetMap» и тайлы `data/protomaps/{z}/{x}/{y}.pbf`. Рендеров и статических картинок нет. Сеть — только `maps_public_net` (`internal: true`) вместе с Caddy: выхода наружу нет, порта на хосте нет. Лог — только неудачные запросы, без URL.
- **Ключа в `maps-tiles` нет.** Caddy срезает его из пути и передаёт как `X-Forwarded-Path`; tileserver-gl строит из него, `Host` и `X-Forwarded-Proto` ссылки внутри `style.json` и TileJSON, и только для имени `PLUMA_MAPS_DOMAIN` (`TILESERVER_GL_ALLOWED_HOSTS`).
- **Caddy** (`maps-public.caddy`) пропускает под ключом только `GET`/`HEAD` путей `styles/<id>/style.json`, `styles/<id>/sprite[@2x].{json,png}`, `fonts/<стек>/<a>-<b>.pbf`, `data/protomaps.json`, `data/protomaps/<z>/<x>/<y>.pbf` и `OPTIONS` (204); остальное — пустой 404, одинаковый и без ключа, и с чужим. CORS для любого источника (пикер Desktop — страница своей схемы в веб-вью). Тайлы, шрифты, спрайты кэшируются сутки, `style.json` и TileJSON — `no-cache`. Журнала нет.
- **Клиенты узнают адрес из app config:** bff дописывает `tdesktop_config_map` (`maps`, `bmaps`), `pluma_maps_url`, `pluma_maps_styles` из `MAPS_PUBLIC_URL` и `MAPS_STYLES` (`docs/ops/app-config.md`, «Выбор места на карте»). Без `MAPS_PUBLIC_URL` ключей нет: Desktop без пункта «Геопозиция», Android выбирает место без карты.
- **Что узнаёт дом:** IP устройства и номера тайлов видимой области, пока открыт выбор места. Наружу ничего не уходит, журналов нет.

### Включение

0. **Порядок:** `MAPS_PUBLIC_URL` задавать только когда выпущены Android и Desktop с патчами задачи 112. Desktop без патча, получив `tdesktop_config_map.maps`, покажет пункт «Геопозиция» с картой Mapbox, которая не загрузится (адрес он примет за токен Mapbox); Android без патча ключи Pluma просто не читает. Раздачу (`maps-tiles`, Caddy) можно поднять и проверить раньше — с пустым `MAPS_PUBLIC_URL`.
1. **DNS:** A-запись `PLUMA_MAPS_DOMAIN` (предложение — `maps.pluma.chat`) на адрес VPS, как у `PLUMA_CDN_ORIGIN_DOMAIN` (`docs/ops/home-server.md`); 443 уже идёт домой. CAA зоны допускает только наш ACME-аккаунт — ничего не менять.
2. **`.env`** (ключ генерировать на сервере и никуда не копировать):
   ```bash
   openssl rand -base64 24 | tr '+/' '-_'     # → MAPS_PUBLIC_KEY, 32 символа
   ```
   `PLUMA_MAPS_DOMAIN=maps.pluma.chat`, `MAPS_PUBLIC_KEY=<ключ>`, `MAPS_PUBLIC_URL=https://maps.pluma.chat/<ключ>` — без `/` на конце.
3. **Образ** с тёмными стилями и `config-public.json`: `$C build maps`, затем `$C up -d maps` (новые стили рисующему не нужны, но образ общий).
4. **Запуск** — с наложениями сайта и карт вместе (`web` входит в `maps_public_net`):
   ```bash
   W="$C -f docker-compose.web.yaml"
   $W up -d maps-tiles web bff
   MAPS_COMPOSE="$W" teamgramd/deploy/maps/check.sh
   ```
   Если `PLUMA_MAPS_DOMAIN` задан, а `MAPS_PUBLIC_KEY` пуст или не 16–128 символов base64url, `web` не стартует и пишет причину; при неверном `MAPS_PUBLIC_URL` не стартует bff.
5. **Проверка снаружи:** `curl -sI https://maps.pluma.chat/<неверный ключ>/styles/ru/style.json` — 404; с верным — 200. Клиенты — строки задачи 112 в `docs/project/TESTING-BACKLOG.md`.

Сеть `maps_public_net` объявлена одинаково в обоих наложениях, и `web` входит в неё по `docker-compose.web.yaml`: выкладка, пересоздающая `web` без наложения карт, связи с `maps-tiles` не рвёт. Но команда с `--remove-orphans` без `-f docker-compose.maps.yaml` удалит `maps-tiles` (и `maps`) как сироту — в скрипте выкладки наложение карт нужно в обеих командах, как сказано в «Команда compose».

### Смена ключа

Новый ключ в `MAPS_PUBLIC_KEY` и `MAPS_PUBLIC_URL`, затем `$W up -d web bff` (`maps-tiles` ключа не знает и не пересоздаётся). Клиенты получат новый адрес со следующим app config (Android — минуты, tdesktop — периодически); открытые экраны карты до переоткрытия перестают грузить новые тайлы.

### Проверка (`check.sh`)

Если `maps-tiles` запущен, `check.sh` после рисующего проверяет и его (имя и ключ читает из контейнера `web`, ключ не печатает):
- изнутри `maps-tiles`, как его спрашивает Caddy: `style.json` каждого стиля — ссылки только под `https://<домен>/<ключ>/`; TileJSON называет OpenStreetMap; статической картинки нет;
- через Caddy по имени сайта (из `maps_public_net`, с его сертификатом): `style.json`, тайл 0/0/0, шрифты 0–255, спрайт `@2x` — 200 с `Access-Control-Allow-Origin: *` и без `Server`; `OPTIONS` — 204; без ключа, с чужим ключом, статическая картинка и `/styles.json` — 404.

### Выключение

`MAPS_PUBLIC_URL=` и `PLUMA_MAPS_DOMAIN=` в `.env`, `$W up -d bff web`, `$W stop maps-tiles`. Клиенты со следующим app config теряют карту выбора места.

## Обновление данных

Вручную, раз в полгода (решение к подтверждению владельцем) или при смене региона: шаги 1, 3 и 5. Новая вырезка ложится рядом со старой, ссылка переключается после проверки, старый файл удаляется только когда `check.sh` прошёл на новом; при провале ссылка возвращается на старый файл. Кэш bff (6 ч) при этом не чистится: уже нарисованные картинки досматриваются по старым данным.

Смена региона: поправить список стран в `make_region.py`, `make-region.sh` (Docker, одноразовый контейнер), закоммитить `region.geojson` (не больше 500 КБ) и вырезать заново.

## Языки подписей

`MAPS_STYLES` читают и сборка образа (список стилей), и bff (какой стиль просить). Язык сессии выбирает стиль: есть такой — он; `be`, `kk`, `ky`, `tg`, `uz`, `hy`, `az` — `ru`; остальные — `en` (украинская сессия видит английские подписи, пока `uk` не добавлен). Каждый стиль — свои рендеры и память.

Добавить язык: дописать его в `MAPS_STYLES`, пересобрать образ (`$C build maps`), затем `$C up -d maps bff` — сначала образ, иначе bff попросит стиль, которого у сервиса нет (404, `webmap_render_total{result="error"}`). Пересчитать память (ниже) и `docs/ops/memory.md`.

## Память

Замер 2026-11-16 (два стиля, масштабы 1–4, до 1024×1024@2 и 512×512@4, 8 запросов одновременно): около 300 МиБ в простое, пик около 2 ГиБ, пузырь чата рисуется за 0,1–0,3 с, самые большие картинки — до 2 с, первая после простоя — около 0,7 с. Пулы рендереров — не больше одного на стиль и масштаб, в простое ни одного (`minRendererPoolSizes: [0]`, `maxRendererPoolSizes: [1]`): с пулами по два пик доходил до 3 ГиБ. Потолок контейнера — `MAPS_MEMORY` (по умолчанию 2560m, без подкачки) и `MAPS_CPUS` (2), внутри `pluma.slice` (`PLUMA_CGROUP_PARENT`). bff рисует одновременно не больше `MAPS_CONCURRENCY` (4) карт; декодирование и подпись в bff — около 1 МБ на пузырь чата и до 16–20 МБ на самую большую карту 2048×2048 (оценка по размеру картинки, не замер).

Больше стилей или пулов — больше памяти: поднять `MAPS_MEMORY` в `.env` (не правя compose-файл на сервере) и пересчитать `docs/ops/memory.md`.

## Переменные

Секретов нет: `MAPS_PUBLIC_KEY` получает каждый вошедший пользователь (ADR 0070, §4), но в репозиторий и в логи он не попадает.

| Переменная | Кто читает | По умолчанию | Что |
|---|---|---|---|
| `MAPS_URL` | bff | пусто — выключено | `http://maps:8080`; только схема и хост |
| `MAPS_STYLES` | bff и сборка образа | `ru,en` | стили по языкам, `[a-z-]{2,10}` через запятую, до 8 |
| `MAPS_TIMEOUT` | bff | `10s` | одна карта и, отдельно, ожидание свободного места рендера; 1 с – 1 мин |
| `MAPS_CONCURRENCY` | bff | `4` | карт одновременно на bff |
| `MAPS_CACHE_TTL` | bff | `6h` | срок картинки в Redis; 1 мин – 7 сут |
| `MAPS_PER_USER_PER_MINUTE` | bff | `60` | новых (мимо кэша) карт в минуту на пользователя, дальше `FLOOD_WAIT_X` |
| `MAPS_DATA_DIR` | compose, `extract.sh` | `/opt/pluma-maps` | каталог данных, в контейнере `/data` только на чтение |
| `MAPS_MEMORY`, `MAPS_CPUS` | compose | `2560m`, `2` | потолки контейнера `maps` |
| `PLUMA_MAPS_IMAGE` | compose | `pluma-maps:local` | образ сервиса |
| `MAPS_COMPOSE` | `extract.sh`, `check.sh` | `docker compose -f docker-compose.yaml -f docker-compose.maps.yaml` | команда compose |
| `MAPS_EXTRACT_MEMORY_GB` | `extract.sh` | `12` | сколько доступной памяти нужно для вырезки |
| `MAPS_CHECK_TIMEOUT` | `check.sh` | `180` | сколько секунд ждать готовности |
| `PLUMA_MAPS_DOMAIN` | `web` (Caddy), `maps-tiles` | пусто — нет сайта | имя публичной раздачи; A-запись на VPS |
| `MAPS_PUBLIC_KEY` | `web` (Caddy) | пусто | ключ в пути, 16–128 символов `A-Za-z0-9_-` (генерируется 32); обязателен при `PLUMA_MAPS_DOMAIN` |
| `MAPS_PUBLIC_URL` | bff | пусто — клиенты без карты | `https://<PLUMA_MAPS_DOMAIN>/<MAPS_PUBLIC_KEY>`, без `/` на конце; уходит клиентам в app config |
| `MAPS_TILES_MEMORY`, `MAPS_TILES_CPUS` | compose | `512m`, `1` | потолки контейнера `maps-tiles` |

## Метрики и алерты

Метрики bff (у `maps` своих нет, а Prometheus в `tiles_net` не входит):
- `webmap_render_total{result}` — нарисованные карты: `ok`; `timeout` — сервис или ожидание места рендера дольше `MAPS_TIMEOUT`; `error` — сервис недоступен или ответил ошибкой; `invalid` — ответ не PNG нужного размера;
- `webmap_render_seconds` — время одной карты;
- `webmap_cache_total{result}` — `hit`, `miss`, `error` (Redis не ответил: карта рисуется и не кэшируется);
- `webmap_flood_total` — отказы по пределу на пользователя; `webmap_limit_errors_total` — проверки предела, на которых Redis не ответил (карта рисуется без предела).

| Алерт | Что значит |
|---|---|
| `MapsRendererDown` | 15 минут ни одной нарисованной карты при запросах: `maps` остановлен, не стартует или bff пересоздан без `docker-compose.maps.yaml` |
| `MapsRenderFailing` | больше 20 % карт за 10 минут (от 10 запросов) не рисуются: перегрузка, нехватка памяти, стиль не из образа |

## Неполадки

Лог bff пишет только класс ошибки и `user_id` (`staticmap: render for user …`), без координат; лог `maps` — только неудачные запросы и без URL (метод, код, размер, время).

| Признак | Что делать |
|---|---|
| `maps` перезапускается, в логе нет файла `pluma.pmtiles` | нет данных или битая ссылка: `ls -l /opt/pluma-maps`; переключиться на имеющийся файл `extract.sh --use pluma-<дата>.pmtiles` или вырезать заново. Без файла tileserver-gl не стартует, bff отвечает 500 |
| `docker inspect -f '{{.State.OOMKilled}}' pluma-maps-1` — `true`, `journalctl -k` — `Memory cgroup out of memory` | не хватает `MAPS_MEMORY`: поднять (например, `3g`), `$C up -d maps`, пересчитать `docs/ops/memory.md`; либо меньше стилей |
| `MapsRendererDown`, `maps` здоров | bff не в `tiles_net`: `$C up -d bff` с наложением карт (раздел «Команда compose») |
| `result="error"` растёт после смены `MAPS_STYLES` | образ собран со старым списком: `$C build maps && $C up -d maps` |
| `result="timeout"` растёт | `docker stats pluma-maps-1`: упирается в `MAPS_CPUS` — поднять. Рендеров у tileserver-gl по одному на стиль и масштаб, поэтому `MAPS_CONCURRENCY` выше 4 без больших пулов (и памяти) не помогает |
| карта серая, без дорог | точка вне региона (Египет, ОАЭ, Азия) — так и задумано; внутри региона — проверить `check.sh` |
| Redis недоступен | карты рисуются без кэша (`webmap_cache_total{result="error"}`), предел не действует; чинить Redis |
| выбор места на Android или Desktop без карты, `curl` с ключом — 502 | `maps-tiles` не запущен (или удалён как сирота выкладкой без наложения карт): `$W up -d maps-tiles`, затем `check.sh` |
| `web` перезапускается, в логе `PLUMA_MAPS_DOMAIN needs MAPS_PUBLIC_KEY` | ключ пуст или не того вида: исправить `.env`, `$W up -d web` |
| bff не стартует, в логе `MAPS_PUBLIC_URL must be https://…` | адрес не `https://<домен>/<ключ>` (часто — `/` на конце): исправить `.env`, `$C up -d bff` |
| в `style.json` ссылки относительные или на чужое имя | `PLUMA_MAPS_DOMAIN` у `maps-tiles` не тот, что у Caddy: `$W up -d maps-tiles` |

Выключить карты: `MAPS_URL=` в `.env`, `$C up -d bff`, затем `$C stop maps`. Клиенты показывают геопозиции без карты.

## Подпись и лицензии

- **Данные** — © участники OpenStreetMap, ODbL, через сборки Protomaps («OpenStreetMap attribution required», https://docs.protomaps.com/basemaps/downloads). Подпись «© OpenStreetMap» рисует bff в левом нижнем углу (в правом клиенты рисуют время сообщения) на картах от 100×100; миниатюры меньше — без подписи, это разрешает https://osmfoundation.org/wiki/Licence/Attribution_Guidelines. Подпись не переводится: имя собственное и юридическая отметка. На интерактивной карте подпись — `attribution` TileJSON публичной раздачи, её показывает кнопка атрибуции MapLibre (Android и Desktop её не выключают).
- **tileserver-gl** v5.7.0 — BSD 2-Clause, © 2023 MapTiler.com, © 2016 Klokan Technologies GmbH (`LICENSE.md` репозитория maptiler/tileserver-gl на теге v5.7.0; в образе файла нет, `package.json` образа — `BSD-2-Clause`).
- **Стили** `@protomaps/basemaps` 5.7.2 — код BSD 3-Clause (Protomaps LLC, Kelso Cartography), оформление CC0, значки — производные значков Mapzen, MIT; текст — `teamgramd/deploy/maps/licenses/protomaps-basemaps.LICENSE`, в образе — `/licenses/`.
- **Шрифты** Noto Sans — SIL OFL 1.1 (`/licenses/noto-sans.OFL.txt` в образе), из protomaps/basemaps-assets.
- **Граница региона** — Natural Earth, общественное достояние. **`pmtiles`** (go-pmtiles) — BSD 3-Clause, только для вырезки на хосте.
- Шрифт подписи в bff — Go fonts (`golang.org/x/image/font/gofont`), BSD.
