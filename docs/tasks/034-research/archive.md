# Archive settings in Pluma: what the server must do (research only)

Short answer: the clients never move a cloud chat out of the archive on their own. They take a chat's folder from the server, either from an `updateFolderPeers` or from the `folder_id` of a dialog they fetch with `messages.getPeerDialogs`. So all three settings (`archive_and_mute_new_noncontact_peers`, `keep_archived_unmuted`, `keep_archived_folders`) only work if the server acts on them. Pluma stores the three flags today and acts on none of them. `folders.editPeerFolders` also still answers `ERR_ENTERPRISE_IS_BLOCKED`, so archiving by hand is not served either.

## Sources on the semantics
| Item | Source |
|---|---|
| `archive_and_mute_new_noncontact_peers`: "Whether to archive and mute new chats from non-contacts" | core.telegram.org/constructor/globalPrivacySettings |
| `keep_archived_unmuted`: unmuted chats stay in the archive when they get a new message (so when it is off, they leave it) | same page |
| `keep_archived_folders`: unmuted chats that are "always included or pinned in a folder" stay in the archive. The fetched text was cut off; the part saying it is ignored when `keep_archived_unmuted` is set is UNVERIFIED on the page, but the clients behave that way (rows below) | same page |
| tdesktop has three modes: None sends both keep flags, NotInFoldersUnmuted sends only `keep_archived_folders`, AnyUnmuted sends neither | tdesktop `api/api_global_privacy.h:21-25`, `api/api_global_privacy.cpp:236-245` |
| tweb forces `keep_archived_folders` on when `keep_archived_unmuted` is on | tweb `components/sidebarLeft/tabs/archiveSettingsTab.tsx:68-69` |
| `autoarchive_setting_available` (bool): "Whether clients can invoke account.setGlobalPrivacySettings with archive_and_mute_new_noncontact_peers = boolTrue" | core.telegram.org/api/config |
| The archive is `folder_id` 1. `folders.editPeerFolders` returns one `updateFolderPeers`, and clients also receive it "as a normal update" | core.telegram.org/api/folders |
| `updateFolderPeers` carries `pts` and `pts_count`, so it belongs to the common pts sequence | tdesktop `mtproto/scheme/api.tl:384`, `api/api_updates.cpp:1838-1841`; Android `MessagesController.java:17694-17729` |

## 1. A new message in an archived dialog: what each client does

| Client | Dialog it already knows (archived) | Dialog it does not know |
|---|---|---|
| Android | `updateInterfaceWithMessages` only updates the top message and date; it never touches `folder_id` (`MessagesController.java:21946-21980`). Only `updateFolderPeers` moves the chat, and it also unpins it (`applyFoldersUpdates`, `:18307-18333`; storage `MessagesStorage.java:17180-17220`). `userFull.folder_id` is applied when flag 2048 is set (`:7866-7871`). | Looks up the folder in the local DB, otherwise calls `loadUnknownDialog`, which sends `messages.getPeerDialogs` and uses `dialog.folder_id` (`:21932-21945`, `:12683-12727`). |
| tdesktop | `addNewMessage` changes no folder (`history/history.cpp:1695-1705`). The folder changes only through `updateFolderPeers` (`api/api_updates.cpp:1381-1395`), `applyDialogFields` (`history.cpp:3570-3578`) or `updateDialogPinned` (`:2559-2567`). `setFolderPointer` unpins the chat (`:2433-2445`). | When the folder is unknown it calls `requestDialogEntry` (`history.cpp:1703-1705`) and uses the dialog's `folder_id`. |
| iOS | Cloud chats: the chat's place in the list changes only through `UpdatePeerChatInclusion`, which comes from `updateFolderPeers` (`AccountStateManagementUtils.swift:1799-1806`, `:4467-4486`) or from a fetched dialog. The only local "unarchive on new message" code is for **secret chats**: archived, `!keepArchivedUnmuted` and not muted, then it moves the chat to root (`:5944-5960`). That code mirrors the rule the server applies to cloud chats. | Every message peer goes into `peerIdsRequiringLocalChatState` (`:108-146`). For one with no chat info, `resolveMissingPeerChatInfos` calls `messages.getPeerDialogs` and uses `folderId` and `notify_settings` (`:2972-2995`, `:3040-3060`, `:3277-3326`). |
| tweb | `onUpdateNewMessage` changes no folder for a known dialog. `onUpdateFolderPeers` moves the chat, unpins it in the old folder and pushes it again (`lib/storages/dialogs.ts:2405-2427`). | `handleNewUpdateAfterReload` calls `reloadConversation` (`messages.getPeerDialogs`) (`lib/appManagers/appMessagesManager.ts:9496-9506`, `:5735`). |

