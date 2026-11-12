# 0058. Почта восстановления облачного пароля: коды на 6 цифр, хэш вместо кода, отправка через узкий интерфейс (сначала Resend)

- Статус: proposed (2026-11-12)
- Задача: 091 (ветка `task/091-password-recovery-email`). Обслуживаются `account.updatePasswordSettings` с `email`, `account.confirmPasswordEmail`, `account.resendPasswordEmail`, `account.cancelPasswordEmail`, `auth.requestPasswordRecovery`, `auth.checkRecoveryPassword`, `auth.recoverPassword`; меняется `account.getPassword` (`has_recovery`, `email_unconfirmed_pattern`) и `account.getPasswordSettings` (`email`).
- Связано: задача 008 (2FA по SRP, «почты нет»), ADR 0026 (служебные сообщения 777000), ADR 0044 (коды входа, тот же приём HMAC и пределов), ADR 0046 (свои строки на 37 языках), ADR 0041 (всё открыто всем).
- Заменяет решение задачи 008 «почты на сервере нет, восстановления по email нет» (`docs/tasks/008-security-p1.md:45,147`). Сброс через 7 дней и `pluma-admin password reset` остаются.

## Контекст

### Документация (прочитано 2026-11-12)
- https://core.telegram.org/api/srp, раздел о почте: почта задаётся `account.updatePasswordSettings` в `passwordInputSettings.email`; пока она не подтверждена, сервер отвечает `EMAIL_UNCONFIRMED_X`, где X — длина кода; `account.confirmPasswordEmail` включает почту, `account.resendPasswordEmail` шлёт код заново, `account.cancelPasswordEmail` отменяет.
- Там же: восстановление по почте доступно и вошедшей сессии, и невошедшей, если она «successfully provided the login code»; нужна заданная почта; `auth.recoverPassword` по коду «delete the current 2FA password», новый пароль ставится обычным путём. Сброс `account.resetPassword` — только из вошедшей сессии, таймер 7 дней.
- Страница **не говорит** ни о сроке жизни кода, ни о числе попыток, ни о том, что происходит с другими сессиями после восстановления. Всё это ниже — наше решение (Telegram здесь не проверен).
- Страницы методов: /method/account.updatePasswordSettings (`EMAIL_INVALID`, `NEW_SETTINGS_EMPTY`, `SRP_ID_INVALID`, `SRP_PASSWORD_CHANGED`, `PASSWORD_HASH_INVALID`, `NEW_SALT_INVALID`, `EMAIL_UNCONFIRMED_X`), /method/account.confirmPasswordEmail (`CODE_INVALID`, `EMAIL_HASH_EXPIRED`), /method/auth.requestPasswordRecovery (`PASSWORD_EMPTY`, `PASSWORD_RECOVERY_NA`), /method/auth.checkRecoveryPassword (`CODE_EMPTY`, `PASSWORD_RECOVERY_EXPIRED`), /method/auth.recoverPassword (`CODE_EMPTY`, `NEW_SETTINGS_INVALID`), /constructor/account.password.

