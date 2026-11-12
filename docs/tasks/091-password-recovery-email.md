# 091. Почта восстановления облачного пароля (Resend)

- Статус: done (2026-11-12; замечания независимого чтения исправлены, проверки зелёные)
- Решение: ADR 0058.
- Зависит от: 008 (2FA по SRP), 073 (`pkg/logincode`: HMAC, пределы, образец отправителя), 076 / ADR 0046 (37 языков строк).
- Ветка: `task/091-password-recovery-email`.

## Цель
Человек, задавший почту восстановления, возвращает облачный пароль сам — из настроек или при входе после кода телефона — во всех четырёх клиентах так же, как в Telegram. Письма уходят через Resend за узким интерфейсом; без ключа функция выключена и всё работает как сегодня (сброс через 7 дней, `pluma-admin password reset`).

## Область
В задаче:
- `account.updatePasswordSettings` с `new_settings.email` (flags.1): ожидающая почта, письмо, `EMAIL_UNCONFIRMED_6`; пустая — удалить почту.
- `account.confirmPasswordEmail`, `account.resendPasswordEmail`, `account.cancelPasswordEmail` (сейчас `ErrEnterpriseIsBlocked` в `not_served.go:30-46`).
- `auth.requestPasswordRecovery`, `auth.checkRecoveryPassword`, `auth.recoverPassword` — в вошедшей сессии и при входе (ключ в `twofa_login_`).
- `account.getPassword` (`has_recovery`, `email_unconfirmed_pattern`), `account.getPasswordSettings` (`email`).
- Пакет `pkg/mail` (интерфейс и Resend), тексты `pkg/langpack/mail.json` на 37 языках, служебное сообщение о восстановлении.
- Миграция, конфигурация, метрики, алерты, `logaudit`, `docs/ops/email.md`, тестовый поставщик в e2e-стенде.

Не в задаче (ADR 0058, «Что не делается»): почта для входа (`login_email_pattern`, `sendVerifyEmailCode`, `verifyEmail`, `resetLoginEmail`) — остаётся `ErrEnterpriseIsBlocked`; Passport; SMTP; вебхуки Resend.

## Контракт клиентов
Источники — закреплённые выпуски в `~/pluma-work/reference-src-new` и core.telegram.org, подробно с номерами строк — ADR 0058, «Контекст». Главное:
- Код — 6 цифр: Android `TwoStepVerificationSetupActivity.java:122`, `LoginActivity.java:7387`; Web K `forgotPasswordLink.ts:~67`.
- Ответ на новую почту — только `EMAIL_UNCONFIRMED_6`, пароль из того же вызова уже применён: Android `TwoStepVerificationSetupActivity.java:1985-1997`, iOS `TwoStepVerification.swift:202-213`, tdesktop `api_cloud_password.cpp:244-252`; голый `EMAIL_UNCONFIRMED` ломает Web K (`2fa/email.tsx:71-72`).
- iOS сразу перечитывает `getPassword` и без `email_unconfirmed_pattern` экрана кода не покажет (`TwoStepVerification.swift:210-212`).
- «Забыли пароль» Android и tdesktop предлагают только при `has_recovery` (`LoginActivity.java:5682-5745`, `intro_password_check.cpp:297-327`); iOS и Web K зовут `requestPasswordRecovery` всегда и на `PASSWORD_RECOVERY_NA` уходят к сбросу (`AuthorizationSequenceController.swift:965-993`, `PasswordCard.tsx:73-100`).
- `checkRecoveryPassword`: дальше только на `boolTrue` (Android `:1487-1514`); `CODE_INVALID`, `PASSWORD_RECOVERY_EXPIRED`, `FLOOD_WAIT` разбирают все.
- `recoverPassword` без настроек снимает пароль (Web K `passwordManager.ts:97-104`), с настройками ставит новый; в невошедшей сессии ответ входит в аккаунт (Android `LoginActivity.java:7912`, iOS `Authorization.swift:1355`, tdesktop `intro_password_check.cpp:243`).
- Шаблон адреса — со `*` (спойлер Android `LoginActivity.java:5693-5701`) и без пробелов (tdesktop `settings_privacy_security.cpp:694`).
- Не проверено (документации нет): срок кода, пределы, что Telegram делает с сессиями после восстановления, судьба подтверждённой почты при `cancelPasswordEmail`. Решения — ADR 0058, разделы 2, 3, 5, 4.

