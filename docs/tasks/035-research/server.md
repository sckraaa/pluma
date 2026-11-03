# Stage 1 (channels and supergroups): current state, target model, task breakdown

Everything below comes from files I read at master 76b00b0d. Nothing was run. A client or Telegram claim that has no file or URL next to it is marked UNVERIFIED.

**Client sources.** The only client tree on this machine is `~/aaaaaaa/Telegram-iOS` (commit 756315d7, 2026-04-25). I did not check that this is the release pinned in `upstream.env`, so treat the iOS citations as "probably current" until someone checks the pin. There are no tdesktop, DrKLO or tweb sources locally (`~/pluma-work/reference` holds only an APK and a simulator zip). Claims about those three clients are UNVERIFIED and must be read in their sources before any brief relies on them.

---

## 1. What exists today

### 1.1 Routing and handlers
- **No bff module for channels.** Every session config has the route commented out: `#"/mtproto.RPCChannels": "bff.bff"` (`teamgramd/etc2/session.yaml:82`, `teamgramd/etc/session.yaml:82`, `app/interface/session/etc/session.yaml:63`). `#"/mtproto.RPCMessageThreads"` is commented out too (`teamgramd/etc2/session.yaml`, the block around line 100). That service carries `messages.getReplies`, `getDiscussionMessage` and `readDiscussion`. The project rules say an unrouted service gets `ERR_ENTERPRISE_IS_BLOCKED` or a fake from `app/bff/bff/client/fake_rpc_result.go`.
- **Scattered `channels.*` stubs** sit in other bff modules (chats, messages, usernames, chatinvites, sponsoredmessages, savedmessagedialogs, userchannelprofiles). Each returns `ErrEnterpriseIsBlocked` (see the table in 1.5).
- **The one `ok` channels method is a canned empty answer.** `channels.getSponsoredMessages` (`app/bff/sponsoredmessages/internal/core/channels.getSponsoredMessages_handler.go:27`) returns an empty `messages.sponsoredMessages` ("disable SponsoredMessages"). It is not a real channel feature.
- **`updates.getChannelDifference` is stubbed twice:**
  - bff: `app/bff/updates/internal/core/updates.getChannelDifference_handler.go:27` returns `ErrEnterpriseIsBlocked`;
  - biz: `app/service/biz/updates/internal/core/updates.getChannelDifferenceV2_handler.go` logs "blocked" and returns a non-final empty difference that echoes `in.Pts`.
- **`messages.migrateChat`** (`app/bff/chats/internal/core/messages.migrateChat_handler.go:27`) returns `ErrEnterpriseIsBlocked`.
- **Half-built migration helper.** biz chat has `chat.migratedToChannel` (`app/service/biz/chat/internal/core/chat.migratedToChannel_handler.go`). It sets `chats.migrated_to_id/access_hash` and sets participants to `ChatMemberStateMigrated` in one transaction. Nothing outside the generated client or server stubs calls it.
- **`PEER_CHANNEL` branches in the message paths** are blocked or skipped:
  - `msg.sendMessageV2`: `ErrEnterpriseIsBlocked` (`app/messenger/msg/msg/internal/core/msg.sendMessageV2_handler.go:51-53`);
  - `msg.editMessageV2`: the same (`msg.editMessageV2_handler.go:41-43`);
  - `msg.deleteMessages`: `ErrPeerIdInvalid` (`msg.deleteMessages_handler.go:32`);
  - `messages.forwardMessages`: blocked (`app/bff/messages/internal/core/messages.forwardMessages_handler.go:76,169`);
  - `messages.getHistory`: commented-out `ChannelClient` code (`messages.getHistory_handler.go:60-64`);
  - `messages.getPeerDialogs`: `continue` with a "blocked" log (`messages.getPeerDialogs_handler.go:93-95`);
  - `messages.editChatDefaultBannedRights`: blocked (`messages.editChatDefaultBannedRights_handler.go:60-63`).
  
  About 45 more files only mention `PEER_CHANNEL` in switches: notify settings, drafts, setTyping, usernames, saved history, `biz/message/internal/dao/history.go`, and others.
- **Leftover channel hooks in biz:**
  - biz dialog already lists `PEER_CHANNEL` dialogs (`app/service/biz/dialog/internal/core/dialog.getMyDialogsData_handler.go:102-130`, cache key `GetChannelsCacheKey`);
  - `dialog.getChannelMessageReadParticipants` answers from `dialogs.read_inbox_max_id` (`dialog.getChannelMessageReadParticipants_handler.go`);
  - invite links whose hash starts with a letter are classified as channel links (`app/service/biz/chat/chat/chat_invite_helper.go:23-24`).
