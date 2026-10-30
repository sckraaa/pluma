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
4. Push — `push.md`, алерты — `monitoring.md`, бэкапы — `backup.md`, разделы «Первый запуск». База GeoIP — раздел «GeoIP» ниже: ключ MaxMind в `.env` и сервис `geoipupdate`.
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
   - GeoIP — проверкой из раздела «GeoIP».

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

## GeoIP

Место сеанса («Prague, Czechia») authsession находит по IP-адресу в базе GeoIP. Место показывают «Устройства» (`account.getAuthorizations`) и сообщение о новом входе. База — бесплатная **GeoLite City** от MaxMind в формате MMDB. Владелец выбрал её 2026-10-30, сравнение баз — в брифе 017, пункт «Выбор базы GeoIP» в «Ходе работы». До этого стояла DB-IP Lite.

- **Где лежит.** На сервере — `/opt/pluma/geoip/GeoLite2-City.mmdb`, то есть каталог `geoip/` в корне клона.
  - `docker-compose.yaml` монтирует этот каталог в authsession только для чтения как `/app/geoip`. Путь к файлу — ключ `GeoIPDatabase` в `teamgramd/etc2/authsession.yaml`.
  - Без Docker используется тот же `geoip/` в корне репозитория (`teamgramd/etc/authsession.yaml`).
  - В git, в образ и в бэкап файл не попадает (`.gitignore`, `.dockerignore`): его можно скачать заново, а раздавать его лицензия не разрешает.
  - **Не копировать файл поверх старого** (`cp`, распаковка на место). authsession держит открытую базу отображённой в память, и запись в этот файл ломает работающий сервис. Только переименование — так делает и geoipupdate.
- **Кто скачивает.** Сервис `geoipupdate` в `docker-compose-env.yaml`: официальный образ MaxMind `ghcr.io/maxmind/geoipupdate` v8.0.0, закреплённый по digest.
  - Сервис проверяет новый выпуск каждые 12 часов. MaxMind выпускает GeoLite City по вторникам и пятницам (support.maxmind.com, «Download and update MaxMind databases»).
  - Проверка без загрузки не входит в лимит 30 загрузок в сутки (там же).
  - Новый выпуск geoipupdate сверяет по MD5 с данными MaxMind, пишет во временный файл и переименовывает поверх старого (maxmind/geoipupdate v8.0.0, коммит `07618e0e7c`: `client/download.go`, `internal/geoipupdate/database/local_file_writer.go`).
  - **Неверный или отозванный ключ.** geoipupdate сразу завершается, Docker перезапускает его не реже раза в минуту, и каждый запуск — неудачный запрос к MaxMind. MaxMind предупреждает, что может ограничить такие запросы (dev.maxmind.com/geoip/updating-databases). Если в `docker logs geoipupdate` ошибка авторизации или «invalid account ID», нужно исправить ключ в `.env` или остановить сервис: `docker stop geoipupdate`.
- **Подхват без перезапуска.** Раз в 10 минут authsession проверяет, заменён ли файл. Если заменён, он открывает новый и переходит на него, а старый закрывает, когда тот больше не читается.
  - Новую базу видно по строке лога `geoip: database …, GeoLite2-City built <дата>`.
  - Файл, который нельзя открыть, или пропавший файл оставляет в работе прежнюю базу. Ошибка пишется в лог один раз.
- **Город — только при малом радиусе.** У каждого адреса GeoLite есть `accuracy_radius`, то есть радиус в километрах, где адрес вероятно находится. Если он больше 100 км, authsession показывает только страну. Обычно это мобильные сети: по сравнению самой MaxMind, GeoLite размещает в пределах 50 км от верного места только 28% мобильных адресов Чехии. Порог выбран нами, что делает Telegram — неизвестно.
- **Без базы** authsession работает: в логе ошибка `geoip: cannot use the database <путь>, every place is UNKNOWN: <причина>`, а место у всех сеансов — `UNKNOWN`, пока файл не появится. Пустой `GeoIPDatabase` выключает GeoIP, тогда в логе строка уровня info.
- **Ключ.** `MAXMIND_ACCOUNT_ID` (число) и `MAXMIND_LICENSE_KEY` в `.env`. Это бесплатный аккаунт GeoLite владельца, ключ создаётся в Account → Manage License Keys. Значения владелец вносит прямо на сервере, в git и в логи они не попадают. Без них `docker compose -f docker-compose-env.yaml …` не запускается, как и без других обязательных переменных.
- **Без compose** (`teamgramd/etc`) файл кладут в `geoip/` в корне репозитория тем же образом, один раз:
  ```bash
  set -a; . ./.env; set +a
  docker run --rm -e GEOIPUPDATE_ACCOUNT_ID="$MAXMIND_ACCOUNT_ID" -e GEOIPUPDATE_LICENSE_KEY="$MAXMIND_LICENSE_KEY" \
    -e GEOIPUPDATE_EDITION_IDS=GeoLite2-City -v "$PWD/geoip:/usr/share/GeoIP" \
    ghcr.io/maxmind/geoipupdate:v8.0.0@sha256:51e70dd6f16cd3e4d845ac02d09940b10772a75b9d741427d235a78570923c1d
  ```

