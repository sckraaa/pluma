# 0064. Перевод сообщений: своя модель HY-MT1.5-1.8B в сервисе `translate` (llama.cpp) за протоколом OpenAI, разметка — тегами, кэш по содержимому

- Статус: предложено (2026-11-15)
- Задача: 101 (ветка `task/101-message-translation`). Обслуживаются `messages.translateText`, `messages.togglePeerTranslations`, перевод через `messages.composeMessageWithAI` (только форма «перевести», её шлёт iOS); `translations_disabled` в `userFull`, `chatFull`, `channelFull`; ключи `translations_manual_enabled` / `translations_auto_enabled`.
- Связано: ADR 0041 (Premium всем), 0050 (автоперевод каналов: флаг обслуживается, перевод — сюда), 0056 (сервис распознавания: тот же приём «движок за протоколом, модель меняется образом»), 0057 и 0061 (открытое содержимое не покидает дом), 0026 (сообщения 777000), 0046 (свои строки на 37 языках); задачи 027 (внешние сервисы), 079 (ИИ-редактор убран из клиентов), 098 (пакет шума: `summarizeText`).
- Решение владельца (2026-11-15): модель — HY-MT1.5-1.8B (Tencent); всё своё, дома; Premium открыт всем; тексты пользователю — только ключами локализации.

## Контекст

### Схема (слой 233, `~/pluma-work/reference-src-new/tdesktop/Telegram/SourceFiles/mtproto/scheme/api.tl`)
- `messages.translateText#a5eec345 flags peer:flags.0?InputPeer id:flags.0?Vector<int> text:flags.1?Vector<TextWithEntities> to_lang:string tone:flags.2?string = messages.TranslatedText` (`:2770`), ответ `messages.translateResult#33db32f8 result:Vector<TextWithEntities>` (`:1695`). Поля `from_lang` в слое 233 нет; `msg_id`, `text` (строка) и `from_lang` в `third_party/teamgram-proto/mtproto/schema.tl.sync_service.proto:2396-2398` — слитые старые слои, клиенты 228/233 их не шлют.
- `messages.togglePeerTranslations#e47cb579 flags disabled:flags.0?true peer = Bool` (`:2799`); флаг `translations_disabled` — у `chatFull` (`:111`), `channelFull` (`:112`), `userFull` (`:276`).
- `messages.composeMessageWithAI#daecc589 flags proofread:flags.0 emojify:flags.3 text:TextWithEntities translate_to_lang:flags.1?string tone:flags.2?InputAiComposeTone` (`:2860`) → `messages.composedMessageWithAI#90d7adfa flags result_text diff_text:flags.0?` (`:2208`). В vendored proto метод входит в сервис `RPCAiComposeTone` вместе с семью `aicompose.*` (`schema.tl.sync_service.proto:6319-6331`); сервис не зарегистрирован и не маршрутизирован.
- `messages.translateRichMessage#1a542004` (`:2872`) переводит `message.rich_message` (`:126`, flags2.13; `richMessage` из `PageBlock`, `:2247`). `messages.summarizeText#abbbd346` (`:2854`) — пересказ сообщения, у которого сервер выставил `summary_from_language` (`:126`, flags2.11).

### Документация (прочитано 2026-11-15)
- core.telegram.org/method/messages.translateText: `to_lang` — двухбуквенный код ISO 639-1; `tone` — тон ИИ-редактора; оформление сохраняется только для Premium (у Pluma Premium у всех, ADR 0041); вызывать могут только пользователи. Ошибки: 400 `INPUT_TEXT_EMPTY`, `INPUT_TEXT_TOO_LONG`, `MSG_ID_INVALID`, `PEER_ID_INVALID`, `TO_LANG_INVALID`, `TRANSLATE_REQ_QUOTA_EXCEEDED`; 500 `TRANSLATE_REQ_FAILED`, `TRANSLATION_TIMEOUT`; 406 `TRANSLATIONS_DISABLED` (с пояснением через `updateServiceNotification`).
- Прочее о переводе (порог показа панели, автоперевод каналов) — ADR 0050, «Автоперевод: документация и клиенты».

