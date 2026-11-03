# The public ("fallback") profile photo and a contact's personal photo: research

**In short:** Pluma does not support either feature yet, and the public photo already causes a privacy bug. When a user sets a public photo, Pluma ignores the flag that marks it as public and makes it the main photo instead. Users the privacy rule hides the photo from still see no photo at all. Personal photos for contacts are not implemented: the method returns `METHOD_NOT_IMPL`.

Only code and documentation were read. Nothing was edited, committed or run in Docker.

## 1. How Telegram defines them (core.telegram.org)

| Item | Source | What it says |
|---|---|---|
| `photos.uploadProfilePhoto` `fallback:flags.3` | /method/photos.uploadProfilePhoto | The photo is "shown to users who can't see your main photo due to privacy settings". `bot:flags.5` changes the photo of a bot you own. Errors include `ALBUM_PHOTOS_TOO_MANY` and `BOT_INVALID`. |
| `photos.updateProfilePhoto` `fallback:flags.0`, `bot:flags.1`, `id:InputPhoto` | /method/photos.updateProfilePhoto | `fallback` makes an existing photo the public one. `inputPhotoEmpty` removes it. Errors: `BOT_FALLBACK_UNSUPPORTED` ("The fallback flag can't be set for bots"), `PHOTO_ID_INVALID`, `ALBUM_PHOTOS_TOO_MANY`. |
| `photos.uploadContactProfilePhoto` | /method/photos.uploadContactProfilePhoto | `suggest`: sends a `messageActionSuggestProfilePhoto` service message to `user_id`. Otherwise it sets "a personal profile picture … only visible to the current user". `save`: the page says it "removes a previously set personal profile picture" and does not affect suggestions, which are removed by deleting the message. Errors: `CONTACT_MISSING`, `NEED_ACTION_MISSING`, `USER_ID_INVALID`. Returns `photos.Photo`. |
| `userFull.personal_photo` (flag 21) | /constructor/userFull | Shown instead of `profile_photo`. |
| `userFull.fallback_photo` (flag 22) | /constructor/userFull | Shown "if no photo is present in profile_photo or personal_photo, due to privacy settings". |
| `userProfilePhoto.personal` (flags.2) | /constructor/userProfilePhoto | The photo is visible only to you and was set with `uploadContactProfilePhoto`. |
| A dedicated profile-photos page | /api/profile-photos returns 404; /api/files has no section on these | None exists. |

**Not in the documentation (UNVERIFIED):**
- which updates are sent;
- whether `fallback_photo` is also sent to viewers who can see the main photo;
- what `photos.getUserPhotos` returns to a viewer who cannot see the main photo;
- what happens to a personal photo when the contact is deleted.

These need a check against real Telegram (the reference-telegram-probe method).

The `save` wording reads oddly, but the clients settle it: they send `save` together with a file to set a personal photo, and `save` without a file to remove it.

## 2. What the clients do

### Setting and removing the public photo (Privacy > Profile photo)

| Client | Set | Remove | Where it keeps it |
|---|---|---|---|
| tdesktop | `uploadProfilePhoto` with `f_fallback` (api_peer_photo.cpp:474-481) | `updateProfilePhoto(f_fallback, inputPhotoEmpty)` (api_peer_photo.cpp:361-366) | A separate "back" slot outside the photo album (`UserPhotosSetBack`, api_peer_photo.cpp:489-492). Read back from `userFull.fallback_photo` (data_user.cpp:861-869, settings_privacy_controllers.cpp:1299). |
| Android | `uploadProfilePhoto`, `fallback=true` (PrivacyControlActivity.java:257-258) | `photos.deletePhotos([fallback])` (PrivacyControlActivity.java:604-618 → MessagesController.java:9038-9042) | `userFull.fallback_photo` of itself (PrivacyControlActivity.java:264-267). Added to the end of its own gallery by the client, own profile only (ProfileGalleryView.java:930-934). |
| iOS | `uploadProfilePhoto`, flags bit 3 (PeerPhotoUpdater.swift:195-201). Kept only in cached `fallbackPhoto`; `user.photo` is not changed (PeerPhotoUpdater.swift:293-309). | `updateProfilePhoto(fallback, inputPhotoEmpty)` (PeerPhotoUpdater.swift:431-434, 618-626) | `fallbackPhoto` read from `userFull` (UpdateCachedPeerData.swift:385, 493). |
| Web K | `uploadProfilePhoto`, `fallback` (appProfileManager.ts:1031-1053; privacy/profilePhoto.tsx:58-96) | n/a | Added as the last item of the photo list, own profile only (avatarListLoader.ts:8-41). |

