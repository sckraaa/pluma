# Cross-check of the clients report and the server report on user online status

I spot-checked 12 claims: iOS at `6ad963e5b62d354da79040f388ae2b9132fb17b8` and tdesktop at `v7.0.9` through `gh api`, and the Pluma server in `/Users/sckr.a/teamgram-server`. All 12 hold, though two need a correction or a caveat. I edited, committed and deployed nothing.

## 1. Spot-checks

| # | Claim | Verdict | What I read |
|---|---|---|---|
| 1 | iOS sends `offline=false` every 30 s while it should stay online, and `offline=true` cancels the timer | **CONFIRMED** | `submodules/TelegramCore/Sources/State/ManagedAccountPresence.swift`, `updatePresence`: a one-shot `SignalKitTimer(timeout: 30.0, repeat: false)` calls `updatePresence(true)` again; the `else` branch invalidates `onlineTimer` and sends `boolTrue`. The request is only sent when the value changes (`if self.wasOnline != value`). |
| 2 | iOS: `shouldKeepOnlinePresence = primary && inForeground`, set to false at `:1197` | **CONFIRMED** | `submodules/TelegramUI/Sources/SharedWakeupManager.swift:1150` and `:1197`. |
| 3 | iOS does not store its own presence from `updateUserStatus`; it only sets `delayNotificatonsUntil` (online: `expires+30`; offline: `was_online`) | **CONFIRMED** | `AccountStateManagementUtils.swift:1508-1509` (`mergePeerPresences(..., explicit: true)`) and about `:4923-4943` (`if peerId == accountPeerId { if explicit {...} } else { presences[peerId] = status }`). |
| 4 | iOS re-renders locally at `until`, then every minute up to 1 h, and at `lastActivity+30` for "recently" | **CONFIRMED, wrong path** | The file is `submodules/PeerPresenceStatusManager/Sources/PeerPresenceStatusManager.swift`, not `submodules/TelegramUI/Components/...` (that path returns 404). `suggestedUserPresenceStringRefreshTimeout` returns `statusTimestamp - timestamp` while still online, then `(30 - difference) + 1` under 30 s, then `(difference % 60) + 1` under 1 h, then `infinity`. For `.recently` it returns `lastActivity + 30 - timestamp + 1`. |
| 5 | tdesktop defaults: `onlineUpdatePeriod`=120000, `offlineBlurTimeout`=5000, `offlineIdleTimeout`=30000 | **CONFIRMED, plus one field both reports missed** | `mtproto/mtproto_config.h`: `onlineCloudTimeout = 300000` and `notifyCloudDelay = 30000`. Both reports leave out `onlineCloudTimeout`. |
| 6 | tdesktop `updateOnline` sends `account.updateStatus(!isOnline)` when the state changes, when the period elapses, or when another session reported offline | **CONFIRMED** | `api/api_updates.cpp:989-1054`, condition `isOnline != _lastWasOnline \|\| (isOnline && _lastSetOnline + onlineUpdatePeriod <= ms) \|\| (isOnline && gotOtherOffline)`. It sets its own user to `OnlineTill(now + onlineUpdatePeriod/1000)`, or `now - 1` when going offline. |
| 7 | When tdesktop receives its own `userStatusOffline` or `userStatusEmpty`, it calls `updateOnline(..., true)`, so an active Desktop re-sends online at once | **CONFIRMED** | `api_updates.cpp:2053-2077`: the `mtpc_updateUserStatus` case, branch `UserId(d.vuser_id()) == session().userId()`. |
| 8 | Pluma `teamgramd/bin/config.json` has `online_update_period_ms` = 210000 | **CONFIRMED** | `config.json:22`. Line 25 has `online_cloud_timeout_ms` = 300000. The configuration service loads `./config.json` (`app/bff/configuration/internal/svc/service_context.go:36`). I did not check which copy production serves. |
| 9 | `account.updateStatus` uses `expires=300`, never pushes (the push is commented out under `// TODO: push`), writes through a fire-and-forget `GoSafe`, and always returns `boolTrue` | **CONFIRMED** | `app/bff/userchannelprofiles/internal/core/account.updateStatus_handler.go:47`, `:62-70`, `:72-100`, `:102`. The commented-out code also skipped blocked users in both directions (`CheckBlockUserList`, `IsBlockedByUser`). |
| 10 | `MakeUserStatus` ignores the stored `expires`: online means `now <= lastSeenAt+60`, answered as `expires = lastSeenAt+60`. The rounding thresholds are 3, 7 and 30 days, and `by_me` is never set | **CONFIRMED** | `third_party/teamgram-proto/mtproto/user_status_helper.go:87-112`. |
| 11 | `contacts.getStatuses` applies no privacy and ignores errors | **CONFIRMED** | `app/bff/contacts/internal/core/contacts.getStatuses_handler.go:42-50`: it calls `MakeUserStatus(v.LastSeenAt, true)` and drops the error from `lastSeenList, _`. |
| 12 | `privacyIsAllow` returns `true` for chat-participant rules without checking membership, and does not handle `DisallowContacts` | **CONFIRMED, one correction** | `third_party/teamgram-proto/mtproto/privacy_rule_helper.go:348-416`. Correction: under `ALLOW_ALL`, a `DisallowChatParticipants` rule makes the loop return `true` straight away. That stops the check of any `DisallowUsers` that comes after it. It does not make status visible to more people, so for that case it is a missed restriction rather than a leak. The leak described in the server report is real for `AllowChatParticipants` under `Contacts` or `Nobody`. Also, a rule list with no base rule ends with `RULE_TYPE_INVALID` and returns `false`. |

