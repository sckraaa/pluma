# 094. Групповые звонки, этап 2: сквозное шифрование каждого звонка

- Статус: в работе (2026-11-12; решения владельца 2026-11-12: каждый групповой звонок сквозной, RTMP убрать, обработки звука на сервере нет).
- ADR: 0060 (решение), 0059 (SFU, этап 1; §7 заменён 0060), 0057 (SFU на VPS).
- Зависит от: 093 (этап 1: `pkg/groupcalls`, `app/bff/groupcalls`, служба `sfu`, `pkg/tgcallsjson`, `pkg/groupcallpeer`).
- Ветка: `task/094-group-calls-e2e` от `task/093-group-calls`. Клиенты — ветки `task/094-group-calls-e2e` в `pluma-android`, `pluma-ios`, `pluma-desktop`, `pluma-web`.
- Источники и обозначения (`TD`, `AND`, `IOS`, `TWEB`, `TGC`, `TDE`, `GC`, `api.tl`) — как в ADR 0060.
- **Машина сборки CachyOS выключена** (2026-11-12). Код сервера пишется на Mac без сборки, проверяется в GitHub CI на пушнутой ветке (`.github/workflows/ci.yml`: build, vet, unit, race, lint, integration, e2e). Клиенты проверяются в CI своих репозиториев. Всё, что требует CachyOS, — живые клиенты, дифференциальный тест с настоящим tde2e, полный прогон партии — ждёт её включения и записано ниже отдельно.

## Цель
Любой групповой звонок Pluma — в группе, в супергруппе, в канале — сквозной на всех четырёх клиентах. Сервер упорядочивает и проверяет цепочку блоков, но не знает ключей. SFU пересылает только шифр и может стоять на чужой машине. RTMP нет ни в клиентах, ни на сервере. Рассинхрон ключей виден оператору без доступа к ключам и медиа, а клиенты восстанавливаются сами.

## Область
**Трогаем:**
- сервер: `pkg/tde2e` (новый), `pkg/groupcalls`, `app/bff/groupcalls`, служба `sfu`, `pkg/groupcallpeer`, `tools/pluma-admin`, `tools/logaudit`, миграции, маршрут `RPCConferenceCalls`, правила Prometheus, документы;
- клиенты: правки в `ci/patch.py` четырёх репозиториев и записи в реестрах `clients/patches/*.md`.

**Не трогаем:**
- конференции как продукт (`createConferenceCall`, ссылки, приглашения, перевод звонка 1:1) — этап «конференции» ADR 0059;
- сообщения в звонке;
- качество (патч tgcalls этапа 3 ADR 0059);
- GCC.

## Контракт (главное; подробно — ADR 0060)
- **Вход.** `phone.joinGroupCall` обязан нести `public_key` и `block`.
  - На пустой цепочке — нулевой блок (высота 0, в составе только вызывающий; `TD/calls/group/calls_group_call.cpp:1633-1650`).
  - Иначе — блок на `height+1`, где состав = прежний минус старая запись вызывающего плюс он с новым ключом (`TDE Call.cpp:600-608`).
  - Ответ — `updateGroupCall`, `updateGroupCallConnection`, свой участник и `updateGroupCallChainBlocks` подцепочки 0, который кончается блоком входа (`calls_group_call.cpp:1797-1806`).
- **Магия +1.** Каждый отдаваемый блок — с первым int32 + 1 (`TDE Blockchain.cpp:742-760`).
- **`getGroupCallChainBlocks(call, sub_chain_id, offset, limit)`:**
  - `offset` — высота (в подцепочке 1 — сквозной номер), `-1` — последний блок;
  - `next_offset` — индекс после последнего;
  - не участник получает только последний блок (`GC`).
- **`deleteConferenceCallParticipants(only_left|kick, ids, block)`** — состав минус ровно `ids`. Первый блок принимается, повторы получают `CONF_WRITE_CHAIN_INVALID`. Удалённый получает `GROUPCALL_FORBIDDEN`.
- **`sendConferenceCallBroadcast(call, block)`** — commit и reveal по правилам `TDE Call.cpp:96-245`.
- **Ошибки:**
  - `CONF_WRITE_CHAIN_INVALID` — высота или хэш устарели;
  - `BLOCK_INVALID`;
  - `GROUPCALL_FORBIDDEN`;
  - `GROUPCALL_PARTICIPANTS_TOO_MUCH` (100);
  - `JOIN_AS_PEER_INVALID` на `join_as` ≠ сам.
