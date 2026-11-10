Plan for web (tweb): the "Check Telegram" screen and the "didn't reach Telegram" fallback

Source trees: upstream `~/pluma-work/reference-src-new/tweb` at aa74832e (2.3/686, layer 233). Patch repo `~/pluma-work/wt-074-web` (ci/patch.py, ci/patch_test.py). I edited no files.

## How the code card is built today, and the Fragment variant
- `src/pages/cards/AuthCodeCard.tsx` has a single card for every digit-code type.
  - **Header:** `<MediaHeader>` (L625–637). It holds a sticker host of 100 px on mobile and 130 px otherwise, then the title, which is the **phone number** with a pencil icon to edit it. The subtitle, `sentTypeContent()`, says how the code was sent.
  - **Body:** the code field, then `AuthCardError`.
  - **Optional full-width filled button:** `Button class="btn-primary btn-color-primary" + styles.wordSubmit` (L648–654). Its style is `.wordSubmit {width:100%; margin-top:1rem}` in authFlow.module.scss:217. This is the native big button of the auth cards; it is used for word codes.
  - **Resend row:** `styles.cardLink`, a centred row (L656–658, scss:225).
- **Fragment variant:**
  - `rebuildAnimation()` (L330–352) replaces the tracking monkey with a looping lottie (`loadAnimationAsAsset(..., 'jolly_roger')`).
  - In `applySentCode()` (L568–575) the subtitle is `PhoneNumber.Code.Fragment.Info` with an inline `<a href=type.url>`.
  - There is no button. The title is still the phone number.
- **Resend:**
  - `updateResend()` (L391–412) shows a countdown with `getResendPendingLangKey(next_type)` ("You can request an SMS in 0:45", updated every 1 s through `ctx.setTimeout`). After it, `getResendLangKey(next_type)` shows a link (`anchorCallback(resendCode)`).
  - `resendCode()` (L414–440) shows `Login.Code.Resending`, calls `auth.resendCode` and passes the result to `continueLogin`.
  - `applyNewSentCode` (L367–377) applies a new `sentCode` in place. So Sms→App and App→Sms swap the subtitle, sticker and resend row with no navigation.
  - Errors only show `toastNew('Error.AnError')` (L438).
- Helpers in `src/pages/sentCode.ts`: `getResendLangKey` L107, `getResendPendingLangKey` L123, `getResendTimeout` L137.
- tweb has no `auth.reportMissingCode`, no sms@telegram.org and no change-phone flow (I grepped src). So nothing has to be removed, and point 3 of the task does not apply to web.

## Target UI
- **App screen (`auth.sentCodeTypeApp`):**
  - The sticker is the tracking monkey, as now.
  - The title is the phone number. This is the native web pattern, and the phone is not repeated in the text.
  - Subtitle `Pluma.Code.SentTelegram`: `'**Check Telegram**\nWe sent the code to your Telegram, in a message from the Pluma account.'`
  - Resend row: pending `Pluma.Code.NotReceivedIn` = `'Didn\'t get the code? Ask again in %s'`; when ready, `Pluma.Code.NotReceived` = `'[Didn\'t get the code?]()'`, which calls `resendCode`.
- **Fallback (`auth.sentCodeTypeSms`), the Fragment-like layout:**
  - **Sticker:** the looping `UtyanSearch` lottie. It is in `LottieAssetName` (lottieLoader.ts:51) and already in public/assets/tgs. It replaces the monkey, the same way the Fragment branch does. Choosing this sticker is a design decision for the owner to confirm.
  - **Subtitle `Pluma.Code.NotDelivered`:**
    `'**Code didn\'t reach Telegram**\nIn Telegram, open **Settings › Privacy and Security › Phone Number** and set **Who can find me by my number** to **Everybody**, then send the code again.\n\nNo Telegram? Ask the administrator for the code and enter it here.'`
  - **Big filled button** (the same classes as `wordSubmit`), `Pluma.Code.OpenTelegramSettings` = `'Open Telegram Settings'`. It opens `tg://settings/phone_privacy`. The link is confirmed in all three apps:
    - Android LaunchActivity.java:2654;
    - iOS OpenUrl.swift:1025;
    - Desktop deep_links_settings.cpp:2127, an alias to `privacy/phone-number`.
  - **Link row:** `Pluma.Code.SendAgainIn` = `'Send again in %s'` while waiting, then `Pluma.Code.SendAgain` = `'[Send again]()'`.
  - The server keeps `next_type=codeTypeSms` on both screens, so the resend path always exists.

