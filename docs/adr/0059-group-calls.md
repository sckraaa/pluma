# 0059. Групповые звонки: свой SFU на Go (pion), отдельный и переносимый; сквозное шифрование — в конференциях, как в Telegram

- Статус: proposed (2026-11-12). Владелец дал ход реализации 2026-11-12; пункты «Решения, требующие подтверждения» ждут его слова.
- Задача: 093 (`docs/tasks/093-group-calls.md`, ветка `task/093-group-calls`).
- Связано: ADR 0057 (дом и VPS как труба для шифра, §4 — групповые звонки), ADR 0004 и задача 077 (качество звонков, `CALLS_QUALITY`, исследование `docs/tasks/077-research/group.md`), ADR 0041 (всё Premium открыто).
- Обозначения источников:
  - `TD` — `~/pluma-work/reference-src-new/tdesktop/Telegram/SourceFiles`;
  - `AND` — `~/pluma-work/reference-src-new/android/TMessagesProj/src/main/java/org/telegram`;
  - `IOS` — `~/pluma-work/reference-src-new/ios/submodules`;
  - `TWEB` — `~/pluma-work/reference-src-new/tweb/src`;
  - `TGC` — tgcalls (`github.com/TelegramMessenger/tgcalls`) на коммите `1c236c09`, закреплённом в tdesktop (`git ls-tree HEAD Telegram/ThirdParty/tgcalls`), пути от корня репозитория tgcalls;
  - `api.tl` — `TD/mtproto/scheme/api.tl` (LAYER 233; те же конструкторы есть в `third_party/teamgram-proto`).
- **Оговорка об исходниках.** Подмодули tgcalls в `reference-src-new` не выкачаны (во всех трёх клиентах каталоги пусты). Факты `TGC` взяты из выкачки ровно этого коммита (проверено `git rev-parse`, origin — TelegramMessenger/tgcalls). У Android закреплён `545f5ae5`, у iOS — `c1de0882`. `GroupJoinPayloadInternal.cpp` во всех трёх совпадает побайтно. Количество слоёв, ptime 120, ограничение 32 кбит/с, `colibriClass`, `setup "passive"` сверены во всех трёх. Остальное в `GroupInstanceCustomImpl.cpp` у Android и iOS не сверялось. Первый шаг задачи 093 — выкачать подмодули в `reference-src-new`.

## Контекст

### Чего хочет владелец
- Групповые звонки в группах и каналах на всех четырёх клиентах, поведение как в Telegram.
- Качество лучше, чем в Telegram: звук с более высоким битрейтом, более чёткий показ экрана.
- Сквозное шифрование там, где оно возможно.
- SFU — отдельный компонент, который ставится на другую машину сменой конфигурации (ADR 0057 §4). В будущем ядро работает дома, а VPS несёт только шифр.

### Сервер сегодня
- Групповых звонков нет:
  - маршрут `"/mtproto.RPCGroupCalls"` закомментирован (`teamgramd/etc/session.yaml:113`, `teamgramd/etc2/session.yaml:113`, `app/interface/session/etc/session.yaml:93`);
  - для `RPCConferenceCalls` маршрута нет;
  - в `api-coverage` все методы `missing`.
- Состояние звонка в чатах берётся из заготовок upstream. `GetChatCallActiveAndNotEmpty` и `GetChatGroupCall` объявлены в `app/service/biz/chat/plugin/plugin.go:28-29` и вызываются из `chat_tx.go:132-133` и `chat_cache.go:95-96`; `GroupcallDefaultJoinAs` в `messages.getFullChat_handler.go:88` всегда `nil`.
- Методы по сервисам (`third_party/teamgram-proto/mtproto/rpc_client_registers.go`):
  - `RPCGroupCalls` — create, join, leave, check, get, getParticipants, editParticipant, editTitle, toggleSettings, discard, invite, exportInvite, joinAs, startScheduled, toggleStartSubscription, presentation, record, stream и сообщения в звонке;
  - `RPCConferenceCalls` — createConferenceCall, deleteConferenceCallParticipants, sendConferenceCallBroadcast, inviteConferenceCallParticipant, declineConferenceCallInvite, getGroupCallChainBlocks, sendGroupCallEncryptedMessage.