## Порядок работ
1. Миграция и `Store` (MySQL и `memstore`), новые условные записи.
2. `pkg/mail`: интерфейс, Resend, разбор ошибок, тесты на `httptest`.
3. Коды в `pkg/twofa`: выпуск, хэш, Lua попыток и «проверить и забрать», пределы.
4. `pkg/langpack/mail.json` и `MailText`; тексты на 37 языках (вычитка как у строк 076).
5. `pkg/twofa`: `Password`, `Settings`, `Update`, новые `ConfirmEmail`, `ResendEmail`, `CancelEmail`, `RequestRecovery`, `CheckRecovery`, `Recover`.
6. Обработчики bff twofa и authorization, служебное сообщение.
7. Конфигурация, `.env.example`, compose, метрики, алерты, `logaudit`.
8. Тестовый поставщик `test/e2e/fakemail`, e2e.
9. Документы: `docs/ops/email.md`, `security-setup.md`, `monitoring.md`, help `pluma-admin`, доккомментарии «почты нет».

## Файлы
Добавить:
- `teamgramd/deploy/sql/migrate-20261116-password-email.sql` — три колонки `user_passwords`, идемпотентно через `information_schema`.
- `pkg/mail/mail.go` (интерфейс, `Message`, `Error`), `pkg/mail/resend.go`, `pkg/mail/config.go`, тесты.
- `pkg/twofa/email.go` (поток почты), `pkg/twofa/emailcode.go` (коды, Lua, пределы), `pkg/twofa/pattern.go` (шаблон адреса), тесты.
- `pkg/langpack/mail.json`, `pkg/langpack/mail.go`, `mail_test.go`.
- `app/bff/twofa/internal/core/account.confirmPasswordEmail_handler.go`, `account.resendPasswordEmail_handler.go`, `account.cancelPasswordEmail_handler.go`.
- `test/e2e/fakemail/` (`main.go`, `Dockerfile`): принимает запросы как Resend (проверяет `Bearer` и `User-Agent`), отдаёт тестам последние письма на loopback-порту, умеет отвечать запланированными ошибками.
- `test/e2e/password_email_test.go`.
- `docs/ops/email.md`.

Изменить:
- `pkg/twofa/twofa.go` (доккомментарий пакета, конфиг почты), `password.go` (`Password`, `Settings`, `Update`), `store.go`, `memstore.go`, `redis.go` (ключи `twofa_email_code_`).
- `app/bff/twofa/internal/core/account.getPassword_handler.go`, `account.updatePasswordSettings_handler.go`, `account.getPasswordSettings_handler.go`; `internal/server/grpc/service/not_served.go` (убрать три метода); `internal/svc/service_context.go`, `internal/config/config.go`.
- `app/bff/authorization/internal/core/auth.requestPasswordRecovery_handler.go`, `auth.checkRecoveryPassword_handler.go`, `auth.recoverPassword_handler.go`; svc и config модуля; `servicetext` — сообщение о восстановлении (тексты en/ru там, как у соседей; письмо — через `mail.json`).
- `app/bff/bff/internal/config/config.go`, `app/bff/bff/etc/bff.yaml`, `teamgramd/etc/bff.yaml`, `teamgramd/etc2/bff.yaml`, `teamgramd/etc/admin.yaml` (если `pluma-admin` строит `twofa.Service`).
- `.env.example`, `docker-compose.yaml` (`MAIL_API_KEY`, `MAIL_FROM`); `test/e2e/docker-compose.e2e.yaml`, `test/e2e/run.sh` (сервис `fakemail`, `Mail.Endpoint` на него).
- `tools/logaudit/logaudit.go` (`email`, `mail`, `emailunconfirmed`, `apikey`, `mailapikey`).
- `teamgramd/deploy/prometheus/server/rules/codes.yml`, `docs/ops/monitoring.md`.
- `tools/pluma-admin/main.go` (help: сброс удаляет и почту), `docs/ops/security-setup.md`.
- `app/interface/session/internal/sess/check_api_request_type.go` — только проверить, что методы уже в списке; не менять без нужды.

