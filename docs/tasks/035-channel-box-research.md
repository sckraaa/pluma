# 035. Каналы: сверка с Telegram и ADR «Ящик канала»

- Статус: in-progress (2026-11-03: ADR 0034 написан, U1–U10 и C1–C5 закрыты по исходникам и документации; ждёт ответов владельца и сверки с Telegram для эталонов)
- Зависит от: —
- ADR: 0011, 0012, 0018, 0019, 0020, 0021, 0022; результат — ADR 0034 «Ящик канала». Исследование: `docs/tasks/035-research/` (`server.md` — сервер сегодня и целевая модель, `clients.md` — что ждут клиенты, `plan.md` — выборочная проверка обоих отчётов и разбивка этапа; номера задач в `plan.md` на единицу меньше здешних, соответствие — в `plan-ru.md`).

## Цель
До первой строки кода этапа 1 закрыть открытые вопросы о поведении Telegram и клиентов и записать модель хранения каналов в ADR, чтобы задачи 036–049 опирались на прочитанные источники, а не на догадки.

## Что известно
- **Исходники клиентов не сверены с закреплёнными релизами.** `upstream.env` нашёлся только у Web K (`~/pluma-web/upstream.env`, `346844bf`, слой 228, версия 2.2 (676)). Для iOS, Android и tdesktop закрепления на машине нет, в `clients/*.md` тоже (`plan.md`, §1). Отчёты читали разные деревья:
  - iOS: локальное `~/aaaaaaa/Telegram-iOS` `756315d7` (`server.md`) и `6ad963e5` через `gh api` (`clients.md`);
  - Android: локальное `~/aaaaaaa/Telegram-Android` `20091c78`, 12.6.4;
  - tdesktop: тег v7.0.9.
  Совпадают ли они с тем, что собирает Pluma, — UNVERIFIED.
- **Противоречия между отчётами** (`plan.md`, §2):
  - **C1.** Таймаут по умолчанию в `channelDifference*`: core.telegram.org/api/updates — 1 с; iOS без флага ждёт 30 с (`AccountStateManagementUtils.swift:3143-3149`); tdesktop и Android — 1 с (по `clients.md`). Решение плана: сервер всегда шлёт явный `timeout > 0`; значение — из сверки с Telegram.
  - **C2.** `toggleJoinRequest` и `toggleJoinToSend` в `clients.md` названы «уже обслуживаются»; по `docs/project/api-coverage.md:339-340` они `blocked`. Верно второе.
  - **C3.** Строки iOS `pollChannel`: `:3413` (`756315d7`) и `:3433` (`6ad963e5`) — два разных коммита, ни один не сверен с закреплением.
  - **C4.** Как iOS догоняет каналы после `getDifference`: по `server.md` — `updateChannelTooLong` → `pollChannel`; по `clients.md` — сброс каналов через `messages.getPeerDialogs` (`AccountStateManagementUtils.swift:2042-2070`, `resetChannels` `:3226-3240`). Эти строки в выборочной проверке не читались (UNVERIFIED). Если верно второе, `getPeerDialogs` с правильными `dialog.pts`, состоянием прочтения и верхним сообщением нужен уже в 038.
  - **C5.** iOS повторяет `getChannelDifference` бесконечно при любой ошибке, кроме `CHANNEL_PRIVATE`/`CHANNEL_INVALID` (`AccountStateManagementUtils.swift:3448-3458`, `Network/Network.swift:1240-1243`). Правило проекта «неподдержанное — `ErrEnterpriseIsBlocked`» этот метод не спасает: его нужно реализовать (040) до того, как каналы появятся у пользователей.
