# Развёртывание на сервере

Как Pluma стоит на сервере, как его запустить впервые и как обновлять. Секреты и ключ сервера — `security-setup.md`, алерты — `monitoring.md`, бэкапы — `backup.md`, push — `push.md`.

## Схема

- **Код.** Клон репозитория в `/opt/pluma` на том коммите, который запущен. Из него берутся compose-файлы, конфиги и миграции.
- **Образы.** Сервер их не собирает. Workflow `images` собирает для каждого коммита `master` три образа: `ghcr.io/sckraaa/pluma-server`, `pluma-alertpush` и `pluma-backup`. Тег у всех — полный SHA коммита. В `.env` сервера записаны образы его коммита (`TEAMGRAM_IMAGE`, `PLUMA_ALERTPUSH_IMAGE`, `PLUMA_BACKUP_IMAGE`). Без них compose собирает образы на месте: так работает машина разработки.
- **Свой адрес.** У Pluma может быть IPv4 рядом с основным адресом сервера (`PLUMA_PUBLIC_IPV4`, `docker-compose.public-ip.yaml`):
  - шлюз публикует порты только на нём;
  - исходящий трафик всех контейнеров уходит с него, потому что мост `teamgram_net` подменяет адрес на этот (SNAT) вместо основного.

  С основного адреса идёт только скачивание образов демоном Docker. Этот адрес вшит в клиентов. Floating IP можно переназначить на другой сервер той же сетевой зоны, и клиенты продолжат работать без пересборки.

На таком сервере обе команды compose получают второй файл:

```bash
docker compose -f docker-compose-env.yaml -f docker-compose.public-ip.yaml …   # инфраструктура
docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml …       # сервисы
```

Сеть создаёт та команда, что запустилась первой, поэтому файл нужен обеим.

## Адрес Pluma (Floating IP Hetzner)

1. Hetzner Console → Floating IPs: создать IPv4 в сетевой зоне сервера и назначить его серверу.
2. На сервере сделать адрес постоянным. Сеть при этом не перезапускается:
   ```bash
   cat >/etc/netplan/60-pluma-floating-ip.yaml <<'EOF'
   network:
     version: 2
     ethernets:
       eth0:
         addresses:
           - <адрес>/32
   EOF
   chmod 600 /etc/netplan/60-pluma-floating-ip.yaml
   netplan generate                     # проверка; адрес войдёт в силу при загрузке
   ip addr add <адрес>/32 dev eth0      # и сейчас
   ```
3. В `.env` указать `PLUMA_PUBLIC_IPV4=<адрес>`.

## Первый запуск

1. Docker с плагином compose.
2. Получить код:
   ```bash
   git clone https://github.com/sckraaa/pluma.git /opt/pluma
   cd /opt/pluma && git checkout <коммит>
   ```
3. Ключ сервера и `.env` — `security-setup.md` §1–2. Go на сервере не нужен: `genserverkey` собирается на другой машине и копируется на сервер.
   ```bash
   GOOS=linux GOARCH=amd64 go build -o genserverkey ./tools/genserverkey
   ```
4. Push — `push.md`, алерты — `monitoring.md`, бэкапы — `backup.md`, разделы «Первый запуск».
5. В `.env` записать образы коммита и запустить:
   ```bash
   docker compose -f docker-compose-env.yaml -f docker-compose.public-ip.yaml pull
   docker compose -f docker-compose-env.yaml -f docker-compose.public-ip.yaml up -d --no-build
   docker compose -f docker-compose-env.yaml -f docker-compose.public-ip.yaml ps    # healthy: mysql, redis, kafka, etcd
   docker pull "$(sed -n 's/^TEAMGRAM_IMAGE=//p' .env)"
   docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml up -d --no-build
   ```
   Образ сервисов скачивается `docker pull`, а не `compose pull`: у него `pull_policy: never`, потому что на машине разработки его собирает сервис `idgen`.
   Схему чистой базы создаёт сервис `migrate`. Ручные миграции из STATUS нужны только при обновлении уже существующей базы.
6. Поставить фото служебному аккаунту «Pluma» (777000). Команда загружает встроенное фото Pluma так же, как клиент ставит своё: файл уходит в dfs, media делает из него фото, biz рассылает `updateUser` тем, у кого есть чат с аккаунтом. Достаточно одного раза: повторный запуск добавит фото в альбом аккаунта.
   ```bash
   docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml exec bff ./pluma-admin -f ../etc2/bff.yaml service-photo
   ```
7. Проверить:
   - `docker compose … ps`: все сервисы healthy;
   - `ss -tlnp | grep -E ':(10443|5222|11443) '`: порты слушает только адрес Pluma;
   - `iptables -t nat -S POSTROUTING | grep 172.20`: стоит `SNAT --to-source <адрес>`, а не `MASQUERADE`;
   - push, алерты и бэкап — проверками из `push.md`, `monitoring.md`, `backup.md`.

## Веб-клиент

Web K раздаёт с адреса Pluma контейнер `web` (`docker-compose.web.yaml`): Caddy с сайтом из образа `pluma-web`.
- **TLS.** Сертификаты Let's Encrypt Caddy получает и продлевает сам. Нужны A-записи всех имён на адрес Pluma и свободные порты 80 и 443 на этом адресе.
- **WebSocket.** `/apiws` Caddy передаёт на отдельный слушатель шлюза `11444` с PROXY protocol v1. Так шлюз видит настоящий адрес каждого веб-клиента, и лимиты входа считаются по нему. Соединение без заголовка PROXY этот слушатель закрывает. Порт `11444` не публикуется. Собственный WebSocket-порт шлюза `11443` при этом файле тоже не публикуется.
- **Имена.** `PLUMA_WEB_DOMAIN` — имя веб-клиента. Остальные имена сайта из `PLUMA_SITE_DOMAINS` перенаправляют на него.

Порядок:
1. В `pluma-web` задать переменные `PLUMA_SERVER_URL=wss://<PLUMA_WEB_DOMAIN>/apiws` и `PLUMA_RSA_PUBLIC_KEY`. Сборка `main` под сервер публикует образ `ghcr.io/sckraaa/pluma-web:<коммит pluma-web>`.
2. В `.env` указать `PLUMA_WEB_IMAGE`, `PLUMA_WEB_DOMAIN` и `PLUMA_SITE_DOMAINS`.
3. Команда сервисов получает третий файл:
   ```bash
   docker pull "$(sed -n 's/^PLUMA_WEB_IMAGE=//p' .env)"
   docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml -f docker-compose.web.yaml up -d --no-build
   ```
4. Проверить: `https://<PLUMA_WEB_DOMAIN>/` открывается с сертификатом Let's Encrypt, вход проходит, `ss -tlnp | grep 11443` пуст.

## Обновление

1. Дождаться зелёного запуска `images` для нужного коммита `master`.
2. Получить код этого коммита и проверить в STATUS, нет ли ручных шагов при обновлении:
   ```bash
   cd /opt/pluma && git fetch && git checkout <коммит>
   ```
3. Заменить в `.env` тег трёх образов на этот коммит.
4. Скачать образы и выполнить `up -d --no-build` обеих команд, как при первом запуске (шаг 5). Миграции применяет `migrate`.

Откат — то же самое с прежним коммитом и тегом. Схема базы назад не откатывается, поэтому откат возможен только к версии, совместимой с уже применёнными миграциями.
