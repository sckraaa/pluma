# 114. Полная локализация четырёх клиентов: языковые пакеты, свои строки, подсказка языка

- Статус: серверная часть сделана (2026-11-19, ветка `task/114-server`, не влито, не выкачено); клиентская (встроенный ru Android и др.) — отдельные ветки клиентов.
- Поставлена владельцем 2026-11-18: на русском в Android часть интерфейса по-английски («запросы разрешений, кнопки»); «надо проверить на наличие этой проблемы все клиенты».
- Решение: дополнения к ADR 0025 (подсказка языка по `system_lang_code`, рассылка `updateLangPack`) и к ADR 0046 (свои строки для всех пакетов, включая Android; полнота по языкам; проверка ключей патчей). Если дополнения выйдут больше раздела — новое ADR со следующим свободным номером (0073 занят задачей 113).
- Зависит от: 021 (языковые пакеты, ADR 0025), 076 и 080 (свои строки и `override.json`, ADR 0046), 094 (ключи групповых звонков E2E, ADR 0060:323).
- Ветки (план): сервер `task/114-langpack-coverage`; клиенты — только если владелец выберет пункты 6–8 («Решения к подтверждению»): `task/114-ios-infoplist`, `task/114-desktop-infoplist`, `task/114-android-bundled-ru`.
- Клиенты на момент обследования (`upstream.env` веток 109): Android DrKLO 13.2.1 `7be672ea`, iOS release-13.2 `24638bc6`, tdesktop v7.4.1 `157f5cfc`, tweb `aa74832e` (2.3/686). Все слоя 233.

## Что обследовано (2026-11-18)
Прочитаны исходники закреплённых клиентов и патчи Pluma (`ci/patch.py` веток 109), код сервера (`pkg/langpack`, `app/bff/langpack`, `app/bff/configuration`), прод на домашнем сервере (только `SELECT`: `teamgram.langpack_languages`, `langpack_strings`, `auths`), официальные пакеты Telegram через `langpack.*` из Web K. Ничего не менялось. Сравнение — ключи клиента против живых строк сервера (`deleted=0`, непустые), множественные формы приведены к правилам клиента.

### Источник: что отдаёт Telegram
`langpack.getLanguages` / `getLangPack`, ru против en (версия / ключей / множественных):

| Пакет | Языков | ru | en |
|---|---|---|---|
| android | 33 | v61898857 / 11801 / 714 | v61669506 / 11691 / 712 |
| ios | 33 | v22654911 / 12823 / 735 | v22594703 / 12454 / 720 |
| tdesktop | 34 | v6171755 / 8892 / 645 | v6046851 / 8860 / 638 |
| macos | 25 | v2476602 / 9870 / 372 | v2419915 / 9060 / 350 |
| webk (=web) | 29 | v389686 / 6011 / 317 | v384186 / 6023 / 317 |

- Списки языков на проде совпадают с Telegram: android 33, ios 33, tdesktop 34, macos 25, webk 29. weba/webz и android_x сервер не хранит (`pkg/langpack/model.go:46`) — нашим клиентам они не нужны.
- Прод отстаёт от Telegram не больше чем на сутки (таймер `pluma-langpacks.timer`, последний прогон 2026-11-18 04:34 UTC; `docs/ops/langpacks.md:35-69`). Строки en старше (2026-11-05…13): Telegram менял версию, а для нас ничего не менялось (ios/en `version` 22501477 < `upstream_version` 22594703) — так и задумано (`Store.Import`, `pkg/langpack/store.go:359-480`).
- **Импорт с Telegram и ежедневное обновление уже есть** (`pluma-admin langpack fetch|import|list`, `tools/pluma-admin/langpack.go:59-75`; `pkg/langpack/fetch.go`, `telegram.go`). Отдельный инструмент синхронизации не нужен; не хватает проверки полноты (см. «Дизайн», п. 3).

