# 009. Push: APNs (включая VoIP) и FCM

- Статус: todo
- Зависит от: 003, 006
- ADR: 0003

## Цель
Сообщения приходят в фоне на iOS и Android; содержимое push зашифровано ключом устройства.

## Область
- `account.registerDevice` / `unregisterDevice` (сейчас заглушки), хранение токенов и секретов устройств.
- Сервис push: APNs (HTTP/2, token-based auth), FCM HTTP v1; зашифрованные push по схеме Telegram; VoIP push для звонков.
- Учёт настроек уведомлений (mute, исключения), отсутствие push при активной сессии.

## Данные владельца
Получены 2026-10-25. Секреты лежат на Mac владельца в `~/pluma-secrets` (каталог 700, файлы 600), в git и в чат не попадают.
- **APNs.**
  - Team ID — в `.env` (`APNS_TEAM_ID`).
  - Ключ «Pluma APNs», Key ID — в `.env` (`APNS_KEY_ID`), P-256, файл `~/pluma-secrets/apns/AuthKey_<Key ID>.p8`. Один ключ на sandbox и production, для обычных и VoIP push.
  - Bundle ID `chat.pluma.app` с Push Notifications.
- **FCM.**
  - Проект Firebase Pluma (ID и номер — в `google-services.json`).
  - Android-приложение `chat.pluma.app`, `~/pluma-secrets/firebase/google-services.json`. Файл не секретный, он вшивается в APK вместо файла Telegram.
  - Ключ сервера: отдельный сервисный аккаунт `pluma-push@<проект>.iam.gserviceaccount.com` с ролью Firebase Cloud Messaging API Admin (не Admin SDK), файл `~/pluma-secrets/firebase/<проект>-<id ключа>.json`.

## Критерии приёмки
- iOS и Android в фоне получают уведомление ≤3 с после отправки; текст расшифровывается Notification Service Extension.

## Результат
—
