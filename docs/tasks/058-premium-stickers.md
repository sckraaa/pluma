# 058. Премиум-стикеры и эмодзи без Premium

- Статус: in progress (2026-11-08): код и тесты готовы, ждёт выкатки и проверки в клиентах со скриншотами; вопрос об эмодзи — владельцу.
- Зависит от: —
- ADR: —. Подход — как в 053 (реакции и теги без Premium).

## Цель
Премиум-стикеры (с полноэкранной анимацией) и, если владелец подтвердит, премиум-эмодзи отправляются без Premium во всех четырёх клиентах, как обычные.

## Что известно
- **Отчёт владельца 2026-11-07:** премиум-стикеры просят Premium.
- В 053 найдено: клиенты не читают никакого серверного признака, снимающего ограничение, — везде проверяется `user.premium`, а он открыл бы все функции Premium. Поэтому тогда делали патчи клиентов по каждому месту проверки (реестр `clients/patches/*.md`). Здесь, вероятно, так же; места проверки в исходниках четырёх клиентов (пины `upstream.env`) ещё не найдены.
- Решение владельца по 053: «снимаем необходимость наличия премиум для данной фичи». Распространяется ли оно на стикеры и эмодзи — **подтвердить у владельца**; пока задача исходит из того, что да (владелец сообщил об ограничении как о дефекте).

## Область
- Исследование: где каждый клиент запрещает отправку премиум-стикера и премиум-эмодзи без Premium; что сервер должен разрешить (отправка документа премиум-стикера, `messages.getStickerSet` для премиум-наборов).
- Сервер: не отказывать в отправке премиум-стикера и эмодзи.
- Патчи клиентов — минимальные, строкой в реестре каждого.

## Критерии приёмки
- e2e: отправка премиум-стикера аккаунтом без Premium проходит.
- В четырёх клиентах со скриншотами: премиум-стикер отправляется и проигрывается у получателя.

## Вопрос владельцу (премиум-эмодзи)
Патчи — только для стикеров. Открывать ли кастомные эмодзи без Premium так же? Места проверки (исследование, строки закреплённых апстримов):
- **Android** (`aac1efcb`): `ChatActivityEnterView.java:7477-7485`, `11820` (отправка и вставка в текст), `EmojiView.java:1391, 3597, 4190, 4316`, `EditTextEmoji.java:889-891`, `PhotoViewerCaptionEnterView.java:697`, `SelectAnimatedEmojiDialog.java:2625, 2741-2745`, `StickersAlert.java:1218, 1478-1496`, `EmojiPacksAlert.java:975, 1459`, `FeaturedStickerSetCell2.java:302`, `TopicCreateFragment.java:563`, подсказки `MediaDataController.java:9130`. Все смотрят `UserConfig.isPremium()`, `MessageObject.isFreeEmoji`.
- **iOS** (`6ad963e5`): `isPremiumEmoji` в `ChatEntityKeyboardInputNode.swift:729, 1026, 2480, 2885`, `EmojiPagerContentSignals.swift:1218, 1379, 1470`, `EmojiSuggestionsComponent.swift:72, 130`, `InputContextQueries.swift:257, 307`, `ChatTextInputPanelNode.swift:4406`, `EmojisChatInputContextPanelNode.swift:291`, `StickerPackScreen.swift:849`, `ChatInterfaceStateContextQueries.swift`, `ChatControllerNode.swift`, `ChatControllerLoadDisplayNode.swift`.
- **Desktop** (`a1e89e1`): `history_widget.cpp:548, 2317`, `chat_helpers/emoji_list_widget.cpp:3076, 3564, 3604`, `chat_helpers/message_field.cpp:109`, `boxes/sticker_set_box.cpp:907` (`Data::AllowEmojiWithoutPremium`, `session().premium()`).
- **Web K** (`346844bf`): `components/emoticonsDropdown/tabs/emoji.ts:560, 865-868, 993, 1150` (`rootScope.premium`, `isPremiumFeaturesHidden`).

## Результат
Премиум-стикеры отправляются без Premium: в сервере не было ни одной проверки, он отдавал и принимал такие стикеры всегда; мешали только четыре клиента, в них поставлены патчи. Не выкачено и в клиентах со скриншотами не проверено (живая проверка — позже, по критериям приёмки).

