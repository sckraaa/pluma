# 0025. Языковые пакеты: снимки из `langpack.*` Telegram, хранение с версиями в MySQL, замена «Telegram» на «Pluma» при импорте

- Статус: accepted
- Дата: 2026-11-01
- Задача 021. Отчёты исследования, на которые ссылается решение, — в брифе задачи (раздел «Проект»).

## Контекст
- **Сервер пакетов не отдавал.** `langpack.getLanguages/getLangPack/getDifference/getStrings` отвечали пустыми заглушками прокси bff (`app/bff/bff/client/fake_rpc_result.go`), `langpack.getLanguage` не было вовсе, маршрут `/mtproto.RPCLangpack` в `session.yaml` закомментирован. `help.getConfig` отдавал статические `suggested_lang_code = "classic-zh-cn"` и `lang_pack_version = 262834` из `teamgramd/bin/config.json`.
- **Что ждут клиенты** (прочитано по исходникам закреплённых версий):
  - **Пакет называется по платформе** и приходит в `initConnection.lang_pack`: `android` (`ConnectionsManager.cpp`), `ios`/`macos` (`MTApiEnvironment.m`), `tdesktop` (`lang_instance.cpp`), `webk` (Web K `config/app.ts:28`).
  - **Android и iOS шлют в методах `lang_pack = ""`** или старые конструкторы без него (Android `TLRPC.java:57563-57624`, `LocaleController.java:3306, 3334`; iOS `Localizations.swift:23`) и имеют в виду пакет своего `initConnection`.
  - **Web K просит `web`** в `getLangPack` и `getLanguages` и `webk` в остальных (`lib/langPack.ts:177-182`, `tabs/language.tsx:106-108`, `appLangPackManager.ts:31-44`).
  - **Версии.** `getLangPack` — весь пакет с `from_version = 0`. Разница применяется на iOS и в Web K, только если `from_version` равен их версии (`ManagedLocalizationUpdatesOperations.swift:148-247`, `langPack.ts:752-788`); tdesktop при версии ниже `from_version` просит снова (`lang_cloud_manager.cpp:383-395`). После `help.getConfig` клиенты сравнивают `lang_pack_version` со своей и просят разницу (Android `LocaleController.java:847-874`, iOS `ManagedConfigurationUpdates.swift:98-111`, tdesktop `lang_cloud_manager.cpp:271-279`).
  - **iOS повторяет без конца** упавшие `getLanguages` и `getStrings` (`retryRequest`, `Localizations.swift:9, 24, 58`).
  - **Web K показывает ключ** вместо английского текста для `langPackStringDeleted`, полученного в разнице (`langPack.ts:463-475`).
  - **Android вызывает `.replace` на `plural_code`** без проверки: пустым он быть не может.
- **Источник строк.** Экспорт translations.telegram.org (старый скрипт владельца) теряет форму строк: множественные формы расплющены по ключам, нет имён языков, `plural_code` и версий. Методы `langpack.*` самого Telegram отдают всё это и работают без входа («can be invoked over an unauthenticated connection», страницы методов на core.telegram.org); нужен только `api_id` приложения в `initConnection`, `api_hash` не нужен.
- **Объём.** Пакет — от ~3 тыс. (webk) до ~14 тыс. строк (android) на язык, 19–29 языков на платформу: около миллиона строк и 250–350 МБ в MySQL на все платформы. Весь пакет в ответе — 0,3–1,5 МБ.

