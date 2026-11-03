# Реестр правок iOS-клиента (pluma-ios)

Всё, что Pluma меняет в официальном Telegram-iOS: правки исходников, картинки, тексты, конфигурация сборки, скрипты и CI. Реестр нужен для обновления апстрима: каждая запись говорит, на что правка опирается и что сломается, если Telegram это место изменит.

Состояние на 2026-11-03, `main` pluma-ios = `74bf939`. Записи составлены по `ci/patch.py`, `ci/patch_test.py`, README, скриптам и всей истории репозитория (28 коммитов, `807e2a8..74bf939`), а также по брифам задач 006, 017, 020, 029, 033 в `docs/tasks`.

## Репозиторий и апстрим

- Репозиторий: `sckraaa/pluma-ios` (публичный), ветка `main`. Копии кода приложения в нём нет: только патч, картинки бренда, конфигурация и CI.
- Апстрим (`upstream.env`):
  - `UPSTREAM_REPO=https://github.com/TelegramMessenger/Telegram-iOS.git`
  - `UPSTREAM_TAG=release-12.9.2`, `UPSTREAM_REF=6ad963e5b62d354da79040f388ae2b9132fb17b8`
  - `UPSTREAM_VERSION=12.9.2`, `UPSTREAM_LAYER=228`
  - `XCODE_VERSION=26.2`, `BAZEL_VERSION=8.4.2` (из `versions.json` апстрима)
- Приложение (`config/app.env`): `PLUMA_BUNDLE_ID=chat.pluma.app`, `PLUMA_APP_NAME=Pluma`, `PLUMA_URL_SCHEME=plm`, `PLUMA_API_ID=10002`, `PLUMA_API_HASH` (не секрет, его несёт каждый клиент), `PLUMA_SIM_CONFIGURATION=debug_sim_arm64`.

## Как накладывается патч

- Точка входа: `ci/configure.sh dev|server` вызывает `python3 -I ci/patch.py --source <checkout> --upstream upstream.env --app config/app.env --server-ipv4 … --server-port … [--server-ipv6 …] --rsa-public-key <pem> --team-id …`.
  - `dev`: сервер и ключ из `config/dev` (`127.0.0.1:10443`, `server_public.pem` — публичный dev-ключ), Team ID-заглушка из `config/dev/server.env` (Make.py требует Team ID и для симулятора).
  - `server`: переменные репозитория `PLUMA_SERVER_IPV4`, `PLUMA_SERVER_PORT`, `PLUMA_SERVER_IPV6`, `PLUMA_RSA_PUBLIC_KEY`, `PLUMA_TEAM_ID`.
- Исходники: `ci/fetch-upstream.sh sparse` (только файлы из `ci/patch.py --paths`, для проверок) или `full` (со всеми сабмодулями, для сборки). В CI сборка идёт в `/Users/Shared/telegram-ios`.
- `patch.py` работает в два шага: `patch()` делает правки, `verify()` проверяет результат независимо от того, как он получен. Любое расхождение — `PatchError` с именем файла и ожидаемым текстом, сборка останавливается.
- Виды правок (класс `Tree` в `patch.py`):
  - `replace` — точная замена текста, число вхождений задано (обычно 1);
  - `replace_lines` — то же, но пустые строки шаблона совпадают с любым отступом (Xcode оставляет в них пробелы);
  - `replace_in_line` — правки внутри единственной строки, найденной по якорю;
  - `replace_body` — замена тела между `head` и первым `tail`, с обязательным содержимым (`must_contain`);
  - `replace_block` — замена `head`, `tail` и всего между ними, тоже с `must_contain`;
  - `replace_file` / `remove_file` — замена или удаление файла после сверки SHA-256 с закреплённым коммитом;
  - `add_file` — новый файл; если апстрим уже имеет такой, патч падает;
  - `expect` — правки нет, но текст обязан быть ровно в таком количестве (на него опирается сборка или другая правка).
- Сборка: `ci/build.sh sim|ipa|appstore` (Make.py апстрима, `--configurationPath=build-system/pluma-configuration.json`).

## Как запускать тесты

```sh
ci/fetch-upstream.sh sparse                       # ./upstream, секунды
python3 -I -m unittest discover -s ci -p '*_test.py' -v
ci/configure.sh dev                               # патч на dev-сервер, с verify()
ci/lint.sh                                        # shellcheck и actionlint
```

Без `./upstream` 7 тестов класса `UpstreamTest` пропускаются (в CI не пропускаются никогда). Проверено 2026-11-03 на свежем клоне: 61 тест, все проходят, с апстримом и без пропусков.

Основные тесты `ci/patch_test.py`:
- `UpstreamTest.test_patch` — патч на закреплённом коммите и проверка каждого результата;
- `UpstreamTest.test_damage` — таблица `DAMAGE`: для каждой якорной строки портится апстрим, патч обязан упасть с именем файла;
- `UpstreamTest.test_damaged_images` — изменённая или пропавшая картинка апстрима, уже существующий файл Pluma;
- `UpstreamTest.test_patched_tree_is_refused` — повторный патч того же дерева падает;
- `UpstreamTest.test_intro_without_wordmark`, `test_intro_without_mark` — `verify()` ловит откат правок вступления;
- `UpstreamTest.test_dev_server` — патч на dev-конфигурацию;
- `KeyTest`, `ConfigTest`, `ScanTest`, `TreeTest`, `RenameTest`, `BrandTest`, `ClientLinksTest`, `BirthdayTest`, `IdentityTest` — по областям (ниже в записях).

Тесты подписи — `ci/preflight_sign_test.py`, кэша — `ci/cache_gc_test.py`.

## Что проверять при обновлении апстрима

1. Новый тег в `upstream.env`: тег, коммит, версия, слой (только тот, что говорит сервер), Xcode и Bazel из `versions.json`. Есть ли образ `macos-*` с этим Xcode (`ci/setup.sh` падает без `/Applications/Xcode_<версия>.app`).
2. `ci/fetch-upstream.sh sparse`, затем тесты. Каждое падение называет файл и текст — по ID записи ниже понять, зачем правка, и перенести её на новый код, а не ослабить проверку.
3. Счётчики, которые почти наверняка изменятся с любым релизом:
   - `STRINGS_RENAMED = (556, 609)` — записи и вхождения «Telegram» в `en.lproj/Localizable.strings` (I-033);
   - `INFOPLIST_NAMES` — число вхождений имени в каждом `InfoPlist.strings` (I-034);
   - SHA-256 всех картинок в `BRAND_REPLACED`, `BRAND_REMOVED`, `CAMERA_FONTS`, `SIRI_UPSTREAM` (I-015…I-023, I-061, I-086);
   - `count=` в `PALETTE_EDITS` (I-042, I-043), счётчики `CFBundleName` (7) и `CFBundleDisplayName` (2) в `Telegram/BUILD` (I-032).
4. Новые места, которые патч не знает, но должен покрыть (их не поймает ни одна проверка, кроме `verify()` сетевого кода):
   - новые адреса или ключи Telegram в MtProtoKit, TelegramCore, TelegramVoip — ловит `verify()` (I-012);
   - новые ссылки `tg://`, которые приложение создаёт, вне `SCHEME_LINKS` (I-039);
   - новые места с синим Telegram в темах по умолчанию вне `TELEGRAM_BLUES` (I-042…I-047);
   - новые таблицы семи цветов-идентичностей (I-048);
   - новые предложения Premium-подарков или покупок, которые надо скрыть при `premium_purchase_blocked` (I-053…I-060);
   - новые внешние сервисы Telegram (DNS-over-HTTPS, CloudKit, отражатели звонков) (I-004…I-011).
5. `Make.py` и `BuildConfiguration.py`: ключи `required_keys` (патч пишет ровно их, I-065), опции и конфигурации, на которые опирается `ci/build.sh` (I-064).
6. Минимальная iOS: App Store Connect требует iOS 15 с апреля 2027 (ITMS-90068). Если новый релиз поднял `minimum_os_version`, предупреждение уходит само; если нет — см. I-084.
7. После сборки — проверка на симуляторе: вход, список чатов, обе темы, вступление, «Устройства», баннер дня рождения, меню своего сообщения. При установке поверх старой сборки обновлять даты файлов (`find Pluma.app -exec touch {} +`), иначе `simctl install` оставляет старые фреймворки (I-078).
8. Сравнение с официальным клиентом: workflow `reference` собирает тот же тег без патча (I-081).

---

## Сервер и ключ

