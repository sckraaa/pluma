# Каталог из Telegram

Реакции, эффекты, стикеры и наборы эмодзи, подсказки эмодзи, цвета, темы, обои, списки эмодзи по умолчанию и часовые пояса — общие для всех аккаунтов данные. Сервер отдаёт их теми же методами, что Telegram (задача 024, ADR 0028). Их выгружают из Telegram и импортируют с id Pluma.

Пока каталог не импортирован, методы отвечают пустыми списками: реакций и стикеров в клиентах нет, но и ошибок нет.

## Как это устроено
1. **Выгрузка.** Скрипт `tools/catalog-collector/collector.js` работает во вкладке Telegram Web K (web.telegram.org/k), где уже вошёл запасной аккаунт Telegram владельца. Нового входа нет.
   - Он только читает: ответы методов, все наборы, на которые они ссылаются, и файлы всех документов.
   - Останавливается при любом признаке риска для сессии: 401/406, ошибки ключа или сессии, `FLOOD_WAIT` дольше 2 минут.
   - Метаданные запрашивает по одному вызову с паузой, файлы — не больше 4 сразу.
2. **Приёмник** (`docker-compose.catalog.yaml`, `pluma-admin catalog receive`).
   - Принимает файлы выгрузки на `https://pluma.chat/collect/…`: только с `Origin: https://web.telegram.org` и с токеном запуска.
   - Пишет их в `/opt/pluma/catalog/<запуск>/`. Манифест приходит последним.
   - Работает только во время выгрузки.
3. **Импорт** (`pluma-admin catalog import`).
   - Документы — в хранилище с id Pluma. Шесть документов, которые клиенты знают по id, сохраняют id Telegram.
   - Ссылки переписываются, хэши считаются заново.
   - Каталог заменяется одной транзакцией.
   - Повторный импорт не загружает уже загруженные файлы: id документов и наборов остаются прежними.

## Выгрузка и импорт
Команды compose на сервере — всегда с `docker-compose.public-ip.yaml` (`deployment.md`, «Схема»).

```bash
cd /opt/pluma
S="docker compose -f docker-compose.yaml -f docker-compose.public-ip.yaml -f docker-compose.web.yaml"
export PLUMA_CATALOG_RUN=$(date -u +%Y%m%d-%H%M) CATALOG_UPLOAD_TOKEN=$(openssl rand -hex 24)
mkdir -p /opt/pluma/catalog
$S -f docker-compose.catalog.yaml up -d catalog-receiver
```

Токен передаётся в скрипт выгрузки и больше нигде не хранится.

Во вкладке Telegram Web K: вставить содержимое `collector.js` в консоль, затем запустить:

```js
const T = '<токен>';
await collectCatalog({
  put: (path, body) => fetch('https://pluma.chat/collect/' + path, {method: 'PUT', headers: {'x-run-token': T, 'content-type': 'application/octet-stream'}, body})
    .then((r) => { if (!r.ok) throw new Error(path + ': ' + r.status); }),
  have: (path) => fetch('https://pluma.chat/collect/' + path, {method: 'HEAD', headers: {'x-run-token': T}}).then((r) => r.status === 204),
  log: console.log,
});
```

Если выгрузка прервалась (закрылась вкладка, остановился скрипт), её запускают снова в тот же каталог запуска (`PLUMA_CATALOG_RUN`). Скрипт заново соберёт ответы и наборы, свежие ссылки на файлы придут вместе с ними. Файлы, которые уже есть у приёмника (`have`), повторно не скачиваются.

Когда пришёл `manifest.json` (в логе приёмника: «receiver: N files»), приёмник останавливается, а каталог импортируется:

```bash
$S -f docker-compose.catalog.yaml rm -sf catalog-receiver
$S run --rm --no-deps -v /opt/pluma/catalog:/catalog bff ./pluma-admin -f ../etc2/bff.yaml catalog import /catalog/$PLUMA_CATALOG_RUN
rm -rf /opt/pluma/catalog/$PLUMA_CATALOG_RUN
```

Выгрузка удаляется после импорта: в ней `access_hash` и `file_reference` аккаунта Telegram.

- **Ошибка импорта** — например, ссылка на набор, которого нет в выгрузке, или файл без миниатюры. Каталог остаётся прежним. Загруженные документы сохраняются, следующий запуск их не повторяет.
- **Ошибка выгрузки** — манифест не пишется. Список несостоявшихся файлов лежит в `failed.json`.

## Обновление
Повторить выгрузку и импорт, не чаще раза в сутки и только по решению владельца. Изменённые наборы находятся по `short_name` и сохраняют свои id. Документы, которых у Telegram больше нет, остаются: на них ссылаются сообщения.
