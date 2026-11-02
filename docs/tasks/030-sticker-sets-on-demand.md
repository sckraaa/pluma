# 030. Наборы стикеров и эмодзи из Telegram по запросу

- Статус: in-progress (2026-11-02)
- Зависит от: 024 (каталог, импорт документов)
- ADR: новый (бот Telegram на сервере, хранение наборов по запросу)

## Цель
Любой набор стикеров или эмодзи из Telegram доступен в Pluma. Пользователь открывает `t.me/addstickers/<имя>` или `t.me/addemoji/<имя>`, и набор появляется сразу. При первом запросе сервер берёт его у Telegram и пополняет свою базу (запрос владельца, 2026-11-02).

## Что известно
- **Клиенты** сами разбирают такие ссылки и спрашивают сервер `messages.getStickerSet(inputStickerSetShortName)`. Сейчас набора, которого нет в каталоге, сервер не знает и отвечает `STICKERSET_INVALID`.
- **Боты** могут вызывать `messages.getStickerSet` и `upload.getFile`. На страницах core.telegram.org/method/messages.getStickerSet и /upload.getFile: «Both users and bots can use this method», проверено 2026-11-02.
  - Бот входит через `auth.importBotAuthorization(api_id, api_hash, bot_auth_token)`, без телефона и кода.
  - Аккаунты владельца не участвуют.
- **На сервере** (2026-11-02) лежат `TELEGRAM_BOT_TOKEN` и `TELEGRAM_API_HASH` в `/opt/pluma/.env`. Их положил владелец; значения не читались. `TELEGRAM_API_ID` был там раньше (задача 021).
- **Подключение к Telegram** уже есть в `pkg/langpack/telegram.go`: `mtclient` с RSA-ключом Telegram, DC 2. Ответы приходят в слое 228, их разбирает наш кодек, перевод из tweb не нужен.
- **Импорт** — путь задачи 024: `dfs/media.importDocument`, перенумерация и `catalog_ids`.
- **Разница с Telegram:** по документации `STICKERSET_INVALID` у Telegram — ошибка 406, у нас — 400 (`mtproto.ErrStickersetInvalid`). Проверить, что ждут клиенты.

## Область
- **Клиент бота на сервере.**
  - Вход ботом и переподключение.
  - Подключения к другим DC для файлов: `auth.exportAuthorization` / `importAuthorization`, адреса из `help.getConfig`.
  - Обработка `FILE_MIGRATE`, `FLOOD_WAIT`, ограничение частоты.
- **Получение по запросу.**
  - В `messages.getStickerSet` (по имени и по id набора, пришедшего из Telegram) набор, которого нет, берётся у Telegram с ожиданием в несколько секунд.
  - Одна загрузка на набор, сколько бы пользователей ни открыли ссылку одновременно.
  - Ограничения на размер набора и общий объём.
- **Хранение.** Наборы по запросу хранятся отдельно от каталога: `Store.Replace` при новом импорте каталога не должен их удалять. Добавить у набора источник (`catalog` или `demand`), id — через `catalog_ids`.
- **Установка наборов пользователями** (иначе «Добавить» ничего не сохраняет): `installStickerSet`, `uninstallStickerSet`, `getAllStickers`, `getEmojiStickers`, `reorderStickerSets` с хэшем по формуле клиентов и `updateNewStickerSet` / `updateStickerSets` на другие устройства. Это часть этапа 2, без неё задача неполна.
- **Свои эмодзи в сообщениях** из наборов, полученных по запросу, — через `getCustomEmojiDocuments` (документы каталога).

## Критерии приёмки
- Ссылки `t.me/addstickers/…` и `t.me/addemoji/…` открывают набор в Android, iOS, Desktop и Web K. После «Добавить» набор остаётся в панели на всех устройствах аккаунта.
- Повторный запрос того же набора не обращается к Telegram.
- Новый импорт каталога не удаляет наборы, полученные по запросу.
- e2e: набор по имени через поддельный Telegram в стенде. Настоящий Telegram в CI не используется.

## Ход работы
- 2026-11-02: владелец создал бота и положил `TELEGRAM_BOT_TOKEN` и `TELEGRAM_API_HASH` в `/opt/pluma/.env` (значения не читались).
- **Готово в ветке `task/030-sticker-sets-on-demand`:**
  - `pkg/tgbot` (`cb08c9af`) — клиент бота. Вход ботом, переход на его DC, адреса DC из `help.getConfig`, перенос авторизации на DC файла, `FILE_MIGRATE`, `FLOOD_WAIT`, параллельное скачивание. Тесты на поддельном Telegram.
  - `pkg/catalog/demand` и `importer.ImportSet` (`1cfc2bad`). Одна загрузка на имя, «нет такого» запоминается на час, лимиты. Хранение рядом с каталогом: `source = 'demand'`, `migrate-20261103-catalog-demand.sql`, импорт каталога такие наборы не удаляет.
  - `getStickerSet` по имени берёт недостающий набор у Telegram (`5983276d`). Переменные бота передаются в `bff` и `migrate`, пустые — бот выключен. Описание — `docs/ops/catalog.md`.
- **Не проверено на настоящем Telegram:** нужна выкатка.
- **Дальше:** установленные наборы пользователя: `installStickerSet`, `uninstallStickerSet`, `toggleStickerSets`, `reorderStickerSets`, `getAllStickers`, `getEmojiStickers`, `getMaskStickers`, `getArchivedStickers`. По образцу сохранённых GIF (`f5293da1`): сервис user, таблица, обновления с `seq` на другие устройства (`updateNewStickerSet`, `updateStickerSets`, `updateStickerSetsOrder`), хэш `getAllStickers` по формуле клиентов (`CountStickersOrderHash`, `docs/tasks/024-research/semantics.md`).

## Результат
