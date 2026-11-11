# Архитектурные решения (ADR)

Одно решение — один файл `NNNN-kebab-name.md`. Статусы: `proposed` → `accepted` → (`superseded by NNNN`). Принятое решение не пересматривается в рабочем порядке: если оно перестало подходить, пишется новый ADR, который его заменяет.

Шаблон: [0000-template.md](0000-template.md).

| № | Решение | Статус |
|---|---|---|
| [0001](0001-official-clients-as-spec.md) | Официальные клиенты Telegram — спецификация и приёмочный тест | accepted |
| [0002](0002-conformance-testing.md) | Сравнительное тестирование с настоящим Telegram | accepted |
| [0003](0003-mtproto-security-baseline.md) | Базовый уровень безопасности MTProto | accepted |
| [0004](0004-calls-architecture.md) | Архитектура звонков: родной протокол + свой SFU | proposed |
| [0005](0005-scale-and-infrastructure.md) | Масштаб и инфраструктура: оставить и починить | accepted |
| [0006](0006-ios-distribution.md) | Распространение iOS: ad-hoc | accepted |
| [0007](0007-repo-as-memory.md) | Репозиторий как память проекта | accepted |
| [0008](0008-client-layer-pinning.md) | Закрепление версий клиентов по layer | accepted |
| [0009](0009-dependency-forks.md) | Свои копии teamgram/proto и teamgram/marmota в third_party | accepted |
| [0010](0010-cloud-client-builds.md) | Сборка клиентов в облаке (публичные репо, GitHub Actions) | accepted |
| [0011](0011-durable-sequences.md) | Долговечные счётчики: состояние ящика пользователя в MySQL и одна транзакция на ящик | accepted |
| [0012](0012-reliable-delivery.md) | Надёжная доставка: outbox в транзакции ящика, применение событий ровно один раз, push по порядку | accepted |
| [0013](0013-observability-alerts-backups.md) | Наблюдаемость, алерты на телефон (ntfy + healthchecks.io), бэкапы restic в S3 | accepted |
| [0014](0014-object-storage-garage.md) | Объектное хранилище: Garage вместо MinIO | accepted |
| [0015](0015-contacts-by-phone.md) | Контакты по номеру: телефонная книга на сервере, лимит, уведомления о регистрации | accepted |
| [0016](0016-no-premium.md) | Premium в Pluma: по умолчанию ни у кого, выдаёт администратор | accepted |
| [0017](0017-push-notifications.md) | Push-уведомления: отдельный сервис на потоке Sync-T, только зашифрованные push | accepted |
| [0018](0018-conformance-harness.md) | Сравнительные тесты: свой клиент mtclient, сценарии на Go, эталоны в JSON | accepted |
| [0019](0019-difference-composition-and-seq.md) | Состав updates.getDifference и seq состояния | accepted |
| [0020](0020-seq-journal.md) | Журнал seq: обновления состояния аккаунта, которые восстанавливает getDifference | accepted |
| [0021](0021-seq-to-other-users.md) | Обновления с seq другому пользователю: inbox-событие получателя | accepted |
| [0022](0022-seq-except-authorization.md) | Обновление с seq всем сессиям аккаунта, кроме одной (новый вход) | accepted |
| [0023](0023-geoip-geolite.md) | GeoIP: MaxMind GeoLite City, обновление без перезапуска, город только при малом радиусе | accepted |
| [0024](0024-android-push-session.md) | Пуш-сессия Android (token_type 7): приём регистрации и разбор id без знака | accepted |
| [0025](0025-language-packs.md) | Языковые пакеты: снимки из `langpack.*` Telegram, версии в MySQL, замена «Telegram» на «Pluma» при импорте | accepted |
| [0026](0026-service-chat-history.md) | Сообщения служебного аккаунта 777000 — настоящие сообщения истории; заменяет п. 4 ADR 0022 | accepted |
| [0027](0027-link-previews.md) | Превью ссылок: сборщик только в публичный интернет, кэш в MySQL, без webPagePending | accepted |
| [0028](0028-telegram-catalog-import.md) | Каталог из Telegram: выгрузка через открытую сессию Web K, перенумерация, хранение как ответы методов | proposed |
| [0029](0029-sticker-sets-on-demand.md) | Наборы стикеров из Telegram по запросу через бота; установленные наборы аккаунта в журнале seq | proposed |
| [0030](0030-online-status-push.md) | Статус «в сети»: состояние каждой авторизации в Redis, рассылка `updateUserStatus` без seq всем, кто видит пользователя | proposed |
| [0031](0031-privacy-evaluation.md) | Приватность: один вычислитель правил, членство и контакты — от сервиса пользователей | proposed |
| [0032](0032-chat-archive.md) | Архив чатов: перенос в транзакции ящика с pts, автоархив и возврат — на сервере | proposed |
| [0033](0033-public-photo.md) | Фото «для всех» и личное фото контакта: отдельные ссылки, а не альбом; выбор фото — один на всех путях | proposed |
| [0034](0034-channel-box.md) | Ящик канала: одна копия сообщений и свой `pts` на канал, журнал канала, рассылка через `Channel-T` без журнала на участника | accepted |
| [0035](0035-client-releases.md) | Выпуски клиентов: сервис `releases` на `/dl/` сайта, `help.getAppUpdate` по таблице `app_releases`, апдейтер Desktop со своим ключом | accepted |
| [0036](0036-pluma-links.md) | Ссылки pluma.chat: один хост ссылок, сервис `links` за Caddy, клиенты считают pluma.chat своим | accepted |
| [0037](0037-ios-release-versions.md) | Выпуски iOS: версия повторяется, сборку различает номер; конфликт версии — 422, а не 409 | accepted |
| [0038](0038-channel-rights.md) | Права в каналах: одна функция прав, ограниченный остаётся участником, исключённый — своё состояние, ошибки по типу медиа | accepted |
| [0039](0039-message-search.md) | Поиск по сообщениям: слова и основы в запросе, без отдельного индекса | accepted |
| [0043](0043-bots-core.md) | Боты на уровне MTProto: обычные аккаунты, токены только хэшем, управление через `pluma-admin` | proposed |
| [0040](0040-moderation-panel.md) | Модерация: очередь жалоб, ограничения, панель только в tailnet (`tailscale serve`, ключи доступа, шаги решений) | proposed |
| [0042](0042-admin-console.md) | Панель — пульт владельца: без ролей, блокировка каналов и групп, регистрация открыта или по списку | proposed |
| [0041](0041-premium-and-boosts-for-everyone.md) | Все функции Premium и бустов доступны всем | accepted (2026-11-09) |
| [0044](0044-login-codes-through-tdlib.md) | Коды входа пишет настоящий аккаунт Telegram: отдельный сервис на TDLib, запасной путь — администратор | proposed |
| [0045](0045-several-layers-at-once.md) | Сервер обслуживает несколько слоёв сразу: кодировка по слою сессии, клавиатуры в форме 228 |
| [0047](0047-channel-recommendations.md) | Похожие каналы: подбор по общим читателям, порядок — пересечение, размер, id; лимит 100, ответ без скрытых «для Premium» | proposed |
| [0048](0048-support-ghost.md) | Аккаунт-призрак: скрытая строка участника, вход по упоминанию в той же транзакции, вся история; вне счётчиков, списков, журнала и просмотров | proposed |
| [0049](0049-channel-media-unread.md) | Непрослушанные голосовые и кружки в каналах — у каждого участника свои (`channel_contents_read`) | proposed |
