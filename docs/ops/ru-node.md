# Российский узел amf

Задачи 109 и 113, ADR 0072 §8. VPS **amf** (194.87.226.185, Timeweb, Москва, Ubuntu 26.04) несёт для пользователей из России пять ролей, все — только шифр. Имена `*.pluma.chat` в России заблокированы, поэтому у узла свои имена — пока на личном домене владельца `sckra.dev` (решение 8 задачи 113; другой домен — только смена переменных, без пересборки):

1. **Выдача файлов 202** — `https://r1.sckra.dev`, nginx Pluma с кэшем на узле (TLS на узле, как у Gcore). На том же имени — кэш выпусков приложений `/dl/` (задача 115, раздел «Выпуски»).
2. **Край мессенджера и Web K** — `wss://ru.sckra.dev/apiws` и сайт Web K на том же имени, чистая TCP-труба: TLS заканчивается дома в Caddy.
3. **Зеркала сайта и входа на сайты** (задача 113) — `https://pluma.sckra.dev` (сайт целиком: страницы, установщики и обновления приложений в `/dl/`, установка iPhone, ссылки) и `https://oauth.sckra.dev` (вход на сайты через Pluma). Та же труба, что у края: TLS заканчивается дома в Caddy.
4. **TURN звонков** — coturn на 3478 и 49160–49659/udp со своим секретом.
5. **Труба групповых звонков** (задача 116, ADR 0074) — nginx Pluma на 20600/udp и 20601/tcp адреса узла передаёт медиа как есть SFU на краю Hetzner (раздел «Труба групповых звонков»).

Ядра, базы, ключей авторизации и секретов дома на узле нет. Docker на узле нет: службы из пакетов Ubuntu, свой экземпляр nginx. Узел общий: на нём сайт владельца `form.amf25.ru` (nginx владельца на 80 и 443); его не ломать.

```
клиент ──443, SNI ru.sckra.dev──► nginx stream владельца ──PROXY v1, TLS как есть──► туннель ──► дом 10.78.0.2:8443 Caddy ──► gnetway Edge, статика Web K
клиент ──443, SNI pluma.sckra.dev, oauth.sckra.dev──► так же ──► дом 10.78.0.2:8443 Caddy ──► сайт (releases, iosinstall, links), links (вход)
клиент ──443, SNI r1.sckra.dev──► nginx stream владельца ──PROXY v1──► nginx Pluma 127.0.0.1:9443 (TLS, подпись, кэш) ──► туннель ──► дом 10.78.0.2:443 Caddy, источник CDN (шифр — bff, /dl/ — releases)
клиент ──UDP 3478, 49160–49659──► coturn Pluma
клиент ──UDP 20600, TCP 20601──► nginx Pluma (stream, как есть) ──► SFU 195.201.252.126:20600/20601 на краю Hetzner
дом ──WireGuard plumaru, MTU 1280──► amf 51820/udp;   Prometheus дома ──► 10.78.0.1:9100
```

## Что стоит на узле

| Что | Где | Откуда |
|---|---|---|
| Исходники на коммит | `/opt/pluma-ru/src` | `deploy.sh` (git clone/checkout) |
| Переменные и секреты | `/etc/pluma-ru/env` (0600 root) | оператор, раздел «Переменные» |
| nginx Pluma | `/etc/pluma-ru/nginx.conf`, `files.conf`, `stream.conf`, `secrets.conf` (0600) | `teamgramd/deploy/ru/nginx.conf`, `files.conf.tmpl`, `stream.conf.tmpl` |
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
- `/etc/nginx/stream.d/pluma-sni.map` — имена → эти направления: имя края и имена зеркал (`PLUMA_RU_MIRROR_DOMAINS`) → `pluma_ru_edge`, имя выдачи 202 → `pluma_ru_files` (имя файла не на `.conf`: `stream.d/*.conf` включается на уровне `stream {}`);
- строка `include /etc/nginx/stream.d/pluma-sni.map;` в `map $ssl_preread_server_name` файла `sni.conf` (сразу после заголовка карты);
- `/etc/nginx/conf.d/pluma-acme-http.conf` — порт 80: вызовы HTTP-01 для `r1` из `/var/lib/pluma-ru/acme`, для `ru` и зеркал — домой на `http://10.78.0.2:80`; остальное — 301 на `https://`.

