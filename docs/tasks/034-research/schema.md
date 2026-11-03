# Privacy settings catalog, layer 228

## Sources and method

- **Schema.** I checked the server's vendored proto (`third_party/teamgram-proto/mtproto`, module v0.228.0; it has all 14 keys, all 12 rule constructors and the `userFull`/`globalPrivacySettings` fields). The TL text I quote is from tdesktop's `Telegram/SourceFiles/mtproto/scheme/api.tl` (`// LAYER 228`, line 3087).
- **Docs.** I downloaded the raw markdown of core.telegram.org (`https://core.telegram.org/<path>.md`) on 2026-11-03. The copies are in `/private/tmp/claude-503/-Users-sckr-a-teamgram-server/76bbed60-70da-4b26-9bff-19b96e18ecba/scratchpad/docs/`. Pages read:
  - `api/privacy`, `api/paid-messages`, `api/calls`, `api/invites`, `api/contacts`, `api/action-bar`, `api/profile`, `api/gifts`, `api/stories`, `api/min`, `api/config`, `api/scheduled-messages`;
  - all `(input)privacyKey*` constructors, `type/InputPrivacyRule`, `type/PrivacyRule`, `type/InputPrivacyKey`;
  - `globalPrivacySettings`, `user`, `userFull`, `userStatus*`, `messageFwdHeader`, `peerSettings`, `missingInvitee`, `requirementToContact*`, `phoneCall`, `updatePrivacy`;
  - methods: `account.get/setPrivacy`, `account.setGlobalPrivacySettings`, `messages.getOutboxReadDate`, `users.getRequirementsToContact`, `phone.requestCall`, `messages.addChatUser`, `messages.createChat`, `channels.inviteToChannel`, `messages.sendMessage`, `messages.sendMedia`, `messages.forwardMessages`, `contacts.addContact`, `contacts.importContacts`, `contacts.resolvePhone`, `account.toggleNoPaidMessagesException`.
- **Clients**, at the pinned releases:
  - tdesktop: `api/api_user_privacy.cpp`, `settings/settings_privacy_controllers.cpp`, `apiwrap.cpp`, `data/data_user.cpp`, `api/api_who_reacted.cpp`.
  - Android: `ui/PrivacyControlActivity.java`, `res/values/strings.xml`.
  - iOS: `TelegramCore/.../Privacy/UpdatedAccountPrivacySettings.swift`, `TelegramCore/Sources/Settings/PrivacySettings.swift`.
  - tweb: `components/privacySection.tsx`, `lib/appManagers/utils/privacy/getPrivacyRulesDetails.ts`.
- Anything marked **UNVERIFIED** is from memory or inference. Code must not rely on it until it is probed against real Telegram.

## 1. Schema inventory (layer 228)

**Keys.** Each input/output pair maps one to one:

| InputPrivacyKey | PrivacyKey |
|---|---|
| inputPrivacyKeyStatusTimestamp#4f96cb18 | privacyKeyStatusTimestamp#bc2eab30 |
| inputPrivacyKeyChatInvite#bdfb0426 | privacyKeyChatInvite#500e6dfa |
| inputPrivacyKeyPhoneCall#fabadc5f | privacyKeyPhoneCall#3d662b7b |
| inputPrivacyKeyPhoneP2P#db9e70d2 | privacyKeyPhoneP2P#39491cc8 |
| inputPrivacyKeyForwards#a4dd4c08 | privacyKeyForwards#69ec56a3 |
| inputPrivacyKeyProfilePhoto#5719bacc | privacyKeyProfilePhoto#96151fed |
| inputPrivacyKeyPhoneNumber#352dafa | privacyKeyPhoneNumber#d19ae46d |
| inputPrivacyKeyAddedByPhone#d1219bdd | privacyKeyAddedByPhone#42ffd42b |
| inputPrivacyKeyVoiceMessages#aee69d68 | privacyKeyVoiceMessages#697f414 |
| inputPrivacyKeyAbout#3823cc40 | privacyKeyAbout#a486b761 |
| inputPrivacyKeyBirthday#d65a11cc | privacyKeyBirthday#2000a518 |
| inputPrivacyKeyStarGiftsAutoSave#e1732341 | privacyKeyStarGiftsAutoSave#2ca4fdf8 |
| inputPrivacyKeyNoPaidMessages#bdc597b4 | privacyKeyNoPaidMessages#17d348d2 |
| inputPrivacyKeySavedMusic#4dbe9226 | privacyKeySavedMusic#ff7a571b |

