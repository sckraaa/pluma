# 071. Premium и бусты для всех

- Статус: done на сервере и в патчах клиентов; ждёт общего прогона пакета (e2e, `-race`, `make lint`) и проверки в клиентах.
- ADR: 0041 (заменяет ADR 0016 в части того, кому доступны функции).

## Цель
Все функции Premium и все функции, требующие бустов, доступны всем пользователям; лимиты премиальные, файлы до 4 ГБ; экранов покупки нет.

## Как сделано и почему
**Один источник — `ImmutableUser.Premium()`** (`third_party/teamgram-proto/mtproto/immutable_user_helper.go`): `!m.IsBot()`, поле `users.premium` больше не читается. Поэтому не нужны ни миграция, ни сброс кэша пользователей: в Redis и старых сессиях значение игнорируется, решает метод. Флаг получают и своя карточка, и чужие (`ToSelfUser`, `ToUnsafeUser`, `ToUser`).

**Почему флаг всем, а не только себе.** Рассмотрен вариант «только себе»: звезда у чужих исчезла бы без патчей. Он неполон: клиенты читают флаг собеседника для самих функций:
- Android: анимация аватарки (`ImageReceiver.java:452`, `ImageLocation.java:172`), ссылки в био (`ProfileActivity.java:13618`), `VectorAvatarThumbDrawable`;
- Web K: анимация в списке (`avatarNew.tsx:662`), ссылки в био (`peerProfile.tsx:922`);
- tdesktop: разметка био (`info_profile_values.cpp:245`).

Android на `aac1efcb`, iOS `6ad963e5`, tdesktop `a1e89e1`, tweb `346844bf`; строки ниже — по этим ревизиям.

**`premium_purchase_blocked = true` остаётся**: он скрывает покупку. Важно, как он сочетается с флагом:
- Android `premiumFeaturesBlocked() = premiumLocked && !isPremium()` (`MessagesController.java:739`) — с флагом ложно, функции открываются; `premiumPurchaseBlocked()` (`:742`) — покупка остаётся скрытой.
- Web K `isPremiumFeaturesHidden = blocked && !premium` (`apiManagerProxy.ts:1374`).
- tdesktop `premiumPossible = premium() || premiumCanBuy()` (`main_session.cpp:349`), `premiumBadgesShown = supportMode() || premiumPossible()` (`:353`).
- iOS `isPremiumDisabled` — только конфиг, без учёта флага (`Premium.swift:282`), поэтому звезда там уже скрыта, а часть функций спрятана (ниже).

**`app_config.json`.** Все `*_default`, у которых есть `*_premium`, равны премиальным; `upload_max_fileparts_default = 8000` (4 ГБ, dfs и так принимает 8000, `MaxFileParts`). Добавлены `channel_*_level_min` и `group_*_level_min` со значением 0 (15 ключей, разбор: Android `MessagesController.java:4208-4362`, tdesktop `data_premium_limits.cpp:255-311`, iOS `Premium.swift:295` — `channel_profile_color_level_min` читает только он).

**`premium.getBoostsStatus` отвечает уровнем 0.** Сначала здесь был `level 100`, чтобы открыть свои реакции канала без правок клиентов (ключа у них нет: клиенты разрешают столько, сколько уровней), но уровень стал виден на экранах бустов. Решение владельца: бустов для пользователя Pluma нет, правим клиенты (A-110, I-110, D-085, W-069). Сервер по-прежнему принимает до `reactions.InChatMax` своих реакций и уровня не смотрит. `next_level_boosts` = 1: без него Android считает канал на максимальном уровне (`ChannelBoostsController.java:73`).

## Что убрано на сервере
- Проверки: `schedule_repeat_period` (`scheduled.go`, отправка и правка), `messages.getOutboxReadDate` (ветка без Premium), подпись 4096 (`caption.go`), `VOICE_MESSAGES` и `normalizeRules`, `new_noncontact_peers_require_premium` (`setGlobalPrivacySettings`, `getGlobalPrivacySettings`), био 140 (`about.go`), `for_both` (`wallpapers.go`), части файлов (`upload.saveBigFilePart`, `premium.go` удалён), папки и теги и «Все чаты» в любом месте (`filters.go`, `toggleDialogFilterTags`, `updateDialogFiltersOrder`, `dialog_filter.go`), `chatlist.go`, сохранённые GIF, лимиты каналов и публичных каналов (`pkg/premium/limits.go` — только константы).
- `RequiredToContact`, `MissingInvitee` и `viewerAllows` в `ImmutableUser` оставлены как есть: они читают единый `Premium()`, а правило «бот без Premium» в них остаётся (`privacy_rules` не трогаем).
- **Панель и CLI.** Удалены `pluma-admin premium grant/revoke/list`, страница Premium панели, `/api/premium`, поиск аккаунта и список, `docs/ops/premium.md`. Воркер истечения Premium удалён, `dao.ChangePremium` и смежное удалены.
- **Нейтрализовано:** RPC `user.updatePremium` — обработчик отвечает `ErrEnterpriseIsBlocked` (так принято для не обслуживаемых методов сервиса). Схему и сгенерированный код не трогал, чтобы не перегенерировать biz user параллельно другим веткам; удаление метода целиком — отдельной уборкой.