`owner-nginx.sh check` читает `nginx -T` и `nginx -V` и ничего не меняет. Проверяет: модули `secure_link`, `slice`, `realip`, `v2`, `ssl` в сборке (в пакете Ubuntu 26.04, 1.28.3, они есть — `nginx -V` на amf 2026-11-17); одна карта `$ssl_preread_server_name` в `stream {}`; её сервер `stream` слушает 443 на всех IPv4 (дом проверяет выдачу на `10.78.0.1:443`), шлёт PROXY (`proxy_protocol on`) и ведёт по карте; `stream.d/*.conf` включается в `stream {}`, `conf.d/*.conf` — в `http {}`; имена Pluma не заняты ни в карте, ни в `server_name` сайтов владельца. Печатает `state installed` или `state not installed`.

`install`: сначала `check`; копия `sni.conf` в `/var/lib/pluma-ru/owner-nginx-backup/`; файлы и строка; `nginx -t`; `systemctl reload nginx`. При любой ошибке файлы владельца возвращаются как были. Повторный запуск обновляет файлы Pluma (после смены имён), строку второй раз не добавляет. `remove` убирает строку и файлы, `nginx -t`, `reload`.

`include` внутри `map` модуля `stream` работает: проверено тестом на nginx 1.28.3 Ubuntu 26.04 (`teamgramd/deploy/ru/test`, `TestNodeNginx`: соединение с SNI `r1` проходит через карту владельца в nginx Pluma).

## Переменные
`/etc/pluma-ru/env` — строки `ИМЯ=значение` без кавычек, права 0600, владелец root. Другие имена — ошибка. Значения проверяются до записи чего-либо; сообщение об ошибке называет переменную, но не значение. Ничего из файла не печатается.

| Переменная | Что | Как получить |
|---|---|---|
| `PLUMA_RU_PUBLIC_IPV4` | публичный адрес узла (TURN: `listening-ip`, `relay-ip`, `external-ip`) | `194.87.226.185` |
| `PLUMA_RU_FILES_DOMAIN` | имя выдачи 202 | `r1.sckra.dev` |
| `PLUMA_RU_EDGE_DOMAIN` | имя края и Web K | `ru.sckra.dev` |
| `PLUMA_RU_MIRROR_DOMAINS` | необязательно (задача 113): другие имена, TLS которых идёт домой, как у края, — зеркала сайта и входа на сайты, через запятую без пробелов, до 8, не совпадают с именами края и выдачи 202 | `pluma.sckra.dev,oauth.sckra.dev`; дома те же имена — `PLUMA_RU_SITE_DOMAIN` и `PLUMA_RU_OAUTH_DOMAIN` |
| `PLUMA_CDN_ORIGIN_DOMAIN` | имя источника CDN дома (SNI и `Host` запросов домой) | то же, что в `.env` дома |
| `PLUMA_WEB_DOMAIN` | сайт Web K (CORS) | то же, что в `.env` дома |
| `CDN_RU_TOKEN_KEY` | ключ подписи выдачи 202 (формула Secure Token Gcore), 6–32 символа `A-Z a-z 0-9 . _ ~ -` | `openssl rand -hex 16`; то же значение в `.env` дома |
| `CDN_RU_ORIGIN_AUTH` | значение `X-Pluma-Origin-Auth` выдачи 202, 64 шестнадцатеричные цифры | `openssl rand -hex 32`; то же значение в `.env` дома |
| `CALLS_TURN_SECRETS` | секрет TURN узла: `ru=<секрет>` (32–128 букв и цифр); **только метка `ru`** — секреты других TURN на узле запрещены, `deploy.sh` откажет | `openssl rand -hex 32`; та же запись в `.env` дома |
| `PLUMA_GCORE_EDGE_DOMAIN` | необязательно: публичное имя края Gcore (`<префикс2>.gcorecdn.com`, `docs/ops/edge.md`) — Web K, открытый на нём, получает файлы с выдачи 202 (CORS) | имя второго ресурса Gcore |
| `PLUMA_RU_ORIGIN_ADDR` | необязательно: адрес Caddy дома, по умолчанию `10.78.0.2:443` | — |
| `PLUMA_RU_ACME_EMAIL` | необязательно: адрес для аккаунта ACME узла | — |
| `PLUMA_RU_SFU_ADDR` | необязательно (задача 116): публичный IPv4 SFU групповых звонков, куда труба передаёт медиа; не адрес самого узла. Пусто — трубы нет, порты 20600/20601 не слушаются | `195.201.252.126` (адрес Pluma на краю Hetzner, `PLUMA_PUBLIC_IPV4` края) |

