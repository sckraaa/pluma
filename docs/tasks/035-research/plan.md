I spot-checked 10 claims from the two reports against the files, read-only. 8 hold and 2 need corrections. The plan for stage 1 follows.

# 1. Spot-checks

| # | Claim | Result | Source I read |
|---|---|---|---|
| 1 | `RPCChannels` and `RPCMessageThreads` routes are commented out in all three session configs | **Holds.** | `teamgramd/etc2/session.yaml:82,104`, `teamgramd/etc/session.yaml:82,104`, `app/interface/session/etc/session.yaml:63,84` |
| 2 | `getDifference` drops channel updates | **Holds.** The comment reads "Channels have their own pts". | `app/service/biz/updates/internal/core/merge_updates_helper.go:148-153` |
| 3 | `msg.sendMessageV2` blocks `PEER_CHANNEL` | **Holds.** It returns `ErrEnterpriseIsBlocked` after the "License key" log. | `app/messenger/msg/msg/internal/core/msg.sendMessageV2_handler.go:51-53` |
| 4 | `getPeerDialogs` skips channels | **Holds, wrong path.** The file is `app/bff/dialogs/...`, not `app/bff/messages/...`. | `app/bff/dialogs/internal/core/messages.getPeerDialogs_handler.go:93-95` |
| 5 | Invite hashes that start with a letter are treated as channel links | **Holds.** The hash must also be 20 characters long. | `app/service/biz/chat/chat/chat_invite_helper.go:18-30` |
| 6 | `ERR_ENTERPRISE_IS_BLOCKED` is a 400 | **Holds.** | `third_party/teamgram-proto/mtproto/rpc_error_codes.go:341` |
| 7 | `chat.migratedToChannel` swallows an error | **Holds.** The return value of `UpdateStateByChatIdTx` is never checked. | `app/service/biz/chat/internal/core/chat.migratedToChannel_handler.go:42` |
| 8 | iOS `pollChannel` details | **Holds.** limit 100 (2 in DEBUG), `pts: max(pollPts,1)`, flags 0, `CHANNEL_PRIVATE`/`CHANNEL_INVALID` become nil, any other error goes to `retryRequest`. Local tree is 756315d7. | `~/aaaaaaa/Telegram-iOS/submodules/TelegramCore/Sources/State/AccountStateManagementUtils.swift:3413-3440` |
| 9 | iOS `migrateChat` handling | **Holds.** 5 s timeout, `CHANNELS_TOO_MUCH` mapped, new peer id taken from the first message whose peer is not the old chat. | `.../TelegramEngine/Peers/ConvertGroupToSupergroup.swift:13-29` |
| 10 | iOS waits `timeout ?? 30` between polls | **Holds.** | `AccountStateManagementUtils.swift:3143-3149` |
| 11 | Android crashes on an empty invite list | **Holds.** It calls `invites.invites.get(0)` on `messages.getExportedChatInvites(limit=1)`. | `~/aaaaaaa/Telegram-Android/TMessagesProj/src/main/java/org/telegram/ui/ChannelCreateActivity.java:940-943` |
| 12 | Web K pin is 346844bf | **Holds.** It is layer 228, version 2.2 (676). | `~/pluma-web/upstream.env` |

Two more facts:
- **`api-coverage.md` lines checked:** 280, 339-340, 651 and 1016 (`checkUsername`, `toggleJoinRequest`, `toggleJoinToSend`, `migrateChat`, `getChannelDifference`) are all marked `blocked`.
- **Pins for iOS, Android and tdesktop:** I did not find their `upstream.env` anywhere on this machine; only Web K has one. `clients/*.md` give no pin either. So whether the local iOS tree (756315d7), the iOS commit the clients report read (6ad963e5), the Android tree (20091c78, 12.6.4) and tdesktop v7.0.9 match the releases Pluma builds is still unchecked.

# 2. Contradictions and unverified claims

## Contradictions between the reports

- **C1. Default channel-difference timeout.** The server report designs for "1 s default" (core.telegram.org). iOS falls back to **30 s** when `timeout` is absent (`AccountStateManagementUtils.swift:3143-3149`). tdesktop and Android fall back to 1 s, per the clients report.
  - Resolution: the server always sends an explicit timeout greater than 0 and never omits the flag. The value comes from the T0 probe.
