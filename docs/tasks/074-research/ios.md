# ios: обновление до нового upstream

Текущая: 12.9.2 (release-12.9.2, 6ad963e5b62d354da79040f388ae2b9132fb17b8, layer 228). Новейшая: 13.2. Слой: 233.

## Итог
Новейший стабильный iOS-клиент: **13.2**, layer **233**. Патч почти не сломан: 384 из 415 проверок проходят. Обновить клиент отдельно нельзя, сначала нужен layer 233 на сервере. Сборка не запускалась, поэтому работоспособность клиента на 13.2 не проверена.

## Релиз
- Тег `release-13.2`, коммит `24638bc69bd083981b178dd1e7f921eebbf7fc46`, дата коммита 2026-11-02 (`git ls-remote`, GitHub API). На момент проверки это же голова `master`.
- `versions.json` этого тега: `app` 13.2.
- App Store (`itunes.apple.com/lookup?id=686449807`): версия 13.2, выпущена 2026-11-05, минимальная iOS 15.0. Примечания к выпуску только «Bug fixes, minor improvements».
- GitHub Releases устарели: последний `build-26855` от 2023 года.

| Тег | Коммит от | layer (`Serialization.swift`, `currentLayer`) | Xcode | Bazel | macOS |
|---|---|---|---|---|---|
| 12.9.2 (наш) | 07-17 | 228 | 26.2 | 8.4.2 | 26 |
| 13.0.0 | 10-07 | 230 | 26.6 (deploy 26.2) | 9.2.0 | 26 |
| 13.1, 13.1.1, 13.1.2 | 10-19…10-28 | 232 | 27.1 | 9.2.0 | 27 |
| 13.2 | 11-02 | 233 | 27.1 | 9.2.0 | 27 |

Клон для чтения: `/Users/sckr.a/pluma-work/reference-src-new/ios` (731 МБ, `--filter=blob:none`). Сабмодуль движка: `/Users/sckr.a/pluma-work/reference-src-new/ios-mtproto-engine` (6,6 МБ). Весь каталог `reference-src-new` занимает 2,0 ГБ вместе с клонами других клиентов.

## Схема
`submodules/TelegramApi/swift_scheme.tl` (файл появился после нашего пина). У 12.9.2 в дереве только сгенерированный код, поэтому база сравнения — `api.tl` закреплённого tdesktop (`// LAYER 228`, строка 3087). Отличия iOS-варианта 228 от него не проверялись.

| 228 → 233 | Добавлено | Удалено | Изменён CRC |
|---|---|---|---|
| Конструкторы (1643 → 1739) | 114 | 18 | 17 |
| Методы (806 → 867) | 61 | 0 | 7 |

- Изменили CRC `channel`, `channelFull`, `userFull`, `messageReplyHeader`, `inputReplyToMessage`, `replyInlineMarkup`, `keyboardButton`.
- Кнопки переделаны: все `keyboardButtonUrl`, `keyboardButtonCallback` и подобные удалены. Вместо них `keyboardButton` с полем `type:ButtonType` и новый `keyboardInlineButton` с `InlineButtonType`.

## Что изменилось для пользователей
Источники: 1716 коммитов без слияний, +707 ключей в `Localizable.strings`, схема. Блог telegram.org заканчивается постом от 2026-08-25, ветку 13.x он не описывает. Коммиты и ключи показывают наличие кода, но не то, что функция включена у пользователей.
- **TON-кошелёк в приложении:** 22 каталога `Components/Wallet*`, 351 строка, 149 коммитов `feat(wallet)`, методы `wallet.*`, `toncenter.*`, TonConnect, покупка через `payments.*Onramp*`.
- **Виджеты в сообщениях:** `inputMediaWidget`, `messageMediaWidget`, `messages.editMessageWidgetData`.
- **Rich text и InstantPage V2:** `RichText.*`, часть форматирования требует Premium.
- **Приветственные сообщения в группах:** `WelcomeMessages.*`, `ephemeral.getWelcomeMessages`.
- **Новые разделы:** `News.*`, QR Studio и темы-подарки (`shareThemeGift`), `Agent.*` («Add Your AI API key»), транскрипция файлов (`messages.transcribeAudioFile`).
- **Подарки и платежи:** вывод и депозит подарков (`payments.depositStarGift`, `withdrawStarGift`), токены без платных сообщений.
- **Вход:** Firebase PNV.
- **Интерфейс:** новая навигационная панель, боковой список чатов, раскрытие профиля, адаптация под «iPhone Duo» (название из коммитов), новая сетка медиа, новая камера `CameraNeo`, больше строк VoiceOver (+43).
- **Минимальная iOS:** с 13.0 поднята до 15.0 (`Telegram/BUILD:169`). Запись I-084 реестра этим закрывается, устройства на iOS 13–14 отпадают.

