# Клиенты под свой сервер

Решение — ADR 0010, задачи 006 и 012. Клиенты — официальные приложения Telegram, собранные в облаке из закреплённого релиза или коммита upstream с патчем под сервер Pluma. Репозитории (публичные):
- Android — [`sckraaa/pluma-android`](https://github.com/sckraaa/pluma-android): инструкции по переменным, подписи и выпуску — в его README.
- Web — [`sckraaa/pluma-web`](https://github.com/sckraaa/pluma-web) (Telegram Web K): переменные, артефакт и проверка на стенде — в его README.
- iOS — [`sckraaa/pluma-ios`](https://github.com/sckraaa/pluma-ios) (Telegram-iOS): подпись, переменные и чек-лист Apple Developer — в его README.
- Desktop — [`sckraaa/pluma-desktop`](https://github.com/sckraaa/pluma-desktop) (Telegram Desktop для Linux, Windows и macOS): переменные и сборки — в его README.

## Что вшивается в клиент

- **Адрес сервера.**
  - Задаётся IPv4 (и при желании IPv6) и портом gnetway. Это адрес, а не имя: tgnet не ищет DC через DNS.
  - После входа клиент держит вшитый адрес: сервер отдаёт в `help.getConfig` пустой `dc_options` (`teamgramd/bin/config.json`). Если его заполнить, клиенты переключатся на эти адреса.
- **Публичный RSA-ключ сервера** (PKCS#1). Получить его так:
  ```bash
  openssl rsa -in secrets/server_pkcs1.key -RSAPublicKey_out
  ```
  Отпечаток, который сборка печатает в логе (`server key 0x…`), должен совпасть с `SERVER_RSA_KEY_FINGERPRINT` из `.env` (там он десятичный). Проверить: `go run ./tools/genserverkey -show secrets/server_pkcs1.key`.
- **`api_id` и `api_hash` Pluma.** `api_id`: Android — 10001, iOS — 10002, Desktop — 10003, Web — 10004. Это не секреты, их несёт каждый клиент. Сервер пока их не проверяет (задача 008).
- **ID приложения** `chat.pluma.app` и название «Pluma».

Смена адреса или ключа сервера требует новой сборки всех клиентов и их переустановки. Поэтому ключ сервера не ротируется без крайней нужды (`docs/ops/security-setup.md`).

## Проверка Android-клиента на локальном стенде

Сборка без переменных сервера — dev-сборка. Она идёт на `10.0.2.2:10443` (хост эмулятора Android) с dev-ключом из `config/dev` репозитория клиента. Приватная часть dev-ключа хранится только у владельца, в `~/pluma-dev-secrets/server_pkcs1.key`. Если она потеряна, нужно создать новый ключ (`go run ./tools/genserverkey -out …`) и заменить `config/dev/server_public.pem` в репозиториях клиентов.

1. Поднять стенд с этим ключом и шлюзом на порту 10443:
   ```bash
   E2E_SERVER_KEY=~/pluma-dev-secrets/server_pkcs1.key E2E_GATEWAY_PORT=10443 test/e2e/run.sh up
   ```
2. Добавить номер в allowlist:
   ```bash
   test/e2e/run.sh admin allowlist add +79990000001
   ```
3. Взять dev-APK: артефакт `apk-build` запуска workflow `android`. Для эмулятора на x86-машине — ручной запуск с ABI `arm64-v8a,x86_64`; на Apple Silicon подходит `arm64-v8a`.
4. Установить APK на эмулятор: `adb install pluma-android-*-dev.apk`.
5. Войти в приложении по номеру. Код получить так:
   ```bash
   test/e2e/run.sh admin login-code +79990000001
   ```
6. После проверки снести стенд, его тома и секреты: `test/e2e/run.sh down`.

### Push на эмуляторе (задача 009)

1. **Сборка клиента с нашим Firebase.** В репозитории `pluma-android` переменная `PLUMA_GOOGLE_SERVICES_JSON` содержит `google-services.json` проекта Firebase (`docs/ops/push.md`). Без неё у клиента нет FCM-токена, и push не придут.
2. **Эмулятор с Google Play services** — образ `google_apis` или `google_apis_playstore`.
3. **Стенд с настоящим ключом FCM.** Ключ копируется в каталог стенда и удаляется при `down`:
   ```bash
   E2E_SERVER_KEY=~/pluma-dev-secrets/server_pkcs1.key E2E_GATEWAY_PORT=10443 E2E_FCM_CREDENTIALS=<JSON-ключ сервисного аккаунта> test/e2e/run.sh up
   ```
4. **Отправка.** Войти в клиенте, свернуть или закрыть приложение и написать ему с тестового пользователя:
   ```bash
   PLUMA_E2E_SEND_TO=+79990000081 E2E_GOTEST_FLAGS='-run TestManualSend' test/e2e/run.sh test
   ```
   Уведомление должно появиться в течение пары секунд. Что ушло в FCM, видно в логе сервиса push (`test/e2e/run.sh compose logs push`).

## Проверка iOS-клиента на симуляторе

Сборка для симулятора идёт на `127.0.0.1:10443` с тем же dev-ключом (симулятор видит сеть Mac). Нужен Xcode с рантаймом iOS.

1. Поднять стенд и добавить номер в allowlist — шаги 1–2 раздела про Android.
2. Взять приложение: артефакт `simulator` запуска workflow `ios` (`gh run download <run> -R sckraaa/pluma-ios -n simulator`), распаковать `Pluma.app`.
3. Создать и запустить симулятор, поставить приложение:
   ```bash
   xcrun simctl create "Pluma" com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro com.apple.CoreSimulator.SimRuntime.iOS-26-4
   ```
   затем `xcrun simctl boot <udid>`, `xcrun simctl install <udid> Pluma.app`, `xcrun simctl launch <udid> chat.pluma.app`.
4. Войти по номеру; код — `test/e2e/run.sh admin login-code <номер>`.
5. После проверки: `xcrun simctl delete <udid>` и `test/e2e/run.sh down`.

## Проверка Desktop-клиента на стенде

Dev-сборки Desktop идут на `127.0.0.1:10443` с тем же dev-ключом. Их артефакты в запуске workflow `desktop` — `pluma-desktop-linux-dev`, `pluma-desktop-windows-dev` и `pluma-desktop-macos-dev`:
```bash
gh run download <run> -R sckraaa/pluma-desktop -n pluma-desktop-macos-dev
```

1. Поднять стенд и добавить номер в allowlist — шаги 1–2 раздела про Android.
2. Запустить клиент с отдельным каталогом данных, чтобы не задеть установленный Pluma:
   - macOS: `open -n Pluma.app --args -workdir <каталог>/`;
   - Linux: `Pluma/Pluma -workdir <каталог>/`;
   - Windows: `Pluma\Pluma.exe -workdir <каталог>\`.
3. Если клиент на другой машине, стенд пробрасывается к ней обратным SSH-туннелем, и адрес `127.0.0.1:10443` остаётся верным:
   ```bash
   ssh -N -R 127.0.0.1:10443:127.0.0.1:10443 <пользователь>@<машина>
   ```
   Сборку для Windows можно проверить и под Wine (11.x).
4. «Start Messaging» → «Log in using phone number», ввести номер. Код — `test/e2e/run.sh admin login-code <номер>`.
5. После проверки удалить каталог данных и снести стенд: `test/e2e/run.sh down`.

## Веб-клиент

Официальный Telegram Web K ([morethanwords/tweb](https://github.com/morethanwords/tweb)) на последнем коммите с layer 228 (`346844bf`, 2026-09-22) с патчем под Pluma. Его собирает workflow `web` репозитория `sckraaa/pluma-web`. Результат — статический сайт и его архив `.tar.gz` с SHA-256 в артефакте запуска. Архив не шифруется: эту статику и так публично раздаёт сайт сервера.

Отличия от мобильных клиентов:
- **Адрес сервера — URL WebSocket** `wss://<домен>/apiws` (переменная `PLUMA_SERVER_URL`), а не IPv4 и порт.
  - Страница, открытая по HTTPS, может ходить только на `wss://`. Поэтому WebSocket-порт шлюза (11443) публикуется через обратный прокси с TLS по пути `/apiws`.
  - Раздача сайта, TLS и домен — отдельная серверная задача.
  - `ws://` сборка принимает только для `localhost`.
- Все DC клиента ведут на этот URL: для клиента сервер — DC 1. HTTP-транспорт tweb (`/apiw1`) выключен: шлюз принимает MTProto только по WebSocket.
- Ключ задаётся переменной `PLUMA_RSA_PUBLIC_KEY` (PKCS#1 PEM, как у Android). Сборка печатает отпечаток в hex и десятичном виде; десятичный должен совпасть с `SERVER_RSA_KEY_FINGERPRINT`.
- `api_id` — 10004.
- Web push выключен до задачи 009: без VAPID-ключа сервера клиент не подписывается.

### Проверка веб-клиента на локальном стенде

Одна команда в рабочей копии `pluma-web`:
1. поднимает e2e-стенд указанной рабочей копии сервера с одноразовым ключом;
2. собирает клиент под этот ключ;
3. в headless Chromium проходит вход по номеру из allowlist с кодом из `pluma-admin`, регистрацию и отправку сообщения в «Избранное»; второй браузер входит в тот же аккаунт и должен найти сообщение;
4. сносит стенд.

```bash
PLUMA_SERVER_DIR=~/pluma e2e/run.sh
```

Вручную, dev-сборкой — она идёт на `ws://localhost:11443/apiws` с dev-ключом:
1. Поднять стенд с dev-ключом и WebSocket-портом шлюза 11443:
   ```bash
   E2E_SERVER_KEY=~/pluma-dev-secrets/server_pkcs1.key E2E_GATEWAY_WS_PORT=11443 test/e2e/run.sh up
   ```
2. Добавить номер в allowlist:
   ```bash
   test/e2e/run.sh admin allowlist add +79990000001
   ```
3. Взять dev-сборку: артефакт `pluma-web-<версия>-dev` запуска workflow `web` или `ci/build.sh dev` в рабочей копии клиента. Распаковать и раздать на localhost:
   ```bash
   python3 -m http.server --bind 127.0.0.1 --directory pluma-web-2.2-676-dev 8080
   ```
4. Открыть http://localhost:8080/, нажать «Log in by phone number», ввести номер. Код получить так:
   ```bash
   test/e2e/run.sh admin login-code +79990000001
   ```
5. Снести стенд: `test/e2e/run.sh down`.

Сейчас после входа список чатов пуст: на сервере нет `messages.getDialogFilters` (задача 013), а без папок Web K не рисует список. Сообщения работают, «Избранное» открывается из меню.