- **Не проверено, закрыть в этой задаче** (`plan.md`, §2):
  - **U1.** Несут ли вступление, выход, исключение и `updateReadChannelInbox` общий `pts` или `seq` для своих устройств пользователя. UNVERIFIED.
  - **U2.** По какому признаку Telegram кладёт `updateChannelTooLong` в `getDifference`. UNVERIFIED.
  - **U3.** Имена ошибок: `SLOWMODE_WAIT_X`, ошибка отправки в перенесённую группу (`CHAT_MIGRATED` или другая), `RANDOM_ID_DUPLICATE`, `USER_BANNED_IN_CHANNEL`, ошибки вида `CHAT_SEND_MEDIA_FORBIDDEN`. UNVERIFIED.
  - **U4.** Как права админов группы переносятся при переходе в супергруппу; освобождены ли админы от медленного режима. UNVERIFIED.
  - **U5.** Что такое `read_outbox_max_id` в супергруппе. UNVERIFIED.
  - **U6.** Какие настройки базовой группы заставляют клиент запускать переход сам. UNVERIFIED.
  - **U7.** Всё о tdesktop сверх процитированных строк; поток создания канала в tdesktop не читался. UNVERIFIED.
  - **U8.** Совпадает ли форма ответа `messages.migrateChat` с тем, что разбирают Android, tdesktop и Web K. UNVERIFIED.
  - **U9.** Настоящие значения appConfig Telegram (`channels_limit_default` и другие); прочитаны только умолчания клиентов. UNVERIFIED.
  - **U10.** Строки `clients.md`, не перечитанные при проверке: Android `MessagesController`, Web K `apiUpdatesManager`, iOS `JoinChannel.swift:70-80`, iOS `UpdateCachedPeerData.swift`. UNVERIFIED.
- **Проверено по файлам** (`plan.md`, §1, выборочная проверка): маршруты `RPCChannels` и `RPCMessageThreads` закомментированы во всех трёх `session.yaml` (`teamgramd/etc2/session.yaml:82,104`, `teamgramd/etc/session.yaml:82,104`, `app/interface/session/etc/session.yaml:63,84`); `getDifference` отбрасывает канальные обновления (`app/service/biz/updates/internal/core/merge_updates_helper.go:148-153`); `ERR_ENTERPRISE_IS_BLOCKED` — 400 (`third_party/teamgram-proto/mtproto/rpc_error_codes.go:341`).
- **Модель, которую предлагает исследование** (`server.md`, §2.3–2.5): счётчики в строке `channels` (как `user_state`); журнал `channel_pts_updates` с окном 100000 pts (core.telegram.org/api/updates); рассылка участникам без журнала на каждого пользователя; непрочитанное считается при чтении; ключ Kafka — id канала (новый тип ключа, меняет ключ по получателю из ADR 0012); id сообщений последовательные внутри канала.

## Область
- Сверка с настоящим Telegram через Web K `invokeApi` в двух вкладках (как для 034): U1–U6, U8; таймаут, который Telegram отдаёт в `channelDifference*` (C1); имена ошибок (U3); содержимое `channelDifferenceTooLong`; ответ `messages.migrateChat`.
- Найти и записать закрепления iOS, Android и tdesktop (если их нет — вопрос владельцу, ниже), затем прочитать по ним: C3, C4, U7, U10; лимиты и путь tooLong в `getChannelDifference` у Android, tdesktop и Web K; разбор `migrateChat`.
- ADR «Ящик канала»:
  - счётчики (`pts`, `last_message_id`, количества) в строке `channels`, без idgen (ADR 0011, l.230, l.411);
  - журнал `channel_pts_updates` и его окно;
  - рассылка без журнала на пользователя; непрочитанное — при чтении;
  - ключ Kafka — id канала и как это соотносится с ADR 0012;
  - когда сервер шлёт `updateChannelTooLong`;
  - какие канальные события идут через общий ящик пользователя (`seq` или `pts`, ADR 0019–0022);
  - явный `timeout` в ответах `getChannelDifference`.
- Эталонные JSON для сравнительных сценариев (ADR 0018, `test/conformance/testdata/`): создание, отправка, `getChannelDifference`, `migrateChat`, права.
- Не трогаем: код сервера и клиентов.