Секреты создаются `openssl rand` на одной машине и переносятся по SSH без печати. Пример для дома (запись `CALLS_TURN_SECRETS` на узел — только `ru=…`; если в `.env` дома у переменной несколько меток, переносится одна она, а без метки `ru` не переносится ничего):

```bash
# дом: значения уже в .env; на amf уходят только три строки, без вывода на экран
grep -E '^(CDN_RU_TOKEN_KEY|CDN_RU_ORIGIN_AUTH)=' .env | ssh root@194.87.226.185 'umask 077; cat >> /etc/pluma-ru/env'
grep -E '^CALLS_TURN_SECRETS=' .env | sed -nE 's/^CALLS_TURN_SECRETS=(.*,)?(ru=[0-9A-Za-z]+)(,.*)?$/CALLS_TURN_SECRETS=\2/p' \
  | ssh root@194.87.226.185 'umask 077; cat >> /etc/pluma-ru/env'
```

## Первое включение
Порядок всего включения — бриф 109, «Эксплуатация», «Российский узел amf». Здесь — шаги на узле. Перед началом: туннель поднят (`tunnel.sh --link ru`, часть 9; `wg show plumaru` на обеих сторонах, `ping -M do -s 1252 10.78.0.1` из дома), A-записи `ru`, `r1` и зеркал → 194.87.226.185 без проксирования Cloudflare.

1. **Переменные.** На amf: `install -d -m 0755 /etc/pluma-ru`, файл `/etc/pluma-ru/env` (0600) по таблице выше.
2. **Выкладка.** С Mac:
   ```bash
   ssh root@194.87.226.185 'NEW=<sha> bash -s' < teamgramd/deploy/ru/deploy.sh
   ```
   Первый запуск ставит недостающие пакеты (`coturn`, `prometheus-node-exporter`, `certbot`, `wireguard-tools`, `nginx`, `libnginx-mod-stream` (модуль, который грузит `nginx.conf` для трубы групповых звонков; пакет nginx Ubuntu его не тянет) — только те, которых нет: `apt-get install` установленного пакета обновил бы и nginx владельца), создаёт пользователя `pluma-ru` и каталоги, пишет файлы, запускает TURN, node-exporter и таймеры. nginx Pluma без сертификата не запускается (`no certificate for … yet`); конфигурация при этом всё равно проверяется `nginx -t` с временным самоподписанным сертификатом, который никогда не отдаётся.
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
   Владелец добавляет эту запись CAA на имя выдачи 202 (`r1.sckra.dev`; проверка владельца 7 брифа 109): записи CAA зоны разрешают только аккаунт дома, а на имени с собственными CAA записи родителя не действуют (RFC 8659 — по памяти, НЕ ПРОВЕРЕНО; проверка 7 это и проверяет: `dig CAA r1.sckra.dev` — только новая запись). Затем:
   ```bash
   /opt/pluma-ru/src/teamgramd/deploy/ru/deploy.sh cert-issue      # HTTP-01 через порт 80 владельца, ключ ECDSA P-256, затем nginx Pluma
   ```
   `cert-register` вызывает `certbot register --agree-tos`: запуск команды — согласие с условиями Let's Encrypt от имени оператора.
5. **Дом.** `.env` (бриф 109, «Эксплуатация», шаг 5), деплой с `docker-compose.ru.yaml`, цели Prometheus:
   ```bash
   echo '[{"targets":["10.78.0.1:9100"],"labels":{"node":"amf"}}]' | sudo tee /opt/pluma/prometheus-targets/node-ru.json
   ```
6. **Проверки** — раздел «Проверка».

