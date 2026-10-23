# Аудит 02 — транспорт, протокол MTProto и безопасность

Область: `app/interface/gnetway`, `app/interface/session`, `app/interface/httpserver`, `app/service/authsession`, логин (`app/bff/authorization`), конфиги `teamgramd/etc*`, `docker-compose*.yaml`, зависимости `go.mod`, криптобиблиотека `github.com/teamgram/proto@v0.228.0/mtproto/crypto`.
Метод: только чтение кода, без запуска. Пункты, которые не удалось подтвердить, помечены **(не подтверждено)**.

---

## Вердикт

**В текущем виде сервер нельзя выставлять в интернет даже для закрытой группы из 100 человек.**

Механика протокола (транспорты, обфускация, AES-IGE MTProto 2.0, проверка msg_key, соли, контейнеры, ack/resend) в целом работает и совместима с официальными клиентами. Но в безопасности есть несколько **критических** дыр, каждая из которых сама по себе даёт захват чужого аккаунта или полный доступ к серверу:

1. **Вход в любой аккаунт по коду `12345`.** Это поведение конфигурации по умолчанию (`Code.Name: none`). Перебор кода ничем не ограничен.
2. **Захват сессии через temp-ключ.** Gateway берёт `perm_auth_key_id` из *непроверенного* `auth.bindTempAuthKey` и сразу отправляет RPC от имени владельца этого perm-ключа. Кроме того, `authsession.bindTempAuthKey` привязывает ключ к `perm_auth_key_id` из внутренней структуры и не сверяет его с ключом, которым она расшифрована.
3. **Приватный RSA-ключ сервера лежит в git** (`teamgramd/bin/server_pkcs1.key`).
4. **В Docker-развёртывании все внутренние gRPC-сервисы, etcd, Redis, MySQL (root/root) и MinIO опубликованы на 0.0.0.0 без аутентификации.** Метаданные с `UserId` передаются в gRPC в открытом виде, то есть любой, кто достучится до порта bff, может действовать от имени любого пользователя.
5. **2FA фактически нет.** `auth.checkPassword` возвращает авторизацию без проверки пароля, а `account.getPassword` — заглушка.
6. **Несколько DoS до аутентификации, которые роняют процесс:** panic в handshake, вероятный OOM при декодировании векторов, gzip-бомба.
7. **В логи уровня Info пишутся данные, из которых восстанавливается каждый новый auth_key:** `a`, `new_nonce`, `g_b`.

Хорошая новость: всё это исправимо точечно, архитектуру переписывать не нужно. Приблизительный объём, чтобы довести сервер до приемлемого уровня (P0+P1): **3–5 недель одного опытного Go-разработчика**. Подробности — в последнем разделе.

---

## Как устроено сейчас

### Топология
- **gnetway** (`app/interface/gnetway`) — фронтенд на `gnet/v2` (event-loop без горутины на соединение). Порты: TCP 10443/5222, WebSocket 11443 (`teamgramd/etc/gnetway.yaml:33-39`). Есть и HTTP-транспорт (`server_http.go`) — он включается флагом в конфиге.
  Gateway выполняет DH-handshake, хранит LRU-кэш auth-ключей (`cache_auth_key.go`), расшифровывает сообщения и пересылает открытый payload в session по gRPC/stream (`server_gnet.go:406-459`).
- **session** (`app/interface/session`) — для каждого perm-ключа заводится `MainAuthWrapper` с двумя горутинами: `runLoop` и `rpcRunLoop` (`sess/main_auth_wrapper.go:595,671`). Здесь проверяются соль и msg_id, обрабатываются контейнеры, ack, `msgs_state_req`, resend, а RPC отправляются в bff через `dao.InvokeContext`. RPC-метаданные (`UserId`, `PermAuthKeyId`) собираются из состояния `MainAuthWrapper` (`main_auth_wrapper.go:684-698`).
- **authsession** (`app/service/authsession`) хранит ключи, привязки perm/temp, соли и авторизации в MySQL. Кэш — в Redis через go-zero `CachedConn` (`internal/dao/auth_key.go:90-190`).
- **httpserver** (`app/interface/httpserver`) — вторая, отдельная реализация HTTP-транспорта MTProto на go-zero rest, порт 8801 (`etc/httpserver.yaml`). Handshake в ней скопирован (`internal/dao/handshake.go`).

