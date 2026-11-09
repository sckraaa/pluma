# Панель управления

Внутренняя веб-панель владельца: обзор, пользователи, каналы и группы, жалобы, регистрация, рассылка, выпуски, система, журнал, доступ (задачи 025, 072; ADR 0040, 0042). Открыта только из tailnet, в интернет не выставлена.

## Что нужно от владельца
- Аккаунт Tailscale и Tailscale на Маке и телефоне. Вход в tailnet — через аккаунт с двухфакторкой.
- Tailscale на сервере Pluma: сервис из этого репозитория за него это не сделает.

## Развёртывание
1. Применить миграции: `migrate-20261109-moderation.sql`, `migrate-20261109-panel-console.sql`, `migrate-20261109-panel-roles.sql` (сервис migrate применяет сам). Образ панели выкатывается вместе с миграцией `panel-roles`: столбец `role` исчезает, старый образ панели его читает.
2. Пользователь БД панели. Пароль — в `.env` как `ADMIN_MYSQL_PASSWORD`; в репозиторий он не попадает.

   ```sql
   CREATE USER IF NOT EXISTS 'pluma_admin'@'172.20.%' IDENTIFIED BY '<ADMIN_MYSQL_PASSWORD>';
   GRANT SELECT, INSERT, UPDATE ON teamgram.moderation_reports TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE, DELETE ON teamgram.moderation_restrictions TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE, DELETE ON teamgram.admin_users TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE, DELETE ON teamgram.admin_passkeys TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE, DELETE ON teamgram.admin_sessions TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE, DELETE ON teamgram.admin_challenges TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT ON teamgram.admin_audit TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE ON teamgram.admin_broadcasts TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE ON teamgram.registration_settings TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, DELETE ON teamgram.phone_allowlist TO 'pluma_admin'@'172.20.%';
   GRANT SELECT (id, phone, first_name, last_name, username, about, photo_id, is_bot, deleted, created_at) ON teamgram.users TO 'pluma_admin'@'172.20.%';
   GRANT SELECT (id, creator_user_id, title, broadcast, megagroup, participants_count, blocked, deleted, `date`) ON teamgram.channels TO 'pluma_admin'@'172.20.%';
   GRANT SELECT (id, creator_user_id, title, participant_count, blocked, deactivated, `date`) ON teamgram.chats TO 'pluma_admin'@'172.20.%';
   GRANT SELECT (peer_type, peer_id, username, deleted) ON teamgram.username TO 'pluma_admin'@'172.20.%';
   GRANT SELECT (user_id, last_seen_at) ON teamgram.user_presences TO 'pluma_admin'@'172.20.%';
   GRANT SELECT (date2, sender_user_id, user_id) ON teamgram.messages TO 'pluma_admin'@'172.20.%';
   GRANT SELECT (date2) ON teamgram.channel_messages TO 'pluma_admin'@'172.20.%';
   GRANT SELECT (channel_id, user_id, state) ON teamgram.channel_participants TO 'pluma_admin'@'172.20.%';
   GRANT SELECT (user_id), DELETE ON teamgram.user_passwords TO 'pluma_admin'@'172.20.%';
   -- Повтор и отброс зависших событий (pkg/outbox, в транзакциях ящика).
   GRANT SELECT, INSERT, UPDATE, DELETE ON teamgram.mq_outbox TO 'pluma_admin'@'172.20.%';
   GRANT SELECT, INSERT, UPDATE ON teamgram.user_state TO 'pluma_admin'@'172.20.%';
   FLUSH PRIVILEGES;
   ```

   Адрес `172.20.%` — сеть `teamgram_net`: порт MySQL опубликован Docker, и для сервера подключение панели приходит со шлюза этой сети, а не с `127.0.0.1`. Снаружи MySQL закрыт: порт привязан к `127.0.0.1` хоста.

   Таблица `admin_audit` только на добавление: ни обновить, ни удалить запись пользователь панели не может. В таблицы других сервисов панель пишет только через их RPC; исключения записаны в ADR 0042.
