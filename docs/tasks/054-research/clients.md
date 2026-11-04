# 054. Исследование: публикация сборок на pluma.chat и самообновление клиентов

Дата: 2026-11-04. Только чтение исходников. Деревья:
- Android — `~/pluma-work/reference-src/android`, коммит `aac1efcb` (12.9.1), совпадает с `UPSTREAM_REF` в `~/pluma-android/upstream.env:4`.
- iOS — `~/pluma-work/reference-src/ios`, коммит `6ad963e5` (release-12.9.2), совпадает с `~/pluma-work/pluma-ios-052/upstream.env:6`.
- Desktop — `~/pluma-work/reference-src/tdesktop`, коммит `a1e89e1f` (v7.0.9), совпадает с `~/pluma-work/pluma-desktop-053/upstream.env:7`. Подмодуль `Telegram/lib_base` в дереве не скачан, поэтому всё, что живёт в нём, помечено НЕ ПРОВЕРЕНО.

Пути ниже даны от корня соответствующего дерева: `TMessagesProj…` — Android, `submodules/…` — iOS, `Telegram/…` — tdesktop. Пометка НЕ ПРОВЕРЕНО значит: по памяти или по косвенным признакам, код на это опираться не должен.

---

## 1. Android

### 1.1 Как официальный клиент проверяет обновления
- **Кто проверяет.** `LaunchActivity.checkAppUpdate` сразу выходит, если сборка не standalone и не beta (`TMessagesProj/src/main/java/org/telegram/ui/LaunchActivity.java:5957-5960`). Флаг даёт модуль приложения: `ApplicationLoader.isStandaloneBuild()` возвращает `isStandalone()` (`ApplicationLoader.java:131-133`), по умолчанию `false` (`:147-149`). `true` он только в модуле `TMessagesProj_AppStandalone` (`TMessagesProj_AppStandalone/.../ApplicationLoaderImpl.java:42-45`). У `TMessagesProj_App` (сборка Google Play) переопределён только id (`TMessagesProj_App/.../ApplicationLoaderImpl.java:5-10`).
- **Флаги BuildVars.** `CHECK_UPDATES = true` (`BuildVars.java:25`). Это только автоматическая проверка: при `force` она идёт и без него (`LaunchActivity.java:5961`).
- **Когда.**
  - При каждом возврате в приложение (`LaunchActivity.java:7026`), не чаще `updateCheckDelay`. По умолчанию это 24 часа (`MessagesController.java:1590`), сервер меняет его ключом `inapp_update_check_delay` в app config (`MessagesController.java:3239-3245`).
  - Вручную — пунктом «Check app update» в отладочном меню (`SettingsActivity.java:1430`, `:1539`).
- **Запрос.** `help.getAppUpdate` с `source` = имя пакета установщика (`getInstallerPackageName`), иначе пустая строка (`LaunchActivity.java:5988-5995`).
- **Сравнение версий** — на клиенте:
  - `SharedConfig.setNewAppVersionAvailable` сравнивает `update.version` с `versionName` пакета (`SharedConfig.java:785-808`);
  - `versionBiggerOrEqual` разбивает обе строки по точкам, делает `Integer.parseInt` и сравнивает только общее число компонент (`:811-824`).

  Следствия:
  1. `"12.9.1"` против `"12.9.1.5"` даёт «не меньше» — обновления нет;
  2. нечисловая версия (`12.9.1-pluma`) бросит `NumberFormatException` в UI-потоке (вызов в `runOnUIThread`, `LaunchActivity.java:6003-6007`).

  Версия в ответе — только цифры через точку, а число компонент у установленной и новой сборки должно совпадать.
- **UI.**
  - `can_not_skip` → полноэкранный `BlockingUpdateView` (`LaunchActivity.java:6009-6010`, `:7023-7024`).
  - Иначе в standalone — нижний лист `UpdateAppAlertDialog` (`LaunchActivity.java:6011-6012` → `ApplicationLoaderImpl.java:101-109` standalone). Лист показывает `sticker`, если он есть (`TMessagesProj_AppStandalone/.../UpdateAppAlertDialog.java:235-244`), строку «версия • размер» из `appUpdate.document.size` (`:263`) и текст с entities. Кнопка «Download now» запускает `FileLoader.loadFile(document, "update", …)` (`:291-294`).
  - Плашка внизу бокового меню (`UpdateLayout.java:46-80`, standalone): скачать / отменить / после загрузки `AndroidUtilities.openForView(document)`. Плашка видна только при `SharedConfig.isAppUpdateAvailable()`, а тот требует `pendingAppUpdate.document != null` и standalone (`SharedConfig.java:770-783`).
