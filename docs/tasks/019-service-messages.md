# 019. Служебные сообщения как у Telegram

- Статус: todo (тексты собраны; новый вход у Telegram проверен по документации, коду клиентов и входу владельца официальным клиентом 2026-10-29)
- Зависит от: 017
- ADR: 0020 (журнал `seq`, доставка обновлений)

## Цель
Аккаунт «Pluma» (777000) пишет пользователю те же сообщения, что аккаунт Telegram: те же тексты, разметку и поводы, на языке получателя. «Telegram» в текстах заменяется на «Pluma». Решение владельца от 2026-10-29: тексты берутся у Telegram в точности, с переводами, и только свежие.

## Как помечены утверждения
«[проверено: …]» — источник прочитан 2026-10-29, ссылка рядом. «[не проверено]» — гипотеза, на неё нельзя опираться в коде. Клиенты — те версии, из которых собираются наши: Desktop v7.0.9, Android 12.9.1, iOS 12.9.2 (слой 228); код читался по закреплённым коммитам из `upstream.env` клиентских репозиториев.

## Что сейчас
| Сообщение | Где | Проблемы |
|---|---|---|
| Код входа (`auth.sendCode`, код «в приложение») | `app/bff/authorization/internal/core/core.go`, `signInMessageTpl`; хранится в чате | Только английский; нет `❗️` перед «This code…» |
| Новый вход (`auth.signIn`) | `MakeSignInServiceNotification` в `third_party/teamgram-proto/mtproto/service_notification_helper.go`; `updateServiceNotification` через `sync.updatesNotMe` | Нет пробела после «New login.»; дата в формате Go (`2026-10-29 13:21:12.547466005 +0000 UTC`); в месте есть IP; только английский; доходит только до онлайн-сессий |
| Запрос удаления аккаунта без пароля | `account.deleteAccount_handler.go`, `notifyDeletionWait` | Текст свой, не Telegram: настоящий не найден ни на одном языке |

Где ещё завершается вход: `auth.checkPassword` (вход с облачным паролем) и QR (`auth.importLoginToken`, `auth.acceptLoginToken`). Шлёт ли там «Новый вход» наш сервер — проверить; шлёт ли Telegram — [не проверено].

Неподтверждённой сессии у нас нет [проверено по коду 2026-10-29]: `updateNewAuthorization` сервер не шлёт; `account.changeAuthorizationSettings` — заглушка `ErrEnterpriseIsBlocked`; `authorization_autoconfirm_period` в конфиге нет; `account.getAuthorizations` не ставит `unconfirmed` и `password_pending`; проверка «сессия моложе суток» есть в `auth.resetAuthorizations`, но не в `account.resetAuthorization`.

Telegram шлёт и то, чего у нас нет: «Незавершенная попытка входа» (код введён, пароль нет), «Изменены настройки двухэтапной аутентификации», «Отключена двухэтапная аутентификация».

## Тексты
Источники (вне репозитория, `~/pluma-work/rebrand-research/`):
- `service-texts-export.md` — шаблоны из экспорта чата официальным Telegram Desktop (владелец, 2026-10-29; разбор — `export_texts.py` в рабочей папке сессии);
- `service-texts.md` — веб-поиск со ссылками;
- `service-texts-telegram.md` — захват `tglogin -qr -service-texts FILE` (`docs/ops/testing.md`).

