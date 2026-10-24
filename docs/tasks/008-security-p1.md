# 008. Безопасность P1: 2FA, PFS, проверки протокола

- Статус: in-progress
- Зависит от: 001
- ADR: 0003, 0009

## Цель
Полное соответствие MTProto security guidelines и настоящая 2FA.

## Область
По `docs/audit/02-transport-and-security.md` (P1/medium):
- 2FA по SRP: `account.getPassword`, `account.updatePasswordSettings`, `auth.checkPassword`, восстановление.
- PFS: срок жизни temp-ключей, `destroy_auth_key`, auth_key в БД зашифрованы (ключ шифрования вне БД).
- Handshake: проверки `g_a`/`g_b`, хэшей, nonce; pq с proof-of-work вместо фиксированного.
- Окно msg_id (+30 с / −300 с), проверки seqno, защита от повторов.
- Rate limiting по методам.

## Решения (план реализации)
Состояние кода сверено 2026-10-24. Многое из аудита закрыла задача 001:
- автомат handshake;
- проверки расшифровки (паддинг, длина, константное сравнение `msg_key`);
- сроки temp-ключей и полная проверка `bindTempAuthKey`;
- recover в event loop и в `runLoop`.

Ниже — то, что осталось. Шаги идут по порядку, каждый — отдельные коммиты.