### Клиенты (`~/pluma-work/reference-src-new`: Android 7be672ea = 13.2.1, tdesktop v7.4.1 157f5cfc, iOS 24638bc69b, Web K aa74832e)
- **Что включает перевод.**
  - Android: ручной перевод — `translations_manual_enabled != "disabled"`, перевод чата — `translations_auto_enabled != "disabled"` и Premium (или `channel.autotranslation`) (`messenger/TranslateController.java:93-121`, `MessagesController.java:765-768`); без ключа — `"enabled"` (`MessagesController.java:1741-1742`). `alternative` и `system` шлют текст с устройства в Google (`TranslateController.java:1106-1124`, `ui/Components/TranslateAlert2.java:323-326`).
  - iOS: без ключа — `disabled` (`AccountContext/Sources/AccountContext.swift:2252-2259`); кнопка «Перевести» — при `enabled`/`alternative`/`system` (`TranslateUI/Sources/Translate.swift:192-208`), перевод чата — только `enabled` или `system` на iOS 18 (`Translate.swift:175-190`); панель перевода — только Premium (`TranslateHeaderPanelComponent/Sources/ChatTranslationPanelNode.swift:214-223`).
  - tdesktop ключи не читает: переводит через сервер, если не выбран системный переводчик или URL экспериментальной опции (`lang/translate_provider.cpp:34-45`); следит за языком чата при `Premium || autotranslation` (`history/view/history_view_translate_tracker.cpp:58-71`).
  - Web K: только `enabled` (`hooks/usePeerTranslation.ts:27`, `helpers/detectLanguageForTranslation.ts:12`).
- **Как просят.**
  - Пачки: Android — до 20 сообщений и 25 000 знаков (`TranslateController.java:66-67`), tdesktop — до 20 и 24 КБ (`history_view_translate_tracker.cpp:35-36`), Web K — до 20 (`appTranslationsManager.ts:16, 224`); iOS — видимые сообщения пачкой `peer`+`id` (`TelegramCore/.../Messages/Translate.swift:385`).
  - Сообщения — `peer`+`id`; расшифровки голосовых, опросы и чужие тексты — `text` (Android `TranslateController.java:1145-1155, 1393-1410`; iOS `Translate.swift:296-322`; tdesktop `lang/translate_mtproto_provider.cpp:89-141` — текстом, если пачка из разных чатов).
  - Ответ сопоставляется **по порядку**: Android `TranslateController.java:1162-1167`, iOS `Translate.swift:406-412`, tdesktop `translate_mtproto_provider.cpp:67-82` (короткий список — ошибка для недостающих). iOS кладёт в `id` и опросы (без текста), поэтому пустой элемент ответа обязателен для выравнивания.
  - `to_lang`: Android нормализует в `xx` или `xx-YY` (`TranslateController.java:2123-2133`), `nb` → `no` (`TranslateAlert2.java:336-338`); tdesktop — `twoLetterCode()` (`translate_mtproto_provider.cpp:107`).
  - `tone` не шлёт ни один клиент: Android закомментировано (`TranslateAlert2.java:417`), tdesktop — пустая строка (`translate_mtproto_provider.cpp:108`), iOS — только не `neutral`, а не-нейтральных вызовов в интерфейсе нет (поиск `.casual`/`.formal` вне `Translate.swift` пуст).
