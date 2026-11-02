## Sources read
- Local tweb checkout `~/pluma-web/upstream` (commit `346844bf`, layer 228, build 676, per `~/pluma-web/upstream.env:4-9`). Paths below are relative to `~/pluma-web/upstream/src/`.
- tweb master schema, fetched raw from GitHub: `"layer":229`, `public/version` = `2.2 (684)`. The live web.telegram.org/k probably runs this or newer. **I did not check it in the browser.**
- `teamgram-proto` CRC tables: `~/teamgram-server/third_party/teamgram-proto/mtproto/schema.tl.crc32.pb.go`.
- Docs fetched: https://core.telegram.org/api/files, https://developer.chrome.com/blog/local-network-access, https://developer.chrome.com/blog/pna-on-hold, the WICG LNA explainer (https://github.com/WICG/local-network-access/blob/main/explainer.md), MDN `showDirectoryPicker`.
- Response headers of `https://web.telegram.org/k/`: there is **no CSP header and no CSP meta tag**, so `connect-src` does not block loopback today.
- Local Chrome is 156.0.8078.79.

## 1. invokeApi from the page console
**How to reach it**
- `MOUNT_CLASS_TO` is always `window`: `config/debug.ts:16` (`DEBUG || true`).
- So `window.rootScope` exists in production (`lib/rootScope.ts:337`).
- `rootScope.managers` is set in `lib/appDialogsManager.ts:934`. It is a Proxy: every `managers.X.method(...args)` becomes `apiManagerProxy.invoke('manager', {name, method, args, accountNumber})` (`lib/getProxiedManagers.ts:73-105`).
- That call is a postMessage to the managers worker. Arguments and results are therefore **structured-cloned**: plain objects and `Uint8Array` survive, functions do not.
- `apiManager` and `apiFileManager` are both registered managers (`lib/appManagers/createManagers.ts:99,102`).
- Call form: `await rootScope.managers.apiManager.invokeApi('messages.getStickerSet', {stickerset:{_:'inputStickerSetAnimatedEmoji'}, hash:0}, {noErrorBox:true})`

**Signature and built-in retries.** `invokeApi(method, params, options)` is at `lib/appManagers/apiManager.ts:604`, with options typed in `types.d.ts:10-33`. It handles these errors itself:
- `401` on a non-base DC: runs `auth.exportAuthorization` / `auth.importAuthorization` automatically (`apiManager.ts:707-721`).
- `303 *_MIGRATE_X` (`:722-732`).
- `FILE_MIGRATE_X`: switches `options.dcId` and retries (`:733-740`).
- `420 FLOOD_WAIT_X`: sleeps X seconds and retries if X ≤ `floodMaxTimeout` (default 60), otherwise throws (`:744-776`). `rawError` disables this.
- `500`: exponential backoff up to 60 s (`:785-795`).

**Hazard: automatic logout**
- If a 401 reaches `rejectPromise` and `noErrorBox` is not set, tweb calls `this.logOut()` after 100 ms (`apiManager.ts:655-663`).
- `SESSION_REVOKED` and `AUTH_KEY_DUPLICATED` always log out (`:640-644`).
- **Every collector call must pass `{noErrorBox:true}`.** It must also stop at once on any 401 or 406.

**Result object shape** (`lib/mtproto/tl_utils.ts`)
- The predicate is under the `'_'` key, e.g. `{_:'document', ...}` (`:741`). Field names are the TL snake_case names, e.g. `access_hash`, `file_reference`, `dc_id`.
- **long** values are signed. A JS number is returned if |v| ≤ 2^53−1; otherwise a decimal **string** (`fetchLong`, `:481-498`). The same field can be a number in one object and a string in another. Inputs accept either a number or a string (`storeLong`, `:145-155`).
- **bytes** are `Uint8Array` (`fetchBytes`, `:548-567`, `slice` at `:25`). `JSON.stringify` turns a Uint8Array into `{"0":..}`, so the collector must base64-encode them with a replacer.
- **flags**: there is no `flags` field. Flags of type `true` go into `obj.pFlags[name]`; other optional fields are present or absent (`:746-776`).
- On input, `true` flags are read from `obj.pFlags` if present, otherwise from `obj` itself. The flags word is computed from which fields are defined (`storeBody`, `:360-404`).
- `JSONValue` is converted to plain JS (`:780+`).

