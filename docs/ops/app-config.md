# Конфиг приложения (`help.getAppConfig`)

По ключам этого конфига официальные клиенты включают и выключают функции и выставляют лимиты (задача 015). Сервер отдаёт его в обоих конструкторах `help.getAppConfig`.

## Где лежит и как менять

- **Файл.** `teamgramd/bin/app_config.json`, рядом с `config.json` (он для `help.getConfig`). В образе это `/app/bin/app_config.json`. Файл — JSON-объект, ключи в нём сгруппированы по темам, порядок сохраняется в ответе.
- **Когда читается.** Сервис bff читает файл при старте. Ошибка в файле (не объект, повтор ключа, лишние данные после объекта) не даёт bff запуститься, в логе — причина. После правки нужно перезапустить bff.
- **`hash`.** Считается от содержимого без пробелов и переносов (FNV-1a, 31 бит, не 0). Клиент присылает `hash` своего конфига. Если он совпал, сервер отвечает `help.appConfigNotModified`. Изменение любого значения меняет `hash`, и клиенты забирают новый конфиг при следующем запросе:
  - Android — раз в несколько минут;
  - Web K — когда его копии больше суток;
  - tdesktop — периодически.
- **Тесты.**
  - Каждый ключ файла должен быть в таблицах ниже (`app/bff/configuration/internal/appconfig`).
  - Лимиты папок должны совпадать с константами сервиса диалогов (`app/service/biz/dialog/internal/dao`).

## Как выбраны ключи

