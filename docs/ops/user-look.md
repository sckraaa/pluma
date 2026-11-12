# Внешний вид аккаунта, мелодии, заметки и ссылки на профиль (задача 084, ADR 0052)

## Что появилось
- Цвет имени и профиля, эмодзи-статус, недавние статусы.
- Свои мелодии уведомлений: загрузка, список, удаление.
- Личная заметка о контакте, временная QR-ссылка на профиль, «предложить день рождения».
- Сохранение настроек автозагрузки, ссылка в приглашении друзей, список исключений уведомлений по категориям.

## Развёртывание
- Миграция `migrate-20261114-user-look.sql` (сервис migrate применяет сам, повторный запуск безопасен):
  - `users.color` и `users.profile_color`: невыбранные 0 становятся −1, умолчание колонок — −1. Делается один раз, признак — само умолчание.
  - таблицы `user_recent_emoji_statuses`, `user_saved_ringtones`, `user_contact_notes`, `contact_tokens`, `user_autodownload_settings`.
- В `session.yaml` (`teamgramd/etc`, `teamgramd/etc2`, `app/interface/session/etc`) добавлены маршруты `/mtproto.RPCRingtone` и `/mtproto.RPCProfileLinks` на `bff.bff`: нужен перезапуск session.
- `teamgramd/bin/app_config.json` получил `ringtone_size_max`, `ringtone_duration_max`, `ringtone_saved_count_max`, `contact_note_length_limit`: нужен перезапуск bff; клиенты заберут конфиг по новому `hash` сами.
- Сервисы biz, bff, dfs, media, msg и sync перезапускаются вместе: появились новые обновления с seq (`updateUserEmojiStatus`, `updateRecentEmojiStatuses`, `updateSavedRingtones`).
- Новых переменных и секретов нет.

## Что нужно на сервере
- **ffmpeg и ffprobe в контейнере dfs** с кодировщиком `libmp3lame` (пакет `ffmpeg` из `Dockerfile` его содержит). Проверка: `docker compose exec dfs ffmpeg -hide_banner -encoders | grep libmp3lame`. Без него загрузка Ogg-мелодии ответит ошибкой, MP3 продолжит работать.
- **Каталог** (`docs/ops/catalog.md`): цвета профиля (`help.getPeerProfileColors`), дополнительные цвета имени и custom emoji для узоров и статусов берутся из него. Без каталога работают только 7 встроенных цветов имени.

## Проверка после выката
- `SELECT COLUMN_DEFAULT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME IN ('color', 'profile_color')` — обе строки `-1`.
- В клиенте: «Настройки → Уведомления → звук → Загрузить» — мелодия до 5 секунд появляется в списке; мелодия длиннее отклоняется с понятным сообщением.

## Данные
- Токены ссылок на профиль живут 5 минут; просроченные строки `contact_tokens` удаляются при следующем создании токена кем угодно. Токен в логи не пишется.
- Заметка о контакте удаляется вместе с контактом.
- `user_autodownload_settings` хранит последнее, что клиент сохранил под каждым из трёх имён; на пресеты, которые получают клиенты, таблица не влияет (ADR 0052, п. 9).

## Известные ограничения
- Выбранная для чата мелодия сервером не хранится (`account.updateNotifySettings` не сохраняет звук): она действует на устройстве, где выбрана, и не попадает в push.
- Голосовое сообщение или другой не-MP3 файл из чата сохранить как мелодию нельзя (`RINGTONE_INVALID`).
