# Российский узел amf

Задача 109, ADR 0072 §8. VPS **amf** (194.87.226.185, Timeweb, Москва, Ubuntu 26.04) несёт для пользователей из России три роли, все — только шифр:

1. **Выдача файлов 202** — `https://r1.pluma.chat`, nginx Pluma с кэшем на узле (TLS на узле, как у Gcore).
2. **Край мессенджера** — `wss://ru.pluma.chat/apiws`, чистая TCP-труба: TLS заканчивается дома в Caddy.
3. **TURN звонков** — coturn на 3478 и 49160–49659/udp со своим секретом.

Ядра, базы, ключей авторизации и секретов дома на узле нет. Docker на узле нет: службы из пакетов Ubuntu, свой экземпляр nginx. Узел общий: на нём сайт владельца `form.amf25.ru` (nginx владельца на 80 и 443); его не ломать.

```
клиент ──443, SNI ru.pluma.chat──► nginx stream владельца ──PROXY v1, TLS как есть──► туннель ──► дом 10.78.0.2:8443 Caddy ──► gnetway Edge
клиент ──443, SNI r1.pluma.chat──► nginx stream владельца ──PROXY v1──► nginx Pluma 127.0.0.1:9443 (TLS, подпись, кэш) ──► туннель ──► дом 10.78.0.2:443 Caddy, источник CDN
клиент ──UDP 3478, 49160–49659──► coturn Pluma
дом ──WireGuard plumaru, MTU 1280──► amf 51820/udp;   Prometheus дома ──► 10.78.0.1:9100
```

## Что стоит на узле

| Что | Где | Откуда |
|---|---|---|
| Исходники на коммит | `/opt/pluma-ru/src` | `deploy.sh` (git clone/checkout) |
| Переменные и секреты | `/etc/pluma-ru/env` (0600 root) | оператор, раздел «Переменные» |
| nginx Pluma | `/etc/pluma-ru/nginx.conf`, `files.conf`, `secrets.conf` (0600) | `teamgramd/deploy/ru/nginx.conf`, `files.conf.tmpl` |
| coturn | `/etc/pluma-ru/turn.conf` (0640 root:turnserver) | `teamgramd/deploy/turn/turnserver.conf` + адрес и секрет узла |
| Файлы для nginx владельца | `/etc/pluma-ru/owner/` | `stream-upstreams.conf`, `sni.map.tmpl`, `acme-http.conf.tmpl` |
| Сертификат `r1` | `/etc/pluma-ru/letsencrypt/` | certbot узла, свой каталог настроек |
| Кэш выдачи 202 | `/var/cache/pluma-ru/files` (15 ГБ, 36 ч) | nginx Pluma |
| Метрики textfile | `/var/lib/pluma-ru/textfile/pluma_ru.prom` | `wgstat.sh` раз в минуту |
| Службы | `pluma-ru-nginx`, `pluma-ru-turn`, `pluma-ru-node-exporter`, `pluma-ru-wgstat.timer`, `pluma-ru-certbot.timer` | `/etc/systemd/system/` |

- Службы пакетов `coturn.service` и `prometheus-node-exporter.service` замаскированы (`systemctl mask`) до установки пакетов, чтобы postinst их не запустил: `deb-systemd-helper unmask` снимает только свою маску (Ubuntu 26.04, `/usr/bin/deb-systemd-helper`, `unmask_service`).
- nginx Pluma — отдельный процесс (`nginx -c /etc/pluma-ru/nginx.conf`), свои пользователь `pluma-ru`, pid, временные каталоги и журнал (journald юнита). Журнала доступа нет.
- Журналы: `journalctl -u pluma-ru-nginx`, `-u pluma-ru-turn`, `-u pluma-ru-node-exporter`, `-u pluma-ru-certbot`.

## Что меняется у nginx владельца
Только через `owner-nginx.sh install`, один раз и по слову владельца (решение 14 брифа 109). Выкладка (`deploy.sh`) nginx владельца не трогает и не перезагружает.

- `/etc/nginx/stream.d/pluma-upstreams.conf` — направления `pluma_ru_edge` (`10.78.0.2:8443`) и `pluma_ru_files` (`127.0.0.1:9443`);
- `/etc/nginx/stream.d/pluma-sni.map` — имена → эти направления (имя не на `.conf`: `stream.d/*.conf` включается на уровне `stream {}`);
- строка `include /etc/nginx/stream.d/pluma-sni.map;` в `map $ssl_preread_server_name` файла `sni.conf` (сразу после заголовка карты);
- `/etc/nginx/conf.d/pluma-acme-http.conf` — порт 80: вызовы HTTP-01 для `r1` из `/var/lib/pluma-ru/acme`, для `ru` — домой на `http://10.78.0.2:80`; остальное — 301 на `https://`.

