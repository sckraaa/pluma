# 117. Конференции: звонок без чата, ссылки, звонящие приглашения, перевод звонка один на один

- Статус: серверная часть сделана (2026-11-21), не выложена. Решения 1–12 ждут подтверждения владельца; проверки владельца — в `TESTING-BACKLOG.md`.
- Поставлена владельцем 2026-11-20: конференции, как в Telegram; сквозное шифрование, как у звонков один на один и групповых; работают из России (через amf) и из-за границы (прямо); ссылки — на именах Pluma (`pluma.chat` и зеркало `pluma.sckra.dev`).
- Зависит от: 093/094 (SFU, цепочка блоков, сквозные групповые звонки; ADR 0059, 0060), 116 (маршруты SFU по стране, ADR 0074), 026 (страницы ссылок, служба `links`), 113 (зеркало сайта `pluma.sckra.dev`).
- ADR: 0075 (`docs/adr/0075-conference-calls.md`).
- Ветка: `task/117-conference-calls`.

## Цель
Пользователь Pluma на любом из четырёх клиентов:
- создаёт конференцию из вкладки «Звонки» с выбранными людьми или только ссылку на неё;
- приглашает людей в идущую конференцию — у них звонит, как обычный звонок, на всех устройствах; принять, отклонить, пропустить;
- входит по ссылке `pluma.chat/call/<slug>` из любого чата или извне;
- превращает идущий звонок один на один в конференцию кнопкой «добавить» — собеседник переходит в неё сам;
- видит приглашения в чате и во вкладке «Звонки» (входящие, исходящие, пропущенные).
Каждая конференция сквозная (tde2e, ADR 0060). Из России — через трубу amf (ADR 0074), из-за границы — прямо, без выбора пользователем.

## Источники
Закреплённые апстримы (по `upstream.env` каждого клиента; layer 233), чистые копии в `~/pluma-work/reference-src-new`:
- Android — DrKLO/Telegram 13.2.1, `7be672ea` (`android/`); пути ниже от `TMessagesProj/src/main/java/org/telegram/` (сокращение `AND`).
- iOS — Telegram-iOS 13.2, `24638bc69b` (`ios/`); пути от `submodules/` (`IOS`).
- Desktop — tdesktop v7.4.1, `157f5cfc` (`tdesktop/`); пути от `Telegram/SourceFiles/` (`TD`).
- Web K — tweb `aa74832e` (`tweb/`); пути от `src/` (`TWEB`).
- Патчи: `ci/patch.py` рабочих копий `wt-pluma-<client>-109-<client>` и `origin/main` pluma-ios (676ce1c); реестры `clients/patches/*.md`.
- Исходников сервера Telegram (tdlib `GroupCallManager.cpp` и т. п.) на диске нет: в `tdlib-td` только схемы и `tde2e`. Поведение сервера выведено из клиентов; где клиенты молчат — помечено «вывод» и вынесено в решения.

Конструкторы — в схеме сервера `third_party/teamgram-proto` (v0.233.0), CRC совпадают с клиентами (`schema.tl.crc32.proto`: `messageActionConferenceCall` 2ffe2f7a :368, `phoneCallDiscardReasonMigrateConferenceCall` 9fbbf1f7 :1147, `inputGroupCallSlug` fe06823f :1525, `createConferenceCall` 7d0444bb :3232). Схема в каталоге: `pkg/catalog/tl/api.tl:197` (действие), `:406` (`updateGroupCall`), `:1050-1051` (`phoneCall`, `phoneCallDiscarded`), `:1439` (`groupCall`).

## Как сейчас
- `app/bff/groupcalls` обслуживает звонки групп и каналов, все сквозные: `joinGroupCall` с ключом и блоком, `getGroupCallChainBlocks`, `sendConferenceCallBroadcast`, `deleteConferenceCallParticipants` (бриф 094). `createConferenceCall` (обе версии), `inviteConferenceCallParticipant`, `declineConferenceCallInvite` отвечают `ErrEnterpriseIsBlocked` (`internal/core/not_served.go`).
- `load` принимает только `inputGroupCall`, slug и сообщение — `GROUPCALL_INVALID` (`internal/core/core.go:80-98`; `getGroupCallChainBlocks_handler.go:47` так же).
- Звонок всегда принадлежит чату: `group_calls.peer_type/peer_id`, уникальный ключ `live_peer (peer_type, peer_id, live)` и `creator_random (creator_id, peer_type, peer_id, random_id)` (`teamgramd/deploy/sql/migrate-20261117-group-calls.sql`). Права — `access` (членство, `manage_call`; `internal/core/access.go`), вход проверяет `a.canView` (`phone.joinGroupCall_handler.go:59-64`).
- `exportGroupCallInvite` строит `https://<TDotMe>/<username>?voicechat=<hash>`, у частного чата — `PUBLIC_CHANNEL_MISSING` (`phone.exportGroupCallInvite_handler.go:40-90`).
- Маршрут SFU по стране выбирается в `joinGroupCall` (`routes()`, `connect.go:153-162`; `phone.joinGroupCall_handler.go:91, 124`), ответы и рассылки цепочки несут канонический `call.Input()` (`:116, :162`).
- Звонки один на один: `phone.discardCall` сводит любую незнакомую причину к `hangup` (`pkg/phonecalls/phonecalls.go:201-213`), slug причины перевода теряется; `conference_supported` не ставится нигде (поиск `ConferenceSupported` в `app/`, `pkg/` пуст — есть только в сгенерированном коде). Значит, кнопка «добавить» в звонке один на один сейчас скрыта на Android (`AND/ui/VoIPFragment.java:2054`), iOS (`IOS/TelegramCore/Sources/State/CallSessionManager.swift:1204`), Desktop (`TD/calls/calls_call.cpp:1106`).
- Пуши: вид звонка `KindCall` только для `updatePhoneCall` с `phoneCallRequested` (`app/messenger/push/internal/notify/notification.go:218-260`, Android `PHONE_CALL_REQUEST` — `android.go:103-104`); служебные сообщения личных чатов пушей не дают (`withAction`, `notification.go:364-414`: только действия групп).
- Поиск звонков: тип `MEDIA_PHONE_CALL` даёт только `messageActionPhoneCall` (`third_party/teamgram-proto/mtproto/message_filter_util.go:60-64`); «пропущенные» — по `$.action.reason` (`pkg/msgsearch/kind.go:113-118`).
- Служба ссылок разбирает пути `pluma.chat/...` (`app/interface/links/internal/links/route.go`), `call/` в ней нет.
- `conference_call_size_limit` в app config нет (`teamgramd/bin/app_config.json`); `call_ring_timeout_ms` = 90000 (`teamgramd/bin/config.json:41`).

## Как клиенты ведут конференцию (прочитано 2026-11-20)

### Создание
- `phone.createConferenceCall#7d0444bb flags:# muted:flags.0?true video_stopped:flags.2?true join:flags.3?true random_id:int public_key:flags.3?int256 block:flags.3?bytes params:flags.3?DataJSON = Updates` (TD `mtproto/scheme/api.tl:3151`).
- **Только ссылка** (`flags = 0`, один `random_id`): Android — «Новый звонок», «Ссылка на звонок» (`AND/ui/Components/CreateGroupCallSheet.java:181-226`, `ui/CallLogActivity.java:1794-1845`), iOS (`IOS/TelegramCore/Sources/TelegramEngine/Calls/GroupCalls.swift:3348`), Desktop «Создать звонок» (`TD/calls/group/calls_group_common.cpp:498-503`), Web K «Новый звонок» (`TWEB/lib/appManagers/appGroupCallsManager.ts:1563-1580`).
  - Все ищут в ответе `updateGroupCall`: без него Desktop пишет «Call not found!» (`TD/data/data_session.cpp:1472-1500`), iOS падает (`GroupCalls.swift:3352-3354`), Web K бросает «no unique active group call» (`TWEB/helpers/groupCallUpdates.ts`).
  - Desktop и Web K берут ссылку из `groupCall.invite_link` (`TD/data/data_group_call.cpp:550`, «Call link not found!» без неё — `calls_group_common.cpp:505-516`; `TWEB/helpers/groupCallUpdates.ts:59`). Android берёт `invite_link` из `updateGroupCall` или зовёт `exportGroupCallInvite` при ответе `phone.groupCall` (`CallLogActivity.java:1810-1838`). iOS `invite_link` не читает (`GroupCalls.swift:150`) и сразу зовёт `exportGroupCallInvite(can_self_unmute)`, slug — последний сегмент пути (`:3365-3373`).
  - Значит, в `groupCall` конференции нужны `conference` (flags.14), `creator` (flags.15) для создателя и `invite_link` (flags.16) (AND `TLRPC.java:2168-2194`). Создатель видит «завершить» и «исключить» только с `creator` (AND `ui/GroupCallActivity.java:1767, 7645, 8131`; TD `data_group_call.cpp:147, 520`).
