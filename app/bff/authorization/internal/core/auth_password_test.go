package core

import (
	"context"
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
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/twofa"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The login of a user with a 2FA password (task 008): auth.signIn and a
// future auth token answer SESSION_PASSWORD_NEEDED without binding the key,
// auth.checkPassword binds it only for the right password.

const (
	pwUserId  = int64(42)
	pwPhone   = "79991230042"
	pwKey     = int64(7001)
	freeUser  = int64(43)
	freePhone = "79991230043"
)

type passwordUsers struct {
	user_client.UserClient
}

func immutableUser(id int64, phone string) *mtproto.ImmutableUser {
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{Id: id, FirstName: "U", Phone: phone}).To_UserData(),
	}).To_ImmutableUser()
}

func (passwordUsers) UserGetImmutableUserByPhone(_ context.Context, in *userpb.TLUserGetImmutableUserByPhone) (*mtproto.ImmutableUser, error) {
	switch in.Phone {
	case pwPhone:
		return immutableUser(pwUserId, pwPhone), nil
	case freePhone:
		return immutableUser(freeUser, freePhone), nil
	}
	return nil, mtproto.ErrPhoneNumberUnoccupied
}

func (passwordUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	switch in.Id {
	case pwUserId:
		return immutableUser(pwUserId, pwPhone), nil
	case freeUser:
		return immutableUser(freeUser, freePhone), nil
	}
	return nil, mtproto.ErrUserIdInvalid
}

// bindings records auth keys bound to users.
type bindings struct {
	authsession_client.AuthsessionClient
	mu    sync.Mutex
	bound map[int64]int64
}

func (b *bindings) AuthsessionBindAuthKeyUser(_ context.Context, in *authsession.TLAuthsessionBindAuthKeyUser) (*mtproto.Int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bound[in.AuthKeyId] = in.UserId
	return &mtproto.Int64{V: 1}, nil
}

func (b *bindings) of(key int64) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bound[key]
}

type pushes struct {
	sync_client.SyncClient
	mu    sync.Mutex
	users []int64
}

func (p *pushes) SyncUpdatesNotMe(_ context.Context, in *syncpb.TLSyncUpdatesNotMe) (*mtproto.Void, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.users = append(p.users, in.UserId)
	return &mtproto.Void{}, nil
}

type passwordEnv struct {
	ctx      context.Context
	codes    *logincode.Service
	auth     *logic.AuthLogic
	twofa    *twofa.Service
	bound    *bindings
	pushes   *pushes
	svcCtx   *svc.ServiceContext
	phoneLog *memPhoneCodes
}

func newPasswordEnv(t *testing.T) *passwordEnv {
	t.Helper()
	mr := miniredis.RunT(t)
	store := testKV(mr)
	codes, err := logincode.NewService(logincode.Config{HmacKey: strings.Repeat("k", 32)}, allowedPhones{pwPhone: true, freePhone: true}, store)
	if err != nil {
		t.Fatal(err)
	}
	e := &passwordEnv{
		ctx:      context.Background(),
		codes:    codes,
		twofa:    twofa.NewService(twofa.NewMemStore(), store),
		bound:    &bindings{bound: map[int64]int64{}},
		pushes:   &pushes{},
		phoneLog: &memPhoneCodes{m: map[string]model.PhoneCodeTransaction{}},
	}
	e.auth = logic.NewAuthSignLogic(e.phoneLog, codes)
	e.svcCtx = &svc.ServiceContext{
		Dao:       &dao.Dao{UserClient: passwordUsers{}, AuthsessionClient: e.bound, SyncClient: e.pushes},
		AuthLogic: e.auth,
		Twofa:     e.twofa,
	}
	return e
}

// core returns the handlers as a request of key with the user of the key.
func (e *passwordEnv) core(key, userId int64) *AuthorizationCore {
	return &AuthorizationCore{
		ctx:    e.ctx,
		svcCtx: e.svcCtx,
		Logger: logx.WithContext(e.ctx),
		MD:     &metadata.RpcMetadata{PermAuthKeyId: key, UserId: userId, ClientAddr: "203.0.113.7", Layer: 228},
	}
}

