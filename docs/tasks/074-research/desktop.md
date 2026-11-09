# desktop: обновление до нового upstream

Текущая: v7.0.9 (a1e89e1f64f08cb058caf1c61ff43f319f98a6ec, layer 228). Новейшая: v7.4.1. Слой: 233.

## Итог
Новейший стабильный релиз tdesktop: **v7.4.1**, коммит `157f5cfc85e2dd1fa67f129a5e31a81db5ddeb19`, 2026-11-06, **layer 233**. Отдельно обновить нельзя: нужен новый layer на сервере, а `teamgram/proto` доходит только до v0.231.0. Патч ломается умеренно: 171 из 191 якоря на месте. Главная работа не в якорях, а в новой системе обновлений v2 и в Rust в сборке.

## Версия и источники
- Теги прочитаны через `git ls-remote`; последний тег — v7.4.1. Ветка `master` устарела (6.1.1, сентябрь 2025), `dev` ушла вперёд. GitHub Releases не проверял.
- Стабильность: `Telegram/SourceFiles/core/version.h:25-27` — `AppVersion = 7004001`, `AppVersionStr = "7.4.1"`, `AppBetaVersion = false`; в `changelog.txt` нет пометки beta.
- Layer из `Telegram/SourceFiles/mtproto/scheme/api.tl:3300`.

| Тег | Layer |
|---|---|
| v7.0.9 | 228 |
| v7.1.0, v7.1.5, v7.2.5, v7.2.10 (beta) | 229 |
| v7.3.0, v7.3.1 | 230 |
| v7.3.2, v7.3.3, v7.3.4, v7.3.5 | 232 |
| v7.4.0, v7.4.1 | 233 |

Промежуточные теги 7.1.1–7.1.4 и 7.2.6–7.2.9 по layer не проверял.

## Что изменилось для пользователей (`changelog.txt` v7.4.1, 7.0.9..7.4.1)
- 7.1: приветственные сообщения в группах и каналах, кнопки и файлы в rich-сообщениях, самоуничтожающиеся фото и видео, видео-аватары, видеоредактор, восстановление окон, новый тип WEB-прокси.
- 7.1.3: слэш-меню в редакторе статей, бан в личных сообщениях канала.
- 7.2–7.2.5: **система обновлений v2**, ротация и отзыв ключа подписи, новый источник манифеста.
- 7.2.6 beta: текст в редакторе фото, папки как архив, эмодзи из видеостикеров.
- 7.2.10 beta: Unicode 17.
- 7.3: **Money** (кошелёк TON). 7.3.2: ответы цитатой на rich-сообщения, уникальные подарки в Money-карте, QR-дизайн юзернеймов, **Qt 6 на Windows**.
- 7.3.3–7.3.5: разрешения камеры и микрофона в мини-приложениях, вкладки поиска, анимация отправки, много исправлений.
- 7.4: **HTML-виджеты в ботах**. 7.4.1: исправление падения при запуске на Windows.
- Объём кода 7.0.9..7.4.1: 2540 файлов, +556993/−40409 (`git diff --shortstat`).

## Стоимость патча
Метод: одноразовый скрипт `(одноразовый скрипт)`. Он исполняет `ci/patch.py` в памяти и не трогает форк (`git status` пуст); ошибки записывает и идёт дальше. Значения идентичности и ключи подставлены фиктивные, `config/app.env` и PEM-файлы не читались. Подмодули cmake, lib_ui и lib_base взяты на коммитах тега.

**Контроль на закреплённом v7.0.9: 191 из 191, 0 прочих ошибок**, то есть замер корректен. Для v7.4.1 подставлены новые версия, layer и Qt.

**Результат v7.4.1: совпало 171 из 191, сломано 20.** Из них с ожиданием «ровно один раз» совпало 153 из 168. Сборку не запускал.

| ID реестра | Сломано | Что случилось |
|---|---|---|
| D-053 | 3 | Бэкпорт MSYS2 UCRT: `b244f1b34` уже входит в v7.4.1 (проверено `merge-base`), записи удалить |
| D-057 / D-060 | 2 | `check_upstream`: в `centos_env/build.sh` добавлен `${KEEP_GOING:+-- -k 0}`; в `optionsList` первым стал `'qt5'` (`qt6` остался, `prepare.py:64`) |
| D-055 | 1 | macOS: блок `CONFIGURATIONS` / configure Qt в `prepare.py` изменился |
| D-075 | 3 | Ключи `UpdatesPublicKey` и `UpdatesPublicBetaKey` удалены из `config.h`; запуск `MtpChecker` переписан (`update_checker.cpp:1768-1772`) |
| D-077 | 1 | Сравнение теперь `versionNum <= AppVersion` (`update_checker.cpp:2116`) |
| D-042 | 1 | В `MimeType` файла `.desktop` добавлен `x-scheme-handler/ton` |
| D-070 | 2 | `dialogs_top_bar_suggestion.cpp` изменён |
| D-079 | 1 | `click_handler_types.cpp`, `LINK_HOSTS` |
| D-013 | 1 | `page.js`: `data.title` стал `data.windowTitle` (строки 947, 972) |
| D-014 | 4 | Новые `tg://`: `application.cpp` 4 вместо 3, `local_url_handlers.cpp` 35 вместо 30, `history_drag_area.cpp` 3 вместо 2, `bot_attach_web_view.cpp` 3 вместо 2 |
| D-030 | 1 | `BuildHelpSection` уже без `lng_telegram_features_url` |

