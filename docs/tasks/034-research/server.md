# Pluma privacy audit: how each setting is stored and enforced (research only; nothing was edited)

Paths are relative to `/Users/sckr.a/teamgram-server`. `proto/` means `third_party/teamgram-proto/mtproto/`. This report adds to `docs/tasks/032-research/server.md` and ADR 0030 and does not repeat them.

## 0. Storage and the two evaluators

**Storage of per-key rules**
- **Table:** MySQL `user_privacies(user_id, key_type, rules json)`.
- **Cache:** Redis has one key per privacy key: `user_privacy_<key>_<id>`. The birthday key has a version suffix, `user_privacy_birthday.2` (`app/service/biz/user/internal/dao/user_privacy.go:38-53`).
- **Read:** `GetUserPrivacyRules` (`user_privacy.go:185-225`).
- **Defaults when there is no row** (`dao/vars.go:76-84`):

  | Key | Default |
  |---|---|
  | PhoneNumber | `DisallowAll` |
  | Birthday | `AllowContacts` |
  | Everything else | `AllowAll` |

  Telegram's real defaults are UNVERIFIED.
- **Write:** `SetUserPrivacyRules` (`user_privacy.go:325-373`) runs one box transaction with `FOR UPDATE` and pushes `updatePrivacy` with seq to the owner's own devices.
- **No validation on write.** `account.setPrivacy` (`app/bff/privacysettings/internal/core/account.setPrivacy_handler.go:41`) stores the rules exactly as sent:
  - empty user and chat lists are kept;
  - `AllowCloseFriends` is accepted on any key, although core.telegram.org/api/privacy says it "can be used only when posting stories";
  - chat ids are not checked;
  - channel ids are not resolved (`:65-67`, `// TODO`).
- **Cached copy.** The user-data cache holds only the StatusTimestamp, ProfilePhoto and PhoneNumber rules (`dao/cache_user_data.go:300-328`). Errors there are swallowed (`rules1, _ = ...`).

**How chat-participant rules are stored**
- They keep the client's bare `chats: Vector<long>` as is, in the same JSON.
- Ids below `MinNebulaChatChannelID` (1073741824, `proto/vars.go:43,59`) are treated as basic chats, the rest as channels (`SplitChatAndChannelIdList`, `proto/chat_helper.go:50`).
- What each client sends:
  - Android sends `-dialogId`, the positive bare id (`PrivacyControlActivity.java:1083,1101`).
  - tdesktop sends `peerToBareMTPInt` (`api/api_user_privacy.cpp:41-50`).
  - iOS sends the group or channel id (`UpdatedAccountPrivacySettings.swift:553,574`).
- Channels do not exist in Pluma yet, so in practice only basic-chat ids occur.

**Evaluator A: `privacyIsAllow`** (`proto/privacy_rule_helper.go:348-416`)
- Called through `ImmutableUser.CheckPrivacy` (`proto/immutable_user_helper.go:265-289`).
- Used for phone, photo and status in `ToUnsafeUser` and `ToUser`, for `contacts.getStatuses`, and for the status push (`account.updateStatus_handler.go:171-172`).
- It has no membership data. Every `Allow/DisallowChatParticipants` rule returns `true` at once, even with an empty list.
- It returns `true` when the key's rules are missing (`immutable_user_helper.go:281-283`). That is fail-open.

**Evaluator B: `CheckPrivacyIsAllow`** (`privacy_rule_helper.go:256-336`)
- It takes callbacks for "is contact" and "is in these chats", and skips empty chat lists (`len(r.Chats) > 0`).
- It handles `AllowAll`, `AllowContacts` and `DisallowAll`, plus user and chat exceptions.
- It ignores `DisallowContacts`, `AllowPremium`, `AllowBots`, `DisallowBots` and `AllowCloseFriends`.

**The `user.checkPrivacy` RPC** (`app/service/biz/user/internal/core/user.checkPrivacy_handler.go:31-33`) always answers `boolTrue` (`// TODO`). Nobody calls it today; it is a trap for future callers.

