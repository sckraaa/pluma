# 04. Клиенты, push, звонки и сборочная инфраструктура

Дата исследования: 2026-10-21. Сервер: teamgram-server, API layer 228 (`README.md`, `go.mod`: `github.com/teamgram/proto v0.228.0`).
Обозначения: **[проверено]** — подтверждено чтением исходников/официальной документации; **[оценка]** — инженерная оценка без прямого источника; **[не проверено]** — требует проверки на практике.

---

## 0. Ключевые выводы (TL;DR)

1. **Разрыв по layer.** Сервер — 228. Актуальные upstream-клиенты уже ушли вперёд: tdesktop `dev`/v7.3.2 — **232**, Android 13.0.3 — **231**, Telegram-tt (Web A) — **231**, iOS master / release-13.0.0 — **230**, tweb (Web K) master — **229**. Последние версии клиентов с ровно layer 228: **tdesktop v7.0.1–v7.0.9**, **Android 12.9.0–12.9.1** (коммиты `9bcf3d2769`, `aac1efcb83`), **iOS release-12.9.2**. Патченые ветки teamgram (`teamgram2`/`teamgram`) ещё старее — layer **223** (iOS, Android, tdesktop), Web K fork — **177**, Web A fork — **199**. [проверено]
2. **В open-source сервере нет push и звонков.** `account.registerDevice` — заглушка с сообщением «License key from https://teamgram.net required» (`app/bff/notification/internal/core/account.registerDevice_handler.go`); в `app/` нет обработчиков `phone.*`. README прямо относит push (APNs/FCM/web), audio/video/group/conference calls, каналы/мегагруппы, ботов, 2FA, секретные чаты к Enterprise edition. Значит, push и звонки придётся писать самостоятельно. [проверено]
3. **RSA-ключ.** В репозитории лежит приватный ключ `teamgramd/bin/server_pkcs1.key` (fingerprint `12240908862933197005` = `0xa9e071c1771060cd`, модуль `BCA2C439…`). Тот же публичный ключ вшит в форки teamgram. Для продакшена ключ **обязательно перегенерировать** и вшить новый публичный ключ во все клиенты. [проверено]
4. **Для групповых звонков есть неожиданная находка:** в официальном `TelegramMessenger/tgcalls` (ветка `development`, LGPL-3.0) появился `tools/go_sfu` — тестовый SFU на Go + Pion, понимающий join-payload tgcalls, Colibri-сообщения (`ReceiverVideoConstraints`), simulcast, transport-cc. Это не продакшен-сервер, но лучшая из существующих отправных точек для собственного SFU, совместимого с родными клиентами, — и он на Go, как и teamgram. [проверено]
5. **Сборки:** рекомендуемая схема — iOS на собственном Mac как self-hosted runner GitHub Actions (бесплатно) + bazel-remote на Ryzen-ПК как удалённый кэш; Android и tdesktop (Linux/Windows) — на Ryzen-ПК (self-hosted runner, Docker, ccache + Gradle build cache); web — на GitHub-hosted Linux. Облачный macOS (GitHub $0.062/мин, Codemagic 500 бесплатных мин) — как резерв. Подробно в разделе 5.

---

## 1. Клиенты: репозитории, layer, сборка, точки патча

### 1.1 Сводная таблица

