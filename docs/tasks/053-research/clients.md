# 053. Реакции и теги «Избранного» в официальных клиентах

Исследование: что сервер должен делать, чтобы реакции и теги Saved Messages работали в четырёх клиентах так же, как с Telegram, и где клиенты закрывают теги за Premium.

## Источники

| Клиент | Дерево | Версия | Репозиторий Pluma |
|---|---|---|---|
| Android | `/Users/sckr.a/pluma-work/reference-src/android` (DrKLO/Telegram `aac1efcb83d6`) | 12.9.1 | `~/pluma-android`, `upstream.env` пинит тот же коммит |
| iOS | `/Users/sckr.a/pluma-work/reference-src/ios` (`6ad963e5b62d`) | release-12.9.2 | `~/pluma-work/pluma-ios-052`, `upstream.env` пинит тот же коммит |
| Desktop | `/Users/sckr.a/pluma-work/reference-src/tdesktop` (`a1e89e1f64f0`, «Version 7.0.9») | 7.0.9 | `upstream.env` форка Desktop локально не найден; соответствие пину не проверено (UNVERIFIED), версия совпадает с отчётом 052 |
| Web K | `/Users/sckr.a/pluma-work/reference-src/tweb` (`346844bf8c00`) | 2.2 (676) | `~/pluma-web`, `upstream.env` пинит тот же коммит |

Сокращения путей:
- Android: от `TMessagesProj/src/main/java/org/telegram/`. MC = `messenger/MessagesController.java`, MO = `messenger/MessageObject.java`, MS = `messenger/MessagesStorage.java`, MDC = `messenger/MediaDataController.java`, CA = `ui/ChatActivity.java`, RCL = `ui/Components/ReactionsContainerLayout.java`, STL = `ui/Components/SearchTagsList.java`.
- iOS: от `submodules/`. TC = `TelegramCore/Sources`, TU = `TelegramUI/Sources`, MR = `TC/State/MessageReactions.swift`, ASMU = `TC/State/AccountStateManagementUtils.swift`.
- Desktop: от `Telegram/SourceFiles/`. DMR = `data/data_message_reactions.cpp`.
- Web K: от `src/`. ARM = `lib/appManagers/appReactionsManager.ts`, AMM = `lib/appManagers/appMessagesManager.ts`.

Сервер:
- Схема слоя 228: `pkg/catalog/tl/api.tl` (`// LAYER 228`, строка 3087).
  - `messages.sendReaction#d30d78d4` (2608), `getMessagesReactions` (2609), `getMessageReactionsList` (2610), `setChatAvailableReactions` (2611), `setDefaultReaction` (2613), `getUnreadReactions` (2615), `readReactions` (2616), `getSavedReactionTags` (2654), `updateSavedReactionTag` (2655).
  - `messages.search` с `saved_reaction:flags.3` и `saved_peer_id:flags.2` (2463).
  - `updateMessageReactions#1e297bfa` (415), `updateSavedReactionTags` (441).
  - `messageReactions` (1530), `reactionCount` (1528), `messagePeerReaction` (1539), `chatReactions*` (1629-1631), `savedReactionTag` и `messages.savedReactionTags` (1829-1832).
  - `dialog.unread_reactions_count` (216), `config.reactions_default:flags.15` (510).
- Текущее покрытие (`docs/project/api-coverage.md`):
  - Есть: `getAvailableReactions`, `getTopReactions`, `getRecentReactions`, `getDefaultTagReactions`.
  - Нет: `sendReaction`, `getMessagesReactions`, `getMessageReactionsList`, `getUnreadReactions`, `readReactions`, `setDefaultReaction`, `setChatAvailableReactions`, `clearRecentReactions`, `getSavedReactionTags`, `updateSavedReactionTag`.
  - Нет также `deleteParticipantReaction(s)`, `reportReaction`, `sendPaidReaction`.
- appConfig сервера: `teamgramd/bin/app_config.json`.
  - `premium_purchase_blocked: true` (строка 2).
  - `reactions_user_max_default: 1`, `reactions_user_max_premium: 1` (61-62).
  - `reactions_uniq_max` и `reactions_in_chat_max` не задаются.
- `help.getConfig` (`app/bff/configuration/internal/core/help.getConfig_handler.go`) не заполняет `reactions_default`: в коде сервера нет ни одного присваивания `ReactionsDefault`.

Утверждения о поведении официального сервера Telegram, не прочитанные в коде клиентов, помечены UNVERIFIED.

## Сводка для сервера

1. **`messages.sendReaction` возвращает `Updates`.** Ни один клиент не требует конкретного типа апдейта. Все применяют ответ общим конвейером, поэтому подходит `updateMessageReactions` (Web K переводит в него и `updateEditMessage`). Отвечать нужно полным объектом для реагирующего, с `min=false` и `chosen_order`. Остальным участникам — `min=true` без `chosen_order`, но с их собственным `my` (см. п. 2.3).
2. **`messageReactions` в каждом объекте `message`.** Desktop стирает реакции, если в отредактированном сообщении поля нет (`history/history_item.cpp:2240-2241`, 2439, 2451). Desktop же стирает реакции у опрошенного сообщения, для которого `getMessagesReactions` не вернул `updateMessageReactions` (DMR:1731-1736). Значит, `getMessagesReactions` возвращает апдейт на каждый запрошенный id, в том числе с пустым `results`.
3. **`updateMessageReactions` должен попадать в `other_updates` `getDifference`.**
   - Его применяют Android (MC:16729-16730), iOS (ASMU:708-737 → 1922), Desktop (`api/api_updates.cpp:610-621`) и Web K (`lib/appManagers/apiUpdatesManager.ts:347-361`).
   - В `getChannelDifference` iOS этот апдейт игнорирует: в `switch` по `otherUpdates` ветки для него нет, кроме общей обработки на ASMU:1922 (ASMU:3519-3599). Для каналов iOS восстанавливается опросом видимых сообщений и через реакции в отредактированных сообщениях.
