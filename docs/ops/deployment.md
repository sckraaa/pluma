# Развёртывание на сервере

Как Pluma стоит на сервере, как его запустить впервые и как обновлять. Секреты и ключ сервера — `security-setup.md`, алерты — `monitoring.md`, бэкапы — `backup.md`, push — `push.md`.

## Схема

- **Код.** Клон репозитория в `/opt/pluma` на том коммите, который запущен. Из него берутся compose-файлы, конфиги и миграции.
- **Образы.** Сервер их не собирает. Задача `Images` в CI собирает образы коммита `master` после того, как на нём прошли все проверки: `ghcr.io/sckraaa/pluma-server`, `pluma-alertpush`, `pluma-backup`, `pluma-iosinstall`, `pluma-codes`, `pluma-stt` (сервис распознавания речи с моделью внутри, `docs/ops/transcription.md`), `pluma-translate` (перевод сообщений, модель внутри, `docs/ops/translation.md`) и `pluma-sfu` (медиа групповых звонков, `docs/ops/group-calls.md`). У коммита только с документацией, с красным CI и с CI, отменённым следующим push, образов нет; выкатывать надо последний коммит с кодом и зелёным CI, а при необходимости образы собирает ручной запуск `images` на нужной ветке. Тег у всех — полный SHA коммита. В `.env` сервера записаны образы его коммита (`TEAMGRAM_IMAGE`, `PLUMA_ALERTPUSH_IMAGE`, `PLUMA_BACKUP_IMAGE`). Без них compose собирает образы на месте: так работает машина разработки.
- **Свой адрес.** У Pluma может быть IPv4 рядом с основным адресом сервера (`PLUMA_PUBLIC_IPV4`, `docker-compose.public-ip.yaml`):
  - шлюз публикует порты только на нём;
  - исходящий трафик всех контейнеров уходит с него, потому что мост `teamgram_net` подменяет адрес на этот (SNAT) вместо основного.

  С основного адреса идёт только скачивание образов демоном Docker. Этот адрес вшит в клиентов. Floating IP можно переназначить на другой сервер той же сетевой зоны, и клиенты продолжат работать без пересборки.

На таком сервере обе команды compose получают второй файл:

```bash
docker compose -f docker-compose-env.yaml -f docker-compose.public-ip.yaml …   # инфраструктура
docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml …       # сервисы
```

Сеть создаёт та команда, что запустилась первой, поэтому файл нужен обеим. И всем остальным командам compose на сервере (`run`, `exec`, таймеры): команда без него видит сеть с другими настройками и пересоздаёт её. Контейнеры, которые уже работают, получают новые адреса, исходящий трафик уходит с основного адреса, а `up` инфраструктуры при подключённых сервисах оставляет её контейнеры вовсе без сети. Исправление — снова `up -d --force-recreate` обеих команд с этим файлом (сначала инфраструктура, потом сервисы). Kafka слушает все адреса контейнера и смену адреса переживает.

## Том для файлов (Hetzner Volume)
С 2026-11-12 файлы пользователей лежат на отдельном томе Hetzner (100 ГБ, `/mnt/HC_Volume_107445359`, строка в `/etc/fstab` с `nofail`). Пути в compose не менялись: каталоги тома подмонтированы на прежние места bind-строками `/etc/fstab` с `x-systemd.requires-mounts-for` тома:
- `…/garage` → `/opt/pluma/data/garage` (объектное хранилище Garage);
- `…/dfs-parts` → `/var/lib/docker/volumes/pluma_dfs_parts/_data` (части загрузок dfs, хранятся около 3 часов).