## Решение
1. **Источник — `langpack.*` Telegram.** `pluma-admin langpack fetch` подключается к DC 2 Telegram (`149.154.167.51:443`, адрес из встроенного списка Android `ConnectionsManager.cpp`) клиентом `pkg/mtclient` с открытым ключом Telegram (из Web K `rsaKeysManager.ts`, где он приведён со ссылкой на core.telegram.org), создаёт одноразовый ключ авторизации и в `invokeWithLayer(initConnection(...))` с `api_id` владельца (флаг `-api-id` или `$TELEGRAM_API_ID`, в репозитории его нет) вызывает `getLanguages` и `getLangPack` для пакетов android, ios, tdesktop, macos, webk. Результат — файлы-снимки `<каталог>/<пакет>/<язык>.json`: язык из `getLanguages` (с местом в списке), версия Telegram и строки в форме TL (простая или множественная).
2. **Хранение** — миграция `migrate-20261101-langpacks.sql`:
   - `langpack_languages` (PK `lang_pack, lang_code`): описание языка, `version` (наша), `upstream_version` (Telegram), `sort_order`, `updated_at`;
   - `langpack_strings` (PK `lang_pack, lang_code, str_key`, ключ сравнивается побайтно): текст после замены и исходный текст Telegram, признак множественной строки, `deleted` (надгробие), `version` последнего изменения; индекс `(lang_pack, lang_code, version)`.
3. **Импорт** (`pluma-admin langpack import`, `langpack.Store.Import`) — одна транзакция на пакет и язык, под блокировкой строки языка:
   - строки сравниваются с сохранёнными после замены;
   - новые, изменённые и вернувшиеся из надгробия получают новую версию языка, исчезнувшие становятся надгробиями с ней же;
   - новая версия — `max(сохранённая + 1, версия Telegram)`; без изменений строки не пишутся и версия не растёт; описание языка обновляется всегда;
   - если изменился только исходный текст Telegram, а после замены текст тот же, обновляется исходный текст без новой версии.
   Надгробия не удаляются: разница от любой прошлой версии отвечается точно, журнал не нужен.
4. **Ответы** (`app/bff/langpack`, новый сервис bff, маршрут `/mtproto.RPCLangpack` → `bff.bff`; методы работают без входа, как и раньше пропускала сессия):
   - пакет — `lang_pack` запроса, пустой — из метаданных сессии (`initConnection`); `web` — это `webk`; код языка — в нижнем регистре;
   - `getLangPack` и `getDifference(0)` — весь пакет, `from_version = 0`; собранный ответ кэшируется в памяти по (пакет, язык, версия), не больше 32 пакетов, версия сверяется дешёвым запросом на каждый вызов;
   - `getDifference(from)` — строки с версией больше `from` и надгробия как `langPackStringDeleted`, `from_version = from`; для `webk` без надгробий; `from` не меньше текущей версии — пустой список;
   - `getStrings` — найденные строки, остальные `langPackStringDeleted` (так отвечает Telegram, запись от 2026-04-10);
   - неизвестный пакет — `LANG_PACK_INVALID`, неизвестный язык известного пакета — `LANG_CODE_NOT_SUPPORTED` для `getLangPack`, `getDifference`, `getLanguage`; для `getLanguages` и `getStrings` — пустой вектор.
5. **`help.getConfig`** берёт `flags.2` из базы для пакета и языка сессии: `suggested_lang_code` — язык клиента, `lang_pack_version` — его версия, `base_lang_pack_version` — версия базового языка (0 без него). Языка нет — флаг не ставится, клиенты остаются на встроенных строках. Три поля делят один флаг и пишутся только вместе. Статические значения из `config.json` убраны.
6. **Сессия** сохраняет `lang_code` первого `initConnection` из `lang_code` (раньше туда попадал `lang_pack`).
7. **Замена «Telegram» → «Pluma»** при импорте, только в значениях, по правилам патчей клиентов (`pkg/langpack/rebrand.go`):
   - слово целиком по правилу Android-патча: `(?<![A-Za-z0-9@/.])(Telegram|TELEGRAM)(?![A-Za-z0-9]|\.\w)` → «Pluma»/«PLUMA»; адреса, `@Telegram`, `Telegram.org`, `t.me/...` и составные слова остаются;
   - «Telegram Desktop» → «Pluma» (Desktop-патч);
   - написания в других письменностях по языку, длинные первыми: ar, fa, ko (с частицами), nl (Android `STRING_LOCAL_NAMES`, Persian — из iOS-патча);
   - ссылки входа по QR (`AuthAnotherClientDownloadClientUrl`, `AuthAnotherWebClientUrl`, `AuthAnotherClientInfo1`, `AuthAnotherClientInfo5`) — на pluma.chat и web.pluma.chat;
   - фраза про «Telegram 4.7» переписана, как в Android-патче;
   - `lng_about_text1_api` («Telegram API») не меняется;
   - импорт печатает строки, где имя Telegram осталось вне адресов (`langpack.Leftovers`), с ключами по флагу `-keys`; ключи, оставленные намеренно, в отчёт не входят.