### Транспорты (`gnetway/internal/server/gnet/codec`)
- Поддерживаются abridged, intermediate, padded intermediate и full (с CRC32 и seqno). Сервер требует obfuscated2 (флаг `-obfuscated=true` по умолчанию, `mtproto_transport_codec.go:122-125`), открытый abridged по умолчанию не принимается. WebSocket работает поверх gobwas/ws (`ws/codec.go`).
- **Fake-TLS (`ee`-секреты) и MTProxy-секрет (`dd`) не реализованы.** Obfuscated2 используется без секрета — то есть со стандартной схемой клиент→DC, в которой ключи CTR передаются открытым текстом в 64-байтном заголовке. От пассивного наблюдателя это не защищает, это только маскировка (`mtproto_transport_codec.go:287-337`).
- Максимальный кадр — 16 МБ (`mtproto_transport_codec.go:78`). Минимальная длина кадра проверяется только как `> 8` байт (`mtproto_abridged_codec.go:166,182`).
- Quick ACK реализован (`server_gnet.go:296-334`).
- Поддерживается PROXY protocol v1, включается для конкретного адреса в конфиге (`server_gnet.go:214-252`, `config.go:83`).

### Создание auth key (`gnetway/internal/server/gnet/handshake.go`)
- `pq` — **фиксированная константа** `0x17ED48941A08F981` с заранее известными `p`/`q` (`handshake.go:56-60`). Proof-of-work от клиента не требуется, клиент может заранее посчитать ответ.
- DH-группа — стандартный 2048-битный safe prime Telegram, `g=3` (`handshake.go:90-115`). Секрет сервера `a` — 2048 случайных бит (`handshake.go:588`).
- `req_DH_params`: RSA расшифровывается вручную (raw RSA, `crypto/rsa_cryptor.go:72-76`), затем снимается RSA_PAD (AES-IGE с temp_key). **Хэш `SHA256(temp_key + data_with_padding)` не проверяется** — проверка закомментирована (`handshake.go:496-510`). Nonce, server_nonce, p, q и pq сверяются (`handshake.go:384-413, 550-580`). Поддерживаются `p_q_inner_data`, `_dc`, `_temp`, `_temp_dc`.
- `set_client_DH_params`: **SHA1 от расшифрованных данных не проверяется** (`handshake.go:736`), **`g_b` не проверяется на диапазон** (`1 < g_b < p-1`, `2^{2048-64} < g_b < p-2^{2048-64}`, `handshake.go:766-773`), состояние handshake (`ctx.State`) тоже не проверяется. Коллизия auth_key_id не проверяется (TODO на `handshake.go:793`).
- Начальная соль считается как `new_nonce[0:8] XOR server_nonce[0:8]` и живёт 30 минут — по спецификации (`handshake.go:867-876`).
- Сохранение ключа: gateway → `session.setAuthKey` → authsession → MySQL `auth_keys.body` — **base64 без шифрования** (`authsession/internal/dao/auth_key.go:164-190`, `teamgramd/deploy/sql/1_teamgram.sql:57-63`). Параметр `expires_in` temp-ключей **игнорируется** (`auth_key.go:165-166`), поэтому temp-ключи живут вечно.

### Шифрование сообщений (`proto@v0.228.0/mtproto/crypto/auth_key.go`)
- MTProto 2.0: `msg_key = SHA256(auth_key[88+x:120+x] + plaintext)[8:24]`, KDF по SHA256 — корректно (`auth_key.go:170-194, 236-270`). При шифровании паддинг 12..27 байт, случайный — это допустимо (спецификация разрешает 12..1024).
- При расшифровке msg_key проверяется по всему расшифрованному буферу, включая паддинг, — корректно (`auth_key.go:306-341`). Но:
  - длина паддинга (12..1024) и выравнивание `message_data_length % 4` **не проверяются**, хотя это требование security guidelines;
  - если `rawData` короче 32 байт, происходит **panic** на `x[28:]` (`auth_key.go:315`);
  - сравнение msg_key не константное по времени (`bytes.Equal`);
  - **при ошибке в сообщение об ошибке пишутся `aesKey`, `aesIV` и `auth_key[88:120]`** (`auth_key.go:329-336`), а gateway логирует его вместе с полным payload (`server_gnet.go:315`).

