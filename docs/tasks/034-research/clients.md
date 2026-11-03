
# What the official clients expect from the server for privacy (layer 228)

Sources read are pinned:
- **A** = Android `TMessagesProj/src/main/java/org/telegram/...`
- **I** = iOS `submodules/...`
- **D** = tdesktop `Telegram/SourceFiles/...`
- **W** = Web K `src/...`

Docs fetched:
- core.telegram.org/api/privacy
- /type/InputPrivacyKey
- /constructor/userStatusRecently
- /constructor/userFull
- /constructor/globalPrivacySettings
- /constructor/missingInvitee
- /method/messages.getOutboxReadDate

Schema checked in D `mtproto/scheme/api.tl` (layer 228, line 3087).

Read-only research: no code was edited or run, and no Docker was used.

---

## 1. account.setPrivacy: how each client builds the rules

### 1.1 Keys offered, and the order of rules

| | Android | iOS | tdesktop | Web K |
|---|---|---|---|---|
| Keys | 14: StatusTimestamp, ChatInvite, PhoneCall, PhoneP2P, ProfilePhoto, About, SavedMusic, Forwards, PhoneNumber, VoiceMessages, Birthday, StarGiftsAutoSave, NoPaidMessages, AddedByPhone (A `messenger/ContactsController.java:2683-2722`) | The same 14 keys (I `TelegramCore/.../Privacy/UpdatedAccountPrivacySettings.swift:58-71`) | The same 14 keys (D `api/api_user_privacy.cpp:198-217`) | One tab per key (W `components/sidebarLeft/tabs/privacy/*.tsx`) |
| Order of rules sent | AllowUsers, AllowChatParticipants, DisallowUsers, DisallowChatParticipants, **then the base rule**, then AllowPremium / AllowBots (A `ui/PrivacyControlActivity.java:1072-1126`) | Exceptions, then the base rule, then **AllowCloseFriends / AllowPremium / AllowBots after DisallowAll** (I `UpdatedAccountPrivacySettings.swift:546-601`) | Allow exceptions, Disallow exceptions, then **the base rule last** (D `api_user_privacy.cpp:56-102`) | **Base rule first**, then the exceptions (W `components/privacySection.tsx:290-341`) |
| Empty vectors | **Sends an empty `AllowChatParticipants{chats:[]}` or `AllowUsers{users:[]}`** whenever the exception list has only users or only chats (`PrivacyControlActivity.java:1089-1090, 1108-1109`) | Skipped when empty | Skipped when empty | Skipped when empty |
| Exceptions dropped by the client | Allow exceptions dropped when Everybody, Disallow exceptions dropped when Nobody (`:1072, :1092`) | Encoded by case: disableEveryone, enableContacts, enableEveryone | `ignoreAlways` / `ignoreNever` flags | The same rule (`privacySection.tsx:313-318`) |
| Premium / Bots exceptions | AllowPremium for Invites by default (`:1264`). Gifts: DisallowBots when Everybody, otherwise AllowBots (`:1121-1126`) | AllowPremium and AllowBots for Contacts and Nobody | AllowPremium only on the Invites "Always" list (D `settings/settings_privacy_controllers.cpp:831-834`). AllowBots / DisallowBots for gifts (`:1665`) | AllowBots / DisallowBots only for gifts (`allowMiniApps`, W `sidebarLeft/tabs/privacy/gifts.tsx:64`) |
| Close friends | Not offered | Only in disableEveryone (stories) | `hasOption` excludes CloseFriends from these screens (D `boxes/edit_privacy_box.cpp:606-607`) | Not offered |
| Phone number | Choosing Nobody also saves AddedByPhone as AllowAll or AllowContacts (`PrivacyControlActivity.java:1026-1040`) | AddedByPhone counts as "on" only when an `AllowAll` rule is present (I `UpdatedAccountPrivacySettings.swift:170-182`) | The AddedByPhone block appears only when phone = Nobody and offers only Everyone and Contacts (D `settings_privacy_controllers.cpp:595-620`) | AddedByPhone omits Nobody (W `phoneNumber.tsx:50-53`) |
| Errors on save | Any error shows a generic alert (`:1137`) | | `PREMIUM_ACCOUNT_REQUIRED` opens the Premium screen (D `api_user_privacy.cpp:282-286`) | |

**Consequence: the server must evaluate rules independently of their order.**
- Web K sends `[AllowAll, DisallowUsers]`. A first-match evaluator would never apply the exclusion.
- iOS sends `[..., DisallowAll, AllowPremium]` and expects AllowPremium to still apply.

