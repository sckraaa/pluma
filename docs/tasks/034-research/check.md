# Adversarial check of the three privacy research reports

Research only. Nothing was edited, committed, deployed or run in Docker. No secrets were read.

**Sources:**
- Server: paths are relative to `/Users/sckr.a/teamgram-server`; `proto/` means `third_party/teamgram-proto/mtproto/`.
- Clients: the pinned trees under `/Users/sckr.a/pluma-work/reference-src/{android,ios,tdesktop,tweb}`.
- Docs:
  - I fetched two pages live with WebFetch: core.telegram.org/constructor/globalPrivacySettings and core.telegram.org/method/messages.createChat.
  - All other doc claims were checked against the schema report's raw-markdown copies in `/private/tmp/claude-503/-Users-sckr-a-teamgram-server/76bbed60-70da-4b26-9bff-19b96e18ecba/scratchpad/docs/`.
  - The two live pages matched those copies.

## 1. Spot-checks (38 claims)

### Server code

| # | Claim (report) | Verdict | Evidence or correction |
|---|---|---|---|
| 1 | `privacyIsAllow` returns `true` on any `AllowChatParticipants` (schema §4, audit B1) | CONFIRMED, and it is worse than described | `proto/privacy_rule_helper.go:348-416`. Three more effects: **(a)** under ALLOW_CONTACTS, `DisallowChatParticipants` also returns `true`, so "My contacts, except chat X" lets everyone see. **(b)** Under ALLOW_ALL, a `DisallowChatParticipants` rule placed before a `DisallowUsers` rule returns before the user list is read, so the user exclusion is lost. **(c)** Under DISALLOW_ALL, an empty `AllowChatParticipants` returns `true`. |
| 2 | `CheckPrivacyIsAllow` takes the *last* base rule, and within one base the result depends on rule order (schema §4) | CONFIRMED | `privacy_rule_helper.go:256-336`. The loop overwrites `ruleType`, and every case returns at the first matching rule. |
| 3 | `CheckPrivacy` allows when no rules are stored (fail-open) (audit B9) | CONFIRMED | `proto/immutable_user_helper.go:265-289`, the `if rules == nil { return true }` branch. |
| 4 | Defaults with no stored row: PhoneNumber=DisallowAll, Birthday=AllowContacts, every other key AllowAll (audit §0) | CONFIRMED | `app/service/biz/user/internal/dao/vars.go:76-84` and the vars above it. `account.getPrivacy_handler.go:47-66` repeats the same table. |
| 5 | `user.checkPrivacy` "always answers boolTrue" (audit) | Mostly right; a correction | It returns `BoolFalse` when the read fails and `BoolTrue` in every other case (`user.checkPrivacy_handler.go:20-33`). It is still a trap for future callers. |
| 6 | `contacts.resolvePhone` evaluates AddedByPhone with owner and viewer swapped (audit B3) | CONFIRMED | `app/bff/users/internal/core/contacts.resolvePhone_handler.go:61-81`. The audit gave `app/bff/contacts/...`; the file is under `app/bff/users/`. The call is `CheckPrivacyIsAllow(c.MD.UserId, rules, id.GetV(), …)`, and its contact callback runs `resolved.CheckContact(resolved's own id)`. |
| 7 | `userFull.profile_photo` and `userFull.about` are always the real values (audit B4) | CONFIRMED | `users.getFullUser_handler.go:90,93`. Nothing later in the handler changes them. `FallbackPhoto` is always nil (`:94`). `PhoneCallsPrivate` is always false (`:80`). |
| 8 | `photos.getUserPhotos` has no privacy check (audit B4) | CONFIRMED | `app/bff/userchannelprofiles/internal/core/photos.getUserPhotos_handler.go:29-62`. |
| 9 | `createChat` drops a refused invitee with `continue`, and dereferences `rules.Datas` on a possibly-nil pointer (audit B5/B6) | CONFIRMED | `app/bff/chats/internal/core/messages.createChat_handler.go:97-121`. The same nil dereference is in `messages.addChatUser_handler.go:75` and `messages.forwardMessages_handler.go:121`. |
| 10 | `account.getPrivacy` swallows a read error and returns the default (audit B15) | CONFIRMED | `account.getPrivacy_handler.go:42-46`: `ruleList, _ :=`. A nil list then takes the default branch. |
| 11 | `FromInputPrivacyKeyType` and `ToPrivacyRuleByInput` call `panic` (schema §4) | CONFIRMED, with a nuance | `privacy_rule_helper.go:146,182,222`. The `KEY_TYPE_INVALID` check in `account.setPrivacy_handler.go:35` is therefore dead code. The panic can only be reached through a constructor the TL decoder already accepted, or a nil key, so it is a latent bug rather than a live one. |
| 12 | The status push uses Evaluator A (audit, `account.updateStatus_handler.go:171-172`) | CONFIRMED, plus one gap no report noted | The push is skipped entirely when *the viewer's own* StatusTimestamp rule hides the viewer from the owner (`:166-167`). Telegram's documented behaviour is to deliver a bucketed status with `by_me` instead (privacyKeyStatusTimestamp page, userStatusRecently line 29). There is also no Premium exemption. |
| 13 | `getOutboxReadDate` has no privacy checks (audit B13) | CONFIRMED | `messages.getOutboxReadDate_handler.go:55-56` (`// TODO: 1. Check USER_PRIVACY_RESTRICTED 2. Check YOUR_PRIVACY_RESTRICTED`). |
| 14 | The user-data cache swallows errors when it reads rules (audit B9) | CONFIRMED | `dao/cache_user_data.go:300-308` (`rules0, _ = …`). |
| 15 | `getFullUser` voice-message check: the chat callback is always `false` and an error is treated as "allowed" (audit §1) | CONFIRMED, plus one item | `users.getFullUser_handler.go:211-234`. Not reported: the check also runs when the viewer is the owner (no `peerId != c.MD.UserId` guard). With Voice = Nobody, an owner viewing their own profile gets `voice_messages_forbidden`. |

