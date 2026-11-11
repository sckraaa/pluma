# 084. Свои наборы стикеров и эмодзи, поиск стикеров

- Статус: сделано (ветка `task/084-own-stickers`, ждёт сборки партии; e2e написан и скомпилирован, на стенде не запускался).
- Решение: ADR 0053.

## Что нужно
Свои наборы стикеров и эмодзи пользователей и поиск: `stickers.createStickerSet`, `addStickerToSet`, `removeStickerFromSet`, `changeStickerPosition`, `changeSticker`, `replaceSticker`, `renameStickerSet`, `deleteStickerSet`, `setStickerSetThumb`, `checkShortName`, `suggestShortName`; `messages.getMyStickers`, `getAttachedStickers`, `searchStickers`, `searchStickerSets`, `searchEmojiStickerSets`, `searchCustomEmoji`, `readFeaturedStickers`. Наборы живут рядом с каталогом (задачи 024/058), ставятся и открываются по ссылке `pluma.chat/addstickers/<имя>`.

## Источники
Разобраны в ADR 0053 (раздел «Контекст»): core.telegram.org/stickers, страницы методов, код Android, iOS, tdesktop и Web из `reference-src-new` с номерами строк.

## Результат
Пользователь создаёт набор стикеров, масок или кастомных эмодзи из загруженных файлов (WEBP, TGS, WEBM) или из уже существующих стикеров; сервер проверяет каждый файл по байтам по правилам core.telegram.org/stickers и хранит стикер отдельным документом набора. Набор сразу установлен у автора, открывается и ставится другими по короткому имени, меняется только автором; все, у кого он установлен, получают `updateStickerSets` при каждом изменении и теряют набор при удалении. Работают поиск наборов и стикеров (по названию, эмодзи, ключевым словам и словам эмодзи), отметка прочитанных трендовых наборов, обложка набора и «стикеры на этом фото».

Изменения:
- Схема: `migrate-20261114-own-sticker-sets.sql` — `catalog_sticker_sets.owner_id`, `thumb_document_id`; таблицы `user_sticker_set_documents`, `user_featured_sticker_reads`, `media_attached_stickers`.
- `pkg/catalog`: `Set.OwnerID`, `Set.ThumbDocumentID`; `Replace` и `PutSet` не заменяют набор пользователя; `CatalogDocuments` отдаёт и стикеры наборов пользователей; `SetVersions`, `AnswerHash`.
- `pkg/catalog/stickerfile`: проверка WEBP, TGS, WEBM; разбор эмодзи и ключевых слов.
- `pkg/catalog/usersets`: сборка и изменение `messages.stickerSet`, хранилище (создание, изменение в транзакции, удаление), правила имён, плагин обложек для `upload.getFile`.
- `pkg/catalog/search`: индекс поиска в памяти.
- `pkg/catalog/installed`: `Holders`, `Changed`, `Drop`, прочитанные трендовые (`FeaturedRead`, `ReadFeatured`).
- `pkg/catalog/attached`: стикеры на фото и видео.
- `pkg/userbox`: `updateReadFeaturedStickers`, `updateReadFeaturedEmojiStickers` идут с seq.
- bff catalog: 18 обработчиков; `creator` и состояние установки в `decodeSet` / `viewOf`; непрочитанные в трендовых.
- bff messages, bff files: запись стикеров фото и метка `has_stickers` / `documentAttributeHasStickers`.
- bff: `files_helper` получает плагин обложек наборов.
- e2e: `TestOwnStickerSet`; `TestCatalog` берёт примером необслуживаемого метода `messages.getEmojiURL` вместо `stickers.checkShortName`.

Проверка (CachyOS): `go build ./...`; `go vet -tags integration,e2e -unreachable=false` по затронутым пакетам и `./test/e2e/`; `go test ./pkg/... ./app/bff/... ./app/service/media/... ./app/service/dfs/... ./tools/...`; интеграционные тесты `pkg/catalog`, `pkg/catalog/usersets`, `pkg/catalog/installed`, `pkg/catalog/attached` на настоящем MySQL. Проверка «тест падает без изменения»: без проверки владельца в `ownSet` падает `TestOwnStickerSet`; без `keepUserSet` падает интеграционный `usersets.TestStore`.

Решения, требующие подтверждения владельцем:
- Созданный набор сразу ставится автору (iOS сам его не ставит; как делает Telegram, по источникам не установлено).
- Новый стикер добавляется в конец набора.
- Набор без стикеров остаётся (пустой), пока автор его не удалит.
- У всех аккаунтов после обновления трендовые наборы станут непрочитанными (значок), пока их не откроют.
- `messages.readFeaturedStickers` без id отмечает прочитанными трендовые наборы и стикеров, и эмодзи (Android шлёт одинаковый пустой запрос для обоих видов).
- Статичные стикеры принимаются только в WEBP; PNG — `STICKER_FILE_INVALID`.
- Пользователь может занять имя набора, который есть в Telegram, но ещё не запрошен у нас.
- Ботам методы изменения наборов отвечают `BOT_METHOD_INVALID`.

Не сделано:
- e2e на стенде и `apicoverage` не запускались (общий прогон в конце конвейера).
- `docs/project/feature-registry.md` и `.json`, `STATUS.md`, `api-coverage.md` не правились (параллельные задачи); строки методов задачи в реестре нужно снять при сборке партии, включая `stickers.changeSticker` и `stickers.setStickerSetThumb` из «Решает владелец» / «Не нужно».
- Создание наборов ботами (имя `…_by_<бот>`).
- Превью (`thumbs`) у стикеров пользователей: нет кодировщика WEBP без cgo.
- `messages.getStickers` (стикеры аккаунта по эмодзи) по-прежнему отдаёт пусто — вне задачи.