- **`getDifference` drops channel updates.** `merge_updates_helper.go:148-153` ignores `updateNewChannelMessage`, `updateDeleteChannelMessages`, `updateEditChannelMessage`, `updateChannelWebPage` and `updatePinnedChannelMessages` ("Channels have their own pts"). ADR 0021 §7 says channels are not resolved in difference users/chats.
- **idgen keys.** `channel_pts_updates_ngen_<id>` existed in `app/service/idgen/client/vars.go` (audit 01 l.27). ADR 0011 (l.230) has the unused `channel_pts_*` and `channel_message_box_*` keys removed. Future channel counters must be a MySQL row driven by the same primitive (ADR 0011 l.230, l.411).

### 1.2 SQL (teamgramd/deploy/sql)
- There are no channel tables. The only channel mentions are `users.personal_channel_id` (`migrate-20241010.sql`) and the column after it in `migrate-20241123.sql`.
- Existing tables that can be reused, from `1_teamgram.sql`:

| Table | Lines | Notes |
|---|---|---|
| `chats` | 172-189 | already has `migrated_to_id`, `migrated_to_access_hash`, `default_banned_rights`, `deactivated`, `version` |
| `chat_participants` | 240-257 | `admin_rights int`, `state` |
| `chat_invites` | 197-215 | has `migrated_to_id` |
| `dialogs` | 286-308 | generic `peer_type`/`peer_id`, with `top_message`, `read_inbox_max_id`, `read_outbox_max_id`, `unread_count`, `unread_mentions_count`, `pinned_msg_id`; it has no `pts` column |
| `messages` | 409-430 | per-user copy keyed `(user_id, user_message_box_id)` |
| `user_pts_updates` | 743-752 | per-user journal |

- Since 0011 there is also `user_state` (ADR 0011 §1) and `mq_outbox` (ADR 0012 §1).

### 1.3 Storage and update model (what channels must sit beside)
- **Common box only.** Private chats and basic groups use per-user copies, a per-user `pts` in `user_state`, and the journal `user_pts_updates`. Every box change is one transaction, `userbox.Run(ctx, db, userId, fn)` (`pkg/userbox/box.go:50` `type Box`, ADR 0011 §2). Post-commit side effects go through `Box.Emit` into `mq_outbox`, then relay → Kafka → exactly-once consumer `pkg/mqconsumer` (ADR 0012 §§1-5).
- **Basic-group fan-out** writes one copy into each member's box (`msg.sendMessageV2_handler.go:275` `sendChatMessages`; inbox consumer `app/messenger/msg/inbox/internal/server/mq/mq.go:48`).
- **getDifference** folds the per-user journal (ADR 0019 §§1-5). `seq` updates are journaled per account (ADR 0020). `seq` updates to other users go through a recipient inbox event (ADR 0021). Updates to "all sessions except one" are covered by ADR 0022.

### 1.4 Tests available
- `pkg/testinfra` (MySQL, Redis, Kafka) and `pkg/mtclient` exist.
- `test/e2e` has difference, seq, deletion, edit, forward, replies, mentions and other tests, but none for channels.
- `pkg/userbox` already has integration tests (`maint_integration_test.go`, `outbox_integration_test.go`) to copy from.

### 1.5 Layer-228 method status (docs/project/api-coverage.md)
**channels.\*** — 74 methods: ok 1, blocked 15, fake 1, missing 57 (api-coverage.md, summary row, lines 279-352).

- **ok:** `getSponsoredMessages` (empty canned answer).
- **fake:** `reportSpam` (`fake_rpc_result.go:47`).
- **blocked:**
  - `checkSearchPostsFlood`, `searchPosts`, `getSendAs` (bff/messages);
  - `checkUsername`, `updateUsername` (bff/usernames);
  - `clickSponsoredMessage`, `reportSponsoredMessage`, `restrictSponsoredMessages`, `viewSponsoredMessage` (bff/sponsoredmessages);
  - `convertToGigagroup`, `setEmojiStickers` (bff/chats);
  - `getMessageAuthor` (bff/savedmessagedialogs);
  - `setMainProfileTab` (bff/userchannelprofiles);
  - `toggleJoinRequest`, `toggleJoinToSend` (bff/chatinvites).
- **missing:**
  - `createChannel`, `createForumTopic`, `deactivateAllUsernames`, `deleteChannel`;
  - `deleteHistory` (2 variants), `deleteMessages`, `deleteParticipantHistory`, `deleteTopicHistory`;
  - `editAdmin`, `editBanned`, `editCreator`, `editForumTopic`, `editLocation`, `editPhoto`, `editTitle`;
  - `exportMessageLink`, `getAdminLog`, `getAdminedPublicChannels`, `getChannelRecommendations`, `getChannels`;
  - `getForumTopics`, `getForumTopicsByID`, `getFullChannel`, `getFutureCreatorAfterLeave`, `getGroupsForDiscussion`;
  - `getInactiveChannels`, `getLeftChannels`, `getMessages`, `getParticipant`, `getParticipants`;
  - `inviteToChannel` (2 variants), `joinChannel` (2 variants), `leaveChannel`;
  - `readHistory`, `readMessageContents`, `reorderPinnedForumTopics`, `reorderUsernames`;
  - `reportAntiSpamFalsePositive`, `setBoostsToUnblockRestrictions`, `setDiscussionGroup`, `setStickers`;
  - `toggleAntiSpam`, `toggleAutotranslation`, `toggleForum`, `toggleParticipantsHidden`, `togglePreHistoryHidden`;
  - `toggleSignatures`, `toggleSlowMode`, `toggleUsername`, `toggleViewForumAsMessages`;
  - `updateColor`, `updateEmojiStatus`, `updatePaidMessagesPrice`, `updatePinnedForumTopic`.

