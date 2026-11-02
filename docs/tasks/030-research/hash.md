## Answer: how clients compute the installed-sets hash and what the server must answer

### 0. Schema and docs
- TL (pkg/catalog/tl/api.tl):
  - `messages.getAllStickers#b8a0a1a8 hash:long` (:2495), `getMaskStickers#640f82b8 hash:long` (:2529) and `getEmojiStickers#fbfca18f hash:long` (:2629) all return `messages.AllStickers`.
  - `messages.allStickers#cdbbcebb hash:long sets:Vector<StickerSet>` (:658) and `allStickersNotModified#e86602c3` (:657).
  - `stickerSet#2dd14edc ... installed_date:flags.0?int ... count:int hash:int` (:701). The per-set `hash` is an **int32**.
  - `messages.getArchivedStickers#57f17692 flags masks emojis offset_id:long limit:int` (:2528) has **no hash parameter**. It is paged by offset_id/limit, so no NotModified exists for it. The answer is `messages.archivedStickers count sets:Vector<StickerSetCovered>` (:893).
- Hash algorithm: https://core.telegram.org/api/offsets ("Hash generation") gives `h ^= h>>>21; h ^= h<<35; h ^= h>>>4; h += id`, starting from 0, unsigned 64-bit. The page also says "if the result container already has a hash field, that can be used instead".
- https://core.telegram.org/api/stickers says getAllStickers fetches "all installed and non-archived stickersets", and getEmojiStickers fetches "all installed and non-archived custom emoji stickersets".

### 1. The hash each client sends

The input is the same on all four clients: the int32 `stickerSet.hash` of each installed, non-archived set, **sign-extended to 64 bits**, folded in list order. The order is the user's order, i.e. the order the server returned (a newly installed set at index 0).

**Android** (local 12.6.4 ~/aaaaaaa/Telegram-Android .../messenger/MediaDataController.java; line numbers below are from pinned aac1efcb, fetched raw):
- `calcHash(long hash, long id)` (:2463) is the standard step.
- `calcStickersHash(ArrayList<TL_messages_stickerSet>)` (:3151–3164):
  - skips null entries and `set.archived` sets;
  - otherwise does `acc = calcHash(acc, set.hash)`. Because `set.hash` is an `int`, the int-to-long widening sign-extends.
- What is sent:
  - `getAllStickers` / `getEmojiStickers` / `getMaskStickers` send `force ? 0 : loadHash[type]` (:3042, :3045, :3048).
  - After a full answer, `loadHash` is the **server's** `allStickers.hash` (`res.hash2`, :2625, :2640, :2664 into `processLoadedStickers`, which sets `loadHash[type] = hash`).
  - After a cold start from the DB cache, and after every local change (toggle/reorder; local :3374, :3404, :3463, :3488, :3573), it is the **locally recomputed** `calcStickersHash`. In the cache path the local file recomputes at :2959, rather than using the stored hash.
  - So the server hash must equal calcStickersHash over the list it returned. If not, a NotModified chance is lost after each restart, but nothing breaks.
- Behaviour after install/uninstall:
  - Install/archive goes through `installStickerSet` and then `loadStickers(type,false,false,true)`, with the hash (:3555–3562).
  - Uninstall calls `loadStickers(type,false,true,false)`, i.e. `force`, so hash=0 (:3579).
  - A local archive removes the set from `stickerSets[type]`. Unarchive/install moves an already-known set to index 0 (local :3354–3374; pinned :3391–3401).
- On a full answer, Android re-fetches a set with `getStickerSet` only when `oldSet.set.hash != stickerSet.hash` (pinned :2632). So `stickerSet.hash` must change when the set's content changes and stay stable otherwise.

**tdesktop v7.0.9** (raw GitHub):
- `Api::HashUpdate` (api_hash.h:40–45). The `int32` overload goes via `int64` and sign-extends (api_hash.h:55–57). `StickersSet::hash` is a `uint64` filled from `data.vhash().v` (int32, sign-extended) (data_stickers_set.h:110; data_stickers.cpp:1596, :1624).
- `CountStickersOrderHash` (api_hash.cpp:38–61) walks `setsOrder` / `maskSetsOrder` / `emojiSetsOrder`:
  - It skips sets with `Flag::Special` or `Flag::Archived`.
  - It does `HashUpdate(result, set->hash)`.
  - If the legacy `DefaultSetId` placeholder is present and `checkOutdatedInfo` is true, it returns **0**.
- Requests: `GetAllStickers(CountStickersHash(_session,true))`, `GetMaskStickers(CountMasksHash(...))`, `GetEmojiStickers(CountCustomEmojiHash(...))` (apiwrap.cpp:3163–3164, 3187–3188, 3211–3212). NotModified is a no-op (apiwrap.cpp:3156).
- After a full answer it recounts and logs "API Error: received … hash … while counted hash is …" on a mismatch (data_stickers.cpp:778–792). The server hash must therefore equal this formula.
- Local install puts the set at index 0 of the order (`installLocally`, data_stickers.cpp:418–445).