- **Ручной перевод iOS идёт не через `translateText`.** При `translations_manual_enabled = enabled` пункт «Перевести» открывает `TextProcessingScreen` в режиме `.translate` (`TelegramUI/Sources/ChatController.swift:4590-4624`; `presentTranslateScreen` вне `system` ничего не показывает, `TranslateUI/Sources/TranslateScreen.swift:28-41`), а он зовёт `messages.composeMessageWithAI` с `translate_to_lang` (`TextProcessingTranslateContentComponent.swift:240-242, 282`; `Translate.swift:856-919`). На экране есть переключатель «эмодзи» (flags.3, `TextProcessingTranslateContentComponent.swift:715-765`) и выбор стиля (flags.2) из `aicompose.getTones` (`TextProcessingScreen.swift:2305`); стилей у Pluma нет. Вкладки «стиль/исправить» — только в режиме `.edit` (ИИ-редактор, убран задачей 079; `TextProcessingScreen.swift:701-790`).
- **Ошибки.**
  - Android: `TRANSLATIONS_DISABLED_ALT` → перевод с устройства через Google (`TranslateController.java:1168-1183`, `TranslateAlert2.java:420`); `TO_LANG_INVALID` и `QUOTA_EXCEEDED` (ровно так) → всплывающая ошибка; прочие → перевод не показан (`:1184-1196`).
  - iOS: `FLOOD_WAIT*` → `.limitExceeded`, `MSG_ID_INVALID`, `INPUT_TEXT_EMPTY`, `INPUT_TEXT_TOO_LONG`, `TO_LANG_INVALID`, `TRANSLATIONS_DISABLED_ALT` → свои случаи, прочее — `.generic` (`Translate.swift:48-64, 385-400`); у `composeMessageWithAI` — только `AICOMPOSE_FLOOD_PREMIUM`, прочее `.generic` (`Translate.swift:919-926`).
  - **Повторы транспорта.** 500 и `FLOOD_WAIT_X` клиент повторяет сам: Android tgnet — для запросов без `RequestFlagFailOnServerErrors` (так шлёт `TranslateController`) 500 повторяется с нарастающей задержкой без конца, 420 — через X секунд (`jni/tgnet/ConnectionsManager.cpp:1372-1420`); tdesktop — 5xx с удвоением задержки, `FLOOD_WAIT_X` через X (`mtproto/mtp_instance.cpp:1583-1625`); Web K — `FLOOD_WAIT_X` при X ≤ 60 (`lib/appManagers/apiManager.ts:841-870`). iOS `FLOOD_WAIT` не повторяет.
- **Ответ ставится по порядку и с сущностями**; Android затем превращает `textUrl`, у которого адрес равен тексту, обратно в ссылку (`TranslateAlert2.java:853-883`).
- **Флаг чата.** Android скрывает панель по `translations_disabled` полного чата и шлёт `togglePeerTranslations` и при скрытии, и при возврате (`TranslateController.java:194-214, 349-361`); iOS, tdesktop (`data/data_peer.cpp:858-866`), Web K (`appTranslationsManager.ts:522-535`) — так же. Обновления на это нет: другие сессии видят флаг при следующем `getFull*`.
- **Rich-сообщения и пересказ.** `translateRichMessage` клиенты шлют только для сообщений с `rich_message` (Android `TranslateController.java:1506`, iOS `Translate.swift:331-333`, tdesktop `history_view_translate_tracker.cpp:354-406`), `summarizeText` — только при `summary_from_language` (Android `TranslateController.java:143`, Web K `components/chat/bubbles.ts:10327`). Pluma не хранит `rich_message` (`messages.saveDraft_handler.go:91-92`, `sendMessage` его не читает) и не выставляет `summary_from_language`, поэтому оба метода клиенты не вызывают.

### Модель и лицензия (прочитано 2026-11-15)
- huggingface.co/tencent/HY-MT1.5-1.8B: 33 языка и 5 вариантов (среди них `ru`, `uk`, `kk`, `en`, `de`, `fr`, `es`, `pl`, `tr`, `zh`, `zh-Hant`); шаблоны запросов: «XX⇔XX» по-английски, «ZH⇔XX» и «форматированный перевод» по-китайски, теги `<sn></sn>` вокруг текста с оформлением, ответ в `<target>…</target>`; системного запроса у модели нет; параметры выборки `top_k 20`, `top_p 0.6`, `repetition_penalty 1.05`, `temperature 0.7`.
- huggingface.co/tencent/HY-MT1.5-1.8B-GGUF: официальные `Q4_K_M` (1,13 ГБ), `Q6_K` (1,47 ГБ), `Q8_0` (1,91 ГБ), «для llama.cpp» (README github.com/Tencent-Hunyuan/HY-MT). Там же упомянута новая серия Hy-MT2 — не изучалась.
- Лицензия — Tencent HY Community License Agreement (`License.txt` репозитория и модели):
  - §1(l): «Территория» — весь мир, **кроме Европейского союза, Великобритании и Южной Кореи**; вступление: соглашение там не действует; §5(c) и п. 1 политики допустимого использования: использование вне Территории не лицензировано.
  - §3 (вводная): распространять или **делать доступной третьим лицам** — только в пределах Территории.
  - §3(e): кто строит на модели сервис для третьих лиц, обязан ясно и заметно сообщить всем конечным пользователям полное юридическое имя настоящего поставщика сервиса и прямо указать, что Tencent с сервисом не связана, не спонсирует и не одобряет его; имя и знаки Tencent нельзя использовать так, чтобы создать впечатление связи.
  - §3(a), (b), (d): копия соглашения получателям, отметки об изменённых файлах, файл NOTICE с фразой о лицензии (для распространения не через «Hosted Service»).
  - §4: свыше 100 млн активных пользователей в месяц — отдельная лицензия (Pluma — до 100 человек).
  - §5(a): политика допустимого использования (запрет незаконного, вредного, военного использования и т. п.); §5(b): выходы модели нельзя использовать для улучшения других моделей.
  - §9: право Гонконга.
