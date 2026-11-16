# 0065. CDN-DC Pluma: шифр файлов на краю, ключи и токены дома

- Статус: accepted (2026-11-16). Владелец подтвердил политику: какие файлы идут через CDN (§2, 2026-11-15); «Решения к подтверждению владельцем» ждут его слова.
- Задача: 102 (ветка `task/102-cdn`). Обслуживаются `upload.getFile` с `cdn_supported` (ответ `upload.fileCdnRedirect`), `upload.getCdnFileHashes`, `upload.reuploadCdnFile`, `help.getCdnConfig`; `help.getConfig` начинает отдавать `dc_options`; новый сервис `cdn` на VPS обслуживает `upload.getCdnFile`.
- Связано: ADR 0057 (дом — ядро, VPS — труба для шифра; §3 «Файлы» называет CDN-DC направлением), ADR 0061 (туннель WireGuard, край на VPS, mTLS управления SFU как образец), ADR 0059 (mTLS управления), ADR 0041 (всё открыто всем).
- Развивает ADR 0057 §3 и закрывает его пункты «проверить». Остальные решения 0057 и 0061 не меняет. Дополняет 0057 §2: на VPS появляется хранилище шифра файлов и сервер MTProto, который завершает шифрование транспорта (но не файлов).

## Контекст

### Протокол (core.telegram.org/cdn, прочитано 2026-11-15)
- `upload.getFile` может ответить `upload.fileCdnRedirect{dc_id, file_token, encryption_key, encryption_iv, file_hashes}`; клиент идёт на CDN-DC с `upload.getCdnFile(file_token, offset, limit)`.
- Ключ AES-256-CTR свой у каждого файла. Для смещения клиент берёт `encryption_iv` и заменяет последние 4 байта на `offset / 16` (big-endian). CDN «don't have the keys to decrypt files».
- `fileHash` — SHA-256 части файла от `offset` длиной `limit`; хэши приходят в редиректе, в `upload.getCdnFileHashes` и `upload.reuploadCdnFile` (оба — на мастер-DC).
- На `upload.cdnFileReuploadNeeded{request_token}` клиент зовёт `upload.reuploadCdnFile` на мастер-DC. `FILE_TOKEN_INVALID` (getCdnFile, reupload, getCdnFileHashes) и `REQUEST_TOKEN_INVALID` (reupload) — «продолжить с мастер-DC через `upload.getFile`».
- Правила диапазона: `offset` и `limit` кратны 4096, 1 МБ кратен `limit`, часть внутри одного мегабайта.
- CDN принимает только `upload.getCdnFile`, `initConnection`, `invokeWithLayer`. Его auth_key «may be deleted at any given moment» (−404, клиент делает новый). Клиенты не шлют на CDN личных данных `initConnection` и не принимают от него обновлений.
- Ключ CDN клиент сверяет со списком `help.getCdnConfig`.
- Ошибки методов (core.telegram.org/method/…, 2026-11-15): `upload.getCdnFile` — `FILE_TOKEN_INVALID`, `METHOD_INVALID`; `upload.reuploadCdnFile` — `CDN_METHOD_INVALID`, `CDN_UPLOAD_TIMEOUT` (500), `FILE_TOKEN_INVALID`, `LOCATION_INVALID`, `REQUEST_TOKEN_INVALID`, `RSA_DECRYPT_FAILED`; `upload.getCdnFileHashes` — `CDN_METHOD_INVALID`, `FILE_TOKEN_INVALID`, `RSA_DECRYPT_FAILED`. Сколько хэшей отдавать за раз, страницы не говорят.
- Слой 233 (tdesktop `Telegram/SourceFiles/mtproto/scheme/api.tl`): `upload.fileCdnRedirect#f18cda44` (:525), `dcOption#18b7a10d … cdn:flags.3 static:flags.4 this_port_only:flags.5 … secret:flags.10?bytes` (:527), `upload.cdnFileReuploadNeeded#eea8e46e` (:1071), `cdnPublicKey#c982eaba` (:1074), `cdnConfig#5725e40a` (:1076), `fileHash#f39b035c` (:1180), `upload.getFile … cdn_supported:flags.1?true` (:2897), `upload.getCdnFile` (:2900), `upload.reuploadCdnFile` (:2901), `upload.getCdnFileHashes` (:2902), `help.getCdnConfig` (:2911). Флаг `cdn_supported` страница CDN не упоминает; что редирект допустим только с ним, следует из клиентов ниже.

