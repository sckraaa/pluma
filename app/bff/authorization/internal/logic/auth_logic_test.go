package logic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/authorization/model"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc/status"
)

const (
	phone     = "79991234567"
	authKeyId = int64(1001)
	userId    = int64(42)
)

type memStore struct {
	mu sync.Mutex
	m  map[string]model.PhoneCodeTransaction
}

func key(authKeyId int64, phone string) string { return fmt.Sprintf("%d_%s", authKeyId, phone) }

func (s *memStore) GetCachePhoneCode(_ context.Context, authKeyId int64, phone string) (*model.PhoneCodeTransaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key(authKeyId, phone)]
	if !ok {
		return nil, nil
	}
	return &v, nil
}

func (s *memStore) PutCachePhoneCode(_ context.Context, authKeyId int64, phone string, v *model.PhoneCodeTransaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key(authKeyId, phone)] = *v
	return nil
}

func (s *memStore) DeleteCachePhoneCode(_ context.Context, authKeyId int64, phone string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key(authKeyId, phone))
	return nil
}

type allowlist map[string]bool

func (a allowlist) Contains(_ context.Context, phone string) (bool, error) { return a[phone], nil }

// fakeDelivery records what would be sent to the user's app.
type fakeDelivery struct {
	others    bool
	err       error
	asked     int
	delivered []string
}

func (d *fakeDelivery) HasOtherAuthorizations(_ context.Context, uid, kid int64) (bool, error) {
	d.asked++
	if uid != userId || kid != authKeyId {
		return false, fmt.Errorf("unexpected user %d / key %d", uid, kid)
	}
	return d.others, d.err
}

func (d *fakeDelivery) DeliverToApp(_ context.Context, uid int64, code string) {
	if uid == userId {
		d.delivered = append(d.delivered, code)
	}
}

func newLogic(t *testing.T) (*AuthLogic, *memStore) {
	t.Helper()
	// miniredis does not implement CLIENT SETINFO; DisableIdentity keeps the failing
	// connect-time command from tripping the go-zero redis breaker.
	mr := miniredis.RunT(t)
	kvStore := kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
	codes, err := logincode.NewService(logincode.Config{HmacKey: strings.Repeat("k", 32)}, allowlist{phone: true}, kvStore)
	if err != nil {
		t.Fatal(err)
	}
	store := &memStore{m: map[string]model.PhoneCodeTransaction{}}
	return NewAuthSignLogic(store, codes), store
}

func rpcMessage(err error) string {
	if err == nil {
		return ""
	}
	if st, ok := status.FromError(err); ok {
		return st.Message()
	}
	return err.Error()
}

func otherCode(code string) string {
	if code == "000000" {
		return "111111"
	}
	return "000000"
}