- **Сервер.** Продуктового кода нет. Прочитано:
  - отправка документа — `app/bff/messages/internal/core/core_messages_helper.go:166-183` (`inputMediaDocument`) без проверок Premium; `nopremium` сервер никогда не ставит, поэтому получатель проигрывает эффект (Android `ChatActivity.java:15984`, iOS `ChatMessageAnimatedStickerItemNode.swift:1688, 2591`, Web K `wrappers/sticker.ts:742`, tdesktop `history_view_sticker.cpp:140`);
  - `messages.getStickerSet` и каталог отдают стикеры как импортированы; `video_thumbs` типа `f` проходят импорт (`pkg/catalog/importer/importer.go:640, 711`, `tools/catalog-collector/collector.js:231`, наборы по запросу — `pkg/catalog/demand/demand.go:203`, бот скачивает их как любую миниатюру);
  - файл эффекта: `dfs.importDocument` кладёт `f` в видео (`mtproto.PhotoSizeIsVideo("f")`), `upload.getFile` с `thumb_size = "f"` читает его оттуда же (`app/service/dfs/internal/core/dfs.downloadFile_handler.go`, ветка `inputDocumentFileLocation`); клиенты скачивают его как миниатюру (Android `ImageLocation.getForDocument(videoSize, document)`, `EmojiAnimationsOverlay.java:682`).
  - Ответ `upload.getFile` для `f` приходит с типом `storage.fileMp4` (Pluma хранит в видео). В загрузчиках клиентов тип файла не используется: поиск по `Android FileLoadOperation`, tdesktop `file_download_mtproto.cpp`, iOS `Download.swift`/`MultipartFetch.swift`, Web K `apiFileManager.ts` совпадений не дал; Android открывает эффект как `tgs` (`EmojiAnimationsOverlay.java`). Как отвечает настоящий Telegram — не проверено, живой проверкой подтвердить при тесте клиентов.
- **Тесты.** e2e `TestCatalog` дополнен `catalogPremiumSticker`: коллекция с премиум-стикером, `messages.getStickerSet` отдаёт `f` с верным размером, аккаунт без Premium шлёт `messages.sendMedia`, получатель видит документ с `f` без `nopremium`, скачивает эффект, пересылка не ставит `nopremium`.
- **Клиенты** (ветки `task/058-premium-stickers`, не запушены; реестр `clients/patches/*.md`, один `premiumStickersAvailable()` у каждого, только у гейтов стикеров):
  - Android A-108 (`~/pluma-work/wt-058-android`, `54c3da1`), iOS I-108 (`wt-058-ios`, `d8c32bd`), Desktop D-083 (`wt-058-desktop`, `4522de5`), Web W-067 (`wt-058-web`, `0de2ee1`).
  - Эффект у отправителя сразу после отправки: Android требовал Premium (`ChatActivity.java:25320`, `forcePlayEffect`), поставлен патч; Desktop — патч `skipPremiumEffect`; iOS (`ChatMessageAnimatedStickerItemNode.swift:717`) и Web K (`wrappers/sticker.ts:742`) Premium здесь не спрашивают.
  - Подсказка при повторном нажатии на стикер с эффектом молчит при `premium_purchase_blocked` (Android `EmojiAnimationsOverlay.java:840`, iOS `ChatController.swift:8770`, Desktop `history_view_sticker_toast.cpp`, `showFor`); Web K показывает только «набор содержит премиум-стикеры» со ссылкой на набор (`Sticker.Premium.Click.Info`), Premium не предлагает.
- **Проверка** (CachyOS, 2026-11-08): `go build ./...`, `go vet -tags integration,e2e` пакета `test/e2e`, `go test ./tools/...` зелёные; `TestCatalog` на стенде (`run.sh all`) проходит вместе с `catalogPremiumSticker`. Продуктовый Go-код не менялся, поэтому `-race` и тесты импортёров не запускались. e2e подтверждает уже имевшееся поведение сервера: падающего без изменения теста на сервере быть не может. Тесты патч-скриптов падают без правок (`verify_premium_stickers` до применения, порча каждой правки): Android 58 тестов, iOS 60, Desktop 51, Web 35, все OK; iOS, Desktop и Web — на закреплённом апстриме в `./upstream`, Android — применением к файлам закреплённого апстрима из `reference-src`. Сборки клиентов (Gradle, Bazel, CMake, `vite build`) не запускались. `make lint` на чистый результат не вышел: копия рабочего дерева на CachyOS не видит `.git` (файл указывает на путь Mac), поэтому `new-from-rev` не работает и линтер выдаёт сотни старых замечаний всего репозитория; менялись только два файла e2e, для них `go vet` и `gofmt` чистые.
- **Решения** (подтвердить):
  - исходя из отчёта владельца стикеры открыты всем; решение 053 на них формально не распространялось;
  - значок на стикере — как у Premium (звезда в углу на Android и iOS без изменений; на Desktop `StickerPremiumMark` с `unlocked`, панель эмодзи и подарки не меняются; Web K — без замка);
  - Android: список `premiumStickers` панели (`EmojiView.java:6164`) не правится — его код показа закомментирован (`5612, 7039`), он не влияет на панель;
  - Web K: эффекты отправки (`sendContextMenu.ts:118`) и кастомные эмодзи — не стикеры, не трогаются.
