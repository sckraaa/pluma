# Домашний сервер и край на VPS

Решение — ADR 0061, развитие ADR 0057. Ядро Pluma работает дома, на VPS только край: публичный адрес 195.201.252.126, туннель домой, SFU групповых звонков, TURN звонков один на один, пересылка к сайтам владельца.

```
клиенты ──► 195.201.252.126 (VPS) ──DNAT, адрес клиента сохраняется──► WireGuard ──► дом 10.77.0.2: шлюз, Caddy, всё ядро
                 │  SFU 20600/udp 20601/tcp, TURN 3478, 49160-49659/udp — на VPS, только шифр
                 └─ исходящий трафик дома ◄── SNAT 195.201.252.126 ◄── туннель ◄── контейнеры дома
```

**Не трогать на VPS ни в каком виде:** адрес 159.69.215.232 и всё, что на нём; `lawyer-app`, nginx `neo-tunnel`, Tailscale владельца, контейнеры `neo-*` и `learnshell-*`.

## Адреса и доступ
- **Дом:**
  - в локальной сети `192.168.0.139` (резервирование DHCP в роутере, MAC `d0:ad:08:a3:ff:53`);
  - в туннеле `10.77.0.2`;
  - вход `ssh sckr_a@…` только по ключу, `sudo` без пароля.
- **VPS в туннеле:** `10.77.0.1`.
- **Вне дома:** `ssh -J neo_old sckr_a@10.77.0.2`.
- **Псевдонимы на Mac:** `pluma_home` (через VPS) и `pluma_lan`. Деплой ядра: `ssh pluma_home "sudo NEW=<sha> bash -s" < ~/pluma-work/bin/pluma-deploy.sh`.
- **Дом забирает данные с VPS** ключом `/root/.ssh/pluma_edge`. На VPS этот ключ разрешён только с `10.77.0.2` и только на чтение (`rrsync -ro`). После переезда строку `pluma-home-pull` удалить из `/root/.ssh/authorized_keys` на VPS.

## Туннель (`teamgramd/deploy/edge/tunnel.sh`)
Один раз на каждой стороне, от root:

```bash
tunnel.sh key                                  # публичный ключ этой машины
tunnel.sh vps  <ключ дома> 195.201.252.126     # на VPS
tunnel.sh home <ключ VPS> 195.201.252.126      # дома
```

Что создаётся:
- `/etc/wireguard/pluma.{conf,key,env}` с правами 600;
- службы `wg-quick@pluma` и `pluma-tunnel-rules`;
- дома — drop-in, чтобы Docker стартовал после туннеля (порты публикуются на `10.77.0.2`);
- на VPS — правило ufw для 51820/udp на адресе Pluma.

Проверка:

```bash
wg show pluma                       # latest handshake — секунды назад
ip rule; ip route show table 77     # дома: pref 100, 110 и default dev pluma
docker run --rm --network <сеть 172.20.0.0/16> alpine wget -qO- https://ifconfig.me   # 195.201.252.126
```

## Второй туннель: российский узел amf (задача 109)
Решение — ADR 0072 §8. Узел amf (194.87.226.185, Timeweb, Москва) несёт для пользователей из России выдачу файлов 202, край `ru.pluma.chat` и TURN; что стоит на самом узле — `ru-node.md`, края — `edge.md`. Дом связан с ним вторым каналом WireGuard, независимым от Hetzner:

```
дом 10.78.0.2 ──WireGuard plumaru, дом звонит, MTU 1280──► amf 10.78.0.1, 51820/udp
amf nginx ──с 10.78.0.1──► дом 10.78.0.2:80, :443, :8443 (Caddy, docker-compose.ru.yaml)
дом: bff (проба выдачи 202), Prometheus ──► 10.78.0.1:443, :9100
```

