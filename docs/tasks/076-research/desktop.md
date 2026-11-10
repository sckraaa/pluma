DESKTOP (tdesktop 7.4.1): plan for the login-code screens

Sources read: upstream at ~/pluma-work/reference-src-new/tdesktop and the patch repo ~/pluma-work/wt-074-desktop (ci/patch.py, brand/strings.txt). Nothing was edited.

1. HOW THE FRAGMENT SCREEN IS BUILT ON DESKTOP
- Desktop has no sticker or Lottie on the code step. The Fragment screen is the ordinary CodeWidget step with three changes, all driven by `rpl::variable<bool> _isFragment` (intro_code.h ~78):
  - **Title** (intro_code.cpp:52-56): `lng_intro_fragment_title` "Enter code" instead of the formatted phone number.
  - **Description** (intro_code.cpp:94-99): `lng_intro_fragment_about` with `{phone_number}`, `tr::rich`. It uses the `introDescription` label, 300px wide, 20px lines, at contentTop+34.
  - **Bottom button**: `nextButtonText()` gives `lng_intro_fragment_button` (:422-429) and `nextButtonStyle()` gives `&st::introFragmentButton` (:431-436). That style is `RoundButton(introNextButton)` (300x42, radius 6) plus the icon "fragment" at `point(-10px, 9px)` (intro.style:88-94).
- Widget::setupStep (intro_widget.cpp:348-361) re-creates `_next` when the style changes. setupNextButton (:729-753) animates the button away when `nextButtonText()` is empty. That empty text is our clean way to hide the button.
- `submit()` (:365-371) opens the URL with `File::OpenUrl`. The code field still auto-submits a full code through `codeCollected`.
- Geometry (:158-165):
  - code input at contentTop+114, 50px high;
  - link and `_callLabel` share one row at about +188 (`introLinkTop`);
  - errors at +220 (`introErrorBelowLinkTop`);
  - the button at +266.
  So the description has room for about 4 lines (aim for 3), and there is a single link row under the field. There is no room for a second text row.
- There is no resend timer except the call countdown (`_callTimer`/`CallStatus`, :117-151, :300-337). The server must send no `next_type` (CallStatus::Disabled), or "Pluma will call you" appears.

2. INTENDED CHANGES

**Data** (intro_widget.h:65): add `bool codeUndelivered = false;`. In `Step::fillSentCodeData` (intro_step.cpp:363 reset; :368 Sms branch) set `getData()->codeUndelivered = true;`. This also covers the first `sendCode` path (intro_phone.cpp:244).

**Mode.** Replace the use of `_isFragment` with one variable:
```cpp
enum class Mode { Default, App, Undelivered, Fragment }; // pluma
rpl::variable<Mode> _mode;
bool _telegramAvailable = false; // pluma: a tg:// handler is installed
```

**Title** (:52-56):
```cpp
setTitleText(_mode.value() | rpl::map([=](Mode m) {
	switch (m) {
	case Mode::App: return tr::lng_pluma_code_app_title();
	case Mode::Undelivered: return tr::lng_pluma_code_failed_title();
	case Mode::Fragment: return tr::lng_intro_fragment_title();
	}
	return rpl::single(Ui::FormatPhone(getData()->phone));
}) | rpl::flatten_latest());
```

**updateDescText** (:83-115, `replace_body`):
- compute `_mode`;
- set `_telegramAvailable = Platform::File::IsUrlSchemeHandled(u"tg"_q)` on each call (it is cheap);
- descriptions:
  - App: `lng_pluma_code_app_about(lt_phone_number, …, tr::rich)`;
  - Undelivered: `lng_pluma_code_failed_about` when Telegram is available, otherwise `lng_pluma_code_failed_about_admin`;
- `_noTelegramCode` is shown in App mode with the text `lng_pluma_code_no_code` "Didn't get the code?" and in Undelivered mode with `lng_pluma_code_send_again`;
- the same visibility goes into `activate()` (:176-185) and `refreshLang()` (:71-76).

