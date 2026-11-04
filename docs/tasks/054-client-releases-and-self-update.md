# 054. Сборки клиентов на сайте и самообновление

- Статус: in progress (решения владельца получены 2026-11-04; серверная часть — ADR 0035, `docs/ops/releases.md`)
- Зависит от: 029 (сервис `iosinstall`), 052 (подписанные сборки в `release`)
- ADR: 0010 «Облачная сборка клиентов» (подписанные файлы не публикуются артефактами); 0035 «Выпуски клиентов».
- Исследование: `docs/tasks/054-research/clients.md` (исходники Android `aac1efcb`, iOS `6ad963e5`, tdesktop `a1e89e1f`, с номерами строк).

## Цель
Свежая сборка каждого клиента сама появляется на pluma.chat после одобрения выпуска, без ручных шагов. Установленное приложение узнаёт о новой версии и обновляется тем способом, который разрешает платформа: Android — скачивает APK с нашего сервера и открывает системный установщик; iOS — показывает экран обновления со ссылкой на установку; Desktop — скачивает и ставит пакет своим апдейтером; Web — обновляется с сервера, как сейчас.

## Что известно (кратко; подробности и строки — в исследовании)
- **Сейчас** сборки на сайт не попадают: в `~/pluma-site/src/install/releases.ts:49-56` заглушки `dl.pluma.chat/…`, хоста `dl.pluma.chat` нет, подписанные APK и пакеты Desktop лежат в зашифрованных артефактах CI 30 дней. Ad hoc IPA уже уходит в `iosinstall`. `help.getAppUpdate` — заглушка upstream (всегда `help.noAppUpdate`).
- **Android** (§1). Проверка обновлений работает только в standalone-сборке (`LaunchActivity.java:5957-5960`); наш форк собирает модуль Google Play. Наименьший патч — перенести в `TMessagesProj_App` переопределения standalone и два файла UI (§1.4), не беря модуль standalone целиком (он меняет пакет и добавляет SMS-разрешения). APK приходит как `Document` в `help.appUpdate` и качается по MTProto; ставит системный установщик с подтверждением пользователя. Версия в ответе — только цифры через точку с тем же числом компонент (иначе `NumberFormatException` в UI-потоке): нужна схема `12.9.1.N` и растущий `versionCode`.
- **iOS** (§2). Клиент опрашивает `help.getAppUpdate` каждые 12 часов и сам версии не сравнивает: решает сервер. Кнопка «Обновить» открывает App Store (`appstore_id = 0` — несуществующая страница). Нужен патч: брать `url` из ответа и открывать его, отдавать номер сборки в `app_version`. `url` — страница `pluma.chat/iphone/install`. Тихого обновления нет; ad hoc — до 100 айфонов в год и «Режим разработчика».
- **Desktop** (§3). Свой апдейтер tdesktop (HTTP `<prefix>/current` + пакеты `t*upd<ver>`, подпись RSA-1024/SHA-1) у нас вырезан патчем. Нужно: включить апдейтер, свои ключи, префикс `dl.pluma.chat/desktop` (и `autoupdate_url_prefix` в `help.getConfig`), имена бандла и реестра, Packer в CI, схема `AppVersion`. Ключи платформ и `AutoUpdateVersion` живут в `lib_base` — прочитать до реализации (НЕ ПРОВЕРЕНО).
- **Предложенная схема** (§4.2): сервис выпусков на `neo_old` по образцу `iosinstall` (приём от CI по токену, неизменяемые файлы `<платформа>/<версия>`, указатель «текущий», откат переключением), раздача `dl.pluma.chat` через Caddy и `/releases.json` для сайта (сайт читает его, как `/progress.json`), таблица `app_releases` для `help.getAppUpdate`, APK — документ в DFS.

## Область
1. **Сервер.**
   - Сервис выпусков `app/ops/releases`: `POST /releases/upload` (Bearer-токен CI), хранение в томе, указатель текущего выпуска по платформе, `/releases.json`, раздача файлов через Caddy на `dl.pluma.chat`; для Android — APK в DFS и запись в `app_releases`.
   - Миграция `migrate-*.sql`: таблица `app_releases(platform, version, build, document_id, text, entities, can_not_skip, created_at)`.
   - `help.getAppUpdate`: платформа по `api_id` авторизации, версия по `app_version`; Android — `help.appUpdate` с документом, iOS — с `url`, Desktop и Web — `help.noAppUpdate`. Ошибка таблицы — `noAppUpdate` и лог, не 500.
   - `help.getConfig`: `autoupdate_url_prefix` = `https://dl.pluma.chat/desktop`.
   - `docs/ops`: развёртывание сервиса, DNS, токен загрузки, откат выпуска.
