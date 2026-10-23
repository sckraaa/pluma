# 008. Безопасность P1: 2FA, PFS, проверки протокола

- Статус: todo
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

## Критерии приёмки
- Тесты на каждую проверку протокола (некорректные параметры отклоняются).
- 2FA работает в iOS, Android, Web: установка, вход, смена, сброс.

## Результат
—