Известно:
- **Код входа, английский** [проверено: экспорт, октябрь 2026]: `**Login code:** {code}. Do **not** give this code to anyone, even if they say they are from Telegram!` + `\n\n❗️This code can be used to log in to your Telegram account. We never ask it for anything else.` + `\n\nIf you didn't request this code by trying to log in on another device, simply ignore this message.`
- **Код входа, русский** [проверено: экспорт, апрель 2026]: `**Код для входа в Telegram:** {code}. Не давайте код **никому**, даже если его требуют от имени Telegram!\n\n❗️Этот код используется для входа в Ваш аккаунт в Telegram. Он не может быть использован для чего-либо ещё. \n\nЕсли Вы не запрашивали код для входа, проигнорируйте это сообщение.` (пробел после «ещё.» — в оригинале).
- **Код входа с почтой для входа** [проверено: экспорт 2026]: меняется заголовок («**Код для входа в Ваш аккаунт Telegram:**»), добавляется абзац («Мы также отправили код по электронной почте на Ваш адрес {email}. Если у Вас нет доступа к этому адресу, Вы можете изменить его в __Настройках > Конфиденциальность > Почта для входа__.»), меняется последний абзац («…для входа в аккаунт на другом устройстве…»). По-английски: «This code was also sent to your email {email}. If you do not have access to this email, you can change it in __Settings > Privacy & Security > Login Email__.»
- Спойлера на коде нет [проверено: экспорт, октябрь 2026].
- «Incomplete login attempt» (EN, 06.2026), «Незавершенная попытка входа» (RU, 08.2026), «Two-Step Verification settings changed» (EN, 05.2026) [проверено: экспорт].
- Язык одного аккаунта менялся от сообщения к сообщению: в октябре 2026 код пришёл по-английски, в апреле — по-русски [проверено: экспорт]. Почему — [не проверено].
- **Новый вход, английский** [проверено]:
  - полный текст 2025-11-16 — [cloudflare/har-sanitizer#63](https://github.com/cloudflare/har-sanitizer/issues/63): `New login. Dear {name}, we detected a login into your account from a new device on 16/11/2025 at 11:05:12 UTC.\n\nDevice: Telegram Android, 12.1.1, Samsung Galaxy Note8, Android, 13 (33)\nLocation: {city}, India\n\nIf this wasn't you, you can terminate that session in Settings > Devices (or Privacy & Security > Active Sessions).\n\nIf you think that somebody logged in to your account against your will, you can enable Two-Step Verification in Privacy and Security settings.` (разметки в issue нет);
  - жирный шрифт — `New login.`, `Settings > Devices`, `Privacy & Security > Active Sessions`, `Two-Step Verification` — [miranda-ng/miranda-ng#3958](https://github.com/miranda-ng/miranda-ng/issues/3958), 2023-11-23; тогда в месте ещё было `(IP = …)`;
  - первые два абзаца 2026-03-26, снимок экрана и расшифровка — [блог abby-exe](https://github.com/abby-exe/abby-exe.github.io/blob/HEAD/_posts/2026-03-27-tngosint.md): место — только страна, IP нет.
  - 2026-10-29, аккаунт владельца с облачным паролем [проверено: снимок экрана, `~/pluma-work/rebrand-research/new-login/`]: те же три абзаца, **без** абзаца про Two-Step Verification. Это согласуется с гипотезой «этот абзац — только без облачного пароля», но аккаунт без пароля не проверялся.
- **Новый вход, русский** [проверено: снимок экрана 2026-10-29; побайтно — нет: неразрывные пробелы и точные границы сущностей по снимку не видны]: `**Вход с нового устройства.** {name}, мы обнаружили вход в Ваш аккаунт с нового устройства {DD/MM/YYYY} в {HH:MM:SS} UTC.\n\nУстройство: {device}\nМесто входа: {location}\n\nЕсли это были не Вы, как можно скорее перейдите в **Настройки > Устройства** (или **Конфиденциальность > Активные сеансы**) и завершите новый сеанс.` Обращения «Уважаемый» нет, имя сразу в начале.
- Устройство в тексте [проверено, два образца]: `Telegram Web, 12.0.48 A, Safari 26, macOS` (2026-10-29) и `Telegram Android, 12.1.1, Samsung Galaxy Note8, Android, 13 (33)` (2025-11) — приложение, версия, модель и система через запятую; какие поля `authorization` за этим стоят у веба, где «Android, 13 (33)» превращается в «macOS», — [не проверено].
- **Не хватает:** остальных языков. Telegram сам прислал английский текст телефону с чешским интерфейсом (ниже), так что переводы есть не для всех языков клиентов.

«Нового входа» в экспорте нет, и это ожидаемо [проверено по коду клиентов]: сообщение приходит как `updateServiceNotification`, и клиент сам создаёт из него сообщение в чате 777000 у себя:
- Android — только при `inbox_date` (`MessagesController.java`, ветка `TL_updateServiceNotification`);
- iOS — только при `inbox_date` (`TelegramCore/Sources/State/AccountStateManagementUtils.swift`);
- Desktop — для любого не-`popup` (`api/api_updates.cpp`; после этого перезагружает список сессий).

Поэтому тексты снимаются с экрана официального приложения, которое получило уведомление, без стороннего клиента (так снят русский, 2026-10-29).

## Новый вход у Telegram
Проверено:
- **Сообщение «New login» Telegram шлёт и в 2025–2026 годах**: образцы 2025-11-16 и 2026-03-26 (выше). Во втором случае — сразу после входа злоумышленника по выманенному коду со стороннего приложения.
- **Отдельно от сообщения есть баннер неподтверждённого входа** ([core.telegram.org/api/auth](https://core.telegram.org/api/auth#confirming-login), раздел «Confirming login»):
  - при входе другие сессии получают `updateNewAuthorization`;
  - с флагом `unconfirmed` клиент спрашивает, узнаёт ли пользователь вход;
  - «Да» → `account.changeAuthorizationSettings` с `confirmed`, «Нет» → `account.resetAuthorization`;
  - без ответа сессия подтверждается сама через `authorization_autoconfirm_period` секунд после входа ([api/config](https://core.telegram.org/api/config): «An unconfirmed session will be autoconfirmed this many seconds after login», в примере 604800).
- Флаг `unconfirmed` есть и у `authorization` в `account.getAuthorizations` (схема слоя 228, [constructor/authorization](https://core.telegram.org/constructor/authorization)). Ни один из трёх клиентов его в списке сессий не читает: Desktop читает только `updateNewAuthorization` (`api_authorizations.cpp`), Android `SessionsActivity` — только `password_pending`.
- Все три клиента делают одно и то же: `updateNewAuthorization` с `unconfirmed` добавляет вход в баннер (устройство, место, дата из обновления); то же обновление **без** `unconfirmed` убирает его — так сервер снимает баннер на остальных устройствах после подтверждения. Срок по умолчанию 604800 с (Desktop `api_authorizations.cpp`, Android `MessagesController.java`, iOS `SyncCore_NewSessionReview.swift`). Источники: Android `UnconfirmedAuthController.java`, iOS `AccountStateManagementUtils.swift`.
- Тексты баннера — в клиентах, сервер их не шлёт: Desktop `lng_unconfirmed_auth_*`, Android `UnconfirmedAuth*` («Someone just got access to your messages!», «Yes, it’s me», «No, it’s not me!», «New Login Prevented»…). Значит, для нас это ребрендинг клиентов (017), а не задача сервера.
- Баннер появился в Android 10.0.8 (коммит 2023-09-13), в Desktop — 2025-08-26 («Added api support of new unconfirmed authorizations») [проверено: история файлов на GitHub].
- Сессия моложе суток не может завершать другие: 406 `FRESH_RESET_AUTHORISATION_FORBIDDEN` ([method/account.resetAuthorization](https://core.telegram.org/method/account.resetAuthorization)); iOS обрабатывает его отдельно (`RecentAccountSessions.swift`, `.freshReset`).
- «Incomplete login attempts» в «Устройствах» Android — сессии с `password_pending`; пояснение клиента: «The code was entered correctly, but no correct password was given» (`SessionsActivity.java`, `strings.xml`).

Опыт владельца 2026-10-29 [проверено: снимки экрана в `~/pluma-work/rebrand-research/new-login/`]. Два входа официальным Telegram Web A 12.0.48 (Safari 26, macOS) по QR с облачным паролем; телефон — официальный Telegram Android 13.1.0:
- После каждого входа телефон получил **и** сообщение «Новый вход» в чате «Telegram», **и** баннер неподтверждённого входа. Значит, и вход по QR, и вход с паролем дают оба; сессия, одобренная с самого телефона (QR), всё равно неподтверждённая.
- Язык сообщения: интерфейс телефона на чешском (16:44 UTC) — английский текст; на русском (16:46 UTC) — русский. Владелец менял язык только в приложении на телефоне, и **все** остальные устройства получали «Новый вход» на языке телефона в этот момент [проверено владельцем]: один текст на всех, на языке сессии, подтвердившей вход (QR), а не новой и не каждой получающей на своём. Наблюдение 2023 года ([miranda-ng#3958](https://github.com/miranda-ng/miranda-ng/issues/3958): две сессии — английский и русский, каждая на своём) этому противоречит; видимо, правило с тех пор поменялось. Чешского текста у Telegram, похоже, нет: отдан английский.
- Баннер на языке интерфейса телефона; место — «Prague, Czechia», без IP (в примере документации было «(IP=…)»), устройство — «Safari 26» (модель из `authorization`).
- До ответа карточка сессии в «Устройствах» ничем не отличается: приложение, место по IP, «Принимать звонки», «Завершить сеанс».
- «Да, это я» — уведомление «Вход разрешён. Список активных сеансов доступен в разделе Настройки > Устройства.»; «Нет, не я!» — окно «Попытка входа заблокирована. Мы заблокировали попытку входа в аккаунт с Safari 26, Prague, Czechia.» с предупреждением про код и кнопкой «OK» с обратным отсчётом. Оба текста — клиентские (`UnconfirmedAuthConfirmed*`, `UnconfirmedAuthDenied*` в Android).

Ответы владельца после опыта (2026-10-29):
- сама новая сессия «Новый вход» не получает: в чате «Telegram» в браузере его нет [проверено];
- вход по коду даёт то же: баннер есть, сообщение того же вида [проверено];
- сразу подтверждённого нового входа владелец не встречал [не проверено, мнение владельца]; делаем неподтверждённым каждый.

Не проверено:
- что ещё запрещено неподтверждённой сессии (документация называет только 24 часа для сброса сессий, и это ограничение не про неподтверждённость);

Прежний вывод этого брифа («в 2026 году вместо сообщения — баннер») снят: он опирался на один вход, и образец 2026-03 и опыт 2026-10-29 ему противоречат — Telegram шлёт и то, и другое.

## Вход стороннего клиента в аккаунт владельца (2026-10-29)
Проверено:
- Журнал `tglogin`, 15:00 и 15:08 UTC: оба раза Telegram принял пароль (`auth.checkPassword` вернул `auth.authorization`), через 20–50 с каждый запрос получал 401 `AUTH_KEY_UNREGISTERED` — ключ снял сервер. Наш клиент за это время слал только `msgs_ack`, `updates.getState` и `messages.getHistory`.
- Разработчик TDLib о том же симптоме ([tdlib/td#3044](https://github.com/tdlib/td/issues/3044), 2024-09): «The server decided that your account was stolen. There is no way to avoid this from the app side», «There is no way to find the cause». Telegram ([api/obtaining_api_id](https://core.telegram.org/api/obtaining_api_id)): аккаунты, вошедшие через неофициальные клиенты, «automatically put under observation».

Не проверено:
- почему телефон показывал обе сессии в «Incomplete login attempts», хотя пароль был принят: экран загружен до ввода пароля, сервер так показывает снятые сессии, или что-то третье;
- что именно вызвало снятие ключа. Предположения разбора (`~/pluma-work/rebrand-research/mtproto-login-compare.md`): сторонний `api_id`, серия новых ключей, брошенные на пароле попытки, чтение чата 777000 сразу после входа, VPN.

Выводы:
- не входить сторонним клиентом в аккаунт владельца без владельца и чаще раза в сутки;
- тексты брать из официальных приложений (экспорт, снимки экрана), не сторонним входом;
- `tglogin` стоит поправить: `auth.logOut` для брошенной на пароле попытки, повтор пароля на том же ключе, пинги `ping_delay_disconnect`, после входа те же запросы, что у TDLib. Если вход переписывать — через gotd/td (Go, поддерживается).

## Решения
- Тексты — Telegram в точности, «Telegram» → «Pluma». Языки без подтверждённого текста получают английский; свои переводы не пишем. Так делает и Telegram: чешскому интерфейсу пришёл английский (2026-10-29). Поэтому переводы собираем только для языков пользователей Pluma — сейчас русский и английский, оба есть; «все переводы Telegram» не нужны.
- Язык:
  - «Новый вход» — один текст всем остальным сессиям, на языке (`lang_code` из `initConnection`) сессии, подтвердившей вход по QR (`auth.acceptLoginToken`); язык новой сессии не учитывается (опыт 2026-10-29). При входе по коду подтверждающей сессии нет; там язык — как у хранимых сообщений (ниже) [не проверено].
  - Хранимые сообщения (код входа) — одно на аккаунт. Язык — самой недавно активной из других сессий получателя. Это наше решение, у Telegram правило [не проверено].
- Формат [проверено: образцы 2025-11 и 2026-03]: дата и время UTC, `ДД/ММ/ГГГГ` и `ЧЧ:ММ:СС`; место — «Город, Страна» или страна, без IP; устройство — «приложение, версия, модель, платформа, версия ОС», как в `account.getAuthorizations`.
- Неподтверждённая сессия — как в документации Telegram (выше). Неподтверждённым становится каждый новый вход, и по QR тоже, и одновременно уходит «Новый вход» (опыт 2026-10-29).
- Вне задачи: языковые пакеты клиентов (`langpack.*`, у нас все пять методов — «missing»). Старая попытка владельца (`sckraaa/tg_self_host`, `self_hosted_version/scripts/fetch-all-langpacks.cjs`) брала их с `translations.telegram.org/{lang}/{pack}/export` для ru и cs; серверных текстов там нет.

## Шаги
1. ~~Опыт владельца с официальным клиентом~~ — сделан 2026-10-29 (выше).
2. Таблица шаблонов в коде: язык → текст и сущности (en, ru).
3. authsession: метод, который отдаёт сессии пользователя с `lang_code` и временем активности (`authorization.hash` — случайное число, не ключ). Код сервиса генерируется из `authsession.tl.proto`, как в `000a122e`.
4. sync: доставка служебного уведомления всем сессиям, кроме новой, и офлайн-сессиям тоже. Сейчас `sync.updatesNotMe` шлёт только онлайн-сессиям, а `sync.pushUpdatesIfNot` не реализован.
5. Неподтверждённая сессия: флаг `unconfirmed` в `account.getAuthorizations`; `updateNewAuthorization` другим сессиям (с флагом при входе, без флага при подтверждении); `account.changeAuthorizationSettings` с `confirmed`; `authorization_autoconfirm_period` в `help.getAppConfig` и автоподтверждение; `FRESH_RESET_AUTHORISATION_FORBIDDEN` и в `account.resetAuthorization`.
6. Код входа и «Новый вход» по шаблонам — везде, где вход завершается: `auth.signIn`, `auth.checkPassword`, вход по QR. «Новый вход» и `updateNewAuthorization` — всем сессиям, кроме новой.
7. Тесты: шаблоны и сущности (смещения в UTF-16), выбор языка, доставка (интеграционные), e2e: вход по QR, подтверждённый сессией на русском, — все остальные сессии, в том числе на английском, получают русский «Новый вход»; баннер: вход → `updateNewAuthorization` с `unconfirmed` → подтверждение → то же без флага.
8. Позже, отдельно: «Незавершенная попытка входа» и сообщения о 2FA.

## Критерии приёмки
- Сравнение с текстами Telegram: тексты и сущности совпадают побайтно, кроме «Telegram» → «Pluma» и подставленных значений.
- Вход по QR, подтверждённый сессией с `lang_code=ru`, даёт всем остальным сессиям русский «Новый вход»; с `en` и с языком без текста — английский.
- Офлайн-сессия получает «Новый вход» после подключения.
- Официальные клиенты показывают баннер нового входа, «Да» и «Нет» работают, баннер снимается на всех устройствах после подтверждения.

## Результат
_—_
