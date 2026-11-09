# android: обновление до нового upstream

Текущая: 12.9.1 (aac1efcb83d6575efb0c819c6ddc4ea6c8cf64e1, layer 228). Новейшая: 13.2.1 (7291). Слой: 233.

## Итог
Новейший релиз: **13.2.1 (7291), коммит `7be672ead283d022812d75c729d3a04dd4c2a2c6`, 2026-11-07, ветка master, layer 233**. Патч переносится недорого: 800 из 828 проверок совпали. **Обновиться отдельно от сервера нельзя**, сначала нужен слой 233.

**Как определён релиз.** Теги в репозитории заканчиваются на `release-11.4.2-5469`, GitHub releases latest показывает его же (2024). Версия взята из `gradle.properties:16-17` (`APP_VERSION_CODE=7291`, `APP_VERSION_NAME=13.2.1`). Что master соответствует публикации в магазинах, из исходников не проверить (не проверено). Ветки dev и dev2 старые и в master не входят. Файла `.tl` в репозитории нет, источник слоя: `TLRPC.java:76`, `LAYER = 233`.

**Слой по релизам** (`TLRPC.java` в каждом коммите «update to»):
| Релиз | Layer |
|---|---|
| 12.9.2 | 228 |
| 12.10.0–12.10.6 | 229 |
| 13.0.0–13.0.3 | 231 |
| 13.1.0–13.1.1 | 232 |
| 13.2.0–13.2.1 | 233 |

От закрепления до 13.2.1 в истории 85 коммитов, из них 17 «update to». Сообщения коммитов пустые по сути, поэтому всё ниже выведено из изменённых файлов, кода и строк. На устройстве ничего не запускалось.

## Круглые видео: что изменилось
Это не правка старой камеры, а **новый рекордер рядом со старым**.

**Старый `InstantCameraView.java`** изменён мало (+86/−28 вместе с `CameraController` и `CameraView`). Теперь наследует `InstantCameraViewBase`, переведён на media3, добавлено журналирование переключения камеры.

**Новое по релизам:**
- **12.10.4 (`c8480176`):** `RoundVideoProgressView`, а также первый `GlassEngine`.
- **12.10.5 (`dc780e81`):** `InstantCameraView2` (1449 строк), `InstantCameraViewBase`, `TelegramRoundVideoUpload`, `RoundVideoSettingsActivity`, ядро `RoundVideo*`, 14 новых строк.
- **13.0.3 (`0aae9598`):** ядро перенесено в `utils/recorder/round/`.
- **13.2.0 (`55356b2a`):** правки почти всех файлов рекордера и новый `RoundVideoTranscoder`.
- **13.2.1 (`7be672ea`):** правка `RoundVideoCameraController`.

**Что получит пользователь (по коду):**
- Запись через Camera2 и MediaCodec, свой MP4-писатель (`RoundVideoCameraController`, `RoundVideoCodecRecorder`, `RoundVideoMp4Writer`).
- Включается по умолчанию: `InstantCameraViewBase.java:59-62` берёт локальный выбор, иначе `!android_round_video_recorder_v2_disabled`, а умолчание ключа false (`AppGlobalConfig.java:109`). У нашего сервера этого ключа в ответе быть не должно (не проверено), значит новый рекордер будет включён у всех.
- Пауза разбивает запись на сегменты: предпросмотр, обрезка, продолжение с того же места (`RoundVideoSession.java:634-665`, `RoundVideoRemuxer`, с 13.2.0 перекодирование через `RoundVideoTranscoder`).
- При сбое камеры сегмент завершается и открывается предпросмотр записанного вместо ошибки (`RoundVideoSession.java:972-978`).
- Переключение камеры идёт с анимированным размытием (`RoundVideoOverlayRenderer`, `TransitionProgram`, `RoundVideoSwitchTimingStore`).
- Подбор экспозиции для слабого света (`RoundVideoCameraController.java:2449-2455`).
- Загрузка на сервер идёт по мере записи (`TelegramRoundVideoUpload`: «growing round-video output»).
- Лимит 60 с (`RoundVideoSession.java:494`).
- Умолчания (`SharedSettings.java:19-37`): 480p, камера High, 30 FPS, 1,2 Мбит/с. Старый рекордер: 384 и 1000 (`MessagesController.java:1637-1638`). Читает ли новый рекордер `round_video_encoding`, я не проверял.
- Экран настроек: 480p/360p, разрешение камеры, 30/60 FPS, битрейт. Он лежит в разделе «Experimental», за флагом `experimentalSettingsAllowed` (`SettingsActivity.java:846-848`).

