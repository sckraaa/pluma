# Установка iOS-приложения с сайта (Ad Hoc)

Сервис `iosinstall` (задача 029) ставит Pluma на iPhone прямо с pluma.chat, без App Store и без Mac. Человек проходит путь на сайте:
1. скачивает профиль;
2. ставит его в «Настройках», и айфон сообщает свой UDID;
3. сервис регистрирует айфон у Apple, перевыпускает ad hoc-профили и переподписывает сборку;
4. на странице появляется кнопка «Установить».

Для запуска нужен «Режим разработчика» (iOS 16+, проверено на iOS 26). Пуши работают. Apple даёт команде 100 айфонов в год.

## Как устроено
- **Адреса** — на именах сайта (`PLUMA_SITE_DOMAINS`), Caddy передаёт `/ios/adhoc/*` в контейнер `iosinstall:9098`:
  - `GET /ios/adhoc/profile` — профиль `Profile Service` с одноразовым challenge на 30 минут;
  - `POST /ios/adhoc/enroll` — ответ айфона. Подпись проверяется по цепочке до Apple Root CA, сертификат устройства выдан Apple iPhone Device CA. Затем переход на страницу `PUBLIC_URL/iphone/install#t=<токен>`;
  - `GET /ios/adhoc/status?t=` — `waiting`, `ready` (со ссылкой `itms-services`) или `refused`;
  - `GET /ios/adhoc/manifest?t=`, `GET /ios/adhoc/app.ipa?t=` — манифест и IPA, только по токену устройства;
  - `POST /ios/adhoc/upload` — CI выкладывает новую сборку (заголовок `Authorization: Bearer <токен загрузки>`).
- **Подпись.** CI pluma-ios после ad hoc-сборки отправляет:
  - подписанный IPA;
  - версию;
  - список бандлов с их bundle ID и правами (entitlements).

  Сервис переподписывает эту сборку. Порядок:
  1. новые айфоны регистрируются у Apple (`POST /v1/devices`);
  2. ad hoc-профиль каждого бандла удаляется и создаётся заново со всеми включёнными устройствами команды, под тем же именем;
  3. `rcodesign` 0.29.0 подписывает IPA сертификатом Apple Distribution.

  Все айфоны получают один и тот же IPA. Переподпись идёт по одной; при ошибке повторяется с паузой от 1 до 30 минут.
- **Хранение.** `state.json` в томе `iosinstall_data`: айфоны (UDID, модель, версия iOS, id у Apple, токен), текущая сборка и подписанный IPA. UDID — персональные данные: в логи не пишутся, в общие базы не попадают.
- **Ограничения.** Профиль — 5 раз в сутки с одного адреса (`LIMIT_ADDRESS_DAY`). Новых айфонов — 10 в сутки (`LIMIT_DEVICES_DAY`). Только iPhone, iPad не обслуживается.
- **Секреты** доступны только этому контейнеру. Это ключ App Store Connect (Admin), сертификат Apple Distribution с ключом и паролем, токен загрузки. Они лежат файлами в `/opt/pluma/iosinstall` (владелец uid 65532, режим 0400). В общем `.env` только несекретные значения:
  - `PLUMA_IOSINSTALL_IMAGE`;
  - `PLUMA_IOSINSTALL_URL`;
  - `PLUMA_ASC_KEY_ID`;
  - `PLUMA_ASC_ISSUER`;
  - `PLUMA_SIGN_CERTIFICATE_ID`.

  Кто захватит сервер, получит ключ подписи и Admin-ключ команды Apple — риск принят владельцем (вариант «а», бриф 029).

## Первое включение
1. **Секреты** кладёт владелец со своей машины. Скрипт ничего не печатает и не передаёт в командной строке:
   ```bash
   teamgramd/deploy/iosinstall/put-secrets.sh neo_old ~/pluma-secrets/desktop/AuthKey_4FQM72H54Q.p8 \
     ~/pluma-secrets/ios/pluma-distribution.p12 ~/pluma-secrets/ios/pluma-distribution.p12.password
   ```
   Он создаёт на сервере токен загрузки и записывает его секретом `IOSINSTALL_UPLOAD_TOKEN` окружения `release` в pluma-ios (нужен `gh` с правом на этот репозиторий).
2. **`.env` сервера:** `PLUMA_IOSINSTALL_IMAGE=ghcr.io/sckraaa/pluma-iosinstall:<коммит>`, `PLUMA_IOSINSTALL_URL=https://pluma.chat`, `PLUMA_ASC_KEY_ID`, `PLUMA_ASC_ISSUER`, `PLUMA_SIGN_CERTIFICATE_ID` (id сертификата Apple Distribution у Apple: `GET /v1/certificates?filter[certificateType]=DISTRIBUTION`).
3. **Запуск** — в команду сервисов добавляется `-f docker-compose.iosinstall.yaml` после `docker-compose.web.yaml`. Caddy перезапускать не нужно, если его Caddyfile уже с маршрутом `/ios/adhoc/*`; иначе `docker compose … up -d web`.
4. **Первая сборка** — запуск workflow pluma-ios с `ipa`. CI отправляет IPA сервису, через минуту `docker compose … logs iosinstall` пишет `build … uploaded` и `signed for N devices`.

## Проверка
- `curl -sI https://pluma.chat/ios/adhoc/profile` — `200`, `application/x-apple-aspen-config`.
- `docker compose … exec iosinstall /iosinstall healthcheck; echo $?` — `0`.
- На айфоне: страница pluma.chat/iphone/install → профиль → «Настройки» → «Профиль загружен» → «Установить» → Safari возвращается на страницу → «Установить Pluma».

## Отказ и откат
- Apple отказала в регистрации айфона (неверный UDID, исчерпан лимит в 100) — у айфона статус `refused`, причина в `state.json` (поле `failure`), в логе только код HTTP.
- Ошибка подписи или API — в логе `signing failed, again in …`. Айфоны ждут, прежний IPA продолжает раздаваться.
- Выключить — убрать `-f docker-compose.iosinstall.yaml` из команды и `docker compose … rm -sf iosinstall`. Маршрут Caddy тогда отвечает 502, остальной сайт не затронут.