**updates.\*:** `getChannelDifference` is blocked (l.1016).

**Channel-related messages.\*** (status, line in api-coverage.md):

| Status | Methods |
|---|---|
| blocked | `migrateChat` (651) |
| missing | `getReplies` (618), `getDiscussionMessage` (565), `readDiscussion` (654), `createForumTopic` (507), `editForumTopic` (535), `getForumTopics` (584), `getForumTopicsByID` (585), `deleteTopicHistory` (524), `updatePinnedForumTopic` (753), `reorderPinnedForumTopics` (666), `setChatAvailableReactions` (722), `setChatWallPaper` (724), `getUnreadReactions` (638), `readReactions` (661), `getSavedReactionTags` (624) |
| partial | `editChatDefaultBannedRights` (529, channel branch blocked), `getMessagesViews` (599), `getSearchCounters` (627), `getUnreadMentions` (636), `readMentions` (658), `getDialogs` (564), `getPeerDialogs` (605, channel skipped), `sendMessage` (708) |
| notimpl | `getOnlines` (602) |
| ok for basic groups only (channel branch must be added) | `getHistory` (589), `getFullChat` (586), `getChats` (555), `toggleNoForwards` (739), `unpinAllMessages` (750), `getMessageReadParticipants` ×2 (595-596) |
| ok, invite flow (must learn channels) | `exportChatInvite` (539), `checkChatInvite` (493), `importChatInvite` ×2 (646-647), `getExportedChatInvite(s)` (577-578), `editExportedChatInvite` (533), `deleteExportedChatInvite` (511), `deleteRevokedExportedChatInvites` (521), `getAdminsWithInvites` (542), `getChatInviteImporters` (554), `hideChatJoinRequest` (644) |

The ok invite-flow methods are basic-group only. With a letter hash, `chat_invite_helper.go:24` already routes to the channel case.

Also needing `PEER_CHANNEL` branches: `sendMedia`, `sendMultiMedia`, `forwardMessages`, `editMessage`, `deleteMessages`, `readHistory`, `search`, `updatePinnedMessage`, `sendReaction`, `getMessageEditData`, `saveDraft`, `setTyping`, `getPeerSettings`. The bff files are listed in 1.1.

---

## 2. Target storage and update model

### 2.1 Telegram semantics
Source: https://core.telegram.org/api/updates and https://core.telegram.org/api/channel, both fetched.
- **Own box and pts.** Each channel or supergroup has its own message box and its own `pts`. The initial channel `pts` comes from `dialog.pts`, `channelFull.pts` or `updateChannelTooLong.pts`.
- **Updates carrying channel pts:** `updateNewChannelMessage`, `updateEditChannelMessage`, `updateDeleteChannelMessages` (api.tl:346 and neighbours). The client applies the usual check `local_pts + pts_count == pts`.
- **getDifference does not return channel messages.** It marks the channels that need catching up with `updateChannelTooLong` (api.tl:344, `channel_id`, optional `pts`). At startup the client calls only `getDifference`.
- **getChannelDifference:**
  - the client limit is 10-100;
  - a non-final answer means "call again with the returned `pts`";
  - a final answer carries a `timeout` for open channels (default 1 s);
  - `channelDifferenceTooLong` comes when `pts < latestPts - 100000` (channel box size) and carries `dialog`, `messages`, `chats` and `users` (api.tl:770-772);
  - gaps are filled with `channels.getMessages` (up to 200 ids); `messages.getHistory` cannot fill channel gaps.
- **Migration:** `messages.migrateChat` returns the old `chat` with `migrated_to` and the new megagroup `channel`. `channelFull` carries `migrated_from_chat_id` and `migrated_from_max_id`. Old members get `messageActionChatMigrateTo` (api.tl:156). The new channel starts with `messageActionChannelMigrateFrom` (api.tl:157). All new messages go to the supergroup, and clients merge both histories.
- **Kinds:** megagroup is a `channel` with `megagroup` set (up to 200k members, can become a forum); broadcast has `broadcast` set, view counters and signatures; gigagroup is a one-way conversion.

