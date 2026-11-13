# 095. Предложения обновить приложение: выпуск за час, обязательный выпуск

- Статус: в работе (2026-11-13; решение владельца 2026-11-13: о новом выпуске на сайте клиенты узнают примерно за час, оператор может сделать выпуск обязательным).
- ADR: 0035 (выпуски клиентов). Новое решение не нужно: задача меняет периоды проверки и даёт оператору флаг, который в схеме уже есть.
- Зависит от: 054 (выпуски на сайте, `help.getAppUpdate`, самообновление клиентов), 085 (обновление iOS из приложения).
- Ветка сервера: `task/095-update-prompts`. Клиенты — ветки `task/095-update-prompts` в `pluma-ios`, `pluma-desktop`, `pluma-web`; Android правки не требует (см. ниже).
- Источники — версии, на которых собирается Pluma: Android — DrKLO/Telegram `7be672ea` (13.2.1, `AND`), iOS — Telegram-iOS `release-13.2` `24638bc69b` (`IOS`), tdesktop v7.4.1 `157f5cfc8` (`TD`), tweb `aa74832e` (`TWEB`); копии в `~/pluma-work/reference-src-new`.

## Цель
Выложенный на сайт выпуск (`POST /dl/upload`) доходит до установленных приложений примерно за час: приложение само предлагает обновиться. Оператор может пометить выпуск Android или iOS обязательным: приложение показывает экран обновления без «Пропустить».

## Как клиенты узнают о выпуске сейчас
- **Android:** `help.getAppUpdate` при каждом возврате в приложение (`AND LaunchActivity.java:7267`), если с прошлой проверки прошло не меньше `updateCheckDelay` (`:6134`). Период приходит из конфига, ключ `inapp_update_check_delay` (`AND MessagesController.java:3216-3233`), по умолчанию сутки (`:1595`).
- **iOS:** `help.getAppUpdate` при запуске основного приложения и затем каждые 12 часов (`IOS AppUpdate.swift:37-48`, `AccountTaskManager.swift:148`). Задержка `suspendAwareDelay` сверяет абсолютное время каждые 4 секунды (`SwiftSignalKit/Source/Signal_Timing.swift:35-80`): после возврата из фона просроченный опрос уходит сразу. Ключа конфига нет.
- **Desktop:** `GET <autoupdate_url_prefix>/current4` раз в 8–16 часов (`TD config.h:27-28`, `core/update_checker.cpp:1726-1737`), при включённом автообновлении.
- **Web:** раз в 30 минут `fetch('version')`; если текст не равен `App.versionFull`, в меню появляется кнопка обновления (`TWEB src/components/sidebarLeft/index.ts:396-411`). Сборка Pluma оставляет `public/version` и `VITE_VERSION_FULL` апстрима (`2.3 (686)`, `clients/patches/web.md`, W-008), поэтому новые выкладки Pluma кнопку не включают.

## Что делает каждый клиент с `can_not_skip`
- **Android:** полноэкранный `BlockingUpdateView` вместо листа (`AND LaunchActivity.java:6158-6159`, `:1402-1408`), при каждом возврате в приложение снова (`:7264-7265`). Открываясь так, экран спрашивает сервер и закрывается, если пришёл `help.appUpdate` без `can_not_skip`; на `help.noAppUpdate` остаётся (`BlockingUpdateView.java:305-323`). Ответ с версией, которую клиент уже хранит, не рассматривается (`LaunchActivity.java:6153-6155`): флаг, поставленный после первого необязательного предложения, до этого клиента не доходит. Скрытый выход — десять нажатий на картинку (`BlockingUpdateView.java:75-81`). У Pluma блокирующий экран уже включён правкой A-094 (`isSelfUpdatingBuild()` в `BlockingUpdateView.java`, `clients/patches/android.md`).
- **iOS:** `AppUpdateInfo.blocking` = бит 0 флагов (`IOS AppUpdate.swift:22-34`); при нём на экране нет «Пропустить» (`UpdateUI/UpdateInfoController.swift:134`). Экран показывается, когда ответ отличается от прежнего (`TelegramUI/Sources/ApplicationContext.swift:589-599`): смена флага покажет его заново. Убирает ли что-то уже показанный экран после `help.noAppUpdate` — по коду нет (новый ответ только открывает экран), на устройстве не проверено.
- **Desktop:** `current4` несёт только версию и путь пакета (`TD update_checker.cpp:608-690`), обязательных обновлений нет. Апдейтер скачивает пакет сам и предлагает перезапуск.
- **Web:** обязательных обновлений нет; кнопка в меню перезагружает страницу.