**Precedence inferred from the clients:**
1. A per-user exception (Allow/DisallowUsers) is checked first.
2. Then chat-participant exceptions.
3. Then the Premium / Bots / CloseFriends flags.
4. Then the base rule.

This precedence is an inference. The fetched docs say nothing about evaluation order or the default (core.telegram.org/api/privacy). The **exact order between users and chats, and between conflicting chat lists, is UNVERIFIED.**

Supporting evidence:
- tdesktop's parser treats the first base rule as the base. Its comment calls the parser "simplified".
- A user who appears in both the Always and Never lists stays in whichever list came first (D `api_user_privacy.cpp:108-196`).
- tdesktop falls back to DisallowAll when no base rule is present (`:194`).

### 1.2 Premium-locked options

Telegram's UI locks some options for non-Premium accounts:
- **Voice messages:** Contacts and Nobody are locked (A `PrivacyControlActivity.java:653`; I `SettingsUI/.../SelectivePrivacySettingsController.swift:1086-1088, 1105`; W `voices.tsx:18` `premiumOnly`).
- **"New chats from non-contacts → Contacts and Premium":** locked unless the app-config key `new_noncontact_peers_require_premium_without_ownpremium` is set (A `PrivacyControlActivity.java:641`; D `api/api_global_privacy.cpp:232-249`, `main/main_app_config.cpp:560`).

**Open question for Pluma:** whether its users are Premium. If they are not, these settings cannot be set from the official clients. This is the owner's decision.

## 2. account.getPrivacy and updatePrivacy

| | Android | iOS | tdesktop | Web K |
|---|---|---|---|---|
| Load | Calls `getPrivacy` for each key (`ContactsController.java:2653-2722`) | **One `combineLatest` over all 14 keys plus `getAccountTTL`, `getGlobalPrivacySettings` and `getDefaultHistoryTTL`. On any error it completes empty, so the whole Privacy screen never loads** (I `UpdatedAccountPrivacySettings.swift:58-79`) | Per key, on demand (D `api_user_privacy.cpp:309-326`) | Per key, then cached (W `lib/appManagers/appPrivacyManager.ts:59-78`) |
| Exception peers | Taken from `privacyRules.users/chats` via `putUsers` / `putChats` (`PrivacyControlActivity.java:1133-1135`) | **Resolved only from `users` and `chats` of the response. A chat ID is tried as both a basic group and a channel. Missing peers are silently dropped** (I `Settings/PrivacySettings.swift:237-272`, `UpdatedAccountPrivacySettings.swift:229-251`) | **A chat not loaded locally is dropped** (`api_user_privacy.cpp:147-160`) | Stores users and chats (`appPrivacyManager.ts:71-72`) |
| Empty rules | Treated as **Nobody** (`PrivacyControlActivity.java:1277-1278`) | Treated as disableEveryone (`PrivacySettings.swift:223`) | Treated as Nobody (`:194`) | `type: undefined`, falls back to the default radio |
| Base-rule parsing | `[AllowContacts, DisallowAll]` is read as Contacts (`hadAllowContacts`, `:1311-1315`) | The last base rule wins | The first base rule wins | `types[0]`, the first base rule (W `lib/appManagers/utils/privacy/getPrivacyRulesDetails.ts:45`) |
| updatePrivacy | Replaces that key's rules (`MessagesController.java:19783-19805`) | **Not handled; no `.updatePrivacy` case in TelegramCore** (grep) | If any chat in the rules is not loaded, it reloads that key with `getPrivacy` (`api/api_updates.cpp:2316-2345`) | Replaces the cache (`appPrivacyManager.ts:13-18`) |
| After a LastSeen change | `reloadContactsStatuses()` → `contacts.getStatuses` (`ContactsController.java:2893`) | | A non-Premium client degrades all exact statuses to Recently with `by_me` locally, then calls `contacts.getStatuses` (D `apiwrap.cpp:2127-2170`) | |

## 3. Restricted fields and userFull flags