**1. 2FA по SRP** (как в Telegram, [SRP](https://core.telegram.org/api/srp)).
- **Код.**
  - Модуль bff `app/bff/twofa` обслуживает `RPCTwoFa`: `account.getPassword`, `getPasswordSettings`, `updatePasswordSettings`, `declinePasswordReset`.
  - Маршрут добавляется в три `session.yaml`.
  - Общая логика — в `pkg/twofa`: хранение, проверка SRP, лимиты, ожидание пароля. Ей же пользуется `auth.checkPassword` в bff authorization.
- **Хранение.**
  - Таблица `user_passwords` (миграция): `user_id`, `salt1`, `salt2`, verifier `v`, `hint`, время установки, `pending_reset_date`.
  - Алгоритм `passwordKdfAlgoSHA256SHA256PBKDF2HMACSHA512iter100000SHA256ModPow` с простым числом и `g = 3` Telegram; примитивы — `crypto/srp_util.go`.
  - Эфемерное `b` для `srp_id` лежит в Redis со сроком 10 минут и одноразово.
  - Префикс `new_algo.salt1` сервер выдаёт и проверяет (`CheckNewSalt1`).
  - `secure_random` — 256 байт из `crypto/rand`.
- **Вход.**
  - Если у пользователя есть пароль, `auth.signIn`, вход по `future_auth_token` и QR-вход **не привязывают** ключ к пользователю, а отвечают `SESSION_PASSWORD_NEEDED`.
  - Ожидание пароля хранится в Redis по perm-ключу со сроком входа, а не только в памяти session.
  - В этом состоянии session не подставляет `user_id` в метаданные. Разрешены только методы до входа плюс `account.getPassword`, `auth.checkPassword`, `auth.requestPasswordRecovery` и `account.deleteAccount` (сброс аккаунта, как в Telegram).
  - `auth.checkPassword` проверяет `M1` за постоянное время и только тогда привязывает ключ.
  - Хак `SESSION_PASSWORD_NEEDED_<uid>` и плагин `CheckSessionPasswordNeeded` убираются.
- **Лимиты.** Неверный пароль считается на пользователя и на ключ: 5 в час и 20 в сутки. Дальше — `FLOOD_WAIT_X` (дизайн `pkg/logincode`).
- **Восстановление.** Почты на сервере нет, поэтому восстановления по email нет:
  - `account.getPassword` отдаёт `has_recovery = false`;
  - `auth.requestPasswordRecovery` → `PASSWORD_RECOVERY_NA`;
  - `account.resetPassword` из уже вошедшей сессии работает как у Telegram: ожидание 7 дней, затем сброс; `declinePasswordReset` отменяет;
  - для того, кто не вошёл нигде, администратор сбрасывает пароль командой `pluma-admin password reset <номер>`, установив личность вне сервера.
- **Где ещё проверяется пароль.** Методы, которые берут `InputCheckPasswordSRP` (`account.deleteAccount`, `messages.editChatCreator`), проверяют его общим кодом.
- **Логи.** Пароль, `A`, `M1`, `v`, `b` не пишутся (`tools/logaudit`).

**2. PFS и ключи.**
- **Сроки и удаление.**
  - `expires_in` temp-ключа ограничен 7 днями. Больший срок урезается: клиенты Telegram просят сутки.
  - Истёкшие temp-ключи и их привязки удаляет воркер authsession; кэши тоже сбрасываются.
  - `auth.dropTempAuthKeys` удаляет temp-ключи текущего perm-ключа, кроме перечисленных.
  - `destroy_auth_key` удаляет ключ, его привязки и пользователя ключа, закрывает его соединения и отвечает `destroy_auth_key_ok`, `_none` или `_fail`.
- **Шифрование ключей в хранилище.**
  - `auth_keys.body` и копия ключа в Redis шифруются AES-256-GCM. Ключ шифрования `AUTH_KEY_ENCRYPTION_KEY` приходит из `.env`, без него authsession не стартует.
  - Формат `v1:` + nonce + шифртекст.
  - Старые открытые ключи читаются и перешифровываются при чтении; `pluma-admin authkeys encrypt` перешифровывает остальные.
- **Сессии.**
  - `auth.resetAuthorizations` («завершить все другие») реализуется.
  - Оба сброса отвечают `FRESH_RESET_AUTHORISATION_FORBIDDEN` сессии моложе 24 часов, как у Telegram.

**3. Проверки протокола.**
- **Handshake** (gnetway):
  - хэш RSA_PAD;
  - SHA1 в `client_DH_inner_data`;
  - диапазон `g_b`;
  - явная проверка коллизии `auth_key_id` (`dh_gen_retry`) и проверка `retry_id`;
  - `pq` — случайное произведение двух 32-битных простых на каждый `req_pq`, то есть настоящий proof-of-work.
- **Session:**
  - окно `msg_id`: старше 300 с — код 16, из будущего больше 30 с — код 17;
  - младшие 32 бита не нулевые;
  - проверки `seqno` (коды 32–35).
- **Повторы.**
  - Дедупликация держит все `msg_id` окна в 300 с, а не 400 последних.
  - Новая обёртка (после рестарта или вытеснения) отвергает `msg_id` старше своего создания кодом 20, как требует спецификация.
- **Соли** — из `crypto/rand`.
- **Сбои.** Оставшийся `panic` в `runLoop` убирается, сбой одного RPC в пачке не теряет ответы остальных.
- **`httpserver`.** Старый `app/interface/httpserver` с прежним handshake удаляется вместе с конфигами и целями Makefile. HTTP-транспорт есть в gnetway.

**4. Лимиты.**
- **gnetway:** соединений с IP (IPv6 — по /64) и `req_DH_params` с IP в минуту. Лимиты — в процессе (`x/time/rate`), настраиваются в конфиге, значения по умолчанию выдерживают группу за одним NAT.
- **session:** RPC на ключ (токен-бакет) и отдельные лимиты для дорогих методов. Ответ — `FLOOD_WAIT_X`.

**Проверки.**
- Unit-тесты на каждую проверку протокола: неверные параметры отвергаются.
- Интеграционные тесты хранения (пароль, temp-ключи, шифрование).
- e2e: установить пароль, войти с ним, сменить, снять, `FLOOD_WAIT` на подборе. В `pkg/mtclient` — клиентская часть SRP.
- Web K на стенде: установка пароля, вход, смена, сброс. Android — на эмуляторе, если позволит время. Сборки iOS пока нет.

## Критерии приёмки
- Тесты на каждую проверку протокола (некорректные параметры отклоняются).
- 2FA работает в iOS, Android, Web: установка, вход, смена, сброс.

## Результат
—