- **Источники.** Ключи взяты из кода клиентов, на которых собирается Pluma:
  - Android 12.9.1: `MessagesController.applyAppConfig` и `AppGlobalConfig`;
  - tdesktop v7.0.9: `appConfig().get<…>` и `Data::PremiumLimits`;
  - Web K `346844bf`: `MTAppConfig` и `getLimit`.

  Значения Telegram взяты из примера на [core.telegram.org/api/config](https://core.telegram.org/api/config). Всего клиенты читают около 270 ключей.
- **Если ключа нет в ответе.** Android и tdesktop берут своё значение по умолчанию. У Web K значений по умолчанию нет, такой ключ у него `undefined`.
- **Что в файле.**
  - Лимиты. Где сервер лимит проверяет — его значение. Где не проверяет — значение Telegram.
  - Выключатели того, чего на сервере нет. Они передаются явно, даже если у клиента сейчас такое же значение по умолчанию: значения по умолчанию меняются от версии к версии.
  - Ключи, которые Web K читает для реализованных возможностей и лимитов.
  - Значения Telegram, которые расходятся с клиентскими по умолчанию и важны Pluma (фоновое соединение Android).
- **Premium (ADR 0016).** Лимит с Premium как у Telegram ставится только там, где сервер это реализует (папки) или принимает любое число (закреплённые чаты). Для остального значение с Premium равно бесплатному, чтобы клиент не обещал того, чего нет.
- **Чего в файле нет.**
  - Значения Telegram для конкретного пользователя или сессии: `freeze_*`, `call_requests_disabled`, `phone_country_iso2`, `transcribe_audio_trial_cooldown_until`.
  - Боты и сервисы Telegram: `premium_bot_username`, `premium_invoice_slug`, `telegram_antispam_user_id`, `ton_*`, `stars_*` с суммами, `weather_search_username` и т. п.
  - Настройки внутренностей клиента (видео-кружки, очереди загрузки, анимации). Здесь подходят значения клиента по умолчанию.

Клиенты в таблицах: A — Android, D — Desktop (tdesktop), W — Web K.

## Premium и платежи

| Ключ | Значение | Читают | Почему |
|---|---|---|---|
| `premium_purchase_blocked` | `true` | A D W | Оплаты нет (ADR 0016): клиенты скрывают покупку Premium. По умолчанию у Android `false`, у tdesktop `true`. |
| `premium_promo_order` | `["more_upload", "double_limits", "folder_tags", "message_privacy", "profile_badge"]` | A W | Экран Premium показывает только перечисленное. Реализовано: файлы 4 ГБ, лимиты папок и других возможностей (задача 025), теги папок, «только контакты и Premium», значок у имени. |
| `premium_gift_attach_menu_icon` | `false` | A | Подарка Premium нет. |
| `premium_gift_text_field_icon` | `false` | A | Подарка Premium нет. |
| `stars_purchase_blocked` | `true` | A W | Покупки звёзд нет. |
| `stars_gifts_enabled` | `false` | A | Подарков за звёзды нет. У Android по умолчанию `true`. |
| `stargifts_blocked` | `true` | A | Подарков за звёзды нет. |
| `enable_gifts_in_profile` | `false` | A | Вкладка подарков в профиле. У Android по умолчанию `true`. |
| `stars_paid_messages_available` | `false` | A D | Платных сообщений нет. У Android по умолчанию `true`. |
| `giveaway_gifts_purchase_available` | `false` | A D | Розыгрышей нет. |
| `channel_revenue_withdrawal_enabled` | `false` | A | Вывода дохода каналов нет. |
| `starref_program_allowed` | `false` | A D | Партнёрских программ ботов нет. |
| `starref_connect_allowed` | `false` | A D | Партнёрских программ ботов нет. |

## Приватность

| Ключ | Значение | Читают | Почему |
|---|---|---|---|
| `new_noncontact_peers_require_premium_without_ownpremium` | `false` | A D | Настройку «писать могут только контакты и Premium» сервер разрешает включить только с Premium (задача 014). |
| `autoarchive_setting_available` | `false` | A D W | «Архивировать новые чаты»: сервер хранит настройку, но не применяет. У Telegram `true`. |

## Папки

| Ключ | Значение | Читают | Почему |
|---|---|---|---|
| `dialog_filters_enabled` | `true` | A D | Папки есть (задача 013), у Telegram `true`. По умолчанию у клиентов `false`. |
| `dialog_filters_limit_default` | `10` | A D W | Проверяет сервер (`DIALOG_FILTERS_TOO_MUCH`). |
| `dialog_filters_limit_premium` | `30` | A D W | Проверяет сервер. Без ключа Android берёт 20. |
| `dialog_filters_chats_limit_default` | `100` | A D W | Проверяет сервер (`FILTER_INCLUDE_TOO_MUCH`). |
| `dialog_filters_chats_limit_premium` | `200` | A D W | Проверяет сервер. |

## Закреплённые чаты

Сервер эти лимиты не проверяет и принимает любое число. Значения — Telegram.

| Ключ | Значение | Читают | Почему |
|---|---|---|---|
| `dialogs_pinned_limit_default` | `5` | D W | Закреплённые чаты в списке. |
| `dialogs_pinned_limit_premium` | `10` | D W | То же с Premium. |
| `pinned_dialogs_count_max_default` | `5` | A | То же для Android, у него свой ключ. |
| `pinned_dialogs_count_max_premium` | `10` | A | То же с Premium. Без ключа Android берёт 5. |
| `dialogs_folder_pinned_limit_default` | `100` | D W | Закреплённые чаты в архиве. |
| `dialogs_folder_pinned_limit_premium` | `200` | D W | То же с Premium. |
| `saved_dialogs_pinned_limit_default` | `5` | A D W | Закреплённые чаты в «Избранном». |
| `saved_dialogs_pinned_limit_premium` | `100` | A D W | То же с Premium. |
| `topics_pinned_limit` | `5` | A D W | Закреплённые темы форума. |
| `forum_upgrade_participants_min` | `0` | A D W | С какого числа участников владелец может включить темы. Без ключа Android и tdesktop берут 200, Web K не включает никогда; в Pluma столько участников не наберётся. |

## Длины текстов и загрузка

| Ключ | Значение | Читают | Почему |
|---|---|---|---|
| `message_length_limit_default` | `4096` | A D | Как `message_length_max` в `help.getConfig`. Сервер длину не проверяет. |
| `message_length_limit_premium` | `4096` | A D | Длинных сообщений с Premium сервер не реализует. Клиенты по умолчанию берут 8192. |
| `caption_length_limit_default` | `1024` | A D W | Как у Telegram. Сервер проверяет. |
| `caption_length_limit_premium` | `4096` | A D W | Как у Telegram (задача 025). Сервер проверяет по Premium пользователя. |
| `about_length_limit_default` | `70` | A D W | Как у Telegram. Сервер проверяет. |
| `about_length_limit_premium` | `140` | A D W | Как у Telegram (задача 025). |
| `quote_length_max` | `1024` | A D W | Как у Telegram. |
| `upload_max_fileparts_default` | `4000` | A D W | Как у Telegram: 2 ГБ частями по 512 КБ. Без Premium больше частей сервер не примет (задача 025). |
| `upload_max_fileparts_premium` | `8000` | A D W | Как у Telegram: 4 ГБ с Premium (задача 025). |

## Лимиты того, чего на сервере нет

Ничего не включают, но Web K без них получает `undefined` в `getLimit`. Значение — бесплатное значение Telegram для всех.

| Ключ | Значение | Читают | Почему |
|---|---|---|---|
| `channels_limit_default` | `500` | A D W | Каналов пока нет. |
| `channels_limit_premium` | `1000` | A D W | Как у Telegram (задача 025). |
| `channels_public_limit_default` | `10` | A D W | Каналов пока нет. |
| `channels_public_limit_premium` | `20` | A D W | Как у Telegram (задача 025). |
| `recommended_channels_limit_default` | `10` | A D W | Каналов пока нет. |
| `recommended_channels_limit_premium` | `10` | A D W | Каналов пока нет. |
| `chatlist_invites_limit_default` | `3` | A D W | Общих папок (`chatlists.*`) нет. |
| `chatlist_invites_limit_premium` | `3` | A D W | Общих папок нет. |
| `chatlists_joined_limit_default` | `2` | A D W | Общих папок нет. |
| `chatlists_joined_limit_premium` | `2` | A D W | Общих папок нет. |
| `saved_gifs_limit_default` | `200` | A D W | Сохранённых GIF (`messages.saveGif`) нет. |
| `saved_gifs_limit_premium` | `400` | A D W | Как у Telegram (задача 025). |
| `stickers_faved_limit_default` | `5` | A D W | Избранных стикеров нет. |
| `stickers_faved_limit_premium` | `5` | A D W | Избранных стикеров нет. |
| `reactions_user_max_default` | `3` | A D W | Реакций одного человека на сообщение: как в Telegram Premium, у всех (задача 053, решение владельца). Тот же лимит — теги в «Избранном». |
| `reactions_user_max_premium` | `3` | A D W | То же значение: Premium в Pluma нет, а клиенты по умолчанию ждут 3. |
| `reactions_uniq_max` | `11` | A D W | Разных реакций на одно сообщение, как в Telegram. Android число не применяет и берёт своё 11. |
| `reactions_in_chat_max` | `100` | A | Реакций, которые группа может разрешить (`messages.setChatAvailableReactions`), как в Telegram. |

## Выключатели того, чего на сервере нет

| Ключ | Значение | Читают | Почему |
|---|---|---|---|
| `stories_posting` | `"disabled"` | A | Историй нет (`stories.*`). У Android по умолчанию `"enabled"`. |
| `translations_manual_enabled` | `"disabled"` | A W | Перевода нет (`messages.translateText`). У Android по умолчанию `"enabled"`. |
| `translations_auto_enabled` | `"disabled"` | A W | Перевода нет. |
| `rich_message_posting` | `"disabled"` | A D | Расширенного форматирования нет. У клиентов по умолчанию `"premium"`. |
| `transcribe_audio_trial_weekly_number` | `0` | A D | Пробной расшифровки голосовых нет (`messages.transcribeAudio`). |
| `settings_display_passkeys` | `false` | A D W | Входа по passkey нет. Отладочные сборки Android по умолчанию его показывают. |
| `emojies_send_dice` | `[]` | A W | Кубики: наборов стикеров нет (`messages.getStickerSet`), анимации у клиента не будет, а ⚽ и 🎰 сервер разыгрывает неверно. Одиночный эмодзи уходит текстом. |

## Прочитанность

| Ключ | Значение | Читают | Почему |
|---|---|---|---|
| `pm_read_date_expire_period` | `604800` | A W | Время прочтения в личных чатах (`messages.getOutboxReadDate` есть). Web K без ключа его не показывает. |
| `chat_read_mark_expire_period` | `604800` | A W | Кто прочитал в группе (`messages.getMessageReadParticipants`). |
| `chat_read_mark_size_threshold` | `100` | A W | То же, для групп до 100 участников. |

## Новые входы

| Ключ | Значение | Читают | Почему |
|---|---|---|---|
| `authorization_autoconfirm_period` | `604800` | A D | Через сколько секунд после входа неподтверждённая сессия подтверждается сама (задача 019): клиенты снимают баннер «Это вы?», сервер перестаёт отдавать `unconfirmed` в `account.getAuthorizations`. Должно совпадать с `authsession.AuthorizationAutoconfirmPeriod` (это проверяет тест). Читает и iOS; у Telegram в примере и по умолчанию у клиентов то же значение. |

## Фоновое соединение (Android)

| Ключ | Значение | Читают | Почему |
|---|---|---|---|
| `background_connection` | `true` | A | Значение по умолчанию для «Фонового соединения», пока пользователь его не менял. Пушей нет (задача 009), сообщения приходят только по открытому соединению. У Telegram `true`. |
| `keep_alive_service` | `true` | A | То же для «Keep-Alive Service». |

## Домены и сервисы Telegram

| Ключ | Значение | Читают | Почему |
|---|---|---|---|
| `url_auth_domains` | `[]` | A W | Вход на сайты по ссылке: у Telegram — его домены, у Pluma таких нет. |
| `autologin_domains` | `[]` | A W | Домены, куда клиент подставляет токен входа. |
| `whitelisted_domains` | `["pluma.chat"]` | D W | Домены Telegram; у Pluma — домен её ссылок (задача 026). Web K делает ссылки на них в «о себе» кликабельными, tdesktop ищет среди них Fragment. |
| `fragment_prefixes` | `[]` | D W | Анонимные номера Fragment (+888). У tdesktop по умолчанию `["888"]`. |
| `ignore_restriction_reasons` | `[]` | A D W | Как у Telegram. |

## Чего конфигом не исправить

- **Premium-функции, которых на сервере нет.** Пользователь с Premium видит кнопки расшифровки голосовых, чек-листов и т. п.: клиент включает их по флагу `premium`, ключа для них нет. Сервер отвечает ошибкой (ADR 0016).
- **`help.getPremiumPromo`** — заглушка апстрима, отвечает ошибкой. Экран Premium остаётся без видео и описаний с сервера.
