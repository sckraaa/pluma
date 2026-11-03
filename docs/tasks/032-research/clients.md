# User online status in the official clients (pinned releases)

Sources I read:
- Android: DrKLO/Telegram at `aac1efcb83d6575efb0c819c6ddc4ea6c8cf64e1` (12.9.1, layer 228, from `~/pluma-android/upstream.env`). Fetched with `gh api` because the local `~/aaaaaaa/Telegram-Android` is a different main (its MessagesController.java differs by about 3000 diff lines).
- iOS: Telegram-iOS at `6ad963e5b62d354da79040f388ae2b9132fb17b8`.
- tdesktop: `v7.0.9`.
- Web K: `~/pluma-web/upstream` at `346844bf8c002bc1af4de20e78ae1d0df0265019`.

Fetched copies are in `/private/tmp/claude-503/-Users-sckr-a-teamgram-server/76bbed60-70da-4b26-9bff-19b96e18ecba/scratchpad/os/`. Line numbers below refer to those copies, or to the Web K tree.

---

## Android (12.9.1)

### 1. When it calls `account.updateStatus`
- **Where.** Only in `MessagesController.updateTimerProc()` (`MessagesController.java:10505-10556`).
- **How often the timer runs.** Native tgnet calls `onUpdate` on every pass of its event loop (`jni/tgnet/ConnectionsManager.cpp:291`). The loop waits at most 1000 ms (`ConnectionsManager.cpp:160-177`). Java forwards each call to `updateTimerProc` (`ConnectionsManager.java:813-815`). So the check runs about once a second.
- **`offline=false`** is sent when all of these hold (`:10513-10515`):
  - `!ignoreSetOnline`;
  - `getPauseTime()==0`, meaning the app is in the foreground;
  - `ApplicationLoader.isScreenOn`, and the main interface is not paused (with a 1 s settle);
  - either no status was ever set, or 55 000 ms have passed since the last successful set, or `offlineSent` is true.
  - So the period is **about every 55 s while in the foreground**, and at once when the app comes back after offline was sent.
  - On error it retries 5 s later (`:10528-10531`).
- **`offline=true`** is sent once after the app has been paused (backgrounded or screen off) for **2000 ms or more** (`:10537-10555`). `lastPauseTime` is set in `setAppPaused` (`ConnectionsManager.java:751-786`).
- **Calls.** An incoming VoIP call that arrives while the UI is paused or the screen is off sets `ignoreSetOnline = true` (`MessagesController.java:20418-20420`).
- **No status call** on typing, on opening a chat, or after a push.

### 2. `updateUserStatus` and `expires`
- **Applying the update** (`:19816-19836`):
  - The status is stored on the cached user and written to the DB.
  - Recently, LastWeek and LastMonth are mapped to `expires` = -100, -101 and -102.
  - The interface is told through `UPDATE_MASK_STATUS` (`:18948-18953`).
- **Its own user.** `NotificationsController.setLastOnlineFromOtherDevice(expires)` is called (`:19832-19834`, `NotificationsController.java:455-461`). While that time is in the future, local notifications are delayed by 3 s instead of 1 s (`NotificationsController.java:588`, `:3362-3370`).
- **Online expires only when the text is re-rendered.** `LocaleController.formatUserStatus` (`LocaleController.java:3007-3048`) shows Online only if `expires > getCurrentTime()`, otherwise "last seen …". Nothing schedules that re-render at the moment `expires` passes; see item 3.
- **Local "online" bump (`onlinePrivacy`).** A user whose status is hidden (`expires <= 0`, i.e. recently/lastWeek/lastMonth) is marked Online locally for 30 s when they act. Events that put them in `onlinePrivacy` (event time within 30 s of now):
  - an incoming short message (`:17889-17892`);
  - a new incoming message (`:18511-18514`);
  - a read outbox (`:18749-18752`);
  - typing (`:18935-18937`).

  Entries older than `currentServerTime - 30` are dropped in `updateTimerProc`, which repaints the status (`:10702-10717`). `formatUserStatus` returns Online for users in `onlinePrivacy` (`LocaleController.java:3017-3023`).
- **Sending typing.** Typing is not sent to a user whose `expires <= now - 30`, unless that user is "recently" (-100) or in `onlinePrivacy` (`MessagesController.java:11388-11393`).
- **Re-requests.**
  - `putUser` returns "status changed" when a fresh user has a different `expires`, and that repaints the status (`:6937-6939`, `:6985-6999`).
  - Opening a chat calls `loadUserInfo(currentUser, force=true, …)` (`ChatActivity.java:3047`).
  - If the cached full user was loaded more than 60 s ago, `users.getFullUser` is re-requested (`MessagesController.java:10471-10476`). Its `users` carry the status.
  - There is no periodic `users.getUsers` for status.

