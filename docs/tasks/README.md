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
| [006](006-client-build-pipeline.md) | Конвейер сборки клиентов | done (preflight подписи iOS — после профилей Apple владельца) |
| [007](007-conformance-harness.md) | Каркас сравнительных тестов | done |
| [008](008-security-p1.md) | Безопасность P1: 2FA, PFS, проверки протокола | done |
| [009](009-push.md) | Push: APNs (включая VoIP) и FCM | done |
| [010](010-updates-seq-difference.md) | seq, getDifference и полнота апдейтов | done (код) |
| [011](011-contacts.md) | Контакты: импорт по номеру и поиск людей | done |
| [012](012-web-client.md) | Web-клиент (Web K) в облаке | done |
| [013](013-dialog-filters.md) | Папки чатов | done |
| [014](014-premium-by-admin.md) | Premium, который выдаёт администратор | done |
| [015](015-app-config.md) | Конфиг приложения: help.getAppConfig | done |
| [016](016-first-deployment.md) | Первый запуск на сервере | in-progress (остался замер задержки) |
| [017](017-rebranding.md) | Ребрендинг клиентов | in-progress |
| [018](018-apple-site-warning.md) | Снять пометку Apple с домена | in-progress (владелец отправляет обращение) |
| [019](019-service-messages.md) | Служебные сообщения как у Telegram | in-progress (выкачено, ждёт проверки владельцем на Android) |
| [020](020-parity-quick-fixes.md) | Быстрые правки по сверке с Telegram | in progress |
| [021](021-language-packs.md) | Языковые пакеты | todo |
| [022](022-service-chat-history.md) | Служебный чат 777000 с историей на сервере | todo |
| [023](023-link-previews.md) | Превью ссылок | done |
| [024](024-telegram-data-import.md) | Импорт данных Telegram | todo |
| [025](025-moderation-and-premium-panel.md) | Панель модерации: жалобы и Premium | todo |
| [026](026-pluma-links.md) | Ссылки pluma.chat вместо t.me | todo |
| [027](027-external-services.md) | Внешние сервисы: карты, GIF, места, перевод, распознавание речи | todo |
| [028](028-pluma-texts.md) | FAQ Pluma | todo |
| [029](029-ios-install-from-site.md) | Установка iOS-клиента с сайта (Ad Hoc) | blocked |
| [030](030-sticker-sets-on-demand.md) | Наборы стикеров и эмодзи из Telegram по запросу | todo |
| [031](031-external-security-audit.md) | Внешний аудит безопасности | todo |
| [032](032-online-status-push.md) | Статус «в сети» доходит до собеседников сразу | done |
| [033](033-read-date-ios.md) | Время прочтения в iOS | done |
| [034](034-privacy.md) | Все настройки приватности работают как в Telegram | done |
| [035](035-channel-box-research.md) | Каналы: сверка с Telegram и ADR «Ящик канала» | in-progress |
| [036](036-channel-schema-and-box.md) | Каналы: схема и `pkg/channelbox` | done |
| [037](037-channels-service-skeleton.md) | Каналы: сервис biz `channel`, модуль bff `channels`, маршрутизация | todo |
| [038](038-channel-create-and-dialogs.md) | Каналы: создание, `getChannels`, `getFullChannel`, диалоги | todo |
| [039](039-channel-send-and-history.md) | Каналы: отправка, история, `channels.getMessages` | todo |
| [040](040-channel-difference.md) | Каналы: `updates.getChannelDifference` и `updateChannelTooLong` | todo |
| [041](041-channel-membership-and-invites.md) | Каналы: участники и приглашения | todo |
| [042](042-channel-rights.md) | Каналы: права админов, баны, права по умолчанию | todo |
| [043](043-channel-edit-delete-pin-read.md) | Каналы: правка, удаление, закрепление, прочтение | todo |
| [044](044-channel-metadata-and-usernames.md) | Каналы: название, фото, настройки, реакции, публичные имена | todo |
| [045](045-channel-slow-mode.md) | Супергруппы: медленный режим | todo |
| [046](046-broadcast-channel-specifics.md) | Каналы-трансляции: просмотры, подписи, ссылки на посты | todo |
| [047](047-group-to-supergroup-migration.md) | Переход базовой группы в супергруппу | todo |
| [048](048-channel-discussion-and-threads.md) | Каналы: группа обсуждения и комментарии | todo |
| [049](049-channels-conformance-and-client-pass.md) | Каналы: сравнительные тесты и проход по клиентам | todo |
| [050](050-chat-archive.md) | Архив чатов: перенос, автоархив, возврат из архива | done |
| [051](051-public-photo.md) | Фото «для всех» и личное фото контакта | todo |
| [052](052-media-quality.md) | Качество медиа: сжимаем меньше, чем Telegram | in progress |

Порядок этапа 0. Параллельно можно вести две линии — они не пересекаются по файлам:
- **сервер:** 001 → 005 → 002 → 003 → 004 → 011, 013 → 014 → 015 → 010 → 008 → 009 → 007;
- **клиенты:** 006 (Android) → 012 (Web) → iOS → Desktop, все готовы; раздача веб-клиента с сервера (TLS, `/apiws`) и ребрендинг — отдельными задачами, когда до них дойдёт очередь.

Задачи двух линий идут одновременно, каждая — в своей рабочей копии и ветке. Docker на машине разработки один и маленький: стенды и интеграционные тесты разных задач запускаются по очереди.