## Вопросы владельцу (`plan.md`, §4)
1. Форумы и топики (≈3 дня) и журнал действий админов (`getAdminLog`, `reportSpam`, `toggleAntiSpam`, `convertToGigagroup`, ≈2 дня) — в этапе 1 или в этапе 2? ROADMAP относит форумы к этапу 1; в разбивке этапа их нет, пока владелец не решит.
2. Окно журнала канала: только 100000 pts или ещё и срок?
3. `channels_limit_default` и лимит публичных ссылок (у клиентов по умолчанию 500 и 10, `UserLimitsConfiguration.swift:41-42`); разрешать ли публичные каналы в закрытой установке.
4. Принять ли правило «каналы не выкатываются в прод до 040» или скрыть создание в форках клиентов до тех пор.
5. Добавить ли в репозиторий закрепления `upstream.env` для iOS, Android и tdesktop.

## Ответы владельца (2026-11-03)
1. Форумы с темами и журнал действий админов — в этапе 1.
2. Окно журнала канала — как у Telegram: 100000 `pts`, без срока.
3. Публичные каналы и группы разрешены, лимиты Telegram: 500 каналов и групп на пользователя, 10 публичных ссылок.
4. Каналы показываются пользователям после 040; до этого серверная часть выкатывается незаметно, клиенты не патчатся.
5. Закрепления клиентов — техническое решение, не владельца: записать `upstream.env` iOS, Android и tdesktop в репозиторий в 049.

## Критерии приёмки
- ADR «Ящик канала» принят и внесён в `docs/adr/README.md`.
- Каждый пункт U1–U10 и C1, C3, C4 закрыт со ссылкой на прочитанный источник или запрос к Telegram, либо явно отложен с указанием задачи, которая его закроет.
- Закрепления клиентов записаны; строки клиентов в брифах 036–049, которые разошлись с закреплённым деревом, исправлены.
- Эталонные JSON сохранены в `test/conformance/testdata/`, запрос, по которому получен каждый, описан.
- Ответы владельца на вопросы 1–5 записаны в этот бриф.
- Код не меняется; тестов нет.

## Ход работы
- 2026-11-03. **Закрепления клиентов найдены.** Они записаны в задаче 006 и в `upstream.env` клиентских репозиториев. Деревья `~/pluma-work/reference-src/*` с ними совпадают (`FETCH_HEAD` каждого дерева):
  - iOS `release-12.9.2`, `6ad963e5` (`versions.json`: app 12.9.2);
  - Android 12.9.1, `aac1efcb` (`~/pluma-android/upstream.env`, `gradle.properties:17`);
  - tdesktop v7.0.9, `a1e89e1f` (`core/version.h:26`);
  - Web K 2.2 (676), `346844bf` (`~/pluma-web/upstream.env`).
  Все строки клиентов ниже взяты из этих деревьев. Деревья `~/aaaaaaa/*` (iOS `756315d7`, Android `20091c78`), по которым писались отчёты, не закреплены.
- **Прочитаны страницы core.telegram.org:**
  - разделы `api/updates`, `api/channel`, `api/rights`, `api/config`;
  - методы `updates.getChannelDifference`, `messages.sendMessage`, `messages.sendMedia`, `messages.migrateChat`, `channels.createChannel`, `channels.toggleSlowMode`;
  - конструкторы `updates.channelDifferenceTooLong`, `updateChannelTooLong`, `updateReadChannelInbox`, `updateReadChannelOutbox`.
- **Сверки с Telegram не было:** запросы к настоящему Telegram через Web K `invokeApi` в этой задаче были недоступны. Что без неё осталось открытым, перечислено ниже и в ADR 0034 («Открытые вопросы»).

## Результат
Модель хранения каналов записана в ADR 0034 (статус proposed):
- **Хранение.** У канала одна копия сообщений. Свой `pts` и id подряд — в строке `channels`. Журнал `channel_pts_updates` держит окно в 100000 `pts`.
- **Состояние пользователя** — его строка `dialogs`. Непрочитанное, `top_message` и `read_outbox_max_id` вычисляются при чтении.
- **Транзакция** идёт под блокировкой строки канала, внутри `userbox.Run` того, кто действует.
- **Рассылка** — одна строка outbox на событие в новый топик `Channel-T` с ключом по каналу. Вид для каждого получателя строит консьюмер в sync.
- **Свои устройства** получают `updateChannel` и `updateReadChannelInbox` через журнал `seq`.
- **`getChannelDifference`** всегда отдаёт явный `timeout`: 30 с участнику, 10 с не участнику.
- **`updateChannelTooLong`** с `pts` получают каналы, изменившиеся после курсора ключа.