- **Создать и войти** (`join | public_key | block | params`): Desktop (`TD/calls/group/calls_group_call.cpp:1732-1777`, блок — `_e2e->makeJoinBlock()`, нулевой), Web K (`TWEB/lib/calls/groupCallsController.ts:803-812`, нулевой блок с `externalPermissions: 3`). Ответ обрабатывается как ответ на вход (`joinDone(..., justCreated=true)`, TD `calls_group_call.cpp:1782-1832`); Desktop ждёт `updateGroupCall` и `updateGroupCallParticipants` с версией 2 (`data_group_call.cpp:612-617`). Если обработка ответа не удалась, Web K сам зовёт `discardGroupCall` (`appGroupCallsManager.ts:~1220-1235`).
- Android и iOS создают без `join` и входят отдельным `joinGroupCall` с ключом (AND `messenger/voip/VoIPHelper.java:253`, `VoIPService.java:2418-2440`; IOS `TelegramUI/Sources/SharedAccountContext.swift:2206-2310`, `TelegramCallsUI/Sources/PresentationCallManager.swift:1110-1150`).

### Три формы `InputGroupCall`
- Клиенты зовут методы со ссылкой на звонок той формы, которой узнали о нём: `inputGroupCallSlug` (вход по ссылке), `inputGroupCallInviteMessage{msg_id}` (вход из приглашения) или канонический `inputGroupCall`.
  - Desktop: `inputCallSafe()` для `joinGroupCall` и `getGroupCallChainBlocks(0, -1, 1)` перед входом (`TD/calls/group/calls_group_call.cpp:4246-4253, 1644-1730`).
  - Android: `ChatObject.Call.getInputGroupCall()` отдаёт slug из `invite_link` у конференции (`AND/messenger/ChatObject.java:1289-1300`) — и он идёт в выход, проверку, показ экрана, участников, правку (`VoIPService.java:1073, 1420, 2125, 2777, 3798`); все запросы `ConferenceCall` — по `joinConference` (`messenger/voip/ConferenceCall.java:306, 515, 572, 655, 701`).
  - iOS: `InternalGroupCallReference` `.id`/`.link`/`.message` идёт как есть в `getGroupCall`, `joinGroupCall`, `getGroupCallChainBlocks`, `exportGroupCallInvite`, `toggleGroupCallSettings` (`GroupCalls.swift:180-196, 215, 272, 703, 2848, 3400, 3419`); для `.message` — только пользователь и облачное пространство (`:209-212`).
  - Web K: `getGroupCall(limit=5)`, `getGroupCallChainBlocks`, `joinGroupCall` по slug или сообщению (`TWEB/lib/appManagers/appGroupCallsManager.ts:676-685, ~1260-1281`; `lib/calls/groupCallsController.ts:1679-1689`).
- **Ответ — всегда с каноническим звонком.** iOS отбрасывает `updateGroupCallParticipants` и `updateGroupCallChainBlocks` с формой slug или сообщения (`IOS/TelegramCore/Sources/State/Sync/UpdateInterpreter+Calls.swift:18-40`). Web K требует в ответе на вход `updateGroupCall` с id и access_hash предпросмотра («accepted non-canonical call did not match its preview identity», `TWEB/helpers/groupCallUpdates.ts:73`) и `updateGroupCallChainBlocks` с каноническим `call` (`:22-36`).
- **Кончившаяся конференция** — `phone.groupCall` с `groupCallDiscarded`, не ошибка: iOS превращает его в «не существует» и предлагает позвонить тем же людям (`GroupCalls.swift:168-169`; `Peers/JoinLink.swift:277-279`; `TelegramUI/Sources/TelegramBaseController.swift:280-318`), Web K — «InviteExpired» (`TWEB/lib/appManagers/appImManager.ts:2618`), Desktop — отдельная ветка (`TD/window/window_session_controller.cpp:1176`).
- **Неизвестный или сброшенный slug** — `GROUPCALL_INVALID`: Android показывает «LinkIsNoActive» (`AND/ui/GroupCallSheet.java:59-120`) и при опросе повторяет по id (`VoIPService.java:2876-2898`), Desktop — `lng_confcall_not_accessible` (`calls_group_call.cpp:1835-1858`), Web K — «InviteExpired» (`TWEB/lib/calls/conferenceInviteError.ts`).

### Вход
- Ошибки входа, которые клиенты разбирают: `CONF_WRITE_CHAIN_INVALID*` (перечитать вершину и войти снова — TD `calls_group_call.cpp:1835-1858`, Web K `groupCallsController.ts:878-937`; Android апстрима зовёт `stopSelf()` — `VoIPService.java:2711`, патч Pluma задачи 094 повторяет и для конференций — `ci/patch.py:6166-6168`), `GROUPCALL_SSRC_DUPLICATE_MUCH`, `GROUPCALL_FORBIDDEN`/`GROUPCALL_INVALID` (недоступна), `GROUPCALL_PARTICIPANTS_TOO_MUCH` (AND `ui/GroupCallActivity.java:1337`, IOS `GroupCalls.swift:707`). Web K повторяет до 3 раз ошибку с префиксом `NOT_PARTICIPANT:`.
- Предел — app config `conference_call_size_limit` (по умолчанию 100, в тестовом режиме 5): Desktop не даёт войти по ссылке при `fullCount() >=` предела (`TD/main/main_app_config.cpp:154-158`, `window_session_controller.cpp:1140-1143`); Android (`AND/messenger/MessagesController.java:4803-4808`), iOS (`IOS/TelegramCore/Sources/State/UserLimitsConfiguration.swift:186`).
- Вход всегда «от себя» (`joinAsPeerId: nil`, IOS `PresentationCallManager.swift:1139`).

### Приглашение
- `phone.inviteConferenceCallParticipant#bcf22685 flags:# video:flags.0?true call:InputGroupCall user_id:InputUser = Updates` (TD `api.tl:3154`). Шлют после успешного входа по одному на человека (AND `VoIPService.java:2613-2700`; IOS `PresentationCallManager.swift:1151-1153`; TD `calls_group_call.cpp:4048-4083`; Web K `TWEB/lib/appManagers/appCallsManager.ts:322`) и из «Добавить людей» в звонке (AND `GroupCallActivity.java:3806-3826`). В конференции добавлять может любой участник (TD `calls/group/calls_group_members.cpp:1840-1843`: `conference` → `true`).
- Из ответа берут id нового сообщения: Android — `updateNewMessage` с `messageActionConferenceCall`, иначе `updateMessageID` (`VoIPService.java:2629-2655`); iOS — `result.messageIds.first`, без него ошибка (`GroupCalls.swift:1010-1014`).
- Ошибки: `USER_PRIVACY_RESTRICTED` (Android предлагает ссылку через `account.getRequirementsToContact`, `VoIPService.java:2656-2700`; iOS — окно приватности, `GroupCalls.swift:995`), `USER_ALREADY_PARTICIPANT`, `USER_WAS_KICKED`, `GROUPCALL_FORBIDDEN` (войти заново и повторить) (TD `calls_group_call.cpp:4052-4080`; Web K `components/groupCall/inviteParticipants.ts:34-36`).
- **Звонит служебное сообщение, не `updatePhoneCall`.** `messageActionConferenceCall#2ffe2f7a flags:# missed:flags.0?true active:flags.1?true video:flags.4?true call_id:long duration:flags.2?int other_participants:flags.3?Vector<Peer>` (TD `api.tl:200`). Состояние — `ComputeCallData`: `duration` — завершён, `missed` — пропущен, `active` — идёт, ничего — приглашение (TD `data/data_media_types.cpp:513-533`).
  - Android звонит на новое входящее сообщение без `active`/`missed`, если `now - date < callRingTimeout` (`AND/messenger/NotificationsController.java:1088-1109`), затем `phone.getGroupCall{inputGroupCallInviteMessage, limit=3}`; ошибка — приглашение молча забыто (`messenger/voip/VoIPGroupNotification.java:138-169`). По таймеру `callRingTimeout` сам шлёт отказ (`:324`).
  - iOS собирает такие сообщения из входящих в личных чатах (`IOS/TelegramCore/Sources/State/Sync/StateReplayPrepass.swift:97-103`, `AccountStateEventHub.swift:393-394`) и звонит, пока у действия нет ни `missed`, ни `duration` (`CallSessionManager.swift:486-500`). Своего таймера у принимающего нет (в TelegramCore не найден, вывод) — без правки сообщения сервером звонок не кончится.
  - Desktop звонит из `History::applyServiceChanges` (`TD/history/history.cpp:1527-1531`); `showConferenceInvite` не звонит, если конференция уже текущий звонок, сам отклоняет при занятости и если `date + callRingTimeoutMs/1000 < now` (`TD/calls/calls_instance.cpp:1166-1236`), звонок длится `callRingTimeoutMs` (`calls/calls_call.cpp:256-260`).
  - Web K звонит на новое сообщение без `duration`/`missed`/`active`, не старше 45 с (`TWEB/lib/calls/conferenceInvitesController.ts:99, 303`; `constants.ts:33`), сам отклоняет при занятости.
