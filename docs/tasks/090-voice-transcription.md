# 090. Расшифровка голосовых и кружков

- Статус: done (2026-11-12). Сервер — ветка `task/090-voice-transcription`; клиенты — кнопка возвращена в ветках форков вместе с меткой «служебный аккаунт» (A-109, D-084/D-095, W-068/W-076, I-109/I-121 в `clients/patches/*.md`); выкладка: сервер и `stt` раньше клиентов.
- Зависит от: 071 (Premium для всех — кнопку скрывали его патчи), 027 (внешние сервисы: пункт «распознавание речи» закрывается здесь).
- Решение: ADR 0056.
- Решение владельца (2026-11-12): делаем на GigaAM-v3; модель должна меняться легко.
- Решения владельца (2026-11-12, окончательные): (1) пределов для пользователей нет — ни длины записи, ни числа расшифровок, тексты бессрочно; защищается только движок; час записи проходит целиком; (2) одноразовые голосовые и кружки не расшифровываются, и клиенты кнопку у них не показывают; (3) следующая сборка клиентов несёт и метку «служебный аккаунт», и возвращённую кнопку.

## Цель
Кнопка «→A» у голосового сообщения и кружка работает во всех четырёх клиентах так же, как у Telegram: текст появляется сразу или после ожидания, повторный запрос мгновенный, сбой выглядит как сбой. Распознаёт локальная модель в отдельном контейнере; модель и движок меняются конфигурацией и образом, без правки сервера.

## Область
В задаче:
- `messages.transcribeAudio`, `messages.rateTranscribedAudio`, `updateTranscribedAudio` — личные чаты, группы, каналы и супергруппы; голосовые и кружки.
- Очередь, кэш по документу, защита движка (без пределов для пользователей), повторы, поведение при перезапуске — как в ADR 0056, разделы 5–8.
- Сервис `pluma-stt` с GigaAM-v3 `e2e_rnnt` int8, оверлей compose, образ в CI, метрики, правила оповещений, документ оператора.
- Поддельный движок для e2e и общий набор проверок протокола для настоящих движков.
- Снятие патчей клиентов, скрывающих кнопку (в форках, вне этой рабочей копии), и правка их описаний в `clients/patches/*.md` — делается отдельно, в работе над клиентами (см. «Результат»).

Не в задаче (ADR 0056, «Что не делается»): `messages.transcribeAudioFile`, `messages.getTranscriptionLangs`, частичный текст, пробные расшифровки и `FLOOD_WAIT`, секретные чаты, отложенные и одноразовые сообщения, музыка и видео, GPU, перераспознавание после смены модели.

## Контракт клиентов
Источники — `~/pluma-work/reference-src-new` (Android 7be672ea, tdesktop v7.4.1, iOS release-13.2, Web K aa74832), core.telegram.org/api/transcribe и core.telegram.org/method/messages.transcribeAudio. iOS закреплён на `release-12.9.2` (`wt-svc-ios/upstream.env`): те же файлы перечитать в `~/pluma-work/reference-src` и записать расхождения в «Результат».

