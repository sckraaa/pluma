# Spot-check of the "archive" and "photo" research reports

I checked 26 claims against the cited files and against core.telegram.org. Nothing was edited or run, and Docker was not used. 25 claims are CONFIRMED. None is WRONG. One needs a caveat about where its evidence comes from (A13).

## Archive report

| # | Claim | Verdict | Evidence |
|---|---|---|---|
| A1 | `folders.editPeerFolders` returns `ErrEnterpriseIsBlocked` | CONFIRMED | `app/bff/folders/internal/server/grpc/service/not_served.go:30-32` |
| A2 | `autoarchive_setting_available` is false | CONFIRMED | `teamgramd/bin/app_config.json:17` |
| A3 | Nothing reads `KeepArchivedUnmuted` or `KeepArchivedFolders` except storage | CONFIRMED | A grep over `app/` and `pkg/` finds only `user.set/getGlobalPrivacySettings_handler.go` and the DO `user_global_privacy_settings_do.go:17-18` |
| A4 | `user.addPeerSettings` has no caller | CONFIRMED | References are only the handler, generated code, the client wrapper and `contacts_seq_integration_test.go` |
| A5 | The incoming upsert never sets `folder_id`, and rows affected = 1 means a new row | CONFIRMED | `app/messenger/msg/internal/dao/box_store.go:36-38` has no `folder_id` in either INSERT or UPDATE. `app/service/biz/dialog/dialog/vars.go:154-165` relies on "without clientFoundRows", and no DSN in the repo sets it |
| A6 | A deleted dialog's row is removed, so the next message creates it again | CONFIRMED | `box_store.go:303-321` runs `DELETE FROM dialogs` |
| A7 | `dialog.editPeerFolders`: pin handling is confused, no transaction | CONFIRMED | `dialog.editPeerFolders_handler.go:27-74`. When moving to 0 it calls `SelectPinnedDialogsWithCB` but copies `v.FolderPinned` (main-list pins with the wrong column). Then it calls a plain `UpdatePeerDialogListFolderId` (`:78`) |
| A8 | `getDialogFolder` is forced to folder 1 | CONFIRMED | `dialog.getDialogFolder_handler.go:22` |
| A9 | `userFull.folder_id` is not set; `getFullChat` sets it | CONFIRMED | `users.getFullUser_handler.go:99` (nil) and `:243` (TODO); `messages.getFullChat_handler.go:74` |
| A10 | tdesktop modes: None sends both keep flags, NotInFoldersUnmuted sends only `keep_archived_folders`, AnyUnmuted sends neither | CONFIRMED | `tdesktop api/api_global_privacy.cpp:236-245` |
| A11 | tweb forces `keep_archived_folders` on when `keep_archived_unmuted` is on | CONFIRMED (fork code, see A13) | `archiveSettingsTab.tsx:68-69` |
| A12 | Unarchive button on Android: `addDialogToFolder(…, 0)`, clears the three bar flags, resets notifications | CONFIRMED | `ChatActivity.java:9638-9649`. `clearDialogNotificationsSettings` then calls `updateServerNotificationsSettings` (`NotificationsController.java:~5963`) |
| A12b | tdesktop unarchive: `ToggleHistoryArchived(false)`, `resetToDefault`, clears AutoArchived, BlockContact and ReportSpam | CONFIRMED | `history_view_contact_status.cpp:943-955` |
| A13 | "All four clients" do the same on Unarchive, and tweb re-fetches the peer settings | CONFIRMED in the code, but tweb is not evidence of what Telegram does | See the note below |
| A14 | iOS unarchives only **secret** chats on a new message: archived, `!keepArchivedUnmuted`, not muted | CONFIRMED | `AccountStateManagementUtils.swift:5944-5960`. This code ignores `keepArchivedFolders`, so it does not support part C's folder condition |
| A15 | tdesktop `addNewMessage` changes no folder and calls `requestDialogEntry` when the folder is unknown | CONFIRMED | `history.cpp:1703-1705` |
| A16 | Android applies `userFull.folder_id` when flag 2048 is set; the app-config key is read at `MessagesController.java:3184-3190` | CONFIRMED | `MessagesController.java:7869-7872`, `:3184-3190` |
| A17 | Semantics of `globalPrivacySettings`, including that `keep_archived_folders` is ignored when `keep_archived_unmuted` is set | CONFIRMED | core.telegram.org/constructor/globalPrivacySettings. The report marked the "ignored" part UNVERIFIED; the fetched page states it. The fetch tool returned that part as a summary, not verbatim |

**Note on A13 (tweb is the Pluma fork).** `appProfileManager.ts:1304-1325` `unarchiveFromSettingsBar` has a comment saying it "Mirrors tdesktop's `ContactStatus::setupUnarchiveHandler`". `reference-src/tweb` has a single commit (`346844b`). So that code is Pluma's own fork, not upstream tweb. Two things follow:
- "four clients" is in fact three: Android, tdesktop and iOS (iOS does not reset notifications).
- The argument for clearing `autoarchived` on the server rests on our own fork re-reading the peer settings. It is still a sound choice, but it is not evidence about Telegram's server.

## Photo report

