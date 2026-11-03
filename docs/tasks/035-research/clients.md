# What the official clients need from the server for channels and supergroups

## Sources read
- **Android**: local `~/aaaaaaa/Telegram-Android`, HEAD `20091c78`, `gradle.properties:17` `APP_VERSION_NAME=12.6.4`. This is the local tree. I did not check it against the pinned release in `upstream.env`.
- **iOS**: Telegram-iOS at `6ad963e5`, fetched with `gh api`. Files are under `submodules/TelegramCore/Sources/{State,Network,TelegramEngine/Peers}/`.
- **tdesktop**: tag v7.0.9, fetched with `gh api`. Files are under `Telegram/SourceFiles/`.
- **Web K**: `~/pluma-web/upstream` at `346844bf`, as pinned.
- **core.telegram.org/api/updates**: fetched for the update protocol.
- **Server today** (`docs/project/api-coverage.md`): `updates.getChannelDifference` is a stub that returns `ERR_ENTERPRISE_IS_BLOCKED` (line 1016; handler `app/bff/updates/internal/core/updates.getChannelDifference_handler.go`). `channels.createChannel`, `getFullChannel`, `getMessages`, `readHistory`, `deleteMessages`, `editAdmin`, `editBanned` and most other `channels.*` methods are missing (lines 283-322). `channels.checkUsername` is blocked (line 280).

---

## 1. Minimal methods and updates per stage

### Protocol rules (core.telegram.org/api/updates)
- Each channel has its own event sequence. The channel's state is just its pts.
- Call `getChannelDifference` with limit 10-100. If `final` is not set, call it again with the returned pts. Do not call it again after a final result.
- Re-invoke it after `timeout` seconds, or after 1 s when the flag is absent, for channels the user is viewing. Stop when the channel is closed. Short-poll at most 10 channels.
- On startup, call only `updates.getDifference`. Channels that need catching up are reported by `updateChannelTooLong` inside that difference.
- `channelDifferenceTooLong` means the client re-fetches the latest state of that message box.
- `messages.getHistory` cannot fill channel gaps.

### Create
- **Android** (`MessagesController.java:14398-14434`): `channels.createChannel` with `broadcast` or `megagroup` (`forum` for forums). It applies the Updates with `processUpdates`. `updates.chats` must not be empty, otherwise it posts `chatDidFailCreate`.
  - The create screen sends `channels.checkUsername(inputChannelEmpty, "1")` to probe the public-link limit (`ui/ChannelCreateActivity.java:170-173`). Only `CHANNELS_ADMIN_PUBLIC_TOO_MUCH` blocks public links.
  - Then it calls `getAdminedPublicChannels`, `updateUsername` and `messages.getExportedChatInvites(limit=1)` (`:935-944`). See §2 for a crash in that last call.
  - Then `channels.inviteToChannel`.
- **iOS** (`TelegramEngine/Peers/ChannelCreation.swift:50-90`): `channels.createChannel`.
  - The returned `Updates` must contain a message (`updates.messages.first`, built from update messages in `State/UpdatesApiUtils.swift:665-690`) whose peer is the new channel. In practice that is `updateNewChannelMessage` with `messageActionChannelCreate`.
  - That peer must then appear in the local store within 5 s. Otherwise the result is `.generic` failure.
  - Errors it maps: 406 → shows the server text; `CHANNELS_TOO_MUCH`, `CHANNELS_ADMIN_LOCATED_TOO_MUCH`, `USER_RESTRICTED`.
- **Web K** (`appManagers/appChatsManager.ts:588-593`): takes `updates.chats[0].id`. An Updates without chats throws.
- **tdesktop**: not read in detail (UNVERIFIED). It goes through `ApiWrap` and `applyUpdates`.

