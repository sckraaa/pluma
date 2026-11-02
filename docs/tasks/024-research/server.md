## Task 024: server-side research


### 1. What each method returns today

**How a request is routed.** The session's bff proxy finds a gRPC client by the TL type name. A type is known only when its service is in the session.yaml `IDMap` (bff_proxy_client.go:60-76). If no client is found, the proxy calls `TryReturnFakeRpcResult` (bff_proxy_client.go:86-92). That function returns a canned answer when it has one, and otherwise `ErrEnterpriseIsBlocked` (400) (app/bff/bff/client/fake_rpc_result.go:177). So "missing" in api-coverage means the client gets `ERR_ENTERPRISE_IS_BLOCKED`. None of the services below is registered in app/bff/bff/internal/server/server.go (the registrations are at lines 100-453).

| Method | Today (file:line) | RPC service (schema.tl.sync_service_grpc.pb.go line) | session.yaml route |
|---|---|---|---|
| messages.getAvailableReactions | fake, empty list, hash 0 (fake_rpc_result.go:119) | RPCReactions (27680) | commented out (etc2/session.yaml:95) |
| messages.getTopReactions | blocked | RPCReactions (27685) | commented out |
| messages.getDefaultTagReactions | blocked | RPCSavedMessageTags (29356) | absent |
| messages.getAvailableEffects | blocked | RPCMessageEffects (20962), its only method | absent |
| messages.getStickerSet | blocked; the fake case is commented out (fake_rpc_result.go:105-108) | RPCStickers (32414) | commented out (:108) |
| messages.getAllStickers / getStickers / getRecentStickers / getFavedStickers / getArchivedStickers / getMaskStickers / getOldFeaturedStickers / getFeaturedStickers | fake, empty (fake_rpc_result.go:54, 91, 84, 64, 59, 70, 77, 96) | RPCStickers (32411-32445, 33 methods) | commented out |
| messages.searchStickerSets, installStickerSet, getAttachedStickers | blocked | RPCStickers | commented out |
| messages.getFeaturedEmojiStickers, getEmojiStickers | fake, empty (fake_rpc_result.go:98, 72) | RPCCustomEmojis (11402-11409) | absent |
| messages.getCustomEmojiDocuments, account.getDefaultProfilePhotoEmojis, account.getDefaultGroupPhotoEmojis, messages.searchCustomEmoji | blocked | RPCCustomEmojis | absent |
| messages.getEmojiKeywords / getEmojiKeywordsDifference / getEmojiKeywordsLanguages | fake, empty, version echoed back (fake_rpc_result.go:126, 134, 142) | RPCEmoji (12598-12603) | commented out (:86) |
| messages.getEmojiGroups / getEmojiStatusGroups / getEmojiProfilePhotoGroups / getEmojiStickerGroups | blocked | RPCEmojiCategories (12812-12817) | absent |
| account.getDefaultEmojiStatuses, getRecentEmojiStatuses, getChannelDefaultEmojiStatuses, getCollectibleEmojiStatuses | blocked | RPCEmojiStatus (13032-13043) | absent |
| help.getPeerColors, help.getPeerProfileColors, account.getDefaultBackgroundEmojis | blocked | RPCAccentColors (41-47); it also holds account/channels.updateColor | absent |
| account.getThemes, getChatThemes | fake, empty (fake_rpc_result.go:42, 47) | RPCThemes (35676-35687) | commented out (:81) |
| account.getTheme | blocked | RPCThemes | commented out |
| account.getWallPapers | fake, empty (fake_rpc_result.go:27) | RPCWallpapers (39140-39149) | commented out (:111) |
| account.getWallPaper, getMultiWallPapers | blocked | RPCWallpapers | commented out |
| help.getTimezonesList | blocked | RPCTimezones (36109-36111), its only method | absent |

The routes are in three files: teamgramd/etc/session.yaml, teamgramd/etc2/session.yaml and app/interface/session/etc/session.yaml (in the last one the comments sit at lines 66, 75, 61, 88 and 92). There is no sticker, emoji, reaction, theme or wallpaper table anywhere in teamgramd/deploy/sql.

Registering a whole service also takes over its sibling methods. Those currently answer blocked anyway, so nothing gets worse:
- In RPCReactions that includes sendReaction and getMessagesReactions (stage 2).
- In RPCAccentColors it includes updateColor.

Per CLAUDE.md, the siblings must keep answering `ErrEnterpriseIsBlocked`. Each fake case must also be deleted from fake_rpc_result.go once its service is routed.

### 2. How documents are stored and served

