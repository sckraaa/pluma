package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/account/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/account/internal/logic"
	"github.com/teamgram/teamgram-server/app/bff/account/internal/svc"
	"github.com/teamgram/teamgram-server/app/bff/authorization/model"
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
	testUserId    = 7
	testAuthKeyId = 1001
	testNewPhone  = "79991234567"
)

// fakeUserClient records phone changes. Methods the handler must not call
// panic through the nil embedded interface.
type fakeUserClient struct {
	user_client.UserClient
	changed []string
}

func (f *fakeUserClient) UserGetImmutableUserByPhone(context.Context, *userpb.TLUserGetImmutableUserByPhone) (*mtproto.ImmutableUser, error) {
	return nil, mtproto.ErrPhoneNumberUnoccupied
}

func (f *fakeUserClient) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{Id: in.Id, FirstName: "me", Phone: "79990000001"}).To_UserData(),
	}).To_ImmutableUser(), nil
}

func (f *fakeUserClient) UserChangePhone(_ context.Context, in *userpb.TLUserChangePhone) (*mtproto.Bool, error) {
	f.changed = append(f.changed, in.Phone)
	return mtproto.BoolTrue, nil
}

type memStore map[string]model.PhoneCodeTransaction

func (s memStore) GetCachePhoneCode(_ context.Context, authKeyId int64, phone string) (*model.PhoneCodeTransaction, error) {
	v, ok := s[fmt.Sprint(authKeyId, phone)]
	if !ok {
		return nil, nil
	}
	return &v, nil
}

func (s memStore) PutCachePhoneCode(_ context.Context, authKeyId int64, phone string, v *model.PhoneCodeTransaction) error {
	s[fmt.Sprint(authKeyId, phone)] = *v
	return nil
}

func (s memStore) DeleteCachePhoneCode(_ context.Context, authKeyId int64, phone string) error {
	delete(s, fmt.Sprint(authKeyId, phone))
	return nil
}

type allowlist map[string]bool

func (a allowlist) Contains(_ context.Context, phone string) (bool, error) { return a[phone], nil }

func newChangePhoneCore(t *testing.T) (*AccountCore, *fakeUserClient, *logincode.Service) {
	t.Helper()
	mr := miniredis.RunT(t)
	codes, err := logincode.NewService(logincode.Config{HmacKey: strings.Repeat("k", 32)}, allowlist{testNewPhone: true}, kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType},
		Weight:    100,
	}}))
	if err != nil {
		t.Fatal(err)
	}
	users := &fakeUserClient{}
	ctx := context.Background()
	return &AccountCore{
		ctx: ctx,
		svcCtx: &svc.ServiceContext{
			Dao:       &dao.Dao{UserClient: users},
			AuthLogic: logic.NewAuthSignLogic(memStore{}, codes),
		},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: testUserId, PermAuthKeyId: testAuthKeyId, ClientAddr: "203.0.113.1"},
	}, users, codes
}

func rpcMessage(err error) string {
	if st, ok := status.FromError(err); ok && err != nil {
		return st.Message()
	}
	return fmt.Sprint(err)
}

// TestAccountChangePhoneHandlerRequiresCode guards the handler itself: the
// upstream bug was here (the code check's error was discarded and the phone
// changed anyway), below the logic the logic tests cover.
func TestAccountChangePhoneHandlerRequiresCode(t *testing.T) {
	c, users, codes := newChangePhoneCore(t)
	changePhone := func(code, hash string) error {
		_, err := c.AccountChangePhone(&mtproto.TLAccountChangePhone{
			PhoneNumber:   "+" + testNewPhone,
			PhoneCodeHash: hash,
			PhoneCode:     code,
		})
		return err
	}

	if got := rpcMessage(changePhone("123456", "no-such-request")); got != "PHONE_CODE_EXPIRED" {
		t.Fatalf("without a request: %s", got)
	}
	sent, err := c.AccountSendChangePhoneCode(&mtproto.TLAccountSendChangePhoneCode{
		PhoneNumber: "+" + testNewPhone,
		Settings:    mtproto.MakeTLCodeSettings(&mtproto.CodeSettings{}).To_CodeSettings(),
	})
	if err != nil {
		t.Fatal(err)
	}
	hash := sent.GetPhoneCodeHash()
	if got := rpcMessage(changePhone("123456", hash)); got != "PHONE_CODE_INVALID" {
		t.Fatalf("before the administrator issued a code: %s", got)
	}
	r, err := codes.Reissue(context.Background(), testNewPhone, "")
	if err != nil {
		t.Fatal(err)
	}
	wrong := "000000"
	if r.Code == wrong {
		wrong = "111111"
	}
	if got := rpcMessage(changePhone(wrong, hash)); got != "PHONE_CODE_INVALID" {
		t.Fatalf("wrong code: %s", got)
	}
	if got := rpcMessage(changePhone("", hash)); got != "PHONE_CODE_EMPTY" {
		t.Fatalf("empty code: %s", got)
	}
	if len(users.changed) != 0 {
		t.Fatalf("phone changed without the right code: %v", users.changed)
	}

	if err := changePhone(r.Code, hash); err != nil {
		t.Fatalf("right code: %v", err)
	}
	if len(users.changed) != 1 || users.changed[0] != testNewPhone {
		t.Fatalf("changes %v", users.changed)
	}
	// The code is used up.
	if got := rpcMessage(changePhone(r.Code, hash)); got != "PHONE_CODE_EXPIRED" {
		t.Fatalf("code reused: %s", got)
	}
	if len(users.changed) != 1 {
		t.Fatalf("changes %v", users.changed)
	}
}
