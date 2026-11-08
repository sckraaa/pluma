# 062. Опросы, викторины и чек-листы

- Статус: done (серверная часть; проверка в клиентах и клиентские патчи чек-листов — отдельно, см. «Результат»)
- Зависит от: —
- ADR: 0020 (`seq`), 0021 (inbox-события), 0034 (каналы), 0016 (нет Premium).

## Цель
Опросы и викторины со всеми флагами слоя 228 и чек-листы работают в личных чатах, группах и каналах: создание, голос, снятие голоса, закрытие (вручную и по времени), доставка всем участникам и устройствам, результаты для зрителя в истории и разнице, служебные сообщения. Параллельные голоса не портят счётчики, голос идемпотентен.

## Что известно (источники прочитаны)
- **Схема:** `third_party/teamgram-proto/mtproto` слой 228: `poll`, `pollAnswer`/`inputPollAnswer`, `pollResults` (`min`, `has_unread_votes`, `can_view_stats`, `solution_media`), `updateMessagePoll` (`peer`, `msg_id`, `top_msg_id`, `poll_id`, `poll`, `results`), `messages.sendVote`, `getPollResults(poll_hash)`, `getPollVotes`, `addPollAnswer`, `deletePollAnswer`, `getUnreadPollVotes`, `readPollVotes`, `dialog.unread_poll_votes_count`; чек-листы: `inputMediaTodo`, `messageMediaToDo`, `toggleTodoCompleted`, `appendTodoList`, `messageActionTodoCompletions`, `messageActionTodoAppendTasks`.
- **core.telegram.org** (прочитано 2026-11-08): `api/poll` (флаги, `updateMessagePoll` без `peer`/`msg_id` при рассылке подписчикам канала, `subscribers_only` 86400 с, `countries_iso2`, правила `open_answers`, `poll_answer_delete_period`, `readPollVotes`), `api/todo`, `api/config` (пределы), `method/messages.sendVote` (ошибки), `method/messages.getPollVotes`, `method/messages.getPollResults`, `method/messages.toggleTodoCompleted`, `method/messages.appendTodoList`, `method/messages.addPollAnswer`, `method/messages.deletePollAnswer`, `method/messages.sendMedia` (ошибки создания).
- **Клиенты** (`~/pluma-work/reference-src`):
  - min-результаты не трогают `chosen` у клиента: tdesktop `data_poll.cpp:187-300` (`applyResults`), Android `MessageObject.java:4040-4075` (`updatePollResults`), Web K `appPollsManager.ts` `mergeMinAnswerResults`/`saveResults`.
  - Тело результатов для скрытых опросов (`hide_results_until_close`) без цифр — Web K `saveResults`, Android там же; показ всем после закрытия и создателю всегда — tdesktop `history_view_poll.cpp` `Poll::showVotes`.
  - Опрос результатов: Android опрашивает видимые опросы (`getPollResults`) каждые 30 с и раз в секунду после `close_date` (`MessagesController.java:10637-10698`), tdesktop `checkResultsReload`, iOS `HistoryViewStateValidation`.
  - `poll_hash`: Android/Web K/tdesktop шлют `poll.hash`, ответ — обновление без `poll`, если хэш тот же.
  - Голосование iOS/tdesktop: ответ — `Updates` с `updateMessagePoll`, `applyUpdates` (`Polls.swift:37-119`, `api_polls.cpp sendVotes`). Закрытие — `messages.editMessage` с `poll.closed` (`Polls.swift` `_internal_requestClosePoll`, `api_polls.cpp` `close`).
  - Непрочитанные голоса: tdesktop читает их `readMessageContents` при показе (`history_inner_widget.cpp:1558`) и сбрасывает всё через `readPollVotes` (`menu_send.cpp:1050`), iOS `ManagedSynchronizeMarkAllUnseen`, Android `markPollVotesAsRead`, Web K `appMessagesManager.ts:8637`.
  - Чек-листы: создание только Premium: tdesktop `data_peer.cpp:711-718`, `window_peer_menu.cpp:2469`, Android `ChatAttachAlert.java:4524`, `ChatActivity.java:40285`, iOS/Web — см. проверку в задаче патчей. `toggleTodoCompleted`/`appendTodoList` — `Updates` (iOS `Todo.swift`, tdesktop `api_todo_lists.cpp`).