- **Принять:** `getGroupCall(inputGroupCallInviteMessage)` и вход той же формой (AND `VoIPGroupNotification.java:346-363`; IOS `PresentationCall.swift:845-847`; TD `calls_call.cpp:453-480`; Web K `conferenceInvitesController.ts:368`).
- **Отклонить:** `phone.declineConferenceCallInvite#3c479971 msg_id:int = Updates` (AND `VoIPGroupNotification.java:366-386`; IOS `CallSessionManager.swift:800-820`; TD — для всех входящих приглашений этой конференции, `calls_instance.cpp:1099-1120`; Web K `conferenceInvitesController.ts:394`).
- **Пригласивший отменяет** тот же методом со своим исходящим `msg_id` (AND `GroupCallActivity.java:3717-3752`; IOS `CallSessionManager.swift:784-798`; TD `calls_instance.cpp:1122-1164`) либо удаляет приглашение у всех (`deleteMessages(revoke)`: TD там же, IOS `TelegramCallsUI/Sources/PresentationGroupCall.swift:3977-3988`).
- **Как кончается звонок у приглашённого и «звоним» у пригласившего — только правкой сообщения.**
  - Android гасит звонок по правке с `active` или `missed` (`NotificationsController.java:1031-1035`); у пригласившего `invited.calling = !missed && !active` (`MessagesController.java:21022` → `VoIPService.java:5930-5948`).
  - iOS у пригласившего: «звоним» кончается через 30 с от даты сообщения, принято — по `active`, отказ — по `missed`/`duration` (`PresentationGroupCall.swift:236-300`).
  - Desktop: правка заменяет медиа сообщения, деструктор старого снимает приглашение и гасит текущий входящий звонок (`TD/history/history_item.cpp:2682-2702`, `data/data_media_types.cpp:1839-1849`, `calls_instance.cpp:1092-1097`).
  - Web K: любая правка из состояния «приглашение» гасит звонок (`conferenceInvitesController.ts:103`).
- **Пуши приглашения.**
  - Android: `loc_key` `CONF_CALL_REQUEST`/`CONF_VIDEOCALL_REQUEST`, `custom.call_id`, `custom.msg_id`, имена — `loc_args[2]`; звонит, если пуш моложе `callRingTimeout` (`AND/messenger/PushListenerController.java:367-384`). `CONF_CALL_MISSED`: гасит звонок по `call_id`, текст «пропущенный звонок» с `args[0]` — имя, `args[1]` — число остальных (`:1407-1415, 1513-1515`).
  - iOS, расширение уведомлений: `loc-key` `CONF_CALL_REQUEST`/`CONF_VIDEOCALL_REQUEST`, `from_id`, строки `call_id`, `msg_id`, `call_conference_from`, необязательный `call_participants_cnt` (`Telegram/NotificationService/Sources/NotificationService.swift:1095-1113`); расширение передаёт его в CallKit (`reportNewIncomingVoIPPushPayload`, `:1466-1485`).
  - iOS, PushKit: расшифрованный `p` с `from_id`, `group_call_id`, `msg_id`, `from_title` (+ `video`, `member_count`) сразу идёт в CallKit (`IOS/TelegramUI/Sources/AppDelegate.swift:2273-2346`, `2345-2410`). Приглашение ждёт сообщение в локальной базе до 5 с, иначе звонок гаснет (`CallSessionManager.swift:441-456, 476-501`): сообщение должно сразу приходить в `getDifference`.

### Сообщение в чате и вкладка «Звонки»
- Пузырь: AND `ui/Cells/ChatMessagePhoneCall.java:70, 253`, TD `history/view/media/history_view_call.cpp:48, 77`. Тап: `getGroupCall(inviteMessage, limit=conference_call_size_limit)`; есть участники — вход, нет или `GROUPCALL_INVALID` — новый звонок с теми же людьми (AND `ui/ChatActivity.java:40439-40470`; IOS `TelegramBaseController.swift:280-318`; TD — `PrepareInviteToEmptyBox`, `window_session_controller.cpp:1105-1175`; Web K `components/chat/bubbles.ts:4228-4236`).
- Вкладка «Звонки» Android — `messages.search` с `inputMessagesFilterPhoneCalls` (`ui/CallLogActivity.java:1207-1210`); строка конференции входящая или исходящая по `from_id`, пропущенная по `missed` (`:291-300`).

### Перевод звонка один на один
- Кнопка «добавить» — только при `phoneCall.conference_supported` (flags.8): AND `VoIPFragment.java:2054`, IOS `CallSessionManager.swift:1204` (`isConferencePossible`), TD `calls_call.cpp:1106` / `calls_panel.cpp:690-691, 1827`.
- Порядок (Android `VoIPService.java:2201-2214, 2480-2600`; iOS `TelegramCallsUI/Sources/PresentationCall.swift:1721-1736`, `CallSessionManager.swift:949-966, 1035-1066, 1785-1786`; Desktop `calls_panel.cpp:497-525`, `calls_instance.cpp:295-314`, `calls_call.cpp:710-733`): создать конференцию и войти → `exportGroupCallInvite`, slug — последний сегмент пути (Android с `flags=0`) → `phone.discardCall(reason = phoneCallDiscardReasonMigrateConferenceCall{slug})` → приглашения выбранным.
- Собеседник получает `phoneCallDiscarded` с этой причиной и сам входит по `inputGroupCallSlug` без вопроса (AND `VoIPService.java:1807-1819`; IOS `CallSessionManager.swift:1160-1162`, `PresentationCall.swift:849-905`; TD — только если звонок был принят обеими сторонами, `calls_call.cpp:905-918, 950-975`; Web K `lib/calls/callsController.ts:164, 716-738`). Web K сам перевод не начинает и `conference_supported` не читает.
- Сообщение звонка один на один с этой причиной Desktop понимает как отдельное состояние (`TD/data/data_media_types.cpp:501-502`).
- iOS знает ещё `phoneCall.custom_parameters {"inline_conference": slug}` (`PresentationCall.swift:1040-1046`); сервер его не ставит (когда ставит Telegram — не известно).

