# Реестр правок клиента Android

Здесь записано всё, что Pluma меняет в официальном клиенте Telegram для Android: правки исходников, ресурсы, сборка, CI. По реестру при обновлении апстрима видно, что каждая правка затрагивает и где её искать.

Состояние: `main` репозитория `pluma-android` на `e859d6b` (2026-11-03): в него слиты `sticker-set-kinds` (`339fd8c`, A-060) и `video-quality` (`b7accac`, A-079…A-085). Теги «Избранного» без Premium (A-086…A-092, задача 053) — в рабочем дереве ветки `task/053-saved-tags`, не закоммичены (2026-11-04). Реестр составлен 2026-11-03 по `ci/patch.py`, `ci/patch_test.py`, README, скриптам CI и полной истории git (20 коммитов во всех ветках, включая непринятую ветку Dependabot `69205f7`).

## Общее

- **Репозиторий:** `sckraaa/pluma-android` (локально `~/pluma-android`). Кода клиента в нём нет: только скрипт патча, ресурсы бренда, конфиги и CI.
- **Закреплённый апстрим** (`upstream.env`):
  - `UPSTREAM_REPO=https://github.com/DrKLO/Telegram.git`
  - `UPSTREAM_REF=7be672ead283d022812d75c729d3a04dd4c2a2c6`
  - `UPSTREAM_VERSION=13.2.1`, `UPSTREAM_LAYER=233`
  - `NDK_VERSION=27.2.12479018`, `CMAKE_VERSION=3.22.1`, `CMAKE_PACKAGE=3.22.1`, `BUILD_TOOLS=36.0.0`, `PLATFORM=android-36`
- **Как применяется патч:**
  1. `ci/fetch-upstream.sh` скачивает в `./upstream` один коммит `UPSTREAM_REF` (`--depth 1`, три попытки) и сверяет `HEAD`, затем 19 субмодулей апстрима на записанных коммитах (`submodule update --init --recursive --depth 1`, три попытки) и сверяет их `submodule status`.
  2. `ci/configure.sh` выбирает цель (`dev` или `server`) и вызывает `python3 -I ci/patch.py --source upstream --upstream upstream.env --app config/app.env --server-ipv4 … --server-port … --rsa-public-key … [--server-ipv6] [--google-services] [--abis] [--ccache]`.
  3. Точка входа — `main()` в `ci/patch.py`: `Config(...)` проверяет параметры, `patch(tree, c)` делает правки, `verify(tree, c)` проверяет результат независимо от того, как правки сделаны. Любая `PatchError` — код выхода 1, сборка падает.
  4. `ci/build.sh dev|server` собирает `:TMessagesProj_App:assembleAfatRelease`.
- **Виды правок** (`class Tree` в `ci/patch.py`):
  - `expect(rel, s, count)` — текст должен встречаться ровно `count` раз, файл не меняется;
  - `replace(rel, old, new, count)` — точная замена текста, число вхождений обязано совпасть;
  - `sub(rel, pattern, repl, count)` — замена по регулярному выражению, число совпадений обязано совпасть;
  - `replace_body(rel, head, tail, body, must_contain)` — замена всего между `head` (ровно одно вхождение) и первым `tail` после него; заменяемый блок обязан содержать каждую строку `must_contain`;
  - `replace_in_tree(subdir, suffixes, old, new, count)` — точная замена во всех файлах каталога, общее число вхождений обязано совпасть;
  - ресурсы: замена файла только при совпадении SHA-256 и размера в пикселях с ожидаемым (`BRAND_ASSETS`), добавление файла только под незанятым именем (`BRAND_ADDED`), замена слоя Lottie-анимации при совпадении SHA-256 анимации (`BRAND_LAYERS`).
- **Порядок в `patch()`:** проверка версий → tgnet → идентичность → сборка → Firebase → `birthdays()` → `sticker_sets()` → `saved_tags()` (A-086…A-092; от порядка не зависит) → `self_update()` (A-093…A-097; после проверки `APP_VERSION_NAME` в начале) → `links()` (A-099…A-106; от порядка не зависит: якоря не содержат `tg:`, литералов `"tg:` правка не добавляет) → `brand()`. Перед `birthdays()` стоит `video_quality()` (A-079…A-085); от порядка не зависит. Внутри `brand()`: `brand_entry_points` → `brand_strings` → `brand_client_links` → `brand_scheme` → `brand_assets` → `brand_devices` → `brand_intro` → `brand_theme`. Порядок важен: счётчики `SCHEME_JAVA` взяты после удаления строк поиска настроек (A-021), счётчики `STRING_RENAMES` — после правки `AppName` (A-010).
- **Тесты:** `python3 -I ci/patch_test.py -v` (стандартная библиотека, без сети; 55 тестов, на 2026-11-06 проходят). CI запускает их в job `check` вместе с `shellcheck -x ci/*.sh`.
  - Тесты проверяют помощники (`Tree`, разбор ключа, `Config`), правки дней рождения, наборов стикеров, качества видео, тегов «Избранного», интро и тем на синтетических деревьях, собранных из ожидаемых текстов, и сами ресурсы бренда.
  - **Правки tgnet, идентичности, сборки, строк, схемы ссылок, разделов настроек и замены ресурсов тестами на дереве не покрыты.** Их проверяет только запуск `ci/patch.py` на настоящем апстриме (job `build` в CI или локально `ci/fetch-upstream.sh && ci/configure.sh`) и `verify()` после него.
- **Локальная проверка на настоящем апстриме:** `ci/fetch-upstream.sh && ci/configure.sh` (dev-цель, ключ `config/dev/server_public.pem`), затем `ci/build.sh dev`.

### Что проверять при обновлении апстрима

1. Сменить в `upstream.env` вместе: `UPSTREAM_REF`, `UPSTREAM_VERSION`, `UPSTREAM_LAYER` (только слой, который сервер говорит, ADR 0008), `NDK_VERSION`, `CMAKE_VERSION`, `CMAKE_PACKAGE`, при необходимости `BUILD_TOOLS`, `PLATFORM`.
2. Запустить `python3 ci/patch_test.py`, затем `ci/fetch-upstream.sh && ci/configure.sh` локально. Первая же `PatchError` называет файл и ожидаемый текст. По ID в этом реестре найти, что правка делает и почему, и перенести её на новое место, не ослабляя проверку.
3. Отдельно, потому что правки держатся на числах:
   - счётчики `STRING_RENAMES`, `STRING_LOCAL_NAMES`, `JAVA_NAMES` (A-011…A-014): новые строки с «Telegram» меняют числа; каждую новую строку прочитать, прежде чем менять число;
   - счётчики `SCHEME_JAVA` и `SCHEME_KEEP` (A-016): каждое новое `"tg:` или `"tg"` — либо ссылка (менять на `plm`), либо внутренний URI (оставить и записать в `SCHEME_KEEP`);
   - `count=5` в `ContactsController.java` и `count=11` для MIME-типов (A-008), `count=6` в `shortcuts.xml` (A-018);
   - список языковых папок `values*/strings.xml` (A-011, A-015): новая папка остановит сборку;
   - SHA-256 всех файлов `BRAND_ASSETS` и `BRAND_LAYERS` (A-023…A-033): изменённый upstream-файл остановит сборку; посмотреть, что в нём поменялось, перерисовать (`brand/render.py`), если поменялся размер, и обновить хэш;
   - имена `pluma_devices` и `pluma_mark_write` в `res/raw` (A-034, A-035) не должны появиться у апстрима;
   - значения в таблицах `PALETTE_*` (A-042…A-046): каждая строка требует точного старого значения ключа в `.attheme`;
   - ячейки массивов акцентов `ACCENTS` и база `ACCENT_BASES` в `Theme.java` (A-037, A-038);
   - правки тегов «Избранного» A-086…A-092: счётчики `SAVED_TAGS_USES` (сколько раз каждый файл спрашивает `savedTagsAvailable()`) меняются только вместе с `SAVED_TAGS_EDITS`; новое место апстрима, где теги закрыты `isPremium()`, правка не найдёт — искать по `PREMIUM_FEATURE_SAVED_TAGS`, `TYPE_TAGS`, `areTags()`, `getClientUserId() == getDialogId()` рядом с `isPremium()`;
   - ссылки pluma.chat A-099…A-106: каждое новое `"https://t.me/`, `"http://t.me/` или `"t.me/` в Java — либо ссылка, которую приложение делает само (в `LINKS_OUT`, на pluma.chat), либо внутренний литерал или сервис Telegram (в `LINKS_KEEP`); каждая новая строка с хостом `"t.me"`, `"telegram.me"` или `"telegram.dog"` — либо список хостов (добавить `pluma.chat` в `LINKS_EDITS`), либо проверка подарков и платежей (в `HOSTS_KEEP`); новая встроенная строка с `t.me/` — в `LINK_STRINGS` или `LINK_STRINGS_KEEP`. Регулярные выражения с `t\\.me` (`Pattern.compile`, `matches`) `verify_links` не видит: искать их глазами;
   - правки качества видео A-080, A-081, A-083, A-084 и A-107 вместе дают умолчание 720p на мобильной сети и 1080p по Wi-Fi при `video_upload_maxbitrate` сервера 50/85/100; встроенное 85 в `defaultMedium` (A-083) меняется вместе с серверным;
   - модель цветов `verify_theme` (A-052): она читает `Theme.java` и `ThemeColors.java` регулярными выражениями; при новом способе задавать цвета или исключать ключи из движка акцента она остановит сборку («cannot read», «too few colours»).
4. Проверить, не появились ли новые места, где клиент ведёт к Telegram: новые адреса DC и ключи (A-009 проверит `149.154.`, `95.161.76.100`, `2001:b28:`, `2001:67c:`), новые источники резервных адресов кроме `GoogleDnsLoadTask`/`FirebaseTask` (A-005), новые ссылки на telegram.org в строках, новые пункты, ведущие к Premium/подаркам в баннере дня рождения (A-053…A-059 проверяют только ветку баннера).
5. Проверить на эмуляторе или телефоне: вход, список чатов, баннер дня рождения, Настройки → Устройства, интро (первая страница, переключатель луны), пять встроенных тем, ссылки `plm://`, QR-вход.
6. Поведение, которое мы закрывали правкой, могло быть исправлено апстримом (например A-060, обновление наборов стикеров). Тогда правка не применится; снять её, а не переносить.

---

## Сервер и ключ

### A-001. Проверка закреплённого апстрима

- **Что и зачем.** Перед правками скрипт убеждается, что дерево — тот самый релиз: слой схемы, который говорит сервер (ADR 0008), версия и инструменты, которые ставит CI. Правок нет.
- **Где** (`expect`, точный текст, ровно одно вхождение):
  - `TMessagesProj/src/main/java/org/telegram/tgnet/TLRPC.java`: `public static final int LAYER = 233;`
  - `gradle.properties`: `APP_VERSION_NAME=13.2.1\n`
  - `TMessagesProj/build.gradle` и `TMessagesProj_App/build.gradle`: `ndkVersion "27.2.12479018"`
  - `TMessagesProj/build.gradle`: `version '3.22.1'`
- **Как проверяется.** Сам `expect`; тестов на дереве нет.
- **Если апстрим изменит.** Сборка падает с «N occurrences of …, expected 1; upstream changed it». Это ожидаемо при каждом обновлении: значения меняются в `upstream.env`.
- **Апстрим и Telegram.** Значения апстрима 12.9.1; у официального Telegram те же.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-002. Один датацентр — сервер Pluma

- **Что и зачем.** tgnet знает один DC 1 с адресом сервера Pluma (IPv4, опционально IPv6, порт) для прод- и тест-бэкенда. Иначе клиент ходит к Telegram.
- **Где.** `TMessagesProj/jni/tgnet/ConnectionsManager.cpp`, функция `ConnectionsManager::initDatacenters()`. Замена тела (`replace_body`): от `void ConnectionsManager::initDatacenters() {\n` до первого `\n}\n`. Заменяемое тело обязано содержать `addAddressAndPort("149.154.175.50", 443, 0, "")` и `testBackend`. Новое тело:
  ```cpp
      // pluma: a single datacenter, the Pluma server, on both backends.
      if (datacenters.find(1) == datacenters.end()) {
          Datacenter *datacenter = new Datacenter(instanceNum, 1);
          datacenter->addAddressAndPort("<IPv4>", <port>, 0, "");
          [datacenter->addAddressAndPort("<IPv6 exploded>", <port>, 1, "");]
          datacenters[1] = datacenter;
      }
  ```
  Адрес и порт — из `configure.sh` (переменные `PLUMA_SERVER_*` или `config/dev/server.env`). `Config` требует IPv4-адрес (не имя: tgnet берёт адрес), порт 1–65535.
- **Как проверяется.** `verify()`: ни в одном `jni/tgnet/*.cpp` нет `149.154.`, `95.161.76.100`, `2001:b28:`, `2001:67c:`; в `ConnectionsManager.cpp` ровно 1 (или 2 с IPv6) вызова `datacenter->addAddressAndPort("`. Тестов на дереве нет; `ConfigTest.test_rejects` проверяет отказ на неверных адресе и порте.
- **Если апстрим изменит.** Переименование функции, другой адрес DC 2 или отсутствие `testBackend` в теле — падение на `replace_body`. Новые адреса Telegram в другом месте tgnet поймает `verify()`, если они в опубликованных диапазонах.
- **Апстрим до правки.** Пять DC Telegram (прод и тест), по нескольку адресов IPv4/IPv6, с разветвлением по `testBackend`. Официальный Telegram — так же.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-003. Свежая установка начинает с DC 1

- **Что и зачем.** Новый клиент начинает с единственного DC 1, а не с DC 2 Telegram.
- **Где.** `TMessagesProj/jni/tgnet/ConnectionsManager.cpp`, точная замена `            currentDatacenterId = 2;\n` → `            currentDatacenterId = 1;\n` (ровно одно вхождение).
- **Как проверяется.** Только сам `replace`; `verify()` эту строку отдельно не проверяет.
- **Если апстрим изменит.** Падение «0 occurrences». Если апстрим добавит второе такое место — «2 occurrences».
- **Апстрим до правки.** Свежая установка стартует на DC 2. Официальный Telegram — так же.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-004. RSA-ключ сервера вместо ключей Telegram

- **Что и зачем.** В рукопожатии клиент знает только ключ сервера Pluma и его отпечаток, для обоих бэкендов.
- **Где.** `TMessagesProj/jni/tgnet/Handshake.cpp`. Замена тела (`replace_body`): от `            if (serverPublicKeys.empty()) {\n` до `\n            }\n\n            size_t count2`. Блок обязан содержать отпечатки Telegram `0xd09d1d85de64fd85` и `0xb25898df208d2603`. Новый блок: комментарий `// pluma: the key of the Pluma server, for both backends.`, `serverPublicKeys.emplace_back(<PEM построчно>)` и `serverPublicKeysFingerprints.push_back(<отпечаток>)`.
  - Ключ: PKCS#1 PEM (`BEGIN RSA PUBLIC KEY`), ровно 2048 бит, переписывается в канонический PEM (`canonical_pem`). Отпечаток — младшие 64 бита SHA-1 от TL-сериализации модуля и экспоненты (`key_fingerprint`).
  - Dev-сборка берёт публичный ключ `config/dev/server_public.pem`, сборка под сервер — переменную `PLUMA_RSA_PUBLIC_KEY`.
- **Как проверяется.** `verify()`: в tgnet нет отпечатков Telegram; в `Handshake.cpp` ровно один `serverPublicKeysFingerprints.push_back(` и в нём отпечаток ключа. Тесты `KeyTest.test_telegram_fingerprints` (отпечатки известных ключей Telegram совпадают с теми, что в tgnet), `test_dev_key_fingerprint`, `test_canonical_pem_round_trip`, `test_rejects_other_formats`, `ConfigTest.test_rejects` (1024-битный ключ).
- **Если апстрим изменит.** Другой код вокруг ключей или новый отпечаток — падение `replace_body`. Ключ, добавленный в другом месте, поймает `verify()` только если у него один из двух известных отпечатков; иначе — по счётчику `push_back(`, если он в `Handshake.cpp`.
- **Апстрим до правки.** Прод- и тест-ключ Telegram с разветвлением по бэкенду. Официальный Telegram — так же.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-005. Нет резервных адресов Telegram

- **Что и зачем.** Клиент не запрашивает резервные адреса в DNS (Google DoH) и Firebase Telegram: они вели бы к Telegram.
- **Где.** `TMessagesProj/src/main/java/org/telegram/tgnet/ConnectionsManager.java`, метод `onRequestNewServerIpAndPort(final int second, final int currentAccount)`. Замена тела (`replace_body`) от `    public static void onRequestNewServerIpAndPort(final int second, final int currentAccount) {\n` до первого `\n    }\n`; блок обязан содержать `GoogleDnsLoadTask` и `MozillaDnsLoadTask` (с 13.2.1 вторым источником вместо Firebase идёт DoH Mozilla/Cloudflare). Новое тело — один комментарий `// pluma: the client knows its server; Telegram's backup addresses do not apply.`
- **Как проверяется.** Только сам `replace_body`. Сами классы `GoogleDnsLoadTask`/`MozillaDnsLoadTask` в файле остаются (не вызываются).
- **Если апстрим изменит.** Новая сигнатура или перенос задач в другой метод — падение. Новый источник резервных адресов в другом месте не будет замечен: проверять вручную (чек-лист, п. 4).
- **Апстрим до правки.** При неудаче соединения клиент по очереди спрашивает резервные адреса у DNS и Firebase. Официальный Telegram — так же.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-006. api_id и api_hash Pluma

- **Что и зачем.** Клиент представляется серверу как приложение Pluma для Android: `api_id=10001` и свой `api_hash` (`config/app.env`; не секрет, есть в каждом APK).
- **Где.** `TMessagesProj/src/main/java/org/telegram/messenger/BuildVars.java`, регулярные выражения: `public static int APP_ID = \d+;` и `public static String APP_HASH = "[0-9a-f]{32}";` (ровно одно совпадение каждое).
- **Как проверяется.** Сам `sub`; `ConfigTest` проверяет формат (`api_id > 0`, 32 строчные hex-цифры). `verify()` значения не сверяет.
- **Если апстрим изменит.** Другое объявление (например, `final` или другой тип) — падение.
- **Апстрим до правки.** `api_id` и `api_hash` Telegram для Android.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-007. Пакет приложения `chat.pluma.app`

- **Что и зачем.** Своё имя пакета, чтобы Pluma ставился рядом с Telegram и имел свой Firebase-клиент.
- **Где.**
  - `gradle.properties`: регулярное выражение `(?m)^APP_PACKAGE=.*$` → `APP_PACKAGE=chat.pluma.app`.
  - `BuildVars.java`: точная замена `"org.telegram.messenger.beta"` → `"chat.pluma.app.beta"`.
  - `Config` проверяет, что `PLUMA_PACKAGE` — имя Java-пакета.
- **Как проверяется.** `verify()`: в `gradle.properties` есть `APP_PACKAGE=chat.pluma.app\n`.
- **Если апстрим изменит.** Перенос `APP_PACKAGE` или строки beta-пакета — падение.
- **Апстрим до правки.** `org.telegram.messenger` (beta — `org.telegram.messenger.beta`).
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-008. Сосуществование с Telegram: тип аккаунта и MIME-типы контактов

- **Что и зачем.** Тип аккаунта синхронизации контактов и MIME-типы действий контакта (написать, позвонить) — свои, чтобы Pluma и Telegram на одном телефоне не мешали друг другу в системных «Контактах».
- **Где** (точные замены):
  - `TMessagesProj/src/main/java/org/telegram/messenger/ContactsController.java`: `"org.telegram.messenger")` → `"chat.pluma.app")`, ровно 5 вхождений;
  - `TMessagesProj/src/main/res/xml/auth.xml` и `res/xml/sync_contacts.xml`: `android:accountType="org.telegram.messenger"` → `android:accountType="chat.pluma.app"`;
  - `res/xml/auth_menu.xml`: `android:targetPackage="org.telegram.messenger"` → `android:targetPackage="chat.pluma.app"`;
  - все `.java` и `.xml` в `TMessagesProj/src/main` (`replace_in_tree`): `vnd.org.telegram.messenger.android.` → `vnd.chat.pluma.app.android.`, всего ровно 11 вхождений.
- **Как проверяется.** `verify()`: в `auth.xml` и `sync_contacts.xml` не осталось `org.telegram.messenger`. Остальное — счётчики самих замен.
- **Если апстрим изменит.** Любое изменение числа вхождений (5 и 11) — падение; новое место нужно прочитать и решить, относится ли оно к контактам.
- **Апстрим до правки.** Тип аккаунта и MIME-типы `org.telegram.messenger`.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-009. Итоговая проверка tgnet

- **Что и зачем.** Проверка результата, независимая от того, как сделаны A-002…A-004 (`verify()`): ни в одном `TMessagesProj/jni/tgnet/*.cpp` нет `149.154.`, `95.161.76.100`, `2001:b28:`, `2001:67c:` и отпечатков Telegram; адресов DC 1 или 2 (с IPv6); один отпечаток — ключа сервера; `APP_PACKAGE` правильный; тип аккаунта не Telegram. Правок нет.
- **Если апстрим изменит.** Новые адреса Telegram в tgnet в этих диапазонах остановят сборку. Адреса вне этих префиксов и адреса в Java-коде проверка не видит.
- **История.** `991d01b`, 2026-10-23, задача 006.

---

## Брендинг и тексты

### A-010. Имя приложения «Pluma» во всех языках

- **Что и зачем.** Название под иконкой и в системе — «Pluma» (`PLUMA_APP_NAME` из `config/app.env`), beta — «Pluma Beta».
- **Где.** Каждый `TMessagesProj/src/main/res/values*/strings.xml`, где есть `<string name="AppName">`; регулярные выражения `<string name="AppName">[^<]*</string>` и `<string name="AppNameBeta">[^<]*</string>`, ровно по одному совпадению. Файлов с `AppName` должно быть не меньше 10.
- **Как проверяется.** Счётчики `sub` и порог 10 файлов; позже A-011 и `verify_brand` проверяют, что «Telegram» в тексте строк не осталось.
- **Если апстрим изменит.** Файл с `AppName`, но без `AppNameBeta`, — падение. Меньше 10 файлов — падение.
- **Апстрим до правки.** `Telegram`, `Telegram Beta`.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-011. «Telegram» → «Pluma» в тексте встроенных строк

- **Что и зачем.** Пользователь видит Pluma, а не Telegram, включая названия функций («Pluma Premium», «Pluma Stars»). Ключи строк, теги, URL (`t.me/TelegramTips`, `telegram.org`), имена папок (`Pictures/Telegram`), API мини-приложений (`window.Telegram`) и другие слова («Telegramm») не меняются.
- **Где.** Все `res/values*/strings.xml` (`brand_strings` → `rename_strings`). Меняется только текст элементов (регулярное выражение `>[^<]*<`), внутри — целое слово по `TELEGRAM_WORD = (?:(?<![A-Za-z0-9@/.])|(?<=\\n))(Telegram|TELEGRAM)(?![A-Za-z0-9]|\.\w)` (два символа `\n` перевода строки в тексте заканчивают предыдущее слово, хотя `n` для ретроспективной проверки — буква): `Telegram` → `Pluma`, `TELEGRAM` → `PLUMA`. Число замен закреплено по файлу (`STRING_RENAMES`, `(Telegram, TELEGRAM)`):

  | Папка | Telegram | TELEGRAM |
  |---|---|---|
  | `values` | 534 | 1 |
  | `values-ar` | 0 | 0 |
  | `values-de` | 80 | 0 |
  | `values-es` | 95 | 0 |
  | `values-it` | 96 | 0 |
  | `values-ko` | 1 | 0 |
  | `values-nl` | 79 | 0 |
  | `values-pt-rBR` | 97 | 1 |
  | `values-ru` | 18 | 0 |
  | `values-uk` | 21 | 0 |

  Набор папок должен совпасть с этим списком точно.
- **Как проверяется.** Счётчики при замене; `verify_brand`: ни в одном тексте строк нет `TELEGRAM_WORD`, локальных написаний (`LOCAL_NAMES`) и сайтов клиентов Telegram (`CLIENT_SITES`). Тест `BrandTest.test_rename_strings` проверяет правила замены на образце.
- **Если апстрим изменит.** Любая новая или удалённая строка с именем меняет счётчик — падение с именем файла и числами; новая языковая папка — падение. Каждую новую строку прочитать: может понадобиться A-013 (переписать фразу), а не замена.
- **Апстрим до правки.** «Telegram» во всех строках. Официальный Telegram — так же. Строки языкового пакета сервера (задача 021) сюда не относятся: их отдаёт сервер.
- **История.** `46fefef`, 2026-10-29, задача 017; счётчики менялись только при обновлении апстрима. Задача 074: `WalletAddressNoLinkedAccount` с `\nTelegram` не находился ни заменой, ни `verify_brand`, правило дополнено, `JAVA_NAME_LITERAL` тоже.

### A-012. Имя в других письменностях и в составных словах

- **Что и зачем.** Корейский и арабский пишут имя своей письменностью; им дано «Pluma» латиницей. Корейские частицы — как после слова на гласную. В нидерландском — составное слово.
- **Где.** Внутри текста элементов, после A-011, по порядку (`STRING_LOCAL_NAMES`, старое → новое, число):
  - `values-ar`: `لتيليجرام` → `لـPluma` (8), `تيليجرام` → `Pluma` (80), `تليجرام` → `Pluma` (1);
  - `values-ko`: `텔레그램이라는` → `Pluma라는` (1), `텔레그램이` → `Pluma가` (12), `텔레그램으로` → `Pluma로` (2), `텔레그램은` → `Pluma는` (4), `텔레그램을` → `Pluma를` (5), `텔레그램` → `Pluma` (71);
  - `values-nl`: `Telegramberichten` → `Pluma-berichten` (1).