Web K caveat: the tree in `reference-src/tweb` has a single commit (`346844b`) and project-style comments. It looks like a fork rather than the upstream release, so it should be re-checked against upstream.

### Showing another user's public photo

There is no flag for it. Clients recognise it by `userFull.fallback_photo.id == user.photo.photo_id`:
- Android: ProfileActivity.java:6829-6836 and ImageLocation.java:176-181. Under a personal photo, Android's "Reset to original" preview uses `profile_photo`, or `fallback_photo` when that is missing (ContactAddActivity.java:491-494).
- tdesktop: adds any user's `fallback_photo` to the end of their photo list (data_user.cpp:861-869).

So the server must:
- put the public photo in `user.photo` for viewers who cannot see the main photo;
- send the same photo as `userFull.fallback_photo`;
- keep it out of the `photos.getUserPhotos` album.

### Personal photo for a contact

| Client | Set | Remove | Suggest |
|---|---|---|---|
| tdesktop | `uploadContactProfilePhoto(save + file)`, then `updateFullForced()` (api_peer_photo.cpp:529-549) | `save` with no file (api_peer_photo.cpp:382-395) | `suggest + file` (api_peer_photo.cpp:534-535) |
| Android | `save=true` + file. Locally sets `fullUser.personal_photo` and applies it to `user.photo` (ContactAddActivity.java:881-934). | `save` with no file (ContactAddActivity.java:429-436) | `suggest=true`. Shows a local placeholder message until the real one arrives (ContactAddActivity.java:850-878, 900). |
| iOS | Mode `.custom` sets bit 4 (`save`), `.suggest` sets bit 3, `.customAndSuggest` sets both (PeerPhotoUpdater.swift:212-226) | `save` with no file (PeerPhotoUpdater.swift:441-443) | (same as set, bit 3) |

- **How it is displayed:** `user.photo` carries `personal=true` (tdesktop data_user.cpp:175-186). `userFull.personal_photo` is used together with `profile_photo` (data_user.cpp:851-860).
- **The album must not include it.** Both tdesktop (api_peer_photo.cpp:591-597) and Android (ProfileGalleryView.java:926-929) put the personal photo at the front of the gallery themselves.
- **Accepting a suggested photo:**
  - tdesktop calls `photos.updateProfilePhoto(inputPhoto)` with the suggested photo itself, which belongs to the sender (history_view_userpic_suggestion.cpp:51-56, 79-80 → api_peer_photo.cpp:198-207).
  - Android does the same (ChatActivity.java:36994-36999).
  - iOS goes through `updateProfilePhoto(image, mode: .accept)` (ChatController.swift:1306). Whether that re-uploads the image is UNVERIFIED.
- **What clients read back:** `photos.photo.photo` and `.users`. tdesktop calls `processUsers(vusers)` everywhere. The Web K tree builds a user itself when `users` is empty (appProfileManager.ts:1055-1067), which suggests Telegram normally fills `users` (UNVERIFIED).

## 3. Pluma today

