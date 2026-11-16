# CDN больших файлов

Большие файлы (видео, документы, фото и файлы секретных чатов от 1 МиБ) Android, Desktop и iOS со сборкой I-116 качают с узла `cdn` на VPS, а не с домашнего канала. Решение — ADR 0065, задача 102. Дом отдаёт узлу одну зашифрованную копию файла; узел хранит и раздаёт только шифр, ключ файла клиент получает от дома. Узел недоступен, переполнен или исчерпал трафик месяца — всё идёт из дома, как без CDN. Web K CDN не умеет и качает из дома всегда.

```
клиент ── upload.getFile (cdn_supported) ──► дом: редирект {DC 201, токен, ключ, IV, хэши}, только если копия на узле готова
клиент ── upload.getCdnFile ──► 195.201.252.126:8443 (узел cdn на VPS, obfuscated2 с секретом dd…) ── шифр
дом (bff) ── Put/Status/Inventory, gRPC mTLS ──► 10.77.0.1:20510 (туннель) ── узел
Prometheus дома ──► 10.77.0.1:20511 (туннель) ── метрики узла
```

## Как устроено

- **Дом** (`pkg/cdn` в процессе bff, сервис files): решает о редиректе, хранит ключи, токены и хэши файлов в MySQL (`cdn_files`, `cdn_replicas`, `cdn_nodes`; миграция `migrate-20261120-cdn.sql`), заливает копии на узел и проверяет узел. Без `CDN_NODE_CONTROL` CDN выключен целиком: ни редиректов, ни заливки, ни метрик `pluma_cdn_*` у bff.
- **Редирект** — только на **полную** копию на здоровом узле. Первый зритель большого файла качает его из дома, а файл тем временем ставится в очередь заливки; следующие части и все остальные зрители — с узла. Состояние файла bff держит в памяти до 60 с, поэтому редирект появляется не сразу после готовности копии.
- **Заливка:** по одному файлу (`CDN_PUSH_CONCURRENCY`), не быстрее `CDN_PUSH_RATE_MBIT` (20 Мбит/с из примерно 57 Мбит/с отдачи дома). Копия готова, только если узел вернул тот же размер и SHA-256 шифра. Неудачи повторяются через 1, 5, 30 и 120 минут, после 8 попыток копия `failed` (тревога `CdnPushFailing`). Файл больше кэша узла не заливается совсем (`too_large`).
- **Проверка узла** раз в 10 с: `Status` по каналу управления (таймаут 3 с) и рукопожатие до `resPQ` на публичном адресе `CDN_DC_ADDRESS` с секретом — отпечаток ключа должен совпасть с тем, что узел сказал в `Status`. Здоровый узел получает `healthy_until = now + 30 с`; после отказа редиректы прекращаются не позже чем через 30 с. Причина отказа — `cdn_nodes.unhealthy_reason` (таблица ниже). Раз в 10 минут — сверка (`Inventory`): копии, которых на узле нет, становятся `absent` и заливаются снова при следующем запросе; файлы на узле без готовой копии дома удаляются.
- **Узел** (`app/service/cdn`, образ `pluma-cdn`): сервер MTProto, который принимает только `invokeWithLayer`, `initConnection` и `upload.getCdnFile`, на всё прочее — `CDN_METHOD_INVALID`. Свой RSA-ключ (создаётся при первом запуске в томе узла), ключи клиентов только в памяти (после перезапуска — −404, клиенты делают новые). Файлы — LRU с потолком `CDN_CACHE_BYTES` и сроком простоя `CDN_IDLE_TTL` (30 дней без выдачи — файл удаляется). Узел сам считает отданные за месяц байты и помнит их после перезапуска.
- **Повторная загрузка.** Узел потерял файл (вытеснен, сломан диск, перезапуск с очищенным томом) — клиент получает `upload.cdnFileReuploadNeeded`, просит дом `upload.reuploadCdnFile`, дом ставит копию первой в очередь и ждёт её до `CDN_REUPLOAD_WAIT` (10 с). Не успел — клиент докачивает из дома.
- **`dc_options`.** С CDN `help.getConfig` отдаёт клиентам DC 1 (`PLUMA_DC1_ADDRESSES`) и DC 201 (`CDN_DC_ID`, `CDN_DC_ADDRESS`, `CDN_DC_SECRET`). Ошибка в адресах DC 1 отрезает Desktop — подробно `docs/ops/clients.md`, «Адрес сервера».