- **Как проверяется.** Счётчики; `verify_brand` ищет `LOCAL_NAMES` (`텔레그램`, `تيليجرام`, `تليجرام`, `Telegramberichten`) в тексте строк.
- **Если апстрим изменит.** Изменение числа — падение. Новая частица в корейском (например, `텔레그램과`) сейчас попадёт под общую замену `텔레그램` → `Pluma` с неверной частицей; счётчик 71 это поймает, но выбор частицы — вручную.
- **История.** `46fefef`, 2026-10-29, задача 017.

### A-013. Фраза, которая стала бы ложной

- **Что и зачем.** Фраза о видео, отправленных «Telegram 4.7 и раньше», после замены имени была бы ложью; она переписана.
- **Где.** `res/values/strings.xml`, точная замена (до A-011): `Some videos can\'t be streamed – e.g., all videos that were sent from Android using Telegram 4.7 and earlier versions.` → `Some videos can\'t be streamed – e.g., videos that were sent from very old versions of the app.`
- **Как проверяется.** Сам `replace`.
- **Если апстрим изменит.** Падение «0 occurrences».
- **История.** `46fefef`, 2026-10-29, задача 017.

### A-014. Имя в строковых литералах Java

- **Что и зачем.** Тексты, которые видит пользователь, но которые заданы в коде: строки системных «Контактов», экран код-пароля, заглушки служебных аккаунтов, папка приложения в выборе файла и т. п.
- **Где** (`JAVA_NAMES`, точные замены в `TMessagesProj/src/main/java/org/telegram/`, число вхождений):
  - `messenger/ContactsController.java`: `"Telegram Profile"` → `"Pluma Profile"` (1), `"Telegram Voice Call"` → `"Pluma Voice Call"` (1), `"Telegram Video Call"` → `"Pluma Video Call"` (1);
  - `messenger/MessagesController.java`: `user.first_name = "Telegram";` → `user.first_name = "Pluma";` (2);
  - `ui/Components/ChatAttachAlertDocumentLayout.java`: `fs.title = "Telegram";` → `fs.title = "Pluma";` (1);
  - `ui/LoginActivity.java`: `But Telegram says it's` → `But Pluma says it's` (2);
  - `ui/SessionsActivity.java`: `session.app_name = "Telegram Android";` → `session.app_name = "Pluma Android";` (1, текущий сеанс до ответа сервера, с 13.2.0);
  - `ui/ThemePreviewActivity.java`: `"Telegram Beta Chat"` → `"Pluma Beta Chat"` (1).
  С 13.2.1 тексты `MessagePreviewView`, `PasscodeView`, `NewContactBottomSheet` ушли в `strings.xml` (`UnlockToUse`, `NewContactPhone*`) и правит их A-011.
  Ссылка в приглашении (`INVITE_LINK_FILES`, `brand_invite_link`): `"https://telegram.org/dl"` → `"https://pluma.chat/"` в `ui/NewContactBottomSheet.java` (SMS со шторки нового контакта, всегда) и `messenger/ContactsController.java` (`getInviteText`; `help.getInviteText` сервер не обслуживает, поэтому это ссылка всех приглашений: `InviteContactsActivity`, `ChatActivity`, `ContactsActivity`, `AlertsCreator`, `ContactsEmptyView`). Обоих файлов нет в `TELEGRAM_SITE_KEEP`: откат правки останавливает `verify_brand`.
- **Как проверяется.** Счётчики замен и `verify_brand`: строки Java с литералом, где есть слово `Telegram`, — ровно в файлах и количествах `JAVA_NAMES_KEEP` (папки хранилища, API мини-приложений, user agent), строки с литералом `telegram.org` — ровно в `TELEGRAM_SITE_KEEP` (задача 074).
- **Если апстрим изменит.** Изменённый или новый литерал — падение; новый прочитать: виден пользователю — в `JAVA_NAMES`, иначе в таблицу `*_KEEP`.
- **История.** `46fefef`, 2026-10-29, задача 017.

### A-015. Ссылки на клиенты: pluma.chat и web.pluma.chat

- **Что и зачем.** Там, где QR-вход отправляет за клиентом, ссылки ведут на сайт Pluma и его веб-клиент (решение владельца): текст под ноутбуком в Настройки → Устройства (`SessionsActivity`), шаги «Link Desktop Device» (`ActionIntroActivity`), подсказка сканера (`CameraScanActivity`). Это единственные ссылки telegram.org, которые меняет патч.
- **Где.** `res/values*/strings.xml`, по ключу (`CLIENT_LINKS`, `edit_string`). Если старый текст в звёздочках — меняется ссылка внутри фразы (ровно одно вхождение); иначе текст строки обязан быть ровно этим URL.
  - `AuthAnotherClientDownloadClientUrl`: `https://getdesktop.telegram.org/` → `https://pluma.chat/` (`values`); `https://desktop.telegram.org` → `https://pluma.chat/` (`values-ar`); `https://desktop.telegram.org/` → `https://pluma.chat/` (`values-de`, `-es`, `-it`, `-ko`, `-nl`, `-pt-rBR`);
  - `AuthAnotherWebClientUrl`: `https://web.telegram.org/` → `https://web.pluma.chat/` (`values`, `-ar`, `-de`, `-es`, `-it`, `-ko`, `-nl`, `-pt-rBR`);
  - `AuthAnotherClientInfo1`: `*getdesktop.telegram.org*` → `*pluma.chat*` (`values`); `*desktop.telegram.org*` → `*pluma.chat*` (остальные семь);
  - `AuthAnotherClientInfo5`: `*getdesktop.telegram.org*` → `*pluma.chat*` и `*web.telegram.org*` → `*web.pluma.chat*` (`values`).
  - Набор папок, где есть ключ, обязан совпасть со списком; в `values-ru` и `values-uk` этих ключей нет.
- **Как проверяется.** `verify_brand`: ни в одном тексте строк нет `(?:get)?desktop\.telegram\.org|web\.telegram\.org`. Тесты `BrandTest.test_edit_string`, `test_client_links`.
- **Если апстрим изменит.** Другой текст, другой набор языков, второй такой ключ — падение. Новые ключи с этими сайтами поймает `verify_brand`.
- **Апстрим до правки.** Ссылки на getdesktop.telegram.org, desktop.telegram.org, web.telegram.org.
- **История.** `d7cae7e`, 2026-10-30, задача 017 (раздел «Экран „Устройства“ на Android»).

### A-016. Схема ссылок `plm://` вместо `tg://` в коде

- **Что и зачем.** Приложение принимает и создаёт ссылки `plm://`: глубокие ссылки, кнопки ботов, поиск по настройкам, подсказки, возврат плеера к сообщению. QR-вход принимает только `plm://login?token=…`, поэтому работает между клиентами Pluma. `tg://` остаётся у установленного рядом Telegram.
- **Где.** Точные замены в `TMessagesProj/src/main/java/org/telegram/` (`SCHEME_JAVA`): `"tg:` → `"plm:` и `"tg"` → `"plm"`, с числом вхождений (двоеточие, без):
  `messenger/AndroidUtilities.java` (1, 0), `messenger/MusicPlayerService.java` (2, 0), `messenger/NotificationsController.java` (1, 0), `messenger/browser/Browser.java` (3, 1), `ui/Business/BusinessLinksController.java` (1, 0), `ui/CameraScanActivity.java` (1, 0), `ui/QrShare/QrScanOverlay.java` (1, 0), `ui/Cells/ChatMessageBotButtons.java` (2, 1), `ui/ChatActivity.java` (13, 0), `ui/Components/Premium/boosts/GiftInfoBottomSheet.java` (2, 1), `ui/Components/Premium/boosts/UserSelectorBottomSheet.java` (2, 1), `ui/Components/UndoView.java` (3, 0), `ui/LaunchActivity.java` (160, 2), `ui/LinkManager.java` (3, 2), `ui/LoginActivity.java` (1, 0), `ui/PaymentFormActivity.java` (0, 1), `ui/ProfileActivity.java` (75, 0), `ui/SessionsActivity.java` (1, 0), `ui/WebviewActivity.java` (2, 1), `utils/proxy/ProxySettings.java` (7, 1).
  Регулярное выражение `"(?i)^tg:(//)?sendgrams&"` в `ui/LinkManager.java` → `^plm:` (`SCHEME_REGEX`, `verify_brand`).
  Счётчик `ProfileActivity.java` взят после удаления строк поиска «Помощи» (A-021).
- **Что остаётся** (`SCHEME_KEEP`, число литералов `"tg:`/`"tg"`): внутренние URI `tg://`, через которые ExoPlayer читает файлы, и код языка таджикского: `messenger/FileStreamLoadOperation.java` 1, `messenger/MediaController.java` 2, `messenger/TranslateController.java` 1, `messenger/secretmedia/ExtendedDefaultDataSource.java` 1, `ui/Components/Premium/VideoScreenPreview.java` 1, `ui/Components/VideoPlayer.java` 1, `ui/Components/VideoSeekPreviewImage.java` 1, `ui/Stories/PeerStoriesView.java` 2.
- **Как проверяется.** `verify_brand`: по всем `.java` литералы `"tg:`/`"tg"` (регулярное выражение `"tg(?::|")`) остались ровно в файлах и количествах `SCHEME_KEEP`. `BrandTest.test_tables` проверяет, что списки не пересекаются.
- **Если апстрим изменит.** Любой новый или удалённый литерал — падение (в замене или в `verify_brand`). Новый литерал прочитать: ссылка — в `SCHEME_JAVA`, внутренний URI — в `SCHEME_KEEP`.
- **Апстрим до правки.** `tg://`. Официальный Telegram — так же.
- **История.** `36f0a28`, 2026-10-29, задача 017. Связано: задача 026, ссылки `pluma.chat` (A-099…A-106).

### A-017. `plm://` в ссылках текста сообщений

- **Что и зачем.** Ссылки `plm://…` в тексте сообщения становятся кликабельными, как `tg://`.
- **Где.** Точные замены: `messenger/AndroidUtilities.java`: `|ton|tg|tonsite):` → `|ton|plm|tonsite):`; `messenger/LinkifyPort.java`: `|ton|tg|tonsite)://` → `|ton|plm|tonsite)://`.
- **Как проверяется.** Сами замены.
- **Если апстрим изменит.** Новая схема в списке (например, `|ton|tg|tonsite|x)`) — падение.
- **История.** `36f0a28`, 2026-10-29, задача 017.

### A-018. Система отдаёт приложению `plm://`, ярлыки ведут на `plm://`

- **Что и зачем.** Приложение регистрирует схему `plm`, а не `tg`; ярлыки ассистента тоже на `plm://`.
- **Где.** `TMessagesProj/src/main/AndroidManifest.xml`: `<data android:scheme="tg" />` → `<data android:scheme="plm" />` (1); `res/xml/shortcuts.xml`: `android:value="tg://` → `android:value="plm://` (6).
- **Как проверяется.** `verify_brand`: в манифесте нет `android:scheme="tg"`.
- **Если апстрим изменит.** Второй intent-filter с `tg` — падение; новый ярлык — падение по счётчику 6.
- **История.** `36f0a28`, 2026-10-29, задача 017.

### A-019. Скрыт раздел «Помощь» в настройках

- **Что и зачем.** Раздел ведёт только к сервисам Telegram: чат волонтёрской поддержки (`help.getSupport`, сервер тогда не обслуживал), FAQ Telegram, канал советов и политика конфиденциальности Telegram.
- **Где.** Таблица `NOT_SERVED_EDITS`, точные замены (задача 074, раньше — `replace_body`, который с 13.x удалял и новый раздел экспериментальных настроек):
  - `ui/SettingsActivity.java`: заголовок `SettingsHelp` и четыре строки (id 17, 18, 23, 19) убраны вместе с первой тенью раздела экспериментальных настроек; сам раздел (`SharedSettings.experimentalSettingsAllowed`, строки 24, 26, 27) остаётся. Тень перед разделом отладки (`BuildVars.LOGS_ENABLED || DEBUG_PRIVATE_VERSION`) добавляется, только если её ещё нет. Точная замена `        items.add(UItem.asCustomShadow(versionView));\n` (в `brand_entry_points`): перед ней убирается последняя обычная тень, чтобы не было двух подряд;
  - `ui/LogoutActivity.java`: `supportRow = -1` — та же поддержка с экрана выхода (`createSupportAlert` → `help.getSupport`, сервер отвечает `ErrEnterpriseIsBlocked`);
  - `ui/LinkManager.java`, `handleSettings`: ветки `ask-question`/`ask-a-question`, `faq`, `features`, `privacy-policy` убраны; такие ссылки открывают настройки.
  Пути старые (были и в 12.9.1), закрыты в задаче 074. Строки «Помощи» в `ProfileActivity` (`questionRow`, ветка `UserObject.isUserSelf(user) && !myProfile`) мертвы: для себя конструктор ставит `myProfile = true` (`ProfileActivity.java:2209-2211`).
- **Как проверяется.** `verify_not_served`: новые тексты на месте, старых нет, `R.string.SettingsExperimental` ровно одна; `NotServedTest`; порча на настоящем апстриме 13.2.1 останавливает `verify`.
- **Если апстрим изменит.** Блок точный: новая строка внутри него или рядом с якорем — падение. Прочитать её и решить, куда она ведёт.
- **Апстрим до правки.** Раздел «Помощь» с четырьмя пунктами. Официальный Telegram — так же.
- **История.** `00fb7c4`, 2026-10-29, задача 017. Задача 028: «Помощь» вернуть, когда будет куда вести; сейчас скрыта.

### A-020. Скрыт выбор иконки приложения

- **Что и зачем.** Альтернативные иконки — Telegram.
- **Где.** `ui/ThemeActivity.java`, точная замена строк `appIconHeaderRow = rowCount++;`, `appIconSelectorRow = rowCount++;`, `appIconShadowRow = rowCount++;` (три строки подряд, отступ 12) на комментарий `// pluma: no app icon picker; the alternative icons are Telegram's.`
- **Как проверяется.** Сама замена.
- **Если апстрим изменит.** Падение. Сами альтернативные иконки (`activity-alias` в манифесте) остаются в APK.
- **История.** `00fb7c4`, 2026-10-29, задача 017.

### A-021. Убраны строки поиска по настройкам

- **Что и зачем.** Поиск по настройкам не находит скрытые пункты (A-019, A-020).
- **Где.** `ui/ProfileActivity.java`, регулярное выражение, ровно 4 совпадения: `\n {20}new SearchResult\((?:324, getString\(R\.string\.AppIcon\)|402, getString\(R\.string\.AskAQuestion\)|403, getString\(R\.string\.TelegramFAQ\)|404, getString\(R\.string\.PrivacyPolicy\))[^\n]*` → пусто.
- **Как проверяется.** Счётчик `sub`.
- **Если апстрим изменит.** Другие номера или ключи — падение. Новые строки поиска для «Помощи» (например, `TelegramFeatures`) не будут убраны: проверять вручную.
- **История.** `00fb7c4`, 2026-10-29, задача 017.

### A-022. Ресурсы бренда: общий механизм

- **Что и зачем.** Логотипы рендерятся из комплекта бренда владельца (`brand/svg`: `pluma-icon.svg`, `pluma-mark.svg`, `pluma-mark-mono.svg`, `pluma-wordmark.svg`) скриптом `brand/render.py` (`rsvg-convert`, `cwebp`) в `brand/android`, в размерах и форматах заменяемых файлов. Результат закоммичен, CI инструменты рендера не нужны.
- **Где.** `brand_assets()`: набор файлов `brand/android/res` обязан совпасть с `BRAND_ASSETS ∪ BRAND_ADDED`. Каждый файл `BRAND_ASSETS` копируется поверх `TMessagesProj/src/main/res/<путь>` только если SHA-256 upstream-файла равен записанному, а у PNG/WebP совпадает размер в пикселях. Хэши — в `BRAND_ASSETS` в `ci/patch.py` (A-023…A-031).
- **Как проверяется.** `verify_brand`: каждый файл в дереве побайтно равен файлу бренда. `BrandTest.test_assets_are_rendered` (набор файлов, формат по расширению, ненулевой размер), `test_image_size`.
- **Если апстрим изменит.** Изменённый upstream-файл — падение с его именем. Посмотреть, что изменилось; если размер — перерисовать `python3 brand/render.py`; обновить SHA-256.
- **Апстрим до правки** (для A-023…A-033). Логотип Telegram (самолёт, голубой круг), надпись «Telegram»/«TELEGRAM». У официального Telegram — то же.
- **История.** `a946650`, 2026-10-29, задача 017.

### A-023. Иконка приложения

- **Что и зачем.** Иконка лаунчера: адаптивная (передний план, монохромный слой для тематических иконок) и старые иконки до API 26.
- **Где** (замена файлов, SHA-256 upstream в `BRAND_ASSETS`): `mipmap-{mdpi,hdpi,xhdpi,xxhdpi,xxxhdpi}/ic_launcher.png`, `ic_launcher_round.png`, `icon_foreground.png`, `icon_foreground_round.png`, `icon_foreground_sa.png`; `drawable/icon_plane.xml` (монохромный слой).
- **Как проверяется.** A-022.
- **Апстрим до правки.** Самолётик Telegram.
- **История.** `a946650`, 2026-10-29, задача 017.

### A-024. Белый фон адаптивной иконки

- **Что и зачем.** Фон адаптивной иконки белый, как в комплекте бренда.
- **Где.** `res/drawable/icon_background_sa.xml`, точные замены: `android:startColor="#289AD1"` → `android:startColor="#FFFFFF"`, `android:endColor="#38AEEB"` → `android:endColor="#FFFFFF"`.
- **Как проверяется.** Сами замены.
- **Апстрим до правки.** Голубой градиент.
- **История.** `a946650`, 2026-10-29, задача 017.

### A-025. Заставка запуска

- **Где.** `res/drawable/tg_splash_320.xml` (API 31+): круглая иконка в центре 320 dp. Фон заставки — A-050.
- **История.** `a946650`, 2026-10-29, задача 017.

### A-026. Значок уведомления

- **Где.** `drawable-{mdpi,hdpi,xhdpi,xxhdpi}/notification.webp`: белый силуэт знака. Цвет уведомлений — A-049.
- **История.** `a946650`, 2026-10-29, задача 017.

### A-027. Круглая иконка, значок ярлыка чата, логотип условий

- **Где.**
  - `drawable-{mdpi…xxxhdpi}/ic_launcher_dr.webp` — круглая иконка (человек в разговоре, аккаунт звонков, контакты);
  - `drawable-{mdpi…xxxhdpi}/book_logo.webp` — значок ярлыка чата на рабочем столе;
  - `drawable-{mdpi,hdpi}/logo_middle.webp`, `drawable-{xhdpi,xxhdpi}/logo_middle.png` — логотип условий использования, 68 dp.
- **История.** `a946650`, 2026-10-29, задача 017.

### A-028. Слово «pluma» в интро и в заголовке списка чатов

- **Где.** `res/drawable/telegram_logo.xml` (заголовок интро, 83×19 dp, по центру) и `res/drawable/telegram_logo_2.xml` (заголовок списка чатов, 90×20.4 dp, по левому краю). Белые: приложение их тонирует. Положение слова в интро меняет A-035.
- **История.** `a946650`, 2026-10-29, задача 017.

### A-029. Пустая текстура самолёта интро

- **Что и зачем.** Апстрим по-прежнему загружает текстуру самолёта, хотя пропатченный рендерер её не рисует (A-036). Пустая картинка держит самолёт Telegram вне APK.
- **Где.** `drawable-{mdpi,hdpi,xhdpi,xxhdpi}/intro_tg_plane.webp`, размер 82×74 единиц с плотностью.
- **История.** `a946650` (знак на месте самолёта), `d9ad7cf` 2026-10-31 (пустая картинка; задача 020).

### A-030. Центр QR-кодов

- **Где.** `res/raw/qr_logo.svg` (центр QR, который рисует `QRCodeWriter`: белый знак на шалфейном круге); `res/raw/qr_code_logo.json`, `plane_logo_plain.json` (`qr_code_logo_2.json` апстрим удалил в 12.9.2, файл бренда снят; белый неподвижный знак поверх QR и водяного знака круглого видео).
- **История.** `a946650`, 2026-10-29, задача 017.

### A-031. Водяной знак круглого видео без «TELEGRAM»

- **Где.** `res/raw/round_blur_overlay_text.png`: пустая картинка 248×248 вместо надписи «TELEGRAM» по краю.
- **История.** `a946650`, 2026-10-29, задача 017.

### A-032. Знак в анимации «Link desktop device»

- **Что и зачем.** Логотип над овалом в анимации привязки компьютера — знак Pluma.
- **Где.** `res/raw/qr_login.json`, SHA-256 `40cccd31a7f01608dd1e8a3bd4597b05832585f10cdc7a5914204b22c507ab79`; у единственного слоя `"nm": "Logo full"` поле `shapes` заменяется содержимым `brand/android/qr_login_logo.json` (`BRAND_LAYERS`). Файл переписывается компактным JSON.
- **Как проверяется.** `verify_brand`: `shapes` слоя равны файлу бренда. `test_assets_are_rendered`: в файле бренда 4 группы `gr`.
- **Если апстрим изменит.** Любое изменение анимации — падение по SHA-256.
- **История.** `a946650` 2026-10-29 (отдельная константа `QR_LOGIN_SHA256`), `6c01b5f` 2026-10-29 (общая таблица `BRAND_LAYERS`); задача 017.

### A-033. Знак в пояснении к журналу звонков при входе

- **Что и зачем.** Вход просит журнал звонков с анимацией, в которой звонит логотип Telegram (белый круг с вырезанным самолётом). Теперь это белый знак Pluma.
- **Где.** `res/raw/calls_log.json`, SHA-256 `2cbcb5bf3fa757f613e35420704bb4a4fe518e2fff4e23d15a99d6dec87fd6cd`; слой `"nm": "Shape"`, `"ind": 2`; `shapes` ← `brand/android/calls_log_logo.json`.
- **Как проверяется.** Как A-032.
- **История.** `6c01b5f`, 2026-10-29, задача 017 (найдено при проверке на эмуляторе).

### A-034. Настройки → Устройства: анимация ноутбука Pluma

- **Что и зачем.** Над «Link Desktop Device» апстрим показывает стикер №7 (индекс 6) набора `tg_placeholders_android`, который запрашивает у сервера; у сервера Pluma такого набора нет, и место было пустым. Теперь играет встроенная анимация: ноутбук из `Resources/Devices.tgs` Telegram-iOS (релиз 12.9.2, `6ad963e5`, GPL v2 или новее), перекрашенный в палитру, со знаком на месте самолёта на крышке и в QR. Играет как стикер: 130 dp в виде 120 dp, один раз (`setAutoRepeat(2)`) при открытии и снова по нажатию.
- **Где.**
  - Добавленный ресурс `res/raw/pluma_devices.json` (`BRAND_ADDED`, SHA-256 `287368fa82b029ea22b0b7ee0a28654d48557c9eb8f672afd08bddfe759020a7`; источник `brand/lottie/pluma-devices.json`, `render.py` копирует). Добавляется, только если в `res/raw` нет файла с именем `pluma_devices` с любым расширением.
  - `ui/SessionsActivity.java`, точные замены (`SESSIONS_EDITS`):
    - импорт `android.widget.ImageView` после `android.widget.FrameLayout`; `RLottieImageView` перед `RecyclerListView`;
    - удалены импорты `DocumentObject`, `ImageLocation`, `MediaDataController`, `SvgHelper`, `BackupImageView`;
    - `private class ScanQRCodeView extends FrameLayout implements Theme.Colorable, NotificationCenter.NotificationCenterDelegate {` + `BackupImageView imageView;` → без интерфейса наблюдателя, `Theme.Colorable` остаётся, `RLottieImageView imageView;`;
    - создание `new BackupImageView(context)` → `RLottieImageView` с `setAnimation(R.raw.pluma_devices, 130, 130)`, `setAutoRepeat(2)`, `playAnimation()`;
    - `imageView.getImageReceiver().getLottieAnimation()` → `imageView.getAnimatedDrawable()` (4 вхождения, обработчик нажатия);
    - удалён вызов `setSticker();` в конструкторе;
    - удалены целиком `onAttachedToWindow`, `onDetachedFromWindow` (с 13.2 — `addWeakObserver`), `didReceivedNotification` (наблюдатель `diceStickersDidLoad`) и `setSticker()` — от `@Override protected void onAttachedToWindow()` до `private int lastButtonColor, lastButtonPressedColor;` (апстрим добавил после них `updateColors`).
- **Как проверяется.** После правок в файле не должно быть слов `BackupImageView`, `DocumentObject`, `ImageLocation`, `MediaDataController`, `SvgHelper`, `STICKERS_PLACEHOLDER_PACK_NAME`, `diceStickersDidLoad`, `setSticker` (`SESSIONS_GONE`); `verify_brand`: ровно одно `R.raw.pluma_devices`. Тесты `BrandTest.test_devices_animation` (копия источника, 512×512, есть слои), `test_add_asset`.
- **Если апстрим изменит.** Любая из длинных точных замен (особенно блок `setSticker`) — падение. Если апстрим начнёт использовать удалённые импорты в другом месте файла, сработает `SESSIONS_GONE` — тогда импорт надо оставить.
- **Апстрим до правки.** Стикер с сервера (`SessionsActivity.java:1187-1216` по брифу 017). У официального Telegram сервер этот набор отдаёт, ноутбук виден.
- **История.** `d7cae7e`, 2026-10-30, задача 017.

### A-035. Интро, страница 1: знак пишет сам себя