### Android (пакет `android`)
- **Вердикт:** пакет сервера для ru полон (нет 49 ключей, это пробелы самого снимка Telegram); английский, который видел владелец, — скорее всего строки до загрузки пакета или после неудачной загрузки. На устройстве не проверено.
- Порядок поиска строки: пакет сервера (`BuildVars.USE_CLOUD_STRINGS`, `BuildVars.java:24`) → встроенный язык поверх встроенного английского → `LOC_ERR:key` (`LocaleController.java:4783-4805`, `:1474`).
- Встроенный ru — только вход: 257 ключей против 11 537 английских (`values-ru/strings.xml`; языки — `LocaleController.java:600-688`). Из 222 ключей `LoginActivity.java` в ru нет 118, среди них обоснования разрешений `AllowReadCall*`, `AllowFillNumber`, кнопки `Continue`, `Cancel`, `OK`, `Done`. Ни одного из 34 `Permission*`/`Allow*` в `values-ru` нет. Так же у официального Telegram: это `values-ru` апстрима.
- Пакет ru становится remote только после ответа `langpack.getLanguages` (`LocaleController.java:3376-3445`), затем весь пакет грузится асинхронно (`applyRemoteLanguage`, `:3516`). До этого, при первом входе или при сбое загрузки около 11 280 ключей показываются по-английски. Ошибок `RPCLangpack` в логах `pluma-session-1` за 7 дней нет — но загрузку на конкретном устройстве это не доказывает.
- Пробелы снимка ru (49): `SearchEngine1..7*` (35), `Fpnv*` (9, экран согласия Firebase, `LoginActivity.java:3504-3529`; вызывается ли у нас — не проверено), `MakeChannelPrivate*`, `MakeGroupPrivate*` (`ChatEditTypeActivity.java:293-294`), `IncomingCallsCount_*`, `OutgoingCallsCount_*` (`DataUsage2Activity.java:465,472`), `Reactions_one/_other`, `AreTyping`, `Gift2AuctionBidStatusFuture2`. Показывает ли здесь английский официальный клиент — не проверено.
- Другие языки: 48–58 ключей у большинства; en 90, nb 90, pl 144, vi 183, be 196, hu 297, tr 467, fa 485, sk 1756 — неполнота переводов самого Telegram.
- **Наша дыра:** `PlumaCallEncryptionFailed` (`ci/patch.py:7575-7588`) встроен патчем в 10 папок `values-*`, на сервере его нет. В 23 языках без встроенного перевода (be, ca, cs, fa, fi, fr, he, hr, hu, id, kk, ms, nb, pl, ro, sk, sr, sv, tr, uz, vi, zh-hans, zh-hant) он английский. `PlumaLogin*` (12) есть и в патче, и в `own.json`.
- ADR 0046 («Android встраивает свои строки сам, сервер для него их не добавляет») устарел: в `own.json` уже 12 ключей android.
- Не исправит пакет: системные диалоги разрешений Android идут на языке системы (клиент меняет только свою конфигурацию, `LocaleController.java:1410-1413`; `localeConfig`/`LocaleManager` нет). Литералы апстрима: `WallpapersListActivity.java:507,1845,1855`, `FilterChatlistActivity.java:1045`, `NotificationsSettingsActivity.java:1030`, `ChatEditActivity.java:2371`, `EmojiView.java:7148`, `StoryRecorder.java:4810`, `VoIPService.java:3558`, `ChatActivity.java:47882`; остальное — отладочные меню. Так же у Telegram.

### iOS (пакет `ios`)
- **Вердикт:** главная причина — сервер не предлагает русский, iOS остаётся на английском, пока пользователь сам не выберет язык; вторая — `Pluma.GroupCall.EncryptionFailed` нет ни в одном языке.
- iOS встраивает только английский (`Telegram/Telegram-iOS/en.lproj/Localizable.strings`, 12 190 ключей; для 18 других языков сборка кладёт пустые файлы, `Telegram/BUILD:194-199`). Всё остальное — пакет `ios`, `langPack: ""` (`Localizations.swift:23,57,77`). Поиск строки: пакет → базовый язык → en.lproj → ключ (`build-system/GenerateStrings/GenerateStrings.py:448-480`).
- iOS шлёт `lang_code` = язык приложения, по умолчанию `en` (`Network.swift:551`); язык системы — в `system_lang_code` (`MTApiEnvironment.m:408`). Кнопку «Continue in Russian» заставка показывает только по `help.getConfig.suggested_lang_code` (`AuthorizationSequenceSplashController.swift:39-40`, `Localizations.swift:6-14`).
- Сервер берёт `suggested_lang_code` из `lang_code` (`app/bff/configuration/internal/core/help.getConfig_handler.go:97-109`, `pkg/langpack/store.go:280-297`; ADR 0025:59 «Не проверено»): на `en` отвечает `en`, iOS ничего не предлагает. На проде у всех 18 сессий `lang_pack='ios'` в `auths` `lang_code='en'`, в том числе у `system_lang_code` ru-RU (3), ru-US (2), ru-KZ (1).
- `system_lang_code` сессия знает и обновляет при повторном `initConnection` (`app/interface/session/internal/sess/main_auth_wrapper.go:435,464-476`), но в метаданные bff не передаёт (ADR 0025:59).
- Пробелы снимка ru: 45 ключей (42 нет ни в одном языке; из используемых — `Contacts.QrCode.NoCodeFound`, `PhotoEditor.CurvesTool`, `PhotoEditor.TintTool`, `Gift.Auction.ViewVariants`). Другие языки: 43–45 у большинства; hr 89, nl 139, ar 153, de/en 181, id 184, ko 191, nb 218, pl 278, vi 357, tr 400, be 472, fa 680, hu 682, uz 1043, sk 1095. У de, en, ko, hu, fa (импорт 2026-11-12) нет новых групп `VoiceOver.*`, `News.*`, `Agent.*`, `Channel.*` — похоже на состояние снимка Telegram (не проверено).
- **Наши дыры:** `Pluma.GroupCall.EncryptionFailed` (`ci/patch.py:6091-6093`) нет нигде; алерт по-английски на всех языках. `Pluma.Login.*` (11) в `own.json` есть.
- Ссылки: патч заменил t.me/telegram.org в `AuthSessions.AddDevice.ScanInstallInfo` и 8 ключах `STRINGS_LINKS`; сервер переписывает ссылки только для 4 ключей Android (`clientLinkKeys`, `pkg/langpack/rebrand.go:61-77`). В ru на проде остались `getdesktop.telegram.org`, `web.telegram.org`, `https://t.me/%@`.
- Не исправит пакет: запросы разрешений iOS берутся из `*.lproj/InfoPlist.strings` бандла (19 языков, ru есть), язык выбирает iOS. Не переведены нигде `NSMotionUsageDescription`, `NSSpeechRecognitionUsageDescription` (Info.plist `Telegram/BUILD:1758-1779`); на практике запроса нет (`SFSpeechRecognizer` в коде нет, `CMMotionManager` в `WebAppController.swift:2827` разрешения не просит; на устройстве не проверено). Литералы апстрима: «Start Messaging» и заставка RMIntro (`AuthorizationSequenceSplashController.swift:82`, `RMIntroViewController.m:209-212`), «Send Logs», «Only you see this message…» (`ChatInterfaceStateContextMenus.swift:1576,2137`), «Return Fee» (`PeerInfoScreenPerformButtonAction.swift:702`), `PeerInfoSettingsItems.swift:86`, «Crash Logs» (`SettingsSearchableItems.swift:4396`).
- Push-тексты собирает сервер, только ru и en (`app/messenger/push/internal/notify/texts.go:108-113`): у остальных языков уведомления английские.