## Порты

| Порт | Кто | Где опубликован |
|---|---|---|
| TCP `CDN_PORT` (8443) | клиенты, MTProto obfuscated2 с секретом | адрес Pluma `195.201.252.126`; `forward.sh` его домой **не** пересылает |
| TCP 20510 | bff дома, управление (gRPC, взаимный TLS) | только `10.77.0.1` (туннель) |
| TCP 20511 | Prometheus дома, `/metrics` | только `10.77.0.1` |

Почему порт свой и с секретом: без `secret` Android пробует на адресе ещё порты 443 и 5222, а они уходят домой — рукопожатие с ключом дома зациклило бы загрузку ключей CDN (ADR 0065 §5). Секрет `dd…` публичный, это не ключ.

## Включение

Порядок важен: редиректы начинаются, только когда дом видит здоровый узел.

1. **Миграция и деплой дома без `CDN_NODE_CONTROL`.** Миграцию применяет сервис migrate при обычной выкладке (`pluma-admin migrate status` — `0 pending`). Повторный запуск безопасен.
2. **Секрет узла** (на Mac или дома, один раз): `echo "dd$(openssl rand -hex 16)"`. Один и тот же — `CDN_SECRET` на VPS и `CDN_DC_SECRET` дома.
3. **VPS, каталоги и сертификаты** (от root):

   ```bash
   install -d -o 65532 -g 65532 -m 0700 /opt/pluma-edge/cdn-data
   df -h /opt/pluma-edge                       # свободно сейчас: CDN_CACHE_BYTES не больше этого минус 5 ГБ
   /opt/pluma-edge/teamgramd/deploy/cdn/control-certs.sh /opt/pluma-edge/cdn /root/cdn-certs 10.77.0.1
   ```

   Скрипт выпускает свои удостоверяющие центры узла и bff (не общие с SFU), сертификат узла с именами `cdn` и `10.77.0.1`, файлы узла отдаёт uid 65532 с правами 0400. `/root/cdn-certs` перенести домой в `/opt/pluma/cdn` (права 0400, владелец root) и **удалить на VPS**:

   ```bash
   # на Mac
   ssh neo_old 'tar -C /root -cf - cdn-certs' | ssh pluma_home 'sudo tar -C /opt/pluma -xf - && sudo rm -rf /opt/pluma/cdn && sudo mv /opt/pluma/cdn-certs /opt/pluma/cdn && sudo chmod 0400 /opt/pluma/cdn/* && sudo chmod 0500 /opt/pluma/cdn'
   ssh neo_old 'rm -rf /root/cdn-certs'
   ```

4. **VPS, `/opt/pluma-edge/.env`** (права 600):

   | Переменная | Значение |
   |---|---|
   | `COMPOSE_PROFILES` | добавить `cdn` через запятую: `sites,cdn` |
   | `PLUMA_CDN_IMAGE` | `ghcr.io/sckraaa/pluma-cdn:<коммит>` (CI, `.github/workflows/images.yml`) |
   | `CDN_SECRET` | секрет из шага 2 |
   | `CDN_CACHE_BYTES` | байты под файлы: не больше свободного места тома минус 5 ГБ |
   | `CDN_PORT` | 8443 (по умолчанию) |
   | `CDN_DC_ID` | 201 (по умолчанию); тот же, что дома |
   | `CDN_IDLE_TTL` | 720h (по умолчанию) |
   | `PLUMA_CDN_DATA`, `PLUMA_CDN_CERTS` | `/opt/pluma-edge/cdn-data`, `/opt/pluma-edge/cdn` (по умолчанию) |
   | `CDN_MEMORY`, `CDN_CPUS` | 256m и 1 (по умолчанию) |

   Без `CDN_SECRET` или `CDN_CACHE_BYTES` узел не стартует и называет поле в логе; без `PLUMA_CDN_IMAGE` compose отказывается запускать проект с профилем `cdn`.