func TestSendCodeDeliverySelection(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		userId    int64
		delivery  *fakeDelivery
		wantType  string
		wantNext  bool
		wantAsked int
	}{
		{"new phone: administrator", 0, &fakeDelivery{others: true}, mtproto.Predicate_auth_sentCodeTypeSms, false, 0},
		{"account with other sessions: app", userId, &fakeDelivery{others: true}, mtproto.Predicate_auth_sentCodeTypeApp, true, 1},
		{"account without sessions: administrator", userId, &fakeDelivery{}, mtproto.Predicate_auth_sentCodeTypeSms, false, 1},
		{"session lookup fails: administrator", userId, &fakeDelivery{others: true, err: errors.New("authsession down")}, mtproto.Predicate_auth_sentCodeTypeSms, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newLogic(t)
			codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, tt.userId, "203.0.113.5", tt.delivery)
			if err != nil {
				t.Fatal(err)
			}
			sent := codeData.ToAuthSentCode()
			if got := sent.GetType().GetPredicateName(); got != tt.wantType {
				t.Fatalf("type %s, want %s", got, tt.wantType)
			}
			if sent.GetType().GetLength() != logincode.Length {
				t.Fatalf("length %d", sent.GetType().GetLength())
			}
			if (sent.GetNextType() != nil) != tt.wantNext {
				t.Fatalf("next type %v", sent.GetNextType())
			}
			if tt.delivery.asked != tt.wantAsked {
				t.Fatalf("asked for sessions %d times, want %d", tt.delivery.asked, tt.wantAsked)
			}

			if tt.wantType == mtproto.Predicate_auth_sentCodeTypeApp {
				// Exactly the delivered code logs in.
				if len(tt.delivery.delivered) != 1 {
					t.Fatalf("delivered %v", tt.delivery.delivered)
				}
				code := tt.delivery.delivered[0]
				if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, otherCode(code), codeData.PhoneCodeHash, ""); rpcMessage(err) != "PHONE_CODE_INVALID" {
					t.Fatalf("wrong code: %v", err)
				}
				got, err := m.DoAuthSignIn(ctx, authKeyId, phone, code, codeData.PhoneCodeHash, "")
				if err != nil || got.State != model.CodeStateOk {
					t.Fatalf("signIn: %v %+v", err, got)
				}
				return
			}

			if len(tt.delivery.delivered) != 0 {
				t.Fatalf("code delivered to the app: %v", tt.delivery.delivered)
			}
			// No code exists until the administrator issues one: nothing,
			// including the old fixed 12345, gets in.
			for _, c := range []string{"123456", "000000"} {
				if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, c, codeData.PhoneCodeHash, ""); rpcMessage(err) != "PHONE_CODE_INVALID" {
					t.Fatalf("code %s before the administrator: %v", c, err)
				}
			}
			r, err := m.Codes.Reissue(ctx, phone, "")
			if err != nil {
				t.Fatal(err)
			}
			if r.ClientIp != "203.0.113.5" {
				t.Fatalf("request IP %q", r.ClientIp)
			}
			got, err := m.DoAuthSignIn(ctx, authKeyId, phone, r.Code, codeData.PhoneCodeHash, "")
			if err != nil {
				t.Fatalf("signIn with the administrator's code: %v", err)
			}
			wantState := model.CodeStateOk
			if tt.userId == 0 {
				wantState = model.CodeStateSignIn
			}
			if got.State != wantState {
				t.Fatalf("state %d, want %d", got.State, wantState)
			}
		})
	}
}

func TestSignInAttemptLimit(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	d := &fakeDelivery{others: true}
	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, userId, "", d)
	if err != nil {
		t.Fatal(err)
	}
	code := d.delivered[0]
	for i := 1; i < logincode.MaxAttempts; i++ {
		if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, otherCode(code), codeData.PhoneCodeHash, ""); rpcMessage(err) != "PHONE_CODE_INVALID" {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	// The last allowed attempt is still wrong: the request is over.
	if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, otherCode(code), codeData.PhoneCodeHash, ""); rpcMessage(err) != "PHONE_CODE_INVALID" {
		t.Fatalf("last attempt: %v", err)
	}
	// Further checks are refused by the per-phone limit or as expired; the
	// right code never works again.
	_, err = m.DoAuthSignIn(ctx, authKeyId, phone, code, codeData.PhoneCodeHash, "")
	if msg := rpcMessage(err); msg != "PHONE_CODE_EXPIRED" && !strings.HasPrefix(msg, "FLOOD_WAIT_") {
		t.Fatalf("right code after the limit: %v", err)
	}
}

func TestSignInFloodWait(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	for i := 0; ; i++ {
		d := &fakeDelivery{others: true}
		codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, userId, "", d)
		if err != nil {
			t.Fatal(err)
		}
		_, err = m.DoAuthSignIn(ctx, authKeyId, phone, otherCode(d.delivered[0]), codeData.PhoneCodeHash, "")
		if strings.HasPrefix(rpcMessage(err), "FLOOD_WAIT_") {
			if i != int(logincode.CheckCodePerPhone.Limit) {
				t.Fatalf("FLOOD_WAIT after %d checks, want %d", i, logincode.CheckCodePerPhone.Limit)
			}
			return
		}
		if rpcMessage(err) != "PHONE_CODE_INVALID" || i > 20 {
			t.Fatalf("check %d: %v", i, err)
		}
	}
}