`owner-nginx.sh check` читает `nginx -T` и `nginx -V` и ничего не меняет. Проверяет: модули `secure_link`, `slice`, `realip`, `v2`, `ssl` в сборке (в пакете Ubuntu 26.04, 1.28.3, они есть — `nginx -V` на amf 2026-11-17); одна карта `$ssl_preread_server_name` в `stream {}`; её сервер `stream` слушает 443 на всех IPv4 (дом проверяет выдачу на `10.78.0.1:443`), шлёт PROXY (`proxy_protocol on`) и ведёт по карте; `stream.d/*.conf` включается в `stream {}`, `conf.d/*.conf` — в `http {}`; имена Pluma не заняты ни в карте, ни в `server_name` сайтов владельца. Печатает `state installed` или `state not installed`.

`install`: сначала `check`; копия `sni.conf` в `/var/lib/pluma-ru/owner-nginx-backup/`; файлы и строка; `nginx -t`; `systemctl reload nginx`. При любой ошибке файлы владельца возвращаются как были. Повторный запуск обновляет файлы Pluma (после смены имён), строку второй раз не добавляет. `remove` убирает строку и файлы, `nginx -t`, `reload`.

`include` внутри `map` модуля `stream` работает: проверено тестом на nginx 1.28.3 Ubuntu 26.04 (`teamgramd/deploy/ru/test`, `TestNodeNginx`: соединение с SNI `r1` проходит через карту владельца в nginx Pluma).

## Переменные
`/etc/pluma-ru/env` — строки `ИМЯ=значение` без кавычек, права 0600, владелец root. Другие имена — ошибка. Значения проверяются до записи чего-либо; сообщение об ошибке называет переменную, но не значение. Ничего из файла не печатается.

| Переменная | Что | Как получить |
|---|---|---|
| `PLUMA_RU_PUBLIC_IPV4` | публичный адрес узла (TURN: `listening-ip`, `relay-ip`, `external-ip`) | `194.87.226.185` |
| `PLUMA_RU_FILES_DOMAIN` | имя выдачи 202 | `r1.pluma.chat` |
| `PLUMA_RU_EDGE_DOMAIN` | имя края | `ru.pluma.chat` |
| `PLUMA_CDN_ORIGIN_DOMAIN` | имя источника CDN дома (SNI и `Host` запросов домой) | то же, что в `.env` дома |
| `PLUMA_WEB_DOMAIN` | сайт Web K (CORS) | то же, что в `.env` дома |
| `CDN_RU_TOKEN_KEY` | ключ подписи выдачи 202 (формула Secure Token Gcore), 6–32 символа `A-Z a-z 0-9 . _ ~ -` | `openssl rand -hex 16`; то же значение в `.env` дома |
| `CDN_RU_ORIGIN_AUTH` | значение `X-Pluma-Origin-Auth` выдачи 202, 64 шестнадцатеричные цифры | `openssl rand -hex 32`; то же значение в `.env` дома |
| `CALLS_TURN_SECRETS` | секрет TURN узла: `ru=<секрет>` (32–128 букв и цифр); **только метка `ru`** — секреты других TURN на узле запрещены, `deploy.sh` откажет | `openssl rand -hex 32`; та же запись в `.env` дома |
| `PLUMA_GCORE_EDGE_DOMAIN` | необязательно: публичное имя края Gcore (`<префикс2>.gcorecdn.com`, `docs/ops/edge.md`) — Web K, открытый на нём, получает файлы с выдачи 202 (CORS) | имя второго ресурса Gcore |
| `PLUMA_RU_ORIGIN_ADDR` | необязательно: адрес Caddy дома, по умолчанию `10.78.0.2:443` | — |
| `PLUMA_RU_ACME_EMAIL` | необязательно: адрес для аккаунта ACME узла | — |

Секреты создаются `openssl rand` на одной машине и переносятся по SSH без печати. Пример для дома (запись `CALLS_TURN_SECRETS` на узел — только `ru=…`; если в `.env` дома у переменной несколько меток, переносится одна она, а без метки `ru` не переносится ничего):