**Button:**
```cpp
rpl::producer<QString> CodeWidget::nextButtonText() const {
	return _mode.value() | rpl::map([=](Mode m) {
		return (m == Mode::Undelivered)
			? (_telegramAvailable ? tr::lng_pluma_code_failed_button() : rpl::single(QString()))
			: (m == Mode::Fragment) ? tr::lng_intro_fragment_button() : Step::nextButtonText();
	}) | rpl::flatten_latest();
}
// style: Undelivered -> &st::introPlumaTelegramButton, Fragment -> &st::introFragmentButton
void CodeWidget::submit() {
	if (_mode.current() == Mode::Undelivered && _telegramAvailable) {
		File::OpenUrl(u"tg://settings/phone_privacy"_q); // pluma: the real Telegram app
	} else if (_mode.current() == Mode::Fragment) {
		File::OpenUrl(getData()->codeByFragmentUrl);
	} else {
		_code->requestCode();
	}
}
```

**New style**, appended to intro.style:
```
introPlumaTelegramButton: RoundButton(introNextButton) {
	icon: icon{{ "settings/settings_phone_number", activeButtonFg }};
	iconOver: …
	iconPosition: point(-10px, 9px);
}
```
The icon is the existing Resources/icons/settings/settings_phone_number*.png. Its size needs checking on a screenshot.

**Telegram Desktop handles the link.** `tg://settings/phone_privacy` is an alias of `settings/privacy/phone-number` (deep_links_settings.cpp:2125-2128). Pluma registers only plm://, so tg:// stays with Telegram (patch.py SCHEME_FILES comment, :1401-1405). "tg" is in `IsUrlSchemeAllowed` (file_utilities.cpp:135-146). Unverified: whether Telegram for macOS (the Swift app) supports this path.

**Telegram detection.** Add `bool IsUrlSchemeHandled(const QString&)` to platform/platform_file_utilities.h (:19 block), with one implementation per platform:
- mac: `URLForApplicationToOpenURL`, the pattern at file_utilities_mac.mm:114;
- Windows: `AssocQueryStringW(ASSOCF_IS_PROTOCOL, ASSOCSTR_EXECUTABLE, L"tg", …)`;
- Linux: `Gio::AppInfo::get_default_for_uri_scheme`, the pattern at specific_linux.cpp:539.

**Resend:**
- In `noTelegramCodeDone`, delete :469 `getData()->codeByTelegram = false;`, because it overrides an App answer to a resend. Then:
```cpp
const auto was = _mode.current();
updateDescText();
if (was == Mode::Undelivered && _mode.current() == Mode::Undelivered) {
	showCodeError(tr::lng_pluma_code_still_failed());
}
```
- `noTelegramCodeFail` (:479-487) already shows `lng_flood_error` for FLOOD_WAIT, guarded by `_noTelegramCodeRequestId`. Add `PHONE_CODE_EXPIRED` → `goBack()`; today it shows the raw error type.
- Neither screen leads to support: tdesktop has no `reportMissingCode` and no sms@telegram.org on these screens (grep).

**Change phone:** not applicable. Desktop has no `account.sendChangePhoneCode` (grep). api/api_confirm_phone.cpp:48-50 treats an App type as an error and returns length 0, so the server must answer `account.sendConfirmPhoneCode` with Sms, not App.

3. STRINGS
- **English** is inserted by patch.py after the anchor `"lng_intro_fragment_button" = "Open Fragment";\n` (lang.strings:426), `tree.replace(..., count=1)`. The keys (wording to be tuned):
  - lng_pluma_code_app_title "Check Telegram"
  - lng_pluma_code_app_about "We sent the code for **{phone_number}** to your **Telegram**, in a message from the Pluma account."
  - lng_pluma_code_no_code "Didn't get the code?"
  - lng_pluma_code_failed_title "Code didn't reach Telegram"
  - lng_pluma_code_failed_about "Telegram couldn't deliver the code for **{phone_number}**. In Telegram, let **Everybody** find you by your number, then send again. No Telegram? Ask the administrator for the code."
  - lng_pluma_code_failed_about_admin (the same without the button sentence; says to change the setting in Telegram on the phone)
  - lng_pluma_code_failed_button "Open Telegram Settings"
  - lng_pluma_code_send_again "Send again"
  - lng_pluma_code_still_failed "Telegram still couldn't deliver the code."
