## iOS plan: login code screens (12.9.2 in wt-071-ios, 13.2 in wt-074-ios)

**Paths.** All paths below are relative to the upstream tree, the same in both. Line numbers are given as 12.9.2/13.2, and a single number means they match. I only read files and changed nothing.

### 1. How the Fragment screen is built now
The screen is drawn by `submodules/AuthorizationUI/Sources/AuthorizationSequenceCodeEntryControllerNode.swift`. There is one node for every code type, and `containerLayoutUpdated` (505/506) stacks items with `AuthorizationLayoutItem`:

- **Sticker on top.** `animationNode` shows a 100×100 sticker loaded from `Telegram/Telegram-iOS/Resources/<name>.tgs`. Fragment uses `IntroFragment`; the App type uses `IntroMessage`, a message-bubble sticker our brand patch does not replace (it is not in `BRAND_REPLACED`). Fragment plays it 3 times with `.count(3)`.
- **Title.** `titleNode` is semibold 28 and centred, set by a `switch codeType`. The `.sms` case is at 532/544, `.fragment` at 534/546, and `.otherSession` falls into `default` (`Login_EnterCodeTelegramTitle`).
- **Description.** `currentOptionNode` is markdown 17pt, built by `authorizationCurrentOptionText` in `submodules/AuthorizationUtils/Sources/AuthorizationOptionText.swift` (`.sms` at :16, `.otherSession` at :18, `.fragment` at :40).
- **Code field.** `codeInputView` takes its length from the type.
- **Big button.** `proceedNode` is a `SolidRoundedButtonNode(... glass: true, height: 50, cornerRadius: 25)` at 253/254, pinned to the bottom above the keyboard. Fragment sets `title = Login_OpenFragment`, a navy theme `0x37475a` and the Lottie icon `anim_fragment` (546–555 in 13.2), and shows it in the `if case .fragment = codeType` branch (750/759). Keyboard insets for `.email, .fragment` are set at ~565/575.
- **Tap handling.** `proceedPressed` (1077/1110) calls `openFragment(url)`. The Fragment screen has no "Send again" link.
- **Resend link and timer.** `nextOptionButtonNode` with `nextOptionTitleNode` gets its text from `authorizationNextOptionText`. When `nextType` and `timeout` are set, a one-second timer in `updateData` (409/410) shows "You can request an SMS in m:ss" (`Login_WillSendSms`, :77–83) with the link inactive, then the active link (:105–131).
- **What the link does.** A tap leads to `AuthorizationSequenceController.swift:635`. If `nextType == nil` (:642–649), the client calls `reportMissingCode` and opens a mail to `sms@telegram.org` through `presentDidNotGetCodeUI` (:1453). Otherwise it calls `resendAuthorizationCode`, and errors map to alerts (`limitExceeded` → `Login_CodeFloodError`). The controller sets the Fragment action at :721.

### 2. Changes (both trees; the anchors are identical apart from the noted line shifts)

**a) Strings (en is the only bundled language).** Add a new step `patch_login_codes(tree)` to `patch()`, right after `patch_names` (wt-074 `ci/patch.py:1638`). It must run after the rename, which asserts `STRINGS_RENAMED = (585, 638)`.

```python
LOGIN_CODE_STRINGS = {  # pluma: name the real Telegram app; kept by verify_names
 "Pluma.Login.CheckTelegramTitle": "Check Telegram",
 "Pluma.Login.CheckTelegramText": "We've sent the code for **%@** to your **Telegram** app, in a message from **Pluma**.",
 "Pluma.Login.DidNotGetCode": "Didn't get the code?",
 "Pluma.Login.SendAgainIn": "Send again in %@",
 "Pluma.Login.SendAgain": "Send Again",
 "Pluma.Login.NotDeliveredTitle": "Code Didn't Reach Telegram",
 "Pluma.Login.NotDeliveredText": "Telegram couldn't find **%@** by number. In Telegram, open **Settings > Privacy and Security > Phone Number** and set **Who can find me by my number** to **Everybody**, then tap Send Again.\n\nNo Telegram? Ask the administrator for the code and enter it here.",
 "Pluma.Login.OpenTelegramSettings": "Open Telegram Settings",
}
```

- **Appending.** `tree.expect(STRINGS, f'"{k}" = ', 0)` for every key, then append the block to `STRINGS`. `//Telegram:GeneratedPresentationStrings` (`Telegram/BUILD:146`) generates the accessors `strings.Pluma_Login_*` from the en file; values with `%@` become functions returning `FormattedString`.
- **Exceptions.** `verify_names` (:4054) skips an entry only if its key is in `LOGIN_CODE_STRINGS` and its value matches exactly. The step also checks each key occurs exactly once.
- **Labels.** The path in the text matches Telegram iOS's labels: `Settings.PrivacySettings`, `Privacy.PhoneNumber`, `PrivacyPhoneNumberSettings.DiscoveryHeader`.