func TestSignUpRequiresAcceptedCode(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, 0, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Straight to auth.signUp without a code.
	if _, err := m.DoAuthSignUp(ctx, authKeyId, phone, codeData.PhoneCodeHash); rpcMessage(err) != "INPUT_REQUEST_INVALID" {
		t.Fatalf("signUp before signIn: %v", err)
	}
	if _, err := m.DoAuthSignUp(ctx, authKeyId+1, phone, codeData.PhoneCodeHash); rpcMessage(err) != "PHONE_CODE_EXPIRED" {
		t.Fatalf("signUp from another auth key: %v", err)
	}

	r, _ := m.Codes.Reissue(ctx, phone, "")
	if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, r.Code, codeData.PhoneCodeHash, ""); err != nil {
		t.Fatal(err)
	}
	// A repeated signIn of the same key is answered without a new check.
	if got, err := m.DoAuthSignIn(ctx, authKeyId, phone, "garbage", codeData.PhoneCodeHash, ""); err != nil || got.State != model.CodeStateSignIn {
		t.Fatalf("repeated signIn: %v", err)
	}
	if _, err := m.DoAuthSignUp(ctx, authKeyId, phone, "other-hash"); rpcMessage(err) != "PHONE_CODE_INVALID" {
		t.Fatalf("signUp with another hash: %v", err)
	}
	if _, err := m.DoAuthSignUp(ctx, authKeyId, phone, codeData.PhoneCodeHash); err != nil {
		t.Fatalf("signUp: %v", err)
	}
}

func TestResendCodeFallsBackToAdministrator(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	d := &fakeDelivery{others: true}
	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, userId, "", d)
	if err != nil {
		t.Fatal(err)
	}
	resent, err := m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := resent.ToAuthSentCode(); got.GetType().GetPredicateName() != mtproto.Predicate_auth_sentCodeTypeSms || got.GetNextType() != nil || got.GetPhoneCodeHash() != codeData.PhoneCodeHash {
		t.Fatalf("resend answered %v", got)
	}
	if _, err := m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, ""); rpcMessage(err) != "SEND_CODE_UNAVAILABLE" {
		t.Fatalf("second resend: %v", err)
	}
	if _, err := m.DoAuthReSendCode(ctx, authKeyId, phone, "bogus", ""); rpcMessage(err) != "PHONE_CODE_INVALID" {
		t.Fatalf("resend with another hash: %v", err)
	}
	// The administrator's code works for the same phone_code_hash.
	r, err := m.Codes.Reissue(ctx, phone, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, r.Code, codeData.PhoneCodeHash, ""); err != nil {
		t.Fatalf("signIn: %v", err)
	}
}

func TestCancelAndReplaceRequests(t *testing.T) {
	ctx := context.Background()
	m, store := newLogic(t)

	first, _ := m.DoAuthSendCode(ctx, authKeyId, 7, phone, 0, "", nil)
	second, _ := m.DoAuthSendCode(ctx, authKeyId, 7, phone, 0, "", nil)
	if first.PhoneCodeHash == second.PhoneCodeHash {
		t.Fatal("phone_code_hash reused")
	}
	r, _ := m.Codes.Reissue(ctx, phone, "")
	if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, r.Code, first.PhoneCodeHash, ""); rpcMessage(err) != "PHONE_CODE_INVALID" {
		t.Fatalf("replaced request: %v", err)
	}

	// Cancelling with a stale hash keeps the current request.
	if err := m.DoAuthCancelCode(ctx, authKeyId, phone, first.PhoneCodeHash); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.GetCachePhoneCode(ctx, authKeyId, phone); v == nil {
		t.Fatal("stale cancel removed the current request")
	}
	if err := m.DoAuthCancelCode(ctx, authKeyId, phone, second.PhoneCodeHash); err != nil {
		t.Fatal(err)
	}
	if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, r.Code, second.PhoneCodeHash, ""); rpcMessage(err) != "PHONE_CODE_EXPIRED" {
		t.Fatalf("cancelled request: %v", err)
	}
	if _, err := m.Codes.Reissue(ctx, phone, ""); !errors.Is(err, logincode.ErrNoPendingRequest) {
		t.Fatalf("reissue after cancel: %v", err)
	}
}