### Клиенты (`~/pluma-work/reference-src-new`, закреплённые выпуски: Android 13.2.1, iOS release-13.2, tdesktop 7.4.1, Web K)
- **Android — полная поддержка, без патча.** `cdn_supported = true` на каждом `upload.getFile`, кроме веб-файлов (`FileLoadOperation.java:2465-2470`). Редирект требует IV 16 и ключ 32 байта (`:2592-2610`); `FILE_TOKEN_INVALID` из `getCdnFile` — назад на мастер (`:2583-2589`); reupload: вектор — хэши и повтор, `FILE_TOKEN_INVALID`/`REQUEST_TOKEN_INVALID` — назад на мастер, прочее — отказ загрузки (`:2623-2653`). Хэши проверяются кусками по 128 КБ (`cdnChunkCheckSize`, `:168`, `:1889-1891`) по расшифрованным байтам (`:1900-1906`, `:2036-2039`). Адреса из `dc_options` заменяются только у перечисленных DC, и у каждого — все четыре группы (v4, v6, v4 media, v6 media) целиком (`jni/tgnet/ConnectionsManager.cpp:3486-3514`, `Datacenter.cpp:498-516`). Ключи CDN — по `help.getCdnConfig`, когда в `resPQ` нет известного отпечатка (`Handshake.cpp:354-407, 1017-1060`); CDN-соединение — постоянный ключ без привязки (`Datacenter.cpp:1076`), запросы без авторизации (`ConnectionsManager.cpp:2592-2594`), на −404 ключ пересоздаётся (`:850-855`). **Порты:** без `secret` Android перебирает для адреса порты `{порт адреса, 443, 5222, порт адреса}` (`Datacenter.h:84`, `Datacenter.cpp:337-345`); с `secret` — только порт адреса.
- **tdesktop — полная поддержка, без патча.** `f_cdn_supported` на каждом облачном файле (`storage/download_manager_mtproto.cpp:580-582`), стриминг идёт тем же путём. Хэш ищется по точному смещению запроса и сравнивается с SHA-256 всего расшифрованного буфера (`:690-737`); запросы по 128 КБ (`download_manager_mtproto.h:26`, `media_streaming_loader.h:29`); неверный хэш — отмена загрузки (`:704-713`). Токен-ошибки — назад на мастер (`:958-980`). **`help.getConfig` с непустым `dc_options` заменяет весь список DC:** `Config::apply` → `setFromList` (`mtproto/mtproto_config.cpp:285-289`) → `processFromList(overwrite=true)` начинает с пустой карты (`mtproto_dc_options.cpp:198-244`). Пустой `dc_options` только пишется в лог. `config_loader.cpp:234` — путь «special config» (резервные адреса, у Pluma отключён патчем D-005), не обычный config. Ключи CDN — `help.getCdnConfig` (`mtp_instance.cpp:599-608`, `mtproto_dc_options.cpp:613-660`). CDN — временный непривязанный ключ (`session_private.cpp:2604-2630`), `initConnection` с `device_model`/`system_version` = «n/a» (`:678-683`), −404 — новый ключ (`:2685-2711`). Эндпоинты без `tcpo_only` и без секрета tdesktop пробует и по HTTP (`mtproto_dc_options.cpp:686-688`); при включённом прокси оставляет только `static`, если такие есть (`:693-695`). Номер DC ≥ 1000 tdesktop считает временным (`mtp_instance.h:45`, `facade.h:75-78`).
- **iOS — нужен патч.** FetchV2 работает по умолчанию (`MultipartFetch.swift:1135`, `Network.swift:747-750`), флаг `cdn_supported` ставится только при `fastDownloads && !cdnDisabled` (`FetchV2.swift:974`), а `fastDownloads = engineKind == .rust` (`:505`); на iOS движок по умолчанию `.mtProtoKit` (`SyncCore_NetworkEngineSettings.swift:6-11`). Кроме флага, ветки CDN без `fastDownloads` ломаются: ошибка `getCdnFile` (и `FILE_TOKEN_INVALID`) — `.failure` → загрузка `.failed` (`:952-956`, `:1114-1115`); ошибка reupload — `.failed` (`:795-801`); после редиректа уже скачанные части забываются (`:1087-1089`); при неверном хэше нет счётчика и отката (`:635`, `:647`); ошибка запроса хэшей молча оставляет диапазон (`:1158`). Сам MtProtoKit CDN умеет: рабочие соединения `.cdn` (`Network.swift:1079-1094`, `MtProtoKitEngine.swift:541-555`, постоянный ключ без temp — `MTProto.m:909-912`), ключи `help.getCdnConfig` (`Network.swift:1032-1046`), повторный запрос ключей при несовпадении отпечатка (`MTDatacenterAuthMessageService.m:552-562`). Хэш — по расшифрованным байтам `cleanData` (`FetchV2.swift:616-617`), блоки 128 КБ (`:124`). Адреса из `dc_options` заменяются по каждому перечисленному DC (`ManagedConfigurationUpdates.swift:22-38`).
- **Web K — поддержки нет.** `upload.getFile` без `cdn_supported` (`src/lib/mtproto/apiFileManager.ts:483-487`), кода `fileCdnRedirect`/`getCdnFile` нет; `dc_options` читает только тест (`src/tests/api/getDialogs.test.ts:27`, поиск grep). Остаётся на `upload.getFile`.
- `upload.getFileHashes` не вызывает ни один клиент (grep по четырём деревьям).

