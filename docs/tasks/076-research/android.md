ANDROID (13.2.1, upstream 7be672ea), plan. I read the sources and changed no files. The line numbers below are in ~/pluma-work/reference-src-new/android/TMessagesProj/src/main/java/org/telegram/ui/LoginActivity.java, and patch.py lines are in ~/pluma-work/wt-074-android/ci/patch.py.

1. How the native screens are built (read from the source)
- Routing (1933-1959). `sentCodeTypeApp` goes to `VIEW_CODE_MESSAGE` with type `AUTH_TYPE_MESSAGE`. `Sms` and `FirebaseSms` go to `VIEW_CODE_SMS`. `FragmentSms` goes to `VIEW_CODE_FRAGMENT_SMS` with `params.url`. All three are the same class, `LoginActivitySmsView`, built at 693/694/707.
- Header (3966-3988):
  - App: Lottie `R.raw.code_laptop` at 128dp, title `SentAppCodeTitle`.
  - Sms and Fragment: Lottie `R.raw.sms_incoming_info` at 64dp (the Bubble layer takes the accent colour, 4351), title `SentSmsCodeTitle` ("Enter code").
  - Below the header: `confirmTextView` (14sp, grey, `**bold**` through `replaceTags`; Fragment adds 16dp side margins), then `CodeFieldContainer` (42dp tall, 32dp above).
- Fragment bottom (4120-4155):
  - An `errorViewSwitcher` with `scale_in`/`scale_out` and `CubicBezierInterpolator.DEFAULT`.
  - Inside it, a 52dp, MATCH_PARENT `LinearLayout` button: rounded 6dp `createSimpleSelectorRoundRectDrawable`, colour `key_changephoneinfo_image2`, pressed colour `key_chats_actionPressedBackground`, 34dp padding, the 36dp Lottie `R.raw.fragment` icon and bold white 15sp `OpenFragment`. A tap runs `startActivity(ACTION_VIEW, url)`.
  - The switcher sits in `bottomContainer` with a 32dp bottom margin (4163-4166). `wrongCode` swaps with the button on a wrong code (3855).
  - **Fragment has no timer and no resend link**: `problemFrame`/`problemText` are never created for it (4093, 4171).
- Sms bottom: `problemFrame` holds `timeText` (the countdown, which can be tapped when it reaches zero, 4030-4080) and `problemText`. A tap on `problemText` runs `resendCode()` (4386), or, when `nextType==0`, `auth.reportMissingCode` plus a dialog with a `sms@telegram.org` mailer (4185-4327).
- Countdown texts: `SmsAvailableIn` and `ResendSmsAvailableIn` (4723, 4883, 4885).
- Change number uses the same views (`LoginActivity.changePhoneNumber()`, 524), so change-phone gets this for free.

**Design decision.** Keep the Sms page, which already has the timer, the resend state machine and the `FLOOD_WAIT`/`PHONE_CODE_EXPIRED` alerts (4428-4440), and give it the Fragment look:
- Fragment's 16dp text margins.
- The same filled 52dp button, placed in `bottomContainer` above the switcher. Our "Send again" link and timer stay under the button, so we do not rebuild the timer on the Fragment page.
- No Fragment Lottie on the button: a text-only button, or the `R.drawable.msg_settings` icon tinted white.

2. Java edits: a new function `login_codes(tree)` in patch.py
Anchors use `tree.replace` and `replace_body` (patch.py 127-158), so a changed anchor stops the build.
- 3985 title:
  `getString(currentType == AUTH_TYPE_MESSAGE ? R.string.PlumaLoginAppTitle : currentType == AUTH_TYPE_SMS ? R.string.PlumaLoginSmsTitle : R.string.SentSmsCodeTitle)`
