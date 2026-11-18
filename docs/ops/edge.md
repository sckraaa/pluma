# Края: вход клиентов через CDN и российский узел

Решение — ADR 0072, задача 109 (`docs/tasks/109-access-through-cdn.md`). Патченные клиенты, у которых прямой адрес сервера закрыт или «замерзает», подключаются к **краю**: WebSocket по TLS `wss://<хост края>/apiws`, внутри — тот же обфусцированный MTProto, что по TCP. Край — только труба: MTProto зашифрован ключом авторизации между клиентом и домом. Краёв два:

- **Gcore** — второй ресурс Gcore на домене Gcore (`<префикс2>.gcorecdn.com`), источник — сайт Caddy дома `edge-origin.caddy`;
- **ru** — российский узел amf (`ru.pluma.chat`), чистая TCP-труба до Caddy дома, сайт `ru-edge.caddy` (узел — `docs/ops/ru-node.md`).

Оба ведут на вход `Edge` шлюза (`gnetway:11445`, только сеть Docker, не публикуется). Адрес клиента шлюз берёт из `X-Pluma-Client-Addr`, который ставит только Caddy.

## Как устроено
- **Gcore → `edge-origin.caddy`** (имя `PLUMA_EDGE_ORIGIN_DOMAIN`):
  - запрос без верного `X-Pluma-Edge-Auth` (его добавляет ресурс Gcore) — 403 до разбора пути;
  - `/apiws` → `gnetway:11445`; `X-Pluma-Client-Addr` = `X-Forwarded-For` запроса (последний элемент дописал Gcore — шлюз берёт последний правильный адрес), `X-Pluma-Edge-Via: gcore`, секрет дальше не идёт;
  - остальное — Web K из `/srv` с `Cache-Control: no-cache` (Gcore перепроверяет по ETag, новая версия видна сразу);
  - без журнала доступа, без `Server` и `Via`.
- **amf → `ru-edge.caddy`** (имя `PLUMA_RU_EDGE_DOMAIN`, порт 8443):
  - nginx `stream` узла передаёт TLS клиента как есть, с PROXY v1, по туннелю `plumaru` на `10.78.0.2:8443` (`docker-compose.ru.yaml`); сертификат `ru.pluma.chat` — дома;
  - сервер Caddy `:8443` (блок `servers :8443` в `Caddyfile`) принимает PROXY только от `10.78.0.1/32`, остальные соединения закрывает; HTTP/3 на нём выключен; сервер 443 не затронут;
  - запрос с адресом самого узла (узел подключился без PROXY) обрывается: клиент не может оказаться посчитан под адресом узла;
  - `/apiws` → `gnetway:11445`; `X-Pluma-Client-Addr` = адрес из PROXY, `X-Pluma-Edge-Via: ru` (то же имя от клиента затирается);
  - остальное — Web K из `/srv`; без журнала доступа, без `Server` и `Via`.
  - Секрета нет: защита — порт 8443 опубликован только на адресе туннеля, а PROXY принимается только от узла.
- **Ключ `pluma_edge` в `help.getAppConfig`** (контракт клиентов — бриф 109, п. 6):
  `{"v":1,"ws":["wss://…/apiws",…],"order":["direct","edge"],"sticky_s":1800}`.
  - Значение выбирается по стране адреса клиента (`authsession.getIpCountry` по базе GeoIP, кэш в bff на час): запись страны из `EDGE_ROUTES`, иначе — запись `*`. Неизвестная страна (нет базы, частный адрес, authsession не ответил) — `*`.
  - У каждой записи свой хэш: клиент, сменивший страну (включил или выключил VPN), получает другое значение при следующем `help.getAppConfig`.
  - Страна не запрашивается вовсе, если в `EDGE_ROUTES` только запись `*`.
  - Пустой `EDGE_WS_URLS` — ключа нет, клиенты пользуются списком, встроенным в сборку.

## Переменные `.env`
| Переменная | Что | Пусто |
|---|---|---|
| `PLUMA_EDGE_ORIGIN_DOMAIN` | имя источника края Gcore (A-запись на адрес Pluma, CAA как у остальных имён) | сайта нет |
| `PLUMA_EDGE_ORIGIN_AUTH` | значение `X-Pluma-Edge-Auth`, ровно 64 hex (`openssl rand -hex 32`) | обязателен при заданном имени, иначе Caddy не стартует |
| `PLUMA_RU_EDGE_DOMAIN` | `ru.pluma.chat` — край узла amf | сайта нет |
| `EDGE_WS_URLS` | края по именам: `gcore=wss://<префикс2>.gcorecdn.com/apiws,ru=wss://ru.pluma.chat/apiws` | ключа `pluma_edge` нет |
| `EDGE_ROUTES` | записи `<страны или *>:<порядок>:<края>` через `;` | `*:direct,edge:<все края в порядке EDGE_WS_URLS>` |
| `EDGE_STICKY_S` | сколько секунд клиент держится края после перехода, 60–86400 | 1800 |

Правила, которые bff проверяет при старте (ошибка — bff не стартует):
- имя края — 1–16 символов `a-z0-9`, без повторов; адрес — `wss://<DNS-имя>/apiws`, порт 443 или не указан, без пользователя, запроса и фрагмента, не IP; один адрес не у двух имён (клиентам адрес уходит в одном виде: имя хоста строчными, без `:443`);
- страны — коды ISO 3166-1 alpha-2 в верхнем регистре (та же грамматика, что `CDN_ROUTES`); страна не повторяется; запись `*` ровно одна;
- порядок — `direct,edge` или `edge,direct`; в записи от 1 до 4 краёв, каждый из `EDGE_WS_URLS` и один раз.