## Ссылки
- **Разбор в клиентах.** Android: путь `call/` → `conferenceSlug` (`AND/ui/LaunchActivity.java:2127-2128`), схема `call?slug=` (`:2681-2687`). iOS: `<хост>/call/<slug>` → `.joinCall`, ведущий `+` снимается (`IOS/UrlHandling/Sources/UrlHandling.swift:522-527`), схема (`TelegramUI/Sources/OpenUrl.swift:699-701`). Desktop: `t.me/call/<slug>` → `tg://call?slug=` (`TD/core/local_url_handlers.cpp:2220-2222`), знаки slug `[a-zA-Z0-9._-]` (`:2001-2002`). Web K: `TWEB/lib/internalLinkProcessor.ts:334, 352, 1565-1576`, проверка `^[A-Za-z0-9_-]{1,64}$` (`constants.ts:54`). Все берут slug как последний сегмент пути (AND `ChatObject.java:1297-1299`, IOS `GroupCalls.swift:3373`, TD `calls_group_common.cpp:539-553`).
- **Патчи Pluma — нового правила не нужно.** Каждый клиент считает `pluma.chat` своим хостом «как t.me», путь дальше разбирает апстрим, а `call` — не страница сайта:
  - Android: хост в LaunchActivity, LinkManager, Browser, `linkPrefix`, intent-filter `autoVerify` с `/call/` в `LINK_PATH_PREFIXES`, литерал `https://t.me/call/` в VoIPService → `pluma.chat` (`ci/patch.py:3437-3466, 3530-3550, 3700-3715`; `clients/patches/android.md:863, 874-878`, A-099).
  - iOS: `LINK_HOST` в `baseTelegramMePaths`, `telegramMeHosts`, UrlWhitelist, GenerateTextEntities, `applinks:pluma.chat` (`ci/patch.py:555-575, 4873-4887`; `ios.md:966-968`, I-103); `call` нет в `PLUMA_SITE_PATHS` (`:625-626`).
  - Desktop: регэксп `telegramMeMatch` с `pluma.chat` (`ci/patch.py:1538-1547`), `call` нет в `SITE_PATHS` (`:1519-1526`; `desktop.md:913-914`, D-079).
  - Web K: `pluma.chat` в `matchTelegramUrlHost`, `call` нет в `LINK_SITE_PATHS` (`ci/patch.py` ~1528-1720; `web.md:303-309`).
  - Схема приложений — `plm://` вместо `tg://` во всех клиентах (Android A-016, `android.md:245-248`), так что `plm://call?slug=` приходит в тот же обработчик.
- **`pluma.sckra.dev` не знает ни один клиент** (в `ci/patch.py` четырёх клиентов «sckra» есть только в `SOURCE_URL` Web K). Ссылка на зеркало внутри приложения открылась бы в браузере.
- **Дизайн:** сервер всегда строит `https://<TDotMe>/call/<slug>` (`env2.TDotMe`, на проде `pluma.chat`, `pkg/env2/env2.go:16-19`) — и клиенту из России. Внутри приложения slug разбирается без сети, вход идёт по MTProto тем путём, каким клиент ходит к серверу (прямо, край `ru`, Gcore). Вне приложения ссылку открывает страница службы `links` (новый вид `KindConferenceCall`: «Присоединиться к звонку», передача в приложение `plm://call?slug=<slug>`), она же отвечает на зеркале `pluma.sckra.dev/call/<slug>` (зеркало уже отдаёт ссылки — `ru-site.caddy`, бриф 113). Решение 2.
- **Срок и сброс.** Ссылка живёт, пока жива конференция. Создатель сбрасывает её `phone.toggleGroupCallSettings(reset_invite_hash)` и берёт новую `exportGroupCallInvite` (AND `CallLogActivity.java:1613-1625`; IOS `GroupCalls.swift:3399-3415`; TD `calls_group_common.cpp:308-330`; Web K `components/call/callLinkPopup.tsx:87-88`). Старый slug сразу — `GROUPCALL_INVALID`; уже вошедших по нему сброс не выгоняет, а Android после сброса переходит на id (`VoIPService.java:2876-2898`). Решение 3.
- Превью ссылки в чате с кнопкой «JoinCall» у Web K — по `webPage.type == 'telegram_call'` (`TWEB/components/chat/bubbles.ts:649`). Умеет ли наше превью ссылок такой тип — не проверено (часть 8, по возможности).

## Дизайн

### 1. Хранение
- Миграция `teamgramd/deploy/sql/migrate-<дата>-conference-calls.sql`, повторяемая:
  - `group_calls.conference tinyint NOT NULL DEFAULT 0`. Строка конференции: `peer_type = 0`, `peer_id = id` звонка — ключ `live_peer` остаётся верным, `creator_random` не мешает.
  - `group_call_conferences`: `call_id` (PK), `creator_id`, `random_id`, `slug` (UNIQUE), `slug_date`, `had_participants`, `last_empty_at`; UNIQUE `(creator_id, random_id)` — повтор создания с тем же `random_id` отдаёт тот же звонок (как `creator_random` у звонков чатов).
  - `group_call_invites`: `id`, `call_id`, `inviter_id`, `invitee_id`, `inviter_msg_id`, `invitee_msg_id`, `video`, `state` (ringing/active/missed/ended), `date`, `ring_until`; ключи `(invitee_id, invitee_msg_id)`, `(inviter_id, inviter_msg_id)`, `(call_id, state)`, `(state, ring_until)`.
  - `group_call_participants.kicked tinyint` — исключённый создателем (решение 7).
  - `phone_calls.discard_slug varchar(64)` — slug причины перевода; `discard_reason` 5 = перевод.
- `pkg/groupcalls`: `Call.Conference`, `ConferenceStore` (создание с идемпотентностью, поиск по slug, сброс slug, приглашения: создание, смена состояния по условию «было ringing», выбор просроченных), всё в транзакциях с блокировкой строки звонка, как `Join`.

### 2. Политика доступа
- `access` звонка чата и новая `conferenceAccess` за одним интерфейсом `policy` (`canView`, `canJoin`, `canManage`, `members` для рассылки): обработчики перестают знать, чей звонок.
- Конференция:
  - видеть (`getGroupCall`, участники, вершина цепочки) и входить может: участник или бывший участник (не исключённый), создатель, тот, кто пришёл с действующим slug, адресат приглашения этой конференции (по его сообщению в любом состоянии, пока конференция жива — Android входит тапом по старому сообщению, `ChatActivity.java:40439-40470`);
  - приглашать — любой текущий участник (TD `calls_group_members.cpp:1840-1843`);
  - `canManage` (флаг `creator`, исключение, сброс ссылки, завершение для всех) — только создатель (AND `GroupCallActivity.java:1767, 7645, 8131`). Передачи создателя нет.
- Разрешение ссылки на звонок: `resolve(InputGroupCall)` вместо `load`:
  - `inputGroupCall` — как сейчас;
  - `inputGroupCallSlug` — только действующий slug конференции; иначе `GROUPCALL_INVALID`;
  - `inputGroupCallInviteMessage{msg_id}` — приглашение, где вызывающий — `invitee` с этим `invitee_msg_id` или `inviter` с этим `inviter_msg_id`; иначе `GROUPCALL_INVALID`. Пришедшему по slug или приглашению `resolve` отдаёт и основание доступа.
  - Во всех методах `phone.*` с `InputGroupCall` (`getGroupCall`, `joinGroupCall`, `leaveGroupCall`, `checkGroupCall`, `getGroupParticipants`, `editGroupCallParticipant`, `joinGroupCallPresentation`, `leaveGroupCallPresentation`, `toggleGroupCallSettings`, `exportGroupCallInvite`, `discardGroupCall`, `getGroupCallChainBlocks`, `sendConferenceCallBroadcast`, `deleteConferenceCallParticipants`, `inviteConferenceCallParticipant`, `editGroupCallTitle`, `inviteToGroupCall`, `startScheduledGroupCall`, `toggleGroupCallStartSubscription`). Методы, у конференции не имеющие смысла (`editGroupCallTitle`, `inviteToGroupCall`, `startScheduledGroupCall`, `toggleGroupCallStartSubscription`, `saveDefaultGroupCallJoinAs`), у конференции отвечают `GROUPCALL_FORBIDDEN` (решение 11).
- Ответы и рассылки — всегда `call.Input()` (канонический), как уже делают `joinGroupCall` и цепочка.