## Code changes (AuthCodeCard.tsx and sentCode.ts)
```ts
// sentCode.ts, appended
// pluma: codes come from a real Telegram account; Sms means Telegram did not deliver it.
export function getPlumaResendKeys(type: AuthSentCodeType): [LangPackKey, LangPackKey] | undefined {
  switch(type._) {
    case 'auth.sentCodeTypeApp': return ['Pluma.Code.NotReceived', 'Pluma.Code.NotReceivedIn'];
    case 'auth.sentCodeTypeSms': return ['Pluma.Code.SendAgain', 'Pluma.Code.SendAgainIn'];
  }
}
```
In `updateResend` (use `replace_within` between `function updateResend() {` and `function resendCode`):
```ts
const pluma = sentCode.next_type && getPlumaResendKeys(sentCode.type); // pluma
const key = pluma?.[0] ?? getResendLangKey(sentCode.next_type);
... i18n(pluma?.[1] ?? getResendPendingLangKey(sentCode.next_type), [toHHMMSS(diff)])
```
In `applySentCode`:
- Split `case 'auth.sentCodeTypeSms':` (L541) away from FirebaseSms: `key = 'Pluma.Code.NotDelivered'`.
- `case 'auth.sentCodeTypeApp'` (L547): `key = 'Pluma.Code.SentTelegram'`.

In `rebuildAnimation` (L335): `const asset = type === Fragment ? 'jolly_roger' : type === 'auth.sentCodeTypeSms' ? 'UtyanSearch' : undefined; if(asset) {...}`.

The button: `const [canOpenTelegram, setCanOpenTelegram] = createSignal(true);`
```tsx
<Show when={sentCode.type._ === 'auth.sentCodeTypeSms' && canOpenTelegram()}>  {/* via a signal set in applySentCode */}
  <Button class={classNames('btn-primary btn-color-primary', styles.wordSubmit)}
    text="Pluma.Code.OpenTelegramSettings" onClick={openTelegramSettings}/>
</Show>
```
```ts
// pluma: the real Telegram app, not Pluma (plm://): the privacy setting the code's delivery depends on.
const TELEGRAM_PHONE_PRIVACY = 'tg://settings/phone_privacy';
function openTelegramSettings() {
  let left = false;
  const onLeave = () => left = true;
  window.addEventListener('blur', onLeave, {once: true});
  document.addEventListener('visibilitychange', onLeave, {once: true});
  const a = document.createElement('a'); a.href = TELEGRAM_PHONE_PRIVACY; a.click();
  ctx.setTimeout(() => {
    window.removeEventListener('blur', onLeave);
    document.removeEventListener('visibilitychange', onLeave);
    if(!left && !isGone()) setCanOpenTelegram(false); // no app took the link: the text still says where to go
  }, 2000);
}
```
- A browser cannot tell whether an app is installed. So the button stays visible and hides itself after one click that leaves the page untouched. The timeout and the use of blur/visibilitychange are not verified in browsers.
- Use a raw anchor, not `safeWindowOpen`, because `normalizeUrlProtocol` filters schemes. Once the patch is applied, tweb's anchor router only claims `plm:`.

Floods in `resendCode().catch` (L437):
- Reuse the L286 pattern: `const fw = getFloodWaitTime(err); if(fw) { sentCode.resend_deadline = tsNow(true) + fw.waitTime; updateResend(); return; }`.
- For `PHONE_CODE_EXPIRED`, navigate back to signIn.
- Otherwise, toast as now.

