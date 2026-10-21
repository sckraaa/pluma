# Облачная сборка всех клиентов без своего железа (2026-10-21)

Обозначения: **[проверено]** — подтверждено документацией, исходниками или данными GitHub API о реальных запусках; **[оценка]** — инженерная оценка; **[не проверено]** — проверить на практике.
Это продолжение разделов 4–5 файла `docs/audit/04-clients-and-builds.md`. Там опорой были свой Mac и Ryzen-ПК, здесь от них отказываемся.

---

## 0. Короткий ответ

Оптимальный облачный вариант есть, и он бесплатный: **публичные репозитории + стандартные GitHub-hosted раннеры**. Для публичных репо минуты не ограничены, в том числе для macOS на M1 [проверено]. Что сборка Telegram-iOS там проходит, показывают живые форки:

| Репозиторий | Раннер | Холодная | Тёплая | Кэш |
|---|---|---|---|---|
| NextAlone/Nagram-iOS (подписанные сборки в TestFlight, окт. 2026) | `xcode-27` (M1, 3 CPU / 7 ГБ) | — | job 53 мин, Bazel 46.5 мин: 4936 из 7850 действий из кэша, critical path 2032 с | actions/cache, 1.7 ГБ, восстановление ~1 мин |
| pixia1234/telegram-ios (март 2026) | `macos-latest` | 59–65 мин | **3.5–7.3 мин** | actions/cache + `--cacheDir` |
| neymaryshop-jpg/ayugram_ios (авг. 2026) | `macos-15`, Xcode 26.2 | 53–68 мин | — | — |

Источник: GitHub API (runs/jobs/logs) [проверено]. Собственный CI Telegram-iOS (`runs-on: macos-13`) мёртв: запуски падают или отменяются через 24 часа ожидания, поэтому на него опираться нельзя [проверено].

Android, tdesktop и Web тоже штатно собираются на стандартных раннерах; цифры ниже, в разделе 5.

**Стоимость: $0 в месяц** за 10 iOS + 10 Android + 5 desktop + десятки web-сборок. Платные сервисы нужны только как резерв.

---

## 1. Что даёт GitHub в 2026 году [проверено]

| Раннер | Публичный репо | Приватный репо | Метки |
|---|---|---|---|
| Linux x64 | 4 CPU / 16 ГБ / «14 ГБ SSD» | 2 CPU / 8 ГБ | `ubuntu-24.04`, `ubuntu-26.04`, `ubuntu-latest` |
| Windows x64 | 4 CPU / 16 ГБ | 2 CPU / 8 ГБ | `windows-2025`, `windows-latest` |
| macOS arm64 | **3 CPU (M1) / 7 ГБ** | то же | `macos-14/15/26`, `macos-latest`, `xcode-27` |
| macOS Intel | 4 CPU / 14 ГБ | то же | `macos-15-intel`, `macos-26-intel` |

- **Деньги и лимиты.** Для публичных репо: «free and unlimited». Job длится до 6 ч. Одновременно идёт до 20 jobs, из них до 5 на macOS (тариф Free). Кэш — 10 ГБ **на каждый репозиторий**. Записи, к которым не обращались больше 7 дней, удаляются; при переполнении вытесняются самые давно использованные (LRU). Лимит можно поднять до 10 ТБ за $0.07/ГБ-мес. Кэш нельзя восстановить с другого тега или соседней ветки, поэтому сборки нужно запускать с `main` через `workflow_dispatch`, а не пушем тега.
- **Xcode.** В образе `macos-26` arm64 (20261013) стоят Xcode 26.0.1, 26.1.1, **26.2**, 26.3, 26.4.1, 26.5 и 26.6 (по умолчанию). Release-12.9.2 требует Xcode 26.2 и Bazel 8.4.2 (`versions.json`), так что всё совпадает.
- **Диск.** Цифра «14 ГБ SSD» — гарантированный минимум. Фактически на macos-latest свободно около 45 GiB; если удалить лишние Xcode (~38 ГБ) и рантаймы симуляторов (~51 ГБ), будет около 137 GiB [проверено по отчёту пользователя; у Electron: 134–157 ГБ после очистки]. На Linux свободно около 25–29 ГБ, `jlumbroso/free-disk-space` добавляет ещё около 30 ГБ. Для Android при очистке Android SDK оставить: `android: false`.
- **Приватные репо.** macOS стоит $0.062/мин, Linux 2-core $0.006/мин, Windows $0.010/мин. Larger macOS: 5-core M2 Pro $0.102/мин, 12-core $0.077/мин, только на тарифах Team/Enterprise.
- **Скоро в образах:** `ubuntu-latest` переедет на 26.04 в ноябре 2026, образы macOS 14 перестают поддерживаться 2 ноября 2026. Метки нужно указывать явно: `macos-26`, `ubuntu-24.04`.

## 2. Можно ли сделать форки клиентов публичными

**Лицензии.** DrKLO/Telegram — GPL-2.0, tdesktop, tweb и telegram-tt — GPL-3.0. В README Telegram-iOS прямо просят публиковать свой код. GPL требует отдать исходники получателям бинарников; публичный репозиторий это требование закрывает [проверено].