## 2. Contradictions between the reports

1. **The online window and the clients' keep-alive period.**
   - The server report assumes clients send online "roughly every minute (UNVERIFIED)".
   - The clients report gives measured periods: iOS 30 s, Web K 50 s, Android about 55 s, tdesktop 120 s by default and 210 s with Pluma's config.
   - The server answers readers with `lastSeenAt+60`. So today a Desktop user looks offline to others for about 150 s of every 210 s.
   - iOS, tdesktop and Web K flip the status to "last seen" locally at `expires`. That makes the server's `expires` the value that matters, not the server's later re-reads.
2. **The "only push when under 60 s is left" rule** (server design step 2) breaks for tdesktop on Pluma.
   - At t=0 the server pushes online with `expires = t+300`.
   - At t=210 the ping arrives with 90 s left, so nothing is pushed.
   - Viewers flip to "last seen" locally at t=300.
   - At t=420 the next ping arrives, and only then does the user show as online again.
   - The threshold has to be at least the longest client period (210 s, or `online_update_period_ms` from the config), not 60 s.
3. **Who receives the push.**
   - The clients report says contacts, open private chats and members of shared small groups. It gives no citation for this; client code cannot show the server's fan-out.
   - The server report says reverse contacts only, and treats everything else as UNVERIFIED.
   - The second position is the defensible one until a probe settles it.
4. **Pushing a user's own status to their other sessions.**
   - The clients report recommends pushing `updateUserStatus(self)`.
   - tdesktop (claim 7) re-sends online whenever it receives its own offline while its window is active. If the server pushes "offline" because the phone went to the background, the active Desktop re-sends online at once. That is wasteful but self-correcting, and it suggests Telegram uses "last request wins" and lets the active client reassert itself.
   - The server report does not mention this. It also excludes the user's own devices from recipients, which conflicts with the clients report's item 1.
5. **Where the 300 s comes from.**
   - The server report calls 300 "Telegram's own reply" without a source.
   - The only sourced 300 s value is tdesktop's `onlineCloudTimeout` default (300000) and Pluma's `online_cloud_timeout_ms` (300000). Neither of them is the `expires` that Telegram's server returns.

## 3. Uncited or weakly supported claims

