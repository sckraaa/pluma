# 080. Группа-трансляция (channels.convertToGigagroup)

- Статус: сделано (ветка `task/080-gigagroup`, ждёт сборки партии).

## Что нужно
Создатель большой супергруппы превращает её в группу-трансляцию (`channel.gigagroup`): пишут только админы, участники читают, как в канале. Переход необратим. Метод `channels.convertToGigagroup` до этого отвечал `ERR_ENTERPRISE_IS_BLOCKED`.

## Источники
- core.telegram.org/api/channel, раздел про гигагруппы: «одностороннее» превращение, писать могут только админы (как если бы у всех участников был выключен `send_messages`), лимит участников снимается, приглашать нельзя; предложить превращение сервер может подсказкой `CONVERT_GIGAGROUP` в `channelFull.pending_suggestions` (core.telegram.org/api/config, раздел channel suggestions).
- core.telegram.org/method/channels.convertToGigagroup: ошибки `CHANNEL_INVALID`, `CHAT_ADMIN_REQUIRED`, `FORUM_ENABLED`, `PARTICIPANTS_TOO_FEW`, `CHAT_WRITE_FORBIDDEN`. Ни служебного сообщения, ни события «Недавних действий» о превращении в схеме нет.
- Клиенты (`reference-src-new`): все четыре предлагают превращение только создателю супергруппы (не гигагруппы) от `megagroup_size_max - 1000` участников — Android `ChatUsersActivity.java:434-436`, tdesktop `edit_peer_permissions_box.cpp:1300-1305`, iOS `ChannelPermissionsController.swift:735`, Web K `groupPermissions.tsx:201-205`. Ответ применяют как `Updates` (Android `MessagesController.convertToGigaGroup`, tdesktop `AboutGigagroupCallback`, iOS `convertGroupToGigagroup` с таймаутом 5 с, Web K `appChatsManager.convertToGigagroup`).
- Как клиенты рисуют гигагруппу: Android разрешает писать только админам по флагу (`ChatObject.java:2354`); tdesktop решает по запретам по умолчанию (`CanSendAnyOf`, `data_chat_participant_status.cpp:284-290`) и прячет настройку прав (`ChannelData::canEditPermissions`, `data_channel.cpp:801-805`); флаг берёт и из min-канала (`data_session.cpp:1110-1116`).

## Результат
Метод работает. Создатель супергруппы от `GigagroupMin` участников (`MembersMax - 1000` = 199 000) превращает её одной транзакцией бокса: в `channels` ставится новый столбец `gigagroup` (миграция `migrate-20261112-channel-gigagroup.sql`), к запретам по умолчанию добавляются все запреты отправки (`channelperm.SendBanned`, список tdesktop `AllSendRestrictionsList` плюс `send_messages` и `send_media`), версия канала растёт, участники получают `updateChannel`. Ответ — `updateChannel` и канал с `gigagroup`. С этого момента `channelperm` считает участника гигагруппы читателем, как подписчика канала: отправка — `CHAT_WRITE_FORBIDDEN`, остальное — `CHAT_ADMIN_REQUIRED`; админы пишут без `post_messages`, как в супергруппе. Флаг идёт в полный и в min-канал.

Изменения:
- `pkg/channelperm`: поле `Channel.Gigagroup`, правило и `SendBanned`; флаг передаётся во все места, где строится `channelperm.Channel` из строки канала (biz channel `rightsActor`, `infoEditor`; msg — отправка и отложенные).
- biz channel: поле `gigagroup = 48` в `ImmutableChannel` (protoc по `build2.sh`, protoc-gen-go v1.36.11; в TL-кодеке — флаг 17), `channel.SettingGigagroup` в `channel.toggleSetting` (только включение), `dao.ConvertToGigagroup`; `messages.editChatDefaultBannedRights` в гигагруппе — `CHANNEL_INVALID`.
- bff chats: обработчик `channels.convertToGigagroup` (проверка access_hash, удалённый канал — `CHANNEL_PRIVATE`).
- `megagroup_size_max` в `teamgramd/bin/config.json`: 100 000 → 200 000, как `MembersMax` сервера и Telegram; иначе клиенты предлагали бы превращение с 99 000 участников, а сервер отвечал бы `PARTICIPANTS_TOO_FEW`. Константа вынесена в `channel.MembersMax`, тест `help.getConfig` сверяет их.
- e2e `TestChannelsNotServed` берёт примером `channels.editLocation` вместо этого метода.
- `channels.toggleForum` не включает форум в гигагруппе (`CHANNEL_INVALID`): форум в гигагруппу не превращается (`FORUM_ENABLED`), и tdesktop не предлагает форум гигагруппе (`edit_peer_info_box.cpp:1553-1557`). Ошибка Telegram для этого случая не проверена; шаг есть в `TestConvertToGigagroup` (без проверки падает).

Проверка: `go build ./...`, `go vet -tags integration,e2e -unreachable=false` затронутых пакетов и `./test/e2e/`, модульные тесты затронутых пакетов и `./tools/...`, `-race` по `channelperm`, `biz/channel/channel`, `bff/chats/core`, интеграционные тесты `biz/channel/...` и `msg/...` на MySQL — зелёные (CachyOS). Новые тесты: `TestGigagroup` (channelperm; без правила падает), `TestGigagroupView` и флаг в `TestCodecRoundTrip`, `TestConvertToGigagroup` (интеграционный), `TestChannelGigagroup` (e2e, число участников выставляется в MySQL стенда). e2e только собран: стенд в задаче не поднимался, тест идёт при прогоне партии.

Решения, требующие подтверждения:
- Порог `PARTICIPANTS_TOO_FEW` у Telegram не опубликован; взят порог клиентов (`megagroup_size_max - 1000`). При 100 пользователях превращение на проде недостижимо без правки счётчика в базе.
- Повторное превращение — `CHAT_NOT_MODIFIED`, лишний `enabled=false` — `INPUT_REQUEST_INVALID`; ответ Telegram на повтор не проверен.
- «Недавние действия»: события превращения в схеме нет, поэтому пишется событие смены запретов по умолчанию (что и правда меняется).
- Форум в гигагруппе включить нельзя (`CHANNEL_INVALID`); ошибка Telegram не проверена.
- Запреты по умолчанию в гигагруппе менять нельзя (`CHANNEL_INVALID`, как у канала): клиенты эту настройку не показывают; ошибка Telegram не проверена.
- Лимит участников гигагруппы — общий серверный `MembersMax`, как у каналов Pluma (у Telegram лимит снимается).
- Админы по-прежнему могут добавлять участников (`inviteToChannel`), участники — нет; вход по ссылке и по адресу как раньше. Фраза документации «приглашать нельзя» не уточняет, к кому она относится.

Не сделано: подсказка `CONVERT_GIGAGROUP` в `channelFull.pending_suggestions` (нужно хранить её отклонение для канала: `help.dismissSuggestion` с каналом сейчас отвечает `ERR_ENTERPRISE_IS_BLOCKED`; к тому же tdesktop ждёт строку `convert_to_gigagroup`, а Android и iOS — `CONVERT_GIGAGROUP`). Клиенты всё равно предлагают превращение в настройках без подсказки. Голосовых чатов в Pluma нет, поэтому правило «в гигагруппе участники голосового чата без микрофона» не применяется. Записи `feature-registry` о методе и поле не трогались: их обновляет сборка партии.