### Open a channel
- **Android**:
  - `loadFullChat` → `channels.getFullChannel` plus `loadChannelAdmins` (`MessagesController.java:7546-7575`).
  - `messages.getHistory` → `messages.channelMessages`. Its `pts` seeds the channel pts when none is stored, then it calls `getChannelDifference` (`:11480-11494`).
  - `startShortPoll` → `getChannelDifference(type 3, force=false)` while the chat is open (`:15735-15771`). The timer re-polls when the server `timeout` expires (`:10357-10367`, `:16051`).
  - Views: `messages.getMessagesViews(increment)` every 5 s for visible posts (`:10216-10270`).
- **iOS**:
  - `channels.getFullChannel` together with `channels.getParticipant(inputPeerSelf)` (`UpdateCachedPeerData.swift:656-680`).
  - Opening history starts `polledChannel` → `keepPollingChannel` (`State/AccountViewTracker.swift:1877-1920`, `:2015-2019`). That runs `getChannelDifference(flags=0, pts=max(pts,1), limit=100)` and repeats after `timeout ?? 30` s (`State/AccountStateManagementUtils.swift:3112-3170`, `:3220-3224`, `:3433-3448`).
  - Views: `messages.getMessagesViews(increment: true)` (`AccountViewTracker.swift:723`).
- **tdesktop**:
  - An active chat sets a 1 s short poll (`api/api_updates.cpp:78-81`, `:756-772`).
  - After each final difference it waits `timeout` s, or 1 s if absent (`:433-445`).
  - `getChannelDifference` returns early unless `ptsInited` (`:726`). pts comes from `messages.channelMessages.pts` (`data/data_session.cpp:2954-2957`, `apiwrap.cpp:3470-3471`) or from `dialog.pts`.
- **Web K**:
  - Channel state is seeded from `dialog.pts` (`lib/storages/dialogs.ts:1745-1746`) or `channelMessages.pts` (`appMessagesManager.ts:12531-12532`).
  - `addChannelState` throws on pts 0 (`apiUpdatesManager.ts:531-536`).
  - It polls every 3 s only when the user is not a member, or the chat is a forum (`components/chat/bubbles.ts:6440-6456`, `appMessagesManager.ts:11659-11664`, `apiUpdatesManager.ts:849-860`).

### Read history
- `messages.getHistory` must answer with `messages.channelMessages` carrying a non-zero `pts`. All four clients read it (references above).
- `channels.getMessages` resolves replies and missing messages (iOS `AccountStateManagementUtils.swift:2870`, `:2906`; Android uses it at 25 sites).
- Marking read: `channels.readHistory` and `channels.readMessageContents` (Android uses both; iOS `AccountViewTracker` uses `channels.readMessageContents`).

### Send and receive in real time
- `messages.sendMessage` / `sendMedia` with an `inputPeerChannel` returns Updates with `updateMessageID` and `updateNewChannelMessage(pts, pts_count)`.
- Other live updates: `updateEditChannelMessage`, `updateDeleteChannelMessages`, `updateReadChannelInbox`, `updateReadChannelOutbox`, `updateChannelMessageViews`, `updateChannel`.
- **Gap rule, same in every client**: apply the update when `local_pts + pts_count == pts`, skip it when `pts <= local`, otherwise queue it and fetch the difference.
  - Android waits 1.5 s (`MessagesController.java:17405-17500`, `:15554-15610`).
  - Web K waits `SYNC_DELAY = 6` ms (`apiUpdatesManager.ts:54`, `:695-720`).
  - iOS checks the same arithmetic (`AccountStateManagementUtils.swift:974-1036`) and adds `missingUpdatesFromChannels`, which leads to `pollChannel`.
- **The channel (and the users it references) must be in `updates.chats` / `updates.users`**:
  - Android: an unknown channel with no stored pts triggers `messages.getPeerDialogs` (`loadUnknownChannel`, `:15678-15730`), which needs `dialog.pts`.
  - Web K: a missing author or peer triggers `getChannelDifference` or `getDifference` (`apiUpdatesManager.ts:668-690`).
