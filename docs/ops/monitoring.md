# Мониторинг и алерты

Решение — ADR 0013. Что делать по алертам доставки — `docs/ops/delivery.md`, по бэкапам — `docs/ops/backup.md`.

## Первый запуск: пуши на телефон

1. Поставить приложение **ntfy** (App Store / Google Play).
2. Придумать тему: `openssl rand -hex 16`. В приложении — «Subscribe to topic», сервер `ntfy.sh` (или свой), эта тема. Кто знает имя темы, может её читать: никому его не давать.
3. На **healthchecks.io** завести бесплатный аккаунт и проверку «Pluma»: Period 1 минута, Grace 3 минуты. В Integrations добавить **ntfy** с той же темой и приоритетом «urgent». Скопировать ping URL (`https://hc-ping.com/…`).
4. В `.env`: `NTFY_TOPIC`, при своём сервере ntfy — `NTFY_URL` и `NTFY_TOKEN`, `HEALTHCHECKS_PING_URL`.
5. `docker compose -f docker-compose-env.yaml up -d` — поднимаются Prometheus, Alertmanager, alertpush, Grafana, Loki, Alloy, node-exporter.
6. Проверить цепочку целиком:
   ```bash
   docker compose -f docker-compose-env.yaml exec alertmanager amtool alert add PlumaTest severity=critical --annotation='summary="проверка алертов"' --alertmanager.url=http://localhost:9093
   ```
   Через 30–60 секунд на телефоне — срочный пуш «PlumaTest», через 5 минут после истечения — «Resolved». В healthchecks.io проверка должна стать зелёной в течение минуты после запуска.

## Что где

| Что | Где |
|---|---|
| Grafana, дашборд «Pluma overview» | `http://127.0.0.1:3001` (через SSH-туннель), логин из `.env` |
| Prometheus, текущие алерты | `http://127.0.0.1:9090/alerts` |
| Alertmanager, заглушить алерт на время работ | `http://127.0.0.1:9093` → Silences |
| Логи | Grafana → Explore → Loki: `{service="msg"}`, ошибки — `{project=~".+"} \|~ "\"level\":\"error\""` |
| Трассы | Jaeger, `http://127.0.0.1:16686` |
| Правила алертов | `teamgramd/deploy/prometheus/server/rules/` |
| Маршруты и получатели | `teamgramd/deploy/alertmanager/alertmanager.yml` |

## Алерты хоста и сервисов

| Алерт | Что делать |
|---|---|
| `ServiceDown` | `docker compose ps`, `docker compose logs --tail=200 <сервис>`. Docker перезапускает сервис сам; если не поднимается — причина в логе |
| `ServiceRestarting` | цикл перезапусков: лог сервиса, чаще всего MySQL, etcd или конфиг |
| `RPCServerErrors` | лог сервиса за последние 10 минут, Grafana → RPC server errors |
| `DiskSpaceLow`, `DiskSpaceCritical` | `docker system df`, `du -sh /var/lib/docker/volumes/*`; при полном диске MySQL и Kafka останавливаются |
| `MemoryLow` | `docker stats`; самые большие — MySQL и Kafka |
| `BackupMissing`, `RestoreCheckFailed` | `docs/ops/backup.md` |
| `GeoIPStale` | база GeoIP старше 10 дней или её нет: `docker logs geoipupdate` (ключ MaxMind, сеть), строки `geoip` в логе authsession; `docs/ops/deployment.md`, раздел «GeoIP». При ошибке авторизации MaxMind исправить ключ или `docker stop geoipupdate`: иначе он перезапускается и обращается к MaxMind раз в минуту. Если GeoIP выключен намеренно (пустой `GeoIPDatabase`), алерт горит всегда — поставить Silence |
| пуш от healthchecks.io «Pluma is down» | сервер, Prometheus или Alertmanager не работают: зайти на сервер; если он недоступен — у хостера |

Алерты без срочности (warning) повторяются раз в 4 часа, critical — раз в час, пока не починено. На время плановых работ — Silence в Alertmanager.

## Коды входа через аккаунт Telegram
Правила — `teamgramd/deploy/prometheus/server/rules/codes.yml`, цель — `ops.codes` по DNS: где сервис не развёрнут, цели нет и ничего не срабатывает. Что делать по каждому алерту — `docs/ops/login-codes.md`.

| Алерт | Что значит |
|---|---|
| `LoginCodesSessionGone` | у аккаунта нет сессии: Telegram её завершил или вход ещё не делали. Коды идут только через администратора |
| `LoginCodesAccountLimited` | Telegram ограничил аккаунт (ожидание или `PEER_FLOOD`) |
| `LoginCodesSenderUnreachable` | bff не достучался до сервиса |
| `LoginCodesContactsNotRemoved` | импортированные контакты не удаляются |

