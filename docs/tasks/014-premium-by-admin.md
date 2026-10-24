# 014. Premium, который выдаёт администратор

- Статус: done
- Зависит от: 011, 013 (места, где сейчас зашито «Premium нет»)
- ADR: 0016

## Цель
- Администратор выдаёт и снимает Premium отдельному пользователю, бессрочно или на срок.
- Клиенты этого пользователя видят Premium.
- Сервер даёт ему лимиты и поведение Premium там, где возможность реализована.
- Остальные — как пользователи Telegram без подписки.

## Контекст
- В схеме уже есть `users.premium` и `users.premium_expire_date` (`migrate-20220919.sql`, `migrate-20250109.sql`).
- `ImmutableUser.Premium()` (`third_party/teamgram-proto/mtproto/immutable_user_helper.go`) учитывает срок, и `ToUnsafeUser` выставляет флаг `premium` в объекте пользователя для клиентов.
- В сервисе пользователей есть `user.updatePremium(user_id, premium, months)` (`dao.UpdateUserPremium`). Его никто не вызывает. Проверить: что он пишет, как считает срок, сбрасывает ли кэш (`user_data`).
- **Где сейчас зашито «Premium нет»:**
  - **013, папки** (`app/bff/folders`, `app/service/biz/dialog/internal/dao/dialog_filter.go`): лимит 10 папок и 100 чатов; теги — всегда `PREMIUM_ACCOUNT_REQUIRED`; «Все чаты» всегда первые.
  - **011:**
    - `users.getIsPremiumRequiredToContact` — всегда `false`;
    - `users.getRequirementsToContact` — всегда `requirementToContactEmpty` (`app/bff/privacysettings`).
- **Приватность «новые не-контакты — только Premium»:** `user_global_privacy_settings.new_noncontact_peers_require_premium` (`account.setGlobalPrivacySettings`). Сейчас её никто не проверяет.
- Администрирование — `tools/pluma-admin` (по образцу `allowlist`).

## Область
1. **`pluma-admin premium`:**
   - `grant <phone> [-months N]`;
   - `revoke <phone>`;
   - `list` (у кого есть и до какого времени).
   - Номер нормализуется как при входе.
   - В лог — только `user_id`: номера в лог нельзя (`tools/logaudit`).
2. **Сервис пользователей:** выдача и снятие через `user.updatePremium` или его исправленную версию.
   - Сброс кэша пользователя.
   - Устройства пользователя узнают об изменении, как в Telegram. Что именно Telegram шлёт после покупки или окончания Premium (`updateUser`, пользователь в `updates`, …), выяснить по клиентам: DrKLO/Telegram `MessagesController`, tdesktop.
   - Истечение срока работает без действий администратора (`ImmutableUser.Premium()` уже сравнивает со временем). Клиент должен узнать и об истечении.
3. **Лимиты и поведение по флагу пользователя.** Значения Premium — из `help.getAppConfig` Telegram.
   - Папки: 30 папок и 200 чатов в папке.
   - Теги папок: Premium может их включить; `messages.getDialogFilters` отдаёт `tags_enabled`. Хранение — в сервисе диалогов (`dialog.toggleDialogFilterTags`, `getDialogFilterTags` — заглушки апстрима).
   - Положение «Все чаты» Premium может менять, как в Telegram.
   - Если флаг сняли, а папок больше лимита, ведём себя как Telegram (выяснить: Telegram не удаляет лишнее, но не даёт создавать новое).
4. **Приватность «только контакты и Premium»:**
   - Пользователь с Premium включает `new_noncontact_peers_require_premium`.
   - Не-контакт без Premium не может начать с ним переписку: ошибка Telegram `PRIVACY_PREMIUM_REQUIRED` в `messages.sendMessage` и других методах отправки.
   - `users.getIsPremiumRequiredToContact` и `users.getRequirementsToContact` отвечают по этим правилам.
5. **Тесты:**
   - юнит;
   - интеграционные на сервисе пользователей (выдача, срок, снятие);
   - e2e:
     - выдача через `pluma-admin` → свой пользователь с `premium`;
     - 11-я папка у Premium проходит, у бесплатного — нет;
     - приватность.

Не в этой задаче:
- Premium-функции, которых на сервере нет (расшифровка голосовых, перевод, эмодзи-статусы, большие загрузки и т. п.), — каждая своей задачей;
- платежи;
- скрытие экранов покупки в клиентах (ребрендинг).

## Решения (план реализации)

**Выдача и снятие.**
- `pluma-admin premium grant <phone> [-months N]` (без срока — бессрочно), `revoke <phone>`, `list`.
- `-until <дата>` не делаем: для неё нужен новый RPC с перегенерацией TL-кода сервиса, а месяцы — единица, которой Premium выдаёт и сам Telegram.
- `pluma-admin` вызывает `user.updatePremium` по RPC: кэш пользователя и пуш — дело сервиса. `list` читает таблицу `users` напрямую.