- Запасной вариант без территориального исключения — TranslateGemma-4B: Gemma Terms of Use (ai.google.dev/gemma/terms, изменены 2026-04-01) стран не исключают, требуют уведомлять получателей об ограничениях §3.2; модель требует кода исходного языка (сводка исследования, не перепроверено).

### Хост
- Домашний сервер: i5-13500T, 32 ГБ, без GPU (ADR 0061); у Pluma `MemoryMax=10G`, занято около 3,6 ГБ (`docs/ops/home-server.md:58`); рядом медиасервер владельца (`media.slice` до 12 ГБ) и `stt` (2 CPU).
- Скорость на этом процессоре не измерялась. Оценка исследования (не замер): 1.8B Q8_0 — 25–35 токенов/с, 2–4 с на сообщение в 60 слов.

## Решение

### 1. Три слоя, как у распознавания (ADR 0056)
1. **bff** (`app/bff/translation`, `pkg/translate`) проверяет запросы, достаёт тексты сообщений с проверкой доступа, превращает сущности в разметку и обратно, ведёт кэш, очередь и пределы.
2. **Протокол** — `POST /v1/chat/completions` (OpenAI), одно сообщение роли `user` с готовым запросом, `stream: false`. Сервер знает URL, токен, имя модели и **профиль** модели.
3. **Движок** — контейнер `translate`: официальный `llama-server` (llama.cpp, MIT) и GGUF модели, запечённый в образ `pluma-translate` со сверкой SHA-256 (как `pluma-stt`). Замена модели — другой образ и другие `TRANSLATE_MODEL`/`TRANSLATE_PROFILE`; замена движка — любой сервис с тем же протоколом за `TRANSLATE_URL`.

**Профиль** (`pkg/translate/profile_*.go`) — то, что зависит от модели: таблица языков (код клиента → имя языка для запроса), шаблоны запроса (простой и с тегами), синтаксис тегов, разбор ответа, параметры выборки, предельная длина. Первый профиль — `hy-mt1.5`. Версия профиля входит в ключ кэша.

### 2. Модель и квантование
HY-MT1.5-1.8B, официальный GGUF. По умолчанию `Q8_0` (по данным Tencent квантование до INT4 заметно портит качество, FP8 почти без потерь; `Q6_K` — запасной, если замер покажет нехватку скорости). Окончательный выбор — по замеру на домашнем сервере (бриф 101, часть «Замер»), до включения функции.

### 3. Разметка: сущности в теги и обратно
- Сущности делятся на два вида:
  - **оформление** (переводится содержимое): `bold`, `italic`, `underline`, `strike`, `spoiler`, `textUrl` (адрес хранится у сервера), `blockquote` (с `collapsed`);
  - **неприкосновенные** (содержимое не переводится): `code`, `pre`, `url`, `email`, `mention`, `mentionName` (и входное `inputMessageEntityMentionName`), `hashtag`, `cashtag`, `botCommand`, `phone`, `bankCard`, `customEmoji`, `formattedDate`, `tonAddress`, `unknown`.