Muted and unmuted archived chats are handled the same way by every client for cloud chats. Only the server tells them apart. iOS treats a chat as muted when it is "removed from the total unread count", using its per-peer and global notification settings (`:5953-5956`).

## 2. An auto-archived chat from a non-contact in the clients

- **The bar.** It is drawn from `peerSettings.autoarchived` returned by `messages.getPeerSettings`:
  - Android `MessagesController.java:8096`, `ChatActivity.java:28882`, `:29188-29191`;
  - tdesktop `history_view_contact_status.cpp:809-810` (Unarchive or Block), `:840` (Unarchive or Report);
  - iOS `ChatReportPeerTitlePanelNode.swift:69-80`, `:131-133`;
  - tweb `components/chat/actions.tsx:65`, `:210`.
- **The Unarchive button** does the same thing in all four clients:
  1. `folders.editPeerFolders` with `folder_id` 0;
  2. the per-peer notification settings reset to default (`account.updateNotifySettings` with empty settings);
  3. `autoarchived`, `block_contact` and `report_spam` cleared, locally only.

  Sources: Android `ChatActivity.java:9638-9649`; tdesktop `history_view_contact_status.cpp:943-955`; iOS `PeerContactSettings.swift:61-82` and `ChatControllerLoadDisplayNode.swift:4011-4015` (folder change plus clearing the flags locally; it sends no notification reset). tweb (this copy is the Pluma fork) `appProfileManager.ts:1306-1325` re-fetches the peer settings after `editPeerFolders` (`appMessagesManager.ts:7400-7405`). If the server does not clear the flags itself, the bar comes back on the next `getPeerSettings`.
- **What the clients need from the server:**
  - The new dialog is created with `folder_id` 1 and a muted per-peer notification setting, both visible in `messages.getPeerDialogs` and `messages.getDialogs`.
  - `peerSettings.autoarchived` is set on the peer.
  - The account's other devices receive `updateNotifySettings`.
  - No client needs an `updateFolderPeers` for a dialog it does not know yet; the `getPeerDialogs` answer covers it.

## 3. The settings screens
| Client | Archive and mute toggle | keep_archived_* |
|---|---|---|
| Android | Privacy screen: shown if `autoarchiveAvailable` or Premium (`PrivacySettingsActivity.java:756`). Archive settings: locked without Premium or `autoarchiveAvailable` (`ArchiveSettingsActivity.java:99-115`, `:227`). The app-config key is read at `MessagesController.java:3184-3190`. | `ArchiveSettingsActivity.java:90-97` |
| tdesktop | Shown if it is already on or the key is set (`api_global_privacy.cpp:70-102`, `settings_privacy_security.cpp:481-523`, `:981-1014`). The `AUTOARCHIVE_POPULAR` suggestion is at `window_session_controller.cpp:1714-1720`. | `settings_advanced.cpp:1950-2015` |
| iOS | Privacy screen: shown if the key is set (`PrivacyAndSecurityController.swift:1605-1608`, `:791`). Archive settings: locked unless Premium (`ArchiveSettingsController.swift:137-140`). | `ArchiveSettingsController.swift:127-134`; flags at `UpdatedAccountPrivacySettings.swift:376-386` |
| tweb | Hidden unless Premium or `appConfig.autoarchive_setting_available` (`privacyAndSecurity.tsx:584`) | `archiveSettingsTab.tsx:54-70` |

