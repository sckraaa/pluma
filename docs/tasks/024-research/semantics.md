## Sources used and versions

- **TL schema, layer 228.** `(a local download) tdesktop-src/api.tl` (`// LAYER 228` at :3087). It matches tdesktop v7.0.9 `SourceFiles/mtproto/scheme/api.tl`, which I downloaded to `(a local download) tdesk/api.tl`.
  - `third_party/teamgram-proto` contains no `.tl` file, only the generated Go code.
  - I spot-checked the constructor ids against `third_party/teamgram-proto/mtproto/schema.tl.crc32_helper.pb.go` and they agree: availableReaction c077ec01 (:1581), messages.availableReactions 768e3aad (:1583), availableEffect 93c3e27e (:1888), account.getChatThemes d638de89 (:2332).
- **tweb.** `~/pluma-web/upstream`, at exactly the pinned commit 346844bf (`~/pluma-web/upstream.env`).
- **Android.** `~/aaaaaaa/Telegram-Android`, version **12.6.4** (`gradle.properties:17`), commit 20091c78.
  - Pluma pins **12.9.1** (aac1efcb, `~/pluma-android/upstream.env`), so line numbers may drift. The behaviour is very likely the same, but that is UNVERIFIED against 12.9.1.
- **iOS.** `~/aaaaaaa/Telegram-iOS`, commit 756315d7. Its release relative to Pluma's pin is UNVERIFIED.
- **tdesktop.** Raw files from tag v7.0.9, downloaded to `(a local download) tdesk/`. Its api.tl is layer 228.
- **Current Pluma state.** Most of these methods are answered only by the fake proxy results in `app/bff/bff/client/fake_rpc_result.go` (getWallPapers :27, getAvailableReactions :119, getEmojiKeywords* :126–142). No bff handler files exist for them.

## 1. TL at layer 228 (line numbers in api.tl)

**Methods**

| Method | Line | Hash type | Returns |
|---|---|---|---|
| messages.getAvailableReactions | :2612 | **int** | messages.AvailableReactions |
| messages.getTopReactions | :2632 | limit:int, hash:**long** | messages.Reactions |
| messages.getRecentReactions | :2633 | limit:int, hash:**long** | messages.Reactions |
| messages.getDefaultTagReactions | :2656 | **long** | messages.Reactions |
| messages.getAvailableEffects | :2669 | **int** | messages.AvailableEffects |
| messages.getStickerSet | :2500 | **int** | messages.StickerSet |
| messages.getAllStickers | :2495 | **long** | messages.AllStickers |
| messages.getEmojiStickers | :2629 | **long** | messages.AllStickers |
| messages.getFeaturedStickers | :2523 | **long** | messages.FeaturedStickers |
| messages.getFeaturedEmojiStickers | :2630 | **long** | messages.FeaturedStickers |
| messages.getOldFeaturedStickers | :2580 | offset:int, limit:int, hash:**long** | messages.FeaturedStickers |
| messages.getStickers | :2494 | emoticon:string, hash:**long** | messages.Stickers |
| messages.getEmojiKeywords | :2562 | (lang_code) | EmojiKeywordsDifference |
| messages.getEmojiKeywordsDifference | :2563 | (lang_code, from_version) | EmojiKeywordsDifference |
| messages.getEmojiKeywordsLanguages | :2564 | (lang_codes) | Vector<EmojiLanguage> |
| messages.getEmojiURL | :2565 | (lang_code) | EmojiURL |
| messages.getEmojiGroups, getEmojiStatusGroups, getEmojiProfilePhotoGroups | :2639–2641 | **int** | messages.EmojiGroups |
| messages.getEmojiStickerGroups | :2668 | **int** | messages.EmojiGroups |
| help.getPeerColors, help.getPeerProfileColors | :2761, :2762 | **int** | help.PeerColors |
| help.getTimezonesList | :2763 | **int** | help.TimezonesList |
| account.getWallPapers | :2301 | **long** | account.WallPapers |
| account.getThemes | :2355 | format:string, hash:**long** | account.Themes |
| account.getChatThemes | :2364 | **long** | **account.Themes** (not account.ChatThemes) |
| account.getDefaultEmojiStatuses | :2371 | **long** | account.EmojiStatuses |
| account.getDefaultProfilePhotoEmojis, getDefaultGroupPhotoEmojis | :2376, :2377 | **long** | EmojiList |
| account.getDefaultBackgroundEmojis | :2383 | **long** | EmojiList |