- **RTMP:** `rtmp_stream` при создании отклоняется; `"stream":true`, `rtmp_stream`, `stream_dc_id` не выдаются никогда.

## Порядок работ
1. **Исходники** (на Mac, без сборки).
   - Сверить tde2e у Android (`jni/prebuild`, версия сборки) и tdesktop (td `51743df` из `Telegram/build/prepare/prepare.py:1978-1981`) с `e894536b`: выкачать `51743df` разреженно в `~/pluma-work/reference-src-new/tdlib-td`, сравнить `tde2e/td/e2e/` и `e2e_api.tl`.
   - Прочитать `tdutils/td/utils/Ed25519.cpp` (`compute_shared_secret`).
   - Прочитать tgcalls iOS `c1de0882` в местах шифрования кадров.
   - Прочитать полный путь повторного входа в `AND/messenger/voip/VoIPService.java`.
   - Прочитать, как четыре клиента показывают `GROUPCALL_INVALID` и `GROUPCALL_FORBIDDEN` при входе. Отсюда выбирается ошибка для клиента без ключа (решение 8 ADR 0060).
   - Выводы записать в «Результат»; расхождения с ADR 0060 — в ADR.
2. **`pkg/tde2e` — порт проверки на Go.**
   - Разбор и сериализация `e2e_api.tl` (блок, изменения, состав, общий ключ, state proof, commit, reveal).
   - Хэш блока, подпись Ed25519, trie и `kv_hash`, применение состояния с правилами `Blockchain.cpp:368-490, 540-556, 671-693`.
   - Проверка commit и reveal.
   - Отдельный подпакет для клиентской стороны, которую использует только тестовый клиент: расшифровка общего ключа, `encrypt_header`/`decrypt_data` (`TDE MessageEncryption.cpp:23-170`), шифрование кадров (`Call.cpp:344-494`), хэш эмодзи (`Call.cpp:63-95`).
   - Векторы из `tde2e/test/EncryptionTestVectors.h`, `encryption_test.py`.
3. **Хранение.**
   - Миграция `teamgramd/deploy/sql/migrate-20261118-group-call-chain.sql`, повторяемая:
     - таблица `group_call_chain_blocks`;
     - столбцы цепочки в `group_calls`: `chain_height`, `chain_hash`, `chain_state`, `broadcast_count`.
   - `pkg/groupcalls/chain.go`: `AppendMainBlock`, `AppendBroadcast`, `Blocks`, `LastBlock` в транзакции с блокировкой строки звонка вместе с версией участников.
4. **bff.**
   - `joinGroupCall` требует ключ и блок, проверяет политику входа, запрещает `join_as` ≠ сам; `getGroupCallJoinAs` отдаёт только пользователя.
   - Новые обработчики `RPCConferenceCalls`: `getGroupCallChainBlocks`, `sendConferenceCallBroadcast`, `deleteConferenceCallParticipants`.
   - Остальные методы сервиса (`createConferenceCall`, `inviteConferenceCallParticipant`, `declineConferenceCallInvite`, `sendGroupCallEncryptedMessage`) отвечают `ErrEnterpriseIsBlocked`.
   - Регистрация `RPCConferenceCalls` в `app/bff/bff/internal/server/server.go` и маршрут `"/mtproto.RPCConferenceCalls": "bff.bff"` в трёх `session.yaml`.
   - Рассылка `updateGroupCallChainBlocks` участникам.
   - Учёт опроса (высота и время по участнику и подцепочке) — в памяти bff, со сбросом при выходе.
   - Отказ `rtmp_stream`; тест, что в ответе на вход нет `stream`.
5. **SFU.**
   - Комнаты всегда сквозные; `Plaintext` убрать из `app/service/sfu/internal/config` и `app/bff/groupcalls/internal/config` (выбор узла).
   - Разбор `header_a` в пакетах звука: эпоха отправителя в метриках и событиях.
   - Приём `colibriClass: "PlumaE2EStatus"` с проверкой схемы и передача в поток `Events` (`sfu.proto`).
6. **Диагностика.**
   - Метрики уровня звонка и правила тревог (таблица ADR 0060 §5) в `teamgramd/deploy/prometheus/server/rules/`.
   - `pluma-admin calls show <call>`: состав цепочки, высоты, опрос, эпохи отправки, отчёты, ход эмодзи.
   - Правила `logaudit`: никаких байтов блоков, `public_key` вместе с номером, nonce, отчётов целиком.
