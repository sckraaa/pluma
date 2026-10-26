// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package core

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/dialogs/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/dialogs/internal/svc"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

// peerSettingsUsers is the user service of messages.hidePeerSettingsBar.
// Methods a test does not set up panic through the nil embedded interface.
type peerSettingsUsers struct {
	user_client.UserClient
	users  map[int64]*mtproto.ImmutableUser
	hidden []*userpb.TLUserDeletePeerSettings
}

func (f *peerSettingsUsers) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.Id {
		if u, ok := f.users[id]; ok {
			out.Datas = append(out.Datas, u)
		}
	}
	return out, nil
}

func (f *peerSettingsUsers) UserDeletePeerSettings(_ context.Context, in *userpb.TLUserDeletePeerSettings) (*mtproto.Bool, error) {
	f.hidden = append(f.hidden, in)
	return mtproto.BoolTrue, nil
}

func immutableUser(id int64, first string) *mtproto.ImmutableUser {
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{
			Id:         id,
			AccessHash: id * 10,
			FirstName:  first,
			UserType:   userpb.UserTypeRegular,
		}).To_UserData(),
	}).To_ImmutableUser()
}

// The bar goes away on every device: biz pushes updatePeerSettings with
// seq (ADR 0020) and gets the peer for it.
func TestHidePeerSettingsBar(t *testing.T) {
	users := &peerSettingsUsers{users: map[int64]*mtproto.ImmutableUser{1: immutableUser(1, "Me"), 2: immutableUser(2, "Bob")}}
	ctx := context.Background()
	c := &DialogsCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{UserClient: users}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: 1},
	}

	r, err := c.MessagesHidePeerSettingsBar(&mtproto.TLMessagesHidePeerSettingsBar{
		Peer: mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 2, AccessHash: 20}).To_InputPeer(),
	})
	if err != nil || !mtproto.FromBool(r) {
		t.Fatalf("%v, %v", r, err)
	}
	if len(users.hidden) != 1 {
		t.Fatalf("hidden %v", users.hidden)
	}
	h := users.hidden[0]
	if h.GetUserId() != 1 || h.GetPeerType() != mtproto.PEER_USER || h.GetPeerId() != 2 || len(h.GetUsers()) != 1 || h.GetUsers()[0].GetId() != 2 {
		t.Errorf("request %v: the peer with Bob", h)
	}

	// An unknown user, or no peer.
	for _, peer := range []*mtproto.InputPeer{
		mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 3, AccessHash: 30}).To_InputPeer(),
		mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(),
	} {
		_, err = c.MessagesHidePeerSettingsBar(&mtproto.TLMessagesHidePeerSettingsBar{Peer: peer})
		if st, _ := status.FromError(err); st.Message() != "PEER_ID_INVALID" {
			t.Errorf("%s: %v", peer.GetPredicateName(), err)
		}
	}
	if len(users.hidden) != 1 {
		t.Errorf("%d requests to biz", len(users.hidden))
	}
}
