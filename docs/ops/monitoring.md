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
| пуш от healthchecks.io «Pluma is down» | сервер, Prometheus или Alertmanager не работают: зайти на сервер; если он недоступен — у хостера |

Алерты без срочности (warning) повторяются раз в 4 часа, critical — раз в час, пока не починено. На время плановых работ — Silence в Alertmanager.
