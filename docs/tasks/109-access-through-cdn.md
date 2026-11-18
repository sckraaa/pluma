# 109. Доступ к Pluma отовсюду без VPN: весь трафик клиентов через CDN

- Статус: выкачено (2026-11-18, `3af2d055`): край `ru`, выдача 202, TURN узла; край Gcore ждёт ресурса Gcore 2. Части 1–6 и 8–13 — партия `batch/20261117`, влита PR sckraaa/pluma#55; часть 7 — отдельная задача замеров после включения. Итог — раздел «Результат».
- Решение: ADR 0072 (`docs/adr/0072-edge-websocket-through-cdn.md`, accepted 2026-11-17: узел в России — amf, имена `ru.pluma.chat` и `r1.pluma.chat`).
- Зависит от: 092 (замеры доступности из России), 107 (HTTPS-CDN на Gcore — аккаунт, ресурс, опыт подписи и источника), ADR 0057 и 0061 (дом — ядро, VPS — труба для шифра), ADR 0071.
- Поставлена владельцем 2026-11-16: «важно, чтобы клиенты могли заходить откуда угодно, даже там, где блокировки, без VPN». 2026-11-17: сделать вместе с остальными обсуждёнными задачами.

## Цель
Клиенты Pluma подключаются к серверу не к адресу VPS, а к большому CDN, как к обычному HTTPS-сайту. CDN — только труба: внутри TLS идёт трафик MTProto, который и так зашифрован ключом авторизации между клиентом и домом. Содержимое CDN не видит, как сейчас не видит VPS.

## Что это решает и что нет (уточнено исследованием 2026-11-17)
- **Решает:**
  - блокировку и замедление по IP адресу VPS (задача 092: подсети Hetzner из России «замораживаются» после ~16 КБ, HTTPS pluma.chat прошёл с 3 из 30 точек). Адреса и общий домен `*.gcorecdn.com` делят множество сайтов;
  - блокировку по сигнатуре протокола (MTProto, VPN): снаружи это WebSocket внутри обычного TLS к CDN.
- **Не решает или решает частично:**
  - мобильные «белые списки» в России — нужен провайдер с адресами из списка (шов в ADR 0072 §7);
  - распознавание по отпечатку TLS: у нативных клиентов TLS — системный или встроенный стек, не браузерный (ADR 0072 §1); против него — WEB-прокси Telegram (шов);
  - звонки — UDP, CDN их не несёт;
  - целевую блокировку хоста края по SNI — на общем домене Gcore дорого для блокирующего, но возможно.
- Скорость ограничена домашним каналом, как сегодня.

## Исследование (2026-11-17)
Подробно, с источниками — ADR 0072, «Контекст». Главное:
1. **Что CDN может нести** — HTTP(S) и WebSocket. Единственный транспорт MTProto, который уже есть у шлюза и у всех четырёх клиентов в виде готовых библиотек, — WebSocket по TLS:
   - Web K ходит только по WebSocket;
   - tdesktop: TCP поверх `AbstractSocket` и свой `Core::WebSocketClient` по TLS; HTTP-транспорт только `http://IP:80` и держится на `http_wait`, который сервер не обслуживает;
   - Android: tgnet — только TCP в C++, в Java есть nv-websocket-client 2.14;
   - iOS: MtProtoKit на `GCDAsyncSocket`, `URLSessionWebSocketTask` уже используется в приложении, минимальная iOS 15.
2. **WEB-прокси Telegram 13.2** — это локальный MTProxy, байты которого несёт страница оператора в WebView; дому нужен ретранслятор кадров и MTProxy с секретом. Это настройка прокси пользователя без автоматического перехода. Не основной путь, а шов для России (ADR 0072 §1, §7).
3. **Gcore:** WebSocket — опция ресурса; для CNAME-ресурса выключена по умолчанию; **соединение закрывается через 30 с без данных от источника** (поднять нельзя, кроме Enterprise) — шлюз обязан слать ping. Secure Token ресурса файлов сломал бы `Upgrade`, поэтому нужен второй ресурс.
4. **Дом:** шлюз уже принимает WebSocket (11443, 11444 с PROXY v1), ping не шлёт; Caddy ведёт `/apiws` Web K на 11444.

## Контракт клиентов
Общий для четырёх клиентов; патч каждого ссылается на этот раздел.

1. **Адреса краёв:** `wss://<хост края>/apiws`, порт 443. Краёв может быть несколько (Gcore и российский узел amf, раздел «Российский узел amf»). Встроенный в сборку список: `PLUMA_EDGE_WS_URLS` — от 1 до 4 адресов через запятую, в порядке перебора (переменная репозитория клиента, рядом с адресом сервера; пустая — край выключен, патч ничего не добавляет). Рекомендуемое значение сборки: `wss://<префикс2>.gcorecdn.com/apiws,wss://ru.pluma.chat/apiws`. Проверка конфигурации в `ci/patch.py`: только `wss://`, хост — имя (не IP), не домены Telegram, без повторов, не больше 4.
2. **Рукопожатие:** HTTP/1.1 `GET /apiws` с `Upgrade: websocket`, `Sec-WebSocket-Protocol: binary` (как Web K, `tweb/src/lib/mtproto/transports/websocket.ts:51`). Нативные клиенты шлют `User-Agent: Pluma`, без cookie. `Origin` сервер не требует.
3. **Поток:** в бинарных сообщениях — те же байты, что клиент шлёт по TCP на DC 1: 64-байтовый obfuscated2-заголовок и кадры обычного транспорта клиента (abridged, intermediate или padded), без секрета `dd`/`ee`. Границы сообщений произвольны, сообщение не больше 1 МиБ (шлюз принимает до 16 МиБ, `app/interface/gnetway/internal/server/gnet/ws/codec.go:26`). Текстовые сообщения шлюз отбрасывает, клиенты их не шлют.
4. **Живость:** шлюз шлёт ping каждые 20 с; клиент обязан отвечать pong (RFC 6455; все выбранные библиотеки отвечают сами — проверить в каждой части). Клиент считает соединение мёртвым, если от сервера 45 с не пришло ни одного байта (ping включительно), и переподключается.
5. **Порядок путей (нативные клиенты):**
   - прямой адрес первым, края — последними адресами DC 1 в собственном переборе клиента, между собой в порядке списка `ws`;
   - на край клиент переходит, когда прямой адрес не соединился, или когда прямое соединение «замёрзло»: соединение установлено, а на отправленный запрос 15 с нет ни одного байта — дважды подряд;
   - после перехода на край клиент держится его `sticky_s` (по умолчанию 1800 с), затем при следующем переподключении снова пробует прямой путь первым;
   - запись «край выбран до …» живёт в памяти процесса; после перезапуска приложения — снова прямой первым.
6. **Ключ `pluma_edge` в `help.getAppConfig`:** `{"v":1,"ws":["wss://…/apiws"],"order":["direct","edge"],"sticky_s":1800}`. Клиент кэширует последнее принятое значение; принимает `v = 1`, от 1 до 4 адресов `wss://`, `order` — перестановка `direct` и `edge`, `sticky_s` от 60 до 86400. Иначе — встроенное. `order: ["edge","direct"]` — края первыми (на случай решения владельца «всегда через CDN» или для страны). Значение сервер выбирает по стране адреса клиента (`EDGE_ROUTES`, раздел «Российский узел amf», «Край»): клиенту из России приходит `{"ws":["wss://ru.pluma.chat/apiws","wss://<префикс2>.gcorecdn.com/apiws"],"order":["edge","direct"]}`, остальным — Gcore первым и `direct` первым. Клиент страну не знает и не проверяет: он просто применяет последнее принятое значение.
7. **Web K:** не перебирает. Страница, открытая с хоста из списка сборки (`PLUMA_SERVER_URL` и хосты `PLUMA_EDGE_WS_URLS`), ходит в `wss://<location.host>/apiws`; с любого другого хоста — в `PLUMA_SERVER_URL`, как сегодня (W-001).
8. **Что клиент не делает:** не показывает край в интерфейсе, не трогает настройки прокси пользователя; включённый пользователем прокси отключает край (как он отключает прямой путь).

## Дизайн
Решения и причины — ADR 0072. Здесь — как это устроено.

### Сервер
- **Вход `Edge` шлюза** (`app/interface/gnetway`):
  - конфигурация: `Proto: websocket`, `Edge: true`, адрес `0.0.0.0:11445` в `teamgramd/etc2/gnetway.yaml` и `teamgramd/etc/gnetway.yaml`; `Edge` и `PPV1` вместе — ошибка конфигурации при старте;
  - при `Upgrade` (`ws/codec.go`, `OnHeader`) читается `X-Pluma-Client-Addr`: список через запятую, берётся последний элемент, который разбирается как IPv4/IPv6 (без порта); нет — соединение закрывается до счёта пределов;
  - пределы по адресу (`ip_limits.go`) и адрес соединения (`connContext.setClientIp`) — после этого, по образцу PROXY v1 (`server/gnet/server_gnet.go:69-71`, `:228-271`);
  - ping каждые 20 с (`EdgePingInterval`, по умолчанию 20 с, допустимо 5–25 с) через колесо таймаутов (`timeout_wheel.go`); ping и pong не продлевают простой соединения (`extendDeadline`), простой — как у TCP (300 с);
  - через какой край пришло соединение — заголовок `X-Pluma-Edge-Via`, который ставит только Caddy (`gcore` у сайта `edge-origin.caddy`, `ru` у сайта `ru-edge.caddy`); значение вне списка `EdgeVias` (по умолчанию `gcore,ru`) или его отсутствие — `other`. Только для метрик, на пределы и адрес не влияет;
  - метрики Prometheus с меткой `entry` (`tcp`, `ws`, `ws_ppv1`, `edge`) и у входа `edge` — меткой `via` (`gcore`, `ru`, `other`): открытые соединения, закрытия по причине, байты до закрытия (гистограмма с корзинами 16 КБ, 64 КБ, 1 МБ…), счётчик отказов из-за отсутствующего адреса. Адреса в метки и логи не попадают (`tools/logaudit`).
