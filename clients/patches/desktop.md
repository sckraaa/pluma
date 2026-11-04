# Реестр правок Desktop-клиента (pluma-desktop)

Всё, что Pluma меняет в Telegram Desktop: правки исходников апстрима, ресурсы, сборка, подпись, CI. Назначение: при обновлении апстрима сразу видеть, какие места апстрима мы трогаем, на что опирается каждая правка и что сломается, если апстрим их сдвинет.

Состояние реестра: ветка `main` репозитория `sckraaa/pluma-desktop`, коммит `ff05bde` (2026-10-31); в истории 41 коммит, все от Alexandr Skraglenko. Ветка `origin/task/020-parity-fixes` указывает на тот же коммит. Тесты на этом коммите: 65, все проходят (`python3 -I -m unittest discover -s ci -p '*_test.py'` на разреженной выгрузке закреплённого апстрима, 2026-11-03). Ветка `task/053-saved-tags` (задача 053, ещё не в `main`): D-073, тестов 66, все проходят (2026-11-04).

## Общее

**Репозиторий.** `github.com/sckraaa/pluma-desktop`. Кода апстрима в нём нет: CI скачивает закреплённый релиз, правит его скриптом и собирает. Состав: `ci/patch.py` (патч и итоговая проверка), `ci/patch_test.py`, `ci/sign_test.py`, `ci/budget_test.py` (тесты), `ci/*.sh` (выгрузка, сборка, упаковка, подпись, шифрование, кэш), `brand/` (иконки, палитры, таблица строк), `config/app.env` (идентичность), `config/dev/` (dev-сервер и его публичный ключ), `upstream.env` (закрепление апстрима и тулчейна), `.github/workflows/desktop.yml` и `staple-macos.yml`.

**Закреплённый апстрим** (`upstream.env`):
- `UPSTREAM_REPO=https://github.com/telegramdesktop/tdesktop.git`, тег `v7.0.9`, коммит `a1e89e1f64f08cb058caf1c61ff43f319f98a6ec` (дата коммита 2026-08-05), `UPSTREAM_VERSION=7.0.9`, `UPSTREAM_LAYER=228` — последний релиз на layer 228.
- Тулчейн тега: `QT_VERSION=6.11.1` (из `Telegram/build/qt_version.py`), `WINDOWS_SDK=10.0.26100.0` и `MSVC_TOOLSET=14.44` (из `docs/building-win.md`), `XCODE_VERSION=26.6` (Xcode по умолчанию образа `macos-26` на момент закрепления), `CCACHE_VERSION=4.13.6` (из `.github/workflows/linux.yml`), `BUILDKIT_IMAGE=moby/buildkit:v0.33.1@sha256:cec9f139…`.

**Как применяется патч.**
- Точка входа: `ci/configure.sh dev|server` → `python3 -I ci/patch.py --source <checkout> --upstream upstream.env --app config/app.env --server-ipv4 … --server-port … [--server-ipv6 …] --rsa-public-key <PEM>`.
- `dev` берёт `config/dev/server.env` (`127.0.0.1:10443`) и `config/dev/server_public.pem`; `server` — переменные `SERVER_IPV4`, `SERVER_PORT`, `SERVER_IPV6`, `RSA_PUBLIC_KEY` (в CI — переменные репозитория `PLUMA_SERVER_*`, `PLUMA_RSA_PUBLIC_KEY`).
- Порядок внутри `patch()`: `check_upstream()` → `BACKPORTS` + `BUILD_EDITS` → DC и ключи → запасные пути к Telegram → CMake (updater, crash reports) → Info.plist → идентичность → баннер дня рождения (`BIRTHDAY_BANNER`, `BIRTHDAY_GIFTS`, `TOP_BAR_WRAP`) → `rebrand()` → маркер `pluma-patch.txt`. Затем `verify()` (с `verify_brand()`).
- Виды правок в `ci/patch.py` (класс `Tree`):
  - `replace(rel, old, new, count=1)` — точная замена текста; текст обязан встретиться ровно `count` раз, иначе `PatchError`;
  - `sub(rel, pattern, repl, count=1)` — замена по регулярному выражению с тем же контролем числа совпадений;
  - `replace_body(rel, head, tail, body, must_contain)` — замена тела между уникальным `head` и первым `tail` после него; в старом теле обязаны быть строки `must_contain`;
  - `replace_block(...)` — то же, но заменяется и сам `head`/`tail`;
  - запись файлов целиком (иконки, zip тем) — только если SHA-256 файла апстрима совпал с закреплённым;
  - `expect` / `expect_some` — только проверка наличия текста.
- Файлы читаются и пишутся байтами; CRLF в любом правленом файле останавливает патч (`lf_only`). `ci/fetch-upstream.sh` выгружает с `core.autocrlf=false`, `core.eol=lf`.
- Повторный патч того же дерева запрещён (есть `pluma-patch.txt` → ошибка).

**Как запускать тесты.**
```sh
ci/fetch-upstream.sh sparse                       # ./upstream: только файлы, которые читает патч (секунды)
python3 -I -m unittest discover -s ci -p '*_test.py' -v
ci/configure.sh dev                               # сам патч на разреженной выгрузке
ci/lint.sh                                        # shellcheck и actionlint
```
Без выгрузки `./upstream` на закреплённом коммите класс `UpstreamTest` пропускается (в CI — никогда: там `CI` задан и выгрузка делается первым шагом).

Тесты `ci/patch_test.py`:
- `KeyTest` — отпечатки ключей Telegram (`0xD09D1D85DE64FD85` прод, `0xB25898DF208D2603` тест) и dev-ключа, PEM туда-обратно, отказ от других форматов, ключ в C++-литерале;
- `ConfigTest` — `config/app.env`, dev-сервер, отказы (адрес Telegram, GUID Telegram, имя с «telegram», неверный `api_hash` и т. п.);
- `ScanTest` — поиск адресов Telegram, ключей в литералах, конструирования `SpecialConfigRequest`/`DomainResolver`;
- `TreeTest` — механика `replace`/`replace_body`/`replace_block`, сохранение окончаний строк;
- `BrandTest` — таблицы бренда (strings, палитры, IDENTITY_*), `recolor`, `identity`, `is_blue`, `resolve`, стабильность zip ночной темы, `code_telegram`;
- `UpstreamTest` (на закреплённом апстриме): `test_patch`, `test_brand`, `test_people_colours`, `test_people_colours_are_verified`, `test_people_colours_kept`, `test_birthday_banner`, `test_changed_binary_files`, `test_build_edits_stay_in_their_platform`, `test_dev_server`, `test_patched_tree_is_refused`, `test_damage`.
- `test_damage` — главный страж обновлений: таблица `DAMAGE` (82 порчи; 90 с D-073) ломает каждое правленое или проверяемое место апстрима и требует, чтобы патч остановился с ошибкой, называющей файл.

`ci/sign_test.py` (`SignTest`, 11 тестов) гоняет `ci/sign-macos.sh` против подделок macOS-утилит; `ci/budget_test.py` (`BudgetTest`, `AdmitTest`) — бюджет кэша Actions.

**Итоговая проверка `verify()`** — независимо от того, как сделаны правки, по всем `Telegram/SourceFiles/**/*.{cpp,h,mm,m,c,cc,hpp}`:
- нет адресов из опубликованных диапазонов Telegram (`TELEGRAM_NETWORKS`, по `core.telegram.org/resources/cidr.txt`, плюс `95.161.76.100`);
- нет строк MTProto-ключей Telegram (`TELEGRAM_KEY_LINES`); любой PEM-ключ в литерале — либо ключ сервера, либо 1024-битный ключ обновлений Telegram (`UPDATE_KEY_LINES`) только в `config.h` и `_other/packer.cpp`; копий ключа сервера ровно 3;
- нигде, кроме собственных файлов, не конструируются `SpecialConfigRequest` и `DomainResolver` (`CONSTRUCTION`);
- в `mtproto_dc_options.cpp` нет `kBuiltInDcs`, ровно один (или два с IPv6) `applyOneGuarded(1, Flag::f_static…` с адресом сервера; `kDefaultMainDc = 1`; `tryCustomResolve` возвращает `false`; CMake компилирует без updater и crash reports;
- в файлах идентичности не осталось следов Telegram Desktop (таблица в `verify()`); `prepare.py` без пакетов msvcrt, без `dump_syms.vcxproj`, без отладочных Qt/tg_owt на macOS;
- `verify_brand()`: в `lang.strings` никто не называет Telegram, кроме ссылок и `LANG_KEEP`, и нет слова «Teamgram»; в строковых литералах кода «Telegram» остаётся только по таблице `CODE_TELEGRAM`; нигде нет `tg://`; регистрация и объявление только `plm://`; иконки — файлы `brand/tree`; в темах по умолчанию нет синего Telegram, кроме `KEPT_BLUE`; цвета-идентичности как в `IDENTITY_COLORS`; аватары экспорта.
- Чего `verify()` не проверяет: баннер дня рождения и подарки (D-048…D-050, D-070) проверяются только тестом `test_birthday_banner`, теги «Избранного» (D-073) — только `test_saved_tags` на закреплённом апстриме, отдельной итоговой проверки нет (это же отмечено сверкой клиентов в задаче 020, «Перекосы патчей клиентов»).

**`check_upstream()`** перед правками: `api.tl` содержит ровно `// LAYER 228`; `AppVersionStr = "7.0.9"`; Qt в `qt_version.py` (2 раза); SDK и `-vcvars_ver` в `docs/building-win.md`; адрес ccache в `.github/workflows/linux.yml`; тексты `centos_env/build.sh` и `gen_dockerfile.py` (`DEBUG=…`, `LTO=…`), на которые опирается `ci/build-linux.sh` и `ci/linux-env.sh`; `optionsList = ['qt6', …` и `silent` в `prepare.py`; опции `DESKTOP_APP_DISABLE_AUTOUPDATE` и `DESKTOP_APP_DISABLE_CRASH_REPORTS` в `cmake/variables.cmake`; `Telegram/Telegram/Telegram.entitlements` побайтно равен `ci/macos.entitlements`; LF во всех правленых файлах.

## Что проверять при обновлении апстрима

1. Выбрать тег на layer, который говорит сервер (ADR 0008). Обновить в `upstream.env` вместе: тег, коммит, версию, layer, Qt, SDK, MSVC, Xcode образа `macos-26`, ccache. Новый ccache — также SHA-256 трёх архивов в `ci/setup.sh`.
2. `ci/fetch-upstream.sh sparse`, затем тесты. Каждая `PatchError` называет файл и ожидаемый текст: пройти по записям этого реестра для этого файла.
3. `BACKPORTS` (D-053): если новый релиз уже содержит исправление `b244f1b3`, правка перестаёт совпадать — убрать бэкпорт.
4. `BUILD_EDITS` (D-054…D-056): сверить с новым `prepare.py`; `test_build_edits_stay_in_their_platform` требует, чтобы каждая правка лежала в командах одной платформы (`win`, `mac`, `mac`).
5. Строки: `python3 ci/patch.py --strings upstream/Telegram/Resources/langs/lang.strings` → сравнить с `brand/strings.txt`, просмотреть новые строки глазами, переписать таблицу (D-012).
6. Литералы кода: при ошибке `CODE_TELEGRAM` разобрать каждый новый «Telegram» — показать пользователю (добавить в `CODE_NAMES`, D-013) или оставить (добавить в `CODE_TELEGRAM` с причиной).
7. `tg://`: при ошибке счётчиков `SCHEME_FILES` найти новые места `tg://` (D-014).
8. Иконки: `ASSETS` хранит SHA-256 файлов апстрима; изменённая иконка → посмотреть, что поменял апстрим, при нужде перерисовать `brand/svg` и `brand/render.sh` (D-019), обновить хэш.
9. Темы: значения апстрима в `brand/palette-day.txt` и `palette-night.txt` (D-021, D-022); `NIGHT_THEME_SHA256`; `TELEGRAM_THEMES` и `IDENTITY_THEMES` (D-027); новые ключи палитры, которые окажутся синими, остановят `verify_brand()`.
10. `Telegram.entitlements` изменился → решить, что приложению можно, и обновить `ci/macos.entitlements` (D-065).
11. Каталог ассетов macOS: `ci/package.sh` остановит упаковку, если `Assets.car` апстрима держит что-то кроме иконки (D-020).
12. `centos_env` изменился → `linux-env` соберёт новый образ (2–4 ч); проверить, что `gen_dockerfile.py` по-прежнему читает `DEBUG` и `LTO` (D-059).
13. Поведение, которое правки обходят (D-048…D-050, D-070, D-029, D-073): прочитать новые версии `suggestion_birthday_contacts.cpp`, `dialogs_top_bar_suggestion.cpp`, `history_widget.cpp` (`refreshSendGiftToggle`), `info_profile_actions.cpp`, `data_peer.cpp` — исправил ли апстрим сам, не изменился ли смысл. Для D-073 — найти новые проверки Premium у тегов (`grep -rn 'TagsForMessages\|reactionsAreTags'`) и решить, закрывают ли они теги.
14. Пройти разделы «Сервер и ключ» и «Идентичность»: новые сетевые запасные пути (DNS-over-HTTPS, Firebase, новые классы), новые места с именами `Telegram.lnk`, `org.telegram.desktop`, GUID.
15. Собрать dev-сборки трёх систем и проверить на стенде вход, QR (`plm://login?token=`), тёмную и светлую темы, баннер дня рождения.