| Behaviour | Android | iOS | tdesktop | Web K |
|---|---|---|---|---|
| `userStatusRecently/LastWeek/LastMonth` with `by_me` | Stored as `expires` −1000 / −1001 / −1002 (`ContactsController.java:2615-2619`) | `isHidden` flag (I `ApiUtils/TelegramUserPresence.swift:17-28`). The profile uses it (`PeerInfoData.swift:1224-1227`) | `hiddenByMe`. The profile shows a "show my last seen" button that offers to save LastSeen as Everyone or buy Premium, then refreshes the full user (D `info/profile/info_profile_top_bar.cpp:3064-3118`) | The profile shows "when?" for `by_me` (W `components/peerProfile.tsx:339`) |
| `userStatusEmpty` | | `.none` | "Long ago" (D `data/data_user.cpp:117`) | |
| Missing `user.phone` | Phone row shows "Phone hidden" when the profile has no other info. "Share contact" is hidden (`ui/ProfileActivity.java:10660-10676, 13457-13461, 12115`) | | Phone row is omitted | Phone row is omitted |
| Photo | Absent `photo`. `userFull.fallback_photo` is shown when the main photo is hidden (doc: /constructor/userFull) | | Uses `vfallback_photo` (D `data/data_user.cpp:861-868`) | |
| `about`, `birthday` | Absent field = hidden (generic code; no special UI) | | `setAbout(value_or_empty)` (`data_user.cpp:930`) | |
| `phone_calls_available` | **Call button is shown only if set** (`ui/ChatActivity.java:23808`) | Bit 4 only (I `Peers/UpdateCachedPeerData.swift:306`). Bit 5 is read (`:373`) | Button shown when the status is not Disabled, so Private also shows it (`data_user.cpp:822-824`) | Button gated on it (`components/chat/topbar.ts:410`) |
| `phone_calls_private` | Not used. A call that gets **any 403** shows "CallNotAvailable" (`messenger/voip/VoIPService.java:1155-1156`; `ui/VoIPFragment.java:1980`) | `CallSessionManager.swift:1711` maps `USER_PRIVACY_RESTRICTED` | Pre-blocks with "not available" and refreshes the full user (`calls/calls_instance.cpp:205-212`). `USER_PRIVACY_RESTRICTED` gives the same box (`calls/calls_call.cpp:1610`) | Pre-blocks with `Call.PrivacyErrorMessage` (W `lib/appImManager.ts:2203`) |
| `voice_messages_forbidden` | Disables voice and round videos, with tooltip `VoiceMessagesRestrictedByPrivacy` (`ChatActivity.java:13099`; `SendMessagesHelper.java:2113, 2762`) | `voiceMessagesAvailable` = bit 20 not set (`UpdateCachedPeerData.swift:309`) | Voice and video messages restricted (`data/data_peer.cpp:1767-1771`) | Toast before recording (`components/chat/recording/chatRecording.ts:978`) |
| `read_dates_private` | Hides the "read at" row (`ChatActivity.java:30860`) | `.readDatesPrivate` (`UpdateCachedPeerData.swift:310, 327`) | `WhoReadExists` false (`api/api_who_reacted.cpp:748`) | Hides it (`lib/appManagers/appMessagesManager.ts:11539`) |
| Read-date window | `pm_read_date_expire_period` app config, default 7 days (A `MessagesController.java:1631`) | | Same key, default 7 days (D `api_who_reacted.cpp:750-752`) | Same key (W `appMessagesManager.ts:11539`) |
| `contact_require_premium` (user and userFull) | Composer lock (`ChatActivity.java:27537, 32453`) | `premiumRequired` (`UpdateCachedPeerData.swift:311, 319`) | `RequiresPremiumToWrite`. The `user` flag triggers `users.getRequirementsToContact` (`data_user.cpp:896-918`; `api/api_premium.cpp:446-485`) | `appUsersManager.ts:1367-1373` |

## 4. Errors