- Образцы в коде:
  - звонки один на один — `pkg/phonecalls` (каждый переход — одно условное UPDATE, `store.go`) и `app/bff/voipcalls` (рассылка через `SyncUpdatesMe`/`NotMe`/`PushUpdates`, `core.go:227-245`, сборщик `sweeper.go`);
  - TURN — сервис `turn` (задача 077).

### Протокол клиентов: что обязан говорить SFU
SFU Telegram закрыт. Протокол восстановлен по клиентам.

**Сигнализация в MTProto.**
- Клиент шлёт `phone.joinGroupCall … params:DataJSON` (`api.tl:3130`) с таким JSON (`TGC tgcalls/group/GroupJoinPayloadInternal.cpp:302-344`):
  - `ssrc`;
  - `ufrag`, `pwd`;
  - `fingerprints[{hash,fingerprint,setup}]`;
  - `ssrc-groups[{semantics:"SIM"|"FID",sources}]` — только при видео.
- Кодеков, расширений и кандидатов клиент не шлёт.
- Слои видео (`TGC …/GroupInstanceCustomImpl.cpp:3706-3712`):
  - камера — 3 слоя;
  - экран — 2;
  - конференция — 1.
  - SSRC видео идут от `audio+1` парами (слой, RTX).
- Ответ приходит в `updateGroupCallConnection.params` (`api.tl:418`) **внутри Updates, которые вернул сам `joinGroupCall`**:
  - iOS без `updateGroupCall` и `updateGroupCallConnection` в результате считает вход неудавшимся (`IOS/TelegramCore/Sources/TelegramEngine/Calls/GroupCalls.swift:786-829`);
  - Android берёт параметры из результата (`AND/messenger/voip/VoIPService.java:2155-2160`);
  - tdesktop — `TD/calls/group/calls_group_call.cpp:2393-2440`.
- Разбор ответа — `TGC …/GroupJoinPayloadInternal.cpp:41-371`:
  - объект `transport{ufrag,pwd,fingerprints,candidates[…]}` обязателен;
  - все поля кандидата обязательны и передаются строками: без одного поля разбор падает и звонок не соединяется (`:56-172`);
  - `video{endpoint,server_sources,payload-types,rtp-hdrexts}` необязателен для нативных клиентов.
- Web объявляет `video` обязательным полем, а `audio` — необязательным (`TWEB/lib/calls/types.d.ts:73-78`) и строит SDP из `payload-types` сервера (`077-research/group.md`, «Web media parameters»). Поэтому сервер всегда шлёт оба раздела.

**Транспорт.**
- Нативный клиент — всегда ICE-controlled, удалённую сторону считает полным ICE-агентом, своих STUN и TURN не имеет (`TGC …/GroupNetworkManager.cpp:398, 419-421`).
- Его роль в DTLS — сервер (`:434`), SRTP только в режимах AEAD-GCM (`:322-326`).
- Канал данных SCTP «data», id 0, открывает клиент (`SctpDataChannelProviderInterfaceImpl.cpp:30-41`).
- Значит, для нативных клиентов SFU — controlling-сторона и DTLS-клиент. Тестовый SFU в самом репозитории tgcalls (`tools/go_sfu`, на pion) так и делает:
  - ICE-lite плюс `Dial` для нативных клиентов;
  - `Accept` для клиентов на PeerConnection — Web и эталонного движка iOS (`tools/go_sfu/participant.go:121-135, 218-235`; `sfu.go:147-150`).
- Роль приходится выбирать для каждого участника отдельно.

