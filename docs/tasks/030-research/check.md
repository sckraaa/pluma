## Spot-checks: 5 load-bearing claims, all confirmed

1. **TL shapes (api.tl).** CONFIRMED. I read ~/teamgram-server/pkg/catalog/tl/api.tl at lines 351-353, 425, 657-658, 701, 893-901, 2495, 2501-2502, 2508, 2528-2529, 2575 and 2629. Every constructor ID and line number matches.
   - `toggleStickerSets` returns `Bool` (:2575). `installStickerSet` returns `messages.StickerSetInstallResult` (:2501).
   - `stickerSet` has `installed_date:flags.0?int`, `archived:flags.1`, `masks:flags.3` and `emojis:flags.7` (:701).
   - `getArchivedStickers` has no hash parameter (:2528).

2. **Android hash, and the retry loop on NotModified with hash 0.** CONFIRMED in the pinned file (scratchpad/an/MDC.java, which is byte-identical to MediaDataController.java).
   - `calcStickersHash` (:3151-3164) skips null and `set.archived`, and folds `set.hash` (an int, so it is sign-extended).
   - The request sends `force ? 0 : loadHash[type]` (:3042/3045/3048).
   - Any response that is not `TL_messages_allStickers` goes to `processLoadedStickers(type, null, ...)` (:3057-3061). That means `NotModified` and also any RPC error.
   - With `!cache && res == null && hash == 0`, it calls `loadStickers` again after 1000 ms (:3180-3186).
   - **The reports missed one point.** The loop also fires on an *error*, not only on NotModified. So getAllStickers, getMaskStickers and getEmojiStickers must never return an error for a valid user, including transient storage failures. If they do, Android retries every second.
   - Also confirmed: `loadStickers(type,false,true)` (3-arg overload, :2932) passes its third argument as `force`. So the `updateStickerSets` handler forces hash 0, as the updates report says.

3. **Android ignores the flags on `updateStickerSets`.** CONFIRMED. Pinned MessagesController.java:20214-20216 always calls `loadStickers(TYPE_IMAGE, false, true)`. `updateStickerSetsOrder` maps masks/emojis to a type (:20217-20227). `updateNewStickerSet` calls `addNewStickerSet` (:20238-20240).

4. **tdesktop rejects `updateNewStickerSet` without `installed_date` or with `archived`, and needs an exact order in `updateStickerSetsOrder`.** CONFIRMED.
   - In data_stickers.cpp:1797-1821, `newSetReceived` logs "API Error" and returns in both cases. It then picks the order list by `is_emojis`/`is_masks`, moves the set to index 0, and calls `feedSetFull`.
   - In api_updates.cpp:2640-2687, it takes ids from `order` until the first id it does not know.
     - If that count differs from the local order size of the type, or from `order.size()`, it resets and reloads that type. Otherwise it replaces the order.
     - The lookup is in the global `sets()` map, not the per-type order. So an id the device knows from a preview is accepted, and only the size check catches it.

5. **iOS ignores the flags of `updateStickerSets` and `installed_date`, and compares order as a set; Web K throws on NotModified with nothing cached.** CONFIRMED.
   - In AccountStateManagementUtils.swift:1651-1674, `updateNewStickerSet` becomes `.add`. Reorder and move-to-top take their namespace from bit 0 / bit 1. `updateStickerSets` becomes `.sync` with no flags.
   - In :5732-5761, `.reorder` compares `Set(currentInfos ids) != Set(ids)`. If they differ, it syncs that namespace; otherwise it reorders locally (it force-unwraps `currentDict[...]!`, which is safe only because the sets are equal).
   - StickerPack.swift:49-52 destructures `installedDate` without using it (the reports call it `_`; the name is kept but the value is unused, so the claim stands).
   - Web K apiManagerMethods.ts:116-144:
     - it reuses the cached hash only when a cache entry exists;
     - on `*NotModified` it returns `cached.result`, which throws when `cached` is undefined;
     - it caches only when `result.hash` is truthy.

Side check: the 400/406 constants exist at third_party/teamgram-proto/mtproto/rpc_error_codes.go:1155-1156 and :2363-2364. Our getStickerSet returns the 400 one (messages.getStickerSet_handler.go:60, 69). Both CONFIRMED.

## Contradictions, gaps and uncited claims across the three reports

- **No real contradictions.** The reports cite Android line numbers in different numbering spaces:
  - the methods report uses pinned lines (MessagesController :20214);
  - the updates report uses local 12.6.4 lines (:19388) and marks them UNVERIFIED for the pinned release.
  - The pinned numbers checked above should be canonical in the brief.
- **Error responses on getAllStickers (gap in all three reports).** Android's 1-second retry also fires on errors (point 2). The reports frame the hash-0 rule only around NotModified.
- **`updateNewStickerSet` on Android is a silent no-op for a set the device knows only from a preview.** Only the updates report covers this (`addNewStickerSet` returns early if the set is in `stickerSetsById`/`stickerSetsByName`). That device will not show the install until its next hash-based reload. It is a client limitation and not fixable on the server, but worth recording.
- **Echo to the acting session.** The methods report says nothing about the actor. The updates report says the actor does not need the update. The docs say "other sessions". These agree; the implementation should use the `except_auth_key_id` / `sync.updatesNotMe` path.
- **Uncited or weak claims:**
  - "commonly said to be about 200 installed sets" (methods §6) has no source. It is correctly marked UNVERIFIED and must not drive code.
  - "Most other iOS calls catch and ignore errors" (methods §3) is a generalisation from two call sites.
  - Web K's tree may carry local patches (the updates report says so itself). Web K findings (for example, `updateStickerSets` commented out at appStickersManager.ts:100-114) should be re-checked against upstream tweb at 346844bf before anything relies on them.
  - The hash report flags as UNVERIFIED whether Web K serializes the hash as 0 when nothing is cached.
