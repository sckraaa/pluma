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
_Итог задачи заполняется по завершении. Ниже — часть A этапа 1 (порядок работ, шаги 1–3 и 5)._

### Часть A: исходники, проверка транспорта, `pkg/tgcallsjson`, служба `sfu` (2026-11-12)
Свой SFU на pion работает с клиентом, который говорит с ним как tgcalls: вход по JSON `GroupJoinPayloadInternal`, ICE с выбором роли по первому запросу проверки, DTLS (SFU — клиент), SRTP GCM, канал данных SCTP/DCEP «data», звук в обе стороны, переключение слоя видео по `ReceiverVideoConstraints` с непрерывным потоком, ответы на NACK через RTX, ICE-TCP. Запасной путь (JVB) не нужен. Настоящий tdesktop к SFU не подключался: без обработчиков `phone.joinGroupCall` (шаг 6) клиенту неоткуда взять ответ SFU, поэтому живая проверка tdesktop и Web переносится на после шага 6.

**Исходники (шаг 1).**
- Подмодули tgcalls выкачаны в `~/pluma-work/reference-src-new` на закреплённых коммитах: `tdesktop/Telegram/ThirdParty/tgcalls` — `1c236c09`, `android/TMessagesProj/jni/tgcalls` — `545f5ae5` (путь подмодуля у Android — `jni/tgcalls`, не `jni/voip/tgcalls`), `ios/submodules/TgVoipWebrtc/tgcalls` — `c1de0882`. Это клоны `--shared` от `reference-src-new/tgcalls-mirror`, его не удалять.
- `GroupNetworkManager.cpp`, `GroupJoinPayloadInternal.cpp`, `GroupJoinPayload.h`, `SctpDataChannelProviderInterfaceImpl.cpp` совпадают во всех трёх побайтно (md5).
- `GroupInstanceCustomImpl.cpp` различается (у Android и iOS VAD и шумоподавление вынесены в отдельные файлы, другой учёт размеров кадра), но места протокола одинаковы во всех трёх: id расширений 1/2/3/13, число слоёв 3/2/1, ptime 120, `setup "passive"`, первый динамический PT 100, классы Colibri, `_outgoingVideoConstraint = 720` по умолчанию (tdesktop `:4458`, Android `:3727`, iOS `:3716`).
- Прочитано дополнительно и использовано в коде:
  - роль в ICE и DTLS, отсутствие своих кандидатов и TURN — `GroupNetworkManager.cpp:398-434`;
  - неизвестный SSRC звука (PT 111) клиент разрешает через запрос описаний, видео без описания не принимается — `GroupInstanceCustomImpl.cpp:3293-3311, 3440-3495`;
  - приёмник видео показывает только первый SSRC группы SIM (`SetSink(_mainVideoSsrc)`, `:1728-1790`), поэтому все слои переписываются в него;
  - экран — отдельный экземпляр tgcalls без запросов описаний (`TD/calls/group/calls_group_call.cpp:3165-3185`);
  - libwebrtc подключается к TCP-кандидату с портом сам, если `tcptype` пуст (`android/.../webrtc/p2p/base/tcp_port.cc:131-166`), и принимает атрибут NOMINATION 0xC001 на управляемой стороне (`p2p/base/connection.cc:642-660`). Это копия webrtc из Android; копия tdesktop в `reference-src-new` отсутствует.

**Код.**
- `pkg/tgcallsjson` — запрос (подписанные и беззнаковые SSRC, проверка ICE-строк, отпечатка sha-256, `setup`, групп SIM/FID, не больше 3 слоёв), ответ (все поля кандидатов строками, `rtcp-mux`, `audio` и `video` всегда, Opus 111, VP8 100/RTX 101, расширения 1/2/3/13, TCP-кандидат с `tcptype passive`), разбор ответа по правилам tgcalls (для тестового клиента), сообщения Colibri. «Золотые» файлы в `testdata/`.
- `app/service/sfu`:
  - `internal/transport` — общий UDP-порт (IPv4 и IPv6) и порт ICE-TCP; ICE-агент на участника; роль по первому подлинному запросу проверки (USERNAME и MESSAGE-INTEGRITY); DTLS-клиент со сверкой отпечатка; SRTP (GCM, запасной AES-CM для браузеров); SCTP и DCEP; повторное выдвижение пары (renomination, автоматическое раз в 3 с).
  - `internal/room` — комнаты, вход (повторный вход заменяет соединение, дубликат SSRC, лимит участников), выход, серверное выключение микрофона, источники, события, распределение слоёв раз в 500 мс и при изменениях, RR и TWCC отправителям, трансляция SR получателям, NACK из кэша или вверх отправителю, PLI/FIR с ограничением частоты, лимит одновременных видео.
  - `internal/forward` — слой на получателя с переписыванием SSRC, номеров, меток времени, picture id и TL0PICIDX VP8; кэш пакетов; RTX; выбор слоёв по ограничениям и бюджету; оценка канала по потерям; статистика приёма для RR.
  - `internal/audio` — последние говорящие (N, удержание, бонус выбранному против «мигания»).
  - `internal/control` и `sfu/sfu.proto` — gRPC с mTLS (TLS 1.3, сертификат клиента обязателен), поток событий; `sfu/gen.sh` перегенерирует код.
  - `internal/metrics`, `internal/config`, `cmd/sfu`, `etc/sfu.yaml`.