**Rules.** Twelve constructors, each with an input twin:
- AllowContacts, AllowAll, AllowUsers(users), DisallowContacts, DisallowAll, DisallowUsers(users);
- AllowChatParticipants(chats:Vector<long>), DisallowChatParticipants(chats);
- AllowCloseFriends, AllowPremium, AllowBots, DisallowBots.

**Methods and updates:**
- `account.getPrivacy#dadbc950`, `account.setPrivacy#c9f81ce8` → `account.privacyRules#50a04e45 rules chats users`;
- `updatePrivacy#ee3b272a key rules`;
- `account.getGlobalPrivacySettings#eb2b4cf6`, `account.setGlobalPrivacySettings#1edaaac2`;
- `users.getRequirementsToContact#d89a83a3` → `requirementToContactEmpty/Premium/PaidMessages(stars_amount)`;
- `messages.getOutboxReadDate#8c4bfe5d` → `outboxReadDate#3bb842ac`;
- `account.toggleNoPaidMessagesException#fe2eda76`;
- `contacts.addContact` flag `add_phone_privacy_exception`;
- `contacts.editCloseFriends`.

**globalPrivacySettings#fe41b34f flags:**
- archive_and_mute_new_noncontact_peers (0)
- keep_archived_unmuted (1)
- keep_archived_folders (2)
- hide_read_marks (3)
- new_noncontact_peers_require_premium (4)
- noncontact_peers_paid_stars:long (5)
- disallowed_gifts (6)
- display_gifts_button (7)

**Privacy-derived fields:**
- `user`: `phone`, `photo`, `status`, `contact_require_premium` (flags2.10), `send_paid_messages_stars` (flags2.15), `close_friend`, `min`/`apply_min_photo`.
- `userStatusRecently/LastWeek/LastMonth` carry `by_me`.
- `userFull`:
  - `phone_calls_available` (4), `phone_calls_private` (5), `video_calls_available` (13);
  - `voice_messages_forbidden` (20), `contact_require_premium` (29), `read_dates_private` (30);
  - `about` (1), `profile_photo` (2), `personal_photo` (21), `fallback_photo` (22);
  - `private_forward_name` (16);
  - flags2: `birthday` (5), `stargifts_count` (8), `send_paid_messages_stars` (14), `disallowed_gifts` (15), `display_gifts_button` (16), `saved_music` (21).
- `messageFwdHeader`: `from_id` vs `from_name`.
- `phoneCall.p2p_allowed`.
- `peerSettings`: `need_contacts_exception`, `share_contact`, `charge_paid_message_stars`.
- `missingInvitee`: `premium_would_allow_invite`, `premium_required_for_pm`.

**Errors named by the docs:**