- **Что и зачем.** Первая страница показывает знак Pluma, который рисует себя, как на сайте: кольцо за 1,05 с «на двойках», перо ведёт кончик, затем перья один раз «дышат». Без круга. Играет при появлении страницы и при каждом возвращении на неё; при выключенных системных анимациях сразу показан нарисованный знак. Гаснет с прокруткой страницы, сменяется с картинками других страниц в одном затухании их темпа (0,3 с). Чернила по теме: днём кольцо `#111111`, перья `#4A4A4A`, `#8A8A8A`, `#BDBDBD`; ночью `#E8ECE8`, `#BDBDBD`, `#8A8A8A`, `#6B6F6B`. Переключатель луны перекрашивает.
- **Раскладка «B».** Чернила знака 128 dp (`MARK_DP`), 32 dp над словом (`MARK_GAP_DP`); группа от знака до точек страниц — на 44 % (`MARK_BIAS`) свободного места между строкой состояния и кнопкой «Начать»; кнопка в 24 dp над панелью навигации (`BUTTON_BOTTOM_DP`). Картинки страниц 2–6 кончаются там, где чернила знака. Апстрим центрирует блок в нижней четверти экрана.
- **Где.**
  - Добавленный ресурс `res/raw/pluma_mark_write.json` (`BRAND_ADDED`, SHA-256 `f5d89139ddab9cc3d1ffb2ec42c574307c37e92eb92f5d2413e8fc0855221c41`). Его пишет `brand/mark_write.py` из `brand/svg/pluma-mark.svg` (функция сайта `writeMark()` на сетке 512, одинаковые байты при каждом запуске; с `--site <pluma-site>/src` сверяет константы с исходниками сайта). Слои: `Quill`, `Feather1`, `Feather2`, `Feather3`, `Ring`; 512×512, 60 к/с, кадры 0–132.
  - `ui/IntroActivity.java`, точные замены (`INTRO_EDITS`, по одному вхождению):
    - импорт `android.view.animation.LinearInterpolator` после `android.view.WindowManager`;
    - после `ICON_WIDTH_DP = 200, ICON_HEIGHT_DP = 150;` — константы `MARK_DP`, `MARK_GAP_DP`, `MARK_BIAS`, `MARK_GRID`, `MARK_INK_*` (рамка чернил 79.6…433.1 × 99.95…405.15), `PICTURE_BOTTOM_DP`, `TITLE_TO_DOTS_DP`, `BUTTON_BOTTOM_DP`, `MARK_FADE_MS = 300`, `MARK_LAYERS`, `MARK_INK_LIGHT`, `MARK_INK_DARK`;
    - после `private FrameLayout frameContainerView;` — поля `markView`, `markDrawable`, `markAnimator`, `markInk`, `markShown`, `markScroll`;
    - `logoDrawable.setBounds(0, dp(8.666f), dp(115), dp(35));` → `logoDrawable.setBounds(0, 0, dp(115), dp(26.333f));`;
    - `onLayout` контейнера: блок от `int oneFourth = (bottom - top) / 4;` до `y = oneFourth * 3 + (oneFourth - startMessagingButton.getMeasuredHeight()) / 2;` → раскладка B (`titleTop`, `buttonTop`);
    - после `frameLayout2.addView(textureView, …)` — создание `RLottieDrawable(R.raw.pluma_mark_write, …)` и `RLottieImageView markView` поверх GL-окна;
    - после `bottomPages.setPageOffset(position, positionOffset);` — затухание с прокруткой;
    - `onPageSelected` — `showMark(i == 0, true)`;
    - раскладка заголовков страниц: `int oneFourth …; int y = (oneFourth * 3 - dp(275)) / 2; y += dp(ICON_HEIGHT_DP); y += dp(16 + 9);` → `int y = titleTop(bottom - top);`;
    - после `viewPager.setCurrentItem(0); lastPage = 0; }` — `showMark(viewPager.getCurrentItem() == 0, false)`;
    - `onFragmentDestroy` — `releaseMark()`;
    - перед `getThemeDescriptions()` — методы `buttonTop`, `titleTop`, `showMark`, `releaseMark`, `updateMarkAlpha`, `updateMarkInk`;
    - после `darkThemeDrawable.setColorFilter(…key_featuredStickers_addButton…)` — `updateMarkInk()`.
  - `expect` в `IntroRenderer.c`: `static float duration_const = 0.3f;\n` (темп, которому следует `MARK_FADE_MS`).
- **Как проверяется.** После правок нет `oneFourth`, `dp(275)`, `dp(8.666f)` в `IntroActivity.java` (`INTRO_GONE`); `verify_intro`: каждая строка `INTRO_MARK` ровно один раз. Тесты `IntroTest.test_page_one_shows_the_mark`, `test_layout`, `test_upstream_changed`, `test_check`, `test_every_edit_is_checked` (каждая правка оставляет след в `INTRO_MARK` или `INTRO_GONE`), `test_mark_frames`; `BrandTest.test_mark_write_animation` (байты повторяются, рамка чернил совпадает с константами, имена слоёв, цвета чернил).
- **Если апстрим изменит.** Любая правка `IntroActivity` вокруг раскладки или страниц — падение точной замены. Изменение `duration_const` — падение `expect`. Метод `RLottieDrawable.setCurrentFrame(int, boolean, boolean)`, `setInvalidateOnProgressSet`, `beginApplyLayerColors`/`commitApplyLayerColors`, `AndroidUtilities.getAnimatorDurationScale()` используются в добавленном коде: при их изменении в апстриме упадёт компиляция, не патч.
- **Апстрим до правки.** Сфера (белый круг) и самолёт Telegram, нарисованные GL; блок центрирован в нижней четверти. Официальный Telegram — так же.
- **История.** `d9ad7cf`, 2026-10-31, задача 020 (в коде), бриф 017 — бренд. Предшествующий вариант — A-077, A-078.

### A-036. Рендерер интро: без круга и самолёта, спидометр гаснет

- **Что и зачем.** GL-рендерер интро не рисует сферу, самолёт и маску страницы 1; спидометр страницы 2, который раньше прятался под кругом, теперь плавно гаснет и проявляется.
- **Где.** `TMessagesProj/jni/intro/IntroRenderer.c`, точные замены:
  - страница 1, возврат со страницы 2: `fast_body.params.alpha = 1;` + `fast_body.params.scale = xyzMake(scale, scale, 1);` → `alpha = t(1, 0, 0, duration_const, Linear)`;
  - страница 2 со страницы 1: `fast_body.params.scale = xyzMake(1, 1, 1);` + `fast_body.params.alpha = 1;` → `alpha = t(0, 1, 0, duration_const, Linear)`;
  - весь блок `if (current_page == 0) { … telegram_sphere … telegram_plane … telegram_mask … } else if (current_page == 1) { … }` до `} else if (current_page == 2) {` → `if (current_page == 0 || current_page == 1) { rglNormalDraw(); }`.
- **Как проверяется.** `INTRO_GONE`: в `IntroRenderer.c` нет `draw_textured_shape(&telegram_` и `fast_body.params.alpha = 1;`. Тесты `IntroTest`.
- **Если апстрим изменит.** Любое изменение анимации страниц 1–2 — падение.
- **История.** `d9ad7cf`, 2026-10-31, задача 020.

---

### Цвета: палитра «графит и шалфей»

Палитра из брифа задачи 017 во всех пяти встроенных темах: Blue (дневная по умолчанию), Dark Blue (ночная по умолчанию), Day, Arctic Blue, Night. Акценты, которые тема показывает до выбора цвета пользователем, — шалфей. Ключи из таблиц получают точные цвета палитры, остальной синий темы сдвигает к шалфею движок акцента.

### A-037. Массивы акцентов тем в `Theme.java`

- **Что и зачем.** Домашние акценты и акценты, с которых тема начинает, — шалфей; их пузыри берутся из файла темы, а не из массива «мои сообщения».
- **Где.** `ui/ActionBar/Theme.java`, массивы `themeInfo.setAccentColorOptions(…)` темы с `themeInfo.name = "<тема>";` (11 строк-массивов: 0 акцент, 1 мои сообщения, 2 их градиент, 3–6 цвета обоев, 7 id; у Day две строки `null`). Правка ячеек (`edit_accents`), каждая обязана иметь старое значение (строка, столбец, было → стало):
  - Blue: (7,0) `99`; (0,0) `0xFF5890C5` → `0xFF5B7A5E`; (1,0) `0xFFB8E18D` → `0x00000000`;
  - Day: (7,0) `9`; (0,0) `0xFF56A2C9` → `0xFF4F6E52`; (1,0) `0xFF6580DC` → `0x00000000`; (2,0) `0xFF3EC1D6` → `0x00000000`; (7,5) `0`; (0,5) `0xFF4C91DF` → `0xFF5B7A5E`;
  - Arctic Blue: (7,6) `0`; (0,6) `0xFF3490EB` → `0xFF5B7A5E`; обои (3..6,6) `0xffe0e7ed`→`0xffDDE1DD`, `0xffbed7f3`→`0xffB4C2B6`, `0xffe0e8f3`→`0xffDCE1DD`, `0xffc8dbf3`→`0xffBFCBC1`;
  - Night: (7,5) `0`; (0,5) `0xff3e88f7` → `0xff9DBB9F`; обои `0xff0d0e17`→`0xff101211`, `0xff090a0c`→`0xff0A0B0A`, `0xff181c28`→`0xff1D201E`, `0xff0e0f12`→`0xff0F100F`;
  - Dark Blue: (7,8) `0`; (0,8) `0xff3685fa` → `0xff9DBB9F`; обои `0xff0e141a`→`0xff0F120F`, `0xff172431`→`0xff1A211B`, `0xff0f171e`→`0xff121612`, `0xff141c2b`→`0xff171D18`.
- **Как проверяется.** Сама правка ячеек; `verify_theme` (A-052). Тесты `BrandTest.test_edit_accents`, `test_tables`.
- **Если апстрим изменит.** Другой порядок, число массивов или значение ячейки — падение с темой, строкой и столбцом.
- **Апстрим до правки.** Синие акценты Telegram; у Blue зелёные «мои сообщения», у Day — сине-голубые.
- **История.** `adbded3` 2026-10-29 (Blue, Dark Blue); `4be989b` 2026-10-30 (Day, Arctic Blue, Night); `7c78c00` 2026-10-30 (первый акцент Day `#4F6E52` вместо `#5B7A5E`, чтобы отличался от второго); задача 017.

### A-038. Синий Telegram остаётся базой движка акцента

- **Что и зачем.** Движок сдвигает цвета вблизи оттенка базы к выбранному акценту. База — акцент id 0; раз он стал шалфеем, исходный синий записан явно, чтобы шалфейные акценты перекрашивали тему.
- **Где.** `Theme.java`: перед правкой (A-037) проверяется, что акцент id 0 равен базе (`ACCENT_BASES`: Day `0xFF4C91DF`, Arctic Blue `0xFF3490EB`, Night `0xff3e88f7`, Dark Blue `0xff3685fa`); после конца вызова `setAccentColorOptions(…)` (`\n                );\n`) вставляется `// pluma: Telegram's blue stays the base the sage accents shift from.` и `themeInfo.accentBaseColor = <база>;`.
- **Как проверяется.** `verify_theme`; `test_tables` (база — прежнее значение акцента id 0).
- **Если апстрим изменит.** Другой акцент id 0 — падение. Если апстрим сам начнёт задавать `accentBaseColor`, модель берёт последнее значение.
- **История.** `adbded3` 2026-10-29: только Dark Blue, вставка перед `sortAccents(themeInfo); themes.add(themeInfo); themesDict.put("Dark Blue", …)`. `4be989b` 2026-10-30: общая вставка для четырёх тем. Задача 017.

### A-039. Домашние акценты тёмных тем берут пузыри из файлов тем

- **Что и зачем.** Апстрим навязывает сине-фиолетовый градиент пузырей домашнему акценту Dark Blue и каждому акценту Night (Night — ещё и обои). Теперь домашние акценты обеих тем берут пузыри из файла темы, а обои — из массивов (A-037); остальные акценты Night, выбранные пользователем, сохраняют градиент и обои.
- **Где.** `Theme.java` (`ThemeInfo.setAccentColorOptions`), точная замена `                if (isHome(themeAccent) && name.equals("Dark Blue") || name.equals("Night")) {\n` → комментарий и `                if (name.equals("Night") && !isHome(themeAccent)) {\n`.
- **Как проверяется.** `DEFAULT_ACCENT_SHORTCUTS` в `verify_theme`: в файле не должно быть `isHome(themeAccent) && name.equals("Dark Blue")` и `name.equals("Night")) {\n                    themeAccent.myMessagesAccentColor`. Тест `ThemeTest.test_forced_bubbles`.
- **Апстрим до правки.** `Theme.java:2610-2621` по брифу 017.
- **История.** `adbded3` 2026-10-29 (условие `if (name.equals("Night"))` — только Dark Blue); `4be989b` 2026-10-30 (нынешнее условие, домашний акцент Night); задача 017.

### A-040. Домашний акцент Blue идёт через файл темы и движок

- **Что и зачем.** Апстрим для домашнего акцента темы по умолчанию отдаёт встроенные цвета `ThemeColors`, минуя `bluebubbles.attheme` и движок акцента: на свежей установке светлая тема оставалась синей, хотя палитра в файле была. Теперь он как любой другой акцент.
- **Где** (`Theme.java`, точные замены):
  - в `getColor`: весь блок `        if (currentTheme == defaultTheme) {\n            boolean useDefault; … return getDefaultColor(key);\n            }\n        }\n` → комментарий;
  - в `changeColorAccent`: `if (accent == 0 || themeInfo.accentBaseColor == 0 || accent == themeInfo.accentBaseColor || themeInfo.firstAccentIsDefault && themeInfo.currentAccentId == DEFALT_THEME_ACCENT_ID) {` → без последнего условия;
  - в `saveCurrentTheme`: `boolean useDefaultColors = currentTheme.firstAccentIsDefault && accent.id == DEFALT_THEME_ACCENT_ID;` → `boolean useDefaultColors = false;`.
- **Как проверяется.** `DEFAULT_ACCENT_SHORTCUTS`: нет `currentTheme.isDefaultMainAccent()`, `isDefaultMyMessages()`, `isDefaultMyMessagesBubbles()`, `themeInfo.firstAccentIsDefault && themeInfo.currentAccentId == DEFALT_THEME_ACCENT_ID`, `currentTheme.firstAccentIsDefault && accent.id == DEFALT_THEME_ACCENT_ID`. Тест `ThemeTest.test_default_accent_shortcuts`.
- **Если апстрим изменит.** Падение замен; новый такой же обход в другом методе поймает `DEFAULT_ACCENT_SHORTCUTS`, если в нём одна из этих строк.
- **История.** `34275b7`, 2026-10-29, задача 017 (найдено проверкой на эмуляторе).

### A-041. Превью тем по умолчанию

- **Где.** `Theme.java`, точные замены: `previewBackgroundColor = 0xff5f6e82` → `0xff2a302a`, `previewInColor = 0xff76869c` → `0xff3b423b`, `previewOutColor = 0xff82a8e3` → `0xff4f6552` (одним блоком); `return 0xffffffff;\n            }\n            return previewInColor;` → `0xfff1f2f0`; `return 0xfff0fee0;` → `0xffe3eae2`; `return 0xffcfd9e3;` → `0xffdde3dc`.
- **Как проверяется.** Сами замены.
- **История.** `adbded3`, 2026-10-29, задача 017.

### A-042. `bluebubbles.attheme` (Blue)

- **Что и зачем.** Точные цвета палитры: текст `#111111`, вторичный текст `#6B6F6B`, акцент `#5B7A5E` (нажатие `#4F6E52`), текст на акценте белый, ссылки `#4F6E52`, выделение, пузыри (входящий `#F1F2F0`, исходящий `#E3EAE2` с градиентом `#D5E1D4`, текст `#1F2E20`), время и галочки исходящих `#5B7A5E`, цвета людей (A-047), простые синие (A-048).
- **Где.** `TMessagesProj/src/main/assets/bluebubbles.attheme` (`edit_attheme`, таблица `PALETTE_LIGHT` в `ci/patch.py`, 74 ключа). Каждая строка — (ключ, прежнее значение или `None`, новый цвет): ключ с прежним значением обязан быть в файле ровно один раз с этим значением; ключ с `None` обязан отсутствовать и дописывается в конец. Ключ должен быть в `colorKeysMap` из `ThemeColors.java`. Незавершённая последняя строка файла (её приложение не читает) остаётся последней.
- **Как проверяется.** `edit_attheme`; `verify_theme` (A-052). Тесты `BrandTest.test_edit_attheme`, `test_tables`.
- **Если апстрим изменит.** Любое изменённое значение ключа из таблицы — падение с ключом.
- **История.** `adbded3` 2026-10-29; `34275b7` (`dialogTopBackground`, шапка пояснений разрешений при входе); `7a9ccbb`, `7c78c00` 2026-10-30 (A-047, A-048); задача 017.

### A-043. `darkblue.attheme` (Dark Blue)

- **Что и зачем.** Фоны `#131513`, `#0D0F0D`, `#1B1F1B`, `#161916`, `#1E221E`, `#232623`; текст `#E8ECE8`; вторичный `#9AA29A`; акцент `#9DBB9F` (нажатие `#B2CEB4`), текст на акценте `#131513`; ссылки `#B2CEB4`; пузыри (входящий `#232623`, исходящий `#2C3A2D` с градиентом `#36473A`), время и галочки `#B2CEB4`.
- **Где.** `assets/darkblue.attheme`, таблица `PALETTE_DARK` (правила как в A-042).
- **История.** `adbded3` 2026-10-29; `7a9ccbb`, `7c78c00`; задача 017.

### A-044. `day.attheme` (Day)

- **Что и зачем.** Ключи как в Blue, кроме исходящего пузыря: Day сохраняет цветной исходящий пузырь Telegram с белым текстом, залитый акцентом (`#5B7A5E` → градиент `#6F8F72`, выбранный `#4F6E52`); время, просмотры и сведения о файле на нём — светлый шалфей `#E3EAE2` (решение владельца 2026-10-30). Контраст белого 4,8:1 и 3,6:1 на светлом конце (у Telegram 4,2:1 и 2,3:1); время 3,9:1 и 2,9:1 (у Telegram 3,2:1 и 1,8:1).
- **Где.** `assets/day.attheme`, таблица `PALETTE_DAY`.
- **Как проверяется.** Также `BrandTest.test_outgoing_contrast`: написанное на исходящем пузыре читается не хуже, чем цвета Telegram на пузыре Telegram (WCAG 2).
- **История.** `4be989b` 2026-10-30; `7a9ccbb`, `7c78c00`; задача 017.

### A-045. `arctic.attheme` (Arctic Blue)

- **Что и зачем.** Как Day; исходящий пузырь без градиента (`#5B7A5E`, выбранный `#4F6E52`).
- **Где.** `assets/arctic.attheme`, таблица `PALETTE_ARCTIC`.
- **История.** `4be989b` 2026-10-30; `7a9ccbb`, `7c78c00`; задача 017.

### A-046. `night.attheme` (Night)

- **Что и зачем.** Ключи как в Dark Blue, кроме собственных нейтральных серых Night (панели, меню, разделы, диалоги) — они не синие и остаются. Night включает переключатель луны на интро, поэтому его синий был виден сразу.
- **Где.** `assets/night.attheme`, таблица `PALETTE_NIGHT`.
- **История.** `4be989b` 2026-10-30 (найдено на телефоне владельца); `7a9ccbb`, `7c78c00`; задача 017.

### A-047. Цвета людей: синий и бирюзовый

- **Что и зачем.** Telegram даёт каждому пользователю один из семи цветов по `id % 7` для имени, ответов, превью ссылок и буквенного аватара (`UserObject.getColorId`). Синий слот (5) — шалфей: аватар `#7FA082`→`#5B7A5E`, имя `#4F6E52`, в тёмных темах `#9DBB9F`. Бирюзовый (4) — бирюза без синего: аватар `#5BCBC4`→`#2C9C98`, имя `#1F8A80`; в тёмных темах имя остаётся `#53CCC6` Telegram. Остальные пять цветов — как у Telegram. Решения владельца 2026-10-30.
- **Где.** Ключи `avatar_backgroundBlue`, `avatar_background2Blue`, `avatar_nameInMessageBlue`, `avatar_backgroundCyan`, `avatar_background2Cyan`, `avatar_nameInMessageCyan` в таблицах всех пяти тем (A-042…A-046).
- **Как проверяется.** Движок акцента эти ключи пропускает, но `verify_theme` их проверяет (`ALSO_CHECKED`).
- **Апстрим и Telegram.** Синий и бирюзовый Telegram. Те же значения перенесены в iOS, Desktop и Web K (бриф 020).
- **История.** `7a9ccbb` 2026-10-30 (синий), `7c78c00` 2026-10-30 (бирюзовый); задача 017.

### A-048. Простые синий и голубой, «Внутренняя память»

- **Что и зачем.** `color_blue` и `color_lightblue` (ползунок скорости воспроизведения, право бота «Эмодзи-статус», запасные линии графиков статистики) и `chat_attachGalleryBackground` («Внутренняя память» в выборе файла) — шалфей.
- **Где.** Эти ключи в таблицах всех пяти тем.
- **Как проверяется.** `ALSO_CHECKED` в `verify_theme`.
- **История.** `7c78c00`, 2026-10-30, задача 017.

### A-049. Цвет уведомлений и звонков

- **Где** (`BRAND_COLOURS`, точные замены с числом вхождений):
  - `messenger/NotificationsController.java`: `0xff11acfa` → `0xff5b7a5e` (2);
  - `messenger/voip/VoIPService.java`: `0xff2ca5e0` → `0xff5b7a5e` (3);
  - `messenger/voip/VoIPGroupNotification.java`, `messenger/voip/VoIPPreNotificationService.java`: `0xff2ca5e0` → `0xff5b7a5e` (по 1);
  - `res/drawable/call_custom_notification_icon.xml`: `#2ca5e0` → `#5b7a5e` (1).
- **Как проверяется.** `verify_theme`: старых литералов нет. Тест `ThemeTest.test_colour_literals_left`.
- **История.** `adbded3`, 2026-10-29, задача 017.

### A-050. Платформенные стили и фон заставки

- **Где** (`BRAND_COLOURS`):
  - `res/values/styles.xml`: `#ff527da3` → `#ff5b7a5e` (1);
  - `res/values-v21/styles.xml`: `#527da3` → `#5b7a5e` (6), `#ff527da3` → `#ff5b7a5e` (1), `#426482` → `#4f6e52` (1), `#4991cc` → `#5b7a5e` (5), `#408bc8` → `#5b7a5e` (4);
  - `res/values-v31/styles.xml`: `#527da3` → `#5b7a5e` (3), `#426482` → `#4f6e52` (1);
  - `res/values-night/styles.xml`: `#1f2732` → `#131513` (3), `#1c242e` → `#0d0f0d` (1), `#232d3a` → `#1b1f1b` (1), `#ff232d3a` → `#ff1b1f1b` (1);
  - `res/values-night/colors.xml`: `#75ADE5` → `#9DBB9F` (1).
- **Что это.** Выборщики, недавние, фон заставки, тёмные фоны системных окон.
- **Как проверяется.** Как A-049.
- **История.** `adbded3`, 2026-10-29, задача 017.

### A-051. Индикатор страниц и картинки интро

- **Где** (`BRAND_COLOURS`):
  - `ui/Components/BottomPagesView.java`: `paint.setColor(ThemeColors.TELEGRAM_COLOR);` → `paint.setColor(0xFF5B7A5E); // pluma: the sage accent`;
  - `jni/intro/IntroRenderer.c`: `gl_FragColor = vec4(0,0.6,0.898,p);` → `vec4(91./255.,122./255.,94./255.,p)`; `gl_FragColor = vec4(42./255.,180./255.,247./255.,p);` → `vec4(111./255.,143./255.,114./255.,p)`; `{42 / 255.0f, 180 / 255.0f, 247 / 255.0f, 1}` → `{111 / 255.0f, 143 / 255.0f, 114 / 255.0f, 1}`; `{47 / 255.f, 90 / 255.f, 131 / 255.f, 1}` → `{79 / 255.f, 110 / 255.f, 82 / 255.f, 1}`.
- **История.** `adbded3`, 2026-10-29, задача 017.

### A-052. Проверка: синего Telegram в темах нет

- **Что и зачем.** Правки палитры проходили все точные проверки, а светлая тема их не использовала (A-040). Поэтому патч проверяет, что получает экран. `verify_theme` читает пропатченные `Theme.java`, `ThemeColors.java` и пять файлов тем и для каждого акцента, который тема показывает до выбора цвета (`DEFAULT_THEMES`: Blue 99; Day 9 и 0; Arctic Blue 0; Night 0; Dark Blue 0), моделирует ответ `Theme.getColor` на каждый ключ: файл темы, движок акцента поверх файла и встроенных цветов (`ThemeAccent.fillAccentColors`, `Theme.changeColorAccent`: сдвиг в пределах 30° от оттенка базы, светлая тема не светлеет, тёмная не темнеет), `fallbackKeys`. Падает на любом цвете в синем Telegram (оттенок 180–240°, насыщенность и яркость ≥ 0,3, альфа ≥ 0x20) вне ключей, которые движок пропускает (`themeAccentExclusionKeys`: цвета людей, звонки, статистика, Premium, истории), плюс `ALSO_CHECKED` (A-047, A-048). Правок нет.
- **Защита от пустой модели.** Если из `ThemeColors.java` прочитано меньше 500 ключей или меньше 100 синих значений — падение. Неизвестная форма исключения из движка — падение. Акцент с цветами «моих сообщений» модель не умеет — падение.
- **Тесты.** `ThemeTest.test_accent_shift`, `test_theme_defaults`, `test_theme_file_values`, `test_blue_left`, `test_default_accent_shortcuts`, `test_forced_bubbles`, `test_colour_literals_left`.
- **Если апстрим изменит.** Любая новая синяя по умолчанию строка в `ThemeColors.java`, не попадающая под сдвиг, остановит сборку с именем ключа: её добавить в таблицу темы.
- **Звонки (задача 094).** Ключи звонков движок по-прежнему пропускает, и эта проверка их не смотрит; с задачи 094 их и литералы экранов звонка проверяет `verify_call_colours` (A-122): не синий, а любой цвет экранов звонков Telegram. Ветка `task/094-group-calls-e2e`, не влито, не выпущено (2026-11-13).
- **История.** `4a1711e` 2026-10-29 (Blue и Dark Blue; сборка на `72cf711` эту проверку не проходит); `4be989b` 2026-10-30 (пять тем, все такие акценты); `7a9ccbb`, `7c78c00` (`ALSO_CHECKED`); задача 017; описание проверки в README — `24f3c2d`, 2026-10-29 (только документация).