Все пункты U и C закрыты по закреплённым исходникам и документации или отложены с принятым решением. Задача не закрыта по трём критериям приёмки: нет эталонных JSON (нужна сверка с Telegram), нет ответов владельца на вопросы 1–5, ADR ещё не принят. Код не менялся.

### Ответы на U1–U10 и C1–C5
Сокращения: AMU — iOS `submodules/TelegramCore/Sources/State/AccountStateManagementUtils.swift`; MC — Android `TMessagesProj/src/main/java/org/telegram/messenger/MessagesController.java`; TD — tdesktop `Telegram/SourceFiles`; WK — Web K `src/lib/appManagers`.

- **C1. `timeout`.**
  - Документация: без флага — 1 с (core.telegram.org/api/updates).
  - iOS без флага ждёт 30 с, а с 0 опрашивает без паузы (AMU:3158-3163, `:3220-3224`).
  - tdesktop без флага или с 0 ждёт 1 с, и только для открытого чата (TD `api/api_updates.cpp:438-443`).
  - Android ставит таймер, только если флаг есть (MC:16762-16763).
  - Web K поле не читает (WK `apiUpdatesManager.ts:435-505`).
  - **Решение** (ADR 0034, п. 4): явный `timeout` всегда — 30 с участнику, 10 с не участнику. Значение Telegram не проверено.
- **C2.** `toggleJoinRequest` и `toggleJoinToSend` — `blocked` (`api-coverage.md:339-340`), как и записано в брифе.
- **C3.** Закреплён iOS `6ad963e5`. В нём `pollChannel` — AMU:3433-3734: запрос на `:3448`, ошибки `:3451-3458`, tooLong `:3626-3727`. Строки дерева `756315d7` в брифах заменены.
- **C4.** Верны оба отчёта, каждый для своего пути.
  - **После `getDifference`** iOS пропускает канал, если локальный `pts` не меньше `updateChannelTooLong.pts` (AMU:952-957). Остальные каналы он сбрасывает через `messages.getPeerDialogs`: асинхронно по одному каналу (`AccountStateManager.swift:642-704`) или одним запросом на все (AMU:2065-2070).
  - `resetChannels` берёт из `dialog` `top_message`, `read_inbox_max_id`, `read_outbox_max_id`, `unread_count`, счётчики упоминаний и реакций, `pts`, уведомления и папку; берёт и сообщения из ответа (AMU:3226-3431).
  - **Пришедший пушем** `updateChannelTooLong` ведёт к `pollChannel` (AMU:705, `:2076-2078`).
  - Вывод: `getPeerDialogs` с полным `dialog` нужен уже в 038.
- **C5.** Подтверждено. На любую ошибку, кроме `CHANNEL_PRIVATE` и `CHANNEL_INVALID`, iOS бесконечно повторяет запрос через `retryRequest` (AMU:3454-3458). Пауза растёт линейно от 0,2 до 5 с (`Network/Network.swift:1240-1243`, `SSignalKit/SwiftSignalKit/Source/Signal_Catch.swift:101-140`).
- **U1. Общий `pts` или `seq` для своих устройств.**
  - У Telegram не проверено.
  - `updateChannel` и `updateReadChannelInbox` внутри контейнера с `seq` клиенты принимают по правилам `seq`: iOS (`UpdateGroup.swift:206-235`, AMU:651-666), Android (MC:18231), tdesktop (TD `api_updates.cpp:1476-1485`).
  - Web K проводит `updateReadChannelInbox` через проверку `pts` канала с `pts_count = 0` (WK `apiUpdatesManager.ts:639-641`, `:696-724`).
  - **Решение** (ADR 0034, п. 3): оба обновления идут через журнал `seq` аккаунта (ADR 0020, 0021). `pts` в `updateReadChannelInbox` — текущий `pts` канала. Общий `pts` пользователя события каналов не трогают.
