# 051. Фото «для всех» и личное фото контакта

- Статус: done
- Зависит от: 034 (фото скрывается по правилам приватности для каждого зрителя)
- ADR: 0033 (`docs/adr/0033-public-photo.md`)
- Исследование: `docs/tasks/034-research/public-photo.md`, сверка — `docs/tasks/034-research/check-archive-photo.md`.

## Цель
В настройке «Фото профиля» можно поставить фото «для всех». Его видят те, кому основное фото скрыто (`userFull.fallback_photo`). Личное фото, поставленное контакту, видит только тот, кто его поставил.

## Что известно
- Сейчас `photos.uploadProfilePhoto` и `photos.updateProfilePhoto` не смотрят на флаг `fallback`. Фото «для всех» становится основным, и контакты теряют настоящее фото (`034-research/server.md`, B7).
- `photos.uploadContactProfilePhoto` отвечает «не реализовано».
- Семантика по core.telegram.org/api/profile-photos, ожидания клиентов и предложение по хранению — в исследовании, разделы 1–6.

## Область
- Фото «для всех»: загрузка, замена, удаление. Хранится отдельно от основного.
- Показ: `userFull.fallback_photo` тем, кому основное фото скрыто. `user.photo` — по правилам из исследования, раздел 4.
- Личное фото контакта: `photos.uploadContactProfilePhoto` (`save` и `suggest`), `userFull.personal_photo`, `user.photo` с флагом `personal` для того, кто его поставил.
- Обновления на все устройства, как ждут клиенты.

## Открытые вопросы
- Исследование, раздел 7.

## Критерии приёмки
- e2e: фото «для всех» видно зрителю, которому скрыто основное, и не видно контакту, которому основное открыто; личное фото видит только тот, кто его поставил.
- Проверка в Android, iOS, Desktop и Web K.

## Результат
Фото «для всех» и личное фото контакта работают на сервере (ADR 0033). Фото «для всех» хранится ссылкой `users.fallback_photo_id` вне альбома: его ставят загрузкой или выбором своего фото с `fallback`, убирают `updateProfilePhoto(fallback, inputPhotoEmpty)` или `photos.deletePhotos`; основное фото и альбом при этом не меняются. Его видят в `user.photo` и `userFull.fallback_photo` те, кому правило «Фото профиля» скрывает основное, и сам аккаунт (только в `userFull`). Личное фото хранится на строке контакта владельца, видно только ему (`user.photo` с `personal`, `userFull.personal_photo`) и стирается с удалением контакта. `photos.uploadContactProfilePhoto` реализован полностью: `save`, `save` без файла (удаление), `suggest` (служебное сообщение `messageActionSuggestProfilePhoto`; предложенное фото получатель делает основным через `updateProfilePhoto`), оба флага сразу. Выбор фото для зрителя — одна функция для всех путей сборки пользователя. Проверка в клиентах (Android, iOS, Desktop, Web K) не проводилась.

**Изменения.**
- Миграция `teamgramd/deploy/sql/migrate-20261103-profile-photos.sql`: `users.fallback_photo_id`, `user_contacts.personal_photo_id` и `personal_photo_has_video`, таблица `user_suggested_photos`.
- proto (`// pluma:`): `UserData.fallback_photo`, `ContactData.personal_photo_id/personal_photo_has_video`; `ImmutableUser.FallbackPhoto`, `PhotoFor`, `ContactRowOf`, `PersonalProfilePhoto`, `photoFor` в `ToUnsafeUser` и `toUser`.
- biz user: `user.updateProfilePhoto` с `fallback`; новые RPC `user.setContactPersonalPhoto` и `user.addSuggestedPhoto` (по образцу `user.getStatusWatchers`); фото «для всех» в кэше пользователя, ошибка чтения фото — ошибка загрузки; принятие предложенного фото; строки контактов с личным фото на всех путях загрузки.
- bff: `photos.uploadProfilePhoto`/`updateProfilePhoto` (`fallback`, `bot` → `BOT_INVALID`/`BOT_FALLBACK_UNSUPPORTED`, `users` с самим аккаунтом), `photos.uploadContactProfilePhoto`, `users.getFullUser` (три фото), `contacts.deleteContacts` (фото без личного).
- media: ошибки сохранения размеров видео и фото больше не теряются.

**Проверка.**
- `go build ./...` — успешно; `go vet -unreachable=false` затронутых пакетов, с тегами `integration` и `e2e` — без замечаний; `make lint` (golangci-lint v2.15.0) — 0 issues.
- `go test` затронутых пакетов и всего `./app/... ./pkg/... ./tools/...` — успешно, `-race` на затронутых — успешно; `third_party/teamgram-proto/mtproto` — успешно.
- Юнит: `TestPhotoFor` (все пути сборки пользователя, личное над основным над «для всех», боты, сам аккаунт); без изменения `ToUnsafeUser` он падает. bff: `TestFallbackPhoto`, `TestProfilePhotoOfBot`, `TestUploadContactProfilePhoto`.
- Интеграция (CachyOS, MySQL/Redis через `pkg/testinfra`): `TestFallbackPhoto`, `TestContactPersonalPhoto`, `TestAcceptSuggestedPhoto`, `TestProfilePhotosMigrationAgain` и весь пакет `biz/user/internal/core` — успешно.
- e2e (стенд `pluma-e2e-051`): `TestPublicAndPersonalPhoto` — успешно; полный набор e2e — успешно (353 с). Стенд снят.
- `go run ./tools/apicoverage`: `photos.uploadContactProfilePhoto` — ok (photos 5/5).

**Решения, требующие подтверждения** (не описано в документации, выбрано более закрытое или согласованное с клиентами):
- `fallback_photo` шлётся только тем, кому основное скрыто, и самому аккаунту.
- `photos.getUserPhotos` тому, кому основное скрыто, — пустой альбом; фото «для всех» в альбом не входит.
- Личное фото стирается при удалении контакта.
- `CONTACT_MISSING` и для `suggest`.
- Смена фото «для всех» — `updateUser` с seq на устройства аккаунта и всем, кто его видит; личного фото — только на устройства владельца.
- Своё фото из альбома можно сделать «для всех», оно остаётся в альбоме; фото «для всех» можно сделать основным.
- Нет основного фото при разрешающем правиле — фото «для всех» не показывается (оно «из-за приватности»).
- Флаг `bot` — `BOT_INVALID`: у аккаунтов Pluma нет своих ботов.

**Отклонения и открытые вопросы.**
- Исследование утверждало, что эмодзи-аватар падает с `MEDIA_INVALID`. Это не так: tdesktop и Android шлют файл вместе с `video_emoji_markup` (`api_peer_photo.cpp:474-484`, `PrivacyControlActivity.java:242-255`), фото сохраняется; не хранится только сама разметка — отдельная задача.
- `ALBUM_PHOTOS_TOO_MANY` не реализован: у лимита нет источника; прежний TODO оставлен.
- Запись о предложенном фото не удаляется при удалении сообщения.
- Скачивание фото без проверки приватности (`dfs.downloadFile`) — задача 034.
- Сверка с Telegram (probe) по вопросам из ADR 0033 и проверка в четырёх клиентах не проводились.