### Клиенты (`~/pluma-work/reference-src-new`: Android, iOS, tdesktop, Web K, закреплённые выпуски)
- **Длина кода 6.** Android жёстко держит 6 цифр и при подтверждении, и при восстановлении (`TwoStepVerificationSetupActivity.java:122` `emailCodeLength = 6`, `LoginActivity.java:7387` `setNumbersCount(6, …)`); Web K при восстановлении тоже 6 (`forgotPasswordLink.ts:~67`, `pages/cards/EmailRecoverCard.tsx:34`). iOS, tdesktop и Web K берут длину поля подтверждения из `EMAIL_UNCONFIRMED_X` (tdesktop `api/api_cloud_password.cpp:245-252`). Значит, код — 6 цифр и ответ — `EMAIL_UNCONFIRMED_6`.
- **Голый `EMAIL_UNCONFIRMED` запрещён:** Web K делает `+err.type.match(/^EMAIL_UNCONFIRMED_(\d+)/)[1]` и падает без числа (`tweb/src/components/sidebarLeft/tabs/2fa/email.tsx:71-72`).
- **`EMAIL_UNCONFIRMED_6` — успех вызова, остальное из него применено.** Android считает ошибку успехом установки пароля и открывает экран кода (`TwoStepVerificationSetupActivity.java:1985-1997`); iOS превращает её в успешный `.password(…, pendingEmail)` (`TelegramCore/…/TwoStepVerification.swift:202-213`); tdesktop вызывает `finishSet(consumer, codeLength)` (`api_cloud_password.cpp:244-252`).
- **Первый пароль с почтой не действует, пока почта не подтверждена.** Документация об этом молчит; вывод из клиентов (одно наблюдение не правило, но все три согласны):
  - iOS моделирует состояние «пароля нет, почта ждёт» — `TwoStepVerificationConfiguration.notSet(pendingEmail:)`, когда в `account.password` нет `current_algo`, но есть `email_unconfirmed_pattern` (`TwoStepVerification.swift:8-10, 20-24`); после первой установки с почтой переходит именно в него (`TwoStepVerificationUnlockController.swift:619-621`) и отменяет её без текущего пароля, `updateTwoStepVerificationPassword(currentPassword: nil, .none)`, то есть `flags.1`, `email = ""`, без `new_algo` (`TwoStepVerificationUnlockController.swift:774`, `SetupTwoStepVerificationControllerNode.swift:342`). Смена адреса на экране кода — снова первая установка с `currentPassword: ""` (`TwoFactorAuthDataInputScreen.swift:225, 719-733`); если бы пароль уже действовал, iOS сначала проверил бы пустой пароль через `account.getPasswordSettings` и упал (`TwoStepVerification.swift:121-127`).
  - Android на экране кода первой установки держит `has_password = false`, «Прервать» шлёт `account.updatePasswordSettings(inputCheckPasswordEmpty, flags.1, email = "")` (`TwoStepVerificationSetupActivity.java:267-279, 1809, 1837-1839, 1874`), а `has_password = true` ставит только после `account.confirmPasswordEmail` (`:1529-1562`).
  - Web K ветвится на `!has_password && email_unconfirmed_pattern` (`components/solidJsTabs/tabs.ts:1164`); tdesktop прерывает установку `account.cancelPasswordEmail` (`settings_cloud_password_email_confirm.cpp:89-93`, `api/api_cloud_password.cpp:109-126`).
