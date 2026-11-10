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
Начато с тегов участников: чтение клиентов и путей чтения сообщений.

## Результат
_Заполняется по завершении._