### Desktop (пакет `tdesktop`)
- **Вердикт:** в ru почти полно (нет 1 ключа из 8039 — нашего); остальное английское — Info.plist macOS и литералы апстрима.
- Встроен только английский (`Telegram/Resources/langs/lang.strings`, 8026 ключей); пакет один на все ОС — `tdesktop` (`lang/lang_instance.cpp:27,220-221`). Пакет `macos` у нас есть (25 языков), но tdesktop его не просит — он для нативного Telegram for macOS.
- Fallback: базовый язык, иначе встроенный английский (`lang_instance.cpp:298-300,763-777`). У всех 34 языков на проде `base_lang_code` NULL.
- Пробелы: 1 ключ (ru, uk, de, fr, es, it, pt-br, cs, fi, hr, kk, ms, nb, ro, sr, zh-hans, zh-hant); 5 (ko, sl, sq, lv, lt, af, et, el, bg, az); pl 130, ca 198, nl 415, be 417, tr 560, id 2100. Совпадает с `translated_count` снимков Telegram (id 6622/8860, tr 8329/8860).
- **Наша дыра:** `lng_pluma_group_call_e2e_failed` (`ci/patch.py:4511-4517`, тост `:4740`): комментарий патча обещает перевод с сервера, но ключа нет ни в `own.json`, ни в базе (0 из 34).
- **Языки без своего текста:** `own.json` покрывает 37 языков (`pkg/langpack/own.json`), а в tdesktop есть ещё af, sq, az, et, lv: им `lng_pluma_code_*` достаётся по-английски (`ImportResult.OwnMissing`, ADR 0046 п. 4).
- Не исправит пакет: `Telegram/Telegram.plist:73-80` — `NSMicrophoneUsageDescription`, `NSCameraUsageDescription`, `NSLocationUsageDescription`, `NSBluetoothAlwaysUsageDescription` только по-английски, `*.lproj/InfoPlist.strings` нет (так же у апстрима). `lang/lang_hardcoded.h` (15 функций, 73 вызова `Hard::`), 22 тоста `Toast::Show(u"…")`, например `core/local_url_handlers.cpp:244,1419,1435`, `calls/group/calls_group_call.cpp:1655,1706`.

### Web K (пакет `webk`)
- **Вердикт:** в ru не хватает 15 ключей из 5971 (0,3 %), один из них наш; главный источник английского — первый запуск до «Продолжить на русском» (поведение апстрима).
- Встроен английский (`src/lang.ts`, `src/langSign.ts`), сервер перекрывает его (`src/lib/langPack.ts:270-299`). `getLangPack` идёт с `web` (`langPack.ts:232-240`), `getDifference`/`getStrings` — с `webk` (`appLangPackManager.ts:47-61`); сервер сводит `web` к `webk` (`pkg/langpack/model.go:46-58`). Без сохранённого пакета старт на английском (`langPack.ts:208-228`); кнопка смены языка берёт `suggested_lang_code` (`languageChangeButton.tsx:10-23`).
- ru без перевода (15): `AccDescr.Zoom`, `AttachDocument`, `Chat.Input.Editor.Math.Sample`, `Chat.Input.Editor.Toolbar.TableSelectionEnd`, `Community.ChatSettings`, `Experimental.*Sandbox` (3), `MediaEditor.Tabs`, `MediaEditor.VideoCrop*` (2), `MediaEditor.VideoPosition`, `MultiAccount.Switched`, `PaidMessages.ChargeFeeWarning`, `PlumaCall.EncryptionFailed`. Почти все — новые ключи апстрима, которых нет и в снимке Telegram.
- Другие языки: по 3 у en, es, fi, fr, it, kk, ms, sr, uk, zh-hant; pt-br 175 … tr 3480, de 3609, id 3620 (de `translated_count` 2384 из 6023 — снимок Telegram).
- **Наша дыра:** `PlumaCall.EncryptionFailed` (`ci/patch.py:2893-2896`, тост `:2866`) нет в `own.json` — по-английски на всех 29 языках. `Pluma.Code.*` и `Pluma.Update.Required.*` (12) есть.
- Ссылки: ru `PublicLinksLimitSubtitle`, `GroupDescription3` показывают `t.me/…` (в патче `pluma.chat/…`), `ChatList.Main.EmptyPlaceholder.SubtitleNoContacts` ссылается на telegram.org.
- Литералов, видимых пользователю, нет (поиск эвристический): `src/index.ts:401` «Test DC» только на тестовом DC; push-заглушки патча (`src/lib/serviceWorker/push.ts:135,199,449,467`) заменяются ключом `PushNotification.Message.Refreshing` (`webPushApiManager.ts:155`).