- **`updateChannel`**:
  - Android, joined channel without a dialog: `getChannelDifference(type 1)`, i.e. pts=1, limit=1 (`MessagesController.java:19320-19327`, `:15820-15830`).
  - Android, user left or was kicked: deletes the dialog.
  - Web K re-reads the chat; it reloads the dialog via `getPeerDialogs` or drops it (`appMessagesManager.ts:10810-10847`).
- **Method responses with a channel pts**: `messages.affectedMessages` from `channels.deleteMessages` and similar go through `processNewChannelDifferenceParams` (`MessagesController.java:8054-8095`). The pts there must belong to the channel's sequence.

### Catch up after being offline
- `updates.getDifference` must put `updateChannelTooLong(channel_id, pts?)` into `other_updates` for every channel with new events, together with the `Chat` objects.
  - **iOS**: after a `getDifference` (`shouldResetChannels: true`, `AccountStateManagementUtils.swift:782`) these channels are reset with `messages.getPeerDialogs`, not `getChannelDifference` (`:2042-2070`, `resetChannels` `:3226-3240`). So `getPeerDialogs` must return a dialog with `pts`, `top_message`, read state and the top messages. Outside a difference, `updateChannelTooLong` leads to `pollChannel` (`:949-958`, `:2074-2080`).
  - **Android**: on `updateChannelTooLong`, calls `getChannelDifference` when `pts` is absent or greater than the local pts. It uses `getPeerDialogs` when there is no local pts (`MessagesController.java:18462-18494`).
  - **tdesktop**: calls `getChannelDifference` when `pts` is absent or greater than the local pts (`api_updates.cpp:2480-2488`). If the channel is not loaded it ignores the update.
  - **Web K**: acts only if it already has a state for the channel (`apiUpdatesManager.ts:632-636`, `:658-664`).
- **`updates.getChannelDifference` result handling**:
  - `channelDifference{final, pts, timeout?, new_messages, other_updates, chats, users}`:
    - Android feeds `updateMessageID` first and re-requests when not final (`MessagesController.java:15895-16056`).
    - tdesktop re-requests at once when not final (`api_updates.cpp:389-437`).
    - Web K re-requests recursively with limit 1000 (`apiUpdatesManager.ts:435-487`).
    - iOS: I found no `final` handling in `pollChannel` (`:3433-3480`). Inferred from that code, iOS takes one page per poll and gets the next on the following poll or update.
  - `channelDifferenceEmpty{pts, timeout?}`: all four clients take the new pts from it.
  - `channelDifferenceTooLong{dialog with pts, messages, chats, users}`:
    - Android: `overwriteChannel` (`:16027-16045`).
    - tdesktop: `ptsInit(dialog.pts)`, `applyDialogs`, range difference (`api_updates.cpp:403-424`).
    - Web K: drops the state and sends a local `updateChannelReload`, which re-fetches the dialog and history (`apiUpdatesManager.ts:451-457`, `appMessagesManager.ts:10849-10863`).
    - iOS: needs `dialog.pts`, otherwise `assertionFailure` (`AccountStateManagementUtils.swift:3626-3700`).
- **Force flag**:
  - Android sets `force` except during short polls (`:15895`).
  - tdesktop sets `force` except for short-poll requests where it is not waiting for skipped updates (`api_updates.cpp:737-742`).
  - iOS always sends flags=0.
- **Channel state after reconnect**: none of the clients calls `getChannelDifference` for every channel on start. They rely on stored pts and on `updateChannelTooLong` from `getDifference`.

**Minimal server set:** `channels.createChannel`, `channels.getFullChannel`, `channels.getParticipant(self)`, `channels.getParticipants`, `channels.getMessages`, `messages.getHistory` (channelMessages+pts), `messages.getPeerDialogs`/`getDialogs` (dialog.pts), `updates.getChannelDifference` (all 3 constructors), `updateChannelTooLong` in `getDifference`, `channels.readHistory`, `channels.readMessageContents`, `channels.deleteMessages`, `messages.getMessagesViews`, `messages.send*`/`editMessage` with a per-channel pts, `updateChannel`, `channels.joinChannel`, `channels.leaveChannel`, `channels.inviteToChannel`.

