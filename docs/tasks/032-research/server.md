# How Pluma handles user status and status privacy, and what Telegram's docs say

I only read files and fetched docs. Nothing was edited, committed or deployed. All paths are relative to `/Users/sckr.a/teamgram-server`; `proto/` means `third_party/teamgram-proto/mtproto/`.

## 1. `account.updateStatus`: what is stored, where, and what expires

**Handler:** `app/bff/userchannelprofiles/internal/core/account.updateStatus_handler.go`
- **Online:** if `offline=false`, it sets `expires = 300` (l.47) and builds `userStatusOnline{expires: now+300}`.
- **Offline:** if `offline=true`, it builds `userStatusOffline{was_online: now}`.
- **The update is built but never sent.** It is wrapped in `updateShort` with `Date: now-1` (l.42). The send code is commented out under `// TODO: push` (l.72–100).
- **Storage call:** fire-and-forget `threading.GoSafe(UserUpdateLastSeen{Id, LastSeenAt: now, Expires})` (l.62). Errors are ignored.
- **Reply:** always `boolTrue` (l.102).

**Storage:** `user.updateLastSeen` → `Dao.PutLastSeenAt` (`app/service/biz/user/internal/dao/user_status.go:82-93`).
- It writes a Redis cache key `user_presences_<id>` through `CachedConn.SetCache` (l.89).
- It writes the MySQL table `user_presences` (`user_id, last_seen_at, expires`; `teamgramd/deploy/sql/1_teamgram.sql:697`) asynchronously, through `threading2.WrapperGoFunc` + `InsertOrUpdate` (l.90-92). An error there is lost.
- Reads use `GetLastSeenAt` (l.54-80): Redis first, MySQL on a miss.

**How "online" is decided at read time:** `MakeUserStatus(lastSeenAt, allowTimestamp)` (`proto/user_status_helper.go:87-112`, the same copy is in `app/service/biz/user/user/user_status.go:89`).
- The stored `expires` is never used. "Online" means `now <= lastSeenAt+60`, and the answer is `userStatusOnline{expires: lastSeenAt+60}`. Otherwise it is `userStatusOffline{was_online: lastSeenAt}`.

Problems this causes:
- **Offline still reads as online.** `offline=true` stores `last_seen_at = now`, so every reader sees "online" for another 60 s after the user went offline.
- **Online is cut short.** Telegram's own reply would say online until `now+300`, but readers see `+60`. Clients send online roughly every minute (UNVERIFIED), so the status flickers when a ping is late.
- **A lost connection is never turned into offline.** The status service keeps only connection presence: Redis hash, `StatusExpire: 90`, `teamgramd/etc2/status.yaml:29`; `status.setSessionOffline` is called from `app/interface/session/internal/sess/main_auth_wrapper_manager.go:106,131`. Last seen simply ages, which is correct passively, but nothing is pushed.
- **Sign-up:** a new user gets `PutLastSeenAt(now, 300)` (`app/service/biz/user/internal/dao/user.go:131`).

## 2. Where the server builds `UserStatus` for users it returns

`ImmutableUser.LastSeenAt` is filled from `GetLastSeenAt` when user data is loaded (`dao/user.go:193, 551, 591, 672, 798, 827`).

| Path | Code | Privacy applied |
|---|---|---|
| `users.getUsers`, `messages.getDialogs`, `getPeerDialogs` | `Vector_ImmutableUser.GetUserListByIdList` → `ToUnsafeUser(me)` (`app/service/biz/user/user/mutable_users.go:75-90`; `app/bff/users/internal/core/users.getUsers_handler.go:49`; `app/bff/dialogs/internal/core/messages.getDialogs_handler.go:141`, `messages.getPeerDialogs_handler.go:228`) | yes, in both directions (see below) |
| `ToUnsafeUser` itself | `proto/immutable_user_helper.go:401-406` | first `selfUser.CheckPrivacy(STATUS_TIMESTAMP, m.Id())`, then `m.CheckPrivacy(STATUS_TIMESTAMP, self)`, then `MakeUserStatus(m.LastSeenAt, allow)` |
| `ToUser(selfUserId)` (getFullUser, resolvePhone, acceptContact, deleteContacts, contact_signup, msg.sendMessageV2) | `proto/immutable_user_helper.go:633-636` | only the target's own rule; no check of the viewer's rule (there is a "TODO: check if we need to check privacy for status") |
| Self user | `ToSelfUser` (`proto/immutable_user_helper.go:450`) | `MakeUserStatus(..., true)` |
| `contacts.getStatuses` | `app/bff/contacts/internal/core/contacts.getStatuses_handler.go:42-50` | **none**: `MakeUserStatus(v.LastSeenAt, true)` for every contact, which leaks exact last seen whatever the privacy setting. Errors are ignored (`cList, _`, `lastSeenList, _`). |

