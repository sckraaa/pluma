# 063. Автоудаление, черновики и архив с синхронизацией

- Статус: done (сервер; проверка в клиентах — позже)
- Зависит от: 038–044 (ящик канала), 050 (архив), 010 (журнал `seq`)
- ADR: новых нет; расширен список обновлений с `seq` из ADR 0020 (`updatePeerHistoryTTL`).

## Цель
Автоудаление сообщений работает в личных чатах, группах и каналах: таймер ставится участником с нужным правом, новые сообщения получают `ttl_period` и исчезают у всех по истечении, после перезапуска — тоже. Черновики синхронизируются между устройствами полностью по схеме слоя. Архив работает с каналами, есть `folders.deleteFolder`, `dialogFolder` считает непрочитанное правильно.

## Что известно
Источники — исходники клиентов в `~/pluma-work/reference-src`, документация core.telegram.org.

**Автоудаление.**
- `messages.setHistoryTTL`: ошибки `TTL_PERIOD_INVALID`, `CHAT_ADMIN_REQUIRED`, `CHAT_NOT_MODIFIED`, `PEER_ID_INVALID` (core.telegram.org/method/messages.setHistoryTTL).
- `messageActionSetMessagesTTL.auto_setting_from`: таймер поставлен автоматически, потому что у участника включён таймер по умолчанию; пример из документации — служебное сообщение перед *нашим* первым сообщением в новом чате (core.telegram.org/constructor/messageActionSetMessagesTTL). Страницы `core.telegram.org/api/ttl` нет (404).
- `setDefaultHistoryTTL` применяется «ко всем новым чатам» (method/messages.setDefaultHistoryTTL). При создании группы `ttl_period` присылают Android (`GroupCreateFinalActivity.java:191, :687`, `getGlobalTTl`) и tdesktop (`boxes/add_contact_box.cpp:741`); канал Android создаёт без него (`ChannelCreateActivity.java:358`, -1), tdesktop передаёт период для супергруппы (`:871-876`).
- Кто может менять: tdesktop — у пользователя не `self`, у группы и канала `canEditInformation` (`menu/menu_ttl_validator.cpp:111-125`). Android показывает пункт и при праве `delete_messages` (`ProfileActivity.java:6417`, `ChatActivity.java:30737`) — расхождение клиентов.
- Периоды: Android — 1 день … 365 дней в списке и произвольное число минут (`AlertsCreator.createAutoDeleteDatePickerDialog`, `AutoDeletePopupWrapper`, `timeInMinutes * 60`).
- `updatePeerHistoryTTL`: клиенты обновляют `userFull/chatFull/dialog.ttl_period` (Android `MessagesController.java:20540`, tdesktop `api_updates.cpp:2157`, iOS `AccountStateManagementUtils.swift:1785`, tweb `appProfileManager.ts:1543`). Отсутствие поля — выключено.
- Клиенты удаляют сообщения локально по `date + ttl_period` (Android `MessagesStorage.java:12575`).

**Черновики.**
- `messages.saveDraft` слоя 228: `reply_to:InputReplyTo`, `media:InputMedia`, `effect`, `suggested_post`, `rich_message`; `updateDraftMessage` несёт `top_msg_id` (схема).
- Android сохраняет черновик темы в облаке только для форумов (`MediaDataController.java:7803`), reply с `reply_to_msg_id == 0` считает «нет ответа» (`:7790`); `updateDraftMessage` применяет с `top_msg_id` (`MessagesController.java:20259`). tdesktop: `Api::saveDraft` с `topicRootId` в `reply_to` (`apiwrap.cpp:2376`), `ApplyPeerCloudDraft` (`data_drafts.cpp:101`).
- Android запрашивает `getAllDrafts` один раз после загрузки диалогов (`MessagesController.java:12963`, `MediaDataController.loadDraftsIfNeed`).
- ADR 0020, п. 2: черновики идут без `seq`, эхо инициатору может затереть набираемый текст.

**Архив.**
- Peer folders — только архив, `folder_id = 1` (core.telegram.org/api/folders). `folders.deleteFolder` в документации не описан (страница метода — 404); ни один клиент (Android, tdesktop, Web K) его не вызывает.
- `dialogFolder` несёт счётчики `unread_muted_*`, `unread_unmuted_*` (схема).

**Что было не так в коде.** `messages.setHistoryTTL` — заглушка; `ttl_period` у сообщений никогда не ставился, воркера не было. `dialog.getAllDrafts` не разбирал черновик и всегда отвечал пусто; `SaveDraft` не создавал строку диалога и не сбрасывал кэш; черновик каналов — `ErrEnterpriseIsBlocked` (нет плагина); отправка затирала черновик в upsert, поэтому `doClearDraft` ничего не находил и `updateDraftMessage` не уходил. В `editPeerFolders` и `getPinnedDialogs` каналов не было; `dialogFolder` считал всё как muted.

## Область
Автоудаление: личные чаты, группы, каналы/супергруппы; таймер по умолчанию; воркер. Черновики: все поля схемы, кроме `rich_message`; темы; каналы; очистка при отправке. Архив: каналы, `deleteFolder`, `getPinnedDialogs`, счётчики папки.