account.ChatThemes (:1505) is what account.getUniqueGiftChatThemes returns (:2412).

**Constructors**

- **Reactions and effects**
  - `availableReaction#c077ec01 flags inactive:0 premium:2 reaction title static_icon appear_animation select_animation activate_animation effect_animation around_animation:flags.1 center_icon:flags.1` (:1534).
  - `messages.availableReactions#768e3aad hash:int` and NotModified#9f071957 (:1536–1537).
  - `reactionEmoji`, `reactionCustomEmoji`, `reactionPaid` (:1625–1627).
  - `messages.reactions#eafdf716 hash:long` and NotModified#b06fdbdf (:1633–1634).
  - `availableEffect#93c3e27e premium_required:2 id emoticon static_icon_id:flags.0 effect_sticker_id effect_animation_id:flags.1` (:1933).
  - `messages.availableEffects#bddb616e hash:int effects documents` and NotModified (:1935–1936).
- **Sticker sets**
  - `stickerSet#2dd14edc` with flags archived/official/masks/emojis/text_color/channel_emoji_status/creator, installed_date, id, access_hash, title, short_name, thumbs+thumb_dc_id+thumb_version (flags.4), thumb_document_id (flags.8), count, hash:int (:701).
  - `messages.stickerSet#6e153f16 set packs keywords documents` and NotModified#d3f924eb (:703–704).
  - `stickerPack#12b299d4 emoticon documents:Vector<long>` (:655).
  - `stickerKeyword#fcfeb29c document_id keyword:Vector<string>` (:1654).
  - `messages.allStickers#cdbbcebb hash:long sets:Vector<StickerSet>` and NotModified (:657–658).
  - `messages.featuredStickers#be382906 premium:0 hash:long count sets:Vector<StickerSetCovered> unread:Vector<long>` and NotModified#c6dc0c66 `count:int` (:887–888).
  - `stickerSetCovered`, `stickerSetMultiCovered`, `stickerSetFullCovered`, `stickerSetNoCovered` (:898–901).
  - `messages.stickers#30a6ec7e hash:long` and NotModified (:652–653).
  - InputStickerSet variants (:688–699): Empty, ID, ShortName, AnimatedEmoji, Dice, AnimatedEmojiAnimations, PremiumGifts, EmojiGenericAnimations, EmojiDefaultStatuses, EmojiDefaultTopicIcons, EmojiChannelDefaultStatuses, TonGifts.
  - `inputStickerSetThumb` (:70).
  - `documentAttributeSticker#6319d612 … stickerset:InputStickerSet` (:645).
  - `documentAttributeCustomEmoji#fd149899 free text_color alt stickerset` (:650).
- **Emoji keywords, URL and groups**
  - `emojiKeyword`, `emojiKeywordDeleted` (:1312–1313).
  - `emojiKeywordsDifference#5cc761bd lang_code from_version version keywords` (:1315).
  - `emojiURL` (:1317), `emojiLanguage` (:1319).
  - `emojiGroup`, `emojiGroupGreeting`, `emojiGroupPremium` (:1675–1677).
  - `messages.emojiGroups#881fb94b hash:int` and NotModified (:1679–1680).
- **Emoji lists and statuses**
  - `emojiList#7a1e11d1 hash:long document_id:Vector<long>` and NotModified (:1672–1673).
  - `emojiStatus`, `emojiStatusCollectible` (:1617–1618).
  - `account.emojiStatuses#90c467d1 hash:long` and NotModified (:1621–1622).
- **Colors**
  - `help.peerColorSet`, `help.peerColorProfileSet` (:1808–1809).
  - `help.peerColorOption#adec6ebe` (:1811).
  - `help.peerColors#f8ed08 hash:int` and NotModified (:1813–1814).
- **Themes and wallpapers**
  - `theme#a00e67d6 … for_chat:5 slug title document:flags.2 settings:flags.3 emoticon:flags.6` (:1344).
  - `themeSettings#fa58b6d4 … wallpaper:flags.1?WallPaper` (:1365).
  - `account.themes#9a3d8c6d hash:long` and NotModified (:1346–1347).
  - `wallPaper#a437c3ed id … access_hash slug document settings` and `wallPaperNoFile` (:255–256).
  - `account.wallPapers#cdc3858c hash:long` and NotModified (:1301–1302).
