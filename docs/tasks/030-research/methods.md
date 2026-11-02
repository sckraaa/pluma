Installed sticker / mask / custom-emoji sets: layer-228 semantics (read-only research)

Sources and line numbers:
- Android: pinned DrKLO/Telegram aac1efcb, raw files from GitHub. Lines marked "local" are from ~/aaaaaaa 12.6.4.
- tdesktop: tag v7.0.9, raw files.
- iOS: ~/aaaaaaa/Telegram-iOS/submodules/TelegramCore/Sources.
- Web K: ~/pluma-web/upstream at 346844b.
- Docs: core.telegram.org, fetched today.

## 1. Exact TL (pkg/catalog/tl/api.tl)

Methods:
- messages.getAllStickers#b8a0a1a8 hash:long = messages.AllStickers (line 2495)
- messages.installStickerSet#c78fe460 stickerset:InputStickerSet archived:Bool = messages.StickerSetInstallResult (2501)
- messages.uninstallStickerSet#f96e55de stickerset:InputStickerSet = Bool (2502)
- messages.reorderStickerSets#78337739 flags:# masks:flags.0?true emojis:flags.1?true order:Vector<long> = Bool (2508)
- messages.getArchivedStickers#57f17692 flags:# masks:flags.0?true emojis:flags.1?true offset_id:long limit:int = messages.ArchivedStickers (2528)
- messages.getMaskStickers#640f82b8 hash:long = messages.AllStickers (2529)
- messages.toggleStickerSets#b5052fea flags:# uninstall:flags.0?true archive:flags.1?true unarchive:flags.2?true stickersets:Vector<InputStickerSet> = Bool (2575). It returns Bool, not an install result.
- messages.getEmojiStickers#fbfca18f hash:long = messages.AllStickers (2629)

Results:
- messages.allStickersNotModified#e86602c3 (657)
- messages.allStickers#cdbbcebb hash:long sets:Vector<StickerSet> (658)
- messages.archivedStickers#4fcba9c8 count:int sets:Vector<StickerSetCovered> (893)
- messages.stickerSetInstallResultSuccess#38641628 (895)
- messages.stickerSetInstallResultArchive#35e410a8 sets:Vector<StickerSetCovered> (896)
- StickerSetCovered has four constructors:
  - stickerSetCovered#6410a5d2 set cover:Document (898)
  - stickerSetMultiCovered#3407e51b covers:Vector<Document> (899)
  - stickerSetFullCovered#40d13c0e packs keywords documents (900)
  - stickerSetNoCovered#77b15d1c (901)
- stickerSet#2dd14edc (701) has these flags:
  - archived:flags.1, official:flags.2, masks:flags.3, emojis:flags.7
  - installed_date:flags.0?int
  - hash:int

Updates:
- updateNewStickerSet#688a30aa stickerset:messages.StickerSet (351)
- updateStickerSetsOrder#bb2d201 masks:flags.0 emojis:flags.1 order:Vector<long> (352)
- updateStickerSets#31c24808 masks:flags.0 emojis:flags.1 (353)
- updateMoveStickerSetToTop#86fccf85 masks:flags.0 emojis:flags.1 stickerset:long (425)

## 2. Documentation (core.telegram.org)

https://core.telegram.org/api/stickers says:
- Installing can return messages.stickerSetInstallResultArchive, meaning the set was installed "displacing some older unused stickersets". The constructor page (/constructor/messages.stickerSetInstallResultArchive) says "since there are too many stickersets some were archived". Its `sets` field is "Archived stickersets", meaning the sets that were displaced, not the installed one.
- updateNewStickerSet is emitted to the other logged-in sessions on install.
- updateStickerSets is emitted to the other sessions on uninstall or archive. Those sessions then refetch:
  - getMaskStickers if `masks` is set;
  - getEmojiStickers if `emojis` is set;
  - otherwise getAllStickers and getArchivedStickers.