| # | Claim | Verdict | Evidence |
|---|---|---|---|
| P1 | `uploadProfilePhoto` ignores `fallback` and `bot`, returns `users: []`, has a TODO for `ALBUM_PHOTOS_TOO_MANY` | CONFIRMED | `photos.uploadProfilePhoto_handler.go`: no `Fallback` or `Bot` reference anywhere; `:45` (TODO), `:58` (`users: []`) |
| P2 | `updateProfilePhoto(fallback, inputPhotoEmpty)` deletes the main photo | CONFIRMED | `photos.updateProfilePhoto_handler.go:28-86`: `photoId` stays 0, and `UserUpdateProfilePhoto(Id: 0)` with no fallback field removes the main photo (`dao/self.go:270-276` doc comment) |
| P3 | `uploadContactProfilePhoto` returns `ErrMethodNotImpl` | CONFIRMED | `photos.uploadContactProfilePhoto_handler.go:27-32` |
| P4 | `ownPhotoQuery` blocks accepting a suggested photo | CONFIRMED | `self.go:268` and `:294-305`: not-uploaded and not in `user_profile_photos` gives `PHOTO_ID_INVALID` |
| P5 | `cache_user_data.go` discards the `MediaGetPhoto` error | CONFIRMED | `:281` `userData.ProfilePhoto, _ = …` (also `SavedMusic`, `:285`) |
| P6 | Media does not forward `VideoEmojiMarkup`, so an emoji avatar fails with `MEDIA_INVALID`; the `SaveVideoSizeV2` and `SavePhotoV2` errors are discarded | CONFIRMED | bff passes it in (`uploadProfilePhoto_handler.go:35`). `media.uploadProfilePhotoFile_handler.go:21-24` rejects when file and video are both nil; `:26-31` sends dfs only File and Video; errors are discarded at `:43` and `:46-51` |
| P7 | `inputPeerPhotoFileLocation` is served with no peer or privacy check | CONFIRMED | `dfs.downloadFile_handler.go:129-147`: path `a/` or `c/{photo_id}` only. Read errors are also swallowed (`err = nil`) |
| P8 | `MakeUserProfilePhotoByPhoto` has no `personal` flag, `DcId` is 1 and there is no stripped thumbnail | CONFIRMED | `third_party/teamgram-proto/mtproto/photo_helper.go:49-64` |
| P9 | `ALBUM_PHOTOS_TOO_MANY` exists; `CONTACT_MISSING`, `NEED_ACTION_MISSING` and `BOT_FALLBACK_UNSUPPORTED` do not | CONFIRMED | `rpc_error_codes.go:743-744`; a grep finds none of the other three |
| P10 | tdesktop sets with `f_fallback`, removes with `updateProfilePhoto(f_fallback, inputPhotoEmpty)` and keeps it in a "back" slot | CONFIRMED | `api_peer_photo.cpp:474-492` and `:361-369`. Removal is sent only when the photo is the fallback one |
| P11 | tdesktop adds any user's `fallback_photo` to the back of the photo list | CONFIRMED | `data_user.cpp:861-869` (it skips `photoEmpty`) |
| P12 | Android sets with `fallback=true` and removes with `photos.deletePhotos([fallback])` | CONFIRMED | `PrivacyControlActivity.java:257-267` and `:604-619` (`deleteUserPhoto(inputPhoto)`); `MessagesController.java:9039-9042` |
| P13 | Android puts the personal photo first in the gallery and the fallback last, own profile only | CONFIRMED | `ProfileGalleryView.java:926-934` |
| P14 | iOS flags: account bit 3 = fallback; contact `.custom` bit 4 (`save`), `.suggest` bit 3, `.customAndSuggest` both; removal sends `save` with no file; a fallback upload does not change `user.photo` | CONFIRMED | `PeerPhotoUpdater.swift:195-226`, `:293-309`, `:441-443` |
| P15 | Semantics of `uploadContactProfilePhoto` (suggest, save wording, errors) | CONFIRMED | core.telegram.org/method/photos.uploadContactProfilePhoto: errors `CONTACT_MISSING`, `NEED_ACTION_MISSING`, `USER_ID_INVALID` |

## Gaps and decisions still open

1. **Folder on insert (archive B).** `upsertIncomingDialogQuery` has no `folder_id` parameter. Auto-archive needs one of two things:
   - a variant of the query that inserts `folder_id = 1` only on INSERT (never on the duplicate-key UPDATE); or
   - an `UPDATE` after a rows-affected = 1 result.

   The new-row cache keys already include `GetFolderNotPinnedDialogIdListCacheKey` (`vars.go:164`), so either way is safe for the cache.
2. **Deleting a dialog removes its row (A6).** After a deleted, archived chat, the next message from a non-contact is a "new dialog" again. With the setting on it is archived again; with it off it lands in folder 0. This matches "new chat" semantics, but whether Telegram does the same is UNVERIFIED.
3. **The `keep_archived_folders` condition in archive C has no client source.** The iOS secret-chat code ignores it (A14). Only the doc text supports it. Implementing it means parsing filter JSON (`dialog_filters`) inside the inbox transaction, which is a cost to note.
4. **tweb as a reference (A13, P9 of the photo report).** Both reports cite the Pluma fork. Upstream tweb behaviour (the archive settings in A11, `avatarListLoader`, `appProfileManager` filling `users`) is UNVERIFIED until it is read at the pinned upstream release.
5. **P7 is a privacy hole, not a photo task.** Any hidden main photo can be downloaded by id. It belongs to the current privacy work (task 034, "all privacy settings must work"). The check is `peer` plus the profile-photo rule, or the photo is one of the viewer's own, personal or public photos. The dfs swallowed read error should be fixed at the same time.
6. **P6 should be fixed before any fallback or personal photo work.** Emoji avatars fail for every kind of photo.
7. **Owner decision:** `autoarchive_setting_available` (A2) is still the owner's product choice.
8. **Still UNVERIFIED after this check:**
   - the "forever" mute value;
   - whether the server clears `autoarchived` on `editPeerFolders(0)`;
   - whether `fallback_photo` is sent to viewers who can see the main photo;
   - what `getUserPhotos` returns to a hidden viewer;
   - which updates are sent on public and personal photo changes.

   These need the real-Telegram probe (Web K `invokeApi` in two tabs).