### 3. Методы
- **`phone.createConferenceCall#7d0444bb`.** Право — любой пользователь (не бот, не удалённый). Не больше 20 живых конференций на создателя, сверх — `CONFERENCE_LIMIT_EXCEEDED` (решение 10). Повтор с тем же `random_id` — тот же звонок.
  - Без `join`: строка звонка и конференции со slug, цепочка пуста (высота −1), `version = 1`. Ответ — `Updates` с одним `updateGroupCall{call}` (`conference`, `creator`, `invite_link`), без `peer`.
  - С `join` (`public_key`, `block`, `params` обязательны, `muted`, `video_stopped`): в одной транзакции строка, нулевой блок создателя (ADR 0060 §2: высота 0, в составе только он) и вход; затем `connectJoined` с `routes()` (ADR 0074). Ответ — как ответ `joinGroupCall`: `updateGroupCall` (версия 2), `updateGroupCallConnection`, `updateGroupCallParticipants` (версия 2), `updateGroupCallChainBlocks` обеих подцепочек. Отказ SFU — конференция завершается (некого держать), ошибка клиенту; Web K в этом случае сам зовёт `discardGroupCall` — тот отвечает успехом и для уже завершённой.
- **`phone.createConferenceCall#dfc909ab`** (старый, `= phone.PhoneCall`, `schema.tl.crc32.proto:3419`) — остаётся `ErrEnterpriseIsBlocked`: клиенты layer 233 его не шлют.
- **`phone.getGroupCall`** по любой форме: `phone.groupCall` с каноническим звонком, участниками до `limit`, `participants_next_offset`; кончившаяся — `groupCallDiscarded` (не ошибка). Посторонний (нет ни участия, ни slug, ни приглашения) — `GROUPCALL_FORBIDDEN`.
- **`phone.joinGroupCall`** по любой форме — существующий код входа с политикой конференции. Сверх `conference_call_size_limit` участников в звонке — `GROUPCALL_PARTICIPANTS_TOO_MUCH`. Исключённый — `GROUPCALL_FORBIDDEN`. Вход адресата приглашения → приглашение `active` (п. 4).
- **`phone.getGroupCallChainBlocks`** по любой форме; не участнику — только последний блок подцепочки 0 (как сейчас, ADR 0060 §2), что и нужно перед входом (TD `calls_group_call.cpp:1688-1730`, Web K `groupCallsController.ts:1679-1689`). В ответе `call` — канонический; `next_offset` ≥ `len(blocks)`, пустой ответ — `next_offset = 0` (`TWEB/helpers/groupCallUpdates.ts:22-36`). На пустой цепочке клиент строит нулевой блок сам.
- **`phone.exportGroupCallInvite`** конференции: любой, кто может видеть звонок; `https://<TDotMe>/call/<slug>` при любом `can_self_unmute` (iOS просит с ним, Android без — оба получают ту же ссылку).
- **`phone.toggleGroupCallSettings(reset_invite_hash)`** конференции: только создатель; новый slug, `updateGroupCall` с новым `invite_link` участникам. `join_muted`, `messages_enabled` у конференции не меняются (решение 11).
- **`phone.inviteConferenceCallParticipant`**: вызывающий — текущий участник (`in_call`), иначе `GROUPCALL_FORBIDDEN` (Desktop входит заново и повторяет, `calls_group_call.cpp:4052-4080`). Адресат:
  - не сам вызывающий, не бот, не удалённый (`USER_ID_INVALID`, `INPUT_USER_DEACTIVATED`);
  - уже в звонке — `USER_ALREADY_PARTICIPANT`;
  - исключён, а приглашает не создатель — `USER_WAS_KICKED` (приглашение создателя снимает исключение, решение 7);
  - заблокировал вызывающего или приватность звонков (`PHONE_CALL`) не пускает — `USER_PRIVACY_RESTRICTED`, как `phone.requestCall` (`voipcalls/internal/core/phone.requestCall_handler.go:69-79`; решение 5);
  - звонит уже приглашение этой конференции этому адресату — новое не создаётся, ответ — `Updates` с прежним исходящим сообщением (повтор Desktop после повторного входа не даёт второго звонка).
  - Иначе: `msg_sendMessageV2` от вызывающего в личный чат адресата с `messageActionConferenceCall{call_id, video, other_participants}` (`other_participants` — участники в звонке без вызывающего и адресата, не больше 10 — решение 9), строка `group_call_invites` (`ringing`, `ring_until = date + call_ring_timeout_ms`), пуш (п. 6). Ответ — `Updates` от `msg` вызывающему: `updateNewMessage` (и `updateMessageID`) его исходящего сообщения.
  - Не больше 30 приглашений в минуту на пользователя (`FLOOD_WAIT_X`).
- **`phone.declineConferenceCallInvite{msg_id}`**: `msg_id` — в ящике вызывающего, входящее (адресат отклоняет) или исходящее (пригласивший отменяет). Приглашение `ringing` → `missed`; уже не `ringing` — ответ без изменений (повтор, гонка с входом). Ответ — `Updates` с `updateEditMessage` вызывающего. Чужое или не приглашение — `MESSAGE_ID_INVALID`.
- **`messages.deleteMessages(revoke)`** исходящего приглашения пригласившим (TD `calls_instance.cpp:1122-1164`, IOS `PresentationGroupCall.swift:3977-3988`): удаление у обоих уже работает для личных чатов; приглашение `ringing` при этом → `missed` без правки (сообщения нет), у адресата звонок гаснет удалением (Desktop — деструктор медиа; Android и iOS — не проверено, часть 9).
- **`phone.discardGroupCall`** конференции — только создатель; все приглашения `ringing` → `missed`, `active` → `ended` с `duration`.
- **`phone.deleteConferenceCallParticipants`**: `only_left` — как сейчас; `kick` — только создатель, исключённым ставится `kicked`.
- **`phone.discardCall`** (`voipcalls`): `phoneCallDiscardReasonMigrateConferenceCall{slug}` принимается, если slug — живая конференция, где сбросивший — создатель или участник; хранится (`discard_reason = 5`, `discard_slug`) и уходит второй стороне в `phoneCallDiscarded.reason` как есть; сообщение звонка в истории — с той же причиной (Desktop показывает её, `data_media_types.cpp:501-502`). Пустой или чужой slug (Android шлёт пустой на одном пути, `VoIPService.java:4540-4542`) — `hangup`.
- **`phoneCall.conference_supported`** ставится всегда (`pkg/phonecalls/phonecalls.go:243-275`, `Call.TL`). Решение 4.

### 4. Состояния приглашения и правки сообщения
```
ringing ──вход адресата──────────► active ──конец конференции──► ended (duration)
   │──decline адресата / отмена пригласившего / ring_until / удаление / конец──► missed
```
- Каждая смена — одна условная запись (`UPDATE … WHERE state = 'ringing'`), затем правка сообщения в обоих ящиках новым RPC `msg_editConferenceInvite` (по образцу `msg_closePoll`, `app/messenger/msg/msg/internal/core/polls.go:126-230`: своя правка и обновление в ящике пригласившего, правка ящика адресата через `inbox`; обновление здесь — `updateEditMessage`): `active`, `missed` или `duration`; `edit_date` не ставится (служебное сообщение). Правка уходит всем устройствам обоих — так гаснет звонок на остальных устройствах адресата и «звоним» у пригласившего.
- Порядок при входе адресата: сначала вход (то, что важнее), потом правка; сбой правки пишется в журнал и повторяется уборщиком (как `post`, `messages.go`).
- Просрочка: уборщик `groupcalls` (`sweeper.go`, тот же проход) переводит `ringing` с `ring_until < now` в `missed`, шлёт правку и Android-пуш `CONF_CALL_MISSED`.
- Конец конференции: `active` → `ended` с `duration` = длительность конференции (решение 6), `ringing` → `missed`.
- Повтор правки безопасен: сообщение получает то же действие.

### 5. Конец конференции
- Создатель завершает для всех (`discardGroupCall`).
- Пустая конференция (никого `in_call`) кончается сама: через 5 минут после выхода последнего, если в ней кто-то был; через 24 часа после создания, если никто так и не вошёл (ссылка заранее). Уборщик; решение 8.
- Конец: `groupCallDiscarded` бывшим участникам (`updateGroupCall`), комната SFU закрыта, приглашения по п. 4. Slug кончившейся конференции продолжает находить её в `getGroupCall` (`groupCallDiscarded`) 7 дней, вход — `GROUPCALL_ALREADY_DISCARDED`; потом — `GROUPCALL_INVALID`.

