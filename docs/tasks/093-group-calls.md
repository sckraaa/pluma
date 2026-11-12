# 093. Групповые звонки

- Статус: в работе (2026-11-12; владелец дал ход: «пока ебашь групповые звонки»).
- Решение: ADR 0059.
- Зависит от:
  - 068 и 077 — звонки один на один, TURN, `CALLS_QUALITY`, исследование `077-research/group.md`;
  - ADR 0057 — SFU отдельно и переносимо.
- Ветка: `task/093-group-calls`.
- Источники: обозначения `TD`, `AND`, `IOS`, `TWEB`, `TGC`, `api.tl` — как в ADR 0059. tgcalls закреплён у клиентов так:
  - tdesktop — `1c236c09`;
  - Android — `545f5ae5`;
  - iOS — `c1de0882`.

## Цель
Человек в группе или канале начинает звонок, остальные входят с любого из четырёх клиентов. Они слышат и видят друг друга, показывают экран, поднимают руку. Администратор управляет звонком так же, как в Telegram.

SFU — отдельная служба: на другую машину она переносится сменой адреса в конфигурации. Дальше качество поднимается выше Telegram, конференции становятся сквозными.

## Этап 1 (эта задача целиком): звонки в группах и каналах

### Что получают пользователи
- **Звонок.**
  - Начать звонок в базовой группе, супергруппе или канале: сразу или запланированно, с названием.
  - Войти, выйти, вернуться.
  - Строка звонка в чате: «идёт звонок», число участников, аватары.
  - Служебные сообщения о начале, конце (с длительностью), приглашении и расписании.
- **Звук, камера, экран.**
  - Микрофон, «поднять руку».
  - Видео с камеры, показ экрана.
  - Список говорящих, упорядоченный по активности.
- **Администратор** (создатель или `manage_call`):
  - выключить микрофон участнику, задать громкость;
  - запретить включать микрофон новым (`join_muted`);
  - сменить название;
  - пригласить;
  - ссылки для слушателя и для выступающего, сброс ссылки;
  - завершить звонок.
- **Остальные:** личная громкость и личное выключение звука участника.
- **«Войти от имени»** своего канала, с запоминанием выбора.
- **Восстановление.** После обрыва сети или перезапуска SFU клиент сам возвращается в звонок.
- **Качество.** Стоковое, как в Telegram, но с ответами на NACK и выбором слоя для каждого получателя. У iOS звук 64 кбит/с через `voice_chat_send_bitrate`, если проверка на устройстве пройдёт.

### Не входит в этап 1
- Сквозные конференции: `RPCConferenceCalls` не маршрутизируется, `inputGroupCallSlug` и `inputGroupCallInviteMessage` отвечают `GROUPCALL_INVALID`.
- RTMP и трансляция, запись, сообщения в звонке, звёзды, `saveDefaultSendAs` — эти методы отвечают `ErrEnterpriseIsBlocked`, флаги `rtmp_stream`, `record_video_active` и `messages_enabled` не выставляются.
- Патчи клиентов и GCC по задержке.

### Контракт клиентов (главное; подробно — ADR 0059, «Протокол клиентов»)
**Результаты, без которых клиент не войдёт.**
- `createGroupCall` возвращает Updates с `updateGroupCall` (`TD/calls/group/calls_group_call.cpp:1306`; iOS — `GroupCalls.swift:330`).
- `joinGroupCall` и `joinGroupCallPresentation` возвращают `updateGroupCall`, `updateGroupCallConnection` (у показа экрана `presentation:true`) и `updateGroupCallParticipants` с собой. Источники:
  - iOS — `IOS/TelegramCore/Sources/TelegramEngine/Calls/GroupCalls.swift:786-829`;
  - Android свой `source` берёт оттуда — `AND/messenger/voip/VoIPService.java:2145-2160`;
  - tdesktop — `:1989-2030, 2393-2440`.

**JSON запроса** (`TGC tgcalls/group/GroupJoinPayloadInternal.cpp:302-344`):
- поля `ssrc`, `ufrag`, `pwd`, `fingerprints`;
- `ssrc-groups` SIM/FID — только при видео;
- у камеры 3 слоя, у экрана 2 (`GroupInstanceCustomImpl.cpp:3706-3712`);
- Web шлёт те же ключи (`TWEB/lib/calls/helpers/processMediaSection.ts:33-39`).

