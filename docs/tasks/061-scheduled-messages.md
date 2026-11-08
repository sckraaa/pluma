# 061. Отложенные сообщения и «отправить, когда будет в сети»

- Статус: in progress (сервер готов; общий прогон e2e, -race и lint и проверка в клиентах впереди)
- Зависит от: 036, 042 (ящик канала, права `pkg/channelperm`)
- ADR: 0034 «Ящик канала». Новое решение ADR не потребовало: очередь и доставка описаны здесь.

## Цель
Пользователь откладывает сообщение на дату или до появления собеседника в сети, видит и правит очередь, отправляет её вручную; в свой срок сообщение уходит обычным путём отправки со всеми проверками. Работает в личных чатах, Избранном (напоминания), группах и каналах.

## Что известно
Документация, core.telegram.org:
- `api/scheduled-messages`: дата менее чем через 10 секунд отправляет сразу, обычным сообщением; `0x7FFFFFFE` только для личного чата, собеседник в сети получает сразу, иначе сообщение ждёт в очереди с этой датой; скрытый статус даёт `SCHEDULE_STATUS_PRIVATE`. При постановке `updateNewScheduledMessage` с id очереди и `date = schedule_date`; в срок `updateNewMessage` или `updateNewChannelMessage` с `from_scheduled` и `updateDeleteScheduledMessages`, где `messages` и `sent_messages` соответствуют по индексу. Правка даёт `updateNewScheduledMessage` с тем же id.
- `method/messages.sendMessage`: `SCHEDULE_BOT_NOT_ALLOWED`, `SCHEDULE_DATE_TOO_LATE`, `SCHEDULE_STATUS_PRIVATE`, `SCHEDULE_TOO_MUCH`. `method/messages.editMessage`: `SCHEDULE_DATE_INVALID`, `MESSAGE_ID_INVALID`. `method/messages.getScheduledHistory`: `CHAT_ADMIN_REQUIRED`, хеш по `id, edit_date (0), date`. `constructor/updateDeleteScheduledMessages`: `sent_messages` есть только при отправке.

Клиенты (`~/pluma-work/reference-src`):
- Значение `0x7FFFFFFE`: tdesktop `api/api_common.h:20` `kScheduledUntilOnlineTimestamp`; Android `ui/ChatActivity.java:20996`; iOS `TelegramCore/Sources/PendingMessages/EnqueueMessage.swift:963` (при собеседнике в сети отправляет как обычное).
- Предел даты: выбор дня до 365 вперёд, Android `ui/Components/AlertsCreator.java:4529`, iOS `ChatScheduleTimeControllerNode.swift`. Отсюда 366 суток на стороне сервера: последний выбираемый день должен проходить.
- Предел очереди 100: tdesktop `Resources/langs/lang.strings:319` `lng_error_schedule_limit`; Android `AlertsCreator.java:516` `SCHEDULE_TOO_MUCH`.
- Хеш очереди: tdesktop `data/components/scheduled_messages.cpp:651` `countListHash`, новые первыми.
- Напоминание в Избранном непрочитано, пока его не прочли: tdesktop `history/history_item.cpp:4126` `HistoryItem::unread`; Android `messenger/MessagesController.java:18619` `obj.messageOwner.from_scheduled`.
- Очередь канала общая для админов, чужой пост отличается по `out`: Android `MessageObject.canEditMessageScheduleTime` (`MessageObject.java:11586`).

Не подтверждено источниками, поведение выбрано по согласованности (см. решения в «Результате»): что Telegram делает с сообщением, которое больше нельзя отправить в срок; есть ли `from_scheduled` при ручной отправке; держит ли сервер «когда будет в сети» ограниченное время.

## Область
- `schedule_date` в `messages.sendMessage`, `sendMedia`, `sendMultiMedia`, `forwardMessages`; `messages.getScheduledHistory`, `getScheduledMessages`, `sendScheduledMessages`, `deleteScheduledMessages`; правка через `messages.editMessage` с `schedule_date`.
- `has_scheduled` в `userFull`, `chatFull`, `channelFull`.
- Воркер доставки: выдерживает перезапуски и несколько экземпляров, сообщение уходит один раз.
- Не входит: `schedule_repeat_period` (повтор, Premium), обработка видео перед публикацией (`video_processing_pending`).