## Тесты
Модульные:
- `pkg/mail`: запрос к `httptest` (метод, путь, заголовки `Authorization`, `User-Agent`, `Idempotency-Key`, тело); разбор каждой ошибки таблицы Resend в `Retryable`/постоянную; обрезанный и не-JSON ответ; срок контекста.
- `pkg/twofa`: шаблон адреса (короткие и длинные локальные части, есть `*`, нет пробелов); проверка адреса; хэш кода зависит от назначения, адреса, `set_at`; повторы отправки с тем же ключом идемпотентности и только на `Retryable`; код кладётся только после принятого письма; `Update` с новой почтой применяет пароль и отвечает `EMAIL_UNCONFIRMED_6` и при отказе поставщика; пустая почта удаляет обе; почта без пароля — `EMAIL_INVALID`; выключенная почта — `has_recovery` ложно и `PASSWORD_RECOVERY_NA`.
- `pkg/langpack`: `mail.json` загружается, каждый ключ во всех 37 языках `own.json`, запасной язык и английский.
- `tools/logaudit` ловит вывод адреса и ключа.

Интеграционные (`pkg/testinfra`, настоящие MySQL и Redis):
- миграция дважды подряд; `Store` — условные записи почты под гонкой двух обработчиков;
- пять неверных кодов сжигают код; `EMAIL_HASH_EXPIRED`/`PASSWORD_RECOVERY_EXPIRED` после истечения TTL;
- два параллельных `Recover` одним кодом — применяется один;
- пределы: 60 с между письмами, часовой и суточный, общий суточный; `Refund` при отказе поставщика;
- `Forget`, `Remove`, `FinishReset` удаляют почту.

Сквозные (`pkg/mtclient`, стенд с `fakemail`, сессия после `getState`):
- `TestPasswordEmailSetup`: пароль с почтой → `EMAIL_UNCONFIRMED_6`; `getPassword` — `has_password`, `email_unconfirmed_pattern`; код из `fakemail` → `confirmPasswordEmail` → `has_recovery`; `getPasswordSettings` отдаёт адрес; пришло служебное сообщение.
- `TestPasswordEmailResendCancel`: `resendPasswordEmail` даёт новый код, старый не подходит; `FLOOD_WAIT_X` раньше 60 с; `cancelPasswordEmail` убирает шаблон.
- `TestPasswordRecoveryLoggedOut`: код телефона → `SESSION_PASSWORD_NEEDED` → `requestPasswordRecovery` (шаблон) → `checkRecoveryPassword` (`CODE_INVALID`, затем `boolTrue`) → `recoverPassword` с новым паролем → `auth.authorization`; вход новым паролем; другая сессия жива и получила служебное сообщение.
- `TestPasswordRecoveryLoggedIn`: `recoverPassword` без настроек снимает пароль и почту.
- `TestPasswordRecoveryRefusals`: без кода телефона — отказ; без почты — `PASSWORD_RECOVERY_NA`; `fakemail` отвечает 503 — `FLOOD_WAIT_60`, код не выдан; отвечает 401 — тот же ответ и счётчик `permanent`.
- Хотя бы один тест проверить падающим без изменения (например, `EMAIL_UNCONFIRMED_6` на текущем `master` даёт `EMAIL_INVALID`).

## Проверки перед сдачей (CachyOS)
- `go build ./...`; `go vet -tags integration,e2e -unreachable=false` затронутых пакетов; `go test` затронутых и импортирующих их, затем `-race`.
- Интеграционные `pkg/twofa`, `pkg/mail` по одному пакету за раз; e2e из раздела «Тесты» со своими `E2E_PROJECT`/`E2E_DIR`, затем `test/e2e/run.sh down`.
- `make lint`, `go test ./tools/...` (log audit), `go test ./pkg/conf/...` (переменные в `.env.example` и compose), `go run ./tools/apicoverage`.
- На машине разработки ничего не собирать (сборка — только на CachyOS).

