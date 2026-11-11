# 082. Антиспам групп, автоперевод каналов, геогруппы

- Статус: сделано (ветка `task/082-antispam-translate-geo`, ждёт сборки партии).
- Решение: ADR 0050.

## Что нужно
Закрыть оставшиеся пункты «unclear» реестра (`docs/project/feature-registry.json`): `channels.toggleAntiSpam`, `channels.reportAntiSpamFalsePositive`, `channels.toggleAutotranslation`, `channels.editLocation` и всё, что связано с геогруппами (`channels.createChannel` с `geo_point`, `channels.getAdminedPublicChannels` с `by_location`/`check_limit`, `contacts.getLocated`). Владелец: сделать всё, самый низкий приоритет.

## Источники
Разобраны в ADR 0050 (раздел «Контекст»): core.telegram.org/api/antispam, /api/translation, страницы методов; код четырёх клиентов из `reference-src-new` с номерами строк.

## Результат
Антиспам работает: админ с правом удалять сообщения включает его в супергруппе от 10 участников, сервер проверяет каждое сообщение в транзакции отправки по правилам `pkg/antispam` (новичок: ссылка, пересылка из чужого чата, 5+ упоминаний; любой проверяемый: одно и то же в 3+ группах за 10 минут) и удаляет нарушителя от служебного аккаунта 1000001 через событие inbox, с записью в «Недавних действиях», как ждут клиенты. Жалоба админа на ложное срабатывание исключает автора из проверки в группе и ставит жалобу в очередь модерации, в одной транзакции; сообщение не восстанавливается. Автоперевод канала обслуживается как флаг канала (сам перевод — задача 027). Геогрупп нет: точные ошибки Telegram, геоточки нигде не хранятся.

Изменения:
- Схема: `migrate-20261114-channel-antispam.sql` (колонки `channels.antispam`, `channels.autotranslation`; таблицы `channel_antispam_deletions`, `channel_antispam_exempt`, `antispam_recent_posts` — только хэш содержимого), `z_pluma-antispam-user.sql` (аккаунт 1000001, `user_type = 4`, «Pluma Anti-Spam», номер `42001`).
- `pkg/antispam`: правила, хэш содержимого, константы (`UserId`, `MembersMin = 10`, пороги, `RulesVersion`).
- biz channel: поля `antispam = 49`, `autotranslation = 50` в `ImmutableChannel` (TL-кодек — флаги 18 и 19; `autotranslation` идёт и в min-канал); настройки `SettingAntispam = 7` (право `DeleteOthers`, `PARTICIPANTS_TOO_FEW` ниже порога, только супергруппа) и `SettingAutotranslation = 8` (`ChangeInfo`, только вещательный канал) в `channel.toggleSetting` с событиями журнала `ToggleAntiSpam`/`ToggleAutotranslation`; новые RPC `channel.antispamDelete` (идемпотентно, в боксе автора, журнал от 1000001, `channel_antispam_deletions`) и `channel.antispamFalsePositive`.
- msg: `SendChannel` записывает хэш каждого сообщения супергруппы в `antispam_recent_posts`, в группе с антиспамом оценивает сообщения и пишет событие `inbox.channelAntispamToInbox` (получатель — аккаунт антиспама, `from_id` — автор). Обработчик inbox вызывает `channel.antispamDelete`, ошибка оставляет событие на повтор.
- bff channels: `channels.toggleAntiSpam`, `channels.reportAntiSpamFalsePositive`, `channels.toggleAutotranslation`, `channels.editLocation` (`CHANNEL_INVALID` → `MEGAGROUP_REQUIRED` → право → `MEGAGROUP_GEO_REQUIRED`), `channelFull.antispam`; `createChannel` с `geo_point` и `getAdminedPublicChannels` с `by_location` + `check_limit` — `CHANNELS_ADMIN_LOCATED_TOO_MUCH`; `messages.translateText` и `messages.togglePeerTranslations` — `ERR_ENTERPRISE_IS_BLOCKED` из `not_served.go`. `RPCAntiSpam` и `RPCTranslation` зарегистрированы в bff и маршрутизированы в трёх `session.yaml`.
- bff contacts: `contacts.getLocated` — `ERR_ENTERPRISE_IS_BLOCKED` без лога (`not_served.go`), заглушка апстрима удалена.
- Модерация: вид жалобы `KindAntispamFalsePositive = 6`, `moderation.AddReportTx`, общий `moderation.MediaOf` (перенесён из bff reports); подпись вида и причины `not_spam` в панели.
- Панель: счётчики пользователей и регистраций и рассылка «всем» не считают служебные аккаунты (`user_type <> 4` вместо `id <> 777000`).
- Конфиг: `telegram_antispam_user_id: "1000001"`, `telegram_antispam_group_size_min: 10` (+ таблица в `docs/ops/app-config.md`).
- Документы: ADR 0050 (уточнения при реализации), `docs/ops/antispam.md`, реестр функций, `TESTING-BACKLOG`.
- e2e `TestChannelsNotServed` берёт примерами `messages.getPersonalChannelHistory` (bff) и `messages.getFactCheck` (прокси): прежние примеры теперь обслуживаются.