func TestAllowlistRejection(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	if err := m.Codes.CheckSendCode(ctx, "79990000000", authKeyId, "203.0.113.9"); rpcMessage(err) != "PHONE_NUMBER_INVALID" {
		t.Fatalf("sendCode: %v", err)
	}
	if err := m.Codes.CheckPhone(ctx, "79990000000", authKeyId, "203.0.113.9"); rpcMessage(err) != "PHONE_NUMBER_INVALID" {
		t.Fatalf("signIn/signUp/cancelCode: %v", err)
	}
	if err := m.Codes.CheckPhone(ctx, phone, authKeyId, "203.0.113.9"); err != nil {
		t.Fatalf("signIn with an allowed phone: %v", err)
	}
	if err := m.Codes.CheckSendCode(ctx, phone, authKeyId, "203.0.113.9"); err != nil {
		t.Fatalf("allowed phone: %v", err)
	}
}

// fakeSender plays the service of the Telegram account (pkg/logincode.Sender).
type fakeSender struct {
	result  logincode.SendResult
	account string
	codes   []string
}

func (f *fakeSender) Send(_ context.Context, _, code string) (logincode.SendReply, error) {
	f.codes = append(f.codes, code)
	return logincode.SendReply{Result: f.result, Account: f.account}, nil
}

const clientIP = "203.0.113.5"

func typeOf(c *model.PhoneCodeTransaction) string {
	return c.ToAuthSentCode().GetType().GetPredicateName()
}

// A code in the person's Telegram: the Fragment screen's layout, its button
// opens the chat with the account, "Didn't get the code?" at once.
func TestSendCodeThroughTelegramAccount(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	s := &fakeSender{result: logincode.SendSent, account: "pluma_codes"}
	m.Codes.SetSender(s)

	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, 0, clientIP, &fakeDelivery{})
	if err != nil {
		t.Fatal(err)
	}
	sent := codeData.ToAuthSentCode()
	if got := sent.GetType().GetPredicateName(); got != mtproto.Predicate_auth_sentCodeTypeFragmentSms {
		t.Fatalf("type %s", got)
	}
	if url := sent.GetType().GetUrl(); url != "tg://resolve?domain=pluma_codes" {
		t.Fatalf("url %q", url)
	}
	if sent.GetType().GetLength() != logincode.Length {
		t.Fatalf("length %d", sent.GetType().GetLength())
	}
	if sent.GetNextType().GetPredicateName() != mtproto.Predicate_auth_codeTypeSms || sent.GetTimeout() != nil {
		t.Fatalf("next %v, timeout %v", sent.GetNextType(), sent.GetTimeout())
	}
	if len(s.codes) != 1 {
		t.Fatalf("sent %d codes", len(s.codes))
	}
	if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, s.codes[0], codeData.PhoneCodeHash, clientIP); err != nil {
		t.Fatalf("the code written does not sign in: %v", err)
	}
}

// An account without a username gives no link: the apps hide the button.
func TestSendCodeThroughAccountWithoutUsername(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	m.Codes.SetSender(&fakeSender{result: logincode.SendSent})
	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, 0, clientIP, &fakeDelivery{})
	if err != nil {
		t.Fatal(err)
	}
	if typ := codeData.ToAuthSentCode().GetType(); typ.GetPredicateName() != mtproto.Predicate_auth_sentCodeTypeFragmentSms || typ.GetUrl() != "" {
		t.Fatalf("type %v", typ)
	}
}