3. В `.env`: `PANEL_ORIGIN=https://<имя>.<tailnet>.ts.net` и `ADMIN_MYSQL_PASSWORD`. Панель также читает `REDIS_PASSWORD` и `LOGIN_CODE_HMAC_KEY` (те же, что у `bff`), необязательные `PANEL_GRAFANA_URL` (ссылка на Grafana в разделе «Система») и `RELEASES_ADMIN_TOKEN` (раздел «Выпуски», см. `docs/ops/releases.md`).
4. Запустить сервис: `docker compose -f docker-compose.yaml ... -f docker-compose.admin.yaml up -d admin`. Он слушает `127.0.0.1:9100`, порт наружу не пробрасывается: контейнер работает с сетью хоста.
5. Опубликовать в tailnet: `tailscale serve --bg --https=443 http://127.0.0.1:9100`. Пускать только с ваших устройств: правила (ACL) tailnet.
6. Первый вход: `docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml panel owner <login@tailnet>`.
7. Открыть `PANEL_ORIGIN`, зарегистрировать ключ доступа (Touch ID или Face ID).

## Разделы
- **Обзор** — числа из БД, диск и проблемы из Prometheus. Что не прочиталось, показывается как «нет данных» и не мешает остальному.
- **Пользователи** — поиск по номеру, имени, `@username`, id. Карточка: профиль, устройства, ограничения, жалобы. Действия: завершить сеансы, ограничить, заблокировать, снять, показать код входа, написать от Pluma, удалить аккаунт.
- **Каналы и группы** — поиск, карточка, заблокировать, удалить.
- **Жалобы** — очередь, снимок, решение. Сообщения никогда не показываются, кроме снимка жалобы.
- **Регистрация** — переключатель «открыта для всех / по списку» и список номеров (`phone_allowlist`). Срабатывает на следующем `auth.sendCode` и `auth.signUp`: логин читает настройку из БД на каждый запрос.
- **Рассылка** — от служебного аккаунта 777000 всем, по списку id или участникам канала. Идёт по одному, после перезапуска продолжает с места остановки.
- **Выпуски** — версии клиентов на сайте, откат и возврат через сервис `releases` (`docs/ops/releases.md`). Новые сборки приходят из CI.
- **Система** — состояние сервисов и алерты из Prometheus, зависшие события с «Повторить» и «Отбросить» (`pkg/outbox`), ссылка на Grafana.
- **Журнал** и **Доступ** — все действия с фильтром, вход в панель, ключи доступа.

## Запасной доступ
Панель не открывается — работает SSH:

```bash
docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml moderation reports [-all]
docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml moderation restrict +79991234567 [-days 7]
docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml moderation ban +79991234567 [-days 7]
docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml moderation lift +79991234567
docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml moderation block-channel|unblock-channel|block-chat|unblock-chat <id>
docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml registration open|list
docker compose exec bff ./pluma-admin -f ../etc2/bff.yaml panel reset-passkey <login>
```

Потерян ключ доступа — `panel reset-passkey`, при следующем открытии панель попросит зарегистрировать новый. Заблокированный пользователь входит заново после `moderation lift`.

## Что делает решение
- **Отклонить** — только закрывает жалобу.
- **Удалить сообщение** — удаляет у всех участников чата.
- **Ограничить** — в личке только тем, у кого он в контактах (`PEER_FLOOD`), в группах и каналах нельзя (`USER_BANNED_IN_CHANNEL`); уведомление от 777000.
- **Заблокировать** — уведомление от 777000, завершение всех сессий, вход отвечает `PHONE_NUMBER_BANNED`.

Срок необязателен; если не указан, решение бессрочно. Закрыть жалобу и поставить ограничение можно только один раз: при повторе ответ 409. Если шаг решения не выполнился, панель показывает ошибку, сервис повторяет его сам. После 10 неудач есть кнопка «Повторить».

## Блокировка и удаление
Удалить аккаунт или канал можно только после блокировки и отдельным подтверждением: id вводится заново, плюс ключ доступа. Блокировка канала обратима: участники остаются, канал виден как недоступный (`restricted`, клиенты не открывают его и показывают текст), писать и читать нельзя. Обычная группа при блокировке становится `deactivated`, участники тоже остаются.

Опасные действия (блокировка, удаление, ограничение, снятие ограничения, код входа, рассылка, откат, повтор и отброс событий, правка доступа) просят ключ доступа ещё раз и пишутся в журнал.

## Журнал
Страница «Журнал» — все действия и просмотры карточек и жалоб. Копия без подробностей в Grafana: `{service="admin"} |= "moderation audit"`. В журнале нет текстов сообщений и нет кодов входа; показ кода — запись «показан код входа» без самого кода. Номера телефонов видны только на страницах панели, в журнал и логи попадают только последние цифры.