1. **Что расшифровывается.** Голосовое или кружок обычного сообщения, которое вызывающий может прочитать. Кнопку показывают: Android `ui/Cells/ChatMessageCell.java:11143-11173`; tdesktop `history/view/media/history_view_document.cpp:501-510`, `history_view_gif.cpp:2730-2747`; iOS `ChatMessageInteractiveFileNode.swift:787-793`, `ChatMessageInteractiveInstantVideoNode.swift:873-876`; Web `components/wrappers/document.ts:153`, `components/chat/bubbleParts/roundVideoBubble.ts:207-229`.
2. **Ошибки.** `PEER_ID_INVALID`, `MSG_ID_INVALID`, `MSG_VOICE_MISSING`, `MSG_VOICE_TOO_LONG`, `TRANSCRIPTION_FAILED` (страница метода). tdesktop и iOS отличают только `MSG_VOICE_TOO_LONG` (`api/api_transcribes.cpp:218-226`, `Transcription.swift:30-45`); Web превращает `TRANSCRIPTION_FAILED` и `MSG_VOICE_MISSING` в пустой окончательный текст (`appMessagesManager.ts:3175-3182`); Android любую ошибку, кроме `FLOOD_WAIT_`, сохраняет как окончательный пустой текст и больше не спрашивает (`TranscribeButton.java:679-685, 741-754`).
3. **Запрещено:** `FLOOD_WAIT`, `PREMIUM_ACCOUNT_REQUIRED`, `flags.1` (`trial_remains_*`). `FLOOD_WAIT` и `trial_remains_until_date` блокируют кнопку на iOS (`Transcription.swift:32-36, 59-62, 71-77`; `ChatMessageInteractiveFileNode.swift:821-823`), на Android обнуляют пробный счётчик (`TranscribeButton.java:729-741`).
4. **`transcription_id`.** Ненулевой, свой у каждой пары «пользователь + сообщение», одинаковый в ответе, обновлении и оценке. tdesktop хранит `_ids.emplace(transcription_id, fullId)` и находит сообщение обновления только по id (`api_transcribes.cpp:152-171, 214`).
5. **`pending`.** В ответе — `pending=true` с пустым текстом, если не успели; затем ровно одно `updateTranscribedAudio` с `pending=false`, в том числе при провале (с пустым текстом). Обновления `pending` с пустым текстом не шлются: Android их пропускает (`MessagesController.java:20806`). Без окончательного обновления tdesktop и кружок в Web ждут бесконечно (`appMessagesManager.ts:3156-3192`).
6. **Порядок.** Окончательное обновление не должно прийти раньше ответа: tdesktop регистрирует id только из ответа и выбрасывает обновление с неизвестным id.
7. **Peer в обновлении** — диалог, каким его видит вызывающий, и msg_id его копии: iOS и Web сопоставляют по ним (`UpdateInterpreter+Messages.swift:223-226`, `StateReplay+Messages.swift:455-471`, `appMessagesManager.ts:13053-13077`), Android пишет по ним в хранилище.
8. **Текст.** Обрезан по краям; пустой окончательный — «слова не распознаны» на Android (`MessageObject.java:7775-7780`), ошибка на iOS (`ChatMessageItemCommon.swift:211-226`) и в Web (`components/audioTranscription.tsx:42-54`), пусто на tdesktop.
9. **Повторы.** tdesktop и Web держат результат в памяти и спрашивают снова после перезапуска; iOS спрашивает снова при ошибке и незавершённом результате (`ChatMessageInteractiveFileNode.swift:419-430`). Повтор должен быть дешёвым и давать тот же id.
10. **Оценка.** `messages.rateTranscribedAudio` — проверить, сохранить, `boolTrue`; ответ клиенты не разбирают (`api_transcribes.cpp:42-56`, `ChatActivity.java:32440, 32552-32556`, `Transcription.swift:100-143`, `appMessagesManager.ts:3197-3215`).
11. **appConfig** не меняется; ключ `transcribe_audio_trial_cooldown_until` не добавляется.

Не проверено, выяснить по ходу и записать в «Результат»:
- что Android отправляет из секретного чата и для отложенного сообщения (в условии кнопки исключений нет) — сервер должен ответить `PEER_ID_INVALID` или `MSG_ID_INVALID`;
- в каких форматах загружают голосовые и кружки iOS, tdesktop и Web (у Android `audio/ogg` — `MediaController.java:5010,5328`, `video/mp4` — `SendMessagesHelper.java:6296`);
- проходит ли `updateTranscribedAudio` через `pkg/layercompat` на слое 228 (по схеме конструктор есть);
- проверяет ли `ChannelGetMessages` доступ вызывающего к закрытому каналу; если нет — проверить доступ в обработчике;
- лицензия Silero VAD;
- совместимость разделяемых библиотек sherpa-onnx с glibc базового образа — проверить первой, до остального кода.

## Порядок работ
1. **Движок на CachyOS.** Собрать `pluma-stt` (cgo, sherpa-onnx-go, glibc), прогнать на тестовой записи из архива модели, снять время и память. Если sherpa-onnx-go не собирается в образе — остановиться и доложить.
2. **`pkg/transcribe`:** интерфейс `Engine`, клиент HTTP, декодер, хранилище, метрики, конфигурация; миграция.
3. **`app/bff/transcription`:** два обработчика, исполнитель и подметальщик; регистрация и маршруты.
4. **Поддельный движок и e2e.**
5. **Compose, CI образов, Prometheus, оповещения, `.env.example`, документы оператора.**
6. **Набор проверок протокола на трёх целях** (раздел «Тесты», пункт 4).
7. **Клиенты:** снять патчи A-109, I-109, D-084, W-068 и тесты их отсутствия, обновить `clients/patches/*.md`.
8. Реестр функций, `TESTING-BACKLOG`, `apicoverage`, STATUS (в основной сессии), «Результат» брифа.

