# 064. Общие папки (chatlists.*)

- Статус: done (сервер; проверка в клиентах и правка ссылок клиентов — позже)
- Зависит от: 013 (папки), 041 (вступление и приглашения каналов), 047 (перевод группы в супергруппу), 026 (ссылки pluma.chat)
- ADR: 0020 (seq), 0034 («Ящик канала»), 0036 (ссылки pluma.chat)

## Цель
Папку с каналами и группами можно разослать ссылкой `https://pluma.chat/addlist/<slug>`. Открывший ссылку видит каналы, выбирает нужные, вступает и получает папку; владелец добавляет новые каналы в ссылку, а получившие папку видят их как новые.

## Что известно
- **Методы слоя 228:** `chatlists.exportChatlistInvite`, `deleteExportedInvite`, `editExportedInvite`, `getExportedInvites`, `checkChatlistInvite`, `joinChatlistInvite`, `getChatlistUpdates`, `joinChatlistUpdates`, `hideChatlistUpdates`, `getLeaveChatlistSuggestions`, `leaveChatlist`; `dialogFilterChatlist` (core.telegram.org/constructor/dialogFilterChatlist, third_party/teamgram-proto/mtproto/schema.tl.sync.proto).
- **Что можно поделиться.** Только каналы и группы/супергруппы: публичные, либо если пользователь управляет приглашениями; базовые группы при экспорте превращаются в супергруппы (core.telegram.org/api/folders, method/chatlists.exportChatlistInvite). Клиентское правило совпадает: iOS `canShareLinkToPeer` (`ChatFolders.swift:6-29`: создатель, `canInviteUsers` или публичный без request-to-join), Android `FilterCreateActivity.canAddToFolder:746`, tdesktop `edit_filter_links.cpp:87-100`.
- **Папки только из перечисленных чатов.** tdesktop `GoodForExportFilterLink` (`edit_filter_links.cpp:1000-1009`): `lng_filters_link_cant` при исключениях и типах чатов; при `FILTER_NOT_SUPPORTED` — такая же ситуация.
- **Лимиты** (core.telegram.org/api/config): `chatlist_invites_limit_default/_premium` 3/100, `chatlists_joined_limit_default/_premium` 2/20, `chatlist_update_period` 3600 (`data_chat_filters.cpp:37`). `teamgramd/bin/app_config.json` был со значениями премиума 3 и 2 — выставлены по документации.
- **Ошибки** (core.telegram.org/method/...): `PEERS_LIST_EMPTY`, `CHANNEL_INVALID`, `CHANNEL_PRIVATE`, `CHAT_ADMIN_REQUIRED`, `CHATLISTS_TOO_MUCH`, `INVITES_TOO_MUCH`, `FILTER_ID_INVALID`, `FILTER_NOT_SUPPORTED`, `FILTER_INCLUDE_EMPTY`, `INVITE_SLUG_EMPTY`, `INVITE_SLUG_EXPIRED`, `INPUT_CHATLIST_INVALID`, `CHATLIST_EXCLUDE_INVALID`. Реакция клиентов — окна лимитов: tdesktop `edit_filter_box.cpp:1074-1080`, `api_chat_filters.cpp:672-676`; iOS `ChatFolders.swift` (exportChatFolder, joinChatFolderLink), Android `FilterCreateActivity.java:2782-2784`. `INVITE_SLUG_EXPIRED` показывается как плохая ссылка (`api_chat_filters.cpp`, ShowImportError).
- **Ответ joinChatlistInvite обязан содержать `updateDialogFilter`:** iOS иначе возвращает общую ошибку (`ChatFolders.swift:420-446`).
- **Первый импорт `chatlistInvite`, повторный — `chatlistInviteAlready`** с `filter_id`, `missing_peers`, `already_peers` (tdesktop `api_chat_filters.cpp:912-921`, tweb `sharedFolderInvite.tsx`). Участников в превью приватного канала клиенты читают из `chats` (`participantsCount`, iOS `:216-223`), поэтому не-участнику показывается `channel` с `left`, а не `channelForbidden`.
- **Формат ссылки.** `addlist/<slug>`: разбирают `ui/LaunchActivity.java:2164`, tdesktop `local_url_handlers.cpp:1944`, tweb `internalLinkProcessor.ts:246`, iOS `OpenUrl.swift:638`. Патч iOS снимает префикс `https://pluma.chat/addlist/` (`clients/patches/ios.md:814`); патчи Android и Web покрывают `addlist` (`clients/patches/android.md:779`, `clients/patches/web.md:337`). Страница и маршрут ссылки есть (`app/interface/links/internal/links/route.go:154`).
- **Удаление папки с моими ссылками** гасит их (`lng_filters_delete_sure`). Удаление последней ссылки делает папку обычной (Android `FilterCreateActivity.onDelete:750-765`).