- `pkg/groupcallpeer` — тестовый клиент в ролях tgcalls (шаг 9 начат здесь, потому что без него транспорт не проверить).
- `teamgramd/deploy/sfu/Dockerfile` — свой образ SFU на distroless.
- `go.mod`: pion (ice v4.4.9, dtls v3.1.15, srtp v3.1.3, sctp v1.12.1, datachannel v1.6.4, rtp v1.10.6, rtcp v1.2.20, stun v4.0.1, interceptor v0.1.51 — только `pkg/twcc`). Минимальный выбор версий поднял testify 1.11.1 → 1.12.1 и yaml v3.0.4 → v3.0.5.

**Проверка (CachyOS).**
- `go build ./...` — успешно.
- `go vet -unreachable=false` новых пакетов — чисто.
- `go test` и `go test -race` по `./app/service/sfu/... ./pkg/tgcallsjson/... ./pkg/groupcallpeer/...` — все пакеты `ok`. Тесты комнаты трижды подряд (`-count=3`) — без сбоев.
- `go test ./tools/...` (с logaudit) — `ok`, после замены `%d` от enum на `int32(k)` в `control.go`.
- `golangci-lint v2.15.0` по новым пакетам — 0 замечаний.
- Образ `teamgramd/deploy/sfu/Dockerfile` собран, запущен (без сертификатов завершается с понятной ошибкой) и удалён.
- Тест падает без изменения: если не сдвигать номер пакета при смене слоя, `TestDownTrackSwitch` и `TestVideoLayerSwitch` падают («sequence 14026 after 24057»).
- SFU в процессе с клиентами `groupcallpeer` по настоящим UDP и TCP на loopback:
  - `TestAudioBothWays` — роль SFU `controlling`, пара `udp4`, звук в обе стороны с PT 111 и расширениями, своё не возвращается, отправителю идут TWCC и RR, `Sources`;
  - `TestServerMute`;
  - `TestLastSpeakers` (N=1);
  - `TestVideoLayerSwitch` — до ограничений ничего, по 180 — слой 0, по 720 — слой 2; номера, picture id и метки времени непрерывны, SSRC — слоя 0; `SenderVideoConstraints` 0 → 180 → 720; после снятия ограничений поток останавливается;
  - `TestNACKAnsweredWithRTX` — RTX с SSRC из FID, PT 101, исходным номером и тем же содержимым;
  - `TestICETCPOnly` — пара `tcp4`;
  - `TestPeerConnectionRole` — клиент в роли PeerConnection, SFU `controlled`;
  - `TestJoinRules`, `TestRoomRestart`, `TestClientThatNeverComes` (событие `no_check`), `TestE2EKinds`, `TestVideoLimit`, `TestDataChannelRestart`.
- Модульные: переписывание при смене слоя и через переполнение номеров, опоздавшие пакеты и padding, RTX, кэш, выбор слоёв, оценка канала, RR, VP8, последние говорящие, роль по первому запросу и отказ на поддельные, первый кадр ICE-TCP, конфигурация, mTLS (без сертификата, чужой CA, CA сервера — отказ), шина событий.

**Решения (подтвердить).**
1. Роль ICE определяется до запуска агента по первому подлинному запросу на общем сокете. pion-агент не читает пакеты до старта (`ice candidate_base.go` `recvLoop`), поэтому наблюдение идёт на уровне общего UDP-сокета и первого кадра ICE-TCP. Пакеты при этом не теряются: мультиплексор pion держит их до старта.
2. Соединение показа экрана только отправляет: SFU ничего не пересылает в него. tdesktop создаёт экран отдельным экземпляром без запросов описаний, и он проиграл бы звук в loopback-устройство.
3. Выключение микрофона администратором действует на основное соединение. Звук экрана (если он есть) не выключается. _Пересмотрено в части D: без слова нет ни камеры, ни показа экрана._
4. Звук экрана идёт мимо отбора последних говорящих.
5. Ключевые кадры распознаются и picture id переписываются только у VP8: SFU предлагает только VP8, а нативные клиенты выбирают его первым (`GroupInstanceCustomImpl.cpp:3110-3140`; tdesktop `calls_group_call.cpp:2998-3005`).
6. Пакет, которого нет в кэше, SFU запрашивает у отправителя (NACK с исходными номерами).
7. Отправителю сверх лимита видео уходит `idealHeight 0`.
8. Отправителю, которого никто не смотрит, тоже уходит `idealHeight 0`: его слои камеры выключаются (`:2596-2615`).
9. Узлы с `Plaintext: true` по умолчанию; конференции (`E2E`) отклоняются, пока не включены.
10. Порт ICE-TCP по умолчанию — 20601, настраивается; 443 не используется.

**Отклонения от брифа.**
- Классы Colibri — в `pkg/tgcallsjson` (их использует и тестовый клиент). Обработка на стороне SFU — в `internal/room`; отдельного `internal/colibri` нет.
- Dockerfile лежит в `teamgramd/deploy/sfu/`, по образцу остальных образов, а не в `app/service/sfu`.
- `pkg/groupcallpeer` начат в части A (шаг 9).
- Правила `logaudit` для `pwd` и `ufrag` не добавлены: это шаг 8. Сейчас SFU не пишет в лог ни учётных данных ICE, ни JSON входа: отладочный вывод pion отброшен (`transport/logger.go`).

**Не сделано и не проверено.**
- Живой tdesktop (Linux на CachyOS) и Web в Chrome не подключались: нужна сигнализация, шаг 6.
- Поведение libwebrtc при повторном выдвижении пары и переходе Wi-Fi → LTE не проверено на настоящем клиенте.
- Оценка канала — только по потерям (GCC — этап 2).
- Лимит отдачи узла масштабирует бюджет получателей, но под нагрузкой не измерялся.
- H.264 и VP9 не разбираются.
- Перезапуск SCTP клиентом (`TestDataChannelRestart`) проходит. Новый INIT на живой ассоциации pion принимает сам. Цикл новой ассоциации в `transport.runDataChannel` на случай, когда старая закрылась, тестом не покрыт: тест проходит и без него.