- **Timezones**
  - `timezone#ff9289f5 id name utc_offset` (:1864).
  - `help.timezonesList#7b74ed71 timezones hash:int` and NotModified (:1866–1867).

## 2. How hashes are computed

**The common algorithm.** https://core.telegram.org/api/offsets#hash-generation gives it as:

`h ^= h>>>21; h ^= h<<35; h ^= h>>>4; h += id`, starting from 0, unsigned 64-bit arithmetic.

- The page says that if the result "already has a `hash` field, that can be used instead", and that a matching hash gets the `*NotModified` constructor.
- Client implementations, all identical:
  - Android `MediaDataController.calcHash` (MediaDataController.java:2434–2439)
  - tdesktop `Api::HashUpdate` (api_hash.h:40–45)
  - iOS `combineInt64Hash` (TelegramCore/Sources/State/HistoryViewStateValidation.swift:450)

**Hashes computed on the client.** The server must compute the same value to answer NotModified.

- **getAllStickers / getEmojiStickers.** The input is each installed, non-archived set's `stickerSet.hash` (the int32 hash field), in the user's set order.
  - tdesktop `CountStickersOrderHash` (api_hash.cpp:39–62) skips Special and Archived sets. It sends 0 if a placeholder default set is present.
  - iOS `ManagedSynchronizeInstalledStickerPacksOperations.swift:133`.
  - Android `calcStickersHash` (MediaDataController.java:3124–3136) skips archived sets.
  - Android sends `loadHash`, which is the server's `hash` from the previous answer (`processLoadStickersResponse`, :2593–2640). So the server's hash must equal this formula.
  - tdesktop compares and logs "API Error: received … hash while counted hash is …" on a mismatch (data_stickers.cpp:778–791).
- **getFeaturedStickers / getFeaturedEmojiStickers.** The input is each featured set id in server order. After each set that is in `unread`, `HashUpdate(1)` is added.
  - tdesktop `CountFeaturedHash` (api_hash.cpp:64–78)
  - iOS `hashForIdsReverse(ids, unreadIds)` (StickerManagement.swift:41–50, used at :156)
  - Android `calcFeaturedStickersHash` (:2416–2431) also skips archived sets.
  - tdesktop logs a mismatch (data_stickers.cpp:1105–1112).
  - tdesktop sends the *stickers* featured hash for the emoji call too (apiwrap.cpp:3335–3337). That is a client quirk. The server should simply compare, and answer in full on a mismatch.
- **getWallPapers.** Android computes it over `wallPaper.id` of the cached list, skipping ids below 0 (WallpapersListActivity.java:1089–1103).
  - tweb sends back the server's hash (`invokeApiHashable`).
  - So the server's `account.wallPapers.hash` must be this formula over the wallpaper ids in answer order.
- **getRecentReactions.** The formula is defined at https://core.telegram.org/api/reactions:
  - reactionEmoji: append 0, then the int64 built from the first 4 bytes of the binary MD5 of the emoticon with U+FE0F stripped (cast to int32, then int64, then uint64).
  - reactionCustomEmoji: append `document_id>>32`, then `document_id & 0xFFFFFFFF`.
  - The result then goes through the common algorithm.
  - Recents belong to stage 2 (they are per-user), but the formula should be shared with top and tag reactions.

**Opaque hashes.** All clients store the server's `hash` and send it back unchanged, so any stable value works if it changes when the content changes:

- getAvailableReactions
  - Android `loadReactions` sends `reactionsUpdateHash` (:770–771).
  - tdesktop `_defaultHash` (data_message_reactions.cpp:1031–1033).
  - iOS `current?.hash` (AvailableReactions.swift:411).
  - tweb always sends 0 (appReactionsManager.ts:171–196).
- getTopReactions / getDefaultTagReactions
  - Android (:9460–9462, :9502–9503)
  - tdesktop (:990–993, :1096–1097)
  - tweb `invokeApiHashable` (appReactionsManager.ts:313)
- getAvailableEffects
  - Android CacheFetcher (MessagesController.java:23188–23190)
  - tdesktop (:1116–1117)
  - iOS (AvailableMessageEffects.swift:232)
  - tweb sends 0 (:1090–1105).