4. **Опрос `getMessagesReactions`.**
   - Android опрашивает все типы чатов: каждые 15 с, видимый диапазон ±10 сообщений.
   - iOS: группы и каналы, раз в 20 с на сообщение.
   - Desktop: всё, кроме личных чатов, каждые 20 с, и только для сообщений, у которых уже были реакции.
   - Web K: только supergroup/channel, каждые 10 с.
   - Для личных чатов и обычных групп у Desktop и Web K остаются только push и `getDifference`.
5. **Непрочитанные реакции целиком на сервере.**
   - `messagePeerReaction.unread` ставится только на чужие реакции к сообщениям автора и только у автора. Android и iOS не проверяют, что сообщение исходящее (MO:413-422; `TC/SyncCore/SyncCore_ReactionsMessageAttribute.swift:446-452`); Web K проверяет (AMM:9814).
   - Сервер ведёт `dialog.unread_reactions_count` и отдаёт `getUnreadReactions` и `readReactions`.
   - «Прочитано» приходит не только через `readReactions`. iOS и Desktop отмечают просмотр через `messages.readMessageContents` / `channels.readMessageContents` (`TC/State/ManagedConsumePersonalMessagesActions.swift:324-325,368`; `apiwrap.cpp:1432-1483`). Web K шлёт и `readMessageContents`, и `readReactions` (AMM:8758-8821). Значит, `readMessageContents` на сервере обязан снимать флаг `unread` с реакций и уменьшать `unread_reactions_count`.
6. **Лимиты.**
   - Нужно поднять `reactions_user_max_premium` до 3 (у нас 1). Это значение по умолчанию у Android и Desktop: MC:3706-3738, DMR:118-119.
   - Нужно задать `reactions_uniq_max: 11` (значение по умолчанию у Android, Desktop и Web K) и `reactions_in_chat_max: 3` (Android).
   - Для не-Premium `reactions_user_max_default: 1`: новая реакция заменяет старую во всех четырёх клиентах.
   - Сервер должен проверять те же лимиты: `REACTIONS_TOO_MANY`, `REACTION_INVALID`, `PREMIUM_ACCOUNT_REQUIRED` для custom emoji (UNVERIFIED: коды ошибок официального сервера из клиентов не выводятся, кроме `REACTION_INVALID` и `REACTIONS_TOO_MANY`).
7. **Теги в Saved Messages.**
   - У сообщения в чате с самим собой `reactions_as_tags=true`. Нужны `getSavedReactionTags` (глобально и с `peer` = подчат), `updateSavedReactionTag`, `messages.search` с `saved_reaction` и `updateSavedReactionTags` при изменении.
   - Серверного флага, снимающего Premium-гейт тегов, нет ни в одном клиенте. Единственный рычаг — `user.premium=true` для себя, а он открывает всё Premium. Значит, нужны патчи клиентов (п. 4).
   - `premium_purchase_blocked: true` у Android полностью скрывает селектор тегов (CA:31902). У Desktop из-за этого ключа нет custom-emoji тегов: `premiumPossible` зависит от `premiumCanBuy` (`main/main_session.cpp:280,349-351`; DMR:234-246).

## 1. Какие методы вызывают клиенты

### 1.1 `messages.sendReaction`

| | Android | iOS | Desktop | Web K |
|---|---|---|---|---|
| Где | `messenger/SendMessagesHelper.java:3748-3800` | MR:327 (`requestUpdateMessageReaction`), очередь pending-действий MR:112-166, 573 | DMR:1506-1535 (`Reactions::send`) | ARM:672-997 |
| `big` | да, `flags\|=2` (:3788-3790) | да, бит 1 (MR:362-371) | никогда (`f_big` в коде нет) | никогда (:978-980) |
| `add_to_recent` | передаётся как есть (:3768-3771) | `true` из контекстного меню (`TU/Chat/ChatControllerOpenMessageContextMenu.swift:626`), `false` при нажатии на кнопку реакции (`TU/ChatController.swift:2177`) | `true` только из селектора (`history/history_item.cpp:3409`), `remove()` шлёт `false` (DMR:2057) | никогда |
| Порядок списка | порядок `results`, фильтр `chosen` (MO:792-804) | по `chosenOrder`, хвост `suffix(maxCount)` (MR:158-164) | порядок выбора (DMR:2009-2011) | от старой к новой (ARM:977-981) |
| Снять все | пустой список, бит 0 снят | `flags=0`, `reaction=nil` (MR:373) | пустой список без `f_reaction` (DMR:1517) | пустой список |
| Ответ | `processUpdates` (:3792-3797) | сохраняет локально слитый атрибут, затем `addUpdates(result)` (MR:385-397) | `applyUpdates` (DMR:1531); пока запрос летит, входящие данные о реакциях игнорируются (2131-2134, 2192-2195) | `updateEditMessage` / `updateEditChannelMessage` переводятся в `updateMessageReactions` (ARM:938-949) |
| Ошибки | не обрабатываются | любая ошибка снимает pending-атрибут, возврат к серверному состоянию (MR:692-710) | только снимает pending (DMR:1532-1533) | только `REACTION_INVALID` (ARM:981-989) |