## Зеркала сайта и входа на сайты (задача 113)
Сайт `pluma.chat` и вход на сайты `oauth.pluma.chat` в России заблокированы. Зеркала отдают то же самое под именами узла: Caddy дома собирает сайты зеркал из тех же сниппетов Caddyfile, что и основные (`teamgramd/deploy/web/ru-site.caddy`, `ru-oauth.caddy`), так что зеркало не расходится с оригиналом. Узел ничего не расшифровывает: TLS зеркал, как у края, идёт домой на `:8443` с PROXY v1, сертификаты выпускает Caddy дома (HTTP-01 через порт 80 узла).

- **Сайт** (`pluma.sckra.dev`): страницы, `/dl/` (установщики и обновления приложений), установка iPhone (`/ios/adhoc/`), ссылки. Обновления: bff по стране клиента (`PLUMA_RELEASES_ROUTES`, `docs/ops/releases.md`) отдаёт Desktop префикс обновлений, а iPhone — ссылку обновления на зеркале; служба установки iPhone (`MIRROR_HOSTS` = `PLUMA_RU_SITE_DOMAIN`) называет манифест и IPA на том имени, на которое пришёл запрос. `/dl/<платформа>/latest` отвечает переходом по пути того же сайта, так что прямая ссылка на установщик работает и на зеркале. Android обновляется документом через MTProto — через край, правок не нужно.
- **Вход на сайты** (`oauth.sckra.dev`): страницы провайдера в links. links узнаёт провайдера по `Host`, поэтому Caddy передаёт ему имя `PLUMA_OAUTH_DOMAIN`: провайдер тот же, `iss` токенов прежний. Сайт, который пускает пользователей из России, направляет их на `https://oauth.sckra.dev/auth?...` (обмен кода на токен — сервер к серверу, по-прежнему `oauth.pluma.chat`).

**Включение** (оператор; DNS — владелец):
1. DNS: A-записи `pluma.sckra.dev` и `oauth.sckra.dev` → `194.87.226.185`, без проксирования. CAA зоны должны разрешать аккаунт ACME дома (как для `ru.sckra.dev`, который уже работает).
2. amf: в `/etc/pluma-ru/env` строка `PLUMA_RU_MIRROR_DOMAINS=pluma.sckra.dev,oauth.sckra.dev`; выкладка (`deploy.sh`), затем `owner-nginx.sh check` и `owner-nginx.sh install` — изменились карта SNI и порт 80 владельца.
3. Дом, `.env`: `PLUMA_RU_SITE_DOMAIN=pluma.sckra.dev`, `PLUMA_RU_OAUTH_DOMAIN=oauth.sckra.dev`, `PLUMA_RELEASES_ROUTES=RU,BY=https://pluma.sckra.dev` (страны — те же, что у края в `EDGE_ROUTES`); деплой с `docker-compose.ru.yaml`, образ службы установки iPhone — с этой версии (переменная `MIRROR_HOSTS`).
4. Проверка: `curl -sSI https://pluma.sckra.dev/` и `https://pluma.sckra.dev/dl/releases.json` — 200; `curl -sS https://oauth.sckra.dev/.well-known/openid-configuration` — документ с `"issuer":"https://oauth.pluma.chat"`; `curl -sSI http://pluma.sckra.dev/x` — 301. Из России — проверка владельца 6 брифа 113.

Порядок шагов 2 и 3 обязателен: сначала узел направляет новые имена домой, потом дома пересоздаётся `web`. Первая выкладка зеркал (2026-11-18) шла в обратном порядке: Caddy дома начал выпуск сертификатов раньше, чем узел пропускал имена, проверка Let's Encrypt не прошла, а повторы Caddy после этого шли только к тестовому центру Let's Encrypt (staging), который запись CAA `sckra.dev` не разрешает, — зеркала остались без сертификатов. Восстановление: убедиться, что `curl -sSI http://<имя>/x` с внешней машины доходит до дома (301), и перезапустить `web` (со всеми оверлеями хоста); Caddy заново идёт к основному центру и выпускает сертификаты.

Ограничения: страницы сайта и провайдера сами ссылаются на `pluma.chat` там, где адрес задан в их коде (кнопки установки берут адрес из `releases.json`, сборка сайта принимает только адреса `https://pluma.chat/dl/…`; аватары и ссылка в приложение на странице входа) — это правки `pluma-site` и links, вне этой части задачи 113 (открытый вопрос; для кнопок сайта — также бриф 115, «Открытые вопросы»).