**Проверки вне якорей** (21 срабатывание, 17 из них в `verify`, многие следуют из таблицы выше):
- D-012: таблица строк `brand/strings.txt` разошлась с `lang.strings` (есть новые и удалённые ключи).
- D-027: изменились темы `day-blue`, `night-green`, `day-custom-base`.
- D-013: новые литералы «Telegram» в `export_output_abstract.cpp`, `web_proxy_transport.cpp`, `launcher_linux.cpp` и других.
- D-014: новые файлы с `tg://`, не внесённые в `SCHEME_FILES` (`wallet/wallet_content.cpp` и тестовый файл).
- D-079…D-082: `verify_links` видит новые места `t.me` в `local_url_handlers.cpp`.

**Что не сломалось:** хеши 60 иконок (`ASSETS`) и ночной темы, палитра, entitlements (diff пуст). `verify` не нашёл новых адресов Telegram и новых созданий `SpecialConfigRequest` / `DomainResolver`.

**Чего якоря не покажут:**
- **D-074…D-078 (обновления) придётся делать заново.** В v7.4.1 появились `core/update_verify.cpp`, `update_keys.cpp`, `update_unpack.cpp`, манифест с ключами и подписи ES256/Ed25519 (`Telegram/build/sign_update.py`). Наш формат RSA-1024 и `ci/pack_update.py` на него не рассчитаны. Часть якорей Updater при этом совпала, но это не значит, что логика осталась прежней. Формат v2 прочитан только частично, полную стоимость не оценивал.
- Новые внешние адреса в diff, которые `verify` не ловит: `toncenter.com`, `fragment.com`, `nft.fragment.com`, `tonviewer.com`. Появились кошелёк и WEB-прокси (`mtproto/web_proxy/*`). Что с ними делать в закрытой установке, нужно решить отдельно; конкретные места вызова не разбирал.

## Сборка
- Qt 6.11.1 → **6.11.2** (`Telegram/build/qt_version.py`); на Windows Qt 6 теперь по умолчанию, Qt 5 — опцией `qt5`. `ci/windows.sh` с аргументом `qt6` формально остаётся допустимым.
- **Новое: Rust 1.96.1** (`prepare.py:598`, `centos_env/Dockerfile:999`). Новые этапы `prepare.py`: `rust`, `tdesktop_rust`, `tlottie`, `wallet`, `qt6_windows7`; убраны `libde265`, `protobuf`.
- `centos_env/Dockerfile` изменён, значит образ Linux собирать заново (по реестру 2–4 ч). База прежняя: Rocky 8, gcc-toolset-15.
- Подмодули: убран rlottie, добавлены zxcvbn, twidget, widget-sandbox; cmark-gfm переехал на `desktop-app`.
- Без изменений: Windows SDK 10.0.26100.0, MSVC 14.44 для x86/x64 (ARM64 — 14.52), ccache 4.13.6, CMake 3.25...3.31.
- Xcode: в `mac.yml` по-прежнему `macos-latest`; `prepare.py` упоминает Xcode 26.6 для legacy stackwalk. Какой Xcode сейчас стоит в образе по умолчанию — **не проверено**.
- Node в CMake, `prepare.py`, Dockerfile, основных workflow и `docs/building-*.md` не найден. NDK к Desktop не относится.

## Можно ли обновить отдельно
**Нет, сначала нужен новый layer на сервере.**
- ADR 0008: сервер и клиенты переходят вместе, одной задачей; `check_upstream` требует совпадения layer.
- Сравнение `api.tl` v7.0.9 и v7.4.1: +175 конструкторов, −18, у 24 сменился id. Среди сменивших: `channel` (d49f34c6 → 7ad15240), `channelFull`, `userFull`, `messageReplyHeader`, `replyInlineMarkup`, `keyboardButton`. Удалены все `keyboardButtonUrl`, `keyboardButtonCallback` и подобные.
- На сервере (`third_party/teamgram-proto`) есть `d49f34c6`, а `7ad15240`, `c4c4dd40`, `b90c2afd` отсутствуют, то есть клиент 233 получил бы от него незнакомые ему `channel` и подобные.
- В `lib_tl` `tl/generate_tl.py:1283` есть ветка `default: return false`. Что это именно чтение неизвестного id, по одной строке кода — **не подтверждено**.
- Новейший `teamgram/proto` — v0.231.0 (`git ls-remote`), layer 233 в нём нет. Варианты: ждать upstream, делать свою кодогенерацию (риск записан в ADR 0008) или взять v7.3.1 (layer 230, старший стабильный из проверенных тегов не выше 231). Патч на v7.3.1 не измерял. Выбор за владельцем.

## Служебное
Клон: `/Users/sckr.a/pluma-work/reference-src-new/tdesktop` (166 МБ) и `tdesktop-submodules` (3,2 МБ); весь `reference-src-new` — 2,0 ГБ вместе с уже лежавшим там. Коммитов и пушей нет. В первой команде клонирования по моей ошибке оказался лишний `rm -rf /dev/null`; его заблокировала защита, и ничего не выполнилось.