### Session: msg_id, соли, повторы (`session/internal/sess/`)
- Соль: принимается текущая, а также прошлая в течение 300 с после `valid_until`. При несовпадении отправляется `bad_server_salt` (`session_service_messages_about_messages.go:64-104`). Соли генерируются через `math/rand` (`authsession/internal/dao/salts.go`, строка ~206).
- msg_id: сообщение отвергается, если оно старше 60 с (код 16) или **из будущего более чем на 300 с** (код 17). Это перевёрнутое окно: спецификация требует отвергать сообщения старше 300 с и из будущего более чем на 30 с. Проверяются `msg_id % 4 == 0` и «слишком старый» (`...about_messages.go:266-321`). **Проверки seqno (32/33/34/35) закомментированы** (`...about_messages.go:327-360`). Проверка, что младшие 32 бита msg_id не нулевые, отсутствует (TODO на строке 290).
- Защита от повторов держится на дедупликации по `msg_id` в `sessionInboundQueue` (до 400 id, `session_inbound_queue.go:202,294`) и на временно́м окне. Очередь хранится только в памяти, поэтому после рестарта или вытеснения сессии повтор внутри окна будет обработан снова.
- Контейнеры проверяются в `checkContainer`, gzip_packed распаковывается (`session.go:242-262`). `msgs_ack`, `msgs_state_req`/`info`, `msg_resend_req`, `msg_detailed_info`, `get_future_salts`, `ping`, `ping_delay_disconnect`, `destroy_session`, `http_wait` реализованы.
- `invokeAfterMsg`/`invokeAfterMsgs` просто разворачиваются, без ожидания указанного сообщения (`session_invoke.go:64-90`). Порядок отчасти сохраняется благодаря последовательному `rpcRunLoop`.
- `destroy_auth_key` — **заглушка**: отвечает `destroy_auth_key_ok`, но ничего не удаляет (`session_service_messages.go:116-128`).
- RPC до логина пропускаются по белому списку (`check_api_request_type.go`, `checkRpcWithoutLogin`). В этот список входят, например, `upload.getFile` и `upload.getWebFile` с пометкой «TODO debug» (`check_api_request_type.go`, около строки 125).

### Temp-ключи / PFS
- `auth.bindTempAuthKey` обрабатывает сам session, напрямую через authsession (`main_auth_wrapper.go:1091-1100`). Обработчик в bff заблокирован («enterprise»).
- Gateway, получив сообщение по temp-ключу, ещё не привязанному к perm, «угадывает» привязку: он парсит открытый текст, находит `auth.bindTempAuthKey` и **сразу** записывает `PermAuthKeyId` в кэш, ещё до проверки (`server_gnet.go:344-388`).
- `auth.dropTempAuthKeys` заблокирован. Срок жизни temp-ключей не соблюдается (см. выше). Итог: PFS формально поддерживается, но на деле её нет.

### Логин и аккаунты (`app/bff/authorization`)
- `auth.sendCode`: код из 5 цифр генерируется через `math/rand` (`authorization/internal/dao/phone_code.go:48`, `marmota/pkg/random2/random_string.go:24,31`). Провайдер `none` (`pkg/code/none/none_verify_code.go:38-46`) не отправляет SMS и **принимает только `12345`**. Код по умолчанию также отправляется сообщением в уже открытые сессии пользователя (`auth.sendCode_handler.go:340-352`).
- Лимитов попыток нет: `CheckCanDoAction` всегда возвращает `nil` (`authorization/internal/dao/phone_code.go:107-117`), счётчик попыток закомментирован (`logic/auth_logic.go:178-183`). Нигде нет FLOOD_WAIT.
- **2FA:** `auth.checkPassword` возвращает `auth.authorization` **без проверки SRP** (`authorization/internal/core/auth.checkPassword_handler.go:28-47`). `account.getPassword` отдаётся из заглушки `has_password=false` (`bff/bff/client/fake_rpc_result.go:124-137`). `requestPasswordRecovery`, `recoverPassword` и `resetPassword` заблокированы. SRP-примитивы в библиотеке есть (`crypto/srp_util.go`), но не используются.
- Сессии: `account.getAuthorizations` и `account.resetAuthorization` реализованы (последний — с фильтром по `UserId`, `authsession.resetAuthorization`). `auth.resetAuthorizations` («завершить все») заблокирован.
- QR-логин: `auth.exportLoginToken`, `acceptLoginToken` и `importLoginToken` есть (bff qrcode). Детально не проверялись.
- Passkey-логин: обработчики заблокированы.

### Конфиги и развёртывание
- `teamgramd/etc/*.yaml`: MySQL `root:` без пароля, `Log.Level: debug` во всех сервисах, `MinIO SecretAccessKey: miniostorage`, `Code.Name: none`.
- `teamgramd/etc2/*.yaml` (Docker): все gRPC-сервисы слушают `0.0.0.0` (например, `etc2/authsession.yaml:2`), Prometheus тоже на `0.0.0.0`.
- `docker-compose.yaml:5-20` публикует на хосте **все внутренние порты** (20010 bff, 20020 biz, 20450 authsession, 20120 session и т. д.). `docker-compose-env.yaml` публикует etcd 2379/2380 (без auth), Redis 6379 (без пароля), MySQL 3306 (`root`/`root`, `teamgram`/`teamgram`), MinIO 9000/9001 (`minio`/`miniostorage`), Elasticsearch 9200 (`xpack.security.enabled=false`), Grafana (`admin`/`admin`), Jaeger, Kafka.
- TLS и аутентификации на внутреннем gRPC нет (поиск `StrictControl`, `credentials.NewTLS` и `tls.Config` ничего не находит).
- TLS на клиентских портах тоже нет. Для MTProto это допустимо, но WebSocket (11443) и HTTP (8801) работают без TLS.
- Dockerfile: `golang:1.23.0` и `ubuntu:latest`.