| Error | Where |
|---|---|
| 400 PRIVACY_KEY_INVALID | get/setPrivacy |
| 400 PRIVACY_TOO_LONG ("limit is 1000") | setPrivacy |
| 400 PRIVACY_VALUE_INVALID ("rule combination is invalid") | setPrivacy |
| 403 USER_PRIVACY_RESTRICTED | requestCall, addChatUser, inviteToChannel, getOutboxReadDate |
| 403 YOUR_PRIVACY_RESTRICTED | getOutboxReadDate |
| PRIVACY_PREMIUM_REQUIRED | 406 in sendMessage/forwardMessages, 403 in sendMedia and in api/privacy |
| VOICE_MESSAGES_FORBIDDEN | 400 in sendMedia, 403 in forwardMessages |
| 403 PREMIUM_ACCOUNT_REQUIRED | setGlobalPrivacySettings |
| 400 SCHEDULE_STATUS_PRIVATE | sendMessage |
| 403 ALLOW_PAYMENT_REQUIRED_%d / 406 ALLOW_PAYMENT_REQUIRED | send methods |
| 400 PHONE_NOT_OCCUPIED | resolvePhone, also used for privacy |
| 403 USER_NOT_MUTUAL_CONTACT, 400 BUTTON_USER_PRIVACY_RESTRICTED | invites; keyboard button |
| 400 UNSUPPORTED | toggleNoPaidMessagesException |

General rules from the docs:
- `account.setPrivacy` and `account.getPrivacy` are "Only users can use this method" (bots cannot).
- `updatePrivacy` goes "to all currently logged in sessions of the current account" (api/privacy).
- Doc slip: the privacyKey block on api/privacy leaves out `privacyKeyBirthday`. The constructor page and api.tl have it.

## 2. Per-key tables

### privacyKeyStatusTimestamp (last seen and online)

| Aspect | Content |
|---|---|
| Protects | The exact last-online time ("Whether people will be able to see our exact last online timestamp", constructor page). |
| Enforce in | `user.status` in every `user` the server emits to another user (getUsers, getFullUser.users, dialogs, messages, contacts, updates and so on), `updateUserStatus` pushes, `contacts.getStatuses`. Also `messages.getOutboxReadDate` when `hide_read_marks` is set, and send-when-online scheduling (`schedule_date=0x7FFFFFFE`, api/scheduled-messages). |
| Restricted value | Approximate status: `userStatusRecently` / `userStatusLastWeek` / `userStatusLastMonth`. tdesktop maps `userStatusEmpty` to "long ago" (`data/data_user.cpp:117`). |
| Reciprocity (documented) | "if *we* decide to hide our exact last online timestamp to someone ... **and** we do not have a Premium subscription, we won't be able to see the exact last online timestamp of those users ... even if those users *do* share it with us". In that case `by_me` is set on the approximate status (privacyKeyStatusTimestamp page; userStatus* `by_me`). |
| Rule | Viewer V sees owner O exactly iff O's rules allow V AND (V is Premium OR V's own StatusTimestamp rules allow O). If O allows V but the second part fails: approximate with `by_me=true`. If O denies V: approximate with `by_me=false`. |
| Client UX (Android) | `CustomHelp` (strings.xml:3680) says approximate statuses are "recently, within a week, within a month". `PrivacyLastSeenPremiumInfo` (:7648) says Premium sees others "even if you hid yours from them (unless they specifically restricted it)". |
| Thresholds | Not documented. tdesktop's local downgrade, when it reapplies privacy, uses ≤3 days → recently, ≤7 → week, ≤30 → month, else long ago (`apiwrap.cpp:2138-2144`). That is client data, not a server spec. |
| Errors | `SCHEDULE_STATUS_PRIVATE` when send-when-online targets a user whose last seen is hidden (sendMessage, api/scheduled-messages). |
| Client side | Changing this key makes tdesktop re-fetch `contacts.getStatuses` (`api_user_privacy.cpp:303`, `apiwrap.cpp:2155`). The server must therefore recompute statuses for the changer at once. |

### hide_read_marks (global setting, tied to StatusTimestamp)