## Причины (сводка)
1. **iOS не предлагает русский:** `suggested_lang_code` = `lang_code` клиента, а не язык системы.
2. **Android до загрузки пакета** показывает английский всё, кроме экранов входа (так у апстрима); дошёл ли пакет до устройств владельца — не проверено.
3. **Свои ключи патчей не попадают на сервер:** ключи групповых звонков E2E (задача 094) отсутствуют во всех четырёх пакетах; проверки «ключ патча есть в `own.json`» нет.
4. **`own.json` покрывает 37 языков, а пакеты сервера — 41** (af, sq, az, et, lv только в tdesktop).
5. **Ссылки переписываются только у 4 ключей Android**, хотя патчи iOS и Web K тоже меняют ссылки.
6. **Пробелы снимков Telegram** (sk, uz, fa, hu, tr, id, de у webk и т. д.) — это текущее состояние переводов Telegram, не потеря при импорте.
7. **Строки вне пакета:** системные запросы разрешений (Android — язык системы; iOS — `NSMotion*`/`NSSpeech*`; macOS — только английский), литералы апстрима, push-тексты сервера только ru/en.
8. **Обновление у клиентов** идёт только по `help.getConfig`; `updateLangPack` онлайн-сессиям не рассылается (ADR 0025, «Последствия»; поиск `UpdateLangPack` по `app/`, `pkg/` — 0 файлов).

## Цель
Пользователь на любом из 37 + 5 языков пакетов видит на всех четырёх клиентах тот же перевод, что в официальном Telegram, плюс переведённые тексты Pluma; русский предлагается сам, если система на русском; после обновления пакета клиенты получают его без перезапуска.

## Область
- Сервер: `pkg/langpack` (`own.json`, `override.json`, `rebrand.go`, `store.go`), `app/bff/configuration` (`help.getConfig`), передача `system_lang_code` в метаданные bff, рассылка `updateLangPack`, `pluma-admin langpack coverage`, push-тексты.
- Клиенты — только по решениям владельца (пп. 6–8 «Решений к подтверждению»).
- Не входит: дописывать переводы за Telegram (пробелы снимков), пакеты weba/android_x, литералы апстрима, отладочные меню.

## Дизайн
### 1. Свои ключи во всех пакетах и языках
- `own.json`: добавить `PlumaCallEncryptionFailed` (android), `Pluma.GroupCall.EncryptionFailed` (ios), `lng_pluma_group_call_e2e_failed` (tdesktop), `PlumaCall.EncryptionFailed` (webk). Английский — дословно из патчей (`ci/patch.py` веток 109, строки выше); переводы — для всех языков, как у `Pluma.Login.*`. Для Android переводы 10 встроенных языков взять из `values-*` патча, чтобы сервер не перекрывал их другим текстом.
- Довести каждый ключ `own.json` и `override.json` до 42 языков (+ af, sq, az, et, lv; en — 1 из 42). Тест `own_test.go`: каждый ключ имеет все языки списка `pkg/langpack/languages.json` (языки всех пяти пакетов Telegram на 2026-11-18), `OwnMissing` для них пуст.
- Переводы пишутся как в задачах 076/080: текст по смыслу, термины из того же пакета Telegram на этом языке (например, «сквозное шифрование» — как в ключах звонков пакета). Пустых и машинно-дословных значений нет.
- ADR 0046: дополнение — Android тоже получает свои строки с сервера (пункт «Последствия» про Android заменяется).

### 2. Ключи патчей проверяются автоматически
- Каждый клиентский репозиторий уже знает свои новые ключи (Android `LOGIN_STRINGS`, iOS `Pluma.*`, tdesktop `lng_pluma_*`, Web K `Pluma*`). Добавить в сервер `pkg/langpack/patchkeys.json` — `{пакет: [ключи патча]}` — и тест: каждый ключ есть в `own.json` или `override.json` своего пакета.
- Список обновляется при каждом изменении патча клиента, где добавлен ключ: правило в `clients/patches/*.md` и в чек-листе переноса патча на новую версию апстрима. Проверка расхождения — `pluma-admin langpack coverage` (п. 3) на собранном клиенте.

### 3. `pluma-admin langpack coverage`
- Новая подкоманда рядом с `fetch|import|list` (`tools/pluma-admin/langpack.go`). Вход: файл ключей клиента (`-keys`, по строке на ключ, множественные формы — базовым ключом), пакет и языки. Выход: по языку — сколько ключей клиента нет на сервере, сколько из них нет и в снимке Telegram (то есть пробел Telegram, а не наш), список наших ключей без перевода. Только чтение базы, без вывода значений строк.
- Файлы ключей клиента собираются скриптом в репозитории каждого клиента (`ci/langkeys.*`): Android — `values/strings.xml` после патча, iOS — `en.lproj/Localizable.strings`, tdesktop — `lang.strings`, Web K — `lang.ts` + `langSign.ts`. Запуск — при обновлении апстрима клиента и в итоговой проверке этой задачи.
- Отдельного инструмента синхронизации (`tools/langpacksync`) не делать: `fetch` + `import` по таймеру уже держат пакеты свежими (≤ 1 сутки отставания на проде 2026-11-18). Миграция схемы не нужна: свои строки и ссылки проходят обычным `import`, он идемпотентен и поднимает версию только изменённых строк (`store.go:359-480`).

### 4. Ссылки во всех пакетах
- `clientLinkKeys` (`rebrand.go:61-77`) стать таблицей по пакету: android — как сейчас; ios — `AuthSessions.AddDevice.ScanInstallInfo` и ключи `STRINGS_LINKS` патча iOS; webk — `PublicLinksLimitSubtitle`, `GroupDescription3`. Замены — те же, что в патче соответствующего клиента (t.me → pluma.chat, getdesktop/web.telegram.org → pluma.chat/web.pluma.chat). `ChatList.Main.EmptyPlaceholder.SubtitleNoContacts` патч Web K переписал смыслово — в `override.json` на всех языках.
- Тест: для каждого ключа таблицы ни в одном языке тестового снимка после `Rebrand` не остаётся `telegram.org` и `t.me/`.

