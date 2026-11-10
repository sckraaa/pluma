# 073. Коды входа: Telegram и, возможно, SMS

- Статус: done (сервер; аккаунт вошёл по QR 2026-11-10, настоящая отправка коду человеку не проверена)
- Зависит от: —
- ADR: 0042.

## Цель
Сейчас регистрация закрыта: номер добавляют в список, код читают через `pluma-admin`. Нужна настоящая доставка кодов, чтобы регистрация могла быть открытой.

## Решения владельца (2026-11-09)
- В идеале SMS, но это, вероятно, дорого. **Минимум — через Telegram.** В проекте уже есть бот; его токен лежит в `/opt/pluma/.env` как `TELEGRAM_BOT_TOKEN` (значения не читать).

## Решение владельца (2026-11-09, уточнение)
Telegram Gateway отпал: минимальное пополнение 100 $. **Коды отправляет настоящий аккаунт Telegram** (не бот): пользователей немного, все свои.

- **Как:** `contacts.importContacts` находит человека по номеру (метод доступен только пользователям, core.telegram.org/method/contacts.importContacts), затем обычное сообщение с кодом, контакт удаляется.
- **Аккаунт:** отдельный номер, не личный: сессии сторонних клиентов Telegram завершает без объяснений, особенно у свежих аккаунтов (задача 007, tdlib/td#3044, #3101). Заблаговременно зарегистрировать в официальном приложении, включить 2FA, вход сервера по QR (`tglogin -qr`).
- **Клиент: TDLib** (решение владельца 2026-11-09, вместо самописного `pkg/mtclient`, Telegram Web в браузере и SMS через телефон). Отдельный маленький сервис отправки кодов рядом с остальными; рассчитать память до развёртывания, CX33 общий. Вход — QR-кодом этого аккаунта, файлы сессии TDLib только в отдельном томе сервера. Что отзывы сеансов зависят от клиента, не подтверждено: TDLib снижает риск, не устраняет его.
- **Сессия:** только на сервере, как секрет. Одна авторизация — одно соединение (`AUTH_KEY_DUPLICATED`).
- **Запасной путь обязателен:** нет Telegram у человека, у аккаунта ограничение, сессия отозвана — код видит владелец в панели (072), срабатывает алерт.
- **Риски, не проверенные на практике:** ограничения Telegram за сообщения незнакомым; условия API запрещают действия от имени пользователя без его согласия (core.telegram.org/api/terms, 1.4), здесь аккаунт свой.

## Что надо выяснить (источники назвать, память не считается)
- **Бот.** Бот не может написать человеку по номеру: нужен `/start` и отправка контакта (проверить, что контакт свой: `contact.user_id == from.id`). Сверить с документацией Bot API. Какой `auth.sentCodeType` и какой переход показывают все четыре клиента, нужны ли патчи.
- **Telegram Gateway** (доставка кодов от Telegram без бота и без `/start`): условия, цена, ограничения стран, нужен ли аккаунт владельца. Не проверено.
- **SMS:** поставщики и цена для основных стран, защита от накрутки (SMS pumping).
- Защита: действующие лимиты `pkg/logincode`, срок кода, отсутствие кодов и номеров в логах.

## Область
- Абстракция доставки кода в `auth.sendCode`, `auth.resendCode` и смене номера; порядок каналов и запасной канал.
- Переключатель регистрации «открыта / по списку» (хранение и чтение сервисом; интерфейс — 072).
- `TESTING-BACKLOG`: сценарии входа.

## Результат
Сервер коды писать готов, но ни один реальный код ещё не ушёл: **настоящую доставку нельзя проверить, пока владелец не войдёт в аккаунт по QR** (и не заведёт сам аккаунт). Сервис `app/ops/codes` держит сессию TDLib в своём томе, очередь с паузами, лимиты на номер и на аккаунт. bff сначала пробует код в приложении Pluma, затем аккаунт Telegram, затем прежний путь через администратора. Сервис необязательный: без `LOGIN_CODES_SENDER_URL` поведение прежнее. Проверено модулями с подставным TDLib и подставным отправителем; образ не собирался, интеграционные и сквозные тесты — в общем прогоне партии.

**Что сделано**
- `app/ops/codes` — сервис: `serve`, `login` (QR в терминал, пароль 2FA без эха), `healthcheck`. Внутренний API: `POST /v1/send`, `GET /v1/state` (для панели), `/metrics`, `/healthz`. `internal/account` — TDLib, `internal/sender` — очередь, `internal/tdjson` — обвязка над `libtdjson` (собирается только с тегом `tdlib`, в образе).
- `teamgramd/deploy/codes/Dockerfile`, `docker-compose.codes.yaml` (256 МБ, том `codes_data`, порт `127.0.0.1:9378`), `pluma-codes` в CI образов.
- `pkg/logincode`: интерфейс `Sender`, HTTP-клиент, `Service.DeliverByTelegram`, правило `TelegramCodePerIP`, `Service.RegistrationOpen(ctx)`, таблица `registration_settings` (`migrate-20261109-login-codes.sql`).
- `auth.sendCode`, `auth.resendCode`, `account.sendChangePhoneCode` пробуют аккаунт; метрика `login_code_sender_total`, правила `rules/codes.yml`, цель `ops.codes`.
- Документы: `docs/ops/login-codes.md`, ADR 0044, раздел в `monitoring.md`, строка в `security-setup.md`, `TESTING-BACKLOG`.

**Источники**
- TDLib: коммит `c0a540074fa449094340f76a3ea56f7096e0ad4c` (2026-11-08, версия 1.8.69 по `CMakeLists.txt`), лицензия Boost 1.0 (`LICENSE_1_0.txt`), официальный `tdlib/td`, не tdlight (форк не нужен: нужен только JSON-интерфейс и эти методы).
- `td/generate/scheme/td_api.tl` этого коммита: `importContacts contacts:vector<importedContact>`, в ответе `importedContacts.user_ids` стоит 0, если номер не зарегистрирован; `removeContacts user_ids:vector<int53>`; `createPrivateChat user_id:int53 force:Bool`; `sendMessage … input_message_content`; `updateMessageSendSucceeded` и `updateMessageSendFailed` с `old_message_id`; `requestQrCodeAuthentication other_user_ids`; `authorizationStateWaitOtherDeviceConfirmation link` («the link will be updated frequently»); `authorizationStateWaitPassword`, `checkAuthenticationPassword`; `authorizationStateClosed` («one must create a new instance»); `setTdlibParameters` (ошибка 401 при неверном ключе базы).
- `searchUserByPhoneNumber` отброшен: «Returns a 404 error if the user can't be found», задача владельца — `importContacts`.
- Ошибки TDLib: ожидание — код 429 с текстом `Too Many Requests: retry after N` (`td/telegram/Global.cpp`, `get_retry_after`), `contacts.importContacts`, вернувшая все контакты в `retry_contacts`, TDLib отдаёт как `retry after 3600` (`UserManager.cpp`, `ImportContactsQuery`), `USER_IS_BLOCKED` доходит как есть (`MessagesManager.cpp`, `on_send_message_fail`).
- `td_json_client.h`: клиент не шлёт обновления до первого запроса; один поток `td_receive`.
- Память сборки: генератор инструкций `tdlib.github.io/td/build.html` предлагает особый режим для машин с памятью меньше 4 ГБ. Память в работе в документации не указана; 256 МБ — оценка, измерить после входа (`docker stats`).
- Условия API: core.telegram.org/api/terms, п. 1.4. Аккаунт свой, сообщения — по запросу самого человека; риск ограничений остаётся.
- Что показывают клиенты, тип `auth.sentCodeTypeApp`: Android — `LoginActivity.java` 1898 (`AUTH_TYPE_MESSAGE`), строки `SentAppCodeTitle` и `SentAppCodeWithPhone`, ссылка «не пришёл код» при `next_type` sms (4430–4438). iOS — `.otherSession`, `Login.CodeSentInternal` (`AuthorizationSequenceCodeEntryControllerNode.swift`). Desktop — `intro_step.cpp` 365 (`codeByTelegram`), `lng_code_from_telegram` и кнопка `lng_code_no_telegram` (`intro_code.cpp` 73–105). Web K — `AuthCodeCard.tsx` 508 (`Login.Code.SentInApp`), `next_type` — повторный запрос (`sentCode.ts` 99).

**Решения, требующие подтверждения**
- Порядок: код в приложении Pluma (если есть другая сессия), затем аккаунт Telegram, затем администратор. Так аккаунт пишет меньше людям и реже рискует ограничением, а входить заново можно тем же кодом из приложения.
- Тип ответа при доставке через Telegram — `auth.sentCodeTypeApp`, `next_type` sms: единственный тип, у которого во всех четырёх клиентах есть «не пришёл код» и без таймера. Переход работает без патчей клиентов.
- **Текст на экране кода нужно решить.** `Telegram` в строках клиентов заменён на `Pluma`, поэтому экран говорит «проверьте сообщения в Pluma», хотя код в Telegram. Варианты: оставить как есть (люди свои, им скажут) или исключить ключи `SentAppCodeTitle`, `SentAppCode`, `SentAppCodeWithPhone`, `Login.CodeSentInternal`, `lng_code_from_telegram`, `Login.Code.SentInApp` из переименования, но тогда при коде в другой сессии Pluma написано будет «Telegram». В этой задаче не менялось (сервер, клиенты отдельно).
- Смена номера при доставке через Telegram отвечает `App` без `next_type`.
- Лимиты аккаунта (`sender.DefaultLimits`): пауза 5–10 с, 3 в час и 8 в сутки на номер, 30 в час и 120 в сутки всего, после `PEER_FLOOD` — 24 часа. Подобраны с запасом, данных о реальных порогах Telegram нет.

**Открытые вопросы и что не проверено**
- Настоящая доставка, вход по QR и сама сборка образа не выполнялись (нужен аккаунт и QR; образ не собирался по условиям задачи). Привязка cgo скомпилирована и слинкована с заглушкой `libtdjson` и настоящим заголовком `td_json_client.h` этого коммита.
- Текст ошибки `PEER_FLOOD` на отправке не подтверждён чтением документации (в списке `messages.sendMessage` его нет, TDLib не обрабатывает его особо): код только не полагается на него, неизвестные ошибки дают «недоступно», код уходит администратору.
- Desktop после любого `auth.resendCode` сам ставит `codeByTelegram = false` (`intro_code.cpp`, `noTelegramCodeDone`): если повторный запрос ушёл в аккаунт Telegram и ответ снова `App`, экран говорит об SMS и кнопки «не пришёл код» больше нет. Код при этом работает, администратор может выдать новый (`pluma-admin login-code`). Android (тот же экран через `setParams`) и iOS (`Login.SendCodeViaSms`) этот переход проходят.
- **Закрытие регистрации нужно решить.** `allowed` пускает номера списка или всех при открытой регистрации. Кто зарегистрировался, пока она была открыта, в список не попадает, поэтому после переключения на «по списку» он получит `PHONE_NUMBER_INVALID` и не войдёт. Варианты: при регистрации добавлять номер в список либо пускать уже существующих пользователей. Здесь не менялось.
- Смена номера на iOS с типом `App` по исходникам не проверена; Desktop смену номера не реализует (метод есть только в `api.tl`).
- Включение открытой регистрации — задача 072; здесь только `RegistrationOpen` и таблица, по умолчанию регистрация по списку.
- Telegram Gateway и SMS: решением владельца отпали, цены и условия не исследовались. Проверка бота через Bot API не понадобилась: бот не пишет по номеру.

### Развёртывание (2026-11-10)
Образ `pluma-codes` коммита `a149ec683` на проде. В `.env` добавлены `LOGIN_CODES_SENDER_TOKEN`, `LOGIN_CODES_DB_KEY` (сгенерированы на сервере, значения не выводились), `PLUMA_CODES_IMAGE`. Вход аккаунта по QR и паролю двухэтапной аутентификации выполнил владелец; `codes` запущен, `/v1/state`: `session=ready`, `sendable=true`. Затем `LOGIN_CODES_SENDER_URL=http://codes:9378`, bff пересоздан, имя `codes` из bff разрешается. Отправка настоящего кода человеку — в `TESTING-BACKLOG.md`.