**b) Other languages come from the server langpack.** The generated getter falls back from the primary component to the secondary, then to the bundled en (`build-system/GenerateStrings/GenerateStrings.py:454–475`). A missing ru key therefore shows English and does not crash.

- **Server gap.** ADR 0025 says our own texts are not added by import; the snapshot holds only Telegram's keys. The server needs a new mechanism: a table of Pluma keys per pack and language. It must bump the language version so clients fetch the difference, and `Rebrand` must skip it (`keepKeys` in `pkg/langpack/rebrand.go:53` is per key; a key prefix such as `Pluma.` is simpler).
- **Login screens are probably English for most users.** Before login, a fresh install shows them in English unless the person picks the suggested language on the intro screen. I did not verify this.

**c) Texts** (`AuthorizationOptionText.swift`):
- **Description.** `.otherSession` (:18) uses `Pluma_Login_CheckTelegramText(phone)`. `.sms` (:16) uses `Pluma_Login_NotDeliveredText(phone)`.
- **Link.** In `authorizationNextOptionText`, add before the `nextType`/`timeout` branch (:73):
```swift
// pluma: the code comes from Telegram; the link asks the server to send it again.
switch currentType {
case .otherSession, .sms:
    if let timeout, timeout > 0, nextType != nil {
        return (NSAttributedString(string: strings.Pluma_Login_SendAgainIn(String(format: "%d:%.02d", timeout / 60, timeout % 60)).string, font: font, textColor: primaryColor, paragraphAlignment: .center), false)
    }
    let title = currentType == .sms ? strings.Pluma_Login_SendAgain : strings.Pluma_Login_DidNotGetCode
    return (NSAttributedString(string: title, font: font, textColor: accentColor, paragraphAlignment: .center), true)
default: break
}
```
(`SentAuthorizationCodeType` has associated values, so in practice this needs `if case .sms = currentType`.)

**d) Node** (`...CodeEntryControllerNode.swift`):
- **Fields.** Add `var openTelegramSettings: (() -> Void)?` and `var telegramSettingsAvailable = false` next to `openFragment` (:89).
- **Title.** Add `case .otherSession:` before `default`, using `Pluma_Login_CheckTelegramTitle`. Replace the `.sms` title (532/544) with:
```swift
case .sms: // pluma: Telegram did not deliver the code (Fragment's layout, our texts)
    self.titleNode.attributedText = NSAttributedString(string: self.strings.Pluma_Login_NotDeliveredTitle, font: Font.semibold(28.0), textColor: self.theme.list.itemPrimaryTextColor)
    self.proceedNode.title = self.strings.Pluma_Login_OpenTelegramSettings
    self.proceedNode.updateTheme(SolidRoundedButtonTheme(theme: self.theme))
    self.proceedNode.isEnabled = true
    animationPlaybackMode = .count(3)
```
  The sticker stays `IntroMessage`. The button keeps the native accent and glass with no icon: `anim_fragment` is Fragment's logo, and the Telegram logo asset is replaced by our brand.
- **Keyboard inset.** Add `.sms` to `case .email, .fragment` (~565/575).
- **Layout.** In the bottom branch (750/759), add a case before the Fragment one:
```swift
} else if case .sms = codeType {
    additionalBottomInset = 100.0
    self.nextOptionButtonNode.isHidden = false
    items.append(AuthorizationLayoutItem(node: self.nextOptionButtonNode, size: nextOptionSize, spacingBefore: AuthorizationLayoutItemSpacing(weight: 50.0, maxValue: 120.0), spacingAfter: AuthorizationLayoutItemSpacing(weight: 0.0, maxValue: 0.0)))
    self.proceedNode.isHidden = !self.telegramSettingsAvailable
    transition.updateFrame(node: self.proceedNode, frame: /* same buttonFrame as .fragment */)
}
```
  This is the order the word/phrase screens use: "Send Again" link, then the big button.
- **Tap.** In `proceedPressed`, add `case .sms: self.openTelegramSettings?()`.

**e) Controller** (`AuthorizationSequenceCodeEntryController.swift`): pass both fields through, next to :25 and :110.

**f) Sequence controller** (`AuthorizationSequenceController.swift`):
- **Link.** At :642, replace the `nextType == nil` branch (reportMissingCode, mail to `sms@telegram.org`) with the same `resendAuthorizationCode` path; its error alerts never mention Telegram support.
- **Button.** Next to :721:
```swift
// pluma: tg:// is Telegram's scheme (Pluma registers its own), so it opens the real Telegram app.
controller.telegramSettingsAvailable = strongSelf.sharedContext.applicationBindings.canOpenUrl(telegramPhonePrivacyUrl)
controller.openTelegramSettings = { [weak self] in self?.sharedContext.applicationBindings.openUrl(telegramPhonePrivacyUrl) }
```
  `let telegramPhonePrivacyUrl = "tg://settings/phone_privacy"`. Telegram iOS handles this link (`TelegramUI/Sources/OpenUrl.swift:1013–1026` → `OpenResolvedUrl.swift:1107`, 13.2).

