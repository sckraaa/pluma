## Review of the three task 024 research reports


### A. Six load-bearing claims I checked myself

1. **Unrouted methods return a canned answer or `ErrEnterpriseIsBlocked`.** **VERIFIED.**
   - The getAvailableReactions fake answer (empty, hash 0) is at `app/bff/bff/client/fake_rpc_result.go:118-122`. The emoji keyword fakes are at :125-145, and keywordsDifference echoes `in.LangCode` with `Version = FromVersion`. The getStickerSet case is commented out at :105-108. The function falls through to `ErrEnterpriseIsBlocked` at :176-177.
   - In `teamgramd/etc2/session.yaml`, RPCThemes, RPCEmoji, RPCReactions, RPCStickers and RPCWallpapers are commented out (:81, :86, :95, :108, :111). The other services are absent.
   - Small correction: the getAvailableReactions fake is at :119-122, not exactly :119.
2. **dfs takes the storage type from `access_hash>>32`, and `inputStickerSetThumb` is broken.** **VERIFIED.**
   - `app/service/dfs/internal/core/dfs.downloadFile_handler.go:74` reads `sType = int32(location.GetAccessHash() >> 32)`.
   - The sticker set thumb case (:149-161) reads `m/<location.GetId()>.dat`. That constructor has no id field, so the path is effectively `m/0.dat`. It always answers `fileJpeg`.
   - Files Plugin1 is `nil, nil` (`app/bff/bff/internal/server/server.go:203-208`).
   - The upload path builds access_hash as `extType<<32 | rand` at `dfs.uploadDocumentFileV2_handler.go:42-44`, and drops `documentAttributeAnimated` at :72.
3. **Existing bugs in the media DAO.** **VERIFIED, both.**
   - `app/service/media/internal/dao/photo.go:200` calls `jsonx.UnmarshalFromString(szDO.CachedBytes, sizes)` without `&`.
   - `app/service/media/internal/dao/document.go:79` reads `do.DocumentId` before the `if do == nil` at :81. That is a nil-pointer panic.
4. **Android retries every 1 s on error when hash is 0, and an empty answer stops it.** **VERIFIED** in the local 12.6.4 checkout.
   - Retry condition: `MediaDataController.java:3151-3157`, `!cache && res == null && hash == 0`, re-requested after 1000 ms. The featured loop is the same at :2323-2330.
   - An empty answer takes the `res.sets.isEmpty()` branch at :2595-2596.
   - Note that this Android build reads `res.hash2` at :2596, not `res.hash`. The long hash field has a different name in this fork of TLRPC. The behaviour is unchanged. It is still unchecked against the pinned 12.9.1.
5. **tdesktop hash formulas.** **VERIFIED** in the downloaded v7.0.9 `api_hash.cpp`.
   - `CountStickersOrderHash` skips Special and Archived sets and returns 0 if the default set is present.
   - `CountFeaturedHash` hashes each set id and adds `HashUpdate(1)` after each unread set.
6. **iOS drops a reaction unless all five required documents parse, and tweb behaves as described.** **VERIFIED.**
   - iOS: `AvailableReactions.swift:315-352` has a guard on staticIcon, appear, select, activate and effect each.
   - tweb logout: SESSION_REVOKED or AUTH_KEY_DUPLICATED always logs out, and any 401 logs out unless `noErrorBox` is set (`apiManager.ts:640-663`).
   - tweb `fetchLong` returns a number when the value is safe as an integer and a decimal string otherwise (`tl_utils.ts:481-498`).

### B. Claims that are uncited, contradict each other, or look wrong

- **getAppConfig rewrite (semantics §5).** The report says `help.getAppConfig must be rewritten together with the import` because of `emojies_sounds`. Pluma's `teamgramd/bin/app_config.json` has no `emojies_sounds` and no `premiumGiftsStickerPack`. `emojies_send_dice` is `[]` (:70).
  - So there is nothing to rewrite today. The real question is whether to **add** those keys, pointing at our renumbered ids.
  - The report also leaves out that app_config.json is a static file. A key that holds a document id plus an access_hash would have to be generated from the catalog store, not hand-edited.