**iOS** (~/aaaaaaa/Telegram-iOS, TelegramCore):
- `hashForStickerPackInfos` (State/ManagedSynchronizeInstalledStickerPacksOperations.swift:129–137) does `combineInt64Hash(&acc, with: UInt64(bitPattern: Int64(info.hash)))`. That is sign-extended Int32 (`hash: Int32` at SyncCore/SyncCore_StickerPackCollectionInfo.swift:53) over **all** local infos of the namespace, in local order.
- `combineInt64Hash` is the standard step (State/HistoryViewStateValidation.swift:450–455).
- The hash goes to getAllStickers/getMaskStickers/getEmojiStickers (same file :478–488). iOS never uses the server's `allStickers.hash`: it always recomputes locally.
- `allStickersNotModified` means remote = local (:518–519).
- iOS has **no archived filter**. `StickerPackCollectionInfo(apiSet:)` ignores both `installed_date` (discarded as `_`) and the archived flag bit 1 (TelegramEngine/Stickers/StickerPack.swift:49–83). Every set returned by getAllStickers is stored as installed and hashed. Archived sets are kept out of the local namespace by the archive operation (Managed…Operations.swift:645–700).

**Web K** (~/pluma-web/upstream @346844b):
- `getAllStickers` / `getEmojiStickers` use `invokeApiHashable` (src/lib/appManagers/appStickersManager.ts:729–741). Web K never calls getMaskStickers or getArchivedStickers.
- `invokeApiHashable` (src/lib/appManagers/apiManagerMethods.ts:99–156) **echoes the server's `hash`** from the last answer (:116–124, :136–144). It does not compute anything itself.
- On `*NotModified` it returns `cached.result` (:131–133). If nothing is cached, `cached` is undefined and this throws.
- It caches only when `result.hash` is truthy (:136), so after an answer with hash 0 it sends no cached hash next time. That this ends up serialized as hash 0 is UNVERIFIED (the serializer was not read).

### 2. What the server must do
- **Contents:** compute `H = fold(standard step, uint64(int64(set.hash)))` over the user's installed, **non-archived** sets of that type (stickers / masks / emojis kept separate), in the user's order. Put H in `messages.allStickers.hash`. All four clients agree on this only if archived sets are absent from the list:
  - Android and tdesktop skip archived sets in the hash;
  - iOS would hash them and show them as installed.
- **NotModified:**
  - Answer `allStickersNotModified` only when `req.hash != 0 && req.hash == H`.
  - When `req.hash == 0`, always send the full `allStickers` (with `hash:0, sets:[]` when the list is empty).
  - Reason for Android: a NotModified on a request that sent hash 0 makes it re-request every second, endlessly (`!cache && res == null && hash == 0` → `loadStickers` after 1000 ms, pinned MDC.java:3180–3186). An empty local list gives calcStickersHash = 0, and an empty server list also gives H = 0, so this case does happen.
  - Reason for Web K: a NotModified with nothing cached throws (apiManagerMethods.ts:131–133).
  - The rule also covers tdesktop's "outdated → 0" case (api_hash.cpp:58–60).
- **Order:** list order equals the user's order. A newly installed or unarchived set goes to the front, which is what Android (pinned :3401) and tdesktop (data_stickers.cpp:439–445) do locally, so the local hash matches the server's.
- **Per-set `stickerSet.hash`:**
  - It must be identical in getAllStickers and in `messages.getStickerSet` for the same set.
  - It must change when the documents, packs, title or thumbnail change. Android and tdesktop decide on re-fetching by comparing it (Android pinned :2632; tdesktop data_stickers.cpp:1619–1626).
  - The value Telegram itself uses is UNVERIFIED.

### 3. installed_date and the archived flag in getAllStickers
- **getAllStickers does not list archived sets.** Docs: "all installed and non-archived". Every set in it should have flags.1 (archived) **unset**.
  - tdesktop would put an archived set into setsOrder only if it is also official (data_stickers.cpp:723–725), and would leave it out of the hash.
  - Android keeps archived sets in the list but leaves them out of the hash and of the emoji index (MDC.java local :3190).
  - iOS ignores the flag.
- **installed_date (flags.0) must be present and nonzero** on every set of getAllStickers / getEmojiStickers / getMaskStickers:
  - tdesktop sets `Flag::Installed` only when `vinstalled_date()` is present (data_stickers_set.cpp:55). In `somethingReceived` it clears Installed on its non-archived sets of that type, re-sets it from flags, and erases non-installed, non-special, non-archived, non-emoji sets from its map (data_stickers.cpp:713–756). A present 0 becomes `unixtime::now()` (data_stickers.cpp:1615–1618). `installDate` also drives the sort order of the sticker search results (data_stickers.cpp:1256–1264, 1416–1439).
  - Web K treats a set as installed when `installed_date` is truthy, so 0 counts as not installed (appStickersManager.ts:452, 494, 639–658; components/popups/stickers.tsx:90–92).
  - Android ignores `installed_date`. It reads it at TLRPC.java:46582–46584 and uses it nowhere else (grep).
  - iOS discards it (StickerPack.swift:52).
  - The value should be the install time (unixtime).
- **Archived sets** are served by getArchivedStickers as StickerSetCovered with the archived flag set:
  - tdesktop moves a set into `archivedSetsOrder` when the Archived flag changes (data_stickers.cpp:1631–1645).
  - Android requests `limit=0` only for the count (MDC.java local :2573–2577).
  - Whether Telegram also sets installed_date on archived sets is UNVERIFIED. tdesktop's Installed flag for them comes from installed_date presence (data_stickers_set.cpp:55).

The raw tdesktop and pinned Android files used for line checks are in (a local download) and (a local download)