8. **Обновление** — по расписанию на хосте (таймер systemd или cron, `docs/ops/langpacks.md`): `fetch`, затем `import`.

## Последствия
- **Нагрузка.** Каждый вызов делает один короткий запрос к `langpack_languages`; весь пакет собирается один раз на версию. Импорт после первого пишет только изменённые строки.
- **Клиенты видят изменения** при следующем `help.getConfig` (версия в конфиге выросла — клиент просит разницу). Рассылки `updateLangPack` онлайн-сессиям нет — это следующий шаг.
- **Падежные формы** (дополнено 2026-11-01, владелец доверил решение): «Telegram» в падеже становится тем же падежом «Pluma» — `pkg/langpack/inflect.go`, таблицы окончаний по языкам, собранные из всех пакетов Telegram на 2026-11-01: cs, sk (родительный или дательный/предложный «Telegramu» — по предлогу или глаголу перед словом), pl, hr, sr, fi, hu, uz, kk, tr (апостроф и связующие согласные: Pluma'nın, Pluma'yı), sv, nb, de (составные слова через дефис: Pluma-konto, Pluma-Nutzer), ms; заглавные формы тоже. Pluma — существительное женского рода на «-a»: «v Plumě», «w Plumie», «Pluman», «Plumán». Строчное «telegram» в значении названия (чешское «data telegramu», хорватское, шведское и норвежское в составных словах) заменяется так же; украинские ключевые слова поиска «Телеграм» — на «Pluma». Правило слова больше не заменяет имя, за которым идёт латинская или кириллическая буква: раньше словацкое «Telegramách» становилось «Plumaách» и не попадало в отчёт. Имя сразу после аргумента формата («%3$sTelegram Premium») заменяется. Окончание, которого нет в таблице, остаётся и попадает в отчёт импорта. После правил на 2026-11-01 в отчёте остаются только опечатки самого Telegram (слитное «diTelegram» в малайском, «vTelegramu» в чешском), французское слово «TELEGRAMME» и строчное «telegram» в адресах.
- **Наши собственные тексты** (функции Pluma, которых нет в Telegram) этим механизмом не добавляются: снимок содержит только ключи Telegram.
- **`api_id` владельца** уходит в Telegram при каждом `fetch`; номер телефона и `api_hash` — нет.

## Не проверено
- Что Telegram отвечает на `getLangPack(web, …)` и чем пакет `web` отличается от `webk` (сервер считает их одним пакетом).
- Какие ошибки и в каких случаях отдаёт Telegram (выбор ошибок — по списку на страницах методов).
- Как Telegram выбирает `suggested_lang_code` (у нас — язык клиента; `system_lang_code` в метаданные bff не передаётся).
- Работа `fetch` с настоящим Telegram: тесты идут на записанных ответах.

## Рассмотренные альтернативы
- **Экспорт translations.telegram.org** без входа. Отпадает как единственный источник: нет версий, имён языков и `plural_code`, множественные формы пришлось бы собирать из ключей по правилам каждой платформы.
- **Журнал изменений вместо надгробий.** Отпадает: надгробие на ключ дешевле и даёт точную разницу от любой версии.
- **Кэш строк в Redis (go-zero row cache).** Отпадает: десятки тысяч строк на язык, а нужен собранный ответ целиком.
- **Замена при отдаче, а не при импорте.** Отпадает: правила применялись бы на каждый запрос; исходный текст хранится, поэтому смена правил — повторный импорт того же снимка.
