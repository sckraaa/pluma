# 084. Внешний вид аккаунта и малые настройки

- Статус: сделано (ветка `task/084-user-look`, ждёт сборки партии).
- Решение: ADR 0052.

## Что нужно
Обслужить методы, которые клиенты вызывают, а сервер отклонял: `account.updateColor`, `account.updateEmojiStatus`, `account.clearRecentEmojiStatuses`, `account.getCollectibleEmojiStatuses`, списки для выбора статуса, `updateUserEmojiStatus` / `updateUser` устройствам аккаунта и тем, кто его видит; свои мелодии (`account.getSavedRingtones`, `account.saveRingtone`, `account.uploadRingtone`); `contacts.updateContactNote`; `contacts.exportContactToken` / `importContactToken`; `users.suggestBirthday`; `account.saveAutoDownloadSettings`; `help.getInviteText`; довести `account.getNotifyExceptions`.

## Источники
Разобраны в ADR 0052 (раздел «Контекст»): страницы методов и разделов core.telegram.org и код четырёх клиентов из `reference-src-new` с номерами строк.

## Результат
Аккаунт выбирает цвет имени и профиля с узором и эмодзи-статус со сроком; его устройства и те, кто его видит (контакты и собеседники), получают `updateUser` / `updateUserEmojiStatus` в той же транзакции, что и запись; недавние статусы ведутся и очищаются. Свои мелодии загружаются (MP3 и Ogg, хранится всегда MP3), сохраняются, удаляются и синхронизируются между устройствами. У контакта есть личная заметка (`userFull.note`), у профиля — временная ссылка на 5 минут, контакту можно предложить день рождения служебным сообщением. Настройки автозагрузки сохраняются, приглашение друзей ведёт на сайт Pluma, исключения уведомлений отдаются по категориям.

Изменения:
- Схема: `migrate-20261114-user-look.sql` — `users.color` / `users.profile_color` «нет» = −1 (однократный перевод невыбранных нулей, признак — умолчание колонки); таблицы `user_recent_emoji_statuses`, `user_saved_ringtones`, `user_contact_notes`, `contact_tokens`, `user_autodownload_settings`.
- biz user: `user.updateEmojiStatus` и `user.setColor` стали транзакциями ящика (`changeSelf`): строка, обновление с seq устройствам, inbox-события тем, кто видит аккаунт; `selfRow` несёт цвета и статус; `user.MakePeerColor` (цвет 0 — цвет), `user.MakeEmojiStatus`; заметка удаляется вместе с контактом. Новых RPC biz нет: оба RPC уже были в схеме сервиса.
- `third_party(proto)`: `ImmutableUser.EmojiStatus()` не отдаёт статус с прошедшим сроком (`// pluma:`).
- `pkg/userbox`: `updateUserEmojiStatus`, `updateRecentEmojiStatuses`, `updateSavedRingtones` — обновления с seq; `seqpeers` прикладывает пользователя к `updateUserEmojiStatus`. В `updates.getDifference` они сворачиваются (`seq_fold`): последний статус каждого пользователя, по одной перезагрузке списков недавних статусов и мелодий.
- Новые пакеты: `pkg/emojistatus`, `pkg/ringtones` (+ `saved`), `pkg/contactnotes`, `pkg/contacttokens`, `pkg/autodownload`.
- bff catalog: `account.updateColor`, `account.updateEmojiStatus`, `account.getRecentEmojiStatuses` (был пустой список), `account.clearRecentEmojiStatuses`, `account.getCollectibleEmojiStatuses`, сервис `RPCRingtone` (три метода).
- bff contacts: `contacts.updateContactNote`, заметка в `contacts.addContact`, сервис `RPCProfileLinks` (`exportContactToken`, `importContactToken`); конфиг получил `Mysql`.
- bff users: `userFull.note`. bff userchannelprofiles: `users.suggestBirthday`. bff autodownload: сохранение, общие пресеты из `pkg/autodownload`; конфиг получил `Mysql`. bff configuration: `help.getInviteText`. bff notification: `account.getNotifyExceptions` по категориям, каналы грузятся через biz channel (раньше в общем bff каналы исключений не прикладывались вовсе: плагин не задан).
- dfs / media: `uploadRingtoneFile` переписан — формат по содержимому, проверки размера и длительности, Ogg → MP3; `ffmpegutil.ProbeAudio` и `ToMP3` с белым списком форматов и протоколов.
- Регистрация: `RPCRingtone`, `RPCProfileLinks` в `server.go` и трёх `session.yaml`.
- Конфиг: `ringtone_size_max`, `ringtone_duration_max`, `ringtone_saved_count_max`, `contact_note_length_limit` в `app_config.json` и `docs/ops/app-config.md`.
- Документы: ADR 0052, `docs/ops/user-look.md`, реестр функций, `TESTING-BACKLOG`.