## Область
- Обработчики всех 11 методов (`app/bff/folders/internal/core/chatlists.*_handler.go`), логика в `chatlists.go`; `not_served.go` теперь только `folders.deleteFolder`.
- Хранение ссылок, каналов ссылок и «виденных» каналов — `dao/chatlist.go` сервиса dialog, 7 новых RPC (`dialog.exportChatlistInvite` и другие), миграция `migrate-20261108-shared-folders.sql`.
- `dialogFilterChatlist` в `messages.updateDialogFilter`: остаётся общей, `has_my_invites` и импорт хранятся сервером; каналы в папках теперь принимаются (раньше `PEER_ID_INVALID`).
- `ToInvitedChat` — вид канала для ссылки.
- Клиенты не трогаем (вопрос в «Результате»).

## Критерии приёмки
- Модульные тесты: правила папок, вид канала, разбор запроса.
- Интеграционные тесты на MySQL (`pkg/testinfra`): правила и лимиты экспорта, конкурентные ссылки, вступление, повтор, обновления, скрытие, удаление.
- e2e через `pkg/mtclient`: полный сценарий двух пользователей, базовая группа при экспорте.
- Обычные проверки: `go build ./...`, `go vet -unreachable=false`, `go test` и `-race` затронутых пакетов, `make lint`, `go test ./tools/...`.

## Решения
- **Вступление по каналам, затем запись папки.** Сначала проверка лимитов папки (`dialog.joinChatlist`, `check_only`), потом вступление по одному через `channel.joinChannel`, в конце папка из тех каналов, куда пользователь вошёл или где ждёт одобрения. Сбой посередине не теряет вступления: повтор продолжает с них (`USER_ALREADY_PARTICIPANT` — не ошибка).
- **Экспорт и правка ссылок** — одна транзакция ящика владельца с блокировкой каналов по возрастанию id (`channelbox.Open`, `channelperm`): ссылка, приглашения каналов (`chat_invites`) и `updateDialogFilter` с seq фиксируются вместе.
- **Приглашение канала создаётся** для приватного канала и для публичного с `join_request`; публичный без подтверждения вступает по имени.
- **Запрос на вступление** (`INVITE_REQUEST_SENT`) оставляет канал в папке: когда админ одобрит, он появится в ней сам.

## Результат
Общие папки работают на сервере: владелец делает ссылку `pluma.chat/addlist/...` на папку из каналов (приватные — через приглашение на канал, публичные — по имени; базовая группа сначала переводится в супергруппу, как просит документация), получатель видит каналы, выбирает их, вступает и получает папку (`dialogFilterChatlist`); новые каналы в ссылке приходят как новые, их можно принять или скрыть; при удалении папки можно выйти из её каналов; удаление папки или последней ссылки гасит приглашения.

Пока не проверено в живых клиентах (Android, iOS, Desktop, Web); e2e идёт через mtclient. Не сделано: выход из базовых групп в `chatlists.leaveChatlist` (клиенты шлют каналы; Android может отправить `peerChat` только для папок со смешанным составом, такие группы остаются, папка удаляется); флаг `via_chatlist` у импортёров канала (в просмотре импортёров каналов его негде показать, таблица импортёров у каналов отдельным списком не обслуживается).