## Проверки для `docs/project/TESTING-BACKLOG.md`
- [ ] Android, iOS, Desktop, Web: задать пароль с почтой, письмо пришло на языке клиента, код подтверждает почту; «Почта восстановления» видна в настройках.
- [ ] Повторная отправка кода и отмена ожидающей почты в каждом клиенте; смена почты на другую.
- [ ] Выйти, войти по коду телефона, «Забыли пароль?» → письмо → код → новый пароль; вход прошёл, другие устройства остались и получили сообщение 777000.
- [ ] То же с «пропустить» (пароль снят) на Android и Web.
- [ ] Неверный код пять раз — код сгорает, клиент предлагает отправить снова; частая отправка — «подождите».
- [ ] Шаблон адреса при входе (Android — спойлер) и строка «нет почты» у аккаунта без почты.
- [ ] Письмо не в спаме у Gmail и Яндекса; заголовки `dkim=pass`, `spf=pass`, `dmarc=pass`.
- [ ] Русский, английский и один язык с письмом справа налево (арабский) в письме.

## Шаги владельца
1. Завести аккаунт на resend.com (бесплатный план). Это решение владельца: аккаунт, данные о письмах у Resend хранятся в США (ADR 0058).
2. Domains → Add Domain → `pluma.chat`, регион `eu-west-1`.
3. У регистратора `pluma.chat` добавить записи ровно из вкладки Records (значения брать оттуда, не отсюда):
   - MX `send` → `feedback-smtp.eu-west-1.amazonses.com`, приоритет 10;
   - TXT `send` → `v=spf1 include:amazonses.com ~all`;
   - TXT `resend._domainkey` → `p=…` (ключ из панели);
   - TXT `_dmarc` → `v=DMARC1; p=none; rua=mailto:<адрес, который читается>;` — позже `p=quarantine`, затем `p=reject`, когда письма идут с `dmarc=pass`.
   - Остальные записи `pluma.chat` не трогать (почтовых записей у корня нет, 2026-11-12). DNS — Cloudflare: у новых записей «DNS only», без прокси.
4. Дождаться «Verified» (обычно ~15 минут, до 72 ч).
5. Settings → Click/Open tracking для домена — выключить.
6. API Keys → Create: имя `pluma-server`, Permission `Sending access`, Domain `pluma.chat`. Ключ показывается один раз — сразу в `.env` на сервере, никуда больше.
7. В `.env` на сервере:
   ```
   MAIL_API_KEY=re_…
   MAIL_FROM=Pluma <noreply@pluma.chat>
   ```
   и перезапустить `bff` (`docs/ops/email.md`).
8. Проверка: задать себе почту восстановления в любом клиенте; дальше — строки `TESTING-BACKLOG`.

## Решения, требующие подтверждения владельцем
- Подтверждено владельцем 2026-11-12: другие сессии после восстановления не завершаются, приходит сообщение 777000; письма у Resend в США 30 дней допустимы; отправитель `noreply@pluma.chat` на корневом домене, без `Reply-To`; регион `eu-west-1`.
1. Срок кода 30 минут; 5 неверных попыток на код; письма не чаще раза в 60 с, 5 в час, 10 в сутки на человека; 80 в сутки на весь сервер.
2. `cancelPasswordEmail` оставляет уже подтверждённую почту, убирает только ожидающую.
3. Сбой отправки при восстановлении отвечает `FLOOD_WAIT_60`, а при задании почты — всё равно `EMAIL_UNCONFIRMED_6` (настройки уже записаны, «отправить снова» повторит).
4. Первый пароль с почтой не действует до подтверждения почты (вывод из клиентов, документация молчит; ADR 0058, «Клиенты»). Письмо, которое поставщик не принял, возвращает только минутный счётчик.