**Что окажется в открытом доступе и насколько это критично:**
- Адрес и порт сервера. Это не секрет: любой из ~100 пользователей извлечёт его из приложения. Но публичный репо сообщает о существовании проекта всем, это потеря «неизвестности». Можно передавать адрес через `vars`/`secrets` и подставлять при сборке; это лишь маскировка.
- Публичный RSA-ключ: он публичный по определению. api_id и api_hash: для своего сервера ничего не значат.
- Bundle ID и Team ID: всё равно видны внутри IPA.
- **Должны остаться секретами:** p12 и пароль, mobileprovision, release.keystore и пароли, google-services.json (по желанию), SSH-ключ деплоя на VPS. Хранить их нужно в **environment secrets** с обязательным ревьюером.

**Изоляция от форков [проверено].** Workflows, запущенные из форка, не получают секреты (кроме GITHUB_TOKEN, а он у таких PR только на чтение). Поэтому:
1. Не использовать `pull_request_target`.
2. В настройках включить «Require approval for all external contributors».
3. Закреплять actions по SHA и задавать `permissions: contents: read`.

**Что ещё видно посторонним в публичном репо.** Логи. Артефакты: их может скачать любой вошедший пользователь GitHub [вероятно]. Кэши: PR из форка может прочитать кэш базовой ветки. Отсюда правила:
- готовые IPA/APK **не выкладывать** в artifacts или Releases открыто: шифровать `age` (публичный ключ получателя лежит в репо, приватный — в секрете job подписи/публикации) или сразу отправлять rsync на VPS;
- в логах не печатать значения из секретов.

**Альтернатива** — оставить репо приватными. Тогда это ≈ $15–55 в месяц (раздел 7), а Linux/Windows станут 2-ядерными, то есть медленнее.

## 3. Кэши без своего железа

| Вариант | Что кэширует | Плюсы | Минусы | Цена |
|---|---|---|---|---|
| **actions/cache** (основной) | Дисковый кэш Bazel, ccache, Gradle, библиотеки tdesktop | Ничего не нужно поднимать; у каждого репо свои 10 ГБ; 1.7 ГБ скачиваются примерно за 1 мин (≈56 МБ/с, лог Nagram) | Вытеснение через 7 дней и по LRU; записи неизменяемы, каждый раз тянется весь архив | $0 |
| **GHCR** (публичные образы) | Docker-образ тулчейна tdesktop и образ Android SDK/NDK | Не вытесняется, хранение и трафик бесплатны | GHCR бесплатен «пока»; образ виден всем (в нём только библиотеки, это нормально) | $0 |
| **bazel-remote на продакшн-VPS** | Bazel по gRPC или HTTP | Постоянный, инкрементальный, общий для любого CI (GitHub, Codemagic, Depot); TLS + htpasswd; `--max_size`; бэкенд S3/R2 | Нагрузка и поверхность атаки на продакшне; большая задержка до раннеров Azure в США; `Make.py --cacheHost` включает `--experimental_remote_downloader`, поэтому в bazel-remote нужно включить `--experimental_remote_asset_api` [не проверено] | $0 плюс трафик и диск VPS |
| **BuildBuddy Personal** | Bazel | Быстрый, ничего не поднимать | Бесплатно 100 ГБ трафика кэша, внешний сервис без SLA. 10 iOS-сборок в месяц, скорее всего, уложатся [оценка] | $0 |
| **Cloudflare R2** | Как бэкенд bazel-remote (`--s3.endpoint`) или sccache (поддерживает R2 штатно) | Исходящий трафик бесплатен; бесплатно 10 ГБ, 1 млн записей и 10 млн чтений в месяц | Bazel не умеет напрямую подписывать запросы к S3, нужен прокси (bazel-remote или Worker [не проверено]) | $0 |
| **NativeLink** | Bazel (OSS, Apache-2.0) | Аналог bazel-remote с удалённым исполнением | Условия облачного бесплатного тарифа неясны | — |
| **Gradle** | `gradle/actions/setup-gradle` (улучшенный кэш бесплатен для публичных репо; есть MIT-вариант `cache-provider: basic`); Build Cache Node — по желанию | — | Нативную часть (CMake) Gradle не кэширует, для неё нужен ccache/sccache | $0 |

**Важные детали для iOS [проверено по исходникам release-12.9.2]:**
- `--cacheDir` и `--cacheHost` в Make.py взаимоисключающие.
- **`--bazelArguments` разбирается, но в Bazel не передаётся**: `add_additional_args` нигде не вызывается. Дополнительные флаги поэтому писать в `~/.bazelrc` раннера: `build --experimental_disk_cache_gc_max_size=7G` (доступен с Bazel 7.4), а для удалённого кэша ещё `--remote_download_toplevel` и `--remote_header=...`.
- Попадание в кэш требует стабильного пути исходников (`/Users/Shared/telegram-ios`) и стабильного `--bazelUserRoot`. Именно так делает upstream build.yml.
- `buildNumber` попадает только в `BuildNumberInfoPlist`, поэтому смена номера сборки компиляцию не сбрасывает.
- Патчи в TelegramCore (`Network.swift`, адреса DC) и MtProtoKit (RSA) лежат внизу графа зависимостей. Их правка пересобирает почти всё (у Nagram при 63% попаданий в кэш сборка всё равно заняла 46 мин). Эти патчи нужно менять как можно реже.

**Вывод:** начинать с actions/cache и GHCR. bazel-remote на VPS (лучше с бэкендом R2) или BuildBuddy подключать, только если (а) не хватит 10 ГБ, (б) вытеснение через 7 дней регулярно даёт холодные сборки, (в) понадобится общий кэш для GitHub и резервного Codemagic.

