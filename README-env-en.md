# High-Performance Components Environment (docker-compose-env2)

A self-contained environment stack based on `docker-compose-env2.yaml`, independent of `docker-compose-env.yaml`. It includes monitoring, logging, message queue, storage, and related components.

## Components

| Component | Description |
|-----------|-------------|
| kafka | Message queue (KRaft mode, no Zookeeper) |
| etcd | Configuration and discovery |
| redis | Cache |
| mysql | Relational database (MySQL 8.0) |
| minio / minio-mc | Object storage and bucket initialization |
| jaeger | Distributed tracing |
| prometheus | Metrics collection |
| node-exporter | Host metrics export |
| grafana | Visualization and dashboards |
| elasticsearch | Search and log storage |
| kibana | Log/data visualization |
| filebeat | Log collection |
| go-stash | Log pipeline (Kafka → Elasticsearch) |

## Prerequisites

- Docker and Docker Compose (Compose V2: `docker compose`)
- For Elasticsearch, ensure `vm.max_map_count` ≥ 262144 on Linux: `sysctl -w vm.max_map_count=262144`

## Environment Variables

- Template: `.env.example`
- Usage: copy to `.env` and adjust as needed (change secrets in production).

```bash
cp .env.example .env
# Edit .env for MYSQL_*, MINIO_*, GRAFANA_*, etc.
```

There are no default passwords. Every secret (`MYSQL_ROOT_PASSWORD`, `MYSQL_PASSWORD`, `REDIS_PASSWORD`, `MINIO_ROOT_USER`, `MINIO_ROOT_PASSWORD`, `GRAFANA_ADMIN_PASSWORD`, `SERVER_RSA_KEY_FINGERPRINT`) must be set in `.env`; compose refuses to start while one is missing. The services connect as MySQL user `teamgram` to database `teamgram`. Setup and the server RSA key: `docs/ops/security-setup.md`.

## Start and Stop

```bash
# Start all services (detached)
docker compose -f docker-compose-env2.yaml up -d

# Check status
docker compose -f docker-compose-env2.yaml ps

# Stop
docker compose -f docker-compose-env2.yaml down

# Stop (data under ./data/ is kept)
docker compose -f docker-compose-env2.yaml down
```

Compose reads `.env` from the current directory; without it (or with an empty secret) it refuses to start.

## Ports and Access URLs

All services bind to `127.0.0.1` only (local access).

| Service | Port | URL |
|---------|------|-----|
| Jaeger UI | 16686 | http://127.0.0.1:16686 |
| Prometheus | 9090 | http://127.0.0.1:9090 |
| Grafana | 3000 | http://127.0.0.1:3000 |
| Kibana | 5601 | http://127.0.0.1:5601 |
| MinIO API | 9000 | http://127.0.0.1:9000 |
| MinIO Console | 9001 | http://127.0.0.1:9001 |
| MySQL | 3306 | 127.0.0.1:3306 |
| Redis | 6379 | 127.0.0.1:6379 |
| etcd | 2379 | http://127.0.0.1:2379 |
| Kafka | 9092 | 127.0.0.1:9092 |
| Elasticsearch | 9200 | http://127.0.0.1:9200 |
| Node Exporter | 9100 | http://127.0.0.1:9100 |

## Configuration

These services rely on config files under `teamgramd/deploy/`; the files must exist and be valid:

- **Prometheus**: `teamgramd/deploy/prometheus/prometheus.yml` (scrapes Prometheus and node-exporter)
- **Filebeat**: `teamgramd/deploy/filebeat/filebeat.yml`
- **Go-Stash**: `teamgramd/deploy/go-stash/config.yaml` (configure Kafka topics and Elasticsearch index for your use case)

To run project SQL init for MySQL, add a volume to the `mysql` service in `docker-compose-env2.yaml`, for example:

```yaml
volumes:
  - ./data/mysql:/var/lib/mysql
  - ./teamgramd/sql:/docker-entrypoint-initdb.d:ro
```

## Network

- Name: `teamgram_net`
- Driver: bridge
- Subnet: `172.20.0.0/16`

## Data Persistence

Data is stored in **local directories** under the project `data/` folder:

| Service | Host path |
|---------|-----------|
| kafka | `./data/kafka` |
| etcd | `./data/etcd` |
| redis | `./data/redis` |
| mysql | `./data/mysql` |
| minio | `./data/minio` |
| prometheus | `./data/prometheus` |
| grafana | `./data/grafana` |
| elasticsearch | `./data/elasticsearch` |

Docker Compose creates these directories on first run. Running `down` does not remove them; back up or migrate by copying the `data/` directory.

## Notes

- **Go-Stash**: If the image `kevwan/go-stash` is not available, use your own or another image and keep `teamgramd/deploy/go-stash/config.yaml` in sync with the runtime.
- **Grafana**: First login uses `GRAFANA_ADMIN_USER` / `GRAFANA_ADMIN_PASSWORD` from `.env`. Add Prometheus, Elasticsearch, etc. as data sources in Grafana to view metrics and logs.
