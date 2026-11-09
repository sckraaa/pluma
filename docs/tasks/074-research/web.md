# web (morethanwords/tweb, Web K): обновление до нового upstream

Текущая: 2.2 (676), layer 228, commit 346844bf8c002bc1af4de20e78ae1d0df0265019 (2026-09-21). Новейшая: 2.3 (686), master aa74832ed1976008d1ad1859d0b0cd671e75a7bd, commit "Build" 2026-11-08 (тегов и GitHub Releases нет). Слой: 233.

## Итог
Новейшая сборка — `aa74832e`, версия 2.3 (686), layer 233. Сломано 35 якорей из 238 (15%). **Одно веб-обновление невозможно: сначала нужен новый layer на сервере** (ADR 0008: сервер и клиенты переходят вместе). Опубликованный `teamgram/proto` доходит только до v0.231.0, поэтому для 233 нужна своя кодогенерация TL.

## Версия
- `git ls-remote` morethanwords/tweb: 13 веток, 0 тегов, значит, GitHub Releases тоже нет. Отдельного стабильного канала нет, релизы — коммиты «Build» в master, которые меняют `public/version`.
- HEAD master: `aa74832ed1976008d1ad1859d0b0cd671e75a7bd`, 2026-11-08, `public/version` = `2.3 (686)`.
- Закреплённый `346844bf` — предок HEAD, разрыв 486 коммитов. Следующий за ним `8197da75` действительно «Layer 229», то есть пин — последний коммит на 228 (подтверждено).
- Изменение `src`: 2891 файл, +331508/−29681.
- Layer читан из `src/lib/mtproto/schema.ts` и `src/scripts/in/schema.json`: 233. Коммиты смены layer:

| Layer | Коммит | Дата | Версия на коммите |
|---|---|---|---|
| 229 | `8197da75` | 2026-09-22 | |
| 230 | `f4496036` | 2026-10-30 | 2.2 (683) |
| 232 (231 в tweb не было) | `47ad6853` | 2026-10-31 | |
| 233 | `1388e5d1` | 2026-11-07 | |

## Что нового для пользователей с пина
Источник — заголовки коммитов и `CHANGELOG.md`. Раздел `2.3 (685)` — обзор «за всё время», многое в нём было и до пина (аккаунты и `lib/passcode` есть в `346844bf`), поэтому опираюсь на коммиты.
- **Rich-сообщения и статьи:** новый композитор на tiptap, кнопки layer 229, сворачиваемые цитаты (`36d1db91`, `b3094b65`, `c1205157`).
- **Кошелёк Gram/TON:** баланс, переводы, TON Connect (`f4496036`). Пункт меню показывается только при `wallet_available` в appConfig (`sidebarLeft/index.ts:795`).
- **HTML-виджеты ботов в чате** (`1388e5d1`).
- **Звонки:** восстановление вместо обрыва, шумоподавление RNNoise (`8f89c7d6`, `e43493d0`).
- **Просмотр медиа:** удержание для быстрого просмотра, лента альбома (`27cd9a05`, `483cab55`).
- **Прочее:** режим клавиатуры и экранного чтения за `?a11y=1` (`a3f94ab7`), уровень «меньше анимаций» (`688c0b60`), новые иконки (`a10152a2`), QR как у Android (`787b7f0c`), обложки музыки и копирование статей с форматированием (`CHANGELOG.md`, Unreleased).

## Стоимость патча
**Метод.** Одноразовый скрипт вне форка, форк не менялся. Измерен `ci/patch.py` из `origin/main` `5233f00` (включает W-067). Деревья `git archive` без файлов `.env*`; `config/app.env` не читался, подставлены фиктивные значения и случайный ключ. Каждая ошибка записывается, и прогон продолжается.
- Контроль на пине: 238 якорей, 0 сломано, `verify` проходит.
- Якорь — это одна операция replace/sub/expect/replace_file/replace_body/replace_block, каждая замена в replace_within, 2 манифеста и 2 счётчика строк.
- Результат на `aa74832e`: **203 совпали, 35 сломаны**.
- Ветка `task/071-premium-for-everyone` (`18482a0`, не влита): 246 якорей, те же 35 сломаны, её 8 новых совпадают.
- Из-за раннего выхода `identity_colors` не выполнились три её замены: две строки в `getPeerColorById.ts` на месте, третья — та же сломанная строка `DialogColorsFg` (считается одним якорем с уже учтённым сбоем).

**Сломанные по реестру (35):**