- 3987: `int sideMargin = currentType == AUTH_TYPE_FRAGMENT_SMS || currentType == AUTH_TYPE_SMS ? 16 : 0;`
- 4163-4166, the `centerContainer == null` branch: for `AUTH_TYPE_SMS`, put a vertical `LinearLayout` with [button, errorViewSwitcher] into `bottomContainer`:
```java
// pluma: the code goes through Telegram; its privacy setting is one tap away.
openTelegramButton = new TextView(context);
openTelegramButton.setText(getString(R.string.PlumaLoginOpenTelegramSettings));
openTelegramButton.setTextColor(Color.WHITE);
openTelegramButton.setTextSize(TypedValue.COMPLEX_UNIT_DIP, 15);
openTelegramButton.setTypeface(AndroidUtilities.bold());
openTelegramButton.setGravity(Gravity.CENTER);
ThemeBind.selectorBackground(openTelegramButton, Theme.createSimpleSelectorRoundRectDrawable(dp(6),
    Theme.getColor(Theme.key_changephoneinfo_image2), Theme.getColor(Theme.key_chats_actionPressedBackground)),
    ThemeBind.of(Theme.key_changephoneinfo_image2), ThemeBind.of(Theme.key_chats_actionPressedBackground));
openTelegramButton.setOnClickListener(v -> {
    try { getContext().startActivity(telegramPrivacyIntent()); } catch (ActivityNotFoundException e) { FileLog.e(e); }
});
column.addView(openTelegramButton, LayoutHelper.createLinear(MATCH_PARENT, 52, 0, 0, 0, 12));
```
  - `telegramPrivacyIntent()` is `new Intent(ACTION_VIEW, Uri.parse("tg://settings/phone_privacy")).addFlags(FLAG_ACTIVITY_NEW_TASK)`.
  - Its visibility is set in `setParams` (around 4588) as `intent.resolveActivity(pm) != null ? VISIBLE : GONE`, so it is checked again on every show (the user may install Telegram meanwhile).
  - Pluma no longer claims `tg` (patch.py 3378), so the intent resolves only to Telegram.
  - Telegram's side: `LaunchActivity` 2641-2655 maps `phone_privacy` to `open_settings=14`, and 3204 opens `PrivacyControlActivity(PRIVACY_RULES_TYPE_PHONE)`, whose section header is `PrivacyPhoneTitle2`, "Who can find me by my number?" (PrivacyControlActivity 2192).
- 4653-4662 confirm text: App uses `R.string.PlumaLoginAppText`; Sms uses `R.string.PlumaLoginSmsText`, both through `formatString` plus `replaceTags` with `addNbsp(number)`.
- 4668-4679 link text: App uses `PlumaLoginAppProblem` ("Didn't get the code?"); Sms uses `PlumaLoginSendAgain`.
- 4723 and 4883/4885 countdown (`currentType == AUTH_TYPE_SMS` only): `PlumaLoginSendAgainIn` ("Send again in %1$d:%2$02d"). Replace by context lines; the plain `SmsAvailableIn` call occurs 4 times.
- 4183-4327 (`replace_body`, `must_contain=("TL_auth_reportMissingCode","sms@telegram.org")`): the whole `nextType==0` branch becomes `resendCode();`. No support mail and no missing-code report.
- Manifest `<queries>` (AndroidManifest.xml 87-93): add `<intent><action VIEW/><data android:scheme="tg"/></intent>`. Without it, Android 11+ hides Telegram from `resolveActivity`.

3. Strings and patch.py mechanics
- Upstream ships strings.xml in 10 folders: values, ar, de, es, it, ko, nl, pt-rBR, ru, uk (`STRING_RENAMES`, 1869).
- Add a table `LOGIN_STRINGS = {key: {lang: text}}` with the prefix `PlumaLogin*`. `login_codes()` checks:
  - the language set equals `STRING_RENAMES`;
  - each key appears 0 times in upstream, so a clash stops the build;
  - each language inserts before the single `</resources>` (expect count 1).
  - Escape `\'` as the neighbouring strings do.
