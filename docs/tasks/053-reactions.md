# 053. Реакции на сообщения и теги в «Избранном» без Premium

- Статус: in progress (2026-11-03)
- Зависит от: —
- ADR: 0020 (`seq`), 0021 (inbox-события), 0019 (свёртка разницы). Исследование клиентов: `docs/tasks/053-research/clients.md` (Android `aac1efcb`, iOS `6ad963e5`, Web K `346844bf`, Desktop `a1e89e1`; строки по каждому утверждению).

## Цель
Реакции в личных чатах и группах работают так же, как в Telegram, во всех четырёх клиентах: поставить, снять, несколько своих, счётчики под сообщением, «кто отреагировал», значок непрочитанных реакций в списке чатов и переход к ним, реакции по умолчанию, разрешённые реакции группы, доставка на все устройства и после переподключения. Теги реакций в «Избранном» — у всех, без Premium. В каналах реакции — в задаче 044.

## Решения владельца (2026-11-03)
- Теги в «Избранном» делаются в этой задаче и доступны всем: «снимаем необходимость наличия премиум для данной фичи».
- Лимит — как с Premium: 3 реакции одного человека на сообщение и 3 тега на сообщение в «Избранном».
- Реакции в каналах — 044; платные реакции (звёзды) не делаются (ADR 0016).

## Что известно
- **Сервер сегодня.** Каталог есть: `getAvailableReactions`, `getTopReactions`, `getRecentReactions`, `getDefaultTagReactions`, настройки уведомлений о реакциях. Нет: `sendReaction`, `getMessagesReactions`, `getMessageReactionsList`, `getUnreadReactions`, `readReactions`, `setDefaultReaction`, `setChatAvailableReactions`, `clearRecentReactions`, `getSavedReactionTags`, `updateSavedReactionTag` (`docs/project/api-coverage.md`).
- **Telegram** (Web K `invokeApi`, эталонный аккаунт, 2026-11-03): `reactions_user_max_default = 1`, `reactions_user_max_premium = 3`, `reactions_uniq_max = 11`, `reactions_in_chat_max = 100`. Эталонный аккаунт с Premium: поведение без Premium — по исходникам клиентов.
- **Клиенты** (`053-research/clients.md`):
  - ответ на `sendReaction` — `Updates` с `updateMessageReactions`, отдельного типа клиенты не требуют;
  - каждому пользователю — свой объект `messageReactions` (`min = false`, его `chosen_order`, флаги `my`); с `min = true` iOS и Android оставляют одну «свою» реакцию, Web K заменяет объект целиком;
  - реакции — в каждом объекте сообщения: Desktop стирает их у правки без поля и у id, на который `getMessagesReactions` не ответил, поэтому метод отвечает обновлением на каждый запрошенный id;
  - после переподключения `updateMessageReactions` нужен в `other_updates` `getDifference` — его применяют все четыре клиента;
  - клиенты опрашивают `getMessagesReactions` видимых сообщений (Android все чаты раз в 15 с, iOS группы и каналы раз в 20 с, Desktop всё, кроме личных, раз в 20 с, Web K только супергруппы и каналы раз в 10 с);
  - непрочитанные реакции целиком на сервере: флаг `unread` — только в копии автора; iOS и Desktop отмечают их прочитанными через `messages.readMessageContents`, не `readReactions`, поэтому `readMessageContents` тоже снимает флаг и уменьшает `dialog.unread_reactions_count`;
  - сейчас сервер отдаёт `reactions_user_max_premium = 1`, а клиенты по умолчанию ждут 3; `help.getConfig` не задаёт `reactions_default` (Web K берёт из него быструю реакцию);
  - новые группы без `available_reactions` Web K и Android считают «реакции выключены», поэтому по умолчанию — `chatReactionsAll`.
- **Теги и Premium.** Ни одного серверного признака, который снимает ограничение тегов, клиенты не читают: везде проверяется `user.premium`, а он открыл бы все функции Premium. `premium_purchase_blocked = true` (мы его отдаём) на Android совсем прячет выбор тегов (`ChatActivity.java:31902`). Нужен минимальный патч в каждом клиенте: одна функция «теги доступны», которая возвращает `true` и используется только в проверках тегов, плюс лимит 3 тегов (iOS зашивает 1, `MessageReactions.swift:65-73`). Места — в исследовании.