## patch.py mechanics
- Add `login_code_screens(tree)`, called **after `rebrand(tree, c)`** in `patch()` (~L1785). `rename_strings` (L734) runs inside rebrand, so the counts in `STRINGS` (`langSign.ts`, 7 renamed / 0 left) stay unchanged and our Telegram is not renamed to Pluma.
- **Strings:** `tree.replace("src/langSign.ts", "  'Login.Code.Resending': 'Requesting a new code...',\n", same + PLUMA_CODE_STRINGS)`, with count 1. The keys go into langSign.ts so that `LangPackKey` types them (langPack.ts:72).
- **Code edits:**
  - `tree.replace` / `replace_within` with exact anchors (`case 'auth.sentCodeTypeSms':\n      // the web client cannot attest…`, the updateResend body);
  - `must_contain` guards on `getResendLangKey(sentCode.next_type)` and `'jolly_roger'`;
  - `tree.expect` on `getFloodWaitTime(err)` and on `'UtyanSearch'` in lottieLoader.ts.
- **Verify exceptions:**
  - `TELEGRAM_KEEP_KEYS = {'Pluma.Code.SentTelegram', 'Pluma.Code.NotDelivered', 'Pluma.Code.OpenTelegramSettings'}`. In `verify` (L1838–1842), take the key from `m.group(1)` and skip these keys. Also require every key of `PLUMA_CODE_STRINGS` to be present exactly once.
  - `TELEGRAM_APP_LINKS = (("src/pages/cards/AuthCodeCard.tsx", "'tg://settings/phone_privacy'"),)`. Before the `_TG_LINK` scan (L1846–1849), require exactly one occurrence of each and remove it from the comment-stripped text. Any other `tg:` still fails.
  - `telegram_traces` is unaffected: there is no telegram.org in our strings.
- **patch_test.py:**
  - add the anchors to the fake tree (~L394);
  - add a damage test: a second `tg://` literal and a non-whitelisted Telegram value must both fail;
  - assert the English strings after `test_patch` (L672).

## Other languages
- English is bundled. Other languages come from the server langpack `web`/`webk`. `getLangPackAndApply` (langPack.ts:283–293) pushes local strings first and lets server strings override them, so a missing key falls back to English.
- **The server has no layer of Pluma's own strings.** `pkg/langpack` only imports Telegram packs and rebrands them (rebrand.go `keepKeys`). Translations of the `Pluma.Code.*` keys for all imported languages need a server-side "own strings" overlay that is not rebranded. This is a separate server task and a blocker for the "all languages" requirement. Without it, non-English users see English.

## Risks
- **Upstream drift:**
  - `AuthCodeCard` was recently refactored, so all anchors are exact-count edits with `must_contain`;
  - `getResendLangKey` has a signature that may change;
  - the `LottieAssetName` union may change.
- **State machine:**
  - App↔Sms swaps happen in place through `applyNewSentCode`. The button signal and the sticker must follow `applySentCode()`, and the button must hide on App.
  - `resend_deadline` is persisted in authState and survives a reload, so after a flood the deadline must be persisted with it (call `persistSentCode()`).
  - A `resending` guard already exists.
- **Server semantics:** App now means "real Telegram". If the server ever delivers codes to the user's own Pluma session, it needs a different type. This must be decided for all four clients together.
- **Browsers (unverified):**
  - iOS Safari shows "address is invalid" for an unknown scheme;
  - Firefox may show a dialog;
  - on Android Chrome the hide heuristic may misfire.

## Local testing
1. Start the server stand with the dev key and WS on 11443 (`docs/ops/clients.md`). Make the code-sender return Sms, or stub the sender in the stand.
2. In wt-074-web run `ci/configure.sh && ci/build.sh dev`, then `python3 -m http.server --bind 127.0.0.1 --directory out/pluma-web-2.3-686-dev 8080`.
3. Check in Chrome:
   - App screen text and countdown;
   - App→Sms switch in place;
   - "Send again" leading back to App;
   - the button with and without Telegram Desktop installed;
   - mobile width (DevTools 375 px);
   - dark theme;
   - a flood path, via a server limit.
4. Extend `e2e/run.sh` with screenshots of both screens. Run `python3 -m unittest ci/patch_test.py`.