## 4. Платные облачные Mac как резерв

| Вариант | Железо | Цена | Комментарий |
|---|---|---|---|
| Codemagic (личный аккаунт) | Mac mini M2, 8 ядер / 8 ГБ | **500 мин в месяц бесплатно**, дальше $0.095/мин; M4 10 ядер / 16 ГБ — $0.114/мин | Хороший резерв на случай сбоя GitHub. Холодная сборка ≈ 35–50 мин [оценка], без общего кэша каждый раз холодная |
| Depot | macOS M4 8 CPU / 24 ГБ / 400 ГБ | $0.08/мин, план Developer $20/мес | Upstream tdesktop использует Depot для тегов и PR. iOS холодная ≈ 20–30 мин [оценка], ≈ $2 за сборку |
| GitHub larger | 5-core M2 Pro / 12-core | $0.102 / $0.077 за мин | Нужен тариф Team; бесплатных минут нет даже для публичных репо |
| Scaleway Mac mini | M4-S 10C / 16 ГБ | €0.22/ч, **минимум 24 ч** (лицензия Apple), то есть ≥ €5.3 за запуск; €149/мес | Подходит для пакетных сборок, иначе дорого |
| Xcode Cloud | — | 25 ч в месяц входят в подписку разработчика | По-прежнему не рекомендую: Bazel вместо xcodeproj, нет кэша (см. аудит 4.1) |

## 5. Пайплайн: быстрые падения и продолжение после сбоя

### 5.1 Общие принципы
1. **Структура.** Четыре публичных репо. В каждом: `UPSTREAM_REF`, `patches/`, `ci/apply-patches.sh` (каждая замена проверяется через `grep -q`), а логика сборки в `ci/*.sh`, а не в YAML. Тогда те же скрипты запустятся на Codemagic, Depot или арендованном Mac.
2. **Jobs вместо шагов:** `preflight` (ubuntu, секунды) → `build` → `sign` → `publish`. Если упадёт поздний job, «Re-run failed jobs» перезапустит только его; это возможно до 30 дней и до 50 раз [проверено]. Результат `build` передаётся дальше как зашифрованный артефакт.
3. **Кэш, который сохраняется и при падении.** Использовать отдельные `actions/cache/restore` и `actions/cache/save` с `if: always()`; у обычного `actions/cache` сохранение работает только при успехе. Ключ включает `run_id` и `run_attempt` (записи неизменяемы), `restore-keys` подбирают ближайший префикс. Дисковый кэш Bazel пишется по мере выполнения действий, так что повтор после падения продолжит с того места [оценка, на Telegram не проверено].
4. **`--continueOnError` (`--keep_going`)**: все ошибки компиляции видны за один прогон.
5. **Повторы только для сети:** clone с сабмодулями, `sdkmanager`, `npm ci`, `xcodebuild -downloadComponent MetalToolchain`, загрузка на VPS. Обёртка `ci/retry.sh 3 <cmd>`. Компиляцию не повторять.
6. **`timeout-minutes` в каждом job:** iOS 150, Android 90, Windows 180, toolchain 330.
7. **`concurrency`** на каждую платформу, чтобы не было двух одинаковых сборок одновременно.
8. **Еженедельный cron** с полной сборкой (на публичных репо это бесплатно). Он держит кэши живыми вопреки 7-дневному вытеснению и рано ловит изменения образов, например удаление Xcode 26.2.

### 5.2 iOS (Telegram-iOS release-12.9.2, ad-hoc)

**preflight** (`ubuntu-24.04`, около 1 мин):
- патчи накладываются с `--check` на sparse-clone нужных файлов;
- `currentLayer()` равен 228;
- fingerprint RSA совпадает с `gnetway.yaml`;
- для каждого из 8–9 профилей (Telegram, Share, Widget, NotificationService, NotificationContent, Intents, BroadcastUpload, Watch*) после `openssl smime -verify -noverify` и plistlib проверяются: срок годности больше 14 дней, bundle id, Team ID, `aps-environment`, App Groups, число UDID;
- сертификат из p12 входит в `DeveloperCertificates` каждого профиля.

**build** (`macos-26`):
1. Проверить, что есть `Xcode_26.2.app` и `df` показывает ≥ 60 ГБ; иначе очистить (удалить другие Xcode и `/Library/Developer/CoreSimulator`). Проверить `xcrun -f metal`, при необходимости скачать Metal toolchain с повтором.
2. Клонировать с повтором в `/Users/Shared/telegram-ios`, наложить патчи.
3. `cache/restore` каталога `~/bazel-disk`; записать `~/.bazelrc` с GC-флагом.
4. Временный keychain с p12, профили из секретов.
5. `Make.py --bazelUserRoot=/private/var/tmp/_bazel_containerhost --cacheDir=~/bazel-disk build --configuration=release_arm64 --codesigningInformationPath=… --continueOnError --buildNumber=…`.
6. `cache/save` с `if: always()`, только на `main`.
7. `age`-шифрование IPA, upload-artifact с хранением 7 дней.

Подписывать на первом этапе **внутри Bazel**, как Nagram: при ошибке подписи повтор берёт всё из кэша. Отдельный job re-sign (фиктивная подпись или `--disableProvisioningProfiles` + `codesign` по профилям) можно добавить позже [не проверено для 8–9 расширений].

**publish** (ubuntu): расшифровать, сгенерировать `manifest.plist` и OTA-страницу, rsync на VPS.

