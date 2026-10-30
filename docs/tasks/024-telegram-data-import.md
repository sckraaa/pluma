# 024. Импорт данных Telegram

- Статус: todo
- Зависит от: —
- ADR: новый (источник, перенумерация, обновление)

## Цель
Данные, которые у Telegram есть, а у Pluma нет, в Pluma: реакции, стикеры, анимированные эмодзи, подсказки эмодзи, цвета, темы, обои, эффекты, эмодзи-статусы и списки эмодзи по умолчанию. Сервер отдаёт их теми же методами, что Telegram.

## Что известно (`docs/audit/07-telegram-parity.md`, раздел 3 и приложение)
Эталон Telegram — заглушка или пустой ответ Pluma (2026-10-30):

| Данные | Telegram | Pluma |
|---|---|---|
| `messages.getAvailableReactions` | 74 | пусто |
| `messages.getTopReactions` | 20 | заглушка |
| `messages.getDefaultTagReactions` | 16 | заглушка |
| `messages.getAvailableEffects` | 697 эффектов, 894 документа | заглушка |
| `messages.getStickerSet` AnimatedEmoji | 599 документов | заглушка |
| `messages.getStickerSet` EmojiDefaultStatuses | 11 | заглушка |
| `messages.getStickerSet` PremiumGifts | 3 | заглушка |
| `messages.getFeaturedStickers` | 231 набор | пусто |
| `messages.getFeaturedEmojiStickers` | 99 наборов | пусто |
| `messages.getEmojiKeywords` (en) | 4286 | пусто |
| `help.getPeerColors` | 21 вариант | заглушка |
| `help.getPeerProfileColors` | 16 вариантов | заглушка |
| `account.getChatThemes` / `getThemes` | 8 тем | пусто |
| `account.getWallPapers` | 89 обоев | пусто |
| `account.getDefaultEmojiStatuses` | 186 | заглушка |
| `account.getDefaultProfilePhotoEmojis` | 206 | заглушка |
| `account.getDefaultGroupPhotoEmojis` | 208 | заглушка |
| `account.getDefaultBackgroundEmojis` | 30 | заглушка |
| `help.getTimezonesList` | 419 | заглушка |

Клиенты повторяют часть этих запросов примерно раз в 15 с; `messages.getStickerSet` — 259 раз за 72 ч.

## Решения владельца
- Стикеры, подарки и прочее — выкачать с серверов Telegram, в идеале всю базу (2026-10-30).
- id документов и наборов **перенумеровать**: id выдаёт Pluma, ссылки на id Telegram не хранятся (2026-10-30).

## Область
- **Сборщик.** Скачивает из Telegram ответы методов и все документы (`upload.getFile`), перекладывает файлы в dfs, выдаёт новые id и переписывает все ссылки на документы и наборы внутри данных.
- **Аккаунт для выгрузки.** Каким аккаунтом и каким клиентом выгружать, решить в ADR. Сторонний вход с запасного аккаунта Telegram однажды привёл к сбросу ключа (задача 019); не повторять без владельца и не чаще раза в сутки.
- **Обработчики** этих методов, с `hash` и `NotModified`, как у Telegram.
- **Обновление** данных по команде или по расписанию.
- **Реакции.** Импорт даёт список; хранение реакций на сообщениях и обновления — этап 2.

## Критерии приёмки
- Ответы перечисленных методов по форме и количеству совпадают с эталоном: сравнение методов.
- В Android, iOS и Web K видны анимированные эмодзи, панель стикеров с популярными наборами, подсказки эмодзи, цвета имени и профиля, темы и обои.

## Результат