**Premium exists.**
- The admin grants it (`pkg/premium/premium.go`, `docs/ops/premium.md`, ADR 0016).
- `ImmutableUser.Premium()` is reliable (`immutable_user_helper.go:139-149`).
- So `AllowPremium`, the status-reciprocity exemption with `by_me`, and the Premium-only voice-message setting can and must be honoured.

## 1. Key × enforcement point

Status values: **ok**, **bug**, **missing**, **n/a** (the feature does not exist in Pluma). "Evaluator A bugs" means the bugs listed in §2.1.

| Key | Enforcement point | Status | Where |
|---|---|---|---|
| StatusTimestamp | `users.getUsers`, dialogs, history, every `ToUnsafeUser` | bug | `immutable_user_helper.go:401-406`; Evaluator A bugs |
| StatusTimestamp | `ToUser` (getFullUser's peers, resolvePhone, group sends) | bug | `:633-636`: one direction only, plus Evaluator A |
| StatusTimestamp | `contacts.getStatuses` | bug | `contacts.getStatuses_handler.go:51-71`; Evaluator A |
| StatusTimestamp | push of `updateUserStatus` | bug | `account.updateStatus_handler.go:171-172`; Evaluator A |
| StatusTimestamp | Premium exemption and `by_me` | missing | `user_status_helper.go` (see 032 research) |
| StatusTimestamp + `hide_read_marks` | `messages.getOutboxReadDate` | missing | `messages.getOutboxReadDate_handler.go:64-65` (`TODO`) |
| StatusTimestamp + `hide_read_marks` | `userFull.read_dates_private` | missing | not set anywhere in `users.getFullUser_handler.go:77-128` |
| ProfilePhoto | `user.photo` through `ToUnsafeUser` / `ToUser` | bug | `immutable_user_helper.go:397,629`; Evaluator A |
| ProfilePhoto | `userFull.profile_photo` | **bug (leak)** | `users.getFullUser_handler.go:93`: always the real photo |
| ProfilePhoto | `photos.getUserPhotos` | **missing (leak)** | `photos.getUserPhotos_handler.go:39-62`: no check at all |
| ProfilePhoto | fallback ("public") photo | **bug** | `photos.uploadProfilePhoto_handler.go:29-50` and `photos.updateProfilePhoto_handler.go` ignore `fallback` (the field exists: `schema.tl.sync_service.pb.go:30569`); `userFull.fallback_photo` is always nil (`getFullUser:94`) |
| ProfilePhoto | personal photo (`personal_photo`) | missing | `photos.uploadContactProfilePhoto_handler.go:29-31`: `ErrMethodNotImpl` |
| PhoneNumber | `user.phone` through `ToUnsafeUser` / `ToUser` | bug | `immutable_user_helper.go:392,624`; Evaluator A |
| PhoneNumber | `add_phone_privacy_exception` | ok | `user_privacy.go:240-318` |
| AddedByPhone | `contacts.importContacts` | ok | `user.importContacts_handler.go:166-176`, `contacts_import.go:150-155` |
| AddedByPhone | `contacts.resolvePhone` | **bug** | `contacts.resolvePhone_handler.go:66-77`: `selfId` and `checkId` are swapped |
| About | `userFull.about` | **missing (leak)** | `users.getFullUser_handler.go:90`: always sent |
| Birthday | `users.getFullUser` | bug | `:297-329`: the chat callback always returns `false` (`:314-319`) |
| Birthday | `contacts.getBirthdays` | bug | `dao/birthdays.go:77-79`: the chat callback returns `false` |
| VoiceMessages | `userFull.voice_messages_forbidden` | bug | `:212-234`: chat callback `false`; fail-open on error; target's Premium not considered |
| VoiceMessages | send (`messages.sendMedia`, `sendMultiMedia`, `forwardMessages`) | missing | no `VOICE_MESSAGES_FORBIDDEN` anywhere; `msg.sendMessageV2_handler.go:113-150` checks only Premium and blocks |
| Forwards | `fwd_from.from_id` vs `from_name` | ok, with gaps | `messages.forwardMessages_handler.go:115-137,315-334`; nil panic (B5); `AllowPremium` ignored |
| Forwards | `userFull.private_forward_name` | missing | `getFullUser:103` is nil. Low impact: Android never reads it (grep outside `tgnet/`) |
| ChatInvite | `messages.createChat` | **bug** | `messages.createChat_handler.go:96-126`: a refused user is silently skipped instead of listed in `missing_invitees`; nil panic |
| ChatInvite | `messages.addChatUser` | ok, with gaps | `messages.addChatUser_handler.go:71-97` returns `USER_PRIVACY_RESTRICTED`, which Android handles (`MessagesController.java:15528`); nil panic |
| ChatInvite | `channels.inviteToChannel` | n/a | channels not built yet |
| PhoneCall / PhoneP2P | `userFull.phone_calls_private`, `phone.requestCall` | n/a or missing | no phone service; `phone_calls_private` always `false` (`getFullUser:80`) |
| SavedMusic | `userFull.saved_music` | bug | `:236-262`: chat callback `false` |
| SavedMusic | `users.getSavedMusic` / `getSavedMusicByID` | **missing (leak)** | `users.getSavedMusic_handler.go:29-60`: no check |
| StarGiftsAutoSave | gift auto-save | n/a | stored only; `payments.getSavedStarGifts` is `ErrEnterpriseIsBlocked` (`payments.getSavedStarGifts_handler.go:59`) |
| NoPaidMessages | paid-message exceptions | n/a | no paid messages (`send_paid_messages_stars` nil, `getFullUser:121`) |
| Global: `new_noncontact_peers_require_premium` | send, getFullUser, getRequirementsToContact | ok | `pkg/premium/premium.go`; `msg.sendMessageV2_handler.go:136-144` |
| Global: `archive_and_mute_new_noncontact_peers` | first message from a non-contact | missing | stored only; no use in `app` or `pkg` (grep) |
| Global: `keep_archived_unmuted` / `keep_archived_folders` | new message in an archived chat | missing | stored only; no unarchive-on-message logic found (grep of `app/messenger`, `dialog`) |
| Global: `hide_read_marks` | getOutboxReadDate and `read_dates_private` | missing | see above |
| Global: `noncontact_peers_paid_stars`, `disallowed_gifts`, `display_gifts_button` | storage | bug | `user.setGlobalPrivacySettings_handler.go:34-41` drops them, but the bff echoes the input back as if saved (`account.setGlobalPrivacySettings_handler.go:47-56`) |
| Global: get | read | bug | `user.getGlobalPrivacySettings_handler.go:43-49`: a DB error returns the defaults silently |
| `account.getPrivacy` | read | bug | `account.getPrivacy_handler.go:42-67`: an error is swallowed and the default (e.g. "Everybody") is shown |

## 2. Bugs with concrete scenarios

### B1. Evaluator A: "Nobody (or Contacts) + any exception" becomes "Everybody"

This is the most severe bug.
- **Rules Android sends.** When there are "always allow" exceptions, Android always adds `AllowUsers` **and** `AllowChatParticipants`, even with an empty list (`PrivacyControlActivity.java:1072-1088`).
- **Scenario.**
  1. On Android, A sets "Profile photo: Nobody, except B".
  2. The stored rules are `[AllowUsers[B], AllowChatParticipants[], DisallowAll]`.
  3. For a stranger C, `privacyIsAllow` misses on `AllowUsers` and reaches `AllowChatParticipants` → `return true` (`privacy_rule_helper.go:408-409`).
  4. C sees the photo. The same happens for the phone number and last seen.
- **The same with Contacts:** "My contacts, except B" (`:386-387`).
- **Web K** puts chat rules before user rules (`components/privacySection.tsx:288-338`). Any chat exception, real or not, opens the key to everyone, and also skips a later `DisallowUsers` (`:394-395` returns `true` before reaching it).
- **Status:** the ADR 0030 push uses this evaluator, so last seen leaks the same way.

### B2. "Everybody except members of chat X" does nothing
- **Evaluator A:** under `ALLOW_ALL`, `DisallowChatParticipants` returns `true` (`:372-373`).
- **Evaluator B:** call sites whose chat callback returns `false` behave the same way: birthday, voice messages, saved music, contacts.getBirthdays.
- **Scenario:** A hides their birthday from group G under "Everybody except G". Members of G still see it in `getFullUser`.
- **Fail-closed side:** "Contacts + always share with G" never lets G's non-contacts see it, in those call sites.

### B3. `contacts.resolvePhone`: AddedByPhone is evaluated in reverse
- **The call:** `CheckPrivacyIsAllow(c.MD.UserId, rules, id.GetV(), ...)` (`contacts.resolvePhone_handler.go:66-77`). Owner and viewer are swapped.
  - The contact callback checks `resolved.CheckContact(resolved)`, which is always `false`.
  - User exceptions are compared with the resolved user's own id.
- **Scenario 1:** A sets "Who can find me by number: My contacts". A's contact B calls `contacts.resolvePhone(A's number)` and gets `PHONE_NOT_OCCUPIED`.
- **Scenario 2:** "Everybody except B". B still finds A.
- **Debug log:** the denial path also logs `err`, which is nil there (`:79`).

### B4. Profile photo leaks past privacy
- **`userFull.profile_photo`:** `users.getFullUser_handler.go:93` puts the real `Photo` in `userFull.profile_photo` for every viewer.
  - Scenario: A sets "Profile photo: Nobody". C opens A's profile. `user.photo` is empty, but `full_user.profile_photo` carries the photo; clients render it in the profile.
- **`photos.getUserPhotos`:** returns A's whole album to anyone (`photos.getUserPhotos_handler.go:39-62`).

### B5. Nil-pointer panics on a privacy read error
- `rules.Datas` is read from a possibly-nil `*Vector_PrivacyRule`:
  - `messages.forwardMessages_handler.go:120`;
  - `messages.createChat_handler.go:101`;
  - `messages.addChatUser_handler.go:75`.
- A user-service hiccup crashes the request (500 → tgnet retry) instead of returning a clean error.
- The `len == 0` branch in createChat (`:101-106`) would add the user to `missing_invitees` **and** still add them to the chat. It is dead code today, but wrong.

### B6. `messages.createChat` loses privacy-refused users
- **What Pluma does:** a user who refuses invites is dropped with `continue` (`:117-120`) and is not in `missing_invitees`.
- **What is expected:**
  - core.telegram.org/constructor/missingInvitee: when neither flag is set, "we could not add the user because of their privacy settings".
  - core.telegram.org/api/invites: the client then offers an invite link.
  - Android shows that sheet from `missing_invitees` after `createChat` (`MessagesController.java:15020-15028` → `AlertsCreator.checkRestrictedInviteUsers`, `AlertsCreator.java:3650`).
- **Scenario:** A creates a group with B and C; C allows invites from Nobody. The group is created without C, and A gets no "invite via link" prompt.
- **If every invitee refuses,** Pluma answers `USERS_TOO_FEW`. Whether Telegram then creates the group and lists everyone as missing is UNVERIFIED.

### B7. The fallback ("public") photo replaces the real photo
- `photos.uploadProfilePhoto` and `photos.updateProfilePhoto` ignore `fallback:flags.3?true`.
- **Scenario:**
  1. A sets "Profile photo: Contacts".
  2. A uploads a "public photo" for everyone else.
  3. Pluma makes it A's **main** photo, so A's contacts lose the real one.
  4. Non-contacts see nothing, where core.telegram.org/constructor/userFull says `fallback_photo` is "displayed if no photo is present in profile_photo or personal_photo, due to privacy settings".

### B8. `ToUser` evaluates "My contacts" against an incomplete contact set (PLAUSIBLE, from code reading)
- **Where:** `CheckPrivacy` asks `m.Contacts`, the owner's own rows (`immutable_user_helper.go:285`).
- **What is loaded:** in group sends, users come from `UserGetMutableUsersV2{Privacy: true, HasTo: true}` with an empty `To` (`msg.sendMessageV2_handler.go:251-255`). That loads only rows of the form "someone has m" (`dao/user.go:768-781`, `addContactRow:868-886`), never "m has the recipient".
- **Scenario:**
  1. S has photo privacy "My contacts" and writes in group G.
  2. Member R is in S's contacts.
  3. `usersFor(R)` → `ToUser(R)` gives S without a photo.
  4. R's client receives a non-min user with an empty photo and drops S's cached avatar.
- **To confirm:** needs an integration test.

### B9. Fail-open paths
- `CheckPrivacy` allows when the key's rules are absent (`immutable_user_helper.go:281-283`).
- The user cache stores no rules when `GetUserPrivacyRules` fails (`cache_user_data.go:301-308`, errors ignored). Phone and photo are then shown to everyone until the cache expires.
- `getFullUser` voice messages (`:217`) treat an error as "allowed".

### B10. Exceptions Pluma ignores
- **`AllowPremium` is ignored.** iOS appends it after the base rule (`UpdatedAccountPrivacySettings.swift:557-560,578-580`); Android at `PrivacyControlActivity.java:1119-1121`; tdesktop at `api_user_privacy.cpp:68-70`. Scenario: "Last seen / Forwards / Invites: Contacts + Premium users". Non-contact Premium users are still refused.
- **`Allow/DisallowBots`** ("mini apps", same client lines) are ignored. No bots exist today, so impact is low.

### B11. `DisallowContacts`: low priority
- No official client creates this rule:
  - tdesktop: `// Not supported`, `api_user_privacy.cpp`, in `TLToRules`;
  - iOS ignores it on parse (`PrivacySettings.swift:246`);
  - Android has no use outside `tgnet/`;
  - Web K only `AllowAll`, `AllowContacts`, `DisallowAll` plus exceptions (`privacySection.tsx:290-337`).
