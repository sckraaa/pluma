package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/authorization/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/authorization/internal/logic"
	"github.com/teamgram/teamgram-server/app/bff/authorization/internal/svc"
	"github.com/teamgram/teamgram-server/app/bff/authorization/model"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/logincode"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc/status"
)

const (
	signUpAuthKeyId = 2002
	signUpPhone     = "79991234567"
)

// fakeUsers records created accounts. Methods the handler must not call
// panic through the nil embedded interface.
type fakeUsers struct {
	user_client.UserClient
	mu      sync.Mutex
	created []string
}

func (f *fakeUsers) UserCreateNewUser(_ context.Context, in *userpb.TLUserCreateNewUser) (*mtproto.ImmutableUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in.Phone)
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{Id: 42, FirstName: in.FirstName, Phone: in.Phone}).To_UserData(),
	}).To_ImmutableUser(), nil
}

func (f *fakeUsers) createdPhones() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.created...)
}

type fakeAuthsession struct {
	authsession_client.AuthsessionClient
	keys int
}

func (f *fakeAuthsession) AuthsessionSetAuthKey(context.Context, *authsession.TLAuthsessionSetAuthKey) (*mtproto.Bool, error) {
	f.keys++
	return mtproto.BoolTrue, nil
}

func (f *fakeAuthsession) AuthsessionBindAuthKeyUser(context.Context, *authsession.TLAuthsessionBindAuthKeyUser) (*mtproto.Int64, error) {
	return &mtproto.Int64{V: 0}, nil
}

type memPhoneCodes struct {
	mu sync.Mutex
	m  map[string]model.PhoneCodeTransaction
}

func (s *memPhoneCodes) GetCachePhoneCode(_ context.Context, authKeyId int64, phone string) (*model.PhoneCodeTransaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[fmt.Sprint(authKeyId, phone)]
	if !ok {
		return nil, nil
	}
	return &v, nil
}

func (s *memPhoneCodes) PutCachePhoneCode(_ context.Context, authKeyId int64, phone string, v *model.PhoneCodeTransaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[fmt.Sprint(authKeyId, phone)] = *v
	return nil
}

func (s *memPhoneCodes) DeleteCachePhoneCode(_ context.Context, authKeyId int64, phone string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, fmt.Sprint(authKeyId, phone))
	return nil
}

type allowedPhones map[string]bool

func (a allowedPhones) Contains(_ context.Context, phone string) (bool, error) { return a[phone], nil }

func rpcErrorMessage(err error) string {
	if st, ok := status.FromError(err); ok && err != nil {
		return st.Message()
	}
	return fmt.Sprint(err)
}

// TestAuthSignUpHandlerRequiresAcceptedCode guards the handler: upstream
// auth.signUp created an account for any phone_code_hash, without the code
// having been accepted by auth.signIn.
func TestAuthSignUpHandlerRequiresAcceptedCode(t *testing.T) {
	// miniredis does not implement CLIENT SETINFO; DisableIdentity keeps the failing
	// connect-time command from tripping the go-zero redis breaker.
	mr := miniredis.RunT(t)
	codes, err := logincode.NewService(logincode.Config{HmacKey: strings.Repeat("k", 32)}, allowedPhones{signUpPhone: true}, kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}}))
	if err != nil {
		t.Fatal(err)
	}
	var (
		ctx      = context.Background()
		users    = &fakeUsers{}
		sessions = &fakeAuthsession{}
		auth     = logic.NewAuthSignLogic(&memPhoneCodes{m: map[string]model.PhoneCodeTransaction{}}, codes)
		c        = &AuthorizationCore{
			ctx: ctx,
			svcCtx: &svc.ServiceContext{
				Dao:       &dao.Dao{UserClient: users, AuthsessionClient: sessions},
				AuthLogic: auth,
			},
			Logger: logx.WithContext(ctx),
			MD:     &metadata.RpcMetadata{PermAuthKeyId: signUpAuthKeyId, ClientAddr: "203.0.113.1", Layer: 228},
		}
	)
	signUp := func(hash string) error {
		_, err := c.AuthSignUp(&mtproto.TLAuthSignUp{
			PhoneNumber:   "+" + signUpPhone,
			PhoneCodeHash: hash,
			FirstName:     "Alice",
		})
		return err
	}

	if got := rpcErrorMessage(signUp("no-such-request")); got != "PHONE_CODE_EXPIRED" {
		t.Fatalf("without a request: %s", got)
	}
	// A code was requested (new phone: the administrator path) but not entered.
	codeData, err := auth.DoAuthSendCode(ctx, signUpAuthKeyId, 1, signUpPhone, 0, "203.0.113.1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := rpcErrorMessage(signUp(codeData.PhoneCodeHash)); got != "INPUT_REQUEST_INVALID" {
		t.Fatalf("before signIn: %s", got)
	}
	r, err := codes.Reissue(ctx, signUpPhone, "")
	if err != nil {
		t.Fatal(err)
	}
	wrong := "000000"
	if r.Code == wrong {
		wrong = "111111"
	}
	if _, err := c.AuthSignIn(&mtproto.TLAuthSignIn{
		PhoneNumber:      "+" + signUpPhone,
		PhoneCodeHash:    codeData.PhoneCodeHash,
		PhoneCode_STRING: wrong,
	}); rpcErrorMessage(err) != "PHONE_CODE_INVALID" {
		t.Fatalf("signIn with a wrong code: %v", err)
	}
	if got := rpcErrorMessage(signUp(codeData.PhoneCodeHash)); got != "INPUT_REQUEST_INVALID" {
		t.Fatalf("after a wrong code: %s", got)
	}
	if len(users.createdPhones()) != 0 || sessions.keys != 0 {
		t.Fatalf("account created without an accepted code: %v", users.createdPhones())
	}

	// auth.signIn accepts the code: auth.signUp may create the account.
	res, err := c.AuthSignIn(&mtproto.TLAuthSignIn{
		PhoneNumber:      "+" + signUpPhone,
		PhoneCodeHash:    codeData.PhoneCodeHash,
		PhoneCode_STRING: r.Code,
	})
	if err != nil || res.GetPredicateName() != mtproto.Predicate_auth_authorizationSignUpRequired {
		t.Fatalf("signIn with the right code: %v %v", res, err)
	}
	if err := signUp(codeData.PhoneCodeHash); err != nil {
		t.Fatalf("signUp after signIn: %v", err)
	}
	if got := users.createdPhones(); len(got) != 1 || got[0] != signUpPhone {
		t.Fatalf("created %v", got)
	}
}

func TestSignInMessageEntities(t *testing.T) {
	for _, code := range []string{"12345", "123456", "1234567"} {
		text, entities := signInMessageText(code)
		if !strings.Contains(text, code) || len(entities) != 2 {
			t.Fatalf("%q: %q %v", code, text, entities)
		}
		for i, want := range []string{"Login code:", "not"} {
			e := entities[i]
			if got := text[e.Offset : e.Offset+e.Length]; got != want {
				t.Errorf("code %q entity %d covers %q, want %q", code, i, got, want)
			}
		}
	}
}
