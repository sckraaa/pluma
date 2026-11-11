План для сервера (~/pluma-work/wt-batch3, HEAD 3de4c3724). Работал только на чтение.

## 0. Главный риск, его нужно подтвердить: sentCodeTypeApp сейчас означает два разных способа доставки
`DoAuthSendCode` (app/bff/authorization/internal/logic/auth_logic.go:96-131) отвечает `SentCodeTypeApp` в двух случаях:
- (а) у пользователя есть другие сессии Pluma: код приходит в Pluma от 777000 (:116-120);
- (б) код принял отправитель через настоящий Telegram (:126-131).

Новый текст App-экрана («…в ваш Telegram, сообщением от аккаунта Pluma») для случая (а) окажется неправдой. По `PhoneCodeTransaction` клиент эти случаи не различит.

Варианты:
1. **Рекомендую.** Случай (а) остаётся App с родным текстом («в приложение Pluma на другом устройстве»). Случай (б) идёт как `auth.sentCodeTypeFragmentSms{url, length}`, где url — `tg://resolve?domain=<имя аккаунта-отправителя>`, имя берётся из конфига. Тогда экран «Проверьте Telegram» строится на родной вёрстке Fragment с кнопкой «Открыть Telegram», а запасной Sms-экран — на той же вёрстке со своими текстами. На сервере для этого нужен case `SentCodeTypeFragmentSms` в `makeAuthSentCodeType`: сейчас его нет, и функция падает с panic (model/code_type.go:110-145). Клиентам придётся различать Fragment и Sms; это работа клиентских планов.
2. App для обоих случаев и один общий текст («в Pluma на другом устройстве или в Telegram»). Это некрасиво.
3. В случае (а) отправлять код и в Telegram, тогда текст про Telegram верен. Минус: тратится лимит аккаунта-отправителя.

Дальше план расписан под цель брифа (App и Sms), а вариант 1 добавляется как отдельный тип отправленного кода.

## 1. Состояние phone_code_hash и машина состояний resend
**Где хранится состояние.**
- `model.PhoneCodeTransaction` (app/bff/authorization/model/model.go:32-46) в Redis, ключ `phone_codes_<authKeyId>_<phone>` (authorization/internal/dao/redis_phone_code.go:38). Поля: `sent_code_type`, `next_code_type`, `telegram_tried`, `state` (`CodeStateSent`).
- Для смены номера ключ свой: `phone_codes.1#<key>_<phone>` (account/internal/dao/redis_phone_code.go:35-39).
- Сам код лежит в `login_code_<hash>`: hash {hmac, expires_at, attempts…} (pkg/logincode/store.go:17-20).

**Что сейчас.**
- `DoAuthReSendCode` (auth_logic.go:146-179): если `!TelegramTried`, пробует Telegram один раз, иначе переводит в Sms с `next=None`.
- Начальный Sms (Telegram не принял код, :105-115) получает `next=None`, поэтому resend отвечает 406 `SEND_CODE_UNAVAILABLE` (:156-158). Повторить попытку через Telegram сейчас невозможно.

**Новые правила** (в модель добавить поле `TelegramAttempts int` с json `telegram_attempts,omitempty`):
- **App → resend** всегда переводит в Sms без попытки через Telegram. Это меняет текущий путь (а), где сейчас пробуется Telegram. Код из 777000 при этом остаётся действительным.
- **Sms → resend** (кнопка «Отправить ещё раз») вызывает `Codes.DeliverByTelegram`. При успехе ответ App (или Fragment по варианту 1) с `next=Sms`. При неудаче ответ Sms с `next=Sms` и `timeout`.
- Повторы ограничены константой `MaxTelegramResends`, например 5 на запрос. После неё `next=None`, и resend отвечает 406 (клиенты прячут ссылку).
- **Без отправителя или для review-номера** (`s.sender == nil`, sender.go:122) ответ Sms с `next=None`, как сейчас. Для проверки нужен новый метод `Service.TelegramAvailable(phone) bool`.
- Начальный sendCode, если Telegram не принял код, отвечает Sms с `next=Sms`.

```go
// DoAuthReSendCode
switch codeData.SentCodeType {
case model.SentCodeTypeApp:
	codeData.SentCodeType = model.SentCodeTypeSms
default: // Sms fallback: try the person's Telegram again
	codeData.TelegramAttempts++
	if m.Codes.DeliverByTelegram(ctx, phoneNumber, phoneCodeHash, clientIp) {
		codeData.SentCodeType = model.SentCodeTypeApp
	}
}
codeData.NextCodeType = model.CodeTypeSms
if !m.Codes.TelegramAvailable(phoneNumber) || codeData.TelegramAttempts >= MaxTelegramResends {
	codeData.NextCodeType = model.CodeTypeNone
}
```

**`ToAuthSentCode`** (model.go:58-60) уже ставит `timeout=60` для не-App при наличии next. Значит, у кнопки «Отправить ещё раз» будет таймер. Значение подтвердить с клиентами.

**Код администратора должен оставаться действительным. Это настоящая ошибка сейчас.**
- `DeliverByTelegram` записывает новый HMAC на место старого (`store.replace`, pkg/logincode/sender.go:158). `Reissue` пишет в то же поле (logincode.go:502).
- Поэтому успешная повторная доставка через Telegram отменяет код, который уже выдал `pluma-admin login-code`.
- Исправление: второй слот в secret, `admin_hmac` и `admin_expires_at`.
  - `Reissue` пишет только этот слот (новый `replaceAdminScript`).
  - `attemptScript` (store.go:54) возвращает оба слота и считает попытку, если хотя бы один код жив.
  - `Verify` (logincode.go:299-352) принимает любой из двух кодов. `MaxAttempts` общий.
  - `Pending.HasCode` учитывает оба слота.