- **Скачивание.** APK — это обычный `Document` из `help.appUpdate.document`, его качают по MTProto (`upload.getFile`) через `FileLoader`. Канал не нужен.
  - Устаревшую `file_reference` клиент обновляет повторным `help.getAppUpdate`: parent `"update"` в `FileRefController.java:643-651`.
  - Поле `url` используется только в `BlockingUpdateView`, если документа нет: `Browser.openUrl(appUpdate.url)` (`BlockingUpdateView.java:142-152`). В standalone-листе документ обязателен: `appUpdate.document.size` без проверки на null (`UpdateAppAlertDialog.java:263`).
- **Установка.**
  - `openForView` проверяет MIME `application/vnd.android.package-archive` по расширению имени файла. Если `canRequestPackageInstalls()` = false, клиент показывает диалог `ApkRestricted` с переходом в `ACTION_MANAGE_UNKNOWN_APP_SOURCES` (`AndroidUtilities.java:4271-4300`, `AlertsCreator.java:275-288`).
  - Затем `ACTION_VIEW` через FileProvider `${applicationId}.provider` (`AndroidManifest.xml:600`) — системный установщик.
  - Разрешение `REQUEST_INSTALL_PACKAGES` объявлено в манифесте библиотеки (`TMessagesProj/src/main/AndroidManifest.xml:62`), поэтому оно есть во всех вариантах, и в нашем APK тоже: `ci/patch.py` его не трогает (поиск `INSTALL` по `~/pluma-android/ci/patch.py` пуст).
  - Тихой установки нет: подтверждает пользователь в системном диалоге.

### 1.2 Что должен отвечать сервер
TL (`TLRPC.java:5430-5479`): `help.appUpdate#ccbbce30 flags can_not_skip:flags.0 id:int version:string text:string entities:Vector<MessageEntity> document:flags.1?Document url:flags.2?string sticker:flags.3?Document`, а без обновления — `help.noAppUpdate#c45a6536`.

Для Android:
- `version` — `versionName` новой сборки (цифры через точку, то же число компонент);
- `text` и `entities` — список изменений;
- `document` — APK в нашем DFS. Обязательно: имя файла `*.apk` (атрибут filename) и mime `application/vnd.android.package-archive`, иначе `openForView` не распознает APK;
- `id` — номер выпуска;
- `can_not_skip` — по решению владельца;
- `sticker` — необязателен;
- `url` — запасной путь (страница загрузки), только для блокирующего экрана.

Сейчас наш `help.getAppUpdate` — заглушка upstream: всегда `help.noAppUpdate` и лог про лицензию (`app/bff/configuration/internal/core/help.getAppUpdate_handler.go:27-34`).

### 1.3 Что собирает наш форк
- **Что собирается.** `:TMessagesProj_App:assembleAfatRelease` (`~/pluma-android/ci/build.sh:12-14`): модуль Google Play, flavour `afat`, buildType `release`. `isStandalone()` = false, проверка обновлений выключена (п. 1.1).
- **Модуль standalone выброшен** из `settings.gradle` (`~/pluma-android/ci/patch.py:351-352`).
- **Версии.** `versionName` = `APP_VERSION_NAME` upstream, `12.9.1` (`gradle.properties:17`, `ci/patch.py:276` его только проверяет). `versionCode` = `6979*10+9` (`TMessagesProj_App/build.gradle:157-163`). У всех сборок Pluma на одном upstream версия одинакова, отличить их нельзя.

### 1.4 Наименьший патч
Брать модуль `TMessagesProj_AppStandalone` целиком **нельзя**:
- он меняет пакет на `chat.pluma.app.web` (`applicationIdSuffix ".web"`, `TMessagesProj_AppStandalone/build.gradle:67,77`), и это уже не обновление установленного приложения;
- он добавляет SMS-задания Telegram с разрешениями `SEND_SMS`, `READ_PHONE_STATE`, `READ_PHONE_NUMBERS` (`TMessagesProj/config/release/AndroidManifest_standalone.xml:32-34`, `ApplicationLoaderImpl.java:116-200`).

