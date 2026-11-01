# 021. Языковые пакеты

- Статус: in progress (2026-11-01)
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

## Проект (2026-11-01; отчёты исследования — scratchpad сессии `1f4ab8ad…/lang`)
- **Источник** — методы `langpack.*` самого Telegram без входа (`initConnection` с `api_id` владельца, `api_hash` не нужен): точная форма строк (`langPackStringPluralized`), имена языков, `plural_code`, версии, все языки. Экспорт translations.telegram.org (старый скрипт `~/aaaaaaa/self_hosted_version/scripts/fetch-all-langpacks.cjs`) не годится как единственный источник: формы множественного числа расплющены по ключам, нет метаданных и версий.
- **Пакеты:** android, ios, tdesktop, macos, webk (Web K шлёт `web` для `getLanguages`/`getLangPack` и `webk` для остального — один пакет под двумя именами). Пустой `lang_pack` в запросе (Android, iOS) — пакет из `initConnection` сессии.
- **Хранение:** миграция `langpack_languages` (PK `lang_pack, lang_code`; метаданные, `version`) и `langpack_strings` (PK `lang_pack, lang_code, str_key`; простая или множественная, текст после замены и исходный текст Telegram, `deleted`, `version` последнего изменения). Импорт в одной транзакции на пакет и язык: изменённые строки получают новую версию, исчезнувшие — `deleted`; без изменений версия не растёт.
- **Ответы:** `getLangPack` — весь пакет, `from_version = 0`; `getDifference(from)` — строки с `version > from` и удалённые как `langPackStringDeleted`, `from_version = from` (iOS и Web K требуют точного совпадения); `from >= version` — пусто; `getStrings` — найденные, остальные `langPackStringDeleted`; `getLanguages`, `getLanguage`. Неизвестный пакет: `LANG_PACK_INVALID` для `getLangPack`/`getDifference`, пустой вектор для `getLanguages`/`getStrings` (iOS повторяет их без конца при ошибке) — вывод, у Telegram не проверено. Все методы без входа (сессия уже пропускает).
- **`help.getConfig`:** `lang_pack_version` (и `base_lang_pack_version`) — версия пакета сессии и её языка, как вернёт `getDifference`; `suggested_lang_code` — язык клиента, если пакет есть; иначе поля не ставятся. Исправить в сессии `LangCode: initConnection.LangPack` (`main_auth_wrapper.go:428`).
- **Замена Telegram → Pluma** при импорте, правило Android-патча (слово целиком, только значения, не ключи и не адреса), «Telegram Desktop» → «Pluma», таблицы ar/fa/ko/nl, ссылки входа по QR на pluma.chat; импорт печатает отчёт об оставшихся «Telegram» (падежные формы чешского и других языков — «Telegramu» и т. п. — пока остаются, решение владельца ниже).
- **Обновление:** `pluma-admin langpack fetch` (из Telegram в файлы-снимки) и `pluma-admin langpack import` (в базу); по расписанию — таймер на хосте, `docs/ops`. Рассылка `updateLangPack` онлайн-сессиям — следующим шагом.
- **Решения на подтверждение:** источник — API Telegram с `api_id` владельца; падежные формы «Telegram» в чешском, польском и др.

## Результат