// Telegram could not take the code: the screen of a code that did not reach
// it, with the administrator's code and "Send again" after a countdown.
func TestSendCodeRefusedByTelegramFallsBack(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	m.Codes.SetSender(&fakeSender{result: logincode.SendNoTelegram})

	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, 0, clientIP, &fakeDelivery{})
	if err != nil {
		t.Fatal(err)
	}
	sent := codeData.ToAuthSentCode()
	if got := sent.GetType().GetPredicateName(); got != mtproto.Predicate_auth_sentCodeTypeSms {
		t.Fatalf("type %s", got)
	}
	if sent.GetNextType().GetPredicateName() != mtproto.Predicate_auth_codeTypeSms || sent.GetTimeout().GetValue() != 30 {
		t.Fatalf("next %v, timeout %v", sent.GetNextType(), sent.GetTimeout())
	}
	r, err := m.Codes.Reissue(ctx, phone, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, r.Code, codeData.PhoneCodeHash, clientIP); err != nil {
		t.Fatalf("the administrator's code does not sign in: %v", err)
	}
}

// Without a sender nothing can be sent again: the administrator only.
func TestSendCodeWithoutSender(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, 0, clientIP, &fakeDelivery{})
	if err != nil {
		t.Fatal(err)
	}
	sent := codeData.ToAuthSentCode()
	if sent.GetType().GetPredicateName() != mtproto.Predicate_auth_sentCodeTypeSms || sent.GetNextType() != nil || sent.GetTimeout() != nil {
		t.Fatalf("sent %v", sent)
	}
	if _, err := m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP); rpcMessage(err) != "SEND_CODE_UNAVAILABLE" {
		t.Fatalf("resend: %v", err)
	}
}

// "Didn't get the code?" of a code in Telegram leads to the screen of a code
// that did not reach it without writing again; the code sent still works.
func TestDidNotGetTelegramCodeShowsFallback(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	s := &fakeSender{result: logincode.SendSent, account: "pluma_codes"}
	m.Codes.SetSender(s)
	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, 0, clientIP, &fakeDelivery{})
	if err != nil {
		t.Fatal(err)
	}
	resent, err := m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP)
	if err != nil {
		t.Fatal(err)
	}
	if got := typeOf(resent); got != mtproto.Predicate_auth_sentCodeTypeSms || len(s.codes) != 1 {
		t.Fatalf("type %s, codes %d", got, len(s.codes))
	}
	if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, s.codes[0], codeData.PhoneCodeHash, clientIP); err != nil {
		t.Fatalf("the code in Telegram stopped working: %v", err)
	}
}

// "Send again" after the person allowed Telegram to find them by number: a
// new delivery, and "Check Telegram"; the administrator's code stays valid.
func TestSendAgainTriesTelegramAnew(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	s := &fakeSender{result: logincode.SendNoTelegram}
	m.Codes.SetSender(s)
	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, 0, clientIP, &fakeDelivery{})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := m.Codes.Reissue(ctx, phone, "")
	if err != nil {
		t.Fatal(err)
	}
	s.result, s.account = logincode.SendSent, "pluma_codes"
	resent, err := m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP)
	if err != nil {
		t.Fatal(err)
	}
	if got := typeOf(resent); got != mtproto.Predicate_auth_sentCodeTypeFragmentSms || len(s.codes) != 2 {
		t.Fatalf("type %s, codes %d", got, len(s.codes))
	}
	if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, admin.Code, codeData.PhoneCodeHash, clientIP); err != nil {
		t.Fatalf("the administrator's code stopped working: %v", err)
	}
}

// "Send again" ends after maxTelegramAttempts deliveries of one request.
func TestSendAgainIsBounded(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	s := &fakeSender{result: logincode.SendNoTelegram}
	m.Codes.SetSender(s)
	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, 0, clientIP, &fakeDelivery{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < maxTelegramAttempts; i++ {
		if codeData, err = m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP); err != nil {
			t.Fatalf("send again %d: %v", i, err)
		}
	}
	if len(s.codes) != maxTelegramAttempts || codeData.ToAuthSentCode().GetNextType() != nil {
		t.Fatalf("codes %d, next %v", len(s.codes), codeData.ToAuthSentCode().GetNextType())
	}
	if _, err := m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP); rpcMessage(err) != "SEND_CODE_UNAVAILABLE" {
		t.Fatalf("one more: %v", err)
	}
}