5. **Межсетевой экран хостера:** если у VPS есть Hetzner Cloud Firewall — открыть TCP 8443 на адресе Pluma. Docker публикует порт в обход ufw, но не в обход экрана хостера. В `forward.sh` порт не добавлять.
6. **Запуск узла:**

   ```bash
   cd /opt/pluma-edge && docker compose -p pluma-edge -f docker-compose.edge.yaml up -d cdn
   docker compose -p pluma-edge -f docker-compose.edge.yaml logs --tail 20 cdn   # строка "cdn: dc 201, clients on ..., key fingerprint ..."
   ```

7. **Prometheus дома:** `/opt/pluma/prometheus-targets/cdn.json`:

   ```json
   [{"targets":["10.77.0.1:20511"]}]
   ```

   Prometheus перечитывает файлы целей сам; цель `ops.cdn` — `up`.
8. **Дом, `/opt/pluma/.env`:**

   | Переменная | Значение |
   |---|---|
   | `CDN_NODE_CONTROL` | `10.77.0.1:20510` |
   | `PLUMA_CDN_CERTS` | `/opt/pluma/cdn` (по умолчанию) |
   | `CDN_DC_ID` | пусто или 201 |
   | `CDN_DC_ADDRESS` | `195.201.252.126:8443` — адрес, не имя |
   | `CDN_DC_SECRET` | секрет из шага 2 |
   | `CDN_MONTHLY_EGRESS_LIMIT_GB` | обязателен: ниже трафика тарифа VPS (раздел «Трафик и диск») |
   | `PLUMA_DC1_ADDRESSES` | `195.201.252.126:<вшитый порт>,195.201.252.126:<второй порт>` — вшитый в сборки порт первым; сверить с переменной `PLUMA_SERVER_PORT` репозиториев клиентов (`docs/ops/clients.md`) |
   | `CDN_MIN_FILE_SIZE`, `CDN_PUSH_RATE_MBIT`, `CDN_PUSH_CONCURRENCY`, `CDN_REUPLOAD_WAIT` | пусто: 1 МиБ, 20, 1, 10s |

9. **Деплой дома с наложением.** С CDN любая команда compose, которая пересоздаёт bff, включает `-f docker-compose.cdn.yaml` (переменные и сертификаты bff):

   ```bash
   cd /opt/pluma && docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml -f docker-compose.cdn.yaml up -d bff
   ```

   То же должно быть в `pluma-deploy.sh` (раздел «Выкладка»), иначе следующая выкладка пересоздаст bff без CDN. С этого момента клиенты получают `dc_options`.
10. **Проверка:** в Grafana → Explore → Prometheus `pluma_cdn_node_healthy` = 1; `select dc_id, fingerprint, unhealthy_reason, healthy_until from cdn_nodes;` — причина пустая; большое видео, присланное в чат, со второго телефона открывается дважды — после второго раза растёт `pluma_cdn_redirects_total{result="redirect"}`, а у узла — `pluma_cdn_served_bytes_total`.

## Выкладка

Оба скрипта выкладки лежат вне репозитория (`~/pluma-work/bin`); с CDN в них нужны такие шаги.

- **`pluma-edge-deploy.sh` (край):**
  - в список копируемых файлов добавить `teamgramd/deploy/cdn/cdn.yaml` и `teamgramd/deploy/cdn/control-certs.sh`;
  - если в `.env` есть `PLUMA_CDN_IMAGE=`: `docker pull -q "ghcr.io/sckraaa/pluma-cdn:$NEW"` и `sed -i "s#^PLUMA_CDN_IMAGE=.*#PLUMA_CDN_IMAGE=ghcr.io/sckraaa/pluma-cdn:$NEW#" .env` — так же, как для SFU;
  - в очистке старых образов — и `ghcr.io/sckraaa/pluma-cdn:` (оставлять предыдущий для отката).
  `up -d` того же скрипта пересоздаёт узел: ключи клиентов теряются (клиенты делают новые), файлы, ключ узла и счётчик трафика остаются в томе.