Проверка (CachyOS): `go build ./...`; `go vet -tags integration,e2e -unreachable=false ./app/... ./pkg/... ./test/e2e/ ./tools/...`; модульные тесты затронутых пакетов и `./tools/...`; `-race` по `pkg/antispam`, `biz/channel/channel`, bff channels, bff contacts, msg inbox; интеграционные тесты `biz/channel/...`, `msg/...`, `app/ops/admin`, `pkg/moderation`, bff channels и reports на MySQL и Kafka — зелёные. golangci-lint по затронутым пакетам: в новых файлах замечаний нет (остальные — старые, вне задачи). e2e на стенде `pluma-lowprio`: `TestChannelAntispam`, `TestChannelAutotranslationAndGeo`, `TestChannelsNotServed` — PASS, стенд снят, образов и томов не осталось. Новые тесты: `pkg/antispam` (правила, исключения, хэш, сверка с `app_config.json` и SQL), `TestAutotranslationView` и флаги в `TestCodecRoundTrip`, `TestGeoGroups`, `TestGetLocatedNotServed`, интеграционные `TestToggleAntispam`, `TestToggleAutotranslation`, `TestAntispamDelete`, `TestAntispamFalsePositive`, `TestAntispamSend`. Без изменения падают проверены `TestGeoGroups` (старый ответ `ERR_ENTERPRISE_IS_BLOCKED`) и `TestAppConfig` (порог 100). `api-coverage.md` перегенерирован для проверки и возвращён (обновляет сборка партии): `toggleAntiSpam`, `reportAntiSpamFalsePositive`, `toggleAutotranslation`, `editLocation` становятся `ok`, `translateText` и `togglePeerTranslations` — `blocked`.

Решения, требующие подтверждения владельцем:
- Порог включения антиспама — 10 участников (у Telegram 100).
- Правила R1–R4 и пороги: 72 часа и 5 сообщений для «нового», 5 упоминаний, 3 группы за 10 минут, текст от 30 символов для R4.
- Служебный аккаунт «Pluma Anti-Spam», id 1000001, номер `42001`; аватар — по желанию.
- Автоперевод: флаг обслуживается до выбора поставщика перевода (задача 027).
- `PARTICIPANTS_TOO_FEW` при включении антиспама в маленькой группе — документация ошибку не называет (не проверено), клиенты такой запрос не шлют.

Отклонения и открытые вопросы:
- Событие удаления адресовано аккаунту антиспама, а не автору: inbox-событие нельзя адресовать инициатору транзакции (`userbox.CheckEvent`). Все удаления антиспама идут одной очередью по порядку.
- «Восстановление» при жалобе не делается: клиенты его не ждут (ADR 0050).
- Подсказки клиентам при удалении (например, служебного сообщения автору) Telegram не документирует; не делалось.
- Номера полей `ImmutableChannel` 49/50 и номер ADR 0050 нужно сверить с другими ветками партии при слиянии.