**g) `TelegramUI/Sources/AppDelegate.swift`.** `openUrl` drops every `tg` URL (803/851). Narrow it rather than add a new binding:
```swift
// pluma: Telegram's privacy settings, opened in the Telegram app from the login code screen.
if parsed.scheme == "tg" && !(parsed.host == "settings" && parsed.path == "/phone_privacy") {
```
`openUniversalUrl` is not an option: it passes `universalLinksOnly`.

**h) `Telegram/BUILD` `LSApplicationQueriesSchemes`** (1582/1710). Add `<string>tg</string>` after `rizogo` (1621/1749); without it `canOpenURL` returns false.

**Verify steps** (`verify_login_codes`, appended in `verify()`):
- the strings have exact values;
- the node has `Pluma_Login_NotDeliveredTitle` and `openTelegramSettings?()`;
- `presentDidNotGetCodeUI(` and `reportMissingCode(` have no callers in `AuthorizationSequenceController.swift` or `SettingsUI/Sources/ChangePhoneNumberController.swift`;
- the AppDelegate exception occurs exactly once;
- `tg` is in the query schemes;
- `verify_scheme` still passes: it checks only `CFBundleURLSchemes`, and our `tg://` is not in a `SCHEME_LINKS` file.

Tests: a `LoginCodeScreensTest` class in `ci/patch_test.py` on fixture snippets, plus a full `patch.py` run on both upstream checkouts.

**i) Changing the phone number** (optional, moderate cost). `ChangePhoneNumberController.swift:104–113` calls `reportMissingCode` and `presentDidNotGetCodeUI` and passes `nextType: nil`. Replace that with `context.engine.accountData.requestNextChangeAccountPhoneNumberVerification(...)` (`TelegramEngineAccountData.swift:79`, which calls `auth.resendCode`) followed by `codeController.updateData(...)`. Set the same two fields there. The screen itself already comes for free.

### 3. What the server must send
- **App screen:** `sentCodeTypeApp` with `next_type = codeTypeSms` and `timeout`. That gives "Send again in 0:42", then "Didn't get the code?".
- **Fallback:** `resendCode` answers `sentCodeTypeSms` with `next_type = codeTypeSms` and a timeout. The next resend tries Telegram again and answers App if the code was delivered.
- **Rate limit:** the timeout should match the server's limit. Otherwise the person gets `FLOOD_WAIT` → `limitExceeded` → "Too many attempts, please try again later." (no mention of Telegram).

### 4. Risks
- **Upstream drift.** Anchors are exact-count `replace`/`replace_lines` calls, so any upstream change stops the patch, which is intended. The two trees differ in the bottom-band code (13.2 has `bottomBandMinX` and `authorizationLayoutItemsFit`), so the button-frame snippet differs per tree.
- **Small screens on 12.9.2.** 12.9.2 lacks the "drop the sticker if it doesn't fit" logic, so the long description may crowd small screens; consider shorter text or `layout.size.width` checks.
- **Resend state.** A resend changes the type to Sms or back to App through `UnauthorizedAccountState`. I expect the sequence controller to refresh the code screen (`updateData` restarts the timer) but did not verify it. Without `timeout` the link is active at once, and repeated taps hit the server's flood limit.
- **Leftover button state.** `proceedNode`'s title and theme persist when the type changes; they only matter when the button is visible.
- **Telegram installed after the screen opens.** `canOpenUrl` is read once. Re-read it on `applicationIsActive` (`AuthorizationSequenceController.swift:103`) if the person installs Telegram meanwhile.
- **Telegram not signed in.** If the user's Telegram app is not signed in, the link opens Telegram but no settings screen; I did not verify what Telegram shows.

### 5. Local testing
1. Build `debug_sim_arm64` with `ci/build.sh` against a local stand.
2. Make the stand answer App, and Sms after a resend: the Telegram account cannot find the number, or a test switch on the stand forces it.
3. With no Telegram in the simulator, the fallback screen should show without the button.
4. Build upstream Telegram into the same simulator with `ci/build-reference.sh` (it registers `tg`). The button should then appear and open Telegram on Privacy > Phone Number.
5. Check en, ru (after the server overlay), dark mode and keyboard on/off on iPhone SE and Pro Max sizes. Check the timer, the flood alert and the change-number flow.