Предлагаемая правка в `ci/patch.py`, в модуле `TMessagesProj_App`:
1. В `TMessagesProj_App/.../ApplicationLoaderImpl.java` добавить переопределения, перенесённые из standalone (`TMessagesProj_AppStandalone/.../ApplicationLoaderImpl.java:42-114`): `isStandalone()` → true, `checkApkInstallPermissions`, `openApkInstall`, `showUpdateAppPopup`, `takeUpdateLayout`.
2. Скопировать `UpdateAppAlertDialog.java` и `UpdateLayout.java` (543 строки, `TMessagesProj_AppStandalone/src/main/java/org/telegram/ui/Components/`). Оба используют только `org.telegram.messenger.R` библиотеки (`UpdateLayout.java:21`, `UpdateAppAlertDialog.java:30`), то есть переносятся как есть, по SHA-256, как остальные правки патча.
3. Версия: `versionName` в 4 компонентах с первой же сборки для сайта (`12.9.1.N`, N — номер сборки Pluma), `versionCode` растёт с каждой сборкой. Например, `(APP_VERSION_CODE*1000+N)*10+9`: верхний предел int это выдерживает. То, что Android не ставит обновление с меньшим `versionCode`, — НЕ ПРОВЕРЕНО по исходникам (поведение PackageManager), но это общеизвестное правило.

Побочные эффекты `isStandalone()=true`, все прочитаны:
- `useInvoiceBilling` (`BuildVars.java:63`);
- SMS-хеш `getSmsHash` (`BuildVars.java:96`);
- пункт Stars в настройках (`SettingsActivity.java:712`, `ProfileActivity.java:10628`);
- `TONIntroActivity.java:103`;
- URL управления Premium (`MessagesController.java:1776`);
- подпись версии «direct» (`AndroidUtilities.java:6708`, `SettingsActivity.java:929`, `LoginActivity.java:4072`);
- фильтр `restriction_reason` платформы android (`MessagesController.java:22496`, `MessageObject.java:658`);
- кнопки «обновить приложение» у неподдерживаемых сообщений и историй ведут в `checkAppUpdate` вместо Google Play (`ChatActivity.java:41304-41326`, `PeerStoriesView.java:4416-4419`).

Для Pluma заметна только строка Stars, и её стоит проверить в клиенте; остальное либо не используется (биллинга, Premium и restriction_reason нет), либо даже правильнее (обновления идут с нашего сервера, а не из Google Play).

Строка «Update Telegram» (`values/strings.xml:2762`) уже переименовывается брендовым патчем: все «Telegram» во встроенных строках (`~/pluma-android/README.md`, раздел Branding; `ci/patch.py:1416`).

---

## 2. iOS

### 2.1 Что делает официальный клиент
- **Опрос.** TelegramCore опрашивает `help.getAppUpdate(source: "")` при старте аккаунта и затем каждые 12 часов (`submodules/TelegramCore/Sources/State/AppUpdate.swift:37-49`, запуск — `AccountTaskManager.swift:142`).
- **Сравнения версий на клиенте нет.** Любой `help.appUpdate` становится `AppUpdateInfo`, `noAppUpdate` — nil. Клиент берёт только `blocking` (flags.0), `version`, `text`, `entities`; `document`, `url` и `sticker` отбрасываются (`AppUpdate.swift:8-35`). Решать, есть ли обновление, должен сервер.
- **Экран.** На каждое новое значение показывается экран «Обновление» (`submodules/TelegramUI/Sources/ApplicationContext.swift:586-596`) с кнопкой «Пропустить», если не blocking (`UpdateInfoController.swift:134`).
- **Кнопка «Обновить»** вызывает `openAppStorePage` (`UpdateInfoController.swift:76-79`, `:117-118`), а та открывает `itms-apps://itunes.apple.com/app/id<appStoreId>` (`submodules/TelegramUI/Sources/AppDelegate.swift:889-893`). У нас `appstore_id = "0"` (`~/pluma-work/pluma-ios-052/ci/patch.py:855`), так что кнопка сейчас открывает несуществующую страницу App Store.
- **Какую версию клиент сообщает серверу.** В `initConnection` уходит только `CFBundleShortVersionString` (`AppDelegate.swift:528`). Сборка форка требует, чтобы она совпадала с `UPSTREAM_VERSION` (`~/pluma-work/pluma-ios-052/ci/build.sh:109-111`), а номер сборки (`CFBundleVersion` = `BUILD_NUMBER` = номер запуска CI, `ios.yml:176`) серверу не доходит. Значит, сервер не отличит две сборки Pluma на одном upstream.

### 2.2 Может ли ad hoc-сборка предложить «обновить» по itms-services
Да, с патчем клиента. Нужно:
1. Сохранять `url` из `help.appUpdate` в `AppUpdateInfo` (`AppUpdate.swift:8-35`).
2. Открывать его в `UpdateInfoController` вместо `openAppStorePage` (`UpdateInfoController.swift:117-119`) через `UIApplication.shared.open`.
3. Отдавать в `app_version` номер сборки, например `"12.9.2 (N)"` (`AppDelegate.swift:528`), либо перейти на `CFBundleShortVersionString` вида `12.9.2.N`. Сервис `iosinstall` принимает до 4 компонент (`app/ops/iosinstall/server.go:517`). Что App Store Connect не примет 4 компоненты для TestFlight-сборки — НЕ ПРОВЕРЕНО. Поэтому надёжнее первый вариант, номер в `app_version`.

