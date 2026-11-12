# 080. Статус аккаунта поддержки: «сервисный аккаунт»

- Статус: в работе (серверная часть сделана, клиенты — отдельные ветки)
- Решение владельца: 2026-11-11. Часть задачи 080 (`080-support-account.md`).

## Что нужно
У аккаунта поддержки (`user.support`, без статуса «в сети») на месте «был(а) недавно» клиенты пишут слова **«сервисный аккаунт»** — не «поддержка» Telegram и не «Сервисные уведомления». Текст — только ключом локализации, на всех языках, которые клиент встраивает или получает с сервера.

## Ключи по клиентам
Меняется **значение существующего ключа** Telegram; новых ключей нет.

| Клиент | Пакет сервера | Ключ | Где рисуется (исходники, выпуски из `upstream.env`) |
|---|---|---|---|
| Android | `android` | `SupportStatus` | `ChatAvatarContainer.java`, `ProfileActivity.java`, `ProfileActivity2.java`, `ProfileSearchCell.java`; в `values/strings.xml` только английский, в `values-*` ключа нет |
| iOS | `ios` | `Bot.GenericSupportStatus` (`strings.Bot_GenericSupportStatus`) | `ChatTitleView.swift`, `ChatTitleComponent.swift`, `PeerInfoData.swift`, `ItemListAvatarAndNameItem.swift`, `ChatListSearchListPaneNode.swift`; встроен только `en.lproj` |
| tdesktop | `tdesktop` | `lng_status_support` | `data_peer_values.cpp`, `OnlineTextSpecial`: и `isSupport()`, и `isServiceUser()` — оба случая получают один текст |
| Web K | `webk` | `SupportStatus` | `getUserStatusString.ts`; встроен только английский (`lang.ts`) |

- Пакет `macos` не трогаем: клиента macOS у Pluma нет.
- tdesktop тем же ключом подписывает служебных пользователей (`isServiceUser()`); «сервисный аккаунт» подходит и им. Аккаунт уведомлений (`isNotificationsUser()`) идёт раньше и остаётся «service notifications».

## Таблица переводов
Одна таблица для сервера и всех клиентов (37 языков `pkg/langpack/own.json`). Строчные буквы, как у статусов Telegram; в немецком существительное с прописной. Источник истины в коде — `pkg/langpack/override.json`.

| Код | Язык | Текст | Сомнения |
|---|---|---|---|
| `ar` | арабский | حساب خدمة | проверить: без артикля, как статус |
| `be` | белорусский | сэрвісны акаўнт | проверить: орфография слова «сэрвісны» |
| `bg` | болгарский | служебен акаунт |  |
| `ca` | каталанский | compte de servei |  |
| `cs` | чешский | servisní účet |  |
| `de` | немецкий | Dienstkonto |  |
| `el` | греческий | λογαριασμός υπηρεσίας |  |
| `en` | английский | service account |  |
| `es` | испанский | cuenta de servicio |  |
| `fa` | персидский | حساب خدماتی | проверить: возможен вариант «حساب سرویس» |
| `fi` | финский | palvelutili |  |
| `fr` | французский | compte de service |  |
| `he` | иврит | חשבון שירות | проверить |
| `hr` | хорватский | servisni račun |  |
| `hu` | венгерский | szolgáltatásfiók | проверить: возможен вариант «szolgáltatási fiók» |
| `id` | индонезийский | akun layanan |  |
| `it` | итальянский | account di servizio |  |
| `kk` | казахский | қызметтік аккаунт | проверить: возможен вариант «қызметтік тіркелгі» |
| `ko` | корейский | 서비스 계정 |  |
| `lt` | литовский | paslaugos paskyra | проверить: возможен вариант «tarnybinė paskyra» |
| `ms` | малайский | akaun perkhidmatan | проверить |
| `nb` | норвежский (букмол) | tjenestekonto |  |
| `nl` | нидерландский | serviceaccount |  |
| `pl` | польский | konto serwisowe |  |
| `pt-br` | португальский (Бразилия) | conta de serviço |  |
| `ro` | румынский | cont de serviciu |  |
| `ru` | русский | сервисный аккаунт |  |
| `sk` | словацкий | servisný účet |  |
| `sl` | словенский | storitveni račun | проверить: возможен вариант «servisni račun» |
| `sr` | сербский | сервисни налог |  |
| `sv` | шведский | tjänstekonto |  |
| `tr` | турецкий | hizmet hesabı |  |
| `uk` | украинский | сервісний акаунт |  |
| `uz` | узбекский | xizmat akkaunti | проверить: возможен вариант «xizmat hisobi» |
| `vi` | вьетнамский | tài khoản dịch vụ |  |
| `zh-hans` | китайский упрощённый | 服务账号 |  |
| `zh-hant` | китайский традиционный | 服務帳號 |  |

Android встраивает `ar`, `de`, `es`, `it`, `ko`, `nl`, `pt-rBR`, `ru`, `uk` и английский: патч вписывает ключ в каждый из этих файлов теми же словами (`pt-rBR` = `pt-br`).

## Сервер
- `pkg/langpack/override.json` — строки Telegram, которые Pluma формулирует иначе; форма как у `own.json`. При `pluma-admin langpack import` текст ключа берётся отсюда на языке импорта вместо текста Telegram (исходный текст Telegram хранится, как и раньше); если ключа в снимке нет, он добавляется. Язык без перевода получает английский и попадает в отчёт импорта. ADR 0046, «Дополнение».
- Без этого скачанный языковой пакет вернул бы «поддержку» поверх текста, вписанного патчем клиента.
- Оператору после выката: повторить `import` последнего снимка (`docs/ops/langpacks.md`, «Свои строки Pluma»).

## Результат
Серверная часть: `override.json` с четырьмя ключами на 37 языках, замена при импорте (`pkg/langpack/own.go`, `store.go`), тесты (`own_test.go`: в том числе одна таблица слов во всех четырёх пакетах; интеграционный `TestImportOverrides`). Клиентские патчи — в ветках клиентов по этой таблице.
