package core

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/session/internal/dao"
	"github.com/teamgram/teamgram-server/app/interface/session/internal/sess"
	"github.com/teamgram/teamgram-server/app/interface/session/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	authsessionclient "github.com/teamgram/teamgram-server/app/service/authsession/client"

	"github.com/zeromicro/go-zero/core/logx"
)

// stateDataClient answers getAuthStateData and records the keys it was asked
// about; other methods are not used by the routing code.
type stateDataClient struct {
	authsessionclient.AuthsessionClient
	asked []int64
}

func (c *stateDataClient) AuthsessionGetAuthStateData(ctx context.Context, in *authsession.TLAuthsessionGetAuthStateData) (*authsession.AuthKeyStateData, error) {
	c.asked = append(c.asked, in.AuthKeyId)
	return authsession.MakeTLAuthKeyStateData(&authsession.AuthKeyStateData{
		AuthKeyId: in.AuthKeyId,
		KeyState:  mtproto.AuthStateNormal,
		UserId:    42,
	}).To_AuthKeyStateData(), nil
}

func newRoutingCore(client *stateDataClient) *SessionCore {
	d := &dao.Dao{AuthsessionClient: client}
	return &SessionCore{
		ctx: context.Background(),
		svcCtx: &svc.ServiceContext{
			MainAuthMgr: sess.NewMainAuthWrapperManager(d),
			Dao:         d,
		},
		Logger: logx.WithContext(context.Background()),
	}
}

func TestClientMainAuthWrapperRouting(t *testing.T) {
	const (
		tempKeyId = int64(0x7e3f_0000_0000_0001)
		permKeyId = int64(0x1c71_0000_0000_0002)
	)

	t.Run("unbound temp key gets its own wrapper, no user lookup", func(t *testing.T) {
		client := &stateDataClient{}
		c := newRoutingCore(client)

		w, err := c.getOrFetchClientMainAuthWrapper(tempKeyId, mtproto.AuthKeyTypeTemp, 0)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		defer w.Stop()

		if c.svcCtx.MainAuthMgr.GetMainAuthWrapper(tempKeyId) != w {
			t.Fatal("unbound temp key wrapper not keyed by the temp key")
		}
		if w.AuthUserId != 0 {
			t.Fatalf("unbound temp key wrapper user = %d, want 0", w.AuthUserId)
		}
		if len(client.asked) != 0 {
			t.Fatalf("auth state fetched for %v", client.asked)
		}
	})

	t.Run("bound temp key uses the perm key wrapper", func(t *testing.T) {
		client := &stateDataClient{}
		c := newRoutingCore(client)

		w, err := c.getOrFetchClientMainAuthWrapper(tempKeyId, mtproto.AuthKeyTypeTemp, permKeyId)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		defer w.Stop()

		if c.svcCtx.MainAuthMgr.GetMainAuthWrapper(permKeyId) != w {
			t.Fatal("bound temp key not routed to the perm key wrapper")
		}
		if len(client.asked) != 1 || client.asked[0] != permKeyId {
			t.Fatalf("auth state fetched for %v, want [%d]", client.asked, permKeyId)
		}
	})

	t.Run("no key", func(t *testing.T) {
		c := newRoutingCore(&stateDataClient{})
		if _, err := c.getOrFetchClientMainAuthWrapper(0, mtproto.AuthKeyTypePerm, 0); err == nil {
			t.Fatal("expected an error for auth key 0")
		}
		if _, err := c.getOrFetchMainAuthWrapper(0); err == nil {
			t.Fatal("expected an error for wrapper 0 (no shared wrapper for unknown keys)")
		}
	})
}
