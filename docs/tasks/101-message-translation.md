# 101. Перевод сообщений своей моделью

- Статус: todo
- Решение: ADR 0064 (предложено).
- Зависит от: 082 / ADR 0050 (флаг автоперевода канала, `RPCTranslation` уже зарегистрирован на области каналов), 090 / ADR 0056 (образец сервиса-движка, оверлея, чтения сообщения по `peer`+`id`), 091 (образец серверных строк `pkg/langpack/mail.json` и сообщения 777000), 076 / ADR 0046 (37 языков). В этой партии: 098 (пакет шума, часть D) — владелец `messages.summarizeText` (отказ 400), эта задача его не трогает; `RPCAiComposeTone` и `aicompose.*` — только эта задача (098 их не регистрирует); 106 (платформа ботов) — ответ ботам; 104 и 105 правят те же обработчики полных профилей (см. «Порядок работ», общие файлы).
- Ветка: `task/101-message-translation`.

> **Решения владельца после написания брифа (2026-11-15), имеют приоритет над текстом ниже.** Пользователей в ЕС, Великобритании и Южной Корее нет — HY-MT1.5 подходит. Раскрытие по §3(e) (сообщение 777000 «Tencent не связана с Pluma», строка на сайте, ключ `PlumaTranslationNotice`, таблица `translation_notices`, `TRANSLATE_PROVIDER_NAME` и проверка его при старте, `TestTranslateNotice`, строка проверки владельца про это сообщение) **в эту партию не входит**: владелец сначала проверяет качество переводов, раскрытие ставится отдельным шагом выпуска после его решения (записать в STATUS как шаг до публичного включения). `License.txt` и `NOTICE` в образе остаются.

## Цель
Человек переводит сообщение, расшифровку или опрос и включает перевод всего чата во всех четырёх клиентах так же, как в Telegram с Premium; посты каналов с автопереводом переводятся всем. Переводит своя модель HY-MT1.5-1.8B в контейнере `translate` на домашнем сервере, открытый текст дом не покидает. Без `TRANSLATE_URL` всё как сегодня: кнопок перевода нет.

## Область
В задаче:
- `messages.translateText` (`peer`+`id` и `text`, сущности сохраняются), `messages.togglePeerTranslations` (сейчас `ErrEnterpriseIsBlocked` в `app/bff/channels/internal/core/not_served.go:33-46`).
- `messages.composeMessageWithAI` — только форма «перевести» (ручной перевод iOS); остальные формы и `aicompose.*` — `ErrEnterpriseIsBlocked` (400). Регистрация `RPCAiComposeTone` в bff и маршрут в трёх `session.yaml`.
- `translations_disabled` в `users.getFullUser`, `messages.getFullChat`, `channels.getFullChannel`.
- Ключи `translations_manual_enabled` / `translations_auto_enabled` = `enabled` и их переопределение в `disabled` без движка.
- Пакет `pkg/translate` (профиль модели, разметка, клиент движка, кэш, очередь, пределы, метрики), область `app/bff/translation`, миграция.
- Сервис `translate`: образ `pluma-translate` (llama.cpp `llama-server` + GGUF `Q8_0`), оверлей `docker-compose.translate.yaml`, сборка в CI.
- Раскрытие по §3(e) лицензии: сообщение 777000 при первом переводе (37 языков), строка на сайте, `License.txt` и `NOTICE` в образе.
- Патч iOS: скрыть переключатель «эмодзи» на экране перевода.
- Замер на домашнем сервере до включения; `docs/ops/translation.md`, метрики, тревоги, `logaudit`.
- `messages.translateRichMessage` — оставить 400, заменить апстримную заглушку с логом «License key» пояснением (rich-сообщений нет). `messages.summarizeText` — не здесь: 400 делает 098 (часть D).

Не в задаче (ADR 0064, «Что не делается»): ИИ-редактор, пересказ, стили и тон, `emojify`, перевод rich-сообщений, определение языка на сервере, внешние API, `alternative`/`system`, второй профиль модели (TranslateGemma) и выбор модели по стране номера.