7. **Тестовый клиент.** Сквозной режим `pkg/groupcallpeer` на `pkg/tde2e` и режим «сломанный участник» (ADR 0060 §6).
8. **Тесты сервера** (раздел «Тесты»). Пуш ветки, зелёный CI.
9. **Патчи клиентов** (раздел «Патчи клиентов»), каждый со своими проверками `verify_*`/`DAMAGE`, зелёным CI своего репозитория и записями в реестре.
10. **Документы.**
    - `docs/ops/group-calls.md`: SFU на любом узле, тревоги, `pluma-admin calls show`, что делать при петле повторных входов.
    - `go run ./tools/apicoverage` — в CI или на CachyOS.
    - `STATUS`, строки `TESTING-BACKLOG`, «Результат».
11. **CachyOS, когда включится:**
    - дифференциальный тест порта против настоящего tde2e;
    - полный прогон партии;
    - живые клиенты со скриншотами (раздел «Проверки»).

## Файлы сервера
**Добавить:**
- `pkg/tde2e/` — `tl.go` (схема `e2e_api.tl`), `block.go`, `state.go`, `trie.go`, `bitstring.go`, `broadcast.go`, `verify.go`, тесты и `testdata/` с векторами.
- `pkg/tde2e/peer/` — клиентская сторона для тестового клиента: `keys.go`, `encryption.go`, `frame.go`, `emoji.go`, тесты.
- `tools/tde2ediff/` — дифференциальный тест с cgo против tde2e, под build-тегом, по образцу сборки `codes` (`teamgramd/deploy/codes/Dockerfile:24`). Запускается только на CachyOS.
- `teamgramd/deploy/sql/migrate-20261118-group-call-chain.sql`.
- `pkg/groupcalls/chain.go`, `chain_integration_test.go`.
- `app/bff/groupcalls/internal/core/`:
  - `phone.getGroupCallChainBlocks_handler.go`;
  - `phone.sendConferenceCallBroadcast_handler.go`;
  - `phone.deleteConferenceCallParticipants_handler.go`;
  - `chain.go` (общая запись и рассылка);
  - `e2estatus.go` (приём отчётов из событий SFU, метрики).
- `app/service/sfu/internal/room/e2e.go` — разбор `header_a` и отчётов.
- `test/e2e/group_calls_e2e_test.go`.

**Изменить:**
- `app/bff/groupcalls/internal/core/phone.joinGroupCall_handler.go` (сейчас `public_key`/`block` дают отказ, `:39`), `phone.createGroupCall_handler.go`, `phone.getGroupCallJoinAs_handler.go`, `not_served.go`, `metrics.go`, `sweeper.go` (удаление блоков после завершения, тревога «в составе, но не в звонке»).
- `app/bff/groupcalls/internal/config/config.go` и `app/service/sfu/internal/config` — убрать `Plaintext`; конфигурации `teamgramd/etc*/bff.yaml`, `app/service/sfu/etc/sfu.yaml`.
- `app/service/sfu/sfu.proto` и сгенерированный код (`sfu/gen.sh`) — событие отчёта и эпохи.
- `app/bff/bff/internal/server/server.go`; `teamgramd/etc/session.yaml`, `teamgramd/etc2/session.yaml`, `app/interface/session/etc/session.yaml`.
- `pkg/groupcallpeer/peer.go`.
- `tools/pluma-admin` (команда `calls`), `tools/logaudit/logaudit.go`.
- `teamgramd/deploy/prometheus/server/rules/`, `docs/ops/monitoring.md`, `docs/ops/group-calls.md`.

## Патчи клиентов
Номера берутся в `clients/patches/*.md` при коммите правки, следующие свободные. На 2026-11-12 это A-117, I-122, D-096, W-077; таблица в `clients/patches/README.md` отстаёт: I-121, D-095, W-076 уже заняты. Ниже правки названы по смыслу, без номеров. Новые тексты пользователю — только ключами локализации во всех языках клиента.

**Desktop (`pluma-desktop`, tdesktop `157f5cfc`):**
1. **Сквозной режим звонков чатов.**
   - `GroupCall::e2e()` = `conference() || !rtmp()`.
   - `conference()` → `e2e()` в крипто-точках: `initConferenceE2E` в конструкторе (`TD/calls/group/calls_group_call.cpp:693-698`) и в `startRejoin` (`:1559-1561`), ветка `refreshLastBlockAndJoin` (`:1632-1636`; перед входом всегда спрашивать последний блок).