### 6. Пуши приглашения
- Новый вид `KindConferenceInvite` в `app/messenger/push/internal/notify` по новому входящему сообщению с `messageActionConferenceCall` без `active`/`missed`/`duration` в личном чате, и `KindConferenceMissed` по правке в `missed` (Android).
  - Android (FCM, данные): `loc_key` `CONF_CALL_REQUEST` или `CONF_VIDEOCALL_REQUEST`, `custom.call_id`, `custom.msg_id`, `custom.from_id`, `loc_args = [имя пригласившего, имя пригласившего, имена: пригласивший и other_participants]` (`PushListenerController.java:367-384` читает `[2]`); `CONF_CALL_MISSED` — `loc_args = [имя, число остальных]`, `custom.call_id` (`:1407-1415`).
  - iOS: устройству с токеном VoIP — PushKit, `p` с `from_id`, `group_call_id`, `msg_id`, `from_title`, `video`, `member_count` (`AppDelegate.swift:2345`); остальным — пуш расширения (alert) с `loc-key`, `from_id`, `call_id`, `msg_id`, `call_conference_from`, `call_participants_cnt` (`NotificationService.swift:1095-1113`). Один канал на устройство (`oneChannelPerDevice`, `push/internal/core/core.go:286-305`), срок жизни как у звонка (`callExpiration`).
  - Номер телефона в пуш не кладётся (iOS берёт его из своей базы, `AppDelegate`; правило журналов и пушей проекта).
- Авторизации с `call_requests_disabled`: пуши звонков один на один сервер им шлёт как всем, отсекает клиент (Android `VoIPGroupNotification.java:138-169`); для приглашений — так же (решение 12).
- Desktop и Web K пушей не получают: звонят по сообщению в открытом соединении (у Desktop без соединения звонка нет — как у звонков один на один).

### 7. Сквозное шифрование
- Без новых правил: ADR 0060 §2 действует целиком — проверки блоков, политика входа, ключ один на вход, повторное использование ключа другим пользователем — `BLOCK_INVALID`, удаление ушедших `only_left`, сверка эмодзи в подцепочке 1, пределы (состав 100, блок 256 КиБ).
- Кто пишет в подцепочку 0 конференции: создатель при создании с `join` (нулевой блок); каждый входящий — свой блок входа (первый на пустой цепочке — нулевой); участник — блок удаления ушедших; создатель — блок исключения. Приглашение и ссылка блоков не пишут: приглашённый входит сам своим ключом, право входа даёт политика п. 2.
- Подцепочка 1 — любой участник (`sendConferenceCallBroadcast`), как сейчас.
- Ответ на вход и рассылки цепочки — с каноническим звонком (iOS и Web K, раздел «Три формы»).
- Несквозного входа нет: `joinGroupCall` и `createConferenceCall(join)` без ключа и блока — `GROUPCALL_FORBIDDEN`. Клиенты и без того входят в конференцию только сквозными: Android `public_key`/`block` при `conference != null` (`VoIPService.java:2421-2425`), iOS — с `keyPair` (`PresentationCallManager.swift:1110-1150`), Desktop — `GroupCall::e2e()` (патч D-096), Web K — без `RTCRtpScriptTransform` конференцию не открыть (`TWEB/environment/conferenceCallSupport.ts:18-27`).
- Сервер ключа не видит; подставного участника видно в списке и по эмодзи — как в ADR 0059/0060.
- Патчи задачи 094 уже действуют и для конференций: повторный вход по `CONF_WRITE_CHAIN_INVALID`, отчёты `PlumaE2EStatus` (Android `patch.py:6146-6209`, iOS `patch.py:5989-6125`, Desktop D-096…D-101, Web K `patch.py:2489-2700`). Не проверено: самовосстановление iOS при ссылке `.message`, когда приглашение уже `missed` или удалено (часть 9).

### 8. Россия и мир
- Вход в конференцию — тот же `connectJoined` с `routes()` по стране адреса клиента (ADR 0074): из России и Беларуси — кандидаты трубы amf, остальным — прямые. `createConferenceCall` с `join` и `joinGroupCallPresentation` идут тем же кодом. Новых переменных, правил узла и маршрутов нет; e2e это проверяет (часть 9).
- Сигнализация (создание, приглашения, правки, цепочка) — по MTProto тем путём, каким клиент ходит к серверу (прямо, край `ru`, край Gcore).
- Ссылки — `pluma.chat` всем (раздел «Ссылки»); их разбор в приложении сети не требует.
- Пуши FCM и APNs — как у всех уведомлений; из России они ходят так же, как пуши звонков один на один.

### 9. Пределы и приватность
- `conference_call_size_limit = 100` в app config (`teamgramd/bin/app_config.json` и выдача `help.getAppConfig`), тот же предел — на сервере (участников в звонке и в цепочке, ADR 0060 §2).
- Живых конференций на создателя — 20; приглашений — 30 в минуту на пользователя; `other_participants` — до 10.
- Приватность: приглашение — по правилу «звонки» адресата и блокировке, как `phone.requestCall`; вход по ссылке приватностью не ограничен (ссылку дают сознательно). `USER_PRIVACY_RESTRICTED` даёт Android повод предложить ссылку (`VoIPService.java:2656-2700`).
- Журналы: id звонков и пользователей, состояния; ни slug, ни ключей, ни блоков, ни номеров (slug — пропуск в звонок, как хэш приглашения). `tools/logaudit` дополняется правилом на slug, если его ловить нечем.

### 10. Наблюдение
- Метрики `groupcalls`: `pluma_conference_created_total{join}`, `pluma_conference_invites_total{result}` (`ringing`, `privacy`, `kicked`, `already`, `flood`), `pluma_conference_invite_transitions_total{to}` (`active`, `missed`, `ended`), `pluma_conference_invite_edit_failures_total`, живые конференции (gauge). Тревога — рост `edit_failures` (звонки не гаснут).

## Части работы и порядок
1. **Хранение и `pkg/groupcalls`.** Миграция (раздел 1), `Call.Conference`, `ConferenceStore`, приглашения, `kicked`, `discard_slug`; интеграционные тесты MySQL.
2. **Политика и разрешение ссылок.** `policy` (чат / конференция), `resolve` трёх форм во всех методах раздела 2, канонический звонок в ответах; рассылка обновлений участникам конференции (`deliver` без чата).
3. **Создание и вход.** `createConferenceCall` (без `join` и с `join`), `groupCall` конференции (`conference`, `creator`, `invite_link`), `exportGroupCallInvite`, `toggleGroupCallSettings(reset_invite_hash)`, `getGroupCall` с `groupCallDiscarded`, предел состава, исключение и повторный вход, конец пустой конференции (уборщик).
4. **Приглашения.** `inviteConferenceCallParticipant`, `declineConferenceCallInvite`, `msg_editConferenceInvite` в `app/messenger/msg` (rpc, `make gen`), состояния и правки, просрочка в уборщике, удаление приглашения пригласившим.
5. **Пуши.** `KindConferenceInvite`, `KindConferenceMissed` (Android, iOS PushKit и расширение), тесты форм пушей по образцу `call_test.go`.
6. **Перевод звонка один на один.** `conference_supported`, причина перевода со slug в `voipcalls` и `pkg/phonecalls`, сообщение истории.
7. **Поиск и app config.** Патч `third_party(proto):` в `GetMediaType` (`// pluma:`, ADR 0009) — `messageActionConferenceCall` → `MEDIA_PHONE_CALL`; «пропущенные» в `pkg/msgsearch/kind.go` — ещё и `$.action.missed`; `conference_call_size_limit` в app config.
8. **Страница ссылки.** `KindConferenceCall` в `app/interface/links` (`call/<slug>` → `plm://call?slug=`, текст ключами во всех языках сайта), на `pluma.chat` и зеркале. По возможности — превью `telegram_call` (если не выходит без выдумки — в «Не сделано»).
9. **Регистрация и метод.** `RPCConferenceCalls` уже зарегистрирован (бриф 094); убрать три метода из `not_served.go`, `go run ./tools/apicoverage`.
10. **e2e и документы.** Тесты раздела «Тесты», `docs/ops/group-calls.md` (конференции, пределы, неполадки), `TESTING-BACKLOG.md` (проверки владельца), STATUS, api-coverage. Клиентских патчей нет (раздел «Клиенты»).