## Критерии приёмки
- `messages.setHistoryTTL` проверяет права и период, рассылает служебное сообщение и `updatePeerHistoryTTL`; повтор того же периода — `CHAT_NOT_MODIFIED`.
- Новые сообщения несут `ttl_period`; воркер удаляет копии каждого ящика с pts, не теряя и не удваивая при перезапуске и нескольких экземплярах.
- Черновики: личный чат без истории, группа, канал, тема; видны в `getAllDrafts`; `clear_draft` очищает и сообщает другим устройствам.
- Архив: канал переносится, `folders.deleteFolder` возвращает всё; `dialogFolder` с разделением muted/unmuted.

## Результат
Сервер готов, живая проверка в клиентах — позже.

- **Автоудаление.** `messages.setHistoryTTL` работает во всех трёх видах чатов. Период меняется служебным сообщением (`messageActionSetMessagesTTL`): в личном чате и группе через `msg.sendMessageV2`, в канале новым `channel.setHistoryTTL` под блокировкой канала с `channelperm.ChangeInfo`. В базовой группе период пишет `chat.setHistoryTTL` (право `CanChangeInfo`, как `editChatTitle`). Все новые сообщения получают `ttl_period` в `msg` (перекрывая значение у пересланных), служебное сообщение смены периода — нет. `updatePeerHistoryTTL` идёт с `seq` (в ответ — без него): добавлен в список ADR 0020, офлайн-устройство получит его из `getDifference`; канал — пушем `Channel-T`. Таймер по умолчанию применяется к первому сообщению отправителя в чате — со служебным сообщением `auto_setting_from` перед ним; при создании группы и канала без `ttl_period` берётся значение по умолчанию.
- **Воркер** (`msg`, `dao.RunExpiry`, раз в 3 с). Строка очереди (`message_expiry` / `channel_message_expiry`, миграция `migrate-20261108-history-ttl.sql`) пишется в транзакции сообщения. Каждый ящик удаляет свою копию сам (без межпользовательских событий), под блокировкой ящика и порциями по 1000: перезапуск ничего не теряет, параллельные экземпляры не удваивают. Каналы удаляются в транзакции ящика создателя с одной строкой журнала канала.
- **Черновики.** Схема слоя 228 целиком, кроме `rich_message`: `reply_to` (ответ, цитата, история), сущности (`inputMessageEntityMentionName` → `messageEntityMentionName`), `media`, эффект, предложенный пост. Черновики тем (`top_msg_id`) — новая таблица `thread_drafts` (миграция `migrate-20261108-thread-drafts.sql`) и `dialog.saveThreadDraft`. Черновик чата без истории живёт в скрытой строке `dialogs` (`deleted = 1`); `HasPrivateChat` и автоархив её чатом не считают. Каналы работают через `biz channel`. Отправка больше не затирает черновик сама: очищает `clear_draft`, другим устройствам уходит `updateDraftMessage` (и для темы).
- **Архив.** Каналы переносятся в архив. Новый `folders.deleteFolder` возвращает все чаты в основной список одним `updateFolderPeers` и снимает плашку автоархива. `messages.getPinnedDialogs` отдаёт закреплённые каналы и `dialogFolder`, который считает muted и unmuted отдельно по настройкам уведомлений (свои, иначе класса чатов); peer папки — чат с самым свежим сообщением. Ошибки `toggleDialogPin` теперь не теряются.

Решения на подтверждение:
1. Период **60 с … 366 дней** (минимум — минута, как шаг Android; значение 366 — запас к 365 дням). Сервер Telegram мог бы быть строже — **не проверено**.
2. Право менять таймер — `change_info`, как в tdesktop; Android показывает пункт ещё и при `delete_messages`. Выбрано `change_info` (указано и в задаче).
3. Служебное сообщение смены таймера и служебные сообщения, созданные при создании канала, не получают `ttl_period` (как уже было в `messages.createChat`). Остальные служебные сообщения получают — **не проверено** сверкой с Telegram.
4. `auto_setting_from` — у того, кто первым пишет сам: «перед нашим первым сообщением» в формулировке документации. Если отправитель уже писал в чат — таймер по умолчанию не применяется.
5. Новая группа участника не наследует `dialogs.ttl_period` для участников, вошедших после: период группы приходит из `getFullChat`, строка диалога получит его с первым же служебным сообщением смены.
6. Дополнение ADR 0020: `updatePeerHistoryTTL` с `seq`. Новый ADR не писал — это одно обновление в уже согласованном списке.

Не сделано: `rich_message` в черновиках (сервер не принимает rich-сообщения вообще); `saved_peer_id` монофорумов; лимит закреплённых `PINNED_DIALOGS_TOO_MUCH` на сервере (клиент ограничивает сам). `dialogFolder` есть только в `messages.getPinnedDialogs` главного списка (Android `MessagesController.java:17300` и tdesktop `apiwrap.cpp:1125` его запрашивают); добавлять ли его ещё и в `messages.getDialogs`, документация не говорит, клиенты разбирают оба ответа — оставлено как было.

Проверка: на ветке — сборка, `go vet` (с тегами `integration,e2e`), модульные тесты затронутых пакетов; интеграционные тесты на MySQL/Redis/Kafka (`TestAutoDelete*`, `TestDraftRowIsNoChat`, `TestDialogDrafts`, `TestThreadDrafts`) прошли. e2e (`TestHistoryTTLPrivate`, `TestHistoryTTLChannel`, `TestDraftsSync`, `TestArchiveFolderChannels`), `-race` и `make lint` выполняются в общем прогоне объединённой партии. На живых клиентах не проверялось.
