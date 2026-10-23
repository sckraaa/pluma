# Бэкапы и восстановление

Решение — ADR 0013. Сервис `backup` (`docker-compose-env.yaml`, `teamgramd/deploy/backup/backup.sh`) каждый день в `BACKUP_AT` (UTC) делает:
- согласованный дамп MySQL (`mysqldump --single-transaction`), снимок `mysql`;
- копию файлов объектного хранилища Garage, снимок `files`: блоки данных и почасовые снимки его метаданных (живая база метаданных не копируется, ADR 0014);
- приватный RSA-ключ сервера и весь `.env`, снимок `secrets` (зашифрован, как и всё остальное).

Всё шифруется restic паролем `RESTIC_PASSWORD` и уходит в S3-хранилище вне сервера. Раз в неделю (воскресенье) последний дамп восстанавливается во временную MySQL и проверяется, проверяется часть данных хранилища, старые снимки удаляются: 14 ежедневных, 8 еженедельных, 12 ежемесячных. Метрики — `backup_last_success_timestamp_seconds`, `backup_last_restore_check_timestamp_seconds`; алерты `BackupMissing` (нет успешного бэкапа больше 26 ч) и `RestoreCheckFailed` (проверка не проходила больше 8 дней).

Полный цикл «бэкап → потеря базы → восстановление по этой инструкции → вход и история на месте» проверяет сквозной тест `TestBackupAndRestore`.

## Первый запуск

1. **Хранилище.** Cloudflare R2 (10 ГБ бесплатно, без платы за скачивание) или Backblaze B2. Создать отдельный bucket, например `pluma-backups`, и API-ключ S3 с доступом **только к нему** (R2: R2 → Manage API tokens → Object Read & Write, один bucket).
2. **Пароль шифрования:** `openssl rand -base64 32`. **Сохранить его вне сервера** (менеджер паролей): без него ни один бэкап не восстановить, а сервер может погибнуть вместе с `.env`.
3. В `.env`:
   - `RESTIC_REPOSITORY` — R2: `s3:https://<account id>.r2.cloudflarestorage.com/pluma-backups`; B2: `s3:https://s3.<регион>.backblazeb2.com/pluma-backups`;
   - `RESTIC_PASSWORD`, `BACKUP_S3_ACCESS_KEY_ID`, `BACKUP_S3_SECRET_ACCESS_KEY`;
   - `BACKUP_S3_REGION` (R2 — `auto`), `BACKUP_AT`.
4. `docker compose -f docker-compose-env.yaml up -d backup` — при первом старте он создаёт репозиторий и сразу делает бэкап.
5. Проверить: `docker compose -f docker-compose-env.yaml run --rm backup snapshots` (снимки `mysql`, `files`, `secrets`) и `docker compose -f docker-compose-env.yaml run --rm backup check-restore`.

## Ручные команды

```bash
docker compose -f docker-compose-env.yaml run --rm backup now             # бэкап сейчас
docker compose -f docker-compose-env.yaml run --rm backup check-restore   # проверить восстановление
docker compose -f docker-compose-env.yaml run --rm backup snapshots       # список снимков
```

## Восстановление на чистой машине

Сервер потерян, есть `RESTIC_PASSWORD` и ключ S3.

1. Новый сервер: Docker, `git clone` репозитория той версии, что работала (или новее — миграции применятся сами).
2. Временный `.env`: скопировать `.env.example`, вписать **те же** `RESTIC_REPOSITORY`, `RESTIC_PASSWORD`, `BACKUP_S3_*`, а остальные обязательные переменные заполнить любыми значениями — compose без них не запускается.
3. Вернуть ключ сервера и настоящий `.env`:
   ```bash
   docker compose -f docker-compose-env.yaml run --rm -v "$PWD:/restore" backup restore-secrets /restore
   mkdir -p secrets && mv server_pkcs1.key secrets/ && chmod 600 secrets/server_pkcs1.key && mv env .env
   ```
   Теперь все секреты прежние: ключ S3 dfs совпадёт с восстановленными метаданными Garage, отпечаток RSA-ключа — с тем, что вшит в клиенты.
4. Вернуть файлы до запуска Garage:
   ```bash
   docker compose -f docker-compose-env.yaml run --rm -v "$PWD/data/garage:/restore" backup restore-files /restore
   ```
   Метаданные Garage — из последнего снимка: `ls data/garage/meta/snapshots/`, затем `cp data/garage/meta/snapshots/<последний>/db.sqlite data/garage/meta/db.sqlite`.
5. Поднять инфраструктуру: `docker compose -f docker-compose-env.yaml up -d mysql redis etcd kafka kafka-init garage`.
6. Загрузить дамп: `docker compose -f docker-compose-env.yaml run --rm backup restore-mysql` (последний снимок; конкретный — его ID из `snapshots`).
7. Поднять счётчики ящиков выше того, что видели клиенты, и сдвинуть группы Kafka на конец (ADR 0011, п. 11):
   ```bash
   docker compose run --rm --no-deps bff ./pluma-admin -f=../etc2/bff.yaml state-raise -by 1000000
   docker compose -f docker-compose-env.yaml exec kafka /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9094 --reset-offsets --to-latest --execute --group Inbox-MainCommunity-S --topic Inbox-T
   docker compose -f docker-compose-env.yaml exec kafka /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9094 --reset-offsets --to-latest --execute --group Sync-MainCommunity-S --topic Sync-T
   ```
8. Запустить всё: `docker compose -f docker-compose-env.yaml up -d` и `docker compose up -d`. Сервис `migrate` применит миграции новее бэкапа, `storage-init` проверит хранилище.

Сообщения, отправленные после последнего бэкапа, потеряны; клиенты с ними сделают `updates.getDifference` и увидят историю на момент бэкапа.