Пустые точки монтирования под ними помечены `chattr +i`: если том не подключился, Garage и dfs не стартуют на пустом месте, а падают при записи. Проверка после перезагрузки: `findmnt /opt/pluma/data/garage` показывает `/dev/sdb[/garage]`, `docker exec garage /garage status` — свободное место тома. Резервная копия (`docs/ops/backup.md`) читает тот же путь `data/garage`, на ней перенос не сказался.

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
   # проверка во временном каталоге: адрес войдёт в силу при загрузке
   d=$(mktemp -d) && mkdir -p $d/etc/netplan && cp -p /etc/netplan/*.yaml $d/etc/netplan/ && netplan generate --root-dir $d && rm -rf $d
   ip addr add <адрес>/32 dev eth0      # и сейчас
   ```
   `netplan generate` без `--root-dir` на работающем сервере не запускать: он переписывает файлы `systemd-networkd` в `/run/systemd/network`, и с ограничительным `umask` (например, `077`) они получаются `0600`. Работающий networkd этого не замечает, но при следующем перезапуске (его делают автоматические обновления) не может прочитать свой файл, бросает `eth0`, аренда DHCP не продлевается, и через 12–24 часа ядро снимает основной адрес вместе с маршрутом: сервер пропадает из сети целиком. Так было 05.11.2026 (`docs/ops/incidents/2026-11-05-network.md`). Если файлы уже переписаны, проверить: `ls -l /run/systemd/network/` — у `*.network` должны быть права `0640` и группа `systemd-network`.
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
4. Push — `push.md`, алерты — `monitoring.md`, бэкапы — `backup.md`, разделы «Первый запуск». На сервере, где есть другие программы, — общий потолок памяти Pluma (`memory.md`, «Один потолок на всю Pluma»). База GeoIP — раздел «GeoIP» ниже: ключ MaxMind в `.env` и сервис `geoipupdate`.
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
   Так же — фото встроенных инлайн-ботов `@gif` и `@pic` (`docs/ops/inline-bots.md`, «Имена и фото»). Эту команду можно повторять: бот с фото его сохраняет.
   ```bash
   docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml exec bff ./pluma-admin -f ../etc2/bff.yaml inline-bots photos
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
- **Имена.** `PLUMA_WEB_DOMAIN` — имя веб-клиента. Остальные имена сайта из `PLUMA_SITE_DOMAINS` (pluma.chat и www.pluma.chat) отдают лендинг.
- **Лендинг.** Статические файлы из `dist/` репозитория `pluma-site` (`npm ci && npm test && npm run build`). На хосте — `/srv/pluma-site` (другое место — `PLUMA_SITE_DIR` в `.env`), смонтирован в контейнер только для чтения: выпуски в `releases/<дата-время>/`, живой — за относительной ссылкой `current`. Заголовки, постоянные адреса устройств, `/pero`, `/app`, английская версия под `/en/` с теми же адресами и страница 404 — в `teamgramd/deploy/web/Caddyfile`. Журнала обращений у сайта нет: подвал страницы обещает, что она никого не считает.
- **Выложить новую версию лендинга** — контейнер не трогается, ссылка меняется одним `rename`, страница никогда не видна наполовину старой:
  ```bash
  R=$(date -u +%Y%m%d-%H%M%S)
  rsync -a dist/ "neo_old:/srv/pluma-site/releases/$R/"
  ssh neo_old "cd /srv/pluma-site && ln -sfn releases/$R current.new && mv -T current.new current"
  ```
  Откат — то же `ln -sfn … && mv -T` на прежний выпуск. Старые выпуски удаляются руками, кроме двух последних.
- **Ссылки pluma.chat** (задача 026, ADR 0036). Пути сайта, которые не являются его файлами или страницами (`/<имя>`, `/+<хеш>`, `/addstickers/…`, `/s/<канал>` и т. д.), Caddy передаёт сервису `links` (основной `docker-compose.yaml`, порт `9096` внутри сети). Сервис показывает объект, как t.me, и открывает приложение. Без сервиса эти пути отвечают 502. Новую страницу сайта нужно исключить из `@links` в Caddyfile; если её адрес может быть username — добавить его в `user.ReservedUsername`. Файл `/.well-known/assetlinks.json` (Android App Links, отпечаток сертификата подписи релиза) лежит в `public/` сайта. Заняты ли уже зарезервированные имена (список — `reservedUsernames` в `app/service/biz/user/user/username.go`):
  ```bash
  docker exec mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N teamgram -e "SELECT peer_type, peer_id, username FROM username WHERE LOWER(username) IN (\"android\",\"iphone\",\"windows\",\"linux\",\"assets\",\"collect\",\"addemoji\",\"addlist\",\"addstickers\",\"addstyle\",\"addtheme\",\"auction\",\"boost\",\"confirmphone\",\"contact\",\"giftcode\",\"invoice\",\"joinchat\",\"login\",\"proxy\",\"setlanguage\",\"share\",\"socks\",\"newbot\",\"oauth\")"'
  ```
  Пустой ответ — совпадений нет. Совпавшее имя у владельца остаётся, но его ссылка pluma.chat не откроет профиль.
- **Другие сайты на том же адресе.** Сайты оператора вне Pluma (например, свой сервис на отдельном домене) не дописываются в `teamgramd/deploy/web/Caddyfile`: локальная правка остановит `git checkout` при выкатке. Каждый — отдельный файл `<имя>.caddy` в `/srv/pluma-caddy-sites` на хосте (другое место — `PLUMA_CADDY_SITES_DIR` в `.env`); Caddyfile подключает их последней строкой `import`. Каталог может быть пустым. После изменения файла: `docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml -f docker-compose.web.yaml exec web caddy reload --config /etc/caddy/Caddyfile`. Пароли (`basic_auth`) хранятся только в этих файлах, в репозиторий не попадают.

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

## Превью ссылок

Карточки ссылок (ADR 0027) строят сервисы `bff` и `msg`. Им нужен исходящий доступ в интернет по HTTP и HTTPS: на сервере с `PLUMA_PUBLIC_IPV4` сайты видят этот адрес и User-Agent `PlumaBot`. Во внутренние сети сборщик не ходит. Карточки хранятся в таблице `webpages`, её создаёт миграция `migrate-20261102-webpages.sql` при запуске сервиса `migrate`. Настраивать ничего не нужно. Таблицу можно очистить в любой момент (`TRUNCATE webpages`): карточки соберутся заново при следующем запросе.

## Выпуски клиентов
Сборки клиентов после одобрения в `release` выкладываются на сайт по адресам `/dl/…` и предлагаются приложениям (`help.getAppUpdate`). Сервис `releases` запускается из `docker-compose.releases.yaml`, ему нужны токен загрузки и `PLUMA_RELEASES_URL` в `.env`: см. `releases.md`. Таблицу `app_releases` создаёт миграция `migrate-20261105-app-releases.sql`.

## Прогресс на сайте
Блок «Насколько Pluma уже Telegram» на pluma.chat берёт данные из `/progress.json`. Caddy отдаёт по этому адресу `docs/project/api-coverage.json` выкатанной версии: каталог `docs/project` смонтирован в контейнер `web` (`docker-compose.web.yaml`). Файл генерирует `go run ./tools/apicoverage` вместе с `api-coverage.md`, CI проверяет, что он актуален. После каждой выкатки цифры на сайте обновляются сами.
- Один раз, когда монтирование только появилось, контейнер `web` пересоздаётся: `docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml -f docker-compose.web.yaml up -d --no-deps web`.

## Платформа ботов (задача 106)

Выкат один раз, по порядку. Что умеют боты и как их настраивать — `bots.md`; секреты — `security-setup.md`, §2 и §6.

До выката:
1. **Секреты** в `.env` на хосте ядра:
   - `BOT_SECRET_ENCRYPTION_KEY` и `WEBAPP_SIGNING_KEY` — каждый `openssl rand -hex 32`, разные. Без них сервер работает, но мини-приложения и вход через ботов выключены.
   - Ключ ID-токенов `oauth.pluma.chat`: `(umask 077 && openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out secrets/oauth_signing_key.pem)`. Файл должен существовать до `up`: compose монтирует его в `links` как secret и без файла не поднимает стек вовсе. Если провайдер пока не нужен — пустой файл (`install -m 600 /dev/null secrets/oauth_signing_key.pem`).
2. **DNS — шаг владельца.** В Cloudflare A-запись `oauth.pluma.chat` на адрес края `195.201.252.126`, как у `pluma.chat`, без проксирования. Запись CAA `pluma.chat` действует и на это имя: Caddy дома получит сертификат тем же аккаунтом Let's Encrypt (`home-server.md`, «Сертификаты и CAA»). Проверка: `dig +short oauth.pluma.chat` отвечает адресом края. Другое имя провайдера — запись DNS для него и `PLUMA_OAUTH_DOMAIN` в `.env`: из неё берут имя и сайт Caddy, и `OAuth.Issuer` конфига `links` (`teamgramd/etc2/links.yaml`); `links` отвечает как провайдер только на это имя, и оно же стоит в `iss` ID-токенов. После смены имени пересоздать `web` и `links`.
3. **Имя `oauth`** теперь зарезервировано: `pluma.chat/oauth?startapp=…` — ссылка входа на сайт. Запрос из раздела «Ссылки pluma.chat» выше проверяет, не занято ли оно уже.

Выкат:
4. Обычное обновление (раздел «Обновление» или `pluma-deploy.sh`). Таблицы создаёт `migrate` (`migrate-20261121-bot-platform.sql`, повторный запуск безопасен).
5. **Пересоздать `web`:**
   ```bash
   docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml -f docker-compose.web.yaml up -d --no-deps --force-recreate web
   ```
   Caddyfile примонтирован в контейнер файлом. `git checkout` кладёт на его место новый файл, а контейнер продолжает видеть прежний; кроме того, у `web` новая переменная `PLUMA_OAUTH_DOMAIN`. `pluma-deploy.sh` контейнер `web` не пересоздаёт.
6. **Новые токены работающим ботам** (`bots.md`, «Развёртывание»): `bot list`, затем `bot token <имя_bot>` для каждого бота, у которого есть свой процесс. У встроенных `@gif` и `@pic` токенов нет. Токен передаётся владельцу бота, бот перезапускается с ним.
7. Открытый ключ подписи мини-приложений (`bot webapp-key`) отдать разработчикам ботов.

Проверка:
- `pluma-admin migrate status` — `0 pending`.
- `curl -s https://oauth.pluma.chat/.well-known/openid-configuration` — JSON с `"issuer":"https://oauth.pluma.chat"`; `curl -s https://oauth.pluma.chat/.well-known/jwks.json` — ключ RSA.
- После первых открытий приложений и входов в логах `bff` и `links` нет строк `keys of bot … do not open`.
- Ручные проверки в клиентах — `docs/project/TESTING-BACKLOG.md`, разделы задачи 106.

Откат: миграция только добавляет таблицы и колонки, прежняя версия их не читает. Ключи ботов остаются в `bot_secret_keys` и снова работают после повторного выката; боту, получившему токен на прежней версии, после него нужен новый токен.

## Края и российский узел (задача 109)

Патченные клиенты, у которых прямой адрес закрыт или «замерзает», ходят к серверу через **край** — MTProto в WebSocket по TLS (`wss://<хост>/apiws`). Краёв два: второй ресурс Gcore и российский узел amf; у узла ещё выдача файлов 202 и TURN для пользователей из России. Решение — ADR 0072; как устроено и переменные — `edge.md` (края), `ru-node.md` (узел), `cdn.md` (выдача 202), `calls.md` (TURN узла), `home-server.md` (второй туннель). Миграций нет. Всё включается по частям и выключается пустыми переменными; без них сервер ведёт себя как до задачи.

**Шаг 0 — выкат кода с пустыми переменными** (обычное обновление или `pluma-deploy.sh`), затем пересоздать `web`: у него новые Caddyfile (сервер `:8443`, импорты `edge-origin.d`, `ru-edge.d`), переменные и монтирования, а `pluma-deploy.sh` `web` не пересоздаёт (как в разделе «Платформа ботов»):
```bash
docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml -f docker-compose.web.yaml up -d --no-deps --force-recreate web
```
Здесь и ниже в команду входят и прочие наложения хоста, которые меняют `web` (карты, установка iOS, выпуски и т. д., как в обычной выкладке): без них пересозданный `web` теряет их маршруты и монтирования. Проверка: `web`, `bff`, `gnetway` здоровы; в `help.getAppConfig` нет `pluma_edge`; порт `11445` шлюза не опубликован (`ss -ltn | grep 11445` пусто).

**Шаг 1 — край Gcore** (`edge.md`, «Ресурс Gcore 2» и «Включение»):
1. Владелец: A-запись `PLUMA_EDGE_ORIGIN_DOMAIN` на адрес Pluma (без проксирования Cloudflare), ресурс Gcore 2 и его проверка 0.
2. `.env`: `PLUMA_EDGE_ORIGIN_DOMAIN`, `PLUMA_EDGE_ORIGIN_AUTH` (`openssl rand -hex 32`, отдельно от `CDN_ORIGIN_AUTH`), `EDGE_WS_URLS=gcore=wss://<префикс2>.gcorecdn.com/apiws`.
3. Пересоздать `web` (команда шага 0) и выкатить `bff` обычным деплоем.
4. Проверки владельца 1 и 2 брифа 109: Web K через `https://<префикс2>.gcorecdn.com/`, место сессии в «Устройствах».

**Шаг 2 — российский узел amf.** Порядок — бриф 109, «Эксплуатация»:
1. Второй туннель — `home-server.md`, «Второй туннель: российский узел amf».
2. Владелец: A-записи имён узла → 194.87.226.185 (без проксирования Cloudflare). Имена `*.pluma.chat` в России заблокированы, поэтому узел — на `sckra.dev` (решение 8 брифа 113): `ru.sckra.dev` (край и Web K), `r1.sckra.dev` (выдача 202), `pluma.sckra.dev` и `oauth.sckra.dev` (зеркала сайта и входа, шаг 2а).
3. Узел: `/etc/pluma-ru/env`, `deploy.sh`, `owner-nginx.sh check`, затем `install` по слову владельца, сертификат `r1` с CAA владельца — `ru-node.md`, «Первое включение», шаги 1–4. Сайт владельца `form.amf25.ru` проверить сразу после `install`.
4. `.env` дома: `PLUMA_RU_TUNNEL_IPV4=10.78.0.2`, `PLUMA_RU_EDGE_DOMAIN=ru.sckra.dev`, `CDN_RU_BASE_URL=https://r1.sckra.dev`, `CDN_RU_TOKEN_KEY`, `CDN_RU_ORIGIN_AUTH`, `CDN_RU_HEALTH_ADDR=10.78.0.1:443`, `CDN_RU_MONTHLY_GB`, `CDN_ROUTES=RU,BY:202,201;*:201`, `EDGE_WS_URLS` с `ru=wss://ru.sckra.dev/apiws`, `EDGE_ROUTES=RU,BY:edge,direct:ru,gcore;*:direct,edge:gcore,ru`, `CALLS_TURN_SECRETS=ru=…`, `CALLS_ICE_SERVERS` с записью `194.87.226.185:3478:stun+turn@ru`. Секреты создаются `openssl rand` на одной машине и переносятся на узел по SSH без печати (`ru-node.md`, «Переменные»). Значения стран и порядок краёв — решения владельца 1, 10, 11 брифа.
5. Выкладка с наложением узла. Порты `docker-compose.ru.yaml` публикуются на адресе туннеля, поэтому сначала проверить интерфейс:
   ```bash
   ip -4 addr show plumaru | grep -q 'inet 10.78.0.2/' || { echo "plumaru is down: see home-server.md" >&2; exit 1; }
   docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml -f docker-compose.web.yaml -f docker-compose.ru.yaml up -d --no-build --no-deps --force-recreate web bff
   ```
   С этого момента **каждая** команда compose, пересоздающая `web`, включает `-f docker-compose.ru.yaml` (вместе с прочими наложениями хоста — карты, выпуски и т. д.), иначе Caddy потеряет порты туннеля и край `ru` с выдачей 202 перестанут отвечать. `pluma-deploy.sh` (вне репозитория) при заданном `PLUMA_RU_TUNNEL_IPV4` добавляет этот файл и делает ту же проверку `plumaru` до `up`.
6. Цели Prometheus: `/opt/pluma/prometheus-targets/node-ru.json` (`home-server.md`).
7. Проверки владельца 9–12 брифа 109; ручные проверки — `docs/project/TESTING-BACKLOG.md`, раздел «109».

**Шаг 2а — зеркала сайта и входа на сайты через узел** (задача 113, `ru-node.md`, «Зеркала сайта и входа на сайты»): из России должны работать установка и обновления приложений (`/dl/`), сайт и вход на сайты.
1. Владелец: A-записи `pluma.sckra.dev` и `oauth.sckra.dev` → 194.87.226.185.
2. Узел: `PLUMA_RU_MIRROR_DOMAINS=pluma.sckra.dev,oauth.sckra.dev` в `/etc/pluma-ru/env`, `deploy.sh`, `owner-nginx.sh check` и `install` (карта SNI и порт 80 владельца изменились).
3. `.env` дома: `PLUMA_RU_SITE_DOMAIN=pluma.sckra.dev`, `PLUMA_RU_OAUTH_DOMAIN=oauth.sckra.dev`, `PLUMA_RELEASES_ROUTES=RU,BY=https://pluma.sckra.dev`. Пересоздать `web` с `docker-compose.ru.yaml` (новые сайты Caddy, монтирования `ru-site.caddy`, `ru-oauth.caddy`, tmpfs `ru-mirror.d`), выкатить `bff`, `releases` и `iosinstall` (у последнего новая переменная `MIRROR_HOSTS`, образ этой версии).
4. Проверка — `ru-node.md`, «Зеркала», шаг 4; в `help.getConfig` клиента из России `autoupdate_url_prefix` = `https://pluma.sckra.dev/dl/desktop`.

**Шаг 3 — сборки клиентов.** Переменная репозитория `PLUMA_EDGE_WS_URLS` в четырёх репозиториях клиентов (`pluma-android`, `pluma-ios`, `pluma-desktop`, `pluma-web`), рядом с адресом сервера: рекомендованное значение — `wss://<префикс2>.gcorecdn.com/apiws,wss://ru.sckra.dev/apiws` (решение владельца 10; имя узла — решение 8 брифа 113). Задаётся, когда края сервера уже отвечают: встроенный край, который не отвечает, клиенту не мешает (прямой путь первым), но и не помогает. Правки клиентов — A-128, I-133, D-106, W-086 (`clients/patches/`).

**Проверка после выката** (подробно — `edge.md`, «Проверка после деплоя», и `ru-node.md`, «Проверка»):
- метрики шлюза `gnetway_entry_conns{entry="edge",via=…}` и `gnetway_edge_no_client_addr_total` (рост — тревога: Gcore перестал слать адрес или кто-то ходит на вход мимо Caddy);
- `pluma_cdn_delivery_healthy{dc="202"}` = 1 через минуту после выката; в логе bff `deliveries [201 202], 2 routes`;
- цель `10.78.0.1:9100` в Prometheus — UP, алерты `ru-node.yml` молчат.

**Откат** — пустые переменные и деплой, в обратном порядке включения:
- узел: убрать `RU` (и `BY`) из `CDN_ROUTES` и `EDGE_ROUTES`, деплой — пользователи из России снова идут по записи `*`; затем `ru-node.md`, «Откат и выключение»; затем пустые `PLUMA_RU_EDGE_DOMAIN`, `CDN_RU_BASE_URL`, `PLUMA_RU_TUNNEL_IPV4`, `ru=…` из `EDGE_WS_URLS` вместе с `ru` из краёв всех записей `EDGE_ROUTES` (край, которого нет в `EDGE_WS_URLS`, останавливает bff), запись `@ru` из `CALLS_ICE_SERVERS`, выкладка без `docker-compose.ru.yaml`; туннель — `home-server.md`;
- край Gcore: пустой `PLUMA_EDGE_ORIGIN_DOMAIN`, `gcore=…` из `EDGE_WS_URLS` и `gcore` из краёв `EDGE_ROUTES`, пересоздать `web`, деплой;
- всё сразу: пустой `EDGE_WS_URLS` — ключа `pluma_edge` нет; встроенные края клиентов перестают отвечать, клиенты остаются на прямом пути.

## Обновление слоёв (ADR 0045)

Сервер обслуживает клиентов разных слоёв (сейчас 228 и 233) одновременно, клиенты переходят поэтапно.

При обновлении сервера со слоя 228 на 233:
1. Применить `migrate-20261109-forum-sidebar.sql` (идёт через migrate-сервис вместе с остальными).
2. Перезапускать все сервисы вместе: контракт gRPC между ними изменился. Очередь `mq_outbox` и бинарные ответы каталога остаются читаемыми: номера полей protobuf для них не менялись.
3. Каталог заново импортировать не нужно: он собран и читается в схеме 228, проверка слоя в импортёре относится к сбору.

## Обновление

1. Дождаться зелёного запуска `CI` с задачей `Images` для нужного коммита `master`.
2. Получить код этого коммита и проверить в STATUS, нет ли ручных шагов при обновлении:
   ```bash
   cd /opt/pluma && git fetch && git checkout <коммит>
   ```
3. Заменить в `.env` тег образов (сервисы, alertpush, backup, iosinstall) на этот коммит.
4. Скачать образы и выполнить `up -d --no-build` обеих команд, как при первом запуске (шаг 5). Миграции применяет `migrate`.
5. Когда сервисы здоровы, удалить образы Pluma прежних коммитов, кроме этого и предыдущего (для отката). Каждый коммит — около 4 ГБ образов: за 16 выкаток они заняли 40 ГБ из 75, и сработало оповещение о диске (2026-11-01).
   ```bash
   NEW=<коммит>; PREV=<прежний коммит>
   docker image ls --format '{{.Repository}}:{{.Tag}}' | grep '^ghcr.io/sckraaa/pluma-' \
     | grep -v -e ":$NEW\$" -e ":$PREV\$" | xargs -r docker image rm
   ```

Откат — то же самое с прежним коммитом и тегом. Схема базы назад не откатывается, поэтому откат возможен только к версии, совместимой с уже применёнными миграциями.
