# 050. Архив чатов: перенос, автоархив новых чатов от неконтактов, возврат из архива

- Статус: todo
- Зависит от: 034 (глобальные настройки приватности отвечают сохранённым)
- ADR: новый (архив и настройки архива)
- Исследование: `docs/tasks/034-research/archive.md`, сверка — `docs/tasks/034-research/check-archive-photo.md`.

## Цель
Архив работает как в Telegram на всех четырёх клиентах. Чат переносится в архив и обратно. Новые чаты от неконтактов архивируются и глохнут, если пользователь так настроил. Архивный чат без звука возвращается в список при новом сообщении, если «не возвращать из архива» не включено.

## Что известно (исследование, с цитатами)
- Клиенты сами облачный чат из архива не переносят. Папку чата они берут у сервера: `updateFolderPeers` (обновление с pts) или `folder_id` диалога из `messages.getPeerDialogs`. Источники: Android `MessagesController.java:21946-21980`, `:18307-18333`; tdesktop `history.cpp:1695-1705`, `api_updates.cpp:1381-1395`; iOS `AccountStateManagementUtils.swift:1799-1806`, `:5944-5960` (локальный возврат есть только для секретных чатов); tweb `dialogs.ts:2405-2427`.
- В Pluma `folders.editPeerFolders` отвечает `ERR_ENTERPRISE_IS_BLOCKED` (`app/bff/folders/internal/server/grpc/service/not_served.go:30-32`). Значит, ручная архивация тоже не работает.
- Три настройки (`archive_and_mute_new_noncontact_peers`, `keep_archived_unmuted`, `keep_archived_folders`) хранятся, но ни на что не влияют.
- Первое сообщение от неконтакта не ставит панель «Добавить в контакты / Заблокировать / Спам»: `user.addPeerSettings` никто не вызывает.

## Область
- `folders.editPeerFolders`: папка 0 или 1, транзакция ящика, снятие закрепа, `updateFolderPeers` с pts на все устройства. При переносе в 0 снимаются `autoarchived`, `report_spam`, `block_contact` с `updatePeerSettings` (seq).
- Автоархив нового личного чата от неконтакта в транзакции ящика получателя: диалог с `folder_id` 1, отключение звука с `updateNotifySettings` (seq), `peerSettings.autoarchived`.
- Возврат из архива при входящем сообщении по правилам `keep_archived_unmuted` и `keep_archived_folders`: `updateFolderPeers` рядом с `updateNewMessage`.
- `userFull.folder_id`. Запись `dialogFolder` в `messages.getDialogs`, когда архив не пуст.
- Панель действий для первого сообщения от неконтакта (`report_spam`, `add_contact`, `block_contact`) — проверить, нужна ли отдельной задачей.

## Вопросы владельцу
- Включить ли `autoarchive_setting_available` в `app_config.json`. Сейчас переключатель видят только аккаунты с Premium.

## Открытые вопросы (UNVERIFIED, сверка с Telegram)
- Касаются ли автоархив и возврат базовых групп.
- Возвращает ли из архива своё исходящее сообщение.
- Снимает ли сервер Telegram `autoarchived` и отключение звука при переносе в 0.
- Значение `mute_until` для «навсегда».

## Критерии приёмки
- e2e:
  - перенос в архив и обратно доходит до второго устройства;
  - новый чат от неконтакта при включённой настройке — в архиве, без звука, с `autoarchived`;
  - архивный чат со звуком возвращается из архива при новом сообщении;
  - с `keep_archived_unmuted` он остаётся в архиве.
- Интеграционные тесты транзакций ящика.
- Проверка в Android, iOS, Desktop и Web K.

## Результат
