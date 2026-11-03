# 035. Каналы: сверка с Telegram и ADR «Ящик канала»

- Статус: todo
- Зависит от: —
- ADR: 0011, 0012, 0018, 0019, 0020, 0021, 0022; результат — новый ADR «Ящик канала» (следующий свободный номер, сейчас 0032). Исследование: `docs/tasks/035-research/` (`server.md` — сервер сегодня и целевая модель, `clients.md` — что ждут клиенты, `plan.md` — выборочная проверка обоих отчётов и разбивка этапа; номера задач в `plan.md` на единицу меньше здешних, соответствие — в `plan-ru.md`).

## Цель
До первой строки кода этапа 1 закрыть открытые вопросы о поведении Telegram и клиентов и записать модель хранения каналов в ADR, чтобы задачи 036–049 опирались на прочитанные источники, а не на догадки.

## Что известно
- **Исходники клиентов не сверены с закреплёнными релизами.** `upstream.env` нашёлся только у Web K (`~/pluma-web/upstream.env`, `346844bf`, слой 228, версия 2.2 (676)). Для iOS, Android и tdesktop закрепления на машине нет, в `clients/*.md` тоже (`plan.md`, §1). Отчёты читали разные деревья:
  - iOS: локальное `~/aaaaaaa/Telegram-iOS` `756315d7` (`server.md`) и `6ad963e5` через `gh api` (`clients.md`);
  - Android: локальное `~/aaaaaaa/Telegram-Android` `20091c78`, 12.6.4;
  - tdesktop: тег v7.0.9.
  Совпадают ли они с тем, что собирает Pluma, — UNVERIFIED.
- **Противоречия между отчётами** (`plan.md`, §2):
  - **C1.** Таймаут по умолчанию в `channelDifference*`: core.telegram.org/api/updates — 1 с; iOS без флага ждёт 30 с (`AccountStateManagementUtils.swift:3143-3149`); tdesktop и Android — 1 с (по `clients.md`). Решение плана: сервер всегда шлёт явный `timeout > 0`; значение — из сверки с Telegram.
  - **C2.** `toggleJoinRequest` и `toggleJoinToSend` в `clients.md` названы «уже обслуживаются»; по `docs/project/api-coverage.md:339-340` они `blocked`. Верно второе.
  - **C3.** Строки iOS `pollChannel`: `:3413` (`756315d7`) и `:3433` (`6ad963e5`) — два разных коммита, ни один не сверен с закреплением.
  - **C4.** Как iOS догоняет каналы после `getDifference`: по `server.md` — `updateChannelTooLong` → `pollChannel`; по `clients.md` — сброс каналов через `messages.getPeerDialogs` (`AccountStateManagementUtils.swift:2042-2070`, `resetChannels` `:3226-3240`). Эти строки в выборочной проверке не читались (UNVERIFIED). Если верно второе, `getPeerDialogs` с правильными `dialog.pts`, состоянием прочтения и верхним сообщением нужен уже в 038.
  - **C5.** iOS повторяет `getChannelDifference` бесконечно при любой ошибке, кроме `CHANNEL_PRIVATE`/`CHANNEL_INVALID` (`AccountStateManagementUtils.swift:3448-3458`, `Network/Network.swift:1240-1243`). Правило проекта «неподдержанное — `ErrEnterpriseIsBlocked`» этот метод не спасает: его нужно реализовать (040) до того, как каналы появятся у пользователей.
- **Не проверено, закрыть в этой задаче** (`plan.md`, §2):
  - **U1.** Несут ли вступление, выход, исключение и `updateReadChannelInbox` общий `pts` или `seq` для своих устройств пользователя. UNVERIFIED.
  - **U2.** По какому признаку Telegram кладёт `updateChannelTooLong` в `getDifference`. UNVERIFIED.
  - **U3.** Имена ошибок: `SLOWMODE_WAIT_X`, ошибка отправки в перенесённую группу (`CHAT_MIGRATED` или другая), `RANDOM_ID_DUPLICATE`, `USER_BANNED_IN_CHANNEL`, ошибки вида `CHAT_SEND_MEDIA_FORBIDDEN`. UNVERIFIED.
  - **U4.** Как права админов группы переносятся при переходе в супергруппу; освобождены ли админы от медленного режима. UNVERIFIED.
  - **U5.** Что такое `read_outbox_max_id` в супергруппе. UNVERIFIED.
  - **U6.** Какие настройки базовой группы заставляют клиент запускать переход сам. UNVERIFIED.
  - **U7.** Всё о tdesktop сверх процитированных строк; поток создания канала в tdesktop не читался. UNVERIFIED.
  - **U8.** Совпадает ли форма ответа `messages.migrateChat` с тем, что разбирают Android, tdesktop и Web K. UNVERIFIED.
  - **U9.** Настоящие значения appConfig Telegram (`channels_limit_default` и другие); прочитаны только умолчания клиентов. UNVERIFIED.
  - **U10.** Строки `clients.md`, не перечитанные при проверке: Android `MessagesController`, Web K `apiUpdatesManager`, iOS `JoinChannel.swift:70-80`, iOS `UpdateCachedPeerData.swift`. UNVERIFIED.
