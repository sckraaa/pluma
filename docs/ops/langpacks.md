# Языковые пакеты

Сервер отдаёт клиентам интерфейс на всех языках Telegram, с «Pluma» вместо «Telegram» (задача 021, ADR 0025). Пакеты — по платформам: `android`, `ios`, `tdesktop`, `macos`, `webk`. Их берут у самого Telegram методами `langpack.*` без входа и кладут в MySQL с версиями.

## Как это устроено
1. **`pluma-admin langpack fetch`** подключается к Telegram (DC 2, `149.154.167.51:443`) и пишет файлы-снимки `<каталог>/<пакет>/<язык>.json`. Нужен только `api_id` приложения владельца (my.telegram.org): флаг `-api-id` или переменная `TELEGRAM_API_ID`. `api_hash`, телефон и вход не нужны.
2. **`pluma-admin langpack import <каталог>`** кладёт снимки в базу (таблицы `langpack_languages`, `langpack_strings`; миграция `migrate-20261101-langpacks.sql` применяется сервисом `migrate`). На каждый пакет и язык — одна транзакция. Изменённые строки получают новую версию языка, исчезнувшие помечаются удалёнными. Без изменений ничего не пишется и версия не растёт, поэтому повторный импорт безопасен.
3. **Клиенты** узнают новую версию из `help.getConfig` (`lang_pack_version`) и сами запрашивают разницу. Перезапуск сервисов не нужен.

## Команды
Запускать там, где виден MySQL стека, — в контейнере bff. На сервере со своим адресом (`PLUMA_PUBLIC_IPV4`) compose всегда вызывается с обоими файлами, как ниже. Без `docker-compose.public-ip.yaml` compose считает сеть `teamgram_net` изменённой и пересоздаёт её. Тогда все контейнеры на ходу получают новые адреса, а исходящий трафик уходит с основного адреса сервера. Так 2026-11-02 в 04:42 UTC таймер обновления положил доставку сообщений до перезапуска Kafka (`docs/ops/deployment.md`, «Схема»). На машине разработки этого файла нет: там второй `-f` не нужен. Снимки удобно держать на хосте, например в `/opt/pluma/langpacks/<дата>`.

```bash
cd /opt/pluma
set -a; . ./.env; set +a      # TELEGRAM_API_ID и пароли; значения не выводить
D=$(date +%F)
docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml run --rm --no-deps -e TELEGRAM_API_ID \
  -v /opt/pluma/langpacks:/langpacks bff ./pluma-admin langpack fetch -out /langpacks/$D
docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml run --rm --no-deps \
  -v /opt/pluma/langpacks:/langpacks bff ./pluma-admin -f ../etc2/bff.yaml langpack import /langpacks/$D
docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml exec bff ./pluma-admin -f ../etc2/bff.yaml langpack list
```

- **`fetch`**
  - `-pack android,ios` — только эти пакеты (по умолчанию все пять); `-lang ru,uk` — только эти языки (по умолчанию все языки пакета).
  - `-delay 1s` — пауза между запросами. Если Telegram ответит `FLOOD_WAIT_X`, команда ждёт до 5 минут и повторяет запрос; дольше — останавливается с ошибкой. Уже записанные снимки остаются.
  - `-addr`, `-dc` — другой датацентр Telegram, если DC 2 недоступен.
  - Печатает по строке на язык: версию Telegram и число строк.
- **`import`**
  - `-pack`, `-lang` — только часть снимков каталога.
  - Печатает по строке на язык: новый, без изменений или «версия A → B» с числом добавленных, изменённых и удалённых строк.
  - Отдельной строкой — сколько строк ещё называют Telegram и какими словами (например, «diTelegram ×2»). Падежные формы заменяются по таблицам языков (ADR 0025); в отчёт попадают окончания, которых в таблицах нет, опечатки и строчное «telegram». Новые формы после обновления Telegram — повод дополнить таблицу в `pkg/langpack/inflect.go`. Строки, оставленные как есть намеренно (`lng_about_text1_api`), в отчёт не входят. `-keys` перечисляет ключи.
- **`list`** — языки в базе: версия Pluma, версия Telegram при последнем импорте, переведено строк из скольких, время последнего изменения.

## По расписанию
Telegram меняет строки часто, раз в сутки достаточно. Таймер systemd на хосте:

