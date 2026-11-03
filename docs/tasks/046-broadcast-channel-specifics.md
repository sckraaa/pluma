# 046. Каналы-трансляции: просмотры, подписи, ссылки на посты

- Статус: todo
- Зависит от: 040
- ADR: 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` §2.3, §2.6, задача T11; `clients.md` §1 «Open a channel», §4 п. 7–8; `plan.md` §3, задача «045» плана).

## Цель
У постов канала растут просмотры (один раз на пользователя), видна подпись автора, когда она включена, ссылку на пост можно скопировать.

## Что известно
- `messages.getMessagesViews` — `partial` (`api-coverage.md:599`). Клиенты зовут его с `increment` для видимых постов: Android каждые 5 с (`MessagesController.java:10579-10630`, 035), iOS (`AccountViewTracker.swift:723`), Web K. Android берёт оттуда и `replies` (там же, 035). `updateChannelMessageViews` — без `pts`, в журнал канала не идёт; рассылка — `Channel-T` (ADR 0034, пп. 1, 5).
- `updateChannelMessageViews` — Web K `appMessagesManager.ts:10865-10875`; поля `views` и `forwards` в постах (`clients.md` §4 п. 7).
- Дедупликация просмотров — отдельная таблица `channel_message_views` (`server.md` §2.3, «later task»).
- Подписи: `post_author`/`from_id` в постах, `channels.toggleSignatures` (044) (`clients.md` §4 п. 8); в broadcast публикуют только админы с `post_messages` (`server.md` §2.6).
- `channels.exportMessageLink` — `missing`. Формат ссылки — с учётом задачи 026 (ссылки pluma.chat вместо t.me).
- `channels.getSendAs` — `blocked`; в `plan.md` не входит, остаётся 400.

## Область
- Миграция `channel_message_views` (повторный запуск безопасен).
- `messages.getMessagesViews` с `increment` для каналов, рост `views` один раз на пользователя, `updateChannelMessageViews` (частоту рассылки ограничить).
- `post_author` и подписи в постах при включённых подписях.
- `channels.exportMessageLink`.
- Не трогаем: обсуждения и `replies` (048), `getSendAs`.

## Критерии приёмки
- Модульные тесты: сборка `post_author`, формат ссылки.
- Интеграционные тесты на MySQL через `pkg/testinfra`: просмотры растут один раз на пользователя при параллельных вызовах; миграция применяется дважды.
- e2e через `pkg/mtclient`: подписчик не может опубликовать; просмотры растут один раз на пользователя; подпись автора видна при включённых подписях; тест падает на текущем коде.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: счётчик просмотров, подпись автора и копирование ссылки на пост в iOS, Android, Desktop (CachyOS) и Web K.

## Результат