- **C2. `toggleJoinRequest` and `toggleJoinToSend`.** The clients report calls them "already served per api-coverage". `api-coverage.md:339-340` says `blocked`, so the clients report is wrong. Sponsored messages are "served" only as a canned empty answer.
- **C3. iOS line numbers.** The server report cites `pollChannel` at :3413 (756315d7). The clients report cites :3433 (6ad963e5). These are two different iOS commits, and neither has been checked against the pin.
- **C4. How iOS catches up after `getDifference`.** The server report's main path is "updateChannelTooLong → pollChannel". The clients report says that after `getDifference` iOS resets channels with `messages.getPeerDialogs` (`:2042-2070`, `:3226-3240`), not with `getChannelDifference`. Consequence: `getPeerDialogs` with a correct `dialog.pts`, read state and top message is on the critical path as early as T3/T5. I did not read those lines myself; check them in T0.
- **C5. Endless iOS retries on any `getChannelDifference` error.** The current stub's 400 is retried forever by iOS. The project rule "keep unserved methods on `ErrEnterpriseIsBlocked`" is not enough for this one method. It has to be implemented properly before any channel can exist (T5 before any client can create a channel in production).

## Unverified claims (must be resolved in T0 before code relies on them)

- **U1.** Whether join, leave, kick and `updateReadChannelInbox` carry a common pts or seq for the user's own devices.
- **U2.** The criterion Telegram uses to emit `updateChannelTooLong` in `getDifference`.
- **U3.** Error names: `SLOWMODE_WAIT_X`, the error for sending to a migrated chat (CHAT_MIGRATED or other), `RANDOM_ID_DUPLICATE`, `USER_BANNED_IN_CHANNEL`, `CHAT_SEND_MEDIA_FORBIDDEN`-style names.
- **U4.** The admin-rights mapping when a basic group migrates, and whether admins are exempt from slow mode.
- **U5.** How `read_outbox_max_id` is defined for supergroups.
- **U6.** Which basic-group settings make clients start an auto-migration.
- **U7.** All tdesktop details beyond the lines cited. The tdesktop create flow was explicitly not read.
- **U8.** Whether the `messages.migrateChat` result shape matches what Android, tdesktop and Web K parse.
- **U9.** Telegram's real appConfig values (`channels_limit_default` and others). Only the client defaults were read.
- **U10.** The clients report's lines I did not verify myself: Android `MessagesController` lines, Web K `apiUpdatesManager` lines, iOS `JoinChannel.swift:70-80`, iOS `UpdateCachedPeerData.swift`.

# 3. Stage 1 plan, in dependency order

Rules for every task:
- Task numbers continue from 034.
- One branch per task.
- Integration tests through `pkg/testinfra`; e2e through `pkg/mtclient` in `test/e2e`.
- Heavy runs (stands, `-race`, emulators) go to CachyOS.
- All user-visible texts are localization keys.
- Every unserved `channels.*` method returns `ErrEnterpriseIsBlocked` (400).
- "Visible in the clients" means checked by hand in iOS, Android, Desktop and Web, with screenshots in the brief.

