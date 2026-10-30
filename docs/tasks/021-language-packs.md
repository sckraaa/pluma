# 021. Языковые пакеты

- Статус: todo
- Зависит от: —
- ADR: новый (хранение и обновление пакетов)

## Цель
Интерфейс клиентов на всех языках Telegram, с Pluma вместо Telegram в строках. Пакеты отдаются сервером, как у Telegram.

## Что известно (`docs/audit/07-telegram-parity.md`, 1.1 и приложение)
- **Методы.** `langpack.getLanguages`, `getLangPack`, `getDifference`, `getStrings` — пустые fake-ответы; `langpack.getLanguage` — missing.
- **Эталон Telegram:** 29 языков; `getLangPack(webk, ru)` — 5291 строка.
- **Что видно сейчас.**
  - Android-«Русский» почти весь английский: в `values-ru/strings.xml` 257 строк против 11034.
  - iOS и Web K только английские.
  - Экран входа Web K предлагает «Continue in English» вместо «Продолжить на русском».
- **`lang_pack`.** Android и iOS шлют `lang_pack = ""` (`LocaleController.java:3306, 3334`; `Localizations.swift:23`). Сервер выбирает пакет по `lang_pack` из `initConnection`: android, ios, tdesktop, macos, webk, weba. Это вывод из кода, у Telegram не проверено.
- **`suggested_lang_code`.** `help.getConfig` отдаёт `classic-zh-cn` — остаток апстрима. Поставить так, как отдаёт Telegram, сверив с эталоном.
- **Основа.** Старая попытка владельца `sckraaa/tg_self_host`, скрипт `fetch-all-langpacks.cjs`; источник — translations.telegram.org и `langpack.*` Telegram. Локальная копия — `~/aaaaaaa`, её не удалять.

## Область
- Импорт всех пакетов всех платформ из Telegram с версиями. Замена «Telegram» на «Pluma» по тем же правилам, что в патчах клиентов: названия функций — тоже; ключи, URL и имена кода — нет.
- Хранение с версиями, `langpack.getDifference` по версиям, обновление по расписанию или по команде `pluma-admin`.
- Тексты наших собственных функций — ключами во всех языках (правило владельца).

## Критерии приёмки
- Ответы `langpack.*` по форме совпадают с эталоном: сравнение методов из приложения отчёта, число языков и строк.
- Android, iOS, Desktop и Web K на русском без английских остатков: проверено на эмуляторе, симуляторе и в браузере.
- В строках нет слова «Telegram», кроме исключений, описанных в брифе.

## Результат