Особенности:
- **Android.** Для сообщения из треда с `fwd_from` реакция отправляется на исходный чат и `saved_from_msg_id` (`SendMessagesHelper.java:3761-3767`). Серверу нужна корректная `fwd_from.saved_from_*` в обсуждениях.
- **iOS.**
  - В альбоме реакция ставится только на сообщение с наименьшим id (MR:46-54).
  - Одновременно идёт одна операция на чат (MR:507-516).
- **Web K.** Перед RPC применяет локальный `updateMessageReactions{local:true}` (ARM:923-929).

### 1.2 Остальные методы

| Метод | Android | iOS | Desktop | Web K |
|---|---|---|---|---|
| getMessagesReactions | опрос: `messenger/ChatMessagesMetadataController.java:125-150`, вызов из CA:16178; ±10 от видимого диапазона, раз в 15 с на сообщение, все типы чатов; в ответе `updateUnreadState=false` (:135-143) | `TC/State/AccountViewTracker.swift:843-865`, раз в 20 с (:852), только CloudGroup/CloudChannel (`TU/ChatHistoryListNode.swift:3172-3174`); применяются только `updateMessageReactions` из ответа (AccountViewTracker:870-916) | DMR:1537-1560, 1721-1754; из отрисовки видимых (`history/history_inner_widget.cpp:1562`); не для личных чатов и не для сообщений без реакций (1541-1543); каждые 20 с (строка 50) | `components/chat/bubbles.ts:6378-6419`, только `isChannel`, все отрисованные, каждые 10 с |
| getMessageReactionsList | `ui/Components/ReactedUsersListView.java:267-276` | MR:902; `TC/TelegramEngine/Messages/MessageReadStats.swift:62` (limit 1) | `history/view/reactions/history_view_reactions_list.cpp:440-458`; `api/api_who_reacted.cpp:373-386` | ARM:490-509 |
| getUnreadReactions | MC:821-828, `limit=1`, `add_offset=count-1`, `saved_peer_id` только для монофорумов; без `top_msg_id` | `TC/State/Holes.swift:725-740` (дыры тега `.unseenReaction`, с `top_msg_id` / `saved_peer_id`) | `api/api_unread_things.cpp:152-187`: первая страница offset 1, add_offset -10, limit 10; далее limit 100 | AMM:8645-8727: add_offset -50, limit 50, `top_msg_id` |
| readReactions | MC:21467-21495, когда счётчик дошёл до 0 (CA:24068-24079) | «прочитать все» (`TC/State/ManagedSynchronizeMarkAllUnseenPersonalMessagesOperations.swift:293-330`), повтор пока `offset != 0` | только из меню «прочитать все реакции» (`menu/menu_send.cpp:1004-1022`), повтор пока `offset > 0` | вместе с `readMessageContents` (AMM:8758-8821) и в `readMentions` (8826-8860) |
| setDefaultReaction | не вызывается: double-tap реакция хранится локально (MDC:8512-8515) | MR:1080 | DMR:524-530 | ARM:655-670; быстрая реакция берётся из `help.getConfig.reactions_default` (ARM:440-451) |
| clearRecentReactions | MDC:9566 | `TC/TelegramEngine/Stickers/TelegramEngineStickers.swift:255` | не вызывается | не вызывается |
| getAvailableReactions | MDC:778-789, hash, раз в час | `TC/State/AvailableReactions.swift:411` | DMR:1031 | ARM:169-198, hash 0 |
| getTopReactions | MDC:9598-9645, limit 100 | `TC/State/ManagedRecentStickers.swift:599`, limit 32 | DMR:990, limit 14 | ARM:302-320, limit 75 |
| getRecentReactions | там же, limit 50 | там же :538, limit 100 | DMR:1011, limit 40 | там же, limit 75 |
| getDefaultTagReactions | MDC:9663 | `ManagedRecentStickers.swift:660` | DMR:1096 | ARM:302-320 |
| getSavedReactionTags | MC:23970-24030 | `TC/State/SavedMessageTags.swift:158,206-213` (раз в час), по подчату `ManagedConsumePersonalMessagesActions.swift:642-756` | DMR:1072-1088 | при `user_auth` для всех, без проверки Premium (ARM:118, 1044-1074) |
| setChatAvailableReactions | MC:23039-23097: None/All/Some, `reactions_limit`, `paid_enabled` | MR:1012-1074; `CHAT_NOT_MODIFIED` считается успехом | `boxes/peers/edit_peer_reactions.cpp:1001-1035`; `paid_enabled` только для каналов | `lib/appManagers/appChatsManager.ts:1053-1063`, без `reactions_limit` |

Что серверу нужно из таблицы:
- `getMessagesReactions` нужен для всех типов чатов: его опрашивает Android.
- Desktop требует ответ на каждый id (см. сводку, п. 2).
- `setDefaultReaction` должен менять `reactions_default` в `help.getConfig`. Сейчас сервер его не отдаёт, поэтому у Web K нет быстрой реакции (UNVERIFIED: поведение без поля по коду ARM:440-451 не проверено до конца).

