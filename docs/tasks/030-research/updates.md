## Sticker-set sync updates: what Telegram sends and how the four clients handle them

**Bottom line.**
- Install: send `updateNewStickerSet` with the full set to the account's other sessions.
- Uninstall, archive, unarchive: send `updateStickerSets` with the right type flag.
- Reorder: send `updateStickerSetsOrder` with the type flag and the complete installed order.
- Send with `update_stickersets_order`: send `updateMoveStickerSetToTop`.
- None of these has a pts. Telegram's docs do not say whether they go in a seq container. The acting session does not need them: all four clients update their own state after the RPC.

### 1. What Telegram documents (core.telegram.org/api/stickers, fetched 2026-11-02)
- Install: the docs say updateNewStickerSet "will be emitted to the other logged-in sessions when installing stickersets" (api/stickers, "Installing stickersets").
- Uninstall or archive: `updateStickerSets` goes "to the other logged-in sessions". The client should then call:
  - `getMaskStickers` if `masks` is set;
  - `getEmojiStickers` if `emojis` is set;
  - otherwise `getAllStickers` and `getArchivedStickers`.
- Archive or unarchive (the "Archived stickers" section): `updateStickerSets` goes to the other sessions, and should trigger `getArchivedStickers`.
- Reorder: `updateStickerSetsOrder` goes "to the other logged-in sessions".
- Send with `update_stickersets_order` (`sendMessage`, `sendMedia`, `sendMultiMedia`): the server sends `updateMoveStickerSetToTop` "(instead of the usual updateStickerSetsOrder/updateStickerSets updates)" (api/stickers, "Recent stickersets").
- So the docs say "other sessions" every time. Nothing says the acting session gets them. They do not show up in the RPC result either: `installStickerSet` returns `messages.StickerSetInstallResult`; `uninstallStickerSet` and `reorderStickerSets` return `Bool`.
- **pts / seq.** None of the four constructors has a pts field (TL: `pkg/catalog/tl/api.tl:351-353, 425`). Whether Telegram puts them in an `updates` container with `seq != 0` or with `seq = 0` is not documented: **UNVERIFIED**.
  - api/updates says only that `updates`/`updatesCombined` carry seq, and that `seq = 0` is applied without a check.
  - All three native clients accept `seq = 0` containers and apply these updates through the general update path, not a pts check:
    - Android `MessagesController.java:17512` (`updates.seq == 0` → `processUpdate`);
    - iOS `UpdateMessageService.swift:47` (`seqRange: seq == 0 ? nil`);
    - tdesktop `api_updates.cpp:1478` (seq checked only if non-zero).

### 2. Android
Local checkout is 12.6.4; line numbers are from it and were not re-checked against 12.9.1 (aac1efcb): **UNVERIFIED**. Dispatch is in `MessagesController.processUpdateArray`, the main-thread branch (`MessagesController.java:18866`, then `19388-19446`).
- **updateStickerSets** (`19388-19390`): `loadStickers(TYPE_IMAGE, false, true)` with `force=true`, so it sends `getAllStickers` with hash 0 (`MediaDataController.java:3012-3013`).
  - It **ignores the masks/emojis flags**: an emoji or mask change from another device is not picked up from this update.
  - It does not call `getArchivedStickers`.
- **updateStickerSetsOrder** (`19391-19401`): picks the type from the flags and calls `reorderStickers(type, order, false)` (`MediaDataController.java:2168-2181`). This is a local sort by index in `order` plus a hash recount, with no server call (`//loadStickers` is commented out at 2180).
  - A set missing from `order` gets index -1 and sorts first. A set in `order` that the device lacks is ignored.
- **updateNewStickerSet** (`19412-19414`): calls `addNewStickerSet(set)` (`MediaDataController.java:2196-2236`).
  - It **returns early if the set id is already in `stickerSetsById`, or its short name is in `stickerSetsByName`**. Those maps also hold sets cached from `getStickerSet` (`storeTempStickerSet` 2188-2191, called at 2537; the puts at 1363-1425). So a set the device only previewed is silently not installed.
  - Otherwise it uses `set.set.masks` / `set.set.emojis` to pick the type, inserts the set at index 0, indexes the packs, then calls `loadStickers(type, false, true)`, a forced reload.
  - So the full `messages.stickerSet` must carry correct masks/emojis flags.