---

## 2. Error handling, retries and possible loops

### Error handling per client
- **CHANNEL_PRIVATE / CHANNEL_PUBLIC_GROUP_NA / USER_BANNED_IN_CHANNEL**:
  - Android posts `chatInfoCantLoad` from `getFullChannel` and from `getChannelDifference` (`MessagesController.java:16067-16092`). `ChatActivity` then shows a "private / banned" dialog and stops loading (`ui/ChatActivity.java:22186-22225`). No retry.
  - Web K rewrites the chat to `channelForbidden` and sends `updateChannel` (`appMessagesManager.ts:12545-12565`, `appProfileManager.ts:782-830`).
  - tdesktop join shows a "not accessible" toast; `CHANNELS_TOO_MUCH` opens the limit box (`apiwrap.cpp:1901-1930`).
- **iOS `getChannelDifference`**: maps CHANNEL_PRIVATE and CHANNEL_INVALID to "no difference". **Any other error goes to `retryRequest`**, which retries forever with backoff from 0.2 s up to 5 s (`AccountStateManagementUtils.swift:3448-3458`, `Network/Network.swift:1240-1243`). `getFullChannel` errors become nil (`UpdateCachedPeerData.swift:657-663`).
- **tdesktop `getChannelDifference` failure**: retries with backoff 1, 2, 4 … 64 s (`api_updates.cpp:464-472`, `:576-590`).
- **Android `getChannelDifference` failure**: no retry. The gap is retried on the next update (`:16063-16075`).
- **Android tgnet transport** (`jni/tgnet/ConnectionsManager.cpp:1368-1385`): requests sent without `RequestFlagFailOnServerErrors` are re-sent on **500 or a negative code** with growing delay, capped at 10 s, forever. Flood waits are honoured.
- **Web K** (`appManagers/apiManager.ts:785-797`):
  - a **500** is retried with backoff up to 60 s and no limit unless `stopTime` is set;
  - an error whose message does not match `^[A-Z_0-9]+` becomes `UNKNOWN` and is **retried every 1 s forever**. A code of 0 or below becomes 500 (`mtproto/networker.ts:1708-1719`).
- **`ERR_ENTERPRISE_IS_BLOCKED`** is code 400 (`third_party/teamgram-proto/mtproto/rpc_error_codes.go:341`). It is not retried by the Android or Web K transport layers and usually shows as a generic failure. **Exception: iOS `getChannelDifference`, which retries it forever (above).**

### Things that loop or break if the server answers wrong
1. **`final` never set** (or not final while pts does not advance): Android, tdesktop and Web K re-request immediately with no delay (`MessagesController.java:16054`, `api_updates.cpp:433-437`, `apiUpdatesManager.ts:485-487`). This is a tight request loop.
2. **`timeout = 0` sent explicitly**: iOS `keepPollingChannel` waits `Double(timeout ?? 30)`, then `restart` (`:3159-3163`, `:3220-3224`). With 0 it re-polls continuously while the channel is open. This is inferred from the code, not observed. tdesktop and Android fall back to 1 s. Send a real timeout or omit the flag. The value Telegram uses is UNVERIFIED.
3. **`getChannelDifference` returning any error other than CHANNEL_PRIVATE/INVALID** (for example the current stub): iOS retries forever. This call runs inside the account update pipeline (`combineLatest(pollChannelSignals)`, `:2074-2085`), so incoming updates could stall. That consequence is inferred, not observed.
4. **pts 0** in `dialog` or `channelMessages`:
   - Web K `addChannelState` throws (`apiUpdatesManager.ts:531-536`);
   - tdesktop never inits pts and so never fetches the difference (`api_updates.cpp:726`);
   - Android treats 0 as "unknown" and keeps asking `getPeerDialogs` (`MessagesController.java:17417-17428`).
