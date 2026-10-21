# Первичная настройка: ключ сервера и секреты

Что сделать один раз при установке сервера, до выдачи клиентов. Основание — задача `docs/tasks/001-security-p0.md`, ADR 0003.

## 1. RSA-ключ сервера

Клиент шифрует этим ключом начало handshake. Публичная часть вшивается в каждый клиент, приватная есть только у сервера. Кто владеет приватной частью, может выдать себя за сервер (MITM при создании auth_key).

Ключ из upstream teamgram (`teamgramd/bin/server_pkcs1.key`, fingerprint `12240908862933197005` = `0xa9e071c1771060cd`) лежал в git, поэтому **считается скомпрометированным навсегда**. Из репозитория он удалён, историю не переписывали. Клиенты с этим ключом не использовать.

Сгенерировать свой ключ (из корня репозитория):

```bash
go run ./tools/genserverkey -out secrets/server_pkcs1.key
```

- Файл создаётся с правами `0600`. Существующий файл утилита не перезаписывает: новый ключ ломает все клиенты со старым.
- `secrets/` в `.gitignore` и `.dockerignore`: ключ не попадёт ни в git, ни в образ. В контейнер он монтируется как compose secret (`/run/secrets/server_pkcs1.key`).
- Сделайте офлайн-копию ключа (например, в менеджере паролей). Потеря ключа = пересборка всех клиентов.

Утилита печатает:

| Что | Куда |
|---|---|
| `KeyFingerprint` (беззнаковое десятичное) | `.env` → `SERVER_RSA_KEY_FINGERPRINT` |
| hex fingerprint (`0x…`) | Android: `serverPublicKeysFingerprints` в `TMessagesProj/jni/tgnet/Handshake.cpp` |
| PEM `BEGIN RSA PUBLIC KEY` | iOS (MtProtoKit), Android (tgnet), Desktop (`kPublicRSAKeys`) — см. `docs/audit/04-clients-and-builds.md` |
| `modulus` / `exponent` в hex | Web K: `publisKeysHex` в `src/lib/mtproto/rsaKeysManager.ts` |