## Файлы
Новые:
- `pkg/transcribe/{engine.go,http_engine.go,decode.go,store.go,metrics.go,config.go}` и тесты; `pkg/transcribe/enginetest/` — набор проверок протокола для любого движка по URL.
- `app/bff/transcription/` по образцу `app/bff/premium`: `helper.go`, `internal/{config,core,dao,server,svc}`; в `core` — `messages.transcribeAudio_handler.go`, `messages.rateTranscribedAudio_handler.go`, `worker.go`.
- `app/ops/stt/` — сервис распознавания (HTTP, загрузка модели по `model.yaml`, VAD, семафор, сторожевой таймер, `/healthz`, `/metrics`, подкоманда `healthcheck`).
- `teamgramd/deploy/stt/Dockerfile` — сборка с закреплённым архивом модели и SHA-256, аргументы `MODEL_URL`, `MODEL_SHA256`, `MODEL_KIND`, `MODEL_NAME`.
- `docker-compose.stt.yaml`.
- `teamgramd/deploy/sql/migrate-20261115-voice-transcription.sql` — `transcription_jobs`, `transcription_requests`, `CREATE TABLE IF NOT EXISTS`, с пояснением, почему повторный запуск безопасен.
- `teamgramd/deploy/prometheus/server/rules/stt.yml`.
- `test/e2e/fakestt/{main.go,Dockerfile}`, `test/e2e/transcription_test.go`.
- `docs/ops/transcription.md` — включение, переменные, замена модели и движка, память, оповещения, что делать при сбое.

Изменяемые:
- `app/bff/bff/internal/server/server.go` — регистрация `RPCTranscriptionServer`, запуск и остановка исполнителя (как `calls` у звонков).
- `teamgramd/etc/session.yaml`, `teamgramd/etc2/session.yaml`, `app/interface/session/etc/session.yaml` — `"/mtproto.RPCTranscription": "bff.bff"`.
- `teamgramd/etc/bff.yaml`, `teamgramd/etc2/bff.yaml` — блок `Transcribe`.
- `docker-compose.yaml` (проброс `STT_*` как `${NAME:-}`), `.env.example`, `pkg/conf/compose_services_test.go` (`optionalVars`, список оверлеев при необходимости).
- `teamgramd/deploy/prometheus/server/prometheus.yml` (цель `stt` через `dns_sd_configs`).
- `.github/workflows/images.yml` — `pluma-stt`.
- `test/e2e/run.sh`, `test/e2e/docker-compose.e2e.yaml` — поддельный движок.
- `docs/ops/{memory,app-config,deployment}.md`, `docs/project/feature-registry.md` (и `.json`), `docs/project/schema-baseline.txt`, `docs/project/TESTING-BACKLOG.md`, `docs/project/api-coverage.md` (через `go run ./tools/apicoverage`), `clients/patches/{android,ios,desktop,web}.md`.

Вне рабочей копии:
- `~/pluma-work/bin/pluma-deploy.sh` — тянуть `pluma-stt`, подключать `-f docker-compose.stt.yaml`, добавить `stt` в `EXTRA`.
- Форки клиентов `~/pluma-work/wt-svc-{android,ios,desktop,web}/ci/patch.py` — снять скрытие кнопки и тесты `test_no_star_store_or_transcription_on_new_surfaces` в части расшифровки.

## Тесты
1. **Модульные.**
   - Клиент HTTP на `httptest`: поля multipart, заголовок, разбор 200; 400/415/422 → `ErrRejected`; 401/404/429/5xx/обрыв/тайм-аут → `ErrUnavailable`; тайм-аут от длительности.
   - Декодер: аргументы ffmpeg, ограничение `-t`, отмена по контексту.
   - Машина состояний задания и расписание повторов; выбор ошибок обработчика (каждая из пяти); отказ при пустом URL; peer обновления для личного чата, группы, канала и «Избранного».
   - `pkg/layercompat`: `updateTranscribedAudio` и `messages.transcribedAudio` проходят на слоях 228 и 233.
   - `app/ops/stt`: разбор `model.yaml`, 404 на чужое имя модели, 503 при занятом семафоре, 400 на не-WAV.
2. **Интеграционные (`pkg/testinfra`, MySQL).**
   - Два одновременных запроса одного документа — одно задание, два запроса с разными id.
   - Одновременный захват двумя исполнителями — задание достаётся одному.
   - Истёкшая аренда — задание берётся снова, ожидающий получает обновление ровно один раз.
   - Повторный запрос того же пользователя и сообщения — тот же id.
   - Пределов нет: сотни записей одного пользователя по четыре часа встают в очередь и берутся по порядку; правка медиа сообщения не портит кэш и id.
   - Проваленное задание: `permanent` не перезапускается, обычное — после `FailedTTL`.
   - Смена имени модели — новое задание, старое не отдаётся.
   - Запись обновления в outbox и пометка `sent` — в одной транзакции.
