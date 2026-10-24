package core

import (
	"context"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/account/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/account/internal/logic"
	"github.com/teamgram/teamgram-server/app/bff/account/internal/svc"
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
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// account.deleteAccount of an account with a 2FA password (task 008).

const (
	deleteUserId = int64(77)
	deletePhone  = "79991230077"
	deleteKey    = int64(9001)
)

type deletingUsers struct {
	user_client.UserClient
	deleted []int64
}

func (f *deletingUsers) UserGetUserDataById(_ context.Context, in *userpb.TLUserGetUserDataById) (*mtproto.UserData, error) {
	return mtproto.MakeTLUserData(&mtproto.UserData{Id: in.UserId, Phone: deletePhone}).To_UserData(), nil
}

func (f *deletingUsers) UserDeleteUser(_ context.Context, in *userpb.TLUserDeleteUser) (*mtproto.Bool, error) {
	f.deleted = append(f.deleted, in.UserId)
	return mtproto.BoolTrue, nil
}

type deletingSessions struct {
	authsession_client.AuthsessionClient
}

func (deletingSessions) AuthsessionResetAuthorization(context.Context, *authsession.TLAuthsessionResetAuthorization) (*authsession.Vector_Long, error) {
	return &authsession.Vector_Long{}, nil
}

func (deletingSessions) AuthsessionUnbindAuthKeyUser(context.Context, *authsession.TLAuthsessionUnbindAuthKeyUser) (*mtproto.Bool, error) {
	return mtproto.BoolTrue, nil
}

type notices struct {
	sync_client.SyncClient
	users []int64
}

func (n *notices) SyncUpdatesNotMe(_ context.Context, in *syncpb.TLSyncUpdatesNotMe) (*mtproto.Void, error) {
	n.users = append(n.users, in.UserId)
	return &mtproto.Void{}, nil
}

type deleteEnv struct {
	ctx    context.Context
	users  *deletingUsers
	notes  *notices
	twofa  *twofa.Service
	codes  memStore
	svcCtx *svc.ServiceContext
}

func newDeleteEnv(t *testing.T) *deleteEnv {
	t.Helper()
	mr := miniredis.RunT(t)
	store := kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
	codes, err := logincode.NewService(logincode.Config{HmacKey: strings.Repeat("k", 32)}, allowlist{}, store)
	if err != nil {
		t.Fatal(err)
	}
	e := &deleteEnv{
		ctx:   context.Background(),
		users: &deletingUsers{},
		notes: &notices{},
		twofa: twofa.NewService(twofa.NewMemStore(), store),
		codes: memStore{},
	}
	e.svcCtx = &svc.ServiceContext{
		Dao:       &dao.Dao{UserClient: e.users, AuthsessionClient: deletingSessions{}, SyncClient: e.notes},
		AuthLogic: logic.NewAuthSignLogic(e.codes, codes),
		Twofa:     e.twofa,
	}
	return e
}

func (e *deleteEnv) core(userId int64) *AccountCore {
	return &AccountCore{
		ctx:    e.ctx,
		svcCtx: e.svcCtx,
		Logger: logx.WithContext(e.ctx),
		MD:     &metadata.RpcMetadata{UserId: userId, PermAuthKeyId: deleteKey},
	}
}

func (e *deleteEnv) setPassword(t *testing.T, password string) {
	t.Helper()
	pw, err := e.twofa.Password(e.ctx, deleteUserId)
	if err != nil {
		t.Fatal(err)
	}
	algo, _ := mtclient.NewPasswordAlgo(pw.GetNewAlgo())
	hash, _ := mtclient.PasswordHash(algo, password)
	if err = e.twofa.Update(e.ctx, deleteUserId, 0, mtclient.NoPassword(), mtproto.MakeTLAccountPasswordInputSettings(&mtproto.Account_PasswordInputSettings{
		NewAlgo:         algo,
		NewPasswordHash: hash,
		Hint:            wrapperspb.String(""),
	}).To_Account_PasswordInputSettings()); err != nil {
		t.Fatal(err)
	}
}

func (e *deleteEnv) proof(t *testing.T, password string) *mtproto.InputCheckPasswordSRP {
	t.Helper()
	pw, err := e.twofa.Password(e.ctx, deleteUserId)
	if err != nil {
		t.Fatal(err)
	}
	in, err := mtclient.CheckPassword(pw, password)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func TestDeleteAccountWithoutPassword(t *testing.T) {
	e := newDeleteEnv(t)
	if _, err := e.core(0).AccountDeleteAccount(&mtproto.TLAccountDeleteAccount{Reason: "x"}); rpcMessage(err) != "AUTH_KEY_UNREGISTERED" {
		t.Fatalf("a key that is not logged in: %v", err)
	}
	if _, err := e.core(deleteUserId).AccountDeleteAccount(&mtproto.TLAccountDeleteAccount{Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	if len(e.users.deleted) != 1 || len(e.notes.users) != 0 {
		t.Fatalf("deleted %v, notified %v", e.users.deleted, e.notes.users)
	}
}

func TestDeleteAccountNeedsPassword(t *testing.T) {
	e := newDeleteEnv(t)
	e.setPassword(t, "pw")

	// A logged-in session: a wrong password is refused, the right one deletes.
	c := e.core(deleteUserId)
	if _, err := c.AccountDeleteAccount(&mtproto.TLAccountDeleteAccount{Password: e.proof(t, "guess")}); rpcMessage(err) != "PASSWORD_HASH_INVALID" {
		t.Fatalf("a wrong password: %v", err)
	}
	if _, err := c.AccountDeleteAccount(&mtproto.TLAccountDeleteAccount{Password: e.proof(t, "pw")}); err != nil {
		t.Fatalf("the right password: %v", err)
	}
	if len(e.users.deleted) != 1 {
		t.Fatalf("deleted %v", e.users.deleted)
	}
	if has, _ := e.twofa.HasPassword(e.ctx, deleteUserId); has {
		t.Fatal("the password of the deleted account is left")
	}
}

func TestResetAccountWaits(t *testing.T) {
	e := newDeleteEnv(t)
	e.setPassword(t, "pw")
	// The login with the code waits for the password the user forgot.
	if err := e.twofa.StartLogin(e.ctx, deleteKey, twofa.Login{UserId: deleteUserId, Phone: deletePhone}); err != nil {
		t.Fatal(err)
	}
	_ = e.codes.PutCachePhoneCode(e.ctx, deleteKey, deletePhone, &model.PhoneCodeTransaction{
		PhoneNumber: deletePhone, PhoneCodeHash: "h", State: model.CodeStateOk, PhoneNumberRegistered: true,
	})

	c := e.core(0)
	if _, err := c.AccountDeleteAccount(&mtproto.TLAccountDeleteAccount{Reason: "Forgot password"}); rpcMessage(err) != "2FA_CONFIRM_WAIT_604800" {
		t.Fatalf("the reset: %v", err)
	}
	if len(e.users.deleted) != 0 || len(e.notes.users) != 1 || e.notes.users[0] != deleteUserId {
		t.Fatalf("deleted %v, notified %v", e.users.deleted, e.notes.users)
	}
	// Asked again: the same wait, no second notice.
	if _, err := c.AccountDeleteAccount(&mtproto.TLAccountDeleteAccount{Reason: "Forgot password"}); !strings.HasPrefix(rpcMessage(err), "2FA_CONFIRM_WAIT_") {
		t.Fatalf("the reset again: %v", err)
	}
	if len(e.notes.users) != 1 {
		t.Fatalf("notified %v", e.notes.users)
	}
}

func TestResetAccountWithPasswordSignsUpAgain(t *testing.T) {
	e := newDeleteEnv(t)
	e.setPassword(t, "pw")
	if err := e.twofa.StartLogin(e.ctx, deleteKey, twofa.Login{UserId: deleteUserId, Phone: deletePhone}); err != nil {
		t.Fatal(err)
	}
	_ = e.codes.PutCachePhoneCode(e.ctx, deleteKey, deletePhone, &model.PhoneCodeTransaction{
		PhoneNumber: deletePhone, PhoneCodeHash: "h", State: model.CodeStateOk, PhoneNumberRegistered: true,
	})
	if _, err := e.core(0).AccountDeleteAccount(&mtproto.TLAccountDeleteAccount{Password: e.proof(t, "pw")}); err != nil {
		t.Fatal(err)
	}
	if len(e.users.deleted) != 1 {
		t.Fatalf("deleted %v", e.users.deleted)
	}
	if l, _ := e.twofa.WaitingLogin(e.ctx, deleteKey); l != nil {
		t.Fatalf("the login still waits: %v", l)
	}
	v, _ := e.codes.GetCachePhoneCode(e.ctx, deleteKey, deletePhone)
	if v == nil || v.State != model.CodeStateSignIn || v.PhoneNumberRegistered {
		t.Fatalf("the code request after the reset: %+v", v)
	}
}
