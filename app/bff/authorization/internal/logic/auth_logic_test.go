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
	resent, err := m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash)
	if err != nil {
		t.Fatal(err)
	}
	if got := resent.ToAuthSentCode(); got.GetType().GetPredicateName() != mtproto.Predicate_auth_sentCodeTypeSms || got.GetNextType() != nil || got.GetPhoneCodeHash() != codeData.PhoneCodeHash {
		t.Fatalf("resend answered %v", got)
	}
	if _, err := m.DoAuthReSendCode(ctx, authKeyId, phone, codeData.PhoneCodeHash); rpcMessage(err) != "SEND_CODE_UNAVAILABLE" {
		t.Fatalf("second resend: %v", err)
	}
	if _, err := m.DoAuthReSendCode(ctx, authKeyId, phone, "bogus"); rpcMessage(err) != "PHONE_CODE_INVALID" {
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