**Брендинг.** Новый рекордер использует `plane_logo_plain` и `round_blur_overlay_text` (`RoundVideoOverlayRenderer.java:997,1010`). Оба файла по SHA совпали, замены A-030 и A-031 работают и для него.

## Интерфейс и остальное, по релизам
- **13.2.0 — главное обновление интерфейса (914 Java-файлов):** liquid glass (`utils/glass/*`, `GlassRim`, `ProfileButtonsGlass`; жидкое стекло требует Android 13+, `ProfileButtonsGlass.java:207`), новая шапка `SimpleActionBar*` с настройкой кнопки «Готово», `ChatMessageCell` разобран на отдельные классы, боковая панель тем (`TopicsSidePanel`), Compact Mode, `ThemeRevealView`, виджеты в сообщениях (`twidget`, WASM через wamr), пауза live-location.
- **13.2.1:** системная картинка-в-картинке для звонков и трансляций (`utils/pip`, `CallSystemPipView`), вход через Firebase PNV (`FpnvConsent*`), HEIF motion photo.
- **13.1.0:** экран QR-шаринга (`ui/QrShare`), токены отмены платы за сообщения, подарки и кошелёк.
- **13.0.3:** новый диктофон с шумоподавлением RNNoise.
- **13.0.2:** подсветка кода (Prisma), управляемые боты.
- **13.0.0:** кошелёк Gram на TON (41 файл, 252 строки), закрыт ключом `wallet_available`, умолчание равно `DEBUG_VERSION` (`AppGlobalConfig.java:115`).
- **12.10.2 и 12.10.4:** web-прокси.
- **12.10.0:** приветственные сообщения, сообщения к подаркам, расширенные кнопки, таблицы в статьях, блок «неподдерживаемое».

Всего 515 новых строк интерфейса в `values/strings.xml`. Java: 239 файлов добавлено, 966 изменено, 976 удалено (среди них вендоренный exoplayer2).

## Цена патча
Метод: одноразовый скрипт `(одноразовый скрипт)` исполняет копию `ci/patch.py` в режиме «записать и продолжить». Форк не менялся, `config/app.env` и ключи не читались, значения подставные.

Контроль: на закреплённом 12.9.1 совпали **828 из 828**, все `verify` зелёные.

На 13.2.1: **800 из 828, сломано 28**. Из якорей «ровно один раз» совпали 729 из 749, остальные 79 проверок со счётчиками (71 совпала). Сломанные якоря, применённые мягко, не дают каскада: это 28 первичных мест.

| ID | Сломано | Что случилось |
|---|---|---|
| A-001 | 3 | слой, версия, CMake 3.10.2: меняются при любом обновлении |
| A-005 | 1 | в теле `onRequestNewServerIpAndPort` вместо `FirebaseTask` теперь `MozillaDnsLoadTask`; правка остаётся, сменить `must_contain` |
| A-011 | 1 | «Telegram» в `values`: 533 вместо 508 |
| A-014 | 4 | `MessagePreviewView`, `PasscodeView`, два в `NewContactBottomSheet` (ушли в строки) |
| A-016 | 8 | `tg:` переехал: `AndroidUtilities` 13→1, `ChatMessageCell` 2→0, `ProxySettingsActivity` 2→0; `LaunchActivity` 156→160, `LinkManager` 2→3, `ProfileActivity` 76→75 |
| A-030 | 1 | `qr_code_logo_2.json` удалён (ещё в 12.9.2) |
| A-034 | 2 | `ScanQRCodeView` в `SessionsActivity` переписан; ещё 8 проверок «класс не используется» падают как следствие |
| A-066 | 1 | `android-21` → `android-23` |
| A-094 | 2 | `ChatActivity`, `SettingsActivity` |
| A-099 | 2 | хосты: прокси ушёл в `utils/proxy/ProxySettings:238`, кнопки в `ChatMessageBotButtons:213` |
| A-102 | 2 | вставка прокси, тот же перенос |
| A-103 | 1 | ссылка прокси в `SharedConfig` |