```bash
# дом: значения уже в .env; на amf уходят только три строки, без вывода на экран
grep -E '^(CDN_RU_TOKEN_KEY|CDN_RU_ORIGIN_AUTH)=' .env | ssh root@194.87.226.185 'umask 077; cat >> /etc/pluma-ru/env'
grep -E '^CALLS_TURN_SECRETS=' .env | sed -nE 's/^CALLS_TURN_SECRETS=(.*,)?(ru=[0-9A-Za-z]+)(,.*)?$/CALLS_TURN_SECRETS=\2/p' \
  | ssh root@194.87.226.185 'umask 077; cat >> /etc/pluma-ru/env'
```

## Первое включение
Порядок всего включения — бриф 109, «Эксплуатация», «Российский узел amf». Здесь — шаги на узле. Перед началом: туннель поднят (`tunnel.sh --link ru`, часть 9; `wg show plumaru` на обеих сторонах, `ping -M do -s 1252 10.78.0.1` из дома), A-записи `ru` и `r1` → 194.87.226.185 без проксирования Cloudflare.

1. **Переменные.** На amf: `install -d -m 0755 /etc/pluma-ru`, файл `/etc/pluma-ru/env` (0600) по таблице выше.
2. **Выкладка.** С Mac:
   ```bash
   ssh root@194.87.226.185 'NEW=<sha> bash -s' < teamgramd/deploy/ru/deploy.sh
   ```
   Первый запуск ставит недостающие пакеты (`coturn`, `prometheus-node-exporter`, `certbot`, `wireguard-tools`, `nginx` — только те, которых нет: `apt-get install` установленного пакета обновил бы и nginx владельца), создаёт пользователя `pluma-ru` и каталоги, пишет файлы, запускает TURN, node-exporter и таймеры. nginx Pluma без сертификата не запускается (`no certificate for … yet`); конфигурация при этом всё равно проверяется `nginx -t` с временным самоподписанным сертификатом, который никогда не отдаётся.
3. **nginx владельца.** На amf:
   ```bash
   /opt/pluma-ru/src/teamgramd/deploy/ru/owner-nginx.sh check     # все строки ok, state not installed
   /opt/pluma-ru/src/teamgramd/deploy/ru/owner-nginx.sh install
   ```
   Сразу проверить сайт владельца: `curl -sSI https://form.amf25.ru/` — 200 или как было до установки.
4. **Сертификат `r1`** (certbot узла, свой каталог `/etc/pluma-ru/letsencrypt`, аккаунт отдельный от аккаунта владельца):
   ```bash
   /opt/pluma-ru/src/teamgramd/deploy/ru/deploy.sh cert-register   # печатает строку CAA с accounturi аккаунта узла
   ```
   Владелец добавляет эту запись CAA на имя `r1.pluma.chat` (проверка владельца 7 брифа): записи CAA `pluma.chat` разрешают только аккаунт дома, а на имени с собственными CAA записи родителя не действуют (RFC 8659 — по памяти, НЕ ПРОВЕРЕНО; проверка 7 это и проверяет: `dig CAA r1.pluma.chat` — только новая запись). Затем:
   ```bash
   /opt/pluma-ru/src/teamgramd/deploy/ru/deploy.sh cert-issue      # HTTP-01 через порт 80 владельца, ключ ECDSA P-256, затем nginx Pluma
   ```
   `cert-register` вызывает `certbot register --agree-tos`: запуск команды — согласие с условиями Let's Encrypt от имени оператора.
5. **Дом.** `.env` (бриф 109, «Эксплуатация», шаг 5), деплой с `docker-compose.ru.yaml`, цели Prometheus:
   ```bash
   echo '[{"targets":["10.78.0.1:9100"],"labels":{"node":"amf"}}]' | sudo tee /opt/pluma/prometheus-targets/node-ru.json
   ```
6. **Проверки** — раздел «Проверка».

## Выкладка (обновление)
```bash
ssh root@194.87.226.185 'NEW=<sha> bash -s' < teamgramd/deploy/ru/deploy.sh
```
`deploy.sh` получает коммит в `/opt/pluma-ru/src` и запускает `deploy.sh apply` этого коммита: новые файлы собираются в `/etc/pluma-ru/.staging.*`, проверяются `nginx -t` и только потом заменяют рабочие. Перезапускается только изменённое: nginx — `reload` (`restart` при смене юнита), TURN — `restart` только при смене `turn.conf` или юнита (звонки через реле узла переподключаются), node-exporter — при смене юнита. Если изменились файлы для nginx владельца, скрипт об этом пишет — их ставит `owner-nginx.sh install`.

**Обновления пакетов.** `unattended-upgrades` обновляет nginx, coturn, node-exporter. Пакет перезапускает только `nginx.service` владельца; nginx Pluma, TURN и node-exporter продолжают работать на старых бинарниках до перезапуска (`needrestart` на amf нет). После обновления этих пакетов: `systemctl restart pluma-ru-nginx pluma-ru-node-exporter` и в тихое время `pluma-ru-turn`.