### 5. Подсказка языка по языку системы (iOS, Web K)
- Сессия передаёт `system_lang_code` в метаданные bff рядом с `lang_code` (ADR 0025:59). Нужные места: метаданные RPC сессии → `c.MD` bff; поле в proto метаданных — если его нет, отдельный `third_party(proto):` коммит с `// pluma:` (ADR 0009).
- `ConfigVersions` (`store.go:280-297`): `suggested_lang_code` — язык из `system_lang_code`, если он есть в пакете (ru-RU → ru, pt-BR → pt-br, zh-Hans-* → zh-hans; без совпадения — по первому подтегу), иначе `lang_code` как сейчас. `lang_pack_version` и `base_lang_pack_version` остаются у языка `lang_code`: они относятся к пакету, который клиент держит сейчас.
- Опора: iOS показывает кнопку, когда предложенный язык отличается от текущего (`Localizations.swift:6-14`, `AuthorizationSequenceSplashController.swift:39-40`); Web K — `languageChangeButton.tsx:10-23`. Как это вычисляет Telegram, не проверено; перед кодом — проба на настоящем Telegram (ADR 0025 «Не проверено»; способ — Web K invokeApi, iOS-клиент с системным ru): какой `suggested_lang_code` приходит при `lang_code=en` и системном ru. Если Telegram отвечает иначе — делаем как Telegram и записываем в ADR.
- Android не затронут: он выбирает язык сам по системному при первом запуске (`LocaleController`, проверить при реализации и сослаться).

### 6. Клиенты получают изменения сразу
- Сейчас клиент узнаёт о новой версии только при `help.getConfig`. Добавить рассылку `updateLangPackTooLong{lang_code}` онлайн-сессиям пакета и языка после импорта, где версия языка выросла. `TooLong`, а не `updateLangPack` с разницей: разница может быть большой, клиент сам вызовет `langpack.getDifference` от своей версии; так делают клиенты на `updateLangPackTooLong` — сослаться на код обработки в каждом из четырёх (Android `MessagesController`, iOS `ManagedLocalizationUpdatesOperations.swift`, tdesktop `lang_cloud_manager.cpp`, Web K `appLangPackManager.ts`), прочитав их до реализации.
- Кому слать: сессии с `auths.lang_pack`/`lang_code` = пакет/язык (с учётом `web`→`webk`). Импорт пишет список изменённых языков; рассылка — после коммита транзакции, ошибка рассылки не откатывает импорт и пишется в лог (без текстов строк).
- `getDifference` остаётся как есть (`langpack.getDifference_handler.go`: полный пакет при `from ≤ 0`, разница с надгробиями, без надгробий для webk). Тест: после импорта с изменённой своей строкой `getDifference(from=старая)` возвращает её, а версия в `help.getConfig` выросла.

### 7. Push-тексты на всех языках
- `texts.go:108-113` знает только ru и en. Брать шаблоны уведомлений из пакета `ios` нужного языка, если в нём есть соответствующие ключи (в пакете ios есть push-ключи — см. колонку «не push» обследования); каких ключей и с каким форматом аргументов — прочитать в `NotificationService` iOS до реализации. Нет ключа — английский, как сейчас. Если сопоставление ключей окажется неоднозначным — пункт выносится в отдельную задачу, а не делается наполовину.

## Порядок работ
1. Проба Telegram: `suggested_lang_code` при `lang_code=en` и системном ru (п. 5). Запись в бриф.
2. Проверка Android на устройстве владельца или эмуляторе (п. «Проверки владельца», Android 1): дошёл ли пакет. Если нет — найти причину на сервере до остального.
3. `own.json`, `override.json`, `languages.json`, `patchkeys.json`, тесты (пп. 1–2).
4. Ссылки (п. 4).
5. `system_lang_code` в bff и `suggested_lang_code` (п. 5).
6. `updateLangPackTooLong` (п. 6).
7. `pluma-admin langpack coverage` и скрипты ключей в клиентах (п. 3).
8. Push-тексты (п. 7).
9. Выкладка: сервер → `pluma-admin langpack import` последнего снимка (без `fetch`, ADR 0046) → `coverage` по четырём клиентам → проверки владельца.

## Тесты
- Юнит: `own_test.go` (все языки, ключи патчей), `rebrand_test.go` (таблица ссылок по пакетам), `ConfigVersions` (ru-RU/pt-BR/zh-Hans/неизвестный язык/нет `system_lang_code`), сбор языков для рассылки.
- Интеграция на MySQL (`pkg/testinfra`): импорт с новой своей строкой поднимает версию языка, второй импорт ничего не меняет; `getDifference` возвращает строку.
- e2e (`pkg/mtclient`): `initConnection` с `lang_code=en`, `system_lang_code=ru-RU` → `help.getConfig.suggested_lang_code = ru`; после импорта онлайн-сессия получает `updateLangPackTooLong`.
- Хотя бы один тест должен падать без изменения (проверить на тесте `own.json`).