- **Адреса:** интерфейс `plumaru`, сеть `10.78.0.0/24`, amf — `10.78.0.1`, дом — `10.78.0.2`. Звонит дом (`PersistentKeepalive = 25`), роутер дома не трогается.
- **MTU 1280 с обеих сторон.** Замер 2026-11-17: при 1420 дом → amf не проходит ничего (чёрная дыра PMTU), при 1280 — около 50 Мбит/с; amf → дом — около 109 Мбит/с. Плюс `TCPMSS --clamp-mss-to-pmtu`: дома в `mangle FORWARD` в обе стороны (`-o` и `-i plumaru`), на amf — в `mangle OUTPUT`.
- **Без пересылки и без своей таблицы.** amf не делает DNAT: все соединения домой открывает сам (nginx, с `10.78.0.1`), адрес клиента идёт внутри PROXY v1. Поэтому `Table = off`, новых `ip rule` нет. Ответы контейнеров на `10.78.0.1` уходят по уже существующему правилу Hetzner `pref 100 lookup main suppress_prefixlength 0` (в `main` есть маршрут `10.78.0.0/24 dev plumaru`) и до `pref 110` (всё остальное — в Hetzner) не доходят.
- **`AllowedIPs`:** дома для amf — `10.78.0.1/32`; на amf для дома — `10.78.0.2/32, 10.77.0.2/32`. Второй адрес — источник исходящих соединений контейнеров дома (SNAT моста на `host_ipv4`, `docker-compose.public-ip.yaml`): bff и Prometheus ходят на amf именно с него. Других адресов дома и Hetzner amf не получает.
- **ufw дома:** первым правилом `deny in on plumaru` (выше `22/tcp`) — amf не видит SSH и прочие службы хоста. Порты Docker на `10.78.0.2` идут мимо ufw через DNAT Docker; так и нужно.
- **Порядок старта:** Docker публикует порты на `10.78.0.2`, поэтому адрес должен существовать до dockerd: drop-in `/etc/systemd/system/docker.service.d/10-pluma-tunnel-ru.conf` (`Wants=`/`After=wg-quick@plumaru`). Интерфейс поднимается без ответа amf: выключенный amf не мешает дому стартовать. Если `wg-quick@plumaru` не поднялся вовсе, Docker не опубликует порты `docker-compose.ru.yaml` и `web` не стартует — поэтому выкладка с узлом сначала проверяет интерфейс (`deployment.md`, «Края и российский узел»).
- **Файлы:** `/etc/wireguard/plumaru.{conf,key,env}` с правами 600 (AppArmor Ubuntu не даёт `wg` читать `/root`) и служба правил `pluma-tunnel-ru-rules` (правила — отдельной службой, не в `PostUp`: AppArmor не даёт `wg-quick` запускать скрипты). Drop-in `systemd-networkd` `ManageForeignRoutingPolicyRules=no` уже есть (раздел «Дом: хост»).

Один раз на каждой стороне, от root; приватные ключи не печатаются, на экран выходит только публичный:

```bash
tunnel.sh --link ru key                                     # amf, затем дом: публичный ключ машины
tunnel.sh --link ru node <ключ дома> 194.87.226.185         # на amf
tunnel.sh --link ru home <ключ amf> 194.87.226.185          # дома
```

Посмотреть заранее, что скрипт сделает, ничего не меняя: `tunnel.sh --link ru --dry-run /tmp/ru-check home <ключ amf> 194.87.226.185` — файлы ложатся под `/tmp/ru-check`, команды — в `/tmp/ru-check/commands`. В каталоге остаётся сгенерированный приватный ключ: удалить его сразу после просмотра (`rm -rf /tmp/ru-check`).

**Первый туннель после установки второго.** `tunnel.sh … home` ставит себя в `/usr/local/sbin/pluma-tunnel`, а этот же файл запускает служба правил Hetzner `pluma-tunnel-rules`. Без `--link` скрипт ведёт себя как раньше (канал `hetzner` байт в байт, эталоны `teamgramd/deploy/edge/testdata/hetzner-*`), но после установки проверить и его:

```bash
systemctl restart pluma-tunnel-rules && systemctl is-active pluma-tunnel-rules   # active; правила снимаются и ставятся заново — в тихое время
ip rule                                    # как до установки: pref 100 и 110, новых правил нет
grep ExecStart /etc/systemd/system/pluma-tunnel-rules.service                    # без --link
```

Проверка второго туннеля:

