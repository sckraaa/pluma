# 067. Известные пробелы каналов и форумов после партии

- Статус: done (сервер; полный прогон партии и проверка в клиентах — позже)
- Зависит от: 043, 045, 046, 047, 048, 060, 061, 063, 064, 041
- ADR: новых решений уровня ADR нет

## Цель
Закрыть пробелы, оставленные партией: настройки уведомлений и непрочитанные реакции по темам, автоудаление в канале, повтор отложенных сообщений, выход из базовых групп по папке, ссылки групп после перехода в супергруппу, остатки брифов 043, 045, 046.

## Что выяснено у клиентов и в документации
- **Настройки темы.** `notifyForumTopic`/`inputNotifyForumTopic` читают и шлют все клиенты: Android `NotificationsController.java:6071`, `MessagesController.java:20051`, tdesktop `apiwrap.cpp:1975,2017,2092`, `data_notify_settings.cpp:114,133`, iOS `AccountStateManagementUtils.swift:1351`, Web K `appNotificationsManager.ts:255,506`. Исключения форума iOS запрашивает `account.getNotifyExceptions(peer: inputNotifyPeer(форум))` и читает из ответа `updateNotifySettings(notifyForumTopic)` (`ForumChannels.swift:1345-1363`); документация о темах молчит (core.telegram.org/method/account.getNotifyExceptions).
- **Непрочитанные реакции** определяются по `messagePeerReaction.unread` в `recent_reactions`: Android `MessagesController.java:19509` (`hasUnreadReactions`), tdesktop `data_message_reactions.cpp:1761,2169,2273`. Счётчик клиенты держат сами и сверяют с `dialog.unread_reactions_count` (Android `MessagesController.java:9801`).
- **Повтор отложенных** (core.telegram.org/api/scheduled-messages): только Premium; значения — от суток до года (Android `AlertsCreator.java:4604-4610`: 1, 7, 14, 30, 91, 182, 365 дней); только одно сообщение; после отправки сервер сам ставит следующую на «текущее время плюс период» с обновлениями как при новой постановке. Клиенты шлют период при каждой правке, и его отсутствие снимает повтор: tdesktop `api_editing.cpp:86`, `history_view_scheduled_section.cpp:408`, `history_item.cpp:2334-2342`; Android `SendMessagesHelper.java:3294`. tdesktop и Android не предлагают повтор без Premium (`history_view_schedule_box.cpp:358-383`, `AlertsCreator.java:4726`).
- **Ссылка группы после перехода** остаётся рабочей и ведёт в супергруппу: так было решено в 047; проверка показала, что в 047 хэш старой ссылки остаётся «ссылкой группы» и уходил в biz chat, откуда отвечала ошибка.
- **`via_chatlist` у вступивших** показывают tdesktop `api_invite_links.cpp:61` и Android `MemberRequestCell.java:103`.
- **`updateChannelUserTyping`** с `top_msg_id`: core.telegram.org/constructor/updateChannelUserTyping, tdesktop `api_updates.cpp:2020-2027`.
- `messages.getMessagesViews` «sent or forwarded from a channel» (core.telegram.org/method/messages.getMessagesViews).

## Решения
- Настройки темы — новая таблица `user_topic_notify_settings` (миграция `migrate-20261109-topic-notify-settings.sql`, применяется службой миграций), не строка `user_notify_settings`: уникальный ключ и все запросы старой таблицы рассчитаны на пару пир—пользователь. Пишет biz user: тот же box-транзакционный seq и формат значений.
- Автоудаление в канале — новый RPC `channel.expireMessages`: воркер msg только находит каналы с просроченными строками, удаление — в biz channel той же `deleteRows` (и `EmitDeletePush`), под блокировкой каналов связей по возрастанию id. Корень темы не удаляется.
- Непрочитанные реакции — колонка `unread` строки `channel_message_reactions` (была в таблице, не использовалась). Пуш min несёт отметки только автору (`mentioned` у `sync.pushChannelUpdates`): иначе min-обновление затирало бы у автора отметку.
- Чтение при выключении форума — `keepTopicReads`: отметка диалога доходит до места, где всё предшествующее прочитано; непрочитанные не становятся прочитанными (Telegram не описывает; решение на подтверждение ниже).

## Область
Сделано всё из постановки, кроме перечисленного в «Открытые вопросы».