### Первый запуск и переход с DB-IP Lite

1. Владелец дописывает в `/opt/pluma/.env` строки `MAXMIND_ACCOUNT_ID=…` и `MAXMIND_LICENSE_KEY=…`. Проверка без вывода значений — команда выводит `2`:
   ```bash
   grep -cE '^MAXMIND_(ACCOUNT_ID=[0-9]+|LICENSE_KEY=.+)$' .env
   ```
2. Перейти на коммит с сервисом `geoipupdate` (`git fetch && git checkout <коммит>`, как в разделе «Обновление»), запустить сервис и дождаться файла:
   ```bash
   docker compose -f docker-compose-env.yaml -f docker-compose.public-ip.yaml up -d geoipupdate
   docker logs geoipupdate                      # без ошибок; последняя строка "# STATE: Sleeping for 12 hours"
   ls -la geoip/GeoLite2-City.mmdb
   ```
3. Выкатить образы этого коммита (раздел «Обновление»): authsession с путём `GeoLite2-City.mmdb` и остальные сервисы.
4. Пройти «Проверку» ниже. Только после неё удалить прежнюю базу: `rm geoip/dbip-city-lite.mmdb`. Пока файл на месте, откат на прежний коммит возвращает места DB-IP; после удаления у прежнего authsession места `UNKNOWN`. Скрипта `update-dbip.sh` больше нет.

### Лицензия и атрибуция

- **Условия.** Владелец принял GeoLite EULA при регистрации 2026-10-30. Показывать данные GeoLite людям вне организации можно с атрибуцией MaxMind (support.maxmind.com, «Sell or display data from GeoLite databases and web services»). Пример текста оттуда: «This product includes GeoLite Data created by MaxMind, available from https://www.maxmind.com». Его ставят «in all advertising and documentation mentioning features of or use of GeoLite data».
- **Где атрибуция:** в `NOTICE` и в этом разделе — документации, где упомянуто использование GeoLite. Решение владельца (2026-10-30): в клиентах и на pluma.chat её нет, GeoLite там не упоминается.
- **Свежесть.** EULA требует удалить выпуск в течение 30 дней после выхода нового (dev.maxmind.com/geoip/geolite2-free-geolocation-data). geoipupdate заменяет файл, authsession переходит на новый за 10 минут, в бэкапах базы нет.
- **Нельзя:** раздавать файл и определять по адресу конкретное домохозяйство, человека или улицу. Pluma показывает только город и страну, и только их владельцу аккаунта.

### Проверка

1. geoipupdate работает и не перезапускается: `docker inspect --format '{{.State.Status}} {{.RestartCount}}' geoipupdate` выводит `running 0`, последняя строка `docker logs geoipupdate` — `# STATE: Sleeping for 12 hours`, ошибок выше нет. Состояние `healthy` ничего не доказывает: встроенная проверка образа при одной базе проходит всегда (`docker/healthcheck.sh` v8.0.0 считает строки вывода `jq`, а пустой вывод — тоже одна строка). Устаревшую базу ловит алерт `GeoIPStale`.
2. В логе authsession есть строка `geoip: database /app/geoip/GeoLite2-City.mmdb, GeoLite2-City built <дата>`:
   ```bash
   docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml logs authsession | grep geoip
   ```
   Строка `geoip: cannot use the database …` значит, что файла нет или он не тот.

   Без Docker (`teamgramd/etc`, `Log.Mode: file`) первая строка о базе не попадает в файлы `teamgramd/logs/authsession/`. authsession открывает базу раньше, чем go-zero настраивает логгер по конфигу: база открывается в `svc.NewServiceContext` → `dao.New`, а логгер — в `zrpc.NewServer` (`c.SetUp()`), который вызывается после. До настройки go-zero пишет в стандартный вывод, ошибки — в stderr, а `teamgramd/bin/runall2.sh` направляет оба потока authsession в `teamgramd/logs/authsession.log`:
   ```bash
   grep geoip teamgramd/logs/authsession.log
   ```
3. В клиенте «Настройки → Устройства» (`account.getAuthorizations`) у сеансов место «Город, Страна» или страна вместо `UNKNOWN`. То же место — в сообщении о новом входе.
4. Если база открылась, а у всех сеансов `UNKNOWN`, шлюз видит не адрес клиента. Пример — IPv6-клиенты за адресом моста Docker: см. комментарий к портам gnetway в `docker-compose.yaml` и `security-setup.md` §3.

## Обновление

1. Дождаться зелёного запуска `images` для нужного коммита `master`.
2. Получить код этого коммита и проверить в STATUS, нет ли ручных шагов при обновлении:
   ```bash
   cd /opt/pluma && git fetch && git checkout <коммит>
   ```
3. Заменить в `.env` тег трёх образов на этот коммит.
4. Скачать образы и выполнить `up -d --no-build` обеих команд, как при первом запуске (шаг 5). Миграции применяет `migrate`.

Откат — то же самое с прежним коммитом и тегом. Схема базы назад не откатывается, поэтому откат возможен только к версии, совместимой с уже применёнными миграциями.