// A code in the app (an old install's session still counts as one): "Didn't
// get the code?" writes it to the person's Telegram at once, as
// auth.resendCode sends by the next type; from there the screens go on as
// for a code that went to Telegram first.
func TestResendOfInAppCodeGoesThroughTelegram(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	s := &fakeSender{result: logincode.SendSent, account: "pluma_codes"}
	m.Codes.SetSender(s)
	d := &fakeDelivery{others: true}
	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, userId, clientIP, d)
	if err != nil {
		t.Fatal(err)
	}
	sent := codeData.ToAuthSentCode()
	if typeOf(codeData) != mtproto.Predicate_auth_sentCodeTypeApp || len(s.codes) != 0 || len(d.delivered) != 1 {
		t.Fatal("the account wrote while the app could be used")
	}
	if sent.GetNextType().GetPredicateName() != mtproto.Predicate_auth_codeTypeSms || sent.GetTimeout() != nil {
		t.Fatalf("app: next %v, timeout %v", sent.GetNextType(), sent.GetTimeout())
	}

	resent, err := m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP)
	if err != nil {
		t.Fatal(err)
	}
	got := resent.ToAuthSentCode()
	if got.GetType().GetPredicateName() != mtproto.Predicate_auth_sentCodeTypeFragmentSms || len(s.codes) != 1 {
		t.Fatalf("didn't get the code: type %s, codes %d", got.GetType().GetPredicateName(), len(s.codes))
	}
	if got.GetType().GetUrl() != "tg://resolve?domain=pluma_codes" || got.GetNextType().GetPredicateName() != mtproto.Predicate_auth_codeTypeSms {
		t.Fatalf("didn't get the code: %v", got)
	}

	// "Didn't get the code?" of the code in Telegram: the screen of a code
	// that did not reach it, nothing written again; then "Send again".
	if resent, err = m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP); err != nil {
		t.Fatal(err)
	}
	if typeOf(resent) != mtproto.Predicate_auth_sentCodeTypeSms || len(s.codes) != 1 {
		t.Fatalf("fallback: type %s, codes %d", typeOf(resent), len(s.codes))
	}
	if resent, err = m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP); err != nil {
		t.Fatal(err)
	}
	if typeOf(resent) != mtproto.Predicate_auth_sentCodeTypeFragmentSms || len(s.codes) != 2 {
		t.Fatalf("send again: type %s, codes %d", typeOf(resent), len(s.codes))
	}
	if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, s.codes[1], codeData.PhoneCodeHash, clientIP); err != nil {
		t.Fatalf("the code in Telegram does not sign in: %v", err)
	}
}

// A code in the app of a person whom Telegram does not find: "Didn't get the
// code?" tries Telegram and lands on the screen of a code that did not reach
// it, with "Send again" after the countdown.
func TestResendOfInAppCodeNotFoundInTelegram(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	s := &fakeSender{result: logincode.SendNoTelegram}
	m.Codes.SetSender(s)
	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, userId, clientIP, &fakeDelivery{others: true})
	if err != nil {
		t.Fatal(err)
	}
	resent, err := m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP)
	if err != nil {
		t.Fatal(err)
	}
	got := resent.ToAuthSentCode()
	if got.GetType().GetPredicateName() != mtproto.Predicate_auth_sentCodeTypeSms || len(s.codes) != 1 {
		t.Fatalf("type %s, attempts %d", got.GetType().GetPredicateName(), len(s.codes))
	}
	if got.GetNextType().GetPredicateName() != mtproto.Predicate_auth_codeTypeSms || got.GetTimeout().GetValue() != 30 {
		t.Fatalf("next %v, timeout %v", got.GetNextType(), got.GetTimeout())
	}
	s.result, s.account = logincode.SendSent, "pluma_codes"
	if resent, err = m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP); err != nil {
		t.Fatal(err)
	}
	if typeOf(resent) != mtproto.Predicate_auth_sentCodeTypeFragmentSms || len(s.codes) != 2 {
		t.Fatalf("send again: type %s, codes %d", typeOf(resent), len(s.codes))
	}
}