| Area | File:line | State |
|---|---|---|
| `photos.uploadProfilePhoto` | app/bff/userchannelprofiles/internal/core/photos.uploadProfilePhoto_handler.go:28-60 | **Ignores `fallback` and `bot`.** Every upload becomes the main photo and is added to the album. A public photo set from Privacy (Android, tdesktop) therefore replaces the main photo and is hidden by the same rule it was meant to work around. Returns `users: []`. Has a `TODO: ALBUM_PHOTOS_TOO_MANY`. |
| `photos.updateProfilePhoto` | photos.updateProfilePhoto_handler.go:28-86 | Ignores `fallback` and `bot`. `updateProfilePhoto(fallback, inputPhotoEmpty)` from tdesktop and iOS **deletes the main photo**. Returns `users: []`. |
| `photos.uploadContactProfilePhoto` | photos.uploadContactProfilePhoto_handler.go:27-32 | `TODO: not impl`, returns `ErrMethodNotImpl`. |
| `photos.getUserPhotos` | photos.getUserPhotos_handler.go:39-51 | Returns an empty album when the privacy rule hides the photo (task 034). |
| `photos.deletePhotos` | photos.deletePhotos_handler.go:28-48 → biz `DeleteProfilePhotos` | Album photos only. |
| Ownership check for making a photo main | app/service/biz/user/internal/dao/self.go:294-305 (`ownPhotoQuery` :268) | Rejects a photo the account does not own with `PHOTO_ID_INVALID`, which blocks **accepting a suggested photo**. |
| Storage | `users.photo_id` (1_teamgram.sql:585); `user_profile_photos(user_id, photo_id, date2, deleted)` (:727-735); `user_contacts` (:601-613, plus `close_friend` and `stories_hidden` from migrations 20240108 and 20240620) | No column for a public or personal photo. |
| Cache | dao/cache_user_data.go:279-285 loads `UserData.profile_photo` through `MediaGetPhoto` | The error is discarded with `_`. |
| Proto `UserData` | third_party/teamgram-proto/mtproto/schema.tl.sync.proto:14246-14283 | `profile_photo` only. |
| Proto `ContactData` | schema.tl.sync.proto:2500-2511 | No photo field. |
| Builders of `user.photo` | immutable_user_helper.go:71-73 (`ProfilePhoto()`); ToUnsafeUser :424; toUser :671; bots :646; ToSelfUser :471 | Show the main photo or nothing (privacy since task 034). |
| `MakeUserProfilePhotoByPhoto` | photo_helper.go:49-64 | No `personal` flag, `DcId` fixed to 1, no stripped thumbnail. |
| Contact row in the viewed user | toUser: `m.GetReverseContactData(selfUserId)` (:647-ish); ToUnsafeUser: `selfUser.GetContactData(m.Id())` | The viewer's own contact row is already available on both paths, so a personal photo can ride on it. Contact rows are built in dao/user.go:566, :609, :915 (`addContactRow`), user_contact.go:207, :274, :296 and contact_signup.go:331. |
| `userFull` | app/bff/users/internal/core/users.getFullUser_handler.go:92-94 (all nil), :138-140 | Only `profile_photo` is filled, under the privacy check. |
| Updates | self.go:79-154 (`changeSelf`: `updateUser` with seq to the account's devices, plus `tellWatchers` with per-viewer `EmitSeqInbox`, dao/box.go:91-97, profile_watchers.go:57-68) | Watchers' users are built "as `to` sees them" when the update is applied, so a public photo shown per viewer reaches them automatically. |
| Media | app/service/media/internal/core/media.uploadProfilePhotoFile_handler.go:20-54 | `VideoEmojiMarkup` is passed in by bff but never forwarded: an emoji avatar arrives with file and video both nil and fails with `MEDIA_INVALID`. `SaveVideoSizeV2` and `SavePhotoV2` errors are discarded. |
| Photo download | app/service/dfs/internal/core/dfs.downloadFile_handler.go:129-147 | `inputPeerPhotoFileLocation` serves `a/` or `c/{photo_id}` **without checking the peer or the privacy rule**. Public and personal photos will download fine, but a hidden main photo can also be downloaded by anyone who knows its id. |
| Error codes | mtproto/rpc_error_codes.go:742-744 | `ALBUM_PHOTOS_TOO_MANY` exists. `CONTACT_MISSING`, `NEED_ACTION_MISSING` and `BOT_FALLBACK_UNSUPPORTED` are missing. |
| ADR 0031 | docs/adr/0031-privacy-evaluation.md:65 | Lists the public and personal photos as a separate task. |

## 4. How it should behave

What `user.photo` should hold for a viewer V looking at user U:
1. **The account itself:** its main photo. The public photo appears only in its own `userFull.fallback_photo`.
2. **V has a personal photo set for U** (V's contact row): that photo with `personal=true`, whatever U's privacy says.
3. **U's rule lets V see the photo:** U's main photo.
4. **Otherwise, U has a public photo:** the public photo. No flag on it; clients recognise it by its id.
5. **Otherwise:** no photo.
6. **Bots:** always the main photo, and no public photo (`BOT_FALLBACK_UNSUPPORTED`).

What `userFull` should hold for V:
- `personal_photo`: V's personal photo for U, if any.
- `profile_photo`: U's main photo when the rule allows it.
- `fallback_photo`: sent when V is U, or when `profile_photo` is hidden from V. Whether Telegram also sends it to viewers who can see the main photo is UNVERIFIED. If it did, tdesktop would add it to the end of that viewer's gallery (data_user.cpp:861-869).

What `photos.getUserPhotos` returns:
- U's album only, never the public or personal photo.
- An empty album when hidden, as now. What Telegram returns to a viewer who cannot see the main photo is UNVERIFIED.

## 5. Storage design proposal

**1. Migration `migrate-2026110x-profile-photos.sql`** (safe to run again; add columns only when missing, as migrate-20261027-profile-watchers.sql does):
- `users.fallback_photo_id BIGINT NOT NULL DEFAULT 0`.
- `user_contacts.personal_photo_id BIGINT NOT NULL DEFAULT 0`. It lives on the contact row because the method answers `CONTACT_MISSING` for non-contacts. The row is already cached as `UserContactsDO` JSON and already reaches `ContactData`.
- `user_suggested_photos(user_id, photo_id, from_user_id, date2, PRIMARY KEY(user_id, photo_id))`. This lets the recipient make a suggested photo main without owning it.

**2. Proto patches** (ADR 0009, `// pluma:`):
- `UserData.fallback_photo` (Photo).
- `ContactData.personal_photo` (Photo), or the id plus `has_video` if cache size matters.
- A `personal` argument for `MakeUserProfilePhotoByPhoto`.
- `ImmutableUser.FallbackPhoto()` and a single `photoFor(viewerContactRow, allowed)` used by ToUnsafeUser :424, toUser :671 and the getFullUser handler, so the paths cannot drift apart.
- The new errors `CONTACT_MISSING`, `NEED_ACTION_MISSING` and `BOT_FALLBACK_UNSUPPORTED`.

**3. The public photo is not part of the album.** It is a reference in `users.fallback_photo_id`, not a row in `user_profile_photos`. Clients add it to the gallery themselves (ProfileGalleryView.java:930-934, avatarListLoader.ts), and uploading with `fallback` does not change the main photo (iOS PeerPhotoUpdater.swift:293-299).

## 6. Exact changes

**biz user (`dao/self.go`)**
- Add `fallback_photo_id` to `selfRow` and `selfRowQuery` (:44-53), and to `selfUser` (:187-198).
- `UpdateProfilePhoto(ctx, id, photoId, uploaded, fallback)`. With `fallback`:
  - set or clear `users.fallback_photo_id`; the album is not touched;
  - push `updateUser` with seq to the account's devices and `tellWatchers` (the viewers who see the public photo change);
  - setting an existing photo as public: allow only the account's own photos (the `ownPhotoQuery` check).
- Without `fallback`, allow `photoId` that is in `user_suggested_photos` for the account. Making it main adds it to the album.
- `deleteProfilePhotos` (:342-375): when `ids` contains `fallback_photo_id`, clear it (Android removes the public photo with `deletePhotos`).
- Add `fallback` to `TL_user_updateProfilePhoto` (user.tl.proto:643-650).
- Load `fallback_photo` in `cache_user_data.go` next to `profile_photo` (:279-285), and fail the load on a read error instead of discarding it.

**biz user, new method `user.setContactPersonalPhoto(user_id, contact_id, photo_id)`**
- One box transaction of the owner: lock the contact row; `CONTACT_MISSING` if absent or deleted.
- Update `personal_photo_id` (0 removes it) and invalidate the contact-row cache key.
- Send `updateUser` with seq to the owner's devices only, with the user as the owner sees it. The ADR 0020 pattern; that Telegram sends this update is UNVERIFIED.
- Fill `ContactData.personal_photo` wherever contact rows are built (dao/user.go:566, :609, :915; user_contact.go:207, :274, :296; contact_signup.go:331).

**bff `photos.uploadProfilePhoto` / `photos.updateProfilePhoto`**
- Pass `fallback` through.
- `bot` set: `BOT_INVALID`, since Pluma has no bot owners. Today the request silently changes the caller's own photo.
- `fallback` together with `bot`: `BOT_FALLBACK_UNSUPPORTED`.
- `updateProfilePhoto(fallback, inputPhotoEmpty)` returns `photoEmpty`.
- Fill `users` with the account as it sees itself.
- Implement `ALBUM_PHOTOS_TOO_MANY` or drop the TODO (the limit needs a source).

**bff `photos.uploadContactProfilePhoto`**
- Neither `save` nor `suggest`: `NEED_ACTION_MISSING`.
- Target is not a user, or is the caller: `USER_ID_INVALID`.
- Not a contact: `CONTACT_MISSING`, at least for `save`; whether it also applies to `suggest` is UNVERIFIED.
- A file, video or emoji markup is present: upload through `MediaUploadProfilePhotoFile`. The photo is not added to anyone's album.
- `save`: `setContactPersonalPhoto(photo id, or 0 with no file)`.
- `suggest`: write `user_suggested_photos` for the recipient, then send a service message `messageActionSuggestProfilePhoto{photo}` from the caller to the recipient in their private chat through `MsgSendMessageV2`, as in messages.createChat_handler.go:139-206.
- Both flags together (iOS `.customAndSuggest`): do both.
- Return `photos.photo{photo, users:[the contact as the caller now sees it]}`.

**bff `users.getFullUser`** (:92-94, :138-140): fill `personal_photo`, `profile_photo` and `fallback_photo` by the rules in section 4.

**media:** forward `VideoEmojiMarkup` (media.uploadProfilePhotoFile_handler.go:26-31) and stop discarding errors (:43-50). Without this, emoji avatars fail for main, public and personal photos alike.

**Tests**
- Unit: the evaluator for the `user.photo` and `userFull` rules (personal over allowed over public), bots.
- Integration on MySQL: public photo set, removed by `updateProfilePhoto` and by `deletePhotos`; personal photo with `CONTACT_MISSING`; making a suggested photo main.
- e2e through `pkg/mtclient`:
  - B's rule is "nobody": A sees the public photo in `user.photo` and `userFull.fallback_photo`;
  - A sets a personal photo for B: A sees `personal=true`, B and C do not;
  - a suggestion reaches B as a service message, and B's `updateProfilePhoto` with that photo works.

## 7. Open questions

1. **Does Telegram send `fallback_photo` to viewers who can see the main photo?** If it does, tdesktop shows it at the end of their gallery. Check with the probe.
2. **What does `photos.getUserPhotos` return to a viewer who cannot see the main photo?** An empty album, or the public photo alone.
3. **Does the personal photo survive deleting and re-adding the contact?** With storage on the contact row, the row is soft-deleted, so the photo could be kept or cleared.
4. **Is `CONTACT_MISSING` also returned for `suggest`?**
5. **Does the suggestion's `updateNewMessage` reach the calling device?** The method returns `photos.Photo`, not `Updates`. If msg leaves the calling authorization out of the push, it must be delivered to it separately. Check msg's send-path semantics before implementing.
6. **Which updates does Telegram send** for a public-photo change (`updateUser` to everyone who sees the user?) and for a personal-photo change (`updateUser` to the caller's other devices?). Both are UNVERIFIED.
7. **Can a public photo be removed from the album and stay public?** Can an album photo be made public (`updateProfilePhoto(fallback, inputPhoto)`), and is it then taken out of the album? UNVERIFIED.
8. **Should downloads check privacy?** `upload.getFile` with `inputPeerPhotoFileLocation` serves any photo id with no check (dfs.downloadFile_handler.go:129-147), which partly defeats hiding the photo. This lies outside the public/personal photo work; it belongs to the privacy work (task 034, ADR 0031).
9. **Is the Web K reference tree upstream?** It has one commit (`346844b`) and project-style comments; re-read the upstream release before citing it.