**Layer**
- The checkout is layer 228 (`schema.ts` ends with `"layer":228`); it is sent via `invokeWithLayer` with `Schema.layer` (`lib/mtproto/networker.ts:412-415`).
- I ran a script over 60 relevant constructors and methods: `document`, `stickerSet`, `messages.stickerSet`, `stickerSetCovered` / `FullCovered` / `MultiCovered` / `NoCovered`, `photoSize` / `photoCachedSize` / `photoStrippedSize` / `photoPathSize` / `photoSizeProgressive`, `videoSize*`, `availableReaction`, `messages.availableReactions`, `availableEffect`, `messages.availableEffects`, `emojiKeyword*`, `emojiKeywordsDifference`, `help.peerColorSet` / `ProfileSet` / `Option` / `help.peerColors`, `theme`, `themeSettings`, `wallPaper*`, `account.themes` / `wallPapers` / `emojiStatuses`, `emojiStatus*`, `emojiList`, `emojiGroup*`, `messages.emojiGroups`, `timezone`, `help.timezonesList`, the `documentAttribute*` set, `inputStickerSetThumb`, `inputDocumentFileLocation`, `upload.getFile`, `upload.file`.
- Result: they are **identical in id and params between tweb 228 and tweb master 229**.
- Their CRCs also match `teamgram-proto`. Examples: `document_8fd4c4d8` (`schema.tl.crc32.pb.go:752`), `stickerSet_2dd14edc` (`:889`), `messages_stickerSet_6e153f16` (`:891`), `availableReaction` c077ec01 (`:1604`), `availableEffect` 93c3e27e (`:1911`), `help_peerColorSet` 26219a58 (`:1832`), `theme` a00e67d6 (`:1454`), `wallPaper` a437c3ed (`:441`), `timezone` ff9289f5 (`:1867`), `emojiGroup` 7a9abda9 (`:1722`), `emojiKeyword` d5b3b9f9 (`:1431`).
- So the JSON maps one-to-one onto layer 228. The Go importer should still refuse any `_` it does not know, as a guard in case the live site has moved on.

## 2. Downloading documents and thumbnails
**Do not use `apiFileManager.download()` or `downloadMedia()`**
- They **transform the bytes** (`getConvertMethod`, `apiFileManager.ts:686-703`): `.tgs` is gunzipped to JSON, `application/x-tgwallpattern` is gunzipped to SVG, webp/opus may be converted.
- They also write every file into tweb's CacheStorage `cachedFiles` (`:141`, `:813`). That would put 1–2 GB into the owner's profile.

**Use one of these instead**
- Raw call: `rootScope.managers.apiManager.invokeApi('upload.getFile', {location, offset, limit}, {dcId: doc.dc_id, fileDownload:true, noErrorBox:true, floodMaxTimeout:120})`. `fileDownload` selects the separate 'download' connections; non-premium accounts get up to 3 networkers per DC (`apiManager.ts:395-416`, `:49-50`). Wrong-DC errors are still fixed by `FILE_MIGRATE` handling.
- Or `rootScope.managers.apiFileManager.requestFilePart({dcId, location, offset, limit, floodMaxTimeout})` (`apiFileManager.ts:460-505`). This adds tweb's per-DC queue but no conversion.

**Building the location** (as tweb does)
- Main file: `{_:'inputDocumentFileLocation', id, access_hash, file_reference, thumb_size:''}` with `dcId = doc.dc_id` (`lib/appManagers/utils/docs/getDocumentInputFileLocation.ts:3-11`, `getDocumentDownloadOptions.ts:15-33`).
- Thumbnails: the same location with `thumb_size = photoSize.type` for `photoSize` / `photoSizeProgressive`, or `videoSize.type` for `video_thumbs`. Size comes from `.size` (`utils/photos/getPhotoDownloadOptions.ts:18-35`).
- `photoStrippedSize` ('i'), `photoPathSize` ('j') and `photoCachedSize` carry inline bytes; nothing to download.
- Sticker set covers: `{_:'inputStickerSetThumb', stickerset:{_:'inputStickerSetID', id, access_hash}, thumb_version}` on `dcId = set.thumb_dc_id` (`appStickersManager.ts:498-517`). Sets that use `thumb_document_id` point at a normal document instead.

**Chunking rules** (core.telegram.org/api/files, "Downloading files")
- Without `precise`: offset divisible by 4 KB, limit divisible by 4 KB, and 1 MB divisible by limit. Each part must stay inside one 1 MB block.
- tweb itself uses parts of 64 KB to 1 MB, growing only when there would be more than 8000 parts (`getLimitPart`, `apiFileManager.ts:515-535`, consts `:87-89`).
- Recommendation: `limit = 1048576` and offsets of k·1 MB. A short `bytes.length` means end of file. Almost every sticker then comes in one request. Check the total against `doc.size`.
- `cdn_supported` is never set (tweb does not set it either), so `upload.fileCdnRedirect` should not occur. Treat it as an error if it does.

