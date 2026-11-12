# Память

Сервер Pluma (Hetzner CX33, 7,6 ГБ) общий: на нём же туннель НАВИгатора (sshd и nginx на `127.0.0.1:8081/8082`), его резервные копии в `/root/neo-backups` и контейнер `lawyer-app-1` с собственным лимитом в 1 ГБ. Pluma не должна отнимать у них память, как бы ни вела себя сама. Защита в три слоя.

## 1. Лимит у каждого контейнера

У каждого сервиса во всех compose-файлах есть `mem_limit` и `memswap_limit` с тем же значением, то есть без подкачки. Если сервис выйдет за лимит, ядро убьёт только его, а Docker перезапустит по `restart`. Тест `pkg/conf` (`TestComposeMemory`) не даёт добавить сервис без лимита.

| Сервис | Лимит | Обычно (p99 за 7 дней, 05.11.2026) |
|---|---|---|
| dfs | 1536m | 93 МБ; дочерние ffmpeg — в том же лимите |
| session, gnetway | 512m | 240 МБ |
| bff, biz | 384m | 125 МБ |
| msg, sync, push, media, authsession, migrate | 256m | 90–120 МБ |
| idgen, status | 192m | 85 МБ |
| storage-init | 128m | — |
| releases, iosinstall | 512m | — |
| web, catalog-receiver | 256m | — |
| mysql | 2048m | 380 МБ; под нагрузкой e2e (восстановление дампа и поток сообщений) 1 ГБ не хватило |
| kafka | 1024m | 680 МБ при куче 1 ГБ, теперь куча 512 МБ |
| redis | 1280m | `maxmemory 1gb` с запасом на fork при перезаписи AOF |
| backup | 1024m | mysqldump, временный mysqld для проверки восстановления, restic |
| kafka-init, garage, prometheus, loki, grafana | 512m | |
| alloy | 384m | 130 МБ |
| jaeger | 512m | хранит в памяти последние 10 000 трассировок; 50 000 не помещались в 256 МБ под нагрузкой e2e |
| etcd | 256m | |
| alertmanager, geoipupdate | 128m | |
| alertpush, node-exporter | 64m | |
| stt (необязательный, `docker-compose.stt.yaml`) | 768m (`STT_MEMORY`) | GigaAM-v3 e2e_rnnt int8: 340 МБ после загрузки, пик 415–435 МБ на записях от 90 с до часа — от длины не зависит (замер 2026-11-12 на машине сборки, `docs/ops/transcription.md`); 2 CPU. Parakeet TDT v3 — `STT_MEMORY=2g` (пик 1,27 ГБ) |

Kafka запускается с кучей 512 МБ (`KAFKA_HEAP_OPTS`) вместо гигабайта по умолчанию.

## 2. Go держит кучу ниже лимита

Все Go-сервисы стартуют через `commands.Run` (`third_party/teamgram-marmota/pkg/commands/memlimit.go`). При старте он читает лимит своего cgroup и задаёт рантайму мягкий лимит в 80% от него, как `GOMEMLIMIT`. Когда процесс подходит к лимиту, сборщик мусора работает чаще, а не ждёт, пока куча вырастет вдвое. Остальные 20% оставлены дочерним процессам (ffmpeg у dfs) и запасу. Явная переменная `GOMEMLIMIT` в окружении сервиса имеет приоритет. В логе при старте: `memory limit N MiB of the container's M MiB`.

## 3. Один потолок на всю Pluma

Сумма лимитов отдельных контейнеров больше, чем можно отдать Pluma: это потолки, а не резерв. Общий потолок задаёт systemd-slice `pluma.slice` (`teamgramd/deploy/systemd/pluma.slice`): `MemoryMax` = память сервера − 2 ГБ хосту − лимиты чужих контейнеров. Для CX33: 7,6 − 2 − 1 = 4,5 ГБ, без подкачки.

Установка (один раз):

```bash
cp /opt/pluma/teamgramd/deploy/systemd/pluma.slice /etc/systemd/system/pluma.slice
systemctl daemon-reload
# в .env:
PLUMA_CGROUP_PARENT=pluma.slice
```

Затем пересоздать контейнеры обычным деплоем (`up -d` обеих команд compose). Проверка:

```bash
systemctl status pluma.slice                    # Memory: … (max: 4.5G)
docker inspect -f '{{.HostConfig.CgroupParent}} {{.HostConfig.Memory}}' pluma-dfs-1
```

Без `PLUMA_CGROUP_PARENT` (машина разработчика, CI, e2e) контейнеры остаются в cgroup Docker по умолчанию, а их собственные лимиты действуют как обычно.

## Хост

sshd и nginx защищены от OOM drop-in'ами `/etc/systemd/system/{ssh,nginx}.service.d/oom.conf` (`OOMScoreAdjust=-900`). Если память всё же кончится, первыми ядро убьёт контейнеры, а не туннель. Файл `ssh.service.d/nofile.conf` принадлежит туннелю, его не трогать.

## Изменить лимит

Поправить значение в compose-файле (`mem_limit` и `memswap_limit` вместе) и в таблице выше, затем деплой. Чтобы узнать, сколько сервис занимает:

```bash
docker stats --no-stream
# история Go-сервисов в Prometheus:
docker exec prometheus wget -qO- 'http://localhost:9090/api/v1/query?query=max_over_time(process_resident_memory_bytes[30d])'
```

Если контейнер упирается в лимит, Docker показывает у него `OOMKilled: true` (`docker inspect`), а в журнале ядра (`journalctl -k`) появляется `Memory cgroup out of memory`.