## Критерии приёмки
- Модульные тесты: общая дата отправки, разбиение на альбомы, постоянные и временные ошибки, вид сообщения для зрителя, хеш, очередь канала, срок аренды.
- Интеграционные на MySQL, Redis, Kafka через `pkg/testinfra`: три воркера и доставка один раз, аренда упавшего воркера, пределы, чтение, правка и удаление, сброс при выходе отправителя, альбом одним пакетом, постраничное чтение ожидающих собеседника, напоминание.
- e2e через `pkg/mtclient`: личный чат с правкой и ручной отправкой, пределы дат, доставка в срок, «когда будет в сети», общая очередь канала.
- В клиентах, со скриншотами: iOS, Android, Desktop, Web K.

## Результат
Сервер готов: отложенные сообщения работают в личных чатах, Избранном, группах и каналах. Новая таблица `scheduled_messages` (миграция `migrate-20261108-scheduled-messages.sql`, сервис `migrate` применяет её сам, действий оператора нет), пять RPC `msg.*` (`getScheduledHistory`, `getScheduledMessages`, `sendScheduledMessages`, `deleteScheduledMessages`, `hasScheduled`), сервис `app/bff/scheduledmessages` с маршрутом во всех трёх `session.yaml`, ответ-заглушка `getScheduledMessages` удалён.

Как устроена доставка. Воркер (`scheduled_worker.go`) раз в секунду берёт наступившие строки, раз в 5 секунд ожидающих собеседника. Строка берётся условным `UPDATE` с арендой на 2 минуты и удаляется в той же транзакции ящика, что отправляет сообщение, поэтому при любом падении сообщение уходит один раз. Временная ошибка оставляет строку до конца аренды. После захвата строки читаются заново: правка, успевшая до захвата, учитывается, а перенос на другую дату возвращает строку в очередь. После захвата правка отклоняется (`MESSAGE_ID_INVALID`). Ожидающие собеседника читаются постранично по id, иначе самые старые заслоняли бы остальные. Отправка идёт обычным путём со всеми проверками на момент доставки.

Проверка. На ветке: `go build ./...`, `go vet -tags integration,e2e` и `go test` затронутых пакетов, зелёные. Новый `TestRescheduled` падает без проверки в воркере (проверено заменой условия). Ранее на этой же ветке e2e `TestScheduled*` прошли на стенде. Интеграционные тесты по `Scheduled` в последнем запуске падали только в `TestScheduledDeliveredOnce`, из-за сравнения JSON в самом тесте; тест исправлен, но повторно не запускался. Два интеграционных теста (`TestScheduledAlbumDeliveredTogether`, `TestScheduledWhenOnlinePages`), чтение строк после захвата и проверка `SCHEDULE_STATUS_PRIVATE` при правке добавлены позже, компилируются, но ещё не запускались. Общий прогон (e2e, -race, `make lint`) и проверка в клиентах впереди.

Решения для подтверждения:
- Сообщение, которое в срок уже нельзя отправить (нет прав, вышел из чата), удаляется из очереди, отправитель получает `updateDeleteScheduledMessages` без `sent_messages`. Документация об этом молчит.
- Ручная отправка идёт без `from_scheduled`: документация связывает флаг с отправкой в срок; значит, напоминание, отправленное вручную, сразу прочитано.
- «Когда будет в сети» ждёт без ограничения по времени: документация и клиенты предела не называют. Удерживают только предел очереди 100 и удаление вручную.
- В канале очередь общая для админов с `post_messages`; чужой пост правит `edit_messages`, удаляет `delete_messages`, ручная отправка считается правкой. В супергруппе очередь у каждого своя.
- Предел даты 366 суток выведен из клиентского выбора дня, точное значение Telegram не опубликовано.

Открыто: `schedule_repeat_period` не поддержан и сейчас молча игнорируется; нужен отдельный бриф, это функция Premium.