## Стоимость патча
Измерено своим скриптом: загружает `ci/patch.py` форка, выполняет все правки по порядку и записывает результат каждой, не останавливаясь на ошибке. Работал на APFS-копиях; `git status` форка пуст. Конфигурация условная: случайный модуль RSA, `app.env` и ключи не читались. Контроль на закреплённом 12.9.2: **415/415**.

**На 13.2: 384 из 415 проходят, сломано 31.** Прерванных проходов нет.
- С ожидаемым числом вхождений 1: 339 из 367.
- Текстовые якоря: 352 из 362.
- Файлы по SHA: 32 из 51.
- Счётчики: 0 из 2 (`STRINGS_RENAMED`, `required_keys`).

Все картинки бренда совпали: сломаны только файлы Siri.

| ID реестра | Сломано | Что случилось | Работа |
|---|---|---|---|
| I-086 Siri | 19 | Все `AppIntentVocabulary.plist` изменились, в английском +4 намерения | Новые файлы и хэши |
| I-036 имя в коде | 2 | `CXProviderConfiguration()` теперь без имени (`CallKitIntegration.swift:168`); «Telegram Notifications» переехало в `State/Sync/UpdateInterpreter+Messages.swift:143` | Новый путь, первую правку заменить |
| I-063 layer | 1 | `return 233` | `upstream.env` |
| I-064 Make/BUILD | 1 | `disableProvisioningProfilesSetting` 9 вместо 10, какая цель убрана, не установлено | Счётчик |
| I-065 конфигурация | 1 | `app_center_id` убран из `required_keys` | Не писать ключ |
| I-007 DNS | 1 | Апстрим сам убрал DoH Google (`MTDNS.m:237-251`) | Правку удалить |
| I-010 отражатель | 1 | Ветки 12.0.0 больше нет (`OngoingCallContext.swift:987-988`) | Правку удалить |
| I-028 вступление | 1 | После `_loadedView` новое поле | Новый якорь |
| I-033 строки | 1 | Счётчик (585, 638) вместо (556, 609) | Счётчик |
| I-105 ссылки в текстах | 1 | Ключа `CreateBot.UsernameStatus.Link` больше нет | Убрать из `STRINGS_LINKS` |
| I-090 битрейт | 1 | Кружок в `MediaEditorValues.swift:201-202`: 1200 вместо 1000 | Пересмотреть |
| I-093 запись кружка | 1 | `CameraOutput.swift` переехал в `Camera/CameraLegacy/Sources:356`; новый рекордер `CameraNeo/Sources/VideoRecorder.swift:625` с 1 200 000 патчем не покрыт | Новый путь и новая правка |

**Проверки `verify`, которые упадут сверх якорей:**
- I-012: скан сети теперь захватывает тесты с адресами Telegram (`MtProtoKit/Tests/MTBackupAddressApplyTests.m`, `MTDiscoverConnectionSignalsTests.m`). Каталог `Tests` надо исключить. RSA-ключей вне тестов по-прежнему 3.
- I-037/I-038: появились схемы `ton` и `tc` (`Telegram/BUILD:445-446`), нужно решение.
- I-104: новая ссылка `t.me/sendgrams` (`InstantPageTextItem.swift:1182`). `verify_links` останавливается на первой находке, так что могут быть ещё.

