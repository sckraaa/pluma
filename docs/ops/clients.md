# Клиенты под свой сервер

Решение — ADR 0010, задача 006. Клиенты — официальные приложения Telegram, собранные в облаке из закреплённого релиза upstream с патчем под сервер Pluma. Репозитории (публичные):
- Android — [`sckraaa/pluma-android`](https://github.com/sckraaa/pluma-android): инструкции по переменным, подписи и выпуску — в его README.
- iOS, Desktop, Web — `pluma-ios`, `pluma-desktop`, `pluma-web` (ещё не созданы).

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
