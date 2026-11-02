# 023. Превью ссылок

- Статус: done (код слит; выкатка и проверка в клиентах — см. «Результат»)
- Зависит от: —
- ADR: [0027](../adr/0027-link-previews.md)

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
Карточки ссылок работают как у Telegram (ADR 0027). Пакет `pkg/webpage` загружает страницу только из публичного интернета. Адрес проверяется после DNS и на каждом редиректе; метаданные облака, локальная сеть и имена вроде `localhost` недоступны. Сборщик читает Open Graph, Twitter Card и `<title>`, картинку сохраняет как обычное фото, карточку хранит в MySQL (`webpages`) сутки. Одну ссылку загружает один раз для всех, кто её ждёт. Новый bff-сервис `webpage` отвечает `messages.getWebPagePreview` и `messages.getWebPage`. `messages.sendMedia` с `inputMediaWebPage` сохраняет выбранную карточку с размером картинки. Сообщение со ссылкой получает карточку в сервисе msg, скрытые ссылки тоже учитываются.

- **Форма ответа как у Telegram** (эталоны 2026-11-02):
  - `display_url` без схемы, `www.` и `/`;
  - `type` `article`/`photo`/`video` по `og:type` и `twitter:card`;
  - `embed_url` для https-плееров;
  - `webPageEmpty`, если у страницы только заголовок;
  - `messageMediaEmpty` для сообщения без ссылки и для ссылки во внутреннюю сеть;
  - `WEBPAGE_NOT_FOUND` и `MESSAGE_EMPTY` по документации `inputMediaWebPage`.
- **Проверка:**
  - unit-тесты `pkg/webpage`: адреса, разбор, кодировки, лимиты, сборка карточки, кэш и повторная загрузка с тем же id, одна загрузка на всех, редирект во внутреннюю сеть (без проверки редиректа тест падает);
  - тест карточки сообщения в плагине msg: на старом коде падает 5 случаев;
  - интеграционный тест хранилища на MySQL (CI);
  - e2e `TestWebPagePreviews` (CI);
  - `make lint`, `go test ./tools/...`, `-race` на затронутых пакетах; каждый коммит собирается отдельно.
- **Решение на подтверждение:** без `webPagePending`/`updateWebPage` (ADR 0027). Сообщение ждёт карточку до 4 с; если не дождалось, уходит без неё. Обычно карточка уже в кэше с момента набора.
- **Не сделано:**
  - Instant View;
  - флаг `safe`;
  - `attributes` (темы, стикерсеты);
  - `@username` в сообщениях по-прежнему не разрешается в упоминание: плагин msg теперь включён, но разрешение имён оставлено прежним, это отдельное изменение (`mtproto.CheckHasMention`).
- Интеграционный тест и e2e на Mac не запускались: colima остановлена, CachyOS выключен. Прошли в GitHub CI.