### Конкурентность и производительность
- gnet: обработка в event-loop. Конфиг по умолчанию `Multicore: false`, то есть вся криптография TCP/WS (AES-IGE, SHA256) выполняется на одном ядре. Для 100 пользователей это приемлемо.
- Тяжёлые операции (RSA, DH modexp, gRPC в session) уходят в пул `ants` (`goroutine.Default()`, `server.go:79`), результат возвращается через `eng.Trigger`. Ants восстанавливается после panic в задачах. Код в `Trigger`-колбэках и в `OnTraffic` выполняется в event-loop gnet. **(не подтверждено: исходников gnet v2.9.1 нет в module cache, но, насколько известно, gnet v2 не перехватывает panic в event-loop, и panic роняет процесс.)**
- Session: на каждый perm-ключ свой `MainAuthWrapper`, RPC выполняются **строго последовательно** (`rpcRunLoop`). Один медленный RPC (например, большой upload или getHistory) блокирует все остальные запросы этого устройства (head-of-line blocking).
- LRU-кэш ключей в gateway: `Size()` возвращает 1, а ёмкость задана как `AuthKeyCacheM*1024*1024`. В итоге «10 МБ» на деле означает 10 млн записей, и в этот же кэш попадают HTTP handshake-состояния (`cache_auth_key.go:29-31,77-81`, `server.go:73-77`).
- Таймауты простоя: 30 с до первого байта, 300 с после трафика (`server_gnet.go:120-123, 207-213`) — разумно.

---

## Находки

Шкала: **Critical** — прямой захват аккаунтов, ключей или инфраструктуры; **High** — массовая утечка, DoS до аутентификации или серьёзное нарушение протокола; **Medium** — ослабление защиты или ограниченный DoS; **Low** — гигиена.