### 034. Research and ADR "Ящик канала" (2 days, no code)
- **Scope:**
  - Web K probe against real Telegram for U1-U6 and U8.
  - Read the pinned Android, tdesktop and iOS sources for C3, C4, U7 and U10, after first locating the pins (only Web K's is found today).
  - Record the timeout Telegram returns (C1).
- **Output, the ADR:**
  - counters in a `channels` row;
  - the `channel_pts_updates` journal and its retention (100000 pts);
  - fan-out with no per-user journal;
  - unread derived on read;
  - Kafka key = channel id (a new key type, which changes ADR 0012's keying by recipient);
  - how `updateChannelTooLong` is chosen;
  - which updates go through the common box.
- **Also:** conformance reference JSONs (ADR 0018).
- **Accepted when:** the ADR is accepted and every Ux item is closed or explicitly deferred.

### 035. Schema and `pkg/channelbox` (3 days)
- **Data:** `migrate-YYYYMMDD-channels.sql`, idempotent:
  - `channels`;
  - `channel_messages`, with PK (channel_id, id) and UNIQUE (channel_id, sender_user_id, random_id);
  - `channel_pts_updates`, with UNIQUE (channel_id, pts);
  - `channel_participants`.
- **Code:** `Run(ctx, db, channelId, fn)`, which takes `SELECT … FOR UPDATE`, plus `NextMessageId`, `AddPtsUpdate` and `Emit` through `mq_outbox`. It mirrors `pkg/userbox` and ADR 0011 И1-И4.
- **Tests:**
  - fake-DB unit tests;
  - MySQL integration: concurrent Run gives gapless pts and ids, a rollback burns nothing;
  - a test that fails without the lock.
- **Visible in clients:** nothing.

### 036. biz `channel`, bff `channels` skeleton, routing (2 days)
- **Scope:**
  - The `app/service/biz/channel` reads.
  - The `RPCChannels` route in the three session.yaml files; registration in `app/bff/bff/internal/server/server.go`.
  - All 74 `channels.*` methods explicitly answer 400 until they are served.
  - The per-viewer constructors: `channel` (min, left, admin_rights, banned_rights) and `channelForbidden`.
  - A channel source for the ADR 0021 resolver.
- **Tests:** unit tests for the constructor table; re-run apicoverage.
- **Accepted when:** every `channels.*` method answers 400 (none answers 500).

### 037. Create, getChannels, getFullChannel, dialogs (3 days)
- **Methods:**
  - `channels.createChannel` (broadcast and megagroup). Its result carries `channel` in chats and `updateNewChannelMessage(messageActionChannelCreate)` at pts 1, as iOS `ChannelCreation.swift` needs and Web K `chats[0]` needs.
  - `getChannels`, `getFullChannel` (pts and counts), `getParticipant(self)`.
  - The channel branch in `getDialogs`, `getPeerDialogs` (`app/bff/dialogs/.../messages.getPeerDialogs_handler.go:93`) and `getMyDialogsData`, with a non-zero `dialog.pts`.
  - `messages.getExportedChatInvites` must return the permanent link to the creator (Android crash, `ChannelCreateActivity.java:943`).
  - `channels.checkUsername` unblocked with a minimal real check: the create screen probes it.
- **Data:** creator participant, the creator's dialog via `userbox.Run`, `updateChannel`.
- **Tests:** integration; e2e create → getFullChannel → getDialogs shows pts > 0.
- **Visible in clients:** a channel and a supergroup can be created and opened in all four clients; Android shows the invite link without crashing.

### 038. Send, history, `channels.getMessages` (3 days)
- **Methods:**
  - `PEER_CHANNEL` in `msg.sendMessageV2` (`:51`), `sendMedia` and `sendMultiMedia`;
  - `updateMessageID` plus `updateNewChannelMessage(pts, pts_count=1)`;
  - a durable `random_id` uniqueness check;
  - `messages.getHistory`, which returns `messages.channelMessages` with pts;
  - `channels.getMessages`, `messages.search`;
  - push to online members through sync, keyed by channel.
- **Tests:**
  - integration: concurrent senders; a `random_id` retry after the Redis TTL is not duplicated;
  - e2e: two clients see consecutive pts.
- **Visible in clients:** messages arrive live, with no "loading" loops.

### 039. `getChannelDifference` and `updateChannelTooLong` (3 days)
- **Methods:**
  - bff and biz `getChannelDifference`: fold, slice without final, `channelDifferenceEmpty` with an explicit timeout > 0, `tooLong` with a full `dialog` (pts, read ids, counters) plus messages, chats and users;
  - `force`; errors `CHANNEL_PRIVATE` and `CHANNEL_INVALID`;
  - the `available_min_id` filter;
  - `updateChannelTooLong` in `getDifference` per the 034 ADR, with channels resolved in difference chats.
- **Tests:**
  - fold unit tests;
  - integration at limits 2 and 100 and for tooLong after a trim;
  - e2e: offline → 150 posts → reconnect reaches `final`;
  - a test that fails against the current stub.
- **Visible in clients:** after the app is killed and reopened, missed posts appear in all four clients; network logs show no tight loops (C1, C5).
- **Gate:** no channel creation in production before 039 lands (C5).

### 040. Membership and invites (3 days)
- **Methods:**
  - `joinChannel`, `leaveChannel`, `inviteToChannel`, `getParticipants` (all filters), `getLeftChannels`;
  - channel branches of `exportChatInvite`, `checkChatInvite`, `importChatInvite`, `editExportedChatInvite`, `getChatInviteImporters`, `hideChatJoinRequest`;
  - `toggleJoinRequest` and `toggleJoinToSend` made real;
  - service messages; `hidden_prehistory` through `available_min_id`.
- **Tests:**
  - integration: concurrent join and leave keep the counts right;
  - e2e: join by link; leave gives `left` or `channelForbidden`.
- **Visible in clients:** join by link or username; after joining, iOS does not show `.generic` (`getParticipant(self)` works); leaving removes the dialog on Android.

### 041. Rights: admin, ban, default rights (3 days)
- **Methods:** `editAdmin`, `editBanned`, `editCreator`, `getFutureCreatorAfterLeave`, and the channel branch of `editChatDefaultBannedRights`.
- **Code:** a pure `channelperm.Can` called from every write handler; `channelForbidden` with `until_date`; `updateChannelParticipant`.
- **Tests:** a unit table over every rights bit; e2e (a banned member cannot send media; an admin without `pin_messages` cannot pin).
- **Visible in clients:** the admin and banned-users screens work.

### 042. Edit, delete, pin, read (3 days)
- **Methods:**
  - `PEER_CHANNEL` in `msg.editMessageV2` (`:41`), `getMessageEditData`;
  - `channels.deleteMessages` (`affectedMessages` with the channel's pts), `deleteHistory`, `deleteParticipantHistory`;
  - `updatePinnedMessage`, `unpinAllMessages`;
  - `channels.readHistory` (`updateReadChannelInbox` to own devices, `updateReadChannelOutbox` to others), `readMessageContents`;
  - `readMentions` and `getUnreadMentions` channel branches.
- **Tests:** integration fold of edit then delete; e2e for a read on device A reaching device B.
- **Visible in clients:** edits and deletes are seen by other members; read ticks and unread counters stay consistent across devices.

### 043. Metadata and public usernames (2 days)
- **Methods:**
  - `editTitle`, `editPhoto`, `toggleSignatures`, `togglePreHistoryHidden`, `toggleParticipantsHidden`;
  - `toggleNoForwards`, `setChatAvailableReactions` and `sendReaction` in channels;
  - `deleteChannel`;
  - `updateUsername`, `toggleUsername`, `reorderUsernames`, `deactivateAllUsernames`, `getAdminedPublicChannels`;
  - `contacts.resolveUsername` for channels.
- **Tests:** username uniqueness; e2e for each visible change (service message plus `updateChannel`).

### 044. Slow mode (1 day)
- **Methods:** `toggleSlowMode`, the send-path check, `slowmode_next_send_date`.
- **Error:** a 420 with the name from 034.
- **Tests:** unit tests of the time math; e2e.
- **Visible in clients:** the countdown shows in the input field.

### 045. Broadcast specifics (2 days)
- **Methods:**
  - `getMessagesViews(increment)`, deduplicated through `channel_message_views`;
  - `updateChannelMessageViews`;
  - `post_author` and signatures;
  - `exportMessageLink`.
- **Tests:** views grow once per user; a subscriber cannot post.

### 046. Basic group → supergroup migration (3 days)
- **Methods:** `messages.migrateChat` (`app/bff/chats/.../messages.migrateChat_handler.go:27`), plus the auto-migration triggers found in 034.
- **Code:**
  - fix `chat.migratedToChannel:42`;
  - steps ordered with the channel and the caller's result first, members reached through outbox events;
  - idempotent per `chat_id`;
  - old-chat sends answer the migrate error;
  - cross-links between `getFullChat` and `getFullChannel`;
  - invite links moved.
- **Tests:**
  - integration: a crash between steps, then a rerun converges with no second channel;
  - e2e: the result contains `chat(migrated_to)`, `channel` and the `channelMigrateFrom` message within 5 s (`ConvertGroupToSupergroup.swift:20-29`).
- **Visible in clients:** "Convert to supergroup" works in all four; members keep the merged history.

### 047. Linked discussion and threads (3 days)
- **Methods:** `setDiscussionGroup`, `getGroupsForDiscussion`, auto-forward of posts, `getDiscussionMessage`, `getReplies`, `readDiscussion`, `replies` counters, and the `RPCMessageThreads` route.
- **Visible in clients:** a comments button appears under posts and its counter grows.

### 048. Conformance and the client pass (2 days)
- **Scope:** ADR 0018 scenarios for create, send, difference, migrate and rights; a manual pass in iOS, Android, Desktop (CachyOS) and Web.
- **Accepted when:** the stage 1 exit criterion in ROADMAP is met.

**Order and size.**
- 034 → 035 → 036 → 037 → 038 → 039 are strictly sequential.
- After 039, two parallel tracks: {040 → 041 → 044} and {042, 043, 045}.
- 046 needs 040 and 041; 047 needs 045 and 040; 048 comes last.
- About 36 working days without forums and the admin log.

**Deferred (owner's call, §4):**
- forums and topics, about 3 days;
- `getAdminLog`, a real `reportSpam`, `toggleAntiSpam` and `convertToGigagroup`, about 2 days;
- sponsored messages, boosts, paid messages, stories, recommendations, autotranslation, emoji status and sticker sets keep returning 400 (ADR 0016).

# 4. Decisions for the owner

1. Are forums and the admin log part of stage 1 or stage 2?
2. Retention of the channel journal: 100000 pts only, or also a time limit?
3. The value of `channels_limit_default` and the public-username limit (client default 500/10, `UserLimitsConfiguration.swift:41-42`), and whether creating public channels is allowed in a closed installation.
4. Do we accept the production gate "no channels in clients before 039"? The other option is to hide the create entry in the client forks until then.
5. Whether to add the missing `upstream.env` pins for iOS, Android and tdesktop to the repo, so that 034 has a fixed source to read.

