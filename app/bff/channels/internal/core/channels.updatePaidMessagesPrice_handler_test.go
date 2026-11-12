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
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/channels/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"

	"github.com/zeromicro/go-zero/core/logx"
)

// fakeDirect is biz channel with three channels of the account 1, their
// creator: the broadcast channel 10, its monoforum 11 and the supergroup 12.
type fakeDirect struct {
	channel_client.ChannelClient
	asked *channelpb.TLChannelUpdateDirectMessages
}

func (f *fakeDirect) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	ch := &channelpb.ImmutableChannel{Id: in.GetChannelId(), AccessHash: 7, Title: "c"}
	switch in.GetChannelId() {
	case 10:
		ch.Broadcast = true
	case 11:
		ch.Megagroup, ch.Monoforum, ch.LinkedMonoforumId = true, true, 10
	case 12:
		ch.Megagroup = true
	default:
		return nil, mtproto.ErrChannelInvalid
	}
	return &channelpb.MutableChannel{
		Channel:      ch,
		Participants: []*channelpb.ImmutableChannelParticipant{{ChannelId: ch.Id, UserId: 1, Role: channelpb.RoleCreator, State: channelpb.StateIn}},
	}, nil
}

func (f *fakeDirect) ChannelUpdateDirectMessages(_ context.Context, in *channelpb.TLChannelUpdateDirectMessages) (*mtproto.Updates, error) {
	f.asked = in
	return mtproto.MakeTLUpdates(&mtproto.Updates{}).To_Updates(), nil
}

// The switch of a channel's direct messages: free alone, for a broadcast
// channel alone; a supergroup has only the price, which is the one there
// is.
func TestUpdatePaidMessagesPrice(t *testing.T) {
	f := &fakeDirect{}
	c := &ChannelsCore{
		ctx:    context.Background(),
		Logger: logx.WithContext(context.Background()),
		MD:     &metadata.RpcMetadata{UserId: 1, PermAuthKeyId: 77},
		svcCtx: &svc.ServiceContext{Channel: f},
	}
	input := func(id int64) *mtproto.InputChannel {
		return mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: id, AccessHash: 7}).To_InputChannel()
	}
	call := func(id int64, on bool, stars int64) error {
		_, err := c.ChannelsUpdatePaidMessagesPrice(&mtproto.TLChannelsUpdatePaidMessagesPrice{
			BroadcastMessagesAllowed: on, Channel: input(id), SendPaidMessagesStars: stars,
		})
		return err
	}
	if err := call(10, true, 5); !errors.Is(err, errStarsAmountInvalid) || f.asked != nil {
		t.Errorf("a price in stars: %v (biz asked: %v), want STARS_AMOUNT_INVALID", err, f.asked)
	}
	if err := call(11, true, 0); !errors.Is(err, errChannelMonoforumUnsupported) {
		t.Errorf("the direct messages of a monoforum: %v, want CHANNEL_MONOFORUM_UNSUPPORTED", err)
	}
	if err := call(12, false, 0); !errors.Is(err, mtproto.ErrChatNotModified) || f.asked != nil {
		t.Errorf("the price of a supergroup: %v, want CHAT_NOT_MODIFIED", err)
	}
	if err := call(13, true, 0); !errors.Is(err, mtproto.ErrChannelInvalid) {
		t.Errorf("a channel that does not exist: %v", err)
	}
	if err := call(10, true, 0); err != nil {
		t.Fatal(err)
	}
	if in := f.asked; in.GetSelfId() != 1 || in.GetPermAuthKeyId() != 77 || in.GetChannelId() != 10 || !mtproto.FromBool(in.GetEnabled()) {
		t.Errorf("switching on asked biz %v", in)
	}
	if err := call(10, false, 0); err != nil || mtproto.FromBool(f.asked.GetEnabled()) {
		t.Errorf("switching off: %v, biz asked %v", err, f.asked)
	}
	// tdesktop switches them off with the amount -1
	// (edit_peer_info_box.cpp:331-337).
	f.asked = nil
	if err := call(10, false, -1); err != nil || f.asked == nil || mtproto.FromBool(f.asked.GetEnabled()) {
		t.Errorf("switching off with -1: %v, biz asked %v", err, f.asked)
	}
	f.asked = nil
	if err := call(10, true, -1); !errors.Is(err, errStarsAmountInvalid) || f.asked != nil {
		t.Errorf("switching on with -1: %v (biz asked: %v), want STARS_AMOUNT_INVALID", err, f.asked)
	}
	if err := call(12, false, 5); !errors.Is(err, errStarsAmountInvalid) {
		t.Errorf("a price of a supergroup: %v, want STARS_AMOUNT_INVALID", err)
	}
}