## Область
- **Хранение.** Реакции личных чатов и групп — одна запись на (сообщение диалога, пользователь, реакция) по общему id сообщения диалога, не по копиям в ящиках; порядок своих реакций (`chosen_order`), отметка «непрочитано» для автора, дата. Счётчик непрочитанных реакций диалога. Реакция по умолчанию пользователя, недавние реакции, теги «Избранного» с названиями. Миграция `migrate-*.sql`, повторный запуск безопасен.
- **Методы:** `sendReaction` (до 3 своих на сообщение, проверка разрешённых реакций группы, `add_to_recent`, `big`), `getMessagesReactions`, `getMessageReactionsList`, `getUnreadReactions`, `readReactions`, реакции в `readMessageContents`, `setDefaultReaction`, `setChatAvailableReactions`, `clearRecentReactions`, `getSavedReactionTags`, `updateSavedReactionTag`, поиск по тегу в `messages.search`/`getSavedHistory`.
- **Доставка.** `updateMessageReactions` своим устройствам и устройствам собеседника или участников группы, каждому в его виде; восстановление через `getDifference` по механизму `seq` (ADR 0020 и 0021: своё действие — `EmitSeqUpdate`, чужое — inbox-событие получателя). `updateSavedReactionTags` — своим устройствам.
- **Объекты сообщений.** `reactions` в каждом сообщении истории, разницы, ответов на отправку и правку, в виде зрителя.
- **Настройки:** `reactions_user_max_default = reactions_user_max_premium = 3`, `reactions_uniq_max = 11`, `reactions_in_chat_max = 100`, `reactions_default` в `help.getConfig`.
- **Клиенты:** патчи тегов без Premium и лимита 3 тегов в четырёх клиентах, каждый — строкой в реестре `clients/patches/*.md`.
- Не трогаем: каналы (044), платные реакции, реакции в историях.

## Устройство (решения 2026-11-03)
- **Хранение** — `message_reactions`: одна строка на (общий `dialog_message_id`, пользователь, реакция), `chosen_order`, `big`, `unread` (для автора), дата. `dialog_message_id` — snowflake, общий у всех копий сообщения в ящиках участников (`messages.dialog_message_id`), поэтому реакции личного чата и группы хранятся один раз. Старые колонки `messages.reaction*` (одна реакция на копию, апстрим) не используются. Счётчик `dialogs.unread_reactions_count` уже есть. Теги «Избранного» — реакции в чате с собой (`reactions_as_tags`); названия — `saved_reaction_tags (user_id, reaction, title)`. Реакция по умолчанию — настройка пользователя.
- **Запись** — новый RPC `msg.sendReaction` в транзакции ящика того, кто ставит реакцию: строки `message_reactions`, свой `updateMessageReactions` через `seq` (`EmitSeqUpdate`), inbox-событие `inbox.updateMessageReactionsToInbox(user_id, from_id, dialog_message_id, …)` каждому другому участнику. Получатель в своей транзакции находит свою копию по `dialog_message_id`, строит вид реакций для себя и журналирует `updateMessageReactions` со своим `msg_id` через `seq`. Автор сообщения получает отметку непрочитанного и `unread_reactions_count` в той же транзакции.
- **`seq` и свёртка** — `updateMessageReactions` добавляется в `userbox.SeqUpdate` (это полное состояние реакций сообщения, повтор безопасен), в `seqKey` — ключ «реакции: peer + msg_id» (последнее состояние, ADR 0020).
- **Вид для зрителя** — одна чистая функция: результаты по числу, `chosen_order` и `my` зрителя, `recent_reactions` (в личных и маленьких группах), `unread` — только автору, `can_see_list` в группах, `reactions_as_tags` в «Избранном», `min = false`.
- **Объекты сообщений** — реакции добавляются там, где сообщения из базы превращаются в объекты для клиента: `biz message` `MakeMessageBox` (история, `getMessages`, поиск) — пачкой на список; ответы на отправку и правку; `getDifference` получает текущее состояние через `updateMessageReactions` в `other_updates`.

## Ход работы
Шаги (отмечать по мере выполнения):
- [x] Миграция `migrate-20261104-reactions.sql`; `pkg/reactions` (ключи, лимиты, вид для зрителя) с тестами.
- [x] `seq`: `updateMessageReactions` и `updateSavedReactionTags` в `userbox.SeqUpdate`, ключи свёртки в `seq_fold.go`, `seqpeers.Pick`.
- [x] RPC `msg.sendReaction`, `msg.readReactions`; `inbox.updateMessageReactionsToInbox` (proto, кодек, клиенты, обработчики).
- [x] msg dao: строки реакций, счётчик автора, снятие `unread` в `readMessageContents` и `readReactions`.
- [x] biz message: реакции в каждом сообщении (`MakeMessageBox`, пачкой), `message.getMessagesReactions`, `getMessageReactionsList`, сообщения с непрочитанными реакциями; ответы правки.
- [x] bff (модуль `catalog`, он зарегистрирован за `RPCReactions` и `RPCSavedMessageTags`): `sendReaction`, `getMessagesReactions`, `getMessageReactionsList`, `getUnreadReactions`, `readReactions`, `setDefaultReaction`, `setChatAvailableReactions`, `clearRecentReactions`, `getSavedReactionTags`, `updateSavedReactionTag`, поиск по тегу.
- [x] Настройки: `app_config.json` (лимиты), `reactions_default` в `help.getConfig`.
- [x] e2e; проверки; клиентские патчи тегов (4 клиента, реестр).
- [ ] Выкатка; проверка в клиентах со скриншотами; подписанные сборки клиентов.