- getEmoji*Groups
  - Android (StickerCategoriesListView.java:902–926)
  - tdesktop (data_emoji_statuses.cpp:255–275)
  - tweb always sends 0 (appEmojiManager.ts:428–430).
- help.getPeerColors / getPeerProfileColors
  - Android (MessagesController.java:22968–23005)
  - tdesktop (api_peer_colors.cpp:37–64)
  - iOS (ManagedPeerColorUpdates.swift:341–343)
  - tweb sends 0 (apiManagerMethods.ts:399–403).
- help.getTimezonesList
  - Android (TimezonesController.java:76–77)
  - tweb sends 0 (apiManagerMethods.ts:365).
- account.getThemes / getChatThemes
  - Android (Theme.java:7515–7520, ChatThemeController.java:122–123)
  - tdesktop (data_cloud_themes.cpp:486–528)
  - tweb (appThemesManager.ts:106–107)
- account.getDefaultEmojiStatuses
  - Android (:9345–9346)
  - tdesktop (data_emoji_statuses.cpp:354–355)
- account.getDefault*Emojis
  - Android (MediaDataController.java:9587–9591, :9630–9634)

**getStickerSet: every client sends hash 0.**

- Android never sets `req.hash` (MediaDataController.java:1596, 2619–2622, 2661, 2725, 2760, 2804).
- tdesktop sends `MTP_int(0)` and logs "Unexpected messages.stickerSetNotModified" (stickers_emoji_pack.cpp:331–341, 352–362; data_message_reactions.cpp:1050–1058; data_emoji_statuses.cpp:375–382; stickers_dice_pack.cpp:50–57).
- iOS sends `hash: 0` at all 7 call sites.
- tweb sends `hash: 0` (appStickersManager.ts:219–222).
- https://core.telegram.org/api/stickers still defines the semantics: start with 0, then send `stickerSet.hash` back, and get NotModified when unchanged. Clients detect changes through `stickerSet.hash` inside getAllStickers and featured answers instead (Android :2603–2614; iOS StickerManagement.swift:73–76).

**Server rule that follows from the client code: never answer NotModified when the request hash is 0 or differs from yours.** Some clients crash or break on an unexpected NotModified:

- tweb `invokeApiHashable` returns `cached.result` on NotModified, and `cached` is undefined when nothing is cached (apiManagerMethods.ts:116–134).
- tweb `getAvailableReactions` and `getEmojiGroups` assume the full constructor (appReactionsManager.ts:173–174; appEmojiManager.ts:431–432).
- Android casts `messages.getStickers` (⭐⭐ premium preview, hash 0) straight to `TL_messages_stickers` (MediaDataController.java:8432–8441), and the getStickerSet response straight to `TL_messages_stickerSet` (:2622–2624, :1603–1605).

## 3. Startup requests, refresh intervals, and empty or error answers

**Android**

- **Refresh intervals**
  - `checkStickers`, `checkReactions`, `checkFeaturedStickers` and `checkFeaturedEmoji` each re-request after 1 h (MediaDataController.java:419–428, 886–895).
  - After loading reactions from cache it always also asks the network: `|| true` (:810).
  - Emoji keywords: at most once per hour per language (:8570).
  - PremiumGifts, GenericAnimations and DefaultTopicIcons sets: once per 24 h (:2656, :2720, :2755).
  - getDefault*Emojis: once per 24 h (:9587, :9630).
  - Peer colors: on login, 400 ms after config, and forced on every `updateConfig` (MessagesController.java:1928, :5684; LoginActivity.java:1666).
  - Chat themes: when the theme picker opens.
- **Retry loop on error.** If getAllStickers, getEmojiStickers or getFeatured* return an error (or anything that is not the expected constructor) while the client hash is 0, Android re-requests every 1 s indefinitely (`!cache && res == null && hash == 0` then `loadStickers` after 1000 ms, :3151–3157; featured :2323–2330).
  - An **empty** `messages.allStickers` or `messages.featuredStickers` stops the loop (`res.sets.isEmpty()` gives a non-null empty list, :2595–2596).
  - So these methods must never return an error.
- **Peer colors.** A response that is not `help.peerColors` (NotModified or error) leaves `loadingPeerColors=true` for the rest of the session (MessagesController.java:22974–22986). No retry, colors stay stale.
  - `PeerColors.needUpdate()` forces hash 0 if no option has id below 7 or none has levels (:5097–5118). An empty list therefore means re-requesting full every time.