## Результат
Почта восстановления облачного пароля работает целиком, как в ADR 0058: `account.updatePasswordSettings` с почтой отвечает `EMAIL_UNCONFIRMED_6` (смена действующего пароля применяется сразу, первый пароль вступает в силу с подтверждением почты), письмо с 6-значным кодом уходит через Resend (`pkg/mail`), `account.confirmPasswordEmail`/`resendPasswordEmail`/`cancelPasswordEmail` обслуживаются, `account.getPassword` отдаёт `has_recovery` и `email_unconfirmed_pattern`, `account.getPasswordSettings` — адрес; `auth.requestPasswordRecovery`, `checkRecoveryPassword`, `recoverPassword` работают в вошедшей сессии и при входе после кода телефона (вход через тот же `logIn`, что у `auth.checkPassword`), всем сессиям приходит сообщение 777000, другие сессии не завершаются. Без `MAIL_API_KEY` почта выключена и всё ведёт себя как раньше. Код живёт 30 минут, в Redis только HMAC, 5 неверных вводов сжигают код, код восстановления одноразовый и привязан к паролю; пределы и `FLOOD_WAIT_X` — по разделу 3 ADR.

Что сделано:
- `pkg/mail` (интерфейс `Sender`, `Message`, `Error{Code, Status, Retryable}`, `Config`, реализация Resend с `User-Agent`, `Idempotency-Key`, тегами и разбором ошибок по таблице Resend).
- `pkg/twofa`: `email.go` (поток почты), `emailcode.go` (коды, HMAC с меткой `pluma-email-code-v1`, Lua «проверить/забрать», пределы, повторы 0,5 и 1,5 с под сроком 8 с, письмо), `pattern.go` (проверка адреса, шаблон `ab***@домен`); `Store` — `Replace`, `RemoveIf` и `SwapEmail` (одна условная запись по прочитанной строке; ADR раздел 6); `Update` → `UpdateIn` с языком сессии.
- Миграция `migrate-20261116-password-email.sql` (четыре колонки, с `awaits_email`; идемпотентно).
- Тексты: `pkg/langpack/mail.json` (7 ключей × 37 языков), `langpack.MailText`, `MailRTL`; сообщение 777000 о сбросе — `servicetext.PasswordRecovered` по ключу `PlumaPasswordRecoveredMessage`.
- Обработчики bff twofa (три новых, `not_served.go` удалён) и authorization (три переписаны), конфиг `Mail` в bff, twofa, authorization и в `teamgramd/etc`, `teamgramd/etc2`, `app/bff/bff/etc`; `MAIL_API_KEY`, `MAIL_FROM` в `.env.example`, `docker-compose.yaml` (bff и migrate), `pkg/conf`.
- Метрики `password_email_send_total`, `password_email_code_total`; алерты `PasswordEmailSenderFailing`, `PasswordEmailSenderRejected`, `PasswordEmailBruteForce` (группа `password_email` в `codes.yml`), строки в `docs/ops/monitoring.md`.
- `tools/logaudit`: имена `email`, `emailunconfirmed`, шаблоны, `mail`, `apikey`, `mailapikey` с тестами.
- e2e: `test/e2e/fakemail` (как Resend: Bearer, User-Agent, идемпотентность, запланированные ошибки), сервис в стенде, `test/e2e/password_email_test.go` (5 тестов брифа), `TestTwoFa` поправлен под стенд с почтой.
- Документы: `docs/ops/email.md`, `security-setup.md`, `monitoring.md`, help `pluma-admin`, строки `TESTING-BACKLOG`.

Проверки (CachyOS, 2026-11-12): `go build ./...` — ок; `go vet -tags integration,e2e -unreachable=false` затронутых пакетов и `./test/e2e/...` — ок; `go test ./app/... ./pkg/... ./tools/...` — без падений; `-race` затронутых пакетов — ок; интеграционные `pkg/twofa` (MySQL, Redis: миграция дважды, гонка подтверждений, параллельные `Recover`, сгорание кода, удаление почты с паролем) — ок; e2e на своём стенде (`E2E_PROJECT=mail`): `TestPasswordEmail*`, `TestPasswordRecovery*`, `TestTwoFa*`, `TestNewLogin` — ок, в логах bff нет адресов, стенд снят, образы удалены; `golangci-lint` v2.15.0 по затронутым пакетам — замечаний в изменённых файлах нет (`make lint` целиком на CachyOS не годится: в копии нет git для `new-from-rev`); проверено, что `TestSetUpRecoveryEmail` и `TestRules` падают без изменения. `go run ./tools/apicoverage` — +3 метода (`account.*PasswordEmail`).