- **U2. Признак `updateChannelTooLong`.**
  - Документация говорит, что это обновление «не обязательно» означает переполнение окна (core.telegram.org/constructor/updateChannelTooLong). Точного правила нет, признак Telegram не проверен.
  - **Решение** (ADR 0034, п. 6): обновление получают каналы, где пользователь участник и которые изменились не раньше `min(date запроса, дата курсора ключа) − MaxRunTime`. `pts` в нём всегда. Клиенты пропускают уже догнанные каналы (MC:19231-19233, TD `api_updates.cpp:2484-2485`, AMU:952-957).
- **U3. Имена ошибок.**
  - По core.telegram.org/method/messages.sendMessage и messages.sendMedia:
    - `SLOWMODE_WAIT_%d` — 420;
    - `USER_BANNED_IN_CHANNEL` — 400;
    - `CHAT_SEND_*_FORBIDDEN`, в том числе `CHAT_SEND_MEDIA_FORBIDDEN`, — 403;
    - `CHAT_WRITE_FORBIDDEN` — 403;
    - `RANDOM_ID_DUPLICATE` — 500.
  - Ошибки для отправки в перенесённую группу в документации нет. `CHAT_MIGRATED` нет ни в документации, ни ни в одном из четырёх клиентов (искал по всем деревьям).
  - **Решение:** `CHAT_WRITE_FORBIDDEN`. Клиенты сами уходят в супергруппу по `chat.migrated_to`. Какое имя у Telegram, не проверено.
  - Где клиенты разбирают эти ошибки:
    - iOS — `PendingMessageManager.swift:89-108`;
    - Android — `ui/Components/AlertsCreator.java:471-513`, `jni/tgnet/ConnectionsManager.cpp:1398-1404`;
    - tdesktop — `apiwrap.cpp:530-551`, `mtproto/mtp_instance.cpp:1388`, `:1495-1496` (при X < 3 повторяет сам);
    - Web K — `appImManager.ts:539-540`, `apiManager.ts:744`.
- **U4. Права админов при переходе; медленный режим.**
  - **Права.** Документация о переносе прав молчит (core.telegram.org/api/channel, /api/rights). Правами админа базовой группы клиенты считают `other`, `change_info`, `delete_messages`, `ban_users`, `invite_users`, `pin_messages`, `manage_call` (TD `data/data_chat.cpp:52-63`); Android добавляет `manage_ranks` (`ui/ChatRightsEditActivity.java:1089-1091`).
  - **Решение по правам:** админы группы становятся админами канала с этим набором и `manage_ranks` (ADR 0034, п. 10). У Telegram не проверено.
  - **Медленный режим.** Создатель и любой админ им не ограничены во всех четырёх клиентах:
    - iOS — `ApiUtils/TelegramChannel.swift:302-312`;
    - tdesktop — TD `data/data_peer.cpp:1831-1837`;
    - Android — `messenger/ChatObject.java:2119-2120`, `ChatActivityEnterView.java:6186`;
    - Web K — WK `appMessagesManager.ts:9222-9233`.
  - **Решение по медленному режиму:** сервер создателя и админов не ограничивает.
- **U5. `read_outbox_max_id` в супергруппе.**
  - Документация: «Position up to which all outgoing messages are read» (/constructor/updateReadChannelOutbox). Что это значит в группе, не сказано; определение Telegram не проверено.
  - **Решение** (ADR 0034, пп. 1, 3): для зрителя это наибольший `read_inbox_max_id` других участников. Значение вычисляется при чтении. `updateReadChannelOutbox` уходит авторам пушем без `seq`.