- **`pluma-deploy.sh` (дом):** если в `.env` задан `CDN_NODE_CONTROL` и есть `/opt/pluma/cdn/client.pem`, добавлять `-f docker-compose.cdn.yaml` к `C2` (как `docker-compose.groupcalls.yaml`); иначе печатать «cdn: no CDN_NODE_CONTROL or certificates yet; CDN off».

## Трафик и диск

- **Трафик.** Узел считает байты, отданные клиентам за календарный месяц UTC (с транспортом). Дом выводит узел из редиректов, когда счётчик достигает `CDN_MONTHLY_EGRESS_LIMIT_GB` (ГБ = 10⁹ байт), и возвращает его 1-го числа. Тревога `CdnEgressBudget` — на 80 %. Сколько трафика входит в тариф VPS, оператор проверяет в консоли Hetzner (у стандартных облачных тарифов в ЕС, по отчёту исследования, 20 ТБ; не проверено, тариф понижен) и ставит предел ниже него с запасом на SFU, TURN и трубу домой — они идут по тому же тарифу.
- **Диск.** Файлы занимают не больше `CDN_CACHE_BYTES`; при нехватке места вытесняются давно не отданные. Свободного места на томе меньше 2 ГБ — узел отказывает в новых файлах, и дом выводит его из редиректов (`disk_low`). Тревога `CdnEvictionChurn` — вытесняются файлы младше суток: кэш мал для того, что смотрят. Увеличить `CDN_CACHE_BYTES` (по `df`), `docker compose -p pluma-edge -f docker-compose.edge.yaml up -d cdn`.
- **Память и CPU:** 256 МБ и одно ядро; заливка дома — AES-CTR и SHA-256 при 20 Мбит/с, заметной нагрузки нет.
- **Том узла** (`PLUMA_CDN_DATA`): `files/` (шифр, файл на токен), `tmp/` (недописанное, чистится при запуске), `index.json` (порядок LRU, пишется раз в минуту), `rsa.key` (ключ узла, 0600), `egress.json` (трафик месяца). В бэкап не нужен: шифр заливается снова, новый ключ клиенты получают сами.

## Неполадки

| Тревога или симптом | Что значит и что делать |
|---|---|
| `CdnNodeUnhealthy` | дом 10 минут не пускает узел в редиректы. Причина — `select unhealthy_reason from cdn_nodes`: `unreachable` (управление не отвечает: узел остановлен, туннель, сертификаты), `mtproto_unreachable` (публичный порт не отвечает: экран хостера, `CDN_PORT`, маршрут дом → адрес Pluma), `fingerprint_mismatch` (на публичном адресе другой ключ: не тот порт или другой процесс), `dc_mismatch` (`CDN_DC_ID` разный на краю и дома), `no_key`, `request_keys_unset` (узел только что перезапущен — проходит сам), `disk_low` (свободного места меньше 2 ГБ), `egress_limit` (трафик месяца исчерпан). Лог bff: `docker compose logs --tail 100 bff \| grep 'cdn: node'` |
| `CdnNodeDown` | Prometheus 5 минут не получает метрик узла при включённом CDN (если файл цели есть, раньше, через 2 минуты, приходит общий `ServiceDown` с `job="ops.cdn"`, critical, как у `ops.sfu`): `docker compose -p pluma-edge -f docker-compose.edge.yaml ps cdn` и `logs --tail 100 cdn` на VPS; `wg show pluma`; есть ли `/opt/pluma/prometheus-targets/cdn.json` |
| `CdnPushFailing` | заливки не проходят. По причинам: `sum by (result) (increase(pluma_cdn_push_total[1h]))`; навсегда упавшие — `select last_error, count(*) from cdn_replicas where state = 4 group by last_error`. `short_read`/`read_error` — dfs не отдаёт файл целиком (лог dfs); `node_full` — диск узла; `node_error` — канал управления; `node_mismatch` — узел сохранил не то (диск VPS); `too_large` — файл больше `CDN_CACHE_BYTES`, ожидаемо. После устранения причины вернуть упавшие в очередь: `update cdn_replicas set state = 0, attempts = 0, next_try_at = 0, last_error = '' where state = 4 and last_error <> 'too_large';` |
| `CdnEgressBudget` | узел отдал 80 % месячного предела: проверить тариф в консоли Hetzner, при запасе поднять `CDN_MONTHLY_EGRESS_LIMIT_GB` дома и пересоздать bff с наложением |
| `CdnEvictionChurn` | раздел «Трафик и диск» |
| Клиенты качают большие файлы из дома при здоровом узле | норма для первого зрителя файла; дальше — `pluma_cdn_redirects_total` по `result`: `not_ready` (копия ещё в очереди — `pluma_cdn_queue{state="queued"}`), `small` (меньше `CDN_MIN_FILE_SIZE`), `error` (сбой MySQL или media — лог bff). Web и iOS без I-116 CDN не используют |