**Tables** (teamgramd/deploy/sql/1_teamgram.sql):
- **`documents`** (:334-351): `document_id`, `access_hash`, `file_path`, `file_size`, `mime_type`, `thumb_id`, `video_thumb_id`, `attributes` json, `date2`. `document_id` has only a non-unique KEY (:901-903).
- **`photo_sizes`** (:477): migrate-20220326.sql:1-4 dropped the volume/stripped columns and added `cached_type` and `cached_bytes`. There is a UNIQUE key on (`photo_size_id`, `size_type`) (:956).
- **`video_sizes`** (:776; unique key at :1074).

**Media DAO** (app/service/media/internal/dao):
- **`SaveDocumentV2`** (document.go, end of file): inserts a documents row with `file_path = "<id>.dat"`, and sets `ThumbId = VideoThumbId = document.Id` when thumbs exist. The rule is that photo_size_id equals document_id. It does not save the thumbs; callers call `SavePhotoSizeV2` and `SaveVideoSizeV2` separately (media.uploadedDocumentMedia_handler.go).
- **`makePhotoSizesDO`** (photo.go:117-158): keeps photoPathSize 'j' and photoStrippedSize 'i' inline, base64 in `cached_bytes`. Cached and progressive sizes are kept too. A photoSize row gets `file_path = "<type>/<id>.dat"`.
- **`getPhotoSize`** (photo.go:160-221) rebuilds 'j' and 'i' from the stored bytes.
- **`SaveVideoSizeV2`** (video.go:108+) stores markup sizes as JSON; plain videoSize rows are read back at video.go:75-106.
- **`MakeDocumentByDO`** and **`GetDocumentById`** (document.go) go through a Redis cache keyed `document_<id>`. File reference is empty and dc_id is 1.

**Object storage** (app/service/dfs; buckets in etc2/dfs.yaml:42-46: documents, photos, videos, encryptedfiles):
- **Main file:** `documents/<docId>.dat`. While the parts are still in the SSDB/Redis cache, the file is read from there (dao/cache_file.go:100-140).
- **Thumbs:** `photos/<type>/<docId>.dat`, or `videos/<type>/<docId>.dat` when `PhotoSizeIsVideo` says so (types u, v, f; image_thumbnail_types.go:62).

**`upload.getFile`** (app/bff/files/internal/core/upload.getFile_handler.go):
- For `inputDocumentFileLocation` it forwards to dfs.downloadFile.
- Without `thumb_size`, dfs reads `<id>.dat` from documents, and the storage type is taken from `access_hash >> 32` (dfs.downloadFile_handler.go:~58-73).
- With `thumb_size`, dfs reads `<thumb_size>/<id>.dat` from photos (always answered as `storage.fileJpeg`) or from videos (`fileMp4`).
- The access_hash is never checked.
- So any importer must make access_hash = `storageTypeCRC << 32 | rand32`, as dfs.uploadDocumentFileV2_handler.go:42-44 does. For .tgs and .webm, `GetStorageFileTypeConstructor` gives `filePartial` (dfs/internal/model/image_util.go:67-92); .webp gives `fileWebp`.

**`inputStickerSetThumb` (set thumbnails) cannot be served today:**
- The bff needs `Plugin1` (a `ThumbFilesPlugin`, app/bff/files/plugin/plugin.go:26-28) to turn the set into a location (upload.getFile_handler.go:83-99).
- server.go:203-208 passes `nil, nil`, so the request answers `ErrEnterpriseIsBlocked`.
- dfs then reads `photos/m/<location.Id>.dat` and labels it jpeg (dfs.downloadFile_handler.go:149-160). The TL constructor has no id field, so the plugin has to fill one in.
- A real `ThumbFilesPlugin` is needed. It should map set → thumb location with the right size type, bucket and storage type, because set thumbs can be webp, tgs or webm (UNVERIFIED).

**The existing upload paths cannot import Telegram documents faithfully:**
- `dfs.uploadDocumentFileV2` drops `documentAttributeAnimated` (dfs.uploadDocumentFileV2_handler.go:72).
- It makes its own stripped and 'm' thumbs, only for images. It cannot accept thumbs it is given (photoPathSize 'j', webp 'm', video thumbs).
- It allocates its own id through idgen. That part is fine, since the owner wants Pluma ids.
- It puts the main file in the SSDB cache and copies it to S3 asynchronously (lines 58, 190-210).
- `media.uploadStickerFile` is an enterprise stub (media.uploadStickerFile_handler.go:19-23), and `stickers.*` are all blocked.
- The gif and mp4 paths re-encode as well.