**JSON ответа** (`GroupJoinPayloadInternal.cpp:41-371`):
- `transport` обязателен, все поля кандидата — строки;
- `audio` и `video` есть всегда: Web считает `video` обязательным (`TWEB/lib/calls/types.d.ts:73-78`);
- `video.endpoint` совпадает с `groupCallParticipantVideo.endpoint` и с id в сообщениях Colibri;
- VP8 100 / rtx 101, Opus 111, расширения 1/2/3/13.

**Ошибки, которые разбирают клиенты** (Android `VoIPService.java:2169-2190`, iOS `GroupCalls.swift:705-725`, tdesktop `:1877-1884, 2020-2024`):
- `GROUPCALL_INVALID`, `GROUPCALL_FORBIDDEN`, `GROUPCALL_JOIN_MISSING`;
- `GROUPCALL_SSRC_DUPLICATE_MUCH` — клиент входит заново со свежим SSRC;
- `GROUPCALL_PARTICIPANTS_TOO_MUCH`, `GROUPCALL_ANONYMOUS_FORBIDDEN`, `JOIN_AS_PEER_INVALID`.

**Версии.**
- Вход и выход — `versioned`, `just_joined`, `left`, версия +1. Прочие изменения несут текущую версию, 0 не бывает (`TD/data/data_group_call.cpp:413-441, 515-517, 600-680`).
- Чужие объекты участников — `min`, личные поля зрителя клиенты держат у себя (`:742-752`).

**Страницы и разрешение SSRC.**
- `getGroupCall(limit)`: tdesktop 3, iOS 4 (`GroupCalls.swift:215`).
- `getGroupParticipants(offset)` продолжает, пустой `next_offset` — конец (tdesktop `:187-205`, по 50).
- `getGroupParticipants(sources=[…])` разрешает неизвестный SSRC (`:897-905, 1036-1043`).

**`checkGroupCall`.**
- Опрос раз в 4 с: tdesktop и iOS — пока соединяются, Android — весь звонок (`TD …calls_group_call.cpp:55, 3746-3776`; `AND …VoIPService.java:2853-2935`).
- Ответ — SSRC, которые держит SFU.

**Изменения участника.**
- tdesktop шлёт `editGroupCallParticipant` по одному флагу за запрос (`:3915-3945`); Android — `:3797-3830`.
- Громкость 1..20000, по умолчанию 10000 (`TD/calls/group/calls_group_common.h:89-90`).
- `canSelfUnmute = !muted || can_self_unmute` (`TD/data/data_group_call.cpp:738`).

**Порядок списка** (iOS `GroupCalls.swift:1377-1423`):
1. может включить микрофон;
2. активность, затем `active_date` по убыванию;
3. `raise_hand_rating` по убыванию;
4. дата входа (по возрастанию при `join_date_asc`);
5. id.

**Транспорт.**
- Нативный клиент — ICE-controlled, DTLS-сервер, SRTP GCM, канал «data» (`TGC tgcalls/group/GroupNetworkManager.cpp:322-326, 398, 419-434`; `SctpDataChannelProviderInterfaceImpl.cpp:30-41`).
- Web — PeerConnection: роль ICE другая (`tools/go_sfu/sfu.go:147-150`).

**Colibri.**
- От клиента — `ReceiverVideoConstraints` (`GroupInstanceCustomImpl.cpp:3511-3578`).
- От SFU — `SenderVideoConstraints` (`:3330-3440`).

**Пакеты до рассылки источника.** Пакеты с SSRC, о котором клиент ещё не знает, отбрасываются (`:2181, 3505-3518`). Поэтому рассылка источника идёт раньше пересылки.

**Не проверено, читать до кода соответствующего шага:**
- кто получает `getGroupCallJoinAs` (`https://core.telegram.org/method/phone.getGroupCallJoinAs`);
- правило рассылки `updateGroupCall` не участникам;
- роль ICE у Chrome.

