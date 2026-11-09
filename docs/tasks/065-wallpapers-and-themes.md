# 065. Обои и темы

- Статус: done (сервер; проверка полным прогоном и в клиентах — позже)
- Зависит от: 024 (каталог обоев и тем), 014 (Premium), 044 (каналы и права), 053 (решения владельца о Premium)
- ADR: 0016 (Premium), 0020 (seq), 0034 («Ящик канала»)

## Цель
Пользователь загружает свои обои и темы, держит их в своём списке, ставит обои на чат (себе или обоим), откатывает чужой выбор; у канала есть обои и тема чата с правами на их смену. Каталог Telegram остаётся основой, поверх него — своё у аккаунта.

## Что известно
- **Документация** (core.telegram.org, страницы прочитаны через загрузку с сокращением):
  - `api/wallpapers`: `for_chat`, сохранение и установка, `revert`, подтверждение через `id` и `same`, `for_both` только Premium, у канала без `for_both`, заливки (`wallPaperNoFile`) не сохраняются и не устанавливаются, сохранённый список и сброс.
  - `method/messages.setChatWallPaper`: ошибки `PEER_ID_INVALID`, `WALLPAPER_INVALID`, `WALLPAPER_NOT_FOUND`. `method/messages.setChatTheme`: `EMOJI_INVALID`, `EMOJI_NOT_MODIFIED`, `PEER_ID_INVALID`. `method/account.uploadWallPaper`: `WALLPAPER_FILE_INVALID`, `WALLPAPER_MIME_INVALID`, «JPG или PNG». `method/account.createTheme`/`updateTheme`/`getTheme`/`uploadTheme`: `THEME_MIME_INVALID`, `THEME_TITLE_INVALID`, `THEME_INVALID`, `THEME_FORMAT_INVALID`, `THEME_SLUG_INVALID`, `THEME_FILE_INVALID`.
  - `api/themes`: обновить может только создатель, `updateTheme` получают установившие.
- **Клиенты** (`~/pluma-work/reference-src`):
  - **Обновление обоев чата.** Android `ChatThemeController.processUpdate:594`, tdesktop `api_updates.cpp:2196`, iOS `AccountStateManagementUtils.swift:1983` применяют `updatePeerWallpaper`. tdesktop и Android смотрят `wallpaper_overridden` (`data_user.cpp:1005`, `MessageObject.isCurrentWallpaper`), откат доступен, только пока обои выбраны собеседником (`WallPaperRevertable`, `ChatActivity.java:36915`).
  - **Сброс без ответа.** Android игнорирует ответ на очистку и на revert (`ChatThemeController.clearWallpaper:675`) — обновление должно прийти и на устройство, которое просило; поэтому `EmitSeqUpdate` всем устройствам.
  - **Ответ на установку.** Android после `setWallpaperToPeer` ищет `updateNewMessage` со служебным сообщением в ответе (`:823`) — ответ должен содержать его. tdesktop `setExistingForPeer:666` применяет ответ через `applyUpdates`.
  - **`revert`.** iOS: `WALLPAPER_NOT_FOUND` = «обоев нет» (`ChatThemes.swift:375-395`).
  - **Заливка канала.** Android шлёт `inputWallPaperNoFile{id:0}` с `settings.emoticon` (`ChannelColorActivity.java:667-690`).
  - **Текст служебного сообщения.** `same` и `for_both` разбирают Android (`MessageObject.java:4650`) и tdesktop (`history_item.cpp:6474`). Канал показывается как `ActionSetWallpaperForThisChannel` (Android `:4689`).
  - **Тема чата.** tdesktop берёт из служебного сообщения (`history.cpp:1463`), `prepareSetChatTheme` с пустым `emoticon` — «тема выключена» (`:6200`); ответ — `Updates` (`ChatThemeController.java:318`, iOS `ChatThemes.swift:289`). Android `ChatActivity.java:42970` читает тему только у пользователей; tdesktop даёт выбор темы только для пользователей (`window_peer_menu.cpp:1431`). `channelFull.theme_emoticon` читает tdesktop `data_channel.cpp:1506`.
  - **Файл обоев.** tdesktop `checkWallPaperProperties` (`data_document.cpp:734`) требует тип файла (не стикер), миниатюру и размеры; клиенты шлют `image/jpeg` (`window_theme.cpp:660`, Android `MessagesController:6162`), tweb — тип файла (`appDocsManager.ts:406`), iOS — `Wallpapers.swift:145`.
  - **Без `for_chat` обои попадают в список загрузившего:** tdesktop не вызывает `saveWallPaper` после загрузки (`window_theme.cpp:660-672`), а `account.getWallPapers` им возвращает.
  - **Проверка ссылки темы.** Android пробует слаг через `createTheme` с пустым заголовком и считает занятым только `THEME_SLUG_INVALID`/`THEME_SLUG_OCCUPIED` (`ThemeSetUrlActivity.java:512-525`) — поэтому слаг проверяется первым. Правила слага: `checkUrl:450-500` (5–64 символа, `a-zA-Z0-9_`, не с цифры, без `_` по краям).
  - **Тема по формату.** Mime `application/x-tgtheme-<format>` (`MessagesController.java:6247`, `window_theme_editor_box.cpp:587`); `getTheme` для чужого формата отдаётся без документа, клиент берёт настройки (`LaunchActivity.java:5246`).