5. **A pts that does not match the `pts_count` arithmetic** (shared or global pts used for a channel): every update becomes a gap, and each one triggers a channel difference.
6. **Android crash**: an empty list from `messages.getExportedChatInvites(limit=1)` crashes with `invites.get(0)` (`ChannelCreateActivity.java:940-944`). The server must always return the permanent link to the creator.
7. **iOS join**: after `joinChannel` or `importChatInvite`, iOS calls `channels.getParticipant(self)`. If that fails it reports `.generic` even though the user joined (`JoinChannel.swift:70-80`).
8. **A gRPC Unimplemented (500) or a lowercase or non-standard error text** gives endless retries on Web K and Android (above). Keep unserved methods on `ErrEnterpriseIsBlocked` (400), as the project rules require.

---

## 3. Features gated by config and limits

**Values from `help.getConfig`**, not appConfig:
- `megagroup_size_max` and `chat_size_max`: Android `MessagesController.java:5686-5687` (defaults 10000 and 200 at `:1532-1533`); tdesktop `mtproto/mtproto_config.cpp:243-244`.
- `channels_read_media_period`: parsed by tdesktop (`mtproto_config.cpp:258`). In Android it is only parsed (`tgnet/TLRPC.java:37567`); I found no use of it.

**appConfig keys** (the client defaults read here; Telegram's own values are UNVERIFIED):

| Key | Default in client code | Used for |
|---|---|---|
| `channels_limit_default` / `_premium` (iOS reads `channels_limit` + suffix) | Android 500 (`:1592`); iOS 500 (`UserLimitsConfiguration.swift:41`, `:163`) | joined-channels limit (`CHANNELS_TOO_MUCH` box) |
| `channels_public_limit_default` / `_premium` | Android 10 (`:1608`); iOS 10 (`:42`, `:164`) | public usernames |
| `chat_read_mark_size_threshold` | Android 100 (`:1581`); tdesktop 50 (`api/api_who_reacted.cpp:775-777`) | "seen by" in supergroups only, not broadcast channels (Android `ChatActivity.java:30259`) |
| `chat_read_mark_expire_period` | 7·86400 (Android `:1582`; tdesktop `:769-771`) | "seen by" |
| `reactions_uniq_max` | 11 (Android `:1729`; tdesktop `data/data_peer_values.cpp:86-87`) | reactions on a post |
| `reactions_user_max_default` | 1 (Android `:1616`; iOS `:173`) | reactions per user |
| `reactions_in_chat_max` | 3 (Android `:1618`) | allowed-reaction settings |
| `forum_upgrade_participants_min` | 200 (Android `:1629`) | forum toggle |
| `hidden_members_group_size_min` | 100 (Android `:1633`) | hide members |
| `telegram_antispam_group_size_min` / `telegram_antispam_user_id` | 100 (Android `:1632`, `:3717`) | anti-spam toggle |
| `caption_length_limit_*`, `about_length_limit_*` | iOS 1024 / 70 (`:47-49`) | post captions, channel description |
| boost levels: `channel_color_level_min`, `channel_wallpaper_level_min`, `group_emoji_stickers_level_min`, `channel_autotranslation_level_min`, etc. | Android `:4175-4318` | customisation behind boosts |

Web K reads `chat_read_mark_*`, `forum_upgrade_participants_min`, `telegram_antispam_group_size_min` and `reactions_default` the same way, plus `getLimit('reactions'|'caption'|'recommendedChannels')` (grep over `src/`).

---

## 4. Order of user-visible features and what each needs

1. **Create a channel or supergroup** (§1):
   - `createChannel` returning Updates with `channel` in chats and `updateNewChannelMessage(messageActionChannelCreate)` at pts 1;
   - `updateChannel`;
   - `getFullChannel`;
   - `getExportedChatInvites` with the permanent invite;
   - `checkUsername` (not blocked);
   - `inviteToChannel`;
   - `dialog.pts` in `getDialogs` and `getPeerDialogs`.
2. **Open, read, send and receive** (§1):
   - `getHistory` returning `channelMessages`;
   - per-channel pts on send/edit/delete;
   - the three `getChannelDifference` constructors with a sane `timeout`;
   - `updateChannelTooLong` in `getDifference`;
   - `readHistory` with `updateReadChannelInbox` / `updateReadChannelOutbox`;
   - `channels.getMessages`.
   - Without this, iOS loops on difference errors.
3. **Join by link or username, leave**:
   - `messages.checkChatInvite`, `messages.importChatInvite` / `channels.joinChannel` returning `messages.ChatInviteJoinResult` (Web K `appChatsManager.ts:681-688`, `appChatInvitesManager.ts:93-99`);
   - `channels.getParticipant(self)` (iOS);
   - `leaveChannel` with `updateChannel`;
   - errors `INVITE_REQUEST_SENT`, `USERS_TOO_MUCH`, `CHANNELS_TOO_MUCH`, `CHANNEL_PRIVATE` (iOS `JoinChannel.swift:36-41`, tdesktop `apiwrap.cpp:1901-1930`);
   - for invite management: `exportChatInvite`, `editExportedChatInvite`, `getChatInviteImporters`, `hideChatJoinRequest` (iOS `InvitationLinks.swift`).
4. **Members and admins**:
   - `channels.getParticipants` (filters recent, admins, kicked, banned, search);
   - `channels.editAdmin` (`PeerAdmins.swift`) emitting `updateChannelParticipant`, and the chat's `admin_rights` in `Chat`;
   - `channelFull.admins_count`, plus `participants_count` and the `can_view_participants` flag (iOS `UpdateCachedPeerData.swift:694-700`).
5. **Bans and default rights**:
   - `channels.editBanned`, `messages.editChatDefaultBannedRights` (`ChannelBlacklist.swift`);
   - `channelForbidden` / `kicked` handling: Android deletes the dialog on `updateChannel` (`MessagesController.java:19327-19332`);
   - `channels.togglePreHistoryHidden` (`ChannelHistoryAvailabilitySettings.swift`).
6. **Slow mode**:
   - `channels.toggleSlowMode` (`SlowMode.swift`);
   - `slowmode_seconds` and `slowmode_next_send_date` in channelFull;
   - `SLOWMODE_WAIT_X` as a 420 error. Android parses it (`ConnectionsManager.cpp:1399-1420`). Web K does not auto-retry it (`apiManager.ts:744`).
7. **Views and forwards on broadcast posts**:
   - `messages.getMessagesViews(increment)` (Android every 5 s; iOS; Web K);
   - `updateChannelMessageViews` (Web K `appMessagesManager.ts:10865-10875`);
   - `views` and `forwards` fields on posts.
8. **Signatures**:
   - `channels.toggleSignatures` (`ToggleChannelSignatures.swift`);
   - `post_author` / `from_id` on posts;
   - Web K reloads the channel when profile signatures change (`appChatsManager.ts:307-313`).
9. **Discussion group (comments)**:
   - `channels.getGroupsForDiscussion`, `setDiscussionGroup` (`ManageChannelDiscussionGroup.swift`);
   - `linked_chat_id`;
   - `messages.getDiscussionMessage`, `getReplies`, `readDiscussion`;
   - `replies` in the post and in `getMessagesViews` (Android `:10255-10260`).
10. **Reactions in channels**:
    - `messages.setChatAvailableReactions`, `sendReaction`, `getMessagesReactions`, `getMessageReactionsList`;
    - `available_reactions` / `reactions_limit` in channelFull;
    - `updateMessageReactions`;
    - limits from §3.

Later and optional: admin log (`channels.getAdminLog`), stats, boosts, sponsored messages (already served), forums, `toggleJoinToSend` / `toggleJoinRequest` (already served per api-coverage). This order is my proposal from the dependencies above; the clients do not prescribe it.