- Пересекающиеся сущности режутся по границам в правильно вложенное дерево. Каждый узел получает тег с номером (`<s1>…</s1>`); неприкосновенный — тот же тег, содержимое которого сервер после ответа **восстанавливает из оригинала**, что бы модель ни вернула.
- Текст без сущностей идёт простым шаблоном, с сущностями — шаблоном с тегами.
- Проверка ответа: каждый тег ровно один раз, вложенность цела, нет лишних тегов, ответ внутри `<target>`. Если не так — **запасной путь**: текст режется по неприкосновенным кускам, переводимые куски переводятся простым шаблоном по отдельности, неприкосновенные вставляются как есть; оформление в этом случае теряется (счётчик `translate_markup_fallback_total`). Пустой перевод или ответ длиннее `MaxOutputFactor` × оригинала — ошибка элемента (повтор по разделу 6).
- Смещения и длины пересчитываются в UTF-16, как требует MTProto. Пробелы и переводы строк по краям тегов нормализуются так, чтобы сущность не начиналась и не кончалась пробелом.

### 4. Язык
- `to_lang` принимает `xx`, `xx-YY`, `xx_YY` в любом регистре; таблица профиля сводит его к языку модели (`zh-CN`, `zh-Hans` → `zh`; `zh-TW`, `zh-HK`, `zh-Hant` → `zh-Hant`; `pt-BR` → `pt`; `nb`/`no` модели нет). Нет в таблице — `TO_LANG_INVALID` (Android показывает понятную ошибку, iOS — `.invalidLanguage`).
- Исходный язык сервер не определяет: в слое 233 его нет, клиенты определяют сами и не просят перевода на тот же язык, а HY-MT код источника не требует. Детектор на сервере понадобится только профилю, которому нужен код источника (TranslateGemma), — это отдельное решение.

### 5. Кэш по содержимому
- Таблица `translation_cache`: ключ — SHA-256 от (профиль с версией, модель, язык назначения, исходный текст, нормализованные сущности); значение — перевод и его сущности. Перевод одного текста одинаков для всех, поэтому пост канала с автопереводом переводится один раз на язык для всех подписчиков, пересланное сообщение — тоже.
- Правка сообщения меняет текст, значит и ключ: старый перевод не отдаётся, отдельная инвалидация не нужна. Смена модели или профиля — тоже новые ключи.
- Срок хранения — `CacheTTL` (30 дней) от последнего обращения; подметальщик удаляет просроченное пачками. Удалённое сообщение не вычищается из кэша сразу — его перевод живёт до истечения срока (решение к подтверждению; адресная чистка потребовала бы ключа по сообщению и врезки во все пути удаления).
- Redis не используется: при ~100 пользователях хватает MySQL и одного экземпляра bff; одинаковые одновременные запросы склеиваются внутри процесса (singleflight по ключу).

### 6. Очередь, слоты, ожидание
- Очередь в памяти bff: элементы, которых нет в кэше. Порядок: ручной перевод (один текст, человек ждёт) раньше пачек чата; внутри приоритета — по кругу между пользователями, чтобы автоперевод одного не задерживал других.
- В движок одновременно уходит не больше `Slots` запросов (по числу слотов `llama-server --parallel`); один элемент — один запрос.
- Обработчик ждёт до `SyncWait` (25 с для пачки, 45 с для одного текста — этим живёт экран перевода iOS). Не успели все элементы — ответ `FLOOD_WAIT_X` (X — оценка оставшегося, от 3 до 30 с), а работа **продолжается**: готовые переводы ложатся в кэш, повтор клиента (Android, tdesktop, Web K повторяют сами, раздел «Клиенты») получает их сразу.
- Срок одного обращения к движку — `Timeout` = 10 с + 25 мс × входной знак (настраивается). Сбой или срок — до двух повторов с паузой; затем элемент считается неудачным на `FailedTTL` (60 с, в памяти), и запросы с ним отвечают `FLOOD_WAIT_X`.
- Движок недоступен (соединение, 5xx, 429) — `FLOOD_WAIT_10` без постановки в очередь новых элементов, пока проверка здоровья не пройдёт; ответ 500 сервер не отдаёт никогда (клиенты повторяли бы его без конца).
- Перезапуск bff: очередь теряется, кэш остаётся; клиенты повторяют запросы, и недостающее ставится снова. Долговременная очередь не нужна: перевод — ответ на открытый экран, а не обещанное обновление.