```bash
wg show plumaru                                  # обе стороны: latest handshake — секунды назад
ping -M do -s 1252 -c 5 10.78.0.1                # дом: без потерь (1252 + 28 = 1280)
ping -M do -s 1253 -c 1 10.78.0.1                # дом: message too long
ip route get 10.78.0.1                           # дом: dev plumaru
iptables -t mangle -S FORWARD | grep plumaru     # дом: два TCPMSS (-o и -i)
ufw status numbered | head -5                    # дом: первым — Anywhere on plumaru DENY IN
systemctl list-dependencies --after docker.service | grep plumaru   # дом: wg-quick@plumaru
ss -ltn | grep 10.78.0.2                         # дом, стек с docker-compose.ru.yaml: 80, 443, 8443
docker run --rm --network <сеть 172.20.0.0/16> alpine wget -qO- -T5 http://10.78.0.1:9100/metrics | head -1   # после deploy.sh узла: ответ есть
```

С amf: `nc -zv -w3 10.78.0.2 22` — не соединяется (ufw), `curl -sI http://10.78.0.2/` — отвечает Caddy. Исходящий трафик дома по-прежнему выходит с адреса Hetzner (проверка `ifconfig.me` первого туннеля).

Мониторинг: цель Prometheus `/opt/pluma/prometheus-targets/node-ru.json` — `[{"targets":["10.78.0.1:9100"],"labels":{"node":"amf"}}]`; метрика рукопожатия `pluma_wg_latest_handshake_seconds` узла и алерты `teamgramd/deploy/prometheus/server/rules/ru-node.yml` (узел недоступен, рукопожатие старше 3 минут).

Откат туннеля — только после отката узла (`ru-node.md`, «Откат и выключение»: пока `RU` есть в `CDN_ROUTES` и `EDGE_ROUTES`, клиентов из России ведут на узел):
1. Убрать `docker-compose.ru.yaml` из команды выкладки и выкатить: без туннеля `web` с ним не стартует.
2. На обеих сторонах: `systemctl disable --now pluma-tunnel-ru-rules wg-quick@plumaru`.
3. Дома: удалить `/etc/systemd/system/docker.service.d/10-pluma-tunnel-ru.conf`, `systemctl daemon-reload`; правило `plumaru` — `ufw status numbered`, затем `ufw delete <номер>` (и его v6-пару).

## Дом: хост
- Ubuntu 26.04, LVM на весь диск. Автообновления безопасности включены. ufw открывает только 22/tcp; порты Docker идут мимо ufw и опубликованы только на `10.77.0.2`.
- Docker из `download.docker.com`. `/etc/docker/daemon.json` настраивает:
  - логи `local` по 20 МБ × 5;
  - `live-restore`: перезапуск dockerd не останавливает контейнеры;
  - DNS контейнеров `172.20.0.1`. Это дополнительный адрес systemd-resolved: `/etc/systemd/resolved.conf.d/10-pluma-dot.conf`, резолвер слушает его ещё до появления моста.
- **DNS хоста и контейнеров — только DNS over TLS** к 1.1.1.1/1.0.0.1/9.9.9.9 с проверкой имени сертификата.
  - Резолверы из DHCP и RA не берутся: `/etc/systemd/network/10-netplan-eno1.network.d/10-pluma-dns.conf`.
  - ufw пускает к `172.20.0.1:53` только из `172.16.0.0/12`.
  - Провайдер видит TLS до Cloudflare, VPS не видит запросов DNS вовсе.
  - Новый контейнер берёт этот DNS при создании; уже созданный — только после `--force-recreate`.
- **`/etc/systemd/networkd.conf.d/10-pluma-tunnel.conf`** (`ManageForeignRoutingPolicyRules=no`, `ManageForeignRoutes=no`) — обязательно. Без него любой `networkctl reload` или перезапуск networkd (в том числе при обновлении системы) удаляет правила туннеля, и ответы клиентам уходят мимо него. Так было 2026-11-14, простой около двух минут.
- `pluma.slice` из репозитория плюс `/etc/systemd/system/pluma.slice.d/10-home.conf`: `MemoryMax=10G`, `MemoryLow=6G` (Pluma занимает около 3,6 ГБ, замер 2026-11-14).
- **Медиасервер владельца** (Jellyfin, торренты) на том же хосте:
  - `/opt/media`, диск `/srv/media`;
  - `media.slice` до 12 ГБ;
  - сеть `172.30.40.0/24` — вне туннеля, его трафик идёт напрямую;
  - свой блок в `/etc/ufw/after.rules` и `DOCKER-USER`, его сохранять.