### A-114. Значок верификации в цветах Плюмы (задача 080)

- **Что и зачем.** Аккаунт Плюмы (@support) получает `user.verified`; значок рисуется той же розеткой с галочкой, что у Telegram, но в цветах палитры: розетка — акцент темы, галочка — текст на акценте. Светлые темы (Blue, Day, Arctic Blue): `#5B7A5E` и белая галочка `#FFFFFF`; тёмные (Night, Dark Blue): `#9DBB9F` и галочка `#131513`. Форма не меняется, ресурсы не перекрашиваются: `verified_area.webp`, `verified_check.webp` и `verified_profile.webp` в апстриме белые (проверено декодированием `drawable-xxhdpi`), цвет им даёт фильтр `MULTIPLY`/`SRC_IN`.
- **Где.**
  - Таблицы тем `PALETTE_LIGHT`, `PALETTE_DARK`, `PALETTE_DAY`, `PALETTE_ARCTIC`, `PALETTE_NIGHT` (A-042…A-046), по четыре ключа после «текста на акценте»: `chats_verifiedBackground`, `chats_verifiedCheck`, `profile_verifiedBackground`, `profile_verifiedCheck`. Прежние значения апстрима: `bluebubbles.attheme` — `chats_verifiedBackground=-13458971`, остальные три ключа не заданы; `day.attheme` — `-11358728`, `profile_verifiedCheck=-1`, `profile_verifiedBackground=-11296533`; `arctic.attheme` — `-12998152`, `-1`, `-12211980`; `night.attheme` — `-12145929`, `-1`, `-10636552`; `darkblue.attheme` — `-10177041`, `-1`, `-10177027`; `chats_verifiedCheck` не задан ни в одном файле.
  - `BRAND_COLOURS`: `ui/Cells/GroupCallUserCell.java`, класс `VerifiedDrawable` (список участников голосового чата, всегда тёмный, ключей темы не берёт), две строки `drawables[0].setColorFilter(new PorterDuffColorFilter(0xff75B3EE, PorterDuff.Mode.MULTIPLY));` + `drawables[1] = …verified_check).mutate();` → комментарий `// pluma: …`, фильтр `0xff9DBB9F` и тот же `drawables[1]` с фильтром `0xff131513`.
- **Где рисуется значок (апстрим 13.2.1, `7be672ea`).** Все места берут цвет из этих ключей: `Theme.dialogs_verifiedDrawable`/`dialogs_verifiedCheckDrawable` (`Theme.java` 7953–7956, окраска 8063–8064: список чатов `DialogCell`, `ProfileSearchCell` в поиске и списках участников, `StatusBadgeComponent`, `CheckBoxUserCell`, `JoinGroupAlert`, `DialogMeUrlCell`); `Theme.profile_verifiedDrawable` (8987–8988, 9024–9025: профиль `ProfileActivity`); шапка чата `ChatAvatarContainer` (960–963, 1688–1719: `profile_verified*`); подпись пересланного `ChatMessageCell.getForwardVerifiedSpan` и диалоги `AlertsCreator` (`chats_verified*`); значок эмодзи-статуса, значок верификации бота и звезда в `DialogsActivity`, `SettingsActivity`, `PeerColorActivity`, `UserCell`, `ReactedUserHolderView`, `MessageSeenView`, `PollVotesAlert`, `SelectorUserCell`, `EmojiView`, `PremiumGradient`, `BotVerifySheet` — `chats_verifiedBackground`/`profile_verifiedBackground`; списки `ThemeDescription` в `CallLogActivity`, `ContactsActivity`, `FilteredSearchView` перекрашивают по тем же ключам. Вне ключей: `GroupCallUserCell` (правка выше); `BotWebViewSheet`/`BotWebViewAttachedSheet` — `featuredStickers_addButton` (уже акцент палитры); `PeerStoriesView` и вторая половина `CrossfadeDrawable` профиля — белый `verified_profile` поверх медиа, `DialogStoriesCell.createVerifiedDrawable` и `BotFullscreenButtons` — цвета шапки и белый: синего там нет, не правятся.
- **Апстрим без правки.** Движок акцента (A-052) сдвигает синюю розетку к шалфею, но в каждой теме в свой оттенок (модель: Blue `#648765`/`#658D68`, Day от `#547456` до `#658865`, Arctic Blue `#608161`/`#617F62`, Night `#A1BB9E`/`#A5BCA3`, Dark Blue `#A0B39D`/`#A7BDA6`), а галочка остаётся белой — на светлом шалфее тёмных тем это около 2:1. В голосовом чате розетка голубая `#75B3EE` Telegram.
- **Как проверяется.** `verify_theme` (A-052) теперь вызывает и `default_theme_badges`: для каждого акцента `DEFAULT_THEMES` модель `Theme.getColor` должна дать розеткам цвет `chats_unreadCounter`, галочкам — `chats_unreadCounterText` той же темы (`VERIFIED_BADGE`); иначе падение с файлом темы и ключами. Без строк таблиц сборка на `7be672ea` падает: `bluebubbles.attheme: the verified badge is not the accent with the text on it: chats_verifiedBackground, profile_verifiedBackground` (в тёмных темах — все четыре ключа). Литерал `0xff75B3EE` проверяет `verify_theme` как любой из `BRAND_COLOURS` (`test_colour_literals_left`). Тесты `ThemeTest.test_verified_badge` (модель, переименованный ключ апстрима останавливает проверку) и `test_verified_badge_tables` (в каждой таблице значок = акцент и текст на нём). Полный прогон `ci/patch.py` с проверкой на копии апстрима `7be672ea` проходит.
- **Если апстрим изменит.** Другое значение ключа в файле темы — падение `edit_attheme` с ключом; переименованный ключ — падение `default_theme_badges`; изменённые строки `VerifiedDrawable` — падение замены `BRAND_COLOURS`.
- **Не проверено.** На устройстве и эмуляторе не смотрелось (сборка не запускалась).
- **История.** Ветка `task/080-verified-badge` репозитория `pluma-android`, 2026-11-11, не закоммичено; задача 080.

### A-115. Качество личного звонка из `custom_parameters` (задача 077, этап 2)

- **Что и зачем.** Значения качества личного звонка задаёт сервер на каждый звонок в `phoneCall.custom_parameters` (ADR 0004: «один патч tgcalls, лимиты от сервера»). Отсутствующий ключ, не число или значение вне диапазона оставляют значение Telegram, поэтому звонок без ключей — звонок Telegram, и владелец сравнивает (A/B), меняя значения на сервере. Ключи, единицы и диапазоны — сервера (`pkg/phonecalls/parameters.go`, `docs/ops/calls.md`): кбит/с сервера — 1000 бит/с, а собственные значения Telegram tgcalls пишет как кбит × 1024 (`32 * 1024`), поэтому члены движка хранят бит/с, ключ `*_kbps` умножается на 1000 (`getCustomParameterKbpsAsBps`), а без ключа остаётся число апстрима. Таблица `CALL_PARAMETERS` в `ci/patch.py`:

  | Ключ | Telegram (апстрим) | Диапазон | Где действует |
  |---|---|---|---|
  | `audio_ptime_ms` | 60 | 10–120 | `OutgoingAudioChannel`: `kCodecParamPTime` отправителя |
  | `audio_max_bitrate_kbps` | 32·1024 бит/с | 6–128 | `adjustBitratePreferences`: потолок аудио |
  | `audio_min_bitrate_kbps` | нет (Opus стоит на потолке) | 6–128 | полевой эксперимент `WebRTC-Audio-Allocation/min:…bps,max:…bps/`, только если ≤ потолка |
  | `audio_jitter_min_ms` | 50 | 1–500 | `IncomingV2AudioChannel`: `audio_jitter_buffer_min_delay_ms` получателя |
  | `audio_rnnoise` | выключено | `true`/`false` | APM: `AudioCapturePostProcessor` групповых звонков (RNNoise) как пост-обработка захвата |
  | `video_max_bitrate_kbps` | 1000·1024 бит/с | 100–20000 | `adjustBitratePreferences`: потолок камеры |
  | `screen_max_bitrate_kbps` | нет (умолчание WebRTC по размеру кадра) | 100–20000 | `adjustBitratePreferences`: потолок экрана, у апстрима его нет |
  | `start_bitrate_kbps` | нет (300 кбит/с WebRTC) | 30–20000 | `CallConfig.bitrate_config.start_bitrate_bps` |
  | `screen_max_size_px` | ≈1000 | 320–3840 | Java: `VideoCapturerDevice.getScreenCaptureSize` |

  Ключи сервера `video_fps` и `screen_fps` Android не читает: камера и экран захватываются с 30 кадр/с (`VideoCapturerDevice.CAPTURE_FPS`), это и есть значение Плюмы.

- **Почему эти значения** (исследование `docs/tasks/077-research/` сервера): при 60 мс Opus шлёт три кадра по 20 мс, и FEC восстанавливает только последние 20 мс потерянного пакета; 20 мс дают полное восстановление и около 40 мс меньше задержки (`audio.md`, «In-band FEC», «Opus packet time»). Без диапазона Opus стоит на 32 768 бит/с при любой сети: `UpdateAllowedBitrateRange` ставит min = max = цели (`audio.md`, «Opus bitrate»; `webrtc_voice_engine.cc:1105-1126`), диапазон через эксперимент читает `AudioSendStream::GetMinMaxBitrateConstraints` (`audio_send_stream.cc:847-850`). Порог джиттера 50 мс не маскируется пакетами 60 мс (`audio.md`, «Jitter buffer»). Экран в 1:1 без потолка, камера — 1000·1024 (`video.md`, «Camera send cap», «Screen-share send cap»). Старт 300 кбит/с роняет первые кадры 720p и понижает разрешение (`video.md`, «Start bitrate»). Экран Android ужимается до ≈1000 px по длинной стороне (`video.md`, «Screen capture path»).
- **Где** (`CALL_PARAMETERS_EDITS`, 20 точных замен; tgcalls `545f5ae`, субмодуль `TMessagesProj/jni/tgcalls` апстрима 13.2.1; Java — апстрим `7be672ea`):
  - `tgcalls/v2/CustomParameters.h`: новые `getCustomParameterIntOr(parameters, name, default, min, max)` (у апстримной `getCustomParameterInt` отсутствие ключа неотличимо от 0) и `getCustomParameterKbpsAsBps(parameters, name, defaultBps, minKbps, maxKbps)` (кбит/с сервера × 1000, иначе число апстрима в бит/с) перед `} // namespace tgcalls`;
  - `tgcalls/v2/InstanceV2Impl.cpp` (движок 9.0.0, который сервер выбирает между нативными клиентами):
    - подключение `group/GroupAudioCapturePostProcessor.h`, `<mutex>`, `<set>`; `persistentFieldTrials` в начале анонимного пространства имён: WebRTC хранит указатель на строку экспериментов (`field_trial.cc:163-170`), поэтому каждая разная строка живёт до конца процесса;
    - конструктор `InstanceV2ImplInternal`: апстримный вызов `InitFieldTrialsFromString` с четырьмя экспериментами заменён комментарием; после разбора `_customParameters` читаются ключи, строка экспериментов — те же четыре плюс, при `audio_min_bitrate_kbps`, `WebRTC-Audio-Allocation`; одна строка лога с числами;
    - конструкторы `OutgoingAudioChannel` (`int ptimeMs`) и `IncomingV2AudioChannel` (`int jitterBufferMinDelayMs`) и два их вызова в `createNegotiatedChannels` (третий — в закомментированном блоке апстрима, не тронут);
    - `callConfig.audio_state = …` → старт оценки канала; `AudioProcessingBuilder builder;` → пост-обработка RNNoise под `#if USE_RNNOISE`; `adjustBitratePreferences` → потолки из членов и потолок экрана; члены после `_customParameters` с умолчаниями апстрима.
  - `messenger/voip/VideoCapturerDevice.java`: `public static volatile int screenCaptureMaxSide` после `CAPTURE_FPS`; в `getScreenCaptureSize` числа 1000, 800, 970 → `limit`, `floor`, `fallback` (при 0 — те же числа, иначе в тех же пропорциях: ×1, ×0,8, ×0,97); при заданном пределе экран меньше предела не увеличивается.
  - `messenger/voip/NativeInstance.java`: импорт `org.json.JSONException`, `org.json.JSONObject`; `make()` ставит предел из `config.customParameters` перед `makeNativeInstance`, `stop()` (только личный звонок, у групп `stopGroup`) сбрасывает в 0; групповые звонки и эфиры историй (`LivePlayer`) живут с пределом апстрима.
- **Апстрим без правки.** `VoIPService.java:3529` уже передаёт `custom_parameters` в tgcalls; `InstanceV2Impl` читает из них только ключи `network_*` (`video.md`, «phoneCall.custom_parameters»). Пакет Opus 60 мс, Opus 32·1024 бит/с без диапазона, порог джиттера 50 мс, камера 1000·1024, экран без потолка, старт 300 кбит/с, без RNNoise в личных звонках, захват экрана ≈1000 px.
- **Как проверяется.** `verify_call_parameters`: каждая замена ровно один раз, якорь остаётся только внутри своей новой строки; в движке нет `kCodecParamPTime, 60)`, `audio_jitter_buffer_min_delay_ms = 50;`, `setMaxBitrate(32 * 1024)`, `setMaxBitrate(1000 * 1024)`; `InitFieldTrialsFromString(` ровно один; каждый ключ читается ровно раз. `call_parameters` сначала проверяет все якоря, потом пишет: дерево с уже применённым патчем падает, ничего не записав. Тесты `CallParametersTest`: `test_defaults_are_upstreams` (умолчание каждого ключа равно выражению из якоря апстрима, например `32 * 1024`, у ключей без значения апстрима умолчание 0 вне диапазона; члены класса стартуют с тех же чисел; числа захвата экрана при 0 — 1000/800/970; с подменённым умолчанием `audio_ptime_ms` тест падает), `test_edits`, `test_second_run`, `test_upstream_changed`, `test_check`. Полный `ci/patch.py` с `verify` на копии апстрима `7be672ea` и tgcalls `545f5ae` проходит; изменённые `.java` разобраны `javac` без синтаксических ошибок.
- **Если апстрим изменит.** Сдвинутый якорь — `PatchError` с файлом и текстом. Новый вызов `InitFieldTrialsFromString` или возвращённое число апстрима — падение `verify_call_parameters`. При смене пина tgcalls сверить, что движок 9.0.0 всё ещё `InstanceV2Impl` и что `custom_parameters` доходят до `descriptor.config.customParameters`.
- **Не проверено.** C++ не компилировался (сборка только в CI); звонок не делался, эффект не замерен. RNNoise: при частоте APM не 48 кГц `AudioCapturePostProcessor` шумодавит копию буфера, которую затем удаляет (так же в групповых звонках апстрима) — работает ли шумоподавление в 1:1 на устройстве, надо проверить. Платформенный NS Android при включённом RNNoise не выключается (как в группах). Экран: поведение `getScreenCaptureSize` при пределе проверено чтением, не на устройстве; уровень H.264 3.1 при захвате выше 720p (`video.md`, «H.264 level») не проверялся.
- **Не сделано в этом этапе.** Ключи `video_fps` и `screen_fps` (сейчас сервер шлёт 30 — как у Android; A/B по частоте на Android не действует); TURN через TCP/TLS (`network.md`: нужен флаг в трёх клиентах или соглашение о порте и решение о проверке сертификата — в пакете TLS нет корня ISRG); переключатель RNNoise для пользователя в окне звонка; разрешение и частота камеры (у Android уже 1280×720 при 30 кадр/с, 1080p ждёт вопроса об уровне H.264); тайм-ауты ICE и 20 с до разрыва.
- **История.** Ветка `task/080-call-parameters` репозитория `pluma-android`, 2026-11-11, не закоммичено; задача 077 (этап 2).

### A-116. Сервисный аккаунт вместо «support» (задача 080)

- **Что и зачем.** Аккаунт Плюмы (@support) сервер отдаёт с `user.support` и без статуса «в сети». Там, где у человека пишется «был(а) в сети», у него написано «сервисный аккаунт» — не «support» Telegram и не «Service notifications» (решение владельца, 2026-11-11). Текст — значение ключа `SupportStatus` на всех десяти встроенных языках; имя ключа не меняется.
- **Где** (`service_account` в `ci/patch.py`).
  - `res/values/strings.xml`: `    <string name="SupportStatus">support</string>` → `service account`.
  - Остальные девять `res/values-*/strings.xml` (`ar`, `de`, `es`, `it`, `ko`, `nl`, `pt-rBR`, `ru`, `uk`): ключа у апстрима нет, строка дописывается перед `</resources>`. Таблица `SERVICE_ACCOUNT_STATUS`: `حساب خدمة`, `Dienstkonto`, `cuenta de servicio`, `account di servizio`, `서비스 계정`, `serviceaccount`, `conta de serviço`, `сервисный аккаунт`, `сервісний акаунт` — со строчной, как `online` и `bot`; немецкое существительное с заглавной.
  - `messenger/LocaleController.java`, начало `formatUserStatus(int currentAccount, TLRPC.User user, boolean[] isOnline, boolean[] madeShorter)`: `if (user != null && user.support) { // pluma: the service account has no last seen` → `return getString(R.string.SupportStatus);`.
- **Где апстрим пишет ключ (13.2.1, `7be672ea`).** Четыре места, все после `MessagesController.isSupportUser(user)`: шапка чата `ui/Components/ChatAvatarContainer.java:1239`, профиль `ui/ProfileActivity.java:11621` и `ui/ProfileActivity2.java:755`, строка поиска `ui/Cells/ProfileSearchCell.java:650`. Они не правятся: меняется только текст. Таблица `SERVICE_ACCOUNT_USES` держит этот список.
- **Апстрим без правки.**
  - В четырёх местах выше — `support`, только по-английски, на любом встроенном языке.
  - Во всех остальных списках (контакты `UserCell`, участники `ManageChatUserCell`, выбор людей `GroupCreateUserCell`, `ContactAddActivity`, `DialogsAdapter` и др. — 13 вызовов `LocaleController.formatUserStatus` вне четырёх файлов выше) аккаунт без статуса подписан «был(а) очень давно» (`ALongTimeAgo`: `user.status == null`). Правка `formatUserStatus` даёт там тот же текст, что в шапке чата. У официального Telegram в этих списках так же «давно»; правка сделана потому, что решение владельца — про всё место статуса (подтвердить).
- **Общий ключ.** `MessagesController.isSupportUser` — это `user.support` и список служебных id Telegram (777000, 333000 и др.). В шапке чата и в обоих профилях 777000, 333000 и 42777 раньше получают `ServiceNotifications`, а в `ProfileSearchCell` ветка `isSupportUser` стоит первой: в строке поиска 777000 тоже подписан «сервисный аккаунт» (у апстрима — `support`). Правка `formatUserStatus` смотрит только на `user.support`.
- **Языковой пакет сервера.** Скачанный пакет главнее встроенных строк (`LocaleController.getStringV2`: сначала `localizationExternal`), поэтому сервер должен отдавать те же слова в ключе `SupportStatus` пакета `android` на всех 37 языках (`pkg/langpack/override.json`: импорт пакета заменяет текст Telegram или добавляет ключ; задача 080). Десять встроенных переводов совпадают с этой таблицей слово в слово. Без этого на языке из пакета вернётся «поддержка» Telegram. Встроенный текст виден до загрузки пакета и на языках без него.
- **Как проверяется.** `verify_service_account`: в каждом `strings.xml` ровно одна строка ключа с текстом таблицы; правка `formatUserStatus` на месте; `R.string.SupportStatus` встречается только в четырёх файлах апстрима и в `LocaleController`. `ServiceAccountTest` (5 тестов). На копии апстрима `7be672ea` патч и `verify` проходят целиком (без шага звонков: в закреплённой копии нет подмодуля tgcalls); порча (перевод, английский текст, условие `user.support`, новое место с ключом) останавливает `verify_service_account`.
- **Если апстрим изменит.** Другой английский текст ключа — падение замены; ключ появился в переводе — `upstream has a string SupportStatus; read its text`; новая папка `values-*` — падение со списком папок (нужен перевод); новое место с `R.string.SupportStatus` — падение со списком файлов: прочитать, подходит ли там «сервисный аккаунт»; изменённая сигнатура `formatUserStatus` — падение замены.
- **Не проверено.** Сборка (Gradle, javac) не запускалась, на устройстве и эмуляторе не смотрелось. Переводы `ar` и `ko` носителем не проверены.
- **История.** Ветка `task/080-service-account` репозитория `pluma-android`, 2026-11-11, не закоммичено; задача 080.

---

## Premium и платные функции

### Баннер дня рождения (A-053…A-059)

Баннер над списком чатов о контактах, у которых сегодня день рождения (`DialogsActivity.updateDialogsHint`, список из `contacts.getBirthdays`). Апстрим показывает его, только пока Premium можно купить, и ведёт к окну подарка Premium. Pluma Premium не продаёт (`help.getAppConfig` отдаёт `premium_purchase_blocked`), поэтому баннер не показывался. Решение владельца (бриф 020, вариант А): показывать с заголовком, аватарами и крестиком Telegram и вести в чат. Все правки — точные замены в `TMessagesProj/src/main/java/org/telegram/ui/DialogsActivity.java` (`BIRTHDAY_EDITS`), по одному вхождению. Коммит `9e6ca71`, 2026-10-31, задача 020. Проверка на эмуляторе — в брифе 020.

Общая проверка (`verify_birthdays`): в ветке баннера (от условия с `dismissedSuggestions.contains("BIRTHDAY_CONTACTS_TODAY")) {` до следующего `} else if (`) нет `premiumPurchaseBlocked`, `GiftSheet`, `UserSelectorBottomSheet`, `BirthdayTodaySingleMessage2`, `BirthdayTodayMultipleMessage2`, `BoostingPremiumChristmasToast`, `loadStarGifts`; в коде ветки без комментариев нет `Gift|Premium|Stars`; ровно два `presentFragment(ChatActivity.of(`. Тесты `BirthdaysTest.test_notice_leads_to_the_chat`, `test_upstream_changed`, `test_check`.

### A-053. Баннер показывается без продажи Premium

- **Где.** Условие ветки: `        } else if (folderId == 0 && communityId == 0 && !getMessagesController().premiumPurchaseBlocked() && BirthdayController.getInstance(currentAccount).contains() && !getMessagesController().dismissedSuggestions.contains("BIRTHDAY_CONTACTS_TODAY")) {\n` → без `!getMessagesController().premiumPurchaseBlocked() && ` и с комментарием `// pluma: no Premium to gift; the notice leads to the chat.`
- **Апстрим и Telegram.** У Telegram Premium продаётся, баннер виден и ведёт к подарку. Web K прячет баннер так же; iOS и Desktop показывают (бриф 020).

### A-054. Один именинник — открывается его чат

- **Где.** `showDialog(new GiftSheet(getContext(), currentAccount, state.today.get(0).id, null, null).setBirthday());` → `presentFragment(ChatActivity.of(state.today.get(0).id));`

### A-055. Несколько — список, выбор открывает чат

- **Где.** `UserSelectorBottomSheet.open(0, state);` → `ItemOptions.makeOptions(this, dialogsHintCell)`, `addChat(user, false, () -> presentFragment(ChatActivity.of(user.id)))` для каждого из `state.today`, `setDrawScrim(false).show()`. Так выбирают собеседника собственные окна апстрима (`ChannelAffiliateProgramsFragment.java:939`, `StarsReactionsSheet.java:603` по брифу 020).

### A-056. Вторая строка — «Send Message»

- **Где.** `LocaleController.formatString(users.size() == 1 ? R.string.BirthdayTodaySingleMessage2 : R.string.BirthdayTodayMultipleMessage2)` → `LocaleController.getString(R.string.SendMessage)`.
- **Ограничение.** Ключ `SendMessage` есть только в английском `values/`; до языкового пакета сервера (задача 021) строка на других языках — по-английски (бриф 020).

### A-057. Закрытие без тоста и без предзагрузки подарков

- **Где.** Блок закрытия: после `removeSuggestion(0, "BIRTHDAY_CONTACTS_TODAY"); updateDialogsHint();` убран `BulletinFactory…createSimpleBulletin(R.raw.gift, …BoostingPremiumChristmasToast…)`, а после `});` — `StarsController.getInstance(currentAccount).loadStarGifts();`.
- **Зачем.** Тост обещает подарить Premium позже в Настройках, где такой строки без продажи Premium нет.

### A-058. Убран импорт окна подарка

- **Где.** `import org.telegram.ui.Gifts.GiftSheet;\n` → пусто. После правок в файле не должно остаться слова `GiftSheet` (иначе падение «GiftSheet is still used»); `StarGiftSheet` — другой класс, остаётся.

### A-059. Ожидаемые тексты баннера

- **Что и зачем.** Проверяется, что английские тексты баннера не предлагают подарок; изменённый текст остановит сборку. Правок нет.
- **Где.** `expect` в `res/values/strings.xml`: `<string name="BirthdayTodaySingleTitle">It’s %s’s **birthday** today! 🎂</string>`, `<string name="BirthdayTodayMultipleTitle_other">%d contacts have **birthdays** today! 🎂</string>`, `<string name="SendMessage">Send Message</string>`.
- **Не патчится.** Чат и профиль именинника подарок не предлагают: апстрим там сам проверяет продажу Premium (`ChatActivityEnterView`, `ProfileActivity`, README).

### Теги «Избранного» без Premium (A-086…A-092)

