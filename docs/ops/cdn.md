# CDN больших файлов

CDN не включается до задачи 107 (ADR 0071, HTTPS-CDN на Gcore с источником дома). Узел MTProto на VPS и `dc_options` задачи 102 сняты до выката (задача 107, часть 0): образа `pluma-cdn`, сервиса `cdn` края, наложения `docker-compose.cdn.yaml`, цели Prometheus `ops.cdn` и тревог группы `cdn` больше нет. `help.getConfig` снова отдаёт пустой `dc_options` (`docs/ops/clients.md`, «Адрес сервера»).

Что осталось от 102 и спит:
- домашний код `pkg/cdn` в bff и таблицы `cdn_files`, `cdn_replicas`, `cdn_nodes` (`migrate-20261120-cdn.sql`): migrate их создаёт, они остаются пустыми;
- переменные `CDN_*` в `.env` — **оставить пустыми**. Пока пуст `CDN_NODE_CONTROL`, CDN выключен целиком: ни редиректов, ни заливки, ни метрик `pluma_cdn_*`, `help.getCdnConfig` пуст, все файлы клиенты качают из дома.

Включение, проверка и откат HTTPS-CDN появятся здесь вместе с задачей 107.