**Управление видео — диалект Jitsi Colibri v1.**
- tgcalls прямо ссылается на Jitsi Videobridge (`TGC …/GroupInstanceCustomImpl.cpp:1097-1099`).
- Клиент шлёт `ReceiverVideoConstraints` с `onStageEndpoints` и `constraints{endpoint:{minHeight,maxHeight}}`: высоты 180/360/720, при открытии канала, при изменениях и раз в 5 с (`:3511-3578`, `:2997-3006`; Web — `TWEB/lib/calls/groupCallConnectionInstance.ts:682-713`).
- Клиент понимает `SenderVideoConstraints{videoConstraints{idealHeight}}`: по нему включает и выключает свои слои (`:3330-3440`).
- Текущий JVB говорит на Colibri2 и использует `onStageSources` (https://github.com/jitsi/jitsi-videobridge/blob/master/doc/allocation.md). Готового SFU, понимающего tgcalls, нет.

**RTP.**
- Расширения с фиксированными id (`:1586-1600, 1716-1719`):
  - 1 — audio-level;
  - 2 — abs-send-time;
  - 3 — transport-cc;
  - 13 — rotation.
- Opus: PT 111, ptime 120, 32 кбит/с. Полевой эксперимент `WebRTC-Audio-Allocation/min:32kbps,max:32kbps` (`:2175`, `:2713-2725`).
- Пакеты с неизвестным SSRC отбрасываются (`WebRTC-Video-DiscardPacketsWithUnknownSsrc`, `:2181`). Повторная выдача из буфера закомментирована (`:3505-3518`).
- Неизвестный SSRC клиент разрешает через `phone.getGroupParticipants(sources=[…])` (`TD/data/data_group_call.cpp:897-905, 1036-1043`).
- Входящих аудиоканалов у клиента около 11 (`:4023-4040`).

**Версии участников.**
- Вход и выход `versioned` и увеличивают `version` на 1. Прочие изменения несут текущую версию.
- Разрыв версий клиенты закрывают перечитыванием: tdesktop через 3 с, `getGroupCall(limit=3)` (`TD/data/data_group_call.cpp:29, 413-441, 600-680`); Android через 1,5 с (`AND/messenger/ChatObject.java:1040-1054`).
- Нулевая версия у tdesktop — ошибка API (`:515-517`).
- Документация: https://core.telegram.org/api/group-calls.

**Восстановление.**
- tdesktop и iOS, пока соединяются, раз в 4 с спрашивают `phone.checkGroupCall([ssrc…])`. Если своего SSRC в ответе нет, они входят заново (`TD/calls/group/calls_group_call.cpp:55, 3746-3776`; `IOS/TelegramCallsUI/Sources/PresentationGroupCall.swift:2221-2226`).
- Android спрашивает весь звонок (`VoIPService.java:2853-2935`).

**Сквозное шифрование.**
- Сквозные только конференции: `phone.createConferenceCall`, ссылки `inputGroupCallSlug`, приглашения, перевод звонка один на один.
- tdesktop включает шифрование только при `conference()` (`TD/calls/group/calls_group_call.cpp:693-698`). iOS для звонка в группе передаёт `keyPair: nil` (`IOS/TelegramCallsUI/Sources/PresentationCallManager.swift:917-919`).
- Кадры шифруются в клиенте до упаковки в RTP (`TGC …/GroupInstanceCustomImpl.cpp:1393-1530`). Открытым остаётся заголовок кодека: H.264 до PPS id, у VP8 10 байт на ключевых кадрах и 1 байт на остальных (`:1097-1110, 1238-1272`).
- Ключи и блокчейн состава описаны на https://core.telegram.org/api/end-to-end/group-calls. Сервер — единственный, кто упорядочивает блоки. Эталон серверной проверки — `ServerBlockchain` в tde2e: прочитаны только заголовки (`~/pluma-work/reference-src-new/ios/third-party/td/TdBinding/SharedHeaders/td/tde2e/td/e2e/Blockchain.h`, `TestBlockchain.h:63-85`), реализация не прочитана.

## Решение

### 1. Свой SFU на Go поверх низкоуровневых пакетов pion
- **SFU пишем сами**, отдельной программой `sfu` (`app/service/sfu`). Это Go, как весь сервер, с теми же `testinfra`, `-race` и `logaudit`.
- **Пакеты pion:**
  - `pion/ice/v4`, `dtls/v3`, `srtp/v3`, `sctp`, `datachannel`, `rtp`, `rtcp` — лицензия MIT;
  - `pion/webrtc` с его PeerConnection не берём: tgcalls говорит не SDP, а JSON.
  - Тот же набор пакетов использует `tools/go_sfu` (`go.mod:5-13`).
- **Образцы:**
  - `tools/go_sfu` — образец совместимости, только для чтения. Это часть репозитория tgcalls под LGPL-3.0, своей лицензии у каталога нет; код не копируем.
  - `pkg/sfu` из LiveKit (Apache-2.0) — образец устройства пересылки и распределения слоёв.
- **Запасной путь.** Если на первом шаге задачи 093 pion не справится с ролью controlling или ICE-TCP, берём JVB за переводчиком. Цена — JVM и Colibri2.

### 2. Разделение на две части
- **Сигнализация: модуль `app/bff/groupcalls`.** Обслуживает `RPCGroupCalls`, позже `RPCConferenceCalls`. Регистрируется в `app/bff/bff/internal/server/server.go`, маршрут прописывается во всех трёх `session.yaml`. Неподдержанные методы отвечают `ErrEnterpriseIsBlocked`.
- **Состояние: `pkg/groupcalls`** — MySQL, по образцу `pkg/phonecalls`.
  - Таблицы: звонки, участники, личные настройки зрителя (громкость, `muted_by_you`), позже блоки цепочки.
  - Вход и выход меняют `version` ровно на +1 в одной транзакции с блокировкой строки звонка. Версия никогда не 0.
  - Уникальность SSRC действует внутри звонка. Повтор отвечает `GROUPCALL_SSRC_DUPLICATE_MUCH`, и клиент входит заново со свежим SSRC.
- **Медиа живут только в SFU** и в базе не хранятся.

### 3. Управление SFU и размещение (ADR 0057)
**Управление идёт по gRPC.**
- Методы:
  - `CreateRoom(call, e2e)` и `CloseRoom`;
  - `Join(call, participant, kind{main|presentation}, client_json) → answer_json`;
  - `Leave`;
  - `SetMuted` — SFU не пересылает звук участника, которому выключили микрофон;
  - `Sources(call)` — SSRC, которые SFU действительно держит;
  - поток `Events`: подключился, отвалился, потерял согласие ICE, уровни говорящих, используемые слои.
- Адрес задаётся в конфигурации прямыми адресами, без etcd: SFU может стоять на другой машине, а etcd ядра наружу не выходит.
- Соединение защищено mTLS: управление может идти через чужую сеть.

**Узлы.**
- В конфигурации — список узлов SFU. У каждого задано, какие звонки ему можно доверить:
  - `plaintext: true` — узел в доверенной зоне ядра, он принимает обычные звонки групп и каналов;
  - `plaintext: false` — только сквозные конференции.
- bff выбирает узел при создании комнаты и записывает его в строку звонка.
- Комната живёт на одном узле. Каскада между узлами нет: при ~100 пользователях хватает одного.

**Публичные адреса.**
- Публичные адреса кандидатов (IPv4, IPv6, UDP-порт, TCP-порт) задаются в конфигурации узла.
- Это может быть адрес VPS, который только пересылает UDP и TCP домой и не снимает DTLS-SRTP. Так ADR 0057 §2 разрешает «трубу» и для обычных звонков: SFU с открытыми медиа стоит дома, VPS видит только SRTP.

**Сегодня** ядро на CX33. SFU стоит рядом с ним (`plaintext: true`), отдельной службой Compose с сетью хоста для медиапортов.

### 4. Транспорт SFU
- **ICE.** Роль выбирается для каждого участника по атрибуту первого входящего запроса проверки (`ICE-CONTROLLED` или `ICE-CONTROLLING`):
  - для нативных клиентов SFU — controlling и сам выдвигает пару;
  - для Web и эталонного движка iOS — controlled.
  - Повторное выдвижение (renomination) включено, чтобы переход Wi-Fi → LTE не требовал повторного входа (`077-research/group.md`, «Path migration»).
- **Порты.** Один мультиплексированный UDP-порт для IPv4 и IPv6. Плюс пассивный ICE-TCP на настраиваемом порту: у групповых звонков в стоковых клиентах нет TURN, и TCP — единственный запасной путь.
  - Порт 443 адреса Pluma занят Caddy (`docs/ops/deployment.md:89`). Для ICE-TCP на 443 нужен второй адрес, иначе — другой порт.
  - `tcptype` нативный клиент разбирает, но в кандидат не передаёт (`TGC …/GroupInstanceCustomImpl.cpp:3863-3875`). Работа TCP проверяется на всех клиентах до того, как на неё полагаться.
- **DTLS.** SFU — DTLS-клиент и отвечает `setup:"active"`. SRTP — `AEAD_AES_128_GCM` и `AEAD_AES_256_GCM`.
- **Канал данных.** SFU принимает DCEP-открытие канала «data» на SCTP 5000/5000.

### 5. Пересылка медиа
- **Порядок.** Сначала рассылается `updateGroupCallParticipants` с `source` и `video.source_groups`, потом SFU начинает пересылать новый SSRC. Иначе клиент отбросит пакеты.
- **Звук.** Пересылаются последние говорящие по расширению 1 (RFC 6464): не больше 5 одновременно (настраивается), с удержанием, при ~11 каналах у клиента. Звук участника с выключенным микрофоном не пересылается вовсе.
- **Видео.**
  - Каждому получателю идёт один слой simulcast от каждого отправителя.
  - Слой переписывается в SSRC слоя 0, с непрерывными номерами пакетов и метками времени (`tools/go_sfu/CLAUDE.md`, «Layer Selection and SSRC Rewriting»; в коде tgcalls это не проверено).
  - Слой выбирается по `ReceiverVideoConstraints` получателя и по оценке его канала.
  - Отправителю уходит `SenderVideoConstraints` с наибольшей высотой, которую кто-то смотрит, чтобы неиспользуемые слои не кодировались.
- **Повторы и запросы кадров.** Ответы на NACK — из кэша пакетов каждого потока, через RTX. PLI и FIR пересылаются отправителю с ограничением частоты.
- **Обратная связь.**
  - Отправителям SFU шлёт TWCC каждые 100 мс.
  - На пересылаемые пакеты SFU ставит расширение 3 и сам оценивает канал каждого получателя.
  - Задача 093, этап 1 — оценка по потерям из отчётов получателя; этап 2 — GCC по задержке.
- **Ответ SFU.**
  - `transport` (все поля кандидатов строками);
  - `audio` (Opus 111/48000/2, `useinbandfec`);
  - `video` с полями:
    - `endpoint` — тот же id, что в сообщениях Colibri и в `groupCallParticipantVideo.endpoint`;
    - `server_sources`;
    - VP8 100 / rtx 101 — нативные клиенты назначают id сами в этом порядке (`TGC …/GroupInstanceCustomImpl.cpp:299-350`), а Web берёт их из ответа;
    - расширения 1/2/3/13.
  - `stream:true` не отдаётся никогда.

### 6. Поведение для клиентов
- **Результаты.** `create`, `join`, `joinGroupCallPresentation`, а позже и `createConferenceCall` возвращают `updateGroupCall`, `updateGroupCallConnection` и своего участника.
- **Объекты участников.** Чужие объекты рассылаются как `min`; объект самого пользователя и личные поля (`muted_by_you`, личная громкость) — только ему.
- **Права.**
  - Создатель чата и администраторы с `manage_call` (`api.tl:1304`) создают, завершают и меняют настройки звонка, выключают микрофон другим и задают им громкость (`volume_by_admin`).
  - Выключение и громкость от остальных — личные (`muted_by_you`).
  - Включить себе микрофон можно только при `can_self_unmute`.
  - `raise_hand_rating` назначает сервер.
- **Рассылка.**
  - `updateGroupCall` с `peer` получают участники чата.
  - `updateGroupCallParticipants` получают участники звонка.
  - У чата и канала выставляются `call_active` и `call_not_empty`, у chatFull и channelFull — `call`.
  - Служебные сообщения: `messageActionGroupCall` (начало и конец с длительностью), `messageActionInviteToGroupCall`, `messageActionGroupCallScheduled`.
  - Точное правило рассылки Telegram не проверено. Разрывы клиенты закрывают перечитыванием, поэтому лишняя или недостающая рассылка не ломает состояние.
- **`checkGroupCall`** отвечает по `Sources` из SFU, а не по базе.

### 7. Сквозное шифрование
- **Конференции — сквозные, как в Telegram.** Сервер упорядочивает обе подцепочки:
  - принимает блок только на `height+1` от последнего хэша;
  - проверяет подписи, права, `state_proof` и правило «блок входа добавляет только входящего»;
  - отдаёт блоки с ID конструктора +1;
  - отвечает `CONF_WRITE_CHAIN_INVALID`, `BLOCK_INVALID`, `GROUPCALL_FORBIDDEN`;
  - рассылает `updateGroupCallChainBlocks`.
- **Как проверять блоки** — порт на Go или tde2e через cgo либо отдельную службу. Это решит отдельный ADR перед этапом конференций, после чтения исходников tde2e.
- **SFU конференций** может стоять на VPS (`plaintext: false`). Он видит заголовки RTP, SSRC, открытый префикс кадра, размеры и время, но не содержимое.
- **Обычные звонки групп и каналов** в стоковых клиентах не сквозные. Сделать их сквозными можно только патчем всех клиентов; без решения владельца этого не делаем.
- **Доверие к серверу.** Сервер решает, кого впустить в конференцию. От подставного участника защищают видимый список и эмодзи (commit-reveal во второй подцепочке).

### 8. Качество
- **Без патчей клиентов:**
  - iOS берёт битрейт звука из appConfig `voice_chat_send_bitrate` (`IOS/TelegramCallsUI/Sources/PresentationGroupCall.swift:1856`); сервер отдаёт 64 после проверки;
  - Web берёт параметры Opus из ответа;
  - на стороне SFU — NACK/RTX, выбор слоя для каждого получателя, ICE-TCP, повторное выдвижение пары.
- **Патч tgcalls** (продолжение этапа 2 задачи 077, значения из appConfig по образцу `CALLS_QUALITY`):
  - Opus 20 мс и 48–64 кбит/с: снять эксперимент `min:32,max:32` и потолок 32 кбит/с без видео (`:2175`, `:3190-3194`);
  - верхний слой камеры около 2,5 Мбит/с, общий потолок выше 1052 кбит/с (`:3180-3187`, `:2600-2630`);
  - экран 1080p30 до 4 Мбит/с, верхний слой сейчас 1 Мбит/с (`:2558-2592`);
  - simulcast в конференциях (сейчас 1 слой).
- **Сквозное шифрование** добавляет к кадру заголовки эпох и подпись, по оценке ~150 байт. Битрейт звука в конференциях выбирается с этим запасом (оценка по формату, не замер).

### 9. Пределы
- Участников в звонке — 100, вся аудитория.
- Видео одновременно (`unmuted_video_limit`, appConfig `groupcall_video_participants_max`, `AND/messenger/MessagesController.java:3172`) — из конфигурации узла: 30 на VPS. Для дома значение выбирает владелец: при 60 Мбит/с отдачи это примерно 8 получателей видео Pluma-качества или 20 стокового (оценка по потолкам tgcalls, не замер).
- `conference_call_size_limit` (`:4803`) — 100.
- Экран — один на участника.
- При нехватке отдачи узла SFU понижает слои, а не отказывает во входе. Лимит отдачи узла задаётся в конфигурации.

### 10. Сбои и перезапуск
- **bff.** Состояние в MySQL, перезапуск bff незаметен.
- **SFU.** Перезапуск или потеря узла уничтожают комнаты, и `Sources` их не находит.
  - Клиент по `checkGroupCall` не видит свой SSRC и входит заново.
  - `Join` пересоздаёт комнату на том же узле или на другом подходящем; `sfu_node` меняется условным UPDATE.
- **Участник.** Отключение, потеря согласия ICE или отсутствие в SFU дольше настраиваемого срока помечают его `left` с увеличением версии.
- **Сборщик** по образцу `voipcalls/sweeper.go`:
  - выводит отвалившихся участников;
  - закрывает пустые комнаты;
  - снимает `call_not_empty`.
- **Обычный звонок** без участников остаётся активным до `discardGroupCall`, как в Telegram. Срок Telegram не проверен; автозавершение пустого звонка — по конфигурации, по умолчанию выключено.
- **Порядок шагов:** транзакция состояния → SFU → рассылка. Сбой рассылки пишется в лог, клиенты закрывают разрыв перечитыванием, как у звонков один на один (`core.go:225-226`).

### 11. Метрики и журнал
- **SFU:**
  - комнаты и участники;
  - байты и пакеты по видам (звук, камера, экран);
  - слой на получателя и переключения;
  - NACK, RTX, PLI, потери;
  - оценка канала;
  - тип пары ICE (udp/tcp, v4/v6);
  - сбои ICE и DTLS;
  - время входа;
  - отдача узла против лимита.
- **bff:**
  - входы и выходы;
  - ошибки по кодам;
  - перечитывания при разрывах;
  - позже — принятые и отклонённые блоки.
- **`logaudit`:** никаких ключей, отпечатков DTLS с привязкой к номеру, содержимого блоков и телефонов.

## Этапы
1. **Звонки в группах и каналах** (задача 093, подробно в брифе).
   - Первый шаг — проверка транспорта pion с tdesktop на CachyOS.
   - Дальше на всех четырёх клиентах:
     - звук, камера и экран;
     - выключение микрофона и «поднять руку»;
     - действия администратора;
     - ссылки и приглашения;
     - «войти от имени»;
     - запланированные звонки.
   - SFU стоит рядом с ядром. Качество стоковое, у iOS 64 кбит/с через appConfig после проверки.
2. **Адаптация к сети.** GCC по задержке, проверка ICE-TCP на реальных сетях, настройка числа последних говорящих, замеры против Telegram.
3. **Качество лучше Telegram.** Патч tgcalls на трёх нативных клиентах и параметры для Web.
4. **Сквозные конференции.**
   - Отдельный ADR о проверке блоков.
   - createConferenceCall, ссылки, приглашения со звонком, перевод звонка один на один в конференцию.
   - SFU на VPS.
5. **Сообщения в звонке** (`messages_enabled`, `phone.sendGroupCallMessage`): после чтения клиентов.

## Альтернативы
- **JVB за переводчиком.** Ближе всего по происхождению. Но нужны JVM, перевод Colibri v1 в Colibri2 через Jicofo, и неизвестно, понимает ли JVB ещё старые сообщения по endpoint так, как их ждёт tgcalls. Отклонено как основной путь, оставлено запасным.
- **LiveKit, mediasoup, Janus, ion-sfu.** Ни один не говорит на протоколе tgcalls, переводчик нужен в любом случае.
  - LiveKit — своя сигнализация на protobuf.
  - mediasoup — Node и C++.
  - Janus — GPL и модель плагинов.
  - Как образец кода берём LiveKit, но не как основу.
- **SFU внутри bff.** Нельзя вынести на другую машину, нарушает ADR 0057 §4. Отклонено.
- **Сквозное шифрование обычных звонков патчем клиентов.** Расходится с Telegram и с правилом минимальных правок клиентов. Решает владелец.

## Последствия
- Новые компоненты:
  - служба `sfu` (образ, служба Compose, `docs/ops/group-calls.md`);
  - модуль `app/bff/groupcalls`;
  - пакет `pkg/groupcalls`;
  - миграция.
- Новые зависимости pion в `go.mod`.
- Открываются порты: UDP SFU и TCP для ICE-TCP.
- Отдача сервера растёт на величину видео в звонках. На CX33 это укладывается в канал; месячная квота трафика VPS не проверена.
- Для e2e нужен свой тестовый клиент tgcalls на Go (`pkg/groupcallpeer`), он же служит для отладки.

## Что не делается
- **RTMP и режим трансляции** (`stream_dc_id`, `getGroupCallStreamChannels`, `getGroupCallStreamRtmpUrl`, части `inputGroupCallStream`) — `ErrEnterpriseIsBlocked`, `rtmp_stream` не выставляется. Формат частей (`TG_META`, контейнер `0xa12e810d`) прочитан не полностью.
- **Запись звонков** (`toggleGroupCallRecord`) — требует расшифровки на сервере и хранилища. Решает владелец.
- Платные сообщения в звонке (`getGroupCallStars`).
- Каскад SFU и перекодирование.
- TURN для групповых звонков: в стоковых клиентах его нет (`GroupNetworkManager.cpp:398`).
- Сквозное шифрование обычных звонков без решения владельца.

## Решения, требующие подтверждения владельцем
1. Свой SFU на pion, а не JVB.
2. Сквозное шифрование — только в конференциях, как в Telegram.
3. Лимит видео одновременно для дома (при 60 Мбит/с) и для VPS (30).
4. ICE-TCP: второй IPv4 для порта 443 или другой порт.
5. Запись и RTMP: делать ли когда-нибудь.

## Не проверено
- tgcalls у Android и iOS на их закреплённых коммитах сверен только по перечисленным файлам.
- Роль ICE у Web в Chrome: в SDP Web строка `a=ice-lite` закомментирована (`TWEB/lib/calls/sdpBuilder.ts:89`).
- API pion для ICE-TCP, повторного выдвижения и выбора роли по первому запросу.
- Есть ли в сквозном режиме полезный уровень звука в расширении 1. По `TGC …/GroupNetworkManager.cpp:60-80` уровень огрубляется до 0/106/127; где включается огрубление — не прочитано.
- Правило рассылки, срок вывода отвалившихся и завершение пустых звонков у Telegram.
- Поведение клиентов при `ErrEnterpriseIsBlocked` на запись и трансляцию.
- Смысл `offset` у второй подцепочки и реализация tde2e.
- `Endpoints` в конфигурации клиента go-zero вместо etcd.