### Сервер сегодня
- CDN-обработчики — заглушки апстрима «License key … required»: `help.getCdnConfig` — пустой список, `upload.getCdnFile` — пустые байты (Android прочёл бы конец файла, `FileLoadOperation.java:1884-1886`), `upload.getCdnFileHashes` и `upload.reuploadCdnFile` — пустые векторы (`app/bff/files/internal/core/*_handler.go`).
- `upload.getFile` не перенаправляет (`upload.getFile_handler.go:28-122`), правила диапазона уже проверяет (`:131-156`).
- `help.getConfig` отдаёт пустой `dc_options` (`teamgramd/bin/config.json:17`), клиенты держат вшитый адрес (`docs/ops/clients.md:13`).
- Вшитые адреса: Android — один IPv4 и необязательный IPv6 (`clients/patches/android.md:73-85`), tdesktop — `applyOneGuarded(1, Flag::f_static…)` с IPv4 и необязательным IPv6 (`clients/patches/desktop.md:92-98`), iOS — один DC 1 (`clients/patches/ios.md:85-87`). Шлюз опубликован только по IPv4 на 10443 и 5222 (`docs/ops/security-setup.md:81`), адрес 195.201.252.126 (ADR 0061).
- Рукопожатие (RSA_PAD, DH, temp/perm) и транспорты (abridged, intermediate, padded, obfuscated2) есть в gnetway (`app/interface/gnetway/internal/server/gnet/handshake.go`, `handshake_state.go`, `pq.go`, `codec/`), но во `internal` и сцеплены с сохранением ключа через session (`handshake.go:772-841`). Секрета в стиле MTProxy шлюз не принимает (`docs/tasks/092-ru-reachability.md`, раздел «Сервер сегодня»).

## Решение

### 1. Кто что знает
- **Дом:** открытый файл, ключ и IV каждого файла, токены, хэши, решение о редиректе. Всё — в MySQL дома.
- **VPS (узел `cdn`):** шифр файлов, свой RSA-ключ MTProto (создан на узле, не покидает его), временные auth_key клиентов в памяти, ключ подписи `request_token`, который ему выдаёт дом. Ни открытого содержимого, ни ключей файлов, ни ключей сервера дома.
- Узел видит адреса клиентов, размеры, время и **какие адреса берут один и тот же `file_token`**. Это новое по сравнению с трубой ADR 0061 (там видна только связь «адрес ↔ сервер»). Владелец это принял (2026-11-15).

### 2. Какие файлы идут через CDN (политика владельца, 2026-11-15)
- Только запросы с `cdn_supported` и без `precise` (правила CDN строже: 4 КБ против 1 КБ).
- Виды файлов: `inputDocumentFileLocation`, `inputPhotoFileLocation`, `inputEncryptedFileLocation` (файлы секретных чатов — шифр поверх их собственного шифра). Размер ≥ 1 МБ (`CDN_MIN_FILE_SIZE`, кратен 1 МиБ). Чаты любые.
- Не идут: аватары (`inputPeerPhotoFileLocation`, меньше порога), миниатюры наборов, потоки звонков, веб-файлы, secure, takeout.
- Редирект — только если на здоровом узле лежит **полная** копия файла (§5). Иначе файл отдаётся из дома, как сегодня, а копия ставится в очередь. Первый зритель большого файла частично качает из дома; со следующей части и у всех остальных — с узла.