### Порядок работ
1. **Исходники.**
   - Выкачать подмодули tgcalls в `~/pluma-work/reference-src-new`: tdesktop `Telegram/ThirdParty/tgcalls`, Android `jni/voip/tgcalls`, iOS `submodules/TgVoipWebrtc/tgcalls`.
   - Сверить `GroupInstanceCustomImpl.cpp` трёх коммитов в местах, перечисленных в ADR 0059.
2. **Проверка транспорта на CachyOS, до остального кода.** Минимальный SFU на pion отвечает на настоящий вход tdesktop: JSON-ответ вручную, ICE controlling, DTLS active, SCTP. Затем:
   - пересылка звука между двумя tdesktop;
   - ICE-TCP;
   - переключение слоя по `ReceiverVideoConstraints`;
   - Web в Chrome: какая роль ICE.
   - Если pion не справится — стоп и вопрос владельцу (запасной путь JVB, ADR 0059 §1).
3. **Пакет `pkg/tgcallsjson`:** типы и разбор JSON запроса и ответа, общие для SFU, bff и тестового клиента.
4. **Пакет `pkg/groupcalls`:**
   - миграция и `Store`;
   - версии, права, порядок рейтинга руки;
   - личные настройки зрителя;
   - сборщик.
5. **Служба `app/service/sfu`:**
   - транспорт (ICE с выбором роли, renomination, UDP-мультиплекс, ICE-TCP, DTLS, SRTP, SCTP/DCEP);
   - комнаты;
   - пересылка звука последних говорящих и серверное выключение микрофона;
   - видео: SIM/FID, слой на получателя, переписывание SSRC, `SenderVideoConstraints`, кэш NACK/RTX, PLI/FIR, TWCC отправителям, оценка канала получателя по потерям;
   - gRPC-управление с mTLS;
   - поток событий;
   - метрики.
6. **Модуль `app/bff/groupcalls`:** обработчики (список ниже), клиент SFU с выбором узла, приём событий, рассылки, служебные сообщения.
7. **Состояние звонка у чатов и каналов:**
   - `call_active`, `call_not_empty`, `call`, `groupcall_default_join_as`;
   - сброс кэша чата при начале и конце звонка.
8. **Маршруты, конфигурация, appConfig, Compose, документы:**
   - маршруты в `session.yaml`;
   - конфигурация `bff` и `sfu`;
   - appConfig: `groupcall_video_participants_max`, `voice_chat_send_bitrate` для iOS после проверки;
   - Compose и e2e-стенд;
   - `docs/ops/group-calls.md`, `logaudit`, правила Prometheus.
9. **Тестовый клиент `pkg/groupcallpeer` и e2e.**
10. **`go run ./tools/apicoverage`**, `STATUS`, строки `TESTING-BACKLOG`, «Результат».

### Обработчики этапа 1 (`app/bff/groupcalls/internal/core/phone.<method>_handler.go`)
Обслуживаются:
- **`createGroupCall`**
  - `random_id` идемпотентен;
  - `schedule_date` создаёт запланированный звонок;
  - `rtmp_stream` отклоняется.
- **`joinGroupCall`**
  - `join_as`, `invite_hash` (ссылка выступающего даёт `can_self_unmute`), `muted`, `video_stopped`;
  - `public_key` и `block` дают `GROUPCALL_INVALID`.
- **`leaveGroupCall`** — по `source`.
- **`joinGroupCallPresentation`** и **`leaveGroupCallPresentation`.**
- **`checkGroupCall`.**
- **`getGroupCall`**, **`getGroupParticipants`** — по `ids`, `sources` и `offset`.
- **`editGroupCallParticipant`:** `muted`, `volume`, `raise_hand`, `video_stopped`, `video_paused`, `presentation_paused`.
- **`editGroupCallTitle`.**
- **`toggleGroupCallSettings`:** `join_muted`, `reset_invite_hash`; `messages_enabled` даёт `GROUPCALL_NOT_MODIFIED`.

  > Ошибка для `messages_enabled` выбрана без чтения клиентов. Проверить, как её показывают клиенты; если плохо — `ErrEnterpriseIsBlocked`.
- **`discardGroupCall`.**
- **`inviteToGroupCall`.**
- **`exportGroupCallInvite`.**
- **`getGroupCallJoinAs`**, **`saveDefaultGroupCallJoinAs`.**
- **`startScheduledGroupCall`**, **`toggleGroupCallStartSubscription`.**