- **Статус.** Задача 053, решение владельца 2026-11-03. Ветка `task/053-saved-tags` репозитория `pluma-android`, правки в рабочем дереве, не закоммичены (2026-11-04).
- **Что и зачем.** Теги реакций в Saved Messages работают у всех, как у пользователя с Premium в Telegram; Pluma Premium не продаёт. Серверного признака для тегов клиент не читает: каждое место спрашивает `UserConfig.isPremium()`, а `premium_purchase_blocked`, который отдаёт сервер, вообще прячет селектор тегов в меню сообщения (`ChatActivity.java:31909`, `premiumFeaturesBlocked()`, `MessagesController.java:739-741`). `user.premium=true` открыл бы все функции Premium, поэтому в `MessagesController` добавлен `savedTagsAvailable()` (`return true`), и его спрашивают только места тегов. Остальные проверки Premium не тронуты. Исследование — `docs/tasks/053-research/clients.md`, разделы 4.2–4.3; места ниже перечитаны в дереве апстрима и дополнены теми, которых в исследовании не было (8851, 10271, 38579, 44934 в `ChatActivity`, `BulletinFactory`).
- **Как сделано.** Точные замены `SAVED_TAGS_EDITS` в `ci/patch.py` (функция `saved_tags()`, вызывается в `patch()` после `sticker_sets()`), каждая по одному вхождению, перед каждой изменённой строкой комментарий `// pluma: saved message tags without Premium.` Номера строк — апстрима 12.9.1 (`aac1efcb`). Пути от `TMessagesProj/src/main/java/org/telegram/`.
- **Как проверяется.** `verify_saved_tags` (из `verify()`): каждый новый текст есть ровно один раз, старого нет; число вызовов `savedTagsAvailable()` в каждом файле равно `SAVED_TAGS_USES` (MessagesController 1, ChatActivity 11, SearchTagsList 4, ReactionsContainerLayout 3, SharedMediaLayout 1, BulletinFactory 2), так что лишнее место, открывающее что-то ещё, остановит сборку; метод возвращает `true`. Тесты `SavedTagsTest.test_tags_without_premium` (правки применяются, в новых текстах нет `isPremium()`/`premiumFeaturesBlocked`, лимит тегов, соседние проверки Premium не тронуты, повторный запуск падает), `test_upstream_changed` (изменённый или задвоенный якорь — `PatchError` с числом вхождений), `test_check` (откат, удаление или задвоение любой правки, лишний вызов `savedTagsAvailable()`, метод, снова зависящий от Premium). Полный `ci/patch.py` на копии апстрима `aac1efcb` проходит (2026-11-04). Сборка APK и проверка на устройстве не делались.
- **Если апстрим изменит.** Любое изменение якоря — падение с именем файла. Новое место апстрима, где теги закрыты Premium, патч не заметит (см. «Что проверять при обновлении апстрима»).
- **Апстрим и Telegram.** Официальный Telegram Android 12.9.1 (это его код) даёт теги только с Premium; без Premium — шторка Premium, а при `premium_purchase_blocked` селектора тегов в меню сообщения нет.
- **Тексты.** Новых строк нет. Тексты промо тегов (`SavedTagReactionsPremiumHint` и т. п.) после патча недостижимы.

### A-086. `MessagesController.savedTagsAvailable()`

- **Где.** `messenger/MessagesController.java`, после `premiumPurchaseBlocked()` (742-744): метод `public boolean savedTagsAvailable() { return true; }` с комментарием `// pluma: tags in Saved Messages are open to every user; Pluma sells no Premium.` Якорь — три строки `premiumPurchaseBlocked()`, они остаются.

### A-087. `ChatActivity`: 11 мест тегов

- **Где.** `ui/ChatActivity.java`, везде `getUserConfig().isPremium()` → `getMessagesController().savedTagsAvailable()`, кроме 31909:
  - 8851 — подсказка «долгое нажатие на тег» (`savedMessagesTagHint`, текст `SavedTagLongpressHint`) создаётся;
  - 10252 — действие «Тег» в режиме выделения (`tag_message`);
  - 10271 — `showTagSelector()` открывает селектор тегов (без этого 10324 недостижимо);
  - 10324 — нажатие в селекторе тегов выделенных сообщений (иначе шторка Premium);
  - 19217 — пункт «Тег» в панели выделения (`tagItem`);
  - 31899 — подсказка над селектором тегов в меню сообщения: `SavedTagReactionsHint2` вместо `SavedTagReactionsPremiumHint`;
  - 31909 — селектор тегов в меню сообщения: `(!tags || !getMessagesController().premiumFeaturesBlocked())` → `(!tags || getMessagesController().savedTagsAvailable())`; у Premium в апстриме `premiumFeaturesBlocked()` ложно, то есть селектор показывается так же;
  - 32654 — `selectReaction`: поставить или снять тег (иначе шторка Premium);
  - 38579 — `isSupportedTags()`: поиск по тегам (значок, подсказка поля, открытие поиска с тегами; 3941, 4138, 4221, 4227, 8321, 38280, 38550);
  - 44862 — нажатие на тег под сообщением (фильтр; иначе шторка Premium);
  - 44934 — пункт «Назвать тег / Переименовать» в меню тега под сообщением.

### A-088. `SearchTagsList`: фильтр, переименование, баннер

- **Где.** `ui/Components/SearchTagsList.java`, `UserConfig.getInstance(currentAccount).isPremium()` → `MessagesController.getInstance(currentAccount).savedTagsAvailable()`:
  - 272 — нажатие на тег над результатами поиска фильтрует (иначе шторка Premium);
  - 303 и 305 — долгое нажатие открывает переименование;
  - 655 — баннер «закрыто Premium» (`createPremiumLayout`) не показывается.

### A-089. `ReactionsContainerLayout`: селектор тегов

- **Где.** `ui/Components/ReactionsContainerLayout.java`:
  - 1073 — `allReactionsAvailable` для `TYPE_TAGS` = `savedTagsAvailable()`: полный список (`fillRecentReactionsList`, ветка 1350-1409) и кнопка развёртывания (`showCustomEmojiReaction()`, 538-540), как у Premium;
  - 1390 — в список тегов идут недавние реакции и все включённые стандартные (`getRecentReactions`, `getEnabledReactionsList`), как у Premium;
  - 1964 — замок на каждом теге (`isLocked` для `TYPE_TAGS`) снят; для эффектов сообщений (`TYPE_MESSAGE_EFFECTS`) замок по Premium остаётся.
- **Не патчится.** 1119-1122: для `TYPE_TAGS` без Premium `showExpandableReactions = false` — то же значение, что у Premium в апстриме (`allReactionsAvailable && !isPremium() && …` ложно), поэтому окно развёрнутого списка остаётся типа `TYPE_TAGS`. Custom emoji в развёрнутом окне по-прежнему требуют Premium (`ui/Components/Reactions/CustomEmojiReactionsWindow.java:209`, булетин `UnlockPremiumEmojiReaction`), как у обычных реакций.
- **Замечание.** Недавние реакции (1390) могут содержать custom emoji, поставленный в канале; в полосе тегов его можно нажать без Premium — как и в полосе обычных реакций личного чата в апстриме (1391-1397, без фильтра). Отвергать ли такой тег, решает сервер.

### A-090. `SharedMediaLayout`: значок поиска по тегам

- **Где.** `ui/Components/SharedMediaLayout.java:3787`: значок `navbar_search_tag` у поиска Saved Messages в профиле — `profileActivity.getUserConfig().isPremium()` → `profileActivity.getMessagesController().savedTagsAvailable()`.

### A-091. `BulletinFactory`: тег сразу после пересылки в «Избранное»

- **Где.** `ui/Components/BulletinFactory.java`, `UserConfig.getInstance(UserConfig.selectedAccount).isPremium()` → `MessagesController.getInstance(UserConfig.selectedAccount).savedTagsAvailable()`:
  - 1321 — `showForwardedBulletinWithTag` (вызовы `ChatActivity.java:34224`, `39112`);
  - 1361 — `createForwardedBulletin`: при пересылке в Saved Messages булетин с селектором тегов (`Bulletin.LottieLayoutWithReactions`, `TYPE_TAGS`, `ui/Components/Bulletin.java:1739-1811`).

### A-092. Лимит тегов на сообщение — как у Premium

- **Где.** `messenger/MessageObject.java`, `selectReaction` (12341): `int maxReactionsCount = MessagesController.getInstance(currentAccount).getMaxUserReactionsCount();` → `areTags() ? …reactionsUserMaxPremium : …getMaxUserReactionsCount()` с комментарием `// pluma: tags in Saved Messages take the Premium limit.`
- **Что и зачем.** Сообщение «Избранного» (`reactions_as_tags`, `areTags()` 13069-13073; у нового — 12302-12306) берёт столько тегов, сколько с Premium: `reactions_user_max_premium` сервера (3). При достижении лимита апстрим снимает самый старый выбор (12363-12389).
- **Лимит обычных реакций не патчится.** `getMaxUserReactionsCount()` (`MessagesController.java:908-910`) для пользователя без Premium берёт `reactions_user_max_default` сервера (разбор 3706-3715, по умолчанию 1, 1662); сервер отдаёт 3, значит три реакции на сообщение работают без правки. `reactions_uniq_max` клиент не применяет (ошибка разбора, `MessagesController.java:4483-4492`) и берёт 11 по умолчанию (1775) — совпадает с сервером. `getChatReactionsCount()` (912-914) нигде не вызывается.

---

### A-108. Премиум-стикеры без Premium (задача 058)
- **Статус.** Ветка `task/058-premium-stickers` (`54c3da1`), не влита, не выпущена (2026-11-08).
- **Что и зачем.** Премиум-стикеры (`video_thumbs` с `type = "f"`, `MessageObject.java:425`) отправляются, показываются и предлагаются всем, как с Premium в Telegram. Pluma Premium не продаёт, владелец сообщил об ограничении как о дефекте (2026-11-07, подтвердить решение). Серверного признака клиент не читает: места проверяют `UserConfig.isPremium()`, а `premium_purchase_blocked` (мы его отдаём) вырезает такие стикеры из списков. В `MessagesController` добавлен `premiumStickersAvailable()` (`return true`), спрашивают его только места ниже.
- **Где** (строки апстрима 12.9.1, `aac1efcb`; функция `premium_stickers()` в `ci/patch.py`, `PREMIUM_STICKERS_EDITS`, каждая по одному вхождению, перед изменённой строкой `// pluma: premium stickers without Premium.`):
  - `messenger/MessagesController.java:969` — метод и `filterPremiumStickers`: наборы панели, окна и настроек оставляют премиум-стикеры (без правки `premiumFeaturesBlocked()` их убирает);
  - `ui/Components/EmojiView.java:2350` (тап: отправка вместо окна разблокировки) и `:6179` (избранные и недавние не теряют их);
  - `ui/ContentPreviewViewer.java:714` (долгое нажатие: меню с «Отправить» вместо `showUnlockPremiumView()`);
  - `ui/Adapters/MentionsAdapter.java:273, 298` (подсказки стикеров по эмодзи);
  - `ui/ChatActivity.java:25320` (эффект у отправителя: `forcePlayEffect` ставится только с Premium, а `:15985` без него не играет эффект исходящего);
  - `ui/Cells/StickerEmojiCell.java:298-318` и `ui/Cells/StickerCell.java:212-231` (значок: звезда Premium в углу вместо замка посередине), плюс импорт `MessagesController`.
- **Не правится.** Воспроизведение эффекта у получателя (`ChatActivity.java:15984`, `EmojiAnimationsOverlay.java:682-687`; нужны `video_thumbs` и отсутствие `nopremium`) и кастомные эмодзи (`docs/tasks/058-premium-stickers.md`, вопрос владельцу).
- **Как проверяется.** `verify_premium_stickers` (из `verify()`): каждое новое место ровно один раз, старого нет, число вызовов по файлам (`PREMIUM_STICKERS_USES`), метод возвращает `true`. Тесты `PremiumStickersTest` в `ci/patch_test.py`: применение, изменённый якорь — `PatchError`, откат/удаление правки и лишний вызов — падает. Все 58 тестов зелёные; правки и `verify` применены к файлам закреплённого апстрима. Сборка Gradle и проверка на устройстве не делались.

### A-113. Нет ИИ-редактора сообщений (задача 079)
- **Статус.** Ветка `task/079-remove-ai-editor` pluma-android, не закоммичено, не влито (2026-11-11).
- **Что и зачем.** Решение владельца 2026-11-11 (задача 079): у Pluma нет языковой модели, поэтому ИИ-редактор сообщений (переписать, исправить, сменить стиль, перевести набранный текст; `messages.composeMessageWithAI`, `messages.composeRichMessageWithAI`, `aicompose.*`) не предлагается ни в одном клиенте. Сервер их не обслуживает: `messages.composeRichMessageWithAI` отвечает `ErrEnterpriseIsBlocked` (`app/bff/messages/internal/core/messages.composeRichMessageWithAI_handler.go`), а служба `RPCAiComposeTone` (`messages.composeMessageWithAI`, `aicompose.*`) не маршрутизирована в `session.yaml`, и прокси отвечает `ERR_ENTERPRISE_IS_BLOCKED`.
- **Где** (`AI_EDITOR_EDITS`, `no_ai_editor()` в `ci/patch.py`; апстрим 13.2.1, `7be672ea`):
  - `ui/Components/ChatActivityEnterView.java`, `ChatAttachAlert.java` (обе подписи: `aiButton` и `topAiButton`), `CaptionPhotoViewer.java`: в `showAiButton` первая строка `final boolean show = …` → `false`; кнопка создаётся скрытой (`GONE`) и больше не показывается;
  - `ui/iv/RichEditor.java` (полноэкранный редактор) и `ui/iv/RichEditorToolbar.java` (панель вложения «rich»): нижняя кнопка ИИ (`RichAIComposeSheet`) — `setVisibility(View.GONE)` после `bottomPanel.addView(aiButton, …)`; группа `formattingLayout1`, где только кнопка стиля ИИ (`AIEditorAlert` по выделению), — `GONE`, и `onMeasure` панели форматирования не резервирует под неё место.
- **Апстрим без правки.** Кнопка ИИ появляется в поле сообщения и в подписях с третьей строки текста (`showAiButton(lineCount > 2 …)`), кроме секретных чатов; в редакторе rich — всегда. Нажатие открывает `AIEditorAlert` / `RichAIComposeSheet`, запросы уходят на сервер и падают.
- **Как проверяется.** `verify_no_ai_editor`: каждая правка на месте ровно один раз; число мест, открывающих `AIEditorAlert` и `RichAIComposeSheet` (`ChatActivityEnterView` 2, `ChatAttachAlert` 2, `CaptionPhotoViewer` 1, `RichEditor` 1+1, `ChatAttachAlertRichLayout` 1+1), и строк `setVisibility(View.VISIBLE)` у кнопок подписей прежнее; в редакторах rich кнопки не показываются снова. `NoAiEditorTest` в `ci/patch_test.py` (без правки проверка падает; изменённый якорь — `PatchError`; новый вход — ошибка). Патч применён с `verify_no_ai_editor` к копии файлов `7be672ea`.
- **Осталось (сознательно).** Ссылки на чужой стиль (`LinkManager.handleAiStyle`, `aicompose.getTone`): стилей в Pluma создать негде, сервер ответит ошибкой, клиент покажет её. Пункт «ИИ-редактор» на экране Premium не показывается: сервер не включает `ai_compose` в `premium_promo_order` (`teamgramd/bin/app_config.json`), Android убирает функции не из списка (`PremiumPreviewFragment.java:1020-1027`).
- **Не проверено.** Сборка Gradle и устройство (CI после пуша).


## Самообновление (задача 054)