## Проверка
- Службы: `systemctl status pluma-ru-nginx pluma-ru-turn pluma-ru-node-exporter pluma-ru-wgstat.timer pluma-ru-certbot.timer`.
- Выдача 202 из дома (через туннель; URL канарейки — `pluma-admin cdn canary-url 202`, как в `docs/ops/cdn.md`, «Проверки владельца»):
  ```bash
  curl -sS -D - -o /dev/null --resolve r1.pluma.chat:443:10.78.0.1 -H 'Range: bytes=0-99' '<URL канарейки>'   # 206, Content-Range: bytes 0-99/262144
  curl -sS -D - -o /dev/null --resolve r1.pluma.chat:443:10.78.0.1 '<URL из canary-url -expired>'          # 410
  ```
  Снаружи то же без `--resolve`.
- Порт 80: `curl -sSI http://r1.pluma.chat/x` и `http://ru.pluma.chat/x` — 301 на `https://`.
- Метрики: `curl -s http://10.78.0.1:9100/metrics | grep '^pluma_'` из дома.
- TURN: `turnutils_uclient` с учётными данными записи `@ru` (как в `docs/ops/calls.md`) или проверка владельца 11.
- Сертификат: `certbot certificates --config-dir /etc/pluma-ru/letsencrypt`; продление: `certbot renew --dry-run --config-dir /etc/pluma-ru/letsencrypt --work-dir /var/lib/pluma-ru/certbot --logs-dir /var/log/pluma-ru/certbot`.

## Как устроена выдача 202
- Подпись — формула Secure Token Gcore (`pkg/cdn/signer_gcore.go`): `md5 = base64url(MD5(expires + путь + " " + ключ))` без `=`, `?md5=…&expires=…`. nginx проверяет её модулем `secure_link`: `secure_link_md5 "$secure_link_expires$uri $pluma_token_key"`. Неверная или отсутствующая подпись — 403, истёкшая — 410. Сервер подписывает выдачу 202 тем же `GcoreSigner`, что и Gcore. Тест: `TestNodeNginx` подписывает URL кодом `pkg/cdn` и проверяет 206, 403, 410 на nginx Ubuntu 26.04.
- Пути — только `/o/<64 hex>` и `/c/v1`; методы GET и HEAD, OPTIONS — ответ CORS 204, иные — 405; остальные пути — 404.
- Кэш: куски `slice` по 1 МиБ, ключ — путь и диапазон куска без строки запроса (все токены делят объект), 15 ГБ, 36 ч без запросов; срок — `Cache-Control` источника (`immutable`, 30 дней). Любой одиночный `Range` nginx отдаёт из кусков с точным `Content-Range`; несколько диапазонов — весь объект (`max_ranges 1`).
- Домой уходят только путь со строкой запроса (токен, источник его не читает), диапазон куска и `X-Pluma-Origin-Auth` выдачи 202 (`proxy_pass_request_headers off`): ни адреса, ни `User-Agent`, ни cookie клиента. TLS к источнику проверяется (`proxy_ssl_verify`, имя источника).
- CORS: `Access-Control-Allow-Origin` только для `https://<PLUMA_WEB_DOMAIN>`, `https://<PLUMA_RU_EDGE_DOMAIN>` и, если задано, `https://<PLUMA_GCORE_EDGE_DOMAIN>`, `Access-Control-Expose-Headers: Content-Range, Content-Length`, `Vary: Origin` — на всех ответах, в том числе 403/410.
- Предел — 64 соединения на адрес клиента (адрес из PROXY v1 nginx владельца); предела частоты запросов нет.
- Заголовок `Server: nginx` остаётся (без версии): убрать его можно только модулем `headers-more`, а его пакет включил бы модуль и в nginx владельца.

## Метрики и алерты
Метрики textfile (`wgstat.sh`): `pluma_wg_up`, `pluma_wg_latest_handshake_seconds` (интерфейс `plumaru`), `pluma_ru_cache_bytes`, `pluma_ru_cache_max_bytes`, `pluma_ru_stats_timestamp_seconds`; остальное — стандартный node-exporter с меткой `node="amf"`.

Алерты — `teamgramd/deploy/prometheus/server/rules/ru-node.yml`:

| Алерт | Когда |
|---|---|
| `RuNodeDown` | Prometheus дома 5 минут не достаёт `10.78.0.1:9100` |
| `RuTunnelHandshakeStale` | рукопожатия с домом нет больше 3 минут или нет интерфейса `plumaru` |
| `RuNodeStatsStale` | файл textfile старше 5 минут (таймер `wgstat` встал) |
| `RuNodeCacheOverLimit` | кэш больше 110 % своего `max_size` 30 минут |

Диск и память узла покрывают общие `DiskSpaceLow`, `DiskSpaceCritical`, `MemoryLow` (`general.yml`); выдачу 202 — `CdnDeliveryUnhealthy`, `CdnBudget`, `CdnFallbackHigh` по `dc="202"` (`cdn.yml`).

## Неполадки
| Признак | Что смотреть |
|---|---|
| `RuNodeDown` | Дом: `ping -c3 10.78.0.1`, `sudo wg show plumaru`. amf: `systemctl status pluma-ru-node-exporter wg-quick@plumaru`. node-exporter слушает только `10.78.0.1` и ждёт интерфейс (`Restart=always`). |
| `RuTunnelHandshakeStale` | Обе стороны: `wg show plumaru` (latest handshake, transfer); MTU 1280 с обеих сторон; UDP 51820 до amf. |
| `RuNodeStatsStale` | `systemctl status pluma-ru-wgstat.timer pluma-ru-wgstat.service`, `journalctl -u pluma-ru-wgstat --since -1h`. |
| `RuNodeCacheOverLimit` | `journalctl -u pluma-ru-nginx --since -1h | grep -i cache`, `du -sh /var/cache/pluma-ru/files`. Кэш можно очистить: `systemctl stop pluma-ru-nginx; rm -rf /var/cache/pluma-ru/files/*; systemctl start pluma-ru-nginx`. |
| Выдача 202 нездорова (`CdnDeliveryUnhealthy{dc="202"}`) | URL канарейки вручную (раздел «Проверка»). 403 на верный URL — `CDN_RU_TOKEN_KEY` дома и на узле разные. 502/504 — источник: туннель, Caddy дома (`docker compose logs --tail 100 web`), `CDN_RU_ORIGIN_AUTH` дома и на узле (источник отвечает 403). Ошибки TLS к источнику — `journalctl -u pluma-ru-nginx`. |
| `deploy.sh`: `nginx -t failed on the new files` | Рабочие файлы не тронуты; ошибка выше в выводе. |
| `owner-nginx.sh install`: `check failed` | Строки `FAIL` в выводе; ничего не менялось. Если карта владельца устроена иначе (заголовок не на одной строке, несколько карт) — строку `include` добавить руками после согласования с владельцем. |
| Сайт владельца сломан после `install` | `owner-nginx.sh remove`; копии `sni.conf` — `/var/lib/pluma-ru/owner-nginx-backup/`. |

## Откат и выключение
- Пользователи из России без узла: дома убрать `RU` из `CDN_ROUTES` и `EDGE_ROUTES`, деплой — все идут по записи `*` (Gcore и прямой путь).
- nginx владельца: `owner-nginx.sh remove`.
- Службы узла: `systemctl disable --now pluma-ru-nginx pluma-ru-turn pluma-ru-node-exporter pluma-ru-wgstat.timer pluma-ru-certbot.timer`.
- TURN узла из списка звонков: убрать запись `@ru` из `CALLS_ICE_SERVERS` дома, деплой.

## Что видит узел и его хостер
Адреса пользователей, которые ходят через узел, время и объёмы; имена `ru`/`r1` в SNI; шифр файлов в кэше (до 36 ч без запросов) и диапазоны; пакеты SRTP звонков через TURN; внешний адрес домашнего роутера (источник WireGuard). Не видит: содержимое MTProto (TLS края заканчивается дома), ключи и хэши файлов, голос и видео звонков. Секреты на узле — только его собственные: ключ подписи и значение доступа выдачи 202, секрет TURN узла.

## Не проверено
- Отвечает ли Caddy на TLS-ALPN-01 за PROXY на `:8443`; HTTP-01 через порт 80 (`pluma-acme-http.conf`) работает независимо.
- Наследование CAA (RFC 8659 — по памяти; проверка владельца 7).
- Вывод `certbot show_account` (строка `Account URL:`), по которой `cert-register` печатает запись CAA, — по памяти. Если строки нет, `cert-register` печатает только заголовок: URL аккаунта тогда взять из `certbot show_account --config-dir /etc/pluma-ru/letsencrypt` глазами.
- Работа юнитов под systemd на самом amf: в тесте юниты проверены `systemd-analyze verify`, запуск — только при установке.
- Память nginx, coturn и node-exporter на узле — замерить после установки.
