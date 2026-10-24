package core

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/chats/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/twofa"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// messages.editChatCreator checks the 2FA password with the shared code
// before it answers that the transfer is not served (task 008).
func TestEditChatCreatorChecksPassword(t *testing.T) {
	const userId = int64(51)
	ctx := context.Background()
	mr := miniredis.RunT(t)
	s := twofa.NewService(twofa.NewMemStore(), kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}}))
	c := &ChatsCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Twofa: s},
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
	if got := edit(proof("pw")); got != "ERR_ENTERPRISE_IS_BLOCKED" {
		t.Fatalf("the right password: %s", got)
	}
}