## Область
**Сервер (эта ветка):**
1. `teamgramd/bin/app_config.json`: `inapp_update_check_delay = 3600`; строка в `docs/ops/app-config.md`, проверка значения в `TestShippedConfig`.
2. `pluma-admin release mandatory|optional android|ios <сборка>` → `appreleases.SetCanNotSkip`; `release list` показывает колонку `MANDATORY`. Desktop-платформы — ошибка: флаг у них ничего не делает. Аргументы проверяются до чтения конфига.
3. `docs/ops/releases.md`: когда каждый клиент узнаёт о выпуске, что он делает с обязательным выпуском, команды оператора, порядок «сначала optional, потом withdraw».

**Клиенты (отдельные ветки):**
- **iOS (`pluma-ios`):** период опроса `help.getAppUpdate` 12 часов → 1 час: `12.0 * 60.0 * 60.0` → `1.0 * 60.0 * 60.0` в `submodules/TelegramCore/Sources/State/AppUpdate.swift:48`, с проверкой в `ci/patch.py` (точная замена, одно вхождение) и тестом `AppUpdateTest`. Благодаря `suspendAwareDelay` приложение, вернувшееся из фона через час и больше, спрашивает сразу. Сервер отвечает без изменений. Ссылка в ответе постоянна для сессии и сборки в пределах суток UTC (задача 085, `iosUpdateURL`), поэтому ежечасный опрос не показывает экран заново; после смены суток экран необязательного выпуска покажется ещё раз, как и сейчас при 12-часовом опросе.
- **Desktop (`pluma-desktop`):** `UpdateDelayConstPart = 8 * 3600` и `UpdateDelayRandPart = 8 * 3600` (`Telegram/SourceFiles/config.h:27-28`) → 50 и 20 минут (проверка раз в 50–70 минут), точной заменой в патче с проверкой. Ответ `current4` — маленький JSON; сто клиентов раз в час нагрузки не дают.
- **Web (`pluma-web`):** `public/version` и `VITE_VERSION_FULL` (`.env`) — одна и та же строка, своя у каждой выкладки Pluma (версия апстрима и номер сборки CI); тогда 30-минутный опрос замечает выкладку. `VITE_VERSION` не трогать: это версия состояния клиента (`TWEB src/config/state.ts:24`). `versionFull` ещё уходит в `clientVersion` (`appPrivacyManager.ts:232`) и в выгрузку логов (`exportLogs.ts:76`). Проверка апстрима W-008 должна сверять версию апстрима отдельно от этой строки. Период 30 минут подходит, обязательности нет.
- **Android:** ничего: период приходит с сервера, блокирующий экран уже включён A-094.
- **CI Android и iOS (по желанию, решение владельца):** вход `mandatory` у workflow выпуска, который передаёт `can_not_skip=1` при загрузке. Иначе между загрузкой и `release mandatory` есть окно, в которое Android может получить выпуск необязательным и больше его не пересмотрит (`LaunchActivity.java:6153-6155`).

**Не трогаем:**
- панель (`app/ops/admin`): флаг там не показывается и не меняется; добавить можно отдельно;
- TestFlight (обновляет сам), Google Play и App Store (Pluma там нет);
- тексты: новых строк, видимых пользователю, нет; экраны обновления используют строки апстрима (`UpdateTelegram`, `Update.Title`, `Update.Skip`), которые уже переименованы правилами бренда и `pkg/langpack`.

## Тесты
- `app/bff/configuration/internal/appconfig`: `TestShippedConfig` — `inapp_update_check_delay = 3600`; `TestShippedConfigDocumented` — ключ в таблице `docs/ops/app-config.md`. Сквозной `test/e2e/app_config_test.go` сверяет весь файл и подхватывает ключ сам.
- `tools/pluma-admin`: `TestReleaseUsage` (неверные аргументы — код 2 без чтения конфига; desktop — ошибка «no mandatory updates»), `TestMandatoryColumn`.
- Интеграционный `TestHelpGetAppUpdateMandatory` (`app/bff/configuration/internal/core`, MySQL через `pkg/testinfra`): после `SetCanNotSkip` Android и iOS получают `can_not_skip`, после снятия — нет; повтор того же значения находит выпуск; чужая сборка не находится.

## Проверки для `docs/project/TESTING-BACKLOG.md`
Записаны в разделе «Обновления приложений (задача 095)».

## Решения, требующие подтверждения владельцем
1. Обязательность — только у Android и iOS; для Desktop команда отвечает ошибкой (обязательных обновлений у tdesktop нет, `update_checker.cpp:608-690`).
2. Desktop проверяет раз в 50–70 минут (было 8–16 часов): это «примерно час» с разбросом, чтобы сто клиентов не шли одновременно.
3. Вход `mandatory` в CI выпуска Android и iOS — делать ли (закрывает окно между загрузкой и командой оператора).

## Результат
(заполняется по завершении)