Какой `url` отдавать:
- Ссылка `itms-services` у `iosinstall` своя для каждого устройства: манифест и IPA выдаются только по токену устройства (`server.go:338-357`, `:359-427`). MTProto-сервер не знает ни UDID, ни токен.
- Поэтому `url` — страница сайта `https://pluma.chat/iphone/install`. Она хранит токен устройства в `localStorage` Safari (`~/pluma-site/public/iphone/install/install.js:110-118`) и сама получает свежую ссылку из `/ios/adhoc/status` (`install.js:221`). На ней пользователь нажимает «Установить».
- Если Safari потерял `localStorage`, страница начнёт регистрацию заново. Что WebKit стирает хранилище сайта без посещений через 7 дней (ITP) — НЕ ПРОВЕРЕНО.
- Альтернатива: общая ссылка на манифест без токена устройства. IPA ставится только на зарегистрированные устройства, а адрес сервера и так публичен. Это решение владельца (п. 5).

### 2.3 Что сейчас делает `iosinstall`
`app/ops/iosinstall/main.go:16-37`, `server.go:38-50`, `docs/ops/ios-install.md`:
- **Маршруты:** `GET /ios/adhoc/profile` (Profile Service), `POST /ios/adhoc/enroll` (UDID с подписью Apple), `GET /ios/adhoc/status?t=`, `GET /ios/adhoc/manifest?t=`, `GET /ios/adhoc/app.ipa?t=`, `POST /ios/adhoc/upload` (Bearer-токен CI).
- **Подпись.** Сервис регистрирует новые айфоны у Apple, пересоздаёт ad hoc-профили и переподписывает IPA через `rcodesign` (`ios-install.md:23-28`).
- **Выпуск.** Для всех один IPA; номер версии хранится в `st.Build.Version` и попадает в манифест (`server.go:350`, `:393-394`).
- **Загрузка из CI.** Её делает шаг `Hand the IPA to the install service` (`.github/workflows/ios.yml:242-250` → `ci/install-upload.sh`): IPA, версия, номер сборки и entitlements бандлов.

Сервис уже обновляет *раздачу*: новая сборка из CI сразу становится доступна всем зарегистрированным. Но приложение об этом не знает, потому что `help.getAppUpdate` — заглушка.

### 2.4 Честные ограничения
- **Тихого самообновления на iOS нет.**
  - App Store-приложение не ставит обновления само (кнопка ведёт в App Store, п. 2.1).
  - Ad hoc ставится только через системный диалог itms-services, который пользователь подтверждает.
  - Для ad hoc на iOS 16+ нужен «Режим разработчика» (`docs/ops/ios-install.md:9`).
- **Лимит Apple** — 100 айфонов в год (`ios-install.md:9`, `:57`).
- **TestFlight.** Сайт сейчас ведёт iPhone и iPad в TestFlight (`~/pluma-site/src/install/releases.ts:47-51`). Такие сборки обновляет приложение TestFlight, наш `url` им не нужен. Сервер не отличит TestFlight-сборку от ad hoc по `app_version`: у них один `BUILD_NUMBER` (`ios.yml:306`). Значит, нужен ещё признак в `app_version` либо один канал установки (решение владельца).
- Что при установке по itms-services поверх существующего приложения с тем же bundle ID данные сохраняются — НЕ ПРОВЕРЕНО (поведение iOS).

---

## 3. Desktop (tdesktop)

### 3.1 Как устроен апдейтер
- **Свой апдейтер на всех трёх системах, Sparkle не используется:**
  - `Telegram/SourceFiles/core/update_checker.cpp` проверяет, качает, проверяет подпись и распаковывает в `tupdates/temp`;
  - отдельный бинарник `Updater` (`_other/updater_win.cpp`, `updater_osx.m`, `updater_linux.cpp`) заменяет файлы после выхода приложения.

  Цель `Updater` собирается, только если не выключен автоапдейт и это не сборка для магазинов (`Telegram/CMakeLists.txt:2411-2425`). На macOS он кладётся в `Contents/Frameworks/Updater` (`CMakeLists.txt:2455-2458`).