- updateStickerSetsOrder is emitted to the other sessions on reorder.
- Normal, mask and custom-emoji sets are "ordered independently", chosen by the masks/emojis flags.
- getAllStickers does not return archived sets.
- stickerSetFullCovered is "currently only used for custom emoji stickersets".
- updateMoveStickerSetToTop is produced by sending with `update_stickersets_order` (outside this task's methods).
- There is no stated maximum number of installed sets.

Method pages:
- /method/messages.installStickerSet: errors = 406 STICKERSET_INVALID only.
- /method/messages.uninstallStickerSet: errors = 406 STICKERSET_INVALID only.
- /method/messages.toggleStickerSets, reorderStickerSets, getArchivedStickers and getAllStickers: no error table; only "Only users can use this method".
- getArchivedStickers paging is described only as "Offsets for pagination", with no algorithm.

## 3. STICKERSET_INVALID: 400 or 406?

- The docs list 406 for install, uninstall and getStickerSet (/method/messages.getStickerSet: 400 EMOTICON_STICKERPACK_MISSING, 406 STICKERSET_INVALID).
- https://core.telegram.org/api/errors: 406 means the client must not show the error. It waits for updateServiceNotification instead.
- Android: AlertsCreator.processError returns silently for code 406 (pinned AlertsCreator.java:291 and :6808).
- tdesktop: StickerSetBox install `.fail` shows "not found" (boxes/sticker_set_box.cpp:2400-2408) for any error. StickersBox::installFail undoes the local install (boxes/stickers_box.cpp:970-978).
- iOS: maps any error to `.generic` (StickerSetInstallation.swift:177-179). Most other iOS calls `catch` and ignore errors (ManagedSynchronizeInstalledStickerPacksOperations.swift:259-261, 281-292).
- Upstream proto has both constants:
  - mtproto.ErrStickersetInvalid (400), third_party/teamgram-proto/mtproto/rpc_error_codes.go:1155-1156
  - ErrNotAcceptableStickersetInvalid (406), :2363-2364
- Our getStickerSet already uses the 400 one (app/bff/catalog/internal/core/messages.getStickerSet_handler.go:60,69).
- Which code live Telegram actually returns is UNVERIFIED. It can be checked with the Web K probe.
- Recommendation:
  - The clients' functional handling is the same for both codes (tdesktop and iOS ignore the code; Android only differs in showing or hiding an alert).
  - Pick one code for the whole sticker area and record it in the brief.
  - The docs say 406. With 406 and no updateServiceNotification, Android shows nothing at all.

## 4. Per method: what the server must do

### getAllStickers / getEmojiStickers / getMaskStickers (hash:long)

- Return messages.allStickers{hash, sets} with the account's installed, non-archived sets of that kind, in the user's order.
  - Kind: plain sets for getAllStickers, emojis-flag sets for getEmojiStickers, masks-flag sets for getMaskStickers.
- Every stickerSet must carry `installed_date` and must not carry `archived`.
  - tdesktop rejects updateNewStickerSet without installed_date or with archived set (data/stickers/data_stickers.cpp:1797-1806). The same set objects come back in getAllStickers.
- `hash` must equal the client formula over each set's int32 stickerSet.hash in order (docs/tasks/024-research/semantics.md §2; Android calcStickersHash at pinned MediaDataController.java:3151).
- Answer allStickersNotModified when the incoming hash equals that value.
- Request sites:
  - Android: pinned MediaDataController.java:3041-3047
  - iOS: ManagedSynchronizeInstalledStickerPacksOperations.swift:483-487
  - Web K: invokeApiHashable, appStickersManager.ts:729-741
- An empty set list must still return allStickers with hash 0. Android handles the empty case (local MediaDataController.java:2593-2596).

### installStickerSet(stickerset, archived:Bool)

archived=false, the normal install:
- The set becomes installed at the top of its kind's order. Clients insert it at index 0:
  - iOS StickerSetInstallation.swift:236-240 (`collections.insert(... at: 0)`)
  - tdesktop newSetReceived, insertAtIndex 0, data_stickers.cpp:1807-1815
  - Android addNewStickerSet `add(0, set)` (local MediaDataController.java:2206; pinned :2225)
- If the set was archived, it is unarchived. tdesktop sends installStickerSet(false) to unarchive (apiwrap.cpp:1797-1812; boxes/stickers_box.cpp:943-953).
- Return messages.stickerSetInstallResultSuccess, or stickerSetInstallResultArchive{sets} listing the other sets that were auto-archived.
- What clients do with the archive result:
  - tdesktop applyArchivedResult (data_stickers.cpp:361-410) removes those sets from the installed order and shows the toast "lng_stickers_packs_archived".
  - Android processStickerSetInstallResultArchive (pinned MediaDataController.java:3647) shows StickersArchiveAlert.
  - iOS (StickerSetInstallation.swift:185-215) and its sync path (ManagedSynchronize…:239-258) accept all four StickerSetCovered constructors.
- Other sessions: updateNewStickerSet carrying the full messages.stickerSet, with installed_date set and not archived.
- Installing an already-installed set: UNVERIFIED. Recommendation: Success, move it to the top, no error.

archived=true:
- Marks an installed set archived and removes it from the installed order.
- The expected answer is Success. tdesktop archiveStickers fires `_setArchived` only on stickerSetInstallResultSuccess (boxes/sticker_set_box.cpp:2410-2420).
- iOS uses it for archiving a single set (ManagedSynchronize…:309-316).
- Other sessions: updateStickerSets with the kind's masks/emojis flag (docs). Archiving a set that is not installed: UNVERIFIED.

### uninstallStickerSet(stickerset) → Bool

- Removes the set from the installed list, and also from the archive: iOS removes an archived pack with uninstallStickerSet (TelegramEngine/Stickers/ArchivedStickerPacks.swift:58-66).
- Return boolTrue.
- Other sessions: updateStickerSets with the kind flag.
- Callers:
  - Android: pinned MediaDataController.java:3575; then loadStickers with the hash.
  - tdesktop: apiwrap.cpp:1751-1758
  - Web K: appStickersManager.ts:640-650, which treats a falsy result as failure.
- An unknown set or a wrong access_hash gives STICKERSET_INVALID (§3).
- A set the user does not have: UNVERIFIED. Recommendation: true, idempotent.

### toggleStickerSets(uninstall|archive|unarchive, stickersets) → Bool

- Applies the action to every set. The result is Bool by schema.
- Android checks the response for stickerSetInstallResultArchive (pinned MediaDataController.java:3622-3640). That branch can never fire with a Bool, so the server must still return Bool.
- Android's local semantics (local MediaDataController.java:3577-3610):
  - uninstall and archive remove the set from the installed list;
  - unarchive puts it at index 0 of the installed order.
- iOS uses flag 1<<0 (uninstall) and 1<<1 (archive) for more than one set (ManagedSynchronize…:279, 302).
- Web K does not call it: its toggleStickerSets loops install/uninstall (appStickersManager.ts:670-672).
- Other sessions: updateStickerSets for each affected kind. A batch may mix kinds, so send one update per kind.
- No flag set, or more than one flag set: UNVERIFIED. Recommendation: 400 with a generic error, or treat as a no-op. Record the choice.
- Unarchiving past a limit cannot return an archive result here (Bool), so the server must never auto-archive on toggle.

### reorderStickerSets(masks?, emojis?, order) → Bool

- Sets the full order of that kind. Neither flag means normal stickers.
- Clients send the whole list:
  - tdesktop apiwrap.cpp:1626-1645 (skips when there are fewer than 2 sets; on failure resets and refetches);
  - Android StickersActivity.java:611-618 and EmojiView (local :2378);
  - iOS ManagedSynchronize…:321-333;
  - Web K appStickersManager.ts:885-899, which also applies the order locally.
- Other sessions: updateStickerSetsOrder with the same flags and the stored full order.
- tdesktop refetches when the order has an unknown id, or its size differs from the local list (api/api_updates.cpp:2640-2687). The update must carry exactly the installed set of ids.
- Unknown or extra ids, or missing ids: UNVERIFIED. Recommendation:
  - ignore ids that are not installed of that kind;
  - append the missing installed ones at the end in their old order;
  - return true.

### getArchivedStickers(masks?, emojis?, offset_id, limit)

- Returns messages.archivedStickers{count, sets}.
- `count` is the total number of archived sets of that kind. Android reads only `count` with limit=0 (pinned MediaDataController.java:2602-2612), so limit=0 must return the count with an empty list.
- Paging: offset_id is the id of the last set the client already has, and the server returns the sets after it.
  - Android: `offset_id = last set id`, limit 15, stops when nothing new arrives (pinned ArchivedStickersActivity.java:213-216; local :226-236 dedups by id).
  - tdesktop: limit 10 on the first request and 30 per page (boxes/stickers_box.cpp:57-58, 978-991). offset_id is the last archived id in its order (:785-806). It stops on an empty page or when nothing changed (:521-577).
  - iOS: one request with offset 0 and limit 200 (ArchivedStickerPacks.swift:34-41).
  - Web K does not call it.
- Recommendation:
  - Stable order by archive time, newest first (UNVERIFIED for Telegram).
  - offset_id is exclusive.
  - If offset_id is not in the archive, start from the beginning (UNVERIFIED).
  - Clamp limit to roughly 0..100 (UNVERIFIED).
- Each element is StickerSetCovered with set.archived=true. All clients accept all four constructors.
  - Docs: stickerSetFullCovered for custom emoji.
  - For plain sets: stickerSetCovered (first document as cover) or MultiCovered.
  - stickerSetNoCovered is fine when the set has its own thumbs.

## 5. Updates to other devices: how clients react

updateStickerSets:
- Android always reloads normal stickers only and ignores masks/emojis (pinned MessagesController.java:20214-20216). An emoji-kind change is not picked up live on Android, a client limitation.
- tdesktop honours the flags (api_updates.cpp:2719-2731).
- iOS runs `.sync` (AccountStateManagementUtils.swift:1673-1674).
- Web K's handler is commented out (appStickersManager.ts:100+).

updateNewStickerSet:
- Android addNewStickerSet (pinned :20238-20240) skips sets it already knows.
- tdesktop (api_updates.cpp:2631-2638) logs an error on stickerSetNotModified. Always send the full stickerSet.
- Web K appStickersManager.ts:79-83.

updateStickerSetsOrder:
- Android pinned :20217-20227
- iOS :1653-1662
- Web K :93-98

updateMoveStickerSetToTop:
- Android :20270
- tdesktop :2689-2717
- iOS :1663-1672

None of these updates carries pts. How Telegram delivers them, as plain pushed updates with no recovery through getDifference, is UNVERIFIED.

## 6. Limits

- No appConfig key limits the number of installed sets. https://core.telegram.org/api/config lists stickers_faved_limit_default and stickers_faved_limit_premium, but those are for faved stickers.
  - Android parses them (pinned MessagesController.java:3486, 3497) with defaults 5 and 200 (:1642-1643).
- The docs give auto-archive only as "displacing some older unused stickersets" when there are "too many". The threshold is UNVERIFIED; it is commonly said to be about 200 installed sets.
- Recommendation for ≤100 users:
  - Return Success always and never auto-archive.
  - Or pick an explicit cap, such as 200 per kind, archive the least recently used sets, and return them in stickerSetInstallResultArchive.
  - Both are protocol-correct. This is a product choice to record in the brief.
- iOS asks for up to 200 archived sets in one page (ArchivedStickerPacks.swift:41).

## 7. Open points needing a live Telegram probe (all UNVERIFIED)

- 400 or 406 for STICKERSET_INVALID.
- Behaviour for an already-installed set and for a non-installed set on uninstall or archive.
- The auto-archive threshold.
- Archived list order, and offset_id when it is not found.
- toggleStickerSets with zero or several flags.
- reorderStickerSets with partial or unknown ids.

tdesktop raw files fetched for these checks are in (a local download) and the pinned Android files are in (a local download)