2. **Android** (`~/pluma-android`, запись в `clients/patches/android.md`): standalone-переопределения и UI обновления в `TMessagesProj_App`; `versionName` `12.9.1.N`, растущий `versionCode`; шаг CI после `sign` — загрузка в сервис выпусков.
3. **iOS** (`pluma-ios`): `url` в `AppUpdateInfo` и открытие его вместо App Store; номер сборки в `app_version`; запись о выпуске из `iosinstall` или шага CI.
4. **Desktop** (`pluma-desktop`): апдейтер включён, свои ключи, префикс, имена бандла и реестра, схема версий, Packer в `release`, загрузка пакетов и полных установщиков.
5. **Сайт** (`~/pluma-site`): загрузки из `/releases.json`, `releases.ts` — форма и запас; кнопки iPhone по решению о канале.
- Не трогаем: каналы раздачи (`tdhbcfeed`, документ в канале) — не нужны (§5 п. 9 исследования); Web K — без изменений.

## Критерии приёмки
- Модульные тесты: сравнение версий Android по правилу клиента (`SharedConfig.java:811-824`), разбор `app_version` всех платформ, выбор ответа `help.getAppUpdate`.
- Интеграционные тесты (MySQL, DFS): запись выпуска, текущий и откат; `help.getAppUpdate` отдаёт документ, который качается `upload.getFile`.
- e2e через `pkg/mtclient`: клиент Android старой версии получает `help.appUpdate` с документом APK, новой — `help.noAppUpdate`; iOS — `url`; Desktop — `noAppUpdate`; `help.getConfig` содержит `autoupdate_url_prefix`.
- На стенде: `dl.pluma.chat/desktop/current` отдаёт JSON формата tdesktop, пакет принимается апдейтером (подпись своим ключом).
- В клиентах, со скриншотами: Android — плашка и лист обновления, скачивание, системный установщик, данные сохраняются; iOS — экран обновления ведёт на страницу установки; Desktop (macOS и Windows на CachyOS) — обновление ставится и приложение перезапускается; сайт показывает свежие ссылки после выпуска без пересборки.
- Обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race`, `make lint`, `go test ./tools/...`, `go run ./tools/apicoverage`.

## Решения владельца (2026-11-04)
1. **Публикация.** Одобрение в `release` остаётся, но один клик на платформу: после него сборка сама подписывается, выкладывается на сайт и предлагается приложениям. iOS — один клик вместо двух.
2. **iPhone.** Основной канал на сайте — TestFlight; ad hoc (`iosinstall`) — запасной. Приложение из ad hoc предлагает обновление ссылкой на страницу установки; TestFlight-сборкам `help.getAppUpdate` обновление не предлагает (их обновляет TestFlight) — нужен признак канала в `app_version`.
3. **Windows.** Пока без Authenticode: переносная папка (zip) с автообновлением, предупреждение SmartScreen при первом запуске. Сертификат — позже, отдельным решением.
4. **Ключ обновлений Desktop.** Формат tdesktop: RSA-1024, подпись SHA-1, без патча алгоритма. Ключ генерируется скриптом сразу в `~/pluma-secrets/desktop` и в секрет окружения `release`, без вывода на экран; резервную копию хранит владелец.

**По умолчанию, на подтверждение** (§5 исследования):
- версия Android — `12.9.1.N`, `versionCode` растёт с каждой сборкой, release-ключ Android один навсегда;
- схема версий Desktop — выбрать после чтения `lib_base` (альфы upstream или сдвиг `AppVersion`), с проверкой, что следующий upstream не столкнётся с номером;
- `can_not_skip` — выключено; включается для отдельного выпуска вручную;
- общая ссылка на манифест ad hoc без токена устройства — не делаем, ссылка ведёт на страницу установки;
- текст изменений — файл выпуска в каждом форке, на русском и английском, язык ответа — по языку сессии;
- каналы раздачи (`tdhbcfeed`, документ в канале) — не делаем.

**Принято при реализации** (на подтверждение):
- Раздача — не на отдельном имени `dl.pluma.chat`, а на путях `/dl/…` того же `pluma.chat`: тот же Caddy и сертификат, DNS-запись не нужна. Префикс Desktop — `https://pluma.chat/dl/desktop`. Сервис — `app/service/releases` (на образе сервисов), не `app/ops/releases`.
- Заметки к выпуску — два поля `notes_en`/`notes_ru` без entities (`help.appUpdate.entities` пустой).
- Сборки Android со старой схемой версии `12.9.1` (3 части) обновления не увидят: правило клиента сравнивает лишь общие части. Их надо один раз поставить с сайта заново.