3. **e2e (`pkg/mtclient`, поддельный движок `test/e2e/fakestt`).** Поддельный движок отдаёт настоящий `POST /v1/audio/transcriptions`, проверяет, что тело — RIFF 16 кГц моно, и управляется через `/e2e/*`: текст, задержка, N ошибок подряд, зависание, счётчик вызовов.
   - Быстрый ответ: `pending=false`, текст, ненулевой id.
   - Медленный: `pending=true` с пустым текстом, затем одно `updateTranscribedAudio` с тем же id и правильным peer, после ответа; сессия получает его только после `updates.getState`.
   - Два пользователя и одно голосовое — один вызов движка, разные id.
   - Пересланное голосовое — движок не вызывается, id другой.
   - Кружок (MP4) и голосовое (OGG/Opus) — оба доходят до движка как WAV.
   - Личный чат, группа, супергруппа, канал.
   - Ошибка движка — повторы, затем окончательное пустое обновление; ошибка в пределах ожидания — `TRANSCRIPTION_FAILED`.
   - `MSG_VOICE_MISSING` (текст, фото, музыка), `MSG_ID_INVALID`, `PEER_ID_INVALID` (чужой канал), `MSG_VOICE_TOO_LONG`.
   - Перезапуск bff посреди задания — окончательное обновление всё равно приходит.
   - Оценка: `boolTrue`; чужой id — ошибка.
   - Ответ не содержит `flags.1`; `FLOOD_WAIT` не встречается ни в одном сценарии.
4. **Настоящая модель на CachyOS** (вне e2e, результаты — в «Результат»). Набор `pkg/transcribe/enginetest` запускается по URL против трёх целей; между ними меняются только URL и имя модели:
   - `pluma-stt` с GigaAM-v3 `e2e_rnnt` — основная;
   - `pluma-stt` с GigaAM-v3 `e2e_ctc` (другой `model.yaml` и `kind`) — замена модели;
   - сторонний сервер с тем же протоколом (speaches с малой моделью Whisper) — замена движка. Если образ не помещается на диск — записать это и проверить на любом другом из ADR 0056.

   Плюс полный путь на стенде с настоящим `pluma-stt`: голосовое и кружок, записанные живым голосом на русском (короткое, около минуты, длиннее 25 с с паузами, тишина, шум, смесь с английским). Записать: текст глазами, время на минуту звука при `--cpus=2`, пик памяти контейнера при лимите 768 МБ, память bff во время декодирования записи на 10 минут.

Хотя бы один тест проверить на провал без изменения (например, e2e быстрого ответа при снятом маршруте).

## Проверки перед сдачей (CachyOS)
- `go build ./...`;
- `go vet -tags integration,e2e -unreachable=false` затронутых пакетов;
- `go test` затронутых пакетов и импортирующих их, `-race` по ним;
- `make lint` (в настоящем клоне на CachyOS);
- `go test ./tools/...` (включая `logaudit`: текст расшифровки не логируется нигде);
- `pkg/conf` — `TestComposeMemory` и тест сервисов compose с новым оверлеем;
- интеграционные тесты `pkg/transcribe` и `app/bff/transcription`; e2e `transcription_test.go` и соседние (`channel_media_unread_test.go`, тест необслуживаемых методов) со своими `E2E_PROJECT=stt`, `E2E_DIR=$HOME/pluma-work/e2e-stt`, один стенд за раз;
- `go run ./tools/apicoverage`;
- сборка образа `pluma-stt` и его удаление после прогона; `test/e2e/run.sh down`.

## Проверки для `docs/project/TESTING-BACKLOG.md`
Раздел **Расшифровка голосовых (задача 090, ADR 0056)**: проверки по каждому клиенту — короткое голосовое и кружок, голосовое на 30 минут целиком, одноразовые голосовое и кружок без кнопки, `stt` остановлен и запущен снова, пересылка расшифрованного голосового, оценка; общие — второй аккаунт, все виды чатов, два устройства, смена модели, английская речь, шум, память `stt` и bff на длинной записи.

## Решения, требующие подтверждения владельцем
Подтверждены владельцем 2026-11-12: пределов для пользователей нет; тексты бессрочно и не удаляются с сообщением; одноразовые не расшифровываются; кнопка возвращается в одной сборке клиентов с меткой «служебный аккаунт».

Остаются (ADR 0056, последний раздел): память и CPU движка на общем сервере (768 МБ, 2 CPU); `TRANSCRIPTION_FAILED` для записи, которая сама не распознаётся; обновление всем сессиям пользователя; только русский язык у GigaAM (замена — Parakeet TDT v3, замерен).

Дополнительно по выкладке:
- сервер и `pluma-stt` выкладываются раньше клиентов с возвращённой кнопкой;
- `STT_TOKEN` — нужен ли он, если движок доступен только во внутренней сети compose (по умолчанию задаём).
- текст длиннее 1 МиБ (около 11 часов речи) — окончательный отказ, потому что Android не принимает пакет больше 2 МиБ; альтернатива — отдавать начало текста, но это выдало бы часть за целое;
- запас свободного места для копии записи — 1 ГиБ сверх её размера (константа `diskReserve`).

