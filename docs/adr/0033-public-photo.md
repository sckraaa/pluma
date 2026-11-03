# 0033. Фото «для всех» и личное фото контакта: отдельные ссылки, а не альбом; выбор фото — один на всех путях

- Статус: proposed (2026-11-03).
- Задача: 051. Исследование: `docs/tasks/034-research/public-photo.md`, сверка — `check-archive-photo.md`. Продолжает ADR 0031 (фото скрывается для каждого зрителя).

## Контекст
- `photos.uploadProfilePhoto` и `photos.updateProfilePhoto` не смотрели на `fallback`. Фото «для всех» из настроек приватности становилось основным и пряталось тем же правилом, ради обхода которого его ставили. `updateProfilePhoto(fallback, inputPhotoEmpty)` (tdesktop, iOS) удалял основное фото.
- `photos.uploadContactProfilePhoto` отвечал «не реализовано».
- Клиенты сами добавляют фото «для всех» и личное фото в галерею (`ProfileGalleryView.java:926-934`, tdesktop `data_user.cpp:851-869`), а фото «для всех» другого пользователя узнают по совпадению `userFull.fallback_photo.id` с `user.photo.photo_id` (`ProfileActivity.java:6829-6836`). Флага у такого `user.photo` нет.
- Личное фото клиенты узнают по `userProfilePhoto.personal` (tdesktop `data_user.cpp:175-186`).

## Решение
1. **Хранение** (`migrate-20261103-profile-photos.sql`, повторный запуск безопасен):
   - `users.fallback_photo_id` — фото «для всех». В альбом (`user_profile_photos`) не входит.
   - `user_contacts.personal_photo_id`, `personal_photo_has_video` — личное фото на строке контакта владельца: метод отвечает `CONTACT_MISSING` для неконтакта, а строка уже доходит до всех загрузчиков пользователя (`ContactData`).
   - `user_suggested_photos(user_id, photo_id, from_user_id, date2)` — предложенные фото: получатель может сделать такое фото основным, хотя загрузил его другой.
   - В proto (ADR 0009, `// pluma:`): `UserData.fallback_photo`, `ContactData.personal_photo_id` и `personal_photo_has_video`. Кэш пользователя грузит фото «для всех» рядом с основным; ошибка чтения любого из них — ошибка загрузки, а не пользователь без фото.
2. **Какое фото видит зритель V у пользователя U** — одна функция `ImmutableUser.photoFor` (`PhotoFor` снаружи) для `ToUnsafeUser`, `ToUser`/`ToUserFor`; пути не расходятся:
   1. сам аккаунт и боты — основное фото;
   2. у V есть личное фото U — оно, с `personal`, что бы ни говорило правило U;
   3. правило U «Фото профиля» разрешает V — основное фото U (нет основного — нет фото);
   4. иначе — фото «для всех» без флага, если есть;
   5. иначе — ничего.
3. **`userFull`**: `profile_photo` — основное, если правило разрешает; `fallback_photo` — самому аккаунту и тем, кому основное скрыто (не ботам); `personal_photo` — личное фото зрителя.
4. **Фото «для всех»** — `user.updateProfilePhoto` с `fallback`:
   - загрузка с `fallback` ставит его, основное фото и альбом не меняются;
   - `updateProfilePhoto(fallback, inputPhoto)` — только своё фото из альбома или основное, иначе `PHOTO_ID_INVALID`; фото остаётся в альбоме;
   - `updateProfilePhoto(fallback, inputPhotoEmpty)` убирает его, ответ `photoEmpty`; `photos.deletePhotos` с его id тоже убирает (так удаляет Android, `PrivacyControlActivity.java:604-618`);
   - обновления — как у основного фото: `updateUser` с seq на все устройства аккаунта и всем, кто видит аккаунт (`tellWatchers`); каждому пользователь собирается «как видит он», поэтому смена видна ровно тем, кому основное скрыто;
   - флаг `bot`: с `fallback` — `BOT_FALLBACK_UNSUPPORTED` (core.telegram.org), без — `BOT_INVALID`: в Pluma у аккаунтов нет своих ботов. Раньше запрос менял фото самого вызывающего.
5. **Личное фото** — новый метод biz `user.setContactPersonalPhoto`: транзакция ящика владельца, строка контакта под блокировкой, `CONTACT_MISSING` для неконтакта, сброс кэша строки, `updateUser` с seq и контактом «как видит владелец» на все его устройства. Контакту и другим никто ничего не шлёт: фото видит только владелец. Удаление контакта стирает личное фото вместе со строкой.
6. **`photos.uploadContactProfilePhoto`**:
   - ни `save`, ни `suggest` — `NEED_ACTION_MISSING`; сам себе или не пользователь — `USER_ID_INVALID`; неконтакт — `CONTACT_MISSING` (и для `suggest`);
   - `save` с файлом ставит личное фото, `save` без файла убирает его (так шлют tdesktop, Android и iOS);
   - `suggest` — запись в `user_suggested_photos`, затем служебное сообщение `messageActionSuggestProfilePhoto` в личный чат через `msg.sendMessageV2` без исключения вызвавшего устройства: метод отвечает `photos.Photo`, поэтому сообщение приходит пушем на все устройства отправителя; `suggest` без файла — `PHOTO_FILE_MISSING`;
   - оба флага (iOS `.customAndSuggest`) — оба действия с одним загруженным фото;
   - фото ни в чей альбом не попадает; ответ — фото и контакт, как его видит вызывающий.
7. **Принять предложенное фото** — `photos.updateProfilePhoto` с этим фото (tdesktop `history_view_userpic_suggestion.cpp:51-56`, Android `ChatActivity.java:36994-36999`): biz разрешает фото из `user_suggested_photos` получателя, оно становится основным и входит в альбом.
8. Ответы `photos.uploadProfilePhoto` и `photos.updateProfilePhoto` несут в `users` сам аккаунт (tdesktop читает `processUsers`).
9. Ошибки media при сохранении размеров видео и самого фото больше не теряются.

## Последствия
- Правила показа фото — в одном месте рядом с вычислителем приватности; тест проверяет все пути сборки пользователя.
- В кэше строк контактов и пользователя новые поля; старые записи читаются как «нет фото», что верно: до миграции таких фото не было. Версию ключей поднимать не нужно.
- Личное фото на строке контакта стоит 9 байт на строку и не требует отдельного запроса при загрузке пользователя.

## Открытые вопросы (UNVERIFIED, сверка с Telegram — `reference-telegram-probe`)
- Шлёт ли Telegram `fallback_photo` и тем, кто видит основное фото. Сейчас нет; иначе tdesktop добавил бы его в конец их галереи.
- Что `photos.getUserPhotos` отдаёт тому, кому основное фото скрыто. Сейчас пустой альбом, фото «для всех» в альбом не входит.
- Сохраняется ли личное фото после удаления и повторного добавления контакта. Сейчас стирается.
- `CONTACT_MISSING` для `suggest`.
- Какие обновления Telegram шлёт при смене фото «для всех» и личного фото.
- Запись о предложенном фото не удаляется при удалении сообщения: получатель может принять фото и позже, если знает его id и access_hash.
- Скачивание фото по `inputPeerPhotoFileLocation` не проверяет приватность (`dfs.downloadFile_handler.go`): это задача приватности (034), не этой.
- Эмодзи-аватар: клиенты шлют файл вместе с `video_emoji_markup` (tdesktop `api_peer_photo.cpp:474-484`, Android `PrivacyControlActivity.java:242-255`), поэтому фото сохраняется; сама разметка (`videoSizeEmojiMarkup`) не хранится — отдельная задача.
