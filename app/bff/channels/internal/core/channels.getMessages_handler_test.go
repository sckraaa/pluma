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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/channels/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	getMessagesMe      = int64(501)
	getMessagesChannel = int64(9001)
	getMessagesHash    = int64(777)
)

// pinnedChannels is a channel whose newest pinned message is pinned (0 for
// none); it answers the ids asked as messages.
type pinnedChannels struct {
	channel_client.ChannelClient
	pinned   int32
	searches int
	asked    []int32
}

func (f *pinnedChannels) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	return &channelpb.MutableChannel{
		Channel: &channelpb.ImmutableChannel{Id: in.ChannelId, AccessHash: getMessagesHash, Megagroup: true, Title: "pins"},
		Participants: []*channelpb.ImmutableChannelParticipant{
			{UserId: getMessagesMe, State: channelpb.StateIn},
		},
	}, nil
}

func (f *pinnedChannels) ChannelSearch(_ context.Context, in *channelpb.TLChannelSearch) (*mtproto.Messages_Messages, error) {
	f.searches++
	out := &mtproto.Messages_Messages{}
	if in.MediaType == mtproto.MEDIA_PINNED && in.Limit == 1 && in.SelfId == getMessagesMe && f.pinned != 0 {
		out.Messages = []*mtproto.Message{mtproto.MakeTLMessage(&mtproto.Message{Id: f.pinned}).To_Message()}
	}
	return out, nil
}

func (f *pinnedChannels) ChannelGetMessages(_ context.Context, in *channelpb.TLChannelGetMessages) (*channelpb.Vector_Message, error) {
	f.asked = append([]int32(nil), in.Id...)
	out := &channelpb.Vector_Message{}
	for _, id := range in.Id {
		out.Datas = append(out.Datas, mtproto.MakeTLMessage(&mtproto.Message{
			Id:     id,
			PeerId: mtproto.MakePeerChannel(in.ChannelId),
		}).To_Message())
	}
	return out, nil
}

type noUsers struct{ user_client.UserClient }

func (noUsers) UserGetMutableUsersV2(context.Context, *userpb.TLUserGetMutableUsersV2) (*mtproto.MutableUsers, error) {
	return &mtproto.MutableUsers{}, nil
}

func getMessagesCore(ch *pinnedChannels) *ChannelsCore {
	ctx := context.Background()
	return &ChannelsCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Channel: ch, User: noUsers{}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: getMessagesMe},
	}
}

func getMessagesRequest(ids ...*mtproto.InputMessage) *mtproto.TLChannelsGetMessages {
	return &mtproto.TLChannelsGetMessages{
		Channel:               mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: getMessagesChannel, AccessHash: getMessagesHash}).To_InputChannel(),
		Id_VECTORINPUTMESSAGE: ids,
	}
}

func byId(id int32) *mtproto.InputMessage {
	return mtproto.MakeTLInputMessageID(&mtproto.InputMessage{Id: id}).To_InputMessage()
}

func pinnedMessage() *mtproto.InputMessage {
	return mtproto.MakeTLInputMessagePinned(nil).To_InputMessage()
}

func messageIds(m *mtproto.Messages_Messages) []int32 {
	var ids []int32
	for _, msg := range m.GetMessages() {
		ids = append(ids, msg.GetId())
	}
	return ids
}

// inputMessagePinned is the newest pinned message of the channel, as
// channelFull.pinned_msg_id; it is asked once and not twice when also
// named by id.
func TestChannelsGetMessagesPinned(t *testing.T) {
	ch := &pinnedChannels{pinned: 42}
	got, err := getMessagesCore(ch).ChannelsGetMessages(getMessagesRequest(byId(7), pinnedMessage()))
	if err != nil {
		t.Fatal(err)
	}
	if ids := messageIds(got); !slices.Equal(ids, []int32{7, 42}) {
		t.Errorf("messages %v, want [7 42]", ids)
	}

	ch = &pinnedChannels{pinned: 42}
	if _, err = getMessagesCore(ch).ChannelsGetMessages(getMessagesRequest(byId(42), pinnedMessage())); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ch.asked, []int32{42}) {
		t.Errorf("asked %v, want [42] once", ch.asked)
	}
}

// Nothing pinned adds nothing; without inputMessagePinned the pins are
// not read at all.
func TestChannelsGetMessagesNothingPinned(t *testing.T) {
	ch := &pinnedChannels{}
	got, err := getMessagesCore(ch).ChannelsGetMessages(getMessagesRequest(byId(3), pinnedMessage()))
	if err != nil {
		t.Fatal(err)
	}
	if ids := messageIds(got); !slices.Equal(ids, []int32{3}) {
		t.Errorf("messages %v, want [3]", ids)
	}

	ch = &pinnedChannels{pinned: 9}
	if _, err = getMessagesCore(ch).ChannelsGetMessages(getMessagesRequest(byId(3))); err != nil {
		t.Fatal(err)
	}
	if ch.searches != 0 || !slices.Equal(ch.asked, []int32{3}) {
		t.Errorf("without inputMessagePinned: %d searches, asked %v", ch.searches, ch.asked)
	}
}