| Error | Android | iOS | tdesktop | Web K |
|---|---|---|---|---|
| `USER_PRIVACY_RESTRICTED` on addChatUser / inviteToChannel / editAdmin | `InviteToGroupError` / `InviteToChannelError` alert (`ui/Components/AlertsCreator.java:6855`). editAdmin opens the "restricted users" sheet (`MessagesController.java:8849`) | `.privacy` error (I `Peers/AddPeerMember.swift:60, 174, 290`) | `ChatInviteForbidden` box (`boxes/add_contact_box.cpp:243`) | Toast `InviteToGroupError` (W `components/addChatUsers.ts:206`) |
| `messages.invitedUsers.missing_invitees` (layer 228: createChat, addChatUser and inviteToChannel all return `messages.InvitedUsers`, D `api.tl:2479, 2481, 2781`) | `checkRestrictedInviteUsers`. **Looks the user up first in `invitedUsers.updates.users`**, then in its cache (`AlertsCreator.java:3649-3679`) | Peer resolved from the local store only (else dropped), then calls `getRequirementsToContact` (`AddPeerMember.swift:104-118`). For a channel, the first missing invitee becomes `.restricted` (`:194-203`) | `CollectForbiddenUsers` uses `userLoaded` (dropped if unknown) and shows a box: **"Send invite link"** if it can invite, else Close (`boxes/peers/add_participants_box.cpp:1099-1177`) | `handleMissingInvitees`: gets the chat's invite link and **sends it as a text message** to the users picked (`helpers/dom/createParticipantContextMenu.ts:15-80`) |
| createChat result | | | Needs `updates` or `updatesCombined` with `chats.front()` of type `chat` (`add_contact_box.cpp:72-125`) | Reads `updates.chats[0].id` (W `appChatsManager.ts:627-636`) | 
| `PRIVACY_PREMIUM_REQUIRED` | On add-user it builds a synthetic missing invitee (`MessagesController.java:15645-15659`). On sendMessage, a Premium alert (`AlertsCreator.java:310`) | `.nonPremiumMessagesForbidden` (`State/PendingMessageManager.swift:104`); secret chat (`CreateSecretChat.swift:38`) | | |
| `VOICE_MESSAGES_FORBIDDEN` | Pre-checked via `userFull` (`SendMessagesHelper.java:10224`) | `.voiceMessagesForbidden` (`PendingMessageManager.swift:102`) | Toast in the share box (`boxes/share_box.cpp:1976`) | Toast on forward (`components/chat/input.ts:4437`) |
| `getOutboxReadDate` errors | `USER_PRIVACY_RESTRICTED` shows "PmReadUnknown". `YOUR_PRIVACY_RESTRICTED` shows "Read" plus "show when" (Premium upsell). Other errors give a generic bulletin (`ui/Components/MessagePrivateSeenView.java:154-170`) | Any error gives an empty result (`TelegramEngine/Messages/MessageReadStats.swift:30-37`) | YOUR → MyHidden, USER → HisHidden, `MESSAGE_TOO_OLD` → TooOld, else Empty (`api_who_reacted.cpp:304-316`) | Only YOUR gives "show when". Any other error removes the row (W `components/chat/contextMenu.ts:1659`) |

What the docs say (/method/messages.getOutboxReadDate):
- 400 `MESSAGE_ID_INVALID`, `MESSAGE_NOT_READ_YET`, `MESSAGE_TOO_OLD`, `PEER_ID_INVALID`.
- 403 `USER_PRIVACY_RESTRICTED`.
- 403 `YOUR_PRIVACY_RESTRICTED`: "allow other users to see your exact last online date OR purchase a Telegram Premium subscription".

## 5. Forwards with a hidden sender

| Client | Rendering of `fwd_from` with only `from_name` and `date` (no `from_id`) |
|---|---|
| Android | `currentForwardName = fwd_from.from_name` (`ui/Cells/ChatMessageCell.java:18998-18999`). A tap shows the hint `HidAccount` (`ui/Components/HintView.java:238`) |
| iOS | Standard author-signature path (not traced further) |
| tdesktop | `originalSenderName` becomes `HiddenSenderInfo`: a colored empty userpic made from the name, and a tap shows the toast `lng_forwarded_hidden` (`history/history_item.cpp:253-262`; `history/history_item_components.cpp:205-247`). In Saved Messages such forwards go to the hidden-author sublist (`history_item.cpp:4718`) |
| Web K | Title = `from_name` (`components/chat/bubbles.ts:10477, 10648`) |

`userFull.private_forward_name` is stored by tdesktop (`data_user.cpp:944`), but no client uses it to render a forward.

## 6. globalPrivacySettings