### 2.2 iOS client behaviour (Telegram-iOS 756315d7)
- **Polling.** `pollChannel` calls `getChannelDifference(flags:0, filter:.channelMessagesFilterEmpty, pts:max(pollPts,1), limit:100)`; the limit is 2 in DEBUG builds (`submodules/TelegramCore/Sources/State/AccountStateManagementUtils.swift:3413-3428`). With no stored state it starts from `pts = 1`.
- **Errors.** `CHANNEL_PRIVATE` and `CHANNEL_INVALID` are handled specially (same function, l.3431).
- **Reset path.** On `channelDifferenceTooLong` it takes `dialog.pts`, `topMessage`, the read ids and the unread counters, resets the channel state and its invalidation `pts`, and merges `chats` and `users` (l.3606-3640). So the `dialog` in tooLong must carry `pts` and correct counters.
- **Grouping.** `updateChannelTooLong` from `getDifference` and push is grouped per channel and triggers a poll (l.115-116, l.364-392, l.776-800).
- **Migration result.** `_internal_convertGroupToSupergroup` calls `messages.migrateChat` with a 5 s timeout and maps `CHANNELS_TOO_MUCH` (`.../TelegramEngine/Peers/ConvertGroupToSupergroup.swift:13-21`). It takes the new peer id from the first message in `updates.messages` whose peer is not the old chat. With no such message it fails `.generic`. So the result must contain the new channel's service message, and do so within 5 s.
- **channelForbidden** is parsed in `UpdatesApiUtils.swift:216` and `ApiUtils/ApiGroupOrChannel.swift:205`.
- **UNVERIFIED:** Android, tdesktop and Web K behaviour for each of these (limits, tooLong handling, migrateChat result parsing) must be read in their pinned sources.

### 2.3 Tables to add
One migration per task, `migrate-YYYYMMDD-channels*.sql`, idempotent with `CREATE TABLE IF NOT EXISTS`.

- **`channels`** — one row per channel, the counter row, analogous to `user_state`:
  - columns: `id`, `access_hash`, `creator_user_id`, `title`, `about`, `photo_id`, `broadcast`, `megagroup`, `forum`, `signatures`, `pts` (base 1, as `userbox.BasePts`), `last_message_id`, `top_message`, `participants_count`, `admins_count`, `kicked_count`, `banned_count`, `default_banned_rights`, `slowmode_seconds`, `hidden_prehistory`, `linked_chat_id`, `migrated_from_chat_id`, `migrated_from_max_id`, `noforwards`, `join_to_send`, `join_request`, `participants_hidden`, `available_reactions`, `deleted`, `version`, `date`;
  - **ids:** message ids are sequential per channel (`last_message_id + 1`), not snowflakes and not per-user box ids.
