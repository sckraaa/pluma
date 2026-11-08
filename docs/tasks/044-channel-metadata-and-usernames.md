# 044. Каналы: название, фото, настройки, реакции, публичные имена

- Статус: in progress (сервер готов; ждёт проверки в клиентах)
- Зависит от: 040
- ADR: 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` §1.5, §2.6, задача T9; `clients.md` §1 «Create», §3, §4 п. 1, 5, 8, 10; `plan.md` §3, задача «043» плана, §4 п. 3).

## Цель
Админ меняет название, фото и настройки канала, и все участники видят изменение сразу; работают реакции в каналах; у канала может быть публичное имя, по которому его находят; канал можно удалить.

## Что известно
- Все методы задачи, кроме `checkUsername` и `updateUsername` (`blocked`, `app/bff/usernames`), — `missing` в `api-coverage.md` (`server.md` §1.5). `messages.toggleNoForwards` работает только для базовых групп (`api-coverage.md:739`); `setChatAvailableReactions` — `missing` (`:722`).
- Видимое изменение — служебное сообщение в журнале канала и `updateChannel` (api.tl:345) без `seq` онлайн-участникам через `Channel-T`; офлайн-устройство получает новый `channel` в `chats` (ADR 0034, п. 3, 035). Web K перечитывает канал при смене подписей в профиле (`appChatsManager.ts:307-313`).
- Клиенты iOS: `ToggleChannelSignatures.swift`, `ChannelHistoryAvailabilitySettings.swift` (`togglePreHistoryHidden`).
- Экран создания Android использует `checkUsername`, `getAdminedPublicChannels`, `updateUsername` (`ui/ChannelCreateActivity.java:170-173`, `:935-944`); единственная блокирующая ошибка публичной ссылки — `CHANNELS_ADMIN_PUBLIC_TOO_MUCH`.
- Лимиты appConfig (значения по core.telegram.org/api/config совпадают с умолчаниями клиентов; лимиты каналов уже в `teamgramd/bin/app_config.json`, 035, U9):
  - `channels_public_limit_default` — 10 (Android `:1608`, iOS `UserLimitsConfiguration.swift:42`);
  - `reactions_uniq_max` — 11, `reactions_user_max_default` — 1, `reactions_in_chat_max` — 3;
  - `hidden_members_group_size_min` — 100 (Android `MessagesController.java:3788`, core.telegram.org/api/config) для скрытия участников;
  - `about_length_limit_*` — 70 (iOS `:47-49`).
  Разрешены ли публичные каналы в закрытой установке и какой лимит — вопрос владельцу из 035 (`plan.md` §4 п. 3).
- Реакции: `setChatAvailableReactions`, `sendReaction`, `getMessagesReactions`, `getMessageReactionsList`, `available_reactions`/`reactions_limit` в `channelFull`, `updateMessageReactions` (`clients.md` §4 п. 10). Хранение реакций — этап 2 (ROADMAP); здесь — ветка каналов на том, что уже есть для групп.
- `channels.updateColor` в `plan.md` не входит: цвет канала у Telegram открывается бустами (`channel_color_level_min`; в закреплённом Android `aac1efcb` ключ не найден, 035); остаётся 400, пока владелец не решит иначе (ADR 0016).

- **Отчёт владельца 2026-11-07:** реакции в каналах не работают (ни поставить, ни увидеть). Это область этой задачи: `sendReaction` и `getMessagesReactions` для каналов, `available_reactions` канала.

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
### Название и описание (2026-11-04, ветка `task/038-channel-create`)
- `channels.editTitle` → biz `channel.editTitle`: в транзакции редактора под блокировкой канала — новое название, служебное `messageActionChatEditTitle` в журнале канала (в супергруппе — от редактора, в канале — пост без автора, как первое сообщение канала) и пуш участникам: сообщение и `updateChannel`. Ответ — сообщение, `updateChannel` и канал с новым названием. То же название — `CHAT_NOT_MODIFIED`. Пустое — `CHAT_TITLE_EMPTY`, длиннее 128 символов — обрезается, как при создании.
- `messages.editChatAbout` для канала (модуль bff chats получил клиент biz channel) → biz `channel.editAbout`: описание и `updateChannel` участникам пушем; служебного сообщения в схеме для описания нет. То же описание — `CHAT_ABOUT_NOT_MODIFIED`, длиннее 255 — `CHAT_ABOUT_TOO_LONG`.
- Права: создатель и админы с `change_info`; в супергруппе обычный участник — если ни права по умолчанию, ни его ограничение не запрещают `change_info`; подписчик канала — никогда (`CHAT_ADMIN_REQUIRED`).
- **Проверка:** интеграционный `TestEditInfo`; e2e `TestChannelInfo` (пост о переименовании, `updateChannel` подписчику, `CHAT_NOT_MODIFIED`, подписчик не меняет ни название, ни описание, описание видно в `getFullChannel`).

### Публичные имена и бусты (2026-11-04, по отчёту владельца с Android)
Владелец на Android: публичный канал не создаётся («… is already taken» на любое имя), в канале всплывает `ERR_ENTERPRISE_IS_BLOCKED`, пост не редактируется («Sorry, you can't edit this message»). Причины найдены по прод-логам и исходникам Android `aac1efcb`:
- **«already taken»:** `channels.checkUsername` был заглушкой; Android показывает «занято» на любой ответ, кроме `true` и нескольких ошибок (`ChannelCreateActivity.java:1317-1347`).
- **Всплывающая ошибка:** `premium.getBoostsStatus` — сервис `RPCBoosts` не маршрутизирован; Android зовёт его при каждом открытии канала и показывает ошибку (`ChatActivity.java:22397-22428`, `ChannelBoostsController.java:58-60`).
- **Правка поста:** перед правкой Android вызывает `messages.getMessageEditData` и на любую ошибку показывает этот текст (`ChatActivity.java:32881-32899`); ветка каналов была заглушкой.

Сделано:
- `channels.checkUsername` (с `inputChannelEmpty` для ещё не созданного канала; лимит проверяется раньше формата, как ждёт проба Android с «1»), `channels.updateUsername` (транзакция редактора под блокировкой канала, общая таблица `username` с уникальным ключом, `USERNAME_OCCUPIED`, `USERNAME_NOT_MODIFIED`, «» — канал снова закрытый, участникам `updateChannel`), лимит 10 публичных каналов на создателя (`CHANNELS_ADMIN_PUBLIC_TOO_MUCH`), `channels.getAdminedPublicChannels`, `contacts.resolveUsername` находит каналы (вступление по имени).
- Бусты: `premium.getBoostsStatus` для канала — 0 бустов, уровень 0 (в Pluma бустов нет), `boost_url` — `t.me/boost/<имя>` или `t.me/boost?c=<id>` (`LaunchActivity.java:2119-2125`) на хосте ссылок сервера; `premium.getMyBoosts` — пусто. Остальные методы `RPCBoosts` — `ERR_ENTERPRISE_IS_BLOCKED`. Маршрут `/mtproto.RPCBoosts` во всех трёх `session.yaml`.
- `messages.getMessageEditData` для каналов (те же права, что у правки; `caption` — для сообщения с медиа).
- **Проверка:** интеграционные `TestUsername`, `TestUsernameRace`; e2e `TestChannelUsername` (проба создания, проверка и запись имени, поиск другим, вступление по имени, бусты), проверки `getMessageEditData` в `TestChannelEdit` и `TestChannelInfo`.
- **Молча падающие в Android (не видны пользователю, остаются):** `messages.getMessagesViews` (просмотры постов, 046), `messages.getMessagesReactions`, `messages.search` и `getSearchCounters` в канале (общие медиа), `messages.getAdminsWithInvites`, `messages.saveDraft` для канала (черновики в каналах теряются).
- **Ревью исправлений (до выката):** ссылку мог менять любой участник супергруппы (права проверялись как для названия), лимит публичных каналов считался по редактору, а не по создателю; в статусе бустов не было `next_level_boosts`, и Android рисует «Level -1» и максимальный уровень (`ChannelBoostsController.java:73`, `LimitPreviewView.java:631-634`). Исправлено: ссылку меняет только создатель — Android и показывает выбор типа только ему при `can_set_username` (`ChatEditActivity.java:891`), остальным `CHAT_ADMIN_REQUIRED`; лимит считается под блокировкой ящика создателя; `next_level_boosts` = 1 (пороги Telegram не сверены).

### Фото канала и реакции (2026-11-07, ветка `task/044-reactions`, по отчёту владельца)
- **Фото** (`channels.editPhoto` → biz `channel.editPhoto`): загруженное фото (и видео) сохраняется, как у базовой группы (`media.uploadProfilePhotoFile`), существующее берётся по id, пустое убирает фото. Запись в транзакции редактора под блокировкой канала, права — `change_info`; служебное `messageActionChatEditPhoto`/`messageActionChatDeletePhoto`, участники перечитывают канал. Служебные сообщения каналов теперь пишутся со своим типом медиа (смена фото находится фильтром фото чата).
- **Реакции** (миграция `migrate-20261107-channel-reactions.sql`, таблица `channel_message_reactions`):
  - запись — `msg.sendReaction` в транзакции реагирующего под блокировкой канала (держится лимит разных реакций `reactions.UniqMax`); в трансляции реагирует любой подписчик, в супергруппе — участник без запрета `send_reactions` (иначе `CHAT_WRITE_FORBIDDEN`, имя ошибки Telegram не сверено);
  - участники получают `updateMessageReactions` пушем (`min`, без своих отметок), клиенты к тому же опрашивают `messages.getMessagesReactions` (ветка каналов есть);
  - реакции есть в истории, поиске и `getMessages`; в супергруппе виден список отреагировавших (`getMessageReactionsList`), в трансляции — только счётчики (`can_see_list = false`, список — `BROADCAST_FORBIDDEN`; как это у Telegram, не сверено);
  - «недавние реакции» учитывают и каналы. Отметка «непрочитано» для автора в каналах не ведётся.
- **Проверка:** интеграционный `TestEditPhoto`, модульный `TestViewBroadcast`, e2e `TestChannelReactions` (ответ, пуш автору, `getMessagesReactions`, история, список, снятие реакции).
- **Осталось:** подписи (`toggleSignatures`), история для новых (`togglePreHistoryHidden`), скрытие участников, `toggleNoForwards` и `setChatAvailableReactions` для каналов; проверка в клиентах.

### Настройки (2026-11-08, ветка `task/044-settings`)
- Один RPC biz `channel.toggleSetting` (номера — `channel.Setting*`): в транзакции меняющего под блокировкой канала, права — `change_info`; тот же — `CHAT_NOT_MODIFIED`; версия канала растёт, участники перечитывают канал.
  - `channels.toggleSignatures` (оба варианта): посты трансляции подписываются именем автора (`post_author`, имя и фамилия). Подписи-ссылки на профиль (`profiles_enabled`) сделаны в задаче 045.
  - `channels.togglePreHistoryHidden`: новые участники супергруппы видят историю со своего вступления (как уже делали вступление и приглашение); у группы обсуждения — `CHAT_LINK_EXISTS` (её читают комментаторы).
  - `channels.toggleParticipantsHidden`: не-админам список участников — только админы (любой фильтр; список, а не ошибка — iOS повторяет ошибку бесконечно; как отвечает Telegram, не сверено); включение — от 100 участников (`hidden_members_group_size_min`), иначе `PARTICIPANTS_TOO_FEW`. В `channelFull` — `participants_hidden`, `can_view_participants` у не-админа выключен.
  - `messages.toggleNoForwards` для канала: пересылка из него — `CHAT_FORWARDS_RESTRICTED` (проверка в пересылке уже была).
  - `channels.toggleJoinToSend`: комментаторы группы обсуждения вступают сначала (иначе — гости, 048).
  - `channels.toggleJoinRequest` не включён: без заявок на вступление (041) он закрыл бы вход.
- Реакции: `messages.setChatAvailableReactions` для канала → biz `channel.setAvailableReactions` (как у базовых групп: нет / все без своих эмодзи / все / выбранные, до 100); `channelFull.available_reactions` отдаёт их (канал без выбора — стандартные); реакция вне списка — `REACTION_INVALID`. Вид канала (`immutableChannel`) получил `available_reactions_type` и `available_reactions` — конструктор сменился вместе с ними.
- **Проверка:** e2e `TestChannelSettings` (подписчик не меняет, подпись поста, повтор — `CHAT_NOT_MODIFIED`, запрет пересылки, разрешённая и неразрешённая реакции, скрытая история, `PARTICIPANTS_TOO_FEW`).
- **Осталось:** проверка в клиентах.