| # | Severity | Файл:строка | Проблема | Риск |
|---|---|---|---|---|
| 1 | **Critical** | `pkg/code/none/none_verify_code.go:42-46`, `teamgramd/etc/bff.yaml:28-34`, `bff/authorization/internal/core/auth.signIn_handler.go:89-99` | При `Code.Name: none` (значение по умолчанию) `VerifySmsCode` принимает фиксированный `12345` для любого номера. | Любой, кто знает номер телефона, входит в чужой аккаунт. |
| 2 | **Critical** | `bff/authorization/internal/dao/phone_code.go:107-117`, `logic/auth_logic.go:178-183` | Нет лимита попыток ввода кода и FLOOD_WAIT на sendCode/signIn; код из 5 цифр создаётся через `math/rand`. | Даже с настоящим SMS-провайдером код подбирается перебором 10^5 вариантов. |
| 3 | **Critical** | `gnetway/internal/server/gnet/server_gnet.go:344-355, 416-428`; `session/internal/core/session.sendDataToSession_handler.go:39`; `session/internal/sess/main_auth_wrapper.go:919-924, 684-698` | По temp-ключу без привязки gateway берёт `perm_auth_key_id` из **непроверенного** `auth.bindTempAuthKey`, кладёт его в кэш (`PutAuthKey(clone)`) и пересылает в session. Session находит `MainAuthWrapper` жертвы и выполняет RPC с `UserId` жертвы. Ответы шифруются temp-ключом атакующего. Остальные сообщения того же контейнера тоже выполняются. Кэш остаётся «отравленным» даже после отказа привязки. | **Полный захват сессии любого пользователя**, если известен его `perm_auth_key_id`. Этот id 8 байт передаётся открытым текстом в каждом пакете клиентов без PFS: obfuscated2 снимается пассивно, потому что ключи CTR есть в заголовке. Id также пишется в логи. |
| 4 | **Critical** | `service/authsession/internal/core/authsession.bindTempAuthKey_handler.go:123-160` | `bind_auth_key_inner` расшифровывается ключом `in.PermAuthKeyId`, а привязка делается к `inner.perm_auth_key_id` и `inner.temp_auth_key_id` **без сверки** с внешними полями, без проверки `nonce`, `expires_at`, `temp_session_id` и того, что запрос пришёл именно по этому temp-ключу. Ошибки `UnsafeBindKeyIdV2` игнорируются. | Любой зарегистрированный пользователь навсегда (в БД) привязывает свой temp-ключ к perm-ключу жертвы. Это та же атака, что в #3, но устойчивая к перезапуску. |
| 5 | **Critical** | `teamgramd/bin/server_pkcs1.key` (в git), `teamgramd/etc/gnetway.yaml:24-28` | Приватный RSA-ключ сервера публичен (fingerprint `12240908862933197005`). | Активный MITM на этапе создания ключа: атакующий выдаёт себя за сервер. Клиенты, собранные с этим публичным ключом, не защищены. |
| 6 | **Critical** (при Docker-развёртывании) | `docker-compose.yaml:5-20`, `teamgramd/etc2/*.yaml:2`, `docker-compose-env.yaml:42-44,66-67,85-90,112-117,217,235-237` | Все внутренние gRPC-порты, etcd, Redis, MySQL, MinIO, ES и Grafana опубликованы на 0.0.0.0. Аутентификации и TLS между сервисами нет; `RpcMetadata` (`UserId`) задаётся вызывающей стороной. Пароли по умолчанию: `root`/`root`, `minio`/`miniostorage`, `admin`/`admin`. Docker обходит ufw/iptables-INPUT. | Удалённый вызов bff/biz от имени любого пользователя; подмена сервисов через etcd; чтение `auth_keys` из MySQL/Redis, то есть расшифровка всего трафика. |
| 7 | **High** | `bff/authorization/internal/core/auth.checkPassword_handler.go:28-47`, `bff/bff/client/fake_rpc_result.go:124-137` | `auth.checkPassword` выдаёт авторизацию без проверки SRP; `account.getPassword` — заглушка. | 2FA отсутствует. Если где-то возникает состояние `NeedPassword`, его можно обойти. |
| 8 | **High** | `gnetway/.../handshake.go:690, 807`; `httpserver/internal/dao/handshake.go:236,484,492,540,593` | Состояние handshake (`A` — секрет DH сервера, `NewNonce`) и запрос пишутся в лог уровня Info (`%s` на структуре печатает сырые байты). В httpserver дополнительно логируется `client_DH_inner_data` с `g_b`. | Любой с доступом к логам (filebeat → ES без аутентификации, см. #6) восстанавливает auth_key каждого нового ключа и расшифровывает весь трафик. |
| 9 | **High** | `proto@v0.228.0/mtproto/crypto/auth_key.go:329-336`; `gnetway/.../server_gnet.go:315`, `server_http.go:284` | Ошибка расшифровки содержит `aesKey`, `aesIV` и `auth_key[88:120]`, а gateway логирует её вместе с полным пакетом. Достаточно отправить мусор с чужим `auth_key_id`. | Ключевой материал жертвы утекает в логи по запросу атакующего. |
| 10 | **High** | `gnetway/.../handshake.go:695-725, 737`; `saveAuthKeyInfo` `handshake.go:867-870` | `set_client_DH_params` не проверяет `ctx.State`. Если отправить его сразу после `req_pq` (nonce и server_nonce известны), `ctx.NewNonce == nil`, и `ctx.NewNonce[:4]` вызывает panic. Если зашифрованные данные короче 20 байт, panic на `decryptedData[20:]`. Всё это выполняется синхронно в `OnTraffic`. | **Падение gnetway до аутентификации** одним пакетом **(не подтверждено: зависит от того, восстанавливается ли gnet после panic; по имеющимся данным — нет)**. |
| 11 | **High** | `gnetway/.../server_gnet.go:313`, `server_http.go:282`, `crypto/auth_key.go:315` | Длина зашифрованного кадра не проверяется (минимум 8+16+32). `mmsg[24:]`, `x[28:]` и т. п. вызывают panic на коротком кадре с существующим `auth_key_id` (атакующий может создать свой ключ). | Падение процесса **(не подтверждено, см. #10)**. В httpserver go-zero перехватывает panic. |
| 12 | **High** | `proto@v0.228.0/mtproto/decode.go:326,356,386,416,461` | `make([]T, size)` выполняется с `size` из входных данных (до 2^31) без сравнения с оставшимися байтами. Нешифрованные handshake-сообщения декодируются через `dBuf.Object()` с любым конструктором (`message_pack_util.go:43-53`), например `msgs_ack` с вектором 0x7fffffff. | Попытка выделить 16–32 ГБ — фатальный `out of memory` (его нельзя перехватить через `recover`) **до аутентификации** **(вероятно; не воспроизводилось, зависит от overcommit ОС)**. |
| 13 | **High** | `proto@v0.228.0/mtproto/parsed_manually_types.go:306-330` | `gzip_packed` распаковывается через `io.Copy` без ограничения размера. Для temp-ключа без привязки это происходит прямо в event-loop gateway (`tryGetUnknownTLObject`), в остальных случаях — в session. | Gzip-бомба: кадр 16 МБ раздувается до гигабайт и вызывает OOM. Нужен только свой auth_key, а его может создать кто угодно. |
| 14 | **High** | `authsession/internal/dao/auth_key.go:164-190`; `1_teamgram.sql:57-63` | auth_key хранится в MySQL как base64 без шифрования и дублируется в Redis без пароля. `expires_in` temp-ключей игнорируется, temp-ключи не удаляются. | Утечка БД или Redis означает расшифровку всего прошлого и будущего трафика. PFS бесполезна. |
| 15 | **High** | `gnetway/.../ws/codec.go:102-116, 143-203` | Размер WS-кадра и сообщения не ограничен, `w.Buf` и `cachedBuf` растут без предела. Таймаут 300 с продлевается любым трафиком. | Исчерпание памяти до аутентификации медленным потоком данных. |
| 16 | **Medium** | `gnetway/.../handshake.go:56-60, 323-328` | Фиксированный `pq`: клиент не выполняет PoW, а сервер на каждый `req_DH_params` делает приватную RSA-операцию и DH modexp. Лимитов на IP нет. | Дешёвая для атакующего CPU-DoS на RSA/DH. |
| 17 | **Medium** | `gnetway/.../handshake.go:496-510, 736, 766-773` | Не проверяются хэш RSA_PAD, SHA1 в `client_DH_inner_data` и диапазон `g_b`. | Нарушение спецификации. `g_b ∈ {1, p-1}` даёт предсказуемый ключ; риск прежде всего для самого клиента, но это обязательная проверка. |
| 18 | **Medium** | `session/internal/sess/session_service_messages_about_messages.go:266-280, 327-360, 290` | Окно времени перевёрнуто: из будущего принимаются сообщения до +300 с вместо +30 с. Проверки seqno отключены, младшие 32 бита msg_id не проверяются. | Ослабленная защита от повторов и манипуляций порядком. |
| 19 | **Medium** | `session/internal/sess/session_inbound_queue.go:202,294` | Дедупликация — только 400 последних msg_id в памяти. Если внутри окна в 60 с было больше 400 сообщений, после рестарта session или вытеснения сессии повтор будет обработан. | Повтор (replay) перехваченных шифротекстов (например, `messages.sendMessage`) внутри 60 с. |
| 20 | **Medium** | `crypto/auth_key.go:314-320` | Не проверяются паддинг 12..1024, `message_data_length % 4`; `messageLen+32` может переполнить uint32. | Расхождение с security guidelines. Прямой эксплуатации не найдено. |
| 21 | **Medium** | `session/internal/sess/session_service_messages.go:116-128` | `destroy_auth_key` отвечает `ok`, но ключ не удаляет. | Пользователь считает, что ключ уничтожен, хотя он остаётся действующим. |
| 22 | **Medium** | `gnetway/.../cache_auth_key.go:29-31, 77-81`; `server.go:73-77` | LRU считает записи, а не байты (10 млн записей «за 10 МБ»). HTTP handshake-состояния попадают в тот же кэш без аутентификации. | Рост памяти и вытеснение auth-ключей из кэша флудом `req_pq` по HTTP. |
| 23 | **Medium** | `session/internal/sess/check_api_request_type.go` (около стр. 125) | `upload.getFile` и `upload.getWebFile` разрешены без логина (помечено «TODO debug»). | Возможно скачивание файлов по известным `file_reference`/id без авторизации **(не проверено, есть ли проверка доступа ниже, в media/dfs)**. |
| 24 | **Medium** | `httpserver` (`etc/httpserver.yaml:5-6`) | Отдельный HTTP-сервер: `Timeout: 0`, `MaxBytes: 64MB`, дублирует handshake с теми же дефектами (#8, #17). | Slowloris или исчерпание памяти; лишняя поверхность атаки. |
| 25 | **Medium** | `teamgramd/etc/*.yaml` `Log.Level: debug` | Debug-логи во всех сервисах. Session и bff логируют содержимое запросов (например, `onNewSessionCreated - %#v`, `session.go:268`). | Переписка и метаданные оказываются в логах. |
| 26 | **Medium** | `Dockerfile:1,6`; `go.mod` | Go 1.23.0: в stdlib с тех пор закрыт ряд CVE (net/http, crypto/x509, encoding/gob и др.). `golang.org/x/image v0.0.0-2021…` — известные DoS в TIFF/WebP-декодерах (например, CVE-2022-41727, CVE-2023-29407/29408, CVE-2024-24792), а изображения от пользователей декодируются. `ubuntu:latest` не закреплён. **(govulncheck не запускался — список CVE ориентировочный.)** | DoS media-сервиса через подготовленные изображения; невоспроизводимые сборки. |
| 27 | **Low** | `authsession/internal/dao/salts.go` (около стр. 206) | Соли генерируются через `math/rand`. | Предсказуемость солей. Соль не секрет, но спецификация предполагает случайность. |
| 28 | **Low** | `gnetway/.../handshake.go:462-466` | `rsa.Decrypt` возвращает `big.Int.Bytes()` без ведущих нулей, а проверка `len != 256` отбрасывает около 1/256 легитимных handshake. | Редкие повторы handshake у клиентов. |
| 29 | **Low** | `gnetway/.../handshake.go:791-833` | `ctx` (`HandshakeStateCtx`) читается в горутине пула и одновременно может изменяться в event-loop (data race). | Нестабильность при одновременных повторах. |
| 30 | **Low** | `gnetway/.../http/http_codec.go:155-169` | `Access-Control-Allow-Origin: *`; `Strict-Transport-Security` отдаётся по HTTP. | Косметика. |
| 31 | **Low** | `crypto/auth_key.go:328` | msg_key сравнивается не за константное время. | Теоретический тайминг-канал. |

---

## Отсутствующие возможности

| Возможность | Статус |
|---|---|
| **PFS (temp-ключи, `auth.bindTempAuthKey`)** | Формально есть, по сути сломано: привязка не проверяется (#3, #4), срок жизни игнорируется, temp-ключи хранятся вечно (#14), `auth.dropTempAuthKeys` заблокирован. |
| **2FA (SRP, `account.getPassword` / `updatePasswordSettings` / `auth.checkPassword`)** | Нет. checkPassword — обход, getPassword — заглушка, восстановление пароля заблокировано. |
| **`destroy_auth_key`** | Заглушка (#21). |
| **Завершить все другие сессии (`auth.resetAuthorizations`)** | Заблокировано. Работает только `account.resetAuthorization` по одной. |
| **Passkey-логин, email-логин, Firebase SMS** | Заблокированы. |
| **Реальная доставка SMS** | Только провайдер `me` (свой HTTP) или `none` (фиксированный код). Для закрытой группы удобнее инвайт-коды или ручная выдача кода администратором. |
| **Ограничение регистрации (закрытая аудитория)** | Не найдено: любой номер может пройти `signUp` **(смежная область, детально не проверялась)**. |
| **Rate limiting / FLOOD_WAIT** | Нет нигде: ни на handshake, ни на логин, ни на RPC. |
| **Fake-TLS / MTProxy-секреты** | Нет. Только obfuscated2 без секрета: трафик легко классифицируется DPI. |
| **TLS для WS/HTTP и между сервисами** | Нет. |
| **Шифрование ключей и секретов в хранилище** | Нет (auth_keys в base64). |
| **Проверки seqno, паддинга, msg_id (младшие биты)** | Отключены или отсутствуют. |
| **Ротация RSA-ключа** | Конфиг поддерживает список ключей (`RSAKey: []`), но в репозитории лежит только публичный тестовый ключ. |

---

## Рекомендации и оценка объёма

### P0 — до любого внешнего доступа (≈ 5–8 рабочих дней)
1. **Сгенерировать собственный RSA-2048**, удалить `server_pkcs1.key` из репозитория (и из истории, если репозиторий публичный), встроить новый публичный ключ в патченные клиенты. *0.5 д.*
2. **Убрать `12345`**: никогда не собирать сервер с `none` в проде. Для закрытой группы — whitelist номеров или инвайт-коды плюс выдача кода через доверенный канал или админ-бота. Добавить лимиты: не больше 5 попыток на `phone_code_hash`, sendCode не чаще N раз в час на номер и IP; код генерировать через `crypto/rand`. *2 д.*
3. **Исправить bindTempAuthKey**:
   - в authsession проверять `inner.perm_auth_key_id == in.PermAuthKeyId`, `inner.nonce == in.Nonce`, `inner.expires_at == in.ExpiresAt`, `inner.temp_auth_key_id ==` ключ, по которому пришёл запрос (его нужно передать из session в метаданных), а также `temp_session_id`, msg_id внешнего сообщения и то, что temp-ключ ещё не привязан;
   - в gateway **не** писать `PermAuthKeyId` в кэш, пока authsession не подтвердит привязку. Неподтверждённые temp-ключи маршрутизировать в «пустой» MainAuthWrapper (`perm=0`), где разрешён только `bindTempAuthKey`; после успеха обновлять кэш по ответу.
   *2–3 д. с тестами.*
4. **Закрыть сеть**: в compose оставить наружу только 10443/5222/11443 (и 8801, если нужен HTTP). Всё внутреннее — в отдельную docker-сеть без `ports:` или на `127.0.0.1:`. Задать пароли для MySQL, Redis (`requirepass`), etcd (auth или хотя бы изоляция), MinIO, Grafana; ES выключить или включить security. *1 д.*
5. **Перестать логировать секреты**: убрать `%s ctx` в handshake (gnetway и httpserver), вывод `client_DH_inner_data`, ключи в ошибках `AesIgeDecrypt` (это fork или патч `teamgram/proto`); перевести `Log.Level` в `info`/`error`. *0.5–1 д.*
6. **Pre-auth panic/OOM**: проверка состояния и длины в `onSetClientDHParams`; минимальная длина зашифрованного кадра — 8+16+32 байта и кратность 16; `recover` в `OnTraffic` и в `Trigger`-колбэках (обёртка); ограничить `size` векторов в `DecodeBuf` значением `(m.size-m.off)/elemSize`; лимит на размер распаковки gzip (например, 16 МБ через `io.LimitReader`) и запрет вложенного gzip; лимиты WS-кадра и буфера. Часть изменений нужно сделать в `teamgram/proto` — через fork или `replace`. *2 д.*

### P1 — до запуска для пользователей (≈ 2 недели)
7. **2FA (SRP)**: реализовать `account.getPassword`, `updatePasswordSettings`, `auth.checkPassword` по спецификации ([SRP](https://core.telegram.org/api/srp)). Примитивы есть в `crypto/srp_util.go`. Хранить `v` (verifier) и salt; восстановление через email — опционально. *4–6 д.*
8. **Протокольные проверки по [security guidelines](https://core.telegram.org/mtproto/security_guidelines)**: диапазон `g_b`, SHA1/SHA256-хэши в handshake, паддинг 12..1024 и `len%4`, окно msg_id (−300 / +30 с), seqno, ненулевые младшие биты msg_id, константное сравнение msg_key, `crypto/rand` для солей. *2–3 д.*
9. **Temp-ключи**: соблюдать `expires_in`, удалять истёкшие, реализовать `auth.dropTempAuthKeys` и настоящий `destroy_auth_key` (удаление ключа, разрыв соединений, сброс кэшей gateway и session). *2 д.*
10. **Rate limiting**: на IP — handshake (`req_DH_params`) и число соединений; на ключ — RPC/сек с FLOOD_WAIT. В go-zero есть `limit.PeriodLimit`/`TokenLimiter` на Redis. *2 д.*
11. **`auth.resetAuthorizations`** («завершить все сессии») и проверка, что `auth.logOut` удаляет или помечает ключ. *1 д.*
12. **Обновить зависимости**: Go ≥ 1.24.x (или 1.25), `golang.org/x/image` до последней версии, закрепить базовый образ. Прогнать `govulncheck ./...` и устранить находки. *1 д.*

### P2 — укрепление (≈ 1–2 недели, по желанию)
13. Шифрование `auth_keys.body` на уровне приложения (AES-GCM с ключом из KMS/файла вне БД); Redis — только как кэш с TTL и паролем.
14. mTLS или хотя бы общий токен для gRPC между сервисами; не доверять `RpcMetadata.UserId`, пришедшему не из session.
15. Fake-TLS (`ee`) или MTProxy-секрет, если важна устойчивость к DPI и блокировкам; TLS для WS/HTTP через reverse-proxy (nginx/caddy) с PROXY protocol v1 (уже поддерживается).
16. Убрать дублирующий `httpserver`, если HTTP-транспорт gnetway достаточен, — одна реализация handshake вместо двух.
17. Head-of-line в `rpcRunLoop`: параллельная обработка независимых RPC (upload/download отдельно) с сохранением порядка там, где этого требует `invokeAfterMsg`.
18. Fuzz-тесты (`go test -fuzz`) для кодеков транспорта, `DecodeBuf.Object` и handshake — это дёшево и сразу находит panic вроде #10–#13.

### Сводная оценка
| Блок | Объём |
|---|---|
| P0 (обязательно до выхода в сеть) | 5–8 чел.-дней |
| P1 (обязательно до запуска пользователей) | 10–14 чел.-дней |
| P2 (желательно) | 5–10 чел.-дней |
| **Итого до «production-grade для 100 пользователей»** | **≈ 3–5 недель** одного Go-разработчика, знакомого с MTProto |

Часть исправлений (#9, #12, #13, #20, #31) нужно делать в `github.com/teamgram/proto`. Нужно заранее решить, вести собственный fork с `replace` в `go.mod` или отправлять PR апстриму.