- Перезапуск Docker, перезагрузка хоста и правки ufw затрагивают оба стека.
- `ssh.service.d/oom.conf`.
- `/opt/pluma` — клон репозитория. Его `.env` отличается от VPS:
  - `PLUMA_PUBLIC_IPV4=10.77.0.2`;
  - `GROUPCALLS_SFU_ENDPOINT=10.77.0.1:20500`;
  - нет `TURN_PUBLIC_IPV4` и `SFU_PUBLIC_IPV4`: они нужны только краю.
- Цели Prometheus на краю: `/opt/pluma/prometheus-targets/sfu.json` и `node.json`, например `[{"targets":["10.77.0.1:20501"]}]`.
- **Карты в геопозициях** (задача 100, ADR 0063, `docs/ops/maps.md`):
  - сервис `maps` (tileserver-gl) — только дома, на краю его нет; образ `pluma-maps:local` собирается здесь же (`… -f docker-compose.maps.yaml build maps`);
  - данные — `/opt/pluma-maps` (root, 0755, файлы 0644): `pluma-<дата>.pmtiles` около 103 ГБ и ссылка `pluma.pmtiles`; на время обновления — ещё столько же. Диск общий с медиасервером: `df -h /opt` перед вырезкой;
  - вырезка (`extract.sh`) держит около 10 ГБ памяти вне `pluma.slice`: перед ней `free -g`; при занятом медиасервере резать на другой машине и переключаться `--use`;
  - с `MAPS_URL=http://maps:8080` в `.env` любая команда compose, пересоздающая bff, включает `-f docker-compose.maps.yaml` (сеть `tiles_net`): `docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml -f docker-compose.maps.yaml up -d maps bff`. То же — в `pluma-deploy.sh`, иначе выкладка пересоздаст bff без карт;
  - `MAPS_COMPOSE` для `extract.sh` и `check.sh` — эта же команда.
  - публичная раздача тайлов для выбора места (задача 112, ADR 0070, `docs/ops/maps.md`, «Публичная раздача»): контейнер `maps-tiles` (тот же образ) только в `maps_public_net` вместе с Caddy; Caddy публикует его на имени `PLUMA_MAPS_DOMAIN` (предложение — `maps.pluma.chat`, A-запись на 195.201.252.126, как у `PLUMA_CDN_ORIGIN_DOMAIN`; 443 уже идёт домой DNAT, на VPS ничего нового). `.env` дома (только имена): `PLUMA_MAPS_DOMAIN`, `MAPS_PUBLIC_KEY`, `MAPS_PUBLIC_URL`; запуск — с наложениями сайта и карт вместе: `… -f docker-compose.web.yaml -f docker-compose.maps.yaml up -d maps-tiles web bff`.
- **HTTPS-CDN больших файлов** (задача 107, ADR 0071, `docs/ops/cdn.md`):
  - источник шифра — слушатель bff на 20480 только в `teamgram_net`, на хост не публикуется; Caddy дома публикует его на имени `PLUMA_CDN_ORIGIN_DOMAIN` (A-запись на 195.201.252.126, 443 уже идёт домой DNAT). На VPS ничего нового;
  - `.env` дома (только имена): `CDN_GCORE_BASE_URL`, `CDN_GCORE_TOKEN_KEY`, `CDN_GCORE_SIGNER`, `CDN_GCORE_MONTHLY_GB`, `CDN_ORIGIN_AUTH`, `PLUMA_CDN_ORIGIN_DOMAIN`, `CDN_ROUTES`, `CDN_URL_TTL`, `CDN_KINDS`; `CDN_DEV_ALLOW_HTTP`, `CDN_NODE_CONTROL` и `PLUMA_DC1_ADDRESSES` не задавать. Пустой `CDN_GCORE_BASE_URL` — CDN выключен;
  - проверки здоровья выдачи идут из bff наружу через туннель, как весь исходящий трафик дома;
  - Gcore тянет у дома каждый кусок 10 МБ один раз на точку за 36 ч простоя: на отдачу канала дома приходятся первые просмотры, повторные идут с точек Gcore.