### Часть B: состояние, сигнализация, состояние чатов, развёртывание (шаги 4, 6, 7, 8; 2026-11-12)
Сервер обслуживает `RPCGroupCalls`: звонок создаётся, в него входят и выходят, SFU получает и отдаёт соединения, участники видят друг друга в обновлениях с версиями, у чатов и каналов видно, что идёт звонок, в истории — служебные сообщения. Проверено на MySQL с поддельными SFU и сервисами biz, msg и sync. Живые клиенты к стенду не подключались: e2e и стенд — шаг 9.

**Решения владельца 2026-11-12, учтённые в коде.**
- «Войти от имени канала» убрано (ADR 0060). `getGroupCallJoinAs` отдаёт только пользователя. `joinGroupCall` и `saveDefaultGroupCallJoinAs` с каналом отвечают `JOIN_AS_PEER_INVALID`: на него Android забывает выбор и кладёт трубку (`AND/messenger/voip/VoIPService.java:2169-2181`). `groupcall_default_join_as` не выставляется, таблицы выбора нет. tdesktop при единственном варианте показывает простое подтверждение вместо выбора (`TD/calls/group/calls_choose_join_as.cpp:444-460`) — это закрывает пункт «не проверено» ADR 0060 о показе выбора.
- Узлы SFU в конфигурации bff — без признака `plaintext`: bff не выбирает узел по тому, что тот видит (ADR 0060 §3). Комнаты этапа 1 открываются с `e2e=false`; в SFU признак узла пока остаётся из части A.
- RTMP и запись — `ErrEnterpriseIsBlocked`; `stream` в ответе SFU не бывает; `public_key`, `block` и `key_fingerprint` во входе — `GROUPCALL_INVALID`.

**Код.**
- `pkg/groupcalls`: миграция, `Store`, версии, права, рейтинг руки, личные настройки зрителя, сборщик, состояние звонка для чатов (`ChatPlugin`, `ChatStates`).
- `app/bff/groupcalls`:
  - `config`, `dao`, `svc`, `server/grpc/service` (все 27 методов `RPCGroupCallsServer`);
  - `sfuclient` — mTLS, выбор узла, поток событий с повторным подключением; узел без адреса пропускается;
  - `core` — обработчики этапа 1, `not_served.go`, приём событий, рассылки, служебные сообщения, сборщик, метрики `groupcalls_*`.
  - `helper.go` — модуль, сборщик и приём событий запускаются вместе с bff и останавливаются с ним.
- bff: регистрация `RPCGroupCalls` (`app/bff/bff/internal/server/server.go`), секция `GroupCalls` конфигурации (`teamgramd/etc/bff.yaml`, `teamgramd/etc2/bff.yaml`; `app/bff/bff/etc/bff.yaml` — ссылка на документ).
- Маршрут `"/mtproto.RPCGroupCalls": "bff.bff"` во всех трёх `session.yaml`. `RPCConferenceCalls` не маршрутизируется: прокси отвечает `ErrEnterpriseIsBlocked` без фальшивого ответа (`app/bff/bff/client/fake_rpc_result.go:19-48`), регистрировать сервис не нужно.
- Состояние чатов: `call_active`, `call_not_empty`, `chatFull.call` базовых групп — через `ChatPlugin` в biz; у каналов — поля 55–57 `ImmutableChannel`, читаются вместе с каналом; `channelFull.call` — в `channels.getFullChannel`. Кэш сбрасывать не нужно: у чатов состояние звонка читается после кэша (`chat_cache.go:95-96`, `chat_tx.go:132-133`), каналы в dao не кэшируются. Объект чата с новыми флагами уходит вместе с `updateGroupCall`, когда звонок начинается, пустеет или кончается.
- appConfig: `groupcall_video_participants_max: 30` (Android `MessagesController.java:3172`, iOS `PresentationCallManager.swift:200`), строка в `docs/ops/app-config.md`.
- Развёртывание:
  - `docker-compose.sfu.yaml` — служба `sfu` (свой образ, `mem_limit`/`cpus` из `SFU_MEMORY`/`SFU_CPUS`, опубликованные UDP и TCP на `PLUMA_PUBLIC_IPV4`, только чтение) и дополнение bff: адрес `sfu:20500` и сертификат;
  - `teamgramd/deploy/sfu/sfu.yaml` — конфигурация SFU в Docker;
  - `teamgramd/deploy/sfu/control-certs.sh` — два центра (узлов и bff), сертификаты обеих сторон;
  - `GROUPCALLS_SFU_ENDPOINT` в `docker-compose.yaml` (migrate, bff) и `.env.example`;
  - `docs/ops/group-calls.md`, разделы в `docs/ops/monitoring.md` и `docs/ops/memory.md`.
- Prometheus: цель `ops.sfu` по DNS, правила `rules/groupcalls.yml` (`GroupCallsSFUUnreachable`, `GroupCallsConnectFailing`, `GroupCallsUplinkSaturated`, `GroupCallsEventsDropped`).
- `tools/logaudit`: `ufrag`, `pwd`, `fingerprints`, `params` (JSON входа и ответа), ключи DTLS и SRTP; типы `pkg/tgcallsjson` — объекты, их нельзя печатать целиком. Одиночное `fingerprint` не запрещено: так называются отпечатки ключа RSA сервера в gnetway и `pkg/mtclient`, они не секрет.
- `docs/project/api-coverage.*` перегенерированы: phone — 27 ok, 1 partial (`createGroupCall`: RTMP), 9 blocked.