- **Источники.** Одновременно запускаются HTTP- и MTP-проверка (`update_checker.cpp:1505-1513`):
  - **HTTP.** `GET <prefix>/current`, с суффиксом `AutoUpdateVersion()`, если он больше 1 (`:717-721`).
    - Префикс по умолчанию — `https://td.telegram.org` (`Telegram/SourceFiles/storage/localstorage.cpp:557`). Он хранится в `tdata/prefix` (`:536-541`).
    - Сервер его меняет: `config.autoupdate_url_prefix` из `help.getConfig` записывается через `writeAutoupdatePrefix` (`mtproto/mtp_instance.cpp:935-937`, `localstorage.cpp:560-578`).
    - Ответ — JSON `{ "<платформа>": { "stable"|"beta"|"alpha": { "released"|"testing": "<версия>", "link": "<путь>" } } }` (`update_checker.cpp:606-670`, `:804-836`). `link` дописывается к префиксу, `{version}` в нём подставляется (`:835`, `:847-849`). Старый формат — строка `версия:url` (`:787-802`).
    - Версия — целое `AppVersion`; оно должно быть больше текущего (`:842-846`).
  - **MTP.** Канал `tdhbcfeed` (плюс номер `AutoUpdateVersion`): последнее сообщение — JSON того же вида, где `released` = `"<версия>:<username>#<postId>"`. Файл качается из этого поста `DedicatedLoader`'ом (`update_checker.cpp:994-1092`). Нам он не нужен: у нашего сервера такого канала нет, проверка просто упадёт, а HTTP продолжит работу (`:1633-1645`).
  - **Ключи платформ** `Platform::AutoUpdateKey()` и номер `AutoUpdateVersion()` живут в `lib_base`, которого в дереве нет — НЕ ПРОВЕРЕНО. По памяти: `win`, `win64`, `winarm`, `mac`, `armmac`, `linux`. Перед реализацией прочитать `lib_base` на коммите подмодуля тега v7.0.9.
- **Имена пакетов** (Packer, `Telegram/SourceFiles/_other/packer.cpp:498-502`; `Telegram/build/build.sh:57,85-86`, `deploy.sh:88-108`): `tupdate<ver>` (win32), `tx64upd<ver>`, `tarm64upd<ver>`, `tmacupd<ver>`, `tarmacupd<ver>`, `tlinuxupd<ver>`.
- **Формат пакета** (`update_checker.cpp:327-576`, `packer.cpp:220-330`, `:440-470`):
  - заголовок: RSA-подпись 128 байт, SHA-1 20 байт, на Windows ещё LZMA props 5 байт, исходный размер int32;
  - тело: LZMA (Windows, LZMA SDK) или xz (остальные);
  - SHA-1 считается по телу (props + размер + сжатые данные);
  - подпись — `RSA_sign(NID_sha1)` над этим SHA-1, ключ строго 1024 бита (`packer.cpp:459`: `RSA_size == 128`);
  - внутри `QDataStream` (Qt_5_1): `quint32 version` (или `0x7FFFFFFF` и `quint64 alphaVersion`), `quint32 filesCount`, затем для каждого файла `QString name`, `quint32 size`, `QByteArray data` и, кроме Windows, `bool executable` (`update_checker.cpp:470-560`, `packer.cpp:250-275`).
- **Ключи проверки** — два публичных RSA-1024 в `Telegram/SourceFiles/config.h:45-58` (`UpdatesPublicKey`, `UpdatesPublicBetaKey`). Клиент пробует ключ своего канала, затем второй (`update_checker.cpp:369-402`).
- **Ключи подписи.** Packer берёт приватные из `../../../../DesktopPrivate/packer_private.h` и `alpha_private.h` (`packer.cpp:30-33`). Цель Packer есть только при `DESKTOP_APP_SPECIAL_TARGET` (`CMakeLists.txt:2461-2486`). Альфа-сборкам нужна ещё подпись имени файла отдельным ключом (`update_checker.cpp:1923-1961`).
- **Установка по платформам:**
  - **Windows** — `Updater.exe` рядом с exe (`update_checker.cpp:1800-1802`). Если каталог защищён от записи (Program Files), включается write-protected (`:1817-1822`). Updater подменяет `Telegram.exe` в пакете на реальное имя exe из `-exename` (`updater_win.cpp:207-210`, `:375-388`). Он же пишет в реестр «Telegram Desktop» и «Telegram FZ-LLC» (`updater_win.cpp:300-320`): если будет установщик, это надо переименовать.
  - **macOS** — `Updater` в `Contents/Frameworks`. И проверка, и Updater ждут в пакете бандл с именем `Telegram.app` (`update_checker.cpp:1803-1805`, `updater_osx.m:11`, `:170`). Реальное имя установленного приложения Updater подставляет сам (`updater_osx.m:77-91`, `:208-213`). Карантин снимается (`update_checker.cpp:1872-1875`). Пакет должен содержать уже подписанный и нотаризованный бандл под именем `Telegram.app`, либо патч меняет это имя в обоих местах. Что переименование каталога бандла не ломает подпись — НЕ ПРОВЕРЕНО.
  - **Linux** — `Updater` рядом с бинарником (`update_checker.cpp:1806-1807`), имя бинарника подменяется так же (`updater_linux.cpp:272-276`, `:400-402`).