## В процессе (2026-11-04)
- **Сервер** — ветка `task/054-releases`: `pkg/appreleases`, `app/service/releases` (+ интеграционный тест), миграция `app_releases`, `help.getAppUpdate` (+ интеграционный тест), `autoupdate_url_prefix`, `/dl/desktop/current4` для апдейтера tdesktop (ключи систем из `lib_base` `82d182a2`), `pluma-admin release`, Caddy `/dl/*`, `docker-compose.releases.yaml`, `put-token.sh`, ADR 0035, `docs/ops/releases.md`. Каждый коммит собирается и проходит свои тесты отдельно. CI ждёт GitHub (сбой Actions).
- **Найдено при реализации:** iOS шлёт `app_version` = `"<версия> (<сборка>) "` из MtProtoKit (`MTApiEnvironment.m:384-394`), а не только версию, как писало исследование §2.1: номер сборки патчить не нужно, только признак `adhoc`. Сервер обрезает пробел.
- **Android** — PR sckraaa/pluma-android#2 (`e314edb`): `isSelfUpdatingBuild()` в 9 местах обновления вместо `isStandalone=true` (тот включил бы строку TON и прочее), лист и плашка из модуля standalone, `androidx.core` в модуле приложения, `12.9.1.N` и `versionCode = (6979·1000+N)·10+9`. Локально `0f3ff30`: `ci/publish.sh` и шаг в `sign`.
- **iOS** — ветка `task/054-app-update` (`13f0ac4`): `AppUpdateInfo.url`, экран открывает его, `adhoc` по `ProvisionedDevices`. Локально `1ca1c32`: `ci/release-upload.sh`, preflight внутри `build-ipa` (одно одобрение).
- **Блокеры:** коммиты с `.github/workflows` не пушатся — у токена `gh` нет права `workflow` (владелец: `gh auth refresh -h github.com -s workflow`). Токен загрузки и `PLUMA_RELEASES_URL` в `.env` сервера ставит владелец (`teamgramd/deploy/releases/put-token.sh neo_old`); скрипт выкатки запускает `releases` только когда они есть.
- **Desktop** — локальная ветка `task/054-updates` в `~/pluma-work/pluma-desktop-053` (`4ba0ba8`, `9b3a688`, `b8b64ad`; реестр `clients/patches/desktop.md`, D-074…D-078): апдейтер включён, ключ Pluma в оба слота, префикс `https://pluma.chat/dl/desktop` (у dev — `127.0.0.1`), без MTP-проверки, имена `Pluma.app` и GUID Pluma в Updater, `AppVersion` = апстрим + номер сборки с защитой от миграций, Updater в пакетах, `ci/pack_update.py` (формат Packer на Python, подпись `openssl`, обратное чтение), `ci/publish.sh`, `ci/update-key.sh`, job `release` (одно одобрение) и публикация из `staple-macos`. Патч применён к апстриму, тесты зелёные. Не запушено: нужен `config/update_public.pem` (владелец: `ci/update-key.sh`) и право `workflow`. **Не проверено:** сырой LZMA для Windows в `LzmaUncompress` — первым обновлением на Windows.
- **Сайт** (`~/pluma-site`, не закоммичен, как и весь репозиторий): кнопки на `pluma.chat/dl/<платформа>/latest`, данные из `/dl/releases.json` при загрузке (`fromServer`, `liveReleases`), тесты и сборка зелёные. Выкладывать после выкатки сервера.
- **Ревью 054** (отдельные проверяющие по областям, каждую находку проверяли два скептика): 17 подтверждено, исправлено всё.
  - Сервер: чистка `work/` при запуске; публикация по одному, оставленный без строки каталог версии больше не блокирует её (409 вместо 500); fsync каталогов; у скачиваний скользящий срок вместо 30 минут; `migrate` получал `bff.yaml` без `PLUMA_RELEASES_URL` (это же поймал e2e).
  - Android: номер сборки — 5 цифр с нулями (клиент сравнивает сохранённое предложение строкой), версия только в модуле приложения (кэш Gradle), сборка с `abis` не публикуется, canary не вытесняет push.
  - iOS: без токена `iosinstall` сборка не записывается; порядок отката в README.
  - Desktop (критично): сборки не передавали `DISABLE_AUTOUPDATE=OFF`, configure падал бы на нашей же проверке. Схема версий переделана: `AppVersion` — апстрима (по нему помечены локальные данные tdesktop), апдейтер сравнивает `PlumaBuild`; пустая MTP-проверка вместо удалённой; dev-ключ; `release` публикует собравшиеся системы; `staple-macos` — с коммита своего запуска.
  - Документы: отзыв не догоняет уже полученные Android-предложения и IPA страницы установки.
- **Дальше:** CI сервера → слияние → выкатка; после шагов владельца — пуш веток клиентов, сборки, проверка обновления в клиентах.

## Результат