**`user.updatePremium` исправить.**
- Было:
  - выдача бессрочного Premium пользователю без Premium давала срок «сейчас», то есть Premium истекал сразу;
  - ошибки терялись.
- Будет:
  - `months = 0` — бессрочно;
  - `months > 0` — от действующего конечного срока или от сейчас;
  - снятие обнуляет оба поля.

**Транзакция и пуш.**
- Изменение — транзакция ящика пользователя с событием пуша (outbox): `updates{updateUser, users: [свой пользователь с флагом premium]}` на все его устройства.
- Android и tdesktop узнают Premium из объекта своего пользователя (`UserConfig` → `MessagesController.updatePremium`).

**Истечение срока.** Воркер в biz (как доставка регистраций в 011) раз в минуту находит истёкших, снимает флаг и шлёт тот же пуш.

**Лимиты папок по флагу:** 10 и 100 без Premium, 30 и 200 с ним.
- Хранение (`dao/dialog_filter.go`) читает флаг пользователя из `users` в той же транзакции ящика. Лимит чатов в папке переезжает туда же: оба лимита в одном месте.
- После снятия Premium лишние папки не удаляются (так делает Telegram, клиенты блокируют их у себя); новые сверх лимита не создаются.

**Теги папок.**
- Флаг пользователя хранится в `user_settings` (`dialog.toggleDialogFilterTags` / `getDialogFilterTags` вместо заглушек).
- Включить может только Premium. `messages.getDialogFilters` отдаёт `tags_enabled = флаг && Premium`.

**«Все чаты».** Premium может ставить их не первыми: позиция хранится строкой папки с id 0. Без Premium она игнорируется, «Все чаты» первые.

**Приватность «только контакты и Premium»** (`new_noncontact_peers_require_premium`), как в Telegram.
- Писать пользователю могут: Premium, его контакты и те, с кем уже есть личный чат. Остальным — `PRIVACY_PREMIUM_REQUIRED` при отправке.
- Включить настройку может только Premium.
- `users.getIsPremiumRequiredToContact` и `users.getRequirementsToContact` (`requirementToContactPremium`) отвечают по тем же правилам.

## Критерии приёмки
- `pluma-admin premium grant/revoke/list` работает на стенде.
- Официальный клиент выданного пользователя показывает Premium. Проверяет ревьюер на Android или Web K.
- e2e-сценарии из п. 5 зелёные. ADR 0016 соблюдён во всех местах из «Контекста».

## Результат
Сделано в ветке `task/014-premium`; работа с Premium описана в `docs/ops/premium.md`.

**Выдача и снятие.**
- `pluma-admin premium grant/revoke/list` вызывает исправленный `user.updatePremium`.
- Изменение — транзакция ящика пользователя (`dao/premium.go`) с пушем `updates{updateUser, users: [свой пользователь]}` на все его устройства.
- Истёкшие сроки снимает воркер biz раз в минуту.

**Папки** (`dialog_filter.go`).
- Лимиты 10/100 и 30/200 проверяются по флагу в той же транзакции.
- Теги хранятся в `user_settings` и включены, только пока есть Premium.
- «Все чаты» с Premium стоят на своём месте (строка папки 0), без Premium — первыми.

**Приватность «только контакты и Premium»** — общее правило `pkg/premium.RequiredToContact`.
- Отправка отвечает `PRIVACY_PREMIUM_REQUIRED`.
- `contact_require_premium` есть в пользователе и в `userFull`.
- По тому же правилу отвечают `users.getRequirementsToContact` и `getIsPremiumRequiredToContact`.
- Настройка едет с данными пользователя: поле `userData.new_noncontact_peers_require_premium`, патч `third_party(proto):` с перегенерацией `schema.tl.sync.pb.go` (protoc-gen-go v1.36.5). Ключ кэша — `user_data.3`.

**Попутно.** Из `users.getFullUser` убраны варианты подарка Premium апстрима (цены в CNY, ссылки на бот Telegram).

**Проверки.**
- Unit-тесты.
- Интеграционные тесты: сервис пользователей (выдача, срок, снятие, настройка в кэше), хранение папок, отправка в msg.
- e2e `TestPremium` (выдача, папки, теги, «Все чаты», приватность, снятие, срок) зелёный.
- Миграций нет.

**Осталось.**
- `help.getAppConfig` — пустая заглушка. Android поэтому ограничивает Premium 20 папками вместо 30 и показывает экраны покупки (задача 015).
- Лимит закреплённых чатов сервер не проверяет вовсе.
- Проверка в официальном клиенте за ревьюером (критерий приёмки).