**Rounding when the timestamp is hidden** (`proto/user_status_helper.go:101-110`):
- 30 days or more since last seen → `nil` (no status at all; the client shows "long time ago");
- 7 days or more → `userStatusLastMonth`;
- 3 days or more → `userStatusLastWeek`;
- otherwise → `userStatusRecently`.

The 3-day boundary for "last week" is upstream's choice; Telegram's real boundaries are UNVERIFIED. The `by_me` flag is never set.

Neither `ToUnsafeUser` nor `ToUser` hides status from users who block you, or whom you block: there is no "block" in `immutable_user_helper.go` (grep).

## 3. How status privacy rules are stored and evaluated

**Storage:**
- MySQL `user_privacies(user_id, key_type, rules json)` (`teamgramd/deploy/sql/1_teamgram.sql:712`).
- Redis key `user_privacy_status_timestamp_<id>` (`app/service/biz/user/internal/dao/user_privacy.go:39`).
- Read by `GetUserPrivacyRules` (`user_privacy.go`, about l.185-225). With no row, the default is `defaultRules = [privacyValueAllowAll]` (`app/service/biz/user/internal/dao/vars.go:31-33, 76-84`).
- The user cache carries the status-timestamp, photo and phone rules (`cache_user_data.go:301-328`), and they are attached to `ImmutableUser.KeysPrivacyRules` when `privacy=true` (`dao/user.go:229-230, 694-695`).

**Writing:** `SetUserPrivacyRules` (`user_privacy.go`, about l.325-370) runs one box transaction (`RunBox`). It drops the cache keys, including the user-data cache for the status, photo and phone keys, and sends `updatePrivacy` with seq to the account's own devices through `EmitSeqUpdate`.

**Evaluation:** `ImmutableUser.CheckPrivacy` → `privacyIsAllow(rules, id, isContact)` (`proto/immutable_user_helper.go:265-289`, `proto/privacy_rule_helper.go:348-416`).
- **Base rule:** `AllowAll`, `AllowContacts` or `DisallowAll`, whichever appears last.
- **Exceptions that work:** the users in `AllowUsers` / `DisallowUsers`.
- **Chat-participant rules are wrong.** `Allow/DisallowChatParticipants` returns `true` unconditionally, without checking membership (l.372-373, 386-387, 394-395, 408-409). Under `Contacts` or `Nobody` with an "allow chat X" exception, everyone is allowed: a privacy leak. The early return also skips any `DisallowUsers` that comes later in the list.
- **Not handled at all:** `privacyValueDisallowContacts`, `AllowCloseFriends`, `AllowPremium`, `Allow/DisallowBots`. For example, "Everybody, except contacts" still shows the status to contacts. The callback-based `CheckPrivacyIsAllow` (l.256-336) checks chats properly but is not used here.
- **Contact check:** `isContact` comes from `m.Contacts`, the target's own contact list, so "my contacts" means people in the owner's address book. That matches Telegram (UNVERIFIED).
- **The viewer's own setting counts too:** `ToUnsafeUser` also checks the viewer's own rule (l.402). That matches the documented reciprocity, but Premium is not exempted and `by_me` is not set.

**`hide_read_marks`:**
- Stored and returned: table `user_global_privacy_settings.hide_read_marks` (`migrate-20240420.sql:2`); `user.setGlobalPrivacySettings_handler.go:38`; `user.getGlobalPrivacySettings_handler.go:51`.
- **Never enforced.** The only uses of `HideReadMarks` in `app` and `pkg` are in the user service storage and handlers.
- `messages.getOutboxReadDate` (`app/bff/messages/internal/core/messages.getOutboxReadDate_handler.go:55-56`) still has `// TODO: 1. Check USER_PRIVACY_RESTRICTED 2. Check YOUR_PRIVACY_RESTRICTED`.
- `userFull.read_dates_private` is not computed. I did not check this one by grep, so it is unverified.

## 4. How the server pushes an update to another user's devices