- **Clients report:**
  - The recipient set (contacts, open private chats, shared small groups): no source.
  - "Real Telegram presumably sends offline": correctly marked UNVERIFIED.
  - "No status call on typing, on opening a chat, or after a push" for every client: these are claims that something is absent, with no search described. Treat them as "not found in the files read".
  - The Android line numbers point to the copies in the scratchpad. I did not re-check them against `gh api`.
  - The wrong iOS path (claim 4).
  - The Android "no expiry timer" finding is stated honestly as not found.
- **Server report:**
  - "Telegram's own reply would say online until now+300": uncited.
  - Contact semantics, block hiding and the 3/7/30-day thresholds: marked UNVERIFIED, correctly.
  - `read_dates_private` not computed: self-marked unverified.
  - The line references "about l.185-225" and "about l.325-370" in `user_privacy.go` are approximate.
  - The 10 s retry and the drop in `pushUpdatesToSession` (`core.go:188-215`): I did not check them.

## 4. Decisions the implementer still has to make

1. **The online window.**
   - What `expires` the server stores and returns for online: keep 300, or derive it from `online_cloud_timeout_ms` or `online_update_period_ms`.
   - Make `MakeUserStatus` use the stored `expires` (a `// pluma:` patch in proto, or a server-side wrapper), and make offline end the window at once.
   - Whether to lower `online_update_period_ms` from 210000, or rely on `expires` ≥ that period plus some margin.
2. **When a refresh is pushed.** Decide when an online-to-online refresh is sent: when the remaining window is below the longest client ping period, or every time `expires` moves forward by more than X. Then decide offline-to-online and online-to-offline change detection: an atomic compare-and-set in Redis, plus a guard against flapping.
3. **One state per user, or a combination of devices.**
   - Option A: last request wins. This matches tdesktop's reassert logic (claim 7).
   - Option B: the user is online while any session is online. This needs per-session state.
   - Also decide whether the user's own other sessions get the push. If they do, accept that an active tdesktop answers with an immediate extra online request.
4. **Recipients.**
   - Reverse contacts only, or also non-contact private chats and small-group members. Settle this with a two-tab probe against real Telegram before going beyond contacts.
   - Only recipients with online sessions (`status.getUsersOnlineSessionsList`, which exists).
5. **Privacy for each recipient.**
   - Fix `privacyIsAllow` first: membership checks for chat participants, `DisallowContacts`, the early `return true` cases, and a decision on Premium, Bots and CloseFriends. The alternative is to switch to the callback-based `CheckPrivacyIsAllow`.
   - Whether to send rounded statuses (`Recently`, with or without `by_me`) at all. Telegram's behaviour here is UNVERIFIED.
   - Whether Premium exists in Pluma. If it does not, `by_me` applies whenever the reciprocity rule applies.
6. **Blocking.** Skip recipients in both directions, as the commented-out upstream code intended (claim 9). The matching Telegram rule is UNVERIFIED.
7. **Unclean disconnect.** Rely on `expires` passing on the clients, or have the server send offline when the last session drops (`status.setSessionOffline`). If the server sends it, decide the delay.
8. **Delivery.** Send `updateShort` with no seq through `SyncPushUpdates` (ADR 0020 §2). Decide whether stale status events are dropped by age in the sync pusher.
9. **Storage error handling.** Write synchronously and return the error, replacing the `GoSafe` and `WrapperGoFunc` calls that lose errors.
10. **Scope.** Decide whether these go into task 032 or separate tasks:
    - `contacts.getStatuses` privacy;
    - making `ToUser` symmetric with `ToUnsafeUser`;
    - `hide_read_marks`, `read_dates_private` and `getOutboxReadDate` privacy;
    - the `privacyIsAllow` leak, which also affects photo and phone.

I wrote the fetched tdesktop and iOS files to the scratchpad: `/private/tmp/claude-503/-Users-sckr-a-teamgram-server/76bbed60-70da-4b26-9bff-19b96e18ecba/scratchpad/au.cpp` and `/private/tmp/claude-503/-Users-sckr-a-teamgram-server/76bbed60-70da-4b26-9bff-19b96e18ecba/scratchpad/asmu.swift`.
