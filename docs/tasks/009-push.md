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

## Критерии приёмки
- iOS и Android в фоне получают уведомление ≤3 с после отправки; текст расшифровывается Notification Service Extension.

## Результат
—