**Without seq: `SyncPushUpdates`.**
- In bff, `SyncClient` is `sync_client.MustNewSyncOutboxClient` (`app/bff/dialogs/internal/dao/dao.go:46`, `app/bff/userchannelprofiles/internal/dao/dao.go:39`). userchannelprofiles already has it.
- Each call is one autocommit INSERT into `mq_outbox`, topic `Sync-T` (`teamgramd/etc2/bff.yaml:103-104`), Kafka key = recipient (`app/messenger/sync/client/sync_outbox_client.go`, `insert` / `SyncPushUpdates`). The msg relay publishes it.
- The sync consumer `SyncPushUpdates` (`app/messenger/sync/internal/core/sync.pushUpdates_handler.go`) calls `processUpdates` and then `pushUpdatesToSession`.
- `pushUpdatesToSession` (`core.go:141-185`) looks up the recipient's online sessions in the status service, retrying for up to 10 s (l.188-215), and pushes to each gateway. When the lookup fails, the push is dropped.
- `processUpdates` (`core.go:50-140`) only decides `notification`: whether APNs/FCM fires. `updateUserStatus` is not in its list, so it would not wake devices, which is correct.
- **Precedent for the pattern:** `messages.setTyping` (`app/bff/dialogs/internal/core/messages.setTyping_handler.go:40-50, 70`) sends `updateShort(updateUserTyping)` this way to the peer, or to each group member.

**With seq: `EmitSeqUpdate(b, topic, u, users, chats)`.**
- `app/messenger/sync/client/seq_push.go:27-79`: journals `user_seq_updates` inside the owner's box transaction and pushes an `updates{seq}` container to all the owner's devices.
- For other users' accounts, ADR 0021 uses `inbox.EmitSeqInbox` on topic `Inbox-T`.

**Which updates go without seq:** ADR 0020 §2 (`docs/adr/0020-seq-journal.md:31-36`) lists them: drafts, `updateServiceNotification`, typing **and statuses** ("эфемерны", ephemeral), `updateLoginToken`. pts updates never carry seq either.

**What `contacts.getStatuses` returns:** `Vector<ContactStatus{user_id, status}>` for every contact that has a presence row, with no privacy (see §2).

## 5. What core.telegram.org says (fetched 2026-11-03)

