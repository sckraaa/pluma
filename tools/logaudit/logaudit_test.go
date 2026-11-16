package logaudit

import (
	"go/token"
	"reflect"
	"sort"
	"testing"
)

// skipped are debug programs that are never built into a service.
var skipped = []string{
	// prints a hard-coded PROXY protocol header
	"app/interface/gnetway/internal/server/gnet/pp/c2",
	// decrypts captured packets with hard-coded upstream test keys
	"app/interface/gnetway/internal/server/gnet/codec/checker",
}

// TestRepository keeps the code base free of the logging this package finds.
// To fix a finding, log ids, lengths and types (%T) instead; if it is not a
// leak, mark the call with a "// logaudit:ok <reason>" comment.
func TestRepository(t *testing.T) {
	findings, err := Check("../..", []string{"app", "pkg", "tools", "third_party/teamgram-proto/mtproto"}, skipped)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

func TestRules(t *testing.T) {
	const header = `package p

import (
	"encoding/hex"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/mtserver/handshake"
	"github.com/zeromicro/go-zero/core/logx"
)

type HandshakeStateCtx struct{ NewNonce, A []byte }
`
	cases := []struct {
		name string
		body string
		want []string
	}{
		// the bot platform (task 106)
		{"init data", `func f(initData string) { logx.Infof("open %s", initData) }`, []string{"secret"}},
		{"web app key", `func f(webAppKey []byte) error { return fmt.Errorf("key %x", webAppKey) }`, []string{"secret"}},
		{"match code", `func f(in *mtproto.TLMessagesAcceptUrlAuth) { logx.Infof("code %s", in.GetMatchCode()) }`, []string{"secret"}},
		{"client secret", `func f(clientSecret string) { logx.Errorf("client %q", clientSecret) }`, []string{"secret"}},
		{"bot id ok", `func f(botId int64) { logx.Infof("bot %d", botId) }`, nil},
		// the translation of messages (task 101)
		{"translation", `func f(translation, sourceText string) { logx.Infof("%s from %s", translation, sourceText) }`, []string{"secret", "secret"}},
		{"model prompt", `func f(req struct{ Prompt string }) error { return fmt.Errorf("prompt %q failed", req.Prompt) }`, []string{"secret"}},
		{"model answer", `func f(content, completion string) { logx.Errorf("answer %s / %s", content, completion) }`, []string{"secret", "secret"}},
		{"compose result", `func f(r *mtproto.Messages_ComposedMessageWithAI) { logx.Infof("%v", r.GetResultText()) }`, []string{"secret"}},
		{"translated", `func f(translated []string) { logx.Infof("%v", translated) }`, []string{"secret"}},
		{"translation length ok", `func f(translation string, toLang string) { logx.Infof("%d characters to %s", len(translation), toLang) }`, nil},
		// audit #8: handshake secrets
		{"handshake state", `func f(ctx *HandshakeStateCtx) { logx.Infof("state: %s", ctx) }`, []string{"object"}},
		{"new_nonce", `func f(ctx *HandshakeStateCtx) { logx.Infof("n: %x", ctx.NewNonce) }`, []string{"secret"}},
		{"dh a", `func f(ctx *HandshakeStateCtx) { logx.Debugf("a: %v", ctx.A) }`, []string{"secret"}},
		{"shared handshake key", `func f(k *handshake.DHGenKey) { logx.Infof("key: %v", k) }`, []string{"object"}},
		{"shared handshake result", `func f(r *ReqDHParamsResult) { logx.Infof("res: %+v", r) }`, []string{"object"}},
		{"g_b", `func f(in *mtproto.Client_DHInnerData) { logx.Infof("g_b: %s", in.GetGB()) }`, []string{"secret"}},
		{"inner data", `func f(in *mtproto.TLClient_DHInnerData) { logx.Infof("inner: %#v", in.String()) }`, []string{"object"}},
		// audit #9: key material in errors and packet dumps
		{"aes key in error", `func f(aesKey, aesIV []byte) error { return fmt.Errorf("bad: %s %s", hex.EncodeToString(aesKey), aesIV) }`, []string{"dump", "secret", "secret"}},
		{"auth key slice", `func f(authKey []byte) error { return fmt.Errorf("k: %x", authKey[88:120]) }`, []string{"secret"}},
		{"packet dump", `func f(mmsg []byte, err error) { logx.Errorf("decrypt: %v, payload: %s", err, hex.EncodeToString(mmsg)) }`, []string{"dump"}},
		// audit #25: requests and content
		{"request", `func f(in *mtproto.TLMessagesSendMessage) { logx.Debugf("request: %s", in) }`, []string{"body", "object"}},
		{"request field ok", `func f(in *mtproto.TLMessagesSendMessage) { logx.Debugf("random_id: %d", in.RandomId) }`, nil},
		{"request type ok", `func f(in *mtproto.TLMessagesSendMessage) { logx.Debugf("request: %T", in) }`, nil},
		{"app request", `func f(in *msg.TLMsgSendMessageV2) { logx.Errorf("bad %v", in) }`, []string{"object"}},
		{"debug string", `func f(m interface{ DebugString() string }) { logx.Infof("%s", m.DebugString()) }`, []string{"object"}},
		{"made object", `func f() { u := mtproto.MakeTLUpdateShort(nil).To_Updates(); logx.Infof("u: %v", u) }`, []string{"object"}},
		{"closure", `func f(in *mtproto.TLMessagesSendMessage) { go func() { logx.Info(in) }() }`, []string{"object"}},
		{"reply local", `func f(c interface{ Do() (any, error) }) { r, _ := c.Do(); logx.Debugf("x - reply: {%s}", r) }`, []string{"body"}},
		{"slice of messages", `func f(msgs []*mtproto.TLMessage2) { logx.Infof("%#v", msgs) }`, []string{"object"}},
		{"login code", `func f(phoneCode, phoneCodeHash string) { logx.Infof("code %s hash %s", phoneCode, phoneCodeHash) }`, []string{"secret", "secret"}},
		{"code getter", `func f(in *mtproto.TLAuthSignIn) { logx.Infof("%s", in.GetPhoneCode_STRING()) }`, []string{"secret"}},
		{"shown login codes", `func f(in *mtproto.TLAccountInvalidateSignInCodes, candidates []string) { logx.Infof("%v %v", in.GetCodes(), candidates) }`, []string{"secret", "secret"}},
		{"shown login codes count ok", `func f(in *mtproto.TLAccountInvalidateSignInCodes, n int) { logx.Infof("%d of %d", n, len(in.GetCodes())) }`, nil},
		{"srp proof", `func f(in *mtproto.InputCheckPasswordSRP) { logx.Infof("A %x M1 %x", in.GetA(), in.M1) }`, []string{"secret", "secret"}},
		{"srp values", `func f(p *mtproto.Account_Password, s *mtproto.Account_PasswordInputSettings) { logx.Infof("%x %x", p.GetSrp_B(), s.GetNewPasswordHash()) }`, []string{"secret", "secret"}},
		{"srp id ok", `func f(in *mtproto.InputCheckPasswordSRP) { logx.Infof("srp_id %d", in.GetSrpId()) }`, nil},
		{"token", `func f(in *mtproto.TLInvokeWithReCaptcha) { logx.Infof("%s", in.Token) }`, []string{"secret"}},
		{"recovery email", `func f(p struct{ Email, EmailUnconfirmed string }) { logx.Infof("%s %s", p.Email, p.EmailUnconfirmed) }`, []string{"secret", "secret"}},
		{"email getter", `func f(s *mtproto.Account_PasswordSettings) { logx.Infof("%s", s.GetEmail()) }`, []string{"secret"}},
		{"email pattern", `func f(p *mtproto.Account_Password) error { return fmt.Errorf("pattern %s", p.GetEmailUnconfirmedPattern()) }`, []string{"secret"}},
		{"mail api key", `func f(c struct{ Mail struct{ ApiKey, From string } }) { logx.Errorf("key %s, mail %+v", c.Mail.ApiKey, c.Mail) }`, []string{"secret", "secret"}},
		{"email flag ok", `func f(confirmed bool) { logx.Infof("confirmed %v", confirmed) }`, nil},
		{"ice credentials", `func f(r struct{ Ufrag, Pwd string }) { logx.Infof("%s %s", r.Ufrag, r.Pwd) }`, []string{"secret", "secret"}},
		{"join params", `func f(params []byte) error { return fmt.Errorf("join %s", params) }`, []string{"secret"}},
		{"dtls fingerprints", `func f(r struct{ Fingerprints []string }) { logx.Infof("%v", r.Fingerprints) }`, []string{"secret"}},
		{"chain block", `func f(in *mtproto.TLPhoneJoinGroupCall) { logx.Infof("join %x key %x", in.GetBlock(), in.GetPublicKey()) }`, []string{"secret", "secret"}},
		{"chain blocks dump", `func f(blocks [][]byte) error { return fmt.Errorf("blocks %v", blocks) }`, []string{"secret"}},
		{"status report", `func f(report []byte) { logx.Infof("report %s", report) }`, []string{"secret"}},
		{"chain height ok", `func f(height int32, callId int64) { logx.Infof("call %d at height %d", callId, height) }`, nil},
		{"config with credentials", `func f(c struct{ Etcd struct{ Hosts []string; Pass string } }) { logx.Errorf("etcd %+v", c.Etcd) }`, []string{"secret"}},
		{"config hosts ok", `func f(c struct{ Etcd struct{ Hosts []string; Pass string } }) { logx.Errorf("etcd %v", c.Etcd.Hosts) }`, nil},
		{"geo point", `func f(in *mtproto.InputGeoPoint) { logx.Infof("at %f %f", in.GetLat(), in.Long) }`, []string{"secret", "secret"}},
		{"geo point whole", `func f(in *mtproto.InputWebFileLocation) error { return fmt.Errorf("map %v", in.GetGeoPoint()) }`, []string{"secret"}},
		{"inline query text", `func f(inlineQuery string, webFileUrl string) { logx.Infof("asked %s for %s", inlineQuery, webFileUrl) }`, []string{"secret", "secret"}},
		{"inline query point", `func f(in *mtproto.Update) { logx.Errorf("near %v", in.GetGeo()) }`, []string{"secret"}},
		{"business address", `func f(in *mtproto.TLAccountUpdateBusinessLocation, address string) { logx.Infof("%v %s", in.GetAddress(), address) }`, []string{"secret", "secret"}},
		{"chat link slug", `func f(in *mtproto.TLAccountResolveBusinessChatLink, slug string) { logx.Errorf("%s %s", in.GetSlug(), slug) }`, []string{"secret", "secret"}},
		{"geo zoom ok", `func f(in *mtproto.InputWebFileLocation) { logx.Infof("zoom %d, %dx%d", in.GetZoom(), in.GetW(), in.GetH()) }`, nil},
		// not findings
		{"error", `func f(err error) { logx.Errorf("request: %v", err) }`, nil},
		{"len", `func f(b []byte) { logx.Errorf("payload: %d bytes", len(b)) }`, nil},
		{"ids", `func f(in *mtproto.TLMessagesSendMessage, keyId int64) { logx.Infof("auth_key_id: %d, peer: %v", keyId, in.Peer) }`, nil},
		{"error code", `func f(e *mtproto.TLRpcError) { logx.Infof("code: %d", e.Code) }`, nil},
		{"fmt to stdout", `func f(code string) { fmt.Println(code) }`, nil},
		{"marked", "func f(in *mtproto.TLPing) {\n\t// logaudit:ok ping carries only ping_id\n\tlogx.Infof(\"%v\", in)\n}", nil},
		{"mark needs reason", "func f(in *mtproto.TLPing) {\n\tlogx.Infof(\"%v\", in) // logaudit:ok\n}", []string{"object"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs, err := CheckFile(token.NewFileSet(), "p.go", []byte(header+tc.body+"\n"))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range fs {
				got = append(got, f.Rule)
			}
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("rules = %v, want %v; findings: %v", got, want, fs)
			}
		})
	}
}

func TestFormatVerbs(t *testing.T) {
	for format, want := range map[string]string{
		"a %d b %s":        "ds",
		"%% %v %-08.3f %T": "vfT",
		"%*d %#v":          "ddv",
		"no verbs":         "",
	} {
		if got := string(formatVerbs(format)); got != want {
			t.Errorf("formatVerbs(%q) = %q, want %q", format, got, want)
		}
	}
}