- **updateMoveStickerSetToTop** (`19444-19446`): `moveStickerSetToTop` (`MediaDataController.java:8468-8487`) moves the set to the top locally. If the set is unknown it does nothing.
- **Acting session.** It does not need the updates:
  - install calls `loadStickers(type,false,false,true)` after the response (`MediaDataController.java:3522`);
  - uninstall forces a reload (`3534`);
  - `toggleStickerSets` changes the list locally, then reloads (`3545-3599`);
  - reorder only sends the RPC and recomputes the hash locally (`ui/StickersActivity.java:609-618`, `ui/Components/EmojiView.java:2378-2385`).
  - An echo would be harmless: `updateNewStickerSet` would hit the early return; a same-order reorder does nothing.

### 3. iOS
File: `submodules/TelegramCore/Sources/State/AccountStateManagementUtils.swift`.
- Each update is turned into an install operation in the general update switch:
  - `updateNewStickerSet` → `.add` (1651-1652);
  - `updateStickerSetsOrder` → `.reorder(namespace, order)`, namespace taken from flag bit 0 (masks) or bit 1 (emoji) (1653-1662);
  - `updateMoveStickerSetToTop` → `.reorderToTop` (1663-1672);
  - `updateStickerSets` → `.sync` (1673-1674), with the **flags ignored**.
- The operations are applied at 5630-5800:
  - **`.sync`** in a batch: a sync operation on **all three** namespaces (5631-5640, 5789-5793). The sync sends `getAllStickers`, `getMaskStickers` and `getEmojiStickers` with the local hash (`ManagedSynchronizeInstalledStickerPacksOperations.swift:483-487`).
  - **`.add`** (5647-5731): builds the items from documents, packs and keywords. The namespace comes from the set's flags: bit 3 masks → mask packs, bit 7 emojis → emoji packs (5698-5705). An existing set is moved to the top; a new one is inserted at the top with its items (5717-5726). This is not idempotent with respect to order: replaying it after a later reorder moves the set back to the top.
  - **`.reorder`** (5732-5761): if the set of ids differs from the local set, it marks that namespace for sync (a full reload); otherwise it reorders locally. **The order must list exactly the installed, non-archived sets of that type.**
  - **`.reorderToTop`** (5762-5788): reorders locally; unknown ids are skipped.
- **Acting session**: it works local-first through `ManagedSynchronizeInstalledStickerPacksOperations` (calls at lines 234, 279, 287, 302, 310, 332) and then reconciles by hash. It does not rely on updates.

### 4. tdesktop v7.0.9
Sources: raw `Telegram/SourceFiles/api/api_updates.cpp` and `data/stickers/data_stickers.cpp`. These updates are handled in `Updates::feedUpdate`, outside the pts path.
- **updateNewStickerSet** (`api_updates.cpp:2631-2638`) → `Stickers::newSetReceived` (`data_stickers.cpp:1797-1821`).
  - **It rejects, with "API Error", a set without `installed_date` or with `archived`.**
  - It picks the order list by `is_emojis()` / `is_masks()`, moves or inserts the set at index 0, then calls `feedSetFull(set)`. There is no server reload.
- **updateStickerSetsOrder** (`2640-2687`):
  - It takes ids from `order` until the first one it does not know.
  - If the result size differs from the local order size of that type, or from `order.size()`, it resets that type's last update time and reloads (`updateStickers`, `updateMasks` or `updateCustomEmoji`).
  - Otherwise it replaces the order locally.
  - Again, **the order must equal the full installed list of that type**.
- **updateMoveStickerSetToTop** (`2689-2717`):
  - It **handles only `emojis` versus everything else**: the `masks` flag is ignored, and a mask set id is looked up in the sticker order.
  - If the id is not found, it reloads that type; otherwise it rotates the set to the top locally.
- **updateStickerSets** (`2719-2731`): it honours the flags (emojis → `updateCustomEmoji`, masks → `updateMasks`, otherwise `updateStickers`), with the last update time reset to 0, which forces a reload.
- **Acting session**: `Stickers::installLocally` / `undoInstallLocally` (`data_stickers.cpp:418, 483`) update local state. That they cover every case was not traced: **UNVERIFIED**.