| Aspect | Content |
|---|---|
| Protects | The exact read date of private messages. It does not affect read receipts themselves. Android `HideReadTimeInfo` (strings.xml:7628): "This setting does not affect group chats." |
| Semantics (globalPrivacySettings page) | If set, the StatusTimestamp key "will also apply to the ability to use messages.getOutboxReadDate on messages sent to us". Users that cannot see our exact last seen get `403 USER_PRIVACY_RESTRICTED`, and `userFull.read_dates_private` is set. |
| Reciprocity | getOutboxReadDate gives `403 YOUR_PRIVACY_RESTRICTED`: "you have disallowed other users to do so for *your* messages; to fix, allow other users to see *your* exact last online date OR purchase Premium". Android `HideReadTimeInfo`: "If you turn this on, their read time will also be hidden from you." |
| Other getOutboxReadDate errors | `MESSAGE_ID_INVALID`, `MESSAGE_NOT_READ_YET`, `MESSAGE_TOO_OLD` (older than `pm_read_date_expire_period`, default 604800 in api/config), `PEER_ID_INVALID`. |
| Client | tdesktop maps the errors to MyHidden / HisHidden / TooOld (`api/api_who_reacted.cpp:309-314`). |

### privacyKeyChatInvite

| Aspect | Content |
|---|---|
| Protects | "Whether people will be able to invite you to chats". |
| Enforce in | `messages.createChat`, `messages.addChatUser`, `channels.inviteToChannel` (evaluated against the inviter). Probably also `phone.inviteConferenceCallParticipant`, which tdesktop expects to answer USER_PRIVACY_RESTRICTED (`calls/group/calls_group_call.cpp:4072`); which key governs it there is UNVERIFIED. |
| Restricted behaviour | The batch methods return `messages.invitedUsers` with `missingInvitee` (api/invites): no flags means a privacy denial, and the inviter may send a link. `premium_would_allow_invite` means Premium on the inviter would allow it. `premium_required_for_pm` means denied, and Premium is also needed to DM a link. Single-user paths give `403 USER_PRIVACY_RESTRICTED`; tdesktop shows ChatInviteForbidden (`boxes/add_contact_box.cpp:243`). |
| Special rule | `AllowPremium` is "currently only usable for inputPrivacyKeyChatInvite" (privacyValueAllowPremium page). tdesktop offers the Premium toggle only here (`settings_privacy_controllers.cpp:831`). Android pre-checks it for the Contacts option (`PrivacyControlActivity.java:1264`). |

### privacyKeyPhoneCall

| Aspect | Content |
|---|---|
| Protects | "Whether you will accept phone calls". |
| Enforce in | `phone.requestCall` → `403 USER_PRIVACY_RESTRICTED` (method page); api/calls says "The Server S performs privacy checks". Also `userFull.phone_calls_private` ("Whether this user's privacy settings allow you to call them"), computed per viewer. |
| Client | tdesktop shows "not available" on USER_PRIVACY_RESTRICTED (`calls/calls_call.cpp:1610`) and sets the calls status from `phone_calls_private` (`data/data_user.cpp:923`). |

### privacyKeyPhoneP2P

| Aspect | Content |
|---|---|
| Protects | Direct (P2P) connection in 1:1 calls. Android `PrivacyCallsP2PHelp` (strings.xml:3702): disabling relays all calls "to avoid revealing your IP address". |
| Enforce in | Building `phoneCall`: api/calls says the server uses this key "to choose whether to also return P2P STUN phoneConnectionWebrtc connection options, along with the usual reflector TURN" options. Also `phoneCall.p2p_allowed` ("Whether P2P connection to the other peer is allowed"); tdesktop feeds it to `enableP2P` (`calls/calls_call.cpp:1090`). |
| Combination | Must both sides' rules allow each other? Not documented. UNVERIFIED. See open points. |

### privacyKeyForwards

| Aspect | Content |
|---|---|
| Protects | "Whether messages forwarded from you will be anonymous". Android `PrivacyForwardsInfo`: "Messages you send will not link back to your account when forwarded by other users." |
| Enforce in | `messages.forwardMessages` when building `fwd_from`: if denied, leave out `from_id` and set `from_name` (messageFwdHeader: `from_id` = "ID of the user that originally sent", `from_name` = "name of the user"). Also `userFull.private_forward_name` ("Anonymized text to be shown instead of the user's name on forwarded messages"). |
| min side effect | api/min: a `user_id` from a fwd_header can be used to interact with the original sender "if they don't have privacy settings for forwards enabled". So a hidden sender's id must not leak into `fwd_from` or the users vector. |
| Client | tdesktop reads `from_name` when `from_id` is absent (`history/history_item.cpp:262`). `private_forward_name` is stored but not displayed in tdesktop (`data/data_user.cpp:944`). |

