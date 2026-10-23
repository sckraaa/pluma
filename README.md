# Pluma (a fork of Teamgram Server)

[中文](README-zh.md) | **English**

> **This repository is a fork of [teamgram/teamgram-server](https://github.com/teamgram/teamgram-server)**, reworked for a small, closed, self-hosted deployment used with the official Telegram clients. It is not affiliated with the Teamgram authors or with Telegram. The upstream code keeps its license, the Apache License 2.0 ([LICENSE](LICENSE)), and its copyright notices; our changes start after upstream commit `708fb953` and are listed in [NOTICE](NOTICE), [docs/project/STATUS.md](docs/project/STATUS.md) and the git history.
>
> What the fork changes so far:
>
> - **Closed sign-in**: only phones on an allowlist can sign in or sign up; one-time codes are issued by the administrator (`tools/pluma-admin`), with rate limits.
> - **Security**: a rotatable server RSA key checked at startup, verified PFS (`auth.bindTempAuthKey`), limits against pre-auth DoS, bounded TL and gzip decoding, logs without keys, codes or message text, secrets only from `.env`, internal ports closed.
> - **Tests and CI**: unit and race tests, lint, govulncheck, integration tests on real MySQL/Redis/Kafka and an end-to-end test of the whole `docker compose` stack over MTProto ([docs/ops/testing.md](docs/ops/testing.md)).
> - The vendored `github.com/teamgram/proto` lives in `third_party/teamgram-proto`, with our patches as separate commits.
>
> The rest of this README is the upstream one and does not describe all of the above yet: for a deployment of the fork, follow [docs/ops/security-setup.md](docs/ops/security-setup.md) (in Russian) as well. Project documents under `docs/project`, `docs/adr`, `docs/tasks` and `docs/ops` are in Russian.

---

Unofficial open-source [MTProto](https://core.telegram.org/mtproto) server implementation in Go. Compatible with Telegram clients; supports self-hosted deployment.

## Features

- **MTProto 2.0**
  - **Abridged**
  - **Intermediate**
  - **Padded intermediate**
  - **Full**
- **API Layer: 228**
- **Core features**
  - **private chat**
  - **basic group**
  - **contacts**
  - **web**

## Architecture

![Architecture](docs/image/architecture-001.png)

- [Architecture Docs](https://deepwiki.com/teamgram/teamgram-server) - `DeepWiki` teamgram/teamgram-server
- [Architecture (specs)](specs/architecture.md) — service topology, data flow, and ports  
- [Service topology and configuration](docs/service-topology.md) — ports, infrastructure dependencies, call graph, and Mermaid diagram ([中文](docs/service-topology-zh.md))

## Prerequisites

| Component | Purpose |
|-----------|---------|
| [MySQL](https://www.mysql.com/) 5.7+ / 8.0 | Primary data store |
| [Redis](https://redis.io/) | Cache, session, deduplication |
| [etcd](https://etcd.io/) | Service discovery & config |
| [Kafka](https://kafka.apache.org/) | Message & event pipeline |
| [MinIO](https://minio.io/) | Object storage |
| [FFmpeg](https://ffmpeg.org/) | Media transcoding (on server) |

Detailed versions and optional monitoring stack: [Dependencies and runtime (specs)](specs/dependencies-and-runtime.md).

---

## Manual installation

For running the server from source (Go build), follow the step-by-step guides:

- **[Manual installation (Linux)](docs/install-manual-linux.md)** — CentOS, Fedora, Ubuntu/Debian
- **[Manual installation (macOS)](docs/install-manual-macos.md)** — Intel and Apple Silicon

Requires Go 1.27 (`go.mod` pins `toolchain go1.27.3`; Go 1.21+ downloads it automatically with `GOTOOLCHAIN=auto`). You must install and configure dependencies (MySQL, Redis, etcd, Kafka, MinIO, FFmpeg), initialize the database and MinIO, then build and run.

---

## Docker installation

For running the full stack with Docker. **No manual data initialization:** the dependency stack initializes the database (via mounted SQL) and MinIO buckets (via `minio-mc`) on first start.

### 1. Clone

```bash
git clone https://github.com/teamgram/teamgram-server.git
cd teamgram-server
```

### 2. Start dependency stack

This starts MySQL, Redis, etcd, Kafka, MinIO (and optional monitoring). The database and MinIO buckets are initialized automatically.

```bash
docker compose -f docker-compose-env.yaml up -d
```

Wait until `docker compose -f docker-compose-env.yaml ps` shows mysql, redis, kafka, etcd and minio as healthy: the services connect to Kafka when they start.

### 3. Start application

```bash
docker compose up -d
```

---

## Logging, monitoring & tracing

| Area | Brief | Detailed docs |
|------|--------|----------------|
| **Log** | [Filebeat](https://www.elastic.co/beats/filebeat) → Kafka (`teamgram-log`) → [go-stash](https://github.com/kevwan/go-stash) → Elasticsearch → Kibana. Config: `teamgramd/deploy/filebeat/conf/filebeat.yml`, `teamgramd/deploy/go-stash/etc/`. | [Log collection](docs/log-collection.md) ([中文](docs/log-collection-zh.md)) |
| **Monitor** | [Prometheus](https://prometheus.io/) scrapes metrics; [Grafana](https://grafana.com/) for dashboards. Enable via `Prometheus` block in `teamgramd/etc2/*.yaml`. Config: `teamgramd/deploy/prometheus/server/prometheus.yml`. | [Service monitoring](docs/service-monitoring.md) ([中文](docs/service-monitoring-zh.md)) |
| **Tracing** | go-zero [Jaeger](https://www.jaegertracing.io/) / Zipkin support. Set `Telemetry` in `teamgramd/etc2/*.yaml`. Jaeger is included in `docker-compose-env.yaml`. | [Link tracking](docs/link-tracking.md) ([中文](docs/link-tracking-zh.md)) |

---

## Compatible clients

**Default sign-in verification code:** `12345` (change for production.)

| Platform | Repository | Patch Link |
|----------|------------|------------|
| Android | [https://github.com/teamgram/teamgram-android](https://github.com/teamgram/teamgram-android) | [teamgram-android](clients/teamgram-android.md) |
| iOS | [https://github.com/teamgram/teamgram-ios](https://github.com/teamgram/teamgram-ios) | [teamgram-ios](clients/teamgram-ios.md) |
| Desktop (TDesktop) | [https://github.com/teamgram/teamgram-tdesktop](https://github.com/teamgram/teamgram-tdesktop) | [teamgram-tdesktop](clients/teamgram-tdesktop.md) |

---

## Documentation

- [Project specs](specs/README.md) — Architecture, protocol, dependencies, contributing, security, roadmap
- [Service topology and configuration](docs/service-topology.md) — Ports, infrastructure, call graph ([中文](docs/service-topology-zh.md))
- [CONTRIBUTING](CONTRIBUTING.md) · [SECURITY](SECURITY.md) · [CHANGELOG](CHANGELOG.md)


---

## Community & feedback

- **Issues:** bugs and feature requests
- **Telegram:** [Teamgram group](https://t.me/+TjD5LZJ5XLRlCYLF)

---

## Enterprise edition

The following are available in the enterprise edition (contact the [author](https://t.me/benqi)):

- sticker/theme/chat_theme/wallpaper/reactions/secretchat/2fa/sms/push(apns/web/fcm)/web/scheduled/autodelete/... 
- channels/megagroups
- audio/video/group/conferenceCall
- bots
- miniapp

See [specs/roadmap.md](specs/roadmap.md) for community vs. enterprise scope.

---

## License

[Apache License 2.0](LICENSE).

---

## Give a Star! ⭐

If this project helps you, consider giving it a star.