### Android

| # | Claim (report) | Verdict | Evidence or correction |
|---|---|---|---|
| 16 | Rule order and empty vectors: AllowUsers and AllowChatParticipants are both sent even when one list is empty; base rule next; then AllowPremium and Bots (clients §1.1, audit B1) | CONFIRMED | `PrivacyControlActivity.java:1072-1126`. Both rule objects are added whenever `currentPlus`/`currentMinus` is non-empty. Chat ids are sent as `-dialogId`, which is the positive bare id. |
| 17 | Empty rules read as Nobody; `[AllowContacts, DisallowAll]` reads as Contacts (all three reports) | CONFIRMED | `PrivacyControlActivity.java:1277-1278` and 1311-1315. AllowAll or AllowContacts appearing later overrides an earlier base rule. |

### tdesktop

| # | Claim (report) | Verdict | Evidence or correction |
|---|---|---|---|
| 18 | Send order is AllowUsers, AllowChats, AllowPremium, AllowBots, DisallowUsers, DisallowChats, DisallowBots, then the base rule (schema §3) | CONFIRMED | `api/api_user_privacy.cpp:56-102`. The clients report's "Allow exceptions, Disallow exceptions, base last" is the same thing. |
| 19 | On parse, the first base rule wins, `DisallowAll` is fed as the default, a peer in both lists stays in the first list, chats that are not loaded are dropped, and `DisallowContacts` is "Not supported" (all three reports) | CONFIRMED | `api_user_privacy.cpp:104-196`. |
| 20 | Local last-seen downgrade thresholds of 3, 7 and 30 days, then `contacts.getStatuses` (schema, clients) | CONFIRMED | `apiwrap.cpp:2127-2160`. This is client-side only. |
| 21 | On `updatePrivacy`, tdesktop reloads the key with `getPrivacy` if any chat in the rules is not loaded (clients §2) | CONFIRMED | `api/api_updates.cpp:2316-2345` together with `api_user_privacy.cpp` `apply()`/`reload()`. |

