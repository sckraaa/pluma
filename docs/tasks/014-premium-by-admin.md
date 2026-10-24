# 014. Premium, который выдаёт администратор

- Статус: todo
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
   - `grant <phone> [-months N | -until YYYY-MM-DD]`;
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

## Критерии приёмки
- `pluma-admin premium grant/revoke/list` работает на стенде.
- Официальный клиент выданного пользователя показывает Premium. Проверяет ревьюер на Android или Web K.
- e2e-сценарии из п. 5 зелёные. ADR 0016 соблюдён во всех местах из «Контекста».

## Результат
—