- **Неизвестно/не проверено:**
  - Что отвечает Telegram на `installTheme` без темы и на выход за длину заголовка темы (использован предел 64 символа, UNVERIFIED).
  - Настоящий сервер Telegram мог бы иначе сочетать `for_both` со сбросом обоев (здесь сброс только своей стороны, как в tdesktop `BackgroundBox::resetForPeer`).
  - Создаются ли темы в чужом списке сразу (здесь создатель получает её в списке — соответствует применению темы сразу после создания у Android и tdesktop).

## Область
- **Хранение** (`migrate-20261109-wallpapers-themes.sql`): `wallpapers`, `user_wallpapers`, `chat_wallpapers`, `themes`, `user_themes`; `channels.wallpaper`, `channels.theme_emoticon`.
- **Методы:** `account.uploadWallPaper`, `saveWallPaper`, `installWallPaper`, `resetWallPapers`, `uploadTheme`, `createTheme`, `updateTheme`, `saveTheme`, `installTheme`, `getTheme`; `getWallPaper`, `getMultiWallPapers`, `getWallPapers`, `getThemes` дополнены своим поверх каталога; `messages.setChatWallPaper`, `messages.setChatTheme`.
- **`media.uploadWallPaperFile`, `media.uploadThemeFile`** вместо заглушек; ошибки записи миниатюр в dfs больше не теряются, плохой файл — `WALLPAPER_FILE_INVALID`/`THEME_FILE_INVALID`.
- **Доставка:** `updatePeerWallpaper` и `updateTheme` — обновления с seq, переживают `getDifference`.
- **Канал:** RPC `channel.setChatWallpaper`, `channel.setChatTheme` под блокировкой канала с правом `ChangeInfo`, служебное сообщение, `updatePeerWallpaper` участникам, `channelFull.wallpaper`/`theme_emoticon`.
- **`userFull`:** `wallpaper`, `wallpaper_overridden`, `theme`.

## Критерии приёмки
- Юнит-тесты: `pkg/appearance`, `app/bff/catalog/internal/core` (слаги, порядок проверок, слияние списков, хэш, формат темы, заливка, mime).
- Интеграционные тесты на MySQL (`pkg/appearance/appearance_integration_test.go`): список, цикл `override`/`revert`, установки темы без двойного счёта под конкуренцией, занятый слаг.
- e2e (`test/e2e/wallpapers_themes_test.go`): обои чата (Premium, служебное сообщение, `same`, сброс), обои канала, облачная тема (слаг, чужой вид, установка, `updateTheme`).