**Проверка (CachyOS).**
- `go build ./...` — успешно.
- `go vet -tags integration,e2e -unreachable=false` затронутых пакетов — чисто.
- `go test -race` затронутых пакетов и импортирующих (bff, channels, chats, configuration, biz channel и chat, pluma-admin) — все `ok`.
- `go test ./tools/... ./pkg/conf/...` — `ok`, включая logaudit и проверки Compose.
- Интеграционные на MySQL (`pkg/testinfra`), с `-race`:
  - `pkg/groupcalls`: миграция дважды, десять параллельных входов дают версии 2..11 без пропусков, дубликат SSRC, повторный вход и устаревший выход, выключение администратором переживает выход, гонка за `sfu_node`, сборщик, страницы и поиск по SSRC, завершение, запланированный звонок;
  - `app/bff/groupcalls/internal/core` (поддельные SFU и biz/msg/sync):
    - `TestGroupCallFlow` — права на создание, повтор `random_id`, служебные сообщения начала и конца с длительностью, три обновления во входе, отказы (канал, ключ, чужой, кривой JSON, дубликат SSRC с закрытием соединения на SFU), `min`-строки и версии у других, `checkGroupCall` по SFU, личное и администраторское выключение (`SetMuted` на SFU), рука, поиск по SSRC и страницы, `getGroupCallJoinAs`, выход, конец звонка;
    - `TestGroupCallSfuRestart` — комната потеряна, `checkGroupCall` пуст, повторный вход, сборщик выводит невернувшегося с версией.
- Модульные: `connect` (размещение, гонка, потерянная комната, узел недоступен, ошибки SFU в ошибки API), версии, служебные сообщения, `sfuclient` с настоящим mTLS (запросы и события проходят; сертификат центра узлов вместо bff — отказ), состояние звонка в объекте канала.
- Тест ловит ошибку: `TestGroupCallFlow` упал на `editGroupCallParticipant` с `inputPeerSelf` (`GROUPCALL_JOIN_MISSING`). Это была настоящая ошибка: `inputPeerSelf` не приводился к пользователю. Исправлено (`participantKey`).
- `control-certs.sh` проверен `openssl verify`: сертификат узла проходит центр узлов, сертификат bff — центр bff, перекрёстно — отказ.
- golangci-lint v2.15.0 по затронутым пакетам: в новых файлах замечаний нет. Шесть замечаний — в старых файлах (`app/bff/bff/client/*.go`, `pkg/conf/conf.go`); в копии на CachyOS нет git, и фильтр по новым строкам не сработал.

**Отклонения от брифа.**
- Служба `sfu` — в наложении `docker-compose.sfu.yaml`, а не в `docker-compose.yaml`: там опубликованные порты разрешены только gnetway и push (`pkg/conf/compose_services_test.go`), и все службы — на общем образе. Конфигурация SFU для Docker — `teamgramd/deploy/sfu/sfu.yaml`, а не `teamgramd/etc2`: файлы `etc2` — службы общего образа.
- Таблица `group_call_default_join_as` не создаётся (решение владельца).
- Обработчики проверены интеграционными тестами на MySQL с поддельными SFU и клиентами, а не модульными: `Store` — MySQL.
- e2e-стенд (`test/e2e/docker-compose.e2e.yaml`, `run.sh`) не менялся: SFU и сертификаты в стенде — вместе с e2e-тестами, шаг 9.
- `voice_chat_send_bitrate` в appConfig не добавлен: бриф требует проверки на устройстве iOS.

**Не сделано и не проверено.**
- Живые клиенты (tdesktop и Web на CachyOS, iOS, Android) — после e2e-стенда.
- Как клиенты показывают `GROUPCALL_NOT_MODIFIED` на `messages_enabled` и `GROUPCALLS_NOT_CONFIGURED`, когда SFU не настроен.
- Правило рассылки Telegram (кому `updateGroupCall`): сделано по ADR 0059 §6 — участникам чата и звонка, плюс тем, кто был в звонке при его завершении.
- CI не собирает образ SFU; на сервере образ собирается на месте или задаётся `PLUMA_SFU_IMAGE`.
- Нагрузка на узел (память, CPU, отдача) не замерялась; лимиты контейнера — оценка.

**Решения (подтвердить).**
1. Порядок шагов входа: сначала SFU, потом состояние (ответ SFU нужен клиенту); если состояние отказывает, соединение на SFU закрывается. Остальные шаги — состояние, SFU, рассылка (ADR 0059 §10).
2. Сборщик раз в 5 с; отсутствие в SFU дольше 30 с выводит участника; пустая комната закрывается через 30 с, звонок остаётся до `discardGroupCall`.
3. `can_start_video` и `video_joined` — пока в звонке меньше камер, чем лимит; камера сверх лимита — `GROUPCALL_VIDEO_TOO_MUCH`.
4. Ссылки приглашения — только у публичных чатов (`PUBLIC_CHANNEL_MISSING` у частных), как предлагает Android (`GroupCallActivity.java:6632-6634`).
5. `inviteToGroupCall` — только участников чата (`USER_NOT_PARTICIPANT`). _Уточнено в части D: не участники пропускаются, ошибка — только если участников в списке нет._
6. Без узлов SFU `createGroupCall` и `joinGroupCall` отвечают `GROUPCALLS_NOT_CONFIGURED`.
7. SFU включается наложением `docker-compose.sfu.yaml`; оно же задаёт bff адрес и сертификат. Центров два, их ключи не хранятся.