## Клиенты
Патчи — по `ci/patch.py` каждого клиента, ветки `task/071-premium-for-everyone` от `origin/main`, не запушены; реестр `clients/patches/*.md`.

| Клиент | Ветка/коммит | Что меняем |
|---|---|---|
| Android (`~/pluma-work/wt-071-android`, `7d0df1b`) | A-109 | Звезды: `isPremiumUser` возвращает `false` (9 мест рисуют звезду через него), прямые `user.premium` в `SettingsActivity:1067`, `ChatActivity:19346`, `ChatMessageCell:19698`, `GroupCallUserCell:486`, `StatusBadgeComponent:48`, `TableView:234`; `ImageReceiver`/`ImageLocation` переведены на `user.premium`, чтобы анимация аватарок осталась. Строки Premium и Business: `SettingsActivity:703, 730`, `ProfileActivity:10621, 10631` читают `premiumPurchaseBlocked`. Кнопка расшифровки: `useTranscribeButton` выключен. |
| iOS (`wt-071-ios`, `091c206`) | I-109 | Звезда уже скрыта через `isPremiumDisabled` у всех строк. Строки Premium и Business: `PeerInfoSettingsItems.swift:254, 293`, убрано `\|\| context.isPremium`. Клавиатура: `isPremiumDisabled = false` в `EmojiPagerContentSignals.swift` (72, 1644, 2211), иначе премиум-эмодзи вычёркиваются всегда. Кнопка расшифровки выключена (`ChatMessageInteractiveFileNode`, `ChatMessageInteractiveInstantVideoNode`). |
| Desktop (`wt-071-desktop`, `eb3a112`) | D-084 | Звезда: `premiumBadgesShown` → только поддержка (`Badge::setContent` снимает `BadgeType::Premium`). Раздел Premium: `BuildPremiumSection` по `premiumCanBuy`. Ссылка «статус» в меню скрыта. Расшифровка выключена (`history_view_document.cpp`). |
| Web K (`wt-071-web`, `b34d7f8`) | W-068 | Звезда: `generateTitleIcons.ts:85`. Кнопка эмодзи-статуса в шапке (`sidebarLeft/index.ts:343`). Расшифровка голосовых (`bubbles.ts:9440, 9914`). Строка Premium в настройках уже скрыта (`isPremiumPurchaseBlocked`, `settings.tsx:349`). |

Патчи стикеров (058) и тегов (053) оставлены: они избыточны при флаге, но безвредны; убрать можно одной уборкой.

Тесты патч-скриптов зелёные: Android 61, iOS 63, Desktop 55, Web 38; `PremiumForEveryoneTest` проверяет применение, `verify` и падение при изменении апстрима. Якоря сверены с закреплёнными апстримами в `reference-src`; сборки клиентов не запускались.

## Осталось поправить в патчах клиентов
Найдено при проверке ветки, в ветках клиентов этого ещё нет:
- **iOS, панель реакций.** `ChatControllerOpenMessageContextMenu.swift:187`: `areCustomEmojiEnabled: !premiumConfiguration.isPremiumDisabled`. При `premium_purchase_blocked = true` своих эмодзи в расширенной панели реакций нет, хотя Premium есть. Нужна правка в `PREMIUM_FOR_EVERYONE_EDITS`: `areCustomEmojiEnabled: true`.
- **Android, поиск в настройках.** `ProfileActivity.java:14768-14774` (`isPremiumFeatureAvailable`): с флагом условие истинно, в поиске появляются «Telegram Premium» и пункты экрана покупки (`:14646-14659`). Нужна проверка `premiumPurchaseBlocked()`.

