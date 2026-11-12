# Реестр правок веб-клиента (Web K)

Полный перечень того, чем сборка Pluma отличается от официального Telegram Web K: правки исходников апстрима, заменённые ресурсы, добавленные файлы и всё в репозитории клиента, что влияет на сборку. Назначение — при обновлении апстрима быстро найти, какие места сдвинулись и почему они правились.

Составлено 2026-11-03 по репозиторию `~/pluma-web`: прочитаны `ci/patch.py`, `ci/patch_test.py`, `ci/check_bundle.py`, все скрипты `ci/`, `brand/`, `e2e/`, workflow, README и вся история git (все ветки), а также брифы 012, 017, 020, 030 сервера.

## Общие сведения

- **Репозиторий:** `github.com/sckraaa/pluma-web` (локально `~/pluma-web`). Копии кода приложения в нём нет: CI скачивает апстрим и применяет патч.
- **Апстрим** (`upstream.env`): `UPSTREAM_REPO=https://github.com/morethanwords/tweb.git`, `UPSTREAM_REF=aa74832ed1976008d1ad1859d0b0cd671e75a7bd` (коммит «Build» 2026-11-08), `UPSTREAM_VERSION=2.3`, `UPSTREAM_BUILD=686` (`public/version` = `2.3 (686)`), `UPSTREAM_LAYER=233`, `NODE_VERSION=24.22.1`, `PNPM_VERSION=11.16.0`. Почему этот коммит: новейшая сборка на момент задачи 074; сервер обслуживает слои 228 и 233 одновременно (ADR 0045). До задачи 074 — `346844bf`, 2.2 (676), layer 228.
- **Как применяется патч:** `ci/fetch-upstream.sh` (чистый checkout закреплённого коммита в `./upstream`) → `ci/configure.sh` (выбирает цель `server`/`dev` и вызывает `python3 -I ci/patch.py --source upstream --upstream upstream.env --app config/app.env --server-url … --rsa-public-key … --target …`) → `ci/build.sh <target>` (typecheck, `vite build`, сборка сайта, `ci/check_bundle.py`, архив). Точка входа патча — `main()` → `patch(tree, c)` → `verify(tree, c)` → запись `.pluma-patch.json`.
- **Виды правок** (методы класса `Tree` в `ci/patch.py`). Любая правка, у которой не совпало число вхождений, роняет весь прогон с `PatchError`, где названы файл и ожидаемый текст:
  - `replace(rel, old, new, count=1)` — точная замена текста, `old` должен встретиться ровно `count` раз;
  - `sub(...)` — регулярное выражение с точным числом совпадений;
  - `replace_body(rel, head, tail, body, must_contain)` — замена тела между `head` и первым `tail` после него; `must_contain` — строки, которые обязаны быть в заменяемом блоке;
  - `replace_block(...)` — то же, но вместе с `head` и `tail`;
  - `replace_within(rel, head, tail, edits)` — точные замены только внутри блока;
  - `expect(rel, text, count)` — правки нет, только проверка, что текст, на который опирается патч, на месте;
  - `replace_file(rel, sha256, data)` — замена бинарного файла, только если у апстримного файла ожидаемый SHA-256; `data=None` удаляет файл;
  - `tree.write` — новый файл.
- **Тесты:**
  - `ci/fetch-upstream.sh && python3 -I ci/patch_test.py -v`. Без checkout закреплённого коммита в `./upstream` класс `UpstreamTest` пропускается, кроме CI (`CI=1`): там он не пропускается никогда.
  - `UpstreamTest.test_patch` применяет патч к закреплённому коммиту и проверяет результат.
  - `UpstreamTest.test_damage` портит по одному месту из списка `DAMAGE` и ждёт `PatchError` с именем файла: 84 случая на `main`, 118 на `task/020-parity-fixes`.
  - Остальное: `test_icon_changed`, `test_identity_colors`, `test_dev_server`, `test_env_file_of_upstream`, `KeyTest`, `ServerUrlTest`, `NodeEnginesTest`, `ConfigTest`, `TraceTest`, `TreeTest`, `BundleTest`.
  - На 2026-11-03 на `task/020-parity-fixes` проходят 32 теста за 48 с.
  - На 2026-11-04 на `task/053-saved-tags` (W-065) проходят 33 теста, в `DAMAGE` 134 случая.
  - На 2026-11-09 на `task/074-upstream` (2.3 (686), layer 233) проходят 38 тестов.
- **Проверки результата** — в двух местах, независимо от того, как делались правки:
  - `verify()` в `ci/patch.py` — по исходникам (W-057);
  - `ci/check_bundle.py` — по собранному сайту (W-056).

### Ветки: что где лежит (важно)

| Ветка | Голова | Что содержит |
|---|---|---|
| `main` (с неё собирается образ для прода) | `fe68fd5` 2026-11-02 | всё до `d6475fe` + W-048 (стикеры). **Нет** W-039…W-046 (цвета-идентичности, баннер дня рождения, строка профиля); синий аватар — в варианте W-034. |
| `task/020-parity-fixes` | `e9a6865` 2026-10-31 | всё до `d6475fe` + `cb01766` (W-043…W-046) + `e9a6865` (W-039…W-042, заменяет W-034). **Нет** W-048. |
| `fix/sticker-set-link-add` | `fe68fd5` | то же, что `main`. |

Ветки разошлись от `d6475fe`. По STATUS сервера ветка 020 ждёт слияния. При слиянии будут пересекаться конец `patch()` (вызовы `birthday_banner`/`birthday_row` и `sticker_set_toggle`) и начало `DAMAGE` в `ci/patch_test.py`: это вывод по диффам, слияние не пробовалось. Образ `ghcr.io/sckraaa/pluma-web:fe68fd56…` стоит на проде с 2026-11-02 22:59 UTC (бриф 030), значит, на web.pluma.chat правок 020 сейчас нет.

**2026-11-03: ветки сведены.** `task/020-parity-fixes` слита в `main` коммитом `ffda01d` без конфликтов, 32 теста патча проходят. Теперь в `main` есть и W-039…W-046, и W-048. Образ `ffda01d` на прод выкатывается вместе с ближайшей выкаткой сервера.

### Что проверять при обновлении апстрима

