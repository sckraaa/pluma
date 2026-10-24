# 013. Папки чатов

- Статус: done
- Зависит от: 002, 003 (ящики и доставка), 012 (веб-клиент показал, что без папок нет списка чатов)
- ADR: 0011, 0012, 0016

## Цель
Папки чатов работают как в Telegram на всех официальных клиентах.
- Список папок, создание, правка, удаление и порядок.
- Изменения видны на других устройствах пользователя.
- Web K после входа показывает список чатов. Сейчас он пуст: клиент ждёт `messages.getDialogFilters` и без ответа не заканчивает инициализацию (`appDialogsManager.ts` в tweb).

## Контекст
- В layer 228 у `messages.getDialogFilters` два конструктора: `#efd48c89 → messages.DialogFilters` (новый, с `tags_enabled`) и `#f19ed96d → Vector<DialogFilter>` (старый). Оба объявлены в gRPC-сервисе `mtproto.RPCFolders` (`third_party/teamgram-proto`), `messages.toggleDialogFilterTags` — в `mtproto.RPCFolderTags`. Ни один модуль bff эти сервисы не реализует, поэтому в `docs/project/api-coverage.md` они `missing`.
  - `app/bff/bff/client/fake_rpc_result.go` отвечает пустым вектором на устаревшее имя `TLMessagesGetDialogFilters`.
  - Нужен модуль bff `app/bff/folders` по образцу соседних (`app/bff/drafts`) и его регистрация в `app/bff/bff/internal/server/server.go`.
- Сервис `app/service/biz/dialog` уже умеет `dialog.getDialogFilters` (`DialogFilterExt`), `insertOrUpdateDialogFilter`, `deleteDialogFilter`, `updateDialogFiltersOrder`. Таблица `dialog_filters` есть (`1_teamgram.sql`, уникальный ключ `(user_id, dialog_filter_id)` — `migrate-20220401.sql`). Но запись в апстриме игнорирует ошибки (всегда `boolTrue`), а `toggleDialogFilterTags`/`getDialogFilterTags` — заглушки: довести, а не дублировать.
- bff диалогов: `app/bff/dialogs/internal/core/`. Обработчик на метод — по образцу соседних.
- Апдейты на другие устройства: `updateDialogFilter`, `updateDialogFilterOrder`, `updateDialogFilters`. Посмотреть, как сервер сейчас доставляет апдейты без `pts` (например `updatePeerSettings` из `contacts.addContact`), и сделать так же. Полный `seq` — задача 010, не эта.
- Premium в Pluma нет.

## Область
1. **`messages.getDialogFilters`, оба конструктора.**
   - У пользователя без папок ответ такой же, как у Telegram, включая `dialogFilterDefault` («Все чаты»), если его даёт Telegram. Порядок — сохранённый пользователем.
   - Устаревший ответ из `fake_rpc_result.go` убрать.
2. **`messages.updateDialogFilter`** — создать, изменить, удалить папку (без `filter` = удаление).
   - Проверки и ошибки как у Telegram: `FILTER_ID_INVALID`, `FILTER_TITLE_EMPTY`, `FILTER_INCLUDE_EMPTY`, лимиты числа папок и чатов в папке.
   - Пиры из `include_peers`, `exclude_peers`, `pinned_peers` проверяются на доступность пользователю.
   - Лимиты — как для пользователя Telegram без Premium. Их же отдаёт `help.getAppConfig`, если он их отдаёт; иначе зафиксировать в коде и в отчёте.
3. **`messages.updateDialogFiltersOrder`** — порядок папок.
4. **`messages.getSuggestedDialogFilters`** — предложения как у Telegram или пустой список. Выбор обосновать по поведению официальных клиентов.
5. **`messages.toggleDialogFilterTags`** — ответ как у Telegram для пользователя без Premium.
6. **Апдейты.** Каждое изменение доходит до других сессий пользователя; сессия-инициатор не получает дубля, если Telegram его не шлёт.
7. **Тесты:**
   - юнит;
   - интеграционные на сервисе диалогов (`pkg/testinfra`, MySQL): порядок, удаление, лимиты, гонка двух правок;
   - e2e через `pkg/mtclient`: папка создаётся на одной сессии и видна на второй, апдейт пришёл, порядок сохраняется.