- **Края клиентов и российский узел** (задача 109, ADR 0072, `docs/ops/edge.md`, `docs/ops/ru-node.md`):
  - край Gcore — сайт Caddy `edge-origin.caddy` на имени `PLUMA_EDGE_ORIGIN_DOMAIN` (A-запись на 195.201.252.126, как у `PLUMA_CDN_ORIGIN_DOMAIN`; 443 уже идёт домой DNAT, на VPS ничего нового); вход `Edge` шлюза — `gnetway:11445`, только сеть Docker;
  - край и выдача 202 узла amf — через второй туннель (раздел выше) и `docker-compose.ru.yaml`: Caddy публикует 80, 443 и 8443 на `10.78.0.2`. Сертификат `ru.pluma.chat` выпускает Caddy дома (HTTP-01 через порт 80 amf, CAA `pluma.chat` не меняются); сертификат `r1.pluma.chat` — certbot на amf под своим аккаунтом и своими CAA на этом имени (`ru-node.md`, шаг 4);
  - `.env` дома (только имена): `PLUMA_EDGE_ORIGIN_DOMAIN`, `PLUMA_EDGE_ORIGIN_AUTH`, `PLUMA_RU_EDGE_DOMAIN`, `PLUMA_RU_TUNNEL_IPV4=10.78.0.2`, `EDGE_WS_URLS`, `EDGE_ROUTES`, `EDGE_STICKY_S`, `CDN_RU_BASE_URL`, `CDN_RU_TOKEN_KEY`, `CDN_RU_ORIGIN_AUTH`, `CDN_RU_HEALTH_ADDR=10.78.0.1:443`, `CDN_RU_MONTHLY_GB`, `CDN_ROUTES` со страной, `CALLS_TURN_SECRETS`, `CALLS_ICE_SERVERS` с записью `@ru`; `CDN_RU_TOKEN_KEY`, `CDN_RU_ORIGIN_AUTH` и запись `ru=` из `CALLS_TURN_SECRETS` — те же в `/etc/pluma-ru/env` узла (перенос без печати — `ru-node.md`, «Переменные»);
  - с `PLUMA_RU_TUNNEL_IPV4` любая команда compose, пересоздающая `web`, включает `-f docker-compose.ru.yaml` после `docker-compose.web.yaml`, иначе Caddy потеряет порты туннеля; то же — в `pluma-deploy.sh`.
- Сайты владельца: `/srv/pluma-caddy-sites/*.caddy`. В `sckra.dev.caddy` upstream — `10.77.0.1:3000`, это `site-relay` края.

## VPS: край
Край живёт в `/opt/pluma-edge`, отдельно от проверки репозитория ядра. Там лежат:
- копии своих файлов из репозитория: `docker-compose.edge.yaml`, `teamgramd/deploy/{sfu,turn,edge}/…`;
- `.env` с правами 600:
  - `PLUMA_PUBLIC_IPV4=195.201.252.126`;
  - `PLUMA_SFU_IMAGE`;
  - `CALLS_TURN_SECRET` — тот же, что дома;
  - `PLUMA_SFU_CERTS=/opt/pluma-edge/sfu`;
  - `EDGE_SITE_RELAY=lawyer-app:3000`;
  - `COMPOSE_PROFILES=sites`;
  - `PLUMA_CGROUP_PARENT`.

```bash
cd /opt/pluma-edge && docker compose -p pluma-edge -f docker-compose.edge.yaml up -d
```

Обновление края до коммита (файлы из репозитория и образ SFU): `ssh neo_old 'NEW=<sha> bash -s' < ~/pluma-work/bin/pluma-edge-deploy.sh`.

После переезда на VPS не остаётся секретов ядра. С подтверждения владельца, когда дом проработает несколько дней, удаляются:
- `/opt/pluma/.env`, `.env.bak-*`, `secrets/`, `iosinstall/`, `releases/`, `groupcalls/`, `sfu/`, `data/`;
- остановленные контейнеры и тома ядра;
- том Hetzner.