## Выпуски (задача 115)
Desktop из России обновляется с узла: bff даёт ему `autoupdate_url_prefix = https://r1.sckra.dev/dl/desktop` (`PLUMA_RELEASES_FILES_ROUTES=RU,BY=https://r1.sckra.dev` дома, `releases.md`, «Раздача через CDN»). nginx Pluma отдаёт `/dl/` на имени выдачи 202:
- файл версии (`/dl/[desktop/]<платформа>/<версия>/<файл>`) — из своего кэша `pluma_releases` в `/var/cache/pluma-ru/releases`: куски по 8 МиБ, 6 ГБ, 30 дней без запросов, срок — `Cache-Control` сервиса (`immutable`, год); первый скачавший тянет файл домой один раз, остальные — с узла;
- остальное (`releases.json`, `desktop/current4`, переходы `latest`, `health`) — насквозь, без кэша: новый выпуск виден сразу; переход `latest` ведёт на путь того же имени `r1.sckra.dev`;
- только GET и HEAD, остальное — 403 (загрузка CI идёт на сайт, не сюда).

Домой — тем же туннелем на имя источника CDN (`PLUMA_CDN_ORIGIN_DOMAIN`, `cdn-origin.caddy`: `/dl/*` → `releases` для GET и HEAD), без `X-Pluma-Origin-Auth` (файлы публичные; значение доступа уходит только с шифром) и без заголовков клиента, кроме диапазона. Новых переменных узла нет: включается выкладкой этой версии; клиенты идут сюда, только когда дома задан `PLUMA_RELEASES_FILES_ROUTES`. Порядок — сначала дом (иначе источник отвечает на `/dl/` как bff, 403), потом узел.

Проверка: `curl -sSI https://r1.sckra.dev/dl/desktop/current4` — 200, `Cache-Control: no-cache`; файл версии из `current4` (`https://r1.sckra.dev/dl/desktop<link>`) — 200, `immutable`; `du -sh /var/cache/pluma-ru/releases` после первого скачивания растёт, после второго — нет. Метрика `pluma_ru_cache_bytes` и алерт `RuNodeCacheOverLimit` — только про кэш шифра; кэш выпусков покрывают общие алерты диска. Очистить: `systemctl stop pluma-ru-nginx; rm -rf /var/cache/pluma-ru/releases/*; systemctl start pluma-ru-nginx`.

## Труба групповых звонков (задача 116)
Групповые звонки клиентов не используют TURN, а адрес SFU на Hetzner из России закрыт. Поэтому ответ входа клиента из России называет адрес узла, и nginx Pluma передаёт его UDP и ICE-TCP на адрес SFU (`stream.conf.tmpl` → `/etc/pluma-ru/stream.conf`, включается из `nginx.conf`; модуль stream — `load_module` по пути пакета). Что делать дома и на краю — `docs/ops/group-calls.md`, «Звонки из России»; порядок: узел → край → дом.

- Слушает `PLUMA_RU_PUBLIC_IPV4:20600` (UDP, `reuseport`) и `:20601` (TCP); апстрим — `PLUMA_RU_SFU_ADDR` на те же порты. Без `PLUMA_RU_SFU_ADDR` `stream.conf` — одна строка комментария.
- Каждый клиент — своя сессия nginx и свой исходящий порт: SFU видит клиентов узла по отдельности. `reuseport` держит датаграммы клиента в одном рабочем процессе (без него один клиент приходил к SFU с 8–12 портов — `TestNodeRelay`).
- UDP-сессия закрывается после 30 с тишины (SFU проверяет пару раз в 2 с); TCP — 5 с на соединение с SFU, 60 с простоя. Не больше 32 сессий на адрес клиента. Журнала доступа нет.
- Узел ничего не расшифровывает: внутри DTLS-SRTP между клиентом и SFU, а в нём кадры сквозного шифрования (ADR 0060). SFU отвечает только тем, кто прошёл проверку ICE с учётными данными звонка, поэтому трубу нельзя использовать как усилитель.
- Перезагрузка nginx Pluma (`deploy.sh apply` при смене любого его файла) открывает клиентам новые сессии: SFU переводит их на новую пару сам, звук может прерваться на несколько секунд (на живом nginx не замерено).
- **Экран Timeweb**: 20600/udp и 20601/tcp должны быть открыты (ufw на amf выключен).