2. **Удаление ушедших для звонков чатов.** Логика «participants with access / stale» в `TD/data/data_group_call.cpp:71-96` — и для `GroupCallOrigin::Group`. `trackParticipantsWithAccess` (`:806-820`) — через `lookupReal()` вместо `_sharedCall`.
3. **Эмодзи в списке участников:** `conference()` → `e2e()` (`TD/calls/group/calls_group_members.cpp:553, 1768, 1840`).
4. **«Войти от имени»** скрыт в сквозных звонках.
5. **Счётчики и отчёт.**
   - Счётчики расшифровки в `TD/tde2e/tde2e_api.cpp:78-106`, сейчас ошибки там выбрасываются (`:91-100`).
   - Отчёт `PlumaE2EStatus` по каналу данных: tgcalls `GroupInstanceCustomImpl.cpp` рядом с `ReceiverVideoConstraints` (`:2777, 2833`).
6. **Удаление RTMP:**
   - `TD/window/window_peer_menu.cpp:4401-4403, 4428-4433`;
   - `TD/calls/calls_instance.cpp:191, 376-381`, `calls_instance.h:97`;
   - `TD/calls/group/calls_group_rtmp.cpp` из сборки;
   - `TD/calls/group/calls_group_settings.cpp:702-786`;
   - разбор `stream`/`rtmp_stream_*` (`calls_group_call.cpp:116-121`), `f_rtmp_stream` (`:1309`);
   - живые истории (`TD/data/data_stories.cpp:92`, `TD/media/view/media_view_video_stream.cpp`) — после решения 4.

Сообщения в звонке (`:2451-2484`, `calls_group_messages.cpp:132, 173`) не трогать: сервер их не обслуживает.

**iOS (`pluma-ios`, `24638bc6`):**
1. **Сквозной режим звонков чатов.**
   - `keyPair: generateKeyPair()` вместо `nil` при `isConference: false` (`IOS/TelegramCallsUI/Sources/PresentationCallManager.swift:917, 1101`).
   - `e2eContext` создаётся и для звонка, созданного внутри (`createGroupCall`, `IOS/TelegramCallsUI/Sources/PresentationGroupCall.swift:922-942, 3219`), после ответа.
2. **«Войти от имени»** скрыт; блок и контекст — от `account.peerId` (`:927, 2054`).
3. **Самовосстановление:** на `ConferenceCallE2EContext.isFailed` (`IOS/TelegramCore/Sources/State/ConferenceCallE2EContext.swift:176-178, 217-219`) — повторный вход, не больше 3 за 5 мин.
4. **Счётчики и отчёт:** `ios/third-party/td/TdBinding/Sources/TdBinding.mm:337-348` и tgcalls `c1de0882` (канал данных).
5. **Удаление RTMP:**
   - `IOS/TelegramUI/Components/PeerInfo/PeerInfoScreen/Sources/PeerInfoScreenCallActions.swift:73-78, 330-352`;
   - `IOS/PeerInfoUI/CreateExternalMediaStreamScreen` — вход;
   - `IOS/TelegramCallsUI/Sources/Components/MediaStreamComponent.swift:605-614`;
   - живые истории `IOS/TelegramUI/Components/ShareWithPeersScreen/Sources/LiveStreamSettingsScreen.swift:142, 194, 248` — после решения 4.

**Android (`pluma-android`, `7be672ea`):**
1. **Сквозной режим звонков чатов.**
   - В пути `startGroupCall` создать `ConferenceCall` с `groupCall.getInputGroupCall()`, передать `true` в `makeGroup`, вызвать `setConferenceCallId` (`AND/messenger/voip/VoIPService.java:3026-3036, 3160-3174`).
   - Разделить `isConference()` (`:308`) на крипто- и UI-признак и разобрать ~45 проверок `conference != null`.
2. **Эмодзи** в `GroupCallActivity` (`:1453, 4933-4936`) — по крипто-признаку.
3. **Самовосстановление.**
   - Ошибка блока (`ConferenceCall.java:423-425`) → повторный вход.
   - Ошибка записи в цепочку не завершает звонок (`VoIPService.java:2711`): перечитать последний блок и повторить.