## 2. Применение `updateMessageReactions` и `messageReactions`

### 2.1 Какие поля читаются

| Поле | Android | iOS | Desktop | Web K |
|---|---|---|---|---|
| peer, msg_id | MC:19501-19519 → `MS.updateMessageReactions` (MS:5252-5322) | ASMU:1922-1931 | `api/api_updates.cpp:1757-1779` | AMM:9762-9838 |
| top_msg_id | топик для счётчика непрочитанных (MC:19510) | становится `threadId`, но при применении игнорируется (ASMU:5278) | игнорируется | id треда |
| saved_peer_id | только для админов монофорума (MC:19510); подчат Saved берётся из сохранённого сообщения (MS:5275) | `threadId` (ASMU:1924-1929) | игнорируется | только для монофорума (:9768) |
| min | см. 2.3 | см. 2.3 | см. 2.3 | не читается, объект заменяется целиком (:9835-9837) |
| can_see_list | заменяется серверным | бит 2 (`TC/ApiUtils/ReactionsMessageAttribute.swift:10-12`) | → CanViewReactions (`history/history_item.cpp:5073-5077`) | сохраняется, если есть; иначе выводится локально (ARM:747-749) |
| reactions_as_tags | режим тегов (MO:13069-13073) | бит 3; `areReactionsTags` (`TC/Utils/MessageUtils.swift:664-672`) | → ReactionsAreTags; пустые results в Saved тоже считаются тегами (`history_item.cpp:5067-5072`) | `components/chat/reactions.ts:149-160` |
| results.chosen_order | `chosen` | `chosenOrder` (:16-18) | `my` = есть `chosen_order`, сортировка по нему (DMR:2203-2245) | `chosen_order >= 0` |
| recent_reactions: my, unread, big | `unread` → счётчик (MO:413-422) | биты 0-2 (:30-32) | хранятся; `IsMyRecent` (DMR:122-144) | `unread` → счётчик (AMM:9791-9792) |
| top_reactors | заменяется серверным | разбирается (:61-73) | `IsMyTop`, при min сохраняется локальный «мой» (DMR:2309-2322) | — |

Неизвестное сообщение:
- Desktop запрашивает запись диалога (`requestDialogEntry`) и добавляет id в непрочитанные (`api_updates.cpp:1757-1779`).
- Web K перезагружает диалог (AMM:9774-9776, 8515-8519).
- Android при отсутствии строки в `reaction_mentions` перезапрашивает счётчик через `getPeerDialogs` / `getForumTopicsByID` / `getSavedDialogsByID` (MC:21216-21318).

### 2.2 Реакции внутри объекта `message`

- **Desktop.** `updateEditMessage` и `applyEdition` вызывают `updateReactions(message.reactions)`. Без поля реакции стираются (`history_item.cpp:2240-2241,2439,2451`). Флаг `useSameReactions` ставится только для локальных результатов отправки (`api/api_sending.cpp:725`).
- **Web K.** `updateEditMessage` с изменёнными реакциями превращается в `updateMessageReactions` (AMM:9940-9951).
- **iOS.** Входящее сообщение с непрочитанными реакциями получает тег `.unseenReaction` (`TC/ApiUtils/StoreMessage_Telegram.swift:24-38`). Путь редактирования (ASMU:4488-4580) не вызывает `withUpdatedResults`, то есть объект заменяется без слияния min (UNVERIFIED: прочитано не полностью).

Вывод для сервера: каждое отдаваемое сообщение (`getHistory`, `getMessages`, edit и т.д.) содержит актуальные `reactions` с персональными `chosen_order` и `my` для запрашивающего.

### 2.3 min=true и личное состояние

- **Android** (MO:3942-3967). При min сохраняет `chosen` только у первого совпавшего результата. `chosen_order`, `can_see_list` и `top_reactors` берёт от сервера.
- **iOS** (`ReactionsMessageAttribute.swift:44-58`). Переносит локальный `chosenOrder`, но из-за `break` на :49 только у первой выбранной реакции.
- **Desktop** (DMR:2216). При min сохраняет локальный `my`. Если у сообщения есть непрочитанная реакция, данные с min вообще не применяются: сообщение помечается «неизвестным» и уходит в опрос (`history_item.cpp:5087-5092`).
- **Web K.** Слияния нет: объект заменяется целиком.

Вывод: несколько реакций с `min=true` и без `chosen_order` теряют часть «моих» в iOS и Android, а в Web K — все. Поэтому серверу надёжнее всегда отдавать каждому пользователю персональный объект (`min=false`, его `chosen_order` и `my`). Для каналов это значит рассылку по подписчикам, а не один общий апдейт. Как поступает официальный сервер в больших каналах — UNVERIFIED.

### 2.4 Непрочитанные реакции: бейдж и кнопка перехода