- **iOS сразу перечитывает `account.getPassword`** и берёт оттуда `email_unconfirmed_pattern` (`TwoStepVerification.swift:210-212`); без него экрана кода нет (`PasswordSetupUI/Sources/TwoFactorAuthDataInputScreen.swift:236-252`). Web K открывает экран кода и шлёт `resendPasswordEmail`, если шаблон задан (`components/solidJsTabs/tabs.ts:1164-1171`).
- **Пустая почта с flags.1 удаляет почту** — это клиентское «пропустить» (Android `:1850`, Web K `email.tsx`).
- **«Забыли пароль».** Android в настройках предлагает почту, только если `has_recovery` и нет `pending_reset_date`, иначе — сброс (`TwoStepVerificationActivity.java:575-630`); при входе — только если `has_recovery`, иначе диалог «нет почты» со сбросом аккаунта (`LoginActivity.java:5682-5745`). tdesktop — так же по `hasRecovery` (`intro/intro_password_check.cpp:297-327`). iOS и Web K при входе зовут `auth.requestPasswordRecovery` всегда: iOS на любую ошибку показывает `TwoStepAuth_RecoveryUnavailable` (`AuthorizationUI/Sources/AuthorizationSequenceController.swift:965-993`), Web K на `PASSWORD_RECOVERY_NA` ведёт к сбросу (`pages/cards/PasswordCard.tsx:73-100`). «Отправить снова» в Web K — повторный `requestPasswordRecovery` (`emailConfirmation.tsx:118-121`).
- **Шаблон адреса.** Формат в документации не задан. Android прячет под спойлер всё между первой и последней `*` (`LoginActivity.java:5693-5701`); tdesktop и Web K считают строку с пробелом заглушкой, а не адресом (`settings_privacy_security.cpp:694`, `emailSetupSuggestion.ts:27-28`). Шаблон Pluma содержит `*` и не содержит пробелов; точная форма Telegram не проверена.
- **`auth.recoverPassword`** без `new_settings` снимает пароль (Web K всегда так, `lib/appManagers/passwordManager.ts:97-104`; Android «пропустить» — `LoginActivity.java:7778`, `TwoStepVerificationSetupActivity.java:323`); с `new_settings` ставит новый пароль, подсказку и, возможно, почту (Android `LoginActivity.java:7880-7890`, iOS `Authorization.swift:1384-1411`, tdesktop `api_cloud_password.cpp:483-488`). В невошедшей сессии возвращённый `auth.authorization` входит в аккаунт (iOS `Authorization.swift:1355`, Android `LoginActivity.java:7912`, tdesktop `intro_password_check.cpp:243`). Android при `SRP_ID_INVALID`/`NEW_SALT_INVALID` перечитывает `getPassword` и повторяет (`LoginActivity.java:7898-7906`).
- **`checkRecoveryPassword`.** Android идёт дальше только на `boolTrue` (`TwoStepVerificationSetupActivity.java:1487-1514`, `LoginActivity.java:7553-7575`); iOS разбирает `FLOOD_WAIT`, `PASSWORD_RECOVERY_EXPIRED`, остальное — «неверный код» (`Authorization.swift:1331-1345`); tdesktop на `PASSWORD_RECOVERY_NA`/`PASSWORD_RECOVERY_EXPIRED` возвращает на шаг назад, на `CODE_INVALID` пишет «неверный код» (`settings_cloud_password_email_confirm.cpp:313-343`).
- **Ошибки подтверждения.** iOS: `EMAIL_INVALID`, `CODE_INVALID`, `EMAIL_HASH_EXPIRED`, `FLOOD_WAIT` (`TelegramEngine/Auth/ConfirmTwoStepRecoveryEmail.swift:16-28`); tdesktop: `CODE_INVALID`, `EMAIL_HASH_EXPIRED`, flood (`settings_cloud_password_email_confirm.cpp:285-294`); Web K: `CODE_INVALID`, `EMAIL_HASH_EXPIRED` (`2fa/emailConfirmation.tsx:72-86`); Android: `CODE_INVALID*`, `FLOOD_WAIT` (`:1580-1590`). На `updatePasswordSettings` Android показывает `EMAIL_INVALID` и `EMAIL_NOT_ALLOWED` и разбирает `FLOOD_WAIT_X` (`:1999-2012`).
- **`cancelPasswordEmail`.** Android после него ставит `has_recovery = false` и чистит шаблон (`TwoStepVerificationSetupActivity.java:1811-1818`); Web K зовёт его для «сменить почту» (`emailConfirmation.tsx:108`).
- **Почта для входа (`login_email_pattern`, `sentCodeTypeSetUpEmailRequired`, `SETUP_LOGIN_EMAIL`) — отдельная функция.** Клиенты требуют её только по этим сигналам сервера (Android `LoginActivity.java:1965`, tdesktop `intro/intro_step.cpp:388`, Web K `emailSetupSuggestion.ts:7-35`); строка в настройках появляется только при `login_email_pattern` (tdesktop `settings_privacy_security.cpp:688`, Android `PrivacySettingsActivity.java:448`). Pluma их не шлёт — почта для входа не нужна.

### Сервер сегодня
- `pkg/twofa`: `Password` отдаёт `HasRecovery: false` (`password.go:34`), `Update` отклоняет непустую почту `EMAIL_INVALID` (`password.go:249`), `Settings` без `email` (`password.go:229`). Таблица `user_passwords` (`migrate-20261024-passwords.sql`). Ожидающий пароля вход — `twofa_login_<authKeyId>` в Redis (`twofa.go:100,156-198`).
- `app/bff/twofa/internal/server/grpc/service/not_served.go:30-46` — три метода `*PasswordEmail` отвечают `ErrEnterpriseIsBlocked`; `auth.requestPasswordRecovery`, `checkRecoveryPassword`, `recoverPassword` в `app/bff/authorization/internal/core` отвечают `PASSWORD_RECOVERY_NA`.
- Сессия уже пускает эти методы без входа (`app/interface/session/internal/sess/check_api_request_type.go:65-75,162-164`).
- Образец кодов — `pkg/logincode`: 6 цифр из `crypto/rand`, HMAC-SHA256 под `LOGIN_CODE_HMAC_KEY` с меткой домена, хэши в Redis, счётчик попыток в Lua, `Limiter` с правилами, «код хранится только после того, как отправитель его принял».
- Почтового кода в репозитории нет.

