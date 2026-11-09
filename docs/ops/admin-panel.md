# Панель модерации

Внутренняя веб-панель: очередь жалоб, решения, журнал, люди панели (задача 025, ADR 0040). Открыта только из tailnet, в интернет не выставлена.

## Что нужно от владельца
- Аккаунт Tailscale и Tailscale на Маке и телефоне (задача брифа). Вход в tailnet — через аккаунт с двухфакторкой.
- Tailscale на сервере Pluma: сервис из этого репозитория за него это не сделает.

## Развёртывание
1. Применить миграцию: `teamgramd/deploy/sql/migrate-20261109-moderation.sql` (сервис migrate применяет сам).
2. Создать пользователя БД панели. Пароль — в `.env` как `ADMIN_MYSQL_PASSWORD`; в репозиторий он не попадает.

   ```sql
   CREATE USER IF NOT EXISTS 'pluma_admin'@'172.20.%' IDENTIFIED BY '<ADMIN_MYSQL_PASSWORD>';
   GRANT SELECT, INSERT, UPDATE ON teamgram.moderation_reports TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE, DELETE ON teamgram.moderation_restrictions TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE, DELETE ON teamgram.admin_users TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE, DELETE ON teamgram.admin_passkeys TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE, DELETE ON teamgram.admin_sessions TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE, DELETE ON teamgram.admin_challenges TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT ON teamgram.admin_audit TO 'pluma_admin'@'172.20.%';
   GRANT SELECT (id, phone, first_name, deleted) ON teamgram.users TO 'pluma_admin'@'172.20.%';
   GRANT SELECT (id, creator_user_id, title) ON teamgram.channels TO 'pluma_admin'@'172.20.%';
   GRANT INSERT ON teamgram.mq_outbox TO 'pluma_admin'@'172.20.%';
   FLUSH PRIVILEGES;
   ```

   Адрес `172.20.%` — сеть `teamgram_net`: порт MySQL опубликован Docker, и для сервера подключение панели приходит со шлюза этой сети, а не с `127.0.0.1`. Снаружи MySQL закрыт: порт привязан к `127.0.0.1` хоста.

   Таблица `admin_audit` только на добавление: ни обновить, ни удалить запись пользователь панели не может.
3. В `.env`: `PANEL_ORIGIN=https://<имя>.<tailnet>.ts.net` и `ADMIN_MYSQL_PASSWORD`.
4. Запустить сервис: `docker compose -f docker-compose.yaml ... -f docker-compose.admin.yaml up -d admin`. Он слушает `127.0.0.1:9100`, порт наружу не пробрасывается: контейнер работает с сетью хоста.
5. Опубликовать в tailnet: `tailscale serve --bg --https=443 http://127.0.0.1:9100`. Пускать только с ваших устройств: правила (ACL) tailnet.
6. Первый владелец: `docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml panel owner <login@tailnet>`.
7. Открыть `PANEL_ORIGIN`, зарегистрировать ключ доступа (Touch ID или Face ID). Панель сразу доступна владельцу.

## Запасной доступ
Панель не открывается — работает SSH:

```bash
docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml moderation reports [-all]
docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml moderation restrict +79991234567 [-days 7]
docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml moderation ban +79991234567 [-days 7]
docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml moderation lift +79991234567
docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml panel reset-passkey <login>
```

Потерян ключ доступа — `panel reset-passkey`, при следующем открытии панель попросит зарегистрировать новый. Заблокированный пользователь входит заново после `moderation lift`.

## Что делает решение
- **Отклонить** — только закрывает жалобу.
- **Удалить сообщение** — удаляет у всех участников чата.
- **Ограничить** — в личке только тем, у кого он в контактах (`PEER_FLOOD`), в группах и каналах нельзя (`USER_BANNED_IN_CHANNEL`); уведомление от 777000.
- **Заблокировать** — уведомление от 777000, завершение всех сессий, вход отвечает `PHONE_NUMBER_BANNED`.

Срок необязателен; если не указан, решение бессрочно. Закрыть жалобу и поставить ограничение можно только один раз: при повторе ответ 409. Если шаг решения не выполнился, панель показывает ошибку, сервис повторяет его сам. После 10 неудач есть кнопка «Повторить».

Опасные действия (всё, кроме «отклонить»: блокировка, удаление, ограничение, снятие ограничения, правка людей) просят ключ доступа ещё раз.

## Журнал
Страница «Журнал» — все действия и просмотры. Копия без подробностей в Grafana: `{service="admin"} |= "moderation audit"`.