### 7. Пределы
- На элемент: текст не длиннее `MaxItemLength` = 4096 знаков UTF-16 (`message_length_max` и подпись Premium) — иначе `INPUT_TEXT_TOO_LONG`.
- На запрос: не больше 100 элементов и 32 768 знаков вместе (клиенты шлют до 20 и до 25 000) — иначе `INPUT_TEXT_TOO_LONG`.
- Квот на пользователя нет (как у расшифровки, ADR 0056). От злоупотребления — предел частоты на элементы, ушедшие в движок (кэш бесплатен): 300 в минуту и 3000 в час на пользователя (`logincode.Limiter`), сверх — `FLOOD_WAIT_X`. Значения — к подтверждению.

### 8. Методы
- **`messages.translateText`.**
  - Вызывает бот — `BOT_METHOD_INVALID`. Ни `peer`+`id`, ни `text` — `INPUT_TEXT_EMPTY`.
  - `peer`+`id`: сообщения читаются так же, как в расшифровке (`app/bff/transcription/internal/core/record.go:43-100`): копии пользователя в личных и группах (`message.getUserMessageList`), сообщения канала через `channel.getMessages` с проверкой доступа. Нет диалога или канал закрыт — `PEER_ID_INVALID`; пустой список — `MSG_ID_INVALID`. Сообщение не найдено, без текста (медиа без подписи, опрос, служебное) — пустой `textWithEntities` на его месте.
  - `text`: пустой элемент — пустой ответ на его месте.
  - Ответ — ровно по одному элементу на вход в том же порядке.
  - `tone` не учитывается: его не шлёт ни один клиент, а модель тона не умеет (решение к подтверждению).
  - Флаг `translations_disabled` перевод не запрещает — он только прячет панель.
- **`messages.composeMessageWithAI`** — только форма «перевести»: задан `translate_to_lang`, нет `proofread`, `emojify`, `tone`. Ответ `messages.composedMessageWithAI{result_text}` без `diff_text`. Любая другая форма и все `aicompose.*` — `ErrEnterpriseIsBlocked` (400): языковой модели общего назначения у Pluma нет.
- **`messages.togglePeerTranslations`** — запись (пользователь, чат) в `peer_translation_settings`, `boolTrue`; чат должен существовать для пользователя, иначе `PEER_ID_INVALID`. Обновлений нет; `users.getFullUser`, `messages.getFullChat`, `channels.getFullChannel` отдают флаг вызывающего.
- **`channels.toggleAutotranslation`** не меняется (ADR 0050); его реализация остаётся в области каналов, область перевода вызывает её через интерфейс, потому что gRPC-сервис `RPCTranslation` регистрируется один раз.
- **`messages.translateRichMessage`** — `ErrEnterpriseIsBlocked` с пояснением: rich-сообщений у Pluma нет; перевод блоков `PageBlock` той же моделью — вместе с задачей, которая начнёт их хранить. **`messages.summarizeText`** — `ErrEnterpriseIsBlocked` вместо пустого успеха; делает задача 098 (часть D), здесь не меняется.
- Перевод выключен (`TRANSLATE_URL` пуст) — `translateText` и форма перевода `composeMessageWithAI` отвечают `ErrEnterpriseIsBlocked`, как сегодня, и ключи конфигурации отдаются `disabled` (раздел 9).

### 9. Конфигурация клиентов
- В `teamgramd/bin/app_config.json` оба ключа — `"enabled"` (не `system` и не `alternative`: те отправляют текст с устройства в Google или Apple). `appconfig` получает переопределение при старте bff: без `TRANSLATE_URL` оба ключа отдаются `"disabled"` (хэш конфига пересчитывается). Так iOS, Android и Web K не показывают кнопки при выключенном движке; tdesktop ключи не читает и получает ошибку, как сегодня.
- Падение движка ключи не переключает: короткий сбой обслуживается ответом `FLOOD_WAIT_X`.

### 10. Раскрытие (§3(e) лицензии)
- **Сервер** отправляет пользователю одно сообщение от 777000 при первом успешном переводе: перевод выполняет Pluma на своём сервере моделью HY-MT1.5; поставщик сервиса — <полное юридическое имя, решение владельца>; Tencent не связана с Pluma, не спонсирует и не одобряет её. Текст — ключ `PlumaTranslationNotice` в серверных строках (`pkg/langpack/mail.json`, как `PlumaPasswordRecoveredMessage`) на 37 языках; без логотипов и знаков Tencent. Признак «уже отправлено» — таблица `translation_notices`.
- **Сайт** `pluma.chat`: строка в подвале или на странице о конфиденциальности с тем же смыслом и ссылкой на текст лицензии — ключами локализации сайта (`~/pluma-site`, вне репозитория сервера).
- **Образ** `pluma-translate` несёт `License.txt` модели и файл `NOTICE` с фразой §3(d), лицензии llama.cpp.
- Клиенты не патчатся ради раскрытия.

