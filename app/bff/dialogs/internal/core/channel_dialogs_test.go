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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

type fakeChannelDialogs struct {
	channel_client.ChannelClient
	dialogs []*mtproto.Dialog
}

func (f *fakeChannelDialogs) ChannelGetChannelDialogs(context.Context, *channelpb.TLChannelGetChannelDialogs) (*channelpb.Vector_Dialog, error) {
	return &channelpb.Vector_Dialog{Datas: f.dialogs}, nil
}

// The auto-delete period of a channel is the channel's: the dialog row of a
// member does not hold it.
func TestFillChannelDialogsPeriod(t *testing.T) {
	peer := mtproto.MakePeerChannel(7)
	c := &DialogsCore{
		ctx: context.Background(),
		MD:  &metadata.RpcMetadata{UserId: 1},
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{Channel: &fakeChannelDialogs{dialogs: []*mtproto.Dialog{
			mtproto.MakeTLDialog(&mtproto.Dialog{Peer: peer, TtlPeriod: mtproto.MakeFlagsInt32(86400)}).To_Dialog(),
		}}}},
	}
	list := dialog.DialogExtList{{Dialog: mtproto.MakeTLDialog(&mtproto.Dialog{Peer: peer}).To_Dialog()}}
	if err := c.fillChannelDialogs(list); err != nil {
		t.Fatal(err)
	}
	if got := list[0].Dialog.GetTtlPeriod().GetValue(); got != 86400 {
		t.Errorf("ttl_period %d", got)
	}
}