- **Без апдейтера** «Обновить» открывает `https://desktop.telegram.org` (`update_checker.cpp:1880-1897`).
- `help.getAppUpdate` tdesktop не вызывает вовсе (поиск `MTPhelp_GetAppUpdate` по `SourceFiles` пуст).

### 3.2 Что у нас сейчас
- **Апдейтер вырезан намеренно.** Патч заставляет CMake падать без `DESKTOP_APP_DISABLE_AUTOUPDATE=ON` и всегда задаёт `TDESKTOP_DISABLE_AUTOUPDATE` (`~/pluma-work/pluma-desktop-053/ci/patch.py:500-531`; Linux — `ci/build-linux.sh:70`). Ключи Telegram остаются только потому, что «verify nothing» (`patch.py:48-54`).
- **Пакеты** (`ci/package.sh:1-12`):
  - Windows — zip с одним `Pluma/Pluma.exe`, без `Updater.exe`, без установщика и без подписи Authenticode (`signtool` в `ci/` не найден);
  - Linux — tar.xz с `Pluma/Pluma` и `.desktop`;
  - macOS — zip с `Pluma.app`, подпись ad hoc; Developer ID и нотаризация — в job `sign-macos` (`desktop.yml:540-620`, README `:157`).
- `AppVersion = 7000009` (`Telegram/SourceFiles/core/version.h:25`). Патч проверяет `AppVersionStr` (`patch.py:1603`), версию не меняет.

### 3.3 Что нужно сделать и разместить
1. **Патч.**
   - Включить апдейтер: убрать `DISABLE_AUTOUPDATE` из обязательных опций (`patch.py:514-531`), собрать `Updater`. Crash reports остаются выключены.
   - Свои публичные ключи вместо `config.h:45-58`.
   - Префикс по умолчанию `localstorage.cpp:557` → `https://dl.pluma.chat/desktop`, чтобы первая проверка до `help.getConfig` не шла к Telegram.
   - Имена `Telegram.app` и реестра — по п. 3.1.
   - Строки `CODE_TELEGRAM` и проверки «нет адресов Telegram» в `patch.py` (`:928-934`) обновить.
   - MTP-проверку отключить (`update_checker.cpp:1509-1511`), чтобы клиент не просил у сервера канал `tdhbcfeed`. Её отказ безвреден, но это лишний запрос.
2. **Сервер:** `help.getConfig` отдаёт `autoupdate_url_prefix` (поле 37 в `third_party/teamgram-proto/mtproto/schema.tl.sync.proto:2395`; сейчас не задаётся, поиск по `app/` пуст). Android и iOS это поле не используют: iOS только сохраняет его (`ManagedConfigurationUpdates.swift:45`).
3. **CI**, в `release` после сборки:
   - собрать Packer со своим `packer_private.h`, который создаётся из секрета окружения во временном каталоге и стирается;
   - упаковать `Telegram(.exe)` + `Updater` либо `Telegram.app`;
   - выложить `t*upd<AppVersion>` и обновить `current`.

   Пакет Windows/Linux — `-path <бинарник> -path Updater` (`build.sh:175`), macOS — `-path <bundle>.app -target … -arch …` (`build.sh:433`).
4. **Хостинг:** `dl.pluma.chat/desktop/current` (JSON по п. 3.1) и файлы пакетов. Полные установщики (dmg, zip или setup.exe, tar.xz) — для первой установки с сайта.
5. **Версия.** Пакет принимается, только если `version > AppVersion` (`update_checker.cpp:491-494`, `:1787-1790`). Пересборка на том же upstream требует нового `AppVersion`. Способы (решение — в п. 5):
   - (а) механизм альф upstream: `AlphaVersion = AppVersion*1000 + N`, нужен третий ключ и подпись имени файла (`update_checker.cpp:479-488`, `:1923-1961`). Альфу MTP-путь отвергает (`:1059-1061`);
   - (б) сдвигать `AppVersion` патчем, например `7000009` → `7000009 + N` (отображение `FormatVersionDisplay`, `core/changelogs.cpp:151-157`, покажет 7.0.9+N). Это столкнётся с номером следующего upstream.