## Проверки перед сдачей (CachyOS)
`go build ./...`; `go vet -tags integration,e2e -unreachable=false` и `go test` (с `-race` в итоговом прогоне) для `pkg/langpack`, `app/bff/langpack`, `app/bff/configuration`, `app/interface/session`, `app/messenger/push`, `tools/pluma-admin`; `make lint`; `go test ./tools/...` (log audit); интеграционные и e2e этой области; `go run ./tools/apicoverage` (обработчики меняются).

## Проверка покрытия (цифры)
После импорта на проде `pluma-admin langpack coverage` для четырёх клиентов:
- наших ключей без перевода — 0 во всех языках всех пакетов;
- ключей, которых нет на сервере, но есть в снимке Telegram — 0;
- остаток = пробелы снимков Telegram; ожидаемо для ru: android ~49, ios ~45, tdesktop 0, webk ~14 (числа обследования 2026-11-18 минус наши ключи).

## Проверки владельца (по клиентам, со скриншотами)
- **Android** (ru-система, ru в приложении): 1) свежая установка → экран входа → после входа Настройки → Язык: выбран ли «Русский» (remote) и через сколько секунд интерфейс полностью русский; в logcat нет ошибок `langpack`. 2) Вход: обоснования разрешений (звонки, контакты) и кнопки — какие английские до входа, какие после. 3) Групповой звонок с ошибкой E2E (или по ADR 0060 способ вызвать алерт) — текст на русском, затем на польском.
- **iOS** (iPhone с системным русским): 1) свежая установка → на заставке есть «Продолжить на русском»; после нажатия вход (`Pluma.Login.*`) по-русски. 2) Настройки → Устройства → «Подключить устройство»: ссылки pluma.chat. 3) Алерт ошибки шифрования группового звонка по-русски. 4) Push от другого пользователя на языке, кроме ru/en (например, de), — по-немецки.
- **Desktop** (Windows и macOS, язык ru): 1) тост ошибки E2E группового звонка по-русски; 2) экран кода входа на af/et/lv — переведён; 3) macOS: системный запрос микрофона — каким будет текст (по решению п. 7).
- **Web K** (браузер с ru): 1) первый запуск — есть «Продолжить на русском»; 2) после выбора: Настройки, публичная ссылка группы — `pluma.chat/…`; 3) тост ошибки шифрования звонка по-русски.
- Все пункты — в `docs/project/TESTING-BACKLOG.md` в ветке задачи.

## Эксплуатация
- `docs/ops/langpacks.md`: после выкладки — `pluma-admin langpack import` последнего снимка; `coverage` при обновлении клиента; `OwnMissing` в выводе импорта должен быть пуст.
- Новых переменных и миграций схемы нет.

## Решения владельца (2026-11-18)
- Телефон, на котором владелец видел английский, — с русским языком системы. Значит, системные окна разрешений там русские, а английским было собственное: обоснования разрешений и кнопки приложения до загрузки пакета или ключи, которых нет на сервере.
- Встроить русский в само Android-приложение (`values-ru` патчем) для всего, что видно до загрузки пакета: экран входа целиком, `Allow*`/`Permission*`, общие кнопки (`OK`, `Cancel`, `Continue`, `Done` и т. п.). Тексты — из официального пакета Telegram `android`/`ru` той же версии, с теми же заменами Telegram→Pluma, что делает сервер.

## Решения (принятые в брифе)
- Пробелы переводов Telegram не дописываем: поведение один к одному с Telegram (CLAUDE.md, правило 5); показываем их числом в `coverage`.
- Синхронизация остаётся `fetch` + `import` по таймеру; новый инструмент только для проверки полноты.
- Рассылаем `updateLangPackTooLong`, не `updateLangPack` (см. п. 6; подтвердить чтением клиентов).

## Решения к подтверждению
1. `suggested_lang_code` по языку системы — если проба покажет, что Telegram так не делает, всё равно делать? (Влияет на iOS и Web K: без этого русский пользователь видит английский до ручного выбора.)
2. Языки af, sq, az, et, lv для своих строк: переводить (42 языка) или оставить английский.
3. `ViaBot`=«via», `PrivacyCallsP2PTitle` «Peer-to-Peer» и др. — оставить, как у Telegram (предложение — оставить).
4. Push-тексты на всех языках (п. 7) — в этой задаче или отдельной.
5. `Fpnv*` (экран Firebase на Android): проверить, вызывается ли у нас; если нет — ничего не делать.
6. **iOS Info.plist:** добавить переводы `NSMotionUsageDescription`, `NSSpeechRecognitionUsageDescription` (19 языков) — правка клиента, на практике запрос не показывается. Предложение — не делать.
7. **macOS Info.plist (Desktop):** добавить `<lang>.lproj/InfoPlist.strings` с переводом 4 запросов разрешений — правка клиента; у апстрима их нет. Решение владельца.
8. **Android встроенный ru:** встроить в `values-ru` (и другие встроенные папки) ключи, нужные до загрузки пакета (экран входа, `Allow*`/`Permission*`, общие кнопки) — правка клиента, у апстрима так же английский. Только если проверка Android 1 покажет, что английский виден заметно долго.
9. Лицензия переводов Telegram: явного разрешения на translations.telegram.org не найдено (проверена главная страница; страница «official translations» не проверена); переводы в репозиториях клиентов — под GPLv2/GPLv3 (Android, tdesktop, tweb). Сервер раздаёт их с 2026-11-01 (задача 021); оставить как есть — решение владельца.