### Часть C: тестовый клиент, SFU на e2e-стенде, сквозные сценарии (шаг 9 и строки TESTING-BACKLOG; 2026-11-12)
Весь путь звонка проверен сквозным тестом на стенде: настоящие gnetway, session, bff, biz, msg, sync и служба `sfu` в своём образе; клиенты — `pkg/mtclient` для сигнализации и `pkg/groupcallpeer` для медиа, по настоящим UDP и TCP через опубликованные порты Docker, как у сервера. Все семь сценариев проходят; весь набор e2e на том же стенде — см. «Проверка». Живые клиенты (tdesktop и Web на CachyOS, iOS-симулятор, Android-эмулятор) ещё не подключались — это следующий шаг, строки для владельца — в `TESTING-BACKLOG`.

**Код.**
- `pkg/groupcallpeer/media.go` — то, что нужно сценариям от клиента: поток звука с ptime tgcalls (120 мс, `GroupInstanceCustomImpl.cpp:2716`), ожидание и отсутствие пакетов источника, поток видео всех слоёв с ответом ключевым кадром на PLI/FIR и записью `SenderVideoConstraints` из канала данных (`:3330-3440`), номер слоя кадра.
- `test/e2e/group_calls_test.go`:
  - `TestGroupCallSupergroup` — звонок в супергруппе: `updateGroupCall` участникам чата, `call_active`/`call_not_empty` и `channelFull.call`, служебное сообщение о начале; вход двоих, затем третьего, ответ входа с тремя обновлениями, `min`-строки и версии +1 по порядку у других; звук всех ко всем, своего звука нет; `getGroupParticipants(sources)`, `checkGroupCall`, `getGroupCall` целиком; выход (версия +1, звук ушедшего прекращается) и повторный вход новым клиентом; `discardGroupCall` только администратором, `groupCallDiscarded` с длительностью всем, сообщение с той же длительностью, флаги канала сняты, звук прекращён, вход после конца — `GROUPCALL_ALREADY_DISCARDED`.
  - `TestGroupCallMuteAndHand` — выключение администратором (без `can_self_unmute`, SFU не пересылает), самовключение без права — `CHAT_ADMIN_REQUIRED`, рука с рейтингом, слово от администратора опускает руку, звук снова идёт; личные выключение и громкость (`muted_by_you` только в ответе, SFU пересылает), громкость администратора с `volume_by_admin`.
  - `TestGroupCallVideoAndScreen` — камера в 3 слоя: `video.endpoint` равен endpoint ответа SFU, `source_groups` (SIM из трёх и три FID); по `ReceiverVideoConstraints` 180 приходит слой 0, по 720 — слой 2, всё под первым SSRC; отправителю приходят `SenderVideoConstraints` 180 и 720. Экран вторым соединением (`joinGroupCallPresentation`, `presentation:true`), у других — `presentation` с endpoint экрана; верхний слой экрана по ограничениям; `leaveGroupCallPresentation` убирает его и его пакеты.
  - `TestGroupCallTCPOnly` — клиент только с TCP входит по ICE-TCP (пара `tcp4`), звук в обе стороны.
  - `TestGroupCallChannel` — канал: `getGroupCallJoinAs` только сам пользователь, вход от имени канала — `JOIN_AS_PEER_INVALID` (решение владельца); `join_muted`: подписчик слушает, SFU его не пересылает, рука, слово, звук; ссылка выступающего только администратору (`CHAT_ADMIN_REQUIRED` подписчику), вход по ней не подписчика — с правом голоса, по ссылке слушателя — без; сброс даёт новые ссылки; конец с длительностью в канале и `updateGroupCall` вошедшему по ссылке.
  - `TestGroupCallScheduled` — базовая группа: запланированный звонок, `messageActionGroupCallScheduled`, подписка (`schedule_start_subscribed`), вход до старта отклонён, старт — `updateGroupCall` без `schedule_date` и сообщение о начале, звук.
  - `TestGroupCallSFURestart` — `docker compose kill sfu`, стенд возвращается: `checkGroupCall` больше не содержит источник, клиенты входят заново, как tdesktop и iOS (`calls_group_call.cpp:3746-3776`, `PresentationGroupCall.swift:2221-2226`), и снова слышат друг друга; не параллельный: SFU на стенде один.
- Стенд: `test/e2e/run.sh` поднимает `sfu` наложением `docker-compose.sfu.yaml` (образ `pluma-e2e-sfu:$E2E_PROJECT`, удаляется `down`), выпускает сертификаты управления на прогон (`control-certs.sh` в `$E2E_DIR/sfu` и `$E2E_DIR/groupcalls`; файлы SFU открыты на чтение его пользователю 65532), выбирает свободный порт для UDP и ICE-TCP и пишет `SFU_PUBLIC_IPV4=127.0.0.1`, `SFU_UDP_PORT`, `SFU_TCP_PORT` в env стенда; `check_running` следит и за `sfu`. `test/e2e/docker-compose.e2e.yaml` публикует медиапорты только на 127.0.0.1. `docs/ops/testing.md` дополнен.
- Пул телефонов e2e расширен с 400 до 800 (коды 201..208): с новыми тестами полный набор упёрся в 400 и шесть чужих тестов упали на «400 of 400 are taken».