### 3. Push or poll
- Android relies on the server **pushing `updateUserStatus`**.
- The chat header recomputes only on `UPDATE_MASK_STATUS` (`ChatActivity.java:21796-21800`).
- There is no header timer for "online → last seen". The only periodic repaint is the `onlinePrivacy` expiry.
- An `expires` passing without any event is reflected at the next UI refresh. That a status update arrives on time is assumed, not confirmed; I found no expiry timer.

### 4. Recently / LastWeek / LastMonth and privacy
- **Texts.** "Lately", "WithinAWeek" and "WithinAMonth" (`LocaleController.java:3037-3042`); `expires == -1` shows "Invisible".
- **`by_me`.** Maps to -1000, -1001 and -1002, with the same texts (`:3008-3016`).
- **Profile button.** It shows the "show my last seen" button when the status is Recently/LastWeek/LastMonth with `by_me`, the user is not online, and the viewer is not Premium (`ProfileActivity.java:11360`).
- **Hide-read-time.** I did not read any hide-read-time (`hide_read_marks`) code for Android.

---

## iOS (6ad963e5)

### 1. When it calls `account.updateStatus`
- **Who decides.** `AccountPresenceManagerImpl` (`TelegramCore/Sources/State/ManagedAccountPresence.swift:21-73`) is driven by `shouldKeepOnlinePresence`.
- **`offline=false`** is sent when that becomes true. A one-shot 30.0 s timer then re-calls `updatePresence(true)` (`:45-56`). So it sends **every 30 s while the app is in the foreground**.
- **`offline=true`** is sent when it becomes false, and the timer is cancelled (`:57-61`).
- **What sets it.** `shouldKeepOnlinePresence = primary && self.inForeground` (`TelegramUI/Sources/SharedWakeupManager.swift:1150`). It is set to false when transactions are disabled, i.e. in the background (`:1197`).
- **No status call** on typing, on opening a chat, or after a push.

### 2. `updateUserStatus` and `expires`
- **Applying the update.** `updateUserStatus` → `mergePeerPresences(…, explicit: true)` (`AccountStateManagementUtils.swift:1508-1509`).
- **Its own user** (`:4923-4943`): the status is **not stored**. It only sets `delayNotificatonsUntil`:
  - for online, `expires + 30`;
  - for offline, `was_online`.
- **Mapping** (`ApiUtils/TelegramUserPresence.swift:7-29`):
  - online → `.present(until: expires)`;
  - offline → `.present(until: was_online)`;
  - recently/lastWeek/lastMonth → `.recently/.lastWeek/.lastMonth(isHidden: by_me)`.
- **Local bump.** Activity records `lastActivity`:
  - from an incoming non-automatic message author's timestamp (`AccountStateManagementUtils.swift:4055`);
  - from a ReadOutbox timestamp (`:4597`).

  It is applied at `:5682-5683`. `updatePeerPresenceLastActivities` (`UpdatePeers.swift:491-512`) raises `.present(until)` to the event time if it was older. A `.recently` user is shown Online while `lastActivity + 30 >= now` (`PresenceStrings.swift:618-623`).
- **Re-requests.**
  - Open peer views trigger `users.getFullUser` (`UpdateCachedPeerData.swift:244`) through `AccountViewTracker.updateCachedPeerData`, at most once per **5 minutes** per peer (`60.0 * 5`).
  - The users in the result go through `updatePeers` → `updatePeerPresences` (`UpdatePeers.swift:133, 200, 430-458`).

### 3. Push or poll
- iOS uses pushed updates, plus a **local re-render timer**: `PeerPresenceStatusManager` (`PeerPresenceStatusManager/Sources/PeerPresenceStatusManager.swift:5-33`) fires at:
  - `until - now` (the online → "last seen" flip, done locally);
  - 30 s after the end;
  - each minute up to 1 h;
  - `lastActivity + 30` for "recently".
- The chat title uses it (`ChatTitleView.swift:647-651`). So iOS expires Online locally at `expires` without asking the server.

### 4. Display
- `stringAndActivityForUserPresence` (`PresenceStrings.swift:574-631`):
  - Online while `until >= now`;
  - otherwise just now, minutes, hours or date;
  - `LastSeen_Lately`, `LastSeen_WithinAWeek`, `LastSeen_WithinAMonth`, `LastSeen_ALongTimeAgo`.
- `isHidden` (`by_me`) is parsed, but this function does not change the text for it.
- I did not read the hide-read-time UI.

---

## tdesktop (v7.0.9)