## Не проверено
- Как Telegram вычисляет `suggested_lang_code`.
- Применён ли remote-пакет ru на Android-устройствах владельца; виден ли у официального Telegram английский на месте пробелов снимка.
- Отличается ли у Telegram `web` от `webk` (ADR 0025).
- Актуальность снимков de, en, ko, hu, fa пакета ios (нет групп `VoiceOver.*`, `News.*`, `Agent.*`, `Channel.*`).
- Запрашивает ли iOS разрешения движения и распознавания речи на практике.
- Что системный запрос разрешения официального Telegram Desktop на macOS тоже английский.
- Поиск литералов во всех клиентах — эвристический grep, список может быть неполным.

## Результат
Серверная часть сделана (2026-11-19, ветка `task/114-server` от `origin/master` `cba008f3e`, 13 коммитов вместе с двумя коммитами брифа, не запушено, не выкачено). Свои строки Pluma — во всех четырёх пакетах и на всех 42 языках пакетов Telegram, включая ключи ошибки шифрования групповых звонков (задача 094); список ключей патчей проверяется тестом; ссылки переписываются по таблице пакетов, как в патчах клиентов; `help.getConfig` предлагает язык системы (так же ответил настоящий Telegram); после импорта онлайн-клиенты получают `updateLangPackTooLong`; `pluma-admin langpack coverage` сверяет ключи собранного клиента с сервером. Push-тексты на всех языках не сделаны: сопоставление с ключами `PUSH_*` неоднозначно (ниже), вынесено в отдельную задачу.

**Проба Telegram (порядок работ, п. 1).** 2026-11-19, встроенный браузер, web.telegram.org/k (запасной аккаунт владельца), только чтение: `rootScope.managers.apiManager.invokeApi('help.getConfig', {})` → `suggested_lang_code = "ru"`, `lang_pack_version = 384186` (версия webk/en), `base_lang_pack_version = 0`; `navigator.language = "ru"` (`navigator.languages` ru, ru-KZ, cs-KZ), интерфейс Web K в английском (`I18n.getLastRequestedLangCode() = "en"`, `<html lang="en">`). Web K шлёт `navigator.language` в `system_lang_code` (`networkerFactory.ts:57`), свой язык — в `lang_code`. Вывод: Telegram предлагает не язык клиента; совпадает с языком системы (страну номера и IP одно наблюдение не исключает). Версии в конфиге — языка клиента. Сделано так же.

**Что изменено**
- `pkg/langpack/own.json`: + `PlumaCallEncryptionFailed` (android; 10 встроенных языков — дословно из `E2E_FAILED_TEXT` патча Android), `Pluma.GroupCall.EncryptionFailed` (ios), `lng_pluma_group_call_e2e_failed` (tdesktop), `PlumaCall.EncryptionFailed` (webk); английский — дословно из патчей (у Android, iOS и Desktop/Web K он разный, переведён каждый). Все ключи `own.json` и `override.json` — на 42 языках: добавлены af, az, et, lv, sq (термины — из пакета tdesktop этих языков: «Instellings → Privaatheid en sekuriteit → …», «Otspunktkrüpteeritud» и т. п.). Также `games.json`, `inlinebots.json` — на 42 языках.
- `pkg/langpack/languages.json` (языки пяти пакетов на 2026-11-18), `patchkeys.json` (ключи патчей: android 13, ios 12, tdesktop 13, webk 13 — по `ci/patch.py` веток `main` клиентов на 2026-11-18). Тесты: все языки у каждого ключа, пустой `OwnMissing` во всех языках всех пакетов, `own.json` пакета = `patchkeys.json`, аргументы формата и разметка как в английском.
- `pkg/langpack/rebrand.go`: `linkRules` — пакет → ключ → правило (Android: вход по QR, `GroupDescription3`; iOS: `AuthSessions.AddDevice.ScanInstallInfo`, 8 ключей `STRINGS_LINKS`; Web K: `PublicLinksLimitSubtitle`, `GroupDescription3`, ссылки на telegram.org в `ChatList.Main.EmptyPlaceholder.SubtitleNoContacts` снимаются со словами, пустая ссылка «Подробнее» в `ChatAutomation.HeroDescription` убирается — как `TELEGRAM_SITE_EDITS` патча Web K). Адреса клиентов — без учёта регистра (казахское «Desktop.telegram.org»). `Rebrand`/`RebrandValue` получили пакет. Тест по `testdata/links.json` (эти ключи на всех языках снимков 2026-11-18): ни telegram.org, ни t.me не остаётся.
- `RpcMetadata.system_lang_code` (поле 25, коммит `third_party(proto):`, `// pluma:`), сессия передаёт его; `Store.ConfigVersions(pack, lang, systemLang)` и `SystemLanguages`; `help.getConfig` передаёт язык системы.
- `pkg/langpack/notify.go`: `Store.Readers` (авторизации `auths` + `auth_users` с языком пакета или языком, у которого он базовый), `NotifyChanged` (`updateLangPackTooLong` через `sync.updatesMe`), `ImportResult.Raised`. `pluma-admin langpack import` после всех импортов рассылает по поднявшимся языкам через outbox sync (секция `SyncClient` конфига); ошибка печатается и завершает команду с ошибкой, импорт остаётся.
- `pluma-admin langpack coverage -pack -keys [-lang] [-snapshots] [-list]`, `Store.Keys`, `langpack.OwnMissing`.
- Документы: ADR 0025 и 0046 — дополнения 2026-11-19; `docs/ops/langpacks.md`; правило «новый ключ патча → `patchkeys.json` и `own.json`» в `clients/patches/README.md`; пункты в `docs/project/TESTING-BACKLOG.md`.