Pluma currently sends `"autoarchive_setting_available": false` (`teamgramd/bin/app_config.json:17`), so only Premium accounts see the toggle.

## 4. Pluma today
| Area | State (file:line) |
|---|---|
| `folders.editPeerFolders` (bff) | **Not served**: returns `ErrEnterpriseIsBlocked` (`app/bff/folders/internal/server/grpc/service/not_served.go:30-32`) |
| `dialog.editPeerFolders` (service) | Exists but nothing calls it. The pin handling is confused: going to 0 it gathers the main-list pins and copies `FolderPinned` (`app/service/biz/dialog/internal/core/dialog.editPeerFolders_handler.go:27-74`). The update is `UpdatePeerDialogListFolderId` on a plain connection, not in a box transaction: no cache invalidation, no pin reset, no `updateFolderPeers` (`:78`; `mysql_dao/dialogs_dao.go:~1229-1234`) |
| Folder in dialogs | `MakeDialog` sets `FolderId` (`app/service/biz/dialog/internal/dao/dialog.go:40`, pins `:53-60`). `getDialogFolder` is forced to 1 (`dialog.getDialogFolder_handler.go:22`). The folder entry is added only in `messages.getPinnedDialogs` and `messages.getPeerDialogs` (`app/bff/dialogs/internal/core/messages.getPinnedDialogs_handler.go:72-81`, `messages.getPeerDialogs_handler.go:153`). |
| `userFull.folder_id` | Not set (`app/bff/users/internal/core/users.getFullUser_handler.go:99`, `:243` "TODO: FolderId"). `messages.getFullChat` does set it (`app/bff/chats/internal/core/messages.getFullChat_handler.go:74`). |
| First incoming private message | `InboxSendUserMessageToInboxV2`: one recipient box transaction, then `EmitPushUpdates` (`app/messenger/msg/inbox/internal/core/inbox.sendUserMessageToInboxV2_handler.go:31-41`). `ReceiveIncoming` (`app/messenger/msg/internal/dao/send.go:145`) calls `upsertIncomingDialog` (`send.go:268`). The upsert (`app/messenger/msg/internal/dao/box_store.go:36-38`, `:278-282`) never sets or changes `folder_id`; rows affected = 1 means a new row (`box_store.go:221-233`). A deleted dialog's row is removed (`box_store.go:303-321`), so the next message creates it again with `folder_id` 0. |
| Unarchive on a new message | **Nothing does it.** No code reads `KeepArchivedUnmuted` or `KeepArchivedFolders` except the storage handlers. |
| Global settings | Stored and read back correctly (`app/service/biz/user/internal/core/user.setGlobalPrivacySettings_handler.go`, `user.getGlobalPrivacySettings_handler.go`; bff `account.setGlobalPrivacySettings_handler.go:33-73`). Nothing acts on them. |
| Notification settings | Stored by the user service in a box transaction that pushes `updateNotifySettings` with seq (`app/service/biz/user/internal/dao/user_notify_settings.go:141-154`, update at `:203-209`). Dialogs carry them (`app/bff/dialogs/internal/core/messages.getDialogs_handler.go:63-94`, `messages.getPeerDialogs_handler.go:168-189`). |
| Peer settings | `messages.getPeerSettings` (`app/bff/dialogs/internal/core/messages.getPeerSettings_handler.go`) reads `user_peer_settings`, which has an `autoarchived` column (`app/service/biz/user/internal/dao/user_peer_settings.go:73-88`, `:95`). Writes go through `SetUserPeerSettings` / `DeleteUserPeerSettings` with `updatePeerSettings` seq (`:169-218`). **No message path writes peer settings**: `user.addPeerSettings` has no caller, so a non-contact's first message produces no `report_spam`, `add_contact`, `block_contact` or `autoarchived` bar. |
| Dialog filters (to check "in a folder") | The `dialog_filters` table keeps the filter as JSON per user (`teamgramd/deploy/sql/1_teamgram.sql:317-326`) |