### 3. Шифрование и хэши
- Для файла: случайные ключ 32 байта, IV 16 байт (последние 4 байта нулевые: клиент всё равно их заменяет), `file_token` — случайные 32 байта (уникальный индекс). Один шифр на файл для всех получателей и всех узлов.
- Шифр: AES-256-CTR, счётчик блока на смещении `p` — `IV[0:12] ‖ uint32_be(p/16)`, то есть стандартный 128-битный CTR от IV с нулевым хвостом. Совпадает с тем, что делают клиенты (tdesktop `download_manager_mtproto.cpp:694-698`, Android `:1900-1906`, iOS `FetchV2.swift:929-939`) для файлов до 64 ГБ.
- Хэши — SHA-256 **открытых** кусков по 128 КБ, выровненных по 128 КБ, последний короче. Хранятся одним блобом (32 байта на кусок: 1 МБ на файл 4 ГБ).
- Ответ хэшами: от `offset`, округлённого вниз до 128 КБ, до 64 кусков (8 МБ) — в редиректе, в `getCdnFileHashes` и в `reuploadCdnFile`. Число — наше (документация молчит, клиенты принимают любое: Android — по смещению, tdesktop — по точному смещению, iOS — диапазонами).

### 4. Токены и повторная загрузка
- Узел ищет файл по точным байтам `file_token`. Неизвестный токен — `upload.cdnFileReuploadNeeded{request_token}`.
- `request_token` = версия ‖ время (мс) ‖ первые 16 байт HMAC-SHA256(ключ узла, версия ‖ время ‖ file_token). Ключ создаёт дом, хранит в MySQL (текущий и предыдущий) и передаёт узлу по каналу управления при каждом его новом запуске; на диск узла он не пишется.
- `upload.reuploadCdnFile` дома: неизвестный `file_token` → `FILE_TOKEN_INVALID`; подпись или срок (10 минут) не сходятся → `REQUEST_TOKEN_INVALID`; иначе копия на этом узле помечается отсутствующей, ставится в очередь первой, и обработчик ждёт её готовности до `CDN_REUPLOAD_WAIT` (10 с). Готово — хэши; нет — `FILE_TOKEN_INVALID`, и клиент докачивает из дома (все три клиента так делают, см. «Клиенты»). `CDN_UPLOAD_TIMEOUT` (500) не используем: Android на прочих ошибках отказывает загрузку, tdesktop тоже, а 500 tgnet повторяет.
- `upload.getCdnFile` на DC 1 (дом) — `FILE_TOKEN_INVALID` (клиент вернётся к `getFile`); `getCdnFileHashes`/`reuploadCdnFile` на узле — `CDN_METHOD_INVALID`.
- Пока копия не готова, `upload.getFile` редиректа не даёт, поэтому цикла «редирект → reupload → назад на мастер → редирект» нет.