- **U6. Какие действия переводят группу в супергруппу.**
  - Документация: клиенты переводят группу сами, когда админ делает то, что есть только у супергрупп (core.telegram.org/api/channel).
  - По клиентам, к `migrateChat` ведут:
    - **публичное имя:** iOS `PeerInfoUI/Sources/ChannelVisibilityController.swift:2050-2057`, Android `ui/ChatEditTypeActivity.java:1208`, TD `boxes/peers/edit_peer_info_box.cpp:2511`, WK `components/sidebarRight/tabs/chatType.tsx:206`;
    - **видимость истории для новых:** iOS `GroupPreHistorySetupController.swift:162-164`, Android `ui/ChatEditActivity.java:2091`, TD `edit_peer_info_box.cpp:2755`, WK `appChatsManager.ts:1133`;
    - **права админа не по умолчанию или звание:** iOS `ChannelAdminController.swift:1983-1990`, Android `ChatRightsEditActivity.java:1518`, TD `edit_participants_box.cpp:415`, WK `appChatsManager.ts:858`;
    - **ограничение участника:** iOS `ChannelBannedMemberController.swift:1031-1032`, Android `ChatRightsEditActivity.java:1518`, TD `edit_participants_box.cpp:497`, WK `appChatsManager.ts:988`;
    - **медленный режим:** iOS `ChannelPermissionsController.swift:1208-1224`, Android `ui/ChatUsersActivity.java:2282`, TD `edit_peer_info_box.cpp:418`, WK `appChatsManager.ts:1273`;
    - **привязка обсуждения:** iOS `ChannelDiscussionGroupSetupController.swift:364-365`, Android `ui/ChatLinkActivity.java:579`, TD `edit_discussion_link_box.cpp:199`, WK `appChatsManager.ts:1242`;
    - **форум:** iOS `ForumSettingsScreen.swift:199-203`, Android `ChatEditActivity.java:1128`, TD `edit_peer_info_box.cpp:2836`;
    - **заявки на вступление и «писать могут только участники»:** Android `ChatEditTypeActivity.java:748` и `ChatLinkActivity.java:911`, WK `appChatsManager.ts:1133`, `:1174`;
    - **запрет пересылки**, только Android: `ChatEditTypeActivity.java:1171`;
    - **передача владения:** iOS `ChannelOwnershipTransferController.swift:97-98`, Android `ChatRightsEditActivity.java:1107`;
    - **импорт истории:** iOS `ChatImportActivityScreen.swift:843`, Android `SendMessagesHelper.java:8909`.
  - По числу участников группу не переводит ни один клиент. Серверный автопереход не нужен.
- **U7. tdesktop.**
  - **Создание канала** (`boxes/add_contact_box.cpp:822-905`):
    - `createChannel` ждёт в ответе `updates` или `updatesCombined`, где канал стоит первым в `chats`. Иначе клиент пишет в лог «channel not found in updates» и закрывает окно (`:847-868`, `:887-891`);
    - затем зовёт `getFullChannel` (`:879`) и `exportChatInvite`, если ссылки ещё нет (`:907-917`);
    - разбирает ошибки `NO_CHAT_TITLE`, `USER_RESTRICTED`, `CHANNELS_TOO_MUCH` (`:896-903`).
  - **Разница канала** (`api/api_updates.cpp`):
    - `limit` — 100 (`:79`);
    - `force` ставится всегда, кроме обычного запроса без ожидания пропущенных обновлений (`:737-741`);
    - `pts` должен быть инициализирован (`:726`);
    - при сбое повтор с паузой от 1 до 64 с (`:576-590`);
    - на tooLong — `ptsInit(dialog.pts)`, `applyDialogs` и разница по диапазону (`:403-425`, `:791-884`);
    - `updateChannelTooLong` для незагруженного канала игнорируется (`:2480-2488`).
- **U8. Ответ `migrateChat`.**
  - **iOS** берёт первое сообщение не из старой группы и ждёт его 5 с (`TelegramEngine/Peers/ConvertGroupToSupergroup.swift:13-29`).
  - **Android** ищет в `chats` старую группу с `migrated_to` и сам канал (MC:15103-15122).
  - **tdesktop** после `applyUpdates` берёт `migrated_to` из `chats` (`apiwrap.cpp:1387-1391`, `data/data_session.cpp:889-896`) и зовёт `getFullChannel`.
  - **Web K** ищет `updateChannel` в `updates`; без него — TypeError (WK `appChatsManager.ts:784-786`).
  - Ответ, который устраивает всех четверых, описан в ADR 0034, п. 10.
  - Ошибки метода (core.telegram.org/method/messages.migrateChat): `CHANNELS_TOO_MUCH`, `CHAT_ADMIN_REQUIRED`, `CHAT_ID_INVALID`, `PEER_ID_INVALID`.
