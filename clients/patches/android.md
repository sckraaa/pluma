# Реестр правок клиента Android

Здесь записано всё, что Pluma меняет в официальном клиенте Telegram для Android: правки исходников, ресурсы, сборка, CI. По реестру при обновлении апстрима видно, что каждая правка затрагивает и где её искать.

Состояние: `main` репозитория `pluma-android` на `e859d6b` (2026-11-03): в него слиты `sticker-set-kinds` (`339fd8c`, A-060) и `video-quality` (`b7accac`, A-079…A-085). Теги «Избранного» без Premium (A-086…A-092, задача 053) — в рабочем дереве ветки `task/053-saved-tags`, не закоммичены (2026-11-04). Реестр составлен 2026-11-03 по `ci/patch.py`, `ci/patch_test.py`, README, скриптам CI и полной истории git (20 коммитов во всех ветках, включая непринятую ветку Dependabot `69205f7`).

## Общее

- **Репозиторий:** `sckraaa/pluma-android` (локально `~/pluma-android`). Кода клиента в нём нет: только скрипт патча, ресурсы бренда, конфиги и CI.
- **Закреплённый апстрим** (`upstream.env`):
  - `UPSTREAM_REPO=https://github.com/DrKLO/Telegram.git`
  - `UPSTREAM_REF=aac1efcb83d6575efb0c819c6ddc4ea6c8cf64e1`
  - `UPSTREAM_VERSION=12.9.1`, `UPSTREAM_LAYER=228`
  - `NDK_VERSION=27.2.12479018`, `CMAKE_VERSION=3.10.2`, `CMAKE_PACKAGE=3.10.2.4988404`, `BUILD_TOOLS=35.0.0`, `PLATFORM=android-35`
- **Как применяется патч:**
  1. `ci/fetch-upstream.sh` скачивает в `./upstream` один коммит `UPSTREAM_REF` (`--depth 1`, три попытки) и сверяет `HEAD`.
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
  - `TMessagesProj/src/main/java/org/telegram/tgnet/TLRPC.java`: `public static final int LAYER = 228;`
  - `gradle.properties`: `APP_VERSION_NAME=12.9.1\n`
  - `TMessagesProj/build.gradle` и `TMessagesProj_App/build.gradle`: `ndkVersion "27.2.12479018"`
  - `TMessagesProj/build.gradle`: `version '3.10.2'`
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
- **Где.** `TMessagesProj/src/main/java/org/telegram/tgnet/ConnectionsManager.java`, метод `onRequestNewServerIpAndPort(final int second, final int currentAccount)`. Замена тела (`replace_body`) от `    public static void onRequestNewServerIpAndPort(final int second, final int currentAccount) {\n` до первого `\n    }\n`; блок обязан содержать `GoogleDnsLoadTask` и `FirebaseTask`. Новое тело — один комментарий `// pluma: the client knows its server; Telegram's backup addresses do not apply.`
- **Как проверяется.** Только сам `replace_body`. Сами классы `GoogleDnsLoadTask`/`FirebaseTask` в файле остаются (не вызываются).
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
- **Где.** Все `res/values*/strings.xml` (`brand_strings` → `rename_strings`). Меняется только текст элементов (регулярное выражение `>[^<]*<`), внутри — целое слово по `TELEGRAM_WORD = (?<![A-Za-z0-9@/.])(Telegram|TELEGRAM)(?![A-Za-z0-9]|\.\w)`: `Telegram` → `Pluma`, `TELEGRAM` → `PLUMA`. Число замен закреплено по файлу (`STRING_RENAMES`, `(Telegram, TELEGRAM)`):

  | Папка | Telegram | TELEGRAM |
  |---|---|---|
  | `values` | 508 | 1 |
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
- **История.** `46fefef`, 2026-10-29, задача 017. Счётчики с тех пор не менялись.

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
  - `ui/Components/MessagePreviewView.java`: `"Subscribe to **Telegram Premium** to` → `"Subscribe to **Pluma Premium** to` (1);
  - `ui/Components/PasscodeView.java`: `"Unlock to use Telegram"` → `"Unlock to use Pluma"` (1);
  - `ui/LoginActivity.java`: `But Telegram says it's` → `But Pluma says it's` (2);
  - `ui/NewContactBottomSheet.java`: `"This phone number is not on Telegram.` → `"This phone number is not on Pluma.` (1), `"This phone number is on Telegram."` → `"This phone number is on Pluma."` (1);
  - `ui/ThemePreviewActivity.java`: `"Telegram Beta Chat"` → `"Pluma Beta Chat"` (1).