### 11. Патч клиента — только iOS, только переключатель «эмодзи»
На экране перевода iOS переключатель «эмодзи» всегда приводил бы к ошибке (это работа ИИ-редактора). Патч скрывает его в режиме `.translate` (`displayEmojify = false`, `TextProcessingTranslateContentComponent.swift:715-720`); выбор стиля пуст сам (`aicompose.getTones` — ошибка, стилей нет). Остальные клиенты без правок.

### 12. Сервис `translate`
- Оверлей `docker-compose.translate.yaml`: `mem_limit = memswap_limit = ${TRANSLATE_MEMORY:-4g}`, `cpus: ${TRANSLATE_CPUS:-6}`, `cpu_shares: 256` (ниже ядра Pluma: при нехватке процессора уступает сообщениям и звонкам), `cgroup_parent`, `read_only`, пользователь без прав, без опубликованных портов, `HEALTHCHECK` по `/health`, метрики `llama-server` (`--metrics`).
- Параметры `llama-server` из окружения: потоки `TRANSLATE_THREADS` (6), слоты `TRANSLATE_SLOTS` (4), контекст `TRANSLATE_CTX` (16384 — по 4096 токенов на слот), ключ `TRANSLATE_TOKEN` (≥ 32 знаков, тот же, что шлёт bff). Точные имена флагов сверяются с закреплённой версией llama.cpp при сборке образа.
- Модель внутри образа; старт не зависит от сети; исходящие соединения сервису не нужны.

### 13. Наблюдаемость и журнал
- Метрики bff: `translate_requests_total{method,result}`, `translate_items_total{source="cache|engine|empty"}`, `translate_engine_seconds`, `translate_engine_errors_total{kind}`, `translate_queue_depth`, `translate_markup_fallback_total`, `translate_cache_rows`. Тревога: движок недоступен 10 минут при включённом переводе.
- В журнал не пишется ни исходный текст, ни перевод, ни запрос к модели (`tools/logaudit`); только длины, языки, время, коды ошибок.

## Последствия
- Перевод работает во всех четырёх клиентах без патчей, кроме одного переключателя в iOS; открытый текст не покидает домашний сервер.
- Новый контейнер около 2,5–4 ГБ памяти (оценка, уточняется замером) и до 6 потоков процессора с низким приоритетом; образ около 2,2 ГБ (модель `Q8_0` 1,91 ГБ); `docs/ops/memory.md` пересчитывается в пределах `MemoryMax=10G`.
- Новые таблицы `translation_cache`, `peer_translation_settings`, `translation_notices`. В кэше лежат переводы текстов сообщений до 30 дней после последнего обращения.
- Лицензия HY-MT связывает: сервис нельзя предоставлять пользователям в ЕС, Великобритании и Южной Корее, а раскрытие §3(e) обязательно. Если среди пользователей есть живущие там, модель надо менять (раздел «Что не делается», решение владельца); код к этому готов — профиль и образ.
- Качество казахского у всех открытых моделей среднее (исследование); проверяется замером.

## Что не делается
- `alternative` и `system` в конфиге: текст уходил бы с устройства в Google или Apple.
- Внешние API перевода (Google, DeepL): открытый текст покинул бы дом (ADR 0057, 0061).
- ИИ-редактор, пересказ (`summarizeText`), стили (`aicompose.*`), тон (`tone`), `emojify`: нужна языковая модель общего назначения.
- `translateRichMessage`: нет rich-сообщений.
- Определение языка на сервере.
- Долговременная очередь заданий и обновления о готовом переводе (в протоколе их нет: перевод — только ответ на запрос).
- TranslateGemma и выбор модели по стране номера: делается, только если владелец решит, что пользователи из ЕС, Великобритании или Южной Кореи будут (профиль `translategemma` с детектором языка — отдельная задача).
- Переключение ключей конфигурации при падении движка.