4. **Счётчики и отчёт:** `jni/voip/org_telegram_messenger_voip_Instance.cpp:504-519` и tgcalls `545f5ae5` (канал данных).
5. **«Войти от имени»** скрыт.
6. **Удаление RTMP:**
   - `AND/ui/Components/CreateGroupCallBottomSheet.java:84, 238-239, 261`;
   - `AND/ui/Components/CreateRtmpStreamBottomSheet.java` — вход;
   - `isRtmpStream` в `JoinCallAlert.java:129`, `VoIPService.java:2049, 2243, 2348`;
   - режим частей трансляции `VoIPService.java:3086-3136`;
   - живые истории `AND/ui/Stories/recorder/StoryPrivacyBottomSheet.java:557-565`, `StoryRecorder.java:3351` — после решения 4.

**Web (`pluma-web`, `aa74832e`):**
1. **Сквозной режим звонков чатов.** Вход через `joinConferenceCommon` с `inputGroupCall(id, access_hash)` и `chatId` (`TWEB/lib/calls/groupCallsController.ts:803-1057`, место для формы с id — `:1031-1038`).
2. **Разделить `isConference`** (`TWEB/lib/calls/groupCallInstance.ts:2177-2179`) на шифрование и UI. Simulcast (`groupCallConnectionInstance.ts:227`) и меню (`groupCallsController.ts:591, 672, 687`) — по UI-признаку.
3. **Браузер без `RTCRtpScriptTransform`** (`TWEB/environment/conferenceCallSupport.ts`): понятное сообщение вместо входа (решение 5).
4. **Счётчики и отчёт** в трансформере `TWEB/lib/calls/e2e/*` и канале данных.
5. **«Войти от имени»** скрыт.
6. **Удаление RTMP:**
   - `TWEB/components/chat/topbar.ts:987-993`;
   - `TWEB/components/rtmp/*`;
   - `TWEB/components/mediaViewer/rtmp.ts`;
   - `TWEB/lib/calls/rtmpCallsController.ts`;
   - распознавание в `appGroupCallsManager.ts:556-565, 731` и `components/peerProfile.tsx:1474-1480`.

**Проверки патчей:**
- `verify_*` на каждое место (старого текста нет, нового ровно столько, сколько нужно);
- порча якорей в `DAMAGE`;
- тест «правка падает на изменённом апстриме»;
- сборка в CI своего репозитория;
- для Desktop и Android — что tgcalls собирается с изменением канала данных.

## Тесты
**Модульные (`pkg/tde2e`):**
- векторы шифрования tde2e;
- хэш блока по известному блоку;
- магия +1 туда и обратно;
- подпись: верная, чужая, без `signature_public_key`;
- нулевой блок;
- блок входа: добавляет только себя, заменяет свою старую запись, чужой ключ в составе — отказ, повторный ключ — отказ;
- `SetSharedKey` с недостающим или лишним заголовком — отказ;
- `state_proof` с составом при его смене — отказ;
- `kv_hash` пустого trie;
- права: рост внешних прав и выдача чужих флагов — отказ;
- commit и reveal: двойной commit, reveal до всех commit, неверный nonce, чужая подпись;
- хэш эмодзи совпадает у всех участников;
- кадр: шифрование и расшифровка через две эпохи, неизвестная эпоха, повтор и старый seqno.

**Модульные (bff, SFU):**
- политика входа и удаления (`only_left` для неушедшего — отказ, `kick` без прав — отказ);
- предел состава 100;
- `join_as` ≠ сам — отказ;
- `rtmp_stream` — отказ;
- ответ на вход без `stream`;
- разбор `header_a` из пакета звука;
- отчёт с лишним полем — отброшен;
- метрики отставания опроса.

**Интеграционные** (`pkg/testinfra`, MySQL):
- миграция дважды;
- десять параллельных входов на одну высоту: принимается ровно один блок на высоту, остальные `CONF_WRITE_CHAIN_INVALID`, версии участников без пропусков;
- `next_offset` обеих подцепочек;
- не участник видит только последний блок;
- удаление блоков сборщиком после завершения.

**Сквозные** (`pkg/mtclient` + `pkg/groupcallpeer` в сквозном режиме, стенд со службой `sfu`, сессии после `getState`):
- `TestGroupCallE2EJoin`:
  - первый входит нулевым блоком, второй — блоком входа;
  - оба получают блоки с магией +1;
  - звук расшифровывается в обе стороны;
  - SFU видит эпоху отправителя, равную текущей высоте.
