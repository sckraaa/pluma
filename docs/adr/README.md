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