## Результат
На сервере закрыты: настройки уведомлений по темам (`account.getNotifySettings`/`updateNotifySettings` с `inputNotifyForumTopic`, `notify_settings` в `forumTopic`, `getNotifyExceptions` по форуму, push и `resetNotifySettings` их учитывают, удаление темы их снимает); непрочитанные реакции в супергруппах и по темам (`unread_reactions_count` диалога и темы, `messages.getUnreadReactions`/`readReactions` с `top_msg_id`, снятие через `readMessageContents`); `unread_poll_votes_count` по темам, чтение голосов по теме теперь считает `topic_id` в форуме; `messages.getSearchCounters` с темой и каналом; `messages.setTyping` в супергруппе и с темой (раньше не слалось вовсе); создатели тем в `users` у `messages.channelMessages`; чтение только по темам переживает выключение форума; корень темы после выключения форума удаляется; повторное `upsertDialog` не сдвигает отметку существующего диалога форума; служебные сообщения чек-листов в форуме не сдвигают её (`serviceMoved`). Автоудаление в канале идёт через общую `deleteRows` (`channel.expireMessages`): копии постов, комментарии, темы и опросы обрабатываются. `schedule_repeat_period` работает: правка и повтор, следующая отправка в той же транзакции, что и текущая. `chatlists.leaveChatlist` и подсказки включают базовые группы, у вступивших по ссылке папки — `via_chatlist`. Ссылка базовой группы после перехода ведёт в супергруппу (`checkChatInvite`/`importChatInvite`), вступившие и ждущие заявки перенесены. `getMessagesViews` отдаёт просмотры поста, пересланного в личный чат или группу. `getLeftChannels` не считает комментатора-гостя.

- **Новые RPC:** `channel.expireMessages`, `channel.getUnreadReactions`, `channel.readReactions`, `user.setTopicNotifySettings`, `user.getTopicNotifySettings`, `user.getTopicNotifyExceptions`.
- **Миграция:** `migrate-20261109-topic-notify-settings.sql`, действий оператора нет.
- **Тесты.** Интеграционные `TestExpireMessagesTakesTheCopy`, `TestExpireMessagesInTopic`, `TestTopicCounters`, `TestForumOffKeepsReads`, `TestImportersViaChatlist`, `TestLeftChannelsSkipsCommenters` (biz channel), `TestAutoDeleteChannel` (msg, вызов biz); модульные `TestRepeatPeriodOf`, `TestApplyScheduledEditRepeat`, `TestOverTopic`, `TestTopicOf`, `TestWithUnreadOf`, `TestTopicNotifyRowSettings`, `TestChannelViewUnreadReactions`; e2e `TestForumTopicGaps`, `TestMigratedGroupLink`.
- **Проверка на ветке:** `go build ./...`, `go vet -tags integration,e2e -unreachable=false` и `go test` затронутых пакетов — зелёные. Интеграционные и e2e-тесты написаны и компилируются; выполнены будут в общем прогоне партии, как и `-race`, `make lint`. `TestChannelViewUnreadReactions` падает без снятия отметки в `sync` (проверено).

## Решения на подтверждение
- Без Premium `schedule_repeat_period` → `PREMIUM_ACCOUNT_REQUIRED` (документация: «Premium users can»); ошибка в документации не названа, значение недопустимого периода — `SCHEDULE_DATE_INVALID` (не проверено).
- Повтор, поставленный близко к текущему моменту (`schedule_date` меньше 10 с вперёд), остаётся в очереди и уйдёт со следующим тиком воркера, а не отправляется сразу: иначе не от чего считать следующую отправку.
- Выключение форума: прочитанное за первым непрочитанным сообщением другой темы снова считается непрочитанным (потерь «прочтения» нет, но есть лишние непрочитанные). Как делает Telegram, не проверено.
- `updateMessageReactions` с `top_msg_id` ставится только для не-General, как и `top_msg_id` сообщений General.
- `setTyping` — раздача по участникам супергруппы синхронно из bff; ошибка только логируется, сигнал короткоживущий.
- `messages.sendScheduledMessages` у повторяющегося сообщения ставит следующую отправку так же, как срабатывание по времени. core.telegram.org/api/scheduled-messages о ручной отправке молчит (не проверено).
- Настройки темы действуют, пока канал — форум: push спрашивает General о каждом сообщении супергруппы, поэтому после выключения форума настройки General не должны глушить группу. Строка остаётся и снова действует при повторном включении.

## Открытые вопросы (не сделано)
- Черновики по темам (060) — есть отдельная таблица `thread_drafts` из другой задачи; в постановке этой задачи их нет.
- `updateChannelParticipant.via_chatlist` и журнал действий канала: Pluma не шлёт `updateChannelParticipant` и не ведёт журнал администраторов (только `via_chatlist` у списка вступивших).
- Интеграционного теста самой повторной отправки нет: путь — ядро `MsgCore`, тестовой обвязки MySQL для него нет, а e2e требует Premium и минимальный период сутки. Проверяется `TestRepeatPeriodOf`, `TestApplyScheduledEditRepeat` и общим кодом отправки.
- Живые клиенты (Android, iOS, Desktop, Web K) не проверены; проверка — за владельцем.
- Служебное сообщение чек-листа или опроса (`PostChannelService`) в форуме всегда попадает в General, даже если сообщение из темы: `topic_id` не пишется. Так было до этой задачи.
- Группы, ставшие супергруппами до этой задачи: `chat_invite_participants` и ждущие заявки не перенесены (перенос только при миграции чата, отдельной SQL-миграции данных нет).
- Решения 043 и 045 о поведении Telegram без документации (`available_min_id`, первое сообщение канала, `post_author`) остаются вопросами, новых данных нет.