Сертификаты управления SFU:
- на VPS: `control-certs.sh /opt/pluma-edge/sfu /root/groupcalls-certs 10.77.0.1`;
- `/root/groupcalls-certs` перенести домой в `/opt/pluma/groupcalls` с правами 0400 и удалить на VPS.

## Сертификаты и CAA
- **Caddy берёт сертификаты только у Let's Encrypt** (`acme_ca` в `Caddyfile`), под ACME-аккаунтом домашнего сервера `https://acme-v02.api.letsencrypt.org/acme/acct/3996877102`. Аккаунт создан 2026-11-14; прежний аккаунт и ключи сертификатов были на VPS, сертификаты отозваны (keyCompromise).
- **Записи CAA у `pluma.chat` и `sckra.dev`:**
  ```
  0 issue "letsencrypt.org; accounturi=https://acme-v02.api.letsencrypt.org/acme/acct/3996877102; validationmethods=http-01,tls-alpn-01"
  ```
  Тот, у кого адрес на входе (VPS, хостер), не выпустит сертификат на эти имена ни у другого CA, ни под другим аккаунтом.
  - `pluma.chat` — в Cloudflare. Universal SSL выключен: с ним Cloudflare сам добавляет скрытые записи CAA для своих CA. Зона без проксирования, поэтому Universal SSL не нужен.
  - `sckra.dev` — в Spaceship; значение там без пробелов после `;` (с пробелами Spaceship его не принимает).
  - Проверено 2026-11-14: `dig CAA` показывает только эти записи; `www.pluma.chat` и `sckra.dev` перевыпущены с ними.
- **Если аккаунт пропадёт** (новый `caddy_data`): Caddy создаст новый, выпуск упрётся в CAA. Сначала поменять `accounturi` в записях на новый URL из `caddy/acme/acme-v02.api.letsencrypt.org-directory/users/default/default.json`, поле `location`.

## Переключение
1. **Черновая копия на ходу** (дома, около 30 ГБ): `sudo pluma-seed`.
2. **Окно.** Предупредить пользователей.
3. **VPS:** остановить ядро без удаления сети (`pluma_teamgram_net` нужна `lawyer-app` и краю):
   ```bash
   cd /opt/pluma && docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml -f docker-compose.web.yaml -f docker-compose.iosinstall.yaml -f docker-compose.releases.yaml -f docker-compose.stt.yaml -f docker-compose.codes.yaml -f docker-compose.admin.yaml stop
   docker compose -f docker-compose-env.yaml -f docker-compose.public-ip.yaml stop
   ```
   `sfu` и `turn` в этой команде не останавливаются: их сменяет край в шаге 6.
4. **Дом:** `sudo pluma-seed` — досылает изменения, теперь копия согласована. Затем разложить её по местам (`pluma-seed` печатает размеры):
   - `/srv/pluma-seed/data/*` → `/opt/pluma/data/`;
   - тома → `/var/lib/docker/volumes/pluma_<имя>/_data`.
5. **Дом:** запустить ядро обычным деплоем (`pluma-deploy.sh` с текущим коммитом); `codes` и `admin` — по их инструкциям.
6. **VPS:**
   - `forward.sh on`;
   - остановить `sfu` и `turn` проекта ядра и сразу запустить край (`cd /opt/pluma-edge && docker compose -p pluma-edge -f docker-compose.edge.yaml up -d`): порты те же, звонки один на один прервутся на секунды;
   - убрать контейнеры ядра: `… rm -f`, той же командой, что в шаге 3.
7. **Таймер языковых пакетов** (`pluma-langpacks.timer`): выключить на VPS, включить дома вместе с `/opt/pluma/langpacks`.
8. **Проверка:**
   - вход и история в клиентах;
   - сайт, `/dl/`, веб-клиент;
   - звонок один на один и групповой;
   - `sckra.dev`;
   - Prometheus дома: цели `ops.sfu` и `node-exporter` края `up`.

## Откат
Пока дома не появились новые данные:
- VPS: `forward.sh off`, затем `up -d` ядра той же командой, что в шаге 3.

После того как пользователи писали домой, откат — это обратная копия: `pluma-seed` в другую сторону, с остановкой дома.
