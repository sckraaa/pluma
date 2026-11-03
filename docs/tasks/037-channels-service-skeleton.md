# 037. Каналы: сервис biz `channel`, модуль bff `channels`, маршрутизация

- Статус: todo
- Зависит от: 036
- ADR: 0021, «Ящик канала» (из 035). Исследование: `docs/tasks/035-research/` (`server.md` §1.1, §1.5, §2.5; `plan.md` §3, задача «036» плана).

## Цель
Все 74 метода `channels.*` слоя 228 доходят до нашего модуля bff и отвечают осознанно: обслуженные — по делу, остальные — `ErrEnterpriseIsBlocked` (400), ни один не отвечает 500. Есть сервис чтения каналов и построение объекта `Chat` канала для конкретного зрителя.

## Что известно
- Модуля bff для каналов нет. Маршрут закомментирован во всех конфигурациях сессии: `#"/mtproto.RPCChannels": "bff.bff"` (`teamgramd/etc2/session.yaml:82`, `teamgramd/etc/session.yaml:82`, `app/interface/session/etc/session.yaml:63`). Немаршрутизированный сервис получает `ERR_ENTERPRISE_IS_BLOCKED` или подделку из `app/bff/bff/client/fake_rpc_result.go` (правило проекта для новых сервисов bff).
- Заглушки `channels.*` разбросаны по модулям chats, messages, usernames, chatinvites, sponsoredmessages, savedmessagedialogs, userchannelprofiles (`server.md`, §1.5):
  - ok: `getSponsoredMessages` — пустой заготовленный ответ (`app/bff/sponsoredmessages/internal/core/channels.getSponsoredMessages_handler.go:27`);
  - fake: `reportSpam` (`fake_rpc_result.go:47`);
  - blocked: 15 методов (`checkSearchPostsFlood`, `searchPosts`, `getSendAs`, `checkUsername`, `updateUsername`, четыре метода спонсорских сообщений, `convertToGigagroup`, `setEmojiStickers`, `getMessageAuthor`, `setMainProfileTab`, `toggleJoinRequest`, `toggleJoinToSend`);
  - missing: 57 методов.
- `ERR_ENTERPRISE_IS_BLOCKED` — код 400 (`third_party/teamgram-proto/mtproto/rpc_error_codes.go:341`). Транспорты Android и Web K его не повторяют. gRPC Unimplemented (500) или ошибка не в виде `^[A-Z_0-9]+` повторяется бесконечно: Android tgnet (`jni/tgnet/ConnectionsManager.cpp:1368-1385`), Web K (`appManagers/apiManager.ts:785-797`, `mtproto/networker.ts:1708-1719`) (`clients.md`, §2).
- Резолвер пользователей и чатов ADR 0021 (`Pick`/`Resolve`) каналы пока не умеет: ADR 0021 §7 — каналы в `chats` разницы не разрешаются (`server.md`, §1.1, §2.5).
- Объект канала: `channel#d49f34c6` с `min`, `left`, `admin_rights`, `banned_rights` для зрителя; `channelForbidden` (api.tl:105) — для исключённого или забаненного с `view_messages`, или для покинутого закрытого канала, `until_date` — для временного бана. iOS разбирает `channelForbidden` в `UpdatesApiUtils.swift:216` и `ApiUtils/ApiGroupOrChannel.swift:205` (`server.md`, §2.2, §2.5).

## Область
- Сервис `app/service/biz/channel`: чтения (`getMutableChannel`, `getChannelListByIdList`, чтения участников) поверх таблиц 036.
- Модуль `app/bff/channels`: регистрация в `app/bff/bff/internal/server/server.go`, маршрут `"/mtproto.RPCChannels": "bff.bff"` в трёх `session.yaml`.
- Каждый ещё не обслуженный метод `channels.*` явно отвечает `ErrEnterpriseIsBlocked`. Разбросанные заглушки остаются или переезжают — по тому, как устроены соседние модули; спонсорские, бусты, платные сообщения, истории, рекомендации, автоперевод, эмодзи-статус, наборы стикеров остаются на 400 (ADR 0016, `plan.md` §3 «Deferred»).
- Построение `channel`/`channelForbidden` для зрителя по состоянию участника.
- Источник каналов для резолвера ADR 0021.
- Не трогаем: запись в каналы, обработчики `messages.*`.

## Критерии приёмки
- Модульные тесты: таблица построения объекта канала по состоянию участника (создатель, админ, участник, вышел, исключён, забанен с `until_date`, не участник открытого и закрытого канала, `min`).
- Интеграционный тест сервиса `channel` на MySQL через `pkg/testinfra`.
- e2e через `pkg/mtclient` (`test/e2e`): вызовы нескольких необслуженных `channels.*` получают 400 `ERR_ENTERPRISE_IS_BLOCKED`, не 500; тест падает без маршрута.
- `go run ./tools/apicoverage` перезапущен: ни одного `channels.*` в состоянии `missing`.
- Обычные проверки (`go build`, `go vet -unreachable=false`, `go test` и `-race`, `make lint`, `go test ./tools/...`).
- В клиентах: видимых изменений нет; проверка в клиентах не нужна.

## Результат
