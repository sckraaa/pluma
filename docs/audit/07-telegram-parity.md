# Чего Pluma не хватает по сравнению с Telegram

_Сводка на 2026-10-30. Основа:_
- _разбор пяти областей: настройки и аккаунт, список чатов, экран чата, медиа, группы с каналами, ботами, звонками и push;_
- _сравнение с живым Telegram на запасном аккаунте владельца (Web K, Android 13.1.0, iOS 12.9.2, Desktop 7.3.3);_
- _логи сервера за 72 ч._

_Спорные утверждения областей перепроверены по исходникам, расхождения разобраны в разделе 6._

## Как читать

**Корни путей**
- `A:` — Android `TMessagesProj/src/main/java/org/telegram` (DrKLO/Telegram aac1efcb, клиент 12.9.1); `A-res:` — `TMessagesProj/src/main/res`; `A-jni:` — `TMessagesProj/jni`.
- `I:` — Telegram-iOS 6ad963e5 (12.9.2), пути от корня репозитория.
- `W:` — tweb `src` 346844bf (Web K).
- `S:` — `/Users/sckr.a/teamgram-server`.

**Источники наблюдений**
- **Живой Telegram** — наблюдения 2026-10-30: одни и те же методы чтения вызваны через API-клиент Web K у Telegram и у Pluma, сравнивалось только устройство ответов.
- **Логи** — ошибки сервера за 72 ч. Владелец, по его словам, заходил с телефона и, возможно, с веба; STATUS: «Владелец вошёл с Android-телефона». Значит, в логах Android и, может быть, Web K. Про iOS (подписанной сборки ещё нет) и Desktop логи ничего не говорят.

**Статусы методов** (`S:docs/project/api-coverage.md`, `S:app/bff/bff/client/fake_rpc_result.go`)
- `ok` — обработчик есть. Это не значит, что он делает всё как Telegram; где не делает, сказано.
- `partial` — часть веток заблокирована.
- `blocked` — заглушка апстрима: 400 `ERR_ENTERPRISE_IS_BLOCKED`.
- `notimpl` — ответ `METHOD_NOT_IMPL`.
- `missing` — обработчика нет. Прокси отвечает 400 `ERR_ENTERPRISE_IS_BLOCKED` и пишет «License key» в лог (`S:app/bff/bff/client/bff_proxy_client.go:86-93`, `fake_rpc_result.go:231-232`).
- `fake` — обработчика нет, прокси отвечает пустым успехом без лога. Матрица api-coverage считает такие методы `missing`.
- Подмена `fake` срабатывает по имени Go-типа. Типов `TLMessagesGetWebPage`, `TLMessagesGetWebPageView` и `TLMessagesReport` в layer 228 нет. Есть `TLMessagesGetWebPage8D9692A3`/`32CA8F91` и `TLMessagesReport8953AB4E`/`FC78AF9B` (`S:third_party/teamgram-proto/mtproto/schema.tl.sync_service.pb.go:13346, 17730, 54614, 55566`). Поэтому эти три заглушки не срабатывают никогда, и клиент получает 400.

**Размер**
- **малый** — один-два обработчика;
- **средний** — функция: хранилище, несколько методов, обновления;
- **крупный** — этап.

**«Видит»** — вывод из кода клиентов, если не сказано «проверено». На устройствах проверено мало; такие выводы стоит подтвердить на телефоне.

## Коротко

### Самое заметное в обычный день
1. **Нет русского языка.** Android-«Русский» почти весь по-английски, iOS и Web K только английские (1.1).
2. **Нет превью ссылок** ни при наборе, ни в отправленных сообщениях (1.2).
3. **Нет стикеров, анимированных эмодзи, реакций и подсказок эмодзи.** Нужны данные Telegram (3.2–3.5).
4. **Правка сообщения стирает форматирование у всех:** жирный, курсив, спойлер, ссылку-текст (1.3).
5. **Нет вкладки «Медиа» в профиле:** на Android её нет, в Web K пусто. Просмотрщик не листает дальше загруженной истории (1.4).
6. **Архив не синхронизируется.** На Android и iOS чат уходит в архив только на этом устройстве, в Web K не уходит вовсе (1.5).
7. **Звонки не проходят:** кнопка есть во всех клиентах, звонка нет (1.6).
8. **Отложенное сообщение и «Отправить, когда будет в сети»** — красный значок ошибки (1.7).
9. **Опрос** превращается у всех в «сообщение не поддерживается» (1.8).
10. **«Прочитать все упоминания»** не доходит до сервера, и «@» возвращается (1.9).
11. **Настройки приватности, которые сервер принимает, но не выполняет:**
    - публичное фото становится основным и видно всем;
    - «О себе» видят все;
    - автоудаление ничего не удаляет;
    - скрытие времени прочтения не скрывает (1.12).
12. **Push-уведомления.**
    - Android: 13 ошибок регистрации за 72 ч, фоновая доставка держится на отдельном соединении.
    - Web K без открытой вкладки ничего не получает (1.10).
13. **Каналы создать нельзя.** Действия в группе, для которых клиент переводит её в супергруппу, кончаются ошибкой (4.1).

### Дешёвые правки с большим эффектом (малый размер)
- вкладка «Медиа»: SQL и счётчики;
- правка сообщения без потери форматирования;
- `messages.readMentions`;
- регистрация push-сессии типа 7;
- `help.getSupport`;
- удаление под-чата «Избранного» и удаление истории за выбранные дни;
- «Скрыть имя отправителя» и «Ответить в другом чате»;
- `messages.getMessages(inputMessageReplyTo)`;
- «О себе» в 70 символов;
- время в «Кто прочитал»;
- сохранённые GIF;
- дни рождения контактов;
- iOS: `account.getReactionsNotifySettings`.

---

## 1. Ломает или путает ежедневное использование

### 1.1 Язык интерфейса (нужны данные Telegram, см. 3.1)
- **Видит.**
  - Android: «Русский» в списке есть, но интерфейс почти весь английский. В `A-res:values-ru/strings.xml` 257 строк против 11034 в `values`; остальное клиент берёт из языкового пакета сервера.
  - iOS: только `I:Telegram/Telegram-iOS/en.lproj/Localizable.strings`.
  - Web K: список языков пуст (`W:components/sidebarLeft/tabs/language.tsx:98-122`).
  - На экране входа Telegram предлагает «Продолжить на русском», Pluma — «Continue in English» (живой Telegram).
- **Методы.**
  - `langpack.getLanguages`, `getLangPack`, `getDifference`, `getStrings` — fake, пустые ответы (`fake_rpc_result.go:27-50`).
  - `langpack.getLanguage` — missing (`A:ui/LaunchActivity.java:5167`).
  - Где клиенты зовут: `A:messenger/LocaleController.java:3213, 3303-3348`, `A:ui/IntroActivity.java:507`; `I:submodules/TelegramCore/Sources/TelegramEngine/Localization/Localizations.swift:23`; `W:lib/appManagers/appLangPackManager.ts:18-43`.
  - Android и iOS шлют `lang_pack = ""` (`A:messenger/LocaleController.java:3306, 3334`; `Localizations.swift:23`). Значит, сервер должен выбирать пакет по `lang_pack` из `initConnection`. Это вывод из кода, у Telegram не проверено.
- **Живой Telegram:** 29 языков против 0; `langpack.getLangPack` (webk, ru) — 5291 строка против 0.
- **Попутно:** `help.getConfig` отдаёт `suggested_lang_code = "classic-zh-cn"` (`S:teamgramd/bin/config.json:60`) — это остаток апстрима. Как Telegram выбирает это значение, не проверено.
- **При импорте** «Telegram» в строках заменяется на «Pluma» во всех языках, как патчи клиентов уже делают со встроенными строками.
- **Размер:** средний. В STATUS это уже следующая задача (основа — `sckraaa/tg_self_host`, `fetch-all-langpacks.cjs`).