- The owner's "everybody except contacts" is therefore really B1 or B2. Handle `DisallowContacts` anyway, for completeness.

### B12. Voice messages are not enforced on the server
- core.telegram.org/method/messages.sendMedia lists `400 VOICE_MESSAGES_FORBIDDEN` ("This user's privacy settings forbid you from sending voice messages").
- Pluma relies only on the client greying the button (`ChatActivityEnterView.java:6513`).
- A modified or older client can still send voice messages and round videos.
- The key is "Premium users only" (core.telegram.org/constructor/inputPrivacyKeyVoiceMessages). Android offers Contacts or Nobody only with Premium (`PrivacyControlActivity.java:653`).
- Whether the restriction lapses when the owner's Premium ends is UNVERIFIED.

### B13. `getOutboxReadDate` and `read_dates_private` are not implemented
- **What is required** (core.telegram.org/method/messages.getOutboxReadDate and `constructor/globalPrivacySettings`):
  - `403 USER_PRIVACY_RESTRICTED` when the recipient has `hide_read_marks` and the caller cannot see their exact last seen;
  - `403 YOUR_PRIVACY_RESTRICTED` when the caller hides their own last seen and has no Premium;
  - `userFull.read_dates_private`. Android hides "seen at" when it is set (`ChatActivity.java:30860`).