`/etc/systemd/system/pluma-langpacks.service`:
```ini
[Unit]
Description=Pluma: language packs from Telegram
After=docker.service

[Service]
Type=oneshot
WorkingDirectory=/opt/pluma
EnvironmentFile=/opt/pluma/.env
ExecStart=/bin/sh -c 'D=$(date +%%F) && \
  docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml run --rm --no-deps -e TELEGRAM_API_ID -v /opt/pluma/langpacks:/langpacks bff ./pluma-admin langpack fetch -out /langpacks/$D && \
  docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml run --rm --no-deps -v /opt/pluma/langpacks:/langpacks bff ./pluma-admin -f ../etc2/bff.yaml langpack import /langpacks/$D && \
  find /opt/pluma/langpacks -mindepth 1 -maxdepth 1 -type d -mtime +14 -exec rm -rf {} +'
```

`/etc/systemd/system/pluma-langpacks.timer`:
```ini
[Unit]
Description=Pluma: language packs daily

[Timer]
OnCalendar=*-*-* 04:30:00
RandomizedDelaySec=30m
Persistent=true

[Install]
WantedBy=timers.target
```

```bash
systemctl daemon-reload && systemctl enable --now pluma-langpacks.timer
systemctl start pluma-langpacks.service && journalctl -u pluma-langpacks.service -n 50   # первый прогон
```

- Снимки за 14 дней остаются на диске: можно повторить импорт старого снимка.
- Ошибка `fetch` (нет связи с Telegram, долгий `FLOOD_WAIT`) не трогает базу: клиенты остаются на прежней версии.
- Снимок весит 0,2–1,5 МБ, все пакеты и языки — около 150 МБ на прогон.

## Первое включение
1. Владелец заполняет в `/opt/pluma/.env` переменную `TELEGRAM_API_ID` (число из my.telegram.org; строка есть в `.env.example`, раздел «Language packs»). В git и в логи значение не попадает. Пустое значение: `fetch` останавливается с ошибкой, база не меняется.
2. Обновить сервисы как обычно (`deployment.md`, «Обновление»); миграцию применит `migrate`.
3. Выполнить команды из раздела «Команды» и проверить `langpack list`: у каждого пакета 19–29 языков.
4. Включить таймер.

Пока пакетов в базе нет, `help.getConfig` не сообщает версию пакета, и клиенты остаются на встроенных строках: Android и Desktop — на своих, включая русский у Android; Web K и iOS — на английском.

## Что важно знать
- **Замена «Telegram» → «Pluma»** идёт при импорте, только в текстах, по правилам патчей клиентов: слово целиком, адреса и `@Telegram` остаются; «Telegram Desktop» → «Pluma»; написания на арабском, персидском, корейском, нидерландском; ссылки входа по QR ведут на pluma.chat и web.pluma.chat; «Telegram API» в `lng_about_text1_api` остаётся. Исходный текст Telegram тоже хранится: после смены правил достаточно повторить `import` того же снимка.
- **Web K** просит пакет `web` и `webk` — сервер отдаёт оба из `webk`.
- **Изменения видны клиенту** после его следующего `help.getConfig`; рассылки `updateLangPack` онлайн-клиентам пока нет.

## Свои строки Pluma (ADR 0046)
- Тексты патчей клиентов, которых нет у Telegram (например, экраны кода входа, задача 076), лежат в `pkg/langpack/own.json` и добавляются при каждом `import` к пакетам `ios`, `tdesktop`, `webk` без замены «Telegram».
- **После выката версии, где `own.json` изменился, повторить `import` последнего снимка** (команда выше, без `fetch`): иначе клиенты получат новые строки только со следующим плановым обновлением.
- Строки Telegram, которым Pluma даёт другой текст (статус аккаунта поддержки «сервисный аккаунт», задача 080), лежат в `pkg/langpack/override.json`: при `import` их текст заменяет текст Telegram в пакетах `android`, `ios`, `tdesktop`, `webk`. Правило то же: после выката версии, где этот файл изменился, повторить `import` последнего снимка:
  ```bash
  cd /opt/pluma
  D=$(ls -1 /opt/pluma/langpacks | tail -1)      # последний снимок
  docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml run --rm --no-deps \
    -v /opt/pluma/langpacks:/langpacks bff ./pluma-admin -f ../etc2/bff.yaml langpack import /langpacks/$D
  ```
  В выводе у каждого языка четырёх пакетов — «1 changed» (или «1 added», если ключа у Telegram на этом языке не было); повторный запуск — «no changes».
- Импорт печатает ключи без перевода на язык («in English, no translation»): такой язык показывает английский текст.

