//go:build e2e

package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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

func webViewResult(t *testing.T, o mtproto.TLObject, err error, method string) *mtproto.WebViewResult {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return o.(interface {
		To_WebViewResult() *mtproto.WebViewResult
	}).To_WebViewResult()
}

// checkInitData reads the launch parameters of a web view's address and
// checks its initData as core.telegram.org/bots/webapps tells a bot and a
// third party to: hash = hex(HMAC_SHA256(data_check_string,
// HMAC_SHA256("WebAppData", token))) over every field but hash, and
// signature, base64url Ed25519 under the server's key over
// "<bot_id>:WebAppData\n" and every field but hash and signature.
func checkInitData(t *testing.T, link, appURL, token string, botId int64, publicKey ed25519.PublicKey) (url.Values, url.Values) {
	t.Helper()
	base, frag, ok := strings.Cut(link, "#")
	if !ok || base != appURL {
		t.Fatalf("address %q, want %s with a fragment", link, appURL)
	}
	params, err := url.ParseQuery(frag)
	if err != nil {
		t.Fatalf("fragment %q: %v", frag, err)
	}
	data, err := url.ParseQuery(params.Get("tgWebAppData"))
	if err != nil {
		t.Fatalf("tgWebAppData %q: %v", params.Get("tgWebAppData"), err)
	}
	checkString := func(skip ...string) string {
		var lines []string
	next:
		for k := range data {
			for _, s := range skip {
				if k == s {
					continue next
				}
			}
			lines = append(lines, k+"="+data.Get(k))
		}
		sort.Strings(lines)
		return strings.Join(lines, "\n")
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(checkString("hash")))
	if want := hex.EncodeToString(mac.Sum(nil)); data.Get("hash") != want {
		t.Fatalf("hash %q, want %q", data.Get("hash"), want)
	}
	sig, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(data.Get("signature"), "="))
	if err != nil || !ed25519.Verify(publicKey, []byte(fmt.Sprintf("%d:WebAppData\n", botId)+checkString("hash", "signature")), sig) {
		t.Fatalf("signature %q does not verify: %v", data.Get("signature"), err)
	}
	return params, data
}

// TestWebViewInitData: the menu button's mini app of a bot opens with
// initData signed with the bot's token and the server's Ed25519 key, and a
// query_id the client prolongs; a query of nobody's is QUERY_ID_INVALID.
// The bot's main app opens full size from the chat with the bot. A bot the
// user never wrote to may not write; the user lets it, the chat gets
// messageActionBotAllowed(from_request), and then it may. (task 106, ADR
// 0068 p. 4.)
func TestWebViewInitData(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot, token := botLoggedInWithToken(ctx, t, phone, username, 228)
	getState(ctx, t, bot)
	keyHex := strings.TrimSpace(admin(t, "bot", "webapp-key"))
	publicKey, err := hex.DecodeString(keyHex)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		t.Fatalf("the server's web app key %q: %v", keyHex, err)
	}
	toBot := resolveBot(ctx, t, alice, username)
	botUser := inputUserOfPeer(alice.c, toBot)
	const menuURL = "https://shop.example/app?x=1"

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesRequestWebView{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestWebView),
		FromBotMenu: true, Peer: toBot, Bot: botUser, Url: wrapperspb.String(menuURL),
		ThemeParams: &mtproto.DataJSON{Constructor: alice.c.Constructor(mtproto.Predicate_dataJSON), Data: `{"bg_color":"#FFFFFF","text_color":-16777216}`},
		Platform:    "tdesktop",
		StartParam:  wrapperspb.String("promo"),
	})
	r := webViewResult(t, o, err, "messages.requestWebView")
	params, data := checkInitData(t, r.GetUrl(), menuURL, token, bot.self.GetId(), publicKey)
	if params.Get("tgWebAppPlatform") != "tdesktop" || params.Get("tgWebAppVersion") == "" || params.Get("tgWebAppStartParam") != "promo" ||
		params.Get("tgWebAppThemeParams") != `{"bg_color":"#ffffff","text_color":"#000000"}` {
		t.Errorf("launch parameters %v", params)
	}
	queryId := r.GetQueryId_FLAGINT64().GetValue()
	if queryId == 0 || data.Get("query_id") != fmt.Sprint(queryId) || data.Get("start_param") != "promo" ||
		!strings.Contains(data.Get("user"), fmt.Sprintf(`"id":%d`, alice.self.GetId())) || !strings.Contains(data.Get("user"), `"first_name":"Alice"`) {
		t.Errorf("initData %v, query %d", data, queryId)
	}

	prolong := func(q int64) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLMessagesProlongWebView{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_prolongWebView),
			Peer:        toBot, Bot: botUser, QueryId: q,
		})
		return err
	}
	if err = prolong(queryId); err != nil {
		t.Fatalf("messages.prolongWebView: %v", err)
	}
	if err = prolong(queryId ^ 1); !strings.Contains(fmt.Sprint(err), "QUERY_ID_INVALID") {
		t.Errorf("a query of nobody's: %v", err)
	}

	// The main app.
	admin(t, "bot", "set", username, "-main-app", "https://shop.example/main")
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesRequestMainWebView{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestMainWebView),
		Peer:        toBot, Bot: botUser, Platform: "android",
	})
	r = webViewResult(t, o, err, "messages.requestMainWebView")
	if _, data = checkInitData(t, r.GetUrl(), "https://shop.example/main", token, bot.self.GetId(), publicKey); !r.GetFullsize() || data.Get("chat_type") != "sender" || data.Get("chat_instance") == "" {
		t.Errorf("the main app: %v, %v", r, data)
	}

	canSend := func() bool {
		o, err := alice.c.Invoke(ctx, &mtproto.TLBotsCanSendMessage{Constructor: alice.c.Constructor(mtproto.Predicate_bots_canSendMessage), Bot: botUser})
		if err != nil {
			t.Fatalf("bots.canSendMessage: %v", err)
		}
		return mtproto.FromBool(o.(interface{ To_Bool() *mtproto.Bool }).To_Bool())
	}
	if canSend() {
		t.Fatal("a bot the user never wrote to may write")
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLBotsAllowSendMessage{Constructor: alice.c.Constructor(mtproto.Predicate_bots_allowSendMessage), Bot: botUser}); err != nil {
		t.Fatalf("bots.allowSendMessage: %v", err)
	}
	allowed := func(u *mtproto.Update) bool {
		a := u.GetMessage_MESSAGE().GetAction()
		return a.GetPredicateName() == mtproto.Predicate_messageActionBotAllowed && a.GetFromRequest()
	}
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateNewMessage, allowed)
	if !canSend() {
		t.Error("the bot may not write after the user let it")
	}
}