### 5. Узел `cdn` (`app/service/cdn`, свой образ, как SFU)
- **Транспорт:** abridged, intermediate, padded intermediate, obfuscated2 — тот же код, что у gnetway, вынесенный в `pkg/mtserver/codec`, плюс obfuscated2 с секретом `0xdd`+16 байт (ключи `SHA256(key ‖ secret)`, как у MTProxy).
- **Рукопожатие:** `req_pq_multi`, `req_DH_params` с `p_q_inner_data_dc` (Android, iOS — постоянный ключ) и `p_q_inner_data_temp_dc` (tdesktop — временный непривязанный), `set_client_DH_params`. Ядро вынесено из gnetway в `pkg/mtserver/handshake`; сохранение ключа — интерфейс (gnetway — session, узел — память). Ключи клиентов живут только в памяти узла с ограничением числа и сроком; после перезапуска — −404, клиенты создают новые.
- **Зашифрованный слой (минимальная сессия):** MTProto 2.0, msg_id/seqno, соли, контейнеры, `gzip_packed`, `msgs_ack`, `ping`/`ping_delay_disconnect`, `get_future_salts`, `destroy_session`, `bad_server_salt`, `bad_msg_notification`, `new_session_created`. Методы: `invokeWithLayer`, `initConnection` (поля не пишутся в лог и не хранятся), `upload.getCdnFile`. Остальное — `rpc_error 400 CDN_METHOD_INVALID`. Обновлений узел не шлёт.
- **`upload.getCdnFile`:** проверки диапазона как у `getFile`; байты `[offset, min(offset+limit, size))`; `offset ≥ size` — пустые байты (конец файла у всех клиентов).
- **Хранилище:** каталог на диске, файл на токен, временный файл → `fsync` → `rename`; индекс в памяти, восстанавливается сканированием при запуске (недописанные удаляются). LRU с потолком `CDN_CACHE_BYTES` и сроком простоя `CDN_IDLE_TTL` (30 дней): шифр удалённых дома файлов не живёт на VPS вечно.
- **RSA-ключ:** 2048 бит, создаётся на узле при первом запуске в его томе (0600), наружу — только публичная часть через канал управления.
- **Канал управления дом → узел:** gRPC с взаимным TLS на адресе туннеля (`10.77.0.1:20510`), как управление SFU (ADR 0059, 0061): `Status` (отпечаток и публичный ключ, `boot_id`, занято/потолок, байты за месяц), `SetRequestTokenKeys`, `Put` (потоком: токен, размер, куски шифра; ответ — SHA-256 всего шифра и размер), `Delete`, `Inventory`. Метрики — `10.77.0.1:20511`. Наружу узел слушает только порт MTProto.
- **Адрес:** адрес Pluma 195.201.252.126, свой порт (по умолчанию 8443, не из списка пересылки домой `forward.sh`), в `dc_options` — с секретом `dd…`. Секрет нужен, чтобы Android не пробовал на этом адресе порты 443 и 5222 (они уходят домой, и рукопожатие с ключом дома зациклило бы загрузку CDN-ключей); секрет публичный, это не ключ.

### 6. Дом (`pkg/cdn` в процессе bff, сервис files)
- **Таблицы** (`migrate-20261120-cdn.sql`): `cdn_files` (файл: вид, id, thumb, размер, токен, ключ, IV, хэши), `cdn_replicas` (копия файла на узле: состояние, аренда, попытки), `cdn_nodes` (узел: номер DC, ключ, отпечаток, `healthy_until`, байты за месяц, ключи `request_token`). Несколько узлов позже — только конфигурация.
- **Заливщик:** берёт копии в очереди по аренде в MySQL, читает файл частями по 1 МБ тем же путём, что `upload.getFile` (dfs), сверяет итоговый размер с размером из media/secretchat (dfs при ошибке чтения отдаёт пустые байты — без сверки на узел ушёл бы обрезанный файл), считает хэши открытого текста, шифрует и шлёт `Put` с ограничением скорости (`CDN_PUSH_RATE_MBIT`, по умолчанию 20 Мбит/с из ~57 отдачи дома) и одним файлом за раз. Готово, только если узел вернул тот же размер и SHA-256 шифра.
- **Проверка узла** раз в 10 с: `Status` + рукопожатие `req_pq_multi` на публичном адресе (сверка отпечатка с ключом из `Status`). Здоров — `healthy_until = now + 30 s`; бюджет трафика за месяц исчерпан (`CDN_MONTHLY_EGRESS_LIMIT_GB`) или нет места — нездоров. Новый `boot_id` — повторно `SetRequestTokenKeys`. Раз в 10 минут `Inventory`: копии, которых на узле нет, помечаются отсутствующими.
- **Редирект** в `upload.getFile`: после всех проверок доступа, до чтения из dfs; кэш состояний файлов в памяти (60 с) поверх MySQL, чтобы не ходить в базу на каждый мегабайт.

### 7. `help.getConfig` `dc_options`
- Пустой список больше не отдаётся, когда CDN включён. Список собирается из конфигурации, не из `config.json`:
  - DC 1: `195.201.252.126:<порт>` для каждого порта шлюза (10443 и 5222; вшитый в сборки порт — первым), флаг `static` (как у вшитого адреса tdesktop, D-002), без `ipv6`: шлюз по IPv6 не опубликован, так что вшитый IPv6, если он есть, и сегодня не работает;
  - DC 201: адрес узла, флаги `cdn | tcpo_only`, `secret`.