**Время:**
- холодная сборка 55–70 мин [проверено на форках];
- тёплая без изменений кода 4–10 мин [проверено: 3.5–7.3 мин];
- правка UI-модулей — 10–30 мин [оценка];
- правка TelegramCore/MtProtoKit — 45–55 мин [проверено на Nagram];
- повтор после падения на подписи — 5–10 мин [оценка].

### 5.3 Android (DrKLO 12.9.1, `aac1efcb83`)

**preflight** (ubuntu, секунды):
- патчи, `LAYER = 228` в TLRPC.java;
- RSA в Handshake.cpp, `APP_PACKAGE`;
- package в google-services.json;
- `keytool -list -v`: пароль и срок keystore.

**build** (`ubuntu-24.04`, 4 vCPU):
- JDK 17; `sdkmanager "ndk;27.2.12479018" "cmake;3.22.1"` с повтором. На образе есть только NDK 27.3/29/30 и CMake 3.31/4.1 [проверено]. Как вариант — образ из Dockerfile DrKLO в GHCR.
- Патч в `TMessagesProj_App/build.gradle`: `afat` → `abiFilters "arm64-v8a"`. Там же в cmake `arguments` добавить `-DCMAKE_C(XX)_COMPILER_LAUNCHER=ccache`.
- ccache через restore/save с `if: always()`, `CCACHE_BASEDIR`, `CCACHE_NOHASHDIR`; `setup-gradle`.
- `assembleAfatRelease` без release signingConfig, на выходе неподписанный APK.

**sign** (ubuntu, 1–2 мин): `zipalign` + `apksigner sign/verify`, keystore из environment secret.

**publish:** rsync на VPS или Obtainium.

**Время:** холодная 25–40 мин, тёплая 8–14 мин [оценка по YATGram: 2 ABI с ccache — 47 и 12–13 мин].

### 5.4 Desktop (tdesktop v7.0.9)

- **toolchain-linux.** Запускается только при изменении хэша `Telegram/build/docker/centos_env/**`. `ubuntu-24.04`, очистка диска, сборка образа и push в `ghcr.io/<you>/tdesktop-env:<hash>`. Займёт 2 ч 10 мин – 3 ч 40 мин [проверено на upstream docker.yml]. Образ хранится постоянно.
- **linux.** `docker run` этого образа, ccache через actions/cache, флаги `-D TDESKTOP_API_ID/HASH` и отключение автообновления (имя опции проверить в `cmake/options.cmake`). Job ≈ 15–20 мин [проверено: 19 мин, из них сборка 6].
- **windows** (`windows-2025`). Кэши ThirdParty, Libraries и Qt по хэшу `prepare.py`, как в upstream win.yml. Сборка библиотек при частичном промахе кэша — 52–56 мин, сборка клиента — 48–62 мин [проверено]. sccache (бэкенд GHA или R2) мог бы ускорить [не проверено с MSVC].
- **macOS** (`macos-26`, по желанию): около 70 мин [проверено], подпись ad-hoc, пользователям придётся обходить Gatekeeper.
- Если кэши Windows и macOS вместе не влезут в 10 ГБ, вынести их в отдельный репо или поднять лимит за $0.07/ГБ.

### 5.5 Web K/A
`ubuntu-24.04`, `npm ci` с кэшем, сборка 2–6 мин, rsync статики на VPS за Caddy/nginx (там же `/apiws`). Можно собирать сколько угодно, это бесплатно.

### 5.6 Нагрузка в месяц [оценка]
- iOS: 10 сборок, из них 2 холодные и 8 тёплых, ≈ 200–300 мин macOS.
- Android: 10 сборок ≈ 150 мин.
- Desktop: 5 × (Linux 20 + Windows 60 + macOS 70) ≈ 750 мин.
- Web: 50 × 5 ≈ 250 мин.
- Плюс 4 канарейки в неделю.

Для публичных репо всё это $0.

## 6. Что поменять в разделе 5 аудита
- Схему «self-hosted раннер на своём Mac + bazel-remote на Ryzen» заменить на «стандартные раннеры GitHub в публичных репо + actions/cache/GHCR». Удалённый кэш остаётся опцией на продакшн-VPS (bazel-remote + R2) или в BuildBuddy.
- Причина того, что «после ошибки всё собиралось заново», остаётся прежней: не было `--cacheDir`. В облаке к этому добавляется второе условие: кэш нужно сохранять **и при падении** (`cache/save` с `if: always()`).
- Шаги fail-fast из 5.2 аудита переходят в отдельный job `preflight` на ubuntu. Подпись и публикация выносятся в отдельные jobs.

## 7. Стоимость

| Сценарий | Сумма в месяц |
|---|---|
| Публичные репо, стандартные раннеры, actions/cache, GHCR | **$0** |
| Плюс расширение кэша сверх 10 ГБ | ≈ $0.5–1.5 |
| Плюс R2 для bazel-remote или sccache | $0 (бесплатный тариф) |
| Резерв: Codemagic | $0 в пределах 500 мин, дальше $0.095/мин |
| Ускоритель: Depot | $20 + $0.08/мин macOS (≈ $2 за холодную сборку iOS) |
| Если репо приватные | ≈ $15–25 без macOS-десктопа, ≈ $45–55 с ним [оценка] |
| Apple Developer Program | $99 в год, для ad-hoc он нужен в любом случае |

