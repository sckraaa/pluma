# 010. seq, getDifference и полнота апдейтов

- Статус: todo
- Зависит от: 002, 003, 007
- ADR: 0002

## Цель
Офлайн-устройство после возвращения получает ровно то, что пропустило, включая апдейты без `pts`.

## Область
По `docs/audit/01-updates-and-storage.md`:
- Реализовать `seq` и его журнал (`auth_seq_updates` сейчас никто не пишет): участники групп, имена, закрепы, настройки уведомлений.
- `updates.getDifference`: `differenceSlice`, `differenceTooLong`, корректные `state`; дыры от «сожжённых» `pts` исключены задачей 002.
- `qts` для секретных чатов — каркас.

## Критерии приёмки
- Сравнительные сценарии: устройство офлайн, N событий разных типов, возврат → совпадение с эталоном Telegram.
- Сняты известные отличия сценария `saved_messages` (`test/conformance/scenarios_test.go`, эталон Telegram от 2026-10-26).

## Что уже показал эталон Telegram (`saved_messages`, 2026-10-26)
Другой сеанс того же аккаунта запрашивает `getDifference` от состояния до отправки.
- **После правки** Telegram отдаёт:
  - в `new_messages` — сообщение в текущем виде: правленый текст и `edit_date`;
  - в `other_updates` — `updateMessageID` (id и `random_id` сообщения отправителя) и `updateReadHistoryInbox`, без `updateEditMessage`.
  У нас — сообщение в исходном виде и отдельный `updateEditMessage`.
- **После удаления** Telegram отдаёт:
  - `new_messages` нет;
  - в `other_updates` — `updateDeleteMessages` и `updateReadHistoryInbox`.
  У нас — удалённое сообщение в `new_messages` и `updateEditMessage`.
- **`state` разницы** у Telegram содержит `seq` (как `updates.getState`), у нас `seq` нет.
- **Совпадает уже сейчас:** у обновлений в `other_updates` нет `pts_count`, а `pts` равен итоговому `pts` разницы — так делает и Telegram.

## Результат
—