- **Как проверяется.** Только счётчики замен. Проверки, что в литералах Java не осталось других «Telegram», нет.
- **Если апстрим изменит.** Изменённый литерал — падение. Новый литерал с «Telegram» не будет замечен: искать вручную (`grep '"[^"]*Telegram'`).
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
  `messenger/AndroidUtilities.java` (13, 1), `messenger/MusicPlayerService.java` (2, 0), `messenger/browser/Browser.java` (3, 1), `ui/Business/BusinessLinksController.java` (1, 0), `ui/CameraScanActivity.java` (1, 0), `ui/Cells/ChatMessageCell.java` (2, 1), `ui/ChatActivity.java` (13, 0), `ui/Components/Premium/boosts/GiftInfoBottomSheet.java` (2, 1), `ui/Components/Premium/boosts/UserSelectorBottomSheet.java` (2, 1), `ui/Components/UndoView.java` (3, 0), `ui/LaunchActivity.java` (156, 2), `ui/LinkManager.java` (2, 2), `ui/LoginActivity.java` (1, 0), `ui/PaymentFormActivity.java` (0, 1), `ui/ProfileActivity.java` (76, 0), `ui/ProxySettingsActivity.java` (2, 0), `ui/SessionsActivity.java` (1, 0), `ui/WebviewActivity.java` (2, 1).
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
- **Где.** `TMessagesProj/src/main/java/org/telegram/ui/SettingsActivity.java`.
  - Замена тела (`replace_body`): от `getString(R.string.SendAGift)));\n        }\n` до `            items.add(UItem.asHeader(getString(R.string.SettingsDebug)));\n`; блок обязан содержать `R.string.AskAQuestion`, `R.string.TelegramFAQ`, `R.string.TelegramFeatures`, `R.string.PrivacyPolicy`, `items.add(UItem.asShadow(null));`. Новое тело оставляет только вход в раздел отладки (`if (BuildVars.LOGS_ENABLED || BuildVars.DEBUG_PRIVATE_VERSION)`) с тенью перед ним, если её ещё нет.
  - Точная замена `        items.add(UItem.asCustomShadow(versionView));\n`: перед ней убирается последняя обычная тень, чтобы не было двух подряд.
- **Как проверяется.** Сами правки; тестов и `verify` нет.
- **Если апстрим изменит.** Новый пункт между «Send a Gift» и отладкой попадёт в удаляемый блок без предупреждения, если `must_contain` выполнены. При обновлении сравнить блок апстрима глазами.
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

- **Где.** `res/raw/qr_logo.svg` (центр QR, который рисует `QRCodeWriter`: белый знак на шалфейном круге); `res/raw/qr_code_logo.json`, `qr_code_logo_2.json`, `plane_logo_plain.json` (белый неподвижный знак поверх QR и водяного знака круглого видео).
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
    - `private class ScanQRCodeView extends FrameLayout implements NotificationCenter.NotificationCenterDelegate {` + `BackupImageView imageView;` → без интерфейса, `RLottieImageView imageView;`;
    - создание `new BackupImageView(context)` → `RLottieImageView` с `setAnimation(R.raw.pluma_devices, 130, 130)`, `setAutoRepeat(2)`, `playAnimation()`;
    - `imageView.getImageReceiver().getLottieAnimation()` → `imageView.getAnimatedDrawable()` (4 вхождения, обработчик нажатия);
    - удалён вызов `setSticker();` в конструкторе;
    - удалены целиком `onAttachedToWindow`, `onDetachedFromWindow`, `didReceivedNotification` (наблюдатель `diceStickersDidLoad`) и `setSticker()` — от `@Override protected void onAttachedToWindow()` до `private void openCameraScanActivity() {`.
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
- **История.** `4a1711e` 2026-10-29 (Blue и Dark Blue; сборка на `72cf711` эту проверку не проходит); `4be989b` 2026-10-30 (пять тем, все такие акценты); `7a9ccbb`, `7c78c00` (`ALSO_CHECKED`); задача 017; описание проверки в README — `24f3c2d`, 2026-10-29 (только документация).

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