| | Android | iOS | Desktop | Web K |
|---|---|---|---|---|
| Источник счётчика | `dialog.unread_reactions_count` или `topic.unread_reactions_count` (CA:8802-8809); таблицы `reaction_mentions(_topics)` (MS:675,716) | `.unseenReaction` tag summary из `dialog.unread_reactions_count` (ASMU:3065,3314,3697; `TC/State/FetchChatList.swift:83-89`); перезапрос через getPeerDialogs (`ManagedConsumePersonalMessagesActions.swift:454-486`) | `history/history.cpp:3439`, топики `data/data_forum_topic.cpp:432`, монофорум `data/data_saved_sublist.cpp:788` | `dialog.unread_reactions_count` (AMM:8540-8573) |
| Бейдж в списке чатов | от счётчика диалога | `TC/TelegramEngine/Messages/ChatList.swift:567-573` | от счётчика | `lib/appDialogsManager.ts:2665`, скрыт для Saved |
| Кнопка перехода | CA:46339-46348 → `getNextReactionMention` (локально, затем `getUnreadReactions`) | `TU/Chat/ChatControllerLoadDisplayNode.swift:5102-5125`, переход через `earliestUnseenPersonalReactionMessage` (`TC/TelegramEngine/Messages/EarliestUnseenPersonalMentionMessage.swift:79`) | `history/view/history_view_corner_buttons.cpp:289-299,150` | `components/chat/input.ts:887-888,2154-2157`, только для `chat.type === Chat` |
| Отметка просмотра | локально (MS:18169-18215); `readReactions` на весь диалог, когда счётчик стал 0 | `readMessageContents` / `channels.readMessageContents` (`ManagedConsumePersonalMessagesActions.swift:324-325,368`) | `readMessageContents` / `channels.readMessageContents` (`apiwrap.cpp:1432-1483`) | `readMessageContents` и `readReactions` (AMM:8758-8821) |
| Только исходящие | не проверяет | не проверяет | — | `pFlags.out` (AMM:9814) |

Desktop не ведёт непрочитанные реакции в broadcast-каналах (`api/api_unread_things.cpp:40-43`).

Что серверу нужно:
- (а) `unread` у `messagePeerReaction` только в копии для автора сообщения.
- (б) `dialog.unread_reactions_count` и такой же счётчик топика.
- (в) `readMessageContents` и `channels.readMessageContents` снимают `unread` с реакций на этих сообщениях. iOS и Desktop при обычном просмотре вызывают только их.
- (г) `readReactions` возвращает `messages.affectedHistory` с pts и `offset` (0, когда всё прочитано).
- (д) `getUnreadReactions` отдаёт сообщения с непрочитанными реакциями по обычной семантике offset_id / add_offset / limit / max_id / min_id.

### 2.5 Восстановление после переподключения

- `getDifference.other_updates`: применяют все четыре (ссылки в сводке, п. 3). Android применяет и через `getChannelDifference` (MC:17014), Desktop тоже (`api_updates.cpp:453-459`), Web K тоже (`apiUpdatesManager.ts:464-465`). iOS в `getChannelDifference` апдейт игнорирует (ASMU:3519-3599).
- Опрос видимых сообщений (см. 1.2) дополняет difference, но у Android он не трогает счётчик непрочитанных (`ChatMessagesMetadataController.java:135-143`).
- Вывод: `updateMessageReactions` получает pts и пишется в общий журнал апдейтов пользователя (для каналов — в журнал канала) так, чтобы попадать в difference. Иначе в личных чатах и обычных группах Desktop и Web K не узнают о пропущенных реакциях до открытия сообщения. Получает ли `updateMessageReactions` pts у официального сервера — UNVERIFIED: в TL у апдейта нет поля pts.

Тут нужно решение: апдейт без pts не может быть «единицей» в журнале. Вариант — класть его в `other_updates` difference как последнее состояние реакций для сообщений, изменившихся после клиентского pts, и одновременно хранить реакции в сообщении, чтобы `getHistory` отдавал актуальное. Окончательный выбор — за ADR задачи.

## 3. Лимиты и конфигурация

| Ключ / поле | Android | iOS | Desktop | Web K |
|---|---|---|---|---|
| `reactions_user_max_default` / `_premium` | MC:3706-3738, по умолчанию 1 / 3; `getMaxUserReactionsCount` (MC:908-910) | `TC/State/UserLimitsConfiguration.swift:137-173`; для не-Premium жёстко 1 (MR:65-73), ключ влияет только на подсказку (`TU/ChatController.swift:2150`) | DMR:114-120, по умолчанию 1 / 3 | `lib/appManagers/apiManagerMethods.ts:375,390-394` |
| `reactions_in_chat_max` | читается, по умолчанию 3; `getChatReactionsCount` без вызовов (MC:912-914) | не используется | не используется | объявлен, не читается (`appConfig.d.ts:64`) |
| `reactions_uniq_max` | по умолчанию 11 (MC:1775); ошибка разбора: проверка `TL_jsonBool`, затем приведение к `TL_jsonNumber` (MC:4483-4492), поэтому число никогда не применяется | не используется; в `TopMessageReactions.swift:31-36` жёстко 11 | `data/data_peer_values.cpp:87`, по умолчанию 11 | `lib/appManagers/utils/reactions/filterReactionsAtUniqCap.ts:5`, по умолчанию 11 |
| `chatFull.reactions_limit` | MC:916-922 (флаг 1048576 у chatFull, flags2 8192 у channelFull) | `TC/TelegramEngine/Peers/UpdateCachedPeerData.swift:546,629,697,906` | `data/data_chat.cpp:524-531`, `data/data_channel.cpp:1509-1520` | не читается |
| `available_reactions` | RCL:1086-1110: None — меню нет; All — custom emoji не в broadcast; Some — список | `TopMessageReactions.swift:12-79` | `data/data_peer.cpp:185-213` (All без `allow_custom` = Default) | ARM:207-282; для групп без поля — None (:239-241) |
| `help.getConfig.reactions_default` | — | — | — | ARM:440-451 |