**File references**
- On `FILE_REFERENCE_EXPIRED` / `FILE_REFERENCE_INVALID`, tweb refreshes only through `referencesStorage` contexts (`apiFileManager.ts:592-671`, `lib/storages/references.ts:339`). Those contexts are saved only for documents that went through tweb's own managers, not for raw `invokeApi` results.
- So the collector refreshes by itself: re-invoke the source method, take the new `file_reference` for that doc id, and retry once. Sources:
  - `messages.getStickerSet` (hash 0);
  - `messages.getCustomEmojiDocuments` (tweb batches 100 ids, `appEmojiManager.ts:368-377`);
  - `messages.getAvailableReactions`, `messages.getAvailableEffects`, `account.getWallPapers` / `getThemes`.
- To keep references fresh, download each source's documents right after fetching that source.

**DCs.** The first request to another DC triggers the export/import of the authorization (`apiManager.ts:707-721`). tweb does this for any foreign-DC media, so it is not a new login.

**FLOOD_WAIT.** It is handled inside `invokeApi`. Pass `floodMaxTimeout` around 120; anything longer throws, and the collector should then stop the whole run, not keep retrying.

## 3. Handing data to the Mac
**Loopback receiver** (recommended; Chrome 156 is installed)
- Chrome's Local Network Access permission prompt launched in Chrome 142, and covers requests from a public site to loopback (127.0.0.0/8, ::1) (developer.chrome.com/blog/local-network-access, "Update 2025-09-29" and "What kinds of requests are affected").
- The owner must click **Allow** once for web.telegram.org, and can revoke it afterwards in site settings.
- `http://127.0.0.1` is known ahead of time to be loopback, so it is exempt from mixed-content blocking (WICG explainer: lines 87, 137-143; line 35 notes http://localhost is embeddable). An optional `fetch(url, {targetAddressSpace:'loopback'})` makes this explicit (explainer :129, :145).
- PNA preflights (`Access-Control-Allow-Private-Network`) were put on hold and not enforced (developer.chrome.com/blog/pna-on-hold); LNA replaced them. Sending the header is harmless.
- Normal CORS still applies, because octet-stream bodies and custom headers trigger a preflight. The receiver must:
  - answer `OPTIONS` with `Access-Control-Allow-Origin: https://web.telegram.org`, `Access-Control-Allow-Methods: GET, PUT, POST`, `Access-Control-Allow-Headers: content-type, x-run-token, x-sha256` (and `Access-Control-Allow-Private-Network: true`);
  - bind 127.0.0.1 only.

**Fallback: File System Access API.** `showDirectoryPicker()` needs a secure context and transient user activation (MDN). Calling it from the console without a click fails. The collector can inject a button that the owner clicks, then write files with `getFileHandle(..., {create:true}).createWritable()`. This needs no site permission and no server, but nothing is verified on the Mac side while it runs.

**Avoid browser downloads.** 20k downloads trigger the multiple-downloads prompt and clutter `~/Downloads`. **UNVERIFIED** (from memory): the exact prompt behaviour.

## 4. Pacing
**What tweb does**
- Per-DC queue budget is `REGULAR_DOWNLOAD_DELTA` = 9·512 KB / 64 KB = 72 units, where one request costs `limit/64KB` (`apiFileManager.ts:97-99`, `:271-308`). That means about 4.5 MB in flight per DC: up to 72 parallel 64 KB requests, or 4 parallel 1 MB requests. Premium gets 448 units.
- File networkers: 3 regular, 6 premium (`apiManager.ts:49-50`).
- FLOOD_WAIT: sleep and retry (`apiManager.ts:744-776`).
- tweb ignores the documented `small_queue_max_active_operations_count` (grep finds no use). There is no other throttling: sticker sets are fetched on demand (`appStickersManager.ts:219`), and featured sets come from `invokeApiHashable('messages.getFeaturedStickers')` (`:540`).

**Recommended, deliberately below tweb's budget**
- At most 4 concurrent `upload.getFile` per DC and about 8 total, with 1 MB limit.
- Metadata methods one at a time, with 300–800 ms jitter between calls.
- Stop the whole run on any `FLOOD_WAIT` above 120 s, on any 401/406, or on `AUTH_KEY_*` / `SESSION_*`.
- About 25–40k getFile requests (documents plus thumbnails) at roughly 8 requests per second is about 1–1.5 hours.
- Run it once, in one tab, on the already logged-in session.

## Collector design (`collector.js`, pasted into DevTools Console or kept as a Snippet)
```js
const API=(m,p,o={})=>rootScope.managers.apiManager.invokeApi(m,p,{noErrorBox:true,floodMaxTimeout:120,...o});
const b64=u=>btoa(String.fromCharCode(...u)); // chunked for big arrays
const ser=v=>JSON.stringify(v,(k,x)=>x instanceof Uint8Array?{$b64:b64(x)}:x);
// 1 sources: getAvailableReactions, getTopReactions/getDefaultTagReactions(limit 100), getAvailableEffects,
//   getStickerSet {AnimatedEmoji, EmojiDefaultStatuses, PremiumGifts, AnimatedEmojiAnimations?, EmojiGenericAnimations?},
//   getFeaturedStickers + getOldFeaturedStickers(offset,limit) pages, getFeaturedEmojiStickers, then getStickerSet(id) for each covered set,
//   getEmojiKeywordsLanguages + getEmojiKeywords(lang) per lang, help.getPeerColors/getPeerProfileColors, account.getChatThemes/getThemes,
//   account.getWallPapers, getDefaultEmojiStatuses/ProfilePhotoEmojis/GroupPhotoEmojis/BackgroundEmojis, getEmoji*Groups, help.getTimezonesList
//   -> PUT /v1/<run>/method/<name>.json  (raw result, hash:0)
// 2 collect doc ids that are only referenced (emojiStatus.document_id, emojiList.document_id, emojiGroup icon ids, effect ids, thumb_document_id)
//   -> messages.getCustomEmojiDocuments in batches of 100 -> PUT .../method/customEmojiDocs-<n>.json
// 3 download queue keyed by (docId, thumbType|''): GET /v1/<run>/have/<key> to skip what exists (resume),
//   loop offset+=1MB until short part; on FILE_REFERENCE_* refetch the source once; PUT /v1/<run>/file/<docId>/<thumb> with x-sha256
// 4 PUT manifest.json: doc -> {source, dc_id, mime, size, thumbs, attributes}; errors list; tweb version, userAgent, timestamps
```
Documents are stored under Telegram's ids only in this local staging dump. The Go importer renumbers them when it puts them into dfs, as the owner decided, and the dump is deleted after import, because `access_hash` and `file_reference` are tied to the account.

## Receiver design (`tools/tgimport-receiver`, Go)
- `net.Listen("tcp","127.0.0.1:0")` on a random port.
- Prints a random run token; the collector must send it as `x-run-token`.
- CORS: only `https://web.telegram.org`; `OPTIONS` answered as in section 3.
- Endpoints:
  - `PUT /method/*`: writes the JSON atomically (temp file, then rename).
  - `PUT /file/*`: streams to disk, checks `x-sha256` and the size, renames into place. Body limit 64 MB.
  - `GET /have/*`: lets the collector resume.
- Output directory under `$HOME`, plus a `progress.json`.
- Logs no file contents and no references.
- A separate importer step then parses the JSON, accepting long fields as number or string and decoding `{$b64}`, checks every `_` against the layer-228 registry, uploads to dfs, assigns Pluma ids and rewrites all document and set references.

OPEN:
The live web.telegram.org/k build and layer were not checked in the browser (tweb master is layer 229; the 60 relevant constructors are identical to 228). Should the collector's first step print the live version, and abort if any constructor it gets is unknown?
Chrome 156 shows a Local Network Access prompt for web.telegram.org: does the owner accept granting it once and revoking it afterwards, or prefer the File System Access fallback (a page button plus a picked folder)?
Which sets count as 'everything': AnimatedEmojiAnimations, EmojiGenericAnimations, Dice sets, the effect stickers, and premium custom emoji sets in addition to the 231 featured and 99 featured emoji sets? Which languages for getEmojiKeywords: all from getEmojiKeywordsLanguages, or only those bundled in the clients?
UNVERIFIED: whether the LNA permission prompt can be triggered by a fetch() typed into the DevTools console without a user gesture. If not, the collector needs an injected 'Start' button.
Is the logged-in Web K account premium? tweb's download budget (72 vs 448 units) and the premium-only custom emoji behave differently, and this affects pacing and completeness.