**tdesktop**

- **Refresh intervals**
  - Reactions and effects: every 1 h (`kRefreshFullListEach`, data_message_reactions.cpp:49, :382–386). Top reactions: delayed 60 s (:55, :1256).
  - AnimatedEmoji and AnimatedEmojiAnimations: every 2 h (`kRefreshTimeout` 7200 s, stickers_emoji_pack.cpp:38, :327–365, :530). Retried after 2 h on error.
  - Emoji keywords: every 1 h (emoji_keywords.cpp:28, :385–417). It uses `getEmojiKeywords` when `version == 0`, otherwise `getEmojiKeywordsDifference`. The language list comes from `getEmojiKeywordsLanguages` (:703–716).
  - Peer colors: every 1 h (api_peer_colors.cpp:19).
  - Themes: first after 10 s, then every 1 h (data_cloud_themes.cpp:30–31).
  - Default emoji statuses: every 1 h (data_emoji_statuses.cpp:27).
- **Errors.** On error the client mostly resets its hash to 0 and waits for the next tick (data_message_reactions.cpp:999–1002, 1039–1042).
- **Emoji keywords strictness.** If `emojiKeywordsDifference.lang_code` is not exactly the requested code, the client logs "Bad lang_code", resets the version to 0 and ignores the answer (emoji_keywords.cpp:422–431). Empty keywords with `version <= stored` means unchanged (:432–434).

**iOS**

- **Refresh intervals**
  - AvailableReactions: every 1 h, and on each pass it downloads **every** reaction document (static, appear, select, activate, effect, plus center and around) (AvailableReactions.swift:408–481).
  - AvailableEffects: every 1 h (AvailableMessageEffects.swift:315).
  - AnimatedEmoji, AnimatedEmojiAnimations and EmojiGenericAnimations: every 2 h with `forceActualized: true` (ManagedAnimatedEmojiUpdates.swift:7–29).
  - Peer colors: every 2 h (ManagedPeerColorUpdates.swift:320).
  - Installed stickers sync: every 1 h (StickerManagement.swift:54–62).
- **Featured sets.** iOS issues one getStickerSet per featured set that comes back as `stickerSetCovered` or `stickerSetNoCovered` (StickerManagement.swift:64–137).
- **Reaction parsing.** `AvailableReactions.Reaction.init?(apiReaction:)` drops a reaction if any of static_icon, appear, select, activate or effect fails `telegramMediaFileFromApiDocument` (AvailableReactions.swift:315–352). All five must be real documents, not documentEmpty.
- **Emoji keywords.** `getEmojiKeywordsDifference` goes through `retryRequest`, so errors are retried (ManagedSynchronizeEmojiKeywordsOperations.swift:122–123). The difference is applied only if `langCode == languageCode` (:128).

**tweb**

- **Startup.** On `user_auth`, after 1 s, it fetches AnimatedEmoji, AnimatedEmojiAnimations, GenericAnimations and appConfig `emojies_sounds` (appStickersManager.ts:56–67, 243–250).
  - It downloads **every** GenericAnimations document (:60–64).
  - The greeting sticker request follows at 5 s (:68–73).
- **Sticker set cache.** Sets are cached for 1 h (`CACHE_TIME`, :27, :184–186). An error resolves to null (:224–226).
- **Emoji keywords.** It always calls `getEmojiKeywordsDifference` with `from_version` = stored version, starting at 0 (appEmojiManager.ts:25–29, 117–158).
  - It writes every keyword as an add, even `emojiKeywordDeleted` (:141–145). Do not send deleted entries for from_version 0.
- **Hash cache.** `invokeApiHashable` caches only when `result.hash` is truthy (apiManagerMethods.ts:136–144).

**Language codes, getEmojiURL and getStickers(emoticon)**

