# 068. Личные голосовые и видеозвонки (этап 3)

- Статус: in progress (сервер написан и сверен с исходниками клиентов; общий прогон e2e, -race, lint и проверка в клиентах впереди; coturn не развёрнут)
- Зависит от: 009 (push), 031 (приватность)
- ADR: 0004 «Архитектура звонков». Отдельного ADR не потребовалось: решения совместимы с ним и записаны здесь.

## Цель
Все методы `phone.*` для личного звонка четырёх клиентов в layer 228: запрос, ответ, подтверждение, отказ, оценка, отладка, сигналинг; обновления нужным устройствам; push входящего звонка; сообщение о звонке в истории; приватность, блокировки, таймауты; список соединений со STUN/TURN. Групповые звонки вне задачи.

## Что известно (источники)
- `core.telegram.org/api/end-to-end/voice-calls`: `phoneCallRequested` всем устройствам B; после `acceptCall` остальным устройствам B — `phoneCallDiscarded` («the server S works only with that of B's devices which has invoked phone.acceptCall first»); `phoneCallAccepted` A; `phoneCall` B.
- `core.telegram.org/method/phone.*`: сигнатуры и ошибки, в т.ч. `CALL_ALREADY_ACCEPTED`, `CALL_ALREADY_DECLINED`, `CALL_PEER_INVALID`, `CALL_NOT_ACTIVE`, `CALL_PROTOCOL_*`, `RANDOM_ID_DUPLICATE`, `USER_IS_BLOCKED` (403), `USER_PRIVACY_RESTRICTED`, `CALL_PROTOCOL_COMPAT_LAYER_INVALID` (406).
- `core.telegram.org/api/push-updates`: `PHONE_CALL_REQUEST`, в `custom` — `call_id`, `call_ah`, `updates` (base64url TL Updates с `updatePhoneCall`).
- Клиенты (`~/pluma-work/reference-src`):
  - Android `MessagesController.java:20340` — «ignoring too old call» (`date + callRingTimeout`); `:20390` — второй `phoneCallRequested` при живом `VoIPService` автоматически отклоняется как `Busy`, поэтому один звонок показывается устройству один раз; `PushListenerController.java:1400-1405` — `PHONE_CALL_REQUEST` не разбирается, push только будит приложение, звонок приходит с `getDifference` (`other_updates`, `:16729`); `VoIPService.java:1100-1145` и `:1796-1806` — таймеры `callReceiveTimeout`/`callRingTimeout` и `discardCall` с причиной `Missed`.
  - tdesktop `calls_call.cpp:498` — ответ на `acceptCall` обязан быть `phoneCallWaiting`; `:985` — на `confirmCall` — `phoneCall`; `:241`, `:389`, `:773` — таймеры по `call_*_timeout_ms`; `:849-857` — причина `Disconnect`/пустая.
  - iOS `CallSessionManager.swift`: `phoneCall` берёт `connections.first!` (`:1226`), `versions[0]` — первую библиотеку, отсюда сервер сужает протокол до одной общей: наибольшей в строковом порядке. Это первая версия в списках iOS и tdesktop (отсортированный реестр в обратном порядке: `OngoingCallThreadLocalContext.mm:1651`, `:558` здесь, `calls_call.cpp:195`). Порядок списка звонящего решать не может: Android шлёт реестр `std::map` без разворота, первым `10.0.0` (`tgcalls/Instance.cpp`), а `VoIPService.java:3465` строковым сравнением с `2.7.7` выключает камеру исходящего звонка на такой версии; `phoneCallDiscarded` читается из ответа `discardCall` (`:1757-1799`); `AppDelegate.swift pushRegistryImpl` и `NotificationService.swift:1053` — полезная нагрузка (`call_id`, `call_ah`, `from_id`, `updates`), `AccountStateManager.extractIncomingCallUpdate` — требует `updates` с `updatePhoneCall` и `users`.
  - tweb `callsController.ts`, `appCallsManager.ts` — те же состояния, `receivedCall` при `phoneCallRequested`.

Не подтверждено источниками (решено здесь, код на Telegram не опирается): причина в `phoneCallDiscarded` для «проигравших» устройств (шлём без причины, клиенты её не требуют); действительно ли Telegram хранит вызов в `getDifference` (см. решения).

## Область
- `phone.getCallConfig`, `requestCall`, `acceptCall`, `confirmCall`, `receivedCall`, `discardCall`, `setCallRating`, `saveCallDebug`, `sendSignalingData`, `saveCallLog`, `messages.deletePhoneCallHistory`.
- Обновления: `updatePhoneCall` (все состояния), `updatePhoneCallSignalingData`.
- Push: VoIP (PushKit), APNs (расширение), FCM.
- Таймауты сервера, выдерживающие перезапуск.

## Критерии приёмки
- Модульные: выбор общего протокола, представление звонка для каждой стороны, причины, совпадение таймаутов с `help.getConfig`, разбор серверов и пароль TURN, push звонка для Android и iOS, выбор канала.
- Интеграционные на MySQL (`pkg/phonecalls`): повтор `random_id`, полный цикл с длительностью, гонка принятия, просроченные звонки и незаписанные сообщения, звонок с `getDifference` один раз, очистка.
- e2e (`test/e2e/calls_test.go`): полный звонок на двух устройствах собеседника, отказ по приватности и блокировке, пропущенный звонок по таймеру, звонок с `getDifference`.
- В клиентах: реальный разговор, скриншоты на iOS, Android, Desktop, Web K (после развёртывания coturn).

## Результат
Написан сервер личных звонков. Новая таблица `phone_calls` с конечным автоматом (`pkg/phonecalls`, миграция `migrate-20261109-phone-calls.sql`): все переходы — условные `UPDATE`, поэтому первое устройство выигрывает гонку, перезапуск и несколько экземпляров ничего не меняют. Сервис `app/bff/voipcalls` (маршрут `/mtproto.RPCVoipCalls` в трёх `session.yaml`) реализует все методы задачи, подмена `phone.getCallConfig` убрана. Сервер только пересылает `g_a_hash`, `g_b`, `g_a`. Обновления адресные: звонящее и принявшее устройства определены, адрес находится через статус-сервис (`sync.updatesMe` с `server_id`), остальным устройствам — `phoneCallDiscarded`. Push входящего: PushKit если есть VoIP-токен, иначе APNs с расширением; FCM `PHONE_CALL_REQUEST`; устройство, пропустившее живое обновление, получает звонок из `getDifference`, при необходимости `differenceEmpty` заменяется на `difference`. Сообщение `messageActionPhoneCall` (причина, длительность по серверному времени) пишется в истории обоих через `msg.sendMessageV2` с `random_id = id звонка`: при сбое подчищает воркер, сообщение одно. Воркер (`Sweeper`, 2 секунды, в `bff`) завершает брошенные звонки и удаляет старые строки. Список соединений — `phoneConnectionWebrtc` из `CALLS_ICE_SERVERS`, пароль TURN делается на звонок из `CALLS_TURN_SECRET` (`pkg/phonecalls/ice`, порядок оператора — `docs/ops/calls.md`).

Проверка на ветке (CachyOS): `go build ./...`, `go vet -tags integration,e2e -unreachable=false` и `go test` затронутых пакетов, `pkg/conf`, `tools/...` — зелёные. Не запускались: интеграционные тесты (`pkg/phonecalls`), e2e, -race, make lint — полный прогон партии; проверка в клиентах — нужен развёрнутый coturn. **Сервер TURN не развёрнут**: без `CALLS_ICE_SERVERS` звонки выключены.

Решения для подтверждения:
- Таймауты сервера = таймауты `help.getConfig` (20/90/30 с) + 10 с запаса, чтобы завершал клиент; активный без устройств — 48 часов.
- Устройство, не получившее живое обновление, получает `phoneCallRequested` через `getDifference` (иначе Android ничего не узнаёт, см. источник), а предложение без `phone.receivedCall` повторяется (см. «Доработка после чтения клиентов»). В документации Telegram этого нет; поведение выведено из клиентов: кроме Android, tdesktop прямо ждёт такой повтор (`calls_instance.cpp:680`, «May be a repeated phoneCallRequested update from getDifference»).
- Из общих библиотек сервер называет наибольшую в строковом порядке (`Negotiate`, источники выше): с Web K это единственная `13.0.0`, между остальными клиентами `9.0.0`.
- Настройки уведомлений чата на звонок не влияют (клиенты их не проверяют для `updatePhoneCall`).
- `need_rating` и `need_debug` сервер не ставит, поэтому `saveCallLog` файл не хранит, `saveCallDebug` пишет лог в строку звонка (до 64 КБ), `setCallRating` хранит оценку. Без `need_rating` оценку можно оставить только там, где клиент предлагает её сам (Android, пункт в сообщении о звонке: `ChatActivity.java:33716`); tdesktop показывает окно оценки лишь при `need_rating` (`calls_call.cpp`, `phoneCallDiscarded`).
- Сервер без TURN-серверов в конфиге отвечает `CALLS_NOT_CONFIGURED` на `requestCall`: iOS падает на пустом списке соединений.
- Занятость не проверяется сервером: автоотказ `Busy` делает клиент (Android, tweb, tdesktop). `Busy` об этом же звонке сервер не принимает за ответ (см. ниже).
- Ограниченному модерацией звонящему `phone.requestCall` отвечает `USER_PRIVACY_RESTRICTED`, контакт вызываемого остаётся доступен, забаненному — нет (как у сообщений). `PEER_FLOOD` нет в списке ошибок `core.telegram.org/method/phone.requestCall`, а tdesktop показал бы его как есть (`calls_call.cpp:1610`). Как поступает Telegram, не проверено.
- Лимит: 30 звонков в минуту от одного пользователя и 10 одному собеседнику, сверх — `FLOOD_WAIT_60`. Числа наши; Telegram их не публикует.
- Сообщение о звонке не проверяется настройкой «только контакты и Premium» (как служебные сообщения у ограничений). Отказывает ли Telegram в самом звонке по этой настройке, не проверено.

### Доработка после чтения клиентов
Звонок нельзя попробовать без TURN, поэтому код сверен с исходниками клиентов. Подтвердились и исправлены все 15 замечаний:
- **`messages.getDhConfig`** не был реализован и не маршрутизировался, поэтому ни один звонок не начался бы (Android `VoIPService.java:1068`, tdesktop `calls_instance.cpp:507`, iOS `SecretChatEncryptionConfig.swift:9`, tweb `appCallsManager.ts:165`). Добавлен сервис `app/bff/secretchats`: группа — простое число обмена ключами (tdesktop знает его как хорошее, `mtproto_dh_utils.cpp:107`), `g = 3`, версия 1, `random` запрошенной длины; остальные методы секретных чатов — `ErrEnterpriseIsBlocked`. Маршрут `/mtproto.RPCSecretChats` включён в трёх `session.yaml`.
- **ICE:** соединение без IPv4 больше не выдаётся (Android берёт только `ip`, `Instance.cpp:854`; Web K строит URL без проверки, `p2P/utils.ts:169,175`). Запись `[IPv6]` присоединяется к записи с IPv4 и теми же портом и видами, без такой записи `bff` не стартует.
- **Гонки:** отказ устройства проигрывает принятию на другом (`DiscardAsParticipant`, ответ `CALL_ALREADY_ACCEPTED`); воркер завершает только то, что всё ещё просрочено (`Expire`), и по порядку сроков; флаг `video` из `phone.discardCall` сохраняется.
- **Что знает сервер об устройстве:** `phone_call_deliveries` теперь хранит предложения звонка, `phone.receivedCall` и пропущенные шаги (миграция этой задачи изменена: она нигде не применялась). Доказательство получения — только `phone.receivedCall`; без него звонок предлагается снова через 5 секунд: воркером (до 3 раз устройству, которое онлайн) и через `getDifference`.
- **Пропущенные обновления:** шаг обмена ключами или конец звонка, не дошедшие до устройства, воркер отправляет, когда оно снова онлайн, с текущим состоянием звонка, один раз (iOS завершает звонок при повторном `phoneCallAccepted`, `CallSessionManager.swift:1131`). То же для звонивших устройств, которые были офлайн при отмене или ответе на другом устройстве: Android и iOS сами звонок не завершают. `phoneCallWaiting` и сигналинг не повторяются.
- **`phone.receivedCall`** отвечает `CALL_ALREADY_DECLINED`, если звонок уже принят другим устройством или закончен: так iPhone, разбуженный PushKit, перестаёт звонить (`CallSessionManager.swift:759-773`).
- **Busy об этом же звонке** (Android не сравнивает id, `MessagesController.java:20377`) сервер не считает ответом, если устройство уже приняло звонок, подтвердило его или получило его не единственным живым предложением; ответ — пустой `Updates`.
- **Прочее:** `phone.requestCall` учитывает ограничения модерации и лимит частоты; сообщение о звонке не отклоняется настройкой Premium; e2e проверяет повторный `discardCall` и `getDhConfig`.

Проверка (CachyOS): `go build ./...`; `go vet -tags integration,e2e -unreachable=false` и `go test` затронутых пакетов, `pkg/conf`, `tools/...`; интеграционные тесты `pkg/phonecalls` целиком и `TestSendToPremiumOnly`, `TestServiceMessageToPremiumOnly` в `app/messenger/msg/internal/dao` — зелёные. Без исправлений падают `TestExpireRechecks`, `TestDeviceThatDeclinesLosesToTheOneThatAccepted`, `TestServiceMessageToPremiumOnly`. Новые e2e только скомпилированы. Не запускались e2e, -race, make lint (общий прогон партии). **Реальный звонок и клиенты не проверялись:** нет TURN.

Что остаётся: доставка шагов — не более одного раза: сбой между отметкой и отправкой теряет шаг, а запись статуса устаревшего соединения (до 90 секунд) выглядит как онлайн, и тогда шаг уходит в никуда. Android, получивший повтор до своего `phone.receivedCall` (повторная доставка из Kafka после сбоя sync в этом окне), может завершить звонок как `Busy`: живое предложение было единственным, отличить нечем. На `phone.receivedCall` с ошибкой tdesktop показывает окно с текстом ошибки (`calls_call.cpp:1610`), это возможно только в короткой гонке с ответом на другом устройстве.

Открытые вопросы: координация выдачи `p2p_allowed` со странами (ADR 0004: GeoIP и регионы) — на этапе SFU/форвардеров; развёртывание coturn; проверка в клиентах.