Отвечают `ErrEnterpriseIsBlocked`:
- `toggleGroupCallRecord`;
- `getGroupCallStreamChannels`, `getGroupCallStreamRtmpUrl`;
- `sendGroupCallMessage` (обе формы);
- `deleteGroupCallMessages`, `deleteGroupCallParticipantMessages`;
- `getGroupCallStars`, `saveDefaultSendAs`.

Что отвечает прокси на неподключённый `RPCConferenceCalls` (`app/bff/bff/client/fake_rpc_result.go`) — проверить. Если там фальшивый ответ, сервис регистрируется, и все его методы отвечают `ErrEnterpriseIsBlocked`.

### Файлы
**Добавить:**
- `teamgramd/deploy/sql/migrate-20261117-group-calls.sql`, повторяемая:
  - `group_calls`: id, access_hash, peer, creator, title, flags, version, participants_count, schedule_date, invite hashes, `sfu_node`, даты;
  - `group_call_participants`: уникальные (call, peer) и (call, ssrc), ssrc показа экрана, группы источников JSON, флаги, громкость, рейтинг руки, даты, auth_key_id;
  - `group_call_viewer_prefs`;
  - `group_call_subscriptions` — подписки на запланированные звонки;
  - выбор «от имени» хранится в чате (`groupcall_default_join_as`) — по месту хранения настроек чата.
- `pkg/tgcallsjson/` с тестами на «золотых» файлах:
  - запросы из tdesktop и Web;
  - ответ, который разбирает `GroupJoinPayloadInternal.cpp`.
- `pkg/groupcalls/`: `call.go`, `store.go`, `store_integration_test.go`, `versions.go`, `rights.go`, `sweeper.go`, тесты.
- `app/service/sfu/`:
  - `cmd/sfu/main.go`, `etc/sfu.yaml`, `sfu.proto` и сгенерированный код;
  - `internal/transport` — ICE, DTLS, SRTP, SCTP, мультиплекс;
  - `internal/room`;
  - `internal/forward` — слои, переписывание SSRC, NACK-кэш, TWCC, оценка канала;
  - `internal/audio` — последние говорящие, выключение микрофона;
  - `internal/colibri`;
  - `internal/control` — gRPC, mTLS, события;
  - `internal/metrics`;
  - тесты; `Dockerfile`.
- `app/bff/groupcalls/` по образцу `app/bff/voipcalls`: `internal/{config,core,dao,server,svc}`, клиент SFU (`internal/sfuclient`, выбор узла по `plaintext`), приём событий.
- `pkg/groupcallpeer/` — тестовый клиент в ролях tgcalls:
  - ICE controlled, DTLS-сервер с `setup:"passive"`, SRTP GCM, канал «data»;
  - синтетический Opus с расширением уровня;
  - VP8 в 3 слоя simulcast;
  - приём и проверка SSRC;
  - сообщения Colibri;
  - режим PeerConnection-роли для проверки Web-пути.
- `test/e2e/group_calls_test.go`.
- `docs/ops/group-calls.md`: порты, адреса, mTLS-сертификаты управления, перенос SFU на другую машину, лимиты.

**Изменить:**
- `app/bff/bff/internal/server/server.go` — регистрация `RPCGroupCalls`.
- Маршрут `"/mtproto.RPCGroupCalls": "bff.bff"`: `teamgramd/etc/session.yaml:113`, `teamgramd/etc2/session.yaml:113`, `app/interface/session/etc/session.yaml:93`.
- Конфигурация bff: `app/bff/bff/internal/config/config.go`, `app/bff/bff/etc/bff.yaml`, `teamgramd/etc/bff.yaml`, `teamgramd/etc2/bff.yaml` — узлы SFU, сертификаты, лимиты.
- Состояние звонка у чатов и каналов:
  - `app/service/biz/chat/plugin/plugin.go` (реализация `GetChatCallActiveAndNotEmpty`, `GetChatGroupCall`), `chat_tx.go`, `chat_cache.go`;
  - `app/bff/chats/internal/core/messages.getFullChat_handler.go:88` (`GroupcallDefaultJoinAs`);
  - `app/service/biz/channel/channel/channel_view.go`;
  - `app/bff/channels/internal/core/channels.getFullChannel_handler.go`.