### privacyKeyProfilePhoto

| Aspect | Content |
|---|---|
| Protects | "Whether people will be able to see your profile picture". |
| Enforce in | `user.photo` everywhere, `userFull.profile_photo`, `photos.getUserPhotos`, photo-change pushes (`updateUser`). |
| Restricted value | `userFull.fallback_photo`: "displayed if no photo is present in profile_photo or personal_photo, due to privacy settings". Android calls it a "public photo for those who are restricted" (`PhotoForRestDescription`, strings.xml:6112). `personal_photo` is the viewer's own choice and is not subject to the owner's privacy. |
| min rule | Clients apply `photo` from a min user only if `apply_min_photo` is set or the cached user is min (user page). The server must not set `apply_min_photo` with a photo the viewer may not see. |

### privacyKeyPhoneNumber and privacyKeyAddedByPhone

| Aspect | PhoneNumber | AddedByPhone |
|---|---|---|
| Protects | "Whether people will be able to see your phone number" | "Whether people can add you to their contact list by your phone number" |
| Enforce in | `user.phone` in every user constructor for other viewers (contacts, getUsers, updates, imported results); `updateUserPhone` | `contacts.importContacts` (users may be left out; api/contacts:34); `contacts.resolvePhone` → `PHONE_NOT_OCCUPIED`, the same error as "no such user" (api/contacts:51); `contacts.getContactIDs` gives 0 for "hidden their account using privacy settings" (api/contacts:147) |
| UI coupling | Android sends AddedByPhone only when PhoneNumber = Nobody, with values AllowAll or AllowContacts (`PrivacyControlActivity.java:1025-1040`); iOS does the same (`_internal_updatePhoneNumberDiscovery`). | Values seen: AllowAll / AllowContacts; Android also parses DisallowAll (`:1352-1366`). |
| Extra visibility | Android `PrivacyPhoneInfo` (strings.xml:3618): "Users who have your number saved in their contacts will also see it on Telegram." With AddedByPhone = Contacts, `PrivacyPhoneInfo3` (:3619): "...only if they are your contacts". So viewers who imported the number see it subject to AddedByPhone. This is client text; the exact server rule is UNVERIFIED. | |
| Exceptions | `contacts.addContact add_phone_privacy_exception` = "Allow the other user to see our phone number?". It must be set when `peerSettings.need_contacts_exception` is set (api/contacts, api/action-bar). `contacts.acceptContact` shares the number. How the exception is stored is UNVERIFIED. | |
| min rule | `phone` from a min user is applied only if the cached entry is min too (user page). | |

### privacyKeyVoiceMessages

| Aspect | Content |
|---|---|
| Protects | "Whether people can send you voice messages or round videos (Premium users only)". |
| Enforce in | `messages.sendMedia` (voice, round video) → `400 VOICE_MESSAGES_FORBIDDEN`. `messages.forwardMessages` → `403 VOICE_MESSAGES_FORBIDDEN`. Probably also `sendMultiMedia` and album paths (UNVERIFIED). `userFull.voice_messages_forbidden` per viewer. |
| Premium | Only Premium users may restrict it. Android blocks Contacts/Nobody for non-Premium on the client (`PrivacyControlActivity.java:653`). The server error for a non-Premium restrict is UNVERIFIED; tdesktop opens the Premium screen on `PREMIUM_ACCOUNT_REQUIRED` from setPrivacy (`api_user_privacy.cpp:283-286`). |
| Client | tdesktop's share box toasts on VOICE_MESSAGES_FORBIDDEN (`boxes/share_box.cpp:1976`). |