- **Which account does the collecting.**
  - Server report: a Go `TelegramClient` on the pkg/langpack model needs `auth.signIn` and FILE_MIGRATE handling, and pkg/mtclient lacks both.
  - Collect report: assumes the owner's logged-in Web K session, with no Go client at all.
  - These are two incompatible collector designs. The brief (`docs/tasks/024-telegram-data-import.md:43`) leaves the account to the ADR. Neither report states the risk of the Web K route on the owner's main account, which is the task 019 key-reset concern.
- **Hashes.** The semantics report says to "never answer NotModified when the request hash is 0 or differs". That is consistent with the client code it cites. The server report never says this. The handler design must make it explicit.
- **"Keep short names" vs the renumbering rule.**
  - The semantics report cites EmojiAnimations by short name (`EmojiAnimationsOverlay.java:52`) and the hard-coded wallpaper slug.
  - The server report raises short_name as an open question.
  - Both are right that names and slugs are not ids. This should go into the ADR as an explicit carve-out.
- **Set thumb formats.** The server report marks "set thumbs can be webp, tgs or webm" UNVERIFIED. No report checks what `stickerSet.thumbs[].type` is for animated sets.
- **gRPC message size.** Every report leaves it UNVERIFIED. My grep for MaxMsgSize-style settings in dfs, media and pkg found nothing, so the go-zero/grpc default of 4 MB is likely, but that is also UNVERIFIED. Wallpapers and effect animations must go through parts.
- **Reference counts.** The collect report says "231 featured and 99 featured emoji sets", which is not from the brief table I read. The brief has 8 themes, 89 wallpapers, 186/206/208/30 emoji lists (:27-32). The "25–40k getFile requests" estimate has no derivation.
- **LNA and mixed content.** These rest on Chrome blog and explainer line numbers. "No CSP header" is a single observation; the report itself warns it may change. The prompt behaviour from the console is UNVERIFIED, as the report admits.
- **Hard-coded Telegram ids in clients** (tweb premium group icon, General topic icon, Android DIAMOND_EMOJI and others). These are cited but not reconciled with the renumbering decision. Patching the clients, or keeping an id map for those few ids, needs the owner's call.
- **Platform-specific themes.** The claim that `account.getThemes` documents differ per format is marked as the report's own inference. It needs one collector run per format: android, tdesktop, macos, and ios, which is unverified.

### C. What is still missing for an implementation design

1. **Storage.**
   - Choose typed tables or stored JSON of mtproto structs for reactions, effects, colors, themes and wallpapers.
   - The migration file name and the `IF NOT EXISTS` pattern.
   - A UNIQUE constraint on `documents.document_id`: today it is only a non-unique KEY, per the server report (not rechecked).
   - Per-user tables (installed, recent, faved, read-featured) only if getAllStickers state is in scope. Otherwise the answer is always empty with hash 0, which is valid.
2. **Renumbering map.** One function must rewrite every Telegram id. Places to cover:
   - Document ids in `messages.stickerSet.documents` and `packs[].documents`;
   - `stickerKeyword.document_id`;
   - `documentAttributeSticker.stickerset` and `documentAttributeCustomEmoji.stickerset` (id and access_hash, as `inputStickerSetID`);
   - `stickerSet.id`, `access_hash` and `thumb_document_id`;
   - stickerSetCovered/MultiCovered/FullCovered covers and documents, and featured `unread` ids;
   - availableReaction's seven documents;
   - availableEffect `static_icon_id`, `effect_sticker_id` and `effect_animation_id`;
   - `emojiStatus.document_id` and `emojiList.document_id`;
   - `emojiGroup*` `icon_emoji_id` and `document_id` lists;
   - `reactionCustomEmoji.document_id` in top and tag reactions;
   - `wallPaper.id`, `access_hash` and `document`;
   - `theme.id`, `access_hash`, `document`, and `themeSettings.wallpaper`;
   - `peerColorOption` has no document ids. Keep color ids 0-6 as-is, since Android's `needUpdate` relies on ids below 7.

   Also needed:
   - Sticker set thumb versions.
   - Any `messageEntityCustomEmoji` inside texts, if any are imported.
   - A rule for an id that is referenced but was never downloaded: fail the import, do not leave a dangling id.