- **U9. appConfig.**
  - Значения по core.telegram.org/api/config: `channels_limit_default` 500 (`_premium` 1000), `channels_public_limit_default` 10 (`_premium` 20), `hidden_members_group_size_min` 100, `chat_read_mark_size_threshold` 100.
  - Наш `teamgramd/bin/app_config.json` (задача 015) уже отдаёт 500 и 500, 10 и 10: Premium равен бесплатному (ADR 0016).
  - Живые значения настоящего Telegram не сняты. Для решений они не нужны: лимиты закрытой установки решает владелец (вопрос 3).
- **U10. Строки `clients.md`.**
  - **iOS.** `JoinChannel.swift:70-81` подтверждено: после вступления клиент зовёт `getParticipant(self)`, при сбое показывает `.generic`. В `UpdateCachedPeerData.swift` запросы — `:657-680`, поля — `:697-935`.
  - **Android.** Строки версии 12.6.4 разошлись с 12.9.1. Верные строки:
    - `getChannelDifference` — MC:16525-16794;
    - `updateChannelTooLong` — MC:19206-19238;
    - `updateChannel` — MC:20146-20173;
    - `loadUnknownChannel` — MC:16380-16441;
    - разрыв `pts` — MC:18171-18210, `:16256-16316`;
    - `processNewChannelDifferenceParams` — MC:8171-8213;
    - создание канала — MC:15036-15069;
    - `loadFullChat` — MC:7673-7677;
    - засев `pts` из `getHistory` — MC:11892-11906;
    - ошибки канала — MC:16798-16806;
    - просмотры — MC:10579-10630.
    - `channel_color_level_min` в закреплённом Android не найден.
  - **Web K.** Подтверждено, со сдвигами строк:
    - `apiUpdatesManager.ts:451-458`, `:632-637`, `:658-665`, `:667-691`, `:696-724`;
    - `appMessagesManager.ts:10849-10865`, `:10867-10878`, `:12531-12533`, `:12545-12573`;
    - `appChatsManager.ts:681-693`;
    - `appChatInvitesManager.ts:91-102`;
    - `apiManager.ts` лежит в `src/lib/appManagers/`, а не в `mtproto/`.
  - **Найдено сверх отчётов:**
    - `updateChannelParticipant` не обрабатывают Android, iOS и tdesktop; Web K только сбрасывает кэш (`appChatsManager.ts:1419-1422`);
    - Android падает на `channelDifferenceTooLong` с `dialog = null` (`MessagesStorage.java:11321`, вывод из кода);
    - Web K на `updateChannelTooLong` не сравнивает `pts` и запрашивает разницу почти всегда (`apiUpdatesManager.ts:658-665`).

### Решения на подтверждение владельцем
1. Рассылка идёт через новый топик `Channel-T` с ключом по каналу, а не N строками outbox с ключом по получателю (ADR 0034, п. 5).
2. `timeout`: 30 с участнику и 10 с не участнику — до сверки с Telegram.
3. Отправка в перенесённую группу получает `CHAT_WRITE_FORBIDDEN`. Перенесённые админы получают набор прав из п. 10.
4. Непрочитанное и `top_message` каналов вычисляются при чтении, копий у получателей нет.

### Не сделано
- **Эталонные JSON** в `test/conformance/testdata/` не сняты: для них нужна сверка с настоящим Telegram через Web K `invokeApi`. Что снять, перечислено в ADR 0034 («Открытые вопросы»). Снять до 040 (`timeout`, tooLong) и не позже 049.
- **Ответы владельца на вопросы 1–5** не получены. По вопросу 5: закрепления есть (задача 006, `upstream.env` в `~/pluma-android` и `~/pluma-web`), но в этом репозитории их нет. Для iOS и tdesktop `upstream.env` на машине нет; их закрепление видно только по задаче 006 и по `FETCH_HEAD` деревьев.

### Изменения в брифах 036–049
- Ссылка «“Ящик канала” (из 035)» в заголовках «ADR» заменена на ADR 0034.
- Строки клиентов исправлены по закреплённым деревьям.
- Закрытые пометки U и C заменены ответами.
- Каждая правка в «Что известно» отмечена «(035)».
