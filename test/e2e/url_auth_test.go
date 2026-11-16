//go:build e2e

package e2e

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// acceptedURL is the address of urlAuthResultAccepted in either layer's
// form.
func acceptedURL(r *mtproto.UrlAuthResult) string {
	if v := r.GetUrl_FLAGSTRING(); v != nil {
		return v.GetValue()
	}
	return r.GetUrl_STRING()
}

func urlAuthResult(t *testing.T, o mtproto.TLObject, err error, method string) *mtproto.UrlAuthResult {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return o.(interface {
		To_UrlAuthResult() *mtproto.UrlAuthResult
	}).To_UrlAuthResult()
}

// checkLoginData checks the data a login gives the site as the site would
// (core.telegram.org/widgets/login-legacy, "Checking authorization"):
// hash = hex(HMAC_SHA256(data_check_string, SHA256(token))).
func checkLoginData(t *testing.T, link, token string, userId int64) {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("address %q: %v", link, err)
	}
	q := u.Query()
	if q.Get("next") != "/home" {
		t.Errorf("the site's own field is lost: %v", q)
	}
	q.Del("next")
	var lines []string
	for k := range q {
		if k != "hash" {
			lines = append(lines, k+"="+q.Get(k))
		}
	}
	sort.Strings(lines)
	key := sha256.Sum256([]byte(token))
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(strings.Join(lines, "\n")))
	if want := hex.EncodeToString(mac.Sum(nil)); q.Get("hash") != want {
		t.Fatalf("hash %q, want %q of %q", q.Get("hash"), want, lines)
	}
	if q.Get("id") != fmt.Sprint(userId) || q.Get("first_name") != "Alice" || q.Get("auth_date") == "" {
		t.Errorf("data %v", q)
	}
}