3. **Importer.**
   - Snapshot format, ideally shared with both possible collectors: JSON with `{$b64}` bytes and longs as number-or-string.
   - Reject any unknown `_`.
   - Two passes: allocate set ids, then documents.
   - Dedupe by sha256 so a re-import is idempotent.
   - New `dfs.importDocument` and `media.importDocument` RPCs that keep the given thumbs: 'i' and 'j' inline, 'm' webp, video type 'f'.
   - A transaction boundary, and an atomic switch-over of the catalog version so a half-done import is never served.
4. **Handlers.**
   - Register 12 services in server.go and route them in all 3 session.yaml files.
   - Delete the fake cases.
   - Keep `ErrEnterpriseIsBlocked` for siblings that are not served.
   - Compute hashes over our ids with the core.telegram.org/api/offsets algorithm. Cover the int-hash vs long-hash methods. Compute `stickerSet.hash` (int32) for our sets.
   - Never answer NotModified on hash 0. Never return errors from getAllStickers or getFeatured* (the Android loop).
   - Echo `lang_code` exactly for keywords, and return an empty difference with a version for an unknown language.
   - A real `ThumbFilesPlugin` (Plugin1), plus a fix to the dfs `inputStickerSetThumb` path and storage type.
5. **Collector.**
   - The account and client decision (ADR).
   - Concrete scope: AnimatedEmojiAnimations, GenericAnimations, Dice per emoticon, TonGifts, DefaultTopicIcons, ChannelDefaultStatuses, EmojiAnimations by short name, all getEmojiKeywordsLanguages languages, and themes per format.
   - Downloads of thumbs and video_thumbs for every document.
   - A refresh on FILE_REFERENCE_EXPIRED.
   - Pacing and stop rules.
6. **Refresh.**
   - At most daily, owner-triggered (brief :43).
   - How a changed upstream set maps to the existing Pluma set: match by short_name or system_kind.
   - Bump `stickerSet.hash` and the list hashes so clients pick up the change within their 1–2 h poll.
   - Handle documents removed upstream: keep them, since messages reference them.

### Files read
- ~/teamgram-server/app/bff/bff/client/fake_rpc_result.go
- ~/teamgram-server/teamgramd/etc2/session.yaml
- ~/teamgram-server/app/service/dfs/internal/core/dfs.downloadFile_handler.go
- ~/teamgram-server/app/service/dfs/internal/core/dfs.uploadDocumentFileV2_handler.go
- ~/teamgram-server/app/service/media/internal/dao/photo.go
- ~/teamgram-server/app/service/media/internal/dao/document.go
- ~/teamgram-server/app/bff/bff/internal/server/server.go
- ~/teamgram-server/teamgramd/bin/app_config.json
- ~/teamgram-server/docs/tasks/024-telegram-data-import.md
- ~/aaaaaaa/Telegram-Android/TMessagesProj/src/main/java/org/telegram/messenger/MediaDataController.java
- (a local download) tdesk/api_hash.cpp
- Telegram-iOS AvailableReactions.swift (under ~/aaaaaaa/Telegram-iOS)
- ~/pluma-web/upstream/src/lib/appManagers/apiManager.ts
- ~/pluma-web/upstream/src/lib/mtproto/tl_utils.ts

OPEN:
Collector route: a Go MTProto client with login on a spare account (needs auth.signIn and FILE_MIGRATE in pkg/mtclient), or the owner's logged-in Web K session (risk to the main account)? The two reports assume different designs; this is an ADR and owner decision (brief :43).
Is it acceptable to keep Telegram short_names and wallpaper slugs (EmojiAnimations, fqv01SQemVIBAAAApND8LDRUhRU) under the renumber-all-ids decision?
Telegram document ids hard-coded in clients (tweb 5269590556232664327 and 5390854796011906616, Android DIAMOND_EMOJI and others): patch the clients, keep those ids, or accept the loss?
Should app_config.json gain emojies_sounds, emojies_send_dice and premiumGiftsStickerPack pointing at imported data? That would need generated, not static, config.
Are per-user installed, recent and faved sticker tables in scope for 024, or do getAllStickers and getEmojiStickers stay empty with hash 0?
What is the actual gRPC max message size for dfs and media? My grep found no setting, so likely the 4 MB default (UNVERIFIED).
Re-verify the Android line numbers (retry loop :3151, peer colors) at the pinned 12.9.1, and the iOS lines at the pinned commit.
Theme formats: collect account.getThemes per format (android, tdesktop, macos, ios), or only the cross-platform chat themes?