- **Order matters.** Call `login_codes(tree)` last in `patch()`, after `brand()` (patch.py 343). Before that, `brand_strings` would rename our "Telegram" and break its exact counts, and `brand_scheme` would turn our `"tg://settings/phone_privacy"` into plm: and break `SCHEME_JAVA["ui/LoginActivity.java"] (1,0)`.
- Verify-side exceptions:
  - `verify_brand` (3579-3584): strip `<string name="PlumaLogin\w+">…</string>` before the `TELEGRAM_WORD`/`LOCAL_NAMES` scan. ar and ko keep تيليجرام and 텔레그램.
  - `SCHEME_KEEP` (around 2004): add `"ui/LoginActivity.java": 1`.
  - `TELEGRAM_SITE_KEEP["ui/LoginActivity.java"]` goes from 3 to 2: the mailer is gone, and the one at 10245 in LoginPayView stays.
  - `verify_brand` 3606: check `android:scheme="tg"` only in the part after `<application`, because `<queries>` now has it.
  - A new `verify_login_codes(tree)` in `verify()` (3649) checks:
    - every key/text in every language, and that each text still contains the word Telegram;
    - `R.string.PlumaLogin*` is used, with the number of uses in Java;
    - no `TL_auth_reportMissingCode` or `sms@telegram.org` left in `LoginActivitySmsView`;
    - `scheme="tg"` is in `<queries>`.
- Settings menu path in each language: copy it verbatim from upstream's own strings of that language before the rename (`PrivacySettings`, `PrivacyPhone`, `PrivacyPhoneTitle2`, `LastSeenEverybody`). `patch_test.py` compares them, so the path matches what Telegram really shows. Example en text:
  `<string name="PlumaLoginSmsText">We couldn\'t deliver the code for %1$s to Telegram. In Telegram, open **Settings › Privacy and Security › Phone Number**, set **Who can find me by my number?** to **Everybody**, then tap Send again.\n\nNo Telegram? The administrator will give you the code to enter here.</string>`
  - Title: "Code didn't reach Telegram" / "Код не дошёл до Telegram".
  - App screen: title "Check Telegram"; text "We sent the code for %1$s to your **Telegram**, in a message from the Pluma account."
- **Server langpack.** Android lays the `android` server pack over the bundled strings (STATUS 021). Keys missing there fall back to the bundled text, so the 9 to 19 server-only languages show English. For "all languages", the server langpack needs a Pluma overlay with the same keys, exempt from `pkg/langpack/rebrand.go`. `pkg/langpack` has no overlay today, so this is server work.

4. Risks
- **Upstream drift.** Every anchor is count-checked. The `SmsAvailableIn` sites are the brittle ones, so anchor them with their surrounding `if` line.
- **Resend state machine:**
  - Sms→Sms runs `fillNextCodeParams` → `setPage(VIEW_CODE_SMS)` back onto the same view. That path exists upstream (nextType SMS) but must be checked on a device.
  - The server must send `next_type=codeTypeSms` and a real `timeout`. With 0, the client uses 60s (1938). Otherwise the link shows the wrong text or no timer.
  - App has no timer, so the link is active at once. A server rate limit is required.
- **Flood errors.** `FLOOD_WAIT` shows the generic `FloodWait` alert (4439), with no Telegram mention. Other errors show `error.text`; the server must return documented codes only.
- `setWaitingForSms(true)` (4561) registers the SMS Retriever. It is harmless and left as is.
- The Telegram beta and web APKs also take `tg:`, which is fine. A third-party client claiming `tg` would also open, which is acceptable.

5. Local test
- `python3 ci/patch_test.py`, plus a test for each new check, each shown to fail without its edit.
- `patch.py` against a fresh upstream checkout, then build on CachyOS.
- Android emulator with a Google Play image (Telegram from Play, logged in), and a second one without Telegram so the button is hidden.
- Point at the local stand:
  - force the server's delivery to fail, so it answers Sms;
  - check the button opens Telegram's "Phone Number" privacy page;
  - check Send again after the countdown, change number from Settings, and the ru, ar and ko layouts in light and dark;
  - check a rapid resend gets `FLOOD_WAIT`.
- Take screenshots of each state.