| Toggle | Android | iOS | tdesktop | Web K |
|---|---|---|---|---|
| `hide_read_marks` | LastSeen screen. The request copies all other fields from cache, and **omits `disallowed_gifts` unless Premium** (`PrivacyControlActivity.java:1140-1158`) | Parsed from bit 3 (`UpdatedAccountPrivacySettings.swift:259`) | Shown and saved only when LastSeen ≠ Everyone or there are Never exceptions (D `settings_privacy_controllers.cpp:730-733, 771-780`) | **Sends only `pFlags`, with no `noncontact_peers_paid_stars` and no `disallowed_gifts`** (W `lastSeen.tsx:45-56`) |
| `archive_and_mute_new_noncontact_peers` | | | Shown only if it is already on, or the app-config key `autoarchive_setting_available` is set (D `api/api_global_privacy.cpp:72-101`) | |
| `keep_archived_unmuted` / `keep_archived_folders` | Copied | Parsed | Sent together (`api_global_privacy.cpp:236-245`) | |
| `new_noncontact_peers_require_premium` / `noncontact_peers_paid_stars` | "Messages" screen (`PrivacyControlActivity.java:924-994`) | `.requirePremium` / `.paidMessages` (`:263-269`) | Sent always. If it fails with `PREMIUM_ACCOUNT_REQUIRED`, the client retries with premium off (`:271-292`) | `messages/useSaveSettings.ts:34-37` |
| `display_gifts_button`, `disallowed_gifts` | Gifts screen | | Always sent (`:250-252`) | gifts.tsx |

Doc (/constructor/globalPrivacySettings): when `hide_read_marks` is set, the StatusTimestamp rules also govern `getOutboxReadDate`. Those who cannot see my last seen get 403 `USER_PRIVACY_RESTRICTED`. A user with this flag on has `userFull.read_dates_private` set for others.

---

## 7. Server requirements derived from the clients

**Rule storage and evaluation**
1. Accept all 14 `InputPrivacyKey` values in both `getPrivacy` and `setPrivacy`.
   - Never return an error for any key: iOS loses the whole Privacy screen if one call fails (I `UpdatedAccountPrivacySettings.swift:77-79`).
   - `getAccountTTL`, `getGlobalPrivacySettings` and `messages.getDefaultHistoryTTL` must also succeed.
2. Accept the rules in any order. Evaluate them independently of order: user-specific Allow/Disallow first, then chat-participant lists, then the Premium / Bots / CloseFriends flags, then the base rule.
   - Without a base rule, default to deny (D `api_user_privacy.cpp:194`; A `:1277`).
   - Tie-breaks between users and chats: UNVERIFIED.
3. Accept empty `users` and `chats` vectors (Android sends them) and ignore them.
   - Resolve `inputUser` and validate access.
   - A chat ID may be a basic group or a channel.
4. The result of `getPrivacy` and `setPrivacy` (`account.privacyRules`) must contain:
   - every user referenced in the rules, in `users`;
   - every chat or channel referenced in the rules, in `chats`.
   Otherwise iOS and tdesktop silently drop those exceptions.
   - Return rules normalised to: one base rule plus the non-empty exception rules.
5. After `setPrivacy`, push `updatePrivacy` to the account's other sessions (doc: "sent to all currently logged in sessions").
   - Send it inside `updates`, with the users and chats it references.
   - tdesktop reloads the key if a chat is missing.
   - iOS ignores this update.
6. `AllowChatParticipants` / `DisallowChatParticipants` must check current membership of the chat or channel at evaluation time. This is the bug the owner reported, where "allow participants of chat X" let everyone through.
7. CloseFriends is used only for stories. AllowPremium is Premium-dependent. AllowBots / DisallowBots apply to gifts only. The server can store and evaluate them correctly without any special case.

**Restricted fields (by viewer)**

8. StatusTimestamp:
   - A viewer not allowed sees an approximate status: Recently (≤3 days), LastWeek (≤7) or LastMonth (≤30), else Empty. These thresholds are the ones tdesktop uses locally (`apiwrap.cpp:2138-2144`); the server-side bucketing is UNVERIFIED but consistent with them.
   - If the viewer hides their own exact status from the target and is not Premium, the viewer gets the bucketed status **with `by_me`**, even if the target allows them. The flag means the exact status "is actually available to us, but to view it we must ... allow this user to see our exact last online status" (doc).
   - The same applies in `users`, `getFullUser`, `contacts.getStatuses`, `updateUserStatus`, and every embedded `user`.
9. When anyone's StatusTimestamp rules change, push `updateUserStatus` with the newly visible status to affected online viewers.
   - `contacts.getStatuses` must return the recomputed status (A and D call it right after a change).
   - Push or recompute for non-contacts too: the owner wants status for everyone, not only contacts.
   - "Show my last seen" in tdesktop saves LastSeen as AllowAll and then calls `getFullUser`. That call must return the exact status at once.
10. PhoneNumber:
    - Omit `user.phone` (flag absent) for viewers who are not allowed.
    - For contacts, `contacts.importContacts` must honour AddedByPhone, which offers only Everyone or Contacts.