## 8. Риски
Подробный список — в поле risks. Главные:
- GitHub может изменить бесплатный тариф для публичных репо или удалить Xcode 26.2 из образов;
- 7 ГБ RAM и около 45 ГБ диска на macOS-раннере;
- вытеснение кэша через 7 дней;
- правки нижних модулей (TelegramCore/MtProtoKit) пересобирают почти всё;
- в публичном репо видны логи, артефакты и кэши;
- сбои Metal toolchain в Xcode 26;
- лимит ad-hoc у Apple: 100 устройств в год.

## Источники
- GitHub: [раннеры](https://docs.github.com/en/actions/reference/runners/github-hosted-runners), [лимиты](https://docs.github.com/en/actions/reference/limits), [кэш](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching), [биллинг Actions](https://docs.github.com/en/billing/concepts/product-billing/github-actions), [цены раннеров](https://docs.github.com/en/billing/reference/actions-runner-pricing), [повторные запуски](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/re-run-workflows-and-jobs), [секреты и форки](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets), [безопасное использование](https://docs.github.com/en/actions/reference/security/secure-use), [артефакты](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/download-workflow-artifacts), [Packages](https://docs.github.com/en/billing/concepts/product-billing/github-packages), [runner-images](https://github.com/actions/runner-images) (macos-26-arm64, macos-15-arm64, Ubuntu2404 README).
- Доказательства по сборкам (GitHub API): [Telegram-iOS build.yml](https://github.com/TelegramMessenger/Telegram-iOS/blob/master/.github/workflows/build.yml), [Nagram-iOS testflight.yml](https://github.com/NextAlone/Nagram-iOS/blob/HEAD/.github/workflows/testflight.yml) (run 39456858958), [pixia1234/telegram-ios](https://github.com/pixia1234/telegram-ios/blob/HEAD/.github/workflows/build-unsigned-ipa.yml), [ayugram_ios](https://github.com/neymaryshop-jpg/ayugram_ios), [tdesktop workflows](https://github.com/telegramdesktop/tdesktop/tree/v7.0.9/.github/workflows), [YATGram](https://github.com/et-ness/YATGram), [Nekogram](https://github.com/Nekogram/Nekogram/blob/HEAD/.github/workflows/build.yml).
- Исходники: Telegram-iOS `build-system/Make/Make.py`, `versions.json`, `Telegram/BUILD`, `.bazelrc` @release-12.9.2; DrKLO `TMessagesProj_App/build.gradle` @aac1efcb83.
- Кэши: [Bazel remote caching](https://bazel.build/remote/caching), [bazel-remote](https://github.com/buchgr/bazel-remote), [BuildBuddy pricing](https://www.buildbuddy.io/pricing), [R2 pricing](https://developers.cloudflare.com/r2/pricing/), [sccache S3/R2](https://github.com/mozilla/sccache/blob/main/docs/S3.md), [gradle/actions v6](https://blog.gradle.org/github-actions-for-gradle-v6).
- Облачные Mac: [Codemagic](https://docs.codemagic.io/billing/pricing/), [Depot pricing](https://depot.dev/pricing), [Depot runners](https://depot.dev/docs/github-actions/runner-types), [Scaleway Apple silicon](https://www.scaleway.com/en/pricing/apple-silicon/), [Scaleway FAQ (24 ч)](https://www.scaleway.com/en/docs/apple-silicon/faq).
- Диск и Metal: [очистка macOS-раннера](https://scrapbox.io/yuseiito/GitHub_Actions%E3%81%AEmacos-latest%E3%81%A7_%E3%83%87%E3%82%A3%E3%82%B9%E3%82%AF%E5%AE%B9%E9%87%8F%E3%81%8C%E4%B8%8D%E8%B6%B3%E3%81%97%E3%81%9F%E6%99%82%E3%81%AEWorkaround), [Electron PR 49118](https://releases.electronjs.org/pr/49118), [Apple forums: Metal в Xcode 26](https://developer.apple.com/forums/thread/802155), [CircleCI: Xcode 26.1](https://circleci.com/changelog/xcode-26-1-available/).


---

## Проверка фактов (независимый проверяющий)

| Утверждение | Статус | Поправка | Источник |
|---|---|---|---|
| 1. Standard GitHub-hosted runners are free and unlimited in public repos. macOS arm64 (macos-14/15/26/latest) has 3 CPU M1, 7 GB RAM and 14 GB SSD. Linux/Windows x64 have 4 CPU / 16 GB in public repos and 2 CPU / 8 GB in private ones. | confirmed | Exact quote: 'free and unlimited on public repositories'. The specs table matches. The xcode-27 label (arm64) also exists. | https://docs.github.com/en/actions/reference/runners/github-hosted-runners |
| 2. 6-hour limit per job; at most 5 concurrent macOS jobs on Free/Pro/Team; 10 GB cache per repo; 200 cache uploads and 1500 downloads per minute. | confirmed | Matches the page. Total concurrency is 20/40/60 for Free/Pro/Team, and the macOS limit is 5 on each. | https://docs.github.com/en/actions/reference/limits |
| 3. Cache entries not accessed for 7 days are deleted, and LRU eviction applies over the limit. The limit can go up to 10 TB at $0.07/GB-month above 10 GB. Caches from other tags and sibling branches cannot be restored. | confirmed | Billing applies only when the limit is set above 10 GB. Without a payment method, usage is blocked once the quota runs out. Runs on any ref can restore caches from the default branch. | https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching ; https://docs.github.com/en/billing/concepts/product-billing/github-actions |
| 4. The macos-26 arm64 image has Xcode 26.0.1–26.6 including 26.2 (release-12.9.2 needs xcode 26.2, bazel 8.4.2). macos-15 also has 26.2. | confirmed | macos-26 image 20261013.0424.1: the default is 26.6, and 26.2 (17C52) is installed. macos-15 image 20260907: 26.2 is installed, but the default is 16.4. In both cases you need an explicit xcode-select based on versions.json. versions.json@release-12.9.2: xcode 26.2, bazel 8.4.2, macos 26. | github.com/actions/runner-images images/macos/macos-26-arm64-Readme.md, macos-15-arm64-Readme.md; raw Telegram-iOS versions.json@release-12.9.2 |
| 5. Telegram-iOS forks build on standard runners. Nagram: job ~53 min, Bazel 46.5 min, 4936 of 7850 actions from disk cache, critical path 2032 s, cache ~1.7 GB. pixia1234: cold 59–65 min, warm 3.5–7.3 min. ayugram (macos-15): 53–68 min. | confirmed | Nagram job build-upload ran 16:22:43–17:15:40 (52m57s). Elapsed 2789.7 s, Critical Path 2031.79 s, '7850 processes: 4936 disk cache hit', cache 1715 MB. It used the xcode-27 label with Xcode 27, not 26.2. pixia: 59.0/64.7 min cold, 3.6/6.4 min warm. ayugram (macos-15) succeeded at 52.8 and 68.3 min. Note: even with 63% cache hits, Nagram took 53 min, which supports the '45–55 min after a core change' estimate. | GitHub API: NextAlone/Nagram-iOS run 39456858958 + job log; pixia1234/telegram-ios runs 22553640413/22556660482/22566692350/22706776332; neymaryshop-jpg/ayugram_ios runs 31898405179/31886299457 |
| 6. Telegram-iOS's own CI (build.yml, runs-on: macos-13) is broken: runs get cancelled after a day of waiting. | confirmed | Runs 37855605703, 29801477076 and 27213141382 were all 'cancelled' about 24 hours after creation. build.yml on master uses runs-on: macos-13. | GitHub API: TelegramMessenger/Telegram-iOS actions/runs; raw .github/workflows/build.yml |
| 7. --bazelArguments in Make.py@release-12.9.2 is parsed but never applied (add_additional_args is never called). --cacheDir and --cacheHost are mutually exclusive. --cacheHost adds --experimental_remote_downloader. | confirmed | add_additional_args is defined at line 117 and has no call sites, and self.additional_args is never read. cacheTypeGroup is add_mutually_exclusive_group. --cacheHost adds both --remote_cache and --experimental_remote_downloader. Make.py does not pass --nohome_rc or --bazelrc (startup args contain only --output_user_root), so ~/.bazelrc is read. | Telegram-iOS build-system/Make/Make.py@release-12.9.2 |
| 8. buildNumber goes only into BuildNumberInfoPlist, so a new build number triggers no recompilation. | wrong | In Telegram/BUILD it is indeed only BuildNumberInfoPlist. However, Telegram/prebuilt_watchos.bzl also reads ctx.var['buildNumber'] for the embedded watch app, and that action is marked no-remote/local, so it is never cached. Without an embedded watch app (no watch flags passed to Make.py), the claim holds in practice. | Telegram-iOS Telegram/BUILD and Telegram/prebuilt_watchos.bzl @release-12.9.2 |
| 9. Since Bazel 7.4, disk cache size is limited with --experimental_disk_cache_gc_max_size/--experimental_disk_cache_gc_max_age. | confirmed | The flags exist. However, GC runs only 'while idle between builds' (idle_delay defaults to 5 min). My inference is that on an ephemeral CI runner it may never run before the cache is saved. Prune explicitly before actions/cache/save, using the standalone src/tools/diskcache gc or deletion by atime. | https://bazel.build/remote/caching ; bazel src/tools/diskcache/README.md |
| 10. macos-latest has ~45 GiB free by default; removing extra Xcodes (~38 GB) and simulator runtimes (~51 GB) gives ~137 GiB. | confirmed | The figures match the cited sources, but they are third-party measurements, not GitHub documentation (GitHub only states 14 GB SSD). Electron PR 49118 reports 134–157 GB after cleanup. A ~1.7 GB Bazel cache fits without any cleanup. | scrapbox.io/yuseiito/... ; github.com/electron/electron/pull/49118 |
| 11. tdesktop: Linux job 19 min; centos_env image 2h10m–3h40m; Windows 48–62 min, Depot 16-core 17–20 min; macOS build 61 min, job 70 min. | confirmed | Linux (Rocky Linux 8) job: 18m47s. macOS job: 70 min. docker.yml: 2h08m and 3h41m. Windows jobs on windows-latest took 59–68 min in total, with the 'Telegram Desktop build' step at ~52 min. On depot-windows-latest-16, jobs took 19–25 min in total. Upstream tdesktop caches already take ~6.7 GB of the 10 GB, and there is no Windows x64 libs entry among them. | GitHub API: telegramdesktop/tdesktop runs 36877662083, 36877662026, 36877389368, 32276506741, docker.yml runs 39161123357/39304113948, actions/caches + cache/usage |
| 12. YATGram: cold ~47 min, warm 12–13 min. Partisan: 37–59 min. ubuntu-24.04 has NDK 27.3/29/30 and CMake 3.31/4.1, while DrKLO 12.9.1 needs NDK 27.2.12479018 and CMake 3.22.1. | confirmed | YATGram: 47.1 min cold, 12.4–12.6 min warm. Partisan: 36.6–58.8 min. Image 20261012: NDK 27.3.13750724 (default), 29, 30; SDK CMake 3.31.5/4.1.2. build.gradle@aac1efcb83 sets ndkVersion '27.2.12479018' explicitly but has no CMake 3.22.1. 3.22.1 is the default for AGP 8.6.1 (my inference), and build.gradle contains version '3.10.2' only in defaultConfig. | GitHub API: et-ness/YATGram, reborngenesis/Partisan-Telegram; runner-images Ubuntu2404-Readme.md; DrKLO TMessagesProj/build.gradle + build.gradle@aac1efcb83 |
| 13. Secrets other than GITHUB_TOKEN are not passed to fork workflows; GITHUB_TOKEN is read-only for such PRs; pull_request_target is dangerous. | confirmed | Quote: 'The GITHUB_TOKEN has read-only permissions in pull requests from forked repositories'. First-time contributors in public repos need approval from a maintainer. | https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets ; https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows ; https://docs.github.com/en/actions/reference/security/secure-use |
| 14. Artifacts can be downloaded by anyone signed in with read access, which for a public repo means any GitHub user. | confirmed | The docs say: 'Read access to the repository is required'. For a public repo, everyone has read access. The recommendation correctly keeps IPA/APK out of artifacts. | https://docs.github.com/en/actions/how-tos/manage-workflow-runs/download-workflow-artifacts |
| 15. Public GitHub Packages are free; Container registry storage and bandwidth are 'currently free', with at least a month's notice of changes. | confirmed |  | https://docs.github.com/en/billing/concepts/product-billing/github-packages |
| 16. BuildBuddy Personal: 100 GB of cache transfer, up to 10 users. | confirmed | The page does not explicitly say the 100 GB is monthly. | https://www.buildbuddy.io/pricing |
| 17. R2 free tier: 10 GB-month, 1M Class A, 10M Class B, free egress; paid $0.015/GB-month, $4.50/$0.36 per million operations. | confirmed |  | https://developers.cloudflare.com/r2/pricing/ |
| 18. bazel-remote: HTTP/1.1 + gRPC, S3 backend, htpasswd, mTLS, --allow_unauthenticated_reads, --max_size required, Remote Asset API very experimental, no Gradle support. | confirmed | Everything except 'no Gradle support' is confirmed. The README simply does not mention Gradle, so that part cannot be verified from the source, but it does not affect the recommendation. | https://github.com/buchgr/bazel-remote |
| 19. sccache supports R2 through the S3 backend (SCCACHE_ENDPOINT, SCCACHE_REGION=auto). | confirmed |  | https://github.com/mozilla/sccache/blob/main/docs/S3.md |
| 20. Caching in setup-gradle v6 is proprietary, free for public repos, and there is an MIT option cache-provider: basic. | confirmed | The blog does not mention basic, but the gradle/actions docs do: 'cache-provider: basic' is MIT and free for all repos. Enhanced is free for public repos and a 'Free Preview' for private ones, and the blog warns of possible future restrictions. | https://blog.gradle.org/github-actions-for-gradle-v6 ; github.com/gradle/actions docs/setup-gradle.md |
| 21. Codemagic: 500 free M2 minutes per month for personal accounts; M2 8 cores / 8 GB at $0.095/min; M4 10 cores / 16 GB at $0.114/min. | confirmed | The free minutes apply only to personal accounts (not Teams) and only to M2. | https://docs.codemagic.io/billing/pricing/ |
| 22. GitHub: standard macOS $0.062/min; macOS 5-core M2 Pro $0.102; 12-core $0.077; larger runners only on Team/Enterprise. | confirmed | Also: Linux 2-core $0.006/min, Windows 2-core $0.010/min (relevant to the private-repo estimate). The 12-core macOS runner is Intel. | https://docs.github.com/en/billing/reference/actions-runner-pricing |
| 23. Depot: macOS 8 CPU / 24 GB / 400 GB (M4 on depot-macos-26) at $0.08/min; Developer plan $20/month. | confirmed | Developer includes 2,000 minutes and macOS runners. The page does not say how included minutes are counted against macOS. | https://depot.dev/docs/github-actions/runner-types ; https://depot.dev/pricing |
| 24. Scaleway Mac mini M4-S: €0.22/h or €149/month; 24-hour minimum because of the macOS license. | confirmed |  | https://www.scaleway.com/en/pricing/apple-silicon/ ; https://www.scaleway.com/en/docs/apple-silicon/faq/ |
| 25. Since Xcode 26, the Metal toolchain is downloaded separately, and it has known CI failures. | confirmed | Stronger sources: actions/runner-images issues #13014 (open, 'Metal Toolchain on macOS 26 Runners with Xcode 26'), #14013 and #14548. Telegram-iOS compiles .metal files, and the Nagram log shows a separate step `xcodebuild -downloadComponent MetalToolchain`. This step is missing from the recommendation. | https://developer.apple.com/forums/thread/802155 ; github.com/actions/runner-images/issues/13014 ; Nagram-iOS job log run 39456858958 |
| 26. Failed jobs can be re-run within 30 days, at most 50 times, on the same GITHUB_SHA/GITHUB_REF. | confirmed |  | https://docs.github.com/en/actions/how-tos/manage-workflow-runs/re-run-workflows-and-jobs |
| 27. With actions/cache/save if: always(), a re-run after a late-stage failure continues from the Bazel disk cache. | unverifiable | The mechanism is plausible but has not been tested on Telegram-iOS. Requirements: a unique save key (for example with run_id-run_attempt) plus restore-keys by prefix, because save is skipped when the key already exists. The re-run must be in the same branch scope. | no primary source; inferred from Bazel disk cache + actions/cache docs |

**Итоговая рекомендация после проверки:**

Рекомендация в силе: всё собираем в облаке, на стандартных GitHub-hosted раннерах в публичных репозиториях. Это $0/мес, macOS M1 входит, своё железо не нужно. После проверки уточняю несколько пунктов.

1. Структура та же: 4 публичных репо (tg-ios, tg-android, tg-desktop, tg-web), в каждом UPSTREAM_REF, patches/, ci/apply-patches.sh и ci/*.sh, не зависящие от CI. Учтите, что в публичном репо всем видны код, патчи и логи сборок. Для GPL-клиентов исходники всё равно нужно публиковать. Секреты в лог не выводить.

2. iOS: runs-on: macos-26. Xcode 26.2 в образе есть, но по умолчанию стоит 26.6. Поэтому в preflight явный xcode-select по versions.json и проверка xcodebuild -version.
   - Обязательно добавить шаг `xcodebuild -downloadComponent MetalToolchain` с повтором и проверкой `xcrun --find metal`. На раннерах это известный источник сбоев (runner-images #13014, #14548), а Telegram-iOS компилирует .metal.
   - Кэш: actions/cache/restore с restore-keys по префиксу и actions/cache/save с if: always() и уникальным ключом (run_id-run_attempt). С уже существующим ключом save просто пропускается.
   - Флаги Bazel кладём в ~/.bazelrc, потому что --bazelArguments в Make.py 12.9.2 не применяется.
   - GC дискового кэша (--experimental_disk_cache_gc_max_size) срабатывает только после ≥5 мин простоя сервера Bazel и на одноразовом раннере может не успеть. Перед save кэш чистить явно (утилита src/tools/diskcache или удаление по atime) и держать запись около 2–3 ГБ.
   - Watch-приложение не встраивать: оно зависит от buildNumber и собирается мимо кэша.
   - Время: холодная сборка 55–70 мин, тёплая без изменений кода 4–10 мин, после правки TelegramCore/MtProtoKit около 45–55 мин. Для сравнения, Nagram собирался 53 мин даже при 63% попаданий в кэш.

3. Android: ubuntu-24.04. Через sdkmanager с повтором ставим NDK 27.2.12479018 (так в build.gradle) и CMake 3.22.1 (дефолт AGP 8.6.1), в образе их нет. Собираем arm64-v8a, ccache (restore/save always), setup-gradle v6: cache-provider: basic (MIT) или enhanced (для публичных репо бесплатно). Подписываем отдельным job через apksigner. Холодная сборка ~25–40 мин, тёплая ~8–14 мин.

4. Desktop: образ centos_env в GHCR (2–4 ч на версию зависимостей, публичный, бесплатно). Linux с ccache ~15–20 мин. Windows на windows-latest: job 59–68 мин, сама сборка ~52 мин. macOS-десктоп по желанию, ~70 мин. У upstream tdesktop кэши уже занимают ~6.7 из 10 ГБ, поэтому лимит надо мониторить. При нехватке есть три варианта: поднять лимит ($0.07/ГБ-мес, нужен способ оплаты), вынести macOS-десктоп в отдельное репо или ускорить Windows на Depot (16 ядер, job 19–25 мин, $0.064/мин).

5. Web K/A без изменений: ubuntu, npm-кэш, 2–6 мин, rsync на VPS.

6. Jobs: preflight (Xcode, MetalToolchain, профили/ключи, layer 228) → build → sign → publish. Повторы только для сетевых шагов. «Re-run failed jobs» доступен 30 дней, не больше 50 раз, на том же SHA. Продолжение с кэша после падения в принципе должно работать, но на Telegram-iOS это пока не проверено, проверить на первой же сборке.

7. Еженедельная cron-«канарейка» на основной ветке. Она держит кэши живыми (запуски с тегов могут читать кэш основной ветки) и рано ловит изменения образов.

8. Безопасность без изменений: секреты только в environment secrets с обязательным ревьюером, никаких pull_request_target, actions закреплены по SHA. IPA/APK не выкладывать в artifacts: их может скачать любой пользователь GitHub. Шифруем (age) или сразу отправляем на VPS.

9. bazel-remote, R2 и BuildBuddy понадобятся, только если не хватит 10 ГБ или будет мешать вытеснение через 7 дней.

10. Резервы: Codemagic даёт 500 бесплатных минут M2 в месяц, только личному аккаунту, M2 с 8 ГБ RAM. Depot: macOS M4 8 CPU / 24 ГБ за $0.08/мин плюс план $20/мес. Аренда Mac (Scaleway M4 €149/мес, минимум 24 ч) не нужна.

Стоимость: $0/мес плюс Apple Developer Program $99/год. Оценки для приватных репо ($15–55/мес) не проверены. Если понадобятся, считать от тарифов: macOS $0.062/мин, Linux 2-core $0.006, Windows 2-core $0.010 и 2000 бесплатных минут на тарифе Free.