- **Per-set `stickerSet.hash` stability.**
  - The hash report requires it to be identical in getAllStickers and getStickerSet, and to change on content change. Android (pinned MDC :2632) and tdesktop re-fetch when it differs.
  - The methods report does not mention this. It touches the existing catalog code (task 030, sets fetched from Telegram on demand). The implementer must check where the catalog's per-set hash comes from and that it is stable across re-fetches.
- **Archived sets.**
  - Both reports agree: never list archived sets in getAll*, and use the archived flag in getArchivedStickers.
  - Whether `installed_date` should be present on archived sets is UNVERIFIED. tdesktop's Installed flag for them depends on whether `installed_date` is present (data_stickers_set.cpp:55, not re-read by me).

## Open questions the implementer must decide (record them in the brief)

1. **Error code for an unknown set or a wrong access_hash.**
   - 400 is what our getStickerSet already returns. 406 is what the docs list.
   - With 406 and no `updateServiceNotification`, Android shows nothing.
   - Pick one code for the whole sticker area. A live probe would settle it.
2. **Idempotency.** What happens on:
   - installing an already-installed set (recommended: Success and move it to the top);
   - uninstalling or archiving a set that is not installed (recommended: true / Success, no error).
3. **Auto-archive policy.**
   - Option A: never auto-archive, so install always returns Success.
   - Option B: an explicit cap per kind, archiving least-recently-used sets and returning `stickerSetInstallResultArchive`. If chosen, it also needs:
     - which sets are evicted;
     - an additional `updateStickerSets` to other sessions;
     - that toggle/unarchive never auto-archives, since it returns Bool.
4. **toggleStickerSets with zero or more than one flag.** Error or no-op? Also, a batch that mixes kinds sends one `updateStickerSets` per affected kind.
5. **reorderStickerSets with unknown, missing or duplicate ids.**
   - Normalise to exactly the installed, non-archived ids of that kind: drop unknown ids and append missing ones in their old order.
   - Broadcast the stored full order. iOS and tdesktop do a full reload on any mismatch, and Android sorts missing sets first.
6. **getArchivedStickers paging.**
   - Order: archive time, newest first?
   - offset_id exclusive; and when offset_id is not in the archive, restart from the beginning or return empty?
   - limit clamp, given that iOS asks for 200 and limit=0 must return the count only.
   - Which StickerSetCovered constructor per kind: FullCovered for emoji per the docs, Covered or MultiCovered for plain and mask sets.
7. **Delivery of the updates (none carries pts).**
   - Option 1: journal them in the seq journal (ADR 0020/0022).
     - `updateNewStickerSet` is not order-idempotent on iOS and tdesktop: replaying it moves the set to the top. The difference fold would have to drop it when a later order or sets update of the same kind follows.
     - It is also large, since it carries all documents.
   - Option 2: push with seq=0 and accept loss for offline devices, which recover through the hash on next load.
   - Possibly a split: journal `updateStickerSets` and `updateStickerSetsOrder`, push `updateNewStickerSet` with seq=0.
   - This is not verified against Telegram, so it needs an ADR-level decision.
8. **getAll\*/NotModified contract.**
   - The server hash H is folded over sign-extended int32 set hashes in the user's order.
   - Answer NotModified only when `req.hash != 0 && req.hash == H`.
   - An empty list returns `allStickers{hash:0, sets:[]}`.
   - Every set carries `installed_date` (non-zero, the install time) and no archived flag.
   - Internal failures must not surface as errors to Android in a tight loop: return a real error once, and rely on the 1 s retry being acceptable, or degrade some other way. Decide explicitly.
9. **Scope boundary.** `updateMoveStickerSetToTop`, produced by sending with `update_stickersets_order`, belongs to the send path. In or out of this task? If out, the server-side order will drift from clients that move a set to the top locally after sending. The order hashes then mismatch, which costs one extra reload but breaks nothing.
10. **Storage.** How order and kind are stored per user:
    - per-kind order as a position column, or an ordered list;
    - installed_date and archived_date per (user, set);
    - transactional reorder.
    - The migration goes through `teamgramd/deploy/sql/migrate-*.sql`.

Files read for the checks:
- ~/teamgram-server/pkg/catalog/tl/api.tl
- (a local download)
- (a local download)
- (a local download)
- (a local download)
- ~/aaaaaaa/Telegram-iOS/submodules/TelegramCore/Sources/State/AccountStateManagementUtils.swift
- ~/aaaaaaa/Telegram-iOS/submodules/TelegramCore/Sources/TelegramEngine/Stickers/StickerPack.swift
- ~/pluma-web/upstream/src/lib/appManagers/apiManagerMethods.ts
- ~/teamgram-server/third_party/teamgram-proto/mtproto/rpc_error_codes.go
- ~/teamgram-server/app/bff/catalog/internal/core/messages.getStickerSet_handler.go