Проверка после выкладки:
- `ss -lunp 'sport = :20600'` и `ss -ltnp 'sport = :20601'` — слушает `nginx` на `194.87.226.185`.
- `timeout 5 bash -c '</dev/tcp/195.201.252.126/20601' && echo ok` — SFU принимает соединение с узла.
- Из дома: `curl -s http://10.78.0.1:9100/metrics | grep -E 'pluma_ru_(sfu_upstream_up|unit_active)'` — оба 1.
- Звонок: проверки владельца брифа 116.

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
  curl -sS -D - -o /dev/null --resolve r1.sckra.dev:443:10.78.0.1 -H 'Range: bytes=0-99' '<URL канарейки>'   # 206, Content-Range: bytes 0-99/262144
  curl -sS -D - -o /dev/null --resolve r1.sckra.dev:443:10.78.0.1 '<URL из canary-url -expired>'          # 410
  ```
  Снаружи то же без `--resolve`.
- Порт 80: `curl -sSI http://r1.sckra.dev/x`, `http://ru.sckra.dev/x` и зеркал — 301 на `https://`.
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
Метрики textfile (`wgstat.sh`): `pluma_wg_up`, `pluma_wg_latest_handshake_seconds` (интерфейс `plumaru`), `pluma_ru_cache_bytes`, `pluma_ru_cache_max_bytes`, `pluma_ru_unit_active{unit="pluma-ru-nginx"}`, `pluma_ru_sfu_upstream_up` (только с трубой групповых звонков: SFU принимает TCP-соединение на 20601; UDP так не проверить — SFU не отвечает без учётных данных звонка), `pluma_ru_stats_timestamp_seconds`; остальное — стандартный node-exporter с меткой `node="amf"`.

Алерты — `teamgramd/deploy/prometheus/server/rules/ru-node.yml`:

| Алерт | Когда |
|---|---|
| `RuNodeDown` | Prometheus дома 5 минут не достаёт `10.78.0.1:9100` |
| `RuTunnelHandshakeStale` | рукопожатия с домом нет больше 3 минут или нет интерфейса `plumaru` |
| `RuNodeStatsStale` | файл textfile старше 5 минут (таймер `wgstat` встал) |
| `RuNodeCacheOverLimit` | кэш больше 110 % своего `max_size` 30 минут |
| `RuSfuRelayDown` | на узле с трубой групповых звонков 5 минут: SFU не принимает TCP-соединение с узла или `pluma-ru-nginx` не работает |

Диск и память узла покрывают общие `DiskSpaceLow`, `DiskSpaceCritical`, `MemoryLow` (`general.yml`); выдачу 202 — `CdnDeliveryUnhealthy`, `CdnBudget`, `CdnFallbackHigh` по `dc="202"` (`cdn.yml`).

