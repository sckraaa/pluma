# 022. Служебный чат 777000 с историей на сервере

- Статус: done (2026-11-02; «Incomplete login attempt» и Premium — отдельно)
- Зависит от: 019
- ADR: новый, заменяет пункт 4 ADR 0022

## Цель
Чат «Pluma» (777000) — обычный чат с историей на сервере, как у Telegram. Новое устройство видит в нём прежние служебные сообщения.

## Что известно (`docs/audit/07-telegram-parity.md`, 2.6 и приложение)
- **У Telegram.** Проверено 2026-10-30 на запасном аккаунте владельца. Веб-сессия, открытая в тот день, видит сообщения с 2024-05-16:
  - коды входа;
  - «New login»;
  - «Two-Step Verification settings changed»;
  - «Incomplete login attempt»;
  - «Thank you for subscribing to Telegram Premium».

  У каждого обычный id сообщения.
- **У Pluma.** «Новый вход» — `updateServiceNotification`: клиент создаёт сообщение локально, и только на устройствах, которые в этот момент были в сети (ADR 0022, п. 4; задача 019). В Pluma Web, вошедшем позже, этого чата нет вовсе.
- **Формат «New login» у Telegram (en)** — в приложении отчёта:
  - устройство: «Telegram Android, 13.1.0, Google sdk_gphone64_arm64, Android, 15 (35)»;
  - место: «Prague, Czechia»;
  - концовка: «If this wasn't you, you can terminate that session in Settings > Devices (or Privacy & Security > Active Sessions).».
- **Код входа у Telegram** добавляет: код отправлен и на почту; «If you didn't request this code by trying to log in on another device, simply ignore this message.».

## Область
- Служебные сообщения — настоящие сообщения от 777000 в истории пользователя.
  - Доставка — `updateNewMessage` с `pts`, как обычные входящие.
  - Повод — каждое событие, о котором пишет Telegram: код входа, новый вход, изменение 2FA, незавершённая попытка входа (шаг 8 задачи 019), Premium от администратора.
- Баннер неподтверждённой сессии (`updateNewAuthorization`) остаётся как есть.
- Тексты — шаблоны 019 (en/ru, по языку сессии, которая подтверждала вход), сверенные с эталоном слово в слово.
- Новый ADR: что меняется в ADR 0022 и почему, с источником.

## Критерии приёмки
- Новое устройство после входа видит в чате 777000 историю прежних служебных сообщений: e2e.
- Тексты совпадают с эталоном Telegram (en; ru — по шаблонам 019).
- Android и iOS не дублируют сообщения при `getDifference`: проверено на клиентах.

## Ход работы (2026-11-01) — держать актуальным
- ADR 0026 (заменяет п. 4 ADR 0022). Пакет `app/bff/authorization/servicemsg`: сообщение от 777000 через `msg.pushUserMessage` — история пользователя, `updateNewMessage` с `pts`. Через него идут код входа (как было), «Новый вход» (вместо `updateServiceNotification`; QR-вход тоже, в `qrcode` подключён клиент msg) и новое «Two-Step Verification settings changed» после `account.updatePasswordSettings` с `new_algo`: устройство и место сессии, язык сессии; русского текста Telegram нет — английский, по правилу `servicetext`.
- Тексты: «Two-Step Verification settings changed» — слово в слово по выгрузке владельца (2026-05-11; типографский апостроф в «didn’t», дата `11/05/2026 at 18:48:28 UTC` — сверено с исходной выгрузкой).
- Тесты: юнит (`servicetext`, `newlogin`, `auth_password_test`), e2e `TestServiceChatHistory` (новое устройство находит чат 777000 в диалогах и в нём коды, «Новый вход» и сообщение о пароле), `TestNewLogin` читает «Новый вход» из истории.
- Не сделано: «Incomplete login attempt» (нужен надёжный момент — вход, брошенный на пароле; запись `pkg/twofa` истекает в Redis молча), Premium (задача 025), проверка на клиентах Android и iOS (дубли при `getDifference`).

## Результат
Выкачено 2026-11-01 (прод `09ecd660`): сообщения от 777000 — настоящие сообщения истории (ADR 0026, заменяет п. 4 ADR 0022). Через `app/bff/authorization/servicemsg` идут код входа, «Новый вход» (вместо `updateServiceNotification`, и при QR-входе) и новое «Two-Step Verification settings changed» после смены облачного пароля; текст — слово в слово по выгрузке владельца, русского у Telegram нет — английский. Проверено: юнит-тесты, `-race`, линт, весь e2e в CI (новый `TestServiceChatHistory`: новое устройство находит чат 777000 в диалогах и в нём коды, «Новый вход», сообщение о пароле; `TestDifferenceFoldsEditsAndDeletes` пропускает сообщения 777000). Не сделано: «Incomplete login attempt» (нужен надёжный момент — вход, брошенный на пароле; запись `pkg/twofa` истекает в Redis молча), Premium (задача 025), проверка дублей на Android и iOS при `getDifference`. В полном прогоне e2e на CachyOS однажды упал `TestDialogs` (срез говорил 4 диалога, полный список вернул 3); три отдельных прогона прошли, причина не найдена.