### 5. tweb (Web K, `~/pluma-web/upstream` @ 346844b)
Note: this tree contains comments that read like local patches (e.g. `stickers.ts:373-375`), so it may not be pure upstream.
- Handlers are in `src/lib/appManagers/appStickersManager.ts:78-115`:
  - **updateNewStickerSet** (79-83): `saveStickerSet` into the cache, then dispatches `stickers_installed`. The panel adds a category only if it does not exist yet (`components/emoticonsDropdown/tabs/stickers.ts:371-378`), so an echo is harmless. No server reload.
  - **updateMoveStickerSetToTop** (89-91): dispatches `stickers_top` (local reposition, `stickers.ts:412-420`). The flags are ignored.
  - **updateStickerSetsOrder** (93-98): dispatches `stickers_order` with the type from the flags. The panel applies it only to `type === 'stickers'` (`stickers.ts:422-433`), and the settings tab applies it too (`components/sidebarLeft/tabs/stickersAndEmoji.tsx:228`).
  - **updateStickerSets is commented out (100-114), so it is ignored.** An uninstall or archive done on another device does not reach a running Web K session until its stickers are reloaded.
- **Acting session**:
  - `toggleStickerSet` (633-665) applies the change locally and dispatches `stickers_deleted` / `stickers_installed`;
  - `reorderStickerSets` (885-899) calls `processLocalUpdate({_: 'updateStickerSetsOrder', ...})` itself after the RPC, so Web K needs no echo.

### 6. What this means for the server

**What to send for each action**
- `installStickerSet` with `archived=false`: `updateNewStickerSet` carrying the full `messages.stickerSet`, with:
  - `installed_date` set (tdesktop drops it otherwise);
  - `archived` false;
  - correct `masks` / `emojis` flags on the set (Android, iOS and tdesktop pick the list from them).
- When the install archived other sets (`stickerSetInstallResultArchive`): Telegram's exact behaviour is **UNVERIFIED**. Sending an additional `updateStickerSets` for that type is consistent with the docs ("when … archiving stickersets").
- `installStickerSet` with `archived=true`, `uninstallStickerSet`, and `toggleStickerSets` with uninstall/archive/unarchive: `updateStickerSets` with that type's flag.
  - Android reloads only regular stickers, and Web K ignores this update. These are client limits that also apply against Telegram.
- `reorderStickerSets`: `updateStickerSetsOrder` with the flags and **the complete order of installed, non-archived sets of that type**. A partial order makes iOS and tdesktop do a full reload, and makes Android sort the missing sets to the front.
- `sendMessage`/`sendMedia` with `update_stickersets_order`: `updateMoveStickerSetToTop` with the type flags. This also applies the move to the server-side order, so later order hashes match.

**Recipients**
- "Other logged-in sessions" per the docs. The acting session does not need the update with any client.
- An echo would be harmless in Android, Web K and tdesktop. In iOS an `.add` echo moves the set to the top, which is its position after an install anyway.
- ADR 0022's `except_auth_key_id` / `sync.updatesNotMe` path fits this.

**seq (our choice; the Telegram reference is UNVERIFIED)**
- `updateStickerSets` and `updateStickerSetsOrder` are idempotent state updates, so they suit the seq journal of ADR 0020.
- `updateNewStickerSet` is not order-idempotent in iOS and tdesktop, both of which move the set to the top on replay. If it is journalled, the difference fold must drop it when a later `updateStickerSetsOrder`, `updateStickerSets` or `updateMoveStickerSetToTop` of the same type comes after it.
  - It is also large (it holds all documents) and stored as JSON.
  - An alternative is to send it with `seq = 0` (lossy for offline devices), relying on the clients' periodic hash reload: Android forces a reload after it anyway.
- This is a design decision to record in the brief, not a verified Telegram behaviour.

**Hashes**
- The order the server stores must equal the order it then returns from `getAllStickers`, `getMaskStickers` and `getEmojiStickers`. Android recomputes `loadHash` locally after reorder and move-to-top (`MediaDataController.java:2178, 2209`), and iOS compares local and remote hashes.
- See `docs/tasks/024-research/semantics.md` §2.

**Existing server plumbing**
- `sync.updatesNotMe` (`app/messenger/sync/internal/core/sync.updatesNotMe_handler.go:19-35`).
- The seq journal with an excluded authorization key (ADR 0020, ADR 0022, `app/service/biz/updates/internal/dao/seq.go`).
- Updates without pts and seq are not recoverable (`app/messenger/sync/internal/dao/pusher.go:479-497`).

**UNVERIFIED in this answer**
- Whether Telegram uses seq or `seq = 0` for these updates.
- Whether Telegram echoes them to the acting session (the docs say "other sessions").
- What Telegram sends on auto-archive during install.
- Android line numbers at the pinned 12.9.1.
- Full coverage of tdesktop's local install path.