## Критерии приёмки
- Модульные тесты: вид `messageReactions` для зрителя (свои `chosen_order`, `my`, `unread` только автору, порядок результатов), лимиты, свёртка обновлений.
- Интеграционные тесты хранения на MySQL через `pkg/testinfra`.
- e2e через `pkg/mtclient`: реакция в личном чате и в группе доходит до собеседника и до второго устройства пушем и через `getDifference`; непрочитанная реакция видна автору в диалоге и снимается `readMessageContents`; `getMessagesReactions` отвечает на каждый id; лимит 3; тег с названием в «Избранном» и поиск по нему.
- В клиентах (iOS, Android, Desktop, Web K): поставить и снять реакцию, три реакции, счётчик и «кто отреагировал», значок непрочитанной реакции и переход к ней, тег в «Избранном» с названием и фильтр. Скриншоты в брифе, сравнение с настоящим Telegram на эталонных клиентах.
- `go run ./tools/apicoverage` перезапущен; обычные проверки.

## Результат
Реакции в личных чатах и базовых группах и теги «Избранного» работают на сервере. Лимиты как у Telegram Premium, для всех: 3 реакции и 3 тега на сообщение, 11 разных, 100 разрешённых в группе. Патчи тегов без Premium готовы во всех четырёх клиентах. Не выкачено, в клиентах ещё не проверено.

- **Сервер.**
  - Миграция `migrate-20261104-reactions.sql`: `message_reactions`, `saved_reaction_tags`, `user_reaction_settings`.
  - `pkg/reactions` — ключи, лимиты, вид `messageReactions` для зрителя; чтения (теги, поиск по тегам, недавние, реакция по умолчанию).
  - RPC `msg.sendReaction`, `msg.readReactions`, `msg.updateSavedReactionTag`, событие `inbox.updateMessageReactionsToInbox`, `message.getUnreadReactions`.
  - `updateMessageReactions` и `updateSavedReactionTags` — обновления с `seq` (свёртка: последнее состояние сообщения).
  - Реакции добавляются в каждое сообщение biz message и в обновления правки.
  - Методы bff в модуле `catalog` (он зарегистрирован за `RPCReactions` и `RPCSavedMessageTags`): `sendReaction`, `getMessagesReactions`, `getMessageReactionsList`, `getUnreadReactions`, `readReactions`, `setDefaultReaction`, `setChatAvailableReactions`, `clearRecentReactions`, `getRecentReactions` (теперь настоящий), `getSavedReactionTags`, `updateSavedReactionTag`.
  - Поиск по тегу в `messages.search`; `reactions_default` в `help.getConfig` (своя реакция или `❤`, как у Telegram); лимиты в `app_config.json` и `docs/ops/app-config.md`.
- **Клиенты** (ветки `task/053-saved-tags`, реестр `clients/patches/*.md`).
  - Android A-086…A-092: 14 гейтов, в том числе пропущенные исследованием. Лимит тегов — Premium-лимит. Лимит реакций клиент уже берёт с сервера.
  - iOS I-096…I-098: гейты тегов. iOS игнорировал `reactions_user_max_default` и всем держал 1 — пропатчено. Тост «несколько реакций с Premium» скрыт.
  - Desktop D-073: три гейта. Лимит 3 он уже берёт с сервера, исследование ошибалось.
  - Web K W-065: шесть гейтов, лимит с сервера.
  - Тесты патч-скриптов зелёные во всех форках.
- **Проверка.**
  - `go build ./...`, `go vet`, `make lint` (0 issues), `go test ./tools/...`, модульные тесты затронутых пакетов.
  - Интеграционные `pkg/reactions` на MySQL.
  - e2e на CachyOS: `TestReactionsPrivate` (вид у обеих сторон и второго устройства, пуш и `getDifference`, непрочитанные и `readMessageContents`, лимит 3, `getMessagesReactions`, снятие всех), `TestSavedReactionTags`.
  - e2e поймал настоящую ошибку: `mtproto.MakeFlagsInt32(0)` возвращает `nil`, и первая своя реакция теряла `chosen_order`. Модульный тест её пропускал, потому что `GetValue()` у `nil` даёт 0; исправлен.
  - Независимое ревью подтвердило четыре недочёта, все исправлены:
    - `readMessageContents` по своему голосовому с реакцией снимал «не прослушано»;
    - в живом пуше не было пользователей, вышедших из группы;
    - отрицательный `add_offset` в `getUnreadReactions` не был ограничен;
    - `getMessageReactionsList` прятал ошибки.
- **Решения** (технические, приняты):
  - `readReactions` ничего не журналирует (иначе голосовые автора помечались бы прослушанными);
  - недавние реакции — собственные реакции вне «Избранного» после очистки;
  - группа без настройки — «все стандартные»;
  - название тега до 12 символов (как Web K);
  - чтения тегов и поиска по тегам в bff читают `messages` вместе с `message_reactions`.
- **Не сделано:** реакции в каналах — 044; платные реакции — не делаются; `updateChatParticipants` при смене разрешённых реакций группы другим участникам не шлётся (клиенты перечитывают `chatFull`; не проверено).