### I-001. Один датацентр: сервер Pluma
- **Что и зачем.** Список начальных адресов MtProtoKit — один DC 1 с адресами сервера (IPv4, необязательный IPv6) и его портом, одинаково для боевого и тестового окружения. Клиент не должен знать ни одного адреса Telegram.
- **Где.** `submodules/TelegramCore/Sources/Network/Network.swift`, блок `SEED_OLD` (от `let seedAddressList: [Int: [String]]` через `if testingEnvironment { … } else { … }` до цикла `context.setSeedAddressSetForDatacenterWithId(id, …port: 443…)`). `replace_lines`, отступ 12.
- **Проверка.** `verify()`: в `Network.swift` ровно один `setSeedAddressSetForDatacenterWithId(`, нет `seedAddressList = `, есть строка `1: [<адреса>]` и `MTDatacenterAddress(ip: $0, port: <порт>, `. `UpstreamTest.test_patch` (IPv6 и порт 8443), `test_damage` (5 порч `NETWORK`).
- **Если якорь изменится.** Патч падает на `Network.swift`. Если Telegram перенёс список адресов в другой файл, `verify()` всё равно найдёт адреса Telegram в сетевом коде (I-012).
- **Апстрим.** Тест: DC 1–3 `149.154.175.10`, `149.154.167.40`, `149.154.175.117`; боевые: DC 1–5 с IPv4 и IPv6 Telegram, порт 443. Официальный Telegram — так же.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-002. Свежая установка начинает с DC 1
- **Что и зачем.** Новый аккаунт стартует на датацентре 1 (сервер), а не на 2.
- **Где.** `submodules/TelegramCore/Sources/Account/Account.swift`, `INITIAL_DC_OLD`: `#if DEBUG / let initialDatacenterId: Int = 1 / #else / let initialDatacenterId: Int = 2 / #endif` (отступ 24). `replace_lines`.
- **Проверка.** `verify()`: есть `let initialDatacenterId: Int = 1\n`, нет `initialDatacenterId: Int = 2`. `test_damage` (`ACCOUNT`).
- **Апстрим.** В релизной сборке DC 2. Официальный — DC 2.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-003. Ключи заранее — только для DC 1
- **Что и зачем.** Клиент заранее делает ключи авторизации только для сервера.
- **Где.** `Account.swift`, `WARMUP_OLD`: `var datacenterIds: [Int] = [1, 2]` с веткой `testingEnvironment → [3]`, иначе `append(contentsOf: [4])`. Заменяется на `let datacenterIds: [Int] = [1]`. `replace_lines`.
- **Проверка.** `test_patch` (`let datacenterIds: [Int] = [1]`), `test_damage` (`ACCOUNT`).
- **Апстрим.** Боевые DC 1, 2, 4; тест DC 3. Официальный — так же.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-004. Нет резервных адресов из CloudKit
- **Что и зачем.** Telegram публикует резервные адреса в CloudKit; они указывают на Telegram. Источник `wrappedAdditionalSource` убран.
- **Где.** `Network.swift`, `CLOUD_SOURCE_OLD` (от `var wrappedAdditionalSource: MTSignal?` через `#if os(iOS) … makeCloudDataContext … #endif`). Заменяется комментарием. `replace_lines`.
- **Проверка.** `test_patch` (`= makeCloudDataContext(` и `wrappedAdditionalSource` отсутствуют). Плюс `enable_icloud: false` (I-065).
- **Апстрим.** При включённом iCloud клиент читает записи CloudKit. Официальный — так же.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-005. Нет поиска резервных адресов
- **Что и зачем.** Вызов `context.setDiscoverBackupAddressListSignal(MTBackupAddressSignals.fetchBackupIps(…))` убран целиком.
- **Где.** `Network.swift`, `DISCOVER_OLD` (одна строка с отступом 16). `replace`.
- **Проверка.** `verify()`: в `Network.swift` нет `fetchBackupIps(` и `setDiscoverBackupAddressListSignal(`; ни один `.swift` сетевого кода не вызывает `fetchBackupIps(`.
- **Апстрим.** При недоступности DC клиент ищет адреса через DNS-over-HTTPS Google и CloudKit. Официальный — так же.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-006. Библиотечная функция резервных адресов ничего не возвращает
- **Что и зачем.** Защита на случай нового вызова: `fetchBackupIps` в MtProtoKit сразу возвращает `[MTSignal complete]`.
- **Где.** `submodules/MtProtoKit/Sources/MTBackupAddressSignals.m`, `replace_body` между `BACKUP_HEAD` (`+ (MTSignal * _Nonnull)fetchBackupIps:(bool)isTestingEnvironment currentContext:… mainDatacenterId:(NSInteger)mainDatacenterId {\n`) и `\n}\n\n@end\n`; в заменяемом теле обязаны быть `fetchBackupIpsResolveGoogle:` и `fetchConfigFromAddress:`.
- **Проверка.** `verify()`: есть `[MTSignal complete];\n}\n\n@end\n`. `test_damage` (2 порчи `BACKUP`).
- **Если якорь изменится.** Если метод перестал быть последним в файле или сменил сигнатуру — падение.
- **Апстрим.** DNS-over-HTTPS к Google за TXT-записями Telegram, проверка подписи ключом из `MTEncryption.m`. Официальный — так же.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-007. Имена прокси — системным резолвером
- **Что и зачем.** Имя хоста прокси разрешается только системой, без DNS-over-HTTPS у Google.
- **Где.** `submodules/MtProtoKit/Sources/MTDNS.m`, `DNS_OLD`: `return [[self resolveHostname:hostname] timeout:10.0 onQueue:… orSignal:[self resolveHostnameNative:hostname port:port]];` → `return [self resolveHostnameNative:hostname port:port];`. `replace`.
- **Проверка.** `test_patch`, `test_damage` (`DNS`).
- **Апстрим.** Сначала Google DoH, через 10 с системный резолвер. Официальный — так же.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-008. RSA-ключ сервера вместо ключей Telegram
- **Что и зачем.** Боевой и тестовый ключи MtProtoKit заменены ключом сервера (PKCS#1, 2048 бит, e = 65537). Клиент выбирает ключ по отпечатку, который объявляет сервер.
- **Где.** `submodules/MtProtoKit/Sources/MTDatacenterAuthMessageService.m`, `replace_block` от `    dispatch_once(&onceToken, ^{\n        testingPublicKeys = @[\n` до `    });\n`; в блоке обязаны быть первые строки ключей Telegram (`TELEGRAM_KEY_LINES[:2]`). Ключ вписывается как Objective-C-литерал (`objc_key`, отступ 13).
- **Проверка.** `verify()`: ключей в сетевом коде ровно 3, все — ключ сервера; строк ключей Telegram нет. `KeyTest` (отпечатки `0xD09D1D85DE64FD85`, `0xB25898DF208D2603`, dev-ключ, литерал), `ConfigTest.test_rejects` (не 2048 бит, ключ Telegram), `test_damage` (2 порчи `AUTH_KEYS`, в том числе подложенный ключ Telegram в комментарии), порча `MTContext.m` с лишним ключом.
- **Апстрим.** Боевой и тестовый ключи Telegram. Официальный — так же.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-009. Ключ записей резервных адресов — тоже ключ сервера
- **Что и зачем.** В `MTEncryption.m` хранится ключ, которым Telegram подписывает резервные записи. Записи выключены (I-005, I-006), но ни одного ключа Telegram в клиенте не остаётся.
- **Где.** `submodules/MtProtoKit/Sources/MTEncryption.m`, `replace_block` от `    NSString *keyString = @"-----BEGIN RSA PUBLIC KEY-----\n"\n` до `"-----END RSA PUBLIC KEY-----";\n`; обязателен `TELEGRAM_KEY_LINES[2]`.
- **Проверка.** `verify()` (3 ключа), `test_patch` (один ключ сервера в `MTEncryption.m`), `test_damage` (`ENCRYPTION`).
- **Апстрим.** Ключ Telegram для резервных записей. Официальный — так же.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-010. Звонки: без лишнего TCP-отражателя Telegram
- **Что и зачем.** Для протокола звонков 12.0.0 апстрим добавляет к каждому отражателю свой TCP-отражатель `91.108.9.38:595`. Pluma использует только отражатели сервера.
- **Где.** `submodules/TelegramVoip/Sources/OngoingCallContext.swift`, `CALL_REFLECTOR_OLD` (от `var unfilteredConnections: [CallSessionConnection]` до конца `if version == "12.0.0" { … }`). Заменяется на `let unfilteredConnections = [connections.primary] + connections.alternatives`. `replace_lines`, отступ 16.
- **Проверка.** `verify()` (адрес Telegram в сетевом коде), `test_patch` (`CallSessionConnection.Reflector(` нет), `test_damage` (2 порчи `CALLS`).
- **Апстрим.** Отражатель Telegram добавлен. Официальный — так же.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-011. Бенчмарк отражателя не запускается
- **Что и зачем.** В `AppDelegate` TelegramUI есть бенчмарк отражателя Telegram. Он работает только во внутренней сборке или за скрытым переключателем отладки. Патч его не правит: сборка публичная (`is_internal_build: false`, I-065).
- **Где.** Правки нет. Опора — ключ конфигурации.
- **Проверка.** Только через I-065. Отдельной проверки нет.
- **Апстрим.** В официальной App Store-сборке тоже выключен (внутренняя сборка у Telegram своя).
- **История.** `807e2a8`, 2026-10-27 (комментарий в `build_configuration`).

### I-012. Итоговая проверка сетевого кода
- **Что и зачем.** Не правка, а страховка: после всех правок в `submodules/MtProtoKit`, `submodules/TelegramCore/Sources`, `submodules/TelegramVoip/Sources` (файлы `.swift .m .h .mm .c .cpp`) нет адресов из диапазонов Telegram (`TELEGRAM_NETWORKS`: опубликованный `core.telegram.org/resources/cidr.txt` плюс `95.161.76.100/32`), нет строк ключей Telegram, все PEM-ключи — ключ сервера, ключей ровно 3.
- **Где.** `verify()` в `ci/patch.py`.
- **Проверка.** `ScanTest` (IPv4/IPv6, литералы, код вокруг маркеров в `MTRsa.m`), `ConfigTest.test_rejects` (адрес сервера в диапазонах Telegram).
- **Если апстрим изменится.** Новый адрес или ключ Telegram в любом файле этих каталогов останавливает сборку. Список диапазонов в `TELEGRAM_NETWORKS` надо сверять с `cidr.txt` при обновлении.
- **Чего проверка не видит.** Адреса и ключи вне трёх каталогов. Подключения по именам хостов (например, `google.com` для пинга прокси, I-089).
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-013. Адрес, порт и ключ сервера при сборке
- **Что и зачем.** Адрес сервера не зашит в репозиторий, кроме dev-стенда. `Config` в `patch.py` проверяет: IPv4 — адрес, а не имя (MtProtoKit принимает только адрес); IPv6 необязателен; адреса не из диапазонов Telegram; порт 1–65535; ключ — PKCS#1 PEM, 2048 бит, e = 65537 (MtProtoKit приклеивает заголовок 2048-битного ключа в `MTRsa.m`), отпечаток не Telegram.
- **Где.** `config/dev/server.env` (`PLUMA_SERVER_IPV4`, `PLUMA_SERVER_PORT`, `PLUMA_TEAM_ID`), `config/dev/server_public.pem`, переменные репозитория для `server` (I-072). `ci/configure.sh`.
- **Проверка.** `ConfigTest.test_repository_config`, `test_dev_server`, `test_rejects`; `KeyTest.test_dev_key_fingerprint`.
- **История.** `807e2a8`, 2026-10-27, задача 006.

---

## Брендинг и тексты

Источник бренда — набор задачи 017 (знак, палитра «графит и шалфей»). Картинки рендерит `brand/render.sh` (I-085) в `brand/ios`, каждая под путём заменяемого файла апстрима.

### I-014. Набор файлов бренда совпадает со списками патча
- **Что и зачем.** `patch_brand` сначала сверяет, что `brand/ios` содержит ровно `BRAND_REPLACED ∪ BRAND_ADDED`. Новый или переименованный файл без записи в списках останавливает патч.
- **Где.** `ci/patch.py`, `brand_files()`, `patch_brand()`.
- **Проверка.** `BrandTest.test_brand_files_are_the_patch_list`, `test_upstream_paths_cover_the_brand`; `verify_brand` сверяет SHA-256 каждого файла в дереве с `brand/ios`.
- **История.** `8c50bc6`, 2026-10-29, задача 017.

### I-015. Иконка приложения (Icon Composer)
- **Что и зачем.** Иконка — знак на белом, светлый знак на графите в тёмном виде, тонированный и прозрачный виды iOS 26 получаются из слоёв.
- **Где.** `Telegram/Telegram-iOS/Telegram.icon/icon.json` — `replace_file` (SHA-256 апстрима `cf7c84b1…4c89`). Добавлены (`add_file`) слои `Telegram.icon/Assets/Feather1.png`, `Feather2.png`, `Feather3.png`, `Ring.png`. Удалены (`remove_file`) слои Telegram `Assets/Oval.svg` (`30b7245c…`) и `Assets/Plane.svg` (`f4da98d5…`).
- **Проверка.** `verify_brand` (хэши, удалённые файлы отсутствуют), `test_damaged_images` (порча и пропажа `Oval.svg`, уже существующий `Feather1.png`).
- **Если апстрим изменит.** Любая правка иконки Telegram (`icon.json` или слоёв) меняет хэш — патч падает; новый слой апстрима останется в иконке, если его не добавить в `BRAND_REMOVED`.
- **Апстрим / официальный.** Бумажный самолёт на синем.
- **История.** Рендер `512ace1`, замена `8c50bc6`, 2026-10-29, задача 017.

### I-016. Альтернативная иконка BlueIcon — иконка Pluma
- **Что и зачем.** Приложение считает `BlueIcon` своей иконкой по умолчанию (выбор иконки, тип файла темы). Её 8 картинок — знак Pluma.
- **Где.** `Telegram/Telegram-iOS/BlueIcon.alticon/BlueIcon@2x.png`, `BlueIcon@3x.png`, `BlueIconIpad.png`, `BlueIconIpad@2x.png`, `BlueIconLargeIpad@2x.png`, `BlueNotificationIcon.png`, `BlueNotificationIcon@2x.png`, `BlueNotificationIcon@3x.png`. `replace_file` по SHA-256.
- **Проверка.** `verify_brand`, `test_damaged_images` (`BlueIcon@3x.png`).
- **Апстрим / официальный.** Синяя иконка Telegram.
- **История.** `512ace1`, `8c50bc6`, 2026-10-29, задача 017.

### I-017. Только одна альтернативная иконка в бандле
- **Что и зачем.** Остальные альтернативные иконки — Telegram (включая Premium): бандл их не несёт.
- **Где.** `Telegram/BUILD`, `ALTERNATE_ICONS_OLD` — список `alternate_icon_folders` из 12 папок (`BlackIcon`, `BlackClassicIcon`, `BlackFilledIcon`, `BlueIcon`, `BlueClassicIcon`, `BlueFilledIcon`, `WhiteFilledIcon`, `New1`, `New2`, `Premium`, `PremiumBlack`, `PremiumTurbo`) → только `"BlueIcon"`. `replace`.
- **Проверка.** `verify_brand` (список ровно `["BlueIcon"]`), `test_patch`.
- **Если апстрим изменит.** Новая иконка в списке — падение (текст списка точный).
- **Апстрим / официальный.** 12 иконок, Premium-иконки для подписчиков.
- **История.** `8c50bc6`, 2026-10-29, задача 017.

### I-018. Выбор иконки предлагает только иконку Pluma
- **Где.** `submodules/TelegramUI/Sources/AppDelegate.swift`, `APP_ICONS_OLD` (от `var icons = [` с 8 `PresentationAppIcon(…)` через `if buildConfig.isInternalBuild { … WhiteFilledIcon }` и три Premium-иконки до `return icons`). Замена: `return [PresentationAppIcon(name: "BlueIcon", imageName: "BlueIcon", isDefault: buildConfig.isAppStoreBuild)]`. `replace_lines`, отступ 16.
- **Проверка.** `verify_brand` (в `AppDelegate.swift` ровно один `PresentationAppIcon(`), `test_damage` (`APP_DELEGATE`).
- **Апстрим / официальный.** 8 иконок плюс 3 для Premium.
- **История.** `8c50bc6`, 2026-10-29, задача 017.

### I-019. Логотип быстрого действия и пункта контекстного меню
- **Где.** `Telegram/Telegram-iOS/Icons.xcassets/Shortcuts/AppIcon.imageset/tlogo_24.pdf` и `submodules/TelegramUI/Images.xcassets/Chat/Context Menu/Telegram.imageset/tlogo_24.pdf` (у обоих SHA-256 апстрима `78718646…ea3b`). `replace_file`. Знак в скруглённом квадрате, шаблон.
- **Проверка.** `verify_brand`, `test_damaged_images` (`Context Menu/Telegram.imageset/tlogo_24.pdf`).
- **История.** `512ace1`, `8c50bc6`, 2026-10-29, задача 017.

### I-020. Логотипы звонков
- **Где.** `submodules/TelegramUI/Images.xcassets/Call/CallTitleLogo.imageset/ic_calls_tlogo.pdf` (заголовок экрана звонка), `Call/CallKitLogo.imageset/CallKitLogo@2x.png`, `CallKitLogo@3x.png` (кнопка приложения в системном экране звонка). `replace_file`.
- **Проверка.** `verify_brand`.
- **История.** `512ace1`, `8c50bc6`, 2026-10-29, задача 017.

### I-021. Логотипы QR-кодов и их анимации
- **Где.** `Images.xcassets/Chat/Links/QrLogo.imageset/ic_qrlogo.pdf` (графитовый круг с белым знаком), `Images.xcassets/Share/QrPlaneIcon.imageset/ic_qrlogo.pdf` (белый знак), `Telegram/Telegram-iOS/Resources/PlaneLogo.tgs`, `PlaneLogoPlain.tgs` (неподвижные Lottie из `brand/tgs.py`). `replace_file`.
- **Проверка.** `verify_brand`, `test_damaged_images` (`PlaneLogo.tgs`).
- **Апстрим / официальный.** Самолёт Telegram, анимированный.
- **История.** `512ace1`, `8c50bc6`, 2026-10-29, задача 017.

### I-022. Ноутбук в «Настройки → Устройства»
- **Что и зачем.** Анимация заголовка `RecentSessionsController`: анимация Telegram, перекрашенная в палитру, знак Pluma вместо самолёта на крышке и в QR-коде.
- **Где.** `Telegram/Telegram-iOS/Resources/Devices.tgs` (`9abb2d5d…85fe`), `replace_file`. Источник — `brand/pluma-devices.tgs`, копируется `render.sh` без перерисовки.
- **Проверка.** `verify_brand`, `BrandTest.test_devices_animation`, `test_damaged_images` (`Devices.tgs`).
- **Если апстрим изменит.** Новая анимация Telegram меняет хэш — `pluma-devices.tgs` надо переделать с новой.
- **История.** `b0586ec`, 2026-10-30, задача 017.

### I-023. Текстуры шара и самолёта вступления — прозрачные
- **Что и зачем.** Первая страница вступления рисует знак отдельным видом (I-026). Текстуры шара и самолёта прозрачные, тех же размеров. Вступление заливает текстуру шара цветом фона (I-025), и шар остаётся квадратом цвета фона, который прячет спидометр следующей страницы.
- **Где.** `Telegram/Telegram-iOS/Resources/intro/telegram_sphere@2x.png` (`621831c4…`), `telegram_plane1@2x.png` (`c28d4e54…`). `replace_file`.
- **Проверка.** `verify_brand`, `test_damaged_images` (`telegram_sphere@2x.png`).
- **История.** `512ace1` (знак на белом диске, без самолёта) → `6101991` (диск по формуле Android, 0,535) → `8314118` (прозрачная текстура), 2026-10-29…31, задачи 017 и 020.

### I-024. Цвета страниц «Powerful» и «Cloud-Based»
- **Где.** `submodules/RMIntro/Sources/core/animations.c`: `vec4 powerful_bg_color = {47/255., 90/255., 131/255., 1};` → `{79/255., 110/255., 82/255., 1}` (`#4F6E52`); `vec4 cloud_color = {42/255., 180/255., 247/255., 1};` → `{111/255., 143/255., 114/255., 1}` (`#6F8F72`). `replace`.
- **Проверка.** `test_patch`, `test_damage` (`INTRO`).
- **Апстрим / официальный.** Синие.
- **Не сделано.** Ободок спидометра страницы «Fast» остаётся тёмно-синим (бриф 020, замечание без решения).
- **История.** `8c50bc6`, 2026-10-29, задача 017.

### I-025. Строки вступления, на которые опирается маска шара (только закрепление)
- **Что и зачем.** Правки нет: патч требует, чтобы эти строки апстрима остались как есть, иначе спидометр следующей страницы будет виден под знаком.
- **Где.** `INTRO_SPHERE_MASK`, `expect`:
  - `submodules/RMIntro/Sources/platform/ios/texture_helper.m`: `if ([fileName isEqualToString:@"telegram_sphere.png"]) { CGContextSetFillColorWithColor(…); CGContextFillRect(…); }`;
  - `RMIntroViewController.m`: `UIColor *color = _backgroundColor;`, `set_telegram_textures(setup_texture(@"telegram_sphere.png", color), `;
  - `animations.c`: `telegram_sphere = create_textured_rectangle(CSizeMake(148, 148), …)`, `fast_body = create_textured_rectangle(CSizeMake(148, 148), …)`, блоки `// LAYER0 - PREPARATION` и `// LAYER4 - DETAILS` для `current_page == 0` (прозрачность шара `t(0, 1, 0, duration_const*.8, Linear)`, масштаб 1.005).
- **Проверка.** `test_patch` (строки остались), `test_damage` (`INTRO_TEXTURES` ×2, `INTRO` и др.).
- **История.** `8314118`, 2026-10-31, задача 020.

### I-026. Вступление: знак пишет себя на первой странице
- **Что и зачем.** Первая страница показывает знак, который рисует себя пером, как на сайте Pluma (`writeMark`), над словесным знаком; без диска и самолёта. Lottie-файл `pluma_mark_write.json` (из `brand/mark_write.py`), тот же, что у Android. Хост вступления проигрывает его плеером приложения (rlottie, `ManagedAnimationNode`); в тёмных темах — заменой цветов. С Reduce Motion знак показывается готовым.
- **Где.** Добавлен файл `Telegram/Telegram-iOS/Resources/intro/pluma_mark_write.json` (`add_file`). Правки кода — I-027…I-029.
- **Проверка.** `verify_intro_mark`, `BrandTest.test_intro_mark` (хэши `mark_write.py` и анимации закреплены: `MARK_WRITE_SHA256`, `INTRO_MARK_SHA256`), `test_intro_without_mark`.
- **История.** `8314118`, 2026-10-31, задача 020.

### I-027. Вступление: заголовок RMIntroViewController — свойства для знака
- **Где.** `submodules/RMIntro/PublicHeaders/RMIntro/RMIntroViewController.h`, после якоря `@property (nonatomic, copy) UIView *(^createStartButton)(CGFloat);` добавлены `firstPageMarkView`, `firstPageMarkAppeared`, `topInset`, класс-свойство `firstPageMarkSize`. `replace` (якорь + добавка).
- **Проверка.** `verify_intro_mark` (ровно одно `firstPageMarkView`), `test_intro_without_mark`, `test_damage` (`INTRO_HEADER`).
- **История.** `8314118`, 2026-10-31, задача 020.

### I-028. Вступление: правки RMIntroViewController.m
Все в `submodules/RMIntro/Sources/platform/ios/RMIntroViewController.m`, все `replace` (якорь плюс добавка), кроме раскладки (`replace_lines`):
- **Константы раскладки** после `} DeviceScreen;`: `PlumaIntroMarkHeight = 128`, `PlumaIntroMarkGap = 32`, `PlumaIntroGroupBias = 0.44`, геометрия холста знака (512, чернила x 79.6–433.1, y 99.95–405.15), `PlumaIntroMarkSide()`.
- **Поля** вместо `    bool _loadedView;\n}\n@end\n`: контейнер знака, флаги «на экране», «показан», «ждёт записи».
- **animateIn** (`INTRO_ANIMATE_IN`, 5 якорей вокруг `_glkView.transform`, `_glkView.alpha`, `_startButton.alpha = 1.0; } completion:nil];`): знак входит вместе с GL-видом и через 0,15 с начинает писать себя.
- **GL-вид под всем**: `[self.view addSubview:_glkView];` → `[self.view insertSubview:_glkView atIndex:0];` (при повторном создании вида после ухода вступления непрозрачный GL-вид иначе ложится поверх заголовков и знака).
- **Контейнер знака** перед концом `loadView` (якорь `    [_wrapperView addSubview:_pageControl];\n}\n`): между GL-видом и страницами, скрыт от VoiceOver.
- **Снимок для анимации перехода** (`- (UIView *)createAnimationSnapshot {`): на первой странице снимок — знак.
- **Раскладка** `updateLayout` (`INTRO_LAYOUT_OLD`: `isVertical`, высоты по `DeviceScreen` для iPad, iPadPro, Inch35…Inch65): заменена на группу «знак + словесный знак + текст + точки» на 44 % свободной высоты между верхним отступом и кнопкой «Start»; кнопка остаётся на месте апстрима; картинки других страниц заканчиваются там, где знак.
- **viewWillAppear / viewDidDisappear** (`INTRO_APPEAR_OLD`): знак показывается при возвращении и прячется при уходе.
- **Прокрутка** (`    [_pageControl setCurrentPage:_currentPage];\n}\n`): знак следует за прокруткой.
- **Методы** перед `\n@end\n\n@implementation TGAvailableLocalization\n`: `updateFirstPageMarkAnimated:` (появление 0,24 с, исчезновение 0,3 с — время шара апстрима; при прокрутке гаснет к середине пути), `setTopInset:`, `firstPageMarkSize`.
- **Проверка.** `verify_intro_mark` (константы и раскладка ровно один раз, нет `isVertical`, три вызова `[self updateFirstPageMarkAnimated:`, нет `[self.view addSubview:_glkView];`), `BrandTest.test_intro_mark`, `test_intro_without_mark`, `test_damage` (17 порч `INTRO_CONTROLLER`).
- **Апстрим / официальный.** Шар с самолётом, фиксированные высоты по размерам экрана, на новых телефонах пустота между точками и кнопкой.
- **История.** `8314118`, 2026-10-31, задача 020.

### I-029. Вступление: хост знака в AuthorizationSequenceSplashController
- **Где.** `submodules/AuthorizationUI/Sources/AuthorizationSequenceSplashController.swift`, `INTRO_SPLASH_EDITS` (6 якорей, `replace` якорь + добавка): `import ManagedAnimationNode` после `import RMIntro`; поле `markNode`; создание `IntroMarkNode(size:dark: theme.overallDarkAppearance)` после `suggestedLocalizationSignal: localizationSignal)`; передача `firstPageMarkView` и `firstPageMarkAppeared` после `return self?.startButton.view }`; `topInset = max(layout.safeInsets.top, layout.statusBarHeight ?? 0.0)` после `self.controller.defaultFrame = controllerFrame`; класс `IntroMarkNode: ManagedAnimationNode` в конце файла (замена цветов `0x111111→0xe8ece8`, `0x4a4a4a→0xbdbdbd`, `0xbdbdbd→0x6b6f6b` в тёмной теме; `advanceState` держит последний кадр).
- **Проверка.** `verify_intro_mark` (`self.controller.firstPageMarkView = self.markNode.view`, `ManagedAnimationSource.local("pluma_mark_write")`), `test_intro_without_mark`, `test_damage` (6 порч `INTRO_SPLASH`).
- **Зависимость.** Опирается на API `ManagedAnimationNode` (`trackTo`, `trackStack`, `state.item.frames`, `ManagedAnimationItem(source:replaceColors:frames:duration:)`) — его изменение даст ошибку компиляции, а не патча.
- **История.** `8314118`, 2026-10-31, задача 020.

### I-030. Вступление: словесный знак как заголовок первой страницы
- **Что и зачем.** Как на Android (`IntroActivity`, ImageSpan над `Page1Title`): заголовок — словесный знак «pluma» цветом текста на 90 %, базовая линия на линии заголовка. Метка остаётся, VoiceOver читает имя.
- **Где.** `RMIntroViewController.m`: проверка `_headlines = @[ _englishStrings[@"Tour.Title1"], ` (`expect`); вставка `INTRO_WORDMARK` между `[_pageScrollView addSubview:p];\n    }\n` и `    [_pageScrollView setPage:0];\n` (`replace`). Добавлены файлы `intro/pluma_wordmark@2x.png`, `@3x.png` (`add_file`).
- **Проверка.** `verify_brand` (ровно один `[UIImage imageNamed:@"pluma_wordmark.png"]`), `BrandTest.test_intro`, `test_intro_without_wordmark`, `test_damaged_images` (существующий `pluma_wordmark@2x.png`).
- **История.** `6101991`, 2026-10-31, задача 020.

### I-031. Вступление: GL-анимация и в arm64-симуляторе
- **Что и зачем.** Апстрим в `loadGL` сразу выходит при `TARGET_OS_SIMULATOR && defined(__aarch64__)`, без объяснения, и страницы вступления в симуляторе пусты. Симуляторные сборки Pluma только arm64 и вступление проверяется в них; на устройствах код тот же.
- **Где.** `RMIntroViewController.m`, `INTRO_GL_OLD` (`- (void)loadGL\n{\n#if TARGET_OS_SIMULATOR && defined(__aarch64__)\n    return;\n#endif\n`) → `INTRO_GL`. `replace`.
- **Проверка.** `verify_brand` (нет `TARGET_OS_SIMULATOR`), `test_patch`.
- **Апстрим / официальный.** Официальный 12.9.2 в симуляторе iOS 26.4 тоже ничего не рисует (сборка `reference`, бриф 020). На устройствах поведение не меняется.
- **История.** `7edb844`, 2026-10-31, задача 020.

### I-032. Имя под иконкой и в меню «Поделиться»
- **Где.** `Telegram/BUILD`: `<key>CFBundleDisplayName</key>\n    <string>Telegram</string>` → `Pluma` (2 вхождения), `<key>CFBundleName</key>\n    <string>Telegram</string>` → `Pluma` (7 вхождений: приложение и расширения). `replace` с `count`.
- **Проверка.** `verify()` (в `BUILD` нет `<string>Telegram</string>`), `test_patch` (`<string>Pluma</string>` 9 раз), `test_damage` (`BUILD`).
- **Если апстрим изменит.** Новое расширение меняет счётчик — падение.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-033. Встроенные английские строки: Telegram → Pluma
- **Что и зачем.** Интерфейс (генерируемые `PresentationStrings`) и тексты push-уведомлений. Слово `Telegram` (`TELEGRAM`) становится `Pluma` (`PLUMA`) только в значениях, включая названия функций («Pluma Premium»). Ключи, URL, имена пользователей и цели ссылок не меняются (они в нижнем регистре или часть другого слова: `telegram.org`, `t.me`, `TelegramTips`).
- **Где.** `Telegram/Telegram-iOS/en.lproj/Localizable.strings`, регулярные выражения `STRINGS_ENTRY` и `TELEGRAM_WORD` (слово целиком, также после `\n`). Счётчик `STRINGS_RENAMED = (556, 609)` — записей и вхождений в закреплённом коммите.
- **Проверка.** `verify_names` (ни одно значение не называет Telegram), `RenameTest.test_values_only`, `test_damage` (`STRINGS`).
- **Если апстрим изменит.** Любая новая или удалённая строка с «Telegram» меняет счётчик — патч падает; счётчик обновлять после просмотра новых строк.
- **Ограничение.** Английский — единственный язык, встроенный в приложение. Остальные языки приходят с сервера языковыми пакетами (задача 021).
- **История.** `2530746`, 2026-10-29, задача 017.

### I-034. Разрешения и имя в других языках (InfoPlist.strings)
- **Где.** `Telegram/Telegram-iOS/<lang>.lproj/InfoPlist.strings` для 19 языков (`INFOPLIST_NAMES`): `ar` «تيليجرام» ×7 (и имя под иконкой), `be ca de en es id it ms nl pl pt tr uk uz` «Telegram» ×4, `fa` «تلگرام» ×4, `fr` ×5, `ru` ×2, `ko` «Telegram» ×2, «텔레그램이» ×2 → «Pluma가» (частица после гласной), «텔레그램» ×1 (имя под иконкой). `replace` с `count`.
- **Проверка.** `verify_names` (старых имён нет; где есть `CFBundleDisplayName`, он равен `Pluma`), `test_damage` (3 порчи `INFOPLIST_STRINGS`).
- **История.** `2530746`, 2026-10-29, задача 017.

### I-035. Info.plist: английские тексты разрешений и тип файла темы
- **Где.** `Telegram/BUILD`: `<string>Telegram stores your contacts heavily encrypted` (1), `When you send your location to your friends, Telegram needs access to show them a map.` (3), `<string>Telegram iOS Color Theme File</string>` (1). `replace`.
- **Проверка.** `verify_names` (ни одна строка `<string>` с пробелом в `BUILD` не называет Telegram).
- **История.** `2530746`, 2026-10-29, задача 017.

### I-036. Имя в коде (`CODE_NAMES`)
- **Что и зачем.** Тексты, зашитые в исходники. Все `replace`, `{name}` = Telegram → Pluma:
  - `submodules/TelegramUI/Sources/StoreDownloadedMedia.swift`: `let albumName = "Telegram"` — альбом «Фото»;
  - `submodules/TelegramCallsUI/Sources/CallKitIntegration.swift`: `CXProviderConfiguration(localizedName: "Telegram")` — имя в системном экране звонка (iOS 13);
  - `submodules/WidgetItems/Sources/WidgetItems.swift` и `Telegram/SiriIntents/IntentHandler.swift`: «Open Telegram and enter passcode to edit widget.»;
  - `submodules/TelegramCore/Sources/State/AccountStateManagementUtils.swift`: `firstName: "Telegram Notifications"` — служебный аккаунт 777000 до прихода данных сервера;
  - `submodules/AuthorizationUI/Sources/AuthorizationSequenceController.swift`: `emailBody.append("Telegram: \(appVersion)\n")` — письмо о непришедшем коде;
  - `submodules/AccountContext/Sources/DeviceContactData.swift` (`self.init(label: "Telegram", value: "\(phonebookUsernamePrefix)`), `submodules/TelegramUI/Sources/DeviceContactDataManager.swift` (×3) и `Telegram/SiriIntents/IntentContacts.swift` (`if address.label == "Telegram", let peerId = parseAppSpecificContactReference(`) — метка записи приложения в контакте адресной книги: иначе её делит Telegram, установленный на том же телефоне.
- **Проверка.** `verify_names`, `test_damage` (`StoreDownloadedMedia.swift`, `DeviceContactDataManager.swift`).
- **История.** `2530746`, 2026-10-29, задача 017.

### I-037. Схема ссылок plm:// вместо telegram:// в регистрации
- **Что и зачем.** Схема собственных ссылок приложения (виджет открывает приложение ею) — `plm`. Сначала была `pluma`, сменена на `plm`, общую для всех клиентов Pluma.
- **Где.** `Telegram/BUILD`: `                <string>telegram</string>\n` → `<string>plm</string>`. `replace`. Плюс ключ `app_specific_url_scheme` конфигурации (I-065).
- **Проверка.** `verify_scheme` (зарегистрированы ровно `plm` и `tonsite`), `verify()` (нет `<string>telegram</string>`), `ConfigTest.test_rejects` (схемы `tg`, `telegram`, `tonsite` запрещены).
- **История.** `807e2a8` (схема `pluma`), 2026-10-27, задача 006; `e1c8fc5` (`plm`, `config/app.env`), 2026-10-29, задача 017.

### I-038. tg:// больше не регистрируется
- **Что и зачем.** `tg://` из других приложений открывает Telegram, если он установлен. `tonsite` остаётся.
- **Где.** `Telegram/BUILD`: `<string>tg</string>\n                <string>tonsite</string>\n` → только `tonsite`. `replace`.
- **Проверка.** `verify_scheme`, `test_patch`.
- **Апстрим / официальный.** Регистрирует `telegram`, `tg`, `tonsite`.
- **История.** `e1c8fc5`, 2026-10-29, задача 017.

### I-039. Ссылки, которые приложение создаёт, — plm://
- **Где.** `SCHEME_LINKS`, все `replace` (`tg` → `plm`):
  - `AuthorizationUI/Sources/AuthorizationSequencePhoneEntryControllerNode.swift`: `let urlString = "tg://login?token=\(tokenString)"` — QR входа на другом устройстве;
  - `QrCodeUI/Sources/QrCodeScanScreen.swift`: `filteredCodes = codes.filter { $0.message.hasPrefix("tg://") }` — сканер принимает только коды Pluma, вход по QR работает между клиентами Pluma;
  - `QrCodeUI/Sources/QrCodeScreen.swift`: `: "tg://proxy")?server=`, `: "tg://socks")?server=` — QR прокси;
  - `TelegramUI/Components/PeerInfo/PeerInfoScreen/Sources/PeerInfoScreen.swift`: `let link = "tg://settings/\(item.id)"`;
  - `ChatImportUI/Sources/ChatImportActivityScreen.swift`: `tg://localpeer?id=`;
  - `TelegramUI/Components/ShareExtensionContext/Sources/ShareExtensionContext.swift`: `appSpecificScheme: "tg", openUrl:` и `self.openUrl("tg://shareStory?session=\(sessionId)")`.
- **Проверка.** `verify_scheme` (в этих файлах нет `"tg://` и `appSpecificScheme: "tg"`), `test_damage` (`QrCodeScanScreen.swift`, `ShareExtensionContext.swift`).
- **Если апстрим добавит новое место.** Новые `tg://` в других файлах не ловятся — искать `"tg://` по коду при обновлении.
- **История.** `e1c8fc5`, 2026-10-29, задача 017.

### I-040. Приложение читает plm:// наравне с tg://
- **Что и зачем.** Ссылки на прокси, наборы стикеров, обои и рекламу принимают и свою схему. `tg://` внутри приложения (в сообщениях, от ботов) по-прежнему открывается.
- **Где.** `submodules/UrlHandling/Sources/UrlHandling.swift`: `if let parsedUrl = URL(string: url), parsedUrl.scheme == "tg", let host = parsedUrl.host,` → `… == "tg" || parsedUrl.scheme == sharedContext.applicationBindings.appSpecificScheme, …`, 4 вхождения. `replace`.
- **Проверка.** `test_patch`, `test_damage` (`URL_HANDLING`).
- **История.** `e1c8fc5`, 2026-10-29, задача 017.

### I-041. Ссылки «Устройств» и сканера на сайты Pluma
- **Что и зачем.** В заголовке «Настройки → Устройства» «Pluma Desktop» открывает `https://pluma.chat/`, «Pluma Web» — `https://web.pluma.chat/`, как в Android (решение владельца). То же под видоискателем сканера («Link Desktop Device») и в его тексте.
- **Где.** `CLIENT_LINKS`, `replace`:
  - `submodules/SettingsUI/Sources/Privacy and Security/Recent Sessions/RecentSessionsController.swift`: после `}, openDesktopLink: {` и `}, openWebLink: {` — `openExternalUrl(… url: "https://getdesktop.telegram.org", ` / `"https://web.telegram.org"`;
  - `submodules/QrCodeUI/Sources/QrCodeScanScreen.swift`: `case "desktop":` и `case "web":` — те же URL.
  - `CLIENT_TEXTS`: `en.lproj/Localizable.strings`, `"AuthSessions.AddDevice.ScanInstallInfo" = "Go to [getdesktop.telegram.org](desktop) or [web.telegram.org](web) to get the QR code";` → `[pluma.chat](desktop) or [web.pluma.chat](web)`.
- **Проверка.** `verify_client_links` (в этих файлах нет `(get)?desktop.telegram.org`, `web.telegram.org`; новые URL и текст на месте), `ClientLinksTest.test_links`, `test_scanner_changed`, `test_upstream_changed`, `test_table`, `test_damage` (3 порчи `SESSIONS`, `QR_SCAN`).
- **Что оставлено.** Текст заголовка не называет сайта — строка не меняется. «Learn More» (`openOtherAppsUrl`, `telegram.org/apps`) остаётся; показывается, только если сервер включает `qr_login_camera`.
- **История.** `b0586ec`, 2026-10-30, задача 017.

### I-042. Тема Day Classic: палитра
- **Что и зачем.** Тема свежей установки. Акцент `#5B7A5E` (из него следуют 64 цвета темы), ссылки `#4F6E52`, входящий пузырь `#F1F2F0`, исходящий — градиент `#E3EAE2 → #D5E1D4`, текст `#1F2E20`, время и галочки `#5B7A5E` (не зелёные), переключатели, выделение, архив, кольца историй — шалфей. Белый фон чата вместо узора Telegram (это файл на серверах Telegram); даты и служебные сообщения — серые на `#F1F2F0`. Серо-синие кнопок в чате и контуров стикеров — нейтральные серые. Тема Day тоже получает новый акцент и градиент исходящих.
- **Где.** `submodules/TelegramPresentationData/Sources/DefaultDayPresentationTheme.swift`, строки `PALETTE_EDITS` с `DAY_THEME` (31 правка `replace` со счётчиками от 1 до 12). Ключевые якоря: `public let defaultDayAccentColor = UIColor(rgb: 0x0088ff)`, `startButtonColor: UIColor(rgb: 0x2ca5e0)`, `UIColor(rgb: 0x3fc33b)` (×12), `UIColor(rgb: 0x004bad)` (×6), `let defaultPatternWallpaper: TelegramWallpaper = defaultBuiltinWallpaper(data: .default, colors: defaultBuiltinWallpaperGradientColors.map(\.rgb))` → `.color(0xffffff)`, `withDefaultWallpaper: PresentationThemeServiceMessageColorComponents(…)`, `dateTextColor: PresentationThemeVariableColor(color: UIColor(rgb: 0xffffff))`.
- **Проверка.** `verify_palette` (ни одного `TELEGRAM_BLUES` в теме), `test_patch`, `test_damage` (2 порчи `DAY_THEME`).
- **Если апстрим изменит.** Любой сдвиг счётчика — падение. Новый синий вне списка `TELEGRAM_BLUES` не ловится.
- **Апстрим / официальный.** Синий акцент `#0088FF`, зелёные исходящие, узор на фоне.
- **История.** `6f57e5e`, 2026-10-29, задача 017.

### I-043. Тема Night: палитра
- **Что и зачем.** Шалфей `#9DBB9F` там, где был белый или синий (с тёмным текстом `#131513`), ссылки, время и галочки `#B2CEB4`, пузыри `#232623` и `#2C3A2D → #36473A`, фон `#131513`, графитовый фон чата вместо узора. Тёмный текст на акценте.
- **Где.** `submodules/TelegramPresentationData/Sources/DefaultDarkPresentationTheme.swift`, строки `PALETTE_EDITS` с `DARK_THEME` (около 40 `replace`). Ключевые якоря: `accentColor: UIColor(rgb: 0x3e88f7), backgroundColors: [], …`, ветка `} else if accentColor.rgb == 0x3e88f7 { bubbleColors = [0x0771ff, 0x9047ff, 0xa256bf].reversed()` → `[0x2c3a2d, 0x36473a]`, `if initialAccentColor.lightness > 0.735 {` (плюс `|| initialAccentColor.rgb == 0x9dbb9f`), `let defaultPatternWallpaper … defaultDarkWallpaperGradientColors … intensity: -34` → `.color(0x131513)`.
- **Проверка.** `verify_palette`, `test_damage` (2 порчи `DARK_THEME`).
- **Апстрим / официальный.** Белый акцент, синие исходящие, узор.
- **История.** `6f57e5e`, 2026-10-29, задача 017.

### I-044. Тема Night: цвета сообщений в длинных строках
- **Где.** `DefaultDarkPresentationTheme.swift`, `PALETTE_LINE_EDITS`, `replace_in_line`: строка входящих (якорь `secondaryTextColor: UIColor(rgb: 0xffffff, alpha: 0.5), linkTextColor: UIColor(rgb: 0xffffff),`, 8 правок: ссылки, акцент, элементы управления, полоса, выделение текста) и строка исходящих (якорь `), primaryTextColor: UIColor(rgb: 0xffffff), secondaryTextColor: …`, 10 правок).
- **Проверка.** `TreeTest.test_replace_in_line`, `test_patch`.
- **История.** `6f57e5e`, 2026-10-29, задача 017.

### I-045. Night без навязанного синего акцента
- **Что и зачем.** С тёмным режимом системы приложение давало Night синий акцент; теперь у Night цвета Pluma.
- **Где.** `submodules/TelegramPresentationData/Sources/PresentationData.swift`: `if automaticTheme == .builtin(.night) && effectiveColors == nil { effectiveColors = PresentationThemeAccentColor(baseColor: .blue) }` → комментарий. `replace`.
- **Проверка.** `verify_palette` (нет `PresentationThemeAccentColor(baseColor: .blue)`), `test_damage` (`PRESENTATION_DATA`).
- **История.** `6f57e5e`, 2026-10-29, задача 017.

### I-046. Первый акцент в настройках темы и акцент старых экранов
- **Где.**
  - `submodules/TelegramUIPreferences/Sources/PresentationThemeSettings.swift`: `case .blue:\n                value = 0x0088ff` → `0x5b7a5e`; `if baseTheme == .night && self.baseColor == .blue { return UIColor(rgb: 0x3e88f7)` → `0x9dbb9f`. `replace`.
  - `submodules/LegacyComponents/Sources/TGColor.m`: `color = TGColorWithHex(0x0088ff);` → `0x5b7a5e` (камера, редактор медиа, геопозиция).
- **Проверка.** `test_patch`, `test_damage` (`THEME_SETTINGS`, `LEGACY_COLORS`).
- **Что оставлено.** Темы «Day», «Night Tinted» и прочие акценты — как в апстриме.
- **Не сделано.** Миниатюра ночной темы в «Оформлении» показывает синий исходящий пузырь Telegram (бриф 020, замечание без решения).
- **История.** `6f57e5e`, 2026-10-29, задача 017.

### I-047. Аватары «Избранного» и репостов
- **Что и зачем.** `AvatarNode` держит эти градиенты константами в синем Telegram в любой теме.
- **Где.** `submodules/AvatarNode/Sources/AvatarNode.swift`: `static let savedMessagesColors: [UIColor] = [\n        UIColor(rgb: 0x2a9ef1), UIColor(rgb: 0x72d5fd)` → `0x5b7a5e, 0x9dbb9f`; `static let repostColors … UIColor(rgb: 0x3DA1FD), UIColor(rgb: 0x34C76F)` → `0x4f6e52, 0x9dbb9f`. `replace`.
- **Проверка.** `verify_palette` (в этих строках нет синих Telegram), `test_damage` (3 порчи `AVATAR_NODE`).
- **История.** `370fbae`, 2026-10-29, задача 017.

### I-048. Цвета-идентичности: синий → шалфей, бирюзовый → teal
- **Что и зачем.** Telegram даёт каждому пользователю и чату один из семи цветов (`id % 7`): аватар-буквы, имя в группе, полоса и заголовок ответа и превью ссылки, тег папки. Синий (5) становится шалфеем: аватар `#7FA082 → #5B7A5E`, имя `#4F6E52`, в тёмных темах `#9DBB9F`. Бирюзовый (4) — teal без синевы: аватар `#5BCBC4 → #2C9C98`, имя `#1F8A80`, в тёмных `#53CCC6` (значение Android). Остальные пять — Telegram (решение владельца, как в Android). Цвета 7+ — серверные (`help.getPeerColors`).
- **Где.** `IDENTITY_TABLES`, каждая таблица обязана содержать семь цветов Telegram по порядку (`TELEGRAM_IDENTITY`), `replace` всей таблицы:
  - `submodules/AvatarNode/Sources/AvatarNode.swift`: `public static let gradientColors: [[UIColor]] = [`;
  - копии `private let gradientColors: [NSArray] = [` в `Telegram/NotificationService/Sources/NotificationService.swift`, `Telegram/WidgetKitWidget/PeerNode.swift`, `Telegram/SiriIntents/IntentHandler.swift`, `submodules/TelegramUI/Components/Stories/StoryContainerScreen/Sources/LiveChatReactionStreamView.swift`;
  - `submodules/AccountContext/Sources/PeerNameColors.swift`: `public static var defaultSingleColors: [Int32: Colors] { return [`;
  - `submodules/TelegramCore/Sources/State/ManagedPeerColorUpdates.swift`: `let staticMap: [UInt32] = [`.
- **Проверка.** `verify_identity` (цвета каждой таблицы — цвета Pluma по порядку), `IdentityTest.test_owner_colours`, `test_colours`, `test_upstream_changed`, `test_verify`, `test_table`, `test_damage` (`NOTIFICATION_AVATARS`, `WIDGET_AVATARS`, `INTENT_AVATARS`, `LIVE_STREAM_AVATARS`, `PEER_NAME_COLORS`, `PEER_COLOR_UPDATES`).
- **Если апстрим изменит.** Новая копия таблицы в другом файле не ловится — искать `0x2a9ef1`/`0x368ad1`/`0x309eba` по коду.
- **История.** `49e46cf`, 2026-10-31; тёмный бирюзовый `#53CCC6` — `3ceb773`, 2026-10-31; задача 020. (На ветке задачи был коммит `dd7ec02`, по ошибке захвативший незаконченные цвета; в `main` он не попал, история чистая: `162ba82 → 4d338ac → 49e46cf → 3ceb773`.)

### I-049. Тёмные цвета имён для шалфея и teal
- **Где.**
  - `PeerNameColors.swift`: `colors: defaultSingleColors,\n            darkColors: [:],` → `darkColors: [4: #53CCC6, 5: #9DBB9F]` (`name_dark`). `replace`.
  - `ManagedPeerColorUpdates.swift`: `STATIC_DARK_OLD` (`let colorPack = MultiColorPack(colors: [staticMap[Int(colorId)]])` … `ColorOptionPack(light: defaultColors, dark: nil, …)`) → с `staticDarkMap` и `dark: defaultDarkColors` (`static_dark`). `replace`.
- **Проверка.** `verify_identity` (`name_dark`, `static_dark` на месте, нет `dark: nil,`).
- **Апстрим / официальный.** Тёмных цветов имён для 0–6 нет; тёмные темы показывают светлые.
- **История.** `49e46cf`, `3ceb773`, 2026-10-31, задача 020.

### I-050. Запасной цвет имени в тёмных темах
- **Что и зачем.** Для цвета, которого приложение не знает, `PeerNameColors.get` и `getChatFolderTag` берут синий (5) светлый. Светлый шалфей не читается на тёмном фоне, поэтому в тёмных темах — тёмный.
- **Где.** `PeerNameColors.swift`: `            return PeerNameColors.defaultSingleColors[5]!\n` ×2 → `return dark ? (PeerNameColors.defaultValue.darkColors[5] ?? …) : …`. `replace`, `count=2`.
- **Проверка.** `verify_identity` (ровно 2 новых, ни одного старого), `IdentityTest.test_fallback`.
- **Известные отличия от Telegram (README).** Аватар-буква цвета выше 13, которого нет у приложения, по оттенку запасного (шалфей) получается зелёным. Цитата канала в поле ввода берёт светлый цвет имени в любой теме, как в апстриме, поэтому у синего там `#4F6E52` и в тёмных темах.
- **История.** `49e46cf`, 2026-10-31, задача 020.

### I-051. Не сделано: значки «Устройств»
- **Что.** Значки iPhone, iPad, Mac, Windows в «Устройствах» — синий значков настроек (`#0079FF`), не цвет-идентичность. Останутся ли синими, не решено (README).
- **Где.** Правки нет.

### I-052. Раздел поддержки в настройках убран
- **Что и зачем.** Последний раздел настроек ведёт только к сервисам Telegram: «Ask a Question» просит у сервера аккаунт поддержки Telegram (`help.getSupport`, сервер Pluma не обслуживает), FAQ — `telegram.org/faq`, «Pluma Features» — канал `@TelegramTips`. Раздел и его пункты в поиске по настройкам убраны.
- **Где.**
  - `submodules/TelegramUI/Components/PeerInfo/PeerInfoScreen/Sources/PeerInfoSettingsItems.swift`, `SUPPORT_ITEMS_OLD`: три `items[.support]!.append(PeerInfoScreenDisclosureItem(id: 0|1|2, …Settings_Support|Settings_FAQ|Settings_Tips…))`. `replace_lines`.
  - `submodules/SettingsUI/Sources/Search/SettingsSearchableItems.swift`: `let helpItems = helpSearchableItems(context: context)\n allItems.append(contentsOf: helpItems)`. `replace_lines`.
- **Проверка.** `verify_support`, `test_patch`, `test_damage` (`SETTINGS_ITEMS`, `SETTINGS_SEARCH`).
- **Апстрим / официальный.** Раздел есть.
- **История.** `a4843f4`, 2026-10-29, задача 017.

---

## Premium и платные функции

### I-053. Публичная сборка без покупок
- **Что и зачем.** В конфигурации Make.py: `is_appstore_build: true`, `is_internal_build: false`, `appstore_id: "0"`, `premium_iap_product_id: ""` (нет покупки Premium в приложении), `app_center_id: "0"` (нет App Center). Pluma Premium не продаёт; сервер шлёт `premium_purchase_blocked`.
- **Где.** `Config.build_configuration()` → `build-system/pluma-configuration.json` (I-065).
- **Проверка.** `test_patch` (ключи совпадают с `required_keys`).
- **Официальный.** Значения Telegram — в `build-system/appstore-configuration.json` апстрима; их содержимое для этого реестра не сверялось.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-054. Баннер дня рождения: нажатие открывает чат
- **Что и зачем.** Баннер наверху списка чатов про контакты с днём рождения (`contacts.getBirthdays`) предлагал подарить им Premium. Теперь один именинник — его чат; несколько — лист действий с их именами (как панель трансляции геопозиции, `LiveLocationHeaderPanelComponent.swift`), выбор открывает чат. Порядок — по показанным именам, при равенстве по id: пиры баннера приходят в порядке словаря (`GlobalControlPanelsContext.swift`) и могут меняться между запусками. Решение владельца (задача 020, вариант А).
- **Где.** `submodules/ChatListUI/Sources/ChatListControllerNode.swift`:
  - `BIRTHDAY_OPEN_OLD` (`case let .birthdayPremiumGift(peers, birthdays):\n    self.effectiveContainerNode.currentItemNode.interaction?.openPremiumGift(peers, birthdays)`, отступ 24) → `self.openBirthdayChats(peers)`. `replace`.
  - Метод `openBirthdayChats(_:)` вставлен перед `private func updateNavigationBar(layout: ContainerViewLayout, deferScrollApplication: Bool, transition: ComponentTransition) -> (navigationHeight: CGFloat, storiesInset: CGFloat) {`. Отдельным методом, потому что внутри выражения панелей `updateNavigationBar` компилятор Swift не успевал вывести типы. `replace`.
- **Проверка.** `verify_birthdays` (в ветке нет `openPremiumGift(`, метод ровно один, два вызова `.peerSelected?(peer, nil, true, true, nil)`), `BirthdayTest.test_notice`, `test_verify`, `test_upstream_changed`, `test_table`, `test_damage` (`CHAT_LIST`).
- **Апстрим / официальный.** Открывает экран подарка Premium. Android и Web K прячут баннер при `premium_purchase_blocked` (бриф 020); Pluma показывает его везде, где апстрим.
- **История.** `162ba82`, 2026-10-31; метод вынесен `4d338ac`, 2026-10-31 (сборка падала по времени проверки типов); задача 020.

### I-055. Баннер дня рождения: текст «Send Message»
- **Где.** `submodules/TelegramUI/Components/ChatList/ChatListHeaderNoticeComponent/Sources/ChatListNoticeItem.swift`, `BIRTHDAY_TEXT_OLD`: `text = item.strings.ChatList_BirthdaySingleText` и `…MultipleText` → `item.strings.ContactList_Context_SendMessage`. `replace`. Наличие ключа `"ContactList.Context.SendMessage" = "Send Message";` в `Localizable.strings` — `expect`.
- **Проверка.** `verify_birthdays`, `test_damage` (`CHAT_LIST_NOTICE`).
- **Апстрим.** «Gift them Telegram Premium.» (после I-033 — «Pluma Premium»).
- **История.** `162ba82`, 2026-10-31, задача 020.

### I-056. Баннер дня рождения: закрытие без тоста про подарки
- **Где.** `submodules/TelegramUI/Components/GlobalControlPanelsContext/Sources/GlobalControlPanelsContext.swift`, `BIRTHDAY_CLOSE_OLD`: после `dismissServerProvidedSuggestion(suggestion: ServerProvidedSuggestion.todayBirthdays.id)` убран `UndoOverlayController(… ChatList_PremiumGiftInSettingsInfo …)`. `replace`. `help.dismissSuggestion` с `BIRTHDAY_CONTACTS_TODAY` остаётся.
- **Проверка.** `verify_birthdays`, `test_damage` (`CHAT_LIST_PANELS`).
- **Апстрим.** Тост отправляет в настройки дарить Premium; там подарков нет (Premium заблокирован, Stars нет).
- **История.** `162ba82`, 2026-10-31, задача 020.

### I-057. Строка дня рождения в профиле: подарок только при продаже Premium
- **Что и зачем.** Строка дня рождения в профиле контакта предлагала подарок (нажатием и значком), хотя чат сначала проверяет, что Premium можно купить (`ChatControllerOpenAttachmentMenu.swift`). Теперь — так же, как в чате, Android и Web.
- **Где.** `submodules/TelegramUI/Components/PeerInfo/PeerInfoScreen/Sources/PeerInfoProfileItems.swift`, `BIRTHDAY_PROFILE`: после `let isBirthdayToday = hasBirthdayToday(birthday: birthday)` добавлен `canGiftOnBirthday = isBirthdayToday && !PremiumConfiguration…isPremiumDisabled`; в `} else if isBirthdayToday && cachedData.disallowedGifts != TelegramDisallowedGifts.All {` и `icon: isBirthdayToday ? .premiumGift : nil, action: birthdayAction,` `isBirthdayToday` → `canGiftOnBirthday`. `replace`.
- **Проверка.** `verify_birthdays`, `test_damage` (2 порчи `PROFILE_ITEMS`).
- **История.** `162ba82`, 2026-10-31, задача 020.

### I-058. Время прочтения без Premium
- **Что и зачем.** Меню своего сообщения в личном чате показывает «прочитано в …» (`messages.getOutboxReadDate`). Апстрим показывает его пользователю без Premium только там, где Premium продаётся; при `premium_purchase_blocked` строки не было. Решение владельца (задача 033, вариант А): как в Android, где такой проверки нет.
- **Где.** `submodules/TelegramUI/Sources/ChatInterfaceStateContextMenus.swift`, функция `canViewReadStats`: `READ_DATE_GATE_OLD` (`if !isPremium { let premiumConfiguration = …; if premiumConfiguration.isPremiumDisabled { return false } }`, отступ 8) → комментарий. `replace_lines`.
- **Проверка.** `verify_read_date` (в теле `canViewReadStats` нет `isPremiumDisabled`), `test_damage` (`CONTEXT_MENUS`).
- **Апстрим / официальный.** В странах без Premium официальный Telegram ведёт себя как апстрим (бриф 033).
- **История.** `74bf939`, 2026-11-03, задача 033.

### I-059. Скрытое время прочтения без значка Premium
- **Что и зачем.** Где собеседник скрыл время, апстрим пишет «Read» со значком, который открывает экран приватности Premium. Без продажи Premium — только «Read», без значка и без действия.
- **Где.** `ChatInterfaceStateContextMenus.swift`:
  - `self.buttonNode.isUserInteractionEnabled = self.item.action != nil && stats.peers.isEmpty` (отступ 12) + `&& !PremiumConfiguration…isPremiumDisabled`. `replace`.
  - `self.textNode.attributedText = text\n    showReadBadge = true` (отступ 20) → `showReadBadge = !…isPremiumDisabled`. `replace`.
- **Проверка.** `verify_read_date` (новые строки на месте, нет `showReadBadge = true`), `test_damage` (`CONTEXT_MENUS`).
- **История.** `74bf939`, 2026-11-03, задача 033.

### I-060. Что из Premium не тронуто
- Рождественский баннер со своим `openPremiumGift([], nil)` и тостом `ChatList_PremiumGiftInSettingsInfo` оставлен как в апстриме (`test_patch` проверяет, что он на месте).
- Остальные места, которые прячет сам апстрим по `premium_purchase_blocked`, работают без правок. Полного обхода всех предложений Premium/Stars в iOS-клиенте не делалось — при обновлении апстрима это открытый пункт.

---

## Медиа

### I-061. Без шрифта Apple SF Camera
- **Что и зачем.** ITMS-91198: шрифты Apple в бандле нельзя (с 1 октября 2027 такие загрузки отклоняются). SF Camera — шрифт элементов камеры. Без него камера берёт системный шрифт того же веса, как `Font.with` уже делает, когда шрифт не загрузился.
- **Где.**
  - `submodules/TelegramUI/Resources/Fonts/AremacFS-Regular.otf` (`1a78b533…`), `AremacFS-Semibold.otf` (`28eab504…`) — `remove_file`.
  - `Telegram/BUILD`: блок `<key>UIAppFonts</key> <array> <string>AremacFS-Regular.otf</string> <string>AremacFS-Semibold.otf</string> </array>` удалён (`replace` на пустое).
  - `submodules/Display/Source/Font.swift`: две строки запасного шрифта (`UIFont(name: encodeText(string: "TGDbnfsb.Tfnjcpme", key: -1), size: size) ?? UIFont.systemFont(…)` и `"TGDbnfsb.Sfhvmbs"`) — `expect`, на них правка опирается.
- **Проверка.** `verify_store` (нет `AremacFS` в `BUILD`, файлов нет), `test_patch`, `test_damage` (`FONT`).
- **Апстрим / официальный.** Шрифт в бандле и в `UIAppFonts`. Как с этим поступает сборка Telegram для App Store, не проверялось.
- **История.** `4c6fe94`, 2026-11-01, задача 029.

### I-062. Альбом «Фото» называется Pluma
См. I-036 (`StoreDownloadedMedia.swift`).

---

## Уведомления и push

Отдельных правок кода уведомлений нет. Что их касается:
- тексты push-уведомлений — встроенные английские строки (I-033);
- аватары-буквы в уведомлениях — копия таблицы цветов в `NotificationService.swift` (I-048);
- App Group `group.chat.pluma.app` и `aps-environment = production` у профиля приложения проверяет preflight (I-075); расширение NotificationService читает ключи аккаунта из App Group, чтобы расшифровать push. Ключ APNs (задача 009) остаётся на сервере и в сборку не входит;
- проверка `Telegram/BUILD`: `<string>group.{telegram_bundle_id}</string>` — `expect` (I-064).

---

## Сборка и подпись

### I-063. Проверка закреплённого апстрима
- **Что и зачем.** Правок нет, только `expect`: слой схемы `public func currentLayer() -> UInt {\n        return 228\n    }` в `submodules/TelegramCore/Sources/State/Serialization.swift` (слой, который говорит сервер; ADR 0008 сервера); `versions.json`: `app` = 12.9.2, `xcode` = 26.2, `bazel` начинается с 8.4.2.
- **Проверка.** `test_damage` (`SERIALIZATION`, 3 порчи `VERSIONS`).
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-064. Что сборка ожидает от Make.py и Telegram/BUILD
- **Где.** `expect`:
  - `build-system/Make/Make.py`: `elif configuration == 'debug_sim_arm64':`, `'release_sim_arm64'`, `'release_arm64'`, опции `--xcodeManagedCodesigning`, `--codesigningInformationPath`, `--cacheDir`, `--bazelUserRoot`, `--continueOnError`;
  - `build-system/Make/BuildConfiguration.py`: `'<суффикс>': '<имя профиля>'` для семи целей `TARGETS` (`''`→`Telegram`, `.Share`, `.NotificationService`, `.NotificationContent`, `.Widget`, `.SiriIntents`→`Intents`, `.BroadcastUpload`);
  - `Telegram/BUILD`: блок `extensions = select({ ":disableExtensionsSetting": [], … ShareExtension … BroadcastUploadExtension })`, `name = "disableProvisioningProfiles"`, `":disableProvisioningProfilesSetting": None,` ×10, `<string>group.{telegram_bundle_id}</string>`.
- **Проверка.** `test_damage` (3 порчи `MAKE`, 2 `BUILD_CONFIGURATION`, 9 `BUILD`).
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-065. Конфигурация сборки Make.py (идентичность приложения)
- **Что и зачем.** Не правка исходника, а добавленный файл `build-system/pluma-configuration.json` (`--configurationPath`): `bundle_id` `chat.pluma.app` (расширения `chat.pluma.app.<имя>`, App Group `group.chat.pluma.app`), `api_id` 10002, `api_hash`, `team_id`, `app_center_id "0"`, `is_internal_build "false"`, `is_appstore_build "true"`, `appstore_id "0"`, `app_specific_url_scheme "plm"`, `premium_iap_product_id ""`, `enable_siri false`, `enable_icloud false` (Siri и iCloud требуют своих возможностей; iCloud — ещё и источник резервных адресов). Набор ключей обязан совпадать с `required_keys` из `BuildConfiguration.py`.
- **Проверка.** `verify()` (bundle ID, api_id, Team ID), `test_patch`, `test_damage` (`BUILD_CONFIGURATION`).
- **Если апстрим изменит.** Новый обязательный ключ — патч падает и называет оба набора.
- **Официальный.** `build-system/appstore-configuration.json` апстрима (его использует сборка `reference`, I-081).
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-066. `.bazelrc` импортирует `pluma.bazelrc`
- **Что и зачем.** Make.py не даёт передать свои флаги Bazel. Патч дописывает в `.bazelrc` апстрима `try-import %workspace%/pluma.bazelrc`; `ci/build.sh` пишет этот файл: `--repository_cache`, в CI `--color=no --curses=no --show_progress_rate_limit=60`, для симулятора `build --//Telegram:disableProvisioningProfiles`.
- **Где.** `.bazelrc` — дописывание в конец; патч падает, если там уже есть `try-import` или `pluma.bazelrc`.
- **Проверка.** `test_patch`, `test_damage` (`BAZELRC`).
- **История.** `807e2a8` (патч), `44fd3ea` (`build.sh`), 2026-10-27, задача 006.

### I-067. Сборка: `ci/build.sh`
- **Что.** `sim` — конфигурация `PLUMA_SIM_CONFIGURATION`, без подписи (`--xcodeManagedCodesigning` без профилей), `out/pluma-ios-<версия>-<сборка>-simulator.zip` с `Pluma.app`; `ipa` — `release_arm64`, профили из `$PLUMA_CODESIGNING_DIR`; `appstore` — то же с профилями App Store. Проверяет: дерево пропатчено, bundle ID = `PLUMA_BUNDLE_ID`, версия = `UPSTREAM_VERSION`, у каждого бандла есть `embedded.mobileprovision`, `codesign --verify --deep --strict`. Корень Bazel — `/private/var/tmp/_bazel_containerhost`, как у CI апстрима.
- **История.** `44fd3ea`, 2026-10-27, задача 006; `appstore` — `dc8a8b4`, 2026-11-01, задача 029.

### I-068. Подготовка раннера: `ci/setup.sh`
- **Что.** Выбирает Xcode из `upstream.env` (Make.py не принимает другой), освобождает диск до `PLUMA_MIN_FREE_GB` (60): удаляет другие Xcode, затем рантаймы симуляторов; скачивает Metal toolchain; режим `age` ставит `age`.
- **История.** `44fd3ea`, 2026-10-27, задача 006.

### I-069. Загрузка апстрима: `ci/fetch-upstream.sh`
- **Что.** Один коммит без истории; `sparse` — только пути из `patch.py --paths` (`UPSTREAM_PATHS`), `full` — со всеми сабмодулями. Проверяет, что HEAD = `UPSTREAM_REF`. Повторы и таймауты.
- **Если апстрим изменит.** Новый файл, который патч начнёт читать, надо добавить в `UPSTREAM_PATHS` (`BrandTest.test_upstream_paths_cover_the_brand` проверяет картинки).
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-070. Наложение патча: `ci/configure.sh`
- **Что.** Режимы `dev` и `server` (см. «Как накладывается патч»). В `server` отсутствие любой из `SERVER_IPV4`, `SERVER_PORT`, `RSA_PUBLIC_KEY`, `TEAM_ID` — ошибка: половина конфигурации дала бы клиент без входа.
- **История.** `807e2a8`, 2026-10-27, задача 006.

### I-071. Конфигурация в репозитории
- `upstream.env` — с первого коммита без изменений.
- `config/app.env` — `807e2a8`; `e1c8fc5` сменил `PLUMA_URL_SCHEME` с `pluma` на `plm` и описание (tg:// не регистрируется).
- `config/dev/server.env`, `config/dev/server_public.pem` — `807e2a8`. Только dev-стенд: `127.0.0.1:10443`, публичный dev-ключ; закрытый ключ — вне репозитория.
- `.gitignore`: `/upstream/`, `/out/`, `__pycache__/`, `*.p12`, `*.mobileprovision`, `*.agekey`.

### I-072. Переменные и секреты CI
- **Переменные репозитория** (не секреты, каждая IPA их несёт): `PLUMA_SERVER_IPV4`, `PLUMA_SERVER_PORT`, `PLUMA_SERVER_IPV6`, `PLUMA_RSA_PUBLIC_KEY`, `PLUMA_TEAM_ID` (включает подписанные сборки), `PLUMA_AGE_RECIPIENT`, `PLUMA_ASC_APP_ID`, `PLUMA_INSTALL_URL` (по умолчанию `https://pluma.chat`), `PLUMA_BAZEL_CACHE_GB` (12).
- **Секреты окружения `release`** (только имена): `PLUMA_DIST_P12_B64`, `PLUMA_DIST_P12_PASSWORD`, `PLUMA_PROFILE_{MAIN,SHARE,NOTIFICATION_SERVICE,NOTIFICATION_CONTENT,WIDGET,SIRI_INTENTS,BROADCAST_UPLOAD}`, `PLUMA_APPSTORE_PROFILE_{…те же…}`, `PLUMA_ASC_KEY_ID`, `PLUMA_ASC_ISSUER_ID`, `PLUMA_ASC_KEY_P8_B64`, `IOSINSTALL_UPLOAD_TOKEN`.
- Окружение `release`: обязательный ревьюер — владелец, развёртывание только с `main`.

### I-073. Workflow `ios.yml`
- **Задачи.** `check` (Ubuntu: тесты, патч для dev и, при `PLUMA_TEAM_ID`, для сервера, lint); `build-sim` (macos-26, артефакт `simulator` 7 дней на push и ручной запуск); `preflight-sign` → `build-ipa` (ad hoc, `release`, артефакт `pluma-ios` 30 дней, зашифрован); `preflight-testflight` → `build-testflight` (ручной запуск `main` с `testflight`); `prune-caches`.
- **Триггеры.** push в `main`, pull request (без артефакта и без сохранения кэша), еженедельный canary `17 3 * * 1` (свежий образ раннера, не даёт кэшу истечь), ручной запуск с флажками `ipa`, `testflight`.
- **Кэш Bazel.** Ключ `bazel-sim|ipa-<hashFiles(upstream.env, config/**, ci/patch.py)>-<run_id>-<attempt>`, восстановление по префиксам; сохраняется и при неудачной сборке; перед сохранением подрезается `cache_gc.py` до 12 ГБ; `build-testflight` читает кэш IPA и не сохраняет свой.
- **Действия** закреплены по SHA (`actions/checkout` v7.0.1, `actions/cache` v6.1.0, `actions/upload-artifact` v7.0.2), обновляет Dependabot.
- **История.** `44fd3ea`, 2026-10-27 (задача 006); `b3e0ff9` 2026-10-27 — порог кэша 6 → 12 ГБ (тёплый прогон после урезанного кэша выполнял 1531 из 5886 действий); `5f499a2` 2026-10-29 — кэш только того же патча, откачен `ec34597` в тот же день (виноват был не кэш, а `simctl install`, I-078); `dc8a8b4` 2026-11-01 — TestFlight; `2238137` 2026-11-01 — передача IPA в сервис установки.

### I-074. Кэш: `ci/cache_gc.py`, `ci/prune-caches.sh`
- **Что.** `cache_gc.py` удаляет из дискового кэша Bazel давно не использованные записи (по mtime) до заданного размера и недописанные `tmp/` (собственная очистка Bazel срабатывает только после минут простоя). `prune-caches.sh` удаляет старые записи кэша Actions на `main`, оставляя новейшую каждого вида, чтобы оба вида умещались в 10 ГБ репозитория.
- **Проверка.** `cache_gc_test.py` (3 теста).
- **История.** `44fd3ea`, 2026-10-27, задача 006.

### I-075. Проверка подписи: `ci/preflight_sign.py`
- **Что.** До сборки, за секунды, в окружении `release`: сертификат Apple Distribution команды, действителен ещё ≥14 дней, ключ в `.p12` — его; по одному профилю на каждый из семи App ID, команды, ≥14 дней, с этим сертификатом; у всех App Group, у приложения push `production`; ad hoc — одинаковые устройства во всех; App Store (`--kind appstore`) — без устройств, плюс ключ API, issuer и `PLUMA_ASC_APP_ID`, ключ — закрытый ключ. С `--out-dir` раскладывает профили и `identity.p12` для Make.py и `keychain.sh`. Секретов не печатает.
- **Проверка.** `preflight_sign_test.py` (13 тестов).
- **История.** `44fd3ea`, 2026-10-27, задача 006; App Store — `dc8a8b4`, 2026-11-01, задача 029.

### I-076. Связка ключей и шифрование
- `ci/keychain.sh create|delete` — временная связка ключей в `$RUNNER_TEMP` с идентичностью, первой в списке поиска; добавляет промежуточный `AppleWWDRCAG3.cer` апстрима, если он есть; удаляется после сборки (`if: always()`).
- `ci/encrypt.sh` — каждая IPA шифруется `age` ключом владельца, открытая удаляется: артефакты публичного репозитория доступны всем (ADR 0010 сервера).
- **История.** `44fd3ea`, 2026-10-27, задача 006.

### I-077. TestFlight: `ci/testflight.sh`
- **Что.** Загружает IPA App Store через `xcrun altool --upload-package` ключом App Store Connect API (как `RemoteBuild.py` апстрима); номер сборки — номер запуска workflow. `.p8` пишется во временный закрытый каталог и удаляется. Приложение не объявляет неосвобождённого шифрования (`ITSAppUsesNonExemptEncryption` уже в `Telegram/BUILD` апстрима — без правки).
- **История.** `dc8a8b4`, 2026-11-01, задача 029.

### I-078. Ловушка установки в симулятор (не правка)
- `simctl install` поверх установленного приложения может оставить старый фреймворк: в zip у всех файлов дата 1980 года, и при том же размере файл не заменяется. Перед установкой: `find Pluma.app -exec touch {} +` или удалить приложение. Найдено 2026-10-29 (`ec34597`, бриф 017).

### I-079. Передача IPA сервису установки: `ci/install-upload.sh`
- **Что.** После ad hoc-сборки, до шифрования: IPA, версия, номер сборки и для каждого бандла bundle ID и entitlements (`codesign -d --entitlements`) уходят на `$PLUMA_INSTALL_URL/ios/adhoc/upload` с токеном `IOSINSTALL_UPLOAD_TOKEN` (через файл заголовка). Сервис на сервере переподписывает IPA для каждого iPhone с сайта. Без токена шаг проходит с сообщением.
- **История.** `2238137`, 2026-11-01, задача 029.

### I-080. Линтер: `ci/lint.sh`, `.shellcheckrc`
- shellcheck для `ci/*.sh` и `brand/*.sh`, actionlint 1.7.12 (скачивается с проверкой SHA-256 для linux_amd64 и darwin_arm64). `.shellcheckrc`: `source-path=SCRIPTDIR/..`.
- **История.** `44fd3ea`, 2026-10-27; `brand/*.sh` добавлен `512ace1`, 2026-10-29.

### I-081. Эталонная сборка Telegram: `reference.yml`, `ci/build-reference.sh`
- **Что и зачем.** Ручной workflow собирает тот же тег без патча, с конфигурацией Telegram `build-system/appstore-configuration.json`, для симулятора: официальный клиент на серверах Telegram для сравнения (App Store-сборка идёт только на устройствах, Telegram не публикует сборку для симулятора). Артефакт `telegram-reference-simulator` (`ph.telegra.Telegraph`, 7 дней), ставится рядом с Pluma. Флаг без профилей пишется в `~/.bazelrc`, поэтому скрипт работает только в CI; отказывается от пропатченного дерева. Читает кэш симуляторной сборки Pluma, свой не сохраняет.
- **История.** `05c9e06`, 2026-10-30; номер задачи в коммите не указан, используется в задаче 020.

### I-082. Dependabot
- `.github/dependabot.yml`: еженедельные PR для действий GitHub (одной группой). Апстрим, Xcode и Bazel обновляются вручную вместе с `patch.py`.
- **История.** `44fd3ea`, 2026-10-27, задача 006.

### I-083. Конвейер бренда: `brand/`
- **Что.** `brand/render.sh` рендерит картинки из SVG набора (`pluma-icon-square.svg`, `pluma-mark.svg`, `pluma-mark-mono.svg`, `pluma-wordmark.svg`) в `brand/ios`; нужен rsvg-convert, ImageMagick 7, Python 3; повторный запуск даёт те же байты (`SOURCE_DATE_EPOCH=0`). `brand/tgs.py` пишет неподвижный знак в `.tgs`. `brand/mark_write.py` пишет анимацию `pluma_mark_write.json` по `writeMark` сайта (кольцо за 1,05 с «на двойках», 13 рисунков; перья едут за пером; одно «дыхание» перьев; покой с 2,11–2,12 с; 60 кадров/с; с `--site` сверяет константы с исходниками сайта, которых в репозитории нет). `brand/pluma-devices.tgs` копируется как есть. Результат коммитится: в CI рендерера нет.
- **Проверка.** `BrandTest` (хэши `mark_write.py` и анимации, список файлов), lint.
- **История.** `512ace1` 2026-10-29 (задача 017); `b0586ec` 2026-10-30 (`pluma-devices.tgs`); `6101991` 2026-10-31 (`pluma-wordmark.svg`, словесный знак); `8314118` 2026-10-31 (`mark_write.py`, прозрачная текстура шара).

### I-084. Минимальная iOS остаётся 13.0 (отменённая правка)
- **Что.** `4c6fe94` (2026-11-01) поднимал `minimum_os_version = "13.0"` в `Telegram/BUILD` до 15.0 (ITMS-90068: iOS 15 с апреля 2027). `ee2ac77` (2026-11-01) откатил: при 15.0 API, устаревшие в iOS 14–15 (`UIApplication.windows` в `AppDelegate.swift` и других), становятся предупреждениями, а 673 модуля апстрима собираются с `-warnings-as-errors`. Ждём, когда Telegram поднимет цель сам.
- **При обновлении апстрима.** Проверить `minimum_os_version` нового релиза. Задача 029.

---

## Прочее: требования App Store Connect

По письму App Store Connect о сборке 20 (2026-11-01), задача 029.

### I-085. Назначение фонового доступа к геопозиции
- **Что и зачем.** ITMS-90683: в Info.plist нужен `NSLocationAlwaysAndWhenInUseUsageDescription`. Каждый `InfoPlist.strings` уже переводит этот ключ; английский текст берётся из `en.lproj/InfoPlist.strings`.
- **Где.** `Telegram/BUILD`, перед `    <key>NSLocationWhenInUseUsageDescription</key>\n` вставлены ключ и строка. `replace`. Значение читается `infoplist_value()` (если ключа в `en.lproj` нет — падение).
- **Проверка.** `verify_store`, `test_patch`.
- **Апстрим / официальный.** Ключа нет в Info.plist.
- **История.** `4c6fe94`, 2026-11-01, задача 029.

### I-086. Фразы Siri для каждого намерения на каждом языке
- **Что и зачем.** ITMS-90626: пример фразы для каждого системного намерения расширения Intents на каждом языке. Апстрим даёт фразу только для отправки сообщения, с именем Telegram, и для большинства языков по-английски. Каждый `AppIntentVocabulary.plist` заменяется пятью фразами на своём языке (`INSendMessageIntent`, `INStartAudioCallIntent`, `INSearchCallHistoryIntent`, `INSearchForMessagesIntent`, `INSetMessageAttributeIntent`) с именем Pluma. Где у апстрима фраза своего языка (ar, de, es, it, ko, nl, pt, ru), фраза сообщения — апстрима с именем Pluma.
- **Где.** `Telegram/Telegram-iOS/<lang>.lproj/AppIntentVocabulary.plist`, 19 языков, `replace_file` по SHA-256 (`SIRI_UPSTREAM`; у be, ca, en, fa, fr, id, ms, pl, tr, uk, uz одинаковый английский файл `eb529c93…`). Список намерений расширения в `Telegram/BUILD` (`IntentsSupported`, 7 строк, включая собственные `SelectFriendsIntent`, `SelectAvatarFriendsIntent` без фраз) — `expect`.
- **Проверка.** `verify_store` (нет слова Telegram, есть все пять намерений), `test_patch`, `test_damage` (`SIRI_VOCABULARY`).
- **Замечание.** Siri выключена в конфигурации (`enable_siri: false`, I-065); фразы нужны только ради требования App Store Connect.
- **История.** `4c6fe94`, 2026-11-01, задача 029.

### I-087. Документация репозитория
- `README.md` — описание патча, бренда, конфигурации, подписи, TestFlight, установки с сайта, сборок и обновления апстрима; дополняется почти каждым коммитом (`c6ff429` 2026-10-27, `a8e56e6` 2026-10-29 и далее). Расхождение README и `patch.py` ничем не проверяется.
- `LICENSE` — GPL v2 (`807e2a8`).

### I-088. Ссылки на Telegram, оставленные намеренно
- Около 120 ссылок на `t.me`, `telegram.org`, `fragment.com` в коде и 54 в строках (ссылки «Поделиться», условия, политика): открываются только по нажатию. Ребрендинг URL не меняет, кроме «Устройств» (I-041). Отдельно открыт вопрос `pluma.chat` вместо `t.me` (задача 026, `todo`).
- Перевод через Google и иконки приложений из App Store в «Открыть в…» загружаются только по запросу пользователя.

### I-089. Проверка связи через google.com (оставлено)
- MtProtoKit пингует `google.com` или `8.8.8.8` (ICMP), когда перестаёт отвечать прокси, настроенный пользователем. Правки нет (README). `verify()` это не ловит: `8.8.8.8` не в диапазонах Telegram.

---

## Пробелы и открытые вопросы

- **Счётчики и хэши.** Почти каждое обновление апстрима сломает `STRINGS_RENAMED`, хэши картинок и `AppIntentVocabulary.plist`, счётчики палитры. Это намеренно: каждое падение требует посмотреть, что изменилось.
- **Не ловится проверками.** Новые `tg://` в других файлах, новые синие вне `TELEGRAM_BLUES`, новые копии таблиц цветов-идентичностей, новые предложения Premium/Stars, адреса и ключи вне трёх сетевых каталогов, обращения к внешним сервисам по именам хостов.
- **Не решено или не сделано.** Миниатюра ночной темы в «Оформлении» с синим пузырём; ободок спидометра «Fast»; значки «Устройств» (`#0079FF`); минимальная iOS 15 (I-084); ссылки `t.me` → `pluma.chat` (задача 026).
- **Не определено по репозиторию.** Номер задачи сборки `reference` (`05c9e06`) в коммите не указан.