## Почта восстановления облачного пароля
Правила — группа `password_email` в `teamgramd/deploy/prometheus/server/rules/codes.yml`, метрики bff `password_email_send_total{purpose, result}` и `password_email_code_total{purpose, result}` (задача 091, ADR 0058). Без `MAIL_API_KEY` писем нет и ничего не срабатывает. Что делать — `docs/ops/email.md`.

| Алерт | Что значит |
|---|---|
| `PasswordEmailSenderFailing` | Resend больше двух раз за 15 минут не принял письмо: коды не доходят, пользователи получают «подождите» |
| `PasswordEmailSenderRejected` | Resend отказал насовсем: отозван ключ, домен не подтверждён или кончилась месячная квота; код ошибки — в логе bff |
| `PasswordEmailBruteForce` | больше 20 неверных кодов за час: кто-то подбирает; строка `possible brute force` в логе bff называет пользователя |

## Групповые звонки
Правила — `teamgramd/deploy/prometheus/server/rules/groupcalls.yml`, цель SFU — `ops.sfu` по DNS (`sfu:20501`): без `docker-compose.sfu.yaml` цели нет и ничего не срабатывает. Если контейнер `sfu` не запущен, имя пропадает из DNS Docker, цели нет и `ServiceDown` молчит — для этого `GroupCallsSFUDown` (только когда у bff настроены узлы SFU, `groupcalls_sfu_configured`). Метрики SFU — `sfu_*`, bff — `groupcalls_*` (задача 093, ADR 0059 §11). Что делать — `docs/ops/group-calls.md`, раздел «Неполадки». Тревоги сквозного шифрования (группа `groupcalls-e2e`, задача 094, ADR 0060 §5) — по `groupcalls_e2e_participants{call, alarm}` (участники звонка в каждом состоянии, время условия уже учтено в bff) и `sfu_e2e_*`; подробности по участнику — `pluma-admin calls show <call>`, раздел «Шифрование звонка» там же. Отчёты и эпохи, которые узел SFU прислал о звонке не со своего узла или об участнике не в звонке, bff отбрасывает и считает в `groupcalls_sfu_foreign_events_total{type}`, отчёты сверх пределов схемы — в `groupcalls_e2e_reports_total{result="refused"}`; тревоги на них нет, рост значит неверно настроенный или чужой узел.

| Алерт | Что значит |
|---|---|
| `GroupCallsSFUDown` | контейнер SFU не запущен (цели `ops.sfu` нет): войти в звонок и остаться в нём нельзя |
| `GroupCallsSFUUnreachable` | bff не достучался до управления SFU: вход в звонки не проходит |
| `GroupCallsConnectFailing` | больше половины соединений с SFU не проходят ICE или DTLS: адрес, порты, межсетевой экран |
| `GroupCallsUplinkSaturated` | SFU отдаёт больше 90% заданного лимита отдачи, видео идёт в худшем качестве |
| `GroupCallsEventsDropped` | bff не успевает читать события SFU; ушедших из звонка выведет сборщик, но позже |

## CDN больших файлов
Правила — группа `cdn` в `teamgramd/deploy/prometheus/server/rules/cdn.yml` (задача 102, ADR 0065). Метрики дома (bff, только при `CDN_NODE_CONTROL`): `pluma_cdn_redirects_total{result}` (`redirect`, `not_ready`, `unhealthy`, `small`, `error`), `pluma_cdn_push_total{result}`, `pluma_cdn_push_bytes_total`, `pluma_cdn_queue{state}`, `pluma_cdn_node_healthy{dc}`, `pluma_cdn_node_egress_bytes{dc}`, `pluma_cdn_node_egress_limit_bytes{dc}`, `pluma_cdn_reupload_total{result}`. Метрики узла на краю — цель `ops.cdn` из файла `PLUMA_PROMETHEUS_TARGETS/cdn.json` (`10.77.0.1:20511`): `pluma_cdn_served_bytes_total`, `pluma_cdn_requests_total{result}`, `pluma_cdn_handshakes_total{type, result}`, `pluma_cdn_connections`, `pluma_cdn_auth_keys`, `pluma_cdn_cache_bytes`, `pluma_cdn_cache_cap_bytes`, `pluma_cdn_cache_files`, `pluma_cdn_disk_free_bytes`, `pluma_cdn_evictions_total{reason}`, `pluma_cdn_capacity_eviction_age_seconds`, `pluma_cdn_egress_month_bytes`, `pluma_cdn_puts_total{result}`. Ни в одной нет токенов, id файлов и адресов. Без CDN метрик дома нет и ничего не срабатывает. Узел вне строя — не отказ: клиенты качают из дома, поэтому все тревоги группы — warning; общий `ServiceDown` (`general.yml`, critical) на остановленный узел всё равно приходит через 2 минуты, как на `ops.sfu`. Что делать — `docs/ops/cdn.md`, раздел «Неполадки».