Правила, которые клиенты применяют сами:
- **Не-Premium, лимит 1:** новая реакция заменяет старую (MO:12362-12376; MR:158-164; DMR:1966-1995; ARM:767).
- **Custom emoji** требуют Premium везде, кроме broadcast-каналов: Desktop `window/section_widget.cpp:630-640`; iOS `ChatControllerOpenMessageContextMenu.swift:535-543`; Android RCL:1093-1098.

Что отдавать серверу:
- appConfig:
  - `reactions_user_max_default: 1`, `reactions_user_max_premium: 3`, `reactions_uniq_max: 11`, `reactions_in_chat_max: 3`.
  - Это значения по умолчанию в коде клиентов. Что их отдаёт и сам Telegram — UNVERIFIED.
- `chatFull` / `channelFull`:
  - `available_reactions` всегда.
  - Для новых групп `chatReactionsAll` (Web K и Android при отсутствии поля показывают None).
  - Как Telegram выставляет значение по умолчанию при создании группы — UNVERIFIED.
  - `reactions_limit` — по настройке.
- `help.getConfig.reactions_default`: `reactionEmoji` (обычно 👍; UNVERIFIED), затем то, что поставил `setDefaultReaction`.
- Сервер сам проверяет:
  - per-user лимит (по Premium);
  - уникальный лимит;
  - список разрешённых реакций;
  - custom emoji только для Premium (кроме каналов);
  - право `send_reactions` в группах (Android CA:32659; Desktop `history_item.cpp:3361`).

## 4. Теги Saved Messages

### 4.1 Протокол

- **Признак тега.** Сообщения в чате с самим собой несут `messageReactions.reactions_as_tags=true`. Все клиенты по этому флагу (или по пустым `results` в self-чате) включают режим тегов: Android MO:13069-13073, MO:12302-12306; iOS `MessageUtils.swift:664-672`; Desktop `history_item.cpp:5067-5072`; Web K `reactions.ts:149-160`. В self-чате `my` у recent всегда истинно (Desktop DMR:130-131).
- **`getSavedReactionTags {peer?, hash}`** возвращает `savedReactionTag{reaction, title?, count}`.
  - Без `peer` — по всем Saved.
  - С `peer` — по подчату (Desktop шлёт `inputPeerEmpty` для «всех», DMR:1072-1088).
  - Нужен hash и `messages.savedReactionTagsNotModified`.
- **`updateSavedReactionTag {reaction, title?}`** возвращает Bool. Без title заголовок снимается (Desktop DMR:577-599). Web K ограничивает заголовок 12 символами (`components/chat/contextMenu.ts:777-799`).
- **`messages.search`** с `saved_reaction` (и `saved_peer_id` для подчата) — фильтр по тегу: Android MDC:3940-3948, 4033, 4175; iOS `TC/State/Holes.swift:930-958`; Desktop `api/api_messages_search.cpp:114-128`; Web K AMM:12379-12419. Пустой `q` и `inputMessagesFilterEmpty` допустимы.
- **`updateSavedReactionTags`** вызывает перезапрос у Android, Desktop и Web K:
  - Android MC:20232-20233. Ветка там мёртвая из-за дублирующегося `updateRecentReactions` на MC:20228-20231, поэтому фактически перезапроса нет.
  - Desktop `api_updates.cpp:2770-2772`.
  - Web K ARM:121-134.
  - iOS апдейт не обрабатывает и опрашивает раз в час.
  - Отправлять его после каждого изменения состава или заголовков тегов.
- **Сколько тегов на сообщение.** Лимит тот же `reactions_user_max_*`; у не-Premium один. В Telegram, по-видимому, теги только для Premium, а у Premium до `reactions_user_max_premium` (UNVERIFIED). iOS для не-Premium жёстко держит 1 (MR:65-73), значит после снятия гейта будет один тег на сообщение, если не патчить и это место.

### 4.2 Premium-гейты и минимальные патчи

Серверного сигнала нет. Ни один клиент не читает ключ appConfig для тегов. Все гейты смотрят на `user.premium` своего пользователя:
- Android `messenger/UserConfig.java:574-579`;
- iOS `TU/ChatController.swift:6348-6355`, MR:65;
- Desktop `main/main_session.cpp:345-347`;
- Web K `lib/appManagers/appUsersManager.ts:762-763`, `lib/rootScope.ts:291-295`.

`user.premium=true` открыло бы все Premium-функции, поэтому нужен патч. Во всех клиентах патч одинаковый по форме: одна функция «теги доступны» (`return true`) и её подстановка вместо проверки Premium только в местах тегов. Лимит реакций, custom emoji и прочие Premium-функции не меняются.

#### Android