### Поставщик (документация Resend, прочитано 2026-11-12)
- `POST https://api.resend.com/emails`, `Authorization: Bearer`, обязателен `User-Agent` (без него 403/1010), поля `from`, `to`, `subject`, `text`, `html`, `reply_to`, `tags`; ответ `{"id"}`; заголовок `Idempotency-Key` живёт 24 ч (https://resend.com/docs/api-reference/emails/send-email).
- 10 запросов в секунду на команду; повторяемые ошибки — 429 `rate_limit_exceeded`/`daily_quota_exceeded`, 500, 503, 409 `concurrent_idempotent_requests`/`resource_locked`; остальные 4xx — постоянные (https://resend.com/docs/api-reference/errors).
- Отдельный поддомен рекомендуется; записи MX и TXT (SPF) на `send.<поддомен>`, TXT DKIM `resend._domainkey`; DMARC начинать с `p=none` (https://resend.com/docs/dashboard/domains/introduction, /dmarc).
- Регион задаёт только маршрут отправки; данные, метаданные и логи — в США; письма хранятся 30 дней, DPA по GDPR есть у каждого аккаунта (https://resend.com/docs/dashboard/domains/regions, https://resend.com/security/gdpr). **Код в теле письма лежит у Resend 30 дней** — поэтому код у нас короткоживущий и одноразовый.
- Ключи: `full_access` и `sending_access`, последний ограничивается одним доменом (https://resend.com/docs/dashboard/api-keys/introduction).
- Бесплатный план: 100 писем в день, 3000 в месяц (https://resend.com/pricing).

## Решение

### 1. Что обслуживается
- Почта восстановления облачного пароля целиком: задать, подтвердить, переслать код, отменить, восстановить пароль по коду — в вошедшей сессии и при входе после принятого кода телефона.
- `account.getPassword`: `has_recovery` — есть подтверждённая почта **и** почта включена на сервере; `email_unconfirmed_pattern` — пока есть ожидающая почта. `login_email_pattern` не задаётся.
- `account.getPasswordSettings` (после SRP) отдаёт подтверждённую почту полностью — её видит только владелец пароля.
- Почта существует только при пароле: она хранится в строке `user_passwords`; снятие пароля, `pluma-admin password reset`, удаление аккаунта (`Forget`) и завершённый 7-дневный сброс удаляют и её.

### 2. Код
- 6 десятичных цифр, `crypto/rand` (`logincode.Generate`).
- Хранится только HMAC-SHA256 под `LOGIN_CODE_HMAC_KEY` с меткой домена `pluma-email-code-v1` по полям длиной с префиксом: назначение (`setup`/`recovery`), `user_id`, адрес, для восстановления — `set_at` пароля, сам код. Код, выданный до смены адреса или пароля, перестаёт подходить сам собой.
- Redis, хэш `twofa_email_code_<purpose>_<user_id>`: `hash`, `code_id` (случайный, для ключа идемпотентности), `attempts`, `issued_at`; TTL = срок кода. На диск код не пишется никогда.
- Срок кода: **30 минут** для обоих назначений (`EmailCodeTTL`). Документация срока не называет; 30 минут хватает переключиться в почту и не держат код живым долго, при том что копия письма лежит у Resend 30 дней.
- Одна живая запись на назначение и пользователя: новый код заменяет старый.
- Неверный ввод увеличивает `attempts` (Lua, атомарно); на пятой ошибке (`MaxAttempts = 5`) код сгорает. Дальше: при подтверждении — `EMAIL_HASH_EXPIRED`, при восстановлении — `PASSWORD_RECOVERY_EXPIRED`; клиенты на них возвращают пользователя к повторной отправке.
- Код восстановления одноразовый: `auth.recoverPassword` забирает его скриптом «проверить и удалить» до изменения пароля, поэтому два параллельных вызова не применят его дважды. `auth.checkRecoveryPassword` код не тратит.

### 3. Пределы и `FLOOD_WAIT_X`
Правила `logincode.Rule` через `logincode.Limiter` (свои имена ключей), ответ — `FLOOD_WAIT_<секунды до конца окна>`:
- отправка, на пользователя и назначение: не чаще раза в 60 с; 5 в час; 10 в сутки;
- отправка, на ключ авторизации: 10 в час (невошедшая сессия не перебирает аккаунты);
- отправка, на весь сервер: 80 в сутки по UTC (`DailyLimit`, настраивается) — ниже 100 бесплатного плана Resend, чтобы один человек не исчерпал суточную квоту всем;
- ввод кода (подтверждение, проверка, восстановление вместе): на пользователя 10 в час и 30 в сутки, на ключ 10 в час; сверх — `FLOOD_WAIT_X`;
- тревожное правило: 20 неверных кодов на пользователя за сутки — строка лога «possible brute force» и счётчик (как `CheckCodeFailuresAlert` в `pkg/logincode`).
Минутный счётчик считается на пользователя, назначение и адрес (хэш адреса в ключе): новый адрес после опечатки получает письмо сразу. Попытка отправки, которую поставщик не принял, возвращает (`Refund`) только минутный счётчик пользователя, чтобы «отправить снова» сработало сразу. Часовой, суточный и ключевой счётчики остаются, глобальный тоже: иначе клиент, повторяющий запрос, пока поставщик падает (или отказывает его адресу), израсходовал бы суточный предел сервера на всех. Кроме того, сетевая ошибка и нечитаемый ответ 2xx не доказывают, что письмо не ушло.

### 4. Ход вызовов
- **`account.updatePasswordSettings` с `email`.** Порядок 008 сохраняется: проверка новых настроек (адрес: `net/mail.ParseAddress`, одно голое ASCII-имя без отображаемого имени, ≤ 254 байт, домен в нижнем регистре, иначе `EMAIL_INVALID`), проверка текущего пароля, запись пароля. Затем, если адрес новый (не совпадает с подтверждённым): адрес пишется в `email_unconfirmed` той же условной записью, что и пароль; код отправляется; ответ — **`EMAIL_UNCONFIRMED_6`** (никогда без числа), даже если отправка не удалась: запись уже сделана, а клиенты на любой другой ошибке считают вызов неудачным; «отправить снова» повторит письмо. Смена действующего пароля применяется сразу. **Первый пароль** с адресом записывается с `awaits_email = 1` и не действует до подтверждения (раздел «Клиенты»): `getPassword` отдаёт `has_password = false` без `current_algo`, но с `email_unconfirmed_pattern`; вход его не спрашивает; новый первый пароль (с адресом или без) заменяет ожидающий без проверки; `email = ""` без `new_algo` при ожидающем первом пароле удаляет его вместе с адресом и кодом (отмена установки Android и iOS), без ожидающего — `NEW_SETTINGS_EMPTY`, как раньше. Служебное сообщение о смене пароля для ожидающего первого пароля не отправляется — оно уходит при подтверждении. Тот же адрес, что подтверждён, — без письма, успех. Пустой адрес — удалить и подтверждённую, и ожидающую почту. Почта без пароля (ни текущего, ни нового) — `EMAIL_INVALID`; поведение Telegram здесь не проверено.
- **`account.confirmPasswordEmail(code)`.** Нет ожидающей почты или кода — `EMAIL_HASH_EXPIRED`; пустой код — `CODE_EMPTY`; неверный — `CODE_INVALID`; верный — `email = email_unconfirmed`, `email_unconfirmed = ''`, код удалён, `boolTrue`; ожидающий первый пароль вступает в силу той же условной записью (`awaits_email = 0`, `set_at` — момент подтверждения); служебное сообщение `TwoStepChanged` всем сессиям, как при смене пароля. Android шлёт этот вызов с `RequestFlagWithoutLogin` из вошедшей сессии; сервер определяет пользователя по ключу как обычно, флаг ничего не меняет.
- **`account.resendPasswordEmail()`.** Есть ожидающая почта — новый код и письмо (пределы раздела 3); нет — `EMAIL_HASH_EXPIRED`. `boolTrue`.
- **`account.cancelPasswordEmail()`.** Удаляет ожидающую почту и её код; ожидающий её первый пароль удаляется вместе с ней (так tdesktop прерывает установку); подтверждённая почта действующего пароля остаётся (Android после вызова локально ставит `has_recovery = false`, но перечитывает `getPassword` при следующем входе в экран; Telegram здесь не проверен — подтвердить). `boolTrue` и без ожидающей почты.
- **`auth.requestPasswordRecovery()`.** Пользователь — вошедший или ожидающий пароля по `twofa_login_<authKeyId>` (то есть принявший код телефона, как требует /api/srp); иначе `AUTH_KEY_UNREGISTERED`. Нет пароля — `PASSWORD_EMPTY`; нет подтверждённой почты или почта выключена — `PASSWORD_RECOVERY_NA`. Иначе код и письмо, ответ `auth.passwordRecovery{email_pattern}`. Поставщик не принял письмо — `FLOOD_WAIT_60` (клиенты его разбирают, а 500 tgnet повторяет сам), счётчик и строка лога.
- **`auth.checkRecoveryPassword(code)`.** `CODE_EMPTY`; нет кода — `PASSWORD_RECOVERY_EXPIRED`; неверный — `CODE_INVALID` (и попытка списывается); верный — `boolTrue`, код остаётся.
- **`auth.recoverPassword(code, new_settings?)`.** Код забирается (раздел 2). Удаление или замена пароля — условная запись по прочитанной строке: если пароль или почту успели сменить после чтения, ответ `PASSWORD_RECOVERY_EXPIRED` (код к новому паролю всё равно не подошёл бы), а новая смена остаётся. Без `new_settings` — пароль и почта удаляются. С `new_settings` — проверка как в `updatePasswordSettings` (`NEW_SETTINGS_INVALID`, `NEW_SALT_INVALID`, `SRP_ID_INVALID` по тем же правилам), новый пароль и подсказка; подтверждённая почта остаётся, если `email` не передан или совпадает; другой адрес становится ожидающим с письмом-кодом, а ответ остаётся успешным (`auth.authorization` нельзя заменить на `EMAIL_UNCONFIRMED_6`; клиент увидит `email_unconfirmed_pattern` в `getPassword`). Во всех случаях снимаются `pending_reset_date` (как `Put`) и `twofa_new_algo_`. В невошедшей сессии — проверка модерации (`PhoneBanned`) и вход через тот же `logIn`, что у `auth.checkPassword`, затем `EndLogin`; в вошедшей — `auth.authorization` текущего пользователя.

### 5. Сессии после восстановления
- Документация и прочитанные клиенты не говорят, что Telegram делает с другими сессиями (не проверено).
- Решение: другие сессии **не завершаются** — человек, забывший пароль, обычно держит в руках другие свои устройства, а для входа без сессии восстановление и так требует кода телефона (/api/srp). Все сессии, включая новую, получают служебное сообщение 777000 о том, что пароль сброшен по почте восстановления, с устройством и местом (новый вид `servicetext`, как `TwoStepChanged`; ADR 0026), и в нём — путь к «Устройствам».
- Ключ не помечается как «свежий» иначе, чем при обычном входе.

### 6. Хранение (`migrate-20261116-password-email.sql`)
- Колонки `user_passwords`, добавляются идемпотентно через `information_schema` (как `migrate-20261114-monoforum.sql`): `email varchar(254) NOT NULL DEFAULT ''`, `email_unconfirmed varchar(254) NOT NULL DEFAULT ''`, `email_set_at bigint NOT NULL DEFAULT 0`, `awaits_email tinyint(1) NOT NULL DEFAULT 0` (первый пароль, ждущий подтверждения почты).
- Адрес хранится открыто: его нужно отправлять. Кода в MySQL нет. Колонки в той же строке, что пароль, поэтому `Delete`, `Forget`, `Remove` и `FinishReset` удаляют почту без отдельного шага.
- Каждое изменение запроса — одна условная запись `Store` по прочитанной строке, так что запрос не затирает изменение, сделанное после его чтения: `SwapEmail(user, set_at, было, стало)` — задать ожидающую почту, подтвердить, отменить, удалить у действующего пароля (условие: пароль тот же, почта та же, `awaits_email = 0`); `Replace(было, стало)` — пароль вместе с почтой и `awaits_email` (`updatePasswordSettings` с новым паролем, `auth.recoverPassword`, подтверждение первого пароля; без строки — вставка, только если строки нет); `RemoveIf(было)` — удаление пароля (`updatePasswordSettings`, `auth.recoverPassword`, отмена установки). Условие `Replace` и `RemoveIf`: те же `set_at`, `verifier`, `email`, `email_unconfirmed`, `awaits_email`. Несработавшее условие — `SRP_PASSWORD_CHANGED` (клиент перечитывает `getPassword`), у восстановления — `PASSWORD_RECOVERY_EXPIRED`. `Put` и `Delete` безусловны и остаются для инструментов, тестов, `Forget` и `pluma-admin`.

### 7. Отправка: пакет `pkg/mail`
- Интерфейс `Sender { Send(ctx, Message) (id string, err error) }`, `Message{To, Subject, Text, HTML, IdempotencyKey, Tag}`; ошибка `*Error{Code, Retryable}`.
- Первая реализация — Resend по HTTPS (`resend.go`): `User-Agent: pluma-server`, `Idempotency-Key: pluma/<purpose>/<user_id>/<code_id>`, теги `type=recovery_code|email_confirm`, `LimitReader` на ответе, разбор `{name, message}` в `*Error` по таблице раздела «Поставщик».
- Повторы — у вызывающего: до 3 попыток с паузой 0,5 и 1,5 с только для `Retryable`, всё под общим сроком 8 с; ключ идемпотентности тот же, поэтому двух разных писем не будет.
- Код кладётся в Redis только после того, как поставщик принял письмо.
- Замена поставщика — новая реализация интерфейса и значение `Mail.Provider`; SMTP-реализация не делается сейчас (раздел «Что не делается»).
- Почта выключена, когда `Mail.ApiKey` пуст: `has_recovery` ложно, `updatePasswordSettings` с непустой почтой — `EMAIL_INVALID` (как сегодня), `requestPasswordRecovery` — `PASSWORD_RECOVERY_NA`; клиенты уходят на сброс через 7 дней. Ключ без `From` — ошибка запуска (`ErrMailMisconfigured`), как `ErrSenderMisconfigured`.

### 8. Отправитель и домен
- Домен `pluma.chat`, отправитель `Pluma <noreply@pluma.chat>`, без `Reply-To` (решение владельца 2026-11-12). Resend советует слать с поддомена, чтобы репутация писем не задевала основной домен; владелец выбрал корневой домен: писем мало (десятки в месяц), почтовых записей у `pluma.chat` нет (2026-11-12: ни MX, ни SPF, ни DMARC; DNS — Cloudflare), записи Resend ложатся на `send.pluma.chat` и `resend._domainkey.pluma.chat` и корень не меняют.
- Регион Resend — `eu-west-1` (Ирландия): ближе к серверу; хранение данных у Resend всё равно в США, письма — 30 дней. Владелец принял (2026-11-12).
- Трекинг открытий и кликов выключен, ссылок в письме нет, картинок и вложений нет.

### 9. Тексты письма
- Язык — `lang_code` сессии: `c.MD.GetLangCode()` в вошедшей сессии, `Login.LangCode` для ожидающего пароля входа.
- Тексты — ключи в новом встроенном файле `pkg/langpack/mail.json` той же формы, что `own.json` без уровня пакета (`{key: {lang: text}}`), с тем же проверяющим загрузчиком и функцией `langpack.MailText(key, lang)`: точный язык, затем базовый (`pt-br` → `pt`, нет — дальше), затем английский. Тест требует каждый ключ во всех 37 языках `own.json` (ar … zh-hant).
- Ключи: `PlumaMailConfirmSubject`, `PlumaMailConfirmBody`, `PlumaMailRecoverySubject`, `PlumaMailRecoveryBody`, `PlumaMailIgnore`, `PlumaMailValidFor` и строки служебного сообщения о восстановлении. Тело — имя продукта, код отдельной строкой, срок «30 минут», фраза «если это были не вы — ничего не делайте, пароль не изменится». Текст и простой HTML (одна таблица со встроенным CSS), меньше 102 КБ.
- В письме нет телефона, имени аккаунта и устройства.

### 10. Шаблон адреса
- Первые два символа локальной части (один, если она короче четырёх), `***`, `@`, домен целиком: `ab***@gmail.com`. Есть `*`, нет пробелов (раздел «Клиенты»). Совпадение с форматом Telegram не проверено.

### 11. Что пишется в лог
- `user_id`, назначение, `code_id`, исход, `id` письма у Resend, код ошибки поставщика, длительность.
- Никогда: код, хэш кода, адрес целиком и шаблон, тело письма, ключ API. `tools/logaudit` получает имена `email`, `mail`, `emailunconfirmed`, `apikey`, `mailapikey`, чтобы сборка падала на их выводе.

### 12. Метрики и алерты
- `password_email_send_total{purpose, result}`: `ok`, `retryable`, `permanent`, `limited`.
- `password_email_code_total{purpose, result}`: `ok`, `invalid`, `expired`, `limited`.
- Алерты в `teamgramd/deploy/prometheus/server/rules/codes.yml` и строки в `docs/ops/monitoring.md`:
  - `PasswordEmailSenderFailing`: `sum(increase(password_email_send_total{result=~"retryable|permanent"}[15m])) > 2`;
  - `PasswordEmailSenderRejected`: `sum(increase(password_email_send_total{result="permanent"}[1h])) > 0` — отозванный ключ, неподтверждённый домен, кончилась квота месяца;
  - `PasswordEmailBruteForce`: `sum(increase(password_email_code_total{result="invalid"}[1h])) > 20`.

### 13. Конфигурация и оператор
- Раздел `Mail` в `teamgramd/etc/bff.yaml`, `teamgramd/etc2/bff.yaml`, `app/bff/bff/etc/bff.yaml` и `config.go` модулей bff и twofa: `Provider: resend`, `ApiKey: "${MAIL_API_KEY}"`, `From: "${MAIL_FROM}"`, `ReplyTo` (`,optional`), `Endpoint` (`,optional`, для тестового стенда), `DailyLimit` (`,optional`, 80).
- `MAIL_API_KEY`, `MAIL_FROM` — в `.env.example` и `docker-compose.yaml` (`TestDeployConfigVariablesAreProvided`). Ключ — `sending_access`, только на `pluma.chat`.
- `docs/ops/email.md` (новый): записи DNS, ключ, проверка, ротация, что видно в метриках; `docs/ops/security-setup.md` — ротация ключа; `pluma-admin` help и `docs/ops/security-setup.md` — про почту и сброс.

## Последствия
- Пароль можно вернуть без администратора и без 7 дней ожидания, если человек задал почту.
- Сервер зависит от внешнего поставщика только в этой функции; без ключа она выключена, остальное работает как сегодня.
- Адреса почты появляются в MySQL и в бэкапах: это персональные данные, их видят только владелец (после SRP) и оператор БД.
- Код восстановления 30 дней лежит у Resend (в США). Это цена поставщика; защищает короткий срок и одноразовость кода у нас.
- Тексты 37 языков нужно поддерживать вместе с остальными строками.

## Что не делается
- Почта для входа (`login_email_pattern`, `account.sendVerifyEmailCode`, `account.verifyEmail`, `auth.resetLoginEmail`, `sentCodeTypeSetUpEmailRequired`) — клиенты её не требуют, пока сервер не просит; методы остаются `ErrEnterpriseIsBlocked`.
- Почта Telegram Passport (`emailVerifyPurposePassport`).
- SMTP-реализация и вебхуки Resend (отказы и жалобы): доставку доказывает сам код подтверждения; добавляются, когда понадобятся, без изменения интерфейса.
- Письмо на старый адрес при смене почты и письмо о сбросе пароля на почту — Telegram здесь не проверен; уведомление идёт служебным сообщением.

## Рассмотренные альтернативы
- **Отдельный сервис-отправитель (как `app/ops/codes`, ADR 0044).** Не нужен: Resend — обычный HTTPS, у bff уже есть выход в интернет (`docs/ops/deployment.md:185`).
- **Свой Postfix.** Репутация нового IP и обратная зона у хостера — долгий путь для 100 человек; интерфейс оставляет эту дорогу открытой.
- **Код в MySQL.** Пережил бы перезапуск Redis, но код живёт 30 минут; потеря Redis означает «отправьте снова», а код на диске и в бэкапах — лишний риск.
- **Тексты в `servicetext`.** Там только en/ru; требование — все языки клиентов.
- **Завершать другие сессии при восстановлении.** Отвергнуто в разделе 5, вынесено владельцу на подтверждение.

## Решения владельца (2026-11-12)
1. Другие сессии после восстановления не завершаются, всем устройствам приходит служебное сообщение 777000 — «так и надо».
2. Хранение писем у Resend в США 30 дней — «ничего страшного».
3. Отправитель `noreply@pluma.chat` на корневом домене, `Reply-To` нет.

## Что осталось подтвердить владельцу
1. Срок кода 30 минут, пределы раздела 3, общий суточный предел 80.
2. `cancelPasswordEmail` оставляет подтверждённую почту.