- Без DC 1 сервис не стартует (проверка конфигурации): tdesktop заменил бы весь список и потерял сервер.
- С этого момента клиенты берут адрес DC 1 из конфигурации. Чтобы снять CDN, достаточно убрать DC 201 из списка, DC 1 оставить: tdesktop заменит список, Android и iOS оставят запись DC 201, но без редиректов туда не пойдут.
- Номер 201 — наш выбор (номера CDN Telegram не проверены), ограничение tdesktop — меньше 1000.

### 8. Клиенты
- Android, tdesktop — без изменений.
- iOS — патч I-116 в pluma-ios: флаг `cdn_supported` при `!cdnDisabled` (`FetchV2.swift:974`) и все ветки CDN (`:635`, `:647`, `:952-956`, `:1087`, `:1092`, `:1158`) по логике `fastDownloads` независимо от движка; размеры окна и частей (`:512-513`, `:693`, `:1045`) остаются за движком.
- Web K — без CDN.

## Последствия
- Отдача дома на большое видео в большой группе падает с N копий до одной на узел (плюс часть первой загрузки). Для веба и до выхода iOS-сборки с патчем всё как сегодня.
- На VPS появляется сервер MTProto (с завершением транспортного шифрования, но без ключей файлов) и хранилище шифра — правка ADR 0057 §2 в части «завершение MTProto на VPS запрещено»: запрет остаётся для ядра; узел CDN по протоколу не видит открытого содержимого.
- Клиенты начинают следовать `dc_options`: ошибка в списке DC 1 отрезает tdesktop до переустановки. Поэтому — проверка при старте и тест.
- tdesktop один раз переподключится к DC 1 при первом списке (эндпоинты меняются, `mtproto_dc_options.cpp:229-239`).
- Трафик узла считается по тарифу VPS. Сколько входит в пониженный тариф, оператор проверяет в консоли Hetzner; сервер следит за месячным бюджетом сам.
- Ключ узла можно менять: клиенты сами перезапрашивают `help.getCdnConfig` при несовпадении отпечатка (Android `Handshake.cpp:404-407`, tdesktop `session_private.cpp:2582-2587`, iOS `MTDatacenterAuthMessageService.m:552-562`).

## Что не делается
- CDN для Web K (WebSocket-эндпоинт на узле и порт кода CDN в клиент) — отдельная задача, если понадобится.
- `upload.getFileHashes` — остаётся `ErrEnterpriseIsBlocked` (его не зовёт ни один клиент).
- Политика «только популярное» Telegram — у нас порог по размеру.
- Второй узел у другого провайдера (для России) — после замеров задачи 092; схема таблиц к этому готова.
- Хранение шифра на узле дольше `CDN_IDLE_TTL` и предзагрузка до первого запроса.

## Рассмотренные альтернативы
- **Отдельный IPv4 для узла** (плавающий адрес Hetzner) с портами 443 и 5222 без секрета. Чище по протоколу и годится для 443, но стоит денег (цена не проверена) и ещё один адрес на VPS. Остаётся вариантом, если владелец предпочтёт.
- **Синхронная заливка в `reuploadCdnFile` до конца.** Файл 2 ГБ при 57 Мбит/с — минуты; вызов истёк бы, Android повторял бы reupload по кругу.
- **Редирект сразу, заливка по требованию.** Каждая первая загрузка шла бы через `cdnFileReuploadNeeded`, а iOS без патча ветки fast отказывал бы загрузку.
- **Заливщик в dfs.** У dfs прямой доступ к Garage, но нет MySQL, и пришлось бы менять его TL; bff уже читает файлы из dfs частями по 1 МБ и знает MySQL.
- **Узел без секрета на адресе Pluma.** Android пробовал бы 443 и 5222, которые уходят домой (`forward.sh`).

## Решения к подтверждению владельцем
- Секрет `dd…` на адресе Pluma, порт 8443, против отдельного IPv4.
- `CDN_MONTHLY_EGRESS_LIMIT_GB` и `CDN_CACHE_BYTES` под пониженный тариф VPS.
- Первый зритель частично качает из дома; файлы секретных чатов и фото ≥ 1 МБ тоже идут через CDN.