### iOS

| # | Claim (report) | Verdict | Evidence or correction |
|---|---|---|---|
| 22 | Order: Nobody is sent as exceptions, `DisallowAll`, then CloseFriends, Premium and Bots (both reports) | CONFIRMED | `UpdatedAccountPrivacySettings.swift:546-601`. |
| 23 | "iOS: the last base rule wins" (clients §2) | **WRONG** | In `Settings/PrivacySettings.swift:222-260`, `privacyValueDisallowAll` is `break` and changes nothing. The default is `.disableEveryone`; `AllowAll` and `AllowContacts` overwrite the current value. So `[AllowContacts, DisallowAll]` reads as Contacts on iOS, as on Android, and the last of AllowAll/AllowContacts wins. `DisallowContacts` is ignored as well. |
| 24 | The privacy screen loads with one `combineLatest` over the 14 keys, `getAccountTTL`, `getGlobalPrivacySettings` and `getDefaultHistoryTTL`; any error ends it empty (clients §2) | CONFIRMED | `UpdatedAccountPrivacySettings.swift:58-79`. Pluma has handlers for `account.getAccountTTL` and `messages.getDefaultHistoryTTL`; I did not check whether they succeed. |
| 25 | iOS does not handle `updatePrivacy` (clients §2) | CONFIRMED | No `updatePrivacy` match in `submodules/` outside the generated Api files. |
| 26 | AddedByPhone is read as "on" only when `AllowAll` is present, and is written as `AllowAll` or `AllowContacts` (clients §1.1, schema) | CONFIRMED | `UpdatedAccountPrivacySettings.swift:170-182` and `:458-465`. |

### Web K

| # | Claim (report) | Verdict | Evidence or correction |
|---|---|---|---|
| 27 | Base rule first, then chats, users, bots for allow and then for disallow; parse takes `types[0]` (all three reports) | CONFIRMED | `components/privacySection.tsx:288-338`; `getPrivacyRulesDetails.ts:45`. Web K never sends `AllowPremium`. |

### Docs and schema

| # | Claim (report) | Verdict | Evidence or correction |
|---|---|---|---|
| 28 | `AllowPremium` is "currently only usable for inputPrivacyKeyChatInvite"; `AllowCloseFriends` "can be used only when posting stories" (schema §3) | CONFIRMED | `constructor_privacyValueAllowPremium.md:3`; `privacy.md:47`. |
| 29 | setPrivacy errors: PRIVACY_KEY_INVALID, PRIVACY_TOO_LONG ("limit is 1000"), PRIVACY_VALUE_INVALID (schema §1) | CONFIRMED | `method_account.setPrivacy.md:27-29`. "Too many privacy rules" leaves open whether rules or ids are counted. |
| 30 | getOutboxReadDate errors (400 MESSAGE_ID_INVALID, NOT_READ_YET, TOO_OLD, PEER_ID_INVALID; 403 USER_ and YOUR_PRIVACY_RESTRICTED) and how clients map them (schema, clients §4) | CONFIRMED | `method_messages.getOutboxReadDate.md:33-38`; tdesktop `api_who_reacted.cpp:304-316`; Android `MessagePrivateSeenView.java:157-170`. |
| 31 | Error codes on send paths: VOICE_MESSAGES_FORBIDDEN is 400 on sendMedia and 403 on forwardMessages; PRIVACY_PREMIUM_REQUIRED is 406 on sendMessage and forwardMessages and 403 on sendMedia; SCHEDULE_STATUS_PRIVATE is 400 (schema §1) | CONFIRMED | `method_messages.sendMedia.md:126,162`; `method_messages.forwardMessages.md:87,108`; `method_messages.sendMessage.md:103,118`. |
| 32 | `createChat`, `addChatUser` and `inviteToChannel` all return `messages.InvitedUsers` (clients §4) | CONFIRMED | tdesktop `api.tl:2479,2481,2781`; live createChat page. Correction: the createChat error list has **no** USER_PRIVACY_RESTRICTED; addChatUser (`method_messages.addChatUser.md:42`) and inviteToChannel (`:46`) both list 403 USER_PRIVACY_RESTRICTED. See contradiction C1. |
| 33 | The `hide_read_marks` wording (schema §2) | CONFIRMED | Live globalPrivacySettings page: "the inputPrivacyKeyStatusTimestamp key will also apply to the ability to use messages.getOutboxReadDate on messages sent to us"; `read_dates_private` "will be set for users that have this flag enabled". |
| 34 | `user` has no `birthday` field in layer 228, so api/profile's `user.birthday` is a doc slip (schema) | CONFIRMED | `user#31774388` in `privacy.md:36` has no birthday field; `userFull` has `birthday:flags2.5`. |
| 35 | 14 keys and 12 rule constructors in layer 228 (schema §1) | CONFIRMED | tdesktop `api.tl:583-596`; there are 26 `inputPrivacyKey*`/`inputPrivacyValue*` lines in all. |