## Контракт клиентов
Источники — `~/pluma-work/reference-src-new` (Android 7be672ea = 13.2.1, tdesktop v7.4.1, iOS 24638bc69b, Web K aa74832e), подробно с номерами строк — ADR 0064, «Контекст». Главное:
- Включение: Android и Web K — ключи конфига (Android без ключа считает `enabled`: `MessagesController.java:1741-1742`; Web K — только `enabled`: `hooks/usePeerTranslation.ts:27`); iOS без ключа — `disabled` (`AccountContext.swift:2252-2259`), перевод чата — только `enabled` (`TranslateUI/Sources/Translate.swift:175-190`); tdesktop ключи не читает (`lang/translate_provider.cpp:34-45`). Premium нужен панели чата (Android `TranslateController.java:93-106`, iOS `ChatTranslationPanelNode.swift:214-223`, tdesktop `history_view_translate_tracker.cpp:58-71`) — у Pluma он у всех (ADR 0041).
- Пачки до 20 элементов и около 25 000 знаков (Android `TranslateController.java:66-67`, tdesktop `history_view_translate_tracker.cpp:35-36`, Web K `appTranslationsManager.ts:16`); iOS — видимые сообщения (`TelegramCore/.../Messages/Translate.swift:385`).
- Ответ сопоставляется по порядку (Android `TranslateController.java:1162-1167`, iOS `Translate.swift:406-412`, tdesktop `translate_mtproto_provider.cpp:67-82`). iOS кладёт в `id` и опросы — на месте сообщения без текста нужен пустой `textWithEntities`.
- `to_lang`: `xx`, `xx-YY` (Android `TranslateController.java:2123-2133`), `no` вместо `nb` (`TranslateAlert2.java:336-338`), tdesktop — двухбуквенный (`translate_mtproto_provider.cpp:107`). `tone` не шлёт никто.
- Ручной перевод iOS — `messages.composeMessageWithAI` с `translate_to_lang` (`ChatController.swift:4590-4624`, `TextProcessingTranslateContentComponent.swift:240-282`, `Translate.swift:856-926`); ошибки там все `.generic`, кроме `AICOMPOSE_FLOOD_PREMIUM` (его не отдавать: покажет продажу Premium).
- `FLOOD_WAIT_X` Android, tdesktop и Web K (X ≤ 60) повторяют сами (`ConnectionsManager.cpp:1372-1420`, `mtp_instance.cpp:1583-1625`, `apiManager.ts:841-870`); iOS показывает `.limitExceeded`. 500 Android и tdesktop повторяют без конца — сервер его не отдаёт.
- `TRANSLATIONS_DISABLED_ALT` никогда не отдавать: Android и iOS уйдут переводить с устройства в Google (`TranslateController.java:1168-1183`, `Translate.swift:60`).
- `togglePeerTranslations` шлётся и с `disabled`, и без (Android `TranslateController.java:349-361`); флаг читается из полного чата или пользователя (`:194-214`). Обновления нет.
- `translateRichMessage` — только для `rich_message`, `summarizeText` — только при `summary_from_language`; Pluma ни того, ни другого не выставляет, клиенты эти методы не зовут (ADR 0064).
- Не проверено: что Telegram отвечает на сообщение без текста в пачке (выбрано: пустой элемент), какие пределы пачки у Telegram, что клиенты делают с переводом после правки сообщения (сервер от этого не зависит: кэш по содержимому). iOS прочитан на 24638bc69b — сверить пути с закреплённым выпуском iOS (`upstream.env`).

## Дизайн

### Таблицы (`teamgramd/deploy/sql/migrate-20261119-translation.sql`)
Имя закреплено в плане партии (у других задач партии свои имена с той же датой; от чужих таблиц миграция не зависит). Только `CREATE TABLE IF NOT EXISTS`, повторный запуск ничего не меняет.

```sql
-- One row per translated text: the key is the text, its entities, the
-- target language, the model and the profile, so a post read by many and
-- every forward of a message are translated once; an edited message is
-- another text and another key. Rows unused for CacheTTL are swept.
CREATE TABLE IF NOT EXISTS `translation_cache` (
  `cache_key` binary(32) NOT NULL COMMENT 'SHA-256, pkg/translate.CacheKey',
  `to_lang` varchar(16) NOT NULL COMMENT 'the language of the profile, not the client''s spelling',
  `model` varchar(64) NOT NULL,
  `text` mediumtext NOT NULL,
  `entities` mediumblob NOT NULL COMMENT 'protobuf of the entities of the translation',
  `markup` tinyint NOT NULL DEFAULT 1 COMMENT '1 tags kept, 2 fallback: formatting lost',
  `created_at` bigint NOT NULL COMMENT 'unix ms',
  `used_at` bigint NOT NULL COMMENT 'unix ms, updated at most once an hour',
  PRIMARY KEY (`cache_key`),
  KEY `idx_used` (`used_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- messages.togglePeerTranslations: the translation bar of a chat hidden