- appConfig — там, где его собирает `configuration`.
- `docker-compose.yaml`, `.env.example` (адреса и порты SFU), `test/e2e/docker-compose.e2e.yaml`, `test/e2e/run.sh`.
- `tools/logaudit/logaudit.go` — запрет ключей DTLS, `pwd`, `ufrag` и содержимого JSON входа.
- `teamgramd/deploy/prometheus/server/rules/`, `docs/ops/monitoring.md`.
- `go.mod`, `go.sum` — пакеты pion.

### Тесты
**Модульные:**
- `pkg/tgcallsjson`: разбор запросов tdesktop и Web; ответ содержит все поля кандидата строками, `audio` и `video`; отказ на кривой JSON.
- `pkg/groupcalls`:
  - версии (+1 на вход и выход, текущая на прочее, никогда 0);
  - права (создатель, `manage_call`, обычный участник);
  - `can_self_unmute`;
  - личные и администраторские громкость и выключение;
  - рейтинг руки;
  - страницы `next_offset`.
- SFU:
  - выбор слоя по ограничениям и оценке канала;
  - переписывание SSRC с непрерывностью номеров и меток при смене слоя;
  - NACK-кэш и RTX;
  - отбор последних говорящих;
  - `SenderVideoConstraints` — наибольшая смотрящая высота;
  - выбор роли ICE по первому запросу.

**SFU в процессе, без Docker:** два и три `groupcallpeer` на loopback.
- Звук доходит с правильным SSRC.
- Видео: слой переключается по `ReceiverVideoConstraints`.
- Выключенный микрофон не пересылается.
- Потеря пакетов даёт NACK и RTX.
- Только TCP.
- Перезапуск комнаты.

**Интеграционные** (`pkg/testinfra`, MySQL):
- миграция дважды подряд;
- десять параллельных входов дают версии без пропусков и повторов;
- дубликат SSRC даёт `GROUPCALL_SSRC_DUPLICATE_MUCH`;
- условная смена `sfu_node` под гонкой;
- сборщик выводит отвалившихся с увеличением версии.

**Сквозные** (`pkg/mtclient` + `pkg/groupcallpeer`, стенд со службой `sfu`, сессии после `getState`):
- `TestGroupCallCreateJoin`:
  - создать в супергруппе;
  - результат входа содержит три обновления;
  - второй участник входит;
  - звук идёт в обе стороны;
  - `call_active`/`call_not_empty` у канала;
  - служебное сообщение о начале.
- `TestGroupCallVersions`: третья сессия получает `updateGroupCallParticipants` с версиями по порядку; при пропуске `getGroupCall` даёт целое состояние.
- `TestGroupCallMuteRights`:
  - администратор выключает микрофон (`can_self_unmute` снят, звук не пересылается);
  - обычный участник выключает только для себя (`muted_by_you` только ему);
  - самовключение без права отклоняется.
- `TestGroupCallVideoAndScreen`: камера в 3 слоя и экран вторым подключением; получатель видит `video.endpoint` и `source_groups`; слой меняется по ограничениям.
- `TestGroupCallSourcesLookup`: `getGroupParticipants(sources)` по SSRC.
- `TestGroupCallSfuRestart`: убить `sfu`; `checkGroupCall` не содержит SSRC; повторный вход восстанавливает звук.
- `TestGroupCallInviteJoinAsSchedule`:
  - ссылка выступающего и слушателя;
  - вход от имени канала;
  - запланированный звонок, подписка, старт;
  - `discardGroupCall` даёт `groupCallDiscarded` и сообщение с длительностью.
- `TestGroupCallTcpOnly`: UDP закрыт, вход по ICE-TCP.

Хотя бы один тест проверить падающим без изменения: например, `TestGroupCallCreateJoin` на `master` получает `ERR_ENTERPRISE_IS_BLOCKED`.

### Проверки перед сдачей (CachyOS)
- Ветка:
  - `go build ./...`;
  - `go vet -tags integration,e2e -unreachable=false` затронутых пакетов;
  - `go test` затронутых и импортирующих их.
