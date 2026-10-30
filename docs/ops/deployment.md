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
4. Push — `push.md`, алерты — `monitoring.md`, бэкапы — `backup.md`, разделы «Первый запуск». База GeoIP — раздел «GeoIP (DB-IP Lite)» ниже.
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
   - push, алерты и бэкап — проверками из `push.md`, `monitoring.md`, `backup.md`;
   - GeoIP — проверкой из раздела «GeoIP (DB-IP Lite)».

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

## GeoIP (DB-IP Lite)

Место сеанса («Prague, Czechia») authsession находит по IP-адресу в базе GeoIP. Место показывают «Устройства» (`account.getAuthorizations`) и сообщение о новом входе. База — бесплатная **IP to City Lite** от DB-IP в формате MMDB. Решение владельца — пробно (задача 017).

- **Где лежит.** На сервере — `/opt/pluma/geoip/dbip-city-lite.mmdb`, то есть каталог `geoip/` в корне клона.
  - `docker-compose.yaml` монтирует этот каталог в authsession только для чтения как `/app/geoip`. Путь к файлу — ключ `GeoIPDatabase` в `teamgramd/etc2/authsession.yaml`.
  - Без Docker используется тот же `geoip/` в корне репозитория (`teamgramd/etc/authsession.yaml`).
  - В git и в образ файл не попадает (`.gitignore`, `.dockerignore`). В бэкап тоже: его можно скачать заново.
- **Без базы** authsession работает. При старте он пишет в лог ошибку `geoip: cannot use the database <путь>, every place is UNKNOWN: <причина>`, и место у всех сеансов — `UNKNOWN`. Пустой `GeoIPDatabase` выключает GeoIP, тогда в логе строка уровня info.
- **Откуда.** Страница загрузки — https://db-ip.com/db/download/ip-to-city-lite, файл — `https://download.db-ip.com/free/dbip-city-lite-ГГГГ-ММ.mmdb.gz`, аккаунт не нужен. Размер, MD5SUM и SHA1SUM на странице относятся к распакованному файлу. Выпуск 2026-10: 126 998 165 байт, SHA1 `df17bd24390108ed1d24dcb7718dc60f6b9abd4f`; сжатый файл — 60 МБ.
- **Формат.** По странице https://db-ip.com/db/format/ip-to-city-lite/mmdb.html файл соответствует спецификации MMDB 2.0, а схема повторяет привычную для читателей MMDB: `city.names`, `country.names`, `country.iso_code`. Читатель authsession (`github.com/oschwald/geoip2-golang` v1.8.0, `reader.go:285`) открывает тип `DBIP-City-Lite` как базу городов.
  - Проверено 2026-10-29 на выпуске 2026-10 через код authsession: 8.8.8.8 → «Mountain View, United States», 1.1.1.1 → «Sydney, Australia», резолверы CZ.NIC 193.17.47.1 и 2001:148f:ffff::1 → «Prague, Czechia», 10.0.0.1 → `UNKNOWN`.
  - Lite-база местами даёт район вместо города: 195.113.89.54 → «Old Town, Czechia».

### Установка и ежемесячное обновление

DB-IP выпускает базу раз в месяц: выпуск 2026-10 датирован 1 октября. Обновлять раз в месяц, в первые дни месяца:

```bash
cd /opt/pluma
teamgramd/deploy/geoip/update-dbip.sh                     # выпуск текущего месяца (UTC)
teamgramd/deploy/geoip/update-dbip.sh 2026-10 <SHA1SUM>   # или конкретный, со сверкой SHA1SUM со страницы
docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml restart authsession
```

- **Что делает скрипт.** Скачивает выпуск во временный каталог внутри `geoip/` и проверяет его:
  - целостность gzip;
  - размер не меньше 64 МБ;
  - метаданные MMDB с типом `DBIP-City-Lite`;
  - SHA1, если он задан.

  Только после этого скрипт заменяет файл переименованием. При любой ошибке остаётся прежняя база, в том числе когда выпуск месяца ещё не вышел (404).
- **Не копировать файл поверх старого** (`cp`, распаковка на место). authsession держит открытую базу отображённой в память, и запись в этот файл ломает работающий сервис. Переименование безопасно.
- **Перезапуск.** authsession читает базу только при старте, поэтому после обновления его перезапускают. Пока он перезапускается, запросы к нему завершаются ошибкой, поэтому обновлять лучше, когда пользователей мало.

### Лицензия и атрибуция — открытый вопрос

- **Условия DB-IP.** По странице загрузки база распространяется по лицензии Creative Commons Attribution 4.0. Использовать её можно, если указать DB-IP.com как источник данных. Веб-приложение должно ставить ссылку на db-ip.com на страницах, где показаны результаты базы; DB-IP предлагает для этого ссылку «IP Geolocation by DB-IP».
- **Согласие с условиями.** На странице загрузки стоит флажок «I agree with the licensing terms». Скачивание скриптом по прямой ссылке этот шаг не заменяет: условия принимает владелец.
- **Решение владельца (2026-10-29).** DB-IP Lite используется пробно. Ссылку на DB-IP в приложениях пока не показывать, до его утверждения.
- **Что открыто.** Пока атрибуции в приложениях нет, использование не соответствует этим условиям. До конца пробного периода нужно выбрать одно из двух:
  - показать атрибуцию в клиентах: ключом строки во всех встроенных языках, место выбирает владелец;
  - перейти на коммерческую базу DB-IP. По той же странице ей атрибуция не нужна.

### Проверка

1. В логе authsession после старта есть строка `geoip: database /app/geoip/dbip-city-lite.mmdb, DBIP-City-Lite built 2026-10-01` (дата — дата выпуска):
   ```bash
   docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml logs authsession | grep geoip
   ```
   Строка `geoip: cannot use the database …` значит, что файла нет или он не тот.

   Без Docker (`teamgramd/etc`, `Log.Mode: file`) этой строки нет в файлах `teamgramd/logs/authsession/`. authsession открывает базу раньше, чем go-zero настраивает логгер по конфигу: база открывается в `svc.NewServiceContext` → `dao.New`, а логгер — в `zrpc.NewServer` (`c.SetUp()`), который вызывается после. До настройки go-zero пишет в стандартный вывод, ошибки — в stderr, а `teamgramd/bin/runall2.sh` направляет оба потока authsession в `teamgramd/logs/authsession.log`. Путь в строке — `../../geoip/dbip-city-lite.mmdb`:
   ```bash
   grep geoip teamgramd/logs/authsession.log
   ```
2. В клиенте «Настройки → Устройства» (`account.getAuthorizations`) у сеансов место «Город, Страна» вместо `UNKNOWN`. То же место — в сообщении о новом входе.
3. Если база открылась, а у всех сеансов `UNKNOWN`, шлюз видит не адрес клиента. Пример — IPv6-клиенты за адресом моста Docker: см. комментарий к портам gnetway в `docker-compose.yaml` и `security-setup.md` §3.

## Обновление

1. Дождаться зелёного запуска `images` для нужного коммита `master`.
2. Получить код этого коммита и проверить в STATUS, нет ли ручных шагов при обновлении:
   ```bash
   cd /opt/pluma && git fetch && git checkout <коммит>
   ```
3. Заменить в `.env` тег трёх образов на этот коммит.
4. Скачать образы и выполнить `up -d --no-build` обеих команд, как при первом запуске (шаг 5). Миграции применяет `migrate`.

Откат — то же самое с прежним коммитом и тегом. Схема базы назад не откатывается, поэтому откат возможен только к версии, совместимой с уже применёнными миграциями.