// TestUrlAuthButton: a bot with a login domain sends a login button
// (task 106, ADR 0068 p. 6). The press asks the user; the user accepts with
// the permission to be written to and gets the address with the data, signed
// with the bot's token; the chat with the bot gets messageActionBotAllowed
// with the domain; the next press logs in at once; the site is listed among
// the sites logged in on, and after it is reset the press asks again.
func TestUrlAuthButton(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	domain := fmt.Sprintf("s%d.example", randomID()&0xffffff)
	bot, token := botLoggedInWithToken(ctx, t, phone, username, 228)
	admin(t, "bot", "set", username, "-domain", domain)
	getState(ctx, t, bot)
	toBot := resolveBot(ctx, t, alice, username)
	toAlice := &mtproto.InputPeer{Constructor: bot.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: alice.self.GetId(), AccessHash: alice.self.GetAccessHash().GetValue()}

	loginButton := func(link string, write bool) *mtproto.KeyboardButton {
		return mtproto.MakeTLInputKeyboardButtonUrlAuth(&mtproto.KeyboardButton{
			Text: "Log in", Url: link, RequestWriteAccess: write,
			Bot: &mtproto.InputUser{Constructor: bot.c.Constructor(mtproto.Predicate_inputUserSelf)},
		}).To_KeyboardButton()
	}
	send := func(text string, b *mtproto.KeyboardButton) error {
		_, err := bot.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor: bot.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        toAlice, Message: text, RandomId: randomID(),
			ReplyMarkup: mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
				Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{b}}).To_KeyboardButtonRow()},
			}).To_ReplyMarkup(),
		})
		return err
	}
	// A link to another domain is refused (BUTTON_TYPE_INVALID before the
	// login buttons were served).
	if err := send("elsewhere", loginButton("https://elsewhere.example/login", false)); !strings.Contains(fmt.Sprint(err), "BOT_DOMAIN_INVALID") {
		t.Fatalf("a login button to another domain: %v", err)
	}
	if err := send("log in", loginButton("https://"+domain+"/login?next=%2Fhome", true)); err != nil {
		t.Fatalf("messages.sendMessage with a login button: %v", err)
	}
	var (
		msgId    int32
		buttonId int32
		link     string
	)
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		if m.GetMessage() != "log in" {
			return false
		}
		b := m.GetReplyMarkup().GetRows()[0].GetButtons()[0]
		if b.GetPredicateName() != mtproto.Predicate_keyboardButtonUrlAuth || b.GetUserId_INT64() != 0 || b.GetRequestWriteAccess() {
			t.Fatalf("the button as the user gets it: %v", b)
		}
		msgId, buttonId, link = m.GetId(), b.GetButtonId(), b.GetUrl()
		return true
	})
	if link != "https://"+domain+"/login?next=%2Fhome" {
		t.Fatalf("the button's link: %q", link)
	}

	request := func() *mtproto.UrlAuthResult {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesRequestUrlAuth{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestUrlAuth),
			Peer:        toBot, MsgId: wrapperspb.Int32(msgId), ButtonId: wrapperspb.Int32(buttonId),
		})
		return urlAuthResult(t, o, err, "messages.requestUrlAuth")
	}
	r := request()
	if r.GetPredicateName() != mtproto.Predicate_urlAuthResultRequest || !r.GetRequestWriteAccess() || r.GetDomain() != domain || r.GetBot().GetId() != bot.self.GetId() {
		t.Fatalf("the first press: %v", r)
	}

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesAcceptUrlAuth{
		Constructor:  alice.c.Constructor(mtproto.Predicate_messages_acceptUrlAuth),
		WriteAllowed: true,
		Peer:         toBot, MsgId: wrapperspb.Int32(msgId), ButtonId: wrapperspb.Int32(buttonId),
	})
	r = urlAuthResult(t, o, err, "messages.acceptUrlAuth")
	if r.GetPredicateName() != mtproto.Predicate_urlAuthResultAccepted {
		t.Fatalf("accepted: %v", r)
	}
	checkLoginData(t, acceptedURL(r), token, alice.self.GetId())
	allowed := func(u *mtproto.Update) bool {
		a := u.GetMessage_MESSAGE().GetAction()
		if a.GetPredicateName() != mtproto.Predicate_messageActionBotAllowed {
			return false
		}
		if d := a.GetDomain_FLAGSTRING(); d != nil {
			return d.GetValue() == domain
		}
		return a.GetDomain_STRING() == domain
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, allowed)
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateNewMessage, allowed)

	// Logged in, nothing new to ask: in at once.
	r = request()
	if r.GetPredicateName() != mtproto.Predicate_urlAuthResultAccepted {
		t.Fatalf("the second press: %v", r)
	}
	checkLoginData(t, acceptedURL(r), token, alice.self.GetId())

	o, err = alice.c.Invoke(ctx, &mtproto.TLAccountGetWebAuthorizations{Constructor: alice.c.Constructor(mtproto.Predicate_account_getWebAuthorizations)})
	if err != nil {
		t.Fatalf("account.getWebAuthorizations: %v", err)
	}
	sites := o.(interface {
		To_Account_WebAuthorizations() *mtproto.Account_WebAuthorizations
	}).To_Account_WebAuthorizations()
	if len(sites.GetAuthorizations()) != 1 || sites.GetAuthorizations()[0].GetDomain() != domain || sites.GetAuthorizations()[0].GetBotId() != bot.self.GetId() ||
		len(sites.GetUsers()) != 1 || sites.GetUsers()[0].GetId() != bot.self.GetId() {
		t.Fatalf("the sites logged in on: %v", sites)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLAccountResetWebAuthorization{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_resetWebAuthorization),
		Hash:        sites.GetAuthorizations()[0].GetHash(),
	}); err != nil {
		t.Fatalf("account.resetWebAuthorization: %v", err)
	}
	if r = request(); r.GetPredicateName() != mtproto.Predicate_urlAuthResultRequest || r.GetRequestWriteAccess() {
		t.Fatalf("after the reset, with the permission given: %v", r)
	}
}