- `TestGroupCallE2EPlainRejected`: вход без `public_key`/`block` отклонён; `join_as` канала отклонён.
- `TestGroupCallE2EChainRace`: два входа на одну высоту, второй получает `CONF_WRITE_CHAIN_INVALID`, перечитывает последний блок и входит.
- `TestGroupCallE2ELeaveRemoval`:
  - третий уходит;
  - двое шлют `only_left`, принимается один;
  - ушедший получает `GROUPCALL_FORBIDDEN`;
  - оставшиеся меняют ключ и слышат друг друга.
- `TestGroupCallE2EEmoji`: commit и reveal трёх участников; хэши эмодзи равны.
- `TestGroupCallE2EChannelListener`: слушатель канала входит в цепочку и расшифровывает звук выступающего.
- `TestGroupCallE2EDesync`: «сломанный участник» перестаёт применять блоки:
  - срабатывают метрики отставания опроса, эпохи отправителя и `UnknownEpoch` из отчётов;
  - `pluma-admin calls show` показывает его;
  - после повторного входа метрики возвращаются к норме.
- `TestGroupCallRtmpNeverServed`: `createGroupCall(rtmp_stream)`, `getGroupCallStreamRtmpUrl`, `getGroupCallStreamChannels` — ошибки; `"stream"` в ответах нет.

Хотя бы один тест проверить падающим без изменения: например, `TestGroupCallE2EJoin` на `task/093-group-calls` получает отказ на `public_key`.

**Дифференциальный (CachyOS):** `tools/tde2ediff` строит блоки и кадры настоящим tde2e и порт их принимает; обратная проверка — тоже.

## Проверки
**Сейчас (CachyOS выключена) — GitHub CI на ветке `task/094-group-calls-e2e`:**
- `make build`, `make vet`, `make test`, `make test-race`, lint, `make test-integration`, `make e2e`;
- `make apicoverage-check` — после `go run ./tools/apicoverage`; если запустить его негде, это делает CI-джоб `API coverage summary`, а матрица обновляется на CachyOS;
- `go test ./tools/...` с `logaudit` входит в `make test` (`go test ./...`, `Makefile:161-163`);
- `make test-race` гоняет только `RACE_PKGS` (`Makefile:165-167`): `pkg/tde2e`, `pkg/groupcalls`, `app/bff/groupcalls` и `app/service/sfu` должны быть в этом списке.

На Mac ничего не собирать и не запускать. Ошибка CI исправляется на ветке, с тестом.

**Клиенты:** CI четырёх репозиториев: тесты патча, `DAMAGE`, сборка.

**Когда включится CachyOS:**
- `tools/tde2ediff`;
- полный прогон партии (ADR 0059, бриф 093);
- живая проверка со скриншотами: tdesktop и Web на CachyOS, iOS в симуляторе, Android в эмуляторе — все четыре в одном звонке.

## Проверки для `docs/project/TESTING-BACKLOG.md`
- [ ] Android, iOS, Desktop, Web в одном звонке супергруппы: слышно всех, у всех одинаковые эмодзи.
- [ ] Канал: слушатель входит, слышит выступающего, его эмодзи совпадают с остальными.
- [ ] Участник выходит и возвращается: звук у остальных не пропадает дольше пары секунд, эмодзи у всех обновляются одинаково.
- [ ] Камера у трёх участников и показ экрана в сквозном звонке; при плохой сети картинка падает в качестве, а не пропадает.
- [ ] Сеть рвётся на 30 с у одного участника: он возвращается сам, его снова слышно; в `pluma-admin calls show` видно повторный вход.
- [ ] Нигде нет пунктов «Stream with…», «Начать трансляцию», RTMP-ключа (и живых историй, если владелец решил их убрать).
- [ ] Выбора «войти от имени канала» в групповом звонке нет.
- [ ] Web в браузере без `RTCRtpScriptTransform`: понятное сообщение вместо входа.
- [ ] Старая сборка клиента без патча не входит в звонок и показывает понятную ошибку.

## Решения, требующие подтверждения владельцем
Повторяют ADR 0060:
1. Звонки чатов — криптографическим путём конференций с интерфейсом звонка чата.
2. «Войти от имени канала» убирается.
3. Этап 1 не выходит к пользователям без этапа 2.
4. Живые истории убираются вместе с RTMP.
5. Web без `RTCRtpScriptTransform` групповые звонки не поддерживает.
6. Порт проверки на Go без cgo в сервере.
7. Отчёты по каналу данных; подробности по участнику — в `pluma-admin`.
8. Ошибка для клиента без ключа — после шага 1.

## Результат
_Заполняется по ходу и по завершении._