| ID | Сколько | Причина |
|---|---|---|
| W-065 теги Saved | 9 | импорт `showPremiumPopup` заменён на `hotReloadGuard.showPremiumPopup`, все гейты переписаны под него |
| W-036 белое на акценте | 5 | `_button`, `_row`, `_chat` (`.btn-send`), `_replyKeyboard`, `starGiftValue` |
| W-066 ссылки pluma.chat | 4 | код уехал в `helpers/copyContact.ts`, `popups/myQrCode/link.ts`, `utils/stories/storyLink.ts` |
| W-026 цвета тем | 3 | `link-color` убран из `colorMap` day/night (одна из трёх — комментарий, следствие) |
| W-043, W-045 баннер дня рождения | 2 | |
| W-039, W-040 цвета-идентичности | 2 | `DialogColors` вынесен в `dialogColors.ts`, список `Fg` — отдельная константа |
| W-005 | 1 | `BASE_DC_ID` теперь в `lib/accounts/authKeyFingerprint.ts:4` |
| W-019 строки | 1 | счётчик `lang.ts`: 272 против 210, 62 новые строки с Telegram читать вручную |
| W-020 | 1 | подпись версии в `getAppVersionText()`, `config/app.ts` |
| W-027 | 1 | список импорта цветов |
| W-029 | 1 | ветка `outgoingGradient` |
| W-033 | 1 | |
| W-035 | 1 | |
| W-037 | 1 | `#fff` 11 вместо 9 |
| W-048 | 1 | `toggleStickerSet` теперь принимает сам набор (`appStickersManager.ts:852`), правку, видимо, убрать; по сигнатуре, не проверено запуском |
| W-067 | 1 | изменился отступ |

Механика (новый якорь без новых решений): W-005, W-020, W-039/040, W-065, W-066, W-067, W-033, W-035, большая часть W-036. Чтение логики: W-026, W-027, W-029, W-037, W-043/045, W-019.

**Сверх якорей упадёт `verify` (на частично применённом дереве):**
- W-024: новые `tg:` в 5 файлах — `inputEditor/richMessage.ts`, `normalizeLinkUrl.ts`, `validateRichMessage.ts`, `internalLinkProcessor.ts`, `wrapRichText.ts`.
- W-066: счётчик `t.me` не сходится с `LINK_KEEP` в 15 файлах, включая `businessChatLinks.tsx`, `copyContact.ts`, `storyLink.ts`, `tonConnectWire.ts`, `widget-sandbox`.
- `DAMAGE` в `patch_test.py` обновлять вместе с якорями.
- Следов серверов Telegram (`TELEGRAM_TRACES`) не осталось, новых флагов `VITE_MTPROTO_*` нет.
- Новые внешние хосты в исходниках: только `toncenter.com`, `tonviewer.com`, `nft.fragment.com` (https); новых литералов `wss://` нет. Куда реально ходит кошелёк, не проверялось.
- Появился флаг `VITE_TEST_DC` (`config/modes.ts:111`), W-008 его не охватывает, значение в `.env` не читалось.

## Сборка
- Node и pnpm не менялись: `engines.node` `^22.18.0 || >=24.11.0`, pnpm 11.16.0, Dockerfile `node:24`. `NODE_VERSION=24.22.1` подходит.
- `.docker/Dockerfile_dependencies` теперь копирует `pnpm-workspace.yaml`, `pnpm-lock.yaml`, `patches/` (pnpm 11 требует `allowBuilds`). `ci/build.sh` ставит из полного checkout, это его не затрагивает.
- Новые зависимости при установке: tarball `@ton/wallet-engine` с github.com/i582, `link:src/vendor/widget-sandbox-quickjs`, tiptap, patched `mediabunny`/`temml`.
- `keepAsset.js` добавил `embed.html`; `ci/build.sh` берёт `keepAsset` из апстрима, поэтому файл попадёт сам.
- Сборку и `typecheck` на новом дереве не запускал: нужны исправленные якоря.

## Зависимость от сервера
Сравнение `src/scripts/in/schema.json` 228 и 233:
- Конструкторы: 17 удалены, у 18 сменился id, 113 новых.
- Методы: у 7 сменился id, 59 новых.
- Сменили id `channel`, `channelFull`, `userFull`, `messageReplyHeader`, `inputReplyToMessage`, `keyboardButton`, `replyInlineMarkup`; удалены `keyboardButtonUrl`, `keyboardButtonCallback` и другие.
- `tl_utils.ts:724-735`: неизвестный конструктор — `throw 'Constructor not found'`. Значит, против сервера 228 клиент не разберёт каналы и `userFull`, а сервер не примет ответы с новым id.
- Сервер: `go.mod` `teamgram/proto v0.228.0`. Теги proto (`git ls-remote`) заканчиваются на v0.231.0, содержимое тегов не читал.

Варианты шага: своя кодогенерация до 233, либо промежуточный web на `f4496036` (2.2 (683), layer 230) при proto v0.230.0. Коммита tweb на layer 231 нет. Новые wallet, widget и onramp могут оставаться выключенными на сервере: для кошелька есть флаг `wallet_available`, про остальные не проверено.

## Прочее
Клон: `/Users/sckr.a/pluma-work/reference-src-new/tweb` (259 МБ, `--filter=blob:none`), свободно 66 ГБ. Скрипт и JSON с результатами: `(одноразовый скрипт)`. Scratch-каталог общий с соседними агентами: первый запуск столкнулся с чужим `h/measure.py`, поэтому всё перезапущено из отдельной папки, итоговые числа только из неё. Локальная ветка `main` форка отстаёт от `origin/main`, мерил по `origin/main`.