Пример с узлом amf (рекомендация брифа, решения владельца 1, 10, 11):
```
EDGE_WS_URLS=gcore=wss://<префикс2>.gcorecdn.com/apiws,ru=wss://ru.pluma.chat/apiws
EDGE_ROUTES=RU,BY:edge,direct:ru,gcore;*:direct,edge:gcore,ru
```
«Всегда через CDN» для всех — `EDGE_ROUTES=*:edge,direct:gcore,ru`; клиенты не меняются.

## Ресурс Gcore 2 (делает владелец)
1. **CDN → CDN resources → Create CDN resource**, домен — **Gcore domain**, нейтральный префикс; **Origin** = `PLUMA_EDGE_ORIGIN_DOMAIN`, протокол HTTPS; **Host header override** = то же имя.
2. Сразу после создания — проверка 0 брифа: `curl -sS -o /dev/null -w '%{http_code} %{ssl_verify_result}\n' https://<префикс2>.gcorecdn.com/` даёт `ssl_verify_result` = 0 (ответ — 403, пока заголовок не задан).
3. **WebSockets** — включить (для ресурса выключено по умолчанию). Gcore закрывает WebSocket через 30 с без данных от источника; шлюз шлёт ping каждые 20 с.
4. **Заголовки к источнику:** `X-Pluma-Edge-Auth` = `PLUMA_EDGE_ORIGIN_AUTH`. Значение отдельное от `CDN_ORIGIN_AUTH` файлов.
5. **Secure Token** — выключен (он ответил бы 403 на `Upgrade`); **кэш** — «Origin controlled»; методы — GET и HEAD; **WAAP** не подключать.

## Включение
1. DNS: A-запись `PLUMA_EDGE_ORIGIN_DOMAIN` на адрес Pluma; CAA как у остальных имён (задача 096).
2. `.env`: `PLUMA_EDGE_ORIGIN_DOMAIN`, `PLUMA_EDGE_ORIGIN_AUTH`, `EDGE_WS_URLS` (сначала только `gcore=…`), при желании `EDGE_ROUTES`, `EDGE_STICKY_S`. Деплой обычный.
3. Ресурс Gcore (выше), проверки 1 и 2 брифа.
4. Узел amf — по `docs/ops/ru-node.md`; затем `PLUMA_RU_EDGE_DOMAIN=ru.pluma.chat`, `ru=…` в `EDGE_WS_URLS`, `EDGE_ROUTES` со странами, деплой с `docker-compose.ru.yaml`.

Сертификат `ru.pluma.chat` выпускает домашний Caddy: HTTP-01 приходит на порт 80 узла и проксируется на `10.78.0.2:80`. Отвечает ли Caddy на TLS-ALPN-01 на сервере `:8443` за PROXY — НЕ ПРОВЕРЕНО; HTTP-01 от этого не зависит.

## Проверка после деплоя
- `docker compose logs web` — нет ошибок разбора Caddyfile; при неверном `PLUMA_EDGE_ORIGIN_AUTH` контейнер не стартует с сообщением о 64 hex.
- Без заголовка: `curl -s -o /dev/null -w '%{http_code}\n' https://<PLUMA_EDGE_ORIGIN_DOMAIN>/apiws` → 403.
- Метрики шлюза (Prometheus):
  - `gnetway_entry_conns{entry="edge",via="gcore"|"ru"}` — открытые соединения края;
  - `gnetway_entry_closed_total{entry="edge",…}` — закрытия по причине, `gnetway_entry_conn_bytes` — байты до закрытия;
  - `gnetway_edge_pings_total{via=…}` — ping шлюза;
  - `gnetway_edge_no_client_addr_total{via=…}` — отказы без адреса клиента. Рост — тревога: Gcore перестал слать `X-Forwarded-For`, или кто-то ходит на вход мимо Caddy.
- `pluma_ipcountry_errors_total` — authsession не ответил о стране: такие клиенты получают запись `*`.

## Смена секрета
Новое значение — в Gcore и в `.env`, деплой. Около минуты край отвечает 403; клиенты остаются на прямом пути или на другом крае.

## Откат
- Край Gcore: пустые `PLUMA_EDGE_ORIGIN_DOMAIN` и `gcore=…` из `EDGE_WS_URLS`, деплой.
- Край amf: пустой `PLUMA_RU_EDGE_DOMAIN`, `ru=…` из `EDGE_WS_URLS` и страны из `EDGE_ROUTES`, деплой (узел — `docs/ops/ru-node.md`, «Откат»).
- Всё сразу: пустой `EDGE_WS_URLS` — ключа `pluma_edge` нет. Встроенные в сборки края перестают отвечать, клиенты остаются на прямом пути.

## Что видит край
Gcore и узел amf видят адреса, время и объёмы соединений тех, кто идёт через край, и имя хоста края; содержимого MTProto не видят (TLS края Gcore заканчивается на Gcore, но внутри — MTProto под ключом авторизации; TLS края amf заканчивается дома). Что пишет Log viewer Gcore о соединениях к `/apiws` — проверка владельца 4, записать сюда.

## Не проверено
- Какой заголовок с адресом клиента Gcore передаёт источнику (предполагается `X-Forwarded-For`, проверка владельца 2).
- Опция WebSockets на бесплатном/PAYG-плане Gcore и на ресурсе с доменом Gcore; предел жизни одного соединения.
- TLS-ALPN-01 Caddy на `:8443` за PROXY.