## Клиенты
- **Патчей не нужно** ни для продукта, ни для ссылок: весь путь конференций — апстрим, сквозной в любом клиенте; ссылки `pluma.chat/call/` уже внутри приложения (раздел «Ссылки»); маршрут по стране — в ответе сервера.
- Проверки патчей, которые могут сработать при будущих обновлениях апстрима, уже есть: Desktop `verify_group_calls_e2e()` останавливается на новом пути создания или входа (`clients/patches/desktop.md:75`), iOS `verify_group_calls` ждёт ровно три `PresentationGroupCallImpl` — «scheduled, joined, conference» (`ci/patch.py:7330-7340`).
- Если владелец захочет, чтобы ссылки `pluma.sckra.dev/call/` открывались прямо в приложении, это отдельная правка всех четырёх клиентов (хост, app links, `assetlinks.json`/AASA на зеркале) — решение 2, не в задаче.

## Тесты
- **unit:** разбор трёх форм `InputGroupCall` (чужое сообщение, исходящее, входящее, сброшенный slug); slug (длина, алфавит Web K и tdesktop); `groupCall` конференции (флаги для создателя и остальных, без `peer`); переходы приглашения (каждый путь, повтор, гонка «вход против просрочки»); формы пушей Android и iOS (ключи, `loc_args`, PushKit против расширения); причина перевода (живая конференция, чужая, пустой slug); `GetMediaType` и фильтр пропущенных.
- **интеграционные (MySQL, Kafka где `msg`):** идемпотентное создание; вход по slug и приглашению с проверкой политики; `kicked`; приглашение → сообщения в обоих ящиках → правки (`updateEditMessage` обоим); просрочка уборщиком; конец пустой конференции; `messages.search(inputMessagesFilterPhoneCalls, missed)` находит конференции; `deletePhoneCallHistory` их удаляет.
- **e2e (`test/e2e`, `pkg/mtclient`, tde2e тестового клиента ADR 0060 §6):**
  - `TestConferenceLinkOnly`: A создаёт без `join`, получает `invite_link` `https://<TDotMe>/call/<slug>`; B по `inputGroupCallSlug` — `getGroupCall`, вершина (пусто), нулевой блок, вход; C входит вторым; звук в обе стороны, эмодзи совпадают у всех; ответы с каноническим звонком.
  - `TestConferenceCreateJoin`: создать с `join` — ответ как у входа (обе подцепочки, версия 2).
  - `TestConferenceInviteRings`: A в конференции приглашает B (два устройства B): у обоих устройств B — `updateNewMessage` без флагов; B принимает на одном (`inputGroupCallInviteMessage`) — второе устройство B и A получают правку `active`; приглашение C — отказ — `missed`; приглашение D без ответа — `missed` через `call_ring_timeout_ms` стенда (укороченный); отмена пригласившим — `missed`.
  - `TestConferenceInvitePrivacy`: D запретил звонки — `USER_PRIVACY_RESTRICTED`; исключённый — `USER_WAS_KICKED`; уже в звонке — `USER_ALREADY_PARTICIPANT`.
  - `TestConferenceRevokeLink`: создатель сбрасывает — старый slug `GROUPCALL_INVALID`, новый работает; не создатель — `GROUPCALL_FORBIDDEN` (решение 11).
  - `TestConferenceKick`: создатель исключает B блоком `kick`; B не входит ни по ссылке, ни по приглашению участника; входит после приглашения создателя.
  - `TestConferenceMigrate`: звонок один на один A↔B принят (`conference_supported` у обоих), A создаёт конференцию, сбрасывает звонок с причиной перевода — B получает `phoneCallDiscarded` с тем же slug и входит по нему.
  - `TestConferenceFromRussia`: участник «из России» (`countryClients`, через край Gcore) создаёт и входит — кандидаты трубы; участник из Германии — прямые; слышат друг друга (`sfu_connections{route}`).
  - `TestConferenceEnds`: все вышли — через укороченный срок `groupCallDiscarded`, приглашение `active` получает `duration`.
  - Пуши — через тестовый поставщик пушей стенда, если он есть; иначе unit-тест форм и интеграционный тест выбора устройств.
- Хотя бы один тест проверяется падением без изменения (вход по slug при нынешнем `load` — `GROUPCALL_INVALID`).

## Проверки владельца (в `TESTING-BACKLOG.md`)
1. **Android:** «Звонки» → новый звонок с двумя контактами; у них звонит (экран блокировки, приложение закрыто); один принимает, второй отклоняет; у пригласившего «звоним» сменяется. Пропущенная конференция видна во вкладке «Звонки».
2. **iOS:** то же; звонок через CallKit при закрытом приложении (PushKit); приглашение, принятое на другом устройстве, гаснет на айфоне.
3. **Desktop:** «Создать звонок» → ссылка; ссылку открыть на другом клиенте — вход с предпросмотром; сбросить ссылку — старая не работает.
4. **Web K:** войти по ссылке `pluma.chat/call/…` из чата; приглашение звонит в открытой вкладке.
5. **Перевод:** звонок один на один Android↔iPhone, «добавить» → третий человек; собеседник переходит сам; то же с Desktop.
6. **Россия:** участник из России без VPN (дом и мобильная сеть) в конференции с участником из Германии — слышат и видят друг друга; ссылка из чата открывается в приложении.
7. **Эмодзи** совпадают у всех участников; исключённый создателем не может вернуться по ссылке.
8. Ссылка, открытая в браузере (`pluma.chat/call/…` и `pluma.sckra.dev/call/…`), — страница с кнопкой, переход в приложение.

## Решения к подтверждению
1. **Конференция — звонок без чата в тех же таблицах и коде**, со своей политикой доступа (ADR 0075 п. 1).
2. **Ссылки всегда на `pluma.chat`**, и клиентам из России: внутри приложения работают везде; вне приложения — страница службы ссылок на `pluma.chat` и на зеркале `pluma.sckra.dev`. Правило для `pluma.sckra.dev` в клиентах не делается.
3. **Ссылка не истекает**, пока конференция жива; сбрасывает только создатель; уже вошедших сброс не выгоняет.
4. **`conference_supported` — всегда** (у всех клиентов Pluma конференции есть). Правило Telegram не известно.
5. **Приглашение — по приватности «звонки» адресата и блокировке**, как обычный звонок; вход по ссылке приватностью не ограничен.
6. **Состояния сообщения:** вход адресата — `active`; отказ, отмена, 90 с без ответа — `missed`; конец конференции — `duration` = длительность конференции (не время адресата в ней). Точный смысл у Telegram не известен.
7. **Исключать может только создатель**; исключённый не возвращается ни по ссылке, ни по приглашению участника, пока создатель сам не пригласит.
8. **Конец пустой конференции:** 5 минут после выхода последнего, 24 часа, если никто не вошёл; slug кончившейся показывает «звонок завершён» 7 дней.
9. **Пределы:** 100 участников (`conference_call_size_limit`), 20 живых конференций на создателя, 30 приглашений в минуту, `other_participants` — до 10.
10. **Ошибка сверх 20 живых конференций — свой код `CONFERENCE_LIMIT_EXCEEDED`** (клиенты покажут общую ошибку). `FLOOD_WAIT_X` не годится: конференции не освобождаются по часам. Код Telegram не известен; другой вариант — `GROUPCALL_FORBIDDEN`.
11. **Методы, не имеющие смысла у конференции** (`editGroupCallTitle`, `inviteToGroupCall`, расписание, `join_muted`, `messages_enabled`), и действия создателя от не создателя: `GROUPCALL_FORBIDDEN` (клиенты их у конференции не показывают; код ошибки у Telegram не известен).
12. **Пуши приглашений идут и авторизациям с `call_requests_disabled`**, как пуши обычных звонков; отсекает клиент.