**Recommended import path:** add an import RPC that keeps what Telegram sent.
1. **`dfs.importDocument`**:
   - Input: creator, `InputFile` with the main bytes already written through `dfs.writeFilePartData`, mime type, and `thumbs` as photo sizes, where photoSize entries carry their bytes as parts or inline. Also `video_thumbs` with their bytes, and attributes passed through unchanged.
   - It gets the id from idgen, sets access_hash = type<<32|rand, and writes the main file synchronously with `PutDocumentFile("<id>.dat")`.
   - Each photoSize/progressive thumb goes to `photos/<type>/<id>.dat` and each videoSize to `videos/<type>/<id>.dat`.
   - 'i' and 'j' stay inline. It returns the `Document`.
2. **`media.importDocument`** calls it, then runs `SavePhotoSizeV2(id, thumbs)`, `SaveVideoSizeV2(id, videoThumbs)` and `SaveDocumentV2`.

The precedent for writing parts and then calling a media RPC from a tool is pkg/webpage/photos.go:43-66 and tools/pluma-admin/servicephoto.go. The importer rewrites `documentAttributeSticker.stickerset` and `documentAttributeCustomEmoji.stickerset` to our `inputStickerSetID` (our id and access_hash) before saving. A two-pass import does this: allocate the set ids first, then the documents.

The gRPC message-size limit makes inline thumb bytes fine but argues for parts for large files such as wallpapers and effect animations (the limit value is UNVERIFIED).

**Bugs I found in the existing code** (not fixed; report them separately):
- photo.go getPhotoSize, progressive case: `jsonx.UnmarshalFromString(szDO.CachedBytes, sizes)` passes `sizes` instead of `&sizes`, so a stored progressive size is lost.
- document.go `MakeDocumentByDO` reads `do.DocumentId` before its `do == nil` check.
- No access_hash check, on download or on `inputMediaDocument`: app/bff/messages/internal/dao/media.go:163-178 only does `MediaGetDocument` by id.
- Document thumbs are always labelled `storage.fileJpeg`, even when they are webp (UNVERIFIED whether clients care).

### 3. Existing sticker code and the store patterns to follow

**Sticker code:** apart from the stubs, references are limited to Plugin1, the dfs `inputStickerSetThumb` case, channels.setEmojiStickers (a stub) and push notification.go:356 and :380 (reading the sticker attribute). There is no storage.

**Pattern for imported catalog data: pkg/langpack (task 021, ADR 0025)**
- **`TelegramClient`**: connects to DC2 with a throwaway key and no login (pkg/langpack/telegram.go:28-110).
- **`Fetcher`**: handles FLOOD_WAIT (fetch.go:32-175).
- **Snapshots** on disk: `WriteSnapshot` / `ReadSnapshot` (model.go:183-240).
- **`Store.Import`**: plans the change and upserts it with versions and tombstones (store.go:361-530).
- **pluma-admin `langpack import`** (tools/pluma-admin/langpack.go:41-62).
- **Migration:** migrate-20261101-langpacks.sql (`CREATE TABLE IF NOT EXISTS`, safe to run again).
- **Serving:** the bff reads MySQL directly through `langpack.NewStore(sqlx)`.

Catalog methods need a logged-in account; the langpack fetch is anonymous. Which account to use is the open ADR question; the brief mentions the task 019 key reset. pkg/mtclient has no login flow or FILE_MIGRATE handling (exported functions in client.go:141-312).

**Pattern for hash / NotModified and per-user lists: app/bff/gifs**
- `documentsHash` follows core.telegram.org/api/offsets with Android's calcDocumentsHash (app/bff/gifs/internal/core/gifs.go:43-55).
- `NotModified` is returned when the client's hash equals ours (messages.getSavedGifs_handler.go:49-56).
- The per-user list table is `user_saved_gifs` (migrate-20261030-saved-gifs.sql), with changes made in box transactions (ADR 0020).

### 4. How to add a new bff service

1. In app/bff/bff/internal/server/server.go, call `mtproto.RegisterRPC<Name>Server(grpcServer, helper.New(...))`. The existing helpers take `Mysql`, `MediaClient` and `BizServiceClient` config (e.g. gifs at :277-283, langpack at :118).
2. Add `"/mtproto.RPC<Name>": "bff.bff"` to all three session.yaml files.
3. Delete the matching fake cases.
4. Add one handler file per method under app/bff/<area>/internal/core.

The services needed: RPCStickers, RPCCustomEmojis, RPCEmoji, RPCEmojiCategories, RPCEmojiStatus, RPCAccentColors, RPCThemes, RPCWallpapers, RPCTimezones, RPCMessageEffects, RPCReactions and RPCSavedMessageTags.

### Recommended storage design

