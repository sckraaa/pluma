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
- `pluma.slice` из репозитория плюс `/etc/systemd/system/pluma.slice.d/10-home.conf`: `MemoryMax=18G`, `MemoryLow=6G`.
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