- **Fit.** Each description must stay at 3-4 lines at 300px; check with screenshots.
- **Other languages** come from the server pack `tdesktop`. ADR 0025 says our own texts are not added by the import, and the import renames Telegram to Pluma. The server therefore needs an overlay of Pluma's own strings (ru and the other languages) that bypasses the rebrand, plus Leftovers exemptions. Without it, the cloud language shows the built-in English for the missing keys, because `Instance::reset` fills originals first (lang_instance.cpp:282-300).
- **Why new keys:** the existing `lng_code_from_telegram`, `lng_code_call` and the others are already renamed to "Pluma" (brand/strings.txt:38-41). So we cannot reuse them; new keys are the right choice.

4. PATCH.PY MECHANICS
- A new `login_code_screens(tree)` is called in `patch()` right after `rebrand(tree, c)` (:2599). The `lang_counts` check against brand/strings.txt (:2038-2047) then never sees the new keys, and the rename (:2052-2057) does not touch them.
- `LOGIN_CODE_STRINGS = {key: value}` is the single table used for both the insertion and the verify.
- Code edits:
  - `tree.replace(rel, old, new)` (count is exact);
  - for the whole bodies of `updateDescText`, `submit`, `nextButtonText/Style` and `noTelegramCodeDone`, `tree.replace_body(INTRO_CODE, "void CodeWidget::updateDescText() {\n", "\n}\n", body, must_contain=("lng_intro_fragment_about", "_callTimer.callEach(1000);"))`;
  - inserted code marked `// pluma:`.
- **Verify changes:**
  - `verify_brand` (:2194-2196): skip `LOGIN_CODE_STRINGS` keys like `LANG_KEEP`, and require each key once with its exact value, which pins the word "Telegram";
  - tg:// check (:2208-2210): `if tree.scan(rel).count("tg://") != TELEGRAM_APP_LINKS.get(rel, 0)` with `TELEGRAM_APP_LINKS = {INTRO_CODE: 1}`;
  - `code_telegram` (:2269) stays green: no "Telegram" in string literals, and comments are skipped;
  - add a verify that intro_code.cpp contains the `lng_pluma_code_failed_button` and `codeUndelivered` markers.
- **Tests:** ci/patch_test.py gets cases where the verify fails: a renamed key, an extra tg://, a lost key.

5. RISKS
- **Upstream drift:** intro_code.cpp changes often (email login, payment). Anchors fail loudly, which is the design.
- **The :469 override:** if it is not removed, a resend that is delivered still shows the Undelivered screen.
- **Server contract:**
  - Sms carries no `next_type` call and no `timeout` (no call countdown);
  - FLOOD_WAIT on repeated resends;
  - a resend that is delivered answers App, and the client returns to the App screen;
  - SEND_CODE_UNAVAILABLE is shown silently (:483), so the server should answer Sms instead.
- **Detection:** Flatpak or Snap sandboxes may not see the host's tg handler. The button is then hidden and the admin text shown (acceptable). On Linux, `xdg-open` reports success even without a handler, so detection must not rely on the open result.
- **Language packs:** without the server overlay, Russian users see English on these screens.

6. LOCAL TESTING
- `python3 ci/patch.py` on a fresh 7.4.1 checkout, plus `python3 -m unittest ci/patch_test.py`.
- Build on CachyOS (Linux) and the Mac. Run against a local stand (`test/e2e`) whose code sender can be told to report "undelivered".
- Scenarios:
  - App screen, then "Didn't get the code?", then the Undelivered screen;
  - "Send again" still failing gives the red line; delivered returns to the App screen;
  - several fast clicks give a flood error;
  - Telegram Desktop installed: the button opens Privacy > Phone Number. Not installed: on Linux remove the handler with `xdg-mime default` in an isolated HOME; the button hides and the admin text shows;
  - ru from the server pack;
  - screenshots at the minimum window height to check the text fits.