**Code layout:**
- **pkg/catalog**, on the pkg/langpack model:
  - a fetcher that writes a raw snapshot (TL answers plus the downloaded files);
  - an importer that renumbers ids and rewrites every document and set reference;
  - a `Store` (sqlx) that the bff helpers read;
  - a pluma-admin `catalog import` command (on demand or scheduled, at most daily).
- **Migration:** a new `migrate-2026xxxx-catalog.sql` using `IF NOT EXISTS`.

**Tables:**
- **`catalog_documents`** `(sha256 PK, document_id, imported_at)`: makes re-import idempotent by the content of the main file, without storing Telegram ids. Documents are never changed; new content means a new id, so the Redis `document_<id>` cache never goes stale.
- **`sticker_sets`**:
  - Columns: `set_id PK` (from idgen), `access_hash`, `short_name UNIQUE`, `title`, `system_kind NULL UNIQUE`, set flags (official, masks, emojis, text_color, channel_emoji_status, creator), thumbs as JSON, `thumb_document_id`, `thumb_version`, `count`, `hash`, `updated_at`.
  - Values of `system_kind`: animated_emoji, emoji_default_statuses, premium_gifts, generic_animations, default_topic_icons, channel_default_statuses, animated_emoji_animations, dice:<emoticon>, ton_gifts. These match the `inputStickerSet*` predicates in class_name_registers.go.
- **`sticker_set_documents`** `(set_id, position, document_id)`, **`sticker_set_packs`** `(set_id, emoticon, position, document_id)` and **`sticker_set_keywords`** `(set_id, document_id, keyword)`: these build `messages.stickerSet` and `messages.getStickers`.
- **`catalog_lists`** `(list_key, position, ref_kind, ref_id, data json)`: ordered lists. Keys: featured_stickers, featured_emoji, default_emoji_statuses, default_profile_photo_emojis, default_group_photo_emojis, default_background_emojis, top_reactions, default_tag_reactions, plus a `catalog_list_hash (list_key, hash)` row.
- **`catalog_objects`** `(object_key PK, data json, hash, version, updated_at)`: whole answers after renumbering, kept as JSON of the mtproto struct, the way `documents.attributes` is stored. Covers available reactions, available effects, peer colors, peer profile colors, themes, chat themes, wallpapers, timezones and the four emoji group lists. The hash is recomputed over our own ids (core.telegram.org/api/offsets), since clients only send back what they received.
- **`emoji_keywords`** `(lang_code, keyword, emoticons json, version, deleted)` with **`emoji_keyword_langs`** `(lang_code, version)`: versions and tombstones like langpack_strings, so `getEmojiKeywordsDifference` works.
- **Per-user state (box transactions):**
  - `user_installed_sticker_sets` `(user_id, set_id, kind, order2, archived, installed_date)`;
  - `user_recent_stickers` and `user_faved_stickers` `(user_id, document_id, order2/date)`;
  - `user_read_featured_sets`.

  These follow user_saved_gifs. They are needed for getAllStickers, installStickerSet and the recent stickers.

**Storage reuse:** documents go through the new dfs/media import RPCs into the existing documents, photo_sizes, video_sizes tables and S3 keys. That way upload.getFile, media.getDocument, messages.sendMedia (`inputMediaDocument`) and the messages that embed documents work without changes. The exception is set thumbs: they need a real Plugin1.

OPEN:
Which Telegram account and client the import logs in with (logged-in is needed for catalog methods; pkg/mtclient has no auth.signIn or FILE_MIGRATE/DC switching), given the task 019 key reset: owner decision for the ADR.
Set thumbnails: implement Plugin1 (ThumbFilesPlugin) mapping inputStickerSetThumb to a location with id plus size type, and fix the dfs case that always reads photos/m/<id>.dat as jpeg; or always serve set thumbs through thumb_document_id? Which clients need which needs checking in client sources (UNVERIFIED).
Whether storing whole answers (reactions, effects, colors, themes, wallpapers, timezones) as JSON of mtproto structs is acceptable, or each should get typed columns like langpack.
Idempotent re-import without Telegram ids: dedupe documents by the sha256 of their content (recommended), and match sets by short_name/system_kind. Confirm that keeping short_name (a public Telegram name, not an id) is within the owner's renumbering decision.
gRPC max message size for the new dfs.importDocument: inline thumbs, but large main files (wallpapers, effect animations) should go through writeFilePartData parts. The configured zrpc limit was not checked.
Pre-existing bugs found, separate from task 024: photo.go getPhotoSize progressive unmarshal into a non-pointer; MakeDocumentByDO reads do before its nil check; no access_hash check on document download or on inputMediaDocument; document thumbs always labelled storage.fileJpeg.