- **`channel_messages(channel_id, id, sender_user_id, random_id, message_data json, message, date, edit_date, views, forwards, reply_to_top_id, pinned, grouped_id, deleted, pts)`:**
  - one shared copy per message;
  - `PRIMARY KEY(channel_id, id)`;
  - `UNIQUE(channel_id, sender_user_id, random_id)` — a durable `RANDOM_ID_DUPLICATE` check. Today the only guard is Redis `DoIdempotent` (audit 01 #10).
- **`channel_pts_updates(channel_id, pts, pts_count, update_type, update_data json, date)`** with `UNIQUE(channel_id, pts)`. It is the channel journal and feeds `getChannelDifference`. It is trimmed beyond the 100000-pts window or a time limit, and older requests answer `channelDifferenceTooLong`.
- **`channel_participants(channel_id, user_id, role, admin_rights, rank, banned_rights, until_date, inviter_id, promoted_by, kicked_by, date, left, state, read_inbox_max_id?, available_min_id)`:**
  - `role` is creator, admin, member, banned, left or kicked; `banned_rights` is the bitmask of `chatBannedRights` (api.tl:1295); `admin_rights` is `chatAdminRights` (api.tl:1293);
  - `available_min_id` implements `hidden_prehistory` and `deleteHistory` "for me";
  - for 100 users, per-user read state stays in `dialogs`.
- **`dialogs` for channels:**
  - reuse the existing rows with `peer_type = PEER_CHANNEL` (already queried by `dialog.getMyDialogsData_handler.go:115`);
  - `top_message`, `read_inbox_max_id`, `read_outbox_max_id`, `unread_count`, `unread_mentions_count` and `pinned_msg_id` keep the same meaning;
  - `dialog.pts` must come from `channels.pts`, a join or a second read, not a stored copy;
  - for channels, `read_outbox_max_id` is the channel-wide max read by others; it can be computed (UNVERIFIED how Telegram defines it for supergroups).
- **`channel_message_mentions` and per-user unread reactions** for `unread_mentions_count` and `getUnreadMentions`. Reuse the approach of `migrate-20261030-unread-mentions.sql`.
- **`channel_admin_log`** — later task, for `getAdminLog`.
- **`channel_message_views`** (dedupe of `getMessagesViews` with `increment`) — later task.

### 2.4 Write path: a channel box primitive
- **`pkg/channelbox`, the mirror of `pkg/userbox`.** `Run(ctx, db, channelId, fn)` takes `SELECT … FROM channels WHERE id=? FOR UPDATE`. Inside, the message rows, the journal rows and the new `pts` and `last_message_id` commit in one transaction (ADR 0011 invariants И1-И4, applied per channel). `Emit` writes `mq_outbox` rows in the same transaction (ADR 0012 §2).
- **Per-user side effects** (dialog `top_message` and unread for each member, `updateNewChannelMessage` push, mentions) must not live in the channel transaction for every member. Two options:
  - (a) push the same channel update to every online member's sessions with no per-user journal row, and set the sender's dialog in a `userbox.Run` of the sender;
  - (b) recipient dialogs stay derived: `unread_count = count(channel_messages id > read_inbox_max_id and sender != me and not deleted)`, computed on read; at 100 users and a 200k cap this is acceptable with an index (audit 01 §"Что нужно для каналов" item 1 suggests the same).
  
  Recommendation: derive unread for channels; write only the reader's own dialog row on `readHistory`.
- **Kafka key = channel id.** Then one channel's push events keep order across members. That is a new topic, or a new key type on `Sync-T`. ADR 0012 currently keys by recipient.
- **This needs a new ADR** ("Ящик канала"): channel counters in the `channels` row, journal semantics, fan-out without per-user journal, retention window, and the relation to the 0011, 0012 and 0019 invariants.

### 2.5 Read and update path
- **`updates.getChannelDifference`.** Fold `channel_pts_updates` in `(pts, channels.pts]`, as ADR 0019 does for the user box:
  - new messages that are not deleted, in their latest edit, by ascending id;
  - edits and deletes in `other_updates`;
  - a slice when more than `limit` rows, without `final`;
  - `channelDifferenceEmpty` with `final` and a timeout for open channels (Telegram gives 1 s as the default when absent);
  - `channelDifferenceTooLong` when the requested `pts` is outside the retained window or `force` asks for it; it carries the viewer's `dialog` with `pts`, the last N messages and chats/users;
  - errors: `CHANNEL_PRIVATE` when the viewer is not a member and the channel is private, `CHANNEL_INVALID` for a bad access hash;
  - each viewer only sees messages with `id >= available_min_id`.
- **Push:** `updateNewChannelMessage`, `updateEditChannelMessage`, `updateDeleteChannelMessages` (with channel `pts`) to online members; `updateChannel` (api.tl:345) on metadata, rights or membership change; `updateReadChannelInbox` (api.tl:347, own `pts`), `updateReadChannelOutbox`, `updateChannelMessageViews` and `updatePinnedChannelMessages` as needed.
- **Common-box updates the user's devices must not miss** go through the user's box or seq journal: joining or being kicked (`updateChannel`) and `updateReadChannelInbox` to own devices. Whether Telegram gives these a common `pts`/`seq` is UNVERIFIED and needs a probe with the Web K tool (memory "Telegram probe").
- **`updates.getDifference` must emit `updateChannelTooLong`** for channels where the user's stored state lags. Telegram's exact criterion is UNVERIFIED. A defensible design: record in the user's box a `channel_too_long` marker when a channel event was not delivered live (offline). Simpler: include `updateChannelTooLong(channel_id, pts)` for every member channel whose `pts` advanced since the difference base `date`. Today `merge_updates_helper.go:148-153` drops channel updates, which is correct.
- **`chats` in `getDifference` and `getChannelDifference`** must resolve `peerChannel` (ADR 0021 §7 says channels are not resolved yet). The resolver `Pick`/`Resolve` needs a channel source.
- **`channelForbidden`** (api.tl:105) for a viewer who was kicked or banned with `view_messages`, or for a private channel the viewer left. `until_date` carries a temporary ban.
- **`messages.getDialogs` and `getPeerDialogs`** must return channel dialogs with `pts` (`dialog#fc89f7f3`, api.tl:216). Today getPeerDialogs skips them (`messages.getPeerDialogs_handler.go:93`).

### 2.6 Rights, slow mode, linked discussion, kinds
- **Permission check, one function.** `admin_rights` (`chatAdminRights`) and `banned_rights` (`chatBannedRights`, with `until_date`) apply per participant. `default_banned_rights` is on the channel. Every write handler (send with media type, pin, invite, change info) calls one pure function `channelperm.Can(channel, participant, action)`.
- **Slow mode:**
  - `slowmode_seconds` lives on the channel; each member's last send time lives in `channel_participants`;
  - the send path rejects with `SLOWMODE_WAIT_X`; `channelFull` returns `slowmode_seconds` and `slowmode_next_send_date` (api.tl:110);
  - error names are UNVERIFIED until checked against https://core.telegram.org/method/messages.sendMessage.
- **Linked discussion:**
  - `channels.setDiscussionGroup` (api.tl:2794) sets `linked_chat_id` on both sides;
  - each broadcast post is auto-forwarded into the group;
  - `messages.getDiscussionMessage`, `getReplies` and `readDiscussion` work by thread `reply_to_top_id`;
  - `replies` counters live on the channel post;
  - the `RPCMessageThreads` route must be enabled.
- **Megagroup vs broadcast:**
  - broadcast: only admins with `post_messages` post; posts carry `views` and an optional `post_author` (signatures); there is no member list for non-admins;
  - megagroup: normal members send under rights; `hidden_prehistory` uses `available_min_id` set to `last_message_id` at join;
  - `participants_hidden` is a flag.

### 2.7 Basic group → supergroup migration
`messages.migrateChat` (api.tl:2506) and the auto-migration that clients trigger. Clients start the migration themselves for some actions; ROADMAP §Этап 1 says those actions currently fail.

In one logical operation:
1. Under the chat transaction, check that the caller is the creator, the chat is not deactivated and not already migrated.
2. Create the `channels` row (`megagroup`, `migrated_from_chat_id`, `migrated_from_max_id` = the chat's last message id, `title`, `photo`, `about`, `default_banned_rights`). Copy participants: creator → creator; chat admins → admin with the default admin rights (UNVERIFIED exact mapping, check against https://core.telegram.org/api/channel and a probe).
3. In the channel box: `messageActionChannelMigrateFrom(title, chat_id)` as message 1.
4. In each member's user box: `messageActionChatMigrateTo(channel_id)` as a service message, `updateChat` (seq), and the chat dialog marked migrated. Use `chat.migratedToChannel` (exists, `chat.migratedToChannel_handler.go`), but its swallowed error on `UpdateStateByChatIdTx` must be fixed.
5. Answer the caller with an `updates` that contains both chats (old `chat` with `migrated_to`, new `channel`) and the channel's service message. iOS needs this message (`ConvertGroupToSupergroup.swift:24-29`) within 5 s.
6. From then on, sends to the old chat answer `CHAT_MIGRATED`/`MIGRATED` (UNVERIFIED code name). `getFullChat` on the old chat and `getFullChannel` on the new one link to each other.

**Step ordering.** Several boxes are written in turn, so the order must keep a crash from silently dropping a step. The channel and the caller's result go first. Members are reached by outbox events, as ADR 0021 does. A rerun must be safe: key the migration by `chat_id`, and on retry return the existing channel.

---

## 3. Dependency-ordered tasks (1-3 days each)

Shared rules:
- every task adds e2e under `test/e2e` via `pkg/mtclient` when visible to clients, and integration tests via `pkg/testinfra`;
- per memory, heavy runs go to CachyOS;
- all user-visible texts are localization keys;
- task numbers continue after 033 (`docs/tasks`).

**Ordering and estimate.** T1 → T2 → T3 → T4 → T5 are strictly ordered. After T5, the T6-T9 group, T10 and T11 can run partly in parallel. Then T12 → T13 → T14 → T15, with T16 and T17 last. Rough total: about 30-35 working days, consistent with the audit's 6-10 weeks (audit 01 l.125) once forums, admin log and discussion are included.

### T0. Research and ADR "Ящик канала" (2 days)
- **Covers:** no code. A Web K probe against real Telegram (memory "Telegram probe"): `updates` on join, leave, kick, read and migrate; whether those carry common `pts`/`seq`; how `getDifference` emits `updateChannelTooLong`; `channelDifferenceTooLong` contents; `messages.migrateChat` result shape; error names (`CHANNEL_PRIVATE`, `SLOWMODE_WAIT_X`, `CHAT_MIGRATED`, `RANDOM_ID_DUPLICATE`). Read the Android, tdesktop and Web K sources at their pinned tags for getChannelDifference limits, the tooLong path and migrateChat parsing. Confirm that the iOS tree matches the pin.
- **Output:** a new ADR (channel box, journal retention, fan-out without per-user journal, unread derivation, Kafka key) and references for conformance scenarios (ADR 0018).
- **Tests:** none. It produces conformance reference JSONs.

### T1. Schema and `pkg/channelbox` (3 days)
- **Data:** migration with `channels`, `channel_messages`, `channel_pts_updates`, `channel_participants`.
- **Code:** `pkg/channelbox` (`Check`, `Read`, `Run`, `NextMessageId`, `AddPtsUpdate`, `Emit` through `pkg/outbox`).
- **Tests:**
  - unit tests with the fake DB, as `pkg/userbox/fakedb_test.go` does;
  - MySQL integration tests: concurrent `Run` on one channel gives gapless `pts` and ids, rollback burns nothing, the `UNIQUE(channel_id,pts)` guard holds;
  - a test that fails without the lock.

### T2. biz `channel` service and bff `channels` module skeleton with routing (2 days)
- **Covers:** service `app/service/biz/channel` (`getMutableChannel`, `getChannelListByIdList`, participant reads) and the `RPCChannels` route in all three `session.yaml` files. Register the service in `app/bff/bff/internal/server/server.go`. Every not-yet-served `channels.*` method returns `ErrEnterpriseIsBlocked` (400) explicitly, not Unimplemented.
- **Also:** the channel `Chat` constructor (`channel#d49f34c6`, `channelForbidden`) per viewer (`min`, `left`, rights) and the resolver source for ADR 0021 `Resolve`.
- **Tests:** unit tests for the constructor from participant state; `tools/apicoverage` re-run.

### T3. createChannel, getChannels, getFullChannel, getMessages (3 days)
- **Methods:** `channels.createChannel` (broadcast and megagroup, no forum yet), `channels.getChannels`, `channels.getFullChannel` (`channelFull` with `pts`, counts, rights flags), `channels.getMessages`.
- **Data:** the creator participant, service message `messageActionChannelCreate` id 1, the creator's dialog in the user box with `updateChannel`.
- **Tests:**
  - integration on MySQL;
  - e2e: create, then getFullChannel, getDialogs shows the channel with `dialog.pts`;
  - `messages.getDialogs`/`getPeerDialogs` channel branch (`getPeerDialogs_handler.go:93`, `getMyDialogsData`).

### T4. Send to a channel and channel history (3 days)
- **Methods:** `PEER_CHANNEL` in `msg.sendMessageV2`, `sendMedia`, `sendMultiMedia` (`channel_messages`, journal `updateNewChannelMessage`, `random_id` uniqueness, `updateMessageID` to the sender); `messages.getHistory`, `messages.search` and `getMessages` for channels; `channelMessagesFilter`.
- **Push:** online members get the update via sync, keyed by channel id.
- **Tests:**
  - integration: concurrent sends from two users give ordered ids and `pts` with no duplicates on `random_id` retry after 90 s (fails today per audit #10);
  - e2e: two clients, one sends, the other receives `updateNewChannelMessage` with consecutive `pts`.

### T5. `updates.getChannelDifference` and `updateChannelTooLong` in getDifference (3 days)
- **Methods:** bff and biz `getChannelDifference` (fold, slice, empty with timeout, tooLong with `dialog`, `force`, errors); `getDifference` emitting `updateChannelTooLong` per the T0 ADR; resolve `peerChannel` in difference chats.
- **Tests:**
  - unit tests of the fold (mirror `seq_fold_test.go`);
  - integration: slice boundaries at `limit` 100 and 2 (iOS DEBUG), tooLong after retention trim;
  - e2e: client offline, 150 posts, reconnect, `getDifference` gives `updateChannelTooLong`, then a `getChannelDifference` loop reaches `final`;
  - a test that fails with the current stub.

### T6. Membership: join, leave, invite, participants (3 days)
- **Methods:** `channels.joinChannel` (both), `leaveChannel`, `inviteToChannel` (both), `getParticipants` (all filters in api.tl:784-791), `getParticipant`, `getLeftChannels`; channel branches in `messages.exportChatInvite`, `checkChatInvite`, `importChatInvite` and join requests (`hideChatJoinRequest`, `toggleJoinRequest`, `toggleJoinToSend`, unblocking them).
- **Data:** `channel_participants`, service messages `chatAddUser`/`chatJoinedByLink`, counts, `available_min_id` with `hidden_prehistory`.
- **Tests:** integration (concurrent join/leave counts); e2e (invite link join, then history visible from `available_min_id`; leave gives `channelForbidden` or `left` in the constructor).

### T7. Admin and banned rights (3 days)
- **Methods:** `channels.editAdmin`, `editBanned`, `editCreator`, `getFutureCreatorAfterLeave`; `messages.editChatDefaultBannedRights` channel branch (unblock `:60`).
- **Code:** pure `channelperm.Can` applied in every write handler; `channelForbidden` with `until_date` for `view_messages` bans; `updateChannel` / `updateChannelParticipant` pushes.
- **Tests:** a unit table for `Can` covering all rights bits and until_date expiry; e2e: a banned member cannot send media (error name from T0), and an admin without `pin_messages` cannot pin.

### T8. Edit, delete, pin, read in channels (3 days)
- **Methods:**
  - `PEER_CHANNEL` in `msg.editMessageV2` (unblock `:41`) and `messages.editMessage`, `getMessageEditData`;
  - `channels.deleteMessages`, `deleteHistory` (both), `deleteParticipantHistory`;
  - `messages.updatePinnedMessage` and `unpinAllMessages` for channels (`updatePinnedChannelMessages`);
  - `channels.readHistory` (`updateReadChannelInbox` to own devices, outbox to others), `channels.readMessageContents`, `readMentions`/`getUnreadMentions` channel branches.
- **Tests:** integration (journal fold of edit then delete); e2e (edit and delete seen by another member; read on device A reaches device B).

### T9. Channel metadata (2 days)
- **Methods:** `channels.editTitle`, `editPhoto`, `toggleSignatures`, `togglePreHistoryHidden`, `toggleParticipantsHidden`, `messages.toggleNoForwards` (channel), `setChatAvailableReactions`, `channels.updateColor`, `deleteChannel`; usernames for public channels (`checkUsername`, `updateUsername`, `toggleUsername`, `reorderUsernames`, `deactivateAllUsernames`, `getAdminedPublicChannels`, resolve via `contacts.resolveUsername`).
- **Tests:** e2e per visible change (service message plus `updateChannel` to members); integration for username uniqueness.

### T10. Slow mode (1 day)
- **Methods:** `channels.toggleSlowMode`; the send-path check; `channelFull.slowmode_next_send_date`.
- **Tests:** unit (time math); e2e (second send within N s rejected with the T0 error name; admin exempt — UNVERIFIED, confirm in T0).

### T11. Broadcast specifics (2 days)
- **Methods:** `messages.getMessagesViews` (increment with dedup table, `updateChannelMessageViews`), post signatures and `post_author`, `channels.exportMessageLink`, `channels.getSendAs` if `send_as` is in scope.
- **Tests:** integration (views dedup per user); e2e (subscriber cannot post; views grow once per user).

### T12. Basic group → supergroup migration (3 days)
- **Methods:** `messages.migrateChat`, plus the server-side migration paths that clients trigger by changing settings a basic group lacks (which settings: from T0, UNVERIFIED now).
- **Code:** reuse and fix `chat.migratedToChannel`; old-chat sends answer the migrate error; `getFullChat` and `getFullChannel` cross-links; history merge fields; invite links moved (`chat_invites.migrated_to_id`).
- **Tests:**
  - integration: crash between steps, rerun converges, no double channel;
  - e2e: three members, migrate, result contains `chat(migrated_to)`, `channel` and the `channelMigrateFrom` message in under 5 s (iOS requirement, `ConvertGroupToSupergroup.swift:20-29`); other members get `chatMigrateTo` and can send into the supergroup;
  - a manual check in all four clients with screenshots (memory "verify yourself").

### T13. Linked discussion and threads (3 days)
- **Methods:** `channels.setDiscussionGroup`, `getGroupsForDiscussion`; auto-forward of posts into the group; `messages.getDiscussionMessage`, `getReplies`, `readDiscussion`; `replies` counters; enable the `RPCMessageThreads` route.
- **Tests:** e2e (post appears in the group, comment counter grows, readDiscussion).

### T14. Forums and topics (3 days)
- **Methods:** `channels.toggleForum`, `createForumTopic`, `editForumTopic`, `getForumTopics`, `getForumTopicsByID`, `updatePinnedForumTopic`, `reorderPinnedForumTopics`, `deleteTopicHistory`, `toggleViewForumAsMessages`, plus the same `messages.*` variants (api-coverage.md l.507, 524, 535, 584-585, 666, 753); `top_msg_id` on send, search and read.
- **Data:** a `channel_forum_topics` table.
- **Tests:** integration; e2e (create topic, post, per-topic unread).

### T15. Admin log and moderation leftovers (2 days)
- **Methods:** `channels.getAdminLog` (`channel_admin_log` written by T6-T14 handlers in the same transaction), `channels.reportSpam` (replace the fake with a real handler), `toggleAntiSpam` (blocked or real by decision), `getInactiveChannels`, `convertToGigagroup`.
- **Tests:** integration (log rows per action); e2e (`getAdminLog` filters).

### T16. Conformance scenarios and client checks (2 days)
- **Covers:** ADR 0018 scenarios comparing create, send, getChannelDifference, migrate and rights with real Telegram; manual passes in iOS, Android, Desktop (CachyOS host) and Web.
- **Tests:** `test/conformance`; screenshots in the brief.

### T17. Out of scope or owner decision
Sponsored messages, boosts, paid messages, stories in channels, recommendations, autotranslation, emoji status, stickers sets (`setStickers`, `setEmojiStickers`), `updatePaidMessagesPrice`. These should keep returning `ErrEnterpriseIsBlocked` (400) unless the owner wants them. ADR 0016 (no premium) bears on some of them.

---

## Open questions for the owner and T0
1. Should forums (T14) and admin log (T15) be in stage 1 or deferred to stage 2?
2. Retention window for `channel_pts_updates`: Telegram's box size is 100000 (core.telegram.org/api/updates); a time limit is optional.
3. Which Telegram error names to use for slow mode, migrated chats and channel-count limits. They are UNVERIFIED until the T0 probe.