**Что нашли сценарии и что исправлено.**
- **Каждый вход ждал лишнюю секунду.** SFU видит у tgcalls только peer-reflexive кандидатов (клиент своих не шлёт, `GroupNetworkManager.cpp:398-421`), а pion перед выдвижением такой пары ждёт 1 с (`prflxAcceptanceMinWait`, `pion/ice agent_config.go:40`, `selection.go:40-55`). На стенде каждое соединение занимало ровно 1008–1013 мс. Теперь `ice.WithPrflxAcceptanceMinWait(0)` (`app/service/sfu/internal/transport/endpoint.go`), соединение — 7–9 мс; повторное выдвижение по-прежнему переводит на лучшую пару. `TestAudioBothWays` проверяет, что вход быстрее 800 мс; без исправления падает («connected in 1.005s»).
- **После перезапуска SFU повторные входы падали до 30 с.** Во втором прогоне контейнер `sfu` вернулся с другим адресом (172.18.0.7 → 172.18.0.13), а bff продолжал стучаться на старый: распознаватель DNS gRPC повторяет разрешение не чаще раза в 30 с (`grpc internal/resolver/dns/dns_resolver.go:50-52`, `MinResolutionInterval`). Повторный вход клиента получал `GROUPCALL_ADD_PARTICIPANTS_FAILED` (500). Теперь `sfuclient` подключается через `passthrough:///`, и имя узла разрешается при каждом подключении (`app/bff/groupcalls/internal/sfuclient/sfuclient.go`); `TestGroupCallSFURestart` пересоздаёт контейнер (`compose rm --stop --force sfu`), как при сбое и выкладке. Падение зависит от того, успевает ли разрешение имени попасть в окно, пока контейнера нет: повторный прогон без исправления на свежем стенде прошёл, поэтому «тест падает без изменения» здесь подтверждён только первым наблюдением, а не повтором.
- Повторный вход того, кто ещё в звонке (после перезапуска SFU), отдаёт свою строку без `just_joined` и без новой версии — так решено в части B и закреплено её интеграционным тестом `TestGroupCallSfuRestart` (`app/bff/groupcalls/internal/core`). Тест e2e сначала требовал `just_joined` у любого входа; это было неверное ожидание теста, а не ошибка сервера: проверка `just_joined` и версии +1 оставлена у первых входов.

**Проверка (CachyOS).**
- `go build ./...` — успешно.
- `go vet -tags integration,e2e -unreachable=false` пакетов SFU, `pkg/groupcallpeer`, `pkg/tgcallsjson`, `pkg/groupcalls`, `app/bff/groupcalls`, bff, channels, chats, configuration, biz, pluma-admin, `pkg/conf` и `./test/e2e/` — чисто.
- `go test` и `go test -race` тех же пакетов — 25 пакетов `ok`; после правки `sfuclient` ещё раз `-race` для `app/bff/groupcalls/...`, SFU, `pkg/groupcallpeer`, bff — `ok`.
- Интеграционный `app/bff/groupcalls/internal/core` (MySQL, `-race`) — `ok`.
- `go test ./tools/... ./pkg/conf/...` (logaudit, проверки Compose) — `ok`. `make shellcheck` (с `run.sh`) — чисто. `make apicoverage-check` — матрица актуальна.
- `make lint`: в затронутых и новых файлах замечаний нет. Всего 447 замечаний — старый код: в копии на CachyOS нет git (рабочее дерево — worktree), и `new-from-rev` не отсекает старое; в затронутых пакетах только прежние `SA5008` в `pkg/conf/conf.go` и `app/service/biz/biz/internal/config/config.go`, которые не менялись.
- e2e на своём стенде (`E2E_PROJECT=gc`, `E2E_DIR=$HOME/pluma-work/e2e-gc`), свежий `run.sh all`: весь набор `ok` (`test/e2e` 677 с, 212 тестов PASS, `test/conformance` `ok`), из них все семь `TestGroupCall*`. Отдельный прогон только групповых звонков до правок — 7 из 7. Стенд снят `run.sh down`, контейнеры, образы и тома проекта `gc` удалены, каталог стенда удалён.
- Тест падает без изменения: `TestAudioBothWays` без `WithPrflxAcceptanceMinWait(0)` — «participant 1 connected in 1.005s». Про `passthrough` — см. выше.
- Повторный `run.sh test` на уже использованном стенде даёт падения чужих тестов (`TestNewLogin`, `TestRegistrationSwitch` и др.: телефоны первого прогона уже зарегистрированы) — это свойство повторного прогона, набор рассчитан на свежий стенд.

**Отклонения от брифа.**
- Сценарии разбиты не так, как в списке «Сквозные»: `TestGroupCallCreateJoin`, `TestGroupCallVersions` и `TestGroupCallSourcesLookup` вошли в `TestGroupCallSupergroup`; `TestGroupCallMuteRights` — `TestGroupCallMuteAndHand`; `TestGroupCallInviteJoinAsSchedule` разделён на `TestGroupCallChannel` (ссылки, отказ входа от имени канала) и `TestGroupCallScheduled`.
- «Вход от имени канала» проверяется как отказ `JOIN_AS_PEER_INVALID` (решение владельца 2026-11-12, ADR 0060), а не как вход.
- `phone.joinGroupCall` до старта запланированного звонка проверяется только как ошибка, без кода: что делают клиенты с конкретным кодом, не читалось.

**Не сделано и не проверено.**
- Живые клиенты всех четырёх платформ — строки в `TESTING-BACKLOG` («Групповые звонки»).
- Обрыв сети клиента без перезапуска SFU (Wi-Fi → LTE, повторное выдвижение на настоящем libwebrtc) сквозным тестом не покрыт: у pion-клиента нет смены адреса.
- Значение `join_muted` по умолчанию в канале у Telegram не проверено (по клиентам — серверное решение); сейчас звонок в канале создаётся без него, администратор включает.
- Доступность UDP-порта стенда с Mac через colima не проверялась: прогон — на Linux.
- Отладочные сообщения pion в журнале SFU («Failed to read from candidate … closed pipe», «Failed to ping without candidate pairs») идут уровнем info на каждое закрытие соединения — шум, не ошибка; предупреждение pion про `UDPMuxDefault` на неуказанном адресе — при старте.