## Выключение и откат

- **Выключить CDN:** убрать `CDN_NODE_CONTROL` дома и пересоздать bff **без** `-f docker-compose.cdn.yaml`. Новых редиректов нет сразу после перезапуска bff; клиенты, уже качающие с узла, докачивают с него (дом узлу для отдачи не нужен), а если узел попросит reupload — из дома. `PLUMA_DC1_ADDRESSES` оставить: Desktop заменит список DC, пустой `dc_options` вернуть его к вшитому адресу не может (`docs/ops/clients.md`).
- **Остановить узел:** `docker compose -p pluma-edge -f docker-compose.edge.yaml stop cdn` — через ≤ 30 с дом выводит его из редиректов, всё идёт из дома. Убрать насовсем: `cdn` из `COMPOSE_PROFILES`, `docker compose -p pluma-edge -f docker-compose.edge.yaml rm -sf cdn`, затем удалить `/opt/pluma-edge/cdn-data` и `/opt/pluma-edge/cdn`.
- **Новый ключ узла:** остановить узел, удалить `rsa.key` в томе, запустить. Клиенты, увидев в `resPQ` незнакомый отпечаток, сами перезапрашивают `help.getCdnConfig` (ADR 0065, «Последствия»), дом берёт новый ключ из `Status`.
- **Новые сертификаты управления:** шаг 3 «Включения» заново, затем перезапустить узел и bff (с наложением).
- **Секрет `dd…` не менять без нужды:** он в `dc_options`, клиенты узнают новый только с очередным `help.getConfig`; до этого их соединения с узлом не проходят, и они качают из дома.

## Что видит узел

Узел на VPS вне доверенной зоны (ADR 0057). Он видит адреса клиентов, размеры файлов, время загрузок и **какие адреса берут один и тот же `file_token`** — новое по сравнению с трубой ADR 0061; владелец это принял (ADR 0065 §1). Не видит: содержимого файлов (ключ и IV каждого файла только дома и у клиента), ключей и данных дома, хэшей открытого текста, личных данных `initConnection` (не пишет и не хранит). Тот, кто владеет VPS, может подменить шифр: клиенты сверяют SHA-256 каждого расшифрованного куска с хэшами от дома и такой файл не примут. Секреты на VPS: сертификат и ключ управления (`/opt/pluma-edge/cdn`), RSA-ключ узла (`rsa.key` в томе), ключ подписи `request_token` (только в памяти, его выдаёт дом при каждом запуске узла). Утечка любого из них не раскрывает файлов.

Узел пишет в лог только счётчики и ошибки, без токенов, ключей и идентификаторов файлов; bff — так же (`tools/logaudit` ловит токены, ключи и хэши CDN в логах). Метрики без токенов, id файлов и адресов клиентов.
