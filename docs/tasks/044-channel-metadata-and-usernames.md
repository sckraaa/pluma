# 044. Каналы: название, фото, настройки, реакции, публичные имена

- Статус: todo
- Зависит от: 040
- ADR: «Ящик канала» (из 035). Исследование: `docs/tasks/035-research/` (`server.md` §1.5, §2.6, задача T9; `clients.md` §1 «Create», §3, §4 п. 1, 5, 8, 10; `plan.md` §3, задача «043» плана, §4 п. 3).

## Цель
Админ меняет название, фото и настройки канала, и все участники видят изменение сразу; работают реакции в каналах; у канала может быть публичное имя, по которому его находят; канал можно удалить.

## Что известно
- Все методы задачи, кроме `checkUsername` и `updateUsername` (`blocked`, `app/bff/usernames`), — `missing` в `api-coverage.md` (`server.md` §1.5). `messages.toggleNoForwards` работает только для базовых групп (`api-coverage.md:739`); `setChatAvailableReactions` — `missing` (`:722`).
- Видимое изменение — служебное сообщение в ящике канала и `updateChannel` (api.tl:345) участникам (`server.md` задача T9). Web K перечитывает канал при смене подписей в профиле (`appChatsManager.ts:307-313`).
- Клиенты iOS: `ToggleChannelSignatures.swift`, `ChannelHistoryAvailabilitySettings.swift` (`togglePreHistoryHidden`).
- Экран создания Android использует `checkUsername`, `getAdminedPublicChannels`, `updateUsername` (`ui/ChannelCreateActivity.java:170-173`, `:935-944`); единственная блокирующая ошибка публичной ссылки — `CHANNELS_ADMIN_PUBLIC_TOO_MUCH`.
- Лимиты appConfig (умолчания клиентов; значения Telegram — UNVERIFIED, U9):
  - `channels_public_limit_default` — 10 (Android `:1608`, iOS `UserLimitsConfiguration.swift:42`);
  - `reactions_uniq_max` — 11, `reactions_user_max_default` — 1, `reactions_in_chat_max` — 3;
  - `hidden_members_group_size_min` — 100 (Android `:1633`) для скрытия участников;
  - `about_length_limit_*` — 70 (iOS `:47-49`).
  Разрешены ли публичные каналы в закрытой установке и какой лимит — вопрос владельцу из 035 (`plan.md` §4 п. 3).
- Реакции: `setChatAvailableReactions`, `sendReaction`, `getMessagesReactions`, `getMessageReactionsList`, `available_reactions`/`reactions_limit` в `channelFull`, `updateMessageReactions` (`clients.md` §4 п. 10). Хранение реакций — этап 2 (ROADMAP); здесь — ветка каналов на том, что уже есть для групп.
- `channels.updateColor` в `plan.md` не входит: цвет канала у Telegram открывается бустами (`channel_color_level_min`, Android `MessagesController.java:4175-4318`, UNVERIFIED, U10); остаётся 400, пока владелец не решит иначе (ADR 0016).

## Область
- `channels.editTitle`, `editPhoto`, `toggleSignatures`, `togglePreHistoryHidden`, `toggleParticipantsHidden`, `deleteChannel`.
- Ветки каналов в `messages.toggleNoForwards`, `setChatAvailableReactions`, `sendReaction`.
- Публичные имена: `checkUsername` (полная проверка), `updateUsername`, `toggleUsername`, `reorderUsernames`, `deactivateAllUsernames`, `getAdminedPublicChannels`; `contacts.resolveUsername` находит каналы; уникальность имени общая с пользователями.
- Проверки прав (`change_info`, `delete_messages` и т. п.) — по роли, если 042 ещё нет; иначе `channelperm.Can`.
- Не трогаем: медленный режим (045), подписи в постах (`post_author`, 046).

## Критерии приёмки
- Модульные тесты: проверка имени (формат, лимит), сборка служебных сообщений.
- Интеграционные тесты на MySQL через `pkg/testinfra`: уникальность имени между каналами и пользователями при параллельной записи; удаление канала убирает его у всех участников.
- e2e через `pkg/mtclient`: на каждое видимое изменение — служебное сообщение и `updateChannel` другому участнику; `resolveUsername` находит канал; тест падает на текущем коде.
- `go run ./tools/apicoverage` перезапущен; обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.
- В клиентах, со скриншотами: смена названия, фото, подписей, истории для новых, скрытия участников, запрета пересылки и реакций видна другим участникам в iOS, Android, Desktop (CachyOS) и Web K; канал создаётся публичным и находится по имени; вступление по имени (перенесено из 041).

## Результат