Не сломаны: видео (A-079…085, A-107), дни рождения, наборы стикеров, теги Избранного, темы, 400 ключей `.attheme`, tgnet и RSA-ключ.

**После починки `verify` потребует разобрать новые места:**
- `utils/proxy/ProxySettings`: 3 литерала `t.me`, 1 проверка хоста, 8 `tg:`.
- `ChatMessageBotButtons`: 1 хост и 3 `tg:`.
- `WalletActivity`: 1 хост.
- `NotificationsController` и `QrScanOverlay`: по 1 `tg:`.
- Вход Firebase PNV и web-прокси надо осмотреть отдельно: это новые пути, которые ни один якорь не сторожит.

## Сборка
- NDK без изменений: 27.2.12479018.
- CMake 3.10.2 → **3.22.1**; SDK и build-tools 35 → **36**; minSdk 21 → 23; AGP 8.6.1 → 8.13.2; Gradle 8.13 (Dockerfile был 8.7); Kotlin-плагин 1.9.20 → 2.1.0; JDK 17 остаётся.
- **Главное: 19 git-субмодулей** (`.gitmodules`: ffmpeg, boringssl, tgcalls, td, media, jlatexmath и др.). `ci/fetch-upstream.sh` берёт один коммит без субмодулей, его нужно менять. Хосты разные (GitHub, chromium.googlesource.com, личные репозитории dkaraush и Arseny271).
- Новые `buildSrc` и плагин `org.telegram.build-plugin`, Firebase BoM 34.19 с `firebase-pnv`, `gradle.properties` с `-Xmx8g`. Хватит ли этого раннеру, не проверено.
- В репозитории лежат готовые `.a` 16 библиотек на 4 ABI.
- Сама сборка не запускалась.

## Можно ли обновить отдельно
**Нет.** A-001 и ADR 0008 требуют одного слоя у сервера и клиента, и это не формальность. Сверка с `third_party/teamgram-proto/mtproto/schema.tl.crc32.proto`:
- Из 210 новых конструкторов клиента сервер знает 5.
- Неизвестны 64 метода и 141 тип.
- `inputReplyToMessage` теперь `0x75c1ad70`, а сервер знает `0x3bd4b7c2`. По схеме, каждый ответ на сообщение перестанет декодироваться (вывод из схемы, в работе не проверялось).
- Неизвестны также новые `channels.toggleForum` и `messages.acceptUrlAuth`, `replyInlineMarkup`.
- Клиент больше не читает `urlAuthResultRequest` `0x3cd623ec`.
- Обычные ответы сервера 228 клиент читать сможет: старые id оставлены как `*_layer232` и подобные.

**Препятствие выше по цепочке:** последняя версия `teamgram/proto` — v0.231.0 (`git ls-remote`), слоя 233 там нет. ADR 0008 и STATUS предупреждали об этом риске: нужна своя кодогенерация TL или ожидание апстрима.

**Вариант без сервера:** 12.9.2 (`b7561f0c`, layer 228). Совпало 826 из 828, ломаются только версия (A-001) и `qr_code_logo_2.json` (A-030). Но нужны субмодули, изменено 64 Java-файла, и новой камеры и интерфейса там нет.

Клон лежит в `/Users/sckr.a/pluma-work/reference-src-new/android` (2,0 ГБ, partial). Временные деревья удалены.