### privacyKeyAbout

Protects the bio ("Whether people can see your bio"). Enforce in `userFull.about` for other viewers.

### privacyKeyBirthday

| Aspect | Content |
|---|---|
| Protects | "Whether the user can see our birthday." Default is "only contacts by default" (api/profile:159). This is the only documented default. |
| Enforce in | `userFull.birthday`, `contacts.getBirthdays`. api/profile:161 says `user.birthday`, but `user` has no such field in layer 228, so it is a doc slip for `userFull.birthday`. Also `users.suggestBirthday`: allowed when the birthday is unset OR hidden from the caller by this key (api/profile:182). |

### privacyKeyStarGiftsAutoSave

Protects whether received gifts are automatically displayed on the profile (api/gifts:88). It is evaluated against the gift sender; `AllowBots`/`DisallowBots` mean "bots and mini apps". tdesktop offers the mini-apps toggle only for this key (`settings_privacy_controllers.cpp:1665`). Android pre-checks mini apps for Nobody (`PrivacyControlActivity.java:1267`). The result is the saved/unsaved state of `messageActionStarGift.saved` / `savedStarGift.unsaved` and the profile count. The related `disallowed_gifts` and `display_gifts_button` live in global settings.

### privacyKeyNoPaidMessages

| Aspect | Content |
|---|---|
| Protects | "Who can send you messages without paying, if paid messages are enabled." Only meaningful when `noncontact_peers_paid_stars` > 0. |
| Exempt (api/paid-messages:44-50) | Users in our contact list; users we messaged first; users in this key's `AllowUsers`; members of `AllowChatParticipants` chats "that contact us for the first time through one of our messages in a chat" (via min locations); users added by `account.toggleNoPaidMessagesException`, which can only add to `AllowUsers` (removing gives `UNSUPPORTED`). |
| Fields and errors | `user.send_paid_messages_stars`, `userFull.send_paid_messages_stars` (0 = exempt), `peerSettings.charge_paid_message_stars`, `users.getRequirementsToContact`, `403 ALLOW_PAYMENT_REQUIRED_%d`. |
| Client | Android writes `[AllowContacts, AllowUsers?, AllowChatParticipants?]` with no trailing DisallowAll (`PrivacyControlActivity.java:930-945`). It relies on default-deny. |

### privacyKeySavedMusic

Protects the songs pinned to the profile ("Whether the user can see the songs we pinned"). Enforce in `userFull.saved_music`, `users.getSavedMusic` and `users.getSavedMusicByID`.

### Global "who can message me" (not a key, but part of the same settings screen)

- `new_noncontact_peers_require_premium`: only Premium users, contacts, or users with an existing private chat may write. Anyone else gets `PRIVACY_PREMIUM_REQUIRED`. `user.contact_require_premium` is the raw flag; `userFull.contact_require_premium` is the fully checked value (api/privacy).
- Enabling it without Premium gives `PREMIUM_ACCOUNT_REQUIRED`, unless the config `new_noncontact_peers_require_premium_without_ownpremium` is true.
- Android also warns to review the Invites and Calls keys when this is enabled while they are Everybody (`PrivacyControlActivity.java:900-917`).

## 3. Rule evaluation

**Documented:**
- Rules say *who*; keys say *what* (api/privacy).
- `AllowCloseFriends` is only usable for stories; the list is managed with `contacts.editCloseFriends`, and only contacts can be added.
- `AllowPremium` is only for ChatInvite.
- Bots rules mean "bots and mini apps".
- `ChatParticipants.chats` holds basic-group and supergroup IDs, both "verbatim the way it is received in the constructor".
- Limit: `PRIVACY_TOO_LONG` above 1000. Invalid combinations: `PRIVACY_VALUE_INVALID`.
- Nothing documents evaluation order or precedence.

**What the clients send.** The four official clients put the rules in three different orders:

| Client | Order sent to setPrivacy |
|---|---|
| tdesktop (`api_user_privacy.cpp:56-99`) | AllowUsers, AllowChatParticipants, AllowPremium, AllowBots, DisallowUsers, DisallowChatParticipants, DisallowBots, then the base option last |
| Android (`PrivacyControlActivity.java:1072-1126`) | AllowUsers, AllowChats, DisallowUsers, DisallowChats, base option, then AllowPremium, AllowBots or DisallowBots after it |
| iOS (`UpdatedAccountPrivacySettings.swift:546-601`) | Nobody: AllowUsers, AllowChats, DisallowAll, AllowCloseFriends, AllowPremium, AllowBots. Contacts: allows, disallows, AllowContacts, Premium, Bots. Everybody: disallows, AllowAll |
| tweb (`components/privacySection.tsx:288-338`) | Base option first, then allow chats, users and bots, then disallow chats, users and bots |

So:
- **Precedence cannot depend on list position.** A plain first-match pass breaks:
  - tweb's "Everybody except X", which is `[AllowAll, DisallowUsers X]`;
  - Android's "Everybody, no mini apps";
  - iOS's "Nobody + Premium".
- **Exceptions must beat the base option** wherever they appear. The client wording agrees: Android `CustomShareSettingsHelp`: "exceptions that will override the settings above". `CustomShareInfo` says "regardless of the settings above" (strings.xml:3684, 3695).