### 1.2 Превью ссылок
- **Видит:** ссылка кликабельна, но карточки нет ни при наборе, ни в отправленном сообщении, ни у собеседника. Instant View не открывается.
- **Методы.**
  - `messages.getWebPagePreview` (#570d6f6f, в Android `TL_account.getWebPagePreview`) — missing.
  - `messages.getWebPage` (#8d9692a3) — missing: заглушка не срабатывает, см. «Как читать».
  - При отправке плагин превью сервиса msg отвечает `ErrMethodNotImpl`, поэтому `messageMediaWebPage` не строится никогда (`S:app/messenger/msg/internal/dao/fake_plugin.go:21-23`).
- **Клиенты:** `A:ui/ChatActivity.java:14129, 14198-14230`; `I:submodules/TelegramCore/Sources/WebpagePreview.swift:154, 219, 276`; `W:components/chat/input.ts:3452-3460`, `W:lib/appManagers/appWebPagesManager.ts:273-284`.
- **Логи:** `messages.getWebPage`. Его зовут Web K при наборе ссылки и Android — в поиске по FAQ в настройках (`A:ui/ProfileActivity.java:14784-14790`) и для Instant View.
- **Живой Telegram:** `getWebPagePreview` отдаёт превью.
- **Что нужно.** Данные Telegram не нужны, нужен серверный сборщик превью:
  - загрузка страницы без обращений во внутреннюю сеть, с лимитами размера и времени;
  - кэш;
  - картинка в dfs.
- **Размер:** средний.

### 1.3 Правка сообщения стирает форматирование
- **Видит** (по коду, на устройстве не проверено): после «Изменить» у всех пропадают жирный, курсив, спойлер, моноширинный, цитата-блок, ссылка-текст и упоминание по имени. Остаются только авто-ссылки, @username и хэштеги.
- **Причина:** `messages.editMessage` — partial.
  - Обработчик берёт сущности клиента (`S:app/bff/messages/internal/core/messages.editMessage_handler.go:88-90`).
  - При новом тексте он их обнуляет (`:113-114`).
  - msg заново строит только авто-сущности (`S:app/messenger/msg/msg/internal/core/msg.editMessageV2_handler.go:74`).
  - Android шлёт текст вместе с `entities` (`A:messenger/SendMessagesHelper.java:3371-3386`).
- Это тихая порча переписки при каждой правке. Сравнительный сценарий «Избранное» правку проверяет; судя по этому, без форматирования. Сценарий стоит расширить.
- **Размер:** малый.

### 1.4 Вкладка «Медиа» и листание в просмотрщике
- **Видит.**
  - Android: в профиле и инфо чата вкладки «Медиа» нет, даже если в чате есть фото. Вкладка добавляется только при `hasMedia[0] > 0` (`A:ui/Components/SharedMediaLayout.java:6884, 6998`). Файлы, Голосовые, Ссылки, Музыка и GIF работают.
  - Web K: «Медиа» пуста.
  - iOS: показывает только то, что уже загружено с историей (не проверено).
  - Полноэкранный просмотрщик листает только загруженную часть истории; «N из M», вероятно, неверно (`A:ui/PhotoViewer.java:4345, 14410`).
- **Причины.** Обе на сервере, данные Telegram не нужны.
  - **Поиск.** `messages.search` с `inputMessagesFilterPhotoVideo` (partial) всегда отвечает пусто.
    - В запросе `SelectByPhotoVideoMediaTypeWithCB` 5 плейсхолдеров, а передаётся 6 аргументов (`S:app/service/biz/message/internal/dal/dao/mysql_dao/messages_ext_dao.go:44, 47`).
    - go-zero такой запрос отвергает («arguments provided, not matching sql»), ошибка проглатывается.
    - Это код апстрима, теста на него нет.
  - **Счётчики.** `messages.getSearchCounters` (partial) считает PhotoVideo по `message_filter_type = 0`.
    - Места в коде: `S:app/bff/messages/internal/core/messages.getSearchCounters_handler.go:87-88`, `S:app/service/biz/message/internal/core/message.getSearchCounter_handler.go`.
    - Фото и видео при этом хранятся с типами 7 и 8 (`S:third_party/teamgram-proto/mtproto/message_filter_util.go:34, 41-42`).
    - Photos и Video всегда 0: они уходят в ветку default (`getSearchCounters_handler.go:101-119`).
- **Размер:** малый, плюс интеграционный тест на настоящем MySQL.

### 1.5 Архив
- **Видит.**
  - Android и iOS сразу переносят чат в архив у себя и глотают ошибку сервера (`A:messenger/MessagesController.java:12320-12404`; `I:submodules/TelegramCore/Sources/State/ManagedSynchronizeGroupedPeersOperations.swift:144-152`). На других устройствах и после нового входа чат снова в основном списке.
  - Web K переносит чат только после ответа сервера (`W:lib/appManagers/appMessagesManager.ts:7386-7408`), поэтому «Архивировать» ничего не делает.
- **Методы.**
  - `folders.editPeerFolders` — missing (400, `S:app/bff/folders/internal/server/grpc/service/not_served.go:30-32`); `folders.deleteFolder` — missing.
  - `messages.getDialogs(folder_id=1)` — partial, всегда пусто.
  - «Архивировать новые чаты» скрыто: `autoarchive_setting_available=false` (`S:teamgramd/bin/app_config.json:17`).
- **Живой Telegram:** `account.getGlobalPrivacySettings` по умолчанию отдаёт `keep_archived_unmuted` и `keep_archived_folders`, у Pluma этих флагов нет.
- **Размер:** средний: папка у диалога, `editPeerFolders` с `seq`, `getDialogs`/`getPinnedDialogs` по папке, флаги `keep_archived_*`.

### 1.6 Звонки: кнопка есть, звонка нет
- **Почему видна кнопка.** У любого собеседника есть кнопка звонка:
  - `help.getConfig`: `phonecalls_enabled=true` (`S:teamgramd/bin/config.json:4`);
  - `users.getFullUser` отдаёт `phone_calls_available` и `video_calls_available = true` (`S:app/bff/users/internal/core/users.getFullUser_handler.go:79-83, 169-177`).
- **Что происходит в клиентах.**
  - Android: экран звонка, затем «Звонок не удался» (`A:voip/VoIPService.java:1056-1072, 1167-1171`; `A:ui/VoIPFragment.java:1934-1935`).
  - iOS: `getDhConfig` повторяется без конца, звонок висит в «Запрос…», пока не положить трубку (`I:submodules/TelegramCore/Sources/SecretChats/SecretChatEncryptionConfig.swift:8-10`, `I:submodules/TelegramCore/Sources/Network/Network.swift:1240-1243`).
  - Desktop: панель звонка закрывается (tdesktop v7.0.9 `calls/calls_instance.cpp`, `Instance::refreshDhConfig`).
  - Web K: окно закрывается без сообщения (`W:lib/calls/callsController.ts:536-571`).
  - Собеседник ничего не получает.
  - Групповой звонок на Android: панель открывается и молча закрывается (`A:voip/VoIPService.java:1947, 1983-1986`; `A:ui/LaunchActivity.java:7180-7183`).
- **Методы:**
  - `messages.getDhConfig` — missing, падает первым;
  - `phone.*` — 45 из 45 missing;
  - `phone.getCallConfig` — fake `"{}"` (`fake_rpc_result.go:212-216`).
- **Логи:** `phone.*` и `getDhConfig` в них нет — звонить ещё не пробовали.
- **Размер:** сами звонки — крупный (4.2, ADR 0004 в статусе proposed). Пока их нет, оставлять кнопку или не объявлять звонки доступными — решение владельца (раздел 7).

### 1.7 Отложенные сообщения и «Отправить, когда будет в сети»
- **Видит:** сообщение остаётся с красным значком «не отправлено», без пояснения (`A:messenger/SendMessagesHelper.java:8255-8268`; в `A:ui/Components/AlertsCreator.java:448-516` нет ветки для этой ошибки). Список отложенных пуст.
- **Методы.**
  - `sendMessage`/`sendMedia`/`sendMultiMedia`/`forwardMessages` с `schedule_date`: сервис msg на любой `schedule_date` отвечает `ERR_ENTERPRISE_IS_BLOCKED` (`S:app/messenger/msg/msg/internal/core/msg.sendMessageV2_handler.go:35-39`).
  - `messages.getScheduledHistory`, `sendScheduledMessages`, `deleteScheduledMessages` — missing.
  - `getScheduledMessages` — fake.
- **Документация:** core.telegram.org/api/scheduled-messages; дата 0x7FFFFFFE значит «когда будет в сети».
- **Размер:** средний: хранилище, планировщик, отдельная история, обновления о новых и удалённых отложенных, отправка при выходе собеседника в сеть.

### 1.8 Опросы и чек-листы
- **Видит.** Android даёт кнопку «Опрос» в группах, в «Избранном» и у ботов (`A:ui/Components/ChatAttachAlert.java:5997-6003`). После отправки у всех, включая автора, — «This message is not supported on your version…» (`A:messenger/MessageObject.java:6017-6018`). Чек-лист у пользователя с Premium — так же.
- **Методы.**
  - `messages.sendMedia` с `inputMediaPoll` или `inputMediaTodo`: сервер подменяет медиа на `messageMediaUnsupported` (`S:app/bff/messages/internal/core/core_messages_helper.go:219-222, 260-262`).
  - `sendVote`, `getPollResults`, `getPollVotes`, `addPollAnswer`, `appendTodoList`, `toggleTodoCompleted` — missing.
- **Размер:** средний.

### 1.9 Упоминания: «прочитать все»
- **Видит.**
  - Android: «Прочитать» в списке чатов и долгое нажатие на «@» сбрасывают счётчик только на этом устройстве (`A:messenger/MessagesController.java:14602-14613`; `A:ui/DialogsActivity.java:9576, 9602`; `A:ui/ChatActivity.java:46450`).
  - Web K — так же (`W:lib/appManagers/appMessagesManager.ts:8813-8818, 8826-8840`).
  - На других устройствах «@» остаётся, а после сверки с сервером возвращается и здесь.
  - iOS работает: он использует `getUnreadMentions` + `readMessageContents` (`I:submodules/TelegramCore/Sources/State/ManagedSynchronizeMarkAllUnseenPersonalMessagesOperations.swift:126, 160`).
  - Упоминание, которое пролистали в чате, снимается на всех устройствах.
- **Методы:** `messages.readMentions` — blocked (`S:app/bff/messages/internal/core/messages.readMentions_handler.go:27-31`); `readHistory` упоминания не трогает (`S:app/messenger/msg/internal/dao/read.go:61`).
- **Размер:** малый.

### 1.10 Уведомления

**Android: push-сессия типа 7**
- **Видит пользователь:** ничего.
- **Логи:** 13 ошибок `account.registerDevice` → `TOKEN_TYPE_INVALID` за 72 ч, все с `token_type 7`. В одной из выборок их 10.
- **Откуда.**
  - tgnet регистрирует внутреннюю push-сессию MTProto (`A-jni:tgnet/ConnectionsManager.cpp:2398-2419`, `token_type = 7`).
  - Он повторяет регистрацию на каждый pong push-соединения, пока она не пройдёт (`:1165-1170`).
  - Push-соединение включено, потому что сервер отдаёт `background_connection: true` (`S:teamgramd/bin/app_config.json:76`).
- **Сервер.**
  - session сам привязывает push-сессию (`S:app/interface/session/internal/sess/session_invoke.go:372-378`) и будит её `updatesTooLong` (`session_sync.go:30-41`).
  - Затем bff отвечает `TOKEN_TYPE_INVALID`: принимаются только типы 1, 2, 9 и 10 (`S:app/bff/notification/internal/core/account.registerDevice_handler.go:73-87`; `S:pkg/pushdevices/pushdevices.go:30-35`).
  - У Telegram тип 7 — «MTProto separate session» (core.telegram.org/api/push-updates).
- **Следствие.**
  - Пока push-соединение живо, авторизация считается онлайн (`S:app/interface/session/internal/sess/main_auth_wrapper.go:316-321, 779-783`), и FCM ей не шлётся (ADR 0017, п. 4).
  - Фоновая доставка держится на этом TCP-соединении. Как это работает в режиме Doze, не проверено.
- **Ошибка в ADR:** ADR 0017 (`S:docs/adr/0017-push-notifications.md:9`) утверждает, что Android тип 7 сам не регистрирует. Это неверно.
- **Размер:** малый: принимать тип 7 без записи в `push_devices`. Плюс новый ADR вместо неверного пункта и проверка доставки в Doze.

**Web K**
- Web push (тип 10) хранится, но не отправляется (ADR 0017, п. 7). Уведомления приходят, только пока вкладка открыта (`W:lib/webPushApiManager.ts:236`).
- Причина из STATUS («нет TLS-раздачи Web K») устарела: web.pluma.chat с TLS работает.
- **Размер:** средний (VAPID, шифрование Web Push).

**iOS**
- Push не проверен: нужна подписанная ad hoc-сборка, это действие владельца.
- VoIP-токен (тип 9) хранится, но не используется.

**Нет push на правки, реакции и удаления** (известная проблема в STATUS).

**Уведомления о реакциях**
- `account.getReactionsNotifySettings` и `setReactionsNotifySettings` — missing (`A:messenger/MessagesController.java:12612`; `W:lib/appManagers/appNotificationsManager.ts:184, 209`).
- Android и Web K показывают локальные значения, сохранение отвечает ошибкой.
- На iOS последствия серьёзнее, см. 1.13.

### 1.11 «Задать вопрос»
- **Видит.**
  - Android: «Задать вопрос» → «Спросить» → крутилка → ничего. Ветка ошибки только закрывает крутилку (`A:ui/Components/AlertsCreator.java:1975-1982`).
  - iOS: «ОК» → ничего (`I:submodules/TelegramUI/Components/PeerInfo/PeerInfoScreen/Sources/PeerInfoScreenSettingsActions.swift:174-189`).
  - У Telegram открывается чат с поддержкой.
- **Метод:** `help.getSupport` — blocked (`S:app/bff/configuration/internal/core/help.getSupport_handler.go:24-30`). Служебный аккаунт с username `support` у Pluma уже есть (STATUS, `ab047bca`).
- **Размер:** малый.

### 1.12 Настройки приватности и безопасности, которые сервер не выполняет
Пользователь думает, что защищён. Данные Telegram здесь не нужны.

| Настройка | Что происходит | Доказательства | Размер |
|---|---|---|---|
| Публичное фото (Конфиденциальность → Фото профиля → «Установить публичное фото») | Становится основным, его видят все; клиент у себя считает его публичным | Клиент шлёт `fallback=true` (`A:ui/PrivacyControlActivity.java:242-266`). Сервер флаг не читает (`S:app/bff/userchannelprofiles/internal/core/photos.uploadProfilePhoto_handler.go:30-56`). `users.getFullUser` отдаёт `FallbackPhoto: nil` (`users.getFullUser_handler.go:94`) | малый–средний |
| «О себе»: кто видит | Правило хранится, но `getFullUser` всегда отдаёт описание. Android подписывает поле «видят только контакты» | `users.getFullUser_handler.go:90`; `S:app/service/biz/user/internal/dao/user_privacy.go:75` | малый |
| Исключения «участники группы» в правилах | В `getFullUser` не срабатывают: `cbChat` всегда false | `users.getFullUser_handler.go:222-228` | малый |
| «Скрывать время прочтения» (`hide_read_marks`) | Хранится, но `messages.getOutboxReadDate` его не проверяет | `S:app/service/biz/user/internal/core/user.setGlobalPrivacySettings_handler.go:38`; в `S:app/bff` и `S:app/messenger` нет ни одного использования `HideReadMarks` | малый |
| Автоудаление для новых чатов | Сохраняется, но при отправке `TtlPeriod: nil` | `S:app/bff/privacysettings/internal/core/messages.setDefaultHistoryTTL_handler.go:28-42`; `messages.sendMessage_handler.go:110` | средний (вместе со следующей строкой) |
| Таймер автоудаления в чате | `messages.setHistoryTTL` — notimpl. Android ставит таймер у себя и не откатывает его: он выглядит включённым, но ничего не удаляется, а собеседник не получает служебного сообщения | `S:app/bff/dialogs/internal/core/messages.setHistoryTTL_handler.go:27-31`; `A:messenger/MessagesController.java:9989-10010` | |
| «Завершать сеансы при неактивности» | Сохраняется (ошибка записи проглатывается), но кода, который завершал бы сеансы, нет | `S:app/service/biz/user/internal/core/user.setAuthorizationTTL_handler.go:28-30` | малый–средний |
| «Удалить аккаунт при неактивности» | Срок хранится (по умолчанию 548 дней), кода удаления нет | `S:app/service/biz/user/internal/dao/user.go:90` | средний (разрушительное действие: нужны осторожность и тесты) |
| Одноразовые фото и видео | `ttl_seconds` сохраняется, но после просмотра медиа на сервере не истекает. На другом устройстве получателя его, вероятно, снова видно (не проверено) | `S:app/bff/messages/internal/core/core_messages_helper.go:80, 104, 173`; в `S:app/messenger` и `S:app/service/biz` `TtlSeconds` нигде не обрабатывается | средний |
| Удаление аккаунта | Пользователь помечается удалённым, сеансы снимаются, но переписка не стирается («TODO … Clear account data») | `S:app/bff/account/internal/core/account.deleteAccount_handler.go:42-131` | средний |

**Риски безопасности, которых пользователь не видит**
- **Доступ к файлам.** `upload.getFile` не проверяет `access_hash` и `file_reference`: любой вошедший скачает файл, зная его id. `access_hash` используется только как тип хранения (`S:app/service/dfs/internal/core/dfs.downloadFile_handler.go:74`). Отсутствующий файл отдаётся пустыми байтами вместо ошибки (`:64-73, 125-128, 143-147`). **Размер:** средний.
- **Большие файлы.** Клиенту объявлено 4000 частей (2 ГБ), но dfs собирает файл в памяти, а число частей не проверяет (`S:docs/tasks/015-app-config.md:88`). **Размер:** средний.

### 1.13 iOS (по коду; к выходу iOS-сборки)
Сборки ещё нет у пользователей, но эти места стоит закрыть заранее: они дают видимые сбои и постоянную нагрузку на сервер.
- **Настройки уведомлений с сервера не загрузятся никогда.**
  - `account.getReactionsNotifySettings` (missing) стоит в одном `combineLatest` с четырьмя другими запросами (`I:submodules/TelegramCore/Sources/State/ManagedGlobalNotificationSettings.swift:108-117`).
  - `retryRequestIfNotFrozen` повторяет его без конца, с задержкой до 5 с (`I:submodules/TelegramCore/Sources/Network/Network.swift:1245-1256`).
  - Экран покажет значения по умолчанию, отключение звука с других устройств iOS не увидит, а пять запросов будут уходить каждые ≤5 с.
  - **Размер:** малый.
- **«Принять все» и «Отклонить все» заявки на вступление.** `messages.hideAllChatJoinRequests` — blocked, iOS повторяет его бесконечно, и заявки возвращаются (`I:submodules/TelegramCore/Sources/TelegramEngine/Peers/InvitationLinks.swift:23-35`). Android принимает заявки по одной, это работает. **Размер:** малый.
- **Не подгружаются заголовки ответов на сообщения вне загруженной истории.**
  - iOS запрашивает такие сообщения через `messages.getMessages(inputMessageReplyTo)` (`I:submodules/TelegramCore/Sources/State/Holes.swift:251`; `AccountIntermediateState.swift:183-194`).
  - Сервер понимает `inputMessageReplyTo` как id самого сообщения (`S:app/bff/messages/internal/core/messages.getMessages_handler.go:44-51`). По документации это «Message to which the specified message replies to» (core.telegram.org/constructor/inputMessageReplyTo).
  - `inputMessagePinned` и `inputMessageCallbackQuery` сервер молча пропускает.
  - **Размер:** малый.
- **Постоянные повторы.** Около 15 неподдержанных методов (реакции, эмодзи-статусы, Stars, `getStickerSet`) повторяются примерно раз в 15 с (STATUS). Звонок висит в «Запрос…» (1.6).

---

## 2. Видимые пробелы, которые пользователь заметит скоро

### 2.1 Поиск
- **Глобальный поиск по сообщениям.**
  - `messages.searchGlobal` в матрице ok, но в хранилище уходят только `q`, `offset_id` и `limit`, а сам поиск — LIKE по тексту (`S:app/bff/messages/internal/core/messages.searchGlobal_handler.go:40-75`; `S:app/service/biz/message/internal/dal/tables/messages.xml:356`).
  - Не действуют: фильтры «Личные / Группы / Каналы» (`A:ui/Adapters/DialogsSearchAdapter.java:550-556`; `W:lib/appManagers/appMessagesManager.ts:12437-12440`), папка (в том числе архив) и даты.
- **Вкладки «Медиа», «Ссылки», «Файлы», «Музыка», «Голосовые».**
  - Клиент шлёт `q=""` с фильтром (`A:ui/FilteredSearchView.java:623-628, 665-674`); без текста сервер отвечает `SEARCH_QUERY_EMPTY` (`searchGlobal_handler.go:40-44`).
  - С текстом во вкладках оказываются обычные текстовые сообщения.
- **Вкладки Android «Каналы», «Посты», «Приложения».**
  - «Каналы» показывает сообщения из личных чатов и групп: `broadcasts_only` игнорируется (`A:ui/Components/DialogsChannelsAdapter.java:198-205`).
  - «Посты» — ошибка или пусто: `channels.searchPosts` и `checkSearchPostsFlood` — blocked.
  - «Приложения» пусты: `bots.getPopularAppBots` — missing.
- **Поиск внутри чата.** `messages.search` — partial (`S:app/bff/messages/internal/core/messages.search_handler.go`).
  - Пусто по голосовым, кружкам, «моим упоминаниям», гео и контактам (`:175-177, 230-241`). iOS шлёт фильтры Voice и RoundVideo (`I:submodules/TelegramCore/Sources/State/Holes.swift:112-115`).
  - `from_id` в личном чате даёт `PEER_ID_INVALID`.
  - Внутри под-чата «Избранного» — пусто (`search_handler.go:90-93`), счётчики медиа там нулевые (`getSearchCounters_handler.go:36-38`).
- **Календарь и прокрутка.**
  - Календарь по нажатию на дату и «Показать по датам» в медиа: `messages.getSearchResultsCalendar` — blocked (`A:ui/CalendarActivity.java:472-493`).
  - Быстрая прокрутка медиа: `messages.getSearchResultsPositions` — notimpl, отвечает `count = 0` (`A:ui/Components/SharedMediaLayout.java:5077`).
- **Размер:** средний.

### 2.2 GIF
- **Сохранённые GIF пропадают.**
  - `messages.saveGif` — fake: отвечает true и ничего не сохраняет. `getSavedGifs` — fake: всегда пусто.
  - При синхронизации Android заменяет список пустым ответом и стирает кэш (`A:messenger/MediaDataController.java:2000-2018, 2117-2118`). На других устройствах сохранённых GIF нет.
  - **Размер:** малый–средний.
- **Поиск GIF, популярные GIF и поиск картинок в вебе.**
  - Сервер объявляет ботов `gif` и `bing` (`S:teamgramd/bin/config.json:45-53`), но их нет, поэтому `USERNAME_NOT_OCCUPIED`; `messages.getInlineBotResults` — missing.
  - Свои GIF из галереи отправляются.
  - Нужен внешний провайдер (решение владельца) и боты (4.4).

### 2.3 Пересылка и ответы
- **«Скрыть имя отправителя» и «Скрыть подписи» не действуют.**
  - Флаги `drop_author` и `drop_media_captions` (`A:messenger/SendMessagesHelper.java:2535-2536`) в `S:app/bff` и `S:app/messenger` нигде не читаются.
  - Если приватность пересылки скрывает автора, в `fwd_from.from_name` попадает username или пустая строка вместо имени (`S:app/bff/messages/internal/core/messages.forwardMessages_handler.go:213-251, 288-311`; core.telegram.org/constructor/messageFwdHeader).
  - **Размер:** малый.
- **«Ответить в другом чате»** (Android, `A:ui/Components/MessagePreviewView.java:964`).
  - Текст уходит без ответа: `reply_to_peer_id` выбрасывается (`S:app/bff/messages/internal/core/messages.sendMessage_handler.go:164-168`).
  - Медиа уходит с ответом на сообщение с тем же номером в текущем чате (`messages.sendMedia_handler.go:120-134`).
  - **Размер:** малый.

### 2.4 Удаление
- **Удаление за выбранные дни** (выделение дней в календаре Android, `A:ui/ChatActivity.java:1351`).
  - `messages.deleteHistory` с `min_date`/`max_date` ничего не удаляет и отвечает успехом (`S:app/bff/messages/internal/core/messages.deleteHistory_handler.go:45-59`).
  - Android по успешному ответу удаляет сообщения у себя (`A:messenger/MessagesController.java:23004-23025`); у собеседника и на других устройствах они остаются.
  - **Размер:** малый.
- **Удаление под-чата «Избранного».**
  - `messages.deleteSavedHistory` — blocked (`S:app/bff/savedmessagedialogs/internal/core/messages.deleteSavedHistory_handler.go:27-31`).
  - Android удаляет у себя и ошибку не показывает (`A:messenger/MessagesController.java:10305-10321`); сообщения остаются на сервере.
  - В Web K удаление не проходит (`W:lib/appManagers/appMessagesManager.ts:6035`).
  - **Размер:** малый.

### 2.5 Жалобы
- «Пожаловаться» на сообщение — ошибка: `messages.report` — missing, заглушка не срабатывает из-за имени типа.
- «Это спам» (`messages.reportSpam`) и жалоба на профиль (`account.reportPeer`) — fake: успех без действия (`fake_rpc_result.go:201-210`).
- Куда должны идти жалобы на закрытом сервере (например, в очередь для администратора в `pluma-admin`) — решение владельца.
- **Размер:** малый–средний.

### 2.6 Служебный чат 777000 и «Новый вход»
- **Живой Telegram:** 777000 — обычный чат с историей на сервере. Новая веб-сессия видит сообщения с 2024 года: коды, «New login», смена 2FA, «Incomplete login attempt». У каждого обычный id.
- **Pluma.**
  - Коды входа — настоящие сообщения от 777000 (`S:app/bff/authorization/internal/core/core.go:129-185`).
  - «Новый вход» — `updateServiceNotification` (`S:app/bff/authorization/newlogin/newlogin.go:153`; ADR 0022, п. 4, `S:docs/adr/0022-seq-except-authorization.md:20`): клиент сам создаёт локальное сообщение, и новое устройство его не видит.
  - В Pluma Web чата 777000 не было вовсе (живое наблюдение).
- **Строка устройства:** у Telegram «Android, 15 (35)», у Pluma «SDK 35» (живое наблюдение). Android шлёт `system_version` вида «SDK nn» (`A:tgnet/ConnectionsManager.java:229`).
- **Размер:** малый–средний, плюс новый ADR вместо п. 4 ADR 0022. Дубль из `getDifference`, ради которого п. 4 и писался, у настоящего сообщения невозможен: у него есть id.

### 2.7 Ссылки наружу
- **Пригласительные ссылки в группу.**
  - Сервер выдаёт `https://t.me/+ХЕШ` (`S:app/service/biz/chat/internal/dao/chat_invite.go:24`, `S:pkg/env2/env2.go:16`), хотя `me_url_prefix` — `https://pluma.chat/` (`S:teamgramd/bin/config.json:44`).
  - Внутри Android ссылку t.me открывает само приложение (`A:messenger/browser/Browser.java:726`), а вне приложения она ведёт на сайт Telegram.
  - Патч pluma-android меняет только ссылки входа на Desktop и Web (`/Users/sckr.a/pluma-android/ci/patch.py:633-640`); домен pluma.chat клиенты своим не считают.
  - Переход на pluma.chat затрагивает сервер, четыре клиента и страницу на pluma.chat.
  - **Размер:** средний; решение владельца.
- **«Пригласить друзей».**
  - `help.getInviteText` — blocked. Android подставляет запасную `https://telegram.org/dl` (`A:messenger/ContactsController.java:357-377`), патч её не меняет (`/Users/sckr.a/pluma-android/ci/patch.py:577-579`). Друг получает ссылку на Telegram.
  - iOS берёт ссылку из строки `InviteText_URL` (`I:submodules/ContactListUI/Sources/InviteContactsController.swift:131-133`); заменена ли она в сборке Pluma, не проверено.
  - **Размер:** малый.
- **Ссылка или QR своего контакта без username.** `contacts.exportContactToken` и `importContactToken` — missing: служба `RPCProfileLinks` никуда не направлена. **Размер:** малый–средний.

### 2.8 Дни рождения и серверные подсказки
- **Дни рождения контактов.**
  - `contacts.getBirthdays` в матрице ok, но biz всегда отдаёт пустой список (`S:app/service/biz/user/internal/core/user.getBirthdays_handler.go:29-36`, «TODO: not impl»). Баннера «Сегодня день рождения» и отметок нет ни на одном клиенте.
  - На Android и Web K баннер вдобавок скрыт при `premium_purchase_blocked=true`: нажатие на него ведёт к подарку (`A:ui/DialogsActivity.java:6088`; `W:components/sidebarLeft/birthdaySuggestions.tsx:224-231`; `S:teamgramd/bin/app_config.json:2`).
  - iOS показывает баннер без этого условия.
- **Серверные подсказки** (`BIRTHDAY_SETUP`, `USERPIC_SETUP`, `VALIDATE_PASSWORD`, `VALIDATE_PHONE_NUMBER` и др.).
  - `help.getPromoData` — fake `promoDataEmpty`. Живой Telegram отдаёт `promoData` с `pending_suggestions`.
  - `help.dismissSuggestion` — blocked (core.telegram.org/api/config, раздел Suggestions).
- **Размер:** малый для дней рождения, малый–средний для подсказок.

### 2.9 Профиль
- **«О себе».**
  - Сервер ограничивает 128 байтами вместо 70 символов (`S:app/bff/userchannelprofiles/internal/core/account.updateProfile_handler.go:55-56`), а клиенту объявлено 70 (`app_config.json:41`). Кириллица длиннее ~64 символов получает `ABOUT_TOO_LONG`.
  - Ошибка записи проглатывается: ответ успешный, а текст остаётся старым (`:62-70`).
  - **Размер:** малый.
- **«last seen Jan 1, 1970».** Web K показывает так самого пользователя (STATUS). В `account.updateStatus` стоит «TODO: push» (`S:app/bff/userchannelprofiles/internal/core/account.updateStatus_handler.go:78`). **Размер:** малый.
- **Личный канал** выбрать нельзя: каналов нет (4.1).

### 2.10 Группы
- **«Кто прочитал»:** время всегда 0, и Android его не показывает (`S:app/bff/chats/internal/core/messages.getMessageReadParticipants31C1C44F_handler.go:77-80, 94`; `A:ui/MessageSeenView.java:416-419`). **Размер:** малый.
- **Закреплённое сообщение группы:** `getFullChat` не заполняет `pinned_msg_id` (TODO, `S:app/bff/chats/internal/core/messages.getFullChat_handler.go:61-83`). Видимый эффект не проверен. **Размер:** малый.
- **Выбор реакций группы** — см. 3.2.

### 2.11 Карта, обложки, превью файлов
- **Карта в геопозиции (Android).** Вместо карты пустой прямоугольник (по коду).
  - Сервер объявляет `static_maps_provider = telegram` (`S:teamgramd/bin/config.json:54-56`).
  - Поэтому Android берёт карту через `upload.getWebFile` (`A:messenger/MessagesController.java:5749-5781`), а этот метод — blocked (`S:app/bff/files/internal/core/upload.getWebFile_handler.go:31-44`).
  - Источник карт — решение владельца: ключи и оплата провайдера или свой рендер.
- **Обложки и превью.**
  - Обложки музыки отдаются как `WEBFILE_NOT_AVAILABLE`.
  - Превью документа и обложку видео, присланные клиентом, сервер не сохраняет; у видео превью — первый кадр из ffmpeg (`S:app/service/dfs/internal/core/dfs.uploadMp4DocumentMedia_handler.go:90`).
- **Размер:** малый–средний.

### 2.12 Синхронизация настроек между устройствами
- **Автозагрузка медиа:** `account.saveAutoDownloadSettings` — blocked (`A:messenger/DownloadController.java:1068`).
- **Тема и узор:** `account.installTheme` и `installWallPaper` — missing. Android шлёт их при каждом выборе (`A:messenger/MessagesController.java:9073-9100`); в логах ошибки.
- **Встроенный браузер:** `account.getWebBrowserSettings` и связанные методы — blocked (`A:messenger/MessagesController.java:25267-25290`); в логах ошибка.
- **Свой фон.**
  - Android ставит его только у себя (`A:messenger/MessagesController.java:6162-6185`).
  - Web K: своё изображение фоном не ставится, плитка удаляется (`W:components/sidebarLeft/tabs/background.tsx:409-433`) — `account.uploadWallPaper` missing.
  - Фон отдельного чата: `messages.setChatWallPaper` — missing.
- **Размер:** малый для каждого пункта. Для своих обоев данные Telegram не нужны.

### 2.13 Свои звуки уведомлений
- `account.getSavedRingtones` (есть в логах), `uploadRingtone` и `saveRingtone` — missing.
- Загрузить свой звук или сохранить аудио из чата как рингтон нельзя: ошибка (`A:messenger/ringtone/RingtoneUploader.java:39-55`).
- Часть dfs и media для рингтонов уже есть (`S:app/service/dfs/internal/core/dfs.uploadRingtoneFile_handler.go`).
- **Размер:** средний.

### 2.14 Помощь
- **FAQ и Политика конфиденциальности:** после крутилки открываются в браузере на telegram.org (`A:messenger/browser/Browser.java:307-340`), если патч задачи 017 не заменил `TelegramFaqUrl` и `PrivacyPolicyUrl` (не проверено).
- **Поиск в настройках** не находит статьи FAQ (`messages.getWebPage`, `A:ui/ProfileActivity.java:14778-14792`).
- **«Возможности Telegram»** (Android, t.me/TelegramTips) и **«Советы»** (iOS) ничего не открывают.
- Содержимое этих страниц — решение владельца. **Размер:** малый.

### 2.15 Длина подписи к медиа — сверить с Telegram
- `help.getConfig` отдаёт `caption_length_max = 200` (`S:teamgramd/bin/config.json:57`), а appConfig — `caption_length_limit_default = 1024` (`app_config.json:39`).
- Android берёт значение из `help.getConfig` (`A:messenger/MessagesController.java:5725`):
  - для поля подписи при выборе фото из галереи (`A:ui/PhotoPickerActivity.java:996`, `A:ui/PhotoAlbumPickerActivity.java:370`);
  - при правке подписи (`A:ui/Components/ChatActivityEnterView.java:9961`).
- Там подпись длиннее 200 символов не ввести и не отредактировать.
- Значение Telegram на живом сервере не сверялось (по памяти 1024, не проверено).
- **Размер:** малый.

### 2.16 Секретные чаты (как это выглядит)
- Android: «Новый секретный чат» (`A:ui/ContactsActivity.java:607`) и «Начать секретный чат» в профиле (`A:ui/ProfileActivity.java:2771`) → крутилка → она молча закрывается.
- Причина: первым идёт `messages.getDhConfig` (`A:messenger/SecretChatHelper.java:1935`), а ветка ошибки только закрывает крутилку (`:2031-2042`).
- Сама функция — 4.3.

### 2.17 Пользователи с Premium от администратора
Они видят пункты, которые не работают:
- **«Telegram Business»** (`A:ui/SettingsActivity.java:730-732`): часы работы, место, приветствие, отсутствие, быстрые ответы — ничего не сохраняется. `account.updateBusiness*` и `messages.getQuickReplies` — missing (`getQuickReplies` есть в логах).
- **Расшифровка голосовых:** кнопка есть, результат пустой. `messages.transcribeAudio` — missing; Android при ошибке показывает пустую расшифровку (`A:ui/Components/TranscribeButton.java:740-747`).
- **Чек-лист** превращается в «не поддерживается».
- **Эмодзи-статус, цвет имени и профиля, кастомные эмодзи, теги «Избранного»** — пусто или ошибка (данные — раздел 3).
- **Экран Premium** — без промо-видео. В Web K окно Premium, судя по коду, не строится: `Promise.all` без `catch` (`W:components/popups/premium.tsx:125-128`; не проверено).
- Делать эти функции или не включать их вместе с Premium — решение владельца.

### 2.18 Вход и регистрация (известное)
- Экран кода говорит «SMS» (STATUS).
- Условий использования при регистрации нет: `auth.signIn` отдаёт `TermsOfService: nil` (`S:app/bff/authorization/internal/core/auth.signIn_handler.go:118-120`). Нужен ли текст, решает владелец.
- `help.getTermsOfServiceUpdate` отвечает правильно, но пишет в лог ложную ошибку «License key» (`S:app/bff/tos/internal/core/help.getTermsOfServiceUpdate_handler.go:30-37`).
- 2FA: почту указать нельзя (`EMAIL_INVALID`, `S:pkg/twofa/password.go:210-225`). Пароль восстанавливается только сбросом через 7 дней или администратором (STATUS).
- Рекомендованных папок нет намеренно (`S:app/bff/folders/internal/core/messages.getSuggestedDialogFilters_handler.go:29-33`).

### 2.19 Мелкие расхождения в ответах (живой Telegram)
Размер каждого — малый.
- `account.getNotifySettings`: Telegram отдаёт `ios_sound`, `android_sound`, `other_sound`, Pluma — нет.
- `account.getPrivacy`: Pluma кладёт в `users` пользователя, на которого правила не ссылаются; у Telegram там пусто.
- `contacts.getTopPeers` без собеседников: Telegram отдаёт `categories: []`, Pluma — пустую категорию.
- `help.getCountriesList`: у Pluma 245 стран, у Telegram 236.
- `help.getAppConfig`: у Pluma около 70 ключей, у Telegram около 230.
  - Часть выключена нарочно (задача 015): Premium, звёзды, подарки, истории, переводы.
  - Не связаны с Premium: `round_video_encoding`, `qr_login_camera`, `qr_login_code`, `reactions_uniq_max`, `reactions_in_chat_max`, `message_animated_emoji_max`, `poll_answers_max`, `poll_close_period_max`, `todo_*`, `ringtone_*`, `stickers_emoji_suggest_only_api`, `stickers_emoji_cache_time`, `hidden_members_group_size_min`, `chatlist_update_period` и др.
  - Ключи эмодзи и кубиков — в 3.3.

---

## 3. Нужны данные Telegram

Сами данные выгружаются методами Telegram с запасного аккаунта; владелец решил выкачать базу (STATUS). Кроме выгрузки, почти везде нужна и серверная функция: хранение установленных наборов, недавних и избранных, синхронизация. Она указана в столбце «Pluma».

| № | Данные | Метод Telegram для выгрузки | Объём у Telegram (живой) | Pluma: методы и статус | Где видно |
|---|---|---|---|---|---|
| 3.1 | Языковые пакеты | `langpack.getLanguages(lang_pack)`, `langpack.getLangPack(lang_pack, lang_code)`, затем `langpack.getDifference` | 29 языков; webk/ru — 5291 строка | `langpack.getLanguages`, `getLangPack`, `getDifference`, `getStrings` — fake; `getLanguage` — missing | Весь интерфейс (1.1) |
| 3.2 | Реакции | `messages.getAvailableReactions` (анимации — документы), `messages.getTopReactions`, `messages.getDefaultTagReactions`; реакция по умолчанию — `reactions_default` в `help.getConfig` (core.telegram.org/api/reactions) | 74 реакции; top — 20; теги — 16 | `getAvailableReactions` — fake (пусто). Missing: `sendReaction`, `getMessagesReactions`, `getMessageReactionsList`, `getUnreadReactions`, `readReactions`, `setDefaultReaction`, `setChatAvailableReactions`, `getTopReactions`, `getRecentReactions`, `getSavedReactionTags`, `updateSavedReactionTag`. Нужно и хранилище реакций с обновлениями: средний–крупный объём сверх данных | Android прячет полоску реакций и двойной тап при пустом списке (`A:ui/ChatActivity.java:30820-30856`; `A:messenger/MediaDataController.java:8497-8509`). Быстрая реакция в настройках пуста. Реакции группы: список пуст, сохранение — ошибка. Нет значка непрочитанных реакций и тегов «Избранного» |
| 3.3 | Анимированные эмодзи, эффекты по тапу, кубики, заглушки пустых экранов | `messages.getStickerSet` для `inputStickerSetAnimatedEmoji`, `AnimatedEmojiAnimations`, `EmojiGenericAnimations`, `Dice` (по каждому эмодзи), `shortName tg_placeholders_android`, `PremiumGifts`, `EmojiDefaultStatuses`; ключи appConfig `emojies_send_dice`, `emojies_send_dice_success`, `emojies_animated_zoom`, `emojies_sounds` | AnimatedEmoji — 599 документов; EmojiDefaultStatuses — 11; PremiumGifts — 3; кубиков 7 | `getStickerSet` — missing, 259 ошибок за 72 ч; `emojies_send_dice = []` (`app_config.json:70`); ⚽ и 🎰 сервер разыгрывает как 1–6 вместо 1–5 и 1–64 (`core_messages_helper.go:224-246`) | Одиночный эмодзи крупный, но статичный (`A:messenger/MessageObject.java:1977-2029`); тап ничего не проигрывает; 🎲 уходит текстом; на пустых экранах Android нет анимаций (`A:messenger/MediaDataController.java:208`). iOS повторяет запрос каждые ~15 с |
| 3.4 | Стикеры | `messages.getStickerSet` (по short name или id), `messages.getFeaturedStickers`, `messages.getOldFeaturedStickers`; наборы — документами | Популярных — 231 | Fake: `getAllStickers`, `getRecentStickers`, `getFavedStickers`, `getArchivedStickers`, `getMaskStickers`, `getFeaturedStickers`, `getStickers`. Missing: `getStickerSet`, `installStickerSet`, `uninstallStickerSet`, `toggleStickerSets`, `reorderStickerSets`, `saveRecentSticker`, `faveSticker`, `clearRecentStickers`, `readFeaturedStickers`, `searchStickerSets`, `searchStickers`, `getAttachedStickers`, `stickers.*` (11). Таблиц стикеров на сервере нет | Панель стикеров пуста, ссылка `t.me/addstickers` — ошибка, «Недавние» и «Избранные» не хранятся, подсказок стикеров по эмодзи нет, стикеров в фоторедакторе нет |
| 3.5 | Подсказки эмодзи по словам | `messages.getEmojiKeywords(lang_code)`, `getEmojiKeywordsDifference`, `getEmojiKeywordsLanguages` | en — 4286 | Все три — fake (`fake_rpc_result.go:179-199`) | При наборе слова эмодзи не предлагаются, поиск в панели эмодзи пуст |
| 3.6 | Категории эмодзи | `messages.getEmojiGroups`, `getEmojiStatusGroups`, `getEmojiProfilePhotoGroups`, `getEmojiStickerGroups` | Не замерялось | Missing | Нет строки категорий в поиске эмодзи и стикеров (iOS, Web K) |
| 3.7 | Эффекты сообщений | `messages.getAvailableEffects` | 697 эффектов, 894 документа | Missing; поле `effect` при отправке передаётся как есть | Эффектов в меню отправки нет (`A:messenger/MessagesController.java:24276-24290`) |
| 3.8 | Кастомные эмодзи (Premium) | `messages.getFeaturedEmojiStickers`, наборы через `getStickerSet`, документы — `messages.getCustomEmojiDocuments` | Популярных — 99 | `getEmojiStickers`, `getFeaturedEmojiStickers` — fake; `getCustomEmojiDocuments`, `searchCustomEmoji`, `searchEmojiStickerSets` — missing | Вкладки наборов эмодзи пусты; кастомный эмодзи не вставить и не отрисовать |
| 3.9 | Эмодзи-статусы (Premium) | `account.getDefaultEmojiStatuses`, `getChannelDefaultEmojiStatuses`, `getCollectibleEmojiStatuses` | 186 | Все — missing, `account.updateEmojiStatus` тоже | Выбор статуса пуст, установка — ошибка |
| 3.10 | Конструктор аватара из эмодзи, значок фона профиля | `account.getDefaultProfilePhotoEmojis`, `getDefaultGroupPhotoEmojis`, `getDefaultBackgroundEmojis` | 206 / 208 / 30 | Missing; первый и третий есть в логах (Android зовёт при каждом старте, `A:messenger/MediaDataController.java:208-212`) | Сетка эмодзи в «Установить фото → эмодзи» пуста |
| 3.11 | Цвета имени и профиля | `help.getPeerColors`, `help.getPeerProfileColors` | 21 / 16 | Missing, оба в логах; `account.updateColor` — missing. Android после ошибки флаг загрузки не сбрасывает и не повторяет запрос (`A:messenger/MessagesController.java:24058-24095`) | Видны только 7 встроенных цветов имени, цветов профиля нет; с Premium сохранение — ошибка |
| 3.12 | Темы | `account.getThemes(format)` (Android — `"android"`, Web K — `"macos"`), `account.getChatThemes` | 8 / 8 | Оба — fake (пусто); `installTheme`, `saveTheme`, `getTheme`, `uploadTheme`, `createTheme`, `updateTheme`, `messages.setChatTheme`, `account.getUniqueGiftChatThemes` — missing | **Android, «Настройки чатов»:** карточки «Color theme» — вечная серая заглушка загрузки: `FlickerLoadingView` служит пустым видом списка (`A:ui/DefaultThemesPreviewCell.java:142-157, 297`; `A:ui/ActionBar/Theme.java:7520-7563, 7660`). Тема чата — только «Без темы», выбор темы не синхронизируется |
| 3.13 | Обои | `account.getWallPapers`, узоры по slug — `account.getMultiWallPapers` и `getWallPaper` | 89 | `getWallPapers` — fake; `getMultiWallPapers`, `getWallPaper`, `saveWallPaper`, `installWallPaper`, `uploadWallPaper`, `resetWallPapers`, `messages.setChatWallPaper` — missing | В галерее только цвета и своё фото; узоры у акцентов встроенных тем Android, вероятно, не загрузятся |
| 3.14 | Промо Premium | `help.getPremiumPromo` (видео и описания) | Есть | Blocked, в логах | Экран Premium без видео (у пользователей с Premium и в окнах лимитов) |
| 3.15 | Приветственный и Premium-стикеры | `messages.getStickers("👋⭐")`, `messages.getStickers("📂⭐")` | Есть | Fake | В новом пустом чате нет стикера-приветствия, раздела Premium-стикеров нет |
| 3.16 | Подарки | `payments.getStarGifts` | 151 | Missing, в логах; скрыто конфигом | Пользователю не видно |
| 3.17 | Часовые пояса | `help.getTimezonesList` | 419 | Missing | Часы работы Business (только Premium) |

**Общие замечания по данным**
- **Файлы лежат в DC Telegram.** Документы (стикеры, анимации, иконки) выгружают через `upload.getFile` и кладут в dfs Pluma.
- **Id документов связаны между собой.** На них ссылаются другие данные: кастомные эмодзи в тексте (`messageEntityCustomEmoji`), эмодзи-статусы, значки фона в цветах, реакции, эффекты. Сохранять id Telegram или переписывать все ссылки — решение для ADR. У Pluma id генерирует snowflake; пересечения с id Telegram не проверялись.
- **Порядок по заметности:** языки (3.1) → реакции (3.2) → анимированные эмодзи (3.3) и стикеры (3.4) → подсказки эмодзи (3.5) → темы и обои (3.12, 3.13). Всё остальное касается в основном пользователей с Premium.

**Не данные Telegram, а внешние сервисы** (решения владельца: аккаунты и деньги):
- поиск GIF (бот `gif`) и картинок (`bing`);
- поиск мест (`venue_search_username = foursquare`, `S:teamgramd/bin/config.json:48-50`; что видит пользователь, не проверено);
- перевод (`messages.translateText`, скрыт конфигом);
- распознавание речи (`messages.transcribeAudio`);
- статические карты (2.11).

---

## 4. Крупные функции следующих этапов

### 4.1 Каналы и супергруппы — крупный
- **Создание канала — ошибка.**
  - Android: «Произошла ошибка» с текстом `ERR_ENTERPRISE_IS_BLOCKED` (`A:messenger/MessagesController.java:15034-15071`; `A:ui/Components/AlertsCreator.java:6909-6911`).
  - iOS: «Произошла ошибка» (`Login_UnknownError`, `I:submodules/TelegramUI/Sources/CreateChannelController.swift:490-493`).
- **Ошибкой кончается всё, для чего клиент переводит обычную группу в супергруппу.** `messages.migrateChat` — blocked (`S:app/bff/chats/internal/core/messages.migrateChat_handler.go:26-31`). Это:
  - «История видна новым участникам», «Публичная группа» (username), «Темы» (`A:ui/ChatEditActivity.java:2090-2091`; `A:ui/ChatEditTypeActivity.java:748, 1171, 1208`);
  - администратор с правами не по умолчанию или со званием, ограничение отдельного участника, передача владения (`A:ui/ChatRightsEditActivity.java:1106-1107, 1517-1518`);
  - медленный режим (`A:ui/ChatUsersActivity.java:2280-2282`);
  - создание сообщества (`A:ui/community/CommunityCreateActivity.java:179, 203`).
- **Методы:** `channels.*` — ok 1, blocked 15, missing 58 из 74; `updates.getChannelDifference` — blocked.
- **Что от этого зависит:**
  - форумы (`messages.getForumTopics` и др.), журнал действий, обсуждения, подписи;
  - статистика (`stats.*`, 12 missing) и бусты (`premium.*`);
  - сообщества Android 12.9 (`communities.*`, 9 missing; `getJoinedCommunities` есть в логах);
  - общие папки (`chatlists.*`, 11 missing; «Поделиться папкой» — ошибка, `A:ui/FilterCreateActivity.java:689-697`);
  - вкладки поиска «Каналы» и «Посты», личный канал в профиле.

### 4.2 Звонки — крупный
- **Состав:**
  - конфигурация DH (общая с секретными чатами);
  - `phone.*`: сигнализация, relay или SFU, TURN/STUN;
  - VoIP push (тип 9 уже хранится);
  - список звонков;
  - групповые звонки.
- ADR 0004 в статусе proposed. Как это выглядит сейчас — 1.6.

### 4.3 Секретные чаты — крупный
- **Методы:** `messages.getDhConfig`, `requestEncryption`, `acceptEncryption`, `discardEncryption`, `sendEncrypted*`, `readEncryptedHistory`, `setEncryptedTyping`; кроме того, `qts` в обновлениях и разнице, push. В матрице `requestEncryption`, `acceptEncryption`, `sendEncrypted` — missing.
- Как это выглядит сейчас — 2.16. Ни одна область этот экран не разбирала; Android проверен при сводке.

### 4.4 Боты и мини-приложения — крупный
- **Состояние сервера:**
  - бот не может войти: `auth.importBotAuthorization` — blocked (`S:app/bff/authorization/internal/core/auth.importBotAuthorization_handler.go:27`);
  - `bots.*` — 39 missing;
  - `messages.startBot`, `getBotCallbackAnswer`, `getInlineBotResults`, `sendInlineBotResult`, `requestWebView` и др. — missing;
  - сервера Bot API нет.
- **В логах:** `messages.getAttachMenuBots` — Android зовёт его при старте (`A:messenger/MediaDataController.java:565`).
- **Что откроет:** поиск GIF и картинок, меню вложений, вкладку «Приложения».

### 4.5 Истории — крупный
- **Методы:** `stories.*` — 44 missing, служба `RPCStories` не направлена; `users.getStoriesMaxIDs` и `contacts.toggleStoriesHidden` — тоже missing.
- **Что видно:** публикация скрыта конфигом (`stories_posting = disabled`, `app_config.json:64`). Android при каждом открытии списка чатов запрашивает `stories.getAllStories` (`A:ui/DialogsActivity.java:2956`; `A:ui/Stories/StoriesController.java:386`).

### 4.6 Платежи, Stars, подарки, покупка Premium — крупный
- **Состояние:** скрыто конфигом (`premium_purchase_blocked`, `stars_purchase_blocked`, `stargifts_blocked`, `enable_gifts_in_profile=false`: `app_config.json:2, 6-9`); `payments.*` — 0 из 70.
- **В логах фоновые ошибки:**
  - `payments.getStarsStatus` — Android зовёт его при каждом открытии настроек (`A:ui/SettingsActivity.java:711-712`);
  - `getSavedStarGifts` — ×9;
  - `getStarGifts`.
- **Видимое:** «Очистить платёжные данные» в настройках приватности отвечает ошибкой (`payments.clearSavedInfo`).

### 4.7 Прочее
- **Telegram Business** (только Premium) — средний–крупный (2.17).
- **Перевод и расшифровка речи** — внешние движки.
- **Импорт истории из WhatsApp и др.** — средний: `messages.checkHistoryImport`, `initHistoryImport`, `uploadImportedMedia`, `startHistoryImport` — missing. Android при ошибке предлагает отправить архив обычным файлом (`A:ui/LaunchActivity.java:3730-3803`).
- **Экспорт данных в Desktop:** `account.initTakeoutSession` — missing; поведение tdesktop не проверено.
- **Instant View** — не разбирался.

**Намеренно, не пробел:**
- реклама: `getSponsoredMessages` и `getSponsoredPeers` пусты;
- промо-чат: `getPromoData` без спонсорского прокси;
- «Люди рядом» — этой функции нет в самих клиентах;
- платная SMS при входе, ключи доступа, покупка Premium и звёзд скрыты намеренно;
- Desktop не регистрирует push, как и Telegram Desktop.

---

## 5. Расхождения с документами проекта

- **ADR 0017** (`S:docs/adr/0017-push-notifications.md:9`) утверждает, что Android тип 7 сам не регистрирует. Код говорит обратное (`A-jni:tgnet/ConnectionsManager.cpp:2398-2419, 1165-1170`). Нужен новый ADR, заменяющий этот пункт (1.10).
- **ADR 0022, п. 4** («Новый вход» не пишется в журнал и не становится сообщением) расходится с живым Telegram, где эти сообщения хранятся в истории 777000 (2.6). Нужен новый ADR.
- **Бриф 017** (`S:docs/tasks/017-rebranding.md:150`): карточки «Color theme» ждут `account.getThemes`, а не `account.getChatThemes` (3.12).
- **STATUS:** «Web push не отправляется: нет TLS-раздачи Web K» устарело, TLS уже есть (1.10).
- **api-coverage:** статус `ok` не значит «работает». Без эффекта или с потерей части запроса работают:
  - `contacts.getBirthdays` (заглушка внутри biz);
  - `messages.searchGlobal` (фильтры отбрасываются);
  - `messages.forwardMessages` (флаги игнорируются);
  - `account.setAuthorizationTTL`, `account.setAccountTTL`, `messages.setDefaultHistoryTTL` (значения хранятся без действия);
  - `photos.uploadProfilePhoto` (флаг `fallback`).
- **Генератор матрицы:** fake-ответы он показывает как `missing`, а три мёртвые заглушки (`getWebPage`, `getWebPageView`, `report`) не замечает. Стоит научить его показывать `fake` и ловить имена, которых нет в схеме (малый).

---

## 6. Что не сопоставлено и не проверено

### Экраны, которые ни одна область не разобрала
- **Секретные чаты** — при сводке проверен только Android (2.16, 4.3). iOS не смотрели, у Web K секретных чатов, насколько известно, нет (не проверено).
- **Трансляция геопозиции.** Отправка, по коду, работает (`core_messages_helper.go:212-218`), но `heading` и `proximity_notification_radius` отбрасываются. Обновления, остановка и уведомление о приближении не проверены.
- **Поиск мест** в выборе геопозиции (бот `foursquare`) — не проверен.
- **Экспорт данных и истории чата** (Desktop, `account.initTakeoutSession` — missing) — не проверен.
- **Telegram Passport** (`account.getAllSecureValues` и др.) — не проверен; обычно скрыт.
- **«Общие группы»** в профиле (`messages.getCommonChats` — ok) — обработчик не проверялся.
- **Счётчик на значке приложения** в push (iOS badge, Android) — не проверен.
- **Локальные экраны Android** («Энергосбережение», «Использование памяти», прокси, несколько аккаунтов) на обращения к серверу не проверялись.
- **iOS целиком** — только по коду: пользователи iOS-сборку не запускали.

### tdesktop
Исходники tdesktop не входили в разобранный набор.
- **Что читалось:**
  - `calls/calls_instance.cpp` (v7.0.9) — звонок закрывается после ошибки `getDhConfig`;
  - поиск кода по основной ветке GitHub (не по тегу v7.0.9) — `account.registerDevice` tdesktop не зовёт.
- **Чего не видно в логах:** по словам владельца, Desktop он не запускал, так что его трафика в логах нет.
- **Не проверено для Desktop:**
  - языковые пакеты (свой пакет tdesktop — по памяти, не проверено);
  - панели стикеров и эмодзи, анимированные эмодзи, реакции;
  - облачные темы и обои;
  - архив;
  - вкладка «Медиа»: серверная ошибка SQL общая, но какой фильтр шлёт tdesktop, не проверено;
  - отложенные сообщения, опросы, превью ссылок, поиск, общие папки;
  - экспорт данных (`account.initTakeoutSession`);
  - загрузка из Telegram наборов эмодзи и словарей проверки орфографии (по памяти, не проверено);
  - как tdesktop показывает ответы 400 вообще;
  - запись «config with empty dc_options» в его логе (STATUS) — последствия не изучены.

### Открытые вопросы по логам
- **Историй нет в выборке.** Android при каждом открытии списка чатов зовёт `stories.getAllStories` (`A:ui/DialogsActivity.java:2956` → `A:ui/Stories/StoriesController.java:386`). Служба `RPCStories` в `S:teamgramd/etc2/session.yaml` не направлена, значит, вызов должен попасть в лог как blocked. В выборке за 72 ч его нет.
  - Мало и `messages.getMessagesReactions`, хотя Android опрашивает видимые сообщения не чаще раза в 15 с (`A:messenger/ChatMessagesMetadataController.java:42-46`).
  - Вероятно, выборка неполная. Стоит пересчитать все строки «License key» по имени типа.
- **`messages.readHistory` → `PEER_ID_INVALID` ×4.** Возможны два пути:
  - bff отвергает peer, отличный от user и chat (`S:app/bff/messages/internal/core/messages.readHistory_handler.go:34-37`);
  - msg отвечает так, когда у читателя нет строки диалога (`S:app/messenger/msg/internal/dao/read.go:59-69`).
  - Какой peer пришёл, нужно смотреть в логе. Что Telegram отвечает в таком случае, не проверено.
- **`TOKEN_TYPE_INVALID`: 10 или 13.** В двух выборках разное число, но тип в обеих — 7. Предположение «tdesktop или ошибки до выкатки задачи 009» снято: tdesktop этот метод не зовёт, а тип 7 шлёт tgnet Android.
- **Ошибка SQL вкладки «Медиа».** biz пишет её в лог («queryx in SelectByMediaType(_)»). Есть ли она в логах, не проверялось; если есть, это подтвердит 1.4.

---

## 7. Решения владельца

1. **Звонки до их реализации:** оставить кнопку, которая не работает, или не объявлять звонки доступными (`phonecalls_enabled`, `phone_calls_available`).
2. **Домен ссылок:** pluma.chat вместо t.me в приглашениях и ссылках на профили. Это сервер, патчи четырёх клиентов и страница на pluma.chat.
3. **Внешние провайдеры:** карты, GIF, поиск мест, перевод, распознавание речи.
4. **Куда идут жалобы** на сообщения и профили.
5. **Тексты Pluma:** FAQ, политика, «Советы», Условия использования при регистрации.
6. **Premium от администратора:** делать Business, расшифровку и чек-листы или не включать их вместе с Premium.
7. **Баннер дней рождения на Android и Web K:** он скрыт при `premium_purchase_blocked`. Оставить как есть (как у Telegram, когда покупка Premium недоступна) или менять патчами.
8. **Импорт данных Telegram:** сохранять id документов Telegram или переписывать все ссылки на них (ADR).
9. **Порядок крупных этапов:** каналы и супергруппы, звонки, боты, секретные чаты.

### Ответы владельца (2026-10-30)
1. **Звонки.** Кнопку оставить как сейчас, пока звонки не сделаны.
2. **Домен ссылок.** Да: pluma.chat вместо t.me в приглашениях и ссылках на профили (задача 026).
3. **Внешние сервисы.** Да: карты, поиск GIF, поиск мест, перевод, распознавание речи (задача 027).
4. **Жалобы.** Как у Telegram: модерация через внутреннюю панель администратора — очередь жалоб, решения, ограничения. Её устройство у Telegram не опубликовано, поэтому проектируем по тому, что видно снаружи: причины жалоб в API, @SpamBot, сообщения 777000 об ограничениях (задача 025).
5. **Тексты.** FAQ — как у Telegram, с заменой Telegram на Pluma (задача 028). Политику конфиденциальности, «Советы» и условия при регистрации не делать вообще; пункты, которые на них ведут, скрыть.
6. **Premium.** Всё как у Telegram, но выдаёт его вручную администратор через ту же панель (задача 025).
7. **Дни рождения.** Не скрывать: пользователь может добавить день рождения, и баннер показывается (задача 020).
8. **Импорт данных Telegram.** Документы и наборы перенумеровать: id выдаёт Pluma, ссылки на id Telegram не сохраняются (задача 024).
9. **Порядок крупных этапов** — на усмотрение разработки. Выбран порядок в `docs/project/ROADMAP.md`.

---

## Приложение. Наблюдения на живом Telegram (2026-10-30, запасной аккаунт владельца)

Эталон: web.telegram.org/k (свежий tweb), Telegram Android 13.1.0 (`org.telegram.messenger.web`), Telegram iOS 12.9.2 из исходников с конфигурацией Telegram, Telegram Desktop 7.3.3. Pluma: web.pluma.chat (tweb 346844bf), аккаунт владельца. Вызовы — только методы чтения через API-клиент Web K (`rootScope.managers.apiManager.invokeApi`), в отчёте только устройство ответов, без значений.

### Служебный чат 777000
- У Telegram это обычный чат с историей на сервере. Веб-сессия, открытая 2026-10-30 в 19:56, видит сообщения с 2024-05-16. Это коды входа, «New login», «Two-Step Verification settings changed» (2026-10-26), «Incomplete login attempt» (2026-10-26) и «Thank you for subscribing to Telegram Premium». У каждого обычный id сообщения (`data-mid`).
- У Pluma «Новый вход» — `updateServiceNotification`: клиент сам создаёт локальное сообщение. Новое устройство этой истории не видит, а в Pluma Web чата 777000 нет вовсе. Это расходится с решением задачи 019 (ADR 0022). Сообщения должны быть настоящими сообщениями от 777000 в истории пользователя.
- Формат «New login» у Telegram (en):
  - «Device: Telegram Android, 13.1.0, Google sdk_gphone64_arm64, Android, 15 (35)»;
  - «Device: Telegram iOS, 12.9.2 (1), iPhone Simulator, iOS 26.4.1»;
  - «Location: Prague, Czechia»;
  - концовка «If this wasn't you, you can terminate that session in Settings > Devices (or Privacy & Security > Active Sessions).».
  
  Второй образец Android после «16 (36)» подтверждает правило «Android, <версия> (<SDK>)». У Pluma — «SDK 35».
- Сообщение с кодом входа у Telegram дополнительно говорит, что код отправлен и на почту, и «If you didn't request this code by trying to log in on another device, simply ignore this message.».

### Ответы методов: Telegram и Pluma
Telegram отвечает данными, Pluma — `ERR_ENTERPRISE_IS_BLOCKED`:
- `help.getPeerColors` — 21 вариант; `help.getPeerProfileColors` — 16;
- `help.getPremiumPromo`; `help.getTimezonesList` — 419;
- `account.getDefaultBackgroundEmojis` — 30, `getDefaultProfilePhotoEmojis` — 206, `getDefaultGroupPhotoEmojis` — 208, `getDefaultEmojiStatuses` — 186;
- пустые у Telegram: `account.getRecentEmojiStatuses`, `getSavedRingtones`, `getConnectedBots`, `getBusinessChatLinks`, `getCollectibleEmojiStatuses`;
- `account.getReactionsNotifySettings`;
- `messages.getTopReactions` — 20, `getRecentReactions` — пусто, `getDefaultTagReactions` — 16, `getSavedReactionTags` — notModified;
- `messages.getAvailableEffects` — 697 эффектов, 894 документа;
- `messages.getStickerSet`: AnimatedEmoji — 599 документов, EmojiDefaultStatuses — 11, PremiumGifts — 3;
- `messages.getAttachMenuBots` — 1 бот; `messages.getQuickReplies` — пусто;
- `messages.getWebPagePreview` — превью;
- `stories.getAllStories`, `stories.getPeerStories`;
- `payments.getStarsStatus`, `getStarGifts` — 151 подарок, `getSavedStarGifts`.

Pluma отвечает пустым «поддельным» ответом, Telegram — данными:
- `langpack.getLanguages` — 0 против 29; `langpack.getLangPack` webk ru — 0 строк против 5291;
- `account.getChatThemes` и `account.getThemes` — 0 против 8; `account.getWallPapers` — 0 против 89;
- `messages.getAvailableReactions` — 0 против 74 (без них реакции не работают);
- `messages.getFeaturedStickers` — 0 против 231; `getFeaturedEmojiStickers` — 0 против 99;
- `messages.getEmojiKeywords` en — 0 против 4286 (подсказки эмодзи);
- `help.getCdnConfig` — 0 против 3 ключей; `help.getConfig.dc_options` — 0 против 23 (известно, STATUS).

Мелкие расхождения формы:
- `help.getPromoData`: у Pluma `promoDataEmpty`, у Telegram `promoData` с `pending_suggestions` (например, подсказка добавить день рождения);
- `account.getGlobalPrivacySettings`: у Telegram по умолчанию `keep_archived_unmuted` и `keep_archived_folders`, у Pluma флагов нет;
- `account.getNotifySettings`: Telegram отдаёт `ios_sound`, `android_sound`, `other_sound`, Pluma — нет;
- `account.getPrivacy`: Pluma кладёт в `users` пользователя, на которого правила не ссылаются, Telegram — пусто;
- `contacts.getTopPeers` без пиров: Telegram — `categories: []`, Pluma — пустая категория;
- `help.getCountriesList`: 245 стран у Pluma, 236 у Telegram.

### help.getAppConfig
- У Pluma ключей около 70, у Telegram около 230.
- Нарочно выключено у Pluma (задача 015): Premium, звёзды, подарки, истории, переводы, rich-сообщения, ключи доступа.
- Нет у Pluma, хотя к Premium это не относится:
  - кубики (`emojies_send_dice`: у Pluma `[]`, у Telegram 7 эмодзи) и `emojies_send_dice_success`;
  - `emojies_animated_zoom`, `emojies_sounds`;
  - `gif_search_branding` и `gif_search_emojies`;
  - `qr_login_camera`, `qr_login_code`;
  - `reactions_uniq_max`, `reactions_in_chat_max`, `message_animated_emoji_max`;
  - рингтоны: `ringtone_*`;
  - `round_video_encoding`;
  - `poll_answers_max`, `poll_close_period_max`, `todo_*`;
  - `groupcall_video_participants_max`, `conference_call_size_limit`;
  - `stickers_emoji_suggest_only_api`, `stickers_emoji_cache_time`;
  - `default_emoji_statuses_stickerset_id`;
  - `hidden_members_group_size_min`, `forum_upgrade_participants_min`;
  - `story_*`;
  - `chatlist_update_period`;
  - `channel_*_level_min`, `group_*_level_min`;
  - `quick_replies_limit` и другие.

### Логи сервера (72 ч трафика клиентов Pluma)
- Заглушки, до которых доходили клиенты:
  - `messages.getStickerSet` — 259 вызовов;
  - `payments.getSavedStarGifts` — 9;
  - по 1–3: `account.getDefaultBackgroundEmojis`, `payments.getStarGifts`, `payments.getStarsStatus`, `messages.getWebPage`, `account.installTheme`, `messages.getAttachMenuBots`, `account.getDefaultProfilePhotoEmojis`, `communities.getJoinedCommunities`, `messages.getQuickReplies`, `messages.getSavedReactionTags`, `messages.getMessagesReactions`, `account.installWallPaper`, `messages.getRecentReactions`, `messages.getTopReactions`, `help.getPremiumPromo`, `account.getSavedRingtones`, `help.getPeerColors`, `help.getPeerProfileColors`, `account.getWebBrowserSettings`.
- Ошибки:
  - `account.registerDevice` → `TOKEN_TYPE_INVALID`, 10 раз: какой-то клиент не может зарегистрировать пуши;
  - `messages.readHistory` → `PEER_ID_INVALID`, 4 раза.

### Вход
- Web K у Telegram предлагает «Log in by passkey»: это более свежий tweb и `passkeys_*` в appConfig. У Pluma этого нет.
- Telegram предлагает продолжить на языке браузера («Продолжить на русском»), Pluma — «Continue in English»: сервер не отдаёт языки.
- Telegram Android при входе может показать «One-time SMS Fee»: платная SMS или неделя Premium. Pluma этого не делает и не должна: SMS у Pluma нет.