Повторно вывести эти данные (или проверить ключ, вшитый в патч клиента): `go run ./tools/genserverkey -show <файл>`. Принимается приватный ключ или публичный (PKCS#1 или PKIX).

### Проверки при старте gnetway (и httpserver)

Сервис не запустится и напишет причину, если:
- файл ключа отсутствует, не читается или это не обычный файл;
- файл доступен «прочим» пользователям (любые биты `o`; лечится `chmod 600`). Чтение группой (`0640`) допустимо;
- ключ не PKCS#1 PEM (`BEGIN RSA PRIVATE KEY`), зашифрован или не 2048 бит (другие размеры клиенты Telegram не поддерживают);
- fingerprint ключа не совпадает с `KeyFingerprint` в конфиге (например, оставили старое значение после генерации);
- список `RSAKey` пуст или ключ указан дважды.

## 2. Пароли инфраструктуры (`.env`)

```bash
cp .env.example .env && chmod 600 .env
openssl rand -hex 24   # по одному значению на каждый пароль
```

Заполните все пустые значения. Пока хоть одно пустое, `docker compose` откажется запускаться с ошибкой `required variable … is missing a value`. Используйте только hex: значения подставляются в YAML и DSN как есть.

| Переменная | Назначение |
|---|---|
| `MYSQL_ROOT_PASSWORD` | root MySQL, только для администрирования |
| `MYSQL_PASSWORD` | пользователь `teamgram`, под которым ходят сервисы |
| `REDIS_PASSWORD` | `requirepass` Redis |
| `MINIO_ROOT_USER`, `MINIO_ROOT_PASSWORD` | MinIO (им же пользуется dfs); пароль не короче 8 символов |
| `GRAFANA_ADMIN_PASSWORD` | вход в Grafana |
| `SERVER_RSA_KEY_FILE` | путь к ключу на хосте, по умолчанию `./secrets/server_pkcs1.key` |
| `SERVER_RSA_KEY_FINGERPRINT` | fingerprint из `genserverkey` |

Конфиги сервисов (`teamgramd/etc2`, `teamgramd/etc`) берут секреты из окружения через `${ИМЯ}`. Если переменная не задана, сервис не стартует и называет её. В ошибках фигурируют только имена переменных, без значений. Конфиг целиком в лог при старте больше не пишется.

## 3. Запуск в Docker

```bash
docker compose -f docker-compose-env.yaml up -d   # инфраструктура
docker compose up -d --build                      # сервисы teamgram
```

Сеть:
- Наружу опубликованы только порты клиентов: `10443` и `5222` (MTProto TCP), `11443` (WebSocket для веб-клиентов).
- Внутренние gRPC-порты (`20xxx`), метрики и dfs HTTP доступны только внутри сети `teamgram_net`.
- Порты для администрирования привязаны к `127.0.0.1`: MySQL 3306, Redis 6379, etcd 2379, MinIO 9000/9001, Kafka 9092/9094, Grafana 3001, Prometheus 9090, Jaeger 16686/14268, Kibana 5601, Elasticsearch 9200. С другой машины к ним ходят через SSH-туннель: `ssh -L 3001:127.0.0.1:3001 user@server`.
- Публикация портов в Docker обходит ufw/iptables INPUT. Поэтому любой новый порт добавляйте с `127.0.0.1:`, если он не должен быть открыт в интернет.

Проверить итоговую публикацию портов: `docker compose config | grep -A3 ports` (или `docker ps`).

## 4. Обновление существующей установки

- **MySQL:** переменные `MYSQL_*` применяются только при первой инициализации (пустой `./data/mysql`). Для уже созданной базы смените пароли вручную:
  ```sql
  ALTER USER 'teamgram'@'%' IDENTIFIED BY '<MYSQL_PASSWORD>';
  ALTER USER 'root'@'%' IDENTIFIED BY '<MYSQL_ROOT_PASSWORD>';
  ALTER USER 'root'@'localhost' IDENTIFIED BY '<MYSQL_ROOT_PASSWORD>';
  ```
- **Grafana:** пароль из переменной действует только для нового тома: `docker exec grafana grafana-cli admin reset-admin-password <пароль>`.
- **Redis, MinIO:** новые учётные данные применяются после перезапуска контейнеров.
- **Ключ:** клиенты со старым (скомпрометированным) ключом перестанут подключаться. Их нужно пересобрать с новым публичным ключом.

## 5. Запуск без Docker (`teamgramd/etc`)

`teamgramd/bin/runall2.sh` подхватывает переменные из `.env` в корне репозитория. Ключ берётся из `secrets/server_pkcs1.key` (в конфиге путь `../../secrets/server_pkcs1.key` относительно `teamgramd/bin`). Сервисы подключаются к MySQL пользователем `teamgram`, а не `root`.

## 6. Ротация ключа

1. Сгенерируйте новый ключ в отдельный файл: `go run ./tools/genserverkey -out secrets/server_pkcs1.next.key`.
2. Добавьте его в `RSAKey` второй записью (`teamgramd/etc2/gnetway.yaml`) со своей переменной fingerprint. Пропишите в `docker-compose.yaml` второй secret и переменную, а в `.env.example` — переменную. Gateway объявляет в `resPQ` оба fingerprint, и клиенты с любым из ключей работают.
3. Выпустите клиенты с новым публичным ключом.
4. Когда старых клиентов не останется, уберите старый ключ из конфига и удалите файл.

## Что ещё не закрыто

etcd, Kafka и Elasticsearch/Kibana работают без аутентификации. Они не опубликованы наружу, но доступны любому контейнеру в `teamgram_net`. Между сервисами нет TLS. Это задачи следующих этапов (см. `docs/audit/02-transport-and-security.md`, #6).