### Часть D: разбор трёх независимых прочтений (2026-11-12)
Двенадцать замечаний проверены по коду и исходникам клиентов; все подтвердились, все исправлены с тестами, каждый новый тест проверен падающим без своего исправления.

**Что исправлено.**
1. **Повторный вход забирал слово, данное администратором** (`join_muted`). `JoinState` теперь сохраняет слово тому, кто ещё в звонке и входит заново (новое соединение после смены сети или перезапуска SFU; tdesktop входит заново сам, `TD/calls/group/calls_group_call.cpp:3746-3776`). `join_muted` по-прежнему действует на входящих; тот, кто вышел и вернулся, слово не сохраняет (как поведёт себя Telegram — не проверено). Тесты: `TestJoinState` (три новых случая), `TestGivenWordSurvivesRejoin` (MySQL).
2. **Объект базовой группы в рассылке нёс флаги звонка до шага.** `deliver` перечитывает чат после шага, когда шаг меняет `call_active`/`call_not_empty`, и возвращает его для ответа устройству, сделавшему шаг (создание, старт, первый вход, последний выход, завершение). tdesktop применяет флаги каждого полученного чата (`TD/data/data_session.cpp:999-1016`), поэтому старый объект отменял шаг, а после завершения оставлял «идёт звонок». `withRows` удалён. Тест: e2e `TestGroupCallScheduled` (базовая группа) — флаги в рассылке при старте, первом входе, завершении и в ответе на завершение.
3. **`inviteToGroupCall` отказывал всему списку из-за одного не участника.** tdesktop сначала добавляет выбранных не участников в группу и затем приглашает всех, что бы ни вышло с добавлением (`calls_group_invite_controller.cpp:914-925`), отказ куска молча проглатывается (`calls_group_call.cpp:4121-4146`). Теперь не участники пропускаются; `USER_NOT_PARTICIPANT` — только если участников в списке нет. Тест: `TestGroupCallRepairsAndRaces`.
4. **Пакет, потерянный между отправителем и SFU, не запрашивался у отправителя.** `DownTrack` держит места пропусков потока (`keepGap`, до 512 подряд): NACK получателя на пропуск находит исходный номер, из кэша отвечает RTX, иначе NACK уходит отправителю; пришедший повтор (RTX отправителя) или опоздавший пакет встаёт на своё место с теми же номером, меткой времени и picture id (`late`), дубль не пересылается. То же для пакета, придержанного при смене слоя (второй пакет ключевого кадра раньше первого). Смещения переписывания хранятся в записи, так что повтор после смены слоя переписывается как первый раз. Несколько получателей на один пакет дают несколько запросов отправителю; libwebrtc повторяет пакет не чаще раза за RTT (`rtp_packet_history.cc:260-271`, копия Android). Тесты: `TestDownTrackSenderLoss`, `TestDownTrackHeldBack`, `TestNACKAskedOfSender` (настоящие UDP, потеря у отправителя, NACK отправителю, повтор заполняет пропуск).
5. **Оценку канала получателя резали пропуски, которые делает сам SFU (отбор последних говорящих) и потери у отправителя.** Оценка берёт только отчёты по видео, которое SFU шлёт получателю, и считает потери как долю отправленных SFU пакетов (`DownTrack.DownstreamLoss`: накопленные потери и наибольший номер из отчёта против отметок «отправлен» в истории). Звук не учитывается: libwebrtc считает потерей скачок номеров до 50 (`receive_statistics_impl.cc:93, 137`, `rtp_rtcp_config.h:16`, копия Android), при ptime 120 мс (`GroupInstanceCustomImpl.cpp:2716`) это пауза до 6 с. Тесты: `TestDownstreamLoss`, `TestEstimateIgnoresSoundGaps`.
6. **FIR не доходил до отправителя:** у FIR media SSRC равен 0 (`fir.cc:98`), поток — в записях FCI. Теперь ключевой кадр запрашивается по каждой записи. Тест: `TestFIRFromReceiver`.
7. **Без слова можно было показывать экран (со звуком) и включать камеру.** `joinGroupCallPresentation` и `Store.SetPresentation` отказывают `CHAT_ADMIN_REQUIRED` без слова (tdesktop показывает на такой отказ `MutedNoScreen`, `calls_group_call.cpp:2030-2036`); `editGroupCallParticipant` с `video_stopped=false` без слова — тоже (tdesktop сам не даёт, `:2780`). Когда администратор забирает слово, строка получает `video_stopped`, показ экрана снимается и его соединение на SFU закрывается (tdesktop сам выключает то и другое, `:2315-2318`); вход без слова — с `video_stopped`. На SFU выключение (`SetMuted`) теперь держит и камеру: пакеты не пересылаются, дорожки ставятся на паузу, отправителю уходит `idealHeight 0`; при возврате слова видео идёт снова с ключевого кадра. Тесты: `TestApplyEditSelf`, `TestApplyEditOthers`, `TestAdminMuteNoScreen` (MySQL), `TestServerMuteStopsCamera`.
8. **Исключённый или забаненный оставался в звонке.** Сборщик на каждом проходе (5 с) выводит тех, кто больше не видит чат (`leaves_total{reason="access"}`); его клиент получает свой `left` со своим источником, входит заново (`calls_group_call.cpp:2620-2626`) и получает `GROUPCALL_FORBIDDEN`, на который вешает трубку (`:1874-1886`). Обход «кто в звонке, тот видит» в `viewable` и `editGroupCallParticipant` убран; `joinGroupCallPresentation` проверяет доступ. Мгновенного вывода при исключении нет: обработчики `channels`/`chats` в группы звонков не ходят, задержка — до прохода сборщика. Заканчивает ли Telegram звонок исключённому — не проверено. Тест: `TestGroupCallRepairsAndRaces`.
9. **Вход, гонящийся с `discardGroupCall`, оставлял открытую комнату.** `Store.Join` получает узел соединения; при `ErrDiscarded` (и при переезде комнаты, `ErrMoved`) обработчик закрывает комнату, которую открыл. При `ErrMoved` вход повторяется один раз на новом узле. Тест: `TestGroupCallRepairsAndRaces` (завершение в момент входа).
10. **Сборщик мог забыть узел комнаты, в которую только что вошли.** Узел пустой комнаты забывается новым `Store.ForgetEmpty` — только если в звонке никого нет (в транзакции под блокировкой строки звонка); `Store.Join`, найдя пустой узел, записывает свой (комната на нём есть). Тесты: `TestJoinNodeAndForgetEmpty` (MySQL), `TestGroupCallRepairsAndRaces`.
11. **Неудачный `SetMuted` не повторялся.** `Sources` отдаёт `muted` основного соединения (`sfu.proto`, поле 5; код перегенерирован `gen.sh`). Сборщик сравнивает его со словом в строке, перечитывает строку и повторяет `SetMuted` (`groupcalls_mute_repairs_total`); гонку с администратором исправит следующий проход. Тест: `TestGroupCallRepairsAndRaces`.
12. **Остановленный SFU не давал алерта.** Новый `GroupCallsSFUDown`: `absent(up{job="ops.sfu"})` при `groupcalls_sfu_configured > 0` (метрика выставляется, когда у bff есть узлы и запущен сборщик). Строки в `docs/ops/monitoring.md` и `docs/ops/group-calls.md` («Неполадки»).