-- by its reader; translations_disabled of userFull, chatFull, channelFull.
CREATE TABLE IF NOT EXISTS `peer_translation_settings` (
  `user_id` bigint NOT NULL,
  `peer_type` tinyint NOT NULL,
  `peer_id` bigint NOT NULL,
  `disabled` tinyint(1) NOT NULL,
  `updated_at` bigint NOT NULL,
  PRIMARY KEY (`user_id`, `peer_type`, `peer_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- The notice of the model's license (ADR 0064, section 10): sent once.
CREATE TABLE IF NOT EXISTS `translation_notices` (
  `user_id` bigint NOT NULL,
  `sent_at` bigint NOT NULL,
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

Строки `peer_translation_settings` с `disabled = 0` можно удалять, а не хранить (отсутствие = не скрыто); выбрать одно и держаться его.

### `pkg/translate`
- `engine.go`: интерфейс `Engine{ Translate(ctx, Request) (string, error) }`, ошибки `ErrUnavailable` (соединение, 5xx, 429 — повторить) и `ErrRejected` (4xx, кроме 429 — не повторять); `http_engine.go` — `POST {URL}/v1/chat/completions`, `Authorization: Bearer` при заданном токене, тело `{model, messages:[{role:"user",content}], stream:false, temperature, top_p, top_k, repeat_penalty, max_tokens, stop}`; ответ — `choices[0].message.content`; `finish_reason = "length"` — ошибка элемента.
- `profile.go`, `profile_hymt.go`: профиль `hy-mt1.5` (версия `1`): языки модели (`zh`, `en`, `fr`, `pt`, `es`, `ja`, `tr`, `ru`, `ar`, `ko`, `th`, `it`, `de`, `vi`, `ms`, `id`, `tl`, `hi`, `zh-Hant`, `pl`, `cs`, `nl`, `km`, `my`, `fa`, `gu`, `ur`, `te`, `mr`, `he`, `bn`, `ta`, `uk`, `bo`, `kk`, `mn`, `ug`, `yue` — по карточке модели) с английским и китайским именами; нормализация `to_lang` (ADR 0064, раздел 4); шаблоны карточки: «XX⇔XX» для перевода не на китайский, «ZH⇔XX» для перевода на китайский, «форматированный» (китайский текст запроса, имя языка по-китайски, `<source>…</source>`, ответ в `<target>…</target>`); параметры выборки карточки; `max_tokens` = 3 × входных знаков / 2 + 64, не больше половины контекста слота. Шаблоны и параметры — константы профиля с ссылкой на карточку; изменение — новая версия профиля (новые ключи кэша). Какой шаблон лучше для источника на китайском и нужна ли `temperature` ниже 0,7 для устойчивости — решает замер (часть 1).
- `markup.go`: сущности → дерево → текст с тегами `<sN>`; ответ → проверка → текст и сущности; запасной путь кусками (ADR 0064, раздел 3). Смещения в UTF-16 (`unicode/utf16`), суррогатные пары не режутся. `inputMessageEntityMentionName` из входа превращается в `messageEntityMentionName` с `user_id` (пользователь должен существовать, иначе сущность отбрасывается — как при отправке сообщения). `textUrl` сохраняет адрес, `pre` — язык, `customEmoji` — `document_id`, `blockquote` — `collapsed`.
- `cache.go`, `store.go`: `CacheKey`, чтение пачкой (`WHERE cache_key IN (…)`), запись `INSERT … ON DUPLICATE KEY UPDATE used_at`, обновление `used_at` не чаще раза в час, подметальщик (каждые 10 минут удаляет `used_at < now − CacheTTL` пачками по 1000); `peer_translation_settings` (`Set`, `Disabled(user, peers…)`), `translation_notices` (`MarkNoticeSent` — `INSERT IGNORE`, отправка только если вставилось).
- `scheduler.go`: очередь в памяти с двумя приоритетами и кругом по пользователям, `Slots` исполнителей, singleflight по ключу кэша, `SyncWait`, продолжение работы после ответа клиенту, повторы (до двух, паузы 1 и 3 с), неудача элемента на `FailedTTL` (60 с), состояние движка (после `ErrUnavailable` — проверка `/health` раз в 5 с, новые элементы не ставятся, ответ `FLOOD_WAIT_10`), остановка при выключении bff (`Stop` — дождаться текущих обращений до 10 с).
- `limits.go`: пределы раздела 7 ADR через `logincode.Limiter` (ключи `translate_items_m_<user>`, `translate_items_h_<user>`); считаются только элементы, ушедшие в движок.
- `errors.go`: `TO_LANG_INVALID`, `INPUT_TEXT_TOO_LONG` — своими `status.Error(mtproto.ErrBadRequest, …)` (в `rpc_error_codes.go` их нет); `mtproto.ErrInputTextEmpty`, `ErrMsgIdInvalid`, `ErrPeerIdInvalid`, `ErrBotMethodInvalid` — готовые; `FLOOD_WAIT_X` — как у соседей (`pkg/logincode`).
- `config.go`, `metrics.go` (метрики ADR 0064, раздел 13).

### `app/bff/translation` (по образцу `app/bff/transcription`)
- `helper.go`, `internal/{config,svc,core,server/grpc/service}`; по файлу на метод:
  - `messages.translateText_handler.go`, `messages.togglePeerTranslations_handler.go`, `channels.toggleAutotranslation_handler.go` (делегирует в область каналов через интерфейс `AutotranslationToggler`, который `server.go` передаёт при сборке: реализация `channels.toggle` остаётся на месте);
  - `messages.composeMessageWithAI_handler.go` (форма «перевести»);
  - `not_served.go` — семь `aicompose.*` с `ErrEnterpriseIsBlocked` и комментарием.
- Чтение сообщений — пачкой: `message.getUserMessageList` для личных, «Избранного» и групп (проверка, что каждая копия из этого диалога, как `dialogOf` в `transcription/internal/core/record.go:107-112`), `channel.getMutableChannel` + `channel.getMessages` для каналов (`record.go:69-96`). Текст элемента — `message.message` и `entities` сообщения (`Predicate_message`); иначе пустой элемент.
- Раскрытие: после первого успешного ответа (кэш или движок) — `MarkNoticeSent`; если вставилось — сообщение 777000 через тот же путь, что у служебных сообщений авторизации (`app/bff/authorization/servicetext`, `PasswordRecovered.Text`), текст `langpack.MailText("PlumaTranslationNotice", lang)` с подстановкой `{provider}` из конфигурации (`Translate.ProviderName`). Ошибка отправки не роняет перевод: строка лога и счётчик, отметка снимается, чтобы отправить в следующий раз.

### Файлы
Добавить:
- миграция (выше);
- `pkg/translate/*.go` и тесты, `pkg/translate/enginetest` (набор для настоящего движка, как `pkg/transcribe/enginetest`);
- `app/bff/translation/...`;
- `teamgramd/deploy/translate/Dockerfile` (стадия сборки скачивает GGUF по `MODEL_URL`, сверяет `MODEL_SHA256`; база — официальный образ `llama-server` закреплённой версии; кладёт `License.txt` модели, `NOTICE` с фразой §3(d), лицензию llama.cpp), `teamgramd/deploy/translate/entrypoint.sh` (флаги из окружения), `docker-compose.translate.yaml`;
- `tools/translatebench` (замер: корпус, задержки, удержание тегов, память) — или подкоманда `enginetest`, на выбор исполнителя;
- `test/e2e/faketranslate/` (`main.go`, `Dockerfile`): отвечает по протоколу OpenAI детерминированным «переводом» (например, `[ru] ` + текст с сохранением тегов), проверяет `Bearer`, умеет задержку, 503, потерю тегов и лишний тег по сценарию; отдаёт тестам число обращений на loopback-порту;
- `test/e2e/translation_test.go`;
- `docs/ops/translation.md`.

Изменить:
- `app/bff/bff/internal/server/server.go`: `RPCTranslation` — с области `channels` на `translation`; регистрация `RPCAiComposeTone`; запуск и `Stop` планировщика;
- `app/bff/channels/internal/core/not_served.go` (убрать два метода), `app/bff/channels/internal/server/grpc/service/translation_service_impl.go` (удалить; `ChannelsToggleAutotranslation` остаётся методом `ChannelsCore`);
- `teamgramd/etc/session.yaml`, `teamgramd/etc2/session.yaml`, `app/interface/session/etc/session.yaml`: `"/mtproto.RPCAiComposeTone": "bff.bff"`;
- `app/bff/messages/internal/core/messages.translateRichMessage_handler.go` (комментарий вместо лога «License key», ответ тот же), `messages.summarizeText_handler.go` — не трогать (владелец 098 D);
- `app/bff/users/internal/core/users.getFullUser_handler.go:98`, `app/bff/chats/internal/core/messages.getFullChat_handler.go:74`, `app/bff/channels/internal/core/channels.getFullChannel_handler.go:264` и их `svc` (доступ к `peer_translation_settings`);
- `app/bff/configuration/internal/appconfig/appconfig.go` (переопределение ключей с пересчётом хэша), `app/bff/configuration/internal/svc/service_context.go`; `teamgramd/bin/app_config.json:109-110` → `enabled`; `docs/ops/app-config.md`;
- `app/bff/bff/internal/config/config.go`, `app/bff/bff/etc/bff.yaml`, `teamgramd/etc/bff.yaml`, `teamgramd/etc2/bff.yaml` — блок `Translate`;
- `.env.example`, `docker-compose.yaml` (переменные bff), `pkg/conf/compose_services_test.go` (`optionalVars`), `TestComposeMemory` (новый сервис с лимитом);
- `pkg/langpack/mail.json` (ключ `PlumaTranslationNotice` на 37 языках), тест полноты;
- `tools/logaudit/logaudit.go` (имена `translation`, `translated`, `sourcetext`, `prompt`, `completion` в аргументах логов);
- `teamgramd/deploy/prometheus/server/rules/*.yml`, `docs/ops/monitoring.md`, `docs/ops/memory.md`, `docs/ops/home-server.md`, `.github/workflows/images.yml` (образ `pluma-translate`), `~/pluma-work/bin/pluma-deploy.sh` (подтянуть образ и оверлей; вне репозитория);
- клиент iOS: `ci/patch.py` с якорем `displayEmojify` в `TextProcessingTranslateContentComponent.swift`, проверка и тест, запись в реестр `clients/patches`.

### Обновления другим устройствам и людям
- Перевод — только ответ на запрос; обновлений нет (в схеме их нет).
- `togglePeerTranslations` — без обновления; другие сессии читают флаг из `getFull*`.
- Сообщение 777000 о лицензии — обычное служебное сообщение всем сессиям пользователя.

### Пределы и ошибки (сводка)
| Случай | Ответ |
|---|---|
| бот | `BOT_METHOD_INVALID` |
| ни `peer`+`id`, ни `text` | `INPUT_TEXT_EMPTY` |
| `peer` без `id` | `MSG_ID_INVALID` |
| диалога нет, канал закрыт или удалён | `PEER_ID_INVALID` |
| язык вне таблицы профиля | `TO_LANG_INVALID` |
| элемент > 4096, запрос > 100 элементов или > 32 768 знаков | `INPUT_TEXT_TOO_LONG` |
| сообщения нет или без текста; пустой текст | пустой элемент |
| частота сверх предела, не успели за `SyncWait`, движок недоступен, элемент недавно не удался | `FLOOD_WAIT_X`, X от 3 до 30 |
| перевод выключен (`TRANSLATE_URL` пуст) | `ErrEnterpriseIsBlocked` |
| `composeMessageWithAI` не в форме «перевести», `aicompose.*`, `translateRichMessage`, `summarizeText` | `ErrEnterpriseIsBlocked` |

Никогда: 500, `TRANSLATIONS_DISABLED_ALT`, `AICOMPOSE_FLOOD_PREMIUM`.

### Поведение после перезапуска
- bff: очередь и неудачи элементов теряются, кэш, флаги чатов и отметки раскрытия — в MySQL. Клиенты повторяют; повтор Android/tdesktop/Web K после `FLOOD_WAIT_X` найдёт готовое или поставит снова.
- `translate`: на время загрузки модели (секунды) — `FLOOD_WAIT_10`; затем работа продолжается сама.
- Смена модели или профиля: старые строки кэша не отдаются и уходят по сроку.

## Порядок работ
Части 1, 2 и 3 независимы и идут параллельно; 4 — после 2 и 3; 5 — после 4; 6 и 7 — после 3 и параллельно с 4–5; 8 — в репозитории iOS, когда угодно; 9 — после 5–7; 10 — последней.

1. **Движок и замер, первый проход (M).** `teamgramd/deploy/translate/*`, `docker-compose.translate.yaml`, `pkg/translate/enginetest` или `tools/translatebench`. Собрать образ на CachyOS с `Q8_0` и `Q6_K`; проверить шаблоны карточки на ru⇄en, uk→ru, kk→ru, en→kk и на тестовом наборе сообщений с сущностями (жирный внутри ссылки, упоминание, код, свой эмодзи, цитата, хештег, вложенные и пересекающиеся); выбрать шаблон для текста с тегами и `temperature`. Записать в `docs/ops/translation.md`. Общих файлов с другими частями нет.
2. **Разметка, профиль, клиент движка (M–L).** `pkg/translate/{markup,profile,profile_hymt,engine,http_engine,errors}.go` с модульными тестами. Чистый Go.
3. **Хранилище (S–M).** Миграция, `pkg/translate/{cache,store}.go`, интеграционные тесты.
4. **Планировщик и пределы (M).** `pkg/translate/{scheduler,limits,metrics,config}.go`.
5. **Область bff (M).** `app/bff/translation`, `server.go`, три `session.yaml`, конфиги bff, `not_served.go` каналов, `translateRichMessage`, раскрытие 777000. Общие файлы: `server.go` и `session.yaml` правят и другие задачи партии — сливать по одной (план партии ставит такие части в разные волны). `RPCAiComposeTone` регистрирует только эта часть.
6. **Флаг в полных профилях (S).** Три обработчика `getFull*` и их `svc`. Общие файлы с 104 (имена пользователей, истории) и 105 (Business) — `users.getFullUser_handler.go`, `channels.getFullChannel_handler.go`; при слиянии читать обе стороны.
7. **Конфиг клиентов и тексты (S–M).** `appconfig` с переопределением, `app_config.json`, `docs/ops/app-config.md`, `PlumaTranslationNotice` на 37 языках (вычитка как у строк 076), `logaudit`.
8. **Патч iOS (S).** Скрыть «эмодзи» в режиме `.translate`; `patch_test.py`, тест на настоящем апстриме с порчей якоря; запись I-xxx в реестр.
9. **Тесты (M).** `test/e2e/faketranslate`, сервис в стенде (`test/e2e/docker-compose.e2e.yaml`, `run.sh`: `TRANSLATE_URL` на него), `test/e2e/translation_test.go`, интеграционные тесты потока.
10. **Эксплуатация и выкладка (S–M).** Метрики и тревоги, `docs/ops/*`, `images.yml`, `.env.example`, compose, `pluma-deploy.sh`; **замер на домашнем сервере** (ниже) и включение.

### Замер на домашнем сервере (часть 10, до включения)
- Только когда у владельца ничего не играет (Jellyfin) и нет звонков; не трогать чужие контейнеры; перед замером `df -h`, `free -m`; после — удалить временные контейнеры.
- `llama-bench` из того же образа: `Q8_0` и `Q6_K`, потоки 4, 6, 8; отдельно — только производительные ядра (`--cpuset-cpus`), если номера ядер различимы (`lscpu -e`).
- Набор `enginetest`/`translatebench` против поднятого `translate` с `--cpus` и `cpu_shares` как в оверлее: задержка p50/p95 одного сообщения (20, 60, 300, 4096 знаков) и пачки из 20 при слотах 1, 2, 4; доля сохранённых тегов; пик памяти (`docker stats`); загрузка процессора остальной Pluma и время ответа `messages.sendMessage` на стенде под нагрузкой перевода.
- Качество: chrF++ на FLORES-200 devtest (ru⇄en, uk→ru, kk→ru, en→kk; набор скачивается при замере, в репозиторий не кладётся — лицензия CC-BY-SA) — для сравнения `Q8_0` и `Q6_K`, не для приёмки.
- Итог в `docs/ops/translation.md`: выбранные квантование, `TRANSLATE_THREADS`, `TRANSLATE_SLOTS`, `TRANSLATE_CTX`, `TRANSLATE_MEMORY`, `TRANSLATE_CPUS`, `SyncWait`, `Timeout`; строка в `docs/ops/memory.md`. Порог включения: одно сообщение в 60 слов — p95 ≤ 6 с, пачка из 20 — ≤ 25 с при слотах по замеру; иначе — `Q6_K` или меньше слотов и вопрос владельцу.

## Тесты
Модульные:
- `markup`: каждая сущность слоя 233 туда и обратно; вложенные и пересекающиеся; эмодзи и суррогатные пары на границах; пробелы у краёв тегов; модель потеряла тег, добавила тег, переставила вложенность, перевела содержимое неприкосновенного тега (восстанавливается из оригинала) — запасной путь кусками; пустой ответ; ответ без `<target>`; `inputMessageEntityMentionName` → `messageEntityMentionName`.
- `profile_hymt`: нормализация `to_lang` (`en`, `EN`, `pt-BR`, `pt_br`, `zh-CN`, `zh-Hans`, `zh-TW`, `nb`, `xx`), выбор шаблона, `max_tokens`.
- `http_engine` на `httptest`: тело и заголовки, `finish_reason = length`, 429/503 → `ErrUnavailable`, 400 → `ErrRejected`, обрезанный и не-JSON ответ, срок контекста.
- `scheduler`: порядок приоритетов и круг по пользователям; singleflight; `SyncWait` истёк — `FLOOD_WAIT_X`, работа дошла до кэша; повторы и `FailedTTL`; движок недоступен — новые элементы не ставятся; `Stop`.
- обработчик `translateText` с поддельным движком и хранилищем: порядок и длина ответа равны входу; пустые элементы; ошибки таблицы «Пределы и ошибки»; `composeMessageWithAI` — форма «перевести» и отказ остальным; бот.
- `appconfig`: без движка оба ключа `disabled`, хэш другой; с движком — как в файле.
- `pkg/langpack`: `PlumaTranslationNotice` во всех 37 языках; `tools/logaudit` ловит вывод текста и запроса.

Интеграционные (`pkg/testinfra`, настоящий MySQL):
- миграция дважды подряд;
- кэш: запись, чтение пачкой, `used_at` не чаще раза в час, подметальщик удаляет только просроченное, гонка двух записей одного ключа;
- `peer_translation_settings` и `translation_notices` (одна отправка при гонке двух первых переводов);
- поток `app/bff/translation` с поддельным движком на `httptest` и настоящими сервисами сообщений (как `transcription/internal/core/flow_integration_test.go`): сообщение канала, к которому нет доступа, — `PEER_ID_INVALID`; чужая копия по чужому `id` — пустой элемент, а не чужой текст.

Сквозные (`pkg/mtclient`, стенд с `faketranslate`, сессии после `getState`):
- `TestTranslateMessages`: A пишет B три сообщения (одно с жирным, ссылкой `textUrl`, упоминанием и кодом, одно — фото без подписи); B переводит `peer`+`id` — три элемента по порядку, второй пустой, сущности на местах, код не тронут.
- `TestTranslateTextBatch`: `text` из 20 элементов, один пустой; `to_lang = pt-BR` и `zh-TW` — принят, `xx` — `TO_LANG_INVALID`; 4097 знаков — `INPUT_TEXT_TOO_LONG`.
- `TestTranslateChannelCache`: пост канала, два подписчика переводят — к `faketranslate` одно обращение; правка поста — новый перевод.
- `TestTranslateEngineDown`: `faketranslate` отвечает 503 — `FLOOD_WAIT_10`; задержка дольше `SyncWait` — `FLOOD_WAIT_X`, повтор после X — готовый перевод без нового обращения.
- `TestTranslateMarkupFallback`: `faketranslate` теряет тег — ответ без оформления, неприкосновенные сущности на местах.
- `TestTogglePeerTranslations`: скрыть и вернуть для пользователя, группы, канала; `getFull*` второй сессии отдаёт флаг; чужой канал — `PEER_ID_INVALID`.
- `TestComposeMessageWithAITranslate`: форма «перевести» — `result_text`; `emojify` — `ERR_ENTERPRISE_IS_BLOCKED`; `aicompose.getTones` — то же.
- `TestTranslateNotice`: первый перевод — одно сообщение 777000 на языке сессии, второй — нет.
- `TestTranslationOff`: стенд без `TRANSLATE_URL` — `help.getAppConfig` с `disabled`, `translateText` — `ERR_ENTERPRISE_IS_BLOCKED`.
- Проверить падающим без изменения хотя бы `TestTranslateMessages` (на `master` — `ERR_ENTERPRISE_IS_BLOCKED`).

Против настоящего движка (CachyOS, не в CI): `enginetest` — ru⇄en, kk→ru с сущностями, доля сохранённых тегов не ниже порога замера.

## Проверки перед сдачей (CachyOS)
- `go build ./...`; `go vet -tags integration,e2e -unreachable=false` затронутых пакетов; `go test` затронутых и импортирующих их, затем `-race`.
- Интеграционные `pkg/translate`, `app/bff/translation` по одному пакету; e2e из раздела «Тесты» со своими `E2E_PROJECT`/`E2E_DIR`, затем `test/e2e/run.sh down` и удаление образов стенда.
- `make lint`, `go test ./tools/...`, `go test ./pkg/conf/...`, `go run ./tools/apicoverage` (+3 метода: `translateText`, `togglePeerTranslations`, `composeMessageWithAI`).
- На машине разработки ничего не собирать.

## Проверки для `docs/project/TESTING-BACKLOG.md`
- [ ] Android, iOS, Desktop, Web: «Перевести» у чужого сообщения на русском, английском, украинском, казахском — перевод на язык интерфейса; жирный, ссылки, упоминания, код и свои эмодзи на местах.
- [ ] Панель перевода чата на иностранном языке (8+ сообщений) появляется во всех четырёх клиентах; «Перевести» переводит видимые сообщения, «Показать оригинал» возвращает; прокрутка вверх переводит старые.
- [ ] «Не переводить» на панели на телефоне — после перезахода в чат панели нет и на Desktop.
- [ ] Канал с автопереводом (включить в настройках канала): подписчик с другим языком видит посты переведёнными сразу.
- [ ] Перевод расшифровки голосового и опроса (Android, iOS).
- [ ] iOS: экран перевода без переключателя «эмодзи», смена языка на экране работает; язык, которого нет у модели (норвежский), — понятная ошибка.
- [ ] Первый перевод — сообщение от Pluma о модели и о том, что Tencent не связана с Pluma, на языке интерфейса; второго такого нет.
- [ ] Остановить `translate` на минуту: Android, Desktop, Web дожидаются и показывают перевод после запуска; iOS — ошибка, повтор после запуска работает.
- [ ] Перевод длинного сообщения (около 4000 знаков) приходит меньше чем за минуту; звонок и отправка сообщений во время пачки переводов не тормозят.
- [ ] Правка переведённого сообщения: перевод обновляется после повторного «Перевести».

## Эксплуатация
- `docs/ops/translation.md`: что делает сервис, включение и выключение (`TRANSLATE_URL`), замена модели и квантования (коммит значений `ARG MODEL_*`, сборка в CI, как в `docs/ops/transcription.md`), итоги замера, метрики, что делать при тревоге, раскрытие по лицензии и где оно показано, ограничение территории лицензии.
- Переменные (`.env.example`, все необязательные): `TRANSLATE_URL` (`http://translate:8080`; пусто — выключено), `TRANSLATE_TOKEN` (≥ 32 знаков, общий для bff и сервиса), `TRANSLATE_MODEL` (имя, на которое отвечает `llama-server`), `TRANSLATE_PROFILE` (`hy-mt1.5`), `TRANSLATE_PROVIDER_NAME` (юридическое имя поставщика для раскрытия; без него перевод не включается — проверка при старте bff), `TRANSLATE_THREADS`, `TRANSLATE_SLOTS`, `TRANSLATE_CTX`, `TRANSLATE_MEMORY`, `TRANSLATE_CPUS`, `PLUMA_TRANSLATE_IMAGE`. Блок `Translate` в `bff.yaml`: `URL`, `Token`, `Model`, `Profile`, `ProviderName`, `Slots`, `SyncWait`, `SyncWaitSingle`, `Timeout`, `FailedTTL`, `CacheTTL`, `MaxItemLength`, `MaxItems`, `MaxRequestLength`.
- Миграция применяется сервисом `migrate` при выкладке.
- Образ `pluma-translate` около 2,2 ГБ, диск — один слой модели; память по замеру (оценка 2,5–4 ГБ в `MemoryMax=10G` вместе с 3,6 ГБ остальной Pluma); 6 потоков с `cpu_shares: 256`.
- Кэш растёт с объёмом переводов: строка ~2× длины текста; при 100 пользователях — десятки мегабайт за 30 дней (оценка).
- Тревога `TranslateEngineDown` (нет здоровья 10 минут при включённом переводе), панель в Grafana: очередь, задержка, доля кэша, запасной путь разметки.
- Выкладка: образ из CI → `.env` (`TRANSLATE_*`) → оверлей `translate` → замер → перезапуск bff. Откат: пустой `TRANSLATE_URL` и перезапуск bff — клиенты при следующем `help.getAppConfig` прячут перевод.

## Решения, требующие подтверждения владельцем
1. **Территория лицензии.** HY-MT1.5 нельзя предоставлять пользователям в ЕС, Великобритании и Южной Корее (§1(l), §3, §5(c)). 2026-11-15 владелец назвал регионы «Европа, Россия, СНГ и Америка» — если это о том, где живут пользователи Pluma и среди них есть ЕС или Великобритания, HY-MT не годится: нужен TranslateGemma-4B (условия Gemma стран не исключают; медленнее, нужен детектор языка — отдельная задача) или перевод только для пользователей вне этих стран. Где стоит сервер (дома) и где живут люди — подтвердить.
2. **Имя поставщика для раскрытия** (§3(e) требует полное юридическое имя настоящего поставщика сервиса) — `TRANSLATE_PROVIDER_NAME`: ФИО владельца или иное лицо.
3. Раскрытие: сообщение 777000 при первом переводе и строка на сайте; патчей клиентов ради раскрытия нет.
4. Квот нет; от злоупотребления — 300 элементов в движок в минуту и 3000 в час на человека.
5. Кэш переводов 30 дней от последнего обращения; перевод удалённого сообщения живёт в кэше до срока.
6. Сбой, перегрузка или долгий перевод — `FLOOD_WAIT_X` (Android, Desktop, Web дождутся сами; iOS покажет ошибку), не `TRANSLATE_REQ_QUOTA_EXCEEDED`.
7. `tone` игнорируется (никто не шлёт); сообщение без текста в пачке — пустой элемент.
8. Патч iOS: скрыть переключатель «эмодзи» на экране перевода.
9. Квантование `Q8_0` (запасное `Q6_K`) и параметры сервиса — по замеру.

## Не проверено
- Скорость, память и качество HY-MT1.5-1.8B на i5-13500T — только оценки; DDR4 или DDR5 у сервера, поддержка AVX-VNNI в сборке llama.cpp — не выяснялись.
- Удержание тегов `<sn>` моделью на ru/uk/kk/en: карточка показывает форматированный шаблон только по-китайски и с переводом на китайский; нумерованные теги (`<s1>`) — наше прочтение «sn», проверяется в части 1.
- Поддержка архитектуры HY-MT1.5 закреплённой версией llama.cpp и точные имена флагов `llama-server` — сверить при сборке (официальный GGUF «для llama.cpp» есть).
- Новая серия Hy-MT2 (упомянута в README Tencent) — не изучалась.
- Пределы пачки и поведение Telegram на сообщение без текста; что клиенты делают с переводом после правки.
- iOS прочитан на 24638bc69b, не на закреплённом выпуске.
- TranslateGemma: требования к коду исходного языка и качество казахского — из исследования, не перепроверены.
- Как ответят клиенты на 406 `TRANSLATIONS_DISABLED` — не нужно: не используется.

## Результат
_(заполняется по завершении)_