## Решения
- **Обои чата в своей таблице `chat_wallpapers`**, а не в колонках `dialogs` (`wallpaper_id`): настройки принадлежат чату, а не обоям, поля диалога о них не говорят; строки `dialogs` кроме того могут не быть у первого сообщения. Колонки `dialogs.wallpaper_id`, `wallpaper_overridden` и RPC `dialog.setChatWallpaper` остаются неиспользуемыми, как и были.
- **Порядок шагов в личном чате:** сначала служебное сообщение — единственный шаг, который может отказать (заблокировавший собеседник), затем строки обоев своей и чужой стороны, каждая в своём ящике и идемпотентно. Сбой между шагами — ошибка клиенту, повтор безопасен (добавит второе сообщение, но состояние верно).
- **Обои из служебного сообщения (`id`)** — `same = true`, без `for_both`; настройки из запроса поверх настроек сообщения.
- **Откат `for_both`:** предыдущее сохраняется только при первом принудительном выборе, второй раз не затирается; сброс (своя очистка) всё забывает.
- **Флаг `creator`** у обоев — только в ответах загрузившему; в сообщении и у собеседника не ставится.
- **Без `for_chat`** загруженные обои сразу в списке; PNG и JPEG считаются обычными обоями (`pattern` не ставится).
- **Заливка без записи в каталоге:** `inputWallPaperNoFile` с настройками даёт `wallPaperNoFile` с этим `id`; без настроек — `WALLPAPER_INVALID`.
- **Каталог:** `account.getWallPapers`/`getThemes` без личного списка отдают ответ каталога с его хэшем; с личным — хэш считается поверх (id, настройки, изменения темы), поэтому `notModified` работает у обоих.
- **Базовые группы:** `messages.setChatTheme` и `setChatWallPaper` — `PEER_ID_INVALID` (ни один клиент не предлагает, `chatFull` темы/обои не хранит).
- **Тема по подарку** (`inputChatThemeUniqueGift`) — `EMOJI_INVALID`: подарков нет (ADR 0016).

## Результат
Обои и темы работают на сервере: загрузка с JPEG/PNG и миниатюрами, сохранённый список с настройками и удалёнными предустановленными, сброс; обои личного чата на одну сторону или на обе (Premium), применение чужих обоев по сообщению, откат; тема чата с служебным сообщением и `userFull.theme`; обои и тема канала с правом смены информации и `channelFull`; облачные темы с созданием, правкой только создателем, сохранением, установкой со счётчиком и `updateTheme` всем установившим.

Проверено сборкой, `go vet` с `integration,e2e` и `go test` затронутых пакетов на сборочном узле; один тест проверен намеренной поломкой фильтра формата темы (падает). Интеграционные и e2e-тесты и полный прогон (`-race`, `make lint`, `go test ./tools/...`) ещё не запускались — они идут в общем прогоне партии. В живых клиентах не проверено. Не сделано и вынесено в вопросы: боевая проверка загрузки и приёма обоев через клиентов; канальные обои недоступны из интерфейса клиентов, пока нет бустов (см. вопросы владельцу); запись `channelAdminLogEventActionChangeWallpaper` не делается (журнала действий в каналах нет); TGV-паттерны как загрузка не поддержаны (нужен растеризатор, клиенты их не загружают).

## Вопросы владельцу
1. **`for_both` — только Premium?** Сейчас так (решение задачи 014 и документация, Android и tdesktop закрывают кнопку без Premium). В 053 владелец снял Premium с тегов; нужно решить для «обоев на двоих» (если снять, потребуется и патч клиентов, как в 053).
2. **Обои и темы каналов из интерфейса клиентов.** Сервер принимает их без бустов (право `ChangeInfo`), но клиенты сами требуют уровень буста (`channel_wallpaper_level_min`, `channel_custom_wallpaper_level_min`; у нас бустов нет и уровень 0, как и у `channels.updateColor` в 044). Открывать ли: выставить минимальные уровни в 0 в `app_config.json` и `help.getAppConfig`, или оставить закрытым (ADR 0016)?