## Область
- Хранение: `polls` (постоянные данные опроса, правильные варианты и объяснение отдельно), `poll_votes` (по строке на выбор), `todo_lists` (состояние чек-листа для личных чатов/групп, ключ — `dialog_message_id`). Миграция `migrate-20261108-polls-checklists.sql`, повторный запуск безопасен.
- `pkg/polls`: чистая логика — проверка ввода, вид для зрителя (Full/Min), хэш, чек-листы; чтения (`Attach`, `AttachMessages`, `AttachUnreadDialogs`, `VotersPage`).
- msg: новые RPC `msg.sendVote`, `closePoll`, `addPollAnswer`, `deletePollAnswer`, `readPollVotes`, `toggleTodoCompleted`, `appendTodoList`, `editTodoList`; события `inbox.updatePollToInbox`, `inbox.updateTodoToInbox`; свипер закрытия опросов (`SweepDuePolls`, раз в 5 с); bff `RPCPolls` и `RPCTodoLists` в модуле `messages`, маршруты в трёх `session.yaml`.
- Чтение: опрос в каждом сообщении истории, `getMessages`, `getDifference`, `getChannelDifference` с результатами зрителя.
- `seq`: `updateMessagePoll` в журнале с `seq` (свёртка отдельно для min и личного представления).

## Устройство
- **Создание.** bff проверяет `inputMediaPoll` и даёт каждому ответу `option` (из `inputPollAnswer` их нет, позиция — как строка); правильные варианты и объяснение доезжают в `results`, msg кладёт их в `polls` и убирает из копий. Копии в `messages`/`channel_messages` хранят только определение и пустые результаты; `creator` и всё личное считается на чтении.
- **Форварды** разделяют один `poll_id` и одни результаты; результаты не копируются (`ForForward`). Голоса в пересланном опросе в другом чате дойдут до источника при опросе `getPollResults` (Android, tdesktop); пуш приходит только в чат, где голосовали. Отметка «создатель видит голос» — событием автору и в этом случае.
- **Голос** в транзакции ящика голосующего, строка `polls` — `LOCK IN SHARE MODE` (голоса разных людей не ждут друг друга), `FOR UPDATE` у закрытия и правок ответов. Голос пишется целиком (удалить старые, вставить новые) — счётчики считаются из строк, расхождений быть не может. Повтор того же голоса — без записей и событий.
- **Доставка.** Частные и группы: свои устройства — через `seq` (полный вид), остальным — inbox-событие; получатель читает текущее состояние и журналирует min (`has_unread_votes` и полный вид — только создателю). Канал: min-обновление без `peer`/`msg_id` пушем всем (как реакции каналов, `pts` не двигается), полный вид — другим устройствам голосующего; создателю публичного опроса — inbox-событие.
- **Закрытие.** `IsClosed` смотрит и на `close_date`: голос и чтение не ждут свипера. Свипер берёт просроченные, в транзакции создателя повторно проверяет `closed` под `FOR UPDATE` (несколько инстансов, повторные запуски) и шлёт обновление с `poll`.
- **Чек-листы.** Состояние в одном месте: `todo_lists FOR UPDATE` для личных чатов и групп, канальная строка под блокировкой канала. Изменение (без `edit_date`) пишется правкой копии и журналом `updateEditMessage`/`updateEditChannelMessage`; у других участников копию обновляет inbox-событие из текущего состояния, поэтому порядок событий не важен. Служебные сообщения идут как обычно (`reply_to` — на список).