Решения, требующие подтверждения: срок кода 30 минут и пределы раздела 3; `cancelPasswordEmail` оставляет подтверждённую почту; сбой отправки — `FLOOD_WAIT_60` при восстановлении и повторной отправке, `EMAIL_UNCONFIRMED_6` при задании почты; при `recoverPassword` с новой почтой она ждёт кода (ответ успешный). Тексты письма и сообщения 777000 на 37 языках — свои, вычитку носителями не проходили. Не проверено: метрика `permanent` в e2e (проверены ответ и отсутствие письма), правила Prometheus через `promtool`.

Исправления по независимому чтению (2026-11-12):
- **Первая установка пароля с почтой.** Раньше пароль включался сразу, и экран кода первой установки ломался: «Прервать» на Android (`inputCheckPasswordEmpty`, `email = ""`) получал `PASSWORD_HASH_INVALID`, смена адреса на iOS проверяла пустой пароль и показывала неизвестную ошибку. Все три клиента моделируют состояние «пароля нет, почта ждёт» (iOS `TwoStepVerification.swift:8-10, 20-24`, `TwoStepVerificationUnlockController.swift:619-621, 774`; Android `TwoStepVerificationSetupActivity.java:1529-1562, 1809, 1837-1839, 1874`; Web K `tabs.ts:1164`), поэтому теперь первый пароль с адресом хранится с `awaits_email = 1` и вступает в силу только с `account.confirmPasswordEmail` (`set_at` — момент подтверждения). До этого `getPassword` отдаёт `has_password = false` с шаблоном адреса, вход пароль не спрашивает; новый первый пароль заменяет ожидающий; `email = ""` без `new_algo` и `account.cancelPasswordEmail` (tdesktop) удаляют его. Сообщение 777000 о смене уходит при подтверждении, а не при установке. Документация этого не описывает — вывод из клиентов (ADR 0058, «Клиенты», п. 4).
- **Пределы при сбое отправки.** Возвращается только минутный счётчик пользователя; часовой, суточный, ключевой и общий остаются, так что клиент, повторяющий запрос при падающем поставщике, не исчерпает суточный предел сервера.
- **Гонки записей.** Все записи запросов условны по прочитанной строке (`Replace`, `RemoveIf`; `PutWithEmail` убран): восстановление, опоздавшее к смене пароля, отвечает `PASSWORD_RECOVERY_EXPIRED` и не затирает новый пароль; смена пароля с почтой не откатывает подтверждение, сделанное между чтением и записью (`SRP_PASSWORD_CHANGED`).

Проверки исправлений (CachyOS): `go build ./...` — ок; `gofmt` — чисто; `go vet -tags integration,e2e -unreachable=false` для `pkg/twofa`, `app/bff/twofa`, `app/bff/authorization`, `test/e2e` — ок; `go test` затронутых пакетов и всех импортёров `pkg/twofa` — ок, `-race` — ок; `go test ./tools/...` — ок; интеграционные `pkg/twofa` с `-race` (MySQL, Redis; `TestMySQLStoreEmail` проверяет условия `Replace`/`RemoveIf`, `awaits_email` и параллельную вставку) — ок; e2e на своём стенде (`E2E_PROJECT=mailfix`): `TestPasswordEmail*` (с новым `TestPasswordEmailFirstSetupCodeScreen` — последовательности Android и iOS), `TestPasswordRecovery*`, `TestTwoFa*` — ок, адресов в логе bff нет, стенд и образы удалены. Новые модульные тесты (`TestFirstPassword*`, `TestFailedSendsStayCounted`, `TestRecoveryLosesToANewerChange`, `TestUpdateLosesToANewerConfirmation`) падают при откате каждого исправления. `make lint` — на сборной ветке.

Минутный предел отправки считается на пользователя, назначение и адрес (ключ — первые 8 байт SHA-256 адреса, сам адрес в Redis не попадает): исправленная на экране кода опечатка в адресе получает письмо сразу, часовой и суточный пределы остаются на пользователя (решение 2026-11-12; поведение Telegram не проверено).