- **Сайт Caddy `teamgramd/deploy/web/edge-origin.caddy`** (импорт `/etc/caddy/edge-origin.d/*.caddy`, ссылка создаётся при старте контейнера только при заданном `PLUMA_EDGE_ORIGIN_DOMAIN`, как `cdn-origin.caddy`):
  - `@denied not header X-Pluma-Edge-Auth {$PLUMA_EDGE_ORIGIN_AUTH}` → 403 до всего остального (сравнение Caddy — не постоянного времени; секрет 256 бит, перебор по сети нереален — записать в комментарии);
  - `handle /apiws` → `reverse_proxy gnetway:11445` с `header_up X-Pluma-Client-Addr {http.request.header.X-Forwarded-For}` и `header_up -X-Pluma-Edge-Auth`;
  - остальное — статика Web K из `/srv` с теми же заголовками кэша, что у сайта Web K;
  - без `log`, `-Server`, `-Via`.
- **Сайт Caddy `teamgramd/deploy/web/ru-edge.caddy`** — край через российский узел amf (раздел «Российский узел amf», «Край»), импорт `/etc/caddy/ru-edge.d/*.caddy`, ссылка при заданном `PLUMA_RU_EDGE_DOMAIN`; сайт `{$PLUMA_RU_EDGE_DOMAIN}:8443` на отдельном сервере Caddy `:8443` с `listener_wrappers { proxy_protocol { allow 10.78.0.1/32; fallback_policy reject } tls }` (глобальный блок `servers :8443` в `Caddyfile`; на сервер 443 не влияет):
  - `handle /apiws` → `reverse_proxy gnetway:11445` с `header_up X-Pluma-Client-Addr {http.request.remote.host}` (адрес из PROXY v1 узла amf) и `header_up X-Pluma-Edge-Via ru`; заголовки клиента с этими именами затираются;
  - остальное — статика Web K, как в `edge-origin.caddy`; без `log`, `-Server`, `-Via`;
  - секрета нет: порт 8443 опубликован только на адресе туннеля `10.78.0.2` (`docker-compose.ru.yaml`, часть 9), PROXY принимается только от `10.78.0.1`.
- **Ключ `pluma_edge`** в `app/bff/configuration/internal/appconfig/appconfig.go`:
  - края по именам: `EDGE_WS_URLS=gcore=wss://<префикс2>.gcorecdn.com/apiws,ru=wss://ru.pluma.chat/apiws` (имя — `[a-z0-9]{1,16}`);
  - маршруты по стране: `EDGE_ROUTES=RU:edge,direct:ru,gcore;*:direct,edge:gcore,ru` — записи через `;`, в записи `<страны через запятую или *>:<order>:<имена краёв по порядку>`; та же грамматика стран, что `CDN_ROUTES` (ADR 0071 §1). Пусто — одна запись `*:direct,edge:<все края в порядке EDGE_WS_URLS>`;
  - `EDGE_STICKY_S` (1800) общий;
  - страна — `authsession.getIpCountry` по `MD.ClientAddr` (часть 8) с кэшем; неизвестная страна — запись `*`;
  - у каждой записи свой объект и свой хэш (`hashOf` с именем записи, как `WithURLAuthDomains`): клиент, сменивший страну, получает новый объект при следующем `help.getAppConfig`;
  - пустой `EDGE_WS_URLS` — ключа нет. Проверка при старте: `wss://`, 1–4 края в записи, имена есть в `EDGE_WS_URLS`, порядок — перестановка `direct,edge`, ровно одна запись `*`, страна не повторяется.
- **`pkg/mtclient`:** транспорт WebSocket (gobwas/ws уже в зависимостях шлюза) с возможностью задать заголовки — для e2e входа `Edge` и для замеров.