- **Android keywords.** Android stores `res.lang_code` as an alias. If a later difference returns a different lang_code, it deletes the local data and refetches (MediaDataController.java:8594–8600).
- **Server rule for keywords.** Echo the requested `lang_code` exactly. For an unknown language return an empty difference with a version, never an error.
- **getEmojiURL.** Android calls it only on a tap in the emoji-suggestion info dialog (EmojiView.java:7808–7824). It opens `emojiURL.url` and ignores errors.
- **getStickers(emoticon).** Android preview uses hash 0 (MediaDataController.java:8432). tweb uses `invokeApiHashable` (appStickersManager.ts:778–784). The `stickers_emoji_cache_time` appConfig key governs the cache (https://core.telegram.org/api/stickers, Sticker suggestions).

## 4. Special sticker sets and what renders from them

**AnimatedEmoji: the big single emoji**

- Android `getEmojiAnimatedSticker` strips U+FE0F, finds the `stickerPack` whose emoticon equals the emoji, and takes `pack.documents[0]` among the set's documents (MediaDataController.java:1698–1715).
- tdesktop `EmojiPack::applySet` maps packs to documents (stickers_emoji_pack.cpp:370–392).
- iOS `managedAnimatedEmojiUpdates`. https://core.telegram.org/api/animated-emojis says it is fetched at startup.
- **Consequence:** `packs[].documents` must be remapped to the new ids together with `documents[]`.

**AnimatedEmojiAnimations: emoji interaction animations**

- tdesktop gives packs whose emoticon starts with '1'..'9' or 🔟 an **index** meaning (`IndexFromEmoticon`, stickers_emoji_pack.cpp:42–52). Documents in those index packs are joined with the per-emoji packs (`collectAnimationsIndices` and `applyAnimationsSet`, :396–442).
- The index is the 1-based `i` in the `sendMessageEmojiInteraction` JSON (https://core.telegram.org/api/animated-emojis).
- **Consequence:** keep every pack and its emoticon string exactly, remapping only the ids.

**EmojiAnimations, resolved by short name**

- Android's interaction overlay loads a set by short name `"EmojiAnimations"` through `inputStickerSetShortName` (EmojiAnimationsOverlay.java:52, :153–158).
- **Consequence:** short names must be kept, not renumbered.
- Android also keys several sets by `short_name.toLowerCase()` (MediaDataController.java:1396, :1423).

**EmojiGenericAnimations**

- This is the fallback `effect_animation` for custom-emoji reactions, played at random (https://core.telegram.org/api/reactions).
- tdesktop `requestGeneric` (data_message_reactions.cpp:1045–1062).
- tweb preloads all its documents at startup.

**Dice**

- `inputStickerSetDice{emoticon}`: Android (MediaDataController.java:2804–2811), tdesktop (stickers_dice_pack.cpp:50–57).
- Values and emoticons come from appConfig `emojies_send_dice` / `emojies_send_dice_success` (MessagesController.java:3050, :3094).
- Dice are not in the task 024 table but use the same path.

**PremiumGifts, DefaultStatuses, DefaultTopicIcons, ChannelDefaultStatuses**

- PremiumGifts: Android `checkPremiumGiftStickers`, once per 24 h. It also matches appConfig `premiumGiftsStickerPack` by name (:2643–2665, :2805–2806).
- EmojiDefaultStatuses ("colored" statuses): tdesktop `requestColored` (data_emoji_statuses.cpp:368–387).
- DefaultTopicIcons and ChannelDefaultStatuses: Android (:2755–2761); tweb `STICKERS_LOCAL_IDS` (utils/stickers/constants.ts).

**Reaction fields**

- **Android**
  - The first 10 reactions have `activate_animation` and `appear_animation` preloaded (MediaDataController.java:815–830).
  - Field use by file: static_icon in AvailableReactionCell and ReactedUserHolderView; appear and select in ReactionsContainerLayout and ReactionsEffectOverlay; effect in ReactionsEffectOverlay; around and center in ReactionsEffectOverlay, ReactedHeaderView and ChatActivity. These are from a grep of field references, not read in depth.
- **tdesktop** uses appear, select, center_icon and around_animation. static_icon, activate and effect are commented out (data_message_reactions.cpp:1442–1461). It also rejects reactions whose emoji is unknown to it (:1436–1441).
- **iOS** requires all five non-optional fields and downloads all seven.
- **tweb** saves the docs listed in SAVE_DOC_KEYS (appReactionsManager.ts:31–).
- **core.telegram.org/api/reactions:** for custom emoji, appear and select equal the emoji itself. effect_animation equals the matching normal emoji's, otherwise a random GenericAnimations sticker.

**Effects**

- Every long id except `id` is a document id inside the same `messages.availableEffects.documents` (https://core.telegram.org/api/effects).
- tdesktop rejects an effect whose `effect_sticker_id`, `effect_animation_id` or `static_icon_id` document is not a sticker, i.e. lacks documentAttributeSticker (data_message_reactions.cpp:1464–1492).
- When `effect_animation_id` is absent, the effect is the sticker's **videoSize type "f"** (premium effect, TGS):
  - Android `getPremiumStickerAnimation` (MessageObject.java:737–747), EmojiAnimationsOverlay.java:670–681.
  - https://core.telegram.org/api/files: download with `thumb_size="f"`.
- **Consequence:** the collector must also download `video_thumbs` (type f) and photo thumbs of every document, plus set thumbnails (`inputStickerSetThumb` with `thumb_version`, or `thumb_document_id`).

**Featured covers**

- iOS refetches sets that come as `stickerSetCovered` or `stickerSetNoCovered`.
- tweb stores `stickerSetFullCovered` documents directly (appStickersManager.ts:539–556).

## 5. Renumbering: Telegram ids outside these methods

- **appConfig `emojies_sounds`** carries document `id`, `access_hash` and `file_reference_base64`.
  - Clients build a document from it and download it (tweb appStickersManager.ts:254–295; https://core.telegram.org/api/animated-emojis).
  - help.getAppConfig must be rewritten together with the import.
- **Telegram document ids hard-coded in the clients.** These will not resolve after renumbering:
  - tweb `emojiGroupPremium.icon_emoji_id '5269590556232664327'`, fetched with getCustomEmojiDocuments (appEmojiManager.ts:419–425, :434).
  - tweb General-topic icon `'5390854796011906616'` (components/wrappers/messageActionTextNewUnsafe.ts:108–110).
  - Android `DIAMOND_EMOJI 5471952986970267163L` (ChannelMonetizationLayout.java:1019).
  - Android theme preview entities 5386654653003864312L and 5373141891321699086L (ThemePreviewMessagesCell.java:197, :237).
  - Android TranslateButton 5197252827247841976L (:366).
- **Wallpaper slugs.** Android hard-codes the slug `fqv01SQemVIBAAAApND8LDRUhRU` (WallpapersListActivity.java:1023, :1120). Slugs must be kept.
- **account.getThemes format differs per client:** "android" (Theme.java:7516), "tdesktop" (data_cloud_themes.cpp:238–241), "macos" for tweb (appThemesManager.ts:107). The `theme.document` is platform-specific, so the import would need per-format answers. That is an inference from the format parameter.
- **getDefaultTagReactions limit.** tweb passes `limit: 75` to getDefaultTagReactions (appReactionsManager.ts:311–314). The method has no limit field, so it is simply not serialized.

OPEN:
Android/iOS line numbers come from local checkouts (Android 12.6.4, iOS commit 756315d7), not the pinned releases (Android 12.9.1 aac1efcb; iOS pin not checked). Should the key sites (MediaDataController retry loop :3151, checkPeerColors, AvailableReactions.swift) be re-verified at the pins?
Hard-coded Telegram document ids in clients (tweb 5269590556232664327 and 5390854796011906616; Android 5471952986970267163, 5386654653003864312, 5373141891321699086, 5197252827247841976) will not resolve after renumbering. Options: keep those specific ids, patch the clients, or accept the loss. Owner/ADR decision.
help.getAppConfig values that point at Telegram data (emojies_sounds document id/access_hash/file_reference, premiumGiftsStickerPack name, emojies_send_dice) must be rewritten with the imported data. Is that in task 024's scope?
account.getThemes depends on format ('android', 'tdesktop', 'macos', and presumably 'ios', unverified). Should the collector export themes once per format, or only themes with themeSettings (for_chat / chat themes), which are cross-platform?
getRecentReactions is per-user (stage 2). Should its hash formula (MD5-based, /api/reactions) be implemented now for top/tag/recent reactions so they share one helper?
Dice sets (inputStickerSetDice) and inputStickerSetTonGifts are not in the 024 table, but clients request them through the same getStickerSet path. Add them to the import?
Does the server need per-user getAllStickers/getEmojiStickers state (installed sets and order) in this task, or only catalogue data? The hash must equal calcHash over the installed sets' stickerSet.hash, or tdesktop logs a mismatch and Android loops on errors.