1. Новый коммит обязан иметь layer, который понимает сервер: `"layer":N` в `src/lib/mtproto/schema.ts` = `UPSTREAM_LAYER`. Иначе меняется сервер, а не только патч (ADR 0045).
2. Обновить вместе все поля `upstream.env`: `UPSTREAM_REF`, `UPSTREAM_VERSION`, `UPSTREAM_BUILD` (как в `public/version`), `PNPM_VERSION` (= `packageManager` в `package.json`), `NODE_VERSION` (должен проходить `engines.node`). Патч сверяет всё это сам (W-008).
3. `ci/fetch-upstream.sh && python3 -I ci/patch_test.py -v`. Каждое упавшее место искать в этом реестре по имени файла.
4. Флаги `import.meta.env.VITE_MTPROTO_*`: новый флаг или исчезнувший роняет патч (W-008). Решить, каким он должен быть при транспорте «только WebSocket».
5. Не появился ли у апстрима свой `.env.production.local` (W-002).
6. Новые следы Telegram — адреса DC, `*.web.telegram.org`, ключи, учётные данные (W-057, W-056), новые `telegram.org` (`SITE_KEEP`, W-070). Отдельно смотреть новые места, куда клиент сам ходит к Telegram, вроде `telegramMeWebManager` (W-007); `verify` их ловит только по списку известных следов.
7. Строки. Счётчики «Telegram» в `src/lang.ts` (272 заменяемых / 45 оставляемых) и `src/langSign.ts` (7 / 0) сломаются на любой новой строке. Каждую новую строку прочитать и решить вручную (W-020).
8. Иконки: если апстрим заменил любой файл из `ICONS` (SHA-256), проверить, что `brand/render.sh` рендерит файл того же назначения и размера, и обновить хэш (W-015).
9. Новые ссылки `tg:` в коде (`verify` падает на любой `'tg:`, `"tg:`, `` `tg: ``, `^tg:` вне комментариев) — сделать их `plm:` (W-025, W-026).
10. Цвета:
    - новые места с `color: #fff` на `var(--primary-color)` — добавить в `ON_PRIMARY` (W-036), иначе в тёмной теме белое окажется на светлом шалфее;
    - новые цвета тем в `themeController.ts`;
    - синий или бирюзовый Telegram в новых местах (на ветке 020 `verify` их ловит).
11. Баннер дня рождения и строка профиля: поменялась ли логика гейта Premium и подарков (W-043…W-046).
12. Настройки: не появились ли у апстрима новые строки, ведущие к сервисам Telegram (W-070).
13. После сборки: `ci/check_bundle.py` (CI делает сам) и `PLUMA_SERVER_DIR=~/teamgram-server e2e/run.sh` на стенде (W-062). Затем глазами в браузере: вход, QR (`plm://login`), светлая и тёмная темы.

---

## Сервер и ключ

### W-001. Один URL WebSocket для всех DC
- **Что и зачем:** каждый DC, тип соединения (client, download, upload) и режим (test, premium) идут на один сервер Pluma по `wss://<host>/apiws` (dev: `ws://localhost:11443/apiws`). URL проверяется в `parse_server_url`: `ws://` допускается только для `localhost`, `127.0.0.1` и `[::1]`.
- **Где:** `src/lib/mtproto/dcConfigurator.ts`.
  - `replace_body`: тело функции от `export function constructTelegramWebSocketUrl(_dcId: DcId, connectionType: ConnectionType, premium?: boolean) {\n` до `\n}\n`, `must_contain` `.web.telegram.org/${path}`. Новое тело — проверка `VITE_MTPROTO_HAS_WS`, `assertValidDcId(_dcId)`, `return '<url>'`.
  - `replace` комментария над проверкой dcId: блок `// A dcId can arrive from the service worker's \`stream/\` route… interpolated unchecked…` заменён текстом без примера `wss://kws1.evil.com/…`. Иначе в исходнике остался бы след `.web.telegram.org`. Проверка dcId остаётся: от неё зависит ключ хранилища.
- **Проверка:** `verify` — в `dcConfigurator.ts` ровно один литерал `wss?://`, и это URL сервера. `check_bundle` — в JS бандла множество `wss?://…` равно `{server_url}`. Тесты: `test_patch`, `test_dev_server`, `ServerUrlTest`, `DAMAGE` (сигнатура с `dcId`, `${route}` вместо `${path}`, «interpolated as is», второй `wss://`). Если апстрим изменит сигнатуру или формат URL, патч упадёт на `replace_body`.
- **Апстрим до правки:** `wss://${s}ws${dcId}${suffix}.web.telegram.org/apiws…` — свой хост на DC и тип соединения (kws1, pluto и т. п.). Официальный web.telegram.org/k делает так же, то есть ходит к Telegram.
- **Коммиты:** `facdd16` (2026-10-24), задача 012.

### W-002. HTTP-транспорт выключен
- **Что и зачем:** шлюз Pluma принимает MTProto только по WebSocket. По README и брифу 012, сборка апстрима по умолчанию стартует на HTTP.
- **Где:**
  - **Добавленный файл** `.env.production.local` (Vite читает его после `.env`): `VITE_MTPROTO_HAS_WS=1`, а `VITE_MTPROTO_HAS_HTTP`, `VITE_MTPROTO_HTTP`, `VITE_MTPROTO_HTTP_UPLOAD`, `VITE_MTPROTO_AUTO` пустые (`TRANSPORT_FLAGS`). `VITE_MTPROTO_WORKER` и `VITE_MTPROTO_SW` остаются как у апстрима (`UPSTREAM_FLAGS`). Если апстрим сам поставляет `.env.production.local`, патч падает.
  - `dcConfigurator.ts`, `replace_body` от `export class DcConfigurator {\n` до `  public chosenServers: Servers`, `must_contain` `sslSubdomains`, `private dcOptions`, `149.154.175.50`: удаляются списки адресов DC Telegram для HTTP.
  - `dcConfigurator.ts`, `replace_block` от `  private transportHTTP = (` до `\n  };\n`, `must_contain` `this.sslSubdomains`, `this.dcOptions`, `.web.telegram.org/`, `new HTTP(`: функция возвращает `undefined`.
  - `ci/build.sh` снимает все `VITE_*` из окружения, иначе Vite предпочтёт их файлам `.env`.
- **Проверка:** `verify` — `.env.production.local` равен `TRANSPORT_FLAGS`. `check_bundle` — в коде нет переключателей `noMultipleTransports=1` и `http=1` (`HTTP_TRANSPORT_SWITCHES`). Тесты: `test_env_file_of_upstream`, `DAMAGE` (адрес `149.154.175.51`, `transportHttp`, `new Http(`, `VITE_MTPROTO_HTTP_UPLOADS` в `apiManager.ts`).
- **Апстрим до правки:** HTTP по `https://<sub>.web.telegram.org/apiw1` или `http://149.154.x.x/apiw1` как основной или запасной транспорт. Значения флагов в `.env` апстрима здесь не приводятся: файлы `.env` не читались.
- **Коммиты:** `facdd16`, задача 012.

### W-003. Ключ сервера вместо ключей Telegram
- **Что и зачем:** клиент знает только RSA-ключ сервера Pluma. Режим `?test=1` ходит на тот же сервер с тем же ключом.
- **Где:** `src/lib/mtproto/rsaKeysManager.ts`.
  - `replace_body` от `export class RSAKeysManager {\n` до `  private publicKeysParsed: {`, `must_contain` `e8bb3305c0b52c6cf2afdf7637313489`, `private testPublicKeysHex`, `private publisKeysHex` (опечатка `publis` — апстримная). Новое тело: `publisKeysHex` с одним `{modulus, exponent}` и комментарием с отпечатком.
  - `replace`: удалён конструктор `if(Modes.test) { this.publisKeysHex = this.testPublicKeysHex; }`.
  - `replace`: удалён `import Modes from '@config/modes';`.
  - PEM (PKCS#1) переводится в hex в `parse_pkcs1_public_key`. Требуется 2048 бит. Отпечаток считается в `key_fingerprint` так же, как у tweb.
- **Проверка:** `verify` — ровно один `modulus: '` и это ключ сервера. `check_bundle` — modulus есть в коде. Тесты: `KeyTest` (отпечатки Telegram `0xD09D1D85DE64FD85`/`0xB25898DF208D2603`, hex совпадает с тем, что несёт tweb, dev-ключ `14037083214707814241`), `DAMAGE` (имя класса, модуль, `testKeysHex`, импорт `@/config/modes`).
- **Апстрим до правки:** прод- и тест-ключи Telegram, в тест-режиме тестовые. Официальный клиент — так же.
- **Коммиты:** `facdd16`, задача 012.

### W-004. Идентичность приложения и отказ от режима web.telegram.org
- **Что и зачем:** свои `api_id`/`api_hash` (из `config/app.env`, W-052). Ни один хост не считается доменом Telegram, поэтому не включаются учётные данные Webogram, push-ключ Telegram и переход на Web A.
- **Где:** `src/config/app.ts`.
  - `replace` `export const MAIN_DOMAINS = ['web.telegram.org', 'webk.telegram.org'];` → `export const MAIN_DOMAINS: string[] = [];`;
  - `replace` `  id: +import.meta.env.VITE_API_ID,` → `  id: 10004,`;
  - `replace` `  hash: import.meta.env.VITE_API_HASH,` → строковый литерал;
  - `sub` (регэксп) `if\(App\.isMainDomain\) \{ // use Webogram credentials then\n(?:  App\.\w+ = '?[\w-]+'?;\n){3}\}\n\n` → пусто: блок с `App.id = 2496` и т. д.
- **Проверка:** `verify` — в `app.ts` есть `id`, `hash`, `baseDcId: 1`. Во всех исходниках нет `import.meta.env.VITE_API_ID|VITE_API_HASH|VITE_PUSH_SERVER_KEY`. `check_bundle` — `api_hash` есть в коде. Тесты: `ConfigTest`, `DAMAGE` (добавленный `webz.telegram.org`, `Number(...)`, `String(...)`, `App.suffix = 'W'` в блоке).
- **Апстрим до правки:** `api_id`/`api_hash` из переменных сборки. На `web.telegram.org`/`webk.telegram.org` — учётные данные Webogram (2496), а посетителей из поисковиков он случайно отправляет на Web A (README).
- **Коммиты:** `facdd16`, задача 012.

### W-005. Базовый DC — 1
- **Где:** `src/lib/accounts/authKeyFingerprint.ts` (`BASE_DC`), `replace` `export const BASE_DC_ID = 2;` → `export const BASE_DC_ID = 1;`; `src/config/app.ts`, `expect` `  baseDcId: BASE_DC_ID as TrueDcId,`.
- **Зачем:** сервер Pluma — DC 1.
- **Проверка:** `verify` (`BASE_DC_ID = 1`), `DAMAGE` (`baseDcId: 4`, `BASE_DC_ID = 4`).
- **Апстрим до правки:** DC 2.
- **Коммиты:** `facdd16`, задача 012.

### W-006. Без «прогрева» DC 2–5 на экране входа
- **Где:** `src/pages/cards/SignInCard.tsx`, `replace` `      const dcs = new Set([1, 2, 3, 4, 5]);` → `      const dcs = new Set<number>();`.
- **Зачем:** у Pluma один DC. Апстрим заранее делает рукопожатия с DC 1–5.
- **Проверка:** `DAMAGE` (`new Set([1, 2, 3, 4, 5, 6])`).
- **Коммиты:** `facdd16`, задача 012.

### W-007. Без синхронизации сессии с t.me и telegram.me
- **Где:** `src/lib/telegramMeWebManager.ts`, `replace_body` от `  public setAuthorized(canRedirect: boolean) {\n` до `\n  }\n}\n`, `must_contain` `_websync_`, `'//t.me/' + path`, `loadScript(url)`. Тело заменено на `return;`.
- **Зачем:** после входа апстрим грузит скрипты `//t.me/_websync_…` и `//telegram.me/_websync_…` с сайтов Telegram.
- **Проверка:** `verify` — нет следа `_websync_`. `test_patch` — нет `loadScript(`. `DAMAGE` (сигнатура с `force?`, `_sync_`).
- **Коммиты:** `facdd16`, задача 012.

### W-008. Сверка апстрима с `upstream.env` и флагов транспорта (правки нет, только проверки)
- **Где:** начало `patch()`.
  - `"layer":(\d+)` в `src/lib/mtproto/schema.ts` = `UPSTREAM_LAYER`;
  - `public/version` = `"2.2 (676)"`;
  - `package.json`: `packageManager` = `pnpm@11.16.0`, `engines.node` принимает `NODE_VERSION` (`node_satisfies`);
  - множество `import.meta.env.VITE_MTPROTO_*` во всех исходниках (`Tree.sources()`) равно `TRANSPORT_FLAGS ∪ UPSTREAM_FLAGS`: новый флаг или исчезнувший — ошибка.
- **Проверка:** `NodeEnginesTest`, `DAMAGE` (layer 229, версия 2.3 (700), pnpm 11.17.0, engines `>=26.0.0`, `VITE_MTPROTO_HTTP_UPLOADS`).
- **Коммиты:** `facdd16`.

## Уведомления и пуши

### W-009. Web push без ключа Telegram
- **Что и зачем:** апстрим подписывается на web push с VAPID-ключом своей сборки или с GCM sender id манифеста. У сервера Pluma своего ключа web push пока нет. Уведомления при открытой странице работают.
- **Где:**
  - `src/config/app.ts`, `replace` `  pushServerKey: import.meta.env.VITE_PUSH_SERVER_KEY,` → `  pushServerKey: '',`;
  - `src/lib/webPushApiManager.ts`, `replace` в `public subscribe = (): Promise<PushSubscriptionNotify | void> => {\n    if(!this.isAvailable) {` → `if(!this.isAvailable || !App.pushServerKey) {`;
  - GCM sender id удаляется из манифестов (W-010).
- **Проверка:** `verify` (нет `VITE_PUSH_SERVER_KEY`, нет ключа и sender id Telegram из `TELEGRAM_TRACES["credential"]`), `DAMAGE` (`VITE_PUSH_KEY`, `async()`).
- **Открыто:** когда сервер начнёт слать web push (бриф 009: «Web push … не отправляются»), ключ придётся передавать в сборку. Сейчас пустой ключ зашит.
- **Коммиты:** `facdd16`, задача 012.

### W-010. Тексты push в service worker
- **Где:** `src/lib/serviceWorker/push.ts`, четыре `replace`:
  - `    push_message_error: 'Telegram is syncing in the background...',` → `Pluma is syncing…`;
  - `ctx.registration.showNotification('Telegram Web', {` → `'Pluma'`;
  - `  let title = obj.title || 'Telegram';` → `'Pluma'`;
  - `    title = 'Telegram';\n    body = lang.push_message_nopreview;` → `'Pluma'`.
- **Зачем:** имя приложения в уведомлениях.
- **Проверка:** `DAMAGE` есть для `showNotification('Telegram Web K'` и `title = 'Telegram Web'`. Для двух других замен отдельного случая нет, но точная замена с `count=1` всё равно упадёт при сдвиге. Пока web push выключен (W-009), эти тексты почти не видны.
- **Коммиты:** `bb3c634` (2026-10-29), задача 017.

## Брендинг и тексты

### W-011. Манифесты PWA
- **Где:** `public/site.webmanifest`, `public/site_apple.webmanifest`, функция `patch_manifest`. Файл разбирается как JSON и записывается заново с отступом 4.
  - Ожидается: `name` и `short_name` = `"Telegram Web"`, есть `description`; `scope_extensions` = `https://t.me`, `https://telegram.me`; есть `gcm_sender_id`; `screenshots` = `[assets/img/screenshot.jpg]`.
  - Результат: `name`, `short_name`, `description` = `Pluma`; `scope_extensions`, `gcm_sender_id`, `screenshots` удалены.
- **Зачем:** имя установленного приложения. Не перехватывать ссылки t.me. Без sender id push Telegram. Без скриншота интерфейса Telegram.
- **Проверка:** `verify` (имя, нет `t.me`), `check_bundle` (`name`/`short_name`), `DAMAGE` (`"name": "Telegram"`, `telegram.dog`, `gcm_sender`, `screenshot2.jpg`).
- **Коммиты:** `facdd16` (имя, scope, gcm), `8cd3f81` (2026-10-29, screenshots), задачи 012, 017.

### W-012. Заголовок страницы и описание (vite)
- **Где:** `vite.config.ts`, `replace_body` от `const handlebarsPlugin = handlebars({\n  context: {\n` до `\n  }\n});\n`, `must_contain` `title: 'Telegram Web'`, `url: 'https://web.telegram.org/k/'`, `origin: 'https://web.telegram.org/'`. Контекст стал `title: 'Pluma', description: 'Pluma'`: `url` и `origin` удалены.
- **Проверка:** `check_bundle` — `<title>Pluma</title>` в `index.html`. `DAMAGE` (`'Telegram Web K'`).
- **Коммиты:** `facdd16`, задача 012.

### W-013. Без канонического адреса в `index.html`
- **Где:** `index.html`, три `replace` на пустую строку: `    <meta property="og:url" content="{{url}}">`, `    <meta property="twitter:url" content="{{url}}">`, `    <link rel="canonical" href="{{origin}}">`.
- **Зачем:** адрес, с которого сервер раздаёт сайт, на этапе сборки неизвестен. У апстрима здесь адрес web.telegram.org.
- **Проверка:** `verify` — нет `{{url}}`/`{{origin}}`. `DAMAGE` (canonical с `{{url}}`).
- **Коммиты:** `facdd16`.

### W-014. Иконки и логотипы (ресурсы)
- **Что и зачем:** все иконки Web K заменены на иконки Pluma, имена файлов сохранены.
- **Где:** `ICONS` в `ci/patch.py` — 33 файла в `public/assets/img/`, `replace_file` по SHA-256 апстримного файла. Источник — `brand/web/assets/img/…` (W-061).
  - `android-chrome-{36,48,72,96,144,192,256,384,512}`, `apple-touch-icon(-precomposed).png`;
  - `favicon-16x16`, `favicon-32x32`, `favicon-194x194`, `favicon.ico`, `favicon_unread.ico`;
  - `icon_square_{192,384,512}.png`;
  - `logo.svg`, `logo_512.png`, `logo_filled.svg`, `logo_filled_rounded.png`, `logo_padded.svg`, `logo_plain.svg`;
  - `masked.svg`, `masked copy.svg`, `safari-pinned-tab.svg`;
  - `mstile-{70x70,144x144,150x150,310x150,310x310}.png`.
  - Хэши — в коде. У `masked.svg` и `safari-pinned-tab.svg` апстримный хэш одинаковый.
  - `SCREENSHOT`: `public/assets/img/screenshot.jpg` (SHA-256 `af2c7c54…`) удаляется.
- **Проверка:** `verify` — каждый файл побайтно равен `brand/web`, скриншота нет. `check_bundle` — то же в сайте. Тесты: `test_icon_changed` (изменённый `favicon.ico` роняет патч), `BundleTest`.
- **Если апстрим поменяет иконку:** SHA-256 не совпадёт, и патч упадёт. Нужно убедиться, что рендер Pluma подходит по назначению и размеру, и обновить хэш.
- **Коммиты:** `19c2741` (рендер), `8cd3f81` (замена), 2026-10-29, задача 017.

### W-015. Логотип на экране входа
- **Где:**
  - `index.html`, `replace_block` от `        <path id="logo" fill-rule="evenodd" d="M80,0 ` до `"/>\n`, `must_contain` `M114.262551,46.4516129`. Путь Telegram заменён на `<g id="logo" … style="stroke: var(--primary-text-color)" transform="translate(80 80) scale(.3878) translate(-260 -248)">` с кольцом и тремя перьями.
  - Знак строит `mark_logo()` из `brand/src/pluma-mark.svg`: требуется `viewBox="64 80 392 336"` и штрихи `#111111`, `#4A4A4A`, `#8A8A8A`, `#BDBDBD`. Серые переведены в `stroke-opacity`, чтобы знак читался и в тёмной теме.
  - `expect` в `src/pages/cards/SignInCard.tsx`: `<svg class={styles.logo} … viewBox="0 0 160 160">` и `<use href="#logo"/>`.
- **Проверка:** `verify` — `<g id="logo"` есть. `test_patch` — конкретный путь с `stroke-opacity="0.76"`. `DAMAGE` (без `fill-rule`, координаты, `#logo-round`).
- **Апстрим до правки:** самолётик Telegram, залитый одним цветом.
- **Коммиты:** `8cd3f81`, задача 017.

### W-016. Логотип в центре QR-кода (правки нет, только проверка)
- **Где:** `expect` в `src/helpers/qrCode/paintQrCode.ts`: ``text.replace(/(fill:).+?(;)/, `$1${logoColor}$2`)``.
- **Зачем:** код красит первый CSS `fill:` в `logo_padded.svg`. У файла Pluma (`brand/src/pluma-qr-logo.svg`, белый диск со знаком) CSS-заливки нет, поэтому он остаётся в своих цветах.
- **Проверка:** `DAMAGE` (флаг `/g`).
- **Связано:** нечитаемость QR из-за короткого токена исправлена на сервере, а не в клиенте (бриф 017, токен 34 байта).
- **Коммиты:** `8cd3f81`.

### W-017. Цвет плитки Windows
- **Где:**
  - `index.html`, `replace` `    <meta name="msapplication-TileColor" content="#2d89ef">` → `#5b7a5e`;
  - `public/browserconfig.xml`, `replace` `<TileColor>#2d89ef</TileColor>` → `#5b7a5e`.
- **Проверка:** `test_patch`, `DAMAGE` (`#2b5797` в обоих).
- **Коммиты:** `8cd3f81`.

### W-018. Счётчик непрочитанных на favicon
- **Где:** `src/lib/uiNotificationsManager.ts`, `replace` `    ctx.fillStyle = '#3390ec';` → `'#5b7a5e'` (акцент).
- **Проверка:** `DAMAGE` (`#3390EC`).
- **Коммиты:** `8cd3f81`.

### W-019. Имя «Pluma» во встроенных английских строках
- **Что и зачем:** каждое «Telegram» в **значениях** строк становится `PLUMA_APP_NAME` («Sign in to Pluma», «Pluma Premium», «Pluma Stars»). Ключи, комментарии и URL (`https://t.me/TelegramTips`) остаются.
- **Где:** `rename_strings()` для `STRINGS`. Строка вида `'key': value` (регэксп `_STRING_ENTRY`); слово — регэксп `_TELEGRAM_WORD`: не после `[\w./@-]`, но допускается после `\n`.
  - `src/lang.ts`: ожидается ровно 272 замены и 45 оставшихся «Telegram» (новые строки 2.3 прочитаны: кошелёк, бизнес, настройка Experimental, подсказки Premium);
  - `src/langSign.ts`: 7 замен, 0 оставшихся.
- **Проверка:** `verify` — ни одно значение не содержит слово. `check_bundle` — есть «Sign in to Pluma», нет «Sign in to Telegram» и «Scan with Telegram». `test_patch` проверяет `AppName`, `BoostingStars`, `Premium.Boarding.Peer.Title`. `DAMAGE` (новая строка `AppNameFull`, «Sign in» без Telegram, `t.me/Tips`).
- **При обновлении:** любая новая или удалённая строка с Telegram меняет счётчик, и патч падает, пока строку не прочтут.
- **Не здесь:** языковые пакеты с сервера (задача 021, ADR 0025) — замена в них делается на сервере.
- **Коммиты:** `bb3c634`, задача 017.

### W-020. Подвал меню: ссылка на исходники и версия
- **Где:** `src/components/sidebarLeft/index.ts`.
  - `replace` `  btnMenuFooter.href = 'https://github.com/morethanwords/tweb/blob/master/CHANGELOG.md';` → `'https://github.com/sckraaa/pluma-web'` (`SOURCE_URL`; исходники сборки по GPL);
  - `expect` `  t.textContent = getAppVersionText();`;
  - `src/config/app.ts`, `replace` ``  return `Telegram Web${App.suffix} ${App.version} (${App.build})`;`` → ``  return `Pluma Web ${App.version} (${App.build})`;`` (та же подпись и в «Настройках», и в `Experimental`).
- **Проверка:** `test_patch`, `DAMAGE` (`github.com/tweb`, пробел перед `${App.suffix}`).
- **Коммиты:** `bb3c634`.

### W-021. Уведомление об обновлении
- **Где:** `src/lib/appManagers/apiUpdatesManager.ts`, `replace` ``const pre = `**Telegram Web${App.suffix} ${langStr} ${newVersion}**\n\n`;`` → `**Pluma Web …**`.
- **Проверка:** `DAMAGE`.
- **Коммиты:** `bb3c634`.

### W-022. Служебный аккаунт 777000 до ответа сервера
- **Где:** `src/lib/appManagers/appMessagesManager.ts`, `replace` `        first_name: 'Telegram',\n        phone: '42777'` → `first_name: 'Pluma'`.
- **Зачем:** заглушка пользователя 777000, пока не пришёл настоящий от сервера. На сервере он тоже «Pluma» (бриф 017, `c5751d23`).
- **Проверка:** `DAMAGE` (`Telegram Notifications`).
- **Коммиты:** `bb3c634`.

### W-023. Пункты меню «Telegram Features» и «Report Bug» убраны
- **Где:** `src/components/sidebarLeft/index.ts`, одна точная замена всего фрагмента от `    }, {\n      icon: 'help',\n      text: 'TelegramFeatures',` до `a.href = 'https://bugs.telegram.org/?tag_ids=40&sort=time';` и конца второго пункта. Пункты заменены комментарием.
- **Зачем:** пункты ведут на сервисы Telegram (t.me/TelegramTips, bugs.telegram.org).
- **Проверка:** `test_patch` — нет `'ReportBug'`. `DAMAGE` (`bugs.telegram.org/`).
- **При обновлении:** фрагмент длинный (21 строка), поэтому любое изменение в соседних пунктах меню роняет замену.
- **Коммиты:** `bb3c634`.

### W-070. В «Настройках» нет раздела помощи; `telegram.org` под счётом (задача 074)
- **Что и зачем.** С 2.3 в конце настроек есть раздел: FAQ на telegram.org и «Ask a Question» с чатом волонтёров Telegram (`help.getSupport`). У остальных клиентов Pluma раздела нет (Android, iOS, Desktop D-030), поэтому и здесь.
- **Где** (`help_section()` в `ci/patch.py`): `src/components/sidebarLeft/tabs/settings.tsx` — импорт `askQuestion`, `openTelegramFaq` и блок от `{/* help, the way tdesktop ends its main settings */}` до `</Section>`; `src/lib/settingsSearch/generated.ts` — записи `AppSettingsTab:AskAQuestion` и `AppSettingsTab:TelegramFaq`. Строки в `lang.ts` и `askQuestion.ts` остаются недостижимыми.
- **Счёт `telegram.org`.** `SITE_KEEP` и проверка в `verify`: число хостов `telegram.org` по файлам в коде без комментариев; новый останавливает патч, пока кто-то не решит, ведёт ли он к сервисам Telegram. Что оставлено и почему — комментарии в `SITE_KEEP`.
- **Поиск по настройкам.** Записи `AppSettingsTab:Premium.Boarding.Title`, `TelegramBusiness`, `MenuTelegramStars`, `MenuTelegramStarsTon`, `Chat.Menu.SendGift` (`SETTINGS_SEARCH_SALE_ROWS`) не показываются: сами строки скрыты `premium_purchase_blocked`, у поиска такого условия нет. Правка в `src/lib/settingsSearch/openers.ts` (набор `SALE_ENTRIES`) и `registry.ts` (фильтр рядом с `WALLET_ENTRIES`), а не в `generated.ts`: индекс генерируется из вкладок при старте сборки (`scripts/settings-search-plugin.mjs`), и правка `generated.ts` стиралась бы. Записи FAQ и Ask a Question исчезают сами, вместе с разделом в `settings.tsx`.
- **Ссылки на экранах** (`TELEGRAM_SITE_EDITS`, применяются до `rebrand`):
  - `src/lang.ts`, `ChatList.Main.EmptyPlaceholder.SubtitleNoContacts`: у аккаунта без контактов на пустом списке чатов были ссылки на приложения Telegram; текст остался, ссылок нет;
  - `src/lang.ts`, `ChatAutomation.HeroDescription`, и `sidebarLeft/tabs/chatAutomation.tsx`: нет «Learn More» на telegram.org. Вкладку открывает поиск по настройкам (`settingsSearch/openers.ts`), даже когда сервер не отвечает на `account.getConnectedBots`.
- **Как проверяется.** `verify_help_section`, `test_patch`, `test_no_telegram_site_on_screens`, счёт `SITE_KEEP`, `DAMAGE` (импорт, заголовок блока, записи поиска; новый `telegram.org` в `helpers/copyContact.ts`; опоры ссылок на экранах).

### W-071. Нет сообщения о новой версии (задача 074)
- **Что и зачем.** В 2.3 есть `public/changelogs/{en,ru}_2.3.md`. `apiUpdatesManager.deliverChangelog` после обновления кладёт текст файла текущей версии в служебный чат каждого вошедшего аккаунта, а там написано Telegram и Telegram Premium и дана ссылка на t.me. Файла нет — сообщения нет (404 считается «у версии его нет»).
- **Где.** `CHANGELOGS` и `rebrand()` в `ci/patch.py`: оба файла удаляются по SHA-256 (`replace_file`); `tree.expect` на `` fetch(`changelogs/${lang}_… `` в `apiUpdatesManager.ts`.
- **Как проверяется.** `verify` и `check_bundle` падают на `changelogs/*_<текущая версия>.md`, `test_patch`, `DAMAGE` (оба файла и строка `fetch`), `BundleTest.test_rejects`.
- **При обновлении.** Новая версия со своим файлом роняет `verify`: добавить файл в `CHANGELOGS`. Файлы прежних версий не читаются и лежат как есть.

### W-024. Ссылки `plm://`, которые создаёт клиент
- **Что и зачем:** приложения Pluma работают с `plm://`, а `tg://` остаётся за Telegram, установленным рядом. QR входа Pluma не откроет Telegram, и наоборот (бриф 017). Веб-страница не может зарегистрировать `plm://` в системе (браузеры принимают только `web+`).
- **Где:** `PLM_LINKS`, точные замены `tg:` → `plm:`:
  - `src/pages/cards/SignQRCard.tsx` `'tg://login?token='` (1);
  - `src/components/instantView.tsx` `'tg://iv?url='` (2);
  - `src/lib/richTextProcessor/wrapTelegramRichText.ts` `'tg://iv?url='` (1);
  - `src/components/wrappers/joinVoiceChatAnchor.ts` `` `tg://voicechat? `` (1);
  - `src/lib/richTextProcessor/wrapRichText.ts` `'tg://bot_command?` (1);
  - `src/lib/richTextProcessor/index.ts` `'tg://search_hashtag?` (1);
  - `src/lib/settingsSearch/link.ts` `'tg://settings'` (1);
  - `src/lib/richTextProcessor/wrapRichText.ts` `'tg://sendgrams?to='` (1, адрес TON в сообщении, 2.3);
  - `src/components/chat/inputEditor/richMessage.ts` `url.startsWith('tg://user?')` (1, упоминание ссылкой в редакторе rich-сообщений, 2.3);
  - `src/helpers/string/normalizeLinkUrl.ts` `  'tg:',` и `url.protocol === 'tg:'` (допустимые схемы ссылки, 2.3);
  - `src/lib/appManagers/utils/richMessage/validateRichMessage.ts` список схем (2.3);
  - `src/lib/internalLinkProcessor.ts` `element.href.replace(/^tg:(?:` (путь неизвестной ссылки, 2.3);
  - `src/lib/richTextProcessor/inferRichMessageNoAutolink.ts` `|tg:\/\/|` (что композитор считает кандидатом на автоссылку, 2.3).
- **Проверка:** `verify` — нигде в коде (без комментариев) нет `'tg:`, `"tg:`, `` `tg: ``, `^tg:` и `tg://` (в том числе `tg:\/\/` в регулярном выражении); есть `'plm://login?token='`. `check_bundle` — есть `plm://login?token=`, нет `tg://login`. `DAMAGE` (`login/`, `iv/`, `settings/`, новая константа `'tg://resolve'`).
- **Коммиты:** `abb939c` (2026-10-29), задача 017.

### W-025. Клиент открывает `plm:`-ссылки вместо `tg:`
- **Где:** `src/lib/richTextProcessor/wrapUrl.ts`, `replace` `  } else if((tgMatch = url.match(/^tg:(?:\/\/)?(.+?)(?:\?|$)/))) {` → `/^plm:…/`. Дальше ссылки идут в те же обработчики `tg_*`.
- **Следствие:** ссылки `tg://` в сообщениях клиент больше не разбирает как внутренние.
- **Проверка:** `test_patch`, `DAMAGE` (регэксп без `?`).
- **Коммиты:** `abb939c`.

### W-066. Ссылки `pluma.chat` вместо `t.me`
- **Что и зачем:** задача 026. `pluma.chat` — хост ссылок Pluma, как `t.me` у Telegram: сервер отдаёт на нём приглашения, ссылки на бусты и `me_url_prefix`. Web K не читает `me_url_prefix` (он есть только в `layer.d.ts`) и собирает ссылки из литералов `https://t.me/`. Правка делает две вещи. Клиент открывает ссылку `pluma.chat` внутри приложения так же, как ссылку `t.me`. Ссылки, которые клиент сам делает для копирования, показа, отправки и QR, ведут на `https://pluma.chat/`.
- **Где:** функция `link_host()` в `ci/patch.py`, константы `LINK_HOST`, `LINK_BUILDERS`, `LINK_KEEP`. Номера строк — закреплённый апстрим `346844bf`.
  - **Распознавание.** `src/lib/richTextProcessor/matchTelegramUrlHost.ts` — единственное место, где решается, что хост ссылки «телеграмный». Через него идут `wrapUrl.ts:19` (ссылки в сообщениях, `appImManager.openUrl`, `#?tgaddr=`) и `helpers/addAnchorListener.ts:71`; патч сверяет оба вызова (`expect`).
    - После `export const TELESCOPE_LINK_HOST = 'telesco.pe';` добавляется `export const PLUMA_LINK_HOST = 'pluma.chat';`.
    - В начале `matchTelegramUrlHost` добавляется `if(matchUrlHost(url, [PLUMA_LINK_HOST])?.subdomain === '') return {prefix: undefined};`.
    - Это точное совпадение хоста, без поддоменов: `web.pluma.chat` — сайт, а не юзернейм, как `durov.t.me`. Поэтому `pluma.chat` не добавлен в `TELEGRAM_LINK_HOSTS`: тот пропускает любой поддомен, а `T_ME_PREFIXES` (`appManagers/constants.ts:28`) отбрасывает только `web`, `k`, `z`, `a`.
    - Страницы самого сайта `pluma.chat` не открываются как профили. Константа `LINK_SITE_PATHS` в `ci/patch.py` (тот же список, что у других клиентов): корень `/` и первые сегменты пути `iphone`, `ipad`, `android`, `mac`, `windows`, `linux`, `web`, `pero`, `app`, `en`, `dl`, `ios`, `collect`, `assets`, `_l`, `.well-known`, файлы `favicon.svg`, `og.png`, `apple-touch-icon.png`, `404.html`, `progress.json`. Из неё патч пишет `const PLUMA_SITE_PATHS = new Set([...])` после `PLUMA_LINK_HOST`, а в проверке точного хоста `pluma.chat` сначала берётся первый сегмент `url.pathname` в нижнем регистре: пустой или из набора — `matchTelegramUrlHost` отдаёт `undefined`. Тогда `wrapUrl` не даёт `onclick`, ссылка остаётся обычным `<a target="_blank">`, а `appImManager.openUrl` (`appImManager.ts:1873`) открывает её `safeWindowOpen`, как любую внешнюю. Только для `pluma.chat`: `t.me/iphone` по-прежнему юзернейм. Исправление по ревью задачи 026.
    - `TELEGRAM_LINK_HOSTS = ['t.me', 'telegram.me']` не меняется (`expect`). Ссылки `t.me` по-прежнему открываются внутри приложения и ищутся на сервере Pluma, как у апстрима. `telegram.dog` у Web K в списке нет и не было.
  - **Исходящие ссылки** (`LINK_BUILDERS`, в тексте якоря `t.me/` заменяется на `pluma.chat/`, число вхождений точное):
    - `helpers/copyContact.ts` (2.3, импортируется в `helpers/dom/anchorCopy.ts`): `export const T_ME = 'https://t.me/';`. Это юзернейм и ссылка профиля в информации о чате и в настройках.
    - Ссылки на сообщения из «Копировать ссылку»: `components/chat/contextMenu.ts:1916` и `components/popups/forward.tsx:174`, `    let url = 'https://t.me/';`.
    - Строка ссылки в информации группы или канала: `components/peerProfile.tsx:992` (тема) и `:1006` (юзернейм).
    - QR профиля:
      - `helpers/qrCode/paintQrCode.ts:110` `buildTelegramUserQrUrl`;
      - `components/popups/myQrCode/link.ts` (`+<телефон>`, 2.3) и `components/popups/myQrCode.tsx` (`c/<id>`).
      - `components/sidebarLeft/tabs/businessChatLinks.tsx` (ссылки бизнес-аккаунта: `copyLink('https://t.me/' + path)` и подпись, 2.3).
    - Истории, альбомы историй и коллекции подарков:
      - `lib/appManagers/utils/stories/storyLink.ts` (`/s/`, 2.3: один генератор для списка и просмотрщика) и `components/stories/profileList.tsx` (`/a/`);
      - `components/stargifts/profileList.tsx:341` (`/c/`).
    - Наборы стикеров и эмодзи: `components/popups/stickers.tsx:281`.
    - Публичная ссылка чата строкой приглашения: `components/sidebarRight/tabs/chatInviteLinkShared.ts:57`.
    - Префикс поля публичной ссылки: `components/sidebarRight/tabs/chatType.tsx:141` `const placeholder = 't.me/';`. `UsernameInputField` берёт длину префикса из `head`, поэтому длина не важна.
    - «Последние действия», смена публичной ссылки: `components/chat/bubbleParts/adminLogsResolver/index.tsx:304, 327, 341, 344` — якорь `t.me/${action.`, 4 вхождения.
    - Ссылка на трансляцию канала: `lib/rtmp/url.ts:18, 21`.
    - Пост розыгрыша в транзакции: `components/popups/starsPay.tsx:255` `https://t.me/c/…`. Это ссылка на пост канала Pluma, а не на функцию Telegram.
    - Подпись ссылки нового бота: `components/popups/createBot/CreateBotPopup.tsx:210` `t.me/{fullUsername()}`.
    - Встроенные английские строки `src/lang.ts`. Это не новые строки, ключи те же; правится только английский текст, как в W-019:
      - `:1350` `'PublicLinksLimitSubtitle'`: `'Reserve up to %d pluma.chat/username links'`;
      - `:1851` `'GroupDescription3'`: `'Public links such as pluma.chat/title'`.
  - **Исправление строки приглашения** `components/peerProfile.tsx:1014`.
    - Апстрим: `exportedInvite.link.slice(exportedInvite.link.indexOf('t.me/'))`. Для `https://pluma.chat/+hash` `indexOf` даёт `-1`, и строка показывает последний символ ссылки.
    - Патч: `exportedInvite.link.replace(/^https?:\/\//, '')`. Ссылка остаётся той, что дал сервер, без схемы; `onClick` (`:1019`) добавляет `https://`, как у апстрима.
  - **Оставлены `t.me`** (`LINK_KEEP`, число хостов `t.me`/`telegram.me`/`telegram.dog` в коде без комментариев, по файлам):
    - список хостов `matchTelegramUrlHost.ts` (2).
    - Внутренние ссылки. Их тут же разбирает `wrapUrl`, и они не показываются:
      - упоминание `wrapRichText.ts:676`;
      - якорь темы `wrapTopicThreadAnchor.ts:12`;
      - ссылка из мини-приложения `helpers/getWebViewTgLink.ts:5`.
    - Ссылки на функции самого Telegram:
      - `lang.ts`: BotFather, `TelegramFeaturesUrl` (2);
      - `giftLink.tsx` (2), `premium/promoSlide.tsx` — `giftcode`; `popups/premium.tsx` — счёт-фактура, сразу уходит во внутренний маршрут; `stories/viewer.tsx` — NFT-подарок, сразу уходит во внутренний маршрут; `validateRichMessage.ts` — основание для разбора относительной ссылки; `lib/wallet/tonConnectWire.ts`, `vendor/widget-sandbox/**` (API виджетов, кошелёк не выпущен; 2.3);
      - `starGiftInfo.tsx` (2), `stargiftsGrid.tsx` (2), `starsPay.tsx:491` — `nft`;
      - `aiEditorPopup/styleTab.tsx` — `addstyle`;
      - `editBot.tsx` — `botfather`;
      - `frozen.tsx` — `t.me/telegram`.
    - Данные песочницы окон `components/popupSandbox/…` — страница разработки.
- **Без правки (апстрим):** ссылки `https://pluma.chat/…` в сообщениях — внешние: у них нет внутреннего обработчика (`wrapUrl` отдаёт `onclick` undefined), их открывает браузер, а не приложение. Скопированные ссылки на профиль, сообщение, набор стикеров и QR ведут на `t.me`. Строка приглашения частной группы при ссылке сервера на `pluma.chat` показывает один символ.
- **Не делается:**
  - `scope_extensions` PWA для `pluma.chat` (решение 026: без него; W-012 убрал `t.me` из манифестов).
  - Ссылки, которые приходят с сервера: приглашения, бусты, `freeze_appeal_url`, `contacts.exportContactToken`, `chatlists`. Их хост задаёт сервер.
- **Проверка:**
  - `verify`. В `matchTelegramUrlHost.ts` есть `PLUMA_LINK_HOST = 'pluma.chat'` и проверка точного хоста, а в ней ровно один раз набор `PLUMA_SITE_PATHS` и проверка первого сегмента; `pluma` нет в `TELEGRAM_LINK_HOSTS`. В каждом файле исходников (без комментариев) число `t.me`/`telegram.me`/`telegram.dog` равно `LINK_KEEP`, в остальных файлах оно 0. Значит, новая ссылка `t.me` у апстрима роняет патч, пока её не отнесут к `LINK_BUILDERS` или `LINK_KEEP`. В `peerProfile.tsx` нет `exportedInvite.link.indexOf(`.
  - `test_patch`: проверка хоста, `anchorCopy`, `contextMenu`, QR, 4 места журнала действий, строка приглашения, строка `lang.ts`; оставленные упоминание, `giftcode` и BotFather.
  - `DAMAGE`: 8 случаев. Это сигнатура вызова `matchUrlHost`, список хостов, `addAnchorListener` без `matchTelegramUrlHost`, `anchorCopy`, одно из 4 мест журнала, `indexOf` строки приглашения, новая ссылка `t.me` в `forward.tsx`, лишний хост в `getWebViewTgLink.ts`.
  - 2026-11-05: `python3 -I ci/patch_test.py` — 33 теста, OK (UpstreamTest не пропущен, 142 случая `DAMAGE`). Без вызова `link_host()` `test_patch` падает на `verify`.
  - Патч на копии `reference-src/tweb` (`346844bf`, цель `dev`, одноразовый ключ) проходит с `verify`. После него `tsc --noEmit` проходит без ошибок.
  - vitest на той же копии: `src/tests/matchTelegramUrlHost.test.ts` и `src/tests/wrapUrl.test.ts` апстрима плюс разовый тест `pluma.chat`, 79 тестов, OK. Что проверено:
    - `pluma.chat`, `http://`, верхний регистр и порт — внутренняя ссылка без префикса;
    - `web.pluma.chat`, `durov.pluma.chat`, `pluma.chat.evil.com`, `pluma.chat@evil.com`, `user@pluma.chat`, `ftp://` — внешние;
    - `wrapUrl` отдаёт `im`, `joinchat`, `addstickers`, `addemoji`, `addlist`, `boost` для путей `pluma.chat`;
    - `addAnchorListener` передаёт путь.
    - С апстримным `matchTelegramUrlHost.ts` разовый тест падает.
  - Разовый тест в репозиторий клиента не входит: CI клиента тесты апстрима не запускает.
  - 2026-11-06, страницы сайта: `test_patch` сверяет набор `PLUMA_SITE_PATHS` строкой целиком и проверку пути; без `"pero"` в `LINK_SITE_PATHS` он падает. Патч на свежей копии `346844bf` проходит с `verify`, `tsc --noEmit` без ошибок. vitest: тесты апстрима `matchTelegramUrlHost`, `wrapUrl` и разовый тест — 90 тестов, OK. Разовый тест: все 21 путь сайта, корень с запросом и без, `IPhone`, `PLUMA.chat/Windows/`, вложенные (`en/iphone`, `dl/x.apk`, `.well-known/…`) — `undefined` и без `onclick`; `durov`, `durov/12`, `iphones`, `+abcdef`, `joinchat/…`, `addstickers/…`, `addemoji/…`, `addlist/…`, `boost/…`, `c/1/2` — внутри приложения с прежним `onclick`; `t.me/iphone`, `t.me/windows`, `t.me/web` — `im`. Без проверки пути 28 случаев сайта падают.
  - Сборка (`vite build`) и проверка в браузере не выполнялись.
- **При обновлении:** новая ссылка `t.me` или изменившееся их число роняет `verify` с именем файла. Нужно решить, делает ли эту ссылку клиент для пользователя (тогда `LINK_BUILDERS`) или она внутренняя либо Telegram (тогда `LINK_KEEP`). Если апстрим начнёт читать `me_url_prefix`, исходящие замены можно убрать.
- **Коммиты:** `fc49343`, влито в `main` (`4109bc2`, PR #2), выкачено на web.pluma.chat 2026-11-06, задача 026.

### Цвета: тема «графит и шалфей»

Палитра выбрана владельцем 2026-10-29 (бриф 017). Пресеты *Day*, *Dark Blue* и акценты, которые пользователь выбирает сам, остаются как у апстрима, кроме цветов-идентичностей (W-039). Исходящие пузыри сплошные: Web K не рисует градиентные пузыри, владелец согласился (бриф 017).

### W-026. Цвета светлой и тёмной тем по умолчанию
- **Где:** `src/helpers/themeController.ts`, `replace_within` в блоках `  day: {\n` … `\n  },\n` и `  night: {\n` … `\n  },\n`, по одной точной замене `'<ключ>': '<цвет>'` (`THEME_COLORS`).
  - day: `primary-color` `#3390ec`→`#5b7a5e`, `message-out-primary-color` `#5CA853`→`#5b7a5e`, `message-background-color` `#ffffff`→`#f1f2f0`, `primary-text-color` `#000000`→`#111111`, `secondary-text-color` `#707579`→`#6b6f6b`, `saved-color` `#359AD4`→`#5b7a5e`, `background-color` `#f4f4f5`→`#f4f5f4`, `border-color` `#dfe1e5`→`#dfe2df`, `secondary-color` `#c4c9cc`→`#c4c9c4`.
  - night: `primary-color` `#8774E1`→`#9dbb9f`, `message-out-primary-color` `#8774E1`→`#b2ceb4`, `message-background-color` `#212121`→`#232623`, `surface-color` `#212121`→`#131513`, `primary-text-color` `#ffffff`→`#e8ece8`, `secondary-text-color` `#aaaaaa`→`#9aa29a`, `saved-color` `#8774E1`→`#9dbb9f`, `background-color` и `body-background-color` `#181818`→`#0c0e0c`, `border-color` `#0f0f0f`→`#070807`, `secondary-color` `#707579`→`#6e756e`, `input-search-background-color` `#181818`→`#0c0e0c`.
  - `link-color` с версии 2.3 в таблице нет: апстрим красит ссылки акцентом (`applyAppColor({name: 'link-color', hex: newAccentHex})`), поэтому ссылки — шалфей акцента (`#5b7a5e`, `#9dbb9f`), а не прежние `#4f6e52`, `#b2ceb4`.
- **Проверка:** `test_patch`, `DAMAGE` (`#3390EC`).
- **Коммиты:** `3460637` (2026-10-29), задача 017.

### W-027. `--on-primary-color`: текст и значки на заливке акцентом
- **Что и зачем:** акцент тёмной темы — светлый шалфей, белое на нём не читается. Тема ставит `--on-primary-color`: тёмный `#131513`, если яркость акцента больше 0,65, иначе белый.
- **Где:**
  - `themeController.ts`, `replace` начала строки импорта из `@helpers/color` (до `getAccentColor`) — добавлен `computePerceivedBrightness`.
  - `themeController.ts`, `replace` после `applyAppColor({ name: 'primary-color', hex: newAccentHex, darkenAlpha: 0.04 });` добавлен `element.style.setProperty('--on-primary-color', …)`.
  - `src/scss/base.scss`: после `  --badge-text-color: #fff;\n  --original-ripple-color: …` добавлено `  --on-primary-color: #fff;`.
  - Потребители — W-035, W-036.
- **Проверка:** `test_patch`, `DAMAGE` (порядок импорта, `darkenAlpha: 0.05`, `--badge-text-color: white`).
- **Коммиты:** `3460637`.

### W-028. Исходящий пузырь светлой темы без пересыщения
- **Где:** `themeController.ts`, `replace` фрагмента `    let newMessageOutBackgroundColor = mixColors(myMessagesAccent, surfaceForMessage, messageLightenAlpha);\n\n    if(!isNight/*  || true */) {`.
  - Добавлен `isBubbleColor` (не ночь, L ≥ 85, S ≤ 30): такой цвет берётся как есть, без смешивания и насыщения.
  - Условие стало `if(!isNight && !isBubbleColor)`.
- **Зачем:** апстрим смешивает цвет сообщения с фоном и поднимает насыщенность на 63 пункта, и `#E3EAE2` становится ярко-зелёным.
- **Проверка:** `DAMAGE` (`if(!isNight) {`).
- **Коммиты:** `3460637`.

### W-029. Время и галочки исходящих в тёмной теме — цвет outbox accent
- **Где:** `themeController.ts`, `replace` `      hex: isNight ? '#ffffff' : outgoingGradient ? keepReadableOnOutgoingMessageGradient(outgoingAccent, outgoingGradient) : outgoingAccent,` → `hex: isNight && !accentColor2 ? '#ffffff' : outgoingGradient ? … : outgoingAccent` (`outgoingAccent` апстрима и есть outbox accent, если он задан).
- **Апстрим до правки:** ночью всегда белый.
- **Проверка:** `DAMAGE` (`'#fff'`).
- **Коммиты:** `3460637`.

### W-030. Настройки тем по умолчанию: акцент, пузырь, обои, highlightingColor
- **Где:** `src/config/state.ts`, точные замены `THEME_SETTINGS`.
  - day: `accent_color: 0x3390ec, message_colors: [0x5CA853]` → `0x5B7A5E`, `[0xE3EAE2]`, добавлено `outbox_accent_color: 0x5B7A5E`; обои `0xdbddbb/0x6ba587/0xd5d88d/0x88b884` → `0xd5ddd3/0x8fa992/0xc9d4c5/0xa3b8a4`.
  - night: `0x8774E1, [0x8774E1]` → `0x9DBB9F`, `[0x2C3A2D]`, `outbox_accent_color: 0xB2CEB4`; обои `0xfec496/0xdd6cb9/0x962fbf/0x4f5bd5` → `0x9dbb9f/0x4f6e52/0x7fa082/0x5b7a5e`.
  - `baseThemeClassic`/`baseThemeNight` (hsla для служебных сообщений и строки заголовка установленного приложения) → значения среднего по новым обоям.
- **Проверка:** `test_patch`, `DAMAGE` (`0x3390EC`, `0x4f5bd6`, `299.14`).
- **Коммиты:** `3460637`.

### W-031. Перевод тем, сохранённых ранней сборкой
- **Где:** `src/lib/appManagers/utils/state/loadState.ts`.
  - `replace` импорта из `@config/state` — добавлен `SETTINGS_INIT`;
  - `replace` после блока `theme.settings = [theme.settings as any]; migrated = true; } });` — цикл: если у сохранённой темы акцент, единственный цвет сообщения или обои равны умолчаниям Telegram, они меняются на умолчания Pluma, `migrated = true`.
- **Зачем:** тема в браузере, сохранённая до перекраски, сама не сбрасывается.
- **Проверка:** `DAMAGE` (`type AppTheme`, `theme.settings = [theme.settings];`).
- **Коммиты:** `3460637`.

### W-032. Синий значков строк настроек — акцент
- **Где:** `src/helpers/rowIconBackground.ts`, `replace` `  blue: '#2196F3',` → `'#5b7a5e'`.
- **Следствие:** значки сессий в «Устройствах» тоже получают акцент (README).
- **Проверка:** `DAMAGE`.
- **Коммиты:** `3460637`.

### W-033. Переменные `base.scss`: кольцо историй, галочка выбора, ссылки и время исходящих
- **Где:** `src/scss/base.scss`, точные замены.
  - `--avatar-color-story-unread-from: #34c76f; …-to: #3da1fd;` → `#9DBB9F`, `#5B7A5E`;
  - `--message-checkbox-color: #61c642;` → `var(--primary-color)`;
  - `--message-out-link-color: #fff; --message-out-status-color: #fff; --message-out-time-color: rgba(255, 255, 255, .6);` → `var(--message-out-primary-color)`, `var(--message-out-primary-color)`, `var(--message-out-status-color)`.
- **Проверка:** `DAMAGE` (`#3DA1FD`, `#61C642`, `.5`).
- **Коммиты:** `3460637`.

### W-034. Синий аватар — вариант ветки `main` (заменён W-039 на ветке 020)
- **Где:** `src/scss/base.scss`, `replace` `  @include avatar-color("blue", #5CAFFA, #408ACF);` → `#7FA082, #6F8F72` (`// pluma: sage, not blue`).
- **Статус:** есть на `main` (`3460637`). На `task/020-parity-fixes` правку удалил `e9a6865`, вместо неё W-039 (`#7FA082`→`#5B7A5E`).
- **Проверка:** `DAMAGE` (`#408ACE`).

### W-035. Значки-счётчики на акценте
- **Где:** точные замены с добавлением `color: …` (комментарий `// pluma`):
  - `src/scss/partials/_badge.scss`, в `&-primary { background-color: var(--primary-color); }` — `var(--on-primary-color)`;
  - `src/scss/partials/_slider.scss`, в `.badge { background-color: var(--primary-color); }` — `var(--on-primary-color)`;
  - `src/scss/partials/_chatlist.scss`, `.is-muted.backwards .dialog-subtitle-badge-pollvote` — `var(--on-primary-color)`;
  - `src/scss/partials/_chatlist.scss`, `.no-unmuted-topic .dialog-subtitle-badge-pollvote` — `var(--badge-text-color)`.
- **Проверка:** `DAMAGE` (`background:` вместо `background-color:`).
- **Коммиты:** `3460637`.

### W-036. Белый на акцентной заливке → `--on-primary-color` (24 правила)
- **Где:** `ON_PRIMARY` — пары (файл, точный многострочный фрагмент правила). Внутри фрагмента регэксп `_WHITE_ON_PRIMARY` должен найти ровно одно `color:`/`stroke:` со значением `#fff`, `white`, `var(--white)` или `var(--badge-text-color)`; оно меняется на `var(--on-primary-color)`. Затем точная замена фрагмента в файле.
  - `_button.scss` ×2, `_chat.scss` ×3 (`.btn-send`, команды, ещё одна кнопка; значки `&-badge` в `_button.scss` и `_row.scss` апстрим удалил в 2.3, клавиатура ботов в 2.3 без заливки акцентом);
  - `_chatInlineHelper.scss`, `_chatMarkupTooltip.scss`, `_chatPinned.scss`, `_checkbox.scss` (`stroke`), `_poll.scss`, `_searchSuper.scss`, `_topbarSearch.scss`, `_voiceRecordingPanel.scss`, `_leftSidebar.scss`;
  - `popups/_datePicker.scss` ×2, `popups/_giftPremium.scss`;
  - `components/call/callLogo.module.scss`, `featureDetailsIconSticker.module.scss`, `popups/sendGift.module.scss`, `popups/starGiftValue.module.scss` (заливка теперь `--primary-button-color`), `popups/webAppLocationAccess.module.scss`, `wallet/tonConnect/tonConnect.module.scss` (буква вместо значка, 2.3), `chat/inputEditor/style.scss` (ручка ячейки таблицы, 2.3).
  - Точные тексты — в `ci/patch.py`.
- **Проверка:** `test_patch` (`.btn-color-primary`), `DAMAGE` (порядок свойств в `_chat.scss`, `white` в `_button.scss`, `_checkbox.scss`, `#fff` в `webAppLocationAccess`).
- **При обновлении:** новые места «белое на `--primary-color`» патч сам не найдёт: их надо искать вручную (`grep -n "primary-color" -A3 | grep "#fff"`).
- **Коммиты:** `3460637`.

### W-037. Выбранный чат в списке
- **Где:** `src/scss/partials/_chatlist.scss`, `replace_within` от `      &.active {\n        --background: var(--primary-color) !important;\n` до `\n      }\n`:
  - `#fff` → `var(--on-primary-color)` (ровно 11; в 2.3 добавились `.folder-tag` и обводка `.dialog-subtitle-badge-bot-app`);
  - `color: white;` (1);
  - `rgba(255, 255, 255, .7)` и `.2` → `color-mix(in srgb, var(--on-primary-color) 70%|20%, transparent)`.
- **Проверка:** `test_patch` (в блоке не осталось `#fff`), `DAMAGE` (`.6`, `--color: white`).
- **Коммиты:** `3460637`.

### W-038. Пресеты тем и прочие цвета — без правки
Записано, чтобы не искать: кроме перечисленного, цвета апстрима не меняются. Пресеты тем (`src/config/themePresets.ts`) патч не трогает. На ветке 020 `verify` проверяет только, что там не осталось синего и бирюзового Telegram, заменённых в W-039.

### W-039. Цвета-идентичности: аватары синего и бирюзового (только ветка 020)
- **Что и зачем:** из семи цветов, которые Telegram даёт пользователю или чату по id (`getPeerColorIndexByPeer`: `peer.color`, иначе `id % 7`), синий становится шалфеем, а бирюзовый — teal без синевы, во всех темах. Остальные пять и id ≥ 7 остаются как есть. Решение владельца — выровнять с Android (бриф 020, «Равномерность платформ»).
- **Где:** `identity_colors()`.
  - `expect` в `src/lib/appManagers/utils/peers/dialogColors.ts` (2.3: вынесен из `getPeerColorById.ts`) списка `export const DialogColors = ['red', 'orange', 'violet', 'green', 'cyan', 'blue', 'pink'] as const;`;
  - `src/components/wallet/wallet.module.scss`, `replace` `background: linear-gradient(#70a7db, #408acf);` → градиент синей идентичности (`--peer-avatar-blue-top`, `-bottom`), синий Telegram в кошельке 2.3;
  - `src/scss/base.scss`, `replace`:
    - `  @include avatar-color("cyan", #53edd6, #28c9b7);` → `#5BCBC4, #2C9C98`;
    - `  @include avatar-color("blue", #5CAFFA, #408ACF);` → `#7FA082, #5B7A5E`.
  - Таблица `IDENTITY_COLORS`: одна строка на цвет, сверяется с апстримным значением.
- **Проверка:** `verify` — заменённые цвета Telegram (`53edd6`, `28c9b7`, `309EBA`, `5CAFFA`, `408ACF`, `368AD1`) не встречаются нигде в исходниках, ни как CSS, ни как число. `IdentityColorsTest`, `test_identity_colors`, `DAMAGE` (порядок `DialogColors`, регистр cyan, `_avatar.scss` с `#5cafFA`, `themePresets.ts` с `0x368ad1`/`0x28C9B7`).
- **Апстрим до правки:** синий `#5CAFFA→#408ACF`, бирюзовый `#53edd6→#28c9b7`. Официальный Telegram — те же цвета.
- **Коммиты:** `e9a6865` (2026-10-31), задача 020.

### W-040. Цвета имён идентичностей, своя палитра для тёмных тем (только ветка 020)
- **Где:** `getPeerColorById.ts`.
  - Регэксп `_NAME_COLORS` ищет ровно одну строку `const DialogColorsFg: Array<string[]> = [[...] ×7];` (2.3: своя константа, раньше `,` в общем объявлении). Цвета cyan (`#309EBA`→`#1F8A80`) и blue (`#368AD1`→`#4F6E52`) сверяются и заменяются точной заменой всей строки.
  - `replace` `const _DialogColorsFg = DialogColorsFg;` → `const DialogColorsFgNight = {4: ['#53CCC6'], 5: ['#9DBB9F']}` и `const _DialogColorsFg = DialogColorsFg.slice();`. Копия нужна потому, что `setPeerColors` пишет цвета темы прямо в `DialogColorsFg`.
  - `replace` `  let newColors = replaceColors(_DialogColorsFg.slice(), peerColorOptions);\n  if(themeController.isNight()) {` → ночью берутся `DialogColorsFgNight`, поверх — цвета сервера (`help.getPeerColors`).
- **Зачем:** у Web K один цвет имени на все темы. Тёмные имена бирюзового `#53CCC6` (значение ночной темы Android) — решение на подтверждение (бриф 020).
- **Известное:** Web K применяет цвета имён только после ответа `help.getPeerColors`. Сервер его пока не отдаёт, поэтому имена в группах не окрашены вовсе (бриф 020, задача 024).
- **Проверка:** `test_patch`, `test_identity_colors`, `DAMAGE` (`#3E88F7`, восьмой цвет, `[...DialogColorsFg]`, `peerColorOptions || []`).
- **Коммиты:** `e9a6865`, задача 020.

### W-041. Синий градиент как просто синий — архив и иконка его окна (только ветка 020)
- **Где:** `IDENTITY_AS_PLAIN`, `replace` `background: linear-gradient(#5CAFFA, #408ACF);` → `linear-gradient(var(--peer-avatar-blue-top), var(--peer-avatar-blue-bottom))` в `src/components/archiveDialog.module.scss` и `src/components/featureDetailsIconSticker.module.scss`.
- **Проверка:** `test_patch`, `DAMAGE` (`10%`, `90%`).
- **Коммиты:** `e9a6865`.

### W-042. Белый значок на градиентах иконки возможностей (только ветка 020)
- **Где:** `featureDetailsIconSticker.module.scss`.
  - `expect` — ровно 3 вхождения `  &:global(.background-gradient-`;
  - `replace` для `avatar`, `archive`, `stories` — добавлено `color: white;`.
- **Зачем:** W-036 перевёл значок этой иконки на `--on-primary-color`, а на градиентах он должен остаться белым, как у апстрима.
- **Проверка:** `test_patch`, `DAMAGE` (четвёртый градиент `premium`, `background-gradient-peer`).
- **Коммиты:** `e9a6865`.

### W-074. Значок верификации в цветах Pluma (задача 080)
- **Что и зачем:** аккаунт Pluma (@support) получает `user.verified`, и значок рядом с его именем должен быть «как у Telegram, только в цветах Плюмы» (владелец, задача 080). Форма розетки — Telegram, меняются только цвета: розетка — акцент темы (шалфей `#5B7A5E` в светлой, `#9DBB9F` в тёмной), галочка — цвет «на акценте» (`--on-primary-color`, W-027): белая на светлой теме, `#131513` на светлом шалфее тёмной, как у кнопок (W-036).
- **Апстрим:** значок рисует один `generateVerifiedIcon.ts` (через `generateTitleIcons` и строку верификации профиля): два `<use>` на символы `index.html` — квадрат `#verified-icon-check` (`M8 8H18V18H8V8Z`) под розеткой `#verified-icon-background`, из которой галочка вырезана (`evenodd`), так что галочка — это квадрат, видный сквозь вырез. Цвета — `src/scss/base.scss`: `&-check { fill: #fff; }`, `&-background { fill: var(--primary-color); }`. Отсюда шалфей розетки в темах Pluma был и без правки (W-026); белая галочка на светлом шалфее тёмной темы не читалась. Выбранный чат (W-037 уже переводит его `fill: #fff !important` на `--on-primary-color`) и шапка профиля поверх фото выворачивают розетку и прячут галочку — это остаётся как у апстрима. Строка верификации в профиле (`.profile-bot-verification`) рисует розетку серым цветом текста (`fill: currentColor`) с белой галочкой.
- **Где:** `VERIFIED_BADGE` и `VERIFIED_BADGE_SHAPE` в `ci/patch.py`, вызов в `rebrand()` после W-037.
  - `src/scss/base.scss`, `replace` всего правила `.verified-icon { &-svg …; &-check { fill: #fff; } &-background { fill: var(--primary-color); } }` → `&-check { fill: var(--on-primary-color); }`.
  - `src/scss/partials/_profile.scss`, `replace` `    .verified-icon {\n      &-background {\n        fill: currentColor;\n      }\n    }\n` (строка верификации) — добавлено `&-check { fill: #fff; }`: серая розетка — не акцентная заливка, галочка остаётся белой, как у апстрима (тот же приём, что W-042).
  - `expect` того, на чём держатся цвета: в `generateVerifiedIcon.ts` классы `verified-icon-background`/`verified-icon-check` у двух `<use>` и порядок `svg.append(use2, use);`, в `index.html` — квадрат `#verified-icon-check`. Если апстрим перерисует значок (галочка поверх розетки, другие классы), сборка упадёт.
- **Места показа:** все через этот компонент и эти правила — список чатов, шапка чата, профиль, списки участников, поиск, упоминания, приглашение в чат (`joinChatInvite`), похожие каналы. Отдельной картинки значок не рисует: `public/assets/img/icon-verified.svg` (синий `#33a8e5`) в коде апстрима не используется (`grep` по `src/` и `index.html` пуст), поэтому не перекрашивается.
- **Темы:** *Day*, *Dark Blue* и выбранные пользователем акценты остаются как у апстрима (W-038): розетка в них — их акцент, галочка — `--on-primary-color`, который тема считает от яркости акцента.
- **Проверка:** `verify_verified_badge` — оба правила на месте, и ни в одном `.scss` нет белой галочки (`&-check { fill: #fff|white }`), кроме строки профиля. `test_verified_badge` (правила, выбранный чат, откат каждой правки и лишняя белая галочка в `_chatBubble.scss` роняют `verify`; без правки тест падает), `DAMAGE` (оба правила, классы `<use>`, порядок слоёв, путь символа галочки). Полный прогон `ci/*_test.py` на закреплённом `aa74832e`: 44 теста, OK.
- **Не проверено:** вид в браузере (сборка на этой машине не запускается); проверить в списке чатов, шапке и профиле @support в светлой и тёмной темах.
- **Коммиты:** — (ветка `task/080-verified-badge` в `pluma-web`, не закоммичено).

### W-075. Статус аккаунта поддержки: «service account» вместо «support» (задача 080)
- **Статус.** Ветка `task/080-service-account` репозитория `pluma-web` (от `origin/main` `ac0bf8f`), не закоммичено, не влито, не выпущено (2026-11-11).
- **Что и зачем.** Решение владельца 2026-11-11: аккаунт поддержки (`@support`, `user.support = true`, без статуса «в сети») там, где у человека «был(а) недавно», показывает «service account» — не «support» Telegram и не «service notifications». Текст — ключ локализации на всех языках, не литерал.
- **Где** (`SERVICE_ACCOUNT_STRING`, `service_account()` в `ci/patch.py`, вызов в `patch()` после `no_ai_editor`; апстрим `aa74832e`):
  - `src/lang.ts`, `replace` `  'SupportStatus': 'support',\n` → `  'SupportStatus': 'service account',\n`. Меняется значение существующего ключа; нового ключа и правок кода нет.
  - `expect` в `src/components/wrappers/getUserStatusString.ts`: `      if(user.pFlags.support) {\n        key = 'SupportStatus';\n        break;\n      }\n` (`SERVICE_ACCOUNT_STATUS`) — то, на чём держится текст.
- **Остальные языки.** Web K встраивает только английский (`lang.ts`, `langSign.ts`), всё остальное берёт с сервера: `langPack.ts`, `loadLangPack` запрашивает пакет `web` (сервер отдаёт `webk`) на любом языке, включая `en`, и `getLangPackAndApply` кладёт строки сервера после встроенных, так что строка сервера перекрывает встроенную. Поэтому нужна запись на сервере: `pkg/langpack/override.json`, пакет `webk`, ключ `SupportStatus`, 37 языков (таблица — `docs/tasks/080-service-account-strings.md`), и `pluma-admin langpack import` после выката. Без неё пакет сервера вернёт слово Telegram («support» / «поддержка») и на английском.
- **Кто получает текст.** Ключ читает только `getUserStatusString` (строка 35), после ботов (`user.pFlags.bot` → `Bot`) и до статусов «был(а)…». Аккаунт служебных уведомлений (`SERVICE_PEER_ID`, 777000) идёт раньше, своим ключом `Peer.ServiceNotifications`, и не тронут.
- **Где рисуется.** Всё через `getUserStatusString`: шапка чата и профиль (`appImManager.ts:4242`, `getUserStatus`; для `support` строка «печатает…» и зелёный «в сети» не показываются — `appImManager.ts:4244`), контакты (`sidebarLeft/contactsList.tsx:352`), выбор людей (`appSelectPeers.tsx:1339`), поиск и участники (`appSearchSuper.ts:2150, 2571`, `sortedUserList.ts:67`), новая группа (`sidebarLeft/tabs/newGroup.tsx:241`), чёрный список (`blockedUsers.tsx:56`, если нет имени пользователя), права участника (`permissionsPeerDialog.ts:33`), списки реакций и окна (`popups/reactedList.tsx:91`, `popups/richButton.tsx:88`).
- **Апстрим без правки.** «support» во всех этих местах; на других языках — перевод Telegram из пакета сервера.
- **Проверка.** `verify_service_account(sources)`: в `lang.ts` текст ключа — «service account», старого нет; ветка `support` в `getUserStatusString.ts` на месте; по всем исходникам без комментариев `'SupportStatus'` встречается только в `lang.ts` и `getUserStatusString.ts`, по разу (`SUPPORT_STATUS_USES`). `UpstreamTest.test_service_account` (текст, ветка, строка служебных уведомлений не тронута; возврат «support», другой ключ у статуса и ключ в чужом месте роняют `verify`; упоминание в комментарии — нет; без правки тест падает), четыре порчи в `DAMAGE` (строка `lang.ts`, ключ ветки `support`, ключ у ботов, ключ в `appImManager.ts`).
- **Если апстрим сдвинет.** Другой текст ключа, статус поддержки другим ключом или `SupportStatus` в новом месте → `PatchError` с именем файла: прочитать новое место и решить, годится ли там «service account».
- **Не проверено.** `pnpm run typecheck`, сборка и браузер (на этой машине клиент не собирается); сверка с сервером (импорт пакета `webk` с `override.json`).
- **Коммиты:** — (не закоммичено).

## Premium и платные функции

Контекст: Pluma не продаёт Premium и подарки. Сервер ставит `premium_purchase_blocked` в `help.getAppConfig` (задачи 014, 015). Решение владельца по баннеру — вариант А (бриф 020, пункт про баннер дня рождения). Всё в этом разделе есть **только на ветке `task/020-parity-fixes`** (`cb01766`, 2026-10-31).

### W-043. Баннер дней рождения: нажатие открывает чат, а не подарок
- **Где:** `src/components/sidebarLeft/birthdaySuggestions.tsx`, функция `birthday_banner()`.
  - Сначала `expect` опор: в `src/lang.ts` строки `'SendMessageTitle': 'Send message'` и `'Suggestion.BirthdayContacts.Today': 'Birthday today'`; в `src/components/popups/pickUser.tsx` `export default function showPickUserPopup(options: PopupPickUserOptions) {`; в `src/lib/appImManager.ts` `public async setPeer(options: Partial<ChatSetInnerPeerOptions> = {}, animate?: boolean): Promise<boolean> {`.
  - Импорты: `showSendGiftPicker` и `showSendGiftPopup` → `showPickUserPopup`; удалены `toastNew` и `useAppState`; добавлен `appImManager`.
  - Большая точная замена от `  const nearbyContacts = createMemo(() => [` до конца `onClick` (`showSendGiftPicker({…});\n  };\n`):
    - один именинник — `appImManager.setPeer({peerId})`, как нажатие в списке чатов (`appDialogsManager`, без `openInner`);
    - несколько — `showPickUserPopup` с `titleLangKey: 'SendMessageTitle'`, `peerType: ['custom']`, списком сегодняшних, взятым в момент нажатия, подписью «Birthday today», без поиска; выбор открывает чат.
- **Апстрим до правки:** нажатие открывает окно подарка или выбор получателя подарка; в список попадают и вчерашние/завтрашние.
- **Проверка:** `verify` — в файле нет `premium_purchase_blocked`, `showSendGift`, `popups/sendGift`, `appGiftsManager`, `Suggestion.BirthdayContacts.Subtitle`, `…Dismissed`. `test_patch` — набор строк есть, а `Gift`, `appConfig`, `toastNew`, `nearbyContacts`, `createEffect`, `setInnerPeer` отсутствуют. `DAMAGE` — 17 случаев по этому файлу и опорам.
- **Коммиты:** `cb01766`, задача 020.

### W-044. Баннер: подпись «Send message» и закрытие без тоста о подарках
- **Где:** тот же файл.
  - `replace` `<PendingSuggestion.Subtitle>{i18n('Suggestion.BirthdayContacts.Subtitle')}</PendingSuggestion.Subtitle>` → `{i18n('SendMessageTitle')}`;
  - `replace` `    toastNew({langPackKey: 'Suggestion.BirthdayContacts.Dismissed'});` → комментарий.
- **Остаётся как у апстрима:** закрытие прячет баннер на день, `help.dismissSuggestion` с `BIRTHDAY_CONTACTS_TODAY` (`test_patch` проверяет `closable={props.onDismiss}` и `appPromoManager.dismissSuggestion`).
- **Апстрим до правки:** подпись «Send them a Gift.»; после закрытия тост, что подарок можно отправить позже в Настройках.
- **Коммиты:** `cb01766`.

### W-045. Баннер показывается без продажи Premium
- **Где:** тот же файл.
  - Импорт `solid-js` без `createEffect`;
  - удалено `const [{appConfig}] = useAppState();`;
  - `canLoadBirthdayContacts` (`!appConfig.premium_purchase_blocked`) удалена: убрана из `canShowBirthdayContacts`, из `refreshForNewDay` и из `onAccountSwitched` (две последние проверки появились в 2.3, загрузка там теперь без условия);
  - `createEffect`, который следил за гейтом, заменён на `onMount(() => { void loadContactBirthdays(); })`;
  - удалён эффект, который заранее загружал `appGiftsManager.getStarGiftOptions()`.
- **Апстрим до правки:** при `premium_purchase_blocked` баннер скрыт. Официальный Telegram ведёт себя так же: Android и Web K прячут его по этому флагу (бриф 020). Здесь Pluma сознательно отличается от Telegram.
- **Остаётся:** показ после ответа `help.getPromoData` и если `dismissed_suggestions` не содержит ключа. Список именинников — из `contacts.getBirthdays`.
- **Коммиты:** `cb01766`.

### W-046. Строка «День рождения» в профиле: подарок только при продаже Premium
- **Где:** `src/components/peerProfile.tsx`, `birthday_row()`.
  - `replace` `PeerProfile.Birthday = () => {\n  const context = useContext(PeerProfileContext);` — добавлено `const appConfig = useAppConfig();`;
  - `replace` `    if(isToday()) {\n      return () => showSendGiftPopup({` → `if(isToday() && !appConfig.premium_purchase_blocked)`.
- **Зачем:** без этого в день рождения строка открывала окно подарка. Так ведут себя вход подарка в чате Web K (`Chat.canGiftPremium`) и профиль Android (`ProfileActivity` `containsGift`), по брифу 020. Без подарка строка копирует дату, как в любой другой день.
- **Апстрим до правки:** подарок без проверки флага. Здесь Pluma повторяет поведение Telegram Android.
- **Проверка:** `test_patch`, `DAMAGE` (`usePeerProfile()`, `canGift()`).
- **Замечание:** `useAppConfig` используется без добавления импорта, значит, апстримный файл уже импортирует его. `expect` на этот импорт нет: если апстрим уберёт импорт, сломается `pnpm run typecheck`, а не патч.
- **Коммиты:** `cb01766`.

### W-047. Что из Premium не трогается
Записано, чтобы не искать. Кроме тегов «Избранного» (W-065), остальные экраны Premium, Stars и подарков патч не меняет. Их видимость задаёт сервер (`premium_purchase_blocked` и т. п.). Тексты «Telegram Premium» превращаются в «Pluma Premium» только через W-019.

### W-065. Теги «Избранного» без Premium
- **Что и зачем:** решение владельца 2026-11-03 (задача 053). Теги Saved Messages доступны каждому пользователю, как Premium-пользователю Telegram: поставить и снять тег, фильтр по тегу, переименование. Pluma не продаёт Premium, а сервер обслуживает теги для всех (`messages.getSavedReactionTags`, `updateSavedReactionTag`, `messages.search` с `saved_reaction`, `updateSavedReactionTags`). Серверного сигнала у Web K нет: все гейты смотрят на Premium своего пользователя (`rootScope.premium`, `usePremium()`).
- **Где:** функция `saved_tags()` в `ci/patch.py`.
  - Новый файл `src/lib/appManagers/utils/reactions/savedTagsAvailable.ts`: `export default function savedTagsAvailable() { return true; }`. Если апстрим принесёт файл с таким путём, патч падает.
  - Импорт `savedTagsAvailable` добавляется после импорта `hotReloadGuard` из `@lib/solidjs/hotReloadGuardProvider` в четырёх файлах гейтов (`SAVED_TAG_GATES`; с 2.3 окно открывается через `hotReloadGuard.showPremiumPopup`, своего импорта `showPremiumPopup` в файлах нет).
  - Гейты (номера строк — закреплённый апстрим `346844bf`):
    - `src/components/chat/contextMenu.ts:465-468` — правый клик по тегу: `this.isTag && !rootScope.premium` → `this.isTag && !savedTagsAvailable()`. Без правки вместо меню «Фильтр / Переименовать» (`:764-799`) открывается окно Premium.
    - `src/components/chat/contextMenu.ts:1809-1812` — тег из полосы реакций контекстного меню: `!rootScope.premium && tags` → `!savedTagsAvailable() && tags`.
    - `src/components/chat/bubbles.ts:3650-3654` — клик по тегу на сообщении (фильтр или меню): `!rootScope.premium` → `!savedTagsAvailable()`.
    - `src/components/chat/topbarSearch.tsx:951-952` — теги над поиском: без `is-locked` и без псевдо-тега «Unlock» (`premium_lock`); `isPremium()` → `savedTagsAvailable()`.
    - `src/components/chat/topbarSearch.tsx:1008-1011` — клик по тегу в поиске: `!isPremium()` → `!savedTagsAvailable()`. Других проверок Premium в файле нет, поэтому удалены ставшие ненужными `const isPremium = usePremium();` (`:387`) и импорт `usePremium` (`:45`).
    - `src/components/chat/reactionsMenu.ts:117` — подсказка над тегами: `Reactions.Tag.Description` вместо промо `Reactions.Tag.PremiumHint`.
  - Быстрая реакция при наведении (`bubbles.ts:3189-3197`) в «Избранном» не показывается (`bubbles.ts:3093`, `this.peerId !== rootScope.myId`), поэтому обходного пути без гейта нет.
- **Лимит тегов на сообщение не правится.** Web K берёт его из appConfig сервера: `appReactionsManager.sendReaction` (`src/lib/appManagers/appReactionsManager.ts:694`) → `apiManager.getLimit('reactions')` (`src/lib/appManagers/apiManagerMethods.ts:368-395`): для не-Premium `reactions_user_max_default` (`:375`, `:390`). Лишние снимаются по `chosen_order` (`appReactionsManager.ts:767`). Теги и обычные реакции идут через этот же код. Сервер отдаёт `reactions_user_max_default=3`, значит, до трёх реакций и до трёх тегов на сообщение. Патч только сверяет (`expect`) обе строки.
- **Что не меняется:** custom emoji в тегах и реакциях, остальные Premium-функции тех же файлов (например, `onAddTaskClick` в `contextMenu.ts` по-прежнему проверяет `rootScope.premium`). Новых строк интерфейса нет: промо-тексты становятся недостижимыми.
- **Проверка:**
  - `verify`: файл функции возвращает `true`; `savedTagsAvailable` встречается только в нём и в четырёх файлах гейтов; каждый `showPremiumPopup({feature: 'saved_tags'})` в этих файлах стоит под `if(…)`, где спрашивается `savedTagsAvailable()` и нет `premium` (в `reactionsMenu.ts` ещё один — ссылка «Learn more» в промо-подсказке); новый путь к этому окну, которого патч не знает, роняет прогон; подсказка тегов не зависит от Premium; в `topbarSearch.tsx` нет `isPremium`/`usePremium`.
  - `test_patch`: результат каждого гейта, импорт ровно один раз, `onAddTaskClick` остался за Premium, `getLimit('reactions')` на месте, список файлов, где вызывается функция.
  - `test_saved_tags_file_of_upstream`: апстрим с собственным `savedTagsAvailable.ts` роняет патч.
  - `DAMAGE`: 15 случаев — каждый гейт, импорты, `getLimit`/ключи лимита, неизвестный гейт (в `bubbles.ts` и `contextMenu.ts`), вызов функции вне гейтов (`chat.ts`).
  - 2026-11-04: `python3 -I ci/patch_test.py` — 33 теста, OK (UpstreamTest не пропущен, 134 случая `DAMAGE`). Без вызова `saved_tags()` `test_patch` падает. Сборка (`pnpm run typecheck`, `vite build`) и проверка в браузере не выполнялись.
- **При обновлении:** если апстрим сам откроет теги или поменяет гейты, упадёт `replace` или проверка в `verify`; новый вызов окна Premium с `saved_tags` нужно прочитать и решить, гейт ли это тегов. Если `getLimit('reactions')` перестанет определять число тегов, упадёт `expect`: тогда проверить, не стал ли лимит жёстким для не-Premium.
- **Коммиты:** не закоммичено, ветка `task/053-saved-tags`, задача 053.

### W-067. Премиум-стикеры без Premium (задача 058)
- **Статус.** Ветка `task/058-premium-stickers` (`0de2ee1`), не влита, не выпущена (2026-11-08).
- **Что и зачем.** Премиум-стикеры (`getStickerEffectThumb`, `video_thumbs`) отправляются, показываются и предлагаются всем, как с Premium в Telegram; Pluma Premium не продаёт (отчёт владельца 2026-11-07, подтвердить решение). `premium_purchase_blocked` ещё и прячет такие стикеры из наборов (`tabs/stickers.ts`).
- **Где** (`premium_stickers()` в `ci/patch.py`, `PREMIUM_STICKER_GATES`; новый файл `src/lib/appManagers/utils/stickers/premiumStickersAvailable.ts`, импорт — за импортом `getStickerEffectThumb` в каждом файле):
  - `src/components/chat/input.ts:4583` — окно Premium вместо отправки;
  - `src/lib/appManagers/appMessagesManager.ts:5100` — локальная копия пересылки с `nopremium`;
  - `src/components/emoticonsDropdown/tabs/stickers.ts:570` — стикеры, скрытые при `premium_purchase_blocked`;
  - `src/lib/appManagers/appStickersManager.ts` (`filterShowableStickers`, 2.3) — подсказки по эмодзи;
  - `src/components/wrappers/sticker.ts:194` — значок замка.
- **Не правится.** Эффекты отправки (`sendContextMenu.ts:118`) и кастомные эмодзи (вопрос владельцу в брифе). Эффект у получателя играет `wrappers/sticker.ts:742` без проверки Premium.
- **Как проверяется.** `verify_premium_stickers` (функция `true`, вызовы только в пяти гейтах, каждый гейт), `test_premium_stickers`, `test_premium_stickers_verify`, порча каждого гейта в `DAMAGE`. `ci/patch_test.py` со `./upstream`: 35 тестов, OK. Сборка и проверка в браузере не делались.

### W-073. Нет ИИ-редактора сообщений (задача 079)
- **Статус.** Ветка `task/079-remove-ai-editor` pluma-web, не закоммичено, не влито (2026-11-11).
- **Что и зачем.** Решение владельца 2026-11-11 (задача 079): у Pluma нет языковой модели, поэтому ИИ-редактор сообщений (переписать, исправить, сменить стиль, перевести набранный текст; `messages.composeMessageWithAI`, `messages.composeRichMessageWithAI`, `aicompose.*`) не предлагается ни в одном клиенте. Сервер их не обслуживает: `messages.composeRichMessageWithAI` отвечает `ErrEnterpriseIsBlocked` (`app/bff/messages/internal/core/messages.composeRichMessageWithAI_handler.go`), а служба `RPCAiComposeTone` (`messages.composeMessageWithAI`, `aicompose.*`) не маршрутизирована в `session.yaml`, и прокси отвечает `ERR_ENTERPRISE_IS_BLOCKED`. В tweb это `aiEditorPopup` и `createWithAiPopup`.
- **Где** (`NO_AI_EDITOR_EDITS`, `no_ai_editor()` в `ci/patch.py`; апстрим `aa74832e`):
  - `components/chat/input.ts`: поле сообщения (`RichMessageInput`) не получает `ai: this.getAiEditorContext()`, поэтому не создаёт кнопку ИИ, а кнопка ИИ его панели инструментов скрыта (`aiButton.hidden = !this.options.onAi`);
  - `components/popups/newMedia.tsx`: подпись в окне отправки медиа (`InputFieldMessage`) не получает `ai`, кнопки нет;
  - `components/chat/markupTooltip.ts`: `aiAvailable = false`, кнопка ИИ всплывающей панели форматирования и разделитель скрыты (она нажимала ту же кнопку поля).
- **Апстрим без правки.** Кнопка ИИ появляется в поле от 72 px высоты и в подписи медиа, во всплывающей панели форматирования — всегда.
- **Как проверяется.** `verify_no_ai_editor(sources)`: правки на месте; по всем исходникам без комментариев вызовов `.getAiEditorContext(` и `createAiEditorContext(` — только определение в `input.ts` и сама функция, `useAiEditorButton(` / `<AiEditorButton` — только в `richMessageInput/ai.tsx`, `index.tsx`, `aiButton.tsx`, `inputFieldMessage.tsx`. `NoAiEditorTest`; порча каждого якоря в `DAMAGE` на настоящем апстриме (`UpstreamTest`).
- **Осталось (сознательно).** Меню отправки «отправить как есть / выключить стиль» появляется только при сохранённом стиле, а сохранить его можно только в редакторе. Ссылки на стиль (`internalLinkProcessor.ts`, `viewTonePopup`) — ошибка сервера. Окно перевода — задача 027.
- **Не проверено.** `pnpm run typecheck`, сборка vite и браузер (CI после пуша).


## Медиа

### W-048. «Добавить» в наборе стикеров, открытом по ссылке (снято в задаче 074)
- **Статус:** правка убрана при переходе на 2.3 (686). Апстрим исправил это сам: `toggleStickerSet(set)` принимает набор и читает кэш по нему (`set = this.getCachedStickerSets(set)[0]?.stickerSet.set ?? set`, `appStickersManager.ts`); набор, открытый по ссылке, передаётся ей напрямую. По чтению кода; запуском на 2.3 не проверено.

## Сборка, CI и подпись

Подписи у веб-клиента нет: сайт — статика. Секретов сборка не использует. `PLUMA_SERVER_URL` и `PLUMA_RSA_PUBLIC_KEY` — переменные (`vars`), не секреты. `GITHUB_TOKEN` нужен только для публикации образа в ghcr.

### W-049. Закрепление апстрима — `upstream.env`
- Поля перечислены в «Общих сведениях». Обновляются вручную и все вместе. Dependabot их не трогает.
- Коммиты: `facdd16`.

### W-050. `ci/fetch-upstream.sh`
- `git init` + `fetch --depth 1 origin $UPSTREAM_REF`, три попытки с таймаутом 10 мин, проверка `rev-parse HEAD`.
- Сабмодули (`src/solid`, `src/opus-recorder`) остаются пустыми: Solid берётся из `src/vendor`, воркеры Opus — из `public/`.
- Коммиты: `facdd16`.

### W-051. `ci/configure.sh`
- Задан хотя бы один из `SERVER_URL`/`RSA_PUBLIC_KEY` — цель `server`, и тогда обязательны оба; иначе цель `dev` из `config/dev`.
- Патчит только чистый checkout (`git status --porcelain` пуст).
- Пишет `target=` в `$GITHUB_OUTPUT`.
- Коммиты: `facdd16`.

### W-052. `config/app.env`
- `PLUMA_APP_NAME=Pluma`, `PLUMA_API_ID=10004` (10001 Android, 10002 iOS, 10003 Desktop, 10004 Web), `PLUMA_API_HASH` — 32 hex. Это не секрет: значение несёт любой клиент. В реестр значение не переносится.
- `Config` проверяет формат (имя — буквы, цифры, пробел, точка, дефис; hash — строчные hex).
- Коммиты: `facdd16`.

### W-053. `config/dev/`
- `server.env`: `PLUMA_SERVER_URL=ws://localhost:11443/apiws`.
- `server_public.pem`: публичный dev-ключ сервера, отпечаток `14037083214707814241` (`KeyTest.test_dev_key_fingerprint`).
- Коммиты: `facdd16`.

### W-054. `ci/setup.sh`
- Проверяет, что `node --version` = `v$NODE_VERSION`, ставит `pnpm@$PNPM_VERSION` через npm (три попытки) и сверяет версию.
- Коммиты: `facdd16`.

### W-055. `ci/build.sh`
- Требует `.pluma-patch.json` с той же целью.
- Снимает все `VITE_*` из окружения.
- `pnpm install --frozen-lockfile`, `pnpm run typecheck` (как в сборке апстрима), `pnpm exec vite build`.
- Сайт собирается так же, как `build.js` апстрима: каталоги и файлы `public/`, которые оставляет `keepAsset.js` апстрима, затем поверх `dist/`.
- Затем `check_bundle.py`, архив `pluma-web-2.2-676-<target>.tar.gz` и `.sha256`.
- **При обновлении:** если апстрим поменяет устройство `build.js` или `keepAsset.js`, сборку сайта надо сверить заново.
- Коммиты: `facdd16`.

### W-056. `ci/check_bundle.py` — проверка готового сайта
- Читает каждый файл сайта, включая source maps и бинарные (как latin-1).
- Ищет: адреса из сетей Telegram (`TELEGRAM_NETWORKS`), IPv6-префиксы, ключи и отпечатки, `.web.telegram.org`, `_websync_`, учётные данные (api_hash Telegram, VAPID-ключ, GCM sender id `122867383838`).
- Требует:
  - WebSocket только на URL сервера, нет чтения флагов HTTP (на 2.3: `Modes.http = isFlagOn('http')` и `!isFlagOn('noMultipleTransports')` в минифицированном виде; сами имена флагов остаются в любой сборке, поэтому ищется форма чтения). `tree.expect` в `ci/patch.py` останавливает патч, если апстрим изменит эти строки в `src/config/modes.ts`. На настоящей сборке новые выражения ещё не проверены;
  - есть modulus, api_hash и `layer:228`;
  - `<title>Pluma</title>` и имя в манифестах;
  - иконки из `brand/web`, нет скриншота;
  - «Sign in to Pluma», `plm://login?token=` и нет `tg://login`.
- Тесты: `BundleTest`.
- Коммиты: `facdd16`, `8cd3f81`, `bb3c634`, `abb939c`.

### W-057. `verify()` в `ci/patch.py` — проверка исходников после правок
- Во всех исходниках (`src/**/*.{ts,tsx,js,mjs,scss}` без тестов, моков, фикстур и `src/scripts`), а также в `index.html`, `vite.config.ts` и манифестах нет следов Telegram (`telegram_traces`) и чтения `VITE_API_ID`/`VITE_API_HASH`/`VITE_PUSH_SERVER_KEY`.
- Дальше проверки из W-001, W-002, W-003, W-004, W-011, W-013, W-014, W-015, W-019, W-024. На ветке 020 ещё W-039 и W-043.
- Тесты: `TraceTest`, `DAMAGE` (адрес, URL и модуль в комментарии `networker.ts`).
- Коммиты: `facdd16` и далее по каждой области.

### W-058. Workflow `.github/workflows/web.yml`
- Триггеры: push в `main`, pull request, еженедельно (`41 4 * * 1`, «канарейка»), вручную.
- Задачи:
  - `check`: `fetch-upstream`, `python3 -I ci/patch_test.py -v`, `shellcheck -x ci/*.sh e2e/*.sh brand/*.sh`;
  - `build`: Node из `upstream.env` через `actions/setup-node`, `ci/setup.sh`, fetch, `configure` с `vars.PLUMA_SERVER_URL`/`vars.PLUMA_RSA_PUBLIC_KEY`, кэш pnpm store по `upstream/pnpm-lock.yaml`, `ci/build.sh`, артефакт на 30 дней (push и ручной запуск);
  - `image`: только цель `server` на `main` — `docker/build-push-action` с `ci/site.Dockerfile`, `ghcr.io/<owner>/pluma-web:<sha>`, `linux/amd64`, `provenance: false`.
- Все actions закреплены по SHA.
- Коммиты: `06895df` (2026-10-24), `214779d` (образ, 2026-10-29), `19c2741` (shellcheck для `brand/`).

### W-059. `.github/dependabot.yml`
- Еженедельные PR только для github-actions.
- Апстрим, Node, pnpm и Playwright обновляются вручную.
- Коммиты: `06895df`.

### W-060. `ci/site.Dockerfile`
- `FROM caddy:2.11.6-alpine@sha256:d44355d3…`, `COPY . /srv/`.
- Caddyfile (TLS, `/apiws` на шлюз) приносит репозиторий сервера (`docker-compose.web.yaml`, `docs/ops/deployment.md`).
- Коммиты: `214779d`.

### W-061. Бренд-кит и рендер — `brand/`
- `brand/src`:
  - `pluma-icon.svg`, `pluma-icon-square.svg`, `pluma-mark.svg`, `pluma-mark-mono.svg` — из кита владельца как есть;
  - `pluma-favicon.svg` (знак на плотной подложке для 16 px) и `pluma-qr-logo.svg` (знак на белом диске для QR) — производные.
- `brand/render.sh` (rsvg-convert, ImageMagick 7) рендерит `brand/web/assets/img/*` по путям `public/` апстрима, без меток времени, так что рендер воспроизводим.
  - Размеры и раскладка плиток Windows повторяют апстрим.
  - `favicon_unread.ico` — PNG 32 px под этим именем, как у апстрима. По комментарию скрипта, на файл никто не ссылается.
  - Скрипт печатает SHA-256 каждого файла.
- После изменения SVG — перерендерить и закоммитить.
- Коммиты: `19c2741` (2026-10-29), задача 017.

## Прочее

### W-062. Проверка на стенде — `e2e/`
- `e2e/run.sh` поднимает e2e-стенд из `PLUMA_SERVER_DIR` (его `test/e2e/run.sh`, `E2E_PROJECT` по умолчанию `pluma-web-e2e`), собирает клиент под одноразовый ключ стенда (`go run ./tools/genserverkey -show`) и раздаёт его на `127.0.0.1`.
- Номер — фиктивный `+120155501xx`, добавляется в allowlist.
- Playwright 1.64.1, Chromium, `--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE localhost`. Любой запрос не на localhost роняет прогон.
- Сценарий: вход с кодом из `pluma-admin`, регистрация, сообщение в «Избранное», создание папки чатов (Web K берёт лимиты из `help.getAppConfig`); второй браузер должен увидеть сообщение и папку.
- Стенд и файлы браузера удаляются в конце.
- Коммиты: `5f1ffda` (2026-10-24), `93ebb14` (папка, `actionTimeout` 30 с), `4b42d8e` (пузырь отбирается по `data-mid`: в 2.3 при отправке есть летящая копия), `5409719` (меню открывается повторно).
- 2026-11-09, 2.3 (686), сервер на слое 233 (ветка `task/batch-20261109d`): сценарий пройден, 0 запросов вне машины.

### W-063. Остальные файлы репозитория
- `LICENSE` — GPL-3.0, как у апстрима.
- `README.md` — описание всего перечисленного. Коммиты: `c77afd2`, `d6475fe`, дополнения в `214779d`, `cb01766`, `e9a6865`, `fe68fd5`.
- `.gitignore` — `/upstream/`, `/out/`, `/.e2e/`, `/.pnpm-store/`, `/e2e/node_modules/`, `__pycache__/`.
- `.shellcheckrc` — `source-path=SCRIPTDIR/..`.
- Коммиты: `b7fef84`, `facdd16`.

### W-064. `.pluma-patch.json` в checkout (добавленный файл)
- Пишется после успешных `patch` и `verify`.
- Содержит цель, имя, `api_id`, `api_hash`, layer, URL сервера, modulus, exponent и отпечаток.
- Читают `ci/build.sh` (сверка цели) и `check_bundle.py`. В сайт файл не попадает: он лежит в корне checkout, а не в `public/` или `dist/`.
- Коммиты: `facdd16`.

---

## Пробелы и замечания

- Номера записей стабильны: новая правка получает следующий свободный номер, удалённая оставляет свой номер пустым с пометкой.
- W-038 и W-047 — не правки, а пометки «здесь патч ничего не меняет».
- Значения флагов `VITE_MTPROTO_*` в апстримном `.env` здесь не приведены: файлы `.env` по правилам проекта не читаются. Что апстрим по умолчанию стартует на HTTP, известно из README и брифа 012.
- `useAppConfig` в W-046 не охранён `expect` (см. замечание в записи).
- Решение на подтверждение (бриф 020): тёмный цвет имён бирюзового `#53CCC6` на всех платформах.

### W-068. Premium для всех: звезда, кнопка статуса, расшифровка (задача 071)
- **Статус.** Ветка `task/071-premium-for-everyone` (`b34d7f8`), не влита, не выпущена (2026-11-09).
- **Что и зачем.** Сервер ставит флаг `premium` всем (ADR 0041). Строка Premium в настройках уже закрыта `isPremiumPurchaseBlocked`.
- **Где** (`PREMIUM_FOR_EVERYONE_EDITS` в `ci/patch.py`):
  - `components/generateTitleIcons.ts`: звезды нет;
  - `components/sidebarLeft/index.ts`: кнопки эмодзи-статуса в шапке нет;
  - `components/chat/bubbles.ts`: `canTranscribeVoice: false` у голосовых;
  - `components/chat/bubbles.ts`: вызова `wrapRoundVideoBubble` нет (задача 074): эта обёртка только добавляет кнопку текста к круглому видео и разворачивает его в голосовое, а `messages.transcribeAudio` сервер не обслуживает;
  - `helpers/dom/createStickersContextMenu.ts`: пункта «Set as Status» в меню эмодзи нет (задача 074), `account.updateEmojiStatus` сервер не обслуживает;
  - `components/webApp.tsx`: `web_app_set_emoji_status` сразу отвечает мини-приложению `emoji_status_failed` с `SERVER_ERROR`, без окна согласия; `web_app_request_emoji_status_access` отвечает `cancelled` (`bots.toggleUserEmojiStatusPermission` сервер тоже не обслуживает);
  - `lib/internalLinkProcessor.ts`: ссылка `emoji-status` (из `chats` и `settings`) ничего не открывает; без правки она вела к промо Premium, потому что кнопки в шапке нет.
- **Как проверяется.** `verify_premium_for_everyone`, `PremiumForEveryoneTest`, `test_patch` (круглое видео), `test_no_way_to_an_emoji_status`.

### W-069. Бустов нет: пункты меню, ссылка, «Не ограничивать бустеров» (задача 071)
- **Статус.** Ветка `task/071-premium-for-everyone` (`18482a0`), не влита, не выпущена (2026-11-09).
- **Что и зачем.** `premium.getBoostsStatus` отвечает уровнем 0. Ограничения своих реакций по уровню в Web K нет (`sidebarRight/tabs/chatReactions.tsx`), правки не требует.
- **Где** (`PREMIUM_FOR_EVERYONE_EDITS` в `ci/patch.py`):
  - `components/chat/topbar.ts`: пунктов `BoostChannel`, `BoostGroup` нет, через них открывалась и вкладка `boosts.tsx`;
  - `lib/internalLinkProcessor.ts`: ссылка `boost` открывает канал;
  - `groupPermissions.tsx`: секция бустеров не показывается.
- **Как проверяется.** `verify_premium_for_everyone`, `PremiumForEveryoneTest`; якоря — на `346844bf`. Сборка и экран не проверены.

### W-072. Подарков нет: меню чата, профиль, QR-код (задача 074)
- **Что и зачем.** Подарки продаются за Stars, которых в Pluma нет (ADR 0041). `stargifts_blocked` из конфигурации tweb не читает (ключ есть только в `appConfig.d.ts`), поэтому входы показывались бы всем.
- **Где** (`PREMIUM_FOR_EVERYONE_EDITS` в `ci/patch.py`):
  - `components/chat/topbar.ts`: «Send a Gift» в меню чата, был и на 2.2;
  - `components/peerProfile.tsx`: тот же пункт в меню профиля, новое в 2.3;
  - `components/popups/myQrCode.tsx`: панели «Подарки» нет, новое в 2.3 (её «Browse Gifts» открывал магазин). Код панели остаётся недостижимым;
  - `components/chat/input.ts`: `hasGift = false`, на нижней плашке канала, куда пользователь не может писать, нет кнопки подарка (новое в 2.3);
  - `lib/internalLinkProcessor.ts`: ссылки `send-gift` и `send-gift/self` ничего не открывают.
- **Без правки.** `Chat.canGiftPremium` и строка в настройках закрыты `premium_purchase_blocked`; строка дня рождения в профиле — W-046, баннер — W-043; `btnSendGift` требует `display_gifts_button`, сервер его не ставит.
- **Как проверяется.** `verify_premium_for_everyone`, `PremiumForEveryoneTest`, `test_patch` (нет `id: 'gifts'`), `test_no_gift_entries`.