| # | Место | Что закрывает |
|---|---|---|
| 1 | CA:32654-32657 | добавление и снятие реакции в Saved (показывает Premium-шторку) |
| 2 | CA:10324 | нажатие в мультиселекторе тегов |
| 3 | CA:44862 | нажатие на тег в пузыре (фильтр) |
| 4 | CA:31899 | текст подсказки над селектором |
| 5 | CA:31902 | `!tags \|\| !premiumFeaturesBlocked()`: при `premium_purchase_blocked` (у нас `true`) селектор тегов **скрыт** (MC:739-741, 2855) |
| 6 | CA:10252, CA:19217 | видимость действия и пункта меню «Тег» |
| 7 | STL:272 | фильтр по тегу в поиске |
| 8 | STL:303-306 | долгое нажатие — переименование |
| 9 | STL:655 | баннер «закрыто» |
| 10 | RCL:1073 | весь набор реакций для тегов (иначе 16 тегов по умолчанию) |
| 11 | RCL:1120 | кнопка «развернуть» |
| 12 | RCL:1390 | недавние и стандартные в списке |
| 13 | RCL:1964 | замок на каждом теге |
| 14 | `ui/Components/SharedMediaLayout.java:3787` | значок поиска по тегам |

Патч:
- В MessagesController добавить `savedTagsAvailable()`.
- В пп. 1-3 и 6-14 заменить `getUserConfig().isPremium()` на `savedTagsAvailable()`.
- В п. 5 условие становится `(!tags || savedTagsAvailable() || !premiumFeaturesBlocked())`.
- Лимит тегов на сообщение — `getMaxUserReactionsCount()` (MO:12341); при желании для self-чата брать `_premium`.

#### iOS

| # | Место | Что закрывает |
|---|---|---|
| a | `TU/ChatController.swift:1725-1732` | нажатие на тег (добавить/снять) → paywall |
| b | `TU/ChatControllerOpenMessageReactionContextMenu.swift:39-43` (paywall `.messageTags` на :25-36) | долгое нажатие на тег (переименовать / фильтр / снять) |
| c | `TU/Chat/ChatControllerOpenMessageContextMenu.swift:134-141` | полоса тегов в контекстном меню заблокирована («NonPremiumTagsTitle»); `reactionsLocked` → paywall (:342-351; `ReactionSelectionNode/Sources/ReactionContextNode.swift:1697,1721,3094,3285`) |
| d | `ChatControllerOpenMessageContextMenu.swift:527-532` | выбор тега вне топ-списка |
| e | `TU/ChatSearchTitleAccessoryPanelNode.swift:583-610` | промо «Unlock / Add tags» в панели фильтра |
| f | там же :639-644 | фильтр по тегу |
| g | там же :669-672 | переименование из панели |
| h | там же :706 | замок на тегах |
| i | `TU/ChatInterfaceTitlePanelNodes.swift:25` | панель тегов показывается не-Premium даже без тегов |
| j | там же :34-37 | в embedded-режиме панель скрыта для не-Premium |

Патч:
- В `ChatPresentationInterfaceState` добавить `canUseSavedTags` (`true`).
- В a-h и j подставить его вместо `isPremium`; в i условие — `hasSearchTags`.
- В MR:65-73 для сообщений Saved брать `maxReactionsPerMessage` вместо жёсткой 1. Иначе разрешён один тег на сообщение.

#### Desktop

| # | Место | Что закрывает |
|---|---|---|
| a | `window/section_widget.cpp:621-626` (`ShowReactPremiumError`); вызовы `history/history_inner_widget.cpp:559,2741`, `history/view/history_view_element.cpp:2738`, `history/view/history_view_list_widget.cpp:201` | добавление и снятие тега через селектор, быструю реакцию, клик |
| b | `history/view/history_view_element.cpp:2715-2723` | клик по тегу на сообщении (поиск по тегу или промо) |
| c | `dialogs/dialogs_search_tags.cpp:92-99,125-128,162-173` | фильтр по тегу и правое меню с «Переименовать»; не-Premium видит промо-чип |
| d | `history/view/reactions/history_view_reactions_selector.cpp:1535-1547` | только текст |
| e | DMR:234-246 | custom-emoji теги (`premiumPossible`, который у нас ложен из-за `premium_purchase_blocked`) |
| f | `core/local_url_handlers.cpp:903-911` | промо-окно по ссылке `internal:about_tags` |

Переименование (`history/view/history_view_context_menu.cpp:2253-2265`, `ShowTagInListMenu` :2334) своей проверки не имеет: оно доступно только через (c).

Патч:
- (a) В `section_widget.cpp:622` для тегов вернуть `false` без проверки `premium()`.
- (b) В `history_view_element.cpp:2716` подставить функцию «теги доступны».
- (c) В `dialogs_search_tags.cpp:94` заменить `Data::AmPremiumValue(...)` на `rpl::single(true)`.
- (d, e) по желанию.
- Лимит на сообщение (`SentReactionsLimit`, DMR:114-120) по-прежнему зависит от Premium, значит один тег у не-Premium.

#### Web K

| # | Место | Что закрывает |
|---|---|---|
| a | `components/chat/contextMenu.ts:465-468` | правый клик по тегу → `PopupPremium('saved_tags')` вместо меню «фильтр / переименовать» (:765-799) |
| b | `components/chat/contextMenu.ts:1809-1812` | добавление тега из полосы реакций контекстного меню |
| c | `components/chat/bubbles.ts:3650-3654` | клик по тегу на пузыре (фильтр / меню) |
| d | `components/chat/topbarSearch.tsx:951-960` | список тегов в поиске с `is-locked` и псевдо-тегом `premium_lock` |
| e | `components/chat/topbarSearch.tsx:1008-1011` | клик по тегу в поиске |
| f | `components/chat/reactionsMenu.ts:116-123` | текст «Reactions.Tag.PremiumHint» |

