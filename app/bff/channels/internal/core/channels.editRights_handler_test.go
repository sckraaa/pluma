package core

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/channels/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/twofa"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc/status"
)

// channels.editCreator checks the 2FA password before anything else, as
// Android probes it with an empty one and no channel needed to answer
// (ChatRightsEditActivity.java:1116-1148): without a password it is
// PASSWORD_MISSING whatever the channel.
func TestEditCreatorChecksPasswordFirst(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	s := twofa.NewService(twofa.NewMemStore(), kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}}))
	c := &ChannelsCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Twofa: s},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: 52, PermAuthKeyId: 3},
	}
	_, err := c.ChannelsEditCreator(&mtproto.TLChannelsEditCreator{Password: mtclient.NoPassword()})
	if st, _ := status.FromError(err); st.Message() != "PASSWORD_MISSING" {
		t.Fatalf("without a password: %v", err)
	}
}
