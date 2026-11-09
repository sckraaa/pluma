# Задачи

Одна задача — один файл `NNN-kebab-name.md`. Бриф пишется так, чтобы по нему можно было начать работу без дополнительного контекста.

Шаблон:

```markdown
# NNN. Название

- Статус: todo | in-progress | done | blocked
- Зависит от: NNN, …
- ADR: NNNN, …

## Цель
Что должно стать правдой после задачи.

## Область
Файлы/сервисы, которые трогаем; что явно НЕ трогаем.

## Критерии приёмки
- Проверяемые пункты: тесты, сценарии в клиентах, метрики.

## Результат
(заполняется по завершении: что сделано, где, что осталось)
```

| № | Задача | Статус |
|---|---|---|
| [001](001-security-p0.md) | Безопасность P0: закрыть критические дыры | done (код) |
| [002](002-durable-sequences.md) | Долговечные счётчики и транзакционный путь записи | done |
| [003](003-reliable-delivery.md) | Надёжная доставка через Kafka и push в сессии | done |
| [004](004-deployment.md) | Развёртывание, наблюдаемость, бэкапы | done |
| [005](005-tests-and-ci.md) | Зелёная база тестов и CI | done |
| [006](006-client-build-pipeline.md) | Конвейер сборки клиентов | done (сборки и вход всех клиентов) |
| [007](007-conformance-harness.md) | Каркас сравнительных тестов | done |
| [008](008-security-p1.md) | Безопасность P1: 2FA, PFS, проверки протокола | done |
| [009](009-push.md) | Push: APNs (включая VoIP) и FCM | done |
| [010](010-updates-seq-difference.md) | seq, getDifference и полнота апдейтов | done (код) |
| [011](011-contacts.md) | Контакты: импорт по номеру и поиск людей | done |
| [012](012-web-client.md) | Web-клиент (Web K) в облаке | done |
| [013](013-dialog-filters.md) | Папки чатов | done |
| [014](014-premium-by-admin.md) | Premium, который выдаёт администратор | done |
| [015](015-app-config.md) | Конфиг приложения: help.getAppConfig | done |
| [016](016-first-deployment.md) | Первый запуск на сервере | in progress (сервер, телефон и веб-клиент работают) |
| [017](017-rebranding.md) | Ребрендинг клиентов | in-progress |
| [018](018-apple-site-warning.md) | Снять пометку Apple с домена | in progress (владелец отправляет обращение) |
| [019](019-service-messages.md) | Служебные сообщения как у Telegram | in progress (выкачено 2026-10-29, сервер на `5ddf8e89`) |
| [020](020-parity-quick-fixes.md) | Быстрые правки по сверке с Telegram | in progress |
| [021](021-language-packs.md) | Языковые пакеты | done (2026-11-01) |
| [022](022-service-chat-history.md) | Служебный чат 777000 с историей на сервере | done (2026-11-02) |
| [023](023-link-previews.md) | Превью ссылок | done |
| [024](024-telegram-data-import.md) | Импорт данных Telegram | in progress (2026-11-02) |
| [025](025-moderation-and-premium-panel.md) | Панель модерации: жалобы и Premium | in progress (панель написана, ждёт Tailscale; ждёт проверки) |
| [026](026-pluma-links.md) | Ссылки pluma.chat вместо t.me | done (выкачено 2026-11-06, прод `74decf8d`) |
| [027](027-external-services.md) | Внешние сервисы: карты, GIF, места, перевод, распознавание речи | todo |
| [028](028-pluma-texts.md) | FAQ Pluma | todo |
| [029](029-ios-install-from-site.md) | Установка iOS-клиента с сайта (Ad Hoc) | in progress (2026-11-01) |
| [030](030-sticker-sets-on-demand.md) | Наборы стикеров и эмодзи из Telegram по запросу | in progress (2026-11-02) |
| [031](031-external-security-audit.md) | Внешний аудит безопасности | todo |
| [032](032-online-status-push.md) | Статус «в сети» доходит до собеседников сразу | done |
| [033](033-read-date-ios.md) | Время прочтения в iOS | done |
| [034](034-privacy.md) | Все настройки приватности работают как в Telegram | done |
| [035](035-channel-box-research.md) | Каналы: сверка с Telegram и ADR «Ящик канала» | in-progress |
| [036](036-channel-schema-and-box.md) | Каналы: схема и `pkg/channelbox` | done |
| [037](037-channels-service-skeleton.md) | Каналы: сервис biz `channel`, модуль bff `channels`, маршрутизация | done |
| [038](038-channel-create-and-dialogs.md) | Каналы: создание, `getChannels`, `getFullChannel`, диалоги | in progress (сервер выкачен 2026-11-04; проверка в клиентах) |
| [039](039-channel-send-and-history.md) | Каналы: отправка, история, `channels.getMessages` | done (сервер) |
| [040](040-channel-difference.md) | Каналы: `updates.getChannelDifference` и `updateChannelTooLong` | done (сервер) |
| [041](041-channel-membership-and-invites.md) | Каналы: участники и приглашения | in progress (сервер готов; ждёт проверки в клиентах) |
| [042](042-channel-rights.md) | Каналы: права админов, баны, права по умолчанию | in progress (выкачено, ждёт проверки в клиентах) |
| [043](043-channel-edit-delete-pin-read.md) | Каналы: правка, удаление, закрепление, прочтение | in progress (сервер готов; ждёт проверки в клиентах) |
| [044](044-channel-metadata-and-usernames.md) | Каналы: название, фото, настройки, реакции, публичные имена | in progress (нет фото канала, настроек и реакций) |
| [045](045-channel-slow-mode.md) | Супергруппы: медленный режим | in progress (сервер готов; ждёт проверки в клиентах) |
| [046](046-broadcast-channel-specifics.md) | Каналы-трансляции: просмотры, подписи, ссылки на посты | in progress (сервер готов; ждёт проверки в клиентах) |
| [047](047-group-to-supergroup-migration.md) | Переход базовой группы в супергруппу | in progress (сервер готов) |
| [048](048-channel-discussion-and-threads.md) | Каналы: группа обсуждения и комментарии | in progress (сервер готов; ждёт проверки в клиентах) |
| [049](049-channels-conformance-and-client-pass.md) | Каналы: сравнительные тесты и проход по клиентам | todo |
| [050](050-chat-archive.md) | Архив чатов: перенос, автоархив, возврат из архива | done |
| [051](051-public-photo.md) | Фото «для всех» и личное фото контакта | done |
| [052](052-media-quality.md) | Качество медиа: сжимаем меньше, чем Telegram | in progress |
| [053](053-reactions.md) | Реакции на сообщения и теги «Избранного» без Premium | in progress |
| [054](054-client-releases-and-self-update.md) | Сборки клиентов на сайте и самообновление | in progress (сервер выкачен; апдейтеры на устройствах не проверены) |
| [055](055-channel-fixes.md) | Каналы после проверки владельцем | done |
| [056](056-memory-limits.md) | Инцидент 05.11: сеть сервера и потолки памяти | done |
| [057](057-dfs-memory.md) | dfs: файлы не держатся в памяти, части загрузок не в Redis | done |
| [058](058-premium-stickers.md) | Премиум-стикеры и эмодзи без Premium | in progress (патчи клиентов на ветках, не выпущены) |
| [059](059-ios-camera-crash.md) | iOS: вылет при открытии камеры из скрепки | in progress (причина найдена, pluma-ios PR #3) |
| [060](060-forums-and-topics.md) | Форумы и темы | in progress (сервер готов; ждёт проверки в клиентах) |
| [061](061-scheduled-messages.md) | Отложенные сообщения | in progress (сервер готов; ждёт проверки в клиентах) |
| [062](062-polls-and-checklists.md) | Опросы и чек-листы | in progress (сервер готов; ждёт проверки в клиентах) |
| [063](063-ttl-drafts-archive.md) | Автоудаление, черновики, архив | in progress (сервер готов; ждёт проверки в клиентах) |
| [064](064-shared-folders.md) | Общие папки | in progress (сервер готов; ждёт проверки в клиентах) |
| [065](065-wallpapers-and-themes.md) | Обои и темы | in progress (сервер готов; ждёт проверки в клиентах) |
| [066](066-search.md) | Поиск | in progress (сервер готов; ждёт проверки в клиентах) |
| [067](067-channel-gaps.md) | Хвосты партии каналов | in progress (сервер готов; ждёт проверки в клиентах) |
| [068](068-calls-1to1.md) | Звонки один на один | in progress |
| [069](069-secret-chats.md) | Секретные чаты | in progress |
| [070](070-bots-core.md) | Боты (MTProto) | in progress |
| [071](071-premium-for-everyone.md) | Premium и бусты для всех | in progress |
| [072](072-admin-panel.md) | Панель управления | todo |
| [073](073-login-codes.md) | Коды входа: Telegram, SMS | todo |
| 074 | Обновление версий клиентов (Android: интерфейс, камера в кружках) | todo, после 072 |

Порядок этапа 0. Параллельно можно вести две линии — они не пересекаются по файлам:
- **сервер:** 001 → 005 → 002 → 003 → 004 → 011, 013 → 014 → 015 → 010 → 008 → 009 → 007;
- **клиенты:** 006 (Android) → 012 (Web) → iOS → Desktop, все готовы; раздача веб-клиента с сервера (TLS, `/apiws`) и ребрендинг — отдельными задачами, когда до них дойдёт очередь.

Задачи двух линий идут одновременно, каждая — в своей рабочей копии и ветке. Docker на машине разработки один и маленький: стенды и интеграционные тесты разных задач запускаются по очереди.