## Решения, требующие подтверждения
- `messages.editMessage` с опросом только закрывает; иное — `MESSAGE_NOT_MODIFIED`. В документации упомянуто только закрытие.
- `getPollVotes` для анонимного опроса — `BROADCAST_FORBIDDEN` (документация называет его для каналов; других кодов для анонимных не описано). Для публичного: создатель, проголосовавший, или любой после закрытия; иначе `POLL_VOTE_REQUIRED`.
- Правильный ответ и объяснение видит тот, кто проголосовал, все после закрытия и создатель (создателю раскрыто — по поведению клиентов, `Poll::showVotes`; не проверено напрямую на Telegram).
- Викторина с `multiple_choice`: разрешена при нескольких правильных (iOS `.quiz(multipleAnswers:)`), хотя `sendMedia` перечисляет `QUIZ_MULTIPLE_INVALID`/`QUIZ_CORRECT_ANSWERS_TOO_MUCH`; для одиночной проверяется «один правильный». Клиенты слоя 228 иначе не собрать опрос.
- Чек-лист в вещательном канале сервер не запрещает (клиенты сами не дают создать: tdesktop `canCreateTodoLists`).

## Вопросы владельцу
- **Premium и чек-листы.** На сервере ограничения нет. Клиенты требуют Premium для создания/добавления (`window_peer_menu.cpp:2469,2557`, Android `ChatAttachAlert.java:4524`, `ChatActivity.java:40285`, и `TodoWantsPremium` в целом). По аналогии с задачей 053 (снятие Premium у тегов) нужны точечные патчи клиентов; это решение за владельцем.
- **Поиск.** `inputMessagesFilterPoll` в `messages.search` не поддержан: `GetMediaType` в `third_party` не имеет типа «опрос». Правка общего файла вне этой задачи.

## Критерии приёмки
- Модульные тесты `pkg/polls`; интеграция msg на MySQL/Kafka: параллельные голоса, повторы, запрет переголосования, закрытие, чек-лист с параллельными отметками.
- e2e: опрос в личном чате, викторина с объяснением и закрытием, параллельные голоса в супергруппе, закрытие по `close_date`, чек-лист в группе.
- `go run ./tools/apicoverage` без `missing` по `RPCPolls`/`RPCTodoLists`.

## Результат
Сделано на сервере; в клиентах не проверялось, клиентские патчи чек-листов не делались (вопрос владельцу).
- Миграция `migrate-20261108-polls-checklists.sql`: таблицы `polls`, `poll_votes`, `todo_lists`.
- `pkg/polls` — правила опросов и чек-листов, виды для зрителя и общий min, чтения.
- msg: RPC голоса, закрытия, ответов открытых опросов, чтения голосов, чек-листов; inbox-события; свипер `close_date`.
- bff `messages`: `RPCPolls` (все 7 методов) и `RPCTodoLists` (2 метода), закрытие через `editMessage`, опрос и чек-лист в `sendMedia`, форварды (`ForForward`, `QUIZ_ANSWER_MISSING`), `poll_option`/`todo_item_id` в заголовке ответа, маршруты в трёх `session.yaml`.
- Чтение: `polls.Attach` в сервисе message и истории канала, `getDifference`, `getChannelDifference`; `unread_poll_votes_count` в диалогах из `polls`.
- Пользователи, которых называет опрос или чек-лист (`recent_voters`, `added_by`, `completed_by`), попадают в `users` ответов: `PickByMessageMedia` (`third_party(proto)`) и `seqpeers`.
- Проверка на ветке: `go build ./...`, `go vet -tags integration,e2e -unreachable=false` и `go test` затронутых пакетов. Интеграционные тесты (`polls_integration_test.go`) и e2e (`test/e2e/polls_test.go`) написаны и компилируются; запускаются в общем прогоне объединённой партии вместе с `-race` и `make lint`, до него не считаются пройденными. Единственный запуск e2e на стенде упал на коде самого теста (вложенные объекты без конструктора), после исправления не повторялся. В живых клиентах не проверялось.
- Не сделано: `updateMessagePollVote` (только ботам по документации, ботов нет), `stats.getPollStats`, `inputMessagesFilterPoll`, уведомления о голосах в публичных опросах (`poll_votes_notify_from`), и живая проверка в клиентах.
