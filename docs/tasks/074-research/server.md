# server: обновление до нового upstream

Текущая: 228. Новейшая: 233. Слой: 233.

## Итог
Сервер на layer 228. Новейшая схема — 233: tdesktop v7.4.1 (`api.tl:3300`, `// LAYER 233`, релиз 2026-11-06, prerelease=false по GitHub API). Android `TLRPC.java:76` и tweb `schema.ts` в reference-src-new тоже на 233.
Upstream `teamgram/proto` кончается на v0.231.0 (HEAD main = тег, c145a4d, 2026-10-26). Upstream teamgram-server пока на v0.230.0 (его go.mod).
Переход до 231 — около 2–3 дней. 232–233 требуют либо своего генератора, либо ручной правки кодека.

## Как устроен вендор и что такое bump
- **Происхождение кода.** Шапки файлов: «Created from 'scheme.tl' by 'mtprotoc'» (`class_name_registers.go`, `codec_schema.tl.pb.go`, `class_id_registers.tl.go`).
- **Цепочка.** mtprotoc из TL даёт `.proto`, кодек и реестры. Затем `build2.sh` запускает protoc.
- **Генератора нет.** mtprotoc отсутствует локально (`which`) и в организации teamgram (список репозиториев через gh). Исходных `.tl` в модуле нет (`find` в v0.231.0). Есть только старые сторонние копии: yeahbull/mtprotoc (2018), snow1emperor/mtprotoc (2023). Их совместимость с текущим форматом не проверена. Каталог `v2/` в proto v0.231.0 — другой, сгенерированный кодек, не генератор. docs/audit/03 (строки 14, 98) говорит то же.
- **Что воспроизводимо.** Только этап protoc. Я прогнал его на временной копии (protoc-gen-go 1.36.5, grpc 1.5.1, как в задаче 020).
- **Порядок bump по ADR 0009.** Заменить копию новым upstream одним коммитом и наложить наши `third_party(proto):` заново.
- **Проверка на временной копии (репозиторий не менялся).** Наши правки — 1084 ханка в 13 файлах. На v0.231.0 не легло 34:
  - 8 в `schema.tl.sync.pb.go` — снимаются перегенерацией protoc;
  - 26 в `codec_schema.tl.pb.go` — всё те же механические границы векторов («stop at the first decode error», «bound the length…»).
  Все остальные файлы, включая `schema.tl.sync.proto`, `decode.go`, `crypto/auth_key.go`, легли чисто. Наши поля 11, 36, 37 (ContactData, UserData) не заняты upstream 231.
- **Сборка сервера.** `go build -modfile -gcflags=-e ./...` с копией 231: одна первопричина, `missing method AuthCancelWebTokenAuthorization` в `authorization/.../grpc.go:26` и `bff/.../server.go:169`. Но сборка всех пакетов, зависящих от этого сервиса, не проверялась. Upstream при bump 229/230 добавил лишь заглушки: 9 и 5 файлов (коммиты cef5d30c9, 8adf26bc0).
- **Жёсткие привязки к 228:** `pkg/catalog/tl/tl.go:50`, `pkg/mtclient/client.go:23`, проверка `pkg/catalog/importer/manifest.go:85` (коллекция каталога должна быть того же слоя, значит нужен реимпорт или смягчение проверки), `UPSTREAM_LAYER` в клиентах. `tools/apicoverage` берёт слой из версии модуля.

## Несколько слоёв одновременно: да, с оговорками
**Механизм есть.**
- `GetClazzID(name, layer)` (`class_name_registers.go:186378`) выбирает конструктор по слою, Encode сериализует по этому case.
- Decode идёт по конструктору; в `clazzIdRegisters2` есть исторические id.
- Слой хранится на auth key (`main_auth_wrapper.go:366`). Ответы и пуши кодируются слоем своей сессии (`session.go:477, 509, 537, 558, 596`), хранимые сообщения — JSON (`channel_pin.go:77`).
- Мой разбор v0.231.0 для слоёв 218–231: у каждого id типа-конструктора есть ветки Encode и Decode, каждый id зарегистрирован. Значит клиент 228 на кодеке 231 обслуживается. Методы проверены только регистрацией id и выборочно (`messages.sendMessage`).
- `pkg/mtclient` принимает `cfg.Layer`, тесты могут ходить любым слоем.

**Оговорки.**
1. Слой выше верхнего в кодеке не обслуживается: `GetClazzID` вернёт 0, почти всё падает с «not found clazzId». Клиенты 233 не заработают на кодеке 231.
2. Ошибки Encode проглатываются: `session.go:477, 509, 537`, `parsed_manually_types.go:405`. Если клиенту старого слоя уйдёт конструктор, которого в его слое нет (в реестре 231 на 124 предиката больше, чем в 228), в сеть уйдёт усечённое тело. Это надо чинить до смешанного режима.
3. Пуши строятся один раз на всех: фильтр по слою в bff не покрывает их, преобразование должно жить в session или перед Encode. Сейчас есть только точечные `md.Layer` (`bff_proxy_client.go:196, 226`). Байтовый канал `onSyncRpcResultData` (`session_sync.go:61`) отдаёт готовые байты. Откуда они берутся, не проследил.
4. На 229 inline-клавиатуры переделаны, 17 конструкторов `keyboardButton*` удалены. В кодеке 231 `keyboardButtonCallback` есть только до слоя 228, `replyInlineMarkup` хранит два разных поля строк. JSON-имя `rows` исчезает (в 231 это `rows_VECTORKEYBOARDBUTTONROW` и `..INLINEBUTTONROW`). В коде app/pkg клавиатуры не собираются, ботов нет (bots: 39 missing). Есть ли `reply_markup` в строках базы — не проверено, на проде не смотрел.
5. Номера protobuf-полей сдвинулись у 182 полей (ChatFull 68, PageBlock 41, KeyboardButton 19, Update 17 и др.). JSON в БД это не задевает; gRPC и бинарный каталог (`bff/catalog/core.go:58`) задевает. Сервисы надо перезапускать вместе. Формат очереди sync (Marshal в `sync_outbox_client.go`) не разбирал — считать, что её нужно опустошать перед выкладкой.
6. ADR 0008 («сервер и клиенты вместе») придётся заменить новым ADR.

