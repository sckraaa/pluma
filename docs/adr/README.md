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