## Замеры модели (2026-11-12, до начала кода)
CachyOS, Ryzen 5 5600, `docker run --cpus=2 --memory=2g`, sherpa-onnx 2.0.1 (пакет Python), модели int8 из релиза `asr-models`, жадный поиск; запись — русский образец из архива модели (11,29 с) и он же шесть раз с паузами (71,3 с, 12 кусков Silero VAD).

| Вариант | Потоки | Загрузка | 11,3 с | 25 с одним проходом | Секунд на минуту звука | Пик RSS |
|---|---|---|---|---|---|---|
| e2e_rnnt | 2 | 0,79 с | 0,56 с | 1,35 с | 3,25 | 410 МБ |
| e2e_ctc | 2 | 0,83 с | 0,56 с | 1,32 с | 2,73 | 407 МБ |
| e2e_rnnt | 1 | 0,79 с | 0,96 с | 2,23 с | 4,65 | 408 МБ |
| e2e_ctc | 1 | 0,73 с | 0,93 с | 2,20 с | 4,48 | 409 МБ |

Образец — чистая начитанная речь; качество на живых голосовых не измерено (пункт 4 тестов).

## Результат
Сервер расшифровывает голосовые и кружки во всех диалогах: `messages.transcribeAudio` и `messages.rateTranscribedAudio` обслуживает область `app/bff/transcription` (регистрация в `server.go`, маршрут `/mtproto.RPCTranscription` в трёх `session.yaml`), очередь и кэш — таблицы `transcription_jobs` и `transcription_requests`, движок — сервис `pluma-stt` (`app/ops/stt`, GigaAM-v3 `e2e_rnnt` int8 на sherpa-onnx, модель запечена в образ с SHA-256) за протоколом OpenAI. По решениям владельца 2026-11-12 пределов для пользователей нет: запись любой длины (`MSG_VOICE_TOO_LONG` не отвечается), любое число расшифровок, очередь без предела и по порядку, тексты бессрочно; защищён только движок — одна запись на слот, сроки по декодированной длине, ожидание остановленного или неверно настроенного движка без потери записей, терпеливые повторы. Час речи проходит целиком: `stt` читает форму потоком и распознаёт кусками (память 393–435 МБ на записях от 90 с до 60 минут), bff держит на диске только копию документа и декодирует звук прямо в запрос к движку. Одноразовые голосовые и кружки сервер не расшифровывает (`MSG_VOICE_MISSING`, e2e). Не сделано в этой рабочей копии: кнопка в клиентах (снятие патчей A-109, I-109, D-084, W-068) и метка «служебный аккаунт» — работа в форках, в одной сборке клиентов; `clients/patches/*.md` здесь не правились.

Находки ревью и что с ними:
- Временный отказ движка 10 минут отвечался `TRANSCRIPTION_FAILED` — исправлено раньше (повторная постановка, ответ `pending`), теперь сильнее: движок «недоступен» (нет соединения, 401/403/404) и «занят» (503/429) — ожидание без предела и без траты попыток. `TestEngineDownIsWaitedFor`, e2e `TestTranscribeEngineFailures` (три 404 подряд на стенде с `MaxAttempts: 2` — текст приходит).
- Читатели заданий после смены `STT_MODEL` получали пустое обновление — исправлено (`Store.Retire`), `TestRetireHandsTheReadersOver`, `TestJobOfAnotherModelIsLeftAlone`.
- Обход часового лимита правкой медиа — снято решением владельца (лимита нет; таблица `transcription_charges` удалена). Проверено, что правка медиа не портит кэш и id: `TestEditedMediaKeepsTheIdAndTheTexts`.
- Срок и аренда от объявленной длительности — исправлено: подготовка — 90 с + 4 с/МБ от размера, известного серверу; движок — `STT_TIMEOUT` от декодированной длины. `TestEngineTimeIsOfTheDecodedSound`, `TestPreparationGrowsWithTheDocument`, e2e `TestTranscribeLongRecord` (30 минут, объявлена минута).
- Повтор на «занят» заново скачивал и декодировал запись; при `Workers>1` старые задания проваливались — исправлено: звук держится, «занят» не попытка и без предела терпения; кроме того, звук уходит только после `100 Continue`, поэтому 503 на длинной записи не превращается в оборванную запись тела. `TestBusyEngineIsNotAnAttempt`, `TestBusyAfterALongQueueIsNotAFailure`, `TestBusyEngineIsAnsweredBeforeTheSound`.
- Захват без учёта модели — исправлено (`Claim` по `STT_MODEL`), `TestAnotherModelTranscribesAgain`, `TestJobOfAnotherModelIsLeftAlone`.
- Срок движка — константа кода — исправлено: `STT_TIMEOUT`, `TestHTTPEngineGivesTheConfiguredTime`.
- Нет пути на прод для другой модели — путь: коммит со значениями `ARG MODEL_*` в `Dockerfile` (CI собирает только из репозитория, сервер ничего не собирает — `docs/ops/deployment.md`); таблица проверенных наборов с памятью — `docs/ops/transcription.md`. Проверено сборкой с другими аргументами (`e2e_ctc` прошёл набор; Parakeet TDT v3 собран и замерен).
- Память 768m зашита в файле — исправлено: `STT_MEMORY`, `STT_CPUS`, `STT_THREADS` из `.env` с документированными значениями; Parakeet TDT v3 при 768m убит OOM при загрузке (подтверждено), нужно `2g` (пик 1,27 ГБ).
- Скрытый предел 900 с у `stt` — снято: предела нет, форма читается потоком, сторожевой таймер — на кусок речи, общего срока запроса нет (простой чтения — 408). `TestALongRecordIsStreamed` (30 минут, выделено меньше 16 МБ), `TestACallerThatStopsSending`, `TestFieldsAfterTheSound`; замер на настоящей модели — ниже.
- `STT_LANGUAGE` игнорировался нашим `stt` — исправлено: переменная передаётся и сервису; Whisper получает её вместо языка образа, модель одного языка с другим языком не запускается. `TestSpeak`.