func (e *passwordEnv) setPassword(t *testing.T, userId int64, password string) {
	t.Helper()
	pw, err := e.twofa.Password(e.ctx, userId)
	if err != nil {
		t.Fatal(err)
	}
	algo, err := mtclient.NewPasswordAlgo(pw.GetNewAlgo())
	if err != nil {
		t.Fatal(err)
	}
	hash, err := mtclient.PasswordHash(algo, password)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.twofa.Update(e.ctx, userId, 0, mtclient.NoPassword(), mtproto.MakeTLAccountPasswordInputSettings(&mtproto.Account_PasswordInputSettings{
		NewAlgo:         algo,
		NewPasswordHash: hash,
		Hint:            wrapperspb.String(""),
	}).To_Account_PasswordInputSettings()); err != nil {
		t.Fatal(err)
	}
}

func (e *passwordEnv) proof(t *testing.T, userId int64, password string) *mtproto.InputCheckPasswordSRP {
	t.Helper()
	pw, err := e.twofa.Password(e.ctx, userId)
	if err != nil {
		t.Fatal(err)
	}
	in, err := mtclient.CheckPassword(pw, password)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// signIn requests a code for phone on key, has the administrator issue it
// and signs in with it.
func (e *passwordEnv) signIn(t *testing.T, key int64, phone string, userId int64) (*mtproto.Auth_Authorization, string, error) {
	t.Helper()
	codeData, err := e.auth.DoAuthSendCode(e.ctx, key, 1, phone, userId, "203.0.113.7", nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.codes.Reissue(e.ctx, phone, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.core(key, 0).AuthSignIn(&mtproto.TLAuthSignIn{
		PhoneNumber:      "+" + phone,
		PhoneCodeHash:    codeData.PhoneCodeHash,
		PhoneCode_STRING: r.Code,
	})
	return a, codeData.PhoneCodeHash, err
}

func TestSignInWaitsForPassword(t *testing.T) {
	e := newPasswordEnv(t)
	e.setPassword(t, pwUserId, "secret")

	_, _, err := e.signIn(t, pwKey, pwPhone, pwUserId)
	if got := rpcErrorMessage(err); got != "SESSION_PASSWORD_NEEDED" {
		t.Fatalf("auth.signIn of a user with a password: %s", got)
	}
	if e.bound.of(pwKey) != 0 {
		t.Fatal("the key was bound before the password")
	}
	if l, _ := e.twofa.WaitingLogin(e.ctx, pwKey); l == nil || l.UserId != pwUserId || l.Phone != pwPhone {
		t.Fatalf("waiting login %v", l)
	}

	// Wrong passwords leave the key unbound.
	for _, in := range []*mtproto.InputCheckPasswordSRP{e.proof(t, pwUserId, "guess"), mtclient.NoPassword()} {
		if _, err = e.core(pwKey, 0).AuthCheckPassword(&mtproto.TLAuthCheckPassword{Password: in}); rpcErrorMessage(err) != "PASSWORD_HASH_INVALID" {
			t.Fatalf("a wrong password: %v", err)
		}
	}
	// Another key has no login waiting.
	if _, err = e.core(pwKey+1, 0).AuthCheckPassword(&mtproto.TLAuthCheckPassword{Password: e.proof(t, pwUserId, "secret")}); rpcErrorMessage(err) != "AUTH_KEY_UNREGISTERED" {
		t.Fatalf("auth.checkPassword on another key: %v", err)
	}
	if e.bound.of(pwKey) != 0 || e.bound.of(pwKey+1) != 0 {
		t.Fatal("a key was bound without the password")
	}

	a, err := e.core(pwKey, 0).AuthCheckPassword(&mtproto.TLAuthCheckPassword{Password: e.proof(t, pwUserId, "secret")})
	if err != nil || a.GetPredicateName() != mtproto.Predicate_auth_authorization || a.GetUser().GetId() != pwUserId || !a.GetUser().GetSelf() {
		t.Fatalf("auth.checkPassword: %v %v", a, err)
	}
	if e.bound.of(pwKey) != pwUserId {
		t.Fatal("the key is not bound after the password")
	}
	if l, _ := e.twofa.WaitingLogin(e.ctx, pwKey); l != nil {
		t.Fatalf("the login still waits: %v", l)
	}
	if v, _ := e.phoneLog.GetCachePhoneCode(e.ctx, pwKey, pwPhone); v != nil {
		t.Fatal("the code request is left")
	}
	if len(e.pushes.users) != 1 || e.pushes.users[0] != pwUserId {
		t.Fatalf("the other devices were told of %v", e.pushes.users)
	}

	// The login is done: the same proof again finds no login.
	if _, err = e.core(pwKey+2, 0).AuthCheckPassword(&mtproto.TLAuthCheckPassword{Password: e.proof(t, pwUserId, "secret")}); rpcErrorMessage(err) != "AUTH_KEY_UNREGISTERED" {
		t.Fatalf("auth.checkPassword after the login: %v", err)
	}
	// A logged-in key checks its own password.
	if a, err = e.core(pwKey, pwUserId).AuthCheckPassword(&mtproto.TLAuthCheckPassword{Password: e.proof(t, pwUserId, "secret")}); err != nil || a.GetUser().GetId() != pwUserId {
		t.Fatalf("auth.checkPassword of a logged-in key: %v %v", a, err)
	}
}

func TestSignInWithoutPassword(t *testing.T) {
	e := newPasswordEnv(t)
	a, _, err := e.signIn(t, pwKey, freePhone, freeUser)
	if err != nil || a.GetUser().GetId() != freeUser {
		t.Fatalf("auth.signIn: %v %v", a, err)
	}
	if e.bound.of(pwKey) != freeUser {
		t.Fatal("the key is not bound")
	}
}

func TestPasswordRemovedWhileWaiting(t *testing.T) {
	e := newPasswordEnv(t)
	e.setPassword(t, pwUserId, "secret")
	if _, _, err := e.signIn(t, pwKey, pwPhone, pwUserId); rpcErrorMessage(err) != "SESSION_PASSWORD_NEEDED" {
		t.Fatal(err)
	}
	in := e.proof(t, pwUserId, "secret")
	if _, err := e.twofa.Remove(e.ctx, pwUserId); err != nil {
		t.Fatal(err)
	}
	// The administrator removed it: the client starts over and logs in
	// without one.
	if _, err := e.core(pwKey, 0).AuthCheckPassword(&mtproto.TLAuthCheckPassword{Password: in}); rpcErrorMessage(err) != "PASSWORD_EMPTY" {
		t.Fatalf("auth.checkPassword: %v", err)
	}
	if l, _ := e.twofa.WaitingLogin(e.ctx, pwKey); l != nil || e.bound.of(pwKey) != 0 {
		t.Fatalf("login %v, bound to %d", l, e.bound.of(pwKey))
	}
}

func TestRecoveryIsNotAvailable(t *testing.T) {
	e := newPasswordEnv(t)
	c := e.core(pwKey, 0)
	if _, err := c.AuthRequestPasswordRecovery(&mtproto.TLAuthRequestPasswordRecovery{}); rpcErrorMessage(err) != "PASSWORD_RECOVERY_NA" {
		t.Fatalf("auth.requestPasswordRecovery: %v", err)
	}
	if _, err := c.AuthRecoverPassword(&mtproto.TLAuthRecoverPassword{Code: "123456"}); rpcErrorMessage(err) != "PASSWORD_RECOVERY_NA" {
		t.Fatalf("auth.recoverPassword: %v", err)
	}
	if _, err := c.AuthCheckRecoveryPassword(&mtproto.TLAuthCheckRecoveryPassword{Code: "123456"}); rpcErrorMessage(err) != "PASSWORD_RECOVERY_NA" {
		t.Fatalf("auth.checkRecoveryPassword: %v", err)
	}
	if _, err := c.AccountResetPassword(&mtproto.TLAccountResetPassword{}); rpcErrorMessage(err) != "AUTH_KEY_UNREGISTERED" {
		t.Fatalf("account.resetPassword without a user: %v", err)
	}
	e.setPassword(t, pwUserId, "secret")
	r, err := e.core(pwKey, pwUserId).AccountResetPassword(&mtproto.TLAccountResetPassword{})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_account_resetPasswordRequestedWait {
		t.Fatalf("account.resetPassword: %v %v", r, err)
	}
}