- **Проверено по файлам** (`plan.md`, §1, выборочная проверка): маршруты `RPCChannels` и `RPCMessageThreads` закомментированы во всех трёх `session.yaml` (`teamgramd/etc2/session.yaml:82,104`, `teamgramd/etc/session.yaml:82,104`, `app/interface/session/etc/session.yaml:63,84`); `getDifference` отбрасывает канальные обновления (`app/service/biz/updates/internal/core/merge_updates_helper.go:148-153`); `ERR_ENTERPRISE_IS_BLOCKED` — 400 (`third_party/teamgram-proto/mtproto/rpc_error_codes.go:341`).
- **Модель, которую предлагает исследование** (`server.md`, §2.3–2.5): счётчики в строке `channels` (как `user_state`); журнал `channel_pts_updates` с окном 100000 pts (core.telegram.org/api/updates); рассылка участникам без журнала на каждого пользователя; непрочитанное считается при чтении; ключ Kafka — id канала (новый тип ключа, меняет ключ по получателю из ADR 0012); id сообщений последовательные внутри канала.

## Область
- Сверка с настоящим Telegram через Web K `invokeApi` в двух вкладках (как для 034): U1–U6, U8; таймаут, который Telegram отдаёт в `channelDifference*` (C1); имена ошибок (U3); содержимое `channelDifferenceTooLong`; ответ `messages.migrateChat`.
- Найти и записать закрепления iOS, Android и tdesktop (если их нет — вопрос владельцу, ниже), затем прочитать по ним: C3, C4, U7, U10; лимиты и путь tooLong в `getChannelDifference` у Android, tdesktop и Web K; разбор `migrateChat`.
- ADR «Ящик канала»:
  - счётчики (`pts`, `last_message_id`, количества) в строке `channels`, без idgen (ADR 0011, l.230, l.411);
  - журнал `channel_pts_updates` и его окно;
  - рассылка без журнала на пользователя; непрочитанное — при чтении;
  - ключ Kafka — id канала и как это соотносится с ADR 0012;
  - когда сервер шлёт `updateChannelTooLong`;
  - какие канальные события идут через общий ящик пользователя (`seq` или `pts`, ADR 0019–0022);
  - явный `timeout` в ответах `getChannelDifference`.
- Эталонные JSON для сравнительных сценариев (ADR 0018, `test/conformance/testdata/`): создание, отправка, `getChannelDifference`, `migrateChat`, права.
- Не трогаем: код сервера и клиентов.

## Вопросы владельцу (`plan.md`, §4)
1. Форумы и топики (≈3 дня) и журнал действий админов (`getAdminLog`, `reportSpam`, `toggleAntiSpam`, `convertToGigagroup`, ≈2 дня) — в этапе 1 или в этапе 2? ROADMAP относит форумы к этапу 1; в разбивке этапа их нет, пока владелец не решит.
2. Окно журнала канала: только 100000 pts или ещё и срок?
3. `channels_limit_default` и лимит публичных ссылок (у клиентов по умолчанию 500 и 10, `UserLimitsConfiguration.swift:41-42`); разрешать ли публичные каналы в закрытой установке.
4. Принять ли правило «каналы не выкатываются в прод до 040» или скрыть создание в форках клиентов до тех пор.
5. Добавить ли в репозиторий закрепления `upstream.env` для iOS, Android и tdesktop.

## Критерии приёмки
- ADR «Ящик канала» принят и внесён в `docs/adr/README.md`.
- Каждый пункт U1–U10 и C1, C3, C4 закрыт со ссылкой на прочитанный источник или запрос к Telegram, либо явно отложен с указанием задачи, которая его закроет.
- Закрепления клиентов записаны; строки клиентов в брифах 036–049, которые разошлись с закреплённым деревом, исправлены.
- Эталонные JSON сохранены в `test/conformance/testdata/`, запрос, по которому получен каждый, описан.
- Ответы владельца на вопросы 1–5 записаны в этот бриф.
- Код не меняется; тестов нет.

## Результат