// Only a resend that writes a code counts under the code request limits; one
// that moves the app to the screen of a code that did not reach Telegram
// passes with the budget spent, and a delivery over it changes nothing.
func TestResendLimitsCountDeliveriesOnly(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	s := &fakeSender{result: logincode.SendSent, account: "pluma_codes"}
	m.Codes.SetSender(s)
	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, userId, clientIP, &fakeDelivery{others: true})
	if err != nil {
		t.Fatal(err)
	}
	// The other requests of this phone from this address this hour, from
	// other devices: one delivery short of the per-address budget.
	for i := int64(1); i < logincode.SendCodePerPhoneIP.Limit; i++ {
		if err := m.Codes.CheckSendCode(ctx, phone, authKeyId+i, clientIP); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	// "Didn't get the code?" of the app: the last delivery of the budget.
	if codeData, err = m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP); err != nil {
		t.Fatal(err)
	}
	if typeOf(codeData) != mtproto.Predicate_auth_sentCodeTypeFragmentSms || len(s.codes) != 1 {
		t.Fatalf("type %s, codes %d", typeOf(codeData), len(s.codes))
	}
	// Screen moves are free, as are any number of checks of the phone.
	for i := 0; i < 3; i++ {
		if err := m.Codes.CheckPhone(ctx, phone, authKeyId, clientIP); err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
	}
	if codeData, err = m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP); err != nil {
		t.Fatalf("didn't get the code in Telegram: %v", err)
	}
	if typeOf(codeData) != mtproto.Predicate_auth_sentCodeTypeSms {
		t.Fatalf("type %s", typeOf(codeData))
	}
	// "Send again" over the budget: FLOOD_WAIT, nothing written, the screen
	// and its attempts as they were.
	before := *codeData
	if _, err := m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash, clientIP); !strings.HasPrefix(rpcMessage(err), "FLOOD_WAIT_") {
		t.Fatalf("send again over the budget: %v", err)
	}
	after, err := m.store.GetCachePhoneCode(ctx, authKeyId, phone)
	if err != nil || after == nil {
		t.Fatalf("request: %v %v", after, err)
	}
	if len(s.codes) != 1 || after.SentCodeType != before.SentCodeType || after.NextCodeType != before.NextCodeType || after.TelegramAttempts != before.TelegramAttempts {
		t.Fatalf("a refused delivery changed the request: codes %d, %+v", len(s.codes), after)
	}
	// The code in Telegram still signs in.
	if _, err := m.DoAuthSignIn(ctx, authKeyId, phone, s.codes[0], codeData.PhoneCodeHash, clientIP); err != nil {
		t.Fatalf("signIn: %v", err)
	}
}

// auth.reportMissingCode of older apps: accepted for a live request only.
func TestReportMissingCode(t *testing.T) {
	ctx := context.Background()
	m, _ := newLogic(t)
	m.Codes.SetSender(&fakeSender{result: logincode.SendNoTelegram})
	codeData, err := m.DoAuthSendCode(ctx, authKeyId, 7, phone, 0, clientIP, &fakeDelivery{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.DoAuthReportMissingCode(ctx, authKeyId, phone, codeData.PhoneCodeHash); err != nil {
		t.Fatal(err)
	}
	if err := m.DoAuthReportMissingCode(ctx, authKeyId, phone, "another-hash"); rpcMessage(err) != "PHONE_CODE_INVALID" {
		t.Fatalf("another request: %v", err)
	}
}