## Дельта 228 → 233 (tdesktop api.tl: 228, v7.1.0=229, v7.3.0=230, v7.3.2=232, v7.4.1=233)
Всего: +114 конструкторов, −18, изменено 17; +61 метод, изменено 7. Удалённых методов нет. Метка 231 есть только в кодеке teamgram: тега tdesktop со слоем 231 нет.

**Форма того, что сервер уже шлёт:**
- `message`, `messageService`, `user`, `chat`, `dialog`, `updates*`, `updateShort*` — без изменений.
- 232: `channel` d49f34c6→7ad15240 (+in_news flags2.21, forum_sidebar flags2.23, pinned_msg_id flags2.22).
- 232: `channelFull` a04e8d3a→c4c4dd40 (+has_welcome_messages, share_theme).
- 232: `userFull` 6cbe645→b90c2afd (+wallet_card, share_theme).
- 232: вложенный `messageReplyHeader` 1b97dd66→ddb5ab75 (+quote_rich), вход `inputReplyToMessage` 3bd4b7c2→75c1ad70 (+quote_rich).
- 229: `keyboardButton` (+type:ButtonType), `replyInlineMarkup` (+flags, строки KeyboardInlineButtonRow), `pageBlockBlockquote`, `messageActionStarGiftUnique`, `sendMessageTextDraftAction` и `sendMessageRichMessageDraftAction` (+flags), `ephemeralMessage`.
- 230: `botVerification`, `botVerifierSettings`.
- 231 (по реестру кодека): `urlAuthResultRequest`.
Новые поля опциональны, кроме типа кнопки. Для 232+ достаточно нового конструктора с пустыми флагами; как клиенты ведут себя без них, не проверял.

**Методы, которые трогают реализованное:** `channels.toggleForum` a2952b54 (233, обязательный `sidebar:Bool`; статус ok, `channels.forum_handler.go:32`). Остальные изменённые (`bots.checkUsername`, `bots.setCustomVerification`, `messages.acceptUrlAuth`, `ephemeral.*`, `payments.getStarGiftWithdrawalUrl`) в api-coverage значатся как missing.

**Новое, но не форма текущего:** кошелёк, TON, onramp (wallet.*, toncenter.*, payments.*Onramp*), widget (`messageMediaWidget`, updates), share theme, `updateEditForumTopic`, `updateTranscribedAudioFile`, `messages.transcribeAudioFile`, `messageActionChatJoinedViaCommunity`, `messageEntityTonAddress`.

**Чего нет в кодеке 231 (по id, относительно 233):** 40 конструкторов (6 изменённых, 34 новых) и 19 методов (2 изменённых, 17 новых). Это и есть объём ручной работы или генератора.

## Этапы (рабочие дни одного инженера, оценка)
1. Кодек 231: замена копии, 26 ханков, protoc, заглушка `AuthCancelWebTokenAuthorization` и прочие auth-методы, api-coverage, полные тесты и e2e. Клиенты остаются на 228. 2–3 дня.
2. Безопасность смешанных слоёв: обработка ошибок Encode, преобразование вниз по слою в session, тесты 228 и верхнего слоя через mtclient, новый ADR. 3–5 дней.
3. Кодек 232–233, на выбор:
   - свой генератор в формате mtprotoc, с контрольной проверкой: повторить вендоренный 228 из `api.tl`; заодно снимает риск ADR 0008 — 5–8 дней (рекомендую);
   - ручная правка 40+19 в шести файлах по образцу прежних внутренних правок — 3–5 дней, ошибки вероятнее;
   - ждать upstream: сроки неизвестны.
4. Содержимое 232–233: `toggleForum sidebar`, чтение `quote_rich`, новые конструкторы `channel`, `channelFull`, `userFull`; понятные ошибки для 61 нового метода (`ErrEnterpriseIsBlocked`, не 500). 3–5 дней минимум; каждая отдельная функция (share theme, widget и т. д.) — сверх этого.
5. Эксплуатация: одновременный перезапуск, очередь, каталог, инструкции в `docs/ops`. 1–2 дня.
6. Смешанная проверка клиентов 228 и 233 на стенде. 2–3 дня (без самих работ в клиентах).
Итого сервер: 16–26 дней.

## Не проверено
Запуск тестов на кодеке 231 (только сборка). Поведение клиентов 232+ при пустых новых флагах. Наличие `reply_markup` в продовой базе. Формат очереди. Происхождение байт в `onSyncRpcResultData`. Даты тегов 229–230.

Свежие исходники: `/Users/sckr.a/pluma-work/reference-src-new/{proto-v0.228.0,proto-v0.231.0,tdesktop-schema}`, вместе менее 100 МБ.