Новые тесты этого шага проверены на старом поведении в копии рабочего дерева: ожидание «недоступного» движка как попытка — `TestEngineDownIsWaitedFor` падает; без `Expect: 100-continue` — `TestBusyEngineIsAnsweredBeforeTheSound` падает; декодер с `-t 600` — `TestDecodeTakesTheWholeRecord` падает (декодировано 10 минут из 20); `stt`, держащий форму в памяти, — `TestALongRecordIsStreamed` падает (320 МБ на запись в 54 МБ).

Не проверено → проверено:
- iOS `release-12.9.2` (`~/pluma-work/reference-src/ios`, коммит 6ad963e5): `Transcription.swift` отличается от 13.2 только `internal import TelegramApi`; обновление сопоставляется по peer и msg_id (`State/AccountStateManagementUtils.swift:1340-1343`); строки про кнопку в `ChatMessageInteractiveFileNode.swift` совпадают.
- Форматы загрузки: tdesktop — голосовое `audio/ogg`, кружок `video/mp4` (`storage/localimageloader.cpp:854-859`); iOS — голосовое `audio/ogg` (`Chat/ChatControllerMediaRecording.swift:396, 748`); Web — голосовое `audio/ogg` (`components/chat/recording/chatRecording.ts:252`), контейнер кружка Web в источниках не найден (ffmpeg читает и MP4, и WebM). Сервер декодирует любой формат ffmpeg’ом; e2e шлёт Ogg/Opus и MP4.
- `pkg/layercompat`: `updateTranscribedAudio` и `messages.transcribedAudio` проходят на слоях 228 и 233 (`pkg/layercompat/transcription_test.go`).
- `ChannelGetMessages` проверяет доступ: `viewerOf` и `CHANNEL_PRIVATE` (`app/service/biz/channel/internal/dao/history.go:279-288`); e2e — чужой закрытый канал даёт `PEER_ID_INVALID`.
- Лицензия Silero VAD — MIT (LICENSE репозитория snakers4/silero-vad), записано в ADR.
- glibc и sherpa-onnx: образ собирается и работает (ubuntu 26.04, библиотеки из модуля `sherpa-onnx-go-linux`).
- Не проверено: что Android шлёт для секретного чата и отложенного сообщения (сервер ответит `PEER_ID_INVALID` или `MSG_ID_INVALID` — в чек-листе владельца); память bff на длинной записи прямо не измерялась — e2e с записью на 30 минут проходит под лимитом 384 МБ без перезапуска bff.

Замер длинной записи (2026-11-12, CachyOS, Ryzen 5 5600, образ из `teamgramd/deploy/stt/Dockerfile`, контейнер с пределами `docker-compose.stt.yaml`: `--memory=768m --memory-swap=768m --cpus=2 --read-only`; образец из архива модели по кругу с паузами 1,5 с, как голосовое Ogg/Opus, путь сервера ffmpeg → WAV → `POST /v1/audio/transcriptions`, `.e2e/long-run.sh`):