Проверка (CachyOS): `go build ./...`; `go vet -tags integration,e2e -unreachable=false` затронутых пакетов и `./test/e2e/`; модульные тесты `./app/... ./pkg/...` и `./tools/...`; интеграционные тесты `pkg/emojistatus`, `pkg/ringtones/saved`, `pkg/contactnotes`, `pkg/contacttokens`, `pkg/autodownload`, `app/service/biz/user/internal/core` (в том числе миграция цветов и события статуса и цвета); тесты ffmpeg (`ProbeAudio`, `ToMP3`) на настоящем ffmpeg. Три теста проверены на падение без изменения (цвет 0, сохранение при загрузке, категории исключений). Сквозные тесты написаны и скомпилированы (`test/e2e/user_look_test.go`), на стенде не запускались: запуск — в конце партии.

Решения, требующие подтверждения владельцем:
- Срок временной ссылки на профиль — 300 секунд. Telegram срок не документирует; число взято из заглушки iOS (`ChatQrCodeScreen.swift:1899-1906`). Стоит сверить с настоящим Telegram.
- `account.getAutoDownloadSettings` не возвращает сохранённое аккаунтом (ADR 0052, п. 9): иначе клиенты перезаписали бы свои базовые пресеты, а iOS снизил бы качество отправляемого видео. Поведение настоящего Telegram не проверено.
- `help.getInviteText` отдаёт только `https://pluma.chat`, без фразы: Android вставляет ответ на место ссылки в свою локализованную строку.
- Недавних статусов хранится 50 (число Telegram не документировано).
- Переполненный список мелодий (100) отвечает `RINGTONE_INVALID`: имя ошибки Telegram для этого случая не найдено (не проверено).
- Слишком длинная заметка обрезается до 128 единиц UTF-16, а не отклоняется: ошибка Telegram для этого случая не документирована.
- Заметка удаляется вместе с контактом (документация об этом молчит).
- `contacts.updateContactNote` не шлёт обновление другим устройствам: обновления для заметки в схеме нет, другие устройства читают её при открытии профиля.
- `users.suggestBirthday` не требует, чтобы получатель был контактом: в документации такой ошибки нет (у предложенного фото в задаче 051 выбрано строже).
- Истёкший статус перестаёт отдаваться при чтении; отдельное `updateUserEmojiStatus` в момент истечения не шлётся.

Отклонения и открытые вопросы:
- **Звук чата не хранится сервером.** `account.updateNotifySettings` не сохраняет `NotificationSound` (в том числе выбранную мелодию) и настройки историй — пробел прежней реализации настроек уведомлений. Из-за него выбранная мелодия действует только на устройстве, где выбрана, не попадает в push (iOS в фоне сыграет стандартный звук), а `compare_sound` сравнивает только строку `sound` старых слоёв. Нужна отдельная задача: хранение трёх звуков по платформам, настройки историй, звук в push.
- **`account.saveRingtone` не конвертирует чужие документы**: голосовое сообщение или m4a из чата — `RINGTONE_INVALID` (Android предлагает «Сохранить для уведомлений» и для них).
- **Статус и узор на стенде без каталога не проверить**: сквозной тест покрывает цвета из встроенных и ошибки; успешная установка статуса покрыта интеграционным тестом biz user и модульным тестом bff.
- Найдено попутно, не исправлялось: `account.uploadTheme` и загрузка обоев (`app/bff/catalog/internal/core/account.uploadTheme_handler.go:37`, `wallpapers.go:311`) передают в media `OwnerId: c.MD.UserId`, а части файла хранятся под `PermAuthKeyId` (`upload.saveFilePart`) — загрузка, по-видимому, не находит файл; сквозного теста на неё нет.
- Найдено попутно: `ffmpegutil.GetDuration` разбирает длительность как `чч:мм:сс`, а ffprobe отдаёт секунды числом — функция возвращает 0 (в новом коде не используется).
- Номер ADR 0052 и строки `session.yaml` нужно сверить с другими ветками партии при слиянии.