11. ProfilePhoto:
    - Hidden photo means `user.photo` absent, or `userProfilePhotoEmpty`, and `userFull.profile_photo` absent.
    - `photos.getUserPhotos` must be empty.
    - Set `fallback_photo` if the target has a public fallback.
12. About and Birthday: omit `userFull.about` and `userFull.birthday` for viewers who are not allowed.
13. PhoneCall:
    - Set `phone_calls_available = true` together with `phone_calls_private = true` when the viewer is not allowed. Android only shows the call button when `available` is set, and it relies on the error.
    - `phone.requestCall` must fail with **403** `USER_PRIVACY_RESTRICTED` (Android maps any 403 to "not available").
    - The combination of flags is inferred from client code; UNVERIFIED against Telegram.
14. PhoneP2P: restrict the `p2p_allowed` / relay-only choice in call setup (not traced in this pass).
15. VoiceMessages:
    - Set `userFull.voice_messages_forbidden` for viewers who are not allowed.
    - Reject voice and round-video sends and forwards with `VOICE_MESSAGES_FORBIDDEN` (400 assumed, UNVERIFIED).
16. Forwards:
    - When the original sender disallows the forwarder's audience, `fwd_from` has no `from_id` and carries `from_name` (the sender's display name) and `date`.
    - Set `userFull.private_forward_name` if used.
    - Whose rules apply (the recipient of the forward vs the forwarder): UNVERIFIED, per the doc description of the key.
17. ChatInvite:
    - `messages.createChat`, `messages.addChatUser` and `channels.inviteToChannel` return `messages.invitedUsers` with `missing_invitees` (flags unset) for the restricted users. They are not added, and the call does not fail.
    - Include those users in `updates.users`: Android reads them there, and tdesktop and iOS drop unknown users.
    - `createChat` must still create the chat and return `updates` whose `chats[0]` is the new `chat` (D, W).
    - The inviter can then get `exported_invite` from `getFullChat` / `getFullChannel` (Web K sends the link).
    - Use 403 `USER_PRIVACY_RESTRICTED` as an error only for single-target actions such as editAdmin (A `:8849`).
18. `hide_read_marks`, read dates (doc /constructor/globalPrivacySettings):
    - Persist the flag.
    - Set `userFull.read_dates_private` for viewers when the target has `hide_read_marks` (Android, iOS and tdesktop then hide the row).
    - `getOutboxReadDate`: if the reader has `hide_read_marks` and the requester cannot see the reader's last seen per StatusTimestamp, return 403 `USER_PRIVACY_RESTRICTED`.
    - If the requester has hidden their own last seen from the reader (and is not Premium), return 403 `YOUR_PRIVACY_RESTRICTED`. The exact condition is UNVERIFIED; it follows the doc's "allow other users to see your exact last online date OR purchase Premium".
    - Also return `MESSAGE_TOO_OLD` beyond `pm_read_date_expire_period` (publish it in app config), and `MESSAGE_NOT_READ_YET`.
19. Saving globalPrivacySettings:
    - Return the full saved object.
    - When `disallowed_gifts` (flag 6) or `noncontact_peers_paid_stars` is absent, **keep the stored value**. Android (non-Premium) and Web K omit them when only toggling read time. This is inferred, and UNVERIFIED against Telegram.
    - If non-Premium users must be able to set `new_noncontact_peers_require_premium`, publish `new_noncontact_peers_require_premium_without_ownpremium: true` in app config. Otherwise reject it with `PREMIUM_ACCOUNT_REQUIRED` (doc).
    - If the require-Premium setting is enforced: return `PRIVACY_PREMIUM_REQUIRED` (403) on sends, set `contact_require_premium` on `user` and `userFull`, and make `users.getRequirementsToContact` answer correctly (all clients call it).
20. Publish the app-config key `autoarchive_setting_available` if the auto-archive toggle should be visible in tdesktop.

## 8. Open questions for the owner

- Are Pluma accounts Premium (`user.premium`)? This decides:
  - whether Voice-message privacy and "Contacts and Premium" can be set at all;
  - whether `by_me` and `YOUR_PRIVACY_RESTRICTED` apply. With Premium for all, they never apply.
- Should the auto-archive toggle be shown (`autoarchive_setting_available`)?

Items not traced in this pass:
- Rule-evaluation tie-breaks.
- The forward-privacy perspective (item 16).
- The P2P call setup (item 14).
- iOS hidden-forward rendering.

All are marked UNVERIFIED above.