| Запись | Время | На минуту звука | Пик памяти | Полнота текста |
|---|---|---|---|---|
| 25,2 мин (1509 с) | 83 с; повтор на окончательном коде — 66 с | 3,3 / 2,6 с | 415 / 395 МБ | ключевое слово 118 из 118, 19 114 символов (оба прогона) |
| 60,1 мин (3607 с) | 177 с | 2,9 с | 393 МБ | 282 из 282, 45 682 символа |

Память после загрузки — 337 МБ; OOM и перезапусков нет. Parakeet TDT 0.6b v3 int8: при 768m убит OOM при загрузке; при 2g — 994 МБ после загрузки, пик 1,27 ГБ, 4,0–4,2 с на минуту звука, образ 1,35 ГБ; проверка слов набора не прошла (ключевое слово в 2 повторах из 6 — другое написание или пропуск, текст не сохранён, не выяснено).

Замер настоящей модели (2026-11-12, CachyOS, Ryzen 5 5600, `docker run --cpus=2 --memory=768m --memory-swap=768m --read-only`, образ из `teamgramd/deploy/stt/Dockerfile`; образец — русская речь из архива модели, 11,3 с, превращённая в голосовое Ogg/Opus 48 кГц и в кружок H.264+AAC, и запись 89,5 с из шести повторов с паузами; путь как у сервера: ffmpeg → WAV 16 кГц → `POST /v1/audio/transcriptions`, набор `pkg/transcribe/enginetest`):

| Цель | Образ, сборка | Загрузка | Память после загрузки / пик | 11,3 с звука | Секунд на минуту (11 с / 90 с) | Набор |
|---|---|---|---|---|---|---|
| `pluma-stt`, GigaAM-v3 `e2e_rnnt` | 612 МБ, 35 с | 0,7 с | 340 / 426 МБ | 0,57 с | 3,05 / 2,64 | пройден (голосовое и кружок) |
| `pluma-stt`, GigaAM-v3 `e2e_ctc` (смена модели аргументами сборки) | 598 МБ, 51 с | 0,7 с | 335 / 450 МБ | 0,49 с | 2,63 / 2,26 | пройден |
| speaches 0.9.0-rc.3-cpu, faster-whisper-small (смена движка, `--memory=2g`) | 3,35 ГБ | — | 260 МБ / 1,6 ГБ | 5,9 с | 31,5 / 11,8 | протокол совместим (тишина, не-звук, чужое имя модели, два запроса сразу — без ошибок); проверка слов не прошла: модель теряет последнюю фразу записи |

Текст глазами (GigaAM `e2e_rnnt`): пушкинский отрывок распознан дословно, с пунктуацией и заглавными; у кружка и голосового текст одинаков, кроме первой буквы. Живые записи голосом (шум, смесь с английским, тишина в настоящем голосовом) не делались — они в `TESTING-BACKLOG`.

Проверки (CachyOS, 2026-11-12, все зелёные): `go build ./...`; `go vet -tags integration,e2e -unreachable=false` затронутых пакетов и `./test/e2e/`, `go vet -tags sherpa` для `app/ops/stt`; `go test -count=1 ./...`; `go test -race` затронутых пакетов и импортирующих (`pkg/outbox`, sync client, `layercompat`, bff, `pkg/conf`, `pluma-admin`); интеграционные `pkg/transcribe` и `app/bff/transcription/internal/core` с `-race`; `go test ./tools/...` (с `logaudit`); `go run ./tools/apicoverage` (файлы покрытия не изменились); `make lint` в настоящем клоне — 0 замечаний (после исправления трёх: `sqlclosecheck` в `Store.Retire`, `gosec` G706 и лишний `nolint` в `stt`; затем повтор build/vet/`-race`/интеграционных/tools); e2e `TestTranscribe*` (8) и `TestChannelMediaUnread` на своём стенде `E2E_PROJECT=stt`, стенд снят, образы удалены.

