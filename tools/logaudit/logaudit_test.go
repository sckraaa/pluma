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
	"github.com/zeromicro/go-zero/core/logx"
)

type HandshakeStateCtx struct{ NewNonce, A []byte }
`
	cases := []struct {
		name string
		body string
		want []string
	}{
		// audit #8: handshake secrets
		{"handshake state", `func f(ctx *HandshakeStateCtx) { logx.Infof("state: %s", ctx) }`, []string{"object"}},
		{"new_nonce", `func f(ctx *HandshakeStateCtx) { logx.Infof("n: %x", ctx.NewNonce) }`, []string{"secret"}},
		{"dh a", `func f(ctx *HandshakeStateCtx) { logx.Debugf("a: %v", ctx.A) }`, []string{"secret"}},
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
		{"srp proof", `func f(in *mtproto.InputCheckPasswordSRP) { logx.Infof("A %x M1 %x", in.GetA(), in.M1) }`, []string{"secret", "secret"}},
		{"srp values", `func f(p *mtproto.Account_Password, s *mtproto.Account_PasswordInputSettings) { logx.Infof("%x %x", p.GetSrp_B(), s.GetNewPasswordHash()) }`, []string{"secret", "secret"}},
		{"srp id ok", `func f(in *mtproto.InputCheckPasswordSRP) { logx.Infof("srp_id %d", in.GetSrpId()) }`, nil},
		{"token", `func f(in *mtproto.TLInvokeWithReCaptcha) { logx.Infof("%s", in.Token) }`, []string{"secret"}},
		{"config with credentials", `func f(c struct{ Etcd struct{ Hosts []string; Pass string } }) { logx.Errorf("etcd %+v", c.Etcd) }`, []string{"secret"}},
		{"config hosts ok", `func f(c struct{ Etcd struct{ Hosts []string; Pass string } }) { logx.Errorf("etcd %v", c.Etcd.Hosts) }`, nil},
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