### Клиенты
Номера правок — следующие свободные на 2026-11-17 с учётом ветки `task/112-maps-public` (PR #50: A-125…A-127 и D-105 заняты картами задачи 112): A-128, D-106, I-133, W-086. Перед записью в реестр номер сверить ещё раз с `clients/patches/README.md` на `origin/master` и открытыми ветками задач.

Все три нативных патча принимают список краёв (контракт, п. 1): каждый край — отдельный адрес-метка в переборе, в порядке списка; `pluma_edge` может переставить края и поставить их перед прямым путём.

- **W-086, Web K** (малый, ≈30 строк патча и тесты): в теле `constructTelegramWebSocketUrl` (правка W-001) — если `location.host` равен хосту `PLUMA_SERVER_URL` или одному из хостов `PLUMA_EDGE_WS_URLS`, вернуть `wss://${location.host}/apiws`, иначе — URL сервера. `configure` принимает `PLUMA_EDGE_WS_URLS`. CSP сайта: `connect-src 'self'` уже покрывает свой хост — проверить в Chrome, Firefox, Safari. Состояние Web K (вход, кэш) у адреса края своё: пользователь входит там отдельно (решение владельца 3).
- **D-106, Desktop** (≈250–350 строк C++ в патче, ≈150 строк `patch.py` и тесты):
  - `MTP::details::EdgeSocket : AbstractSocket` на `Core::WebSocketClient` (`core/websocket_client.h`): `connectToHost` открывает `wss://` на хост края, `read`/`write` — через буфер, `isGoodStartNonce` как у `TcpSocket`, `debugPostfix` `_edge`;
  - выбор сокета в `connection_tcp.cpp:529-537`: адрес-метка края → `EdgeSocket`;
  - `session_private.cpp`, после перебора `variants` (`:1059-1074`): добавить тестовое соединение края с приоритетом 0 (ниже любого прямого TCP, `:203-205`), если край не выключен и прокси пользователя нет; tdesktop сам ждёт лучшего 2 с (`:34`);
  - «замерзание» прямого: при закрытии соединения, которое ни разу не получило ответа за 15 с после запроса, — счётчик; на втором подряд — край первым на `sticky_s` (поднять его приоритет выше прямого);
  - `pluma_edge` читается из `appConfig` (`main/main_app_config.h`) и хранится в настройках;
  - что `WebSocketClient` отвечает pong и шлёт `User-Agent`, проверить по `websocket_client.cpp`; если не шлёт — дописать заголовок в `sendUpgradeRequest`.
- **A-128, Android** (≈350–450 строк Java, ≈60–100 строк C++ в патче, тесты `patch_test.py`):
  - `org.telegram.utils.proxy.PlumaEdgeBridge` — локальный `ServerSocket` на `127.0.0.1` (образец — `WebProxyTransport.java:323`, `acceptLoop` `:370-400`): каждое принятое соединение tgnet — отдельный `WebSocket` nv-websocket-client на `wss://…/apiws` с `binary`, байты в обе стороны без разбора, сообщение ≤ 64 КБ; закрытие одной стороны закрывает другую;
  - tgnet: в `ConnectionsManager::initDatacenters()` (правка A-002, `jni/tgnet/ConnectionsManager.cpp:1836`) — последним адресом DC 1 адрес-метка края; в `ConnectionSocket::openConnection` (`jni/tgnet/ConnectionSocket.cpp:68`) метка заменяется на `127.0.0.1:<порт моста>`, порт — из Java через JNI (`TgNetWrapper.cpp`); мост запускается в `ConnectionsManager.init` рядом с WEB-прокси (`ConnectionsManager.java:675-680`), не запускается при включённом прокси пользователя;
  - «замерзание» и `sticky_s`: tgnet меняет адрес после 3 неудач, если данные уже шли (бриф 092, «Перебор адресов»); через `sticky_s` мост вызывает `Datacenter::resetAddressAndPortNum` (`Datacenter.cpp:486`) через JNI, чтобы вернуться к прямому. Точное поведение `nextAddressOrPort` (`:374`) и сохранения адреса (`:468`) прочитать и описать в A-128 до кода;
  - метка не должна попасть в обновление адресов из `help.getConfig` (`dc_options` у Pluma пусты) — проверить.
- **I-133, iOS** (≈300–400 строк ObjC/Swift в патче, тесты `patch.py`):
  - новый класс `MTWebSocketConnection` с интерфейсом `MTTcpConnection` на `NSURLSessionWebSocketTask` (iOS 15+);
  - адрес края — `MTDatacenterAddress` с `host` и новым флагом `websocket` (поле `NSCoding` с умолчанием `false`), добавляется в seed DC 1 (правка I-00x `Network.swift`, блок `SEED_OLD`);
  - выбор класса в `MTTcpTransport.m:199` и `MTDiscoverConnectionSignals.m:99` по флагу; поиск уже параллельный, с перепроверкой лучшего через 30 с (`MTDiscoverConnectionSignals.m:260-342`) — край получает задержку старта 3 с, чтобы прямой выигрывал при равных;
  - патч пишется на ту ветку iOS, которая пойдёт в выпуск (сейчас выпуск на 12.9.2, 13.2 — ветка `task/074-upstream`): проверить якоря на обеих.

## Российский узел amf (дополнение 2026-11-17)
Решения и причины — ADR 0072 §8. Здесь — что ставится и как.

### Что установлено 2026-11-17 (владелец и замеры)
- Знакомый в России на домашнем провайдере не открыл файловый CDN Gcore (`fx7k2.gcorecdn.com`): долгое ожидание, затем `ERR_CONNECTION_RESET`. Globalping из российских дата-центров — 14/15 успешно, то есть дата-центры не показывают того, что видят домашние сети.
- Адрес Hetzner 195.201.252.126 из России недоступен или «замерзает» даже из российского дата-центра: `pluma.chat` с amf — таймаут, порт 10443 — отказ.
- Сайты на российском VPS владельца у того же знакомого открываются.
- Владелец отдал Pluma VPS **amf**: 194.87.226.185, Timeweb, Москва, Ubuntu 26.04, 2 vCPU, 2 ГБ памяти (свободно около 1,7 ГБ), 28 ГБ диска свободно; Docker нет, ufw выключен, работают агент Zabbix и fail2ban.
- Порт 443 amf делится по SNI с сайтом владельца `form.amf25.ru`: nginx `stream`, `/etc/nginx/stream.d/sni.conf`, `map $ssl_preread_server_name`, `default` → сайт на `127.0.0.1:8443` с PROXY. Порт 80 — nginx сайта (HTTP-01 его сертификата). Сайт не ломать; DNS `form.amf25.ru` остаётся на amf.
- Туннель дом → amf: UDP проходит; рукопожатие WireGuard есть, ping без потерь около 74 мс; iperf3 amf → дом 109 Мбит/с; дом → amf 0 байт при MTU 1420 (чёрная дыра PMTU), 50 Мбит/с при MTU 1280.
- Мобильные «белые списки»: есть ли адрес amf в них — НЕ ПРОВЕРЕНО.
- Роли узла по желанию владельца: (1) выдача файлового CDN для пользователей из России (только шифр, ключи дома); (2) вход в мессенджер без VPN для пользователей из России (край этой задачи); (3) TURN звонков пользователей из России. Позже, возможно, — основной край вместо Hetzner (не в этой задаче).

### Схема
```
клиент в России ──TLS 443, SNI ru.pluma.chat──► amf nginx stream (владельца) ──PROXY v1 + TLS как есть──► WireGuard plumaru ──► дом 10.78.0.2:8443 Caddy (TLS, сертификат дома) ──► gnetway Edge 11445
клиент в России ──TLS 443, SNI r1.pluma.chat──► amf nginx stream ──PROXY v1──► nginx Pluma 127.0.0.1:9443 (TLS amf, secure_link, кэш) ──► WireGuard ──► дом 10.78.0.2:443 Caddy, источник CDN (шифр)
клиент в России ──UDP 3478, 49160–49659──► coturn на amf (только шифр SRTP)
дом ──WireGuard, дом звонит, MTU 1280──► amf 51820/udp     дом: Prometheus ──► 10.78.0.1:9100 node-exporter
```

### Туннель (часть 9)
- Второй канал WireGuard, отдельный от Hetzner: интерфейс `plumaru`, amf — `10.78.0.1`, дом — `10.78.0.2`, сеть `10.78.0.0/24`, порт 51820/udp на amf. Звонит дом (`PersistentKeepalive = 25`), роутер дома не трогается.
- **MTU 1280 с обеих сторон** (замер: 1420 — чёрная дыра дом → amf, 1280 — 50 Мбит/с) и `TCPMSS --clamp-mss-to-pmtu` для `plumaru` в обе стороны на доме (FORWARD к контейнерам) и на amf.
- **Без пересылки адресов и без таблицы маршрутов.** В отличие от Hetzner, amf не делает DNAT: все соединения из amf к дому открывает сам amf с адреса `10.78.0.1` (nginx), адрес клиента идёт в PROXY v1. Ответы контейнеров на `10.78.0.1` идут по правилу `pref 100 lookup main suppress_prefixlength 0`: у `main` есть подключённый маршрут `10.78.0.0/24 dev plumaru` — правило `pref 110` (всё остальное — в Hetzner) до них не доходит. Поэтому `Table = off` и ни одного нового `ip rule`.
- `AllowedIPs`: дома для amf — `10.78.0.1/32`; на amf для дома — `10.78.0.2/32, 10.77.0.2/32`. Второй адрес нужен, потому что Docker подменяет источник всех контейнеров на `host_ipv4` сети (`10.77.0.2`, `docker-compose.public-ip.yaml`), а bff (проверка здоровья выдачи 202) и Prometheus (сбор метрик amf) ходят на `10.78.0.1` из контейнеров. На amf это только маршрут к `10.77.0.2` в `plumaru`; адресов Hetzner amf не получает.
- Дома: служба `pluma-tunnel-ru-rules` (правила — в отдельной службе, не в `PostUp`: AppArmor Ubuntu не даёт `wg-quick` запускать скрипты), ключи в `/etc/wireguard/plumaru.{key,conf,env}` с правами 600 (AppArmor запрещает `wg` читать `/root`); drop-in `systemd-networkd` `ManageForeignRoutingPolicyRules=no` уже есть.
- Дома `ufw`: `deny in on plumaru` выше `allow 22/tcp` — amf не должен видеть SSH и другие службы хоста. Порты Docker на `10.78.0.2` (80, 443, 8443) идут мимо ufw через DNAT Docker — это и нужно.
- Docker публикует на `10.78.0.2`, поэтому адрес должен существовать до запуска Docker: drop-in `docker.service.d/10-pluma-tunnel-ru.conf` (`Wants=`/`After=wg-quick@plumaru`). Интерфейс поднимается локально без ответа amf, так что недоступный amf не мешает дому стартовать. Если сам `wg-quick@plumaru` не поднимется, `docker-compose.ru.yaml` не сможет опубликовать порты, и `pluma-deploy.sh` при заданном `PLUMA_RU_TUNNEL_IPV4` проверяет `ip -4 addr show plumaru` до `up` и останавливается с понятной ошибкой (правка скрипта вне репозитория — `~/pluma-work/bin/pluma-deploy.sh`, делает основная сессия).
- `tunnel.sh` получает параметр канала (`--link hetzner|ru`, по умолчанию `hetzner` — все существующие вызовы и службы не меняются): имя интерфейса, адреса, MTU, порт, роль `node` для amf (без SNAT и без пересылки, только ufw не трогается — он выключен) и `home` (без таблицы). Вывод — только публичный ключ, как сегодня.

### Имена и TLS
- **Имена** (A-записи в Cloudflare делает владелец, без проксирования): `ru.pluma.chat` — край и Web K для России; `r1.pluma.chat` — выдача файлов 202. Оба → 194.87.226.185. Нейтральный домен вместо `pluma.chat` — решение владельца 9.
- **Край `ru.pluma.chat` — TLS дома, amf — чистая TCP-труба** (как Hetzner, ADR 0061):
  - nginx `stream` владельца пропускает соединение без расшифровки к `10.78.0.2:8443` через туннель; PROXY v1 включён у сервера `stream` для всех его направлений (у nginx `proxy_protocol` — директива сервера `stream`, `on` = версия 1, https://nginx.org/en/docs/stream/ngx_stream_proxy_module.html, прочитано 2026-11-17), так что адрес клиента приходит домой без DNAT;
  - сертификат выпускает домашний Caddy под своим ACME-аккаунтом, записи CAA `pluma.chat` (`accounturi` дома) не меняются: владелец amf (или тот, кто его захватит) не выпустит сертификат `ru.pluma.chat`;
  - проверка ACME: TLS-ALPN-01 приходит на amf:443 с SNI `ru.pluma.chat` и проходит трубой до Caddy; HTTP-01 приходит на amf:80 к nginx сайта — файл Pluma `acme-http.conf` в nginx владельца проксирует `/.well-known/acme-challenge/` для `ru.pluma.chat` на `http://10.78.0.2:80` (Caddy дома), остальное — 301 на `https://`. Отвечает ли Caddy на TLS-ALPN-01 на сервере `:8443` за PROXY — НЕ ПРОВЕРЕНО; HTTP-01 через порт 80 работает независимо от этого.
- **Выдача `r1.pluma.chat` — TLS на amf.** Кэш невозможен без расшифровки HTTP на узле, а без кэша узел не снимает отдачу с канала дома. Узел при этом видит то же, что Gcore (ADR 0071 §11): шифр AES-256-CTR под 256-битными именами, диапазоны, адреса и время. Ключи, IV и хэши — только по MTProto из дома.
  - сертификат: `certbot` из Ubuntu на amf, `--webroot` через порт 80 nginx владельца (`acme-http.conf`: для `r1.pluma.chat` — `root /var/lib/pluma-ru/acme`), ключ `ECDSA P-256`, хук продления — `systemctl reload pluma-ru-nginx`;
  - **CAA**: у `pluma.chat` записи разрешают только аккаунт дома, поэтому выпуск на amf упрётся в них. Владелец добавляет записи CAA на само имя `r1.pluma.chat` — у ближайшего имени с CAA записи наследования не действуют (RFC 8659 — по памяти, НЕ ПРОВЕРЕНО, проверка владельца 7): `0 issue "letsencrypt.org; accounturi=<аккаунт amf>; validationmethods=http-01"`. Аккаунт создаётся на amf первым шагом (`certbot register`), URL печатается;
  - захват amf даёт выпуск только `r1.pluma.chat`, а он и так на amf.
- **Почему не TLS дома и для файлов:** узел без кэша — каждый байт идёт из дома через туннель (50 Мбит/с дом → amf), повторные просмотры не дешевеют. Почему не TLS на amf и для края: край не выигрывает от расшифровки ничего, а узел увидел бы обфусцированный MTProto (содержимое всё равно закрыто ключом авторизации, но принцип ADR 0057 — узлы видят только шифр транспорта).

### nginx на amf (часть 10)
- **Свой экземпляр nginx** (`pluma-ru-nginx.service`, `/usr/sbin/nginx -c /etc/pluma-ru/nginx.conf`, свой `pid`, свои журналы ошибок, журнала доступа нет) из того же пакета Ubuntu. Выкладка Pluma никогда не перезагружает nginx владельца; nginx владельца трогается один раз (`owner-nginx.sh install`): файл `/etc/nginx/stream.d/pluma-upstreams.conf` (направления `pluma_ru_edge` → `10.78.0.2:8443`, `pluma_ru_files` → `127.0.0.1:9443`), файл карты `/etc/nginx/stream.d/pluma-sni.map` и одна строка `include /etc/nginx/stream.d/pluma-sni.map;` в `map` `sni.conf`, файл `acme-http.conf` для порта 80; затем `nginx -t` и `reload` nginx владельца. Поддерживает ли `map` у `stream` `include` — проверить по документации nginx в части 10 (по памяти да — НЕ ПРОВЕРЕНО); если нет — две строки прямо в `sni.conf`, скрипт проверяет их наличие. Попадает ли `stream.d/*.conf` в `stream {}` владельца — `owner-nginx.sh check` читает `nginx -T`.
- Модули: `secure_link` и `slice` не собираются по умолчанию (`--with-http_secure_link_module`, `--with-http_slice_module`, https://nginx.org/en/docs/http/ngx_http_secure_link_module.html, https://nginx.org/en/docs/http/ngx_http_slice_module.html, прочитано 2026-11-17); есть ли они в пакете Ubuntu 26.04 — `nginx -V` на amf (`owner-nginx.sh check`), НЕ ПРОВЕРЕНО.
- **Сервер `r1.pluma.chat`** на `127.0.0.1:9443 ssl proxy_protocol`, `set_real_ip_from 127.0.0.1; real_ip_header proxy_protocol;`, HTTP/1.1 и HTTP/2, `server_tokens off`, `access_log off`:
  - только `location ~ ^/(o/[0-9a-f]{64}|c/v1)$`, методы GET и HEAD (`OPTIONS` — ответ CORS 204), остальное — 404;
  - **подпись — та же, что у Gcore** (`pkg/cdn/signer_gcore.go`: `base64url(MD5(expires + path + " " + key))` без `=`, `?md5=…&expires=…`): `secure_link $arg_md5,$arg_expires; secure_link_md5 "$secure_link_expires$uri $pluma_token_key";` — nginx хэширует выражение и сравнивает с base64url значения из запроса; пусто — 403, `0` (истёк) — 410 (документация secure_link, пример с `tr +/ -_ | tr -d =`). Значит, сервер подписывает выдачу 202 уже существующим `GcoreSigner` без нового кода подписи; пробы здоровья «испорченная подпись → 403/410» (ADR 0071 §5) работают как есть. Ключ — 32 печатных символа без пробелов (`validGcoreKey`), в `/etc/pluma-ru/secrets.conf` (0600, `map "" $pluma_token_key { default "…"; }` и `$pluma_origin_auth`), подключается `include`; в файлах репозитория ключа нет;
  - **кэш**: `proxy_cache_path /var/cache/pluma-ru levels=1:2 keys_zone=pluma_files:20m max_size=15g inactive=36h use_temp_path=off;` (15 ГБ из 28 свободных; 36 ч — как у Gcore), `slice 1m; proxy_cache_key $uri$slice_range; proxy_set_header Range $slice_range; proxy_cache_valid 200 206 30d; proxy_cache_lock on;` (ключ без строки запроса — все токены делят объект; пример из документации slice берёт `$uri$is_args$args$slice_range`, у нас аргументы — токен, их в ключе нет). 1 МиБ, а не 10 МБ Gcore: клиент берёт блоки по 128 КБ, кусок дешевле тянуть через туннель на 50 Мбит/с. `max_ranges 1`;
  - **к источнику**: `proxy_pass https://10.78.0.2:443` (Caddy дома, сайт `PLUMA_CDN_ORIGIN_DOMAIN`) с `proxy_ssl_server_name on; proxy_ssl_name <источник>; proxy_ssl_verify on; proxy_ssl_trusted_certificate /etc/ssl/certs/ca-certificates.crt;`, `Host` — имя источника, `X-Pluma-Origin-Auth: $pluma_origin_auth` — **отдельное значение выдачи 202** (часть 11), без `Accept-Encoding` и `Cookie`, `proxy_http_version 1.1`, keepalive к источнику, таймауты соединения 5 с, чтения 30 с;
  - ответ: `Cache-Control` и `ETag` источника, CORS — `Access-Control-Allow-Origin` из `map $http_origin` (только `https://<PLUMA_WEB_DOMAIN>` и `https://ru.pluma.chat`), `Vary: Origin`, `Access-Control-Expose-Headers: Content-Range, Content-Length`, `always`; без `Server`;
  - пределы: `limit_conn` 64 на адрес, `limit_req` нет (клиент берёт много частей подряд).
- Проверять, что nginx отдаёт `206` с точным `Content-Range` на произвольный `Range` из закэшированных кусков `slice` — e2e части 11 (контракт клиента ADR 0071 §8).

### Выдача 202 и маршрут по стране (части 8 и 11)
- **Страна** — `authsession.getIpCountry` по `MD.ClientAddr` (часть 8). База GeoIP только в authsession (`app/service/authsession/internal/dao/geoip.go`), как и для `getIpRegion` — копий нет. Клиенты RPC — bff files и bff configuration, оба через `pkg/ipcountry` с LRU.
- **`CDN_ROUTES`**: `RU,BY:202,201;*:201` — записи со странами принимаются. `Router.Pick(ctx, authKeyID, country, now)`: запись со страной клиента, иначе `*`; внутри записи — как сегодня (здоровье, бюджет, избегание по ключу, удержание 10 мин). Удержание помнит запись, по которой выбрано: смена страны (VPN включили или выключили) — новый выбор сразу. Неизвестная страна — `*`.
- **Вторая выдача в `bff.yaml`**: `Id: 202`, `Name: ru`, `Signer: gcore-md5`, `BaseURL: ${CDN_RU_BASE_URL}` (`https://r1.pluma.chat`), `Key: ${CDN_RU_TOKEN_KEY}`, `MonthlyGB: ${CDN_RU_MONTHLY_GB}` (по умолчанию 2000 — предел от разгона, не от провайдера: тариф трафика Timeweb для этого VPS НЕ ПРОВЕРЕН, проверка владельца 8), `OriginAuth: ${CDN_RU_ORIGIN_AUTH}`, `HealthAddr: ${CDN_RU_HEALTH_ADDR}` (`10.78.0.1:443`).
- **Источник** различает выдачи по значению `X-Pluma-Origin-Auth` (у 201 — `CDN_ORIGIN_AUTH`, у 202 — `CDN_RU_ORIGIN_AUTH`; сравнение каждого за постоянное время, без раннего выхода): байты считаются своей выдаче (бюджет и метрики). Сегодня `/o/` считается одной `plain` выдаче (`pkg/cdn/origin.go:123-127`) — это обобщается до списка. Пустой `OriginAuth` у выдачи — общий `CDN_ORIGIN_AUTH` (обратная совместимость), но тогда две `gcore-md5` выдачи с одним значением — ошибка старта (байты не различить).
- **Здоровье 202**: та же проба канарейки раз в минуту, но соединение — на `HealthAddr` (`10.78.0.1:443`, через туннель; SNI и `Host` — `r1.pluma.chat`, сертификат проверяется), потому что исходящий трафик дома выходит с адреса Hetzner, а путь Hetzner → amf из России может быть закрыт так же, как обратный (НЕ ПРОВЕРЕНО). Так проверяются nginx, кэш, подпись и туннель, но не доступность amf из российских сетей — её дают сигнал отката по ключу (ADR 0071 §5) и метрика `pluma_cdn_fallback_total{dc="202"}`. nginx `stream` владельца должен слушать 443 и на `10.78.0.1` (`listen 443` без адреса) — `owner-nginx.sh check`.
- Бюджет 202: оценка отдачи по редиректам (как у 201) и точные байты источника; 80 % — алерт.

### Край для России (части 1 и 2)
- `ru.pluma.chat` → nginx `stream` владельца → `10.78.0.2:8443` (PROXY v1) → Caddy `ru-edge.caddy` → `gnetway:11445` с `X-Pluma-Client-Addr` = адрес из PROXY и `X-Pluma-Edge-Via: ru`. Тот же вход `Edge` шлюза, что у Gcore; секрета Gcore тут нет — доверие держится на том, что 8443 опубликован только на `10.78.0.2` и PROXY принимается только от `10.78.0.1` (`fallback_policy reject`).
- Web K по `https://ru.pluma.chat/` — тот же сайт: страница открыта с хоста из `PLUMA_EDGE_WS_URLS` и ходит в `wss://ru.pluma.chat/apiws` (W-086). CSP `connect-src` сайта Web K должна разрешать `https://r1.pluma.chat` и `https://<префикс>.gcorecdn.com` (файлы).
- `pluma_edge` по стране: `RU` — `order: ["edge","direct"]`, края `ru, gcore`; остальные — `direct` первым, `gcore, ru`. Первый вход нового клиента в России до получения `pluma_edge`: прямой путь «замерзает» (контракт, п. 5), затем Gcore, затем `ru` — до минуты; после первого `help.getAppConfig` порядок из России — `ru` первым. Ставить `ru` первым во встроенном списке (быстрее первый вход в России, но тогда пользователи вне России с закрытым прямым путём сначала идут через Россию) — решение владельца 10.
- Прямой TCP MTProto (10443/5222) на amf не пересылается: адрес Hetzner зашит в клиенты, а голый обфусцированный MTProto к российскому адресу ничем не лучше WebSocket в TLS. Шов на будущее — тот же `forward.sh` на amf, если владелец сделает его основным краем.

### TURN на amf (часть 12)
- **coturn из пакета Ubuntu** (`pluma-ru-turn.service`, свой юнит, не `coturn.service` пакета; пользователь `turnserver`), настройки — общий `teamgramd/deploy/turn/turnserver.conf` (порты 3478 udp/tcp, реле 49160–49659/udp, `no-tls`, `no-tcp-relay`, `total-quota=400`, `denied-peer-ip` всех частных сетей, в том числе `10.0.0.0/8` — реле не достанет туннель) плюс `/etc/pluma-ru/turn.conf` (0640 `root:turnserver`): `listening-ip`, `relay-ip`, `external-ip` = 194.87.226.185 и `static-auth-secret`. Секрет не в командной строке процесса.
- **Отдельный секрет amf** (`CALLS_TURN_SECRETS=ru=<64 hex>`, запись `194.87.226.185:3478:stun+turn@ru`): захват amf даёт выпуск учётных данных только для TURN amf, не для TURN Hetzner. Сегодня секрет один на все записи (`pkg/phonecalls/ice/ice.go`, `Parse(cfg, secret)`).
- **Порядок по стране не нужен и не делается**: обе стороны звонка получают один и тот же список (`phonecalls.Media` в `phone.confirmCall_handler.go:96-102` — `c.media(call)` для обеих сторон), а ICE каждой стороны собирает кандидатов со всех TURN списка и сам выбирает рабочую пару. Что Android, tdesktop, iOS и tweb передают в WebRTC все серверы, а не первый, — по памяти, НЕ ПРОВЕРЕНО; часть 12 проверяет по исходникам и записывает адреса строк. Если окажется, что клиент берёт один TURN, — порядок по стороне звонка (по `MD.ClientAddr` каждой стороны) отдельной правкой; запись в «Решениях к подтверждению» не нужна, пока не проверено.
- `CALLS_ICE_SERVERS=195.201.252.126:3478:stun+turn,194.87.226.185:3478:stun+turn@ru`.
- **UDP из российских сетей**: о замедлении или блокировке звонков WebRTC российскими провайдерами — только сообщения в прессе (по памяти, НЕ ПРОВЕРЕНО); путь «Россия → российский адрес» обычно фильтруется меньше, чем к зарубежным адресам, — тоже не проверено. Замер — звонок двух пользователей из России через amf (проверка владельца 11). TURN по TCP/TLS на 443 (SNI `turn.pluma.chat` в той же карте `stream`) — шов: стоковые клиенты такого вида соединения не знают (`docs/ops/calls.md`), нужен патч клиентов и отдельная задача.
- Память (по памяти, НЕ ПРОВЕРЕНО, замерить после установки): coturn десятки МБ при малой нагрузке, nginx Pluma 20–40 МБ (зона ключей 20 МБ), node-exporter около 20 МБ — вместе меньше 200 МБ из 1,7 ГБ свободных. Docker не ставится: он добавил бы демон и свои цепочки iptables на чужом VPS без выигрыша.

### Мониторинг и выкладка (часть 10)
- `prometheus-node-exporter` из пакета Ubuntu, слушает только `10.78.0.1:9100` (`--web.listen-address`), сборщик textfile: `pluma-ru-wgstat.timer` раз в минуту пишет `pluma_wg_latest_handshake_seconds` и размер кэша `pluma_ru_cache_bytes`.
- Дома: цели `/opt/pluma/prometheus-targets/node-ru.json` (`[{"targets":["10.78.0.1:9100"],"labels":{"node":"amf"}}]`, файлы `node*.json` уже читает `teamgramd/deploy/prometheus/server/prometheus.yml:58-59`). Алерты: amf недоступен 5 мин, рукопожатие старше 3 мин, диск кэша > 90 %, выдача 202 нездорова 10 мин. Правила алертов — в части 10 рядом с существующими правилами Prometheus.
- **Выкладка**: `teamgramd/deploy/ru/deploy.sh` на amf от root (`ssh amf 'NEW=<sha> bash -s' < teamgramd/deploy/ru/deploy.sh`, как `pluma-edge-deploy.sh`): клон репозитория в `/opt/pluma-ru/src` на коммит, установка пакетов при первом запуске (`wireguard-tools`, `coturn`, `prometheus-node-exporter`, `certbot`), файлы в `/etc/pluma-ru/` из шаблонов и `/etc/pluma-ru/env` (0600, только имена в репозитории), `nginx -t -c /etc/pluma-ru/nginx.conf`, перезагрузка своих служб. Ничего из `env` и `secrets.conf` не печатается. nginx владельца скрипт не трогает (это `owner-nginx.sh install`, один раз и по слову владельца).
- **Откат узла**: убрать `RU` из `CDN_ROUTES` и `EDGE_ROUTES` дома и деплой — пользователи из России снова идут по `*`; `owner-nginx.sh remove` убирает строку `include` и файлы Pluma у nginx владельца; `systemctl disable --now pluma-ru-*`.
- Документ оператора — `docs/ops/ru-node.md`.

### Что видит amf и его хостер
- **Видит:** адреса пользователей, которые ходят через amf, время и объёмы соединений; SNI `ru.pluma.chat`/`r1.pluma.chat`; шифр файлов в кэше (≤ 36 ч без запросов) и диапазоны; пакеты SRTP звонков через TURN; внешний адрес домашнего роутера — как источник WireGuard (так же его видит Hetzner).
- **Не видит:** содержимое MTProto (TLS края заканчивается дома), ключи и хэши файлов, голос и видео звонков (SRTP/tde2e).
- Юрисдикция — Россия; хостер может по требованию снять образ диска и трафик — в нём только перечисленное выше. Принятие этого — решение владельца 8.

## Порядок работ
Каждая часть — один исполнитель, своя ветка, свой набор проверок. Части 1 и 2 — сервер, 3–6 — клиенты, зависят только от контракта.

Сервер: части 1, 2 и 8–13; клиенты: 3–6. Распределение по волнам партии — в конце раздела.

1. **Шлюз: вход `Edge`** (с меткой `via`). Файлы: `app/interface/gnetway/internal/config/config.go`, `server/gnet/server_gnet.go`, `server/gnet/server_websocket.go`, `server/gnet/ws/codec.go`, `server/gnet/conn.go`, `server/gnet/ip_limits.go`, `server/gnet/metrics.go`, `timeout_wheel.go` (если нужен второй вид таймера), `teamgramd/etc2/gnetway.yaml`, `teamgramd/etc/gnetway.yaml`, тесты рядом (`*_test.go`); `pkg/mtclient/transport.go` (+ тест) — транспорт WebSocket с заголовками.
2. **Источники краёв и конфигурация** (после части 8). Файлы: `teamgramd/deploy/web/edge-origin.caddy`, `teamgramd/deploy/web/ru-edge.caddy` (новый), `teamgramd/deploy/web/Caddyfile` (импорты, `servers :8443`), `docker-compose.web.yaml` (ссылки при заданных именах, переменные), `app/bff/configuration/internal/appconfig/appconfig.go` и новый `edge.go` (+ тесты), `app/bff/configuration/internal/config`, `app/bff/configuration/internal/svc` (клиент страны части 8), `teamgramd/etc*/bff.yaml` (секция configuration), `test/e2e` (e2e края), `docs/ops/edge.md` (новый). `.env.example`, `docs/ops/deployment.md` и `TESTING-BACKLOG.md` — в части 13, чтобы части одной волны не делили файлы.
3. **Web K, W-086.** `~/pluma-web`: `ci/patch.py`, `ci/patch_test.py`, `ci/configure.sh`, `config/dev/server.env`, `.github/workflows/*` (переменная), `clients/patches/web.md` в репозитории сервера.
4. **Desktop, D-106.** `pluma-desktop`: `ci/patch.py` (+ тест), `ci/configure.sh`, переменная CI; `clients/patches/desktop.md`. Сборка и проверка — CachyOS (Xwayland :9).
5. **Android, A-128.** `~/pluma-android`: `ci/patch.py` (+ тест), `ci/configure.sh`, переменная CI; `clients/patches/android.md`.
6. **iOS, I-133.** `pluma-ios`: `ci/patch.py` (+ тест), `ci/configure.sh`, переменная CI; `clients/patches/ios.md`.
7. **Замеры для России** — отдельная задача после включения края (см. «Эксплуатация»), не блокирует 1–6 и 8–13.
8. **Страна адреса клиента (шов GeoIP).** `authsession.getIpCountry ip:string = String;` — ISO 3166-1 alpha-2 в верхнем регистре по той же базе, что `getIpRegion`, `""` — неизвестно (нет базы, частный адрес, нет страны в записи). Файлы: `app/service/authsession/authsession/authsession.tl.proto` и сгенерированный код (перегенерация — один раз на слияние в партию), `app/service/authsession/internal/core/authsession.getIpCountry_handler.go` (новый), `app/service/authsession/internal/dao/authorization.go` (`IpCountry`, + тест), `app/service/authsession/client/authsession_client.go`; новый пакет `pkg/ipcountry` — `Locator` поверх клиента authsession с LRU (10 000 адресов, 1 ч; ошибка RPC — `""` без кэша, метрика ошибок; адрес в логи не попадает) и тесты.
9. **Туннель дом ↔ amf.** `teamgramd/deploy/edge/tunnel.sh`: второй канал `--link ru` (интерфейс `plumaru`, `10.78.0.0/24`, MTU 1280, порт amf 51820, без таблицы и без пересылки, правила — служба `pluma-tunnel-ru-rules`), роли `node` (amf) и `home`; без `--link` всё как сегодня (Hetzner, `pluma`). Новый `docker-compose.ru.yaml`: Caddy публикует 80, 443 и 8443 на `10.78.0.2`. Тесты скрипта: `shellcheck`, прогон `tunnel.sh … --dry-run` с выводом конфигурации и правил в файлы, сверка с эталоном (`teamgramd/deploy/edge/testdata/`), тест в `tools/` (`go test ./tools/...`) или `test/deploy` — по образцу существующих проверок скриптов, если они есть; иначе новый `teamgramd/deploy/edge/tunnel_test.sh`, запускаемый из `make lint`.
10. **Узел amf: файлы и выкладка** (без Docker). `teamgramd/deploy/ru/`: `nginx.conf` (свой экземпляр nginx), `files.conf.tmpl`, `sni.map.tmpl`, `stream-upstreams.conf`, `acme-http.conf.tmpl`, `turnserver.conf` (общая часть — `teamgramd/deploy/turn/turnserver.conf`, без копии), `pluma-ru-nginx.service`, `pluma-ru-turn.service`, `pluma-ru-wgstat.{service,timer}` и `wgstat.sh` (метрика рукопожатия туннеля для textfile node-exporter), `deploy.sh` (выкладка на коммит), `owner-nginx.sh` (`check|install`: только файлы Pluma в nginx владельца и одна строка `include` в `sni.conf`), правила алертов `teamgramd/deploy/prometheus/server/rules/ru-node.yml` (новый; выдача 202 покрыта общими правилами `cdn.yml` по `dc` — проверить), `docs/ops/ru-node.md` (новый). Проверки: `nginx -t -c` на собранной конфигурации в контейнере nginx той же версии, что в Ubuntu 26.04 (на CachyOS), `shellcheck`, e2e подписи (часть 11).
11. **Выдача 202 и маршрут файлов по стране** (после части 8). `pkg/cdn`: `ParseRoutes` принимает страны, `Router` выбирает запись по стране (`Locator` части 8), `ErrCountryRoutes` убирается; у выдачи свои `OriginAuth` и `HealthAddr`; источник различает выдачи по значению заголовка доступа; тесты. `app/bff/files` (передача адреса клиента в `Pick`), `teamgramd/etc*/bff.yaml` (вторая выдача `CDN_RU_*`, секция Cdn), `docs/ops/cdn.md` (выдача 202). e2e: стенд поднимает nginx с `files.conf.tmpl` части 10 перед источником и проверяет 206, 403, 410, общий кэш.
12. **TURN на amf.** `pkg/phonecalls/ice`: метка секрета у записи (`host:port:виды@метка`), секреты `CALLS_TURN_SECRETS=ru=<hex>` рядом с общим `CALLS_TURN_SECRET`; `app/bff/voipcalls` (конфигурация), `teamgramd/etc*/bff.yaml` (секция Calls), `docs/ops/calls.md`; тесты `ice`. Проверка по исходникам tgcalls (Android `jni/voip/org_telegram_messenger_voip_Instance.cpp`, tdesktop `calls_call.cpp`, tweb `p2P`): клиент использует все TURN из списка, а не первый — записать с адресами строк.
13. **Документы партии.** `.env.example` (все новые переменные 2, 9, 11, 12), `docs/ops/deployment.md`, `docs/ops/home-server.md` (второй туннель), `docs/project/TESTING-BACKLOG.md`, `clients/patches/README.md` (следующие свободные номера).

**Волны партии `batch/20261117`** (от `origin/master`, не больше 4 частей в волне, части с общими файлами — в разных волнах):
1. части 1, 8, 9, 10;
2. части 2, 3, 4, 5;
3. части 11, 6;
4. части 12, 13.

## Тесты
- **Часть 1, модульные:** разбор `X-Pluma-Client-Addr` (один адрес, список, IPv6, порт в значении, мусор, пусто, подделанный левый элемент — берётся правый); `Edge` с `PPV1` — отказ конфигурации; ping через 20 с при молчании, ping не продлевает простой; пределы по адресу считаются по адресу из заголовка (два соединения с разными адресами из одного «края» не делят предел). Хотя бы один тест должен падать без правки.
- **Часть 1, e2e (`test/e2e`, через `pkg/mtclient`):**
  - вход, `updates.getState`, отправка и получение сообщения через вход `Edge` с заголовком;
  - прокси-помощник теста с поведением Gcore — рвёт соединение после 30 с без данных от источника; соединение через него живёт 70 с простоя и получает обновление, отправленное другим клиентом в конце;
  - без заголовка адреса — соединение закрыто, метрика отказа выросла;
  - список сессий (`account.getAuthorizations`) показывает адрес из заголовка, а не адрес Caddy.
- **Часть 2:** `caddy validate` на собранном Caddyfile с заданным и пустым `PLUMA_EDGE_ORIGIN_DOMAIN`; e2e стенда с Caddy (если стенд его поднимает; иначе — проверка владельца 1); модульный тест `pluma_edge` (пусто — ключа нет, неверные значения — отказ старта).
- **Часть 2, край amf:** `caddy validate` с заданным и пустым `PLUMA_RU_EDGE_DOMAIN`; e2e: помощник теста шлёт PROXY v1 с выбранным адресом на 8443 стенда — вход через `ru-edge.caddy`, `account.getAuthorizations` показывает адрес из PROXY; соединение без PROXY или с чужого адреса — закрыто; `pluma_edge` для адреса из `RU` (тестовая база GeoIP стенда, как в тестах authsession) и для прочих — разные объекты и хэши.
- **Часть 8:** `IpCountry` — адрес из России, Германии, частный, IPv6, мусор, без базы; `pkg/ipcountry` — кэш, ошибка RPC не кэшируется, LRU вытесняет. Тест должен падать без правки.
- **Часть 9:** эталон конфигурации и правил обеих ролей обоих каналов; канал `hetzner` без `--link` даёт байт в байт то же, что сегодня (защита прода).
- **Часть 10:** `nginx -t` на собранной конфигурации; тест подписи: URL, подписанный `GcoreSigner` (Go, из `pkg/cdn`), nginx принимает (200/206), испорченный — 403, истёкший — 410, без `md5` — 403; общий кэш при разных токенах (второй запрос — `X-Cache-Status: HIT` только в тестовой сборке конфигурации); `owner-nginx.sh check` на копии `nginx -T` с картой владельца.
- **Часть 11:** `ParseRoutes` со странами; выбор по стране, смена страны сбрасывает удержание, неизвестная — `*`; две выдачи с одним `OriginAuth` — отказ старта; источник считает байты нужной выдаче; здоровье ходит на `HealthAddr` с SNI имени выдачи. e2e: nginx части 10 перед источником стенда, клиент `pkg/mtclient` из «российского» адреса получает редирект на 202, качает части `Range` (206, точный `Content-Range`, хэши сходятся), второй клиент с другим токеном берёт из кэша.
- **Часть 12:** `ice.Parse` с меткой, без секрета метки — отказ старта; учётные данные записи `@ru` считаются своим секретом и проверяются эталоном coturn REST.
- **Части 3–6:** `patch_test.py` каждого клиента: правка ложится на закреплённый апстрим, `verify_*` ловит порчи (`DAMAGE`): потерянная метка, край выше прямого по умолчанию, URL не `wss://`. Сборка каждого клиента на CachyOS (iOS — по возможности macOS). Тестовая сборка с недостижимым прямым адресом (`192.0.2.1`, TEST-NET-1) против стенда с краем — клиент входит через край.

## Проверки владельца
0. **Ресурс 2 Gcore** — сразу после создания: `curl -sS -o /dev/null -w '%{http_code} %{ssl_verify_result}\n' https://<префикс2>.gcorecdn.com/` даёт `ssl_verify_result` = 0 (как проверка 0 в `docs/ops/cdn.md`).
1. **WebSocket через Gcore:** открыть Web K по `https://<префикс2>.gcorecdn.com/`, войти, отправить и получить сообщение; оставить вкладку на 5 минут без действий; написать с телефона — сообщение приходит без перезагрузки.
2. **Адрес клиента:** в «Устройствах» сессия Web K по краю показывает ваше место (город и страну), а не место точки Gcore. Самого адреса «Устройства» не показывают: `authorization.ip` пуст, как у Telegram (с `6791630b1`), место authsession находит по адресу сессии.
3. **Нативный запасной путь:** тестовые сборки Desktop, Android и iOS с недостижимым прямым адресом входят и работают; обычные сборки в обычной сети в «Устройствах» видны с адресом, как сегодня (прямой путь первым).
4. **Log viewer Gcore:** что записано о соединениях к `/apiws` (адрес, время, размер); записать в `docs/ops/edge.md`.
5. **Условия Gcore:** прочитать AUP (https://gcore.com/legal) на предмет запрета прокси или туннелей; ответ записать в бриф.
6. **Из сети с блокировкой** (кто-то из пользователей за пределами России или с блокировкой VPS): вход без VPN.
7. **CAA для `r1.pluma.chat`:** после `certbot register` на amf — записи CAA на имени `r1.pluma.chat` с `accounturi` amf; `dig CAA r1.pluma.chat` показывает только их, `dig CAA ru.pluma.chat` — записи `pluma.chat` (аккаунт дома); выпуск `r1` на amf проходит.
8. **amf как узел:** тариф трафика Timeweb (есть ли предел или плата за трафик), что хостер пишет в журналы; согласие с тем, что видит узел (раздел «Что видит amf»).
9. **Знакомый в России, домашний провайдер:** Web K по `https://ru.pluma.chat/` — вход, сообщения, 5 минут простоя; файл ≥ 1 МБ открывается (в инструментах разработчика — запросы к `r1.pluma.chat` со статусом 206).
10. **Нативный клиент из России без VPN:** вход и работа; в «Устройствах» — место в России; файл ≥ 1 МБ — с `r1.pluma.chat` (метрика `pluma_cdn_redirects_total{dc="202"}` растёт).
11. **Звонок из России:** двое пользователей из России без VPN, затем Россия ↔ Европа; в статистике coturn amf — выделения реле; качество звука.
12. **Сайт `form.amf25.ru`** открывается и продлевает сертификат после `owner-nginx.sh install` (`certbot renew --dry-run` владельца или его способ).
13. **Мобильная сеть в России:** `ru.pluma.chat` в обычном режиме и при «белых списках» (НЕ ПРОВЕРЕНО, есть ли amf в списках).

## Эксплуатация
- **Gcore, ресурс 2** (делает владелец, `docs/ops/edge.md`):
  - домен — Gcore domain, нейтральный префикс; источник — `https://<PLUMA_EDGE_ORIGIN_DOMAIN>`, Host — имя источника;
  - WebSockets — включить; заголовок к источнику `X-Pluma-Edge-Auth` = `PLUMA_EDGE_ORIGIN_AUTH` (`openssl rand -hex 32`);
  - Secure Token — выключен; кэш — «Origin controlled» (Caddy задаёт `no-cache` страницам и `immutable` файлам с хэшем); Always online для `/apiws` не важен (WebSocket не кэшируется);
  - методы — GET и HEAD (рукопожатие WebSocket — GET);
  - WAAP — не подключать (может отвечать 403 на `Upgrade`).
- **DNS:** A-запись `PLUMA_EDGE_ORIGIN_DOMAIN` на адрес Pluma, CAA как у остальных имён (задача 096).
- **`.env` дома:** `PLUMA_EDGE_ORIGIN_DOMAIN`, `PLUMA_EDGE_ORIGIN_AUTH` (секрет), `EDGE_WS_URLS=gcore=wss://<префикс2>.gcorecdn.com/apiws` (с узлом amf — и `ru=…`), при желании `EDGE_ROUTES`, `EDGE_STICKY_S`.
- **Сборки клиентов:** переменная репозитория `PLUMA_EDGE_WS_URLS` в четырёх репозиториях клиентов.
- **Деплой:** обычный; проверка — метрика соединений входа `edge` после проверки 1.
- **Мониторинг:** доля соединений `edge` от всех; закрытия по причине; рост отказов «нет адреса» — тревога (Gcore перестал слать адрес или кто-то обходит Gcore).
- **Трафик:** весь трафик края считается Gcore (вместе с источником), оценка — ADR 0072, «Последствия»; бюджет 1 ТБ общий с файлами (ADR 0071).
- **Смена секрета:** новое значение в Gcore и `.env`, деплой; минуту край отвечает 403, клиенты остаются на прямом пути.
- **Откат:** пустой `PLUMA_EDGE_ORIGIN_DOMAIN` и `EDGE_WS_URLS`, деплой; встроенный адрес края в клиентах перестаёт отвечать — они остаются на прямом.
- **Российский узел amf** (подробно — `docs/ops/ru-node.md`, часть 10). Порядок первого включения:
  1. amf: `tunnel.sh --link ru key`, дом: `tunnel.sh --link ru key`; amf: `tunnel.sh --link ru node <ключ дома> 194.87.226.185`; дом: `tunnel.sh --link ru home <ключ amf> 194.87.226.185`; проверка `wg show plumaru`, `ping -M do -s 1252 10.78.0.1`.
  2. Владелец: A-записи `ru.pluma.chat`, `r1.pluma.chat` → 194.87.226.185 (без проксирования Cloudflare).
  3. amf: `deploy.sh` с `NEW=<sha>`; `owner-nginx.sh check`, затем `install` (nginx владельца: `nginx -t`, `reload`); проверить `form.amf25.ru`.
  4. amf: `certbot register`, владелец — CAA `r1.pluma.chat` (проверка 7), `certbot certonly --webroot` для `r1`.
  5. Дом: `.env` — `PLUMA_RU_TUNNEL_IPV4=10.78.0.2`, `PLUMA_RU_EDGE_DOMAIN=ru.pluma.chat`, `CDN_RU_BASE_URL`, `CDN_RU_TOKEN_KEY`, `CDN_RU_ORIGIN_AUTH`, `CDN_RU_HEALTH_ADDR`, `CDN_RU_MONTHLY_GB`, `CDN_ROUTES=RU,BY:202,201;*:201`, `EDGE_WS_URLS`, `EDGE_ROUTES`, `CALLS_TURN_SECRETS`, `CALLS_ICE_SERVERS`; те же `CDN_RU_TOKEN_KEY`, `CDN_RU_ORIGIN_AUTH`, `CALLS_TURN_SECRETS` (значение `ru`) — в `/etc/pluma-ru/env` amf. Секреты создаются `openssl rand` на одной машине и переносятся по SSH без печати.
  6. Дом: деплой с `docker-compose.ru.yaml`; цели Prometheus `node-ru.json`.
  7. Проверки владельца 9–12.
- **Замеры для России (задача после включения, по плану 092):**
  - страница диагностики 092 добавляет тест `wss://<префикс2>.gcorecdn.com/apiws`: рукопожатие, 1 МБ в обе стороны, удержание 10 минут с ping — отдельный путь на шлюзе `/apiws-probe` без MTProto, только эхо с пределом;
  - пассивно: метрики входа `edge` и `tcp` по ASN клиента (через `app/service/geoip`, IP не хранится): байты до закрытия, доля закрытий на 10–30 КБ;
  - мобильные сети отдельно, обычный режим и «белые списки»; кандидаты российских CDN с WebSocket (Yandex Cloud CDN, VK Cloud, Selectel, NGENIX, CDNvideo — ни один не проверен) — только после замеров.

## Решения к подтверждению
1. **Прямой путь первым, край — запасной** (рекомендация) — или всегда через CDN (одна строка `EDGE_ROUTES=*:edge,direct:gcore,ru`, клиенты не меняются).
2. **Второй ресурс Gcore на домене Gcore** `<префикс2>.gcorecdn.com` (рекомендация: общий домен дороже блокировать) — или свой нейтральный домен с Let's Encrypt Gcore.
3. **Web K доступен вторым адресом через CDN** (весь сайт), вход там отдельный.
4. **Gcore видит адреса, время и объёмы тех, кто идёт через край** (содержимого нет) — как для файлов в ADR 0071.
5. **WEB-прокси Telegram не используем сейчас**, оставляем шов для России.
6. **Звонки при заблокированном VPS не работают** до отдельной задачи (TURN по TLS); для России — TURN на amf (часть 12), если UDP до amf проходит (проверка 11).
7. **Порог «замерзания»** — 15 с без ответа дважды подряд, `sticky` 30 минут.
8. **amf — узел Pluma в российской юрисдикции** для трёх ролей (файлы, край, TURN) с тем, что он видит (раздел «Что видит amf»); TLS файлов на amf (ради кэша), TLS края — дома. **Принято владельцем 2026-11-17.**
9. **Имена `ru.pluma.chat` и `r1.pluma.chat`** (рекомендация: просто и в одной зоне с CAA) — или нейтральный домен, не связанный с Pluma, против блокировки по SNI. **Принято владельцем 2026-11-17: `ru.pluma.chat` и `r1.pluma.chat`.**
10. **Встроенный порядок краёв в сборках** `gcore, ru` (рекомендация: пользователи вне России не идут через Россию до первого `pluma_edge`) — или `ru, gcore` (быстрее первый вход из России).
11. **Маршрут по стране** — `RU,BY` (рекомендация) или только `RU`; Казахстан и другие — по замерам.
12. **Отдельный секрет TURN на amf** (рекомендация) — или общий `CALLS_TURN_SECRET`.
13. **Без Docker на amf**, службы из пакетов Ubuntu и свой экземпляр nginx (рекомендация) — или Docker, как на Hetzner.
14. **Вмешательство в nginx владельца**: файлы Pluma и одна строка `include` в `sni.conf`, файл порта 80 для ACME; выполняется `owner-nginx.sh install` с `nginx -t` перед `reload`.

## Не проверено
- Опция WebSockets на бесплатном/PAYG-плане Gcore и на ресурсе с доменом Gcore.
- Какой заголовок с адресом клиента Gcore передаёт источнику (`X-Forwarded-For` предполагается, проверка владельца 2). Если не передаёт — пределы по адресу на входе `Edge` делятся на точку Gcore; тогда предел входа считается по ключу авторизации, а адрес в «Устройствах» — адрес точки.
- Предел жизни одного соединения WebSocket у Gcore; учитывается ли входящий трафик в лимите.
- AUP Gcore (не прочитана, ADR 0071).
- Отвечают ли pong сами `Core::WebSocketClient` (tdesktop), nv-websocket-client и `URLSessionWebSocketTask` — проверить в коде каждой части.
- Как быстро tgnet уходит с «замёрзшего» прямого соединения (бриф 092) — определяет, хватит ли правил контракта без правки C++ сверх метки.
- Детали WEB-прокси на стороне оператора (ретранслятор tproxy-server) — по вторичным источникам.
- Доступность `*.gcorecdn.com` из России — замер 2026-11-16 был до gcore.com, не до хоста края. Файловый ресурс Gcore у знакомого на домашнем провайдере 2026-11-17 не открылся (одно наблюдение).
- Российский узел amf:
  - есть ли модули `secure_link` и `slice` в nginx Ubuntu 26.04; поддерживает ли `map` модуля `stream` директиву `include`; слушает ли `stream` владельца 443 на всех адресах;
  - отвечает ли Caddy на TLS-ALPN-01 на сервере `:8443` за PROXY (HTTP-01 через порт 80 — основной путь);
  - что записи CAA на `r1.pluma.chat` отменяют для него записи `pluma.chat` (RFC 8659 по памяти);
  - путь Hetzner → amf (исходящий трафик дома) — открыт ли;
  - тариф трафика Timeweb и что хостер журналирует;
  - есть ли адрес amf в мобильных «белых списках»;
  - замедление звонков WebRTC российскими провайдерами и путь UDP «Россия → российский адрес»;
  - что клиенты (tgcalls Android, tdesktop, iOS; tweb) используют все TURN из списка;
  - память coturn, nginx и node-exporter на amf;
  - устойчивость WireGuard между домом и amf против фильтрации (рукопожатие прошло 2026-11-17, одно наблюдение).

## Не в задаче
- Звонки через CDN и TURN по TCP/TLS — отдельно (TURN по UDP на amf — в задаче, часть 12).
- amf как основной край вместо Hetzner (пересылка 10443/5222 и смена адреса в клиентах).
- Порядок TURN по стране или по стороне звонка (только если проверка части 12 покажет, что клиент берёт один TURN).
- HTTP-транспорт (`http_wait`) на сервере.

## Результат
Доступ через края сделан: сервер (части 1, 2, 8–13) и патчи четырёх клиентов (части 3–6) сведены в партию `batch/20261117` (не влита в master, не выкачена, ничего не запушено), полный прогон на ней зелёный. Пока в `.env` дома пусты `PLUMA_EDGE_ORIGIN_DOMAIN`, `PLUMA_RU_EDGE_DOMAIN`, `EDGE_WS_URLS`, `CDN_RU_*`, `CALLS_TURN_SECRETS`, а сборки клиентов собраны без `PLUMA_EDGE_WS_URLS`, всё ведёт себя как до задачи: ключа `pluma_edge` нет, вход `Edge` шлюза (11445) наружу не опубликован, файлы идут по прежнему маршруту. С заданными переменными патченные клиенты идут к серверу через край — MTProto в WebSocket по TLS к второму ресурсу Gcore или к узлу amf (`ru.pluma.chat`), — когда прямой адрес не соединяется или «замерзает», а клиентам из России сервер по стране адреса отдаёт `pluma_edge` с краем `ru` первым, файлы через выдачу 202 узла (`r1.pluma.chat`) и TURN узла. Часть 7 (замеры) — отдельная задача после включения.

Что сделано по частям:
- **1.** Вход `Edge` шлюза (`11b445247`): адрес клиента из `X-Pluma-Client-Addr` (последний разбираемый элемент), без него `Upgrade` получает 400 до счёта пределов; ping каждые `EdgePingInterval` (20 с), не продлевающий простой; метрики с метками `entry` и `via`. `pkg/mtclient`: транспорт WebSocket с заголовками и `DialWebSocketVia` (`dd64e7eec`, `b15ffe884`).
- **2.** Сайты Caddy `edge-origin.caddy` (403 без `X-Pluma-Edge-Auth`) и `ru-edge.caddy` (сервер `:8443`, PROXY только от `10.78.0.1`), подключаются только при заданных именах; ключ `pluma_edge` из `EDGE_WS_URLS`, `EDGE_ROUTES`, `EDGE_STICKY_S` со своим хэшем у записи страны, неверное значение останавливает bff (`581c6f3d2`, `7f28d1b95`); `docs/ops/edge.md`.
- **3–6.** W-086, D-106, A-128, I-133 — записи `clients/patches/*.md`, коммиты веток `task/109-*-edge`: pluma-web `c9228b6`, pluma-desktop `9330c0e`, pluma-android `0340663`, pluma-ios `96eddd8` (не влиты, не выпущены).
- **8.** `authsession.getIpCountry` и `pkg/ipcountry` (LRU 10 000 адресов на 1 ч, ошибка RPC не кэшируется) (`9a7384224`, `30a670be5`).
- **9.** `tunnel.sh --link ru` (интерфейс `plumaru`, `10.78.0.0/24`, MTU 1280), без `--link` — байт в байт как раньше, эталоны `--dry-run` в `tunnel_test.sh` из `make lint`; `docker-compose.ru.yaml` публикует 80, 443, 8443 Caddy только на `10.78.0.2` (`9348fe87c`, `0e86b3f95`).
- **10.** Файлы узла amf без Docker (`teamgramd/deploy/ru/`): свой nginx, coturn, node-exporter, `deploy.sh`, `owner-nginx.sh`, правила `ru-node.yml`, `docs/ops/ru-node.md`; интеграционные тесты в закреплённом образе Ubuntu 26.04 (`pkg/testinfra` `RunImage`) (`1e5f822e9`, `b6f7d8beb`).
- **11.** Выдача 202 со своими `OriginAuth` и `HealthAddr`, `CDN_ROUTES` по странам (ровно одна запись `*`), e2e `TestHttpsCdnRussia` с nginx узла перед источником стенда (`04df8fd07`, `b90d08fed`).
- **12.** Записи `host:port:виды@метка` в `CALLS_ICE_SERVERS` подписываются секретом метки из `CALLS_TURN_SECRETS`; все официальные клиенты отдают WebRTC каждый TURN списка (`67c788a45`, `docs/ops/calls.md`).
- **13.** `.env.example`, `docs/ops/deployment.md` («Края и российский узел»), `docs/ops/home-server.md` (второй туннель), реестр клиентов, `TESTING-BACKLOG.md` (`f767df4b2`).

Проверка (полный прогон на `5e86d59ca`, CachyOS): `go build` и `go vet -tags integration,e2e -unreachable=false` обоих модулей; `go test ./...` — 234 пакета, proto — 3, `./tools/...` — 6 (с `logaudit`), без падений; `-race` — 151 пакет, гонок нет; `make lint` — 0 замечаний, `tunnel_test.sh` ok; интеграционные — 71 из 71 пакета; e2e на свежем стенде — 352 прошли, 0 упали, 2 пропущены (`TestSendLatency`, `TestManualSend`), среди них `TestEdgeGateway*`, `TestEdgeOrigin*`, `TestEdgeAppConfigByCountry`, `TestHttpsCdnRussia`; `make apicoverage-check` чист. Тесты патчей клиентов на Mac: Android 132 OK (5 пропущены), Desktop 128 OK, iOS 151 OK, Web 71 OK (30 пропущены); пропущенные требуют выгрузки апстрима и не запускались.

Найдено и исправлено полным прогоном:
- Lint: SA5008 у тегов go-zero `GnetwayServer.Proto` и `PPV1`, S1021 в `edge_test.go` (`1b979ba36`).
- Три e2e краёв сравнивали `authorization.ip` с адресом клиента, а это поле с `6791630b1` намеренно пусто, как у Telegram (ветки задач e2e только компилировали). Теперь проверяется `authorization.country`, которую authsession находит по адресу сессии: адреса клиентов за краем база стенда относит к России, сети стенда и подделанные заголовки — к Германии (`5e86d59ca`).
- Гонка кэша диалогов, из-за которой мигал `TestReadMentions`: чтение строки диалога, начатое до коммита изменения, клало старую строку в Redis уже после удаления ключа, и `messages.getPeerDialogs` видел старые непрочитанные упоминания до следующего изменения диалога. `GetDialogByPeerDialogId` и `GetDialogListByIdList` переведены на `QueryRowLeased`/`QueryRowsLeased` (`e0ad24aa6`), интеграционный тест `TestDialogLoadOverlappingAChangeDoesNotFillTheCache` падает без правки.
- CI партии (PR sckraaa/pluma#55):
  - `TestHttpsCdnRussia` брал порт `rufiles` в начале прогона, а стенд пересоздаёт контейнер с другим портом. Теперь порты `rufiles` и `web` читаются при каждом соединении (`d500dc10a`).
  - Мигающий `TestGroupCallSFURestart` (падал и до партии): тест ждал весь стек после перезапуска SFU, на медленной машине дольше `AbsentTimeout` (30 с), и сборщик по ADR 0059 §10 справедливо выводил участников из звонка. Теперь тест поднимает только SFU (`974c94a74`).
  - Найдено попутно: после падения SFU bff переподключался к узлу с паузой до 2 минут (`backoff.DefaultConfig` grpc-go) и всё это время отказывал во входе в звонок. Пауза ограничена 2 с (`377d6363c`), юнит-тест `TestRedialOfNodeDown`.
  - В партию влита правка одновременного входа на сайт через бота (PR sckraaa/pluma#49).

Отклонения от брифа:
- Ветки: всё в партии `batch/20261117`; клиенты — четыре ветки `task/109-*-edge`.
- Запрос без адреса на входе `Edge` получает ответ 400 на `Upgrade`, а не просто закрытие.
- «Замерзание» в Android и iOS считается по собственному таймауту ответа клиента (около 12–16 с без байта после запроса), а не отдельным таймером 15 с: апстрим закрывает тихое соединение раньше, таймер не сработал бы. Desktop держит таймер 15 с от первого неотвеченного запроса через перезапуски апстрима. «Дважды подряд» везде заменено окном: второе «замерзание» в пределах 60 с (Android) или 10 минут (iOS, Desktop), что бы ни ответило между ними — на стенде «замерзающий» путь отвечал на пинги и обрывал ряд. Переход на край — примерно через 25–30 с тишины.
- Android: мост слушает всегда, пока у сборки есть края, а при прокси пользователя tgnet к нему не идёт; возврат по `sticky_s` — своя `Datacenter::returnFromPlumaEdge` вместо `resetAddressAndPortNum`. iOS: край — реализация `MTTcpConnectionInterface`, а не отдельный класс соединения. Web K для хоста сервера возвращает сам `PLUMA_SERVER_URL`, а не `wss://<location.host>/apiws`. Причины — записи реестра.
- CORS выдачи 202 шире брифа (строка 153): к двум сайтам добавлен необязательный `PLUMA_GCORE_EDGE_DOMAIN` — сайт края Gcore. Web K, открытый на крае, иначе получал отказ CORS и качал файлы мимо CDN. По той же причине у ресурса файлов Gcore (201) в Access-Control-Allow-Origin — оба сайта краёв (`docs/ops/cdn.md`, шаг 3).

Решения: ADR 0072 принят 2026-11-17. Владелец решил: узел в России — amf (решение 8), имена `ru.pluma.chat` и `r1.pluma.chat` (решение 9). Остальные решения 1–7 и 10–14 реализованы по рекомендациям и ждут подтверждения: прямой путь первым, домен Gcore у ресурса 2, Web K вторым адресом с отдельным входом, `RU,BY` в маршрутах, порядок краёв сборки `gcore, ru`, отдельный секрет TURN узла, без Docker на amf, правка nginx владельца одной строкой `include`.

Известные проблемы:
- Кэшированные списки id диалогов (закреплённые и папки, `cache_dialog.go`) по-прежнему читаются обычными запросами — та же гонка кэша там возможна; для мигающего теста не нужна, не исправлялась.
- «Устройства» показывают место сессии, а не адрес: `authorization.ip` пуст, как у Telegram. Проверки владельца 2 и 10 и строки `TESTING-BACKLOG` сверяют место.
- Состояние «край выбран» живёт в памяти процесса клиента, выбор страны в bff — до часа в кэше `pkg/ipcountry`; так и задумано.
- Desktop: «замерзание» посреди ответа при создании временного ключа не ловится (ни апстримом, ни правкой) до разрыва сервером; на пути, который «замерзает» после ~16 КБ, обмен ключами проходит (D-106).

Не проверено:
- WebSocket Gcore на бесплатном плане и на ресурсе с доменом Gcore, предел жизни соединения, заголовок с адресом клиента (`X-Forwarded-For` предполагается), AUP Gcore (проверки владельца 0–5).
- Установка на amf: `deploy.sh`, `owner-nginx.sh install` на живом nginx владельца, сертификат `r1` и CAA (RFC 8659 — по памяти), юниты systemd на самом узле, TLS-ALPN-01 Caddy за PROXY, память служб узла, тариф и журналы Timeweb (проверки 7, 8, 12).
- Россия с домашних провайдеров (проверки 9–11) и мобильные «белые списки» (проверка 13): есть ли amf в списках, проходит ли WebSocket до `ru.pluma.chat`, UDP до TURN узла.
- Сборки клиентов: Gradle/NDK (Android), Bazel (iOS), Qt на Windows, macOS, Linux (Desktop), Vite (Web); ни один клиент не входил через край против стенда или прода; pong `URLSessionWebSocketTask` (iOS) и поведение nv-websocket-client на JVM — только по исходникам и документации.