---

## Сервер и ключ

### D-001. Встроенные датацентры и ключи Telegram заменены ключом сервера Pluma
- **Что и зачем.** Удаляются таблицы встроенных DC Telegram (прод, тест, IPv6) и оба MTProto-ключа Telegram; на их место — ключ сервера Pluma для прод- и тест-бэкенда. Иначе свежий клиент стучится в Telegram и не проходит рукопожатие с нашим сервером.
- **Где.** `Telegram/SourceFiles/mtproto/mtproto_dc_options.cpp`, `replace_block` от `struct BuiltInDc {\n` (стр. 25 в 7.0.9) до `-----END RSA PUBLIC KEY-----" };\n\n} // namespace\n`. `must_contain`: все 11 адресов `DC_ADDRESSES` (`149.154.175.50`, `149.154.167.51`, `95.161.76.100`, …, `2001:0b28:f23d:f003:0000:0000:0000:000e`) и две первые строки ключей Telegram. Новый текст: `const char *kTestPublicRSAKeys[] = { "…" };` и `const char *kPublicRSAKeys[] = { "…" };` с ключом сервера в раскладке tdesktop (`cpp_key`: каждая строка PEM с `\n\` в конце). Перед заменой `expect`: `\t\t? gsl::make_span(kTestPublicRSAKeys)\n\t\t: gsl::make_span(kPublicRSAKeys);\n`.
- **Ключ.** PKCS#1 PEM (`BEGIN RSA PUBLIC KEY`), 2048 бит, e=65537; `Config` отказывает другим размерам и ключам Telegram по отпечатку (`key_fingerprint` = младшие 64 бита SHA-1 от TL-сериализации n и e, как `MTP::details::RSAPublicKey`).
- **Проверка.** `verify()`: ни одного адреса из диапазонов Telegram, ни одной строки ключа Telegram, ключ сервера ровно в 3 копиях. Тесты: `test_patch` (2 копии в `mtproto_dc_options.cpp`), `KeyTest.*`, `ScanTest.*`; `DAMAGE`: изменённый адрес DC5, изменённая строка ключа, `kProductionPublicRSAKeys`.
- **Если апстрим сдвинет.** Новый адрес/ключ, переименованные массивы или другой хвост блока → `PatchError` (замена не найдена или `must_contain` не выполнен). Новый адрес Telegram в другом файле → `verify()` остановит сборку.
- **Апстрим до правки.** 5 прод-DC и 3 тест-DC Telegram с IPv4/IPv6, ключи Telegram с отпечатками выше. Официальный клиент так и работает.
- **Коммит.** `9cbdbf0` (2026-10-27), задача 006.

### D-002. Сервер Pluma — единственный встроенный DC 1
- **Что и зачем.** Тело `DcOptions::constructFromBuiltIn()` заменено: читаются встроенные ключи (`readBuiltInPublicKeys()`), затем `applyOneGuarded(1, Flag::f_static | 0, "<IPv4>", <port>, {});` и, если задан IPv6, то же с `Flag::f_ipv6` (IPv6 записан полностью, `exploded`, как пишет tdesktop). Один и тот же DC для прод- и тест-бэкенда.
- **Где.** `mtproto_dc_options.cpp`, `replace_body` от `void DcOptions::constructFromBuiltIn() {\n` (стр. 166) до `\n}\n`; `must_contain`: `readBuiltInPublicKeys();`, `kBuiltInDcsTest`, `kBuiltInDcsIPv6Test`.
- **Проверка.** `verify()`: нет `kBuiltInDcs`, нужное число `applyOneGuarded(1, Flag::f_static`, адрес и порт сервера. Тесты `test_patch` (IPv4+IPv6, порт 8443), `test_dev_server` (`127.0.0.1`, `10443`, без IPv6); `DAMAGE`: сигнатура `constructFromBuiltIn(bool test)`.
- **Если апстрим сдвинет.** Изменение сигнатуры или исчезновение тестовых таблиц → `PatchError`.
- **Апстрим до правки.** Обход `kBuiltInDcs`/`kBuiltInDcsIPv6` или тестовых таблиц по `isTestMode()`.
- **Конфигурация.** Адрес должен быть IP, не имя (встроенный DC принимает адрес); адрес из диапазонов Telegram отвергается.
- **Коммит.** `9cbdbf0`, задача 006.

### D-003. Свежая установка начинает с DC 1
- **Что.** `\t\tstatic constexpr auto kDefaultMainDc = 2;\n` → `= 1; // pluma: the server is datacenter 1.` Точная замена.
- **Где.** `Telegram/SourceFiles/mtproto/mtp_instance.h`, стр. 43.
- **Проверка.** `verify()` ищет `static constexpr auto kDefaultMainDc = 1;`; `test_patch`; `DAMAGE`: `kDefaultMainDc = 4;`.
- **Апстрим до правки.** Главный DC по умолчанию — 2 (Telegram).
- **Коммит.** `9cbdbf0`, задача 006.

### D-004. Ключ «special config» заменён ключом сервера
- **Что и зачем.** Ключ, которым Telegram подписывает резервные адреса («special config»), заменён ключом сервера, чтобы ни одного ключа Telegram не осталось в клиенте (запросы special config всё равно выключены, D-005).
- **Где.** `Telegram/SourceFiles/mtproto/special_config_request.cpp`, `replace_block` от `constexpr auto kPublicKey = "\\\n` (стр. 26) до `-----END RSA PUBLIC KEY-----\\\n"_cs;\n`; `must_contain`: третья строка `TELEGRAM_KEY_LINES` (`MIIBCgKCAQEAyr+18Rex…`).
- **Проверка.** `verify()` (3 копии ключа сервера); `test_patch` (одна копия в этом файле); `DAMAGE`: `kSpecialConfigKey`.
- **Апстрим до правки.** Ключ Telegram для проверки подписи резервных адресов.
- **Коммит.** `9cbdbf0`, задача 006.

### D-005. Нет резервных адресов через Google, Mozilla и Firebase
- **Что и зачем.** Тело `ConfigLoader::createSpecialLoader()` заменено на `_triedSpecialEndpoints.clear(); _specialLoader = nullptr;`. Telegram публикует резервные адреса в DNS-записях (DNS-over-HTTPS у Google и Mozilla) и в Firebase; они ведут к Telegram.
- **Где.** `Telegram/SourceFiles/mtproto/config_loader.cpp`, `replace_body` от `void ConfigLoader::createSpecialLoader() {\n` (стр. 138); `must_contain`: `std::make_unique<SpecialConfigRequest>(`.
- **Проверка.** `verify()` (`CONSTRUCTION`: класс нигде не конструируется); `test_patch`; `DAMAGE`: сигнатура `createSpecialLoader(bool force)`, вставка `make_unique<SpecialConfigRequest>` в `refreshSpecialLoader`.
- **Апстрим до правки.** Создаёт `SpecialConfigRequest` (DoH Google/Mozilla, Firebase Remote Config/Firestore) с `txtDomainString` и телефоном.
- **Коммит.** `9cbdbf0`, задача 006.

### D-006. Нет времени из HTTP-заголовков Google, Mozilla, Firebase
- **Что.** Тело `Instance::Private::syncHttpUnixtime()` заменено комментарием (пустая функция): часы по-прежнему поправляют сообщения сервера.
- **Где.** `Telegram/SourceFiles/mtproto/mtp_instance.cpp`, `replace_body` от `void Instance::Private::syncHttpUnixtime() {\n` (стр. 543); `must_contain`: `std::make_unique<SpecialConfigRequest>(`.
- **Проверка.** `verify()`, `test_patch`; `DAMAGE`: сигнатура `syncHttpUnixtime(bool force)`.
- **Апстрим до правки.** Создаёт `SpecialConfigRequest` ради заголовка `Date`.
- **Коммит.** `9cbdbf0`, задача 006.

### D-007. Имена прокси — через системный резолвер
- **Что.** Тело `ProxyData::tryCustomResolve() const` → `return false;`.
- **Где.** `Telegram/SourceFiles/mtproto/mtproto_proxy_data.cpp`, `replace_body` от `bool ProxyData::tryCustomResolve() const {\n` (стр. 165); `must_contain`: `Type::Socks5`, `is_ipv6(host)`.
- **Проверка.** `verify()` — регулярное выражение «тело — только комментарии и `return false;`»; `DAMAGE`: условие без `Socks5`.
- **Апстрим до правки.** Для SOCKS5 и MTProto-прокси с именем хоста (не IP) — DoH у Google и Mozilla (`MTP::details::DomainResolver`).
- **Сверка клиентов (задача 020).** iOS и Desktop резолвят системно; Android ходит через DoH Google — перекос.
- **Коммит.** `9cbdbf0`, задача 006.

### D-008. `resolveProxyDomain` никогда не создаёт `DomainResolver`
- **Что.** Тело `Instance::Private::resolveProxyDomain(const QString &host)` → `Q_UNUSED(host);` (после D-007 не вызывается).
- **Где.** `mtp_instance.cpp`, `replace_body` от `void Instance::Private::resolveProxyDomain(const QString &host) {\n` (стр. 390); `must_contain`: `std::make_unique<DomainResolver>(`.
- **Проверка.** `verify()` (`CONSTRUCTION`), `test_patch`; `DAMAGE`: `_domainResolver.reset(new DomainResolver(…`.
- **Коммит.** `9cbdbf0`, задача 006.

### D-009. Автообновление и отчёты о сбоях вырезаны при компиляции
- **Что и зачем.** Updater Telegram скачал бы сборки Telegram (другой layer, другие ключи); crash reports шлют на `tdesktop.com`. Условные `target_compile_definitions` заменены безусловными `TDESKTOP_DISABLE_AUTOUPDATE TDESKTOP_DISABLE_CRASH_REPORTS`, а конфигурация без обеих опций останавливается `message(FATAL_ERROR "Pluma: configure with -D DESKTOP_APP_DISABLE_AUTOUPDATE=ON -D DESKTOP_APP_DISABLE_CRASH_REPORTS=ON")`.
- **Где.** `Telegram/cmake/telegram_options.cmake`, точная замена блока `CMAKE_OPTIONS_OLD` (от `if (DESKTOP_APP_DISABLE_AUTOUPDATE)`, стр. 39, до `endif()` блока crash reports). Плюс `check_upstream()` требует опции в `cmake/variables.cmake` (сабмодуль `cmake`): `option(DESKTOP_APP_DISABLE_AUTOUPDATE "Disable autoupdate."` и `option(DESKTOP_APP_DISABLE_CRASH_REPORTS "Disable crash report generation."`.
- **Сборка.** Все три сборочных скрипта передают `-D DESKTOP_APP_DISABLE_AUTOUPDATE=ON -D DESKTOP_APP_DISABLE_CRASH_REPORTS=ON` (D-010).
- **Проверка.** `verify()`: есть `CMAKE_DEFINITIONS`, нет `if (DESKTOP_APP_DISABLE_AUTOUPDATE)`; `test_patch`; `DAMAGE`: `TDESKTOP_DISABLE_AUTOUPDATE=1`, переименование опции `DESKTOP_APP_NO_AUTOUPDATE`.
- **Остаётся.** 1024-битные ключи пакетов обновлений Telegram в `config.h` и `_other/packer.cpp` (`UPDATE_KEY_LINES`): ничего не проверяют, updater не собирается.
- **Коммит.** `9cbdbf0`, задача 006.

### D-010. `api_id`, `api_hash` и опции CMake — параметры сборки, не правки
- **Что.** `-D TDESKTOP_API_ID=$PLUMA_API_ID -D TDESKTOP_API_HASH=$PLUMA_API_HASH` (10003, `c62739c93dc105640862bf4dcea3ba1d` из `config/app.env`; не секрет) вместе с опциями D-009 передаются CMake:
  - Linux: `ci/build-linux.sh` → `centos_env/build.sh` (`-D CMAKE_CONFIGURATION_TYPES=Release`, `CMAKE_C/CXX_FLAGS_RELEASE="-O3 -DNDEBUG -fpch-preprocess"`);
  - Windows: `ci/windows.sh client` → `configure.bat -G "Ninja Multi-Config" qt6 … -D CMAKE_CONFIGURATION_TYPES=Release -D CMAKE_MSVC_DEBUG_INFORMATION_FORMAT=`;
  - macOS: `ci/macos.sh client` → `./configure.sh -D CMAKE_CONFIGURATION_TYPES=Release -D CMAKE_XCODE_ATTRIBUTE_CODE_SIGNING_ALLOWED=NO …`.
- **Проверка.** `ConfigTest` (`api_id` > 0, `api_hash` — 32 строчные hex-цифры). Сам факт передачи — только по сборке (вход на стенде с `api_id` 10003, задача 006).
- **Апстрим до правки.** Без `TDESKTOP_API_ID/HASH` CMake апстрима сборку не настраивает (официальные сборки несут ключи Telegram Desktop). В `api/api_authorizations.cpp` остаётся `api_id` Telegram Desktop для списка сессий (`CODE_TELEGRAM`).
- **Коммиты.** `9cbdbf0` (значения), `b441350` (скрипты сборки), `006c33d` (Windows: без отладочной информации), задача 006.

### D-011. Адрес и ключ сервера: dev-конфигурация и переменные репозитория
- **Что.** `config/dev/server.env` (`PLUMA_SERVER_IPV4=127.0.0.1`, `PLUMA_SERVER_PORT=10443`) и публичный dev-ключ `config/dev/server_public.pem` (публичный, закоммичен); для сервера — переменные Actions `PLUMA_SERVER_IPV4`, `PLUMA_SERVER_PORT`, `PLUMA_SERVER_IPV6`, `PLUMA_RSA_PUBLIC_KEY`. Половина переменных → job `check` падает (`configure.sh server`).
- **Проверка.** `ConfigTest.test_dev_server`, `KeyTest.test_dev_key_fingerprint`, `UpstreamTest.test_dev_server`; job `check` патчит и под dev, и под сервер.
- **Коммиты.** `9cbdbf0`, `b441350`, задача 006.

---

## Брендинг и тексты

Источник: задача 017 серверного репозитория (палитра «графит и шалфей», схема `plm://`).

### D-012. Встроенный английский: «Telegram» → «Pluma» в 321 строке
- **Что и зачем.** В `lang.strings` каждое «Telegram» вне ссылки, включая «Telegram Desktop», «Telegram Premium», «Telegram Stars», заменяется именем (`PLUMA_APP_NAME`); «Telegram Desktop» → «Pluma» целиком. Ссылки (`telegram.org`, `telegram.me`, `t.me`, `fragment.com`, регэксп `LANG_LINK`) не трогаются. Исключение `LANG_KEEP`: `lng_about_text1_api` = «Telegram API» (упоминание протокола в «О программе»). Другие языки приходят языковым пакетом сервера.
- **Где.** `Telegram/Resources/langs/lang.strings`, регулярное выражение по записям `LANG_ENTRY` (`"key" = "value";`), замена значения функцией `name_telegram`. Ключи и число вхождений закреплены в `brand/strings.txt` (321 ключ, 357 вхождений по коммиту); если найденная таблица отличается — `PatchError` с новыми/пропавшими/изменёнными ключами. Запись в форме, которую регэксп не читает, тоже ошибка (`lang_entries`).
- **Проверка.** `verify_brand()` (ни одна строка не называет Telegram, нет «Teamgram»); `BrandTest.test_strings_table`, `test_name_telegram`, `test_lang_entries`; `test_brand` (`lng_intro_qr_title` = «Scan From Mobile Pluma», `lng_premium_summary_title` = «Pluma Premium», ссылка `lng_telegram_features_url` нетронута); `DAMAGE`: новая строка с Telegram, изменённое число, изменённый `lng_about_text1_api`.
- **Если апстрим сдвинет.** Любая новая/изменённая строка с «Telegram» → ошибка; таблицу печатает `python3 ci/patch.py --strings <lang.strings>`.
- **Коммит.** `7b07c40` (2026-10-29), задача 017.

### D-013. «Telegram» в текстах кода → «Pluma» (`CODE_NAMES`)
- **Что.** Точные замены `replace` с числом вхождений; `{name}` = `PLUMA_APP_NAME`:

| Файл (`Telegram/SourceFiles/…` если не указано) | Текст апстрима | Раз |
|---|---|---|
| `intro/intro_start.cpp` | `setTitleText(rpl::single(u"Telegram Desktop"_q));` | 1 |
| `boxes/about_box.cpp` | `box->setTitle(u"Telegram Desktop"_q);` | 1 |
| `window/main_window.cpp` | `(user.isEmpty() ? u"Telegram"_q : user)` | 1 |
| `window/notifications_manager_default.cpp` | `TextWithEntities{ u"Telegram Desktop"_q }` | 1 |
| `settings/sections/settings_notifications.cpp` | `elided(u"Telegram Desktop"_q, rectForName.width())` | 1 |
| `platform/mac/window_title_mac.mm` | `p.drawText(titleRect, u"Telegram"_q, style::al_center);` | 1 |
| `platform/mac/global_menu_mac.mm` | `u"About Telegram"_q,`; `u"Show Telegram"_q,`; `_menuBar->addMenu(u"Telegram"_q)` | 1+1+1 |
| `platform/linux/main_window_linux.cpp` | `u"Telegram"_q)` («Quit/About {telegram}») | 2 |
| `platform/win/specific_win.cpp` | `L"Telegram autorun link.\n" … "in Telegram settings."`; то же для «send to link» | 1+1 |
| `ui/controls/round_video_recorder.cpp` | `u"Telegram"_q.toUpper(),` (кольцо текста вокруг записи кружка) | 1 |
| `ui/chat/attach/attach_bot_webview_linux_shell.cpp` | `"document.title = 'Telegram';"` | 1 |
| `Telegram/Resources/bot_webview_shell_html/page.js` | `document.title = (data && data.title) \|\| 'Telegram';` | 2 |
| `data/data_session.cpp` | `MTP_string("Telegram"),` (имя служебного аккаунта 777000 до ответа сервера) | 1 |
| `data/data_message_reactions.cpp` | `.title = u"Telegram Star"_q,` | 1 |
| `lang/lang_hardcoded.h` | `Telegram Passport` | 5 |
| `export/output/export_output_html.cpp` | `"of Telegram Desktop. Please update the application."`; `" joined Telegram";`; `Telegram Premium` (7); `" Telegram Stars."` (5); `"Telegram and received` (2); `"selected by Telegram and received` (2); `" was randomly selected by Telegram."` | — |
| `core/crash_report_window.cpp` | `setWindowTitle(title.isEmpty() ? u"Telegram"_q : title);`; `u"Could not start Telegram Desktop!\nYou can see complete log below:"_q` | 1+1 |
| `core/external_control.cpp` | `u"Telegram Desktop over the local socket`; `u"Telegram window to enable"_q` | 1+1 |
| `core/mime_type.cpp` | `u"Telegram sticker (*.tgs)"_q` | 1 |
| `core/shortcuts.cpp` | заголовки файлов `// This is a list of default/changed shortcuts for Telegram Desktop` | 1+1 |
| `Telegram/Resources/default_shortcuts-custom.json` | `// This is a list of your own shortcuts for Telegram Desktop` | 1 |

- **Что остаётся намеренно (`CODE_TELEGRAM`, счётчик по файлу).** Updater и его задачи (`_other/startup_task_win.cpp` 1, `updater_linux.cpp` 4, `updater_osx.m` 2, `updater_win.cpp` 9, `core/update_checker.cpp` 1); строки логов (`api/api_updates.cpp`, `core/crash_reports.cpp`, `core/launcher.cpp` 2, `core/sandbox.cpp`); окна crash report, вырезанные компиляцией (`core/crash_report_window.cpp` 7); сборки, которых Pluma не делает — альфа, Mac App Store, Windows Store (`boxes/about_box.cpp`, `core/file_utilities.cpp`, `platform/win/specific_win.cpp`, `platform/win/windows_app_user_model_id.cpp`); `api_id` Telegram Desktop в списке сессий (`api/api_authorizations.cpp`); имена в форматах, которые никто не читает (`core/click_handler_types.cpp` PRODID календаря, `iv/iv_rich_page.h`, `export/output/export_output_abstract.cpp`, `payments/payments_form.cpp` — имя компании для Stripe, `platform/mac/launcher_mac.mm`).
- **Проверка.** `verify_brand()` считает «Telegram» как слово в строковых литералах всех исходников (ссылки и комментарии не считаются, `code_telegram`) и сравнивает с `CODE_TELEGRAM`; `BrandTest.test_code_telegram`; `test_brand`; `DAMAGE`: `intro_start.cpp` с `u"Telegram"_q`, `export_output_html.cpp` с `" has joined Telegram"`, новый литерал в `main_window.cpp`.
- **Если апстрим сдвинет.** Изменённый текст → `PatchError` в `replace`; новый литерал с «Telegram» где угодно → `verify_brand()`.
- **Коммит.** `7b07c40` (2026-10-29), задача 017. Ранее (`9cbdbf0`) были только имена идентичности (D-035…D-046).

### D-014. Ссылки `plm://` вместо `tg://` в исходниках
- **Что и зачем.** Схема Pluma — `plm://` (задача 017). Во всех `tg://` 23 файлов исходников (67 вхождений) — `plm://`: приложение открывает `plm://`, превращает в них `t.me` как прежде и создаёт их везде, где создавал `tg://` (QR входа `plm://login?token=…`, скопированные ссылки настроек и постов). `tg://` и `tonsite://` остаются установленному рядом Telegram Desktop.
- **Где.** Точная замена `tg://` → `plm://` с числом вхождений по файлу (`SCHEME_FILES`): `api/api_single_message_search.cpp` 3, `boxes/connection_box.cpp` 3, `core/application.cpp` 3, `core/click_handler_types.cpp` 2, `core/deep_links/deep_links_settings.cpp` 2, `core/local_url_handlers.cpp` 30, `core/ui_integration.cpp` 1, `data/data_cloud_themes.cpp` 1, `data/data_story.cpp` 1, `history/history_drag_area.cpp` 2, `history/view/media/history_view_theme_document.cpp` 1, `history/view/media/history_view_userpic_suggestion.cpp` 1, `inline_bots/bot_attach_web_view.cpp` 2, `intro/intro_qr.cpp` 1, `iv/editor/iv_editor_clipboard_import.cpp` 1, `iv/iv_rich_message_html_export.cpp` 4, `media/stories/media_stories_controller.cpp` 1, `menu/menu_ttl_validator.cpp` 1, `settings/sections/settings_main.cpp` 1, `settings/sections/settings_privacy_security.cpp` 1, `settings/settings_experimental.cpp` 1, `ui/boxes/about_cocoon_box.cpp` 1, `window/window_filters_favorite.cpp` 3.
- **Проверка.** `verify_brand()`: ни одного `tg://` во всех исходниках; `test_brand` (`_qrCodes.fire_copy("plm://login?token=" + encoded);`, `openCustomUrl("plm://", …)`); `DAMAGE`: QR на `https://t.me/login?token=`, новый `tg://resolve` в `core/sandbox.cpp`.
- **Если апстрим сдвинет.** Изменённое число `tg://` в файле → `PatchError`; новый файл с `tg://` → `verify_brand()`.
- **Поведение официального клиента.** Telegram Desktop создаёт и открывает `tg://`. Задача 017: Android и iOS Pluma принимают `plm://login?token=` — QR-вход между клиентами работает, когда все на `plm://`. Задача 026 (ссылки `pluma.chat` вместо `t.me`) ещё `todo`: `t.me` в сообщениях открываются внутри Pluma, как в Telegram.
- **Коммит.** `7e6c8d6` (2026-10-29), задача 017. В `9cbdbf0` было иначе: схема `tg://` оставалась в коде, но не регистрировалась при первом запуске (`autoRegisterUrlScheme` опустошался), `CFBundleURLTypes` удалялся из Info.plist, `MimeType` — из `.desktop`; `7e6c8d6` это заменил.

### D-015. Регистрация в системе только `plm://`
- **Что.** Тело `Application::RegisterUrlScheme()` заменено одной регистрацией: `.protocol = u"plm"_q`, `.protocolName = u"Pluma Link"_q`, `.shortAppName = u"pluma"_q`, `.longAppName = QCoreApplication::applicationName()`, `.displayAppName/.displayAppDescription = AppName.utf16()`. Регистрация при первом запуске апстрима (`if (!OptionSkipUrlSchemeRegister.value()) { InvokeQueued(this, [] { RegisterUrlScheme(); });`) не тронута; ввод `registertg` в настройках регистрирует `plm://` снова.
- **Где.** `Telegram/SourceFiles/core/application.cpp`, `replace_body` от `void Application::RegisterUrlScheme() {\n` (стр. 1931) до `\n}\n`; `must_contain`: `.protocol = u"tg"_q`, `.protocol = u"tonsite"_q`, `u"Telegram Link"_q`.
- **Проверка.** `verify_brand()`: ровно один `base::Platform::RegisterUrlScheme(`, есть `.protocol = u"plm"_q` и регистрация при первом запуске; `test_brand`; `DAMAGE`: `RegisterUrlSchemes()`, `.protocol = u"telegram"_q`.
- **Апстрим до правки.** Регистрирует `tg` («Telegram Link») и `tonsite` («TonSite Link»), `shortAppName = "tdesktop"`.
- **Коммит.** `7e6c8d6`, задача 017.

### D-016. Info.plist объявляет только `plm`
- **Что.** `\t\t\t\t<string>tg</string>\n\t\t\t\t<string>tonsite</string>\n` → `\t\t\t\t<string>plm</string>\n` в `CFBundleURLSchemes`.
- **Где.** `Telegram/Telegram.plist` (стр. 40), точная замена.
- **Проверка.** `verify_brand()` (`<string>plm</string>\n\t\t\t</array>`); `verify()`: нет `<string>tg</string>`, `<string>tonsite</string>`; `test_patch`; `DAMAGE`: удалённый `tonsite`.
- **Коммит.** `7e6c8d6`, задача 017.

### D-017. lib_ui распознаёт `plm` как локальную ссылку в тексте
- **Что.** `\taddOne(QString::fromLatin1("tg")); // local urls\n` → `"plm"` `// local urls (pluma)`.
- **Где.** Сабмодуль `Telegram/lib_ui`, `ui/text/text_entity.cpp` (стр. 185), точная замена.
- **Проверка.** `verify_brand()`; `test_brand`; `DAMAGE`: изменённый комментарий.
- **Коммит.** `7e6c8d6`, задача 017.

### D-018. lib_base считает `plm` ссылкой
- **Что.** `\t\t|| equals(qstr("tg"))\n` → `\t\t|| equals(qstr("plm")) // pluma\n`.
- **Где.** Сабмодуль `Telegram/lib_base`, `base/qthelp_url.cpp` (стр. 63), точная замена.
- **Проверка.** `verify_brand()` (нет `qstr("tg")`); `DAMAGE`: добавленный `tg2`.
- **Коммит.** `7e6c8d6`, задача 017.

### D-019. Иконки Pluma вместо иконок Telegram (60 файлов)
- **Что.** Файлы `brand/tree/<путь апстрима>` записываются поверх файлов апстрима, только если SHA-256 файла апстрима равен закреплённому в `ASSETS`. Состав:
  - `Telegram/Resources/art/`: `icon{16,32,48,64,128,256,512}.png` и `@2x`, `icon256.ico` (иконка exe: BMP до 48, PNG на 256, как у апстрима), `icon_round512@2x.png` (круглый вариант macOS), `logo_256.png` (окно, панель задач, трей Windows/Linux; тело 228 из 256), `logo_256_no_margin.png` (круглый логотип без поля: мелкие иконки трея, уведомления);
  - `Telegram/Resources/icons/`: альфа-маски 1x/2x/3x `intro_plane_{inner,outer,top,trace}` (знак на заставке: кольцо и три пера, окрашиваются палитрой), `intro_qr_plane` (знак в центре QR), `mac_tray_icon` (строка меню macOS); SVG `plane_white.svg`, `tray_monochrome{,_attention,_mute}.svg`;
  - `Telegram/Telegram/Images.xcassets/Icon.appiconset/*` и `Icon.iconset/*` (иконка macOS).
- **Источник.** `brand/svg/*.svg` → `brand/render.sh` (rsvg-convert + ImageMagick 7, без меток времени, воспроизводимо); результат закоммичен, сборке инструменты не нужны.
- **Проверка.** `verify_brand()`: каждый файл равен `brand/tree`; `BrandTest.test_tree_is_the_assets`; `test_brand`; `test_changed_binary_files` (изменённая иконка апстрима останавливает патч).
- **Если апстрим сдвинет.** Изменённая иконка апстрима → `PatchError` «not the pinned upstream's file»; нужно посмотреть изменение и перерисовать. Новая иконка в другом месте не будет замечена — ищите глазами.
- **Коммиты.** `9569cc4` (рендер, 2026-10-29), `736e7fd` (применение), `8a3b45d` (`logo_256_no_margin` сделан круглым, как у апстрима: квадрат резался кругом аватара служебного аккаунта), задача 017.

### D-020. Иконка macOS 26 из Icon Composer
- **Что и зачем.** macOS 26 рисует иконку старого формата на серой подложке. `ci/package.sh macos` компилирует `brand/Icon.icon` (знак на белом, без стекла, тени и бликов; `icon.json` + `Assets/mark.png` из `brand/svg/macos-mark.svg`) через `xcrun actool --app-icon Icon` и кладёт `Assets.car` и `Icon.icns` в `Contents/Resources` поверх апстрима; actool рендерит и для старых систем.
- **Где.** Не правка исходников: шаг упаковки. Опирается на `CFBundleIconName = Icon` и `CFBundleIconFile = Icon` в Info.plist и на то, что `Assets.car` апстрима содержит только иконку (иначе упаковка падает: «the upstream asset catalog holds more than the icon»).
- **Проверка.** Только сборкой macOS в CI; локально проверено (задача 017). Тестов нет.
- **Коммит.** `cd537bf` (2026-10-29), задача 017.

### D-021. Светлая тема по умолчанию (Classic): палитра «графит и шалфей»
- **Что.** 94 цвета `Telegram/lib_ui/ui/colors.palette` (сабмодуль lib_ui) заменяются по таблице `brand/palette-day.txt` (`ключ: значение апстрима -> значение Pluma`). Например `windowBgActive: #40a7e3 -> #5b7a5e`, `windowFg: #000000 -> #111111`, `windowActiveTextFg: #168acd -> #4f6e52`. Акцент `#5b7a5e`, ссылки `#4f6e52`, исходящие пузыри `#e3eae2`. Остаются цвета типов файлов, тревоги, серые, палитры звонков и Premium, фон чата (узор Telegram, `data/data_wall_paper.cpp`).
- **Где.** `recolor()`: каждая строка `\nключ: старое;` должна встретиться ровно один раз; старое `-` значит «добавить ключ, которого в апстриме нет».
- **Проверка.** `verify_brand()`: после разрешения ссылок ни один ключ не синий (`is_blue`: тон 185–240°) и не из `TELEGRAM_BLUES`, кроме `KEPT_BLUE` (`msgFile1*`, `mediaviewFileBlueCornerFg`, `settingsIconBg[45]`, `stickerPanPremium[12]`, `premiumButtonBg1`, `statisticsChart*`, `groupCall*`); `BrandTest.test_palette_tables`, `test_recolor`, `test_load_table_errors`; `test_brand`; `DAMAGE`: `windowBgActive: #40a7e4`, новый ключ `newAccentFg: #3390ec`.
- **Если апстрим сдвинет.** Изменённое значение → `PatchError` «upstream changed the colour»; новый синий ключ → `verify_brand()`.
- **Сверка клиентов (задача 020).** Светлый фон чата — узор Telegram в Android и Desktop, свой в iOS и Web K.
- **Коммиты.** `736e7fd` (2026-10-29), задача 017; `831289b` (2026-10-31) поправил таблицу под цвета-идентичности.

### D-022. Тёмная тема по умолчанию (Tinted): палитра и zip заново
- **Что.** `Telegram/Resources/night.tdesktop-theme` — zip из `colors.tdesktop-theme` и `background.png`. Патч проверяет SHA-256 (`NIGHT_THEME_SHA256 = 364fcd72…`), что в zip ровно эти два файла, меняет 207 строк по `brand/palette-night.txt` (одна добавляет ключ: `historyPeerSavedMessagesBg: - -> #7fa082`), применяет цвета-идентичности (D-027) и пишет zip заново детерминированно (дата 1980-01-01, deflate) с фоном 1×1 `#0f110f`.
- **Проверка.** `verify_brand()` (нет синего, кроме `KEPT_BLUE`); `test_brand` (`windowBg #131513`, `msgOutBg #2c3a2d`); `BrandTest.test_night_theme_is_stable`; `test_changed_binary_files`.
- **Если апстрим сдвинет.** Любое изменение файла → `PatchError` по SHA-256: сверить таблицу и обновить хэш.
- **Коммиты.** `736e7fd`, `831289b`, задачи 017 и 020.

### D-023. Плитка Classic в «Настройках чатов» и база выбора акцента
- **Что.** В `Telegram/SourceFiles/window/themes/window_themes_embedded.cpp` блок `EmbeddedType::Default` (стр. 241): `qColor("eaffdc")` → `"e3eae2"` (дважды) и последний `qColor("40a7e3")` → `qColor("5b7a5e") // pluma: the accent of brand/palette-day.txt`. Точная замена блока `EMBEDDED_CLASSIC`.
- **Проверка.** `test_brand`, `test_damage` (общий для D-023…D-025: `qColor("5288c2")`).
- **Коммит.** `736e7fd`, задача 017.

### D-024. Плитка Tinted и её акцент
- **Что.** Блок `EmbeddedType::Night` того же файла: цвета плитки `485761/5ca7d4/6b808d` → `3a423a/7fa082/5f685f`, акцент `5288c1` → `9dbb9f`. Точная замена `EMBEDDED_NIGHT`.
- **Проверка.** `test_brand` (`qColor("9dbb9f") // pluma`); `DAMAGE`.
- **Коммит.** `736e7fd`, задача 017.

### D-025. Контраст текста на акценте ночной темы (`keepContrast`)
- **Что.** Таблица `result.keepContrast` (стр. 136): пары синих Telegram заменены парами `9dbb9f`/`b2ceb4` → `ffffff` (у Pluma текст на акценте тёмный; тёмный акцент, выбранный пользователем, делает его белым). Убрана закомментированная строка `windowFgActive`. Точная замена `NIGHT_CONTRAST`.
- **Проверка.** Только точностью замены (`PatchError`) и `test_patch`/`test_brand` через прогон патча; отдельного утверждения нет.
- **Коммит.** `736e7fd`, задача 017.

### D-026. Круг под знаком в центре QR-кодов
- **Что.** `QColor(0x40, 0xA7, 0xE3)` → `QColor(0x5B, 0x7A, 0x5E)` в `intro/intro_qr.cpp` (стр. 61, вход по QR) и `boxes/connection_box.cpp` (стр. 158, QR прокси). Точные замены.
- **Проверка.** `test_brand`; `DAMAGE`: без комментария `// Default windowBgActive.`.
- **Коммит.** `736e7fd`, задача 017.

### D-027. Цвета-идентичности людей: синий → шалфей, бирюзовый → teal во всех темах
- **Что и зачем.** Telegram даёт пользователю/чату один из семи цветов по id (`id % 7`: 0 красный, 1 оранжевый, 2 фиолетовый, 3 зелёный, 4 бирюзовый, 5 синий, 6 розовый) для буквенного аватара (градиент), имени в группах и полос ответов/превью. Синий — слот 4 палитры (`historyPeer4*`), бирюзовый — слот 7. По `IDENTITY_COLORS`:
  - `historyPeer4NameFg`: светлые `#4f6e52`, тёмные `#9dbb9f`;
  - `historyPeer4UserpicBg` `#5caffa` → `#7fa082`, `historyPeer4UserpicBg2` `#408acf` → `#5b7a5e`;
  - `historyPeer7NameFg`: светлые `#1f8a80`, тёмные `#53ccc6` (teal ночного Android-клиента Telegram);
  - `historyPeer7UserpicBg` `#5bcbe3` → `#5bcbc4`, `historyPeer7UserpicBg2` `#359ad4` → `#2c9c98`.
  Остальные пять цветов и цвета от 7 (сервер, `help.getPeerColors`) остаются Telegram. Следуют им: иконки сессий в «Устройствах», заглушка фото, картинка ссылки на папку, иконки фильтров папок, бусты, аватары «Избранного»/ответов/заметок в Day, Night и базах (их `historyPeerSavedMessagesBg2` ссылается на синий слот), `st::colorIndexBlue` (на деле бирюзовый, id 4) и др. — полный список в комментарии `IDENTITY_COLORS`.
- **Где.** Шесть тем (`IDENTITY_THEMES`): `colors.palette` (lib_ui), `night.tdesktop-theme`, `day-blue.tdesktop-theme`, `night-green.tdesktop-theme`, `day-custom-base.tdesktop-theme`, `night-custom-base.tdesktop-theme`. Регулярное выражение `^ключ: <значение Telegram>( | fallback)?;` должно совпасть ровно раз; значение Telegram — общее или своё у темы (например имя бирюзового `#2996ad` в светлых, `#62d4e3` в Tinted, `#57c9e0` в Night). Четыре zip-темы закреплены SHA-256 (`TELEGRAM_THEMES`) и пишутся заново с изменённым только `colors.tdesktop-theme`.
- **Проверка.** `verify_brand()`: в каждой теме каждый ключ разрешается в значение Pluma (или остаётся Telegram, где значение `None`); `BrandTest.test_identity_tables`, `test_identity`, `test_with_colors`; `UpstreamTest.test_people_colours`, `test_people_colours_are_verified`, `test_people_colours_kept`; `test_changed_binary_files`; `DAMAGE`: `historyPeer7NameFg: #2996ae`.
- **Если апстрим сдвинет.** Иное значение или изменённая zip-тема → `PatchError`.
- **Поведение официального клиента.** Синий и бирюзовый Telegram. Значения взяты из Android Pluma (утверждены владельцем); `#53ccc6` в тёмных — решение на подтверждение (задача 020).
- **Коммиты.** `831289b` (2026-10-31), `ff05bde` (2026-10-31: тёмные имена бирюзового тоже teal, исключение `historyPeer7NameFg` из `KEPT_BLUE` убрано), задача 020.

### D-028. Буквенные аватары HTML-экспорта
- **Что.** В `Telegram/Resources/export_html/css/style.css` правила `.color_blue,\n.userpic4,\n.media_audio_file .fill,\n.media_voice_message .fill { background-color: #4f9cd9; }` и `.color_sea,\n.userpic7,\n.media_location .fill,\n.media_video .fill { background-color: #47bcd1; }` теряют `.userpic4`/`.userpic7`, а после них добавлены `.userpic4 { background-color: #6d8d70; /* pluma */ }` и `.userpic7 { background-color: #44b4ae; /* pluma */ }` — середина градиента Pluma (`export_userpic`). Иконки медиа, делившие правило, остаются цвета Telegram.
- **Проверка.** `verify_brand()`: `.userpicN` в единственном правиле с этим цветом; `DAMAGE`: `.media_audio .fill`.
- **Коммит.** `831289b`, задача 020.

### D-029. Служебный аккаунт рисуется по своему фото, а не логотипом приложения
- **Что и зачем.** В `PeerData` удалена ветка `} else if (isNotificationsUser()) { static auto result = Window::LogoNoMargin().scaledToWidth(…); return &result; }`. Сервер даёт аккаунту 777000 фото (`pluma-admin service-photo`), до загрузки — буква. Апстрим рисовал логотип, а список чатов держал закэшированный логотип поверх загруженного фото (кэш перерисовывается только после буквы).
- **Где.** `Telegram/SourceFiles/data/data_peer.cpp` (стр. 443), точная замена.
- **Проверка.** `test_brand` (нет `isNotificationsUser()`); `DAMAGE`: `Window::Logo()`.
- **Поведение официального клиента.** Telegram рисует логотип (у служебного аккаунта Telegram фото нет — не проверено).
- **Коммит.** `deef719` (2026-10-29), задача 017.

### D-030. В «Настройках» нет FAQ, Telegram Features и Ask a Question
- **Что и зачем.** Тело `BuildHelpSection(SectionBuilder &builder)` → `Q_UNUSED(builder);`. Они ведут на страницы и поддержку Telegram (`help.getSupport` сервер не обслуживает).
- **Где.** `Telegram/SourceFiles/settings/sections/settings_main.cpp`, `replace_body` от `void BuildHelpSection(SectionBuilder &builder) {\n` (стр. 574); `must_contain`: `OpenFaq(controller);`, `lng_telegram_features_url`, `OpenAskQuestionConfirm(controller);`.
- **Проверка.** `test_brand`; `DAMAGE`: `OpenAskQuestion(controller)`.
- **Коммит.** `a1fa34a` (2026-10-29), задача 017.

### D-031. Нет FAQ в настройках экрана входа
- **Что.** Удалены `Ui::AddDivider`, `Ui::AddSkip` и кнопка `tr::lng_settings_faq()` с `OpenFaq(nullptr);`.
- **Где.** `Telegram/SourceFiles/settings/settings_intro.cpp` (около стр. 113), точная замена блока.
- **Проверка.** `test_brand` (нет `OpenFaq`); `DAMAGE`: `OpenFaq({})`.
- **Коммит.** `a1fa34a`, задача 017.

### D-032. Нет FAQ в «О программе»
- **Что.** Удалена функция `Text3()` (`replace_block` от `rpl::producer<TextWithEntities> Text3() {\n` до `}\n\n`, `must_contain`: `telegramFaqLink()`) и её вызов `\taddText(Text3());\n`.
- **Где.** `Telegram/SourceFiles/boxes/about_box.cpp` (стр. 57 и 128).
- **Проверка.** `test_brand` (нет `Text3`); `DAMAGE`: `addText(Text3(), true)`.
- **Коммит.** `a1fa34a`, задача 017.

### D-033. Подвал главного меню без ссылки на desktop.telegram.org
- **Что.** `_telegram->setMarkedText(tr::link(u"Telegram Desktop"_q, u"https://desktop.telegram.org"_q));` → `_telegram->setMarkedText(TextWithEntities{ u"Pluma"_q });`.
- **Где.** `Telegram/SourceFiles/window/window_main_menu.cpp` (стр. 386–387), точная замена.
- **Проверка.** `test_brand`; `DAMAGE`: другой URL.
- **Сверка клиентов (задача 020).** Ссылки на сайты клиентов Telegram в других текстах Desktop остаются — открытый перекос.
- **Коммит.** `a1fa34a`, задача 017.

### D-034. Заблокированный номер: без «Help», писавшего на login@stel.com
- **Что.** Удалена `SendToBannedHelp` (`replace_block` от `namespace {\n\nvoid SendToBannedHelp(` до `} // namespace\n\n`, `must_contain`: `"login@stel.com"`); тело функции показа (`replace_body` от `\t\tconst QString &phone) {\n`, `must_contain`: `SendToBannedHelp(phone);`) → `controller->show(Ui::MakeInformBox(tr::lng_signin_banned_text()), Ui::LayerOption::CloseOther);`.
- **Где.** `Telegram/SourceFiles/boxes/phone_banned_box.cpp` (стр. 22, 63).
- **Проверка.** `test_brand`; `DAMAGE`: `recover@telegram.org`.
- **Сверка клиентов (задача 020).** Письма в поддержку Telegram с экранов входа убраны только в Desktop; Android и iOS оставляют.
- **Коммит.** `a1fa34a`, задача 017.

### Идентичность приложения (сосуществование с Telegram Desktop)

Значения — `config/app.env` (D-047). Смена любого после выпуска делает приложение для системы другим (новая папка данных, новые ярлыки).

### D-035. Свой GUID приложения (Windows, ключ удаления)
- **Что.** Регулярное выражение `constexpr auto AppId = "\{[0-9A-F-]{36}\}"_cs;` → `"{BB4EA67A-F90A-4B50-B202-49660CE32557}"_cs; // pluma: Pluma's own`.
- **Где.** `Telegram/SourceFiles/core/version.h` (стр. 21), `sub`, 1 совпадение.
- **Проверка.** `test_patch`; `Config` отвергает GUID Telegram Desktop (`TELEGRAM_GUIDS`) и совпадающие GUID.
- **Апстрим до правки.** `{53F49750-6209-4FBF-9CA8-7A333C87D1ED}`.
- **Коммит.** `9cbdbf0`, задача 006.

### D-036. Нет старой папки для переноса данных
- **Что.** `constexpr auto AppNameOld = "Telegram Win (Unofficial)"_cs;` → `"Pluma"_cs; // pluma: no older folder to move data from`.
- **Где.** `version.h` (стр. 22), точная замена.
- **Проверка.** `verify()`: в `version.h` нет `Unofficial`.
- **Коммит.** `9cbdbf0`, задача 006.

### D-037. Имя приложения и файла
- **Что.** `AppName = "Telegram Desktop"_cs` → `"Pluma"_cs`; `AppFile = "Telegram"_cs` → `"Pluma"_cs`. С `AppName` меняется папка данных: `%APPDATA%\Pluma` или переносная рядом с `Pluma.exe`, `~/Library/Application Support/Pluma`.
- **Где.** `version.h` (стр. 23–24), точные замены.
- **Проверка.** `verify()` (`AppName = "Pluma"`, нет `"Telegram Desktop"`, `"Telegram"_cs`); `test_patch`; `DAMAGE`: `"Telegram"_cs`.
- **Коммит.** `9cbdbf0`, задача 006.

### D-038. Свой GUID блокировки единственного экземпляра
- **Что.** `static const char *gGuidStr = "{87A94AB0-E370-4cde-98D3-ACC110C5967D}";` → `"{BC9AF030-72DA-400E-8363-5E5CC0C05F70}"; // pluma: Pluma's own`.
- **Где.** `Telegram/SourceFiles/config.h` (стр. 37), точная замена.
- **Проверка.** `verify()`; `test_patch`; `DAMAGE`: GUID в верхнем регистре.
- **Коммит.** `9cbdbf0`, задача 006.

### D-039. Имя приложения Qt (папка данных Linux)
- **Что.** `QApplication::setApplicationName(u"TelegramDesktop"_q);` → `u"Pluma"_q` → `~/.local/share/Pluma`.
- **Где.** `Telegram/SourceFiles/core/launcher.cpp` (стр. 339), точная замена.
- **Проверка.** `verify()`; `DAMAGE`: `u"Telegram"_q`.
- **Коммит.** `9cbdbf0`, задача 006.

### D-040. ID приложения Linux (Wayland app_id, D-Bus, .desktop)
- **Что.** `\t\treturn u"org.telegram.desktop"_q;\n` → `chat.pluma.desktop`; `\t\t\treturn u"org.telegram.desktop._%1"_q.arg(\n` → `chat.pluma.desktop._%1`.
- **Где.** `Telegram/SourceFiles/platform/linux/specific_linux.cpp` (стр. 744, 740), точные замены. Остальные упоминания `org.telegram.desktop` в файле (ресурс `:/misc/org.telegram.desktop.desktop`, удаление старых файлов) не трогаются.
- **Проверка.** `verify()` (`return u"chat.pluma.desktop"_q;`, нет `return u"org.telegram.desktop`); `test_patch`; `DAMAGE`: `org.telegram.Desktop`.
- **Коммит.** `9cbdbf0`, задача 006.

### D-041. Linux: не смотреть в `~/.TelegramDesktop`
- **Что.** Тело `psAppDataPath()` → `return QStandardPaths::writableLocation(QStandardPaths::AppLocalDataLocation) + '/';`.
- **Где.** `specific_linux.cpp`, `replace_body` от `QString psAppDataPath() {\n` (стр. 687); `must_contain`: `u"/.TelegramDesktop/"_q`, `AppLocalDataLocation`.
- **Апстрим до правки.** Если есть `~/.TelegramDesktop/tdata/settings{0,1,s}`, данные берутся оттуда (старый Telegram Desktop).
- **Проверка.** `verify()` (нет `/.TelegramDesktop/`); `DAMAGE`: `/.telegram-desktop/`.
- **Коммит.** `9cbdbf0`, задача 006.

### D-042. Файл `.desktop`
- **Что.** В `lib/xdg/org.telegram.desktop.desktop` 10 точных замен: `Name=Telegram` → `Name=Pluma`; `TryExec=Telegram` → `TryExec=Pluma`; `Exec=Telegram -- %U` → `Exec=Pluma -- %U`; `Icon=org.telegram.desktop` → `Icon=chat.pluma.desktop`; `StartupWMClass=TelegramDesktop` → `StartupWMClass=Pluma`; `MimeType=x-scheme-handler/tg;x-scheme-handler/tonsite;` → `MimeType=x-scheme-handler/plm;`; `Keywords=tg;chat;im;messaging;messenger;sms;tdesktop;` → `Keywords=chat;im;messaging;messenger;`; строка `DBusActivatable=true` удалена (с приложением не идёт D-Bus service file); `Exec=Telegram -quit` → `Exec=Pluma -quit`; `Name=Quit Telegram` → `Name=Quit Pluma`. Упаковка кладёт его как `Pluma/chat.pluma.desktop.desktop` (D-063).
- **Проверка.** `verify()` (нет `Telegram`, `x-scheme-handler/tg`, `tonsite`); `verify_brand()` (`MimeType=x-scheme-handler/plm;`); `test_patch`; `DAMAGE`: `Name=Telegram Desktop`, `MimeType` без `tonsite`.
- **Коммиты.** `9cbdbf0` (в нём `MimeType` удалялся), `7e6c8d6` (`MimeType` → `plm`), задачи 006 и 017.

### D-043. Windows: ярлыки `Pluma.lnk`
- **Что.**
  - `windows_app_user_model_id.cpp`: `u"Telegram.lnk"_q` → `u"Pluma.lnk"_q` (2 раза); `const auto installed = u"Telegram Desktop/Telegram.lnk"_q;` → `u"Pluma/Pluma.lnk"_q`; `const auto old = u"Telegram Win (Unofficial)/Telegram.lnk"_q;` → `u"Pluma/Pluma.lnk"_q`.
  - `specific_win.cpp`: `"\\Telegram.lnk";` → `"\\Pluma.lnk";` (2 раза: рабочий стол пользователя и общий).
  Ярлыки меню «Пуск», автозапуска, «Отправить», рабочего стола.
- **Где.** `Telegram/SourceFiles/platform/win/windows_app_user_model_id.cpp`, `platform/win/specific_win.cpp` (стр. 325, 328), точные замены.
- **Проверка.** `verify()` (нет `Telegram.lnk`); `test_patch` (2 × `"Pluma.lnk"_q`); `DAMAGE`: `Telegram Desktop.lnk` в обоих файлах.
- **Коммит.** `9cbdbf0`, задача 006.

### D-044. Windows: сведения о версии exe
- **Что.** `VALUE "FileDescription", "Telegram Desktop"` и `VALUE "ProductName", "Telegram Desktop"` → `"Pluma"`; `VALUE "CompanyName", "Telegram FZ-LLC"` → `"Pluma"`.
- **Где.** `Telegram/Resources/winrc/Telegram.rc` (стр. 63 и рядом), точные замены.
- **Проверка.** `verify()` (нет `Telegram FZ-LLC`, `Telegram Desktop`); `DAMAGE`.
- **Коммит.** `9cbdbf0`, задача 006.

### D-045. macOS: bundle ID
- **Что.** `set(bundle_identifier "com.tdesktop.Telegram$<$<CONFIG:Debug>:Debug>")` → `"chat.pluma.desktop$<$<CONFIG:Debug>:.debug>"`; `set(bundle_identifier "com.tdesktop.Telegram")` → `"chat.pluma.desktop"`.
- **Где.** `Telegram/CMakeLists.txt` (стр. 2278, 2280), точные замены.
- **Проверка.** `verify()`; `test_patch`; `DAMAGE`; упаковка сверяет `CFBundleIdentifier` с `PLUMA_APP_ID` (D-063).
- **Коммит.** `9cbdbf0`, задача 006.

### D-046. macOS: имя в Info.plist
- **Что.** `CFBundleName` `@output_name@` → `Pluma`; `CFBundleGetInfoString` `Telegram Desktop messaging app` → `Pluma`.
- **Где.** `Telegram/Telegram.plist` (стр. 17 и далее), точные замены.
- **Проверка.** `verify()` (нет `Telegram Desktop`); `test_patch`.
- **Известно (задача 006).** Внутри `Pluma.app` исполняемый файл и ресурсы по-прежнему называются `Telegram` (переименовывается только бандл при упаковке).
- **Коммит.** `9cbdbf0`, задача 006.

### D-047. `config/app.env` — значения идентичности
- **Что.** `PLUMA_APP_NAME=Pluma`, `PLUMA_APP_ID=chat.pluma.desktop`, `PLUMA_WINDOWS_APP_USER_MODEL_ID=Pluma.PlumaDesktop`, `PLUMA_WINDOWS_APP_GUID=BB4EA67A-…`, `PLUMA_INSTANCE_GUID=BC9AF030-…`, `PLUMA_TOAST_ACTIVATOR_CLSID=1BE834F0-…`, `PLUMA_API_ID=10003`, `PLUMA_API_HASH=…` (не секрет). Правила в `Config`: имя с заглавной, без «telegram»; reverse-DNS не `org.telegram.`/`com.tdesktop.`; AUMID не `Telegram.`; три разных GUID, не Telegram Desktop.
- **Проверка.** `ConfigTest.test_repository_config`, `test_rejects`.
- **Коммит.** `9cbdbf0`, задача 006.

---

## Premium и платные функции

Сервер Pluma ставит `premium_purchase_blocked` (`help.getAppConfig`), поэтому `Session::premiumCanBuy()` ложно (`main/main_session.cpp:372`).

### D-048. Баннер дней рождения контактов: без Premium, нажатие открывает чат
- **Что и зачем.** Четыре точные замены в `Telegram/SourceFiles/dialogs/suggestions/suggestion_birthday_contacts.cpp` (`BIRTHDAY_BANNER`):
  1. `#include "boxes/star_gift_box.h"` → `#include "boxes/peer_list_controllers.h"`;
  2. `bool Available(const Context &context)` (стр. 32): убрано условие `session->premiumCanBuy() &&` — баннер показывается всем, если сервер держит подсказку `BIRTHDAY_CONTACTS_TODAY`; перед функцией добавлен класс `BirthdayContactsController : ContactsBoxController` (строки только для пользователей из списка; без search controller поиск фильтрует по имени и не шлёт `contacts.search`);
  3. колбэки (стр. 59–): нажатие при одном имениннике — `controller->showPeerHistory(first)` вместо `Ui::ShowStarGiftBox`, при нескольких — `PeerListBox` с `BirthdayContactsController`, заголовком `tr::lng_gift_subtitle_birthdays()` («Birthdays») и кнопкой `tr::lng_close()` вместо `Ui::ChooseStarGiftRecipient`; закрытие по-прежнему `promo->dismiss(kSugBirthdayContacts…)`, но без тоста `lng_dialogs_suggestions_birthday_contact_dismiss` («подарок можно отправить позже»);
  4. подпись: `lng_dialogs_suggestions_birthday_contact(s)_about` («Send them a Gift.») → `tr::lng_context_send_message` («Send message»).
  Когда показывается и как выглядит — как у апстрима. Решение владельца — вариант А (задача 020).
- **Проверка.** `UpstreamTest.test_birthday_banner` (тексты кода, отсутствие `ShowStarGiftBox`/`ChooseStarGiftRecipient`/тоста, ключи `lang.strings` без слова gift); `DAMAGE`: `premiumPossible()`, `ChooseStarGiftRecipient(controller, users)`, `birthday_gift_about`, `birthday_contacts_dismiss`. В `verify()` проверки нет.
- **Если апстрим сдвинет.** Любое изменение четырёх фрагментов → `PatchError`.
- **Апстрим до правки / официальный клиент.** Баннер только тем, кто может купить Premium; нажатие открывает окно подарка. В Telegram Premium продаётся, так что баннер ведёт к подарку. Telegram сам снимает `BIRTHDAY_CONTACTS_TODAY` после подарка (core.telegram.org/api/config, по записи задачи 020); у нас — до закрытия.
- **Зависимости на сервере.** `contacts.getBirthdays` (пользователи с флагом `contact`), `help.getPromoData`, `help.dismissSuggestion` (задача 020).
- **Коммит.** `2f6fbbe` (2026-10-31), задача 020.

### D-049. Кнопка подарка в чате — только когда Premium продаётся
- **Что.** В `HistoryWidget::refreshSendGiftToggle` после `\t\t&& !user->isBot()\n` вставлено `\t\t&& session().premiumCanBuy()\n` (перед `&& ((disallowed & Type::SendHide)`).
- **Где.** `Telegram/SourceFiles/history/history_widget.cpp` (стр. 3745), точная замена.
- **Проверка.** `test_birthday_banner`; `DAMAGE`: вставка `isInaccessible()`.
- **Апстрим до правки.** Кнопка подарка у поля ввода в день рождения контакта независимо от Premium.
- **Коммит.** `2f6fbbe`, задача 020.

### D-050. Строка дня рождения в профиле — подарок только в день рождения и только при продаже Premium
- **Что.** `const auto disable = !today && user->session().premiumCanBuy();` → `const auto disable = !today || !user->session().premiumCanBuy();`.
- **Где.** `Telegram/SourceFiles/info/profile/info_profile_actions.cpp` (стр. 967), точная замена.
- **Проверка.** `test_birthday_banner`; `DAMAGE`: `premiumPossible()`.
- **Апстрим до правки.** При непродаваемом Premium строка открывала окно подарка (подарки за Stars) в любой день. Теперь как у кнопки подарка верхней панели профиля (`info_profile_top_bar.cpp`) и у Android/Web K.
- **Коммит.** `2f6fbbe`, задача 020.

### D-073. Теги «Избранного» без Premium
- **Что и зачем.** Решение владельца 2026-11-03 (задача 053): теги сообщений в «Избранном» доступны всем, как в Telegram у Premium; Premium в Pluma нет. Сигнала с сервера для тегов клиент не читает: все гейты смотрят на `Session::premium()` (`main/main_session.cpp:345-347`), а `user.premium=true` открыло бы весь Premium. Поэтому одна функция `Main::Session::savedTagsAvailable()` (всегда `true`) подставлена вместо `premium()` только в местах тегов. Шесть точных замен (`SAVED_TAGS` в `ci/patch.py`):
  1. `main/main_session.h:132` — объявление `savedTagsAvailable()` после `premiumCanBuy()`;
  2. `main/main_session.cpp:372-374` — определение после `Session::premiumCanBuy()`, `return true;`;
  3. `window/section_widget.cpp:621-626` (`Window::ShowReactPremiumError`, ветка `reactionsAreTags()`) — добавление и снятие тега через селектор, быструю реакцию и клик больше не показывают окно Premium `TagsForMessages`; вызовы: `history/history_inner_widget.cpp:559,2741`, `history/view/history_view_element.cpp:2738`, `history/view/history_view_list_widget.cpp:201`, `info/saved/info_saved_sublist_inline.cpp:566`;
  4. `history/view/history_view_element.cpp:2714-2722` — клик по тегу на сообщении ищет по тегу (`HashtagClickHandler(Data::SearchTagToQuery(id))`), а не открывает окно Premium;
  5. `history/view/reactions/history_view_reactions_selector.cpp:1535-1545` (`ItemReactionsAbout`) — подсказка над тегами селектора: `lng_add_tag_about` («Tag this message with an emoji for quick search.») вместо `lng_subscribe_tag_about` со ссылкой `internal:about_tags` на окно Premium;
  6. `dialogs/dialogs_search_tags.cpp:92-99` — `Data::AmPremiumValue(&owner->session())` → `rpl::single(owner->session().savedTagsAvailable())`: теги над поиском «Избранного» (в списке чатов, `dialogs/dialogs_inner_widget.cpp:4148`, и в чате, `history/view/controls/history_view_compose_search.cpp:479`) — фильтр по тегу и правое меню с переименованием (`HistoryView::ShowTagInListMenu`, `history/view/history_view_context_menu.cpp`) вместо промо-чипа «Add tags»/«Unlock» (`:126-129`, `:163-174`).
  Меню тега из сообщения (`ShowTagMenu`, `history_view_context_menu.cpp:2385`) своей проверки Premium не имеет. Промо `internal:about_tags` (`core/local_url_handlers.cpp:903-911`) и код промо-чипа остаются в апстриме, но после правки недостижимы. Новых строк нет.
- **Что не открывается (сверено по коду, опоры — `tree.expect`, `SAVED_TAGS_KEPT`).**
  - Число тегов (и реакций) на сообщение: `SentReactionsLimit` (`data/data_message_reactions.cpp:114-120`) у не-Premium берёт `reactions_user_max_default` из appConfig сервера (по умолчанию 1); сервер шлёт 3 — значит, до трёх тегов и трёх реакций, как у Premium в Telegram. Лимит не патчится. `MessageReactions::add` (`:1951-1975`) снимает самый старый свой тег/реакцию сверх лимита.
  - Custom emoji в тегах: `LookupPossibleReactions` (`:234-246`) для тегов даёт `customAllowed = premiumPossible` и в полосе только теги из `Reactions::Type::Tags` (`messages.getDefaultTagReactions`) — при `premium_purchase_blocked=true` custom emoji недоступны, как и прочий Premium.
- **Проверка.** `UpstreamTest.test_saved_tags`: тексты всех шести мест; `savedTagsAvailable` встречается только в этих файлах и по одному разу в каждом гейте; каждый файл отличается от апстрима ровно заменой; `SAVED_TAGS_KEPT` на месте; ключ `lng_add_tag_about` есть во встроенном английском. Без `SAVED_TAGS` тест падает (проверено). `DAMAGE` (8): `premiumCanBuy()` без `const`, тело `premiumCanBuy`, `premiumPossible()` в трёх гейтах, `AmPremiumValue(owner)`, лимит `: 1;`, `customAllowed = true`. В `verify()` нет.
- **Если апстрим сдвинет.** Любое изменение шести фрагментов или опор `SAVED_TAGS_KEPT` → `PatchError`. Новый гейт тегов апстрима патч не заметит — см. п. 13 раздела «Что проверять при обновлении апстрима».
- **Апстрим до правки / официальный клиент.** Не-Premium видит промо-чип, окно Premium `TagsForMessages` при попытке поставить тег или нажать на него и текст-предложение подписки в селекторе.
- **Зависимости на сервере.** appConfig `reactions_user_max_default=3`, `reactions_user_max_premium=3`, `premium_purchase_blocked=true`; `messages.sendReaction`, `messages.getSavedReactionTags`, `messages.updateSavedReactionTag`, `messages.getDefaultTagReactions`, `messages.search` с `saved_reaction`, апдейт `updateSavedReactionTags` (задача 053).
- **Не проверено.** Сборка клиента и поведение на стенде (полная сборка не запускалась; только применение патча к закреплённому апстриму в тестах).
- **Коммит.** Не закоммичено (ветка `task/053-saved-tags`, 2026-11-04), задача 053.

Прочие места Premium/Stars/подарков в Desktop не правятся: их скрывает `premium_purchase_blocked` сервера и поведение апстрима. Тексты «Telegram Premium»/«Telegram Stars» переименованы в D-012, D-013.

---

## Медиа

Правок поведения медиа нет. Связанное с медиа: текст кольца вокруг записываемого кружка (D-013, `round_video_recorder.cpp`), фильтр файлов «Pluma sticker (*.tgs)» (D-013, `mime_type.cpp`), аватары HTML-экспорта (D-028). Папка загрузок называется по `AppName` — «Pluma» (задача 020, сверка клиентов).

---

## Уведомления и пуши

Пушей у Desktop нет: уведомления строятся из обновлений MTProto-соединения. Имя в уведомлениях и в настройках уведомлений — D-013.

### D-051. Windows: свой AppUserModelID
- **Что.** `const WCHAR AppUserModelIdBase[] = L"Telegram.TelegramDesktop";` → `L"Pluma.PlumaDesktop"; // pluma`. Группировка в панели задач, уведомления Windows.
- **Где.** `Telegram/SourceFiles/platform/win/windows_app_user_model_id.cpp` (стр. 31), точная замена. Строка `L"Telegram.TelegramDesktop.Store"` (стр. 29, сборка Store) не трогается и учтена в `CODE_TELEGRAM`.
- **Проверка.** `verify()`; `test_patch`; `DAMAGE`: `L"Telegram.Desktop"`.
- **Коммит.** `9cbdbf0`, задача 006.

### D-052. Windows: свой CLSID активатора уведомлений
- **Что.** `// {F11932D3-6110-4BBC-9B02-B2EC07A1BD19}\nclass DECLSPEC_UUID("F11932D3-…")` → `// {1BE834F0-6DC2-429A-9B59-2B262F6AA37C} (pluma: Pluma's own)\nclass DECLSPEC_UUID("1BE834F0-…")`. COM-класс, который активирует уведомление; общий с Telegram Desktop CLSID отправлял бы нажатия не тому приложению.
- **Где.** `Telegram/SourceFiles/platform/win/windows_toast_activator.h` (стр. 15–16), точная замена.
- **Проверка.** `verify()`; `test_patch`; `DAMAGE`: изменённый GUID.
- **Коммит.** `9cbdbf0`, задача 006.

---

## Сборка и подпись

### D-053. Бэкпорт апстрима `b244f1b3`: пакеты MSYS2 UCRT в `prepare.py`
- **Что и зачем.** MSYS2 удалил пакеты `mingw-w64-x86_64-*` (msvcrt), и `prepare.py` 7.0.9 падал на Windows («target not found: mingw-w64-x86_64-diffutils»). Три точные замены в `Telegram/build/prepare/prepare.py`, побайтно как в апстриме (tdesktop `b244f1b3`, 2026-09-30, «Update msys2 and use ucrt versions of packages»):
  - `'ThirdParty\\msys64\\mingw64\\bin',` → `'ThirdParty\\msys64\\ucrt64\\bin',` (стр. 85);
  - установщик `releases/download/2025-08-30/msys2-base-x86_64-20250830.sfx.exe` → `2026-09-27/msys2-base-x86_64-20260927.sfx.exe` (стр. 471);
  - пять пакетов `mingw-w64-x86_64-{diffutils,gperf,nasm,perl,pkgconf}` → `mingw-w64-ucrt-x86_64-…` (стр. 478–482).
- **Проверка.** `verify()`: нет `mingw-w64-x86_64-` и `msys64\\mingw64`; `test_patch`; `DAMAGE`: изменённый список пакетов; апстрим, уже содержащий исправление (тогда бэкпорт надо убрать).
- **При обновлении.** Когда закреплённый релиз содержит `b244f1b3`, замена перестанет совпадать — бэкпорт удалить.
- **Коммит.** `1c49115` (2026-10-27), задача 006.

### D-054. Windows: не собирать `dump_syms` breakpad
- **Что и зачем.** Release-вариант этапа breakpad собирает `dump_syms` (символы для отчётов о сбоях Telegram); ему нужен ATL, которого нет в тулсете MSVC 14.44 образа («Cannot open include file: 'atlbase.h'»). CI апстрима собирает только Debug и сюда не доходит. Удалены три строки после `ninja -C out/Release%FolderPostfix% common crash_generation_client exception_handler`: `cd tools\\windows\\dump_syms`, `gyp dump_syms.gyp --format=msvs` (стр. 1428), `msbuild -m dump_syms.vcxproj …`.
- **Где.** `prepare.py`, точная замена (`BUILD_EDITS[0]`).
- **Проверка.** `verify()`: нет `dump_syms.vcxproj`; `test_patch` (macOS-сборка `dump_syms.xcodeproj` осталась); `test_build_edits_stay_in_their_platform` (правка только в области `win`); `DAMAGE`: `--depth=.`.
- **Коммит.** `9e5e718` (2026-10-27), задача 006.

### D-055. macOS: Qt только release, без `-force-debug-info`, в очищенный префикс
- **Что и зачем.** Библиотеки macOS занимали 20 ГБ (4,8 ГБ кэша) из-за отладочных Qt и tg_owt; Release-клиент их не линкует (cmake_helpers выбирает вариант по `$<CONFIG>`). В mac-ветке `release:` `CONFIGURATIONS=-debug-and-release` → `-release`; перед `./configure -prefix "$USED_PREFIX/Qt-$QT"` добавлено `rm -rf "$USED_PREFIX/Qt-$QT"`; убрана строка `-force-debug-info`.
- **Где.** `prepare.py` (стр. 1603 и далее), точная замена (`BUILD_EDITS[1]`). Windows-строка `SET CONFIGURATIONS=-debug-and-release` (стр. 1539) не трогается.
- **Проверка.** `verify()`: в mac-конфигурации Qt нет `-force-debug-info`, нет `    CONFIGURATIONS=-debug-and-release`; `test_patch` (у Windows 2 × `-force-debug-info` остались); `test_build_edits_stay_in_their_platform` (`mac`); `DAMAGE`: новая область `mac_asserts:` между.
- **Коммит.** `bfbe6b9` (2026-10-28), задача 006.

### D-056. macOS: tg_owt без Debug-сборок
- **Что.** Удалены блоки `mkdir Debug.x86_64 … cmake -DCMAKE_BUILD_TYPE=Debug …` и то же для `arm64` (стр. 1456 и далее), а также `mkdir Debug` и `lipo -create Debug.arm64/libtg_owt.a Debug.x86_64/libtg_owt.a -output Debug/libtg_owt.a`. Release-сборки и `lipo` Release остаются.
- **Где.** `prepare.py`, точная замена на пустую строку (`BUILD_EDITS[2]`).
- **Проверка.** `verify()` (нет `Debug.arm64/libtg_owt.a`); `test_patch`; `test_build_edits_stay_in_their_platform`; `DAMAGE`: `lipo -create Debug.*/libtg_owt.a`.
- **Коммит.** `bfbe6b9`, задача 006.

### D-057. Закрепление апстрима и проверка его тулчейна
- **Что.** `upstream.env` (см. «Общее») и `check_upstream()`: патч отказывается работать с выгрузкой не того layer, не той версии и не того тулчейна, а также если изменились места апстрима, на которые опираются наши скрипты (`centos_env/build.sh`, `gen_dockerfile.py`, опции `prepare.py`, опции `cmake/variables.cmake`, entitlements).
- **Где (опоры `check_upstream`, правок нет).**
  - `Telegram/SourceFiles/mtproto/scheme/api.tl`: ровно одна строка `// LAYER 228` (регулярное выражение `^// LAYER (\d+)$`);
  - `Telegram/SourceFiles/core/version.h`: `constexpr auto AppVersionStr = "7.0.9";\n` (`expect`);
  - `Telegram/build/qt_version.py`: `os.environ['QT'] = '6.11.1'\n`, ровно 2 вхождения (macOS и Windows с qt6);
  - `docs/building-win.md`: `**10.0.26100.0** SDK version` (`expect`) и хотя бы одно `-vcvars_ver=14.44` (`expect_some`);
  - `.github/workflows/linux.yml`: хотя бы одно `/ccache/releases/download/v4.13.6/ccache-4.13.6-linux-x86_64-glibc.tar.xz` (`expect_some`);
  - `Telegram/build/docker/centos_env/build.sh`: `cd Telegram\n./configure.sh "$@"\ncmake --build ../out --config "${CONFIG:-Release}"\n`; `centos_env/gen_dockerfile.py`: `DEBUG=checkEnv("DEBUG", True),\n` и `LTO=checkEnv("LTO", True),\n`;
  - `Telegram/build/prepare/prepare.py`: `optionsList = [\n    'qt6',\n` и `        elif arg == 'silent':\n            rebuildStale = True\n`;
  - `cmake/variables.cmake`: `option(DESKTOP_APP_DISABLE_AUTOUPDATE "Disable autoupdate."` и `option(DESKTOP_APP_DISABLE_CRASH_REPORTS "Disable crash report generation."`;
  - `Telegram/Telegram/Telegram.entitlements` побайтно равен `ci/macos.entitlements` (D-065);
  - каждый правленый файл (`EDITED` и файлы бренда) без CRLF (`lf_only`, D-071).
  Значения версий берутся из `upstream.env` (`UPSTREAM_VERSION`, `UPSTREAM_LAYER`, `QT_VERSION`, `WINDOWS_SDK`, `MSVC_TOOLSET`, `CCACHE_VERSION`).
- **Проверка.** `DAMAGE`: `LAYER 229`, `7.0.10`, Qt `6.12.0`, SDK `10.0.26200.0`, `-vcvars_ver=14.50`, ccache `4.14.0`, `build.sh` с `--config Release`, `ENABLE_LTO`, `quiet` вместо `silent`; `fetch-upstream.sh` проверяет, что `HEAD` = `UPSTREAM_REF`.
- **Коммиты.** `9cbdbf0`; `66af343` (SHA-256 ccache перенесены из `upstream.env` в `ci/setup.sh`), задача 006.

### D-058. Выгрузка апстрима: `ci/fetch-upstream.sh`
- **Что.** `sparse` — только файлы, которые читает патч (`patch.py --paths`, `--submodule-paths`; сабмодули `cmake`, `Telegram/lib_ui`, `Telegram/lib_base` с коммитов, записанных апстримом), `--depth 1 --filter=blob:none`; `full` — со всеми сабмодулями. Всегда `core.autocrlf=false`, `core.eol=lf`, `core.longpaths=true`. Три попытки с таймаутом; повтор `submodule update` с `--force` и проверка, что каждый сабмодуль выгружен (сетевой сбой оставлял `lib_storage` и `lib_qr` пустыми).
- **Проверка.** Шаг CI; отдельных тестов нет.
- **Коммиты.** `9cbdbf0`, `b46ab91` (2026-10-28), задача 006.

### D-059. Образ сборки Linux: `ci/linux-env.sh`
- **Что.** Образ из `Telegram/build/docker/centos_env` апстрима (Rocky Linux 8, gcc-toolset-15, все библиотеки и Qt из исходников): `DEBUG= LTO= python3 gen_dockerfile.py` (вариант CI апстрима: без отладочной информации и LTO библиотек), проверка `FROM rockylinux:8 AS builder`, buildx с `BUILDKIT_IMAGE`, тег `ghcr.io/<owner>/pluma-desktop-linux-env:<16 знаков tree-хэша centos_env>-r1`, слои в `:buildcache`. Собирается только при изменении `centos_env` (2–4 ч).
- **Проверка.** `check_upstream()` требует `DEBUG=checkEnv("DEBUG", True),` и `LTO=checkEnv("LTO", True),` в `gen_dockerfile.py`.
- **Коммит.** `b441350` (2026-10-27), задача 006.

### D-060. Сборка Linux: `ci/build-linux.sh`
- **Что.** В образе D-059 запускается `centos_env/build.sh` апстрима с Release, флагами `-O3 -DNDEBUG -fpch-preprocess`, `api_id`/`api_hash` и опциями D-009; поверх ccache 3.7.7 образа монтируется статический ccache 4.13.6 с раннера (`-v $ccache:/usr/bin/ccache:ro`; 3.7.7 выбрасывал предкомпилированные заголовки), `CCACHE_SLOPPINESS=pch_defines,time_macros`, `CCACHE_MAXSIZE`, журнал статистики. Требует `pluma-patch.txt` и ccache нужной версии.
- **Проверка.** `check_upstream()` требует текст `build.sh`: `cd Telegram\n./configure.sh "$@"\ncmake --build ../out --config "${CONFIG:-Release}"\n`.
- **Коммиты.** `b441350`, `66af343` (ccache 4), `e7654de` (журнал промахов), задача 006.

### D-061. Сборка Windows: `ci/windows.sh`
- **Что.**
  - Каталог сборки `D:\TBuild` (на C: образа ~30 ГБ).
  - `cmd.exe` с окружением `vcvarsall.bat x64 <SDK> -vcvars_ver=<MSVC>`, с PATH настоящего `shell: cmd` шага (`PLUMA_CMD_PATH`), с проверкой, что `find.exe` — из System32; без `MSYSTEM`, `MSYS_NO_PATHCONV`, `MSYS2_ARG_CONV_EXCL` (`//d //c` для Git Bash).
  - `libraries`: `Telegram\build\prepare\win.bat silent qt6`; перед этим **на раннере правится CMake**: `sed` убирает `${_Zi}` из `CMAKE_${lang}_FLAGS_DEBUG_INIT` в `Windows-MSVC.cmake` (Debug-библиотеки без PDB, как «First set up» CI апстрима); git user `Pluma CI <ci@pluma.invalid>` (этапы коммитят свои патчи); источник nuget.
  - `trim`: удаление ненужного из библиотек, как «Free up some disk space» апстрима.
  - `client`: `configure.bat -G "Ninja Multi-Config" qt6`, Release, `CMAKE_MSVC_DEBUG_INFORMATION_FORMAT=` (без PDB), ccache как `CMAKE_C/CXX_COMPILER_LAUNCHER`, `CCACHE_COMPILERCHECK=string:MSVC %VCToolsVersion% %VSCMD_ARG_TGT_ARCH%`, `QT_RCC_SOURCE_DATE_OVERRIDE` = дата коммита апстрима.
  - В workflow переменная `PLUMA_TARGET` (не `TARGET`: MSBuild падает на двух переменных, различающихся регистром).
- **Проверка.** Только сборкой в CI.
- **Коммиты.** `b441350`, `41b4da1`, `7ba13dd`, `dbcf059`, `e06593f`, `e650b13`, `6293a28`, `f67bf74` (2026-10-27), `006c33d`, `714056d` (2026-10-28), задача 006.

### D-062. Сборка macOS: `ci/macos.sh`
- **Что.**
  - `setup`: `xcode-select` на `/Applications/Xcode_26.6.app` и сверка версии; при нехватке места (по умолчанию нужно 60 ГБ) удаляются другие Xcode и рантаймы симуляторов; `brew install automake libtool meson nasm ninja pkg-config`; Spotlight выключен; git user `Pluma CI`.
  - `libraries`: `Telegram/build/prepare/mac.sh silent` (universal; с D-055, D-056). `trim` — как в апстриме, плюс список крупнейших этапов.
  - `client`: `./configure.sh` Release, `CMAKE_XCODE_ATTRIBUTE_CODE_SIGNING_ALLOWED=NO`, `api_id`/`api_hash`, опции D-009; ccache как `CMAKE_XCODE_ATTRIBUTE_C_COMPILER_LAUNCHER`, `OTHER_PRECOMP_CFLAGS=-Xclang -fno-pch-timestamp`, `SDK_STAT_CACHE_ENABLE=NO`, `CCACHE_COMPILERCHECK=string:Xcode 26.6 (<build>)`.
- **Проверка.** Только сборкой в CI.
- **Коммиты.** `b441350`, `bfbe6b9`, `006c33d`, `775d383`, `714056d` (2026-10-28), задача 006.

### D-063. Упаковка: `ci/package.sh`
- **Что.** Исполняемые файлы сохраняют имена апстрима внутри сборки и получают имена Pluma только здесь:
  - Linux: `out/Release/Telegram` → `Pluma/Pluma`, `lib/xdg/org.telegram.desktop.desktop` → `Pluma/chat.pluma.desktop.desktop`, `pluma-desktop-<версия>-linux[-dev].tar.xz`;
  - Windows: `Telegram.exe` → `Pluma/Pluma.exe`, `.zip`; кодовой подписи нет;
  - macOS: `Telegram.app` → `Pluma.app` (внутри исполняемый файл `Telegram`), сверка `CFBundleIdentifier` = `chat.pluma.desktop` и `CFBundleShortVersionString` = версии апстрима, иконка D-020, ad hoc подпись `codesign --force --deep --sign -` и проверка, `.zip`.
  - Рядом с каждым файлом `.sha256`.
- **Проверка.** Только сборкой в CI.
- **Коммиты.** `b441350`, `cd537bf`, задача 006 (иконка — 017).

### D-064. Подпись Developer ID и нотаризация macOS: `ci/sign-macos.sh`, job `sign-macos`, workflow `staple-macos.yml`
- **Что.** Для сборок под сервер job `sign-macos` в environment `release` (ждёт подтверждения владельца) расшифровывает zip (`PLUMA_AGE_IDENTITY`), подписывает `codesign --force --deep --timestamp --options runtime --entitlements ci/macos.entitlements` (как `Telegram/build/build.sh` апстрима) временной связкой ключей, отправляет в `notarytool submit` без ожидания, берёт id отправки, ждёт 30 мин (`notarytool wait`, затем `info`, читаются оба потока). Accepted → `stapler staple`, `stapler validate`, `spctl --assess`, артефакт `pluma-desktop-macos-signed`; ещё проверяется → подписанное приложение и `<имя>.notary-id`, артефакт `pluma-desktop-macos-notarizing`, дозавершение workflow `staple-macos` (`sign-macos.sh staple`, ждёт 10 мин, выход 75 — повторить позже); Invalid/Rejected → журнал Apple и ошибка. Шаг `sign` — только на раннере GitHub (меняет список связок ключей).
- **Секреты (только имена).** `PLUMA_DEVELOPER_ID_P12`, `PLUMA_DEVELOPER_ID_PASSWORD`, `PLUMA_NOTARY_KEY`, `PLUMA_NOTARY_KEY_ID`, `PLUMA_NOTARY_ISSUER_ID`, `PLUMA_AGE_IDENTITY` в environment `release`. Без `PLUMA_DEVELOPER_ID_P12` job ничего не подписывает и предупреждает.
- **Проверка.** `ci/sign_test.py` (`SignTest`: `test_sign_accepted`, `test_sign_outlasted`, `test_sign_keeps_the_app_when_apple_cannot_be_asked`, `test_sign_rejected`, `test_sign_upload_failed`, `test_sign_on_a_runner_only`, `test_staple_accepted`, `test_staple_still_checking`, `test_staple_rejected`, `test_staple_needs_no_developer_id`, `test_staple_checks_the_id`).
- **Windows** не подписывается (сертификата нет); dev-сборки macOS — ad hoc.
- **Коммиты.** `ead11ff` (2026-10-29), `92a4467` (два шага, `staple-macos.yml`, тесты), `9128b78` (id отправки до ожидания), задача 017 (подпись Desktop).

### D-065. Entitlements macOS — копия апстрима, сверяемая патчем
- **Что.** `ci/macos.entitlements`: `com.apple.security.device.audio-input`, `com.apple.security.device.camera`, `com.apple.security.personal-information.location` (= `Telegram/Telegram/Telegram.entitlements` 7.0.9).
- **Проверка.** `check_upstream()` требует побайтного равенства; `DAMAGE`: `device.usb` вместо `device.camera`.
- **Если апстрим сдвинет.** Изменённые entitlements → `PatchError`: решить, что приложению можно, и обновить копию.
- **Коммит.** `ead11ff`, задача 017.

### D-066. Шифрование сборок под сервер: `ci/encrypt.sh`
- **Что.** Артефакты публичного репозитория читает кто угодно (ADR 0010), поэтому сборки под сервер выходят из job только как `.age` для `PLUMA_AGE_RECIPIENT`; `.sha256` и `.notary-id` остаются открытыми. Job `check` падает, если получатель не похож на ключ age.
- **Коммиты.** `b441350`, `92a4467`, задача 006.

### D-067. Кэш GitHub Actions в пределах 10 ГБ
- **Что.** `ci/cache-budget.sh` (пределы: `linux-ccache` 2000 МБ, `windows-ccache` 2500, `macos-ccache` 2250, `windows-libs` 1500, `macos-libs` 750 — сумма 9000; перед сохранением удаляет вытесняемую запись того же вида; запись сверх предела сохраняется только если кэш не превысит 10 ГБ), `ci/cache-key.sh` (ключ библиотек — хэш `prepare.py` апстрима как закоммичен, плюс хэш `cache_keys` этапов; наши правки `prepare.py` пересобирают только затронутые этапы), `ci/prune-caches.sh`, `ci/ccache-stats.sh`. Сохраняет только `main`.
- **Проверка.** `ci/budget_test.py` (`BudgetTest.test_caps_fit`, `test_readme_lists_the_caps`, `test_workflow_kinds`; `AdmitTest.*`).
- **Коммиты.** `1c49115` (ключ по `prepare.py`), `999d64d`, `8ea3799`, `3528c73`, `e7654de` (2026-10-28), задача 006 (ADR 0010).

### D-068. Workflow `desktop.yml` и dependabot
- **Что.** Jobs: `check` (тесты, патч dev и сервер, проверка ключа age, ссылка на образ, lint) → `linux-env` → `linux`, `windows` (`windows-2025`), `macos` (`macos-26`) → `sign-macos` → `prune-caches`. Триггеры: push в `main`, pull request (без артефактов), ручной запуск (выбор систем, `dev`), еженедельная канарейка `41 2 * * 1` (dev-сборка закреплённого апстрима на свежих образах); изменения `*.md` и `LICENSE` ничего не собирают. Очереди по системе и ветке. Actions закреплены по SHA; `dependabot.yml` еженедельно обновляет только actions (апстрим и тулчейн — вручную через `upstream.env`). Таймауты шагов библиотек: Windows 250 мин, macOS 210 мин. Артефакты: dev — открыто 7 дней, сервер — шифровано 30 дней.
- **Коммиты.** `b441350`, `96788b4`, `e650b13`, `999d64d`, `006c33d`, `e7654de`, `ead11ff`, `92a4467`, задача 006.

### D-069. Закреплённые инструменты, lint и настройки репозитория
- **Что.** `ci/setup.sh`: age 1.3.2 и ccache 4.13.6 по SHA-256 для Linux x86-64 (ccache — статический musl), macOS arm64, Windows x86-64. `ci/lint.sh`: `shellcheck -x ci/*.sh brand/*.sh` и actionlint 1.7.12 по SHA-256. `.gitattributes` `* text=auto eol=lf`; `.shellcheckrc` `source-path=SCRIPTDIR/..`; `.gitignore` (`/upstream/`, `/out/`, `/.ccache/`, `*.agekey`).
- **Коммиты.** `9cbdbf0`, `b441350`, `006c33d`, `66af343`, `9569cc4` (lint охватывает `brand/`), задача 006.

---

## Прочее

### D-070. Исправление апстрима: подсказка над списком чатов, которая грузится первой, показывается
- **Что и зачем.** `TopBarSuggestionValue` отдаёт списку чатов новую обёртку, только если подсказка готова, пока обрабатывается событие promo/авторизации/Premium. Баннер дня рождения готов лишь после ответа `contacts.getBirthdays`, и тогда его никто не передавал: баннер не показывался (увидено на dev-сборке против стенда, 2026-10-31). Четыре точные замены в `Telegram/SourceFiles/dialogs/dialogs_top_bar_suggestion.cpp` (`TOP_BAR_WRAP`):
  1. в состояние после `int activationId = 0;` (стр. 82) добавлено поле `bool processing = false;`;
  2. перед `if (!state->wrap) {` запоминается `const auto created = !state->wrap;`;
  3. после `state->desiredWrapToggle.force_assign(Toggle{ true, anim::type::normal });` в `done` подсказки: `if (created && !state->processing) { consumer.put_next_copy(state->wrap.get()); }` — обёртка, созданная после возврата обработки, передаётся один раз (список держит её в `unique_qptr`, повторная передача удалила бы её);
  4. вокруг `processCurrentSuggestion(processCurrentSuggestion);` (стр. 244) — `state->processing = true;` / `= false;`.
- **Проверка.** `test_birthday_banner` (два `consumer.put_next_copy(state->wrap.get());`); `DAMAGE`: `processCurrentSuggestion(…, true)`, `anim::type::slow`. В `verify()` нет.
- **Официальный Telegram.** Затрагивает ли ошибка официальный клиент — не проверено (у Telegram баннер требует ещё `premiumCanBuy`, и день рождения может приходить иначе).
- **При обновлении.** Проверить, не исправил ли апстрим это сам; тогда правку убрать.
- **Коммит.** `adfaeb7` (2026-10-31), задача 020.

### D-071. Маркер патча и защитные проверки
- **Что.** Патч пишет `pluma-patch.txt` («Patched by pluma-desktop ci/patch.py: chat.pluma.desktop "Pluma", layer 228, server <адрес> port <порт>, server key 0x…»); сборочные скрипты отказываются собирать дерево без маркера, патч — дерево с маркером. CRLF в правленом файле — ошибка (`lf_only`). `Tree` отвергает каталог без `core/version.h` и `Telegram/CMakeLists.txt`.
- **Проверка.** `test_patched_tree_is_refused`, `TreeTest.test_not_a_checkout`, `test_keeps_line_endings`; `DAMAGE`: CRLF в `launcher.cpp`.
- **Коммит.** `9cbdbf0`, задача 006.

### D-072. Документация репозитория: `README.md`, `LICENSE`
- **Что.** `README.md` описывает патч, сборки, настройку, кэши компиляторов, бренд и то, что намеренно осталось как в апстриме; на сборку не влияет. `LICENSE` — лицензия репозитория.
- **Коммиты, менявшие только README.** `5611766` (2026-10-27, описание патча, сборок и настройки), `9940637` и `8cd3916` (2026-10-28, повторные сборки, размеры и промахи ccache), `c671fbf` (2026-10-29, бренд); задачи 006, 017. Остальные изменения README идут вместе с правками, к которым относятся (см. их записи).

---

## Что осталось как в апстриме намеренно (по README)

- Ссылки на `t.me`, `telegram.org`, `fragment.com` в интерфейсе (условия, политика, страницы Premium/Stars/рекламы/мини-приложений, исходники, история изменений) — открываются только по нажатию.
- Запросы, которые запускает пользователь: перевод через веб-провайдера, адрес отправляемого места (Mapbox), платёжные формы (Stripe, Smartglocal), веб-приложения ботов, Instant View, файлы по ссылкам.
- 1024-битные ключи обновлений Telegram в `config.h` (D-009).
- Всё, что идёт через MTProto (наборы эмодзи, словари, языковой пакет, звонки), — к серверу Pluma.
- Пресеты акцентов, темы Day и Night (кроме цветов-идентичности и ключей, которые они берут из Classic), фон чата, кольцо историй — цвета Telegram (сверка клиентов, задача 020).

## Пробелы и открытые вопросы

- Баннер и подарки (D-048…D-050), исправление D-070 и теги «Избранного» (D-073) не проверяются `verify()`, только тестом на закреплённом апстриме.
- D-025 (`keepContrast`) не имеет собственного утверждения в тестах — держится только точностью замены.
- Иконка macOS 26 (D-020), упаковка, сборочные флаги (D-010, D-060…D-063) проверяются только самой сборкой в CI.
- Windows-сборка не подписана; на Windows проверена под Wine (задача 006).
- Ссылки `pluma.chat` вместо `t.me` (задача 026) в Desktop ещё не сделаны.