## Альтернативы
- **А. Отдельные таблицы и код конференций.** Дублирование входа, цепочки, SFU, уборщика. Отклонено.
- **Б. Ссылки на зеркале для России** (`pluma.sckra.dev/call/`). Ни один клиент не знает этого имени; правка четырёх клиентов и app links ради ссылки, которая разбирается без сети. Отклонено (может стать отдельной задачей по решению 2).
- **В. Приглашение через `updatePhoneCall`.** Клиенты так приглашения не принимают. Отклонено.
- **Г. Только пуш без правок сообщения.** iOS и Desktop не гасят звонок. Отклонено.
- **Д. Звонок чата пометить `conference`.** Ломает интерфейс звонка чата в tdesktop и Web K (ADR 0060 «Стоковые клиенты»). Отклонено.

## Не проверено
- Поведение сервера Telegram в целом: исходников нет, всё выведено из клиентов (решения 3–12).
- Когда Telegram ставит `conference_supported` и `inline_conference`.
- Гаснет ли звонок у адресата на Android и iOS, когда приглашение удаляют, а не правят.
- Самовосстановление iOS (патч задачи 094) при ссылке `.message`, когда приглашение уже `missed` или удалено.
- Нужен ли iOS отдельный entitlement для `reportNewIncomingVoIPPushPayload` из расширения (звонки один на один идут тем же путём — вероятно, нет).
- Открываются ли универсальные ссылки `pluma.chat` вне приложения в России, где имя заблокировано (поведение CDN AASA Apple не читалось).
- Умеет ли наше превью ссылок тип `telegram_call` (Web K «JoinCall»).
- Пуши FCM/APNs из России для приглашений — как для звонков один на один, отдельно не замерялись.

## Не в задаче
- Сообщения в звонке (`sendGroupCallEncryptedMessage`) — отдельный этап (ADR 0060).
- Запись, трансляции, вход от имени канала.
- Правило ссылок `pluma.sckra.dev` в клиентах (решение 2).
- Старая форма `createConferenceCall#dfc909ab` (`= phone.PhoneCall`), которую клиенты layer 233 не шлют.

## Результат
Серверная часть сделана на ветке `task/117-conference-calls` (не выложена, не отправлена): конференции обслуживаются целиком по дизайну выше — создание ссылкой и с входом, три формы `InputGroupCall` во всех `phone.*`, политика конференции, ссылка `https://pluma.chat/call/<slug>` и её сброс создателем, приглашения, которые звонят (служебное сообщение, правки в обоих ящиках, пуши Android и iOS), отказ, отмена, пропуск по таймауту, исключение и возврат по приглашению создателя, конец создателем и конец пустой конференции, перевод звонка один на один (`conference_supported`, причина со slug), поиск пропущенных, `conference_call_size_limit`, страница ссылки на `pluma.chat` и зеркале. Каждая конференция сквозная (ADR 0060 §2 без исключений); из России вход идёт через трубу amf тем же `routes()` (проверено e2e, в том числе создание с входом). Клиентских патчей не нужно.

**Коммиты** (после двух коммитов дизайна): `69ceff115` хранение (`migrate-20261126-conference-calls.sql`, `pkg/groupcalls/conference.go`, `invites.go`, `joinTx`); `beb743635` bff (`conference.go`, `invites.go`, политика в `access`, `load` трёх форм, ветки конференции в обработчиках и сборщике); `9e055995d` пуши; `f87f0eac7` перевод звонка один на один (`pkg/phonecalls`, `voipcalls`); `a55d6c1e4` `third_party(proto):` `GetMediaType`; `db42af238` поиск пропущенных и app config; `6fcf1b7a3` страница `links`; `e499907a3` e2e; `bdb56a3c4` logaudit; `msgsearch: format…` — формат теста; последний коммит — документы и api-coverage.

**Проверка** (всё на CachyOS): `go build ./...` — зелёный; `go vet -tags integration,e2e -unreachable=false` затронутых пакетов и `test/e2e` — чисто; `go test -race` затронутых пакетов и импортёров (`pkg/groupcalls`, `pkg/phonecalls`, `pkg/msgsearch`, `app/bff/groupcalls/...`, `app/bff/voipcalls/...`, `app/bff/bff/...`, `app/messenger/push/...`, `app/interface/links/...`, `app/bff/configuration/...`, `tools/pluma-admin`) — все ok; `third_party/teamgram-proto/mtproto` — ok; `go test ./tools/...` (с logaudit) — ok; golangci-lint по диффу ветки (в копии на CachyOS нет истории git для `new-from-rev`, поэтому `--new-from-patch` с диффом от `a9e666b80`) — 0 замечаний в обоих модулях, `tunnel_test.sh` — ok. Интеграционные: `pkg/groupcalls` (создание, повтор, предел 20, создание с входом в одной транзакции, исключение, сброс slug, конец пустой, переходы приглашений), bff `TestConferenceFlow` и `TestConferenceInviteRingsOut` (три формы, права создателя, приглашения и правки, отказ, исключение, повтор правки сборщиком), `pkg/phonecalls` (`TestDiscardMigrateKeepsSlug`), `pkg/msgsearch` (`TestSearchCalls`) — ok. e2e на стенде `pluma-117`: `TestConferenceLinkOnly`, `CreateJoin`, `InviteRings` (два устройства приглашённого, пропуск через 90 с), `InvitePrivacy`, `RevokeLink`, `Kick`, `Migrate`, `FromRussia`, `Ends`, `InvitePush`, и все `TestGroupCall*`, `TestCall*` — PASS; стенд снят (`run.sh down`). Тест, падающий без изменения: `TestSearchCalls` без нового фильтра — «missed calls: [2], want [4 2]»; прежний `TestGroupCallChain` ждал `ErrEnterpriseIsBlocked` от `createConferenceCall` и переписан на старую форму `#dfc909ab`.

**Отклонения от дизайна (к подтверждению):**
- Политика — тот же тип `access` с веткой `conf` (`conferenceAccess`), а не отдельный интерфейс: обработчики спрашивают одно и то же (`canView`, `canManage`, `members`, `chatFor`), а код звонков чатов почти не тронут.
- Правка приглашения идёт существующим `msg.editMessageV2` (правка пригласившим своего сообщения, `auth_key_id` 0 — всем устройствам, копия адресата — через inbox), а не новым RPC `msg_editConferenceInvite`: он делает ровно это, `make gen` не нужен.
- Повторное приглашение, пока прежнее звонит: прежнее становится «пропущено», новое звонит (вместо ответа прежним сообщением — его нельзя вернуть второй раз без нового pts).
- `declineConferenceCallInvite`: пригласившему ответ несёт его `updateEditMessage`; адресату — пустой `Updates`, правка приходит ему по ящику всем устройствам.
- Удаление приглашения пригласившим отдельно не обрабатывается: звонящее приглашение уходит в «пропущено» по таймауту, правка видит, что сообщения нет, и считает его показанным (до 90 с позже, чем в дизайне).
- Метрики — в пространстве `groupcalls` (`groupcalls_conferences_total`, `groupcalls_conferences_live`, `groupcalls_conference_invites_total`), как остальные метрики модуля, а не `pluma_conference_*`.
- Сроки (звонок приглашения 90 с, конец пустой 5 мин / 24 ч, slug завершённой 7 дней, 20 конференций, 30 приглашений в минуту) — константы `pkg/groupcalls`, без переменных окружения; конец пустой конференции проверен интеграционным тестом, e2e `TestConferenceEnds` проверяет конец создателем.
- Пропуск приглашения в e2e проверяется по `updates.getDifference` устройства: после 90 с простоя тестовый клиент без пингов перестаёт получать пуши (сервер правку делает — видно в базе и в разнице).

**Не сделано:** превью ссылки `telegram_call` (кнопка «JoinCall» в Web K) — не проверено, умеет ли наше превью такой тип; `STATUS.md` не обновлялся (его ведёт основная сессия).

**Не проверено:** на живых клиентах ничего (список — `TESTING-BACKLOG.md`, «Конференции»); пуш `CONF_CALL_MISSED` — только unit; гаснет ли звонок у адресата на Android и iOS при удалении приглашения; самовосстановление iOS по ссылке `.message` на пропущенное приглашение; открытие `pluma.chat/call/` вне приложения в России.