## What to implement

**A. `folders.editPeerFolders`** (new bff handler, replacing the stub at `not_served.go:30`):
- Accept only `folder_id` 0 or 1 and dialogs that exist.
- In the account's box transaction:
  - set `folder_id`;
  - clear the pin of the folder being left (`pinned` or `folder_pinned`), as the clients do locally;
  - invalidate the dialog cache keys;
  - journal **one** `updateFolderPeers{folder_peers, pts, pts_count = number of peers}` as a pts update;
  - push it to the other devices and return it in `Updates`.
- When moving to 0 a peer whose `user_peer_settings.autoarchived` is set, also clear `autoarchived`, `report_spam` and `block_contact` and emit `updatePeerSettings` with seq. This persists what all four clients do locally; whether Telegram's server does it is UNVERIFIED.
- Do not unmute here: the clients send their own `account.updateNotifySettings`.
- Fix or replace `dialog.editPeerFolders` (no transaction, wrong pin handling).

**B. Auto-archive a new private chat from a non-contact** (`ReceiveIncoming` / inbox handler, same recipient box transaction):
- Conditions:
  - the dialog row is new (rows affected 1);
  - the peer is a user (UNVERIFIED for basic groups);
  - the sender is not in the recipient's contacts and is not the recipient;
  - the recipient has `archive_and_mute_new_noncontact_peers` on.
- Then, in that transaction:
  1. insert the dialog with `folder_id` 1;
  2. store per-peer `mute_until` = 2147483647 and journal `updateNotifySettings` with seq. The "forever" value is a design choice; Telegram's exact value is UNVERIFIED;
  3. store peer settings with `autoarchived`, plus `report_spam` and `block_contact`, and journal `updatePeerSettings` with seq.
- Send no `updateFolderPeers` for a dialog the clients do not know. Android, tdesktop, iOS and tweb all fetch it with `getPeerDialogs`, which must already show `folder_id` 1 and the mute. The transaction commits before the push, so the order is correct.

**C. Unarchive on a new message** (same transaction, for an existing dialog with `folder_id` 1 receiving an incoming message):
- Move it to 0 if all of these hold:
  - `keep_archived_unmuted` is off;
  - the chat is not muted (per-peer `mute_until` ≤ now; with no per-peer value, the per-type default for users, chats or broadcasts, as iOS does at `:5953-5956`);
  - and either `keep_archived_folders` is off, or the peer is in no filter's `include_peers` or `pinned_peers`.
- Then clear `folder_pinned` and journal `updateFolderPeers` (pts) next to the `updateNewMessage`, in the same push.

**D. Smaller items**
- Set `autoarchive_setting_available: true` in `teamgramd/bin/app_config.json:17`. This is the owner's product choice.
- Fill `userFull.folder_id` (`users.getFullUser_handler.go:243`).
- Check that `messages.getDialogs` with `folder_id` 0 returns the `dialogFolder` entry once the archive is no longer empty.

## Open questions
1. Do auto-archive and unarchive apply to basic groups and channels? A new group from a non-contact inviter is UNVERIFIED.
2. Should the recipient's own outgoing message in an archived chat unarchive it? UNVERIFIED; no client hint found.
3. Does the Telegram server itself clear `autoarchived` and the mute on `editPeerFolders(0)`, or only the clients? UNVERIFIED. Clearing the flags server-side is the safe choice because tweb re-reads them.
4. Does a non-contact's first message also need the ordinary `add_contact`, `block_contact` and `report_spam` bar when the setting is off? Pluma sets no bar today; this is a separate gap.
5. Is it worth the owner enabling `autoarchive_setting_available` for a closed group of about 100 users?