Вторая независимая вычитка (2026-11-12), находки и что с ними:
- Декодированный WAV на диске хоста без предела (32 КБ/с при любой записи, в 16–85 раз больше голосового; документ до ~4 ГБ; на полном диске ffmpeg падал «не записать», попытка повторялась и снова заполняла диск) — исправлено. WAV больше не пишется: `Decoder.Measure` прогоняет звук в трубу и считает байты (длина), `Decoder.Open` декодирует заново прямо в тело запроса к движку при каждом обращении (ffmpeg запускается на первом чтении, поэтому 503 до `100 Continue` ничего не декодирует). На диске только копия документа; перед скачиванием — проверка места (`statfs`: размер документа + 1 ГиБ), иначе ожидание 30 с без траты попытки, метрика `no_room`, алерт `TranscriptionNoDisk`. Тесты: `TestDecodeVoiceAndRoundVideo`, `TestDecodeTakesTheWholeRecord` (поток без размеров, 20 минут), `TestDecodeStreamStartsLateAndStops`, `TestRoom`, `TestTooLittleDiskIsWaitedFor`.
- Записи длиннее 37 часов: (1) RIFF-размер 32 бита — снято тем же: из трубы ffmpeg пишет `0xffffffff` в оба размера (проверено на ffmpeg n9.0.2; `-rf64` по умолчанию `never`, `ffmpeg -h muxer=wav`), `stt` и fakestt читают такие отсчёты до конца потока, длину bff считает по байтам трубы, не по заголовку. (2) Текст больше предела события — решено явно: текст длиннее 1 МиБ (около 11 часов речи) — окончательный отказ записи (ожидающие получают пустое окончательное обновление), потому что Android рвёт соединение на пакете больше 2 МиБ (`android/.../tgnet/Connection.cpp:216`), а ответ RPC сервер не сжимает (`session.go`, `sendRpcResultToQueue`; tdesktop принимает до 16 МиБ, `mtproto/session_private.cpp:53`). Тест `TestTextLongerThanAClientTakes`. Это предел протокола клиентов, не пользователя; записано в ADR 0056 и `docs/ops/transcription.md` — подтвердить владельцу.
- Статус «done» при невыполненной клиентской части цели — исправлено: статус «in progress», серверная часть готова. Проверено по источникам, что кнопка у одноразовых скрыта самими клиентами: Android `ChatMessageCell.java:11172` (`ttl_seconds == 0`), tdesktop `history_view_document.cpp:502` и `history_view_gif.cpp:2732`, iOS `ChatMessageInteractiveFileNode.swift:789` (`!isViewOnceMessage`), Web K `canSeeMessageMedia.ts` (`ttl_seconds` → `false`). e2e `TestTranscribeRefusals` теперь проверяет и копию получателя. Порядок выкладки (сервер и `stt` раньше клиентов) — в `docs/ops/transcription.md` и `TESTING-BACKLOG`; кодом не обеспечивается, это шаг выкладки.
- Неверные модель или язык: `stt` дочитывал всю запись перед 404, bff повторял каждые ≤30 с — исправлено на стороне bff: после «недоступен» движок сначала спрашивают звуком без отсчётов (`transcribe.Probe`, 44 байта), запись уходит, только когда он принят. Так для любого движка OpenAI-протокола, без изменения `stt`. Тесты: `TestEngineDownIsWaitedFor` (размеры отправленного: запись, 44, 44, запись; без пробы падает — `[64044 64044 64044]`), `TestProbe`, `TestASoundOfNoSamples` (`stt`), e2e `TestTranscribeEngineFailures` (пять запросов).

Проверки после второй вычитки (CachyOS, 2026-11-12, все зелёные): `go build ./...`; `go vet -tags integration,e2e -unreachable=false` для `pkg/transcribe/...`, `app/bff/transcription/...`, `app/bff/bff/...`, `./test/e2e/`, `go vet -tags sherpa,integration,e2e` для `app/ops/stt/...`; `go test -race` этих пакетов и импортирующих `pkg/transcribe` (`app/bff/bff/internal/server`, `config`, `enginetest`); интеграционные `pkg/transcribe` и `app/bff/transcription/internal/core` с `-race`; `go test ./tools/...` (с `logaudit`); `make lint` во временном настоящем клоне — 0 замечаний (после трёх G204 у вызовов ffmpeg — помечены `nolint` с причиной); правила оповещений разобраны как YAML (promtool на машине нет); e2e `TestTranscribe*` (8) и `TestChannelMediaUnread` на своём стенде `E2E_PROJECT=sttfix` — все прошли, стенд снят, образы, тома и сеть удалены. Обработчики не менялись — `apicoverage` не перезапускался.

Вне рабочей копии: `~/pluma-work/bin/pluma-deploy.sh` тянет `pluma-stt`, подключает `docker-compose.stt.yaml` и добавляет `stt` в `EXTRA`, когда в `.env` есть `PLUMA_STT_IMAGE=ghcr.io/…` и `STT_URL=http://stt:…`.

Не сделано / не проверено:
- Кнопка в клиентах и метка «служебный аккаунт» — в форках, отдельно.
- Качество Parakeet TDT v3 на русском не подтверждено; качество GigaAM на живых голосах (шум, смесь с английским) — в `TESTING-BACKLOG`.
- Время на общих vCPU прода не измерено (оценка: час речи — 6–13 минут).
- Запись длиннее 37 часов на настоящем движке не прогонялась (часы звука на стенде); путь без предела RIFF проверен тестами декодера (заголовок без размеров) и `wav.Reader` (`sizeUnknown` — до конца потока).