| Клиент | Upstream | Последний релиз (на 2026-10-21) | Layer upstream | Последняя версия с layer 228 | Форк teamgram (патч-ветка) | Layer форка | Сборка |
|---|---|---|---|---|---|---|---|
| iOS | [TelegramMessenger/Telegram-iOS](https://github.com/TelegramMessenger/Telegram-iOS) | tag `release-13.0.0` (master от 2026-10-07) | 230 | `release-12.9.2` (Xcode 26.2, Bazel 8.4.2) | [teamgram/teamgram-ios](https://github.com/teamgram/teamgram-ios) `teamgram2` (2026-04-21) | 223 | Bazel 9.2.0 + Xcode 26.6 (`versions.json`), `build-system/Make/Make.py` |
| Android | [DrKLO/Telegram](https://github.com/DrKLO/Telegram) | 13.0.3 (7183), 2026-10-20 | 231 | 12.9.1 (6976), коммит `aac1efcb83` | [teamgram/teamgram-android](https://github.com/teamgram/teamgram-android) `teamgram2` (2026-03-19) | 223 | Gradle 8.13, JDK 17, SDK 36, NDK 27.2.12479018, CMake 3.22.1 |
| Desktop | [telegramdesktop/tdesktop](https://github.com/telegramdesktop/tdesktop) | v7.3.2, 2026-10-20 | 232 | v7.0.1 … v7.0.9 | [teamgram/teamgram-tdesktop](https://github.com/teamgram/teamgram-tdesktop) `teamgram` (2026-03-20) | 223 | CMake; Linux — Docker `tdesktop:centos_env`; Windows — `Telegram/build/prepare/prepare.py` + MSVC |
| Web K | [morethanwords/tweb](https://github.com/morethanwords/tweb) | без тегов, master 2026-10-19 | 229 (`src/lib/mtproto/schema.ts`) | нужен поиск по истории `schema.ts` [не проверено] | [teamgram/teamgram-tweb](https://github.com/teamgram/teamgram-tweb) `teamgram` (2024-04-24) | 177 | npm/pnpm + Vite |
| Web A | [Ajaxy/telegram-tt](https://github.com/Ajaxy/telegram-tt) | `air_v2.12.2` / v10.9.51, 2026-10-16 | 231 (`src/lib/gramjs/tl/AllTLObjects.ts`) | нужен поиск по истории [не проверено] | [teamgram/teamgram-tt](https://github.com/teamgram/teamgram-tt) `teamgram3` (2025-12-12) | 199 | npm + webpack |

Важно: ветки `master` форков teamgram-ios/teamgram-android — это просто зеркала upstream (там layer 228, но без патчей сервера). Патчи живут в `teamgram2`.

**Разрыв по layer — что это значит на практике.** Клиент шлёт `invokeWithLayer(N, initConnection(...))` и сериализует запросы/ожидает ответы по своей схеме N. Если N > 228, клиент будет отправлять конструкторы/флаги, которых сервер не знает (ошибка десериализации → `INPUT_*`/обрыв), а сервер будет отдавать объекты, в которых не хватает новых полей (клиенты обычно это переживают). Безопасная стратегия: **пинить клиентов на версии с layer 228** (таблица выше) и обновлять клиента и сервер синхронно, по одному layer за раз. Teamgram выпускает `proto` по слоям (v0.228.0) — следить за выходом v0.229+. [оценка]

### 1.2 iOS (Telegram-iOS)

Точки патча (по форку teamgram-ios `teamgram2`, сверено с upstream master) [проверено]:

| Что | Где |
|---|---|
| Адреса DC (seed) | `submodules/TelegramCore/Sources/Network/Network.swift`, блок `seedAddressList` (≈стр. 529–550 в форке). Teamgram дополнительно меняет порт на `10443` в `MTDatacenterAddress(ip:, port: 10443, ...)`. В upstream — `149.154.175.x`, порт 443. |
| RSA public key | `submodules/MtProtoKit/Sources/MTDatacenterAuthMessageService.m` — массив `MTDatacenterAuthPublicKey initWithPublicKey:@"-----BEGIN RSA PUBLIC KEY-----..."` (teamgram заменяет оба набора: prod и test, ключ `MIIBCgKCAQEAvKLE…`). |
| api_id / api_hash, bundle_id, team_id | JSON-конфиг сборки: `build-system/template_minimal_development_configuration.json` / `appstore-configuration.json` (`bundle_id`, `api_id`, `api_hash`, `team_id`, `enable_siri`, `enable_icloud`, `app_specific_url_scheme`). |
| Layer | `submodules/TelegramCore/Sources/State/Serialization.swift` → `currentLayer()`; сама схема генерируется в `submodules/TelegramApi` (SwiftTL, `build-system/SwiftTL`). Менять layer вручную нельзя — нужна вся схема соответствующей версии. |
| Ссылки t.me, домены | `associated_domains_fragment` в `Telegram/BUILD` включается только для официальных bundle_id — для своего bundle ничего делать не надо. |

Внимание: в upstream master (13.x) появился новый сетевой движок **MTProtoRustEngine** (`submodules/MTProtoRustEngine`, сабмодуль `third-party/mtproto-engine` → `github.com/macosswift/mtproto-engine`). Он берёт layer из `context.serialization.currentLayer()`, но содержит ли он собственный список DC/RSA-ключей и включён ли по умолчанию — **[не проверено]**. В `release-12.9.2` (layer 228) этого движка ещё нет — ещё один довод в пользу 12.9.2.

### 1.3 Android (DrKLO/Telegram)

| Что | Где [проверено] |
|---|---|
| Адреса DC | `TMessagesProj/jni/tgnet/ConnectionsManager.cpp`, `ConnectionsManager::initDatacenters()` (≈стр. 1819 в master): `datacenter->addAddressAndPort("149.154.175.50", 443, 0, "")` → заменить на свой IP/порт для prod и `testBackend`. |
| RSA public key | `TMessagesProj/jni/tgnet/Handshake.cpp`, `serverPublicKeys.emplace_back("-----BEGIN RSA PUBLIC KEY-----...")` (≈стр. 365+), отдельно для prod/test; там же `serverPublicKeysFingerprints`. |
| api_id / api_hash | `TMessagesProj/src/main/java/org/telegram/messenger/BuildVars.java` (`APP_ID = 4`, `APP_HASH = …`). |
| Layer | `TMessagesProj/src/main/java/org/telegram/tgnet/TLRPC.java` `LAYER = …` (вместе со всеми TL-классами). |
| FCM | `google-services.json` в `TMessagesProj_App/` (README требует два Firebase-приложения: `org.telegram.messenger` и `.beta` — для своего package переименовать). |
| Подпись | `TMessagesProj/config/release.keystore` + `RELEASE_KEY_*` в `gradle.properties`. |
| Package | `APP_PACKAGE` в `gradle.properties`. |

Сборка: README требует Android Studio 2025.1.4, NDK 27.2.12479018, SDK 36; в корне есть `Dockerfile` (gradle:8.13-jdk17 + SDK/NDK) — удобно для CI на Linux. Нативная часть (`jni/CMakeLists.txt`: tgnet, tgcalls/WebRTC, ffmpeg, libvpx, dav1d, opus, lottie…) — самая тяжёлая; flavor `afat` собирает 4 ABI (`armeabi-v7a, arm64-v8a, x86, x86_64`). Для закрытой группы достаточно **одного ABI `arm64-v8a`** — это почти вчетверо сокращает нативную сборку.

### 1.4 Desktop (tdesktop)

| Что | Где [проверено] |
|---|---|
| Адреса DC | `Telegram/SourceFiles/mtproto/mtproto_dc_options.cpp`: `kBuiltInDcs[]`, `kBuiltInDcsIPv6[]`, `kBuiltInDcsTest[]`, `kBuiltInDcsIPv6Test[]` (стр. 31–58). |
| RSA public key | там же: `kPublicRSAKeys[]` (стр. 70) и `kTestPublicRSAKeys[]` (стр. 60). |
| api_id / api_hash | параметры CMake `-D TDESKTOP_API_ID=… -D TDESKTOP_API_HASH=…`. |
| Layer | `Telegram/SourceFiles/mtproto/scheme/api.tl` (`// LAYER N`), код генерируется при сборке. |
| Автообновление | обязательно отключить/перенаправить (иначе клиент скачает официальный апдейт с другим layer/ключами): флаг `-D DESKTOP_APP_DISABLE_AUTOUPDATE=ON` [не проверено — проверить имя опции в `cmake/options.cmake`]. |

Сборка: Linux — `Telegram/build/prepare/linux.sh` (строит Docker-образ `tdesktop:centos_env` со всеми библиотеками), затем `docker run … build.sh -D TDESKTOP_API_ID=…`. Windows — `prepare.py` собирает Qt/ffmpeg/WebRTC/OpenSSL и т.д. (первый раз — часы), затем CMake + MSVC. Upstream CI (`.github/workflows/linux.yml`, `win.yml`) уже использует `ccache` + `actions/cache` для библиотек/Qt по хэшу `Telegram/build/**` — эти workflow можно взять почти как есть.

### 1.5 Web K (tweb) и Web A (telegram-tt)

**tweb** [проверено]:
- адреса: `src/lib/mtproto/dcConfigurator.ts` — функция `constructTelegramWebSocketUrl` (в upstream `wss://${…}ws${dcId}.web.telegram.org/apiws`; teamgram заменил на `wss://web.teamgram.net/apiws`) и массив `dcOptions` (для HTTP-транспорта; teamgram: `{id:1, host:'43.155.11.190', port:8081}`);
- RSA: `src/lib/mtproto/rsaKeysManager.ts` (`publisKeysHex` / `testPublicKeysHex`, модуль+экспонента в hex);
- api_id/hash: `src/config/app.ts` (`VITE_API_ID`, `VITE_API_HASH`);
- layer: `src/lib/mtproto/schema.ts` (`"layer":229`).

**telegram-tt** [проверено частично]: api_id/hash — `.env` (`TELEGRAM_API_ID`, `TELEGRAM_API_HASH`) → `src/config.ts`; layer — `src/lib/gramjs/tl/AllTLObjects.ts` (`LAYER = 231`); форк teamgram меняет `src/lib/gramjs/Utils.ts` (DC), `src/lib/gramjs/crypto/RSA.ts` (ключ), `src/lib/gramjs/client/TelegramClient.ts`, `src/config.ts` (по diff `master...teamgram3`).

Веб-клиентам нужен **WebSocket поверх TLS** (страница по https не может открыть `ws://`). У teamgram gnetway есть WebSocket-листенер `0.0.0.0:11443` (`teamgramd/etc/gnetway.yaml`) — его нужно публиковать через reverse-proxy с TLS (nginx/Caddy) по пути `/apiws`.

### 1.6 Как это сделал сам teamgram
Минимально-инвазивно: заменены (1) seed-адреса DC и порт 10443, (2) RSA-ключ на ключ из `server_pkcs1.key`, (3) брендинг/строки; api_id/hash берётся тестовый. Сервер принимает любой api_id [оценка — проверить в `initConnection`/`auth.sendCode` обработчиках]. Патчи живут в отдельных ветках поверх периодически мёржащегося upstream. Рекомендация: повторить ту же модель, но держать патчи как **маленький набор git-патчей / скрипт `apply-patches.sh`** (sed по известным местам + проверка, что замена произошла), а не как долгоживущую ветку — так перенос на новый тег upstream занимает минуты.

---

## 2. Push-уведомления

### 2.1 Протокол (общий для всех клиентов) [проверено: core.telegram.org/api/push-updates]
`account.registerDevice(token_type, token, app_sandbox, secret, other_uids)`. Типы: `1` APNs, `2` FCM, `4` Simple push/UnifiedPush, `7` MTProto separate session, `9` APNs VoIP, `10` Web push (token = JSON `{endpoint, keys}`), `13` Huawei. Если клиент передал `secret`, сервер шифрует payload этим ключом как `auth_key` по MTProto 2.0 и кладёт в поле `p` (base64url); после расшифровки — JSON с 4-байтовым префиксом длины. Web push шифруется по RFC 8291 (ECDH P-256).

**Состояние teamgram OSS:** `account.registerDevice` не реализован (заглушка), отправителя push нет. Нужно реализовать сервис: хранение токенов (user, auth_key_id, token_type, secret, sandbox), подписка на события (новое сообщение, `updatePhoneCall`), формирование payload в формате Telegram (`loc_key`, `loc_args`, `custom.msg_id`, `from_id`, `chat_id`/`channel_id`, `call_id`, `call_ah`, …), шифрование и отправка в APNs/FCM/Web Push.

### 2.2 iOS
- **APNs (обычные):** ключ `.p8` (token-based auth) из Apple Developer, topic = bundle_id. В ad-hoc сборке `aps-environment = production` (ad-hoc использует production-шлюз APNs) [проверено частично: в `Telegram/BUILD` `aps_fragment` берёт значение из конфигурации]. Telegram-iOS ожидает **зашифрованные** push: их расшифровывает **Notification Service Extension**, читая auth key аккаунта из общего контейнера App Group. Поэтому сервер обязан поддерживать `secret` и шифрование, иначе уведомления будут показываться пустыми/не будут обработаны [оценка по устройству NotificationService в Telegram-iOS].
- **VoIP (PushKit):** token_type 9, topic `<bundle_id>.voip`, тот же `.p8` (или VoIP Services certificate). Entitlement `com.apple.developer.pushkit.unrestricted-voip` в `Telegram/BUILD` включается **только для официальных bundle_id** — для своей сборки действует обычное правило iOS 13+: каждый VoIP push обязан немедленно репортиться в CallKit, иначе система убивает приложение и перестаёт доставлять VoIP push. Значит, сервер должен слать VoIP push **только** для входящих звонков (`PHONE_CALL_REQUEST`), а не для сообщений [проверено по `Telegram/BUILD`; правило iOS — общеизвестное].
- **Communication notifications / filtering** (`usernotifications.communication`, `.filtering`) тоже включаются только для официальных ID — для своей сборки не нужны.

### 2.3 Расширения и App Group для ad-hoc
Из `build-system/fake-codesigning/profiles` и `Telegram/BUILD` [проверено] нужно завести **App ID + ad-hoc provisioning profile** для каждой цели:

| Цель | Bundle ID (суффикс) |
|---|---|
| Основное приложение | `<bundle>` |
| Share extension | `<bundle>.Share` |
| Notification Service | `<bundle>.NotificationService` |
| Notification Content | `<bundle>.NotificationContent` |
| Widget (WidgetKit) | `<bundle>.Widget` |
| Siri Intents | `<bundle>.SiriIntents` |
| Broadcast Upload (демонстрация экрана) | `<bundle>.BroadcastUpload` |
| Watch App / Watch Extension | `<bundle>.watchkitapp(.watchkitextension)` — можно отключить |

Точные суффиксы сверять с `build-system/fake-codesigning/profiles/*.mobileprovision` и `Telegram/BUILD` [частично не проверено]. Все цели должны иметь capability **App Groups** с группой `group.<bundle>`; основное приложение — **Push Notifications**; iCloud/Siri — только если `enable_icloud`/`enable_siri` = true (рекомендую выключить). Keychain sharing задаётся через `application-identifier`/team prefix.

Ограничения ad-hoc: **100 устройств каждого типа (iPhone, iPad, …) на год членства**; каждое устройство надо регистрировать по UDID и перевыпускать профили. Для ≤100 пользователей это впритык. Альтернатива без лимита UDID — **TestFlight internal testing** (до 100 внутренних тестеров из App Store Connect, без ревью) [проверить актуальные правила Apple]; external TestFlight требует Beta App Review.

### 2.4 Android
- **FCM:** свой Firebase-проект, `google-services.json` под свой package, на сервере — FCM HTTP v1 API (service account JSON). Telegram-Android шлёт `secret` и ожидает data-only сообщения с полем `p` (расшифровка в `PushListenerController`/`GcmPushListenerService`) [оценка по названию классов].
- **Без Google Play Services:** у Telegram-Android есть «keep-alive service»/фоновое соединение, а token_type 4 позволяет UnifiedPush/ntfy — для закрытой группы самый простой путь до готовности FCM [оценка].

### 2.5 Desktop и Web
- tdesktop push не использует — держит постоянное MTProto-соединение.
- Web K/A: Web Push (token_type 10) через Service Worker; серверу нужен отправитель Web Push (RFC 8030/8291, VAPID-подпись желательна). Работает только при TLS на домене web-клиента.

---

## 3. Звонки

### 3.1 Звонки 1:1 [проверено частично]
Схема: `phone.requestCall` → `updatePhoneCall(phoneCallRequested)` у собеседника (+ VoIP push) → `phone.acceptCall` (DH g_a/g_b) → `phone.confirmCall` → сервер отдаёт `phoneCall` со списком `connections: Vector<PhoneConnection>` и `protocol` (min/max layer, `library_versions`, `udp_p2p`, `udp_reflector`). Сигналинг (SDP/ICE для tgcalls v2+) идёт через `phone.sendSignalingData` → `updatePhoneCallSignalingData`.
- `phoneConnection` — **Telegram-рефлектор** (собственный UDP/TCP-протокол с 16-байтовым `peer_tag`; клиентская сторона — `tgcalls/v2/ReflectorPort.cpp`). В tdesktop он превращается в `tgcalls::RtcServer` с `login = "reflector"` (`calls_call.cpp`, стр. ~97–130).
- `phoneConnectionWebrtc` — **обычный STUN/TURN** (`ip, ipv6, port, username, password, turn, stun`). Обрабатывается всеми клиентами (tdesktop `calls_call.cpp` стр. 92/134/143–156) [проверено для tdesktop].
- **Открытой реализации серверного рефлектора Telegram не найдено** (поиск по GitHub/вебу; есть только клиентская сторона в tgcalls) [проверено поиском, отрицательный результат].
- **Практичный путь:** отдавать клиентам `phoneConnectionWebrtc` с собственным **coturn** (STUN+TURN, TURN-креды с коротким TTL, генерируемые сервером) и `udp_p2p = true`. Клиенты сами установят P2P или пойдут через TURN. Всё остальное — реализация `phone.*` методов и стейт-машины звонка на сервере teamgram (в OSS отсутствует). [оценка; реализовано в некоторых сторонних серверах, но не проверено здесь]

### 3.2 Групповые звонки (video chats) [проверено: core.telegram.org/api/group-calls + tgcalls]
- Клиент генерирует join-payload движком tgcalls (`GroupInstanceCustomImpl`/`GroupInstanceReferenceImpl`): JSON с `ssrc` (аудио, ненулевой), `ufrag`, `pwd`, `fingerprints[]`, при видео — `ssrc-groups` (SIM/FID), и передаёт в `phone.joinGroupCall(params: DataJSON)`.
- Сервер отвечает `updateGroupCallConnection(params: DataJSON)` — «обычный tgcalls join response»: транспорт SFU (ufrag/pwd/fingerprints/candidates), payload-types, расширения RTP. Документация поля не расписывает, но их формат по факту задаёт эталонный SFU (см. ниже). Режимы `{"stream":true}` / `{"stream":true,"rtmp":true}` — стриминг чанками через `upload.getFile` с `inputGroupCallStream`.
- **Демонстрация экрана** = вторая независимая инстанция движка + `phone.joinGroupCallPresentation`; ответ — `updateGroupCallConnection` с флагом `presentation`. Выключение — `phone.leaveGroupCallPresentation`.
- Медиа: ICE-lite SFU, DTLS-SRTP, SCTP data-channel с Colibri-сообщениями (`ReceiverVideoConstraints`, `SenderVideoConstraints`, `ActiveVideoSsrcs`), H.264/VP8 simulcast (180/360/720), transport-cc. Исторически Telegram использует Jitsi-подобную семантику Colibri [оценка].
- **Эталонный SFU:** `TelegramMessenger/tgcalls` → `tools/go_sfu/` (Go 1.24, Pion ice/dtls/srtp/sctp; `sfu.go` ≈1300 строк; активно развивается — коммиты 2026-09). Это тестовый SFU, встраиваемый в CLI через CGo (`GoSfu_Join/Leave/...`), с REMB/TWCC, переписыванием SSRC, выбором слоя simulcast. Для продакшена его нужно вынести в отдельный сервис (UDP-порт, ICE-кандидаты с публичным IP, интеграция с `phone.joinGroupCall` в teamgram), добавить аутентификацию и лимиты. Лицензия LGPL-3.0 — для серверного использования без распространения бинарников ограничений практически нет [оценка, не юридическая консультация].
- **Conference calls (E2E-групповые звонки, `phone.createConferenceCall`, блокчейн-ключи tde2e)** в новых layer — отдельная большая тема; на layer 228 клиенты уже их используют для «звонок → добавить участника». Без реализации сервер должен корректно отвечать ошибкой. [оценка]

### 3.3 Замена групповых звонков на LiveKit
Готовых форков Telegram-клиентов с LiveKit **не найдено** [проверено поиском]. Оценка трудоёмкости:
- LiveKit SDK есть для Swift (iOS), Kotlin (Android), JS (Web), Flutter/React Native, Rust; для C++ (tdesktop) — только через Rust SDK/FFI или `client-sdk-cpp` [не проверено — статус C++ SDK уточнить].
- В клиентах UI групповых звонков жёстко завязан на tgcalls (`PresentationGroupCall` в iOS, `VoIPService`/`GroupCallActivity` в Android, `Calls::GroupCall` в tdesktop, `groupCalls` в tt/tweb): участники, громкость, активный говорящий, видео-тайлы, демонстрация экрана (в iOS через Broadcast Upload extension с IPC к основному приложению). Замена = переписать слой медиа в 5 кодовых базах + поддерживать при каждом обновлении upstream.
- **Вывод:** LiveKit оправдан только как отдельное приложение/веб-комната (например, ссылка на LiveKit Meet из чата). Для «родных» групповых звонков в патченых клиентах дешевле реализовать сервер-сторону под tgcalls на базе `go_sfu` (или Jitsi Videobridge с адаптером, [не проверено]) — клиенты остаются без изменений.

---

## 4. Инфраструктура сборки: варианты и цены (2025–2026)

### 4.1 Облачные CI

| Сервис | Условия | macOS | Linux/Windows | Комментарий |
|---|---|---|---|---|
| **GitHub Actions** | Публичные репо — бесплатно; приватные — Free 2000 мин/мес, Pro/Team 3000 мин/мес. **Self-hosted runner — бесплатно** (план платы $0.002/мин за self-hosted, анонсированный на март 2026, по текущей странице GitHub не действует) | $0.062/мин (3–4 ядра M1), $0.102/мин (M2 Pro 5 ядер) | Linux 2-core $0.006/мин, Windows $0.010/мин | Кэш 10 ГБ/репо бесплатно, сверх — $0.07/ГиБ/мес (только Pro/Team), retention 7 дней. Лимит job — 6 ч [оценка по общеизвестным лимитам]. Публиковать форк клиентов публично допустимо (GPL), но в нём будут адрес сервера и публичный ключ — это не секрет. |
| **Codemagic** | Личный аккаунт: **500 бесплатных мин macOS M2/мес** | $0.095/мин M2, $0.114/мин M4 | $0.045/мин | Удобно как резерв для iOS. |
| **Bitrise** | Hobby — бесплатно, 300 кредитов/мес; Starter $99/мес | кредиты | кредиты | Мало для Telegram-сборок. |
| **Xcode Cloud** | 25 compute-часов/мес входят в Apple Developer Program, далее от $49.99/100 ч | — | — | Нужен Xcode-проект/схема; Telegram собирается Bazel'ом (проект генерирует rules_xcodeproj и сам вызывает Bazel). Запуск Bazel из `ci_scripts` теоретически возможен, но без удалённого кэша каждый прогон холодный (~1 ч+) → 25 ч ≈ 15–20 сборок/мес. **Не рекомендую** [не проверено на практике]. |
| **Cirrus CI** | **Закрыт 1 июня 2026** (команда ушла в OpenAI); Tart/Orchard остаются open-source | — | — | Не вариант. Tart полезен для VM на своём Mac. |
| **Аренда Mac** | Scaleway M4-S €0.22/ч (мин. 24 ч) или €149/мес, M4-M €199/мес; MacStadium M4.S $149/мес, M4.M $249/мес (с июня 2026) | | | Имеет смысл только если свой Mac нельзя держать включённым. |

Источники: [GitHub runner pricing](https://docs.github.com/en/billing/reference/actions-runner-pricing), [GitHub Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions), [GitHub cache >10GB](https://github.blog/changelog/2025-11-20-github-actions-cache-size-can-now-exceed-10-gb-per-repository/), [обсуждение платы за self-hosted](https://theplatformengineering.substack.com/p/github-actions-2026-pricing-changes), [Codemagic pricing](https://docs.codemagic.io/billing/pricing/), [Bitrise pricing](https://www.bitrise.io/pricing), [Xcode Cloud](https://developer.apple.com/xcode-cloud/), [Cirrus CI shutdown](https://warpbuild.com/blog/cirrus-ci-shutting-down), [MacStadium → OpenAI](https://macstadium.com/blog/cirrus-labs-is-joining-openai), [Scaleway Apple silicon](https://scaleway.com/en/pricing/apple-silicon), [MacStadium price history](https://pricingsaas.com/companies/macstadium), [BuildBuddy pricing](https://www.buildbuddy.io/pricing).

### 4.2 Кэширование

**Bazel (iOS).** `Make.py` уже поддерживает оба режима [проверено]:
- `--cacheDir=<path>` → `--disk_cache` (локальный дисковый кэш);
- `--cacheHost=grpc://host:9092` → `--remote_cache` + `--experimental_remote_downloader` (README рекомендует [buchgr/bazel-remote](https://github.com/buchgr/bazel-remote)); `--remote_cache_async` включён.
- `--continueOnError` → `--keep_going`: сборка собирает **все** ошибки за один прогон.
- Условия попадания в кэш: одинаковая версия Xcode и Bazel (`versions.json`), **стабильный путь исходников** (upstream CI копирует в `/Users/Shared/telegram-ios`) и стабильный `--bazelUserRoot` — иначе ключи действий расходятся.
- **bazel-remote на Ryzen-ПК** (Docker, 100–300 ГБ SSD, gRPC 9092, доступ через WireGuard/Tailscale) — бесплатно, и его можно использовать с Mac и с облачных macOS-раннеров. **BuildBuddy Personal (free)**: 100 ГБ трафика кэша/мес — iOS-сборка Telegram с полным кэшем может прокачивать десятки ГБ за прогон [оценка], поэтому free-тир быстро кончится; свой bazel-remote предпочтительнее.

**Gradle (Android).** `org.gradle.caching=true` + локальный кэш (`~/.gradle/caches/build-cache-1`) или удалённый Gradle Build Cache Node (Docker-образ `gradle/build-cache-node`, бесплатен) [оценка]. Gradle-кэш **не** кэширует нативную CMake-часть — для неё `ccache` через `-DCMAKE_C_COMPILER_LAUNCHER=ccache -DCMAKE_CXX_COMPILER_LAUNCHER=ccache` в `externalNativeBuild.cmake.arguments` и постоянный каталог `.cxx`/`build` на self-hosted раннере (инкрементальная сборка без `clean`).

**tdesktop.** Самое дорогое — библиотеки (Qt, WebRTC, ffmpeg, OpenSSL…): кэшировать Docker-образ `tdesktop:centos_env` (Linux) и каталог `Libraries` (Windows) по хэшу `Telegram/build/**` — ровно так делает upstream CI. Сам клиент — `ccache` (Linux) / `sccache` или `ccache` с MSVC (Windows).

### 4.3 Что может Ryzen 5 5600 / 32 ГБ
- **Android** — да, полностью (Linux/WSL2/Docker или нативно Windows). 32 ГБ достаточно (`org.gradle.jvmargs=-Xmx8g` по умолчанию).
- **tdesktop Linux** — да (Docker). **tdesktop Windows** — да, если ПК на Windows (или Windows VM; для Linux-хоста — только Linux-сборка).
- **iOS — нет** (Xcode только на macOS; кросс-компиляции iOS с Linux нет). Но ПК может быть **удалённым кэшем Bazel** для Mac и облачных раннеров, хранилищем артефактов и Gradle cache node.
- **Web** — да, тривиально.

### 4.4 Реалистичное время сборки [оценка]
| Сборка | Холодная | С тёплым кэшем, мелкий патч |
|---|---|---|
| iOS (Mac M1/M2/M4, Bazel) | 45–90 мин | 3–10 мин (локальный disk cache); 10–20 мин на облачном раннере с remote cache (упор в сеть) |
| Android, 1 ABI, release + R8 (Ryzen 5600) | 25–45 мин | 4–10 мин (ccache + Gradle cache; R8 всё равно 2–4 мин) |
| Android, `afat` 4 ABI | 60–100 мин | 8–15 мин |
| tdesktop Linux (библиотеки) | 2–4 ч (один раз на версию зависимостей) | — |
| tdesktop клиент | 25–50 мин | 3–8 мин (ccache) |
| Web K / Web A | 2–5 мин | 1–3 мин |

Почему iOS «падает в конце и начинается заново»: типичные причины — подпись/профили (не хватает профиля для одного из 8–9 расширений, App Group/aps-environment не совпадает с entitlements), несовпадение Xcode с `versions.json`, нехватка диска. И главное — **если не передавать `--cacheDir`** (или путь исходников меняется), Bazel при перезапуске ничего не переиспользует. С `--cacheDir` + стабильным путём повторный запуск после ошибки на последнем шаге займёт минуты, а не час. [оценка]

---

## 5. Рекомендуемый пайплайн

### 5.1 Архитектура
```
GitHub (приватные репо: tg-ios, tg-android, tg-desktop, tg-web; патчи — скрипт + .patch)
   │  push тега / workflow_dispatch
   ├─► self-hosted runner "mac"  (свой Mac, launchd, labels: macOS, ios)
   │      iOS: Make.py build --cacheDir=~/tg-bazel-cache --cacheHost=grpc://pc:9092 (опц.)
   ├─► self-hosted runner "pc"   (Ryzen, Docker/WSL2, labels: linux, android, desktop)
   │      Android: Docker-образ из Dockerfile DrKLO + ccache + Gradle cache (тома)
   │      tdesktop Linux: tdesktop:centos_env + ccache
   │      tdesktop Windows: отдельный runner в Windows (если ПК на Windows)
   │      сервисы: bazel-remote (gRPC 9092), gradle build-cache-node, MinIO/HTTP для артефактов
   ├─► GitHub-hosted ubuntu-latest: Web K/A (npm ci + build) → деплой статики
   └─► резерв iOS: macos-latest GitHub ($0.062/мин) или Codemagic (500 бесплатных мин)
          с --cacheHost=grpc://pc:9092 через Tailscale
```
Стоимость: **$0** (self-hosted бесплатны; web укладывается в 2000 бесплатных мин). Резервная облачная iOS-сборка с тёплым удалённым кэшем ≈ 15–25 мин ≈ $1–1.5 за прогон на GitHub или бесплатно в рамках 500 мин Codemagic.

### 5.2 Fail-fast: порядок шагов в каждом workflow
1. **Preflight (секунды, до компиляции):**
   - проверка версий: Xcode == `versions.json.xcode`, Bazel, NDK/SDK, JDK, свободное место ≥ 60 ГБ;
   - применение патчей скриптом с **проверкой** каждой замены (`grep -q` нового адреса/ключа; если upstream поменял файл — падаем сразу);
   - проверка layer клиента == layer сервера (`currentLayer()`, `LAYER =`, `// LAYER`, `"layer":`) — несовпадение = ошибка;
   - проверка fingerprint вшитого RSA-ключа == `KeyFingerprint` из `gnetway.yaml`;
   - iOS: наличие всех `.mobileprovision` для всех целей, срок действия, совпадение bundle_id/App Group/`aps-environment`, импорт сертификата в отдельный keychain; Android: наличие keystore и пароля (`keytool -list`), `google-services.json` с нужным package.
2. **Быстрые проверки:** `bazel build --nobuild` / `Make.py … --continueOnError` на analysis-фазе (ошибки конфигурации видны за 1–2 мин) [оценка]; Android — `./gradlew help`/`processReleaseGoogleServices`/`validateSigningRelease`.
3. **Компиляция с кэшем** (`--cacheDir`/`--cacheHost`, ccache, Gradle cache) и `--keep_going`/`--continueOnError`, чтобы за один прогон увидеть все ошибки.
4. **Подпись/упаковка отдельным шагом**, артефакты промежуточных стадий сохраняются на раннере → при падении на подписи повтор занимает минуты (Bazel переиспользует всё из кэша).
5. **Публикация:** IPA → своя страница OTA-установки (manifest.plist по HTTPS) или TestFlight internal; APK → свой сервер/Obtainium; desktop → архив в Releases приватного репо.
6. **Повтор:** `continue-on-error: false`, но `retry` (например, `nick-fields/retry`) только для сетевых шагов (git submodules, загрузка зависимостей). Таймаут job разумный (iOS 120 мин), чтобы зависание не съедало часы.

### 5.3 Порядок работ
1. Перегенерировать RSA-ключ сервера, обновить `gnetway.yaml` (fingerprint).
2. Зафиксировать базовые теги клиентов с layer 228: iOS `release-12.9.2`, Android 12.9.1 (`aac1efcb83`), tdesktop v7.0.9, tweb/tt — найти коммиты с layer 228 по истории файла схемы.
3. Написать `apply-patches.sh` для каждого клиента (DC, порт, RSA, api_id, отключение автообновления tdesktop, bundle/package).
4. Поднять на ПК: GitHub runner (Docker), bazel-remote, ccache-тома; на Mac: runner как launchd-сервис, `--cacheDir` на SSD, стабильный путь `/Users/Shared/telegram-ios`.
5. Собрать по одному разу «холодно» — дальше все сборки инкрементальные.
6. Параллельно — серверные доработки: push (APNs .p8, FCM v1, Web Push) и `phone.*` + coturn для 1:1, затем SFU на базе `tgcalls/tools/go_sfu` для групповых звонков.
7. При обновлении сервера на layer 229+ — сдвигать теги клиентов синхронно.

---

## Источники
- Клиенты: [Telegram-iOS](https://github.com/TelegramMessenger/Telegram-iOS) (README, `versions.json`, `build-system/Make/Make.py`, `Telegram/BUILD`, `.github/workflows/build.yml`), [DrKLO/Telegram](https://github.com/DrKLO/Telegram) (README, `Dockerfile`, `TMessagesProj/build.gradle`, `gradle.properties`, `BuildVars.java`, `jni/tgnet/*`), [tdesktop](https://github.com/telegramdesktop/tdesktop) (`docs/building-linux.md`, `.github/workflows/linux.yml`, `win.yml`, `mtproto_dc_options.cpp`, `calls_call.cpp`), [tweb](https://github.com/morethanwords/tweb), [telegram-tt](https://github.com/Ajaxy/telegram-tt).
- Форки teamgram: [github.com/teamgram](https://github.com/teamgram) — teamgram-ios, teamgram-android, teamgram-tdesktop, teamgram-tweb, teamgram-tt, tgcalls.
- Протокол: [Push updates](https://core.telegram.org/api/push-updates), [Group calls](https://core.telegram.org/api/group-calls), [phone.joinGroupCall](https://core.telegram.org/method/phone.joinGroupCall), [E2E group calls](https://core.telegram.org/api/end-to-end/group-calls).
- Звонки: [TelegramMessenger/tgcalls](https://github.com/TelegramMessenger/tgcalls) (`tools/go_sfu/CLAUDE.md`, `tools/cli/CLAUDE.md`, `tgcalls/v2/ReflectorPort.cpp`).
- Сборки/цены: см. список в п. 4.1; [bazel-remote](https://github.com/buchgr/bazel-remote).
