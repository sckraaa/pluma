# 033. Время прочтения в iOS

- Статус: done (2026-11-03)
- Зависит от: 020 (время прочтения на сервере)
- ADR: —

## Находка (2026-11-03, владелец)
В меню своего сообщения в личном чате Android показывает «прочитано в 01:46», а iOS этой строки не показывает. У настоящего Telegram на iOS строка есть.

## Причина
- Сервер Pluma отдаёт в `help.getAppConfig` `premium_purchase_blocked: true` (`teamgramd/bin/app_config.json`), потому что Premium не продаётся.
- iOS читает его как «Premium отключён» (`submodules/AccountContext/Sources/Premium.swift:280`, `isPremiumDisabled`) и для пользователя без Premium не показывает время прочтения в личных чатах (`submodules/TelegramUI/Sources/ChatInterfaceStateContextMenus.swift:270-274`, `canViewReadStats`). У настоящего Telegram так же — в странах, где Premium недоступен.
- Android такой проверки не делает. Desktop не проверен.

## Варианты (решение владельца)
- **А.** Патч iOS: время прочтения показывается и при отключённом Premium. Тот же подход, что для баннера дня рождения (задача 020).
- **Б.** Оставить как у Telegram там, где Premium недоступен.

## Результат
Вариант А владельца сделан в pluma-ios (`74bf939` в `main`): строку «прочитано в …» больше не закрывает проверка Premium (`PremiumConfiguration.isPremiumDisabled`), ведь Pluma Premium не продаёт. Проверено в симуляторе на стенде: в меню своего сообщения видна строка «read today at 02:06». Тесты патча (46) зелёные. Сборка `41559341416` прошла: ad hoc IPA ушла в сервис установки (pluma.chat/iphone/install), сборка выгружена в TestFlight. Приватность времени прочтения (`hide_read_marks`) на сервере — задача 034.