- Pluma has none of these.

### B14. Global settings
- **Archive and mute:** `archive_and_mute_new_noncontact_peers` is never applied. Scenario: with it on, a first message from a stranger lands in the main list, unmuted.
- **Unarchiving:** nothing found that unarchives archived chats on a new message, so `keep_archived_unmuted` and `keep_archived_folders` have no effect either way. Whether Pluma unarchives at all needs a check.
- **Dropped fields:** stars, `disallowed_gifts` and `display_gifts_button` are dropped but echoed back as saved.

### B15. `account.getPrivacy` misreports on error
On error it returns the default, e.g. "Everybody". A user can see a wrong setting and save it (`account.getPrivacy_handler.go:42-67`).

## 3. Data the evaluator needs

One evaluator replaces both A and B, and every call site uses it.

1. **Owner rules per key.** From `user_privacies`, with per-key defaults. The cache must carry all keys read on hot paths: phone, photo and status today; About and Birthday should join them for `ToUnsafeUser` and `getFullUser`. Missing rules must fail **closed**, or fail the request.
2. **Viewer id.** The owner always sees their own data.
3. **"Viewer is the owner's contact".** Contact rows are `user_contacts(owner_user_id = owner, contact_user_id = viewer, is_deleted = 0)`, cached as `ContactIdList` in user data (`user.checkContact_handler.go:19-24`). Every `ToUser` and `ToUnsafeUser` caller must load the owner→viewer row (see B8). Simpler: decide it from the owner's cached `ContactIdList`.
4. **Viewer's membership in the rule's chats.**
   - **Basic groups:** `chat_participants.state = 0` with `chats.deactivated = 0` (`chat_participants_dao.go:651-664`, through `chat.getUsersChatIdList`; helper `CheckParticipantIsExist`, `chat/client/chat_client_helper.go:49-70`).
   - **Batching for one viewer** (getDialogs, getHistory: many owners): load the viewer's chat ids once and intersect in memory.
   - **Batching for fan-out** (status push: one owner, many viewers): one query `chat_participants WHERE user_id IN (viewers) AND chat_id IN (rule chats)`.
   - **Open questions:**
     - Must the owner also still be a member? (UNVERIFIED.)
     - Basic groups migrated to supergroups, and channels: needed once channels exist.