Быстрая реакция при наведении (`bubbles.ts:3189-3197`) и `chat.sendReaction` (`components/chat/chat.ts:1480-1576`) проверки Premium не содержат. Вероятно, через них тег ставится и без Premium (UNVERIFIED: условие показа на `bubbles.ts:3093-3120` не дочитано).

Патч:
- Добавить функцию `canUseSavedTags()` (`true`) и подставить её в a-f.
- Для нескольких тегов на сообщение на ARM:694 для self-чата брать Premium-лимит.

#### Тексты

Тексты промо («Unlock tags» и т.п.) после патча становятся недостижимыми. Новых пользовательских строк патч не вводит.

### 4.3 Решения, требующие подтверждения

- **Сколько тегов на сообщение у не-Premium.** Минимальный патч оставляет один (как лимит обычных реакций). Чтобы было как у Premium в Telegram (до `reactions_user_max_premium`), нужен дополнительный патч лимита в каждом клиенте (места перечислены выше) и та же логика на сервере: для self-чата лимит `_premium`.
- **Custom-emoji теги.** Они требуют Premium (Android RCL:1073, Desktop DMR:234-246). Минимальный патч их не открывает.
- **`premium_purchase_blocked`.** Если оставить `true`, у Android селектор тегов скрыт (п. 5 таблицы Android), и патч там обязателен в любом случае.

## 5. Различия по типам чатов

| Поведение | Личные | Обычные группы | Супергруппы | Каналы (broadcast) | Saved Messages |
|---|---|---|---|---|---|
| Разрешённые реакции | всегда All (Web K ARM:214-225; Android CA:30849-30852) | `chatFull.available_reactions` | `channelFull.available_reactions` | то же; `chatReactionsAll` = только стандартные, без custom (RCL:1093-1098; iOS `TopMessageReactions.swift:53-71`) | все (iOS `TopMessageReactions.swift:13-15`); теги |
| Опрос getMessagesReactions | только Android | Android, iOS, Desktop | все четыре | все четыре | Android; Desktop, если у сообщения есть реакции |
| `can_see_list` по умолчанию | да | да | да | нет (Web K ARM:747-749; Android CA:30822 — список «кто поставил» только в группах) | — |
| Право `send_reactions` | — | да (Android CA:32659; Desktop `history_item.cpp:3361`) | да | нет | — |
| Непрочитанные реакции | да | да | да | Desktop не ведёт (`api_unread_things.cpp:40-43`) | бейдж скрыт (Web K `appDialogsManager.ts:2665`) |
| Отметка просмотра | `messages.readMessageContents` | `messages.readMessageContents` | `channels.readMessageContents` | `channels.readMessageContents` | — |
| Custom emoji без Premium | нет | нет | нет | да | нет (см. 4.3) |
| Платные реакции | — | никогда (Desktop `data/data_chat.cpp:526`) | `paid_reactions_available` | `paid_reactions_available` | — |
| Send-as (анонимный админ) | — | — | реакции от имени `sendAsPeerId` (iOS MR:56-63); если send-as = сам канал, реакций нет (DMR:197-203) | — | — |

Ещё:
- **Топики форумов.** `top_msg_id` в `updateMessageReactions`, `getUnreadReactions`, `readReactions`. Счётчик топика тоже ведётся.
- **Монофорумы.** Для них используется `saved_peer_id` (Android MC:19510, 21480-21492; Desktop `api_unread_things.cpp:121,166-174`).
- **Подчаты Saved Messages.** Клиенты определяют подчат по сохранённому сообщению, а не по `saved_peer_id` апдейта (Android MS:5275). Счётчики тегов ведутся и по подчату, и по «всем» (Desktop DMR:536-538; Web K ARM:1133-1140).
- **Смена `available_reactions`.** Desktop снимает реакции у всех сообщений (если реакции выключены) или переопрашивает их (`history/history.cpp:4221-4231`). Серверу достаточно `updateChatParticipants`/`updateChannel` и свежего `chatFull` (UNVERIFIED: какой именно апдейт заставляет клиенты перечитать chatFull, здесь не проверялось).
- **Обсуждения.** Android для сообщения треда с `fwd_from` реагирует на оригинал в канале (`SendMessagesHelper.java:3761-3767`). Реакции поста канала и его копии в группе обсуждения — одна сущность (UNVERIFIED для других клиентов).
- **Double-tap.** В Saved Messages быстрый double-tap отключён на iOS (`TU/ChatController.swift:1716`).

## Найденные ошибки клиентов (сервер должен их обходить, не чинить)

- **Android, `reactions_uniq_max`** (MC:4483-4492). Число никогда не применяется, действует значение по умолчанию 11. Ключ стоит отдавать числом 11, чтобы не расходиться с ним; булево значение уронит клиент.
- **Android, `updateSavedReactionTags`** (MC:20228-20233). Мёртвая ветка, перезапроса тегов нет.
- **iOS и Android, `min=true`.** Сохраняется только одна «моя» реакция (см. 2.3). Отсюда требование отдавать персональный объект.