| Алерт | Что значит |
|---|---|
| `CdnNodeUnhealthy` | дом 10 минут не пускает узел в редиректы; причина — `cdn_nodes.unhealthy_reason` |
| `CdnNodeDown` | 5 минут нет метрик узла при включённом CDN: узел остановлен, туннель лежит или нет файла цели `cdn.json` (тогда и `ServiceDown` молчит) |
| `CdnPushFailing` | больше половины заливок за 30 минут не прошли (от трёх), или за час появились копии, упавшие насовсем |
| `CdnEgressBudget` | узел отдал 80 % `CDN_MONTHLY_EGRESS_LIMIT_GB` за месяц; на 100 % дом выводит его до 1-го числа |
| `CdnEvictionChurn` | за 6 часов вытеснено три и больше файлов младше суток: мал `CDN_CACHE_BYTES` |

## Перевод сообщений
Правила — `teamgramd/deploy/prometheus/server/rules/translate.yml`, метрики bff `translate_*` (задача 101, ADR 0064 §13), панели — строка «Translation» дашборда «Pluma overview». Без `TRANSLATE_URL` bff метрик перевода не публикует и ничего не срабатывает. Что делать — `docs/ops/translation.md`, раздел «Тревоги».

| Алерт | Что значит |
|---|---|
| `TranslateEngineDown` | движок 10 минут недоступен (не отвечает, 5xx, 429, отверг токен): все переводы получают «подождите» |
| `TranslateItemsFailing` | движок отвечает, но переводы в нём не получаются: отказ, срок, ответ без перевода |
| `TranslateTooSlow` | движок не успевает: больше половины пачек чата отвечены «подождите» |
| `TranslateCacheFailing` | кэш переводов в MySQL не читается или не пишется: каждый перевод снова идёт в движок |

## Карты в геопозициях
Правила — группа `maps` в `teamgramd/deploy/prometheus/server/rules/maps.yml`, метрики bff `webmap_render_total{result}`, `webmap_render_seconds`, `webmap_cache_total{result}`, `webmap_flood_total`, `webmap_limit_errors_total` (задача 100, ADR 0063 §8). Цели у самого сервиса `maps` нет: он в закрытой сети `maps_net`, куда Prometheus не входит, и метрик у tileserver-gl нет, поэтому `ServiceDown` о нём молчит — о нём говорят рендеры bff. Без `MAPS_URL` карты не рисуются и ничего не срабатывает; попадания в кэш Redis рендеров не вызывают. Что делать — `docs/ops/maps.md`, раздел «Неполадки».

| Алерт | Что значит |
|---|---|
| `MapsRendererDown` | 15 минут ни одной нарисованной карты при запросах: контейнер `maps` остановлен или не стартует (нет файла данных, нехватка памяти), либо bff пересоздан без `docker-compose.maps.yaml` и не видит `maps`. Новые геопозиции показываются без карты |
| `MapsRenderFailing` | больше 20 % рендеров за 10 минут (от 10 запросов) не удаются: `timeout` — перегрузка или нехватка CPU, `error` — стиль, которого нет в образе, или сбой сервиса, `invalid` — картинка не того размера |

## Встроенные инлайн-боты `@gif` и `@pic`
Правила — группа `inline_bots` в `teamgramd/deploy/prometheus/server/rules/codes.yml`, метрики bff (задача 099, ADR 0062):
- `inline_builtin_requests_total{bot, result}`;
- `inline_import_total{kind, result}`, `inline_import_bytes_total{kind}`;
- `inline_bot_misconfigured{bot}`;
- `webfile_requests_total{kind, result}`, `webfile_cache_bytes`.

Что делать по каждому алерту и что значат метки — `docs/ops/inline-bots.md`, раздел «Метрики и алерты».

| Алерт | Что значит |
|---|---|
| `InlineProviderFailing` | у бота за 15 минут больше половины запросов кончились ошибкой (и их не меньше трёх). Пользователи видят «ничего не найдено». У `@pic` обычно SearXNG не работает или все движки картинок блокируют адрес |
| `InlineBotMisconfigured` | учётная запись бота расходится с реестром, обычно его имя занято. Бот отвечает `BOT_INLINE_DISABLED`, вкладка GIF пуста, если это `@gif` |
| `InlineImportBudgetSpent` | дневной бюджет импорта медиа кончился до 18:00 UTC: новые картинки не импортируются до 00:00 UTC |