- **Статус.** В ветке `task/054-self-update` репозитория `pluma-android` (2026-11-04, PR #2): правка клиента (`e314edb`) запушена, шаг выпуска в CI (`0f3ff30`) ждёт права `workflow` у токена `gh`. Проверено применением на файлах апстрима `aac1efcb` и тестами `SelfUpdateTest`; компиляция Gradle — сборкой PR; поведение на устройстве — после подписанной сборки.
- **Зачем.** APK с сайта обновляется сам, как APK Telegram с telegram.org: `help.getAppUpdate` → лист обновления и плашка бокового меню → APK по MTProto → системный установщик (исследование `docs/tasks/054-research/clients.md`, §1; ADR 0035).
- **Почему не `isStandalone() = true`.** Он включил бы и прочее standalone-сборки: строку TON в настройках и профиле (`SettingsActivity.java:712`, `ProfileActivity.java:10628`), биллинг по счетам (`BuildVars.java:63`), подпись «direct». Поэтому новый признак — только в местах обновления.

### A-093. `ApplicationLoader.isSelfUpdatingBuild()`

- **Где.** `messenger/ApplicationLoader.java`, после `isStandaloneBuild()` (131-133): `public static boolean isSelfUpdatingBuild() { return true; }` с комментарием `// pluma: …`.

### A-094. Места обновления спрашивают `isSelfUpdatingBuild()`

- **Где.** `isStandaloneBuild()` → `isSelfUpdatingBuild()`:
  - `ui/LaunchActivity.java:5958` — проверка обновлений `checkAppUpdate`, `:6011` — лист `showUpdateAppPopup`;
  - `ui/ChatActivity.java` (`didPressAppUpdateButtonInternal`), `ui/ChannelAdminLogActivity.java` (2, с 13.2.0, два делегата ячейки), `ui/Stories/PeerStoriesView.java` — кнопки «обновить приложение» у неподдерживаемых сообщений и историй (вместо Google Play);
  - `ui/Components/BlockingUpdateView.java:142`, `:303` — блокирующий экран (`can_not_skip`);
  - `messenger/SharedConfig.java:771` — плашка бокового меню (`isAppUpdateAvailable`);
  - `ui/ProfileActivity.java`, `ui/DebugSettingsActivity.java` (с 13.2.0, раньше `SettingsActivity`) — пункт отладочного меню «Check app update».
- **Проверка.** `verify_self_update` считает вызовы в каждом файле (`SELF_UPDATE_USES`): лишнее место — ошибка.

### A-095. `ApplicationLoaderImpl` приложения: установка, лист, плашка

- **Где.** `TMessagesProj_App/src/main/java/org/telegram/messenger/ApplicationLoaderImpl.java` заменяется целиком (сверка с текстом апстрима): переопределения `checkApkInstallPermissions`, `openApkInstall`, `showUpdateAppPopup`, `takeUpdateLayout` из `TMessagesProj_AppStandalone/.../ApplicationLoaderImpl.java:63-114`, без SMS-заданий.

### A-096. Файлы листа и плашки

- **Где.** `UpdateAppAlertDialog.java` (SHA-256 `4545e52e…`) и `UpdateLayout.java` (`e1050c8c…`) копируются из `TMessagesProj_AppStandalone/src/main/java/org/telegram/ui/Components/` в тот же путь `TMessagesProj_App`; изменённый апстримом файл — ошибка. Строки и ресурсы — библиотеки (`org.telegram.messenger.R`).
- `TMessagesProj_App/build.gradle`: `implementation 'androidx.core:core:1.16.0'` (`FileProvider`, `NestedScrollView`): библиотека держит его как `implementation`, модуль standalone тоже объявляет его сам; версия сверяется с библиотекой.

### A-097. Версия `<версия апстрима>.NNNNN` (сейчас `13.2.1.NNNNN`)

- **Где.** `TMessagesProj_App/build.gradle:171` (13.2.1): `versionName APP_VERSION_NAME + ".NNNNN"` (5 цифр с нулями); `:152`: `versionCode = APP_VERSION_CODE * 1000 + N` (`APP_VERSION_CODE` 7291). `gradle.properties` не меняется: `BUILD_VERSION_STRING` библиотеки остаётся версией апстрима, её компиляция не выпадает из кэша Gradle; приложение сообщает и сравнивает версию пакета (`ConnectionsManager.java:223`, `SharedConfig.java:567`, `:791`).
- **Почему 5 цифр.** После перезапуска клиент сохраняет предложение, только пока своя `versionName` меньше как строка (`SharedConfig.java:576`): `12.9.1.99` против `12.9.1.100` сбросило бы его.
- **N** — `--build` у `ci/patch.py` (`PLUMA_BUILD` в `configure.sh`, в CI `github.run_number`; 0 для локальной сборки).
- **Зачем.** Android сравнивает версии по общим частям (`SharedConfig.java:811-824`): без четвёртой части сборки одного апстрима неразличимы. Установки с тремя частями версии обновления не увидят — один раз поставить с сайта.

### A-098. Выпуск на сайт (`ci/publish.sh`, `android.yml`)

- После подписи в job `sign` ветки `main`: `ci/publish.sh signed/<apk> $GITHUB_RUN_NUMBER` → `POST <PLUMA_RELEASES_URL>/dl/upload` (токен — секрет `RELEASES_UPLOAD_TOKEN` окружения `release`, заголовок через файл), версия `<версия апстрима>.NNNNN`, заметки `release/notes.en.txt`, `release/notes.ru.txt`. 409 (сборка уже выложена) — не ошибка. Нет токена или адреса — job падает. Сборка с входом `abis` не публикуется (только подписывается); недельный canary в своей группе concurrency и не вытесняет ожидающий push.

---

## Ссылки pluma.chat (задача 026)

- **Статус.** `9fba686`, влито в `main` (`3380488`, PR #4), выпуск `12.9.1.00031` (2026-11-06), задача 026. Проверено применением `ci/patch.py` на копии апстрима `aac1efcb` (весь `patch()` и `verify()`), выборочными `rg` по результату и тестами `LinksTest` (6 тестов, всего 55, проходят). Пути сайта (A-105, A-106, 2026-11-06): то же применение на копии апстрима, `aapt2 link` манифеста с новым фильтром на `android-35`, компиляция блока Java из A-106 с `android.jar` и проверка `isPlumaSitePath` на 20 путях. Компиляция Gradle и поведение на устройстве не проверены.
- **Зачем.** pluma.chat для Pluma — то же, что t.me для Telegram (решение владельца 2026-10-30; бриф `docs/tasks/026-pluma-links.md`, исследование `docs/tasks/026-research/clients.md`, раздел 1). Сервер отдаёт ссылки на pluma.chat (приглашения, бусты, `me_url_prefix` конфига, из которого приложение берёт `linkPrefix`, `MessagesController.java:5708-5716`). Приложение принимает ссылки pluma.chat как свои, наравне с t.me, telegram.me и telegram.dog, которые остаются. Хост только точный: `x.pluma.chat` — не юзернейм, в отличие от `x.t.me` (`PREFIX_T_ME_PATTERN`, `LaunchActivity.java:264`), потому что web.pluma.chat и подобные — сайты.
- **Код.** `LINK_HOST`, `LINKS_EDITS`, `LINKS_OUT`, `LINKS_KEEP`, `HOSTS_KEEP`, `LINK_STRINGS`, `LINK_STRINGS_KEEP`; функции `links()` и `verify_links()` в `ci/patch.py`. Строки апстрима ниже — в `aac1efcb`.
- **Как проверяется** (`verify_links`, для всех правок A-099…A-106):
  - каждая замена `LINKS_EDITS` есть столько раз, сколько ожидается;
  - литералы `"https://t.me/`, `"http://t.me/` и `"t.me/` (регулярное выражение `"(?:https?://)?t\.me/`) остались во всех `.java` ровно в файлах и количествах `LINKS_KEEP`;
  - строки Java с хостом `"t.me"`, `"telegram.me"` или `"telegram.dog"` без `pluma.chat` — ровно в файлах и количествах `HOSTS_KEEP`;
  - `t.me/` в тексте встроенных строк всех языков — только у ключей `LINK_STRINGS_KEEP`.
  - `LinksTest` собирает синтетическое дерево из ожидаемых текстов: правка проходит, вторая правка падает, пропавший или удвоенный якорь падает, откат любой замены, новая ссылка t.me, новая проверка хоста без pluma.chat и новая строка с t.me ловятся `verify_links`. `verify_links` на чистом апстриме падает.
- **Апстрим до правки.** Только t.me, telegram.me, telegram.dog. Официальный Telegram — так же (для своих доменов).

### A-099. pluma.chat в списках хостов

- **Что и зачем.** Ссылка `https://pluma.chat/…` открывается в приложении, а не в браузере, как `t.me`.
- **Где** (`LINKS_EDITS`, по одному вхождению):
  - `ui/LaunchActivity.java:1969` и `ui/LinkManager.java:1518`: `host.equals("t.me") || host.equals("telegram.dog") || isPrefix` → `… || host.equals("pluma.chat") || isPrefix` — ветка http(s) `handleIntent` (интенты системы и `Browser.openUrl`) и `LinkManager`;
  - `ui/LinkManager.java:107` (`handleHttp`): `!"telegram.dog".equalsIgnoreCase(host) && !isPrefix` → с `!"pluma.chat".equalsIgnoreCase(host)`;
  - `utils/proxy/ProxySettings.java` (`fromUri`, с 13.2.0 вместо `AndroidUtilities`): ссылка на прокси (`/socks`, `/proxy`, `/webproxy`);
  - `messenger/browser/Browser.java:727` (`isInternalUri`): `"telegram.me".equals(host) || "t.me".equals(host)` → с `"pluma.chat".equals(host)`; отсюда каждая нажатая ссылка решает «в приложении или в браузере». Ветка `telegram.dog` (`:712`, блог и FAQ Telegram) не меняется;
  - `ui/Cells/ChatMessageBotButtons.java` (с 13.2.0 вынесено из `ChatMessageCell`): кнопка бота «добавить в группу» (`startgroup`);
  - `ui/web/BotWebViewContainer.java:4089`: встроенный пост (`embed=1`) остаётся в веб-вью мини-приложения;
  - `ui/Business/BusinessLinksController.java:311` (`findLink`): ссылка на свой бизнес-чат `https://pluma.chat/m/<slug>` — такую сделает сервер по `me_url_prefix` (сейчас `account.getBusinessChatLinks` не обслуживается).
- **Не меняются** (`HOSTS_KEEP`): коды подарков `UserSelectorBottomSheet.java:146`, `GiftInfoBottomSheet.java:88`; платежи и подарки Premium/звёзд `PaymentFormActivity.java:1257`, `:2539`, `GiftPremiumBottomSheet.java:304`, `SendGiftSheet.java:835`, `PremiumPreviewFragment.java:1168` — этого в Pluma нет; ветка `telegram.dog` в `Browser.java:712`; `ui/Wallet/WalletActivity.isTelegramHost` (кошелёк закрыт `wallet_available`, сервер его не отдаёт).
- **Если апстрим изменит.** Якорь сдвинулся — падение `replace`. Новая проверка хоста — падение `verify_links` по `HOSTS_KEEP`.

### A-100. Юзернейм из ссылки pluma.chat (`Browser.extractUsername`)

- **Что и зачем.** QR-код профиля Pluma (`https://pluma.chat/<username>` из `linkPrefix`) сканируется в приложении: `QrActivity.java:829`, `NewContactBottomSheet.java:662`; так же вставленная ссылка в `ChatActivity.java:36027` и `PeerStoriesView.java:967`. Без правки `extractUsername` знает только `t.me/`, `http://t.me/`, `https://t.me/` и `x.t.me`.
- **Где.** `messenger/browser/Browser.java:213-215`: после блока `https://t.me/` добавлены `pluma.chat/`, `http://pluma.chat/`, `https://pluma.chat/` (`plumaUsername(link.substring(11))`, `(18)`, `(19)`). `plumaUsername` (A-106) даёт `null` для корня и страниц сайта: QR-код `https://pluma.chat/iphone` показывает «Произошла ошибка», как любой QR не с юзернеймом, а вставленная ссылка идёт дальше как обычная.

### A-101. Регулярные выражения ссылок в чате

- **Что и зачем.** Ссылки на сообщение открытого чата прокручивают к нему, а не открывают чат заново; кнопка «добавить стикеры» у превью набора — как для t.me.
- **Где** (`ui/ChatActivity.java`): `:41391` `t\\.me` → `(?:t\\.me|pluma\\.chat)` (кнопка набора стикеров/эмодзи); `:41920` `publicMsgUrlPattern`, `:41921` `voiceChatUrlPattern`, `:42003` `privateMsgUrlPattern`: `t.me` → `(?:t.me|pluma\\.chat)`. Группа незахватывающая, номера групп те же.
- **Не меняются:** `sponsoredUrlPattern` (`:24798`, реклама Telegram), `t.me/iv` (`:36446`, `ChannelAdminLogActivity.java:3102`, `Browser.java:226`; мгновенный просмотр Telegram), `AccountFrozenAlert.java:52` (бот апелляций Telegram).

### A-102. Вставка ссылки на прокси, перевод упоминания

- **Где.**
  - Снято в 13.2.0: вставка ссылки из буфера идёт через `ProxySettings.fromUri`, там `pluma.chat` добавлен (A-099).
  - `ui/Components/TranslateAlert2.java:863-865`: ссылка-упоминание из перевода `https://pluma.chat/<username>` с текстом `@username` снова становится упоминанием; длина префикса берётся как `entity.url.indexOf('/', 8) + 1` вместо 13. `messages.translateText` сервер пока не обслуживает.

### A-103. Ссылки, которые приложение делает само

- **Что и зачем.** Скопированная, отправленная, показанная или в QR ссылка — pluma.chat. Большинство ссылок апстрим уже строит из `linkPrefix` (около 100 мест: профиль, юзернейм, наборы стикеров, темы, QR профиля); здесь — жёстко прописанные `https://t.me/`.
- **Где** (`LINKS_OUT`, число литералов `"https://t.me/` и `"t.me/` → pluma.chat):
  `utils/proxy/ProxySettings.java` (3, 0) — ссылка на прокси `getLink()`, с web-прокси (`/webproxy`, пути в манифесте A-105); `messenger/voip/VoIPService.java` (1, 0) — `/call/<slug>` `:2241`; `ui/Cells/StickerSetCell.java` (0, 2) — префиксы `addstickers/`, `addemoji/` `:70-71`; `ui/ChatActivity.java` (4, 0) — ссылка на сообщение с `?t=` `:35984`, `:35990`, `:44224`, `:44230`; `ui/ChatEditActivity.java` (0, 1) — публичная ссылка бота `:1627`; `ui/Components/PermanentLinkBottomSheet.java` (1, 0) `:127`; `ui/Components/StickersAlert.java` (0, 1) — префикс поля `:1667`; `ui/FilterChatlistActivity.java` (0, 1) — образец ссылки на папку под спойлером `:967`; `ui/GroupStickersActivity.java` (0, 1) `:1012`; `ui/ManageLinksActivity.java` (5, 0) — публичная ссылка `:837` и заголовок приглашения `:1362-1365` (`https://pluma.chat/+` → `pluma.chat/<hash>`; ссылка t.me падает в ветку `https://`); `ui/PhotoViewer.java` (2, 0) `:1397`, `:1403`; `ui/PrivacyControlActivity.java` (1, 0) — ссылка на номер `+%s` `:2007`; `ui/ProxySettingsActivity.java` (2, 0) `:520`, `:526`; `ui/SessionsActivity.java` (1, 0) — подключённые чат-боты в подтверждении «завершить все сеансы» `:331`; `ui/Stories/ChannelBoostUtilities.java` (2, 0) `:14`, `:16`; `ui/Stories/PeerStoriesView.java` (4, 0) — ссылки на историю `:7103-7114`.
- **По умолчанию `linkPrefix`.** `messenger/MessagesController.java:1585`: `getString("linkPrefix", "t.me")` → `"pluma.chat"` — до первого конфига; потом значение из `me_url_prefix`.
- **Остаются** (`LINKS_KEEP`, число литералов): ссылки, которые приложение само превращает обратно в t.me и открывает у себя — `LaunchActivity.java` 1 (`:1971`, `x.t.me`), `LinkManager.java` 1 (`:111`), `messenger/browser/Browser.java` 4 (`:207-213` распознавание, `:693`), `Components/URLSpanNoUnderline.java` 1 (`@username`), `web/BotWebViewContainer.java` 1 (`openTgLink` мини-приложения, `:1759`), `Components/DialogsBotsAdapter.java` 1; формы t.me, которые приложение по-прежнему распознаёт рядом с pluma.chat — `Business/BusinessLinksController.java` 1, `Components/TranslateAlert2.java` 1; сервисы Telegram, которых в Pluma нет — `PremiumPreviewFragment.java` 2, `ChatEditActivity.java` 4 (BotFather), `Stars/StarsIntroActivity.java` 1, `Stars/ProfileGiftsView.java` 1 (`nft`), `Components/CreateBotAlert.java` 1, `Components/AIEditorAlert.java` 1 (`addstyle`), `Components/Reactions/ChatCustomReactionsEditActivity.java` 1 (@stickers), `Components/Premium/boosts/PremiumPreviewGiftLinkBottomSheet.java` 1, `…/adapters/GiftInfoAdapter.java` 1, `…/cells/LinkCell.java` 3 (`giftcode`), `messenger/MessagesController.java` 3 (`premiumbot`, `spambot` ×2), `messenger/MessageObject.java` 1 (метка `+PublicChat` журнала действий), `ChatActivity.java` 1 и `ChannelAdminLogActivity.java` 1 (`t.me/iv`); закомментированные строки `SecretMediaViewer.java` 2.

### A-104. Текст о публичных ссылках

- **Где.** `res/values/strings.xml`, `GroupDescription3` (пустой экран группы, `ChatBigEmptyView.java:138`): `Public links such as t.me/title` → `Public links such as pluma.chat/title` (`edit_string`, текст целиком). Ключ есть только в `values`; появится в другом языке — падение. Новых ключей нет.
- **Остаются** (`LINK_STRINGS_KEEP`): `TelegramFeaturesUrl`, `CocoonFeature1TextLink`, `CocoonFooter`, `CocoonFooterLink` (сервисы Telegram), `PublicLinksLimitSubtitle` (лист лимитов Premium, `DoubledLimitsBottomSheet.java:289`).

### A-105. Манифест: проверенные app links pluma.chat только для путей ссылок

- **Где.** `TMessagesProj/src/main/AndroidManifest.xml`. Фильтр `LaunchActivity` с t.me (`:257-267`) не меняется. После него новый `<intent-filter android:autoVerify="true">` (VIEW, DEFAULT, BROWSABLE) с комментарием `<!-- pluma: verified app links of pluma.chat, the links and not the site's pages -->`: `host="pluma.chat"` со `scheme` `http` и `https`, `pathPrefix` из `LINK_PATH_PREFIXES` (`/+`, `/joinchat/`, `/addstickers/`, `/addemoji/`, `/addlist/`, `/folder/`, `/c/`, `/s/`, `/m/`, `/call/`, `/boost`, `/proxy`, `/socks`, `/setlanguage/`, `/addtheme/`, `/bg/`, `/login/`, `/contact/`, `/share`) и два `pathAdvancedPattern` из `LINK_USERNAME_PATTERNS`: `/[A-Za-z][A-Za-z0-9_]{4,31}` и `/[A-Za-z][A-Za-z0-9_]{4,31}/.*` (юзернейм 5–32 символа и что угодно после него). Все `<data>` одного фильтра складываются (developer.android.com, `<data>`: «All the `<data>` elements … contribute to the same filter»), поэтому каждый путь действует для обеих схем.
- **Почему pluma.chat нет ни в одном фильтре без пути.** Система проверяет хосты из всех фильтров с VIEW, BROWSABLE, DEFAULT и http(s), а не только из фильтра с `autoVerify` («The system inspects all intent filters…», «For each unique hostname…», developer.android.com, Verify Android App Links), и состояние проверки ведётся по хосту (`pm get-app-links`). Запись `host="pluma.chat"` без пути в общем фильтре t.me, как было в первой версии этой правки, взяла бы весь сайт. Поэтому записи pluma.chat из фильтра t.me убраны (отступление от формулировки «оставить записи непроверенного фильтра»: записи t.me, telegram.me, telegram.dog остались как в апстриме).
- **Почему `pathAdvancedPattern`, а не `pathPattern`.** `minSdkVersion 23`, `targetSdkVersion 36`, `compileSdkVersion 36` (13.2.1, `TMessagesProj/build.gradle:118`, `:143-144`). `pathAdvancedPattern` появился в API 31 (developer.android.com, `<data>`: «`pathSuffix` and `pathAdvancedPattern` were introduced in API level 31»); на Android 11 и ниже атрибут пропускается, и система передаёт приложению только ссылки с префиксами, а ссылку на юзернейм из другого приложения — нет. `pathPattern` (все версии) не знает классов символов: шаблон «5 символов и больше» (`/......*`, `.` совпадает и с `/`) взял бы и `/en/iphone`, `/dl/…`, `/assets/…`. Выбор — точность на Android 12+ ценой ссылок на юзернейм на Android 11 и ниже (решение к подтверждению).
- **Остаток.** Слова сайта из 5+ букв (`iphone`, `android`, `windows`, `linux`, `assets`, `collect`) подходят под шаблон юзернейма. Такую ссылку из другого приложения система отдаст Pluma, и `handleIntent` сразу откроет её в браузере (A-106). Тест `test_username_patterns` фиксирует этот список.
- **Добавлены к списку брифа** `/folder/`, `/m/`, `/call/`: такие ссылки делает само приложение или сервер (`FilterChatlistActivity`, `BusinessLinksController`, `VoIPService`, A-103).
- **`<queries>`.** В `<queries>` манифеста (`:89-95`) после `ton` добавлен `<intent>` VIEW + BROWSABLE + `scheme="https"` с комментарием `<!-- pluma: the browsers that show the pages of pluma.chat (Browser.plumaSiteBrowser) -->`: без него видимость пакетов Android 11+ скрывает браузеры от `queryIntentActivities` в `plumaSiteBrowser` (A-106).
- **Проверено.** `aapt2 link` (build-tools 36.0.0, `android-35.jar`) минимального манифеста с этим фильтром и `<queries>`: атрибуты распознаны (`pathAdvancedPattern` 0x01010620), `aapt2 dump xmltree` показывает все 19 префиксов и 2 шаблона. Совпадения шаблонов проверены в `test_username_patterns` регулярными выражениями Python, которые для этих шаблонов совпадают с синтаксисом advanced glob из документации `PatternMatcher`; на `PatternMatcher` самого Android и на устройстве не проверено.
- **Что нужно вне клиента.** `https://pluma.chat/.well-known/assetlinks.json` с пакетом `chat.pluma.app` (`PLUMA_PACKAGE`) и SHA-256 сертификата подписи релиза (у владельца). На Android 11 и ниже система делает приложение обработчиком по умолчанию, только если `assetlinks.json` подтверждает все хосты манифеста (там же, примечание «On Android 11 … and lower»); t.me, telegram.me и telegram.dog Pluma не подтвердит, поэтому там ссылки идут через выбор приложения. Не проверено на устройстве.
- **Если апстрим изменит.** Якорь (последняя строка t.me фильтра) сдвинулся — падение `replace`. `LinksTest.test_links` проверяет, что pluma.chat есть только в фильтре с `autoVerify`, путями и без `pathPattern`.

### A-106. Страницы сайта pluma.chat открываются в браузере

- **Что и зачем.** Корень `https://pluma.chat/` и пути, первый сегмент которых — страница или файл сайта (`LINK_SITE_PATHS`, без учёта регистра: `iphone`, `ipad`, `android`, `mac`, `windows`, `linux`, `web`, `pero`, `app`, `en`, `dl`, `ios`, `collect`, `assets`, `_l`, `.well-known`, `favicon.svg`, `og.png`, `apple-touch-icon.png`, `404.html`, `progress.json`; один список во всех клиентах), открываются в браузере так же, как любая внешняя ссылка, а не как профиль. Только для хоста pluma.chat; t.me не меняется. Остальные пути pluma.chat — в приложении, как раньше.
- **Где** (`LINKS_EDITS`, по одному вхождению, `messenger/browser/Browser.java`, кроме оговорённых):
  - перед `getBrowserPackageName` вставлен блок `PLUMA_SITE_JAVA`: `PLUMA_SITE_PATHS` (из `LINK_SITE_PATHS`), `isPlumaSitePath(String)` (первый сегмент до `/`, `?`, `#`; пустой — корень), `isPlumaSite(Uri)` (http(s), хост pluma.chat), `plumaUsername` (A-100) и `plumaSiteBrowser(Context)`;
  - `isInternalUri` (`:734`, ветка t.me/telegram.me/pluma.chat): `if (path.equals("iv") || path.startsWith("s/"))` → `… || "pluma.chat".equals(host) && isPlumaSitePath(path))` — страница сайта не внутренняя, `forceBrowser[0] = true`, как `t.me/iv` и `t.me/s/`. С `all` (Linkify, `AndroidUtilities.java:1283`) ссылка остаётся «своей», как у t.me/iv; корень (путь короче 2) выходит из ветки, как у t.me;
  - `openInExternalApp` (`:560`): `|| isPlumaSite(Uri.parse(url))` → `false`. Без этого ветка встроенного браузера (`openUrl`, `:413-438`) вызывает `openInExternalApp` и при `forceBrowser`, а интент с `FLAG_ACTIVITY_REQUIRE_NON_BROWSER` (`:574-579`, Android 11+) разрешился бы в саму Pluma через проверенную ссылку;
  - вкладка Custom Tabs (`:401`, после `builder.build()`): если у интента нет пакета (сессия не привязана, `getSession()` вернул `null`; с сессией пакет ставит `CustomTabsIntent.Builder`, `support/customtabs/CustomTabsIntent.java:298`), а ссылка — страница сайта, пакет = `plumaSiteBrowser`;
  - `openInExternalBrowser` (`:518`): без явного `browser` для страницы сайта пакет = `plumaSiteBrowser`;
  - `ui/LaunchActivity.java:1886` (`handleIntent`, ветка `ACTION_VIEW`, перед `LinkManager`): `Browser.isPlumaSite(data)` → `Browser.openInExternalBrowser(this, data.toString(), false)`, `intent.setAction(null)`, `progress.end()`, `return false` — как ветка `tonsite` (`:1957-1963`). Сюда приходит остаток A-105 (слово сайта из 5+ букв из другого приложения);
  - `ui/LinkManager.java:1503` (`isWebAppLink`): страница сайта — не ссылка мини-приложения (кнопка бота `BotButton.java:219`, меню бота `ChatActivityEnterView.java:4499`).
- **`plumaSiteBrowser`.** Браузер по умолчанию для `https://example.com/` (`resolveActivity`, `MATCH_DEFAULT_ONLY`), иначе первый из `queryIntentActivities`, никогда не сама Pluma; `null`, если браузеров нет (тогда решает система, как в апстриме). Пакет явный, потому что система отдаёт часть адресов сайта Pluma (A-105) и неявный интент мог бы вернуться в приложение.
- **Что показало чтение `Browser.java` (апстрим `aac1efcb`).** `openUrl` (`:291-443`): при `forceBrowser` ветка Custom Tabs пропускает `openInExternalApp` и запускает вкладку; пакет у неё есть только при привязанной сессии (`bindCustomTabsService`, `:89-131`), иначе интент неявный. Ветка встроенного браузера вызывает `openInExternalApp` без учёта `forceBrowser`. `openInExternalBrowser` (`:509-527`) без `browser` шлёт неявный `ACTION_VIEW`. То есть без этой правки возврат в Pluma через проверенную ссылку был возможен в трёх местах; правка закрывает все три. Резолвинг интентов Android по проверенным доменам — по документации, на устройстве не проверено.
- **Как проверяется.** `verify_links` (каждая замена на месте). `LinksTest.test_links`: в Browser.java все имена `LINK_SITE_PATHS`, `plumaUsername` в `extractUsername`; `test_site_paths`: список совпадает с общим списком клиентов, префиксы манифеста не пересекаются с ним; `test_username_patterns`. Блок Java скомпилирован отдельно (`javac` с `android-35.jar`), `isPlumaSitePath`/`plumaUsername` проверены на 20 путях (корень, `/IPhone/x`, `/en/iphone`, `/.well-known/…`, `/progress.json?x=1`, `/durov`, `/+abc`, `/webapp`, `/iphones` и др.).
- **Апстрим до правки.** Нет (у Telegram сайт t.me своими путями не пересекается так; `t.me/iv` и `t.me/s/` — в браузер через `forceBrowser`).

---

## Медиа

### A-060. `updateStickerSets` перечитывает наборы нужного вида

- **Статус.** В `main` с `e859d6b` (коммит `339fd8c`, 2026-11-03, слит через `835c184`).
- **Что и зачем.** `updateStickerSets` говорит, какой вид наборов изменился (`masks`, `emojis`), как и `updateStickerSetsOrder`. Апстрим перечитывает только обычные стикеры, поэтому набор эмодзи, поставленный или снятый на другом устройстве, доходил до этого только после перезапуска. Вид берётся как для `updateStickerSetsOrder`: `masks` → `TYPE_MASK`, `emojis` → `TYPE_EMOJIPACKS`, иначе `TYPE_IMAGE`.
- **Где.** `TMessagesProj/src/main/java/org/telegram/messenger/MessagesController.java`, точная замена двух строк:
  ```java
                          TL_update.TL_updateStickerSets update = (TL_update.TL_updateStickerSets) baseUpdate;
                          getMediaDataController().loadStickers(MediaDataController.TYPE_IMAGE, false, true);
  ```
  на ту же первую строку, комментарий `// pluma: the kind of sets the update is about.` и выбор `type` перед `loadStickers(type, false, true)`.
- **Как проверяется.** `verify_sticker_sets`: в файле есть новый блок целиком. Тесты `StickerSetsTest.test_kind_of_sets`, `test_upstream_changed`.
- **Если апстрим изменит.** Любое изменение двух строк — падение. Если апстрим сам начнёт учитывать флаги, правку снять.
- **Апстрим и Telegram.** Официальный Telegram Android 12.9.1 ведёт себя так же, как апстрим (это его код). Шлёт ли сервер Telegram `updateStickerSets` с `emojis` — не проверено.
- **История.** `339fd8c`, 2026-11-03, задача 030 (раздел «Ограничение Android (клиент)»). Прошла ли сборка CI этой ветки, по репозиторию определить нельзя; тесты скрипта проходят.

### Качество видео (A-079…A-085)

- **Статус.** В `main` с `e859d6b` (коммит `b7accac`, 2026-11-03, задача 052). Значение 85 в A-083 меняется вместе с `videoUploadMaxBitrateMedium` сервера (`app/bff/autodownload`). Устройства, где Pluma уже стоял, сохраняют на мобильном интернете 1080p, пока пользователь сам не выберет пресет автозагрузки.
- **Что и зачем.** Задача 052, решение владельца: при отправке видео выбор качества как в Telegram, но битрейт на каждом разрешении выше. По умолчанию 720p на мобильной сети и 1080p по Wi-Fi: сервер отдаёт `video_upload_maxbitrate` 50/85/100 (`app/bff/autodownload`, `account.getAutoDownloadSettings`). Исследование и таблицы — `docs/tasks/052-research/clients.md`.
- **Как сделано.** Точные замены `VIDEO_QUALITY_EDITS` в `ci/patch.py` (функция `video_quality()`, вызывается в `patch()` перед `birthdays()`), каждая по одному вхождению. Номера строк — апстрима 12.9.1.
- **Как проверяется.** `verify_video_quality` (из `verify()`): каждый новый текст есть ровно один раз, старого нет. Тесты `VideoQualityTest.test_bitrates` (правки применяются, значения потолков, шагов и битрейта GIF; повторный запуск падает), `test_upstream_changed` (изменённый или задвоенный якорь — `PatchError` с числом вхождений), `test_check` (откат, удаление или задвоение любой правки ловит проверка). Полный патч на копии апстрима `aac1efcb` проходит (2026-11-03).
- **Если апстрим изменит.** Любое изменение якоря — падение с именем файла. Перед переносом сверить с этим разделом: правки A-080, A-081, A-083 и A-084 вместе дают умолчание 720p/1080p; одна без других его ломает.
- **Апстрим и Telegram.** Официальный Telegram Android 12.9.1 ведёт себя как апстрим (это его код): потолки 0,75/1,0/2,6/6,8 Мбит/с, по умолчанию 720p и на мобильной сети, и по Wi-Fi.

### A-079. Потолки битрейта `makeVideoBitrate`

- **Где.** `TMessagesProj/src/main/java/org/telegram/messenger/MediaController.java`, `makeVideoBitrate` (6766-6797): весь блок выбора ступени `if (Math.min(height, width) >= 1080) { … } else { … }` (`VIDEO_BITRATE_OLD` → `VIDEO_BITRATE_NEW`).
- **Что.** `maxBitrate` по короткой стороне результата: ≥1080 — 6800_000 → 12000_000, ≥720 — 2600_000 → 5000_000, ≥480 — 1000_000 → 2500_000, меньше — 750_000 → 1500_000. У двух нижних ступеней `compressFactor` 0.75f/0.6f и `minCompressFactor` 0.9f/0.7f → 1f: битрейт исходника больше не урезается. Комментарий `// pluma: higher caps than Telegram's, …`.
- **Почему не раздувает малые файлы.** Конвертер берёт `Math.min(originalBitrate, bitrate)` (`messenger/video/MediaCodecVideoConvertor.java:427-429`); правка этого места не трогает.
- **Апстрим без правки.** 720p — 2,6 Мбит/с, 1080p — 6,8; 480p и ниже — 0,75–0,6 от пересчитанного битрейта исходника.

### A-080. Предел качества — ступень сети

- **Где.** `TMessagesProj/src/main/java/org/telegram/ui/PhotoViewer.java`, `selectCompression` (21689-21705): `        int maxCompression = 2;\n` → комментарий `// pluma: the network's step is the top: …` и `int maxCompression = Math.max(0, Math.min(this.compressionsCount - 1, Math.round(…getMaxVideoBitrate() / (100f / this.compressionsCount)) - 1));`.
- **Что и зачем.** Апстрим ограничивает ступенью 2 (720p) и сохранённый выбор (`compress_video_%d`, `didChangedCompressionLevel` 21233-21243; он пишется при любом движении ползунка и при «Отмене» выбора качества, 6140-6143), и ступень по умолчанию. Теперь предел — ступень сети: для 1080p-исходника на мобильной сети (85) — 720p, по Wi-Fi (100) — 1080p. Выбор, сохранённый по Wi-Fi, на мобильной сети возвращается не выше 720p (бриф 052: 720p на мобильной сети, 1080p по Wi-Fi), меньший выбор сохраняется, как у апстрима.
- **История.** До 2026-11-06 предел был верхней ступенью видео (`this.compressionsCount - 1`): выбор 1080p, сделанный по Wi-Fi, возвращался и на мобильной сети (сообщение владельца 2026-11-06). Исправлено в PR #5.

### A-081. Ступень по умолчанию считается по ступеням видео

- **Где.** Там же, последняя строка `selectCompression` (21704): `return Math.min(maxCompression, … / (100f / compressionsCount)) - 1);` → `return maxCompression; // pluma: the network's step, above` (ступень сети считается в A-080 по ступеням видео).
- **Что и зачем.** Локальная `compressionsCount` после цикла по сохранённым выборам (21697-21703) всегда равна 5, поэтому апстрим делит `video_upload_maxbitrate` на 20: 85 дало бы верхнюю ступень (1080p) и на мобильной сети. После правки делится на число ступеней видео, как в `SendMessagesHelper` (11727): для 1080p-исходника (4 ступени) 85 → ступень 2 (720p), 100 → 3 (1080p), 50 → 1 (480p). У апстрима это незаметно, потому что результат всё равно обрезается до 2 (A-080).

### A-082. Оценка размера беззвучного видео (GIF)

- **Где.** `PhotoViewer.java`, `calculateEstimatedVideoSize` (21296-21298): `                bitrate = 921600;\n            }\n            estimatedSize = (long) (bitrate / 8 * (estimatedDuration / 1000.0f));\n` → `bitrate = 1843200; // pluma: as the convertor gives a muted video`.
- **Зачем.** Оценка совпадает с битрейтом конвертера после A-085.

### A-083. Встроенный пресет «medium»: `video_upload_maxbitrate` 85

- **Где.** `TMessagesProj/src/main/java/org/telegram/messenger/DownloadController.java` (262): `        String defaultMedium = "13_13_13_13_1048576_10485760_1048576_524288_1_1_1_0_100_1";\n` → `…_1_1_1_0_85_1";` с комментарием `// pluma: video_upload_maxbitrate 85, the server's medium preset (720p on mobile data).`
- **Что и зачем.** Пресет мобильной сети новой установки — копия встроенного medium (261-275, `currentMobilePreset = 3`). `loadAutoDownloadConfig` (363-383) переносит в него значения сервера, только если он равен пресету medium, уже обновлённому с сервера (`Preset.equals` сравнивает и `maxVideoBitrate`). Поэтому 85 сервера, отличное от встроенных 100, до мобильного пресета не доходило бы никогда, и на мобильной сети было бы 1080p. Встроенное значение = серверному: мобильный пресет получает 85 и дальше следует серверному medium (остальные поля встроенного medium и серверного совпадают).
- **Установки до 85.** Пресет, уже сохранённый со значением 100, догоняет серверное значение через A-107. Ветка миграции старых установок (308) задаёт 100 литералом; новых установок Pluma она не касается. Число нужно менять вместе с `videoUploadMaxBitrateMedium` сервера.

### A-107. Пресеты следуют за серверными и после их изменения

- **Где.** `DownloadController.java`, `loadAutoDownloadConfig` (360-383): перед `lowPreset.set(res.low)` — копии `oldLow`, `oldMedium`, `oldHigh` и встроенного medium апстрима (`…_100_1`), в цикле сравнение с ними вместо уже обновлённых `lowPreset`/`mediumPreset`/`highPreset`, а с medium — ещё и со встроенным medium апстрима.
- **Что и зачем.** Апстрим сначала ставит пресетам low/medium/high значения сервера, а потом переносит их в мобильный, Wi-Fi и роуминг-пресеты, равные уже новым значениям. Когда сервер меняет значение (у нас 0 → 100 → 85), пресет, который следовал за medium, перестаёт за ним следовать: установки до 85 остались на 100, 1080p на мобильной сети. Пресет, равный встроенному medium апстрима, пользователь не менял, поэтому он тоже следует за medium.
- **Проверка.** `VideoQualityTest` в `ci/patch_test.py`; `video_quality` и `verify_video_quality` на файлах апстрима `aac1efcb`. Сборка Gradle и устройство — CI и владелец.
- **История.** PR #5, 2026-11-06.

### A-084. Отправка без редактора: шаги редактора и 1080p

- **Где.** `TMessagesProj/src/main/java/org/telegram/messenger/SendMessagesHelper.java`, `createCompressionSettings` (11679-11777), две замены:
  - условие (11733-11734): `if (selectedCompression != compressionsCount || Math.max(videoEditedInfo.originalWidth, videoEditedInfo.originalHeight) > 1280) {` → `… > 1920) {` с комментарием `// pluma: the top step keeps up to 1920 px, as in the editor.`;
  - `switch (selectedCompression)` (11736-11749): длинная сторона 432/640/848/1280 → 480/854/1280/1920 (`VIDEO_STEPS_OLD` → `VIDEO_STEPS_NEW`, комментарий `// pluma: the sizes of the editor's steps (PhotoViewer.calculateResultVideoSize).`).
- **Что и зачем.** Путь без редактора — видео, отправленное из меню вложений без открытия, или «Поделиться» из другого приложения (вызов при `info.videoEditedInfo == null`, 10943). Шаг — `round(v / (100 / steps))`, с единицы (11727-11731). Апстрим уменьшал любой исходник больше 1280 px до 1280, а его шаги на одну ступень ниже шагов редактора (`PhotoViewer.calculateResultVideoSize` 21403-21438: 480/854/1280/1920): с 85 мобильная сеть получила бы 848 px (480p), хуже Telegram. Теперь 85 → 1280 (720p), 100 → исходник до 1920 px без уменьшения, как в редакторе.
- **Апстрим без правки.** Telegram (100 во всех пресетах) отправляет так 1080p-исходник в 720p.

### A-085. Битрейт беззвучного видео (GIF) вдвое выше

- **Где.** `TMessagesProj/src/main/java/org/telegram/messenger/video/MediaCodecVideoConvertor.java` (424-429): `                            } else if (bitrate <= 0) {\n                                bitrate = 921600;\n                            }\n                            if (originalBitrate > 0) {\n` → `bitrate = 1843200; // pluma: twice Telegram's, for a muted video (GIF)`; следующая строка `Math.min(originalBitrate, bitrate)` в якоре и не меняется.
- **Что и зачем.** Беззвучное видео уходит как GIF без своего битрейта (-1, `PhotoViewer.java` 9818-9820), конвертер давал ему 921 600 бит/с (0,92 Мбит/с), теперь 1,84 Мбит/с, не выше битрейта исходника.

---

## Уведомления и пуши

### A-061. Firebase-конфиг приложения

- **Что и зачем.** Пуши через FCM проекта Firebase Pluma (задача 009). Без конфига — заглушка, сборка проходит, пушей нет.
- **Где.** Записывается файл `TMessagesProj_App/google-services.json` (заменяет файл апстрима):
  - если задан `--google-services` (переменная `PLUMA_GOOGLE_SERVICES_JSON` в CI) — этот конфиг; в нём обязан быть клиент `chat.pluma.app`;
  - иначе `placeholder_google_services(["chat.pluma.app", "chat.pluma.app.beta"])`: правильный по форме конфиг несуществующего проекта `pluma-placeholder`, номер `100000000000`, ключи выведены из SHA-256 имени пакета.
- **Как проверяется.** `GoogleServicesTest.test_placeholder_is_well_formed`; отказ при конфиге без нужного клиента — в `patch()`.
- **Если апстрим изменит.** Перенос модуля приложения или плагина `google-services` сломает сборку Gradle, а не патч.
- **Апстрим до правки.** Конфиг Firebase Telegram.
- **Расхождение с брифом 009.** Бриф говорит о секрете GitHub Actions; workflow берёт переменную `vars.PLUMA_GOOGLE_SERVICES_JSON` (не секрет; README: значение есть в каждом APK).
- **История.** `991d01b`, 2026-10-23, задачи 006 и 009.

### A-062. Заглушка Firebase для модуля библиотеки

- **Где.** Записывается `TMessagesProj/google-services.json` = `placeholder_google_services(["org.telegram.messenger"])`: модуль библиотеки сохраняет своё пространство имён.
- **История.** `991d01b`, 2026-10-23, задача 006.

Связанное: цвет уведомлений — A-049, значок — A-026. Пуш-сессия Android (тип токена 7) решена на сервере без правки клиента (бриф 020, ADR 0024).

---

## Сборка и подпись

### A-063. Только нужные модули

- **Где.** `settings.gradle`, точные замены `include ':TMessagesProj_AppHuawei'\n`, `include ':TMessagesProj_AppHockeyApp'\n`, `include ':TMessagesProj_AppStandalone'\n`, `include ':TMessagesProj_AppTests'\n` → пусто.
- **Если апстрим изменит.** Переименование или удаление модуля — падение; новый модуль будет собираться.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-064. Без репозитория и плагина Huawei

- **Где.** `build.gradle`: точная замена `        maven { url 'https://developer.huawei.com/repo/' }\n` → пусто; регулярное выражение `        classpath 'com\.huawei\.agconnect:agcp:[^']+'\n` → пусто.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-065. ABI APK

- **Что и зачем.** `arm64-v8a` (`PLUMA_ABIS` в `config/app.env`), для эмулятора добавляется `x86_64` (вход `abis` ручного запуска).
- **Где.** `TMessagesProj_App/build.gradle`, регулярное выражение `(        afat \{\n            ndk \{\n                abiFilters )[^\n]+` → тот же префикс и список ABI. `Config` допускает `armeabi-v7a`, `arm64-v8a`, `x86`, `x86_64` без повторов.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-066. ccache для нативного кода

- **Что и зачем.** Launcher задан в аргументах CMake модуля, который собирает нативный код. Только с `--ccache` (CI).
- **Где.** `TMessagesProj/build.gradle`, точная замена `arguments '-DANDROID_STL=c++_static', '-DANDROID_PLATFORM=android-23'` → с добавлением `'-DCMAKE_C_COMPILER_LAUNCHER=ccache', '-DCMAKE_CXX_COMPILER_LAUNCHER=ccache'`.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-067. Закрепление апстрима и его загрузка

- **Где.** `upstream.env` (см. «Общее»); `ci/fetch-upstream.sh`: `git init`, `fetch --depth 1 origin $UPSTREAM_REF` с таймаутом 10 мин и тремя попытками, `checkout FETCH_HEAD`, сверка `rev-parse HEAD`; затем `submodule update --init --recursive --depth 1 --jobs 4` (40 мин, три попытки) и сверка `submodule status` (с 13.2.1 у апстрима 19 субмодулей). `MEDIA_BUILD_TOOLS=35.0.0` в `upstream.env` и `ci/setup.sh`: модули media не называют build-tools и берут умолчание AGP 8.13.
- **История.** `991d01b`, 2026-10-23, задача 006; `5de5e96`, `7eb8d34`, 2026-11-09, задача 074.

### A-068. Цели сборки: `configure.sh`

- **Что и зачем.**
  - Нет серверных переменных — dev-сборка на сервер `config/dev` (эмулятор, `10.0.2.2:10443`, публичный dev-ключ `config/dev/server_public.pem`); не подписывается.
  - Заданы `SERVER_IPV4`, `SERVER_PORT`, `RSA_PUBLIC_KEY` — цель `server`; если задана только часть — ошибка (половина конфигурации дала бы клиент, который не входит).
  - `DEV_SERVER_IPV4` (вход `dev_address`) — dev-сборка на стенд по этому IPv4 и порту 10443 с dev-ключом, что бы ни говорили серверные переменные (Wi-Fi-адрес машины стенда или `127.0.0.1` с `adb reverse`).
  - Печатает `target=dev|server` в `$GITHUB_OUTPUT`.
- **Где.** `ci/configure.sh`; `config/dev/server.env` (`PLUMA_SERVER_IPV4=10.0.2.2`, `PLUMA_SERVER_PORT=10443`).
- **История.** `991d01b` 2026-10-23; `6a1221d` 2026-10-29 (`DEV_SERVER_IPV4`); задача 006, `docs/ops/clients.md`.

### A-069. Идентичность в `config/app.env`

- **Где.** `PLUMA_PACKAGE=chat.pluma.app`, `PLUMA_APP_NAME=Pluma`, `PLUMA_API_ID=10001` (10002 iOS, 10003 Desktop, 10004 Web), `PLUMA_API_HASH=…`, `PLUMA_ABIS=arm64-v8a`. Используется в A-006, A-007, A-008, A-010, A-065.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-070. Сборка APK: `build.sh`

- **Где.** `ci/build.sh dev|server`: `./gradlew --build-cache --stacktrace -Dorg.gradle.jvmargs="-Xmx6g …" :TMessagesProj_App:assembleAfatRelease`; копия `TMessagesProj_App/build/outputs/apk/afat/release/app.apk` в `out/pluma-android-<версия>-<цель>.apk` и её SHA-256. APK подписан публичной тестовой подписью апстрима.
- **Если апстрим изменит.** Переименование задачи, flavor `afat` или пути выхода — падение сборки.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-071. Подпись: `sign.sh`

- **Где.** `ci/sign.sh in out`: `zipalign -c 4`, `apksigner sign` ключом выпуска (`PLUMA_KEYSTORE`, `PLUMA_KEYSTORE_PASSWORD`, `PLUMA_KEY_ALIAS`, `PLUMA_KEY_PASSWORD`), `apksigner verify`, сверка SHA-256 сертификата с `PLUMA_SIGNING_CERT_SHA256`, если задан. Ключ выпуска — в секретах окружения `release` (`PLUMA_KEYSTORE_B64` и др.), в репозитории только имена; `*.jks` и `*.keystore` в `.gitignore`.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-072. Шифрование APK: `encrypt.sh`

- **Что и зачем.** Артефакты публичного репозитория читает кто угодно, поэтому сборка под настоящий сервер покидает job только зашифрованной age-ключом владельца (`PLUMA_AGE_RECIPIENT`, ADR 0010); dev-сборки остаются открытыми.
- **Где.** `ci/encrypt.sh <dir>`: каждый `*.apk` → `*.apk.age`, открытый удаляется, `.sha256` остаётся.
- **История.** `dd9136d`, 2026-10-23, задача 006.

### A-073. Установка инструментов: `setup.sh`

- **Где.** `ci/setup.sh build` — `ccache`, `age`, SDK `platforms;$PLATFORM`, `build-tools;$BUILD_TOOLS`, `build-tools;$MEDIA_BUILD_TOOLS`, `ndk;$NDK_VERSION`, `cmake;$CMAKE_PACKAGE` (из `upstream.env`: `android-36`, `36.0.0`, `35.0.0`, `27.2.12479018`, `3.22.1`); `CCACHE_COMPILERCHECK=string:<NDK>`. `ci/setup.sh sign` — `age` и build-tools. Повторы и таймауты.
- **История.** `991d01b` 2026-10-23; `dd9136d` (age, переработка); задача 006.

### A-074. Workflow `android.yml`

- **Где.** `.github/workflows/android.yml`:
  - запуск: push в `main`, pull request, еженедельная «канарейка» (`23 4 * * 1`, закреплённый апстрим на свежем образе раннера), ручной запуск с входами `abis` и `dev_address`;
  - job `check`: `python3 -I ci/patch_test.py -v`, `shellcheck -x ci/*.sh`;
  - job `build`: освобождение диска, загрузка, патч, preflight (`PLUMA_AGE_RECIPIENT` для сборки под сервер), JDK 17, `setup.sh build`, ccache в `actions/cache` (ключ по `upstream.env`, сохраняется и при падении, кроме PR), Gradle build cache, `build.sh`, шифрование, артефакт `apk-build` на 3 дня;
  - job `sign`: только push/ручной запуск и цель `server`, окружение `release` с обязательным подтверждением; расшифровка, подпись, повторное шифрование, артефакт `pluma-android` на 30 дней;
  - переменные репозитория: `PLUMA_SERVER_IPV4`, `PLUMA_SERVER_PORT`, `PLUMA_SERVER_IPV6`, `PLUMA_RSA_PUBLIC_KEY`, `PLUMA_GOOGLE_SERVICES_JSON`, `PLUMA_SIGNING_CERT_SHA256`, `PLUMA_AGE_RECIPIENT`; секреты окружения `release`: `PLUMA_KEYSTORE_B64`, `PLUMA_KEYSTORE_PASSWORD`, `PLUMA_KEY_ALIAS`, `PLUMA_KEY_PASSWORD`, `PLUMA_AGE_IDENTITY` (только имена).
  - действия закреплены по SHA коммита.
- **История.** `991d01b` 2026-10-23; `dd9136d` (шифрование, preflight); `6a1221d` (`dev_address`); задача 006.

### A-075. Dependabot

- **Где.** `.github/dependabot.yml`: еженедельные PR для действий GitHub (одна группа). Апстрим, NDK и CMake обновляются вручную вместе с `ci/patch.py`.
- **Не слито.** Ветка Dependabot `origin/dependabot/github_actions/github-actions-3bc6d10805` (`69205f7`, 2026-11-02) поднимает `gradle/actions/setup-gradle` в `android.yml` с v6.4.0 (`3f5f9ad…`) до v6.4.1 (`8da46b5…`); в `main` её нет.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-076. Инструменты бренда

- **Где.** `brand/render.py` (рендер всех файлов `brand/android`), `brand/mark_write.py` (анимация A-035), `brand/svg/*` (комплект владельца), `brand/lottie/pluma-devices.json` (A-034). После изменения SVG: `python3 brand/render.py`, затем `python3 ci/patch_test.py`; при изменении добавленных анимаций — обновить SHA-256 в `BRAND_ADDED` (для знака — и `MARK_WRITE_SHA256` в `ci/patch_test.py`).
- **История.** `a946650` 2026-10-29; `6c01b5f` (`calls_log_logo.json`); `d7cae7e` (копия ноутбука); `d9ad7cf` (`mark_write.py`, пустой самолёт); задачи 017, 020.

---

### A-109. Premium для всех: звезда, строки покупки (задача 071)
- **Статус.** Влито в main форка (PR #7, `7d0df1b`). Правка кнопки расшифровки снята в ветке `task/080-service-account` (2026-11-12): не закоммичено, не выпущено.
- **Что и зачем.** Сервер ставит флаг `premium` всем (ADR 0041). Ненужное от этого убираем, функции флаг открывает сам.
- **Где** (`PREMIUM_FOR_EVERYONE_EDITS` в `ci/patch.py`):
  - `messenger/MessagesController.isPremiumUser` возвращает `false`: звезды в `DialogCell`, `UserCell`, `ProfileSearchCell`, `DialogStoriesCell`, `DialogsActivity`, заголовке `ChatActivity`, `ProfileActivity`;
  - `ImageReceiver`, `ImageLocation` читают `user.premium`, анимация аватарок остаётся;
  - звезды прямых чтений: `SettingsActivity`, `ChatActivity:19346`, `ChatMessageCell`, `GroupCallUserCell`, `StatusBadgeComponent`, `TableView`;
  - строки Premium и Business: `SettingsActivity`, `ProfileActivity` — по `premiumPurchaseBlocked`.
- **Расшифровка голосовых и кружков.** Кнопка снова апстримовская (решение владельца 2026-11-12, задача 080): сервер выполняет `messages.transcribeAudio` для всех с Premium. Правка `false && …` в `useTranscribeButton` (`ChatMessageCell.updateWaveform`) убрана. На сообщениях «на один раз» апстрим кнопку не показывает сам: условие требует `media.ttl_seconds == 0` (`ui/Cells/ChatMessageCell.java:11172`, апстрим `7be672ea`), а у голосового или кружка «на один раз» `ttl_seconds` = `0x7FFFFFFF` (`messenger/MessageObject.java:11524-11530`). Плеер `ui/SecretVoicePlayer.java:316,340,519` рисует тот же `MessageObject` в `ChatMessageCell`. Запрос уходит только по нажатию этой кнопки (`ui/Components/TranscribeButton.java:204-242,690`; касание `ChatMessageCell.java:2836`, доступность `ChatMessageAccessibilityNodeProvider.java:435,1006` — оба закрыты `useTranscribeButton`). Сервер такие медиа расшифровывать отказывается.
- **Как проверяется.** `verify_premium_for_everyone`, `PremiumForEveryoneTest`. `verify_transcribe_button`: условие кнопки ровно одно, с `isPremium()` и проверкой `ttl_seconds` в том же операторе, без `false &&`; тест — `TranscribeButtonTest`.
- **При обновлении апстрима.** Новые места со звездой — по `user.premium` и `msg_premium_liststar`. Изменённое условие `useTranscribeButton` останавливает сборку в `verify_transcribe_button`: перечитать, скрыта ли кнопка на сообщениях «на один раз». Новый вызов `TL_messages_transcribeAudio` вне `TranscribeButton` тоже нужно перечитать.

### A-110. Бустов нет: свои реакции и цвета без уровня, входы и ссылки убраны (задача 071)
- **Статус.** Ветка `task/071-premium-for-everyone` (`b39e60b`, `f791823`), не влита, не выпущена (2026-11-09).
- **Что и зачем.** Канал не должен показывать уровень; `premium.getBoostsStatus` отвечает уровнем 0. Ничего не ждёт уровня, бустов как сущности нет.
- **Где** (`PREMIUM_FOR_EVERYONE_EDITS` в `ci/patch.py`):
  - `ChatCustomReactionsEditActivity`: три сравнения с уровнем (кнопка, выход, подсказка), лимит — `maxReactionsCount`;
  - `MessagesController.PeerColor.getLvl` → 0 (цвета из `help.getPeerColors` с `channel_min_level`);
  - скрыто: «Статистика и бусты» в `ChatEditActivity`, вкладка бустов `StatisticActivity`, пункт меню `ChatActivity` и `TopicsFragment`, число бустов в превью `ChannelColorActivity`, «Не ограничивать бустеров» `ChatUsersActivity`;
  - `ProfileActivity`: пункт «Статистика» только при настоящей статистике; для владельца без неё он открывал экран бустов (условие `canPostStories`, `StatisticActivity.create`);
  - `LaunchActivity`: три входа ссылки `boost` не открывают окно буста, ссылка открывает канал.
  - Не правилось: `ChatObject.isBoostSupported` (им же закрыты истории каналов, `StoriesController.canPostStories`); остальные ворота сравнивают уровень с `*_level_min` = 0; `boosts_unrestrict` и `BOOSTS_REQUIRED` сервер не отдаёт.
- **Как проверяется.** `verify_premium_for_everyone`, `PremiumForEveryoneTest`; якоря — на `aac1efcb`. Компиляция и экран на устройстве не проверены.

### A-111. Обновление до 13.2.1 (слой 233): новые места (задача 074)

- **Статус.** Ветка `task/074-upstream` (`5de5e96`…`bf8ac2a`), не влита, не выпущена (2026-11-09).
- **Что и зачем.** Апстрим 12.9.1 → 13.2.1 (`7be672ea`). Сервер отдаёт 228 и 233 одновременно (ADR 0045). Перенесены сломанные якоря A-001, A-005, A-011, A-014, A-016, A-030, A-034, A-066, A-094, A-099, A-102, A-103 (изменения в их записях), добавлены места, которых раньше не было:
  - `utils/proxy/ProxySettings.java` (новый класс 13.2.0): `pluma.chat` в проверке хоста, `getLink()` на `pluma.chat`, схема `plm:` (A-016, A-099, A-103); путь `/webproxy` в манифесте (`LINK_PATH_PREFIXES`, A-105);
  - `ui/Cells/ChatMessageBotButtons.java`, `ui/QrShare/QrScanOverlay.java`, `messenger/NotificationsController.java`: схема `plm:` (A-016), `ChatMessageBotButtons` ещё и хост `pluma.chat` (A-099);
  - `ui/LinkManager.java`: регулярное выражение `sendgrams` на `plm:` (`SCHEME_REGEX`);
  - `ui/ChannelAdminLogActivity.java`: кнопки обновления спрашивают `isSelfUpdatingBuild()` (A-094);
  - `ui/SessionsActivity.createLocalSession`: `"Telegram Android"` → `"Pluma Android"` (A-014);
  - TON Connect (`tc://`, новый путь 13.x, `NOT_SERVED_EDITS`): `LinkManager.handleTc` спрашивает `walletAvailable`, как `handleTon` и `handleSendGrams`, иначе ссылка оставляла спиннер в ожидании кошелька, которого нет (`GramWallet.whenReady`); фильтр `tc` с `priority="1"` убран из манифеста, чтобы Pluma не забирала чужие ссылки TON Connect;
  - экран QR `ui/QrShare/QrShareView.canBuyGifts` (новый экран 13.2.0, `PREMIUM_FOR_EVERYONE_EDITS`): при `premiumPurchaseBlocked()` пустой слой подарков не предлагает магазин (`Browse Gifts` → `GiftSheet`) и говорит только, что подарков нет; собственные подарки в слое остаются. Что показал бы `GiftSheet` на сервере Pluma, не проверялось: вход закрыт.
- **Как проверяется.** `verify_links`, `verify_brand` (и новые `JAVA_NAMES_KEEP`, `TELEGRAM_SITE_KEEP`, `SCHEME_REGEX`), `verify_self_update`; `SchemeTest`; порча таблиц на настоящем апстриме (убранная правка `ProxySettings`, `QrScanOverlay`, `ChannelAdminLog`, `SessionsActivity`, `SCHEME_REGEX`, `LINKS_OUT`) останавливает `verify`.
- **Осмотрено, правок не потребовало.**
  - Запись кружков (`InstantCameraView2`, `InstantCameraViewBase`, `utils/recorder/round/*`, `TelegramRoundVideoUpload`, `RoundVideoSettingsActivity`): нет хостов Telegram; настройки только локальные (`SharedSettings`), единственный внешний выключатель — ключ `android_round_video_recorder_v2_disabled` (`InstantCameraViewBase.isCamera2Enabled`, умолчание false, `AppGlobalConfig.java:109`), сервер его не отдаёт, значит новый рекордер включён. Ключ `round_video_encoding` новый рекордер не читает: умолчания 480p и 1,2 Мбит/с (`SharedSettings.java:19-37`), правки качества A-079…A-085 его не касаются.
  - Вход Firebase PNV: закрыт `BuildVars.SUPPORTS_FPNV` и `messagesController.fpnvAllowed` (`help.getConfig`, флаг 17, сервер не ставит): запрос не уходит.
  - Web-прокси (`utils/proxy/WebProxyTransport`): только адрес, заданный пользователем, своих хостов нет.
  - Кошелёк (`ui/Wallet/*`): закрыт `wallet_available` (умолчание `DEBUG_VERSION`), сервер его не отдаёт; с `handleTc` (выше) проверка стоит на всех трёх входах ссылками.
- **Сборка.** Настоящая сборка как в CI (`ci/fetch-upstream.sh`, `ci/patch.py --ccache` с подставными значениями, `ci/build.sh dev` в образе из `Dockerfile` апстрима): APK 40 МБ, arm64-v8a, `13.2.1.00000`, подпись тестовая апстрима. Javac нашёл то, чего не видит `verify`: конструктор `RLottieDrawable` без имени кэша (правка интро, якорь исправлен).
- **Не проверено.** Работа на устройстве; соединение реального клиента 233 с сервером; сборка на раннере GitHub. Настоящая сборка выше сделана до правок A-014 (ссылка приглашения), A-019, A-111 (`tc`, QR), A-112: они проверены применением патча с `verify` на копии апстрима `7be672ea`, порчей и `patch_test.py`; изменённые `.java` разобраны `javac` без синтаксических ошибок, манифест разбирается как XML. Gradle и устройство после них не запускались.
- **Осталось (не правилось).** `handleSettings`: ссылки `settings/stars…`, `settings/send-gift`, `settings/ton` по-прежнему открывают свои экраны; фильтр манифеста `ton://transfer` остался (ссылка закрыта `walletAvailable` в `handleTon`, но Pluma предлагается как обработчик).

## Групповые звонки (задача 094)

Задача 093 (первый этап групповых звонков) клиент не меняла: патчи клиентов в неё не входили. Все правки ниже — задача 094 и ADR 0060 серверного репозитория: каждый групповой звонок сквозной, другого сервер не обслуживает. Ветка `task/094-group-calls-e2e` репозитория `pluma-android`, три коммита: `ad2fa92` (2026-11-12: сквозной звонок, без «войти от имени», RTMP и живых историй, первый отчёт), `ac9d6c0` (2026-11-12: отчёт `PlumaE2EStatus` v1 и одно правило самовосстановления, без пункта записи, цвета звонков), `f6fcdfd` (2026-11-13: повторы входа по правилу ADR 0060 §5 от 2026-11-13). Таблицы правок — `GROUP_CALLS_E2E_EDITS`, `CALL_COLOUR_EDITS`, `VOIPGROUP_COLOURS` в `ci/patch.py`; описание — README форка, «Group calls end to end» и «Call colours».

Общее «не проверено» для A-117…A-122: Gradle и C++ (tgcalls, JNI) не собирались — нативная сборка только на CachyOS или в CI; на устройстве и эмуляторе не смотрелось, живой звонок с SFU и другими клиентами не делался.

### A-117. Каждый групповой звонок сквозной: звонок чата и канала идёт крипто-путём конференции (задача 094)

- **Статус.** Ветка `task/094-group-calls-e2e`, не влито, не выпущено (2026-11-13).
- **Что и зачем.** ADR 0060: сервер обслуживает только сквозные групповые звонки и отклоняет вход без ключа. Апстрим шифрует лишь конференцию (`VoIPService.isConference()` = `joinConference || conference`, `VoIPService.java:308`): звонок чата входит без ключа, tgcalls без шифрования кадров (`makeGroup(..., conference != null)`, `:3165`). Теперь звонок группы или канала получает объект tde2e конференции (`ConferenceCall`) и сохраняет интерфейс и права звонка чата.
- **Где** (`GROUP_CALLS_E2E_EDITS`, апстрим `7be672ea`):
  - `messenger/voip/VoIPService.java`: `isConference()` — признак интерфейса (`conference != null && chat == null`), `conference != null` — признак шифрования. Обновления звонка чата применяет `MessagesController` (`MessagesController.java:20502-20531`), поэтому `VoIPService` применяет их сам только в конференции (`:1657, 1724`).
  - `startGroupCall`: объект звонка создаётся до инстанса tgcalls; вход читает последний блок, строит на нём блок добавления и шлёт `public_key`/`block` (`flags 8`); блоки ответа уходят в объект, затем `joined()`, опрос цепочки, `setConferenceCallId`. Повторный вход инстанса (`!first[0]`) сбрасывает объект — новый ключ. Инстанс без объекта звонка не входит (выход).
  - `messenger/voip/NativeInstance.java` и JNI `makeGroupNativeInstance(..., e2e, conference)`: tgcalls шифрует кадры любого группового звонка (включает шифрование по наличию `e2eEncryptDecrypt`, `GroupInstanceCustomImpl.cpp:1386, 1801, 2012`); три слоя видео у звонка чата, один — только у конференции (`:2966-2971`), как в апстриме.
  - `ui/GroupCallActivity.java`: строка эмодзи ключа по `isE2E()` (`:4933, 8625`), защита `conferenceEmojiUpdated` от пустого drawable (`:1451`).
  - `messenger/ChatObject.java`: сквозной звонок грузит участников сразу, до `conference_call_size_limit` (`:574`); список «в цепочке, но не в звонке» — для любого сквозного звонка (`:1519`); `ConferenceCall.updateParticipants` удаляет ушедших (`only_left`) только когда известен весь список (`membersLoadEndReached`).
  - Сообщения в звонке не тронуты: сервер их не обслуживает.
- **Повторы входа (`CONF_WRITE_CHAIN_INVALID`, правило ADR 0060 §5 от 2026-11-13).** Высоту занял другой блок: последний блок читается заново, блок входа строится на нём и вход повторяется, как tdesktop `GroupCall::joinFail` (`calls_group_call.cpp:1858-1866`). К пределу 10 подряд (`PLUMA_CHAIN_STALLED_RETRIES`) идёт только отказ, после которого прочитанный последний блок тот же, на котором был построен отвергнутый (или сам отвергнутый блок входа): цепочка стоит, это не гонка. Пока цепочка движется — например, все входят заново после перезапуска SFU и за круг проходит один вход, — вход продолжается, но не больше 100 попыток на вход (`PLUMA_CHAIN_JOIN_ATTEMPTS`, `conference_call_size_limit`). Дальше — сообщение об ошибке и выход (`plumaChainJoinBase()`, `plumaChainRetry()`, `plumaChainJoinReset()`). Повторы держат ключ и в предел самовосстановления (A-121) не идут. Так же в конференции: апстрим здесь завершал звонок (`stopSelf()`, `VoIPService.java:2711`). До 2026-11-13 предел был 10 подряд при любом движении цепочки (`ac9d6c0`).
- **Апстрим без правки.** Звонки групп и каналов без шифрования, сквозная только конференция; на `CONF_WRITE_CHAIN_INVALID` конференция завершается.
- **Как проверяется.** `verify_group_calls_e2e`: каждая правка ровно один раз; нет `startGroupCall(ssrc, json, true)` и `}, conference != null);`; оба `joinGroupCall` — с `public_key`. Тесты `GroupCallsE2ETest`: синтетическое дерево, порча каждой правки, изменённый якорь, `test_chain_retries` (стоящая цепочка останавливает вход на 10-м отказе подряд, движущаяся — только на 100-й попытке), `test_real_upstream` (применение и порча на настоящем апстриме `7be672ea`; после отката правок файлы побайтно равны апстриму).
- **Если апстрим изменит.** Сломанный якорь в `VoIPService`, `ConferenceCall`, `NativeInstance`, JNI — перечитать путь входа и места `conference != null`/`isConference()`. Требования к серверу: в ответе на вход подцепочка 0 — ровно блок входа (Android создаёт объект из первого блока обновления, `ConferenceCall.java:404-407, 468-470`; `Call::create` требует вызывающего в составе, tde2e `Call.cpp:619-630`); последняя страница `getGroupParticipants` — с пустым `next_offset`.
- **Не проверено.** Simulcast вместе с шифрованием; соответствие готовой `libtde2e.a` исходникам td `022d6020`.
- **История.** `ad2fa92` (2026-11-12), повторы входа — `ac9d6c0` (2026-11-12) и `f6fcdfd` (2026-11-13); задача 094.

### A-118. Нет «войти от имени» в групповых звонках (задача 094)

- **Статус.** Ветка `task/094-group-calls-e2e`, не влито, не выпущено (2026-11-13).
- **Что и зачем.** Ключ в цепочке — пользователя, сервер отклоняет `join_as` ≠ сам (ADR 0060). Звонок начинается и идёт только от себя.
- **Где.** `ui/Components/JoinCallAlert.java`, `checkFewUsers`/`open` (`:221-298`): только сам пользователь, `phone.getGroupCallJoinAs` не запрашивается; лист «начать/запланировать» при начале звонка остаётся. `VoIPService.onStartCommand`: пир канала или чата из intent (в том числе из `groupcall_default_join_as`, `VoIPHelper.java:311-316`) отбрасывается, `hasFewPeers = false` — строка «отображать как» (`GroupCallActivity:1777`) не появляется.
- **Апстрим без правки.** Список «войти от имени» с каналами пользователя, выбор запоминается.
- **Как проверяется.** `verify_group_calls_e2e` (в `JoinCallAlert` нет `getGroupCallJoinAs`), порча в `GroupCallsE2ETest`.
- **История.** `ad2fa92`, 2026-11-12, задача 094.

### A-119. Нет RTMP и живых историй (задача 094)

- **Статус.** Ветка `task/094-group-calls-e2e`, не влито, не выпущено (2026-11-13).
- **Что и зачем.** RTMP-трансляции и живые истории — групповые звонки без сквозного шифрования (части трансляции приходят с сервера открытыми); сервер их не обслуживает (ADR 0060 §4).
- **Где.**
  - `ui/Components/CreateGroupCallBottomSheet.java` (`:84, 236-241, 261`): нет «Stream with another app».
  - `VoIPService`: `isRtmpStream` всегда `false` (`:828`); ответ на вход не переключает звонок в режим трансляции (`"stream"`, `:1719`); части трансляции не запрашиваются.
  - `StoryModeTabs` (`:75-76`): у камеры историй нет вкладки Live.
  - `ui/LinkManager.java` (`:259-260`): ссылка `post/live` открывает камеру для фото, а не в режиме Live, — `StoryRecorder.startLive` (`:3316-3351`) недостижим.
  - `StoryPrivacyBottomSheet`: нет строк RTMP (`:1187-1201, 1275-1280`); обработчик строки 5 остался без строки.
- **Апстрим без правки.** «Stream with another app» в листе звонка, вкладка Live и настройки RTMP у камеры историй.
- **Как проверяется.** `verify_group_calls_e2e` падает, если `CreateRtmpStreamBottomSheet` упомянут где-то кроме своего файла и `StoryPrivacyBottomSheet`; есть `ItemInner.asButton(5,`; у камеры вкладки кроме 0 и 1; `MODE_LIVE` назван не только в `StoryRecorder` или не только в сравнениях; есть `optBoolean("stream")` или чтение `isRtmpStream`. Тесты `GroupCallsE2ETest.test_check`, `test_real_upstream`.
- **Если апстрим изменит.** Новый вход в лист RTMP, вкладка Live или новое место, переводящее камеру в режим Live, останавливают `verify`.
- **Остаётся.** `ui/Stories/LivePlayer.java` (просмотр чужой живой истории, `:229-237`) входит без ключа; сервер живые истории не создаёт и вход без ключа отклоняет.
- **История.** `ad2fa92`, 2026-11-12, задача 094.

### A-120. Пункт записи звонка скрыт (задача 094)

- **Статус.** Ветка `task/094-group-calls-e2e`, не влито, не выпущено (2026-11-13).
- **Что и зачем.** Сервер звонки не записывает (`phone.toggleGroupCallRecord` → `ErrEnterpriseIsBlocked`), сквозной звонок на сервере записать нельзя.
- **Где.** `ui/GroupCallActivity.java`: оба `recordItem.setVisibility(View.VISIBLE)` (`:1693-1710`) → `View.GONE`. Обработчик скрытого пункта остаётся.
- **Апстрим без правки.** Пункт «Начать запись» в меню звонка чата; у конференции апстрим его уже прячет (`:1706-1708`).
- **Как проверяется.** `verify_group_calls_e2e` падает, если у `recordItem` появится видимость не `View.GONE` или `.toggleRecord(` вызывается не из `GroupCallActivity`. Тесты `GroupCallsE2ETest.test_check` (порча: показ пункта, запись из другого файла), `test_real_upstream` (у апстрима 2 показа, после патча 0, обработчик на месте).
- **История.** `ac9d6c0`, 2026-11-12, задача 094.

### A-121. Отчёт `PlumaE2EStatus` v1 и самовосстановление шифрования (задача 094, ADR 0060 §5)

- **Статус.** Ветка `task/094-group-calls-e2e`, не влито, не выпущено (2026-11-13).
- **Что и зачем.** Рассинхрон ключей виден серверу без ключей и медиа, клиент восстанавливается сам. Схема отчёта одна для всех четырёх клиентов (ADR 0060 §5).
- **Отчёт.**
  - **Канал:** раз в 5 с после входа, по каналу данных основного соединения к SFU, рядом с `ReceiverVideoConstraints`, и только пока канал открыт (`GroupInstanceCustomImpl::isDataChannelOpen()`, атомарный флаг в `updateIsDataChannelOpen`, JNI). Неотправленный отчёт не теряется: его приращения уходят в следующем.
  - **Поля — ровно v1** (`E2E_STATUS_FIELDS`): `colibriClass` (`"PlumaE2EStatus"`), `v` (1), `seq` (с 1 после каждого входа), `height` (`call_get_height`, −1 без состояния), `lastBlock` (первые 8 байт SHA-256 блока на этой высоте в форме, в которой его хранит сервер: магия снимается как в tde2e `Blockchain.cpp:742-753`; без состояния `""`), `offset0`/`offset1` (с какого блока опрашивается каждая подцепочка), `error` (залипшая ошибка tde2e, `jni/tde2e/bridge.cpp` `call_get_error_code`), `sendEpochs` ≤15, `decryptEpochs` ≤15, `unknownEpochs` ≤8 (8-байтные префиксы хэшей эпох из открытого `header_a`, формат кадра tde2e `Call.cpp:286-399`), `encrypted`, `encryptFailed`, `senders` ≤100 по возрастанию `user`, только с кадрами за интервал (`user`, `ok`, `unknownEpoch`, `tooOld`, `duplicate`, `failed`, `lastOkMs`).
  - Счётчики кадров и списки эпох — приращения с прошлого отчёта (`PlumaE2EStats::take`). Больше 16 КиБ или больше 100 отправителей — первыми отбрасываются записи с наибольшим `lastOkMs`. Ключей, nonce, эмодзи и байтов кадров нет.
- **Самовосстановление** — повторный вход с новым ключом, как tdesktop на любую ошибку tde2e (`calls_group_call.cpp:786-789`), когда выполнено одно из условий:
  1. залипшая ошибка tde2e: сбой применения блока (`ConferenceCall` `:423-425`: апстрим только пишет в лог, и очередь стоит на этом индексе навсегда) или `checkError()` на тике отчёта;
  2. подцепочка 0 отстаёт от `next_offset` сервера дольше 12 с (`BEHIND_LIMIT_MS`);
  3. `GROUPCALL_FORBIDDEN` на любом запросе к цепочке — чтение и запись: опрос, последний блок, рассылка, удаление (`ConferenceCall.processUpdates`; tdesktop `calls_group_call.cpp:848-853, 1909-1917, 1930-1934`);
  4. 15 с кадры приходят, и **ни один** отправитель с кадрами не расшифрован (`ok = 0` и есть `unknownEpoch` или `failed`), а своя цепочка не отстаёт (`PLUMA_E2E_UNDECRYPTED_LIMIT_MS`; сторож Web K `groupCallInstance.ts:51`). Один сбоящий отправитель вход не вызывает.
- **Пределы.** Не больше 3 входов за скользящие 5 мин, задержки 0/2/5 с плюс 0–1000 мс; дальше выход с сообщением `PlumaCallEncryptionFailed` на всех десяти встроенных языках. Пока вход ожидается или идёт, условия не проверяются и отчёт остановлен. Повторный вход после обрыва связи и повторы `CONF_WRITE_CHAIN_INVALID` (A-117) в предел не идут. `reset()` всегда отменяет опрос и сбрасывает `polling`/`joined`.
- **Где.** `messenger/voip/VoIPService.java` (`plumaE2EFailed()`), `ConferenceCall.java` (`fail()`), `NativeInstance.java`; `jni/voip/org_telegram_messenger_voip_Instance.cpp` (`PlumaE2EStats` в колбэке `e2eEncryptDecrypt`, `:496-525`; JNI `takeE2EStats`, `sendDataChannelMessage`); `jni/tde2e/bridge.cpp` (`call_get_error_code`); tgcalls `TMessagesProj/jni/tgcalls` (`545f5ae5`) `group/GroupInstanceCustomImpl.h/.cpp` (`sendDataChannelMessage`, `isDataChannelOpen`, `:2772-2838`); строки `PlumaCallEncryptionFailed` в десяти `res/values*/strings.xml`.
- **Апстрим без правки.** Отчёта нет; ошибка блока только в логе, звонок остаётся без ключа.
- **Как проверяется.** `verify_group_calls_e2e`. Тесты `GroupCallsE2ETest`: `test_report_fields` (поля в коде = v1, запрещённых имён нет), `test_report_semantics`, `test_self_recovery`, `test_texts`, `test_check`, `test_upstream_changed`, `test_second_run`, `test_real_upstream` (DAMAGE каждой правки на `7be672ea`).
- **Если апстрим изменит.** Изменённый колбэк шифрования или канал данных tgcalls останавливает патч. Схему отчёта менять только вместе с SFU и тремя другими клиентами (ADR 0060 §5).
- **Не проверено.** Приём отчёта SFU; правило 4 работает только на тиках с открытым каналом данных; задержки и предел на живом звонке не наблюдались.
- **История.** Первый отчёт — `ad2fa92` (2026-11-12, своя схема); v1 и одно правило самовосстановления для всех клиентов — `ac9d6c0` (2026-11-12); задача 094.

### A-122. Цвета звонков: вариант A «Шалфей» (задача 094, решение владельца 2026-11-12)

- **Статус.** Ветка `task/094-group-calls-e2e`, не влито, не выпущено (2026-11-13).
- **Что и зачем.** Экраны звонков без цветов Telegram. Telegram iOS рисует фон звонка тремя градиентами по четыре точки (`CallBackgroundLayer.swift:108-124`); у Плюмы в том же порядке точек:
  - соединение — `#3A4A3C #4F6E52 #2C3A2D #5B7A5E`;
  - соединён — `#5B7A5E #6F8F72 #4F6E52 #7FA082`;
  - слабый сигнал — `#8A5A44 #A86B4C #7A4E3B #B97A55`.

  Перенос по смыслу: синее и фиолетовое Telegram → «соединение», зелёное → «соединён», красное и оранжевое → «слабый сигнал»; акценты тёмных экранов → `#9DBB9F`, ссылки → `#B2CEB4` (палитра 017); голубовато-серые панели, иконка «микрофон выкл.» и серые тексты → шалфейно-серый той же светлоты (`call_neutral`).
- **Где.**
  - **Личный звонок, `ui/Components/voip/VoIpGradientLayout.java`.** Конструктор, `bg*`: `bgBlueViolet` и `bgBlueGreen` → «соединение», `bgGreen` → «соединён», `bgOrangeRed` → «слабый сигнал»; `*Dark` — value×0,82, `*Light` — value×1,22 при saturation×0,8, `*Reveal` — как у апстрима; сплошной фон `0xFF191F26` → `0xFF1C231D`. Ещё `VoIPFragment` (градиент `0xFF1b354e/0xFF255b7d`, `0xFF222A33`), `PrivateVideoPreviewDialogNew` (`bgGreen`, `bgBlueViolet`).
  - **Группа, `ui/ActionBar/ThemeColors.java`.** 55 ключей `voipgroup_*` (`VOIPGROUP_COLOURS`), включая `mutedIcon`, `mutedIconUnscrolled`, `lastSeenText`, `lastSeenTextUnscrolled`, `searchPlaceholder`; темы апстрима оставляют их по умолчанию, движок акцента их пропускает (`themeAccentExclusionKeys`). `darkblue.attheme`: `voipgroup_topPanelGray=-10521727` → шалфейно-серый.
  - **Литералы (`CALL_COLOUR_EDITS`):** `GroupCallActivity` (громкость, LIVE, ссылка, фон сообщений, селекторы и кнопка расписания, серые тексты расписания `0xff7B8389`, попап, подсказки, фон), `CreateGroupCallSheet` (кнопка закрытия), `GroupCallSheet`; `CallPipSelfView`, `CallSystemPipView`, `GroupCallPipContent`, `RtmpStreamPipContent`; `VoIPTextureView`, `GroupCallMiniTextureView` (плитка без видео); `PrivateVideoPreviewDialog`, `GroupCallRecordAlert`, `VoIPOverlayBackground`, `GroupCallPip`, `CapsuleBlobDrawable`, `FragmentContextView` (кнопка «напомнить»); уведомление — `VoIPService` `builder.setColor(0xff282e31)`, `res/drawable/call_notification_bg.xml`.
- **Оставлено как у Telegram** (действия, не состояния; на подтверждение владельцу): зелёная «принять» и красные «отклонить»/«завершить» личного звонка (`AcceptDeclineView`, `VoIPFragment`, `EndCloseLayout`); красное «завершить» и зелёное «ответить» в уведомлениях (`VoIPService`, `VoIPGroupNotification`, `VoIPPreNotificationService`); красные «выйти» группового звонка (`voipgroup_leaveButton`, `leaveButtonScrolled`, `leaveCallMenu`, `GroupCallPipAlertView`); красный при перетаскивании пузыря на закрытие (`GroupCallPipButton`); точка записи скрытого пункта (A-120) и красный текст сверх лимита. Синий акцент уведомления звонка уже перекрашен брендом (A-049), значок верификации в `GroupCallUserCell` — A-114.
- **Апстрим без правки.** Синие, фиолетовые, зелёные и красные градиенты Telegram, голубовато-серые панели группового звонка.
- **Как проверяется.** `verify_call_colours`: каждая правка ровно нужное число раз, старого текста нет; каждый ключ `voipgroup_*` в `ThemeColors` и в пяти встроенных темах — не цвет Telegram (`telegram_call_colour`); каждый цветовой литерал экранов и уведомлений звонка (`CALL_UI_FILES` и маски `ui/Components/voip/*`, `ui/Cells/GroupCall*`, `ui/Components/GroupCall*`) — не цвет Telegram, кроме `CALL_COLOURS_KEPT` с точным числом вхождений; пропавший файл экрана — падение. Классификатор ловит синие, голубые, фиолетовые и розовые (голубовато-серые — от насыщенности 0,08), яркие зелёные, красные, оранжевые и коралловые. `call_colours` сначала проверяет все якоря и падает, если у апстрима появился ключ с цветом Telegram вне таблицы. Тесты `CallColoursTest`: `test_palette`, `test_classifier`, `test_mapping`, `test_applies`, `test_upstream_changed`, `test_check`, `test_real_upstream` (DAMAGE на `7be672ea`).
- **Если апстрим изменит.** Сдвинутый якорь — `PatchError`. Новый ключ или литерал с цветом Telegram в файлах звонка — падение проверки с файлом и цветом.
- **Не проверено.** Контраст белого текста и иконок на шалфее (верхняя панель `#7FA082` — около 2,9:1, у Telegram `#52CE5D` хуже); различимость «микрофон выкл.» (`#4F6E52`/`#5B7A5E`) и «говорит» (`#7FA082`/`#6F8F72`); Lottie-анимации кнопок с динамическими цветами. Ключи `voipgroup_*` красят и тёмные листы вне звонка (шаринг, просмотрщик фото): там тоже шалфейно-серый.
- **История.** `ac9d6c0`, 2026-11-12, задача 094.

---

## Снятые правки

Правки, которые были в клиенте и потом убраны; записаны, чтобы история была полной.

### A-077. Снято: якорь самолёта интро

- **Статус.** Снято в `d9ad7cf` (2026-10-31).
- **Что было.** `jni/intro/IntroRenderer.c`: `telegram_plane.params.anchor = xyzMake(6, -5, 0);` → `xyzMake(0, 0, 0)` — текстура знака была центрирована, самолёт нет.
- **История.** Введено `a946650`, 2026-10-29, задача 017. Заменено A-035/A-036.

### A-078. Снято: белый круг под знаком в интро

- **Статус.** Снято в `d9ad7cf` (2026-10-31).
- **Что было.** `ui/IntroActivity.java`: `paint.setColor(ThemeColors.TELEGRAM_COLOR); // It's logo color, it should not be colored by the theme` → `paint.setColor(0xFFFFFFFF); // pluma: the white disc of the app icon, under the mark`.
- **История.** Введено `a946650`, 2026-10-29, задача 017. Заменено A-035/A-036.

---

## Пробелы и замечания

- Правки A-001…A-021 и замена ресурсов проверяются только на настоящем апстриме (CI `build` или локальный `configure.sh`); `patch_test.py` их не воспроизводит.
- A-021 не ловит новые строки поиска «Помощи»; A-005 не ловит новый источник резервных адресов вне `onRequestNewServerIpAndPort`.
- Ветки: `origin/main` и `task/020-parity-fixes` на `d9ad7cf`; локальная `main` отстаёт (`7c78c00`). `sticker-set-kinds` = `origin/main` + `339fd8c`.
- A-099…A-106 (задача 026) проверены применением на копии апстрима `aac1efcb` и тестами `LinksTest` (A-105, A-106 ещё `aapt2 link` манифеста и `javac` блока Java); сборка CI (Gradle) прошла, выпуск `12.9.1.00031`; `assetlinks.json` с отпечатком подписи отдаётся на pluma.chat. Поведение на устройстве и проверка app links системой не проверены.
- Не сделано и правок нет: возврат «Помощи» (задача 028); перевод «Send Message» баннера на другие встроенные языки (A-056).
- A-086…A-092 проверены применением на копии апстрима и тестами скрипта; компиляция Java (Gradle) и поведение на устройстве не проверены.

### A-112. Ссылка `getpremium` закрыта (задача 074)
- **Статус.** Ветка `task/074-upstream`, не влита, не выпущена (2026-11-09).
- **Что.** Premium есть у всех (ADR 0041), экран покупки не нужен, поэтому закрыты все входы в него ссылками: `LinkManager` — маршрут `getpremium` и ветки `settings/premium`, `settings/business` (такие ссылки открывают настройки); `LaunchActivity`, ветка `premium_offer` — ссылка только открывает приложение (счётчик `SCHEME_JAVA` для `LaunchActivity` стал 158: ушла строка `finalUrl` с двумя литералами `tg:`). Проверяется таблицей `PREMIUM_FOR_EVERYONE_EDITS` (`verify_premium_for_everyone`, `PremiumForEveryoneTest`), порчей на настоящем апстриме.