### 1. When it calls `account.updateStatus`
- **Where.** `Api::Updates::updateOnline` (`api/api_updates.cpp:989-1054`).
- **Online condition.** `isOnline = Core::App().hasActiveWindow(session)` and idle time `< offlineIdleTimeout`.
- **When it sends.** It sends `account.updateStatus(!isOnline)` when any of these holds:
  - the state changed;
  - online and `onlineUpdatePeriod` has elapsed;
  - online and another session reported offline.

  It then re-arms `_onlineTimer` for `min(onlineUpdatePeriod, time left to idle)`.
- **Locally on each send.** It sets self to `OnlineTill(now + onlineUpdatePeriod/1000)`, or `-1` when going offline (`:1036-1042`). On going offline it saves the cloud draft.
- **Timings come from `help.getConfig`** (`mtproto/mtproto_config.cpp:246-251`). Defaults (`mtproto_config.h:20-26`):
  - `online_update_period_ms` = 120000;
  - `offline_blur_timeout_ms` = 5000;
  - `offline_idle_timeout_ms` = 30000.
- **Pluma's `teamgramd/bin/config.json:22`** has `online_update_period_ms` = **210000**, so Desktop on Pluma re-sends online every 210 s. I did not check that this file is the one deployed.
- **Triggers.**
  - Activity — key press, mouse press, touch, wheel, app activate — calls `updateNonIdle()` (`core/application.cpp:674-711`). That calls `checkIdleFinish` (`:1297-1300`), which re-sends online after idle (`api_updates.cpp:1056-1068`). While idle it re-checks every 900 ms.
  - Deactivating the app calls `updateOnline()` (`application.cpp:655-665`), so offline goes out at once.
  - Getting the updates state calls `updateOnline()` (`api_updates.cpp:483`).
  - On quit, it sends offline and holds the quit until the request completes (`:1022-1033`, `:1078-1084`).
- **No status call** on typing or on opening a chat.

### 2. `updateUserStatus`
- **Applying the update** (`api_updates.cpp:2053-2077`): `LastseenFromMTP` (`data/data_user.cpp:114-134`) maps online and offline to `OnlineTill(expires / was_online)`.
- **Recently.** It keeps a *local* online value if one is active; otherwise it becomes `Recently(by_me)`.
- **Its own user.**
  - `userStatusOffline` or `userStatusEmpty` calls `updateOnline(…, gotOtherOffline=true)`. If this Desktop is active, it **immediately re-sends online**.
  - Online or offline also sets `cSetOtherOnline`.
- **Local bump.** `UserData::madeAction(when)` sets `OnlineTill(when + 30)` (`kSetOnlineAfterActivity = 30`, `data_user.cpp:45, 526-538`). It is called:
  - on a new message from that user (`history/history.cpp:1657`);
  - on typing (`data/data_send_action.cpp:61`);
  - on read outbox or contents read (`api_updates.cpp:1338, 1370, 1819`; skipped while getting difference).
- **Local expiry.**
  - `Session::watchForOffline` / `checkLocalUsersWentOffline` (`data/data_session.cpp:1406-1477`) fire when `onlineTill` passes and send `OnlineStatus` peer updates.
  - The top bar also re-arms `_onlineUpdater` with `OnlineChangeTimeout` (`history/view/history_view_top_bar_widget.cpp:1977-2001`, `data_peer_values.cpp:30-48, 428-435`).
  - The phrase changes at `till`, then every minute up to 1 h, then every hour up to 12 h, then at midnight.
- **Re-requests.**
  - After a last-seen privacy change, `ApiWrap::updatePrivacyLastSeens` (`apiwrap.cpp:2127-2172`) runs.
  - For non-Premium users it locally converts every known status to `Recently/WithinWeek/WithinMonth/LongAgo(by_me=true)` using thresholds of 3, 7 and 30 days.
  - It then calls `contacts.getStatuses`.

### 3. Push or poll
- Pushed `updateUserStatus` updates, plus local timers for expiry and text changes.
- There is no periodic status polling of `users.getUsers` (from the files read).

### 4. Display
- `OnlineTextCommon` (`data_peer_values.cpp:69-84`): Online, `lng_status_offline` (long ago), recently, last week, last month.
- **Hidden statuses** also show "recently".
- `OnlineText` (`:444-472`): just now, minutes up to 1 h, hours up to 12 h, today at, yesterday at, date.

---

## Web K (346844bf)

### 1. When it calls `account.updateStatus`
- **Where.** `appImManager` reacts to `idleController` changes (`src/lib/appImManager.ts:346-353`):
  - It sends at once: `offline=idle`.
  - While not idle it repeats **every 50 s** (`setInterval(…, 50e3)`).
- **What is idle.** Idle means window `blur`; activity means `focus`, or the first `mousemove`/`touchstart` after load (`src/helpers/idleController.ts:18-23, 59-61`). There is no inactivity timeout while the window has focus.
- **Locally.** `updateMyOnlineStatus` first sets self locally to online with `expires = now + 50`, or offline with `was_online = now` (`appUsersManager.ts:1223-1249`).
- **Explicit offline** (`goOffline`) on logout and account switch (`sidebarLeft/index.ts:866, 1757`).
- **No status call** on typing, on opening a chat, or after a push.