Прочее: `pkg/groupcallpeer.LoseVideoFrame` (пакет, потерянный по пути к SFU, для тестов); справки метрик `sfu_retransmissions_total` и `groupcalls_leaves_total`; ADR 0059 (§ управления и §10) дополнен; `docs/project/api-coverage.*` перегенерированы (сдвинулись строки обработчиков).

**Решения (подтвердить).**
1. Слово, данное администратором, сохраняется при повторном входе того, кто ещё в звонке; после выхода `join_muted` действует снова.
2. Без слова нет камеры и показа экрана (раньше — решение 3 части A: звук экрана не выключался).
3. Исключённого выводит сборщик в пределах 5 с, а не обработчик исключения.
4. Не участники в `inviteToGroupCall` пропускаются молча.

**Проверка (CachyOS).**
- `go build ./...` — успешно (и после последних правок).
- `go vet -tags integration,e2e -unreachable=false` пакетов `pkg/groupcalls`, `pkg/groupcallpeer`, `app/bff/groupcalls`, `app/service/sfu` и `./test/e2e/` — чисто.
- `go test -race` пакетов `pkg/groupcalls`, `pkg/groupcallpeer`, `pkg/tgcallsjson`, `app/bff/groupcalls`, `app/service/sfu`, `app/bff/bff`, `app/service/biz/biz`, `app/service/biz/channel` — все `ok`; новые тесты комнаты SFU трижды подряд (`-count=3`) — без сбоев.
- Интеграционные (MySQL, `-race`): `pkg/groupcalls` и `app/bff/groupcalls/internal/core` — `ok`.
- `go test ./tools/... ./pkg/conf/...` (logaudit, Compose) — `ok`. golangci-lint v2.15.0 по затронутым пакетам (и с тегами `integration,e2e`) — 0 замечаний. `make apicoverage-check` — матрица актуальна.
- e2e на своём стенде (`E2E_PROJECT=gcfix`, `E2E_DIR=$HOME/pluma-work/e2e-gcfix`), свежий `run.sh all`: `test/e2e` `ok` (694 с, 228 PASS, все семь `TestGroupCall*`), `test/conformance` `ok`. Стенд снят, тома, образ SFU и каталог стенда удалены.
- Тесты падают без исправлений (каждое откачено на копии CachyOS по отдельности): `TestNACKAskedOfSender` (нет NACK отправителю), `TestFIRFromReceiver` (нет PLI), `TestServerMuteStopsCamera` (нет `idealHeight 0`), `TestEstimateIgnoresSoundGaps` («estimate cut by the sound's loss»), `TestGroupCallRepairsAndRaces` — по отдельности без повтора `SetMuted`, без вывода исключённых, со старым приглашением, без закрытия комнаты при гонке с завершением, без записи узла в `Store.Join`; `TestApplyEditSelf` — без запрета камеры. Проверка e2e `TestGroupCallScheduled` без исправления 2 не прогонялась (ещё один стенд); то же поведение проверено логикой `deliver`.

**Не сделано и не проверено.**
- Живые клиенты (tdesktop и Web на CachyOS, iOS, Android) по-прежнему не подключались; строки части D добавлены в `TESTING-BACKLOG`.
- Мгновенный вывод из звонка при исключении из чата (из обработчиков `channels`/`chats`) не сделан: вывод — проходом сборщика, до 5 с.
- Повторный запрос отправителю на один пакет от нескольких получателей не схлопывается на SFU (полагаемся на ограничение libwebrtc «раз за RTT»).
- Пропуск длиннее 512 пакетов запоминается последними 512 местами.