## Самообновление (задача 054)

- **Статус.** В ветке `task/054-self-update` репозитория `pluma-android` (2026-11-04, PR #2): правка клиента (`e314edb`) запушена, шаг выпуска в CI (`0f3ff30`) ждёт права `workflow` у токена `gh`. Проверено применением на файлах апстрима `aac1efcb` и тестами `SelfUpdateTest`; компиляция Gradle — сборкой PR; поведение на устройстве — после подписанной сборки.
- **Зачем.** APK с сайта обновляется сам, как APK Telegram с telegram.org: `help.getAppUpdate` → лист обновления и плашка бокового меню → APK по MTProto → системный установщик (исследование `docs/tasks/054-research/clients.md`, §1; ADR 0035).
- **Почему не `isStandalone() = true`.** Он включил бы и прочее standalone-сборки: строку TON в настройках и профиле (`SettingsActivity.java:712`, `ProfileActivity.java:10628`), биллинг по счетам (`BuildVars.java:63`), подпись «direct». Поэтому новый признак — только в местах обновления.

### A-093. `ApplicationLoader.isSelfUpdatingBuild()`

- **Где.** `messenger/ApplicationLoader.java`, после `isStandaloneBuild()` (131-133): `public static boolean isSelfUpdatingBuild() { return true; }` с комментарием `// pluma: …`.

### A-094. Места обновления спрашивают `isSelfUpdatingBuild()`

- **Где.** `isStandaloneBuild()` → `isSelfUpdatingBuild()`:
  - `ui/LaunchActivity.java:5958` — проверка обновлений `checkAppUpdate`, `:6011` — лист `showUpdateAppPopup`;
  - `ui/ChatActivity.java:41304`, `ui/Stories/PeerStoriesView.java:4416` — кнопки «обновить приложение» у неподдерживаемых сообщений и историй (вместо Google Play);
  - `ui/Components/BlockingUpdateView.java:142`, `:303` — блокирующий экран (`can_not_skip`);
  - `messenger/SharedConfig.java:771` — плашка бокового меню (`isAppUpdateAvailable`);
  - `ui/ProfileActivity.java:4653`, `ui/SettingsActivity.java:1430` — пункт отладочного меню «Check app update».
- **Проверка.** `verify_self_update` считает вызовы в каждом файле (`SELF_UPDATE_USES`): лишнее место — ошибка.

### A-095. `ApplicationLoaderImpl` приложения: установка, лист, плашка

- **Где.** `TMessagesProj_App/src/main/java/org/telegram/messenger/ApplicationLoaderImpl.java` заменяется целиком (сверка с текстом апстрима): переопределения `checkApkInstallPermissions`, `openApkInstall`, `showUpdateAppPopup`, `takeUpdateLayout` из `TMessagesProj_AppStandalone/.../ApplicationLoaderImpl.java:63-114`, без SMS-заданий.

### A-096. Файлы листа и плашки

- **Где.** `UpdateAppAlertDialog.java` (SHA-256 `4545e52e…`) и `UpdateLayout.java` (`e1050c8c…`) копируются из `TMessagesProj_AppStandalone/src/main/java/org/telegram/ui/Components/` в тот же путь `TMessagesProj_App`; изменённый апстримом файл — ошибка. Строки и ресурсы — библиотеки (`org.telegram.messenger.R`).
- `TMessagesProj_App/build.gradle`: `implementation 'androidx.core:core:1.16.0'` (`FileProvider`, `NestedScrollView`): библиотека держит его как `implementation`, модуль standalone тоже объявляет его сам; версия сверяется с библиотекой.

### A-097. Версия `12.9.1.NNNNN`

- **Где.** `TMessagesProj_App/build.gradle:176`: `versionName APP_VERSION_NAME + ".NNNNN"` (5 цифр с нулями); `:157`: `versionCode = APP_VERSION_CODE * 1000 + N` (итог `(6979000 + N) * 10 + 9`). `gradle.properties` не меняется: `BUILD_VERSION_STRING` библиотеки остаётся `12.9.1`, её компиляция не выпадает из кэша Gradle; приложение сообщает и сравнивает версию пакета (`ConnectionsManager.java:223`, `SharedConfig.java:567`, `:791`).
- **Почему 5 цифр.** После перезапуска клиент сохраняет предложение, только пока своя `versionName` меньше как строка (`SharedConfig.java:576`): `12.9.1.99` против `12.9.1.100` сбросило бы его.
- **N** — `--build` у `ci/patch.py` (`PLUMA_BUILD` в `configure.sh`, в CI `github.run_number`; 0 для локальной сборки).
- **Зачем.** Android сравнивает версии по общим частям (`SharedConfig.java:811-824`): без четвёртой части сборки одного апстрима неразличимы. Установки `12.9.1` (три части) обновления не увидят — один раз поставить с сайта.

### A-098. Выпуск на сайт (`ci/publish.sh`, `android.yml`)

- После подписи в job `sign` ветки `main`: `ci/publish.sh signed/<apk> $GITHUB_RUN_NUMBER` → `POST <PLUMA_RELEASES_URL>/dl/upload` (токен — секрет `RELEASES_UPLOAD_TOKEN` окружения `release`, заголовок через файл), версия `12.9.1.NNNNN`, заметки `release/notes.en.txt`, `release/notes.ru.txt`. 409 (сборка уже выложена) — не ошибка. Нет токена или адреса — job падает. Сборка с входом `abis` не публикуется (только подписывается); недельный canary в своей группе concurrency и не вытесняет ожидающий push.

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
  - `messenger/AndroidUtilities.java:4609`: ссылка на прокси (`/socks`, `/proxy`);
  - `messenger/browser/Browser.java:727` (`isInternalUri`): `"telegram.me".equals(host) || "t.me".equals(host)` → с `"pluma.chat".equals(host)`; отсюда каждая нажатая ссылка решает «в приложении или в браузере». Ветка `telegram.dog` (`:712`, блог и FAQ Telegram) не меняется;
  - `ui/Cells/ChatMessageCell.java:10884`: кнопка бота «добавить в группу» (`startgroup`);
  - `ui/web/BotWebViewContainer.java:4089`: встроенный пост (`embed=1`) остаётся в веб-вью мини-приложения;
  - `ui/Business/BusinessLinksController.java:311` (`findLink`): ссылка на свой бизнес-чат `https://pluma.chat/m/<slug>` — такую сделает сервер по `me_url_prefix` (сейчас `account.getBusinessChatLinks` не обслуживается).
- **Не меняются** (`HOSTS_KEEP`): коды подарков `UserSelectorBottomSheet.java:146`, `GiftInfoBottomSheet.java:88`; платежи и подарки Premium/звёзд `PaymentFormActivity.java:1257`, `:2539`, `GiftPremiumBottomSheet.java:304`, `SendGiftSheet.java:835`, `PremiumPreviewFragment.java:1168` — этого в Pluma нет; ветка `telegram.dog` в `Browser.java:712`.
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
  - `ui/ProxySettingsActivity.java:597`, `:608`: `{"t.me/socks?", ` → `{"t.me/socks?", "pluma.chat/socks?", ` и то же для `proxy?` — ссылка на прокси из буфера обмена (A-103 делает такие ссылки на pluma.chat).
  - `ui/Components/TranslateAlert2.java:863-865`: ссылка-упоминание из перевода `https://pluma.chat/<username>` с текстом `@username` снова становится упоминанием; длина префикса берётся как `entity.url.indexOf('/', 8) + 1` вместо 13. `messages.translateText` сервер пока не обслуживает.

### A-103. Ссылки, которые приложение делает само

- **Что и зачем.** Скопированная, отправленная, показанная или в QR ссылка — pluma.chat. Большинство ссылок апстрим уже строит из `linkPrefix` (около 100 мест: профиль, юзернейм, наборы стикеров, темы, QR профиля); здесь — жёстко прописанные `https://t.me/`.
- **Где** (`LINKS_OUT`, число литералов `"https://t.me/` и `"t.me/` → pluma.chat):
  `messenger/SharedConfig.java` (2, 0) — ссылка на прокси `:408`; `messenger/voip/VoIPService.java` (1, 0) — `/call/<slug>` `:2241`; `ui/Cells/StickerSetCell.java` (0, 2) — префиксы `addstickers/`, `addemoji/` `:70-71`; `ui/ChatActivity.java` (4, 0) — ссылка на сообщение с `?t=` `:35984`, `:35990`, `:44224`, `:44230`; `ui/ChatEditActivity.java` (0, 1) — публичная ссылка бота `:1627`; `ui/Components/PermanentLinkBottomSheet.java` (1, 0) `:127`; `ui/Components/StickersAlert.java` (0, 1) — префикс поля `:1667`; `ui/FilterChatlistActivity.java` (0, 1) — образец ссылки на папку под спойлером `:967`; `ui/GroupStickersActivity.java` (0, 1) `:1012`; `ui/ManageLinksActivity.java` (5, 0) — публичная ссылка `:837` и заголовок приглашения `:1362-1365` (`https://pluma.chat/+` → `pluma.chat/<hash>`; ссылка t.me падает в ветку `https://`); `ui/PhotoViewer.java` (2, 0) `:1397`, `:1403`; `ui/PrivacyControlActivity.java` (1, 0) — ссылка на номер `+%s` `:2007`; `ui/ProxySettingsActivity.java` (2, 0) `:520`, `:526`; `ui/SessionsActivity.java` (1, 0) — подключённые чат-боты в подтверждении «завершить все сеансы» `:331`; `ui/Stories/ChannelBoostUtilities.java` (2, 0) `:14`, `:16`; `ui/Stories/PeerStoriesView.java` (4, 0) — ссылки на историю `:7103-7114`.
- **По умолчанию `linkPrefix`.** `messenger/MessagesController.java:1585`: `getString("linkPrefix", "t.me")` → `"pluma.chat"` — до первого конфига; потом значение из `me_url_prefix`.
- **Остаются** (`LINKS_KEEP`, число литералов): ссылки, которые приложение само превращает обратно в t.me и открывает у себя — `LaunchActivity.java` 1 (`:1971`, `x.t.me`), `LinkManager.java` 1 (`:111`), `messenger/browser/Browser.java` 4 (`:207-213` распознавание, `:693`), `Components/URLSpanNoUnderline.java` 1 (`@username`), `web/BotWebViewContainer.java` 1 (`openTgLink` мини-приложения, `:1759`), `Components/DialogsBotsAdapter.java` 1; формы t.me, которые приложение по-прежнему распознаёт рядом с pluma.chat — `ProxySettingsActivity.java` 2, `Business/BusinessLinksController.java` 1, `Components/TranslateAlert2.java` 1; сервисы Telegram, которых в Pluma нет — `PremiumPreviewFragment.java` 2, `ChatEditActivity.java` 4 (BotFather), `Stars/StarsIntroActivity.java` 1, `Stars/ProfileGiftsView.java` 1 (`nft`), `Components/CreateBotAlert.java` 1, `Components/AIEditorAlert.java` 1 (`addstyle`), `Components/Reactions/ChatCustomReactionsEditActivity.java` 1 (@stickers), `Components/Premium/boosts/PremiumPreviewGiftLinkBottomSheet.java` 1, `…/adapters/GiftInfoAdapter.java` 1, `…/cells/LinkCell.java` 3 (`giftcode`), `messenger/MessagesController.java` 3 (`premiumbot`, `spambot` ×2), `messenger/MessageObject.java` 1 (метка `+PublicChat` журнала действий), `ChatActivity.java` 1 и `ChannelAdminLogActivity.java` 1 (`t.me/iv`); закомментированные строки `SecretMediaViewer.java` 2.

### A-104. Текст о публичных ссылках

- **Где.** `res/values/strings.xml`, `GroupDescription3` (пустой экран группы, `ChatBigEmptyView.java:138`): `Public links such as t.me/title` → `Public links such as pluma.chat/title` (`edit_string`, текст целиком). Ключ есть только в `values`; появится в другом языке — падение. Новых ключей нет.
- **Остаются** (`LINK_STRINGS_KEEP`): `TelegramFeaturesUrl`, `CocoonFeature1TextLink`, `CocoonFooter`, `CocoonFooterLink` (сервисы Telegram), `PublicLinksLimitSubtitle` (лист лимитов Premium, `DoubledLimitsBottomSheet.java:289`).

### A-105. Манифест: проверенные app links pluma.chat только для путей ссылок

- **Где.** `TMessagesProj/src/main/AndroidManifest.xml`. Фильтр `LaunchActivity` с t.me (`:257-267`) не меняется. После него новый `<intent-filter android:autoVerify="true">` (VIEW, DEFAULT, BROWSABLE) с комментарием `<!-- pluma: verified app links of pluma.chat, the links and not the site's pages -->`: `host="pluma.chat"` со `scheme` `http` и `https`, `pathPrefix` из `LINK_PATH_PREFIXES` (`/+`, `/joinchat/`, `/addstickers/`, `/addemoji/`, `/addlist/`, `/folder/`, `/c/`, `/s/`, `/m/`, `/call/`, `/boost`, `/proxy`, `/socks`, `/setlanguage/`, `/addtheme/`, `/bg/`, `/login/`, `/contact/`, `/share`) и два `pathAdvancedPattern` из `LINK_USERNAME_PATTERNS`: `/[A-Za-z][A-Za-z0-9_]{4,31}` и `/[A-Za-z][A-Za-z0-9_]{4,31}/.*` (юзернейм 5–32 символа и что угодно после него). Все `<data>` одного фильтра складываются (developer.android.com, `<data>`: «All the `<data>` elements … contribute to the same filter»), поэтому каждый путь действует для обеих схем.
- **Почему pluma.chat нет ни в одном фильтре без пути.** Система проверяет хосты из всех фильтров с VIEW, BROWSABLE, DEFAULT и http(s), а не только из фильтра с `autoVerify` («The system inspects all intent filters…», «For each unique hostname…», developer.android.com, Verify Android App Links), и состояние проверки ведётся по хосту (`pm get-app-links`). Запись `host="pluma.chat"` без пути в общем фильтре t.me, как было в первой версии этой правки, взяла бы весь сайт. Поэтому записи pluma.chat из фильтра t.me убраны (отступление от формулировки «оставить записи непроверенного фильтра»: записи t.me, telegram.me, telegram.dog остались как в апстриме).
- **Почему `pathAdvancedPattern`, а не `pathPattern`.** `minSdkVersion 21`, `targetSdkVersion 35`, `compileSdkVersion 35` (`TMessagesProj/build.gradle:86`, `:113-114`). `pathAdvancedPattern` появился в API 31 (developer.android.com, `<data>`: «`pathSuffix` and `pathAdvancedPattern` were introduced in API level 31»); на Android 11 и ниже атрибут пропускается, и система передаёт приложению только ссылки с префиксами, а ссылку на юзернейм из другого приложения — нет. `pathPattern` (все версии) не знает классов символов: шаблон «5 символов и больше» (`/......*`, `.` совпадает и с `/`) взял бы и `/en/iphone`, `/dl/…`, `/assets/…`. Выбор — точность на Android 12+ ценой ссылок на юзернейм на Android 11 и ниже (решение к подтверждению).
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

- **Что и зачем.** CMake 3.10 не берёт launcher из окружения, поэтому он в аргументах CMake. Только с `--ccache` (CI).
- **Где.** `TMessagesProj/build.gradle`, точная замена `arguments '-DANDROID_STL=c++_static', '-DANDROID_PLATFORM=android-21'` → с добавлением `'-DCMAKE_C_COMPILER_LAUNCHER=ccache', '-DCMAKE_CXX_COMPILER_LAUNCHER=ccache'`.
- **История.** `991d01b`, 2026-10-23, задача 006.

### A-067. Закрепление апстрима и его загрузка

- **Где.** `upstream.env` (см. «Общее»); `ci/fetch-upstream.sh`: `git init`, `fetch --depth 1 origin $UPSTREAM_REF` с таймаутом 10 мин и тремя попытками, `checkout FETCH_HEAD`, сверка `rev-parse HEAD`.
- **История.** `991d01b`, 2026-10-23, задача 006.

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

- **Где.** `ci/setup.sh build` — `ccache`, `age`, SDK `platforms;android-35`, `build-tools;35.0.0`, `ndk;27.2.12479018`, `cmake;3.10.2.4988404`; `CCACHE_COMPILERCHECK=string:<NDK>`. `ci/setup.sh sign` — `age` и build-tools. Повторы и таймауты.
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

### A-109. Premium для всех: звезда, строки покупки, расшифровка (задача 071)
- **Статус.** Ветка `task/071-premium-for-everyone` (`7d0df1b`), не влита, не выпущена (2026-11-09).
- **Что и зачем.** Сервер ставит флаг `premium` всем (ADR 0041). Ненужное от этого убираем, функции флаг открывает сам.
- **Где** (`PREMIUM_FOR_EVERYONE_EDITS` в `ci/patch.py`):
  - `messenger/MessagesController.isPremiumUser` возвращает `false`: звезды в `DialogCell`, `UserCell`, `ProfileSearchCell`, `DialogStoriesCell`, `DialogsActivity`, заголовке `ChatActivity`, `ProfileActivity`;
  - `ImageReceiver`, `ImageLocation` читают `user.premium`, анимация аватарок остаётся;
  - звезды прямых чтений: `SettingsActivity`, `ChatActivity:19346`, `ChatMessageCell`, `GroupCallUserCell`, `StatusBadgeComponent`, `TableView`;
  - строки Premium и Business: `SettingsActivity`, `ProfileActivity` — по `premiumPurchaseBlocked`;
  - кнопка расшифровки (`useTranscribeButton`).
- **Как проверяется.** `verify_premium_for_everyone`, `PremiumForEveryoneTest`.
- **При обновлении апстрима.** Новые места со звездой — по `user.premium` и `msg_premium_liststar`.

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
- A-014 и A-021 не ловят новые места апстрима с «Telegram» в литералах Java и новые строки поиска «Помощи»; A-005 не ловит новый источник резервных адресов вне `onRequestNewServerIpAndPort`; A-019 удалит без предупреждения новый пункт апстрима внутри блока «Помощь».
- Ветки: `origin/main` и `task/020-parity-fixes` на `d9ad7cf`; локальная `main` отстаёт (`7c78c00`). `sticker-set-kinds` = `origin/main` + `339fd8c`.
- A-099…A-106 (задача 026) проверены применением на копии апстрима `aac1efcb` и тестами `LinksTest` (A-105, A-106 ещё `aapt2 link` манифеста и `javac` блока Java); сборка CI (Gradle) прошла, выпуск `12.9.1.00031`; `assetlinks.json` с отпечатком подписи отдаётся на pluma.chat. Поведение на устройстве и проверка app links системой не проверены.
- Не сделано и правок нет: возврат «Помощи» (задача 028); перевод «Send Message» баннера на другие встроенные языки (A-056).
- A-086…A-092 проверены применением на копии апстрима и тестами скрипта; компиляция Java (Gradle) и поведение на устройстве не проверены.
