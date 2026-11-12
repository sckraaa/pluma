# 091. Почта восстановления облачного пароля (Resend)

- Статус: в работе (2026-11-12; дизайн готов, кода нет)
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
3. Сбой отправки при восстановлении отвечает `FLOOD_WAIT_60`, а при задании почты — всё равно `EMAIL_UNCONFIRMED_6` (пароль уже сохранён, «отправить снова» повторит).

## Результат
_Заполняется по завершении._