5. **Viewer's Premium** (`ImmutableUser.Premium()`), for `AllowPremium` and for the status exemption with `by_me`.
6. **Viewer is a bot** (`IsBot()`), for `Allow/DisallowBots`.
7. **Close friends** (`ContactData.CloseFriend`), only for story privacy; reject it in `account.setPrivacy` for other keys.
8. **For status:** the viewer's own StatusTimestamp rule (reciprocity) and the viewer's Premium.
9. **For read dates:** the owner's `hide_read_marks` plus the StatusTimestamp result. Also the caller's own `hide_read_marks` and Premium, for `YOUR_PRIVACY_RESTRICTED`.
10. **Rule order and precedence** (decision to confirm).
    - **What the clients do:**
      - Android, iOS and tdesktop send exceptions first and the base rule last; Web K sends the base rule first.
      - All of them send user exceptions and chat exceptions as separate rules.
      - tdesktop takes the **first** base option it meets and defaults to "Nobody" (`api_user_privacy.cpp`, `TLToRules`, `feed(MTP_privacyValueDisallowAll())`). Pluma takes the **last** one.
    - **Proposal** (UNVERIFIED against Telegram; settle with a two-tab probe):
      - one base rule, with no base meaning "Nobody";
      - explicit user exceptions beat chat-member exceptions;
      - empty lists are ignored;
      - the result must not depend on the order of the rules in the list.

## Open questions for a probe against real Telegram (`reference-telegram-probe`)
- When a user is in an allowed chat and also individually disallowed, or the other way round, which rule wins?
- What does `createChat` return when every invitee refuses?
- Are `user.photo` and `phone` hidden when blocked?
- Do other users' caches get refreshed (`updateUser`) after a privacy change?
- When I imported someone's phone, do I get `user.phone` regardless of their setting?
- Does the voice-message restriction lapse when the owner's Premium ends?
- Is `forwards` evaluated against the forwarder or against each reader?