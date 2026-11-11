# 078. Группы полностью

- Статус: в работе
- Зависит от: 042 (права), 047 (группа → супергруппа), 074 (слой 233).
- Источник: запрос владельца 2026-11-10 «закрывай группы полностью»; реестр методов и полей.

## Что не сделано на начало задачи
- **Теги участников** (`rank`). Базовая группа: назначить можно, но `message.from_rank` сервер не заполняет. Супергруппа: метод отвечает ошибкой. Запрет `edit_rank` не обслуживается.
- **Журнал действий** `channels.getAdminLog`.
- **От имени канала** `channels.getSendAs`.
- **Наборы стикеров группы** `channels.setStickers`, `channels.setEmojiStickers`.
- **Счётчик онлайн** `messages.getOnlines`.
- **Передача владения базовой группой**, `messages.getFutureChatCreatorAfterLeave`, `channels.getFutureCreatorAfterLeave`.
- **Заявки разом в базовой группе** `messages.hideAllChatJoinRequests`.
- **«Кто прочитал» в супергруппе** `messages.getMessageReadParticipants`.
- **Уведомление о скриншоте** `messages.sendScreenshotNotification`.
- **Реакции участника** `messages.deleteParticipantReaction(s)`, `messages.reportReaction`.
- **Адреса и оформление** `channels.toggleUsername`, `reorderUsernames`, `deactivateAllUsernames`, `updateColor`, `updateEmojiStatus`.
- **Прочее** `channels.getMessageAuthor`, `getInactiveChannels`, `toggleAntiSpam`, `toggleAutotranslation`, `getChannelRecommendations`.

## В процессе
Теги участников:
- базовые группы: права по клиентам (`canEditChatParticipantRank`), проверка длины и эмодзи (`channelperm.CheckRank`), `from_rank` ставится при отправке, как `post_author` (Android обновляет видимые сообщения локально; что делает Telegram со старыми сообщениями, снаружи не проверено);
- `updateChatParticipantRank` пока не шлётся: в слое 228 такого типа нет, `Encode` вернёт ошибку; нужна отправка по слою;
- супергруппы: нужен RPC `channel.editParticipantRank` (protoc + правка `class_name_registers`, `codec_schema`, `rpc_client_registers`, `channel_client`, `service_impl`, как в коммите eb4d42c56), `rank` в `channelParticipant` и `channelParticipantSelf`, `from_rank` в `SendChannel`.

Готово кодом: теги (базовые группы и супергруппы, e2e), `messages.getOnlines`, `messages.sendScreenshotNotification`.
Готово кодом также: `hideAllChatJoinRequests` для базовых групп; «кто прочитал» в супергруппах (таблица `channel_read_marks`, миграция `migrate-20261111-channel-read-marks.sql`; прочтения до выката не видны).
Готово также: `deleteParticipantReaction(s)` в супергруппах (в базовых группах реакции лежат в ящике каждого участника, не обслуживается).
Журнал действий (`channels.getAdminLog`, `pkg/adminlog`, 48 часов): пишутся название, описание, фото, адрес, вход, выход, тег, общие запреты, админы, ограничения и баны, правки и удаления чужих сообщений и постов, закрепы; следующие — медленный режим, подписи, реакции, ссылки-приглашения, темы.
Передача владения обычной группой (`chat.editChatCreator`, как в tdesktop).
Дальше по порядку: `getFutureChatCreatorAfterLeave`, `getSendAs`, стикеры группы, адреса и оформление.

## Результат
_Заполняется по завершении._
