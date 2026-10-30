# 023. Превью ссылок

- Статус: todo
- Зависит от: —
- ADR: новый (сборщик превью: безопасность, кэш, лимиты)

## Цель
Карточка ссылки при наборе, в отправленном сообщении и у собеседника, как у Telegram.

## Что известно (`docs/audit/07-telegram-parity.md`, 1.2)
- **Методы.**
  - `messages.getWebPagePreview` — missing.
  - `messages.getWebPage` (#8d9692a3) — missing: подмена fake не срабатывает.
  - Плагин превью сервиса msg отвечает `ErrMethodNotImpl` (`app/messenger/msg/internal/dao/fake_plugin.go:21-23`), поэтому `messageMediaWebPage` не строится никогда.
- **Клиенты:**
  - Android: `ChatActivity.java:14129, 14198-14230`;
  - iOS: `WebpagePreview.swift:154, 219, 276`;
  - Web K: `components/chat/input.ts:3452-3460`, `appWebPagesManager.ts:273-284`.
- **Эталон.** Telegram на `getWebPagePreview(https://telegram.org)` отдаёт `messages.webPagePreview` с `messageMediaWebPage`, флаг `safe`.

## Область
- Серверный сборщик:
  - запрос страницы без доступа во внутреннюю сеть (защита от SSRF);
  - лимиты размера и времени;
  - разбор Open Graph и Twitter Card;
  - картинка — в dfs;
  - кэш по URL.
- `getWebPagePreview`, `getWebPage`, превью при отправке, `updateWebPage` для превью, которое ещё строится (`webPagePending`).
- Instant View — отдельно, позже.

## Критерии приёмки
- Для набора типовых сайтов поля карточки совпадают с ответами Telegram по форме: сравнение методов.
- Адреса внутренней сети не запрашиваются: тест.
- Превью видно у отправителя и получателя на Android, iOS и Web K.

## Результат