## Бусты в клиентах
Уровень нигде не сравнивается, входы в бусты скрыты; записи A-110, I-110, D-085, W-069. Android: `b39e60b`, iOS: `28d5533`, Desktop: `a1b03bc`, Web K: `18482a0`. Цвета из `help.getPeerColors` приходят с `channel_min_level`, поэтому правка на клиентах: `getLvl`, `requiredBoostSubjectLevel`, `requiredLevelFor` возвращают 0. Web K выбора цвета канала и лимита своих реакций по уровню не имеет.

## Что клиенты теперь предлагают, а сервер не умеет
С флагом открылись входы в функции, которых на сервере нет (`docs/project/api-coverage.md`). Часть скрыта патчем, остальное нужно делать отдельными задачами:
- **Скрыто патчем:** расшифровка голосовых (`messages.transcribeAudio`), установка эмодзи-статуса (`account.updateEmojiStatus`) на Desktop и Web; на Android и iOS вход исчезает вместе со звездой (`DialogsActivity`, `ProfileActivity`), на iOS — `isPremiumDisabled`.
- **Осталось видно:** цвет имени и профиля (`account.updateColor`, `channels.updateColor`) — строки видны и раньше; эмодзи-статус канала (`channels.updateEmojiStatus`) и наборы группы (`channels.setEmojiStickers`, `channels.setStickers`) — теперь открыты уровнями 0; перевод (`messages.translateText`) закрыт `translations_*_enabled: disabled`; Business (все `account.updateBusiness*`) закрыт строкой настроек; сообщения-эффекты — ответ сервера не проверялся.
- Эмодзи-статусы и кастомные эмодзи чужих пользователей показывать нечего: статус никто выставить не может.

## Проверка
На CachyOS: `go build ./...`, `go vet -tags integration,e2e -unreachable=false` затронутых пакетов (`test/e2e` компилируется), `go test` затронутых пакетов (бфф приватности, профили, папки, каталог, файлы, biz dialog, biz user, `pkg/premium`, `tools/pluma-admin`, `app/ops/admin`, `third_party/teamgram-proto/mtproto`) — зелёные. Падающий без изменения тест — `TestUserDataRequirePremium` проверяет `Premium() == !IsBot()` при сохранённом `Premium:false`; другая часть — `pkg/premium/app_config_test.go` (каждый `_default` равен `_premium`, все `*_level_min` равны 0).

Не проверено и ждёт общего прогона: интеграционные тесты `dialog`, `msg`, `user`; новый `TestPremiumForEveryone` и правки e2e `TestDialogFilters` не запускались; `make lint`; `-race`; проверка в клиентах.

**Не проверено:** как клиенты узнают о флаге у себя без перезапуска (рассылка `updateUser` при выкатке не делается). Android читает своего пользователя при каждом запросе с `self` (`UserConfig.setCurrentUser`), но что именно приходит при старте — не прослеживал; достаточно перезапустить клиент.

## Решения, требующие подтверждения
- «Написать могут только контакты и Premium»: опция остаётся, но сервер считает Premium всех людей, поэтому никого не отсекает. Убрать её из клиентов — патчи.
- Боты считаются без Premium (`!IsBot`), как в Telegram; пользователей `bot` с флагом клиенты никак не показывают.
- Значения `stickers_faved_limit_premium: 5`, `recommended_channels_limit_premium: 10` не менялись: `messages.faveSticker` сервер не обслуживает, рекомендаций каналов нет. По умолчанию у клиентов они больше: Android 200 и 100 (`MessagesController.java:1643,1758`), tdesktop 10 и 100 (`data_premium_limits.cpp:35,59`).
- Клиенты до обновления с патчами 071 при уровне 0 будут просить буст для своих реакций и цветов канала: выкатывать сервер вместе с клиентами.

## Результат
Premium и бусты открыты всем одним методом `ImmutableUser.Premium()`; гейтов нет, лимиты премиальные, файлы до 4 ГБ; выдача Premium убрана из `pluma-admin` и панели, воркер истечения удалён; RPC `user.updatePremium` нейтрализован. Клиентские патчи (звезда, строки покупки, расшифровка, статус, отсутствие бустов) готовы, `premium.getBoostsStatus` отвечает уровнем 0, ветки локальные, в клиентах со скриншотами не проверены. Функции Premium, которых нет на сервере (цвета, эмодзи-статус, транскрипция, перевод, Business), перечислены выше и требуют отдельных задач.