- Партия:
  - `-race`;
  - интеграционные по одному пакету;
  - e2e раздела «Тесты» со своими `E2E_PROJECT`/`E2E_DIR`, затем `test/e2e/run.sh down`;
  - `make lint`, `go test ./tools/...`, `go test ./pkg/conf/...`;
  - `go run ./tools/apicoverage`, `make apicoverage-check`.
- Живая проверка, со скриншотами:
  - tdesktop и Web на CachyOS;
  - iOS в симуляторе;
  - Android в эмуляторе.
- На машине разработки ничего не собирать.

### Проверки для `docs/project/TESTING-BACKLOG.md`
- [ ] Android, iOS, Desktop, Web: начать звонок в супергруппе, войти со второго аккаунта, слышно в обе стороны; строка звонка в чате; служебные сообщения о начале и конце с длительностью.
- [ ] То же в базовой группе и в канале (слушатель без права голоса, «поднять руку», администратор даёт слово).
- [ ] Камера у трёх участников, один показывает экран; у плохой сети (ограничить канал) картинка падает в качестве, а не замирает.
- [ ] Администратор выключает микрофон участнику и меняет громкость; обычный участник — личная громкость и выключение только у себя.
- [ ] Ссылка слушателя и ссылка выступающего; сброс ссылки; приглашение участника чата.
- [ ] «Войти от имени» своего канала, выбор запоминается.
- [ ] Запланированный звонок: подписка, уведомление, старт.
- [ ] Переход Wi-Fi → LTE в звонке без выхода; перезапуск `sfu` — клиенты возвращаются сами.
- [ ] Сеть без UDP: вход по TCP.
- [ ] iOS: звук 64 кбит/с (`voice_chat_send_bitrate`) слышно лучше, ничего не ломается.

### Решения, требующие подтверждения владельцем
1. Свой SFU на pion вместо JVB (ADR 0059 §1).
2. Сквозное шифрование только в конференциях, как в Telegram. Обычные звонки групп и каналов не сквозные: SFU для них стоит в доверенной зоне ядра, сегодня на CX33, после переезда дома.
3. Лимит видео одновременно: 30 на CX33; дома при 60 Мбит/с — около 8 при качестве Pluma.
4. ICE-TCP: второй IPv4 на сервере для порта 443 (443 занят Caddy) или другой порт.
5. Пустой обычный звонок не завершается сам, пока его не завершат (по умолчанию).
6. Запись звонков и RTMP — не делаются; нужны ли когда-нибудь.

## Дальше (наброски)
- **Этап 2 — адаптация к сети.**
  - GCC по задержке на получателя.
  - Подбор числа последних говорящих.
  - ICE-TCP и кандидат `ssltcp` по замерам (`077-research/group.md`).
  - Замеры против Telegram на одной плохой сети.
- **Этап 3 — качество лучше Telegram** (патч tgcalls трёх нативных клиентов вместе с этапом 2 задачи 077):
  - Opus 20 мс, 48–64 кбит/с;
  - камера до 2,5 Мбит/с, экран 1080p30 до 4 Мбит/с;
  - общий потолок выше 1052 кбит/с;
  - simulcast в конференциях;
  - параметры из appConfig по образцу `CALLS_QUALITY`;
  - для Web — параметры Opus в ответе.
- **Этап 4 — сквозные конференции.**
  - ADR о проверке блоков (порт на Go или tde2e) после чтения исходников tde2e.
  - `RPCConferenceCalls`: `createConferenceCall`, `getGroupCallChainBlocks`, `sendConferenceCallBroadcast`, `deleteConferenceCallParticipants`, `inviteConferenceCallParticipant` (звонит по `messageActionConferenceCall`, `TD/history/history.cpp:1527-1531`), `declineConferenceCallInvite`.
  - Ссылки `inputGroupCallSlug`, перевод звонка один на один (`phoneCallDiscardReasonMigrateConferenceCall`).
  - SFU на VPS (`plaintext: false`).
  - Тестовые блоки из tde2e.
- **Этап 5 — сообщения в звонке** (`messages_enabled`, `sendGroupCallMessage`), после чтения клиентов.

## Результат
_Заполняется по завершении._