**Флуд.**
- `CheckSendCode` (logincode.go:189-204) не возвращает удачные попытки. `SendCodePerPhoneIP` = 5 в час (limiter.go:41): sendCode, переход в Sms и три «Отправить ещё раз» уже дают `FLOOD_WAIT`.
- Предлагаю оставить как есть: 420 клиенты показывают штатно. Если нужно, можно поднять лимит до 8. Это решение владельца или моё, записать в бриф.
- Лимит `TelegramCodePerIP` (limiter.go:47) молча уводит на путь администратора, так и остаётся.

**`auth.reportMissingCode`.** Сейчас это заглушка `ErrEnterpriseIsBlocked` (auth.reportMissingCode_handler.go). Сделать настоящую реализацию:
- проверить хэш через `getPhoneCode`;
- увеличить метрику `login_code_missing_total` и записать в лог «code not received» без номера;
- ответить `boolTrue`.

Клиенты всё равно уберут путь в поддержку, но старые сборки не получат ошибку.

**Смена номера (необязательно).**
- `DoSendChangePhoneCode` (account/internal/logic/auth_logic.go:60-98) ставит `next=None`.
- Клиенты повторно запрашивают код через `auth.resendCode`. Это не проверено, по памяти, уточнить в клиентских планах. Обработчик ищет запрос в ключе authorization, поэтому найдёт PHONE_CODE_EXPIRED.
- Чтобы было дёшево: вынести машину resend в `pkg/logincode` (функция над `PhoneCodeTransaction`), а `AuthResendCode` (auth.resendCode_handler.go:120-130) при отсутствии login-запроса искал бы ключ смены номера через интерфейс `ChangePhoneStore`.
- Это отдельный шаг, не в первом коммите.

Мелочь вне задачи, только сообщить: `CodeStateReSent == CodeStateChangePhone == 6` (code_type.go:77,80). Обе константы не используются.

## 2. Строки langpack для iOS, Desktop и Web
**Почему нужна поправка к ADR.**
- ADR 0025, «Последствия», прямо говорит: «Наши собственные тексты… этим механизмом не добавляются».
- iOS, tdesktop и Web K встраивают только английский, остальные языки берут с сервера. Без серверных строк русский пользователь увидит английский.
- Нужен новый ADR (например 0045), который дополняет 0025.

**Как добавить наши строки.**
- Файл `pkg/langpack/own.json`, встроенный через `//go:embed`. Структура: `{pack: {key: {lang: value}}}` для пакетов `ios`, `tdesktop`, `webk` (по желанию `android`). Ключи берутся из клиентских планов.
- В `Store.Import` (pkg/langpack/store.go:361-376) после `Rebrand` добавить наши строки для (pack, lang) в `incoming`. Для них не вызывать `Rebrand` (:371) и не собирать `Leftovers` (:373-375): слово Telegram в них означает настоящий Telegram.
- Иначе нельзя: `planImport` (:464-497) превращает в надгробия ключи, которых нет в снимке. Поэтому наши строки обязаны входить в каждый импорт.
- Если наш ключ совпадает с ключом Telegram, импорт падает с ошибкой.
- Язык без перевода получает английский текст, а в `ImportResult` добавляется поле `OwnMissing`. Его печатает pluma-admin (tools/pluma-admin/langpack.go:204-236).

**Развёртывание.** Хранимый исходный текст позволяет переимпортировать без похода в Telegram. После деплоя нужен `pluma-admin langpack import`: записать в docs/ops/langpacks.md. Версия языка вырастет, клиенты получат разницу после `help.getConfig`. Web K не получает надгробий (ADR 0025 п.4), поэтому ключи нельзя удалять, а переименовывать только с сохранением старого.

## 3. Тесты
**Модульные.**
- app/bff/authorization/internal/logic/auth_logic_test.go: заменить `TestResendOfInAppCodeTriesTelegramOnce` (:440) и `TestResendAfterTelegramGoesToAdministrator` (:419). Новые случаи:
  - App → Sms без вызова отправителя;
  - Sms → resend → App, когда отправитель принял код;
  - Sms → resend → Sms с `next=Sms`, когда отказал;
  - после `MaxTelegramResends` `next=None`, затем 406;
  - без отправителя `next=None`;
  - `timeout` только у Sms.
- pkg/logincode: код администратора проходит `Verify` после успешного `DeliverByTelegram` и наоборот; общий `MaxAttempts`.
- pkg/langpack: наши строки не проходят rebrand, полны по списку языков, не становятся надгробиями при повторном импорте, конфликт ключей даёт ошибку.
- Обработчик `reportMissingCode`.

**Интеграционные** (testinfra, Redis/MySQL):
- `TestRedisAdministratorPath` (logincode_integration_test.go:159) дополнить двумя слотами;
- `store_integration_test.go`: импорт с нашими строками, затем `getDifference`.

**e2e** (test/e2e, новый файл login_resend_test.go):
- В стенде отправитель не задан (bff.yaml:48 берёт `${LOGIN_CODES_SENDER_URL}`). Нужна заглушка `/v1/send` в docker-compose.e2e.yaml с переключаемым результатом.
- Сценарий: sendCode → Sms; resend при «no_telegram» → Sms с `next=Sms`; переключить на «sent» → resend → App; `pluma-admin login-code` и signIn кодом администратора проходят.
- Отдельно: `langpack.getStrings` отдаёт наши ключи с «Telegram» в ru.
- Не проверено, что bff в compose достучится до заглушки по имени сервиса; по аналогии с остальными сервисами стенда должно.

**Локально:** стенд `test/e2e/run.sh` со своим `E2E_PROJECT`, клиенты против него. Пути через Telegram проверяются через заглушку; настоящий отправитель только на сервере владельца.