6. **Ключи.** Формат задан клиентом: RSA-1024 (PKCS#1 PEM, `PEM_read_bio_RSAPublicKey`) и подпись SHA-1. Две пары (stable и beta) или одна пара в обоих слотах. Более стойкий алгоритм — отклонение от upstream в двух файлах (`update_checker.cpp`, `packer.cpp`). Ключи генерирует владелец, приватный ключ лежит только в `release`-окружении и в резервной копии.

---

## 4. Сегодняшний конвейер и предлагаемая автоматизация

### 4.1 Что есть сейчас

| Форк | Что собирает | Подпись и одобрение | Куда попадает |
|---|---|---|---|
| Android (`~/pluma-android/.github/workflows/android.yml`) | APK `afat release` с тестовой подписью upstream (`:115-121`, `ci/build.sh`), на push в main, по ручному запуску, раз в неделю (canary) | job `sign` в окружении `release` (`:145-150`); у `release` обязательный ревьюер (владелец) и только ветка `main` (README `:119-120`, `:131`) | артификат `pluma-android`, зашифрованный age, 30 дней (`:191-198`); дальше никуда |
| iOS (`~/pluma-work/pluma-ios-052/.github/workflows/ios.yml`) | симулятор; ad hoc IPA (`build-ipa`); по галочке App Store IPA → TestFlight | `preflight-sign` и `build-ipa` в `release`: два одобрения (`:137-176`, README `:194`) | IPA уходит в `iosinstall` (`:242-250`) и в зашифрованный артификат `pluma-ios` на 30 дней (`:251-261`); TestFlight — `ci/testflight.sh` (`:366-373`) |
| Desktop (`~/pluma-work/pluma-desktop-053/.github/workflows/desktop.yml`) | Linux tar.xz, Windows zip, macOS zip (`ci/package.sh`) | `sign-macos` в `release` с одобрением (README `:157`); Windows и Linux не подписываются | зашифрованные артификаты, 30 дней для сервера (`:232-236`, `:392-396`, `:534`), `pluma-desktop-macos-signed` (`:612-619`) |
| Web (`~/pluma-web/.github/workflows/web.yml`) | бандл Web K | без подписи | образ `ghcr.io/sckraaa/pluma-web:<sha>` (`:141-151`), сервер ставит его через `.env` (`docs/ops/deployment.md:94-99`) |

- **Сайт сейчас.** Загрузки записаны в коде: в `~/pluma-site/src/install/releases.ts:49-56` заглушки `dl.pluma.chat/{android,mac,windows,linux}`, а iPhone ведёт в TestFlight. Сборка падает на неполной записи (`releases.ts:67-91`). Выпуск обновляется `rsync` и переключением ссылки (`~/pluma-site/docs/ops/deploy.md`). Владелец велел оставить заглушки (`~/pluma-site/docs/questions.md`, Q1).
- **Хоста `dl.pluma.chat` нет.** В Caddyfile только блоки `{$PLUMA_WEB_DOMAIN}` и `{$PLUMA_SITE_DOMAINS}` (`teamgramd/deploy/web/Caddyfile:9`, `:30`).
- Сборки из CI на сайт не попадают совсем, кроме ad hoc IPA, которую получает `iosinstall`.

### 4.2 Предложение

**Сервис выпусков** на хосте `neo_old` (по образцу `iosinstall`), например `app/ops/releases`:
- **Приём.** `POST /releases/upload` с Bearer-токеном загрузки (секрет `RELEASES_UPLOAD_TOKEN` в окружении `release` каждого форка, ставится скриптом, как `put-secrets.sh` у `iosinstall`, `ios-install.md:41-46`). CI отправляет подписанные файлы сразу на VPS, без публичных артификатов, как требует ADR 0010 (`docs/adr/0010-cloud-client-builds.md:12,14`). Вместе с файлами идут платформа, версия, номер сборки, SHA-256 и текст изменений.
- **Хранение.** Том `releases_data`; неизменяемые файлы `<платформа>/<версия>/…` плюс указатель «текущий». Откат — переключение указателя.
- **Раздача.** `dl.pluma.chat` через Caddy:
  - постоянные адреса из `releases.ts`;
  - версионные файлы;
  - `desktop/current` и `t*upd<ver>`;
  - `/releases.json` для сайта.

  Сайт читает `/releases.json` при загрузке страницы, как уже делает с `/progress.json` (`~/pluma-site/docs/STATUS.md:12`). Тогда новая сборка появляется на сайте без пересборки сайта, а `releases.ts` остаётся описанием формы и запасом.
- **Связь с MTProto-сервером:**
  - **Android.** Сервис кладёт APK в DFS как документ (системный владелец, имя `Pluma-<ver>.apk`, mime APK) и пишет запись в таблицу выпусков: `app_releases(platform, version, build, document_id, text, entities, can_not_skip, created_at)`, миграция `migrate-*.sql`.
  - **iOS и Desktop.** Запись без документа; у iOS с `url`.

**`help.getAppUpdate`** (bff configuration, вместо заглушки `help.getAppUpdate_handler.go:27-34`):
- **Платформа** — по `api_id` сессии: 10001 Android, 10002 iOS, 10003 Desktop, 10004 Web (`~/pluma-android/config/app.env:4-6`, `~/pluma-work/pluma-ios-052/config/app.env`). `api_id` и `app_version` есть в авторизации: `authsession_getAuthorization` (`app/service/authsession/authsession/authsession.tl.proto:331`, `app_version` — `internal/dao/auths.go:326`). В `RpcMetadata` их нет (`third_party/teamgram-proto/mtproto/rpc/metadata/rpc_metadata.proto:29-60`).
- **Android:**
  - установленная версия — `versionName` из `app_version` вида `"12.9.1.N (code)"` (`ConnectionsManager.java:223`);
  - если последний выпуск новее — `help.appUpdate` с `document` и `version` = `versionName`, иначе `help.noAppUpdate`;
  - сравнение — по тому же правилу, что у клиента (`SharedConfig.java:811-824`).
- **iOS** (после патча п. 2.2): по номеру сборки из `app_version`; `url` = страница `/iphone/install`. Клиент не сравнивает версии сам, поэтому ошибка сервера показала бы экран обновления при каждом запуске.
- **Desktop и Web:** всегда `help.noAppUpdate`. Desktop обновляется через `current`, Web — с сервера, при перезагрузке страницы. Кэширование Service Worker'ом Web K — НЕ ПРОВЕРЕНО.
- **Кэш** — короткий в памяти bff. Ошибка таблицы → `noAppUpdate` и лог, не 500.

**Что патчит каждый форк:**
- **Android** — п. 1.4: standalone-переопределения и два файла UI; `versionName` из 4 компонент и растущий `versionCode` из номера сборки; шаг CI «загрузить в сервис выпусков» после `sign`.
- **iOS** — п. 2.2: `url` в `AppUpdateInfo`, открытие `url` вместо App Store, номер сборки в `app_version`; шаг загрузки уже есть (`iosinstall`), сервису выпусков нужна только запись о версии (её может отдать `iosinstall`).
- **Desktop** — п. 3.3: включённый апдейтер, свои ключи, префикс `dl.pluma.chat/desktop`, имена бандла и реестра, схема `AppVersion`, Packer в `release`, загрузка пакетов и установщиков.
- **Web** — ничего.

---

## 5. Что решает владелец
1. **Одобрение.** Сейчас каждый подписанный выпуск ждёт одобрения в `release`: Android — 1, iOS — 2, macOS — 1. Варианты:
   - оставить: публикация автоматическая после одного клика на платформу;
   - убрать обязательного ревьюера и подписывать каждый push в main;
   - подписывать и публиковать только по тегу.
2. **Ключ подписи обновлений Desktop.** Сгенерировать RSA-1024 (формат tdesktop: SHA-1, 128 байт) — одну пару или stable+beta, плюс ключ альф, если выбран путь (а) из п. 3.3. Где хранить резервную копию. Принять слабый формат upstream или патчить на стойкий.
3. **Схема версий Desktop:** альфы upstream или сдвиг `AppVersion` (п. 3.3, шаг 5).
4. **Windows.** Сертификат Authenticode (сейчас подписи нет, SmartScreen будет предупреждать). Установщик (Inno `setup.iss` upstream) или переносной zip; Updater требует каталог, доступный на запись.
5. **iOS.**
   - Основной канал на сайте — TestFlight (сейчас) или ad hoc `iosinstall`.
   - Разрешить ли общую ссылку на манифест без токена устройства (п. 2.2).
   - `can_not_skip` для критичных выпусков.
6. **Android.** Схема `12.9.1.N`; один и тот же release-ключ навсегда (README форка `:112`: обновление ставится только с тем же ключом).
7. **Домен `dl.pluma.chat`.** DNS-запись и маршрут Caddy; новый токен загрузки в `release` трёх форков.
8. **Текст изменений:** откуда берётся (файл в форке, описание запуска) и на каких языках. Пользовательские тексты — ключами локализации (правило проекта), а `help.appUpdate.text` — свободный текст сервера.
9. **Каналы.** Каналы для раздачи (`tdhbcfeed` у Desktop, документ в канале у Android) не нужны: Android качает документ напрямую по `help.appUpdate`, Desktop — по HTTP. Подтвердить, что их не делаем.