- **[updateUserStatus](https://core.telegram.org/constructor/updateUserStatus):** "Contact status update." Fields: `user_id`, `status` ("New status"). The word "Contact" suggests contacts receive it. The page says nothing more about recipients.
- **[account.updateStatus](https://core.telegram.org/method/account.updateStatus):** "Updates online user status." `offline` is described as "If (boolTrue) is transmitted, user status will change to (userStatusOffline)". "Only users can use this method." No errors are listed, and nothing is said about who is notified or about the expiry period.
- **[api/privacy](https://core.telegram.org/api/privacy):** lists `inputPrivacyKeyStatusTimestamp` only in the schema. It does not describe evaluation order or recipients. Close friends is a rule that "can be used only when posting stories".
- **[privacyKeyStatusTimestamp](https://core.telegram.org/constructor/privacyKeyStatusTimestamp):**
  - Meaning: "Whether we can see the last online timestamp of this user".
  - Reciprocity: without Premium, if you hide your own, "we won't be able to see the exact last online timestamp of those users". In that case the `by_me` flag of `userStatusRecently` / `LastWeek` / `LastMonth` "will be set".
- **[userStatusRecently](https://core.telegram.org/constructor/userStatusRecently):** `by_me` means the exact status "is actually available to us" but needs Premium, or allowing that user to see ours.
- **[globalPrivacySettings](https://core.telegram.org/constructor/globalPrivacySettings):** `hide_read_marks` makes the status-timestamp key "also apply to the ability to use messages.getOutboxReadDate". Users restricted by it get 403 `USER_PRIVACY_RESTRICTED`, and `userFull.read_dates_private` is set for them.

**Who receives status updates, per the docs:** only "contact" (from the constructor's description). Nothing about chat partners or group members. Anything beyond contacts must come from the official clients' sources or a probe against real Telegram (`reference-telegram-probe`); until then it is UNVERIFIED.

## Minimal correct design for pushing `updateUserStatus`

1. **Fix the stored model first.**
   - Store `last_seen_at` and an "online until" time: online `expires = now+300`, offline `expires = now` (or 0).
   - Make `MakeUserStatus` use the stored `expires` instead of `lastSeenAt+60`. This needs a `// pluma:` patch in proto, or a server-side wrapper.
   - Write synchronously in the handler and return its error. Don't use `GoSafe` / `WrapperGoFunc` with lost errors.
2. **Send only on a change.**
   - Send when offline becomes online, or online becomes offline. For an online-to-online refresh, send only if the old `expires` is close to running out (for example, under 60 s left). Without this, every ping every ~60 s from every device would fan out.
   - Decide the change with an atomic Redis compare-and-set on the presence key (Lua), so two devices cannot both send.
   - A per-user minimum interval (for example 5–10 s) protects against flapping.
3. **Recipients: the user's reverse contacts** (people who have the user in their contact list; `ReverseContactIdList` is in `cache_user_data.go`), minus the user's own devices.
   - Online check: one `status.getUsersOnlineSessionsList` call for the whole list (the handler exists in `app/service/status/internal/core/`), then send only to recipients with an online session.
   - Chat partners who aren't contacts: an open question. UNVERIFIED; settle it from client sources or a probe.
4. **Privacy, per recipient R, computed before sending:**
   - R is blocked by the user, or blocks the user → skip. This needs a block lookup; the matching Telegram rule is UNVERIFIED.
   - Allowed both ways (the user's rule for R, and R's own rule for the user) → the exact `userStatusOnline` / `Offline`.
   - Otherwise → the rounded `userStatusRecently`, with `by_me` when only R's own rule blocks it (and R is not Premium). It should go only when the rounded value changes, which in practice means not at all on online/offline flips. Whether Telegram sends `updateUserStatus(recently)` when someone comes online is UNVERIFIED.
   - Before relying on the evaluator, fix `privacyIsAllow`: membership check for ChatParticipants, plus `DisallowContacts`, CloseFriends, Premium and Bots. It is cleaner to move to `CheckPrivacyIsAllow` with callbacks.
   - Apply the same evaluation to `contacts.getStatuses`, and make `ToUser` symmetric with `ToUnsafeUser`.
5. **Delivery: no seq, no pts, no durable journal.**
   - Send `updateShort{updateUserStatus}` with `date = now` through `SyncPushUpdates`, as typing does, following ADR 0020 §2.
   - Durability: it goes through `mq_outbox` like typing. That is acceptable at ≤100 users. A status that is late by more than about 10 s is stale; consider a TTL or skipping old status events in the sync pusher.
   - No APNs/FCM: already the case, since `processUpdates` doesn't set `needPush` for this update.
6. **Behaviour after a restart or a lost connection.**
   - Nothing needs replaying: clients read status through `getDifference` plus `users.getUsers` / `contacts.getStatuses`.
   - Users whose connection dies without `updateStatus(offline)` are covered by the stored `expires`, which readers see passively.
   - Optionally, a sweeper or a trigger on the last `status.setSessionOffline` could send offline with `was_online = last_seen_at`. That needs a decision; Telegram's behaviour here is UNVERIFIED.
7. **Tests:**
   - e2e: two contacts; A sends `updateStatus(false)` → B gets `updateUserStatus(online)`; A sends `updateStatus(true)` → B gets offline.
   - A with "Nobody" → B gets nothing exact.
   - A non-contact C → gets nothing.
   - Unit tests for the fixed `privacyIsAllow` and for the change detection.

## Open questions

1. Recipients beyond contacts: open private chats, members of shared groups, or a viewer currently looking at the profile? The docs say only "Contact status update". Needs the clients' sources or a two-tab probe against real Telegram.
2. Does Telegram send a rounded status (`userStatusRecently`, with or without `by_me`) to recipients who can't see the exact one, and when?
3. Blocked users: Telegram probably hides status in both directions (UNVERIFIED).
4. The exact boundaries of Telegram's rounding for "recently", "last week" and "last month". The 3/7/30 days here are upstream's choice.
5. Does Telegram itself send offline after an unclean disconnect, and after how long?
6. Premium: is it ever enabled in Pluma? If not, reciprocity always applies and `by_me` is always set when it applies.
7. Not done yet, separate pieces of work:
   - `hide_read_marks`, `read_dates_private` and the `USER_PRIVACY_RESTRICTED` / `YOUR_PRIVACY_RESTRICTED` checks in `getOutboxReadDate`.
   - The `privacyIsAllow` leak, which also affects photo and phone, not just status.
   - The privacy-blind `contacts.getStatuses`.

   These go beyond task 032. Report them, and fix them in their own task or explicitly inside 032.