Не трогаем: `chatlists.*` (папки по ссылке), архив (`folder_id`), TL-схему, транспорт. Не в этой задаче: счётчик «0 messages» в «Избранном» у Web K (замечен в 012) — отдельно.

## Критерии приёмки
- e2e-сценарии из п. 7 зелёные.
- Интеграционные тесты зелёные с `-count=3 -shuffle=on`.
- `tools/apicoverage`: `messages.getDialogFilters` (оба), `updateDialogFilter`, `updateDialogFiltersOrder`, `getSuggestedDialogFilters`, `toggleDialogFilterTags` — `ok`.
- Ревьюер проверяет на Web K (сценарий `pluma-web`) и на Android-клиенте: список чатов виден, папка, созданная на одном клиенте, появляется на другом.

## Результат

Папки чатов работают на официальных клиентах. Web K после входа показывает список чатов: проверено сценарием `pluma-web` на стенде.

**Почему не работало.**
- В `session.yaml` сервисы `mtproto.RPCFolders` и `mtproto.RPCFolderTags` не были направлены в bff, и их не реализовывал ни один модуль. Прокси отвечал `ERR_ENTERPRISE_IS_BLOCKED`, кроме устаревшего фейкового `TLMessagesGetDialogFilters`.
- В сервисе диалогов запись папок игнорировала ошибки и при каждой правке переставляла папку наверх.
- Все выборки читали колонку `from_suggested`, которой нет ни в одной миграции.

**Сделано.**
- Новый модуль bff `app/bff/folders`:
  - `messages.getDialogFilters` в обоих конструкторах — `#efd48c89` (`messages.dialogFilters`, теги выключены) и `#f19ed96d` (вектор для старых слоёв): «Все чаты» (`dialogFilterDefault`) первыми, затем папки в порядке пользователя;
  - `messages.updateDialogFilter`: создание, правка, удаление;
  - `messages.updateDialogFiltersOrder`;
  - `messages.getSuggestedDialogFilters`: пусто, потому что предложения Telegram приходят с серверными строками на языке сервера;
  - `messages.toggleDialogFilterTags`: включение — `PREMIUM_ACCOUNT_REQUIRED`.
  - Архив (`folders.*`) и общие папки (`chatlists.*`) того же сервиса по-прежнему отвечают 400 `ERR_ENTERPRISE_IS_BLOCKED`.
- **Проверки — как у Telegram для пользователя без Premium (ADR 0016):**
  - id 2…255;
  - id в папке равен id запроса;
  - непустой заголовок, длиннее 12 символов обрезается вместе с сущностями;
  - `FILTER_INCLUDE_EMPTY`;
  - до 100 закреплённых и включённых чатов и до 100 исключённых;
  - пиры — только существующие пользователи, «Избранное» и группы, где пользователь состоит (`PEER_ID_INVALID`);
  - общие папки — `FILTER_NOT_SUPPORTED`.
- **Хранение** (сервис диалогов, `dao/dialog_filter.go`):
  - каждое изменение — транзакция ящика пользователя (блокировка сериализует правки с двух устройств);
  - лимит 10 папок (`DIALOG_FILTERS_TOO_MUCH`);
  - новая папка встаёт последней, правка сохраняет место;
  - перестановка игнорирует «Все чаты» и неизвестные id;
  - ошибки возвращаются клиенту.
- **Апдейты.** Другие устройства получают `updateDialogFilter` (с папкой или без неё) и `updateDialogFilterOrder` с сохранённым порядком.
- **Схема.** `migrate-20261024-dialog-filters.sql` добавляет `from_suggested`.

**Проверка.**
- Юнит-тесты проверок папки.
- Интеграционные тесты хранения на MySQL: места, лимит, гонка 20 параллельных созданий, перестановка.
- e2e `TestDialogFilters` на двух устройствах:
  - оба конструктора;
  - апдейт с новой папкой;
  - перестановка с апдейтом порядка;
  - правка и удаление;
  - коды ошибок.
- Сценарий `pluma-web` на стенде этой ветки: список чатов с «Избранным» виден в обоих браузерах.

**Решения на подтверждение.** ADR 0016 (нет Premium; имена `DIALOG_FILTERS_TOO_MUCH` и `FILTER_INCLUDE_TOO_MUCH` — для неофициальных клиентов, в схеме таких кодов нет).