### 2. `updateUserStatus`
- **Applying the update.** It replaces `user.status` and dispatches `user_update` (`appUsersManager.ts:77-89`). Self is not treated specially.
- **Clock offset.** `expires` is adjusted by the server time offset when users are saved (`:617-618`).
- **Local expiry.** `updateUsersStatuses` runs every **60 s** and on `state_synchronized` (`:68-70, 975-992`). It turns an expired `userStatusOnline` into `userStatusOffline{was_online: expires}`.
- **Local bump.** `forceUserOnline(id, eventTs)` sets `userStatusOnline{expires: now+60}` if the event is less than 60 s old and the user is not already online or empty (`:994-1024`). It is called:
  - on an incoming non-offline message (`appMessagesManager.ts:9655-9656`);
  - on read outbox (`:10032-10033`);
  - on typing (`appProfileManager.ts:1494`);
  - on `updateUserName` and `updateUserEmojiStatus` (`appUsersManager.ts:122, 139`).

### 3. Push or poll
- Pushed updates, plus local timers. The top bar re-renders on `user_update`, on typing, and every 60 s (`components/chat/topbar.ts:1752-1767`).
- I found no periodic status polling of `users.getUsers`.

### 4. Display
- `getUserStatusString.ts:39-87`:
  - Recently → `Lately`; LastWeek → `WithinAWeek`; LastMonth → `WithinAMonth`;
  - offline → just now, minutes, hours today, or "last seen at";
  - online → `Online`; empty → `ALongTimeAgo`.
- The text is the same with `by_me`.

---

## Summary: what the server must do

### What the server must send
1. **To the user's other sessions.** On `account.updateStatus`, push `updateUserStatus(self)` to the same user's other sessions:
   - tdesktop re-sends online when it sees itself go offline while active;
   - iOS and Android use it only to delay notifications.
2. **To every user allowed to see the status.** Push `updateUserStatus(userId, status)`:
   - `userStatusOnline{expires}` when the user goes online;
   - `userStatusOffline{was_online}` when they go offline.

   Those who see it are contacts, open private chats and members of shared small groups, filtered by the last-seen privacy rules. That a user who cannot see the exact time gets `userStatusRecently/LastWeek/LastMonth` follows from the clients' mappings; exactly when the server sends those is not visible in client code.
3. **`by_me`.** Set it when the viewer is hidden because of the *viewer's own* privacy setting. Android uses it for the "show my last seen" button; tdesktop itself creates `by_me` statuses after a privacy change.
4. **Fresh `user.status` in responses.** Every `users.getUsers`, `users.getFullUser`, `contacts.getStatuses` and dialog/difference response should carry the current `user.status`:
   - Android re-fetches the full user when a chat opens, if the cache is older than 60 s;
   - iOS at most every 5 min per open peer;
   - tdesktop calls `contacts.getStatuses` after a privacy change.
5. **Sending offline when the client does not.** Real Telegram presumably sends `userStatusOffline` itself when no keep-alive arrives before `expires` (UNVERIFIED). The clients flip Online to "last seen" locally at `expires` (iOS, tdesktop, Web K, and Android on its next re-render). So the server's `expires` must cover the client's ping period.

### Who sees the status on screen
- Chat header, profile and contact lists of the viewer.
- All clients show a local 30–60 s "online" after a message, typing or a read event from the user, even when the status is hidden:
  - Android: 30 s, for hidden statuses;
  - iOS: 30 s, via `lastActivity`;
  - tdesktop: 30 s;
  - Web K: 60 s.

### Client timings

| Client | Online keep-alive | Goes offline | Local expiry and re-render |
|---|---|---|---|
| Android | about every 55 s in the foreground (1 s tick) | 2 s after pause or screen off | no timer; repaints on updates and when the 30 s activity window ends |
| iOS | every 30 s in the foreground | at once on background | `PeerPresenceStatusManager` fires at `expires`, then each minute |
| tdesktop | every `online_update_period_ms` (default 120 s, Pluma config 210 s) | on deactivation, or after `offline_idle_timeout_ms` (30 s) idle | timer at `expires`, then minute, hour, midnight |
| Web K | every 50 s while focused | at once on window blur; on logout or account switch | sweep every 60 s; header every 60 s |

**Implication for `expires`.** If the server's online `expires` is shorter than tdesktop's 210 s period, Desktop users will flicker to "last seen" for others between pings. Telegram's own server value is UNVERIFIED; I did not read Pluma's server code.

### Not verified
- Android and iOS hide-read-time (`hide_read_marks`) code.
- Which `config.json` the production server actually serves.
