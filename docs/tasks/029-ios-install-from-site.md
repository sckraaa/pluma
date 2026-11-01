# 029. Установка iOS-клиента с сайта

- Статус: todo
- Зависит от: подписанная ad hoc-сборка iOS (готова, pluma-ios `8314118`, запуск `41269958693`); страницы на pluma.chat (pluma-site, раздача — Caddy на сервере)
- ADR: —

## Цель
Человек открывает pluma.chat на айфоне в Safari, нажимает «Установить» и получает Pluma с пушами, без кабеля, Mac и переподписи каждую неделю. Решение владельца (2026-11-01): Ad Hoc, а не TestFlight.

## Почему это не повторит судьбу «сервисов установки»
Массовые сервисы подписывали приложения enterprise-сертификатами (In-House). Apple выдаёт их только для внутренних приложений компании и отзывала за раздачу посторонним; после отзыва переставали запускаться все копии. Ad Hoc — штатный способ Apple для зарегистрированных устройств своего аккаунта: каждое устройство вписано в профиль по UDID, их не больше 100 в год на семейство (iPhone). Профили живут год (сейчас до 2027-08-18).

## Что выяснено по документации Apple (2026-11-01)
- **Установка по ссылке.** Страница с `<a href="itms-services://?action=download-manifest&url=…">` на манифест (XML plist); манифест и IPA — только по HTTPS с сертификатом, которому доверяет устройство; MIME: `application/octet-stream` для `.ipa`, `text/xml` для `.plist`; ссылаться на манифест, не на IPA ([Distribute proprietary in-house apps](https://support.apple.com/en-gb/guide/deployment/depce7cefc4d/web)). Поля манифеста Xcode: URL IPA (HTTPS), картинка 57×57 на время загрузки, большая картинка ([Export an iOS app](https://help.apple.com/xcode/mac/current/en.lproj/dev23ea8b877.html)). Страница Apple описывает этот путь для in-house; что он работает и для Ad Hoc — вывод (у Xcode тот же флажок «Include manifest for over-the-air installation»), проверить на первом же шаге.
  - Сообщения на форуме Apple (не документация): устройство не проходит страницу входа при загрузке IPA — доступ закрывать одноразовым токеном в URL, а не авторизацией.
- **«Режим разработчика».** Нужен для приложений, подписанных сертификатом разработки (development), и для IPA, установленных Apple Configurator; не нужен для TestFlight, App Store и in-house ([WWDC22 «Get to know Developer Mode»](https://developer.apple.com/videos/play/wwdc2022/110344/), [Enabling Developer Mode](https://developer.apple.com/documentation/xcode/enabling-developer-mode-on-a-device.md)). Переключатель появляется в настройках только после сопряжения с Mac или установки development-подписанного приложения. Страница [Distributing your app to registered devices](https://developer.apple.com/documentation/xcode/distributing-your-app-to-registered-devices.md) требует его для IPA, экспортированного как «Debugging» (development). Наш IPA подписан Apple Distribution (ad hoc), про него Apple прямо не пишет; на форуме есть сообщение, что ad hoc запросил режим. **Проверить первым делом:** поставить IPA `8314118` на айфон владельца и посмотреть, просит ли iOS «Режим разработчика». Если просит — установка без Mac невозможна, и задача меняется (TestFlight).
- **UDID с сайта.** Профиль конфигурации с полезной нагрузкой `Profile Service`: `PayloadContent.URL` — наш HTTPS-адрес, `DeviceAttributes` — `UDID` (и `VERSION`, `PRODUCT`), `Challenge` — одноразовый токен. После установки айфон шлёт POST: plist атрибутов, подписанный PKCS#7 сертификатом устройства (цепочка «Apple iPhone Device CA»); сервер должен проверить подпись и challenge ([Creating a Profile Server](https://developer.apple.com/library/archive/documentation/NetworkingInternet/Conceptual/iPhoneOTAConfiguration/profile-service/profile-service.html), архив Apple). Чем закончить обмен, чтобы Safari открыл нашу страницу (в примере Apple — редирект 301), и показывает ли iOS неподписанный профиль как «Не проверен» — проверить на устройстве.
- **Регистрация и профили через API** (спецификация OpenAPI App Store Connect 4.5.1): `POST /v1/devices` (`name`, `udid`, `platform: IOS`), `POST /v1/profiles` (`profileType: IOS_APP_ADHOC`, связи `bundleId`, `certificates`, `devices`; ответ с `profileContent`). Нужен командный ключ API с ролью Admin: индивидуальные ключи к provisioning не допускаются, регистрация устройств и профили — роли Account Holder или Admin ([Creating API keys](https://developer.apple.com/documentation/appstoreconnectapi/creating-api-keys-for-app-store-connect-api.md), [Register a single device](https://developer.apple.com/help/account/devices/register-a-single-device/)).
- **Лимит.** Устройства — на семейство в год; удалённое устройство освобождает место только с новым годом членства ([Distributing your app to registered devices](https://developer.apple.com/documentation/xcode/distributing-your-app-to-registered-devices.md), [Devices overview](https://developer.apple.com/help/account/devices/devices-overview)).

## Область, по этапам
1. **Проверка без кода.** IPA `8314118` на айфон владельца через Finder: нужен ли «Режим разработчика», приходят ли пуши. От результата зависит всё дальнейшее.
2. **Установка по ссылке для уже вписанных устройств.** Манифест и IPA на сервере (Caddy, HTTPS), страница pluma.chat/iphone с кнопкой; доступ по одноразовому токену. CI pluma-ios кладёт подписанный IPA и манифест туда, откуда их раздаёт Caddy (как веб-клиент: образ или каталог на хосте — решить).
3. **Сбор UDID с сайта.** Профиль `Profile Service` с challenge, обработчик POST на сервере (проверка подписи устройства и challenge), заявки в базе, одобрение владельцем через `pluma-admin`. UDID — персональные данные: храним только заявки и одобренные устройства, в логи не пишем.
4. **Автоматика.** По одобрению — регистрация устройства и перевыпуск семи профилей через API, переподпись готового IPA новыми профилями (без пересборки), ссылка на установку человеку. Нужен ключ API с ролью Admin (решение владельца).

## Открытые вопросы владельцу
- Ключ App Store Connect API с ролью Admin (для этапа 4).
- Кто одобряет устройства и как человек узнаёт, что можно ставить (письмо, сообщение в Pluma, страница статуса).
- Обновления: человек снова нажимает «Установить»; нужен ли баннер «вышла новая версия» в приложении.

## Критерии приёмки
- Этап 1: записано, нужен ли «Режим разработчика» для ad hoc и работают ли пуши.
- Этап 2: на вписанном айфоне установка из Safari по ссылке pluma.chat без кабеля; IPA недоступен без токена.
- Этапы 3–4: новый айфон от открытия pluma.chat до установленного приложения — без Mac и без ручных шагов владельца, кроме одобрения.

## Результат
—