**Suggested model** (consistent with all four clients; the tie-breaks inside one level are UNVERIFIED):
1. The owner always sees their own data.
2. Explicit user lists: `DisallowUsers` / `AllowUsers`.
3. Chat-participant lists: `DisallowChatParticipants` / `AllowChatParticipants`. The viewer must currently be a member of one of the listed chats.
4. Category rules: `AllowPremium` (ChatInvite), `AllowBots` / `DisallowBots` (viewer is a bot or mini app), `AllowCloseFriends` (stories).
5. Base option: `AllowAll` / `AllowContacts` (viewer is in the owner's contact list) / `DisallowAll`. `DisallowContacts` is never produced by clients (tdesktop "Not supported", `api_user_privacy.cpp:161`), but it must still be evaluated if received.
6. No match: deny.

Within one level, prefer the more specific rule (user over chat). The clients keep their "always" and "never" lists disjoint (tdesktop dedup at `api_user_privacy.cpp:142,170`).

**Reading rules back:**
- An empty list means **Nobody** in all clients: tdesktop `feed(DisallowAll) // Disallow by default` (`api_user_privacy.cpp:194`); iOS default `.disableEveryone` (`PrivacySettings.swift:223`); Android `TYPE_NOBODY` on empty (`PrivacyControlActivity.java:1277`).
- So `account.getPrivacy` for a key the user never set must return the key's **default rules explicitly**, never an empty vector, unless the default is Nobody.
- Android treats `[AllowContacts, DisallowAll]` as Contacts (`hadAllowContacts`, `:1314`). tweb takes the first base option (`getPrivacyRulesDetails.ts`). A trailing DisallowAll must therefore not cancel AllowContacts. Returning exactly one base option, last, is safest.

**Defaults for a new account.** Undocumented except Birthday = Contacts. UNVERIFIED recollection: LastSeen, ProfilePhoto, Forwards, Calls, P2P, Invites, AddedByPhone, Voice, About, GiftsAutoSave and SavedMusic are Everybody; PhoneNumber is Contacts or Nobody. Probe this on a fresh Telegram account.

**Where else rules apply:** story `privacy_rules` (`stories.sendStory`, `editStory`, `startLive`), with `storyItem` flags `public`, `contacts`, `close_friends`, `selected_contacts` (api/stories).

## 4. Side finding in the current server (relevant to the deferred bug)

- **Profile reads.** `third_party/teamgram-proto/mtproto/privacy_rule_helper.go:348-416` (`privacyIsAllow`, used by `ImmutableUser` key checks at `immutable_user_helper.go:286`) returns `true` unconditionally on `AllowChatParticipants`, and on `DisallowChatParticipants` under `AllowAll`. This is exactly the "chat X lets everyone in / all-except broken" bug.
- **Base option.** `CheckPrivacyIsAllow` (`:256-345`) picks the *last* base option, so `[AllowContacts, DisallowAll]` becomes Nobody.
- **Order dependence.** Under Contacts, whichever of allow or disallow comes first wins.
- **Ignored rules.** Both functions ignore AllowPremium, AllowBots, DisallowBots, AllowCloseFriends and DisallowContacts.
- **No stored rules means allow-all** (`immutable_user_helper.go:280`), so per-key defaults are not applied, Birthday included.
- **Panics.** `FromInputPrivacyKeyType`, `ToPrivacyKey` and `ToPrivacyRuleByInput` `panic` on unknown input instead of answering `PRIVACY_KEY_INVALID` / `PRIVACY_VALUE_INVALID`.

## 5. Open points the docs do not answer

1. **Precedence inside one level.** A user in `AllowUsers` and also in a `DisallowChatParticipants` chat, or the reverse. Specific beats general is assumed but UNVERIFIED. Probe it on real Telegram (Web K probe).
2. **ChatParticipants.** Is membership checked live at evaluation time? Must the owner still be a member? Does the rule hold for basic groups migrated to supergroups? UNVERIFIED.
3. **Meaning of "contacts".** The owner's contact list one way, or mutual? The docs say only "Allow only contacts". Undocumented.
4. **Approximate status thresholds** on the server, and what a hidden user who is online right now looks like (Recently?). Is `updateUserStatus` pushed to viewers who may only see approximate status? Only tdesktop's 3/7/30-day client heuristic is known.
5. **`read_dates_private`.** Is it set per viewer (only those who cannot see the owner's last seen) or whenever `hide_read_marks` is on? The two doc passages differ. Per viewer is the consistent reading.
6. **P2P.** Must both parties allow each other for `p2p_allowed` and STUN, or only the callee's rules against the caller?
7. **Forwards.** Is the rule evaluated against the forwarding user, the destination chat's members, or the reader? How does it interact with `saved_from_id` and forwards to Saved Messages?
8. **Phone exceptions.** How do `add_phone_privacy_exception` and `acceptContact` store the exception (an implicit AllowUsers on PhoneNumber, or a separate flag)? Exactly when does a viewer who has the owner's number in their address book see it?
9. **PRIVACY_VALUE_INVALID combinations** (AllowPremium on keys other than ChatInvite, AllowCloseFriends on account keys, both AllowAll and DisallowAll present). Does `PRIVACY_TOO_LONG` count rules or users? Undocumented.
10. **Error code mismatches.** `PRIVACY_PREMIUM_REQUIRED` is 403 or 406, and `VOICE_MESSAGES_FORBIDDEN` is 400 or 403, depending on the page. Clients match on the error type string, not the code (tdesktop `share_box.cpp:1976`).
11. **Voice messages on other paths:** `sendMultiMedia`, inline results, scheduled messages. Also the error a non-Premium user gets when restricting this key.
12. **Premium in Pluma (owner's decision).** Several semantics hinge on Premium:
    - the last-seen and read-date reciprocity bypass;
    - restricting voice messages;
    - `new_noncontact_peers_require_premium` and `AllowPremium` for invites;
    - `missingInvitee` premium flags.

    Pluma must choose whether nobody, everybody or a configured set of users counts as Premium.
13. **Defaults per key for new accounts.** Only Birthday = Contacts is documented.
14. **`updateUser` on changes.** When the owner changes a key, which viewers must get `updateUser` / `updateUserStatus` / `updateUserPhone` so their cached `user.photo`, `phone` and `status` are recomputed? The docs only promise `updatePrivacy` to the owner's own sessions.