**Чего патч не увидит:**
- Кошелёк и другие новые платные разделы (I-053…I-060): скрываются ли они при `premium_purchase_blocked` и что покажет клиент без `wallet.*` на сервере, не проверялось.
- Rust-движок MTProto встроен в каждую iOS-сборку (заметки разработчиков в `submodules/MTProtoRustEngine`). На iOS по умолчанию остаётся MtProtoKit (`SyncCore_NetworkEngineSettings.swift:6-11`), переключатель только в Debug. Ключи движок получает извне (`crates/mtproto-ffi/src/lib.rs:897`), ключ Telegram в `rsa.rs:209` под `cfg(test)`. Но `third-party/mtproto-engine` не входит в `NETWORK_DIRS`, скан его не видит.
- `rlottie` заменён на `tlottie` (`.gitmodules`). Вызов с `colorReplacements` сохранился (`ManagedAnimationNode.swift:49`), но то, как тёмная тема знака во вступлении (I-026) рисуется новым плеером, не проверено.
- Новые хосты в `TelegramCore`, `MtProtoKit`, `TelegramUI`, `WebUI`: только `maps.apple.com`.

## Сборка
- `upstream.env`: Xcode 27.1, Bazel 9.2.0 (`versions.json`), macOS 27. NDK, Qt и Node к iOS не относятся.
- `.bazelrc` и `MODULE.bazel` заметно изменились. Добавлены `rules_rust` (Rust 1.98.1) и crate-зависимости двух движков. Новые сабмодули `third-party/mtproto-engine`, `third-party/wallet-engine/wallet-engine` (личный репозиторий `i582`), `submodules/tlottie/tlottie`. Холодная сборка станет тяжелее; насколько, не измерено.
- Опции `Make.py`, на которые опирается `ci/build.sh`, совпали (проверки `expect` прошли), хотя сам файл сильно переписан. Совместимость `pluma.bazelrc` с Bazel 9 проверит только запуск.
- CI: форк использует `macos-26`, на нём Xcode 27.1 нет (`toolset-26.json`, default 26.6). Xcode 27.1 есть в метках `xcode-27` и `xcode-27-xlarge`, образ macOS 27.0.1, но со статусом **preview** (`actions/runner-images` README и `xcode-27-arm64-Readme.md`).
- Локально стоят macOS 26.5 и Xcode 26.4.1, собрать 13.2 здесь нельзя. Работает ли Xcode 27.1 на macOS 26, не проверялось.

## Можно ли обновить отдельно
**Нет, сначала нужен новый layer сервера.**
- ADR 0008 требует переходить вместе, а `patch.py` проверяет совпадение layer.
- Это и техническая необходимость. Старых CRC (`channel` d49f34c6, `userFull` 6cbe645, `messageReplyHeader` 1b97dd66, `inputReplyToMessage` 3bd4b7c2 и другие) в схеме 233 нет, а в `third_party/teamgram-proto` есть только они. Клиент не разберёт ответы сервера, сервер не разберёт ответы с reply.
- Новейший тег `teamgram/proto` — v0.231.0, схемы 233 (и 232) там нет. Для 13.2 понадобится своя кодогенерация TL либо ожидание апстрима (риск из ADR 0008 и STATUS).
- Промежуточный вариант для сведения: 13.0.0 — layer 230, proto v0.230.0 есть, Xcode 26.6 на `macos-26`. Стоимость патча на нём не измерялась.

## Не проверено
Сборка, запуск, поведение клиента против сервера. Реальная совместимость с Xcode 27 preview.

Скрипт и результат: `(одноразовый скрипт)`, `.../ios-measure/new.json`.

Каталог scratchpad общий с другими агентами этого запуска. Мой первый скрипт там был перезаписан, поэтому замер повторён в отдельной папке, числа совпали. Также я удалил `scratchpad/t/old` и `scratchpad/t/new`: если эти пути использовал другой агент, его копии пропали.