## Неполадки
| Признак | Что смотреть |
|---|---|
| `RuNodeDown` | Дом: `ping -c3 10.78.0.1`, `sudo wg show plumaru`. amf: `systemctl status pluma-ru-node-exporter wg-quick@plumaru`. node-exporter слушает только `10.78.0.1` и ждёт интерфейс (`Restart=always`). |
| `RuTunnelHandshakeStale` | Обе стороны: `wg show plumaru` (latest handshake, transfer); MTU 1280 с обеих сторон; UDP 51820 до amf. |
| `RuNodeStatsStale` | `systemctl status pluma-ru-wgstat.timer pluma-ru-wgstat.service`, `journalctl -u pluma-ru-wgstat --since -1h`. |
| `RuNodeCacheOverLimit` | `journalctl -u pluma-ru-nginx --since -1h | grep -i cache`, `du -sh /var/cache/pluma-ru/files`. Кэш можно очистить: `systemctl stop pluma-ru-nginx; rm -rf /var/cache/pluma-ru/files/*; systemctl start pluma-ru-nginx`. |
| Выдача 202 нездорова (`CdnDeliveryUnhealthy{dc="202"}`) | URL канарейки вручную (раздел «Проверка»). 403 на верный URL — `CDN_RU_TOKEN_KEY` дома и на узле разные. 502/504 — источник: туннель, Caddy дома (`docker compose logs --tail 100 web`), `CDN_RU_ORIGIN_AUTH` дома и на узле (источник отвечает 403). Ошибки TLS к источнику — `journalctl -u pluma-ru-nginx`. |
| `RuSfuRelayDown` | `systemctl status pluma-ru-nginx`; `timeout 5 bash -c '</dev/tcp/<PLUMA_RU_SFU_ADDR>/20601'`; на краю `docker compose ps sfu` и экран Hetzner. Пока чинится — дома убрать `RU,BY` из `GROUPCALLS_ROUTES` (`docs/ops/group-calls.md`). |
| Россияне не входят в групповые звонки, `GroupCallsRelayNoCheck` | До SFU через трубу ничего не доходит: `ss -lunp 'sport = :20600'`, экран Timeweb для 20600/udp и 20601/tcp, `journalctl -u pluma-ru-nginx --since -1h`. |
| `deploy.sh`: `nginx -t failed on the new files` | Рабочие файлы не тронуты; ошибка выше в выводе. |
| `owner-nginx.sh install`: `check failed` | Строки `FAIL` в выводе; ничего не менялось. Если карта владельца устроена иначе (заголовок не на одной строке, несколько карт) — строку `include` добавить руками после согласования с владельцем. |
| Сайт владельца сломан после `install` | `owner-nginx.sh remove`; копии `sni.conf` — `/var/lib/pluma-ru/owner-nginx-backup/`. |

## Откат и выключение
- Пользователи из России без узла: дома убрать `RU` из `CDN_ROUTES` и `EDGE_ROUTES`, деплой — все идут по записи `*` (Gcore и прямой путь).
- nginx владельца: `owner-nginx.sh remove`.
- Службы узла: `systemctl disable --now pluma-ru-nginx pluma-ru-turn pluma-ru-node-exporter pluma-ru-wgstat.timer pluma-ru-certbot.timer`.
- TURN узла из списка звонков: убрать запись `@ru` из `CALLS_ICE_SERVERS` дома, деплой.
- Труба групповых звонков: дома убрать `RU,BY` из `GROUPCALLS_ROUTES` и перезапустить bff; затем убрать `PLUMA_RU_SFU_ADDR` из `/etc/pluma-ru/env` и выложить узел.

## Что видит узел и его хостер
Адреса пользователей, которые ходят через узел, время и объёмы; имена `ru`, `r1` и зеркал в SNI; шифр файлов в кэше (до 36 ч без запросов) и диапазоны; какие публичные установщики и обновления приложений скачивают через `r1` (файлы выпусков в кэше, задача 115); пакеты SRTP звонков через TURN и групповых звонков через трубу; внешний адрес домашнего роутера (источник WireGuard). Не видит: содержимое MTProto (TLS края заканчивается дома), ключи и хэши файлов, голос и видео звонков. Секреты на узле — только его собственные: ключ подписи и значение доступа выдачи 202, секрет TURN узла.

## Не проверено
- Отвечает ли Caddy на TLS-ALPN-01 за PROXY на `:8443`; HTTP-01 через порт 80 (`pluma-acme-http.conf`) работает независимо.
- Наследование CAA (RFC 8659 — по памяти; проверка владельца 7).
- Вывод `certbot show_account` (строка `Account URL:`), по которой `cert-register` печатает запись CAA, — по памяти. Если строки нет, `cert-register` печатает только заголовок: URL аккаунта тогда взять из `certbot show_account --config-dir /etc/pluma-ru/letsencrypt` глазами.
- Работа юнитов под systemd на самом amf: в тесте юниты проверены `systemd-analyze verify`, запуск — только при установке.
- Память nginx, coturn и node-exporter на узле — замерить после установки.
- Труба групповых звонков (задача 116): длинный поток amf → Hetzner, нагрузка UDP на nginx 2 vCPU, полоса и тариф Timeweb, экран Timeweb для 20600/udp и 20601/tcp, белые списки мобильных сетей.