**Как обрабатывают обновления клиенты (прочитано до реализации).** Android `MessagesController.java:19623-19625` → `LocaleController.reloadCurrentRemoteLocale` (`:841-848`); iOS `UpdateInterpreter+Settings.swift:71-72` → `StateReplay+Settings.swift:81-89` → `StateReplay+Finalize.swift:404-406` → `ManagedLocalizationUpdatesOperations.swift:158-170` (`getDifference(langPack: "", …)` основного и второго языка); tdesktop `api_updates.cpp:2918-2924` → `lang_cloud_manager.cpp:193-201`; Web K `appLangPackManager.ts:11-13` → `langPack.ts:940-971`. Все четыре на `TooLong` просят разницу от своей версии — рассылается `TooLong`. Android тоже читает `suggested_lang_code`: предложение сменить язык (`LaunchActivity.java:8279-8320`).

**Push-тексты (п. 7) — не сделаны, отдельная задача.** iOS показывает `aps.alert` как есть (`NotificationService.swift:1217-1232`), тексты собирает сервер (`texts.go`, ru и en). В пакете ios есть 216 ключей `PUSH_*` в форме «заголовок|текст», но однозначно на нынешние тексты они не ложатся: (1) «Пересланные сообщения: N» — у Telegram множественная `PUSH_MESSAGE_FWDS`, а правил множественного числа по языкам (CLDR) на сервере нет; (2) подпись к фото («📷 Фото, подпись») ключа `PUSH_*` не имеет; (3) закреплённое сообщение — у Telegram отдельный ключ на каждый вид содержимого (`PUSH_PINNED_*`), у нас один шаблон; (4) переход на `PUSH_*` меняет и нынешние ru/en тексты («{1} закрепил(а) «{2}»» → «📌 Закрепил(а) «…»» в теле). Это переделка слоя push-текстов с правилами множественного числа, а не подстановка ключей; по брифу выносится.

**Проверки (CachyOS).** `go build ./...` — ок; `go vet -tags integration,e2e -unreachable=false` пакетов `pkg/langpack`, `app/bff/langpack`, `app/bff/configuration`, `app/interface/session`, `app/messenger/push`, `app/messenger/sync`, `tools/pluma-admin`, `test/e2e` — ок; `go test` и `go test -race` тех же — ок; `go test ./tools/...` (журнальный аудит) — ок; интеграционные `pkg/langpack` на MySQL (в т. ч. новые `TestReaders`, `TestKeys`, `TestImportChangedOwnString`, расширенный `TestConfigVersions`) — ок; e2e на стенде `pluma-114` (`TestLangPacks`, новый `TestLangPackSystemLanguageAndUpdates`: `lang_code=en` + `system_lang_code=ru-RU` → `suggested_lang_code=ru`; после импорта онлайн-сессия получает `updateLangPackTooLong`, `getDifference` отдаёт изменение) — ок, стенд снят. `make lint`: на копии без `.git` `new-from-rev` не работает (438 старых замечаний по всему репозиторию); `golangci-lint` v2.15.0 `--new-from-patch` по изменениям ветки — 0 замечаний. `go run ./tools/apicoverage` — матрица не изменилась. Без изменения падает: тест своих строк на старом `own.json` — 239 ошибок.

**Решения к подтверждению**
1. `suggested_lang_code` по языку системы — сделано, т. к. Telegram отвечает так же (проба выше); влияет и на Android (предложение сменить язык, как у Telegram).
2. af, az, et, lv, sq — переведены (свои строки, кнопка игры, описания @gif/@pic); письма и подписи статистики остаются на 37 языках — для этих пяти там английский.
3. Ссылки Web K `SubtitleNoContacts` сделаны правилом (слова остаются, ссылки на telegram.org снимаются), а не `override.json`, как в брифе: так сохраняется перевод Telegram на всех языках. Дополнительно, по патчу Web K, убирается пустая ссылка «Подробнее» в `ChatAutomation.HeroDescription` (в брифе не было). Android `GroupDescription3` тоже переписывается (патч Android меняет его, `LINK_STRINGS`).
4. Рассылка — всем авторизациям языка (sync отбрасывает офлайн-сессии), не только активным недавно; новый язык не рассылается.

**Не сделано / не проверено**
- Push-тексты на всех языках — отдельная задача (причины выше).
- Скрипты `ci/langkeys.*` в репозиториях клиентов и прогон `coverage` по четырём собранным клиентам на проде — нужны выкладка сервера и сборки клиентов; не делалось. Цифры «пробелы Telegram ≈ 49/45/0/14» не подтверждены.
- `patchkeys.json` составлен вручную по `ci/patch.py` веток `main` клиентов (Android `58c1015`, iOS `676ce1c`, Desktop `deb13f9`, Web `9899991`); ветка `task/114-android-strings` (встроенный ru Android) ключей не добавляет — проверить при её слиянии.
- В живых клиентах ничего не проверено (пункты в TESTING-BACKLOG). Переводы на 41 язык — мои, носителями не вычитаны.
- Lint вендорного модуля `third_party/teamgram-proto` не запускался (изменён только сгенерированный `rpc_metadata.pb.go`, `protoc-gen-go` v1.36.11, `protoc` 36.1 — как в прошлом коммите `third_party(proto):`).

**Для выкладки:** сервер → `pluma-admin langpack import` последнего снимка (без `fetch`) — в выводе нет «in English, no translation», есть «… told of the new versions …»; миграций и новых переменных нет.