### Android call and status handling

| # | Claim (report) | Verdict | Evidence or correction |
|---|---|---|---|
| 36 | Android treats any 403 from requestCall as a privacy error, and shows the call button only when `phone_calls_available` is set (clients §3) | CONFIRMED | `VoIPService.java:1155-1156` (`code == 403` → `ERROR_PRIVACY`); `ChatActivity.java:23808`. |
| 37 | `by_me` is stored as expires −1000, −1001 or −1002 (clients §3) | CONFIRMED | `ContactsController.java:2615-2619`. Without `by_me` the values are −100, −101 and −102. |
| 38 | Android locks Contacts and Nobody for voice messages on non-Premium accounts (schema, clients) | CONFIRMED | `PrivacyControlActivity.java:653`. |

**Tally:** 35 confirmed (some with additions), 1 wrong (#23, the iOS parse), 2 partly corrected (#5 the `user.checkPrivacy` RPC; #32's missing nuance about errors). One path was wrong (the resolvePhone file is under `app/bff/users`).

## 2. Contradictions between the reports

| ID | Topic | Disagreement | Assessment |
|---|---|---|---|
| C1 | `addChatUser` when the invitee's privacy refuses | **Audit:** returning 403 USER_PRIVACY_RESTRICTED is "ok". **Clients §7.17:** return `invitedUsers` with `missing_invitees` and never fail; use the error only for single-target actions such as editAdmin. **Schema:** single-user paths give 403. | The docs list the 403 for addChatUser and inviteToChannel, and api/invites says they return missing invitees. Android handles both forms (`MessagesController.java:15515-15535`: an error callback for USER_PRIVACY_RESTRICTED plus collection of `missing_invitees`). Which one Telegram sends is **UNVERIFIED**; probe it. createChat lists no such error, so a missing invitee is the only option there. |
| C2 | `read_dates_private` per viewer or global | **Clients §7.18:** set it "when the target has hide_read_marks" (reads as global). **Schema open point 5 and audit:** per viewer. | The doc says "set for users that have this flag enabled". Android `HideReadTimeInfo` (strings.xml:7628) says "from people who can't see your last seen", which means per viewer. Unresolved; probe. |
| C3 | Condition for `YOUR_PRIVACY_RESTRICTED` | **Audit and clients:** the caller hides their own last seen and has no Premium. | Android `HideReadTimeInfo` says the penalty follows from turning *Hide Read Time* on ("their read time will also be hidden from you"), and the doc says "you have disallowed other users to do so for your messages". The likely condition also needs the caller's own `hide_read_marks` (and their StatusTimestamp denying that peer). **UNVERIFIED.** |
| C4 | Premium in Pluma | **Schema §5.12 and clients §8:** an open owner question. **Audit:** Premium exists, granted by the admin. | The audit is right: `users.getFullUser_handler.go:130` notes "the admin grants it", and `pkg/premium` exists. The question is settled. Every Premium-dependent rule (by_me exemption, AllowPremium, voice restriction, missingInvitee flags) must be implemented for real. |
| C5 | `AllowCloseFriends` on account keys | **Clients §7.7:** store and evaluate it, no special case. **Audit:** reject it for keys other than stories. | The docs say "can be used only when posting stories". Rejecting it with `PRIVACY_VALUE_INVALID` matches that, but the exact error is UNVERIFIED. |
| C6 | iOS base-rule parse | **Clients:** last wins. | Wrong; see #23. All four clients read `[AllowContacts, DisallowAll]` as Contacts or tolerate it; tdesktop and Web K take the first base rule. |
| C7 | Where the voice-message check belongs | **Schema:** sendMedia gives 400, forwardMessages gives 403. **Clients §7.15:** "400 assumed". | The docs give different codes per method (confirmed, #31). Use the per-method code. |

## 3. Enforcement points and keys none of the reports covered

| Gap | Where in Pluma | Notes |
|---|---|---|
| `contacts.search` by phone number | `app/bff/contacts/internal/core/contacts.search_handler.go:107-117,181-185` | A user found by phone is gated on PhoneNumber visibility through `GetUnsafeUser` (Evaluator A, so it leaks with B1/B2). AddedByPhone is never consulted. Which key Telegram uses for search by number is UNVERIFIED (Android `PrivacyPhoneInfo` ties finding by number to AddedByPhone). |
| Downloading a hidden profile photo | `app/bff/files/internal/core/upload.getFile_handler.go:77-81` and `dfs.downloadFile_handler.go:129` | `inputPeerPhotoFileLocation` and `inputPhotoFileLocation` have no ProfilePhoto check. A viewer with a cached `photo_id` keeps downloading the photo after access is withdrawn. What Telegram does here is UNVERIFIED (the file_reference mechanism). |
| Status push reciprocity | `account.updateStatus_handler.go:166-167` | The push is dropped instead of sent as a bucketed status with `by_me`; there is no Premium exemption (see #12). |
| Owner's own `getFullUser` | `users.getFullUser_handler.go:211-234` | Voice and saved-music checks run with viewer = owner. |
| Online window | not in any report | The owner's request sets the "online" window to at most 60 s after the last device signal, with repeated signals in the window handled correctly. None of the three privacy reports addresses it. It belongs to the same status work (ADR 0030 area). |
| Peer settings | `messages.getPeerSettings_handler.go`, `dao/user_peer_settings.go` | The schema report requires `need_contacts_exception` when the phone is hidden. The audit did not check Pluma's logic; `NeedContactsException` is referenced there and in addContact and deleteContacts. Not verified. |
| Re-forwarding and replies | `messages.forwardMessages_handler.go:315-334` (`forwardOrigin`) | No report says whether forwarding an already-forwarded message re-evaluates the original author's Forwards rule or copies the stored header. The same question applies to `reply_from` headers. Pluma evaluates against the forwarder at forward time. Telegram's behaviour is UNVERIFIED. |
| Blocked viewers | — | Do blocked users see photo, status and phone? All reports mention it only as a probe. Today Pluma consults blocks only in the status push. |
| `messages.getOnlines` | `messages.getOnlines_handler.go` | A stub that returns 1, so nothing leaks today. Once implemented, it must respect StatusTimestamp (UNVERIFIED how Telegram counts hidden users). |
| Group read receipts | `messages.getMessageReadParticipants*` | Android says Hide Read Time "does not affect group chats" (strings.xml:7628), so leave them unaffected. Confirm no one adds a check there. |
| Missing keys | — | None. All 14 keys are covered by at least one report. |

## 4. Decisions an implementer still has to make

1. **One evaluator, independent of rule order** (every report agrees, and no doc gives the order). Proposed precedence:
   1. viewer is the owner;
   2. DisallowUsers and AllowUsers;
   3. Disallow and Allow ChatParticipants (live membership, empty lists ignored);
   4. AllowPremium, AllowBots, DisallowBots;
   5. the single base rule;
   6. no base rule means deny.

   Decide the tie-breaks (a user in AllowUsers who is also in a disallowed chat; a user in both an allowed and a disallowed chat) and confirm them with the Web K two-tab probe. Replace both Evaluator A and Evaluator B, and the `user.checkPrivacy` RPC, with it.
2. **Base-rule normalisation on write.** Store and return exactly one base rule plus the non-empty exceptions. A rule list with both `AllowAll` and `DisallowAll`, or with no base rule: normalise it, or reject it with `PRIVACY_VALUE_INVALID`?
3. **Validation in `account.setPrivacy`.** Decide:
   - whether `AllowPremium` is allowed on keys other than ChatInvite (all three native clients send it for other keys; the docs say ChatInvite only, so rejecting it would break client flows: accept it);
   - whether `AllowCloseFriends` on account keys is rejected;
   - how PRIVACY_TOO_LONG is counted;
   - whether chat ids the owner is not a member of are rejected, and how ids above `MinNebulaChatChannelID` are resolved;
   - a Premium gate for restricting VoiceMessages (tdesktop expects `PREMIUM_ACCOUNT_REQUIRED`, `api_user_privacy.cpp:282-286`).
4. **Membership semantics for chat rules.** Must the owner still be a member of the chat? How are migrated basic groups handled? Is a deactivated chat a match?
5. **Meaning of "contact".** The owner has the viewer in their contacts (one-way) or mutual. Pluma uses one-way rows, and it must load the owner→viewer row on every path (audit B8, not yet confirmed).
6. **Fail-closed policy.** Missing or unreadable rules lead to the per-key default, or to a failed request, never to allow. `getPrivacy` must return an error, never a misleading default; iOS then loses the whole privacy screen, which is still better than wrong data the user may save.
7. **Status.**
   - The bucket thresholds on the server: adopt tdesktop's 3/7/30 days, UNVERIFIED as Telegram's server rule.
   - What a hidden user who is online now looks like: Recently.
   - Reciprocity with `by_me`, and the Premium exemption.
   - Which viewers get `updateUserStatus` or `updateUser` after a privacy change, and the 60 s online window.
8. **Read dates.** Settle C2 and C3. Also decide the `MESSAGE_TOO_OLD` window and publish `pm_read_date_expire_period` in app config.
9. **Invites.** Settle C1 for addChatUser. For createChat: still create the chat when all invitees refuse, or answer `USERS_TOO_FEW` (UNVERIFIED). Put refused users in `updates.users` so tdesktop and iOS do not drop them.
10. **Profile photo.** Whether to implement the fallback ("public") photo now (`photos.uploadProfilePhoto` `fallback` flag, `userFull.fallback_photo`) and the personal photo. The decision also covers the `apply_min_photo` rule and the file-download gate from §3.
11. **Phone number.** How `add_phone_privacy_exception` and `acceptContact` store the exception, and whether a viewer who imported the owner's number sees it under AddedByPhone (Android `PrivacyPhoneInfo` says yes; the server rule is UNVERIFIED).
12. **Forwards perspective.** Evaluate against the forwarder (what Pluma does now) or against the reader, and whether re-forwards re-evaluate the rule.
13. **Global settings.** Keep stored fields the client leaves out (Android non-Premium and Web K omit `disallowed_gifts` and the stars amount). Decide whether to implement or hide `archive_and_mute_new_noncontact_peers` (`autoarchive_setting_available`).
14. **Keys whose feature does not exist yet** (PhoneCall, PhoneP2P, StarGiftsAutoSave, NoPaidMessages, story rules). Store and return the rules correctly now. Mark enforcement as not applicable in STATUS rather than faking it.