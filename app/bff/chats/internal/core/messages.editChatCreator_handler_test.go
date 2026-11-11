package core

import (
	"context"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/chats/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/twofa"

	"github.com/teamgram/teamgram-server/app/bff/chats/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// oldSession is a session logged in long ago.
type oldSession struct {
	authsession_client.AuthsessionClient
}

func (oldSession) AuthsessionGetAuthorization(context.Context, *authsession.TLAuthsessionGetAuthorization) (*mtproto.Authorization, error) {
	return mtproto.MakeTLAuthorization(&mtproto.Authorization{DateCreated: 1}).To_Authorization(), nil
}

// messages.editChatCreator checks the 2FA password with the shared code
// before it answers that the transfer is not served (task 008).
func TestEditChatCreatorChecksPassword(t *testing.T) {
	const userId = int64(51)
	ctx := context.Background()
	mr := miniredis.RunT(t)
	store := twofa.NewMemStore()
	s := twofa.NewService(store, kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}}))
	c := &ChatsCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Twofa: s, Dao: &dao.Dao{AuthsessionClient: oldSession{}}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: userId, PermAuthKeyId: 3},
	}
	edit := func(in *mtproto.InputCheckPasswordSRP) string {
		_, err := c.MessagesEditChatCreator(&mtproto.TLMessagesEditChatCreator{Password: in})
		st, _ := status.FromError(err)
		return st.Message()
	}

	if got := edit(mtclient.NoPassword()); got != "PASSWORD_MISSING" {
		t.Fatalf("without a password: %s", got)
	}

	pw, err := s.Password(ctx, userId)
	if err != nil {
		t.Fatal(err)
	}
	algo, _ := mtclient.NewPasswordAlgo(pw.GetNewAlgo())
	hash, _ := mtclient.PasswordHash(algo, "pw")
	if err = s.Update(ctx, userId, 0, mtclient.NoPassword(), mtproto.MakeTLAccountPasswordInputSettings(&mtproto.Account_PasswordInputSettings{
		NewAlgo: algo, NewPasswordHash: hash, Hint: wrapperspb.String(""),
	}).To_Account_PasswordInputSettings()); err != nil {
		t.Fatal(err)
	}
	// A password just set cannot transfer anything for a day.
	if got := edit(mtclient.NoPassword()); !strings.HasPrefix(got, "PASSWORD_TOO_FRESH_") {
		t.Fatalf("a fresh password: %s", got)
	}
	aged, err := store.Get(ctx, userId)
	if err != nil {
		t.Fatal(err)
	}
	aged.SetAt -= 2 * twofa.TransferFreshWait
	if err = store.Put(ctx, aged); err != nil {
		t.Fatal(err)
	}
	proof := func(password string) *mtproto.InputCheckPasswordSRP {
		pw, err := s.Password(ctx, userId)
		if err != nil {
			t.Fatal(err)
		}
		in, err := mtclient.CheckPassword(pw, password)
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	if got := edit(mtclient.NoPassword()); got != "PASSWORD_HASH_INVALID" {
		t.Fatalf("the password not given: %s", got)
	}
	if got := edit(proof("guess")); got != "PASSWORD_HASH_INVALID" {
		t.Fatalf("a wrong password: %s", got)
	}
	if got := edit(proof("pw")); got != "PEER_ID_INVALID" {
		t.Fatalf("the right password: %s", got)
	}
}
